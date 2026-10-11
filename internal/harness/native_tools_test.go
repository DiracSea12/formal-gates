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
	value, err := tools.call(json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return value.(map[string]any)
}

func TestNativeWorkspaceReadsProgramSuppliedTaskMaterials(t *testing.T) {
	workspace, directory := t.TempDir(), t.TempDir()
	materials := filepath.Join(workspace, ".formal-gates-input", "operation")
	files := map[string]string{
		"requirement.md":                 "Approved requirement\n",
		"reviewer-base.md":               "Review the approved scope\n",
		"task.json":                      `{"bindings":{"revision":"opaque-revision","base":"opaque-base","candidate":"opaque-candidate"},"checks":[]}`,
		"comparison/comparison.json":     `{"base":"opaque-base","candidate":"opaque-candidate","changes":[]}`,
		"comparison/full.diff":           "diff --git a/input.txt b/input.txt\n-before\n+after\n",
		"comparison/base/input.txt":      "before\n",
		"comparison/candidate/input.txt": "after\n",
	}
	for name, body := range files {
		path := filepath.Join(materials, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), map[string]any{"workspace": workspace, "read_only": true}); err != nil {
		t.Fatal(err)
	}
	call := func(arguments map[string]any) map[string]any {
		t.Helper()
		request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "workspace", "arguments": arguments}})
		var output, errout bytes.Buffer
		if code := NativeTools(directory, bytes.NewReader(request), &output, &errout); code != 0 {
			t.Fatalf("native tools %d: %s", code, errout.String())
		}
		var response map[string]any
		if err := json.Unmarshal(output.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		result := objectOrEmpty(response["result"])
		if result["isError"] != false {
			t.Fatalf("material read failed: %v", result)
		}
		var value map[string]any
		if err := json.Unmarshal([]byte(textOf(getOf(listOf(result["content"])[0], "text"))), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	for name, body := range files {
		listing, entry := any(nil), any(nil)
		parts := append([]string{".formal-gates-input", "operation"}, strings.Split(name, "/")...)
		for _, part := range parts {
			args := map[string]any{"action": "list"}
			if listing != nil {
				args["listing"], args["entry"] = listing, entry
			}
			value := call(args)
			listing, entry = value["listing"], nil
			for _, choice := range listOf(value["entries"]) {
				if getOf(choice, "name") == part {
					entry = getOf(choice, "entry")
				}
			}
			if entry == nil {
				t.Fatalf("original listing omitted %s", part)
			}
		}
		read := call(map[string]any{"action": "read", "listing": listing, "entry": entry})
		var content strings.Builder
		for _, line := range listOf(read["lines"]) {
			content.WriteString(textOf(line))
		}
		if content.String() != body {
			t.Fatalf("material %s changed: %q != %q", name, content.String(), body)
		}
	}
	if err := verifyNativeToolEvidence(directory); err != nil {
		t.Fatal(err)
	}
}

func TestNativeWorkspaceTransfersOriginalFileBytes(t *testing.T) {
	directory, workspace := nativeToolFixture(t)
	body := []byte{0xff, 0, '\n', 0xfe}
	if err := os.WriteFile(filepath.Join(workspace, "original.bin"), body, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, "destination"), 0755); err != nil {
		t.Fatal(err)
	}
	tools, err := newNativeWorkspaceTools(directory, nativeToolAdmission)
	if err != nil {
		t.Fatal(err)
	}
	list := callWorkspace(t, tools, `{"action":"list"}`)
	if list["listing"] != 1 {
		t.Fatal(list)
	}
	copy := json.RawMessage(`{"action":"copy","listing":1,"entry":2,"targetListing":1,"targetEntry":1,"name":"copy.bin"}`)
	if _, err := tools.call(copy); err != nil {
		t.Fatal(err)
	}
	if copied, err := os.ReadFile(filepath.Join(workspace, "destination", "copy.bin")); err != nil || !bytes.Equal(copied, body) {
		t.Fatalf("copied original bytes: %x %v", copied, err)
	}
	if _, err := tools.call(copy); err == nil {
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
	resumed, err := newNativeWorkspaceTools(continued, nativeToolAdmission)
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
	if _, err := resumed.call(json.RawMessage(`{"action":"copy","listing":1,"entry":1,"name":"directory-copy"}`)); err == nil {
		t.Fatal("file transfer recursively copied a directory")
	}
	if _, err := resumed.call(json.RawMessage(`{"action":"copy","listing":1,"entry":1,"name":"written.bin","content":"model bytes"}`)); err == nil {
		t.Fatal("file transfer accepted model-supplied bytes")
	}
}

func TestNativeWorkspaceShowsOnlyCurrentReadOnlyMaterials(t *testing.T) {
	workspace, directory := t.TempDir(), t.TempDir()
	parent := filepath.Join(workspace, ".formal-gates-input")
	current := filepath.Join(parent, "current")
	other := filepath.Join(parent, "other-role")
	for _, path := range []string{current, other} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "task.json"), []byte("original task\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), map[string]any{"workspace": workspace, "read_only": false, "materials": current}); err != nil {
		t.Fatal(err)
	}
	tools, err := newNativeWorkspaceTools(directory)
	if err != nil {
		t.Fatal(err)
	}
	callWorkspace(t, tools, `{"action":"list"}`)
	listed := callWorkspace(t, tools, `{"action":"list","listing":1,"entry":1}`)
	if !valuesEqual(listed["entries"], []any{map[string]any{"entry": 1, "name": "current", "directory": true}}) {
		t.Fatal("other role materials were visible", listed)
	}
	if _, err := tools.path(".formal-gates-input/other-role/task.json", true); err == nil {
		t.Fatal("other role materials could be read")
	}
	callWorkspace(t, tools, `{"action":"list","listing":2,"entry":1}`)
	read := callWorkspace(t, tools, `{"action":"read","listing":3,"entry":1}`)
	if strings.Join(read["lines"].([]string), "") != "original task\n" {
		t.Fatal("current task was unavailable", read)
	}
	if _, err := tools.call(json.RawMessage(`{"action":"replace","read":4,"from":1,"to":1,"content":"changed"}`)); err == nil {
		t.Fatal("writable developer altered current task materials")
	}
	if _, err := tools.call(json.RawMessage(`{"action":"move","listing":3,"entry":1,"name":"moved-task.json"}`)); err == nil {
		t.Fatal("move removed current task materials")
	}
	if _, err := tools.call(json.RawMessage(`{"action":"create","listing":2,"entry":1,"name":"extra","content":"changed"}`)); err == nil {
		t.Fatal("create changed current task materials")
	}
	callWorkspace(t, tools, `{"action":"create","name":"app.go","content":"authored code\n"}`)
	if body, err := os.ReadFile(filepath.Join(current, "task.json")); err != nil || string(body) != "original task\n" {
		t.Fatal("read-only task bytes changed", err)
	}
	if body, err := os.ReadFile(filepath.Join(workspace, "app.go")); err != nil || string(body) != "authored code\n" {
		t.Fatal("production source was blocked", err)
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
	directory, workspace := nativeToolFixture(t)
	if err := os.WriteFile(filepath.Join(workspace, "file.txt"), []byte("first\nsecond\n"), 0644); err != nil {
		t.Fatal(err)
	}
	tools, err := newNativeWorkspaceTools(directory, nativeToolAdmission)
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
	recovered, err := newNativeWorkspaceTools(continued, nativeToolAdmission)
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
	if _, err := recovered.call(json.RawMessage(`{"action":"replace","read":2,"from":1,"to":1,"content":"stale"}`)); err == nil {
		t.Fatal("changed file reused stale read bytes")
	}
}

func TestNativeWorkspaceFactsRejectDifferentWorkspace(t *testing.T) {
	directory, _ := nativeToolFixture(t)
	tools, err := newNativeWorkspaceTools(directory, nativeToolAdmission)
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
	directory, workspace := nativeToolFixture(t)
	if err := os.WriteFile(filepath.Join(workspace, "input.txt"), []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2024-11-05\"}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"workspace\",\"arguments\":{\"action\":\"list\"}}}\n")
	var output, errors bytes.Buffer
	if code := RunCommand([]string{"native-tools", directory}, input, &output, &errors, nativeToolAdmission); code != 0 {
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
		if id == 2 && len(listOf(getOf(reply["result"], "tools"))) != 1 {
			t.Fatalf("tool surface: %v", reply)
		}
	}
	tools, err := newNativeWorkspaceTools(directory, nativeToolAdmission)
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
	readTools, err := newNativeWorkspaceTools(readDirectory, nativeToolAdmission)
	if err != nil {
		t.Fatal(err)
	}
	callWorkspace(t, readTools, `{"action":"list"}`)
	if _, err := readTools.call(json.RawMessage(`{"action":"create","name":"extra.txt","content":"unauthorized"}`)); err == nil {
		t.Fatal("read-only action wrote a file")
	}
	for _, action := range []string{"copy", "move"} {
		if _, err := readTools.call(json.RawMessage(fmt.Sprintf(`{"action":%q,"listing":1,"entry":1,"name":"extra.txt"}`, action))); err == nil {
			t.Fatalf("read-only action allowed %s", action)
		}
	}
	if _, err := os.Stat(filepath.Join(readWorkspace, "extra.txt")); !os.IsNotExist(err) {
		t.Fatalf("read-only side effect: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "withdrawn"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := tools.call(json.RawMessage(`{"action":"create","name":"extra.txt","content":"late"}`)); err == nil || !strings.Contains(err.Error(), "withdrawn") {
		t.Fatalf("withdrawn action bypassed original admission: %v", err)
	}
	for _, action := range []string{"copy", "move"} {
		if _, err := tools.call(json.RawMessage(fmt.Sprintf(`{"action":%q,"listing":1,"entry":1,"name":"extra.txt"}`, action))); err == nil || !strings.Contains(err.Error(), "withdrawn") {
			t.Fatalf("withdrawn action allowed %s: %v", action, err)
		}
	}
	if _, err := os.Stat(filepath.Join(workspace, "extra.txt")); !os.IsNotExist(err) {
		t.Fatalf("withdrawn action side effect: %v", err)
	}
}

func TestNativeWorkspaceSourceRemainsValidAfterContinuationPublishesFacts(t *testing.T) {
	directory, workspace := nativeToolFixture(t)
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	objectOrEmpty(request)["read_only"] = true
	materialPath := textOf(getOf(getOf(getOf(request, "engine"), "workerMaterial"), "path"))
	if err := writeAtomicJSON(materialPath, worker.Material{ReadOnly: true, Input: protocol.WorkerInput{Pending: protocol.PendingAction{ActionID: "issued-action"}, Validation: &protocol.ValidationInput{Workspace: &protocol.NativeResource{Path: workspace}}}}); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	call := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"workspace","arguments":{"action":"list"},"_meta":{"original":"native-context"}}}` + "\n"
	var output, errors bytes.Buffer
	if code := NativeTools(directory, strings.NewReader(call), &output, &errors, nativeToolAdmission); code != 0 {
		t.Fatalf("original tools call: %d %s", code, &errors)
	}
	for _, name := range []string{"native-source.json", "native-events.jsonl", "answer.raw.txt", "answer.json"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("{}\n"), 0600); err != nil {
			t.Fatal(err)
		}
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
	if code := NativeTools(continued, strings.NewReader(call), &output, &errors, nativeToolAdmission); code != 0 {
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
	tools, err := newNativeWorkspaceTools(directory, nativeToolAdmission)
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
	if _, err := tools.call(json.RawMessage(`{"action":"comparison","base":"different"}`)); err == nil {
		t.Fatal("model supplied a mechanical snapshot reference")
	}
}

func TestNativeWorkspaceParallelProcessesAllocateOriginalFactsInAdmissionWindow(t *testing.T) {
	directory, _ := nativeToolFixture(t)
	var window sync.Mutex
	admit := func(directory string, request map[string]any) (func(), error) {
		if _, err := nativeToolAdmission(directory, request); err != nil {
			return nil, err
		}
		window.Lock()
		return window.Unlock, nil
	}
	first, err := newNativeWorkspaceTools(directory, admit)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newNativeWorkspaceTools(directory, admit)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan int, 2)
	errors := make(chan error, 2)
	for _, tools := range []*nativeWorkspaceTools{first, second} {
		go func(tools *nativeWorkspaceTools) {
			value, err := tools.call(json.RawMessage(`{"action":"list"}`))
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
	recovered, err := newNativeWorkspaceTools(directory, admit)
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
	if code := RunCommand([]string{"native-tools", directory}, bytes.NewReader(frame), &output, &errors, nativeToolAdmission); code != 0 {
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
	for _, role := range []string{"engine.gate", "engine.qa.review.rationale"} {
		t.Run(role, func(t *testing.T) {
			owner := t.TempDir()
			directory := filepath.Join(owner, "call")
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			validation := &protocol.ValidationInput{Workspace: &protocol.NativeResource{Path: workspace}}
			if role == "engine.gate" {
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
				if code := NativeTools(directory, bytes.NewReader(raw), &out, &errout, nativeToolAdmission); code != 0 {
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
			if role == "engine.gate" {
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
			if role == "engine.gate" && entry["testOwned"] != true {
				t.Fatal("listing lost exclusion ownership", entry)
			}
			_, failed = rpc(map[string]any{"action": "read", "listing": checks["listing"], "entry": entry["entry"]})
			if failed != (role == "engine.gate") {
				t.Fatal("owned read violates role boundary", role, failed)
			}
			ordinary, failed := rpc(map[string]any{"action": "read", "listing": listing["listing"], "entry": entries["developer_test.txt"]["entry"]})
			if failed || !strings.Contains(fmt.Sprint(ordinary["lines"]), "ordinary developer test after") {
				t.Fatal("ordinary developer tests not readable", ordinary)
			}
			_, failed = rpc(map[string]any{"action": "read", "listing": listing["listing"], "entry": entries["alias"]["entry"]})
			if failed != (role == "engine.gate") {
				t.Fatal("resolved owned alias violates role boundary", role)
			}
			tools, err := newNativeWorkspaceTools(directory, nativeToolAdmission)
			if err != nil {
				t.Fatal(err)
			}
			_, directErr := tools.path(owned, true)
			if (directErr != nil) != (role == "engine.gate") {
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
			if failed != (role == "engine.gate") {
				t.Fatal("resumed owned read violates role boundary")
			}
			resumed, err := newNativeWorkspaceTools(directory, nativeToolAdmission)
			if err != nil {
				t.Fatal(err)
			}
			for i, original := range facts {
				if !reflect.DeepEqual(resumed.facts[i], original) {
					t.Fatal("resume changed published fact identity")
				}
			}
			if role == "engine.gate" {
				for _, fact := range resumed.facts {
					if strings.Contains(fact.Content, "QA case body") {
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
	directory, workspace := nativeToolFixture(t)
	body := "package sample\nimport (\"testing\"; \"os\")\nfunc helper() {}\nfunc TestWrong(t *testing.T) { t.Fatal(\"wrong test executed\") }\nfunc TestChosen(t *testing.T) { if err := os.WriteFile(\"selected.txt\", []byte(\"chosen\"), 0600); err != nil { t.Fatal(err) } }\n"
	for name, value := range map[string]string{"sample_test.go": body, "go.mod": "module sample\ngo 1.22\n"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tools, err := newNativeWorkspaceTools(directory, nativeToolAdmission)
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
	if len(choices) != 2 || choices[0]["name"] != "TestWrong" || choices[1]["name"] != "TestChosen" || choices[1]["testSymbol"] != 2 {
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
	resumed, err := newNativeWorkspaceTools(directory, nativeToolAdmission)
	if err != nil {
		t.Fatal(err)
	}
	facts := make([]worker.LocationFact, 0, len(resumed.facts))
	for _, fact := range resumed.facts {
		facts = append(facts, worker.LocationFact{Kind: fact.Kind, Paths: fact.Paths, Content: fact.Content})
	}
	input := protocol.WorkerInput{RunID: "run", Handler: "engine.qa.design", Provider: "host", Pending: protocol.PendingAction{ActionID: "action", AttemptID: "attempt"}, Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewWhitebox}}}}
	raw := []byte(fmt.Sprintf(`{"cases":[{"scenario":"SUCCESS","oracle":"selected.txt contains chosen","files":[{"read":%d}],"testFile":1,"testSymbol":2}],"coverage":[]}`, originalRead))
	event, err := worker.DecodeResultWithFacts(input, "result", raw, "original-output", []string{}, facts)
	if err != nil {
		t.Fatal(err)
	}
	newRaw := []byte(fmt.Sprintf(`{"cases":[{"scenario":"SUCCESS","oracle":"created test passes","files":[{"read":%d}],"testFile":1,"testSymbol":1}],"coverage":[]}`, created["read"].(int)))
	newEvent, err := worker.DecodeResultWithFacts(input, "created-result", newRaw, "created-output", []string{}, facts)
	if err != nil || newEvent.Result.Validation.Manifest.Cases[0].TestRef != "sample_test.go::TestCreated" {
		t.Fatal("created code lost its symbol selection", err)
	}
	qaCase := event.Result.Validation.Manifest.Cases[0]
	if qaCase.TestRef != "sample_test.go::TestChosen" {
		t.Fatal("result used another read's symbol table", qaCase.TestRef)
	}
	operations, err := coverage.ResolveCaseOperations(workspace, qaCase)
	if err != nil || len(operations) != 1 {
		t.Fatal("selected test cannot execute", err)
	}
	operation := operations[0]
	cmd := exec.Command(operation.Operation.Argv[0], operation.Operation.Argv[1:]...)
	cmd.Dir = operation.Directory
	output, err := cmd.CombinedOutput()
	if err != nil || coverage.GoTestExecutionResult(output, operation.GoTestName) != "pass" {
		t.Fatalf("original selected test did not actually pass: %v\n%s", err, output)
	}
	value, err := os.ReadFile(filepath.Join(workspace, "selected.txt"))
	if err != nil || string(value) != "chosen" {
		t.Fatal("actual selected test side effect differs", err)
	}
	t.Logf("read=%d testSymbol=2 original=%s actual Go event=pass persisted=%q", originalRead, qaCase.TestRef, value)
}
