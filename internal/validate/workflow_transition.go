package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func requireTransition(state RunState, operation, target string) error {
	if operation == "requirements-clarification" {
		if state.RequirementConfirmed {
			return fmt.Errorf("the current requirement is already confirmed")
		}
		return nil
	}
	if !state.RequirementConfirmed {
		return fmt.Errorf("the current requirement is not confirmed")
	}
	if operation == "route" {
		// 轻量路线在 start 即声明，路线已决定：route 操作既不要求拆分决定（轻量免拆分），
		// 也不能再做路线确认，直接提示轻量路线已在 start 决定，而不是误报缺拆分决定。
		if isLightweight(state) {
			return fmt.Errorf("the lightweight route was already decided at start")
		}
		if !slicingRecorded(state) {
			return fmt.Errorf("the slicing decision must be recorded before the route")
		}
		if state.RouteMode != "" {
			return fmt.Errorf("the run already has its one route decision")
		}
		return nil
	}
	// 产品审（Part 1）、start-readiness（Part 2）与快速路径的黑盒 QA 设计在拆分决定
	// 与路线确认之前进行，不受路线已确认约束；其余下游操作都需要已确认路线。qa-design
	// 的快速路径分支自行在 case 内约束（见下）。
	if routeRequiresConfirmation(operation) && state.RouteMode == "" {
		return fmt.Errorf("the gate route is not confirmed")
	}
	switch operation {
	case "route-add":
		return requireRouteAddTransition(state)
	case "product-review":
		return requireProductReviewTransition(state)
	case "start-readiness":
		return requireStartReadinessTransition(state)
	case "qa-design":
		return requireQADesignTransition(state, target)
	case "qa-review":
		return requireQAReviewTransition(state, target)
	case "development-worker":
		return requireDevelopmentTransition(state)
	case "snapshot":
		return requireSnapshotTransition(state)
	case "qa-execution":
		return requireQAExecutionTransition(state, target)
	case "gate":
		return requireGateTransition(state, target)
	case "carry":
		return requireCarryTransition(state)
	case "seal":
		return requireSealTransition(state)
	default:
		return fmt.Errorf("unknown workflow transition %q", operation)
	}
}

// routeRequiresConfirmation reports whether the operation must run after the gate
// route is confirmed. The pre-route operations are product-review (Part 1),
// start-readiness (Part 2), and the fast-path blackbox qa-design, which run
// before the slicing decision and route confirmation.
func routeRequiresConfirmation(operation string) bool {
	return operation != "product-review" && operation != "start-readiness" && operation != "qa-design"
}

func requireRouteAddTransition(state RunState) error {
	developmentStatus := state.Actions["development-worker"].Status
	if developmentStatus == developmentPrepared || developmentStatus == developmentRepairPrepared {
		return fmt.Errorf("the gate route cannot change while a development worker is prepared")
	}
	if state.PreRepairSnapshot != "" {
		return fmt.Errorf("the gate route cannot change while a repair snapshot requires verification")
	}
	return nil
}

func requireProductReviewTransition(state RunState) error {
	if state.Actions["product-review"].Status == "PASS" {
		return fmt.Errorf("Product Review already has an authoritative PASS result")
	}
	// 重置恢复重做放行：开发已开始但整体审被 workflow reset 清回待做（本审 PENDING）时，
	// 允许重做产品审（需求 5：重置后整体审可重做，正常流程的「开发开始后不得重做整体审」
	// 顺序守卫须为重置后的恢复重做放行）。该组合只在重置后出现——正常流程一旦开发开始，
	// product-review 必已 PASS（先审后开发），不可能再回到 PENDING。仍在待做之外（已记录
	// 权威结果）的状态继续被顺序守卫拦截，正常「先开发后审查」防错顺序不变。
	if developmentStarted(state) && state.Actions["product-review"].Status != "PENDING" {
		return fmt.Errorf("Product Review must be recorded before development")
	}
	return nil
}

