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

func TestDevinControlConfiguresSelectedScope(t *testing.T) {
	for _, scope := range []string{"project", "global"} {
		t.Run(scope, func(t *testing.T) {
			devinTestHome(t)
			project := t.TempDir()
			targets, err := installTargets("devin", scope, project)
			if err != nil {
				t.Fatal(err)
			}
			target := targets[0]
			target.launcherPath = filepath.Join(t.TempDir(), "launcher")
			path := controlConfigPath(target)
			other := "global"
			if scope == "global" {
				other = "project"
			}
			otherTargets, err := installTargets("devin", other, project)
			if err != nil {
				t.Fatal(err)
			}
			otherPath := controlConfigPath(otherTargets[0])
			owned := fmt.Sprintf(`{"custom":17,"mcpServers":{"formal-gates":{"command":%q,"args":["workflow","serve","--package-root",%q],"env":{"USER":"keep"},"disabled":true,"custom":17}}}`, filepath.ToSlash(target.launcherPath), filepath.ToSlash(target.targetPath))
			for name, original := range map[string]string{
				"missing":  "",
				"comments": "{\n// User comment\n\"custom\":17,\"mcpServers\":{}\n}\n",
				"external": `{"mcpServers":{"formal-gates":{"command":"external"}}}`,
				"matching": owned,
			} {
				t.Run(name, func(t *testing.T) {
					if original == "" {
						if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
							t.Fatal(err)
						}
					} else {
						mustWriteValidateTest(t, path, original)
					}
					changed, err := configureControlServer(target, false)
					switch name {
					case "external":
						if err == nil || changed || !strings.Contains(err.Error(), "is not owned by this installation") {
							t.Fatalf("external entry accepted: changed=%v error=%v", changed, err)
						}
						assertDevinFile(t, path, []byte(original))
						if changed, err := configureControlServer(target, true); err != nil || changed {
							t.Fatalf("uninstall touched an external entry: changed=%v error=%v", changed, err)
						}
						assertDevinFile(t, path, []byte(original))
					case "comments":
						if err == nil || changed {
							t.Fatalf("commented config accepted: changed=%v error=%v", changed, err)
						}
						assertDevinFile(t, path, []byte(original))
						if _, err := configureControlServer(target, true); err == nil {
							t.Fatal("uninstall accepted a commented config")
						}
						assertDevinFile(t, path, []byte(original))
					default:
						if err != nil || !changed {
							t.Fatalf("install: changed=%v error=%v", changed, err)
						}
						raw, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						var config map[string]any
						if err := json.Unmarshal(raw, &config); err != nil {
							t.Fatal(err)
						}
						entry := config["mcpServers"].(map[string]any)["formal-gates"].(map[string]any)
						if entry["command"] != filepath.ToSlash(target.launcherPath) || !reflect.DeepEqual(entry["args"], []any{"workflow", "serve", "--package-root", filepath.ToSlash(target.targetPath)}) {
							t.Fatalf("owned entry missing: %s", raw)
						}
						if name == "matching" && (config["custom"] != float64(17) || entry["env"].(map[string]any)["USER"] != "keep" || entry["disabled"] != true || entry["custom"] != float64(17)) {
							t.Fatalf("unknown fields lost: %s", raw)
						}
						if changed, err := configureControlServer(target, false); err != nil || changed {
							t.Fatalf("repeat install: changed=%v error=%v", changed, err)
						}
						if _, err := configureControlServer(target, true); err != nil {
							t.Fatal(err)
						}
						if name == "missing" {
							if _, err := os.Stat(path); !os.IsNotExist(err) {
								t.Fatalf("uninstall retained the config file: %v", err)
							}
						} else {
							raw, err := os.ReadFile(path)
							if err != nil {
								t.Fatal(err)
							}
							var got map[string]any
							if err := json.Unmarshal(raw, &got); err != nil || !reflect.DeepEqual(got, map[string]any{"custom": float64(17)}) {
								t.Fatalf("uninstall changed user fields: %v %s", err, raw)
							}
						}
					}
					if _, err := os.Stat(otherPath); !os.IsNotExist(err) {
						t.Fatalf("other scope config was created: %v", err)
					}
					if original != "" {
						if err := os.Remove(path); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		})
	}
}

func TestDevinHooksUseDocumentedNestedConfig(t *testing.T) {
	for _, scope := range []string{"project", "global"} {
		t.Run(scope, func(t *testing.T) {
			devinTestHome(t)
			targets, err := installTargets("devin", scope, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			target := targets[0]
			target.launcherPath = filepath.Join(t.TempDir(), "launcher")
			original := `{"custom":17,"hooks":{"PreToolUse":[{"matcher":"exec","hooks":[{"type":"command","command":"user-hook"}]}]}}` + "\n"
			mustWriteValidateTest(t, target.hookConfig, original)
			if changed, err := configureInstallHook(target); err != nil || !changed {
				t.Fatalf("configure hook: changed=%v error=%v", changed, err)
			}
			installed, err := os.ReadFile(target.hookConfig)
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err := json.Unmarshal(installed, &config); err != nil {
				t.Fatal(err)
			}
			hooks := config["hooks"].(map[string]any)["PreToolUse"].([]any)
			if config["custom"] != float64(17) || len(hooks) != 2 || !strings.Contains(string(installed), "user-hook") || !strings.Contains(string(installed), "hook decide --provider devin") {
				t.Fatalf("native hook entry or user data lost: %s", installed)
			}
			if changed, err := configureInstallHook(target); err != nil || changed {
				t.Fatalf("repeat hook: changed=%v error=%v", changed, err)
			}
			assertDevinFile(t, target.hookConfig, installed)
			for i := 0; i < 2; i++ {
				if err := removeInstallHooks(target); err != nil {
					t.Fatal(err)
				}
			}
			after, err := os.ReadFile(target.hookConfig)
			if err != nil {
				t.Fatal(err)
			}
			var want any
			if err := json.Unmarshal([]byte(original), &want); err != nil {
				t.Fatal(err)
			}
			var got any
			if err := json.Unmarshal(after, &got); err != nil {
				t.Fatal(err)
			}
			wantBytes, _ := json.Marshal(want)
			gotBytes, _ := json.Marshal(got)
			if string(wantBytes) != string(gotBytes) {
				t.Fatalf("uninstall changed user hooks: %s", after)
			}
			comments := "{\n// Valid Devin JSONC\n\"custom\":17\n}\n"
			mustWriteValidateTest(t, target.hookConfig, comments)
			_, err = configureInstallHook(target)
			if err == nil || !strings.Contains(err.Error(), "refusing to touch it") || strings.Contains(err.Error(), "UNSUPPORTED_DEVIN") {
				t.Fatalf("commented hook config was not refused: %v", err)
			}
			assertDevinFile(t, target.hookConfig, []byte(comments))
			if err := removeInstallHooks(target); err == nil || !strings.Contains(err.Error(), "refusing to touch it") || strings.Contains(err.Error(), "UNSUPPORTED_DEVIN") {
				t.Fatalf("commented hook config removal was not refused: %v", err)
			}
			assertDevinFile(t, target.hookConfig, []byte(comments))
		})
	}
}

func TestDevinInstallRollsBackOnCommentedControlConfig(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	source := copyPackageFixture(t)
	for _, scope := range []string{"project", "global"} {
		t.Run(scope, func(t *testing.T) {
			home, _ := devinTestHome(t)
			project := t.TempDir()
			targets, err := installTargets("devin", scope, project)
			if err != nil {
				t.Fatal(err)
			}
			target := targets[0]
			files := map[string]string{
				target.hookConfig:         "{\"custom\":17}\n",
				target.managedRulePath:    "personal rules\n",
				controlConfigPath(target): "{\n// User MCP comment\n\"mcpServers\":{}\n}\n",
			}
			for path, body := range files {
				mustWriteValidateTest(t, path, body)
			}
			options := InstallOptions{Source: source, Host: "devin", Scope: scope, Project: project, Force: true, RegistryPath: filepath.Join(home, "registry.json"), BinaryTarget: filepath.Join(home, "stable", nativeBinaryName())}
			for i := 0; i < 2; i++ {
				if _, err := Install(options); err == nil || !strings.Contains(err.Error(), "must be a JSON object") {
					t.Fatalf("commented MCP configuration was not refused: %v", err)
				}
				for path, body := range files {
					assertDevinFile(t, path, []byte(body))
				}
				for _, path := range []string{target.targetPath, options.BinaryTarget, options.RegistryPath, installOuterJournalPath(options.RegistryPath)} {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatalf("failed install retained %s: %v", path, err)
					}
				}
			}
		})
	}
}
