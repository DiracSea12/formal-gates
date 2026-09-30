package protocol

import (
	"fmt"
	"sort"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/guard"
	"formal-gates/internal/engine/runtime"
)

// GuardRuntimeIntegration 是 guard 运行时集成状态的权威值：本引擎的签发漏斗
// （issueInto）在同一提交事务内执行 guard 判断，达到额度零签发、账目不一致以
// BLOCKED_BUG 终止签发。公开只读预览（workflow guard-preview）读该常量报告权威
// 状态，不自行持第二份。
const GuardRuntimeIntegration = "ENGINE_AUTHORITATIVE"

// GuardBudget 是从权威事实派生的额度投影：guard.Calculate 的额度数字、basis
// digest 与已用事实。事实源是签发/结果账本本身；本投影只在提交点重derive写入，
// 加载边界重算比对（不一致＝BLOCKED_BUG）。
type GuardBudget struct {
	RequestAllowance  int    `json:"requestAllowance"`
	RetryAllowance    int    `json:"retryAllowance"`
	TotalAllowance    int    `json:"totalAllowance"`
	BudgetBasisDigest string `json:"budgetBasisDigest"`
	UsedRequests      int    `json:"usedRequests"`
	UsedRetries       int    `json:"usedRetries"`
}

// guardEngaged 报告 guard 包络是否作用于当前状态：验证管线就位（拓扑、路
// 线、确认计划与需求文档都在账）即生效，与是否已有签发事实无关。需求重走
// 解绑拓扑/路线的再确认窗口内包络不生效（basis 身份暂缺；该窗口内验证任
// 务本就不可投影签发，不构成扩容通道）。受理前的审查系列派发不进包络
// （plan:361「QA/开发/门的外部派发统一经过 guard」；系列轮次的权威是
// series 账本）。
func (e *Engine) guardEngaged(state *State) bool {
	if state.Validation == nil || state.Topology == nil || state.RouteMode == "" {
		return false
	}
	_, plan, _ := state.confirmedDevelopmentPlan()
	return plan != nil
}

