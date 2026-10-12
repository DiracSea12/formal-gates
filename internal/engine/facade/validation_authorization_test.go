package facade

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"slices"
)

// An unsigned repair follows its current wave; historical requests retain
// evidence without presenting a second executable authorization.
func TestRepairOfferFollowsUnissuedResponsibility(t *testing.T) {
	d := postwaveRepairOffer(t)
	ask := d.validationAsk("REPAIR")
	problems := slices.Clone(ask.Validation.Obligations)
	if len(problems) != 2 {
		t.Fatal("setup must retain both original problems")
	}
	wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	var qa *protocol.ValidationTarget
	for _, target := range wave.Targets {
		if target.Kind == "QA" {
			qa = target
		}
	}
	if qa == nil {
		t.Fatal("current QA target is missing")
	}
	choice := decideValidation(d, ask, "retry_qa", qa.Task.String())
	d.recordExecutionScope(coverage.ReviewBlackbox, "invalid")
	d.reopen()
	d.drive()
	id, input := d.action("engine.qa.execution.v3")
	d.receipt(id)
	before, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	var prior protocol.PendingAsk
	for _, pending := range before.State.PendingAsks {
		if !pending.Resolved && pending.Validation != nil && pending.Validation.Kind == "REPAIR" && pending.Validation.Target == wave.ID {
			if prior.RequestID != "" {
				t.Fatal("unsigned repair already has duplicate current offers")
			}
			prior = pending
		}
	}
	if prior.RequestID == "" {
		t.Fatal("unsigned repair has no offer while QA is pending")
	}
	report := executionFor(input)
	d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: d.host(), Outcome: protocol.OutcomePass, PayloadDigest: string(d.eventID()), Validation: &protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"}}})
	d.reopen()
	d.drive()
	current := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if current.Repair == nil || !reflect.DeepEqual(current.Repair.Problems, problems) {
		t.Fatal("QA retry discarded retained problems")
	}
	var offers []PendingAskFace
	for _, face := range d.run.PendingAsks {
		if face.Validation != nil && face.Validation.Kind == "REPAIR" {
			offers = append(offers, face)
		}
	}
	if len(offers) != 1 || offers[0].Validation.Target != current.ID || offers[0].Validation.Binding.Task != current.Repair.Task || offers[0].Token == "" || !reflect.DeepEqual(offers[0].Validation.Obligations, problems) {
		t.Fatalf("public repair offer does not identify one complete current duty: %+v", offers)
	}
	var summary struct{ Detail *json.RawMessage }
	if err := json.Unmarshal(offers[0].Validation.Summary, &summary); err != nil || summary.Detail == nil {
		t.Fatalf("current authorization lacks its repair summary: %s %v", offers[0].Validation.Summary, err)
	}
	after, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	retired := after.State.PendingAsks[prior.RequestID]
	if !retired.Resolved {
		t.Fatal("superseded request remains an active control obligation")
	}
	retired.Resolved = false
	if !reflect.DeepEqual(retired, prior) || !reflect.DeepEqual(after.State.Decisions[ask.RequestID], before.State.Decisions[ask.RequestID]) {
		t.Fatal("request retirement rewrote original request or decision evidence")
	}
	if _, fabricated := after.State.Decisions[prior.RequestID]; fabricated {
		t.Fatal("request retirement fabricated a user decision")
	}
	if _, err := d.f.Freshness(prior.RequestID); err == nil {
		t.Fatal("retired request still offers a fresh authorization token")
	} else {
		var rejection *protocol.RejectedError
		if !errors.As(err, &rejection) || rejection.Code != protocol.CodeRequestResolved {
			t.Fatalf("retired request freshness: %v", err)
		}
	}
	beforeReplay := after
	d.submit(choice)
	afterReplay, err := d.f.engine.Load()
	if err != nil || !reflect.DeepEqual(afterReplay, beforeReplay) {
		t.Fatalf("recorded QA choice replay changed responsibility: %v", err)
	}
	var readers []*Facade
	for i := 0; i < 2; i++ {
		f, err := Open(d.f.root, d.run.RunID)
		if err != nil {
			t.Fatal(err)
		}
		readers = append(readers, f)
	}
	type driveOutcome struct {
		run Run
		err error
	}
	outcomes := make(chan driveOutcome, len(readers))
	for _, f := range readers {
		go func(f *Facade) {
			run, err := f.Drive()
			outcomes <- driveOutcome{run, err}
		}(f)
	}
	for range readers {
		outcome := <-outcomes
		if outcome.err != nil || outcome.run.Revision != after.Revision || len(outcome.run.PendingAsks) != 1 || outcome.run.PendingAsks[0].RequestID != offers[0].RequestID {
			t.Fatalf("concurrent unchanged drive duplicated responsibility or authorization: %+v %v", outcome.run, outcome.err)
		}
	}
	decideValidation(d, offers[0], "approve")
	d.reopen()
	d.drive()
	id, input = d.action("engine.repair.v5")
	if input.Binding.WaveID != current.ID || input.Repair == nil || !reflect.DeepEqual(input.Repair.Obligation.Problems, problems) {
		t.Fatal("approved repair lost its actual wave or retained problem input")
	}
	d.business(id, gateRepairResult(input))
	d.reopen()
	d.drive()
	completed, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	result, found := completed.State.Results[id]
	if !found || result.Validation == nil || result.Validation.Binding != input.Binding || !completed.State.PendingAsks[prior.RequestID].Resolved {
		t.Fatal("repair completion or reopen lost actual input or retired request evidence")
	}
}

