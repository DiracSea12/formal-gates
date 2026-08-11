package validate

import (
	"fmt"
	"sort"
	"strings"
)

// QACaseInput 是一次 qa-design 记录轮输入的用例。Test 是白盒用例对应的测试
// 引用 = "<文件路径>::<函数名>"：文件路径定位到交付测试代码所在文件、函数名定位到该文件
// 里的测试，两者都是不透明字符串、CLI 不解析代码内容。白盒设计者独立编写结构测试代码，
// 并在 CLI 记录用例时用 --test 指定；CLI 记录时只校验引用非空、且同一引用不被两条白盒
// 用例共用（一个测试实现一个用例），存在性与对应性由 qa-review（读代码核对）与
// qa-execution（实际运行）验证，不满足即拒绝记录。黑盒用例不需要 Test。
type QACaseInput struct{ Mode, Description, Procedure, Oracle, Test string }

type QAReviewInput struct{ CaseID, Outcome, Reason string }

type QAResultInput struct{ CaseID, Outcome, Procedure, Observation, OracleResult string }

// QAScopeInput carries one QA execution rerun scope decision bundled into an
// authorize-repair call: Mode is the dispatch mode (blackbox / whitebox /
// empty for the merged set), Decision is FULL or AFFECTED, CaseIDs is the host
// judged AFFECTED subset, and Reason traces the decision.
type QAScopeInput struct {
	Mode     string
	Decision string
	CaseIDs  []string
	Reason   string
}

// isQAMode reports whether the id is a built-in QA mode entry. Blackbox, whitebox,
// and merge QA share the run's QA case set and QA Execution result; discovered
// gate entries live in the per-gate result map instead. The legacy "qa" id is
// recognized so runs bound to an old catalog that listed QA as a gate still count
// as QA-selected after migration and share the QA Execution result.
func isQAMode(id string) bool {
	return id == blackboxQAID || id == whiteboxQAID || id == mergeQAID || id == legacyQAID
}

// isSelectedQA reports whether any QA mode is selected for this run.
func isSelectedQA(state RunState) bool {
	for id := range selectedSet(state) {
		if isQAMode(id) {
			return true
		}
	}
	return false
}

// isMergeVerification reports whether this run is a retained overall run whose
// split decision forces merge gate and merge QA as its only post-merge
// verification. The route is auto-determined as the merge route when the split
// decision is recorded, so this run does not go through normal route selection.
func isMergeVerification(state RunState) bool {
	return state.RetainedOverall && slicingRecorded(state) && state.Slicing.Decision == "split"
}

// slicingRecorded reports whether the run's formal split decision has been
// recorded. The decision is the binding point: once recorded it is not re-cut.
func slicingRecorded(state RunState) bool {
	return state.Slicing != nil && strings.TrimSpace(state.Slicing.Decision) != ""
}

// selectedQAModes lists the normal QA modes selected for the run, used to tell
// the QA Design agent which modes' cases it must design.
func selectedQAModes(state RunState) []string {
	var modes []string
	if isSelected(state, blackboxQAID) {
		modes = append(modes, "blackbox: real QA designed from the current confirmed requirement in the QA isolation worktree, executed against the built product on the main worktree after the snapshot")
	}
	if isSelected(state, whiteboxQAID) {
		modes = append(modes, "whitebox: structure test cases designed by reading the implementation after development")
	}
	return modes
}

// blackboxReviewPassed reports whether the blackbox review gate is satisfied for
// a snapshot: when blackbox QA is selected, the blackbox review must have
// approved the set, and the blackbox review result itself must not have failed.
// With blackbox cases present, every blackbox case must be approved (ReviewStatus
// PASS) by a blackbox review round AND the blackbox qa-review result must not be
// FAIL: a review round can judge every pending case PASS yet still fail the set
// with a coverage-omission finding (qa-review FAIL, qa-design re-opened), and
// that FAIL must block the snapshot just like a case-level FAIL. With zero
// blackbox cases the gate is NOT vacuous: the snapshot is allowed only when the
// blackbox qa-review result recorded PASS (the review judged the empty set's
// coverage sufficient, requirement 4) — a still-pending or FAIL review keeps
// blocking the snapshot (requirements 1 and 2: snapshot waits until development
// complete 且 blackbox review PASS). Runs without blackbox QA are vacuously
// passed. The only bypass is the user-authorized snapshot release
// (snapshotBlackboxReleased), handled by AdvanceSnapshot. 黑盒门的
// review 结果按 blackbox mode 独立读取，不读取另一 mode 的 review 判定。
func blackboxReviewPassed(state RunState) bool {
	if !isSelected(state, blackboxQAID) && !isSelected(state, legacyQAID) {
		return true
	}
	hasBlackboxCases := false
	// 黑盒用例按 mode 分开存储；从全量跨 mode 视图按 mode 过滤读取，兼容合并
	// 存储（用例在 "" 键）与按 mode 存储两种布局。
	for _, testCase := range state.qaModeCases("blackbox") {
		hasBlackboxCases = true
		if testCase.ReviewStatus != "PASS" {
			return false
		}
	}
	if !hasBlackboxCases {
		// 黑盒门读黑盒 mode 的 review 权威结果；legacy "qa" 合并态经回退取 "" 键。
		return state.qaReview("blackbox").Status == "PASS"
	}
	// 有黑盒用例时整个审查判定仍须通过：集合层面 P1 覆盖遗漏使审查动作 FAIL 时，即使
	// 各用例单独均 PASS 也阻挡快照；唯一绕过是用户授权放行（AdvanceSnapshot 处理）。
	if state.qaReview("blackbox").Status == "FAIL" {
		return false
	}
	return true
}

// snapshotBlackboxReleased reports whether the user has explicitly authorized a
// snapshot while the blackbox review gate is unmet. The authorization persists
// for the rest of the run (until a requirement invalidation clears it): once the
// user releases the gate, the unapproved blackbox cases are treated as PASS, so
// subsequent repair snapshots are not blocked again by the same unapproved cases.
func snapshotBlackboxReleased(state RunState) bool {
	return state.SnapshotOverride != nil
}