// guardBasis 是 Basis 构造单点：从权威 State 构造 guard.Basis（guard 纯核心不
// 重写不绕过）。来源按合同钉死：definition＝编译定义身份＋closed-world agent
// steps；requirement＝确认账本末条；Batch＝确认计划 contract digest；route/
// topology＝绑定 digest；approvedQA＝各选中 mode 的 case-set 版本；selectedGates
// ＝路线＋所选门；established obligations/repairs 只接 adjudication 完成、wave
// 收齐后的有效义务与返修 TaskKey；externalTasks＝已签发外部派发现存事实。
// raw finding、普通 result/receipt/resume 不是这些账本的成员，机械不可达。
func (e *Engine) guardBasis(state *State) (guard.Basis, error) {
	definitionDigest := func() string {
		data, err := encoder.Encode(e.cfg.Definition)
		if err != nil {
			return ""
		}
		return encoder.Digest(data)
	}()
	requirement := state.RequirementRevision()
	requirementID := "engine-run-requirement"
	if state.IntakeConfirmationReceipt != nil && strings.TrimSpace(state.IntakeConfirmationReceipt.RequirementSource) != "" {
		requirementID = state.IntakeConfirmationReceipt.RequirementSource
	}
	if state.Topology == nil || state.RouteMode == "" {
		return guard.Basis{}, fmt.Errorf("guard budget: topology and route bindings are required before the dispatch budget applies")
	}
	_, plan, _ := state.confirmedDevelopmentPlan()
	if plan == nil {
		return guard.Basis{}, fmt.Errorf("guard budget: no confirmed development plan backs the dispatch budget")
	}
	routeDigest := state.validationRouteDigest()
	basis := guard.Basis{
		SchemaVersion: 1,
		Requirement:   guard.ConfirmedIdentity{Identity: guard.Identity{ID: requirementID, Revision: requirement}, ConfirmedRevision: requirement},
		Batch:         guard.ConfirmedIdentity{Identity: guard.Identity{ID: "development-plan", Revision: planContractDigest(plan)}, ConfirmedRevision: planContractDigest(plan)},
		Route:         guard.ConfirmedIdentity{Identity: guard.Identity{ID: state.RouteMode, Revision: routeDigest}, ConfirmedRevision: routeDigest},
		Topology:      guard.ConfirmedIdentity{Identity: guard.Identity{ID: state.Topology.Decision, Revision: digestOfCanonical(state.Topology)}, ConfirmedRevision: digestOfCanonical(state.Topology)},
		Definition: guard.Definition{
			Identity:      guard.Identity{ID: definitionDigest, Revision: string(e.cfg.Definition.Version)},
			ExternalSteps: externalStepsOf(e.cfg.Definition),
		},
	}
	if state.Validation != nil {
		for _, mode := range state.validationKinds() {
			if set := state.Validation.Sets[mode]; set != nil {
				basis.ApprovedQA = append(basis.ApprovedQA, guard.Identity{ID: string(mode), Revision: set.Version})
			}
		}
	}
	for _, gate := range state.SelectedGates {
		basis.SelectedGates = append(basis.SelectedGates, guard.Identity{ID: gate, Revision: routeDigest})
	}
	if state.Validation != nil {
		// 同一有效义务/返修 TaskKey 可能跨多个 wave 出现（继承/重开）：以
		// 首个建立它的 wave 为其身份 revision（wave 追加有序，身份稳定），
		// 不因后续 wave 的 expected digest 变化产生冲突声明。
		obligations := map[string]guard.Identity{}
		repairs := map[string]guard.Identity{}
		for i := range state.Validation.Waves {
			w := &state.Validation.Waves[i]
			if w.Status == "COLLECTING" || !state.validationFindingsAdjudicated(w) {
				continue
			}
			for _, id := range state.validationObligations(w) {
				if _, exists := obligations[id]; !exists {
					obligations[id] = guard.Identity{ID: id, Revision: w.ExpectedDigest}
				}
			}
			if w.Repair != nil {
				if _, exists := repairs[w.Repair.Task.String()]; !exists {
					repairs[w.Repair.Task.String()] = guard.Identity{ID: w.Repair.Task.String(), Revision: w.ExpectedDigest}
				}
				// 需求重走替换后的返修 TaskKey 与原任务同源声明（其派发
				// provenance 需要可匹配的声明来源）。
				if current := state.replacementFold(w.Repair.Task); current != w.Repair.Task {
					if _, exists := repairs[current.String()]; !exists {
						repairs[current.String()] = guard.Identity{ID: current.String(), Revision: w.ExpectedDigest}
					}
				}
			}
		}
		// 预授权 repair 机械加入：repairWork 是 wave 收齐、裁决定案后仍欠执行的
		// 返修义务（含尚未签发的 TaskKey），不需另行确认即占基础槽位。
		for _, work := range state.repairWork() {
			if work.wave.Repair != nil && work.task == work.wave.Repair.Task {
				if _, exists := repairs[work.task.String()]; !exists {
					repairs[work.task.String()] = guard.Identity{ID: work.task.String(), Revision: work.wave.ExpectedDigest}
				}
			}
		}
		for _, id := range sortedIdentityIDs(obligations) {
			basis.EstablishedObligations = append(basis.EstablishedObligations, obligations[id])
		}
		for _, id := range sortedIdentityIDs(repairs) {
			basis.EstablishedRepairs = append(basis.EstablishedRepairs, repairs[id])
		}
	}
	issued := issuedGuardTasks(state, e.cfg.Definition)
	for task := range issued {
		provenance, maxAttempts, ok := e.guardTaskProvenance(state, task)
		if !ok {
			return guard.Basis{}, fmt.Errorf("guard budget: issued dispatch %s has no authorizing source", task.String())
		}
		attempts := maxAttempts
		basis.ExternalTasks = append(basis.ExternalTasks, guard.ExternalTask{TaskKey: task, Provenance: provenance, MaxAttempts: &attempts})
	}
	// 预授权 repair 机械加入（AC-059）：已声明未签发的返修槽位属于授权全集
	// ——与已签发事实同一 provenance/策略声明，RequestAllowance 反映授权全集
	// 而非仅已占用集。
	for _, work := range state.repairWork() {
		if work.wave.Repair == nil || work.task != work.wave.Repair.Task || issued[work.task] {
			continue
		}
		provenance, maxAttempts, ok := e.guardTaskProvenance(state, work.task)
		if !ok {
			continue
		}
		attempts := maxAttempts
		basis.ExternalTasks = append(basis.ExternalTasks, guard.ExternalTask{TaskKey: work.task, Provenance: provenance, MaxAttempts: &attempts})
	}
	return basis, nil
}

