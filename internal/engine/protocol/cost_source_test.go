package protocol

import (
	"testing"
	"time"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/runtime"
)

// costSourceProbe 是可控的计量来源解析器（测试观察解析调用与返回三态）。
type costSourceProbe struct {
	state  CostSourceState
	usage  cost.Usage
	called int
}

func (p *costSourceProbe) resolver() CostSourceResolver {
	return func(ref CostSourceRef) ResolvedCostSource {
		p.called++
		if ref.Provider != testProvider || ref.ActionID == "" || ref.Correlation == "" {
			return ResolvedCostSource{State: CostSourceUnavailable}
		}
		return ResolvedCostSource{State: p.state, Usage: p.usage}
	}
}

// costEngineFixture 构造已初始化、绑定计量解析器的引擎（非审查 agent 步，
// 普通 PASS 结果即可完成步骤）。
func costEngineFixture(t *testing.T, resolver CostSourceResolver) *Engine {
	t.Helper()
	cd, reg := costAgentFixture(t)
	store, err := persistence.NewStore(t.TempDir(), persistence.Config{PackageDigest: "sha256:test-package"})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(store, Config{Definition: cd, Registry: reg, Capacity: 1, CostSource: resolver}, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := decision.NewState(cd.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatal(err)
	}
	if err := view.CompleteStep("prep.parse", cd); err != nil {
		t.Fatal(err)
	}
	if err := engine.Init(view, testProvider, fingerprintOf(t, engine)); err != nil {
		t.Fatal(err)
	}
	return engine
}

// costAgentFixture 是两个非审查 agent 步的最小定义（capacity 1 一次签发
// 一个；PASS 完成步骤并补位）。
func costAgentFixture(t *testing.T) (*compiler.CompiledDefinition, *compiler.Registry) {
	t.Helper()
	reg := compiler.NewRegistry()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	must(reg.RegisterHandler("engine.cost.parse", authoring.RunnerEngineLocal))
	must(reg.RegisterHandler("engine.cost.worker", authoring.RunnerAgentWorker))
	must(reg.RegisterCodec("codec.any.in"))
	must(reg.RegisterCodec("codec.any.out"))
	must(reg.RegisterPredicate("pred.cost.post"))
	header := func(id string, deps ...string) authoring.Header {
		ds := make([]authoring.StepID, 0, len(deps))
		for _, d := range deps {
			ds = append(ds, authoring.StepID(d))
		}
		return authoring.Header{ID: authoring.StepID(id), NodeID: "cost", Dependencies: ds, DefinitionVersion: "1"}
	}
	io := authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out",
		Inputs:         []authoring.InputBinding{{From: "prep.parse", OutputField: "out", ToField: "in"}},
		Postconditions: []authoring.PredicateRef{{ID: "pred.cost.post"}}}
	parse, err := authoring.NewLocalStep(header("prep.parse"), authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out"}, authoring.LocalSpec{Handler: "engine.cost.parse"})
	must(err)
	worker, err := authoring.NewAgentStep(header("cost.worker", "prep.parse"), io, authoring.AgentSpec{
		Handler: "engine.cost.worker", Reason: authoring.ReasonCreativeImplementation, Timeout: time.Minute,
	})
	must(err)
	cd, err := compiler.Compile(&compiler.Definition{
		Version: "1", EntryNode: "cost", Steps: []authoring.Step{parse, worker},
	}, reg)
	must(err)
	return cd, reg
}

// dispatchPassResult 签发首个 agent 任务、落 SPAWNED 回执并提交 PASS 结果，
// 返回 acceptance（错误由调用方断言）。
func dispatchPassResult(t *testing.T, engine *Engine, eventID string) (Acceptance, error) {
	t.Helper()
	fp := fingerprintOf(t, engine)
	plan, _, err := engine.Plan()
	if err != nil {
		t.Fatal(err)
	}
	issued, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 1}, fp)
	if err != nil || len(issued) != 1 {
		t.Fatalf("issue = %+v err=%v", issued, err)
	}
	action := issued[0].ActionID
	if _, err := engine.Submit(spawnReceipt(t, "cost-spawn", action, "agent-cost-1"), fp); err != nil {
		t.Fatal(err)
	}
	event, err := NewWorkerResultEvent(EventID(eventID), action, testProvider, OutcomePass, "sha256:cost-payload", "")
	if err != nil {
		t.Fatal(err)
	}
	return engine.Submit(event, fp)
}

