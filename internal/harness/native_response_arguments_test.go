package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrictDecodePreservesTypedNumbersAndOriginalIdentity(t *testing.T) {
	var value struct {
		Ordinal int            `json:"ordinal"`
		Payload map[string]any `json:"payload"`
	}
	if err := StrictDecode([]byte(`{"ordinal":2,"payload":{"request":9007199254740993,"count":1.25}}`), &value); err != nil {
		t.Fatal(err)
	}
	if value.Ordinal != 2 || value.Payload["request"] != json.Number("9007199254740993") || value.Payload["count"] != json.Number("1.25") {
		t.Fatal(value)
	}
}

func TestNativeResponseArgumentsAnswerOriginalQuestions(t *testing.T) {
	run := t.TempDir()
	job := filepath.Join(run, "jobs", "original")
	if err := os.MkdirAll(job, 0700); err != nil {
		t.Fatal(err)
	}
	writePendingsFile(t, job, `{"request_id":"original","status":"pending","method":"item/tool/requestUserInput","questions":[{"id":"first-original","options":[{"label":"red"},{"label":"blue"}]},{"id":"second-original","isOther":true,"options":[{"label":"green"}]}]}`)
	file := filepath.Join(run, "decision.json")
	text := "中文 \"原文\"\n第二行"
	if code, errors := respondCLI(t, "--run", run, "--response-question", "2", "--response-choice", "1", "--response-text", text, "--response-question", "1", "--response-choice", "2", "--response-choice", "1", "--save-decision", file); code != 0 {
		t.Fatal(code, errors)
	}
	if _, err := os.Stat(nativeAnswerPath(job)); !os.IsNotExist(err) {
		t.Fatal("preparation delivered a vote", err)
	}
	if code, errors := respondCLI(t, "--decision-file", file); code != 0 {
		t.Fatal(code, errors)
	}
	answer, err := readJSONFile(nativeAnswerPath(job))
	want := map[string]any{"first-original": map[string]any{"answers": []any{"blue", "red"}}, "second-original": map[string]any{"answers": []any{"green", text}}}
	if err != nil || !valuesEqual(getOf(answer, "answers"), want) || getOf(answer, "request_id") != "original" {
		t.Fatal(answer, err)
	}
}

func TestNativeResponseArgumentsConstructSchemaTypedFormValues(t *testing.T) {
	run := t.TempDir()
	job := filepath.Join(run, "jobs", "original")
	if err := os.MkdirAll(job, 0700); err != nil {
		t.Fatal(err)
	}
	writePendingsFile(t, job, `{"request_id":"original","status":"pending","method":"mcpServer/elicitation/request","mode":"form","options":[{"value":"accept"}],"requestedSchema":{"properties":{"a-flag":{"type":"boolean"},"b-count":{"type":"integer"},"c-rate":{"type":"number"},"d-text":{"type":"string"},"e-enum":{"type":"string","enum":["red","blue"]}}}}`)
	file := filepath.Join(run, "decision.json")
	text := "中文 \"原文\"\n第二行"
	if code, errors := respondCLI(t, "--run", run, "--choice", "1", "--form-field", "1", "--form-value", "false", "--form-field", "2", "--form-value", "9007199254740993", "--form-field", "3", "--form-value", "1.25", "--form-field", "4", "--form-value", text, "--form-field", "5", "--form-choice", "2", "--save-decision", file); code != 0 {
		t.Fatal(code, errors)
	}
	if code, errors := respondCLI(t, "--decision-file", file); code != 0 {
		t.Fatal(code, errors)
	}
	answer, err := readJSONFile(nativeAnswerPath(job))
	want := map[string]any{"a-flag": false, "b-count": jsonNumber("9007199254740993"), "c-rate": jsonNumber("1.25"), "d-text": text, "e-enum": "blue"}
	if err != nil || !valuesEqual(getOf(answer, "answers"), want) || getOf(answer, "option") != "accept" {
		t.Fatal(answer, err)
	}
}

