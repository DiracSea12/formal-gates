package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

const (
	ProviderClaude  = "claude-code"
	ProviderCodex   = "codex"
	ProviderCursor  = "cursor"
	ProviderDefault = "default"

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
	transcriptPath func(any) string
	// reason 从宿主 stop/error 事件提取中断原因（含 HTTP 错误码），供 RQ-013 自动记录。
	reason func(any) string
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
	case ProviderDefault:
		return defaultAdapter(), nil
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
	if provider := providerFromExecutable(path); provider != ProviderDefault {
		return provider, nil
	}
	// A directly built source binary outside a maintained host installation
	// (go run, a local development build, or an uninstalled copy) resolves to
	// the lenient default provider from its path. When such a binary is driven
	// by a real host, its environment is the authoritative signal for the host
	// provider, so the dispatch binding still matches the host's capture hooks
	// and the subagent transcript cost is recorded instead of being dropped.
	if provider := providerFromEnvironment(); provider != "" {
		return provider, nil
	}
	return ProviderDefault, nil
}

// providerFromEnvironment reports the host that drives the process from its
// environment. It is consulted only when the executable resolves to the
// lenient default provider. Each host exports a distinctive environment in its
// agent shell; AI_AGENT is the host's own declared identity and takes
// precedence, with host-specific variables as a fallback. Empty when no host
// environment is detectable, keeping tests, portable canaries, and truly
// uninstalled contexts on the lenient default provider.
func providerFromEnvironment() string {
	agent := strings.ToLower(strings.TrimSpace(os.Getenv("AI_AGENT")))
	switch {
	case strings.HasPrefix(agent, "claude-code"):
		return ProviderClaude
	case strings.HasPrefix(agent, "codex"):
		return ProviderCodex
	case strings.HasPrefix(agent, "cursor"):
		return ProviderCursor
	}
	switch {
	case os.Getenv("CLAUDE_CODE_ENTRYPOINT") != "":
		return ProviderClaude
	case os.Getenv("CODEX_HOME") != "", os.Getenv("CODEX_CLI_PATH") != "":
		return ProviderCodex
	case os.Getenv("CURSOR_TRACE_ID") != "", os.Getenv("CURSOR_RUNTIME") != "":
		return ProviderCursor
	}
	return ""
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
		// 未安装二进制（go test、canary portable、本地开发构建）解析为宽松的默认
		// provider：无生命周期事件时仍走 UNAVAILABLE。只有真实安装的 Codex 二进制
		// 才解析为 required 的 Codex provider，验证生命周期配对。
		return ProviderDefault
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

// payloadReason extracts an interruption reason from a host stop/error event
// payload (RQ-013). It reads the reason-shaped fields first (stop_reason,
// reason, error, message, status), then falls back to any embedded HTTP error
// code (429/502/503/504/402/529 etc.) found in the payload text. Empty when the
// payload carries no discernible reason; Capture then records "未知".
func payloadReason(value any) string {
	for _, name := range []string{"stop_reason", "stopReason", "reason", "error", "error_message", "errorMessage", "message", "status", "result"} {
		if reason := payloadScalar(value, []string{name}, 0); reason != "" {
			return reason
		}
	}
	if code := payloadHTTPErrorCode(value); code != "" {
		return code
	}
	return ""
}

// payloadHTTPErrorCode scans every scalar string in the payload for a common
// transient HTTP error code and returns the first match as "HTTP <code>". To
// avoid misclassifying non-HTTP text (R 修复清单 item 11), a code only matches
// as a standalone numeric token — a whole number bounded by non-digit characters
// or the string edge — or when it appears in a dedicated error-code field name.
// The scan is bounded by the same depth limit as payloadScalar so a large payload
// does not walk unbounded.
func payloadHTTPErrorCode(value any) string {
	codes := []string{"429", "402", "500", "502", "503", "504", "529"}
	var walk func(any, int, string) string
	walk = func(v any, depth int, field string) string {
		if v == nil || depth > 3 {
			return ""
		}
		if s, ok := v.(string); ok {
			if isErrorCodeField(field) {
				for _, code := range codes {
					if strings.Contains(s, code) {
						return "HTTP " + code
					}
				}
				return ""
			}
			for _, code := range codes {
				if containsStandaloneNumber(s, code) {
					return "HTTP " + code
				}
			}
			return ""
		}
		switch typed := v.(type) {
		case map[string]any:
			for name, raw := range typed {
				if found := walk(raw, depth+1, name); found != "" {
					return found
				}
			}
		case []any:
			for _, raw := range typed {
				if found := walk(raw, depth+1, ""); found != "" {
					return found
				}
			}
		}
		return ""
	}
	return walk(value, 0, "")
}

// isErrorCodeField reports whether a payload field name looks like a dedicated
// error-code carrier (status, code, error, message and their compound forms).
func isErrorCodeField(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "status", "status_code", "statuscode", "code", "error_code", "errorcode", "error", "message":
		return true
	}
	return false
}

// containsStandaloneNumber reports whether s contains the numeric token as a
// whole number token: the token is preceded and followed by whitespace or the
// string edge, so substrings inside longer numbers, words, or decimals ("3.429")
// do not match.
func containsStandaloneNumber(s, token string) bool {
	for i := 0; i+len(token) <= len(s); i++ {
		if s[i:i+len(token)] != token {
			continue
		}
		beforeOK := i == 0 || isSpaceByte(s[i-1])
		afterOK := i+len(token) == len(s) || isSpaceByte(s[i+len(token)])
		if beforeOK && afterOK {
			return true
		}
	}
	return false
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
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
