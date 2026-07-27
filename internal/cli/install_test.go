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
	assertFileContains(t, filepath.Join(target, "prompts", "reviewer-base.md"), "reviewer base")
	assertFileContains(t, filepath.Join(target, "prompts", "actions", "sample-action.md"), "sample action")
	assertFileContains(t, filepath.Join(target, "gates", "sample-gate.md"), "sample gate")
	assertNoScriptRuntimeFiles(t, target)
}

func TestRunInstallCopiesPromptCatalogForEveryHost(t *testing.T) {
	for _, tc := range []struct {
		host      string
		targetRel string
	}{
		{host: "claude", targetRel: ".claude/skills/formal-gates"},
		{host: "codex", targetRel: ".codex/skills/formal-gates"},
		{host: "cursor", targetRel: ".cursor/formal-gates"},
	} {
		t.Run(tc.host, func(t *testing.T) {
			source := writeInstallSource(t, "source")
			project := t.TempDir()
			var stdout, stderr bytes.Buffer
			code := Run("formal-gates", []string{
				"install", "--source", source, "--host", tc.host, "--scope", "project",
				"--project", project, "--skip-hooks",
			}, IO{Stdout: &stdout, Stderr: &stderr})
			if code != 0 {
				t.Fatalf("expected install to pass, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			target := filepath.Join(project, filepath.FromSlash(tc.targetRel))
			assertFileContains(t, filepath.Join(target, "prompts", "reviewer-base.md"), "reviewer base")
			assertFileContains(t, filepath.Join(target, "prompts", "actions", "sample-action.md"), "sample action")
			assertFileContains(t, filepath.Join(target, "gates", "sample-gate.md"), "sample gate")
		})
	}
}

func TestRunInstallConfiguresHooksByDefaultForEveryHostAndScope(t *testing.T) {
	for _, tc := range []struct {
		name      string
		host      string
		configRel string
		preEvent  string
		provider  string
		start     string
		stop      string
	}{
		{name: "claude", host: "claude", configRel: ".claude/settings.json", preEvent: "PreToolUse", provider: "claude-code", start: "SubagentStart", stop: "SubagentStop"},
		{name: "codex", host: "codex", configRel: ".codex/hooks.json", preEvent: "PreToolUse", provider: "codex", start: "SubagentStart", stop: "SubagentStop"},
		{name: "cursor", host: "cursor", configRel: ".cursor/hooks.json", preEvent: "preToolUse", provider: "cursor", start: "subagentStart", stop: "subagentStop"},
	} {
		for _, scope := range []string{"global", "project"} {
			t.Run(tc.name+"/"+scope, func(t *testing.T) {
				source := writeInstallSource(t, "source")
				installRoot := t.TempDir()
				args := []string{
					"install",
					"--source", source,
					"--host", tc.host,
					"--scope", scope,
				}
				if scope == "global" {
					t.Setenv("HOME", installRoot)
					t.Setenv("USERPROFILE", installRoot)
				} else {
					args = append(args, "--project", installRoot)
				}
				configPath := filepath.Join(installRoot, filepath.FromSlash(tc.configRel))
				writeOldHookConfig(t, configPath, tc.host)
				var stdout, stderr bytes.Buffer

				code := Run("formal-gates", args, IO{Stdout: &stdout, Stderr: &stderr})

				if code != 0 {
					t.Fatalf("expected install to pass, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
				}
				if !strings.Contains(stdout.String(), "formal-gates hooks configured for "+tc.host) {
					t.Fatalf("expected hook success output, got %q", stdout.String())
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
					"lifecycle capture",
					tc.provider,
				} {
					if !strings.Contains(raw, expected) {
						t.Fatalf("hook config missing %q: %s", expected, raw)
					}
				}
				if strings.Contains(raw, "receipt capture") {
					t.Fatalf("hook config still contains removed receipt lifecycle command: %s", raw)
				}
				hooks := readHooksMap(t, configPath)
				if _, ok := hooks[tc.preEvent]; !ok {
					t.Fatalf("expected hook event %s in %s", tc.preEvent, raw)
				}
				for _, event := range []string{tc.start, tc.stop} {
					if _, ok := hooks[event]; !ok {
						t.Fatalf("expected lifecycle hook event %s in %s", event, raw)
					}
				}
				for _, command := range hookCommands(hooks) {
					if strings.Contains(command, `\`) {
						t.Fatalf("hook command should use slash paths for shell compatibility: %s", command)
					}
				}
			})
		}
	}
}

func TestRunInstallConfiguresEverySelectedHostByDefault(t *testing.T) {
	source := writeInstallSource(t, "source")
	project := t.TempDir()
	var stdout, stderr bytes.Buffer

	code := Run("formal-gates", []string{
		"install", "--source", source, "--host", "both", "--scope", "project", "--project", project,
	}, IO{Stdout: &stdout, Stderr: &stderr})

	if code != 0 {
		t.Fatalf("expected install to pass, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, expected := range []string{
		filepath.Join(project, ".claude", "settings.json"),
		filepath.Join(project, ".codex", "hooks.json"),
	} {
		if _, err := os.Stat(expected); err != nil {
			t.Fatalf("expected selected-host hook config %s: %v", expected, err)
		}
	}
}

func TestRunInstallSkipHooksLeavesExistingConfigUntouched(t *testing.T) {
	source := writeInstallSource(t, "source")
	project := t.TempDir()
	configPath := filepath.Join(project, ".codex", "hooks.json")
	original := "{\n  \"custom\": \"unchanged\"\n}\n"
	mustWriteCLI(t, configPath, original)
	var stdout, stderr bytes.Buffer

	code := Run("formal-gates", []string{
		"install", "--source", source, "--host", "codex", "--scope", "project", "--project", project, "--skip-hooks",
	}, IO{Stdout: &stdout, Stderr: &stderr})

	if code != 0 {
		t.Fatalf("expected install to pass, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	assertFileContains(t, filepath.Join(project, ".codex", "skills", "formal-gates", "SKILL.md"), "source")
	if got := readFile(t, configPath); got != original {
		t.Fatalf("--skip-hooks changed hook config: got %q want %q", got, original)
	}
	if strings.Contains(stdout.String(), "hooks configured") {
		t.Fatalf("--skip-hooks reported hook configuration success: %q", stdout.String())
	}
}

func TestRunInstallRejectsRemovedConfigureHooksFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run("formal-gates", []string{"install", "--configure-hooks"}, IO{Stdout: &stdout, Stderr: &stderr})

	if code == 0 {
		t.Fatal("expected removed --configure-hooks flag to be rejected")
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -configure-hooks") {
		t.Fatalf("expected unknown flag error, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunInstallHookFailureDoesNotClaimInstallSuccess(t *testing.T) {
	source := writeInstallSource(t, "source")
	project := t.TempDir()
	configPath := filepath.Join(project, ".claude", "settings.json")
	mustWriteCLI(t, configPath, "not json\n")
	var stdout, stderr bytes.Buffer

	code := Run("formal-gates", []string{
		"install", "--source", source, "--host", "claude", "--scope", "project", "--project", project,
	}, IO{Stdout: &stdout, Stderr: &stderr})

	if code == 0 {
		t.Fatal("expected invalid hook config to fail install")
	}
	if stdout.Len() != 0 {
		t.Fatalf("failed install emitted success output: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "existing hook config is not valid JSON") {
		t.Fatalf("expected hook configuration error, got %q", stderr.String())
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
	for _, dir := range []string{"cmd", "internal", "agents", "prompts", "prompts/actions", "gates", "examples", "references"} {
		if err := os.MkdirAll(filepath.Join(source, dir), 0o700); err != nil {
			t.Fatal(err)
		}
		mustWriteCLI(t, filepath.Join(source, dir, ".keep"), "keep\n")
	}
	mustWriteCLI(t, filepath.Join(source, "prompts", "reviewer-base.md"), "reviewer base\n")
	mustWriteCLI(t, filepath.Join(source, "prompts", "actions", "sample-action.md"), "sample action\n")
	mustWriteCLI(t, filepath.Join(source, "gates", "sample-gate.md"), "sample gate\n")
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

func hookCommands(value any) []string {
	var commands []string
	switch typed := value.(type) {
	case map[string]any:
		if command, ok := typed["command"].(string); ok {
			commands = append(commands, command)
		}
		for _, nested := range typed {
			commands = append(commands, hookCommands(nested)...)
		}
	case []any:
		for _, item := range typed {
			commands = append(commands, hookCommands(item)...)
		}
	}
	return commands
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
