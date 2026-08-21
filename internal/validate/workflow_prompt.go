package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PrepareGate prepares a gate review dispatch. userRequested is the explicit
// user override signal for the resume rule: when a claimed, unchanged
// dispatch for the same gate is interrupted, the CLI forces a resume unless the
// user authorizes a fresh dispatch (the source is recorded in ReviewOverrides).
func PrepareGate(root, packageRoot, runID, gateID string, userRequested bool, userReason string) (string, error) {
	return prepareBoundPrompt(root, packageRoot, runID, gateID, "gate", true, "", userRequested, userReason, func(state *RunState, catalog PromptCatalog, route PromptRoute) (string, error) {
		if err := requireTransition(*state, "gate", gateID); err != nil {
			return "", err
		}
		result, ok := state.Gates[gateID]
		if !ok {
			return "", fmt.Errorf("gate %q is not in this run's discovered catalog", gateID)
		}
		if semanticResultRecorded(result.Status, result.Snapshot, state.CurrentSnapshot) && !gatePromptChanged(*state, catalog, gateID) {
			return "", fmt.Errorf("gate %q already has an authoritative %s result for the current snapshot; re-review requires a changed gate prompt or a repair snapshot", gateID, result.Status)
		}
		return ComposeGatePrompt(catalog, gateID, route)
	})
}

// PrepareAction prepares an action dispatch. mode is required for qa-design and
// qa-review (blackbox or whitebox): a blackbox dispatch binds its identity and
// source to the QA isolation worktree (== base), a whitebox dispatch to the main
// worktree (== current). userRequested is the explicit user override signal:
// for product-review / start-readiness it allows re-preparing a review of an
// already-PASS round (the only-user-can-break-the-rule side-B exception) and the
// source is recorded in ReviewOverrides. scope（增量审查）对 product-review /
// start-readiness 声明本次审查的需求项范围：声明项置 PENDING 待判，未声明的已 PASS 项
// 保持 PASS、任何轮不可改（除非主代理下次显式声明变更）；格式无关，不按文件名/后缀解析。
func PrepareAction(root, packageRoot, runID, actionID, mode string, userRequested bool, userReason string, scope ...string) (string, error) {
	if actionID == "development-worker" {
		return prepareDevelopmentAction(root, packageRoot, runID, userRequested, userReason)
	}
	reviewerRequired := actionID != "requirements-clarification"
	return prepareBoundPrompt(root, packageRoot, runID, actionID, "action", reviewerRequired, mode, userRequested, userReason, func(state *RunState, catalog PromptCatalog, route PromptRoute) (string, error) {
		// prepare 时声明本次审查范围——声明项置 PENDING 待判，未声明项保持既有
		// 结论不动（已 PASS 项保持 PASS、任何轮不可改）。
		if (actionID == "product-review" || actionID == "start-readiness") && len(scope) != 0 {
			if state.ReviewItemsByAction == nil {
				state.ReviewItemsByAction = map[string]map[string]ReviewItem{}
			}
			items := state.ReviewItemsByAction[actionID]
			if items == nil {
				items = map[string]ReviewItem{}
			}
			for _, key := range scope {
				key = strings.TrimSpace(key)
				if key == "" {
					continue
				}
				items[key] = ReviewItem{Status: "PENDING"}
			}
			state.ReviewItemsByAction[actionID] = items
		}
		overridePassRound := userRequested && (actionID == "product-review" || actionID == "start-readiness") && state.Actions[actionID].Status == "PASS"
		if !overridePassRound {
			// qa-design / qa-review / qa-execution 的 transition 判定都按派发 mode
			// 传递 target，使 requireTransition 能读对应 mode 的 review/design 结果。
			target := ""
			switch actionID {
			case "qa-design", "qa-review", "qa-execution":
				target = mode
			}
			if err := requireTransition(*state, actionID, target); err != nil {
				return "", err
			}
		} else if state.ReviewOverrides == nil {
			state.ReviewOverrides = map[string]string{}
		}
		if overridePassRound {
			reason := strings.TrimSpace(userReason)
			if reason == "" {
				reason = "user requested a re-review of an already-passed review round"
			}
			state.ReviewOverrides[actionID] = reason
		}
		// 黑盒/白盒各自独立派发、并行执行，同一 snapshot 下每个 mode
		// 的 qa-execution 各记录一次；同 mode 已出权威结果时才挡后续同 mode 派发。
		// --user-requested 显式授权时放行（recordReviewOverride 记录授权来源），否则照旧拒绝。
		if actionID == "qa-execution" && qaExecutionModeResulted(*state, mode) {
			if !userRequested {
				return "", fmt.Errorf("QA Execution already has an authoritative %s result for this mode", state.qaExecution(mode).Status)
			}
			recordReviewOverride(state, actionID, userReason)
		}
		// 重跑强制 scope 决策（CLI 强制，防主代理遗漏）。该 mode 存在更早快照的
		// 权威执行结果（即本次是重跑）时，prepare 前必须已记录覆盖本次重跑（BaseSnapshot
		// 匹配上一轮权威结果快照）的 scope 决策，否则拒绝 prepare。
		if actionID == "qa-execution" {
			if base, ok := qaExecutionPriorResultedBase(*state, mode); ok {
				sc, ok2 := state.ExecutionScopes[mode]
				if !ok2 || sc.BaseSnapshot != base {
					return "", fmt.Errorf("QA Execution rerun requires a scope decision: run `workflow qa-execution-scope --mode %s --decision FULL|AFFECTED ...` first", mode)
				}
			}
		}
		detail, err := actionPromptDetail(*state, catalog, actionID, mode)
		if err != nil {
			return "", err
		}
		return ComposeActionPrompt(catalog, actionID, route, detail)
	})
}

