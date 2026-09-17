package harness

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

// rep builds a string of n copies, since Go has no "c"*n string operator.
func rep(s string, n int) string { return strings.Repeat(s, n) }

// planArtifact decodes a JSON payload the way the bridge would and runs
// ArtifactPlan, returning canonical text and ref.
func planArtifact(t *testing.T, payload string) (string, string) {
	t.Helper()
	var q artifactPlanInput
	if err := StrictDecode([]byte(payload), &q); err != nil {
		t.Fatalf("StrictDecode payload: %v", err)
	}
	value, err := decodeValue(q.Value)
	if err != nil {
		t.Fatalf("decodeValue: %v", err)
	}
	canonical, ref, err := ArtifactPlan(value)
	if err != nil {
		t.Fatalf("ArtifactPlan: %v", err)
	}
	return string(canonical), ref
}

func TestArtifactPlanReferenceMatchesSha256(t *testing.T) {
	canonical, ref := planArtifact(t, `{"value":{"result":"PASS"}}`)
	sum := sha256.Sum256([]byte(canonical))
	want := hex.EncodeToString(sum[:])
	if ref != want {
		t.Fatalf("ref %q != sha256(canonical) %q", ref, want)
	}
	if len(ref) != 64 {
		t.Fatalf("ref must be 64 hex chars, got %d: %q", len(ref), ref)
	}
}

func TestArtifactPlanUnicodeNested(t *testing.T) {
	canonical, ref := planArtifact(t, `{"value":{"text":"日本語","n":[1,"café"]}}`)
	// Recompute independently to confirm stability.
	sum := sha256.Sum256([]byte(canonical))
	if ref != hex.EncodeToString(sum[:]) {
		t.Fatalf("ref mismatch")
	}
	// The canonical text must be Python's sort_keys form (ensure_ascii=False).
	if canonical != `{"n":[1,"café"],"text":"日本語"}` {
		t.Fatalf("canonical wrong: %s", canonical)
	}
}

func TestArtifactPlanNumericFidelity(t *testing.T) {
	_, refInt := planArtifact(t, `{"value":{"x":7}}`)
	_, refFloat := planArtifact(t, `{"value":{"x":7.0}}`)
	if refInt == refFloat {
		t.Fatalf("7 and 7.0 must produce distinct references: %s", refInt)
	}
}

func TestArtifactPlanEmptyValue(t *testing.T) {
	canonical, ref := planArtifact(t, `{"value":{}}`)
	if canonical != "{}" {
		t.Fatalf("empty object canonical wrong: %s", canonical)
	}
	sum := sha256.Sum256([]byte(canonical))
	if ref != hex.EncodeToString(sum[:]) {
		t.Fatalf("ref mismatch")
	}
}

func TestArtifactPlanNonFiniteRejectedAtDecode(t *testing.T) {
	// The non-finite refusal originates at decode (UseNumber), matching the
	// pre-migration canonical call. The transport carries the raw NaN / Infinity
	// token, which Go's scanner rejects before the canonical encoder runs. The
	// full bridge path (StrictDecode of the envelope) rejects the same way; this
	// unit test pins the value-level decode that artifact-plan relies on.
	for _, lit := range []string{`{"x":NaN}`, `{"x":Infinity}`} {
		if _, err := decodeValue([]byte(lit)); err == nil {
			t.Fatalf("expected decode rejection for %s", lit)
		}
	}
}

// F2: VerifyArtifactReference must accept exactly the 64-lower-case-hex form.
func TestVerifyArtifactReferenceValid(t *testing.T) {
	ok, refusal := VerifyArtifactReference("a" + rep("0", 62) + "f")
	if !ok {
		t.Fatalf("valid reference refused: %q", refusal)
	}
}

func TestVerifyArtifactReferenceMalformed(t *testing.T) {
	cases := []string{
		"",
		"a" + rep("0", 62),       // 63 chars
		"a" + rep("0", 64),       // 65 chars
		"A" + rep("0", 62) + "f", // 64 chars, uppercase
		"g" + rep("0", 62) + "f", // 64 chars, non-hex
		"a a" + rep("0", 61),     // 64 chars, contains a space
		"é" + rep("0", 62),       // 64 bytes, multi-byte start
		rep("0", 62) + "é",       // 64 bytes, multi-byte tail
	}
	for _, ref := range cases {
		ok, refusal := VerifyArtifactReference(ref)
		if ok {
			t.Fatalf("ref %q should be refused", ref)
		}
		if refusal != "invalid artifact reference" {
			t.Fatalf("ref %q wrong refusal wording: %q", ref, refusal)
		}
	}
}

