package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFrameResultAccepts(t *testing.T) {
	cases := []struct {
		name string
		text string
		want map[string]any
	}{
		{"plain object", `{"status":"PASS"}`, map[string]any{"status": "PASS"}},
		{"empty object", `{}`, map[string]any{}},
		{"surrounding whitespace", "\n\t {\"a\":1} \r\n", map[string]any{"a": json.Number("1")}},
		{"json fence", "```json\n{\"a\":1}\n```", map[string]any{"a": json.Number("1")}},
		{"bare fence", "```\n{\"a\":1}\n```", map[string]any{"a": json.Number("1")}},
		{"uppercase fence tag", "```JSON\n{\"a\":1}\n```", map[string]any{"a": json.Number("1")}},
		{"crlf fence", "```json\r\n{\"a\":1}\r\n```", map[string]any{"a": json.Number("1")}},
		{"fence without trailing newline", "```json\n{\"a\":1}\n```", map[string]any{"a": json.Number("1")}},
		{"prose then bare object", "Explanation.\n\n{\"a\":1}", map[string]any{"a": json.Number("1")}},
		{"unicode non-bmp", `{"note":"日本語 🚀 café"}`, map[string]any{"note": "日本語 🚀 café"}},
		{"unicode escapes", `{"note":"\u65e5\u672c\u8a9e \ud83d\ude80"}`, map[string]any{"note": "日本語 🚀"}},
		{"combining marks", `{"note":"e\u0301"}`, map[string]any{"note": "e\u0301"}},
		{"empty collections inside", `{"a":[],"b":{}}`, map[string]any{"a": []any{}, "b": map[string]any{}}},
		{"large integer", `{"n":123456789012345678901234567890}`, map[string]any{"n": json.Number("123456789012345678901234567890")}},
		{"exponent form", `{"n":1e2}`, map[string]any{"n": json.Number("1e2")}},
		{"negative and null", `{"a":-1,"b":null,"c":true}`, map[string]any{"a": json.Number("-1"), "b": nil, "c": true}},
		{"nested arrays", `{"a":[{"b":[1,2]}]}`, map[string]any{"a": []any{map[string]any{"b": []any{json.Number("1"), json.Number("2")}}}}},
		{"literal NaN inside a string is not scanned", `{"a":"NaN"}`, map[string]any{"a": "NaN"}},
		{"single empty key", `{"":1}`, map[string]any{"": json.Number("1")}},
		// Prose around the result may mention these words; only the parsed
		// fragment decides. An earlier revision refused all four of these.
		{"prose mentions NaN before the object", "Checked edge cases: NaN and Infinity.\n{\"a\":1}", map[string]any{"a": json.Number("1")}},
		{"prose lists NaN after a colon", "Values seen: NaN, Infinity.\n{\"a\":1}", map[string]any{"a": json.Number("1")}},
		{"prose mentions NaN after the object", "{\"a\":1}\nNote: no NaN was used.", map[string]any{"a": json.Number("1")}},
		{"prose mentions Infinity before a fence", "Checked Infinity.\n```json\n{\"a\":1}\n```", map[string]any{"a": json.Number("1")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, refusal := FrameResult(tc.text)
			if refusal != "" {
				t.Fatalf("refused: %s", refusal)
			}
			if canon(got) != canon(tc.want) {
				t.Fatalf("got %s want %s", canon(got), canon(tc.want))
			}
		})
	}
}