func failCaseReviews(d *validationDriver, count int) {
	d.t.Helper()
	for i := 0; i < count; i++ {
		id, in := qaModeAction(d, "engine.qa.design.v5", coverage.ReviewBlackbox)
		manifest := in.Manifest
		if len(manifest.Cases) == 0 {
			manifest = simpleManifest(in)
		}
		manifest.Cases = slices.Clone(manifest.Cases)
		failedCase := 0
		reason := "The normal case has no preparation that provides executable ./app, so its declared command cannot be run from the clean project."
		switch i {
		case 1:
			manifest.Cases[0].Preconditions = "clean project; build the current command as executable ./app"
			reason = "The normal case procedure does not record the exit status, so its zero-exit oracle has no observable execution evidence."
		case 2:
			manifest.Cases[0].Steps = "execute ./app run valid; record exit status, stdout and stderr separately"
			failedCase = 1
			reason = "The invalid case oracle does not specify a nonzero exit status, so successful acceptance of invalid input could satisfy its ambiguous rejection check."
		}
		d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest})
		id, in = qaModeAction(d, "engine.qa.review.rationale.v2", coverage.ReviewBlackbox)
		review := approvedReview(in.Manifest)
		review.SetStatus = coverage.StatusFail
		review.CaseDecisions[failedCase].Status = coverage.StatusFail
		review.CaseDecisions[failedCase].Reason = reason
		review.PointDecisions[0].Status = coverage.StatusFail
		review.PointDecisions[0].Reason = reason
		review.SourceDecisions[0].Status = coverage.StatusFail
		review.SourceDecisions[0].Reason = reason
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
	dev, di := d.action("engine.development.batch.v4")
	failCaseReviews(d, 3)
	ask := d.validationAsk("QA_REVIEW")
	for _, option := range ask.Options {
		if option.ID == "waive" {
			t.Fatal("waiver offered for unknown future development inputs")
		}
	}
	for _, in := range d.run.ValidationInputs {
		if in.Handler != "engine.development.batch.v4" {
			t.Fatalf("unapproved work after third failure: %s", in.Handler)
		}
	}
	d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented command"}})
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
			qa, qi := d.action("engine.qa.execution.v3")
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
			fresh, fi := d.action("engine.qa.execution.v3")
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
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
			actions := make(map[string]protocol.ValidationInput)
			for id, in := range d.run.ValidationInputs {
				actions[id] = in
			}
			var staged protocol.Event
			for id, in := range actions {
				if delivery == "receipt-first" {
					d.receipt(id)
				}
				if in.Handler == "engine.development.batch.v4" {
					staged = protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "delivered-development", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "completed development"}}}}
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
			terminations := 0
			if evidence != nil {
				for _, receipt := range evidence.HostActionReceipts {
					if receipt.Operation == protocol.HostActionTerminateAgent {
						terminations++
					}
				}
			}
			expected, marshalErr := json.Marshal(d.run.Termination)
			actual, actualErr := json.Marshal(evidence)
			if marshalErr != nil || actualErr != nil || string(actual) != string(expected) {
				t.Fatal("terminal artifact differs from complete returned execution evidence")
			}
			if evidence == nil || evidence.Decisions[string(req.ID)].EventID != string(decision.ID) || terminations != len(actions) || evidence.ObsoleteResults[staged.Result.ActionID][0].PayloadDigest != "delivered-development" {
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

func postwaveRepairOffer(t *testing.T, prepared ...*validationDriver) *validationDriver {
	t.Helper()
	var d *validationDriver
	if len(prepared) == 0 {
		d = newValidationDriver(t, "blackbox", "quality")
	} else {
		d = prepared[0]
	}
	d.reviewReady()
	for index := 0; index < 3; index++ {
		qa, qi := d.action("engine.qa.execution.v3")
		report := executionFor(qi)
		d.business(qa, nativeExecutionResult(qi, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report}))
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
			id, in := d.action("engine.repair.v5")
			if !d.mockNative {
				body := fmt.Sprintf("package main\nimport \"os\"\nfunc main() { if len(os.Args) < 3 || os.Args[1] != \"run\" || os.Args[2] != \"valid\" { os.Exit(%d) } }\n", index+1)
				if err := os.WriteFile(filepath.Join(in.Workspace.Path, "app.go"), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "repair command validation", AffectedCases: []string{"invalid"}}})
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
	id, in := d.action("engine.qa.execution.v3")
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
			dev, di := d.action("engine.development.batch.v4")
			d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented input checks"}})
			d.materialize()
			id, in := d.action("engine.qa.design.v5")
			manifest := simpleManifest(in)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest})
			var reviewTask string
			for attempt := 0; attempt < 2; attempt++ {
				id, in = d.action("engine.qa.review.rationale.v2")
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
				id, in = d.action("engine.qa.design.v5")
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest})
				id, in = d.action("engine.qa.review.rationale.v2")
				if in.Binding.Task.String() == reviewTask {
					t.Fatal("explicit new design reused exhausted review execution identity")
				}
				approved := approvedReview(in.Manifest)
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved})
			}
			d.materialize()
			if remedy == "redesign" {
				id, in = d.action("engine.qa.execution.v3")
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
	for id, in := range d.run.ValidationInputs {
		if in.Handler == "engine.qa.execution.v3" {
			report := executionFor(in)
			d.business(id, nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report}))
		}
	}
	d.reopen()
	d.drive()
	snapshot, err := d.f.engine.Load()
	if err != nil {
		d.t.Fatal(err)
	}
	if !snapshot.State.FinalValidationComplete(d.f.definition) || !d.run.NotGuaranteed || d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
		d.t.Fatal("waived candidate did not complete required validation")
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
				_, scopeInput := d.action("engine.validation.observation.v2")
				if len(scopeInput.Observation.Scopes) != 1 || scopeInput.Observation.Scopes[0].Mode != coverage.ReviewBlackbox {
					t.Fatal("QA redo did not issue its original blackbox scope judgment")
				}
				scope := scopeInput.Observation.Scopes[0]
				d.recordExecutionScope(scope.Mode, scope.ApprovedCases...)
				id, in := d.action("engine.qa.execution.v3")
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
	qa, qi := d.action("engine.qa.execution.v3")
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
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
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
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
			dev, di := d.action("engine.development.batch.v4")
			design, qi := d.action("engine.qa.design.v5")
			manifest := simpleManifest(qi)
			d.business(design, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Manifest: &manifest})
			id, ri := d.action("engine.qa.review.rationale.v2")
			approved := approvedReview(manifest)
			d.business(id, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &approved})
			d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented input checks"}})
			var intent protocol.HostActionIntent
			for _, value := range d.run.AdapterIntents {
				if value.Adapter != nil && value.Adapter.Operation == "op.candidate" {
					intent = value
				}
			}
			if intent.Adapter == nil {
				t.Fatal("development has no original candidate effect")
			}
			var params protocol.CandidateParameters
			bytes, _ := json.Marshal(intent.Adapter.Params)
			if err := json.Unmarshal(bytes, &params); err != nil {
				t.Fatal(err)
			}
			if params.Stage != "CHECK_SOURCE" {
				t.Fatal("original edit source was not frozen before its programmed checks")
			}
			state := driverState(t, d)
			candidateFacts, complete, err := d.f.nativeEvidence(state, intent, true)
			if err != nil || !complete || candidateFacts.Candidate == nil {
				t.Fatalf("actual original candidate execution: %v", err)
			}
			evidence := *candidateFacts.Candidate
			values, err := candidateFacts.fields()
			if err != nil {
				t.Fatal(err)
			}
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
				if original, pending := d.run.AdapterIntents[intent.ActionID]; d.run.Status != "ABORTING" || !pending || !reflect.DeepEqual(original, intent) {
					t.Fatal("unreceipted candidate effect was discarded")
				}
				d.submit(receipt)
			}
			d.submit(receipt)
			d.completeWorkerTerminations()
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
	for i := 0; i < 2; i++ {
		for id, in := range d.run.ValidationInputs {
			if in.Handler == handler && in.Manifest.Binding.ReviewKind == mode {
				return id, in
			}
		}
		if i == 0 {
			d.drive()
		}
	}
	d.t.Fatalf("missing %s for %s", handler, mode)
	return "", protocol.ValidationInput{}
}

