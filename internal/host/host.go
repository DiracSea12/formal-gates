// Package host owns platform descriptions and native evidence interpretation.
// It has no dependency on the engine, settings loader or execution transport.
package host

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

const (
	Claude    = "claude-code"
	Codex     = "codex"
	Cursor    = "cursor"
	DeepSeek  = "deepseek-harness"
	ZCode     = "zcode"
	WorkBuddy = "workbuddy"
	Default   = "default"
)

type HookKind string

const (
	HookNested HookKind = "nested"
	HookFlat   HookKind = "flat"
	HookZCode  HookKind = "zcode"
	HookDSH    HookKind = "dsh"
)

type HookProtocol string

const (
	ProtocolGeneric HookProtocol = "generic"
	ProtocolCodex   HookProtocol = "codex"
	ProtocolZCode   HookProtocol = "zcode"
)

type PathLayout struct {
	GlobalBase             string
	GlobalHookConfig       string
	GlobalManaged          string
	ProjectBase            string
	ProjectHookConfig      string
	ProjectManaged         string
	RemoveManagedWhenEmpty bool
}

type HookSpec struct {
	Kind             HookKind
	Protocol         HookProtocol
	GateEvent        string
	GateMatcher      string
	GateTimeout      bool
	LifecycleMatcher string
	LifecycleTimeout bool
}

type Descriptor struct {
	Lifecycle         *Lifecycle                     `json:"-"`
	ReadUsage         func(io.Reader) (Usage, error) `json:"-"`
	Configuration     []ConfigField
	ID                string
	InstallName       string
	ManifestName      string
	Aliases           []string
	Installable       bool
	Paths             PathLayout
	Hook              HookSpec
	LifecycleEvents   []string
	LifecycleRequired bool
	CostProvider      string
}

