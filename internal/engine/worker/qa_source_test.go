package worker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

// Original workspace reads supply exact invocation values at every QA ingress.
func qaRecipeFacts(t *testing.T) []LocationFact {
	t.Helper()
	contents := []string{"Recipe:\nprintf '%s\\n' '中文'\nprintf '%s\\n' '\"quoted\"'\n", "public-tool", "two words \"quoted\" 中文", "Input:\n first line\nsecond 中文\n", "outputs/中文 raw"}
	var facts []LocationFact
	for index, content := range contents {
		name := fmt.Sprintf("source-%d.txt", index+1)
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		facts = append(facts, LocationFact{Kind: "read", Paths: []string{name}, Content: raw})
	}
	return facts
}

const qaSourceOperations = `[{"kind":"SHELL","command":{"read":1,"from":2,"to":3},"stdin":{"read":4,"from":2,"to":3}},{"kind":"ARGV","argv":[{"read":2,"from":1,"to":1},"",{"read":3,"from":1,"to":1}],"stdin":{"read":4,"from":2,"to":3}}]`

func qaExpectedOperations() []coverage.Operation {
	stdin := " first line\nsecond 中文\n"
	return []coverage.Operation{{Kind: coverage.OperationShell, Command: "printf '%s\\n' '中文'\nprintf '%s\\n' '\"quoted\"'\n", Stdin: stdin}, {Kind: coverage.OperationArgv, Argv: []string{"public-tool", "", "two words \"quoted\" 中文"}, Stdin: stdin}}
}

func TestQAInitialRecipesSelectOriginalReadValues(t *testing.T) {
	facts := qaRecipeFacts(t)
	for _, mode := range []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox, coverage.ReviewMerge} {
		for _, shared := range []bool{false, true} {
			t.Run(string(mode)+map[bool]string{false: "-case", true: "-shared"}[shared], func(t *testing.T) {
				input := semanticInput()
				input.Validation.Manifest.Binding.ReviewKind = mode
				input.Validation.Manifest.Cases = nil
				input.Validation.Manifest.SharedSteps = nil
				raw := []byte(`{"cases":[{"scenario":"SUCCESS","oracle":"exact original output","operations":` + qaSourceOperations + `}],"coverage":[[1],[1]]}`)
				if shared {
					raw = []byte(`{"cases":[{"scenario":"SUCCESS","oracle":"exact original output","sharedSteps":[1]}],"sharedSteps":[{"procedure":"prepare original recipe","artifacts":[{"read":5,"from":1,"to":1}],"operations":` + qaSourceOperations + `}],"coverage":[[1],[1]]}`)
				}
				assertQARecipeReplay(t, input, raw, facts, func(event protocol.Event) {
					manifest := event.Result.Validation.Manifest
					got := manifest.Cases[0].Operations
					if shared {
						got = manifest.SharedSteps[0].Operations
						if !reflect.DeepEqual(manifest.SharedSteps[0].Artifacts, []string{"outputs/中文 raw"}) {
							t.Fatal("artifact bytes changed", manifest.SharedSteps)
						}
					}
					if !reflect.DeepEqual(got, qaExpectedOperations()) {
						t.Fatalf("read bytes changed: %#v", got)
					}
				})
			})
		}
	}
	input := semanticInput()
	input.Handler = "engine.qa.execution.v3"
	input.Validation.CommandCases = []string{"approved-original"}
	raw := []byte(`{"commands":[` + qaSourceOperations + `]}`)
	assertQARecipeReplay(t, input, raw, facts, func(event protocol.Event) {
		commands := event.Result.Validation.QACommands
		if len(commands) != 1 || commands[0].CaseID != "approved-original" || !event.Result.Validation.Checkpoint || !reflect.DeepEqual(commands[0].Operations, qaExpectedOperations()) {
			t.Fatal("command binding lost issued identity, bytes or checkpoint", commands)
		}
	})
	for _, raw := range []string{`{"commands":[]}`, `{"commands":[[],[]]}`, `{"commands":[[{"reuse":1}]]}`, `{"commands":[[{"kind":"SHELL","command":{"read":1,"from":2,"to":3},"caseId":"other"}]]}`} {
		if _, err := DecodeResultWithFacts(input, "invalid", []byte(raw), "original-output", []string{}, facts); err == nil {
			t.Fatal("unissued command binding admitted", raw)
		}
	}
}

