package facade

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"reflect"
	"testing"
)

// Actual case failures retain their own disposition when another case in the same target is waived.
func TestIndividualCaseDispositionSurvivesSiblingWaiver(t *testing.T) {
	for _, held := range []bool{false, true} {
		name := "normal"
		if held {
			name = "result-before-receipt"
		}
		t.Run(name, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox")
			d.fixtureQAResults = map[string]protocol.CheckEvidence{
				"normal":  {ExitCode: 1, StdoutText: "unexpected valid-input error"},
				"invalid": {ExitCode: 0, StdoutText: "QA harness overwrote the rejecting exit"},
			}
			d.reviewReady()
			id, in := d.action("engine.qa.execution.v2")
			report := executionFor(in)
			for i := range report.Records {
				row := &report.Records[i]
				row.Result = coverage.ExecutedFail
				if row.CaseID == "normal" {
					row.Comparison = "valid input must exit zero"
				} else {
					row.Comparison = "invalid input requires a rejecting exit status"
				}
			}
			result := nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Execution: &report})
			event := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "two-distinct-case-failures", Validation: &result}}
			if !held {
				d.receipt(id)
			}
			d.submit(event)
			if held {
				d.reopen()
				d.receipt(id)
			}
			d.drive()
			wave := d.run.Validation.Waves[0]
			raw, _ := json.Marshal(wave.Targets[in.Binding.Task.String()].Execution)
			var normalFinding string
			var resolutions []protocol.FindingResolution
			for fid, f := range d.run.Validation.Findings {
				if f.ExecutionFailure == nil {
					t.Fatal("fixture produced a severity claim")
				}
				r := executionFailureResolution(fid)
				if f.ExecutionFailure.CaseID == "normal" {
					normalFinding = fid
					r.RootCause = "valid-input dispatch returns an error"
				} else {
					r.Decision = "DISMISSED"
					r.NormalPath = "app run with documented invalid input"
					r.RootCause = "QA harness overwrites the captured exit status"
					r.Consequence = "QA observation does not describe the command result"
					r.Verification.Classification = "QA artifact"
					r.Verification.Reproduction = "direct invocation rejects invalid input; harness converts its captured exit to zero"
					r.Reason = "independent direct invocation confirms a harness defect, not a production defect"
				}
				resolutions = append(resolutions, r)
			}
			d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: resolutions}}})
			d.drive()
			problem := d.run.Validation.Findings[normalFinding].History[0].ProblemID
			if problem == "" {
				t.Fatal("valid-input defect lacks its canonical problem")
			}
			repairAction, repairInput := d.action("engine.repair.v2")
			d.chooseValidation(protocol.ValidationRequest{Kind: "VALIDATION_DETAIL_DISPOSITION", Target: wave.ID, Obligations: []string{problem}, Remedies: []string{"REPAIR", "DISMISS"}}, "DISMISS")
			d.reopen()
			d.drive()
			if d.run.Validation.Waves[0].Status == "COMPLETE" {
				t.Fatal("waiving one implementation defect silently disposed a different failed QA case")
			}
			for _, pending := range d.run.PendingActions {
				if pending.Task == repairInput.Binding.Task {
					t.Fatal("waived implementation repair remains executable")
				}
			}
			late := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: repairAction, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "late completed waived repair", Validation: &protocol.ValidationResult{Binding: repairInput.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "late repair completion", AffectedCases: []string{"normal"}}}}}
			if got := d.submit(late); got.Status != "OBSOLETE_RESULT" {
				t.Fatalf("late retired repair was not audited: %+v", got)
			}
			d.reopen()
			d.drive()
			before, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			archived := before.State.ObsoleteResults[repairAction]
			if len(archived) != 1 || !reflect.DeepEqual(archived[0].Validation, late.Result.Validation) || len(d.run.AdapterIntents) != 0 {
				t.Fatal("late repair altered validation or lost raw evidence")
			}
			d.submit(late)
			after, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("late repair replay rewrote state")
			}
			d.chooseValidation(protocol.ValidationRequest{Kind: "VALIDATION_DETAIL_DISPOSITION", Target: wave.ID, Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"invalid"}, Obligations: []string{in.Binding.Task.String()}, Remedies: []string{"QA_ARTIFACT_REPAIR", "DISMISS"}}, "DISMISS")
			d.reopen()
			d.drive()
			current := d.run.Validation.Waves[0]
			if current.Status != "COMPLETE" || !d.run.NotGuaranteed {
				t.Fatal("explicit final target waiver did not complete as not guaranteed")
			}
			executionAfter, _ := json.Marshal(current.Targets[in.Binding.Task.String()].Execution)
			if string(raw) != string(executionAfter) {
				t.Fatal("disposition rewrote actual execution evidence")
			}
			if d.run.Validation.Problems[problem].ResolvedWave != "" {
				t.Fatal("waiver pretended the implementation problem was repaired")
			}
		})
	}
}

