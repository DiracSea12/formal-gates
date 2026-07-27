package lifecycle

func cursorAdapter() providerAdapter {
	return providerAdapter{
		name:     ProviderCursor,
		required: true,
		normalizeEvent: func(eventName string) (string, error) {
			return normalizeNamedEvent(ProviderCursor, eventName, "subagentStart", "subagentStop")
		},
		identity: func(payload any) string {
			return payloadIdentity(payload, []string{"subagentId", "subagent_id", "agentId", "agent_id", "taskId", "task_id"})
		},
	}
}
