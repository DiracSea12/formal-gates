package worker

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
)

func requirementInput(handler string) protocol.WorkerInput {
	points := []requirement.Point{
		{ID: "AC-001", RequirementID: "REQ-001", Requirement: "Both valid and invalid input retain the public contract", Source: "confirmed user request", Text: "Valid input returns the value", Oracle: "actual value"},
		{ID: "AC-002", RequirementID: "REQ-001", Requirement: "Both valid and invalid input retain the public contract", Source: "confirmed user request", Text: "Invalid input returns an error", Oracle: "actual error"},
	}
	return protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: handler, Provider: "host", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "Confirmed complete input contract"}, RequirementPoints: points, Validation: &protocol.ValidationInput{}}
}

func TestRequirementCompletionBindsOriginalPointsAcrossRecovery(t *testing.T) {
	input := requirementInput("engine.development.batch.v5")
	input.Validation.Development = &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "input", RequirementPoints: []string{"AC-001", "AC-002"}, DoD: []protocol.DevelopmentCriterion{{ID: "criterion", Description: "Input behavior"}}}}
	raw := []byte(`{"summary":"Implemented the confirmed input contract","completion":{"dod":["Both paths tested"],"handoff":"Ready","requirements":["Public result matches the requested value","Public error matches the requested rejection"]}}`)
	original, err := DecodeResultWithPaths(input, "result", raw, "actual-output", []string{"input.go"})
	if err != nil {
		t.Fatal(err)
	}
	frozen, _ := json.Marshal(input)
	var recovered protocol.WorkerInput
	if err := json.Unmarshal(frozen, &recovered); err != nil {
		t.Fatal(err)
	}
	replay, err := DecodeResultWithPaths(recovered, "result", raw, "actual-output", []string{"input.go"})
	if err != nil || !reflect.DeepEqual(original, replay) {
		t.Fatalf("original completion changed on recovery: %v", err)
	}
	results := original.Result.Validation.Development.Completion.RequirementResults
	if len(results) != 2 || results[0].PointID != "AC-001" || results[1].PointID != "AC-002" || results[0].Status != "PASS" || results[1].Evidence != "Public error matches the requested rejection" {
		t.Fatalf("lost original requirement identities or evidence: %+v", results)
	}
	for _, requirements := range []string{`[]`, `["only one"]`, `["complete",""]`, `["one","two","extra"]`} {
		bad := strings.Replace(string(raw), `["Public result matches the requested value","Public error matches the requested rejection"]`, requirements, 1)
		if _, err := DecodeResultWithPaths(input, "invalid", []byte(bad), "invalid-output", []string{}); err == nil {
			t.Fatalf("incomplete point evidence accepted: %s", requirements)
		}
	}
	continuation, err := DecodeResultWithPaths(input, "continued", []byte(`{"summary":"Valid path implemented","remainingWork":"Implement invalid input rejection"}`), "actual-progress", []string{"input.go"})
	if err != nil || continuation.Result.Validation.Status != "CONTINUE" || continuation.Result.Validation.Development.Completion != nil {
		t.Fatalf("unfinished point was represented as completion: %v", err)
	}
	schema, err := ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	completion := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["completion"].(map[string]any)
	rows := completion["properties"].(map[string]any)["requirements"].(map[string]any)
	if rows["minItems"] != 2 || rows["maxItems"] != 2 || !strings.Contains(rows["prefixItems"].([]any)[0].(map[string]any)["description"].(string), input.RequirementPoints[0].Requirement) {
		t.Fatal("completion schema lost exact scope or parent requirement semantics")
	}
}

func TestGateRequirementDecisionsRetainFindingSeverity(t *testing.T) {
	input := requirementInput("engine.gate.v3")
	input.Validation.Gate = &protocol.GateInput{Gate: "implementation"}
	raw := []byte(`{"findings":[{"severity":"P2","message":"Bounded input issue"}],"requirements":[{"status":"PASS","evidence":"Public valid path inspected"},{"status":"FAIL","evidence":"The documented invalid path has bounded impact","findings":[1]}]}`)
	original, err := DecodeResultWithPaths(input, "result", raw, "actual-review", nil)
	if err != nil {
		t.Fatal(err)
	}
	if original.Result.Validation.Status != "PASS" || original.Result.Validation.RequirementResults[1].Status != "FAIL" || !reflect.DeepEqual(original.Result.Validation.RequirementResults[1].FindingIndexes, []int{0}) {
		t.Fatalf("point result changed gate severity or original finding binding: %+v", original.Result.Validation)
	}
	for _, bad := range []string{
		`{"findings":[],"requirements":[]}`,
		`{"findings":[],"requirements":[{"status":"PASS","evidence":"one"}]}`,
		`{"findings":[],"requirements":[{"status":"PASS","evidence":"one"},{"status":"FAIL","evidence":"two"}]}`,
		`{"findings":[],"requirements":[{"status":"PASS","evidence":"one"},{"status":"FAIL","evidence":"two","findings":[1]}]}`,
		`{"findings":[],"requirements":[{"status":"PASS","evidence":"one"},{"status":"PASS","evidence":""}]}`,
	} {
		if _, err := DecodeResultWithPaths(input, "invalid", []byte(bad), "invalid-review", nil); err == nil {
			t.Fatalf("unbound point review accepted: %s", bad)
		}
	}
}

