package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"

	"formal-gates/internal/host"
	"formal-gates/internal/settings"
)

func (p *nativePlatform) SelectionOptions(root, runtimeTarget string, native map[string]string, model string) (host.SelectionCatalogue, error) {
	values := maps.Clone(native)
	if values == nil {
		values = map[string]string{}
	}
	if err := p.ResolveNativePaths(values, map[string]string{}); err != nil {
		return host.SelectionCatalogue{}, err
	}
	binding, err := NativeBinding(p.InstallName, settings.Values{Native: values})
	if err != nil {
		return host.SelectionCatalogue{}, err
	}
	directory, err := os.MkdirTemp("", "formal-gates-selection-")
	if err != nil {
		return host.SelectionCatalogue{}, err
	}
	defer os.RemoveAll(directory)
	request := map[string]any{"provider": p.InstallName, "binding": binding, "workspace": root, "read_only": true, "probe": true, "selection_options": true, "selection_model": model}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
		return host.SelectionCatalogue{}, err
	}
	cmd := exec.Command(filepath.Join(runtimeTarget, "bin", "formal-gates"), "harness", "native", directory)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return host.SelectionCatalogue{}, fmt.Errorf("%s native model options: %w: %s", p.InstallName, err, stderr.String())
	}
	var result struct {
		Catalogue *host.SelectionCatalogue `json:"selection_options"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return host.SelectionCatalogue{}, err
	}
	if result.Catalogue == nil {
		return host.SelectionCatalogue{}, fmt.Errorf("%s: native model options are unavailable", p.InstallName)
	}
	return *result.Catalogue, nil
}

func nativeModelSelection(value any) host.ModelSelection {
	return host.ModelSelection{Model: nativeSelectionString(getOf(value, "model")), Effort: nativeSelectionString(getOf(value, "reasoning_effort"))}
}

func nativeSelectionString(value any) string { text, _ := value.(string); return text }

// Control messages inspect print-mode metadata without admitting a user turn.
func nativeControlRequest(client *nativeClient, subtype string) (any, error) {
	id := fmt.Sprintf("models-%d", client.nextCounter())
	if err := client.send(map[string]any{"type": "control_request", "request_id": id, "request": map[string]any{"subtype": subtype}}); err != nil {
		return nil, err
	}
	for {
		message, err := client.receive(nil)
		if err != nil {
			return nil, err
		}
		response := getOf(message, "response")
		if getOf(message, "type") != "control_response" || getOf(response, "request_id") != id {
			continue
		}
		if getOf(response, "subtype") != "success" {
			return nil, fmt.Errorf("%s: %s", subtype, textOf(getOf(response, "error")))
		}
		return getOf(response, "response"), nil
	}
}

func nativeSelectionControl(launch map[string]any, workspace, directory string, environment map[string]string) (*nativeClient, error) {
	return newNativeClientWithEnv(nativeHooks{transportPrefix: "NATIVE_SELECTION"}, nativeArgv(launch, "--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--no-session-persistence", "--tools", "", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--settings", `{"disableAllHooks":true}`), workspace, directory, environment)
}

func nativeStringValues(values any, key string) []string {
	if values == nil {
		return nil
	}
	out := []string{}
	for _, value := range listOf(values) {
		if key != "" {
			value = getOf(value, key)
		}
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
