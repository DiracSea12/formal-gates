package facade

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"formal-gates/internal/engine/protocol"
)

// Waiting submissions re-read the current event/terminal owner. The native
// owner may accept a receipt or finish cleanup before releasing its window.
func TestNativeSubmissionReplaysFactsAcceptedWhileWaiting(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"recorded", "mismatch", "terminal-recorded", "terminal-new"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			f := startSnapshotRun(t, root, "waiting", base)
			event, err := protocol.NewLifecycleEventEvent("waiting-event", DefaultProvider, "session", protocol.LifecycleStart)
			if err != nil {
				t.Fatal(err)
			}
			unlock, err := f.acquireNativeWindow()
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if unlock != nil {
					unlock()
				}
			}()
			type result struct {
				accepted protocol.Acceptance
				err      error
			}
			done := make(chan result, 1)
			started := make(chan struct{})
			go func() { close(started); a, _, err := f.Submit(event); done <- result{a, err} }()
			<-started
			before, err := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			select {
			case r := <-done:
				t.Fatalf("new lifecycle event escaped the native window: %+v", r)
			case <-time.After(time.Second):
			}
			after, err := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
			if err != nil || string(after) != string(before) {
				t.Fatal("waiting event changed authoritative state")
			}
			var original protocol.Acceptance
			if kind != "terminal-new" {
				admitted := event
				if kind == "mismatch" {
					admitted, err = protocol.NewLifecycleEventEvent(event.ID, DefaultProvider, "other-session", protocol.LifecycleStart)
					if err != nil {
						t.Fatal(err)
					}
				}
				original, err = f.engine.Submit(admitted, protocol.Observation{})
				if err != nil {
					t.Fatal(err)
				}
			}
			evidencePath := filepath.Join(f.stateDir, "state.json")
			if kind == "terminal-recorded" || kind == "terminal-new" {
				request, err := protocol.NewRequestEvent("abort-request", protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Abort"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.engine.Submit(request, protocol.Observation{}); err != nil {
					t.Fatal(err)
				}
				token, err := f.Freshness(string(request.ID))
				if err != nil {
					t.Fatal(err)
				}
				decide, err := protocol.NewDecideEvent("abort-decision", protocol.RequestID(request.ID), token, "abort")
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.engine.Submit(decide, protocol.Observation{}); err != nil {
					t.Fatal(err)
				}
				if _, _, err = f.engine.Drive(protocol.Observation{}); err != nil {
					t.Fatal(err)
				}
				run, err := f.project(nil)
				if err != nil || run.Status != "ABORTED" {
					t.Fatalf("normal abort did not finish: %s %v", run.Status, err)
				}
				if err = f.finalizeTerminal(run, ownerInterval{}); err != nil {
					t.Fatal(err)
				}
				evidencePath = terminalSummaryPath(f.stateDir)
			}
			frozen, err := os.ReadFile(evidencePath)
			if err != nil {
				t.Fatal(err)
			}
			unlock()
			unlock = nil
			var r result
			select {
			case r = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("submission did not finish after window release")
			}
			switch kind {
			case "recorded", "terminal-recorded":
				if r.err != nil || !reflect.DeepEqual(r.accepted, original) {
					t.Fatalf("current exact replay changed acceptance: %+v %v want %+v", r.accepted, r.err, original)
				}
			case "mismatch":
				var rejected *protocol.RejectedError
				if !errors.As(r.err, &rejected) || rejected.Code != protocol.CodeDuplicateEventMismatch {
					t.Fatalf("different payload replay: %v", r.err)
				}
			case "terminal-new":
				if r.err == nil {
					t.Fatal("new event accepted after terminal cleanup")
				}
			}
			unchanged, err := os.ReadFile(evidencePath)
			if err != nil || string(unchanged) != string(frozen) {
				t.Fatal("replay/rejection wrote evidence")
			}
		})
	}
}

func TestAcceptedNativeSubmissionReplayDoesNotWait(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	f := startSnapshotRun(t, root, "exact-replay", base)
	event, err := protocol.NewLifecycleEventEvent("accepted-event", DefaultProvider, "session", protocol.LifecycleStart)
	if err != nil {
		t.Fatal(err)
	}
	original, _, err := f.Submit(event)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := f.acquireNativeWindow()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	done := make(chan error, 1)
	go func() {
		again, _, err := f.Submit(event)
		if err == nil && !reflect.DeepEqual(again, original) {
			err = errors.New("replay acceptance changed")
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("exact replay waited for native execution")
	}
	after, err := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
	if err != nil || string(after) != string(before) {
		t.Fatal("exact replay wrote state")
	}
}