// discardUnmatchedQADesign reconciles a fast-path speculative QA design recorded
// before the route was confirmed against the now-confirmed route. The fast-path
// design is always blackbox behavior design, so it matches the confirmed route
// exactly when blackbox QA (or the legacy "qa" mode) is selected; whitebox
// structure cases are designed after development, so they do not apply at route
// confirmation. When the route omits blackbox QA, the parallel design is
// discarded (the documented fast-path tradeoff: a design for a route that does
// not include blackbox QA is abandoned) so the design is re-done against the
// confirmed route. An approved set (QA Review passed) is never discarded here.
func discardUnmatchedQADesign(state *RunState) {
	// 快速路径设计存于合并 "" 键，按 mode 读取与重置。
	if state.qaDesign("").Status != "PASS" || state.qaReview("").Status == "PASS" {
		return
	}
	if isSelected(*state, blackboxQAID) || isSelected(*state, legacyQAID) {
		return
	}
	// 废弃快速路径的投机黑盒设计时清空全部按 mode 存储的用例与执行结果。
	state.QACasesByMode = map[string][]QACase{}
	state.QAExecutionByMode = map[string]QAExecutionResult{}
	state.setQADesign("", ActionResult{Status: "PENDING"})
	state.setQAReview("", ActionResult{Status: "PENDING"})
	// 最终路线不含黑盒时设计废弃，黑盒隔离工作区随之移除（清空登记，host 重建时才需要）。
	state.QAWorktree = ""
}

// pendingQACases lists the pending cases (ReviewStatus != PASS) for a review
// dispatch, filtered to the dispatch's mode when one is set so blackbox and
// whitebox review rounds stay single-mode and do not decide each other's cases.
func pendingQACases(cases []QACase, mode string) []QACase {
	var pending []QACase
	for _, testCase := range cases {
		if testCase.ReviewStatus == "PASS" {
			continue
		}
		if mode != "" && testCase.Mode != mode {
			continue
		}
		pending = append(pending, testCase)
	}
	return pending
}

// qaExecutionRequiredCases is the case set QA Execution must cover. Normally the
// complete approved case set; after a user-authorized snapshot release of the
// blackbox gate, only the approved cases are executed and the unapproved blackbox
// cases are treated as PASS (the origin is recorded by the snapshot override).
// When the dispatch carries a mode, the required set is filtered to that mode so
// blackbox and whitebox execution each dispatch independently and run in parallel
// (执行按 mode 分流). An empty mode keeps the merged set for
// existing single-dispatch flows and merge QA.
func qaExecutionRequiredCases(state RunState, mode string) []QACase {
	// 重跑按 scope 决策分流需执行集。AFFECTED（且 BaseSnapshot 匹配本次重跑
	// 的继承来源）时，需执行集 = 记录的受影响子集（派发前由 host 综合判定定死，
	// ）；否则（FULL / 首次执行）为全部已批准用例（按 mode 过滤，现状不变）。
	// 按 mode 读取该 mode 的需执行用例（从跨 mode 视图按 mode 过滤，兼容合并与
	// 按 mode 存储两种布局）。
	modeCases := state.qaModeCases(mode)
	if sc, ok := qaExecutionAffectedScope(state, mode); ok {
		inSubset := map[string]bool{}
		for _, id := range sc.CaseIDs {
			inSubset[id] = true
		}
		subset := []QACase{}
		for _, testCase := range modeCases {
			if inSubset[testCase.ID] {
				subset = append(subset, testCase)
			}
		}
		return subset
	}
	if !snapshotBlackboxReleased(state) {
		return modeCases
	}
	approved := []QACase{}
	for _, testCase := range modeCases {
		if testCase.ReviewStatus == "PASS" {
			approved = append(approved, testCase)
		}
	}
	return approved
}

// filterQACasesByMode returns only the cases whose mode matches the dispatch mode;
// an empty mode returns every case unchanged.
func filterQACasesByMode(cases []QACase, mode string) []QACase {
	if mode == "" {
		return cases
	}
	filtered := []QACase{}
	for _, testCase := range cases {
		if testCase.Mode == mode {
			filtered = append(filtered, testCase)
		}
	}
	return filtered
}

// qaExecutionModeResulted reports whether the run already has an authoritative QA
// Execution result for the dispatch mode at the current snapshot (full
// decoupling): blackbox and whitebox each record independently into their own
// per-mode result, and the merged empty mode holds the single merged-set result.
// An authoritative result is PASS / FAIL / RUNTIME_ERROR recorded at the current
// snapshot.
func qaExecutionModeResulted(state RunState, mode string) bool {
	result := state.qaExecution(mode)
	if result.Snapshot != state.CurrentSnapshot {
		return false
	}
	return result.Status == "PASS" || result.Status == "FAIL" || result.Status == "RUNTIME_ERROR"
}

// qaResultHasMode reports whether the given QA execution result holds at least one
// case record for the dispatch mode. Every record carries its concrete mode, so the
// merged (empty) mode never matches a record; callers check the empty mode by the
// result's authoritative status directly.
func qaResultHasMode(result QAExecutionResult, mode string) bool {
	for _, record := range result.Cases {
		if record.Mode == mode {
			return true
		}
	}
	return false
}

// priorAuthoritativeQA returns the dispatch mode's prior authoritative QA execution
// result (PASS or FAIL) whose snapshot differs from the current one — the result a
// rerun of that mode inherits from (full decoupling). It prefers
// the mode's preserved current result (e.g. a PASS kept across a repair snapshot
// advance), then the mode's PriorQAExecutionByMode entry. RUNTIME_ERROR is never
// authoritative.
func priorAuthoritativeQA(state RunState, mode string) (QAExecutionResult, bool) {
	current := state.qaExecution(mode)
	if (current.Status == "PASS" || current.Status == "FAIL") && current.Snapshot != "" && current.Snapshot != state.CurrentSnapshot {
		return current, true
	}
	if prior := state.priorQAExecution(mode); prior != nil && (prior.Status == "PASS" || prior.Status == "FAIL") {
		return *prior, true
	}
	return QAExecutionResult{}, false
}

// qaExecutionPriorResultedBase returns the snapshot of the prior authoritative QA
// execution result a rerun of the mode inherits from — the BaseSnapshot the rerun
// scope decision must match. ok is false when there is no prior
// authoritative result: the mode's first execution, which requires no scope.
func qaExecutionPriorResultedBase(state RunState, mode string) (string, bool) {
	result, ok := priorAuthoritativeQA(state, mode)
	if !ok {
		return "", false
	}
	return result.Snapshot, true
}

// qaExecutionRerunSource returns the authoritative QA execution result a rerun of
// the mode inherits from, together with its snapshot. For the prepare path this is
// the prior authoritative result at an earlier snapshot. At the repair-limit path
// (authorize-repair before the repair snapshot advance) the current authoritative
// FAIL result at the current snapshot is itself the inheritance source — after the
// advance it is preserved to the mode's PriorQAExecutionByMode and becomes the
// "上一轮".
func qaExecutionRerunSource(state RunState, mode string) (QAExecutionResult, string, bool) {
	if prior, ok := priorAuthoritativeQA(state, mode); ok {
		return prior, prior.Snapshot, true
	}
	current := state.qaExecution(mode)
	if current.Status == "FAIL" && current.Snapshot == state.CurrentSnapshot {
		return current, state.CurrentSnapshot, true
	}
	return QAExecutionResult{}, "", false
}

