package facade

import (
	"bytes"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
)

// Only material selected for the current action is published. File references
// are handed directly to the worker; there is no extra prompt or index document.
func (m *nativePromptMaterial) bindFiles(job string, requirement protocol.RequirementDocument, root string) error {
	if m.files == nil {
		m.files = map[string][]byte{}
	}
	if _, ok := m.input["requirement"]; ok {
		m.files["requirement.md"] = []byte(requirement.Text)
		m.input["requirement"] = map[string]any{"path": filepath.Join(job, "requirement.md"), "revision": requirement.Revision, "confirmed": requirement.Confirmed, "referenceBase": filepath.Dir(filepath.Join(root, requirement.Path))}
	}
	for key, name := range map[string]string{
		"developmentIncrement": "development-increment.json",
		"design":               "design.json", "previousDesign": "previous-design.json",
		"previousDevelopmentResult": "previous-development.json",
		"committedHandoffs":         "dependency-handoffs.json",
		"existingTestEvidence":      "existing-test-evidence.json",
		"reviewGaps":                "qa-review.json",
		"retainedReview":            "qa-review.json",
	} {
		if _, already := m.files[name]; already {
			continue
		}
		value, ok := m.input[key]
		if !ok {
			continue
		}
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if string(data) == "null" || string(data) == "{}" || string(data) == "[]" {
			delete(m.input, key)
			continue
		}
		m.files[name] = data
		m.input[key] = map[string]string{"path": filepath.Join(job, name)}
	}
	for name := range m.files {
		if filepath.Base(name) != name || name == "." {
			return fmt.Errorf("ACTION_MATERIAL_PATH: invalid file %q", name)
		}
	}
	m.input = resolveActionFileReferences(m.input, job, m.files).(map[string]any)
	m.input["workspace"] = m.workspace
	return nil
}
func resolveActionFileReferences(value any, job string, files map[string][]byte) any {
	switch v := value.(type) {
	case string:
		if _, ok := files[v]; ok {
			return filepath.Join(job, v)
		}
	case map[string]any:
		for k, x := range v {
			v[k] = resolveActionFileReferences(x, job, files)
		}
	case map[string]string:
		for k, x := range v {
			if _, ok := files[x]; ok {
				v[k] = filepath.Join(job, x)
			}
		}
	case []any:
		for i, x := range v {
			v[i] = resolveActionFileReferences(x, job, files)
		}
	}
	return value
}
func (m nativePromptMaterial) writeFiles(job string) error {
	for name, data := range m.files {
		path := filepath.Join(job, name)
		old, err := os.ReadFile(path)
		if err == nil {
			if !bytes.Equal(old, data) {
				return fmt.Errorf("INTENT_MISMATCH: action material differs: %s", name)
			}
			continue
		}
		if !os.IsNotExist(err) {
			return err
		}
		if err = os.WriteFile(path, data, 0600); err != nil {
			return err
		}
	}
	return nil
}

func (m *nativePromptMaterial) bindPublicSources(job string, sources []SubagentSource) {
	contracts := []map[string]string{}
	for i, source := range sources {
		name := fmt.Sprintf("public-source-%d.txt", i)
		m.files[name] = []byte(source.Content)
		contracts = append(contracts, map[string]string{"kind": source.Kind, "path": filepath.Join(job, name)})
	}
	if len(contracts) > 0 {
		m.input["publicContracts"] = contracts
	}
}
