package facade

import (
	"bytes"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"reflect"
	"testing"
)

func TestExecutionScopeControlsDispatchAndAdmission(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		for _, checkpoint := range []bool{false, true} {
			name := map[bool]string{false: "normal", true: "reopened"}[reopen] + "-" + map[bool]string{false: "complete", true: "checkpoint"}[checkpoint]
			t.Run(name, func(t *testing.T) {
				d := newValidationDriver(t, "blackbox", "quality")
				d.reviewReady()
				qa, qi := d.action("engine.qa.execution")
				prior := qi.Binding.Candidate
				report := executionFor(qi)
				d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: "tool-v1", Environment: "environment-v1"})
				gate, gi := d.action("engine.gate")
				d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P2", Message: "invalid input prints wrong diagnostic"}}})
				wave := d.run.Validation.Waves[0]
				var finding string
				for id := range d.run.Validation.Findings {
					finding = id
				}
				resolution := protocol.FindingResolution{FindingID: finding, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run invalid", Module: "input", RootCause: "diagnostic formatter", Consequence: "wrong diagnostic", Severity: "P2", SeverityRationale: "normal command usable", Verification: review.FindingVerification{Scope: "confirmed input behavior", Severity: "limited output", Causality: "formatter", Reproduction: "app run invalid", Classification: "implementation"}, Reason: "observed actual output"}
				d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
				d.drive()
				repair, ri := d.action("engine.repair")
				d.business(repair, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "correct diagnostic", Build: []string{"full build succeeds"}, Tests: []string{"full tests succeed"}, AffectedCases: []string{"invalid"}, CaseRuns: []protocol.RepairCaseEvidence{{Mode: coverage.ReviewBlackbox, CaseID: "invalid", Result: coverage.ExecutedPass, Procedure: "app run invalid", Observation: "correct diagnostic", Comparison: "matches specified error"}}}})
				d.materialize()
				d.materialize()
				if reopen {
					d.reopen()
					d.drive()
				}

				for _, in := range d.run.ValidationInputs {
					if in.Handler == "engine.qa.execution" {
						t.Fatal("QA dispatched before host fixed its execution scope")
					}
				}
				if d.run.Next.Operator == nil {
					t.Fatal("missing scope is not exposed as an operator boundary")
				}
				wave = d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
				d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "EXECUTION_SCOPE", Mode: coverage.ReviewBlackbox, AffectedCases: []string{"invalid"}, Reason: "host checked diagnostic repair path; normal input remains unaffected"}}})
				d.drive()
				qa, qi = d.action("engine.qa.execution")
				if qi.Execution.Scope != coverage.ScopeAffected {
					t.Fatal("host scope changed the required AFFECTED wave")
				}
				raw, _ := json.Marshal(qi)
				var input map[string]json.RawMessage
				json.Unmarshal(raw, &input)
				var required []string
				json.Unmarshal(input["requiredCases"], &required)
				if len(required) != 1 || required[0] != "invalid" {
					t.Fatalf("signed input lacks exact host subset: %v", required)
				}
				before, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				beforeBytes, _ := json.Marshal(before.State)
				_, _, err = d.f.Submit(protocol.Event{ID: d.eventID(), Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "EXECUTION_SCOPE", Mode: coverage.ReviewBlackbox, AffectedCases: []string{"normal", "invalid"}, Reason: "attempt to expand after dispatch"}}})
				if err == nil {
					t.Fatal("scope was mutable after dispatch")
				}
				after, _ := d.f.engine.Load()
				afterBytes, _ := json.Marshal(after.State)
				if !bytes.Equal(beforeBytes, afterBytes) {
					t.Fatal("rejected scope change mutated persistent state")
				}
				report = scopedExecutionReport(qi, prior, false)
				d.receipt(qa)
				accepted := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: qa, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "out-of-scope-execution", Validation: &protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: "tool-v2", Environment: "environment-v2"}}})
				if accepted.Status != "REJECTED_INVALID_RESULT" {
					t.Fatalf("outside EXECUTED accepted: %s", accepted.Status)
				}
				original := qi.Binding
				d.drive()
				qa, qi = d.action("engine.qa.execution")
				if qi.Binding.Task != original.Task || qi.Binding.WaveID != original.WaveID {
					t.Fatal("invalid result changed logical scope responsibility")
				}
				d.receipt(qa)
				report = scopedExecutionReport(qi, prior, true)
				if checkpoint {
					part := report
					part.Records = nil
					part.Binding.ActualCaseIDs = []string{"invalid"}
					for _, row := range report.Records {
						if row.CaseID == "invalid" {
							part.Records = append(part.Records, row)
						}
					}
					a := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: qa, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "scope-checkpoint", Validation: &protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Checkpoint: true, Execution: &part, Tool: "tool-v2", Environment: "environment-v2"}}})
					if a.Status == "REJECTED_INVALID_RESULT" {
						t.Fatal("valid selected-case checkpoint rejected")
					}
					if reopen {
						d.reopen()
					}
					d.drive()
					qa, qi = d.action("engine.qa.execution")
					report = scopedExecutionReport(qi, prior, true)
					report.Records = nil
					report.Binding.ActualCaseIDs = []string{"normal"}
					for _, row := range scopedExecutionReport(qi, prior, true).Records {
						if row.CaseID == "normal" {
							report.Records = append(report.Records, row)
						}
					}
				}
				accepted = d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: qa, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "legal-scoped-result", Validation: &protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: "tool-v2", Environment: "environment-v2"}}})
				if accepted.Status != "ACCEPTED" {
					t.Fatalf("legal exact subset and original PASS rejected: %s", accepted.Status)
				}
				d.drive()
				qa, qi = d.action("engine.qa.execution")
				if qi.Execution.Scope != coverage.ScopeFull {
					t.Fatal("AFFECTED did not require final FULL")
				}
				report = scopedExecutionReport(qi, prior, false)
				d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: "tool-v2", Environment: "environment-v2"})
				d.drive()
				waves := d.run.Validation.Waves
				if waves[len(waves)-1].Status != "COMPLETE" {
					t.Fatal("real final FULL did not complete")
				}
				snapshot, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				originalWave := snapshot.State.Validation.Waves[1]
				for _, target := range originalWave.Targets {
					if target.Kind == "QA" && (len(target.RequiredCases) != 1 || target.RequiredCases[0] != "invalid" || target.ScopeReason == "") {
						t.Fatal("frozen host scope did not persist")
					}
				}
				if len(snapshot.State.RejectedResults) == 0 {
					t.Fatal("rejected out-of-scope evidence was lost")
				}

			})
		}
	}
}
func scopedExecutionReport(in protocol.ValidationInput, prior string, inherit bool) coverage.ExecutionReport {
	r := coverage.ExecutionReport{Binding: *in.Execution}
	seen := map[string]bool{}
	for _, edge := range in.Whitelist.Entries {
		row := coverage.ExecutionRecord{ReviewKind: edge.ReviewKind, SourceID: edge.SourceID, PointID: edge.PointID, CaseID: edge.CaseID, Result: coverage.ExecutedPass, Provenance: coverage.ProvenanceExecuted, Procedure: "run app with approved input", Observation: "captured command output and status", Comparison: "output agrees with approved oracle"}
		if inherit && row.CaseID == "normal" {
			row.Provenance = coverage.ProvenanceInherited
			row.InheritedFromCandidate = prior
			row.ReuseEvidence = "original normal input PASS outside host selected diagnostic repair scope"
			row.Procedure = ""
			row.Observation = ""
			row.Comparison = ""
		}
		r.Records = append(r.Records, row)
		if !seen[row.CaseID] {
			r.Binding.ActualCaseIDs = append(r.Binding.ActualCaseIDs, row.CaseID)
			seen[row.CaseID] = true
		}
	}
	return r
}

