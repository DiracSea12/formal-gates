package harness

import (
	"crypto/rand"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"formal-gates/internal/settings"
)

var workbuddyStream = streamAdapter{provider: "workbuddy", prefix: "WORKBUDDY", capture: workbuddyCaptureBinding, probe: workbuddyProbe, arguments: workbuddyArguments, selection: streamSelection, checkSelection: workbuddyCheckSelection, sessionUnavailable: streamSessionUnavailable, stdinPrompt: true, cleanup: workbuddyCleanup, checkResult: workbuddyCheckResult, checkMessage: workbuddyCheckMessage, repeatedInit: workbuddyRepeatedInit}

func workbuddyCaptureBinding() (map[string]any, error) {
	return workbuddyConfiguredBinding(settings.Values{})
}
func workbuddyConfiguredBinding(config settings.Values) (map[string]any, error) {
	executable := config.NativeString("workbuddyExecutable")
	path, err := exec.LookPath(executable)
	if err != nil && executable == "codebuddy" && runtime.GOOS == "darwin" {
		path = "/Applications/WorkBuddy.app/Contents/Resources/app.asar.unpacked/cli/bin/codebuddy"
		_, err = os.Stat(path)
	}
	if err != nil {
		return nil, &blockedError{message: "WORKBUDDY_UNAVAILABLE: configured native CLI is unavailable"}
	}
	home := config.NativeString("workbuddyHome")
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	home = resolveNativePath(home)
	nativeHome := os.Getenv("WORKBUDDY_CONFIG_DIR")
	if nativeHome == "" || config.NativeString("workbuddyHome") != "" {
		nativeHome = filepath.Join(home, ".workbuddy")
	}
	nativeHome = resolveNativePath(nativeHome)
	product := config.NativeString("workbuddyProductConfig")
	if product == "" {
		product = os.Getenv("ACC_PRODUCT_CONFIG_PATH")
	}
	if product == "" {
		product = filepath.Join(nativeHome, "cache", "acc-product-config-v3.json")
	}
	product = resolveNativePath(product)
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
	binding, err := buildCaptureBinding("workbuddy", map[string]any{"executable": resolveNativePath(path)})
	if err != nil {
		return nil, err
	}
	binding["environment"] = environment
	env := map[string]string{}
	for key, value := range environment {
		env[key] = textOf(value)
	}
	if _, err := workbuddyProbe(path, home, env); err != nil {
		return nil, err
	}
	return binding, nil
}
func workbuddyProbe(executable, workspace string, environment map[string]string) (map[string]any, error) {
	cmd := exec.Command(executable, "--help")
	cmd.Dir = workspace
	cmd.Env = os.Environ()
	for key, value := range environment {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	for _, capability := range []string{"--resume", "--session-id", "stream-json", "--mcp-config", "--tools", "--permission-mode", "dontAsk"} {
		if !strings.Contains(string(output), capability) {
			return nil, &blockedError{message: "WORKBUDDY_CAPABILITY: native CLI lacks " + capability}
		}
	}
	return map[string]any{"provider": "workbuddy", "executable": executable, "capabilities": map[string]any{"source": "native-cli-help", "selection_observed": false}, "selection": map[string]any{}, "model_calls": 0}, nil
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
	return streamExecute(workbuddyStream, request, directory, probe, services...)
}
func workbuddyReconcile(directory string, process any) (map[string]any, error) {
	if original, err := readJSONFile(filepath.Join(directory, "native-source.json")); err == nil {
		defer workbuddyCleanup(textOf(getOf(original, "nativeWorkspace")))
	}
	return streamReconcile(workbuddyStream, directory, process)
}
func workbuddyArguments(request any, directory, executable, workspace string, environment map[string]string, prompt string) ([]string, string, error) {
	effort, _ := getOf(request, "reasoning_effort").(string)
	switch effort {
	case "", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		return nil, "", &blockedError{message: "WORKBUDDY_CONFIG: unsupported reasoning effort"}
	}
	identity, _ := getOf(request, "identity").(string)
	resume := identity != ""
	if resume {
		present, err := workbuddySessionRecordAvailable(environment["CODEBUDDY_CONFIG_DIR"], identity)
		if err != nil {
			return nil, "", err
		}
		if !present {
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
	argv := []string{executable, "--print", "--verbose", "--output-format", "stream-json", "--input-format", "text", "--tools", "", "--strict-mcp-config", "--mcp-config", configPath, "--setting-sources", "", "--settings", `{"disableAllHooks":true}`, "--permission-mode", permission, "--allowedTools", "mcp__formal-gates__workspace,mcp__formal-gates__submit_semantics"}
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

func workbuddySessionRecordAvailable(configRoot, identity string) (bool, error) {
	if !filepath.IsAbs(configRoot) || identity == "" || filepath.Base(identity) != identity {
		return false, &blockedError{message: "WORKBUDDY_RECOVERY: native config root and exact session identity required"}
	}
	entries, err := os.ReadDir(filepath.Join(configRoot, "projects"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	matches := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := os.Stat(filepath.Join(configRoot, "projects", entry.Name(), identity+".jsonl"))
		if err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			matches++
		}
	}
	if matches > 1 {
		return false, &blockedError{message: "WORKBUDDY_RECOVERY: native session identity is ambiguous across project stores"}
	}
	return matches == 1, nil
}
