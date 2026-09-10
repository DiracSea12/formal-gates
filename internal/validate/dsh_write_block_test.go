package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writeStateJSON(t *testing.T, runDir string, state map[string]any) {
	t.Helper()
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "state.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeBindingJSON(t *testing.T, path, dispatchID, identity string) {
	t.Helper()
	data, err := json.Marshal(map[string]string{
		"runId":      "run-1",
		"dispatchId": dispatchID,
		"provider":   "deepseek-harness",
		"identity":   identity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func dshWritePayload(root, identity string) []byte {
	payload := fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q},"agent_id":%q}`, root, filepath.ToSlash(filepath.Join(root, "internal", "code.go")), identity)
	return []byte(payload)
}
