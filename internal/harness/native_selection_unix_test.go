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
	"formal-gates/internal/settings"
)

func TestSelectionRuntimeCommand(t *testing.T) {
	if os.Getenv("FORMAL_GATES_SELECTION_RUNTIME") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) != 4 || args[1] != "harness" || args[2] != "native" {
		os.Exit(91)
	}
	directory := args[3]
	code := RunNative(directory, os.Stderr)
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		os.Exit(91)
	}
	profile, _ := readJSONFile(filepath.Join(directory, "profile.json"))
	_, sourceErr := os.Stat(filepath.Join(directory, "source.json"))
	executable, err := os.Executable()
	if err != nil {
		os.Exit(91)
	}
	if err := writeAtomicJSON(os.Getenv("FORMAL_GATES_SELECTION_RUNTIME_TRACE"), map[string]any{"argv": args[1:], "executable": executable, "request": request, "profile": profile, "sourceAbsent": os.IsNotExist(sourceErr)}); err != nil {
		os.Exit(91)
	}
	os.Exit(code)
}

func selectionRuntime(t *testing.T) (string, string) {
	t.Helper()
	root := resolveNativePath(t.TempDir())
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"bin/native.test", "internal/harness/" + zcodeCredentialHelper, "internal/harness/native_dsh.mjs"} {
		source := filepath.Base(path)
		if path == "bin/native.test" {
			source = executable
		}
		body, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, body, 0700); err != nil {
			t.Fatal(err)
		}
	}
	launcher := "#!/bin/sh\nexec '" + strings.ReplaceAll(filepath.Join(root, "bin", "native.test"), "'", "'\\''") + "' -test.timeout=0 -test.run='^TestSelectionRuntimeCommand$' -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(root, "bin", "formal-gates"), []byte(launcher), 0700); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(root, "native-command.json")
	t.Setenv("FORMAL_GATES_SELECTION_RUNTIME", "1")
	t.Setenv("FORMAL_GATES_SELECTION_RUNTIME_TRACE", trace)
	return root, trace
}

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
	runtimeTarget, _ := selectionRuntime(t)
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
	catalogue, err := adapter.(host.SelectionAdapter).SelectionOptions(root, runtimeTarget, native, "exact-model")
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
	if _, err := adapter.(host.SelectionAdapter).SelectionOptions(root, runtimeTarget, native, "exact-model"); err == nil || !strings.Contains(err.Error(), "CLAUDE_UNAVAILABLE") {
		t.Fatalf("missing installation: %v", err)
	}
}

func TestWorkBuddySelectionOptionsKeepNativeModelEfforts(t *testing.T) {
	root := resolveNativePath(t.TempDir())
	executable, echo := startPeer(t, peerScript{Args: selectionControlArgs(), Workspace: root, Steps: []peerStep{
		{On: `"subtype":"initialize"`, Send: selectionControlReply("models-1", `{"currentModelId":"original","account":{"token":"must-not-return"}}`)},
		{On: `"subtype":"get_available_models"`, Send: selectionControlReply("models-2", `{"rawModels":[{"id":"original","name":"Original","supportsReasoning":false},{"id":"selected","name":"Selected","supportsReasoning":true,"reasoning":{"defaultEffort":"native-high","supportedEfforts":["native-low","native-high"]}}]}`)},
	}})
	catalogue, err := workbuddySelectionOptions(map[string]any{"executable": executable}, root, t.TempDir(), map[string]string{}, "selected")
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
	choices, err := workbuddyCLIEfforts(map[string]any{"executable": executable}, root, map[string]string{})
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

func TestDSHSelectionOptionsUseAdmittedRuntimePlugin(t *testing.T) {
	runtimeTarget, trace := selectionRuntime(t)
	root, home := resolveNativePath(t.TempDir()), resolveNativePath(t.TempDir())
	nativeHome := filepath.Join(home, ".dsh")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	snapshot := `{"sessionId":"native-session","configOptions":[{"id":"model","currentValue":"native-model","options":[{"value":"native-model","name":"Native model"}]},{"id":"reasoning_effort","currentValue":"native-high","options":[{"value":"native-high","name":"High"}]}]}`
	executable, echo := startPeer(t, peerScript{Workspace: root, Environment: map[string]string{"HOME": home, "DSH_HOME": nativeHome}, Steps: []peerStep{
		{On: `"method":"initialize"`, Params: map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}}, Send: `{"id":"@ID@","result":{"protocolVersion":1,"agentCapabilities":{"sessionCapabilities":{"resume":{},"close":{}}}}}`},
		{On: `"method":"session/new"`, Params: map[string]any{"cwd": root, "mcpServers": []any{}}, Send: `{"id":"@ID@","result":` + snapshot + `}`},
		{On: `"method":"session/close"`, Params: map[string]any{"sessionId": "native-session"}, Send: `{"id":"@ID@","result":{}}`},
	}})
	adapter, err := host.LookupAdapter("dsh")
	if err != nil {
		t.Fatal(err)
	}
	native := map[string]string{"dshExecutable": executable, "dshConfigRoot": nativeHome}
	catalogue, err := adapter.(host.SelectionAdapter).SelectionOptions(root, runtimeTarget, native, "native-model")
	current := host.ModelSelection{Model: "native-model", Effort: "native-high"}
	want := host.SelectionCatalogue{Models: []host.ModelOption{{Value: "native-model", Label: "Native model", Efforts: []string{"native-high"}, DefaultEffort: "native-high"}}, Current: current, Default: current, Source: "ACP configOptions"}
	if err != nil || !reflect.DeepEqual(catalogue, want) || len(native) != 2 {
		t.Fatalf("DSH runtime catalogue or caller configuration changed: %+v %v %v", catalogue, err, native)
	}
	command, err := readJSONFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	args := listOf(getOf(command, "argv"))
	if len(args) != 3 || args[0] != "harness" || args[1] != "native" || getOf(command, "executable") != filepath.Join(runtimeTarget, "bin", "native.test") || getOf(command, "sourceAbsent") != true {
		t.Fatalf("DSH did not use the admitted native probe: %v", command)
	}
	request := getOf(command, "request")
	if getOf(request, "provider") != "dsh" || getOf(request, "workspace") != root || getOf(request, "probe") != true || getOf(request, "read_only") != true || getOf(request, "selection_options") != true || getOf(request, "selection_model") != "native-model" || getOf(request, "engine") != nil || getOf(request, "prompt") != nil {
		t.Fatalf("selection probe changed the native request boundary: %v", request)
	}
	profile := []any{
		map[string]any{"insert": []any{map[string]any{
			"id": "harness-native", "name": filepath.Join(runtimeTarget, "internal", "harness", "native_dsh.mjs"), "inject": []any{"tools", "sandboxPolicy"},
			"config": map[string]any{"directory": resolveNativePath(textOf(args[2])), "read_only": true, "executable": filepath.Join(runtimeTarget, "bin", "native.test"), "tools": nil, "managed_tools": false},
		}}},
		map[string]any{"id": "acp", "inject": []any{"acpAppStartup", "harnessNativeReady"}},
		map[string]any{"id": "session-title-llm", "disabled": true},
	}
	if !valuesEqual(getOf(command, "profile"), profile) {
		t.Fatalf("DSH native plugin did not belong to the admitted runtime: %v", command)
	}
	assertSelectionMethods(t, echo, []string{"initialize", "session/new", "session/close"})
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

