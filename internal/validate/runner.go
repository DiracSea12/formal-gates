package validate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"formal-gates/internal/lifecycle"
)

// standaloneGateContract describes detached review output. These observations
// cannot be submitted as a formal engine action or advance a run.
func standaloneGateContract() string {
	return `[Result contract]
Return exactly one JSON object matching: {"status":"PASS|FAIL|RUNTIME_ERROR","message":"...","findings":[{"severity":"P0|P1|P2|P3","message":"...","locations":["repository/relative/path:line"]}]}. PASS permits no findings or P2/P3-only findings. FAIL requires at least one P0 or P1 finding. RUNTIME_ERROR requires a non-empty message and an empty findings array. Every finding requires exactly one severity. Do not add fields or Markdown fences.`
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
func ComposeStandaloneGatePrompt(catalog PromptCatalog, gateID, root, vcs, scope string) (string, error) {
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
		standaloneGateContract(),
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
	status, _, err := validateStandaloneGateResult(result.Status, result.Message, result.Findings)
	if err != nil {
		return StandaloneGateResult{}, err
	}
	result.Status = status
	return result, nil
}

type Finding struct {
	ID                string              `json:"id,omitempty"`
	Severity          string              `json:"severity,omitempty"`
	SeverityRationale string              `json:"severityRationale,omitempty"`
	Category          string              `json:"category,omitempty"`
	Requirement       string              `json:"requirement,omitempty"`
	NormalPath        string              `json:"normalPath,omitempty"`
	ActualError       string              `json:"actualError,omitempty"`
	DeferralImpact    string              `json:"deferralImpact,omitempty"`
	Verification      FindingVerification `json:"verification,omitempty"`
	Message           string              `json:"message"`
	Locations         []string            `json:"locations,omitempty"`
}

// FindingVerification is intentionally per finding. One batch-level operator
// paragraph cannot stand in for independent scope, severity, causality,
// reproduction, and (for Start Readiness) classification checks.
type FindingVerification struct {
	Scope          string `json:"scope,omitempty"`
	Severity       string `json:"severity,omitempty"`
	Causality      string `json:"causality,omitempty"`
	Reproduction   string `json:"reproduction,omitempty"`
	Classification string `json:"classification,omitempty"`
}

type FindingInput struct {
	Severity          string
	SeverityRationale string
	Category          string
	Requirement       string
	NormalPath        string
	ActualError       string
	DeferralImpact    string
	Verification      FindingVerification
	Message           string
	Locations         []string
}

func validateStandaloneGateResult(status, message string, findings []FindingInput) (string, []Finding, error) {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != "PASS" && status != "FAIL" && status != "RUNTIME_ERROR" {
		return "", nil, fmt.Errorf("status must be PASS, FAIL, or RUNTIME_ERROR")
	}
	if status == "FAIL" && len(findings) == 0 {
		return "", nil, fmt.Errorf("FAIL requires at least one finding")
	}
	if status == "RUNTIME_ERROR" {
		if len(findings) != 0 {
			return "", nil, fmt.Errorf("RUNTIME_ERROR cannot include reviewer findings")
		}
		if strings.TrimSpace(message) == "" {
			return "", nil, fmt.Errorf("RUNTIME_ERROR requires a message")
		}
	}
	converted := make([]Finding, 0, len(findings))
	hasBlocking := false
	for _, input := range findings {
		if strings.TrimSpace(input.Message) == "" {
			return "", nil, fmt.Errorf("finding message is required")
		}
		locations := make([]string, 0, len(input.Locations))
		for _, location := range input.Locations {
			if err := validateFindingLocation(location); err != nil {
				return "", nil, err
			}
			locations = append(locations, strings.TrimSpace(location))
		}
		severity := strings.ToUpper(strings.TrimSpace(input.Severity))
		if severity != "P0" && severity != "P1" && severity != "P2" && severity != "P3" {
			return "", nil, fmt.Errorf("gate finding severity must be P0, P1, P2, or P3")
		}
		if severity == "P0" || severity == "P1" {
			hasBlocking = true
		}
		finding := Finding{Severity: severity, SeverityRationale: strings.TrimSpace(input.SeverityRationale), Category: strings.ToUpper(strings.TrimSpace(input.Category)), Requirement: strings.TrimSpace(input.Requirement), NormalPath: strings.TrimSpace(input.NormalPath), ActualError: strings.TrimSpace(input.ActualError), DeferralImpact: strings.TrimSpace(input.DeferralImpact), Verification: input.Verification, Message: strings.TrimSpace(input.Message), Locations: locations}
		finding.ID = findingStableID(finding)
		converted = append(converted, finding)
	}
	if status == "PASS" && hasBlocking {
		return "", nil, fmt.Errorf("PASS can include only P2/P3 findings")
	}
	if status == "FAIL" && !hasBlocking {
		return "", nil, fmt.Errorf("FAIL requires at least one P0 or P1 finding")
	}
	return status, converted, nil
}

func validateFindingLocation(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("finding location is empty")
	}
	if strings.Contains(value, `\`) || strings.Contains(value, "://") || strings.HasPrefix(value, "/") || (len(value) > 1 && value[1] == ':') {
		return fmt.Errorf("finding location must be repository-relative: %s", value)
	}
	path := value
	for count := 0; count < 2; count++ {
		index := strings.LastIndex(path, ":")
		if index <= 0 {
			break
		}
		if !suffixIsDigits(path[index+1:]) {
			break
		}
		path = path[:index]
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("finding location must be repository-relative: %s", value)
	}
	return nil
}

func suffixIsDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func findingStableID(finding Finding) string {
	copy := finding
	copy.ID = ""
	return "finding-" + shortDigest(digestValue(copy))
}
