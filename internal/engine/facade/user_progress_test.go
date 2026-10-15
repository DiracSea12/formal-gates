package facade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/protocol"
)

func workerNumberForAction(run Run, action string) int {
	ids := []string{}
	for id := range run.WorkerMaterials {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for index, id := range ids {
		if id == action {
			return index + 1
		}
	}
	return 0
}
func TestSupplementaryMessageKeepsOriginalActionAndUncertainDeliveryAcrossReopen(t *testing.T) {
	t.Parallel()
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
			text := "Keep the current work.\n原文和空格  "
			message, err := d.f.QueueWorkerMessage(workerNumberForAction(d.run, action), text)
			if err != nil {
				t.Fatal(err)
			}
			snap, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if message.ActionID != action || message.AttemptID != before.State.PendingActions[action].AttemptID || message.Identity != identity || message.Text != text || message.Status != "PENDING" || !reflect.DeepEqual(snap.State.TaskMessages[message.ID], message) || !reflect.DeepEqual(before.State.Attempts, snap.State.Attempts) || !reflect.DeepEqual(before.State.PendingActions, snap.State.PendingActions) || before.State.Guard.UsedRequests != snap.State.Guard.UsedRequests || before.State.Guard.UsedRetries != snap.State.Guard.UsedRetries {
				t.Fatal("message changed execution identity or budget")
			}
			if _, err := d.f.engine.ClaimTaskMessage(action, message.AttemptID, "wrong-native", protocol.Observation{}, false); err == nil {
				t.Fatal("another identity claimed user text")
			}
			if status == protocol.SpawnStatusAttached {
				if _, err := d.f.engine.ClaimTaskMessage(action, message.AttemptID, before.State.SpawnReceipts[action].Correlation, protocol.Observation{}, false); err == nil {
					t.Fatal("transport correlation claimed text intended for the observed native identity")
				}
			}
			claimed, err := d.f.engine.ClaimTaskMessage(action, message.AttemptID, identity, protocol.Observation{}, false)
			if err != nil || claimed == nil || claimed.Status != "SENDING" || claimed.ActionID != action || claimed.Identity != identity || claimed.AttemptID != message.AttemptID || claimed.Text != text {
				t.Fatalf("claim=%+v err=%v", claimed, err)
			}
			reopened, err := Open(d.f.root, d.f.runID)
			if err != nil {
				t.Fatal(err)
			}
			pending, err := reopened.engine.ClaimTaskMessage(action, message.AttemptID, identity, protocol.Observation{}, true)
			if err != nil || pending != nil {
				t.Fatalf("uncertain delivery resent: %+v %v", pending, err)
			}
			current, err := reopened.Show()
			if err != nil {
				t.Fatal(err)
			}
			failed := current.TaskMessages[message.ID]
			if failed.Status != "FAILED" || !strings.Contains(failed.Reason, "outcome unknown") || failed.Text != text {
				t.Fatalf("uncertain delivery lost: %+v", failed)
			}
			second, err := reopened.QueueWorkerMessage(workerNumberForAction(current, action), "one additional instruction")
			if err != nil {
				t.Fatal(err)
			}
			sent, err := reopened.engine.ClaimTaskMessage(action, second.AttemptID, identity, protocol.Observation{}, false)
			if err != nil || sent == nil || sent.Identity != identity || sent.ActionID != action || sent.AttemptID != message.AttemptID || sent.Text != second.Text {
				t.Fatalf("second original identity delivery=%+v err=%v", sent, err)
			}
			sent.Status = "ACCEPTED"
			if err := reopened.engine.RecordTaskMessage(*sent, protocol.Observation{}); err != nil {
				t.Fatal(err)
			}
			settled, err := reopened.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if err := reopened.engine.RecordTaskMessage(*sent, protocol.Observation{}); err != nil {
				t.Fatal(err)
			}
			replayed, err := reopened.engine.Load()
			if err != nil || replayed.Revision != settled.Revision {
				t.Fatal("receipt replay changed state")
			}
			late := *sent
			late.Status = "FAILED"
			late.Reason = "native result unavailable; delivery outcome unknown; not automatically resent"
			if err := reopened.engine.RecordTaskMessage(late, protocol.Observation{}); err != nil {
				t.Fatal(err)
			}
			replayed, err = reopened.engine.Load()
			if err != nil || replayed.Revision != settled.Revision || replayed.State.TaskMessages[sent.ID] != *sent {
				t.Fatal("later invocation failure revoked accepted delivery")
			}
		})
	}
}

