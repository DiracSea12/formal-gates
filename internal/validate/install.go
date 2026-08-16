package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/lifecycle"
)

type InstallOptions struct {
	Source    string
	Host      string
	Scope     string
	Project   string
	Force     bool
	SkipHooks bool
}

type UninstallOptions struct {
	Source  string
	Host    string
	Scope   string
	Project string
}

type InstallReport struct {
	Targets []InstallTargetReport `json:"targets"`
}

type InstallTargetReport struct {
	Host            string `json:"host"`
	TargetPath      string `json:"targetPath"`
	HookConfig      string `json:"hookConfig,omitempty"`
	ManagedRulePath string `json:"managedRulePath,omitempty"`
}

type UninstallReport struct {
	Targets []InstallTargetReport `json:"targets"`
}

type installTarget struct {
	host            string
	targetPath      string
	hookConfig      string
	managedRulePath string
}

var installRuntimeEntries = []string{
	"SKILL.md",
	"README.md",
	"README_EN.md",
	"formal-gates.manifest.json",
	"bin",
	"agents",
	"prompts",
	"gates",
	"references",
}

func Install(options InstallOptions) (InstallReport, error) {
	if strings.TrimSpace(options.Source) == "" {
		return InstallReport{}, fmt.Errorf("formal-gates source is required (--source); it must point at the package directory to install")
	}
	source := lifecycle.CleanRoot(options.Source)
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return InstallReport{}, err
	}
	sourceAbs = filepath.Clean(sourceAbs)
	if err := assertInstallSource(sourceAbs); err != nil {
		return InstallReport{}, err
	}
	rule, err := LoadManagedRule(sourceAbs)
	if err != nil {
		return InstallReport{}, err
	}

	targets, err := resolveInstallTargets(options.Host, options.Scope, options.Project)
	if err != nil {
		return InstallReport{}, err
	}

	report := InstallReport{}
	for _, target := range targets {
		if err := copyInstallRuntime(sourceAbs, target.targetPath, options.Force); err != nil {
			return InstallReport{}, err
		}
		targetReport := InstallTargetReport{
			Host:            target.host,
			TargetPath:      filepath.ToSlash(target.targetPath),
			ManagedRulePath: filepath.ToSlash(target.managedRulePath),
		}
		if !options.SkipHooks {
			if err := configureInstallHook(target); err != nil {
				return InstallReport{}, err
			}
			targetReport.HookConfig = filepath.ToSlash(target.hookConfig)
		}
		if target.managedRulePath != "" {
			if err := manageManagedRuleFile(target.managedRulePath, rule); err != nil {
				return InstallReport{}, err
			}
		}
		report.Targets = append(report.Targets, targetReport)
	}
	return report, nil
}

