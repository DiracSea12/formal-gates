package coverage

import (
	"fmt"
	"strings"
)

// ValidateScenarios requires each acceptance point to have an explicit normal
// failure, misuse or boundary exercise. Reviewers judge whether it is sufficient.
func ValidateScenarios(manifest AcceptanceManifest) error {
	scenarios := map[string]string{}
	for _, c := range manifest.Cases {
		switch c.Scenario {
		case "SUCCESS", "FAILURE", "MISUSE", "BOUNDARY":
		default:
			return fmt.Errorf("case %s needs a scenario (SUCCESS, FAILURE, MISUSE, BOUNDARY)", c.CaseID)
		}
		scenarios[c.CaseID] = c.Scenario
	}
	covered := map[string]bool{}
	for _, edge := range manifest.Edges {
		if v := scenarios[edge.CaseID]; v != "" && v != "SUCCESS" {
			covered[edge.PointID] = true
		}
	}
	for _, point := range manifest.Points {
		if !covered[point.PointID] {
			return fmt.Errorf("point %s has no failure, misuse or boundary case", point.PointID)
		}
	}
	return nil
}

// ValidateEvidence rejects empty or reserved protocol placeholders. Semantic
// truth is assessed by the Operator, not guessed from prose by this validator.
func ValidateEvidence(caseID, procedure, observation, comparison string) error {
	sentinels := map[string]bool{"p": true, "o": true, "m": true, "procedure": true, "observation": true, "oracle": true, "run": true, "executed": true, "ok": true, "success": true, "pass": true, "passed": true, "matched": true, "skipped": true, "authorized pass": true, "observed success": true, "compared": true, "executed blackbox": true, "executed whitebox": true}
	for _, field := range []struct{ name, value string }{{"procedure", procedure}, {"observation", observation}, {"oracle result", comparison}} {
		value := strings.ToLower(strings.Join(strings.Fields(field.value), " "))
		if value == "" || sentinels[value] {
			return fmt.Errorf("QA result %s %s is empty or a reserved protocol sentinel, not per-case execution evidence", caseID, field.name)
		}
	}
	return nil
}