// prepareBoundPrompt composes and registers a prepared dispatch. mode selects the
// QA isolation binding for qa-design/qa-review blackbox dispatches; userRequested
// carries the explicit user override signal for the review-rule enforcement.
func prepareBoundPrompt(root, packageRoot, runID, target, targetKind string, reviewerRequired bool, mode string, userRequested bool, userReason string, compose func(*RunState, PromptCatalog, PromptRoute) (string, error)) (string, error) {
	prompt := ""
	_, err := mutateRun(root, runID, func(state *RunState) error {
		catalog, err := requireCurrentDefinitions(root, *state, packageRoot)
		if err != nil {
			return err
		}
		// 硬闸：任何未处理完毕的继承判定未处理完时拒绝继续 / 重跑入口。
		// carry 是处置命令，准备它的审查派发在待决时放行（否则无法处置、死端）。
		if !(targetKind == "action" && target == "carry") {
			if err := requireNoPendingInheritance(root, *state, catalog); err != nil {
				return err
			}
		}
		// 续用强制：同一职责、源快照不变、任务内容不变且未带 --user-requested
		// 时，拒绝 prepare 同目标新派发，指示恢复原代理继续同一派发；用户显式授权
		// 时记录来源后放行新派发。
		if err := requireResumeInterrupted(root, *state, catalog, targetKind, target, mode); err != nil {
			if !userRequested {
				return err
			}
			recordReviewOverride(state, target, userReason)
		}
		// 开发开始后 qa-design/qa-review 派发 mode 必填：省略 --mode 会静默绑主工作区、
		// 黑盒代理可读开发代码、隔离被绕过。快速路径（开发尚未开始，路线未确认时的预演
		// 设计）空 mode 合法，仅开发开始后必填。
		if (target == "qa-design" || target == "qa-review") && developmentStarted(*state) && mode == "" {
			return fmt.Errorf("%s dispatch requires --mode blackbox or whitebox after development starts", target)
		}
		blackbox := mode == "blackbox" && (target == "qa-design" || target == "qa-review")
		if blackbox {
			if err := requireIsolatedCurrent(root, *state); err != nil {
				return err
			}
		} else if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		wave := 0
		if targetKind == "gate" {
			wave = currentGateReviewWave(*state)
		}
		attempt := nextDispatchAttempt(*state, targetKind, target, wave)
		dispatchID, err := newDispatchID()
		if err != nil {
			return err
		}
		route := routeForState(root, *state)
		if blackbox {
			route.Worktree = absPath(state.QAWorktree)
			route.CurrentSnapshot = state.BaseSnapshot
		}
		route.DispatchID, route.DispatchAttempt, route.ReviewWave = dispatchID, attempt, wave
		prompt, err = compose(state, catalog, route)
		if err != nil {
			return err
		}
		// prepare（生成任务票）SHALL NOT 作废任何派发。作废只发生在认领新派发时
		// （同功能旧 OPEN 空票清理 / CLAIMED 去重与手动终止例外）与全局失效事件（采纳外部
		// 改动、需求作废、快照重绑），不再在 prepare 时把既有在途派发标 STALE。
		source := state.CurrentSnapshot
		if blackbox {
			source = state.BaseSnapshot
		}
		// 确认的 P0/P1 待重审时，新派发的 product-review/start-readiness 轮即重审轮，
		// 记录其派发 id，只有该轮返回 PASS 才算重审完成。
		if (target == "product-review" || target == "start-readiness") && state.NeedsReReview[target] != "" {
			if state.ReReviewDispatch == nil {
				state.ReReviewDispatch = map[string]string{}
			}
			state.ReReviewDispatch[target] = dispatchID
		}
		sum := sha256.Sum256([]byte(prompt))
		promptHash := hex.EncodeToString(sum[:])
		// 需求 6 第 4 条：把完整提示词内容写入本 run 规范提示词文件，并把内容 hash 与
		// 文件路径作为派发状态的一部分记录；写入时（第一时间）立即校验内容 hash == 记录值，
		// 使写钩子/文件系统在写出那一刻的篡改即暴露，而不是拖到派发时才被发现。派发只
		// 消费该规范文件——主代理只发指向文件的薄启动消息、不得手写/凭记忆拼写提示词内容，
		// 子代理读文件执行。
		promptFile, err := writeCanonicalPromptFile(root, state.RunID, dispatchID, prompt, promptHash)
		if err != nil {
			return err
		}
		state.Dispatches[dispatchID] = PreparedDispatch{ID: dispatchID, Target: target, TargetKind: targetKind, Attempt: attempt, ReviewWave: wave, PromptHash: promptHash, PromptFile: promptFile, RequirementRevision: state.RequirementRevision, CatalogRevision: state.CatalogRevision, SourceSnapshot: source, ReviewerRequired: reviewerRequired, Status: "OPEN", Mode: mode}
		return nil
	})
	return prompt, err
}

