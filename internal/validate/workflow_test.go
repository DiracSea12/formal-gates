package validate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClarificationConfirmationAndRouteOrderAreHardGates(t *testing.T) {
	root, pkg := workflowFixture(t)
	if _, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "preconfirmed", Flow: "formal", RequirementSource: "requirements.md", RequirementConfirmed: true, VCS: "git", BaseSnapshot: "base"}); err == nil {
		t.Fatal("pre-confirmed start was accepted")
	}
	if _, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "advanced", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", BaseSnapshot: "base", CurrentSnapshot: "delivery"}); err == nil {
		t.Fatal("advanced start snapshot was accepted")
	}
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "ordered", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", BaseSnapshot: "base"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateRequirement(root, pkg, state.RunID, "", true, ""); err == nil || !strings.Contains(err.Error(), "Clarification") {
		t.Fatalf("requirement confirmation bypassed clarification: %v", err)
	}
	if _, err := SetRoute(root, pkg, state.RunID, "full", nil); err == nil {
		t.Fatal("route was accepted before confirmation")
	}
	if _, err := RouteCandidates(root, pkg, state.RunID); err == nil {
		t.Fatal("route candidates were exposed before confirmation")
	}
	state = recordClarificationAndConfirm(t, root, pkg, state)
	candidates, err := RouteCandidates(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"qa", "architecture", "quality"}; !reflect.DeepEqual(candidates, want) {
		t.Fatalf("route candidates=%v want=%v", candidates, want)
	}
	state, err = SetRoute(root, pkg, state.RunID, "custom", []string{"quality"})
	if err != nil {
		t.Fatal(err)
	}
	if state.RouteMode != "custom" || !reflect.DeepEqual(state.SelectedGates, []string{"quality"}) {
		t.Fatalf("route=%#v", state)
	}
	for _, id := range []string{"qa", "architecture"} {
		if got := state.SkipAuthorizations[id]; got.Origin != "ROUTE" || got.Status != "UNSELECTED" {
			t.Fatalf("route skip %s=%#v", id, got)
		}
	}
	if _, err := SetRoute(root, pkg, state.RunID, "none", nil); err == nil {
		t.Fatal("second route decision was accepted")
	}
}

func TestDirectTransitionsRespectSelectionAndDevelopmentSnapshot(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := beginRoute(t, root, pkg, "direct", "custom", []string{"quality"})
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "base"); err == nil || !strings.Contains(err.Error(), "Start Readiness") {
		t.Fatalf("development bypassed readiness: %v", err)
	}
	state = recordReadiness(t, root, pkg, state)
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "base"); err != nil {
		t.Fatalf("non-QA development was blocked by absent QA cases: %v", err)
	}
	state = advance(t, root, pkg, state, "delivery")
	if _, err := PrepareGate(root, pkg, state.RunID, "architecture", "delivery"); err == nil || !strings.Contains(err.Error(), "not selected") {
		t.Fatalf("unselected gate was prepared: %v", err)
	}
	if _, err := RecordQADesign(root, pkg, state.RunID, []QACaseInput{{Description: "late", Procedure: "late", Oracle: "late"}}, "", state.RequirementRevision, state.CatalogRevision); err == nil || !strings.Contains(err.Error(), "not selected") {
		t.Fatalf("unselected QA Design was recorded: %v", err)
	}
	if _, err := AddRouteGates(root, pkg, state.RunID, []string{"qa"}); err == nil || !strings.Contains(err.Error(), "after development") {
		t.Fatalf("late QA addition was accepted: %v", err)
	}
	state, err := AddRouteGates(root, pkg, state.RunID, []string{"architecture"})
	if err != nil {
		t.Fatalf("post-development gate addition was rejected: %v", err)
	}
	if !reflect.DeepEqual(state.SelectedGates, []string{"architecture", "quality"}) {
		t.Fatalf("added route order=%v", state.SelectedGates)
	}
}

