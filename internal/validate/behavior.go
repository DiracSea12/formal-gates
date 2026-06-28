package validate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type BehaviorOptions struct {
	Root        string
	CasesFile   string
	AnswersFile string
}

type BehaviorReport struct {
	SchemaVersion int              `json:"schemaVersion"`
	CasesFile     string           `json:"casesFile"`
	AnswersFile   string           `json:"answersFile,omitempty"`
	Summary       BehaviorSummary  `json:"summary"`
	Cases         []BehaviorResult `json:"cases"`
}

type BehaviorSummary struct {
	Total   int `json:"total"`
	Pass    int `json:"pass"`
	Fail    int `json:"fail"`
	Pending int `json:"pending"`
}

type BehaviorResult struct {
	ID      string   `json:"id"`
	Status  string   `json:"status"`
	Missing []string `json:"missing,omitempty"`
	Present []string `json:"present,omitempty"`
	Reason  string   `json:"reason,omitempty"`
}

type behaviorCase struct {
	ID               string   `json:"id"`
	ExpectedBehavior string   `json:"expected_behavior"`
	Expected         string   `json:"expected"`
	MustInclude      []string `json:"must_include"`
	MustAvoid        []string `json:"must_avoid"`
}

type behaviorAnswer struct {
	ID     string `json:"id"`
	Answer string `json:"answer"`
}

func Behavior(options BehaviorOptions) (BehaviorReport, Result) {
	root := cleanRoot(options.Root)
	casesRel := firstNonEmpty(options.CasesFile, "examples/skill-behavior-prompts.json")
	casesPath := resolvePath(root, casesRel)
	report := BehaviorReport{
		SchemaVersion: 1,
		CasesFile:     filepath.ToSlash(casesRel),
	}
	var result Result
	cases, err := readBehaviorCases(casesPath)
	if err != nil {
		result.add(filepath.ToSlash(casesRel), err.Error())
		return report, result
	}
	answers := map[string]string{}
	requireAnswers := false
	if strings.TrimSpace(options.AnswersFile) != "" {
		requireAnswers = true
		answersRel := options.AnswersFile
		answersPath := resolvePath(root, answersRel)
		report.AnswersFile = filepath.ToSlash(answersRel)
		answers, err = readBehaviorAnswers(answersPath)
		if err != nil {
			result.add(filepath.ToSlash(answersRel), err.Error())
			return report, result
		}
	}
	for _, tc := range cases {
		answer, hasAnswer := answers[tc.ID]
		check := evaluateBehaviorCase(tc, answer)
		if requireAnswers && (!hasAnswer || strings.TrimSpace(answer) == "") {
			check.Status = "FAIL"
			check.Reason = "answer not supplied"
		}
		report.Cases = append(report.Cases, check)
		report.Summary.Total++
		switch check.Status {
		case "PASS":
			report.Summary.Pass++
		case "FAIL":
			report.Summary.Fail++
			result.add(tc.ID, check.Reason)
		default:
			report.Summary.Pending++
		}
	}
	return report, result
}

func readBehaviorCases(path string) ([]behaviorCase, error) {
	text, err := readText(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read behavior cases: %w", err)
	}
	var cases []behaviorCase
	if err := json.Unmarshal([]byte(text), &cases); err != nil {
		return nil, fmt.Errorf("invalid behavior cases JSON: %w", err)
	}
	return cases, nil
}

func readBehaviorAnswers(path string) (map[string]string, error) {
	text, err := readText(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read behavior answers: %w", err)
	}
	var answers []behaviorAnswer
	if err := json.Unmarshal([]byte(text), &answers); err != nil {
		return nil, fmt.Errorf("invalid behavior answers JSON: %w", err)
	}
	out := map[string]string{}
	for _, answer := range answers {
		out[answer.ID] = answer.Answer
	}
	return out, nil
}

func evaluateBehaviorCase(tc behaviorCase, answer string) BehaviorResult {
	if strings.TrimSpace(answer) == "" {
		return BehaviorResult{ID: tc.ID, Status: "PENDING", Reason: "answer not supplied"}
	}
	mustInclude := tc.MustInclude
	if len(mustInclude) == 0 {
		mustInclude = keyPhrases(firstNonEmpty(tc.ExpectedBehavior, tc.Expected))
	}
	check := BehaviorResult{ID: tc.ID}
	answerLower := strings.ToLower(answer)
	for _, phrase := range mustInclude {
		if !containsPhrase(answerLower, phrase) {
			check.Missing = append(check.Missing, phrase)
		}
	}
	for _, phrase := range tc.MustAvoid {
		if containsPhrase(answerLower, phrase) {
			check.Present = append(check.Present, phrase)
		}
	}
	if len(check.Missing) > 0 || len(check.Present) > 0 {
		check.Status = "FAIL"
		check.Reason = "answer does not match expected behavior markers"
		return check
	}
	check.Status = "PASS"
	return check
}

func keyPhrases(text string) []string {
	normalized := strings.ToLower(text)
	words := regexp.MustCompile(`[a-z][a-z0-9-]+`).FindAllString(normalized, -1)
	seen := map[string]bool{}
	var phrases []string
	for _, word := range words {
		if len(word) < 6 || behaviorStopWords[word] || seen[word] {
			continue
		}
		seen[word] = true
		phrases = append(phrases, word)
		if len(phrases) >= 4 {
			break
		}
	}
	return phrases
}

func containsPhrase(answerLower, phrase string) bool {
	return strings.Contains(answerLower, strings.ToLower(strings.TrimSpace(phrase)))
}

var behaviorStopWords = map[string]bool{
	"expected": true,
	"behavior": true,
	"should":   true,
	"without":  true,
	"before":   true,
	"formal":   true,
	"require":  true,
	"requires": true,
}