// qaOverallResult aggregates the per-mode QA execution results into a single view
// for display / summary purposes (full decoupling). The merged (empty-mode)
// result is returned as-is when the run uses merged storage; otherwise the concrete
// modes' results are combined (union of cases and findings) with FAIL winning over
// RUNTIME_ERROR over PASS, and the snapshot taken from the recorded modes.
func qaOverallResult(state RunState) QAExecutionResult {
	if merged := state.qaExecution(""); merged.Status != "" || merged.Snapshot != "" || len(merged.Cases) != 0 {
		return merged
	}
	var cases []QAResultRecord
	var findings []Finding
	status := "PENDING"
	snapshot := state.CurrentSnapshot
	haveRecorded := false
	for _, mode := range []string{"blackbox", "whitebox"} {
		result := state.qaExecution(mode)
		if result.Status == "" {
			continue
		}
		haveRecorded = true
		cases = append(cases, result.Cases...)
		findings = append(findings, result.Findings...)
		switch {
		case result.Status == "FAIL":
			status = "FAIL"
		case status != "FAIL" && result.Status == "RUNTIME_ERROR":
			status = "RUNTIME_ERROR"
		case status != "FAIL" && status != "RUNTIME_ERROR" && result.Status == "PASS":
			status = "PASS"
		}
		if result.Snapshot != "" {
			snapshot = result.Snapshot
		}
	}
	if !haveRecorded {
		return QAExecutionResult{Status: "PENDING"}
	}
	return QAExecutionResult{Status: status, Snapshot: snapshot, Cases: cases, Findings: findings}
}

// qaExecutionAffectedScope returns the recorded AFFECTED scope decision that applies
// to the current rerun of the mode: Decision AFFECTED and BaseSnapshot matching the
// prior authoritative result this rerun inherits from. ok is false when the run
// is not under an AFFECTED rerun scope (FULL decision or first execution).
func qaExecutionAffectedScope(state RunState, mode string) (QAExecutionScope, bool) {
	sc, ok := state.ExecutionScopes[mode]
	if !ok || sc.Decision != "AFFECTED" {
		return QAExecutionScope{}, false
	}
	base, has := qaExecutionPriorResultedBase(state, mode)
	if !has || sc.BaseSnapshot != base {
		return QAExecutionScope{}, false
	}
	return sc, true
}

// qaModeLabel renders a dispatch mode for user-facing messages: the merged (empty)
// mode is shown as "merged".
func qaModeLabel(mode string) string {
	if mode == "" {
		return "merged"
	}
	return mode
}

// qaReviewDispatchPrepared reports whether a qa-review dispatch for the given mode
// is prepared (OPEN or CLAIMED) and not yet completed — the design lock point
// (full decoupling): once a review dispatch for a mode is prepared,
// that mode's QA case set is frozen until the review returns, so further qa-design
// re-records for that mode are rejected. The lock is per mode: a blackbox review in
// flight does not lock the whitebox design (and vice versa); an empty mode matches
// merged / fast-path reviews. After the review records PASS/FAIL (or is still
// PENDING with no dispatch), the design is unlocked again.
func qaReviewDispatchPrepared(state RunState, mode string) bool {
	for _, dispatch := range state.Dispatches {
		if dispatch.TargetKind != "action" || dispatch.Target != "qa-review" || (dispatch.Status != "OPEN" && dispatch.Status != "CLAIMED") {
			continue
		}
		if dispatch.Mode == mode {
			return true
		}
	}
	return false
}

func formatQACase(testCase QACase, includeReview bool) string {
	value := fmt.Sprintf("%s\nmode: %s\ndescription: %s\nprocedure: %s\noracle: %s", testCase.ID, testCase.Mode, testCase.Description, testCase.Procedure, testCase.Oracle)
	// 白盒用例带测试引用（<文件>::<函数>）绑定，展示给审查/执行代理，使执行按用例
	// 对应的测试跑；文档自包含——读文档即知该测试在哪个文件、叫什么。
	if strings.TrimSpace(testCase.Test) != "" {
		value += "\ntest: " + testCase.Test
	}
	if includeReview {
		value += "\nreview status: " + testCase.ReviewStatus
	}
	return value
}

