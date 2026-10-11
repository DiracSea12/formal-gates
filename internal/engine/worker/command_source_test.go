package worker

import (
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/requirement"
)

func TestReadinessCommandsSelectOriginalReadBytes(t *testing.T) {
	command := "./checks/project-check --label \"原命令 two words\" '' \\\n\t--next 'second line'\n"
	facts := []LocationFact{{Kind: "listing", Paths: []string{"README.md"}}, {Kind: "read", Paths: []string{"README.md"}, Content: []byte("Project check:\n" + command + "Other text\n")}}
	input := protocol.WorkerInput{Handler: "engine.readiness.plan.v4", RunID: "project", Provider: "codex", Pending: protocol.PendingAction{ActionID: "readiness", AttemptID: "issued"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "Confirmed behavior"}, Review: &protocol.WorkerReviewInput{Action: review.StartReadiness, RequirementRevision: "confirmed"}}
	value := Review{Findings: []Finding{}, GranularityReview: &Granularity{Reason: "One cohesive duty", VerificationTier: "FINAL", Batches: []Batch{{Artifacts: []Location{{Read: 2}}, Subtasks: []Subtask{{Description: "Implement public behavior"}}, DoD: []Criterion{{Description: "Public behavior holds"}}, Deliverable: "Confirmed behavior", PublicEntry: "Product entry", Interfaces: "Public interface", StateBoundary: "Project files", Risk: "Local change", Rollback: "Restore base", Recovery: "Resume duty", Handoff: "Ready for validation", ContextCost: "One duty", Checks: []Check{{Read: 2, From: 2, To: 3, Purpose: protocol.CheckEdit}, {Command: "project-build", Purpose: protocol.CheckFullBuild}, {Command: "project-tests", Purpose: protocol.CheckFullTests}}}}}}
	for _, key := range review.RequiredVerificationChecks() {
		value.Verification = append(value.Verification, "Fixture evidence for "+key)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	original, err := DecodeResultWithFacts(input, "result", raw, "verified-native-source", nil, facts)
	if err != nil {
		t.Fatal(err)
	}
	plan := original.Result.Review.GranularityReview
	if err := protocol.ValidateGranularity(plan); err != nil {
		t.Fatal(err)
	}
	if plan.Batches[0].Checks[0].Command != command || plan.Batches[0].Checks[0].Purpose != protocol.CheckEdit {
		t.Fatalf("original command bytes changed: %q", plan.Batches[0].Checks[0].Command)
	}
	stored, _ := json.Marshal(struct {
		Input protocol.WorkerInput
		Facts []LocationFact
	}{input, facts})
	var recovered struct {
		Input protocol.WorkerInput
		Facts []LocationFact
	}
	if err := json.Unmarshal(stored, &recovered); err != nil {
		t.Fatal(err)
	}
	replay, err := DecodeResultWithFacts(recovered.Input, "result", raw, "verified-native-source", nil, recovered.Facts)
	if err != nil || !reflect.DeepEqual(original, replay) {
		t.Fatalf("recovery changed original read selection: %v", err)
	}
	input.ApprovedPlan, input.ApprovedChecks = plan, plan.Batches[0].Checks
	for _, update := range []string{`{"batches":[{"reuse":1,"risk":"Changed boundary"}]}`, `{"batches":[{"reuse":1,"checks":[{"reuse":1},{"reuse":2},{"reuse":3}]}]}`} {
		var change Granularity
		if err := json.Unmarshal([]byte(update), &change); err != nil {
			t.Fatal(err)
		}
		selected, err := DecodeGranularity(input, &change, nil, facts, false)
		if err != nil || !reflect.DeepEqual(selected.Batches[0].Checks, plan.Batches[0].Checks) {
			t.Fatalf("partial approved plan lost command identities or bytes: %v", err)
		}
	}
	input.ApprovedChecks = nil
	var change Granularity
	if err := json.Unmarshal([]byte(`{"batches":[{"reuse":1,"checks":[{"read":2,"from":2,"to":3,"purpose":"EDIT"}]}]}`), &change); err != nil {
		t.Fatal(err)
	}
	selected, err := DecodeGranularity(input, &change, nil, facts, false)
	if err != nil || selected.Batches[0].Checks[0].Command != command {
		t.Fatalf("approved batch change skipped original command assembly: %v", err)
	}
}

func TestCommandSourceRangesAndModesRejectInvalidSelections(t *testing.T) {
	facts := []LocationFact{{Kind: "read", Paths: []string{"unavailable-original-path"}, Content: []byte("Heading\noriginal '' \"中文\"\nlast without newline")}, {Kind: "listing", Paths: []string{"README.md"}}, {Kind: "read"}}
	for _, sample := range []struct {
		name string
		raw  string
	}{
		{"mixed-reuse-source", `{"reuse":1,"read":1,"from":2,"to":2}`},
		{"mixed-reuse-empty-command", `{"reuse":1,"command":""}`},
		{"mixed-source-empty-command", `{"read":1,"from":2,"to":2,"command":""}`},
		{"mixed-command-zero-range", `{"command":"new","from":0}`},
		{"missing-range-end", `{"read":1,"from":2}`},
		{"null-range", `{"read":1,"from":2,"to":null}`},
		{"listing-source", `{"read":2,"from":1,"to":1}`},
		{"unpublished-source", `{"read":4,"from":1,"to":1}`},
		{"zero-source", `{"read":0,"from":1,"to":1}`},
		{"negative-source", `{"read":-1,"from":1,"to":1}`},
		{"zero-range", `{"read":1,"from":0,"to":2}`},
		{"reversed-range", `{"read":1,"from":3,"to":2}`},
		{"outside-range", `{"read":1,"from":2,"to":4}`},
		{"empty-read", `{"read":3,"from":1,"to":1}`},
	} {
		t.Run(sample.name, func(t *testing.T) {
			var value Granularity
			err := json.Unmarshal([]byte(`{"batches":[{"checks":[`+sample.raw+`]}]}`), &value)
			if err == nil {
				_, err = DecodeGranularity(protocol.WorkerInput{ApprovedChecks: []protocol.DevelopmentCheck{{ID: "approved", Command: "approved"}}}, &value, nil, facts, false)
			}
			if err == nil {
				t.Fatal("invalid source selection accepted")
			}
		})
	}
	for _, sample := range []struct {
		from, to int
		command  string
	}{{2, 2, "original '' \"中文\"\n"}, {3, 3, "last without newline"}, {2, 3, "original '' \"中文\"\nlast without newline"}} {
		value := &Granularity{Batches: []Batch{{Checks: []Check{{Read: 1, From: sample.from, To: sample.to}}}}}
		plan, err := DecodeGranularity(protocol.WorkerInput{}, value, nil, facts, false)
		if err != nil || plan.Batches[0].Checks[0].Command != sample.command {
			t.Fatalf("line endings changed or unavailable path was read: %v", err)
		}
	}
}

func TestRepairSourceCommandsKeepApprovedInheritanceAndLocalPolicy(t *testing.T) {
	approved := []protocol.DevelopmentCheck{{ID: "build", Purpose: protocol.CheckFullBuild, Command: "build\n"}, {ID: "tests", Purpose: protocol.CheckFullTests, Command: "tests\n"}, {ID: "edit", Purpose: protocol.CheckEdit, Command: "edit ''\n"}, {ID: "local", Command: "approved-local\n"}}
	facts := []LocationFact{{Kind: "read", Content: []byte("Local check:\nnew-local '' \"中文\"\n")}, {Kind: "read", Content: []byte(approved[3].Command)}}
	input := protocol.WorkerInput{Handler: "engine.repair.v5", Provider: "codex", Pending: protocol.PendingAction{ActionID: "repair"}, ApprovedChecks: approved, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}}}
	continued, err := DecodeResultWithFacts(input, "partial", []byte(`{"summary":"Mapped repair","remainingWork":"Implement the approved change"}`), "verified-partial-source", []string{}, facts)
	if err != nil || continued.Result.Validation.Status != "CONTINUE" {
		t.Fatalf("repair continuation failed: %v", err)
	}
	input.Validation.Repair.Result = &protocol.WorkerResult{ActionID: "repair", Digest: "accepted-partial", Outcome: protocol.OutcomePass, Validation: continued.Result.Validation}
	for _, completed := range []bool{false, true} {
		value := Repair{Summary: "Restored behavior", GranularityReview: &Granularity{Reason: "One cohesive repair", VerificationTier: "FINAL", Batches: []Batch{{DoD: []Criterion{{Description: "Behavior restored"}}, Checks: []Check{{Reuse: 4}, {Read: 1, From: 2, To: 2}}}}}}
		if completed {
			value.Completion = &Completion{DoD: []string{"Behavior restored"}, Handoff: "Ready for checks"}
		}
		raw, _ := json.Marshal(value)
		event, err := DecodeResultWithFacts(input, "result", raw, "verified-source", []string{"source.ext"}, facts)
		if err != nil {
			t.Fatal(err)
		}
		checks := event.Result.Validation.GranularityReview.Batches[0].Checks
		if len(checks) != 5 || !reflect.DeepEqual(checks[:4], approved) || checks[4].Command != "new-local '' \"中文\"\n" || checks[4].Purpose != "" {
			t.Fatalf("source selection changed repair inheritance: %+v", checks)
		}
		if event.Result.SourceResult != "accepted-partial" {
			t.Fatal("source selection lost the same repair continuation identity")
		}
		stored, _ := json.Marshal(struct {
			Input protocol.WorkerInput
			Facts []LocationFact
		}{input, facts})
		var recovered struct {
			Input protocol.WorkerInput
			Facts []LocationFact
		}
		if err := json.Unmarshal(stored, &recovered); err != nil {
			t.Fatal(err)
		}
		replay, err := DecodeResultWithFacts(recovered.Input, "result", raw, "verified-source", []string{"source.ext"}, recovered.Facts)
		if err != nil || !reflect.DeepEqual(event, replay) {
			t.Fatalf("repair recovery changed original selected bytes: %v", err)
		}
		for _, check := range []Check{{Read: 1, From: 2, To: 2, Purpose: protocol.CheckFullTests}, {Read: 1, From: 2, To: 2, Purpose: protocol.CheckEdit}, {Read: 2, From: 1, To: 1}, {Command: approved[3].Command}} {
			value.GranularityReview.Batches[0].Checks = []Check{check}
			raw, _ := json.Marshal(value)
			if _, err := DecodeResultWithFacts(input, "invalid", raw, "verified-source", []string{"source.ext"}, facts); err == nil {
				t.Fatalf("repair policy was relaxed for %+v", check)
			}
		}
	}
}
