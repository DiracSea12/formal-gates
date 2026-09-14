package facade

import (
	_ "embed"
	"fmt"
	"path/filepath"
)

//go:embed runtime_tools_dsh.mjs
var dshNativeOperationPlugin []byte

// dshOperationPatch composes with the installed ACP profile. It uses DSH's own
// tools registry and monotonic guard; no independent permission system is added.
func dshOperationPatch(pluginPath, executable, root, runID, actionID, requestDirectory, step, description string, deniedTools []string) ([]any, error) {
	for _, path := range []string{pluginPath, executable, root, requestDirectory} {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("DSH_CONFIG: native operation paths must be absolute")
		}
	}
	if runID == "" || actionID == "" || deniedTools == nil {
		return nil, fmt.Errorf("DSH_CONFIG: run, action and explicit native tool policy required")
	}
	tool := nativeOperationTool(step, description)
	schema := tool["inputSchema"].(map[string]any)
	schema["required"] = append(schema["required"].([]string), "requestId")
	schema["properties"].(map[string]any)["requestId"] = map[string]any{"type": "string", "description": "Stable operation ID; preserve across recovery."}
	return []any{
		map[string]any{"insert": []any{map[string]any{
			"id": "formal-gates-native-operation", "name": pluginPath,
			"config": map[string]any{"executable": executable, "root": root, "runId": runID, "actionId": actionID, "requestDirectory": requestDirectory, "sourceInfoPath": filepath.Join(filepath.Dir(requestDirectory), "native-source.json"), "tool": tool, "deniedTools": append([]string{}, deniedTools...)},
		}}},
		map[string]any{"id": "agent-loop", "config": map[string]any{"agents": []any{}}},
		map[string]any{"id": "acp", "inject": []string{"acpAppStartup", "formalGatesNativeReady"}},
		// This child already uses the bound engine operation plugin. Do not load
		// the owner's installed lifecycle hook again or inherit its launcher.
		map[string]any{"id": "formal-gates-dsh", "disabled": true},
	}, nil
}