func RecordQADesign(root, packageRoot, runID, dispatchID string, cases []QACaseInput, runtimeError string) (RunState, error) {
	return openDispatchRecord(root, packageRoot, runID, dispatchID, recordDispatchOptions{
		targetKind:             "action",
		target:                 "qa-design",
		transitionOp:           "qa-design",
		transitionTarget:       func(dispatch PreparedDispatch) string { return dispatch.Mode },
		requireIsolationNative: true,
		requireLifecycle:       true,
	}, func(state *RunState, catalog PromptCatalog, dispatch PreparedDispatch) error {
		backfillDispatchCost(root, state, dispatch)
		if strings.TrimSpace(runtimeError) != "" {
			if len(cases) != 0 {
				return fmt.Errorf("QA Design runtime error cannot include cases")
			}
			// 只重置本派发 mode 的执行结果，另一 mode 的执行结果不受影响。
			state.setQAExecution(dispatch.Mode, QAExecutionResult{Status: "PENDING"})
			// qa-design 权威结果按 mode 独立记录。
			state.setQADesign(dispatch.Mode, ActionResult{Status: "RUNTIME_ERROR", Message: strings.TrimSpace(runtimeError), DispatchID: dispatch.ID})
			completeDispatch(state, dispatch.ID)
			return nil
		}
		// 空用例集不再被拒绝：被选中模式零用例是覆盖缺失，由 qa-review 的 set-level 覆盖
		// 判定承担（P1、阻塞），不设机械化质量下限。合并 QA 的零用例既有例外保留。空设计
		// 记录 PASS 后进入 QA Review（无待定用例，只做集合覆盖判定）。
		if len(cases) == 0 {
			message := ""
			if isMergeVerification(*state) {
				// 合并 QA 的用例集可为零/极少：留痕注明"切片基本独立、无跨切片交互用例"。
				message = "切片基本独立、无跨切片交互用例"
			}
			// 只清空本派发 mode 的用例列表与执行结果，不触碰另一 mode 的既有用例/结果。
			state.setQACases(dispatch.Mode, []QACase{})
			state.setQAExecution(dispatch.Mode, QAExecutionResult{Status: "PENDING"})
			// 设计 PASS 记录到本 mode，并把本 mode 的 review 重置为 PENDING（不触碰
			// 另一 mode 的 review 判定）。
			state.setQADesign(dispatch.Mode, ActionResult{Status: "PASS", Message: message, DispatchID: dispatch.ID})
			state.setQAReview(dispatch.Mode, ActionResult{Status: "PENDING"})
			completeDispatch(state, dispatch.ID)
			return nil
		}
		// 设计轮只对本派发 mode 的用例列表做增量替换——从该 mode 自己的存储列表取
		// 既有用例（保留已批准用例、增量补全），另一 mode 的列表保持不动。
		priorCases := state.qaCases(dispatch.Mode)
		seen := map[string]bool{}
		priorByKey := map[string]QACase{}
		// 用例 ID 跨所有 mode 全局唯一（执行记录 / AFFECTED 子集按 ID 索引）：新 ID 生成
		// 时保留全部 mode 已占用的 ID，避免不同 mode 的用例撞号；priorByKey 只从本 mode
		// 的既有用例取，保证只复用/保留本 mode 的已批准用例。
		usedIDs := map[string]bool{}
		for _, prior := range state.allQACases() {
			usedIDs[prior.ID] = true
		}
		for _, prior := range priorCases {
			priorByKey[qaCaseSemanticKeyWithTest(prior.Mode, prior.Description, prior.Procedure, prior.Oracle, prior.Test)] = prior
		}
		nextID := 1
		updated := make([]QACase, 0, len(cases))
		for index, item := range cases {
			normalized := QACase{
				Mode:        strings.ToLower(strings.TrimSpace(item.Mode)),
				Description: strings.TrimSpace(item.Description),
				Procedure:   strings.TrimSpace(item.Procedure),
				Oracle:      strings.TrimSpace(item.Oracle),
				Test:        strings.TrimSpace(item.Test),
			}
			if normalized.Mode != "blackbox" && normalized.Mode != "whitebox" {
				return fmt.Errorf("QA case %d mode must be blackbox or whitebox", index+1)
			}
			// 按 mode 派发的设计轮只记录本 mode 的用例，防止把别的 mode 的用例写进
			// 本 mode 的列表。
			if dispatch.Mode != "" && normalized.Mode != dispatch.Mode {
				return fmt.Errorf("QA case %d mode %q does not match the %s design dispatch", index+1, normalized.Mode, dispatch.Mode)
			}
			for name, value := range map[string]string{"description": normalized.Description, "procedure": normalized.Procedure, "oracle": normalized.Oracle} {
				if value == "" {
					return fmt.Errorf("QA case %d %s is required", index+1, name)
				}
			}
			// caseId↔测试绑定：白盒用例必须写明实现该用例的测试引用（Test 字段 =
			// "<文件路径>::<函数名>"，两个不透明字符串，CLI 不解析代码内容）。CLI 记录时只做
			// 最小校验：引用非空、且同一引用不被两条白盒用例共用（一个测试实现一个用例）；
			// 存在性与对应性由 qa-review（读代码核对）与 qa-execution（实际运行）验证。
			// 黑盒用例无结构测试绑定、不要求 Test。不满足即拒绝记录。
			if normalized.Mode == "whitebox" {
				if normalized.Test == "" {
					return fmt.Errorf("QA case %d (whitebox) requires a --test <file>::<function> reference locating the whitebox designer's delivered test", index+1)
				}
			}
			key := qaCaseSemanticKeyWithTest(normalized.Mode, normalized.Description, normalized.Procedure, normalized.Oracle, normalized.Test)
			if seen[key] {
				return fmt.Errorf("duplicate QA case %d", index+1)
			}
			seen[key] = true
			if prior, ok := priorByKey[key]; ok {
				normalized.ID = prior.ID
				if prior.ReviewStatus == "PASS" {
					normalized.ReviewStatus = "PASS"
				} else {
					normalized.ReviewStatus = "PENDING"
				}
			} else {
				for usedIDs[fmt.Sprintf("CASE-%03d", nextID)] {
					nextID++
				}
				normalized.ID, normalized.ReviewStatus = fmt.Sprintf("CASE-%03d", nextID), "PENDING"
				usedIDs[normalized.ID] = true
				nextID++
			}
			updated = append(updated, normalized)
		}
		// 1:1：同一测试引用（<文件>::<函数>）不能被两条白盒用例共用——一个测试实现
		// 一个用例。对本次记录后的完整用例集检查（含保留的既有用例），撞引用即拒绝记录。
		// 黑盒用例无测试引用，不参与。
		refOwner := map[string]string{}
		for _, testCase := range updated {
			if testCase.Mode != "whitebox" {
				continue
			}
			ref := strings.TrimSpace(testCase.Test)
			if ref == "" {
				continue
			}
			if owner, dup := refOwner[ref]; dup {
				return fmt.Errorf("QA whitebox test reference %q is shared by case %s and case %s; one test implements one case", ref, owner, testCase.ID)
			}
			refOwner[ref] = testCase.ID
		}
		// 返工约束只按本派发 mode 的 review FAIL 检查，不读另一 mode 的 review 判定。
		if state.qaReview(dispatch.Mode).Status == "FAIL" {
			pending := false
			for _, testCase := range updated {
				if testCase.ReviewStatus != "PASS" {
					pending = true
					break
				}
			}
			if !pending {
				if len(updated) == len(priorCases) {
					return fmt.Errorf("QA Design rework must add or revise a case, or remove an obsolete or duplicated case")
				}
			}
		}
		state.setQACases(dispatch.Mode, updated)
		// 设计轮只重置本派发 mode 的执行结果——白盒设计不清黑盒已记录的执行结果。
		state.setQAExecution(dispatch.Mode, QAExecutionResult{Status: "PENDING"})
		// 设计 PASS 记录到本 mode、本 mode 的 review 重置为 PENDING，另一 mode 不动。
		state.setQADesign(dispatch.Mode, ActionResult{Status: "PASS", DispatchID: dispatch.ID})
		state.setQAReview(dispatch.Mode, ActionResult{Status: "PENDING"})
		completeDispatch(state, dispatch.ID)
		return nil
	})
}

