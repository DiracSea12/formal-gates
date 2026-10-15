//go:build !windows

package harness

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
)

// Foreground Ask delivery leaves the original native job available to the same
// consumer's subsequent receipt and result admission.
func TestWorkflowForegroundAskPreservesOriginalLiveJob(t *testing.T) {
	f, _, s, binary := engineNativeFixture(t, true)
	ref, input := stageNativeDevelopment(t, f, s)
	request, _, directory, _, err := f.WorkerExecutionRequest(ref, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	s.request, s.directory, s.workspace = request, directory, input.Validation.Workspace.Path
	s.request["binding"], err = NativeBinding("codex")
	if err != nil {
		t.Fatal(err)
	}
	s.peer.Mode, s.peer.WorkerAction, s.peer.WorkerAttempt, s.peer.Answer = "workflow-held", ref.ActionID, ref.AttemptID, nil
	s.peer.Configuration["sandbox"] = map[string]any{"type": "workspaceWrite"}
	s.save(t)
	cmd := exec.Command(binary, "workflow", "drive", "--root", fRoot(ref), "--run-id", s.runID, "--machine")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	release := func() {
		if err := os.WriteFile(filepath.Join(s.config, "release-turn"), nil, 0600); err != nil {
			t.Error(err)
		}
	}
	defer release()
	for {
		select {
		case err := <-done:
			t.Fatalf("consumer ended before original acknowledgement: %v %s", err, out.Bytes())
		default:
		}
		if _, err := NativeStartEvidence("codex", directory); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	identity, err := NativeStartEvidence("codex", directory)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := f.PrepareControl(facade.ControlInput{Request: protocol.ControlAbort})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := confirmFacadeFixtureControl(t, f, candidate); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Ask foreground: %v %s", err, out.Bytes())
		}
	case <-time.After(time.Second):
		release()
		<-done
		t.Fatal("persisted Ask waited for the independent original job")
	}
	var response struct {
		Boundary string            `json:"boundary"`
		Run      facade.Run        `json:"run"`
		Blocked  map[string]string `json:"blocked"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Boundary != "ASK" || len(response.Run.PendingAsks) != 1 || len(response.Blocked) != 0 || !NativeJobAlive(directory) || nativePeerTurns(t, s) != 1 {
		t.Fatalf("Ask lost its independent original job: %s", out.Bytes())
	}
	if got, err := NativeStartEvidence("codex", directory); err != nil || got != identity {
		t.Fatal("foreground return changed original acknowledgement", got, err)
	}
	body, err := confirmInstalledFixtureControl(t, binary, fRoot(ref), s.runID, candidate)
	var confirmed struct {
		Run struct {
			Next struct {
				Kind string `json:"kind"`
			} `json:"next"`
		} `json:"run"`
	}
	if err != nil || json.Unmarshal(body, &confirmed) != nil || confirmed.Run.Next.Kind != "ASK" || !NativeJobAlive(directory) || nativePeerTurns(t, s) != 1 {
		t.Fatalf("control consumer did not release its local lease at Ask: %v %s", err, body)
	}
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	decline, err := f.PrepareControl(facade.ControlInput{Ask: 1, Choice: "decline", Reason: "Continue the original live task"})
	if err != nil || len(run.PendingAsks) != 1 {
		t.Fatal("original Ask unavailable", err)
	}
	if _, _, err := confirmFacadeFixtureControl(t, f, decline); err != nil {
		t.Fatal(err)
	}
	release()
	var source map[string]any
	for {
		source, err = NativeJobSource("codex", directory)
		if err == nil {
			if _, exitErr := os.Stat(filepath.Join(directory, "exit.json")); exitErr == nil {
				break
			}
		} else if _, exitErr := os.Stat(filepath.Join(directory, "exit.json")); exitErr == nil {
			t.Fatal("original completed job lacks its source", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if source["identity"] != identity || nativePeerTurns(t, s) != 1 {
		t.Fatal("unconsumed original result changed identity or turn count")
	}
	for range 2 {
		body, err := exec.Command(binary, "workflow", "drive", "--root", fRoot(ref), "--run-id", s.runID, "--machine").CombinedOutput()
		if err != nil || json.Unmarshal(body, &response) != nil || response.Boundary != "ASK" || len(response.Blocked) != 0 {
			t.Fatalf("original source collection: %v %s", err, body)
		}
		if recovered, err := NativeJobSource("codex", directory); err != nil || !reflect.DeepEqual(recovered, source) || nativePeerTurns(t, s) != 2 {
			t.Fatal("collection or replay reissued the original turn", recovered, err)
		}
	}
	var state struct {
		Content protocol.State `json:"content"`
	}
	body, err = os.ReadFile(filepath.Join(fRoot(ref), facade.EngineNamespace, s.runID, "state.json"))
	if err != nil || json.Unmarshal(body, &state) != nil {
		t.Fatal("original accepted state unavailable", err)
	}
	if state.Content.Results[ref.ActionID].Outcome != protocol.OutcomePass || state.Content.SpawnReceipts[ref.ActionID].Correlation != identity {
		t.Fatal("same source did not reach original result/receipt admission")
	}
	t.Logf("foreground=ASK live_original_identity=%s original_action=%s original_attempt=%s developer_turns=1 total_with_gate=2", identity, ref.ActionID, ref.AttemptID)
}
