package harness

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/worker"
)

func TestCodexNativeSchemaPreservesThreeClosedBodies(t *testing.T) {
	object := func(name string) map[string]any {
		return map[string]any{"type": "object", "additionalProperties": false, "required": []string{name}, "properties": map[string]any{name: map[string]any{"type": "string"}}}
	}
	domain := map[string]any{"anyOf": []any{object("complete"), object("remainingWork"), object("interruptionReason")}}
	native, err := codexOutputSchema(domain)
	if err != nil {
		t.Fatal(err)
	}
	if getOf(native, "type") != "object" || getOf(getOf(native, "properties"), "result") == nil {
		t.Fatal("native object root lost its closed bodies")
	}
	for _, name := range []string{"complete", "remainingWork", "interruptionReason"} {
		want := map[string]any{name: "original semantic value"}
		got, err := codexDomainResult(domain, map[string]any{"result": want})
		if err != nil || !valuesEqual(got, want) {
			t.Fatalf("closed body %s changed: %v %v", name, got, err)
		}
	}
	nullable := map[string]any{"anyOf": []any{object("value"), map[string]any{"type": "null"}}}
	if codexObjectUnion(nullable) {
		t.Fatal("nullable value was wrapped as an object union")
	}
	if got, err := codexDomainResult(nullable, nil); err != nil || got != nil {
		t.Fatal("native nullable value changed", got, err)
	}
}

// This peer checks native schema admission, not domain business results.
func codexPeerStrictSchema(value any) error {
	schema := objectOrEmpty(value)
	if schema["type"] == "object" {
		if schema["additionalProperties"] != false {
			return fmt.Errorf("object must disallow additional properties")
		}
		properties := objectOrEmpty(schema["properties"])
		required := listOf(schema["required"])
		if len(required) != len(properties) {
			return fmt.Errorf("required must cover every property")
		}
		for name, property := range properties {
			if !slices.Contains(required, any(name)) {
				return fmt.Errorf("required omits %s", name)
			}
			if err := codexPeerStrictSchema(property); err != nil {
				return err
			}
		}
	}
	if child, ok := schema["items"].(map[string]any); ok {
		if err := codexPeerStrictSchema(child); err != nil {
			return err
		}
	}
	for _, key := range []string{"anyOf", "prefixItems"} {
		for _, child := range listOf(schema[key]) {
			if err := codexPeerStrictSchema(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func codexPlain(t *testing.T, value any) any {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := decodeNativeJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	return plain
}

func codexReadinessSchema(t *testing.T) map[string]any {
	t.Helper()
	input := protocol.WorkerInput{Handler: "engine.readiness.plan", Review: &protocol.WorkerReviewInput{Action: review.StartReadiness}}
	schema, err := worker.ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	return codexPlain(t, schema).(map[string]any)
}

func TestCodexSchemaTransportsRoleContracts(t *testing.T) {
	batch := canaryLocalPlan(false).Batches[0]
	batch.DoD[0].ID = "confirmed-outcome"
	batch.Checks[0].ID = "confirmed-verification"
	schema := codexReadinessSchema(t)
	if codexPeerStrictSchema(schema) == nil {
		t.Fatal("original readiness schema must fail native strict admission")
	}
	roles := map[string]protocol.WorkerInput{
		"review":                {Handler: "engine.review.worker", Review: &protocol.WorkerReviewInput{Action: review.ProductReview}},
		"blackbox-design":       {Handler: "engine.qa.design", Validation: &protocol.ValidationInput{}},
		"whitebox-design":       {Handler: "engine.qa.design", Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewWhitebox}}}},
		"case-review":           {Handler: "engine.qa.review.rationale", Validation: &protocol.ValidationInput{}},
		"execution":             {Handler: "engine.qa.execution", Validation: &protocol.ValidationInput{}},
		"gate":                  {Handler: "engine.gate", Validation: &protocol.ValidationInput{}},
		"confirmed-development": {Handler: "engine.development.batch", Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: batch}}},
		"autonomous-repair":     {Handler: "engine.repair", Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}}},
	}
	domains := map[string]any{"readiness": schema, "worker": WorkerSchema()}
	for name, input := range roles {
		body, err := worker.ResultSchema(input)
		if err != nil {
			t.Fatal(err)
		}
		domains[name] = body
	}
	for name, domain := range domains {
		t.Run(name, func(t *testing.T) {
			before, _ := json.Marshal(domain)
			native, err := codexOutputSchema(domain)
			if err != nil {
				t.Fatal(err)
			}
			if err := codexPeerStrictSchema(codexPlain(t, native)); err != nil {
				t.Fatalf("native schema rejected: %v", err)
			}
			after, _ := json.Marshal(domain)
			if string(before) != string(after) {
				t.Fatal("domain schema mutated")
			}

		})
	}
	closed, err := worker.ResultSchema(roles["confirmed-development"])
	if err != nil {
		t.Fatal(err)
	}
	body := closed["anyOf"].([]any)[0].(map[string]any)
	dod := body["properties"].(map[string]any)["completion"].(map[string]any)["properties"].(map[string]any)["dod"].(map[string]any)
	if dod["minItems"] != len(batch.DoD) || dod["items"] != false {
		t.Fatalf("DoD schema not bound to confirmed criteria: %v", dod)
	}
}