func (d *validationDriver) recordExecutionScope(mode coverage.ReviewKind, cases ...string) {
	d.t.Helper()
	wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "EXECUTION_SCOPE", Mode: mode, AffectedCases: cases, Reason: "host checked the repair and prior results; these cases require execution"}}})
	d.drive()
}

func repairedExecutionDriver(t *testing.T) *validationDriver {
	t.Helper()
	d, finding, wave := validationWithGateFinding(t)
	resolveGateFinding(d, finding, wave)
	id, in := d.action("engine.repair")
	d.business(id, gateRepairResult(in))
	d.materialize()
	d.materialize()
	return d
}

func rejectExecutionScope(d *validationDriver, cases ...string) {
	d.t.Helper()
	before, err := d.f.engine.Load()
	if err != nil {
		d.t.Fatal(err)
	}
	wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	_, _, err = d.f.Submit(protocol.Event{ID: d.eventID(), Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "EXECUTION_SCOPE", Mode: coverage.ReviewBlackbox, AffectedCases: cases, Reason: "host scope submission for boundary validation"}}})
	if err == nil {
		d.t.Fatal("invalid execution scope was accepted")
	}
	after, _ := d.f.engine.Load()
	a, _ := json.Marshal(before.State)
	b, _ := json.Marshal(after.State)
	if !bytes.Equal(a, b) {
		d.t.Fatal("rejected host scope mutated persistent state")
	}
}

