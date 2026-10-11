package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The original request and persisted facts retain the same workspace boundary across processes.
func TestNativeWorkspaceVisibilityAcrossProcesses(t *testing.T) {
	directory, workspace := nativeToolFixture(t)
	request, err := os.ReadFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	directory = filepath.Join(workspace, ".gates", "engine", "current", "native", "action", "call")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "native-request.json"), request, 0600); err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{
		"app.go":                         "needle ordinary source\n",
		".gates/input/requirements.md":   "needle confirmed requirement\n",
		".gates/engine/other/state.json": "needle another review result\n",
		".gates/tmp/other/native.json":   "needle another native result\n",
		".gates/results/other.md":        "needle another review report\n",
	}
	for path, body := range sources {
		target := filepath.Join(workspace, path)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(".gates/engine/other", filepath.Join(workspace, "runtime-alias")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if i == 3 {
			directory = filepath.Join(filepath.Dir(directory), "continued")
			if err := os.Mkdir(directory, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "native-request.json"), request, 0600); err != nil {
				t.Fatal(err)
			}
		}
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		child := exec.Command(executable, "-test.run=^TestNativeWorkspaceVisibilityProcess$")
		child.Env = append(os.Environ(), "FORMAL_GATES_WORKSPACE_TOOL_DIRECTORY="+directory)
		output, err := child.CombinedOutput()
		if err != nil {
			t.Fatalf("original tool process: %v\n%s", err, output)
		}
		t.Logf("process %d: %s", i+1, output)
	}
	tools, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range tools.facts {
		for _, path := range fact.Paths {
			if path != "app.go" && path != filepath.FromSlash(".gates/input/requirements.md") {
				t.Fatalf("published runtime fact: %+v", fact)
			}
		}
	}
	root := callWorkspace(t, tools, `{"action":"list"}`)
	for _, item := range root["entries"].([]any) {
		if item.(map[string]any)["name"] == "runtime-alias" {
			t.Fatal("runtime alias was listed", root)
		}
	}
	var gatesEntry int
	for _, item := range root["entries"].([]any) {
		row := item.(map[string]any)
		if row["name"] == ".gates" {
			gatesEntry = row["entry"].(int)
		}
	}
	gates := callWorkspace(t, tools, fmt.Sprintf(`{"action":"list","listing":%d,"entry":%d}`, root["listing"], gatesEntry))
	entries := gates["entries"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["name"] != "input" {
		t.Fatal("runtime namespace was listed", gates)
	}
	for path, body := range sources {
		actual, err := os.ReadFile(filepath.Join(workspace, path))
		if err != nil || string(actual) != body {
			t.Fatalf("original %s bytes changed: %q %v", path, actual, err)
		}
	}
}

func TestNativeWorkspaceVisibilityProcess(t *testing.T) {
	directory := os.Getenv("FORMAL_GATES_WORKSPACE_TOOL_DIRECTORY")
	if directory == "" {
		t.Skip("requires an original tool request")
	}
	var output, errors bytes.Buffer
	raw := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"workspace","arguments":{"action":"search","query":"needle"}}}`
	if code := NativeTools(directory, strings.NewReader(raw), &output, &errors, ExecutionServices{Admit: nativeToolAdmission}); code != 0 {
		t.Fatalf("MCP: %d %s", code, &errors)
	}
	var response struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			}
			IsError bool `json:"isError"`
		}
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Result.IsError {
		t.Fatal(output.String())
	}
	var result struct{ Matches []struct{ Path, Text string } }
	if err := json.Unmarshal([]byte(response.Result.Content[0].Text), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 2 {
		t.Fatal(response.Result.Content[0].Text)
	}
	wanted := map[string]string{"app.go": "needle ordinary source\n", ".gates/input/requirements.md": "needle confirmed requirement\n"}
	for _, match := range result.Matches {
		if wanted[filepath.ToSlash(match.Path)] != match.Text {
			t.Fatal("runtime or changed source returned", match)
		}
	}
	fmt.Print(response.Result.Content[0].Text)
}

func TestNativeWorkspacePublishedAliasesRespectRuntimeVisibility(t *testing.T) {
	directory, workspace := nativeToolFixture(t)
	tools, err := newNativeWorkspaceTools(directory, ExecutionServices{Admit: nativeToolAdmission})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(workspace, ".gates", "engine", "other", "state.json")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("another review result\n"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(workspace, "source.txt")
	if err := os.WriteFile(alias, []byte("original source\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root := callWorkspace(t, tools, `{"action":"list"}`)
	var entry int
	for _, item := range root["entries"].([]any) {
		row := item.(map[string]any)
		if row["name"] == "source.txt" {
			entry = row["entry"].(int)
		}
	}
	read := callWorkspace(t, tools, fmt.Sprintf(`{"action":"read","listing":%d,"entry":%d}`, root["listing"], entry))
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range []map[string]any{
		{"action": "read", "listing": root["listing"], "entry": entry},
		{"action": "list", "listing": root["listing"], "entry": entry},
		{"action": "search", "listing": root["listing"], "entry": entry, "query": "review"},
		{"action": "copy", "listing": root["listing"], "entry": entry, "name": "copy.txt"},
		{"action": "move", "listing": root["listing"], "entry": entry, "name": "move.txt"},
		{"action": "remove", "listing": root["listing"], "entry": entry},
		{"action": "replace", "read": read["read"], "from": 1, "to": 1, "content": "changed"},
		{"action": "create", "listing": root["listing"], "entry": entry, "name": "created.txt", "content": "changed"},
	} {
		raw, _ := json.Marshal(arguments)
		if _, err := tools.call(raw); err == nil {
			t.Fatalf("published alias bypassed visibility: %s", raw)
		}
	}
	actual, err := os.ReadFile(target)
	if err != nil || string(actual) != "another review result\n" {
		t.Fatal("runtime bytes changed", err)
	}
}
