package facade

import (
	"formal-gates/internal/engine/protocol"
	"os"
)

// Caller holds the action lock and supplies freshly read execution state.
func unstartedActionClaim(x protocol.RegularExecution, job string) bool {
	if !x.Claimed || x.Finished || x.SupersededBy != "" || x.ExecutorHandoffID != "" || (x.Executor != nil && x.Executor.Kind == "external") || x.Claim.Operation != "spawn" || x.Identity != "" || x.Claim.Identity != "" || x.Source != nil ||
		x.PreviousAction != "" || x.ResumeClaimed || x.ResumeCount != 0 || x.ReplacementClaimed || x.Recovery != nil || len(x.RecoveryHistory) != 0 ||
		len(x.Operations) != 0 || x.PreparationResult != nil || x.VerificationResult != nil || x.RepairResult != nil {
		return false
	}
	entries, err := os.ReadDir(job)
	return os.IsNotExist(err) || err == nil && len(entries) == 0
}
