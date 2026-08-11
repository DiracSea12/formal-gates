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

	"formal-gates/internal/lifecycle"
)

var promptIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

var requiredActionIDs = []string{"carry", "development-worker", "product-review", "qa-design", "qa-execution", "qa-review", "requirements-clarification", "start-readiness"}

// 内置 QA 模式与合并验证的保留 ID。它们不是门文件，而是 CLI 识别的内置条目：
// blackbox（黑盒，真实 QA：在 QA 隔离工作区按当前需求设计、快照后对主工作区已构建
// 产品实际使用验证）与 whitebox（白盒，开发后读实现设计的结构测试）是正常路线的可选
// QA 模式；merge-qa 与 merge-gate 是分片 >= 2 的保留总任务实例自动附加的合并后验证，
// 不进入正常路线选择列表。legacyQAID 是旧目录把 QA 作为门登记时的保留名，CLI 把它
// 当作内置 QA 模式识别，使旧目录绑定的 run 在迁移后仍被当作 QA 选中。
const (
	blackboxQAID = "blackbox"
	whiteboxQAID = "whitebox"
	mergeQAID    = "merge-qa"
	mergeGateID  = "merge-gate"
	legacyQAID   = "qa"
)

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
	root = lifecycle.CleanRoot(root)
	base, err := readPromptFile(filepath.Join(root, "prompts", "reviewer-base.md"))
	if err != nil {
		return PromptCatalog{}, fmt.Errorf("reviewer base: %w", err)
	}
	gates, err := discoverPromptDirectory(filepath.Join(root, "gates"))
	if err != nil {
		return PromptCatalog{}, fmt.Errorf("gate catalog: %w", err)
	}
	for _, gate := range gates {
		if gate.ID == "qa" || gate.ID == blackboxQAID || gate.ID == whiteboxQAID || gate.ID == mergeQAID {
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

// RouteCandidates 返回正常路线的候选项：黑盒 QA、白盒 QA 与除合并门外的全部已
// 发现门。合并门是条件自动门，只在分片 >= 2 的保留总任务实例中自动附加，不进入
// 正常选择列表；合并 QA 同理。
func (catalog PromptCatalog) RouteCandidates() []string {
	ids := []string{blackboxQAID, whiteboxQAID}
	for _, gate := range catalog.Gates {
		if gate.ID == mergeGateID {
			continue
		}
		ids = append(ids, gate.ID)
	}
	return ids
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

// composedActionPromptHash is the content hash of the catalog-dependent portion
// of a composed action prompt. Injected reviewer actions carry the
// shared reviewer base, so their hash includes the base — a base-only change
// moves every injected reviewer action's hash and enables the inheritance
// judgment, symmetric with the gate path; non-reviewer actions keep the plain
// content hash (their composed prompt is their own content only).
func composedActionPromptHash(catalog PromptCatalog, action PromptDefinition) string {
	if !isReviewerAction(action.ID) {
		return promptContentHash(action.Content)
	}
	h := sha256.New()
	hashPart(h, "base", catalog.Base)
	hashPart(h, "action", action.Content)
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
		hashes["action:"+action.ID] = composedActionPromptHash(catalog, action)
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
