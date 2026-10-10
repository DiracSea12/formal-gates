package harness

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/engine/worker"
)

const nativePathsFile = "native-paths.json"

type nativePathFacts struct {
	RequestDigest string   `json:"requestDigest"`
	RawDigest     string   `json:"rawDigest"`
	Workspace     string   `json:"workspace"`
	Paths         []string `json:"paths"`
}

// Native terminal facts describe the complete workspace diff. The engine's
// existing quiescent workspace boundary owns the combined delivery source.
func nativePathRequest(directory string) (map[string]any, bool, error) {
	value, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	request := objectOrEmpty(value)
	return request, getOf(request, "engine") != nil && getOf(request, "read_only") == false, nil
}

func nativePathEvidence(directory, raw string, capture bool) (*nativePathFacts, error) {
	request, writable, err := nativePathRequest(directory)
	if err != nil || !writable {
		return nil, err
	}
	requestProof, err := fileProofOf(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return nil, err
	}
	rawSum := sha256.Sum256([]byte(raw))
	expected := nativePathFacts{RequestDigest: textOf(requestProof["sha256"]), RawDigest: fmt.Sprintf("%x", rawSum), Workspace: textOf(getOf(request, "workspace"))}
	path := filepath.Join(directory, nativePathsFile)
	body, err := os.ReadFile(path)
	if err == nil {
		var facts nativePathFacts
		if err := json.Unmarshal(body, &facts); err != nil {
			return nil, err
		}
		if facts.RequestDigest != expected.RequestDigest || facts.RawDigest != expected.RawDigest || facts.Workspace != expected.Workspace || facts.Paths == nil {
			return nil, fmt.Errorf("native terminal path facts differ from original request/output")
		}
		return &facts, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	if !capture {
		return nil, fmt.Errorf("original native terminal path facts are missing")
	}
	if _, err := os.Stat(filepath.Join(directory, "exit.json")); err == nil {
		return nil, fmt.Errorf("terminated native job lacks original terminal path facts")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	process, err := readJSONFile(filepath.Join(directory, "process.json"))
	if err != nil {
		return nil, err
	}
	owned := intOf(getOf(process, "pid")) == os.Getpid()
	if !owned && intOf(getOf(process, "pid")) == os.Getppid() && supervisorAlive(directory) {
		outer, err := readJSONFile(filepath.Join(directory, "request.json"))
		if err != nil {
			return nil, err
		}
		argv := extractArgs(getOf(outer, "argv"))
		owned = len(argv) == 4 && argv[1] == "harness" && argv[2] == "native" && resolveNativePath(argv[3]) == resolveNativePath(directory)
	}
	if !owned {
		return nil, fmt.Errorf("native terminal path capture requires the original live execution owner")
	}
	materialPath := textOf(getOf(getOf(getOf(request, "engine"), "workerMaterial"), "path"))
	materialBody, err := os.ReadFile(materialPath)
	if err != nil {
		return nil, err
	}
	var material worker.Material
	if err := json.Unmarshal(materialBody, &material); err != nil {
		return nil, err
	}
	input := material.Input
	if input.Validation == nil || input.Validation.Workspace == nil || input.VCS == "" || filepath.Clean(input.Validation.Workspace.Path) != filepath.Clean(expected.Workspace) {
		return nil, fmt.Errorf("native terminal lacks its issued VCS workspace")
	}
	resource := input.Validation.Workspace
	expected.Paths, err = vcs.NewNative(resource.Path, input.VCS, nil).At(resource.Path, resource.Handle).ChangedPaths()
	if err != nil {
		return nil, err
	}
	if expected.Paths == nil {
		expected.Paths = []string{}
	}
	if err := writeAtomicJSON(path, expected); err != nil {
		return nil, err
	}
	return &expected, nil
}

func nativeEvidenceFiles(directory string) ([]string, error) {
	_, writable, err := nativePathRequest(directory)
	if err != nil {
		return nil, err
	}
	files := append([]string{}, nativeSourceFiles...)
	if writable {
		files = append(files, nativePathsFile)
	}
	toolFiles, err := nativeToolEvidenceFiles(directory)
	if err != nil {
		return nil, err
	}
	files = append(files, toolFiles...)
	return files, nil
}
