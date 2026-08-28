package validate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
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
	"internal/cli/cli.go",
	"internal/validate/catalog.go",
	"internal/validate/runner.go",
	"internal/validate/runstate.go",
	"internal/validate/install.go",
	"internal/validate/managed_rules.go",
	"internal/validate/workflow.go",
	"internal/validate/canary.go",
	"internal/host/host.go",
	"definitions/workflow.json",
	"internal/validate/codex_hook_canary.go",
	"internal/validate/hook.go",
	"agents/openai.yaml",
	"prompts/reviewer-base.md",
	"references/install-and-hooks.md",
	"references/local-validation.md",
	"references/vcs-snapshots.md",
}

var requiredDirs = []string{
	"agents",
	"prompts",
	"prompts/actions",
	"gates",
	"bin",
	"cmd",
	"internal",
	"references",
	"definitions",
}

var requiredHosts = []string{
	"Claude Code",
	"Codex",
	"Cursor",
	"DeepSeek Harness",
	"ZCode",
	"Gemini",
	"OpenCode",
	"Windsurf",
}

var knownManifestParts = []string{
	"SKILL.md",
	"README.md",
	"README_EN.md",
	"formal-gates.manifest.json",
	"go.mod",
	".github/workflows/portable-validation.yml",
	"bin/",
	"references/",
	"cmd/",
	"internal/",
	"agents/",
	"prompts/",
	"gates/",
	"definitions/",
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
	root = lifecycle.CleanRoot(root)
	var result Result
	// A package is an immutable input to installation.  Walk it with Lstat and
	// reject symlink entries before any normal content checks so prompts/gates
	// cannot silently point back to a mutable development worktree.
	if _, err := PackageReceipt(root); err != nil {
		result.add("package", fmt.Sprintf("immutable package validation failed: %v", err))
	}

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
	validatePromptCatalog(root, &result)
	validateManagedRuleSource(root, &result)
	return result
}

func validatePromptCatalog(root string, result *Result) {
	if _, err := LoadPromptCatalog(root); err != nil {
		result.add("prompts/gates", err.Error())
	}
}

func validateManagedRuleSource(root string, result *Result) {
	if _, err := LoadManagedRule(root); err != nil {
		result.add(managedRuleSourceRelativePath, err.Error())
	}
}

