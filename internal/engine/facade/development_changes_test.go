package facade

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
)

func TestSameHostHandoffPreservesWorkAndDoesNotConsumeFailureBudget(t *testing.T) {
	for _, status := range []string{protocol.SpawnStatusSpawned, protocol.SpawnStatusAttached} {
		t.Run(status, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox")
			action, _ := d.action("engine.development.batch.v6")
			identity := observeWorkerDispatch(d, action, status)
			d.reopen()
			before, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			pending := before.State.PendingActions[action]
			original := before.State.Attempts[pending.Task]
			reason := "user requests another developer for unfinished work"
			worker := workerNumberForAction(d.run, action)
			run, err := d.f.RequestWorkerHandoff(worker, reason)
			if err != nil {
				t.Fatal(err)
			}
			d.run = run
			requested, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			var record protocol.EventRecord
			for id, candidate := range requested.State.Events {
				if _, existed := before.State.Events[id]; !existed {
					if record.Acceptance.EventID != "" {
						t.Fatal("handoff created more than one event")
					}
					record = candidate
				}
			}
			event := protocol.Event{ID: protocol.EventID(record.Acceptance.EventID), Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{
				Control: protocol.ControlRecovery, Handoff: &protocol.DevelopmentHandoffRequest{ActionID: action, Reason: reason},
			}}
			digest, err := event.Digest()
			if err != nil {
				t.Fatal(err)
			}
			requestID := record.Acceptance.RequestID
			token, err := d.f.Freshness(requestID)
			if err != nil {
				t.Fatal(err)
			}
			wantAcceptance := protocol.Acceptance{EventID: string(event.ID), Kind: string(event.Kind), Status: "ACCEPTED", Revision: before.Revision + 1, RequestID: requestID, FreshnessToken: token, ActionID: action}
			if record.Digest != digest || requestID == "" || !reflect.DeepEqual(record.Acceptance, wantAcceptance) || requested.Revision != before.Revision+1 {
				t.Fatalf("handoff event record=%+v want=%+v digest=%s", record, wantAcceptance, digest)
			}
			if len(requested.State.RecoveryRecords) != len(before.State.RecoveryRecords)+1 {
				t.Fatal("handoff did not record one recovery")
			}
			recovery := requested.State.RecoveryRecords[len(requested.State.RecoveryRecords)-1]
			wantRecovery := protocol.RecoveryRecord{Handoff: true, ActionID: action, Task: pending.Task, AttemptID: pending.AttemptID, Action: protocol.RecoveryAsk, Detail: reason, Revision: requested.Revision, RequestID: requestID}
			if !reflect.DeepEqual(recovery, wantRecovery) || !reflect.DeepEqual(requested.State.Cost, before.State.Cost) || !reflect.DeepEqual(requested.State.PendingActions, before.State.PendingActions) || !reflect.DeepEqual(requested.State.Attempts, before.State.Attempts) {
				t.Fatalf("handoff lost original recovery facts or cost: recovery=%+v want=%+v", recovery, wantRecovery)
			}
			wantAsk := protocol.PendingAsk{RequestID: requestID, Control: protocol.ControlRecovery, Options: []protocol.AskOption{{ID: "fresh", Label: "Transfer unfinished work to a new developer on the same host"}, {ID: "abort", Label: "Abort the run"}}}
			if !reflect.DeepEqual(requested.State.PendingAsks[requestID], wantAsk) {
				t.Fatalf("handoff offer=%+v", requested.State.PendingAsks[requestID])
			}
			currentWorkers := workerActionOrder(requested.State.WorkerInputs(d.f.definition))
			if worker < 1 || worker > len(currentWorkers) || currentWorkers[worker-1] != action {
				t.Fatal("handoff changed the selected current worker")
			}
			d.reopen()
			if d.run, err = d.f.RequestWorkerHandoff(worker, reason); err != nil {
				t.Fatal(err)
			}
			replayed, _, err := d.f.Submit(event)
			if err != nil || !reflect.DeepEqual(replayed, record.Acceptance) {
				t.Fatalf("handoff replay=%+v err=%v", replayed, err)
			}
			conflict := event
			conflict.Request = &protocol.RequestPayload{Control: protocol.ControlRecovery, Handoff: &protocol.DevelopmentHandoffRequest{ActionID: action, Reason: "a changed reason under the original event ID"}}
			_, _, err = d.f.Submit(conflict)
			var rejected *protocol.RejectedError
			if !errors.As(err, &rejected) || rejected.Code != protocol.CodeDuplicateEventMismatch {
				t.Fatalf("handoff conflict=%v", err)
			}
			unchanged, err := d.f.engine.Load()
			if err != nil || !reflect.DeepEqual(requested, unchanged) {
				t.Fatalf("handoff replay, reopen or rejected payload changed state: %v", err)
			}
			if d.run, err = d.f.RequestWorkerHandoff(worker, "another user reason for the same unfinished work"); err != nil {
				t.Fatal(err)
			}
			repeated, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if repeated.Revision != requested.Revision+1 || len(repeated.State.Events) != len(requested.State.Events)+1 || !reflect.DeepEqual(repeated.State.PendingAsks, requested.State.PendingAsks) || !reflect.DeepEqual(repeated.State.RecoveryRecords, requested.State.RecoveryRecords) {
				t.Fatal("new request for the same execution did not bind its event to the original recovery")
			}
			ask := askPosition(t, d.run, protocol.ControlRecovery)
			candidate, err := d.f.PrepareControl(ControlInput{Ask: ask, Choice: "fresh", Reason: "confirm same-host transfer"})
			if err != nil {
				t.Fatal(err)
			}
			_, d.run, err = d.f.SubmitControl(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if status == protocol.SpawnStatusSpawned {
				if len(d.run.AdapterIntents) == 0 {
					t.Fatal("handoff bypassed native termination")
				}
				terminating, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if d.run, err = d.f.RequestWorkerHandoff(worker, "repeat transfer while the original termination receipt is pending"); err != nil {
					t.Fatal(err)
				}
				waiting, err := d.f.engine.Load()
				if err != nil || !reflect.DeepEqual(waiting.State.PendingHostActions, terminating.State.PendingHostActions) || !reflect.DeepEqual(waiting.State.PendingAsks, terminating.State.PendingAsks) || !reflect.DeepEqual(waiting.State.RecoveryRecords, terminating.State.RecoveryRecords) || !reflect.DeepEqual(waiting.State.Attempts, terminating.State.Attempts) || !reflect.DeepEqual(waiting.State.Cost, terminating.State.Cost) {
					t.Fatalf("repeated handoff replaced its pending native recovery effect: %v", err)
				}
				reopen, err := Open(d.f.root, d.f.runID)
				if err != nil {
					t.Fatal(err)
				}
				d.f = reopen
				d.drive()
				termination := false
				for _, intent := range d.run.AdapterIntents {
					if intent.Terminate != nil && intent.Terminate.WorkerActionID == action {
						termination = true
						if intent.Terminate.Identity != identity || intent.Terminate.AttemptID != pending.AttemptID {
							t.Fatalf("termination differs from original observed execution: %+v", intent.Terminate)
						}
					}
				}
				if !termination {
					t.Fatal("handoff did not request termination of the original native execution")
				}
				d.completeWorkerTerminations()
			} else {
				for _, intent := range d.run.AdapterIntents {
					if intent.Terminate != nil && intent.Terminate.WorkerActionID == action {
						t.Fatal("handoff repeated termination of the observed stopped native execution")
					}
				}
				stopped := false
				for _, lifecycle := range before.State.LifecycleEvents {
					if lifecycle.Identity == identity && lifecycle.Event == protocol.LifecycleStop {
						stopped = true
					}
				}
				if !stopped {
					t.Fatal("fresh handoff has no original native stop evidence")
				}
				d.reopen()
			}
			d.drive()
			after, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			next, exists := after.State.Attempts[pending.Task]
			if !exists || next.ActionID == action || next.PreviousAction != action || next.Attempts != original.Attempts || next.MaxAttempts != original.MaxAttempts || !reflect.DeepEqual(next.Work, original.Work) {
				t.Fatalf("handoff changed work/budget: original=%+v next=%+v", original, next)
			}
			if after.State.RunProvider != before.State.RunProvider || after.State.Guard.UsedRetries != before.State.Guard.UsedRetries || len(after.State.RejectedResults) != len(before.State.RejectedResults) {
				t.Fatal("handoff fabricated failure, host change or retry use")
			}
			if before.State.Cost != nil {
				if after.State.Cost == nil {
					t.Fatal("handoff discarded historical costs")
				}
				for id, entry := range before.State.Cost.Dispatches {
					if actual, exists := after.State.Cost.Dispatches[id]; !exists || !reflect.DeepEqual(actual, entry) {
						t.Fatalf("handoff replaced historical dispatch cost %s: %+v", id, actual)
					}
				}
			}
			replayed, _, err = d.f.Submit(event)
			if err != nil || !reflect.DeepEqual(replayed, record.Acceptance) {
				t.Fatalf("accepted handoff could not replay after replacement: %+v %v", replayed, err)
			}
			unchanged, err = d.f.engine.Load()
			if err != nil || !reflect.DeepEqual(after, unchanged) {
				t.Fatalf("late handoff replay changed state: %v", err)
			}
			for index, record := range before.State.RecoveryRecords {
				if !reflect.DeepEqual(after.State.RecoveryRecords[index], record) {
					t.Fatal("handoff changed original reconciliation evidence")
				}
			}
			for id, worker := range before.State.PendingActions {
				if id != action && (after.State.PendingActions[id] != worker || !reflect.DeepEqual(after.State.Attempts[worker.Task], before.State.Attempts[worker.Task])) {
					t.Fatal("handoff changed an independent unfinished worker")
				}
			}
			for _, record := range after.State.RecoveryRecords[len(before.State.RecoveryRecords):] {
				if record.ActionID == action && record.Class != "" {
					t.Fatalf("handoff fabricated failure class: %+v", record)
				}
			}
		})
	}
}

