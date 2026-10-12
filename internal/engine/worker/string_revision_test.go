package worker

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func TestStringRevisionPreservesOriginalValuesAcrossDesignEntrypoints(t *testing.T) {
	originalArgv := []string{"public-tool", "", "two words", "\"quoted\"", "中文"}
	originalArtifacts := []string{"outputs/raw one", "results/中文"}
	for _, affected := range []bool{false, true} {
		for _, sample := range []struct {
			name, argv, artifacts   string
			wantArgv, wantArtifacts []string
		}{
			{"append", `[{"reuse":1},{"reuse":2},{"reuse":3},{"reuse":4},{"reuse":5},"new-flag"]`, `[{"reuse":1},{"reuse":2},"metrics/new"]`, append(append([]string{}, originalArgv...), "new-flag"), []string{"outputs/raw one", "results/中文", "metrics/new"}},
			{"reorder-delete-repeat", `[{"reuse":1},{"reuse":5},{"reuse":4},{"reuse":2},{"reuse":5}]`, `[{"reuse":2},{"reuse":1},{"reuse":2}]`, []string{"public-tool", "中文", "\"quoted\"", "", "中文"}, []string{"results/中文", "outputs/raw one", "results/中文"}},
			{"replace", `["new-tool","","new \"quoted\" value","新参数"]`, `["new artifact","新产物"]`, []string{"new-tool", "", "new \"quoted\" value", "新参数"}, []string{"new artifact", "新产物"}},
			{"clear-artifacts", `[{"reuse":1}]`, `[]`, []string{"public-tool"}, []string{}},
		} {
			t.Run(sample.name+map[bool]string{false: "-selected", true: "-affected"}[affected], func(t *testing.T) {
				input := retainedDesignInput()
				original := coverage.Operation{Kind: coverage.OperationArgv, Argv: originalArgv, Stdin: " original input\n"}
				input.Validation.Manifest.Cases[0].Operations = []coverage.Operation{original}
				input.Validation.Manifest.SharedSteps[0].Operations = []coverage.Operation{original}
				input.Validation.Manifest.SharedSteps[0].Artifacts = originalArtifacts
				operation := `"operations":[{"reuse":1,"argv":` + sample.argv + `}]`
				raw := []byte(`{"cases":[{` + operation + `}],"sharedSteps":[{` + operation + `,"artifacts":` + sample.artifacts + `}]}`)
				if affected {
					input.Validation.AffectedCases = []string{"keep"}
				} else {
					raw = []byte(`{"cases":[{"case":1,` + operation + `}],"sharedSteps":[{"step":1,` + operation + `,"artifacts":` + sample.artifacts + `}],"coverage":[null,null]}`)
				}
				before, _ := json.Marshal(input)
				originalRaw := bytes.Clone(raw)
				event, err := DecodeResultWithPaths(input, "result", raw, "original-output", []string{})
				if err != nil {
					t.Fatal(err)
				}
				manifest := event.Result.Validation.Manifest
				want := []coverage.Operation{{Kind: original.Kind, Argv: sample.wantArgv, Stdin: original.Stdin}}
				if !reflect.DeepEqual(manifest.Cases[0].Operations, want) || !reflect.DeepEqual(manifest.SharedSteps[0].Operations, want) || !reflect.DeepEqual(manifest.SharedSteps[0].Artifacts, sample.wantArtifacts) || event.Result.PayloadDigest != "original-output" {
					t.Fatal("array assembly changed selected original values or output identity", manifest)
				}
				for _, interruption := range []*protocol.RecoveryRecord{nil, {}} {
					input.Validation.Interruption = interruption
					repeated, err := DecodeResultWithPaths(input, "result", raw, "original-output", []string{})
					if err != nil || !reflect.DeepEqual(event, repeated) {
						t.Fatal("replay or continuation changed array assembly", err)
					}
				}
				input.Validation.Interruption = nil
				after, _ := json.Marshal(input)
				if !bytes.Equal(before, after) || !bytes.Equal(raw, originalRaw) {
					t.Fatal("array revision mutated original material or semantic bytes")
				}
				manifest.Cases[0].Operations[0].Argv[0] = "changed result only"
				manifest.SharedSteps[0].Artifacts = append(manifest.SharedSteps[0].Artifacts, "result only")
				if input.Validation.Manifest.Cases[0].Operations[0].Argv[0] != "public-tool" || !reflect.DeepEqual(input.Validation.Manifest.SharedSteps[0].Artifacts, originalArtifacts) {
					t.Fatal("assembled array aliases original storage")
				}
			})
		}
	}
}

