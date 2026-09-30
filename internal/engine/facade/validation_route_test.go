package facade

import (
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"testing"
)

func validationWithGateFinding(t *testing.T) (*validationDriver, string, string) {
	t.Helper()
	d := newValidationDriver(t, "blackbox", "quality")
	d.reviewReady()
	id, in := d.action("engine.qa.execution")
	r := executionFor(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &r, Tool: "app-v1", Environment: "isolated"})
	id, in = d.gateAction("quality")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P2", Message: "valid output order differs from documented order"}}})
	d.drive()
	w := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if w.Status != "ADJUDICATING" {
		t.Fatalf("setup expected adjudication, got %s", w.Status)
	}
	for id, f := range d.run.Validation.Findings {
		if f.WaveID == w.ID {
			return d, id, w.ID
		}
	}
	t.Fatal("missing original finding")
	return nil, "", ""
}
func resolveGateFinding(d *validationDriver, finding, subject string) {
	resolution := protocol.FindingResolution{FindingID: finding, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run valid-input", Module: "output", RootCause: "output order", Consequence: "documented output order differs", Severity: "P2", SeverityRationale: "input validation remains usable; output can be reordered", Verification: review.FindingVerification{Scope: "documented output", Severity: "limited ordering defect", Causality: "output sorting", Reproduction: "app run valid-input", Classification: "implementation"}, Reason: "observed ordering mismatch"}
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: subject, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
	d.drive()
}
func gateRepairResult(in protocol.ValidationInput) protocol.ValidationResult {
	return protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "apply documented ordering", Build: []string{"full build passed"}, Tests: []string{"full suite passed"}, AffectedCases: []string{"normal"}, CaseRuns: []protocol.RepairCaseEvidence{{Mode: coverage.ReviewBlackbox, CaseID: "normal", Result: coverage.ExecutedPass, Procedure: "run app valid-input", Observation: "ordered output", Comparison: "matches documented order"}}}}
}
func TestRouteAdditionPreservesFindingOwnership(t *testing.T) {
	d, finding, oldID := validationWithGateFinding(t)
	oldDigest := d.run.Validation.Waves[0].ExpectedDigest
	d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
	d.reopen()
	d.drive()
	id, in := d.gateAction("security")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
	d.drive()
	w := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if w.Status != "ADJUDICATING" {
		t.Fatalf("original FAIL lost or unnecessary QA replay: %s", w.Status)
	}
	if len(d.run.Validation.Findings) != 1 || d.run.Validation.Findings[finding].WaveID != oldID || d.run.Validation.Waves[0].ExpectedDigest != oldDigest {
		t.Fatal("route update rewrote/duplicated original evidence")
	}
	resolveGateFinding(d, finding, w.ID)
	_, in = d.action("engine.repair")
	if in.Binding.Candidate != w.Candidate.Identity {
		t.Fatal("repair candidate changed without a repair")
	}
	if len(d.run.Validation.Problems) != 1 {
		t.Fatal("original problem was lost or duplicated")
	}
}
func TestRouteAdditionPreservesIssuedRepair(t *testing.T) {
	d, finding, oldID := validationWithGateFinding(t)
	resolveGateFinding(d, finding, oldID)
	repairID, repairInput := d.action("engine.repair")
	d.receipt(repairID)
	d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
	d.reopen()
	d.drive()
	id, in := d.action("engine.repair")
	if id != repairID || in.Binding != repairInput.Binding {
		t.Fatal("route addition replaced an already issued repair obligation")
	}
	result := gateRepairResult(repairInput)
	a := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: repairID, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "repair-result", Validation: &result}})
	if a.Status != "ACCEPTED" {
		t.Fatalf("original repair was not accepted: %s", a.Status)
	}
	d.materialize()
	d.materialize()
	d.reopen()
	d.drive()
	d.gateAction("security")
	w := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if w.Candidate.Identity == repairInput.Binding.Candidate || w.Status == "COMPLETE" {
		t.Fatal("new route was not pending against repaired candidate")
	}
	if len(d.run.Validation.Problems) != 1 {
		t.Fatal("existing repair problem was lost or duplicated")
	}
}