func TestExecutionScopeFollowsItsResponsibility(t *testing.T) {
	for _, change := range []string{"route", "carry", "carry-route", "redo", "qa-retry"} {
		t.Run(change, func(t *testing.T) {
			d := repairedExecutionDriver(t)
			d.recordExecutionScope(coverage.ReviewBlackbox, "normal")
			original, input := d.action("engine.qa.execution")
			candidate := *d.run.Validation.Candidate
			source := d.run.Validation.Waves[0].Candidate.Identity
			switch change {
			case "route":
				d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
			case "carry":
				confirmValidationWork(d, "")
			case "carry-route":
				confirmValidationWork(d, "")
				d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
			case "redo":
				confirmValidationWork(d, original)
				materializeUnchangedCandidate(d, candidate)
			case "qa-retry":
				d.submit(protocol.Event{Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{Control: protocol.ControlValidation, Options: []protocol.AskOption{{ID: "approve", Label: "Repeat selected QA"}}, Validation: &protocol.ValidationRequest{Kind: "QA_RETRY", Target: d.run.Validation.Waves[len(d.run.Validation.Waves)-1].ID}}})
				decideValidation(d, d.validationAsk("QA_RETRY"), "approve", input.Binding.Task.String())
			}
			d.reopen()
			d.drive()
			if change == "redo" || change == "qa-retry" {
				for _, in := range d.run.ValidationInputs {
					if in.Handler == "engine.qa.execution" {
						t.Fatal("new semantic responsibility reused old host scope")
					}
				}
				d.recordExecutionScope(coverage.ReviewBlackbox, "normal", "invalid")
			}
			id, in := d.action("engine.qa.execution")
			if change == "route" || change == "carry" || change == "carry-route" {
				if change == "carry" && (id != original || in.Binding != input.Binding) || len(in.RequiredCases) != 1 || in.RequiredCases[0] != "normal" {
					t.Fatal("unchanged responsibility lost its signed scope")
				}
			}
			report := executionFor(in)
			if change == "route" || change == "carry" || change == "carry-route" {
				for _, r := range report.Records {
					if r.CaseID == "invalid" && (r.Provenance != coverage.ProvenanceInherited || r.InheritedFromCandidate != source) {
						t.Fatal("original inherited evidence was lost")
					}
				}
			}
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated"})
			if change == "route" || change == "carry-route" {
				id, in = d.gateAction("security")
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
			}
			d.reopen()
			d.drive()
			id, in = d.action("engine.qa.execution")
			if in.Execution.Scope != coverage.ScopeFull {
				t.Fatal("affected responsibility bypassed final FULL")
			}
			report = executionFor(in)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated"})
			d.drive()
			if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
				t.Fatal("scope-preserving transition did not finish")
			}
		})
	}
}

func TestHostScopeRequiresEveryFailureAndCannotUseDisposedCheckpoint(t *testing.T) {
	for _, evidence := range []string{"failure", "disposed-checkpoint"} {
		t.Run(evidence, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			d.reviewReady()
			qa, qi := d.action("engine.qa.execution")
			report := executionFor(qi)
			if evidence == "failure" {
				for i := range report.Records {
					if report.Records[i].CaseID == "invalid" {
						report.Records[i].Result = coverage.ExecutedFail
					}
				}
				d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "FAIL", Execution: &report, Tool: "app-v1", Environment: "isolated", Findings: []review.Finding{{Severity: "P2", Message: "invalid input failed"}}})
			} else {
				report.Records = report.Records[:1]
				report.Binding.ActualCaseIDs = []string{report.Records[0].CaseID}
				d.receipt(qa)
				d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: qa, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "partial-execution", Validation: &protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Checkpoint: true, Execution: &report, Tool: "app-v1", Environment: "isolated"}}})
				for i := 0; i < 2; i++ {
					if i > 0 {
						qa, qi = d.action("engine.qa.execution")
						d.receipt(qa)
					}
					d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: qa, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: "TRANSIENT_ENGINE_ERROR", PayloadDigest: string(d.eventID())}})
					d.drive()
				}
				d.chooseValidation(protocol.ValidationRequest{Kind: "DISPOSE", Target: qi.Binding.Task.String()}, "")
			}
			gate, gi := d.gateAction("quality")
			findings := []review.Finding(nil)
			status := "PASS"
			if evidence == "disposed-checkpoint" {
				status = "FAIL"
				findings = []review.Finding{{Severity: "P2", Message: "normal input output order mismatch"}}
			}
			d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: status, Findings: findings})
			wave := d.run.Validation.Waves[0]
			for id, f := range d.run.Validation.Findings {
				if f.Raw != nil {
					resolveGateFinding(d, id, wave.ID)
				}
			}
			for id, f := range d.run.Validation.Findings {
				if f.ExecutionFailure != nil {
					resolution := executionFailureResolution(id)
					d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
				}
			}
			d.drive()
			id, in := d.action("engine.repair")
			d.business(id, gateRepairResult(in))
			d.materialize()
			d.materialize()
			rejectExecutionScope(d)
			rejectExecutionScope(d, "normal", "normal")
			rejectExecutionScope(d, "unknown")
			rejectExecutionScope(d, "normal")
			d.recordExecutionScope(coverage.ReviewBlackbox, "normal", "invalid")
			id, in = d.action("engine.qa.execution")
			report = executionFor(in)
			for _, row := range report.Records {
				if row.Provenance != coverage.ProvenanceExecuted {
					t.Fatal("missing authoritative evidence became inherited PASS")
				}
			}
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v2", Environment: "isolated"})
			d.drive()
			for _, in := range d.run.ValidationInputs {
				if in.Handler == "engine.gate" {
					id, gi = d.gateAction("quality")
					d.business(id, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
					break
				}
			}
			d.drive()
			id, in = d.action("engine.qa.execution")
			if in.Execution.Scope != coverage.ScopeFull {
				t.Fatal("full regression missing")
			}
			report = executionFor(in)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v2", Environment: "isolated"})
			d.drive()
			if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
				t.Fatal("full evidence did not complete")
			}
			if evidence == "disposed-checkpoint" {
				old := d.run.Validation.Waves[0].Targets[qi.Binding.Task.String()]
				if old.ResultAction != "" || old.Status != "DISPOSED" || len(old.Execution.Records) != 1 {
					t.Fatal("disposed checkpoint was rewritten as authority")
				}
			}
		})
	}
}

