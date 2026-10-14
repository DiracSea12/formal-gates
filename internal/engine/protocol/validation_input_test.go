package protocol

import (
	"encoding/json"
	"fmt"
	"testing"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
)

func TestSemanticRejectionFeedbackRequiresOriginalBinding(t *testing.T) {
	for _, changed := range []string{"none", "task", "binding", "previous", "attempt"} {
		t.Run(changed, func(t *testing.T) {
			key := runtime.TaskKey{Node: "qa", Step: "design", Scope: "current-duty"}
			pending := PendingAction{ActionID: "retry", Task: key, AttemptID: "current-attempt"}
			binding := WorkBinding{Task: key, Generation: 2, RequirementRevision: "confirmed"}
			cd := &compiler.CompiledDefinition{Steps: []compiler.CompiledStep{{Header: compiler.CompiledHeader{ID: "design"}, Payload: compiler.CompiledAgentStep{Handler: "engine.qa.design.v5"}}}}
			state := &State{DispatchBindings: map[string]WorkBinding{"retry": binding, "rejected": binding}, Attempts: map[runtime.TaskKey]Attempt{key: {PreviousAction: "rejected"}}, RejectedResults: map[string]RejectedResult{"rejected": {ActionID: "rejected", Task: key, AttemptID: "original-attempt", Status: "REJECTED_INVALID_RESULT", Reason: "original semantic defect", Payload: WorkerResult{Outcome: OutcomePass}}}, RecoveryRecords: []RecoveryRecord{{ActionID: "rejected", Task: key, AttemptID: "original-attempt", Detail: "original semantic defect"}}, Validation: &Validation{Sets: map[coverage.ReviewKind]*CaseSet{}}}
			switch changed {
			case "task":
				state.RecoveryRecords[0].Task.Scope = "another-duty"
			case "binding":
				previous := binding
				previous.Generation = 1
				state.DispatchBindings["rejected"] = previous
			case "previous":
				state.Attempts[key] = Attempt{PreviousAction: "another-execution"}
			case "attempt":
				state.RecoveryRecords[0].AttemptID = "another-attempt"
			}
			input := state.validationInput(pending, cd)
			if (input.Interruption != nil) != (changed == "none") {
				t.Fatal("rejection feedback crossed its original binding", input.Interruption)
			}
		})
	}
}

func TestValidationInputIssuesReviewScopeFromBaseline(t *testing.T) {
	key := runtime.TaskKey{Node: "qa", Step: "review", Scope: "1:BLACKBOX:case-version"}
	pending := PendingAction{ActionID: "review-action", Task: key}
	cd := &compiler.CompiledDefinition{Steps: []compiler.CompiledStep{{Header: compiler.CompiledHeader{ID: "review"}, Payload: compiler.CompiledAgentStep{Handler: "engine.qa.review.rationale.v2"}}}}
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
	state := &State{}
	if len(state.inheritableSharedReviews(set)) != 1 {
		t.Fatal("identical approved shared operations lost inheritance")
	}
	before := caseReviewIdentity(set.Manifest, "case")
	changed := step
	changed.Operations = []coverage.Operation{{Kind: coverage.OperationShell, Command: "./app prepare-other", Stdin: "original input"}}
	set.Manifest.SharedSteps = []coverage.SharedStep{changed}
	if len(state.inheritableSharedReviews(set)) != 0 || caseReviewIdentity(set.Manifest, "case") == before {
		t.Fatal("changed approved operations inherited old review identity")
	}
}

