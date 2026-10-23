package harness

// Claude's print command owns the session, permissions and persistent history.
// This adapter records its native stream and process exit, then uses the same
// observer for live completion and recovery; it never replays a prompt.

import (
	"formal-gates/internal/host"
	"formal-gates/internal/settings"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var claudeCaptureBinding = nativeCapture(claudeResolvePaths, claudeConfiguredBinding)

// claudeBundleCandidates names the editor-extension install forms in probe
// order: the current layout's native binary under every editor root, then
// the earlier layout's cli.js (a node script, not a direct executable).
func claudeBundleCandidates() []string {
	binary := "claude"
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	candidates := []string{}
	for _, leaf := range []string{filepath.Join("native-binary", binary), filepath.Join("claude-code", "cli.js")} {
		for _, root := range nativeEditorExtensionRoots() {
			candidates = append(candidates, filepath.Join(root, "anthropic.claude-code-*", "resources", leaf))
		}
	}
	return candidates
}

func claudeResolvePaths(native, sources map[string]string) error {
	config := settings.Values{Native: native}
	executable := config.NativeString("claudeExecutable")
	path, executableSource, err := nativeExecutablePath(executable)
	script := ""
	if err != nil && executable == "claude" {
		// The extension bundle is another install form: the native binary
		// launches directly; a cli.js hit runs under node as the script
		// operand.
		for _, pattern := range claudeBundleCandidates() {
			candidate, source, ok := nativeFixedLaunch(pattern)
			if !ok {
				continue
			}
			if strings.HasSuffix(candidate, ".js") {
				node, _, nodeErr := nativeExecutablePath("node")
				if nodeErr != nil {
					continue
				}
				path, script, executableSource, err = node, candidate, "PATH", nil
			} else {
				path, executableSource, err = candidate, source, nil
			}
			break
		}
	}
	if err != nil {
		return &blockedError{message: "CLAUDE_UNAVAILABLE: claude is not on PATH"}
	}
	home := config.NativeString("claudeHome")
	explicitHome := home != ""
	home, homeSource, err := nativeUserHome(home)
	if err != nil {
		return err
	}
	home = resolveNativePath(home)
	nativeHome := native["claudeConfigRoot"]
	rootSource := "claudeHome"
	if nativeHome == "" {
		if !explicitHome {
			nativeHome = os.Getenv("CLAUDE_CONFIG_DIR")
			if nativeHome != "" {
				rootSource = "CLAUDE_CONFIG_DIR"
			}
		}
		if nativeHome == "" {
			nativeHome = filepath.Join(home, ".claude")
		}
	}
	paths := map[string]string{"claudeExecutable": resolveNativePath(path), "claudeHome": home, "claudeConfigRoot": resolveNativePath(nativeHome)}
	origins := map[string]string{"claudeExecutable": executableSource, "claudeHome": homeSource, "claudeConfigRoot": rootSource}
	if script != "" {
		paths["claudeScript"], origins["claudeScript"] = script, "bundle"
	}
	recordNativePaths(native, sources, paths, origins)
	return nil
}

func claudeConfiguredBinding(config settings.Values) (map[string]any, error) {
	executable, home, nativeHome := config.Native["claudeExecutable"], config.Native["claudeHome"], config.Native["claudeConfigRoot"]
	if executable == "" || home == "" || nativeHome == "" {
		return nil, &blockedError{message: "CLAUDE_CONFIG: frozen native paths required"}
	}
	// An extension cli.js binding runs under node with the script operand.
	launch := map[string]any{"executable": executable}
	if script := config.Native["claudeScript"]; script != "" {
		launch["script"] = script
	}
	binding, err := buildCaptureBinding("claude", launch)
	if err != nil {
		return nil, err
	}
	binding["environment"] = map[string]any{"HOME": home, "CLAUDE_CONFIG_DIR": nativeHome}
	return binding, nil
}

func claudeValidateBinding(binding any) error { return validateNativeBinding(binding, "claude") }
func claudeVerifySource(directory string, source any) error {
	return verifyNativeSource(directory, source, "claude")
}

func claudeCheckSelection(request any, selection map[string]any) error {
	model, _ := selection["model"].(string)
	if model == "" {
		return &blockedError{message: "CLAUDE_CONFIG: native selected model is missing"}
	}
	requested, _ := getOf(request, "model").(string)
	if requested != "" && requested != model {
		// Native --model accepts these family aliases as well as full model
		// names. The receipt keeps the resolved native name, never the alias.
		switch requested {
		case "sonnet", "opus", "haiku", "fable":
			if !strings.HasPrefix(model, "claude-"+requested+"-") {
				return &blockedError{message: "CLAUDE_CONFIG: model selection was not applied"}
			}
		default:
			return &blockedError{message: "CLAUDE_CONFIG: model selection was not applied"}
		}
	}
	if effort, _ := getOf(request, "reasoning_effort").(string); effort != "" && !valuesEqual(selection["reasoning_effort"], effort) {
		return &blockedError{message: "CLAUDE_CONFIG: effort selection was not applied"}
	}
	return nil
}

var claudeStream = streamAdapter{provider: "claude", prefix: "CLAUDE", capture: claudeCaptureBinding, probe: claudeProbe, selectionOptions: claudeSelectionOptions, arguments: claudeArguments, selection: streamSelection, checkSelection: claudeCheckSelection, sessionUnavailable: streamSessionUnavailable}

func claudeSelectionOptions(launch map[string]any, workspace, directory string, environment map[string]string, target string) (host.SelectionCatalogue, error) {
	catalogue := host.SelectionCatalogue{Source: "Claude SDK initialize and get_settings"}
	client, err := nativeSelectionControl(launch, workspace, directory, environment)
	if err != nil {
		return catalogue, err
	}
	defer client.close()
	initialized, err := nativeControlRequest(client, "initialize")
	if err != nil {
		return catalogue, err
	}
	settings, err := nativeControlRequest(client, "get_settings")
	if err != nil {
		return catalogue, err
	}
	effective := getOf(settings, "effective")
	catalogue.Current = host.ModelSelection{Model: nativeSelectionString(getOf(effective, "model")), Effort: nativeSelectionString(getOf(effective, "effortLevel"))}
	modelValue := func(entry any) string {
		value := nativeSelectionString(getOf(entry, "resolvedModel"))
		if value == "" {
			value = nativeSelectionString(getOf(entry, "value"))
		}
		return value
	}
	entries := listOf(getOf(initialized, "models"))
	for _, entry := range entries {
		value := modelValue(entry)
		if getOf(entry, "value") == catalogue.Current.Model {
			catalogue.Current.Model = value
		}
		if target == nativeSelectionString(getOf(entry, "value")) {
			target = value
		}
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		value := modelValue(entry)
		if seen[value] {
			continue
		}
		seen[value] = true
		option := host.ModelOption{Value: value, Label: nativeSelectionString(getOf(entry, "displayName"))}
		if value == target || target == "" && value == catalogue.Current.Model {
			option.Efforts = nativeStringValues(getOf(entry, "supportedEffortLevels"), "")
			if getOf(entry, "supportsEffort") == false {
				option.Efforts = []string{}
			}
			if value == catalogue.Current.Model {
				option.DefaultEffort = catalogue.Current.Effort
			}
		}
		catalogue.Models = append(catalogue.Models, option)
	}
	catalogue.Default.Model = catalogue.Current.Model
	if target == "" || target == catalogue.Current.Model {
		catalogue.Default.Effort = catalogue.Current.Effort
	}
	return catalogue, nil
}

func claudeExecute(request any, directory string, probe bool, services ...ExecutionServices) (map[string]any, error) {
	return streamExecute(claudeStream, request, directory, probe)
}
func claudeReconcile(directory string, process any, services ...ExecutionServices) (map[string]any, error) {
	return streamReconcile(claudeStream, directory, process)
}
func claudeProbe(launch map[string]any, workspace string, environment map[string]string) (map[string]any, error) {
	executable, _ := getOf(launch, "executable").(string)
	argv := nativeArgv(launch, "--version")
	command := exec.Command(argv[0], argv[1:]...)
	command.Dir = workspace
	command.Env = os.Environ()
	for key, value := range environment {
		command.Env = append(command.Env, key+"="+value)
	}
	version, err := command.Output()
	if err != nil {
		return nil, err
	}
	return map[string]any{"provider": "claude", "executable": executable, "version": strings.TrimSpace(string(version)), "capabilities": map[string]any{"source": "cli-version", "selection_observed": false}, "selection": map[string]any{}, "model_calls": 0}, nil
}
func claudeArguments(request any, directory string, launch map[string]any, workspace string, environment map[string]string, prompt string) ([]string, string, error) {
	argv := nativeArgv(launch, "-p", prompt, "--output-format", "stream-json", "--verbose", "--include-hook-events")
	server, err := nativeToolServer(directory)
	if err != nil {
		return nil, "", err
	}
	config, err := canonicalBody(map[string]any{"mcpServers": map[string]any{"formal-gates": map[string]any{"command": server["command"], "args": server["args"]}}})
	if err != nil {
		return nil, "", err
	}
	argv = append(argv, "--tools", "", "--strict-mcp-config", "--mcp-config", string(config), "--allowedTools", nativeAllowedMCPTools)
	for _, option := range []struct{ key, flag string }{{"identity", "--resume"}, {"model", "--model"}, {"reasoning_effort", "--effort"}} {
		if value, _ := getOf(request, option.key).(string); value != "" {
			argv = append(argv, option.flag, value)
		}
	}
	configRoot := environment["CLAUDE_CONFIG_DIR"]
	if configRoot == "" {
		return nil, "", &blockedError{message: "CLAUDE_CONFIG: frozen native paths required"}
	}
	settings := filepath.Join(configRoot, "settings.json")
	if _, err := os.Stat(settings); err == nil {
		argv = append(argv, "--settings", settings)
	} else if !os.IsNotExist(err) {
		return nil, "", err
	}
	return argv, workspace, nil
}
