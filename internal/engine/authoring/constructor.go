package authoring

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Constructor 是唯一的合法构造入口：全部必填项强制校验，非法组合返回 error
// （Go 无编译器穷举 sum type，这是可达的最强拦截层）。
//
// obligations（按变体）：
//   - 公共头：ID/NodeID/DefinitionVersion 非空（"未绑定 definition version"
//     的第一拦截层）；依赖去重排序、拒绝空 ID 与自引用；
//   - 可执行变体（local/durable/host/agent）：typed input/output codec 必填
//     （"无类型 I/O"的第一拦截层）；predicate 引用非空；
//   - DurableStep：幂等策略枚举 + ReconcileID + retry/timeout 必填；
//   - HostActionStep：三值封闭边界枚举 + operation 引用 + timeout 必填；
//   - AgentStep：三值封闭理由枚举 + postcondition predicate 引用 + timeout 必填；
//   - HumanAskStep：ask 类型 + request/response schema + freshness TTL 必填；
//   - ParallelStep：join/failure 策略 + >= 2 个 children 必填。

// checkHeader 校验公共头必填项与字符约束：ID/NodeID 不得含 "/"——canonical
// task key 以 "/" 连接 node/step/scope 三段，段内分隔符会使不同键坍缩为同一
// 字符串形态（{n,a/b} 与 {n/a,b} 同为 n/a/b）。
func checkHeader(h Header) error {
	if h.ID == "" {
		return errors.New("step id is empty")
	}
	if strings.Contains(string(h.ID), "/") {
		return fmt.Errorf("step %q: step id must not contain \"/\" (canonical task keys join node/step/scope with \"/\")", h.ID)
	}
	if h.NodeID == "" {
		return fmt.Errorf("step %q: node id is empty", h.ID)
	}
	if strings.Contains(string(h.NodeID), "/") {
		return fmt.Errorf("step %q: node id %q must not contain \"/\" (canonical task keys join node/step/scope with \"/\")", h.ID, h.NodeID)
	}
	if h.DefinitionVersion == "" {
		return fmt.Errorf("step %q: definition version is empty", h.ID)
	}
	return nil
}

