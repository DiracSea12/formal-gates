package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"slices"
)

func failCaseReviews(d *validationDriver, count int) {
	d.t.Helper()
	for i := 0; i < count; i++ {
		id, in := qaModeAction(d, "engine.qa.design", coverage.ReviewBlackbox)
		manifest := in.Manifest
		if len(manifest.Cases) == 0 {
			manifest = simpleManifest(in)
		}
		d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest})
		id, in = qaModeAction(d, "engine.qa.review.rationale", coverage.ReviewBlackbox)
		review := approvedReview(manifest)
		review.SetStatus = coverage.StatusFail
		review.CaseDecisions[0].Status = coverage.StatusFail
		review.PointDecisions[0].Status = coverage.StatusFail
		review.SourceDecisions[0].Status = coverage.StatusFail
		d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Review: &review})
	}
	d.reopen()
	d.drive()
}

func decideValidation(d *validationDriver, ask PendingAskFace, choice string, obligations ...string) protocol.Event {
	d.t.Helper()
	ev := protocol.Event{ID: d.eventID(), Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: protocol.AskOptionID(choice), Validation: &protocol.ValidationDecision{Digest: ask.Validation.Digest, StateRevision: d.run.Revision, Reason: "User accepts these specific snapshot obligations", Obligations: obligations}}}
	d.submit(ev)
	return ev
}

func TestPrewaveWaiverBindsFixedInputsWithoutApprovingCases(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	dev, di := d.action("engine.development.batch")
	failCaseReviews(d, 3)
	ask := d.validationAsk("QA_REVIEW")
	for _, option := range ask.Options {
		if option.ID == "waive" {
			t.Fatal("waiver offered for unknown future development inputs")
		}
	}
	for _, in := range d.run.ValidationInputs {
		if in.Handler != "engine.development.batch" {
			t.Fatalf("unapproved work after third failure: %s", in.Handler)
		}
	}
	d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented command", Build: []string{"build passed"}, Tests: []string{"input checks passed"}}})
	d.materialize()
	ask = d.validationAsk("QA_REVIEW")
	setBefore := d.run.Validation.Sets[coverage.ReviewBlackbox]
	originalHistory := append([]protocol.CaseReviewEvidence(nil), setBefore.ReviewHistory...)
	event := decideValidation(d, ask, "waive")
	d.submit(event)
	d.reopen()
	d.drive()
	d.materialize()
	wave := d.run.Validation.Waves[0]
	if !d.run.NotGuaranteed {
		t.Fatal("explicit skip claimed guaranteed coverage")
	}
	set := d.run.Validation.Sets[coverage.ReviewBlackbox]
	if set.Status == "FROZEN" || set.Review != nil || !reflect.DeepEqual(set.ReviewHistory, originalHistory) {
		t.Fatal("waiver fabricated or changed review evidence")
	}
	for _, target := range wave.Targets {
		if target.Kind == "QA" && (target.Status != "AUTHORIZED_SKIP" || target.Execution != nil || target.ResultAction != "") {
			t.Fatal("unapproved cases entered execution")
		}
	}
	gate, gi := d.gateAction("quality")
	d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
	d.drive()
	if d.run.Validation.Waves[0].Status != "COMPLETE" || !d.run.NotGuaranteed {
		t.Fatal("exact waiver failed to complete as not guaranteed")
	}
	revision := d.run.Revision
	d.reopen()
	d.drive()
	if revision != d.run.Revision {
		t.Fatal("waiver replay created work")
	}
}

