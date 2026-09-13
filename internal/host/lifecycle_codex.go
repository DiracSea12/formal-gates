package host

import "strings"

// codexAdapter adapts the installed Codex CLI. Codex requires paired start and
// stop lifecycle events, matching Claude Code and Cursor: an unpaired dispatch
// verification is REJECTED, with no soft fallback. Only an actually installed
// Codex binary resolves here; uninstalled/test/canary contexts resolve to the
// lenient default provider instead (see defaultAdapter), so go test, the
// portable canary and the workflow test suite still observe UNAVAILABLE without
// lifecycle events.
func codexAdapter() *Lifecycle {
	adapter := codexShapeAdapter(Codex)
	adapter.ExecutableMatches = func(normalized string) bool {
		return strings.Contains(normalized, "/.codex/skills/formal-gates/bin/")
	}
	adapter.AgentPrefixes = []string{"codex"}
	adapter.EnvironmentKeys = []string{"CODEX_HOME", "CODEX_CLI_PATH"}
	return adapter
}

// defaultAdapter adapts the uninstalled/default provider derived from a binary
// that is not installed beneath any host skills path (go test, portable canary,
// local development builds). It is lenient (required=false): verification is
// always UNAVAILABLE, never rejecting on missing lifecycle events. This keeps
// the test and canary verification surface lenient even though the installed
// Codex provider became required.
func defaultAdapter() *Lifecycle {
	return codexShapeAdapter(Default)
}

// codexShapeAdapter builds a provider adapter with the Codex event and identity
// shape. The installed Codex provider and the lenient default provider share
// this shape and differ only in their name and required flag.
func codexShapeAdapter(name string) *Lifecycle {
	return &Lifecycle{

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
