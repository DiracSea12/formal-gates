package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
)

// artifactPlanInput is the payload for the artifact-plan bridge operation. The
// content-addressing rule (ref = sha256(CanonicalJSON(value))) moves here (M3);
// Python keeps the artifact path, the publish-once check and the atomic write.
// The value is decoded with number fidelity (decodeValue / UseNumber) so an
// integer literal stays an integer and the produced canonical bytes match the
// pre-migration hashlib.sha256(canonical(value)) byte for byte.
type artifactPlanInput struct {
	Value json.RawMessage `json:"value"`
}

// ArtifactPlan returns the canonical bytes for value and the content reference
// (sha256 of those bytes, lower-case hex). It reuses CanonicalJSON exactly, so
// the reference is identical to the pre-migration
// hashlib.sha256(canonical(value)).hexdigest(). A non-finite literal
// (NaN/Infinity) cannot be decoded by decodeValue, so the failure surfaces as a
// bridge error with the same message the pre-migration canonical call produced
// ("invalid character 'N' looking for beginning of value").
func ArtifactPlan(value any) (canonical []byte, ref string, err error) {
	encoded, err := CanonicalJSON(value)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(encoded)
	return encoded, hex.EncodeToString(sum[:]), nil
}

// VerifyArtifactReference enforces the reference FORMAT rule: a reference is
// exactly 64 lower-case hex ASCII characters. It mirrors the pre-migration
// `len(ref) != 64 or any(c not in "0123456789abcdef" for c in ref)` test.
//
// Equivalence with Python's code-point count: Go's len(ref) counts bytes, not
// runes. A string that passes BOTH checks here must be 64 ASCII hex characters,
// because the rune whitelist ("0123456789abcdef") contains only ASCII code
// points, each of which is a single byte in UTF-8. Therefore:
//
//	"Go accepts" ⇒ 64 ASCII bytes ⇒ 64 code points ⇒ "Python accepts"
//	"Python accepts" ⇒ 64 hex code points ⇒ 64 ASCII bytes ⇒ "Go accepts"
//
// The two predicates are equivalent: a multi-byte reference such as 'é'*32 (32
// code points / 64 bytes) is rejected by Python's length test and by Go's rune
// whitelist respectively, but always by exactly one of the two, so the verdict
// (accept vs refuse) is identical. See the unit and differential tests for the
// concrete proofs ('é'*32, 'a'*62 + 'é').
func VerifyArtifactReference(ref string) (bool, string) {
	if len(ref) != 64 {
		return false, "invalid artifact reference"
	}
	for _, c := range ref {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false, "invalid artifact reference"
		}
	}
	return true, ""
}

// VerifyArtifactBinding enforces the integrity rule: the sha256 of the supplied
// bytes must equal the reference. It does NOT re-check the reference format —
// the format rule is owned by VerifyArtifactReference / the artifact-reference
// call — so the refusal ordering (format → read → integrity) is structural
// rather than argued. Bytes, never text: a non-UTF-8 tampered artifact yields
// an integrity refusal instead of a UnicodeDecodeError.
func VerifyArtifactBinding(ref string, body []byte) (bool, string) {
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != ref {
		return false, "artifact integrity mismatch"
	}
	return true, ""
}

// VerifyArtifactBindingFile applies the integrity rule to the bytes of the
// artifact file at path, which Go reads itself.
//
// The rule is applied to the file's bytes rather than to bytes carried in the
// request because the request envelope has a fixed input ceiling shared by every
// operation (Bridge). Carrying the body across it made the READ capacity smaller
// than the WRITE capacity by the transport's own expansion factor, so an
// artifact this version had published and referenced could not be read back by
// the same version. Reading the file keeps the request independent of the
// artifact's size, so read capacity covers whatever publication accepts.
//
// The caller keeps its own read of the same path: that read is what produces the
// value returned to the caller and the OS error classes (FileNotFoundError,
// IsADirectoryError) the pre-migration store raised. This function therefore
// reports an unreadable file as an integrity refusal — the reference is not
// confirmed as bound to a readable body — instead of inventing an OS error class
// of its own. The artifact directory is publish-once (Store.put never overwrites
// an existing file), so the two reads see the same bytes.
func VerifyArtifactBindingFile(ref, path string) (bool, string) {
	body, err := os.ReadFile(path)
	if err != nil {
		return false, "artifact integrity mismatch"
	}
	return VerifyArtifactBinding(ref, body)
}
