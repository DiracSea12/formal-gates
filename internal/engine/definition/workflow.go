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

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
)

// Version 是 definition 版本信封。内容为完整拓扑
// （definitions/workflow.json）；changed bytes 必须 bump。identity_gen.go
// 的 WorkflowDefinitionVersion 与其同源。
const Version authoring.DefinitionVersion = "28"

// entryNode 是定义的唯一入口节点（可达性校验的起点）。
const entryNode authoring.NodeID = "entry"

// Static work is carried by explicit handlers; dynamic obligations belong to
// protocol State and are projected as RequiredTasks.
var (
	workflowHandlers = []struct {
		id     authoring.HandlerID
		runner authoring.RunnerKind
	}{
		{"engine.entry.registered", authoring.RunnerEngineLocal},
		{"engine.children.prepare", authoring.RunnerEngineLocal},
		{"engine.children.publish", authoring.RunnerEngineLocal},
		{"engine.review.worker.v3", authoring.RunnerAgentWorker},
		{"engine.readiness.plan.v6", authoring.RunnerAgentWorker},
		{"engine.development.plan.v1", authoring.RunnerAgentWorker},

		{"engine.development.batch.v6", authoring.RunnerAgentWorker},
		{"engine.qa.design.v6", authoring.RunnerAgentWorker},
		{"engine.qa.review.rationale.v2", authoring.RunnerAgentWorker},
		{"engine.qa.execution.v4", authoring.RunnerAgentWorker},
		{"engine.validation.observation.v2", authoring.RunnerAgentWorker},
		{"engine.gate.v3", authoring.RunnerAgentWorker},
		{"engine.repair.v7", authoring.RunnerAgentWorker},
		{"engine.repair.verify.v2", authoring.RunnerAgentWorker},
		{"engine.merge.resolve.v3", authoring.RunnerAgentWorker},
		{"engine.candidate.review-range", authoring.RunnerHostAdapter},
		{"engine.workspace", authoring.RunnerHostAdapter},
		{"engine.cleanup", authoring.RunnerHostAdapter},
		{"engine.check", authoring.RunnerHostAdapter},
	}
	workflowCodecs     = []authoring.CodecID{"codec.any.in", "codec.any.out"}
	workflowPredicates = []authoring.PredicateID{"pred.review.post"}
	workflowSchemas    = []authoring.SchemaID{"schema.candidate.review-range", "schema.workspace", "schema.cleanup", "schema.check"}
	workflowOperations = []authoring.OperationID{"op.candidate", "op.workspace", "op.cleanup", "op.check"}
)

// Registry 返回注册了定义引用的全部封闭 ID 的 registry；条目不携带实现。
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
	for _, id := range workflowSchemas {
		must(id, reg.RegisterSchema(id))
	}
	for _, id := range workflowOperations {
		must(id, reg.RegisterOperation(id))
	}
	return reg
}