func TestZCodeSelectionOptionsKeepNativeProviderPairAcrossLaunches(t *testing.T) {
	for _, paths := range []string{"absolute", "relative"} {
		t.Run(paths, func(t *testing.T) {
			runtimeTarget, trace := selectionRuntime(t)
			data := resolveNativePath(t.TempDir())
			store := filepath.Join(data, ".zcode", "v2", zcodeCredentialsFile)
			if err := os.MkdirAll(filepath.Dir(store), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(store, []byte(`{"account-provider:p1:identity":"fixture","account-provider:coding-plan:p1:account:fixture:api-key":"native-fixture-credential"}`), 0600); err != nil {
				t.Fatal(err)
			}
			active, personal := zcodeTestCatalogue(t, "23"), filepath.Join(data, "personal.json")
			base := "zcode-builtin:23:" + zcodePathHash(t, active)
			configuration := map[string]any{
				"revision": "account-config:" + base + ":p1", "basedOnZCodeBuiltinRevision": base,
				"providers": map[string]any{"p1": map[string]any{"access": map[string]any{"type": "zhipu-account", "entitled": true}}},
				"states":    map[string]any{"p1": map[string]any{"availability": "available", "entitled": true, "current": true}},
			}
			snapshot := `{"model":{"current":{"providerId":"p1","modelId":"native-model"},"available":[{"ref":{"providerId":"p1","modelId":"native-model"},"name":"Native model","reasoning":{"levels":[{"value":"native-low"},{"value":"native-high"}],"defaultLevel":"native-high"}}]},"thoughtLevel":{"current":"native-high"}}`
			s := newZcodeScenario(t, func(string) []peerStep {
				return []peerStep{
					{On: `"method":"runtime/capabilities"`, Params: map[string]any{}, Send: `{"id":"@ID@","result":{}}`},
					{On: `"method":"provider/updateAccountConfig"`, Params: configuration, Send: `{"id":"@ID@","result":{"status":"received","providerCount":1}}`},
					{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("native-session", snapshot)},
					{On: `"method":"session/resume"`, Send: zcodeSnapshotFrame("native-session", snapshot)},
					{On: `"method":"session/close"`, Params: map[string]any{"sessionId": "native-session"}, Send: `{"id":"@ID@","result":{}}`},
				}
			}, nil)
			cataloguePath, personalPath := active, personal
			if paths == "relative" {
				var err error
				cataloguePath, err = filepath.Rel(s.workspace, active)
				if err != nil {
					t.Fatal(err)
				}
				personalPath, err = filepath.Rel(s.workspace, personal)
				if err != nil {
					t.Fatal(err)
				}
			}
			script := filepath.Join(data, "app", "glm", "zcode.cjs")
			seed := filepath.Join(data, "app", "config", "provider", "zcode-builtin.json")
			for _, path := range []string{script, seed} {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			objectOrEmpty(getOf(s.request["binding"], "launch"))["script"] = script
			binding := zcodeTestNodePeer(t, s, data, filepath.Join(runtimeTarget, "internal", "harness", zcodeCredentialHelper))
			peerPath := filepath.Join(filepath.Dir(s.echoPath), "peer.json")
			peerBody, err := os.ReadFile(peerPath)
			if err != nil {
				t.Fatal(err)
			}
			var peer peerScript
			if err := json.Unmarshal(peerBody, &peer); err != nil {
				t.Fatal(err)
			}
			peer.Args = []string{script, "--cwd", s.workspace, "app-server"}
			peer.Workspace = s.workspace
			peer.Environment = map[string]string{"HOME": data, "ZCODE_DATA_BASE_DIR": data, zcodeBuiltinCatalogueEnv: cataloguePath, zcodePersonalConfigEnv: personalPath, zcodeCredentialHelperEnv: store}
			location := map[string]any{"workspacePath": s.workspace, "workspaceKey": s.workspace}
			peer.Steps[2].Params = map[string]any{"workspace": location, "persistence": "immediate", "titleGenerationEnabled": false}
			peer.Steps[3].Params = map[string]any{"workspace": location, "sessionId": "native-session"}
			if err := writeAtomicJSON(peerPath, peer); err != nil {
				t.Fatal(err)
			}
			t.Setenv(zcodeBuiltinCatalogueEnv, " "+cataloguePath+" ")
			t.Setenv(zcodePersonalConfigEnv, " "+personalPath+" ")
			t.Setenv(zcodeCredentialHelperEnv, store)
			native := map[string]string{"zcodeExecutable": textOf(getOf(binding["launch"], "executable")), "zcodeScript": script, "zcodeHome": data}
			adapter, err := host.LookupAdapter("zcode")
			if err != nil {
				t.Fatal(err)
			}
			want := host.SelectionCatalogue{Models: []host.ModelOption{{Value: `{"modelId":"native-model","providerId":"p1"}`, Label: "Native model", Efforts: []string{"native-low", "native-high"}, DefaultEffort: "native-high"}}, Current: host.ModelSelection{Model: `{"modelId":"native-model","providerId":"p1"}`, Effort: "native-high"}, Default: host.ModelSelection{Model: `{"modelId":"native-model","providerId":"p1"}`, Effort: "native-high"}, Source: "ZCode session settings"}
			for call := 0; call < 3; call++ {
				catalogue, err := adapter.(host.SelectionAdapter).SelectionOptions(s.workspace, runtimeTarget, native, want.Current.Model)
				if err != nil || !reflect.DeepEqual(catalogue, want) {
					t.Fatalf("native catalogue at launch %d: %+v %v", call, catalogue, err)
				}
				if call == 0 {
					if len(native) != 3 {
						t.Fatal("selection query changed the caller's native configuration", native)
					}
					if err := zcodeResolvePaths(native, map[string]string{}); err != nil {
						t.Fatal(err)
					}
					t.Setenv(zcodeBuiltinCatalogueEnv, "unrelated-catalogue.json")
					t.Setenv(zcodePersonalConfigEnv, "unrelated-personal.json")
					t.Setenv(zcodeCredentialHelperEnv, "unrelated-credentials.json")
				}
			}
			command, err := readJSONFile(trace)
			if err != nil || getOf(command, "executable") != filepath.Join(runtimeTarget, "bin", "native.test") || getOf(command, "sourceAbsent") != true || getOf(getOf(command, "request"), "probe") != true || getOf(getOf(command, "request"), "selection_options") != true {
				t.Fatalf("selection did not use the admitted runtime probe: %v %v", command, err)
			}
			frozen, err := zcodeConfiguredBinding(settings.Values{Native: native})
			if err != nil {
				t.Fatal(err)
			}
			if err := writeAtomicJSON(filepath.Join(s.directory, "native-request.json"), map[string]any{"provider": "zcode", "probe": true, "binding": frozen, "workspace": s.workspace, "read_only": true, "identity": "native-session", "selection_options": true}); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(filepath.Join(runtimeTarget, "bin", "formal-gates"), "harness", "native", s.directory)
			cmd.Dir = s.workspace
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("resumed native runtime: %v %s", err, out)
			}
			var result struct {
				Catalogue  host.SelectionCatalogue `json:"selection_options"`
				ModelCalls int                     `json:"model_calls"`
			}
			if err := json.Unmarshal(out, &result); err != nil || !reflect.DeepEqual(result.Catalogue, want) || result.ModelCalls != 0 {
				t.Fatalf("resumed native catalogue: %+v %v", result, err)
			}
			assertSelectionMethods(t, s.echoPath, []string{"runtime/capabilities", "provider/updateAccountConfig", "session/create", "session/close", "runtime/capabilities", "provider/updateAccountConfig", "session/create", "session/close", "runtime/capabilities", "provider/updateAccountConfig", "session/create", "session/close", "runtime/capabilities", "provider/updateAccountConfig", "session/resume", "session/close"})
		})
	}
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