func requireStartReadinessTransition(state RunState) error {
	// 与产品审一致：开发已开始但整体审被 reset 清回待做（本审 PENDING）时放行重做
	// start-readiness（需求 5）；待做之外的已记录结果仍被顺序守卫拦截，正常顺序不变。
	// product-review 必须先 PASS 的顺序由下方校验保持，恢复重做也必须先重过产品审。
	if developmentStarted(state) && state.Actions["start-readiness"].Status != "PENDING" {
		return fmt.Errorf("Start Readiness must be recorded before development")
	}
	if !actionPassedOrAbsent(state, "product-review") {
		return fmt.Errorf("Product Review must pass before Start Readiness")
	}
	return nil
}

func requireQADesignTransition(state RunState, target string) error {
	if state.RouteMode == "" {
		// 快速路径：黑盒 QA 设计在拆分决定与路线确认前与 start-readiness 并行开始，
		// 此时路线尚未确认、QA 也未被选中；设计是黑盒且是固有取舍，最终路线不含黑盒
		// QA 时并行设计即废弃。
		if !actionPassedOrAbsent(state, "product-review") {
			return fmt.Errorf("Product Review must pass before QA Design")
		}
		if developmentStarted(state) {
			return fmt.Errorf("QA Design must be recorded before development")
		}
		return nil
	}
	if !isSelectedQA(state) {
		return fmt.Errorf("QA is not selected")
	}
	// 黑盒设计不再被"开发已开始"阻止：黑盒 qa-design/qa-review 在 QA 隔离工作区与
	// 开发并发推进。
	if !actionPassedOrAbsent(state, "product-review") {
		return fmt.Errorf("Product Review must pass before QA Design")
	}
	// 设计记录后、该 mode 的 review 派发尚未准备（无 OPEN/CLAIMED qa-review
	// 派发）时，允许再次调用 qa-design 追加/更新用例集（保留既有已批准用例、增量补全）；
	// 只有该 mode 的 review 派发准备后设计才锁定（锁按 mode，黑盒 review 在飞
	// 不锁白盒 qa-design）。qa-review 为 PENDING（无派发）或 PASS/FAIL 后均允许。
	if qaReviewDispatchPrepared(state, target) {
		return fmt.Errorf("the QA case set is locked for an already-prepared QA Review dispatch")
	}
	return nil
}

func requireQAReviewTransition(state RunState, target string) error {
	if !isSelectedQA(state) {
		return fmt.Errorf("QA is not selected")
	}
	// 黑盒 review 不再被"开发已开始"阻止（与开发并发）；零用例可流到 review 的集合
	// 覆盖判定（被选中模式零用例是覆盖缺失，判 P1 阻塞）。判定只受本 mode
	// 的 design/review 结果约束（target=mode），另一 mode 的 review FAIL 不阻止本 mode。
	if state.qaDesign(target).Status != "PASS" {
		return fmt.Errorf("a complete QA case set is required before QA Review")
	}
	if status := state.qaReview(target).Status; status == "PASS" || status == "FAIL" {
		return fmt.Errorf("QA Review already has an authoritative %s result for the current case set", status)
	}
	return nil
}

func requireDevelopmentTransition(state RunState) error {
	developmentStatus := state.Actions["development-worker"].Status
	if developmentStatus != developmentPending && developmentStatus != developmentPrepared && developmentStatus != developmentRepairPrepared && developmentStatus != developmentComplete && developmentStatus != developmentVerified {
		return fmt.Errorf("development worker is already prepared")
	}
	if !actionPassedOrAbsent(state, "product-review") {
		return fmt.Errorf("Product Review must pass before development")
	}
	if !actionPassedOrAbsent(state, "start-readiness") {
		return fmt.Errorf("Start Readiness must pass before development")
	}
	if !slicingRecorded(state) {
		return fmt.Errorf("the slicing decision must be recorded before development")
	}
	// 开发开始不再要求黑盒 qa-review PASS：黑盒 QA 设计/review/返修在 QA 隔离工作区
	// 与开发并发推进，快照门（需求 2）在两边都完成时才放行。
	if developmentStatus == developmentPrepared || developmentStatus == developmentRepairPrepared {
		return nil
	}
	if developmentStatus == developmentComplete || developmentStatus == developmentVerified {
		if !reviewWaveRecorded(state) {
			if state.PreRepairSnapshot != "" {
				return fmt.Errorf("the current repair still requires verification")
			}
			return fmt.Errorf("all selected review results must be recorded before repair")
		}
		if !hasRepairableBlocker(state) && !hasSuggestionRecommendation(state) {
			if state.PreRepairSnapshot != "" {
				return fmt.Errorf("the current repair still requires verification")
			}
			return fmt.Errorf("no recorded result requires repair")
		}
		if (developmentStatus != developmentVerified || hasSelectedRuntimeError(state)) && !runtimeErrorsAuthorizedForRepair(state) {
			if state.PreRepairSnapshot != "" {
				return fmt.Errorf("the current repair still requires verification")
			}
			return fmt.Errorf("the current review wave is not complete")
		}
		if state.CompletedReviewWaves >= effectiveReviewWaveLimit(state) {
			return fmt.Errorf("review-wave limit is exhausted; explicit additional repair authorization is required")
		}
	}
	return nil
}