// TestWorkerResultHeldAtCostPending：lifecycle stop 未到（PENDING）——结果滞
// 留回执边界：acceptance＝COST_PENDING、不完成步骤、不写用量；stop 证据落
// 定的下一事务在同一配对路径补齐（READY 精确用量入账、结果完成）。
func TestWorkerResultHeldAtCostPending(t *testing.T) {
	probe := &costSourceProbe{state: CostSourcePending}
	engine := costEngineFixture(t, probe.resolver())
	fp := fingerprintOf(t, engine)
	plan, _, err := engine.Plan()
	if err != nil {
		t.Fatal(err)
	}
	issued, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 1}, fp)
	if err != nil || len(issued) != 1 {
		t.Fatalf("issue = %+v err=%v", issued, err)
	}
	action := issued[0].ActionID
	if _, err := engine.Submit(spawnReceipt(t, "cost-spawn", action, "agent-cost-1"), fp); err != nil {
		t.Fatal(err)
	}
	event, err := NewWorkerResultEvent("cost-result", action, testProvider, OutcomePass, "sha256:cost-payload", "")
	if err != nil {
		t.Fatal(err)
	}
	acceptance, err := engine.Submit(event, fp)
	if err != nil {
		t.Fatalf("submit pending-cost result: %v", err)
	}
	if acceptance.Status != StatusCostPending {
		t.Fatalf("acceptance = %+v, want COST_PENDING", acceptance)
	}
	snap := engine.LoadMustSucceed(t)
	if _, admitted := snap.State.Results[action]; admitted {
		t.Fatal("pending-cost result was admitted into Results")
	}
	if _, staged := snap.State.StagedResults[action]; !staged {
		t.Fatal("pending-cost result was not held at the receipt boundary")
	}
	if len(snap.State.Completed) != 1 {
		t.Fatalf("pending-cost result completed steps: %+v", snap.State.Completed)
	}
	if snap.State.Cost != nil {
		t.Fatalf("pending source wrote a cost entry: %+v", snap.State.Cost)
	}
	// stop 证据到达：来源转 READY（精确用量），同一配对路径完成并记录用量。
	probe.state, probe.usage = CostSourceReady, cost.Usage{InputCacheMissTokens: 120, OutputTokens: 30}
	if _, err := engine.Submit(lifecycleEventOf(t, "cost-start", "agent-cost-1", LifecycleStart), fp); err != nil {
		t.Fatal(err)
	}
	snap = engine.LoadMustSucceed(t)
	if _, admitted := snap.State.Results[action]; !admitted {
		t.Fatalf("resolved source did not complete the held result: staged=%+v", snap.State.StagedResults)
	}
	entry := snap.State.Cost.Dispatches[action]
	if entry.Source != cost.SourceTranscript || entry.InputCacheMissTokens != 120 || entry.OutputTokens != 30 || entry.TotalInputTokens != 120 {
		t.Fatalf("cost entry = %+v", entry)
	}
	if snap.State.Cost.TotalInputTokens != 120 || snap.State.Cost.OutputTokens != 30 {
		t.Fatalf("run totals = %+v", snap.State.Cost)
	}
	if len(snap.State.Completed) != 2 {
		t.Fatalf("resolved result did not complete its step: %+v", snap.State.Completed)
	}
}

// TestUnavailableSourceFallsBackToRequestGuard：来源明确不可用——记
// UNAVAILABLE 条目（零数字、不伪造）后照常完成，request/attempt guard 继续
// 生效。
func TestUnavailableSourceFallsBackToRequestGuard(t *testing.T) {
	probe := &costSourceProbe{state: CostSourceUnavailable}
	engine := costEngineFixture(t, probe.resolver())
	acceptance, err := dispatchPassResult(t, engine, "cost-result")
	if err != nil || acceptance.Status != "ACCEPTED" {
		t.Fatalf("unavailable-source result = %+v err=%v", acceptance, err)
	}
	snap := engine.LoadMustSucceed(t)
	entry := snap.State.Cost.Dispatches[acceptance.ActionID]
	if entry.Source != cost.SourceUnavailable || entry.TotalInputTokens != 0 || entry.OutputTokens != 0 {
		t.Fatalf("unavailable entry = %+v", entry)
	}
	if _, admitted := snap.State.Results[acceptance.ActionID]; !admitted {
		t.Fatal("unavailable source blocked result admission")
	}
}

// TestNoCostSourceKeepsLegacyBehavior：未装 bridge（nil）时不参与判定——结
// 果照常完成、无 cost 投影。
func TestNoCostSourceKeepsLegacyBehavior(t *testing.T) {
	engine := costEngineFixture(t, nil)
	acceptance, err := dispatchPassResult(t, engine, "cost-result")
	if err != nil || acceptance.Status != "ACCEPTED" {
		t.Fatalf("result = %+v err=%v", acceptance, err)
	}
	if snap := engine.LoadMustSucceed(t); snap.State.Cost != nil {
		t.Fatalf("nil bridge wrote a cost projection: %+v", snap.State.Cost)
	}
}

// lifecycleEventOf 构造 lifecycle 观察事件（identity 兼作 correlation，与
// 回执 correlation 配对）。
func lifecycleEventOf(t *testing.T, id, identity, event string) Event {
	t.Helper()
	ev, err := NewCorrelatedLifecycleEvent(EventID(id), testProvider, identity, identity, event)
	if err != nil {
		t.Fatalf("lifecycle event: %v", err)
	}
	return ev
}
