package harness

import (
	"encoding/json"
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
// ordered slice rather than a map so that, when several proofs are wrong, the
// refusal names the same one the reference implementation named: it iterated the
// parsed document in insertion order, and a Go map has no order at all.
type Proof struct {
	Name     string
	Expected map[string]any
}

// VerifyProcess checks that a job's recorded receipt still describes the request
// it was launched with, and that the stream evidence it names is intact.
//
// The caller reads the two JSON documents and reports the proof order: reading
// files is the wrapper's job, and the rules below are the evidence contract.
// Numbers are compared by value rather than by spelling, matching the reference
// implementation, where 1 and 1.0 are the same number.
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

// VerifyFiles checks an ordered set of named files against recorded proofs. The
// names are used as given - they may be absolute - and the label prefixes the
// refusal the way the evidence families in this harness already word it.
func VerifyFiles(files []Proof, label string) (string, string) {
	for _, file := range files {
		refusal, matched := proofMatches(file.Name, file.Expected)
		if refusal != "" {
			return KindMissing, refusal
		}
		if !matched {
			return KindChanged, "EVIDENCE_CHANGED: " + label + file.Name
		}
	}
	return "", ""
}

// proofMatches reports either a refusal to propagate (the file cannot be read)
// or whether the recorded proof matches. A content mismatch is not a read
// failure, and the two produce different refusals.
func proofMatches(path string, expected map[string]any) (string, bool) {
	actual, refusal := FileProof(path)
	if refusal != "" {
		return refusal, false
	}
	sum, ok := expected["sha256"].(string)
	if !ok || sum != actual.SHA256 {
		return "", false
	}
	size, ok := asInt64(expected["size"])
	if !ok || size != int64(actual.Size) {
		return "", false
	}
	return "", true
}

func asInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	case float64:
		return int64(typed), true
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	}
	return 0, false
}

// valuesEqual compares two decoded JSON values, treating two numeric literals as
// equal when they denote the same number.
func valuesEqual(a, b any) bool {
	switch left := a.(type) {
	case json.Number:
		right, ok := b.(json.Number)
		if !ok {
			return false
		}
		return numbersEqual(left, right)
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

func numbersEqual(a, b json.Number) bool {
	left, ok := new(big.Rat).SetString(a.String())
	if !ok {
		return a.String() == b.String()
	}
	right, ok := new(big.Rat).SetString(b.String())
	if !ok {
		return a.String() == b.String()
	}
	return left.Cmp(right) == 0
}
