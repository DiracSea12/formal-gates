package validate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBehaviorPendingWithoutAnswers(t *testing.T) {
	root := t.TempDir()
	mustWriteBehaviorTest(t, filepath.Join(root, "cases.json"), `[{"id":"FG-BEH-001","expected_behavior":"Require artifact evidence before PASS."}]`)

	report, result := Behavior(BehaviorOptions{Root: root, CasesFile: "cases.json"})
	if !result.OK() {
		t.Fatalf("expected pending-only report to pass, got %#v", result.Failures)
	}
	if report.Summary.Pending != 1 || report.Cases[0].Status != "PENDING" {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestBehaviorEvaluatesSuppliedAnswers(t *testing.T) {
	root := t.TempDir()
	mustWriteBehaviorTest(t, filepath.Join(root, "cases.json"), `[{
		"id":"FG-BEH-001",
		"expected_behavior":"Require artifact evidence before PASS.",
		"must_include":["artifact","evidence"],
		"must_avoid":["self-approved"]
	}]`)
	mustWriteBehaviorTest(t, filepath.Join(root, "answers-pass.json"), `[{"id":"FG-BEH-001","answer":"PASS requires artifact evidence."}]`)
	mustWriteBehaviorTest(t, filepath.Join(root, "answers-fail.json"), `[{"id":"FG-BEH-001","answer":"This is self-approved."}]`)

	report, result := Behavior(BehaviorOptions{Root: root, CasesFile: "cases.json", AnswersFile: "answers-pass.json"})
	if !result.OK() || report.Summary.Pass != 1 {
		t.Fatalf("expected passing answer, report=%#v failures=%#v", report, result.Failures)
	}

	report, result = Behavior(BehaviorOptions{Root: root, CasesFile: "cases.json", AnswersFile: "answers-fail.json"})
	if result.OK() {
		t.Fatalf("expected failing answer, report=%#v", report)
	}
	if report.Summary.Fail != 1 || len(report.Cases[0].Missing) == 0 || len(report.Cases[0].Present) == 0 {
		t.Fatalf("expected missing and present markers, report=%#v", report)
	}
}

func TestBehaviorRequiresEveryAnswerWhenAnswersFileIsSupplied(t *testing.T) {
	root := t.TempDir()
	mustWriteBehaviorTest(t, filepath.Join(root, "cases.json"), `[
		{"id":"FG-BEH-001","must_include":["artifact"]},
		{"id":"FG-BEH-002","must_include":["independent"]}
	]`)
	mustWriteBehaviorTest(t, filepath.Join(root, "answers.json"), `[{"id":"FG-BEH-001","answer":"artifact"}]`)

	report, result := Behavior(BehaviorOptions{Root: root, CasesFile: "cases.json", AnswersFile: "answers.json"})
	if result.OK() {
		t.Fatalf("expected missing answer to fail, report=%#v", report)
	}
	if report.Summary.Pass != 1 || report.Summary.Fail != 1 || report.Summary.Pending != 0 {
		t.Fatalf("unexpected report summary: %#v", report.Summary)
	}
}

func TestBehaviorFixturePassesAllCases(t *testing.T) {
	root := repoRootValidateTest(t)

	report, result := Behavior(BehaviorOptions{
		Root:        root,
		CasesFile:   "examples/skill-behavior-prompts.json",
		AnswersFile: "examples/skill-behavior-answers.json",
	})
	if !result.OK() {
		t.Fatalf("expected behavior fixture to pass, failures=%#v report=%#v", result.Failures, report)
	}
	if report.Summary.Total == 0 || report.Summary.Pass != report.Summary.Total || report.Summary.Pending != 0 || report.Summary.Fail != 0 {
		t.Fatalf("unexpected fixture report: %#v", report.Summary)
	}
}

func TestBehaviorFixtureRejectsCandidateAcceptanceContractRegressions(t *testing.T) {
	root := repoRootValidateTest(t)
	cases, err := readBehaviorCases(filepath.Join(root, "examples", "skill-behavior-prompts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var candidateCase behaviorCase
	for _, tc := range cases {
		if tc.ID == "FG-BEH-025" {
			candidateCase = tc
			break
		}
	}
	if candidateCase.ID == "" {
		t.Fatal("FG-BEH-025 fixture is missing")
	}

	tests := []struct {
		name        string
		answer      string
		wantMissing string
		wantPresent string
	}{
		{
			name:        "formal acceptance before case approval",
			answer:      "Candidate development and QA Design proceed in parallel from the frozen requirement with mutual blindness. Candidate code may be formally accepted before the approved case chain. After approval, the formal handoff may adopt, modify, or delete the candidate code without rewriting.",
			wantMissing: "not formal acceptance before the approved case chain",
			wantPresent: "formally accepted before",
		},
		{
			name:        "approved candidate cannot be adopted modified or deleted",
			answer:      "Candidate development and QA Design proceed in parallel from the frozen requirement with mutual blindness. Candidate code is not formal acceptance before the approved case chain. After approval, the formal handoff retains the candidate code without rewriting.",
			wantMissing: "adopt, modify, or delete",
		},
		{
			name:        "approved candidate must be rewritten",
			answer:      "Candidate development and QA Design proceed in parallel from the frozen requirement with mutual blindness. Candidate code is not formal acceptance before the approved case chain. After approval, the formal handoff may adopt, modify, or delete the candidate code but must rewrite it.",
			wantMissing: "without rewriting",
			wantPresent: "must rewrite",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			check := evaluateBehaviorCase(candidateCase, tc.answer)
			if check.Status != "FAIL" {
				t.Fatalf("expected FG-BEH-025 regression to fail, got %#v", check)
			}
			if !containsBehaviorMarker(check.Missing, tc.wantMissing) {
				t.Fatalf("expected missing marker %q, got %#v", tc.wantMissing, check)
			}
			if tc.wantPresent != "" && !containsBehaviorMarker(check.Present, tc.wantPresent) {
				t.Fatalf("expected forbidden marker %q, got %#v", tc.wantPresent, check)
			}
		})
	}
}

func containsBehaviorMarker(markers []string, want string) bool {
	for _, marker := range markers {
		if marker == want {
			return true
		}
	}
	return false
}

func mustWriteBehaviorTest(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
