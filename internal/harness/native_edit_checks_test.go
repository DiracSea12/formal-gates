package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
)

func nativeEditCheckFixture(t *testing.T) (*nativeWorkspaceTools, string) {
	t.Helper()
	directory, workspace := nativeToolFixture(t)
	original, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	path := textOf(getOf(getOf(getOf(original, "engine"), "workerMaterial"), "path"))
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var material worker.Material
	if err := json.Unmarshal(body, &material); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = workspace
		body, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s %v", args, body, err)
		}
		return strings.TrimSpace(string(body))
	}
	git("init", "-q")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-qm", "base")
	material.Input.VCS = "git"
	material.Input.Validation.Workspace.Source = git("rev-parse", "HEAD")
	material.Input.Handler = "engine.development.batch.v4"
	material.Input.Validation.Development = &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "approved", Checks: []protocol.DevelopmentCheck{{ID: "edit", Purpose: protocol.CheckEdit, Command: "printf edit; printf diagnostic >&2; printf x >> \"$FORMAL_GATES_WORKSPACE/.gates/tmp/check-count\""}, {ID: "full", Purpose: protocol.CheckFullTests, Command: "touch full-ran"}, {ID: "ordinary", Command: "touch ordinary-ran"}}}}
	if err := os.MkdirAll(filepath.Join(workspace, ".gates", "tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(path, material); err != nil {
		t.Fatal(err)
	}
	tools, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	return tools, workspace
}

