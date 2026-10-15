package facade

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
)

// Cost unit fixtures preserve the managed request/material/source carriers and
// verify the original transcript bytes; provider protocol qualification belongs
// to the harness tests using NativeResultEvidence.
func init() {
	NativeSourceVerifier = func(provider, directory string, source map[string]any) ([]byte, string, string, error) {
		path, _ := source["transcript"].(string)
		identity, _ := source["identity"].(string)
		if source["provider"] != provider || identity == "" {
			return nil, "", "", fmt.Errorf("cost fixture provider/identity differs")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, "", "", err
		}
		if source["sha256"] != fmt.Sprintf("%x", sha256.Sum256(raw)) {
			return nil, "", "", fmt.Errorf("cost fixture transcript differs")
		}
		return raw, "", identity, nil
	}
}

func observeMeteredStart(t *testing.T, root, provider, identity string) {
	t.Helper()
	binding := meteredBinding(t, root, provider, identity)
	if err := lifecycle.ObserveNativeStart(root, binding.RunID, binding.DispatchID, provider, identity); err != nil {
		t.Fatal(err)
	}
}

type meteredDispatchBinding struct{ RunID, DispatchID, Provider, Identity string }

func meteredBinding(t *testing.T, root, provider, identity string) meteredDispatchBinding {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, ".gates", "tmp", "*", "lifecycle", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var binding meteredDispatchBinding
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var value meteredDispatchBinding
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		if value.Provider == provider && value.Identity == identity {
			if binding.DispatchID != "" {
				t.Fatal("ambiguous fixture dispatch")
			}
			binding = value
		}
	}
	if binding.DispatchID == "" {
		t.Fatalf("missing fixture dispatch: %s %s", provider, identity)
	}
	return binding
}

func completeMeteredSource(t *testing.T, root, provider, identity, transcript string) {
	t.Helper()
	binding := meteredBinding(t, root, provider, identity)
	writeManagedCostSource(t, root, binding.RunID, binding.DispatchID, provider, identity, transcript)
	if err := lifecycle.ObserveNativeCompletion(root, binding.RunID, binding.DispatchID, provider, identity); err != nil {
		t.Fatal(err)
	}
}

func writeManagedCostSource(t *testing.T, root, runID, action, provider, identity, transcript string) {
	t.Helper()
	stateDir := filepath.Join(root, EngineNamespace, runID)
	paths, err := filepath.Glob(filepath.Join(stateDir, "materials", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var material WorkerMaterial
	var value worker.Material
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		value = worker.Material{}
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		if value.Input.Pending.ActionID != action {
			continue
		}
		material = WorkerMaterial{ActionID: action, AttemptID: value.Input.Pending.AttemptID, ReadOnly: value.ReadOnly, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(raw)), Path: path}
		break
	}
	if material.Path == "" {
		value = worker.Material{ReadOnly: true, Input: protocol.WorkerInput{RunID: runID, Provider: provider, Pending: protocol.PendingAction{ActionID: action, AttemptID: "attempt"}}}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		material = WorkerMaterial{ActionID: action, AttemptID: "attempt", ReadOnly: true, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(raw)), Path: filepath.Join(stateDir, "materials", "cost.json")}
		if err := os.MkdirAll(filepath.Dir(material.Path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(material.Path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	directory := WorkerExecutionDirectory(material, "")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	descriptor, err := host.Lookup(provider)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, value any) {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	workspace := root
	if value.Input.Validation != nil && value.Input.Validation.Workspace != nil {
		workspace = value.Input.Validation.Workspace.Path
	}
	write("native-request.json", map[string]any{"provider": descriptor.InstallName, "workspace": workspace, "prompt": value.Prompt, "read_only": value.ReadOnly, "schema": value.ResultSchema, "engine": map[string]any{"root": root, "runId": runID, "workerMaterial": material, "resumeActionId": ""}})
	if err := os.WriteFile(filepath.Join(directory, "owner"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(transcript)
	if err != nil {
		t.Fatal(err)
	}
	write("source.json", map[string]any{"provider": descriptor.InstallName, "identity": identity, "transcript": transcript, "sha256": fmt.Sprintf("%x", sha256.Sum256(raw))})
}
