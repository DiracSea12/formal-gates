package protocol

// Native recovery evidence is interpreted only by the matching host adapter.
var recoveryObservers = map[string]func(DeveloperRecovery) string{
	"codex":            codexRecoveryOutcome,
	"workbuddy":        workBuddyRecoveryOutcome,
	"zcode":            zcodeRecoveryOutcome,
	"deepseek-harness": dshRecoveryOutcome,
}
