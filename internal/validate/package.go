package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

var requiredFiles = []string{
	"SKILL.md",
	"README.md",
	"README_EN.md",
	"formal-gates.manifest.json",
	"go.mod",
	".github/workflows/portable-validation.yml",
	"cmd/formal-gates/main.go",
	"cmd/formal-gates-validate/main.go",
	"internal/cli/cli.go",
	"internal/validate/dispatch_prompt.go",
	"internal/validate/gate_state.go",
	"internal/validate/install.go",
	"internal/validate/behavior.go",
	"internal/validate/receipt.go",
	"internal/validate/workflow.go",
	"internal/validate/canary.go",
	"internal/validate/codex_hook_canary.go",
	"internal/validate/complexity.go",
	"internal/validate/handoff.go",
	"agents/requirements-clarification-gate.md",
	"agents/qa-test-gate.md",
	"agents/complexity-gate.md",
	"agents/development-worker.md",
	"agents/anti-complexity-review.md",
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
	"hooks/pollution-patterns.json",
	"assets/showcase/no-evidence-no-pass.svg",
	"examples/skill-behavior-prompts.json",
	"examples/skill-behavior-answers.json",
	"examples/sample-complexity-gate-artifact.md",
}

var requiredDirs = []string{
	"agents",
	"examples",
	"hooks",
	"bin",
	"assets",
	"cmd",
	"internal",
	"references",
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
	Installs   []string       `json:"install_commands"`
	Commands   []string       `json:"verification_commands"`
	Notes      []string       `json:"validation_notes"`
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
	validateNativeBinary(root, &result)
	validateCI(root, &result)
	validateBootstrapScripts(root, &result)
	validateManifest(root, &result)
	validateExamples(root, &result)
	validateNoCoreScriptRuntime(root, &result)
	return result
}

func validateNativeBinary(root string, result *Result) {
	rel := filepath.ToSlash(filepath.Join("bin", nativeBinaryName()))
	if !isFile(filepath.Join(root, filepath.FromSlash(rel))) {
		result.add(rel, "built native CLI binary is missing; build ./cmd/formal-gates before package validation")
	}
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
	validateCIText(text, result)
	workflow, err := parseCIWorkflow(text)
	if err != nil {
		result.add(".github/workflows/portable-validation.yml", fmt.Sprintf("cannot parse CI workflow permissions: %v", err))
		return
	}
	validateCIStructure(workflow, result)
}

func validateCIText(text string, result *Result) {
	for _, required := range []string{
		"windows-latest",
		"macos-arm64",
		"macos-amd64",
		"ubuntu-latest",
		"go test ./...",
		"go build -o",
		"package validate --root .",
		"canary portable --root .",
		"behavior evaluate --root .",
		"examples/skill-behavior-answers.json",
		"portable-canary.json",
		"portable-canary-windows-amd64.json",
		"portable-canary-macos-arm64.json",
		"portable-canary-macos-amd64.json",
		"portable-canary-linux-amd64.json",
		"SHA256SUMS",
		"SHA256SUMS-windows-amd64.txt",
		"SHA256SUMS-macos-arm64.txt",
		"SHA256SUMS-macos-amd64.txt",
		"SHA256SUMS-linux-amd64.txt",
		"actions/upload-artifact",
		"actions/download-artifact",
		"gh release upload",
	} {
		if !strings.Contains(text, required) {
			result.add(".github/workflows/portable-validation.yml", "missing required CI validation text: "+required)
		}
	}
	for _, required := range []string{
		"suffix: macos-arm64",
		"suffix: macos-amd64",
		"binary: formal-gates-macos-arm64",
		"binary: formal-gates-macos-amd64",
		"portable-canary-macos-arm64.json",
		"portable-canary-macos-amd64.json",
		"SHA256SUMS-macos-arm64.txt",
		"SHA256SUMS-macos-amd64.txt",
	} {
		if !strings.Contains(text, required) {
			result.add(".github/workflows/portable-validation.yml", "missing required macOS matrix text: "+required)
		}
	}
	if strings.Contains(text, "go run ./cmd/formal-gates package validate --root .") {
		result.add(".github/workflows/portable-validation.yml", "package validation must run the built native binary, not go run")
	}
	if !strings.Contains(text, "bin") || !strings.Contains(text, "formal-gates.exe") || !strings.Contains(text, "formal-gates") {
		result.add(".github/workflows/portable-validation.yml", "CI must validate with bin/formal-gates(.exe)")
	}
}

