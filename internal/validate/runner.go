package validate

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/prompts"
	"strings"

	"formal-gates/internal/lifecycle"
)

// gateResultContract renders the [Result contract] block shared by the run-bound
// gate review and the standalone gate review. dispatchID selects the
// run-bound form (adds the dispatch id and compared snapshot pair fields and
// instructions); an empty dispatchID renders the standalone form.
func gateResultContract(dispatchID string) string {
	contract := "Return exactly one JSON object matching: {"
	if dispatchID != "" {
		contract += fmt.Sprintf("\"dispatchId\":%q,\"compared\":\"base..current\",", dispatchID)
	}
	if dispatchID != "" {
		contract += "\"status\":\"PASS|FAIL|RUNTIME_ERROR\",\"message\":\"...\",\"findings\":[{\"severity\":\"P0|P1|P2|P3\",\"severityRationale\":\"independent per-finding rationale\",\"requirement\":\"REQ-nnn\",\"normalPath\":\"documented normal-use path\",\"actualError\":\"current failure\",\"deferralImpact\":\"why P2 is insufficient\",\"verification\":{\"scope\":\"...\",\"severity\":\"...\",\"causality\":\"...\",\"reproduction\":\"...\"},\"message\":\"...\",\"locations\":[\"repository/relative/path:line\"]}]}. "
	} else {
		contract += "\"status\":\"PASS|FAIL|RUNTIME_ERROR\",\"message\":\"...\",\"findings\":[{\"severity\":\"P0|P1|P2|P3\",\"message\":\"...\",\"locations\":[\"repository/relative/path:line\"]}]}. "
	}
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
// runs completely outside any run state: it reviews the full working
// tree vs the native head, requires no requirement document, reuses the shared
// reviewer contract (so the first-step contamination check applies), and carries
// no dispatch binding. base = native head, current = the working tree's
// uncommitted changes (default includes untracked new files); the reviewer
// inspects the diff through the named VCS's native commands (git status + git
// diff, svn status + svn diff, p4 opened + p4 diff). An optional logical scope
// narrows the review when supplied.
func ComposeStandaloneGatePrompt(catalog prompts.PromptCatalog, gateID, root, vcs, scope string) (string, error) {
	gate, ok := catalog.Gate(gateID)
	if !ok {
		return "", fmt.Errorf("unknown discovered gate %q", gateID)
	}
	root = lifecycle.CleanRoot(root)
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
	// 单跑脱离 run、无需需求文档，故本提示词不含需求
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
// never persisted.
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