func TestCancellationKeepsInFlightMessageDeliveryOwner(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"accepted", "failed", "interrupted"} {
		t.Run(outcome, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox")
			action, _ := d.action("engine.development.batch.v6")
			d.receipt(action)
			started, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), action, protocol.LifecycleStart)
			if err != nil {
				t.Fatal(err)
			}
			d.submit(started)
			text := "Preserve the original instruction.\n原文和空格  "
			message, err := d.f.QueueWorkerMessage(workerNumberForAction(d.run, action), text)
			if err != nil {
				t.Fatal(err)
			}
			claimed, err := d.f.engine.ClaimTaskMessage(action, message.AttemptID, action, protocol.Observation{}, false)
			if err != nil || claimed == nil || claimed.Status != "SENDING" {
				t.Fatalf("claim=%+v err=%v", claimed, err)
			}
			queued, err := d.f.QueueWorkerMessage(workerNumberForAction(d.run, action), "Still queued")
			if err != nil {
				t.Fatal(err)
			}
			stopped, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), action, protocol.LifecycleStop)
			if err != nil {
				t.Fatal(err)
			}
			d.submit(stopped)
			request, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Abort"})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(request)
			token, err := d.f.Freshness(string(request.ID))
			if err != nil {
				t.Fatal(err)
			}
			decision, err := protocol.NewDecideEvent(d.eventID(), protocol.RequestID(request.ID), token, "abort")
			if err != nil {
				t.Fatal(err)
			}
			d.submit(decision)
			d.reopen()
			snapshot, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if _, active := snapshot.State.PendingActions[action]; active || snapshot.State.TaskMessages[message.ID] != *claimed || snapshot.State.TaskMessages[queued.ID].Status != "FAILED" {
				t.Fatal("cancellation lost the in-flight owner or kept queued text deliverable")
			}
			if remaining, err := d.f.engine.ClaimTaskMessage(action, message.AttemptID, action, protocol.Observation{}, false); err != nil || remaining != nil {
				t.Fatalf("closed action consumed queued or in-flight text: %+v %v", remaining, err)
			}
			if outcome == "interrupted" {
				if remaining, err := d.f.engine.ClaimTaskMessage(action, message.AttemptID, action, protocol.Observation{}, true); err != nil || remaining != nil {
					t.Fatalf("uncertain delivery was resent: %+v %v", remaining, err)
				}
				snapshot, err = d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				unknown := snapshot.State.TaskMessages[message.ID]
				if unknown.Status != "FAILED" || !strings.Contains(unknown.Reason, "outcome unknown") || unknown.Text != text || unknown.ActionID != message.ActionID || unknown.AttemptID != message.AttemptID || unknown.Identity != message.Identity {
					t.Fatalf("uncertain original delivery lost its evidence: %+v", unknown)
				}
				return
			}
			receipt := *claimed
			receipt.Status = "ACCEPTED"
			if outcome == "failed" {
				receipt.Status = "FAILED"
				receipt.Reason = "native turn rejected the supplementary message"
			}
			wrong := receipt
			wrong.Identity = "another-native-session"
			if err := d.f.engine.RecordTaskMessage(wrong, protocol.Observation{}); err == nil {
				t.Fatal("retired delivery accepted another native session")
			}
			if err := d.f.engine.RecordTaskMessage(receipt, protocol.Observation{}); err != nil {
				t.Fatal(err)
			}
			settled, err := d.f.engine.Load()
			if err != nil || settled.State.TaskMessages[message.ID] != receipt || settled.State.TaskMessages[queued.ID].Status != "FAILED" {
				t.Fatalf("actual delivery outcome differs from original receipt: %+v %v", settled.State.TaskMessages, err)
			}
			if err := d.f.engine.RecordTaskMessage(receipt, protocol.Observation{}); err != nil {
				t.Fatal(err)
			}
			repeated, err := d.f.engine.Load()
			if err != nil || repeated.Revision != settled.Revision {
				t.Fatal("receipt replay changed state")
			}
		})
	}
}

