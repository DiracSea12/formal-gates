package facade

import (
	"strings"

	"formal-gates/internal/engine/protocol"
)

// Return only this task's latest rejection. Passing another role's results or a
// full historical transcript would violate independent QA inputs and waste context.
func preparationRetryFeedback(run Run, claim protocol.ExecutionClaim, payload map[string]any) {
	for i := len(run.Regular.PreparationErrors) - 1; i >= 0; i-- {
		e := run.Regular.PreparationErrors[i]
		x, ok := run.Executions[e.Result.ActionID]
		if !ok || x.Claim.Task != claim.Task || e.Result.RequirementRevision != claim.RequirementRevision || e.Result.Candidate != claim.Candidate {
			continue
		}
		payload["previousResultRejection"] = map[string]string{"actionId": e.Result.ActionID, "reason": e.Reason, "status": e.Result.Status, "message": e.Result.Message}
		break
	}
	step := string(claim.Task.Step)
	if strings.HasSuffix(step, "-design") {
		mode := strings.TrimSuffix(step, "-design")
		if q := run.Regular.Preparation.QA[mode]; q != nil && q.DesignGap != "" {
			payload["previousDesignGap"] = q.DesignGap
		}
	}
}
