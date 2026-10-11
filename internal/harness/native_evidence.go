package harness

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/worker"
	"os"
	"path/filepath"
	"strings"
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
	result := worker.ResultSource{Answer: answer, Digest: digest, Identity: identity}
	for _, fact := range facts {
		result.LocationFacts = append(result.LocationFacts, worker.LocationFact{Kind: fact.Kind, Paths: fact.Paths, Content: fact.Content})
	}
	return result, nil
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
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return nil, err
	}
	if materials, _ := getOf(request, "materials").(string); materials != "" {
		relative, err := filepath.Rel(context.Workspace, resolveNativePath(materials))
		if err != nil {
			return nil, err
		}
		// Comparison exports use <side>/<original project path>.
		for i, fact := range context.Facts {
			for j, path := range fact.Paths {
				for _, side := range []string{"base", "candidate"} {
					prefix := filepath.Join(relative, "comparison", side) + string(filepath.Separator)
					if strings.HasPrefix(path, prefix) {
						context.Facts[i].Paths[j] = strings.TrimPrefix(path, prefix)
					}
				}
			}
		}
	}
	return context.Facts, nil
}
