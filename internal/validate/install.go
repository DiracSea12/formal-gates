package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type InstallOptions struct {
	Source         string
	Host           string
	Scope          string
	Project        string
	Force          bool
	ConfigureHooks bool
}

type InstallReport struct {
	Targets []InstallTargetReport `json:"targets"`
}

type InstallTargetReport struct {
	Host       string `json:"host"`
	TargetPath string `json:"targetPath"`
	HookConfig string `json:"hookConfig,omitempty"`
}

type installTarget struct {
	host       string
	targetPath string
	hookConfig string
}

var installRuntimeEntries = []string{
	"SKILL.md",
	"README.md",
	"README_EN.md",
	"formal-gates.manifest.json",
	"go.mod",
	".github/workflows/portable-validation.yml",
	"bin",
	"assets",
	"cmd",
	"internal",
	"agents",
	"examples",
	"references",
	"hooks/pollution-patterns.json",
}

func Install(options InstallOptions) (InstallReport, error) {
	source := cleanRoot(options.Source)
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return InstallReport{}, err
	}
	sourceAbs = filepath.Clean(sourceAbs)
	host, err := normalizeInstallHost(options.Host)
	if err != nil {
		return InstallReport{}, err
	}
	scope, err := normalizeInstallScope(options.Scope)
	if err != nil {
		return InstallReport{}, err
	}
	projectAbs := ""
	if scope == "project" || strings.TrimSpace(options.Project) != "" {
		if strings.TrimSpace(options.Project) == "" {
			return InstallReport{}, fmt.Errorf("--project is required when --scope project is used")
		}
		projectAbs, err = filepath.Abs(options.Project)
		if err != nil {
			return InstallReport{}, err
		}
		projectAbs = filepath.Clean(projectAbs)
	}

	if err := assertInstallSource(sourceAbs); err != nil {
		return InstallReport{}, err
	}

	targets, err := installTargets(host, scope, projectAbs)
	if err != nil {
		return InstallReport{}, err
	}

	report := InstallReport{}
	for _, target := range targets {
		if err := copyInstallRuntime(sourceAbs, target.targetPath, options.Force); err != nil {
			return InstallReport{}, err
		}
		targetReport := InstallTargetReport{
			Host:       target.host,
			TargetPath: filepath.ToSlash(target.targetPath),
		}
		if options.ConfigureHooks {
			receiptWorktree := "."
			if projectAbs != "" {
				receiptWorktree = projectAbs
			}
			if err := configureInstallHook(target, receiptWorktree); err != nil {
				return InstallReport{}, err
			}
			targetReport.HookConfig = filepath.ToSlash(target.hookConfig)
		}
		report.Targets = append(report.Targets, targetReport)
	}
	return report, nil
}

func normalizeInstallHost(host string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "claude":
		return "claude", nil
	case "codex":
		return "codex", nil
	case "cursor":
		return "cursor", nil
	case "both":
		return "both", nil
	default:
		return "", fmt.Errorf("unsupported --host %q (want claude, codex, cursor, or both)", host)
	}
}

func normalizeInstallScope(scope string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "global":
		return "global", nil
	case "project":
		return "project", nil
	default:
		return "", fmt.Errorf("unsupported --scope %q (want global or project)", scope)
	}
}

func assertInstallSource(source string) error {
	for _, entry := range installRuntimeEntries {
		if !exists(filepath.Join(source, filepath.FromSlash(entry))) {
			return fmt.Errorf("formal-gates source is incomplete; missing %s under %s", entry, source)
		}
	}
	binaryRel := filepath.Join("bin", nativeBinaryName())
	if !isFile(filepath.Join(source, binaryRel)) {
		return fmt.Errorf("formal-gates native binary is missing at %s; build it first with: go build -o %s ./cmd/formal-gates", filepath.Join(source, binaryRel), filepath.Join("bin", nativeBinaryName()))
	}
	return nil
}