func requireSnapshotTransition(state RunState) error {
	developmentStatus := state.Actions["development-worker"].Status
	// 白盒测试代码推进路径（方案 A）：开发已完成、白盒 QA 已选中且白盒设计已记录时，
	// host 提交白盒设计者交付的测试代码后推进快照到含测试代码的新提交。此路径无开发
	// 工作者派发可引用，直接放行（product-review / start-readiness 已由开发快照保证）。
	if whiteboxTestCodeAdvancement(state) {
		return nil
	}
	adoptingMergedSlices := state.RetainedOverall && developmentStatus == developmentPending
	adoptingSliceRepair := state.RetainedOverall && (developmentStatus == developmentComplete || developmentStatus == developmentVerified)
	if !adoptingMergedSlices && !adoptingSliceRepair && developmentStatus != developmentPrepared && developmentStatus != developmentRepairPrepared {
		return fmt.Errorf("development worker must be prepared before a snapshot")
	}
	if !actionPassedOrAbsent(state, "product-review") {
		return fmt.Errorf("Product Review must pass before a development snapshot")
	}
	if !actionPassedOrAbsent(state, "start-readiness") {
		return fmt.Errorf("Start Readiness must pass before a development snapshot")
	}
	// 快照黑盒门（开发完成 且 黑盒 qa-review PASS）在 AdvanceSnapshot 内强制，
	// 用户显式授权可手动放行；此处不再重复校验。
	if state.PreRepairSnapshot != "" && !runtimeErrorsAuthorizedForRepair(state) {
		return fmt.Errorf("the current repair still requires verification")
	}
	if developmentStatus == developmentRepairPrepared || adoptingSliceRepair {
		if !reviewWaveRecorded(state) {
			return fmt.Errorf("all selected review results must be recorded before repair")
		}
		if !hasRepairableBlocker(state) && !hasSuggestionRecommendation(state) {
			return fmt.Errorf("no recorded result requires repair")
		}
		if adoptingSliceRepair && (developmentStatus != developmentVerified || hasSelectedRuntimeError(state)) && !runtimeErrorsAuthorizedForRepair(state) {
			return fmt.Errorf("the current review wave is not complete")
		}
		if state.CompletedReviewWaves >= effectiveReviewWaveLimit(state) {
			return fmt.Errorf("review-wave limit is exhausted; explicit additional repair authorization is required")
		}
	}
	return nil
}

func requireQAExecutionTransition(state RunState, target string) error {
	if !isSelectedQA(state) {
		return fmt.Errorf("QA is not selected")
	}
	if !hasDevelopmentSnapshot(state) {
		return fmt.Errorf("an immutable development snapshot is required before QA Execution")
	}
	// 按目标 mode 读 design/review 结果（target=mode；合并/单派发为 ""）。
	if state.qaDesign(target).Status != "PASS" {
		return fmt.Errorf("QA Design must pass before QA Execution")
	}
	// 黑盒 review 经用户授权放行后，qa-execution 只覆盖已批准用例；未放行时仍要求
	// review PASS。
	if state.qaReview(target).Status != "PASS" && !snapshotBlackboxReleased(state) {
		return fmt.Errorf("QA Review must pass before QA Execution")
	}
	return nil
}

