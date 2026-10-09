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
	for _, role := range []string{"engine.development.batch", "engine.repair"} {
		t.Run(role, func(t *testing.T) {
			input := protocol.WorkerInput{RunID: "run", Provider: "host", Handler: role, Pending: protocol.PendingAction{ActionID: "edit", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed"}, Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "batch"}}}}
			schema, err := ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			fields := schema["anyOf"].([]any)[2].(map[string]any)["properties"].(map[string]any)
			if len(fields) != 3 || fields["remainingWork"] == nil || fields["status"] != nil || fields["completion"] != nil {
				t.Fatalf("partial contract: %v", fields)
			}
			raw := []byte(`{"paths":[],"summary":"Mapped the required changes; no file delivered yet","remainingWork":"Implement the public command and its oracle"}`)
			original := string(raw)
			event, err := DecodeResult(input, "partial", raw, "original-native-output")
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
			if role == "engine.development.batch" {
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
			finalRaw := []byte(`{"paths":["app.txt"],"summary":"Completed the public command","completion":{"dod":{},"handoff":"Ready for approved checks"}}`)
			final, err := DecodeResult(input, "final", finalRaw, "original-final-output")
			if err != nil {
				t.Fatal(err)
			}
			if final.Result.SourceResult != prior.Digest || final.Result.Validation.Status != "PASS" || final.Result.Validation.Development.RemainingWork != "" {
				t.Fatalf("completion lost source or retained unfinished work: %#v", final.Result)
			}
			for _, invalid := range []string{
				`{"paths":[],"summary":"progress","remainingWork":" "}`,
				`{"paths":[],"summary":"progress","remainingWork":"pending","completion":{"dod":{},"handoff":"complete"}}`,
				`{"paths":[],"summary":"progress","remainingWork":"pending","affectedCases":["case"]}`,
				`{"paths":[],"summary":"progress","remainingWork":"pending","status":"CONTINUE"}`,
			} {
				if _, err := DecodeResult(input, "invalid", []byte(invalid), "raw"); err == nil {
					t.Fatalf("admitted false partial contract: %s", invalid)
				}
			}
		})
	}
	for _, role := range []string{"engine.qa.design", "engine.qa.execution", "engine.gate", "engine.repair.verify"} {
		input := protocol.WorkerInput{Handler: role, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}}}
		raw, _ := json.Marshal(DevelopmentContinuation{Paths: []string{}, Summary: "progress", RemainingWork: "pending"})
		if _, err := DecodeResult(input, "partial", raw, "original"); err == nil {
			t.Fatalf("admitted continuation for %s", role)
		}
	}
}
