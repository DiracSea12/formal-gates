package facade

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/protocol"
)

func submitRejectedGuaranteePrerequisite(t *testing.T, f *Facade, q protocol.RegularRequest) {
	t.Helper()
	beforeBytes, err := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	ev, err := protocol.NewRegularEvent("phase4-guarantee-prerequisite", protocol.RegularEvent{
		Operation:        "REQUEST",
		ExpectedRevision: before.Revision,
		Request:          &q,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.Submit(ev); err == nil {
		t.Fatal("route request with missing guarantee preconditions was accepted")
	}
	afterBytes, err := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeBytes, afterBytes) {
		t.Fatal("rejected guarantee precondition wrote state bytes")
	}
	if after.Revision != before.Revision {
		t.Fatalf("rejected guarantee precondition advanced revision %d -> %d", before.Revision, after.Revision)
	}
	if after.Regular == nil || after.Regular.Guarantee.Status != "INACTIVE" {
		t.Fatalf("rejected guarantee precondition changed activation: %+v", after.Regular)
	}
}

func TestPhase4GuaranteeActivationMissingPreconditionsIsZeroWrite(t *testing.T) {
	t.Run("unconfirmed-intake", func(t *testing.T) {
		f := newRegularFacade(t)
		submitRejectedGuaranteePrerequisite(t, f, protocol.RegularRequest{
			Action: "route",
			Route:  &protocol.RegularRoute{Kind: "custom", QA: []string{"blackbox"}, Gates: []string{"implementation-quality-gate"}},
			Reason: "normal route request before intake confirmation",
		})
	})

	t.Run("confirmed-without-topology", func(t *testing.T) {
		f := newRegularFacade(t)
		facadeChoice(t, f, protocol.RegularRequest{Action: "confirm-requirement"})
		facadeChoice(t, f, protocol.RegularRequest{Action: "mode", Mode: "regular"})
		submitRejectedGuaranteePrerequisite(t, f, protocol.RegularRequest{
			Action: "route",
			Route:  &protocol.RegularRoute{Kind: "custom", QA: []string{"blackbox"}, Gates: []string{"implementation-quality-gate"}},
			Reason: "normal route request before the no-split topology confirmation",
		})
	})
}