// F3: the multi-byte equivalence proof. Python counts code points; Go counts
// bytes. For 'é'*32 (32 code points, 64 bytes) Python's length test fails while
// Go's rune whitelist fails; for 'a'*62+'é' (63 code points, 64 bytes) Python's
// length test fails while Go's length passes but the rune whitelist fails. Both
// always refuse — the verdict is identical, never divergent.
func TestVerifyArtifactReferenceMultibyteEquivalence(t *testing.T) {
	multibyte := []string{
		rep("é", 32),       // 32 code points / 64 bytes, not hex
		rep("a", 62) + "é", // 63 code points / 64 bytes, not hex
	}
	for _, ref := range multibyte {
		ok, refusal := VerifyArtifactReference(ref)
		if ok {
			t.Fatalf("multi-byte ref %q must be refused", ref)
		}
		if refusal != "invalid artifact reference" {
			t.Fatalf("multi-byte ref %q wrong refusal: %q", ref, refusal)
		}
	}
	// Sanity: a 64-ASCII-hex reference passes on both definitions.
	ok, _ := VerifyArtifactReference("a" + rep("0", 62) + "f")
	if !ok {
		t.Fatalf("64 ascii hex must pass")
	}
}

func TestVerifyArtifactBindingMatching(t *testing.T) {
	body := []byte(`{"result":"PASS"}`)
	sum := sha256.Sum256(body)
	ref := hex.EncodeToString(sum[:])
	ok, refusal := VerifyArtifactBinding(ref, body)
	if !ok {
		t.Fatalf("matching bytes refused: %q", refusal)
	}
}

func TestVerifyArtifactBindingMismatch(t *testing.T) {
	body := []byte(`{"result":"PASS"}`)
	other := []byte(`{"result":"FAIL"}`)
	sum := sha256.Sum256(body)
	ref := hex.EncodeToString(sum[:])
	ok, refusal := VerifyArtifactBinding(ref, other)
	if ok {
		t.Fatalf("mismatched bytes should be refused")
	}
	if refusal != "artifact integrity mismatch" {
		t.Fatalf("wrong refusal wording: %q", refusal)
	}
}

// F4: binding must NOT re-check the reference format. A non-hex (uppercase) ref
// with a mismatched body must report "artifact integrity mismatch", never
// "invalid artifact reference" — proving the two ops are separate.
func TestVerifyArtifactBindingDoesNotCheckFormat(t *testing.T) {
	body := []byte("x")
	ref := "A" + rep("0", 62) + "F" // invalid format (uppercase) but binding only compares
	ok, refusal := VerifyArtifactBinding(ref, body)
	if ok {
		t.Fatalf("mismatched body should be refused")
	}
	if refusal != "artifact integrity mismatch" {
		t.Fatalf("binding must not format-check; got %q", refusal)
	}
}

// F5: non-UTF-8 bytes reach the integrity rule as bytes, not text, so a tampered
// artifact yields an integrity refusal and never a UnicodeDecodeError.
func TestVerifyArtifactBindingNonUTF8IsIntegrityRefusal(t *testing.T) {
	bad := []byte{0xff, 0xfe, 0x00, 'n', 'o', 't'}
	sum := sha256.Sum256([]byte(`{"result":"PASS"}`))
	ref := hex.EncodeToString(sum[:])
	ok, refusal := VerifyArtifactBinding(ref, bad)
	if ok {
		t.Fatalf("tampered bytes should be refused")
	}
	if refusal != "artifact integrity mismatch" {
		t.Fatalf("non-utf8 must be integrity refusal, got %q", refusal)
	}
}

// F6: the bridge round-trips base64 bytes for binding.
func TestArtifactBindingBase64RoundTrip(t *testing.T) {
	body := []byte(`{"result":"PASS"}`)
	sum := sha256.Sum256(body)
	ref := hex.EncodeToString(sum[:])
	b64 := base64.StdEncoding.EncodeToString(body)
	// Mirror the dispatch path: decode base64, then bind.
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	ok, refusal := VerifyArtifactBinding(ref, decoded)
	if !ok {
		t.Fatalf("round-trip failed: %q", refusal)
	}
}

// F7: an invalid base64 body is a caller error and is rejected by the decoder,
// which the bridge turns into a bridge failure (exit 2), not a business refusal.
func TestArtifactBindingInvalidBase64RejectedByDecoder(t *testing.T) {
	if _, err := base64.StdEncoding.DecodeString("!!!not base64!!!"); err == nil {
		t.Fatalf("expected base64 decode failure")
	}
}