func TestSameCandidateQARetryPreservesUnselectedGateAndEvidence(t *testing.T) {
	for _, arrival := range []string{"pending", "staged", "completed"} {
		t.Run(arrival, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			d.reviewReady()
			qa, qi := d.action("engine.qa.execution")
			report := executionFor(qi)
			d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
			gate, gi := d.gateAction("quality")
			gateEvent := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: gate, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "original-gate-result", Validation: &protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"}}}
			if arrival == "staged" {
				d.submit(gateEvent)
			}
			if arrival == "completed" {
				d.receipt(gate)
				d.submit(gateEvent)
			}
			before := d.run.Validation.Waves[0]
			d.submit(protocol.Event{Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{Control: protocol.ControlValidation, Options: []protocol.AskOption{{ID: "approve", Label: "Repeat selected QA"}}, Validation: &protocol.ValidationRequest{Kind: "QA_RETRY"}}})
			ask := d.validationAsk("QA_RETRY")
			chosen := decideValidation(d, ask, "approve", qi.Binding.Task.String())
			d.submit(chosen)
			d.reopen()
			d.drive()
			fresh, fi := d.action("engine.qa.execution")
			if fresh == qa || fi.Binding.Task == qi.Binding.Task || fi.Binding.Candidate != qi.Binding.Candidate || len(d.run.Validation.Waves) != 2 {
				t.Fatal("same-snapshot rerun did not create exactly one authorized semantic duty")
			}
			current := d.run.Validation.Waves[1]
			if current.AuthorizationRequest != ask.RequestID || current.Candidate != before.Candidate || len(d.run.AdapterIntents) != 0 {
				t.Fatal("QA retry rematerialized candidate or lost request binding")
			}
			if arrival != "completed" {
				d.receipt(gate)
				if arrival == "pending" {
					d.submit(gateEvent)
				}
			}
			report = executionFor(fi)
			d.business(fresh, protocol.ValidationResult{Binding: fi.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
			d.reopen()
			d.drive()
			current = d.run.Validation.Waves[1]
			if current.Status != "COMPLETE" || current.Targets[gi.Binding.Task.String()].ResultAction != gate {
				t.Fatalf("rerun failed to retain original gate: %+v", current)
			}
			snap, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if snap.State.Results[gate].PayloadDigest != "original-gate-result" || !reflect.DeepEqual(snap.State.Results[qa].Validation.Execution, before.Targets[qi.Binding.Task.String()].Execution) {
				t.Fatal("rerun rewrote original evidence")
			}
		})
	}
}

func TestAbortWaitsForSignedEffectsAndPreservesTerminalEvidence(t *testing.T) {
	for _, delivery := range []string{"receipt-first", "result-first"} {
		t.Run(delivery, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			actions := make(map[string]protocol.ValidationInput)
			for id, in := range d.run.ValidationInputs {
				actions[id] = in
			}
			var staged protocol.Event
			for id, in := range actions {
				if delivery == "receipt-first" {
					d.receipt(id)
				}
				if in.Handler == "engine.development.batch" {
					staged = protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "delivered-development", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "completed development", Build: []string{"build passed"}, Tests: []string{"tests passed"}}}}}
				}
			}
			req, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Stop run"})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(req)
			token := ""
			for _, ask := range d.run.PendingAsks {
				if ask.RequestID == string(req.ID) {
					token = ask.Token
				}
			}
			decision := protocol.Event{ID: d.eventID(), Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(req.ID), Token: token, Choice: "abort"}}
			d.submit(decision)
			d.submit(decision)
			d.submit(staged)
			d.reopen()
			d.drive()
			if d.run.Status != "ABORTING" || len(d.run.ValidationInputs) != 0 || d.run.Validation.DevelopmentSnapshot != "" {
				t.Fatal("abort advanced business before effects were reconciled")
			}
			snapshot, loadErr := d.f.engine.Load()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			for action, raw := range snapshot.State.Results {
				if _, late := actions[action]; late && raw.Validation != nil && raw.Validation.Development != nil {
					t.Fatal("aborted late development entered effective Results")
				}
			}
			if delivery == "result-first" {
				for id := range actions {
					d.receipt(id)
				}
				d.drive()
			}
			intents := make(map[string]protocol.HostActionIntent)
			for id, intent := range d.run.AdapterIntents {
				intents[id] = intent
			}
			if len(intents) != len(actions) {
				t.Fatalf("termination intents=%d signed actions=%d", len(intents), len(actions))
			}
			for id, intent := range intents {
				if intent.Terminate == nil {
					t.Fatal("abort issued a business host action")
				}
				start, err := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, intent.Terminate.Identity, protocol.LifecycleStart)
				if err != nil {
					t.Fatal(err)
				}
				d.submit(start)
				event, err := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, intent.Terminate.Identity, protocol.LifecycleStop)
				if err != nil {
					t.Fatal(err)
				}
				d.submit(event)
				receipt, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), id, protocol.HostActionTerminateAgent, DefaultProvider, "termination-receipt", intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: intent.Terminate.Identity, Event: protocol.LifecycleStop})
				if err != nil {
					t.Fatal(err)
				}
				d.submit(receipt)
				d.submit(receipt)
			}
			d.drive()
			if d.run.Status != "ABORTED" {
				t.Fatalf("abort not terminal: %s", d.run.Status)
			}
			if _, err := os.Stat(filepath.Join(d.f.stateDir, "state.json")); !os.IsNotExist(err) {
				t.Fatalf("active state survived cleanup: %v", err)
			}
			bytes, err := os.ReadFile(filepath.Join(d.f.stateDir, "terminal-summary.json"))
			if err != nil {
				t.Fatal(err)
			}
			var summary TerminalSummary
			if err = json.Unmarshal(bytes, &summary); err != nil {
				t.Fatal(err)
			}
			evidence := summary.Termination
			if evidence == nil || evidence.Decisions[string(req.ID)].EventID != string(decision.ID) || len(evidence.HostActionReceipts) != len(actions) || evidence.ObsoleteResults[staged.Result.ActionID][0].PayloadDigest != "delivered-development" {
				t.Fatal("terminal cleanup lost original authority or effect evidence")
			}
			d.reopen()
			d.drive()
			again, err := os.ReadFile(filepath.Join(d.f.stateDir, "terminal-summary.json"))
			if err != nil || string(again) != string(bytes) {
				t.Fatal("terminal replay changed summary")
			}
		})
	}
}

