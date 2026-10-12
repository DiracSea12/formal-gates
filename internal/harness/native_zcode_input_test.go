package harness

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestZcodeQuestionDecisionPreservesOriginalValues(t *testing.T) {
	question := func(text string, multiple bool, options ...any) any {
		return map[string]any{"question": text, "header": text, "multiSelect": multiple, "options": options}
	}
	option := func(value, label string) any { return map[string]any{"value": value, "label": label} }
	for _, tc := range []struct {
		name      string
		questions []any
		args      []string
		content   any
		cancel    bool
	}{
		{"single", []any{question("Proceed?", false, option("yes", "Display yes"))}, []string{"--choice", "1"}, map[string]any{"answer": "yes"}, false},
		{"multiple", []any{question("First?", false, option("first", "Display first")), question("Second?", false, option("second", "Display second"))}, []string{"--response-question", "1", "--response-choice", "1", "--response-question", "2", "--response-choice", "1"}, map[string]any{"answers": map[string]any{"First?": "first", "Second?": "second"}}, false},
		{"multiple_selections", []any{question("Select?", true, option("one", "Display one"), option("two", "Display two"))}, []string{"--response-choice", "2", "--response-choice", "1"}, map[string]any{"answer": []any{"two", "one"}}, false},
		{"open_text", []any{question("Explain?", false)}, []string{"--response-text", "original\ntext"}, map[string]any{"answer": "original\ntext"}, false},
		{"mixed", []any{question("Select?", true, option("one", "Display one"), option("two", "Display two")), question("Explain?", false)}, []string{"--response-choice", "1", "--response-choice", "2", "--response-question", "2", "--response-text", "original text"}, map[string]any{"answers": map[string]any{"Select?": []any{"one", "two"}, "Explain?": "original text"}}, false},
		{"cancel", []any{question("First?", false, option("first", "Display first")), question("Second?", false, option("second", "Display second"))}, []string{"--cancel"}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := t.TempDir()
			directory := filepath.Join(run, "native", "action", "invocation")
			params := map[string]any{"sessionId": "session", "requestId": "question", "questions": tc.questions}
			spec := zcodeUserInputInteraction
			hold := nativePendingHold{requestID: "question", method: spec.method, status: "pending", summary: spec.summary(params), options: spec.options(params), body: spec.body(params)}
			if err := writeAtomicJSON(nativePendingPath(directory), []any{hold.pendingRecord(1)}); err != nil {
				t.Fatal(err)
			}
			view := nativeQuestionView(hold.pendingRecord(1), 1)
			for i, raw := range tc.questions {
				row := listOf(view["questions"])[i]
				if !valuesEqual(getOf(row, "multiSelect"), getOf(raw, "multiSelect")) {
					t.Fatal("displayed selection cardinality differs from the original question")
				}
			}
			decision := filepath.Join(run, "decision.json")
			args := append([]string{"--run", run, "--save-decision", decision}, tc.args...)
			var errout bytes.Buffer
			if code := NativeRespond(args, &errout); code != 0 {
				t.Fatalf("prepare exit=%d: %s", code, &errout)
			}
			if _, err := os.Stat(nativeAnswerPath(directory)); !os.IsNotExist(err) {
				t.Fatal("unconfirmed candidate delivered an answer")
			}
			if code := NativeRespond([]string{"--decision-file", decision}, &errout); code != 0 {
				t.Fatalf("confirm exit=%d: %s", code, &errout)
			}
			answer, err := readJSONFile(nativeAnswerPath(directory))
			if err != nil || getOf(answer, "request_id") != "question" {
				t.Fatal("original question binding lost", err)
			}
			payload, err := spec.reply(params, nativeAnswerChoice{Cancel: getOf(answer, "cancel") == true, OptionID: textOf(getOf(answer, "option")), Answers: getOf(answer, "answers")})
			if err != nil {
				t.Fatal(err)
			}
			wanted := map[string]any{"action": "accept", "content": tc.content}
			if tc.cancel {
				wanted = map[string]any{"action": "cancel"}
			}
			if !valuesEqual(getOf(payload, "result"), wanted) {
				t.Fatalf("original protocol reply=%s want=%s", mustCanonical(t, payload), mustCanonical(t, wanted))
			}
			t.Logf("public prepare/confirm exit=0; original protocol reply=%s", mustCanonical(t, payload))
		})
	}
}

func TestZcodeQuestionResponsesRespectDisplayedShape(t *testing.T) {
	question := map[string]any{"question": "Select?", "options": []any{map[string]any{"value": "one", "label": "Display one"}, map[string]any{"value": "two", "label": "Display two"}}}
	record := map[string]any{"method": zcodeUserInputMethod, "questions": []any{question}}
	for _, selection := range []nativeSelection{
		{Responses: []nativeQuestionResponse{{Choices: []int{1, 2}}}},
		{Responses: []nativeQuestionResponse{{Text: "not offered"}}},
		{Responses: []nativeQuestionResponse{{Choices: []int{3}}}},
		{Responses: []nativeQuestionResponse{{}}},
	} {
		if _, err := assembleNativeAnswer(record, selection); err == nil {
			t.Fatal("undisplayed response accepted", selection)
		}
	}
}
