package host

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

func isNormalizedProjectZCodeInstall(normalized string) bool {
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
func zcodeAdapter() *Lifecycle {
	return &Lifecycle{
		ProjectWithoutHooks: isNormalizedProjectZCodeInstall,

		ExecutableMatches: func(normalized string) bool {
			return strings.Contains(normalized, "/.zcode/skills/formal-gates/bin/") && !isNormalizedProjectZCodeInstall(normalized)
		},
		AgentPrefixes:   []string{"zcode", "z-code"},
		EnvironmentKeys: []string{"ZCODE_PLUGIN_ROOT", "ZCODE_PLUGIN_ID", "ZCODE_PLUGIN_NAME"},
		NormalizeEvent:  normalizeZCodeEvent,
		Identity:        func(_ string, payload any) string { return zcodeCallID(payload) },
		Correlation:     func(_ string, payload any) string { return zcodeCallID(payload) },
		ProjectRoots: func(payload any) []string {
			roots := payloadProjectRoots(payload)
			return AppendUniqueStrings(roots, payloadWorkingDirectories(payload)...)
		},
		TranscriptPath: func(payload any) string {
			return payloadScalar(payload, []string{"transcript_path", "transcriptPath"}, 0)
		},
		Reason: payloadReason,
	}
}

func zcodeCallID(payload any) string {
	return payloadIdentity(payload, []string{"tool_use_id", "toolUseId", "call_id", "callId", "tool_call_id", "toolCallId"})
}

func normalizeZCodeEvent(eventName string) (string, error) {
	switch eventName {
	case "PreToolUse":
		return "subagent_start", nil
	case "PostToolUse", "PostToolUseFailure":
		return "subagent_stop", nil
	default:
		return "", unsupportedZCodeEventError(eventName)
	}
}

func unsupportedZCodeEventError(eventName string) error {
	return fmt.Errorf("unsupported %s lifecycle event %q (want PreToolUse, PostToolUse, or PostToolUseFailure)", ZCode, eventName)
}
