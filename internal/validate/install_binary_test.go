package validate

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/coordination"
)

func TestInstallBinaryPublicationOwnsRunningExecutable(t *testing.T) {
	const childAction = "FORMAL_GATES_INSTALL_BINARY_ACTION"
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if action := os.Getenv(childAction); action != "" {
		if action == "hold" {
			fmt.Fprintln(os.Stdout, "held")
			if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
				t.Fatal(err)
			}
			return
		}
		if action == "maintenance" {
			root := os.Getenv("FORMAL_GATES_INSTALL_BINARY_ROOT")
			registry := filepath.Join(root, "registry.json")
			unlock, err := coordination.AcquirePathWaiting(installLockPath(registry), "install/uninstall")
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()
			unlockRegistry, err := coordination.AcquirePathWaiting(registryLockPath(registry), "registry")
			if err != nil {
				t.Fatal(err)
			}
			defer unlockRegistry()
			defer func() {
				if err := retireUnusedInstallLocks(registry); err != nil {
					t.Error(err)
				}
			}()
			continueInstallBinaryCleanup(t, root)
			return
		}
		running, err := filepath.EvalSymlinks(executable)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(os.Stdout, running)
		root := filepath.Dir(executable)
		registry := filepath.Join(root, "registry.json")
		unlock, err := acquireInstallLock(registry)
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		unlockRegistry, err := acquireRegistryLock(registry)
		if err != nil {
			t.Fatal(err)
		}
		defer unlockRegistry()
		defer func() {
			if err := retireUnusedInstallLocks(registry); err != nil {
				t.Error(err)
			}
		}()
		transaction := filepath.Join(root, "transaction")
		binary, err := snapshotOuterFile(executable, filepath.Join(transaction, "binary.before"))
		if err != nil {
			t.Fatal(err)
		}
		journal := outerInstallJournal{Operation: "install", RegistryPath: registry, TransactionRoot: transaction, Phase: "smoke-passed", Binary: binary, Token: "launcher"}
		journalPath := installOuterJournalPath(registry)
		if err := persistOuterJournal(journalPath, journal); err != nil {
			t.Fatal(err)
		}
		if action == "gap" {
			completed := filepath.Join(root, ".formal-gates-binary-completed")
			retired := completed + ".retired"
			journal.Staged = []string{completed, retired}
			if err := persistOuterJournal(journalPath, journal); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(root, "candidate"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(completed, data, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(executable, retired); err != nil {
				t.Fatal(err)
			}
			os.Exit(0)
		}
		if action == "uninstall" {
			if err := retireOuterBinary(journalPath, &journal); err != nil {
				t.Fatal(err)
			}
		} else if err := publishOuterBinary(journalPath, &journal, filepath.Join(root, "candidate")); err != nil {
			t.Fatal(err)
		}
		if action == "recovery" {
			os.Exit(0)
		}
		if action == "rollback" || action == "recovery-continuation" {
			if err := restoreOuterJournal(journalPath, journal, false); err != nil {
				t.Fatal(err)
			}
			if action == "recovery-continuation" {
				continueInstallBinaryCleanup(t, root)
			}
		} else {
			journal.Phase = "committed"
			if err := persistOuterJournal(journalPath, journal); err != nil {
				t.Fatal(err)
			}
			if err := cleanupOuterJournal(journalPath, journal); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	t.Parallel()
	payload, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	testName := t.Name()
	for _, action := range []string{"publish", "rollback", "recovery", "recovery-continuation", "gap", "uninstall"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			destination := filepath.Join(root, "launcher"+filepath.Ext(executable))
			if err := os.WriteFile(destination, payload, 0o700); err != nil {
				t.Fatal(err)
			}
			expectedExecutable, err := filepath.EvalSymlinks(destination)
			if err != nil {
				t.Fatal(err)
			}
			content := []byte("completed launcher bytes\n")
			if err := os.WriteFile(filepath.Join(root, "candidate"), content, 0o700); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(destination, "-test.run=^"+testName+"$", "-test.timeout=0")
			command.Env = append(os.Environ(), childAction+"="+action)
			waitForCleanup := observeInstallCleanup(t, root)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("running launcher transaction: %v\n%s", err, output)
			}
			if running := strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0]); running != expectedExecutable {
				t.Fatalf("running launcher identity = %q, want %q", running, expectedExecutable)
			}
			registry := filepath.Join(root, "registry.json")
			if action == "recovery" || action == "gap" {
				if action == "gap" {
					if _, err := os.Stat(destination); !os.IsNotExist(err) {
						t.Fatalf("interrupted two-rename boundary retained launcher: %v", err)
					}
				}
				unlock, err := acquireInstallLock(registry)
				if err != nil {
					t.Fatal(err)
				}
				unlockRegistry, err := acquireRegistryLock(registry)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := reconcileOuterInstallJournal(registry); err != nil {
					t.Fatal(err)
				}
				if err := retireUnusedInstallLocks(registry); err != nil {
					t.Fatal(err)
				}
				unlockRegistry()
				unlock()
			}
			if action == "uninstall" {
				if _, err := os.Stat(destination); !os.IsNotExist(err) {
					t.Fatalf("uninstalled running launcher remains: %v", err)
				}
			} else {
				want := content
				if action != "publish" {
					want = payload
				}
				if got, err := os.ReadFile(destination); err != nil || !bytes.Equal(got, want) {
					t.Fatalf("launcher content differs from transaction result: %v", err)
				}
			}
			cleanupPaths := []string{installOuterJournalPath(registry), filepath.Join(root, "transaction"), filepath.Join(root, "continued"), registryLockPath(registry), installLockPath(registry)}
			waitForCleanup(cleanupPaths...)
			for _, path := range cleanupPaths {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("transaction path remains %s: %v", path, err)
				}
			}
			if remaining, err := filepath.Glob(filepath.Join(root, ".formal-gates-binary-*")); err != nil || len(remaining) != 0 {
				t.Fatalf("transaction executable files remain after old image exits: %v, %v", remaining, err)
			}
		})
	}
}

func continueInstallBinaryCleanup(t *testing.T, root string) {
	t.Helper()
	registry := filepath.Join(root, "registry.json")
	journalPath := installOuterJournalPath(registry)
	next := outerInstallJournal{Operation: "install", RegistryPath: registry, TransactionRoot: filepath.Join(root, "continued"), Phase: "committed", Token: "continued"}
	if err := inheritOuterCleanup(journalPath, &next); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(next.TransactionRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := persistOuterJournal(journalPath, next); err != nil {
		t.Fatal(err)
	}
	if err := cleanupOuterJournal(journalPath, next); err != nil {
		t.Fatal(err)
	}
}
