package protocol

import "fmt"

// mergeVerificationDelta retains the writer's existing objects literally. The
// caller supplies new rows and the exact remaining set; it cannot rewrite a
// retained row or create another source identity for that observation.
func mergeVerificationDelta(old VerificationResult, next VerificationResult) (VerificationResult, error) {
	seen := map[string]CaseExecution{}
	for _, row := range next.Cases {
		if _, ok := seen[row.CaseID]; ok {
			return next, fmt.Errorf("duplicate case %s", row.CaseID)
		}
		seen[row.CaseID] = row
	}
	invalid := map[string]bool{}
	for _, change := range next.Invalidated {
		invalid[change.CaseID] = true
	}
	var rows []CaseExecution
	for _, row := range old.Cases {
		if invalid[row.CaseID] {
			continue
		}
		if echoed, ok := seen[row.CaseID]; ok {
			if digestOfCanonical(echoed) != digestOfCanonical(row) {
				return next, fmt.Errorf("checkpoint completed evidence cannot be changed: %s", row.CaseID)
			}
			delete(seen, row.CaseID)
		}
		rows = append(rows, row)
	}
	for _, row := range next.Cases {
		if _, ok := seen[row.CaseID]; ok {
			rows = append(rows, row)
		}
	}
	next.Cases = rows
	// Old raw findings remain available for a later final FAIL submission too.
	findings := map[string]ReviewFinding{}
	for _, finding := range next.Findings {
		if _, ok := findings[finding.ID]; ok {
			return next, fmt.Errorf("duplicate finding %s", finding.ID)
		}
		findings[finding.ID] = finding
	}
	var merged []ReviewFinding
	for _, finding := range old.Findings {
		if invalid[finding.Requirement] {
			continue
		}
		if echoed, ok := findings[finding.ID]; ok {
			if digestOfCanonical(echoed) != digestOfCanonical(finding) {
				return next, fmt.Errorf("checkpoint finding cannot be changed: %s", finding.ID)
			}
			delete(findings, finding.ID)
		}
		merged = append(merged, finding)
	}
	for _, finding := range next.Findings {
		if _, ok := findings[finding.ID]; ok {
			merged = append(merged, finding)
		}
	}
	next.Findings = merged
	return next, nil
}

// ValidateQACheckpoint lets the attached operation tool reject a malformed delta
// before the existing writer handles it as a final rejected worker result.
func ValidateQACheckpoint(s *State, result VerificationResult) error {
	x, ok := s.RegularExecutions[result.ActionID]
	if !ok || result.Status != "CHECKPOINT" {
		return fmt.Errorf("checkpoint requires an attached action")
	}
	w := currentWave(s.Regular)
	if w == nil {
		return fmt.Errorf("checkpoint has no current wave")
	}
	merged, err := mergeVerificationDelta(w.Checkpoints[targetName(x.Claim.Task.Step)], result)
	if err != nil {
		return err
	}
	return validateVerificationResult(s.Regular, x, merged, true)
}
