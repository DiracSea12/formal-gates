package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func decodeForTest(t *testing.T, text string) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode %s: %v", text, err)
	}
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("not an object: %s", text)
	}
	return object
}

// fixture builds a job directory that looks like a published one.
func fixture(t *testing.T, exitCode any) (string, map[string]any, map[string]any, map[string]any, []Proof) {
	t.Helper()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "stdout.log"), []byte("stdout\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "stderr.log"), []byte("stderr\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _ := FileProof(filepath.Join(directory, "stdout.log"))
	stderr, _ := FileProof(filepath.Join(directory, "stderr.log"))
	request := decodeForTest(t, `{"kind":"check","attempt":0}`)
	digest, err := CanonicalDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	streams := map[string]any{
		"stdout.log": map[string]any{"sha256": stdout.SHA256, "size": json.Number(strconv.Itoa(stdout.Size))},
		"stderr.log": map[string]any{"sha256": stderr.SHA256, "size": json.Number(strconv.Itoa(stderr.Size))},
	}
	receipt := map[string]any{
		"exit_code":      exitCode,
		"error":          "",
		"request_digest": digest,
		"streams":        streams,
	}
	process := map[string]any{
		"operation":      "job-1",
		"directory":      directory,
		"exit_code":      exitCode,
		"error":          "",
		"request_digest": digest,
		"streams":        streams,
	}
	proofs := []Proof{
		{Name: "stderr.log", Expected: streams["stderr.log"].(map[string]any)},
		{Name: "stdout.log", Expected: streams["stdout.log"].(map[string]any)},
	}
	return directory, process, receipt, request, proofs
}

func TestVerifyProcessAcceptsAnIntactJob(t *testing.T) {
	directory, process, receipt, request, proofs := fixture(t, json.Number("0"))
	kind, refusal := VerifyProcess(directory, process, receipt, request, proofs)
	if kind != "" || refusal != "" {
		t.Fatalf("expected acceptance, got %s / %s", kind, refusal)
	}
}

func TestVerifyProcessNumbersCompareByValue(t *testing.T) {
	directory, process, receipt, request, proofs := fixture(t, json.Number("0"))
	// Different numeric spellings retain the same exit value: the process
	// carries exit_code 0 while the receipt carries 0.0.
	process["exit_code"] = json.Number("0")
	receipt["exit_code"] = json.Number("0.0")
	if kind, refusal := VerifyProcess(directory, process, receipt, request, proofs); refusal != "" {
		t.Fatalf("0 and 0.0 are the same number: %s / %s", kind, refusal)
	}
}

func TestVerifyProcessBindingRefusals(t *testing.T) {
	directory, process, receipt, request, proofs := fixture(t, json.Number("0"))

	t.Run("request digest differs", func(t *testing.T) {
		broken := decodeForTest(t, `{"kind":"check","attempt":7}`)
		kind, refusal := VerifyProcess(directory, process, receipt, broken, proofs)
		if kind != KindBinding || refusal != "EVIDENCE_BINDING: request digest differs" {
			t.Fatalf("%s / %s", kind, refusal)
		}
	})

	t.Run("field differs", func(t *testing.T) {
		moved := map[string]any{}
		for k, v := range process {
			moved[k] = v
		}
		moved["exit_code"] = json.Number("1")
		kind, refusal := VerifyProcess(directory, moved, receipt, request, proofs)
		if kind != KindBinding || refusal != "EVIDENCE_BINDING: process exit_code differs" {
			t.Fatalf("%s / %s", kind, refusal)
		}
	})
}

