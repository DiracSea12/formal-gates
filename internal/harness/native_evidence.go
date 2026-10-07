package harness

import (
	"fmt"
	"os"
	"path/filepath"
)

// NativeResultEvidence uses the native adapter's original source verifier.
// Parsed output and original output identity remain distinct evidence.
func NativeResultEvidence(provider, directory string, source map[string]any) ([]byte, string, string, error) {
	impl, err := nativeImplementationFor(provider)
	if err != nil {
		return nil, "", "", err
	}
	if err := impl.verifySource(directory, source); err != nil {
		return nil, "", "", err
	}
	if textOf(source["provider"]) != provider || textOf(source["identity"]) == "" {
		return nil, "", "", fmt.Errorf("native result provider/session is missing or different")
	}
	proof := getOf(getOf(source["files"], "answer.raw.txt"), "sha256")
	digest, ok := proof.(string)
	if !ok || digest == "" {
		return nil, "", "", fmt.Errorf("native result original output proof is missing")
	}
	answer, err := os.ReadFile(filepath.Join(directory, "answer.json"))
	return answer, "sha256:" + digest, textOf(source["identity"]), err
}
