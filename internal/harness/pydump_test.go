package harness

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pydumps(t *testing.T, value any) string {
	t.Helper()
	text, err := pyDumps(value)
	if err != nil {
		t.Fatalf("pyDumps(%v): %v", value, err)
	}
	return text
}

// TestPyDumpsDefaultSeparators pins the Python default separators ", " and
// ": " and the insertion order of dict (contract: 错误文本内嵌 JSON 与 prompt 内嵌
// schema 的字节形状；出站协议行/protocol.jsonl/stdout 已改为 canonical 有界差异).
func TestPyDumpsDefaultSeparators(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{dict(), "{}"},
		{[]any{}, "[]"},
		{dict("a", 1), `{"a": 1}`},
		{dict("a", 1, "b", 2), `{"a": 1, "b": 2}`},
		{dict("a", []any{1, 2}), `{"a": [1, 2]}`},
		{dict("a", dict("b", true)), `{"a": {"b": true}}`},
		{dict("k", nil), `{"k": null}`},
		{[]any{dict("a", 1), dict("b", 2)}, `[{"a": 1}, {"b": 2}]`},
	}
	for _, tc := range cases {
		if got := pydumps(t, tc.value); got != tc.want {
			t.Fatalf("pyDumps = %q, want %q", got, tc.want)
		}
	}
}

// TestPyDumpsUnicodeAndEscapes pins ensure_ascii=False: non-ASCII stays raw,
// the two structural characters and the short control escapes are escaped, and
// every other control character becomes \u00xx (writeCanonicalString is shared
// with the canonical encoder; this pins that no second escaping crept in).
func TestPyDumpsUnicodeAndEscapes(t *testing.T) {
	cases := []struct {
		value string
		want  string
	}{
		{"héllo ☕", `"héllo ☕"`},
		{"quote\"back\\slash", `"quote\"back\\slash"`},
		{"\b\f\n\r\t", `"\b\f\n\r\t"`},
		{"\x00\x01\x1f", `"\u0000\u0001\u001f"`},
		{"</script>&", `"</script>&"`}, // no HTML escaping, unlike encoding/json
	}
	for _, tc := range cases {
		if got := pydumps(t, tc.value); got != tc.want {
			t.Fatalf("pyDumps(%q) = %s, want %s", tc.value, got, tc.want)
		}
	}
}

// TestPyDumpsNumberSemantics pins the Python round-trip rules shared with the
// canonical encoder: arbitrary-precision integers survive, -0 becomes 0,
// floats render with repr and 1e2 becomes 100.0.
func TestPyDumpsNumberSemantics(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{json.Number("1e2"), "100.0"},
		{json.Number("1.5e2"), "150.0"},
		{json.Number("-0"), "0"},
		{json.Number("123456789012345678901234567890"), "123456789012345678901234567890"},
		{json.Number("3"), "3"},
		{json.Number("0.5"), "0.5"},
		{json.Number("1e20"), "1e+20"},
		{json.Number("1e-5"), "1e-05"},
		{json.Number("1e16"), "1e+16"},
		{json.Number("1e15"), "1000000000000000.0"},
		{int(-5), "-5"},
		{int64(42), "42"},
		{float64(2.5), "2.5"},
		{math_Inf(1), "Infinity"},
		{math_Inf(-1), "-Infinity"},
	}
	for _, tc := range cases {
		if got := pydumps(t, tc.value); got != tc.want {
			t.Fatalf("pyDumps(%v) = %s, want %s", tc.value, got, tc.want)
		}
	}
}

// TestPyDumpsRejectsUnorderedMaps pins that a plain map can never leak random
// iteration order into the byte contract.
func TestPyDumpsRejectsUnorderedMaps(t *testing.T) {
	_, err := pyDumps(map[string]any{"a": 1})
	if err == nil || !strings.Contains(err.Error(), "dict") {
		t.Fatalf("expected map rejection mentioning dict, got %v", err)
	}
	if _, err := CanonicalJSON(map[string]any{"a": 1}); err != nil {
		t.Fatalf("canonical still accepts maps (sorted): %v", err)
	}
}