func TestPlanRequirementAssignmentsUsePublishedPointOrder(t *testing.T) {
	input := requirementInput("engine.readiness.plan.v5")
	input.Review = &protocol.WorkerReviewInput{Action: review.StartReadiness}
	raw := []byte(`{"reason":"One input contract","verificationTier":"FINAL","batches":[{"deliverable":"Input contract","requirements":[2,1]}]}`)
	var plan Granularity
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	result, err := DecodeGranularity(input, &plan, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Batches[0].RequirementPoints, []string{"AC-001", "AC-002"}) {
		t.Fatalf("assignment order diverged from published points: %+v", result.Batches)
	}
	input.ApprovedPlan = result
	var revision Granularity
	if err := json.Unmarshal([]byte(`{"batches":[{"reuse":1,"risk":"Invalid input impact"}]}`), &revision); err != nil {
		t.Fatal(err)
	}
	changed, err := DecodeGranularity(input, &revision, nil, nil, false)
	if err != nil || !reflect.DeepEqual(changed.Batches[0].RequirementPoints, result.Batches[0].RequirementPoints) {
		t.Fatalf("unchanged assignment was rewritten: %v", err)
	}
	input.Handler, input.Review = "engine.development.batch.v5", nil
	input.Validation.Development = &protocol.DevelopmentInput{Batch: changed.Batches[0]}
	catalog, err := prompts.LoadPromptCatalog("../../..")
	if err != nil {
		t.Fatal(err)
	}
	material, err := Render(input, catalog)
	if err != nil {
		t.Fatal(err)
	}
	completion := material.ResultSchema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["completion"].(map[string]any)
	rows := completion["properties"].(map[string]any)["requirements"].(map[string]any)["prefixItems"].([]any)
	for i, point := range input.RequirementPoints {
		if !strings.Contains(rows[i].(map[string]any)["description"].(string), point.Text) {
			t.Fatal("published schema changed point order")
		}
	}
	if strings.Index(material.Prompt, input.RequirementPoints[0].Text) > strings.Index(material.Prompt, input.RequirementPoints[1].Text) {
		t.Fatal("material changed point order")
	}
	completed, err := DecodeResultWithPaths(input, "result", []byte(`{"summary":"Implemented both input paths","completion":{"dod":[],"handoff":"Ready","requirements":["Actual valid result","Actual invalid error"]}}`), "actual-output", []string{"input.go"})
	if err != nil {
		t.Fatal(err)
	}
	results := completed.Result.Validation.Development.Completion.RequirementResults
	if results[0].PointID != "AC-001" || results[0].Evidence != "Actual valid result" || results[1].PointID != "AC-002" || results[1].Evidence != "Actual invalid error" {
		t.Fatalf("reverse authored selection changed evidence binding: %+v", results)
	}
}

func TestRequirementMaterialsKeepQualityAndHonestExecutionDuties(t *testing.T) {
	input := requirementInput("engine.development.batch.v5")
	input.Validation.Development = &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "input", RequirementPoints: []string{"AC-001", "AC-002"}}}
	catalog, err := prompts.LoadPromptCatalog("../../..")
	if err != nil {
		t.Fatal(err)
	}
	material, err := Render(input, catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"requirementPoints", input.RequirementPoints[0].Requirement, input.RequirementPoints[1].Text, "readability", "maintainability", "It does not establish QA execution", "completion.requirements"} {
		if !strings.Contains(material.Prompt, required) {
			t.Fatalf("material omitted current point or independent duty: %s", required)
		}
	}
}

func TestRepairCompletionCoversOnlyConfirmedAffectedContribution(t *testing.T) {
	input := requirementInput("engine.repair.v6")
	input.Validation.Repair = &protocol.RepairInput{}
	raw := []byte(`{"summary":"Restored invalid input rejection","granularityReview":{"reason":"One affected path","verificationTier":"FINAL","batches":[{"deliverable":"Invalid input path","requirements":[2],"dod":[{"description":"Invalid input rejection restored"}]}]},"completion":{"dod":["Original failure restored"],"handoff":"Ready","requirements":["Actual invalid input error restored"]}}`)
	event, err := DecodeResultWithPaths(input, "result", raw, "actual-repair", []string{"input.go"})
	if err != nil {
		t.Fatal(err)
	}
	results := event.Result.Validation.Development.Completion.RequirementResults
	if len(results) != 1 || results[0].PointID != "AC-002" || results[0].Evidence != "Actual invalid input error restored" {
		t.Fatalf("repair expanded beyond the selected original obligation: %+v", results)
	}
	schema, err := ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	alternatives := schema["anyOf"].([]any)
	completed := alternatives[len(alternatives)-1].(map[string]any)
	rows := completed["properties"].(map[string]any)["completion"].(map[string]any)["properties"].(map[string]any)["requirements"].(map[string]any)
	if rows["prefixItems"] != nil || rows["minItems"] != nil || rows["maxItems"] != nil {
		t.Fatal("first repair completion was frozen to the whole choice inventory")
	}
}

