// Package whitebox_qa 承载阶段 1 决策内核的独立白盒结构测试：用例从已确认
// 需求（master-requirements 范围 1–7、ADR-001 十条阶段落地验收）与本套测试
// 自行构造的夹具出发，直接面向 internal/engine 各包的实现结构编写，不依赖
// 交付中已有的任何测试。
package whitebox_qa

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
)

// ---- 夹具定义（六变体全覆盖 + 并行组 + 混合 authority） ----
//
// 图（A → B 表示 B 依赖 A）：
//
//	s.parse(n0) ─→ s.persist(n0)
//	            └→ s.ask(n1) / s.dispatch(n1) / s.review(n1)
//	            └→ s.fan(n2) ─→ s.left(n2) ─→ s.join(n2) ─→ s.cost(n3)
//	                          └→ s.right(n1? no: n2) ↗
//
// s.left/s.right 均在 n2；s.cost 在 n3 且依赖 s.join 与 s.ask。

const fxDefVersion = authoring.DefinitionVersion("1")

// fxOrdinals 是夹具图的确定性 Kahn 拓扑序（(nodeID, stepID) 字典序选点）。
var fxOrdinals = map[string]int{
	"s.parse": 0, "s.persist": 1,
	"s.ask": 2, "s.dispatch": 3, "s.review": 4,
	"s.fan": 5, "s.left": 6, "s.right": 7, "s.join": 8,
	"s.cost": 9,
}

// fxOrderedIDs 是编译产物按 (nodeID, ordinal, id) 排序后的步骤序列。
var fxOrderedIDs = []string{
	"s.parse", "s.persist", "s.ask", "s.dispatch", "s.review",
	"s.fan", "s.left", "s.right", "s.join", "s.cost",
}

func fxHeader(id, node string, deps ...string) authoring.Header {
	ds := make([]authoring.StepID, 0, len(deps))
	for _, d := range deps {
		ds = append(ds, authoring.StepID(d))
	}
	return authoring.Header{
		ID: authoring.StepID(id), NodeID: authoring.NodeID(node),
		Dependencies: ds, DefinitionVersion: fxDefVersion,
	}
}

// fxIO 构造合法共享 IO 段：每个依赖恰好一个 typed input binding。
func fxIO(deps ...string) authoring.IO {
	inputs := make([]authoring.InputBinding, 0, len(deps))
	for _, d := range deps {
		inputs = append(inputs, authoring.InputBinding{
			From: authoring.StepID(d), OutputField: "out", ToField: "in",
		})
	}
	return authoring.IO{InputCodec: "c.in", OutputCodec: "c.out", Inputs: inputs}
}

// mk*Step 系列是夹具用的 constructor 薄包装：失败即 Fatal（夹具步骤都应是
// 合法定义）。
func mkLocalStep(t *testing.T, h authoring.Header, io authoring.IO, spec authoring.LocalSpec) authoring.LocalStep {
	t.Helper()
	s, err := authoring.NewLocalStep(h, io, spec)
	if err != nil {
		t.Fatalf("fixture NewLocalStep: %v", err)
	}
	return s
}

func mkDurableStep(t *testing.T, h authoring.Header, io authoring.IO, spec authoring.DurableSpec) authoring.DurableStep {
	t.Helper()
	s, err := authoring.NewDurableStep(h, io, spec)
	if err != nil {
		t.Fatalf("fixture NewDurableStep: %v", err)
	}
	return s
}

func mkHostActionStep(t *testing.T, h authoring.Header, io authoring.IO, spec authoring.HostActionSpec) authoring.HostActionStep {
	t.Helper()
	s, err := authoring.NewHostActionStep(h, io, spec)
	if err != nil {
		t.Fatalf("fixture NewHostActionStep: %v", err)
	}
	return s
}

func mkAgentStep(t *testing.T, h authoring.Header, io authoring.IO, spec authoring.AgentSpec) authoring.AgentStep {
	t.Helper()
	s, err := authoring.NewAgentStep(h, io, spec)
	if err != nil {
		t.Fatalf("fixture NewAgentStep: %v", err)
	}
	return s
}