func TestReplanRequiresUnchangedSourceAndExactCarryRedoThenUsesOriginalConfirmation(t *testing.T) {
	d := newValidationDriver(t, "blackbox")
	before, err := d.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	choices := make([]WorkChoice, len(before.RequirementWork))
	for i := range choices {
		choices[i] = WorkChoice{Disposition: protocol.RequirementWorkCarry, Reason: "existing unfinished obligation remains valid"}
	}
	candidate, err := d.f.PrepareReplan(RequirementChangeInput{Work: choices, Impact: protocol.RequirementImpact{ChangedItems: []string{"implementation plan"}, CarryReasons: []string{"confirmed product requirement remains identical"}}})
	if err != nil {
		t.Fatal(err)
	}
	pkg := candidate.Event.Request.Package
	if strings.TrimPrefix(pkg.CandidateRevision, "sha256:") != strings.TrimPrefix(before.Review.RequirementRevision, "sha256:") || pkg.RedoProductReview || pkg.RedoTopology || !pkg.RedoStartReadiness || !pkg.RedoRoute {
		t.Fatalf("replan scope=%+v", pkg)
	}
	if _, _, err := d.f.SubmitControl(candidate); err != nil {
		t.Fatal(err)
	}
	d.drive()
	confirm, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "confirm unchanged requirement and plan review with exact work carry"})
	if err != nil {
		t.Fatal(err)
	}
	_, d.run, err = d.f.SubmitControl(confirm)
	if err != nil {
		t.Fatal(err)
	}
	if d.run.Review.RequirementRevision != before.Review.RequirementRevision || !reflect.DeepEqual(d.run.Review.Topology, before.Review.Topology) || d.run.Review.Actions[0].EffectiveStatus != before.Review.Actions[0].EffectiveStatus {
		t.Fatal("replan changed product/source/topology")
	}
}

