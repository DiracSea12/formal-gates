package validate

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/host"
)

func TestInstallerRemovesOwnedLifecycleHooksAndPreservesUserEntries(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{host.Claude, "claude", host.Codex, host.Cursor, host.ZCode} {
		for _, upgrade := range []bool{false, true} {
			t.Run(provider+map[bool]string{true: "/upgrade", false: "/uninstall"}[upgrade], func(t *testing.T) {
				root := t.TempDir()
				target := installTarget{host: provider, targetPath: filepath.Join(root, "skill"), launcherPath: filepath.Join(root, "launcher"), hookConfig: filepath.Join(root, "hooks.json")}
				descriptor, _ := host.Lookup(provider)
				events := map[string]any{}
				for _, event := range retiredLifecycleHookEvents(descriptor.ID) {
					if provider == host.ZCode {
						args := []string{"lifecycle", "capture", "--provider", descriptor.ID, "--event", event}
						events[event] = []any{zcodeHookGroup("Agent|Task", zcodeProcessEntry(target.launcherPath, args)), zcodeHookGroup("Agent|Task", zcodeProcessEntry("user-launcher", args))}
					} else {
						entries := []any{}
						for _, launcher := range []string{target.launcherPath, filepath.Join(target.targetPath, "bin", nativeBinaryName()), "user-launcher"} {
							command := nativeInstallCommand(launcher, "lifecycle", "capture", "--provider", descriptor.ID, "--event", event)
							if descriptor.Hook.Kind == host.HookFlat {
								entries = append(entries, flatHookEntry(command))
							} else {
								entries = append(entries, nestedHookEntry(descriptor.Hook.GateMatcher, command, descriptor.Hook.GateTimeout))
							}
						}
						events[event] = entries
					}
				}
				config := map[string]any{"hooks": events}
				if provider == host.ZCode {
					config = map[string]any{"hooks": map[string]any{"events": events}}
				}
				if err := writeHookConfig(target.hookConfig, config); err != nil {
					t.Fatal(err)
				}
				readConfig := readHookConfig
				if provider == host.ZCode {
					readConfig = readZCodeHookConfig
				}
				original, err := readConfig(target.hookConfig)
				if err != nil {
					t.Fatal(err)
				}
				originalEvents := hookObject(original)
				if provider == host.ZCode {
					originalEvents = zcodeHookEvents(zcodeHookRoot(original))
				}
				userEntries := map[string]any{}
				for _, event := range retiredLifecycleHookEvents(descriptor.ID) {
					entries := originalEvents[event].([]any)
					userEntries[event] = entries[len(entries)-1]
				}
				if upgrade {
					if _, err := configureInstallHook(target); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := removeInstallHooks(target); err != nil {
						t.Fatal(err)
					}
				}
				updated, err := readConfig(target.hookConfig)
				if err != nil {
					t.Fatal(err)
				}
				updatedEvents := hookObject(updated)
				if provider == host.ZCode {
					updatedEvents = zcodeHookEvents(zcodeHookRoot(updated))
				}
				for event, expected := range userEntries {
					entries, _ := updatedEvents[event].([]any)
					found := false
					for _, entry := range entries {
						found = found || reflect.DeepEqual(entry, expected)
					}
					if !found {
						t.Fatalf("user hook content changed for %s: expected %+v, got %+v", event, expected, entries)
					}
				}
				raw, err := json.Marshal(updated)
				if err != nil {
					t.Fatal(err)
				}
				text := string(raw)
				expected := len(retiredLifecycleHookEvents(descriptor.ID))
				captureMarker := "lifecycle capture"
				if provider == host.ZCode {
					captureMarker = `"lifecycle"`
				}
				if strings.Count(text, captureMarker) != expected || strings.Count(text, "user-launcher") != expected {
					t.Fatalf("owned lifecycle hook removal or user preservation failed: %s", raw)
				}
				if upgrade {
					gateMarker := "hook decide"
					if provider == host.ZCode {
						gateMarker = `"decide"`
					}
					if strings.Count(text, gateMarker) != 1 {
						t.Fatalf("write block gate missing or duplicated: %s", raw)
					}
				} else if strings.Contains(text, target.launcherPath) || strings.Contains(text, filepath.ToSlash(target.targetPath)+"/bin") {
					t.Fatalf("owned hooks remain: %s", raw)
				}
			})
		}
	}
}