func postwaveRepairOffer(t *testing.T) *validationDriver {
	t.Helper()
	d := newValidationDriver(t, "blackbox", "quality")
	d.reviewReady()
	for index := 0; index < 3; index++ {
		qa, qi := d.action("engine.qa.execution")
		report := executionFor(qi)
		d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
		gate, gi := d.gateAction("quality")
		findings := []review.Finding{{Severity: "P1", Message: "normal input validation accepts malformed command", SeverityRationale: "accepting malformed command violates the core input contract with no workaround", Requirement: "AC-001", NormalPath: "app run malformed-input", ActualError: "exit status zero instead of input rejection", DeferralImpact: "invalid command remains accepted", Locations: []string{"app.go:1"}}}
		if index == 2 {
			second := findings[0]
			second.Message = "valid command produces a failing exit status"
			findings = append(findings, second)
		}
		d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "FAIL", Findings: findings})
		wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
		var resolutions []protocol.FindingResolution
		for id, finding := range d.run.Validation.Findings {
			if finding.WaveID == wave.ID {
				resolutions = append(resolutions, protocol.FindingResolution{FindingID: id, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run", Module: "input", RootCause: finding.Raw.Message, Consequence: "core command contract violated", Severity: "P1", SeverityRationale: "there is no alternate command input path", Verification: review.FindingVerification{Scope: "confirmed AC-001", Severity: "core behavior without workaround", Causality: "input guard result", Reproduction: "run command and record exit", Classification: "implementation"}, Reason: "reproduced the current candidate behavior"})
			}
		}
		slices.SortFunc(resolutions, func(a, b protocol.FindingResolution) int {
			if a.FindingID < b.FindingID {
				return -1
			}
			if a.FindingID > b.FindingID {
				return 1
			}
			return 0
		})
		d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: resolutions}}})
		d.drive()
		if index < 2 {
			id, in := d.action("engine.repair")
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "repair command validation", Build: []string{"full build executed"}, Tests: []string{"full unit suite executed"}, AffectedCases: []string{"invalid"}}})
			d.materialize()
			d.materialize()
			d.recordExecutionScope(coverage.ReviewBlackbox, "invalid")
		}
	}
	d.reopen()
	d.drive()
	return d
}

