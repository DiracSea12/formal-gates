package lifecycle

func codexAdapter() providerAdapter {
	return providerAdapter{
		name:       ProviderCodex,
		required:   false,
		hookEvents: []string{"SubagentStart", "SubagentStop"},
		normalizeEvent: func(eventName string) (string, error) {
			return normalizeNamedEvent(ProviderCodex, eventName, "SubagentStart", "SubagentStop")
		},
		identity: func(_ string, payload any) string {
			return payloadIdentity(payload, []string{"subagentId", "subagent_id", "agentId", "agent_id", "taskId", "task_id"})
		},
		correlation: func(_ string, _ any) string { return "" },
		projectRoots: func(payload any) []string {
			roots := payloadProjectRoots(payload)
			return appendUniqueStrings(roots, payloadWorkingDirectories(payload)...)
		},
		transcriptPath: func(payload any) string {
			return payloadScalar(payload, []string{"agent_transcript_path"}, 0)
		},
	}
}
