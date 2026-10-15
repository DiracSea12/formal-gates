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

func claudeCaptureBinding() (map[string]any, error) {
	native, sources := map[string]string{}, map[string]string{}
	if err := claudeResolvePaths(native, sources); err != nil {
		return nil, err
	}
	return claudeConfiguredBinding(settings.Values{Native: native})
}

func claudeResolvePaths(native, sources map[string]string) error {
	config := settings.Values{Native: native}
	executable := config.NativeString("claudeExecutable")
	path, err := exec.LookPath(executable)
	if err != nil {
		return &blockedError{message: "CLAUDE_UNAVAILABLE: claude is not on PATH"}
	}
	executableSource := "PATH"
	if filepath.IsAbs(executable) || strings.ContainsAny(executable, `/\`) {
		executableSource = "configured"
	}
	home := config.NativeString("claudeHome")
	explicitHome := home != ""
	homeSource := "configured"
	if !explicitHome {
		home, err = os.UserHomeDir()
		if err != nil {
			return err
		}
		homeSource = "HOME"
		if runtime.GOOS == "windows" {
			homeSource = "USERPROFILE"
		}
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
	for name, value := range map[string]string{"claudeExecutable": resolveNativePath(path), "claudeHome": home, "claudeConfigRoot": resolveNativePath(nativeHome)} {
		native[name] = value
	}
	for name, source := range map[string]string{"claudeExecutable": executableSource, "claudeHome": homeSource, "claudeConfigRoot": rootSource} {
		if sources[name] == "" || sources[name] == "default" {
			sources[name] = source
		}
	}
	return nil
}

func claudeConfiguredBinding(config settings.Values) (map[string]any, error) {
	executable, home, nativeHome := config.Native["claudeExecutable"], config.Native["claudeHome"], config.Native["claudeConfigRoot"]
	if executable == "" || home == "" || nativeHome == "" {
		return nil, &blockedError{message: "CLAUDE_CONFIG: frozen native paths required"}
	}
	binding, err := buildCaptureBinding("claude", map[string]any{"executable": executable})
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

func claudeSelectionOptions(executable, workspace, directory string, environment map[string]string, target string) (host.SelectionCatalogue, error) {
	catalogue := host.SelectionCatalogue{Source: "Claude SDK initialize and get_settings"}
	client, err := nativeSelectionControl(executable, workspace, directory, environment)
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
func claudeProbe(executable, workspace string, environment map[string]string) (map[string]any, error) {
	command := exec.Command(executable, "--version")
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
func claudeArguments(request any, directory, executable, workspace string, environment map[string]string, prompt string) ([]string, string, error) {
	argv := []string{executable, "-p", prompt, "--output-format", "stream-json", "--verbose", "--include-hook-events"}
	server, err := nativeToolServer(directory)
	if err != nil {
		return nil, "", err
	}
	config, err := canonicalBody(map[string]any{"mcpServers": map[string]any{"formal-gates": map[string]any{"command": server["command"], "args": server["args"]}}})
	if err != nil {
		return nil, "", err
	}
	argv = append(argv, "--tools", "", "--strict-mcp-config", "--mcp-config", string(config))
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