func TestExactWaiverAndQARetryPreserveUnselectedProblems(t *testing.T) {
	d := postwaveRepairOffer(t)
	ask := d.validationAsk("REPAIR")
	if len(ask.Validation.Obligations) != 2 {
		t.Fatalf("offered problems=%v", ask.Validation.Obligations)
	}
	first, second := ask.Validation.Obligations[0], ask.Validation.Obligations[1]
	wave := d.run.Validation.Waves[2]
	var qa, gate *protocol.ValidationTarget
	for _, target := range wave.Targets {
		if target.Kind == "QA" {
			qa = target
		} else {
			gate = target
		}
	}
	chosen := decideValidation(d, ask, "waive", first)
	d.submit(chosen)
	d.reopen()
	d.drive()
	current := d.run.Validation.Waves[2]
	if current.Status != "REPAIR" || !reflect.DeepEqual(current.Repair.Problems, []string{second}) || current.Targets[gate.Task.String()].Status != "FAIL" {
		t.Fatal("waiver discarded another problem or changed the raw verdict")
	}
	ask = d.validationAsk("REPAIR")
	decideValidation(d, ask, "retry_qa", qa.Task.String())
	d.recordExecutionScope(coverage.ReviewBlackbox, "invalid")
	d.reopen()
	d.drive()
	id, in := d.action("engine.qa.execution")
	report := executionFor(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
	d.reopen()
	d.drive()
	current = d.run.Validation.Waves[3]
	if current.Status != "REPAIR" || !reflect.DeepEqual(current.Repair.Problems, []string{second}) || current.Targets[gate.Task.String()].ResultAction != gate.ResultAction || current.Counted {
		t.Fatalf("QA retry lost a remaining gate obligation or counted the snapshot twice: status=%s repair=%+v gateAction=%s expected=%s counted=%t", current.Status, current.Repair, current.Targets[gate.Task.String()].ResultAction, gate.ResultAction, current.Counted)
	}
	ask = d.validationAsk("REPAIR")
	if !reflect.DeepEqual(ask.Validation.Obligations, []string{second}) {
		t.Fatal("already waived problem was reoffered")
	}
	decideValidation(d, ask, "waive", second)
	d.reopen()
	d.drive()
	current = d.run.Validation.Waves[3]
	if latest := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]; latest.Scope == coverage.ScopeFull && latest.Status == "COLLECTING" {
		id, in := d.action("engine.qa.execution")
		report := executionFor(in)
		d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
		d.reopen()
		d.drive()
	}
	current = d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	var finalGate *protocol.ValidationTarget
	for _, target := range current.Targets {
		if target.Gate == "quality" {
			finalGate = target
		}
	}
	if current.Status != "COMPLETE" || !d.run.NotGuaranteed || finalGate.Status != "FAIL" {
		t.Fatalf("precise waiver state=%s notGuaranteed=%t gate=%s problems=%v", current.Status, d.run.NotGuaranteed, finalGate.Status, "retained raw failures")
	}
	snap, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.State.Results[gate.ResultAction].Validation.Findings) != 2 {
		t.Fatal("waiver or QA rerun rewrote original findings")
	}
	counted := 0
	for _, w := range d.run.Validation.Waves {
		if w.Counted {
			counted++
		}
	}
	if counted != 3 {
		t.Fatalf("same-candidate rerun changed business count: %d", counted)
	}
}

