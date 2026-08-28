package lifecycle

import "strings"

// codexAdapter adapts the installed Codex CLI. Codex requires paired start and
// stop lifecycle events, matching Claude Code and Cursor: an unpaired dispatch
// verification is REJECTED, with no soft fallback. Only an actually installed
// Codex binary resolves here; uninstalled/test/canary contexts resolve to the
// lenient default provider instead (see defaultAdapter), so go test, the
// portable canary and the workflow test suite still observe UNAVAILABLE without
// lifecycle events.
func codexAdapter() providerAdapter {
	adapter := codexShapeAdapter(ProviderCodex)
	adapter.executableMatches = func(path string) bool {
		return strings.Contains(normalizeProviderExecutablePath(path), "/.codex/skills/formal-gates/bin/")
	}
	adapter.agentPrefixes = []string{"codex"}
	adapter.environmentKeys = []string{"CODEX_HOME", "CODEX_CLI_PATH"}
	return adapter
}

// defaultAdapter adapts the uninstalled/default provider derived from a binary
// that is not installed beneath any host skills path (go test, portable canary,
// local development builds). It is lenient (required=false): verification is
// always UNAVAILABLE, never rejecting on missing lifecycle events. This keeps
// the test and canary verification surface lenient even though the installed
// Codex provider became required.
func defaultAdapter() providerAdapter {
	return codexShapeAdapter(ProviderDefault)
}

// codexShapeAdapter builds a provider adapter with the Codex event and identity
// shape. The installed Codex provider and the lenient default provider share
// this shape and differ only in their name and required flag.
func codexShapeAdapter(name string) providerAdapter {
	return providerAdapter{
		name: name,
		normalizeEvent: func(eventName string) (string, error) {
			return normalizeNamedEvent(name, eventName, "SubagentStart", "SubagentStop")
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
		reason: payloadReason,
	}
}