func TestCaseSetWaiverKeepsOtherModesStrictAndExpiresOnThaw(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "whitebox", "quality")
	dev, di := d.action("engine.development.batch.v4")
	d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented command"}})
	d.materialize()
	d.materialize()
	failCaseReviews(d, 3)
	decideValidation(d, d.validationAsk("QA_REVIEW"), "waive")
	d.reopen()
	d.drive()
	if d.run.Validation.Candidate != nil || len(d.run.Validation.Waves) != 0 {
		t.Fatal("one mode waiver bypassed another mode's review")
	}
	id, in := qaModeAction(d, "engine.qa.design.v5", coverage.ReviewWhitebox)
	manifest := in.Manifest
	manifest.Cases = []coverage.AcceptanceCase{{CaseID: "white-valid", Mode: coverage.CaseWhitebox, Scenario: "SUCCESS", TestRef: "app_test.go::TestValid", Oracle: "valid input succeeds"}, {CaseID: "white-invalid", Mode: coverage.CaseWhitebox, Scenario: "FAILURE", TestRef: "app_test.go::TestInvalid", Oracle: "invalid input is rejected"}}
	manifest.Edges = []coverage.CoverageEdge{{SourceID: "REQ-001", PointID: "AC-001", CaseID: "white-valid"}, {SourceID: "REQ-001", PointID: "AC-001", CaseID: "white-invalid"}}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}})
	d.materialize()
	id, in = qaModeAction(d, "engine.qa.review.rationale.v2", coverage.ReviewWhitebox)
	approved := approvedReview(manifest)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved})
	d.materialize()
	id, in = qaModeAction(d, "engine.qa.execution.v3", coverage.ReviewWhitebox)
	for _, entry := range in.Whitelist.Entries {
		if entry.ReviewKind != coverage.ReviewWhitebox {
			t.Fatal("unapproved waived cases leaked into another mode whitelist")
		}
	}
	report := executionFor(in)
	d.business(id, nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &report}))
	id, in = d.gateAction("quality")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
	d.drive()
	if d.run.Validation.Waves[0].Status != "COMPLETE" || !d.run.NotGuaranteed {
		t.Fatal("mixed reviewed/waived modes did not complete faithfully")
	}
	d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal"}}, "")
	d.reopen()
	d.drive()
	id, in = qaModeAction(d, "engine.qa.design.v5", coverage.ReviewBlackbox)
	m := in.Manifest
	m.Cases[0].Preconditions += "; initialize current command input"
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
	id, in = qaModeAction(d, "engine.qa.review.rationale.v2", coverage.ReviewBlackbox)
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
	id, in := d.action("engine.qa.execution.v3")
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
	id, in = d.action("engine.repair.v5")
	if in.Binding.WaveID != issuedWave || in.Repair == nil || !reflect.DeepEqual(in.Repair.Obligation.Problems, []string{second}) {
		t.Fatalf("newly issued repair changed its current obligation: binding=%+v intendedWave=%s input=%+v", in.Binding, issuedWave, in.Repair)
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "repair input validation", AffectedCases: []string{"invalid"}}})
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

func authorizationSnapshot(d *validationDriver) protocol.Snapshot {
	d.t.Helper()
	snapshot, err := d.f.engine.Load()
	if err != nil {
		d.t.Fatal(err)
	}
	return snapshot
}

func repairOfferQA(d *validationDriver, ask PendingAskFace) *protocol.ValidationTarget {
	d.t.Helper()
	for _, wave := range d.run.Validation.Waves {
		if wave.ID == ask.Validation.Binding.WaveID {
			for _, target := range wave.Targets {
				if target.Kind == "QA" {
					return target
				}
			}
		}
	}
	d.t.Fatal("repair offer must display a real source QA target")
	return nil
}

func repeatRepairQA(d *validationDriver) {
	d.t.Helper()
	ask := d.validationAsk("REPAIR")
	qa := repairOfferQA(d, ask)
	decideValidation(d, ask, "retry_qa", qa.Task.String())
	d.reopen()
	d.drive()
}

func assertResolvedAuthorization(d *validationDriver, prior protocol.PendingAsk, choice protocol.Event) {
	d.t.Helper()
	after := authorizationSnapshot(d)
	retired := after.State.PendingAsks[prior.RequestID]
	if !retired.Resolved || after.State.Decisions[prior.RequestID].EventID != string(choice.ID) || after.State.Decisions[prior.RequestID].Choice != choice.Decide.Choice {
		d.t.Fatal("choice lost its resolved request or actual decision")
	}
	retired.Resolved = prior.Resolved
	if !reflect.DeepEqual(retired, prior) || !reflect.DeepEqual(after.State.Decisions[prior.RequestID].Validation, choice.Decide.Validation) {
		d.t.Fatal("accepted choice rewrote the original request or typed decision")
	}
}

