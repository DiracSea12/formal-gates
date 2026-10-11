package host

// WorkBuddy native actions use the shared verified start/stop journal; no hook
// installation is required for its print-mode CLI.
func workbuddyAdapter() *Lifecycle {
	adapter := codexShapeAdapter(WorkBuddy)
	adapter.EnvironmentKeys = []string{"WORKBUDDY_CONFIG_DIR"}
	return adapter
}
