package facade

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

func (d *validationDriver) reopen() {
	d.t.Helper()
	reopened, err := Open(d.f.root, d.run.RunID)
	if err != nil {
		d.t.Fatal(err)
	}
	d.f = reopened
	d.run, err = d.f.Show()
	if err != nil {
		d.t.Fatal(err)
	}
}

func (d *validationDriver) validationAsk(kind string) PendingAskFace {
	d.t.Helper()
	for _, ask := range d.run.PendingAsks {
		if ask.Validation != nil && ask.Validation.Kind == kind {
			return ask
		}
	}
	d.t.Fatalf("missing %s offer: %+v", kind, d.run.PendingAsks)
	return PendingAskFace{}
}

func (d *validationDriver) chooseValidation(req protocol.ValidationRequest, remedy string) protocol.Event {
	d.t.Helper()
	d.submit(protocol.Event{Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{Control: protocol.ControlValidation, Options: []protocol.AskOption{{ID: "approve", Label: "Approve bound action"}}, Validation: &req}})
	ask := d.validationAsk(req.Kind)
	event := protocol.Event{ID: d.eventID(), Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "approve", Validation: &protocol.ValidationDecision{Digest: ask.Validation.Digest, StateRevision: d.run.Revision, Reason: "Confirmed the specific QA correction or disposition", Remedy: remedy}}}
	d.submit(event)
	return event
}

// Accepted design work, semantic reviews and execution retries retain separate identities.
func TestTestOnlyRedesignKeepsIndependentTaskAndReviewIdentity(t *testing.T) {
	d := newValidationDriver(t, "whitebox", "quality")
	dev, input := d.action("engine.development.batch")
	d.business(dev, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented input rejection", Build: []string{"build succeeded"}, Tests: []string{"input rejection succeeded"}}})
	d.materialize()
	d.materialize()
	var priorManifest, priorCandidate string
	tasks := map[string]bool{}
	for semanticReview := 0; semanticReview < 3; semanticReview++ {
		id, input := d.action("engine.qa.design")
		if tasks[input.Binding.Task.String()] {
			t.Fatal("a new design obligation reused completed work")
		}
		tasks[input.Binding.Task.String()] = true
		if semanticReview == 1 {
			logicalTask := input.Binding.Task
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
			d.drive()
			id, input = d.action("engine.qa.design")
			if input.Binding.Task != logicalTask || len(d.run.Validation.Sets[coverage.ReviewWhitebox].ReviewHistory) != semanticReview {
				t.Fatal("invalid design advanced the task or semantic review generation")
			}
		}
		manifest := input.Manifest
		manifest.Cases = []coverage.AcceptanceCase{{CaseID: "accept", Mode: coverage.CaseWhitebox, Scenario: "SUCCESS", TestRef: "app_test.go#TestAccept", Oracle: "valid input succeeds"}, {CaseID: "reject", Mode: coverage.CaseWhitebox, Scenario: "MISUSE", TestRef: "app_test.go#TestReject", Oracle: "invalid input is rejected"}}
		manifest.Edges = []coverage.CoverageEdge{{SourceID: "REQ-001", PointID: "AC-001", CaseID: "accept"}, {SourceID: "REQ-001", PointID: "AC-001", CaseID: "reject"}}
		encoded, _ := json.Marshal(manifest)
		if semanticReview > 0 && string(encoded) != priorManifest {
			t.Fatal("fixture changed the manifest instead of test implementation")
		}
		priorManifest = string(encoded)
		d.receipt(id)
		event := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: fmt.Sprintf("test-implementation-%d", semanticReview), Validation: &protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}}}}
		d.submit(event)
		d.submit(event)
		event.ID = d.eventID()
		d.submit(event)
		d.materialize()
		id, input = d.action("engine.qa.review.rationale")
		if input.Binding.Candidate == "" || input.Binding.Candidate == priorCandidate {
			t.Fatal("review did not bind a new immutable test candidate")
		}
		priorCandidate = input.Binding.Candidate
		if input.Contract == nil || input.Manifest.Cases[0].TestRef != "app_test.go#TestAccept" {
			t.Fatal("whitebox review lost the test implementation reference")
		}
		result := approvedReview(manifest)
		if semanticReview < 2 {
			result.SetStatus = coverage.StatusFail
			result.CaseDecisions[0].Status = coverage.StatusFail
			result.PointDecisions[0].Status = coverage.StatusFail
			result.SourceDecisions[0].Status = coverage.StatusFail
		}
		d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: string(result.SetStatus), Review: &result})
		if semanticReview == 1 {
			d.reopen()
		}
		d.drive()
		if len(d.run.Validation.Sets[coverage.ReviewWhitebox].ReviewHistory) != semanticReview+1 {
			t.Fatal("duplicate design changed semantic review usage")
		}
	}
	if d.run.Validation.Sets[coverage.ReviewWhitebox].Status != "FROZEN" {
		t.Fatal("allowed third review did not freeze the set")
	}
}

