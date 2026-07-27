package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

const (
	ProviderClaude = "claude-code"
	ProviderCodex  = "codex"
	ProviderCursor = "cursor"

	eventStart = "subagent_start"
	eventStop  = "subagent_stop"
)

type providerAdapter struct {
	name           string
	required       bool
	hookEvents     []string
	normalizeEvent func(string) (string, error)
	identity       func(string, any) string
	correlation    func(string, any) string
	projectRoots   func(any) []string
}

type HookDefinition struct {
	Event   string
	Command []string
}

func adapterFor(provider string) (providerAdapter, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case ProviderClaude, "claude", "claude code":
		return claudeAdapter(), nil
	case ProviderCodex:
		return codexAdapter(), nil
	case ProviderCursor:
		return cursorAdapter(), nil
	default:
		return providerAdapter{}, fmt.Errorf("unsupported lifecycle provider %q", provider)
	}
}

func HookDefinitions(host string) ([]HookDefinition, error) {
	adapter, err := adapterFor(host)
	if err != nil {
		return nil, err
	}
	hooks := make([]HookDefinition, 0, len(adapter.hookEvents))
	for _, event := range adapter.hookEvents {
		hooks = append(hooks, HookDefinition{
			Event:   event,
			Command: []string{"lifecycle", "capture", "--provider", adapter.name, "--event", event},
		})
	}
	return hooks, nil
}

var executablePath = os.Executable

func currentProvider() (string, error) {
	path, err := executablePath()
	if err != nil {
		return "", err
	}
	return providerFromExecutable(path), nil
}

func providerFromExecutable(path string) string {
	path = strings.TrimSpace(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	switch {
	case strings.Contains(normalized, "/.claude/skills/formal-gates/bin/"):
		return ProviderClaude
	case strings.Contains(normalized, "/.cursor/formal-gates/bin/"):
		return ProviderCursor
	case strings.Contains(normalized, "/.codex/skills/formal-gates/bin/"):
		return ProviderCodex
	default:
		return ProviderCodex
	}
}

func normalizeNamedEvent(provider, eventName, startName, stopName string) (string, error) {
	switch strings.TrimSpace(eventName) {
	case startName:
		return eventStart, nil
	case stopName:
		return eventStop, nil
	default:
		return "", fmt.Errorf("unsupported %s lifecycle event %q", provider, eventName)
	}
}

func payloadIdentity(value any, names []string) string {
	return payloadScalar(value, names, 0)
}

func payloadProjectRoots(value any) []string {
	return appendUniqueStrings(
		payloadStrings(value, []string{"projectDir", "project_dir"}, 0),
		payloadWorkspaceRoots(value)...,
	)
}

func payloadWorkingDirectories(value any) []string {
	return payloadStrings(value, []string{"cwd"}, 0)
}

func payloadWorkspaceRoots(value any) []string {
	return payloadStrings(value, []string{"workspaceRoots", "workspace_roots"}, 0)
}

func payloadStrings(value any, names []string, depth int) []string {
	if value == nil || depth > 3 {
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	var results []string
	for _, name := range names {
		for key, raw := range object {
			if !strings.EqualFold(key, name) {
				continue
			}
			if values, ok := raw.([]any); ok {
				for _, value := range values {
					results = appendUniqueStrings(results, scalarString(value))
				}
			} else {
				results = appendUniqueStrings(results, scalarString(raw))
			}
		}
	}
	for _, container := range []string{"payload", "event", "data", "hook", "tool_input", "toolInput", "input"} {
		for key, raw := range object {
			if strings.EqualFold(key, container) {
				results = appendUniqueStrings(results, payloadStrings(raw, names, depth+1)...)
			}
		}
	}
	return results
}

func payloadScalar(value any, names []string, depth int) string {
	if value == nil || depth > 3 {
		return ""
	}
	if object, ok := value.(map[string]any); ok {
		for _, name := range names {
			for key, raw := range object {
				if strings.EqualFold(key, name) {
					if scalar := scalarString(raw); scalar != "" {
						return scalar
					}
				}
			}
		}
		for _, container := range []string{"payload", "event", "data", "hook", "tool_input", "toolInput", "input"} {
			for key, raw := range object {
				if strings.EqualFold(key, container) {
					if scalar := payloadScalar(raw, names, depth+1); scalar != "" {
						return scalar
					}
				}
			}
		}
	}
	return ""
}

func scalarString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64, bool:
		return strings.TrimSpace(fmt.Sprint(typed))
	default:
		reflected := reflect.ValueOf(value)
		if reflected.IsValid() && reflected.Kind() >= reflect.Int && reflected.Kind() <= reflect.Uint64 {
			return strings.TrimSpace(fmt.Sprint(value))
		}
	}
	return ""
}

func appendUniqueStrings(values []string, additions ...string) []string {
	for _, addition := range additions {
		addition = strings.TrimSpace(addition)
		if addition == "" {
			continue
		}
		found := false
		for _, value := range values {
			if value == addition {
				found = true
				break
			}
		}
		if !found {
			values = append(values, addition)
		}
	}
	return values
}