func TestSharedTestMaterialInheritanceRequiresCandidateContent(t *testing.T) {
	for _, variant := range []string{"same", "equivalent", "changed", "unavailable"} {
		t.Run(variant, func(t *testing.T) {
			decision := coverage.ReviewDecision{ID: "setup", Status: coverage.StatusPass, Reason: "Read the associated fixture and independently classified it as test material"}
			manifest := coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewWhitebox}, SharedSteps: []coverage.SharedStep{{ID: "setup", Files: []string{"fixture.data"}, Procedure: "prepare the test input"}}}
			set := &CaseSet{Candidate: "after", Manifest: manifest, ReviewHistory: []CaseReviewEvidence{{Candidate: "before", Manifest: manifest, Review: coverage.ReviewResult{Scope: coverage.ScopeFull, SharedStepDecisions: []coverage.ReviewDecision{decision}}}}}
			state := &State{HostActionReceipts: map[string]HostActionReceipt{}}
			if variant == "same" {
				set.Candidate = "before"
			}
			if variant == "equivalent" || variant == "changed" {
				for _, identity := range []string{"before", "after"} {
					digest := "sha256:original-tree"
					if variant == "changed" && identity == "after" {
						digest = "sha256:changed-fixture"
					}
					raw, err := json.Marshal(CandidateEvidence{Identity: identity, Digest: digest})
					if err != nil {
						t.Fatal(err)
					}
					var values map[string]any
					if err := json.Unmarshal(raw, &values); err != nil {
						t.Fatal(err)
					}
					state.HostActionReceipts[identity] = HostActionReceipt{AdapterOperation: "op.candidate", Status: HostActionStatusExecuted, AdapterEvidence: &AdapterEvidence{Values: values}}
				}
			}
			allowed := variant == "same" || variant == "equivalent"
			if got := state.inheritableSharedReviews(set); (len(got) == 1) != allowed {
				t.Fatalf("shared material inheritance=%v, expected approval=%v", got, allowed)
			}
			inherited := decision
			inherited.Inherited = true
			if err := state.validateReviewPolicy(set, coverage.ReviewResult{Scope: coverage.ScopeAffected, SharedStepDecisions: []coverage.ReviewDecision{inherited}}); (err == nil) != allowed {
				t.Fatalf("shared approval admission=%v, expected approval=%v", err, allowed)
			}
		})
	}
}

func TestIntakeConfirmationFollowsSignedWorkEpoch(t *testing.T) {
	initial := &IntakeConfirmationReceipt{RequirementRevision: "same-requirement", SolutionRevision: "initial-solution"}
	confirmed := &IntakeConfirmationReceipt{RequirementRevision: "same-requirement", SolutionRevision: "confirmed-solution"}
	latest := &IntakeConfirmationReceipt{RequirementRevision: "same-requirement", SolutionRevision: "latest-solution"}
	for _, variant := range []string{"initial", "confirmed", "retry", "unissued", "before-confirmation", "missing-epoch", "wrong-requirement", "missing-intake"} {
		t.Run(variant, func(t *testing.T) {
			binding := WorkBinding{Task: runtime.TaskKey{Node: "qa", Step: "review"}, RequirementRevision: "same-requirement", Generation: 5}
			issued := uint64(6)
			want := confirmed
			switch variant {
			case "initial":
				binding.Generation, want = 0, initial
			case "retry":
				issued = 20
			case "unissued":
				issued, want = 0, nil
			case "before-confirmation":
				issued, want = 4, nil
			case "missing-epoch":
				binding.Generation, want = 4, nil
			case "wrong-requirement":
				binding.RequirementRevision, want = "another-requirement", nil
			case "missing-intake":
				want = nil
			}
			state := &State{IntakeConfirmationReceipt: initial, RequirementRevisions: []ConfirmedRevision{{Revision: initial.RequirementRevision, StateRevision: 1}, {Revision: confirmed.RequirementRevision, StateRevision: 5, Intake: confirmed}, {Revision: latest.RequirementRevision, StateRevision: 10, Intake: latest}}, DispatchBindings: map[string]WorkBinding{"original": binding}, DispatchIssueRevisions: map[string]uint64{"original": issued}}
			if variant == "missing-intake" {
				state.RequirementRevisions[1].Intake = nil
			}
			if got := state.intakeConfirmationForWork(binding); got != want {
				t.Fatalf("signed confirmation = %v, want %v", got, want)
			}
		})
	}
}

