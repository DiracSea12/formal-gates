package facade

import (
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

func (d *validationDriver) gateAction(gate string) (string, protocol.ValidationInput) {
	d.t.Helper()
	wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	for _, target := range wave.Targets {
		if target.Gate != gate {
			continue
		}
		for id, input := range d.run.ValidationInputs {
			if input.Binding.Task == target.Task {
				return id, input
			}
		}
	}
	d.t.Fatalf("missing gate %s dispatch", gate)
	return "", protocol.ValidationInput{}
}

func (d *validationDriver) disposeGate(gate string) {
	d.t.Helper()
	var target string
	for i := 0; i < 2; i++ {
		id, input := d.gateAction(gate)
		target = input.Binding.Task.String()
		d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "INVALID"})
		d.drive()
	}
	d.chooseValidation(protocol.ValidationRequest{Kind: "DISPOSE", Target: target}, "")
}

func TestGateCarryPreservesEvidenceAndCandidateScopedDisposition(t *testing.T) {
	for _, verdict := range []string{"PASS", "FAIL"} {
		for _, original := range []string{"PASS", "DISPOSED"} {
			t.Run(verdict+"/"+original, func(t *testing.T) {
				d := newValidationDriver(t, "blackbox", "quality", "security")
				d.reviewReady()
				id, input := d.action("engine.qa.execution.v3")
				executed := executionFor(input)
				d.business(id, nativeExecutionResult(input, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &executed}))
				id, input = d.gateAction("quality")
				d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: verdict, Findings: []review.Finding{{Severity: "P2", Message: "Output ordering differs from required ordering"}}})
				if original == "PASS" {
					id, input = d.gateAction("security")
					d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
				} else {
					d.disposeGate("security")
				}
				var previous protocol.ValidationTarget
				for _, target := range d.run.Validation.Waves[0].Targets {
					if target.Gate == "security" {
						previous = *target
					}
				}
				var findingID string
				for id := range d.run.Validation.Findings {
					findingID = id
				}
				resolution := protocol.FindingResolution{FindingID: findingID, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run", Module: "output", RootCause: "ordering", Consequence: "limited output order mismatch", Severity: "P2", SeverityRationale: "input validation remains usable", Verification: review.FindingVerification{Scope: "confirmed requirement", Severity: "limited", Causality: "ordering logic", Reproduction: "app run", Classification: "implementation"}, Reason: "Verified the specific ordering defect"}
				d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: d.run.Validation.Waves[0].ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
				d.drive()
				id, input = d.action("engine.repair.v5")
				repairAction := id
				repairResult := protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "corrected ordering", AffectedCases: []string{"normal"}, CaseRuns: []protocol.RepairCaseEvidence{{Mode: coverage.ReviewBlackbox, CaseID: "normal", Result: coverage.ExecutedPass, Procedure: "ran valid input", Observation: "ordered output returned", Comparison: "all output lines match specified ordering"}}}}
				d.business(id, repairResult)
				d.verifyRepair(repairResult)
				d.materialize()
				d.materialize()
				if original == "PASS" {
					wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
					d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "GATE_IMPACT", Gate: "security", Decision: "INHERIT", Reason: "The output ordering repair leaves the independently checked security boundaries unchanged"}}})
				}
				d.recordExecutionScope(coverage.ReviewBlackbox, "normal")
				d.reopen()
				d.drive()
				wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
				if wave.Scope != coverage.ScopeAffected || wave.Candidate == d.run.Validation.Waves[0].Candidate {
					t.Fatal("fixture did not create the repaired candidate")
				}
				for _, target := range wave.Targets {
					if target.Gate == "quality" && (target.Status != "PASS" || target.ResultAction != repairAction) {
						t.Fatal("verified repaired P2 lost its completion evidence")
					}
					if target.Gate == "security" {
						if original == "PASS" && (target.Status != "PASS" || target.ResultAction != previous.ResultAction) {
							t.Fatal("genuine PASS did not retain its result identity")
						}
						if original == "DISPOSED" && target.Status != "PENDING" {
							t.Fatal("old disposition authorized the new candidate")
						}
					}
				}
				if original == "DISPOSED" {
					d.disposeGate("security")
				}
				id, input = d.action("engine.qa.execution.v3")
				executed = executionFor(input)
				d.business(id, nativeExecutionResult(input, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &executed}))
				d.drive()
				concluded := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
				if concluded.Scope != coverage.ScopeAffected || concluded.Candidate != wave.Candidate {
					t.Fatal("completed validation did not retain the affected candidate")
				}
				for _, target := range concluded.Targets {
					if target.Gate == "security" {
						if target.Status != original {
							t.Fatalf("completion changed %s to %s", original, target.Status)
						}
						if original == "DISPOSED" && target.DispositionReason == "" {
							t.Fatal("same-candidate disposition lost its reason")
						}
					}
				}
				d.assertRequiredValidationComplete(coverage.ScopeAffected)
				d.reopen()
				d.drive()
				completed := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
				if completed.Status != "COMPLETE" || completed.Repair != nil || len(d.run.Validation.Problems) != 1 {
					t.Fatal("completed repair did not close final validation")
				}
				for _, pending := range d.run.ValidationInputs {
					if pending.Handler == "engine.repair.v5" || pending.Handler == "engine.gate.v2" {
						t.Fatal("completed gate obligation was issued again")
					}
				}
				for _, target := range d.run.Validation.Waves[0].Targets {
					if target.Gate == "security" && !reflect.DeepEqual(*target, previous) {
						t.Fatal("carry rewrote original gate evidence")
					}
				}
			})
		}
	}
}
