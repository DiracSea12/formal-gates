package testkit

import (
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/protocol"
)

func TestRecoveryReplacesAttemptWhenWorkspaceSourceChanges(t *testing.T) {
	fixture, action := fixtureWithAction(t)
	if _, err := fixture.Host.SpawnEvent("source-spawn", action, "source-agent", protocol.SpawnStatusSpawned); err != nil {
		t.Fatal(err)
	}
	start, err := protocol.NewLifecycleEventEvent("source-start", "fake-host", "source-agent", protocol.LifecycleStart)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := submit(fixture, start); err != nil {
		t.Fatal(err)
	}
	before, err := fixture.Engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	pending := before.State.PendingActions[action]
	attempt := before.State.Attempts[pending.Task]
	if err := fixture.VCS.Write("input.txt", []byte("changed workspace source\n")); err != nil {
		t.Fatal(err)
	}
	input, err := fixture.VCS.Observe()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := input.Facts.Digest()
	if err != nil || digest == attempt.Bindings.Snapshot {
		t.Fatalf("workspace edit did not change its observed source: %s, err=%v", digest, err)
	}
	plan, _, err := fixture.Engine.RecoverAttempt(action, protocol.Interruption{Class: authoring.FailureTransientEngine}, input)
	if err != nil || plan.Action != protocol.RecoveryNewAttempt {
		t.Fatalf("changed workspace recovery = %+v, err=%v", plan, err)
	}
	after, err := fixture.Engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, intent := range after.State.PendingHostActions {
		if intent.Correlation == plan.RequestID && intent.Terminate != nil && intent.Terminate.WorkerActionID == action && intent.Terminate.AttemptID == attempt.ID {
			return
		}
	}
	t.Fatal("changed source did not reconcile the original native execution before replacement")
}