func RecordQAReview(root, packageRoot, runID, dispatchID string, decisions []QAReviewInput, runtimeError string, setFindings []FindingInput) (RunState, error) {
	return openDispatchRecord(root, packageRoot, runID, dispatchID, recordDispatchOptions{
		targetKind:             "action",
		target:                 "qa-review",
		transitionOp:           "qa-review",
		transitionTarget:       func(dispatch PreparedDispatch) string { return dispatch.Mode },
		requireIsolationNative: true,
		requireLifecycle:       true,
	}, func(state *RunState, catalog PromptCatalog, dispatch PreparedDispatch) error {
		backfillDispatchCost(root, state, dispatch)
		if strings.TrimSpace(runtimeError) != "" {
			if len(decisions) != 0 || len(setFindings) != 0 {
				return fmt.Errorf("QA Review runtime error cannot include case decisions or findings")
			}
			// review RUNTIME_ERROR 按 mode 独立记录。
			state.setQAReview(dispatch.Mode, ActionResult{Status: "RUNTIME_ERROR", Message: strings.TrimSpace(runtimeError), DispatchID: dispatch.ID})
			completeDispatch(state, dispatch.ID)
			return nil
		}
		// 待定用例集按派发 mode 限定：黑盒 review 只决定黑盒待定用例、白盒 review 只决定
		// 白盒待定用例，各派发为单 mode、不混合。mode 为空（快速路径/合并 QA）时覆盖全部
		// 待定用例。被选中模式零用例时待定集为空，只做集合覆盖判定。与提示词组装
		// 用同一读取视图（per-mode 键非空即用、否则回退合并 "" 键），保证 recorder 的
		// pending 集与提示词列出的一致；决定后写回同一存储键。
		storageKey, modeCases := state.qaModeCasesWithKey(dispatch.Mode)
		pending := map[string]int{}
		for index, testCase := range modeCases {
			if testCase.ReviewStatus == "PASS" {
				continue
			}
			pending[testCase.ID] = index
		}
		if len(decisions) != len(pending) {
			return fmt.Errorf("QA Review must decide all %d pending cases", len(pending))
		}
		seen := map[string]bool{}
		findings := make([]Finding, 0, len(decisions)+len(setFindings))
		status := "PASS"
		for _, input := range decisions {
			caseID := strings.TrimSpace(input.CaseID)
			index, ok := pending[caseID]
			if !ok {
				return fmt.Errorf("QA Review case %q is not pending in this dispatch", input.CaseID)
			}
			if seen[caseID] {
				return fmt.Errorf("duplicate QA Review decision for %s", caseID)
			}
			seen[caseID] = true
			outcome := strings.ToUpper(strings.TrimSpace(input.Outcome))
			if outcome != "PASS" && outcome != "FAIL" {
				return fmt.Errorf("QA Review outcome for %s must be PASS or FAIL", caseID)
			}
			reason := strings.TrimSpace(input.Reason)
			if outcome == "FAIL" && reason == "" {
				return fmt.Errorf("QA Review FAIL for %s requires a reason", caseID)
			}
			modeCases[index].ReviewStatus = outcome
			if outcome == "FAIL" {
				status = "FAIL"
				findings = append(findings, Finding{Message: caseID + ": " + reason})
			}
		}
		// 集合层面发现项按严重度分类：覆盖遗漏（用例集未覆盖需求验收点/被选中模式）判 P1、
		// 阻塞、必须补用例；P2 仅为建议、不阻塞、不需处置。P0 不接受（集合层面不判终态致命）。
		for _, input := range setFindings {
			severity := strings.ToUpper(strings.TrimSpace(input.Severity))
			if severity != "P1" && severity != "P2" {
				return fmt.Errorf("QA Review set finding severity must be P1 or P2")
			}
			if strings.TrimSpace(input.Message) == "" {
				return fmt.Errorf("QA Review finding message is required")
			}
			locations := make([]string, 0, len(input.Locations))
			for _, location := range input.Locations {
				if err := validateFindingLocation(location); err != nil {
					return err
				}
				locations = append(locations, strings.TrimSpace(location))
			}
			findings = append(findings, Finding{Severity: severity, Message: strings.TrimSpace(input.Message), Locations: locations})
			if severity == "P1" {
				status = "FAIL"
			}
		}
		// review 权威结果按 mode 独立记录；FAIL 只把本 mode 的设计重置为 PENDING，
		// 另一 mode 的设计/执行/审查判定不受影响。
		state.setQAReview(dispatch.Mode, ActionResult{Status: status, Findings: findings, DispatchID: dispatch.ID})
		if status == "FAIL" {
			state.setQADesign(dispatch.Mode, ActionResult{Status: "PENDING"})
			// review FAIL 只重置本派发 mode 的执行结果，另一 mode 的结果不受影响。
			state.setQAExecution(dispatch.Mode, QAExecutionResult{Status: "PENDING"})
			// 黑盒 review 连续 FAIL 计数：PASS 清零、RUNTIME_ERROR 不计入也不打断连续。
			if dispatch.Mode == "blackbox" {
				state.BlackboxReviewFails++
			}
		} else if dispatch.Mode == "blackbox" {
			// 黑盒 qa-review PASS 自动清空隔离工作区、清零连续 FAIL 计数。
			state.QAWorktree = ""
			state.BlackboxReviewFails = 0
		}
		// 把决定的审查状态写回读取时同一存储键（per-mode 键或合并 "" 回退）。
		state.setQACasesForReview(dispatch.Mode, storageKey, modeCases)
		completeDispatch(state, dispatch.ID)
		return nil
	})
}

