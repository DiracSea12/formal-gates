package harness

// Claude's print command owns the session, permissions and persistent history.
// This adapter records its native stream and process exit, then uses the same
// observer for live completion and recovery; it never replays a prompt.

import (
	"formal-gates/internal/settings"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func claudeCaptureBinding() (map[string]any, error) {
	return claudeConfiguredBinding(settings.Values{})
}

func claudeConfiguredBinding(config settings.Values) (map[string]any, error) {
	executable, err := exec.LookPath(config.NativeString("claudeExecutable"))
	if err != nil {
		return nil, &blockedError{message: "CLAUDE_UNAVAILABLE: claude is not on PATH"}
	}
	binding, err := buildCaptureBinding("claude", map[string]any{"executable": resolveNativePath(executable)})
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if override := config.NativeString("claudeHome"); override != "" {
		home = override
	}
	nativeHome := os.Getenv("CLAUDE_CONFIG_DIR")
	if nativeHome == "" || config.NativeString("claudeHome") != "" {
		nativeHome = filepath.Join(home, ".claude")
	}
	binding["environment"] = map[string]any{"HOME": resolveNativePath(home), "CLAUDE_CONFIG_DIR": resolveNativePath(nativeHome)}
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

var claudeStream = streamAdapter{provider: "claude", prefix: "CLAUDE", capture: claudeCaptureBinding, probe: claudeProbe, arguments: claudeArguments, selection: streamSelection, checkSelection: claudeCheckSelection, sessionUnavailable: streamSessionUnavailable}

func claudeExecute(request any, directory string, probe bool, services ...ExecutionServices) (map[string]any, error) {
	return streamExecute(claudeStream, request, directory, probe, services...)
}
func claudeObserve(directory string) (map[string]any, error) {
	return streamObserve(claudeStream, directory)
}
func claudeReconcile(directory string, process any) (map[string]any, error) {
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
		configRoot = os.Getenv("CLAUDE_CONFIG_DIR")
		if configRoot == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, "", err
			}
			configRoot = filepath.Join(home, ".claude")
		}
	}
	settings := filepath.Join(configRoot, "settings.json")
	if _, err := os.Stat(settings); err == nil {
		argv = append(argv, "--settings", settings)
	} else if !os.IsNotExist(err) {
		return nil, "", err
	}
	return argv, workspace, nil
}
