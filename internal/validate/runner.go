package validate

import (
	"encoding/json"
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
		gateResultContract(route.DispatchID),
	}
	return strings.Join(parts, "\n\n") + "\n", nil
}

// gateResultContract renders the [Result contract] block shared by the run-bound
// gate review and the standalone gate review (RQ-010). dispatchID selects the
// run-bound form (adds the dispatch id and compared snapshot pair fields and
// instructions); an empty dispatchID renders the standalone form.
func gateResultContract(dispatchID string) string {
	contract := "Return exactly one JSON object matching: {"
	if dispatchID != "" {
		contract += fmt.Sprintf("\"dispatchId\":%q,\"compared\":\"base..current\",", dispatchID)
	}
	contract += "\"status\":\"PASS|FAIL|RUNTIME_ERROR\",\"message\":\"...\",\"findings\":[{\"severity\":\"P0|P1|P2|P3\",\"message\":\"...\",\"locations\":[\"repository/relative/path:line\"]}]}. "
	if dispatchID != "" {
		contract += "Report the exact snapshot pair you actually compared in compared (base..current). "
	}
	contract += "PASS permits no findings or P2/P3-only findings. FAIL requires at least one P0 or P1 finding"
	if dispatchID != "" {
		contract += " and may include P2/P3 findings"
	}
	contract += ". RUNTIME_ERROR requires a non-empty message and an empty findings array. Every finding requires exactly one severity. "
	if dispatchID != "" {
		contract += "Return this dispatch ID unchanged. "
	}
	return "[Result contract]\n" + contract + "Do not add fields or Markdown fences."
}

// ComposeStandaloneGatePrompt assembles a standalone gate review prompt that
// runs completely outside any run state (RQ-010): it reviews the full working
// tree vs the native head, requires no requirement document, reuses the shared
// reviewer contract (so the first-step contamination check applies), and carries
// no dispatch binding. base = native head, current = the working tree's
// uncommitted changes (default includes untracked new files); the reviewer
// inspects the diff through the named VCS's native commands (git status + git
// diff, svn status + svn diff, p4 opened + p4 diff). An optional logical scope
// narrows the review when supplied.
func ComposeStandaloneGatePrompt(catalog PromptCatalog, gateID, root, vcs, scope string) (string, error) {
	gate, ok := catalog.Gate(gateID)
	if !ok {
		return "", fmt.Errorf("unknown discovered gate %q", gateID)
	}
	root = cleanRoot(root)
	vcs = strings.ToLower(strings.TrimSpace(vcs))
	resolver, err := resolverForVCS(vcs, nil)
	if err != nil {
		return "", err
	}
	base, err := resolver.Resolve(root)
	if err != nil {
		return "", err
	}
	parts := []string{
		"[Shared reviewer contract]\n" + catalog.Base,
		fmt.Sprintf("[Gate: %s]\n%s", gate.ID, gate.Content),
		standaloneChangeBlock(root, vcs, base, scope),
		gateResultContract(""),
	}
	return strings.Join(parts, "\n\n") + "\n", nil
}

func standaloneChangeBlock(root, vcs, base, scope string) string {
	lines := []string{"[Current change]", "worktree: " + absPath(root), "vcs: " + vcs, "base snapshot: " + base, "current: working tree (uncommitted changes)"}
	switch vcs {
	case "git":
		lines = append(lines, "Inspect the complete working-tree changes vs HEAD with: git status + git diff (default includes untracked new files).")
	case "svn":
		lines = append(lines, "Inspect the complete working-tree changes with: svn status + svn diff.")
	case "p4":
		lines = append(lines, "Inspect the complete working-tree changes with: p4 opened + p4 diff.")
	}
	if scope = strings.TrimSpace(scope); scope != "" {
		lines = append(lines, "Review scope (user-specified): "+scope)
	}
	// R 修复清单 item 10：单跑（RQ-010）脱离 run、无需需求文档，故本提示词不含需求
	// 块。这是设计意图，零上下文审查者不应因缺需求块而报 RUNTIME_ERROR。
	lines = append(lines, "This standalone gate run intentionally has no requirement block: it reviews the working tree vs HEAD without a requirement document. A missing requirement block is by design and must not be reported as RUNTIME_ERROR.")
	return strings.Join(lines, "\n")
}

// StandaloneGateResult is the result contract of a standalone gate review.
type StandaloneGateResult struct {
	Status   string         `json:"status"`
	Message  string         `json:"message"`
	Findings []FindingInput `json:"findings"`
}

// ValidateStandaloneGateResult validates a standalone gate review result against
// the same semantic contract as a gate result and returns the normalized status.
// It performs no run-state writes: standalone results are displayed only and
// never persisted (RQ-010).
func ValidateStandaloneGateResult(raw []byte) (StandaloneGateResult, error) {
	var result StandaloneGateResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return StandaloneGateResult{}, fmt.Errorf("standalone gate result is not valid JSON: %w", err)
	}
	status, _, err := validateSemanticResult("", result.Status, result.Message, result.Findings, true)
	if err != nil {
		return StandaloneGateResult{}, err
	}
	result.Status = status
	return result, nil
}