func requireGateTransition(state RunState, target string) error {
	if !isSelected(state, target) {
		return fmt.Errorf("gate %q is not selected", target)
	}
	if !hasDevelopmentSnapshot(state) {
		return fmt.Errorf("an immutable development snapshot is required before post-development review")
	}
	if !actionPassedOrAbsent(state, "product-review") {
		return fmt.Errorf("Product Review must pass before post-development review")
	}
	if !actionPassedOrAbsent(state, "start-readiness") {
		return fmt.Errorf("Start Readiness must pass before post-development review")
	}
	return nil
}

func requireCarryTransition(state RunState) error {
	if !hasDevelopmentSnapshot(state) || state.PreRepairSnapshot == "" {
		return fmt.Errorf("a repaired immutable snapshot is required before Carry")
	}
	return nil
}

func requireSealTransition(state RunState) error {
	// 轻量路线整体豁免 seal 门：轻量 run 从 start 即声明，不做拆分决定、不选路线、
	// 不快照、不做任何验证，start → 需求登记 → Seal 三步直达，只留记录。
	if isLightweight(state) {
		return nil
	}
	if !hasDevelopmentSnapshot(state) {
		return fmt.Errorf("an immutable development snapshot is required before Seal")
	}
	if !actionPassedOrAbsent(state, "product-review") {
		return fmt.Errorf("Product Review must pass before Seal")
	}
	if !actionPassedOrAbsent(state, "start-readiness") {
		return fmt.Errorf("Start Readiness must pass before Seal")
	}
	return requireSelectedResultsResolved(state)
}

func developmentStarted(state RunState) bool {
	return state.Actions["development-worker"].Status != developmentPending
}

// isLightweight reports whether the run is on the lightweight route, declared at
// start via --route lightweight. Lightweight runs create the formal run record but
// perform no verification: no slicing decision, no QA/route selection, no snapshot,
// start → 需求登记 → Seal. The migration gates (route-before-split,
// routeRequiresConfirmation, development snapshot, product-review /
// start-readiness PASS at seal) are waived for lightweight runs.
func isLightweight(state RunState) bool {
	return state.RouteMode == "lightweight"
}

func hasDevelopmentSnapshot(state RunState) bool {
	status := state.Actions["development-worker"].Status
	return status == developmentComplete || status == developmentVerified
}

func semanticResultRecorded(status, snapshot, currentSnapshot string) bool {
	return snapshot == currentSnapshot && (status == "PASS" || status == "FAIL")
}

// comparedRange is the snapshot pair a gate review must report: every gate
// review covers the complete base-to-current delivery, including reruns.
func comparedRange(state RunState) string {
	return state.BaseSnapshot + ".." + state.CurrentSnapshot
}

// gatePromptChanged reports whether a gate's composed prompt (the shared
// reviewer base plus the gate's own content) hash moved since the run started,
// making a recorded authoritative result a candidate for the main agent's
// per-gate judgment rather than a fixed conclusion. A base-only change therefore
// enables per-gate re-dispatch too.
func gatePromptChanged(state RunState, catalog PromptCatalog, gateID string) bool {
	recorded, ok := state.PromptHashes["gate:"+gateID]
	if !ok {
		return false
	}
	gate, ok := catalog.Gate(gateID)
	if !ok {
		return false
	}
	return recorded != composedGatePromptHash(catalog, gate.Content)
}

// actionPromptChanged reports whether an action prompt content hash moved since
// the run recorded it, making a recorded authoritative result a candidate for
// the main agent's judgment. For injected reviewer actions the composed
// prompt carries the shared reviewer base, so the comparison includes the base
// (a base-only change moves every injected reviewer action's hash and triggers
// the inheritance judgment, symmetric with the gate path); non-reviewer actions
// hash only their own content.
func actionPromptChanged(state RunState, catalog PromptCatalog, actionID string) bool {
	recorded, ok := state.PromptHashes["action:"+actionID]
	if !ok {
		return false
	}
	action, ok := catalog.Action(actionID)
	if !ok {
		return false
	}
	return recorded != composedActionPromptHash(catalog, action)
}