func TestProgressUsesActualProcessesAndCursorWithoutCallingOrWriting(t *testing.T) {
	t.Parallel()
	d := newValidationDriver(t, "blackbox")
	action, _ := d.action("engine.development.batch.v6")
	initial, err := d.f.ProgressSince(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Running != 0 || initial.NotStarted == 0 || initial.ElapsedMillis == nil {
		t.Fatalf("issued-but-not-started progress=%+v", initial)
	}
	// A real observed native process is distinct from a queued material.
	directory := filepath.Join(workerActionDirectory(d.f.stateDir, action), "original-call")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	process := map[string]any{"pid": os.Getpid(), "started_at": float64(time.Now().Add(-time.Second).UnixNano()) / 1e9}
	raw, _ := json.Marshal(process)
	os.WriteFile(filepath.Join(directory, "process.json"), raw, 0600)
	raw, _ = json.Marshal(map[string]any{"pid": os.Getpid(), "pgid": os.Getpid()})
	os.WriteFile(filepath.Join(directory, "native-process.json"), raw, 0600)
	before, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	running, err := d.f.ProgressSince(before.Revision, func(path string) bool { return path == directory })
	if err != nil {
		t.Fatal(err)
	}
	if running.Running != 1 || running.NativeCalls != 1 || len(running.NewResults) != 0 || len(running.Current) != 1 || running.Current[0].DurationMillis == nil {
		t.Fatalf("live progress=%+v", running)
	}
	raw, _ = json.Marshal(map[string]any{"exit_code": 130, "error": "owned job was cancelled", "finished_at": float64(time.Now().UnixNano()) / 1e9})
	os.WriteFile(filepath.Join(directory, "exit.json"), raw, 0600)
	stopped, err := d.f.ProgressSince(before.Revision, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Running != 0 || stopped.Completed != initial.Completed || stopped.Blocked[action] != "owned job was cancelled" {
		t.Fatalf("cancelled progress=%+v", stopped)
	}
	latest := filepath.Join(workerActionDirectory(d.f.stateDir, action), "z-continuation")
	if err := os.MkdirAll(latest, 0700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]map[string]any{
		"native-process.json": {"pid": os.Getpid(), "pgid": os.Getpid()},
		"process.json":        {"pid": os.Getpid(), "started_at": float64(time.Now().Add(-500*time.Millisecond).UnixNano()) / 1e9},
		"exit.json":           {"exit_code": 0, "error": "", "finished_at": float64(time.Now().UnixNano()) / 1e9},
	} {
		raw, _ := json.Marshal(value)
		if err := os.WriteFile(filepath.Join(latest, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	repeated, err := d.f.ProgressSince(before.Revision, func(string) bool { return false })
	if err != nil || repeated.NativeCalls != 2 || len(repeated.Current) != 1 || repeated.Current[0].Evidence != latest || repeated.Current[0].DurationMillis == nil || len(repeated.RepeatedExecutions) != 1 || repeated.RepeatedExecutions[0].Calls != 2 {
		t.Fatalf("latest stopped invocation progress=%+v %v", repeated, err)
	}
	after, err := d.f.engine.Load()
	if err != nil || after.Revision != before.Revision {
		t.Fatal("progress read mutated workflow")
	}
}

// Incremental results are the original accepted worker results after the
// cursor, in acceptance-revision order; reads across cursor positions and
// reopen never write state.
func TestProgressReportsRevisionIncrementalWorkerResults(t *testing.T) {
	t.Parallel()
	d := newValidationDriver(t, "blackbox")
	design, designInput := d.action("engine.qa.design.v6")
	development, developmentInput := d.action("engine.development.batch.v6")
	before, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	d.receipt(design)
	manifest := simpleManifest(designInput)
	first := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: design, Provider: d.host(), Outcome: protocol.OutcomePass, PayloadDigest: string(d.eventID()), Validation: &protocol.ValidationResult{Binding: designInput.Binding, Status: "PASS", Manifest: &manifest}}})
	d.drive()
	d.receipt(development)
	second := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: development, Provider: d.host(), Outcome: protocol.OutcomePass, PayloadDigest: string(d.eventID()), Validation: &protocol.ValidationResult{Binding: developmentInput.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: batchEvidence(developmentInput).Completion, Paths: []string{"app.go"}, Summary: "implemented input checks"}}}})
	d.drive()
	settled, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "ACCEPTED" || second.Status != "ACCEPTED" || first.Revision >= second.Revision {
		t.Fatalf("worker results=%+v/%+v", first, second)
	}
	progress, err := d.f.ProgressSince(before.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []ResultProgress{{ActionID: design, Revision: first.Revision, Status: "ACCEPTED"}, {ActionID: development, Revision: second.Revision, Status: "ACCEPTED"}}
	if !reflect.DeepEqual(progress.NewResults, want) || progress.Revision != settled.Revision {
		t.Fatalf("incremental results=%+v want=%+v", progress.NewResults, want)
	}
	tail, err := d.f.ProgressSince(first.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail.NewResults) != 1 || tail.NewResults[0] != want[1] {
		t.Fatalf("middle cursor results=%+v want=%+v", tail.NewResults, want[1])
	}
	current, err := d.f.ProgressSince(progress.Revision, nil)
	if err != nil || len(current.NewResults) != 0 {
		t.Fatalf("current revision results=%+v err=%v", current.NewResults, err)
	}
	if _, err := d.f.ProgressSince(progress.Revision+1, nil); err == nil || !strings.Contains(err.Error(), "exceeds current revision") {
		t.Fatalf("cursor beyond current revision=%v", err)
	}
	d.reopen()
	reopened, err := d.f.ProgressSince(before.Revision, nil)
	if err != nil || !reflect.DeepEqual(reopened.NewResults, want) {
		t.Fatalf("reopened results=%+v want=%+v err=%v", reopened.NewResults, want, err)
	}
	after, err := d.f.engine.Load()
	if err != nil || !reflect.DeepEqual(settled, after) {
		t.Fatal("progress reads mutated workflow")
	}
}

