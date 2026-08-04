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

var requiredActionIDs = []string{"carry", "development-worker", "product-review", "qa-design", "qa-execution", "qa-review", "requirements-clarification", "start-readiness"}

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
	gates, err := discoverPromptDirectory(filepath.Join(root, "gates"))
	if err != nil {
		return PromptCatalog{}, fmt.Errorf("gate catalog: %w", err)
	}
	for _, gate := range gates {
		if gate.ID == "qa" {
			return PromptCatalog{}, fmt.Errorf("gate catalog: gate id %q is reserved for built-in QA", gate.ID)
		}
	}
	actions, err := discoverPromptDirectory(filepath.Join(root, "prompts", "actions"))
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

func PackageRouteCandidates(root string) ([]string, error) {
	catalog, err := LoadPromptCatalog(root)
	if err != nil {
		return nil, err
	}
	return catalog.RouteCandidates(), nil
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

func (catalog PromptCatalog) RouteCandidates() []string {
	return append([]string{"qa"}, catalog.GateIDs()...)
}

func discoverPromptDirectory(dir string) ([]PromptDefinition, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	definitions := make([]PromptDefinition, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			if strings.HasSuffix(name, ".md") {
				return nil, fmt.Errorf("%s is not a direct regular file", name)
			}
			continue
		}
		if filepath.Ext(name) != ".md" {
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

func promptContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// composedGatePromptHash is the content hash of the catalog-dependent portion
// of a composed gate prompt: the shared reviewer base plus the gate's own
// content. A base-only change therefore moves every gate's hash and enables
// per-gate re-dispatch.
func composedGatePromptHash(catalog PromptCatalog, content string) string {
	h := sha256.New()
	hashPart(h, "base", catalog.Base)
	hashPart(h, "gate", content)
	return hex.EncodeToString(h.Sum(nil))
}

// catalogPromptHashes returns one content hash per catalog entry, keyed by
// qualified id ("base", "gate:<id>", "action:<id>") so that deltas can be
// computed per gate and per action. Gate entries hash the composed gate prompt
// (reviewer base plus gate content) so a base-only change is reported per gate.
func catalogPromptHashes(catalog PromptCatalog) map[string]string {
	hashes := map[string]string{"base": promptContentHash(catalog.Base)}
	for _, gate := range catalog.Gates {
		hashes["gate:"+gate.ID] = composedGatePromptHash(catalog, gate.Content)
	}
	for _, action := range catalog.Actions {
		hashes["action:"+action.ID] = promptContentHash(action.Content)
	}
	return hashes
}

// catalogDelta reports the qualified catalog ids whose recorded prompt content
// hash no longer matches the current catalog. Runs started before per-entry
// hashing have no recorded hashes; when their catalog revision moved, every
// current entry is reported as changed so the main agent can classify it.
func catalogDelta(state RunState, catalog PromptCatalog) []string {
	if state.PromptHashes == nil {
		if state.CatalogRevision == catalog.CatalogRevision && state.BasePromptRevision == catalog.BaseRevision {
			return nil
		}
		changed := make([]string, 0, len(catalog.Gates)+len(catalog.Actions)+1)
		for id := range catalogPromptHashes(catalog) {
			changed = append(changed, id)
		}
		sort.Strings(changed)
		return changed
	}
	current := catalogPromptHashes(catalog)
	changed := []string{}
	seen := map[string]bool{}
	for id, hash := range current {
		seen[id] = true
		if recorded, ok := state.PromptHashes[id]; !ok || recorded != hash {
			changed = append(changed, id)
		}
	}
	// Report recorded entries removed from the current catalog so a removed
	// selected gate is a recoverable delta rather than an invisible dead-end.
	for id := range state.PromptHashes {
		if !seen[id] {
			changed = append(changed, id)
		}
	}
	sort.Strings(changed)
	return changed
}
