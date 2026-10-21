package harness

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"formal-gates/internal/host"
	"formal-gates/internal/settings"
)

var workbuddyStream = streamAdapter{provider: "workbuddy", prefix: "WORKBUDDY", capture: workbuddyCaptureBinding, probe: workbuddyProbe, selectionOptions: workbuddySelectionOptions, arguments: workbuddyArguments, selection: streamSelection, checkSelection: workbuddyCheckSelection, sessionUnavailable: streamSessionUnavailable, stdinPrompt: true, cleanup: workbuddyCleanup, checkResult: workbuddyCheckResult, checkMessage: workbuddyCheckMessage, repeatedInit: workbuddyRepeatedInit, usageTranscript: workbuddyUsageTranscript}

func workbuddySelectionOptions(launch map[string]any, workspace, directory string, environment map[string]string, target string) (host.SelectionCatalogue, error) {
	catalogue := host.SelectionCatalogue{Source: "WorkBuddy initialize and get_available_models"}
	client, err := nativeSelectionControl(launch, workspace, directory, environment)
	if err != nil {
		return catalogue, err
	}
	defer client.close()
	initialized, err := nativeControlRequest(client, "initialize")
	if err != nil {
		return catalogue, err
	}
	models, err := nativeControlRequest(client, "get_available_models")
	if err != nil {
		return catalogue, err
	}
	catalogue.Current.Model = nativeSelectionString(getOf(initialized, "currentModelId"))
	catalogue.Default.Model = catalogue.Current.Model
	if target == "" {
		target = catalogue.Current.Model
	}
	for _, entry := range listOf(getOf(models, "rawModels")) {
		option := host.ModelOption{Value: nativeSelectionString(getOf(entry, "id")), Label: nativeSelectionString(getOf(entry, "name"))}
		reasoning := getOf(entry, "reasoning")
		effort := nativeSelectionString(getOf(reasoning, "defaultEffort"))
		if effort == "" {
			effort = nativeSelectionString(getOf(reasoning, "effort"))
		}
		if option.Value == catalogue.Current.Model {
			catalogue.Default.Effort = effort
		}
		if option.Value == target {
			option.Efforts = nativeStringValues(getOf(reasoning, "supportedEfforts"), "")
			if option.Efforts == nil && getOf(entry, "supportsReasoning") == true {
				option.Efforts, err = workbuddyCLIEfforts(launch, workspace, environment)
				if err != nil {
					return catalogue, err
				}
			}
			if getOf(entry, "supportsReasoning") == false {
				option.Efforts = []string{}
			}
			option.DefaultEffort = effort
		}
		catalogue.Models = append(catalogue.Models, option)
	}
	return catalogue, nil
}

// WorkBuddy's native selector uses the CLI effort choices when the selected
// reasoning model does not publish a narrower supportedEfforts list.
func workbuddyCLIEfforts(launch map[string]any, workspace string, environment map[string]string) ([]string, error) {
	argv := nativeArgv(launch, "--help")
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = workspace
	cmd.Env = nativeChildEnvironment(environment)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(output), "\n") {
		if !strings.Contains(line, "--effort ") {
			continue
		}
		_, choices, ok := strings.Cut(line, "(")
		if !ok {
			continue
		}
		choices, _, ok = strings.Cut(choices, ")")
		if !ok {
			continue
		}
		out := []string{}
		for _, effort := range strings.Split(choices, ",") {
			out = append(out, strings.TrimSpace(effort))
		}
		return out, nil
	}
	return nil, nil
}

var workbuddyCaptureBinding = nativeCapture(workbuddyResolvePaths, workbuddyConfiguredBinding)

