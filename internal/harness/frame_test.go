package harness

import (
	"encoding/json"
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