// isReviewerAction reports whether an action is a zero-context reviewer action
// whose composed prompt carries the shared reviewer contract (RQ-003):
// product-review、qa-review、start-readiness 三个零上下文审查者动作注入；非审查动作
// （development-worker、carry、qa-execution、requirements-clarification）不注入，
// 避免"你是独立审查者、不要编辑仓库文件"的契约段落出现在开发工/执行动作中。
// qa-design 也不注入：它已是设计写者（白盒写测试代码、黑盒写用例文档，RQ-011/RQ-013），
// 收到"不要编辑仓库文件"契约会与写角色直接矛盾。
func isReviewerAction(actionID string) bool {
	switch actionID {
	case "product-review", "qa-review", "start-readiness":
		return true
	}
	return false
}

func ComposeActionPrompt(catalog PromptCatalog, actionID string, route PromptRoute, detail string) (string, error) {
	action, ok := catalog.Action(actionID)
	if !ok {
		return "", fmt.Errorf("unknown action prompt %q", actionID)
	}
	if err := validateRoute(route, actionID == "carry"); err != nil {
		return "", err
	}
	parts := []string{}
	// RQ-003：审查类动作注入共享审查者契约，块序与门提示词一致（[Shared reviewer
	// contract] 头部在前、action 块随后）；其余动作不注入。
	if isReviewerAction(actionID) {
		parts = append(parts, "[Shared reviewer contract]\n"+catalog.Base)
	}
	parts = append(parts, fmt.Sprintf("[Action: %s]\n%s", action.ID, action.Content))
	requirement := currentRequirementBlock(route)
	if actionID == "qa-review" {
		requirement += "\nworktree: " + route.Worktree
	}
	parts = append(parts, requirement)
	if actionID != "qa-review" {
		change := currentChangeBlock(route, actionID == "qa-execution")
		if route.PreRepairSnapshot != "" {
			change += "\npre-repair snapshot: " + route.PreRepairSnapshot
		}
		parts = append(parts, change)
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
		return prefix + "Return only ordered semantic cases. Each case must contain exactly one mode (blackbox or whitebox), description, procedure, and oracle. blackbox covers real QA: verify the requirement by actually using the product through a documented public entry at execution time; whitebox covers structure tests (unit, system, integration) whose sufficiency depends on the implementation and is designed after development by reading it. For every whitebox case, independently design and write the structural test code you deliver, and name the test implementing that case as a --test \"<file>::<function>\" reference (the delivered test code file and the test function inside it; opaque strings, the CLI does not parse code): the CLI records the reference and requires it non-empty and unique per case (one test implements one case). Test existence and correspondence are verified by QA Review and QA Execution, so the case ID and the executed test are truly bound. Do not assign case IDs; the CLI assigns them."
	case "qa-execution":
		return prefix + "Return one semantic result for every supplied case: case ID, PASS or FAIL outcome, executed procedure, observation, and oracle result. blackbox cases run against the built product on the main worktree via a documented public entry; whitebox cases run their direct-owner structure tests. Return a runtime error separately if execution could not run."
	case "qa-review":
		return prefix + "Return one PASS or FAIL decision for every supplied pending case; each FAIL decision requires a reason. Do not return decisions for accepted cases. Return set-level findings separately with severity P1 or P2: a selected mode with zero cases or an unmet acceptance point is a P1 coverage omission that blocks; P2 is a suggestion only. The CLI derives the aggregate result. For whitebox cases you may read the implementation to judge structure-test sufficiency; blackbox cases stay zero-context. Return a runtime error separately if review could not run."
	case "carry":
		return prefix + "Return exactly one decision for every supplied gate: gate ID, INHERIT or RERUN, and a concise reason. Return a runtime error separately if the native comparison could not run."
	case "development-worker":
		return "Perform the development action, track every delivery path in the named VCS before fixing the snapshot, and return the immutable current snapshot plus the delivery path names to the host. Do not return QA cases or a gate verdict."
	case "product-review":
		return prefix + "Return PASS with no findings, FAIL with one or more findings as candidate inputs for the user's per-item decision, or a separate runtime error message. Each finding carries a severity (P0, P1, P2, or P3), a message, and optional repository-relative locations. Do not re-raise findings and decisions the user already settled (listed in the action input); re-raise one only if a requirement revision changed its premise. The review itself never produces a terminal FAIL; the user decides whether the requirement stands."
	case "start-readiness":
		return prefix + "Return PASS with no findings, FAIL with one or more findings, or a separate runtime error message. Each finding carries a severity (P0, P1, P2, or P3), a message, and optional repository-relative locations."
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
			if route.PreRepairSnapshot != "" {
				lines = append(lines, fmt.Sprintf("这是返修后第 %d 轮重审；上一轮覆盖 base→pre-repair；你的范围是完整的 base→current（pre-repair 快照 %s 仅供参考，不要只审返修增量）。", route.ReviewWave, route.PreRepairSnapshot))
			} else {
				lines = append(lines, fmt.Sprintf("这是第 %d 轮重审；你的范围是完整的 base→current，不要只审最近改动。", route.ReviewWave))
			}
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