func TestStringRevisionRetainsOmittedFieldsAndCreatesNewArrays(t *testing.T) {
	input := retainedDesignInput()
	input.Validation.Manifest.SharedSteps[0].Artifacts = []string{"outputs/raw one", "results/中文"}
	for _, raw := range []string{
		`{"cases":[{"case":1}],"sharedSteps":[{"step":1}],"coverage":[null,null]}`,
		`{"cases":[{}],"sharedSteps":[{}]}`,
	} {
		if raw == `{"cases":[{}],"sharedSteps":[{}]}` {
			input.Validation.AffectedCases = []string{"keep"}
		}
		event, err := DecodeResultWithPaths(input, "result", []byte(raw), "original-output", []string{})
		if err != nil || !reflect.DeepEqual(event.Result.Validation.Manifest.Cases[0].Operations, input.Validation.Manifest.Cases[0].Operations) || !reflect.DeepEqual(event.Result.Validation.Manifest.SharedSteps[0].Artifacts, input.Validation.Manifest.SharedSteps[0].Artifacts) {
			t.Fatal("omitted field failed to inherit its original array", err)
		}
	}
	input.Validation.AffectedCases = nil
	raw := []byte(`{"cases":[{"scenario":"SUCCESS","oracle":"new oracle","operations":[{"kind":"ARGV","argv":["new-tool","","two words","\"quoted\"","中文"]}]}],"sharedSteps":[{"procedure":"new preparation","artifacts":["new path","新产物"],"operations":[{"kind":"ARGV","argv":["prepare-tool"]}]}],"coverage":[null,null]}`)
	event, err := DecodeResultWithPaths(input, "result", raw, "original-output", []string{})
	if err != nil {
		t.Fatal(err)
	}
	manifest := event.Result.Validation.Manifest
	if len(manifest.Cases) != len(input.Validation.Manifest.Cases)+1 || len(manifest.SharedSteps) != len(input.Validation.Manifest.SharedSteps)+1 || !reflect.DeepEqual(manifest.Cases[len(manifest.Cases)-1].Operations[0].Argv, []string{"new-tool", "", "two words", "\"quoted\"", "中文"}) || !reflect.DeepEqual(manifest.SharedSteps[len(manifest.SharedSteps)-1].Artifacts, []string{"new path", "新产物"}) {
		t.Fatal("new semantic strings changed during assembly", manifest)
	}
}

func TestStringRevisionRejectsUnissuedPositionsAndIncompatibleKindReuse(t *testing.T) {
	input := retainedDesignInput()
	for _, affected := range []bool{false, true} {
		for _, fields := range []string{
			`"operations":[{"reuse":1,"argv":[{"reuse":0}]}]`,
			`"operations":[{"reuse":1,"argv":[{"reuse":99}]}]`,
			`"operations":[{"reuse":1,"argv":[{}]}]`,
			`"operations":[{"reuse":1,"argv":[{"reuse":1,"value":"copy"}]}]`,
			`"operations":[{"reuse":1,"kind":"SHELL","command":"printf converted","argv":[{"reuse":1}]}]`,
		} {
			input.Validation.AffectedCases = nil
			raw := `{"cases":[{"case":1,` + fields + `}],"coverage":[null,null]}`
			if affected {
				input.Validation.AffectedCases = []string{"keep"}
				raw = `{"cases":[{` + fields + `}]}`
			}
			if _, err := DecodeResultWithPaths(input, "invalid", []byte(raw), "original-output", []string{}); err == nil {
				t.Fatal("invalid or incompatible element selection admitted", raw)
			}
		}
	}
	input.Validation.AffectedCases = nil
	for _, raw := range []string{
		`{"cases":[{"scenario":"SUCCESS","oracle":"new oracle","operations":[{"kind":"ARGV","argv":[{"reuse":1}]}]}],"coverage":[null,null]}`,
		`{"cases":[],"sharedSteps":[{"procedure":"new preparation","artifacts":[{"reuse":1}]}],"coverage":[null,null]}`,
		`{"cases":[],"sharedSteps":[{"step":1,"artifacts":[{"reuse":99}]}],"coverage":[null,null]}`,
	} {
		if _, err := DecodeResultWithPaths(input, "invalid", []byte(raw), "original-output", []string{}); err == nil {
			t.Fatal("new or unissued artifact selection admitted", raw)
		}
	}
}

