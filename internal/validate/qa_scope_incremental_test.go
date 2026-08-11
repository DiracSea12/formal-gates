package validate

import (
	"reflect"
	"strings"
	"testing"
)

// recordFailingQARound records a merged QA execution FAIL plus the quality gate
// FAIL (the architecture gate stays carried PASS across repair waves, so it is not
// re-recorded). Used for rerun waves where failingQAExecution would re-record the
// already-carried architecture gate.
func recordFailingQARound(t *testing.T, root, pkg string, state RunState, failing map[string]bool, suffix string) RunState {
	t.Helper()
	qaDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err := RecordQAExecution(root, pkg, state.RunID, qaDispatch, executionOutcomes(state.allQACases(), failing), "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordGateResult(t, root, pkg, state, "quality", "failing-quality-"+suffix, "FAIL", "", []FindingInput{{Severity: "P1", Message: "blocker"}})
	return state
}

// TestRecordExecutionScopeRejectsInvalidInput asserts the mechanical scope
// validator rejections directly: invalid mode, invalid decision, empty AFFECTED
// subset, and duplicate AFFECTED case id.
func TestRecordExecutionScopeRejectsInvalidInput(t *testing.T) {
	state := RunState{QAExecutionByMode: map[string]QAExecutionResult{}, ExecutionScopes: map[string]QAExecutionScope{}}
	if err := recordExecutionScope(&state, "BOGUS", "FULL", nil, "", "PREPARE", "s1", QAExecutionResult{}); err == nil || !strings.Contains(err.Error(), "invalid QA execution scope mode") {
		t.Fatalf("mode=BOGUS was accepted: %v", err)
	}
	if err := recordExecutionScope(&state, "blackbox", "BOGUS", nil, "", "PREPARE", "s1", QAExecutionResult{}); err == nil || !strings.Contains(err.Error(), "must be FULL or AFFECTED") {
		t.Fatalf("decision=BOGUS was accepted: %v", err)
	}
	if err := recordExecutionScope(&state, "blackbox", "AFFECTED", nil, "", "PREPARE", "s1", QAExecutionResult{}); err == nil || !strings.Contains(err.Error(), "non-empty") {
		t.Fatalf("empty AFFECTED subset was accepted: %v", err)
	}
	if err := recordExecutionScope(&state, "blackbox", "AFFECTED", []string{"CASE-001", "CASE-001"}, "", "PREPARE", "s1", QAExecutionResult{}); err == nil || !strings.Contains(err.Error(), "duplicate case") {
		t.Fatalf("duplicate AFFECTED case id was accepted: %v", err)
	}
}

// TestScopeFULLOverwritesAndAffectedRequiresApprovedCases asserts that recording a
// new scope decision for a mode overwrites the previous one (latest wins), and
// that an AFFECTED subset must reference approved cases of the mode.
func TestScopeFULLOverwritesAndAffectedRequiresApprovedCases(t *testing.T) {
	state := RunState{
		QAExecutionByMode: map[string]QAExecutionResult{},
		ExecutionScopes:   map[string]QAExecutionScope{},
		QACasesByMode:     map[string][]QACase{"blackbox": {{ID: "CASE-001", Mode: "blackbox", ReviewStatus: "PASS"}}},
	}
	if err := recordExecutionScope(&state, "blackbox", "FULL", nil, "", "PREPARE", "s1", QAExecutionResult{}); err != nil {
		t.Fatal(err)
	}
	// 最新决策覆盖：FULL 被 AFFECTED 取代。
	if err := recordExecutionScope(&state, "blackbox", "AFFECTED", []string{"CASE-001"}, "", "PREPARE", "s1", QAExecutionResult{}); err != nil {
		t.Fatal(err)
	}
	if sc := state.ExecutionScopes["blackbox"]; sc.Decision != "AFFECTED" || sc.BaseSnapshot != "s1" || sc.Source != "PREPARE" || sc.Origin != "USER" {
		t.Fatalf("overwritten scope=%#v", sc)
	}
	// 未批准用例拒绝。
	if err := recordExecutionScope(&state, "blackbox", "AFFECTED", []string{"CASE-999"}, "", "PREPARE", "s1", QAExecutionResult{}); err == nil || !strings.Contains(err.Error(), "not an approved") {
		t.Fatalf("non-approved AFFECTED case was accepted: %v", err)
	}
}

// TestScopeBaseMismatchRejectedAtPrepareAndFallsBackToFull drives the full flow:
// a recorded scope whose BaseSnapshot no longer matches the current prior (a newer
// repair advanced the prior) is rejected at prepare, and the AFFECTED scope no
// longer applies so the required set falls back to the full approved set.
func TestScopeBaseMismatchRejectedAtPrepareAndFallsBackToFull(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "scope-base-mismatch")
	// 波 1：合并 QA FAIL（CASE-002）+ 架构 PASS + 质量 FAIL，波次完成。
	state = failingQAExecution(t, root, pkg, state, map[string]bool{"CASE-002": true}, "base1")
	// 修复 → S2，prior = S1 FAIL；架构 PASS 被 carry。
	state = advanceRepairWithCarry(t, root, pkg, state, "base1-repair")
	// 记录 AFFECTED scope（base = S1 上一轮）。
	state, err := RecordExecutionScope(root, pkg, state.RunID, "", "AFFECTED", []string{"CASE-001", "CASE-002"}, "")
	if err != nil {
		t.Fatal(err)
	}
	// 波 2 重跑：scope base（S1）匹配上一轮 → 放行，仍 FAIL。
	state = recordFailingQARound(t, root, pkg, state, map[string]bool{"CASE-002": true}, "base2")
	// 再修复 → S3，prior = S2 FAIL；旧 scope base（S1）不再匹配 → prepare 拒绝。
	state = advanceRepairWithCarry(t, root, pkg, state, "base2-repair")
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "", false, ""); err == nil || !strings.Contains(err.Error(), "requires a scope decision") {
		t.Fatalf("stale-base scope was not rejected at prepare: %v", err)
	}
	// AFFECTED 不生效 → 需执行集回退全量（FULL 语义）。
	if _, ok := qaExecutionAffectedScope(state, ""); ok {
		t.Fatalf("stale-base AFFECTED scope still applied")
	}
	if required := qaExecutionRequiredCases(state, ""); len(required) != len(state.qaModeCases("")) {
		t.Fatalf("stale-base scope did not fall back to the full set: %d != %d", len(required), len(state.qaModeCases("")))
	}
}