func TestThawDoesNotReviveRetiredRepair(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "original", true: "replacement"}[replacement], func(t *testing.T) {
			d, finding, oldID := validationWithGateFinding(t)
			resolveGateFinding(d, finding, oldID)
			oldRepair, _ := d.action("engine.repair")
			if replacement {
				confirmValidationWork(d, oldRepair)
				d.materialize()
				current, _ := d.action("engine.repair")
				if current == oldRepair {
					t.Fatal("REDO retained original dispatch")
				}
				oldRepair = current
			}
			d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal"}}, "")
			d.reopen()
			d.drive()
			for action, input := range d.run.ValidationInputs {
				if input.Handler == "engine.repair" {
					t.Errorf("THAW revived canceled repair %s as %s with task %s", oldRepair, action, input.Binding.Task.String())
				}
			}
			d.action("engine.qa.design")
		})
	}
}

func TestRouteAdditionPreservesIssuedReview(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	id, in := d.action("engine.qa.design")
	m := simpleManifest(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
	d.drive()
	reviewID, reviewInput := d.action("engine.qa.review.rationale")
	r := approvedReview(m)
	d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
	d.reopen()
	d.receipt(reviewID)
	a := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: reviewID, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "same-approved-cases", Validation: &protocol.ValidationResult{Binding: reviewInput.Binding, Status: "PASS", Review: &r}}})
	if a.Status != "ACCEPTED" {
		t.Fatalf("ordinary gate addition rejected unchanged in-flight QA review: %s", a.Status)
	}
	id, in = d.action("engine.development.batch")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "validate inputs", Build: []string{"build complete"}, Tests: []string{"valid and invalid input checks complete"}}})
	d.materialize()
	d.materialize()
	d.drive()
	d.gateAction("security")
}

func TestRepeatedRouteKeepsFrozenObligation(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	d.reviewReady()
	original := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	for i := 0; i < 2; i++ {
		d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality"}))
		d.reopen()
		d.drive()
	}
	current := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if len(d.run.Validation.Waves) != 1 || current.ID != original.ID || current.ExpectedDigest != original.ExpectedDigest {
		t.Fatal("repeating the identical decision changed frozen obligation")
	}
	id, in := d.action("engine.qa.execution")
	report := executionFor(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated"})
	id, in = d.gateAction("quality")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
	d.drive()
	if d.run.Validation.Waves[0].Status != "COMPLETE" {
		t.Fatal("identical decision prevented normal completion")
	}
}

func TestAddedGateRebuildsCollectingValidation(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	d.reviewReady()
	d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
	d.reopen()
	d.drive()
	id, in := d.action("engine.qa.execution")
	r := executionFor(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &r, Tool: "app-v1", Environment: "isolated"})
	id, in = d.gateAction("quality")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
	d.drive()
	w := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	gates := []string{}
	for _, target := range w.Targets {
		if target.Gate != "" {
			gates = append(gates, target.Gate)
		}
	}
	t.Logf("confirmed selected gates=%v; frozen targets=%v; wave=%s next=%s", d.run.Review.SelectedGates, gates, w.Status, d.run.Next.Kind)
	if w.Status == "COMPLETE" {
		t.Fatal("validation completed without running newly confirmed security gate")
	}
}

func TestRouteAdditionPreservesIssuedDesign(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	id, in := d.action("engine.qa.design")
	manifest := simpleManifest(in)
	d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
	d.reopen()
	currentID, current := d.action("engine.qa.design")
	if currentID != id || current.Binding != in.Binding || current.Manifest.Binding != in.Manifest.Binding {
		t.Fatal("route addition replaced the issued design context")
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest})
	d.drive()
	id, in = d.action("engine.qa.review.rationale")
	if in.Manifest.Binding.RouteScope == manifest.Binding.RouteScope {
		t.Fatal("accepted design was not projected to the new route")
	}
	r := approvedReview(in.Manifest)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r})
	if d.run.Validation.Sets[coverage.ReviewBlackbox].Status != "FROZEN" {
		t.Fatal("unchanged cases failed to freeze")
	}
}

