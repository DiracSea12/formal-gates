package facade

import (
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"testing"
)

func TestExecutionInheritanceChecksEveryRepair(t *testing.T) {
	for _, example := range []struct {
		name     string
		affected []string
		addGate  bool
		accepted bool
	}{
		{"unaffected", []string{"invalid", "invalid"}, false, true},
		{"first repair affected", []string{"normal", "invalid"}, false, false},
		{"last repair affected", []string{"invalid", "normal"}, false, false},
		{"route addition preserves evidence", []string{"invalid", "invalid"}, true, true},
	} {
		t.Run(example.name, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			d.reviewReady()
			original := d.run.Validation.Waves[0].Candidate.Identity
			for round, affected := range example.affected {
				id, in := d.action("engine.qa.execution")
				report := executionFor(in)
				if round == 1 {
					for i := range report.Records {
						if report.Records[i].CaseID == affected {
							report.Records[i].Result = coverage.ExecutedFail
						}
					}
				}
				result := protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"}
				if round == 1 {
					result.Status = "FAIL"
					result.Findings = []review.Finding{{Severity: "P2", Message: affected + " input output mismatch"}}
				}
				d.business(id, result)
				if round == 0 || example.addGate && round == 1 {
					if round == 0 {
						id, in = d.gateAction("quality")
					} else {
						id, in = d.gateAction("security")
					}
					if round == 0 {
						d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P2", Message: affected + " input output mismatch"}}})
					} else {
						d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
					}
				}
				wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
				var finding string
				for id, f := range d.run.Validation.Findings {
					if f.WaveID == wave.ID && f.Raw != nil {
						finding = id
					}
				}
				resolution := protocol.FindingResolution{FindingID: finding, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run", Module: "input", RootCause: "output formatting", Consequence: "wrong returned output", Severity: "P2", SeverityRationale: "limited output mismatch", Verification: review.FindingVerification{Scope: "confirmed input behavior", Severity: "limited mismatch", Causality: "input formatter", Reproduction: "app run", Classification: "implementation"}, Reason: "observed wrong output"}
				d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
				if round == 1 {
					for id, f := range d.run.Validation.Findings {
						if f.WaveID == wave.ID && f.ExecutionFailure != nil {
							caseResolution := resolution
							caseResolution.FindingID, caseResolution.Decision = id, "SAME_AS"
							caseResolution.ProblemID = d.run.Validation.Findings[finding].History[0].ProblemID
							caseResolution.Severity, caseResolution.SeverityRationale, caseResolution.Verification.Severity = "", "", ""
							d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{caseResolution}}}})
						}
					}
				}
				d.drive()
				id, in = d.action("engine.repair")
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "correct affected case output", Build: []string{"full build succeeds"}, Tests: []string{"full tests succeed"}, AffectedCases: []string{affected}, CaseRuns: []protocol.RepairCaseEvidence{{Mode: coverage.ReviewBlackbox, CaseID: affected, Result: coverage.ExecutedPass, Procedure: "run app with the affected input", Observation: "specified output returned", Comparison: "exact output matches requirement"}}}})

				d.materialize()
				d.materialize()
				if round == 0 {
					d.recordExecutionScope(coverage.ReviewBlackbox, "normal", "invalid")
				} else {
					d.recordExecutionScope(coverage.ReviewBlackbox, affected)
				}
				if example.addGate && round == 0 {
					d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
					d.reopen()
					d.drive()
				}
			}
			id, in := d.action("engine.qa.execution")
			report := executionFor(in)
			for i := range report.Records {
				if report.Records[i].CaseID == "normal" {
					report.Records[i].Provenance = coverage.ProvenanceInherited
					report.Records[i].InheritedFromCandidate = original
					report.Records[i].ReuseEvidence = "use original recorded normal-input execution"
					report.Records[i].Procedure = ""
					report.Records[i].Observation = ""
					report.Records[i].Comparison = ""
				}
			}
			d.receipt(id)
			a := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "inheritance-evidence", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"}}})
			t.Logf("source=%s current=%s preceding repair affected=%v acceptance=%s", original, in.Binding.Candidate, example.affected, a.Status)
			if (a.Status == "ACCEPTED") != example.accepted {
				t.Errorf("inheritance acceptance=%s, want accepted=%v", a.Status, example.accepted)
			}
		})
	}
}