func mkHumanAskStep(t *testing.T, h authoring.Header, spec authoring.HumanAskSpec) authoring.HumanAskStep {
	t.Helper()
	s, err := authoring.NewHumanAskStep(h, spec)
	if err != nil {
		t.Fatalf("fixture NewHumanAskStep: %v", err)
	}
	return s
}

func mkParallelStep(t *testing.T, h authoring.Header, spec authoring.ParallelSpec) authoring.ParallelStep {
	t.Helper()
	s, err := authoring.NewParallelStep(h, spec)
	if err != nil {
		t.Fatalf("fixture NewParallelStep: %v", err)
	}
	return s
}

func fxParse(t *testing.T) authoring.LocalStep {
	t.Helper()
	return mkLocalStep(t, fxHeader("s.parse", "n0"), fxIO(),
		authoring.LocalSpec{Handler: "h.s.parse"})
}

func fxPersist(t *testing.T) authoring.DurableStep {
	t.Helper()
	return mkDurableStep(t, fxHeader("s.persist", "n0", "s.parse"), fxIO("s.parse"),
		authoring.DurableSpec{
			Handler: "h.s.persist", Idempotency: authoring.IdempotencyDeterministicInput,
			Reconcile: "r.persist", Timeout: 30 * time.Second,
			Retry: authoring.RetryPolicy{MaxAttempts: 3, Backoff: time.Second},
		})
}

func fxDispatch(t *testing.T) authoring.HostActionStep {
	t.Helper()
	return mkHostActionStep(t, fxHeader("s.dispatch", "n1", "s.parse"), fxIO("s.parse"),
		authoring.HostActionSpec{
			Handler: "h.s.dispatch", Boundary: authoring.BoundaryExternalCapability,
			Operation: "op.x", Timeout: 5 * time.Second,
		})
}

func fxReview(t *testing.T) authoring.AgentStep {
	t.Helper()
	io := fxIO("s.parse")
	io.Postconditions = []authoring.PredicateRef{{ID: "pred.done"}}
	return mkAgentStep(t, fxHeader("s.review", "n1", "s.parse"), io,
		authoring.AgentSpec{
			Handler: "h.s.review", Reason: authoring.ReasonSemanticJudgment, Timeout: time.Minute,
		})
}

func fxAsk(t *testing.T) authoring.HumanAskStep {
	t.Helper()
	return mkHumanAskStep(t, fxHeader("s.ask", "n1", "s.parse"),
		authoring.HumanAskSpec{
			AskKind: "confirm", RequestSchema: "s.req", ResponseSchema: "s.resp",
			FreshnessTTL: 10 * time.Minute,
		})
}

func fxFan(t *testing.T) authoring.ParallelStep {
	t.Helper()
	return mkParallelStep(t, fxHeader("s.fan", "n2", "s.parse"),
		authoring.ParallelSpec{
			Children: []authoring.StepID{"s.left", "s.right"},
			Join:     authoring.JoinPolicy{JoinStep: "s.join", Mode: authoring.JoinAll},
			Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
		})
}

func fxLeft(t *testing.T) authoring.LocalStep {
	t.Helper()
	return mkLocalStep(t, fxHeader("s.left", "n2", "s.fan"), fxIO("s.fan"),
		authoring.LocalSpec{Handler: "h.s.left"})
}

func fxRight(t *testing.T) authoring.LocalStep {
	t.Helper()
	return mkLocalStep(t, fxHeader("s.right", "n2", "s.fan"), fxIO("s.fan"),
		authoring.LocalSpec{Handler: "h.s.right"})
}

func fxJoin(t *testing.T) authoring.LocalStep {
	t.Helper()
	return mkLocalStep(t, fxHeader("s.join", "n2", "s.left", "s.right"),
		fxIO("s.left", "s.right"), authoring.LocalSpec{Handler: "h.s.join"})
}

