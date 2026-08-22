package compiler

import (
	"formal-gates/internal/engine/authoring"
)

// 本文件承载 compiled IR 二次防线与 registry 完备解析。前者是 enforcement
// matrix 中 constructor 主拦项的 compiler 层复核（绕过 constructor 的零值/
// 非法枚举步骤在此拒绝），后者是锁步激活的编译期落实（ADR-001 决策 9）。

// validateStepIR 对单个 compiled 步骤做 payload 必填项与枚举合法性复核。
// 正常 authoring API 下这些非法状态不可构造（constructor 已拦）；本函数
// 拦截的是绕过 constructor 的直接结构体构造——"compiler 仍拒绝 nil、未知
// 变体与零值"（final-implementation-draft enforcement matrix）。
func validateStepIR(cs CompiledStep) error {
	h := cs.Header
	checkIO := func() error {
		if cs.IO.InputCodec == "" {
			return invariantError(`step %q: input codec id required (untyped IO)`, h.ID)
		}
		if cs.IO.OutputCodec == "" {
			return invariantError(`step %q: output codec id required (untyped IO)`, h.ID)
		}
		for _, ref := range cs.IO.Preconditions {
			if ref.ID == "" {
				return invariantError(`step %q: precondition predicate id required`, h.ID)
			}
		}
		for _, ref := range cs.IO.Postconditions {
			if ref.ID == "" {
				return invariantError(`step %q: postcondition predicate id required`, h.ID)
			}
		}
		return nil
	}
	checkRetryPtr := func(r *authoring.RetryPolicy) error {
		if r == nil {
			return nil
		}
		return checkRetryValue(h.ID, *r)
	}
	switch p := cs.Payload.(type) {
	case CompiledLocalStep:
		if err := checkIO(); err != nil {
			return err
		}
		if p.Handler == "" {
			return invariantError(`local step %q: handler id required`, h.ID)
		}
		if p.Timeout < 0 {
			return invariantError(`local step %q: timeout must be >= 0`, h.ID)
		}
		return checkRetryPtr(p.Retry)
	case CompiledDurableStep:
		if err := checkIO(); err != nil {
			return err
		}
		if p.Handler == "" {
			return invariantError(`durable step %q: handler id required`, h.ID)
		}
		if !p.Idempotency.Valid() {
			return invariantError(`durable step %q: idempotency key strategy required (DETERMINISTIC_INPUT|TASK_KEY_SCOPED)`, h.ID)
		}
		if p.Reconcile == "" {
			return invariantError(`durable step %q: reconcile id required`, h.ID)
		}
		if p.Timeout <= 0 {
			return invariantError(`durable step %q: positive timeout required`, h.ID)
		}
		return checkRetryValue(h.ID, p.Retry)
	case CompiledHostActionStep:
		if err := checkIO(); err != nil {
			return err
		}
		if p.Handler == "" {
			return invariantError(`host action step %q: handler id required`, h.ID)
		}
		if !p.Boundary.Valid() {
			return invariantError(`host action step %q: hostBoundaryReason required (EXTERNAL_CAPABILITY_BOUNDARY|USER_IO_TRANSPORT|AGENT_DISPATCH_API)`, h.ID)
		}
		if p.Operation == "" {
			return invariantError(`host action step %q: registered operation id required`, h.ID)
		}
		if p.Timeout <= 0 {
			return invariantError(`host action step %q: positive timeout required`, h.ID)
		}
		return nil
	case CompiledAgentStep:
		if err := checkIO(); err != nil {
			return err
		}
		if p.Handler == "" {
			return invariantError(`agent step %q: handler id required`, h.ID)
		}
		if !p.Reason.Valid() {
			return invariantError(`agent step %q: nonProgrammableReason required (SEMANTIC_JUDGMENT|CREATIVE_IMPLEMENTATION|INDEPENDENT_REVIEW)`, h.ID)
		}
		if p.Timeout <= 0 {
			return invariantError(`agent step %q: positive timeout required`, h.ID)
		}
		return checkRetryPtr(p.Retry)
	case CompiledHumanAskStep:
		if p.AskKind == "" {
			return invariantError(`human ask step %q: ask kind required`, h.ID)
		}
		if p.RequestSchema == "" {
			return invariantError(`human ask step %q: request schema id required`, h.ID)
		}
		if p.ResponseSchema == "" {
			return invariantError(`human ask step %q: response schema id required`, h.ID)
		}
		if p.FreshnessTTL <= 0 {
			return invariantError(`human ask step %q: positive freshness ttl required`, h.ID)
		}
		return nil
	case CompiledParallelStep:
		if len(p.Children) < 2 {
			return invariantError(`parallel step %q: at least 2 children required, got %d`, h.ID, len(p.Children))
		}
		if p.Join.JoinStep == "" || !p.Join.Mode.Valid() {
			return invariantError(`parallel step %q: join policy (joinStep + mode ALL|ANY) required`, h.ID)
		}
		if !p.Failure.Mode.Valid() || !p.Failure.Escalate.Valid() {
			return invariantError(`parallel step %q: failure policy (mode FAIL_FAST|WAIT_ALL + escalate class) required`, h.ID)
		}
		return nil
	default:
		return invariantError(`step %q: unknown payload variant %T`, h.ID, cs.Payload)
	}
}

