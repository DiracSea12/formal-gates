//go:build !windows

package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/lifecycle"
)

func TestNativeEditCheckAllowsSiblingResultsAndControl(t *testing.T) {
	f, _, scenario, _ := engineNativeFixture(t)
	command := `printf started > "$FORMAL_GATES_WORKSPACE/.gates/tmp/started"; while test ! -e "$FORMAL_GATES_WORKSPACE/.gates/tmp/release"; do sleep 0.02; done; printf original-check`
	stageNativeReviewsWithEdit(t, f, scenario, command, false, "left", "right")
	stageNativeDevelopmentReady(t, f, scenario)
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	refs := []facade.WorkerMaterial{}
	inputs := []protocol.WorkerInput{}
	for id, input := range run.ValidationInputs {
		if input.Handler != "engine.development.batch.v6" {
			continue
		}
		ref := run.WorkerMaterials[id]
		body, err := os.ReadFile(ref.Path)
		if err != nil {
			t.Fatal(err)
		}
		var material worker.Material
		if err := json.Unmarshal(body, &material); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref)
		inputs = append(inputs, material.Input)
	}
	if len(refs) != 2 {
		t.Fatalf("expected independent original developers: %v", run.ValidationInputs)
	}
	request, _, directory, _, err := f.WorkerExecutionRequest(refs[0], "")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	tools, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: facade.AdmitExecution})
	if err != nil {
		t.Fatal(err)
	}
	workspace := inputs[0].Validation.Workspace.Path
	if err := os.MkdirAll(filepath.Join(workspace, ".gates", "tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := tools.invoke(nativeWorkspaceTool, json.RawMessage(`{"action":"create","name":"new-source.txt","content":"actual source"}`))
		done <- err
	}()
	completed := false
	t.Cleanup(func() {
		if !completed {
			if err := os.WriteFile(filepath.Join(workspace, ".gates", "tmp", "release"), nil, 0600); err != nil {
				t.Error(err)
			}
			if err := <-done; err != nil {
				t.Error(err)
			}
		}
	})
	for {
		if _, err := os.Stat(filepath.Join(workspace, ".gates", "tmp", "started")); err == nil {
			break
		}
		select {
		case err := <-done:
			completed = true
			t.Fatalf("original check did not start: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	spawn, err := protocol.NewSpawnReceiptEvent("sibling-start", refs[1].ActionID, "codex", "sibling-original", protocol.SpawnStatusSpawned)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Submit(spawn); err != nil {
		t.Fatal(err)
	}
	event, err := worker.DecodeResultWithPaths(inputs[1], "sibling-progress", []byte(`{"summary":"Mapped this independent artifact","remainingWork":"Implement its next source edit"}`), "fixture-original-sibling-progress", []string{})
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.ObserveNativeCompletion(scenario.workspace, "native-execution", refs[1].ActionID, "codex", "sibling-original"); err != nil {
		t.Fatal(err)
	}
	accepted, _, err := f.Submit(event)
	if err != nil || accepted.Status != "ACCEPTED" {
		t.Fatalf("independent result while original check runs: %+v %v", accepted, err)
	}
	control, err := f.PrepareControl(facade.ControlInput{Request: protocol.ControlRequestRequirementChange})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.SubmitControl(control); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		completed = true
		t.Fatalf("check ended before its original release: %v", err)
	default:
	}
	if err := os.WriteFile(filepath.Join(workspace, ".gates", "tmp", "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	err = <-done
	completed = true
	if err != nil {
		t.Fatal(err)
	}
	factNumber := 0
	for number, fact := range tools.facts {
		if fact.Kind == "edit" {
			factNumber = number + 1
			break
		}
	}
	if factNumber == 0 {
		t.Fatal("original mutation fact missing")
	}
	fact := tools.facts[factNumber-1]
	value, err := readJSONFile(filepath.Join(editCheckDirectory(fact, factNumber, 0), "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := editCheckReceipt(editCheckDirectory(fact, factNumber, 0), objectOrEmpty(value))
	if err != nil || !valuesEqual(receipt["exit_code"], 0) {
		t.Fatal(receipt, err)
	}
	if _, err := tools.invoke(nativeWorkspaceTool, json.RawMessage(`{"action":"create","name":"forbidden.txt","content":"after control"}`)); err == nil {
		t.Fatal("held original action continued writing")
	}
	if _, err := os.Stat(filepath.Join(workspace, "forbidden.txt")); !os.IsNotExist(err) {
		t.Fatal("unissued write after control", err)
	}
	t.Log("real admission released before original check completed; sibling result and Phase A control accepted")
}
