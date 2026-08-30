// Package definition 持有本项目唯一的 workflow definition authoring 源与
// 同源生成入口（ADR-001 决策 4/6）：
//
//   - Workflow/Registry 是包内 authoring 定义表——显式步骤表经 authoring
//     constructor 构造，registry 注册表覆盖定义引用的全部封闭 ID；
//   - Generate 从定义表编译→编码→写盘，一次动作同时产出 canonical 制品
//     definitions/workflow.json 与期望身份常量 identity_gen.go，禁止人工
//     双写 digest。
//
// 本包不重复 encoder/compiler 的机械职责：canonical 形态在 encoder，图不变
// 量与物化在 compiler；这里只有"定义内容本身"。
package definition

import (
	"fmt"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
)

// Version 是 definition 版本信封。批次 1c 起内容从阶段 0 的纯版本信封扩展
// 为完整拓扑（definitions/workflow.json），按"changed bytes 必须 bump"规则
// 从 "1" 晋升 "2"。identity_gen.go 的 WorkflowDefinitionVersion 与其同源。
const Version authoring.DefinitionVersion = "2"

// entryNode 是定义的唯一入口节点（可达性校验的起点）。
const entryNode authoring.NodeID = "entry"

// 定义表拓扑（九步覆盖全部六变体 + 并行组，与 compiler 批的金图表同构；
// 图表 A → B 表示 B 依赖 A）：
//
//	entry.parse ──→ entry.persist ──→ ask.decide ──┐
//	    │              └──────────→ review.worker  │
//	    └──→ fan.split ──→ fan.slice ──→ fan.join ─┴→ report.cost
//	                └───→ fan.transport ──↗
var (
	workflowHandlers = []struct {
		id     authoring.HandlerID
		runner authoring.RunnerKind
	}{
		{"engine.entry.parse", authoring.RunnerEngineLocal},
		{"engine.entry.persist", authoring.RunnerDurableActivity},
		{"engine.review.worker", authoring.RunnerAgentWorker},
		{"engine.fan.slice", authoring.RunnerEngineLocal},
		{"engine.fan.transport", authoring.RunnerHostAdapter},
		{"engine.fan.join", authoring.RunnerEngineLocal},
		{"engine.report.cost", authoring.RunnerEngineLocal},
	}
	workflowCodecs     = []authoring.CodecID{"codec.any.in", "codec.any.out"}
	workflowPredicates = []authoring.PredicateID{"pred.review.post"}
	workflowReconciles = []authoring.ReconcileID{"reconcile.entry.persist"}
	workflowSchemas    = []authoring.SchemaID{"schema.ask.decision.request", "schema.ask.decision.response", "schema.host.fan.transport"}
	workflowOperations = []authoring.OperationID{"op.fan.transport"}
	workflowAskKinds   = []authoring.AskKindID{"decision"}
)

// Registry 返回注册了定义引用的全部封闭 ID 的 registry。条目不携带实现：
// 实现绑定属后续运行时批次，编译期只需存在性、唯一性与 kind 匹配。
func Registry() *compiler.Registry {
	reg := compiler.NewRegistry()
	for _, h := range workflowHandlers {
		must(h.id, reg.RegisterHandler(h.id, h.runner))
	}
	for _, id := range workflowCodecs {
		must(id, reg.RegisterCodec(id))
	}
	for _, id := range workflowPredicates {
		must(id, reg.RegisterPredicate(id))
	}
	for _, id := range workflowReconciles {
		must(id, reg.RegisterReconciler(id))
	}
	for _, id := range workflowSchemas {
		must(id, reg.RegisterSchema(id))
	}
	for _, id := range workflowOperations {
		must(id, reg.RegisterOperation(id))
	}
	for _, id := range workflowAskKinds {
		must(id, reg.RegisterAskKind(id))
	}
	return reg
}

