package validate

import (
	"strings"
	"testing"
)

func TestRepairWaveSplitChildCannotClaimGuaranteeScopeBeforeRevisionRebinding(t *testing.T) {
	state := whitebox35AFixR2GuaranteedState(t)
	state.SplitMasterRunID = "retained-master"
	state.Slicing = nil
	state.RouteMode = "custom"
	state.RequirementGuarantee.Activation = guaranteeActive

	if err := guaranteeReadyForQA(state); err == nil || !strings.Contains(err.Error(), "responsibility binding is missing") {
		t.Fatalf("revision-invalidated slice was considered QA-ready: %v", err)
	}
	testCase := QACase{ID: "CASE-001", Mode: blackboxQAID, AcceptanceCriteria: []string{"AC-001"}}
	if err := validateGuaranteeCaseBindings(state, testCase); err == nil || !strings.Contains(err.Error(), "responsibility binding is missing") {
		t.Fatalf("revision-invalidated slice claimed another owner's AC: %v", err)
	}
}

func TestRepairWaveGuaranteeClosureListsEveryStableGap(t *testing.T) {
	report := RequirementGuaranteeReport{Items: []RequirementGuaranteeItemReport{
		{RequirementID: "REQ-001", AcceptanceID: "AC-001", ReviewStatus: "PENDING", Execution: "PENDING"},
		{RequirementID: "REQ-001", AcceptanceID: "AC-002", Cases: []string{"slice-a::blackbox::CASE-002"}, ReviewStatus: "FAIL", Execution: "PENDING"},
		{RequirementID: "REQ-002", AcceptanceID: "AC-003", Cases: []string{"master::merge::CASE-003"}, ReviewStatus: "PASS", Execution: "PENDING"},
	}}
	gaps := requirementGuaranteeGapDetails(report)
	if len(gaps) != 3 {
		t.Fatalf("gap count = %d, want 3: %#v", len(gaps), gaps)
	}
	for _, id := range []string{"REQ-001/AC-001", "slice-a::blackbox::CASE-002", "master::merge::CASE-003"} {
		if !strings.Contains(strings.Join(gaps, "\n"), id) {
			t.Fatalf("stable gap identifier %q missing from %#v", id, gaps)
		}
	}
}

func TestRepairWaveRetainedMasterExecutionUsesCanonicalMergedBinding(t *testing.T) {
	state := RunState{
		RetainedOverall: true,
		RouteMode:       "merge",
		SelectedGates:   []string{mergeQAID, mergeGateID},
		Slicing:         &Slicing{Decision: "split"},
	}
	for _, requested := range []string{"", blackboxQAID, whiteboxQAID} {
		if got := qaExecutionRecordMode(state, requested); got != "" {
			t.Fatalf("retained-master execution mode for %q = %q, want canonical merged key", requested, got)
		}
	}
	state.RetainedOverall = false
	if got := qaExecutionRecordMode(state, whiteboxQAID); got != whiteboxQAID {
		t.Fatalf("ordinary execution mode = %q, want %q", got, whiteboxQAID)
	}
}
