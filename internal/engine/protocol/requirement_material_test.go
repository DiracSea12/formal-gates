package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCurrentMaterialBindingsReachCoverage(t *testing.T) {
	t.Parallel()
	initial := &IntakeConfirmationReceipt{RequirementSource: "requirements.md", RequirementRevision: "initial-requirement", SolutionSource: "design.md", SolutionRevision: "initial-solution", Artifacts: []IntakeArtifact{{Path: "design.md", Revision: "initial-solution"}}}
	current := &IntakeConfirmationReceipt{RequirementSource: "requirements.md", RequirementRevision: "current-requirement", SolutionSource: "design.md", SolutionRevision: "current-solution", Artifacts: []IntakeArtifact{{Path: "design.md", Revision: "current-solution"}, {Path: "reference.md", Revision: "reference"}}}
	s := &State{RunID: "material", IntakeConfirmationReceipt: initial, Validation: &Validation{RequirementRevision: current.RequirementRevision}, RequirementRevisions: []ConfirmedRevision{{Revision: initial.RequirementRevision}, {Revision: current.RequirementRevision, Intake: current}, {Revision: "later-low-level-confirmation"}}}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var recovered State
	if err = json.Unmarshal(raw, &recovered); err != nil {
		t.Fatal(err)
	}
	contract := recovered.coverageContract(nil)
	if contract.RequiredSources.Binding.SolutionRevision != current.SolutionRevision || !reflect.DeepEqual(recovered.CurrentIntakeConfirmation(), current) || !reflect.DeepEqual(recovered.IntakeConfirmationReceipt, initial) {
		t.Fatal("coverage or recovered material used the initial solution instead of the latest complete confirmation")
	}
	fallback := &State{IntakeConfirmationReceipt: initial, Validation: &Validation{RequirementRevision: initial.RequirementRevision}}
	if fallback.coverageContract(nil).RequiredSources.Binding.SolutionRevision != initial.SolutionRevision {
		t.Fatal("initial material binding was not retained before a complete new confirmation")
	}
}