func TestUnavailableCaseReviewOffersBoundRedesignAndWaiver(t *testing.T) {
	for _, remedy := range []string{"redesign", "waive"} {
		t.Run(remedy, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			dev, di := d.action("engine.development.batch")
			d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented input checks", Build: []string{"build passed"}, Tests: []string{"tests passed"}}})
			d.materialize()
			id, in := d.action("engine.qa.design")
			manifest := simpleManifest(in)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest})
			var reviewTask string
			for attempt := 0; attempt < 2; attempt++ {
				id, in = d.action("engine.qa.review.rationale")
				reviewTask = in.Binding.Task.String()
				d.receipt(id)
				d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, PayloadDigest: string(d.eventID()), FailureClass: "TRANSIENT_ENGINE_ERROR"}})
				d.drive()
			}
			ask := d.validationAsk("RETRY")
			decision := decideValidation(d, ask, remedy)
			d.submit(decision)
			d.reopen()
			d.drive()
			if remedy == "redesign" {
				id, in = d.action("engine.qa.design")
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest})
				id, in = d.action("engine.qa.review.rationale")
				if in.Binding.Task.String() == reviewTask {
					t.Fatal("explicit new design reused exhausted review execution identity")
				}
				approved := approvedReview(in.Manifest)
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved})
			}
			d.materialize()
			if remedy == "redesign" {
				id, in = d.action("engine.qa.execution")
				report := executionFor(in)
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
			}
			id, in = d.gateAction("quality")
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
			d.reopen()
			d.drive()
			if d.run.Validation.Waves[0].Status != "COMPLETE" || d.run.NotGuaranteed != (remedy == "waive") {
				t.Fatal("unavailable review remedy did not reach its exact completion contract")
			}
		})
	}
}

func completeWaivedCandidate(d *validationDriver) {
	d.t.Helper()
	ask := d.validationAsk("REPAIR")
	decideValidation(d, ask, "waive", ask.Validation.Obligations...)
	d.reopen()
	d.drive()
	id, in := d.action("engine.qa.execution")
	report := executionFor(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
	d.reopen()
	d.drive()
	if !d.run.NotGuaranteed || d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
		d.t.Fatal("waived candidate did not complete final FULL validation")
	}
}

func TestWaiverFollowsCarriedResponsibilityWithoutCoveringNewWork(t *testing.T) {
	for _, change := range []string{"qa-redo", "gate-redo", "route-addition"} {
		t.Run(change, func(t *testing.T) {
			d := postwaveRepairOffer(t)
			completeWaivedCandidate(d)
			old := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			var qa, gate *protocol.ValidationTarget
			for _, target := range old.Targets {
				if target.Kind == "QA" {
					qa = target
				} else {
					gate = target
				}
			}
			var source *protocol.ValidationFinding
			for _, finding := range d.run.Validation.Findings {
				if finding.WaveID == d.run.DispatchBindings[gate.ResultAction].WaveID && len(finding.History) > 0 {
					source = finding
					break
				}
			}
			if change == "route-addition" {
				unlockRouteByRequirementRedo(d)
				unlockRouteByRequirementRedo(d)
				d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security"}))
				d.drive()
			} else {
				redo := qa.ResultAction
				if change == "gate-redo" {
					redo = gate.ResultAction
				}
				confirmValidationWork(d, redo)
				materializeUnchangedCandidate(d, old.Candidate)
				d.reopen()
				d.drive()
			}
			if change == "qa-redo" {
				id, in := d.action("engine.qa.execution")
				report := executionFor(in)
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
				d.reopen()
				d.drive()
				latest := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
				if latest.Status != "COMPLETE" || !d.run.NotGuaranteed {
					t.Fatal("unrelated QA redo revoked the explicitly carried gate waiver")
				}
				return
			}
			gateName := "quality"
			if change == "route-addition" {
				gateName = "security"
			}
			id, in := d.gateAction(gateName)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Findings: []review.Finding{*source.Raw}})
			latest := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			var findingID string
			for id, f := range d.run.Validation.Findings {
				if f.WaveID == latest.ID && f.Target == in.Binding.Task.String() {
					findingID = id
				}
			}
			resolution := source.History[len(source.History)-1]
			resolution.FindingID = findingID
			resolution.Decision = "SAME_AS"
			resolution.Supersedes = 0
			d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: latest.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
			d.reopen()
			d.drive()
			latest = d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			if latest.Status != "REPAIR" || !slices.Contains(latest.Repair.Problems, resolution.ProblemID) {
				t.Fatal("same root identity expanded waiver onto a new responsibility")
			}
		})
	}
}

