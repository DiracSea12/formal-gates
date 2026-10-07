package validate

import (
	"formal-gates/internal/prompts"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/lifecycle"
)

func installLegacyBatchPlan(t *testing.T, root string, state RunState) RunState {
	t.Helper()
	state.DevelopmentBatchPlan = &DevelopmentBatchPlan{
		Digest:     "legacy-plan-digest",
		UserReason: "previously confirmed serial development plan",
		Batches: []DevelopmentBatchRecord{
			{ID: "A1", Task: "first task", DoD: "first checks pass", Status: "PENDING"},
			{ID: "A2", Task: "second task", DoD: "all checks pass", Status: "PENDING"},
		},
	}
	if err := SaveRunState(root, state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestLegacySerialPlanContinuesWithoutIntermediateQA(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "legacy-batches"), "custom", []string{prompts.BlackboxQAID, "quality"})
	installLegacyBatchPlan(t, root, state)
	stubLifecycle(t, lifecycle.Verification{Outcome: lifecycle.Verified}, "", "")

	firstPrompt, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(firstPrompt, "batch 1/2 (A1)") || !strings.Contains(firstPrompt, "first task") {
		t.Fatalf("first prompt omitted its compatibility batch: %s", firstPrompt)
	}
	state, _ = LoadRunState(root, state.RunID)
	firstDispatch := openDispatchID(state, "action", "development-worker")
	if state.Dispatches[firstDispatch].BatchID != "A1" {
		t.Fatalf("first dispatch batch = %+v", state.Dispatches[firstDispatch])
	}
	if _, err := ClaimDispatch(root, pkg, state.RunID, firstDispatch, "legacy-a1"); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "a1.txt"), "a1\n")
	commitAll(t, root, "legacy A1")
	firstSnapshot := gitHead(t, root)
	if _, err := AdvanceSnapshot(root, pkg, state.RunID, firstDispatch, false, ""); err == nil || !strings.Contains(err.Error(), "is not final") {
		t.Fatalf("ordinary snapshot accepted an intermediate batch: %v", err)
	}
	state, err = AdvanceDevelopmentBatch(root, pkg, state.RunID, firstDispatch, "A1", "targeted checks passed", "A1 decisions recorded")
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentSnapshot != firstSnapshot || state.Actions["development-worker"].Status != developmentPending || state.Dispatches[firstDispatch].Status != "COMPLETED" {
		t.Fatalf("intermediate completion = %+v", state)
	}
	if state.SnapshotOverride != nil || state.CompletedReviewWaves != 0 || state.PreRepairSnapshot != "" {
		t.Fatalf("intermediate batch opened QA/review: %+v", state)
	}
	if state.ActiveReviewWaveID != "" || len(state.ReviewWaves) != 0 {
		t.Fatalf("intermediate batch opened a convergence review wave: %+v", state.ReviewWaves)
	}
	if state.DevelopmentBatchPlan.Current != 0 || state.DevelopmentBatchPlan.Batches[0].Status != "" {
		t.Fatalf("legacy progress copy survived migration: %+v", state.DevelopmentBatchPlan)
	}

	secondPrompt, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(secondPrompt, "batch 2/2 (A2)") || !strings.Contains(secondPrompt, "Previous batch handoff:\nA1 decisions recorded") {
		t.Fatalf("second prompt omitted task or handoff: %s", secondPrompt)
	}
	state, _ = LoadRunState(root, state.RunID)
	secondDispatch := openDispatchID(state, "action", "development-worker")
	if state.Dispatches[secondDispatch].BatchID != "A2" {
		t.Fatalf("second dispatch batch = %+v", state.Dispatches[secondDispatch])
	}
	catalog, _ := prompts.LoadPromptCatalog(pkg)
	route := routeForState(root, state)
	route.DispatchID, route.DispatchAttempt, route.ReviewWave = secondDispatch, state.Dispatches[secondDispatch].Attempt, state.Dispatches[secondDispatch].ReviewWave
	recomposed, err := composeDispatchPrompt(root, state, catalog, route, state.Dispatches[secondDispatch])
	if err != nil || recomposed != secondPrompt {
		t.Fatalf("batch prompt did not recompose exactly: %v", err)
	}
	if _, err := ClaimDispatch(root, pkg, state.RunID, secondDispatch, "legacy-a2"); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "a2.txt"), "a2\n")
	commitAll(t, root, "legacy A2")
	if _, err := AdvanceDevelopmentBatch(root, pkg, state.RunID, secondDispatch, "A2", "checks passed", "done"); err == nil || !strings.Contains(err.Error(), "is final") {
		t.Fatalf("intermediate snapshot accepted the final batch: %v", err)
	}
	if _, err := AdvanceSnapshot(root, pkg, state.RunID, secondDispatch, false, ""); err == nil || !strings.Contains(err.Error(), "blackbox QA Review must pass") {
		t.Fatalf("final batch bypassed the ordinary QA gate: %v", err)
	}
	state, err = AdvanceSnapshot(root, pkg, state.RunID, secondDispatch, true, "test-authorized blackbox release")
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["development-worker"].Status != developmentComplete || state.Dispatches[secondDispatch].Status != "COMPLETED" {
		t.Fatalf("final batch did not enter ordinary snapshot flow: %+v", state)
	}
	if summary := developmentBatchSummary(state); len(summary) != 2 || summary[1].Status != "COMPLETE" || summary[1].Snapshot != state.CurrentSnapshot {
		t.Fatalf("final batch evidence missing: %+v", summary)
	}
}

