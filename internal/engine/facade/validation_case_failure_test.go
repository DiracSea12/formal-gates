package facade

import (
	"encoding/json"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"reflect"
	"slices"
	"testing"
)

func executionFailureResolution(id string) protocol.FindingResolution {
	return protocol.FindingResolution{FindingID: id, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run with the approved input", Module: "input", RootCause: "input processing returns a result different from the approved oracle", Consequence: "documented input outcome unavailable", Verification: review.FindingVerification{Scope: "confirmed command input contract", Causality: "reproduced input processing branch", Reproduction: "ran the recorded procedure and compared actual exit and output", Classification: "implementation"}, Reason: "confirmed execution evidence against the requirement and case oracle"}
}

func TestCaseFailuresUseOriginalEvidenceAndOneAdjudicatedRepair(t *testing.T) {
	t.Parallel()
	for _, delivery := range []string{"normal", "held", "checkpoint", "native-resume"} {
		t.Run(delivery, func(t *testing.T) {
			t.Parallel()
			d := newValidationDriver(t, "blackbox", "quality")
			d.fixtureQAResults = map[string]protocol.CheckEvidence{
				"normal":  {ExitCode: 1, StdoutText: "unexpected valid-input error"},
				"invalid": {ExitCode: 0, StdoutText: "invalid input accepted"},
			}
			d.reviewReady()
			id, in := d.action("engine.qa.execution.v4")
			report := executionFor(in)
			for i := range report.Records {
				report.Records[i].Result = coverage.ExecutedFail
				report.Records[i].Comparison = "expected outcome differs from the recorded observation"
			}
			result := nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Execution: &report})
			if delivery == "checkpoint" || delivery == "native-resume" {
				d.receipt(id)
				partial := report
				partial.Records = report.Records[:1]
				partial.Binding.ActualCaseIDs = []string{partial.Records[0].CaseID}
				partResult := result
				partResult.Execution = &partial
				partResult.Checkpoint = true
				ev := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "partial-failure", Validation: &partResult}}
				d.submit(ev)
				d.submit(ev)
				d.reopen()
				d.drive()
				if len(d.run.Validation.Findings) != 0 {
					t.Fatal("checkpoint created premature repair findings")
				}
				if delivery == "native-resume" {
					start, _ := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, id, protocol.LifecycleStart)
					d.submit(start)
					plan, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, protocol.Observation{})
					if err != nil {
						t.Fatal(err)
					}
					snapshot, _ := d.f.engine.Load()
					var resume protocol.HostActionIntent
					for _, intent := range snapshot.State.PendingHostActions {
						if intent.Correlation == plan.RequestID {
							resume = intent
						}
					}
					if resume.Resume == nil || resume.Resume.Input == nil || !reflect.DeepEqual(resume.Resume.Input.Completed, partial.Records) || resume.Resume.Input.Binding != in.Binding {
						t.Fatal("resume lost original FAIL checkpoint")
					}
					d.submit(d.resumeReceipt(resume))
				}

				remaining := report
				remaining.Records = report.Records[1:]
				remaining.Binding.ActualCaseIDs = []string{remaining.Records[0].CaseID}
				result.Execution = &remaining
			} else if delivery == "normal" {
				d.receipt(id)
			}
			ev := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "complete-failure", Validation: &result}}
			accepted := d.submit(ev)
			if delivery == "held" {
				if accepted.Status != "STAGED" {
					t.Fatal(accepted)
				}
				d.reopen()
				d.receipt(id)
			} else if accepted.Status != "ACCEPTED" {
				t.Fatal(accepted)
			}
			d.submit(ev)
			d.reopen()
			d.drive()
			gate, gi := d.gateAction("quality")
			d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
			wave := d.run.Validation.Waves[0]
			raw, _ := json.Marshal(d.run.Validation.Waves[0].Targets[in.Binding.Task.String()].Execution)
			var ids []string
			for fid, f := range d.run.Validation.Findings {
				if f.Raw != nil || f.ExecutionFailure == nil || f.ExecutionFailure.ResultAction != id {
					t.Fatalf("failure lost original domain: %+v", f)
				}
				ids = append(ids, fid)
			}
			slices.Sort(ids)
			if len(ids) != 2 || wave.Status != "ADJUDICATING" || wave.Repair != nil {
				t.Fatal("each failed case must independently await Operator verification")
			}
			bad := executionFailureResolution(ids[0])
			bad.Severity = "P1"
			before, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = d.f.Submit(protocol.Event{ID: d.eventID(), Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{bad}}}})
			after, _ := d.f.engine.Load()
			if err == nil || !reflect.DeepEqual(before, after) {
				t.Fatal("invented case severity was not rejected without writes")
			}
			resolution := executionFailureResolution(ids[0])
			d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
			if d.run.Validation.Waves[0].Repair != nil {
				t.Fatal("one diagnosis swallowed the other failed case")
			}
			resolution = executionFailureResolution(ids[1])
			resolution.Decision = "SAME_AS"
			resolution.ProblemID = d.run.Validation.Findings[ids[0]].History[0].ProblemID
			d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
			d.reopen()
			d.drive()
			repair, ri := d.action("engine.repair.v7")
			if len(ri.Repair.Problems) != 1 || len(ri.Repair.ExecutionFailures) != 2 || ri.Repair.Obligation.PureP2 {
				t.Fatalf("wrong repair diagnosis: %+v", ri.Repair)
			}
			for _, failure := range ri.Repair.ExecutionFailures {
				if failure.Binding != in.Binding || len(failure.Records) != 1 {
					t.Fatal("repair omitted original binding or failed edge")
				}
				row := failure.Records[0]
				index := slices.IndexFunc(report.Records, func(r coverage.ExecutionRecord) bool { return r.CaseID == row.CaseID })
				if index < 0 || !reflect.DeepEqual(row, report.Records[index]) {
					t.Fatal("repair changed original execution contents")
				}
			}
			frozen, _ := json.Marshal(ri)
			d.reopen()
			again, _ := json.Marshal(d.run.ValidationInputs[repair])
			if string(frozen) != string(again) {
				t.Fatal("reopen changed signed repair input")
			}
			repaired := gateRepairResult(ri)
			repaired.Development.AffectedCases = []string{"normal", "invalid"}
			repaired.Development.CaseRuns = append(repaired.Development.CaseRuns, protocol.RepairCaseEvidence{Mode: coverage.ReviewBlackbox, CaseID: "invalid", Result: coverage.ExecutedPass, Procedure: "run invalid input", Observation: "input rejected", Comparison: "matches input rejection oracle"})
			d.fixtureQAResults = nil
			d.business(repair, repaired)
			d.materialize()
			d.materialize()
			d.recordExecutionScope(coverage.ReviewBlackbox, "normal", "invalid")
			for tries := 0; tries < 5; tries++ {
				d.drive()
				if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status == "COMPLETE" && d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Scope == coverage.ScopeAffected {
					break
				}
				for action, input := range d.run.ValidationInputs {
					switch input.Handler {
					case "engine.qa.execution.v4":
						r := executionFor(input)
						d.business(action, nativeExecutionResult(input, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &r}))
					case "engine.gate.v3":
						d.business(action, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
					}
				}
			}
			last := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			if last.Status != "COMPLETE" || last.Scope != coverage.ScopeAffected {
				t.Fatal("case repair failed to complete required affected validation")
			}
			retained, _ := json.Marshal(d.run.Validation.Waves[0].Targets[in.Binding.Task.String()].Execution)
			if string(raw) != string(retained) {
				t.Fatal("revalidation rewrote original failed report")
			}
		})
	}
}