func RecordQAExecution(root, packageRoot, runID, dispatchID string, results []QAResultInput, runtimeError string) (RunState, error) {
	return openDispatchRecord(root, packageRoot, runID, dispatchID, recordDispatchOptions{
		targetKind:        "action",
		target:            "qa-execution",
		transitionOp:      "qa-execution",
		transitionTarget:  func(dispatch PreparedDispatch) string { return dispatch.Mode },
		requireMainNative: true,
		requireLifecycle:  true,
	}, func(state *RunState, catalog PromptCatalog, dispatch PreparedDispatch) error {
		backfillDispatchCost(root, state, dispatch)
		// 按派发 mode 分流，黑盒/白盒各自独立派发、并行执行——同一
		// snapshot 下每个 mode 各记录一次，同 mode 已出权威结果时才挡后续同 mode 派发
		// （按 mode 独立）。
		if qaExecutionModeResulted(*state, dispatch.Mode) {
			return fmt.Errorf("QA Execution already has an authoritative %s result for this mode", state.qaExecution(dispatch.Mode).Status)
		}
		if strings.TrimSpace(runtimeError) != "" {
			if len(results) != 0 {
				return fmt.Errorf("QA runtime error cannot include case results")
			}
			// 按 mode 分开记录，只影响本派发 mode。
			state.setQAExecution(dispatch.Mode, QAExecutionResult{Status: "RUNTIME_ERROR", Message: strings.TrimSpace(runtimeError), Snapshot: state.CurrentSnapshot})
			// RUNTIME_ERROR 不是权威结果，不得驱逐存续的上一轮结果（P2-乙确认）：下一轮
			// 重跑的 base 仍取本 mode 的 PriorQAExecution，所以这里不清空本 mode 的 prior。
			completeDispatch(state, dispatch.ID)
			completeReviewWaveIfReady(state)
			return nil
		}
		// 需执行集：正常流程为完整用例集；快照黑盒门经用户授权放行后只覆盖已批准用例，
		// 未批准的（黑盒）用例不计入需执行集、验证状态视为 PASS（授权来源由快照放行记录）。
		// 按派发 mode 过滤：黑盒/白盒各自独立派发、并行执行时，每次
		// 记录只覆盖该派发对应 mode 的需执行集。
		required := qaExecutionRequiredCases(*state, dispatch.Mode)
		// 空需执行集放行：除合并 QA 零用例既有例外外，本派发 mode 的 qa-review 已记录 PASS
		// （空集 review 判定覆盖充分，需求 4，与快照门 blackboxReviewPassed 的空集语义一致）
		// 时同样放行——零用例场景下 review 判定覆盖充分后 QA 执行对空集直接 PASS，避免 run
		// 卡死在 QA 执行、无法 seal。review 仍 PENDING 或 FAIL 时空集不被放行。按
		// 本派发 mode 的 review 结果判定，不读另一 mode。
		if len(required) == 0 && !isMergeVerification(*state) && !snapshotBlackboxReleased(*state) && state.qaReview(dispatch.Mode).Status != "PASS" {
			return fmt.Errorf("approved QA cases are missing")
		}
		if len(results) != len(required) {
			return fmt.Errorf("QA execution must cover all %d approved cases", len(required))
		}
		byID := map[string]QAResultInput{}
		for _, item := range results {
			if _, exists := byID[item.CaseID]; exists {
				return fmt.Errorf("duplicate QA result for %s", item.CaseID)
			}
			if item.Outcome != "PASS" && item.Outcome != "FAIL" {
				return fmt.Errorf("QA outcome for %s must be PASS or FAIL", item.CaseID)
			}
			for name, value := range map[string]string{"procedure": item.Procedure, "observation": item.Observation, "oracle result": item.OracleResult} {
				if strings.TrimSpace(value) == "" {
					return fmt.Errorf("QA result %s %s is required", item.CaseID, name)
				}
			}
			byID[item.CaseID] = item
		}
		// 执行结果按 mode 分开存储——每个 mode 的结果只含本 mode 的用例记录，黑盒/
		// 白盒各自独立、互不累计（另一 mode 的结果在 QAExecutionByModel[otherMode] 原样保持）。
		// 同一 mode 的重复记录由 qaExecutionModeResulted 挡在入口。
		recorded := make([]QAResultRecord, 0, len(required))
		for _, testCase := range required {
			item, ok := byID[testCase.ID]
			if !ok {
				return fmt.Errorf("QA result is missing for %s", testCase.ID)
			}
			recorded = append(recorded, QAResultRecord{CaseID: item.CaseID, Mode: testCase.Mode, Outcome: item.Outcome, Procedure: strings.TrimSpace(item.Procedure), Observation: strings.TrimSpace(item.Observation), OracleResult: strings.TrimSpace(item.OracleResult), Origin: "executed"})
		}
		// 快照放行后未获批准的黑盒用例：经用户授权跳过、验证状态视为 PASS（记录授权来源）。
		// 仅黑盒或合并（空 mode）派发补记这些跳过；白盒派发不补记黑盒用例。
		if snapshotBlackboxReleased(*state) && (dispatch.Mode == "" || dispatch.Mode == "blackbox") {
			for _, testCase := range state.qaModeCases("blackbox") {
				if testCase.ReviewStatus == "PASS" {
					continue
				}
				recorded = append(recorded, QAResultRecord{CaseID: testCase.ID, Mode: testCase.Mode, Outcome: "PASS", Procedure: "skipped", Observation: "authorized skip (user snapshot release)", OracleResult: "authorized PASS", Origin: "executed"})
			}
		}
		// AFFECTED 重跑下未覆盖的已批准用例继承上一轮 PASS——追加 inherited 记录
		// （恒 PASS、不参与 FAIL 聚合），观察记录继承来源快照，供审计与聚合区分。
		// 继承范围是该派发 mode 自己的用例。
		if sc, ok := qaExecutionAffectedScope(*state, dispatch.Mode); ok {
			covered := map[string]bool{}
			for _, record := range recorded {
				covered[record.CaseID] = true
			}
			for _, testCase := range state.qaModeCases(dispatch.Mode) {
				if testCase.ReviewStatus != "PASS" || covered[testCase.ID] {
					continue
				}
				recorded = append(recorded, QAResultRecord{CaseID: testCase.ID, Mode: testCase.Mode, Outcome: "PASS", Procedure: "inherited", Observation: "inherited PASS from " + sc.BaseSnapshot, OracleResult: "inherited", Origin: "inherited"})
			}
		}
		// 本派发 mode 的状态与发现项：该 mode 任一【经执行】用例 FAIL 即该 mode FAIL（
		// 按 mode 独立）；发现项从本 mode 的用例集重新生成，保证返修输入保留各 mode 的失败
		// 原因。继承用例恒 PASS、不参与 FAIL 判定。
		status := "PASS"
		findings := []Finding{}
		for _, existing := range recorded {
			if existing.Origin == "inherited" || existing.Outcome != "FAIL" {
				continue
			}
			status = "FAIL"
			findings = append(findings, Finding{Message: existing.CaseID + ": " + existing.Observation})
		}
		message := ""
		if isMergeVerification(*state) && len(state.allQACases()) == 0 {
			message = "切片基本独立、无跨切片交互用例"
		}
		state.setQAExecution(dispatch.Mode, QAExecutionResult{Status: status, Snapshot: state.CurrentSnapshot, Cases: recorded, Findings: findings, Message: message})
		// 本轮权威结果（PASS/FAIL）记录时只取代本 mode 存续的上一轮结果（
		// 一个 mode 记录 SHALL NOT 清空另一 mode 的 PriorQAExecution）；RUNTIME_ERROR 分支
		// 在上面提前返回、不清空本 mode 的 prior。
		state.deletePriorQAExecution(dispatch.Mode)
		completeDispatch(state, dispatch.ID)
		completeReviewWaveIfReady(state)
		return nil
	})
}

// qaRerunModes returns the QA dispatch modes that have an authoritative FAIL result
// at the current snapshot and will therefore be rerun after the next repair snapshot
// Blackbox and whitebox each need their own scope decision; merge QA and
// the legacy "qa" id use the merged empty mode. A mode with no recorded cases has
// nothing to rerun and needs no scope decision.
func qaRerunModes(state RunState) []string {
	var modes []string
	if !isSelectedQA(state) {
		return modes
	}
	// 合并单派发流程（合并 "" 结果权威）下整个合并集重跑，只需一个合并 scope。
	if qaUsesMergedExecution(state) {
		if state.qaExecution("").Status == "FAIL" {
			return []string{""}
		}
		return modes
	}
	// 任一选中 mode 当前快照有权威 FAIL 结果即触发打包（该 mode 将重跑）；此时
	// 所有在当前快照有权威结果的选中 mode（FAIL 或 PASS）都会在修复快照推进后重跑（PASS
	// 结果成为上一轮），因此都需要 scope 决策。
	anyFail := false
	for id := range selectedSet(state) {
		if !isQAMode(id) {
			continue
		}
		if result := state.qaExecution(qaDispatchMode(id)); result.Status == "FAIL" && result.Snapshot == state.CurrentSnapshot {
			anyFail = true
			break
		}
	}
	if !anyFail {
		return modes
	}
	seen := map[string]bool{}
	for id := range selectedSet(state) {
		if !isQAMode(id) {
			continue
		}
		mode := qaDispatchMode(id)
		result := state.qaExecution(mode)
		if result.Snapshot != state.CurrentSnapshot || result.Status == "" || result.Status == "PENDING" {
			continue
		}
		if !seen[mode] {
			modes = append(modes, mode)
			seen[mode] = true
		}
	}
	sort.Strings(modes)
	return modes
}

