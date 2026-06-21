package validate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

var requiredFiles = []string{
	"SKILL.md",
	"README.md",
	"README_EN.md",
	"formal-gates.manifest.json",
	".github/workflows/portable-validation.yml",
	"agents/requirements-clarification-gate.md",
	"agents/qa-test-gate.md",
	"agents/complexity-gate.md",
	"agents/architecture-health-gate.md",
	"agents/code-quality-gate.md",
	"agents/cold-water-review.md",
	"references/requirements-clarification-gate.md",
	"references/requirements-clarification-artifacts.md",
	"references/requirement-document-adapters.md",
	"references/install-and-hooks.md",
	"references/qa-test-gate.md",
	"references/complexity-gate.md",
	"references/architecture-health-gate.md",
	"references/code-quality-gate.md",
	"references/post-development-artifacts.md",
	"scripts/gate-artifact-validation.ps1",
	"scripts/gate-proof-receipt.ps1",
	"scripts/gate-state.ps1",
	"scripts/gate-workflow.ps1",
	"scripts/validate-dispatch-prompt.ps1",
	"scripts/test-portable-openspec-canary.ps1",
	"hooks/enforce-gate-sequence.ps1",
	"hooks/capture-subagent-receipt.ps1",
	"hooks/test-subagent-receipt-canary-common.ps1",
	"hooks/test-claude-subagent-receipt-canary.ps1",
	"hooks/test-codex-subagent-receipt-canary.ps1",
	"hooks/test-cursor-subagent-receipt-canary.ps1",
	"hooks/pollution-patterns.json",
	"examples/skill-behavior-prompts.json",
	"examples/sample-complexity-gate-artifact.md",
}

var requiredDirs = []string{
	"agents",
	"examples",
	"hooks",
	"references",
	"scripts",
}

var requiredHosts = []string{
	"Claude Code",
	"Codex",
	"Cursor",
	"Gemini",
	"OpenCode",
	"Windsurf",
}

type manifest struct {
	Name       string         `json:"name"`
	Hosts      []manifestHost `json:"hosts"`
	Parts      []string       `json:"package_parts"`
	Commands   []string       `json:"verification_commands"`
	Caveats    []string       `json:"support_caveats"`
	Validators []any          `json:"external_validators"`
}

type manifestHost struct {
	Name         string            `json:"name"`
	Support      string            `json:"support"`
	Capabilities map[string]string `json:"capabilities"`
	Caveat       string            `json:"caveat"`
}

func Package(root string) Result {
	root = cleanRoot(root)
	var result Result

	for _, dir := range requiredDirs {
		path := filepath.Join(root, filepath.FromSlash(dir))
		if !isDir(path) {
			result.add(dir, "required package directory is missing")
		}
	}
	for _, file := range requiredFiles {
		path := filepath.Join(root, filepath.FromSlash(file))
		if !isFile(path) {
			result.add(file, "required package file is missing")
		}
	}

	validateSkillFrontmatter(root, &result)
	validateCI(root, &result)
	validateManifest(root, &result)
	validateExamples(root, &result)
	return result
}

func validateSkillFrontmatter(root string, result *Result) {
	path := filepath.Join(root, "SKILL.md")
	text, err := readText(path)
	if err != nil {
		result.add("SKILL.md", fmt.Sprintf("cannot read skill entrypoint: %v", err))
		return
	}
	if !strings.HasPrefix(text, "---\n") {
		result.add("SKILL.md", "frontmatter block is missing")
	}
	for _, required := range []string{"name: formal-gates", "description:", "# Formal Gates"} {
		if !strings.Contains(text, required) {
			result.add("SKILL.md", "missing required entrypoint text: "+required)
		}
	}
}

func validateCI(root string, result *Result) {
	path := filepath.Join(root, ".github", "workflows", "portable-validation.yml")
	text, err := readText(path)
	if err != nil {
		result.add(".github/workflows/portable-validation.yml", fmt.Sprintf("cannot read CI workflow: %v", err))
		return
	}
	for _, required := range []string{"windows-latest", "macos-latest", "ubuntu-latest", "go test ./...", "go run ./cmd/formal-gates-validate package --root ."} {
		if !strings.Contains(text, required) {
			result.add(".github/workflows/portable-validation.yml", "missing required CI validation text: "+required)
		}
	}
}

func validateManifest(root string, result *Result) {
	path := filepath.Join(root, "formal-gates.manifest.json")
	text, err := readText(path)
	if err != nil {
		result.add("formal-gates.manifest.json", fmt.Sprintf("cannot read manifest: %v", err))
		return
	}
	var doc manifest
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		result.add("formal-gates.manifest.json", fmt.Sprintf("manifest JSON is invalid: %v", err))
		return
	}
	if doc.Name != "formal-gates" {
		result.add("formal-gates.manifest.json", "manifest name must be formal-gates")
	}
	for _, part := range []string{"SKILL.md", "references/", "scripts/", "hooks/", "agents/", "examples/"} {
		if !contains(doc.Parts, part) {
			result.add("formal-gates.manifest.json", "package_parts missing "+part)
		}
	}
	if len(doc.Commands) == 0 {
		result.add("formal-gates.manifest.json", "verification_commands must include a repo-local command")
	}
	for _, host := range requiredHosts {
		found := findHost(doc.Hosts, host)
		if found == nil {
			result.add("formal-gates.manifest.json", "hosts missing "+host)
			continue
		}
		for _, key := range []string{"readable_skill_support", "install_guidance", "hook_configuration", "hook_blocking_live_canary"} {
			if strings.TrimSpace(found.Capabilities[key]) == "" {
				result.add("formal-gates.manifest.json", fmt.Sprintf("host %s missing capability %s", host, key))
			}
		}
		if strings.TrimSpace(found.Caveat) == "" {
			result.add("formal-gates.manifest.json", "host "+host+" missing caveat")
		}
		if strings.Contains(strings.ToLower(found.Capabilities["hook_blocking_live_canary"]), "proven") {
			result.add("formal-gates.manifest.json", "host "+host+" must not claim proven hook blocking without an evidence path")
		}
	}
	if !containsText(doc.Caveats, "live canary") {
		result.add("formal-gates.manifest.json", "support_caveats must preserve live canary wording")
	}
}

func validateExamples(root string, result *Result) {
	behaviorPath := filepath.Join(root, "examples", "skill-behavior-prompts.json")
	text, err := readText(behaviorPath)
	if err != nil {
		result.add("examples/skill-behavior-prompts.json", fmt.Sprintf("cannot read behavior prompts: %v", err))
	} else {
		var decoded any
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			result.add("examples/skill-behavior-prompts.json", fmt.Sprintf("invalid JSON: %v", err))
		}
	}

	samplePath := filepath.Join(root, "examples", "sample-complexity-gate-artifact.md")
	sample, err := readText(samplePath)
	if err != nil {
		result.add("examples/sample-complexity-gate-artifact.md", fmt.Sprintf("cannot read sample artifact: %v", err))
		return
	}
	if !strings.Contains(sample, "Sample-only") {
		result.add("examples/sample-complexity-gate-artifact.md", "sample artifact must clearly say it is not a formal PASS artifact")
	}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func containsText(values []string, expected string) bool {
	expected = strings.ToLower(expected)
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), expected) {
			return true
		}
	}
	return false
}

func findHost(hosts []manifestHost, name string) *manifestHost {
	for i := range hosts {
		if hosts[i].Name == name {
			return &hosts[i]
		}
	}
	return nil
}
