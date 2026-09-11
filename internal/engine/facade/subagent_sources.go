package facade

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SubagentSource is a captured input, never a result or an authorization.
type SubagentSource struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Digest  string `json:"digest"`
	Content string `json:"content"`
}

func readSubagentSources(file, step string) ([]SubagentSource, error) {
	var refs []struct {
		Kind string `json:"kind"`
		Path string `json:"path"`
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &refs); err != nil {
		return nil, err
	}
	var sources []SubagentSource
	for _, ref := range refs {
		development := step == "development-worker" || step == "repair-worker"
		qa := step == "blackbox-design" || step == "blackbox-review" || step == "whitebox-design" || step == "whitebox-review"
		if !(ref.Kind == "public-contract" && (development || qa) || ref.Kind == "environment-observation" && development) {
			return nil, fmt.Errorf("SUBAGENT_SOURCE_SCOPE: %s cannot receive %s", step, ref.Kind)
		}
		path := ref.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(file), path)
		}
		path, err = filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		sources = append(sources, SubagentSource{Kind: ref.Kind, Path: path, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(content)), Content: string(content)})
	}
	return sources, nil
}