// TestDictDuplicateAndSetSemantics pins Python dict semantics: a repeated key
// keeps its first position and last value; Set replaces in place or appends.
func TestDictDuplicateAndSetSemantics(t *testing.T) {
	object := dict("a", 1, "b", 2, "a", 3)
	if got := pydumps(t, object); got != `{"a": 3, "b": 2}` {
		t.Fatalf("duplicates: %s", got)
	}
	object.Set("b", 9)
	if got := pydumps(t, object); got != `{"a": 3, "b": 9}` {
		t.Fatalf("replace in place: %s", got)
	}
	object.Set("c", 4)
	if got := pydumps(t, object); got != `{"a": 3, "b": 9, "c": 4}` {
		t.Fatalf("append: %s", got)
	}
	if _, ok := object.Get("missing"); ok {
		t.Fatal("missing key must be absent")
	}
	if !object.Has("c") {
		t.Fatal("Has(c)")
	}
}

// TestPyDumpsRawRoundTrip pins the inbound-raw re-encoding contract: document
// key order is preserved, duplicate keys collapse last-wins-first-position,
// and numbers follow the Python round trip. PyDumpsRaw is the file's only
// export and exists for the error-text and prompt byte surfaces.
func TestPyDumpsRawRoundTrip(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`{"b": 1, "a": 2}`, `{"b": 1, "a": 2}`},
		{`{"b":1,"a":2}`, `{"b": 1, "a": 2}`},
		{`{"x": 1e2, "y": [1.5e2, 2]}`, `{"x": 100.0, "y": [150.0, 2]}`},
		{`{"a": 1, "a": 2}`, `{"a": 2}`},
		{`{"z": "café ☕"}`, `{"z": "café ☕"}`},
		{`{"big": 123456789012345678901234567890}`, `{"big": 123456789012345678901234567890}`},
		{`[1.0, true, null, "-0"]`, `[1.0, true, null, "-0"]`},
	}
	for _, tc := range cases {
		got, err := PyDumpsRaw([]byte(tc.raw))
		if err != nil {
			t.Fatalf("PyDumpsRaw(%s): %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("PyDumpsRaw(%s) = %s, want %s", tc.raw, got, tc.want)
		}
	}
}

