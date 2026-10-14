package worker

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
)

func TestDevelopmentContinuationPreservesSemanticProgress(t *testing.T) {
	for _, role := range []string{"engine.development.batch.v6", "engine.repair.v7"} {
		t.Run(role, func(t *testing.T) {
			input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, RunID: "run", Provider: "host", Handler: role, Pending: protocol.PendingAction{ActionID: "edit", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed"}, Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "batch"}}}}
			schema, err := ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			fields := schema["anyOf"].([]any)[2].(map[string]any)["properties"].(map[string]any)
			if len(fields) != 2 || fields["remainingWork"] == nil || fields["status"] != nil || fields["completion"] != nil {
				t.Fatalf("partial contract: %v", fields)
			}
			raw := []byte(`{"summary":"Mapped the required changes; no file delivered yet","remainingWork":"Implement the public command and its oracle"}`)
			original := string(raw)
			event, err := DecodeResultWithPaths(input, "partial", raw, "original-native-output", []string{})
			if err != nil {
				t.Fatal(err)
			}
			result := event.Result
			if result.Outcome != protocol.OutcomePass || result.FailureClass != "" || result.Validation.Status != "CONTINUE" || result.Validation.Development.Completion != nil || result.Validation.Development.RemainingWork != "Implement the public command and its oracle" || result.PayloadDigest != "original-native-output" || string(raw) != original {
				t.Fatalf("partial semantics/evidence changed: %#v", result)
			}
			catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "development-worker"}}}
			before, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			prior := &protocol.WorkerResult{ActionID: "edit", Digest: "accepted-partial", Outcome: protocol.OutcomePass, Validation: result.Validation}
			if role == "engine.development.batch.v6" {
				input.Validation.Development.Result = prior
			} else {
				input.Validation.Repair = &protocol.RepairInput{Result: prior}
			}
			after, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			if reflect.DeepEqual(before, after) || !strings.Contains(after.Prompt, result.Validation.Development.RemainingWork) || before.Input.Pending != after.Input.Pending {
				t.Fatal("progress did not change material while retaining issued action/attempt")
			}
			finalRaw := []byte(`{"summary":"Completed the public command","completion":{"dod":[],"handoff":"Ready for approved checks"}}`)
			final, err := DecodeResultWithPaths(input, "final", finalRaw, "original-final-output", []string{})
			if err != nil {
				t.Fatal(err)
			}
			if final.Result.SourceResult != prior.Digest || final.Result.Validation.Status != "PASS" || final.Result.Validation.Development.RemainingWork != "" {
				t.Fatalf("completion lost source or retained unfinished work: %#v", final.Result)
			}
			for _, invalid := range []string{
				`{"summary":"progress","remainingWork":" "}`,
				`{"summary":"progress","remainingWork":"pending","completion":{"dod":[],"handoff":"complete"}}`,
				`{"summary":"progress","remainingWork":"pending","affectedCases":["case"]}`,
				`{"summary":"progress","remainingWork":"pending","status":"CONTINUE"}`,
			} {
				if _, err := DecodeResultWithPaths(input, "invalid", []byte(invalid), "raw", []string{}); err == nil {
					t.Fatalf("admitted false partial contract: %s", invalid)
				}
			}
		})
	}
	for _, role := range []string{"engine.qa.design.v6", "engine.qa.execution.v4", "engine.gate.v3", "engine.repair.verify.v2"} {
		input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: role, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}}}
		raw, _ := json.Marshal(DevelopmentContinuation{Summary: "progress", RemainingWork: "pending"})
		if _, err := DecodeResultWithPaths(input, "partial", raw, "original", []string{}); err == nil {
			t.Fatalf("admitted continuation for %s", role)
		}
	}
}