func TestVerifyProcessEvidenceRefusals(t *testing.T) {
	t.Run("proof set is wrong", func(t *testing.T) {
		directory, process, receipt, request, proofs := fixture(t, json.Number("0"))
		kind, refusal := VerifyProcess(directory, process, receipt, request, proofs[:1])
		if kind != KindMissing || refusal != "EVIDENCE_MISSING: stdout/stderr proofs absent" {
			t.Fatalf("%s / %s", kind, refusal)
		}
	})

	t.Run("stream changed", func(t *testing.T) {
		directory, process, receipt, request, proofs := fixture(t, json.Number("0"))
		if err := os.WriteFile(filepath.Join(directory, "stdout.log"), []byte("tampered\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		kind, refusal := VerifyProcess(directory, process, receipt, request, proofs)
		if kind != KindChanged || refusal != "EVIDENCE_CHANGED: stdout.log" {
			t.Fatalf("%s / %s", kind, refusal)
		}
	})

	t.Run("stream deleted names the path", func(t *testing.T) {
		directory, process, receipt, request, proofs := fixture(t, json.Number("0"))
		if err := os.Remove(filepath.Join(directory, "stdout.log")); err != nil {
			t.Fatal(err)
		}
		kind, refusal := VerifyProcess(directory, process, receipt, request, proofs)
		if kind != KindMissing {
			t.Fatalf("a deleted stream is missing evidence, not %s", kind)
		}
		if want := "EVIDENCE_MISSING: " + filepath.Join(directory, "stdout.log"); refusal != want {
			t.Fatalf("refusal %q want %q", refusal, want)
		}
	})
}

// TestVerifyProcessSizeComparesByValue checks that a proof's size is matched by
// exact numeric value, not by spelling. The streams are real 7-byte files; the
// recorded size is varied across representations that all denote 7 (accepted)
// and ones that denote a different value (rejected, never truncated).
func TestVerifyProcessSizeComparesByValue(t *testing.T) {
	directory, process, receipt, request, _ := fixture(t, json.Number("0"))
	stdout, _ := FileProof(filepath.Join(directory, "stdout.log"))
	stderr, _ := FileProof(filepath.Join(directory, "stderr.log"))
	proofsFor := func(size any) []Proof {
		return []Proof{
			{Name: "stderr.log", Expected: map[string]any{"sha256": stderr.SHA256, "size": size}},
			{Name: "stdout.log", Expected: map[string]any{"sha256": stdout.SHA256, "size": size}},
		}
	}

	accept := []any{
		json.Number("7"),
		json.Number("7.0"),
		json.Number("7e0"),
		float64(7.0),
		int(7),
	}
	for _, size := range accept {
		if kind, refusal := VerifyProcess(directory, process, receipt, request, proofsFor(size)); refusal != "" {
			t.Fatalf("size %#v should be accepted: %s / %s", size, kind, refusal)
		}
	}

	reject := []any{
		json.Number("7.75"),
		float64(7.75),
		float64(7.5),
		json.Number("8"),
	}
	for _, size := range reject {
		kind, refusal := VerifyProcess(directory, process, receipt, request, proofsFor(size))
		if kind != KindChanged || refusal != "EVIDENCE_CHANGED: stderr.log" {
			t.Fatalf("size %#v should be rejected as EVIDENCE_CHANGED: got %s / %s", size, kind, refusal)
		}
	}
}

// TestValuesEqualNumbersCompareByValue checks the exact-value comparison across
// numeric representations and that a number never equals a same-looking string.
func TestValuesEqualNumbersCompareByValue(t *testing.T) {
	equal := []struct{ a, b any }{
		{int(0), float64(0)},
		{json.Number("0"), json.Number("0.0")},
		{json.Number("1e0"), int(1)},
		{json.Number("7"), int(7)},
	}
	for _, tc := range equal {
		if !valuesEqual(tc.a, tc.b) {
			t.Fatalf("%#v should equal %#v", tc.a, tc.b)
		}
	}
	unequal := []struct{ a, b any }{
		{json.Number("7.75"), int(7)},
		{json.Number("7"), "7"},
	}
	for _, tc := range unequal {
		if valuesEqual(tc.a, tc.b) {
			t.Fatalf("%#v should not equal %#v", tc.a, tc.b)
		}
	}
}

func TestVerifyFiles(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "input.txt")
	if err := os.WriteFile(path, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	proof, _ := FileProof(path)
	intact := []Proof{{Name: path, Expected: map[string]any{
		"sha256": proof.SHA256, "size": json.Number(strconv.Itoa(proof.Size))}}}

	if kind, refusal := VerifyFiles(intact, "comparison input "); refusal != "" {
		t.Fatalf("expected acceptance: %s / %s", kind, refusal)
	}

	changed := []Proof{{Name: path, Expected: map[string]any{
		"sha256": "0000000000000000000000000000000000000000000000000000000000000000",
		"size":   json.Number("1")}}}
	kind, refusal := VerifyFiles(changed, "comparison input ")
	if kind != KindChanged || refusal != "EVIDENCE_CHANGED: comparison input "+path {
		t.Fatalf("%s / %s", kind, refusal)
	}

	absent := []Proof{{Name: filepath.Join(directory, "absent.txt"), Expected: map[string]any{
		"sha256": "0", "size": json.Number("0")}}}
	if kind, _ := VerifyFiles(absent, "comparison input "); kind != KindMissing {
		t.Fatalf("a missing file is missing evidence, got %s", kind)
	}
}