// requireIsolatedCurrent verifies the registered QA isolation worktree sits at
// the base snapshot (its native identity == base). Blackbox qa-design/qa-review
// prepare/claim/record all resolve identity against this worktree instead of the
// main worktree, so the parallel QA never observes development code under normal
// navigation. The injected requirement-document / acceptance-artifact hash check
// lives at workflow qa-worktree registration only (requirement 1's "登记或黑盒
// prepare 时校验" guard is contracted to registration); prepare/claim/record do
// not re-read or re-hash the worktree files.
func requireIsolatedCurrent(root string, state RunState) error {
	if strings.TrimSpace(state.QAWorktree) == "" {
		return fmt.Errorf("QA isolation worktree is not registered; run workflow qa-worktree first")
	}
	resolver, err := resolverForVCS(state.VCS, nil)
	if err != nil {
		return err
	}
	resolved, err := resolver.Resolve(cleanWorktree(state.QAWorktree))
	if err != nil {
		return err
	}
	if !strings.EqualFold(resolved, state.BaseSnapshot) {
		return fmt.Errorf("QA isolation worktree native identity %s does not match the base snapshot %s", resolved, state.BaseSnapshot)
	}
	return nil
}

// verifyIsolatedRequirementRevisions verifies the requirement document / acceptance
// artifact hashes inside the QA isolation worktree match the run's registered
// revisions（防 host 遗忘刷新注入）。注入是工作树状态，原生标识校验（Git=提交、P4=
// changelist、SVN=BASE 版本级）不受影响。
func verifyIsolatedRequirementRevisions(state RunState, worktree string) error {
	for _, artifact := range state.RequirementArtifacts {
		path := resolveFromRoot(cleanWorktree(worktree), artifact.Path)
		revision, err := RequirementRevision(path)
		if err != nil {
			return fmt.Errorf("QA isolation worktree requirement artifact %s: %w", artifact.Path, err)
		}
		if revision != artifact.Revision {
			return fmt.Errorf("QA isolation worktree requirement artifact %s does not match the run revision %s", artifact.Path, artifact.Revision)
		}
	}
	return nil
}