var descriptors = []*Descriptor{
	{
		ID: Claude, ReadUsage: parseClaudeTranscript, Lifecycle: claudeAdapter(),
		Configuration: []ConfigField{StringConfig("claudeExecutable", "claude"), StringConfig("claudeHome", "")},
		InstallName:   "claude",
		ManifestName:  "Claude Code",
		Aliases:       []string{"claude", "claude-code", "claude code"},
		Installable:   true,
		Paths: PathLayout{
			GlobalBase: ".claude/skills", GlobalHookConfig: ".claude/settings.json", GlobalManaged: ".claude/CLAUDE.md",
			ProjectBase: ".claude/skills", ProjectHookConfig: ".claude/settings.json", ProjectManaged: "CLAUDE.md",
		},
		Hook:            HookSpec{Kind: HookNested, Protocol: ProtocolGeneric, GateEvent: "PreToolUse", GateMatcher: "*"},
		LifecycleEvents: []string{"SubagentStart", "SubagentStop"}, LifecycleRequired: true, CostProvider: Claude,
	},
	{
		ID: Codex, ReadUsage: parseCodexTranscript, Lifecycle: codexAdapter(),
		Configuration: []ConfigField{StringConfig("codexExecutable", "codex"), StringConfig("codexHome", "")},
		InstallName:   "codex",
		ManifestName:  "Codex",
		Aliases:       []string{"codex"},
		Installable:   true,
		Paths: PathLayout{
			GlobalBase: ".codex/skills", GlobalHookConfig: ".codex/hooks.json", GlobalManaged: ".codex/AGENTS.md",
			ProjectBase: ".codex/skills", ProjectHookConfig: ".codex/hooks.json", ProjectManaged: "AGENTS.md",
		},
		Hook:            HookSpec{Kind: HookNested, Protocol: ProtocolCodex, GateEvent: "PreToolUse", GateMatcher: ".*", GateTimeout: true, LifecycleMatcher: ".*", LifecycleTimeout: true},
		LifecycleEvents: []string{"SubagentStart", "SubagentStop"}, LifecycleRequired: true, CostProvider: Codex,
	},
	{
		ID: Cursor, Lifecycle: cursorAdapter(),
		Configuration: []ConfigField{StringConfig("cursorExecutable", "cursor-agent"), StringConfig("cursorHome", "")},
		InstallName:   "cursor",
		ManifestName:  "Cursor",
		Aliases:       []string{"cursor"},
		Installable:   true,
		Paths: PathLayout{
			GlobalBase: ".cursor", GlobalHookConfig: ".cursor/hooks.json",
			ProjectBase: ".cursor", ProjectHookConfig: ".cursor/hooks.json", ProjectManaged: ".cursor/rules/formal-gates.mdc", RemoveManagedWhenEmpty: true,
		},
		Hook:            HookSpec{Kind: HookFlat, Protocol: ProtocolGeneric, GateEvent: "preToolUse", GateMatcher: "*"},
		LifecycleEvents: []string{"subagentStart", "subagentStop"}, LifecycleRequired: true,
	},
	{
		ID: DeepSeek, ReadUsage: parseDSHTranscript, Lifecycle: deepseekAdapter(),
		Configuration:   []ConfigField{StringConfig("dshExecutable", "dsh")},
		InstallName:     "dsh",
		ManifestName:    "DeepSeek Harness",
		Aliases:         []string{"dsh", "deepseek", "deepseek-harness", "deepseek harness"},
		Installable:     true,
		Paths:           PathLayout{ProjectManaged: "AGENTS.md"},
		Hook:            HookSpec{Kind: HookDSH, Protocol: ProtocolGeneric, GateEvent: "PreToolUse", GateMatcher: "*"},
		LifecycleEvents: []string{"SubagentStart", "SubagentStop"}, LifecycleRequired: true,
		CostProvider: DeepSeek,
	},
	{
		ID: ZCode, ReadUsage: parseZCodeTranscript, Lifecycle: zcodeAdapter(),
		Configuration: []ConfigField{StringConfig("zcodeExecutable", "node"), StringConfig("zcodeScript", ""), StringConfig("zcodeHome", "")},
		InstallName:   "zcode",
		ManifestName:  "ZCode",
		Aliases:       []string{"zcode", "z-code"},
		Installable:   true,
		Paths: PathLayout{
			GlobalBase: ".zcode/skills", GlobalHookConfig: ".zcode/cli/config.json", GlobalManaged: ".zcode/AGENTS.md",
			ProjectBase: ".zcode/skills", ProjectManaged: "AGENTS.md",
		},
		Hook:            HookSpec{Kind: HookZCode, Protocol: ProtocolZCode, GateEvent: "PreToolUse", GateMatcher: ".*", LifecycleMatcher: "Agent|Task"},
		LifecycleEvents: []string{"PreToolUse", "PostToolUse", "PostToolUseFailure"}, LifecycleRequired: true,
		CostProvider: ZCode,
	},
	{
		ID: WorkBuddy, ReadUsage: parseWorkBuddyTranscript, Lifecycle: workbuddyAdapter(), InstallName: "workbuddy", ManifestName: "WorkBuddy",
		Aliases: []string{"workbuddy", "codebuddy", "cbc"}, Installable: true,
		Paths:        PathLayout{GlobalBase: ".codebuddy/skills", GlobalManaged: ".codebuddy/CODEBUDDY.md", ProjectBase: ".codebuddy/skills", ProjectManaged: "CODEBUDDY.md"},
		CostProvider: WorkBuddy, LifecycleRequired: true,
		Configuration: []ConfigField{StringConfig("workbuddyExecutable", "codebuddy"), StringConfig("workbuddyProductConfig", ""), StringConfig("workbuddyHome", "")},
	},
	{
		ID: Default, Lifecycle: defaultAdapter(),
		InstallName:     "",
		ManifestName:    "",
		Aliases:         []string{"default"},
		Installable:     false,
		Hook:            HookSpec{Kind: HookNested, Protocol: ProtocolGeneric},
		LifecycleEvents: []string{"SubagentStart", "SubagentStop"}, LifecycleRequired: false,
	},
}