func TestRequirementRevisionWaitsForExplicitSemanticClassification(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "requirement-change", "full", nil)
	state = recordAllPassing(t, root, pkg, state)
	oldRevision := state.RequirementRevision
	writeTestFile(t, filepath.Join(root, "requirements.md"), "meaning-preserving wording\n")
	resumed, classificationRequired, err := Resume(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !classificationRequired || resumed.RequirementRevision != oldRevision || resumed.Gates["quality"].Status != "PASS" {
		t.Fatalf("resume mutated results before classification: %#v", resumed)
	}
	state, err = UpdateRequirement(root, pkg, state.RunID, "", false, "preserved")
	if err != nil {
		t.Fatal(err)
	}
	if !state.RequirementConfirmed || state.RequirementRevision == oldRevision || state.Gates["quality"].Status != "PASS" {
		t.Fatalf("preserving classification discarded results: %#v", state)
	}
	writeTestFile(t, filepath.Join(root, "requirements.md"), "meaning changed\n")
	state, err = UpdateRequirement(root, pkg, state.RunID, "", false, "changed")
	if err != nil {
		t.Fatal(err)
	}
	if state.RequirementConfirmed || state.RouteMode != "full" || len(state.SelectedGates) != 3 || state.Actions["development-worker"].Status != "PENDING" {
		t.Fatalf("meaning change did not invalidate dependent results while preserving route: %#v", state)
	}
	for id, result := range state.Gates {
		if result.Status != "PENDING" {
			t.Fatalf("gate %s survived meaning change: %#v", id, result)
		}
	}
}

