package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"formal-gates/internal/engine/testkit"
)

func devinTestHome(t *testing.T) (string, string) {
	t.Helper()
	home, configRoot := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", configRoot)
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	return home, filepath.Join(configRoot, "devin")
}

func TestDevinInstallPaths(t *testing.T) {
	home, config := devinTestHome(t)
	project := t.TempDir()
	for _, scope := range []string{"global", "project"} {
		targets, err := installTargets("devin", scope, project)
		if err != nil {
			t.Fatal(err)
		}
		base, rules := config, filepath.Join(config, "AGENTS.md")
		if scope == "project" {
			base, rules = filepath.Join(project, ".devin"), filepath.Join(project, "AGENTS.md")
		}
		target := targets[0]
		if target.targetPath != filepath.Join(base, "skills", "formal-gates") || target.hookConfig != filepath.Join(base, "config.json") || target.managedRulePath != rules || controlConfigPath(target) != filepath.Join(base, "mcp_config.json") {
			t.Fatalf("%s paths: %+v MCP=%s", scope, target, controlConfigPath(target))
		}
	}
	t.Setenv("APPDATA", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	base := filepath.Join(home, ".config", "devin")
	if runtime.GOOS == "windows" {
		base = filepath.Join(home, "AppData", "Roaming", "devin")
	}
	targets, err := installTargets("devin", "global", "")
	if err != nil || targets[0].targetPath != filepath.Join(base, "skills", "formal-gates") {
		t.Fatalf("fallback paths: %+v %v", targets, err)
	}
}

func TestDevinInstallationTransactions(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	source := copyPackageFixture(t)
	for _, scope := range []string{"project", "global"} {
		t.Run(scope, func(t *testing.T) {
			home, config := devinTestHome(t)
			project := t.TempDir()
			base, rules := config, filepath.Join(config, "AGENTS.md")
			if scope == "project" {
				base, rules = filepath.Join(project, ".devin"), filepath.Join(project, "AGENTS.md")
			}
			target := filepath.Join(base, "skills", "formal-gates")
			hooks, mcp := filepath.Join(base, "config.json"), filepath.Join(base, "mcp_config.json")
			hookOriginal := `{"theme":"dark","hooks":{"PreToolUse":[{"matcher":"exec","hooks":[{"type":"command","command":"user-hook"}]}]}}` + "\n"
			mcpOriginal := `{"custom":17,"mcpServers":{"other":{"command":"other","custom":true}}}` + "\n"
			ruleOriginal := "personal rules\n"
			mustWriteValidateTest(t, hooks, hookOriginal)
			mustWriteValidateTest(t, mcp, mcpOriginal)
			mustWriteValidateTest(t, rules, ruleOriginal)
			options := InstallOptions{Source: source, Host: "devin", Scope: scope, Project: project, Force: true, RegistryPath: filepath.Join(home, "registry.json"), BinaryTarget: filepath.Join(home, "stable", nativeBinaryName())}
			t.Setenv("FORMAL_GATES_INSTALL_FAULT", "registry-commit")
			if _, err := Install(options); err == nil {
				t.Fatal("injected failure did not roll back")
			}
			for path, want := range map[string]string{hooks: hookOriginal, mcp: mcpOriginal, rules: ruleOriginal} {
				assertDevinFile(t, path, []byte(want))
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatalf("rollback retained target: %v", err)
			}
			if _, err := os.Stat(options.BinaryTarget); !os.IsNotExist(err) {
				t.Fatalf("rollback retained launcher: %v", err)
			}
			t.Setenv("FORMAL_GATES_INSTALL_FAULT", "")
			report, err := Install(options)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Targets) != 1 || report.Targets[0].TargetPath != filepath.ToSlash(target) || report.Targets[0].ManagedRulePath != filepath.ToSlash(rules) || report.Targets[0].ControlConfig != filepath.ToSlash(mcp) {
				t.Fatalf("incorrect install paths: %+v", report.Targets)
			}
			for path, contains := range map[string]string{filepath.Join(target, "SKILL.md"): "<formal-gates:host-instructions:start>", rules: "<formal-gates:host-instructions:start>", hooks: "hook decide"} {
				body, err := os.ReadFile(path)
				if err != nil || !strings.Contains(string(body), contains) {
					t.Fatalf("missing installed content %s: %v %s", path, err, body)
				}
			}
			var control map[string]any
			mcpBytes, err := os.ReadFile(mcp)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(mcpBytes, &control); err != nil {
				t.Fatal(err)
			}
			server := control["mcpServers"].(map[string]any)["formal-gates"]
			want := map[string]any{"command": filepath.ToSlash(options.BinaryTarget), "args": []any{"workflow", "serve", "--package-root", filepath.ToSlash(target)}}
			if !reflect.DeepEqual(server, want) {
				t.Fatalf("not native stdio configuration: %#v", server)
			}
			var hooksConfig map[string]any
			hookBytes, _ := os.ReadFile(hooks)
			if err := json.Unmarshal(hookBytes, &hooksConfig); err != nil {
				t.Fatal(err)
			}
			entries := hooksConfig["hooks"].(map[string]any)["PreToolUse"].([]any)
			if len(entries) != 2 || entries[1].(map[string]any)["matcher"] != ".*" || !strings.Contains(string(hookBytes), "user-hook") {
				t.Fatalf("native nested hooks lost or duplicated: %s", hookBytes)
			}
			command := entries[1].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
			if !strings.Contains(normalizeHookCommand(command), "hook decide --provider devin") {
				t.Fatalf("native hook does not bind the Devin provider: %s", command)
			}
			ruleBytes, _ := os.ReadFile(rules)
			if _, err := Install(options); err != nil {
				t.Fatal(err)
			}
			for path, want := range map[string][]byte{hooks: hookBytes, mcp: mcpBytes, rules: ruleBytes} {
				assertDevinFile(t, path, want)
			}
			uninstall := UninstallOptions{Host: "devin", Scope: scope, Project: project, RegistryPath: options.RegistryPath}
			t.Setenv("FORMAL_GATES_INSTALL_FAULT", "registry-commit")
			if _, err := Uninstall(uninstall); err == nil {
				t.Fatal("injected uninstall failure did not roll back")
			}
			for path, want := range map[string][]byte{hooks: hookBytes, mcp: mcpBytes, rules: ruleBytes} {
				assertDevinFile(t, path, want)
			}
			t.Setenv("FORMAL_GATES_INSTALL_FAULT", "")
			if _, err := Uninstall(uninstall); err != nil {
				t.Fatal(err)
			}
			assertDevinFile(t, rules, []byte(ruleOriginal))
			for path, original := range map[string]string{hooks: hookOriginal, mcp: mcpOriginal} {
				body, err := os.ReadFile(path)
				var got, want any
				if err != nil || json.Unmarshal(body, &got) != nil || json.Unmarshal([]byte(original), &want) != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("uninstall damaged user configuration %s: %v %s", path, err, body)
				}
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatalf("uninstall retained target: %v", err)
			}
			if _, err := Uninstall(uninstall); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func assertDevinFile(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(want) {
		t.Fatalf("file bytes changed %s: %v\nwant=%s\ngot=%s", path, err, want, got)
	}
}

func TestDevinRefusesCommentedAndInvalidHookConfig(t *testing.T) {
	for name, original := range map[string]string{"comments": "{\n// keep comment\n\"theme\":\"dark\"\n}\n", "invalid": "{broken", "null": "null", "malformed-hooks": `{"hooks":[]}`} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			target := installTarget{host: "devin", targetPath: filepath.Join(root, "skills", "formal-gates"), hookConfig: filepath.Join(root, "config.json")}
			mustWriteValidateTest(t, target.hookConfig, original)
			if _, err := configureGenericInstallHook(target); err == nil || !strings.Contains(err.Error(), "refusing to touch it") {
				t.Fatalf("expected strict JSON refusal, got %v", err)
			}
			assertDevinFile(t, target.hookConfig, []byte(original))
		})
	}
}