func validateCIStructure(workflow ciWorkflow, result *Result) {
	if !workflow.Events["release"] {
		result.add(".github/workflows/portable-validation.yml", "workflow must run on GitHub Release events")
	}
	if !hasExactContentsPermission(workflow.Permissions, "read") {
		result.add(".github/workflows/portable-validation.yml", "workflow-level contents permission must be read-only")
	}
	for name, job := range workflow.Jobs {
		if name != "release-evidence" && grantsContentsWrite(job.Permissions) {
			result.add(".github/workflows/portable-validation.yml", "only the release evidence job may request contents: write")
		}
	}
	releaseJob, ok := workflow.Jobs["release-evidence"]
	if !ok {
		result.add(".github/workflows/portable-validation.yml", "release evidence job is missing")
		return
	}
	if !hasExactContentsPermission(releaseJob.Permissions, "write") {
		result.add(".github/workflows/portable-validation.yml", "release evidence job must carry its own contents: write permission")
	}
	if !contains(releaseJob.Needs, "go-validation") {
		result.add(".github/workflows/portable-validation.yml", "release evidence job must depend on go-validation")
	}
	if !isReleaseEventCondition(releaseJob.Condition) {
		result.add(".github/workflows/portable-validation.yml", "release evidence job must be limited to the release event")
	}
}

func validateBootstrapScripts(root string, result *Result) {
	bashPath := filepath.Join(root, "install.command")
	bash, err := readText(bashPath)
	if err != nil {
		result.add("install.command", fmt.Sprintf("cannot read bootstrap script: %v", err))
	} else {
		for _, required := range []string{
			`Darwin) os="macos" ;;`,
			"macos-arm64|macos-amd64|linux-amd64",
			`binary="formal-gates-${suffix}"`,
			`canary="portable-canary-${suffix}.json"`,
			`checksums="SHA256SUMS-${suffix}.txt"`,
		} {
			if !strings.Contains(bash, required) {
				result.add("install.command", "bootstrap script is not bound to release asset contract: "+required)
			}
		}
		for _, forbidden := range []string{`os="darwin"`, "linux-arm64", "windows-arm64"} {
			if strings.Contains(bash, forbidden) {
				result.add("install.command", "bootstrap script references unpublished release suffix: "+forbidden)
			}
		}
	}

	powershellPath := filepath.Join(root, "install.ps1")
	powershell, err := readText(powershellPath)
	if err != nil {
		result.add("install.ps1", fmt.Sprintf("cannot read bootstrap script: %v", err))
	} else {
		for _, required := range []string{
			`$suffix -ne "windows-amd64"`,
			`$asset = "formal-gates-$suffix.exe"`,
			`$canary = "portable-canary-$suffix.json"`,
			`$checksums = "SHA256SUMS-$suffix.txt"`,
			`foreach ($file in @($asset, $canary))`,
			`throw "checksum validation failed: $file"`,
		} {
			if !strings.Contains(powershell, required) {
				result.add("install.ps1", "bootstrap script is not bound to release asset contract: "+required)
			}
		}
		for _, forbidden := range []string{"windows-arm64", "linux-arm64", "darwin"} {
			if strings.Contains(strings.ToLower(powershell), forbidden) {
				result.add("install.ps1", "bootstrap script references unpublished release suffix: "+forbidden)
			}
		}
	}
}

type ciWorkflow struct {
	Permissions map[string]string
	Jobs        map[string]ciJob
	Events      map[string]bool
}

