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
	d := newValidationDriver(t, "blackbox")
	action, _ := d.action("engine.development.batch.v3")
	d.receipt(action)
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
	if message.Text != text || message.Status != "PENDING" || !reflect.DeepEqual(before.State.Attempts, snap.State.Attempts) || !reflect.DeepEqual(before.State.PendingActions, snap.State.PendingActions) || before.State.Guard.UsedRequests != snap.State.Guard.UsedRequests || before.State.Guard.UsedRetries != snap.State.Guard.UsedRetries {
		t.Fatal("message changed execution identity or budget")
	}
	if _, err := d.f.engine.ClaimTaskMessage(action, message.AttemptID, "wrong-native", emptyFingerprint(), false); err == nil {
		t.Fatal("another identity claimed user text")
	}
	claimed, err := d.f.engine.ClaimTaskMessage(action, message.AttemptID, action, emptyFingerprint(), false)
	if err != nil || claimed == nil || claimed.Status != "SENDING" {
		t.Fatalf("claim=%+v err=%v", claimed, err)
	}
	reopened, err := Open(d.f.root, d.f.runID)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := reopened.engine.ClaimTaskMessage(action, message.AttemptID, action, emptyFingerprint(), true)
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
	sent, err := reopened.engine.ClaimTaskMessage(action, second.AttemptID, action, emptyFingerprint(), false)
	if err != nil {
		t.Fatal(err)
	}
	sent.Status = "ACCEPTED"
	if err := reopened.engine.RecordTaskMessage(*sent, emptyFingerprint()); err != nil {
		t.Fatal(err)
	}
	settled, err := reopened.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.engine.RecordTaskMessage(*sent, emptyFingerprint()); err != nil {
		t.Fatal(err)
	}
	replayed, err := reopened.engine.Load()
	if err != nil || replayed.Revision != settled.Revision {
		t.Fatal("receipt replay changed state")
	}
}
func TestProgressUsesActualProcessesAndCursorWithoutCallingOrWriting(t *testing.T) {
	d := newValidationDriver(t, "blackbox")
	action, _ := d.action("engine.development.batch.v3")
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
	after, err := d.f.engine.Load()
	if err != nil || after.Revision != before.Revision {
		t.Fatal("progress read mutated workflow")
	}
}
