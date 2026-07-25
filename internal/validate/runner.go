package validate

import (
	"fmt"
	"strings"
)

type PromptRoute struct {
	RequirementSource   string
	RequirementRevision string
	CatalogRevision     string
	Worktree            string
	VCS                 string
	BaseSnapshot        string
	CurrentSnapshot     string
	PreRepairSnapshot   string
}

func ComposeGatePrompt(catalog PromptCatalog, gateID string, route PromptRoute) (string, error) {
	gate, ok := catalog.Gate(gateID)
	if !ok {
		return "", fmt.Errorf("unknown discovered gate %q", gateID)
	}
	if err := validateRoute(route, false); err != nil {
		return "", err
	}
	return strings.Join([]string{
		"[Shared reviewer contract]\n" + catalog.Base,
		fmt.Sprintf("[Gate: %s]\n%s", gate.ID, gate.Content),
		fmt.Sprintf("[Current requirement]\nsource: %s\nrevision: %s\ncatalog revision: %s", route.RequirementSource, route.RequirementRevision, route.CatalogRevision),
		fmt.Sprintf("[Current change]\nworktree: %s\nvcs: %s\nbase snapshot: %s\ncurrent snapshot: %s\nUse the named VCS directly to inspect the complete base-to-current comparison.", route.Worktree, route.VCS, route.BaseSnapshot, route.CurrentSnapshot),
		"[Result contract]\nReturn exactly one JSON object matching: {\"status\":\"PASS|FAIL|RUNTIME_ERROR\",\"message\":\"...\",\"findings\":[{\"severity\":\"P0|P1|P2\",\"message\":\"...\",\"locations\":[\"repository/relative/path:line\"]}]}. PASS permits no findings or P2-only findings. FAIL requires at least one P0 or P1 finding and may include P2 findings. RUNTIME_ERROR requires a non-empty message and an empty findings array. Every finding requires exactly one severity. Do not add fields or Markdown fences.",
	}, "\n\n") + "\n", nil
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
		fmt.Sprintf("[Current requirement]\nsource: %s\nrevision: %s\ncatalog revision: %s", route.RequirementSource, route.RequirementRevision, route.CatalogRevision),
	}
	if actionID == "qa-review" {
		parts[1] += "\nworktree: " + route.Worktree
	} else {
		parts = append(parts, fmt.Sprintf("[Current change]\nworktree: %s\nvcs: %s\nbase snapshot: %s\ncurrent snapshot: %s", route.Worktree, route.VCS, route.BaseSnapshot, route.CurrentSnapshot))
		if route.PreRepairSnapshot != "" {
			parts[2] += "\npre-repair snapshot: " + route.PreRepairSnapshot
		}
	}
	if strings.TrimSpace(detail) != "" {
		parts = append(parts, "[Action input]\n"+strings.TrimSpace(detail))
	}
	parts = append(parts, "[Result contract]\n"+actionResultContract(actionID))
	return strings.Join(parts, "\n\n") + "\n", nil
}

func actionResultContract(actionID string) string {
	switch actionID {
	case "qa-design":
		return "Return only ordered semantic cases. Each case must contain description, procedure, and oracle. Do not assign case IDs; the CLI assigns them."
	case "qa-execution":
		return "Return one semantic result for every supplied case: case ID, PASS or FAIL outcome, executed procedure, observation, and oracle result. Return a runtime error separately if execution could not run."
	case "qa-review":
		return "Return PASS with no findings when the complete candidate set is approved, FAIL with one or more findings when it requires rework, or a separate runtime error message. Each finding contains a message and optional repository-relative locations."
	case "carry":
		return "Return exactly one decision for every supplied gate: gate ID, INHERIT or RERUN, and a concise reason. Return a runtime error separately if the native comparison could not run."
	case "development-worker":
		return "Perform the development action, track every delivery path in the named VCS before fixing the snapshot, and return the immutable current snapshot plus the delivery path names to the host. Do not return QA cases or a gate verdict."
	case "requirements-clarification":
		return "Return PASS only after the user confirms the requested outcome and consequential solution choices. Return FAIL with findings for unresolved consequential gaps, or a separate runtime error message."
	default:
		return "Return PASS with no findings, FAIL with one or more findings, or a separate runtime error message. Each finding contains a message and optional repository-relative locations."
	}
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
