package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunInstallProjectCopiesRuntimeSubset(t *testing.T) {
	source := writeInstallSource(t, "source v1")
	project := t.TempDir()
	var stdout, stderr bytes.Buffer

	code := Run("formal-gates", []string{
		"install",
		"--source", source,
		"--host", "claude",
		"--scope", "project",
		"--project", project,
	}, IO{Stdout: &stdout, Stderr: &stderr})

	if code != 0 {
		t.Fatalf("expected install to pass, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	target := filepath.Join(project, ".claude", "skills", "formal-gates")
	assertFileContains(t, filepath.Join(target, "SKILL.md"), "source v1")
	if _, err := os.Stat(filepath.Join(target, "bin", installTestBinaryName())); err != nil {
		t.Fatalf("expected native binary copied: %v", err)
	}
	assertFileContains(t, filepath.Join(target, "assets", "showcase", "no-evidence-no-pass.svg"), "No evidence")
	assertFileContains(t, filepath.Join(target, "hooks", "pollution-patterns.json"), "exact_terms")
	assertNoScriptRuntimeFiles(t, target)
	for _, unexpected := range []string{
		filepath.Join(target, "hooks", "enforce-gate-sequence.ps1"),
		filepath.Join(target, "hooks", "capture-subagent-receipt.ps1"),
		filepath.Join(target, "scripts", "gate-workflow.ps1"),
		filepath.Join(target, "scripts", "complexity_gate.py"),
		filepath.Join(target, "examples", "package-validation-demo.ps1"),
	} {
		if _, err := os.Stat(unexpected); !os.IsNotExist(err) {
			t.Fatalf("native install copied script runtime file %s, err=%v", unexpected, err)
		}
	}
}

func TestRunInstallConfigureHooksUsesNativeBinaryCommands(t *testing.T) {
	for _, tc := range []struct {
		name       string
		host       string
		configRel  string
		preEvent   string
		startEvent string
		stopEvent  string
	}{
		{name: "claude", host: "claude", configRel: ".claude/settings.json", preEvent: "PreToolUse", startEvent: "SubagentStart", stopEvent: "SubagentStop"},
		{name: "codex", host: "codex", configRel: ".codex/hooks.json", preEvent: "PreToolUse", startEvent: "SubagentStart", stopEvent: "SubagentStop"},
		{name: "cursor", host: "cursor", configRel: ".cursor/hooks.json", preEvent: "preToolUse", startEvent: "subagentStart", stopEvent: "subagentStop"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := writeInstallSource(t, "source")
			project := t.TempDir()
			configPath := filepath.Join(project, filepath.FromSlash(tc.configRel))
			writeOldHookConfig(t, configPath, tc.host)
			var stdout, stderr bytes.Buffer

			code := Run("formal-gates", []string{
				"install",
				"--source", source,
				"--host", tc.host,
				"--scope", "project",
				"--project", project,
				"--configure-hooks",
			}, IO{Stdout: &stdout, Stderr: &stderr})

			if code != 0 {
				t.Fatalf("expected install to pass, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			raw := readFile(t, configPath)
			if strings.Contains(raw, ".ps1") {
				t.Fatalf("hook config still contains PowerShell command: %s", raw)
			}
			for _, expected := range []string{
				"keep-non-formal-hook",
				"bin",
				installTestBinaryName(),
				"hook decide",
				"receipt capture",
				"--provider",
				"--worktree",
			} {
				if !strings.Contains(raw, expected) {
					t.Fatalf("hook config missing %q: %s", expected, raw)
				}
			}
			hooks := readHooksMap(t, configPath)
			for _, event := range []string{tc.preEvent, tc.startEvent, tc.stopEvent} {
				if _, ok := hooks[event]; !ok {
					t.Fatalf("expected hook event %s in %s", event, raw)
				}
			}
		})
	}
}

func TestRunInstallRefusesExistingTargetWithoutForceAndReplacesWithForce(t *testing.T) {
	first := writeInstallSource(t, "source v1")
	second := writeInstallSource(t, "source v2")
	project := t.TempDir()
	target := filepath.Join(project, ".codex", "skills", "formal-gates")
	var stdout, stderr bytes.Buffer

	code := Run("formal-gates", []string{
		"install", "--source", first, "--host", "codex", "--scope", "project", "--project", project,
	}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("expected first install to pass, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	mustWriteCLI(t, filepath.Join(target, "old.txt"), "old\n")

	stdout.Reset()
	stderr.Reset()
	code = Run("formal-gates", []string{
		"install", "--source", second, "--host", "codex", "--scope", "project", "--project", project,
	}, IO{Stdout: &stdout, Stderr: &stderr})
	if code == 0 {
		t.Fatalf("expected existing target without --force to fail")
	}
	if _, err := os.Stat(filepath.Join(target, "old.txt")); err != nil {
		t.Fatalf("non-force install changed existing target: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run("formal-gates", []string{
		"install", "--source", second, "--host", "codex", "--scope", "project", "--project", project, "--force",
	}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("expected force install to pass, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(target, "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected force install to replace target, err=%v", err)
	}
	assertFileContains(t, filepath.Join(target, "SKILL.md"), "source v2")
}

func TestRunInstallGlobalUsesTemporaryHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	source := writeInstallSource(t, "global source")
	var stdout, stderr bytes.Buffer

	code := Run("formal-gates", []string{
		"install",
		"--source", source,
		"--host", "codex",
		"--scope", "global",
	}, IO{Stdout: &stdout, Stderr: &stderr})

	if code != 0 {
		t.Fatalf("expected global install to pass, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	assertFileContains(t, filepath.Join(home, ".codex", "skills", "formal-gates", "SKILL.md"), "global source")
}

func TestRunInstallRequiresBuiltNativeBinary(t *testing.T) {
	source := writeInstallSource(t, "source")
	if err := os.Remove(filepath.Join(source, "bin", installTestBinaryName())); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	var stderr bytes.Buffer

	code := Run("formal-gates", []string{
		"install",
		"--source", source,
		"--host", "claude",
		"--scope", "project",
		"--project", project,
	}, IO{Stderr: &stderr})

	if code == 0 {
		t.Fatal("expected install without native binary to fail")
	}
	if !strings.Contains(stderr.String(), "build it first") {
		t.Fatalf("expected build-first error, got %q", stderr.String())
	}
}

func writeInstallSource(t *testing.T, skillText string) string {
	t.Helper()
	source := t.TempDir()
	mustWriteCLI(t, filepath.Join(source, "SKILL.md"), skillText+"\n")
	mustWriteCLI(t, filepath.Join(source, "README.md"), "readme\n")
	mustWriteCLI(t, filepath.Join(source, "README_EN.md"), "readme en\n")
	mustWriteCLI(t, filepath.Join(source, "formal-gates.manifest.json"), `{"name":"formal-gates"}`+"\n")
	mustWriteCLI(t, filepath.Join(source, "go.mod"), "module formal-gates\n")
	mustWriteCLI(t, filepath.Join(source, ".github", "workflows", "portable-validation.yml"), "portable validation\n")
	mustWriteCLI(t, filepath.Join(source, "bin", installTestBinaryName()), "binary\n")
	if err := os.Chmod(filepath.Join(source, "bin", installTestBinaryName()), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"cmd", "internal", "agents", "examples", "hooks", "references", "assets", "assets/showcase", "scripts"} {
		if err := os.MkdirAll(filepath.Join(source, dir), 0o700); err != nil {
			t.Fatal(err)
		}
		mustWriteCLI(t, filepath.Join(source, dir, ".keep"), "keep\n")
	}
	mustWriteCLI(t, filepath.Join(source, "hooks", "pollution-patterns.json"), `{"regex_groups":[],"exact_terms":[]}`+"\n")
	mustWriteCLI(t, filepath.Join(source, "assets", "showcase", "no-evidence-no-pass.svg"), "<svg>No evidence</svg>\n")
	mustWriteCLI(t, filepath.Join(source, "hooks", "enforce-gate-sequence.ps1"), "legacy hook\n")
	mustWriteCLI(t, filepath.Join(source, "hooks", "capture-subagent-receipt.ps1"), "legacy receipt hook\n")
	mustWriteCLI(t, filepath.Join(source, "scripts", "gate-workflow.ps1"), "legacy workflow\n")
	mustWriteCLI(t, filepath.Join(source, "scripts", "complexity_gate.py"), "legacy python\n")
	mustWriteCLI(t, filepath.Join(source, "examples", "package-validation-demo.ps1"), "legacy demo\n")
	return source
}

func installTestBinaryName() string {
	if runtime.GOOS == "windows" {
		return "formal-gates.exe"
	}
	return "formal-gates"
}

func writeOldHookConfig(t *testing.T, path, host string) {
	t.Helper()
	if host == "cursor" {
		writeJSONFile(t, path, map[string]any{
			"version": 1,
			"hooks": map[string]any{
				"preToolUse": []any{
					map[string]any{"command": "keep-non-formal-hook"},
					map[string]any{"command": "pwsh -File hooks/enforce-gate-sequence.ps1"},
				},
			},
		})
		return
	}
	writeJSONFile(t, path, map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "keep",
					"hooks": []any{
						map[string]any{"type": "command", "command": "keep-non-formal-hook"},
					},
				},
				map[string]any{
					"matcher": "*",
					"hooks": []any{
						map[string]any{"type": "command", "command": "pwsh -File hooks/enforce-gate-sequence.ps1"},
					},
				},
			},
		},
	})
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	mustWriteCLI(t, path, string(data)+"\n")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readHooksMap(t *testing.T, path string) map[string]any {
	t.Helper()
	var config map[string]any
	if err := json.Unmarshal([]byte(readFile(t, path)), &config); err != nil {
		t.Fatal(err)
	}
	hooks, ok := config["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("expected hooks object in %s", readFile(t, path))
	}
	return hooks
}

func assertFileContains(t *testing.T, path, expected string) {
	t.Helper()
	if text := readFile(t, path); !strings.Contains(text, expected) {
		t.Fatalf("expected %s to contain %q, got %q", path, expected, text)
	}
}

func assertNoScriptRuntimeFiles(t *testing.T, root string) {
	t.Helper()
	var found []string
	scriptExts := map[string]bool{
		".ps1": true,
		".py":  true,
		".sh":  true,
		".bat": true,
		".cmd": true,
		".js":  true,
		".mjs": true,
		".cjs": true,
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if scriptExts[strings.ToLower(filepath.Ext(entry.Name()))] {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Fatalf("native install copied script runtime files: %v", found)
	}
}
