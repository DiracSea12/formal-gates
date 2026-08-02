package validate

import (
	"fmt"
	"strings"
)

type PromptRoute struct {
	RequirementSource    string
	RequirementRevision  string
	CatalogRevision      string
	Worktree             string
	VCS                  string
	BaseSnapshot         string
	CurrentSnapshot      string
	PreRepairSnapshot    string
	RequirementArtifacts []RequirementArtifact
	DispatchID           string
	DispatchAttempt      int
	ReviewWave           int
}

func ComposeGatePrompt(catalog PromptCatalog, gateID string, route PromptRoute) (string, error) {
	gate, ok := catalog.Gate(gateID)
	if !ok {
		return "", fmt.Errorf("unknown discovered gate %q", gateID)
	}
	if err := validateRoute(route, false); err != nil {
		return "", err
	}
	if strings.TrimSpace(route.DispatchID) == "" || route.DispatchAttempt <= 0 {
		return "", fmt.Errorf("gate dispatch binding is required")
	}
	parts := []string{
		"[Shared reviewer contract]\n" + catalog.Base,
		fmt.Sprintf("[Gate: %s]\n%s", gate.ID, gate.Content),
		currentRequirementBlock(route),
		currentChangeBlock(route, true),
		dispatchBlock(route),
		fmt.Sprintf("[Result contract]\nReturn exactly one JSON object matching: {\"dispatchId\":%q,\"compared\":\"base..current\",\"status\":\"PASS|FAIL|RUNTIME_ERROR\",\"message\":\"...\",\"findings\":[{\"severity\":\"P0|P1|P2\",\"message\":\"...\",\"locations\":[\"repository/relative/path:line\"]}]}. Report the exact snapshot pair you actually compared in compared (base..current). PASS permits no findings or P2-only findings. FAIL requires at least one P0 or P1 finding and may include P2 findings. RUNTIME_ERROR requires a non-empty message and an empty findings array. Every finding requires exactly one severity. Return this dispatch ID unchanged. Do not add fields or Markdown fences.", route.DispatchID),
	}
	return strings.Join(parts, "\n\n") + "\n", nil
}

func ComposeActionPrompt(catalog PromptCatalog, actionID string, route PromptRoute, detail string) (string, error) {
	action, ok := catalog.Action(actionID)
	if !ok {
		return "", fmt.Errorf("unknown action prompt %q", actionID)
	}
	if err := validateRoute(route, actionID == "carry"); err != nil {
		return "", err
	}
	parts := []string{
		fmt.Sprintf("[Action: %s]\n%s", action.ID, action.Content),
		currentRequirementBlock(route),
	}
	if actionID == "qa-review" {
		parts[1] += "\nworktree: " + route.Worktree
	} else {
		parts = append(parts, currentChangeBlock(route, actionID == "qa-execution"))
		if route.PreRepairSnapshot != "" {
			parts[2] += "\npre-repair snapshot: " + route.PreRepairSnapshot
		}
	}
	if strings.TrimSpace(detail) != "" {
		parts = append(parts, "[Action input]\n"+strings.TrimSpace(detail))
	}
	if route.DispatchID != "" {
		parts = append(parts, dispatchBlock(route))
	}
	parts = append(parts, "[Result contract]\n"+actionResultContract(actionID, route.DispatchID))
	return strings.Join(parts, "\n\n") + "\n", nil
}

func actionResultContract(actionID, dispatchID string) string {
	prefix := ""
	if dispatchID != "" {
		prefix = fmt.Sprintf("Return dispatch ID %q unchanged. ", dispatchID)
	}
	switch actionID {
	case "qa-design":
		return prefix + "Return only ordered semantic cases. Each case must contain exactly one kind (STATIC or LIVE), description, procedure, and oracle. Do not assign case IDs; the CLI assigns them."
	case "qa-execution":
		return prefix + "Return one semantic result for every supplied case: case ID, PASS or FAIL outcome, executed procedure, observation, and oracle result. Return a runtime error separately if execution could not run."
	case "qa-review":
		return prefix + "Return one PASS or FAIL decision for every supplied pending case; each FAIL decision requires a reason. Do not return decisions for accepted cases. Return set-level findings separately for missing or duplicated coverage. The CLI derives the aggregate result. Return a runtime error separately if review could not run."
	case "carry":
		return prefix + "Return exactly one decision for every supplied gate: gate ID, INHERIT or RERUN, and a concise reason. Return a runtime error separately if the native comparison could not run."
	case "development-worker":
		return "Perform the development action, track every delivery path in the named VCS before fixing the snapshot, and return the immutable current snapshot plus the delivery path names to the host. Do not return QA cases or a gate verdict."
	case "requirements-clarification":
		return prefix + "Return PASS only after the user confirms the requested outcome and consequential solution choices. Return FAIL with findings for unresolved consequential gaps, or a separate runtime error message."
	default:
		return prefix + "Return PASS with no findings, FAIL with one or more findings, or a separate runtime error message. Each finding contains a message and optional repository-relative locations."
	}
}

func currentRequirementBlock(route PromptRoute) string {
	lines := []string{fmt.Sprintf("[Current requirement]\nsource: %s\nrevision: %s\ncatalog revision: %s", route.RequirementSource, route.RequirementRevision, route.CatalogRevision)}
	if len(route.RequirementArtifacts) != 0 {
		lines = append(lines, "acceptance artifacts:")
		for _, artifact := range route.RequirementArtifacts {
			lines = append(lines, fmt.Sprintf("- %s (revision %s)", artifact.Path, artifact.Revision))
		}
	}
	return strings.Join(lines, "\n")
}

func currentChangeBlock(route PromptRoute, reviewTargets bool) string {
	lines := []string{fmt.Sprintf("[Current change]\nworktree: %s\nvcs: %s\nbase snapshot: %s\ncurrent snapshot: %s", route.Worktree, route.VCS, route.BaseSnapshot, route.CurrentSnapshot)}
	if reviewTargets {
		lines = append(lines, "Use the named VCS directly to inspect the complete base-to-current comparison.", "Excluded review targets (acceptance inputs only):")
		for _, artifact := range route.RequirementArtifacts {
			lines = append(lines, "- "+artifact.Path)
		}
		if route.ReviewWave > 1 || route.PreRepairSnapshot != "" {
			lines = append(lines, fmt.Sprintf("这是返修后第 %d 轮重审；上一轮覆盖 base→pre-repair；你的范围是完整的 base→current（pre-repair 快照 %s 仅供参考，不要只审返修增量）。", route.ReviewWave, route.PreRepairSnapshot))
		}
	}
	return strings.Join(lines, "\n")
}

func dispatchBlock(route PromptRoute) string {
	return fmt.Sprintf("[Dispatch]\nid: %s\nattempt: %d\nreview wave: %d", route.DispatchID, route.DispatchAttempt, route.ReviewWave)
}

func validateRoute(route PromptRoute, requireRepair bool) error {
	for name, value := range map[string]string{
		"requirement source":   route.RequirementSource,
		"requirement revision": route.RequirementRevision,
		"catalog revision":     route.CatalogRevision,
		"worktree":             route.Worktree,
		"VCS":                  route.VCS,
		"base snapshot":        route.BaseSnapshot,
		"current snapshot":     route.CurrentSnapshot,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if requireRepair && strings.TrimSpace(route.PreRepairSnapshot) == "" {
		return fmt.Errorf("pre-repair snapshot is required")
	}
	return nil
}