// bundleRerunScopes enforces the limit-point scope bundling: when QA is
// selected and the current snapshot holds an authoritative FAIL result for a
// dispatch mode (that mode will be rerun), the mode must carry a scope decision
// covering the current snapshot. A pre-recorded scope wins; otherwise the inline
// authorize-repair scope input records it with Source AUTHORIZE_REPAIR, or, when the
// last recorded decision was a user-chosen AFFECTED (Source != CARRY_FORWARD),
// auto-carries it as CARRY_FORWARD with the host-judged subset without asking the
// user again. Each such authorization still grants exactly one extra
// review wave.
func bundleRerunScopes(state *RunState, scopes []QAScopeInput) error {
	if !isSelectedQA(*state) {
		return nil
	}
	modes := qaRerunModes(*state)
	if len(modes) == 0 {
		return nil
	}
	byMode := map[string]QAScopeInput{}
	for _, input := range scopes {
		mode := strings.TrimSpace(input.Mode)
		if _, dup := byMode[mode]; dup {
			return fmt.Errorf("duplicate QA scope for mode %q", qaModeLabel(mode))
		}
		byMode[mode] = input
	}
	for _, mode := range modes {
		existing, ok := state.ExecutionScopes[mode]
		if ok && existing.BaseSnapshot == state.CurrentSnapshot {
			// 已有一份覆盖当前 FAIL 快照的 scope 决策，无需再记录。
			continue
		}
		input, has := byMode[mode]
		if !has {
			return fmt.Errorf("QA Execution rerun requires a scope decision for mode %q: run `workflow qa-execution-scope --mode %s --decision FULL|AFFECTED ...` or pass --qa-scope to this authorize-repair call", qaModeLabel(mode), mode)
		}
		source := scopeSourceAuthorizeRepair
		if ok && existing.Decision == "AFFECTED" && existing.Source != scopeSourceCarryForward && strings.TrimSpace(input.Decision) == "" {
			// 最近一次是用户主动选择 AFFECTED 且 host 未显式改选时自动沿用子集，
			// 不再询问"全量 vs 受影响"；子集扩展由 host 自行决定、不要求用户确认。产品审
			// P2：host 若显式给出新的 decision（如 --qa-scope <mode>=FULL 升级为全量），
			// 则以新 decision 为准，而不是被强制沿用 AFFECTED。
			source = scopeSourceCarryForward
			input.Decision = "AFFECTED"
		}
		// prior 按 mode 取该 mode 自己的权威结果。
		if err := recordExecutionScope(state, mode, input.Decision, input.CaseIDs, input.Reason, source, state.CurrentSnapshot, state.qaExecution(mode)); err != nil {
			return err
		}
	}
	return nil
}

// recordExecutionScope validates and records one QA execution scope decision for the
// mode. prior is the authoritative QA execution result this rerun
// inherits from — its FAIL cases are mandatory members of an AFFECTED subset;
// baseSnapshot is that result's snapshot. source records how the decision was
// captured: PREPARE for the standalone workflow qa-execution-scope command,
// AUTHORIZE_REPAIR or CARRY_FORWARD for the bundled authorize-repair path.
func recordExecutionScope(state *RunState, mode, decision string, caseIDs []string, reason, source, baseSnapshot string, prior QAExecutionResult) error {
	mode = strings.TrimSpace(mode)
	decision = strings.ToUpper(strings.TrimSpace(decision))
	if mode != "blackbox" && mode != "whitebox" && mode != "" {
		return fmt.Errorf("invalid QA execution scope mode %q (want blackbox, whitebox, or empty for the merged set)", mode)
	}
	if decision != "FULL" && decision != "AFFECTED" {
		return fmt.Errorf("QA execution scope decision must be FULL or AFFECTED")
	}
	normalized := []string{}
	seen := map[string]bool{}
	for _, raw := range caseIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if seen[id] {
			return fmt.Errorf("duplicate case %s in QA execution scope", id)
		}
		seen[id] = true
		normalized = append(normalized, id)
	}
	if decision == "AFFECTED" {
		if len(normalized) == 0 {
			return fmt.Errorf("AFFECTED QA execution scope requires a non-empty case subset")
		}
		// 子集必须是该 mode 已批准用例的子集，且包含继承来源（prior）中该 mode 的全部
		// FAIL 用例。受连带影响的既往通过用例由 host 综合判定、自行扩展子集，CLI 只做
		// 机械校验、不要求用户逐项确认。
		// 子集校验按该 mode 自己的用例读取（跨 mode 视图按 mode 过滤）。
		approved := map[string]bool{}
		for _, testCase := range state.qaModeCases(mode) {
			if testCase.ReviewStatus == "PASS" {
				approved[testCase.ID] = true
			}
		}
		for _, id := range normalized {
			if !approved[id] {
				return fmt.Errorf("case %s is not an approved %s QA case", id, qaModeLabel(mode))
			}
		}
		for _, record := range prior.Cases {
			if mode != "" && record.Mode != mode {
				continue
			}
			if record.Outcome != "FAIL" {
				continue
			}
			if !seen[record.CaseID] {
				return fmt.Errorf("AFFECTED scope must include the prior FAIL case %s", record.CaseID)
			}
		}
	}
	if state.ExecutionScopes == nil {
		state.ExecutionScopes = map[string]QAExecutionScope{}
	}
	state.ExecutionScopes[mode] = QAExecutionScope{Decision: decision, Mode: mode, BaseSnapshot: baseSnapshot, CaseIDs: normalized, Reason: strings.TrimSpace(reason), Origin: "USER", Source: source}
	return nil
}

