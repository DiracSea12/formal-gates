package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var promptIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

var requiredActionIDs = []string{"carry", "development-worker", "qa-design", "qa-execution", "qa-review", "requirements-clarification", "start-readiness"}

type PromptDefinition struct {
	ID      string `json:"id"`
	Content string `json:"-"`
}

type PromptCatalog struct {
	Base            string             `json:"-"`
	BaseRevision    string             `json:"baseRevision"`
	CatalogRevision string             `json:"catalogRevision"`
	Gates           []PromptDefinition `json:"gates"`
	Actions         []PromptDefinition `json:"actions"`
}

func LoadPromptCatalog(root string) (PromptCatalog, error) {
	root = cleanRoot(root)
	base, err := readPromptFile(filepath.Join(root, "prompts", "reviewer-base.md"))
	if err != nil {
		return PromptCatalog{}, fmt.Errorf("reviewer base: %w", err)
	}
	gates, err := discoverPromptDirectory(filepath.Join(root, "gates"), true)
	if err != nil {
		return PromptCatalog{}, fmt.Errorf("gate catalog: %w", err)
	}
	if len(gates) == 0 {
		return PromptCatalog{}, fmt.Errorf("gate catalog: no gate prompt files found")
	}
	for _, gate := range gates {
		if gate.ID == "qa" {
			return PromptCatalog{}, fmt.Errorf("gate catalog: gate id %q is reserved for built-in QA", gate.ID)
		}
	}
	actions, err := discoverPromptDirectory(filepath.Join(root, "prompts", "actions"), false)
	if err != nil {
		return PromptCatalog{}, fmt.Errorf("action prompts: %w", err)
	}
	if err := validateActionCatalog(actions); err != nil {
		return PromptCatalog{}, fmt.Errorf("action prompts: %w", err)
	}

	baseHash := sha256.Sum256([]byte(base))
	h := sha256.New()
	hashPart(h, "base", base)
	for _, action := range actions {
		hashPart(h, "action:"+action.ID, action.Content)
	}
	for _, gate := range gates {
		hashPart(h, "gate:"+gate.ID, gate.Content)
	}
	return PromptCatalog{
		Base:            base,
		BaseRevision:    hex.EncodeToString(baseHash[:]),
		CatalogRevision: hex.EncodeToString(h.Sum(nil)),
		Gates:           gates,
		Actions:         actions,
	}, nil
}

func validateActionCatalog(actions []PromptDefinition) error {
	ids := make([]string, 0, len(actions))
	for _, action := range actions {
		ids = append(ids, action.ID)
	}
	if len(ids) != len(requiredActionIDs) {
		return fmt.Errorf("expected actions %s; found %s", strings.Join(requiredActionIDs, ", "), strings.Join(ids, ", "))
	}
	for index, id := range requiredActionIDs {
		if ids[index] != id {
			return fmt.Errorf("expected actions %s; found %s", strings.Join(requiredActionIDs, ", "), strings.Join(ids, ", "))
		}
	}
	return nil
}

func (catalog PromptCatalog) Gate(id string) (PromptDefinition, bool) {
	for _, gate := range catalog.Gates {
		if gate.ID == id {
			return gate, true
		}
	}
	return PromptDefinition{}, false
}

func (catalog PromptCatalog) Action(id string) (PromptDefinition, bool) {
	for _, action := range catalog.Actions {
		if action.ID == id {
			return action, true
		}
	}
	return PromptDefinition{}, false
}

func (catalog PromptCatalog) GateIDs() []string {
	ids := make([]string, 0, len(catalog.Gates))
	for _, gate := range catalog.Gates {
		ids = append(ids, gate.ID)
	}
	return ids
}

func discoverPromptDirectory(dir string, rejectEveryInvalidEntry bool) ([]PromptDefinition, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	definitions := make([]PromptDefinition, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			if rejectEveryInvalidEntry || strings.HasSuffix(name, ".md") {
				return nil, fmt.Errorf("%s is not a direct regular file", name)
			}
			continue
		}
		if filepath.Ext(name) != ".md" {
			if rejectEveryInvalidEntry {
				return nil, fmt.Errorf("invalid gate filename %q", name)
			}
			continue
		}
		id := strings.TrimSuffix(name, ".md")
		if !promptIDPattern.MatchString(id) {
			return nil, fmt.Errorf("invalid prompt filename %q", name)
		}
		content, err := readPromptFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		definitions = append(definitions, PromptDefinition{ID: id, Content: content})
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].ID < definitions[j].ID })
	return definitions, nil
}

func readPromptFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("content is not valid UTF-8")
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", fmt.Errorf("content is empty")
	}
	return content, nil
}

func hashPart(h hash.Hash, name, content string) {
	fmt.Fprintf(h, "%d:%s\n%d:%s\n", len(name), name, len(content), content)
}