func TestRetryOfferSurvivesIndependentProgressAndReopen(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	var task string
	for i := 0; i < 2; i++ {
		id, input := d.action("engine.development.batch")
		task = input.Binding.Task.String()
		d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
		d.drive()
	}
	old := d.validationAsk("RETRY")
	id, input := d.action("engine.qa.design")
	manifest := simpleManifest(input)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
	d.drive()
	id, input = d.action("engine.qa.review.rationale")
	reviewed := approvedReview(manifest)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &reviewed})
	d.reopen()
	d.drive()
	ask := d.validationAsk("RETRY")
	revision := d.run.Revision
	d.drive()
	if d.run.Revision != revision || d.validationAsk("RETRY").RequestID != ask.RequestID {
		t.Fatal("unchanged Drive replaced a current offer")
	}
	stale := protocol.Event{ID: d.eventID(), Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(old.RequestID), Token: old.Token, Choice: "approve", Validation: &protocol.ValidationDecision{Reason: "old offer", Digest: old.Validation.Digest, StateRevision: old.Validation.StateRevision}}}
	if _, _, err := d.f.Submit(stale); err == nil {
		t.Fatal("stale authorization was accepted")
	}
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "approve", Validation: &protocol.ValidationDecision{Reason: "one corrected execution", Digest: ask.Validation.Digest, StateRevision: d.run.Revision}}})
	d.drive()
	id, input = d.action("engine.development.batch")
	if input.Binding.Task.String() != task {
		t.Fatal("extra attempt changed the obligation")
	}
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
	d.drive()
	d.reopen()
	d.drive()
	for _, ask := range d.run.PendingAsks {
		if ask.Validation != nil && ask.Validation.Kind == "RETRY" {
			for _, option := range ask.Options {
				if option.ID == "approve" {
					t.Fatal("one extra attempt expanded again")
				}
			}
			if len(ask.Options) == 0 {
				t.Fatal("exhausted obligation lost its explicit disposition choices")
			}
		}
	}
	for key, a := range d.run.EndedAttempts {
		if key.String() == task && (a.Attempts != 3 || a.MaxAttempts != 3 || !a.RetryExhausted) {
			t.Fatalf("retry policy changed: %+v", a)
		}
	}
}

