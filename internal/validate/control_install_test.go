package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestControlServerConfigurationOwnership(t *testing.T) {
	for _, platform := range []string{"codex", "zcode"} {
		for _, scope := range []string{"global", "project"} {
			t.Run(platform+"_"+scope, func(t *testing.T) {
				home, project := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
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
				if platform == "zcode" {
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
	for _, platform := range []string{"codex", "zcode"} {
		t.Run(platform, func(t *testing.T) {
			root := t.TempDir()
			target := installTarget{host: platform, targetPath: filepath.Join(root, "."+platform, "skills", "formal-gates"), launcherPath: filepath.Join(root, "launcher")}
			path := controlConfigPath(target)
			original := "[mcp_servers.\"formal-gates\"]\ncommand = \"user-owned\"\n"
			if platform == "zcode" {
				original = "{\"mcp\":{\"servers\":{\"formal-gates\":{\"command\":\"user-owned\"}}}}\n"
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

func TestControlServerSkipHooksPreservesHostConfig(t *testing.T) {
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
