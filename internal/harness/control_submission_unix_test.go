//go:build !windows

package harness

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
)

func TestSavedControlSubmissionTargetsParentAndChild(t *testing.T) {
	master, _, scenario, binary := engineNativeFixture(t, true)
	stageNativeReviews(t, master, scenario, "core", "tooling", "master-merge")
	if requested, err := master.RequestWorkflowChoice(); err != nil || !requested {
		t.Fatal("engine did not issue its actual topology Ask", err)
	}
	run, err := master.Show()
	if err != nil {
		t.Fatal(err)
	}
	askPosition := func(t *testing.T, run facade.Run, control protocol.ControlKind) int {
		t.Helper()
		for index, ask := range run.PendingAsks {
			if ask.Control == control {
				return index + 1
			}
		}
		t.Fatalf("missing actual %s Ask", control)
		return 0
	}
	topology, err := master.PrepareControl(facade.ControlInput{Ask: askPosition(t, run, protocol.ControlConfirmTopology), Choice: "split", Parallel: "Two independent deliveries", Owners: []int{0, 0}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := master.SubmitControl(topology); err != nil {
		t.Fatal(err)
	}
	run, err = master.Drive()
	if err != nil || len(run.SliceChildren) != 2 {
		t.Fatal("Controller did not create the actual original children", err)
	}
	child, err := facade.Open(scenario.workspace, run.SliceChildren["core"])
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(t *testing.T, input string, args ...string) []byte {
		t.Helper()
		command := exec.Command(binary, append([]string{"workflow"}, args...)...)
		command.Stdin = bytes.NewBufferString(input)
		body, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("CLI %v failed: %v: %s", args, err, body)
		}
		return body
	}
	for _, target := range []*facade.Facade{master, child} {
		current, err := target.Show()
		if err != nil {
			t.Fatal(err)
		}
		t.Run(current.RunID, func(t *testing.T) {
			saved := filepath.Join(t.TempDir(), "confirmation.json")
			invoke(t, `{"request":"ABORT"}`, "next", "--root", scenario.workspace, "--run-id", current.RunID, "--decision", "-", "--save-candidate", saved)
			for _, decision := range []bool{false, true} {
				if decision {
					run, err := target.Show()
					if err != nil {
						t.Fatal(err)
					}
					input, err := json.Marshal(facade.ControlInput{Ask: askPosition(t, run, protocol.ControlAbort), Choice: "decline", Reason: "Keep the active workflow"})
					if err != nil {
						t.Fatal(err)
					}
					invoke(t, string(input), "next", "--root", scenario.workspace, "--run-id", current.RunID, "--decision", "-", "--save-candidate", saved)
				}
				body, err := os.ReadFile(saved)
				var candidate facade.ControlCandidate
				if err != nil || json.Unmarshal(body, &candidate) != nil || candidate.Root != scenario.workspace || candidate.Run.RunID != current.RunID {
					t.Fatal("public preparation lost the original target", err)
				}
				invoke(t, "", "submit", "--confirmation", saved, "--single-step")
				path := filepath.Join(scenario.workspace, facade.EngineNamespace, current.RunID, "state.json")
				accepted, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var state struct {
					Content protocol.State `json:"content"`
				}
				if err := json.Unmarshal(accepted, &state); err != nil {
					t.Fatal(err)
				}
				if _, exists := state.Content.Events[string(candidate.Event.ID)]; !exists {
					t.Fatal("submission did not accept the original candidate event")
				}
				target, err = facade.Open(scenario.workspace, current.RunID)
				if err != nil {
					t.Fatal(err)
				}
				invoke(t, "", "submit", "--confirmation", saved, "--single-step")
				replayed, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(accepted, replayed) {
					t.Fatal("reopened submission replay added an event or changed state", err)
				}
			}
		})
	}
}
