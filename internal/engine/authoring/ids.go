package authoring

// 稳定 ID 与标识类型。
//
// 所有 ID 均为独立命名类型：不同用途的 ID 之间、以及 ID 与裸 string 类型值
// 之间都不可直接赋值，跨类型必须显式转换。
// registry ID 的解析（存在性、唯一性、kind 匹配）属于后续 compiler 批次；
// 本层只保证类型隔离与非空。
//
// 命名建议沿用 spike 结论：domain.family.name（如 engine.persist.intent、
// reconcile.intent.persist、schema.ask.decision.request）。
type (
	// StepID 是步骤在单个 definition 内的稳定标识。
	StepID string
	// NodeID 是所属用户节点的稳定标识。
	NodeID string
	// DefinitionVersion 是定义版本信封的版本字符串（八类拒绝中
	// "未绑定 definition version" 的第一拦截层：constructor 拒绝空版本）。
	DefinitionVersion string

	// HandlerID 标识封闭 registry 中的可恢复执行合同（非每一版实现）。
	HandlerID string
	// PredicateID 标识封闭 registry 中的可执行 pre/postcondition predicate。
	// 定义中不存在自然语言 condition 字段。
	PredicateID string
	// CodecID 标识封闭 registry 中的 typed input/output codec。
	CodecID string
	// ReconcileID 标识封闭 registry 中的副作用 reconciler。
	ReconcileID string
	// SchemaID 标识封闭 registry 中的 typed schema（如 human ask 的
	// request/response schema）。
	SchemaID string
	// OperationID 标识定义中注册的 host adapter operation。它不是自由 shell
	// 通道（master-requirements §5.12），因此与 HandlerID 等其余 registry ID
	// 一样做成独立命名类型，防止跨槽位误用。
	OperationID string
)
