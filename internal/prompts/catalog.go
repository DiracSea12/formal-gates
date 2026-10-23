package prompts

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"formal-gates/internal/lifecycle"

	"gopkg.in/yaml.v3"
)

var IDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

var RequiredActionIDs = []string{"development-plan", "development-worker", "product-review", "qa-design", "qa-execution", "qa-review", "start-readiness", "validation-observation"}

var roleRuleIDs = []string{"gate", "qa", "qa-design-blackbox", "qa-design-whitebox", "qa-execution-blackbox", "qa-execution-whitebox", "qa-review-blackbox", "qa-review-whitebox"}

// 内置 QA 模式与合并验证的保留 ID。它们不是门文件，而是 CLI 识别的内置条目：
// blackbox（黑盒，真实 QA：在 QA 隔离工作区按当前需求设计、快照后对主工作区已构建
// 产品实际使用验证）与 whitebox（白盒，开发后读实现设计的结构测试）是正常路线的可选
// QA 模式；merge-qa 与 merge-gate 是分片 >= 2 的保留总任务实例自动附加的合并后验证，
// 不进入正常路线选择列表。
const (
	BlackboxQAID = "blackbox"
	WhiteboxQAID = "whitebox"
	MergeQAID    = "merge-qa"
	MergeGateID  = "merge-gate"
)

type PromptDefinition struct {
	ID          string
	Content     string
	Description string
}

type PromptCatalog struct {
	Base      string
	Gates     []PromptDefinition
	Actions   []PromptDefinition
	RoleRules []PromptDefinition
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
		if gate.ID == "qa" || gate.ID == BlackboxQAID || gate.ID == WhiteboxQAID || gate.ID == MergeQAID {
			return PromptCatalog{}, fmt.Errorf("gate catalog: gate id %q is reserved for built-in QA", gate.ID)
		}
	}
	actions, err := discoverPromptDirectory(filepath.Join(root, "prompts", "actions"))
	if err != nil {
		return PromptCatalog{}, fmt.Errorf("action prompts: %w", err)
	}
	if err := validatePromptIDs("actions", actions, RequiredActionIDs); err != nil {
		return PromptCatalog{}, fmt.Errorf("action prompts: %w", err)
	}
	roleRules, err := discoverPromptDirectory(filepath.Join(root, "prompts", "roles"))
	if err != nil {
		return PromptCatalog{}, fmt.Errorf("role rules: %w", err)
	}
	if err := validatePromptIDs("role rules", roleRules, roleRuleIDs); err != nil {
		return PromptCatalog{}, fmt.Errorf("role rules: %w", err)
	}

	return PromptCatalog{
		Base:      base,
		Gates:     gates,
		Actions:   actions,
		RoleRules: roleRules,
	}, nil
}

func PackageRouteCandidates(root string) ([]string, error) {
	catalog, err := LoadPromptCatalog(root)
	if err != nil {
		return nil, err
	}
	return catalog.RouteCandidates(), nil
}

func validatePromptIDs(kind string, definitions []PromptDefinition, expected []string) error {
	ids := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		ids = append(ids, definition.ID)
	}
	if len(ids) != len(expected) {
		return fmt.Errorf("expected %s %s; found %s", kind, strings.Join(expected, ", "), strings.Join(ids, ", "))
	}
	for index, id := range expected {
		if ids[index] != id {
			return fmt.Errorf("expected %s %s; found %s", kind, strings.Join(expected, ", "), strings.Join(ids, ", "))
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

func (catalog PromptCatalog) RoleRule(id string) (PromptDefinition, bool) {
	for _, rules := range catalog.RoleRules {
		if rules.ID == id {
			return rules, true
		}
	}
	return PromptDefinition{}, false
}

// RouteCandidates 返回正常路线的候选项：黑盒 QA、白盒 QA 与除合并门外的全部已
// 发现门。合并门是条件自动门，只在分片 >= 2 的保留总任务实例中自动附加，不进入
// 正常选择列表；合并 QA 同理。
func (catalog PromptCatalog) RouteCandidates() []string {
	ids := []string{BlackboxQAID, WhiteboxQAID}
	for _, gate := range catalog.Gates {
		if gate.ID == MergeGateID {
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
		if !IDPattern.MatchString(id) {
			return nil, fmt.Errorf("invalid prompt filename %q", name)
		}
		content, err := readPromptFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		description, err := promptDescription(content)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		definitions = append(definitions, PromptDefinition{ID: id, Content: content, Description: description})
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].ID < definitions[j].ID })
	return definitions, nil
}

func promptDescription(content string) (string, error) {
	frontmatter, present := strings.CutPrefix(strings.ReplaceAll(content, "\r\n", "\n"), "---\n")
	if !present {
		return "", nil
	}
	lines := strings.Split(frontmatter, "\n")
	for index, line := range lines {
		if line != "---" {
			continue
		}
		var document yaml.Node
		if err := yaml.Unmarshal([]byte(strings.Join(lines[:index], "\n")), &document); err != nil {
			return "", fmt.Errorf("frontmatter: %w", err)
		}
		if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
			return "", nil
		}
		var metadata map[string]yaml.Node
		if err := document.Decode(&metadata); err != nil {
			return "", fmt.Errorf("frontmatter: %w", err)
		}
		node, found := metadata["简介"]
		if !found {
			node = metadata["description"]
		}
		var description string
		if err := node.Decode(&description); err != nil {
			return "", fmt.Errorf("frontmatter description: %w", err)
		}
		return strings.TrimSpace(description), nil
	}
	return "", nil
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