func assertAuthorizationReplay(d *validationDriver, event protocol.Event) {
	d.t.Helper()
	d.reopen()
	d.drive()
	before := authorizationSnapshot(d)
	d.submit(event)
	d.reopen()
	d.drive()
	if after := authorizationSnapshot(d); !reflect.DeepEqual(after, before) {
		d.t.Fatal("recorded choice replay or reopen changed business facts")
	}
	type outcome struct{ err error }
	results := make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		reader, err := Open(d.f.root, d.run.RunID)
		if err != nil {
			d.t.Fatal(err)
		}
		go func() {
			if d.mockNative {
				copy := *d
				copy.f = reader
				copy.drive()
				results <- outcome{}
				return
			}
			_, err := reader.Drive()
			results <- outcome{err}
		}()
	}
	for i := 0; i < 2; i++ {
		if result := <-results; result.err != nil {
			d.t.Fatal(result.err)
		}
	}
	if after := authorizationSnapshot(d); !reflect.DeepEqual(after, before) {
		d.t.Fatal("concurrent drive duplicated authorization, work or evidence")
	}
}

func TestRepairChoicesWhileFreshQAIsPending(t *testing.T) {
	for _, choice := range []string{"approve", "waive", "retry_qa"} {
		t.Run(choice, func(t *testing.T) {
			d := postwaveRepairOffer(t)
			repeatRepairQA(d)
			d.recordExecutionScope(coverage.ReviewBlackbox, "invalid")
			qaAction, qaInput := d.action("engine.qa.execution.v3")
			d.receipt(qaAction)
			d.reopen()
			d.drive()
			ask := d.validationAsk("REPAIR")
			for _, legal := range []string{"approve", "waive", "retry_qa"} {
				if !slices.ContainsFunc(ask.Options, func(option protocol.AskOption) bool { return string(option.ID) == legal }) {
					t.Fatalf("pending QA lost legal displayed choice %s", legal)
				}
			}
			if ask.Validation.Target == qaInput.Binding.WaveID || len(ask.Validation.Obligations) != 2 {
				t.Fatal("fixture must have an older owed repair and two displayed problems")
			}
			before := authorizationSnapshot(d)
			var selected []string
			if choice == "waive" {
				selected = []string{ask.Validation.Obligations[0]}
			} else if choice == "retry_qa" {
				selected = []string{repairOfferQA(d, ask).Task.String()}
			}
			event := decideValidation(d, ask, choice, selected...)
			assertResolvedAuthorization(d, before.State.PendingAsks[ask.RequestID], event)
			after := authorizationSnapshot(d)
			if !reflect.DeepEqual(before.State.Validation.Findings, after.State.Validation.Findings) || !reflect.DeepEqual(before.State.Results, after.State.Results) {
				t.Fatal("authorization changed actual findings or completed results")
			}
			if choice == "retry_qa" {
				if _, retired := after.State.ObsoleteActions[qaAction]; !retired {
					t.Fatal("retry did not retire the actual current selected QA")
				}
				d.recordExecutionScope(coverage.ReviewBlackbox, "invalid")
				fresh, input := d.action("engine.qa.execution.v3")
				if fresh == qaAction || input.Binding.Task == qaInput.Binding.Task || input.Binding.Candidate != qaInput.Binding.Candidate {
					t.Fatal("retry reused the selected execution or changed candidate")
				}
			} else {
				if !reflect.DeepEqual(before.State.DispatchBindings[qaAction], after.State.DispatchBindings[qaAction]) || !reflect.DeepEqual(before.State.SpawnReceipts[qaAction], after.State.SpawnReceipts[qaAction]) || after.State.PendingActions[qaAction].ActionID != qaAction {
					t.Fatal("repair choice cancelled or changed unselected running QA")
				}
				problems := slices.Clone(ask.Validation.Obligations)
				if choice == "waive" {
					problems = problems[1:]
					current := d.validationAsk("REPAIR")
					if !reflect.DeepEqual(current.Validation.Obligations, problems) {
						t.Fatal("partial waiver did not project the exact remaining problem")
					}
					decideValidation(d, current, "approve")
				}
				_, input := d.action("engine.repair.v5")
				if input.Binding.WaveID != ask.Validation.Binding.WaveID || !reflect.DeepEqual(input.Repair.Obligation.Problems, problems) {
					t.Fatal("actual signed repair input differs from current displayed obligations")
				}
			}
			assertAuthorizationReplay(d, event)
		})
	}
}

