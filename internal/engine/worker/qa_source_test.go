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

// Authored invocations retain exact values; workspace reads supply semantic payloads.
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

const qaInvocationOperations = `[{"kind":"SHELL","command":"printf '%s\\n' '中文'\nprintf '%s\\n' '\"quoted\"'\n","stdin":{"read":4,"from":2,"to":3}},{"kind":"ARGV","argv":["public-tool","","two words \"quoted\" 中文"],"stdin":{"read":4,"from":2,"to":3}}]`

func qaExpectedOperations() []coverage.Operation {
	stdin := " first line\nsecond 中文\n"
	return []coverage.Operation{{Kind: coverage.OperationShell, Command: "printf '%s\\n' '中文'\nprintf '%s\\n' '\"quoted\"'\n", Stdin: stdin}, {Kind: coverage.OperationArgv, Argv: []string{"public-tool", "", "two words \"quoted\" 中文"}, Stdin: stdin}}
}

func TestQAInitialRecipesAuthorCallsAndRetainOriginalPayloads(t *testing.T) {
	facts := qaRecipeFacts(t)
	for _, mode := range []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox, coverage.ReviewMerge} {
		for _, shared := range []bool{false, true} {
			t.Run(string(mode)+map[bool]string{false: "-case", true: "-shared"}[shared], func(t *testing.T) {
				input := semanticInput()
				input.Validation.Manifest.Binding.ReviewKind = mode
				input.Validation.Manifest.Cases = nil
				input.Validation.Manifest.SharedSteps = nil
				raw := []byte(`{"cases":[{"scenario":"SUCCESS","oracle":"exact original output","operations":` + qaInvocationOperations + `}],"coverage":[[1],[1]]}`)
				if shared {
					raw = []byte(`{"cases":[{"scenario":"SUCCESS","oracle":"exact original output","sharedSteps":[1]}],"sharedSteps":[{"procedure":"prepare original recipe","artifacts":[{"read":5,"from":1,"to":1}],"operations":` + qaInvocationOperations + `}],"coverage":[[1],[1]]}`)
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
	input.Handler = "engine.qa.execution.v4"
	input.Validation.CommandCases = []string{"approved-original"}
	raw := []byte(`{"commands":[` + qaInvocationOperations + `]}`)
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

func TestQARecipeRevisionReusesArgumentsAndRetainsOriginalPayloads(t *testing.T) {
	facts := qaRecipeFacts(t)
	original := []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{"original-tool", "", "retained 中文"}, Stdin: "old argv input"}, {Kind: coverage.OperationShell, Command: "original shell", Stdin: "old shell input"}}
	for _, affected := range []bool{false, true} {
		input := retainedDesignInput()
		input.Validation.Manifest.Cases[0].Operations = original
		input.Validation.Manifest.SharedSteps[0].Operations = original
		input.Validation.Manifest.SharedSteps[0].Artifacts = []string{"old/path", "retained/path"}
		operations := `[{"reuse":2,"command":"printf '%s\\n' '中文'\nprintf '%s\\n' '\"quoted\"'\n","stdin":{"read":4,"from":2,"to":3}},{"reuse":1,"argv":[{"reuse":1},{"reuse":2},"two words \"quoted\" 中文",{"reuse":3}]}]`
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

func TestQARecipeIdentityUsesAuthoredInvocationContent(t *testing.T) {
	input := semanticInput()
	input.Validation.Manifest.Cases = nil
	input.Validation.Manifest.SharedSteps = nil
	decode := func(command string) protocol.Event {
		t.Helper()
		value, _ := json.Marshal(map[string]any{"cases": []any{map[string]any{"scenario": "SUCCESS", "oracle": "output", "operations": []any{map[string]any{"kind": "SHELL", "command": command}}}}, "coverage": [][]int{{1}, {1}}})
		event, err := DecodeResultWithFacts(input, "result", value, "output", []string{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return event
	}
	first, second := decode("printf first"), decode("printf second")
	if first.Result.Validation.Manifest.Cases[0].CaseID == second.Result.Validation.Manifest.Cases[0].CaseID {
		t.Fatal("new case identity lost its authored invocation content")
	}
	if !reflect.DeepEqual(first, decode("printf first")) {
		t.Fatal("the same authored invocation changed its identity")
	}
}

func TestQARecipeSchemaSeparatesInvocationValuesAndOriginalPayloads(t *testing.T) {
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
		if fields["command"].(map[string]any)["type"] != "string" {
			t.Fatal("command still exposes mechanical text sources")
		}
		sourceString(fields["stdin"].(map[string]any))
		if fields["argv"].(map[string]any)["items"].(map[string]any)["type"] != "string" {
			t.Fatal("new arguments still expose mechanical text sources")
		}
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
	input.Handler = "engine.qa.execution.v4"
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

func TestQACallValuesRejectMechanicalTextSources(t *testing.T) {
	facts := []LocationFact{{Kind: "listing"}, {Kind: "requirement", Content: []byte("Build uses project-build; test uses project-tests.\n")}}
	for _, value := range []string{`{"read":2,"from":1,"to":1}`, `{"read":2,"from":1,"to":1,"fromColumn":1,"toColumn":5}`, `{"parts":[{"read":2,"from":1,"to":1}]}`} {
		for _, operation := range []string{`{"kind":"SHELL","command":` + value + `}`, `{"kind":"ARGV","argv":[` + value + `]}`} {
			for _, entry := range []string{"design", "revision", "preparation"} {
				input := semanticInput()
				raw := `{"cases":[{"scenario":"SUCCESS","oracle":"complete output","operations":[` + operation + `]}],"coverage":[[1],[1]]}`
				switch entry {
				case "design":
					input.Validation.Manifest.Cases = nil
					input.Validation.Manifest.SharedSteps = nil
				case "revision":
					input = retainedDesignInput()
					input.Validation.AffectedCases = []string{"keep"}
					raw = `{"cases":[{"operations":[` + operation + `]}]}`
				case "preparation":
					input.Handler = "engine.qa.execution.v4"
					input.Validation.CommandCases = []string{"approved-case"}
					raw = `{"commands":[[` + operation + `]]}`
				}
				if err := ValidateSemanticBody(input, []byte(raw), facts); err == nil {
					t.Fatalf("%s exposed a mechanical invocation source: %s", entry, operation)
				}
				if _, err := DecodeResultWithFacts(input, "invalid", []byte(raw), "native-source", []string{}, facts); err == nil {
					t.Fatalf("%s admitted a mechanical invocation source", entry)
				}
			}
		}
	}
}