// TestFULLRerunWithoutAffectedUsesFullSet asserts that a FULL rerun scope yields
// the full approved required set (no subset).
func TestFULLRerunWithoutAffectedUsesFullSet(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "full-no-affected")
	state = failingQAExecution(t, root, pkg, state, map[string]bool{"CASE-002": true}, "full1")
	state = advanceRepairWithCarry(t, root, pkg, state, "full1-repair")
	if _, err := RecordExecutionScope(root, pkg, state.RunID, "", "FULL", nil, ""); err != nil {
		t.Fatal(err)
	}
	if required := qaExecutionRequiredCases(state, ""); len(required) != len(state.qaModeCases("")) {
		t.Fatalf("FULL required set=%d want=%d", len(required), len(state.qaModeCases("")))
	}
	if _, ok := qaExecutionAffectedScope(state, ""); ok {
		t.Fatalf("FULL scope was treated as AFFECTED")
	}
}

// TestExecutedAllPassYieldsPASS asserts a QA execution round where every executed
// case passes records an overall PASS (and no FAIL finding).
func TestExecutedAllPassYieldsPASS(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "exec-all-pass")
	qaDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	var err error
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, passingExecution(state.allQACases()), "")
	if err != nil {
		t.Fatal(err)
	}
	result := state.qaExecution("")
	if result.Status != "PASS" || len(result.Findings) != 0 || len(result.Cases) != len(state.allQACases()) {
		t.Fatalf("all-pass execution result=%#v", result)
	}
}

// TestSameModeQARecordRejected asserts re-recording a mode that already has an
// authoritative execution result at the current snapshot is rejected.
func TestSameModeQARecordRejected(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "same-mode-dedup")
	blackbox := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	var err error
	state, err = RecordQAExecution(root, pkg, state.RunID, blackbox, passingExecution(state.qaModeCases("blackbox")), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "blackbox", false, ""); err == nil || !strings.Contains(err.Error(), "already has an authoritative") {
		t.Fatalf("same-mode re-prepare was accepted: %v", err)
	}
}

// TestInvalidateRequirementClearsScopesAndPriors asserts a meaning-changing
// requirement invalidation clears every recorded execution scope and preserved
// prior authoritative result.
func TestInvalidateRequirementClearsScopesAndPriors(t *testing.T) {
	prior := QAExecutionResult{Status: "FAIL", Snapshot: "s1"}
	state := RunState{
		ExecutionScopes:        map[string]QAExecutionScope{"blackbox": {Decision: "FULL", Mode: "blackbox", BaseSnapshot: "s1", Origin: "USER", Source: "PREPARE"}},
		PriorQAExecutionByMode: map[string]*QAExecutionResult{"blackbox": &prior},
		QAExecutionByMode:      map[string]QAExecutionResult{"blackbox": {Status: "PENDING"}},
		SkipAuthorizations:     map[string]SkipAuthorization{},
		SelectedGates:          []string{},
		NeedsReReview:          map[string]string{},
		ReReviewDispatch:       map[string]string{},
		ReviewOverrides:        map[string]string{},
		SettledFindings:        map[string][]SettledFinding{},
		Actions:                pendingRequirementActions(),
	}
	invalidateRequirementResults(&state, []string{"quality"})
	if len(state.ExecutionScopes) != 0 || len(state.PriorQAExecutionByMode) != 0 || len(state.QAExecutionByMode) != 0 {
		t.Fatalf("requirement invalidation did not clear scopes/priors: %#v / %#v", state.ExecutionScopes, state.PriorQAExecutionByMode)
	}
}