func TestQARetryReplacesSelectedExecution(t *testing.T) {
	for _, life := range []string{"unissued", "running", "awaiting-result", "completed"} {
		t.Run(life, func(t *testing.T) {
			d := postwaveRepairOffer(t)
			repeatRepairQA(d)
			var action string
			var input protocol.ValidationInput
			if life != "unissued" {
				d.recordExecutionScope(coverage.ReviewBlackbox, "invalid")
				action, input = d.action("engine.qa.execution.v3")
				d.receipt(action)
			}
			if life == "awaiting-result" {
				attempt := d.run.PendingActions[action].AttemptID
				for _, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
					progress, err := protocol.NewTaskEvent(d.eventID(), input.Binding.Task, attempt, status)
					if err != nil {
						t.Fatal(err)
					}
					d.submit(progress)
				}
			} else if life == "completed" {
				report := executionFor(input)
				d.business(action, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
			}
			d.reopen()
			d.drive()
			ask := d.validationAsk("REPAIR")
			before := authorizationSnapshot(d)
			projection := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			if life == "awaiting-result" && !before.State.EndedAttempts[input.Binding.Task].AwaitingResult {
				t.Fatal("fixture did not reach actual awaiting-result transport state")
			}
			if life == "unissued" {
				for _, in := range d.run.ValidationInputs {
					if in.Handler == "engine.qa.execution.v3" {
						t.Fatal("unissued QA already has an actual execution")
					}
				}
			}
			event := decideValidation(d, ask, "retry_qa", repairOfferQA(d, ask).Task.String())
			assertResolvedAuthorization(d, before.State.PendingAsks[ask.RequestID], event)
			after := authorizationSnapshot(d)
			current := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			if current.AuthorizationRequest != ask.RequestID || current.Counted || current.Candidate != projection.Candidate {
				t.Fatal("retry lost request authority or counted the same candidate again")
			}
			for key, target := range projection.Targets {
				if target.Kind != "QA" && !reflect.DeepEqual(current.Targets[key], target) {
					t.Fatal("retry replaced unselected current gate evidence")
				}
			}
			if action != "" {
				if !reflect.DeepEqual(before.State.SpawnReceipts[action], after.State.SpawnReceipts[action]) || !reflect.DeepEqual(before.State.DispatchBindings[action], after.State.DispatchBindings[action]) || before.State.DispatchIssueRevisions[action] != after.State.DispatchIssueRevisions[action] {
					t.Fatal("replacement rewrote actual dispatch identity or receipt")
				}
				if life == "completed" {
					if !reflect.DeepEqual(before.State.Results[action], after.State.Results[action]) {
						t.Fatal("retry erased a completed selected result")
					}
					if _, retired := after.State.ObsoleteActions[action]; retired {
						t.Fatal("completed execution was retired as still live")
					}
				} else {
					retired, found := after.State.ObsoleteActions[action]
					if !found || retired.Task != input.Binding.Task {
						t.Fatal("retry left selected actual execution active")
					}
					if life == "awaiting-result" && retired.AttemptID != before.State.EndedAttempts[input.Binding.Task].ID {
						t.Fatal("retirement lost known ended attempt identity")
					}
					if _, ended := after.State.EndedAttempts[input.Binding.Task]; ended {
						t.Fatal("selected awaiting result remains reusable")
					}
				}
			}
			d.recordExecutionScope(coverage.ReviewBlackbox, "invalid")
			fresh, freshInput := d.action("engine.qa.execution.v3")
			if fresh == action || freshInput.Binding.Task == input.Binding.Task {
				t.Fatal("fresh QA continued a replaced execution")
			}
			if action != "" {
				if life == "completed" {
					raw := before.State.Results[action]
					accepted := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: action, Provider: raw.Provider, Outcome: raw.Outcome, PayloadDigest: raw.PayloadDigest, Validation: raw.Validation}})
					if accepted.Status != "DUPLICATE" {
						t.Fatal("completed original result cannot be replayed exactly")
					}
				} else {
					report := executionFor(input)
					accepted := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: action, Provider: d.host(), Outcome: protocol.OutcomePass, PayloadDigest: "selected-qa-late-result", Validation: &protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"}}})
					late := authorizationSnapshot(d)
					if accepted.Status != "OBSOLETE_RESULT" || len(late.State.ObsoleteResults[action]) != 1 || late.State.ObsoleteResults[action][0].Validation.Binding != input.Binding || late.State.ObsoleteResults[action][0].PayloadDigest != "selected-qa-late-result" {
						t.Fatal("late selected result was not retained solely as its original obsolete fact")
					}
					if _, effective := late.State.Results[action]; effective {
						t.Fatal("obsolete QA became an effective result")
					}
				}
			}
			report := executionFor(freshInput)
			d.business(fresh, protocol.ValidationResult{Binding: freshInput.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
			assertAuthorizationReplay(d, event)
		})
	}
}

func TestWaiverKeepsFreshQAResultsInTheirOwnResponsibility(t *testing.T) {
	for _, extent := range []string{"partial", "all"} {
		for _, verdict := range []string{"PASS", "FAIL"} {
			t.Run(extent+"/"+verdict, func(t *testing.T) {
				d := postwaveRepairOffer(t)
				if verdict == "FAIL" {
					d.fixtureQAResults = map[string]protocol.CheckEvidence{"invalid": {ExitCode: 0, StdoutText: "malformed command exits successfully on this fresh execution"}}
				}
				repeatRepairQA(d)
				d.recordExecutionScope(coverage.ReviewBlackbox, "invalid")
				qa, input := d.action("engine.qa.execution.v3")
				d.receipt(qa)
				ask := d.validationAsk("REPAIR")
				before := authorizationSnapshot(d)
				selected := slices.Clone(ask.Validation.Obligations)
				var remaining []string
				if extent == "partial" {
					remaining = selected[1:]
					selected = selected[:1]
				}
				event := decideValidation(d, ask, "waive", selected...)
				assertResolvedAuthorization(d, before.State.PendingAsks[ask.RequestID], event)
				afterWaive := authorizationSnapshot(d)
				if extent == "all" && afterWaive.State.Tasks[ask.Validation.Binding.Task] != runtime.TaskTerminal {
					t.Fatal("whole waiver resolved only the request, leaving its unsigned task owed")
				}
				if _, live := afterWaive.State.PendingActions[qa]; !live {
					t.Fatal("waiver cancelled unselected fresh QA")
				}
				report := executionFor(input)
				if verdict == "FAIL" {
					failed := false
					for i := range report.Records {
						if report.Records[i].CaseID == "invalid" && report.Records[i].Provenance == coverage.ProvenanceExecuted {
							report.Records[i].Result = coverage.ExecutedFail
							report.Records[i].Comparison = "specified invalid-input oracle requires rejection"
							failed = true
						}
					}
					if !failed {
						t.Fatal("fixture must fail a newly executed case, never an inherited row")
					}
				}
				d.business(qa, nativeExecutionResult(input, protocol.ValidationResult{Binding: input.Binding, Status: verdict, Execution: &report}))
				if verdict == "FAIL" {
					found := false
					for id, finding := range d.run.Validation.Findings {
						if finding.ExecutionFailure != nil && finding.ExecutionFailure.ResultAction == qa {
							resolution := executionFailureResolution(id)
							resolution.RootCause = "fresh execution exposes an independent malformed-command parser failure"
							d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: input.Binding.WaveID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
							finding = d.run.Validation.Findings[id]
							if len(finding.History) != 1 || slices.Contains(ask.Validation.Obligations, finding.History[0].ProblemID) {
								t.Fatal("new QA failure did not retain independent source responsibility")
							}
							remaining = append(remaining, finding.History[0].ProblemID)
							found = true
						}
					}
					if !found {
						t.Fatal("fresh actual FAIL did not create its execution finding")
					}
				}
				d.reopen()
				d.drive()
				slices.Sort(remaining)
				current := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
				after := authorizationSnapshot(d)
				for id, result := range before.State.Results {
					if !reflect.DeepEqual(after.State.Results[id], result) {
						t.Fatal("waiver or fresh result rewrote original completed evidence")
					}
				}
				for id, finding := range before.State.Validation.Findings {
					if !reflect.DeepEqual(after.State.Validation.Findings[id], finding) {
						t.Fatal("waiver or fresh result rewrote original failure facts")
					}
				}
				if after.State.Results[qa].Validation.Binding != input.Binding || !reflect.DeepEqual(after.State.Results[qa].Validation.Execution, &report) || !reflect.DeepEqual(after.State.Decisions[ask.RequestID], afterWaive.State.Decisions[ask.RequestID]) {
					t.Fatal("actual fresh result or waiver decision lost its complete content")
				}
				d.fixtureQAResults = nil
				if len(remaining) > 0 {
					if current.Repair == nil || current.Repair.Task != ask.Validation.Binding.Task || !reflect.DeepEqual(current.Repair.Problems, remaining) || current.Counted {
						t.Fatal("fresh result lost unique unsigned plan or exact unwaived obligations")
					}
					offer := d.validationAsk("REPAIR")
					if !reflect.DeepEqual(offer.Validation.Obligations, remaining) {
						t.Fatal("repair authorization omits remaining or fresh obligations")
					}
					decideValidation(d, offer, "approve")
					repair, repairInput := d.action("engine.repair.v5")
					if repairInput.Binding.Task != ask.Validation.Binding.Task || repairInput.Binding.WaveID != current.ID || !reflect.DeepEqual(repairInput.Repair.Obligation.Problems, remaining) {
						t.Fatal("actual repair input differs from unique current plan")
					}
					result := gateRepairResult(repairInput)
					d.business(repair, result)
					if result.Binding.PureP2 {
						d.verifyRepair(result)
					}
					d.reopen()
					d.drive()
					completed := authorizationSnapshot(d).State.Results[repair]
					if !reflect.DeepEqual(completed.Validation, &result) {
						t.Fatal("persisted repair result lost its signed input or development evidence")
					}
				} else {
					d.reopen()
					d.drive()
					current = d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
					if current.Status != "COMPLETE" || !d.run.NotGuaranteed || authorizationSnapshot(d).State.Tasks[ask.Validation.Binding.Task] != runtime.TaskTerminal {
						t.Fatal("whole waiver with fresh PASS failed to complete required validation as not guaranteed")
					}
				}
				assertAuthorizationReplay(d, event)
			})
		}
	}
}