func TestLegacyPlanMigrationRestoresCompletedEvidenceAndSealProjection(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "legacy-migration"), "custom", []string{"quality"})
	state.DevelopmentBatchPlan = &DevelopmentBatchPlan{
		Digest:     "old-digest",
		UserReason: "old confirmed plan",
		Current:    1,
		Batches: []DevelopmentBatchRecord{
			{ID: "A1", Task: "first", DoD: "first done", Status: "COMPLETE", DispatchID: "missing-old-dispatch", Snapshot: "snapshot-a1", Verification: "checks passed", Handoff: "next facts"},
			{ID: "A2", Task: "second", DoD: "second done", Status: "PENDING"},
		},
	}
	state.Actions["development-worker"] = ActionResult{Status: "BATCH_PENDING"}
	if err := SaveRunState(root, state); err != nil {
		t.Fatal(err)
	}
	stubLifecycle(t, lifecycle.Verification{Outcome: lifecycle.Verified}, "", "")
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatal(err)
	}
	state, _ = LoadRunState(root, state.RunID)
	if _, batch, ok := activeDevelopmentBatch(state); !ok || batch.ID != "A2" {
		t.Fatalf("migration did not preserve completed A1: %+v", developmentBatchSummary(state))
	}
	summary := runSummary(state)
	if len(summary.DevelopmentBatches) != 2 || summary.DevelopmentBatches[0].Status != "COMPLETE" || summary.DevelopmentBatches[0].Snapshot != "snapshot-a1" || summary.DevelopmentBatches[0].Verification != "checks passed" || summary.DevelopmentBatches[0].Handoff != "next facts" {
		t.Fatalf("summary lost completed batch evidence: %+v", summary.DevelopmentBatches)
	}
}

