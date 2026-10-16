package protocol

import (
	"bytes"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
	"testing"
)

func TestInternalStepRequirementBarrierStopsExecution(t *testing.T) {
	t.Parallel()
	compiled, registry := extensionDefinition(t)
	bindings := extensionHandlers()
	dir := t.TempDir()
	engine, err := New(mustStore(t, dir), Config{Definition: compiled, Registry: registry, InternalSteps: bindings})
	if err != nil {
		t.Fatal(err)
	}
	view, err := decision.NewState("1", runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.InitWithMetadata(view, testProvider, Observation{}, "", "", nil, "", "", nil); err != nil {
		t.Fatal(err)
	}
	request, err := NewRequestEvent("begin-change", ControlRequestRequirementChange, requirementOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = engine.Submit(request, Observation{}); err != nil {
		t.Fatal(err)
	}
	before, err := engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !before.State.requirementChangePending() {
		t.Fatal("barrier was not established")
	}
	plan, _, err := engine.Drive(Observation{})
	if err != nil {
		t.Fatal(err)
	}
	after, err := engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("before completed=%v records=%v revision=%v; after completed=%v records=%v revision=%v plan=%v barrier=%v", before.State.Completed, before.State.RecoveryRecords, before.Revision, after.State.Completed, after.State.RecoveryRecords, after.Revision, plan.Next.Kind, after.State.requirementChangePending())
	if len(after.State.RecoveryRecords) != len(before.State.RecoveryRecords) || len(after.State.Completed) != len(before.State.Completed) {
		t.Fatal("Drive executed business handlers through the active requirement-change barrier")
	}
	// The same pending ask survives a Store reopen without business writes.
	frozen := stateBytesOf(t, dir)
	reopened, err := New(mustStore(t, dir), engine.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(frozen, stateBytesOf(t, dir)) {
		t.Fatal("pending barrier changed persisted state on reopen")
	}
	// Cancelling the requirement change restores the existing frontier.
	abortExistingAsk(t, reopened, "begin-change")
	if _, _, err := reopened.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	resumed, err := reopened.Load()
	if err != nil {
		t.Fatal(err)
	}
	if resumed.State.requirementChangePending() || len(resumed.State.RecoveryRecords) != 2 || len(resumed.State.Completed) != 2 {
		t.Fatalf("frontier did not resume: %+v", resumed.State)
	}
	complete := stateBytesOf(t, dir)
	if _, _, err := reopened.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(complete, stateBytesOf(t, dir)) {
		t.Fatal("repeat Drive changed completed frontier")
	}

}

func TestInternalStepAcceptedAbortStopsExecution(t *testing.T) {
	t.Parallel()
	compiled, registry := extensionDefinition(t)
	dir := t.TempDir()
	engine, err := New(mustStore(t, dir), Config{Definition: compiled, Registry: registry, InternalSteps: extensionHandlers()})
	if err != nil {
		t.Fatal(err)
	}
	view, err := decision.NewState("1", runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.InitWithMetadata(view, testProvider, Observation{}, "", "", nil, "", "", nil); err != nil {
		t.Fatal(err)
	}
	request, err := NewRequestEvent("stop", ControlAbort, AskOption{ID: "abort", Label: "Abort run"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = engine.Submit(request, Observation{}); err != nil {
		t.Fatal(err)
	}
	token, err := engine.Freshness("stop")
	if err != nil {
		t.Fatal(err)
	}
	ev, err := NewDecideEvent("confirm-stop", "stop", token, "abort")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = engine.Submit(ev, Observation{}); err != nil {
		t.Fatal(err)
	}
	after, err := engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("after completed=%v records=%v revision=%v phase=%v abort=%v", after.State.Completed, after.State.RecoveryRecords, after.Revision, after.State.Phase, after.State.abortRequested())
	if len(after.State.RecoveryRecords) != 0 || len(after.State.Completed) != 0 {
		t.Fatal("Accepting abort executed ordinary business handlers")
	}
	terminal := stateBytesOf(t, dir)
	reopened, err := New(mustStore(t, dir), engine.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(terminal, stateBytesOf(t, dir)) {
		t.Fatal("aborted terminal frontier ran after reopen")
	}

}
