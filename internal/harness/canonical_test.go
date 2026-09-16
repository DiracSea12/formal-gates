package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func sha256HexForTest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func canonicalOf(t *testing.T, literal string) string {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(literal))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode %s: %v", literal, err)
	}
	encoded, err := CanonicalJSON(value)
	if err != nil {
		t.Fatalf("canonical %s: %v", literal, err)
	}
	return string(encoded)
}

func TestCanonicalShape(t *testing.T) {
	cases := []struct{ name, literal, want string }{
		{"empty object", `{}`, `{}`},
		{"empty array", `[]`, `[]`},
		{"keys sorted", `{"b":2,"a":1}`, `{"a":1,"b":2}`},
		{"uppercase before lowercase", `{"a":1,"A":2,"z":3}`, `{"A":2,"a":1,"z":3}`},
		{"empty key sorts first", `{"a":1,"":2}`, `{"":2,"a":1}`},
		{"nested keys sorted", `{"o":{"y":1,"x":2}}`, `{"o":{"x":2,"y":1}}`},
		{"booleans and null", `{"a":true,"b":false,"c":null}`, `{"a":true,"b":false,"c":null}`},
		{"no spaces", `{"a": [1, 2]}`, `{"a":[1,2]}`},
		{"unicode kept raw", `{"k":"日本 🚀"}`, `{"k":"日本 🚀"}`},
		{"unicode key", `{"日本":1}`, `{"日本":1}`},
		{"angle brackets not escaped", `{"a":"x<y & z"}`, `{"a":"x<y & z"}`},
		// ensure_ascii=False: an escaped separator in the input comes out as the
		// raw character, which Go's own encoder would have escaped.
		{"line separators not escaped", `{"a":"p\u2028q\u2029r"}`, "{\"a\":\"p\u2028q\u2029r\"}"},
		{"quote and backslash", `{"a":"q\" b\\"}`, `{"a":"q\" b\\"}`},
		{"short control escapes", `{"a":"\b\f\n\r\t"}`, `{"a":"\b\f\n\r\t"}`},
		{"other control chars", `{"a":"\u0000\u0001\u001f"}`, `{"a":"\u0000\u0001\u001f"}`},
		{"solidus not escaped", `{"a":"a/b"}`, `{"a":"a/b"}`},
		{"integer", `{"n":42}`, `{"n":42}`},
		{"negative integer", `{"n":-7}`, `{"n":-7}`},
		{"integer leading minus zero", `{"n":-0}`, `{"n":0}`},
		{"big integer keeps precision", `{"n":123456789012345678901234567890}`, `{"n":123456789012345678901234567890}`},
		// Beyond float64's range: the pre-scan must not narrow these.
		{"integer beyond float64", `{"n":1000000000000000000000000000000000000000000000000000000000000000000}`, `{"n":1000000000000000000000000000000000000000000000000000000000000000000}`},
		{"negative integer beyond float64", `{"n":-1000000000000000000000000000000000000000000000000000000000000000000}`, `{"n":-1000000000000000000000000000000000000000000000000000000000000000000}`},
		{"float keeps a fractional digit", `{"n":1.0}`, `{"n":1.0}`},
		{"negative zero float", `{"n":-0.0}`, `{"n":-0.0}`},
		{"exponent form is normalised", `{"n":1e2}`, `{"n":100.0}`},
		{"shortest repr", `{"n":0.1}`, `{"n":0.1}`},
		{"sum of tenths", `{"n":0.30000000000000004}`, `{"n":0.30000000000000004}`},
		{"deep nesting", `{"a":[[[{"b":[]}]]]}`, `{"a":[[[{"b":[]}]]]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canonicalOf(t, tc.literal); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

// TestCanonicalFloatBoundaries pins the fixed-versus-scientific rule, which is
// the place where Go's own formatting differs most from CPython's.
func TestCanonicalFloatBoundaries(t *testing.T) {
	cases := []struct {
		literal string
		want    string
	}{
		{`{"n":1e14}`, `{"n":100000000000000.0}`},
		{`{"n":1e15}`, `{"n":1000000000000000.0}`},
		{`{"n":1e16}`, `{"n":1e+16}`},
		{`{"n":1e21}`, `{"n":1e+21}`},
		{`{"n":1e-4}`, `{"n":0.0001}`},
		{`{"n":1e-5}`, `{"n":1e-05}`},
		{`{"n":5e-324}`, `{"n":5e-324}`},
		{`{"n":1.7976931348623157e308}`, `{"n":1.7976931348623157e+308}`},
		{`{"n":-2.5e-3}`, `{"n":-0.0025}`},
		{`{"n":100.0}`, `{"n":100.0}`},
		{`{"n":-100.0}`, `{"n":-100.0}`},
	}
	for _, tc := range cases {
		t.Run(tc.literal, func(t *testing.T) {
			if got := canonicalOf(t, tc.literal); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestCanonicalDigestIsSha256OfTheBytes(t *testing.T) {
	decoder := json.NewDecoder(strings.NewReader(`{"b":2,"a":"x<y & z"}`))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	encoded, err := CanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := CanonicalDigest(value)
	if err != nil {
		t.Fatal(err)
	}
	if digest != sha256HexForTest(encoded) {
		t.Fatalf("digest %s does not match the canonical bytes", digest)
	}
	if len(digest) != 64 {
		t.Fatalf("digest length %d", len(digest))
	}
}

func TestCanonicalIsDeterministic(t *testing.T) {
	literal := `{"z":[1,2.5,"日本"],"a":{"b":null},"m":1e16}`
	first := canonicalOf(t, literal)
	for i := 0; i < 5; i++ {
		if again := canonicalOf(t, literal); again != first {
			t.Fatalf("iteration %d differed: %s vs %s", i, again, first)
		}
	}
}

func TestCanonicalRejectsUnsupportedValues(t *testing.T) {
	if _, err := CanonicalJSON(map[string]any{"bad": make(chan int)}); err == nil {
		t.Fatal("an unsupported value type must be reported rather than silently encoded")
	}
}
