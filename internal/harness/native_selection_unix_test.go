//go:build !windows

package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/host"
)

func selectionControlArgs() []string {
	return []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--no-session-persistence", "--tools", "", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--settings", `{"disableAllHooks":true}`}
}

func selectionControlReply(id, response string) string {
	return `{"type":"control_response","response":{"subtype":"success","request_id":"` + id + `","response":` + response + `}}`
}

func assertSelectionMethods(t *testing.T, path string, expected []string) {
	t.Helper()
	methods := []string{}
	for _, line := range strings.Split(strings.TrimSpace(readTestFile(t, path)), "\n") {
		frame := decodeTestJSON(t, line)
		method := nativeSelectionString(getOf(frame, "method"))
		if method == "" {
			if getOf(frame, "type") != "control_request" {
				t.Fatalf("unexpected selection frame: %s", line)
			}
			method = nativeSelectionString(getOf(getOf(frame, "request"), "subtype"))
			if !valuesEqual(getOf(frame, "request"), map[string]any{"subtype": method}) {
				t.Fatalf("selection control carried additional input: %s", line)
			}
		}
		methods = append(methods, method)
	}
	if !reflect.DeepEqual(methods, expected) {
		t.Fatalf("selection methods: %v != %v", methods, expected)
	}
}

func TestClaudeSelectionOptionsUseRegisteredBindingWithoutUserTurn(t *testing.T) {
	root, home := resolveNativePath(t.TempDir()), resolveNativePath(t.TempDir())
	executable, echo := startPeer(t, peerScript{Args: selectionControlArgs(), Workspace: root, Environment: map[string]string{"HOME": home, "CLAUDE_CONFIG_DIR": filepath.Join(home, ".claude")}, Steps: []peerStep{
		{On: `"subtype":"initialize"`, Send: selectionControlReply("models-1", `{"models":[{"value":"native-alias","resolvedModel":"exact-model","displayName":"Native label","supportsEffort":true,"supportedEffortLevels":["native-small","native-large"]}],"account":{"token":"must-not-return"}}`)},
		{On: `"subtype":"get_settings"`, Send: selectionControlReply("models-2", `{"effective":{"model":"native-alias","effortLevel":"native-small"}}`)},
	}})
	adapter, err := host.LookupAdapter("claude")
	if err != nil {
		t.Fatal(err)
	}
	native := map[string]string{"claudeExecutable": executable, "claudeHome": home}
	catalogue, err := adapter.(host.SelectionAdapter).SelectionOptions(root, native, "exact-model")
	if err != nil {
		t.Fatal(err)
	}
	want := host.ModelOption{Value: "exact-model", Label: "Native label", Efforts: []string{"native-small", "native-large"}, DefaultEffort: "native-small"}
	if len(catalogue.Models) != 1 || !reflect.DeepEqual(catalogue.Models[0], want) || catalogue.Current != (host.ModelSelection{Model: "exact-model", Effort: "native-small"}) || catalogue.Default != catalogue.Current {
		t.Fatalf("native catalogue: %+v", catalogue)
	}
	encoded, _ := json.Marshal(catalogue)
	if len(native) != 2 || strings.Contains(string(encoded), "must-not-return") {
		t.Fatal("selection query mutated configuration or exposed credentials")
	}
	assertSelectionMethods(t, echo, []string{"initialize", "get_settings"})
	native["claudeExecutable"] = filepath.Join(home, "absent-cli")
	if _, err := adapter.(host.SelectionAdapter).SelectionOptions(root, native, "exact-model"); err == nil || !strings.Contains(err.Error(), "CLAUDE_UNAVAILABLE") {
		t.Fatalf("missing installation: %v", err)
	}
}

func TestWorkBuddySelectionOptionsKeepNativeModelEfforts(t *testing.T) {
	root := resolveNativePath(t.TempDir())
	executable, echo := startPeer(t, peerScript{Args: selectionControlArgs(), Workspace: root, Steps: []peerStep{
		{On: `"subtype":"initialize"`, Send: selectionControlReply("models-1", `{"currentModelId":"original","account":{"token":"must-not-return"}}`)},
		{On: `"subtype":"get_available_models"`, Send: selectionControlReply("models-2", `{"rawModels":[{"id":"original","name":"Original","supportsReasoning":false},{"id":"selected","name":"Selected","supportsReasoning":true,"reasoning":{"defaultEffort":"native-high","supportedEfforts":["native-low","native-high"]}}]}`)},
	}})
	catalogue, err := workbuddySelectionOptions(executable, root, t.TempDir(), map[string]string{}, "selected")
	if err != nil {
		t.Fatal(err)
	}
	if len(catalogue.Models) != 2 || !reflect.DeepEqual(catalogue.Models[1].Efforts, []string{"native-low", "native-high"}) || catalogue.Models[0].Efforts != nil || catalogue.Models[1].DefaultEffort != "native-high" || catalogue.Default != (host.ModelSelection{Model: "original"}) || catalogue.Current.Effort != "" {
		t.Fatalf("native catalogue: %+v", catalogue)
	}
	encoded, _ := json.Marshal(catalogue)
	if strings.Contains(string(encoded), "must-not-return") {
		t.Fatal("selection catalogue exposed account metadata")
	}
	assertSelectionMethods(t, echo, []string{"initialize", "get_available_models"})
}

