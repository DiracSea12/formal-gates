package facade

import (
	"encoding/json"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

// Completion reconciles approved cases, original evidence and current sources;
// the wave's COMPLETE label alone cannot authorize native delivery.
func TestAffectedCompletionRequiresCurrentApprovedEvidence(t *testing.T) {
	d := repairedExecutionDriver(t)
	d.recordExecutionScope(coverage.ReviewBlackbox, "normal")
	action, input := d.action("engine.qa.execution.v3")
	report := executionFor(input)
	d.business(action, nativeExecutionResult(input, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &report}))
	d.assertRequiredValidationComplete(coverage.ScopeAffected)
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	original, err := json.Marshal(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"missing-case", "missing-original-pass", "affected-complement", "unapproved-source", "thawed-cases", "pending-gate", "gate-without-source", "missing-development-source", "candidate-changed", "requirement-changed", "route-changed", "repair-verification-missing", "unknown-execution", "pending-requirement"} {
		t.Run(name, func(t *testing.T) {
			var state protocol.State
			if err := json.Unmarshal(original, &state); err != nil {
				t.Fatal(err)
			}
			wave := &state.Validation.Waves[len(state.Validation.Waves)-1]
			var qa, gate *protocol.ValidationTarget
			for _, target := range wave.Targets {
				if target.Kind == "QA" {
					qa = target
				} else {
					gate = target
				}
			}
			switch name {
			case "missing-case":
				qa.Execution.Records = qa.Execution.Records[:1]
			case "missing-original-pass":
				for _, target := range state.Validation.Waves[0].Targets {
					if target.Kind == "QA" {
						delete(state.Results, target.ResultAction)
					}
				}
			case "affected-complement":
				qa.RequiredCases = append(qa.RequiredCases, "invalid")
			case "unapproved-source":
				state.Validation.Sets[coverage.ReviewBlackbox].Review.SourceDecisions[0].Status = coverage.StatusFail
			case "thawed-cases":
				state.Validation.Sets[coverage.ReviewBlackbox].Review = nil
			case "pending-gate":
				gate.Status = "PENDING"
			case "gate-without-source":
				gate.ResultAction = ""
			case "missing-development-source":
				wave.SourceTasks = wave.SourceTasks[:len(wave.SourceTasks)-1]
			case "candidate-changed":
				state.Validation.Candidate.Digest = "different-tree"
			case "requirement-changed":
				state.Validation.Generation++
			case "route-changed":
				state.SelectedGates = append(state.SelectedGates, "security")
			case "unknown-execution":
				result := state.Results[qa.ResultAction]
				result.Outcome = protocol.OutcomeRuntimeError
				state.Results[qa.ResultAction] = result
			case "pending-requirement":
				state.PendingAsks["revision"] = protocol.PendingAsk{RequestID: "revision", Control: protocol.ControlRequestRequirementChange}
			case "repair-verification-missing":
				for id, result := range state.Results {
					if result.Validation != nil && result.Validation.Binding.VerificationSource != "" {
						delete(state.Results, id)
					}
				}
			}
			if state.FinalValidationComplete(d.f.definition) || state.AdmitPromotion(d.f.definition) == nil {
				t.Fatal("incomplete or stale evidence authorized delivery")
			}
		})
	}
	d.reopen()
	d.assertRequiredValidationComplete(coverage.ScopeAffected)
	if len(d.run.Validation.Waves) != 2 || d.run.Validation.Waves[0].Scope != coverage.ScopeFull {
		t.Fatal("repair omitted the first FULL or added another validation wave")
	}
	source := d.run.Validation.Candidate
	params := protocol.PromotionParameters{Source: source.Identity, SourceDigest: source.Digest, TargetRoot: d.f.root, TargetBase: snapshot.State.BaseSnapshot, TargetStart: protocol.PromotionObservation{Head: snapshot.State.BaseSnapshot, Tree: frozenTree, IndexTree: frozenTree, Clean: true}, Message: "validated repaired candidate"}
	for id, resource := range snapshot.State.NativeResources {
		if resource.Role == "candidate" && resource.Identity == source.Identity && resource.Status == "READY" {
			params.SourceWorkspace = id
		}
	}
	if err := d.f.engine.OpenPromotion(params, emptyFingerprint()); err != nil {
		t.Fatal(err)
	}
	pending, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	intent, ok := pending.State.PromotionIntent()
	if !ok {
		t.Fatal("qualified affected source did not create its native intent")
	}
	promoted := source.Identity
	proof := protocol.CandidatePromotionReceipt{OperationID: intent.ActionID, Provider: "git", SourceWorkspace: params.SourceWorkspace, Source: source.Identity, SourceDigest: source.Digest, TargetRoot: params.TargetRoot, TargetBase: params.TargetBase, TargetStart: params.TargetStart, Final: promoted, FinalDigest: source.Digest}
	if err := d.f.engine.RecordCandidatePromotion(intent.ActionID, proof, emptyFingerprint()); err != nil {
		t.Fatal(err)
	}
	d.reopen()
	state, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !state.State.DeliveryComplete(d.f.definition) || state.State.CandidatePromotion.Source != report.Binding.Candidate.Identity || state.State.CandidatePromotion.Final != promoted || state.State.CandidatePromotion.FinalDigest != report.Binding.Candidate.Digest {
		t.Fatal("native delivery did not bind the completed affected candidate")
	}
	if raw := state.State.Results[action].Validation; raw == nil || raw.Binding != input.Binding || len(raw.Execution.Records) != len(report.Records) {
		t.Fatal("promotion changed original affected execution evidence")
	}
}