func Uninstall(options UninstallOptions) (UninstallReport, error) {
	targets, err := resolveInstallTargets(options.Host, options.Scope, options.Project)
	if err != nil {
		return UninstallReport{}, err
	}
	report := UninstallReport{}
	for _, target := range targets {
		if target.managedRulePath != "" {
			if err := removeManagedRuleFile(target.managedRulePath, target.host == "cursor"); err != nil {
				return UninstallReport{}, err
			}
		}
		if err := removeInstallHooks(target); err != nil {
			return UninstallReport{}, err
		}
		if exists(target.targetPath) {
			if err := removeExistingInstallTarget(target.targetPath); err != nil {
				return UninstallReport{}, err
			}
		}
		report.Targets = append(report.Targets, installTargetReport(target))
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
	case "dsh", "deepseek", "deepseek-harness":
		return "dsh", nil
	case "both":
		return "both", nil
	default:
		return "", fmt.Errorf("unsupported --host %q (want claude, codex, cursor, dsh, or both)", host)
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
		var managedRulePath string
		if scope == "global" {
			switch h {
			case "claude":
				base = filepath.Join(home, ".claude", "skills")
				hookConfig = filepath.Join(home, ".claude", "settings.json")
				managedRulePath = filepath.Join(home, ".claude", "CLAUDE.md")
			case "codex":
				base = filepath.Join(home, ".codex", "skills")
				hookConfig = filepath.Join(home, ".codex", "hooks.json")
				managedRulePath = filepath.Join(home, ".codex", "AGENTS.md")
			case "cursor":
				base = filepath.Join(home, ".cursor")
				hookConfig = filepath.Join(home, ".cursor", "hooks.json")
			case "dsh":
				var err error
				base, hookConfig, managedRulePath, err = dshInstallTargetPaths(home, "", scope)
				if err != nil {
					return nil, err
				}
			}
		} else {
			switch h {
			case "claude":
				base = filepath.Join(project, ".claude", "skills")
				hookConfig = filepath.Join(project, ".claude", "settings.json")
				managedRulePath = filepath.Join(project, "CLAUDE.md")
			case "codex":
				base = filepath.Join(project, ".codex", "skills")
				hookConfig = filepath.Join(project, ".codex", "hooks.json")
				managedRulePath = filepath.Join(project, "AGENTS.md")
			case "cursor":
				base = filepath.Join(project, ".cursor")
				hookConfig = filepath.Join(project, ".cursor", "hooks.json")
				managedRulePath = filepath.Join(project, ".cursor", "rules", "formal-gates.mdc")
			case "dsh":
				var err error
				base, hookConfig, managedRulePath, err = dshInstallTargetPaths("", project, scope)
				if err != nil {
					return nil, err
				}
			}
		}
		managedRule := ""
		if managedRulePath != "" {
			managedRule = filepath.Clean(managedRulePath)
		}
		hookConfigPath := ""
		if hookConfig != "" {
			hookConfigPath = filepath.Clean(hookConfig)
		}
		targets = append(targets, installTarget{
			host:            h,
			targetPath:      filepath.Clean(filepath.Join(base, "formal-gates")),
			hookConfig:      hookConfigPath,
			managedRulePath: managedRule,
		})
	}
	return targets, nil
}

func resolveInstallTargets(host, scope, project string) ([]installTarget, error) {
	normalizedHost, err := normalizeInstallHost(host)
	if err != nil {
		return nil, err
	}
	normalizedScope, err := normalizeInstallScope(scope)
	if err != nil {
		return nil, err
	}
	projectAbs := ""
	if normalizedScope == "project" || strings.TrimSpace(project) != "" {
		if strings.TrimSpace(project) == "" {
			return nil, fmt.Errorf("--project is required when --scope project is used")
		}
		projectAbs, err = filepath.Abs(project)
		if err != nil {
			return nil, err
		}
		projectAbs = filepath.Clean(projectAbs)
	}
	return installTargets(normalizedHost, normalizedScope, projectAbs)
}

