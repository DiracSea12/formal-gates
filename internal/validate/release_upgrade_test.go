//go:build qastructure

package validate

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Release upgrades exercise the actual CLI and native transaction against an
// isolated home. Download fixtures supply local release artifacts only.
func TestReleaseUpgradeUsesNativeTransaction(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Unix release wrapper execution")
	}
	suffix := runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "darwin" {
		suffix = "macos-" + runtime.GOARCH
	}
	if suffix != "macos-arm64" && suffix != "macos-amd64" && suffix != "linux-amd64" {
		t.Skip("unsupported release platform")
	}
	repo := repoRootValidateTest(t)
	source := copyPackageFixture(t)
	binary := filepath.Join(source, "bin", nativeBinaryName())
	build := exec.Command("go", "build", "-o", binary, "./cmd/formal-gates")
	build.Dir = repo
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build native installer: %v\n%s", err, output)
	}
	artifacts := t.TempDir()
	archivePath := filepath.Join(artifacts, "source.zip")
	releaseSourceArchive(t, source, archivePath)
	asset := filepath.Join(artifacts, "formal-gates-"+suffix)
	installWriteFile(t, asset, installReadFile(t, binary), 0o700)
	canary := filepath.Join(artifacts, "portable-canary-"+suffix+".json")
	installWriteFile(t, canary, []byte("{}\n"), 0o600)
	checksums := filepath.Join(artifacts, "SHA256SUMS-"+suffix+".txt")
	installWriteFile(t, checksums, []byte(fmt.Sprintf("%x  %s\n%x  %s\n", sha256.Sum256(installReadFile(t, asset)), filepath.Base(asset), sha256.Sum256(installReadFile(t, canary)), filepath.Base(canary))), 0o600)
	fixtureBin := filepath.Join(artifacts, "bin")
	installWriteFile(t, filepath.Join(fixtureBin, "curl"), []byte(`#!/bin/sh
set -eu
url=""; output=""
while [ "$#" -gt 0 ]; do
  case "$1" in -o) output="$2"; shift 2 ;; -*) shift ;; *) url="$1"; shift ;; esac
done
case "$url" in *zipball*) input=source.zip ;; *) input=${url##*/} ;; esac
cp "$INSTALL_RELEASE_ARTIFACTS/$input" "$output"
`), 0o700)
	for _, kind := range []string{"regular", "symlink"} {
		for _, fault := range []string{"", "intent", "registry-commit"} {
			name := kind
			if fault != "" {
				name += "/" + fault
			}
			t.Run(name, func(t *testing.T) {
				home := t.TempDir()
				project := t.TempDir()
				launcher := filepath.Join(home, ".local", "bin", nativeBinaryName())
				oldBytes := []byte("#!/bin/sh\nprintf 'flag provided but not defined: -release-root\\n' >&2\nexit 2\n")
				if kind == "symlink" {
					oldBinary := filepath.Join(home, ".formal-gates", "releases", "installed", "bin", nativeBinaryName())
					installWriteFile(t, oldBinary, oldBytes, 0o700)
					if err := os.MkdirAll(filepath.Dir(launcher), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(oldBinary, launcher); err != nil {
						t.Fatal(err)
					}
				} else {
					installWriteFile(t, launcher, oldBytes, 0o700)
				}
				registry := filepath.Join(home, ".formal-gates", "registry.json")
				registryBefore := installWriteRegistry(t, registry, 4)
				target := filepath.Join(project, ".claude", "skills", "formal-gates")
				installWriteFile(t, filepath.Join(target, "owned.txt"), []byte("installed runtime\n"), 0o600)
				env := launcherChildEnvironment(map[string]string{
					"HOME": home, "USERPROFILE": home, "LOCALAPPDATA": home,
					"PATH":                 fixtureBin + string(os.PathListSeparator) + os.Getenv("PATH"),
					"FORMAL_GATES_VERSION": "vrelease", "INSTALL_RELEASE_ARTIFACTS": artifacts,
					"FORMAL_GATES_INSTALL_FAULT": fault,
				})
				run := func() ([]byte, error) {
					command := exec.Command("bash", filepath.Join(repo, "install.command"), "--host", "claude", "--scope", "project", "--project", project, "--force", "--skip-hooks")
					command.Env = env
					return command.CombinedOutput()
				}
				output, err := run()
				if fault != "" {
					if err == nil || !strings.Contains(string(output), "deterministic install fault injected at "+fault) {
						t.Fatalf("release upgrade did not reach native %s: %v\n%s", fault, err, output)
					}
					if !bytes.Equal(installReadFile(t, launcher), oldBytes) || !bytes.Equal(installReadFile(t, registry), registryBefore) || string(installReadFile(t, filepath.Join(target, "owned.txt"))) != "installed runtime\n" {
						t.Fatal("native rollback did not restore original launcher/runtime/registry bytes")
					}
					info, err := os.Lstat(launcher)
					if err != nil || (info.Mode()&os.ModeSymlink != 0) != (kind == "symlink") {
						t.Fatalf("rollback changed launcher kind: %+v %v", info, err)
					}
					var recovery installRecoveryReceipt
					if err := json.Unmarshal(installReadFile(t, installOuterJournalPath(registry)+".failure.json"), &recovery); err != nil {
						t.Fatal(err)
					}
					phase := "intent"
					if fault == "registry-commit" {
						phase = "smoke-passed"
					}
					if !recovery.Recovered || recovery.Outcome != "ROLLED_BACK" || recovery.Phase != phase || recovery.Generation != 5 || recovery.PackageDigest == "" || recovery.StableDigest != fmt.Sprintf("%x", sha256.Sum256(registryBefore)) {
						t.Fatalf("native rollback receipt is incomplete: %+v", recovery)
					}
					if _, err := os.Stat(filepath.Join(home, ".formal-gates", "releases", "release-"+suffix)); !os.IsNotExist(err) {
						t.Fatalf("failed upgrade left candidate release: %v", err)
					}
					installAssertNoTransactionArtifacts(t, registry, target)
					return
				}
				if err != nil {
					t.Fatalf("release upgrade failed: %v\n%s", err, output)
				}
				if !bytes.Equal(installReadFile(t, launcher), installReadFile(t, binary)) {
					t.Fatal("native publication did not install the release binary")
				}
				if info, err := os.Lstat(launcher); err != nil || !info.Mode().IsRegular() {
					t.Fatalf("published launcher is not a regular file: %+v %v", info, err)
				}
				var admission BootstrapReceipt
				if err := json.Unmarshal(installReadFile(t, registry+".bootstrap.json"), &admission); err != nil || !admission.Accepted || admission.StateCreated {
					t.Fatalf("stable bootstrap did not admit the installed release: %+v %v", admission, err)
				}
				if output, err := run(); err != nil {
					t.Fatalf("repeat release installation failed: %v\n%s", err, output)
				}
				if !bytes.Equal(installReadFile(t, launcher), installReadFile(t, binary)) {
					t.Fatal("repeat installation changed published binary identity")
				}
				installAssertNoTransactionArtifacts(t, registry, target)
			})
		}
	}
	// A release installer cannot gain bootstrap or arbitrary launcher rights.
	for _, change := range []string{"no-force", "no-release", "bootstrap", "source-mismatch", "custom-launcher", "no-launcher"} {
		t.Run(change, func(t *testing.T) {
			home := t.TempDir()
			launcher := filepath.Join(home, ".local", "bin", nativeBinaryName())
			if change != "no-launcher" {
				installWriteFile(t, launcher, []byte("existing launcher\n"), 0o700)
			}
			args := []string{"install", "--source", source, "--host", "claude", "--scope", "project", "--project", t.TempDir(), "--binary-target", launcher}
			if change != "no-force" {
				args = append(args, "--force")
			}
			if change != "no-release" {
				args = append(args, "--release-root", filepath.Join(home, ".formal-gates", "releases", "release"))
			}
			switch change {
			case "bootstrap":
				args = append(args, "--bootstrap")
			case "source-mismatch":
				args[2] = t.TempDir()
			case "custom-launcher":
				args = append(args, "--binary-target", filepath.Join(home, "custom", nativeBinaryName()))
			}
			command := exec.Command(binary, args...)
			command.Env = launcherChildEnvironment(map[string]string{"HOME": home, "USERPROFILE": home, "LOCALAPPDATA": home, "FORMAL_GATES_INSTALL_FAULT": ""})
			if output, err := command.CombinedOutput(); err == nil || !strings.Contains(string(output), "UNREGISTERED_INSTALL") {
				t.Fatalf("source binary gained %s mutation rights: %v\n%s", change, err, output)
			}
			if _, err := os.Stat(filepath.Join(home, ".formal-gates", "registry.json")); !os.IsNotExist(err) {
				t.Fatalf("rejected candidate wrote registry: %v", err)
			}
		})
	}
}

func releaseSourceArchive(t *testing.T, source, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		writer, err := archive.Create("package/" + filepath.ToSlash(relative))
		if err != nil {
			return err
		}
		_, err = writer.Write(installReadFile(t, path))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
