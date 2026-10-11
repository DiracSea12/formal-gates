package facade

import (
	"encoding/json"
	"errors"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Each supported abort control preserves its accepted business identity while
// signed workers finish through their original lifecycle and typed receipts.
func TestTerminalAbortDecisionSurvivesLaterHostFacts(t *testing.T) {
	for _, control := range []protocol.ControlKind{protocol.ControlAbort, protocol.ControlValidation, protocol.ControlRecovery} {
		t.Run(string(control), func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), "blackbox", "quality")
			type acceptedEvent struct {
				event   protocol.Event
				receipt protocol.Acceptance
			}
			var accepted []acceptedEvent
			submit := func(event protocol.Event) protocol.Acceptance {
				t.Helper()
				receipt := d.submit(event)
				accepted = append(accepted, acceptedEvent{event, receipt})
				return receipt
			}
			qa, _ := d.action("engine.qa.design.v3")
			d.receipt(qa)
			start, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), qa, protocol.LifecycleStart)
			if err != nil {
				t.Fatal(err)
			}
			submit(start)
			var requestID string
			var validation *protocol.ValidationDecision
			if control == protocol.ControlValidation {
				// Missing required development evidence exhausts the existing two
				// attempts and produces a genuine RETRY disposition with Abort.
				for n := 0; n < 2; n++ {
					id, input := d.action("engine.development.batch.v4")
					d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
					d.drive()
				}
				ask := d.validationAsk("RETRY")
				requestID = ask.RequestID
				validation = &protocol.ValidationDecision{Reason: "Abort the unavailable development obligation", Digest: ask.Validation.Digest, StateRevision: d.run.Revision}
			} else {
				id, _ := d.action("engine.development.batch.v4")
				d.receipt(id)
				start, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), id, protocol.LifecycleStart)
				if err != nil {
					t.Fatal(err)
				}
				submit(start)
				if control == protocol.ControlRecovery {
					plan, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{}, emptyFingerprint())
					if err != nil || plan.Action != protocol.RecoveryAsk {
						t.Fatalf("actual recovery ask: %+v %v", plan, err)
					}
					requestID = plan.RequestID
				} else {
					request, err := protocol.NewRequestEvent(d.eventID(), control, protocol.AskOption{ID: "abort", Label: "Abort run"})
					if err != nil {
						t.Fatal(err)
					}
					submit(request)
					requestID = string(request.ID)
				}
			}
			token, err := d.f.Freshness(requestID)
			if err != nil {
				t.Fatal(err)
			}
			final := protocol.Event{ID: d.eventID(), Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(requestID), Token: token, Choice: "abort", Validation: validation}}
			original := submit(final)
			d.reopen()
			d.drive()
			if d.run.Status != "ABORTING" {
				t.Fatalf("signed responsibility ended early: %s", d.run.Status)
			}
			for _, path := range []string{terminalIntentPath(d.f.stateDir), terminalSummaryPath(d.f.stateDir)} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("terminal evidence published with signed responsibility pending: %s %v", path, err)
				}
			}
			for id, intent := range d.run.AdapterIntents {
				if intent.Operation != protocol.HostActionTerminateAgent || intent.Terminate == nil {
					t.Fatalf("actual abort operation: %+v", intent)
				}
				stop, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), intent.Terminate.Identity, protocol.LifecycleStop)
				if err != nil {
					t.Fatal(err)
				}
				submit(stop)
				// Termination has no issued correlation; the transport binds its
				// receipt correlation to this exact action ID.
				receipt, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), id, intent.Operation, d.host(), id, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: intent.Terminate.Identity, Event: protocol.LifecycleStop})
				if err != nil {
					t.Fatal(err)
				}
				submit(receipt)
			}
			snapshot, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			authority, ok := snapshot.State.AbortDecision()
			if !ok || authority.Control != control || authority.EventID != string(final.ID) || authority.Revision != original.Revision {
				t.Fatalf("accepted abort authority changed: %+v", authority)
			}
			d.reopen()
			d.drive()
			if d.run.Status != "ABORTED" {
				t.Fatalf("resolved abort did not finish: %s", d.run.Status)
			}
			summary, err := d.f.readSummary()
			if err != nil {
				t.Fatal(err)
			}
			digest, err := final.Digest()
			if err != nil {
				t.Fatal(err)
			}
			if summary.LastEvent == nil || summary.LastEvent.Digest != digest || !reflect.DeepEqual(summary.LastEvent.Acceptance, original) {
				t.Fatalf("original terminal business receipt replaced: %+v", summary.LastEvent)
			}
			if summary.Termination == nil {
				t.Fatal("terminal summary lost the State event ledger")
			}
			for id, original := range snapshot.State.Events {
				if !reflect.DeepEqual(summary.Termination.Events[id], original) {
					t.Fatal("later host facts changed or disappeared from the event ledger")
				}
			}
			for id, record := range summary.Termination.Events {
				if _, existed := snapshot.State.Events[id]; !existed && record.Acceptance.Kind != string(protocol.KindHostActionReceipt) {
					t.Fatalf("cleanup added an unrelated event: %+v", record)
				}
			}
			before, err := os.ReadFile(terminalSummaryPath(d.f.stateDir))
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range accepted {
				again, _, err := d.f.Submit(item.event)
				if err != nil || !reflect.DeepEqual(again, item.receipt) {
					t.Fatalf("original event %s replay changed: %+v %v", item.event.ID, again, err)
				}
			}
			d.reopen()
			d.drive()
			after, err := os.ReadFile(terminalSummaryPath(d.f.stateDir))
			if err != nil || string(before) != string(after) {
				t.Fatalf("terminal bytes changed on replay/reopen: %v", err)
			}
		})
	}
}

