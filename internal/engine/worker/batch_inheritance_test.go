package worker

import (
	"encoding/json"
	"formal-gates/internal/engine/protocol"
	"reflect"
	"testing"
)

func TestGranularityReusesConfirmedBatchAndNestedValues(t *testing.T) {
	original := protocol.DevelopmentBatch{ID: "original-batch", Owner: "slice-1", Artifacts: []string{"original.go"}, Subtasks: []protocol.DevelopmentSubtask{{ID: "original-first", Description: "first duty"}, {ID: "original-second", Description: "second duty"}}, Deliverable: "original delivery", PublicEntry: "./app", Interfaces: "original interface", StateBoundary: "original state boundary", Risk: "original risk", DoD: []protocol.DevelopmentCriterion{{ID: "original-check", Description: "original criterion"}, {ID: "original-other", Description: "another criterion"}}, Checks: []protocol.DevelopmentCheck{{ID: "original-build", Purpose: protocol.CheckFullBuild, Command: "go build ./..."}, {ID: "original-tests", Purpose: protocol.CheckFullTests, Command: "go test -timeout=0 ./..."}}, Rollback: "original rollback", Recovery: "original recovery", Handoff: "original handoff", ContextCost: "original context", Route: &protocol.RouteDecision{Mode: "full", Gates: []string{"quality"}}}
	source := &protocol.GranularityReview{Reason: "approved", VerificationTier: "FINAL", Batches: []protocol.DevelopmentBatch{original}}
	input := protocol.WorkerInput{Handler: "engine.readiness.plan.v2", ApprovedPlan: source}
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