func TestRouteAdditionReopensCompletedValidation(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	d.reviewReady()
	id, in := d.action("engine.qa.execution")
	report := executionFor(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated"})
	id, in = d.gateAction("quality")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
	d.drive()
	original := d.run.Validation.Waves[0]
	if original.Status != "COMPLETE" || !original.Counted {
		t.Fatal("setup did not complete")
	}
	d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
	d.reopen()
	d.drive()
	current := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if current.Status != "COLLECTING" || current.ID == original.ID || d.run.Validation.Waves[0].ExpectedDigest != original.ExpectedDigest || !d.run.Validation.Waves[0].Counted {
		t.Fatal("completed wave was mutated or extra gate was omitted")
	}
	id, in = d.gateAction("security")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
	d.drive()
	current = d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if current.Status != "COMPLETE" || current.Counted {
		t.Fatal("route completion lost prior evidence or counted same candidate twice")
	}
}

func TestRouteChangeRetiresStagedAndLateValidationResults(t *testing.T) {
	for _, delivery := range []string{"staged", "receipted", "terminal"} {
		t.Run(delivery, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			d.reviewReady()
			id, in := d.action("engine.qa.execution")
			report := executionFor(in)
			result := protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "qa-evidence", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated"}}}
			if delivery == "staged" {
				d.submit(result)
			} else {
				d.receipt(id)
				if delivery == "terminal" {
					attempt := d.run.PendingActions[id].AttemptID
					for _, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
						e, err := protocol.NewTaskEvent(d.eventID(), in.Binding.Task, attempt, status)
						if err != nil {
							t.Fatal(err)
						}
						d.submit(e)
					}
				}
			}
			d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
			d.reopen()
			d.drive()
			newID, newInput := d.action("engine.qa.execution")
			if newID == id || newInput.Binding.WaveID == in.Binding.WaveID || newInput.Binding.Task == in.Binding.Task {
				t.Fatal("replacement reused old wave dispatch")
			}
			if delivery == "staged" {
				e, err := protocol.NewSpawnReceiptEvent(d.eventID(), id, DefaultProvider, id, protocol.SpawnStatusSpawned)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err = d.f.Submit(e); err == nil {
					t.Fatal("obsolete staged action unexpectedly accepted a spawn receipt")
				}
			} else {
				d.submit(result)
			}
			snapshot, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.State.ObsoleteResults[id]) != 1 || snapshot.State.ObsoleteResults[id][0].Validation.Binding != in.Binding {
				t.Fatal("old result was not retained solely as obsolete evidence")
			}
			if _, exists := snapshot.State.Results[id]; exists {
				t.Fatal("old result remained current")
			}
			current := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			target := current.Targets[newInput.Binding.Task.String()]
			if target.Status != "PENDING" || target.Execution != nil || current.Status != "COLLECTING" {
				t.Fatal("obsolete evidence settled current wave")
			}
			d.gateAction("security")
		})
	}
}

func TestRouteAdditionPreservesCandidateMaterialization(t *testing.T) {
	for _, stage := range []string{"QA_ARTIFACT", "REVIEW_READY"} {
		t.Run(stage, func(t *testing.T) {
			d := newValidationDriver(t, "whitebox", "quality")
			id, in := d.action("engine.development.batch")
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "validate input", Build: []string{"full build passed"}, Tests: []string{"input tests passed"}}})
			d.materialize()
			d.materialize()
			id, in = d.action("engine.qa.design")
			manifest := in.Manifest
			manifest.Cases = []coverage.AcceptanceCase{{CaseID: "normal", Mode: coverage.CaseWhitebox, Scenario: "SUCCESS", TestRef: "app_test.go::TestValid", Oracle: "valid input accepted"}, {CaseID: "invalid", Mode: coverage.CaseWhitebox, Scenario: "FAILURE", TestRef: "app_test.go::TestInvalid", Oracle: "invalid input rejected"}}
			for _, c := range manifest.Cases {
				manifest.Edges = append(manifest.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: c.CaseID})
			}
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}})
			if stage == "REVIEW_READY" {
				d.materialize()
				id, in = d.action("engine.qa.review.rationale")
				approved := approvedReview(in.Manifest)
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved})
			}
			var original protocol.HostActionIntent
			for _, intent := range d.run.AdapterIntents {
				original = intent
			}
			if original.Adapter == nil || original.Adapter.Operation != "op.candidate" || original.Adapter.Params["stage"] != stage {
				t.Fatal("fixture did not issue exact candidate stage")
			}
			d.confirmRoute(routeDecisionForGates([]string{"whitebox", "quality", "security"}))
			d.reopen()
			if d.run.AdapterIntents[original.ActionID].PayloadDigest != original.PayloadDigest {
				t.Fatal("route addition rewrote candidate intent")
			}
			d.materialize()
			if stage == "QA_ARTIFACT" {
				id, in = d.action("engine.qa.review.rationale")
				approved := approvedReview(in.Manifest)
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved})
				d.materialize()
			}
			d.gateAction("security")
			if d.run.Validation.Candidate == nil || d.run.Validation.Sets[coverage.ReviewWhitebox].Status != "FROZEN" {
				t.Fatal("unchanged candidate failed to finalize")
			}
		})
	}
}

