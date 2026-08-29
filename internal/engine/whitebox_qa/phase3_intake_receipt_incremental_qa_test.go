package whitebox_qa

import (
	"bytes"
	"testing"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
)

// TestPhase3ProtocolRejectsFirstIntakeReceiptBindingMismatchBeforeWrite
// covers the protocol-side boundary that is distinct from façade start
// validation: a first-drive receipt must reproduce every typed confirmation
// binding captured during InitWithMetadata.  Each mismatch is made internally
// self-consistent by recomputing its digest, so the rejection is specifically
// attributable to the start-confirmation comparison rather than digest
// parsing.  The revision, bytes, and receipt pointer must remain unchanged.
func TestPhase3ProtocolRejectsFirstIntakeReceiptBindingMismatchBeforeWrite(t *testing.T) {
	baseConfirmation := facade.IntakeConfirmationReceipt{
		Source:              facade.DefaultIntakeSource,
		Authority:           facade.DefaultIntakeAuthority,
		Transport:           facade.DefaultIntakeTransport,
		RequirementSource:   "requirements.md",
		RequirementRevision: "req-r1",
		Artifacts: []facade.IntakeArtifact{
			{Path: "requirements.md", Revision: "req-r1"},
			{Path: "design.md", Revision: "sol-r1"},
		},
		SolutionRevision: "sol-r1",
		SolutionDigest:   "sha256:solution-r1",
	}

	mismatches := []struct {
		name   string
		mutate func(*facade.IntakeConfirmationReceipt)
	}{
		{name: "source", mutate: func(receipt *facade.IntakeConfirmationReceipt) {
			receipt.Source = "other-source"
		}},
		{name: "authority", mutate: func(receipt *facade.IntakeConfirmationReceipt) {
			receipt.Authority = "other-authority"
		}},
		{name: "transport", mutate: func(receipt *facade.IntakeConfirmationReceipt) {
			receipt.Transport = "other-transport"
		}},
		{name: "requirement-source", mutate: func(receipt *facade.IntakeConfirmationReceipt) {
			receipt.RequirementSource = "other-requirements.md"
		}},
		{name: "requirement-revision", mutate: func(receipt *facade.IntakeConfirmationReceipt) {
			receipt.RequirementRevision = "req-r2"
		}},
		{name: "artifact-set", mutate: func(receipt *facade.IntakeConfirmationReceipt) {
			receipt.Artifacts = append(receipt.Artifacts, facade.IntakeArtifact{Path: "notes.md", Revision: "notes-r1"})
		}},
		{name: "solution-revision", mutate: func(receipt *facade.IntakeConfirmationReceipt) {
			receipt.SolutionRevision = "sol-r2"
		}},
		{name: "solution-digest", mutate: func(receipt *facade.IntakeConfirmationReceipt) {
			receipt.SolutionDigest = "sha256:other-solution"
		}},
	}

	for _, mismatch := range mismatches {
		t.Run(mismatch.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := persistence.NewStore(root, persistence.Config{PackageDigest: "sha256:phase3-package"})
			if err != nil {
				t.Fatalf("new store: %v", err)
			}
			compiled, err := compiler.Compile(definition.Workflow(), definition.Registry())
			if err != nil {
				t.Fatalf("compile definition: %v", err)
			}
			engine, err := protocol.New(store, protocol.Config{
				Definition: compiled,
				Registry:   definition.Registry(),
				Capacity:   0,
			}, nil)
			if err != nil {
				t.Fatalf("new protocol engine: %v", err)
			}
			view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
			if err != nil {
				t.Fatalf("new state: %v", err)
			}
			fingerprint, err := engine.ObserveFingerprint()
			if err != nil {
				t.Fatalf("observe initial fingerprint: %v", err)
			}
			confirmation := baseConfirmation
			confirmation.Artifacts = append([]facade.IntakeArtifact(nil), baseConfirmation.Artifacts...)
			if err := engine.InitWithMetadata(view, "engine", fingerprint, "receipt-binding", "lightweight", &confirmation); err != nil {
				t.Fatalf("init with confirmation: %v", err)
			}
			baseline, err := store.Load()
			if err != nil {
				t.Fatalf("load baseline: %v", err)
			}
			mismatchedConfirmation := confirmation
			mismatchedConfirmation.Artifacts = append([]facade.IntakeArtifact(nil), confirmation.Artifacts...)
			mismatch.mutate(&mismatchedConfirmation)
			digest, err := facade.IntakeDigest(mismatchedConfirmation)
			if err != nil {
				t.Fatalf("digest mismatched confirmation: %v", err)
			}
			receipt := protocol.IntakeReceipt{Confirmation: mismatchedConfirmation, IntakeDigest: digest}
			if _, err := engine.RecordIntakeReceipt(receipt, fingerprint); err == nil || !containsProtocolCode(err, protocol.CodeEventSchemaInvalid) {
				t.Fatalf("mismatched first receipt error = %v, want %s", err, protocol.CodeEventSchemaInvalid)
			}
			after, err := store.Load()
			if err != nil {
				t.Fatalf("load after rejection: %v", err)
			}
			if after.Revision != baseline.Revision || !bytes.Equal(after.Content, baseline.Content) {
				t.Fatal("mismatched first receipt changed revision or state bytes")
			}
			loaded, err := engine.Load()
			if err != nil {
				t.Fatalf("load protocol state after rejection: %v", err)
			}
			if loaded.State.IntakeReceipt != nil {
				t.Fatalf("rejected first receipt was persisted: %+v", loaded.State.IntakeReceipt)
			}
		})
	}
}

func containsProtocolCode(err error, code string) bool {
	return err != nil && len(code) > 0 && bytes.Contains([]byte(err.Error()), []byte(code))
}