func TestRuntimeSkipRetainsExactFailedAttemptWithoutGrantingPass(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	d.reviewReady()
	var target string
	var failures []string
	for i := 0; i < 2; i++ {
		id, in := d.gateAction("quality")
		target = in.Binding.Task.String()
		failures = append(failures, id)
		d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "INVALID"})
		d.drive()
	}
	ask := d.validationAsk("RETRY")
	decideValidation(d, ask, "waive", target)
	d.reopen()
	d.drive()
	if d.run.Validation.Waves[0].Targets[target].Status != "AUTHORIZED_SKIP" || !d.run.NotGuaranteed {
		t.Fatal("runtime skip was not recorded separately from PASS")
	}
	qa, qi := d.action("engine.qa.execution")
	report := executionFor(qi)
	d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
	d.reopen()
	d.drive()
	if d.run.Validation.Waves[0].Status != "COMPLETE" {
		t.Fatal("independent QA did not complete after exact runtime skip")
	}
	for _, id := range failures {
		if d.run.RejectedResults[id].Payload.Validation.Status != "INVALID" {
			t.Fatal("runtime skip changed invalid-result evidence")
		}
	}
}

func TestAbortReconcilesUnknownIdentitiesBeforeCleanup(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	var ids []string
	for id := range d.run.PendingActions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		receipt, err := protocol.NewSpawnReceiptEvent(d.eventID(), id, DefaultProvider, "correlation:"+id, protocol.SpawnStatusUnknown)
		if err != nil {
			t.Fatal(err)
		}
		d.submit(receipt)
	}
	request, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Stop run"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(request)
	token := ""
	for _, ask := range d.run.PendingAsks {
		if ask.RequestID == string(request.ID) {
			token = ask.Token
		}
	}
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(request.ID), Token: token, Choice: "abort"}})
	d.reopen()
	d.drive()
	if d.run.Status != "ABORTING" || len(d.run.PendingActions) != len(ids) || len(d.run.AdapterIntents) != 0 {
		t.Fatal("unknown identity was guessed or retired before reconciliation")
	}
	for _, id := range ids {
		for _, edge := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
			event, err := protocol.NewCorrelatedLifecycleEvent(d.eventID(), DefaultProvider, "correlation:"+id, "actual-agent:"+id, edge)
			if err != nil {
				t.Fatal(err)
			}
			d.submit(event)
		}
		_, _, err := d.f.engine.ReconcileUnknownReceipt(id, emptyFingerprint())
		if err != nil {
			t.Fatal(err)
		}
		d.reopen()
	}
	d.drive()
	if d.run.Status != "ABORTED" || len(d.run.Termination.LifecycleEvents) != 2*len(ids) {
		t.Fatal("unique stopped identities did not finish real abort")
	}
	for _, id := range ids {
		if d.run.Termination.SpawnReceipts[id].LifecycleIdentity != "actual-agent:"+id {
			t.Fatal("terminal evidence lost reconciled identity")
		}
	}
}