func TestRouteAdditionPreservesRepairRetryBudget(t *testing.T) {
	d, finding, waveID := validationWithGateFinding(t)
	resolveGateFinding(d, finding, waveID)
	id, in := d.action("engine.repair")
	task := in.Binding.Task
	d.receipt(id)
	d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: authoring.FailureTransientEngine, PayloadDigest: "worker-interrupted"}})
	d.drive()
	id, retry := d.action("engine.repair")
	before := d.run.Attempts[task]
	if before.Attempts != 1 || before.MaxAttempts != 2 || retry.Binding.Task != task {
		t.Fatal("setup did not retain first failure")
	}
	d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
	d.reopen()
	d.drive()
	after := d.run.Attempts[task]
	if after.ID != before.ID || after.Attempts != before.Attempts || after.MaxAttempts != before.MaxAttempts {
		t.Fatal("ordinary gate addition reset repair budget")
	}
	d.business(id, gateRepairResult(retry))
	d.materialize()
	d.materialize()
	d.gateAction("security")
}

func TestRouteAdditionPreservesExplicitDisposition(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	d.reviewReady()
	id, in := d.gateAction("quality")
	d.receipt(id)
	d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: authoring.FailureTransientEngine, PayloadDigest: "gate-environment-unavailable"}})
	d.chooseValidation(protocol.ValidationRequest{Kind: "DISPOSE", Target: in.Binding.Task.String()}, "")
	old := d.run.Validation.Waves[0].Targets[in.Binding.Task.String()]
	if old.Status != "DISPOSED" || old.DispositionReason == "" {
		t.Fatal("setup did not explicitly dispose gate")
	}
	d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
	d.reopen()
	d.drive()
	current := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	for _, target := range current.Targets {
		if target.Gate == "quality" && (target.Status != "DISPOSED" || target.DispositionReason != old.DispositionReason) {
			t.Fatal("route rebind turned disposition into PASS or lost reason")
		}
	}
	d.gateAction("security")
}

func TestDevelopedRouteRejectsChangedQAOrRemovedObligations(t *testing.T) {
	changedRoutes := []protocol.RouteDecision{
		{Mode: "full", Gates: []string{"blackbox", "whitebox", "quality"}},
		{Mode: "custom", Gates: []string{"blackbox", "whitebox", "security"}, Selected: []string{"blackbox", "security"}},
	}
	for _, route := range changedRoutes {
		d := newValidationDriver(t, "blackbox", "quality")
		d.reviewReady()
		requestID := d.eventID()
		request, err := protocol.NewRouteRequestEvent(requestID, route.Gates, protocol.AskOption{ID: protocol.AskOptionID(route.Mode), Label: "Select route"})
		if err != nil {
			t.Fatal(err)
		}
		d.submit(request)
		var token string
		for _, ask := range d.run.PendingAsks {
			if ask.RequestID == string(requestID) {
				token = ask.Token
			}
		}
		event, err := protocol.NewReviewDecideEvent(d.eventID(), protocol.RequestID(requestID), token, protocol.AskOptionID(route.Mode), protocol.ReviewDecisionPayload{Route: &route})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = d.f.Submit(event); err == nil {
			t.Fatalf("accepted post-development route %s %v", route.Mode, route.Gates)
		}
		d.reopen()
		if len(d.run.Validation.Waves) != 1 || len(d.run.Review.SelectedGates) != 2 {
			t.Fatal("rejected route changed validation")
		}
	}
}

