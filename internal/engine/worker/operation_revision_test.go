package worker

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func TestOperationRevisionCarriesOriginalValuesAcrossDesignEntrypoints(t *testing.T) {
	original := []coverage.Operation{
		{Kind: coverage.OperationArgv, Argv: []string{"public-tool", "", "two words", "\"quoted\"", "中文"}, Stdin: " original input\n"},
		{Kind: coverage.OperationShell, Command: "printf '%s' \"$value\"\n", Stdin: "shell input\n"},
	}
	added := coverage.Operation{Kind: coverage.OperationArgv, Argv: []string{"public-tool", "new-observation"}}
	for _, affected := range []bool{false, true} {
		t.Run(map[bool]string{false: "selected-originals", true: "affected-originals"}[affected], func(t *testing.T) {
			input := retainedDesignInput()
			input.Validation.Manifest.Cases[0].Operations = original
			input.Validation.Manifest.SharedSteps[0].Operations = original
			var raw []byte
			if affected {
				input.Validation.AffectedCases = []string{"keep"}
				raw = []byte(`{"cases":[{"operations":[{"reuse":1},{"reuse":2},{"kind":"ARGV","argv":["public-tool","new-observation"]}]}],"sharedSteps":[{"operations":[{"reuse":1},{"reuse":2},{"kind":"ARGV","argv":["public-tool","new-observation"]}]}]}`)
			} else {
				raw = []byte(`{"cases":[{"case":1,"operations":[{"reuse":1},{"reuse":2},{"kind":"ARGV","argv":["public-tool","new-observation"]}]}],"sharedSteps":[{"step":1,"operations":[{"reuse":1},{"reuse":2},{"kind":"ARGV","argv":["public-tool","new-observation"]}]}],"coverage":[null,null]}`)
			}
			before, _ := json.Marshal(input)
			originalRaw := bytes.Clone(raw)
			event, err := DecodeResultWithPaths(input, "result", raw, "original-semantic-proof", []string{})
			if err != nil {
				t.Fatal(err)
			}
			want := append(append([]coverage.Operation{}, original...), added)
			manifest := event.Result.Validation.Manifest
			if !reflect.DeepEqual(manifest.Cases[0].Operations, want) || !reflect.DeepEqual(manifest.SharedSteps[0].Operations, want) || event.Result.PayloadDigest != "original-semantic-proof" {
				t.Fatal("append changed original command bytes or lost original output evidence", manifest)
			}
			for _, interruption := range []*protocol.RecoveryRecord{nil, {}} {
				input.Validation.Interruption = interruption
				repeated, err := DecodeResultWithPaths(input, "result", raw, "original-semantic-proof", []string{})
				if err != nil || !reflect.DeepEqual(event, repeated) {
					t.Fatal("repeated or continued input used different operation assembly", err)
				}
			}
			input.Validation.Interruption = nil
			after, _ := json.Marshal(input)
			if !bytes.Equal(before, after) || !bytes.Equal(originalRaw, raw) {
				t.Fatal("operation revision mutated original input or raw semantic bytes")
			}
		})
	}
}

