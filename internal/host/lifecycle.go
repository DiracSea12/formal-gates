package host

import (
	"fmt"
	"reflect"
	"strings"
)

// Lifecycle interprets a platform's native hook payload. The lifecycle package
// owns the journal, pairing and verification; descriptors own native differences.
// Nil means this platform does not expose hook-based lifecycle observations.
type Lifecycle struct {
	ExecutableMatches   func(string) bool
	ProjectWithoutHooks func(string) bool
	AgentPrefixes       []string
	EnvironmentKeys     []string
	NormalizeEvent      func(string) (string, error)
	Identity            func(string, any) string
	Correlation         func(string, any) string
	ProjectRoots        func(any) []string
	TranscriptPath      func(any) string
	Reason              func(any) string
}

func normalizeNamedEvent(provider, eventName, startName, stopName string) (string, error) {
	switch strings.TrimSpace(eventName) {
	case startName:
		return "subagent_start", nil
	case stopName:
		return "subagent_stop", nil
	default:
		return "", fmt.Errorf("unsupported %s lifecycle event %q", provider, eventName)
	}
}

func payloadIdentity(value any, names []string) string {
	return payloadScalar(value, names, 0)
}

// payloadReason extracts an interruption reason from a host stop/error event
// payload. It reads the reason-shaped fields first (stop_reason,
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
// avoid misclassifying non-HTTP text, a code only matches
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
	return AppendUniqueStrings(
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
					results = AppendUniqueStrings(results, ScalarString(value))
				}
			} else {
				results = AppendUniqueStrings(results, ScalarString(raw))
			}
		}
	}
	for _, container := range []string{"payload", "event", "data", "hook", "tool_input", "toolInput", "input"} {
		for key, raw := range object {
			if strings.EqualFold(key, container) {
				results = AppendUniqueStrings(results, payloadStrings(raw, names, depth+1)...)
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
					if scalar := ScalarString(raw); scalar != "" {
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

func ScalarString(value any) string {
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

func AppendUniqueStrings(values []string, additions ...string) []string {
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
