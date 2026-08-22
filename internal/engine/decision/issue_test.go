package decision

import (
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/runtime"
)

// recorderStore 记录每次持久化的 IssuedSet（可注入错误）。
type recorderStore struct {
	sets []IssuedSet
	err  error
}

func (r *recorderStore) PersistIssued(s IssuedSet) error {
	if r.err != nil {
		return r.err
	}
	r.sets = append(r.sets, s)
	return nil
}

// issueDefinition 构造三个 sibling agent 步骤同时 eligible 的定义，用于
// min(C,N) 与补位测试：
//
//	boot.local ──→ work.a1 ─┐
//	  ├──--------→ work.a2 ─┼→ fin.report
//	  └──--------→ work.a3 ─┘
//
// ordinal：boot.local=0, work.a1=1, work.a2=2, work.a3=3, fin.report=4。
func issueDefinition(t *testing.T) *compiler.CompiledDefinition {
	t.Helper()
	reg := compiler.NewRegistry()
	for _, h := range []struct {
		id     authoring.HandlerID
		runner authoring.RunnerKind
	}{
		{"engine.test.boot", authoring.RunnerEngineLocal},
		{"engine.test.work", authoring.RunnerAgentWorker},
		{"engine.test.fin", authoring.RunnerEngineLocal},
	} {
		if err := reg.RegisterHandler(h.id, h.runner); err != nil {
			t.Fatalf("register handler: %v", err)
		}
	}
	for _, id := range []authoring.CodecID{"codec.test.in", "codec.test.out"} {
		if err := reg.RegisterCodec(id); err != nil {
			t.Fatalf("register codec: %v", err)
		}
	}
	if err := reg.RegisterPredicate("pred.test.work"); err != nil {
		t.Fatalf("register predicate: %v", err)
	}
	mk := func(s authoring.Step, err error) authoring.Step {
		if err != nil {
			t.Fatalf("construct step: %v", err)
		}
		return s
	}
	agent := func(id string) authoring.Step {
		io := authoring.IO{InputCodec: "codec.test.in", OutputCodec: "codec.test.out",
			Inputs:         []authoring.InputBinding{{From: "boot.local", OutputField: "out", ToField: "in"}},
			Postconditions: []authoring.PredicateRef{{ID: "pred.test.work"}}}
		h := authoring.Header{ID: authoring.StepID(id), NodeID: "work",
			Dependencies: []authoring.StepID{"boot.local"}, DefinitionVersion: testDefVersion}
		return mk(authoring.NewAgentStep(h, io, authoring.AgentSpec{
			Handler: "engine.test.work", Reason: authoring.ReasonCreativeImplementation, Timeout: time.Minute,
		}))
	}
	bootH := authoring.Header{ID: "boot.local", NodeID: "boot", DefinitionVersion: testDefVersion}
	finIO := authoring.IO{InputCodec: "codec.test.in", OutputCodec: "codec.test.out",
		Inputs: []authoring.InputBinding{
			{From: "work.a1", OutputField: "out", ToField: "in"},
			{From: "work.a2", OutputField: "out", ToField: "in"},
			{From: "work.a3", OutputField: "out", ToField: "in"},
		}}
	finH := authoring.Header{ID: "fin.report", NodeID: "fin",
		Dependencies: []authoring.StepID{"work.a1", "work.a2", "work.a3"}, DefinitionVersion: testDefVersion}
	def := &compiler.Definition{Version: testDefVersion, EntryNode: "boot", Steps: []authoring.Step{
		mk(authoring.NewLocalStep(bootH,
			authoring.IO{InputCodec: "codec.test.in", OutputCodec: "codec.test.out"},
			authoring.LocalSpec{Handler: "engine.test.boot"})),
		agent("work.a1"), agent("work.a2"), agent("work.a3"),
		mk(authoring.NewLocalStep(finH, finIO, authoring.LocalSpec{Handler: "engine.test.fin"})),
	}}
	cd, err := compiler.Compile(def, reg)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return cd
}