// normalizeIDs 去重、排序并拒绝空 ID 与自引用。返回新切片，不改写调用方入参；
// 编写顺序不得泄漏进制品。
func normalizeIDs(id StepID, what string, in []StepID) ([]StepID, error) {
	seen := make(map[StepID]bool, len(in))
	out := make([]StepID, 0, len(in))
	for _, d := range in {
		if d == "" {
			return nil, fmt.Errorf("step %q: empty %s id", id, what)
		}
		if d == id {
			return nil, fmt.Errorf("step %q: %s references the step itself", id, what)
		}
		if seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// checkIO 校验共享 IO 段：typed codec 与 predicate 引用必填。
func checkIO(id StepID, io IO) error {
	if io.InputCodec == "" {
		return fmt.Errorf("step %q: input codec id required", id)
	}
	if io.OutputCodec == "" {
		return fmt.Errorf("step %q: output codec id required", id)
	}
	for _, ref := range io.Preconditions {
		if ref.ID == "" {
			return fmt.Errorf("step %q: precondition predicate id required", id)
		}
	}
	for _, ref := range io.Postconditions {
		if ref.ID == "" {
			return fmt.Errorf("step %q: postcondition predicate id required", id)
		}
	}
	for _, b := range io.Inputs {
		if b.From == "" {
			return fmt.Errorf("step %q: input binding source step required", id)
		}
	}
	return nil
}

// checkOptionalRetry 校验可选重试策略指针。
func checkOptionalRetry(id StepID, r *RetryPolicy) error {
	if r == nil {
		return nil
	}
	return checkRetryValue(id, *r)
}

// checkRetryValue 校验必填重试策略值。
func checkRetryValue(id StepID, r RetryPolicy) error {
	if r.MaxAttempts < 1 {
		return fmt.Errorf("step %q: retry maxAttempts must be >= 1", id)
	}
	if r.Backoff < 0 {
		return fmt.Errorf("step %q: retry backoff must be >= 0", id)
	}
	return nil
}

func headerWith(h Header, deps []StepID) Header {
	return Header{ID: h.ID, NodeID: h.NodeID, Dependencies: deps, DefinitionVersion: h.DefinitionVersion}
}

// LocalSpec 是 NewLocalStep 的 payload 必填/可选项。
type LocalSpec struct {
	Handler HandlerID
	Timeout time.Duration // 可选；0 表示不设超时，负值非法
	Retry   *RetryPolicy  // 可选
}

// NewLocalStep 构造 engine 本地变换步骤（派生 ENGINE/ENGINE_LOCAL）。
func NewLocalStep(h Header, io IO, spec LocalSpec) (LocalStep, error) {
	if err := checkHeader(h); err != nil {
		return LocalStep{}, err
	}
	if err := checkIO(h.ID, io); err != nil {
		return LocalStep{}, err
	}
	if spec.Handler == "" {
		return LocalStep{}, fmt.Errorf("local step %q: handler id required", h.ID)
	}
	if err := checkOptionalRetry(h.ID, spec.Retry); err != nil {
		return LocalStep{}, err
	}
	if spec.Timeout < 0 {
		return LocalStep{}, fmt.Errorf("local step %q: timeout must be >= 0", h.ID)
	}
	deps, err := normalizeIDs(h.ID, "dependency", h.Dependencies)
	if err != nil {
		return LocalStep{}, err
	}
	return LocalStep{Header: headerWith(h, deps), IO: io,
		Handler: spec.Handler, Timeout: spec.Timeout, Retry: spec.Retry}, nil
}

// DurableSpec 是 NewDurableStep 的 payload 必填项；幂等策略、reconcile、
// retry/timeout 全部必填。
type DurableSpec struct {
	Handler     HandlerID
	Idempotency IdempotencyKeyStrategy
	Reconcile   ReconcileID
	Timeout     time.Duration // 必填 > 0
	Retry       RetryPolicy   // 必填；MaxAttempts >= 1
}

// NewDurableStep 构造持久化副作用步骤（派生 ENGINE/DURABLE_ACTIVITY）。
// 副作用无幂等/reconcile 的定义在此被拒（八类拒绝之一的第一拦截层）。
func NewDurableStep(h Header, io IO, spec DurableSpec) (DurableStep, error) {
	if err := checkHeader(h); err != nil {
		return DurableStep{}, err
	}
	if err := checkIO(h.ID, io); err != nil {
		return DurableStep{}, err
	}
	if spec.Handler == "" {
		return DurableStep{}, fmt.Errorf("durable step %q: handler id required", h.ID)
	}
	if !spec.Idempotency.Valid() {
		return DurableStep{}, fmt.Errorf("durable step %q: idempotency key strategy required (DETERMINISTIC_INPUT|TASK_KEY_SCOPED)", h.ID)
	}
	if spec.Reconcile == "" {
		return DurableStep{}, fmt.Errorf("durable step %q: reconcile id required", h.ID)
	}
	if err := checkRetryValue(h.ID, spec.Retry); err != nil {
		return DurableStep{}, err
	}
	if spec.Timeout <= 0 {
		return DurableStep{}, fmt.Errorf("durable step %q: positive timeout required", h.ID)
	}
	deps, err := normalizeIDs(h.ID, "dependency", h.Dependencies)
	if err != nil {
		return DurableStep{}, err
	}
	return DurableStep{Header: headerWith(h, deps), IO: io,
		Handler: spec.Handler, Idempotency: spec.Idempotency, Reconcile: spec.Reconcile,
		Timeout: spec.Timeout, Retry: spec.Retry}, nil
}

// HostActionSpec 是 NewHostActionStep 的 payload 必填项。
type HostActionSpec struct {
	Handler   HandlerID
	Boundary  HostBoundaryReason
	Operation OperationID
	Timeout   time.Duration // 必填 > 0
}

// NewHostActionStep 构造宿主 adapter 动作步骤（派生 ENGINE/HOST_ADAPTER）。
// 缺合法 hostBoundaryReason 的 HOST 步骤在此被拒（八类拒绝之一的第一拦截层）。
func NewHostActionStep(h Header, io IO, spec HostActionSpec) (HostActionStep, error) {
	if err := checkHeader(h); err != nil {
		return HostActionStep{}, err
	}
	if err := checkIO(h.ID, io); err != nil {
		return HostActionStep{}, err
	}
	if spec.Handler == "" {
		return HostActionStep{}, fmt.Errorf("host action step %q: handler id required", h.ID)
	}
	if !spec.Boundary.Valid() {
		return HostActionStep{}, fmt.Errorf("host action step %q: hostBoundaryReason required (EXTERNAL_CAPABILITY_BOUNDARY|USER_IO_TRANSPORT|AGENT_DISPATCH_API)", h.ID)
	}
	if spec.Operation == "" {
		return HostActionStep{}, fmt.Errorf("host action step %q: registered operation id required", h.ID)
	}
	if spec.Timeout <= 0 {
		return HostActionStep{}, fmt.Errorf("host action step %q: positive timeout required", h.ID)
	}
	deps, err := normalizeIDs(h.ID, "dependency", h.Dependencies)
	if err != nil {
		return HostActionStep{}, err
	}
	return HostActionStep{Header: headerWith(h, deps), IO: io,
		Handler: spec.Handler, Boundary: spec.Boundary, Operation: spec.Operation,
		Timeout: spec.Timeout}, nil
}

// AgentSpec 是 NewAgentStep 的 payload 必填/可选项。postcondition predicate
// 引用由 IO.Postconditions 承载且至少一条（worker result 合同，§5.12）。
type AgentSpec struct {
	Handler HandlerID
	Reason  NonProgrammableReason
	Timeout time.Duration // 必填 > 0
	Retry   *RetryPolicy  // 可选
}

// NewAgentStep 构造不可程序化语义工作步骤（派生 AGENT/AGENT_WORKER）。
// 缺合法 nonProgrammableReason 的 AGENT 步骤在此被拒（八类拒绝之一的第一拦截层）。
func NewAgentStep(h Header, io IO, spec AgentSpec) (AgentStep, error) {
	if err := checkHeader(h); err != nil {
		return AgentStep{}, err
	}
	if err := checkIO(h.ID, io); err != nil {
		return AgentStep{}, err
	}
	if spec.Handler == "" {
		return AgentStep{}, fmt.Errorf("agent step %q: handler id required", h.ID)
	}
	if !spec.Reason.Valid() {
		return AgentStep{}, fmt.Errorf("agent step %q: nonProgrammableReason required (SEMANTIC_JUDGMENT|CREATIVE_IMPLEMENTATION|INDEPENDENT_REVIEW)", h.ID)
	}
	if len(io.Postconditions) == 0 {
		return AgentStep{}, fmt.Errorf("agent step %q: postcondition predicate reference required (worker result contract)", h.ID)
	}
	if err := checkOptionalRetry(h.ID, spec.Retry); err != nil {
		return AgentStep{}, err
	}
	if spec.Timeout <= 0 {
		return AgentStep{}, fmt.Errorf("agent step %q: positive timeout required", h.ID)
	}
	deps, err := normalizeIDs(h.ID, "dependency", h.Dependencies)
	if err != nil {
		return AgentStep{}, err
	}
	return AgentStep{Header: headerWith(h, deps), IO: io,
		Handler: spec.Handler, Reason: spec.Reason, Timeout: spec.Timeout, Retry: spec.Retry}, nil
}

// HumanAskSpec 是 NewHumanAskStep 的 payload 必填项。
type HumanAskSpec struct {
	AskKind        string
	RequestSchema  SchemaID
	ResponseSchema SchemaID
	FreshnessTTL   time.Duration // 必填 > 0
}

// NewHumanAskStep 构造用户 Ask 等待步骤（派生 HUMAN/HOST_ADAPTER）。
// 无 request/schema 的人工等待在此被拒（八类拒绝之一的第一拦截层）。
// 无 IO 参数：human 的 typed I/O 就是 payload 内的 schema，结构上不存在
// codec/retry/timeout 字段。
func NewHumanAskStep(h Header, spec HumanAskSpec) (HumanAskStep, error) {
	if err := checkHeader(h); err != nil {
		return HumanAskStep{}, err
	}
	if spec.AskKind == "" {
		return HumanAskStep{}, fmt.Errorf("human ask step %q: ask kind required", h.ID)
	}
	if spec.RequestSchema == "" {
		return HumanAskStep{}, fmt.Errorf("human ask step %q: request schema id required", h.ID)
	}
	if spec.ResponseSchema == "" {
		return HumanAskStep{}, fmt.Errorf("human ask step %q: response schema id required", h.ID)
	}
	if spec.FreshnessTTL <= 0 {
		return HumanAskStep{}, fmt.Errorf("human ask step %q: positive freshness ttl required", h.ID)
	}
	deps, err := normalizeIDs(h.ID, "dependency", h.Dependencies)
	if err != nil {
		return HumanAskStep{}, err
	}
	return HumanAskStep{Header: headerWith(h, deps),
		AskKind: spec.AskKind, RequestSchema: spec.RequestSchema,
		ResponseSchema: spec.ResponseSchema, FreshnessTTL: spec.FreshnessTTL}, nil
}

// ParallelSpec 是 NewParallelStep 的 payload 必填项。
type ParallelSpec struct {
	Children []StepID
	Join     JoinPolicy
	Failure  FailurePolicy
}

// NewParallelStep 构造 fan-out/fan-in 控制步（派生 ENGINE/ENGINE_LOCAL，
// 无 handler）。无 join/failure policy 的并行组与 children < 2 在此被拒
// （八类拒绝之一的第一拦截层）。children 去重后仍需 >= 2：单成员"并行"是
// 退化组。join 步骤必须是分立步骤，不得同时是 children 成员。
func NewParallelStep(h Header, spec ParallelSpec) (ParallelStep, error) {
	if err := checkHeader(h); err != nil {
		return ParallelStep{}, err
	}
	children, err := normalizeIDs(h.ID, "child", spec.Children)
	if err != nil {
		return ParallelStep{}, err
	}
	if len(children) < 2 {
		return ParallelStep{}, fmt.Errorf("parallel step %q: at least 2 children required after dedup, got %d", h.ID, len(children))
	}
	if spec.Join.JoinStep == "" {
		return ParallelStep{}, fmt.Errorf("parallel step %q: join step id required", h.ID)
	}
	// join 必须是组外分立步骤：join 步 == 并行步自身时 join 依赖集合与
	// children 自指重合，会在 compiler 层绕过 fan-out 覆盖检查（封板后审计
	// H1），在构造层直接拒绝。
	if spec.Join.JoinStep == h.ID {
		return ParallelStep{}, fmt.Errorf("parallel step %q: join step %q must be outside the parallel group (join step is the parallel step itself)", h.ID, spec.Join.JoinStep)
	}
	if !spec.Join.Mode.Valid() {
		return ParallelStep{}, fmt.Errorf("parallel step %q: join mode required (ALL|ANY)", h.ID)
	}
	if !spec.Failure.Mode.Valid() {
		return ParallelStep{}, fmt.Errorf("parallel step %q: failure mode required (FAIL_FAST|WAIT_ALL)", h.ID)
	}
	if !spec.Failure.Escalate.Valid() {
		return ParallelStep{}, fmt.Errorf("parallel step %q: failure escalate class required", h.ID)
	}
	for _, c := range children {
		if c == spec.Join.JoinStep {
			return ParallelStep{}, fmt.Errorf("parallel step %q: join step %q must not be a child", h.ID, c)
		}
	}
	deps, err := normalizeIDs(h.ID, "dependency", h.Dependencies)
	if err != nil {
		return ParallelStep{}, err
	}
	return ParallelStep{Header: headerWith(h, deps),
		Children: children, Join: spec.Join, Failure: spec.Failure}, nil
}