func assertQARecipeReplay(t *testing.T, input protocol.WorkerInput, raw []byte, facts []LocationFact, check func(protocol.Event)) {
	t.Helper()
	before, _ := json.Marshal(input)
	original := bytes.Clone(raw)
	if err := ValidateSemanticBody(input, raw, facts); err != nil {
		t.Fatal("original source admission rejected", err)
	}
	event, err := DecodeResultWithFacts(input, "result", raw, "original-output", []string{}, facts)
	if err != nil {
		t.Fatal(err)
	}
	check(event)
	for _, continued := range []bool{false, true} {
		inputBytes, _ := json.Marshal(input)
		factBytes, _ := json.Marshal(facts)
		var restored protocol.WorkerInput
		var restoredFacts []LocationFact
		if err := json.Unmarshal(inputBytes, &restored); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(factBytes, &restoredFacts); err != nil {
			t.Fatal(err)
		}
		if continued {
			restored.Validation.Interruption = &protocol.RecoveryRecord{}
		}
		repeated, err := DecodeResultWithFacts(restored, "result", original, "original-output", []string{}, restoredFacts)
		if err != nil || !reflect.DeepEqual(event, repeated) {
			t.Fatal("repeat/continued serialized source assembly changed", err)
		}
	}
	after, _ := json.Marshal(input)
	if !bytes.Equal(before, after) || !bytes.Equal(original, raw) {
		t.Fatal("assembly mutated issued input or source semantics")
	}
}

func TestQARecipeRevisionSharesOriginalValueAssembly(t *testing.T) {
	facts := qaRecipeFacts(t)
	original := []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{"original-tool", "", "retained 中文"}, Stdin: "old argv input"}, {Kind: coverage.OperationShell, Command: "original shell", Stdin: "old shell input"}}
	for _, affected := range []bool{false, true} {
		input := retainedDesignInput()
		input.Validation.Manifest.Cases[0].Operations = original
		input.Validation.Manifest.SharedSteps[0].Operations = original
		input.Validation.Manifest.SharedSteps[0].Artifacts = []string{"old/path", "retained/path"}
		operations := `[{"reuse":2,"command":{"read":1,"from":2,"to":3},"stdin":{"read":4,"from":2,"to":3}},{"reuse":1,"argv":[{"reuse":1},{"reuse":2},{"read":3,"from":1,"to":1},{"reuse":3}]}]`
		artifacts := `[{"reuse":2},{"read":5,"from":1,"to":1},{"reuse":2}]`
		raw := []byte(`{"cases":[{"case":1,"operations":` + operations + `}],"sharedSteps":[{"step":1,"operations":` + operations + `,"artifacts":` + artifacts + `}],"coverage":[null,null]}`)
		if affected {
			input.Validation.AffectedCases = []string{"keep"}
			raw = []byte(`{"cases":[{"operations":` + operations + `}],"sharedSteps":[{"operations":` + operations + `,"artifacts":` + artifacts + `}]}`)
		}
		assertQARecipeReplay(t, input, raw, facts, func(event protocol.Event) {
			want := []coverage.Operation{qaExpectedOperations()[0], {Kind: coverage.OperationArgv, Argv: []string{"original-tool", "", "two words \"quoted\" 中文", "retained 中文"}, Stdin: "old argv input"}}
			manifest := event.Result.Validation.Manifest
			if !reflect.DeepEqual(manifest.Cases[0].Operations, want) || !reflect.DeepEqual(manifest.SharedSteps[0].Operations, want) || !reflect.DeepEqual(manifest.SharedSteps[0].Artifacts, []string{"retained/path", "outputs/中文 raw", "retained/path"}) {
				t.Fatal("revision changed inherited bytes/order or source text", manifest)
			}
		})
	}
}

func TestQARecipeSourceRejectsInvalidShapeAndUnavailableOriginal(t *testing.T) {
	input := semanticInput()
	input.Validation.Manifest.Cases = nil
	input.Validation.Manifest.SharedSteps = nil
	facts := qaRecipeFacts(t)
	rawFor := func(value string) []byte {
		return []byte(`{"cases":[{"scenario":"SUCCESS","oracle":"source bytes","operations":[{"kind":"SHELL","command":` + value + `}]}],"coverage":[[1],[1]]}`)
	}
	for _, value := range []string{`{}`, `{"read":0,"from":1,"to":1}`, `{"read":1,"from":0,"to":1}`, `{"read":1,"from":3,"to":2}`, `{"read":1,"from":1}`, `{"read":1,"from":1,"to":null}`, `{"read":1,"from":1,"to":1,"reuse":1}`, `{"read":1,"from":1,"to":1,"value":"copy"}`, `{"reuse":1}`} {
		if err := ValidateSemanticBody(input, rawFor(value), facts); err == nil {
			t.Fatal("invalid source shape admitted", value)
		}
	}
	for _, sample := range []struct {
		name   string
		facts  []LocationFact
		source string
	}{
		{"missing", nil, `{"read":1,"from":2,"to":3}`},
		{"unpublished", facts, `{"read":99,"from":1,"to":1}`},
		{"outside-range", facts, `{"read":1,"from":2,"to":99}`},
		{"listing", []LocationFact{{Kind: "listing", Content: []byte("printf wrong")}}, `{"read":1,"from":1,"to":1}`},
		{"empty", []LocationFact{{Kind: "read"}}, `{"read":1,"from":1,"to":1}`},
		{"blank-command", []LocationFact{{Kind: "read", Content: []byte(" \n")}}, `{"read":1,"from":1,"to":1}`},
	} {
		t.Run(sample.name, func(t *testing.T) {
			raw := rawFor(sample.source)
			if err := ValidateSemanticBody(input, raw, sample.facts); err == nil {
				t.Fatal("source admission accepted unavailable or invalid original bytes")
			}
			if _, err := DecodeResultWithFacts(input, "invalid", raw, "original-output", []string{}, sample.facts); err == nil {
				t.Fatal("missing/invalid original source admitted")
			}
		})
	}
	for _, operations := range []string{`[{"kind":"ARGV","argv":["tool"],"command":{"read":1,"from":2,"to":3}}]`, `[{"kind":"SHELL","argv":["tool"],"command":{"read":1,"from":2,"to":3}}]`, `[{"argv":[{"read":2,"from":1,"to":1}]}]`} {
		raw := []byte(`{"cases":[{"scenario":"SUCCESS","oracle":"source bytes","operations":` + operations + `}],"coverage":[[1],[1]]}`)
		if err := ValidateSemanticBody(input, raw, facts); err == nil {
			t.Fatal("source selection relaxed invocation kind", operations)
		}
	}
}

