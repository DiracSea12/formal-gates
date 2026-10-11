package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
)

func nativePathsFixture(t *testing.T) (string, func(string, string) string) {
	t.Helper()
	workspace := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = workspace
		if body, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(body))
		}
	}
	for _, name := range []string{"left.txt", "right.txt"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte("initial\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-qm", "Fixture"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = workspace
		if body, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(body))
		}
	}
	return workspace, func(name, artifact string) string {
		job := filepath.Join(t.TempDir(), "native")
		if err := os.Mkdir(job, 0700); err != nil {
			t.Fatal(err)
		}
		material := worker.Material{Input: protocol.WorkerInput{VCS: "git", Handler: "engine.development.batch.v2", Validation: &protocol.ValidationInput{Workspace: &protocol.NativeResource{Path: workspace}, Development: &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{Artifacts: []string{artifact}}}}}}
		materialPath := filepath.Join(job, "material.json")
		body, _ := json.Marshal(material)
		if err := os.WriteFile(materialPath, body, 0600); err != nil {
			t.Fatal(err)
		}
		request := map[string]any{"workspace": workspace, "read_only": false, "engine": map[string]any{"workerMaterial": map[string]any{"path": materialPath}, "runId": name}}
		if err := writeAtomicJSON(filepath.Join(job, "native-request.json"), request); err != nil {
			t.Fatal(err)
		}
		if err := claimJob(job, request); err != nil {
			t.Fatal(err)
		}
		for _, file := range []string{"native-source.json", "native-events.jsonl"} {
			if err := os.WriteFile(filepath.Join(job, file), []byte("{}\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return job
	}
}

func TestNativeTerminalPathsPreserveSharedWorkspaceAndOriginalSource(t *testing.T) {
	workspace, job := nativePathsFixture(t)
	left, right := job("left", "left.txt"), job("right", "right.txt")
	for _, name := range []string{"left.txt", "right.txt"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte("delivered\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, directory := range []string{left, right} {
		if err := submitTerminalPathFixture(t, directory); err != nil {
			t.Fatal(err)
		}
		source, err := evidenceSource(directory, "fixture", "session", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyNativeSource(directory, source, "fixture"); err != nil {
			t.Fatal(err)
		}
		if !valuesEqual(getOf(getOf(source, "effects"), "paths"), []any{"left.txt", "right.txt"}) {
			t.Fatal("workspace diff was filtered by an individual batch", source)
		}
		if err := writeAtomicJSON(filepath.Join(directory, "exit.json"), map[string]any{"exit_code": 0}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(workspace, "later.txt"), []byte("later writer\n"), 0600); err != nil {
			t.Fatal(err)
		}
		recovered, err := evidenceSource(directory, "fixture", "session", nil)
		if err != nil || !valuesEqual(source, recovered) {
			t.Fatal("lost response absorbed later workspace changes", err)
		}
		effects := objectOrEmpty(recovered["effects"])
		effects["workspace"] = filepath.Join(workspace, "other")
		if err := verifyNativeSource(directory, recovered, "fixture"); err == nil {
			t.Fatal("source ignored the captured workspace binding")
		}
		if err := os.Remove(filepath.Join(workspace, "later.txt")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNativeTerminalPathsMissingAtCompletedExitNeverRecapture(t *testing.T) {
	workspace, job := nativePathsFixture(t)
	directory := job("completed", "left.txt")
	if err := writeAtomicJSON(filepath.Join(directory, "exit.json"), map[string]any{"exit_code": 0}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "future.txt"), []byte("future\n"), 0600); err != nil {
		t.Fatal(err)
	}
	actual, err := vcs.NewNative(workspace, "git", nil).ChangedPaths()
	if err != nil || !reflect.DeepEqual(actual, []string{"future.txt"}) {
		t.Fatal("negative control lacks real later diff", actual, err)
	}
	if err := submitTerminalPathFixture(t, directory); err == nil || !strings.Contains(err.Error(), "terminated") {
		t.Fatal("completed exit was allowed to recapture future state", err)
	}
	for _, file := range []string{nativePathsFile, "answer.raw.txt"} {
		if _, err := os.Stat(filepath.Join(directory, file)); !os.IsNotExist(err) {
			t.Fatal("recovery created completion evidence", file, err)
		}
	}
}

func TestNativeTerminalPathsBindOriginalRequestAndSubmittedSemantics(t *testing.T) {
	_, job := nativePathsFixture(t)
	directory := job("empty", "left.txt")
	if err := submitTerminalPathFixture(t, directory); err != nil {
		t.Fatal(err)
	}
	facts, err := nativePathEvidence(directory, `{"interruptionReason":"Path evidence fixture ended"}`, false)
	if err != nil || facts.Paths == nil || len(facts.Paths) != 0 {
		t.Fatal("captured empty diff must be an explicit empty array", facts, err)
	}
	if _, err := nativePathEvidence(directory, `{"changed":true}`, false); err == nil {
		t.Fatal("captured facts accepted another native answer")
	}
	if err := os.WriteFile(filepath.Join(directory, "native-request.json"), []byte(`{"workspace":"different","read_only":false,"engine":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := nativePathEvidence(directory, `{"interruptionReason":"Path evidence fixture ended"}`, false); err == nil {
		t.Fatal("captured facts accepted another native request")
	}
}

func TestWhiteboxTerminalPathsIncludeCommittedMaterialsAndActualRemovals(t *testing.T) {
	workspace, job := nativePathsFixture(t)
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = workspace
		body, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(err, string(body))
		}
		return strings.TrimSpace(string(body))
	}
	base := git("rev-parse", "HEAD")
	for _, name := range []string{"retained_test.txt", "removed_test.txt"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte("assert actual behavior\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", "--all")
	git("commit", "-qm", "Test materials")
	directory := job("whitebox", "")
	materialPath := filepath.Join(directory, "material.json")
	body, err := os.ReadFile(materialPath)
	if err != nil {
		t.Fatal(err)
	}
	var material worker.Material
	if err := json.Unmarshal(body, &material); err != nil {
		t.Fatal(err)
	}
	material.Input.Handler = "engine.qa.design.v2"
	material.Input.Validation.Workspace.Source = base
	material.Input.Validation.ReviewBase = base
	body, err = json.Marshal(material)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(materialPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := submitTerminalPathFixture(t, directory); err != nil {
		t.Fatal(err)
	}
	answer, err := os.ReadFile(filepath.Join(directory, "answer.json"))
	if err != nil {
		t.Fatal(err)
	}
	facts, err := nativePathEvidence(directory, string(answer), false)
	if err != nil || !reflect.DeepEqual(facts.Paths, []string{"removed_test.txt", "retained_test.txt"}) {
		t.Fatalf("semantic revision lost committed test materials: %+v %v", facts, err)
	}
	if err := os.Remove(filepath.Join(workspace, "removed_test.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "added_test.txt"), []byte("new assertion\n"), 0600); err != nil {
		t.Fatal(err)
	}
	directory = job("whitebox", "")
	materialPath = filepath.Join(directory, "material.json")
	if err := os.WriteFile(materialPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := submitTerminalPathFixture(t, directory); err != nil {
		t.Fatal(err)
	}
	facts, err = nativePathEvidence(directory, `{"interruptionReason":"Path evidence fixture ended"}`, false)
	if err != nil || !reflect.DeepEqual(facts.Paths, []string{"added_test.txt", "retained_test.txt"}) {
		t.Fatalf("complete material diff retained a file deleted back to development: %+v %v", facts, err)
	}
	if err := writeAtomicJSON(filepath.Join(directory, "exit.json"), map[string]any{"exit_code": 0}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "later_test.txt"), []byte("later\n"), 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := nativePathEvidence(directory, `{"interruptionReason":"Path evidence fixture ended"}`, false)
	if err != nil || !reflect.DeepEqual(facts, recovered) {
		t.Fatal("recovery changed original complete paths", recovered, err)
	}
}

func submitTerminalPathFixture(t *testing.T, directory string) error {
	t.Helper()
	request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": nativeSemanticTool, "arguments": map[string]any{"interruptionReason": "Path evidence fixture ended"}}})
	var out, errout bytes.Buffer
	if code := NativeTools(directory, bytes.NewReader(request), &out, &errout, ExecutionServices{Admit: nativeToolAdmission}); code != 0 {
		return fmt.Errorf("native tools: %s", &errout)
	}
	return writeSubmittedAnswer(directory)
}
