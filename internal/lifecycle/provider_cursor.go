package lifecycle

import "strings"

func cursorAdapter() providerAdapter {
	return providerAdapter{
		name:       ProviderCursor,
		required:   true,
		hookEvents: []string{"subagentStart", "subagentStop"},
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
		projectRoot: func(payload any) string {
			if root := payloadWorkspaceRoot(payload); root != "" {
				return root
			}
			return payloadRoot(payload)
		},
	}
}