func TestGateSeverityControlsSemanticStatusWithoutMutationOnRejection(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "severity", "custom", []string{"quality"})
	before, err := os.ReadFile(RunStatePath(root, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	invalid := []struct {
		status   string
		severity string
	}{
		{status: "FAIL", severity: "P2"},
		{status: "PASS", severity: "P1"},
		{status: "FAIL", severity: "P3"},
	}
	for _, test := range invalid {
		if _, err := RecordGate(root, pkg, state.RunID, "quality", test.status, "", []FindingInput{{Severity: test.severity, Message: "finding"}}, state.RequirementRevision, state.CatalogRevision, "delivery", "delivery"); err == nil {
			t.Fatalf("accepted %s with %s", test.status, test.severity)
		}
		after, readErr := os.ReadFile(RunStatePath(root, state.RunID))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(after) != string(before) {
			t.Fatal("rejected severity result changed state")
		}
	}
	state, err = RecordGate(root, pkg, state.RunID, "quality", "PASS", "", []FindingInput{{Severity: "P2", Message: "recommendation"}}, state.RequirementRevision, state.CatalogRevision, "delivery", "delivery")
	if err != nil {
		t.Fatal(err)
	}
	if state.Gates["quality"].Status != "PASS" || state.Gates["quality"].Findings[0].Severity != "P2" {
		t.Fatalf("P2 PASS not retained: %#v", state.Gates["quality"])
	}
}

func TestSharedRepairCyclesIncludeP2AndRerunQA(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "cycles", "full", nil)
	state = recordQA(t, root, pkg, state, "PASS")
	state = recordGate(t, root, pkg, state, "architecture", "PASS", nil)
	state = recordGate(t, root, pkg, state, "quality", "FAIL", []FindingInput{{Severity: "P1", Message: "blocker"}, {Severity: "P2", Message: "same-wave cleanup"}})
	prompt, err := PrepareAction(root, pkg, state.RunID, "development-worker", state.CurrentSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "quality P1: blocker") || !strings.Contains(prompt, "quality P2: same-wave cleanup") {
		t.Fatalf("repair prompt omitted wave findings: %s", prompt)
	}
	for cycle := 1; cycle <= automaticRepairLimit; cycle++ {
		next := "repair-" + string(rune('0'+cycle))
		state = advance(t, root, pkg, state, next)
		if state.QAExecution.Status != "PENDING" {
			t.Fatalf("QA was not reset for cycle %d: %#v", cycle, state.QAExecution)
		}
		state, err = RecordCarry(root, pkg, state.RunID, []CarryInput{{GateID: "architecture", Decision: "INHERIT", Message: "unaffected"}}, "", state.RequirementRevision, state.CatalogRevision, next, next)
		if err != nil {
			t.Fatal(err)
		}
		state = recordQA(t, root, pkg, state, "PASS")
		state = recordGate(t, root, pkg, state, "quality", "FAIL", []FindingInput{{Severity: "P1", Message: "still failing"}})
		if state.CompletedRepairCycles != cycle || state.PreRepairSnapshot != "" {
			t.Fatalf("completed cycles after wave %d: %#v", cycle, state)
		}
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", state.CurrentSnapshot); err == nil || !strings.Contains(err.Error(), "exhausted") {
		t.Fatalf("repair limit was not enforced: %v", err)
	}
	state, err = AuthorizeExtraRepair(root, pkg, state.RunID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if state.ExtraRepairCycles != 1 {
		t.Fatalf("extra cycles=%d", state.ExtraRepairCycles)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", state.CurrentSnapshot); err != nil {
		t.Fatalf("authorized repair remained blocked: %v", err)
	}
}

func TestRuntimeAndPendingResultsRequireExplicitSealResolution(t *testing.T) {
	root, pkg := workflowFixture(t)
	pending := readyDelivery(t, root, pkg, "pending-seal", "custom", []string{"quality"})
	if _, err := Seal(root, pkg, pending.RunID, "delivery", "delivery", nil); err == nil || !strings.Contains(err.Error(), "PENDING") {
		t.Fatalf("pending selected gate sealed: %v", err)
	}
	runtime := readyDelivery(t, root, pkg, "runtime-seal", "custom", []string{"quality"})
	var err error
	runtime, err = RecordGate(root, pkg, runtime.RunID, "quality", "RUNTIME_ERROR", "review unavailable", nil, runtime.RequirementRevision, runtime.CatalogRevision, "delivery", "delivery")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Seal(root, pkg, runtime.RunID, "delivery", "delivery", nil); err == nil || !strings.Contains(err.Error(), "explicit Seal skip") {
		t.Fatalf("runtime error sealed without authorization: %v", err)
	}
	summary, err := Seal(root, pkg, runtime.RunID, "delivery", "delivery", []string{"quality"})
	if err != nil {
		t.Fatal(err)
	}
	if got := summary.SkipAuthorizations["quality"]; got.Origin != "SEAL" || got.Status != "RUNTIME_ERROR" {
		t.Fatalf("runtime skip not retained: %#v", got)
	}
	repaired := readyDelivery(t, root, pkg, "repair-runtime-seal", "custom", []string{"quality"})
	repaired = recordGate(t, root, pkg, repaired, "quality", "FAIL", []FindingInput{{Severity: "P1", Message: "blocker"}})
	repaired = advance(t, root, pkg, repaired, "runtime-repair")
	repaired, err = RecordGate(root, pkg, repaired.RunID, "quality", "RUNTIME_ERROR", "review unavailable", nil, repaired.RequirementRevision, repaired.CatalogRevision, repaired.CurrentSnapshot, repaired.CurrentSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.CompletedRepairCycles != 0 || repaired.PreRepairSnapshot == "" {
		t.Fatalf("runtime wave consumed or forgot its repair: %#v", repaired)
	}
	if _, err := Seal(root, pkg, repaired.RunID, repaired.CurrentSnapshot, repaired.CurrentSnapshot, []string{"quality"}); err != nil {
		t.Fatalf("repaired runtime result could not be explicitly skipped: %v", err)
	}
}

func TestBlockingSealSkipWaitsForSharedLimit(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "blocking-seal", "custom", []string{"quality"})
	state = recordGate(t, root, pkg, state, "quality", "FAIL", []FindingInput{{Severity: "P1", Message: "blocker"}})
	if _, err := Seal(root, pkg, state.RunID, "delivery", "delivery", []string{"quality"}); err == nil || !strings.Contains(err.Error(), "repair limit") {
		t.Fatalf("early blocker skip was accepted: %v", err)
	}
	for cycle := 1; cycle <= automaticRepairLimit; cycle++ {
		next := "seal-repair-" + string(rune('0'+cycle))
		state = advance(t, root, pkg, state, next)
		state = recordGate(t, root, pkg, state, "quality", "FAIL", []FindingInput{{Severity: "P1", Message: "blocker"}})
	}
	summary, err := Seal(root, pkg, state.RunID, state.CurrentSnapshot, state.CurrentSnapshot, []string{"quality"})
	if err != nil {
		t.Fatal(err)
	}
	if summary.CompletedRepairCycles != automaticRepairLimit || summary.SkipAuthorizations["quality"].Status != "FAIL" {
		t.Fatalf("exhausted blocker authorization missing: %#v", summary)
	}
}

func TestNoneRouteSealsAfterDevelopmentAndRetainsRouteSkips(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "none-route", "none", nil)
	summary, err := Seal(root, pkg, state.RunID, "delivery", "delivery", nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "SEALED" || len(summary.SelectedGates) != 0 || len(summary.SkipAuthorizations) != 3 {
		t.Fatalf("none route summary=%#v", summary)
	}
}

func TestCarryIncludesOnlyPreviouslyPassingSelectedGates(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "carry-scope", "custom", []string{"architecture", "quality"})
	state = recordGate(t, root, pkg, state, "architecture", "PASS", nil)
	state = recordGate(t, root, pkg, state, "quality", "FAIL", []FindingInput{{Severity: "P1", Message: "blocker"}})
	state = advance(t, root, pkg, state, "repair")
	if got := eligibleCarryGates(state); !reflect.DeepEqual(got, []string{"architecture"}) {
		t.Fatalf("Carry gates=%v", got)
	}
	prompt, err := PrepareAction(root, pkg, state.RunID, "carry", "repair")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "[Gate: architecture]") || strings.Contains(prompt, "[Gate: quality]") {
		t.Fatalf("Carry prompt has wrong scope: %s", prompt)
	}
}