func TestLegacyBatchResetPreservesProgressAndRequirementChangeInvalidatesPlan(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "legacy-reset"), "custom", []string{"quality"})
	installLegacyBatchPlan(t, root, state)
	stubLifecycle(t, lifecycle.Verification{Outcome: lifecycle.Verified}, "", "")
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "a1.txt"), "a1\n")
	commitAll(t, root, "legacy A1 before reset")
	state, err := AdvanceDevelopmentBatch(root, pkg, state.RunID, dispatch, "A1", "checks passed", "handoff")
	if err != nil {
		t.Fatal(err)
	}
	result, err := ResetRun(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State.Dispatches[dispatch].Status != "COMPLETED" || result.State.Actions["development-worker"].Status != developmentPending {
		t.Fatalf("same-revision reset lost resumable progress: %+v", result.State)
	}
	if _, next, ok := activeDevelopmentBatch(result.State); !ok || next.ID != "A2" {
		t.Fatalf("same-revision reset selected wrong batch: %+v", developmentBatchSummary(result.State))
	}

	writeTestFile(t, filepath.Join(root, "requirements.md"), testRequirementDocument("Changed requirement."))
	commitAll(t, root, "change requirement")
	result, err = ResetRun(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State.DevelopmentBatchPlan != nil || result.State.Actions["development-worker"].Status != developmentPending {
		t.Fatalf("changed requirement retained or wedged legacy plan: %+v", result.State)
	}
}

func TestPendingBetweenLegacyBatchesStillCountsAsDevelopmentStarted(t *testing.T) {
	state := RunState{
		Actions: map[string]ActionResult{"development-worker": {Status: developmentPending}},
		DevelopmentBatchPlan: &DevelopmentBatchPlan{Batches: []DevelopmentBatchRecord{
			{ID: "A1", Task: "first", DoD: "done"},
			{ID: "A2", Task: "second", DoD: "done"},
		}},
		Dispatches: map[string]PreparedDispatch{"d1": {ID: "d1", Target: "development-worker", TargetKind: "action", BatchID: "A1", Status: "COMPLETED"}},
	}
	if !developmentStarted(state) {
		t.Fatal("completed batch was treated as pre-development")
	}
}

func TestLegacyBatchRequirementRebindingPreservesOrInvalidatesProgress(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "legacy-rebind"), "custom", []string{"quality"})
	installLegacyBatchPlan(t, root, state)
	stubLifecycle(t, lifecycle.Verification{Outcome: lifecycle.Verified}, "", "")
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "a1.txt"), "a1\n")
	commitAll(t, root, "A1 before requirement rebind")
	state, err := AdvanceDevelopmentBatch(root, pkg, state.RunID, dispatch, "A1", "checks passed", "A1 interface ready")
	if err != nil {
		t.Fatal(err)
	}
	completed := state.Dispatches[dispatch]
	writeTestFile(t, filepath.Join(root, "requirements.md"), testRequirementDocument("Requirement.")+"\n")
	commitAll(t, root, "meaning-preserved requirement format")
	state, err = UpdateRequirement(root, pkg, state.RunID, "", true, "preserved", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.DevelopmentBatchPlan == nil || state.DevelopmentBatchPlan.RequirementRevision != state.RequirementRevision || state.Dispatches[dispatch] != completed {
		t.Fatal("meaning-preserved revision lost batch binding or completed evidence")
	}
	if _, next, ok := activeDevelopmentBatch(state); !ok || next.ID != "A2" {
		t.Fatal("meaning-preserved revision did not retain the next batch")
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "requirements.md"), testRequirementDocument("Changed requirement."))
	commitAll(t, root, "meaning-changed requirement")
	state, err = UpdateRequirement(root, pkg, state.RunID, "", false, "changed", nil, repairImpactOptions(t, root, pkg, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if state.DevelopmentBatchPlan != nil || state.Actions["development-worker"].Status != developmentPending {
		t.Fatal("authorized meaning-changing revision retained the old batch plan")
	}
	for _, dispatch := range state.Dispatches {
		if dispatch.BatchID != "" || dispatch.CompletionSnapshot != "" || dispatch.Verification != "" || dispatch.Handoff != "" {
			t.Fatalf("invalidated batch evidence remained bound: %+v", dispatch)
		}
	}
}

func TestLegacyIntermediateBatchRejectsNonDescendantSnapshot(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "legacy-ancestry"), "custom", []string{"quality"})
	state = installLegacyBatchPlan(t, root, state)
	state.DevelopmentBatchPlan.Batches = append(state.DevelopmentBatchPlan.Batches, DevelopmentBatchRecord{ID: "A3", Task: "third task", DoD: "third checks pass"})
	if err := SaveRunState(root, state); err != nil {
		t.Fatal(err)
	}
	stubLifecycle(t, lifecycle.Verification{Outcome: lifecycle.Verified}, "", "")
	first := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "a1.txt"), "a1\n")
	commitAll(t, root, "A1 ancestry basis")
	state, err := AdvanceDevelopmentBatch(root, pkg, state.RunID, first, "A1", "checks passed", "A1 interface ready")
	if err != nil {
		t.Fatal(err)
	}
	second := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	runGit(t, root, "checkout", "-b", "alternate-development", state.BaseSnapshot)
	writeTestFile(t, filepath.Join(root, "alternate.txt"), "alternate\n")
	commitAll(t, root, "unrelated to A1")
	before := stateBytes(t, root, state.RunID)
	if _, err := AdvanceDevelopmentBatch(root, pkg, state.RunID, second, "A2", "checks passed", "next facts"); err == nil || !strings.Contains(err.Error(), "native VCS identity does not match") {
		t.Fatalf("intermediate batch accepted a non-descendant snapshot: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected non-descendant snapshot changed batch state or evidence")
	}
}