func actionPromptDetail(state RunState, catalog PromptCatalog, actionID, mode string) (string, error) {
	if actionID == "qa-design" {
		var lines []string
		if isMergeVerification(state) {
			lines = append(lines, "Merge QA: design cross-slice interaction cases for the merged snapshot. The case set may be empty when the slices are essentially independent (leave a trace noting that instead of forcing cases).")
		} else if state.RouteMode == "" {
			// 快速路径：路线未确认时只开始黑盒设计，与 start-readiness 并行，先于拆分
			// 决定与路线确认；最终路线不含黑盒 QA 时本设计废弃。
			lines = append(lines, "Fast-path blackbox QA design: from the confirmed requirement, design blackbox behavior cases. The route is not yet confirmed; if it omits blackbox QA this design is discarded.")
		} else if mode == "blackbox" {
			lines = append(lines, "This is a blackbox QA design round: design real QA behavior cases from the current confirmed requirement document. Work in the QA isolation worktree; the current requirement document (revision registered by the run) is injected there as working-tree state. Base every case on the current requirement document, not on any baseline product documentation. '实际使用产品' is the execution-phase description: after the snapshot you execute cases against the built product on the main worktree. In this design phase, the runnable product is not yet built, so base the cases on the injected current requirement.")
		} else if mode == "whitebox" {
			lines = append(lines, "This is a whitebox QA design round: read the implementation on the main worktree, independently design structure test cases (unit, system, integration) by responsibility, and directly write the structural test code you deliver. Every whitebox case must bind the test implementing it with a --test \"<file>::<function>\" reference (file path locating the delivered test code file, function name locating the test inside it; both opaque strings, the CLI does not parse code): the CLI records the reference and requires it non-empty and unique per case (one test implements one case). Test existence and correspondence are verified by QA Review (reading your delivered code) and QA Execution (running the tests), so the case ID and the executed test are truly bound.")
		} else if modes := selectedQAModes(state); len(modes) != 0 {
			lines = append(lines, "Selected QA modes: "+strings.Join(modes, "; "))
		}
		// 设计轮只动本派发 mode 的用例，提示词只列该 mode 的既有用例。
		modeCases := state.qaModeCases(mode)
		if len(modeCases) != 0 {
			// 增量契约（改动 2）：qa-design 只返回变更——新增用例不带 id（CLI 分配）、修改
			// 用例用 --case-id 引用既有 id、删除用例用 --remove-case 列出 id；未提及用例及
			// 其 PASS 状态自动保留、不再清除。整体替换（--replace-all）只用于整体工作流变更。
			lines = append(lines, "Review the complete current requirement and every prior case below. Return only your changes: new cases need no id (the CLI assigns one), modified cases must reference their existing CASE id with --case-id, and removed cases must be listed with --remove-case. Unmentioned prior cases and their PASS status are retained automatically; an omitted case is never cleared. Use --replace-all only when the whole case set must change (e.g. the overall workflow changed).")
			// 设计轮只列出本派发 mode 的 review FAIL 发现项，不混入另一 mode。
			if review := state.qaReview(mode); review.Status == "FAIL" {
				lines = append(lines, "Address these QA Review findings while redesigning the affected cases:")
				for _, finding := range review.Findings {
					line := "- " + finding.Message
					if len(finding.Locations) != 0 {
						line += " (" + strings.Join(finding.Locations, ", ") + ")"
					}
					lines = append(lines, line)
				}
			}
			for _, testCase := range modeCases {
				lines = append(lines, formatQACase(testCase, true))
			}
		}
		if len(lines) == 0 {
			return "", nil
		}
		return strings.Join(lines, "\n\n"), nil
	}
	if actionID == "qa-review" {
		// 按派发 mode 读取该 mode 的用例组装审查输入。
		modeCases := state.qaModeCases(mode)
		pendingCases := pendingQACases(modeCases, mode)
		accepted := []string{"Accepted coverage context; do not return new decisions for these cases:"}
		pending := []string{"Return one decision for every pending case below:"}
		// 增量契约上下文（改动 2）：向审查者注入本设计轮的变更清单，让它聚焦本轮
		// 新增/修改/删除的用例；存量 PASS 用例只在上面作为上下文列出、不得重判。
		if change, ok := state.QADesignChangesByMode[mode]; ok {
			var parts []string
			if len(change.Added) != 0 {
				parts = append(parts, "added "+strings.Join(change.Added, ", "))
			}
			if len(change.Modified) != 0 {
				parts = append(parts, "modified "+strings.Join(change.Modified, ", "))
			}
			if len(change.Removed) != 0 {
				parts = append(parts, "removed "+strings.Join(change.Removed, ", "))
			}
			if len(parts) != 0 {
				accepted = append(accepted, "This design round changed: "+strings.Join(parts, "; ")+". Review focuses on these changes and their coverage; unmentioned prior cases keep their status.")
			}
		}
		// 黑盒（改动 1）：用例镜像文件落在 QA isolation worktree 的 .gates/cases/blackbox.md，
		// 指向该文件，让审查者基于镜像审阅并对照主工作区当前确认的需求。
		if mode == "blackbox" && strings.TrimSpace(state.QAWorktree) != "" {
			accepted = append(accepted, "Blackbox cases are mirrored to the QA isolation worktree: "+filepath.Join(cleanWorktree(state.QAWorktree), ".gates", "cases", "blackbox.md"))
		}
		for _, testCase := range modeCases {
			if testCase.ReviewStatus == "PASS" {
				accepted = append(accepted, fmt.Sprintf("%s: %s", testCase.ID, testCase.Description))
			}
		}
		for _, testCase := range pendingCases {
			pending = append(pending, formatQACase(testCase, false))
		}
		if len(pending) == 1 {
			accepted = append(accepted, "There are no pending case decisions. Review the set for missing or duplicated coverage and return no case decisions. Set-level coverage omission for a selected mode is P1 and blocks; P2 findings are suggestions that do not block this round's verdict.")
			return strings.Join(accepted, "\n\n"), nil
		}
		if len(accepted) == 1 {
			return strings.Join(pending, "\n\n"), nil
		}
		return strings.Join(append(accepted, pending...), "\n\n"), nil
	}
	if actionID == "qa-execution" {
		// 按派发 mode 过滤需执行集，黑盒/白盒各自独立派发、并行执行。
		required := qaExecutionRequiredCases(state, mode)
		var lines []string
		for _, testCase := range required {
			lines = append(lines, formatQACase(testCase, false))
		}
		// AFFECTED 重跑的子集在派发前由 host 综合判定定死，向执行者显式说明
		// 本次需执行范围与继承范围，禁止执行中自行补跑/改判/改选名单外（继承）用例。
		if sc, ok := qaExecutionAffectedScope(state, mode); ok {
			lines = append(lines, fmt.Sprintf("This is an AFFECTED rerun: execute ONLY the affected subset listed above (fixed before dispatch by the host). The other approved cases inherit their PASS from snapshot %s and are NOT part of this execution. Run only this subset; do not self-add, re-judge, or re-select cases outside it, and do not report or change the subset mid-execution.", sc.BaseSnapshot))
		}
		return strings.Join(lines, "\n\n"), nil
	}
	if actionID == "carry" {
		eligible := eligibleCarryGates(state)
		if len(eligible) == 0 {
			return "", fmt.Errorf("no prior passing gates require a Carry decision")
		}
		lines := []string{"Decide INHERIT or RERUN for each gate below:"}
		for _, id := range eligible {
			gate, _ := catalog.Gate(id)
			lines = append(lines, fmt.Sprintf("\n[Gate: %s]\n%s", id, gate.Content))
		}
		return strings.Join(lines, "\n"), nil
	}
	if actionID == "product-review" || actionID == "start-readiness" {
		// 增量审查：prepare-action --scope 声明的需求项组成审查输入——PENDING 项
		// "必须判定"、已 PASS 项 "accepted context 不得重判"。与 QA 增量的语义一致：只审新增/
		// 变更的需求项，已审查通过的存量部分由 CLI 保留、不重审。格式无关，不解析文档结构。
		if items := state.ReviewItemsByAction[actionID]; len(items) != 0 {
			var lines []string
			if settled := state.SettledFindings[actionID]; len(settled) != 0 {
				lines = append(lines, "Findings and decisions the user has already settled. Do not re-raise them; re-raise one only if a requirement revision changed the premise it relied on. Dispositions: 确认问题 (confirm, treated as a real problem being fixed) or 驳回问题 (dismiss, voided).")
				for _, settledItem := range settled {
					lines = append(lines, "- ["+settledItem.Disposition+"] "+settledItem.Message)
				}
			}
			pending := []string{"Return one PASS or FAIL decision for every pending item below (each FAIL requires a reason):"}
			accepted := []string{"Accepted coverage context; do not re-judge these already-passed items:"}
			keys := make([]string, 0, len(items))
			for key := range items {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				item := items[key]
				if item.Status == "PASS" {
					accepted = append(accepted, "- "+key)
				} else {
					pending = append(pending, "- "+key)
				}
			}
			if len(pending) == 1 {
				lines = append(lines, "There are no pending items to decide in this incremental review. Review the declared scope and return no item decisions; set-level coverage omission for a declared item is P1 and blocks; P2 findings are suggestions that do not block this round's verdict.")
				return strings.Join(lines, "\n"), nil
			}
			lines = append(lines, strings.Join(pending, "\n"), strings.Join(accepted, "\n"))
			return strings.Join(lines, "\n\n"), nil
		}
		if settled := state.SettledFindings[actionID]; len(settled) != 0 {
			lines := []string{"Findings and decisions the user has already settled. Do not re-raise them; re-raise one only if a requirement revision changed the premise it relied on. Dispositions: 确认问题 (confirm, treated as a real problem being fixed) or 驳回问题 (dismiss, voided)."}
			for _, item := range settled {
				lines = append(lines, "- ["+item.Disposition+"] "+item.Message)
			}
			return strings.Join(lines, "\n"), nil
		}
	}
	return "", nil
}

