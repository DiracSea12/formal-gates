package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"formal-gates/internal/cost"
	"formal-gates/internal/lifecycle"
)

type CarryInput struct{ GateID, Decision, Message string }

func RecordCarry(root, packageRoot, runID, dispatchID string, decisions []CarryInput, runtimeError string, mainAgent bool, mainReason string) (RunState, error) {
	return openRecord(root, packageRoot, runID, false, false, func(state *RunState, catalog PromptCatalog) error {
		if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		if mainAgent {
			if !hasDevelopmentSnapshot(*state) {
				return fmt.Errorf("a development snapshot is required before a main-agent Carry")
			}
		} else if err := requireTransition(*state, "carry", ""); err != nil {
			return err
		}
		if mainAgent {
			if len(decisions) != 0 || strings.TrimSpace(runtimeError) != "" {
				return fmt.Errorf("main-agent Carry cannot include independent decisions or a runtime error")
			}
			if strings.TrimSpace(dispatchID) != "" {
				return fmt.Errorf("main-agent Carry does not accept an independent dispatch")
			}
			reason := strings.TrimSpace(mainReason)
			if reason == "" {
				return fmt.Errorf("main-agent Carry requires a reason")
			}
			delta := catalogDelta(*state, catalog)
			// 存在目录变更（第三类：检测到的中途修改 / 目录接受）时，主代理 Carry
			// 是既定的处置入口，即使先前已记录过独立 Carry 判定也可用于新变更的处置
			// （该入口在存在受影响记录结果时可用，含未发生修复的中途修改场景）。
			if len(delta) == 0 && (hasRecordedCarryDecisions(*state) || (state.PreRepairSnapshot != "" && repairRerunRecorded(*state))) {
				return fmt.Errorf("main-agent Carry must be recorded before independent Carry or repair reruns")
			}
			if len(delta) != 0 {
				state.CatalogRevision = catalog.CatalogRevision
				state.BasePromptRevision = catalog.BaseRevision
				acceptCatalogHashes(state, catalog)
			}
			eligible := eligibleMainCarryResults(*state, len(delta) != 0)
			if len(eligible) == 0 && len(delta) == 0 {
				return fmt.Errorf("no prior passing selected results are eligible for main-agent Carry")
			}
			source := state.PreRepairSnapshot
			if source == "" {
				source = state.CurrentSnapshot
			}
			for _, id := range eligible {
				inheritCarryResult(state, id, carryOriginMainShortcut, reason, source)
			}
			state.Actions["carry"] = ActionResult{Status: "PASS"}
			if state.PreRepairSnapshot != "" {
				resolveCarryBoundary(state)
			}
			return nil
		}
		if strings.TrimSpace(mainReason) != "" {
			return fmt.Errorf("--main-reason requires main-agent Carry")
		}
		dispatch, err := requirePreparedDispatch(*state, dispatchID, "action", "carry")
		if err != nil {
			return err
		}
		if err := requireLifecycleVerification(root, *state, dispatch); err != nil {
			return err
		}
		backfillDispatchCost(root, state, dispatch)
		eligible := eligibleCarryGates(*state)
		if len(eligible) == 0 {
			return fmt.Errorf("no prior passing gates require a Carry decision")
		}
		if strings.TrimSpace(runtimeError) != "" {
			if len(decisions) != 0 {
				return fmt.Errorf("Carry runtime error cannot include decisions")
			}
			state.Actions["carry"] = ActionResult{Status: "RUNTIME_ERROR", Message: strings.TrimSpace(runtimeError), DispatchID: dispatch.ID}
			completeDispatch(state, dispatch.ID)
			completeReviewWaveIfReady(state)
			return nil
		}
		if len(decisions) != len(eligible) {
			return fmt.Errorf("Carry must decide all %d prior passing gates", len(eligible))
		}
		wanted := map[string]bool{}
		for _, id := range eligible {
			wanted[id] = true
		}
		seen := map[string]bool{}
		for _, decision := range decisions {
			if !wanted[decision.GateID] {
				return fmt.Errorf("gate %q is not eligible for Carry", decision.GateID)
			}
			if seen[decision.GateID] {
				return fmt.Errorf("duplicate Carry decision for %s", decision.GateID)
			}
			if decision.Decision != "INHERIT" && decision.Decision != "RERUN" {
				return fmt.Errorf("Carry decision for %s must be INHERIT or RERUN", decision.GateID)
			}
			if strings.TrimSpace(decision.Message) == "" {
				return fmt.Errorf("Carry decision for %s requires a reason", decision.GateID)
			}
			seen[decision.GateID] = true
			if decision.Decision == "INHERIT" {
				inheritCarryResult(state, decision.GateID, carryOriginIndependent, strings.TrimSpace(decision.Message), state.PreRepairSnapshot)
			} else {
				state.Carry[decision.GateID] = CarryResult{Decision: decision.Decision, Origin: carryOriginIndependent, SourceSnapshot: state.PreRepairSnapshot, TargetSnapshot: state.CurrentSnapshot, Message: strings.TrimSpace(decision.Message)}
				state.Gates[decision.GateID] = GateResult{Status: "PENDING"}
			}
		}
		state.Actions["carry"] = ActionResult{Status: "PASS", DispatchID: dispatch.ID}
		completeDispatch(state, dispatch.ID)
		resolveCarryBoundary(state)
		return nil
	})
}