func TestConcurrentSelectedGateRecordingPreservesResults(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "parallel", "custom", []string{"architecture", "quality"})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, gate := range []string{"architecture", "quality"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := RecordGate(root, pkg, state.RunID, id, "PASS", "", nil, state.RequirementRevision, state.CatalogRevision, "delivery", "delivery")
			errs <- err
		}(gate)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range []string{"architecture", "quality"} {
		if got.Gates[gate].Status != "PASS" {
			t.Fatalf("lost %s result: %#v", gate, got.Gates)
		}
	}
}

func TestStaleSourceBindingsDoNotMutateState(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "stale", "custom", []string{"quality"})
	before, err := os.ReadFile(RunStatePath(root, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordGate(root, pkg, state.RunID, "quality", "PASS", "", nil, "old-revision", state.CatalogRevision, "delivery", "delivery"); err == nil {
		t.Fatal("stale requirement result accepted")
	}
	if _, err := RecordGate(root, pkg, state.RunID, "quality", "PASS", "", nil, state.RequirementRevision, state.CatalogRevision, "old-snapshot", "delivery"); err == nil {
		t.Fatal("stale snapshot result accepted")
	}
	after, err := os.ReadFile(RunStatePath(root, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("rejected stale result changed state")
	}
}

func TestResumeRecoversInterruptedStaleLock(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "stale-lock", "none", nil)
	lock := RunStatePath(root, state.RunID) + ".lock"
	writeTestFile(t, lock, "")
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Resume(root, pkg, state.RunID); err != nil {
		t.Fatalf("stale interrupted lock stranded resume: %v", err)
	}
}

func TestAbortRetainsSummaryAndRejectsLateWrites(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "abort", "none", nil)
	summary, err := Abort(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "ABORTED" {
		t.Fatalf("summary=%#v", summary)
	}
	if _, err := os.Stat(RunDir(root, state.RunID)); !os.IsNotExist(err) {
		t.Fatalf("temporary run remained: %v", err)
	}
	if _, err := AddRouteGates(root, pkg, state.RunID, []string{"quality"}); err == nil {
		t.Fatal("late write after abort was accepted")
	}
}

func workflowFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "requirements.md"), "requirement\n")
	return root, promptPackage(t, map[string]string{"quality": "quality checks", "architecture": "architecture checks"})
}