type ciJob struct {
	Permissions map[string]string
	Needs       []string
	Condition   string
}

func parseCIWorkflow(text string) (ciWorkflow, error) {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(text), &root); err != nil {
		return ciWorkflow{}, err
	}
	if len(root.Content) == 0 {
		return ciWorkflow{}, fmt.Errorf("empty workflow")
	}
	doc := root.Content[0]
	workflow := ciWorkflow{Jobs: map[string]ciJob{}, Events: map[string]bool{}}
	if node := yamlMappingValue(doc, "on"); node != nil {
		workflow.Events = parseEventsNode(node)
	}
	if node := yamlMappingValue(doc, "permissions"); node != nil {
		workflow.Permissions = parsePermissionsNode(node)
	}
	jobs := yamlMappingValue(doc, "jobs")
	if jobs == nil {
		return workflow, nil
	}
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		name := jobs.Content[i].Value
		jobNode := jobs.Content[i+1]
		if jobNode.Kind != yaml.MappingNode {
			continue
		}
		workflow.Jobs[name] = parseJobNode(jobNode)
	}
	return workflow, nil
}

func parseEventsNode(node *yaml.Node) map[string]bool {
	events := map[string]bool{}
	switch node.Kind {
	case yaml.ScalarNode:
		events[node.Value] = true
	case yaml.SequenceNode:
		for _, item := range node.Content {
			events[item.Value] = true
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			events[node.Content[i].Value] = true
		}
	}
	return events
}

func parseJobNode(node *yaml.Node) ciJob {
	job := ciJob{}
	if permissions := yamlMappingValue(node, "permissions"); permissions != nil {
		job.Permissions = parsePermissionsNode(permissions)
	}
	if needs := yamlMappingValue(node, "needs"); needs != nil {
		job.Needs = parseNeedsNode(needs)
	}
	if condition := yamlMappingValue(node, "if"); condition != nil {
		job.Condition = strings.TrimSpace(condition.Value)
	}
	return job
}

func yamlMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func parsePermissionsNode(node *yaml.Node) map[string]string {
	permissions := map[string]string{}
	switch node.Kind {
	case yaml.ScalarNode:
		permissions["*"] = strings.ToLower(node.Value)
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := strings.ToLower(node.Content[i].Value)
			value := strings.ToLower(node.Content[i+1].Value)
			permissions[key] = value
		}
	}
	return permissions
}

func parseNeedsNode(node *yaml.Node) []string {
	switch node.Kind {
	case yaml.ScalarNode:
		return []string{node.Value}
	case yaml.SequenceNode:
		needs := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			needs = append(needs, item.Value)
		}
		return needs
	}
	return nil
}

func hasExactContentsPermission(permissions map[string]string, expected string) bool {
	return len(permissions) == 1 && permissions["contents"] == expected
}

func grantsContentsWrite(permissions map[string]string) bool {
	return permissions["contents"] == "write" || permissions["*"] == "write-all"
}