// eligibleMainCarryResults lists the selected prior PASS results a main-agent
// Carry may inherit. In a repair or adoption the previous snapshot is the
// pre-repair boundary; at a catalog-delta rebinding the PASS results at the
// current snapshot are the ones whose judgment is being recorded.
func eligibleMainCarryResults(state RunState, catalogChanged bool) []string {
	if state.PreRepairSnapshot == "" && !catalogChanged {
		return nil
	}
	ids := []string{}
	// 按 mode 各自独立判定 QA 结果的 carry 资格；每个 PASS 的选中 QA mode 都是
	// 独立可继承结果（各自发出一个 QA 模式 id，使 inheritCarryResult 按 isQAMode 把该
	// mode 的结果快照重绑，而不写进虚假的 Gates["qa"]）。
	for id := range selectedSet(state) {
		if !isQAMode(id) {
			continue
		}
		// 直取该 mode 的执行结果（不再经过只返回 current-snapshot 结果的
		// qaModeResult / qaModeResultKey），使修复快照（PreRepairSnapshot）之前已 PASS 的
		// QA mode 可被 main-agent Carry 继承；单派发/legacy 合并流程经 "" 键回退取合并结果。
		result := qaModeCarryResult(state, qaDispatchMode(id))
		if result.Status != "PASS" {
			continue
		}
		eligible := state.PreRepairSnapshot != "" && result.Snapshot == state.PreRepairSnapshot
		eligible = eligible || (catalogChanged && result.Snapshot == state.CurrentSnapshot)
		if eligible {
			ids = append(ids, id)
		}
	}
	for id, result := range state.Gates {
		if !isSelected(state, id) || result.Status != "PASS" {
			continue
		}
		if state.PreRepairSnapshot != "" && result.Snapshot == state.PreRepairSnapshot {
			ids = append(ids, id)
		} else if catalogChanged && result.Snapshot == state.CurrentSnapshot {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func repairRerunRecorded(state RunState) bool {
	if isSelectedQA(state) {
		for id := range selectedSet(state) {
			if !isQAMode(id) {
				continue
			}
			result := qaModeResult(state, qaDispatchMode(id))
			if result.Snapshot == state.CurrentSnapshot && result.Status != "" && result.Status != "PENDING" {
				return true
			}
		}
	}
	for id := range selectedSet(state) {
		if isQAMode(id) {
			continue
		}
		result := state.Gates[id]
		if result.Snapshot == state.CurrentSnapshot && result.Status != "" && result.Status != "PENDING" {
			return true
		}
	}
	return false
}

func inheritCarryResult(state *RunState, id, origin, reason, source string) {
	state.Carry[id] = CarryResult{Decision: "INHERIT", Origin: origin, SourceSnapshot: source, TargetSnapshot: state.CurrentSnapshot, Message: reason}
	if isQAMode(id) {
		// 只重绑该 QA 模式的有效执行结果快照（per-mode 或合并单派发结果），另一
		// mode 不受影响。用与 eligibleMainCarryResults 相同的任意快照读取（qaModeCarryResultKey），
		// 保证写回键就是读取键——修复快照前已 PASS 的 per-mode 结果也能被正确重绑到当前快照。
		key, result := qaModeCarryResultKey(*state, qaDispatchMode(id))
		result.Snapshot = state.CurrentSnapshot
		state.setQAExecution(key, result)
		return
	}
	prior := state.Gates[id]
	prior.SourceSnapshot = source
	prior.Snapshot = state.CurrentSnapshot
	state.Gates[id] = prior
}

// hasRecordedCarryDecisions reports whether any non-adoption Carry judgment has
// been recorded; an adoption's provenance under the reserved adoption key does
// not count as a decision, so a main-agent Carry still records the first one.
func hasRecordedCarryDecisions(state RunState) bool {
	for id := range state.Carry {
		if id != carryAdoptKey {
			return true
		}
	}
	return false
}

// isAdoptionBoundary reports whether the current pre-repair boundary was created
// by an adopted external change rather than by a repair snapshot: the reserved
// adoption key still points its target at the current snapshot. A later repair
// snapshot drops the adoption provenance through resetSnapshotReviewSurface, so
// this stays true only while the adoption itself is the boundary being resolved.
func isAdoptionBoundary(state RunState) bool {
	adopted, ok := state.Carry[carryAdoptKey]
	return ok && adopted.TargetSnapshot == state.CurrentSnapshot && state.PreRepairSnapshot != ""
}

// acceptCatalogHashes records the current per-entry prompt hashes while
// preserving the recorded hash of every gate whose composed prompt changed, so
// a selected gate whose prompt moved stays re-dispatchable until a fresh review
// at the accepted catalog settles it.
func acceptCatalogHashes(state *RunState, catalog PromptCatalog) {
	current := catalogPromptHashes(catalog)
	merged := make(map[string]string, len(current))
	for id, hash := range current {
		if strings.HasPrefix(id, "gate:") {
			if recorded, ok := state.PromptHashes[id]; ok && recorded != hash {
				merged[id] = recorded
				continue
			}
		}
		merged[id] = hash
	}
	state.PromptHashes = merged
}

func Abort(root, runID string) (RunSummary, error) { return finishRun(root, runID, "ABORTED") }

// Seal aggregates the run. For git runs whose base→current range holds more than
// one commit, the range is squashed into a single commit (git reset --soft base +
// a fresh commit with --squash-message) as the last VCS operation, preserving the
// final tree. The summary's current snapshot records the squashed commit, the base
// stays unchanged, gate-review compared records keep history, and the squashed
// message is authored by the main agent (host-provided). Single-commit or empty
// ranges are not rewritten; SVN/P4 are never squashed.
func Seal(root, packageRoot, runID string, skips []string, userRequested bool, squashMessage string) (RunSummary, error) {
	path := RunStatePath(root, runID)
	release, err := acquireStateLock(path)
	if err != nil {
		return RunSummary{}, err
	}
	defer release()
	state, err := LoadRunState(root, runID)
	if err != nil {
		return RunSummary{}, err
	}
	// Seal may rewrite the repository (Git squash) before its terminal state is
	// persisted. Keep the admission registry lock through terminal summary and
	// cleanup as well: a retry after a crash at SEALED is still a workflow write
	// and must not bypass an installation that has since been disabled.
	var admissionUnlock func()
	if strings.TrimSpace(state.AdmissionRegistry) != "" {
		admissionUnlock, err = acquireRegistryLock(state.AdmissionRegistry)
		if err != nil {
			return RunSummary{}, err
		}
		defer admissionUnlock()
		// Re-admit immediately after taking the lock and before any VCS or
		// sidecar mutation.  Checking only at the final state save would allow an
		// already-disabled record to reach the Git squash first.
		if err := verifyRunStateAdmissionLocked(root, state); err != nil {
			return RunSummary{}, err
		}
	} else if err := verifyLegacyStableLauncher(); err != nil {
		return RunSummary{}, err
	}
	if state.Status == "SEALED" {
		return completeTerminalRun(root, state)
	}
	if err := requireActive(state); err != nil {
		return RunSummary{}, err
	}
	catalog, err := requireCurrentDefinitions(root, state, packageRoot)
	if err != nil {
		return RunSummary{}, err
	}
	if err := requireNoPendingInheritance(root, state, catalog); err != nil {
		return RunSummary{}, err
	}
	before, err := resolveNativeSnapshot(root, state.VCS)
	if err != nil {
		return RunSummary{}, err
	}
	if before != state.CurrentSnapshot {
		return RunSummary{}, fmt.Errorf("native VCS identity does not match the current snapshot before aggregation")
	}
	if err := authorizeSealSkips(&state, skips, userRequested); err != nil {
		return RunSummary{}, err
	}
	if err := requireTransition(state, "seal", ""); err != nil {
		if saveErr := saveRunState(root, state, admissionUnlock != nil); saveErr != nil {
			return RunSummary{}, saveErr
		}
		return RunSummary{}, err
	}
	after, err := resolveNativeSnapshot(root, state.VCS)
	if err != nil {
		return RunSummary{}, err
	}
	if after != state.CurrentSnapshot {
		return RunSummary{}, fmt.Errorf("native VCS identity does not match the current snapshot after aggregation")
	}
	// 压缩提交（仅 Git、自动）：校验通过后、落 summary 前执行，作为 seal 的最后一步
	// VCS 操作。压缩前确认工作树干净；单条提交或空范围不操作。
	if state.VCS == "git" {
		count, err := gitCommitCountInRange(root, state.BaseSnapshot, state.CurrentSnapshot)
		if err != nil {
			return RunSummary{}, err
		}
		if count > 1 {
			if err := verifySnapshotReady(root, state.VCS); err != nil {
				return RunSummary{}, fmt.Errorf("seal git squash requires a clean working tree: %w", err)
			}
			message := strings.TrimSpace(squashMessage)
			if message == "" {
				return RunSummary{}, fmt.Errorf("seal git squash requires --squash-message for the combined commit")
			}
			if err := squashGitRangeToBase(root, state.BaseSnapshot, message); err != nil {
				return RunSummary{}, err
			}
			newSnapshot, err := resolveNativeSnapshot(root, state.VCS)
			if err != nil {
				return RunSummary{}, err
			}
			// 最终树不变，所有审查结果对最终树仍成立：快照引用重绑到压缩后的提交，
			// 门审查 compared 记录保持历史（不重写）。
			rebindCurrentSnapshot(&state, newSnapshot)
		}
	}
	sliceInstance := strings.TrimSpace(state.SplitMasterRunID) != ""
	if sliceInstance {
		// 分片实例：封板不产出独立封板文件；成本投影写入 sidecar，供主干
		// （保留总任务实例）最终封板时并入主干封板文件。成本为空（从未记录派发
		// 用量）时不写 sidecar，主干无从并入、也无须清理。
		if state.Cost != nil {
			if err := SaveSliceCost(root, state.SplitMasterRunID, SliceCostRecord{RunID: state.RunID, Cost: state.Cost}); err != nil {
				return RunSummary{}, err
			}
		}
	} else {
		// 主干（保留总任务实例）/单 run：把已封板切片的成本并入最终封板文件。
		// 单 run 无切片，扫描不到引用本 run 的 sidecar，自然为空操作。
		if err := mergeSliceCosts(root, &state); err != nil {
			return RunSummary{}, err
		}
	}
	// Persist the post-squash snapshot and merged cost projection while the run
	// is still ACTIVE, then make the terminal state durable before its summary.
	// A summary failure can be resumed without reopening normal mutations.
	if err := saveRunState(root, state, admissionUnlock != nil); err != nil {
		return RunSummary{}, err
	}
	state.Status = "SEALED"
	if err := saveRunState(root, state, admissionUnlock != nil); err != nil {
		return RunSummary{}, err
	}
	// 黑盒用例 seal 物化（需求 1）：已批准 blackbox 用例从 run-state 物化到主工作区
	// .gates/results/<run-id>.blackbox-cases.md，与 seal ledger 同目录、同交付行为
	// （三 VCS 一致）。分片实例封板不产独立 ledger 文件、不物化。物化读 run-state
	// （单一来源），不依赖隔离工作区残留；CLI 完成、不经 agent 手动合回。
	// 放在 SEALED 持久化之后、completeTerminalRun 收尾之前；物化失败时 SEALED
	// 状态已持久化，重跑 Seal 的幂等守卫会直接 resume completeTerminalRun，因此
	// 物化失败必须在此返回而非静默忽略。
	if !sliceInstance {
		if err := materializeBlackboxCases(root, state); err != nil {
			return RunSummary{}, err
		}
	}
	return completeTerminalRun(root, state)
}

// mergeSliceCosts folds the sealed slice instances' cost sidecars into the
// run's cost projection. It leaves sidecars in the temp run directory until
// the merged state and summary are durable; DeleteRun then removes them with
// the rest of the completed run. A missing/unparseable sidecar is skipped
// best-effort because cost data is display-only and never blocks a seal.
func mergeSliceCosts(root string, state *RunState) error {
	dir := SliceCostDir(root, state.RunID)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".cost.json") {
			continue
		}
		sliceRunID := strings.TrimSuffix(name, ".cost.json")
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		var record SliceCostRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue // 成本仅展示，解析失败不阻塞封板
		}
		if record.Cost == nil {
			continue
		}
		if state.Cost == nil {
			state.Cost = &cost.RunCost{}
		}
		cost.Merge(state.Cost, sliceRunID, record.Cost)
	}
	return nil
}

