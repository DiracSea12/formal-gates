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
	value := map[string]any{"summary": "Actual independent check review", "findings": []any{}, "check_reviews": []string{"The first command exercises the normal entry and compares its documented result.", "The second assertion detects an invalid state transition at the approved boundary."}}
	result, err := validateWorkerAnswer("qa-design", value, checks)
	if err != nil {
		t.Fatal(err)
	}
	expected := WorkerResult{Status: "PASS", Summary: "Actual independent check review\nCheck first: The first command exercises the normal entry and compares its documented result.\nCheck second: The second assertion detects an invalid state transition at the approved boundary.", Findings: []Finding{}, CheckIDs: []string{"first", "second"}}
	if !reflect.DeepEqual(*result, expected) {
		t.Fatalf("lost issued check identity or judgment: %+v", result)
	}
	schema := WorkerSchema(checks...)
	properties := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
	if properties["check_ids"] != nil || properties["status"] != nil || properties["check_reviews"].(map[string]any)["anyOf"] == nil {
		t.Fatal("model still owns approved ID list")
	}
	for _, change := range []func(map[string]any){
		func(v map[string]any) { v["check_ids"] = []any{"first", "second"} },
		func(v map[string]any) { v["check_reviews"] = []string{"actual evidence"} },
		func(v map[string]any) { v["check_reviews"] = []string{"actual evidence", ""} },
		func(v map[string]any) {
			v["check_reviews"] = []string{"actual evidence", "actual evidence", "unapproved"}
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

	value["findings"] = []any{map[string]any{"severity": "P2", "message": "Approved checks miss a required error response.", "paths": []any{"check.go"}}}
	result, err = validateWorkerAnswer("qa-review", value, checks)
	if err != nil || result.Status != "PASS" || !NeedsRepair(*result) || !strings.Contains(result.Summary, "second assertion") {
		t.Fatal("program inferred PASS from mechanical coverage", err)
	}
}
func TestBridgeUsesSemanticWorkerJudgment(t *testing.T) {
	result := map[string]any{"summary": "Reviewed documented behavior", "findings": []any{}, "check_reviews": []string{"The approved check has a real assertion of the required response."}}
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

func TestWorkerJudgmentAssignsFindingIdentityAndPreservesEvidence(t *testing.T) {
	value := map[string]any{"summary": "Observed the documented command failure", "findings": []any{
		map[string]any{"severity": "P1", "message": "Valid input exits without its required result.", "paths": []any{"app.go"}},
		map[string]any{"severity": "P1", "message": "Valid input exits without its required result.", "paths": []any{"app.go"}},
	}, "check_reviews": []string{}}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	result, err := validateWorkerAnswer("implementation-quality-gate", value, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 2 || result.Findings[0].ID == result.Findings[1].ID {
		t.Fatal("repeated findings lost their distinct occurrences")
	}
	for _, finding := range result.Findings {
		if !validID(finding.ID) || finding.Severity != "P1" || finding.Message != "Valid input exits without its required result." || !reflect.DeepEqual(finding.Paths, []string{"app.go"}) {
			t.Fatalf("finding identity assembly changed authored evidence: %+v", finding)
		}
	}
	again, err := validateWorkerAnswer("implementation-quality-gate", value, nil)
	if err != nil || !reflect.DeepEqual(result, again) {
		t.Fatal("same judgment produced different finding identities")
	}
	other, err := validateWorkerAnswer("complexity-gate", value, nil)
	if err != nil || other.Findings[0].ID == result.Findings[0].ID {
		t.Fatal("finding identity ignored the bound role")
	}
	after, err := json.Marshal(value)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("finding identity assembly rewrote the model response")
	}
	findingSchema := WorkerSchema()["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["findings"].(map[string]any)["items"].(map[string]any)
	if findingSchema["properties"].(map[string]any)["id"] != nil || !reflect.DeepEqual(findingSchema["required"], []string{"severity", "message", "paths"}) {
		t.Fatal("model contract contains a program-owned finding identity")
	}
	value["findings"].([]any)[0].(map[string]any)["id"] = "authored-id"
	if _, err := validateWorkerAnswer("implementation-quality-gate", value, nil); err == nil {
		t.Fatal("model-owned finding identity was accepted")
	}
	feedback, err := json.Marshal(map[string]any{"revision": "confirmed", "role": "implementation-quality-gate", "result": result})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := planEvidenceFeedback(evidenceFeedbackInput{Ref: "evidence", Purpose: "repair", Revision: "confirmed", Evidence: feedback})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range result.Findings {
		if !bytes.Contains(body, []byte(finding.ID)) || !bytes.Contains(body, []byte(finding.Message)) {
			t.Fatal("feedback lost the actual finding identity or content")
		}
	}
}

func TestWorkerJudgmentDerivesSemanticStatusAndKeepsRealInability(t *testing.T) {
	for _, sample := range []struct {
		role   string
		body   map[string]any
		status string
	}{
		{"development", map[string]any{"summary": "Progress", "findings": []any{}, "check_reviews": []string{}, "remainingWork": "Implement remaining public behavior"}, "CONTINUE"},
		{"qa-review", map[string]any{"interruptionReason": "The registered workspace is unavailable"}, "BLOCKED"},
	} {
		result, err := validateWorkerAnswer(sample.role, sample.body, nil)
		if err != nil || result.Status != sample.status {
			t.Fatal("program lost actual semantic reason", err)
		}
		sample.body["status"] = sample.status
		if _, err := validateWorkerAnswer(sample.role, sample.body, nil); err == nil {
			t.Fatal("model status accepted")
		}
	}
}
