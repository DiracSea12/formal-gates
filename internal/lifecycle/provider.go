package lifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/host"
)

const (
	ProviderClaude   = host.Claude
	ProviderCodex    = host.Codex
	ProviderCursor   = host.Cursor
	ProviderDeepSeek = host.DeepSeek
	ProviderZCode    = host.ZCode
	ProviderDefault  = host.Default

	eventStart = "subagent_start"
	eventStop  = "subagent_stop"
)

type providerAdapter struct {
	name       string
	required   bool
	hookEvents []string
	// Identity signals are declared by the adapter that owns the host
	// integration. The detector below only scans these declarations; it does
	// not contain a second host-specific switch.
	// executableMatches consumes a normalized absolute path, so scanning
	// adapters does not repeat filesystem resolution for the same executable.
	executableMatches func(string) bool
	agentPrefixes     []string
	environmentKeys   []string
	normalizeEvent    func(string) (string, error)
	identity          func(string, any) string
	correlation       func(string, any) string
	projectRoots      func(any) []string
	transcriptPath    func(any) string
	// reason 从宿主 stop/error 事件提取中断原因（含 HTTP 错误码），供自动记录。
	reason func(any) string
}

type HookDefinition struct {
	Event   string
	Command []string
	Matcher string
	Timeout bool
}

type registeredAdapter struct {
	descriptor host.Descriptor
	adapter    providerAdapter
}

func installedAdapters() []registeredAdapter {
	result := []registeredAdapter{}
	for _, descriptor := range host.All() {
		if descriptor.Installable && descriptor.Lifecycle != nil {
			result = append(result, registeredAdapter{descriptor, descriptorAdapter(descriptor)})
		}
	}
	return result
}