// Accepted events retain their original receipts across every terminal cleanup seam.
func TestTerminalEventReceiptSurvivesCleanup(t *testing.T) {
	for _, seam := range []string{"before-cleanup", "after-cleanup", "intent-before-cleanup", "cleanup-before-summary", "summary-before-intent-removal"} {
		t.Run(seam, func(t *testing.T) {
			d := nativeMasterDriver(t)
			for id := range d.run.PendingActions {
				d.receipt(id)
				for _, kind := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
					event, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), id, kind)
					if err != nil {
						t.Fatal(err)
					}
					d.submit(event)
				}
			}
			request, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Abort"})
			if err != nil {
				t.Fatal(err)
			}
			earlier := d.submit(request)
			token, err := d.f.Freshness(string(request.ID))
			if err != nil {
				t.Fatal(err)
			}
			final := protocol.Event{ID: d.eventID(), Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(request.ID), Token: token, Choice: "abort"}}
			first := d.submit(final)
			settleAbortForPublication(t, d)
			if seam == "after-cleanup" {
				d.drive()
			} else if seam != "before-cleanup" {
				snapshot, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if err := d.f.freezeCleanupSummary(snapshot.State); err != nil {
					t.Fatal(err)
				}
				summary, err := d.f.readTerminalIntent()
				if err != nil {
					t.Fatal(err)
				}
				if seam != "intent-before-cleanup" {
					if err := d.f.store.CleanupTerminal(); err != nil {
						t.Fatal(err)
					}
				}
				if seam == "summary-before-intent-removal" {
					if err := d.f.writeSummaryValue(summary); err != nil {
						t.Fatal(err)
					}
				}
			}
			d.reopen()
			summaryBefore, _ := os.ReadFile(terminalSummaryPath(d.f.stateDir))
			intentBefore, _ := os.ReadFile(terminalIntentPath(d.f.stateDir))
			stateBefore, _ := os.ReadFile(filepath.Join(d.f.stateDir, "state.json"))
			again, run, err := d.f.Submit(final)
			if err != nil {
				t.Fatalf("original receipt lost at %s: %v", seam, err)
			}
			expected, _ := json.Marshal(first)
			actual, _ := json.Marshal(again)
			if string(expected) != string(actual) {
				t.Fatalf("stable receipt changed: %s != %s", expected, actual)
			}
			previous, _, err := d.f.Submit(request)
			if err != nil {
				t.Fatalf("earlier accepted event lost at %s: %v", seam, err)
			}
			previousWant, _ := json.Marshal(earlier)
			previousGot, _ := json.Marshal(previous)
			if string(previousWant) != string(previousGot) {
				t.Fatalf("earlier receipt changed: %s != %s", previousWant, previousGot)
			}
			if seam == "after-cleanup" || seam == "summary-before-intent-removal" {
				if run.Status != "ABORTED" {
					t.Fatalf("terminal not published: %s", run.Status)
				}
			} else if run.Status != "FINALIZING_CLEANUP" {
				t.Fatalf("premature terminal publication: %s", run.Status)
			}
			conflict := final
			payload := *final.Decide
			payload.Choice = "different"
			conflict.Decide = &payload
			_, _, err = d.f.Submit(conflict)
			if err == nil {
				t.Fatal("same ID with different digest accepted")
			}
			var rejected *protocol.RejectedError
			if !errors.As(err, &rejected) || rejected.Code != protocol.CodeDuplicateEventMismatch {
				t.Fatalf("wrong replay conflict: %v", err)
			}
			next := final
			next.ID = d.eventID()
			if _, _, err = d.f.Submit(next); err == nil {
				t.Fatal("new terminal event accepted")
			}
			summaryAfter, _ := os.ReadFile(terminalSummaryPath(d.f.stateDir))
			intentAfter, _ := os.ReadFile(terminalIntentPath(d.f.stateDir))
			stateAfter, _ := os.ReadFile(filepath.Join(d.f.stateDir, "state.json"))
			if string(summaryBefore) != string(summaryAfter) || string(intentBefore) != string(intentAfter) || string(stateBefore) != string(stateAfter) {
				t.Fatal("replay/refusal rewrote durable evidence")
			}
			d.drive()
			if d.run.Status != "ABORTED" {
				t.Fatalf("cleanup recovery failed: %s", d.run.Status)
			}
			summary, err := d.f.readSummary()
			if err != nil {
				t.Fatal(err)
			}
			digest, err := final.Digest()
			if err != nil {
				t.Fatal(err)
			}
			if summary.LastEvent == nil || summary.LastEvent.Digest != digest || summary.LastEvent.Acceptance.EventID != string(final.ID) {
				t.Fatalf("summary lost original final identity: %+v", summary.LastEvent)
			}
			raw, _ := json.Marshal(summary.LastEvent.Acceptance)
			if string(raw) != string(expected) {
				t.Fatal("summary lost final stable acceptance content")
			}
			if summary.Termination == nil || summary.Termination.Events[string(final.ID)].Digest != digest {
				t.Fatal("summary lost original termination event evidence")
			}
			if got, _ := json.Marshal(summary.Termination.Events[string(request.ID)].Acceptance); string(got) != string(previousWant) {
				t.Fatal("summary lost earlier event acceptance content")
			}
		})
	}
}

// LastEvent is sufficient for supported terminal payloads without a State ledger.
func TestTerminalReplaySupportsSummaryWithoutStateLedger(t *testing.T) {
	d := nativeMasterDriver(t)
	event, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Stop"})
	if err != nil {
		t.Fatal(err)
	}
	accepted := d.submit(event)
	run, err := d.f.project()
	if err != nil {
		t.Fatal(err)
	}
	summary := summaryForRun(run, ownerInterval{})
	summary.Status = "ABORTED"
	summary.Termination = nil
	if err := writeJSONAtomic(terminalIntentPath(d.f.stateDir), summary); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(terminalIntentPath(d.f.stateDir))
	if err != nil {
		t.Fatal(err)
	}
	again, err := d.f.replayTerminalEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(accepted)
	got, _ := json.Marshal(again)
	if string(got) != string(want) {
		t.Fatal("LastEvent acceptance changed")
	}
	after, err := os.ReadFile(terminalIntentPath(d.f.stateDir))
	if err != nil || string(before) != string(after) {
		t.Fatalf("supported summary replay changed bytes: %v", err)
	}
}