func TestStringRevisionSchemaBindsEachOriginalArray(t *testing.T) {
	input := retainedDesignInput()
	input.Validation.Manifest.Cases[0].Operations = []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{"tool", "", "中文"}}, {Kind: coverage.OperationArgv, Argv: []string{"another-tool"}}}
	input.Validation.Manifest.SharedSteps[0].Artifacts = []string{"old one", "old two"}
	checkArray := func(t *testing.T, array map[string]any, want []int) {
		t.Helper()
		branches := array["items"].(map[string]any)["anyOf"].([]any)
		if len(branches) != 5 {
			t.Fatal("string choices omit original lines, columns, parts or reuse", array)
		}
		source := branches[1].(map[string]any)
		required, err := json.Marshal(source["required"])
		if err != nil || string(required) != `["read","from","to"]` {
			t.Fatal("original read source range missing", source)
		}
		columns := branches[2].(map[string]any)
		required, err = json.Marshal(columns["required"])
		if err != nil || string(required) != `["read","from","to","fromColumn","toColumn"]` {
			t.Fatal("original column range missing", columns)
		}
		parts := branches[3].(map[string]any)["properties"].(map[string]any)["parts"].(map[string]any)["items"].(map[string]any)["anyOf"].([]any)
		if len(parts) != 4 || !reflect.DeepEqual(parts[3], branches[4]) {
			t.Fatal("parts reuse is not bound to the original array", array)
		}
		if !reflect.DeepEqual(branches[4].(map[string]any)["properties"].(map[string]any)["reuse"].(map[string]any)["enum"], want) || array["description"] == "" {
			t.Fatal("element choices are not bound to the original array", array)
		}
	}
	for _, affected := range []bool{false, true} {
		if affected {
			input.Validation.AffectedCases = []string{"keep"}
		}
		schema, err := ResultSchema(input)
		if err != nil {
			t.Fatal(err)
		}
		fields := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
		var item, step map[string]any
		if affected {
			item = fields["cases"].(map[string]any)["prefixItems"].([]any)[0].(map[string]any)
			step = fields["sharedSteps"].(map[string]any)["prefixItems"].([]any)[0].(map[string]any)
		} else {
			item = fields["cases"].(map[string]any)["items"].(map[string]any)["anyOf"].([]any)[1].(map[string]any)
			step = fields["sharedSteps"].(map[string]any)["items"].(map[string]any)["anyOf"].([]any)[1].(map[string]any)
		}
		operations := item["properties"].(map[string]any)["operations"].(map[string]any)["items"].(map[string]any)["anyOf"].([]any)
		for index, want := range [][]int{{1, 2, 3}, {1}} {
			properties := operations[index+1].(map[string]any)["properties"].(map[string]any)
			if !reflect.DeepEqual(properties["reuse"].(map[string]any)["enum"], []int{index + 1}) {
				t.Fatal("argv choices detached from original operation")
			}
			checkArray(t, properties["argv"].(map[string]any), want)
		}
		checkArray(t, step["properties"].(map[string]any)["artifacts"].(map[string]any), []int{1, 2})
	}
}