// ProviderEnvironmentKeys returns the environment keys consulted by lifecycle
// host detection. Callers that temporarily neutralize host identity (such as
// the portable canary) derive their cleanup set from the same registry.
func ProviderEnvironmentKeys() []string {
	keys := []string{"AI_AGENT"}
	seen := map[string]bool{"AI_AGENT": true}
	for _, registered := range installedAdapters() {
		for _, key := range registered.adapter.environmentKeys {
			if key = strings.TrimSpace(key); key != "" && !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	return keys
}

func adapterFor(provider string) (providerAdapter, error) {
	descriptor, err := host.Lookup(provider)
	if err != nil || descriptor.Lifecycle == nil {
		return providerAdapter{}, fmt.Errorf("unsupported lifecycle provider %q", provider)
	}
	return descriptorAdapter(descriptor), nil
}

func descriptorAdapter(d host.Descriptor) providerAdapter {
	a := d.Lifecycle
	return providerAdapter{name: d.ID, required: d.LifecycleRequired,
		hookEvents: d.LifecycleEvents, executableMatches: a.ExecutableMatches,
		agentPrefixes: a.AgentPrefixes, environmentKeys: a.EnvironmentKeys,
		normalizeEvent: a.NormalizeEvent, identity: a.Identity, correlation: a.Correlation,
		projectRoots: a.ProjectRoots, transcriptPath: a.TranscriptPath, reason: a.Reason}
}

func HookDefinitions(provider string) ([]HookDefinition, error) {
	adapter, err := adapterFor(provider)
	if err != nil {
		return nil, err
	}
	descriptor, err := host.Lookup(adapter.name)
	if err != nil {
		return nil, err
	}
	hooks := make([]HookDefinition, 0, len(adapter.hookEvents))
	for _, event := range adapter.hookEvents {
		hooks = append(hooks, HookDefinition{
			Event:   event,
			Command: []string{"lifecycle", "capture", "--provider", adapter.name, "--event", event},
			Matcher: descriptor.Hook.LifecycleMatcher,
			Timeout: descriptor.Hook.LifecycleTimeout,
		})
	}
	return hooks, nil
}

var executablePath = os.Executable

func currentProvider() (string, error) {
	path, err := executablePath()
	if err != nil {
		return "", err
	}
	normalized := normalizeProviderExecutablePath(path)
	// A project install without native hooks stays lenient. Each platform
	// declares its path shape; detection never adds a platform-specific branch.
	for _, d := range host.All() {
		if a := d.Lifecycle; a != nil && a.ProjectWithoutHooks != nil && a.ProjectWithoutHooks(normalized) {
			return ProviderDefault, nil
		}
	}
	if provider := providerFromNormalizedExecutable(normalized); provider != ProviderDefault {
		return provider, nil
	}
	// The stage-0 launcher is deliberately one fixed user-level path for every
	// host.  Host-specific directory names are therefore no longer available as
	// an identity signal.  Resolve the provider from the same shared registry
	// that admitted the target, scoped to the current project/root and launcher;
	// this is read-only observation and does not create a second registry truth.
	if provider, ambiguous := providerFromRegistryDetailed(path); ambiguous {
		return "", fmt.Errorf("lifecycle provider is ambiguous for shared launcher %q; pass an explicit --provider host context", path)
	} else if provider != "" {
		return provider, nil
	}
	// A directly built source binary outside a maintained host installation
	// (go run, a local development build, or an uninstalled copy) resolves to
	// the lenient default provider from its path. When such a binary is driven
	// by a real host, its environment is the authoritative signal for the host
	// provider, so the dispatch binding still matches the host's capture hooks
	// and the subagent transcript cost is recorded instead of being dropped.
	if provider := providerFromEnvironment(); provider != "" {
		return provider, nil
	}
	return ProviderDefault, nil
}

// providerFromEnvironment reports the host that drives the process from its
// environment. It is consulted only when the executable resolves to the
// lenient default provider. Each host exports a distinctive environment in its
// agent shell; AI_AGENT is the host's own declared identity and takes
// precedence, with host-specific variables as a fallback. Empty when no host
// environment is detectable, keeping tests, portable canaries, and truly
// uninstalled contexts on the lenient default provider.
func providerFromEnvironment() string {
	agent := strings.ToLower(strings.TrimSpace(os.Getenv("AI_AGENT")))
	adapters := installedAdapters()
	for _, registered := range adapters {
		for _, prefix := range registered.adapter.agentPrefixes {
			if strings.HasPrefix(agent, strings.ToLower(strings.TrimSpace(prefix))) {
				return registered.descriptor.ID
			}
		}
	}
	for _, registered := range adapters {
		for _, key := range registered.adapter.environmentKeys {
			if strings.TrimSpace(os.Getenv(key)) != "" {
				return registered.descriptor.ID
			}
		}
	}
	return ""
}

// registryProviderRecord is the read-only subset of the shared admission
// registry needed to recover host identity for the fixed stable launcher.
// Lifecycle does not own registry writes; validate's transaction owner remains
// the only semantic writer.
type registryProviderRecord struct {
	Target       string            `json:"target"`
	LauncherPath string            `json:"launcherPath"`
	Host         string            `json:"host"`
	Scope        string            `json:"scope"`
	ProjectRoot  string            `json:"projectRoot"`
	Canonical    map[string]string `json:"canonicalPaths"`
	Status       string            `json:"status"`
}

type registryProviderDocument struct {
	Records []registryProviderRecord `json:"records"`
}

// providerFromRegistryDetailed returns the most-specific admitted provider.
// A shared stable launcher may have more than one active host admission at the
// same project specificity; silently choosing the first record would bind the
// lifecycle transcript to the wrong host, so that case is explicitly reported
// as ambiguous to the caller.
func providerFromRegistryDetailed(executable string) (string, bool) {
	registryPath := ""
	for _, name := range []string{"HOME", "USERPROFILE"} {
		if home := strings.TrimSpace(os.Getenv(name)); home != "" {
			registryPath = filepath.Join(home, ".formal-gates", "registry.json")
			break
		}
	}
	if registryPath == "" {
		return "", false
	}
	data, err := os.ReadFile(registryPath)
	if err != nil {
		return "", false
	}
	var document registryProviderDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return "", false
	}
	// Admissions commonly share one launcher and project root. Resolve each
	// path once per detection, keeping filesystem changes visible to the next
	// call without repeating symlink traversal for every registry record.
	canonicalPaths := make(map[string]string)
	canonicalPath := func(path string) string {
		if resolved, ok := canonicalPaths[path]; ok {
			return resolved
		}
		resolved := canonicalProviderPath(path)
		canonicalPaths[path] = resolved
		return resolved
	}
	executable = canonicalPath(executable)
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", false
	}
	workingDirectory = canonicalPath(workingDirectory)
	bestHost := ""
	bestRoot := ""
	bestRootLength := -1
	ambiguous := false
	lenientProjectMatch := false
	lenientRootLength := -1
	for _, record := range document.Records {
		if strings.ToLower(strings.TrimSpace(record.Status)) != "active" {
			continue
		}
		descriptor, descriptorErr := host.Lookup(record.Host)
		if descriptorErr != nil {
			continue
		}
		launcher := record.LauncherPath
		if launcher == "" && record.Canonical != nil {
			launcher = record.Canonical["launcher"]
		}
		if canonicalPath(launcher) != executable {
			continue
		}
		root := record.ProjectRoot
		if root == "" && record.Canonical != nil {
			root = record.Canonical["projectRoot"]
		}
		root = canonicalPath(root)
		if root == "" || !providerPathContains(root, workingDirectory) {
			continue
		}
		// A project install without a project hook configuration cannot provide
		// lifecycle observations. Keep the shared stable launcher lenient for
		// such records instead of turning a normal project invocation into a
		// required-provider rejection.
		if strings.EqualFold(strings.TrimSpace(record.Scope), "project") && descriptor.Paths.ProjectHookConfig == "" {
			lenientProjectMatch = true
			if len(root) > lenientRootLength {
				lenientRootLength = len(root)
			}
			continue
		}
		if len(root) <= bestRootLength {
			if len(root) == bestRootLength && root == bestRoot && bestHost != "" {
				adapter, adapterErr := adapterFor(record.Host)
				if adapterErr == nil && adapter.name != bestHost {
					ambiguous = true
				}
			}
			continue
		}
		adapter, adapterErr := adapterFor(record.Host)
		if adapterErr != nil {
			continue
		}
		bestHost = adapter.name
		bestRoot = root
		bestRootLength = len(root)
		ambiguous = false
	}
	if lenientProjectMatch && lenientRootLength > bestRootLength {
		return ProviderDefault, false
	}
	if bestHost == "" && lenientProjectMatch {
		return ProviderDefault, false
	}
	return bestHost, ambiguous
}

func canonicalProviderPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

func providerPathContains(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative))
}

func providerFromExecutable(path string) string {
	return providerFromNormalizedExecutable(normalizeProviderExecutablePath(path))
}

func providerFromNormalizedExecutable(normalized string) string {
	for _, registered := range installedAdapters() {
		if matcher := registered.adapter.executableMatches; matcher != nil && matcher(normalized) {
			return registered.descriptor.ID
		}
	}
	// 未安装二进制（go test、canary portable、本地开发构建）解析为宽松的默认
	// provider：无生命周期事件时仍走 UNAVAILABLE。只有实际安装且声明身份信号
	// 的宿主二进制才解析为 required provider，验证生命周期配对。
	return ProviderDefault
}

func normalizeProviderExecutablePath(path string) string {
	path = strings.TrimSpace(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	return strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
}

// ScalarString retains the public lifecycle helper; native payload decoding
// is owned by the host package and is not reimplemented here.
func ScalarString(value any) string { return host.ScalarString(value) }
