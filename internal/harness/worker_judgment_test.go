package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestWorkerJudgmentBindsApprovedCheckCoverage(t *testing.T) {
	checks := []Check{{ID: "first", Argv: []string{"go", "test", "./first"}}, {ID: "second", Argv: []string{"go", "test", "./second"}}}
	value := map[string]any{"status": "PASS", "summary": "Actual independent check review", "findings": []any{}, "check_reviews": map[string]any{"first": "The first command exercises the normal entry and compares its documented result.", "second": "The second assertion detects an invalid state transition at the approved boundary."}}
	result, err := validateWorkerAnswer("qa-design", value, checks)
	if err != nil {
		t.Fatal(err)
	}
	expected := WorkerResult{Status: "PASS", Summary: "Actual independent check review\nCheck first: The first command exercises the normal entry and compares its documented result.\nCheck second: The second assertion detects an invalid state transition at the approved boundary.", Findings: []Finding{}, CheckIDs: []string{"first", "second"}}
	if !reflect.DeepEqual(*result, expected) {
		t.Fatalf("lost issued check identity or judgment: %+v", result)
	}
	schema := WorkerSchema(checks...)
	properties := schema["properties"].(map[string]any)
	if properties["check_ids"] != nil || !reflect.DeepEqual(properties["check_reviews"].(map[string]any)["properties"].(map[string]any)["first"], map[string]any{"type": "string", "minLength": 1}) {
		t.Fatal("model still owns approved ID list")
	}
	for _, change := range []func(map[string]any){
		func(v map[string]any) { v["check_ids"] = []any{"first", "second"} },
		func(v map[string]any) { v["check_reviews"] = map[string]any{"first": "actual evidence"} },
		func(v map[string]any) { v["check_reviews"] = map[string]any{"first": "actual evidence", "second": ""} },
		func(v map[string]any) {
			v["check_reviews"] = map[string]any{"first": "actual evidence", "second": "actual evidence", "unknown": "unapproved"}
		},
	} {
		raw, _ := json.Marshal(value)
		var candidate map[string]any
		_ = json.Unmarshal(raw, &candidate)
		change(candidate)
		if _, err := validateWorkerAnswer("qa-review", candidate, checks); err == nil {
			t.Fatalf("accepted invalid semantic coverage: %v", candidate)
		}
	}
	value["status"] = "FAIL"
	value["findings"] = []any{map[string]any{"id": "gap", "severity": "P2", "message": "Approved checks miss a required error response.", "paths": []any{"check.go"}}}
	result, err = validateWorkerAnswer("qa-review", value, checks)
	if err != nil || result.Status != "FAIL" || !NeedsRepair(*result) || !strings.Contains(result.Summary, "second assertion") {
		t.Fatal("program inferred PASS from mechanical coverage", err)
	}
}
func TestBridgeUsesSemanticWorkerJudgment(t *testing.T) {
	result := map[string]any{"status": "PASS", "summary": "Reviewed documented behavior", "findings": []any{}, "check_reviews": map[string]any{"check": "The approved check has a real assertion of the required response."}}
	checks := []Check{{ID: "check", Argv: []string{"true"}}}
	payload, _ := json.Marshal(map[string]any{"role": "qa-review", "checks": checks, "result": result})
	actual, err := dispatch(envelope{Operation: "validate-worker", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	checked := actual.(map[string]any)["result"].(WorkerResult)
	if !reflect.DeepEqual(checked.CheckIDs, []string{"check"}) || checked.Status != "PASS" {
		t.Fatal("bridge did not assemble typed coverage")
	}
}

func plainWorkerSchema(checks ...Check) map[string]any {
	body, err := json.Marshal(WorkerSchema(checks...))
	if err != nil {
		return map[string]any{}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return map[string]any{}
	}
	return objectOrEmpty(value)
}

func validateWorkerAnswer(role string, answer map[string]any, checks []Check) (*WorkerResult, error) {
	body, err := canonicalBody(answer)
	if err != nil {
		return nil, err
	}
	var judgment WorkerJudgment
	if err := StrictDecode(body, &judgment); err != nil {
		return nil, fmt.Errorf("worker answer is not a valid judgment: %w", err)
	}
	worker, err := workerJudgmentResult(role, judgment, checks)
	if err != nil {
		return nil, fmt.Errorf("worker answer refused: %w", err)
	}
	return &worker, nil
}
