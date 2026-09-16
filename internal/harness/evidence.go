package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// ContentProof is the fixed shape of an evidence proof. It is a struct rather
// than a map because the shape is part of the evidence contract: exactly two
// fields, neither of them optional.
type ContentProof struct {
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
}

// FileProof reads a file and reports its content proof: the SHA-256 of its
// bytes and its size.
//
// This is the content-addressing primitive the harness uses to bind evidence:
// native executable bindings, job stream logs, saved native source files and
// handover bundles all record this shape and re-check it later. It is a
// read-only computation; it never writes, follows no policy and knows nothing
// about which evidence family a call belongs to.
//
// The refusal text is observable behaviour and is reproduced verbatim: the
// caller supplies the path exactly as it wants it reported, so path formatting
// stays with the caller and this function only reports failures.
func FileProof(path string) (ContentProof, string) {
	body, err := os.ReadFile(path)
	if err != nil {
		return ContentProof{}, "EVIDENCE_MISSING: " + path
	}
	sum := sha256.Sum256(body)
	return ContentProof{SHA256: hex.EncodeToString(sum[:]), Size: len(body)}, ""
}