// externalStepsOf 列出 closed-world 外部步：可经 Ready 漏斗签发的全部 agent 步
// （host 步走 KindHostAction 边界，不经 issueInto）。动态 scope 复用这些步；
// 未声明步不产生槽位。
func externalStepsOf(cd *compiler.CompiledDefinition) []runtime.TaskKey {
	steps := make([]runtime.TaskKey, 0, len(cd.Steps))
	for _, step := range cd.Steps {
		if _, ok := step.Payload.(compiler.CompiledAgentStep); ok {
			key := runtime.TaskKey{Node: step.Header.NodeID, Step: step.Header.ID}
			if !containsTaskKey(steps, key) {
				steps = append(steps, key)
			}
		}
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].String() < steps[j].String() })
	return steps
}

func containsTaskKey(keys []runtime.TaskKey, key runtime.TaskKey) bool {
	for _, existing := range keys {
		if existing == key {
			return true
		}
	}
	return false
}

// taskScopeMode 解析派发 scope 中的 mode 段（generation:mode:...）。解析
// 不出 mode 时返回零值（调用方按无 set 处理）。
func taskScopeMode(task runtime.TaskKey) coverage.ReviewKind {
	parts := strings.SplitN(task.Scope, ":", 3)
	if len(parts) < 2 {
		return ""
	}
	return coverage.ReviewKind(parts[1])
}

