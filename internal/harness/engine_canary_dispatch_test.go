//go:build !windows

package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
)

// Accepted interruption evidence retains Pending without authorizing another call.
func TestCanaryInterruptedDispatchWaitsWithoutNativeEffects(t *testing.T) {
	var run facade.Run
	if err := json.Unmarshal(readTestFileBytes(t, filepath.Join("testdata", "interrupted_dispatch_projection.json")), &run); err != nil {
		t.Fatal(err)
	}
	for _, restored := range []bool{false, true} {
		name := "normal"
		if restored {
			name = "restored"
		}
		t.Run(name, func(t *testing.T) {
			current := run
			if restored {
				body, _ := json.Marshal(current)
				if err := json.Unmarshal(body, &current); err != nil {
					t.Fatal(err)
				}
			}
			recovery := current.RecoveryRecords[len(current.RecoveryRecords)-1]
			if recovery.Action != protocol.RecoveryAsk || recovery.Class != "USER_ACTION_REQUIRED" || current.Next.Kind != decision.KindWait {
				t.Fatalf("fixture lost accepted failure boundary: %+v", current.RecoveryRecords)
			}
			c := &engineCanary{t: t, jobs: t.TempDir(), run: current}
			nativeCalls := 0
			c.execute = func(any, string, bool) (map[string]any, error) {
				nativeCalls++
				t.Error("interrupted dispatch executed a native call")
				return nil, nil
			}
			id := recovery.ActionID
			dir := c.jobDirectory(id)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "native-events.jsonl")
			original := []byte("original native session evidence\n")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			active := map[string]bool{}
			completed := make(chan canaryCompletion, 1)
			if c.dispatchPending(active, completed) || nativeCalls != 0 || len(active) != 0 || len(c.dispatched) != 0 {
				t.Fatal("accepted failure was dispatched again")
			}
			body, err := os.ReadFile(path)
			if err != nil || string(body) != string(original) {
				t.Fatal("original evidence changed")
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 1 {
				t.Fatal("waiting created dispatch evidence")
			}
		})
	}
}

// Recovery records belong to an execution identity, not every obligation in its run.
func TestCanaryDispatchKeepsIndependentAndReplacementExecutions(t *testing.T) {
	var run facade.Run
	if err := json.Unmarshal(readTestFileBytes(t, filepath.Join("testdata", "interrupted_dispatch_projection.json")), &run); err != nil {
		t.Fatal(err)
	}
	original := run.PendingActions[run.RecoveryRecords[len(run.RecoveryRecords)-1].ActionID]
	for _, boundary := range []decision.Kind{decision.KindWait, decision.KindAsk} {
		run.Next.Kind = boundary
		if canaryDispatchable(run, original) {
			t.Fatal("interrupted original became dispatchable")
		}
		independent := original
		independent.Task.Node = "independent"
		independent.ActionID = "independent-action"
		independent.AttemptID = "independent-attempt"
		attempt := run.Attempts[original.Task]
		attempt.Task = independent.Task
		attempt.Bindings.Task = independent.Task
		attempt.ActionID, attempt.ID = independent.ActionID, independent.AttemptID
		run.Attempts[independent.Task] = attempt
		if !canaryDispatchable(run, independent) {
			t.Fatalf("%s blocked independent current execution", boundary)
		}
		// Engine replacement identities are distinct; the retained original record
		// remains evidence and cannot govern the replacement execution.
		replacement := original
		replacement.ActionID = "retry:" + original.ActionID + ":34"
		replacement.AttemptID = "att:" + original.Task.String() + ":34"
		attempt.Task = replacement.Task
		attempt.Bindings.Task = replacement.Task
		attempt.ActionID, attempt.ID = replacement.ActionID, replacement.AttemptID
		run.Attempts[replacement.Task] = attempt
		run.PendingActions = map[string]protocol.PendingAction{replacement.ActionID: replacement}
		if !canaryDispatchable(run, replacement) {
			t.Fatal("current replacement inherited the original failure")
		}
		if canaryDispatchable(run, original) {
			t.Fatal("stale original passed current attempt identity")
		}
	}
}
