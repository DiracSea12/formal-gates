package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
)

// configureZCodeHook writes the user-level ZCode hook protocol. ZCode accepts
// only process/command hooks from the global CLI config or an installed plugin;
// project-level hook configuration is intentionally not supplied by
// installTargets because current ZCode ignores it. The skill itself can still
// be installed project-locally.
func configureZCodeHook(target installTarget) (bool, error) {
	before, beforeErr := os.ReadFile(target.hookConfig)
	if beforeErr != nil && !os.IsNotExist(beforeErr) {
		return false, beforeErr
	}
	config, err := readZCodeHookConfig(target.hookConfig)
	if err != nil {
		return false, err
	}
	backupPath := zcodeHookStatePath(target.hookConfig)
	var originalState zcodeHookState
	if !isFile(backupPath) {
		originalState = captureZCodeHookState(config)
	}
	hookRoot := zcodeHookRoot(config)
	hookRoot["enabled"] = true
	if _, ok := hookRoot["timeoutMs"]; !ok {
		hookRoot["timeoutMs"] = float64(60000)
	}
	events := zcodeHookEvents(hookRoot)
	descriptor, err := host.Lookup(host.ZCode)
	if err != nil {
		return false, err
	}
	lifecycleHooks, err := lifecycle.HookDefinitions(host.ZCode)
	if err != nil {
		return false, err
	}
	launcher := filepath.ToSlash(targetLauncherPath(target))
	gateCommand := zcodeProcessEntry(launcher, []string{"hook", "decide", "--provider", host.ZCode})
	// The gate hook remains broad so write-blocking applies to every tool. The
	// lifecycle matcher comes from the shared host descriptor.
	cleaned := map[string]bool{}
	clean := func(event string) []any {
		entries, _ := events[event].([]any)
		if !cleaned[event] {
			entries = removeZCodeHookEntries(entries, target)
			cleaned[event] = true
		}
		return entries
	}
	events[descriptor.Hook.GateEvent] = append(
		clean(descriptor.Hook.GateEvent),
		zcodeHookGroup(descriptor.Hook.GateMatcher, gateCommand),
	)
	for _, hook := range lifecycleHooks {
		matcher := hook.Matcher
		if matcher == "" {
			matcher = descriptor.Hook.LifecycleMatcher
		}
		entry := zcodeProcessEntry(launcher, append([]string(nil), hook.Command...))
		events[hook.Event] = append(clean(hook.Event), zcodeHookGroup(matcher, entry))
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return false, err
	}
	desired := append(data, '\n')
	if beforeErr == nil && string(before) == string(desired) {
		if !isFile(backupPath) {
			if err := writeZCodeHookState(backupPath, originalState); err != nil {
				return false, err
			}
		}
		return false, nil
	}
	if err := writeHookConfig(target.hookConfig, config); err != nil {
		return false, err
	}
	if !isFile(backupPath) {
		if err := writeZCodeHookState(backupPath, originalState); err != nil {
			return false, err
		}
	}
	return true, nil
}

type zcodeHookState struct {
	EnabledPresent bool `json:"enabledPresent"`
	Enabled        any  `json:"enabled,omitempty"`
	TimeoutPresent bool `json:"timeoutPresent"`
	Timeout        any  `json:"timeoutMs,omitempty"`
}

func zcodeHookStatePath(configPath string) string {
	return configPath + ".formal-gates-state.json"
}

func captureZCodeHookState(config map[string]any) zcodeHookState {
	root, _ := config["hooks"].(map[string]any)
	state := zcodeHookState{}
	if value, ok := root["enabled"]; ok {
		state.EnabledPresent = true
		state.Enabled = value
	}
	if value, ok := root["timeoutMs"]; ok {
		state.TimeoutPresent = true
		state.Timeout = value
	}
	return state
}

func writeZCodeHookState(path string, state zcodeHookState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func readZCodeHookState(path string) (zcodeHookState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return zcodeHookState{}, err
	}
	var state zcodeHookState
	if err := json.Unmarshal(data, &state); err != nil {
		return zcodeHookState{}, fmt.Errorf("invalid ZCode hook state: %s", path)
	}
	return state, nil
}

func readZCodeHookConfig(path string) (map[string]any, error) {
	if !isFile(path) {
		return map[string]any{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return map[string]any{}, nil
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("existing ZCode hook config is not valid JSON; refusing to touch it: %s", path)
	}
	if config == nil {
		return nil, fmt.Errorf("existing ZCode hook config must be a JSON object; refusing to touch it: %s", path)
	}
	if raw, ok := config["hooks"]; ok {
		root, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("existing ZCode hook config has malformed hooks object; refusing to touch it: %s", path)
		}
		if events, present := root["events"]; present {
			if _, ok := events.(map[string]any); !ok {
				return nil, fmt.Errorf("existing ZCode hook config has malformed events object; refusing to touch it: %s", path)
			}
		}
	}
	return config, nil
}