type Adapter interface{ HostDescriptor() *Descriptor }

func (d *Descriptor) HostDescriptor() *Descriptor { return d }

var registryMu sync.RWMutex
var adapters []Adapter

// Register binds runtime capability to its original descriptor or adds one
// complete platform object. Configuration and evidence share that object.
func Register(adapter Adapter) error {
	if adapter == nil || adapter.HostDescriptor() == nil || strings.TrimSpace(adapter.HostDescriptor().ID) == "" {
		return fmt.Errorf("platform descriptor requires an ID")
	}
	d := adapter.HostDescriptor()
	registryMu.Lock()
	defer registryMu.Unlock()
	for _, existing := range adapters {
		if existing.HostDescriptor().ID == d.ID {
			return fmt.Errorf("platform %q is already registered", d.ID)
		}
	}
	for _, existing := range descriptors {
		if existing == d {
			adapters = append(adapters, adapter)
			return nil
		}
		for _, a := range append([]string{existing.ID}, existing.Aliases...) {
			for _, b := range append([]string{d.ID}, d.Aliases...) {
				if strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) {
					return fmt.Errorf("platform alias %q is already registered", b)
				}
			}
		}
		for _, a := range existing.Configuration {
			for _, b := range d.Configuration {
				if a.Name == b.Name {
					return fmt.Errorf("platform configuration field %q is already registered", b.Name)
				}
			}
		}
	}
	descriptors = append(descriptors, d)
	adapters = append(adapters, adapter)
	return nil
}

func LookupAdapter(value string) (Adapter, error) {
	d, err := Lookup(value)
	if err != nil {
		return nil, err
	}
	registryMu.RLock()
	defer registryMu.RUnlock()
	for _, a := range adapters {
		if a.HostDescriptor().ID == d.ID {
			return a, nil
		}
	}
	for _, known := range descriptors {
		if known.ID == d.ID {
			return known, nil
		}
	}
	return nil, fmt.Errorf("unsupported host %q", value)
}

// RegisteredAdapters preserves capability registration order.
func RegisteredAdapters() []Adapter {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return append([]Adapter(nil), adapters...)
}

// All returns a copy of the finite registry. Consumers that need to enumerate
// supported installable hosts should derive their view from this source rather
// than maintaining a second host list.
func All() []Descriptor {
	result := make([]Descriptor, len(descriptors))
	for i, d := range descriptors {
		result[i] = *d
	}
	return result
}

// InstallableNames returns the user-facing install names in registry order.
// CLI/help surfaces should derive their host list from this source rather than
// repeating a second finite list.
func InstallableNames() []string {
	result := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if descriptor.Installable && descriptor.InstallName != "" {
			result = append(result, descriptor.InstallName)
		}
	}
	return result
}

// InstallableIDs returns canonical provider IDs for installable descriptors.
func InstallableIDs() []string {
	result := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if descriptor.Installable {
			result = append(result, descriptor.ID)
		}
	}
	return result
}

func Lookup(value string) (Descriptor, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for _, descriptor := range descriptors {
		if descriptor.ID == normalized {
			return *descriptor, nil
		}
		for _, alias := range descriptor.Aliases {
			if strings.ToLower(strings.TrimSpace(alias)) == normalized {
				return *descriptor, nil
			}
		}
	}
	return Descriptor{}, fmt.Errorf("unsupported host %q", value)
}

// ConfigField is a platform-owned editable string option. Defaults and validation
// live with its adapter, not in the common settings struct or loader.
type ConfigField struct {
	Name     string
	Default  string
	Validate func(string) error
}

func StringConfig(name, value string) ConfigField {
	return ConfigField{Name: name, Default: value, Validate: func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s: use a nonempty command/path or remove the field", name)
		}
		return nil
	}}
}

func Configuration() map[string]ConfigField {
	fields := map[string]ConfigField{}
	for _, d := range All() {
		for _, f := range d.Configuration {
			fields[f.Name] = f
		}
	}
	return fields
}