func TestReplanWithDisabledReadinessPreservesSelectionAndRenewsPreparation(t *testing.T) {
	for _, development := range []bool{true, false} {
		name := "development-disabled"
		if development {
			name = "development-enabled"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			disabled := []string{"product-review", "start-readiness"}
			if !development {
				disabled = append(disabled, "development")
			}
			intake, err := PrepareIntake(root, "requirements.md", "", nil, true)
			if err != nil {
				t.Fatal(err)
			}
			f, run, err := Start(StartOptions{Root: root, ArtifactRoot: root, Request: StartRequest{
				RunID: "selected-replan", Route: "regular", BaseSnapshot: base,
				FlowSelection: &protocol.FlowSelection{Disabled: disabled}, IntakeConfirmationReceipt: intake.IntakeCandidate,
			}, Admission: testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)})
			if err != nil {
				t.Fatal(err)
			}
			d := &validationDriver{t: t, f: f, run: run}
			d.drive()
			var oldAction string
			var oldBinding protocol.WorkBinding
			if development {
				action, input := d.action("engine.development.plan.v1")
				oldAction, oldBinding = action, input.Binding
				d.business(action, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", GranularityReview: commandPlan()})
			}
			confirmChoice := func(choice string) ControlCandidate {
				t.Helper()
				if requested, err := d.f.RequestWorkflowChoice(); err != nil || !requested {
					t.Fatal("original topology/route confirmation was not offered", choice, err)
				}
				candidate, err := d.f.PrepareCurrentControl(ControlInput{Ask: 1, Choice: choice, Reason: "confirm the displayed requirement and plan scope"})
				if err != nil {
					t.Fatal(err)
				}
				_, d.run, err = d.f.SubmitControl(candidate)
				if err != nil {
					t.Fatal(err)
				}
				return candidate
			}
			confirmChoice("no-split")
			original := confirmChoice("custom")
			if development && (original.DevelopmentPlan == nil || original.DevelopmentPlan.Confirmation.ReviewActionID != oldAction) {
				t.Fatal("original confirmation did not bind the prepared proposal")
			}
			before := d.run
			choices := make([]WorkChoice, len(before.RequirementWork))
			for i := range choices {
				choices[i] = WorkChoice{Disposition: protocol.RequirementWorkCarry, Reason: "existing unfinished obligation remains valid"}
				if development && before.RequirementWork[i].Task == original.DevelopmentPlan.Tasks[0].Tasks[0] {
					choices[i] = WorkChoice{Disposition: protocol.RequirementWorkRedo, Reason: "the changed batch contract requires fresh execution"}
				}
			}
			request, err := d.f.PrepareReplan(RequirementChangeInput{Work: choices, Impact: protocol.RequirementImpact{ChangedItems: []string{"implementation plan"}, CarryReasons: []string{"confirmed product requirement remains identical"}}})
			if err != nil {
				t.Fatal(err)
			}
			pkg := request.Event.Request.Package
			if pkg.RedoProductReview || pkg.RedoStartReadiness || pkg.RedoTopology || !pkg.RedoRoute {
				t.Fatal("replan enabled an unselected review or changed topology", pkg)
			}
			_, d.run, err = d.f.SubmitControl(request)
			if err != nil {
				t.Fatal(err)
			}
			confirmation, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "confirm the unchanged requirement and exact work dispositions"})
			if err != nil {
				t.Fatal(err)
			}
			acceptance, confirmed, err := d.f.SubmitControl(confirmation)
			if err != nil {
				t.Fatal(err)
			}
			d.run = confirmed
			d.reopen()
			if d.run.Review.RequirementRevision != before.Review.RequirementRevision || !reflect.DeepEqual(d.run.Review.Topology, before.Review.Topology) || !reflect.DeepEqual(d.run.Review.Actions, before.Review.Actions) || !reflect.DeepEqual(d.run.FlowSelection, before.FlowSelection) || d.run.Review.RouteMode != "" || d.run.Review.ReadinessProposal != nil {
				t.Fatal("replan changed source/selection/reviews/topology or reused an expired proposal")
			}
			if replay, replayed, err := d.f.SubmitControl(confirmation); err != nil || !reflect.DeepEqual(replay, acceptance) || !reflect.DeepEqual(replayed, d.run) {
				t.Fatal("replayed confirmation changed its original facts", err)
			}
			d.drive()
			var freshAction string
			if development {
				action, input := d.action("engine.development.plan.v1")
				freshAction = action
				if action == oldAction || input.Binding.Task == oldBinding.Task || input.Binding.Generation <= oldBinding.Generation || input.Binding.RequirementRevision != oldBinding.RequirementRevision {
					t.Fatal("same requirement did not receive a new preparation identity", input.Binding)
				}
				if source := driverState(t, d).Results[oldAction]; source.Validation == nil || source.Validation.Binding != oldBinding {
					t.Fatal("replan lost the original proposal evidence")
				}
				plan := commandPlan()
				plan.Batches[0].DoD = append(plan.Batches[0].DoD, protocol.DevelopmentCriterion{ID: "diagnostic", Description: "Invalid input reports its specific diagnostic"})
				d.business(action, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", GranularityReview: plan})
			}
			reselected := confirmChoice("custom")
			if development {
				if reselected.DevelopmentPlan == nil || reselected.DevelopmentPlan.Confirmation.ReviewActionID != freshAction || len(reselected.DevelopmentPlan.Proposal.Batches[0].DoD) != 2 {
					t.Fatal("route reconfirmation did not bind the new prepared plan")
				}
			} else if reselected.DevelopmentPlan != nil || d.run.Review.ReadinessProposal != nil || len(d.run.PendingActions) != 0 {
				t.Fatal("disabled development manufactured a plan or worker")
			}
		})
	}
}