func TestWorkBuddyReasoningSelectorUsesNativeCLIChoices(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "native-cli")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n[ \"$#\" -eq 1 ] && [ \"$1\" = --help ] || exit 91\nprintf '%s\\n' '  --effort <level> Reasoning effort level (native-small, native-large)'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	choices, err := workbuddyCLIEfforts(executable, root, map[string]string{})
	if err != nil || !reflect.DeepEqual(choices, []string{"native-small", "native-large"}) {
		t.Fatalf("native choices: %v %v", choices, err)
	}
}

func TestCodexSelectionOptionsReadNativePagesAndConfiguration(t *testing.T) {
	root := resolveNativePath(t.TempDir())
	executable, echo := startPeer(t, peerScript{Workspace: root, Steps: []peerStep{
		{On: `"method":"initialize"`, Params: map[string]any{"clientInfo": map[string]any{"name": "formal-gates", "title": "Formal Gates", "version": "1"}, "capabilities": map[string]any{"experimentalApi": true}}, Send: `{"id":"@ID@","result":{}}`},
		{On: `"method":"config/read"`, Params: map[string]any{"cwd": root, "includeLayers": false}, Send: `{"id":"@ID@","result":{"config":{"model":"original","model_reasoning_effort":"native-small"}}}`},
		{On: `"method":"model/list"`, Params: map[string]any{"includeHidden": true}, Send: `{"id":"@ID@","result":{"data":[{"id":"row-one","model":"original","displayName":"Original","isDefault":true,"defaultReasoningEffort":"native-small","supportedReasoningEfforts":[{"reasoningEffort":"native-small"}]}],"nextCursor":"opaque-page"}}`},
		{On: `"cursor":"opaque-page"`, Params: map[string]any{"includeHidden": true, "cursor": "opaque-page"}, Send: `{"id":"@ID@","result":{"data":[{"id":"row-two","model":"selected","displayName":"Selected","defaultReasoningEffort":"native-large","supportedReasoningEfforts":[{"reasoningEffort":"native-large"}]}],"nextCursor":null}}`},
	}})
	binding, err := buildCaptureBinding("codex", map[string]any{"executable": executable})
	if err != nil {
		t.Fatal(err)
	}
	result, err := codexExecute(map[string]any{"binding": binding, "workspace": root, "selection_options": true, "selection_model": "selected"}, t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	catalogue := result["selection_options"].(host.SelectionCatalogue)
	if len(catalogue.Models) != 2 || catalogue.Models[0].Efforts != nil || catalogue.Models[1].Value != "selected" || !reflect.DeepEqual(catalogue.Models[1].Efforts, []string{"native-large"}) || catalogue.Models[1].DefaultEffort != "native-large" || catalogue.Default != (host.ModelSelection{Model: "original", Effort: "native-small"}) {
		t.Fatalf("native catalogue: %+v", catalogue)
	}
	assertSelectionMethods(t, echo, []string{"initialize", "initialized", "config/read", "model/list", "model/list"})
}

func TestDSHSelectionOptionsQueryOnlyTargetFreshSession(t *testing.T) {
	for _, target := range []string{`["provider","selected"]`, "absent"} {
		t.Run(target, func(t *testing.T) {
			options := func(model, effort string) string {
				return mustCanonical(t, map[string]any{"configOptions": []any{
					map[string]any{"id": "model", "currentValue": model, "options": []any{map[string]any{"group": "provider", "name": "Provider", "options": []any{map[string]any{"value": `["provider","original"]`, "name": "Original"}, map[string]any{"value": `["provider","selected"]`, "name": "Selected"}}}}},
					map[string]any{"id": "reasoning_effort", "currentValue": effort, "options": []any{map[string]any{"value": effort, "name": "Effort"}}},
				}})
			}
			created := objectOrEmpty(decodeTestJSON(t, options(`["provider","original"]`, "native-small")))
			created["sessionId"] = "fresh-session"
			steps := []peerStep{
				{On: `"method":"initialize"`, Params: map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}}, Send: `{"id":"@ID@","result":{"protocolVersion":1,"agentCapabilities":{"sessionCapabilities":{"resume":{},"close":{}}}}}`},
				{On: `"method":"session/new"`, Send: `{"id":"@ID@","result":` + mustCanonical(t, created) + `}`},
				{On: `"method":"session/set_config_option"`, Params: map[string]any{"sessionId": "fresh-session", "configId": "model", "value": `["provider","selected"]`}, Send: `{"id":"@ID@","result":` + options(`["provider","selected"]`, "native-large") + `}`},
				{On: `"method":"session/close"`, Params: map[string]any{"sessionId": "fresh-session"}, Send: `{"id":"@ID@","result":{}}`},
			}
			s := newDSHScenario(t, steps, func(request map[string]any) { request["selection_options"], request["selection_model"] = true, target })
			result, err := dshExecute(s.request, s.directory, true)
			if err != nil {
				t.Fatal(err)
			}
			catalogue := result["selection_options"].(host.SelectionCatalogue)
			methods := []string{"initialize", "session/new"}
			if target != "absent" {
				methods = append(methods, "session/set_config_option")
				if !reflect.DeepEqual(catalogue.Models[1].Efforts, []string{"native-large"}) || catalogue.Models[1].DefaultEffort != "native-large" {
					t.Fatalf("target reasoning: %+v", catalogue)
				}
			}
			if len(catalogue.Models) != 2 || catalogue.Current.Model != `["provider","original"]` || catalogue.Default != (host.ModelSelection{Model: `["provider","original"]`, Effort: "native-small"}) {
				t.Fatalf("original choices: %+v", catalogue)
			}
			assertSelectionMethods(t, s.echoPath, append(methods, "session/close"))
		})
	}
}