func TestNativeWorkspaceRunsDeclaredEditChecks(t *testing.T) {
	tools, workspace := nativeEditCheckFixture(t)
	first := callWorkspace(t, tools, `{"action":"create","name":"first.txt","content":"original"}`)
	checks := first["checks"].([]any)
	if len(checks) != 1 || !valuesEqual(getOf(checks[0], "exitCode"), 0) || getOf(checks[0], "stdout") != "edit" || getOf(checks[0], "stderr") != "diagnostic" || getOf(checks[0], "directory") != nil || getOf(checks[0], "requestDigest") != nil {
		t.Fatal(first)
	}
	callWorkspace(t, tools, `{"action":"create","name":"second.txt","content":"another source"}`)
	body, err := os.ReadFile(filepath.Join(workspace, ".gates", "tmp", "check-count"))
	if err != nil || string(body) != "xx" {
		t.Fatalf("edits check count %q %v", body, err)
	}
	for _, name := range []string{"full-ran", "ordinary-ran"} {
		if _, err := os.Stat(filepath.Join(workspace, name)); !os.IsNotExist(err) {
			t.Fatal("batch-boundary command ran after an edit", name, err)
		}
	}
	for number, fact := range tools.facts {
		if fact.Kind != "edit" {
			continue
		}
		directory := editCheckDirectory(fact, number+1, 0)
		stdout, _ := os.ReadFile(filepath.Join(directory, "stdout.log"))
		stderr, _ := os.ReadFile(filepath.Join(directory, "stderr.log"))
		if string(stdout) != "edit" || string(stderr) != "diagnostic" {
			t.Fatalf("actual check streams %q %q", stdout, stderr)
		}
		request, _ := readJSONFile(filepath.Join(directory, "request.json"))
		if textOf(getOf(request, "source_digest")) == "" || textOf(getOf(request, "environment_digest")) == "" {
			t.Fatal(request)
		}
	}
	resumed, err := newNativeWorkspaceTools(tools.directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	callWorkspace(t, resumed, `{"action":"list"}`)
	body, _ = os.ReadFile(filepath.Join(workspace, ".gates", "tmp", "check-count"))
	if string(body) != "xx" {
		t.Fatal("receipt recovery re-executed the check", string(body))
	}
}

func TestNativeEditCheckResumesAnUnfinishedOriginalRequest(t *testing.T) {
	tools, workspace := nativeEditCheckFixture(t)
	number, err := tools.publish(nativeToolFact{Kind: "edit", Content: []byte(`{"action":"create","name":"current.txt","content":"current"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "current.txt"), []byte("current"), 0600); err != nil {
		t.Fatal(err)
	}
	// The original mutation is durable; no check request or receipt exists yet.
	resumed, err := newNativeWorkspaceTools(tools.directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	callWorkspace(t, resumed, `{"action":"list"}`)
	directory := editCheckDirectory(resumed.facts[number-1], number, 0)
	request, err := readJSONFile(filepath.Join(directory, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := resumed.workspaceDigest()
	if err != nil {
		t.Fatal(err)
	}
	if getOf(request, "source_digest") != expected {
		t.Fatal("recovered guard did not bind the complete original current workspace", request)
	}
	if _, err := editCheckReceipt(directory, objectOrEmpty(request)); err != nil {
		t.Fatal(err)
	}
	callWorkspace(t, resumed, `{"action":"list"}`)
	body, _ := os.ReadFile(filepath.Join(workspace, ".gates", "tmp", "check-count"))
	if string(body) != "x" {
		t.Fatalf("repeated recovery executed %q", body)
	}
}

func TestNativeEditChecksRejectReadOnlyAndInvalidMutations(t *testing.T) {
	tools, workspace := nativeEditCheckFixture(t)
	if _, err := tools.call(json.RawMessage(`{"action":"create","name":"../outside","content":"wrong"}`)); err == nil {
		t.Fatal("invalid mutation accepted")
	}
	tools.readOnly = true
	if _, err := tools.call(json.RawMessage(`{"action":"mkdir","name":"readonly"}`)); err == nil {
		t.Fatal("readonly mutation accepted")
	}
	if _, err := os.Stat(filepath.Join(workspace, ".gates", "tmp", "check-count")); !os.IsNotExist(err) {
		t.Fatal("rejected mutation executed a check", err)
	}
	for _, fact := range tools.facts {
		if fact.Kind == "edit" {
			t.Fatal("rejected mutation published an edit fact", fact)
		}
	}
}

func TestNativeEditChecksCoverEveryWorkspaceMutation(t *testing.T) {
	tools, workspace := nativeEditCheckFixture(t)
	callWorkspace(t, tools, `{"action":"mkdir","name":"destination"}`)
	callWorkspace(t, tools, `{"action":"create","name":"source.txt","content":"first\nsecond\n"}`)
	selectEntry := func(name string) (int, int) {
		t.Helper()
		list := callWorkspace(t, tools, `{"action":"list"}`)
		for _, entry := range list["entries"].([]any) {
			if getOf(entry, "name") == name {
				return list["listing"].(int), getOf(entry, "entry").(int)
			}
		}
		t.Fatal("missing original listing entry", name)
		return 0, 0
	}
	listing, entry := selectEntry("source.txt")
	read := callWorkspace(t, tools, fmt.Sprintf(`{"action":"read","listing":%d,"entry":%d}`, listing, entry))
	callWorkspace(t, tools, fmt.Sprintf(`{"action":"replace","read":%d,"from":2,"to":2,"content":"replaced\n"}`, read["read"].(int)))
	listing, entry = selectEntry("source.txt")
	read = callWorkspace(t, tools, fmt.Sprintf(`{"action":"read","listing":%d,"entry":%d}`, listing, entry))
	callWorkspace(t, tools, fmt.Sprintf(`{"action":"copy-range","sourceRead":%d,"sourceFrom":1,"sourceTo":1,"read":%d,"from":3,"to":2}`, read["read"].(int), read["read"].(int)))
	listing, entry = selectEntry("source.txt")
	targetListing, targetEntry := selectEntry("destination")
	callWorkspace(t, tools, fmt.Sprintf(`{"action":"copy","listing":%d,"entry":%d,"targetListing":%d,"targetEntry":%d}`, listing, entry, targetListing, targetEntry))
	listing, entry = selectEntry("source.txt")
	callWorkspace(t, tools, fmt.Sprintf(`{"action":"move","listing":%d,"entry":%d,"name":"moved.txt"}`, listing, entry))
	listing, entry = selectEntry("moved.txt")
	callWorkspace(t, tools, fmt.Sprintf(`{"action":"remove","listing":%d,"entry":%d}`, listing, entry))
	body, err := os.ReadFile(filepath.Join(workspace, ".gates", "tmp", "check-count"))
	if err != nil || string(body) != "xxxxxxx" {
		t.Fatalf("one original execution per successful mutation: %q %v", body, err)
	}
	copied, err := os.ReadFile(filepath.Join(workspace, "destination", "source.txt"))
	if err != nil || string(copied) != "first\nreplaced\nfirst\n" {
		t.Fatalf("mutation original bytes %q %v", copied, err)
	}
}

func TestNativeEditCheckProcess(t *testing.T) {
	directory := os.Getenv("NATIVE_EDIT_CHECK_JOB")
	if directory == "" {
		return
	}
	request, err := readJSONFile(filepath.Join(directory, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := claimJob(directory, objectOrEmpty(request)); err != nil {
		t.Fatal(err)
	}
	if _, err := executeRequest(directory, objectOrEmpty(request)); err != nil {
		t.Fatal(err)
	}
}

func TestNativeEditCheckObservesInterruptedOriginalProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM observation")
	}
	tools, workspace := nativeEditCheckFixture(t)
	originalRequest, err := readJSONFile(filepath.Join(tools.directory, "native-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	path := textOf(getOf(getOf(getOf(originalRequest, "engine"), "workerMaterial"), "path"))
	material := *tools.material
	material.Input.Validation.Development.Batch.Checks[0].Command = `printf x >> "$FORMAL_GATES_WORKSPACE/.gates/tmp/check-count"; printf started > "$FORMAL_GATES_WORKSPACE/.gates/tmp/check-started"; while :; do sleep 1; done`
	if err := writeAtomicJSON(path, material); err != nil {
		t.Fatal(err)
	}
	tools, err = newNativeWorkspaceTools(tools.directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	number, err := tools.publish(nativeToolFact{Kind: "edit", Content: []byte(`{"action":"create","name":"current.txt","content":"current"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "current.txt"), []byte("current"), 0600); err != nil {
		t.Fatal(err)
	}
	fact := tools.facts[number-1]
	checks, original, err := editFactChecks(fact)
	if err != nil {
		t.Fatal(err)
	}
	directory := editCheckDirectory(fact, number, 0)
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NATIVE_EDIT_CHECK_JOB", directory)
	request, err := editCheckRequest(fact, checks[0], original)
	if err != nil {
		t.Fatal(err)
	}
	request["source_digest"], err = tools.workspaceDigest()
	if err != nil {
		t.Fatal(err)
	}
	request["environment_digest"], err = executionEnvironmentDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(directory, "request.json"), request); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.timeout=0", "-test.run=^TestNativeEditCheckProcess$", "-test.v")
	var output strings.Builder
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(workspace, ".gates", "tmp", "check-started")); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	resumed, err := newNativeWorkspaceTools(tools.directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { _, err := resumed.call(json.RawMessage(`{"action":"list"}`)); finished <- err }()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("original process: %v %s", err, &output)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	receipt, err := editCheckReceipt(directory, request)
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(receipt["exit_code"], 130) || receipt["error"] != "owned job was cancelled" {
		t.Fatal(receipt)
	}
	callWorkspace(t, resumed, `{"action":"list"}`)
	count, _ := os.ReadFile(filepath.Join(workspace, ".gates", "tmp", "check-count"))
	if string(count) != "x" {
		t.Fatalf("recovery re-executed original process: %q", count)
	}
}

func TestNativeEditCheckEvidenceBindsSemanticSubmission(t *testing.T) {
	tools, workspace := nativeEditCheckFixture(t)
	request, err := readJSONFile(filepath.Join(tools.directory, "native-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	path := textOf(getOf(getOf(getOf(request, "engine"), "workerMaterial"), "path"))
	material := *tools.material
	material.Input.Provider = "dsh"
	material.ResultSchema, err = worker.ResultSchema(material.Input)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(path, material); err != nil {
		t.Fatal(err)
	}
	request.(map[string]any)["provider"] = "dsh"
	request.(map[string]any)["schema"] = material.ResultSchema
	if err := writeAtomicJSON(filepath.Join(tools.directory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	call := func(name string, arguments any) {
		t.Helper()
		wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}})
		if err != nil {
			t.Fatal(err)
		}
		var out, errout bytes.Buffer
		if code := NativeTools(tools.directory, bytes.NewReader(wire), &out, &errout, ExecutionServices{Admit: nativeToolAdmission}); code != 0 {
			t.Fatalf("MCP: %d %s", code, &errout)
		}
		response, err := decodeNativeJSON(out.Bytes())
		if err != nil || getOf(getOf(response, "result"), "isError") != false {
			t.Fatalf("MCP %s: %s %v", name, &out, err)
		}
	}
	if err := claimJob(tools.directory, objectOrEmpty(request)); err != nil {
		t.Fatal(err)
	}
	call(nativeWorkspaceTool, map[string]any{"action": "create", "name": "created.txt", "content": "original"})
	value := map[string]any{"summary": "created the declared source", "completion": map[string]any{"dod": []any{}, "handoff": "complete"}}
	call(nativeSemanticTool, value)
	if err := os.WriteFile(filepath.Join(tools.directory, "native-source.json"), []byte(`{"sessionId":"original-session","turn":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	events := `{"sessionId":"original-session","event":{"type":"assistant/message","data":{"turn":1,"message":{"content":[{"type":"text","text":"Done"}]},"usage":{"inputTokens":23,"outputTokens":7}}}}` + "\n" + `{"sessionId":"original-session","event":{"type":"turn/end","data":{"turn":1,"reason":{"kind":"completed"}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(tools.directory, "native-events.jsonl"), []byte(events), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := observeNative(tools.directory, "original-session")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := NativeWorkerResultEvidence("dsh", tools.directory, source)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := nativePathEvidence(tools.directory, string(evidence.Answer), false)
	if err != nil {
		t.Fatal(err)
	}
	event, err := worker.DecodeResultWithFacts(material.Input, "result", evidence.Answer, evidence.Digest, paths.Paths, evidence.LocationFacts)
	if err != nil {
		t.Fatal(err)
	}
	if event.Result.ActionID != material.Input.Pending.ActionID || event.Result.Outcome != protocol.OutcomePass || event.Result.Validation.Development.Completion == nil {
		t.Fatal(event)
	}
	count, _ := os.ReadFile(filepath.Join(workspace, ".gates", "tmp", "check-count"))
	if string(count) != "x" {
		t.Fatalf("original check executions: %q", count)
	}
	facts, err := nativeToolFacts(tools.directory)
	if err != nil {
		t.Fatal(err)
	}
	for number, fact := range facts {
		if fact.Kind != "edit" {
			continue
		}
		if err := os.WriteFile(filepath.Join(editCheckDirectory(fact, number+1, 0), "stdout.log"), []byte("changed"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := NativeWorkerResultEvidence("dsh", tools.directory, source); err == nil {
			t.Fatal("changed original check proof admitted")
		}
		return
	}
	t.Fatal("original tool mutation fact missing")
}

func TestNativeEditCheckFailureDiagnosticsReachTheWorker(t *testing.T) {
	tools, _ := nativeEditCheckFixture(t)
	request, err := readJSONFile(filepath.Join(tools.directory, "native-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	path := textOf(getOf(getOf(getOf(request, "engine"), "workerMaterial"), "path"))
	material := *tools.material
	material.Input.Validation.Development.Batch.Checks[0].Command = "printf original-output; printf 'exact failure: source line 3' >&2; exit 7"
	if err := writeAtomicJSON(path, material); err != nil {
		t.Fatal(err)
	}
	call := func(arguments any) map[string]any {
		t.Helper()
		wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": nativeWorkspaceTool, "arguments": arguments}})
		if err != nil {
			t.Fatal(err)
		}
		var out, errout bytes.Buffer
		if code := NativeTools(tools.directory, bytes.NewReader(wire), &out, &errout, ExecutionServices{Admit: nativeToolAdmission}); code != 0 {
			t.Fatalf("MCP %d: %s", code, &errout)
		}
		response, err := decodeNativeJSON(out.Bytes())
		if err != nil || getOf(getOf(response, "result"), "isError") != false {
			t.Fatalf("MCP reply %s %v", &out, err)
		}
		content := listOf(getOf(getOf(response, "result"), "content"))
		value, err := decodeNativeJSON([]byte(textOf(getOf(content[0], "text"))))
		if err != nil {
			t.Fatal(err)
		}
		return objectOrEmpty(value)
	}
	first := call(map[string]any{"action": "create", "name": "source.txt", "content": "current"})
	recovered := call(map[string]any{"action": "list"})
	for _, reply := range []map[string]any{first, recovered} {
		checks := listOf(reply["checks"])
		if len(checks) != 1 || !valuesEqual(getOf(checks[0], "exitCode"), 7) || getOf(checks[0], "stdout") != "original-output" || getOf(checks[0], "stderr") != "exact failure: source line 3" || getOf(checks[0], "directory") != nil || getOf(checks[0], "requestDigest") != nil {
			t.Fatalf("worker diagnostic: %+v", reply)
		}
	}
	if !valuesEqual(first["checks"], recovered["checks"]) {
		t.Fatal("recovery changed original diagnostics")
	}
}

func TestNativeWorkspaceAuthorsExecutableFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Unix execution mode")
	}
	tools, workspace := nativeEditCheckFixture(t)
	original := []byte("#!/bin/sh\nprintf 'hello\\n'\n")
	args, _ := json.Marshal(map[string]any{"action": "create", "name": "hello.sh", "content": string(original), "executable": true})
	if _, err := tools.call(args); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "hello.sh")
	out, err := exec.Command(path).Output()
	if err != nil || string(out) != "hello\n" {
		t.Fatalf("actual public script %q %v", out, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatal(info, err)
	}
	changes, err := vcs.NewNative(workspace, "git", nil).CompareWorkingTree(tools.material.Input.Validation.Workspace.Source)
	if err != nil || len(changes) != 1 || changes[0].Path != "hello.sh" || changes[0].Mode != "100755" || !bytes.Equal(changes[0].Content, original) {
		t.Fatalf("native executable candidate facts: %+v %v", changes, err)
	}
	list := callWorkspace(t, tools, `{"action":"list"}`)
	entry := 0
	for _, value := range list["entries"].([]any) {
		if getOf(value, "name") == "hello.sh" {
			entry = getOf(value, "entry").(int)
		}
	}
	if entry == 0 {
		t.Fatal("new original script missing from listing")
	}
	read := callWorkspace(t, tools, fmt.Sprintf(`{"action":"read","listing":%d,"entry":%d}`, list["listing"], entry))
	callWorkspace(t, tools, fmt.Sprintf(`{"action":"executable","read":%d,"executable":false}`, read["read"]))
	info, _ = os.Stat(path)
	current, _ := os.ReadFile(path)
	if info.Mode().Perm() != 0644 || !bytes.Equal(current, original) {
		t.Fatal("mode change altered original non-execution bits/bytes", info.Mode(), current)
	}
	callWorkspace(t, tools, fmt.Sprintf(`{"action":"executable","read":%d,"executable":true}`, read["read"]))
	callWorkspace(t, tools, fmt.Sprintf(`{"action":"replace","read":%d,"from":2,"to":2,"content":"printf 'changed\\n'\n"}`, read["read"]))
	info, _ = os.Stat(path)
	if info.Mode().Perm() != 0755 {
		t.Fatal("replace lost execution bits")
	}
	if _, err := tools.call(json.RawMessage(`{"action":"executable","listing":1,"entry":1}`)); err == nil {
		t.Fatal("missing semantic executable value accepted")
	}
	tools.readOnly = true
	if _, err := tools.call(json.RawMessage(`{"action":"executable","listing":1,"entry":1,"executable":false}`)); err == nil {
		t.Fatal("readonly mode mutation accepted")
	}
}