// RecordExecutionScope records a QA execution rerun scope decision for the mode via
// the standalone workflow qa-execution-scope command. The mode must
// have an authoritative QA execution result to rerun (first execution needs no
// scope); the recorded BaseSnapshot is that result's snapshot. Source is PREPARE.
func RecordExecutionScope(root, packageRoot, runID, mode, decision string, caseIDs []string, reason string) (RunState, error) {
	return openRecord(root, packageRoot, runID, true, false, func(state *RunState, _ PromptCatalog) error {
		mode = strings.TrimSpace(mode)
		if mode != "blackbox" && mode != "whitebox" && mode != "" {
			return fmt.Errorf("非法 mode %q：qa-execution-scope 的 mode 只支持 blackbox、whitebox 或空（合并集）", mode)
		}
		// scope 决策按 mode 校验该 mode 的 design/review 前置（target=mode）。
		if err := requireTransition(*state, "qa-execution", mode); err != nil {
			return err
		}
		prior, base, ok := qaExecutionRerunSource(*state, mode)
		if !ok {
			return fmt.Errorf("this mode has no authoritative QA execution result to rerun, so a scope decision is not required (a first execution defaults to the full approved set)")
		}
		return recordExecutionScope(state, mode, decision, caseIDs, reason, scopeSourcePrepare, base, prior)
	})
}

// isBlackboxQADispatch reports whether the dispatch is a blackbox qa-design or
// qa-review round whose identity and source bind to the QA isolation worktree
// (== base) instead of the main worktree (== current).
func isBlackboxQADispatch(dispatch PreparedDispatch) bool {
	return dispatch.Mode == "blackbox" && (dispatch.Target == "qa-design" || dispatch.Target == "qa-review")
}

// qaModeResultKey returns the storage key that currently holds the effective QA
// execution result for a dispatch mode, together with that result: the
// mode's own per-mode key when it holds an authoritative record at the current
// snapshot, otherwise the merged "" key (the single-dispatch flow that executes
// all modes together, or a legacy merged execution migrated into the "" key).
// Aggregate per-mode checks use this so a merged execution in a per-mode-selected
// run is recognized, without coupling the per-mode results.
func qaModeResultKey(state RunState, mode string) (string, QAExecutionResult) {
	if result := state.qaExecution(mode); result.Snapshot == state.CurrentSnapshot && result.Status != "" && result.Status != "PENDING" {
		return mode, result
	}
	return "", state.qaExecution("")
}

// qaModeResult returns the effective QA execution result for a dispatch mode (see
// qaModeResultKey).
func qaModeResult(state RunState, mode string) QAExecutionResult {
	_, result := qaModeResultKey(state, mode)
	return result
}

// qaModeCarryResult returns the QA execution result a main-agent Carry considers
// for a dispatch mode, at ANY snapshot: the mode's own per-mode key when
// it holds a non-PENDING record, else the mode's preserved prior authoritative
// result (a result reset to PENDING after a re-design or an earlier repair round),
// else the merged "" key (single-dispatch / legacy merged storage). Unlike
// qaModeResultKey it does not require the result to sit at the current snapshot,
// so a pre-repair PASS preserved across a repair snapshot is still visible to
// main-agent carry inheritance.
func qaModeCarryResult(state RunState, mode string) QAExecutionResult {
	_, result := qaModeCarryResultKey(state, mode)
	return result
}

// qaModeCarryResultKey returns the storage key and result a main-agent Carry
// should rebind for a dispatch mode, mirroring qaModeCarryResult's read
// precedence so the carry writes back to the same key it read from.
func qaModeCarryResultKey(state RunState, mode string) (string, QAExecutionResult) {
	if result := state.qaExecution(mode); result.Status != "" && result.Status != "PENDING" {
		return mode, result
	}
	if prior := state.priorQAExecution(mode); prior != nil && (prior.Status == "PASS" || prior.Status == "FAIL") {
		return mode, *prior
	}
	return "", state.qaExecution("")
}

// qaUsesMergedExecution reports whether the run's QA execution is the merged
// single-dispatch flow: the merged "" key holds the authoritative result at the
// current snapshot (a legacy merged execution or the single-dispatch flow), as
// opposed to per-mode dispatch where each concrete mode records its own result.
func qaUsesMergedExecution(state RunState) bool {
	merged := state.qaExecution("")
	return merged.Snapshot == state.CurrentSnapshot && merged.Status != "" && merged.Status != "PENDING"
}

// qaModeRecordedAtCurrent reports whether the given QA dispatch mode has recorded
// execution at the current snapshot (per-mode storage). A mode with no
// cases in the run's QA case set is trivially recorded: there is nothing to
// execute, and the qa-review set-level coverage decision already judged the empty
// set (requirement 4). Otherwise the mode's effective execution result must sit
// at the current snapshot with a non-PENDING status (PASS / FAIL / RUNTIME_ERROR)
// — a merged "" result counts only when it truly carries records for the mode.
func qaModeRecordedAtCurrent(state RunState, mode string) bool {
	if len(state.qaModeCases(mode)) == 0 {
		return true
	}
	key, result := qaModeResultKey(state, mode)
	if result.Snapshot != state.CurrentSnapshot || result.Status == "" || result.Status == "PENDING" {
		return false
	}
	if mode != "" && key == "" {
		return qaResultHasMode(result, mode)
	}
	return true
}

// qaDispatchMode maps a selected QA id to its per-mode dispatch mode name.
// blackbox and whitebox dispatch by their own mode; merge QA and the legacy "qa"
// id dispatch as a single merged (empty-mode) set.
func qaDispatchMode(id string) string {
	switch id {
	case blackboxQAID:
		return "blackbox"
	case whiteboxQAID:
		return "whitebox"
	default:
		return ""
	}
}

// qaModeHasRecorded reports whether the given QA dispatch mode has recorded
// execution at the current snapshot (per-mode storage): a mode with no
// cases is trivially recorded, otherwise the mode's effective execution result
// must sit at the current snapshot with records for the mode (merged "" result
// counts only when it truly carries the mode's records).
func qaModeHasRecorded(state RunState, mode string) bool {
	return qaModeRecordedAtCurrent(state, mode)
}

func qaCaseSemanticKey(kind, description, procedure, oracle string) string {
	return strings.Join([]string{strings.ToUpper(strings.TrimSpace(kind)), strings.TrimSpace(description), strings.TrimSpace(procedure), strings.TrimSpace(oracle)}, "\x00")
}

// qaCaseSemanticKeyWithTest is the incremental-design identity of a whitebox case:
// it includes the Test binding (the "<file>::<function>" test reference) so
// that changing a case's test reference produces a new case identity (re-review)
// instead of silently retaining the prior case's PASS for a case whose executed test
// changed. Blackbox cases carry an empty test, so the key reduces to the four-field
// identity.
func qaCaseSemanticKeyWithTest(kind, description, procedure, oracle, test string) string {
	return qaCaseSemanticKey(kind, description, procedure, oracle) + "\x00" + strings.TrimSpace(test)
}
