package protocol

// Repair self-tests use the same checkpoint writer, scoped to the issued repair
// plan. They never populate a formal QA wave result or inherit an affected case.
func qaVerificationTarget(r *RegularState, claim ExecutionClaim) *VerificationTarget {
	w := currentWave(r)
	if claim.Task.Step != "repair" {
		return waveTarget(w, targetName(claim.Task.Step))
	}
	repair := currentRepair(r)
	if repair == nil || repair.ID != claim.PrerequisiteDigest {
		return nil
	}
	target := &VerificationTarget{Target: "repair", Task: claim.Task, Expected: repair.AffectedCases, BindingDigest: repair.ID, Inherited: map[string]CaseExecution{}}
	affected := map[string]bool{}
	for _, id := range repair.AffectedCases {
		affected[id] = true
	}
	for _, result := range w.Results {
		for _, row := range result.Cases {
			if !affected[row.CaseID] && row.Status == "PASS" {
				target.Inherited[row.CaseID] = row
			}
		}
	}
	return target
}
