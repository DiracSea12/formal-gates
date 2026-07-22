package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validReviewerDispatch = "formal_gate_dispatch: complexity-gate\nCurrent requirement: requirements/current.md\nCurrent diff or proposed change: git diff base --\n"

const validFinalReviewerPrompt = validReviewerDispatch + "Worktree: /tmp/repo\nBase commit or snapshot: base..snapshot\nOutput path: .gates/runs/wf/restricted/review.json\nOutput format: schema-version-2 JSON\n"

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
		PromptText: validReviewerDispatch + "Please focus on this path, 刚修了一个问题",
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
		PromptText: validReviewerDispatch + "Use the current schema-version-2 contract and policy qa.execution.v2 for this independent review.",
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
		result, violations := DispatchPromptWithViolations(DispatchPromptOptions{Root: root, PromptText: validReviewerDispatch + test.prompt})
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

func TestDispatchPromptRequiresCurrentContextAndRejectsHistoricalFields(t *testing.T) {
	root := repoRootForCanaryTest(t)
	for _, test := range []struct {
		name   string
		prompt string
		label  string
	}{
		{name: "missing role", prompt: "Current requirement: requirements/current.md\nCurrent diff or proposed change: git diff base --", label: "formal_gate_dispatch:"},
		{name: "missing current requirement", prompt: "formal_gate_dispatch: complexity-gate\nCurrent diff or proposed change: git diff base --", label: "Current requirement:"},
		{name: "missing current diff", prompt: "formal_gate_dispatch: complexity-gate\nCurrent requirement: requirements/current.md", label: "Current diff or proposed change:"},
		{name: "context bundle", prompt: validReviewerDispatch + "Context bundle: bundle.json", label: "extra reviewer context"},
		{name: "workflow run path", prompt: strings.Replace(validReviewerDispatch, "requirements/current.md", ".gates/runs/wf/restricted/requirements.md", 1), label: "workflow-run artifact"},
		{name: "prior verdict", prompt: validReviewerDispatch + "Previous verdict: PASS", label: "extra reviewer context"},
		{name: "repair narrative", prompt: validReviewerDispatch + "Repair narrative: changed validation", label: "extra reviewer context"},
		{name: "target conclusion", prompt: validReviewerDispatch + "Target conclusion: PASS", label: "extra reviewer context"},
		{name: "directed focus", prompt: validReviewerDispatch + "Directed focus: input validation", label: "extra reviewer context"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, violations := DispatchPromptWithViolations(DispatchPromptOptions{Root: root, PromptText: test.prompt})
			if result.OK() {
				t.Fatal("expected prompt to be rejected")
			}
			found := false
			for _, violation := range violations {
				found = found || violation.Label == test.label || violation.Matched == test.label
			}
			if !found {
				t.Fatalf("expected violation %q, got %#v", test.label, violations)
			}
		})
	}
}

func TestDispatchPromptAllowsRestrictedOutputRouting(t *testing.T) {
	root := repoRootForCanaryTest(t)
	prompt := validReviewerDispatch + "Output path: .gates/runs/wf/restricted/review.json\nOutput format: schema-version-2 JSON\n"
	result, violations := DispatchPromptWithViolations(DispatchPromptOptions{Root: root, PromptText: prompt})
	if !result.OK() || len(violations) != 0 {
		t.Fatalf("restricted output routing was rejected: failures=%#v violations=%#v", result.Failures, violations)
	}
}

func TestFinalDispatchPromptRequiresExactSevenFields(t *testing.T) {
	root := repoRootForCanaryTest(t)
	for _, test := range []struct {
		name   string
		prompt string
		accept bool
	}{
		{name: "valid", prompt: validFinalReviewerPrompt, accept: true},
		{name: "lowercase pending prose", prompt: strings.Replace(validFinalReviewerPrompt, "requirements/current.md", "document pending state behavior", 1), accept: true},
		{name: "unknown field", prompt: validFinalReviewerPrompt + "Extra mystery field: value\n"},
		{name: "duplicate field", prompt: validFinalReviewerPrompt + "Current requirement: duplicate\n"},
		{name: "pending placeholder", prompt: strings.Replace(validFinalReviewerPrompt, "base..snapshot", "PENDING", 1)},
		{name: "free text", prompt: validFinalReviewerPrompt + "append this after validation\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, _ := DispatchPromptWithViolations(DispatchPromptOptions{Root: root, PromptText: test.prompt, FinalSend: true})
			if test.accept && !result.OK() {
				t.Fatalf("expected valid final prompt, failures=%#v", result.Failures)
			}
			if !test.accept && result.OK() {
				t.Fatal("expected strict final prompt validation to fail")
			}
		})
	}
}