func TestCompletedCandidateAcceptsNewCorrectedProblem(t *testing.T) {
	t.Parallel()
	d, finding, origin := validationWithGateFinding(t)
	resolveGateFinding(d, finding, origin)
	action, input := d.action("engine.repair.v7")
	originalTask := input.Binding.Task
	repairResult := gateRepairResult(input)
	d.business(action, repairResult)
	d.verifyRepair(repairResult)
	d.materialize()
	d.materialize()
	d.recordExecutionScope(coverage.ReviewBlackbox, "normal", "invalid")
	action, input = d.action("engine.qa.execution.v4")
	execution := executionFor(input)
	d.business(action, nativeExecutionResult(input, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &execution}))
	d.drive()
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	completed := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if !snapshot.State.FinalValidationComplete(d.f.definition) || completed.Status != "COMPLETE" || completed.Counted {
		t.Fatal("setup requires a completed pure P2 candidate")
	}
	correction := d.run.Validation.Findings[finding].History[0]
	correction.Decision = "NEW"
	correction.ProblemID = ""
	correction.Supersedes = 1
	correction.RootCause = "Independent reproduction confirms required output content is missing, separate from ordering"
	correction.Consequence = "Required output value remains unavailable on normal valid input"
	correction.Severity = "P1"
	correction.SeverityRationale = "The documented output value is absent on normal input and formatting workarounds cannot produce it"
	correction.Reason = "Reproduced a distinct missing-content defect after the ordering repair was verified"
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: origin, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{correction}}}})
	d.reopen()
	d.drive()
	_, repair := d.action("engine.repair.v7")
	if repair.Binding.Task == originalTask || repair.Binding.Candidate != completed.Candidate.Identity || repair.Repair == nil {
		t.Fatal("new problem did not follow the repaired candidate's unique duty")
	}
	current := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if !current.Counted {
		t.Fatal("actual new non-P2 obligation was silently treated as pure P2")
	}
	id := d.run.Validation.Findings[finding].History[1].ProblemID
	found := false
	for _, p := range repair.Repair.Problems {
		if p.ID == id {
			found = true
		}
	}
	if !found {
		t.Fatal("new corrected problem omitted from repair evidence")
	}
}

