package harness

// Native transports consume the exact script bytes admitted by the public
// installer. These tests run the current source CLI and the installed credential
// helper with an isolated store; they do not start a host or a model.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"formal-gates/internal/validate"
)

func TestInstalledNativeHostScripts(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the source package")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	name := "formal-gates"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	candidate := supervisorBinary(t)
	receipt, err := validate.PackageReceipt(repository)
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	for _, entry := range receipt.Entries {
		rel := filepath.FromSlash(entry.Path)
		data, err := os.ReadFile(filepath.Join(repository, rel))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(source, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, os.FileMode(entry.Mode)); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "bin", name), data, 0o700); err != nil {
		t.Fatal(err)
	}
	sourceReceipt, err := validate.PackageReceipt(source)
	if err != nil {
		t.Fatal(err)
	}
	assets := make(map[string][]byte)
	for _, name := range []string{"native_dsh.mjs", "zcode_credential.mjs"} {
		rel := "internal/harness/" + name
		assets[rel], err = os.ReadFile(filepath.Join(source, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, host := range []string{"dsh", "zcode"} {
		for _, scope := range []string{"global", "project"} {
			t.Run(host+"/"+scope, func(t *testing.T) {
				t.Parallel()
				home, project := t.TempDir(), t.TempDir()
				launcher := filepath.Join(home, ".local", "bin", name)
				if err := os.MkdirAll(filepath.Dir(launcher), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(launcher, data, 0o700); err != nil {
					t.Fatal(err)
				}
				run := func(binary string, args ...string) []byte {
					t.Helper()
					cmd := exec.Command(binary, args...)
					cmd.Dir = project
					cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home,
						"AI_AGENT=", "CLAUDE_CODE_ENTRYPOINT=", "CODEX_HOME=", "CODEX_CLI_PATH=",
						"CURSOR_TRACE_ID=", "CURSOR_RUNTIME=", "DSH_HOME=", "DSH_PROJECT_DIR=")
					output, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("installed command %v: %v\n%s", args, err, output)
					}
					return output
				}
				args := []string{"install", "--source", source, "--host", host, "--scope", scope,
					"--binary-target", launcher, "--force", "--skip-hooks"}
				if scope == "project" {
					args = append(args, "--project", project)
				}
				// A release-root install stages the whole release package, while
				// each host target keeps only the declared runtime inventory.
				if host == "dsh" && scope == "project" {
					release := filepath.Join(t.TempDir(), "release")
					args = append(args, "--release-root", release)
				}
				run(launcher, args...)
				run(launcher, args...)
				if host == "dsh" && scope == "project" {
					args[2] = args[len(args)-1]
				}
				bootstrapSource, err := validate.PackageReceipt(args[2])
				if err != nil {
					t.Fatal(err)
				}
				run(launcher, append(args, "--bootstrap")...)
				registryPath := filepath.Join(home, ".formal-gates", "registry.json")
				registry, err := validate.LoadRegistry(registryPath)
				if err != nil || len(registry.Records) != 1 {
					t.Fatalf("actual registry: %+v err=%v", registry, err)
				}
				record := registry.Records[0]
				if record.PackageDigest != sourceReceipt.Digest || record.Host != host || record.Scope != scope {
					t.Fatalf("registered source or host identity differs: %+v", record)
				}
				target := filepath.FromSlash(record.Target)
				helper := filepath.Join(target, "internal", "harness", zcodeCredentialHelper)
				run(filepath.Join(target, "bin", name), "package", "validate", "--root", target)
				installed, err := validate.PackageReceipt(target, source)
				if err != nil || installed.Digest != record.InstalledDigest {
					t.Fatalf("installed receipt does not bind registered target: %+v err=%v", installed, err)
				}
				bootstrap, err := os.ReadFile(registryPath + ".bootstrap.json")
				if err != nil {
					t.Fatal(err)
				}
				var admitted validate.BootstrapReceipt
				if err := json.Unmarshal(bootstrap, &admitted); err != nil || !admitted.Accepted ||
					admitted.PackageDigest != bootstrapSource.Digest || len(admitted.Records) != 1 ||
					admitted.Records[0].ID != record.ID || admitted.Records[0].PackageDigest != record.PackageDigest ||
					admitted.Records[0].InstalledDigest != record.InstalledDigest {
					t.Fatalf("bootstrap did not admit installed identity: %+v err=%v", admitted, err)
				}
				artifact, err := os.ReadFile(registryPath + ".install.json")
				if err != nil {
					t.Fatal(err)
				}
				var installReceipt validate.InstallReport
				if err := json.Unmarshal(artifact, &installReceipt); err != nil || len(installReceipt.Targets) != 1 ||
					installReceipt.PackageDigest != record.PackageDigest || installReceipt.Targets[0].TargetPath != record.Target ||
					installReceipt.Targets[0].InstalledDigest != record.InstalledDigest || installReceipt.Targets[0].Smoke != "PASS" {
					t.Fatalf("install artifact does not bind registered target: %+v err=%v", installReceipt, err)
				}
				for rel, want := range assets {
					path := filepath.Join(target, filepath.FromSlash(rel))
					got, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("installed script differs from source %s: %v", rel, err)
					}
					digest := sha256.Sum256(want)
					for _, inventory := range []struct {
						name    string
						entries []validate.PackageEntry
					}{
						{"installed", installed.Entries},
						{"install artifact", installReceipt.Targets[0].Manifest},
					} {
						found := false
						for _, entry := range inventory.entries {
							if entry.Path == rel {
								found = true
								if entry.SHA256 != hex.EncodeToString(digest[:]) || entry.Size != int64(len(want)) {
									t.Fatalf("%s script receipt differs: %+v", inventory.name, entry)
								}
							}
						}
						if !found {
							t.Fatalf("%s receipt omits %s", inventory.name, rel)
						}
					}
					t.Logf("target=%s asset=%s sha256=%x bytes=%d", target, rel, digest, len(got))
				}
				for _, entry := range installed.Entries {
					if strings.HasPrefix(entry.Path, "runtime/") && assets[entry.Path] == nil {
						t.Fatalf("host target includes undeclared runtime payload %s", entry.Path)
					}
				}
				t.Run("credential-store", func(t *testing.T) {
					node, err := exec.LookPath("node")
					if err != nil {
						t.Skip("node is not available; script execution requires the host's Node runtime")
					}
					store := filepath.Join(t.TempDir(), "credentials.json")
					if err := os.WriteFile(store, []byte(`{"account-provider:alpha:identity":"local","account-provider:beta:identity":"local","account-provider:coding-plan:alpha:account:local:api-key":"credential-alpha","account-provider:coding-plan:beta:account:local:api-key":"credential-beta"}`), 0o600); err != nil {
						t.Fatal(err)
					}
					for _, provider := range []string{"alpha", "beta", "absent"} {
						output, err := exec.Command(node, helper, provider, store).CombinedOutput()
						if provider == "absent" {
							exit, ok := err.(*exec.ExitError)
							if !ok || exit.ExitCode() != 3 || len(output) != 0 {
								t.Fatalf("absent provider was not refused: err=%v output=%q", err, output)
							}
						} else if err != nil || string(output) != "credential-"+provider {
							t.Fatalf("installed helper did not read requested provider %s: %v", provider, err)
						}
					}
				})
			})
		}
	}
}