func TestRouteAdditionPreservesAffectedThenFullExecution(t *testing.T) {
	d, finding, waveID := validationWithGateFinding(t)
	source := d.run.Validation.Waves[0].Candidate.Identity
	resolveGateFinding(d, finding, waveID)
	id, in := d.action("engine.repair")
	d.business(id, gateRepairResult(in))
	d.materialize()
	d.materialize()
	d.recordExecutionScope(coverage.ReviewBlackbox, "normal")
	d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
	d.reopen()
	d.drive()
	id, in = d.action("engine.qa.execution")
	if in.Execution.Scope != coverage.ScopeAffected {
		t.Fatal("ordinary gate addition changed AFFECTED into FULL")
	}
	report := executionFor(in)
	for i := range report.Records {
		row := &report.Records[i]
		if row.CaseID == "invalid" {
			row.Provenance = coverage.ProvenanceInherited
			row.InheritedFromCandidate = source
			row.ReuseEvidence = "unchanged invalid-input execution from source candidate"
			row.Procedure, row.Observation, row.Comparison = "", "", ""
		}
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated"})
	id, in = d.gateAction("security")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
	d.drive()
	_, in = d.action("engine.qa.execution")
	if in.Execution.Scope != coverage.ScopeFull || len(in.Completed) != 0 || len(in.PendingCases) != 2 {
		t.Fatal("route change let inherited evidence discharge final FULL execution")
	}
}

func TestRouteAdditionRequiresFullExecutionAfterAffected(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	d.reviewReady()
	id, in := d.action("engine.qa.execution")
	original := in.Binding.Candidate
	report := executionFor(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated"})
	id, in = d.gateAction("quality")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P1", Message: "valid input crashes", SeverityRationale: "core command fails without workaround", Requirement: "AC-001", NormalPath: "app run valid-input", ActualError: "panic instead of successful output", DeferralImpact: "normal valid input stays unavailable", Locations: []string{"app.go:1"}}}})
	d.drive()
	wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	var finding string
	for key, f := range d.run.Validation.Findings {
		if f.WaveID == wave.ID {
			finding = key
		}
	}
	resolution := protocol.FindingResolution{FindingID: finding, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run valid-input", Module: "input", RootCause: "valid input panic", Consequence: "normal command cannot complete", Severity: "P1", SeverityRationale: "core valid input fails with no workaround", Verification: review.FindingVerification{Scope: "confirmed input", Severity: "core failure", Causality: "input panic", Reproduction: "app run valid-input", Classification: "implementation"}, Reason: "observed valid input panic"}
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
	d.drive()
	id, in = d.action("engine.repair")
	d.business(id, gateRepairResult(in))
	d.materialize()
	d.materialize()
	d.recordExecutionScope(coverage.ReviewBlackbox, "normal")
	id, in = d.action("engine.qa.execution")
	if in.Execution.Scope != coverage.ScopeAffected {
		t.Fatal("expected affected repair validation")
	}
	report = executionFor(in)
	for i := range report.Records {
		if report.Records[i].CaseID == "invalid" {
			report.Records[i].Provenance = coverage.ProvenanceInherited
			report.Records[i].InheritedFromCandidate = original
			report.Records[i].ReuseEvidence = "original invalid-input execution unaffected by valid-input fix"
			report.Records[i].Procedure = ""
			report.Records[i].Observation = ""
			report.Records[i].Comparison = ""
		}
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated"})
	wave = d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if wave.Status != "COLLECTING" {
		t.Fatalf("expected pending quality gate, got %s", wave.Status)
	}
	d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
	d.reopen()
	d.drive()
	for _, gate := range []string{"quality", "security"} {
		id, in = d.gateAction(gate)
		d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
	}
	d.drive()
	wave = d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	t.Logf("final wave scope=%s status=%s candidate=%s", wave.Scope, wave.Status, wave.Candidate.Identity)
	for _, target := range wave.Targets {
		if target.Kind == "QA" && target.Execution != nil {
			for _, r := range target.Execution.Records {
				t.Logf("case=%s provenance=%s origin=%s", r.CaseID, r.Provenance, r.InheritedFromCandidate)
			}
		}
	}
	if wave.Scope == coverage.ScopeFull && wave.Status == "COMPLETE" {
		t.Fatal("route addition converted inherited AFFECTED evidence into final FULL completion without executing all cases")
	}
}
