package validate

import (
	"encoding/json"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/host"
	"os"
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

func TestCursorHookUninstallRemovesOnlyInstallerConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config map[string]any
	}{
		{name: "installer-only"},
		{name: "external-hook", config: map[string]any{"version": float64(1), "hooks": map[string]any{"external": []any{map[string]any{"command": "keep"}}}}},
		{name: "external-setting", config: map[string]any{"version": float64(1), "user": "keep", "hooks": map[string]any{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			target := installTarget{host: "cursor", targetPath: filepath.Join(root, "formal-gates"), launcherPath: filepath.Join(root, nativeBinaryName()), hookConfig: filepath.Join(root, "hooks.json")}
			expected, _ := json.Marshal(tc.config)
			if tc.config != nil {
				if err := writeHookConfig(target.hookConfig, tc.config); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := configureInstallHook(target); err != nil {
				t.Fatal(err)
			}
			if err := removeInstallHooks(target); err != nil {
				t.Fatal(err)
			}
			if tc.config == nil {
				if _, err := os.Stat(target.hookConfig); !os.IsNotExist(err) {
					t.Fatalf("installer-only hook configuration remains: %v", err)
				}
			} else {
				actual, err := readHookConfig(target.hookConfig)
				if err != nil {
					t.Fatal(err)
				}
				data, _ := json.Marshal(actual)
				if string(data) != string(expected) {
					t.Fatalf("external configuration changed: got %s want %s", data, expected)
				}
			}
			if err := removeInstallHooks(target); err != nil {
				t.Fatalf("repeated hook removal: %v", err)
			}
		})
	}
}

func TestUninstallPreservesSharedRuleAfterSkipHooksReinstall(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project := t.TempDir()
	registry := filepath.Join(home, ".formal-gates", "registry.json")
	options := InstallOptions{Source: copyPackageFixture(t), Host: "codex", Scope: "project", Project: project, RegistryPath: registry, Force: true}
	if _, err := Install(options); err != nil {
		t.Fatal(err)
	}
	options.Host = "dsh"
	if _, err := Install(options); err != nil {
		t.Fatal(err)
	}
	rulePath := filepath.Join(project, "AGENTS.md")
	before, err := os.ReadFile(rulePath)
	if err != nil {
		t.Fatal(err)
	}
	options.Host, options.SkipHooks = "codex", true
	if _, err := Install(options); err != nil {
		t.Fatal(err)
	}
	uninstall := UninstallOptions{Host: "dsh", Scope: "project", Project: project, RegistryPath: registry}
	t.Setenv("FORMAL_GATES_INSTALL_FAULT", "registry-commit")
	if _, err := Uninstall(uninstall); err == nil {
		t.Fatal("uninstall commit fault did not interrupt the transaction")
	}
	if current, err := os.ReadFile(rulePath); err != nil || string(current) != string(before) {
		t.Fatalf("rollback changed the shared rule: %v", err)
	}
	t.Setenv("FORMAL_GATES_INSTALL_FAULT", "")
	if _, err := Uninstall(uninstall); err != nil {
		t.Fatal(err)
	}
	if current, err := os.ReadFile(rulePath); err != nil || string(current) != string(before) {
		t.Fatalf("remaining installation lost its shared rule: %v", err)
	}
	doc, err := LoadRegistry(registry)
	if err != nil || len(doc.Records) != 1 || doc.Records[0].Host != "codex" || !doc.Records[0].SkipHooks {
		t.Fatalf("remaining installation record changed: %+v, %v", doc, err)
	}
	uninstall.Host = "codex"
	if _, err := Uninstall(uninstall); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rulePath); !os.IsNotExist(err) {
		t.Fatalf("last installation retained its owned rule: %v", err)
	}
}
