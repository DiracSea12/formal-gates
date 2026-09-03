package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetainedMasterWaiverDoesNotRequireSliceGuaranteeSidecars(t *testing.T) {
	root, pkg, master := readyGuaranteeRoute(t, "waived-master-no-sidecars", true)
	var err error
	master, err = RecordSlicing(root, pkg, master.RunID, "split", 2, []string{"waived-slice-a", "waived-slice-b"}, "parallel", "", "", SlicingAmendOptions{ACResponsibilities: []string{"AC-001=waived-slice-a", "AC-009=waived-slice-b"}})
	if err != nil {
		t.Fatal(err)
	}
	master = recordGuaranteeDesignAndReview(t, root, pkg, master, "", nil)
	writeTestFile(t, filepath.Join(root, "waived-master-delivery.txt"), "merged delivery\n")
	commitAll(t, root, "waived master delivery")
	master, err = AdvanceSnapshot(root, pkg, master.RunID, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	gate := prepareAndClaim(t, root, pkg, master.RunID, mergeGateID, "waived-master-merge-reviewer")
	master, err = RecordGate(root, pkg, master.RunID, mergeGateID, gate, "PASS", "", comparedRange(master), nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, sliceID := range master.Slicing.Slices {
		if _, statErr := os.Stat(sliceGuaranteePath(root, master.RunID, sliceID)); !os.IsNotExist(statErr) {
			t.Fatalf("test setup unexpectedly has slice guarantee evidence for %s: %v", sliceID, statErr)
		}
	}
	summary, err := Seal(root, pkg, master.RunID, []string{guaranteeSealSkipID}, true, "", SealOptions{GuaranteeWaiverReason: "user accepts that responsibility slices were not completed"})
	if err != nil {
		t.Fatalf("waived retained master required a nonexistent slice guarantee sidecar: %v", err)
	}
	if summary.Status != "SEALED" || summary.RequirementGuarantee == nil || summary.RequirementGuarantee.Activation != guaranteeWaived {
		t.Fatalf("waived retained master summary = %#v", summary)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".gates", "results", master.RunID+".blackbox-cases.md")); !os.IsNotExist(statErr) {
		t.Fatalf("waived retained master materialized slice cases: %v", statErr)
	}
}

func TestRetainedMasterWaiverPreservesAvailableSliceEvidence(t *testing.T) {
	root := t.TempDir()
	owners := map[string]string{"AC-001": "slice-a", "AC-002": "slice-b", "AC-011": masterMergeOwner}
	master := whitebox35AFixR2GuaranteedState(t)
	master.RunID = "waived-master-partial-evidence"
	master.RetainedOverall = true
	master.RouteMode = "merge"
	master.Slicing = &Slicing{Decision: "split", SplitCount: 2, Slices: []string{"slice-a", "slice-b"}, ACResponsibilities: copyStringMap(owners)}
	master.RequirementGuarantee.Activation = guaranteeWaived
	master.RequirementGuarantee.Reason = "user accepts the incomplete slice set"

	slice := whitebox35AFixR2GuaranteedState(t)
	slice.RunID = "slice-a"
	slice.SplitMasterRunID = master.RunID
	slice.RouteMode = "custom"
	slice.CurrentSnapshot = "slice-a-sealed-candidate"
	slice.Slicing = &Slicing{Decision: "split", MasterRunID: master.RunID, ACResponsibilities: copyStringMap(owners)}
	slice.RequirementGuarantee.ActivationSource = "INHERITED_FROM_MASTER:" + master.RunID
	testCase := QACase{ID: "CASE-A", Mode: blackboxQAID, Description: "slice A completed behavior", Procedure: "run slice A public command", Oracle: "slice A behavior passes", AcceptanceCriteria: []string{"AC-001"}, ReviewStatus: "PASS"}
	slice.QACasesByMode = map[string][]QACase{blackboxQAID: {testCase}}
	whitebox35AFixR2ApproveMode(t, &slice, blackboxQAID)
	slice.QAExecutionByMode = map[string]QAExecutionResult{blackboxQAID: {Status: "PASS", Snapshot: slice.CurrentSnapshot, Cases: []QAResultRecord{{CaseID: testCase.ID, Mode: testCase.Mode, Outcome: "PASS", Procedure: "executed slice A public command", Observation: "captured the documented slice A result", OracleResult: "captured result matched the approved slice A oracle", Origin: "executed"}}}}
	if err := saveSliceGuaranteeRecord(root, slice); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sliceGuaranteePath(root, master.RunID, "slice-b")); !os.IsNotExist(err) {
		t.Fatalf("test setup unexpectedly created slice B evidence: %v", err)
	}

	report := deriveRequirementGuarantee(root, master)
	if report.Status != guaranteeWaived {
		t.Fatalf("waived report status = %q", report.Status)
	}
	var completed, missing RequirementGuaranteeItemReport
	for _, item := range report.Items {
		switch item.AcceptanceID {
		case "AC-001":
			completed = item
		case "AC-002":
			missing = item
		}
	}
	if completed.Owner != "slice-a" || len(completed.Cases) != 1 || completed.Cases[0] != "slice-a::BLACKBOX::CASE-A" || completed.ReviewStatus != "PASS" || completed.Execution != "PASS" {
		t.Fatalf("available slice A evidence was discarded: %#v", completed)
	}
	if missing.Owner != "slice-b" || len(missing.Cases) != 0 || missing.ReviewStatus != "PENDING" || missing.Execution != "PENDING" {
		t.Fatalf("missing slice B was not retained as an unresolved item: %#v", missing)
	}
	if err := materializeBlackboxCases(root, master); err != nil {
		t.Fatal(err)
	}
	materialized, err := os.ReadFile(filepath.Join(root, ".gates", "results", master.RunID+".blackbox-cases.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(materialized), "slice-a::BLACKBOX::CASE-A") {
		t.Fatalf("available waived slice case was not materialized:\n%s", materialized)
	}
}

func TestSealMaterializationFailureKeepsActiveStateAndCanRetry(t *testing.T) {
	root, pkg, state := readyGuaranteeRoute(t, "materialize-retry", false)
	state = recordSlicing(t, root, pkg, state, "no-split")
	state = setRoute(t, root, pkg, state, "custom", []string{blackboxQAID})
	state = recordGuaranteeDesignAndReview(t, root, pkg, state, "", []QACaseInput{{Mode: "blackbox", Description: "all confirmed behavior", Procedure: "run the documented public command and capture its output", Oracle: "both specified outcomes are visible", AcceptanceCriteria: []string{"AC-001", "AC-009"}}})
	state = advanceGuaranteeSnapshot(t, root, pkg, state, "materialize-retry-delivery")
	execution := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err := RecordQAExecution(root, pkg, state.RunID, execution, passingExecution(state.allQACases()), "")
	if err != nil {
		t.Fatal(err)
	}

	materialized := filepath.Join(root, ".gates", "results", state.RunID+".blackbox-cases.md")
	if err := os.MkdirAll(materialized, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Seal(root, pkg, state.RunID, nil, false, ""); err == nil {
		t.Fatal("Seal unexpectedly succeeded while the materialization target was a directory")
	}
	checkpoint, err := LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Status != "ACTIVE" {
		t.Fatalf("materialization failure persisted terminal state %q", checkpoint.Status)
	}
	if _, err := os.Stat(RunSummaryPath(root, state.RunID)); !os.IsNotExist(err) {
		t.Fatalf("materialization failure wrote a Seal summary: %v", err)
	}

	if err := os.Remove(materialized); err != nil {
		t.Fatal(err)
	}
	summary, err := Seal(root, pkg, state.RunID, nil, false, "")
	if err != nil {
		t.Fatalf("Seal retry failed after materialization target was fixed: %v", err)
	}
	if summary.Status != "SEALED" {
		t.Fatalf("Seal retry status = %q", summary.Status)
	}
	data, err := os.ReadFile(materialized)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "CASE-001") {
		t.Fatalf("Seal retry did not materialize approved cases:\n%s", data)
	}
}

func TestQAExecutionEvidenceRejectsOnlyExactNormalizedProtocolSentinels(t *testing.T) {
	for _, input := range []QAResultInput{
		{CaseID: "CASE-001", Outcome: "PASS", Procedure: "p", Observation: "captured output", OracleResult: "compared with the oracle"},
		{CaseID: "CASE-001", Outcome: "PASS", Procedure: "\tSKIPPED\n", Observation: "captured output", OracleResult: "compared with the oracle"},
		{CaseID: "CASE-001", Outcome: "PASS", Procedure: "ran the approved command", Observation: "captured output", OracleResult: " Authorized\tPASS "},
	} {
		if err := validateQAExecutionEvidence(input); err == nil {
			t.Fatalf("exact normalized protocol sentinel was accepted: %#v", input)
		}
	}

	for _, input := range []QAResultInput{
		{CaseID: "CASE-001", Outcome: "PASS", Procedure: "Executed the approved blackbox procedure in the isolated candidate environment for CASE-001", Observation: "The documented public behavior matched the approved case oracle", OracleResult: "Matched after host validation"},
		{CaseID: "CASE-001", Outcome: "PASS", Procedure: "not run yet", Observation: "pending verification", OracleResult: "will check later"},
	} {
		if err := validateQAExecutionEvidence(input); err != nil {
			t.Fatalf("free-form evidence was semantically rejected: %v", err)
		}
	}
}

func TestQAExecutionRecordsRealEvidenceContainingSentinelWords(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "sentinel-words-in-real-evidence")
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	observation := "命令退出 0，输出显示 0 skipped rows，且无 authorized PASS row"
	state, err := RecordQAExecution(root, pkg, state.RunID, dispatch, []QAResultInput{{
		CaseID:       "CASE-002",
		Outcome:      "PASS",
		Procedure:    "运行文档化命令并捕获退出码与标准输出",
		Observation:  observation,
		OracleResult: "捕获结果与已批准 oracle 的每项断言一致",
	}}, "")
	if err != nil {
		t.Fatalf("real evidence containing protocol sentinel words was rejected: %v", err)
	}
	result := state.qaExecution("blackbox")
	if result.Status != "PASS" || len(result.Cases) != 1 || result.Cases[0].Observation != observation {
		t.Fatalf("real evidence containing sentinel words was not recorded intact: %#v", result)
	}
}