func TestCodexDomainProjectionPreservesValidationInputs(t *testing.T) {
	schema := codexPlain(t, map[string]any{"type": "object", "required": []string{"required"}, "properties": map[string]any{
		"required": map[string]any{"type": "string"}, "optional": map[string]any{"type": "string"}, "nullable": map[string]any{"type": []string{"string", "null"}},
	}})
	raw := map[string]any{"required": nil, "optional": nil, "nullable": nil, "unknown": "retained"}
	result, err := codexDomainResult(schema, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, map[string]any{"required": nil, "nullable": nil, "unknown": "retained"}) {
		t.Fatalf("projection rewrote validation inputs: %v", result)
	}
	for name, answer := range map[string]map[string]any{
		"missing-findings": {"summary": "ready", "check_reviews": []any{}},
		"null-summary":     {"summary": nil, "findings": []any{}, "check_reviews": []any{}},
		"wrong-type":       {"summary": 7, "findings": []any{}, "check_reviews": []any{}},
		"unknown-field":    {"summary": "ready", "findings": []any{}, "check_reviews": []any{}, "extra": "reject"},
	} {
		t.Run(name, func(t *testing.T) {
			projected, err := codexDomainResult(codexPlain(t, WorkerSchema()), map[string]any{"result": answer})
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(projected)
			var worker WorkerJudgment
			err = StrictDecode(body, &worker)
			if err == nil {
				_, err = workerJudgmentResult("development", worker, nil)
			}
			if err == nil {
				t.Fatal("existing worker validator accepted invalid domain result")
			}
		})
	}
}

func TestCodexDictionaryProjectionRejectsAmbiguousEntries(t *testing.T) {
	schema := map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}
	for _, rows := range []any{
		[]any{map[string]any{"key": "same", "value": "first"}, map[string]any{"key": "same", "value": "second"}},
		[]any{map[string]any{"key": "missing"}}, []any{map[string]any{"key": 4, "value": "bad"}},
		[]any{map[string]any{"key": "extra", "value": "bad", "unknown": "reject"}},
	} {
		if _, err := codexDomainResult(schema, rows); err == nil {
			t.Fatalf("ambiguous dictionary accepted: %v", rows)
		}
	}
	result, err := codexDomainResult(schema, []any{map[string]any{"key": "new-plan-id", "value": "actual completion"}})
	if err != nil || !valuesEqual(result, map[string]any{"new-plan-id": "actual completion"}) {
		t.Fatalf("dynamic dictionary = %v %v", result, err)
	}
	request := map[string]any{"prompt": "original prompt", "schema": WorkerSchema()}
	params, err := codexTurnParameters(request, "thread")
	if err != nil || !strings.Contains(textOf(params), "original prompt") || getOf(listOf(params["input"])[0], "text") != "original prompt" {
		t.Fatalf("all-required request changed: %v %v", params, err)
	}
}

func TestCodexNullableDictionaryTransport(t *testing.T) {
	dictionary := map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}
	schema := map[string]any{"anyOf": []any{dictionary, map[string]any{"type": "null"}}}
	native, err := codexOutputSchema(schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := codexPeerStrictSchema(native); err != nil {
		t.Fatal(err)
	}
	if getOf(listOf(getOf(native, "anyOf"))[0], "type") != "array" {
		t.Fatalf("dictionary union branch not translated: %v", native)
	}
	result, err := codexDomainResult(schema, []any{map[string]any{"key": "dynamic", "value": "evidence"}})
	if err != nil || !valuesEqual(result, map[string]any{"dynamic": "evidence"}) {
		t.Fatalf("nullable dictionary result=%v %v", result, err)
	}
	result, err = codexDomainResult(schema, nil)
	if err != nil || result != nil {
		t.Fatalf("explicit nullable value changed: %v %v", result, err)
	}
}
