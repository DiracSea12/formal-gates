package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestInvalidateResetsPerModeReviewDesignAndCaseStatus covers a
// meaning-changed requirement invalidation must void the per-mode review/design
// authoritative results AND reset every mode's case ReviewStatus back to PENDING,
// so the snapshot blackbox gate does not read a stale per-mode PASS and release
// (the defect fixes — invalidate only reset Actions, which no longer carry
// qa-review/qa-design after).
func TestInvalidateResetsPerModeReviewDesignAndCaseStatus(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := perModeReadyDelivery(t, root, pkg, "invalidate-per-mode")
	// 两个 mode 的 review/design 均为 PASS、用例均已批准。
	bbCases := state.qaModeCases("blackbox")
	wbCases := state.qaModeCases("whitebox")
	if state.qaDesign("blackbox").Status != "PASS" || state.qaDesign("whitebox").Status != "PASS" {
		t.Fatalf("per-mode designs not PASS before invalidation: bb=%#v wb=%#v", state.qaDesign("blackbox"), state.qaDesign("whitebox"))
	}
	for _, testCase := range append(bbCases, wbCases...) {
		if testCase.ReviewStatus != "PASS" {
			t.Fatalf("case %s not PASS before invalidation: %#v", testCase.ID, testCase)
		}
	}
	// 语义变更（meaning changed）：作废全部结果。先改写需求文档使修订生效。
	writeTestFile(t, filepath.Join(root, "requirements.md"), "changed requirement\n")
	commitAll(t, root, "change requirement")
	state, err := UpdateRequirement(root, pkg, state.RunID, "", false, "changed", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.qaReview("blackbox").Status != "" || state.qaDesign("blackbox").Status != "" {
		t.Fatalf("blackbox per-mode authority not voided: review=%#v design=%#v", state.qaReview("blackbox"), state.qaDesign("blackbox"))
	}
	if state.qaReview("whitebox").Status != "" || state.qaDesign("whitebox").Status != "" {
		t.Fatalf("whitebox per-mode authority not voided: review=%#v design=%#v", state.qaReview("whitebox"), state.qaDesign("whitebox"))
	}
	// 各 mode 用例 ReviewStatus 置回 PENDING：快照黑盒门读到旧 PASS 不放行。
	for _, testCase := range append(state.qaModeCases("blackbox"), state.qaModeCases("whitebox")...) {
		if testCase.ReviewStatus != "PENDING" {
			t.Fatalf("case %s reviewStatus not reset to PENDING after invalidation: %#v", testCase.ID, testCase)
		}
	}
	// 快照黑盒门不放行：没有旧 PASS 可用。
	if blackboxReviewPassed(state) {
		t.Fatal("blackbox snapshot gate released after invalidation with no valid review")
	}
}

// TestStateIntegrityRoundTripRejectsHandEditAndSkipsLegacy covers the run
// state file carries a sha256 StateIntegrity that SaveRunState writes and
// LoadRunState verifies — a hand-edit outside the CLI is rejected with the hard
// message, an untouched round-trip loads, and a legacy state (no field) skips the
// check.
func TestStateIntegrityRoundTripRejectsHandEditAndSkipsLegacy(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := mustStart(t, root, pkg, "integrity")
	// 合法 round-trip：保存后原样加载成功。
	if loaded, err := LoadRunState(root, state.RunID); err != nil {
		t.Fatalf("round-trip load failed: %v", err)
	} else if loaded.StateIntegrity == "" {
		t.Fatal("saved state has no integrity field")
	}
	// 手工改写：任一字段被外部修改后加载硬拒绝。
	path := RunStatePath(root, state.RunID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	modified := strings.Replace(string(data), `"currentSnapshot": "`, `"currentSnapshot": "x`, 1)
	if modified == string(data) {
		t.Fatal("test harness could not perturb the state file")
	}
	if err := os.WriteFile(path, []byte(modified), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRunState(root, state.RunID); err == nil || !strings.Contains(err.Error(), "state was modified outside the CLI") {
		t.Fatalf("hand-modified state was not rejected: %v", err)
	}
	// legacy 状态（无 stateIntegrity 字段）跳过校验：加载成功。
	state2 := mustStart(t, root, pkg, "integrity-legacy")
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stateBytes(t, root, state2.RunID)), &decoded); err != nil {
		t.Fatal(err)
	}
	delete(decoded, "stateIntegrity")
	rewritten, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RunStatePath(root, state2.RunID), append(rewritten, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRunState(root, state2.RunID); err != nil {
		t.Fatalf("legacy state without integrity was rejected: %v", err)
	}
}

// TestStateIntegrityWriteIsFast covers <5ms hashing budget: the sha256
// computation over the normalized state must stay cheap. The full write also
// includes fsync + atomic replace, which is dominated by the filesystem, so the
// bound is measured on the hash path rather than the disk write.
func TestStateIntegrityWriteIsFast(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := mustStart(t, root, pkg, "integrity-fast")
	start := time.Now()
	for i := 0; i < 100; i++ {
		state.StateIntegrity = ""
		data, err := jsonMarshalIndent(state)
		if err != nil {
			t.Fatal(err)
		}
		stateIntegrityHashBytes(data)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("state integrity hashing too slow: %s for 100 hashes", elapsed)
	}
}

// jsonMarshalIndent normalizes the state exactly as SaveRunState does.
func jsonMarshalIndent(state RunState) ([]byte, error) {
	return json.MarshalIndent(state, "", "  ")
}

// stateIntegrityHashBytes computes the sha256 over the normalized state bytes.
func stateIntegrityHashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// TestStartCurrentSnapshotAdoptsAncestor covers start --current-snapshot
// pins the current snapshot to a native ancestor (equal or ancestor of HEAD)
// instead of HEAD, so already-committed development work becomes a pending
// snapshot. A non-ancestor identity is rejected.
func TestStartCurrentSnapshotAdoptsAncestor(t *testing.T) {
	root, pkg := workflowFixture(t)
	// 在 HEAD 上新增一个提交，使原生 HEAD 前进；用其父作为 --current-snapshot。
	writeTestFile(t, filepath.Join(root, "dev-commit.txt"), "dev\n")
	commitAll(t, root, "development commit")
	head := gitHead(t, root)
	parent := runGit(t, root, "rev-parse", head+"^")
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "current-snapshot", Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", CurrentSnapshot: parent, Split: "no"})
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentSnapshot != parent || state.BaseSnapshot != parent {
		t.Fatalf("current snapshot not pinned to the ancestor: current=%s base=%s want=%s", state.CurrentSnapshot, state.BaseSnapshot, parent)
	}
	// 非祖先身份被拒。
	if _, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "current-snapshot-bad", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", CurrentSnapshot: "0000000000000000000000000000000000000000", Split: "no"}); err == nil {
		t.Fatal("non-ancestor current snapshot was accepted")
	}
}

// TestIncrementalReviewScopedItemsJudgedAndAcceptedProtected covers
// prepare-action --scope declares the review items (PENDING, must be judged);
// record-action judges every pending item; a PASS item cannot be re-judged; a
// FAIL item requires a finding; meaning-preserved rebinding keeps the table while
// meaning-changed clears it.
func TestIncrementalReviewScopedItemsJudgedAndAcceptedProtected(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "incremental-review"))

	// Part 1 产品审：声明两个需求项为本次审查范围。
	prompt, err := PrepareAction(root, pkg, state.RunID, "product-review", "", false, "", "item-A", "item-B")
	if err != nil {
		t.Fatal(err)
	}
	state, _ = LoadRunState(root, state.RunID)
	if items := state.ReviewItemsByAction["product-review"]; items["item-A"].Status != "PENDING" || items["item-B"].Status != "PENDING" {
		t.Fatalf("scoped items not set PENDING: %#v", items)
	}
	if !strings.Contains(prompt, "item-A") || !strings.Contains(prompt, "pending item") {
		t.Fatalf("incremental review prompt missing pending items: %s", prompt)
	}
	dispatchID := openDispatchID(state, "action", "product-review")
	state, err = ClaimDispatch(root, pkg, state.RunID, dispatchID, "inc-product-review")
	if err != nil {
		t.Fatal(err)
	}
	// 只判 item-A：item-B 仍是 PENDING → 拒绝（所有 PENDING 必须全判）。
	if _, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "PASS", "", nil, false, "", ReviewItemInput{Key: "item-A", Status: "PASS"}); err == nil || !strings.Contains(err.Error(), "item-B") {
		t.Fatalf("partial item judgment was accepted: %v", err)
	}
	// 全判：item-A PASS、item-B FAIL（带 finding）。
	state, err = RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "FAIL", "", []FindingInput{{Severity: "P1", Message: "item-B premised wrong"}}, false, "", ReviewItemInput{Key: "item-A", Status: "PASS"}, ReviewItemInput{Key: "item-B", Status: "FAIL", Reason: "item-B premised wrong"})
	if err != nil {
		t.Fatal(err)
	}
	if state.ReviewItemsByAction["product-review"]["item-A"].Status != "PASS" || state.ReviewItemsByAction["product-review"]["item-B"].Status != "FAIL" {
		t.Fatalf("item judgments not recorded: %#v", state.ReviewItemsByAction["product-review"])
	}
	// FAIL 项必须带 finding（reason）→ 空 reason 被拒。
	state2 := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "incremental-review-fail-no-finding"))
	_, _ = PrepareAction(root, pkg, state2.RunID, "product-review", "", false, "", "item-X")
	state2, _ = LoadRunState(root, state2.RunID)
	dispatchID2 := openDispatchID(state2, "action", "product-review")
	state2, _ = ClaimDispatch(root, pkg, state2.RunID, dispatchID2, "inc-product-review-fail")
	if _, err := RecordAction(root, pkg, state2.RunID, "product-review", dispatchID2, "FAIL", "", nil, false, "", ReviewItemInput{Key: "item-X", Status: "FAIL"}); err == nil || !strings.Contains(err.Error(), "finding") {
		t.Fatalf("FAIL item without a finding was accepted: %v", err)
	}
	// PASS 项不可被重判（除非主代理下次 prepare 显式重新声明变更）。
	if _, err := PrepareAction(root, pkg, state.RunID, "product-review", "", false, ""); err != nil {
		t.Fatal(err)
	}
	state, _ = LoadRunState(root, state.RunID)
	dispatchID = openDispatchID(state, "action", "product-review")
	state, err = ClaimDispatch(root, pkg, state.RunID, dispatchID, "inc-product-review-again")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "PASS", "", nil, false, "", ReviewItemInput{Key: "item-A", Status: "FAIL", Reason: "re-open"}); err == nil || !strings.Contains(err.Error(), "cannot be re-judged") {
		t.Fatalf("re-judging an accepted PASS item was accepted: %v", err)
	}
}

