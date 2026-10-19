package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/testkit"
)

func TestControlServerConfigurationOwnership(t *testing.T) {
	for _, platform := range []string{"codex", "zcode", "devin"} {
		for _, scope := range []string{"global", "project"} {
			t.Run(platform+"_"+scope, func(t *testing.T) {
				home, project := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("APPDATA", home)
				t.Setenv("XDG_CONFIG_HOME", home)
				targets, err := installTargets(platform, scope, project)
				if err != nil {
					t.Fatal(err)
				}
				target := targets[0]
				target.launcherPath = filepath.Join(home, "stable launcher", nativeBinaryName())
				path := controlConfigPath(target)
				original := "model = \"user-choice\"\n[mcp_servers.other]\ncommand = \"other\"\n"
				if platform == "zcode" {
					original = "{\"theme\":\"dark\",\"mcp\":{\"servers\":{\"other\":{\"command\":\"other\"}}}}\n"
				}
				if platform == "devin" {
					original = "{\"theme\":\"dark\",\"mcpServers\":{\"other\":{\"command\":\"other\"}}}\n"
				}
				mustWriteValidateTest(t, path, original)
				if changed, err := configureControlServer(target, false); err != nil || !changed {
					t.Fatalf("configure: changed=%v error=%v", changed, err)
				}
				installed, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(installed), "workflow") || !strings.Contains(string(installed), "serve") || !strings.Contains(string(installed), filepath.ToSlash(target.launcherPath)) || !strings.Contains(string(installed), "other") {
					t.Fatalf("control command or unrelated server lost: %s", installed)
				}
				if changed, err := configureControlServer(target, false); err != nil || changed {
					t.Fatalf("repeat configure: changed=%v error=%v", changed, err)
				}
				if _, err := configureControlServer(target, true); err != nil {
					t.Fatal(err)
				}
				after, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if platform == "codex" && string(after) != original {
					t.Fatalf("uninstall changed unrelated TOML: %s", after)
				}
				if platform == "zcode" || platform == "devin" {
					var want, got any
					_ = json.Unmarshal([]byte(original), &want)
					_ = json.Unmarshal(after, &got)
					wantJSON, _ := json.Marshal(want)
					gotJSON, _ := json.Marshal(got)
					if string(wantJSON) != string(gotJSON) {
						t.Fatalf("uninstall changed unrelated JSON: %s", after)
					}
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if _, err := configureControlServer(target, false); err != nil {
					t.Fatal(err)
				}
				if _, err := configureControlServer(target, true); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("installer-only configuration remains: %v", err)
				}
			})
		}
	}
}

func TestControlServerConfigurationRefusesUnownedEntry(t *testing.T) {
	t.Parallel()
	for _, platform := range []string{"codex", "zcode", "devin"} {
		t.Run(platform, func(t *testing.T) {
			root := t.TempDir()
			target := installTarget{host: platform, targetPath: filepath.Join(root, "."+platform, "skills", "formal-gates"), launcherPath: filepath.Join(root, "launcher")}
			path := controlConfigPath(target)
			original := "[mcp_servers.\"formal-gates\"]\ncommand = \"user-owned\"\n"
			if platform == "zcode" {
				original = "{\"mcp\":{\"servers\":{\"formal-gates\":{\"command\":\"user-owned\"}}}}\n"
			}
			if platform == "devin" {
				original = "{\"mcpServers\":{\"formal-gates\":{\"command\":\"user-owned\"}}}\n"
			}
			mustWriteValidateTest(t, path, original)
			if _, err := configureControlServer(target, false); err == nil {
				t.Fatal("overwrote an unrelated entry")
			}
			if _, err := configureControlServer(target, true); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != original {
				t.Fatalf("unrelated entry changed: %v %s", err, after)
			}
		})
	}
}

