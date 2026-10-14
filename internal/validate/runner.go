package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"formal-gates/internal/prompts"
	"io"

	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/requirement"
	"strings"
	"unicode/utf8"

	"formal-gates/internal/lifecycle"
)

func gateResultContract() string {
	return "[Result contract]\nUse the program-provided gate report command to submit semantic review arguments. A completed review uses --review-complete, with optional --message. Start each finding with --finding-severity P0|P1|P2|P3, then --finding-message and its evidence: --finding-severity-rationale, --finding-category, --finding-requirement, --finding-normal-path, --finding-actual-error, --finding-deferral-impact, and --finding-verify-scope, --finding-verify-severity, --finding-verify-causality, --finding-verify-reproduction, --finding-verify-classification. Select original source locations with --finding-read and optional --finding-line, or --finding-listing and --finding-entry; repeat a source selector for another location. If you cannot perform the review, use --interruption-reason with the actual reason. Source numbers refer only to the supplied original locationFacts. The program constructs the result, resolves paths, and derives the overall status."
}

// StandaloneGateContext is original comparison evidence, outside run state.
type StandaloneGateContext struct {
	Root          string                   `json:"root"`
	VCS           string                   `json:"vcs"`
	Base          string                   `json:"base"`
	Scope         string                   `json:"scope,omitempty"`
	Comparison    []vcs.SnapshotComparison `json:"comparison"`
	LocationFacts []worker.LocationFact    `json:"locationFacts"`
}

func CaptureStandaloneGateContext(root, provider, scope string) (StandaloneGateContext, error) {
	root = lifecycle.CleanRoot(root)
	provider = strings.ToLower(strings.TrimSpace(provider))
	native := vcs.NewNative(root, provider, nil)
	provider = native.Name()
	base, err := native.Resolve()
	if err != nil {
		return StandaloneGateContext{}, err
	}
	rows, err := native.CompareWorkingTree(base)
	if err != nil {
		return StandaloneGateContext{}, err
	}
	context := StandaloneGateContext{Root: root, VCS: provider, Base: base, Scope: scope, Comparison: rows, LocationFacts: []worker.LocationFact{}}
	for _, row := range rows {
		content, mode := row.Content, row.Mode
		if mode == "000000" {
			content, mode = row.BaseContent, row.BaseMode
		}
		if mode != "040000" && utf8.Valid(content) {
			context.LocationFacts = append(context.LocationFacts, worker.LocationFact{Kind: "read", Paths: []string{row.Path}, Content: content})
		} else {
			context.LocationFacts = append(context.LocationFacts, worker.LocationFact{Kind: "listing", Paths: []string{row.Path}})
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
	comparison := make([]map[string]any, 0, len(context.Comparison))
	for _, row := range context.Comparison {
		value := map[string]any{"path": row.Path, "kind": row.Kind, "baseMode": row.BaseMode, "mode": row.Mode, "before": row.BaseContent, "after": row.Content}
		for _, side := range []struct {
			key     string
			content []byte
		}{{"before", row.BaseContent}, {"after", row.Content}} {
			if utf8.Valid(side.content) {
				value[side.key] = string(side.content)
			} else {
				value[side.key+"Encoding"] = "base64"
			}
		}
		for _, side := range []struct{ key, raw string }{{"baseMetadata", row.BaseMetadata}, {"metadata", row.Metadata}} {
			if side.raw != "" {
				value[side.key] = json.RawMessage(side.raw)
			}
		}
		comparison = append(comparison, value)
	}
	changes, err := json.MarshalIndent(comparison, "", "  ")
	if err != nil {
		return "", err
	}
	parts := []string{
		"[Shared reviewer contract]\n" + catalog.Base,
		fmt.Sprintf("[Gate: %s]\n%s", gate.ID, gate.Content),
		standaloneChangeBlock(context.Root, context.VCS, context.Base, context.Scope),
		"[Native comparison]\nProgram-captured original before/after bytes and native modes, including untracked additions and deletions. Text is UTF-8; binary sides carry an explicit base64 encoding.\n" + string(changes),
		"[Source locations]\nOriginal locationFacts indexes for the comparison above. Read lines select the after content; a deletion selects the before content.\n" + string(facts),
		gateResultContract(),
	}
	return strings.Join(parts, "\n\n") + "\n", nil
}

func standaloneChangeBlock(root, vcs, base, scope string) string {
	lines := []string{"[Current change]", "worktree: " + absPath(root), "vcs: " + vcs, "base snapshot: " + base, "current: working tree (uncommitted changes)"}
	if scope = strings.TrimSpace(scope); scope != "" {
		lines = append(lines, "Review scope (user-specified): "+scope)
	}
	// 单跑脱离 run、无需需求文档，故本提示词不含需求
	// 块。这是设计意图，零上下文审查者不应因缺需求块而报 RUNTIME_ERROR。
	lines = append(lines, "This standalone gate run intentionally has no requirement block: it reviews the working tree vs the base snapshot without a requirement document. A missing requirement block is by design and must not be reported as RUNTIME_ERROR.")
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
	status, err := validateSemanticResult(interruption, result.Findings)
	if err != nil {
		return StandaloneGateResult{}, err
	}
	result.Status = status
	return result, nil
}