// materializeBlackboxCases writes the run's approved blackbox QA cases to the
// main worktree's .gates/results/ ledger directory as <run-id>.blackbox-cases.md
// at seal time (需求 1 的合回时点：黑盒用例执行结束才回主干). The file is derived from
// the run state (single source), so it survives the isolation worktree being
// cleared after a PASSing blackbox review. It lives next to the seal ledger
// (.gates/results/<run-id>.json) with the same delivery behavior across
// git/svn/p4. Slice instances do not materialize (only the non-slice Seal branch
// calls this); a run with no approved blackbox cases writes nothing.
func materializeBlackboxCases(root string, state RunState) error {
	var approved []QACase
	for _, testCase := range state.qaModeCases("blackbox") {
		if testCase.ReviewStatus == "PASS" {
			approved = append(approved, testCase)
		}
	}
	if len(approved) == 0 {
		return nil
	}
	path := filepath.Join(lifecycle.CleanRoot(root), ".gates", "results", state.RunID+".blackbox-cases.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, []byte(blackboxCasesMarkdown(state.RunID, approved)), 0o600)
}

func finishRun(root, runID, status string) (RunSummary, error) {
	path := RunStatePath(root, runID)
	release, err := acquireStateLock(path)
	if err != nil {
		return RunSummary{}, err
	}
	defer release()
	state, err := LoadRunState(root, runID)
	if err != nil {
		return RunSummary{}, err
	}
	var admissionUnlock func()
	if strings.TrimSpace(state.AdmissionRegistry) != "" {
		admissionUnlock, err = acquireRegistryLock(state.AdmissionRegistry)
		if err != nil {
			return RunSummary{}, err
		}
		defer admissionUnlock()
		if err := verifyRunStateAdmissionLocked(root, state); err != nil {
			return RunSummary{}, err
		}
	} else if err := verifyLegacyStableLauncher(); err != nil {
		return RunSummary{}, err
	}
	if state.Status == status {
		return completeTerminalRun(root, state)
	}
	if err := requireActive(state); err != nil {
		return RunSummary{}, err
	}
	sliceInstance := strings.TrimSpace(state.SplitMasterRunID) != ""
	if sliceInstance && state.Cost != nil {
		// 分片实例：中止同样不产出独立封板文件；已记录的成本投影写入 sidecar，
		// 供主干最终封板并入（被中止切片的 token 用量仍计入整体交付成本）。
		if err := SaveSliceCost(root, state.SplitMasterRunID, SliceCostRecord{RunID: state.RunID, Cost: state.Cost}); err != nil {
			return RunSummary{}, err
		}
	}
	if state.RetainedOverall {
		if err := mergeSliceCosts(root, &state); err != nil {
			return RunSummary{}, err
		}
	}
	state.Status = status
	if err := saveRunState(root, state, admissionUnlock != nil); err != nil {
		return RunSummary{}, err
	}
	return completeTerminalRun(root, state)
}

// completeTerminalRun persists the retained summary after terminal state is
// durable, then removes the temporary run. If summary persistence fails, the
// terminal state and any cost sidecars remain; repeating the same Seal or Abort
// call resumes here without reopening the workflow to other mutations.
func completeTerminalRun(root string, state RunState) (RunSummary, error) {
	sliceInstance := strings.TrimSpace(state.SplitMasterRunID) != ""
	if !sliceInstance {
		if err := SaveRunSummary(root, state); err != nil {
			return RunSummary{}, err
		}
	}
	summary := runSummary(state)
	if err := DeleteRun(root, state.RunID); err != nil {
		return RunSummary{}, err
	}
	_, _ = CleanupTempRuns(root) // best-effort sweep of residual terminated runs
	return summary, nil
}

func eligibleCarryGates(state RunState) []string {
	var ids []string
	if state.PreRepairSnapshot == "" {
		return ids
	}
	for id, result := range state.Gates {
		if isSelected(state, id) && result.Status == "PASS" && result.Snapshot == state.PreRepairSnapshot {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// resolveCarryBoundary closes a carry boundary after a Carry judgment: a repair
// boundary completes the review wave when every selected result is recorded at
// the current snapshot, while a boundary created by an adopted external change
// that needed no real rerun resolves through clearResolvedCarryBoundary without
// counting a new review wave. Real reruns after an adoption are counted by the
// review that records them.
func resolveCarryBoundary(state *RunState) {
	if isAdoptionBoundary(*state) {
		clearResolvedCarryBoundary(state)
		return
	}
	completeReviewWaveIfReady(state)
}

// clearResolvedCarryBoundary drops a carry boundary once every selected result
// is recorded at the current snapshot and no prior passing gate still awaits a
// Carry decision. An adopted external change that needs no real rerun resolves
// this way without counting a new review wave.
func clearResolvedCarryBoundary(state *RunState) {
	if state.PreRepairSnapshot == "" || !reviewWaveRecorded(*state) {
		return
	}
	if len(eligibleCarryGates(*state)) != 0 {
		return
	}
	state.PreRepairSnapshot = ""
}

// authorizeSealSkips records seal-time skip authorizations for the named
// non-passing selected gates. FAIL and RUNTIME_ERROR results may be skipped;
// a FAIL skip is only allowed once the shared review-wave limit is
// exhausted, unless the user explicitly requested the skip (userRequested),
// which records a distinguishable SEAL-USER origin. A RUNTIME_ERROR is
// always manually skippable and keeps the SEAL origin. The authorizations
// are bound to the current snapshot and cleared by the next repair snapshot.
func authorizeSealSkips(state *RunState, skips []string, userRequested bool) error {
	wanted := map[string]bool{}
	for _, raw := range skips {
		id := strings.TrimSpace(raw)
		if wanted[id] {
			return fmt.Errorf("duplicate Seal skip %q", id)
		}
		if !isSelected(*state, id) {
			return fmt.Errorf("Seal skip %q is not a selected gate", id)
		}
		status := selectedResultStatus(*state, id)
		if status == "PENDING" || status == "" {
			return fmt.Errorf("selected gate %q is PENDING and cannot be skipped", id)
		}
		if status == "PASS" {
			return fmt.Errorf("selected gate %q already passed", id)
		}
		if status == "FAIL" && !userRequested && state.CompletedReviewWaves < effectiveReviewWaveLimit(*state) {
			return fmt.Errorf("selected gate %q cannot be skipped before the review-wave limit is exhausted", id)
		}
		wanted[id] = true
	}
	for id := range wanted {
		origin := "SEAL"
		if userRequested && selectedResultStatus(*state, id) == "FAIL" {
			origin = "SEAL-USER"
		}
		state.SkipAuthorizations[id] = SkipAuthorization{Origin: origin, Status: selectedResultStatus(*state, id), Snapshot: state.CurrentSnapshot}
	}
	return nil
}

// isSealScopedAuthorization reports whether the authorization is a seal-time
// skip authorization bound to the current snapshot: SEAL for limit-exhausted
// and RUNTIME_ERROR skips, SEAL-USER for FAIL skips the user explicitly
// requested before the limit is exhausted.
func isSealScopedAuthorization(authorization SkipAuthorization) bool {
	return authorization.Origin == "SEAL" || authorization.Origin == "SEAL-USER"
}

func requireSelectedResultsResolved(state RunState) error {
	// seal 前按选中 mode 校验各 mode 均已记录执行，避免单个 mode
	// 记录 PASS 而另一 mode 被静默跳过。
	if !selectedQAModesRecorded(state) {
		return fmt.Errorf("QA Execution has not recorded every selected QA mode at the current snapshot")
	}
	for id := range selectedSet(state) {
		status := selectedResultStatus(state, id)
		if status == "PENDING" || status == "" {
			return fmt.Errorf("selected gate %q is PENDING", id)
		}
		snapshot := state.Gates[id].Snapshot
		if isQAMode(id) {
			// QA 结果的快照按该 mode 的有效执行结果读取（per-mode 或合并单派发）。
			snapshot = qaModeResult(state, qaDispatchMode(id)).Snapshot
		}
		if snapshot != state.CurrentSnapshot {
			return fmt.Errorf("selected gate %q has not been verified at the current snapshot", id)
		}
		if status == "PASS" {
			continue
		}
		authorization, ok := state.SkipAuthorizations[id]
		if !ok || !isSealScopedAuthorization(authorization) || authorization.Status != status || authorization.Snapshot != state.CurrentSnapshot {
			return fmt.Errorf("selected gate %q with status %s requires explicit Seal skip authorization", id, status)
		}
	}
	return nil
}

func selectedResultStatus(state RunState, id string) string {
	if isQAMode(id) {
		// QA 模式的状态按该 mode 的有效执行结果读取（per-mode 或合并单派发）。
		return qaModeResult(state, qaDispatchMode(id)).Status
	}
	return state.Gates[id].Status
}