func fxCost(t *testing.T) authoring.LocalStep {
	t.Helper()
	return mkLocalStep(t, fxHeader("s.cost", "n3", "s.join", "s.ask"),
		fxIO("s.join", "s.ask"), authoring.LocalSpec{
			Handler: "h.s.cost", Retry: &authoring.RetryPolicy{MaxAttempts: 2},
		})
}

func fxAllSteps(t *testing.T) []authoring.Step {
	t.Helper()
	return []authoring.Step{
		fxParse(t), fxPersist(t), fxDispatch(t), fxReview(t), fxAsk(t),
		fxFan(t), fxLeft(t), fxRight(t), fxJoin(t), fxCost(t),
	}
}

func fxDefinition(t *testing.T, steps ...authoring.Step) *compiler.Definition {
	t.Helper()
	if len(steps) == 0 {
		steps = fxAllSteps(t)
	}
	return &compiler.Definition{Version: fxDefVersion, EntryNode: "n0", Steps: steps}
}

// fxRegistry 注册夹具定义引用的全部封闭 ID。
func fxRegistry(t *testing.T) *compiler.Registry {
	t.Helper()
	return fxRegistryBase(t, false)
}

// fxRegistryExtra 在夹具注册之上追加额外（未被夹具定义引用）的注册，
// 供 digest 敏感性/止损用例替换字段值。
func fxRegistryExtra(t *testing.T) *compiler.Registry {
	t.Helper()
	return fxRegistryBase(t, true)
}

func fxRegistryBase(t *testing.T, extras bool) *compiler.Registry {
	t.Helper()
	reg := compiler.NewRegistry()
	handlers := []struct {
		id     authoring.HandlerID
		runner authoring.RunnerKind
	}{
		{"h.s.parse", authoring.RunnerEngineLocal},
		{"h.s.persist", authoring.RunnerDurableActivity},
		{"h.s.dispatch", authoring.RunnerHostAdapter},
		{"h.s.review", authoring.RunnerAgentWorker},
		{"h.s.left", authoring.RunnerEngineLocal},
		{"h.s.right", authoring.RunnerEngineLocal},
		{"h.s.join", authoring.RunnerEngineLocal},
		{"h.s.cost", authoring.RunnerEngineLocal},
	}
	if extras {
		handlers = append(handlers,
			struct {
				id     authoring.HandlerID
				runner authoring.RunnerKind
			}{"h.alt", authoring.RunnerEngineLocal},
			struct {
				id     authoring.HandlerID
				runner authoring.RunnerKind
			}{"h.newbiz", authoring.RunnerEngineLocal})
	}
	for _, h := range handlers {
		if err := reg.RegisterHandler(h.id, h.runner); err != nil {
			t.Fatalf("fixture registry: %v", err)
		}
	}
	regsitryMisc(t, reg, extras)
	return reg
}

func regsitryMisc(t *testing.T, reg *compiler.Registry, extras bool) {
	t.Helper()
	codecs := []authoring.CodecID{"c.in", "c.out"}
	if extras {
		codecs = append(codecs, "c.alt")
	}
	for _, id := range codecs {
		if err := reg.RegisterCodec(id); err != nil {
			t.Fatal(err)
		}
	}
	preds := []authoring.PredicateID{"pred.done"}
	if extras {
		preds = append(preds, "pred.alt")
	}
	for _, id := range preds {
		if err := reg.RegisterPredicate(id); err != nil {
			t.Fatal(err)
		}
	}
	recs := []authoring.ReconcileID{"r.persist"}
	if extras {
		recs = append(recs, "r.alt")
	}
	for _, id := range recs {
		if err := reg.RegisterReconciler(id); err != nil {
			t.Fatal(err)
		}
	}
	schemas := []authoring.SchemaID{"s.req", "s.resp"}
	if extras {
		schemas = append(schemas, "s.alt")
	}
	for _, id := range schemas {
		if err := reg.RegisterSchema(id); err != nil {
			t.Fatal(err)
		}
	}
	ops := []authoring.OperationID{"op.x"}
	if extras {
		ops = append(ops, "op.alt")
	}
	for _, id := range ops {
		if err := reg.RegisterOperation(id); err != nil {
			t.Fatal(err)
		}
	}
}

