package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveZCodeHookPreservesSameLauncherUserHook(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, ".zcode", "cli", "config.json")
	target := installTarget{hookConfig: configPath, launcherPath: filepath.Join(home, ".zcode", "skills", "formal-gates", "bin", nativeBinaryName())}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{
		"hooks": map[string]any{
			"events": map[string]any{
				"PreToolUse": []any{map[string]any{
					"matcher": "*",
					"hooks": []any{
						map[string]any{"type": "process", "command": target.launcherPath, "args": []any{"hook", "custom"}},
						map[string]any{"type": "process", "command": target.launcherPath, "args": []any{"hook", "decide", "--provider", "zcode"}},
					},
				}},
			},
		},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeZCodeHook(target); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	if !strings.Contains(text, `"custom"`) {
		t.Fatalf("same-launcher user hook was removed: %s", text)
	}
	if strings.Contains(text, `"--provider","zcode"`) {
		t.Fatalf("managed ZCode hook remains: %s", text)
	}
}

func TestManagedRuleSharedByDeepSeekAndZCodeProjectInstalls(t *testing.T) {
	project := t.TempDir()
	managed := filepath.Join(project, "AGENTS.md")
	removing := []installTarget{{host: "zcode", targetPath: filepath.Join(project, ".zcode", "skills", "formal-gates")}}
	records := []RegistryRecord{{
		Target:      filepath.Join(project, ".dsh", "skills", "formal-gates"),
		Host:        "dsh",
		Scope:       "project",
		ProjectRoot: project,
		Status:      "active",
	}}
	if !managedRuleSharedByActiveInstall(managed, removing, records) {
		t.Fatal("active project DSH installation was not recognized as sharing AGENTS.md")
	}
	records[0].SkipHooks = true
	if managedRuleSharedByActiveInstall(managed, removing, records) {
		t.Fatal("project DSH skip-hooks installation incorrectly claimed AGENTS.md ownership")
	}
}