func TestRepairIssueRemainsBoundWhileFreshQAResultArrives(t *testing.T) {
	d := postwaveRepairOffer(t)
	repeatRepairQA(d)
	d.recordExecutionScope(coverage.ReviewBlackbox, "invalid")
	qa, qaInput := d.action("engine.qa.execution.v3")
	d.receipt(qa)
	ask := d.validationAsk("REPAIR")
	event := decideValidation(d, ask, "approve")
	repair, input := d.action("engine.repair.v5")
	d.receipt(repair)
	before := authorizationSnapshot(d)
	report := executionFor(qaInput)
	d.business(qa, nativeExecutionResult(qaInput, protocol.ValidationResult{Binding: qaInput.Binding, Status: "PASS", Execution: &report}))
	d.reopen()
	d.drive()
	after := authorizationSnapshot(d)
	actual, actualInput := d.action("engine.repair.v5")
	if actual != repair || !reflect.DeepEqual(actualInput, input) || after.State.DispatchBindings[repair] != before.State.DispatchBindings[repair] || after.State.DispatchIssueRevisions[repair] != before.State.DispatchIssueRevisions[repair] || !reflect.DeepEqual(after.State.PendingAsks[ask.RequestID], before.State.PendingAsks[ask.RequestID]) || !reflect.DeepEqual(after.State.Decisions[ask.RequestID], before.State.Decisions[ask.RequestID]) {
		t.Fatal("fresh QA result moved an already signed repair, request or decision")
	}
	result := gateRepairResult(input)
	d.business(repair, result)
	if result.Binding.PureP2 {
		d.verifyRepair(result)
	}
	d.reopen()
	d.drive()
	if !reflect.DeepEqual(authorizationSnapshot(d).State.Results[repair].Validation, &result) {
		t.Fatal("signed repair completion lost its exact original input and evidence")
	}
	assertAuthorizationReplay(d, event)
}

func TestPendingRepairControlChoicesKeepTheirOwnEffects(t *testing.T) {
	for _, choice := range []string{"decline", "requirement", "abort"} {
		t.Run(choice, func(t *testing.T) {
			var d *validationDriver
			if choice == "abort" {
				root := t.TempDir()
				base := snapshotRepoCommits(t, root)[0]
				prepared := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
				d = postwaveRepairOffer(t, prepared)
			} else {
				d = postwaveRepairOffer(t)
			}
			repeatRepairQA(d)
			d.recordExecutionScope(coverage.ReviewBlackbox, "invalid")
			qa, _ := d.action("engine.qa.execution.v3")
			d.receipt(qa)
			ask := d.validationAsk("REPAIR")
			before := authorizationSnapshot(d)
			var event protocol.Event
			if choice == "decline" {
				event = protocol.Event{ID: d.eventID(), Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "decline"}}
				d.submit(event)
			} else {
				event = decideValidation(d, ask, choice)
			}
			assertResolvedAuthorization(d, before.State.PendingAsks[ask.RequestID], event)
			if choice == "requirement" {
				var request PendingAskFace
				for _, pending := range d.run.PendingAsks {
					if pending.Control == protocol.ControlRequestRequirementChange {
						request = pending
					}
				}
				if request.RequestID == "" {
					t.Fatal("requirement choice did not create its actual confirmation request")
				}
				d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(request.RequestID), Token: request.Token, Choice: "abort"}})
				if authorizationSnapshot(d).State.RequirementRevision() != before.State.RequirementRevision() {
					t.Fatal("cancelled requirement changed approved requirement")
				}
			}
			if choice != "abort" {
				after := authorizationSnapshot(d)
				if after.State.Tasks[ask.Validation.Binding.Task] == runtime.TaskTerminal || !reflect.DeepEqual(after.State.Results, before.State.Results) {
					t.Fatal("control choice fabricated repair completion or rewrote results")
				}
				for _, in := range d.run.ValidationInputs {
					if in.Handler == "engine.repair.v5" {
						t.Fatal("decline or requirement granted repair authorization")
					}
				}
				assertAuthorizationReplay(d, event)
				return
			}
			intents := make(map[string]protocol.HostActionIntent)
			for id, intent := range d.run.AdapterIntents {
				intents[id] = intent
			}
			if len(intents) == 0 || d.run.Status != "ABORTING" {
				t.Fatal("abort did not enter actual termination lifecycle")
			}
			for id, intent := range intents {
				if intent.Terminate == nil {
					t.Fatal("abort emitted a business adapter action")
				}
				for _, lifecycle := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
					fact, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), intent.Terminate.Identity, lifecycle)
					if err != nil {
						t.Fatal(err)
					}
					d.submit(fact)
				}
				receipt, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), id, intent.Operation, d.host(), intent.Terminate.Identity, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: intent.Terminate.Identity, Event: protocol.LifecycleStop})
				if err != nil {
					t.Fatal(err)
				}
				d.submit(receipt)
			}
			run, err := d.f.Drive()
			if err != nil {
				t.Fatal(err)
			}
			d.run = run
			if d.run.Status != "ABORTED" {
				t.Fatal("abort did not complete actual cleanup")
			}
			path := filepath.Join(d.f.stateDir, "terminal-summary.json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var summary TerminalSummary
			if err := json.Unmarshal(raw, &summary); err != nil {
				t.Fatal(err)
			}
			if summary.Termination == nil || summary.Termination.Decisions[ask.RequestID].EventID != string(event.ID) || !reflect.DeepEqual(summary.Termination.Decisions[ask.RequestID].Validation, event.Decide.Validation) {
				t.Fatal("terminal artifact lost actual original decision content")
			}
			d.reopen()
			d.run, err = d.f.Drive()
			if err != nil {
				t.Fatal(err)
			}
			again, err := os.ReadFile(path)
			if err != nil || !reflect.DeepEqual(raw, again) {
				t.Fatal("terminal reopen rewrote durable termination evidence")
			}
		})
	}
}