// requireNoPendingInheritance is the hard gate behind every continue /
// rerun entry (prepare / claim / record / snapshot / seal / qa-* /
// authorize-repair) is rejected until every inheritance judgment is handled. A
// judgment is pending when an old-snapshot PASS result awaits a Carry decision,
// or a selected gate / action prompt moved while that target already has a
// recorded PASS/FAIL result and no main-agent Carry judgment was recorded for it
// yet (the mid-flight modification category; it only fires when a recorded
// result exists, so a pre-development run is never blocked by it). The
// disposition commands (carry / requirement / settle-findings) are exempt and
// route around this gate; requirement-artifact reclassification is enforced
// separately by requireCurrentDefinitions, which every entry already calls.
func requireNoPendingInheritance(root string, state RunState, catalog PromptCatalog) error {
	// 首个开发快照前没有继承边界：无开发快照时不存在"旧快照结果待判定"，且处置入口
	// （carry --main-agent 继承判定）依赖开发快照才能记录。首个快照只是记录交付成果、
	// 不是重跑/继续，跳过硬闸；快照之后（开发快照已存在）硬闸恢复，继承判定可正常记录。
	if !hasDevelopmentSnapshot(state) {
		return nil
	}
	if len(eligibleCarryGates(state)) != 0 {
		return fmt.Errorf("prior passing results at the pre-repair snapshot await a Carry decision; dispose them with `workflow carry --main-agent --main-reason '<reason>'` (or an independent carry) before continuing")
	}
	for id := range selectedSet(state) {
		if isQAMode(id) {
			continue
		}
		result := state.Gates[id]
		if !semanticResultRecorded(result.Status, result.Snapshot, state.CurrentSnapshot) {
			continue
		}
		if !gatePromptChanged(state, catalog, id) {
			continue
		}
		// FAIL 结果提示词变化时重派即处置：已记录 FAIL 的门可直接重派、
		// 重派覆盖旧 FAIL 即完成处置，不进入待决继承，也不需要主代理 Carry 判定；只有已
		// 记录 PASS 的结果才需要判定保留旧结论（INHERIT）还是重跑（RERUN）。
		if result.Status == "FAIL" {
			continue
		}
		// 主代理已对这条提示词变更记录过 Carry 判定（INHERIT / RERUN）时不再待决。
		if state.Carry[id].Decision != "" {
			continue
		}
		return fmt.Errorf("gate %q has a recorded PASS result whose prompt changed; record a main-agent Carry judgment (carry --main-agent) before continuing", id)
	}
	for _, action := range catalog.Actions {
		if !reviewerActionIDs[action.ID] {
			continue
		}
		result := state.Actions[action.ID]
		if result.Status != "PASS" && result.Status != "FAIL" {
			continue
		}
		if !actionPromptChanged(state, catalog, action.ID) {
			continue
		}
		// 与门一致（P1-1）：已记录 FAIL 的动作提示词变化时重派即处置，不进入待决继承。
		if result.Status == "FAIL" {
			continue
		}
		return fmt.Errorf("action %q has a recorded PASS result whose prompt changed; record a main-agent Carry judgment (carry --main-agent) before continuing", action.ID)
	}
	return nil
}

