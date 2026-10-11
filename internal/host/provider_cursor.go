package host

import (
	"os"
	"strings"
)

func cursorAdapter() *Lifecycle {
	return &Lifecycle{
		Name: Cursor,
		ExecutableMatches: func(path string) bool {
			return strings.Contains(NormalizeExecutablePath(path), "/.cursor/formal-gates/bin/")
		},
		AgentPrefixes:   []string{"cursor"},
		EnvironmentKeys: []string{"CURSOR_TRACE_ID", "CURSOR_RUNTIME"},
		NormalizeEvent: func(eventName string) (string, error) {
			return normalizeNamedEvent(Cursor, eventName, "subagentStart", "subagentStop")
		},
		Identity: func(event string, payload any) string {
			if event == eventStop {
				return ""
			}
			return payloadIdentity(payload, []string{"subagentId", "subagent_id", "agentId", "agent_id", "taskId", "task_id"})
		},
		Correlation: func(_ string, payload any) string {
			conversation := payloadIdentity(payload, []string{"parentConversationId", "parent_conversation_id", "conversationId", "conversation_id"})
			generation := payloadIdentity(payload, []string{"generationId", "generation_id"})
			subagentType := payloadIdentity(payload, []string{"subagentType", "subagent_type"})
			task := payloadIdentity(payload, []string{"task"})
			if conversation == "" || generation == "" || subagentType == "" || task == "" {
				return ""
			}
			return strings.Join([]string{conversation, generation, subagentType, task}, "\x00")
		},
		ProjectRoots: func(payload any) []string {
			roots := AppendUniqueStrings(nil, os.Getenv("CURSOR_PROJECT_DIR"), os.Getenv("CLAUDE_PROJECT_DIR"))
			roots = AppendUniqueStrings(roots, payloadWorkspaceRoots(payload)...)
			roots = AppendUniqueStrings(roots, payloadProjectRoots(payload)...)
			return AppendUniqueStrings(roots, payloadWorkingDirectories(payload)...)
		},
		Reason: payloadReason,
	}
}
