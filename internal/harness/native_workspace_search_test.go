package harness

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNativeWorkspaceSearchPublishesOriginalVisibleReads(t *testing.T) {
	workspace, directory := t.TempDir(), t.TempDir()
	sources := map[string]string{
		"app.go":                         "package app\n// needle alpha\n",
		"pkg/app_test.go":                "package app\nimport \"testing\"\nfunc TestNeedle(t *testing.T) {} // needle beta\n",
		".gates/input/task.txt":          "needle confirmed input\n",
		".gates/engine/other/state.json": "needle other role secret\n",
	}
	for name, body := range sources {
		path := filepath.Join(workspace, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), map[string]any{"workspace": workspace, "read_only": true}); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	call := func(args string) map[string]any {
		t.Helper()
		var output, errors bytes.Buffer
		raw := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"workspace","arguments":` + args + `}}`
		if code := NativeTools(directory, strings.NewReader(raw), &output, &errors); code != 0 {
			t.Fatalf("tool transport %d: %s", code, &errors)
		}
		var response struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
				IsError bool `json:"isError"`
			} `json:"result"`
		}
		if err := json.Unmarshal(output.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Result.IsError {
			t.Fatal(response.Result.Content)
		}
		var value map[string]any
		if err := json.Unmarshal([]byte(response.Result.Content[0].Text), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	result := call(`{"action":"search","query":"needle"}`)
	matches := result["matches"].([]any)
	if len(matches) != 3 {
		t.Fatalf("visible matches: %+v", matches)
	}
	wantLines := map[string]int{"app.go": 2, "pkg/app_test.go": 3, ".gates/input/task.txt": 1}
	tools, err := newNativeWorkspaceTools(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range matches {
		row := raw.(map[string]any)
		path := row["path"].(string)
		sourcePath := filepath.ToSlash(path)
		if int(row["line"].(float64)) != wantLines[sourcePath] || !strings.Contains(row["text"].(string), "needle") {
			t.Fatal(row)
		}
		read, err := tools.fact(int(row["read"].(float64)), "read")
		if err != nil {
			t.Fatal(err)
		}
		listing, err := tools.fact(int(row["listing"].(float64)), "listing")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(read.Paths, []string{sourcePath}) || !bytes.Equal(read.Content, []byte(sources[sourcePath])) || listing.Paths[int(row["entry"].(float64))-1] != sourcePath {
			t.Fatalf("original search evidence: %+v %+v", read, listing)
		}
		published := call(`{"action":"read","listing":` + workspaceNumber(row["listing"]) + `,"entry":` + workspaceNumber(row["entry"]) + `}`)
		if sourcePath == "pkg/app_test.go" && len(published["testSymbols"].([]any)) != 1 {
			t.Fatal("search did not retain original symbol read", published)
		}
	}
	root := call(`{"action":"list"}`)
	var listing, entry any
	for _, item := range root["entries"].([]any) {
		choice := item.(map[string]any)
		if choice["name"] == "pkg" {
			listing, entry = root["listing"], choice["entry"]
		}
	}
	selected := call(`{"action":"search","query":"needle","listing":` + workspaceNumber(listing) + `,"entry":` + workspaceNumber(entry) + `}`)
	if rows := selected["matches"].([]any); len(rows) != 1 || rows[0].(map[string]any)["path"] != filepath.Join("pkg", "app_test.go") {
		t.Fatal(selected)
	}
	if rows := call(`{"action":"search","query":"absent literal"}`)["matches"].([]any); len(rows) != 0 {
		t.Fatal(rows)
	}
	if rows := call(`{"action":"search","query":"other role secret"}`)["matches"].([]any); len(rows) != 0 {
		t.Fatal("search leaked invisible role", rows)
	}
	for name, body := range sources {
		actual, err := os.ReadFile(filepath.Join(workspace, name))
		if err != nil || string(actual) != body {
			t.Fatalf("search changed %s", name)
		}
	}
	if after, err := os.Getwd(); err != nil || after != cwd {
		t.Fatal("search changed cwd", after, err)
	}
	if _, err := tools.invoke(nativeWorkspaceTool, json.RawMessage(`{"action":"create","name":"changed.go","content":"changed"}`)); err == nil {
		t.Fatal("search bypassed read-only policy")
	}
	if err := verifyNativeToolEvidence(directory); err != nil {
		t.Fatal(err)
	}
}

func workspaceNumber(value any) string { body, _ := json.Marshal(value); return string(body) }