func workbuddyResolvePaths(native, sources map[string]string) error {
	config := settings.Values{Native: native}
	executable := config.NativeString("workbuddyExecutable")
	path, executableSource, err := nativeExecutablePath(executable)
	script := ""
	if err != nil && executable == "codebuddy" {
		// cbc is the same CLI's second entry name.
		path, executableSource, err = nativeExecutablePath("cbc")
	}
	if err != nil && executable == "codebuddy" {
		// The WorkBuddy desktop launcher on PATH anchors the install root; the
		// bundled CLI is a node script inside its own resources tree.
		if candidate, _, ok := nativeAnchorLaunch([]string{"WorkBuddy"}, "resources", "app.asar.unpacked", "cli", "bin", "codebuddy"); ok {
			if node, nodeSource, nodeErr := nativeExecutablePath("node"); nodeErr == nil {
				path, script, executableSource, err = node, candidate, nodeSource, nil
			}
		}
	}
	if err != nil && executable == "codebuddy" && runtime.GOOS == "darwin" {
		path = "/Applications/WorkBuddy.app/Contents/Resources/app.asar.unpacked/cli/bin/codebuddy"
		_, err = os.Stat(path)
		executableSource = "platform default"
	}
	if err != nil {
		return &blockedError{message: "WORKBUDDY_UNAVAILABLE: configured native CLI is unavailable"}
	}
	home := config.NativeString("workbuddyHome")
	explicitHome := home != ""
	home, homeSource, err := nativeUserHome(home)
	if err != nil {
		return err
	}
	home = resolveNativePath(home)
	nativeHome := native["workbuddyConfigRoot"]
	rootSource := "workbuddyHome"
	if nativeHome == "" {
		if !explicitHome {
			nativeHome = os.Getenv("WORKBUDDY_CONFIG_DIR")
			if nativeHome != "" {
				rootSource = "WORKBUDDY_CONFIG_DIR"
			}
		}
		if nativeHome == "" {
			nativeHome = filepath.Join(home, ".workbuddy")
		}
	}
	nativeHome = resolveNativePath(nativeHome)
	product := config.NativeString("workbuddyProductConfig")
	productSource := "configured"
	if product == "" {
		product = os.Getenv("ACC_PRODUCT_CONFIG_PATH")
		productSource = "ACC_PRODUCT_CONFIG_PATH"
	}
	if product == "" {
		product = filepath.Join(nativeHome, "cache", "acc-product-config-v3.json")
		productSource = "workbuddyConfigRoot"
	}
	paths := map[string]string{
		"workbuddyExecutable": resolveNativePath(path), "workbuddyHome": home,
		"workbuddyConfigRoot": nativeHome, "workbuddyProductConfig": resolveNativePath(product),
	}
	origins := map[string]string{
		"workbuddyExecutable": executableSource, "workbuddyHome": homeSource,
		"workbuddyConfigRoot": rootSource, "workbuddyProductConfig": productSource,
	}
	if script != "" {
		paths["workbuddyScript"], origins["workbuddyScript"] = script, "PATH"
	}
	recordNativePaths(native, sources, paths, origins)
	return nil
}

