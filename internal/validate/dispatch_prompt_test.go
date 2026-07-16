package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDispatchPromptWithViolationsUsesDefaultPatternsFile(t *testing.T) {
	root := t.TempDir()
	patternsDir := filepath.Join(root, "hooks")
	if err := os.MkdirAll(patternsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := pollutionConfig{}
	config.English.PatternGroups = []pollutionPatternGroup{
		{
			Label:       "focus direction",
			Description: "directed review is anchoring",
			Patterns:    []string{`(?i)\bfocus on\b`},
		},
	}
	config.Chinese.TermGroups = []pollutionTermGroup{
		{
			Label:       "fix reference",
			Description: "mentions of fixes are anchoring",
			Terms:       []string{"刚修了"},
		},
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(patternsDir, "pollution-patterns.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	result, violations := DispatchPromptWithViolations(DispatchPromptOptions{
		Root:       root,
		PromptText: "Please focus on this path, 刚修了一个问题",
	})
	if result.OK() {
		t.Fatal("expected violations")
	}
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(violations))
	}
	if violations[0].Label != "focus direction" {
		t.Fatalf("unexpected first label: %#v", violations[0])
	}
	if violations[1].Label != "fix reference" {
		t.Fatalf("unexpected second label: %#v", violations[1])
	}
}

func TestDispatchPromptWithViolationsReportsMissingPatternsFile(t *testing.T) {
	root := t.TempDir()
	result, violations := DispatchPromptWithViolations(DispatchPromptOptions{
		Root:       root,
		PromptText: "clean prompt",
	})
	if result.OK() {
		t.Fatal("expected missing config to fail")
	}
	if len(violations) != 0 {
		t.Fatalf("expected no violations on config failure, got %d", len(violations))
	}
}

func TestDispatchPromptAllowsCurrentContractVersionsAndBlocksAnchoring(t *testing.T) {
	root := repoRootForCanaryTest(t)
	result, violations := DispatchPromptWithViolations(DispatchPromptOptions{
		Root:       root,
		PromptText: "Use the current schema-version-2 contract and policy qa.execution.v2 for this independent review.",
	})
	if !result.OK() || len(violations) != 0 {
		t.Fatalf("expected current contract versions to pass, failures=%#v violations=%#v", result.Failures, violations)
	}

	for _, test := range []struct {
		prompt string
		label  string
	}{
		{prompt: "The prior review used qa.execution.v1; compare it with qa.execution.v2.", label: "previous review reference"},
		{prompt: "This was just fixed in qa.execution.v2; assess the revised result.", label: "fix reference"},
	} {
		result, violations := DispatchPromptWithViolations(DispatchPromptOptions{Root: root, PromptText: test.prompt})
		if result.OK() {
			t.Fatalf("expected anchoring prompt to fail: %q", test.prompt)
		}
		found := false
		for _, violation := range violations {
			found = found || violation.Label == test.label
		}
		if !found {
			t.Fatalf("expected %q violation for %q, got %#v", test.label, test.prompt, violations)
		}
	}
}