func TestResolvedProblemDoesNotDisposeCurrentCaseFailure(t *testing.T) {
	for _, rawOpen := range []bool{false, true} {
		t.Run(fmt.Sprintf("independent-raw=%t", rawOpen), func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			d.reviewReady()
			qa, qi := d.action("engine.qa.execution.v2")
			initial := executionFor(qi)
			d.business(qa, nativeExecutionResult(qi, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &initial}))
			gate, gi := d.gateAction("quality")
			d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P2", Message: "a valid input variant is rejected by the optional whitespace parser"}}})
			d.drive()
			initialWave := d.run.Validation.Waves[0]
			for fid := range d.run.Validation.Findings {
				r := protocol.FindingResolution{FindingID: fid, Decision: "NEW", Requirement: "AC-001 normal input succeeds", NormalPath: "app run valid-input-with-trailing-space", Module: "input", RootCause: "the optional whitespace parser rejects a documented valid input variant", Consequence: "that valid input variant exits one", Severity: "P2", SeverityRationale: "limited valid input variant; the ordinary valid command remains usable", Verification: review.FindingVerification{Scope: "documented valid input", Severity: "limited variant", Causality: "parser trailing space branch", Reproduction: "ran valid input with trailing space and observed exit one", Classification: "implementation"}, Reason: "confirmed the specific valid input rejection"}
				d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: initialWave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{r}}}})
			}
			d.drive()
			repair, ri := d.action("engine.repair.v2")
			completion := protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "correct whitespace validation", AffectedCases: []string{"normal"}, CaseRuns: []protocol.RepairCaseEvidence{{Mode: coverage.ReviewBlackbox, CaseID: "normal", Result: coverage.ExecutedPass, Procedure: "run app with the representative valid input", Observation: "exit zero", Comparison: "normal input succeeds as required"}}}}
			d.business(repair, completion)
			d.verifyRepair(completion)
			d.materialize()
			d.materialize()
			var original string
			for id, p := range d.run.Validation.Problems {
				original = id
				t.Logf("prior problem=%s resolved=%s", id, p.ResolvedWave)
			}

			priorProblem, _ := json.Marshal(d.run.Validation.Problems[original])
			d.recordExecutionScope(coverage.ReviewBlackbox, "normal", "invalid")
			affected, ai := d.action("engine.qa.execution.v2")
			affectedReport := executionFor(ai)
			d.business(affected, nativeExecutionResult(ai, protocol.ValidationResult{Binding: ai.Binding, Status: "PASS", Execution: &affectedReport}))
			d.assertRequiredValidationComplete(coverage.ScopeAffected)
			d.submit(protocol.Event{Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{Control: protocol.ControlValidation, Options: []protocol.AskOption{{ID: "approve", Label: "Repeat selected QA"}}, Validation: &protocol.ValidationRequest{Kind: "QA_RETRY"}}})
			decideValidation(d, d.validationAsk("QA_RETRY"), "approve", ai.Binding.Task.String())
			d.fixtureQAResults = map[string]protocol.CheckEvidence{"normal": {ExitCode: 1, StdoutText: "unexpected whitespace validation error"}}
			d.recordExecutionScope(coverage.ReviewBlackbox, "normal", "invalid")
			d.drive()
			id, in := d.action("engine.qa.execution.v2")
			if in.Execution.Scope != coverage.ScopeAffected || len(in.RequiredCases) != 2 {
				t.Fatal("explicit QA retry lost its complete approved scope")
			}
			report := executionFor(in)
			for i := range report.Records {
				if report.Records[i].CaseID == "normal" {
					report.Records[i].Result = coverage.ExecutedFail
					report.Records[i].Comparison = "documented valid input must exit zero"
				}
			}
			result := nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Execution: &report})
			if rawOpen {
				result.Findings = []review.Finding{{Severity: "P1", Message: "normal commands corrupt the active configuration", SeverityRationale: "Normal invocation corrupts required persistent state", Requirement: "AC-001", NormalPath: "app run valid input", ActualError: "configuration becomes empty", DeferralImpact: "subsequent normal invocations cannot run", Locations: []string{"app.go:1"}}}
			}
			d.business(id, result)
			d.drive()
			wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			var extraFinding string
			for fid, f := range d.run.Validation.Findings {
				if f.WaveID != wave.ID {
					continue
				}
				r := executionFailureResolution(fid)
				if f.ExecutionFailure != nil {
					r.Decision = "SAME_AS"
					r.ProblemID = original
					r.Requirement = "AC-001 normal input succeeds"
					r.NormalPath = "app run valid-input-with-trailing-space"
					r.Module = "input"
					r.Consequence = "that valid input variant exits one"
					r.RootCause = "the optional whitespace parser rejects a documented valid input variant"
					r.Reason = "the newly executed failure has the same confirmed five dimensions"
				} else {
					extraFinding = fid
					r.Severity = "P1"
					r.SeverityRationale = "normal command corrupts persistent configuration"
					r.Verification.Severity = "ordinary invocation corrupts state"
					r.RootCause = "configuration writer truncates the active file"
					r.Consequence = "future invocations cannot load configuration"
				}
				d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{r}}}})
			}
			d.reopen()
			d.drive()
			if rawOpen {
				if extraFinding == "" {
					t.Fatal("missing accepted raw finding")
				}
				extra := d.run.Validation.Findings[extraFinding].History[0].ProblemID
				current := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
				if current.Repair == nil || !reflect.DeepEqual(current.Repair.Problems, []string{extra}) {
					t.Fatal("resolved case created a duplicate implementation repair")
				}
				d.chooseValidation(protocol.ValidationRequest{Kind: "VALIDATION_DETAIL_DISPOSITION", Target: wave.ID, Obligations: []string{extra}, Remedies: []string{"REPAIR", "DISMISS"}}, "DISMISS")
				d.reopen()
				d.drive()
			} else if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Repair != nil {
				t.Fatal("SAME_AS resolved automatically reopened repair")
			}
			current := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			if current.Status == "COMPLETE" {
				t.Fatal("resolved finding silently disposed current actual case failure")
			}
			d.chooseValidation(protocol.ValidationRequest{Kind: "VALIDATION_DETAIL_DISPOSITION", Target: wave.ID, Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal"}, Obligations: []string{in.Binding.Task.String()}, Remedies: []string{"QA_ARTIFACT_REPAIR", "DISMISS"}}, "DISMISS")
			d.reopen()
			d.drive()
			current = d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			if current.Status != "COMPLETE" || !d.run.NotGuaranteed {
				t.Fatal("explicit current target waiver failed to finish without guarantee")
			}
			if !reflect.DeepEqual(current.Targets[in.Binding.Task.String()].Execution, &report) {
				t.Fatal("disposition rewrote original actual failure")
			}
			afterProblem := *d.run.Validation.Problems[original]
			var beforeProblem protocol.ValidationProblem
			if err := json.Unmarshal(priorProblem, &beforeProblem); err != nil {
				t.Fatal(err)
			}
			// SAME_AS extends provenance; the original problem's completion remains fixed.
			if afterProblem.ResolvedWave != beforeProblem.ResolvedWave || afterProblem.ResolvedWave == "" {
				t.Fatal("SAME_AS altered canonical completion")
			}
			before, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			d.drive()
			after, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("repeated settled drive changed facts")
			}
		})
	}
}
