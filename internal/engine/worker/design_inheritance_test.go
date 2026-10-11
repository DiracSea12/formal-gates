package worker

import (
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"reflect"
	"testing"
)

func TestDesignSelectedItemsRetainOmittedSemantics(t *testing.T) {
	operation := coverage.Operation{Kind: coverage.OperationArgv, Argv: []string{"./app", "original input"}}
	original := coverage.AcceptanceCase{CaseID: "issued-case", Scenario: "SUCCESS", PublicEntry: "./app", Preconditions: "built original app", Steps: "run original input", Oracle: "original output", Operations: []coverage.Operation{operation}, SharedSteps: []string{"issued-step"}, Files: []string{"app_test.go"}, TestRef: "app_test.go#TestOriginal"}
	shared := coverage.SharedStep{ID: "issued-step", Procedure: "prepare original input", Artifacts: []string{"prepared.txt"}, Independent: true, Operations: []coverage.Operation{operation}, Files: []string{"setup_test.go"}}
	input := protocol.WorkerInput{Handler: "engine.qa.design.v4", Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewWhitebox}, Cases: []coverage.AcceptanceCase{original}, SharedSteps: []coverage.SharedStep{shared}}}}
	value, _, err := decodeRoleBody(input, []byte(`{"cases":[{"case":1,"oracle":"new output"}],"sharedSteps":[{"step":1,"procedure":"new preparation"}],"coverage":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	item := value.Validation.Manifest.Cases[0]
	expected := Case{CaseID: original.CaseID, Scenario: original.Scenario, PublicEntry: original.PublicEntry, Preconditions: original.Preconditions, Steps: original.Steps, Oracle: "new output", Operations: original.Operations, SharedSteps: original.SharedSteps}
	if !reflect.DeepEqual(item, expected) {
		t.Fatalf("partial case changed omitted values: %#v", item)
	}
	step := value.Validation.Manifest.SharedSteps[0]
	if step.ID != shared.ID || step.Procedure != "new preparation" || !step.Independent || !reflect.DeepEqual(step.Artifacts, shared.Artifacts) || !reflect.DeepEqual(step.Operations, shared.Operations) {
		t.Fatal("partial shared step changed omitted values", step)
	}
	// Relations are retained by the existing issued manifest when no new file selection is supplied.
	if item.Files != nil || item.TestFile != 0 || item.TestSymbol != 0 || step.Files != nil {
		t.Fatal("omission invented a new relation selection")
	}
	value, _, err = decodeRoleBody(input, []byte(`{"cases":[{"case":1,"operations":[],"sharedSteps":[],"preconditions":""}],"sharedSteps":[{"step":1,"artifacts":[],"independent":false}],"coverage":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(value.Validation.Manifest.Cases[0].Operations) != 0 || len(value.Validation.Manifest.Cases[0].SharedSteps) != 0 || value.Validation.Manifest.Cases[0].Preconditions != "" || value.Validation.Manifest.SharedSteps[0].Independent || len(value.Validation.Manifest.SharedSteps[0].Artifacts) != 0 {
		t.Fatal("explicit changed values were not applied")
	}
	if _, _, err := decodeRoleBody(input, []byte(`{"cases":[{"oracle":"new output"}],"coverage":[]}`)); err == nil {
		t.Fatal("new case lacks its required scenario")
	}
}

func TestSharedInstructionSelectionAfterEditingOriginal(t *testing.T) {
	input := retainedDesignInput()
	input.Validation.Manifest.SharedSteps = []coverage.SharedStep{input.Validation.Manifest.SharedSteps[0]}
	raw := []byte(`{"sharedSteps":[{"step":1,"procedure":"edited original setup"},{"procedure":"new setup"}],"cases":[{"case":2,"sharedSteps":[2]}],"coverage":[null,null]}`)
	result, err := DecodeResultWithPaths(input, "observation", raw, "original-semantic-output", []string{})
	if err != nil {
		t.Fatal(err)
	}
	manifest := result.Result.Validation.Manifest
	if len(manifest.SharedSteps) != 2 || manifest.SharedSteps[0].ID != input.Validation.Manifest.SharedSteps[0].ID || manifest.SharedSteps[0].Procedure != "edited original setup" || manifest.Cases[1].SharedSteps[0] != manifest.SharedSteps[1].ID {
		t.Fatalf("original+new instruction choice resolved incorrectly: %#v", manifest)
	}
}
