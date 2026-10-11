// Package host owns the finite, host-facing capability registry.
//
// The workflow engine deliberately does not import this package. Host-specific
// installation paths, hook protocols, lifecycle event names and transcript
// adapters live at the maintenance/integration boundary instead of being
// encoded in workflow definitions.
package host

import (
	"fmt"
	"strings"
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

var descriptors = []Descriptor{
	{
		ID:            Claude,
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
		ID:            Codex,
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
		ID:            Cursor,
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
		ID:              DeepSeek,
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
		ID:            ZCode,
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
		ID: WorkBuddy, InstallName: "workbuddy", ManifestName: "WorkBuddy",
		Aliases: []string{"workbuddy", "codebuddy", "cbc"}, Installable: true,
		Paths:        PathLayout{GlobalBase: ".codebuddy/skills", GlobalManaged: ".codebuddy/CODEBUDDY.md", ProjectBase: ".codebuddy/skills", ProjectManaged: "CODEBUDDY.md"},
		CostProvider: WorkBuddy, LifecycleRequired: true,
		Configuration: []ConfigField{StringConfig("workbuddyExecutable", "codebuddy"), StringConfig("workbuddyProductConfig", ""), StringConfig("workbuddyHome", "")},
	},
	{
		ID:              Default,
		InstallName:     "",
		ManifestName:    "",
		Aliases:         []string{"default"},
		Installable:     false,
		Hook:            HookSpec{Kind: HookNested, Protocol: ProtocolGeneric},
		LifecycleEvents: []string{"SubagentStart", "SubagentStop"}, LifecycleRequired: false,
	},
}

// All returns a copy of the finite registry. Consumers that need to enumerate
// supported installable hosts should derive their view from this source rather
// than maintaining a second host list.
func All() []Descriptor {
	result := make([]Descriptor, len(descriptors))
	copy(result, descriptors)
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
			return descriptor, nil
		}
		for _, alias := range descriptor.Aliases {
			if strings.ToLower(strings.TrimSpace(alias)) == normalized {
				return descriptor, nil
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