func installTargets(host, scope, project string) ([]installTarget, error) {
	hosts := []string{host}
	if host == "both" {
		hosts = []string{"claude", "codex"}
	}
	home := ""
	if scope == "global" {
		var err error
		home, err = installHomeDir()
		if err != nil {
			return nil, err
		}
	}
	targets := make([]installTarget, 0, len(hosts))
	for _, h := range hosts {
		var base string
		var hookConfig string
		if scope == "global" {
			switch h {
			case "claude":
				base = filepath.Join(home, ".claude", "skills")
				hookConfig = filepath.Join(home, ".claude", "settings.json")
			case "codex":
				base = filepath.Join(home, ".codex", "skills")
				hookConfig = filepath.Join(home, ".codex", "hooks.json")
			case "cursor":
				base = filepath.Join(home, ".cursor")
				hookConfig = filepath.Join(home, ".cursor", "hooks.json")
			}
		} else {
			switch h {
			case "claude":
				base = filepath.Join(project, ".claude", "skills")
				hookConfig = filepath.Join(project, ".claude", "settings.json")
			case "codex":
				base = filepath.Join(project, ".codex", "skills")
				hookConfig = filepath.Join(project, ".codex", "hooks.json")
			case "cursor":
				base = filepath.Join(project, ".cursor")
				hookConfig = filepath.Join(project, ".cursor", "hooks.json")
			}
		}
		targets = append(targets, installTarget{
			host:       h,
			targetPath: filepath.Clean(filepath.Join(base, "formal-gates")),
			hookConfig: filepath.Clean(hookConfig),
		})
	}
	return targets, nil
}

func installHomeDir() (string, error) {
	for _, name := range []string{"HOME", "USERPROFILE"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			abs, err := filepath.Abs(value)
			if err != nil {
				return "", err
			}
			return filepath.Clean(abs), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve home directory: %w", err)
	}
	return filepath.Clean(home), nil
}

func copyInstallRuntime(source, target string, force bool) error {
	source = filepath.Clean(source)
	target = filepath.Clean(target)
	if samePath(source, target) {
		return nil
	}
	if exists(target) {
		if !force {
			return fmt.Errorf("target already exists: %s; re-run with --force to replace it", target)
		}
		if err := removeExistingInstallTarget(target); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return err
	}
	for _, entry := range installRuntimeEntries {
		from := filepath.Join(source, filepath.FromSlash(entry))
		to := filepath.Join(target, filepath.FromSlash(entry))
		if err := copyPath(from, to); err != nil {
			return err
		}
	}
	return removePycache(target)
}

func removeExistingInstallTarget(target string) error {
	target = filepath.Clean(target)
	leaf := filepath.Base(target)
	parentLeaf := filepath.Base(filepath.Dir(target))
	if leaf != "formal-gates" || (parentLeaf != "skills" && parentLeaf != ".cursor") {
		return fmt.Errorf("refusing to replace unexpected target path: %s", target)
	}
	return os.RemoveAll(target)
}

func copyPath(from, to string) error {
	info, err := os.Stat(from)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(from, to)
	}
	return copyFile(from, to, info.Mode())
}

func copyDir(from, to string) error {
	return filepath.WalkDir(from, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		if shouldSkipNativeInstallEntry(rel, entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(to, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func shouldSkipNativeInstallEntry(rel string, entry os.DirEntry) bool {
	if rel == "." {
		return false
	}
	name := strings.ToLower(entry.Name())
	if entry.IsDir() {
		return name == "__pycache__"
	}
	switch filepath.Ext(name) {
	case ".ps1", ".psm1", ".psd1", ".py", ".pyc", ".pyo", ".sh", ".bash", ".bat", ".cmd", ".js", ".mjs", ".cjs":
		return true
	default:
		return false
	}
}

func copyFile(from, to string, mode os.FileMode) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o600
	}
	return os.WriteFile(to, data, mode.Perm())
}

func removePycache(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == "__pycache__" {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			return filepath.SkipDir
		}
		return nil
	})
}