func TestLegacyActiveSecondBatchMigratesWithoutLosingBindings(t *testing.T) {
	state := RunState{
		RequirementRevision: "req-1",
		CatalogRevision:     "catalog-1",
		BaseSnapshot:        "base",
		CurrentSnapshot:     "snapshot-a1",
		Actions:             map[string]ActionResult{"development-worker": {Status: developmentPrepared}},
		Dispatches: map[string]PreparedDispatch{
			"d1": {ID: "d1", Target: "development-worker", TargetKind: "action", Status: "COMPLETED", BatchID: "A1"},
			"d2": {ID: "d2", Target: "development-worker", TargetKind: "action", Status: "CLAIMED", BatchID: "A2"},
		},
		DevelopmentBatchPlan: &DevelopmentBatchPlan{
			Digest: "digest", UserReason: "confirmed", Current: 1,
			Batches: []DevelopmentBatchRecord{
				{ID: "A1", Task: "first", DoD: "done", Status: "COMPLETE", DispatchID: "d1", Snapshot: "snapshot-a1", Verification: "passed", Handoff: "facts"},
				{ID: "A2", Task: "second", DoD: "done", Status: "PREPARED", DispatchID: "d2"},
			},
		},
	}
	if err := normalizeLegacyDevelopmentPlan(&state); err != nil {
		t.Fatal(err)
	}
	if _, batch, ok := activeDevelopmentBatch(state); !ok || batch.ID != "A2" || state.Dispatches["d2"].BatchID != "A2" {
		t.Fatalf("active A2 binding was lost: %+v", state)
	}
	if state.Dispatches["d1"].CompletionSnapshot != "snapshot-a1" || state.Dispatches["d1"].Verification != "passed" || state.Dispatches["d1"].Handoff != "facts" {
		t.Fatalf("A1 evidence was not migrated: %+v", state.Dispatches["d1"])
	}
}

func TestLegacyBatchReplacementUsesExistingLifecycleRules(t *testing.T) {
	for _, claimed := range []bool{false, true} {
		name := "unclaimed"
		if claimed {
			name = "stopped-worker"
		}
		t.Run(name, func(t *testing.T) {
			root, pkg := workflowFixture(t)
			state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "replace-batch"), "custom", []string{"quality"})
			installLegacyBatchPlan(t, root, state)
			stubLifecycle(t, lifecycle.Verification{Outcome: lifecycle.Verified}, "", "user abort")
			if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
				t.Fatal(err)
			}
			state, _ = LoadRunState(root, state.RunID)
			first := openDispatchID(state, "action", "development-worker")
			if claimed {
				if _, err := ClaimDispatch(root, pkg, state.RunID, first, "first-worker"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", true, "replace stopped worker"); err != nil {
				t.Fatal(err)
			}
			state, _ = LoadRunState(root, state.RunID)
			// Attempts are now scoped to TaskKey, so both tickets may be attempt 1.
			// Select the explicitly requested replacement, not an arbitrary OPEN tie.
			fresh := ""
			for id, dispatch := range state.Dispatches {
				if id != first && dispatch.Target == "development-worker" && dispatch.Status == "OPEN" && dispatch.UserRequested {
					fresh = id
				}
			}
			if fresh == "" {
				t.Fatal("explicit replacement did not prepare a new batch dispatch")
			}
			state, err := ClaimDispatch(root, pkg, state.RunID, fresh, "replacement-worker")
			if err != nil {
				t.Fatal(err)
			}
			if state.Dispatches[first].Status != "STALE" || state.Dispatches[fresh].Status != "CLAIMED" || state.Dispatches[fresh].BatchID != "A1" {
				t.Fatalf("replacement lost lifecycle or batch binding: %+v", state.Dispatches)
			}
		})
	}
}

func TestLegacyBatchProgressPrefersLiveReplacementAcrossTaskKeys(t *testing.T) {
	state := RunState{
		DevelopmentBatchPlan: &DevelopmentBatchPlan{Batches: []DevelopmentBatchRecord{
			{ID: "A1", Task: "first", DoD: "done"},
			{ID: "A2", Task: "second", DoD: "done"},
		}},
		Dispatches: map[string]PreparedDispatch{
			"old":         {ID: "old", Target: "development-worker", TargetKind: "action", BatchID: "A1", TaskKey: "old-task", Attempt: 2, Status: "STALE"},
			"replacement": {ID: "replacement", Target: "development-worker", TargetKind: "action", BatchID: "A1", TaskKey: "replacement-task", Attempt: 1, Status: "CLAIMED"},
		},
	}
	summary := developmentBatchSummary(state)
	if summary[0].DispatchID != "replacement" || summary[0].Status != "PREPARED" {
		t.Fatalf("batch progress selected a stale attempt from a different TaskKey: %+v", summary[0])
	}
	completed := state.Dispatches["old"]
	completed.Status, completed.CompletionSnapshot = "COMPLETED", "completed-A1"
	state.Dispatches["old"] = completed
	summary = developmentBatchSummary(state)
	if summary[0].Status != "COMPLETE" || summary[0].Snapshot != "completed-A1" {
		t.Fatalf("live replacement shadowed completed batch evidence: %+v", summary[0])
	}
}

