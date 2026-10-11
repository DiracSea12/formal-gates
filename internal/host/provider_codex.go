package host

// codexAdapter adapts Codex CLI start/stop lifecycle events and identity.
func codexAdapter() *Lifecycle {
	adapter := codexShapeAdapter(Codex)
	adapter.EnvironmentKeys = []string{"CODEX_HOME", "CODEX_CLI_PATH"}
	return adapter
}

// defaultAdapter uses the Codex event shape for the lenient default provider.
func defaultAdapter() *Lifecycle {
	return codexShapeAdapter(Default)
}

// codexShapeAdapter builds a provider adapter with the Codex event and identity
// shape. The installed Codex provider and the lenient default provider share
// this shape and differ only in their name and required flag.
func codexShapeAdapter(name string) *Lifecycle {
	return &Lifecycle{
		Name: name,
		NormalizeEvent: func(eventName string) (string, error) {
			return normalizeNamedEvent(name, eventName, "SubagentStart", "SubagentStop")
		},
		Identity: func(_ string, payload any) string {
			return payloadIdentity(payload, []string{"subagentId", "subagent_id", "agentId", "agent_id", "taskId", "task_id", "thread_id", "session_id"})
		},
		Correlation: func(_ string, _ any) string { return "" },
		ProjectRoots: func(payload any) []string {
			roots := payloadProjectRoots(payload)
			return AppendUniqueStrings(roots, payloadWorkingDirectories(payload)...)
		},
		TranscriptPath: func(payload any) string {
			return payloadScalar(payload, []string{"agent_transcript_path", "transcript_path", "rollout_path"}, 0)
		},
		Reason: payloadReason,
	}
}