func TestCaseFailuresRemainModeBound(t *testing.T) {
	t.Parallel()
	d := newValidationDriver(t, "blackbox", "whitebox", "quality")
	id, in := d.action("engine.qa.design.v6")
	m := simpleManifest(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
	d.drive()
	id, in = d.action("engine.qa.review.rationale.v2")
	r := approvedReview(m)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r})
	id, in = d.action("engine.development.batch.v6")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "input validation"}})
	d.materialize()
	d.materialize()
	id, in = d.action("engine.qa.design.v6")
	m = in.Manifest
	for _, item := range []struct{ id, scenario string }{{"normal", "SUCCESS"}, {"invalid", "MISUSE"}} {
		m.Cases = append(m.Cases, coverage.AcceptanceCase{CaseID: "white-" + item.id, Scenario: item.scenario, Mode: coverage.CaseWhitebox, TestRef: "app_test.go::Test" + map[string]string{"normal": "Normal", "invalid": "Invalid"}[item.id], Oracle: "specified input outcome"})
		m.Edges = append(m.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: "white-" + item.id})
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}})
	d.materialize()
	id, in = d.action("engine.qa.review.rationale.v2")
	r = approvedReview(m)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r})
	d.drive()
	d.fixtureQAResults = map[string]protocol.CheckEvidence{
		"normal":       {ExitCode: 1, StdoutText: "unexpected valid-input error"},
		"white-normal": {ExitCode: 1, StdoutText: "{\"Action\":\"run\",\"Test\":\"TestNormal\"}\n{\"Action\":\"fail\",\"Test\":\"TestNormal\"}\n"},
	}
	d.materialize()
	actions := map[coverage.ReviewKind]string{}
	for action, input := range d.run.ValidationInputs {
		if input.Handler != "engine.qa.execution.v4" {
			continue
		}
		report := executionFor(input)
		for i := range report.Records {
			if report.Records[i].CaseID == "normal" || report.Records[i].CaseID == "white-normal" {
				report.Records[i].Result = coverage.ExecutedFail
				report.Records[i].Comparison = "expected success"
			}
		}
		actions[input.Manifest.Binding.ReviewKind] = action
		d.business(action, nativeExecutionResult(input, protocol.ValidationResult{Binding: input.Binding, Status: "FAIL", Execution: &report}))
	}
	gate, gi := d.gateAction("quality")
	d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
	d.reopen()
	d.drive()
	if len(d.run.Validation.Findings) != 2 {
		t.Fatal("failed cases were collapsed across modes")
	}
	wave := d.run.Validation.Waves[0]
	for fid, f := range d.run.Validation.Findings {
		if f.ExecutionFailure == nil || (f.ExecutionFailure.CaseID != "normal" && f.ExecutionFailure.CaseID != "white-normal") {
			t.Fatal("wrong case failure")
		}
		resolution := executionFailureResolution(fid)
		d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
	}
	d.drive()
	_, repair := d.action("engine.repair.v7")
	if len(repair.Repair.Problems) != 2 || len(repair.Repair.ExecutionFailures) != 2 {
		t.Fatal("independent mode diagnoses lost their responsibilities")
	}
	for _, failure := range repair.Repair.ExecutionFailures {
		f := d.run.Validation.Findings[failure.FindingID]
		if actions[failure.Records[0].ReviewKind] != f.ExecutionFailure.ResultAction || len(failure.Records) != 1 || failure.Binding != d.run.DispatchBindings[f.ExecutionFailure.ResultAction] {
			t.Fatal("failure evidence crossed mode or dispatch identity")
		}
	}
}

func TestCaseFailureDoesNotPromoteIndependentAdvisories(t *testing.T) {
	t.Parallel()
	d := newValidationDriver(t, "blackbox")
	d.fixtureQAResults = map[string]protocol.CheckEvidence{"invalid": {ExitCode: 0, StdoutText: "invalid input accepted"}}
	d.reviewReady()
	id, in := d.action("engine.qa.execution.v4")
	report := executionFor(in)
	report.Records[0].Result = coverage.ExecutedFail
	report.Records[0].Comparison = "actual input outcome violates requirement"
	raw := []review.Finding{{Severity: "P2", Message: "optional output formatting improvement"}, {Severity: "P3", Message: "optional internal naming cleanup"}}
	d.business(id, nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Execution: &report, Findings: raw}))
	d.drive()
	wave := d.run.Validation.Waves[0]
	var caseID string
	for fid, f := range d.run.Validation.Findings {
		r := executionFailureResolution(fid)
		if f.Raw != nil {
			r.Severity = f.Raw.Severity
			r.SeverityRationale = "independent advisory does not cause the observed failed case"
			r.Verification.Severity = "documented normal behavior remains available for this suggestion"
			r.RootCause = f.Raw.Message
		} else {
			caseID = fid
		}
		d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{r}}}})
	}
	d.reopen()
	d.drive()
	_, repair := d.action("engine.repair.v7")
	problem := d.run.Validation.Findings[caseID].History[0].ProblemID
	if len(repair.Repair.Obligation.Problems) != 1 || repair.Repair.Obligation.Problems[0] != problem {
		t.Fatal("independent advisory was promoted into mandatory repair")
	}
	for _, f := range d.run.Validation.Findings {
		if f.Raw != nil && !slices.ContainsFunc(raw, func(r review.Finding) bool { return reflect.DeepEqual(r, *f.Raw) }) {
			t.Fatal("raw advisory evidence was rewritten")
		}
	}
}