func TestCompletedLegacyPlanAllowsRouteAddition(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "completed-route"), "custom", []string{"quality"})
	installLegacyBatchPlan(t, root, state)
	stubLifecycle(t, lifecycle.Verification{Outcome: lifecycle.Verified}, "", "")
	for index, batch := range []string{"A1", "A2"} {
		dispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
		writeTestFile(t, filepath.Join(root, batch+".txt"), batch+"\n")
		commitAll(t, root, batch)
		var err error
		if index == 0 {
			state, err = AdvanceDevelopmentBatch(root, pkg, state.RunID, dispatch, batch, "checks passed", "next facts")
		} else {
			state, err = AdvanceSnapshot(root, pkg, state.RunID, dispatch, false, "")
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	state = recordGateResult(t, root, pkg, state, "quality", "quality-reviewer", "PASS", "", nil)
	if _, err := AddRouteGates(root, pkg, state.RunID, []string{"architecture"}); err != nil {
		t.Fatal(err)
	}
}

func TestOrdinaryResetAfterRequirementEditPreservesDevelopment(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "ordinary-reset", "custom", []string{"quality"})
	writeTestFile(t, filepath.Join(root, "requirements.md"), testRequirementDocument("Requirement.")+"\n")
	result, err := ResetRun(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State.CurrentSnapshot != state.CurrentSnapshot || result.State.Actions["development-worker"].Status != state.Actions["development-worker"].Status {
		t.Fatal("ordinary reset lost its recorded development snapshot or status")
	}
	state = confirmRequirement(t, root, pkg, result.State)
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", false, ""); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyFirstBatchRetainsIntegrityEncoding(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := installLegacyBatchPlan(t, root, mustStart(t, root, pkg, "legacy-first"))
	data, err := os.ReadFile(RunStatePath(root, state.RunID))
	if err != nil {
		t.Fatal(err)
	}
	// The old writer always emits current, including before the first completion.
	// Dropping that zero changes the bytes used by its existing integrity digest.
	if !strings.Contains(string(data), `"current": 0`) {
		t.Fatal("legacy first-batch wire format omitted current:0")
	}
	loaded, err := LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if err := normalizeLegacyDevelopmentPlan(&loaded); err != nil {
		t.Fatal(err)
	}
	if _, batch, active := activeDevelopmentBatch(loaded); !active || batch.ID != "A1" {
		t.Fatal("legacy first batch could not continue after integrity validation")
	}
}

func TestAbortLegacyPlanPreservesUnmigratedEvidence(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := installLegacyBatchPlan(t, root, mustStart(t, root, pkg, "legacy-abort"))
	state.DevelopmentBatchPlan.Current = 1
	first := &state.DevelopmentBatchPlan.Batches[0]
	first.Status, first.DispatchID = "COMPLETE", "old-a1"
	first.Snapshot, first.Verification, first.Handoff = "snapshot-a1", "checks passed", "A1 interface ready"
	state.Dispatches["old-a1"] = PreparedDispatch{ID: "old-a1", Target: "development-worker", TargetKind: "action", BatchID: "A1", Status: "COMPLETED"}
	if err := SaveRunState(root, state); err != nil {
		t.Fatal(err)
	}
	if _, err := Abort(root, state.RunID); err != nil {
		t.Fatal(err)
	}
	summary, err := LoadRunSummary(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.DevelopmentBatches) != 2 {
		t.Fatalf("batch history missing: %+v", summary.DevelopmentBatches)
	}
	got := summary.DevelopmentBatches[0]
	if got.Status != "COMPLETE" || got.Snapshot != first.Snapshot || got.Verification != first.Verification || got.Handoff != first.Handoff {
		t.Fatalf("abort lost old evidence: %+v", got)
	}
	if _, err := os.Stat(RunStatePath(root, state.RunID)); !os.IsNotExist(err) {
		t.Fatalf("abort did not finish cleanup: %v", err)
	}
}