func TestCarriedReviewRequiresOriginalCasesAndCandidate(t *testing.T) {
	cd := &compiler.CompiledDefinition{Steps: []compiler.CompiledStep{{Header: compiler.CompiledHeader{NodeID: "qa", ID: "design"}, Payload: compiler.CompiledAgentStep{Handler: "engine.qa.design.v5"}}, {Header: compiler.CompiledHeader{NodeID: "qa", ID: "review"}, Payload: compiler.CompiledAgentStep{Handler: "engine.qa.review.rationale.v2"}}}}
	for _, mode := range []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox, coverage.ReviewMerge} {
		for _, variant := range []string{"carry", "no-carry", "redo", "missing-intake", "case-content", "candidate-content", "reopened"} {
			t.Run(string(mode)+"/"+variant, func(t *testing.T) {
				original := coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{SourceBinding: coverage.SourceBinding{RunID: "run", RequirementRevision: "original", SolutionRevision: "original-solution"}, ReviewKind: mode, RouteScope: "original-route"}, Cases: []coverage.AcceptanceCase{{CaseID: "case", Oracle: "documented result"}}}
				set := &CaseSet{Status: "REVIEW", DesignTask: pipelineTask(cd, "engine.qa.design.v5", fmt.Sprintf("0:%s:1", mode)), Manifest: original, Candidate: "original-candidate"}
				state := &State{RunID: "run", SelectedGates: []string{"blackbox", "whitebox", mergeQAGateID}, IntakeConfirmationReceipt: &IntakeConfirmationReceipt{RequirementRevision: "original", SolutionRevision: "original-solution"}, Validation: &Validation{Generation: 10, RequirementRevision: "current", Sets: map[coverage.ReviewKind]*CaseSet{mode: set}}}
				binding := WorkBinding{Task: state.reviewTask(mode, 0, digestOfCanonical(original), cd), RequirementRevision: "original", RouteScope: "original-route", CaseVersion: digestOfCanonical(original), Candidate: "original-candidate"}
				choice := RequirementWorkDecision{Work: RequirementWork{ActionID: "review", Task: binding.Task, RequirementRevision: "original", IssuedRevision: 2}, Disposition: RequirementWorkCarry, Reason: "unchanged review duty"}
				state.RequirementRevisions = []ConfirmedRevision{{Revision: "original", StateRevision: 1}, {Revision: "current", StateRevision: 10, Intake: &IntakeConfirmationReceipt{RequirementRevision: "current", SolutionRevision: "current-solution"}, WorkDecisions: []RequirementWorkDecision{choice}}}
				state.DispatchBindings = map[string]WorkBinding{"review": binding}
				state.DispatchIssueRevisions = map[string]uint64{"review": 2}
				set.Manifest.Binding.RequirementRevision, set.Manifest.Binding.SolutionRevision, set.Manifest.Binding.RouteScope = "current", "current-solution", "current-route"
				switch variant {
				case "no-carry":
					state.RequirementRevisions[1].WorkDecisions = nil
				case "redo":
					state.RequirementRevisions[1].WorkDecisions[0].Disposition = RequirementWorkRedo
				case "missing-intake":
					state.IntakeConfirmationReceipt = nil
				case "case-content":
					set.Manifest.Cases[0].Oracle = "another result"
				case "candidate-content":
					set.Candidate = "changed-candidate"
				case "reopened":
					set.Status = "THAWED"
					state.PendingAsks = map[string]PendingAsk{"reopen": {Validation: &ValidationRequest{Kind: "THAW", Mode: mode}}}
					state.Decisions = map[string]RecordedDecision{"reopen": {Choice: "approve", Revision: 11}}
				}
				allowed := variant == "carry" || variant == "candidate-content" && mode != coverage.ReviewWhitebox
				if got := state.prewaveWorkCurrent(binding, cd); got != allowed {
					t.Fatalf("review applicability = %t, want %t", got, allowed)
				}
			})
		}
	}
}
