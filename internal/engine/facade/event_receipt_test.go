package facade

import (
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAcceptanceIncludesCommittedRefill(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	id, _ := d.action("engine.development.batch.v3")
	d.receipt(id)
	event := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: "TRANSIENT_ENGINE_ERROR", PayloadDigest: "interrupted development"}}
	got := d.submit(event)
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	stored := snapshot.State.Events[string(event.ID)].Acceptance
	before, err := os.ReadFile(filepath.Join(d.f.stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	d.reopen()
	replay := d.submit(event)
	after, err := os.ReadFile(filepath.Join(d.f.stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("event replay rewrote active state")
	}
	if len(got.Refill) != 1 {
		t.Fatalf("expected real finite retry refill: %+v", got)
	}
	if !reflect.DeepEqual(got, stored) || !reflect.DeepEqual(got, replay) {
		t.Fatal("committed event lost stable refill acceptance")
	}
}