func TestControlServerConfigurationOwnsBareServeArgs(t *testing.T) {
	t.Parallel()
	for _, platform := range []string{"zcode", "devin"} {
		t.Run(platform, func(t *testing.T) {
			root := t.TempDir()
			target := installTarget{host: platform, targetPath: filepath.Join(root, "."+platform, "skills", "formal-gates"), launcherPath: filepath.Join(root, "launcher")}
			path := controlConfigPath(target)
			original := fmt.Sprintf(`{"mcp":{"servers":{"formal-gates":{"type":"stdio","command":%q,"args":["workflow","serve"],"env":{"USER":"keep"}},"other":{"command":"other"}}}}`, filepath.ToSlash(target.launcherPath))
			if platform == "devin" {
				original = fmt.Sprintf(`{"mcpServers":{"formal-gates":{"command":%q,"args":["workflow","serve"],"env":{"USER":"keep"}},"other":{"command":"other"}}}`, filepath.ToSlash(target.launcherPath))
			}
			serversOf := func(t *testing.T) map[string]any {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var config map[string]any
				if err := json.Unmarshal(raw, &config); err != nil {
					t.Fatal(err)
				}
				if platform == "devin" {
					return config["mcpServers"].(map[string]any)
				}
				return config["mcp"].(map[string]any)["servers"].(map[string]any)
			}
			mustWriteValidateTest(t, path, original)
			if changed, err := configureControlServer(target, false); err != nil || !changed {
				t.Fatalf("install over bare-args entry: changed=%v error=%v", changed, err)
			}
			entry := serversOf(t)["formal-gates"].(map[string]any)
			if !reflect.DeepEqual(entry["args"], []any{"workflow", "serve", "--package-root", filepath.ToSlash(target.targetPath)}) || entry["env"].(map[string]any)["USER"] != "keep" {
				raw, _ := os.ReadFile(path)
				t.Fatalf("bare-args entry not migrated or user fields lost: %s", raw)
			}
			mustWriteValidateTest(t, path, original)
			if changed, err := configureControlServer(target, true); err != nil || !changed {
				t.Fatalf("uninstall of bare-args entry: changed=%v error=%v", changed, err)
			}
			servers := serversOf(t)
			if _, present := servers["formal-gates"]; present || servers["other"].(map[string]any)["command"] != "other" {
				raw, _ := os.ReadFile(path)
				t.Fatalf("uninstall kept the bare-args entry or damaged others: %s", raw)
			}
			for _, foreign := range []string{`"args":["workflow","bogus"]`, `"args":["workflow","serve","--package-root","other/root"]`} {
				mustWriteValidateTest(t, path, strings.Replace(original, `"args":["workflow","serve"]`, foreign, 1))
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := configureControlServer(target, false); err == nil || !strings.Contains(err.Error(), "is not owned by this installation") {
					t.Fatalf("foreign args accepted: %v", err)
				}
				if changed, err := configureControlServer(target, true); err != nil || changed {
					t.Fatalf("uninstall touched a foreign-args entry: changed=%v error=%v", changed, err)
				}
				assertDevinFile(t, path, before)
			}
		})
	}
}

func TestControlServerNativeShapeSnapshots(t *testing.T) {
	for _, platform := range []string{"codex", "zcode"} {
		t.Run(platform, func(t *testing.T) {
			root := t.TempDir()
			target := installTarget{host: platform, targetPath: filepath.Join(root, "skills", "formal-gates"), launcherPath: filepath.FromSlash("stable/formal-gates")}
			path := controlConfigPath(target)
			original := "model = \"user-choice\"\n"
			want := original + controlTOMLStart + "\n[mcp_servers.formal-gates]\ncommand = \"stable/formal-gates\"\nargs = [\"workflow\", \"serve\", \"--package-root\", \"" + filepath.ToSlash(target.targetPath) + "\"]\n" + controlTOMLEnd + "\n"
			if platform == "zcode" {
				original = "{\"theme\":\"dark\",\"mcp\":{\"custom\":true,\"servers\":{\"other\":{\"command\":\"other\"}}}}\n"
				want = "{\n  \"mcp\": {\n    \"custom\": true,\n    \"servers\": {\n      \"formal-gates\": {\n        \"args\": [\n          \"workflow\",\n          \"serve\",\n          \"--package-root\",\n          \"" + filepath.ToSlash(target.targetPath) + "\"\n        ],\n        \"command\": \"stable/formal-gates\",\n        \"type\": \"stdio\"\n      },\n      \"other\": {\n        \"command\": \"other\"\n      }\n    }\n  },\n  \"theme\": \"dark\"\n}\n"
			}
			mustWriteValidateTest(t, path, original)
			if _, err := configureControlServer(target, false); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(path)
			if err != nil || string(body) != want {
				t.Fatalf("native configuration changed: %v\nwant=%s\ngot=%s", err, want, body)
			}
			t.Logf("installed snapshot: %q", body)
			if _, err := configureControlServer(target, true); err != nil {
				t.Fatal(err)
			}
			if platform == "zcode" {
				original = "{\n  \"mcp\": {\n    \"custom\": true,\n    \"servers\": {\n      \"other\": {\n        \"command\": \"other\"\n      }\n    }\n  },\n  \"theme\": \"dark\"\n}\n"
			}
			body, err = os.ReadFile(path)
			if err != nil || string(body) != original {
				t.Fatalf("native uninstall changed: %v %s", err, body)
			}
			t.Logf("uninstalled snapshot: %q", body)
		})
	}
}

