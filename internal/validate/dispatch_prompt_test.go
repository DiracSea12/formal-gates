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