func TestDevinInstallationRejectsConfigWithoutPartialWrites(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	source := copyPackageFixture(t)
	for _, scope := range []string{"project", "global"} {
		for _, kind := range []string{"commented-hooks", "external-server"} {
			t.Run(scope+"/"+kind, func(t *testing.T) {
				home, config := devinTestHome(t)
				project := t.TempDir()
				base, rules := config, filepath.Join(config, "AGENTS.md")
				if scope == "project" {
					base, rules = filepath.Join(project, ".devin"), filepath.Join(project, "AGENTS.md")
				}
				hookOriginal := `{"theme":"dark"}` + "\n"
				mcpOriginal := `{"mcpServers":{"other":{"command":"other"}}}` + "\n"
				if kind == "commented-hooks" {
					hookOriginal = "{\n// keep this comment\n\"theme\":\"dark\"\n}\n"
				} else {
					mcpOriginal = `{"mcpServers":{"formal-gates":{"command":"external"}}}` + "\n"
				}
				files := map[string]string{filepath.Join(base, "config.json"): hookOriginal, filepath.Join(base, "mcp_config.json"): mcpOriginal, rules: "personal\n"}
				for path, content := range files {
					mustWriteValidateTest(t, path, content)
				}
				_, err := Install(InstallOptions{Source: source, Host: "devin", Scope: scope, Project: project, Force: true, RegistryPath: filepath.Join(home, "registry.json"), BinaryTarget: filepath.Join(home, "stable", nativeBinaryName())})
				if err == nil {
					t.Fatal("expected configuration conflict")
				}
				for path, content := range files {
					assertDevinFile(t, path, []byte(content))
				}
				if _, err := os.Stat(filepath.Join(base, "skills", "formal-gates")); !os.IsNotExist(err) {
					t.Fatalf("failed install retained target: %v", err)
				}
				if _, err := os.Stat(filepath.Join(home, "stable", nativeBinaryName())); !os.IsNotExist(err) {
					t.Fatalf("failed install retained launcher: %v", err)
				}
			})
		}
	}
}

