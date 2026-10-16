package validate

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/testkit"
)

// The fixture is an executable package owner with a distinct file contract.
// It records actual binary paths, arguments, and working directories so source
// admission and installed-path validation cannot be replaced by a shared stub.
const installPackageContractProgram = `package main
import("encoding/json";"fmt";"os";"path/filepath";"strings")
func fail(err error) { if err != nil { fmt.Fprintln(os.Stderr,err); os.Exit(1) } }
func main() {
 executable,err:=os.Executable();fail(err);dir,err:=os.Getwd();fail(err)
 if path:=os.Getenv("INSTALL_CONTRACT_LOG");path!="" {
  file,err:=os.OpenFile(path,os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600);fail(err)
  data,err:=json.Marshal(map[string]any{"binary":executable,"dir":dir,"args":os.Args[1:]});fail(err)
  _,err=file.Write(append(data,'\n'));fail(err);fail(file.Close())
 }
 if len(os.Args)==2 && os.Args[1]=="--version" { fmt.Println("candidate-contract");return }
 if len(os.Args)!=5 || strings.Join(os.Args[1:4]," ")!="package validate --root" { fail(fmt.Errorf("unexpected protocol: %v",os.Args)) }
 root:=os.Args[4];if root!=dir || filepath.Join(root,"bin",filepath.Base(executable))!=executable { fail(fmt.Errorf("wrong input identity: %s %s %s",root,dir,executable)) }
 if observed:=os.Getenv("INSTALL_CONTRACT_JOURNAL_OBSERVED");observed!="" && root==os.Getenv("INSTALL_CONTRACT_RELEASE_ROOT") {
  data,err:=os.ReadFile(os.Getenv("INSTALL_CONTRACT_JOURNAL"));fail(err);fail(os.WriteFile(observed,data,0600))
  var journal struct { Binary struct { Path,Backup,Kind string } };fail(json.Unmarshal(data,&journal))
  path:=journal.Binary.Backup;if journal.Binary.Kind=="symlink" { path=journal.Binary.Path }
  data,err=os.ReadFile(path);fail(err);fail(os.WriteFile(observed+".binary",data,0600))
 }
 if root==os.Getenv("INSTALL_CONTRACT_REJECT_ROOT") { fail(fmt.Errorf("candidate rejects installed root")) }
 data,err:=os.ReadFile(filepath.Join(root,"formal-gates.manifest.json"));fail(err)
 var doc struct { Parts []string ` + "`json:\"package_parts\"`" + ` };fail(json.Unmarshal(data,&doc))
 for _,part:=range doc.Parts { _,err:=os.Stat(filepath.Join(root,filepath.FromSlash(strings.TrimSuffix(part,"/"))));fail(err) }
 data,err=os.ReadFile(filepath.Join(root,"contract.json"));fail(err)
 var contract map[string]string;fail(json.Unmarshal(data,&contract))
 for path,want:=range contract { got,err:=os.ReadFile(filepath.Join(root,filepath.FromSlash(path)));fail(err);if string(got)!=want { fail(fmt.Errorf("contract content mismatch: %s",path)) } }
 fmt.Println("PASS")
}`

type installPackageInvocation struct {
	Binary string   `json:"binary"`
	Dir    string   `json:"dir"`
	Args   []string `json:"args"`
}