func validateNativeBinary(root string, result *Result) {
	rel := filepath.ToSlash(filepath.Join("bin", nativeBinaryName()))
	path := filepath.Join(root, filepath.FromSlash(rel))
	if !isFile(path) {
		result.add(rel, "built native CLI binary is missing; build ./cmd/formal-gates before package validation")
		return
	}
	if err := validateBinaryFormat(path); err != nil {
		result.add(rel, err.Error())
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
		"git archive --format=",
		"FORMAL_GATES_ARCHIVE_ROOT",
		"package validate --root",
		"canary portable --root",
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
	powershellPath := filepath.Join(root, "install.ps1")
	batchPath := filepath.Join(root, "install.bat")
	if !isFile(bashPath) && !isFile(powershellPath) && !isFile(batchPath) {
		return
	}

	if !isFile(batchPath) {
		result.add("install.bat", "bootstrap script set is incomplete")
	} else {
		batch, err := readText(batchPath)
		if err != nil {
			result.add("install.bat", fmt.Sprintf("cannot read bootstrap script: %v", err))
		} else {
			for _, required := range []string{
				"powershell",
				"-File",
				`%~dp0install.ps1`,
				"exit /b %ERRORLEVEL%",
			} {
				if !strings.Contains(batch, required) {
					result.add("install.bat", "bootstrap script is not bound to PowerShell bootstrap entrypoint: "+required)
				}
			}
		}
	}

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
			`--release-root "$install_root"`,
			`--binary-target "$binary_target"`,
			"--bootstrap",
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
		for _, forbidden := range []string{"ln -s", "ln -sf", "rm -rf \"$install_root\"", "rm -rf \"$release"} {
			if strings.Contains(bash, forbidden) {
				result.add("install.command", "bootstrap script must not mutate a live release pointer directly: "+forbidden)
			}
		}
	}

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
			`"--release-root", $installRoot`,
			`"--binary-target", $formalBinary`,
			`"--bootstrap"`,
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
		for _, forbidden := range []string{"SymbolicLink", "Remove-Item $installRoot", "Remove-Item $current"} {
			if strings.Contains(powershell, forbidden) {
				result.add("install.ps1", "bootstrap script must not mutate a live release pointer directly: "+forbidden)
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
	for _, host := range unknownManifestHosts(doc.Hosts) {
		result.add("formal-gates.manifest.json", fmt.Sprintf("manifest lists unsupported host target %q", host))
	}
	for _, part := range unknownManifestParts(doc.Parts) {
		result.add("formal-gates.manifest.json", fmt.Sprintf("package_parts lists unsupported target %q", part))
	}
	for _, part := range knownManifestParts {
		if !contains(doc.Parts, part) {
			result.add("formal-gates.manifest.json", "package_parts missing "+part)
		}
	}
	if len(doc.Commands) == 0 {
		result.add("formal-gates.manifest.json", "verification_commands must include a repo-local command")
	}
	if !contains(doc.Installs, nativeInstallCommandExample()) {
		result.add("formal-gates.manifest.json", "install_commands must include the release bootstrap entry")
	}
	if !contains(doc.Commands, nativeBinaryCommand()) {
		result.add("formal-gates.manifest.json", "verification_commands must include the built native binary package validation command")
	}
	if contains(doc.Commands, "go run ./cmd/formal-gates package validate --root .") {
		result.add("formal-gates.manifest.json", "verification_commands must not use go run as the installed/package validation proof")
	}
	if !containsText(doc.Notes, "prompt catalog") {
		result.add("formal-gates.manifest.json", "validation_notes must mention the prompt catalog")
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

func unknownManifestParts(parts []string) []string {
	unknown := []string{}
	for _, part := range parts {
		if !contains(knownManifestParts, part) {
			unknown = append(unknown, part)
		}
	}
	return unknown
}

// manifestHostTargetNames derives the manifest target names from the finite
// host registry so adding a host does not require a second production list.
func manifestHostTargetNames() map[string]string {
	names := make(map[string]string)
	for _, descriptor := range host.All() {
		if descriptor.Installable {
			names[descriptor.InstallName] = descriptor.ManifestName
		}
	}
	return names
}

// unregisteredManifestInstallHosts returns the manifest host names the
// requested install targets need but the payload manifest does not register
// with support "host-target". An install into such a host is an unknown
// target for this payload and must be rejected before any target or state is
// created.
func unregisteredManifestInstallHosts(hosts []manifestHost, targets []installTarget) []string {
	var unregistered []string
	seen := map[string]bool{}
	manifestNames := manifestHostTargetNames()
	for _, target := range targets {
		manifestName, known := manifestNames[target.host]
		if !known || seen[manifestName] {
			continue
		}
		seen[manifestName] = true
		found := findHost(hosts, manifestName)
		if found == nil || !strings.EqualFold(strings.TrimSpace(found.Support), "host-target") {
			unregistered = append(unregistered, manifestName)
		}
	}
	return unregistered
}

func nativeBinaryCommand() string {
	if runtime.GOOS == "windows" {
		return "bin\\formal-gates.exe package validate --root ."
	}
	return "bin/formal-gates package validate --root ."
}

func nativeInstallCommandExample() string {
	if runtime.GOOS == "windows" {
		return "install.bat -TargetHost claude -Scope global -Force"
	}
	return "./install.command --host claude --scope global --force"
}

func nativeBinaryName() string {
	if runtime.GOOS == "windows" {
		return "formal-gates.exe"
	}
	return "formal-gates"
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

func unknownManifestHosts(hosts []manifestHost) []string {
	known := make(map[string]bool, len(requiredHosts))
	for _, host := range requiredHosts {
		known[host] = true
	}
	unknown := []string{}
	for _, host := range hosts {
		if !known[host.Name] {
			unknown = append(unknown, host.Name)
		}
	}
	return unknown
}