func TestAbortSettlesOriginalCandidateReceiptWithoutPromotion(t *testing.T) {
	for _, held := range []bool{false, true} {
		t.Run(fmt.Sprintf("held=%t", held), func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			dev, di := d.action("engine.development.batch")
			design, qi := d.action("engine.qa.design")
			manifest := simpleManifest(qi)
			d.business(design, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Manifest: &manifest})
			id, ri := d.action("engine.qa.review.rationale")
			approved := approvedReview(manifest)
			d.business(id, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &approved})
			d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented input checks", Build: []string{"build passed"}, Tests: []string{"tests passed"}}})
			var intent protocol.HostActionIntent
			for _, value := range d.run.AdapterIntents {
				intent = value
			}
			var params protocol.CandidateParameters
			bytes, _ := json.Marshal(intent.Adapter.Params)
			if err := json.Unmarshal(bytes, &params); err != nil {
				t.Fatal(err)
			}
			evidence := protocol.CandidateEvidence{ParametersDigest: intent.PayloadDigest, Identity: "native-" + intent.PayloadDigest, Digest: intent.PayloadDigest, Paths: params.Paths, TestOnly: true, ProductionViewEqual: true}
			evidence.GateView = gateReviewView(params)
			bytes, _ = json.Marshal(evidence)
			var values map[string]any
			_ = json.Unmarshal(bytes, &values)
			receipt, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), intent.ActionID, "op.candidate", DefaultProvider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", values)
			if err != nil {
				t.Fatal(err)
			}
			if held {
				request, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlRequestRequirementChange, protocol.AskOption{ID: "abort", Label: "Keep requirement"})
				if err != nil {
					t.Fatal(err)
				}
				d.submit(request)
				d.submit(receipt)
			}
			request, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Stop run"})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(request)
			token := ""
			for _, ask := range d.run.PendingAsks {
				if ask.RequestID == string(request.ID) {
					token = ask.Token
				}
			}
			d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(request.ID), Token: token, Choice: "abort"}})
			if !held {
				if d.run.Status != "ABORTING" || len(d.run.AdapterIntents) != 1 {
					t.Fatal("unreceipted candidate effect was discarded")
				}
				d.submit(receipt)
			}
			d.submit(receipt)
			d.reopen()
			d.drive()
			if d.run.Status != "ABORTED" || d.run.Termination.Validation.Candidate != nil || d.run.Termination.Validation.DevelopmentSnapshot != "" || len(d.run.Termination.PendingHostActions) != 0 {
				t.Fatal("abort promoted candidate or left its effect unsettled")
			}
			observed := d.run.Termination.HostActionReceipts[intent.ActionID]
			if observed.PayloadDigest != intent.PayloadDigest || observed.AdapterEvidence.Values["identity"] != evidence.Identity {
				t.Fatal("terminal summary lost original candidate receipt values")
			}
		})
	}
}

func qaModeAction(d *validationDriver, handler string, mode coverage.ReviewKind) (string, protocol.ValidationInput) {
	d.t.Helper()
	for id, in := range d.run.ValidationInputs {
		if in.Handler == handler && in.Manifest.Binding.ReviewKind == mode {
			return id, in
		}
	}
	d.t.Fatalf("missing %s for %s", handler, mode)
	return "", protocol.ValidationInput{}
}

func TestCaseSetWaiverKeepsOtherModesStrictAndExpiresOnThaw(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "whitebox", "quality")
	dev, di := d.action("engine.development.batch")
	d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented command", Build: []string{"build passed"}, Tests: []string{"tests passed"}}})
	d.materialize()
	d.materialize()
	failCaseReviews(d, 3)
	decideValidation(d, d.validationAsk("QA_REVIEW"), "waive")
	d.reopen()
	d.drive()
	if d.run.Validation.Candidate != nil || len(d.run.Validation.Waves) != 0 {
		t.Fatal("one mode waiver bypassed another mode's review")
	}
	id, in := qaModeAction(d, "engine.qa.design", coverage.ReviewWhitebox)
	manifest := in.Manifest
	manifest.Cases = []coverage.AcceptanceCase{{CaseID: "white-valid", Mode: coverage.CaseWhitebox, Scenario: "SUCCESS", TestRef: "app_test.go#TestValid", Oracle: "valid input succeeds"}, {CaseID: "white-invalid", Mode: coverage.CaseWhitebox, Scenario: "FAILURE", TestRef: "app_test.go#TestInvalid", Oracle: "invalid input is rejected"}}
	manifest.Edges = []coverage.CoverageEdge{{SourceID: "REQ-001", PointID: "AC-001", CaseID: "white-valid"}, {SourceID: "REQ-001", PointID: "AC-001", CaseID: "white-invalid"}}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}})
	d.materialize()
	id, in = qaModeAction(d, "engine.qa.review.rationale", coverage.ReviewWhitebox)
	approved := approvedReview(manifest)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved})
	d.materialize()
	id, in = qaModeAction(d, "engine.qa.execution", coverage.ReviewWhitebox)
	for _, entry := range in.Whitelist.Entries {
		if entry.ReviewKind != coverage.ReviewWhitebox {
			t.Fatal("unapproved waived cases leaked into another mode whitelist")
		}
	}
	report := executionFor(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "go-test", Environment: "isolated-project"})
	id, in = d.gateAction("quality")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
	d.drive()
	if d.run.Validation.Waves[0].Status != "COMPLETE" || !d.run.NotGuaranteed {
		t.Fatal("mixed reviewed/waived modes did not complete faithfully")
	}
	d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal"}}, "")
	d.reopen()
	d.drive()
	id, in = qaModeAction(d, "engine.qa.design", coverage.ReviewBlackbox)
	m := in.Manifest
	m.Cases[0].Preconditions += "; initialize current command input"
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
	id, in = qaModeAction(d, "engine.qa.review.rationale", coverage.ReviewBlackbox)
	if d.run.Validation.Candidate != nil {
		t.Fatal("old waiver approved a changed case-set")
	}
	approved = approvedReview(in.Manifest)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved})
	d.drive()
	if d.run.Validation.Sets[coverage.ReviewBlackbox].Status != "FROZEN" {
		t.Fatal("explicit artifact correction required a duplicate authorization")
	}
}

