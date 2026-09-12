package host

import (
	"os"
	"strings"
)

func claudeAdapter() *Lifecycle {
	return &Lifecycle{

		ExecutableMatches: func(normalized string) bool {
			return strings.Contains(normalized, "/.claude/skills/formal-gates/bin/")
		},
		AgentPrefixes:   []string{"claude-code"},
		EnvironmentKeys: []string{"CLAUDE_CODE_ENTRYPOINT"},
		NormalizeEvent: func(eventName string) (string, error) {
			return normalizeNamedEvent(Claude, eventName, "SubagentStart", "SubagentStop")
		},
		Identity: func(_ string, payload any) string {
			return payloadIdentity(payload, []string{"subagentId", "subagent_id", "agentId", "agent_id", "taskId", "task_id"})
		},
		Correlation: func(_ string, _ any) string { return "" },
		ProjectRoots: func(payload any) []string {
			roots := AppendUniqueStrings(nil, os.Getenv("CLAUDE_PROJECT_DIR"))
			roots = AppendUniqueStrings(roots, payloadProjectRoots(payload)...)
			return AppendUniqueStrings(roots, payloadWorkingDirectories(payload)...)
		},
		TranscriptPath: func(payload any) string {
			return payloadScalar(payload, []string{"agent_transcript_path"}, 0)
		},
		Reason: payloadReason,
	}
}
