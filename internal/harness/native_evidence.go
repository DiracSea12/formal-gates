package harness

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/worker"
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
	proof := getOf(getOf(source["files"], nativeToolCallsFile), "sha256")
	digest, ok := proof.(string)
	if !ok || digest == "" {
		return nil, "", "", fmt.Errorf("native result original output proof is missing")
	}
	answer, err := submittedSemantics(directory)
	return answer, "sha256:" + digest, textOf(source["identity"]), err
}

// NativeWorkerResultEvidence carries only facts covered by the original source.
func NativeWorkerResultEvidence(provider, directory string, source map[string]any) (worker.ResultSource, error) {
	answer, digest, identity, err := NativeResultEvidence(provider, directory, source)
	if err != nil {
		return worker.ResultSource{}, err
	}
	facts, err := nativeToolFacts(directory)
	if err != nil {
		return worker.ResultSource{}, err
	}
	return worker.ResultSource{Answer: answer, Digest: digest, Identity: identity, LocationFacts: workerLocationFacts(facts)}, nil
}

func workerLocationFacts(facts []nativeToolFact) []worker.LocationFact {
	var result []worker.LocationFact
	for _, fact := range facts {
		result = append(result, worker.LocationFact{Kind: fact.Kind, Paths: fact.Paths, Content: fact.Content})
	}
	return result
}

func nativeToolFacts(directory string) ([]nativeToolFact, error) {
	body, err := os.ReadFile(filepath.Join(directory, nativeToolContextFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var context nativeToolContext
	if err := json.Unmarshal(body, &context); err != nil {
		return nil, err
	}
	return context.Facts, nil
}
