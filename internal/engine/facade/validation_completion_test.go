package facade

import (
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/testkit"
)

func TestEmptyWhiteboxPathsDoNotRepeatPromotionAfterRecovery(t *testing.T) {
	t.Parallel()
	d := newValidationDriver(t, "whitebox", "quality")
	d.fixtureQAResults = map[string]protocol.CheckEvidence{"normal": {ExitCode: 0}, "invalid": {ExitCode: 2}}
	dev, input := d.action("engine.development.batch.v6")
	d.business(dev, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented input validation"}})
	d.materialize()
	d.materialize()
	design, input := d.action("engine.qa.design.v6")
	manifest := simpleManifest(input)
	for i := range manifest.Cases {
		manifest.Cases[i].Mode = coverage.CaseWhitebox
		manifest.Cases[i].PublicEntry, manifest.Cases[i].Preconditions, manifest.Cases[i].Steps = "", "", ""
	}
	d.business(design, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest, Development: &protocol.DevelopmentEvidence{Paths: []string{}}})
	d.reopen()
	state := driverState(t, d)
	if state.Validation.Sets[coverage.ReviewWhitebox] == nil || state.Results[design].Validation == nil || state.Results[design].Validation.Development == nil {
		t.Fatalf("empty-path whitebox design was not accepted: %+v", state.RejectedResults)
	}
	if state.Validation.Sets[coverage.ReviewWhitebox].Paths != nil || state.Results[design].Validation.Development.Paths == nil {
		t.Fatal("recovery did not retain the original empty-path protocol while reloading the case set")
	}
	if len(state.PendingHostActions) != 1 {
		t.Fatalf("empty-path reload left an unused workspace responsibility: %+v", state.PendingHostActions)
	}
	artifact := onlyCandidate(t, d, "QA_ARTIFACT")
	completion := candidateCompletion(t, d, artifact)
	d.submit(completion)
	d.reopen()
	before, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	d.submit(completion)
	after, err := d.f.engine.Load()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("repeated artifact acceptance changed its original evidence or responsibility")
	}
	review, input := d.action("engine.qa.review.rationale.v2")
	approved := approvedReview(input.Manifest)
	d.business(review, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &approved})
	d.materialize()
	qa, input := d.action("engine.qa.execution.v4")
	report := executionFor(input)
	d.business(qa, nativeExecutionResult(input, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &report}))
	gate, input := d.action("engine.gate.v3")
	d.business(gate, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
	state = driverState(t, d)
	if !state.FinalValidationComplete(d.f.definition) || len(state.PendingHostActions) != 0 {
		t.Fatal("completed empty-path validation retained native debt")
	}
	source := *state.Validation.Candidate
	params := protocol.PromotionParameters{Source: source.Identity, SourceDigest: source.Digest, TargetRoot: d.f.root, TargetBase: state.BaseSnapshot, TargetStart: protocol.PromotionObservation{Head: state.BaseSnapshot, Tree: testkit.FrozenTree, IndexTree: testkit.FrozenTree, Clean: true}, Message: "validated delivery"}
	for id, resource := range state.NativeResources {
		if resource.Role == "candidate" && resource.Identity == source.Identity && resource.Status == "READY" {
			params.SourceWorkspace = id
		}
	}
	if err := d.f.engine.OpenPromotion(params, protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	intent, pending := driverState(t, d).PromotionIntent()
	if !pending {
		t.Fatal("completed validation did not sign its promotion")
	}
	if _, err := d.frozen.Run(d.f.root, "git", "merge", "--ff-only", source.Identity); err != nil {
		t.Fatal(err)
	}
	if err := d.git.SquashToBase(params.TargetBase, params.Message); err != nil {
		t.Fatal(err)
	}
	final, err := d.git.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	finalDigest, err := d.git.TreeDigest(final)
	if err != nil || finalDigest != source.Digest {
		t.Fatal("native promotion did not preserve its verified source tree")
	}
	proof := protocol.CandidatePromotionReceipt{OperationID: intent.ActionID, Provider: "git", SourceWorkspace: params.SourceWorkspace, Source: source.Identity, SourceDigest: source.Digest, TargetRoot: params.TargetRoot, TargetBase: params.TargetBase, TargetStart: params.TargetStart, Final: final, FinalDigest: finalDigest}
	if err := d.f.engine.RecordCandidatePromotion(intent.ActionID, proof, protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	d.reopen()
	before, err = d.f.engine.Load()
	if err != nil || !before.State.DeliveryComplete(d.f.definition) {
		t.Fatal("recovered promotion lost completed delivery qualification")
	}
	if err := d.f.engine.RecordCandidatePromotion(intent.ActionID, proof, protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	if repeated, err := d.f.PrepareCandidate(""); err != nil || repeated != final {
		t.Fatalf("completed promotion repeated its native effect: %s %v", repeated, err)
	}
	after, err = d.f.engine.Load()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("recovery or repeated promotion changed the accepted receipt or signed another action")
	}
}

// Completion reconciles approved cases, original evidence and current sources;
// the wave's COMPLETE label alone cannot authorize native delivery.
func TestAffectedCompletionRequiresCurrentApprovedEvidence(t *testing.T) {
	t.Parallel()
	d := repairedExecutionDriver(t)
	d.recordExecutionScope(coverage.ReviewBlackbox, "normal")
	action, input := d.action("engine.qa.execution.v4")
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
	params := protocol.PromotionParameters{Source: source.Identity, SourceDigest: source.Digest, TargetRoot: d.f.root, TargetBase: snapshot.State.BaseSnapshot, TargetStart: protocol.PromotionObservation{Head: snapshot.State.BaseSnapshot, Tree: testkit.FrozenTree, IndexTree: testkit.FrozenTree, Clean: true}, Message: "validated repaired candidate"}
	for id, resource := range snapshot.State.NativeResources {
		if resource.Role == "candidate" && resource.Identity == source.Identity && resource.Status == "READY" {
			params.SourceWorkspace = id
		}
	}
	if err := d.f.engine.OpenPromotion(params, protocol.Observation{}); err != nil {
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
	if err := d.f.engine.RecordCandidatePromotion(intent.ActionID, proof, protocol.Observation{}); err != nil {
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