func TestInstallUsesCandidatePackageContract(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	programRoot := t.TempDir()
	program := filepath.Join(programRoot, "main.go")
	if err := os.WriteFile(program, []byte(installPackageContractProgram), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(programRoot, nativeBinaryName())
	command := exec.Command("go", "build", "-o", binary, program)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build package owner: %v: %s", err, output)
	}
	payload, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	source := func(t *testing.T, future bool) string {
		root := copyPackageFixture(t)
		if err := os.WriteFile(filepath.Join(root, "bin", nativeBinaryName()), payload, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(root, "bin", nativeBinaryName()), 0o755); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(root, "formal-gates.manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		parts := doc["package_parts"].([]any)
		contract := map[string]string{}
		if future {
			path := "runtime/helpers/package_helper.mjs"
			mustWriteValidateTest(t, filepath.Join(root, filepath.FromSlash(path)), "future helper bytes\n")
			if err := os.Chmod(filepath.Join(root, filepath.FromSlash(path)), 0o644); err != nil {
				t.Fatal(err)
			}
			mustWriteValidateTest(t, filepath.Join(root, "runtime", "helpers", "unlisted.mjs"), "undeclared bytes\n")
			if err := os.MkdirAll(filepath.Join(root, "runtime", "empty"), 0o700); err != nil {
				t.Fatal(err)
			}
			parts = append(parts, path, "runtime/empty/")
			contract[path] = "future helper bytes\n"
		} else {
			filtered := []any{}
			for _, part := range parts {
				if strings.HasSuffix(part.(string), "native_dsh.mjs") || strings.HasSuffix(part.(string), "zcode_credential.mjs") {
					if err := os.Remove(filepath.Join(root, filepath.FromSlash(part.(string)))); err != nil {
						t.Fatal(err)
					}
				} else {
					filtered = append(filtered, part)
				}
			}
			parts = filtered
		}
		doc["package_parts"] = append(parts, "contract.json")
		data, err = json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		mustWriteValidateTest(t, filepath.Join(root, "formal-gates.manifest.json"), string(data))
		data, err = json.Marshal(contract)
		if err != nil {
			t.Fatal(err)
		}
		mustWriteValidateTest(t, filepath.Join(root, "contract.json"), string(data))
		if Package(root).OK() {
			t.Fatal("fixture does not differ from the installer's compiled package contract")
		}
		return root
	}
	t.Run("release_launcher_journal", func(t *testing.T) {
		for _, kind := range []string{"regular", "symlink"} {
			t.Run(kind, func(t *testing.T) {
				candidate := source(t, true)
				root := canonicalRegistryPath(t.TempDir())
				launcher := filepath.Join(root, "stable", nativeBinaryName())
				oldBinary := filepath.Join(root, "installed", nativeBinaryName())
				before := []byte("installed launcher bytes\n")
				if err := os.MkdirAll(filepath.Dir(launcher), 0o700); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink" {
					mustWriteValidateTest(t, oldBinary, string(before))
					if err := os.Symlink(oldBinary, launcher); err != nil {
						t.Skipf("symbolic links unavailable: %v", err)
					}
				} else {
					if err := os.WriteFile(launcher, before, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				release := filepath.Join(root, "release")
				registry := filepath.Join(root, "registry.json")
				observed := filepath.Join(root, "observed.json")
				t.Setenv("INSTALL_CONTRACT_JOURNAL", installOuterJournalPath(registry))
				t.Setenv("INSTALL_CONTRACT_RELEASE_ROOT", release)
				t.Setenv("INSTALL_CONTRACT_JOURNAL_OBSERVED", observed)
				if _, err := Install(InstallOptions{Source: candidate, Host: "claude", Scope: "project", Project: t.TempDir(), ReleaseRoot: release, RegistryPath: registry, BinaryTarget: launcher, Force: true, SkipHooks: true}); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(observed)
				if err != nil {
					t.Fatal(err)
				}
				var journal outerInstallJournal
				if err := json.Unmarshal(data, &journal); err != nil {
					t.Fatal(err)
				}
				if journal.Operation != "install" || journal.Phase != "switched" || journal.Binary.Path != launcher || !journal.Binary.Existed || journal.Binary.Kind != kind || journal.Release.Path != release || journal.RegistryPath != registry {
					t.Fatalf("release journal does not own original launcher identity: %+v", journal)
				}
				if kind == "symlink" && journal.Binary.LinkTarget != oldBinary {
					t.Fatalf("journal does not retain original launcher link: %+v", journal.Binary)
				}
				if kind == "regular" && (journal.Binary.Mode != 0o700 || journal.Binary.Backup == "") {
					t.Fatalf("journal does not retain original launcher file identity: %+v", journal.Binary)
				}
				data, err = os.ReadFile(observed + ".binary")
				if err != nil || !bytes.Equal(data, before) {
					t.Fatalf("journal snapshots original launcher bytes before publication: %q %v", data, err)
				}
			})
		}
	})
	t.Run("future_contract", func(t *testing.T) {
		t.Parallel()
		root := source(t, true)
		options := InstallOptions{Source: root, Host: "claude", Scope: "project", Project: t.TempDir(), RegistryPath: filepath.Join(t.TempDir(), "registry.json"), BinaryTarget: filepath.Join(t.TempDir(), "launcher", nativeBinaryName()), Force: true, SkipHooks: true}
		report, err := Install(options)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(report.Targets[0].TargetPath, "runtime", "helpers", "package_helper.mjs"))
		if err != nil || string(got) != "future helper bytes\n" {
			t.Fatalf("future contract helper bytes missing: %q %v", got, err)
		}
		if info, err := os.Stat(filepath.Join(report.Targets[0].TargetPath, "runtime", "empty")); err != nil || !info.IsDir() {
			t.Fatalf("declared empty directory was not copied: %v", err)
		}

		if _, err := os.Stat(filepath.Join(report.Targets[0].TargetPath, "runtime", "helpers", "unlisted.mjs")); !os.IsNotExist(err) {
			t.Fatalf("undeclared runtime file was installed: %v", err)
		}

	})
	t.Run("upgrade_repeat_restore", func(t *testing.T) {
		log := filepath.Join(t.TempDir(), "calls.jsonl")
		t.Setenv("INSTALL_CONTRACT_LOG", log)
		prior, future := source(t, false), source(t, true)
		options := InstallOptions{Host: "claude", Scope: "project", Project: t.TempDir(), RegistryPath: filepath.Join(t.TempDir(), "registry.json"), BinaryTarget: filepath.Join(t.TempDir(), "launcher", nativeBinaryName()), Force: true, SkipHooks: true}
		for _, candidate := range []string{prior, future, future, prior} {
			options.Source = candidate
			if err := os.WriteFile(log, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			report, err := Install(options)
			if err != nil {
				t.Fatal(err)
			}
			target := canonicalRegistryPath(report.Targets[0].TargetPath)
			expected, err := installPackageFiles(candidate, true)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := PackageReceipt(target, candidate)
			if err != nil {
				t.Fatal(err)
			}
			if !sameInstallPackageFiles(expected, actual.Entries) {
				t.Fatal("installed paths, modes, lengths or hashes differ from the source manifest")
			}
			if actual.Digest != report.Targets[0].InstalledDigest {
				t.Fatal("installed receipt does not bind actual bytes")
			}
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			var invocations []installPackageInvocation
			for _, line := range bytes.Split(bytes.TrimSpace(calls), []byte("\n")) {
				var call installPackageInvocation
				if err := json.Unmarshal(line, &call); err != nil {
					t.Fatal(err)
				}
				invocations = append(invocations, call)
			}
			if len(invocations) != 4 {
				t.Fatalf("source/target validation, installed smoke, launcher smoke calls=%+v", invocations)
			}
			for index, root := range []string{candidate, target} {
				call := invocations[index]
				if call.Binary != filepath.Join(root, "bin", nativeBinaryName()) || call.Dir != root || strings.Join(call.Args, " ") != "package validate --root "+root {
					t.Fatalf("wrong validation input identity: %+v", call)
				}
			}
			if invocations[2].Binary != filepath.Join(target, "bin", nativeBinaryName()) || invocations[3].Binary != stableLauncherPath(options.BinaryTarget) || strings.Join(invocations[2].Args, " ") != "--version" || strings.Join(invocations[3].Args, " ") != "--version" {
				t.Fatalf("smoke did not execute actual installed binaries: %+v", invocations)
			}
			path := filepath.Join(target, "runtime", "helpers", "package_helper.mjs")
			if candidate == future {
				got, err := os.ReadFile(path)
				if err != nil || string(got) != "future helper bytes\n" {
					t.Fatalf("future runtime bytes missing: %q %v", got, err)
				}
			} else if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("restoration retained future runtime file: %v", err)
			}
			if candidate == prior {
				if _, err := os.Stat(filepath.Join(target, "runtime", "empty")); !os.IsNotExist(err) {
					t.Fatalf("restoration retained future empty directory: %v", err)
				}
			}
		}
	})
	t.Run("installed_contract_failure_rolls_back", func(t *testing.T) {
		prior, future := source(t, false), source(t, true)
		options := InstallOptions{Source: prior, Host: "claude", Scope: "project", Project: t.TempDir(), RegistryPath: filepath.Join(t.TempDir(), "registry.json"), BinaryTarget: filepath.Join(t.TempDir(), "launcher", nativeBinaryName()), Force: true, SkipHooks: true}
		report, err := Install(options)
		if err != nil {
			t.Fatal(err)
		}
		target := canonicalRegistryPath(report.Targets[0].TargetPath)
		before, err := PackageReceipt(target)
		if err != nil {
			t.Fatal(err)
		}
		registryBefore, err := os.ReadFile(options.RegistryPath)
		if err != nil {
			t.Fatal(err)
		}
		launcherBefore, err := os.ReadFile(options.BinaryTarget)
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("INSTALL_CONTRACT_REJECT_ROOT", target)
		options.Source = future
		if _, err := Install(options); err == nil || !strings.Contains(err.Error(), "candidate rejects installed root") {
			t.Fatalf("actual installed validation failure did not reject: %v", err)
		}
		after, err := PackageReceipt(target)
		if err != nil || before.Digest != after.Digest {
			t.Fatalf("rollback changed target bytes: %v", err)
		}
		registryAfter, err := os.ReadFile(options.RegistryPath)
		if err != nil || !bytes.Equal(registryBefore, registryAfter) {
			t.Fatalf("rollback changed registry identity: %v", err)
		}
		launcherAfter, err := os.ReadFile(options.BinaryTarget)
		if err != nil || !bytes.Equal(launcherBefore, launcherAfter) {
			t.Fatalf("rollback changed stable launcher: %v", err)
		}
		t.Setenv("INSTALL_CONTRACT_REJECT_ROOT", "")
		if _, err := Install(options); err != nil {
			t.Fatalf("replacement after rollback: %v", err)
		}
	})
	t.Run("committed_response_loss_preserves_candidate", func(t *testing.T) {
		prior, future := source(t, false), source(t, true)
		options := InstallOptions{Source: prior, Host: "claude", Scope: "project", Project: t.TempDir(), RegistryPath: filepath.Join(t.TempDir(), "registry.json"), BinaryTarget: filepath.Join(t.TempDir(), "launcher", nativeBinaryName()), Force: true, SkipHooks: true}
		report, err := Install(options)
		if err != nil {
			t.Fatal(err)
		}
		target := canonicalRegistryPath(report.Targets[0].TargetPath)
		transactionRoot := filepath.Join(t.TempDir(), "transaction")
		tree, err := snapshotInstallTree(target, filepath.Join(transactionRoot, "target"))
		if err != nil {
			t.Fatal(err)
		}
		registrySnapshot, err := snapshotOuterFile(options.RegistryPath, filepath.Join(transactionRoot, "registry"))
		if err != nil {
			t.Fatal(err)
		}
		options.Source = future
		if _, err := Install(options); err != nil {
			t.Fatal(err)
		}
		committed, err := PackageReceipt(target)
		if err != nil {
			t.Fatal(err)
		}
		registryBefore, err := os.ReadFile(options.RegistryPath)
		if err != nil {
			t.Fatal(err)
		}
		// Response loss leaves a committed journal after the authoritative
		// registry and target writes. Recovery must sweep its undo material.
		journal := outerInstallJournal{Operation: "install", RegistryPath: options.RegistryPath, TransactionRoot: transactionRoot, Phase: "committed", Registry: registrySnapshot, Targets: []outerTargetSnapshot{{TargetPath: target, Tree: outerTreeFromBackup(tree)}}}
		journalPath := installOuterJournalPath(options.RegistryPath)
		if err := persistOuterJournal(journalPath, journal); err != nil {
			t.Fatal(err)
		}
		t.Setenv("FORMAL_GATES_INSTALL_FAULT", "intent")
		if _, err := Install(options); err == nil || !strings.Contains(err.Error(), "deterministic install fault") {
			t.Fatalf("restart did not reconcile before new install: %v", err)
		}
		after, err := PackageReceipt(target)
		if err != nil || after.Digest != committed.Digest {
			t.Fatalf("response-loss recovery restored obsolete package: %v", err)
		}
		registryAfter, err := os.ReadFile(options.RegistryPath)
		if err != nil || !bytes.Equal(registryBefore, registryAfter) {
			t.Fatalf("response-loss recovery changed committed registry: %v", err)
		}
		if _, err := os.Stat(transactionRoot); !os.IsNotExist(err) {
			t.Fatalf("committed undo tree remains: %v", err)
		}
		if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
			t.Fatalf("committed journal remains: %v", err)
		}
	})
	t.Run("bootstrap_rejects_another_version", func(t *testing.T) {
		t.Parallel()
		prior, future := source(t, false), source(t, true)
		options := InstallOptions{Source: future, Host: "claude", Scope: "project", Project: t.TempDir(), RegistryPath: filepath.Join(t.TempDir(), "registry.json"), BinaryTarget: filepath.Join(t.TempDir(), "launcher", nativeBinaryName()), Force: true, SkipHooks: true}
		report, err := Install(options)
		if err != nil {
			t.Fatal(err)
		}
		target := report.Targets[0].TargetPath
		before, err := PackageReceipt(target)
		if err != nil {
			t.Fatal(err)
		}
		registryBefore, err := os.ReadFile(options.RegistryPath)
		if err != nil {
			t.Fatal(err)
		}
		options.Source, options.Bootstrap = prior, true
		if _, err := Install(options); err == nil || !strings.Contains(err.Error(), "bootstrap target differs from source package") {
			t.Fatalf("bootstrap admitted another source version: %v", err)
		}
		after, err := PackageReceipt(target)
		if err != nil || after.Digest != before.Digest {
			t.Fatalf("bootstrap rejection changed installed package: %v", err)
		}
		registryAfter, err := os.ReadFile(options.RegistryPath)
		if err != nil || !bytes.Equal(registryBefore, registryAfter) {
			t.Fatalf("bootstrap rejection changed registry: %v", err)
		}
	})
	t.Run("checkout_ignored_binary_is_source_identity", func(t *testing.T) {
		t.Parallel()
		root := source(t, true)
		command := exec.Command("git", "-C", root, "init")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("initialize checkout: %v: %s", err, output)
		}
		mustWriteValidateTest(t, filepath.Join(root, ".gitignore"), "bin/\nruntime/helpers/\n")
		receipt, err := PackageReceipt(root)
		if err != nil {
			t.Fatal(err)
		}
		foundBinary, foundHelper := false, false
		for _, entry := range receipt.Entries {
			if entry.Path == "bin/"+nativeBinaryName() {
				foundBinary = true
			}
			if entry.Path == "runtime/helpers/package_helper.mjs" {
				foundHelper = true
			}
			if entry.Path == "runtime/helpers/unlisted.mjs" {
				t.Fatal("a declared file included its unrelated ignored sibling")
			}
		}
		if !foundBinary || !foundHelper {
			t.Fatalf("manifest-declared ignored files absent from source identity: binary=%v helper=%v", foundBinary, foundHelper)
		}
		options := InstallOptions{Source: root, Host: "claude", Scope: "project", Project: t.TempDir(), RegistryPath: filepath.Join(t.TempDir(), "registry.json"), BinaryTarget: filepath.Join(t.TempDir(), "launcher", nativeBinaryName()), Force: true, SkipHooks: true}
		report, err := Install(options)
		if err != nil {
			t.Fatal(err)
		}
		target := report.Targets[0].TargetPath
		expected, err := installPackageFiles(root, true)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := PackageReceipt(target)
		if err != nil || !sameInstallPackageFiles(expected, actual.Entries) {
			t.Fatalf("checkout source identity differs from actual installed bytes: %v", err)
		}
		if report.PackageDigest != receipt.Digest || report.Targets[0].InstalledDigest != actual.Digest {
			t.Fatal("receipt did not bind source and installed identities")
		}
	})
	t.Run("candidate_success_does_not_admit_unknown_host", func(t *testing.T) {
		t.Parallel()
		root := source(t, true)
		options := InstallOptions{Source: root, Host: "unknown", Scope: "project", Project: t.TempDir(), RegistryPath: filepath.Join(t.TempDir(), "registry.json")}
		if _, err := Install(options); err == nil || !strings.Contains(err.Error(), "unsupported --host") {
			t.Fatalf("unknown host was admitted: %v", err)
		}
		if _, err := os.Stat(options.RegistryPath); !os.IsNotExist(err) {
			t.Fatalf("unknown host wrote registry: %v", err)
		}
	})
}
