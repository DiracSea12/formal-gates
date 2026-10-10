package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
)

// A terminal projection may retain the slice registry after active checkpoints
// have retired. The foreground boundary must consume terminal authority first.
func TestWorkflowConsumerReturnsTerminalBeforeChildLookup(t *testing.T) {
	for _, status := range []string{"COMPLETE", "ABORTED"} {
		t.Run(status, func(t *testing.T) {
			root := t.TempDir()
			f, _, err := facade.Start(facade.StartOptions{Root: root, Request: facade.StartRequest{RunID: "terminal-consumer", Route: "lightweight", DefinitionSource: facade.DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: writerIntakeReceipt(t, root)}, Admission: engineWriterAdmission(t, root)})
			if err != nil {
				t.Fatal(err)
			}
			if status == "ABORTED" {
				event, err := protocol.NewRequestEvent("abort-request", protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "Abort"})
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err = f.Submit(event); err != nil {
					t.Fatal(err)
				}
				token, err := f.Freshness(string(event.ID))
				if err != nil {
					t.Fatal(err)
				}
				decide, err := protocol.NewDecideEvent("abort-decision", protocol.RequestID(event.ID), token, "abort")
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err = f.Submit(decide); err != nil {
					t.Fatal(err)
				}
			}
			directory := filepath.Join(root, facade.EngineNamespace, "terminal-consumer")
			retainRetiredChildReference(t, root, "terminal-consumer")
			result, err := consumeWorkflow(IO{}, f, root, "terminal-consumer")
			if err != nil {
				t.Fatal(err)
			}
			if result.Boundary != status || result.Progress.Running != 0 || len(result.Children) != 0 {
				t.Fatalf("terminal boundary=%+v", result)
			}
			if _, err := os.Stat(filepath.Join(directory, "state.json")); !os.IsNotExist(err) {
				t.Fatal("terminal transition retained active state", err)
			}
			summaryPath := filepath.Join(directory, "terminal-summary.json")
			before, err := os.ReadFile(summaryPath)
			if err != nil {
				t.Fatal(err)
			}
			revision := result.Run.Revision
			result, err = consumeWorkflow(IO{}, f, root, "terminal-consumer")
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(summaryPath)
			if err != nil {
				t.Fatal(err)
			}
			if result.Boundary != status || result.Run.Revision != revision || string(before) != string(after) {
				t.Fatal("terminal replay changed evidence or boundary")
			}
		})
	}
}

func TestWorkflowConsumerPreservesNonterminalChildError(t *testing.T) {
	f, root, statePath := startEngineRunForWriterTest(t)
	retainRetiredChildReference(t, root, "writer-exclusive")
	result, err := consumeWorkflow(IO{}, f, root, "writer-exclusive")
	if err == nil || result.Run.Status == "COMPLETE" || result.Run.Status == "ABORTED" {
		t.Fatal("nonterminal child error was hidden", err)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatal("nonterminal error removed active state", err)
	}
	if _, err := os.Stat(filepath.Join(root, facade.EngineNamespace, "writer-exclusive", "terminal-summary.json")); !os.IsNotExist(err) {
		t.Fatal("nonterminal error published terminal", err)
	}
}

// The boundary fixture retains the parent's slice reference and child's intake
// metadata after the child's active checkpoint has retired.
func retainRetiredChildReference(t *testing.T, root, runID string) {
	t.Helper()
	directory := filepath.Join(root, facade.EngineNamespace, runID)
	requestRaw, err := os.ReadFile(filepath.Join(directory, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	childDirectory := filepath.Join(root, facade.EngineNamespace, "retired-child")
	if err := os.MkdirAll(childDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	var request facade.StartRequest
	if err = json.Unmarshal(requestRaw, &request); err != nil {
		t.Fatal(err)
	}
	childRequest := request
	childRequest.RunID = "retired-child"
	childRaw, err := json.Marshal(childRequest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(childDirectory, "request.json"), childRaw, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := persistence.NewStore(directory, persistence.Config{PackageDigest: request.PackageDigest, DefinitionSource: request.DefinitionSource, InstalledTargetIdentity: request.InstalledTargetIdentity})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	var state protocol.State
	if err = json.Unmarshal(snapshot.Content, &state); err != nil {
		t.Fatal(err)
	}
	state.SliceChildren = map[string]string{"slice": "retired-child"}
	if _, err = store.Save(persistence.Transaction{ExpectedRevision: snapshot.Revision, ExpectedFingerprint: "fixture", CollectFingerprint: func() (string, error) { return "fixture", nil }, Content: &state}); err != nil {
		t.Fatal(err)
	}
}