func TestValidationRemedyChoicesRefreshBeforeFreshnessBinding(t *testing.T) {
	d := postwaveRepairOffer(t)
	wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	d.submit(protocol.Event{Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{Control: protocol.ControlValidation, Options: []protocol.AskOption{{ID: "approve", Label: "Repeat selected QA"}}, Validation: &protocol.ValidationRequest{Kind: "QA_RETRY", Target: wave.ID}}})
	prior := d.validationAsk("QA_RETRY")
	if !slices.ContainsFunc(prior.Options, func(option protocol.AskOption) bool { return option.ID == "approve" }) {
		t.Fatal("current QA retry initially has no legal approval")
	}
	before := authorizationSnapshot(d)
	ask := d.validationAsk("REPAIR")
	decideValidation(d, ask, "approve")
	d.action("engine.repair.v5")
	after := authorizationSnapshot(d)
	retired := after.State.PendingAsks[prior.RequestID]
	if !retired.Resolved {
		t.Fatal("commit refreshed the old QA retry despite an in-flight repair")
	}
	retired.Resolved = false
	if !reflect.DeepEqual(retired, before.State.PendingAsks[prior.RequestID]) {
		t.Fatal("choice replacement rewrote the retired request evidence")
	}
	if _, decision := after.State.Decisions[prior.RequestID]; decision {
		t.Fatal("choice replacement fabricated user authorization")
	}
	current := d.validationAsk("QA_RETRY")
	if current.RequestID == prior.RequestID || current.Token == "" || len(current.Options) != 1 || current.Options[0].ID != "decline" {
		t.Fatal("current QA retry does not expose only its actual legal choice")
	}
	bad := protocol.Event{ID: d.eventID(), Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(prior.RequestID), Token: prior.Token, Choice: "approve", Validation: &protocol.ValidationDecision{Digest: prior.Validation.Digest, StateRevision: prior.Validation.StateRevision, Reason: "Request from the retired face", Obligations: []string{repairOfferQA(d, ask).Task.String()}}}}
	_, _, err := d.f.Submit(bad)
	if err == nil || !reflect.DeepEqual(authorizationSnapshot(d), after) {
		t.Fatal("retired choices were accepted or changed state")
	}
	d.reopen()
	d.drive()
	if again := d.validationAsk("QA_RETRY"); again.RequestID != current.RequestID || !reflect.DeepEqual(again.Options, current.Options) {
		t.Fatal("drive refreshed an ineligible choice or duplicated its request")
	}
}