// TestIncrementalReviewAllPassedTableAllowsEmptyDecision covers the dead-end
// fix (P2-1): when the item table is non-empty but every item is already PASS, a
// re-review prepare without --scope generates the "no pending items" prompt, and
// record-action with no --item decisions must be accepted rather than rejected —
// otherwise the re-review round cannot be recorded.
func TestIncrementalReviewAllPassedTableAllowsEmptyDecision(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "incremental-deadend"))

	// 第一轮：声明 item-A，审查者判 PASS，但产品审整体 FAIL（带 P1 finding）。
	if _, err := PrepareAction(root, pkg, state.RunID, "product-review", "", false, "", "item-A"); err != nil {
		t.Fatal(err)
	}
	state, _ = LoadRunState(root, state.RunID)
	dispatchID := openDispatchID(state, "action", "product-review")
	state, err := ClaimDispatch(root, pkg, state.RunID, dispatchID, "deadend-round-1")
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "FAIL", "", []FindingInput{{Severity: "P1", Message: "needs tweak"}}, false, "", ReviewItemInput{Key: "item-A", Status: "PASS"})
	if err != nil {
		t.Fatal(err)
	}
	if state.ReviewItemsByAction["product-review"]["item-A"].Status != "PASS" {
		t.Fatalf("item-A not PASS after round 1: %#v", state.ReviewItemsByAction["product-review"])
	}
	// 用户驳回该 P1（作废不阻塞）；逐项表仍非空全 PASS。
	state, err = RecordSettledFindings(root, pkg, state.RunID, "product-review", nil, []string{"needs tweak"})
	if err != nil {
		t.Fatal(err)
	}

	// 第二轮：无 --scope 的复审 prepare——逐项表非空全 PASS → "无待定项可判"提示。
	prompt, err := PrepareAction(root, pkg, state.RunID, "product-review", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "no pending items") {
		t.Fatalf("re-review prompt did not say no pending items: %s", prompt)
	}
	state, _ = LoadRunState(root, state.RunID)
	dispatchID = openDispatchID(state, "action", "product-review")
	state, err = ClaimDispatch(root, pkg, state.RunID, dispatchID, "deadend-round-2")
	if err != nil {
		t.Fatal(err)
	}
	// 审查者按提示不下发任何 --item：record-action PASS 必须被接受（修复前的死路是被拒）。
	if _, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "PASS", "", nil, false, ""); err != nil {
		t.Fatalf("record with empty item decisions on an all-passed table was rejected: %v", err)
	}
}

// TestIncrementalReviewMeaningChangedClearsTable covers meaning-preserved
// rebinding keeps the item table, while meaning-changed invalidation clears it
// (full re-review).
func TestIncrementalReviewMeaningChangedClearsTable(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "incremental-clear"))
	_, _ = PrepareAction(root, pkg, state.RunID, "product-review", "", false, "", "item-A")
	state, _ = LoadRunState(root, state.RunID)
	if state.ReviewItemsByAction["product-review"]["item-A"].Status != "PENDING" {
		t.Fatalf("scoped item not recorded: %#v", state.ReviewItemsByAction)
	}
	writeTestFile(t, filepath.Join(root, "requirements.md"), "changed requirement\n")
	commitAll(t, root, "change requirement")
	state, err := UpdateRequirement(root, pkg, state.RunID, "", false, "changed", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.ReviewItemsByAction["product-review"]) != 0 {
		t.Fatalf("meaning-changed did not clear the item table: %#v", state.ReviewItemsByAction["product-review"])
	}
}