// TestQaRerunModesAndBundleScopesUnit asserts qaRerunModes and bundleRerunScopes
// directly: per-mode FAIL triggers every recorded mode as a rerun mode, the merged
// execution yields the merged mode only, and bundleRerunScopes records an inline
// FULL decision with Source AUTHORIZE_REPAIR (or a host-supplied override).
func TestQaRerunModesAndBundleScopesUnit(t *testing.T) {
	perMode := RunState{
		SelectedGates:     []string{blackboxQAID, whiteboxQAID},
		CurrentSnapshot:   "s2",
		QAExecutionByMode: map[string]QAExecutionResult{"blackbox": {Status: "FAIL", Snapshot: "s2"}, "whitebox": {Status: "PASS", Snapshot: "s2"}},
		ExecutionScopes:   map[string]QAExecutionScope{},
	}
	if got := qaRerunModes(perMode); !reflect.DeepEqual(got, []string{"blackbox", "whitebox"}) {
		t.Fatalf("qaRerunModes=%v want=[blackbox whitebox]", got)
	}
	if err := bundleRerunScopes(&perMode, []QAScopeInput{{Mode: "blackbox", Decision: "FULL"}, {Mode: "whitebox", Decision: "FULL"}}); err != nil {
		t.Fatal(err)
	}
	if sc := perMode.ExecutionScopes["blackbox"]; sc.Decision != "FULL" || sc.Source != scopeSourceAuthorizeRepair || sc.BaseSnapshot != "s2" {
		t.Fatalf("bundled blackbox scope=%#v", sc)
	}
	// 合并执行：合并 "" FAIL → 只返回合并 mode。
	merged := RunState{
		SelectedGates:     []string{blackboxQAID},
		CurrentSnapshot:   "s2",
		QAExecutionByMode: map[string]QAExecutionResult{"": {Status: "FAIL", Snapshot: "s2"}},
		ExecutionScopes:   map[string]QAExecutionScope{},
	}
	if got := qaRerunModes(merged); !reflect.DeepEqual(got, []string{""}) {
		t.Fatalf("merged qaRerunModes=%v want=['']", got)
	}
	if err := bundleRerunScopes(&merged, []QAScopeInput{{Mode: "", Decision: "FULL"}}); err != nil {
		t.Fatal(err)
	}
	if sc := merged.ExecutionScopes[""]; sc.Decision != "FULL" || sc.Source != scopeSourceAuthorizeRepair {
		t.Fatalf("merged bundled scope=%#v", sc)
	}
}

// TestNoRepairableBlockerWhenNothingFailed asserts hasRepairableBlocker is false
// when every selected QA mode and gate result is PASS at the current snapshot.
func TestNoRepairableBlockerWhenNothingFailed(t *testing.T) {
	state := RunState{
		SelectedGates:     []string{blackboxQAID, whiteboxQAID, "quality"},
		CurrentSnapshot:   "s2",
		QAExecutionByMode: map[string]QAExecutionResult{"blackbox": {Status: "PASS", Snapshot: "s2"}, "whitebox": {Status: "PASS", Snapshot: "s2"}},
		Gates:             map[string]GateResult{"quality": {Status: "PASS", Snapshot: "s2"}},
	}
	if hasRepairableBlocker(state) {
		t.Fatalf("repairable blocker reported with all-PASS results: %#v", state)
	}
}

// TestReviewCompletionAllowsDesignReRecord asserts after a qa-review round
// completes (PASS), re-recording qa-design for the same mode is allowed (the design
// is unlocked once no review dispatch is in flight).
func TestReviewCompletionAllowsDesignReRecord(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := beginQA(t, root, pkg, "review-rerecord")
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "rr-reviewer")
	var err error
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, baselineCases(), "")
	if err != nil {
		t.Fatalf("design re-record after review completion was rejected: %v", err)
	}
	if state.qaDesign("").Status != "PASS" {
		t.Fatalf("design re-record did not pass: %#v", state.qaDesign(""))
	}
}