// Workflow 经 authoring constructor 构造定义表。每次调用产生全新实例；
// 表内容是编译期常量，constructor 失败只可能是本表自身缺陷，直接 panic。
func Workflow() *compiler.Definition {
	parse, err := authoring.NewLocalStep(header("entry.parse", "entry"), ioWith(),
		authoring.LocalSpec{Handler: "engine.entry.parse"})
	must("entry.parse", err)
	persist, err := authoring.NewDurableStep(header("entry.persist", "entry", "entry.parse"),
		ioWith("entry.parse"), authoring.DurableSpec{
			Handler: "engine.entry.persist", Idempotency: authoring.IdempotencyDeterministicInput,
			Reconcile: "reconcile.entry.persist", Timeout: 30 * time.Second,
			Retry: authoring.RetryPolicy{MaxAttempts: 3, Backoff: 2 * time.Second},
		})
	must("entry.persist", err)
	reviewIO := ioWith("entry.persist")
	reviewIO.Postconditions = []authoring.PredicateRef{{ID: "pred.review.post"}}
	review, err := authoring.NewAgentStep(header("review.worker", "review", "entry.persist"),
		reviewIO, authoring.AgentSpec{
			Handler: "engine.review.worker", Reason: authoring.ReasonIndependentReview, Timeout: time.Minute,
		})
	must("review.worker", err)
	ask, err := authoring.NewHumanAskStep(header("ask.decide", "ask", "entry.persist"),
		authoring.HumanAskSpec{
			AskKind: "decision", RequestSchema: "schema.ask.decision.request",
			ResponseSchema: "schema.ask.decision.response", FreshnessTTL: 15 * time.Minute,
		})
	must("ask.decide", err)
	split, err := authoring.NewParallelStep(header("fan.split", "fan", "entry.parse"),
		authoring.ParallelSpec{
			Children: []authoring.StepID{"fan.transport", "fan.slice"},
			Join:     authoring.JoinPolicy{JoinStep: "fan.join", Mode: authoring.JoinAll},
			Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
		})
	must("fan.split", err)
	slice, err := authoring.NewLocalStep(header("fan.slice", "fan", "fan.split"),
		ioWith("fan.split"), authoring.LocalSpec{Handler: "engine.fan.slice"})
	must("fan.slice", err)
	transport, err := authoring.NewHostActionStep(header("fan.transport", "fan", "fan.split"),
		ioWith("fan.split"), authoring.HostActionSpec{
			Handler: "engine.fan.transport", Boundary: authoring.BoundaryAgentDispatchAPI,
			Operation: "op.fan.transport", Schema: "schema.host.fan.transport", Timeout: 10 * time.Second,
		})
	must("fan.transport", err)
	join, err := authoring.NewLocalStep(header("fan.join", "fan", "fan.slice", "fan.transport"),
		ioWith("fan.slice", "fan.transport"), authoring.LocalSpec{Handler: "engine.fan.join"})
	must("fan.join", err)
	cost, err := authoring.NewLocalStep(header("report.cost", "report", "fan.join", "ask.decide"),
		ioWith("fan.join", "ask.decide"), authoring.LocalSpec{
			Handler: "engine.report.cost", Retry: &authoring.RetryPolicy{MaxAttempts: 2},
		})
	must("report.cost", err)
	return &compiler.Definition{Version: Version, EntryNode: entryNode,
		Steps: []authoring.Step{parse, persist, review, ask, split, slice, transport, join, cost}}
}

// must 把定义表/注册表的构造错误转为 panic：表内容是编译期常量，失败即
// 本包自身缺陷，不应在正常路径上传播。
func must(id any, err error) {
	if err != nil {
		panic(fmt.Sprintf("definition: %v: %v", id, err))
	}
}

func header(id, node string, deps ...string) authoring.Header {
	ds := make([]authoring.StepID, 0, len(deps))
	for _, d := range deps {
		ds = append(ds, authoring.StepID(d))
	}
	return authoring.Header{
		ID:                authoring.StepID(id),
		NodeID:            authoring.NodeID(node),
		Dependencies:      ds,
		DefinitionVersion: Version,
	}
}

// ioWith 构造合法共享 IO 段：每个依赖步骤恰好一个 typed input binding
// （inputs 集合 == dependencies 集合是 compiler 强制的不变量）。
func ioWith(bindings ...string) authoring.IO {
	inputs := make([]authoring.InputBinding, 0, len(bindings))
	for _, b := range bindings {
		inputs = append(inputs, authoring.InputBinding{
			From: authoring.StepID(b), OutputField: "out", ToField: "in",
		})
	}
	return authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out", Inputs: inputs}
}
