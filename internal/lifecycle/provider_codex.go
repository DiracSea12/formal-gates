package lifecycle

func codexAdapter() providerAdapter {
	return providerAdapter{
		name:     ProviderCodex,
		required: false,
		normalizeEvent: func(eventName string) (string, error) {
			return normalizeNamedEvent(ProviderCodex, eventName, "SubagentStart", "SubagentStop")
		},
		identity: func(payload any) string {
			return payloadIdentity(payload, []string{"subagentId", "subagent_id", "agentId", "agent_id", "taskId", "task_id"})
		},
	}
}
