package harness

import (
	"encoding/json"
	"math"
	"math/big"
	"path/filepath"
)

// Verification refusals are split into two families, because the caller maps
// them onto two different exception classes: binding failures are configuration
// errors, missing or changed evidence is a blocked run.
const (
	KindBinding = "binding"
	KindMissing = "missing"
	KindChanged = "changed"
)

// Proof names one recorded file proof. The caller supplies the proofs as an
// ordered slice so that the first failing proof follows the receipt's order.
type Proof struct {
	Name     string
	Expected map[string]any
}

// VerifyProcess checks that a job's recorded receipt still describes the request
// it was launched with, and that the stream evidence it names is intact.
//
// The caller reads the two JSON documents and reports the proof order: reading
// files is the wrapper's job, and the rules below are the evidence contract.
// Numbers are compared by value: 1 and 1.0 are the same number.
func VerifyProcess(directory string, process, receipt, request map[string]any, proofs []Proof) (string, string) {
	digest, err := CanonicalDigest(request)
	if err != nil {
		return KindBinding, "EVIDENCE_BINDING: request digest differs"
	}
	if text, ok := receipt["request_digest"].(string); !ok || text != digest {
		return KindBinding, "EVIDENCE_BINDING: request digest differs"
	}
	for _, field := range []string{"exit_code", "error", "request_digest", "streams"} {
		if !valuesEqual(receipt[field], process[field]) {
			return KindBinding, "EVIDENCE_BINDING: process " + field + " differs"
		}
	}
	names := map[string]bool{}
	for _, proof := range proofs {
		names[proof.Name] = true
	}
	if len(proofs) != 2 || !names["stdout.log"] || !names["stderr.log"] {
		return KindMissing, "EVIDENCE_MISSING: stdout/stderr proofs absent"
	}
	for _, proof := range proofs {
		refusal, matched := proofMatches(filepath.Join(directory, proof.Name), proof.Expected)
		if refusal != "" {
			// A stream that cannot be read at all is missing evidence, and the
			// refusal names the path rather than the stream.
			return KindMissing, refusal
		}
		if !matched {
			return KindChanged, "EVIDENCE_CHANGED: " + proof.Name
		}
	}
	return "", ""
}

// proofMatches reports either a refusal to propagate (the file cannot be read)
// or whether the recorded proof matches. A content mismatch is not a read
// failure, and the two produce different refusals.
//
// The size in a proof is compared by exact numeric value, not by spelling: the
// recorded size may arrive as a JSON integer, an integer-valued float
// (json.Number "7.0", float64(7.0)) or a literal (json.Number "7e0"), all of
// which denote the same byte count and must be accepted; a non-integer such as
// "7.75" or float64(7.75) denotes a different value and must be rejected rather
// than truncated. See exactRational for the single primitive that governs both
// this check and valuesEqual.
func proofMatches(path string, expected map[string]any) (string, bool) {
	actual, refusal := FileProof(path)
	if refusal != "" {
		return refusal, false
	}
	sum, ok := expected["sha256"].(string)
	if !ok || sum != actual.SHA256 {
		return "", false
	}
	want, ok := exactRational(expected["size"])
	if !ok {
		return "", false
	}
	if want.Cmp(new(big.Rat).SetInt64(int64(actual.Size))) != 0 {
		return "", false
	}
	return "", true
}

// exactRational converts any numeric value this package can receive into an
// exact rational. It reports false for a non-number, and for a non-finite
// float, which has no rational value.
func exactRational(value any) (*big.Rat, bool) {
	switch typed := value.(type) {
	case json.Number:
		rational := new(big.Rat)
		if _, ok := rational.SetString(typed.String()); !ok {
			return nil, false
		}
		return rational, true
	case int:
		return new(big.Rat).SetInt64(int64(typed)), true
	case int64:
		return new(big.Rat).SetInt64(typed), true
	case float64:
		if math.IsInf(typed, 0) || math.IsNaN(typed) {
			return nil, false
		}
		return new(big.Rat).SetFloat64(typed), true
	}
	return nil, false
}

// valuesEqual compares two decoded JSON values, treating two numeric literals as
// equal when they denote the same number.
func valuesEqual(a, b any) bool {
	// Numeric values compare by exact value across representations: a number on
	// the left must meet a number on the right, and the two rationals must be
	// equal. A non-number on the left keeps the structural branches below
	// (map/slice/nil/other), so bool and string equality are unchanged.
	if left, ok := exactRational(a); ok {
		right, ok := exactRational(b)
		if !ok {
			return false
		}
		return left.Cmp(right) == 0
	}
	switch left := a.(type) {
	case map[string]any:
		right, ok := b.(map[string]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for key, value := range left {
			other, present := right[key]
			if !present || !valuesEqual(value, other) {
				return false
			}
		}
		return true
	case []any:
		right, ok := b.([]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for index := range left {
			if !valuesEqual(left[index], right[index]) {
				return false
			}
		}
		return true
	case nil:
		return b == nil
	}
	return a == b
}
