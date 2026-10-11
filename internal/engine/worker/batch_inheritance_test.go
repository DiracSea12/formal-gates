package worker

import (
	"bytes"
	"encoding/json"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
	"reflect"
	"strings"
	"testing"
)

func TestGranularityReusesConfirmedBatchAndNestedValues(t *testing.T) {
	original := protocol.DevelopmentBatch{ID: "original-batch", Owner: "slice-1", Artifacts: []string{"original.go"}, Subtasks: []protocol.DevelopmentSubtask{{ID: "original-first", Description: "first duty"}, {ID: "original-second", Description: "second duty"}}, Deliverable: "original delivery", PublicEntry: "./app", Interfaces: "original interface", StateBoundary: "original state boundary", Risk: "original risk", DoD: []protocol.DevelopmentCriterion{{ID: "original-check", Description: "original criterion"}, {ID: "original-other", Description: "another criterion"}}, Checks: []protocol.DevelopmentCheck{{ID: "original-build", Purpose: protocol.CheckFullBuild, Command: "go build ./..."}, {ID: "original-tests", Purpose: protocol.CheckFullTests, Command: "go test -timeout=0 ./..."}}, Rollback: "original rollback", Recovery: "original recovery", Handoff: "original handoff", ContextCost: "original context", Route: &protocol.RouteDecision{Mode: "full", Gates: []string{"quality"}}}
	source := &protocol.GranularityReview{Reason: "approved", VerificationTier: "FINAL", Batches: []protocol.DevelopmentBatch{original}}
	input := protocol.WorkerInput{Handler: "engine.readiness.plan.v4", ApprovedPlan: source}
	decode := func(raw string) *protocol.GranularityReview {
		t.Helper()
		var value Granularity
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		result, err := DecodeGranularity(input, &value, nil, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	before, _ := json.Marshal(source)
	result := decode(`{"reason":"same responsibilities with new risk","verificationTier":"FINAL","batches":[{"reuse":1,"risk":"new risk"}]}`)
	expected := original
	expected.Risk = "new risk"
	if !reflect.DeepEqual(result.Batches[0], expected) {
		t.Fatalf("omitted confirmed values changed: %#v", result.Batches[0])
	}
	result = decode(`{"reason":"changed one original duty","verificationTier":"FINAL","batches":[{"reuse":1,"subtasks":[{"reuse":2,"description":"changed second duty"}],"dod":[{"reuse":1,"description":"changed criterion"}]}]}`)
	expected = original
	expected.Subtasks = append([]protocol.DevelopmentSubtask{}, original.Subtasks...)
	expected.DoD = append([]protocol.DevelopmentCriterion{}, original.DoD...)
	expected.Subtasks[1].Description = "changed second duty"
	expected.DoD[0].Description = "changed criterion"
	if !reflect.DeepEqual(result.Batches[0], expected) {
		t.Fatalf("nested change lost original identities/content: %#v", result.Batches[0])
	}
	after, _ := json.Marshal(source)
	if string(before) != string(after) {
		t.Fatal("proposal mutated its confirmed source")
	}
	var invalid Granularity
	if err := json.Unmarshal([]byte(`{"reason":"unknown","verificationTier":"FINAL","batches":[{"reuse":2}]}`), &invalid); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeGranularity(input, &invalid, nil, nil, false); err == nil {
		t.Fatal("unknown batch reference accepted")
	}
}

func TestApprovedPlanRetainsOmittedTopLevelContract(t *testing.T) {
	original := nativePlanForInheritance()
	input := protocol.WorkerInput{RunID: "run", Provider: "codex", Handler: "engine.readiness.plan.v4", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed requirement"}, ApprovedPlan: original, Review: &protocol.WorkerReviewInput{Action: review.StartReadiness}}
	before, _ := json.Marshal(original)
	material, err := Render(input, prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "start-readiness"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(material.Prompt, "original first delivery") || !strings.Contains(material.Prompt, "original checkpoint reason") || !strings.Contains(material.Prompt, `"approvedPlan"`) {
		t.Fatal("plan role lacks original contract and positional source")
	}
	decode := func(raw string) *protocol.GranularityReview {
		t.Helper()
		var value Granularity
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		result, err := DecodeGranularity(input, &value, nil, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	result := decode(`{"batches":[{"reuse":1,"risk":"new risk"}]}`)
	expected := *original
	expected.Batches = append([]protocol.DevelopmentBatch{}, original.Batches...)
	expected.Batches[0].Risk = "new risk"
	if !reflect.DeepEqual(result, &expected) {
		t.Fatalf("omitted plan/other batch changed: %#v", result)
	}
	if err := protocol.ValidateGranularity(result); err != nil {
		t.Fatal(err)
	}
	result = decode(`{"verificationTier":"FINAL"}`)
	if result.VerificationTier != "FINAL" || len(result.CheckpointGroups) != 0 || result.CheckpointReason != "" || !reflect.DeepEqual(result.Batches, original.Batches) {
		t.Fatal("tier change lost batches or retained incompatible checkpoint fields")
	}
	if err := protocol.ValidateGranularity(result); err != nil {
		t.Fatal(err)
	}
	result = decode(`{"checkpointGroups":[],"checkpointReason":""}`)
	if len(result.CheckpointGroups) != 0 || result.CheckpointReason != "" {
		t.Fatal("explicit clear was not applied")
	}
	if err := protocol.ValidateGranularity(result); err == nil {
		t.Fatal("checkpoint proposal without its groups accepted")
	}
	result = decode(`{"reason":""}`)
	if err := protocol.ValidateGranularity(result); err == nil {
		t.Fatal("explicit empty reason was filled from old source")
	}
	after, _ := json.Marshal(original)
	if !bytes.Equal(before, after) {
		t.Fatal("proposal mutated its approved source")
	}
	input.ApprovedPlan = nil
	var first Granularity
	if err := json.Unmarshal([]byte(`{"batches":[]}`), &first); err != nil {
		t.Fatal(err)
	}
	result, err = DecodeGranularity(input, &first, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := protocol.ValidateGranularity(result); err == nil {
		t.Fatal("initial proposal missing new semantic contract accepted")
	}
}

func nativePlanForInheritance() *protocol.GranularityReview {
	batch := func(id, description string) protocol.DevelopmentBatch {
		return protocol.DevelopmentBatch{ID: id, Subtasks: []protocol.DevelopmentSubtask{{ID: id + "-task", Description: "original duty"}}, Deliverable: description, PublicEntry: "./app", Artifacts: []string{id + ".go"}, Interfaces: "original interface", StateBoundary: "original state", Risk: "original risk", DoD: []protocol.DevelopmentCriterion{{ID: id + "-condition", Description: "original criterion"}}, Checks: []protocol.DevelopmentCheck{{ID: id + "-build", Purpose: protocol.CheckFullBuild, Command: "go build ./..."}, {ID: id + "-tests", Purpose: protocol.CheckFullTests, Command: "go test -timeout=0 ./..."}}, Rollback: "original rollback", Recovery: "original recovery", Handoff: "original handoff", ContextCost: "original context"}
	}
	return &protocol.GranularityReview{Reason: "original reason", VerificationTier: "CHECKPOINT", Batches: []protocol.DevelopmentBatch{batch("first", "original first delivery"), batch("second", "original second delivery")}, CheckpointGroups: [][]string{{"first"}, {"second"}}, CheckpointReason: "original checkpoint reason"}
}