// TestPyDumpsRawEqualsDumpsOfDecoded pins the identity the prompt surface
// relies on: PyDumpsRaw(raw) and pyDumps(decodeOrdered(raw)) are the same
// bytes, so re-dumping the decoded schema of a canonical native-request.json
// reproduces the reference's json.dumps(schema) verbatim.
func TestPyDumpsRawEqualsDumpsOfDecoded(t *testing.T) {
	raw := `{"zz": 1e2, "aa": {"unicode": "ü会话", "nested": {"b": 2, "a": [1, null, true]}}}`
	viaRaw, err := PyDumpsRaw([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	value, err := decodeOrdered([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	viaValue, err := pyDumps(value)
	if err != nil {
		t.Fatal(err)
	}
	if viaRaw != viaValue {
		t.Fatalf("PyDumpsRaw(%q) = %s, pyDumps(decodeOrdered) = %s", raw, viaRaw, viaValue)
	}
}

// TestDecodeOrderedRejectsMalformed pins CPython's JSONDecodeError texts for
// the corruption shapes a truncated or hand-edited evidence file can take
// (line/column/char included).
func TestDecodeOrderedRejectsMalformed(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"", "Expecting value: line 1 column 1 (char 0)"},
		{"   ", "Expecting value: line 1 column 4 (char 3)"},
		{"x", "Expecting value: line 1 column 1 (char 0)"},
		{"tru", "Expecting value: line 1 column 1 (char 0)"},
		{"{}", ""},
		{"{}x", "Extra data: line 1 column 3 (char 2)"},
		{"01", "Extra data: line 1 column 2 (char 1)"},
		{"1.", "Extra data: line 1 column 2 (char 1)"},
		{"1e", "Extra data: line 1 column 2 (char 1)"},
		{"-", "Expecting value: line 1 column 1 (char 0)"},
		{`{"a"`, "Expecting ':' delimiter: line 1 column 5 (char 4)"},
		{`{"abc`, "Unterminated string starting at: line 1 column 2 (char 1)"},
		{`"abc`, "Unterminated string starting at: line 1 column 1 (char 0)"},
		{`{"a":1`, "Expecting ',' delimiter: line 1 column 7 (char 6)"},
		{"[1", "Expecting ',' delimiter: line 1 column 3 (char 2)"},
		{`{"a":1,}`, "Expecting property name enclosed in double quotes: line 1 column 8 (char 7)"},
		{"{", "Expecting property name enclosed in double quotes: line 1 column 2 (char 1)"},
		{"[1 2]", "Expecting ',' delimiter: line 1 column 4 (char 3)"},
		{`"\q"`, `Invalid \escape: line 1 column 2 (char 1)`},
		{"\"a\x01b\"", "Invalid control character at: line 1 column 3 (char 2)"},
		{`"a\x01b"`, `Invalid \escape: line 1 column 3 (char 2)`},
		{"{\n  \"a\": x}", "Expecting value: line 2 column 8 (char 9)"},
	}
	for _, tc := range cases {
		_, err := decodeOrdered([]byte(tc.raw))
		if tc.want == "" {
			if err != nil {
				t.Fatalf("decodeOrdered(%q): unexpected %v", tc.raw, err)
			}
			continue
		}
		if err == nil || err.Error() != tc.want {
			t.Fatalf("decodeOrdered(%q) = %v, want %q", tc.raw, err, tc.want)
		}
	}
}

// TestDecodeOrderedAcceptsPythonLiterals pins that Infinity/NaN literals parse
// (json.loads accepts them) and re-encode as Python renders them.
func TestDecodeOrderedAcceptsPythonLiterals(t *testing.T) {
	got, err := PyDumpsRaw([]byte(`{"a": Infinity, "b": -Infinity, "c": NaN}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a": Infinity, "b": -Infinity, "c": NaN}`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

// TestDecodeOrderedUniqueRejectsDuplicates pins the infra.read semantics used
// for evidence files: a repeated key is never silently collapsed.
func TestDecodeOrderedUniqueRejectsDuplicates(t *testing.T) {
	_, err := decodeOrderedUnique([]byte(`{"a": 1, "b": 2, "a": 3}`))
	if err == nil || err.Error() != "duplicate JSON field: a" {
		t.Fatalf("got %v", err)
	}
	if _, err := decodeOrderedUnique([]byte(`{"a": 1}`)); err != nil {
		t.Fatalf("unique decode of a clean object: %v", err)
	}
}

// TestOrdObjMarshalJSON pins the compact ordered form used when an ordered
// object crosses encoding/json (the bridge envelope).
func TestOrdObjMarshalJSON(t *testing.T) {
	encoded, err := json.Marshal(dict("b", 1, "a", []any{json.Number("1e2")}))
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"b":1,"a":[100.0]}` {
		t.Fatalf("got %s", encoded)
	}
}

// TestCanonicalJSONSortsOrdObj pins the other byte contract: the same ordered
// value canonicalises with sorted keys and compact separators, so a value can
// cross both contracts unchanged. The crossing happens in canonicalValue (the
// C3 encoder's accepted input types are untouched; an ordered object reaches
// it as its map form, whose sorted-key bytes are identical).
func TestCanonicalJSONSortsOrdObj(t *testing.T) {
	encoded, err := CanonicalJSON(canonicalValue(dict("b", 1, "a", dict("y", nil, "x", true))))
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"a":{"x":true,"y":null},"b":1}` {
		t.Fatalf("got %s", encoded)
	}
	// writeNativeJSON is the production crossing point for native evidence.
	dir := t.TempDir()
	path := filepath.Join(dir, "ordered.json")
	if err := writeNativeJSON(path, dict("b", []any{dict("z", 1)}, "a", 2)); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"a":2,"b":[{"z":1}]}` {
		t.Fatalf("writeNativeJSON(ordObj) = %s", body)
	}
	// The C3 encoder itself still refuses ordered objects: its input surface
	// is exactly what G2a accepted, and supervise.go stays untouched.
	if _, err := CanonicalJSON(dict("a", 1)); err == nil {
		t.Fatal("CanonicalJSON must not accept ordObj directly")
	}
}

func math_Inf(sign int) float64 { return math.Inf(sign) }
