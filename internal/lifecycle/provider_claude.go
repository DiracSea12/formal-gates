package lifecycle

import (
	"os"
	"strings"
)

func claudeAdapter() providerAdapter {
	return providerAdapter{
		name: ProviderClaude,
		executableMatches: func(path string) bool {
			return strings.Contains(normalizeProviderExecutablePath(path), "/.claude/skills/formal-gates/bin/")
		},
		agentPrefixes:   []string{"claude-code"},
		environmentKeys: []string{"CLAUDE_CODE_ENTRYPOINT"},
		normalizeEvent: func(eventName string) (string, error) {
			return normalizeNamedEvent(ProviderClaude, eventName, "SubagentStart", "SubagentStop")
		},
		identity: func(_ string, payload any) string {
			return payloadIdentity(payload, []string{"subagentId", "subagent_id", "agentId", "agent_id", "taskId", "task_id"})
		},
		correlation: func(_ string, _ any) string { return "" },
		projectRoots: func(payload any) []string {
			roots := appendUniqueStrings(nil, os.Getenv("CLAUDE_PROJECT_DIR"))
			roots = appendUniqueStrings(roots, payloadProjectRoots(payload)...)
			return appendUniqueStrings(roots, payloadWorkingDirectories(payload)...)
		},
		transcriptPath: func(payload any) string {
			return payloadScalar(payload, []string{"agent_transcript_path"}, 0)
		},
		reason: payloadReason,
	}
}