func TestFrameResultRefuses(t *testing.T) {
	const p = "NATIVE_RESULT: "
	cases := []struct {
		name string
		text string
		want string
	}{
		{"duplicate top level", `{"a":1,"a":2}`, p + "duplicate JSON field a"},
		{"duplicate nested", `{"o":{"b":1,"b":2}}`, p + "duplicate JSON field b"},
		{"duplicate in fence", "```json\n{\"a\":1,\"a\":2}\n```", p + "duplicate JSON field a"},
		{"nan", `{"a":NaN}`, p + "non-finite number NaN"},
		{"infinity", `{"a":Infinity}`, p + "non-finite number Infinity"},
		{"negative infinity", `{"a":-Infinity}`, p + "non-finite number -Infinity"},
		{"nan in array", `{"a":[1,NaN]}`, p + "non-finite number NaN"},
		{"two fences", "```json\n{\"a\":1}\n```\ntext\n```json\n{\"a\":2}\n```", p + "final message is not one unambiguous JSON object"},
		// A brace that is not at a line start is not a bare-object candidate at all.
		{"mid-line brace is not a candidate", "Result {\"a\":1} plus [extra]", p + "no unambiguous result object"},
		{"bare object with trailing brace", "{\"a\":1}{\"b\":2}", p + "competing structured content outside result"},
		{"top level array", `[1,2]`, p + "final result must be an object"},
		{"top level string", `"text"`, p + "final result must be an object"},
		{"top level number", `42`, p + "final result must be an object"},
		{"top level null", `null`, p + "final result must be an object"},
		{"empty input", "", p + "no unambiguous result object"},
		{"whitespace only", "   \n\t ", p + "no unambiguous result object"},
		{"prose only", "I could not decide.", p + "no unambiguous result object"},
		{"unclosed fence", "```json\n{\"a\":1}", p + "final message is not one unambiguous JSON object"},
		{"fence with invalid json", "```json\n{\"a\":}\n```", p + "invalid JSON result fence"},
		{"bare object invalid json", "prose\n{\"a\":}", p + "invalid result object"},
		{"trailing comma", `{"a":1,}`, p + "invalid result object"},
		{"two documents", `{"a":1} {"b":2}`, p + "competing structured content outside result"},
		{"duplicate empty key", `{"":1,"":2}`, p + "duplicate JSON field "},
		{"duplicate empty key in fence", "```json\n{\"\":1,\"\":2}\n```", p + "duplicate JSON field "},
		// The mirror image of the acceptance cases above: the non-finite
		// literal really is inside the parsed object, so it must refuse.
		{"non-finite inside the object after prose", "Checked NaN.\n{\"a\":NaN}", p + "non-finite number NaN"},
		{"non-finite inside the fence after prose", "Checked Infinity.\n```json\n{\"a\":Infinity}\n```", p + "non-finite number Infinity"},
		{"non-finite inside a bare object", "prose\n{\"a\":-Infinity}", p + "non-finite number -Infinity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, refusal := FrameResult(tc.text)
			if refusal != tc.want {
				t.Fatalf("refusal %q, want %q", refusal, tc.want)
			}
		})
	}
}

func TestFrameResultIsDeterministic(t *testing.T) {
	text := "prose\n{\"b\":2,\"a\":[1,{\"c\":3}]}"
	first, firstRefusal := FrameResult(text)
	for i := 0; i < 5; i++ {
		again, refusal := FrameResult(text)
		if refusal != firstRefusal || canon(again) != canon(first) {
			t.Fatalf("iteration %d differed: %q vs %q", i, refusal, firstRefusal)
		}
	}
}

func canon(value map[string]any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "MARSHAL_ERROR"
	}
	return string(encoded)
}

// Native final messages may explain code without supplying another JSON value.
// The framed result keeps its exact value, while genuine competitors still fail.
func TestFrameResultDistinguishesCodeFromJSON(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "gate_response_with_code.json"))
	if err != nil {
		t.Fatal(err)
	}
	var replies []struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(body, &replies); err != nil {
		t.Fatal(err)
	}
	for i, reply := range replies {
		result, refusal := FrameResult(reply.Response)
		if refusal != "" || canon(result) != `{"findings":[],"status":"PASS"}` {
			t.Fatalf("native final message %d: %s %s", i, canon(result), refusal)
		}
	}
	for _, tc := range []struct{ outside, refusal string }{
		{`{"other":true}`, "NATIVE_RESULT: competing structured content outside result"},
		{`[1,2]`, "NATIVE_RESULT: competing structured content outside result"},
		{`{"a":1,"a":2}`, "NATIVE_RESULT: duplicate JSON field a"},
		{`{"n":NaN}`, "NATIVE_RESULT: non-finite number NaN"},
		{`{"unfinished":`, "NATIVE_RESULT: competing structured content outside result"},
	} {
		text := tc.outside + "\n```json\n{\"status\":\"PASS\",\"findings\":[]}\n```"
		_, refusal := FrameResult(text)
		if refusal != tc.refusal {
			t.Fatalf("competing content %s: %s", tc.outside, refusal)
		}
	}
}