// observeWorkerDispatch uses public receipt/lifecycle producers. An attached
// receipt resolves the transport key to a distinct observed native identity.
func observeWorkerDispatch(d *validationDriver, action, status string) string {
	d.t.Helper()
	identity := action + "/native-worker"
	correlation := identity
	initialStatus := status
	if status == protocol.SpawnStatusAttached {
		correlation = action + "/transport"
		initialStatus = protocol.SpawnStatusUnknown
	}
	event, err := protocol.NewSpawnReceiptEvent(d.eventID(), action, d.host(), correlation, initialStatus)
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(event)
	if status == protocol.SpawnStatusAttached {
		for _, edge := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
			event, err := protocol.NewCorrelatedLifecycleEvent(d.eventID(), d.host(), correlation, identity, edge)
			if err != nil {
				d.t.Fatal(err)
			}
			d.submit(event)
		}
	}
	snapshot, err := d.f.engine.Load()
	if err != nil {
		d.t.Fatal(err)
	}
	receipt := snapshot.State.SpawnReceipts[action]
	input, current := snapshot.State.WorkerInput(action, d.f.definition)
	if receipt.Status != status || receipt.Provider != d.host() || receipt.Correlation != correlation || !current || input.Pending.AttemptID != snapshot.State.PendingActions[action].AttemptID || status == protocol.SpawnStatusAttached && receipt.LifecycleIdentity != identity {
		d.t.Fatalf("observed dispatch differs from original execution: receipt=%+v input=%+v current=%t", receipt, input, current)
	}
	return identity
}

func TestProgressRespectsObservedSpawnIdentity(t *testing.T) {
	t.Parallel()
	for _, status := range []string{protocol.SpawnStatusUnknown, protocol.SpawnStatusSpawned, protocol.SpawnStatusAttached} {
		t.Run(status, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox")
			action, _ := d.action("engine.development.batch.v6")
			if status == protocol.SpawnStatusUnknown {
				event, err := protocol.NewSpawnReceiptEvent(d.eventID(), action, d.host(), action+"/transport", status)
				if err != nil {
					t.Fatal(err)
				}
				d.submit(event)
			} else {
				observeWorkerDispatch(d, action, status)
			}
			d.reopen()
			before, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			progress, err := d.f.ProgressSince(before.Revision, nil)
			if err != nil {
				t.Fatal(err)
			}
			wantBlocked := ""
			if status == protocol.SpawnStatusUnknown {
				wantBlocked = protocol.SpawnStatusUnknown + ": native start not established"
			}
			if progress.Blocked[action] != wantBlocked || progress.Running != 0 || progress.NativeCalls != 0 || progress.NotStarted == 0 || len(progress.NewResults) != 0 {
				t.Fatalf("receipt without process evidence misreported: %+v", progress)
			}
			if status == protocol.SpawnStatusUnknown {
				if _, err := d.f.QueueWorkerMessage(workerNumberForAction(d.run, action), "retain the original worker"); err == nil || !strings.Contains(err.Error(), "MESSAGE_BINDING") {
					t.Fatalf("unknown dispatch identity accepted user text: %v", err)
				}
			}
			after, err := d.f.engine.Load()
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("read-only progress or rejected message changed state: %v", err)
			}
		})
	}
}
