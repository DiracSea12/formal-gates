package lifecycle

import (
	"os"
	"strings"
)

func cursorAdapter() providerAdapter {
	return providerAdapter{
		name: ProviderCursor,
		executableMatches: func(path string) bool {
			return strings.Contains(normalizeProviderExecutablePath(path), "/.cursor/formal-gates/bin/")
		},
		agentPrefixes:   []string{"cursor"},
		environmentKeys: []string{"CURSOR_TRACE_ID", "CURSOR_RUNTIME"},
		normalizeEvent: func(eventName string) (string, error) {
			return normalizeNamedEvent(ProviderCursor, eventName, "subagentStart", "subagentStop")
		},
		identity: func(event string, payload any) string {
			if event == eventStop {
				return ""
			}
			return payloadIdentity(payload, []string{"subagentId", "subagent_id", "agentId", "agent_id", "taskId", "task_id"})
		},
		correlation: func(_ string, payload any) string {
			conversation := payloadIdentity(payload, []string{"parentConversationId", "parent_conversation_id", "conversationId", "conversation_id"})
			generation := payloadIdentity(payload, []string{"generationId", "generation_id"})
			subagentType := payloadIdentity(payload, []string{"subagentType", "subagent_type"})
			task := payloadIdentity(payload, []string{"task"})
			if conversation == "" || generation == "" || subagentType == "" || task == "" {
				return ""
			}
			return strings.Join([]string{conversation, generation, subagentType, task}, "\x00")
		},
		projectRoots: func(payload any) []string {
			roots := appendUniqueStrings(nil, os.Getenv("CURSOR_PROJECT_DIR"), os.Getenv("CLAUDE_PROJECT_DIR"))
			roots = appendUniqueStrings(roots, payloadWorkspaceRoots(payload)...)
			roots = appendUniqueStrings(roots, payloadProjectRoots(payload)...)
			return appendUniqueStrings(roots, payloadWorkingDirectories(payload)...)
		},
		reason: payloadReason,
	}
}