func TestNativeResponseArgumentsRejectInvalidSchemaSelections(t *testing.T) {
	for _, item := range []struct {
		property map[string]any
		value    string
	}{
		{map[string]any{"type": "boolean"}, "1"},
		{map[string]any{"type": "integer"}, "1.5"},
		{map[string]any{"type": "number"}, `"2"`},
		{map[string]any{"type": "number"}, "NaN"},
		{map[string]any{"type": "string", "enum": []any{"original"}}, "original"},
	} {
		if value, err := nativeFormValue(item.property, item.value); err == nil {
			t.Fatal("invalid schema value accepted", item, value)
		}
	}
	run := t.TempDir()
	job := filepath.Join(run, "jobs", "original")
	if err := os.MkdirAll(job, 0700); err != nil {
		t.Fatal(err)
	}
	writePendingsFile(t, job, `{"request_id":"original","status":"pending","method":"item/tool/requestUserInput","questions":[{"id":"original","options":[{"label":"red"}]}]}`)
	for _, args := range [][]string{
		{"--response-question", "2", "--response-choice", "1"},
		{"--response-text", "not offered"},
		{"--responses", `[ {"choices":[1]} ]`},
		{"--values", `[]`},
	} {
		file := filepath.Join(run, "decision.json")
		args = append([]string{"--run", run, "--save-decision", file}, args...)
		if code, _ := respondCLI(t, args...); code == 0 {
			t.Fatal("unpublished or unoffered selection accepted", strings.Join(args, " "))
		}
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Fatal("invalid selection saved a candidate", err)
		}
	}
}

func TestNativeResponseArgumentsReadOriginalTextFiles(t *testing.T) {
	for _, item := range []struct {
		name, record, flag string
		answer             func(any) any
	}{
		{"question", `{"request_id":"original","status":"pending","method":"item/tool/requestUserInput","questions":[{"id":"original-question"}]}`, "--response-text-file", func(v any) any {
			return listOf(getOf(getOf(getOf(v, "answers"), "original-question"), "answers"))[0]
		}},
		{"form", `{"request_id":"original","status":"pending","method":"mcpServer/elicitation/request","mode":"form","options":[{"value":"accept"}],"requestedSchema":{"properties":{"original-field":{"type":"string"}}}}`, "--form-value-file", func(v any) any {
			return getOf(getOf(v, "answers"), "original-field")
		}},
	} {
		t.Run(item.name, func(t *testing.T) {
			run := t.TempDir()
			job := filepath.Join(run, "jobs", "original")
			if err := os.MkdirAll(job, 0700); err != nil {
				t.Fatal(err)
			}
			writePendingsFile(t, job, item.record)
			text := "中文 \"原文\"\r\n末行\n"
			source := filepath.Join(run, "input.txt")
			if err := os.WriteFile(source, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(run, "decision.json")
			args := []string{"--run", run, item.flag, source, "--save-decision", file}
			if item.name == "form" {
				args = append(args, "--choice", "1")
			}
			if code, errors := respondCLI(t, args...); code != 0 {
				t.Fatal(code, errors)
			}
			if code, errors := respondCLI(t, "--decision-file", file); code != 0 {
				t.Fatal(code, errors)
			}
			answer, err := readJSONFile(nativeAnswerPath(job))
			if err != nil || item.answer(answer) != text {
				t.Fatal(answer, err)
			}
			newFile := filepath.Join(run, "other-decision.json")
			for _, extra := range [][]string{{strings.TrimSuffix(item.flag, "-file"), "new text"}, {item.flag, source}} {
				args := append([]string{"--run", run, "--save-decision", newFile, item.flag, source}, extra...)
				if code, _ := respondCLI(t, args...); code != 2 {
					t.Fatal("multiple text sources accepted", code, args)
				}
				if _, err := os.Stat(newFile); !os.IsNotExist(err) {
					t.Fatal("conflicting text sources saved a candidate", err)
				}
			}
		})
	}
}
