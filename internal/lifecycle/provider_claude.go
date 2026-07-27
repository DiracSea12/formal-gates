package lifecycle

func claudeAdapter() providerAdapter {
	return providerAdapter{
		name:     ProviderClaude,
		required: true,
		normalizeEvent: func(eventName string) (string, error) {
			return normalizeNamedEvent(ProviderClaude, eventName, "SubagentStart", "SubagentStop")
		},
		identity: func(payload any) string {
			return payloadIdentity(payload, []string{"subagentId", "subagent_id", "agentId", "agent_id", "taskId", "task_id"})
		},
	}
}