func TestZCodeSelectionOptionsKeepOpaqueReferenceAndAdvertisedEffort(t *testing.T) {
	settings := `{"model":{"current":{"providerId":"opaque:account","modelId":"original"},"available":[{"ref":{"providerId":"opaque:account","modelId":"original"},"name":"Original"},{"ref":{"providerId":"opaque:account","modelId":"selected"},"name":"Selected","reasoning":{"levels":[{"value":"native-large"}],"defaultLevel":"native-large"}}]},"thoughtLevel":{"current":"native-small"}}`
	s := newZcodeScenario(t, func(string) []peerStep {
		return []peerStep{
			{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("fresh-session", settings)},
			{On: `"method":"session/close"`, Params: map[string]any{"sessionId": "fresh-session"}, Send: `{"id":"@ID@","result":{}}`},
		}
	}, func(request map[string]any) {
		request["selection_options"], request["selection_model"] = true, `{"modelId":"selected","providerId":"opaque:account"}`
	})
	result, err := zcodeExecute(s.request, s.directory, true)
	if err != nil {
		t.Fatal(err)
	}
	catalogue := result["selection_options"].(host.SelectionCatalogue)
	if len(catalogue.Models) != 2 || catalogue.Models[1].Value != `{"modelId":"selected","providerId":"opaque:account"}` || catalogue.Models[1].Label != "Selected" || !reflect.DeepEqual(catalogue.Models[1].Efforts, []string{"native-large"}) || catalogue.Models[1].DefaultEffort != "native-large" || catalogue.Default.Effort != "native-small" {
		t.Fatalf("native catalogue: %+v", catalogue)
	}
	assertSelectionMethods(t, s.echoPath, []string{"session/create", "session/close"})
}

func TestCursorSelectionOptionsRetainUnreportedEfforts(t *testing.T) {
	catalogue, err := cursorSelectionOptions(func(args ...string) *exec.Cmd {
		if !reflect.DeepEqual(args, []string{"models"}) {
			t.Fatalf("metadata command: %v", args)
		}
		return exec.Command("/bin/sh", "-c", "printf '%s\\n' 'Available models' '' 'native-original - Original (current)' 'native-selected - Selected (default)' '' 'Tip: use --model <id>'")
	})
	if err != nil || len(catalogue.Models) != 2 || catalogue.Current.Model != "native-original" || catalogue.Default.Model != "native-selected" || catalogue.Models[0].Efforts != nil || catalogue.Models[1].Efforts != nil {
		t.Fatalf("native catalogue: %+v %v", catalogue, err)
	}
	encoded, err := json.Marshal(catalogue)
	if err != nil || !strings.Contains(string(encoded), `"efforts":null`) {
		t.Fatalf("unknown effort projection: %s %v", encoded, err)
	}
}
