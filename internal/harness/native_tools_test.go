package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
)

func nativeToolFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "action", "call")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	materialPath := filepath.Join(root, "material.json")
	material := worker.Material{Input: protocol.WorkerInput{Pending: protocol.PendingAction{ActionID: "issued-action"}, Validation: &protocol.ValidationInput{Workspace: &protocol.NativeResource{Path: workspace}}}}
	if err := writeAtomicJSON(materialPath, material); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), map[string]any{"workspace": workspace, "read_only": false, "engine": map[string]any{"workerMaterial": map[string]any{"path": materialPath, "actionId": "issued-action"}}}); err != nil {
		t.Fatal(err)
	}
	return directory, workspace
}

func callWorkspace(t *testing.T, tools *nativeWorkspaceTools, args string) map[string]any {
	t.Helper()
	value, err := tools.invoke(nativeWorkspaceTool, json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return value.(map[string]any)
}

func TestNativeWorkspaceTransfersOriginalFileBytes(t *testing.T) {
	t.Parallel()
	directory, workspace := nativeToolFixture(t)
	body := []byte{0xff, 0, '\n', 0xfe}
	if err := os.WriteFile(filepath.Join(workspace, "original.bin"), body, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, "destination"), 0755); err != nil {
		t.Fatal(err)
	}
	tools, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	list := callWorkspace(t, tools, `{"action":"list"}`)
	if list["listing"] != 1 {
		t.Fatal(list)
	}
	copy := json.RawMessage(`{"action":"copy","listing":1,"entry":2,"targetListing":1,"targetEntry":1,"name":"copy.bin"}`)
	if _, err := tools.invoke(nativeWorkspaceTool, copy); err != nil {
		t.Fatal(err)
	}
	if copied, err := os.ReadFile(filepath.Join(workspace, "destination", "copy.bin")); err != nil || !bytes.Equal(copied, body) {
		t.Fatalf("copied original bytes: %x %v", copied, err)
	}
	if _, err := tools.invoke(nativeWorkspaceTool, copy); err == nil {
		t.Fatal("repeated copy overwrote its existing target")
	}
	continued := filepath.Join(filepath.Dir(directory), "continued")
	if err := os.Mkdir(continued, 0700); err != nil {
		t.Fatal(err)
	}
	request, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(continued, "native-request.json"), request, 0600); err != nil {
		t.Fatal(err)
	}
	resumed, err := newNativeWorkspaceTools(continued, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	callWorkspace(t, resumed, `{"action":"move","listing":1,"entry":2,"targetListing":1,"targetEntry":1,"name":"moved.bin"}`)
	if moved, err := os.ReadFile(filepath.Join(workspace, "destination", "moved.bin")); err != nil || !bytes.Equal(moved, body) {
		t.Fatalf("moved original bytes: %x %v", moved, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "original.bin")); !os.IsNotExist(err) {
		t.Fatal("move left its original source", err)
	}
	if _, err := resumed.invoke(nativeWorkspaceTool, json.RawMessage(`{"action":"copy","listing":1,"entry":1,"name":"directory-copy"}`)); err == nil {
		t.Fatal("file transfer recursively copied a directory")
	}
	if _, err := resumed.invoke(nativeWorkspaceTool, json.RawMessage(`{"action":"copy","listing":1,"entry":1,"name":"written.bin","content":"model bytes"}`)); err == nil {
		t.Fatal("file transfer accepted model-supplied bytes")
	}
}

func nativeToolAdmission(directory string, request map[string]any) (func(), error) {
	if getOf(getOf(request, "engine"), "workerMaterial") == nil {
		return nil, fmt.Errorf("missing original material reference")
	}
	if _, err := os.Stat(filepath.Join(directory, "withdrawn")); err == nil {
		return nil, fmt.Errorf("original action was withdrawn")
	}
	return func() {}, nil
}

