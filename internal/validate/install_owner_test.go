package validate

import (
	"encoding/json"
	"formal-gates/internal/host"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnerHookInstallationBindsProviderAndReplacesOldHook(t *testing.T) {
	for _, provider := range []string{host.Claude, host.Codex} {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			target := installTarget{host: provider, targetPath: filepath.Join(root, "skill"), launcherPath: filepath.Join(root, nativeBinaryName()), hookConfig: filepath.Join(root, "hooks.json")}
			descriptor, _ := host.Lookup(provider)
			old := nestedHookEntry(descriptor.Hook.GateMatcher, nativeInstallCommand(target.launcherPath, "hook", "decide"), descriptor.Hook.GateTimeout)
			if err := writeHookConfig(target.hookConfig, map[string]any{"hooks": map[string]any{"PreToolUse": []any{old}}}); err != nil {
				t.Fatal(err)
			}
			if _, err := configureInstallHook(target); err != nil {
				t.Fatal(err)
			}
			config, err := readHookConfig(target.hookConfig)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(hookObject(config)["PreToolUse"])
			if strings.Count(string(data), "hook decide") != 1 || !strings.Contains(string(data), "--provider "+provider) {
				t.Fatalf("owner provider hook missing or duplicated: %s", data)
			}
			if changed, err := configureInstallHook(target); err != nil || changed {
				t.Fatalf("reinstall changed the same owner hook: %v, %v", changed, err)
			}
		})
	}
}