func prepareDevelopmentAction(root, packageRoot, runID string, userRequested bool, userReason string) (string, error) {
	return prepareBoundPrompt(root, packageRoot, runID, "development-worker", "action", true, "", userRequested, userReason, func(state *RunState, catalog PromptCatalog, route PromptRoute) (string, error) {
		if state.RetainedOverall {
			return "", fmt.Errorf("a retained overall run keeps implementation and repair ownership in slice runs; record merged slice snapshots with workflow snapshot")
		}
		if err := requireTransition(*state, "development-worker", ""); err != nil {
			return "", err
		}
		detail := ""
		status := state.Actions["development-worker"].Status
		if status == developmentComplete || status == developmentVerified || status == developmentRepairPrepared {
			detail = repairInput(*state)
		}
		prompt, err := ComposeActionPrompt(catalog, "development-worker", route, detail)
		if err != nil {
			return "", err
		}
		if status == developmentComplete || status == developmentVerified {
			state.Actions["development-worker"] = ActionResult{Status: developmentRepairPrepared}
		} else if status == developmentPending {
			state.Actions["development-worker"] = ActionResult{Status: developmentPrepared}
		}
		return prompt, nil
	})
}

// PromptPath returns the run's canonical prompt file path for a dispatch
// (.gates/tmp/<run-id>/prompts/<dispatch-id>.md). prepare 把完整提示词内容写入该
// 文件，派发只消费该文件；文件路径与内容 hash 记录在派发状态里，作为派发时的兜底
// 校验依据。
func PromptPath(root, runID, dispatchID string) string {
	return filepath.Join(RunDir(root, runID), "prompts", dispatchID+".md")
}

