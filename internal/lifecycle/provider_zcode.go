package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// zcodeGlobalInstallPrefix is the normalized user-level ZCode skill path.
// ZCode ignores project-level hook configuration, so only this global path
// has an active lifecycle bridge.
func zcodeGlobalInstallPrefix() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	prefix := filepath.ToSlash(filepath.Join(home, ".zcode", "skills", "formal-gates", "bin")) + "/"
	return strings.ToLower(prefix)
}

// isProjectZCodeInstall reports whether path is a project-local ZCode skill
// binary rather than the global user-level install.
func isProjectZCodeInstall(path string) bool {
	path = strings.TrimSpace(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	if !strings.Contains(normalized, "/.zcode/skills/formal-gates/bin/") {
		return false
	}
	prefix := zcodeGlobalInstallPrefix()
	return prefix == "" || !strings.HasPrefix(normalized, prefix)
}

// zcodeAdapter maps ZCode's Agent/Task tool hooks to the lifecycle journal's
// existing start/stop event model. PreToolUse is the dispatch attempt, while
// PostToolUse and PostToolUseFailure close the tool-call receipt. The tool
// call id is the only stable identity documented by ZCode, so it is used as
// the correlation key and raw host payloads remain the source of truth for
// any future child-agent identity bridge.
func zcodeAdapter() providerAdapter {
	return providerAdapter{
		name: ProviderZCode,
		executableMatches: func(path string) bool {
			normalized := normalizeProviderExecutablePath(path)
			return strings.Contains(normalized, "/.zcode/skills/formal-gates/bin/") && !isProjectZCodeInstall(path)
		},
		agentPrefixes:   []string{"zcode", "z-code"},
		environmentKeys: []string{"ZCODE_PLUGIN_ROOT", "ZCODE_PLUGIN_ID", "ZCODE_PLUGIN_NAME"},
		normalizeEvent:  normalizeZCodeEvent,
		identity:        func(_ string, payload any) string { return zcodeCallID(payload) },
		correlation:     func(_ string, payload any) string { return zcodeCallID(payload) },
		projectRoots: func(payload any) []string {
			roots := payloadProjectRoots(payload)
			return appendUniqueStrings(roots, payloadWorkingDirectories(payload)...)
		},
		transcriptPath: func(payload any) string {
			return payloadScalar(payload, []string{"transcript_path", "transcriptPath"}, 0)
		},
		reason: payloadReason,
	}
}

func zcodeCallID(payload any) string {
	return payloadIdentity(payload, []string{"tool_use_id", "toolUseId", "call_id", "callId", "tool_call_id", "toolCallId"})
}

func normalizeZCodeEvent(eventName string) (string, error) {
	switch eventName {
	case "PreToolUse":
		return eventStart, nil
	case "PostToolUse", "PostToolUseFailure":
		return eventStop, nil
	default:
		return "", unsupportedZCodeEventError(eventName)
	}
}

func unsupportedZCodeEventError(eventName string) error {
	return fmt.Errorf("unsupported %s lifecycle event %q (want PreToolUse, PostToolUse, or PostToolUseFailure)", ProviderZCode, eventName)
}