func beginRoute(t *testing.T, root, pkg, id, mode string, selected []string) RunState {
	t.Helper()
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: id, Flow: "formal", RequirementSource: "requirements.md", VCS: "git", BaseSnapshot: "base"})
	if err != nil {
		t.Fatal(err)
	}
	state = recordClarificationAndConfirm(t, root, pkg, state)
	state, err = SetRoute(root, pkg, state.RunID, mode, selected)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func recordClarificationAndConfirm(t *testing.T, root, pkg string, state RunState) RunState {
	t.Helper()
	var err error
	state, err = RecordAction(root, pkg, state.RunID, "requirements-clarification", "PASS", "", nil, state.RequirementRevision, state.CatalogRevision)
	if err != nil {
		t.Fatal(err)
	}
	state, err = UpdateRequirement(root, pkg, state.RunID, "", true, "")
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func readyDelivery(t *testing.T, root, pkg, id, mode string, selected []string) RunState {
	t.Helper()
	state := beginRoute(t, root, pkg, id, mode, selected)
	state = recordReadiness(t, root, pkg, state)
	if isSelected(state, "qa") {
		var err error
		state, err = RecordQADesign(root, pkg, state.RunID, []QACaseInput{{Description: "behavior", Procedure: "exercise", Oracle: "observed"}}, "", state.RequirementRevision, state.CatalogRevision)
		if err != nil {
			t.Fatal(err)
		}
	}
	return advance(t, root, pkg, state, "delivery")
}

func recordReadiness(t *testing.T, root, pkg string, state RunState) RunState {
	t.Helper()
	state, err := RecordAction(root, pkg, state.RunID, "start-readiness", "PASS", "", nil, state.RequirementRevision, state.CatalogRevision)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func advance(t *testing.T, root, pkg string, state RunState, snapshot string) RunState {
	t.Helper()
	state, err := AdvanceSnapshot(root, pkg, state.RunID, snapshot, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func recordQA(t *testing.T, root, pkg string, state RunState, outcome string) RunState {
	t.Helper()
	state, err := RecordQAExecution(root, pkg, state.RunID, []QAResultInput{{CaseID: "CASE-001", Outcome: outcome, Procedure: "exercised", Observation: "observed", OracleResult: "compared"}}, "", state.RequirementRevision, state.CatalogRevision, state.CurrentSnapshot, state.CurrentSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func recordGate(t *testing.T, root, pkg string, state RunState, gate, status string, findings []FindingInput) RunState {
	t.Helper()
	state, err := RecordGate(root, pkg, state.RunID, gate, status, "", findings, state.RequirementRevision, state.CatalogRevision, state.CurrentSnapshot, state.CurrentSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func recordAllPassing(t *testing.T, root, pkg string, state RunState) RunState {
	t.Helper()
	if isSelected(state, "qa") {
		state = recordQA(t, root, pkg, state, "PASS")
	}
	for _, gate := range state.SelectedGates {
		if gate != "qa" {
			state = recordGate(t, root, pkg, state, gate, "PASS", nil)
		}
	}
	return state
}