func installTargetReport(target installTarget) InstallTargetReport {
	return InstallTargetReport{
		Host:            target.host,
		TargetPath:      filepath.ToSlash(target.targetPath),
		HookConfig:      filepath.ToSlash(target.hookConfig),
		ManagedRulePath: filepath.ToSlash(target.managedRulePath),
	}
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
		if isLiveEntry(entry) {
			if err := os.Symlink(from, to); err != nil {
				return err
			}
		} else if err := copyPath(from, to); err != nil {
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

// isLiveEntry reports whether an install entry should be a symlink back to the
// source rather than a static copy. Live entries contain prompt/gate content
// that changes without a full reinstall.
func isLiveEntry(entry string) bool {
	switch entry {
	case "gates", "prompts":
		return true
	}
	return false
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

func configureInstallHook(target installTarget) error {
	if strings.TrimSpace(target.hookConfig) == "" {
		return nil
	}
	if target.host == "dsh" {
		return configureDshHook(target)
	}
	config, err := readHookConfig(target.hookConfig)
	if err != nil {
		return err
	}
	lifecycleHooks, err := lifecycle.HookDefinitions(target.host)
	if err != nil {
		return err
	}
	hooks := hookObject(config)
	gateArgs := []string{"hook", "decide"}
	if target.host == "codex" {
		gateArgs = append(gateArgs, "--provider", "codex")
	}
	gateCommand := nativeInstallCommand(target.targetPath, gateArgs...)
	var desired map[string]any
	shape := "nested"
	switch target.host {
	case "claude":
		desired = map[string]any{
			"PreToolUse": nestedHookEntry("*", gateCommand, false),
		}
	case "codex":
		desired = map[string]any{
			"PreToolUse": nestedHookEntry(hostMatcher("codex"), gateCommand, true),
		}
	case "cursor":
		shape = "flat"
		config["version"] = float64(1)
		desired = map[string]any{
			"preToolUse": flatHookEntry(gateCommand),
		}
	}
	for _, hook := range lifecycleHooks {
		command := nativeInstallCommand(target.targetPath, hook.Command...)
		if shape == "flat" {
			desired[hook.Event] = flatHookEntry(command)
		} else {
			desired[hook.Event] = nestedHookEntry(hostMatcher(target.host), command, target.host == "codex")
		}
	}
	for event, entry := range desired {
		existing, _ := hooks[event].([]any)
		hooks[event] = append(removeFormalGatesHookEntries(existing, target, shape), entry)
	}
	for event, value := range hooks {
		if _, ok := desired[event]; ok {
			continue
		}
		existing, ok := value.([]any)
		if !ok {
			continue
		}
		hooks[event] = removeFormalGatesHookEntries(existing, target, shape)
	}
	config["hooks"] = hooks
	return writeHookConfig(target.hookConfig, config)
}

func removeInstallHooks(target installTarget) error {
	if strings.TrimSpace(target.hookConfig) == "" {
		return nil
	}
	if target.host == "dsh" {
		return removeDshHook(target)
	}
	if !isFile(target.hookConfig) {
		return nil
	}
	config, err := readHookConfig(target.hookConfig)
	if err != nil {
		return err
	}
	hooks, ok := config["hooks"].(map[string]any)
	if !ok {
		return nil
	}
	before, _ := json.Marshal(config)
	shape := "nested"
	if target.host == "cursor" {
		shape = "flat"
	}
	for event, value := range hooks {
		existing, ok := value.([]any)
		if !ok {
			continue
		}
		hooks[event] = removeFormalGatesHookEntries(existing, target, shape)
	}
	after, _ := json.Marshal(config)
	if string(before) == string(after) {
		return nil
	}
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

// hostMatcher returns the tool-name matcher for a host: Claude Code uses the glob
// "*" (matches every tool), Codex uses the regex ".*" — Codex's matcher is a regex,
// so the glob "*" is an invalid pattern that matches nothing and the hook never fires.
func hostMatcher(host string) string {
	if host == "codex" {
		return ".*"
	}
	return "*"
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

func removeFormalGatesHookEntries(entries []any, target installTarget, shape string) []any {
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
				removed := false
				for _, hook := range nested {
					if !isInstallerHookValue(entryMap, hook, target, shape) {
						remaining = append(remaining, hook)
					} else {
						removed = true
					}
				}
				if !removed {
					kept = append(kept, entryMap)
				} else if len(remaining) > 0 {
					entryMap["hooks"] = remaining
					kept = append(kept, entryMap)
				}
				continue
			}
		}
		if !isInstallerHookValue(nil, entryMap, target, shape) {
			kept = append(kept, entryMap)
		}
	}
	return kept
}

func isInstallerHookValue(parent map[string]any, value any, target installTarget, shape string) bool {
	command, ok := value.(map[string]any)
	if !ok {
		return false
	}
	commandText, ok := command["command"].(string)
	if !ok || !installerHookCommands(target)[normalizeHookCommand(commandText)] {
		return false
	}
	if shape == "nested" {
		// matcher 接受 "*"（Claude Code 与 legacy hook）或 ".*"（Codex 正则），
		// 其余值视为非 formal-gates 安装的 hook。
		if !exactObjectKeys(parent, "matcher", "hooks") || (parent["matcher"] != "*" && parent["matcher"] != ".*") {
			return false
		}
		return exactNestedHookShape(command, target.host) ||
			(exactLegacyNestedHookShape(command) &&
				(isLegacyInstallerHookCommand(commandText) || isLegacyCodexGateCommand(commandText, target)))
	}
	return exactFlatHookShape(command) ||
		(exactLegacyFlatHookShape(command) && isLegacyInstallerHookCommand(commandText))
}

func installerHookCommands(target installTarget) map[string]bool {
	commands := map[string]bool{}
	add := func(args ...string) {
		commands[normalizeHookCommand(nativeInstallCommand(target.targetPath, args...))] = true
	}
	gateArgs := []string{"hook", "decide"}
	if target.host == "codex" {
		add("hook", "decide")
		gateArgs = append(gateArgs, "--provider", "codex")
	}
	add(gateArgs...)
	lifecycleHooks, err := lifecycle.HookDefinitions(target.host)
	if err == nil {
		for _, hook := range lifecycleHooks {
			add(hook.Command...)
		}
	}
	for _, command := range []string{
		"pwsh -File hooks/" + "enforce-" + "gate-sequence.ps1",
		"pwsh -File hooks/" + "capture-" + "subagent-receipt.ps1",
	} {
		commands[command] = true
	}
	return commands
}

func exactNestedHookShape(value map[string]any, host string) bool {
	if value == nil || value["type"] != "command" {
		return false
	}
	if host == "codex" {
		return exactObjectKeys(value, "type", "command", "timeout") && value["timeout"] == float64(30)
	}
	return exactObjectKeys(value, "type", "command")
}

func exactFlatHookShape(value map[string]any) bool {
	return value != nil && exactObjectKeys(value, "command", "timeout", "failClosed") &&
		value["timeout"] == float64(30) && value["failClosed"] == true
}

func exactLegacyNestedHookShape(value map[string]any) bool {
	return value != nil && exactObjectKeys(value, "type", "command") && value["type"] == "command"
}

func exactLegacyFlatHookShape(value map[string]any) bool {
	return value != nil && exactObjectKeys(value, "command")
}

func isLegacyInstallerHookCommand(command string) bool {
	return command == "pwsh -File hooks/"+"enforce-"+"gate-sequence.ps1" ||
		command == "pwsh -File hooks/"+"capture-"+"subagent-receipt.ps1"
}

func isLegacyCodexGateCommand(command string, target installTarget) bool {
	if target.host != "codex" {
		return false
	}
	return normalizeHookCommand(command) == normalizeHookCommand(nativeInstallCommand(target.targetPath, "hook", "decide"))
}

func exactObjectKeys(value map[string]any, expected ...string) bool {
	if value == nil || len(value) != len(expected) {
		return false
	}
	keys := make(map[string]bool, len(expected))
	for _, key := range expected {
		keys[key] = true
	}
	for key := range value {
		if !keys[key] {
			return false
		}
	}
	return true
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

// normalizeHookCommand 去掉命令字符串里的双引号，用于卸载/升级时识别新旧两种 install
// 格式：旧版无条件给 exe 路径加引号、新版仅在需要时加引号，归一化后相同。
func normalizeHookCommand(command string) string {
	return strings.ReplaceAll(command, `"`, "")
}

func quoteCommandArg(value string) string {
	value = slashCommandPath(value)
	if !requiresShellQuoting(value) {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

// requiresShellQuoting 只在值含空白或 shell 元字符时才需要引号。无空格/特殊字符的路径
// 不加引号：Codex 0.146 会把带引号的 exe 路径当成命令的一部分执行失败，导致 hook 被判
// Failed 后 fail-open 放行。
func requiresShellQuoting(value string) bool {
	for _, r := range value {
		switch r {
		case ' ', '\t', '"', '\'', '&', '|', ';', '<', '>', '(', ')', '^', '`':
			return true
		}
	}
	return false
}

func slashCommandPath(value string) string {
	if strings.Contains(value, `\`) || filepath.IsAbs(value) {
		return filepath.ToSlash(value)
	}
	return value
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