func configureInstallHook(target installTarget, receiptWorktree string) error {
	config, err := readHookConfig(target.hookConfig)
	if err != nil {
		return err
	}
	hooks := hookObject(config)
	gateCommand := nativeInstallCommand(target.targetPath, "hook", "decide")
	var desired map[string]any
	shape := "nested"
	switch target.host {
	case "claude":
		desired = map[string]any{
			"PreToolUse":    nestedHookEntry("*", gateCommand, false),
			"SubagentStart": nestedHookEntry("*", nativeReceiptCommand(target.targetPath, "claude-code", "SubagentStart", receiptWorktree), false),
			"SubagentStop":  nestedHookEntry("*", nativeReceiptCommand(target.targetPath, "claude-code", "SubagentStop", receiptWorktree), false),
		}
	case "codex":
		desired = map[string]any{
			"PreToolUse":    nestedHookEntry("*", gateCommand, true),
			"SubagentStart": nestedHookEntry("*", nativeReceiptCommand(target.targetPath, "codex", "SubagentStart", receiptWorktree), true),
			"SubagentStop":  nestedHookEntry("*", nativeReceiptCommand(target.targetPath, "codex", "SubagentStop", receiptWorktree), true),
		}
	case "cursor":
		shape = "flat"
		config["version"] = float64(1)
		desired = map[string]any{
			"preToolUse":    flatHookEntry(gateCommand),
			"subagentStart": flatHookEntry(nativeReceiptCommand(target.targetPath, "cursor", "SubagentStart", receiptWorktree)),
			"subagentStop":  flatHookEntry(nativeReceiptCommand(target.targetPath, "cursor", "SubagentStop", receiptWorktree)),
		}
	}
	for event, entry := range desired {
		existing, _ := hooks[event].([]any)
		hooks[event] = append(removeFormalGatesHookEntries(existing, shape), entry)
	}
	for event, value := range hooks {
		if _, ok := desired[event]; ok {
			continue
		}
		existing, ok := value.([]any)
		if !ok {
			continue
		}
		hooks[event] = removeFormalGatesHookEntries(existing, shape)
	}
	config["hooks"] = hooks
	return writeHookConfig(target.hookConfig, config)
}

func readHookConfig(path string) (map[string]any, error) {
	if !isFile(path) {
		return map[string]any{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return map[string]any{}, nil
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("existing hook config is not valid JSON; refusing to touch it: %s", path)
	}
	return config, nil
}

func writeHookConfig(path string, config map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if isFile(path) {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path+".bak", data, 0o600); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func hookObject(config map[string]any) map[string]any {
	hooks, ok := config["hooks"].(map[string]any)
	if !ok {
		hooks = map[string]any{}
	}
	return hooks
}

func nestedHookEntry(matcher, command string, timeout bool) map[string]any {
	hook := map[string]any{
		"type":    "command",
		"command": command,
	}
	if timeout {
		hook["timeout"] = float64(30)
	}
	return map[string]any{
		"matcher": matcher,
		"hooks":   []any{hook},
	}
}

func flatHookEntry(command string) map[string]any {
	return map[string]any{
		"command":    command,
		"timeout":    float64(30),
		"failClosed": true,
	}
}

func removeFormalGatesHookEntries(entries []any, shape string) []any {
	kept := make([]any, 0, len(entries))
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			kept = append(kept, entry)
			continue
		}
		if shape == "nested" {
			nested, ok := entryMap["hooks"].([]any)
			if ok {
				remaining := make([]any, 0, len(nested))
				for _, hook := range nested {
					if !isFormalGatesHook(hook) {
						remaining = append(remaining, hook)
					}
				}
				if len(remaining) > 0 {
					entryMap["hooks"] = remaining
					kept = append(kept, entryMap)
				}
				continue
			}
		}
		if !isFormalGatesHook(entryMap) {
			kept = append(kept, entryMap)
		}
	}
	return kept
}

func isFormalGatesHook(value any) bool {
	text := strings.ToLower(fmt.Sprint(value))
	for _, marker := range []string{
		"formal-gates",
		"enforce-gate-sequence.ps1",
		"capture-subagent-receipt.ps1",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func nativeInstallCommand(skillRoot string, args ...string) string {
	parts := []string{quoteCommandArg(filepath.Join(skillRoot, "bin", nativeBinaryName()))}
	for _, arg := range args {
		if isPlainCommandToken(arg) {
			parts = append(parts, arg)
			continue
		}
		parts = append(parts, quoteCommandArg(arg))
	}
	return strings.Join(parts, " ")
}

func nativeReceiptCommand(skillRoot, provider, event, worktree string) string {
	return nativeInstallCommand(skillRoot,
		"receipt",
		"capture",
		"--provider",
		provider,
		"--event",
		event,
		"--worktree",
		worktree,
	)
}

func quoteCommandArg(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func isPlainCommandToken(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '-', '_', '.', '/':
			continue
		default:
			return false
		}
	}
	return true
}