func TestQARetryKeepsUnselectedQADispatch(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "whitebox", "quality")
	dev, devInput := d.action("engine.development.batch.v4")
	d.business(dev, protocol.ValidationResult{Binding: devInput.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented command"}})
	d.materialize()
	d.materialize()
	black, blackInput := qaModeAction(d, "engine.qa.design.v5", coverage.ReviewBlackbox)
	blackManifest := simpleManifest(blackInput)
	d.business(black, protocol.ValidationResult{Binding: blackInput.Binding, Status: "PASS", Manifest: &blackManifest})
	black, blackInput = qaModeAction(d, "engine.qa.review.rationale.v2", coverage.ReviewBlackbox)
	blackReview := approvedReview(blackManifest)
	d.business(black, protocol.ValidationResult{Binding: blackInput.Binding, Status: "PASS", Review: &blackReview})
	white, whiteInput := qaModeAction(d, "engine.qa.design.v5", coverage.ReviewWhitebox)
	whiteManifest := whiteInput.Manifest
	whiteManifest.Cases = []coverage.AcceptanceCase{{CaseID: "white-valid", Mode: coverage.CaseWhitebox, Scenario: "SUCCESS", TestRef: "app_test.go::TestValid", Oracle: "valid input succeeds"}, {CaseID: "white-invalid", Mode: coverage.CaseWhitebox, Scenario: "FAILURE", TestRef: "app_test.go::TestInvalid", Oracle: "invalid input is rejected"}}
	whiteManifest.Edges = []coverage.CoverageEdge{{SourceID: "REQ-001", PointID: "AC-001", CaseID: "white-valid"}, {SourceID: "REQ-001", PointID: "AC-001", CaseID: "white-invalid"}}
	d.business(white, protocol.ValidationResult{Binding: whiteInput.Binding, Status: "PASS", Manifest: &whiteManifest, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}})
	d.materialize()
	white, whiteInput = qaModeAction(d, "engine.qa.review.rationale.v2", coverage.ReviewWhitebox)
	whiteReview := approvedReview(whiteManifest)
	d.business(white, protocol.ValidationResult{Binding: whiteInput.Binding, Status: "PASS", Review: &whiteReview})
	d.materialize()
	black, blackInput = qaModeAction(d, "engine.qa.execution.v3", coverage.ReviewBlackbox)
	white, whiteInput = qaModeAction(d, "engine.qa.execution.v3", coverage.ReviewWhitebox)
	d.receipt(white)
	before := authorizationSnapshot(d)
	whitePending := d.run.PendingActions[white]
	d.submit(protocol.Event{Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{Control: protocol.ControlValidation, Options: []protocol.AskOption{{ID: "approve", Label: "Repeat selected QA"}}, Validation: &protocol.ValidationRequest{Kind: "QA_RETRY"}}})
	ask := d.validationAsk("QA_RETRY")
	event := decideValidation(d, ask, "approve", blackInput.Binding.Task.String())
	after := authorizationSnapshot(d)
	if !reflect.DeepEqual(d.run.PendingActions[white], whitePending) || !reflect.DeepEqual(d.run.ValidationInputs[white], whiteInput) || !reflect.DeepEqual(before.State.SpawnReceipts[white], after.State.SpawnReceipts[white]) || before.State.DispatchBindings[white] != after.State.DispatchBindings[white] || before.State.DispatchIssueRevisions[white] != after.State.DispatchIssueRevisions[white] {
		t.Fatal("selected QA retry replaced unselected QA's actual running identity, input or receipt")
	}
	if _, obsolete := after.State.ObsoleteActions[white]; obsolete {
		t.Fatal("unselected QA was retired")
	}
	fresh, freshInput := qaModeAction(d, "engine.qa.execution.v3", coverage.ReviewBlackbox)
	if fresh == black || freshInput.Binding.Task == blackInput.Binding.Task {
		t.Fatal("selected blackbox QA was not replaced")
	}
	for _, item := range []struct {
		action string
		input  protocol.ValidationInput
	}{{fresh, freshInput}, {white, whiteInput}} {
		report := executionFor(item.input)
		result := nativeExecutionResult(item.input, protocol.ValidationResult{Binding: item.input.Binding, Status: "PASS", Execution: &report})
		d.business(item.action, result)
		if !reflect.DeepEqual(authorizationSnapshot(d).State.Results[item.action].Validation, &result) {
			t.Fatal("QA result lost its actual frozen input and report")
		}
	}
	gate, gateInput := d.gateAction("quality")
	d.business(gate, protocol.ValidationResult{Binding: gateInput.Binding, Status: "PASS"})
	if current := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]; current.Status != "COMPLETE" || current.Targets[whiteInput.Binding.Task.String()].ResultAction != white {
		t.Fatal("unselected QA did not settle with its original actual result")
	}
	assertAuthorizationReplay(d, event)
}

func TestAbortCleansRejectedCandidateWithUndeclaredChanges(t *testing.T) {
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
	dev, di := d.action("engine.development.batch.v4")
	design, qi := d.action("engine.qa.design.v5")
	manifest := simpleManifest(qi)
	d.business(design, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Manifest: &manifest})
	id, ri := d.action("engine.qa.review.rationale.v2")
	approved := approvedReview(manifest)
	d.business(id, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &approved})
	d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented input checks"}})
	var intent protocol.HostActionIntent
	for _, value := range d.run.AdapterIntents {
		if value.Adapter != nil && value.Adapter.Operation == "op.candidate" {
			intent = value
		}
	}
	if intent.Adapter == nil {
		t.Fatal("missing original candidate intent")
	}
	undeclared := filepath.Join(di.Workspace.Path, "unassociated.data")
	if err := os.WriteFile(undeclared, []byte("original rejected artifact\n"), 0600); err != nil {
		t.Fatal(err)
	}
	state := driverState(t, d)
	if _, _, err := d.f.nativeEvidence(state, intent, true); err == nil || err.Error() != "candidate contains undeclared change: unassociated.data" {
		t.Fatal("normal candidate did not reject undeclared change", err)
	}
	prefix, err := observeNativePrefix(d.f.vcs.At(di.Workspace.Path, di.Workspace.Handle))
	if err != nil {
		t.Fatal(err)
	}
	request, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Stop run"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(request)
	for _, ask := range d.run.PendingAsks {
		if ask.RequestID == string(request.ID) {
			d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(request.ID), Token: ask.Token, Choice: "abort"}})
			break
		}
	}
	if d.run.Status != "ABORTING" {
		t.Fatal("abort was not accepted", d.run.Status)
	}
	d.completeWorkerTerminations()
	d.reopen()
	d.drive()
	if d.run.Status != "ABORTED" || d.run.Termination == nil || len(d.run.Termination.PendingHostActions) != 0 {
		t.Fatal("abort retained rejected native work", d.run.Status)
	}
	terminal := d.run.Termination
	effect := terminal.ReconciledEffects[intent.ActionID]
	if effect.Status != "WITHDRAWN" || effect.Resource == nil || effect.Resource.Resource.ID != di.Workspace.ID || !reflect.DeepEqual(effect.Resource.Prefix, &prefix) {
		t.Fatal("withdrawal lost exact original resource/pending facts", effect)
	}
	if terminal.Validation.Candidate != nil || terminal.Validation.DevelopmentSnapshot != "" {
		t.Fatal("abort accepted the rejected candidate")
	}
	if _, exists := terminal.HostActionReceipts[intent.ActionID]; exists {
		t.Fatal("withdrawal forged a candidate receipt")
	}
	for _, resource := range terminal.NativeResources {
		if resource.Status != "CLEANED" {
			t.Fatal("resource not cleaned", resource.ID, resource.Status)
		}
		if _, err := os.Stat(resource.Path); !os.IsNotExist(err) {
			t.Fatal("cleanup retained original workspace", resource.Path, err)
		}
	}
	d.reopen()
	d.drive()
	if d.run.Status != "ABORTED" {
		t.Fatal("repeated recovery changed terminal abort", d.run.Status)
	}
}
