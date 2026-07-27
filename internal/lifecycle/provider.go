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
	normalizeEvent func(string) (string, error)
	identity       func(any) string
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

func CurrentProvider() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return ProviderFromExecutable(path), nil
}

func ProviderFromExecutable(path string) string {
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