// Explanation after an unfinished JSON member cannot erase a competing result.
func TestFrameResultKeepsDamagedJSONStructure(t *testing.T) {
	result := "```json\n{\"status\":\"PASS\",\"findings\":[]}\n```"
	for _, partial := range []string{
		`{"status":"FAIL"`,
		`[1,`,
		`["unfinished`,
		`{"unfinished`,
		`[tru`,
		`[-1e`,
	} {
		for _, before := range []bool{true, false} {
			text := partial + "\nI could not finish the preceding structure.\n" + result
			if !before {
				text = result + "\n" + partial + "\nI could not finish the preceding structure."
			}
			_, refusal := FrameResult(text)
			if refusal != "NATIVE_RESULT: competing structured content outside result" {
				t.Fatalf("unfinished structure %q before=%t: %s", partial, before, refusal)
			}
		}
	}
	for _, explanation := range []string{
		"func size(word string) int { return len(word) }",
		"A non-JSON description { ordinary text } and [ordinary text].",
	} {
		value, refusal := FrameResult(explanation + "\n" + result)
		if refusal != "" || canon(value) != `{"findings":[],"status":"PASS"}` {
			t.Fatalf("non-JSON explanation refused: %s %s", canon(value), refusal)
		}
	}
}

// Program indices in native explanations do not change the original gate
// verdict, severity, locations or findings in the single framed result.
func TestFrameResultRetainsGateResponsesWithIndices(t *testing.T) {
	var replies []struct {
		Response string         `json:"response"`
		Result   map[string]any `json:"result"`
	}
	body, err := os.ReadFile(filepath.Join("testdata", "gate_responses_with_indices.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &replies); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 2 {
		t.Fatal("both original indexed explanations are required")
	}
	for _, reply := range replies {
		result, refusal := FrameResult(reply.Response)
		if refusal != "" || canon(result) != canon(reply.Result) || result["status"] != "FAIL" {
			t.Fatalf("gate result changed: %s %s", canon(result), refusal)
		}
	}
}

func TestFrameResultDistinguishesAttachedIndicesFromIndependentStructures(t *testing.T) {
	result := `{"findings":[],"status":"PASS"}`
	fence := "```json\n" + result + "\n```"
	for _, expression := range []string{"items[0]", "table[42][2]", "lookup()[3]", "σ₂[-12]", "名称[true]", "name\u0301[0]", "object.field[column+1]", "value[null]", "node{\"field\":1}"} {
		for _, wrapped := range []string{result, fence} {
			for _, text := range []string{expression + "\n" + wrapped, wrapped + "\n" + expression} {
				value, refusal := FrameResult(text)
				if refusal != "" || canon(value) != result {
					t.Fatalf("attached expression %q changed result: %s %s", expression, canon(value), refusal)
				}
			}
		}
	}
	for _, outside := range []string{`[1]`, `{"other":true}`, `[1][2]`, `{"a":1}{"b":2}`, `["unfinished`, `[tru`, `[-1e`, `{"unfinished`, `{"a":1,"a":2}`, `[NaN]`, `[Infinity]`} {
		for _, wrapped := range []string{result, fence} {
			for _, text := range []string{"items[0] " + outside + "\n" + wrapped, wrapped + "\nitems[0] " + outside} {
				_, refusal := FrameResult(text)
				if refusal == "" {
					t.Fatalf("independent competitor %q was lost in indexed prose", outside)
				}
			}
		}
	}
	for _, text := range []string{result + `[1]`, result + `{"other":true}`, result + `["unfinished`} {
		if _, refusal := FrameResult(text); refusal != "NATIVE_RESULT: competing structured content outside result" {
			t.Fatalf("adjacent competitor was lost: %s", refusal)
		}
	}
}
