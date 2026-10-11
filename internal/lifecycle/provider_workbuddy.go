package lifecycle

import "strings"

// WorkBuddy native actions use the shared verified start/stop journal; no hook
// installation is required for its print-mode CLI.
func workbuddyAdapter() providerAdapter {
	adapter := codexShapeAdapter(ProviderWorkBuddy)
	adapter.agentPrefixes = []string{"workbuddy", "codebuddy"}
	adapter.environmentKeys = []string{"WORKBUDDY_CONFIG_DIR"}
	adapter.executableMatches = func(path string) bool {
		return strings.Contains(normalizeProviderExecutablePath(path), "/.codebuddy/skills/formal-gates/bin/")
	}
	return adapter
}
