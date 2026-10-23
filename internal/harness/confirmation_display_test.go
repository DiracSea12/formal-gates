package harness

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNativeConfirmationBoundsLongQuestionAndPreservesAnswer(t *testing.T) {
	directory := t.TempDir()
	selected := "ALLOW ONCE: " + strings.Repeat("原始选定答复\n", 2000) + "原始末尾"
	pending := map[string]any{"connection": 1, "request_id": "long-original-question", "method": "item/tool/requestUserInput", "status": "pending", "questions": []any{
		map[string]any{"id": "original-choice", "question": strings.Repeat("请核对原始操作及其影响\n", 2000), "options": []any{
			map[string]any{"label": "DENY: " + strings.Repeat("拒绝\n", 2000)}, map[string]any{"label": selected},
		}},
	}}
	body, err := json.Marshal(pending)
	if err != nil {
		t.Fatal(err)
	}
	writePendingsFile(t, directory, string(body))
	candidate, err := prepareHeldTest(t, directory, 1, nativeSelection{Choice: 2})
	if err != nil {
		t.Fatal(err)
	}
	decision := filepath.Join(directory, "selected-decision.json")
	if err := writeAtomicJSON(decision, candidate); err != nil {
		t.Fatal(err)
	}
	code, errors := respondWithHostDisplay(t, func(message string) {
		characters, lines := utf8.RuneCountInString(message), strings.Count(message, "\n")+1
		t.Logf("native form: %d characters, %d lines", characters, lines)
		if characters > 800 || lines > 12 {
			t.Errorf("native confirmation exceeds its display budget: %d characters, %d lines", characters, lines)
		}
		if !strings.Contains(message, "ALLOW ONCE") || !strings.Contains(message, "long-original-question") {
			t.Error("confirmation omitted the selected answer or original question identity")
		}
	}, "--decision-file", decision)
	if code != 0 {
		t.Fatal(code, errors)
	}
	answer, err := readJSONFile(nativeAnswerPath(directory))
	values := listOf(getOf(getOf(getOf(answer, "answers"), "original-choice"), "answers"))
	if err != nil || getOf(answer, "request_id") != "long-original-question" || len(values) != 1 || values[0] != selected {
		t.Fatal("display shortening changed the original delivered answer", err)
	}
}

func TestNativeConfirmationShowsOriginalStructuredAnswers(t *testing.T) {
	options := []any{map[string]any{"label": "Keep", "value": "keep-original"}, map[string]any{"label": "Apply", "value": "apply-original"}}
	for _, test := range []struct {
		name      string
		questions []any
		selection nativeSelection
		answers   any
		visible   []string
	}{
		{"single option", []any{map[string]any{"question": "Operation?", "options": options}}, nativeSelection{Choice: 2}, map[string]any{"answer": "apply-original"}, []string{"apply-original"}},
		{"single text", []any{map[string]any{"question": "Reason?"}}, nativeSelection{Responses: []nativeQuestionResponse{{Text: "free-original"}}}, map[string]any{"answer": "free-original"}, []string{"free-original"}},
		{"multiple questions", []any{map[string]any{"question": "Operation?", "options": options}, map[string]any{"question": "Reason?"}}, nativeSelection{Responses: []nativeQuestionResponse{{Choices: []int{2}}, {Text: "note-original"}}}, map[string]any{"answers": map[string]any{"Operation?": "apply-original", "Reason?": "note-original"}}, []string{"apply-original", "note-original"}},
		{"multiple options", []any{map[string]any{"question": "Flags?", "multiSelect": true, "options": options}}, nativeSelection{Responses: []nativeQuestionResponse{{Choices: []int{1, 2}}}}, map[string]any{"answer": []any{"keep-original", "apply-original"}}, []string{"keep-original", "apply-original"}},
		{"multiple texts", []any{map[string]any{"question": "A?"}, map[string]any{"question": "B?"}}, nativeSelection{Responses: []nativeQuestionResponse{{Text: "first-original"}, {Text: "second-original"}}}, map[string]any{"answers": map[string]any{"A?": "first-original", "B?": "second-original"}}, []string{"first-original", "second-original"}},
		{"cancel", []any{map[string]any{"question": "Operation?", "options": options}}, nativeSelection{Cancel: true}, nil, []string{`"cancel":true`}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			pending := map[string]any{"connection": 1, "request_id": "original-zcode-question", "method": zcodeUserInputMethod, "status": "pending", "questions": test.questions}
			body, err := json.Marshal(pending)
			if err != nil {
				t.Fatal(err)
			}
			writePendingsFile(t, directory, string(body))
			candidate, err := prepareHeldTest(t, directory, 1, test.selection)
			if err != nil {
				t.Fatal(err)
			}
			decision := filepath.Join(directory, "selected-decision.json")
			if err := writeAtomicJSON(decision, candidate); err != nil {
				t.Fatal(err)
			}
			code, errors := respondWithHostDisplay(t, func(message string) {
				if utf8.RuneCountInString(message) > 800 || strings.Count(message, "\n")+1 > 12 {
					t.Error("structured answer exceeded the shared display budget")
				}
				for _, value := range test.visible {
					if !strings.Contains(message, value) {
						t.Errorf("confirmation omitted original selected meaning %q: %s", value, message)
					}
				}
			}, "--decision-file", decision)
			if code != 0 {
				t.Fatal(code, errors)
			}
			answer, err := readJSONFile(nativeAnswerPath(directory))
			if err != nil || getOf(answer, "request_id") != "original-zcode-question" || getOf(answer, "cancel") != test.selection.Cancel || !valuesEqual(getOf(answer, "answers"), test.answers) {
				t.Fatal("confirmation changed the original structured answer", answer, err)
			}
			reply, err := zcodeUserInputInteraction.reply(map[string]any{"questions": test.questions}, nativeAnswerChoice{Cancel: getOf(answer, "cancel") == true, Answers: getOf(answer, "answers")})
			if err != nil {
				t.Fatal(err)
			}
			if test.selection.Cancel {
				if !valuesEqual(reply, map[string]any{"result": map[string]any{"action": "cancel"}}) {
					t.Fatal("cancel became a different native reply", reply)
				}
			} else if !valuesEqual(reply, map[string]any{"result": map[string]any{"action": "accept", "content": test.answers}}) {
				t.Fatal("native reply changed the original structured content", reply)
			}
		})
	}
}