// Workflow 经 authoring constructor 构造定义表。每次调用产生全新实例；
// 表内容是编译期常量，constructor 失败只可能是本表自身缺陷，直接 panic。
func Workflow() *compiler.Definition {
	registered, err := authoring.NewLocalStep(header("entry.registered", "entry"), typedIO(),
		authoring.LocalSpec{Handler: "engine.entry.registered"})
	must("entry.registered", err)
	review, err := authoring.NewAgentStep(header("review.worker", "review", "entry.registered"),
		reviewIO(), authoring.AgentSpec{
			Handler: "engine.review.worker.v3", Reason: authoring.ReasonIndependentReview,
			ReviewSeries: authoring.ReviewSeriesProductReview, Timeout: 0,
		})
	must("review.worker", err)
	// Readiness includes the structured granularity proposal for route confirmation.
	readiness, err := authoring.NewAgentStep(header("review.readiness", "review", "review.worker"),
		reviewIO(), authoring.AgentSpec{
			Handler: "engine.readiness.plan.v6", Reason: authoring.ReasonIndependentReview,
			ReviewSeries: authoring.ReviewSeriesStartReadiness, Timeout: 0,
		})
	must("review.readiness", err)
	plan, err := authoring.NewAgentStep(header("development.plan", "development", "review.readiness"), reviewIO(), authoring.AgentSpec{
		Handler: "engine.development.plan.v1", Reason: authoring.ReasonCreativeImplementation,
		Timeout: 0, Retry: &authoring.RetryPolicy{MaxAttempts: 2},
	})
	must("development.plan", err)
	prepare, err := authoring.NewLocalStep(header("children.prepare", "children", "development.plan"), typedIO(), authoring.LocalSpec{Handler: "engine.children.prepare"})
	must("children.prepare", err)
	workspace, err := authoring.NewHostActionStep(header("children.workspace", "children", "children.prepare"), typedIO(), authoring.HostActionSpec{Handler: "engine.workspace", Boundary: authoring.BoundaryExternalCapability, Operation: "op.workspace", Schema: "schema.workspace", Timeout: 0})
	must("children.workspace", err)
	publish, err := authoring.NewLocalStep(header("children.publish", "children", "children.workspace"), typedIO(), authoring.LocalSpec{Handler: "engine.children.publish"})
	must("children.publish", err)
	steps := []authoring.Step{registered, review, readiness, plan, prepare, workspace, publish}
	for _, spec := range []struct {
		id, handler string
		reason      authoring.NonProgrammableReason
	}{
		{"development.worker", "engine.development.batch.v6", authoring.ReasonCreativeImplementation},
		{"qa.design", "engine.qa.design.v6", authoring.ReasonSemanticJudgment},
		{"qa.review", "engine.qa.review.rationale.v2", authoring.ReasonIndependentReview},
		{"qa.execution", "engine.qa.execution.v4", authoring.ReasonSemanticJudgment},
		{"validation.observation", "engine.validation.observation.v2", authoring.ReasonSemanticJudgment},
		{"validation.gate", "engine.gate.v3", authoring.ReasonIndependentReview},
		{"development.repair", "engine.repair.v7", authoring.ReasonCreativeImplementation},
		{"repair.verification", "engine.repair.verify.v2", authoring.ReasonSemanticJudgment},
		{"merge.resolve", "engine.merge.resolve.v3", authoring.ReasonCreativeImplementation},
	} {
		payload := authoring.AgentSpec{Handler: authoring.HandlerID(spec.handler), Reason: spec.reason, Timeout: 0, Retry: &authoring.RetryPolicy{MaxAttempts: 2}}
		if spec.reason == authoring.ReasonIndependentReview {
			payload.ReviewSeries = authoring.ReviewSeriesUnaffiliated
		}
		step, err := authoring.NewAgentStep(header(spec.id, "validation", "development.plan"), reviewIO(), payload)
		must(spec.id, err)
		steps = append(steps, step)
	}
	candidate, err := authoring.NewHostActionStep(header("candidate.materialize", "validation", "development.plan"), typedIO(), authoring.HostActionSpec{Handler: "engine.candidate.review-range", Boundary: authoring.BoundaryExternalCapability, Operation: "op.candidate", Schema: "schema.candidate.review-range", Timeout: 0})
	must("candidate.materialize", err)
	steps = append(steps, candidate)
	for _, name := range []string{"workspace", "cleanup"} {
		step, err := authoring.NewHostActionStep(header(name+".materialize", "validation", "development.plan"), typedIO(), authoring.HostActionSpec{Handler: authoring.HandlerID("engine." + name), Boundary: authoring.BoundaryExternalCapability, Operation: authoring.OperationID("op." + name), Schema: authoring.SchemaID("schema." + name), Timeout: 0})
		must(name, err)
		steps = append(steps, step)
	}
	check, err := authoring.NewHostActionStep(header("check.execute", "validation", "development.plan"), typedIO(), authoring.HostActionSpec{Handler: "engine.check", Boundary: authoring.BoundaryExternalCapability, Operation: "op.check", Schema: "schema.check", Timeout: 0})
	must("check.execute", err)
	steps = append(steps, check)
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

// typedIO 构造共享输入输出 codec 引用。
func typedIO() authoring.IO {
	return authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out"}
}

// reviewIO 构造 agent 步的 codec 与 worker result 后置谓词引用。
func reviewIO() authoring.IO {
	review := typedIO()
	review.Postconditions = []authoring.PredicateRef{{ID: "pred.review.post"}}
	return review
}
