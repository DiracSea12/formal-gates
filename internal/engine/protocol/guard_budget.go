package protocol

import (
	"fmt"
	"sort"
	"strings"

	"formal-gates/internal/cost"
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

// token cap 面的三值（独立于 request/attempt 止损判定；无配置时 UNAVAILABLE，
// request/attempt 硬止损继续生效）。
const (
	TokenCapUnavailable = "UNAVAILABLE"
	TokenCapAvailable   = "AVAILABLE"
	TokenCapExceeded    = "EXCEEDED"
)

// GuardBudget 是从权威事实派生的额度投影：guard.Calculate 的额度数字、basis
// digest、已用事实与 token cap 面。事实源是签发/结果账本本身；本投影只在提交
// 点重derive写入，加载边界重算比对（不一致＝BLOCKED_BUG）。
type GuardBudget struct {
	RequestAllowance  int    `json:"requestAllowance"`
	RetryAllowance    int    `json:"retryAllowance"`
	TotalAllowance    int    `json:"totalAllowance"`
	BudgetBasisDigest string `json:"budgetBasisDigest"`
	UsedRequests      int    `json:"usedRequests"`
	UsedRetries       int    `json:"usedRetries"`
	// TokenCapStatus 是 token cap 可用性三值；MaxTotalTokens 只在配置在账时有
	// 效。派发用量合计（DispatchTokens）只统计 engine 派发条目，不含 owner。
	TokenCapStatus   string `json:"tokenCapStatus"`
	TokenCapMax      int64  `json:"tokenCapMaxTokens,omitempty"`
	TokenCapDispatch int64  `json:"tokenCapDispatchTokens,omitempty"`
}

// dispatchTokenTotal 汇总 engine 派发条目的 token 用量（输入＋输出）。owner 终
// 态报告在独立字段，天然不进该合计。
func (s *State) dispatchTokenTotal() int64 {
	if s.Cost == nil {
		return 0
	}
	var total int64
	for _, entry := range s.Cost.Dispatches {
		total += entry.TotalInputTokens + entry.OutputTokens
	}
	return total
}

// tokenCapStatus 是 token cap 可信度三值的单一派生：配置缺失＝UNAVAILABLE；
// 配置在账但任一派发来源不可计量（存在未知用量——不可计量的派发以零占位，
// 已知总量不能再宣称可靠余量）＝UNAVAILABLE；否则按已知总量判定
// AVAILABLE/EXCEEDED。本函数与 tokenCapExceeded 分工：状态回答"cap 数字是否
// 可信"，闸回答"已知总量是否已达上限"——两者各自单点陈述，不得在第三处重述。
func (s *State) tokenCapStatus() string {
	if s.TokenCapMaxTokens == nil {
		return TokenCapUnavailable
	}
	if s.Cost != nil {
		for _, entry := range s.Cost.Dispatches {
			if entry.Source == cost.SourceUnavailable {
				return TokenCapUnavailable
			}
		}
	}
	if s.dispatchTokenTotal() >= *s.TokenCapMaxTokens {
		return TokenCapExceeded
	}
	return TokenCapAvailable
}

// tokenCapExceeded 报告 engine 派发的已知用量是否已达配置的 token cap（已知
// 量达上限即超——未知用量只会更多，已知总量是下界闸）。无配置恒 false；已知
// 量未达而存在未知用量＝不闸（cap 维度不可信时不拦截，request/attempt 止损
// 照常）。可信度判定归 tokenCapStatus 单点。
func (e *Engine) tokenCapExceeded(state *State) bool {
	if state.TokenCapMaxTokens == nil {
		return false
	}
	return state.dispatchTokenTotal() >= *state.TokenCapMaxTokens
}

// guardEngaged 报告 guard 包络是否作用于当前状态：验证管线就位（拓扑、路
// 线、确认计划或主线拆分拓扑与需求文档都在账）即生效，与是否已有签发事实无关。需求重走
// 解绑拓扑/路线的再确认窗口内包络不生效（basis 身份暂缺；该窗口内验证任
// 务本就不可投影签发，不构成扩容通道）。受理前的审查系列派发不进包络
// （plan:361「QA/开发/门的外部派发统一经过 guard」；系列轮次的权威是
// series 账本）。
func (e *Engine) guardEngaged(state *State) bool {
	if state.Validation == nil || state.Topology == nil || state.RouteMode == "" {
		return false
	}
	_, plan, _ := state.confirmedDevelopmentPlan()
	return plan != nil || state.mergeMaster()
}

// guardBasis 是 Basis 构造单点：从权威 State 构造 guard.Basis（guard 纯核心不
// 重写不绕过）。来源按合同钉死：definition＝编译定义身份＋closed-world agent
// steps；requirement＝确认账本末条；Batch＝确认计划或主线拓扑 digest；route/
// topology＝绑定 digest；approvedQA＝各选中 mode 的 case-set 版本；selectedGates
// ＝路线＋所选门；established obligations/repairs 只接 adjudication 完成、wave
// 收齐后的有效义务与返修 TaskKey。ExternalTasks＝已确认授权全集单一枚举
// （额度来自已确认 canonical expected external TaskKey 集，Ready 只决定
// 本次可签发子集）：确认计划的全部批次任务（无论 Ready 与否）＋已建 wave 的全
// 部 target（QA/门）＋返修义务全集（含欠账未签发与重走替换者）＋预授权 QA 设计/
// 审查槽；已签发治理事实逐个归类入集，对不上＝BLOCKED_BUG。raw finding、普通
// result/receipt/resume 不是这些账本的成员，机械不可达。
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
	if plan == nil && !state.mergeMaster() {
		return guard.Basis{}, fmt.Errorf("guard budget: no confirmed development plan backs the dispatch budget")
	}
	batchIdentity := guard.Identity{ID: "development-plan", Revision: planContractDigest(plan)}
	if state.mergeMaster() && len(state.planTasks(e.cfg.Definition)) == 0 {
		batchIdentity = guard.Identity{ID: "merge-topology", Revision: digestOfCanonical(state.Topology)}
	}
	routeDigest := state.validationRouteDigest()
	basis := guard.Basis{
		SchemaVersion: 1,
		Requirement:   guard.ConfirmedIdentity{Identity: guard.Identity{ID: requirementID, Revision: requirement}, ConfirmedRevision: requirement},
		Batch:         guard.ConfirmedIdentity{Identity: batchIdentity, ConfirmedRevision: batchIdentity.Revision},
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
	if state.mergeMaster() {
		for _, sliceID := range state.Topology.Slices {
			receipt := state.SliceReceipts[state.SliceChildren[sliceID]]
			for _, gate := range receipt.Receipt.SelectedGates {
				basis.SelectedGates = append(basis.SelectedGates, guard.Identity{ID: sliceID + ":" + gate, Revision: receipt.Digest})
			}
		}
	}

	if state.Validation != nil {
		// 同一有效义务/返修 TaskKey 可能跨多个 wave 出现（继承/重开）：以
		// 首个建立它的 wave 为其身份 revision（wave 追加有序，身份稳定），
		// 不因后续 wave 的 expected digest 变化产生冲突声明。
		obligations := map[string]guard.Identity{}
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
		}
		repairs := map[string]guard.Identity{}
		for _, w := range state.establishedRepairWaves() {
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
		for _, id := range sortedIdentityIDs(obligations) {
			basis.EstablishedObligations = append(basis.EstablishedObligations, obligations[id])
		}
		for _, id := range sortedIdentityIDs(repairs) {
			basis.EstablishedRepairs = append(basis.EstablishedRepairs, repairs[id])
		}
	}
	// 授权全集枚举（单一事实源）：枚举桶内任务经 guardTaskProvenance 归类——
	// 分类失败的枚举项即构造错误（返回错误，零派发）。
	authorized := map[runtime.TaskKey]bool{}
	declare := func(task runtime.TaskKey) error {
		if !task.Valid() || authorized[task] {
			return nil
		}
		provenance, maxAttempts, ok := e.guardTaskProvenance(state, task)
		if !ok {
			return fmt.Errorf("guard budget: authorized dispatch %s has no authorizing source", task.String())
		}
		authorized[task] = true
		attempts := maxAttempts
		basis.ExternalTasks = append(basis.ExternalTasks, guard.ExternalTask{TaskKey: task, Provenance: provenance, MaxAttempts: &attempts})
		return nil
	}
	if input := state.validationObservationInput(); input != nil {
		if _, registered := pipelineStep(e.cfg.Definition, "engine.validation.observation.v2"); registered {
			if err := declare(observationTask(e.cfg.Definition, input)); err != nil {
				return guard.Basis{}, err
			}
		}
	}
	// 桶 1：确认计划的全部批次任务（currentPlanTask 逐批，依赖未 Ready 同样
	// 计入——批次身份是 plan digest，不随签发进度漂移）。
	for _, task := range state.planTasks(e.cfg.Definition) {
		if err := declare(task); err != nil {
			return guard.Basis{}, err
		}
	}
	// 桶 1b：需求重走替换任务——确认账本的 redo 决定经用户确认，替换 TaskKey
	// 由该 revision 机械派生（预授权；provenance 经 sourceValidationTask 折叠
	// 回原任务归类）。
	for _, entry := range state.RequirementRevisions {
		if !state.requirementWorkRevisionCurrent(entry.StateRevision) {
			continue
		}
		for j, choice := range entry.WorkDecisions {
			if choice.Disposition == RequirementWorkRedo {
				if err := declare(replacementWorkTask(entry, j)); err != nil {
					return guard.Basis{}, err
				}
			}
		}
	}
	if state.Validation != nil {
		if p, ready := state.nextIntegrationParameters(e.cfg.Definition); ready && p.Integration.StartAction != "" && len(state.conflictInput(p.Integration.StartAction).Evidence.Conflicts) > 0 {
			if err := declare(pipelineTask(e.cfg.Definition, "engine.merge.resolve.v3", p.Integration.StartAction)); err != nil {
				return guard.Basis{}, err
			}
		}
		// 桶 2：已建 wave 的全部 target（QA/门，含 PENDING 未签发槽位）。
		for i := range state.Validation.Waves {
			for _, key := range sortedTargetKeys(&state.Validation.Waves[i]) {
				if err := declare(state.Validation.Waves[i].Targets[key].Task); err != nil {
					return guard.Basis{}, err
				}
			}
		}
		// 桶 3：返修义务全集——已成立返修 wave 的返修 TaskKey（含重走替换
		// 者）与 repairWork 现责任人（wave 收齐、裁决定案后仍欠执行的返修义
		// 务，含尚未签发者；预授权机械加入，不需另行确认）。
		for _, repairWave := range state.establishedRepairWaves() {
			if err := declare(repairWave.Repair.Task); err != nil {
				return guard.Basis{}, err
			}
			if err := declare(state.replacementFold(repairWave.Repair.Task)); err != nil {
				return guard.Basis{}, err
			}
		}
		for _, repairWave := range state.Validation.Waves {
			if repairWave.Repair != nil {
				for _, target := range repairWave.Repair.Targets {
					if err := declare(target.Task); err != nil {
						return guard.Basis{}, err
					}
				}
			}
		}
		for _, work := range state.repairWork() {
			if err := declare(work.task); err != nil {
				return guard.Basis{}, err
			}
		}
		for _, context := range state.repairComparisonContexts(e.cfg.Definition) {
			if err := declare(context.binding.Task); err != nil {
				return guard.Basis{}, err
			}
		}
		// 桶 4：预授权 QA 设计/审查槽——镜像 projectValidationTasks 的当前投影
		//（set 缺失或 DRAFT/THAWED 的设计槽、REVIEW 的审查槽；首个 QA design
		// 派发先于 case-set 存在，授权来源是路线选择）。
		for _, mode := range state.validationKinds() {
			set := state.Validation.Sets[mode]
			if set == nil {
				if mode == coverage.ReviewBlackbox || mode == coverage.ReviewMerge || state.Validation.AuthoringWorkspace != "" {
					if err := declare(pipelineTask(e.cfg.Definition, "engine.qa.design.v5", state.nextDesignScope(mode, e.cfg.Definition))); err != nil {
						return guard.Basis{}, err
					}
				}
				continue
			}
			switch set.Status {
			case "DRAFT", "THAWED":
				if err := declare(pipelineTask(e.cfg.Definition, "engine.qa.design.v5", state.nextDesignScope(mode, e.cfg.Definition))); err != nil {
					return guard.Basis{}, err
				}
			case "REVIEW":
				if err := declare(pipelineTask(e.cfg.Definition, "engine.qa.review.rationale.v2", state.reviewTask(mode, state.Validation.Generation, set.Version, e.cfg.Definition).Scope)); err != nil {
					return guard.Basis{}, err
				}
			}
		}
	}
	// 桶 5：已签发治理事实必须能归入全集（provenance 可归类）——对不上即
	// BLOCKED_BUG（账目无法对齐）；历史授权槽（如被替换的 design scope）按
	// 事实入集。
	for task := range issuedGuardTasks(state, e.cfg.Definition) {
		if err := declare(task); err != nil {
			return guard.Basis{}, err
		}
	}
	return basis, nil
}

// establishedRepairWaves 枚举「已成立返修」的 wave——该判定的单一归属点：
// Repair 只在 wave 裁决定案时落账（settleValidationWave），一经成立即为持
// 久的授权事实；后续回归（路线重绑把 wave 置 INVALIDATED、新到未裁决
// finding）不解散已成立的返修，只产生新义务。授权全集（桶 3）与 basis 的
// RepairSource 声明集共用本枚举，声明与来源永不各执一词。
func (s *State) establishedRepairWaves() []*ValidationWave {
	var waves []*ValidationWave
	for i := range s.Validation.Waves {
		if w := &s.Validation.Waves[i]; w.Repair != nil && w.Status != "COLLECTING" {
			waves = append(waves, w)
		}
	}
	return waves
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
	for id, binding := range state.DispatchBindings {
		if state.beforeResetBoundary(id) {
			continue
		}
		collect(binding.Task)
	}
	for task := range state.Attempts {
		collect(task)
	}
	for task, attempt := range state.EndedAttempts {
		if state.beforeResetBoundary(attempt.ActionID) {
			continue
		}
		collect(task)
	}
	for _, pending := range state.PendingActions {
		collect(pending.Task)
	}
	for id, obsolete := range state.ObsoleteActions {
		if state.beforeResetBoundary(id) {
			continue
		}
		collect(obsolete.Task)
	}
	for id, receipt := range state.SpawnReceipts {
		if state.beforeResetBoundary(id) {
			continue
		}
		collect(receipt.Task)
	}
	return tasks
}

// guardTaskProvenance 按任务角色归类 provenance（合同：「provenance 按上述
// source 归类」）并给出该 TaskKey 的现行有限 retry 策略。策略读 attempt 账本
// （一次性用户追加只改该账本的 MaxAttempts，AC-059），无账本条目时用编译步策略。
// 需求重走替换任务先经 sourceValidationTask 折叠回原任务——替换与原任务同源
// 声明，provenance 按其来源归类。
func (e *Engine) guardTaskProvenance(state *State, task runtime.TaskKey) (guard.Provenance, int, bool) {
	role := state.validationRole(task, e.cfg.Definition)
	source := state.sourceValidationTask(task)
	maxAttempts := e.retryMaxAttempts(task.Step)
	if attempt, ok := state.Attempts[task]; ok && attempt.MaxAttempts > 0 {
		maxAttempts = attempt.MaxAttempts
	}
	if attempt, ok := state.EndedAttempts[task]; ok && attempt.MaxAttempts > 0 {
		maxAttempts = attempt.MaxAttempts
	}
	switch role {
	case "engine.validation.observation.v2":
		if input := state.validationObservationInput(); input != nil && source == observationTask(e.cfg.Definition, input) {
			return guard.Provenance{Kind: guard.RouteSource, Identity: guard.Identity{ID: state.RouteMode, Revision: state.validationRouteDigest()}}, maxAttempts, true
		}
		for _, binding := range state.DispatchBindings {
			if state.sourceValidationTask(binding.Task) == source {
				if binding.WaveID == "" && binding.RouteScope != "" {
					return guard.Provenance{Kind: guard.RouteSource, Identity: guard.Identity{ID: state.RouteMode, Revision: binding.RouteScope}}, maxAttempts, true
				}
				if wave := state.validationWave(binding.WaveID); wave != nil && wave.Candidate.Identity == binding.Candidate {
					return guard.Provenance{Kind: guard.RouteSource, Identity: guard.Identity{ID: state.RouteMode, Revision: state.validationRouteDigest()}}, maxAttempts, true
				}
			}
		}
		return guard.Provenance{}, 0, false
	case "engine.merge.resolve.v3":
		input := state.conflictInput(source.Scope)
		if input == nil || len(input.Evidence.Conflicts) == 0 {
			return guard.Provenance{}, 0, false
		}
		return guard.Provenance{Kind: guard.RouteSource, Identity: guard.Identity{ID: state.RouteMode, Revision: state.validationRouteDigest()}}, maxAttempts, true
	case "engine.development.batch.v4":
		_, plan, _ := state.confirmedDevelopmentPlan()
		if plan == nil {
			return guard.Provenance{}, 0, false
		}
		// 封闭性：候选 scope 必须属于确认计划的批次任务集（当前计划的
		// currentPlanTask 或任一已确认计划的批次任务——重走替换经
		// sourceValidationTask 折叠后核对）；计划外的 batch scope 无授权来源。
		if _, _, _, current := state.developmentBatch(task, e.cfg.Definition); !current {
			if _, planned := state.plannedBatchSource(task); !planned {
				return guard.Provenance{}, 0, false
			}
		}
		digest := planContractDigest(plan)
		return guard.Provenance{Kind: guard.BatchSource, Identity: guard.Identity{ID: "development-plan", Revision: digest}}, maxAttempts, true
	case "engine.qa.design.v5", "engine.qa.review.rationale.v2", "engine.qa.execution.v3":
		if state.Validation == nil {
			return guard.Provenance{}, 0, false
		}
		// 派发 scope 内携带 mode 身份（generation:mode:...），按 mode 直查
		// case-set 账本；set 缺失的历史事实按其 wave target 的冻结版本定位。
		mode := taskScopeMode(source)
		if set := state.Validation.Sets[mode]; set != nil {
			return guard.Provenance{Kind: guard.QASource, Identity: guard.Identity{ID: string(mode), Revision: set.Version}}, maxAttempts, true
		}
		for i := range state.Validation.Waves {
			for _, target := range state.Validation.Waves[i].Targets {
				if target.Task == source && target.Mode == mode {
					return guard.Provenance{Kind: guard.QASource, Identity: guard.Identity{ID: string(mode), Revision: state.Validation.Waves[i].Versions[mode]}}, maxAttempts, true
				}
			}
		}
		// 首个 QA design 派发先于 case-set 存在：路线选择（所选门决定的 QA
		// mode 集）是其授权来源。
		return guard.Provenance{Kind: guard.RouteSource, Identity: guard.Identity{ID: state.RouteMode, Revision: state.validationRouteDigest()}}, maxAttempts, true
	case "engine.gate.v2":
		if _, target := state.repairGateTarget(source); target != nil {
			return guard.Provenance{Kind: guard.GateSource, Identity: guard.Identity{ID: target.Gate, Revision: state.validationRouteDigest()}}, maxAttempts, true
		}
		if state.Validation == nil {
			return guard.Provenance{}, 0, false
		}
		for i := range state.Validation.Waves {
			if target := state.Validation.Waves[i].Targets[source.String()]; target != nil && target.Gate != "" {
				identity := guard.Identity{ID: target.Gate, Revision: state.validationRouteDigest()}
				if target.SliceID != "" {
					identity = guard.Identity{ID: target.SliceID + ":" + target.Gate, Revision: state.SliceReceipts[state.SliceChildren[target.SliceID]].Digest}
				}
				return guard.Provenance{Kind: guard.GateSource, Identity: identity}, maxAttempts, true
			}
		}
		return guard.Provenance{}, 0, false
	case "engine.repair.verify.v2":
		context, ok := state.repairComparisonContext(task, e.cfg.Definition)
		if !ok {
			context, ok = state.repairComparisonContext(source, e.cfg.Definition)
		}
		if !ok {
			for _, binding := range state.DispatchBindings {
				if binding.Task != task && state.sourceValidationTask(binding.Task) != source {
					continue
				}
				if wave := state.validationWave(binding.WaveID); wave != nil && wave.Repair != nil && binding.VerificationSource != "" {
					return guard.Provenance{Kind: guard.RepairSource, Identity: guard.Identity{ID: wave.Repair.Task.String(), Revision: wave.ExpectedDigest}}, maxAttempts, true
				}
			}
		}
		if !ok || context.wave.Repair == nil {
			return guard.Provenance{}, 0, false
		}
		return guard.Provenance{Kind: guard.RepairSource, Identity: guard.Identity{ID: context.wave.Repair.Task.String(), Revision: context.wave.ExpectedDigest}}, maxAttempts, true
	case "engine.repair.v5":
		if state.Validation == nil {
			return guard.Provenance{}, 0, false
		}
		for i := range state.Validation.Waves {
			w := &state.Validation.Waves[i]
			if w.Repair == nil {
				continue
			}
			// 需求重走决定可把返修义务替换为新 TaskKey（binding/责任变化）；
			// 折叠后的来源任务与该 wave 的返修义务互为同源。
			if w.Repair.Task == source {
				return guard.Provenance{Kind: guard.RepairSource, Identity: guard.Identity{ID: source.String(), Revision: w.ExpectedDigest}}, maxAttempts, true
			}
		}
		for _, work := range state.repairWork() {
			if state.sourceValidationTask(work.task) == source {
				return guard.Provenance{Kind: guard.RepairSource, Identity: guard.Identity{ID: work.wave.Repair.Task.String(), Revision: work.wave.ExpectedDigest}}, maxAttempts, true
			}
		}
		for _, binding := range state.DispatchBindings {
			if state.sourceValidationTask(binding.Task) != source {
				continue
			}
			if wave := state.validationWave(binding.WaveID); wave != nil && wave.Repair != nil && source.Step == wave.Repair.Task.Step {
				return guard.Provenance{Kind: guard.RepairSource, Identity: guard.Identity{ID: wave.Repair.Task.String(), Revision: wave.ExpectedDigest}}, maxAttempts, true
			}
		}
		return guard.Provenance{}, 0, false
	}
	return guard.Provenance{}, 0, false
}

// replacementFold 把一个返修 TaskKey 沿确认账本的重走决定折叠为当前责任人
// （返修义务枚举与 repairWork 的替换计算共用本归属）。
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
	// token cap 投影：状态是可信度三值（tokenCapStatus 单点派生——配置缺失或
	// 存在不可计量派发＝UNAVAILABLE）；Max/Dispatch 是配置与已记录（已知）总量
	// 的事实值，UNAVAILABLE 下不作余量承诺展示。
	budget.TokenCapStatus = state.tokenCapStatus()
	if state.TokenCapMaxTokens != nil {
		budget.TokenCapMax = *state.TokenCapMaxTokens
		budget.TokenCapDispatch = state.dispatchTokenTotal()
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

// guardTaskAuthorized 是单任务派发 guard 判定（issueInto 漏斗与恢复换新共用）：
// 治理任务（validation role）必须落在授权 basis 枚举内且有限 retry 未耗尽——
// 无授权来源或不在全集＝GUARD_ALLOWANCE_BLOCKED（扩张只经既有 typed Ask 通道）；
// attempt 账本未标记耗尽而重算已超发＝BLOCKED_BUG。
func (e *Engine) guardTaskAuthorized(state *State, basis guard.Basis, task runtime.TaskKey) error {
	_, maxAttempts, ok := e.guardTaskProvenance(state, task)
	if !ok || !basisDeclaresTask(basis, task) {
		return &RejectedError{
			Code:   CodeGuardAllowanceBlocked,
			Detail: fmt.Sprintf("dispatch %s is outside the authorized dispatch budget; expansion requires a new valid obligation, a confirmed plan or a budget disposition Ask", task.String()),
		}
	}
	attempts := 0
	exhausted := false
	if prior, ok := state.EndedAttempts[task]; ok {
		attempts, exhausted = prior.Attempts, prior.RetryExhausted
	} else if prior, ok := state.Attempts[task]; ok {
		attempts, exhausted = prior.Attempts, prior.RetryExhausted
	}
	if attempts >= maxAttempts && !exhausted {
		return &RejectedError{Code: CodeGuardBlockedBug, Detail: fmt.Sprintf("dispatch %s would exceed its finite retry budget (%d/%d) while the attempt ledger does not record exhaustion", task.String(), attempts, maxAttempts)}
	}
	if attempts >= maxAttempts {
		return &RejectedError{Code: CodeGuardAllowanceBlocked, Detail: fmt.Sprintf("dispatch %s consumed its finite retry budget (%d/%d)", task.String(), attempts, maxAttempts)}
	}
	return nil
}

// guardEffectBudget 判定一次宿主效果执行（resume intent 或换新 install）的
// 派发预算，公开 ResumeAgent 与恢复入口的全部执行创建点共用同一判定，两维：
//   - retry 余量：绑定未变的执行消耗有限次数——Attempt 账本已计执行＋在途
//     resume intent＋本次将达上限即拒绝（无有限策略的步骤不参与该维）；
//   - token cap：engine 派发用量达到配置上限且无未消耗的一次继续授权即拒
//     绝（与签发漏斗同一算式）。
//
// 被阻时幂等落 DISPATCH_BUDGET_DISPOSITION Ask 并返回拒绝（Ask 即进展，不是
// 错误）；本函数不改其他状态、不提交——提交形状归调用方（公开入口落 Ask 后
// 提交并拒绝返回；事务收尾内的恢复路径直接跳过效果创建）。workerActionID 解
// 析失败不在此拒（交给 agentHostIntent 以 UNKNOWN_ACTION 拒绝）。
func (e *Engine) guardEffectBudget(s *State, workerActionID string, nextRevision uint64) *RejectedError {
	pending, attempt, ok := s.workerActionTarget(workerActionID)
	if !ok {
		return nil
	}
	if attempt.MaxAttempts > 0 {
		inFlight, blocked := s.effectRetryBudget(attempt)
		if blocked {
			s.ensureDispatchBudgetAsk(pending.Task.String(), nextRevision)
			return &RejectedError{
				Code: CodeGuardAllowanceBlocked,
				Detail: fmt.Sprintf("execution of %s consumed its finite retry budget (%d executions recorded, %d resume in flight, max %d); use the current typed budget disposition",
					pending.Task.String(), attempt.Attempts, inFlight, attempt.MaxAttempts),
			}
		}
	}
	return e.guardTokenBudget(s, pending.Task.String(), nextRevision)
}

func (e *Engine) guardTokenBudget(s *State, target string, nextRevision uint64) *RejectedError {
	if e.tokenCapExceeded(s) && !s.budgetContinueGranted(e.cfg.Definition) {
		s.ensureDispatchBudgetAsk(target, nextRevision)
		return &RejectedError{
			Code:   CodeGuardAllowanceBlocked,
			Detail: "engine dispatch token cap reached; further automatic dispatch requires a budget disposition authorization",
		}
	}
	return nil
}

// effectRetryBudget derives the finite effect boundary from the original attempt
// and already signed resume intents; offers and execution admission share it.
func (s *State) effectRetryBudget(attempt Attempt) (inFlight int, blocked bool) {
	for _, intent := range s.PendingHostActions {
		if intent.Operation == HostActionResumeAgent && intent.Resume != nil && intent.Resume.WorkerActionID == attempt.ActionID {
			inFlight++
		}
	}
	return inFlight, attempt.MaxAttempts > 0 && attempt.Attempts+inFlight+1 >= attempt.MaxAttempts
}

// guardIssuanceGate 返回签发漏斗的 guard 判断（issueInto 在任何落账变更前调
// 用）：治理任务候选必须落在授权 basis 内——不在全集或该 TaskKey 的有限 retry
// 已耗尽＝零签发；已签发事实对不上账、或 attempt 账本未标记耗尽而重算已超发＝
// BLOCKED_BUG。达到 token cap 且无一次继续授权时同样零签发。guard 不回写、不
// 覆盖 series 轮次/Attempt 代次/wave 计数/返修轮次任何一者的判定，只从事实计
// 算横切包络。
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
		if !e.guardEngaged(state) {
			return &RejectedError{Code: CodeGuardAllowanceBlocked, Detail: "dispatch budget is not established for this run yet"}
		}
		if e.tokenCapExceeded(state) && !state.budgetContinueGranted(e.cfg.Definition) {
			return &RejectedError{Code: CodeGuardAllowanceBlocked, Detail: "engine dispatch token cap reached; further automatic dispatch requires a budget disposition authorization"}
		}
		basis, err := e.guardBasis(state)
		if err != nil {
			return &RejectedError{Code: CodeGuardBlockedBug, Detail: err.Error()}
		}
		if _, err := guard.Calculate(basis); err != nil {
			return &RejectedError{Code: CodeGuardBlockedBug, Detail: fmt.Sprintf("dispatch budget does not align with its ledger: %v", err)}
		}
		for _, action := range governed {
			if err := e.guardTaskAuthorized(state, basis, action.Task); err != nil {
				return err
			}
		}
		return nil
	}
}
