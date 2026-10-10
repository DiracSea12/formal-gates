package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"formal-gates/internal/prompts"
	"io"
	"os"
	"path/filepath"

	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/requirement"
	"strings"
	"unicode/utf8"

	"formal-gates/internal/lifecycle"
)

func gateResultContract() string {
	return "[Result contract]\nReturn exactly one JSON object: {\"findings\":[{\"severity\":\"P0|P1|P2|P3\",\"message\":\"...\",\"locations\":[{\"read\":1,\"line\":2}]}]}. If you cannot perform the review, return {\"interruptionReason\":\"the actual reason\"}. Select read/line or listing/entry numbers only from the supplied original locationFacts; the program resolves paths and derives the overall status. Do not fill status, identities, or wrapper metadata. Do not add Markdown fences."
}

// StandaloneGateContext is original comparison evidence, outside run state.
type StandaloneGateContext struct {
	Root          string                `json:"root"`
	VCS           string                `json:"vcs"`
	Base          string                `json:"base"`
	Scope         string                `json:"scope,omitempty"`
	LocationFacts []worker.LocationFact `json:"locationFacts"`
}

func CaptureStandaloneGateContext(root, provider, scope string) (StandaloneGateContext, error) {
	root = lifecycle.CleanRoot(root)
	provider = strings.ToLower(strings.TrimSpace(provider))
	resolver, err := resolverForVCS(provider, nil)
	if err != nil {
		return StandaloneGateContext{}, err
	}
	base, err := resolver.Resolve(root)
	if err != nil {
		return StandaloneGateContext{}, err
	}
	paths, err := vcs.NewNative(root, provider, nil).ChangedPaths()
	if err != nil {
		return StandaloneGateContext{}, err
	}
	context := StandaloneGateContext{Root: root, VCS: provider, Base: base, Scope: scope, LocationFacts: []worker.LocationFact{}}
	for _, path := range paths {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if os.IsNotExist(err) {
			context.LocationFacts = append(context.LocationFacts, worker.LocationFact{Kind: "listing", Paths: []string{path}})
			continue
		}
		if err != nil {
			return StandaloneGateContext{}, err
		}
		if utf8.Valid(content) {
			context.LocationFacts = append(context.LocationFacts, worker.LocationFact{Kind: "read", Paths: []string{path}, Content: string(content)})
		} else {
			context.LocationFacts = append(context.LocationFacts, worker.LocationFact{Kind: "listing", Paths: []string{path}})
		}
	}
	return context, nil
}

// ComposeStandaloneGatePrompt consumes the captured comparison, not a new scope.
func ComposeStandaloneGatePrompt(catalog prompts.PromptCatalog, gateID string, context StandaloneGateContext) (string, error) {
	gate, ok := catalog.Gate(gateID)
	if !ok {
		return "", fmt.Errorf("unknown discovered gate %q", gateID)
	}
	published := []map[string]any{}
	for index, fact := range context.LocationFacts {
		item := map[string]any{}
		if fact.Kind == "read" {
			item["read"], item["path"] = index+1, fact.Paths[0]
		} else {
			item["listing"] = index + 1
			entries := []map[string]any{}
			for entry, path := range fact.Paths {
				entries = append(entries, map[string]any{"entry": entry + 1, "path": path})
			}
			item["entries"] = entries
		}
		published = append(published, item)
	}
	facts, err := json.MarshalIndent(published, "", "  ")
	if err != nil {
		return "", err
	}
	parts := []string{
		"[Shared reviewer contract]\n" + catalog.Base,
		fmt.Sprintf("[Gate: %s]\n%s", gate.ID, gate.Content),
		standaloneChangeBlock(context.Root, context.VCS, context.Base, context.Scope),
		"[Source locations]\nOriginal locationFacts indexes; source content remains in the original report context named in Current change:\n" + string(facts),
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
// It uses only the original captured location facts; no live scope is rediscovered.
func ValidateStandaloneGateResult(raw []byte, context StandaloneGateContext) (StandaloneGateResult, error) {
	var wire struct {
		Message            string            `json:"message,omitempty"`
		Findings           *[]worker.Finding `json:"findings,omitempty"`
		InterruptionReason *string           `json:"interruptionReason,omitempty"`
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
		for _, finding := range *wire.Findings {
			locations, err := worker.LocationPaths(finding.Locations, context.LocationFacts, requirement.FrozenRequirementProjection{})
			if err != nil {
				return StandaloneGateResult{}, err
			}
			result.Findings = append(result.Findings, FindingInput{Severity: finding.Severity, SeverityRationale: finding.SeverityRationale, Category: finding.Category, Requirement: finding.Requirement, NormalPath: finding.NormalPath, ActualError: finding.ActualError, DeferralImpact: finding.DeferralImpact, Verification: FindingVerification(finding.Verification), Message: finding.Message, Locations: locations})
		}
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
