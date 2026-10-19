package validate

import (
	"fmt"
	"path/filepath"

	"formal-gates/internal/host"
)

// platformAdapter bundles the install-time pieces a host cannot express in
// host.Descriptor: custom path resolution, hook writers, hook state files,
// MCP control configuration and retired lifecycle hook events.
type platformAdapter struct {
	paths             installPathResolver
	hook              hookCapability
	hookStatePath     func(hookConfig string) string
	controlPath       func(target installTarget) string
	configureControl  func(target installTarget, path string, remove bool) (bool, error)
	retiredHookEvents []string
	// retiredBareGate marks platforms whose historical installs left a gate
	// hook command without --provider.
	retiredBareGate bool
}

var genericHook = hookCapability{configure: configureGenericInstallHook, remove: removeGenericInstallHooks}

// platformAdapters is keyed by platform ID (host.Descriptor.ID). The table is
// built in init because its function values reach retiredLifecycleHookEvents,
// which reads this same map.
var platformAdapters map[string]platformAdapter

func init() {
	platformAdapters = map[string]platformAdapter{
		host.Claude: {
			hook:              genericHook,
			retiredHookEvents: []string{"SubagentStart", "SubagentStop"},
		},
		host.Codex: {
			hook:              genericHook,
			controlPath:       controlSiblingPath("config.toml"),
			configureControl:  configureCodexControl,
			retiredHookEvents: []string{"SubagentStart", "SubagentStop"},
			retiredBareGate:   true,
		},
		host.Cursor: {
			hook:              genericHook,
			retiredHookEvents: []string{"subagentStart", "subagentStop"},
		},
		host.DeepSeek: {
			paths:             dshInstallTargetPaths,
			hook:              hookCapability{configure: configureDshHook, remove: removeDshHook, packageFiles: dshHookPackageFiles},
			retiredHookEvents: []string{"SubagentStart", "SubagentStop"},
		},
		host.ZCode: {
			hook:              hookCapability{configure: configureZCodeHook, remove: removeZCodeHook},
			hookStatePath:     zcodeHookStatePath,
			controlPath:       zcodeControlPath,
			configureControl:  jsonServerControl("ZCode", []string{"mcp", "servers"}, "stdio"),
			retiredHookEvents: []string{"PreToolUse", "PostToolUse", "PostToolUseFailure"},
		},
		host.WorkBuddy: {
			hook: genericHook,
		},
		host.Devin: {
			paths:            devinInstallTargetPaths,
			hook:             genericHook,
			controlPath:      controlSiblingPath("mcp_config.json"),
			configureControl: jsonServerControl("Devin", []string{"mcpServers"}, ""),
		},
	}
}

// platformAdapterFor resolves a host name or alias to its install adapter.
func platformAdapterFor(hostName string) (platformAdapter, error) {
	descriptor, err := host.Lookup(hostName)
	if err != nil {
		return platformAdapter{}, err
	}
	adapter, ok := platformAdapters[descriptor.ID]
	if !ok {
		return platformAdapter{}, fmt.Errorf("host %q has no install adapter", descriptor.ID)
	}
	return adapter, nil
}

// controlSiblingPath resolves an MCP configuration file next to the
// installation's parent directory.
func controlSiblingPath(name string) func(installTarget) string {
	return func(target installTarget) string {
		return filepath.Join(filepath.Dir(filepath.Dir(target.targetPath)), name)
	}
}

func zcodeControlPath(target installTarget) string {
	if target.hookConfig != "" {
		return target.hookConfig
	}
	return controlSiblingPath("config.json")(target)
}