func checkRetryValue(id authoring.StepID, r authoring.RetryPolicy) error {
	if r.MaxAttempts < 1 {
		return invariantError(`step %q: retry maxAttempts must be >= 1`, id)
	}
	if r.Backoff < 0 {
		return invariantError(`step %q: retry backoff must be >= 0`, id)
	}
	return nil
}

// checkInputsEqualDeps 强制四个可执行变体的 typed source bindings 集合与
// 依赖集合精确相等（spike 拍板的机械不变量）：删除依赖必被拒，且与
// submit 的 source bindings 校验同源（master-requirements §5.15）。
// human/parallel 无共享 IO 段，不适用。
func checkInputsEqualDeps(cs CompiledStep) error {
	switch cs.Payload.(type) {
	case CompiledHumanAskStep, CompiledParallelStep:
		return nil
	}
	deps := make(map[authoring.StepID]bool, len(cs.Header.Dependencies))
	for _, d := range cs.Header.Dependencies {
		deps[d] = true
	}
	frons := make(map[authoring.StepID]bool, len(cs.IO.Inputs))
	for _, b := range cs.IO.Inputs {
		if !deps[b.From] {
			return invariantError(`step %q: input binding source %q is not a dependency`, cs.Header.ID, b.From)
		}
		frons[b.From] = true
	}
	for d := range deps {
		if !frons[d] {
			return invariantError(`step %q: dependency %q has no typed input binding`, cs.Header.ID, d)
		}
	}
	return nil
}

// resolveCtx 承载 registry 解析阶段的模式路由：
//   - "未注册"（closed world not found）：diagnostic 模式记为
//     MISSING_ENGINE_ADAPTER 诊断并继续编译；正常模式路由为 BLOCKED_BUG；
//   - kind 不匹配与 handler runner 不匹配：ID 存在但被错用槽位/错绑执行
//     边界，是定义错误而非未实现，两种模式下都硬拒绝（INVARIANT_VIOLATION）。
type resolveCtx struct {
	reg        *Registry
	diagnostic bool
	diags      []Diagnostic
}

// resolveID 解析单个 registry ID 引用。诊断模式下未注册引用返回
// ("", nil)——调用方据此放行，marker 已记录。
func (c *resolveCtx) resolveID(step authoring.StepID, ref string, want EntryKind) (authoring.RunnerKind, error) {
	e, ok := c.reg.lookup(ref)
	if !ok {
		if c.diagnostic {
			c.diags = append(c.diags, Diagnostic{Step: step, Ref: ref, Want: want})
			return "", nil
		}
		return "", blockedBugError(`step %q: MISSING_ENGINE_ADAPTER: %s id %q not registered (closed world)`, step, want, ref)
	}
	if e.kind != want {
		return "", invariantError(`step %q: registry: id %q registered as %s, want %s`, step, ref, e.kind, want)
	}
	return e.runner, nil
}

// resolveStepRefs 解析单个步骤引用的全部 registry ID（完备性检查：每个
// 引用逐一存在、kind 匹配；handler 另做 runner 与变体派生 runner 的匹配）。
func (c *resolveCtx) resolveStepRefs(cs *CompiledStep) error {
	h := cs.Header
	checkIORefs := func() error {
		if _, err := c.resolveID(h.ID, string(cs.IO.InputCodec), KindCodec); err != nil {
			return err
		}
		if _, err := c.resolveID(h.ID, string(cs.IO.OutputCodec), KindCodec); err != nil {
			return err
		}
		for _, ref := range cs.IO.Preconditions {
			if _, err := c.resolveID(h.ID, string(ref.ID), KindPredicate); err != nil {
				return err
			}
		}
		for _, ref := range cs.IO.Postconditions {
			if _, err := c.resolveID(h.ID, string(ref.ID), KindPredicate); err != nil {
				return err
			}
		}
		return nil
	}
	checkHandler := func(id authoring.HandlerID) error {
		runner, err := c.resolveID(h.ID, string(id), KindHandler)
		if err != nil || runner == "" {
			return err
		}
		if runner != h.Runner {
			return invariantError(`step %q: handler %q runner %s != variant runner %s (kind mismatch)`, h.ID, id, runner, h.Runner)
		}
		return nil
	}
	switch p := cs.Payload.(type) {
	case CompiledLocalStep:
		if err := checkIORefs(); err != nil {
			return err
		}
		return checkHandler(p.Handler)
	case CompiledDurableStep:
		if err := checkIORefs(); err != nil {
			return err
		}
		if err := checkHandler(p.Handler); err != nil {
			return err
		}
		_, err := c.resolveID(h.ID, string(p.Reconcile), KindReconciler)
		return err
	case CompiledHostActionStep:
		if err := checkIORefs(); err != nil {
			return err
		}
		if err := checkHandler(p.Handler); err != nil {
			return err
		}
		_, err := c.resolveID(h.ID, string(p.Operation), KindOperation)
		return err
	case CompiledAgentStep:
		if err := checkIORefs(); err != nil {
			return err
		}
		return checkHandler(p.Handler)
	case CompiledHumanAskStep:
		if _, err := c.resolveID(h.ID, string(p.RequestSchema), KindSchema); err != nil {
			return err
		}
		_, err := c.resolveID(h.ID, string(p.ResponseSchema), KindSchema)
		return err
	case CompiledParallelStep:
		return nil
	default:
		return invariantError(`step %q: unknown payload variant %T`, h.ID, cs.Payload)
	}
}
