package definition

import "formal-gates/internal/engine/authoring"

// FlowNode names a selectable main-flow responsibility and its closed handlers.
// Dependencies and execution order remain owned by Workflow.
type FlowNode struct {
	ID          string                `json:"id"`
	Description string                `json:"description"`
	Handlers    []authoring.HandlerID `json:"handlers"`
}

func MainFlowNodes() []FlowNode {
	return []FlowNode{
		{ID: "product-review", Description: "产品审查", Handlers: []authoring.HandlerID{"engine.review.worker.v3"}},
		{ID: "start-readiness", Description: "开发前技术审查", Handlers: []authoring.HandlerID{"engine.readiness.plan.v6"}},
		{ID: "development", Description: "初始开发实现", Handlers: []authoring.HandlerID{"engine.development.plan.v1", "engine.development.batch.v6"}},
		{ID: "qa", Description: "已选 QA 模式的整组设计、独立审查与执行", Handlers: []authoring.HandlerID{"engine.qa.design.v6", "engine.qa.review.rationale.v2", "engine.qa.execution.v4"}},
		{ID: "gate", Description: "已选普通门与合并门的质量审查", Handlers: []authoring.HandlerID{"engine.gate.v3"}},
		{ID: "repair", Description: "已成立问题的业务返修", Handlers: []authoring.HandlerID{"engine.repair.v7", "engine.repair.verify.v2"}},
	}
}