func TestStandaloneRepairRequiresActualAffectedPointEvidence(t *testing.T) {
	input := requirementInput("engine.repair.v6")
	input.Validation.Repair = &protocol.RepairInput{RequiredPoints: input.RequirementPoints[1:]}
	if _, err := DecodeResultWithPaths(input, "result", []byte(`{"summary":"Restored invalid input rejection"}`), "actual-repair", []string{"input.go"}); err == nil {
		t.Fatal("standalone repair accepted summary-only completion without actual affected point evidence")
	}
	raw := []byte(`{"summary":"Restored invalid input rejection","requirements":["Actual invalid input error restored"]}`)
	event, err := DecodeResultWithPaths(input, "result", raw, "actual-repair", []string{"input.go"})
	if err != nil {
		t.Fatal(err)
	}
	development := event.Result.Validation.Development
	if development.Completion != nil || len(development.RequirementResults) != 1 || development.RequirementResults[0].PointID != "AC-002" || development.RequirementResults[0].Evidence != "Actual invalid input error restored" || development.RequirementResults[0].Status != "PASS" {
		t.Fatalf("standalone evidence lost its exact original duty: %+v", development)
	}
	frozen, _ := json.Marshal(input)
	var recovered protocol.WorkerInput
	if err := json.Unmarshal(frozen, &recovered); err != nil {
		t.Fatal(err)
	}
	replay, err := DecodeResultWithPaths(recovered, "result", raw, "actual-repair", []string{"input.go"})
	if err != nil || !reflect.DeepEqual(event, replay) {
		t.Fatalf("recovery changed standalone point binding: %v", err)
	}
	schema, err := ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	rows := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["requirements"].(map[string]any)
	if rows["minItems"] != 1 || rows["maxItems"] != 1 || !strings.Contains(rows["prefixItems"].([]any)[0].(map[string]any)["description"].(string), input.RequirementPoints[1].Text) {
		t.Fatal("standalone schema used the complete planning inventory instead of actual duties")
	}
	for _, values := range []string{`[]`, `[""]`, `["actual error","unrelated value"]`, `[2]`} {
		bad := strings.Replace(string(raw), `["Actual invalid input error restored"]`, values, 1)
		if _, err := DecodeResultWithPaths(input, "invalid", []byte(bad), "invalid-repair", []string{"input.go"}); err == nil {
			t.Fatalf("missing or unissued repair evidence accepted: %s", values)
		}
	}
	continued, err := DecodeResultWithPaths(input, "continue", []byte(`{"summary":"Inspected original failure","remainingWork":"Restore invalid input rejection"}`), "actual-progress", []string{})
	if err != nil || continued.Result.Validation.Status != "CONTINUE" || len(continued.Result.Validation.Development.RequirementResults) != 0 {
		t.Fatalf("unfinished repair claimed requirement completion: %v", err)
	}
	input.Validation.Repair.RequiredPoints = nil
	quality, err := DecodeResultWithPaths(input, "quality", []byte(`{"summary":"Simplified the confirmed redundant branch"}`), "actual-quality-repair", []string{"input.go"})
	if err != nil || len(quality.Result.Validation.Development.RequirementResults) != 0 {
		t.Fatalf("pure quality repair fabricated point completion: %v", err)
	}
}

func TestRepairMemberUsesOnlyItsBatchContributionEvidence(t *testing.T) {
	input := requirementInput("engine.repair.v6")
	input.RequirementPoints = input.RequirementPoints[1:]
	input.Validation.Repair = &protocol.RepairInput{}
	input.Validation.Development = &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "input", RequirementPoints: []string{"AC-002"}, DoD: []protocol.DevelopmentCriterion{{ID: "criterion", Description: "Invalid input rejection"}}}}
	raw := []byte(`{"summary":"Restored invalid input rejection","completion":{"dod":["Original invalid input failure restored"],"handoff":"Ready","requirements":["Actual invalid input error restored"]}}`)
	event, err := DecodeResultWithPaths(input, "result", raw, "actual-member-repair", []string{"input.go"})
	if err != nil {
		t.Fatal(err)
	}
	development := event.Result.Validation.Development
	if len(development.RequirementResults) != 0 || len(development.Completion.RequirementResults) != 1 || development.Completion.RequirementResults[0].PointID != "AC-002" {
		t.Fatalf("member duplicated evidence or broadened its contribution: %+v", development)
	}
	mixed := append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"requirements":["duplicate point proof"]}`)...)
	if _, err := DecodeResultWithPaths(input, "invalid", mixed, "invalid-member-repair", []string{"input.go"}); err == nil {
		t.Fatal("member accepted parallel direct and batch point evidence")
	}
}
