package validate

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWorkflowUsesOneTemporaryStateFileAndResumesPending(t *testing.T) {
	root, pkg := workflowFixture(t)
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "run-one", Flow: "formal", RequirementSource: "requirements.md", RequirementConfirmed: true, VCS: "git", BaseSnapshot: "base", CurrentSnapshot: "current"})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(RunDir(root, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("temporary run contents=%v", entries)
	}
	data, err := os.ReadFile(RunStatePath(root, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"schemaVersion"`) {
		t.Fatalf("temporary state retains an unrequired schema version: %s", data)
	}
	if state.Gates["architecture"].Status != "PENDING" || state.Gates["quality"].Status != "PENDING" {
		t.Fatalf("new gates are not pending: %#v", state.Gates)
	}
	resumed, invalidated, err := Resume(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if invalidated || resumed.Gates["quality"].Status != "PENDING" {
		t.Fatalf("resume changed pending work: %#v", resumed)
	}
}

func TestWorkflowRejectsUnknownFlowAndUnreadyDevelopment(t *testing.T) {
	root, pkg := workflowFixture(t)
	if _, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "bad-flow", Flow: "release", RequirementSource: "requirements.md", RequirementConfirmed: true, VCS: "git", BaseSnapshot: "base", CurrentSnapshot: "current"}); err == nil {
		t.Fatal("unknown flow was accepted")
	}
	state := startWorkflow(t, root, pkg, "development-admission")
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "current"); err == nil || !strings.Contains(err.Error(), "QA cases") {
		t.Fatalf("development without QA cases was admitted: %v", err)
	}
	if _, err := RecordQADesign(root, pkg, state.RunID, []QACaseInput{{Description: "behavior", Procedure: "exercise", Oracle: "observed"}}, "", state.RequirementRevision, state.CatalogRevision); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "current"); err != nil {
		t.Fatalf("ready development was blocked: %v", err)
	}
}

func TestUnconfirmedRequirementCannotProduceReviewResults(t *testing.T) {
	root, pkg := workflowFixture(t)
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "unconfirmed", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", BaseSnapshot: "base", CurrentSnapshot: "current"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "start-readiness", ""); err == nil {
		t.Fatal("start-readiness was prepared against an unconfirmed requirement")
	}
	if _, err := RecordAction(root, pkg, state.RunID, "start-readiness", "PASS", "", nil, state.RequirementRevision, state.CatalogRevision); err == nil {
		t.Fatal("start-readiness PASS was recorded against an unconfirmed requirement")
	}
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", "current"); err == nil {
		t.Fatal("gate was prepared against an unconfirmed requirement")
	}
	if _, err := RecordGate(root, pkg, state.RunID, "quality", "PASS", "", nil, state.RequirementRevision, state.CatalogRevision, "current", "current"); err == nil {
		t.Fatal("gate PASS was recorded against an unconfirmed requirement")
	}
}

func TestConcurrentGateRecordingPreservesBothResults(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := startWorkflow(t, root, pkg, "parallel")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, gate := range []string{"architecture", "quality"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := RecordGate(root, pkg, state.RunID, id, "PASS", "", nil, state.RequirementRevision, state.CatalogRevision, "current", "current")
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
	if _, err := os.Stat(RunStatePath(root, state.RunID) + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("lock file remained: %v", err)
	}
}

func TestRequirementChangeInvalidatesBoundResults(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := startWorkflow(t, root, pkg, "requirement-change")
	state = recordAllPassing(t, root, pkg, state)
	writeTestFile(t, filepath.Join(root, "requirements.md"), "changed requirement\n")
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", "current"); err == nil || !strings.Contains(err.Error(), "resume") {
		t.Fatalf("changed requirement was not detected: %v", err)
	}
	resumed, invalidated, err := Resume(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !invalidated || resumed.RequirementConfirmed || len(resumed.QACases) != 0 || resumed.QAExecution.Status != "PENDING" {
		t.Fatalf("requirement results were not invalidated: %#v", resumed)
	}
	for id, result := range resumed.Gates {
		if result.Status != "PENDING" {
			t.Fatalf("gate %s was not reset: %#v", id, result)
		}
	}
}

func TestStaleRequirementResultsAreRejectedWithoutChangingState(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := startWorkflow(t, root, pkg, "stale-requirement")
	oldRevision := state.RequirementRevision
	writeTestFile(t, filepath.Join(root, "requirements.md"), "changed requirement\n")
	if _, _, err := Resume(root, pkg, state.RunID); err != nil {
		t.Fatal(err)
	}
	state, err := UpdateRequirement(root, pkg, state.RunID, "requirements.md", true)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(RunStatePath(root, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordAction(root, pkg, state.RunID, "start-readiness", "PASS", "", nil, oldRevision, state.CatalogRevision); err == nil || !strings.Contains(err.Error(), "source requirement revision") {
		t.Fatalf("stale action result was accepted: %v", err)
	}
	if _, err := RecordQADesign(root, pkg, state.RunID, []QACaseInput{{Description: "behavior", Procedure: "exercise", Oracle: "observed"}}, "", oldRevision, state.CatalogRevision); err == nil || !strings.Contains(err.Error(), "source requirement revision") {
		t.Fatalf("stale QA Design result was accepted: %v", err)
	}
	after, err := os.ReadFile(RunStatePath(root, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("rejected stale requirement results changed state\nbefore: %s\nafter: %s", before, after)
	}
	if state.RequirementRevision == oldRevision {
		t.Fatal("requirement revision did not change")
	}
}

func TestStaleSnapshotGateResultIsRejectedWithoutChangingState(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := startWorkflow(t, root, pkg, "stale-snapshot")
	state, err := AdvanceSnapshot(root, pkg, state.RunID, "new-snapshot", "new-snapshot")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(RunStatePath(root, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordGate(root, pkg, state.RunID, "quality", "PASS", "", nil, state.RequirementRevision, state.CatalogRevision, "current", "new-snapshot"); err == nil || !strings.Contains(err.Error(), "source snapshot") {
		t.Fatalf("stale gate result was accepted: %v", err)
	}
	after, err := os.ReadFile(RunStatePath(root, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("rejected stale gate result changed state\nbefore: %s\nafter: %s", before, after)
	}
}

func TestCatalogChangeRequiresNewRun(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := startWorkflow(t, root, pkg, "catalog-change")
	writeTestFile(t, filepath.Join(pkg, "gates", "new-gate.md"), "new checks\n")
	if _, _, err := Resume(root, pkg, state.RunID); err == nil || !strings.Contains(err.Error(), "start a new run") {
		t.Fatalf("catalog change was accepted: %v", err)
	}
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", "current"); err == nil {
		t.Fatal("prompt used changed catalog")
	}
}

func TestStaleCatalogResultIsRejectedWithoutChangingState(t *testing.T) {
	root, pkg := workflowFixture(t)
	oldState := startWorkflow(t, root, pkg, "old-catalog")
	writeTestFile(t, filepath.Join(pkg, "gates", "quality.md"), "updated quality checks\n")
	state := startWorkflow(t, root, pkg, "new-catalog")
	if state.CatalogRevision == oldState.CatalogRevision {
		t.Fatal("catalog revision did not change")
	}
	before, err := os.ReadFile(RunStatePath(root, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordAction(root, pkg, state.RunID, "start-readiness", "PASS", "", nil, state.RequirementRevision, oldState.CatalogRevision); err == nil || !strings.Contains(err.Error(), "source catalog revision") {
		t.Fatalf("stale catalog result was accepted: %v", err)
	}
	after, err := os.ReadFile(RunStatePath(root, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("rejected stale catalog result changed state\nbefore: %s\nafter: %s", before, after)
	}
	state, err = RecordAction(root, pkg, state.RunID, "start-readiness", "PASS", "", nil, state.RequirementRevision, state.CatalogRevision)
	if err != nil {
		t.Fatalf("current catalog result was rejected: %v", err)
	}
	if state.Actions["start-readiness"].Status != "PASS" {
		t.Fatalf("current catalog result was not recorded: %#v", state.Actions["start-readiness"])
	}
}

func TestRepairFlattensCarryAndClearsQA(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := startWorkflow(t, root, pkg, "repair")
	state = recordAllPassing(t, root, pkg, state)
	state, err := AdvanceSnapshot(root, pkg, state.RunID, "repaired", "repaired")
	if err != nil {
		t.Fatal(err)
	}
	if state.PreRepairSnapshot != "current" || state.QAExecution.Status != "PENDING" {
		t.Fatalf("repair state=%#v", state)
	}
	carryPrompt, err := PrepareAction(root, pkg, state.RunID, "carry", "repaired")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(carryPrompt, "pre-repair snapshot: current") || !strings.Contains(carryPrompt, "[Gate: quality]") {
		t.Fatalf("carry prompt missing immediate comparison/catalog: %s", carryPrompt)
	}
	state, err = RecordCarry(root, pkg, state.RunID, []CarryInput{{GateID: "architecture", Decision: "INHERIT", Message: "unaffected"}, {GateID: "quality", Decision: "RERUN", Message: "affected"}}, "", state.RequirementRevision, state.CatalogRevision, "repaired", "repaired")
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Gates["architecture"]; got.Status != "PASS" || got.Snapshot != "repaired" || got.SourceSnapshot != "current" {
		t.Fatalf("inherit not flattened: %#v", got)
	}
	if state.Gates["quality"].Status != "PENDING" {
		t.Fatalf("rerun gate not pending: %#v", state.Gates["quality"])
	}
	prompt, err := PrepareGate(root, pkg, state.RunID, "quality", "repaired")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "base snapshot: base") || !strings.Contains(prompt, "current snapshot: repaired") {
		t.Fatalf("rerun prompt is not cumulative: %s", prompt)
	}
}

func TestSnapshotRejectsAdvanceUntilCarryResolved(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := recordAllPassing(t, root, pkg, startWorkflow(t, root, pkg, "pending-carry"))
	state, err := AdvanceSnapshot(root, pkg, state.RunID, "repair-one", "repair-one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AdvanceSnapshot(root, pkg, state.RunID, "repair-two", "repair-two"); err == nil || !strings.Contains(err.Error(), "await a Carry decision") {
		t.Fatalf("second snapshot before Carry was accepted: %v", err)
	}
	unchanged, err := LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.CurrentSnapshot != "repair-one" || unchanged.PreRepairSnapshot != "current" || len(eligibleCarryGates(unchanged)) != 2 {
		t.Fatalf("rejected snapshot changed state: %#v", unchanged)
	}
	state, err = RecordCarry(root, pkg, state.RunID, []CarryInput{{GateID: "architecture", Decision: "INHERIT", Message: "unaffected"}, {GateID: "quality", Decision: "INHERIT", Message: "unaffected"}}, "", state.RequirementRevision, state.CatalogRevision, "repair-one", "repair-one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AdvanceSnapshot(root, pkg, state.RunID, "repair-two", "repair-two"); err != nil {
		t.Fatalf("snapshot after Carry was rejected: %v", err)
	}
}

func TestCarryRequiresTrimmedReasonWithoutChangingState(t *testing.T) {
	for _, missing := range []struct {
		gate     string
		decision string
	}{
		{gate: "architecture", decision: "INHERIT"},
		{gate: "quality", decision: "RERUN"},
	} {
		t.Run(missing.decision, func(t *testing.T) {
			root, pkg := workflowFixture(t)
			state := recordAllPassing(t, root, pkg, startWorkflow(t, root, pkg, "missing-"+strings.ToLower(missing.decision)))
			state, err := AdvanceSnapshot(root, pkg, state.RunID, "repaired", "repaired")
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(RunStatePath(root, state.RunID))
			if err != nil {
				t.Fatal(err)
			}
			decisions := []CarryInput{
				{GateID: "architecture", Decision: "INHERIT", Message: "unaffected"},
				{GateID: "quality", Decision: "RERUN", Message: "affected"},
			}
			for index := range decisions {
				if decisions[index].GateID == missing.gate {
					decisions[index].Decision = missing.decision
					decisions[index].Message = " \t\n "
				}
			}
			if _, err := RecordCarry(root, pkg, state.RunID, decisions, "", state.RequirementRevision, state.CatalogRevision, "repaired", "repaired"); err == nil || !strings.Contains(err.Error(), "requires a reason") {
				t.Fatalf("Carry accepted missing %s reason: %v", missing.decision, err)
			}
			after, err := os.ReadFile(RunStatePath(root, state.RunID))
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("rejected Carry changed state bytes\nbefore: %s\nafter: %s", before, after)
			}
		})
	}
}

func TestQADesignTrimsBeforeDuplicateCheckAndStorage(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := startWorkflow(t, root, pkg, "qa-normalization")
	if _, err := RecordQADesign(root, pkg, state.RunID, []QACaseInput{
		{Description: " behavior ", Procedure: " exercise ", Oracle: " observed "},
		{Description: "behavior", Procedure: "exercise", Oracle: "observed"},
	}, "", state.RequirementRevision, state.CatalogRevision); err == nil || !strings.Contains(err.Error(), "duplicate QA case 2") {
		t.Fatalf("semantically duplicate QA cases were accepted: %v", err)
	}

	state, err := RecordQADesign(root, pkg, state.RunID, []QACaseInput{{Description: " behavior ", Procedure: " exercise ", Oracle: " observed "}}, "", state.RequirementRevision, state.CatalogRevision)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.QACases) != 1 || state.QACases[0].Description != "behavior" || state.QACases[0].Procedure != "exercise" || state.QACases[0].Oracle != "observed" {
		t.Fatalf("QA case was not stored in normalized form: %#v", state.QACases)
	}
}

func TestFirstDeliverySnapshotDoesNotPretendToBeRepair(t *testing.T) {
	root, pkg := workflowFixture(t)
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "first-delivery", Flow: "formal", RequirementSource: "requirements.md", RequirementConfirmed: true, VCS: "git", BaseSnapshot: "base"})
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentSnapshot != "base" {
		t.Fatalf("start current snapshot=%s", state.CurrentSnapshot)
	}
	state, err = AdvanceSnapshot(root, pkg, state.RunID, "delivery", "delivery")
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentSnapshot != "delivery" || state.PreRepairSnapshot != "" || len(state.Carry) != 0 {
		t.Fatalf("first delivery was modeled as repair: %#v", state)
	}
	if _, ok := state.Actions["carry"]; ok {
		t.Fatalf("first delivery requested Carry: %#v", state.Actions)
	}
}

func TestFindingLocationsMustBeRepositoryRelative(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := startWorkflow(t, root, pkg, "finding-path")
	for _, location := range []string{"/tmp/file.go:1", "../outside.go:2", `C:/file.go:3`, `dir\file.go:4`} {
		if _, err := RecordGate(root, pkg, state.RunID, "quality", "FAIL", "", []FindingInput{{Message: "problem", Locations: []string{location}}}, state.RequirementRevision, state.CatalogRevision, "current", "current"); err == nil {
			t.Fatalf("non-relative finding location was accepted: %s", location)
		}
	}
	if _, err := RecordGate(root, pkg, state.RunID, "quality", "FAIL", "", []FindingInput{{Message: "problem", Locations: []string{"internal/file.go:10"}}}, state.RequirementRevision, state.CatalogRevision, "current", "current"); err != nil {
		t.Fatalf("repository-relative finding was rejected: %v", err)
	}
}

func TestQAExecutionRecordsProcedureObservationAndOracle(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := startWorkflow(t, root, pkg, "qa-record")
	state, err := RecordQADesign(root, pkg, state.RunID, []QACaseInput{{Description: "behavior", Procedure: "design procedure", Oracle: "design oracle"}}, "", state.RequirementRevision, state.CatalogRevision)
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordQAExecution(root, pkg, state.RunID, []QAResultInput{{CaseID: "CASE-001", Outcome: "PASS", Procedure: "actual procedure", Observation: "actual observation", OracleResult: "oracle matched"}}, "", state.RequirementRevision, state.CatalogRevision, "current", "current")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.QAExecution.Cases) != 1 || state.QAExecution.Cases[0].Procedure != "actual procedure" || state.QAExecution.Cases[0].Observation != "actual observation" || state.QAExecution.Cases[0].OracleResult != "oracle matched" {
		t.Fatalf("QA evidence was not retained: %#v", state.QAExecution)
	}
}

func TestQADesignRuntimeErrorIsDistinctFromReviewFailure(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := startWorkflow(t, root, pkg, "qa-design-runtime")
	state, err := RecordQADesign(root, pkg, state.RunID, nil, "model output was malformed", state.RequirementRevision, state.CatalogRevision)
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["qa-design"].Status != "RUNTIME_ERROR" || len(state.QACases) != 0 {
		t.Fatalf("QA Design runtime error was not recorded: %#v", state)
	}
}

func TestSealAllowsNoReviewResultsAndCleansTemporaryRun(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := startWorkflow(t, root, pkg, "seal")
	summary, err := Seal(root, pkg, state.RunID, "current", "current")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "SEALED" || len(summary.Gates) != 2 || summary.QA.Status != "PENDING" {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	for id, result := range summary.Gates {
		if result.Status != "PENDING" {
			t.Fatalf("unrun gate %s was not retained as PENDING: %#v", id, result)
		}
	}
	if _, err := os.Stat(RunDir(root, state.RunID)); !os.IsNotExist(err) {
		t.Fatalf("temporary run was not cleaned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gates", "results", state.RunID+".json")); err != nil {
		t.Fatalf("seal summary missing: %v", err)
	}
	if _, err := RecordGate(root, pkg, state.RunID, "quality", "PASS", "", nil, state.RequirementRevision, state.CatalogRevision, "current", "current"); err == nil {
		t.Fatal("late result after seal was accepted")
	}
	if _, err := os.Stat(RunDir(root, state.RunID)); !os.IsNotExist(err) {
		t.Fatalf("late result recreated temporary run: %v", err)
	}
}

func TestSealAllowsPartialAndNonPassingResults(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := startWorkflow(t, root, pkg, "seal-partial")
	state, err := RecordAction(root, pkg, state.RunID, "start-readiness", "RUNTIME_ERROR", "reviewer unavailable", nil, state.RequirementRevision, state.CatalogRevision)
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordQAExecution(root, pkg, state.RunID, nil, "test runner unavailable", state.RequirementRevision, state.CatalogRevision, state.CurrentSnapshot, state.CurrentSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordGate(root, pkg, state.RunID, "quality", "FAIL", "", []FindingInput{{Message: "review finding"}}, state.RequirementRevision, state.CatalogRevision, state.CurrentSnapshot, state.CurrentSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := Seal(root, pkg, state.RunID, "current", "current")
	if err != nil {
		t.Fatal(err)
	}
	if summary.QA.Status != "RUNTIME_ERROR" || summary.Gates["quality"].Status != "FAIL" || summary.Gates["architecture"].Status != "PENDING" {
		t.Fatalf("partial and non-passing results were not retained: %#v", summary)
	}
}

func TestResumeRecoversInterruptedStaleLock(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := startWorkflow(t, root, pkg, "stale-lock")
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

func TestAtomicStateReplacementCompletes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := writeAtomic(path, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "second\n" {
		t.Fatalf("atomic replacement=%q", data)
	}
}

func TestAbortRetainsOnlySummaryAndRejectsLateWrites(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := startWorkflow(t, root, pkg, "abort")
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
	if _, err := RecordGate(root, pkg, state.RunID, "quality", "PASS", "", nil, state.RequirementRevision, state.CatalogRevision, "current", "current"); err == nil {
		t.Fatal("late result after abort was accepted")
	}
	if _, err := os.Stat(RunDir(root, state.RunID)); !os.IsNotExist(err) {
		t.Fatalf("late result recreated temporary run: %v", err)
	}
}

func workflowFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "requirements.md"), "requirement\n")
	return root, promptPackage(t, map[string]string{"quality": "quality checks", "architecture": "architecture checks"})
}
func startWorkflow(t *testing.T, root, pkg, id string) RunState {
	t.Helper()
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: id, Flow: "formal", RequirementSource: "requirements.md", RequirementConfirmed: true, VCS: "git", BaseSnapshot: "base", CurrentSnapshot: "current"})
	if err != nil {
		t.Fatal(err)
	}
	return state
}
func recordAllPassing(t *testing.T, root, pkg string, state RunState) RunState {
	t.Helper()
	var err error
	state, err = RecordAction(root, pkg, state.RunID, "start-readiness", "PASS", "", nil, state.RequirementRevision, state.CatalogRevision)
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordQADesign(root, pkg, state.RunID, []QACaseInput{{Description: "behavior", Procedure: "exercise it", Oracle: "observed"}}, "", state.RequirementRevision, state.CatalogRevision)
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordQAExecution(root, pkg, state.RunID, []QAResultInput{{CaseID: "CASE-001", Outcome: "PASS", Procedure: "exercised", Observation: "observed", OracleResult: "matched"}}, "", state.RequirementRevision, state.CatalogRevision, state.CurrentSnapshot, state.CurrentSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range []string{"architecture", "quality"} {
		state, err = RecordGate(root, pkg, state.RunID, gate, "PASS", "", nil, state.RequirementRevision, state.CatalogRevision, state.CurrentSnapshot, state.CurrentSnapshot)
		if err != nil {
			t.Fatal(err)
		}
	}
	return state
}