func TestDevinControlPreservesOwnedUnknownFieldsAndRejectsInvalidJSON(t *testing.T) {
	root := t.TempDir()
	target := installTarget{host: "devin", targetPath: filepath.Join(root, "skills", "formal-gates"), launcherPath: filepath.Join(root, "launcher")}
	path := controlConfigPath(target)
	original := fmt.Sprintf(`{"custom":true,"mcpServers":{"formal-gates":{"command":%q,"args":["workflow","serve","--package-root",%q],"env":{"CUSTOM":"keep"},"disabled":true},"other":{"url":"https://example.invalid"}}}`, filepath.ToSlash(target.launcherPath), filepath.ToSlash(target.targetPath))
	mustWriteValidateTest(t, path, original)
	if _, err := configureControlServer(target, false); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var got, want any
	if json.Unmarshal(raw, &got) != nil || json.Unmarshal([]byte(original), &want) != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("unknown fields changed: %s", raw)
	}
	if _, err := configureControlServer(target, true); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	want = map[string]any{"custom": true, "mcpServers": map[string]any{"other": map[string]any{"url": "https://example.invalid"}}}
	if json.Unmarshal(raw, &got) != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("uninstall damaged unrelated fields: %s", raw)
	}
	for _, original := range []string{"null", "[]", "{broken", "{\n// keep\n}", `{"mcpServers":[]}`} {
		mustWriteValidateTest(t, path, original)
		if _, err := configureControlServer(target, false); err == nil {
			t.Fatalf("malformed configuration accepted: %s", original)
		}
		assertDevinFile(t, path, []byte(original))
	}
}

func TestDevinNativeHookToolNames(t *testing.T) {
	root := t.TempDir()
	writeCurrentHookFixture(t, root, "hook", true, true, "", "owner")
	t.Setenv("DEVIN_PROJECT_DIR", root)
	for _, tool := range []string{"exec", "write", "edit"} {
		input := map[string]any{"file_path": filepath.Join(root, "app.go")}
		if tool == "exec" {
			input = map[string]any{"command": "git commit -m test"}
		}
		payload, _ := json.Marshal(map[string]any{"session_id": "owner", "tool_name": tool, "tool_input": input})
		decision, err := Hook(payload, "devin")
		if err != nil || decision.Decision != "block" || HookExitCode("devin", decision) != 2 {
			t.Fatalf("%s native write escaped: %+v %v", tool, decision, err)
		}
		response, _ := json.Marshal(HookResponse("devin", decision))
		if !strings.Contains(string(response), `"decision":"block"`) {
			t.Fatalf("missing native decision: %s", response)
		}
	}
}
