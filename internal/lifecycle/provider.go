package lifecycle

import (
	"fmt"
	"strings"

	"formal-gates/internal/host"
)

const (
	ProviderClaude    = host.Claude
	ProviderCodex     = host.Codex
	ProviderCursor    = host.Cursor
	ProviderDeepSeek  = host.DeepSeek
	ProviderZCode     = host.ZCode
	ProviderWorkBuddy = host.WorkBuddy
	ProviderDefault   = host.Default

	eventStart = "subagent_start"
	eventStop  = "subagent_stop"
)

type providerAdapter = host.Lifecycle

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
	result := make([]registeredAdapter, 0, len(host.All()))
	for _, descriptor := range host.All() {
		if !descriptor.Installable {
			continue
		}
		if descriptor.Lifecycle != nil {
			result = append(result, registeredAdapter{descriptor: descriptor, adapter: *descriptor.Lifecycle})
		}
	}
	return result
}

// ProviderEnvironmentKeys returns host identity environment keys from the
// lifecycle registry for callers that isolate their environment, such as the
// portable canary.
func ProviderEnvironmentKeys() []string {
	keys := []string{"AI_AGENT"}
	seen := map[string]bool{"AI_AGENT": true}
	for _, registered := range installedAdapters() {
		for _, key := range registered.adapter.EnvironmentKeys {
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
	if err != nil {
		return providerAdapter{}, fmt.Errorf("unsupported lifecycle provider %q", provider)
	}
	if descriptor.Lifecycle == nil {
		return providerAdapter{}, fmt.Errorf("unsupported lifecycle provider %q", provider)
	}
	adapter := *descriptor.Lifecycle
	// The finite registry owns event names and required/lenient semantics. The
	// adapter constructors only own payload-specific normalization and identity.
	adapter.Required = descriptor.LifecycleRequired
	if len(descriptor.LifecycleEvents) > 0 {
		adapter.HookEvents = append([]string(nil), descriptor.LifecycleEvents...)
	}
	return adapter, nil
}

func HookDefinitions(provider string) ([]HookDefinition, error) {
	adapter, err := adapterFor(provider)
	if err != nil {
		return nil, err
	}
	descriptor, err := host.Lookup(adapter.Name)
	if err != nil {
		return nil, err
	}
	hooks := make([]HookDefinition, 0, len(adapter.HookEvents))
	for _, event := range adapter.HookEvents {
		hooks = append(hooks, HookDefinition{
			Event:   event,
			Command: []string{"lifecycle", "capture", "--provider", adapter.Name, "--event", event},
			Matcher: descriptor.Hook.LifecycleMatcher,
			Timeout: descriptor.Hook.LifecycleTimeout,
		})
	}
	return hooks, nil
}

func ScalarString(value any) string { return host.ScalarString(value) }
