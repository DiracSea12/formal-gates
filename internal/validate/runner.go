package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"formal-gates/internal/prompts"
	"io"
	"strings"

	"formal-gates/internal/lifecycle"
)

func gateResultContract() string {
	return "[Result contract]\nReturn exactly one JSON object: {\"findings\":[{\"severity\":\"P0|P1|P2|P3\",\"message\":\"...\",\"locations\":[\"repository/relative/path:line\"]}]}. If you cannot perform the review, return {\"interruptionReason\":\"the actual reason\"}. The program derives the overall status from the findings. Do not fill status, identities, or wrapper metadata. Do not add Markdown fences."
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
		gateResultContract(),
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

// StandaloneGateResult is the program-normalized standalone gate observation.
type StandaloneGateResult struct {
	Status   string         `json:"status"`
	Message  string         `json:"message"`
	Findings []FindingInput `json:"findings"`
}

// ValidateStandaloneGateResult decodes semantic findings or actual inability and
// derives their status through the existing semantic normalization owner.
// It performs no run-state writes: standalone results are displayed only and
// never persisted.
func ValidateStandaloneGateResult(raw []byte) (StandaloneGateResult, error) {
	var wire struct {
		Message            string          `json:"message,omitempty"`
		Findings           *[]FindingInput `json:"findings,omitempty"`
		InterruptionReason *string         `json:"interruptionReason,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return StandaloneGateResult{}, fmt.Errorf("standalone gate result is not valid JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return StandaloneGateResult{}, fmt.Errorf("standalone gate result requires one JSON object")
	}
	result := StandaloneGateResult{Message: wire.Message}
	if wire.Findings != nil {
		result.Findings = *wire.Findings
	}
	interruption := ""
	if wire.InterruptionReason != nil {
		interruption = strings.TrimSpace(*wire.InterruptionReason)
		if interruption == "" {
			return StandaloneGateResult{}, fmt.Errorf("interruptionReason must state the actual reason")
		}
		result.Message = interruption
	} else if wire.Findings == nil {
		return StandaloneGateResult{}, fmt.Errorf("completed review requires findings")
	}
	status, _, err := validateSemanticResult(interruption, result.Findings)
	if err != nil {
		return StandaloneGateResult{}, err
	}
	result.Status = status
	return result, nil
}
