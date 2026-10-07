package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
)

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
	for _, child := range listOf(schema["anyOf"]) {
		if err := codexPeerStrictSchema(child); err != nil {
			return err
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
	body, err := os.ReadFile("testdata/readiness-schema.json")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := decodeNativeJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	return schema.(map[string]any)
}

func TestCodexSchemaTransportsRoleContracts(t *testing.T) {
	batch := canaryLocalPlan(false).Batches[0]
	batch.DoD[0].ID = "confirmed-outcome"
	batch.Checks[0].ID = "confirmed-verification"
	schema := codexReadinessSchema(t)
	if codexPeerStrictSchema(schema) == nil {
		t.Fatal("original readiness schema must fail native strict admission")
	}
	if !valuesEqual(schema, codexPlain(t, canaryReviewSchema(true))) {
		t.Fatal("failure fixture differs from current domain schema")
	}
	for name, domain := range map[string]any{
		"readiness": schema, "review": canaryReviewSchema(false), "worker": WorkerSchema(),
		"blackbox-design": canaryDesignSchema(false), "whitebox-design": canaryDesignSchema(true),
		"case-review": canaryCaseReviewSchema(), "execution": canaryExecutionSchema(), "gate": canaryGateSchema(),
		"confirmed-development": canaryDevelopmentSchema(&protocol.DevelopmentInput{Batch: batch}),
		"autonomous-repair":     canaryDevelopmentSchema(nil),
	} {
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
			if name == "worker" && !valuesEqual(native, codexPlain(t, domain)) {
				t.Fatal("all-required schema changed")
			}
		})
	}
	closed := canaryDevelopmentSchema(&protocol.DevelopmentInput{Batch: batch})
	dod := getOf(getOf(closed, "properties"), "dod")
	if getOf(dod, "additionalProperties") != false || !valuesEqual(codexPlain(t, getOf(dod, "required")), []any{"confirmed-outcome"}) || len(objectOrEmpty(getOf(dod, "properties"))) != 1 {
		t.Fatalf("DoD schema not bound to confirmed batch: %v", dod)
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
		"missing-findings": {"status": "PASS", "summary": "ready", "check_ids": []any{}},
		"null-status":      {"status": nil, "summary": "ready", "findings": []any{}, "check_ids": []any{}},
		"wrong-type":       {"status": 7, "summary": "ready", "findings": []any{}, "check_ids": []any{}},
		"unknown-field":    {"status": "PASS", "summary": "ready", "findings": []any{}, "check_ids": []any{}, "extra": "reject"},
	} {
		t.Run(name, func(t *testing.T) {
			projected, err := codexDomainResult(codexPlain(t, WorkerSchema()), answer)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(projected)
			var worker WorkerResult
			err = StrictDecode(body, &worker)
			if err == nil {
				err = ValidateWorker("development", worker, nil)
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