// A repair first issued after a QA retry keeps that actual wave and its evidence.
func TestRepairAuthorizationAfterQARetryKeepsIssuedWave(t *testing.T) {
	d := postwaveRepairOffer(t)
	ask := d.validationAsk("REPAIR")
	if len(ask.Validation.Obligations) != 2 {
		t.Fatalf("offered problems=%v", ask.Validation.Obligations)
	}
	first, second := ask.Validation.Obligations[0], ask.Validation.Obligations[1]
	wave := d.run.Validation.Waves[2]
	var qa, gate *protocol.ValidationTarget
	for _, target := range wave.Targets {
		if target.Kind == "QA" {
			qa = target
		} else {
			gate = target
		}
	}
	chosen := decideValidation(d, ask, "waive", first)
	d.submit(chosen)
	d.reopen()
	d.drive()
	current := d.run.Validation.Waves[2]
	if current.Status != "REPAIR" || !reflect.DeepEqual(current.Repair.Problems, []string{second}) || current.Targets[gate.Task.String()].Status != "FAIL" {
		t.Fatal("waiver discarded another problem or changed the raw verdict")
	}
	ask = d.validationAsk("REPAIR")
	decideValidation(d, ask, "retry_qa", qa.Task.String())
	d.recordExecutionScope(coverage.ReviewBlackbox, "invalid")
	d.reopen()
	d.drive()
	id, in := d.action("engine.qa.execution")
	report := executionFor(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
	d.reopen()
	d.drive()
	current = d.run.Validation.Waves[3]
	if current.Status != "REPAIR" || !reflect.DeepEqual(current.Repair.Problems, []string{second}) || current.Targets[gate.Task.String()].ResultAction != gate.ResultAction || current.Counted {
		t.Fatalf("QA retry lost a remaining gate obligation or counted the snapshot twice: status=%s repair=%+v gateAction=%s expected=%s counted=%t", current.Status, current.Repair, current.Targets[gate.Task.String()].ResultAction, gate.ResultAction, current.Counted)
	}
	ask = d.validationAsk("REPAIR")
	if !reflect.DeepEqual(ask.Validation.Obligations, []string{second}) {
		t.Fatal("already waived problem was reoffered")
	}
	issuedWave := current.ID
	decideValidation(d, ask, "approve")
	d.reopen()
	d.drive()
	id, in = d.action("engine.repair")
	if in.Binding.WaveID != issuedWave || in.Repair == nil || !reflect.DeepEqual(in.Repair.Obligation.Problems, []string{second}) {
		t.Fatalf("newly issued repair changed its current obligation: binding=%+v intendedWave=%s input=%+v", in.Binding, issuedWave, in.Repair)
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "repair input validation", Build: []string{"build passed"}, Tests: []string{"input checks passed"}, AffectedCases: []string{"invalid"}}})
	d.reopen()
	d.drive()
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	result, found := snapshot.State.Results[id]
	if !found || result.Validation == nil || result.Validation.Binding != in.Binding {
		t.Fatal("completed repair lost its actual signed input after recovery")
	}
}
