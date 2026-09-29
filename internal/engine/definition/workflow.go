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

// Version 是 definition 版本信封。内容为完整拓扑
// （definitions/workflow.json）；changed bytes 必须 bump。identity_gen.go
// 的 WorkflowDefinitionVersion 与其同源。
const Version authoring.DefinitionVersion = "6"

// entryNode 是定义的唯一入口节点（可达性校验的起点）。
const entryNode authoring.NodeID = "entry"

// Regular work is carried by explicit handlers; dynamic obligations belong to
// protocol State and are projected as RequiredTasks.
var (
	workflowHandlers = []struct {
		id     authoring.HandlerID
		runner authoring.RunnerKind
	}{
		{"engine.entry.parse", authoring.RunnerEngineLocal},
		{"engine.entry.persist", authoring.RunnerDurableActivity},
		{"engine.review.worker", authoring.RunnerAgentWorker},
		{"engine.readiness.plan", authoring.RunnerAgentWorker},

		{"engine.development.batch", authoring.RunnerAgentWorker},
		{"engine.qa.design", authoring.RunnerAgentWorker},
		{"engine.qa.review.rationale", authoring.RunnerAgentWorker},
		{"engine.qa.execution", authoring.RunnerAgentWorker},
		{"engine.gate", authoring.RunnerAgentWorker},
		{"engine.repair", authoring.RunnerAgentWorker},
		{"engine.candidate.review-range", authoring.RunnerHostAdapter},
	}
	workflowCodecs     = []authoring.CodecID{"codec.any.in", "codec.any.out"}
	workflowPredicates = []authoring.PredicateID{"pred.review.post"}
	workflowReconciles = []authoring.ReconcileID{"reconcile.entry.persist"}
	workflowSchemas    = []authoring.SchemaID{"schema.candidate.review-range"}
	workflowOperations = []authoring.OperationID{"op.candidate"}
	workflowAskKinds   = []authoring.AskKindID{}
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
	review, err := authoring.NewAgentStep(header("review.worker", "review", "entry.persist"),
		reviewIOWith("entry.persist"), authoring.AgentSpec{
			Handler: "engine.review.worker", Reason: authoring.ReasonIndependentReview,
			ReviewSeries: authoring.ReviewSeriesProductReview, Timeout: time.Minute,
		})
	must("review.worker", err)
	// Readiness includes the structured granularity proposal for route confirmation.
	readiness, err := authoring.NewAgentStep(header("review.readiness", "review", "review.worker"),
		reviewIOWith("review.worker"), authoring.AgentSpec{
			Handler: "engine.readiness.plan", Reason: authoring.ReasonIndependentReview,
			ReviewSeries: authoring.ReviewSeriesStartReadiness, Timeout: time.Minute,
		})
	must("review.readiness", err)
	steps := []authoring.Step{parse, persist, review, readiness}
	for _, spec := range []struct {
		id, handler string
		reason      authoring.NonProgrammableReason
	}{
		{"development.worker", "engine.development.batch", authoring.ReasonCreativeImplementation},
		{"qa.design", "engine.qa.design", authoring.ReasonSemanticJudgment},
		{"qa.review", "engine.qa.review.rationale", authoring.ReasonIndependentReview},
		{"qa.execution", "engine.qa.execution", authoring.ReasonSemanticJudgment},
		{"validation.gate", "engine.gate", authoring.ReasonIndependentReview},
		{"development.repair", "engine.repair", authoring.ReasonCreativeImplementation},
	} {
		payload := authoring.AgentSpec{Handler: authoring.HandlerID(spec.handler), Reason: spec.reason, Timeout: time.Minute, Retry: &authoring.RetryPolicy{MaxAttempts: 2}}
		if spec.reason == authoring.ReasonIndependentReview {
			payload.ReviewSeries = authoring.ReviewSeriesUnaffiliated
		}
		step, err := authoring.NewAgentStep(header(spec.id, "validation", "review.readiness"), reviewIOWith("review.readiness"), payload)
		must(spec.id, err)
		steps = append(steps, step)
	}
	candidate, err := authoring.NewHostActionStep(header("candidate.materialize", "validation", "review.readiness"), ioWith("review.readiness"), authoring.HostActionSpec{Handler: "engine.candidate.review-range", Boundary: authoring.BoundaryAgentDispatchAPI, Operation: "op.candidate", Schema: "schema.candidate.review-range", Timeout: time.Minute})
	must("candidate.materialize", err)
	steps = append(steps, candidate)
	return &compiler.Definition{Version: Version, EntryNode: entryNode, Steps: steps}
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

// reviewIOWith 在 ioWith 之上加两条审查 agent 步共用的后置谓词引用
// （agent 步必填的 worker result 合同）。
func reviewIOWith(bindings ...string) authoring.IO {
	review := ioWith(bindings...)
	review.Postconditions = []authoring.PredicateRef{{ID: "pred.review.post"}}
	return review
}
