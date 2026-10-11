package host

import "strings"

// WorkBuddy native actions use the shared verified start/stop journal; no hook
// installation is required for its print-mode CLI.
func workbuddyAdapter() *Lifecycle {
	adapter := codexShapeAdapter(WorkBuddy)
	adapter.AgentPrefixes = []string{"workbuddy", "codebuddy"}
	adapter.EnvironmentKeys = []string{"WORKBUDDY_CONFIG_DIR"}
	adapter.ExecutableMatches = func(path string) bool {
		return strings.Contains(NormalizeExecutablePath(path), "/.codebuddy/skills/formal-gates/bin/")
	}
	return adapter
}