func TestHandoffRejectsCurrentNonDeveloperAndUnstartedExecution(t *testing.T) {
	d := newValidationDriver(t, "blackbox")
	developer, _ := d.action("engine.development.batch.v6")
	qa, _ := d.action("engine.qa.design.v6")
	d.receipt(qa)
	for _, request := range []struct{ action, reason string }{
		{qa, "selected the QA worker by mistake"},
		{developer, "developer has not started"},
	} {
		before, err := d.f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.f.RequestWorkerHandoff(workerNumberForAction(d.run, request.action), request.reason); err == nil || !strings.Contains(err.Error(), "HANDOFF_BINDING") {
			t.Fatalf("invalid current handoff accepted: %+v %v", request, err)
		}
		after, err := d.f.engine.Load()
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("rejected handoff changed state: %v", err)
		}
	}
	d.receipt(developer)
	before, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.f.RequestWorkerHandoff(workerNumberForAction(d.run, developer), " "); err == nil || !strings.Contains(err.Error(), "HANDOFF_BINDING") {
		t.Fatalf("handoff without actual user reason=%v", err)
	}
	barrier, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlRequestRequirementChange, protocol.AskOption{ID: "proceed", Label: "Confirm the complete changed requirement"})
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := d.f.engine.Load()
	if err != nil || !reflect.DeepEqual(before, unchanged) {
		t.Fatalf("missing reason changed state: %v", err)
	}
	d.submit(barrier)
	frozen, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	event := protocol.Event{ID: d.eventID(), Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{
		Control: protocol.ControlRecovery, Handoff: &protocol.DevelopmentHandoffRequest{ActionID: developer, Reason: "transfer while the requirement is being clarified"},
	}}
	if _, _, err := d.f.Submit(event); err == nil || !strings.Contains(err.Error(), "HANDOFF_BINDING") {
		t.Fatalf("requirement barrier admitted handoff: %v", err)
	}
	after, err := d.f.engine.Load()
	if err != nil || !reflect.DeepEqual(frozen, after) {
		t.Fatalf("barrier rejection changed state: %v", err)
	}
}

func TestHandoffWaitsForUnknownSpawnIdentity(t *testing.T) {
	d := newValidationDriver(t, "blackbox")
	action, _ := d.action("engine.development.batch.v6")
	worker := workerNumberForAction(d.run, action)
	unknown, err := protocol.NewSpawnReceiptEvent(d.eventID(), action, d.host(), action, protocol.SpawnStatusUnknown)
	if err != nil {
		t.Fatal(err)
	}
	d.submit(unknown)
	before, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if before.State.SpawnReceipts[action].Status != protocol.SpawnStatusUnknown {
		t.Fatal("unknown identity was silently attached")
	}
	if _, err := d.f.RequestWorkerHandoff(worker, "transfer a developer whose native identity is unknown"); err == nil || !strings.Contains(err.Error(), "HANDOFF_BINDING") {
		t.Fatalf("unknown native identity admitted handoff: %v", err)
	}
	after, err := d.f.engine.Load()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("handoff bypassed unknown receipt reconciliation: %v", err)
	}
}