func TestDismissedFindingPreservesFailedQAEvidenceUntilDisposition(t *testing.T) {
	for _, remedy := range []string{"QA_ARTIFACT_REPAIR", "DISMISS"} {
		t.Run(remedy, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			_, designed := d.action("engine.qa.design")
			manifest := simpleManifest(designed)
			for i := range manifest.Cases {
				if manifest.Cases[i].CaseID == "invalid" {
					manifest.Cases[i].Oracle = "invalid input is rejected with exit status 1"
				}
			}
			d.reviewReadyWithManifest(manifest)
			qa, qi := d.action("engine.qa.execution")
			gate, gi := d.action("engine.gate")
			report := executionFor(qi)
			report.Records[0].Result = coverage.ExecutedFail
			report.Records[0].Procedure = "Invoked the public command with invalid input"
			report.Records[0].Observation = "Process exited 2 and rejected the invalid input"
			report.Records[0].Comparison = "Case expected exit 1, while the requirement only requires input rejection"
			failed := report.Records[0]
			d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "FAIL", Execution: &report, Tool: "app-v1", Environment: "isolated-project", Findings: []review.Finding{{Severity: "P2", Message: "Exit status differs from case oracle"}}})
			req := protocol.ValidationRequest{Kind: "VALIDATION_DETAIL_DISPOSITION", Target: qi.Binding.WaveID, Mode: coverage.ReviewBlackbox, Category: "SUCCESS_FAILURE_CRITERIA", AffectedCases: []string{failed.CaseID}, Remedies: []string{"QA_ARTIFACT_REPAIR", "DISMISS"}}
			request := func() protocol.Event {
				return protocol.Event{ID: d.eventID(), Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{Control: protocol.ControlValidation, Options: []protocol.AskOption{{ID: "approve", Label: "Choose remedy"}}, Validation: &req}}
			}
			if _, _, err := d.f.Submit(request()); err == nil {
				t.Fatal("disposition bypassed an in-flight gate")
			}
			d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
			if _, _, err := d.f.Submit(request()); err == nil {
				t.Fatal("disposition bypassed unadjudicated findings")
			}
			var findingID string
			for id, f := range d.run.Validation.Findings {
				if f.Raw != nil {
					findingID = id
				}
			}
			resolution := protocol.FindingResolution{FindingID: findingID, Decision: "DISMISSED", Requirement: "AC-001", NormalPath: "app run", Module: "input", RootCause: "incorrect case oracle", Consequence: "false QA failure", Severity: "P2", SeverityRationale: "implementation satisfies input rejection", Verification: review.FindingVerification{Scope: "confirmed requirement", Severity: "limited mismatch", Causality: "oracle requires an unspecified exit code", Reproduction: "invalid input exits 2", Classification: "QA artifact"}, Reason: "Requirement mandates rejection, not exit 1"}
			d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: qi.Binding.WaveID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
			for id, f := range d.run.Validation.Findings {
				if f.ExecutionFailure != nil {
					caseResolution := resolution
					caseResolution.FindingID = id
					caseResolution.Severity, caseResolution.SeverityRationale, caseResolution.Verification.Severity = "", "", ""
					d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: qi.Binding.WaveID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{caseResolution}}}})
				}
			}
			d.reopen()
			d.drive()
			wave := d.run.Validation.Waves[0]
			if wave.Status == "COMPLETE" || wave.Repair != nil || len(d.run.PendingActions) != 0 {
				t.Fatal("finding dismissal erased QA failure or invented production repair")
			}
			if d.run.Next.Operator == nil || !strings.Contains(fmt.Sprint(d.run.Next.Operator), "VALIDATION_DETAIL_DISPOSITION") {
				t.Fatalf("missing actionable QA disposition: %+v", d.run.Next)
			}
			event := d.chooseValidation(req, remedy)
			d.submit(event)
			d.drive()
			if remedy == "QA_ARTIFACT_REPAIR" {
				id, input := d.action("engine.qa.design")
				manifest := input.Manifest
				for i := range manifest.Cases {
					if manifest.Cases[i].CaseID == failed.CaseID {
						manifest.Cases[i].Oracle = "invalid input is rejected with any nonzero exit status"
					}
				}
				d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
				d.drive()
				id, input = d.action("engine.qa.review.rationale")
				reviewed := approvedReview(manifest)
				d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &reviewed})
				d.materialize()
				id, input = d.action("engine.qa.execution")
				rerun := executionFor(input)
				d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &rerun, Tool: "app-v1", Environment: "isolated-project"})
				id, input = d.action("engine.gate")
				d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
				d.drive()
				if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
					t.Fatal("corrected QA did not complete on its new candidate")
				}
			}
			target := d.run.Validation.Waves[0].Targets[qi.Binding.Task.String()]
			if target.Execution.Records[0] != failed {
				t.Fatal("raw failed execution was overwritten")
			}
			if remedy == "DISMISS" && (target.Status != "DISPOSED" || target.DispositionReason == "" || d.run.Validation.Waves[0].Status != "COMPLETE") {
				t.Fatal("explicit disposition lost its reason or failed to settle")
			}
		})
	}
}

func TestGateInputExcludesQAMaterialsAcrossReopen(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	d.reviewReady()
	for i := 0; i < 2; i++ {
		_, gate := d.action("engine.gate")
		_, qa := d.action("engine.qa.execution")
		if gate.Contract != nil || len(gate.Manifest.Cases) != 0 || len(gate.Manifest.SharedSteps) != 0 || gate.Execution != nil || len(gate.Completed) != 0 || gate.Whitelist != nil {
			t.Fatal("gate received QA materials")
		}
		if gate.Binding.Candidate != qa.Binding.Candidate || gate.Binding.WaveID != qa.Binding.WaveID || len(gate.Manifest.Points) == 0 {
			t.Fatal("gate lost its candidate or confirmed requirement inputs")
		}
		if qa.Contract == nil || len(qa.Contract.Manifests[0].Cases) != 2 || qa.Contract.Manifests[0].Cases[0].Steps == "" || qa.Whitelist == nil || qa.Execution == nil {
			t.Fatal("QA lost its frozen execution materials")
		}
		d.reopen()
	}
}