func isReleaseEventCondition(condition string) bool {
	normalized := strings.ReplaceAll(condition, " ", "")
	return normalized == "github.event_name=='release'" || normalized == "${{github.event_name=='release'}}"
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
	for _, part := range []string{"SKILL.md", "README.md", "README_EN.md", "formal-gates.manifest.json", "go.mod", ".github/workflows/portable-validation.yml", "bin/", "assets/", "references/", "cmd/", "internal/", "hooks/", "agents/", "examples/"} {
		if !contains(doc.Parts, part) {
			result.add("formal-gates.manifest.json", "package_parts missing "+part)
		}
	}
	if len(doc.Commands) == 0 {
		result.add("formal-gates.manifest.json", "verification_commands must include a repo-local command")
	}
	if !contains(doc.Installs, nativeInstallCommandExample()) {
		result.add("formal-gates.manifest.json", "install_commands must include the native install command")
	}
	if !contains(doc.Commands, nativeBinaryCommand()) {
		result.add("formal-gates.manifest.json", "verification_commands must include the built native binary package validation command")
	}
	if contains(doc.Commands, "go run ./cmd/formal-gates package validate --root .") {
		result.add("formal-gates.manifest.json", "verification_commands must not use go run as the installed/package validation proof")
	}
	if !containsText(doc.Notes, "final-verification") {
		result.add("formal-gates.manifest.json", "validation_notes must mention native final-verification foundation")
	}
	if !containsText(doc.Notes, "cleanup") {
		result.add("formal-gates.manifest.json", "validation_notes must mention native cleanup foundation")
	}
	if !containsText(doc.Notes, "receipt foundation") {
		result.add("formal-gates.manifest.json", "validation_notes must mention native receipt foundation")
	}
	if !containsText(doc.Notes, "native install") {
		result.add("formal-gates.manifest.json", "validation_notes must mention native install")
	}
	if !containsText(doc.Notes, "native canary") {
		result.add("formal-gates.manifest.json", "validation_notes must mention native canary")
	}
	if !containsText(doc.Notes, "hook canary") {
		result.add("formal-gates.manifest.json", "validation_notes must mention native hook canary boundary")
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

func nativeBinaryCommand() string {
	if runtime.GOOS == "windows" {
		return "bin\\formal-gates.exe package validate --root ."
	}
	return "bin/formal-gates package validate --root ."
}

func nativeInstallCommandExample() string {
	if runtime.GOOS == "windows" {
		return "bin\\formal-gates.exe install --source . --host claude --scope global --force"
	}
	return "bin/formal-gates install --source . --host claude --scope global --force"
}

func nativeBinaryName() string {
	if runtime.GOOS == "windows" {
		return "formal-gates.exe"
	}
	return "formal-gates"
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
		} else if cases, ok := decoded.([]any); ok {
			for i, raw := range cases {
				item, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if _, ok := item["must_include"].([]any); !ok {
					result.add("examples/skill-behavior-prompts.json", fmt.Sprintf("case %d missing must_include markers", i))
				}
				if _, ok := item["must_avoid"].([]any); !ok {
					result.add("examples/skill-behavior-prompts.json", fmt.Sprintf("case %d missing must_avoid markers", i))
				}
			}
		}
	}
	behaviorReport, behaviorResult := Behavior(BehaviorOptions{
		Root:        root,
		CasesFile:   "examples/skill-behavior-prompts.json",
		AnswersFile: "examples/skill-behavior-answers.json",
	})
	if !behaviorResult.OK() {
		for _, failure := range behaviorResult.Failures {
			result.add("examples/skill-behavior-answers.json", failure.Path+": "+failure.Message)
		}
	} else if behaviorReport.Summary.Total == 0 || behaviorReport.Summary.Pass != behaviorReport.Summary.Total {
		result.add("examples/skill-behavior-answers.json", fmt.Sprintf("behavior answers must pass every case; total=%d pass=%d pending=%d fail=%d", behaviorReport.Summary.Total, behaviorReport.Summary.Pass, behaviorReport.Summary.Pending, behaviorReport.Summary.Fail))
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

func validateNoCoreScriptRuntime(root string, result *Result) {
	scriptDir := filepath.Join(root, "scripts")
	if isDir(scriptDir) {
		result.add("scripts", "core script runtime directory must not exist in the native package")
	}
	for _, rel := range []string{"hooks", "examples", "tests", "scripts"} {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		if !isDir(dir) {
			continue
		}
		_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				result.add(path, err.Error())
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			if isScriptRuntimeExtension(entry.Name()) {
				result.add(relativePath(root, path), "native package must not keep script runtime files")
			}
			return nil
		})
	}
	for _, rel := range []string{
		"examples/package-validation-demo.ps1",
		"tests/test-validate-dispatch-prompt.ps1",
		"hooks/enforce-gate-sequence.ps1",
		"hooks/capture-subagent-receipt.ps1",
	} {
		if isFile(filepath.Join(root, filepath.FromSlash(rel))) {
			result.add(rel, "native package must not keep replaced script runtime file")
		}
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