// requireResumeInterrupted enforces the three-branch resume rule for a
// claimed, un-resulted dispatch. When the recorded interruption reason is an
// objective transient API cause and every judgment condition is unchanged, the
// CLI forces a resume: it rejects a fresh prepare of the same target and directs
// the host to restore the original agent. When the conditions are unchanged but
// no objective reason is recorded (including "未知"), the CLI forces the user to
// decide (the main agent has no override power). Any change to snapshot,
// responsibility, task content, requirement, method, or intent — or a recorded
// non-objective interruption reason — lets the guard pass so a new dispatch is
// allowed; an explicit --user-requested authorization is handled by the caller.
func requireResumeInterrupted(root string, state RunState, catalog PromptCatalog, targetKind, target string, mode string) error {
	for id, dispatch := range state.Dispatches {
		if dispatch.TargetKind != targetKind || dispatch.Target != target || dispatch.Status != "CLAIMED" {
			continue
		}
		// qa-execution 按 mode 分流：不同 mode 的派发互不拦截，
		// 黑盒与白盒各自独立派发、并行执行。
		if target == "qa-execution" && dispatch.Mode != "" && mode != "" && dispatch.Mode != mode {
			continue
		}
		wantedSource := state.CurrentSnapshot
		if isBlackboxQADispatch(dispatch) {
			wantedSource = state.BaseSnapshot
		}
		if dispatch.SourceSnapshot != wantedSource {
			continue
		}
		unchanged, err := dispatchTaskUnchanged(root, state, catalog, dispatch)
		if err != nil {
			// 无法重算任务内容时不强制续用：放行新派发，避免死端。
			continue
		}
		if !unchanged {
			continue
		}
		// 读取 CLI 自动记录的中断原因；无 stop 事件或未记录时为空。
		reason, err := workflowLifecycle.InterruptionReason(root, state.RunID, dispatch.ID)
		if err != nil {
			// 无法读取原因时不强制续用：放行新派发，避免死端。
			continue
		}
		// 已记录的非客观原因（如用户主动中断、max_turns 正常结束）不受限：
		// 可开新派发。仅"未知"（宿主未提供原因）视同无原因、落入第三分支。
		if reason != "" && reason != "未知" && !isObjectiveInterruptionReason(reason) {
			continue
		}
		if isObjectiveInterruptionReason(reason) {
			// 第一分支：客观瞬时原因 + 一切未变 → 必续用。
			return fmt.Errorf("dispatch %q is claimed and interrupted by an objective transient API cause (recorded reason %q); every resume condition is unchanged, so resume the original agent (identity %q) to continue the same dispatch instead of preparing a new one", id, reason, dispatch.ReviewerIdentity)
		}
		// 第三分支：一切未变但无客观中断原因（含未记录与"未知"）→ 强制询问用户。
		return fmt.Errorf("dispatch %q is claimed and interrupted with an unchanged task and snapshot but no recorded objective interruption reason; the user must decide: resume the original agent (identity %q) to continue the same dispatch, or authorize a fresh dispatch with --user-requested", id, dispatch.ReviewerIdentity)
	}
	return nil
}

// isObjectiveInterruptionReason reports whether a recorded interruption reason is
// an objective transient API cause: HTTP error codes such as
// 429/402/500/502/503/504/529 and transient-overload phrasing. Non-objective
// reasons (user abort, max turns, normal end) are not objective, so lets
// a new dispatch open when the reason is recorded and non-objective.
func isObjectiveInterruptionReason(reason string) bool {
	lower := strings.ToLower(reason)
	for _, code := range []string{"429", "402", "500", "502", "503", "504", "529"} {
		if strings.Contains(lower, code) {
			return true
		}
	}
	for _, phrase := range []string{"rate limit", "rate_limit", "overloaded", "temporarily unavailable", "timeout", "server error", "too many requests"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// dispatchTaskUnchanged reports whether the current catalog and state would
// recompose the dispatch's exact task prompt. The recomposed prompt uses the
// dispatch's own id / attempt / review wave, so the [Dispatch] block is
// identical and only the task content (catalog, requirement, snapshot, detail)
// can move the hash.
func dispatchTaskUnchanged(root string, state RunState, catalog PromptCatalog, dispatch PreparedDispatch) (bool, error) {
	route := routeForState(root, state)
	if isBlackboxQADispatch(dispatch) {
		route.Worktree = absPath(state.QAWorktree)
		route.CurrentSnapshot = state.BaseSnapshot
	}
	route.DispatchID, route.DispatchAttempt, route.ReviewWave = dispatch.ID, dispatch.Attempt, dispatch.ReviewWave
	prompt, err := composeDispatchPrompt(state, catalog, route, dispatch)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:]) == dispatch.PromptHash, nil
}
