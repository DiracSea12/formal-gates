package validate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The endpoint represents a different package version's validator. Install must
// execute it before trusting that version's vocabulary, without a real install.
func TestInstallSourceVersionOwnsManifestVocabulary(t *testing.T) {
	dir := t.TempDir()
	program := `package main
import("os";"strings")
func main(){if len(os.Args)>2&&os.Args[1]=="package"&&os.Args[2]=="validate"{
 os.WriteFile(os.Getenv("FG_INSTALL_FIXTURE_TRACE"),[]byte(strings.Join(os.Args[1:]," ")),0600)
 if os.Getenv("FG_INSTALL_FIXTURE_REJECT")=="1"{os.Exit(17)}
}}
`
	sourceFile := filepath.Join(dir, "main.go")
	if err := os.WriteFile(sourceFile, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, nativeBinaryName())
	if out, err := exec.Command("go", "build", "-o", binary, sourceFile).CombinedOutput(); err != nil {
		t.Fatalf("validator fixture: %v %s", err, out)
	}
	payload, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                         string
		complete, registered, reject bool
		host, part, want             string
	}{
		{name: "complete-future-vocabulary", complete: true, registered: true, host: "claude", part: "future-package-part"},
		{name: "complete-validator-rejects", complete: true, registered: true, reject: true, host: "claude", want: "complete package validation"},
		{name: "complete-selected-host-unregistered", complete: true, host: "claude", want: "does not register"},
		{name: "runtime-only-future-host", registered: true, host: "claude", want: "unsupported host target"},
		{name: "runtime-only-future-part", registered: true, host: "claude", part: "future-package-part", want: "unsupported package target"},
		{name: "unknown-selected-host", complete: true, registered: true, host: "future-fixture-host", want: "unsupported --host"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := installerVersionFixture(t)
			if err := os.Remove(filepath.Join(source, "internal", "validate", "runner.go")); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			trace := filepath.Join(t.TempDir(), "validation.txt")
			t.Setenv("FG_INSTALL_FIXTURE_TRACE", trace)
			reject := "0"
			if tc.reject {
				reject = "1"
			}
			t.Setenv("FG_INSTALL_FIXTURE_REJECT", reject)
			if err := os.WriteFile(filepath.Join(source, "bin", nativeBinaryName()), payload, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(source, "bin", nativeBinaryName()), 0755); err != nil {
				t.Fatal(err)
			}
			var manifest map[string]any
			manifestData, readErr := os.ReadFile(filepath.Join(source, "formal-gates.manifest.json"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if err := json.Unmarshal(manifestData, &manifest); err != nil {
				t.Fatal(err)
			}
			hosts := manifest["hosts"].([]any)
			if !tc.registered {
				for _, item := range hosts {
					h := item.(map[string]any)
					if h["name"] == "Claude Code" {
						h["support"] = "explanation-level"
					}
				}
			}
			if tc.name != "runtime-only-future-part" {
				hosts = append(hosts, map[string]any{"name": "Future Fixture Host", "support": "host-target"})
			}
			manifest["hosts"] = hosts
			if tc.part != "" {
				manifest["package_parts"] = []string{tc.part}
			}
			data, _ := json.Marshal(manifest)
			if err := os.WriteFile(filepath.Join(source, "formal-gates.manifest.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if tc.complete {
				path := filepath.Join(source, "internal", "validate", "runner.go")
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("package validate\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			project := t.TempDir()
			registry := filepath.Join(t.TempDir(), "registry.json")
			launcher := filepath.Join(t.TempDir(), "bin", nativeBinaryName())
			report, err := Install(InstallOptions{Source: source, Host: tc.host, Scope: "project", Project: project, RegistryPath: registry, BinaryTarget: launcher, Force: true})
			if tc.want == "" {
				if err != nil || len(report.Targets) != 1 {
					t.Fatalf("new package vocabulary rejected: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("want %q, got %v", tc.want, err)
				}
				for _, path := range []string{filepath.Join(project, ".claude", "skills", "formal-gates"), registry, launcher} {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatalf("rejected install wrote %s: %v", path, err)
					}
				}
			}
			recorded, traceErr := os.ReadFile(trace)
			if tc.complete && tc.host == "claude" {
				if traceErr != nil || string(recorded) != "package validate --root "+source {
					t.Fatalf("source validator not actually invoked: %s %v", recorded, traceErr)
				}
			} else if !os.IsNotExist(traceErr) {
				t.Fatalf("unexpected complete validator call: %v", traceErr)
			}
		})
	}
}

// A self-contained runtime fixture also works in maintenance source archives
// that intentionally omit the repository's launcher scripts and Git history.
func installerVersionFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"SKILL.md":  "---\nname: formal-gates\n---\n" + hostInstructionsStartMarker + "\nfixture managed rule\n" + hostInstructionsEndMarker + "\n",
		"README.md": "fixture runtime\n", "README_EN.md": "fixture runtime\n",
		"formal-gates.manifest.json": `{"name":"formal-gates","hosts":[{"name":"Claude Code","support":"host-target"}]}`,
		"bin/" + nativeBinaryName():  "placeholder",
		"prompts/reviewer-base.md":   "fixture reviewer",
		"gates/fixture.md":           "fixture gate", "agents/fixture.md": "fixture agent", "references/fixture.md": "fixture reference",
	}
	for _, action := range []string{"carry", "development-worker", "product-review", "qa-design", "qa-execution", "qa-review", "requirements-clarification", "start-readiness"} {
		files["prompts/actions/"+action+".md"] = action + " prompt"
	}
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