func TestControlServerSkipHooksPreservesHostConfig(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	source := copyPackageFixture(t)
	for _, platform := range []string{"codex", "zcode"} {
		t.Run(platform, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			targets, err := resolveInstallTargets(platform, "global", "")
			if err != nil {
				t.Fatal(err)
			}
			path := controlConfigPath(targets[0])
			original := "model=\"user-choice\"\n"
			if platform == "zcode" {
				original = "{\"theme\":\"dark\"}\n"
			}
			mustWriteValidateTest(t, path, original)
			options := InstallOptions{Source: source, Host: platform, Scope: "global", SkipHooks: true, Force: true, RegistryPath: filepath.Join(home, "registry.json"), BinaryTarget: filepath.Join(home, "stable", nativeBinaryName())}
			report, err := Install(options)
			if err != nil {
				t.Fatal(err)
			}
			if report.Targets[0].ControlAction != "SKIPPED" {
				t.Fatalf("incorrect skipped registration receipt: %+v", report.Targets[0])
			}
			if body, err := os.ReadFile(path); err != nil || string(body) != original {
				t.Fatalf("skip-hooks changed control configuration: %v %s", err, body)
			}
			if _, err := Uninstall(UninstallOptions{Host: platform, Scope: "global", RegistryPath: options.RegistryPath}); err != nil {
				t.Fatal(err)
			}
			if body, err := os.ReadFile(path); err != nil || string(body) != original {
				t.Fatalf("uninstall changed unowned control configuration: %v %s", err, body)
			}
		})
	}
}

func TestControlServerInstallRollbackAndUninstall(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	source := copyPackageFixture(t)
	for _, platform := range []string{"codex", "zcode"} {
		t.Run(platform, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			options := InstallOptions{Source: source, Host: platform, Scope: "global", Force: true, RegistryPath: filepath.Join(home, "registry.json"), BinaryTarget: filepath.Join(home, "stable", nativeBinaryName())}
			targets, err := resolveInstallTargets(platform, "global", "")
			if err != nil {
				t.Fatal(err)
			}
			path := controlConfigPath(targets[0])
			original := "model = \"user-choice\"\n"
			if platform == "zcode" {
				original = "{\"theme\":\"dark\"}\n"
			}
			mustWriteValidateTest(t, path, original)
			t.Setenv("FORMAL_GATES_INSTALL_FAULT", "registry-commit")
			if _, err := Install(options); err == nil {
				t.Fatal("expected injected install failure")
			}
			if body, err := os.ReadFile(path); err != nil || string(body) != original {
				t.Fatalf("rollback changed original control configuration: %v %s", err, body)
			}
			t.Setenv("FORMAL_GATES_INSTALL_FAULT", "")
			report, err := Install(options)
			if err != nil {
				t.Fatal(err)
			}
			if report.Targets[0].ControlConfig != filepath.ToSlash(path) || report.Targets[0].ControlDigest == "" {
				t.Fatalf("missing actual control configuration receipt: %+v", report.Targets[0])
			}
			installed, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("FORMAL_GATES_INSTALL_FAULT", "registry-commit")
			uninstall := UninstallOptions{Host: platform, Scope: "global", RegistryPath: options.RegistryPath}
			if _, err := Uninstall(uninstall); err == nil {
				t.Fatal("expected injected uninstall failure")
			}
			if body, err := os.ReadFile(path); err != nil || string(body) != string(installed) {
				t.Fatalf("uninstall rollback changed control configuration: %v %s", err, body)
			}
			t.Setenv("FORMAL_GATES_INSTALL_FAULT", "")
			if _, err := Uninstall(uninstall); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(path)
			if err != nil || strings.Contains(string(body), "workflow") || strings.Contains(string(body), controlTOMLStart) {
				t.Fatalf("owned control entry remains: %v %s", err, body)
			}
		})
	}
}
