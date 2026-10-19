package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/testkit"
)

func TestDevinInstallHelpIncludesExistingHosts(t *testing.T) {
	for _, command := range []string{"install", "uninstall"} {
		var stdout, stderr bytes.Buffer
		if code := Run("formal-gates", []string{command, "--help"}, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
			t.Fatalf("%s help failed: %d %s", command, code, stderr.String())
		}
		text := stdout.String() + stderr.String()
		for _, name := range []string{"claude", "codex", "cursor", "dsh", "zcode", "workbuddy", "devin"} {
			if !strings.Contains(text, name) {
				t.Fatalf("%s help lost host %s: %s", command, name, text)
			}
		}
	}
}

func TestRunInstallDevinNativeProjectAndGlobal(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	for _, scope := range []string{"project", "global"} {
		t.Run(scope, func(t *testing.T) {
			source := writeInstallSource(t, "Devin skill")
			home, project, configRoot := t.TempDir(), t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("APPDATA", configRoot)
			t.Setenv("XDG_CONFIG_HOME", configRoot)
			base, rules := filepath.Join(configRoot, "devin"), filepath.Join(configRoot, "devin", "AGENTS.md")
			if scope == "project" {
				base, rules = filepath.Join(project, ".devin"), filepath.Join(project, "AGENTS.md")
			}
			launcher := filepath.Join(home, ".local", "bin", installTestBinaryName())
			target := filepath.Join(base, "skills", "formal-gates")
			mcp := filepath.Join(base, "mcp_config.json")
			mustWriteCLI(t, mcp, `{"custom":true,"mcpServers":{"other":{"command":"other"}}}`+"\n")
			mustWriteCLI(t, rules, "personal rules\n")
			args := []string{"install", "--source", source, "--host", "devin", "--scope", scope, "--project", project, "--binary-target", launcher}
			for i := 0; i < 2; i++ {
				var stdout, stderr bytes.Buffer
				installArgs := append([]string(nil), args...)
				if i == 1 {
					installArgs = append(installArgs, "--force")
				}
				if code := runInstalledFixture(t, installArgs, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
					t.Fatalf("install --host devin --scope %s: code=%d stdout=%s stderr=%s", scope, code, stdout.String(), stderr.String())
				}
				assertFileContains(t, filepath.Join(target, "SKILL.md"), "Devin skill")
				assertFileContains(t, rules, testManagedRuleLatest)
				assertFileContains(t, filepath.Join(base, "config.json"), "hook decide")
				body, err := os.ReadFile(mcp)
				var config map[string]any
				if err != nil || json.Unmarshal(body, &config) != nil {
					t.Fatalf("invalid native MCP: %v %s", err, body)
				}
				want := map[string]any{"custom": true, "mcpServers": map[string]any{"other": map[string]any{"command": "other"}, "formal-gates": map[string]any{"command": filepath.ToSlash(launcher), "args": []any{"workflow", "serve", "--package-root", filepath.ToSlash(target)}}}}
				if !reflect.DeepEqual(config, want) {
					t.Fatalf("native MCP command/shape or unknown fields lost: %s", body)
				}
			}
			var stdout, stderr bytes.Buffer
			if code := runInstalledFixture(t, []string{"uninstall", "--host", "devin", "--scope", scope, "--project", project}, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
				t.Fatalf("uninstall devin: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			if body, err := os.ReadFile(rules); err != nil || string(body) != "personal rules\n" {
				t.Fatalf("uninstall changed user rules: %v %s", err, body)
			}
			if body, err := os.ReadFile(mcp); err != nil || strings.Contains(string(body), "formal-gates") || !strings.Contains(string(body), "other") {
				t.Fatalf("uninstall changed user MCP: %v %s", err, body)
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatalf("uninstall retained target: %v", err)
			}
		})
	}
}