// writeCanonicalPromptFile writes the dispatch's complete prompt to the run's
// canonical prompt file and immediately verifies (write-time, 第一时间) that the
// on-disk content hash equals the recorded hash. The write-then-reread check makes
// a write hook or filesystem tampering at the moment the file is written surface
// as a hard error instead of being discovered only at dispatch time.
func writeCanonicalPromptFile(root, runID, dispatchID, prompt, promptHash string) (string, error) {
	path := PromptPath(root, runID, dispatchID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := writeAtomic(path, []byte(prompt), 0o600); err != nil {
		return "", err
	}
	written, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(written)
	if hex.EncodeToString(sum[:]) != promptHash {
		return "", fmt.Errorf("canonical prompt file %s failed write-time verification: content hash does not match the prepared dispatch record", path)
	}
	return path, nil
}

// verifyCanonicalPromptFile is the dispatch-time (兜底) check: the subagent reads
// the canonical prompt file and executes it, so before the dispatch is claimed the
// CLI re-verifies the file content hash matches the prepare record. A mismatch
// hard-blocks the claim and the dispatch is judged unusable (every further claim
// attempt rejects the same way); the dispatch must be re-prepared. Dispatches
// prepared before this feature (no recorded prompt file) are not forced.
func verifyCanonicalPromptFile(state RunState, dispatch PreparedDispatch) error {
	if strings.TrimSpace(dispatch.PromptFile) == "" {
		return nil
	}
	data, err := os.ReadFile(dispatch.PromptFile)
	if err != nil {
		return fmt.Errorf("canonical prompt file %s: %w", dispatch.PromptFile, err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != dispatch.PromptHash {
		return fmt.Errorf("canonical prompt file %s content does not match the prepared dispatch record (content hash mismatch): the dispatch is unusable; re-prepare it instead of writing or hand-editing the prompt", dispatch.PromptFile)
	}
	return nil
}

// composeDispatchPrompt recomposes the exact dispatch prompt the prepare flow
// would produce, mirroring the detail computation of prepareDevelopmentAction
// and the actionPromptDetail injection of PrepareAction without re-running the
// transition guards (they are not part of the task text).
func composeDispatchPrompt(state RunState, catalog PromptCatalog, route PromptRoute, dispatch PreparedDispatch) (string, error) {
	if dispatch.TargetKind == "gate" {
		return ComposeGatePrompt(catalog, dispatch.Target, route)
	}
	if dispatch.Target == "development-worker" {
		detail := ""
		status := state.Actions["development-worker"].Status
		if status == developmentComplete || status == developmentVerified || status == developmentRepairPrepared {
			detail = repairInput(state)
		}
		return ComposeActionPrompt(catalog, "development-worker", route, detail)
	}
	detail, err := actionPromptDetail(state, catalog, dispatch.Target, dispatch.Mode)
	if err != nil {
		return "", err
	}
	return ComposeActionPrompt(catalog, dispatch.Target, route, detail)
}