func TestOperationRevisionChangesOnlyPresentFieldsAndUsesFinalOrder(t *testing.T) {
	input := retainedDesignInput()
	original := []coverage.Operation{
		{Kind: coverage.OperationArgv, Argv: []string{"public-tool", "", "two words"}, Stdin: " original input\n"},
		{Kind: coverage.OperationShell, Command: "printf '%s' \"$value\"\n", Stdin: "shell input\n"},
	}
	input.Validation.AffectedCases = []string{"keep"}
	input.Validation.Manifest.Cases[0].Operations = original
	input.Validation.Manifest.SharedSteps[0].Operations = original
	for _, sample := range []struct {
		name, operations string
		want             []coverage.Operation
	}{
		{"change-stdin", `[{"reuse":1,"stdin":"changed input\n"},{"reuse":2,"stdin":""}]`, []coverage.Operation{{Kind: original[0].Kind, Argv: original[0].Argv, Stdin: "changed input\n"}, {Kind: original[1].Kind, Command: original[1].Command}}},
		{"change-argv", `[{"reuse":1,"argv":["new-tool","new argument"]},{"reuse":2}]`, []coverage.Operation{{Kind: original[0].Kind, Argv: []string{"new-tool", "new argument"}, Stdin: original[0].Stdin}, original[1]}},
		{"change-command", `[{"reuse":1},{"reuse":2,"command":"printf changed\n"}]`, []coverage.Operation{original[0], {Kind: original[1].Kind, Command: "printf changed\n", Stdin: original[1].Stdin}}},
		{"reorder", `[{"reuse":2},{"reuse":1}]`, []coverage.Operation{original[1], original[0]}},
		{"delete-original", `[{"reuse":2}]`, []coverage.Operation{original[1]}},
		{"clear", `[]`, []coverage.Operation{}},
		{"change-kind", `[{"reuse":1,"kind":"SHELL","command":"printf converted"},{"reuse":2,"kind":"ARGV","argv":["new-tool"]}]`, []coverage.Operation{{Kind: coverage.OperationShell, Command: "printf converted", Stdin: original[0].Stdin}, {Kind: coverage.OperationArgv, Argv: []string{"new-tool"}, Stdin: original[1].Stdin}}},
	} {
		t.Run(sample.name, func(t *testing.T) {
			for _, affected := range []bool{true, false} {
				raw := []byte(`{"cases":[{"operations":` + sample.operations + `}],"sharedSteps":[{"operations":` + sample.operations + `}]}`)
				if affected {
					input.Validation.AffectedCases = []string{"keep"}
				} else {
					input.Validation.AffectedCases = nil
					raw = []byte(`{"cases":[{"case":1,"operations":` + sample.operations + `}],"sharedSteps":[{"step":1,"operations":` + sample.operations + `}],"coverage":[null,null]}`)
				}
				event, err := DecodeResultWithPaths(input, "result", raw, "original-output", []string{})
				if err != nil {
					t.Fatal(err)
				}
				manifest := event.Result.Validation.Manifest
				if !reflect.DeepEqual(manifest.Cases[0].Operations, sample.want) || !reflect.DeepEqual(manifest.SharedSteps[0].Operations, sample.want) {
					t.Fatal("explicit sequence or field inheritance differed from requested semantics", affected, manifest)
				}
			}
		})
	}
	if !reflect.DeepEqual(input.Validation.Manifest.Cases[0].Operations, original) || !reflect.DeepEqual(input.Validation.Manifest.SharedSteps[0].Operations, original) {
		t.Fatal("local changes mutated original operation storage")
	}
}

func TestOperationRevisionRejectsIncompleteOrConflictingInvocations(t *testing.T) {
	input := retainedDesignInput()
	input.Validation.AffectedCases = []string{"keep"}
	input.Validation.Manifest.Cases[0].Operations = append(input.Validation.Manifest.Cases[0].Operations, coverage.Operation{Kind: coverage.OperationShell, Command: "printf original", Stdin: "shell original input"})
	input.Validation.Manifest.SharedSteps[0].Operations = input.Validation.Manifest.Cases[0].Operations
	for _, operations := range []string{
		`[{"reuse":0,"kind":"ARGV","argv":["tool"]}]`,
		`[{"reuse":-1}]`, `[{"reuse":99}]`,
		`[{"reuse":1,"kind":"SHELL","command":"printf converted","argv":["conflicting-tool"]}]`,
		`[{"reuse":1,"kind":"SHELL"}]`,
		`[{"reuse":2,"kind":"ARGV"}]`,
		`[{"reuse":2,"kind":"ARGV","argv":["tool"],"command":"printf conflict"}]`,
		`[{"reuse":1,"kind":"ARGV","command":"printf conflict","argv":["tool"]}]`,
		`[{"reuse":1,"argv":[]}]`,
		`[{"reuse":1,"command":"printf conflict"}]`,
		`[{"kind":"ARGV"}]`, `[{"argv":["tool"]}]`,
		`[{"kind":"SHELL"}]`, `[{"kind":"SHELL","command":" "}]`,
		`[{"kind":"ARGV","argv":["tool"],"command":"printf conflict"}]`,
		`[{"kind":"SHELL","command":"printf conflict","argv":["tool"]}]`,
		`[{"reuse":1,"remove":true}]`,
	} {
		for _, raw := range []string{
			`{"cases":[{"operations":` + operations + `}]}`,
			`{"cases":[{}],"sharedSteps":[{"operations":` + operations + `}]}`,
		} {
			if _, err := DecodeResultWithPaths(input, "invalid", []byte(raw), "original-output", []string{}); err == nil {
				t.Fatal("incomplete, unknown or conflicting invocation admitted", raw)
			}
		}
	}
	input.Validation.AffectedCases = nil
	for _, raw := range []string{
		`{"cases":[{"scenario":"SUCCESS","oracle":"new oracle","operations":[{"reuse":1}]}],"coverage":[null,null]}`,
		`{"cases":[{"scenario":"SUCCESS","oracle":"new oracle","operations":[{"kind":"ARGV"}]}],"coverage":[null,null]}`,
		`{"cases":[],"sharedSteps":[{"procedure":"new preparation","operations":[{"reuse":1}]}],"coverage":[null,null]}`,
		`{"cases":[],"sharedSteps":[{"procedure":"new preparation","operations":[{"kind":"SHELL"}]}],"coverage":[null,null]}`,
	} {
		if _, err := DecodeResultWithPaths(input, "invalid", []byte(raw), "original-output", []string{}); err == nil {
			t.Fatal("new item admitted an inherited or incomplete operation", raw)
		}
	}
}

