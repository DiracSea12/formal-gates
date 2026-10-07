package facade

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"formal-gates/internal/prompts"
	"net/url"
	"os"
	"path/filepath"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/host"
)

// WorkerMaterial is a thin pointer to immutable evidence for an issued action.
// Input and completion authority remain in protocol.State, not this file.
type WorkerMaterial struct {
	ActionID  string `json:"actionId"`
	AttemptID string `json:"attemptId"`
	Path      string `json:"path"`
	Digest    string `json:"digest"`
	ReadOnly  bool   `json:"readOnly"`
}

func (f *Facade) workerMaterials(s *protocol.State, write bool) (map[string]WorkerMaterial, error) {
	refs := map[string]WorkerMaterial{}
	inputs := s.WorkerInputs(f.definition)
	if len(inputs) == 0 {
		return refs, nil
	}
	catalog, err := prompts.LoadPromptCatalog(f.admission.Target)
	if err != nil {
		return nil, err
	}
	for id, input := range inputs {
		material, err := worker.Render(input, catalog)
		if err != nil {
			return nil, err
		}
		body, err := json.MarshalIndent(material, "", "  ")
		if err != nil {
			return nil, err
		}
		body = append(body, '\n')
		sum := sha256.Sum256(body)
		digest := hex.EncodeToString(sum[:])
		// Content identity allows carried work to read current confirmed context
		// without overwriting the input bytes of an already executing invocation.
		path := filepath.Join(f.stateDir, "materials", url.QueryEscape(id), digest+".json")
		refs[id] = WorkerMaterial{ActionID: id, AttemptID: input.Pending.AttemptID, Path: path, Digest: "sha256:" + digest, ReadOnly: material.ReadOnly}
		if !write {
			continue
		}
		existing, err := os.ReadFile(path)
		if err == nil {
			if !bytes.Equal(existing, body) {
				return nil, fmt.Errorf("worker material bytes differ from their content identity")
			}
			continue
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := f.admission.write(func() error {
			return writeFileAtomic(path, body, 0600)
		}); err != nil {
			return nil, err
		}
	}
	return refs, nil
}

// registerIntakeRequirement uses the same typed requirement admission as
// externally observed confirmations. It never confirms different file bytes.
func (f *Facade) registerIntakeRequirement() error {
	snapshot, err := f.engine.Load()
	if err != nil {
		return err
	}
	s := snapshot.State
	if s.Route != "regular" || s.RequirementGuarantee == nil || s.Validation != nil || s.IntakeConfirmationReceipt == nil || s.ResetPending() {
		return nil
	}
	receipt := s.IntakeConfirmationReceipt
	data, err := os.ReadFile(filepath.Join(f.root, receipt.RequirementSource))
	if err != nil {
		return err
	}
	event := protocol.Event{ID: protocol.EventID("intake.requirement:" + receipt.RequirementRevision), Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: receipt.RequirementRevision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: receipt.RequirementRevision, Document: string(data)}}}
	_, err = f.engine.Submit(event, emptyFingerprint())
	return err
}

// SubmitWorkerResult consumes the source-verified native answer and uses the
// existing Submit transaction. The original spawn correlation owns session
// correlation; native source verifies the separate host session identity.
func (f *Facade) SubmitWorkerResult(eventID protocol.EventID, material WorkerMaterial, directory string, source map[string]any, verifyNative func(string, string, map[string]any) ([]byte, string, string, error)) (protocol.Acceptance, Run, error) {
	if verifyNative == nil {
		return protocol.Acceptance{}, Run{}, fmt.Errorf("native source verification is required")
	}
	body, err := os.ReadFile(material.Path)
	if err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	sum := sha256.Sum256(body)
	if material.Digest != "sha256:"+hex.EncodeToString(sum[:]) {
		return protocol.Acceptance{}, Run{}, fmt.Errorf("worker material proof differs")
	}
	var value worker.Material
	if err := json.Unmarshal(body, &value); err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	if value.Input.Pending.ActionID != material.ActionID || value.Input.Pending.AttemptID != material.AttemptID || value.Input.RunID != f.runID {
		return protocol.Acceptance{}, Run{}, fmt.Errorf("worker material identity differs from its issued reference or run")
	}
	provider := value.Input.Provider
	if descriptor, lookupErr := host.Lookup(provider); lookupErr == nil {
		provider = descriptor.InstallName
	}
	requestBytes, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	var request struct {
		Prompt    string `json:"prompt"`
		Workspace string `json:"workspace"`
		ReadOnly  bool   `json:"read_only"`
	}
	if err := json.Unmarshal(requestBytes, &request); err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	workspace := f.root
	if value.Input.Validation != nil && value.Input.Validation.Workspace != nil {
		workspace = value.Input.Validation.Workspace.Path
	}
	if request.Prompt != value.Prompt || filepath.Clean(request.Workspace) != filepath.Clean(workspace) || request.ReadOnly != value.ReadOnly {
		return protocol.Acceptance{}, Run{}, fmt.Errorf("native invocation differs from issued worker material")
	}
	answer, digest, nativeSession, err := verifyNative(provider, directory, source)
	if err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	if nativeSession == "" {
		return protocol.Acceptance{}, Run{}, fmt.Errorf("native result session proof is missing")
	}
	event, err := worker.DecodeResult(value.Input, eventID, answer, digest)
	if err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	// The verified original request's exact prompt carries run/action/attempt.
	// Submit alone owns current, terminal, duplicate and obsolete-result admission.
	// The native session proof is distinct from the ledger's spawn correlation.
	return f.Submit(event)
}