func workbuddyConfiguredBinding(config settings.Values) (map[string]any, error) {
	path, home := config.Native["workbuddyExecutable"], config.Native["workbuddyHome"]
	nativeHome, product := config.Native["workbuddyConfigRoot"], config.Native["workbuddyProductConfig"]
	if path == "" || home == "" || nativeHome == "" || product == "" {
		return nil, &blockedError{message: "WORKBUDDY_CONFIG: frozen native paths required"}
	}
	if info, e := os.Stat(home); e != nil || !info.IsDir() {
		return nil, &blockedError{message: "WORKBUDDY_CONFIG: native home unavailable"}
	}
	if info, e := os.Stat(product); e != nil || !info.Mode().IsRegular() {
		return nil, &blockedError{message: "WORKBUDDY_CONFIG: native product configuration unavailable; start/sign in to WorkBuddy or set workbuddyProductConfig"}
	}
	environment := map[string]any{"HOME": home, "CODEBUDDY_CONFIG_DIR": nativeHome, "WORKBUDDY_CONFIG_DIR": nativeHome, "ACC_PRODUCT_CONFIG_PATH": product}
	if runtime.GOOS == "windows" {
		environment["USERPROFILE"] = home
	}
	// A PATH-anchored desktop install binds its bundled node script as the
	// launch's script operand; a directly installed CLI leaves it absent.
	launch := map[string]any{"executable": path}
	if script := config.Native["workbuddyScript"]; script != "" {
		launch["script"] = script
	}
	binding, err := buildCaptureBinding("workbuddy", launch)
	if err != nil {
		return nil, err
	}
	binding["environment"] = environment
	env := map[string]string{}
	for key, value := range environment {
		env[key] = textOf(value)
	}
	if _, err := workbuddyProbe(launch, home, env); err != nil {
		return nil, err
	}
	return binding, nil
}
func workbuddyProbe(launch map[string]any, workspace string, environment map[string]string) (map[string]any, error) {
	argv := nativeArgv(launch, "--help")
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = workspace
	cmd.Env = nativeChildEnvironment(environment)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	for _, capability := range []string{"--resume", "--session-id", "stream-json", "--mcp-config", "--tools", "--permission-mode", "dontAsk"} {
		if !strings.Contains(string(output), capability) {
			return nil, &blockedError{message: "WORKBUDDY_CAPABILITY: native CLI lacks " + capability}
		}
	}
	return map[string]any{"provider": "workbuddy", "executable": argv[0], "capabilities": map[string]any{"source": "native-cli-help", "selection_observed": false}, "selection": map[string]any{}, "model_calls": 0}, nil
}
func workbuddyValidateBinding(binding any) error { return validateNativeBinding(binding, "workbuddy") }
func workbuddyVerifySource(directory string, source any) error {
	if err := verifyNativeSource(directory, source, "workbuddy"); err != nil {
		return err
	}
	observed, err := streamObserve(workbuddyStream, directory)
	if err != nil {
		return err
	}
	if !valuesEqual(source, observed) {
		return &blockedError{message: "WORKBUDDY_BINDING: receipt differs from original native evidence"}
	}
	return nil
}
func workbuddyExecute(request any, directory string, probe bool, services ...ExecutionServices) (map[string]any, error) {
	return streamExecute(workbuddyStream, request, directory, probe)
}
func workbuddyReconcile(directory string, process any, services ...ExecutionServices) (map[string]any, error) {
	if original, err := readJSONFile(filepath.Join(directory, "native-source.json")); err == nil {
		defer workbuddyCleanup(textOf(getOf(original, "nativeWorkspace")))
	}
	return streamReconcile(workbuddyStream, directory, process)
}
func workbuddyArguments(request any, directory string, launch map[string]any, workspace string, environment map[string]string, prompt string) ([]string, string, error) {
	effort, _ := getOf(request, "reasoning_effort").(string)
	identity, _ := getOf(request, "identity").(string)
	resume := identity != ""
	if resume {
		record, err := workbuddySessionRecordPath(environment["CODEBUDDY_CONFIG_DIR"], identity)
		if err != nil {
			return nil, "", err
		}
		if record == "" {
			return nil, "", &sessionUnavailableError{blockedError: blockedError{message: "WORKBUDDY_RECOVERY: native session record is unavailable"}, identity: identity, method: "session/store"}
		}
	}
	if !resume {
		var uuid [16]byte
		if _, err := rand.Read(uuid[:]); err != nil {
			return nil, "", err
		}
		uuid[6] = (uuid[6] & 15) | 64
		uuid[8] = (uuid[8] & 63) | 128
		identity = fmt.Sprintf("%x-%x-%x-%x-%x", uuid[:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:])
	}
	if filepath.Base(identity) != identity {
		return nil, "", &blockedError{message: "WORKBUDDY_BINDING: invalid native session identity"}
	}
	// The native store includes cwd in its filename; a short session-owned cwd
	// keeps isolated workspaces usable. All file operations use the engine MCP tool.
	cwd := filepath.Join(os.TempDir(), "formal-gates-workbuddy", identity)
	if err := writeAtomicJSON(filepath.Join(directory, "native-source.json"), map[string]any{"sessionId": "", "workspace": workspace, "nativeWorkspace": cwd, "selection": map[string]any{}}); err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(cwd, 0700); err != nil {
		return nil, "", err
	}
	server, err := nativeToolServer(directory)
	if err != nil {
		return nil, "", err
	}
	config, err := canonicalBody(map[string]any{"mcpServers": map[string]any{"formal-gates": map[string]any{"command": server["command"], "args": server["args"], "alwaysLoad": true}}})
	if err != nil {
		return nil, "", err
	}
	configPath := filepath.Join(directory, "mcp.json")
	if err := os.WriteFile(configPath, config, 0600); err != nil {
		return nil, "", err
	}
	permission := "bypassPermissions"
	if getOf(request, "read_only") == true {
		permission = "dontAsk"
	}
	argv := nativeArgv(launch, "--print", "--verbose", "--output-format", "stream-json", "--input-format", "text", "--tools", "", "--strict-mcp-config", "--mcp-config", configPath, "--setting-sources", "", "--settings", `{"disableAllHooks":true}`, "--permission-mode", permission, "--allowedTools", "mcp__formal-gates__workspace,mcp__formal-gates__submit_semantics")
	if model, _ := getOf(request, "model").(string); model != "" {
		argv = append(argv, "--model", model)
	}
	if effort != "" {
		argv = append(argv, "--effort", effort)
	}
	if resume {
		argv = append(argv, "--resume", identity)
	} else {
		argv = append(argv, "--session-id", identity)
	}
	return argv, cwd, nil
}
func workbuddyCleanup(path string) {
	if filepath.Dir(path) == filepath.Join(os.TempDir(), "formal-gates-workbuddy") && filepath.Base(path) != "." {
		_ = os.RemoveAll(path)
	}
}
func workbuddyCheckSelection(request any, selection map[string]any) error {
	for _, key := range []string{"model", "reasoning_effort"} {
		requested, _ := getOf(request, key).(string)
		observed, _ := selection[key].(string)
		if requested != "" && observed != "" && requested != observed {
			return &blockedError{message: "WORKBUDDY_CONFIG: native " + key + " differs from requested selection"}
		}
	}
	return nil
}
func workbuddyCheckResult(result any) error {
	if getOf(result, "subtype") != "success" || textOf(getOf(result, "uuid")) == "" {
		return &blockedError{message: "WORKBUDDY_RESULT: successful native result UUID required"}
	}
	return nil
}