// fxRegistryWithout 返回缺少 skip 中列出 ID 的夹具 registry（模拟未注册）。
func fxRegistryWithout(t *testing.T, skip ...string) *compiler.Registry {
	t.Helper()
	skipped := make(map[string]bool, len(skip))
	for _, s := range skip {
		skipped[s] = true
	}
	reg := compiler.NewRegistry()
	for _, h := range []struct {
		id     authoring.HandlerID
		runner authoring.RunnerKind
	}{
		{"h.s.parse", authoring.RunnerEngineLocal},
		{"h.s.persist", authoring.RunnerDurableActivity},
		{"h.s.dispatch", authoring.RunnerHostAdapter},
		{"h.s.review", authoring.RunnerAgentWorker},
		{"h.s.left", authoring.RunnerEngineLocal},
		{"h.s.right", authoring.RunnerEngineLocal},
		{"h.s.join", authoring.RunnerEngineLocal},
		{"h.s.cost", authoring.RunnerEngineLocal},
	} {
		if skipped[string(h.id)] {
			continue
		}
		if err := reg.RegisterHandler(h.id, h.runner); err != nil {
			t.Fatalf("fixture registry: %v", err)
		}
	}
	for _, id := range []authoring.CodecID{"c.in", "c.out"} {
		if !skipped[string(id)] {
			if err := reg.RegisterCodec(id); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, id := range []authoring.PredicateID{"pred.done"} {
		if !skipped[string(id)] {
			if err := reg.RegisterPredicate(id); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, id := range []authoring.ReconcileID{"r.persist"} {
		if !skipped[string(id)] {
			if err := reg.RegisterReconciler(id); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, id := range []authoring.SchemaID{"s.req", "s.resp"} {
		if !skipped[string(id)] {
			if err := reg.RegisterSchema(id); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, id := range []authoring.OperationID{"op.x"} {
		if !skipped[string(id)] {
			if err := reg.RegisterOperation(id); err != nil {
				t.Fatal(err)
			}
		}
	}
	return reg
}

func fxCompile(t *testing.T, def *compiler.Definition, reg *compiler.Registry) *compiler.CompiledDefinition {
	t.Helper()
	cd, err := compiler.Compile(def, reg)
	if err != nil {
		t.Fatalf("fixture compile: %v", err)
	}
	return cd
}

// stepID 返回任一夹具变体的步骤 ID。
func stepID(s authoring.Step) string {
	switch v := s.(type) {
	case authoring.LocalStep:
		return string(v.Header.ID)
	case authoring.DurableStep:
		return string(v.Header.ID)
	case authoring.HostActionStep:
		return string(v.Header.ID)
	case authoring.AgentStep:
		return string(v.Header.ID)
	case authoring.HumanAskStep:
		return string(v.Header.ID)
	case authoring.ParallelStep:
		return string(v.Header.ID)
	}
	return "?"
}

// withStep 用 repl 替换 steps 中同 ID 的步骤（顺序无关紧要：assembly 顺序不进入产物）。
func withStep(steps []authoring.Step, id string, repl authoring.Step) []authoring.Step {
	out := make([]authoring.Step, 0, len(steps))
	for _, s := range steps {
		if stepID(s) != id {
			out = append(out, s)
		}
	}
	return append(out, repl)
}

func withoutStep(steps []authoring.Step, id string) []authoring.Step {
	out := make([]authoring.Step, 0, len(steps))
	for _, s := range steps {
		if stepID(s) != id {
			out = append(out, s)
		}
	}
	return out
}

// wantCompileErr 断言 err 是 *compiler.Error、Class 符合且消息包含全部 substr。
func wantCompileErr(t *testing.T, err error, class authoring.FailureClass, substr ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected compiler error, got nil")
	}
	var ce *compiler.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error %v (%T) is not *compiler.Error", err, err)
	}
	if ce.Class != class {
		t.Fatalf("error class = %s, want %s (msg %q)", ce.Class, class, ce.Msg)
	}
	for _, s := range substr {
		if !strings.Contains(ce.Msg, s) {
			t.Fatalf("error msg %q missing substring %q", ce.Msg, s)
		}
	}
}

func wantErrContaining(t *testing.T, err error, substr ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	for _, s := range substr {
		if !strings.Contains(err.Error(), s) {
			t.Fatalf("error %q missing substring %q", err.Error(), s)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", "..", ".."))
}

func readCheckedInArtifact(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "definitions", "workflow.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// findStep 在编译产物中按 ID 定位步骤。
func findStep(t *testing.T, cd *compiler.CompiledDefinition, id string) *compiler.CompiledStep {
	t.Helper()
	for i := range cd.Steps {
		if string(cd.Steps[i].Header.ID) == id {
			return &cd.Steps[i]
		}
	}
	t.Fatalf("step %q not found in compiled definition", id)
	return nil
}

// fakeCollector 是 Observe 用例的可编程收集器。
type fakeCollector struct {
	src   decision.FactSource
	facts []decision.Fact
	err   error
}

func (c *fakeCollector) Source() decision.FactSource { return c.src }
func (c *fakeCollector) Collect(*decision.State) ([]decision.Fact, error) {
	return c.facts, c.err
}

// recordingStore 记录 SelectIssued 落账内容的 ActionStore 桩。
type recordingStore struct {
	sets []decision.IssuedSet
	err  error
}

func (s *recordingStore) PersistIssued(set decision.IssuedSet) error {
	s.sets = append(s.sets, set)
	return s.err
}

// tamperArtifact 把制品字节经 map 修改后重新序列化（保持信封常量，除非用例
// 刻意改写），用于 Decode 严格性用例。
func tamperArtifact(t *testing.T, data []byte, mut func(m map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("tamper: unmarshal: %v", err)
	}
	mut(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("tamper: marshal: %v", err)
	}
	return out
}

// canonicalEncode 与 encoder/decision/shadow 的 canonical 形态一致：JSON、
// 2 空格缩进、不转义 HTML、恰一个尾随换行。
func canonicalEncode(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatalf("canonical encode: %v", err)
	}
	return buf.Bytes()
}

// legacyRunJSON 构造 legacy run 状态文件内容（字段名与 runstate 布局一致）。
func legacyRunJSON(runID, status string, actions, gates map[string]string, selectedGates ...string) string {
	m := map[string]any{
		"runId": runID, "flow": "no-split", "status": status,
		"requirementSource": "req.md", "requirementRevision": "rev1",
		"basePromptRevision": "bp1", "catalogRevision": "cat1",
		"vcs": "git", "baseSnapshot": "b0", "currentSnapshot": "c1",
		"selectedGates": selectedGates,
	}
	am := map[string]any{}
	for k, v := range actions {
		am[k] = map[string]string{"status": v}
	}
	gm := map[string]any{}
	for k, v := range gates {
		gm[k] = map[string]string{"status": v}
	}
	m["actions"] = am
	m["gates"] = gm
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func allPassActions() map[string]string {
	return map[string]string{
		"requirements-clarification": "PASS",
		"product-review":             "PASS",
		"start-readiness":            "PASS",
		"development-worker":         "PASS",
	}
}

// writeLegacyRun 在 root/.gates/tmp/<runID>/state.json 写入 legacy 状态。
func writeLegacyRun(t *testing.T, root, runID, stateJSON string) string {
	t.Helper()
	dir := filepath.Join(root, ".gates", "tmp", runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "state.json")
	if err := os.WriteFile(p, []byte(stateJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
