//go:build !windows

package harness

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"formal-gates/internal/engine/protocol"
)

// codexPeerAnswer spells a domain fixture in the native transport schema.
func codexPeerAnswer(schema, value any) any {
	if value == nil {
		return nil
	}
	contract := objectOrEmpty(schema)
	if codexOpen, ok := codexOpenMap(contract); ok {
		values := objectOrEmpty(value)
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		rows := make([]any, 0, len(keys))
		for _, key := range keys {
			rows = append(rows, map[string]any{"key": key, "value": codexPeerAnswer(codexOpen, values[key])})
		}
		return rows
	}
	switch input := value.(type) {
	case map[string]any:
		properties := objectOrEmpty(contract["properties"])
		output := make(map[string]any, len(properties))
		for key, property := range properties {
			output[key] = codexPeerAnswer(property, input[key])
		}
		return output
	case []any:
		rows := make([]any, len(input))
		for i, item := range input {
			rows[i] = codexPeerAnswer(contract["items"], item)
		}
		return rows
	}
	return value
}

func TestCodexNativeStrictSchemaProjectionAndRecovery(t *testing.T) {
	plan := canaryLocalPlan(false)
	plan.Batches[0].DoD[0].ID = "chosen-in-this-answer"
	plan.Batches[0].Checks[0].ID = "chosen-check-in-this-answer"
	batch := protocol.DevelopmentInput{Batch: plan.Batches[0]}
	completion := map[string]any{"paths": []string{"command.go"}, "summary": "implemented declared behavior", "build": []string{"build exit=0"}, "tests": []string{"check exit=0"}, "dod": map[string]string{"chosen-in-this-answer": "complete"}, "checks": map[string]string{"chosen-check-in-this-answer": "passed"}, "handoff": "ready for native integration"}
	review := map[string]any{"status": "PASS", "findings": []any{}, "verification": map[string]any{"checks": []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"}, "evidence": "read exact current materials"}, "plan": plan}
	repair := map[string]any{}
	for key, value := range completion {
		repair[key] = value
	}
	repair["granularityReview"] = plan
	repairSchema := canaryDevelopmentSchema(nil)
	repairSchema["required"] = []string{}
	repairSchema["properties"].(map[string]any)["granularityReview"] = getOf(getOf(canaryReviewSchema(true), "properties"), "plan")
	for _, sample := range []struct {
		name   string
		schema any
		domain map[string]any
	}{
		{"readiness", codexReadinessSchema(t), review},
		{"confirmed-batch", canaryDevelopmentSchema(&batch), completion},
		{"autonomous-repair", repairSchema, repair},
		{"all-required-worker", WorkerSchema(), map[string]any{"status": "PASS", "summary": "ready", "findings": []any{}, "check_reviews": map[string]any{}}},
	} {
		t.Run(sample.name, func(t *testing.T) {
			s := newCodexScenario(t)
			s.request["schema"] = sample.schema
			s.peer.StrictSchema = true
			domain := codexPlain(t, sample.domain).(map[string]any)
			schema := codexPlain(t, sample.schema)
			s.peer.Answer = codexPeerAnswer(schema, domain).(map[string]any)
			before, _ := json.Marshal(sample.schema)
			source, err := s.execute(t)
			if err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(sample.schema)
			if !bytes.Equal(before, after) {
				t.Fatal("request domain schema mutated")
			}
			got, err := readJSONFile(filepath.Join(s.directory, "answer.json"))
			if err != nil || !valuesEqual(got, domain) {
				t.Fatalf("domain result = %v %v", got, err)
			}
			expectedRaw, _ := json.Marshal(s.peer.Answer)
			raw, err := os.ReadFile(filepath.Join(s.directory, "answer.raw.txt"))
			if err != nil || !bytes.Equal(raw, expectedRaw) {
				t.Fatal("native raw answer was rewritten")
			}
			if err := codexVerifySource(s.directory, source); err != nil {
				t.Fatal(err)
			}
			rpc, err := os.ReadFile(filepath.Join(s.config, "rpc.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			recovered, err := codexReconcile(s.directory, map[string]any{"exit_code": 0})
			if err != nil || !valuesEqual(recovered, source) {
				t.Fatalf("recovered source = %v %v", recovered, err)
			}
			repeated, _ := os.ReadFile(filepath.Join(s.config, "rpc.jsonl"))
			repeatedRaw, _ := os.ReadFile(filepath.Join(s.directory, "answer.raw.txt"))
			if !bytes.Equal(rpc, repeated) || !bytes.Equal(raw, repeatedRaw) {
				t.Fatal("recovery replayed work or changed raw evidence")
			}
			if getOf(source, "usage_evidence") == nil {
				t.Fatal("native usage evidence missing")
			}
			switch sample.name {
			case "readiness":
				var answer canaryReviewAnswer
				if err := decodeCanaryAnswer(got.(map[string]any), &answer); err != nil {
					t.Fatal(err)
				}
				if err := canaryReviewShape(&answer, true); err != nil {
					t.Fatal(err)
				}
			case "autonomous-repair":
				var answer canaryDevelopmentAnswer
				if err := decodeCanaryAnswer(got.(map[string]any), &answer); err != nil {
					t.Fatal(err)
				}
				if err := protocol.ValidateRepairPlan(answer.Plan, true); err != nil {
					t.Fatal(err)
				}
				if err := canaryDevelopmentShape(&answer, true); err != nil {
					t.Fatal(err)
				}
				if answer.DoD[answer.Plan.Batches[0].DoD[0].ID] != "complete" {
					t.Fatal("new plan's completion ID was lost")
				}
			}
		})
	}
}

func TestCodexNativeDuplicateDictionaryKeepsRawEvidence(t *testing.T) {
	s := newCodexScenario(t)
	s.request["schema"] = map[string]any{"type": "object", "additionalProperties": false, "required": []string{"dod"}, "properties": map[string]any{"dod": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}}}
	s.peer.StrictSchema = true
	s.peer.Answer = map[string]any{"dod": []any{map[string]any{"key": "same", "value": "first"}, map[string]any{"key": "same", "value": "second"}}}
	if _, err := s.execute(t); err == nil {
		t.Fatal("duplicate keys accepted")
	}
	before, err := os.ReadFile(filepath.Join(s.directory, "answer.raw.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codexReconcile(s.directory, map[string]any{"exit_code": 0}); err == nil {
		t.Fatal("recovery accepted duplicate keys")
	}
	after, _ := os.ReadFile(filepath.Join(s.directory, "answer.raw.txt"))
	if !bytes.Equal(before, after) {
		t.Fatal("refusal rewrote raw answer")
	}
}