func TestOperationRevisionSchemaBindsSelectionsToCurrentItem(t *testing.T) {
	input := retainedDesignInput()
	input.Validation.Manifest.Cases[0].Operations = append(input.Validation.Manifest.Cases[0].Operations, coverage.Operation{Kind: coverage.OperationShell, Command: "printf original"})
	check := func(t *testing.T, item map[string]any, want []int) {
		t.Helper()
		operations := item["properties"].(map[string]any)["operations"].(map[string]any)
		branches := operations["items"].(map[string]any)["anyOf"].([]any)
		if len(want) == 0 {
			if len(branches) != 1 {
				t.Fatal("empty original operation list published a reuse option")
			}
			return
		}
		if len(branches) != len(want)+1 {
			t.Fatal("original operations lack separately bound reuse alternatives")
		}
		for index, position := range want {
			change := branches[index+1].(map[string]any)
			if !reflect.DeepEqual(change["required"], []any{"reuse"}) || !reflect.DeepEqual(change["properties"].(map[string]any)["reuse"].(map[string]any)["enum"], []int{position}) || operations["description"] == "" {
				t.Fatal("schema lost current operation position or field inheritance contract", change)
			}
		}
	}
	for _, affected := range []bool{false, true} {
		if affected {
			input.Validation.AffectedCases = []string{"keep", "change"}
		}
		schema, err := ResultSchema(input)
		if err != nil {
			t.Fatal(err)
		}
		fields := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
		if affected {
			cases := fields["cases"].(map[string]any)["prefixItems"].([]any)
			check(t, cases[0].(map[string]any), []int{1, 2})
			check(t, cases[1].(map[string]any), nil)
			steps := fields["sharedSteps"].(map[string]any)["prefixItems"].([]any)
			check(t, steps[0].(map[string]any), []int{1})
		} else {
			for _, sample := range []struct {
				field, selection string
				positions        [][]int
			}{
				{"cases", "case", [][]int{{1, 2}, nil, nil}},
				{"sharedSteps", "step", [][]int{{1}, nil}},
			} {
				branches := fields[sample.field].(map[string]any)["items"].(map[string]any)["anyOf"].([]any)
				if len(branches) != len(sample.positions)+1 {
					t.Fatal("schema did not bind each original QA item separately")
				}
				for index, want := range sample.positions {
					item := branches[index+1].(map[string]any)
					if !reflect.DeepEqual(item["properties"].(map[string]any)[sample.selection].(map[string]any)["enum"], []int{index + 1}) {
						t.Fatal("operation choices detached from original item")
					}
					check(t, item, want)
				}
			}
		}
	}
	input.Validation.Manifest.Cases[0].Operations = input.Validation.Manifest.Cases[0].Operations[:1]
	schema, err := ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	item := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["cases"].(map[string]any)["prefixItems"].([]any)[0].(map[string]any)
	check(t, item, []int{1})
}
