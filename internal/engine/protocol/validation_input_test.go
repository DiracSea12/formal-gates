package protocol

import (
	"testing"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
)

func TestValidationInputIssuesReviewScopeFromBaseline(t *testing.T) {
	key := runtime.TaskKey{Node: "qa", Step: "review", Scope: "1:BLACKBOX:case-version"}
	pending := PendingAction{ActionID: "review-action", Task: key}
	cd := &compiler.CompiledDefinition{Steps: []compiler.CompiledStep{{Header: compiler.CompiledHeader{ID: "review"}, Payload: compiler.CompiledAgentStep{Handler: "engine.qa.review.rationale"}}}}
	set := &CaseSet{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewBlackbox}}}
	state := &State{RunID: "run", SelectedGates: []string{"blackbox"}, DispatchBindings: map[string]WorkBinding{pending.ActionID: {Task: key, RequirementRevision: "issued-revision"}}, Validation: &Validation{Sets: map[coverage.ReviewKind]*CaseSet{coverage.ReviewBlackbox: set}}}
	if input := state.validationInput(pending, cd); input.ReviewScope != coverage.ScopeFull || input.Binding.RequirementRevision != "issued-revision" {
		t.Fatal("first review did not receive original binding and FULL baseline")
	}
	set.ReviewHistory = []CaseReviewEvidence{{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewWhitebox}}, Review: coverage.ReviewResult{Scope: coverage.ScopeFull}}}
	if state.validationInput(pending, cd).ReviewScope != coverage.ScopeFull {
		t.Fatal("another mode supplied the baseline")
	}
	set.ReviewHistory = append(set.ReviewHistory, CaseReviewEvidence{Manifest: set.Manifest, Review: coverage.ReviewResult{Scope: coverage.ScopeFull}})
	if state.validationInput(pending, cd).ReviewScope != coverage.ScopeAffected {
		t.Fatal("established baseline did not issue AFFECTED")
	}
}

func TestSharedReviewInheritanceIncludesApprovedOperations(t *testing.T) {
	step := coverage.SharedStep{ID: "setup", Procedure: "prepare documented public state", Operations: []coverage.Operation{{Kind: coverage.OperationShell, Command: "./app prepare", Stdin: "original input"}}}
	set := &CaseSet{Manifest: coverage.AcceptanceManifest{SharedSteps: []coverage.SharedStep{step}, Cases: []coverage.AcceptanceCase{{CaseID: "case", SharedSteps: []string{"setup"}}}}}
	set.ReviewHistory = []CaseReviewEvidence{{Manifest: set.Manifest, Review: coverage.ReviewResult{SharedStepDecisions: []coverage.ReviewDecision{{ID: "setup", Status: coverage.StatusPass, Reason: "approved shared operation and oracle context"}}}}}
	if len(inheritableSharedReviews(set)) != 1 {
		t.Fatal("identical approved shared operations lost inheritance")
	}
	before := caseReviewIdentity(set.Manifest, "case")
	changed := step
	changed.Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: "./app prepare-other", Stdin: "original input"}}
	set.Manifest.SharedSteps = []coverage.SharedStep{changed}
	if len(inheritableSharedReviews(set)) != 0 || caseReviewIdentity(set.Manifest, "case") == before {
		t.Fatal("changed approved operations inherited old review identity")
	}
}