func TestPrepareDispatchPromptBuildsAndValidatesExactSendBytes(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join(root, "hooks", "pollution-patterns.json"), `{"english":{"patternGroups":[]},"chinese":{"termGroups":[]}}`)
	run := filepath.Join(root, ".gates", "runs", "wf", "restricted")
	output := filepath.Join(run, "complexity", "prompt.txt")
	bundle := filepath.Join(run, "complexity", "bundle.json")
	mustWrite(bundle, "{}\n")

	prepared, result := PrepareDispatchPrompt(PrepareDispatchPromptOptions{
		Root: root, OutputFile: output, Gate: "complexity-gate", CurrentRequirement: "requirements/current.md",
		CurrentDiff: "git diff base --", Worktree: root, ChangeSnapshot: "base..snapshot",
		ReviewArtifact: ".gates/runs/wf/restricted/complexity/review.json",
		PolicyID:       "complexity.post-development.v2", ContextBundle: bundle,
	})
	if !result.OK() {
		t.Fatalf("prepare failed: %#v", result.Failures)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{
		"contextBundle=restricted/complexity/bundle.json sha256=" + sha256Bytes([]byte("{}\n")),
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("prepared prompt missing %q: %s", expected, text)
		}
	}
	if prepared.SHA256 != sha256Bytes(data) {
		t.Fatalf("prepared hash mismatch: got %s want %s", prepared.SHA256, sha256Bytes(data))
	}
	validated, _ := DispatchPromptWithViolations(DispatchPromptOptions{Root: root, PromptText: text, FinalSend: true})
	if !validated.OK() {
		t.Fatalf("prepared bytes failed final validation: %#v", validated.Failures)
	}
}

func TestPrepareDispatchPromptAllowsOnlyActiveQADesignReviewCaseSet(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join(root, "hooks", "pollution-patterns.json"), `{"english":{"patternGroups":[]},"chinese":{"termGroups":[]}}`)
	run := filepath.Join(root, ".gates", "runs", "wf", "restricted")
	caseSet := filepath.Join(run, "qa-design", "cases.md")
	bundle := filepath.Join(run, "qa-design", "bundle.json")
	mustWrite(caseSet, string(generatedQADesignTemplate(1)))
	mustWrite(bundle, "{}\n")
	options := PrepareDispatchPromptOptions{Root: root, OutputFile: filepath.Join(run, "qa-design", "prompt.txt"), Gate: "qa-test-gate", Stage: "Design Review", CurrentRequirement: "requirements/current.md", CurrentDiff: ".gates/runs/wf/restricted/qa-design/cases.md", Worktree: root, ChangeSnapshot: "snap", ReviewArtifact: ".gates/runs/wf/restricted/qa-design/review.json", PolicyID: "qa.design-review.v2", ContextBundle: bundle}
	if _, result := PrepareDispatchPrompt(options); !result.OK() {
		t.Fatalf("active QA case set was rejected: %#v", result.Failures)
	}
	for name, mutate := range map[string]func(*PrepareDispatchPromptOptions){
		"current requirement": func(o *PrepareDispatchPromptOptions) { o.CurrentRequirement = caseSet },
		"other gate": func(o *PrepareDispatchPromptOptions) {
			o.Gate, o.Stage, o.PolicyID = "complexity-gate", "", "complexity.post-development.v2"
		},
		"different run": func(o *PrepareDispatchPromptOptions) {
			o.CurrentDiff = ".gates/runs/other/restricted/qa-design/cases.md"
		},
		"outside restricted": func(o *PrepareDispatchPromptOptions) {
			o.CurrentDiff = ".gates/runs/wf/qa-design/cases.md"
		},
		"other run artifact": func(o *PrepareDispatchPromptOptions) {
			o.CurrentDiff = ".gates/runs/wf/restricted/qa-design/context-bundle.json"
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := options
			mutate(&candidate)
			if name == "different run" {
				mustWrite(filepath.Join(root, ".gates", "runs", "other", "restricted", "qa-design", "cases.md"), string(generatedQADesignTemplate(1)))
			}
			if name == "outside restricted" {
				mustWrite(filepath.Join(root, ".gates", "runs", "wf", "qa-design", "cases.md"), string(generatedQADesignTemplate(1)))
			}
			if _, result := PrepareDispatchPrompt(candidate); result.OK() {
				t.Fatal("workflow-run input was accepted outside the QA Design Review exception")
			}
		})
	}
}