func TestQARecipeIdentityUsesAssembledOriginalContent(t *testing.T) {
	input := semanticInput()
	input.Validation.Manifest.Cases = nil
	input.Validation.Manifest.SharedSteps = nil
	raw := []byte(`{"cases":[{"scenario":"SUCCESS","oracle":"output","operations":[{"kind":"SHELL","command":{"read":1,"from":1,"to":1}}]}],"coverage":[[1],[1]]}`)
	first, err := DecodeResultWithFacts(input, "result", raw, "output", []string{}, []LocationFact{{Kind: "read", Content: []byte("printf first")}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := DecodeResultWithFacts(input, "result", raw, "output", []string{}, []LocationFact{{Kind: "read", Content: []byte("printf second")}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Result.Validation.Manifest.Cases[0].CaseID == second.Result.Validation.Manifest.Cases[0].CaseID {
		t.Fatal("new case identity lost assembled command content")
	}
	copied := []byte(`{"cases":[{"scenario":"SUCCESS","oracle":"output","operations":[{"kind":"SHELL","command":"printf first"}]}],"coverage":[[1],[1]]}`)
	authored, err := DecodeResultWithFacts(input, "result", copied, "output", []string{}, nil)
	if err != nil || !reflect.DeepEqual(first, authored) {
		t.Fatal("same final original values have different identity", err)
	}
}

func TestQARecipeSchemaPublishesOriginalSourcesAtAllFields(t *testing.T) {
	input := semanticInput()
	input.Validation.Manifest.Cases = nil
	input.Validation.Manifest.SharedSteps = nil
	sourceString := func(schema map[string]any) {
		branches := schema["anyOf"].([]any)
		if len(branches) != 4 || branches[0].(map[string]any)["type"] != "string" || !reflect.DeepEqual(branches[1].(map[string]any)["required"], []string{"read", "from", "to"}) {
			t.Fatal("string field lost authored text or strict original source", schema)
		}
		if !reflect.DeepEqual(branches[2].(map[string]any)["required"], []string{"read", "from", "to", "fromColumn", "toColumn"}) || !reflect.DeepEqual(branches[3].(map[string]any)["required"], []string{"parts"}) {
			t.Fatal("string field lost paired original columns or ordered parts", schema)
		}
	}
	operation := func(schema map[string]any) {
		fields := schema["properties"].(map[string]any)
		if fields["reuse"] != nil || !reflect.DeepEqual(schema["required"], []any{"kind"}) {
			t.Fatal("new invocation schema advertises unissued operation", schema)
		}
		sourceString(fields["command"].(map[string]any))
		sourceString(fields["stdin"].(map[string]any))
		sourceString(fields["argv"].(map[string]any)["items"].(map[string]any))
	}
	for _, mode := range []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox, coverage.ReviewMerge} {
		input.Validation.Manifest.Binding.ReviewKind = mode
		schema, err := ResultSchema(input)
		if err != nil {
			t.Fatal(err)
		}
		fields := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
		for _, family := range []string{"cases", "sharedSteps"} {
			item := fields[family].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
			operation(item["operations"].(map[string]any)["items"].(map[string]any))
			if family == "sharedSteps" {
				sourceString(item["artifacts"].(map[string]any)["items"].(map[string]any))
			}
		}
	}
	input.Handler = "engine.qa.execution.v3"
	input.Validation.CommandCases = []string{"original-approved"}
	schema, err := ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	fields := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
	item := fields["commands"].(map[string]any)["prefixItems"].([]any)[0].(map[string]any)
	if item["minItems"] != 1 {
		t.Fatal("missing command duty weakened")
	}
	operation(item["items"].(map[string]any))
}