func workbuddyRepeatedInit(initial, message map[string]any) bool {
	if message["type"] != "system" || message["subtype"] != "init" {
		return false
	}
	first, repeated := maps.Clone(initial), maps.Clone(message)
	delete(first, "__timestamp")
	delete(repeated, "__timestamp")
	return valuesEqual(first, repeated)
}

func workbuddyCheckMessage(message any) error {
	if getOf(message, "type") != "assistant" {
		return nil
	}
	content := listOf(getOf(getOf(message, "message"), "content"))
	if len(content) != 1 || getOf(content[0], "type") != "text" {
		return nil
	}
	text, _ := getOf(content[0], "text").(string)
	if text == "Authentication required. Please use /login command to sign in to your account" || text == "Authentication failed. Please use /login command to sign in to your account" {
		return &blockedError{message: "WORKBUDDY_AUTH: native authentication unavailable"}
	}
	return nil
}

func workbuddyInvocationCleanup(directory string) error {
	source, err := readJSONFile(filepath.Join(directory, "native-source.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	workbuddyCleanup(textOf(getOf(source, "nativeWorkspace")))
	return nil
}

func workbuddySessionRecordPath(configRoot, identity string) (string, error) {
	if !filepath.IsAbs(configRoot) || identity == "" || filepath.Base(identity) != identity {
		return "", &blockedError{message: "WORKBUDDY_RECOVERY: native config root and exact session identity required"}
	}
	entries, err := os.ReadDir(filepath.Join(configRoot, "projects"))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	match := ""
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(configRoot, "projects", entry.Name(), identity+".jsonl")
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			if match != "" {
				return "", &blockedError{message: "WORKBUDDY_RECOVERY: native session identity is ambiguous across project stores"}
			}
			match = path
		}
	}
	return match, nil
}

// The print UUID is the SDK item ID. The original session's usage keeps the
// cache classification that the print AssistantMessage does not carry.
func workbuddyUsageTranscript(directory string, request map[string]any, identity string, assistantIDs []string) (string, error) {
	path := filepath.Join(directory, "native-usage.json")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	root := textOf(getOf(getOf(getOf(request, "binding"), "environment"), "CODEBUDDY_CONFIG_DIR"))
	record, err := workbuddySessionRecordPath(root, identity)
	if err != nil {
		return "", err
	}
	if record == "" {
		return "", fmt.Errorf("workbuddy native usage record unavailable")
	}
	file, err := os.Open(record)
	if err != nil {
		return "", err
	}
	defer file.Close()
	wanted := map[string]bool{}
	for _, id := range assistantIDs {
		if id == "" {
			return "", fmt.Errorf("workbuddy assistant UUID unavailable")
		}
		wanted[id] = false
	}
	projected := []any{}
	reader := bufio.NewReaderSize(file, 64*1024)
	for {
		line, readErr := readNativeLine(reader)
		if line != "" {
			raw, err := decodeNativeJSON([]byte(line))
			if err != nil {
				return "", err
			}
			item := objectOrEmpty(raw)
			id := textOf(item["id"])
			if _, selected := wanted[id]; selected {
				wanted[id] = true
				data := objectOrEmpty(item["providerData"])
				if usage, ok := data["usage"].(map[string]any); ok {
					values := map[string]any{"usage": workbuddyUsageFields(usage)}
					if raw, ok := data["rawUsage"].(map[string]any); ok {
						values["rawUsage"] = workbuddyUsageFields(raw)
					}
					projected = append(projected, map[string]any{"id": id, "session_id": identity, "providerData": values})
				}
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				return "", readErr
			}
			break
		}
	}
	for _, found := range wanted {
		if !found {
			return "", fmt.Errorf("workbuddy assistant UUID absent from native usage record")
		}
	}
	if err := writeAtomicJSON(path, projected); err != nil {
		return "", err
	}
	return path, nil
}

func workbuddyUsageFields(value map[string]any) map[string]any {
	result := nativePickParams(value, "inputTokens", "outputTokens", "prompt_tokens", "cache_read_input_tokens", "cacheReadInputTokens", "cache_creation_input_tokens", "cacheCreationInputTokens", "prompt_cache_hit_tokens", "prompt_cache_miss_tokens", "prompt_cache_write_tokens")
	for _, key := range []string{"inputTokensDetails", "prompt_tokens_details"} {
		if details, present := value[key]; present {
			if items, ok := details.([]any); ok {
				filtered := []any{}
				for _, item := range items {
					filtered = append(filtered, nativePickParams(objectOrEmpty(item), "cached_tokens"))
				}
				result[key] = filtered
			} else if item, ok := details.(map[string]any); ok {
				result[key] = nativePickParams(item, "cached_tokens")
			} else {
				result[key] = details
			}
		}
	}
	return result
}
