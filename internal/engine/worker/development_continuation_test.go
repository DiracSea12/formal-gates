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
	for _, role := range []string{"engine.development.batch.v4", "engine.repair.v5"} {
		t.Run(role, func(t *testing.T) {
			input := protocol.WorkerInput{RunID: "run", Provider: "host", Handler: role, Pending: protocol.PendingAction{ActionID: "edit", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed"}, Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "batch"}}}}
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
			if role == "engine.development.batch.v4" {
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
	for _, role := range []string{"engine.qa.design.v3", "engine.qa.execution.v2", "engine.gate.v2", "engine.repair.verify.v2"} {
		input := protocol.WorkerInput{Handler: role, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}}}
		raw, _ := json.Marshal(DevelopmentContinuation{Summary: "progress", RemainingWork: "pending"})
		if _, err := DecodeResultWithPaths(input, "partial", raw, "original", []string{}); err == nil {
			t.Fatalf("admitted continuation for %s", role)
		}
	}
}
