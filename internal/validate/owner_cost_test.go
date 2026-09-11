package validate

import (
	"encoding/json"

	"os"
	"path/filepath"

	"strings"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/host"
)

const ownerAdditionalUsage = `{"timestamp":"after-start","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":60,"cached_input_tokens":5,"output_tokens":4}}}}
`

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

func appendOwnerUsage(t *testing.T, path string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(ownerAdditionalUsage); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func requireOwnerUnavailable(t *testing.T, projection *cost.RunCost) {
	t.Helper()
	if projection == nil || projection.Owner == nil || *projection.Owner != (cost.OwnerCost{Source: cost.SourceUnavailable}) {
		t.Fatalf("want explicit unavailable owner with zero numbers, got %+v", projection)
	}
}