// sortedIdentityIDs 返回身份表的确定性 ID 序（basis 构造对顺序不敏感，这里
// 只为稳定输出）。
func sortedIdentityIDs(identities map[string]guard.Identity) []string {
	ids := make([]string, 0, len(identities))
	for id := range identities {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// issuedGuardTasks 枚举已签发外部派发的现存事实中属 guard 包络（validation
// role）的任务：DispatchBindings/Attempts/EndedAttempts/PendingActions/
// ObsoleteActions/SpawnReceipts 六处账本去重。
func issuedGuardTasks(state *State, cd *compiler.CompiledDefinition) map[runtime.TaskKey]bool {
	tasks := map[runtime.TaskKey]bool{}
	collect := func(key runtime.TaskKey) {
		if key.Valid() && state.validationRole(key, cd) != "" {
			tasks[key] = true
		}
	}
	for _, binding := range state.DispatchBindings {
		collect(binding.Task)
	}
	for task := range state.Attempts {
		collect(task)
	}
	for task := range state.EndedAttempts {
		collect(task)
	}
	for _, pending := range state.PendingActions {
		collect(pending.Task)
	}
	for _, obsolete := range state.ObsoleteActions {
		collect(obsolete.Task)
	}
	for _, receipt := range state.SpawnReceipts {
		collect(receipt.Task)
	}
	return tasks
}

// guardTaskProvenance 按任务角色归类 provenance（合同：「provenance 按上述
// source 归类」）并给出该 TaskKey 的现行有限 retry 策略。策略读 attempt 账本
// （一次性用户追加只改该账本的 MaxAttempts，AC-059），无账本条目时用编译步策略。
func (e *Engine) guardTaskProvenance(state *State, task runtime.TaskKey) (guard.Provenance, int, bool) {
	role := state.validationRole(task, e.cfg.Definition)
	maxAttempts := e.retryMaxAttempts(task.Step)
	if attempt, ok := state.Attempts[task]; ok && attempt.MaxAttempts > 0 {
		maxAttempts = attempt.MaxAttempts
	}
	if attempt, ok := state.EndedAttempts[task]; ok && attempt.MaxAttempts > 0 {
		maxAttempts = attempt.MaxAttempts
	}
	switch role {
	case "engine.development.batch":
		_, plan, _ := state.confirmedDevelopmentPlan()
		if plan == nil {
			return guard.Provenance{}, 0, false
		}
		// 封闭性：候选 scope 必须属于确认计划的批次任务集（当前计划的
		// currentPlanTask 或任一已确认计划的批次任务——重走替换经
		// sourceValidationTask 折叠后核对）；计划外的 batch scope 无授权来源。
		if _, _, _, current := state.developmentBatch(task, e.cfg.Definition); !current {
			if _, planned := state.plannedBatchSource(task, e.cfg.Definition); !planned {
				return guard.Provenance{}, 0, false
			}
		}
		digest := planContractDigest(plan)
		return guard.Provenance{Kind: guard.BatchSource, Identity: guard.Identity{ID: "development-plan", Revision: digest}}, maxAttempts, true
	case "engine.qa.design", "engine.qa.review.rationale", "engine.qa.execution":
		if state.Validation == nil {
			return guard.Provenance{}, 0, false
		}
		// 派发 scope 内携带 mode 身份（generation:mode:...），按 mode 直查
		// case-set 账本；set 缺失的历史事实按其 wave target 的冻结版本定位。
		mode := taskScopeMode(task)
		if set := state.Validation.Sets[mode]; set != nil {
			return guard.Provenance{Kind: guard.QASource, Identity: guard.Identity{ID: string(mode), Revision: set.Version}}, maxAttempts, true
		}
		for i := range state.Validation.Waves {
			for _, target := range state.Validation.Waves[i].Targets {
				if target.Task == task && target.Mode == mode {
					return guard.Provenance{Kind: guard.QASource, Identity: guard.Identity{ID: string(mode), Revision: state.Validation.Waves[i].Versions[mode]}}, maxAttempts, true
				}
			}
		}
		// 首个 QA design 派发先于 case-set 存在：路线选择（所选门决定的 QA
		// mode 集）是其授权来源。
		return guard.Provenance{Kind: guard.RouteSource, Identity: guard.Identity{ID: state.RouteMode, Revision: state.validationRouteDigest()}}, maxAttempts, true
	case "engine.gate":
		if state.Validation == nil {
			return guard.Provenance{}, 0, false
		}
		for i := range state.Validation.Waves {
			if target := state.Validation.Waves[i].Targets[task.String()]; target != nil && target.Gate != "" {
				return guard.Provenance{Kind: guard.GateSource, Identity: guard.Identity{ID: target.Gate, Revision: state.validationRouteDigest()}}, maxAttempts, true
			}
		}
		return guard.Provenance{}, 0, false
	case "engine.repair":
		if state.Validation == nil {
			return guard.Provenance{}, 0, false
		}
		for i := range state.Validation.Waves {
			w := &state.Validation.Waves[i]
			if w.Repair == nil {
				continue
			}
			// 需求重走决定可把返修义务替换为新 TaskKey（binding/责任变化）；
			// 原任务与替换任务同属该 wave 的返修义务来源。
			if w.Repair.Task == task || state.replacementFold(w.Repair.Task) == task {
				return guard.Provenance{Kind: guard.RepairSource, Identity: guard.Identity{ID: task.String(), Revision: w.ExpectedDigest}}, maxAttempts, true
			}
		}
		return guard.Provenance{}, 0, false
	}
	return guard.Provenance{}, 0, false
}

// replacementFold 把一个返修 TaskKey 沿确认账本的重走决定折叠为当前责任人
// （与 repairWork 的替换计算同一规则）。
func (s *State) replacementFold(task runtime.TaskKey) runtime.TaskKey {
	for _, revision := range s.RequirementRevisions {
		for j, choice := range revision.WorkDecisions {
			if choice.Work.Task == task && choice.Disposition == RequirementWorkRedo {
				task = replacementWorkTask(revision, j)
			}
		}
	}
	return task
}

// deriveGuardBudget 从权威事实派生额度投影。包络未就位（受理前）返回 nil。
// 已签发事实无法归类或来源缺失＝账目无法对齐，返回可区分错误（BLOCKED_BUG）。
func (e *Engine) deriveGuardBudget(state *State) (*GuardBudget, error) {
	if !e.guardEngaged(state) {
		return nil, nil
	}
	issued := issuedGuardTasks(state, e.cfg.Definition)
	basis, err := e.guardBasis(state)
	if err != nil {
		return nil, &RejectedError{Code: CodeGuardBlockedBug, Detail: err.Error()}
	}
	result, err := guard.Calculate(basis)
	if err != nil {
		return nil, &RejectedError{Code: CodeGuardBlockedBug, Detail: fmt.Sprintf("dispatch budget does not align with its ledger: %v", err)}
	}
	budget := &GuardBudget{
		RequestAllowance:  result.RequestAllowance,
		RetryAllowance:    result.RetryAllowance,
		TotalAllowance:    result.TotalAllowance,
		BudgetBasisDigest: result.BudgetBasisDigest,
		UsedRequests:      len(issued),
	}
	// 已用 retry 与 guard 自身算术同口径（AC-055：retryAllowance=Σ max(0,
	// MaxAttempts−1)）：每个 TaskKey 的首个执行占用 request 槽，其余执行才是
	// retry 消耗——执行数读 attempt 账本（当前＋已终合计），下限 0。
	for task := range issued {
		executions := 0
		if attempt, ok := state.Attempts[task]; ok {
			executions += attempt.Attempts
		}
		if attempt, ok := state.EndedAttempts[task]; ok {
			executions += attempt.Attempts
		}
		if executions > 1 {
			budget.UsedRetries += executions - 1
		}
	}
	return budget, nil
}

// verifyGuardBudget 在加载边界重算额度投影并与持久值比对：相同输入重算不一致
// （篡改投影或改写 basis 事实）＝BLOCKED_BUG 拒绝读取，与审查账本完整性校验
// 同一加载边界。
func (e *Engine) verifyGuardBudget(state *State) error {
	if state.Guard == nil {
		return nil
	}
	budget, err := e.deriveGuardBudget(state)
	if err != nil {
		return err
	}
	if budget == nil || *budget != *state.Guard {
		return &RejectedError{
			Code:   CodeGuardBlockedBug,
			Detail: fmt.Sprintf("guard budget projection %v does not match the recomputed state facts %v", state.Guard.BudgetBasisDigest, budgetDigestOf(budget)),
		}
	}
	return nil
}

func budgetDigestOf(budget *GuardBudget) string {
	if budget == nil {
		return "<none>"
	}
	return budget.BudgetBasisDigest
}

// AllowanceSummary 是额度可用性的权威展示串（Ask 摘要与 repair 预览面
// 共用；nil 投影＝包络未就位，UNAVAILABLE）。显示 used/allowance 数字与
// basis digest。
func (g *GuardBudget) AllowanceSummary() string {
	if g == nil {
		return "UNAVAILABLE"
	}
	return fmt.Sprintf("AVAILABLE basis=%s requests=%d/%d retries=%d/%d total=%d/%d",
		g.BudgetBasisDigest, g.UsedRequests, g.RequestAllowance,
		g.UsedRetries, g.RetryAllowance, g.UsedRequests+g.UsedRetries, g.TotalAllowance)
}

// basisDeclaresTask 报告 basis 的外部任务集是否已声明该 TaskKey。
func basisDeclaresTask(basis guard.Basis, task runtime.TaskKey) bool {
	for _, external := range basis.ExternalTasks {
		if external.TaskKey == task {
			return true
		}
	}
	return false
}

// refreshGuardBudget 在提交点把额度投影重derive写入同一事务（单一 writer）。
// 派生失败＝本次提交拒绝（账目无法对齐的账本不得落盘）。
func (e *Engine) refreshGuardBudget(state *State) error {
	budget, err := e.deriveGuardBudget(state)
	if err != nil {
		return err
	}
	state.Guard = budget
	return nil
}

// guardIssuanceGate 返回签发漏斗的 guard 判断（issueInto 在任何落账变更前调
// 用）：QA/开发/门/repair 候选必须落在授权 basis 内——新 TaskKey 无授权来源或
// 该 TaskKey 的有限 retry 已耗尽＝零签发（GUARD_ALLOWANCE_BLOCKED，扩张只经
// 既有 typed Ask 通道：新有效义务/经确认新计划/一次性 attempt 追加）；已签发
// 事实对不上账、或 attempt 账本未标记耗尽而重算已超发＝BLOCKED_BUG。guard
// 不回写、不覆盖 series 轮次/Attempt 代次/wave 计数/返修轮次任何一者的判定，
// 只从事实计算横切包络。
func (e *Engine) guardIssuanceGate(state *State) func(decision.IssuedSet) error {
	return func(issued decision.IssuedSet) error {
		var governed []decision.IssuedAction
		for _, action := range issued {
			if state.validationRole(action.Task, e.cfg.Definition) != "" {
				governed = append(governed, action)
			}
		}
		if len(governed) == 0 {
			return nil
		}
		issuedFacts := issuedGuardTasks(state, e.cfg.Definition)
		if !e.guardEngaged(state) {
			return &RejectedError{Code: CodeGuardAllowanceBlocked, Detail: "dispatch budget is not established for this run yet"}
		}
		basis, err := e.guardBasis(state)
		if err != nil {
			return &RejectedError{Code: CodeGuardBlockedBug, Detail: err.Error()}
		}
		if _, err := guard.Calculate(basis); err != nil {
			return &RejectedError{Code: CodeGuardBlockedBug, Detail: fmt.Sprintf("dispatch budget does not align with its ledger: %v", err)}
		}
		for _, action := range governed {
			provenance, maxAttempts, ok := e.guardTaskProvenance(state, action.Task)
			if !ok {
				return &RejectedError{
					Code:   CodeGuardAllowanceBlocked,
					Detail: fmt.Sprintf("dispatch %s has no authorizing basis source; expansion requires a new valid obligation, a confirmed plan or a one-shot attempt Ask", action.Task.String()),
				}
			}
			attempts := 0
			exhausted := false
			if prior, ok := state.EndedAttempts[action.Task]; ok {
				attempts, exhausted = prior.Attempts, prior.RetryExhausted
			} else if prior, ok := state.Attempts[action.Task]; ok {
				attempts, exhausted = prior.Attempts, prior.RetryExhausted
			}
			if attempts >= maxAttempts && !exhausted {
				return &RejectedError{Code: CodeGuardBlockedBug, Detail: fmt.Sprintf("dispatch %s would exceed its finite retry budget (%d/%d) while the attempt ledger does not record exhaustion", action.Task.String(), attempts, maxAttempts)}
			}
			if attempts >= maxAttempts {
				return &RejectedError{Code: CodeGuardAllowanceBlocked, Detail: fmt.Sprintf("dispatch %s consumed its finite retry budget (%d/%d)", action.Task.String(), attempts, maxAttempts)}
			}
			// 新 TaskKey 进入 would-be basis（已在 basis 内的预授权槽位
			// 不重复声明——guard 对相同声明去重，对不同值冲突拒绝）。
			if _, fact := issuedFacts[action.Task]; !fact && !basisDeclaresTask(basis, action.Task) {
				attemptBudget := maxAttempts
				basis.ExternalTasks = append(basis.ExternalTasks, guard.ExternalTask{TaskKey: action.Task, Provenance: provenance, MaxAttempts: &attemptBudget})
			}
		}
		if _, err := guard.Calculate(basis); err != nil {
			return &RejectedError{Code: CodeGuardAllowanceBlocked, Detail: fmt.Sprintf("candidate issuance is outside the closed dispatch budget: %v", err)}
		}
		return nil
	}
}