func zcodeHookRoot(config map[string]any) map[string]any {
	root, ok := config["hooks"].(map[string]any)
	if !ok {
		root = map[string]any{}
		config["hooks"] = root
	}
	return root
}

func zcodeHookEvents(root map[string]any) map[string]any {
	events, ok := root["events"].(map[string]any)
	if !ok {
		events = map[string]any{}
		root["events"] = events
	}
	return events
}

func zcodeHookGroup(matcher string, entry map[string]any) map[string]any {
	if strings.TrimSpace(matcher) == "" {
		matcher = ".*"
	}
	return map[string]any{"matcher": matcher, "hooks": []any{entry}}
}

func zcodeProcessEntry(command string, args []string) map[string]any {
	return map[string]any{
		"type":      "process",
		"command":   command,
		"args":      args,
		"timeoutMs": float64(30000),
	}
}

func removeZCodeHook(target installTarget) error {
	statePath := zcodeHookStatePath(target.hookConfig)
	if !isFile(target.hookConfig) {
		_ = os.Remove(statePath)
		return nil
	}
	config, err := readZCodeHookConfig(target.hookConfig)
	if err != nil {
		return err
	}
	root, ok := config["hooks"].(map[string]any)
	if !ok {
		_ = os.Remove(statePath)
		return nil
	}
	events, ok := root["events"].(map[string]any)
	if !ok {
		if state, stateErr := readZCodeHookState(statePath); stateErr == nil {
			before, _ := json.Marshal(config)
			restoreZCodeHookState(root, state)
			after, _ := json.Marshal(config)
			if string(before) != string(after) {
				if err := writeHookConfig(target.hookConfig, config); err != nil {
					return err
				}
			}
		}
		_ = os.Remove(statePath)
		return nil
	}
	before, _ := json.Marshal(config)
	for event, raw := range events {
		entries, ok := raw.([]any)
		if !ok {
			continue
		}
		events[event] = removeZCodeHookEntries(entries, target)
	}
	if state, stateErr := readZCodeHookState(statePath); stateErr == nil {
		restoreZCodeHookState(root, state)
	}
	after, _ := json.Marshal(config)
	if string(before) != string(after) {
		if err := writeHookConfig(target.hookConfig, config); err != nil {
			return err
		}
	}
	_ = os.Remove(statePath)
	return nil
}

func restoreZCodeHookState(root map[string]any, state zcodeHookState) {
	if state.EnabledPresent {
		if current, ok := root["enabled"]; !ok || reflect.DeepEqual(current, true) {
			root["enabled"] = state.Enabled
		}
	} else if current, ok := root["enabled"]; ok && reflect.DeepEqual(current, true) {
		delete(root, "enabled")
	}
	if state.TimeoutPresent {
		if current, ok := root["timeoutMs"]; !ok || reflect.DeepEqual(current, float64(60000)) {
			root["timeoutMs"] = state.Timeout
		}
	} else if current, ok := root["timeoutMs"]; ok && reflect.DeepEqual(current, float64(60000)) {
		delete(root, "timeoutMs")
	}
}

func removeZCodeHookEntries(entries []any, target installTarget) []any {
	kept := make([]any, 0, len(entries))
	for _, raw := range entries {
		group, ok := raw.(map[string]any)
		if !ok {
			kept = append(kept, raw)
			continue
		}
		hooks, ok := group["hooks"].([]any)
		if !ok {
			kept = append(kept, raw)
			continue
		}
		remaining := make([]any, 0, len(hooks))
		removed := false
		for _, hook := range hooks {
			if isZCodeInstallerHook(hook, target) {
				removed = true
				continue
			}
			remaining = append(remaining, hook)
		}
		if !removed {
			kept = append(kept, raw)
		} else if len(remaining) > 0 {
			group["hooks"] = remaining
			kept = append(kept, group)
		}
	}
	return kept
}

func isZCodeInstallerHook(value any, target installTarget) bool {
	hook, ok := value.(map[string]any)
	if !ok || hook["type"] != "process" {
		return false
	}
	command, ok := hook["command"].(string)
	if !ok {
		return false
	}
	if normalizeHookCommand(command) != normalizeHookCommand(filepath.ToSlash(targetLauncherPath(target))) {
		return false
	}
	args, ok := hook["args"].([]any)
	if !ok || len(args) < 2 {
		return false
	}
	words := make([]string, 0, len(args))
	for _, arg := range args {
		words = append(words, lifecycle.ScalarString(arg))
	}
	if len(words) >= 4 && words[0] == "hook" && words[1] == "decide" {
		return zcodeProviderArgument(words)
	}
	if len(words) >= 4 && words[0] == "lifecycle" && words[1] == "capture" {
		return zcodeProviderArgument(words)
	}
	return false
}

func zcodeProviderArgument(args []string) bool {
	for index := 2; index+1 < len(args); index++ {
		if args[index] == "--provider" && args[index+1] == host.ZCode {
			return true
		}
	}
	return false
}