func TestNativeWorkspacePublishedFactsSurviveContinuation(t *testing.T) {
	t.Parallel()
	directory, workspace := nativeToolFixture(t)
	if err := os.WriteFile(filepath.Join(workspace, "file.txt"), []byte("first\nsecond\n"), 0644); err != nil {
		t.Fatal(err)
	}
	tools, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	listing := callWorkspace(t, tools, `{"action":"list"}`)
	if listing["listing"] != 1 {
		t.Fatalf("first listing: %v", listing)
	}
	read := callWorkspace(t, tools, `{"action":"read","listing":1,"entry":1}`)
	if read["read"] != 2 {
		t.Fatalf("read identity: %v", read)
	}
	if err := os.WriteFile(filepath.Join(workspace, "earlier.txt"), []byte("different target"), 0644); err != nil {
		t.Fatal(err)
	}
	continued := filepath.Join(filepath.Dir(directory), "continued")
	if err := os.Mkdir(continued, 0755); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(continued, "native-request.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := newNativeWorkspaceTools(continued, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	callWorkspace(t, recovered, `{"action":"replace","read":2,"from":2,"to":2,"content":"updated\n"}`)
	got, err := os.ReadFile(filepath.Join(workspace, "file.txt"))
	if err != nil || string(got) != "first\nupdated\n" {
		t.Fatalf("original read target: %q %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(workspace, "earlier.txt"))
	if err != nil || string(got) != "different target" {
		t.Fatalf("new listing entry was rebound: %q %v", got, err)
	}
	listing = callWorkspace(t, recovered, `{"action":"list"}`)
	if listing["listing"] != 3 || len(recovered.facts[0].Paths) != 1 || recovered.facts[0].RequestDigest != tools.requestDigest {
		t.Fatalf("published facts overwritten: %v %+v", listing, recovered.facts)
	}
	if _, err := recovered.invoke(nativeWorkspaceTool, json.RawMessage(`{"action":"replace","read":2,"from":1,"to":1,"content":"stale"}`)); err == nil {
		t.Fatal("changed file reused stale read bytes")
	}
}

func TestNativeWorkspaceFactsRejectDifferentWorkspace(t *testing.T) {
	t.Parallel()
	directory, _ := nativeToolFixture(t)
	tools, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	callWorkspace(t, tools, `{"action":"list"}`)
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	objectOrEmpty(request)["workspace"] = other
	materialPath := textOf(getOf(getOf(getOf(request, "engine"), "workerMaterial"), "path"))
	if err := writeAtomicJSON(materialPath, worker.Material{Input: protocol.WorkerInput{Pending: protocol.PendingAction{ActionID: "issued-action"}, Validation: &protocol.ValidationInput{Workspace: &protocol.NativeResource{Path: other}}}}); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	if _, err := newNativeWorkspaceTools(directory); err == nil || !strings.Contains(err.Error(), "another action/workspace") {
		t.Fatalf("wrong workspace reused published facts: %v", err)
	}
}

func TestNativeWorkspaceMCPUsesOriginalAuthority(t *testing.T) {
	t.Parallel()
	directory, workspace := nativeToolFixture(t)
	if err := os.WriteFile(filepath.Join(workspace, "input.txt"), []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2024-11-05\"}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"workspace\",\"arguments\":{\"action\":\"list\"}}}\n")
	var output, errors bytes.Buffer
	if code := RunCommand([]string{"native-tools", directory}, input, &output, &errors, ExecutionServices{Admit: nativeToolAdmission}); code != 0 {
		t.Fatalf("MCP code %d: %s", code, &errors)
	}
	decoder := json.NewDecoder(&output)
	decoder.UseNumber()
	for _, id := range []int{1, 2, 3} {
		var reply map[string]any
		if err := decoder.Decode(&reply); err != nil {
			t.Fatal(err)
		}
		if intOf(reply["id"]) != id || reply["jsonrpc"] != "2.0" || reply["error"] != nil {
			t.Fatalf("original MCP response: %v", reply)
		}
		if id == 1 && getOf(reply["result"], "protocolVersion") != "2024-11-05" {
			t.Fatalf("changed MCP protocol: %v", reply)
		}
		if id == 2 {
			declared := listOf(getOf(reply["result"], "tools"))
			if len(declared) != 2 || getOf(declared[0], "name") != nativeWorkspaceTool || getOf(declared[1], "name") != nativeSemanticTool {
				t.Fatalf("tool surface: %v", reply)
			}
			for _, tool := range declared {
				schema := objectOrEmpty(getOf(tool, "inputSchema"))
				if schema["type"] != "object" {
					t.Fatalf("tool has no valid object declaration: %v", tool)
				}
			}
			var actions []any
			for _, branch := range listOf(getOf(getOf(declared[0], "inputSchema"), "oneOf")) {
				actions = append(actions, getOf(getOf(getOf(branch, "properties"), "action"), "const"))
			}
			if !slices.Contains(actions, any("replace")) {
				t.Fatal("writable workspace lost its original operation declaration")
			}
		}
	}
	tools, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.facts) != 1 || tools.facts[0].Workspace != resolveNativePath(workspace) || tools.facts[0].RequestDigest != tools.requestDigest {
		t.Fatalf("tool receipt lacks original authority: %+v", tools.facts)
	}
	readDirectory, readWorkspace := nativeToolFixture(t)
	request, err := readJSONFile(filepath.Join(readDirectory, "native-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	objectOrEmpty(request)["read_only"] = true
	materialPath := textOf(getOf(getOf(getOf(request, "engine"), "workerMaterial"), "path"))
	if err := writeAtomicJSON(materialPath, worker.Material{ReadOnly: true, Input: protocol.WorkerInput{Pending: protocol.PendingAction{ActionID: "issued-action"}, Validation: &protocol.ValidationInput{Workspace: &protocol.NativeResource{Path: readWorkspace}}}}); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(readDirectory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(readWorkspace, "input.txt"), []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	readTools, err := newNativeWorkspaceTools(readDirectory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	callWorkspace(t, readTools, `{"action":"list"}`)
	if _, err := readTools.invoke(nativeWorkspaceTool, json.RawMessage(`{"action":"create","name":"extra.txt","content":"unauthorized"}`)); err == nil {
		t.Fatal("read-only action wrote a file")
	}
	for _, action := range []string{"copy", "move"} {
		if _, err := readTools.invoke(nativeWorkspaceTool, json.RawMessage(fmt.Sprintf(`{"action":%q,"listing":1,"entry":1,"name":"extra.txt"}`, action))); err == nil {
			t.Fatalf("read-only action allowed %s", action)
		}
	}
	if _, err := os.Stat(filepath.Join(readWorkspace, "extra.txt")); !os.IsNotExist(err) {
		t.Fatalf("read-only side effect: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "withdrawn"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := tools.invoke(nativeWorkspaceTool, json.RawMessage(`{"action":"create","name":"extra.txt","content":"late"}`)); err == nil || !strings.Contains(err.Error(), "withdrawn") {
		t.Fatalf("withdrawn action bypassed original admission: %v", err)
	}
	for _, action := range []string{"copy", "move"} {
		if _, err := tools.invoke(nativeWorkspaceTool, json.RawMessage(fmt.Sprintf(`{"action":%q,"listing":1,"entry":1,"name":"extra.txt"}`, action))); err == nil || !strings.Contains(err.Error(), "withdrawn") {
			t.Fatalf("withdrawn action allowed %s: %v", action, err)
		}
	}
	if _, err := os.Stat(filepath.Join(workspace, "extra.txt")); !os.IsNotExist(err) {
		t.Fatalf("withdrawn action side effect: %v", err)
	}
}

func TestNativeWorkspaceSourceRemainsValidAfterContinuationPublishesFacts(t *testing.T) {
	t.Parallel()
	directory, workspace := nativeToolFixture(t)
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	objectOrEmpty(request)["read_only"] = true
	materialPath := textOf(getOf(getOf(getOf(request, "engine"), "workerMaterial"), "path"))
	if err := writeAtomicJSON(materialPath, worker.Material{ReadOnly: true, Input: protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Handler: "engine.gate.v3", Pending: protocol.PendingAction{ActionID: "issued-action"}, Validation: &protocol.ValidationInput{Workspace: &protocol.NativeResource{Path: workspace}}}}); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	call := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"workspace","arguments":{"action":"list"},"_meta":{"original":"native-context"}}}` + "\n"
	var output, errors bytes.Buffer
	if code := NativeTools(directory, strings.NewReader(call), &output, &errors, ExecutionServices{Admit: nativeToolAdmission}); code != 0 {
		t.Fatalf("original tools call: %d %s", code, &errors)
	}
	peerSubmitSemantics(directory, map[string]any{"findings": []any{}})
	for _, name := range []string{"native-source.json", "native-events.jsonl"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("{}\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeSubmittedAnswer(directory); err != nil {
		t.Fatal(err)
	}
	source, err := evidenceSource(directory, "fixture", "native-session", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeSource(directory, source, "fixture"); err != nil {
		t.Fatal(err)
	}
	if getOf(getOf(source, "files"), nativeToolCallsFile) == nil || getOf(getOf(source, "files"), nativeToolContextFile) == nil {
		t.Fatal("original source omitted tools proof", source)
	}
	recorded, err := os.ReadFile(filepath.Join(directory, nativeToolCallsFile))
	if err != nil {
		t.Fatal(err)
	}
	var originalCall map[string]any
	callDecoder := json.NewDecoder(bytes.NewReader(recorded))
	callDecoder.UseNumber()
	if err := callDecoder.Decode(&originalCall); err != nil {
		t.Fatal(err)
	}
	if getOf(getOf(getOf(originalCall, "request"), "params"), "_meta") == nil || intOf(getOf(getOf(originalCall, "request"), "id")) != 7 {
		t.Fatal("native call context was discarded", string(recorded))
	}
	continued := filepath.Join(filepath.Dir(directory), "resume")
	if err := os.Mkdir(continued, 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(continued, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	errors.Reset()
	if code := NativeTools(continued, strings.NewReader(call), &output, &errors, ExecutionServices{Admit: nativeToolAdmission}); code != 0 {
		t.Fatalf("continued tools call: %d %s", code, &errors)
	}
	if err := verifyNativeSource(directory, source, "fixture"); err != nil {
		t.Fatalf("later appended facts invalidated completed source: %v", err)
	}
	replayed, err := evidenceSource(directory, "fixture", "native-session", nil)
	if err != nil || !valuesEqual(replayed, source) {
		t.Fatalf("lost response rebound tool facts: %v", err)
	}
	context, err := readJSONFile(filepath.Join(directory, nativeToolContextFile))
	if err != nil {
		t.Fatal(err)
	}
	objectOrEmpty(context)["workspace"] = t.TempDir()
	if err := writeAtomicJSON(filepath.Join(directory, nativeToolContextFile), context); err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeSource(directory, source, "fixture"); err == nil {
		t.Fatal("source accepted changed tool authority")
	}
}

func TestNativeWorkspaceComparisonUsesIssuedSnapshots(t *testing.T) {
	t.Parallel()
	workspace, _ := nativePathsFixture(t)
	provider := vcs.New(workspace, nil)
	base, err := provider.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "left.txt"), []byte("candidate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := provider.Track([]string{"left.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Commit("candidate source"); err != nil {
		t.Fatal(err)
	}
	head, err := provider.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	materialPath := filepath.Join(directory, "material.json")
	material := worker.Material{ReadOnly: true, Input: protocol.WorkerInput{VCS: "git", Pending: protocol.PendingAction{ActionID: "gate-action"}, Validation: &protocol.ValidationInput{Workspace: &protocol.NativeResource{Path: workspace}, Gate: &protocol.GateInput{BaseSnapshot: base, Candidate: coverage.ValidationCandidate{Identity: head}}}}}
	if err := writeAtomicJSON(materialPath, material); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), map[string]any{"workspace": workspace, "read_only": true, "engine": map[string]any{"workerMaterial": map[string]any{"path": materialPath, "actionId": "gate-action"}}}); err != nil {
		t.Fatal(err)
	}
	tools, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "left.txt"), []byte("later workspace edit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	comparison := callWorkspace(t, tools, `{"action":"comparison"}`)
	rows := comparison["changes"].([]any)
	if len(rows) != 1 {
		t.Fatalf("issued range: %v", rows)
	}
	row := rows[0].(map[string]any)
	if row["path"] != "left.txt" || row["before"] != "initial\n" || row["after"] != "candidate\n" {
		t.Fatalf("tool read a later workspace or wrong range: %v", row)
	}
	if row["entry"] != nil || row["baseText"] != nil || row["text"] != nil {
		t.Fatal("comparison duplicates file content", row)
	}
	if _, err := tools.invoke(nativeWorkspaceTool, json.RawMessage(`{"action":"comparison","base":"different"}`)); err == nil {
		t.Fatal("model supplied a mechanical snapshot reference")
	}
}

func TestNativeWorkspaceParallelProcessesAllocateOriginalFactsInAdmissionWindow(t *testing.T) {
	t.Parallel()
	directory, _ := nativeToolFixture(t)
	var window sync.Mutex
	admit := func(directory string, request map[string]any) (func(), error) {
		if _, err := nativeToolAdmission(directory, request); err != nil {
			return nil, err
		}
		window.Lock()
		return window.Unlock, nil
	}
	first, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: admit})
	if err != nil {
		t.Fatal(err)
	}
	second, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: admit})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan int, 2)
	errors := make(chan error, 2)
	for _, tools := range []*nativeWorkspaceTools{first, second} {
		go func(tools *nativeWorkspaceTools) {
			value, err := tools.invoke(nativeWorkspaceTool, json.RawMessage(`{"action":"list"}`))
			if err != nil {
				errors <- err
				return
			}
			results <- value.(map[string]any)["listing"].(int)
		}(tools)
	}
	seen := map[int]bool{}
	for range 2 {
		select {
		case value := <-results:
			seen[value] = true
		case err := <-errors:
			t.Fatal(err)
		}
	}
	if !seen[1] || !seen[2] {
		t.Fatalf("two original calls reused a published identity: %v", seen)
	}
	recovered, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: admit})
	if err != nil || len(recovered.facts) != 2 {
		t.Fatalf("actual published facts: %+v %v", recovered, err)
	}
}

func workspaceMCP(t *testing.T, directory string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	frame, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "workspace", "arguments": args}})
	if err != nil {
		t.Fatal(err)
	}
	var output, errors bytes.Buffer
	if code := RunCommand([]string{"native-tools", directory}, bytes.NewReader(frame), &output, &errors, ExecutionServices{Admit: nativeToolAdmission}); code != 0 {
		t.Fatalf("public MCP: %d %s", code, &errors)
	}
	var reply map[string]any
	decoder := json.NewDecoder(&output)
	decoder.UseNumber()
	if err := decoder.Decode(&reply); err != nil {
		t.Fatal(err)
	}
	result := objectOrEmpty(reply["result"])
	content := listOf(result["content"])
	if len(content) != 1 {
		t.Fatalf("actual MCP reply: %v", reply)
	}
	if result["isError"] == true {
		return map[string]any{"error": getOf(content[0], "text")}, true
	}
	var body map[string]any
	decoder = json.NewDecoder(strings.NewReader(textOf(getOf(content[0], "text"))))
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body, false
}

func TestNativeWorkspaceRemovePreservesSymlinkTargets(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"file", "directory", "dangling"} {
		t.Run(kind, func(t *testing.T) {
			directory, workspace := nativeToolFixture(t)
			target := filepath.Join(workspace, "target")
			if kind == "file" {
				if err := os.WriteFile(target, []byte("target bytes\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "directory" {
				if err := os.Mkdir(target, 0755); err != nil {
					t.Fatal(err)
				}
			}
			link := filepath.Join(workspace, "link")
			if err := os.Symlink("target", link); err != nil {
				t.Fatal(err)
			}
			listing, failed := workspaceMCP(t, directory, map[string]any{"action": "list"})
			if failed {
				t.Fatal(listing)
			}
			var entry any
			for _, choice := range listOf(listing["entries"]) {
				if getOf(choice, "name") == "link" {
					entry = getOf(choice, "entry")
				}
			}
			if entry == nil {
				t.Fatal("original listing omitted link", listing)
			}
			removed, failed := workspaceMCP(t, directory, map[string]any{"action": "remove", "listing": listing["listing"], "entry": entry})
			if failed || removed["removed"] != true {
				t.Fatalf("original link removal: %v %v", removed, failed)
			}
			if _, err := os.Lstat(link); !os.IsNotExist(err) {
				t.Fatalf("selected directory entry remains: %v", err)
			}
			if kind == "file" {
				body, err := os.ReadFile(target)
				if err != nil || string(body) != "target bytes\n" {
					t.Fatalf("removed target instead of link: %q %v", body, err)
				}
			}
			if kind == "directory" {
				info, err := os.Stat(target)
				if err != nil || !info.IsDir() {
					t.Fatalf("removed target directory: %v", err)
				}
			}
			if kind == "dangling" {
				if _, err := os.Stat(target); !os.IsNotExist(err) {
					t.Fatalf("dangling target was created: %v", err)
				}
			}
		})
	}
}

func TestNativeWorkspaceSymlinkReadAndReplaceUseOriginalTarget(t *testing.T) {
	t.Parallel()
	directory, workspace := nativeToolFixture(t)
	if err := os.WriteFile(filepath.Join(workspace, "target"), []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(workspace, "link")); err != nil {
		t.Fatal(err)
	}
	listing, failed := workspaceMCP(t, directory, map[string]any{"action": "list"})
	if failed {
		t.Fatal(listing)
	}
	var entry any
	for _, choice := range listOf(listing["entries"]) {
		if getOf(choice, "name") == "link" {
			entry = getOf(choice, "entry")
		}
	}
	read, failed := workspaceMCP(t, directory, map[string]any{"action": "read", "listing": listing["listing"], "entry": entry})
	if failed || !valuesEqual(read["lines"], []any{"before\n"}) {
		t.Fatalf("link target read: %v", read)
	}
	replaced, failed := workspaceMCP(t, directory, map[string]any{"action": "replace", "read": read["read"], "from": 1, "to": 1, "content": "after\n"})
	if failed || replaced["written"] != true {
		t.Fatalf("link target edit: %v", replaced)
	}
	body, err := os.ReadFile(filepath.Join(workspace, "target"))
	if err != nil || string(body) != "after\n" {
		t.Fatalf("original target edit: %q %v", body, err)
	}
	if link, err := os.Readlink(filepath.Join(workspace, "link")); err != nil || link != "target" {
		t.Fatalf("link entry was replaced: %q %v", link, err)
	}
}

func TestNativeGateToolsExcludeOwnedContentAcrossResume(t *testing.T) {
	t.Parallel()
	workspace, _ := nativePathsFixture(t)
	owned := "checks/recipe.md"
	files := map[string]string{owned: "QA case body before\n", "developer_test.txt": "ordinary developer test before\n"}
	for name, body := range files {
		path := filepath.Join(workspace, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	provider := vcs.New(workspace, nil)
	if err := provider.Track([]string{owned, "developer_test.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Commit("fixture base"); err != nil {
		t.Fatal(err)
	}
	base, err := provider.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(strings.ReplaceAll(body, "before", "after")), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := provider.Track([]string{owned, "developer_test.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Commit("fixture candidate"); err != nil {
		t.Fatal(err)
	}
	head, err := provider.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(owned, filepath.Join(workspace, "alias")); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"engine.gate.v3", "engine.qa.review.rationale.v2"} {
		t.Run(role, func(t *testing.T) {
			owner := t.TempDir()
			directory := filepath.Join(owner, "call")
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			validation := &protocol.ValidationInput{Workspace: &protocol.NativeResource{Path: workspace}}
			if role == "engine.gate.v3" {
				validation.Gate = &protocol.GateInput{BaseSnapshot: base, Candidate: coverage.ValidationCandidate{Identity: head}, TestOwnedPaths: []string{owned}}
			} else {
				validation.ReviewBase = base
				validation.ReviewArtifact = &protocol.CandidateEvidence{Identity: head}
			}
			materialPath := filepath.Join(owner, "material.json")
			material := worker.Material{ReadOnly: true, Input: protocol.WorkerInput{Handler: role, VCS: "git", Pending: protocol.PendingAction{ActionID: role}, Validation: validation}}
			if err := writeAtomicJSON(materialPath, material); err != nil {
				t.Fatal(err)
			}
			request := map[string]any{"workspace": workspace, "read_only": true, "engine": map[string]any{"workerMaterial": map[string]any{"path": materialPath, "actionId": role}}}
			if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
				t.Fatal(err)
			}
			rpc := func(arguments map[string]any) (map[string]any, bool) {
				t.Helper()
				raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "workspace", "arguments": arguments}})
				var out, errout bytes.Buffer
				if code := NativeTools(directory, bytes.NewReader(raw), &out, &errout, ExecutionServices{Admit: nativeToolAdmission}); code != 0 {
					t.Fatalf("RPC %d: %s", code, errout.String())
				}
				var response map[string]any
				if err := json.Unmarshal(out.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				result := objectOrEmpty(response["result"])
				if result["isError"] == true {
					return nil, true
				}
				var value map[string]any
				if err := json.Unmarshal([]byte(textOf(getOf(listOf(result["content"])[0], "text"))), &value); err != nil {
					t.Fatal(err)
				}
				return value, false
			}
			searched, failed := rpc(map[string]any{"action": "search", "query": "QA case"})
			if failed || (len(listOf(searched["matches"])) == 0) != (role == "engine.gate.v3") {
				t.Fatal("search violates the original test-owned role boundary", role, searched)
			}
			comparison, failed := rpc(map[string]any{"action": "comparison"})
			if failed {
				t.Fatal("issued comparison failed")
			}
			var ownedRow map[string]any
			for _, row := range listOf(comparison["changes"]) {
				if getOf(row, "path") == owned {
					ownedRow = objectOrEmpty(row)
				}
				if getOf(row, "path") == "developer_test.txt" && getOf(row, "after") != "ordinary developer test after\n" {
					t.Fatal("ordinary test content hidden", row)
				}
			}
			if ownedRow == nil {
				t.Fatal("owned path identity disappeared", comparison)
			}
			if role == "engine.gate.v3" {
				if len(ownedRow) != 3 || ownedRow["testOwned"] != true {
					t.Fatalf("gate comparison leaks owned body or metadata: %v", ownedRow)
				}
			} else if ownedRow["after"] != "QA case body after\n" {
				t.Fatal("QA reviewer lost owned body", ownedRow)
			}
			listing, failed := rpc(map[string]any{"action": "list"})
			if failed {
				t.Fatal("root listing failed")
			}
			entries := map[string]map[string]any{}
			for _, entry := range listOf(listing["entries"]) {
				entries[textOf(getOf(entry, "name"))] = objectOrEmpty(entry)
			}
			checks, failed := rpc(map[string]any{"action": "list", "listing": listing["listing"], "entry": entries["checks"]["entry"]})
			if failed {
				t.Fatal("owned parent directory hidden")
			}
			entry := objectOrEmpty(listOf(checks["entries"])[0])
			if role == "engine.gate.v3" && entry["testOwned"] != true {
				t.Fatal("listing lost exclusion ownership", entry)
			}
			_, failed = rpc(map[string]any{"action": "read", "listing": checks["listing"], "entry": entry["entry"]})
			if failed != (role == "engine.gate.v3") {
				t.Fatal("owned read violates role boundary", role, failed)
			}
			ordinary, failed := rpc(map[string]any{"action": "read", "listing": listing["listing"], "entry": entries["developer_test.txt"]["entry"]})
			if failed || !strings.Contains(fmt.Sprint(ordinary["lines"]), "ordinary developer test after") {
				t.Fatal("ordinary developer tests not readable", ordinary)
			}
			_, failed = rpc(map[string]any{"action": "read", "listing": listing["listing"], "entry": entries["alias"]["entry"]})
			if failed != (role == "engine.gate.v3") {
				t.Fatal("resolved owned alias violates role boundary", role)
			}
			tools, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
			if err != nil {
				t.Fatal(err)
			}
			_, directErr := tools.path(owned, true)
			if (directErr != nil) != (role == "engine.gate.v3") {
				t.Fatal("direct owned path violates role boundary")
			}
			facts := append([]nativeToolFact(nil), tools.facts...)
			continued := filepath.Join(owner, "continued")
			if err := os.Mkdir(continued, 0700); err != nil {
				t.Fatal(err)
			}
			if err := writeAtomicJSON(filepath.Join(continued, "native-request.json"), request); err != nil {
				t.Fatal(err)
			}
			directory = continued
			_, failed = rpc(map[string]any{"action": "read", "listing": checks["listing"], "entry": entry["entry"]})
			if failed != (role == "engine.gate.v3") {
				t.Fatal("resumed owned read violates role boundary")
			}
			resumed, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
			if err != nil {
				t.Fatal(err)
			}
			for i, original := range facts {
				if !reflect.DeepEqual(resumed.facts[i], original) {
					t.Fatal("resume changed published fact identity")
				}
			}
			if role == "engine.gate.v3" {
				for _, fact := range resumed.facts {
					if bytes.Contains(fact.Content, []byte("QA case body")) {
						t.Fatal("gate location facts contain QA body")
					}
				}
				for _, job := range []string{filepath.Join(owner, "call"), continued} {
					for _, file := range []string{nativeToolCallsFile, nativeToolContextFile} {
						body, err := os.ReadFile(filepath.Join(job, file))
						if err != nil {
							t.Fatal(err)
						}
						if bytes.Contains(body, []byte("QA case body")) {
							t.Fatal("gate original RPC receipt/context leaks QA body", file)
						}
					}
				}
			}
		})
	}
}

func TestNativeWorkspaceTestSymbolsBindOriginalReadAndRealExecution(t *testing.T) {
	t.Parallel()
	directory, workspace := nativeToolFixture(t)
	body := "package sample\nimport (\"testing\"; \"os\")\nfunc helper() {}\nfunc TestWrong(t *testing.T) { t.Fatal(\"wrong test executed\") }\nfunc TestChosen(t *testing.T) { if err := os.WriteFile(\"selected.txt\", []byte(\"chosen\"), 0600); err != nil { t.Fatal(err) } }\n"
	for name, value := range map[string]string{"sample_test.go": body, "go.mod": "module sample\ngo 1.22\n"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tools, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	listing := callWorkspace(t, tools, `{"action":"list"}`)
	entry := 0
	for _, row := range listing["entries"].([]any) {
		if getOf(row, "name") == "sample_test.go" {
			entry = getOf(row, "entry").(int)
		}
	}
	read := callWorkspace(t, tools, fmt.Sprintf(`{"action":"read","listing":%d,"entry":%d}`, listing["listing"].(int), entry))
	choices := read["testSymbols"].([]map[string]any)
	if len(choices) != 2 || choices[0]["name"] != "TestWrong" || choices[1]["name"] != "TestChosen" || choices[1]["symbol"] != 2 {
		t.Fatal("published symbol choices did not match full source declarations", choices)
	}
	originalRead := read["read"].(int)
	// A later read has different ordinal positions; the case selects its original read.
	body = strings.Replace(body, "func helper() {}\n", "func helper() {}\nfunc TestCreated(t *testing.T) {}\n", 1)
	if err := os.WriteFile(filepath.Join(workspace, "sample_test.go"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	created := callWorkspace(t, tools, fmt.Sprintf(`{"action":"read","listing":%d,"entry":%d}`, listing["listing"].(int), entry))
	if len(created["testSymbols"].([]map[string]any)) != 3 {
		t.Fatal("new code symbol was not published")
	}
	resumed, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	facts := make([]worker.LocationFact, 0, len(resumed.facts))
	for _, fact := range resumed.facts {
		facts = append(facts, worker.LocationFact{Kind: fact.Kind, Paths: fact.Paths, Content: fact.Content})
	}
	input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, RunID: "run", Handler: "engine.qa.design.v6", Provider: "host", Pending: protocol.PendingAction{ActionID: "action", AttemptID: "attempt"}, Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewWhitebox}}}}
	raw := []byte(fmt.Sprintf(`{"cases":[{"scenario":"SUCCESS","oracle":"selected.txt contains chosen","test":{"read":%d,"symbol":2}}],"coverage":[]}`, originalRead))
	event, err := worker.DecodeResultWithFacts(input, "result", raw, "original-output", []string{}, facts)
	if err != nil {
		t.Fatal(err)
	}
	newRaw := []byte(fmt.Sprintf(`{"cases":[{"scenario":"SUCCESS","oracle":"created test passes","test":{"read":%d,"symbol":1}}],"coverage":[]}`, created["read"].(int)))
	newEvent, err := worker.DecodeResultWithFacts(input, "created-result", newRaw, "created-output", []string{}, facts)
	if err != nil || newEvent.Result.Validation.Manifest.Cases[0].TestRef != "sample_test.go::TestCreated" {
		t.Fatal("created code lost its symbol selection", err)
	}
	qaCase := event.Result.Validation.Manifest.Cases[0]
	if qaCase.TestRef != "sample_test.go::TestChosen" {
		t.Fatal("result used another read's symbol table", qaCase.TestRef)
	}
	if err := coverage.ValidateGoTestReference(workspace, qaCase.TestRef); err != nil {
		t.Fatal("selected test declaration is invalid", err)
	}
	operation, err := coverage.GoTestOperation(qaCase.TestRef)
	if err != nil {
		t.Fatal("selected test cannot execute", err)
	}
	cmd := exec.Command(operation.Operation.Argv[0], operation.Operation.Argv[1:]...)
	cmd.Dir = filepath.Join(workspace, operation.Directory)
	output, err := cmd.CombinedOutput()
	if err != nil || coverage.GoTestExecutionResult(output, operation.GoTestName) != "pass" {
		t.Fatalf("original selected test did not actually pass: %v\n%s", err, output)
	}
	value, err := os.ReadFile(filepath.Join(workspace, "selected.txt"))
	if err != nil || string(value) != "chosen" {
		t.Fatal("actual selected test side effect differs", err)
	}
	t.Logf("read=%d symbol=2 original=%s actual Go event=pass persisted=%q", originalRead, qaCase.TestRef, value)
}

func TestNativeRequirementReaderReturnsBoundOriginalLinesAfterWorkspaceChanges(t *testing.T) {
	t.Parallel()
	directory, workspace := nativeToolFixture(t)
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	path := textOf(getOf(getOf(getOf(request, "engine"), "workerMaterial"), "path"))
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var material worker.Material
	if err := json.Unmarshal(body, &material); err != nil {
		t.Fatal(err)
	}
	material.Input.RequirementDocument.Path = "requirements.md"
	material.Input.RequirementDocument.ContentDigest = "bound-original"
	material.Input.RequirementDocument.Content = "# Original\n完整确认正文。\n尾行。\n"
	material.Input.Scope = &protocol.WorkerScope{Owner: "slice-2", AcceptanceOwners: map[string]string{"AC-001": "slice-1", "AC-002": "slice-2", "AC-003": "master-merge"}}
	originalScope := material.Input.Scope
	os.WriteFile(filepath.Join(workspace, "requirements.md"), []byte("unconfirmed edits"), 0600)
	for _, handler := range []string{"engine.development.batch.v6", "engine.gate.v3", "engine.validation.observation.v2", "engine.repair.v7", "engine.repair.verify.v2"} {
		t.Run(handler, func(t *testing.T) {
			material.Input.Handler = handler
			if err := writeAtomicJSON(path, material); err != nil {
				t.Fatal(err)
			}
			tools, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(tools.material.Input.Scope, originalScope) {
				t.Fatal("bound child owner/sibling/merge responsibilities changed")
			}
			full := callWorkspace(t, tools, `{"action":"requirement"}`)
			if full["contentDigest"] != "bound-original" || strings.Join(full["lines"].([]string), "") != material.Input.RequirementDocument.Content {
				t.Fatalf("original material missing: %v", full)
			}
			partial := callWorkspace(t, tools, `{"action":"requirement","from":2,"to":2}`)
			if !reflect.DeepEqual(partial["lines"], []string{"完整确认正文。\n"}) {
				t.Fatalf("wrong original line range: %v", partial)
			}
			if _, err := tools.invoke(nativeWorkspaceTool, json.RawMessage(`{"action":"requirement","from":2,"to":9}`)); err == nil {
				t.Fatal("out-of-range original read accepted")
			}
		})
	}
}

func TestNativeWorkspaceCopiesOriginalLineRangesAndBasenames(t *testing.T) {
	t.Parallel()
	directory, workspace := nativeToolFixture(t)
	original := "first\r\n中文 \"existing\"\r\nlast\r\n"
	if err := os.WriteFile(filepath.Join(workspace, "source.go"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "target.go"), []byte("prefix\nend\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, "destination"), 0700); err != nil {
		t.Fatal(err)
	}
	invoke := func(value any) map[string]any {
		t.Helper()
		wire, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": nativeWorkspaceTool, "arguments": value}})
		var out, errout bytes.Buffer
		if code := NativeTools(directory, bytes.NewReader(wire), &out, &errout, ExecutionServices{Admit: nativeToolAdmission}); code != 0 {
			t.Fatal(errout.String())
		}
		reply, err := decodeNativeJSON(out.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		result := getOf(reply, "result")
		if getOf(result, "isError") != false {
			t.Fatal(textOf(result))
		}
		content := textOf(getOf(listOf(getOf(result, "content"))[0], "text"))
		body, err := decodeNativeJSON([]byte(content))
		if err != nil {
			t.Fatal(err)
		}
		return objectOrEmpty(body)
	}
	list := invoke(map[string]any{"action": "list"})
	listing := list["listing"]
	entries := map[string]any{}
	for _, item := range listOf(list["entries"]) {
		entries[textOf(getOf(item, "name"))] = getOf(item, "entry")
	}
	source := invoke(map[string]any{"action": "read", "listing": listing, "entry": entries["source.go"]})
	target := invoke(map[string]any{"action": "read", "listing": listing, "entry": entries["target.go"]})
	invoke(map[string]any{"action": "copy-range", "sourceRead": source["read"], "sourceFrom": 2, "sourceTo": 2, "read": target["read"], "from": 2, "to": 1})
	if body, err := os.ReadFile(filepath.Join(workspace, "target.go")); err != nil || string(body) != "prefix\n中文 \"existing\"\r\nend\n" {
		t.Fatalf("original range changed: %q %v", body, err)
	}
	invoke(map[string]any{"action": "copy", "listing": listing, "entry": entries["source.go"], "targetListing": listing, "targetEntry": entries["destination"]})
	if body, err := os.ReadFile(filepath.Join(workspace, "destination", "source.go")); err != nil || string(body) != original {
		t.Fatal("original basename or bytes changed", err)
	}
	if body, _ := os.ReadFile(filepath.Join(workspace, "source.go")); string(body) != original {
		t.Fatal("copy altered source bytes")
	}
}

// Program-owned fact paths are published with canonical separators: a nested
// file reached only by listing/read ordinals must assemble a test reference
// that coverage validation and the real go tool accept.
func TestNativeWorkspacePublishesCanonicalNestedTestPaths(t *testing.T) {
	t.Parallel()
	directory, workspace := nativeToolFixture(t)
	body := "package labelctl\nimport (\"testing\"; \"os\")\nfunc TestWrong(t *testing.T) { t.Fatal(\"wrong test executed\") }\nfunc TestChosen(t *testing.T) { if err := os.WriteFile(\"selected.txt\", []byte(\"chosen\"), 0600); err != nil { t.Fatal(err) } }\n"
	if err := os.MkdirAll(filepath.Join(workspace, "cmd", "labelctl"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{filepath.Join("cmd", "labelctl", "main_test.go"): body, "go.mod": "module labelctl\ngo 1.22\n"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tools, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	entry := func(list map[string]any, name string) int {
		t.Helper()
		for _, row := range list["entries"].([]any) {
			if getOf(row, "name") == name {
				return getOf(row, "entry").(int)
			}
		}
		t.Fatalf("published listing lacks %s: %v", name, list)
		return 0
	}
	rootList := callWorkspace(t, tools, `{"action":"list"}`)
	cmdList := callWorkspace(t, tools, fmt.Sprintf(`{"action":"list","listing":%d,"entry":%d}`, rootList["listing"].(int), entry(rootList, "cmd")))
	nested := callWorkspace(t, tools, fmt.Sprintf(`{"action":"list","listing":%d,"entry":%d}`, cmdList["listing"].(int), entry(cmdList, "labelctl")))
	read := callWorkspace(t, tools, fmt.Sprintf(`{"action":"read","listing":%d,"entry":%d}`, nested["listing"].(int), entry(nested, "main_test.go")))
	choices := read["testSymbols"].([]map[string]any)
	if len(choices) != 2 || choices[0]["name"] != "TestWrong" || choices[1]["name"] != "TestChosen" || choices[1]["symbol"] != 2 {
		t.Fatal("published symbol choices did not match nested source declarations", choices)
	}
	originalRead := read["read"].(int)
	resumed, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	persisted := resumed.facts[originalRead-1]
	if persisted.Kind != "read" || len(persisted.Paths) != 1 || persisted.Paths[0] != "cmd/labelctl/main_test.go" {
		t.Fatal("persisted read fact lost the canonical nested path", persisted.Paths)
	}
	if nestedList := resumed.facts[nested["listing"].(int)-1]; !slices.Contains(nestedList.Paths, "cmd/labelctl/main_test.go") {
		t.Fatal("persisted listing fact lost the canonical nested path", nestedList.Paths)
	}
	facts := make([]worker.LocationFact, 0, len(resumed.facts))
	for _, fact := range resumed.facts {
		facts = append(facts, worker.LocationFact{Kind: fact.Kind, Paths: fact.Paths, Content: fact.Content})
	}
	binding := coverage.SourceBinding{RunID: "run", RequirementRevision: "req", SolutionRevision: "sol"}
	input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, RunID: "run", Handler: "engine.qa.design.v6", Provider: "host", Pending: protocol.PendingAction{ActionID: "action", AttemptID: "attempt"}, Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{SourceBinding: binding, ReviewKind: coverage.ReviewWhitebox, RouteScope: "regular", TopologyScope: "master"}, Sources: []string{"REQ"}, Points: []coverage.AcceptancePoint{{PointID: "P-1", ObservableBehavior: "chosen test runs", Oracle: "selected.txt contains chosen"}}}, SourcePoints: []protocol.SourcePoint{{SourceID: "REQ", PointID: "P-1"}}}}
	raw := []byte(fmt.Sprintf(`{"cases":[{"scenario":"SUCCESS","oracle":"selected.txt contains chosen","files":[{"read":%d}],"test":{"read":%d,"symbol":2}}],"coverage":[[1]]}`, originalRead, originalRead))
	event, err := worker.DecodeResultWithFacts(input, "result", raw, "original-output", []string{}, facts)
	if err != nil {
		t.Fatal(err)
	}
	qaCase := event.Result.Validation.Manifest.Cases[0]
	if !reflect.DeepEqual(qaCase.Files, []string{"cmd/labelctl/main_test.go"}) {
		t.Fatal("assembled case files are not the canonical nested path", qaCase.Files)
	}
	if qaCase.TestRef != "cmd/labelctl/main_test.go::TestChosen" {
		t.Fatal("assembled test reference is not canonical", qaCase.TestRef)
	}
	contract := coverage.CoverageContract{RequiredSources: coverage.RequiredSources{Binding: binding, Sources: []coverage.RequiredSource{{SourceID: "REQ", Category: coverage.ProductRequirement, Applicability: coverage.QAApplicability}}}, Manifests: []coverage.AcceptanceManifest{*event.Result.Validation.Manifest}, Candidate: &coverage.ValidationCandidate{Identity: "candidate"}}
	if err := contract.Validate(); err != nil {
		t.Fatal("assembled manifest fails ordinary contract validation", err)
	}
	if err := coverage.ValidateGoTestReference(workspace, qaCase.TestRef); err != nil {
		t.Fatal("selected test declaration is invalid", err)
	}
	operation, err := coverage.GoTestOperation(qaCase.TestRef)
	if err != nil {
		t.Fatal("selected test cannot execute", err)
	}
	cmd := exec.Command(operation.Operation.Argv[0], operation.Operation.Argv[1:]...)
	cmd.Dir = filepath.Join(workspace, operation.Directory)
	output, err := cmd.CombinedOutput()
	if err != nil || coverage.GoTestExecutionResult(output, operation.GoTestName) != "pass" {
		t.Fatalf("selected nested test did not actually pass: %v\n%s", err, output)
	}
	value, err := os.ReadFile(filepath.Join(workspace, "cmd", "labelctl", "selected.txt"))
	if err != nil || string(value) != "chosen" {
		t.Fatal("actual selected test side effect differs", err)
	}
	t.Logf("read=%d symbol=2 ref=%s persisted=%q", originalRead, qaCase.TestRef, persisted.Paths)
}
