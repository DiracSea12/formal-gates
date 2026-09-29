package facade

import (
	"encoding/json"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"testing"
)

// Public terminal status waits for the immutable summary committed by Drive.
func TestAbortPublicationWaitsForCleanup(t *testing.T) {
	d := startValidationDriver(t, nil)
	for id := range d.run.PendingActions {
		d.receipt(id)
		ev, err := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, id, protocol.LifecycleStart)
		if err != nil {
			t.Fatal(err)
		}
		d.submit(ev)
		ev, err = protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, id, protocol.LifecycleStop)
		if err != nil {
			t.Fatal(err)
		}
		d.submit(ev)
	}
	req, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Abort"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(req)
	token, err := d.f.Freshness(string(req.ID))
	if err != nil {
		t.Fatal(err)
	}
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(req.ID), Token: token, Choice: "abort"}})
	_, stateErr := os.Stat(filepath.Join(d.f.stateDir, "state.json"))
	_, summaryErr := os.Stat(filepath.Join(d.f.stateDir, "terminal-summary.json"))
	_, intentErr := os.Stat(filepath.Join(d.f.stateDir, terminalIntentName))
	t.Logf("submit status=%s next=%s activeState=%v summary=%v intent=%v", d.run.Status, d.run.Next.Kind, stateErr, summaryErr, intentErr)
	if stateErr == nil && os.IsNotExist(summaryErr) && (d.run.Status == "ABORTED" || d.run.Status == "COMPLETE" || d.run.Next.Kind == "COMPLETE") {
		t.Error("Abort was published Complete before cleanup or terminal summary")
	}
	for _, read := range []func() (Run, error){d.f.Show, d.f.Status} {
		r, err := read()
		if err != nil {
			t.Fatal(err)
		}
		if stateErr == nil && os.IsNotExist(summaryErr) && (r.Status == "ABORTED" || r.Next.Kind == "COMPLETE") {
			t.Error("read-only projection published uncommitted terminal")
		}
	}
	next, err := d.f.Next()
	if err != nil {
		t.Fatal(err)
	}
	if stateErr == nil && os.IsNotExist(summaryErr) && next.Kind == "COMPLETE" {
		t.Error("Next published uncommitted terminal")
	}
	d.drive()
	_, stateErr = os.Stat(filepath.Join(d.f.stateDir, "state.json"))
	_, summaryErr = os.Stat(filepath.Join(d.f.stateDir, "terminal-summary.json"))
	t.Logf("extra Drive status=%s next=%s activeState=%v summary=%v", d.run.Status, d.run.Next.Kind, stateErr, summaryErr)
	if d.run.Status != "ABORTED" || d.run.Next.Kind != "COMPLETE" || !os.IsNotExist(stateErr) || summaryErr != nil {
		t.Fatal("terminal cleanup did not complete")
	}
	raw, err := os.ReadFile(filepath.Join(d.f.stateDir, "terminal-summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var summary TerminalSummary
	if err = json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Termination == nil || len(summary.Termination.Decisions) == 0 || summary.Status != "ABORTED" {
		t.Fatal("summary lost original terminal evidence")
	}
	revision := d.run.Revision
	d.reopen()
	d.drive()
	if d.run.Revision != revision {
		t.Fatal("terminal recovery repeated work")
	}
	again, err := os.ReadFile(filepath.Join(d.f.stateDir, "terminal-summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(again) {
		t.Fatal("recovery rewrote immutable terminal evidence")
	}
}