func TestReadOnlyDevelopmentPreparationRetainsOriginalDuty(t *testing.T) {
	input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, RunID: "run", Provider: "host", Handler: "engine.development.batch.v6", Pending: protocol.PendingAction{ActionID: "edit", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed"}, Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "batch", Checks: []protocol.DevelopmentCheck{{ID: "edit", Purpose: protocol.CheckEdit, Goal: "Check each mutation"}, {ID: "build", Purpose: protocol.CheckFullBuild, Goal: "Build the project"}, {ID: "tests", Purpose: protocol.CheckFullTests, Goal: "Test public behavior"}}}}}}
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "development-worker"}}}
	material, err := Render(input, catalog)
	if err != nil || !material.ReadOnly {
		t.Fatal("missing EDIT calls did not retain the read-only preparation duty", err)
	}
	commands := []string{"project-edit", "project-build", "project-tests"}
	raw, err := json.Marshal(DevelopmentContinuation{Commands: commands, Summary: "Prepared declared calls without changing files", RemainingWork: "Implement public behavior and run the approved calls"})
	if err != nil {
		t.Fatal(err)
	}
	event, err := DecodeResultWithPaths(input, "prepared", raw, "original-preparation-source", nil)
	if err != nil {
		t.Fatal(err)
	}
	result := event.Result
	if result.ActionID != input.Pending.ActionID || result.PayloadDigest != "original-preparation-source" || result.Validation.Status != "CONTINUE" || result.Validation.Development.Paths != nil || result.Validation.Development.Completion != nil || !reflect.DeepEqual(result.Validation.Development.Commands, commands) {
		t.Fatal("read-only preparation invented file facts or changed its original duty", result)
	}
	stored, err := json.Marshal(material)
	if err != nil {
		t.Fatal(err)
	}
	var restored Material
	if err := json.Unmarshal(stored, &restored); err != nil {
		t.Fatal(err)
	}
	replay, err := DecodeResultWithPaths(restored.Input, "prepared", raw, "original-preparation-source", nil)
	if err != nil || !reflect.DeepEqual(event, replay) {
		t.Fatal("persisted preparation changed original command or result evidence", err)
	}
	completion := []byte(`{"summary":"done","completion":{"dod":[],"handoff":"ready"}}`)
	if _, err := DecodeResultWithPaths(input, "invalid-completion", completion, "native-source", nil); err == nil {
		t.Fatal("read-only preparation claimed writable completion")
	}
	input.Validation.Development.Result = &protocol.WorkerResult{ActionID: result.ActionID, Digest: "accepted-preparation", Outcome: result.Outcome, Validation: result.Validation}
	continued, err := Render(input, catalog)
	if err != nil || continued.ReadOnly || continued.Input.Pending != material.Input.Pending || !strings.Contains(continued.Prompt, result.Validation.Development.RemainingWork) {
		t.Fatal("prepared calls did not continue the original writable action and attempt", err)
	}
	continuation := []byte(`{"summary":"implemented one function","remainingWork":"implement the other function"}`)
	if _, err := DecodeResultWithPaths(input, "missing-paths", continuation, "native-write-source", nil); err == nil {
		t.Fatal("writable continuation omitted original terminal path facts")
	}
	writable, err := DecodeResultWithPaths(input, "written", continuation, "native-write-source", []string{"app.py"})
	if err != nil || writable.Result.SourceResult != "accepted-preparation" || !reflect.DeepEqual(writable.Result.Validation.Development.Paths, []string{"app.py"}) || !reflect.DeepEqual(writable.Result.Validation.Development.Commands, commands) {
		t.Fatal("writable continuation lost its prepared calls or original file facts", err)
	}
}

func TestWritableDevelopmentRequiresTerminalPathFacts(t *testing.T) {
	for _, handler := range []string{"engine.development.batch.v6", "engine.repair.v7"} {
		for _, purpose := range []string{protocol.CheckFullTests, protocol.CheckEdit} {
			input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, RunID: "run", Provider: "host", Handler: handler, Pending: protocol.PendingAction{ActionID: "write", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed"}, Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "batch", Checks: []protocol.DevelopmentCheck{{ID: "check", Purpose: purpose, Goal: "Declared check"}}}}}}
			if handler == "engine.development.batch.v6" && purpose == protocol.CheckEdit {
				input.Validation.Development.Batch.Checks[0].Command = "project-edit"
				input.Validation.Development.Batch.Checks[0].Goal = ""
			}
			bodies := []map[string]any{{"summary": "progress", "remainingWork": "pending"}, {"summary": "done", "completion": map[string]any{"dod": []string{}, "handoff": "ready"}}}
			if handler == "engine.development.batch.v6" && purpose == protocol.CheckFullTests {
				bodies[1]["commands"] = []string{"project-tests"}
			}
			for _, body := range bodies {
				raw, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := DecodeResultWithPaths(input, "missing-paths", raw, "original-native-source", nil); err == nil || !strings.Contains(err.Error(), "original native terminal path facts") {
					t.Fatalf("writable %s with %s accepted absent terminal path facts: %v", handler, purpose, err)
				}
			}
		}
	}
}