func TestExecutionScopeKeepsOriginalEvidenceAcrossDeliveryOrder(t *testing.T) {
	for _, held := range []bool{false, true} {
		for _, resultFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("held=%t/result-first=%t", held, resultFirst), func(t *testing.T) {
				d := repairedExecutionDriver(t)
				d.recordExecutionScope(coverage.ReviewBlackbox, "normal")
				qa, in := d.action("engine.qa.execution")
				waveID := in.Binding.WaveID
				report := executionFor(in)
				event := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: qa, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "scoped-execution", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated"}}}
				var request protocol.Event
				if held {
					request = holdRequirement(t, d)
				}
				if !resultFirst {
					d.receipt(qa)
				}
				d.submit(event)
				d.reopen()
				if resultFirst {
					d.receipt(qa)
				}
				if held {
					snap, _ := d.f.engine.Load()
					if snap.State.Validation.Waves[1].Targets[in.Binding.Task.String()].Execution != nil {
						t.Fatal("held evidence applied before barrier release")
					}
					releaseRequirement(t, d, request)
				}
				d.reopen()
				d.drive()
				snap, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				raw := snap.State.Results[qa].Validation
				if raw == nil || !reflect.DeepEqual(raw.Execution, &report) || raw.Binding != in.Binding {
					t.Fatal("delivery order changed original execution evidence")
				}
				for _, w := range snap.State.Validation.Waves {
					if w.ID == waveID {
						target := w.Targets[in.Binding.Task.String()]
						if target.ScopeReason == "" || !reflect.DeepEqual(target.RequiredCases, []string{"normal"}) {
							t.Fatal("delivery order lost frozen host scope")
						}
					}
				}
				before, _ := json.Marshal(snap.State)
				d.submit(event)
				after, _ := d.f.engine.Load()
				afterBytes, _ := json.Marshal(after.State)
				if !bytes.Equal(before, afterBytes) {
					t.Fatal("duplicate scoped result wrote state")
				}
				qa, in = d.action("engine.qa.execution")
				if in.Execution.Scope != coverage.ScopeFull {
					t.Fatal("scope completion bypassed FULL")
				}
				report = executionFor(in)
				d.business(qa, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated"})
				d.drive()
				if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
					t.Fatal("final FULL did not finish")
				}
			})
		}
	}
}

func TestHostCanFixScopeAfterRequirementCarryBeforeDispatch(t *testing.T) {
	d := repairedExecutionDriver(t)
	confirmValidationWork(d, "")
	d.reopen()
	d.drive()
	d.recordExecutionScope(coverage.ReviewBlackbox, "normal")
	id, in := d.action("engine.qa.execution")
	report := executionFor(in)
	if in.Binding.RequirementRevision != d.run.Validation.RequirementRevision || len(in.RequiredCases) != 1 || in.RequiredCases[0] != "normal" {
		t.Fatal("new dispatch lost current requirement or scope")
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated"})
	d.drive()
	id, in = d.action("engine.qa.execution")
	if in.Execution.Scope != coverage.ScopeFull {
		t.Fatal("missing final FULL")
	}
	report = executionFor(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated"})
	d.drive()
	if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
		t.Fatal("carried pending scope could not complete")
	}
}
