package facade

import (
	"encoding/json"
	"errors"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"testing"
)

// The final stable event remains replayable across every terminal cleanup seam.
func TestTerminalEventReceiptSurvivesCleanup(t *testing.T) {
	for _, seam := range []string{"before-cleanup", "after-cleanup", "intent-before-cleanup", "cleanup-before-summary", "summary-before-intent-removal"} {
		t.Run(seam, func(t *testing.T) {
			d := startValidationDriver(t, nil)
			for id := range d.run.PendingActions {
				d.receipt(id)
				for _, kind := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
					event, err := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, id, kind)
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
			d.submit(request)
			token, err := d.f.Freshness(string(request.ID))
			if err != nil {
				t.Fatal(err)
			}
			final := protocol.Event{ID: d.eventID(), Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(request.ID), Token: token, Choice: "abort"}}
			first := d.submit(final)
			if seam == "after-cleanup" {
				d.drive()
			} else if seam != "before-cleanup" {
				run, err := d.f.project()
				if err != nil {
					t.Fatal(err)
				}
				summary := summaryForRun(run, ownerInterval{})
				if err := writeJSONAtomic(terminalIntentPath(d.f.stateDir), summary); err != nil {
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
			if seam == "after-cleanup" {
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
		})
	}
}