func readyPlan(t *testing.T, s *State, cd *compiler.CompiledDefinition) *Plan {
	t.Helper()
	p, err := Decide(s, Observation{}, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if p.Next.Kind != KindReady {
		t.Fatalf("plan kind = %s, want READY", p.Next.Kind)
	}
	return p
}

func issuedIDs(set IssuedSet) string {
	ids := make([]string, 0, len(set))
	for _, a := range set {
		ids = append(ids, string(a.Step))
	}
	return strings.Join(ids, ",")
}

// TestSelectIssuedMinCN 核对容量 C、可签发 N 时恰好签发 min(C,N)、固定
// 顺序与确定性 actionID。
func TestSelectIssuedMinCN(t *testing.T) {
	cd := issueDefinition(t)
	s, err := NewState(testDefVersion, runtime.PhaseDevelopmentParallel)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	complete(t, s, cd, "boot.local")
	plan := readyPlan(t, s, cd)
	if len(plan.Next.Ready.Tasks) != 3 {
		t.Fatalf("issuable N = %d, want 3", len(plan.Next.Ready.Tasks))
	}

	store := &recorderStore{}
	for _, tc := range []struct {
		cap  int
		want string
	}{
		{2, "work.a1,work.a2"},         // C < N：恰好 C 个
		{5, "work.a1,work.a2,work.a3"}, // C > N：恰好 N 个
		{0, ""},                        // C = 0：零个（机械裁剪，不报错）
	} {
		set, err := SelectIssued(plan, Admission{Capacity: tc.cap}, store)
		if err != nil {
			t.Fatalf("select C=%d: %v", tc.cap, err)
		}
		if got := issuedIDs(set); got != tc.want {
			t.Fatalf("C=%d issued %q, want %q", tc.cap, got, tc.want)
		}
	}
	// actionID 确定性派生（无随机/时间）。
	set, err := SelectIssued(plan, Admission{Capacity: 1}, store)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if set[0].ActionID != "act:work/work.a1" {
		t.Fatalf("actionID = %q, want act:work/work.a1", set[0].ActionID)
	}
	set2, _ := SelectIssued(plan, Admission{Capacity: 1}, store)
	if set2[0].ActionID != set[0].ActionID {
		t.Fatal("same plan must derive the same actionID")
	}
	// 持久化接口收到完整签发集（容量循环 3 次 + actionID 校验 2 次）。
	if len(store.sets) != 5 {
		t.Fatalf("persist calls = %d, want 5", len(store.sets))
	}
}

// TestSelectIssuedRefill 核对自动补位：receipt/result 释放容量后重新
// Decide/SelectIssued 立即按固定顺序补满下一批；已签发在途任务不被重复
// 签发。
func TestSelectIssuedRefill(t *testing.T) {
	cd := issueDefinition(t)
	s, err := NewState(testDefVersion, runtime.PhaseDevelopmentParallel)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	complete(t, s, cd, "boot.local")
	store := &recorderStore{}

	// 第一轮：C=1，N=3 → 签发 a1。
	set, err := SelectIssued(readyPlan(t, s, cd), Admission{Capacity: 1}, store)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got := issuedIDs(set); got != "work.a1" {
		t.Fatalf("first issue = %q, want work.a1", got)
	}
	// 在途排除：a1 ISSUED 后 Ready 只含 a2/a3。
	if err := s.TransitionTask(set[0].Task, runtime.TaskIssued); err != nil {
		t.Fatalf("transition: %v", err)
	}
	p := readyPlan(t, s, cd)
	if got, want := issuedIDsPlan(p), "work.a2,work.a3"; got != want {
		t.Fatalf("ready after issue = %q, want %q", got, want)
	}
	// a1 完成（容量释放）：C=1 时立即补位 a2；再释放补 a3。
	complete(t, s, cd, "work.a1")
	set, err = SelectIssued(readyPlan(t, s, cd), Admission{Capacity: 1}, store)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got := issuedIDs(set); got != "work.a2" {
		t.Fatalf("refill = %q, want work.a2", got)
	}
	complete(t, s, cd, "work.a2")
	set, err = SelectIssued(readyPlan(t, s, cd), Admission{Capacity: 1}, store)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got := issuedIDs(set); got != "work.a3" {
		t.Fatalf("refill = %q, want work.a3", got)
	}
	// a3 完成：无 agent 步骤剩余，frontier 只剩 engine-internal fin.report。
	complete(t, s, cd, "work.a3")
	p, err = Decide(s, Observation{}, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if p.Next.Kind != KindWait || p.Next.Wait.Reason != WaitEngineInternal {
		t.Fatalf("kind = %s/%v, want WAIT/ENGINE_INTERNAL", p.Next.Kind, p.Next.Wait)
	}
}

func issuedIDsPlan(p *Plan) string {
	ids := make([]string, 0, len(p.Next.Ready.Tasks))
	for _, tsk := range p.Next.Ready.Tasks {
		ids = append(ids, string(tsk.Step))
	}
	return strings.Join(ids, ",")
}

// TestSelectIssuedRejections 核对签发入口的接口校验。
func TestSelectIssuedRejections(t *testing.T) {
	cd := issueDefinition(t)
	s, err := NewState(testDefVersion, runtime.PhaseDevelopmentParallel)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	// Ready 计划与非 Ready 计划各一。
	waitPlan, err := Decide(s, Observation{}, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if waitPlan.Next.Kind != KindWait {
		t.Fatalf("setup: kind = %s, want WAIT", waitPlan.Next.Kind)
	}
	complete(t, s, cd, "boot.local")
	plan := readyPlan(t, s, cd)

	if _, err := SelectIssued(nil, Admission{Capacity: 1}, &recorderStore{}); err == nil {
		t.Error("nil plan must be rejected")
	}
	if _, err := SelectIssued(plan, Admission{Capacity: 1}, nil); err == nil {
		t.Error("nil store must be rejected")
	}
	if _, err := SelectIssued(plan, Admission{Capacity: -1}, &recorderStore{}); err == nil {
		t.Error("negative capacity must be rejected")
	}
	if _, err := SelectIssued(waitPlan, Admission{Capacity: 1}, &recorderStore{}); err == nil || !strings.Contains(err.Error(), "not READY") {
		t.Errorf("non-ready plan err = %v, want not READY", err)
	}
	// 持久化失败传播（签发未落账则不返回签发集）。
	failing := &recorderStore{err: errTestObserve}
	if _, err := SelectIssued(plan, Admission{Capacity: 1}, failing); err == nil || !strings.Contains(err.Error(), "persist") {
		t.Errorf("persist failure err = %v, want persist error", err)
	}
}

// TestNextResultValidate 穷举六类 Kind 的 tagged union 唯一性：缺 payload、
// 错 payload、双 payload、非法 Kind 全部拒绝；六类合法形态全部通过。
func TestNextResultValidate(t *testing.T) {
	valid := map[Kind]NextResult{
		KindReady:      {Kind: KindReady, Ready: &ReadyPayload{}},
		KindHostAction: {Kind: KindHostAction, HostAction: &HostActionPayload{}},
		KindAsk:        {Kind: KindAsk, Ask: &AskPayload{}},
		KindWait:       {Kind: KindWait, Wait: &WaitPayload{Reason: WaitEngineInternal}},
		KindOperator:   {Kind: KindOperator, Operator: &OperatorPayload{}},
		KindComplete:   {Kind: KindComplete, Complete: &CompletePayload{}},
	}
	for kind, n := range valid {
		if err := n.Validate(); err != nil {
			t.Errorf("valid %s: %v", kind, err)
		}
		// 缺对应 payload。
		missing := NextResult{Kind: kind}
		if err := missing.Validate(); err == nil {
			t.Errorf("missing payload for %s must be rejected", kind)
		}
		// 多余 payload（叠加另一个非空 payload）。
		extra := n
		extra.Ready = &ReadyPayload{}
		if kind == KindReady {
			extra.Ask = &AskPayload{}
		}
		if err := extra.Validate(); err == nil {
			t.Errorf("extra payload alongside %s must be rejected", kind)
		}
	}
	if err := (NextResult{Kind: "NOPE", Ready: &ReadyPayload{}}).Validate(); err == nil {
		t.Error("invalid kind must be rejected")
	}
	if err := (NextResult{}).Validate(); err == nil {
		t.Error("zero-value next result must be rejected")
	}
}
