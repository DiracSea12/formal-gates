package validate

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestInstallCleanupReleasesLocksWhileOldHostUsesExecutable(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	launcher := filepath.Join(root, "launcher.exe")
	if err := os.WriteFile(launcher, payload, 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("complete new launcher\n")
	if err := os.WriteFile(filepath.Join(root, "candidate"), content, 0o700); err != nil {
		t.Fatal(err)
	}
	child := func(binary, action string) *exec.Cmd {
		command := exec.Command(binary, "-test.run=^TestInstallBinaryPublicationOwnsRunningExecutable$", "-test.timeout=0")
		command.Env = append(os.Environ(), "FORMAL_GATES_INSTALL_BINARY_ACTION="+action, "FORMAL_GATES_INSTALL_BINARY_ROOT="+root)
		return command
	}
	host := child(launcher, "hold")
	input, err := host.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	hostOutput, err := host.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var hostErrors bytes.Buffer
	host.Stderr = &hostErrors
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	defer host.Process.Kill()
	if ready, err := bufio.NewReader(hostOutput).ReadString('\n'); err != nil || strings.TrimSpace(ready) != "held" {
		t.Fatalf("old host handshake = %q, %v", ready, err)
	}
	waitForCleanup := observeInstallCleanup(t, root)
	installedOutput, err := child(launcher, "publish").CombinedOutput()
	if err != nil {
		t.Fatalf("captured install caller while old host lives: %v\n%s", err, installedOutput)
	}
	maintainedOutput, err := child(executable, "maintenance").CombinedOutput()
	if err != nil {
		t.Fatalf("captured maintenance caller while old host lives: %v\n%s", err, maintainedOutput)
	}
	if !strings.HasSuffix(strings.TrimSpace(string(installedOutput)), "PASS") {
		t.Fatalf("captured install output: %s", installedOutput)
	}
	registry := filepath.Join(root, "registry.json")
	journalPath := installOuterJournalPath(registry)
	data, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var current outerInstallJournal
	if err := json.Unmarshal(data, &current); err != nil {
		t.Fatal(err)
	}
	if current.Token != "continued" || current.Phase != "committed" {
		t.Fatalf("old cleanup changed the new transaction: %+v", current)
	}
	if got, err := os.ReadFile(launcher); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("live launcher bytes changed: %v", err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := host.Wait(); err != nil {
		t.Fatalf("old host natural exit: %v\n%s", err, hostErrors.Bytes())
	}
	if strings.TrimSpace(string(maintainedOutput)) != "PASS" {
		t.Fatalf("captured maintenance output: %s", maintainedOutput)
	}
	cleanupPaths := []string{journalPath, filepath.Join(root, "transaction"), filepath.Join(root, "continued"), installLockPath(registry), registryLockPath(registry)}
	waitForCleanup(cleanupPaths...)
	for _, path := range cleanupPaths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("completed cleanup retains %s: %v", path, err)
		}
	}
	if paths, err := filepath.Glob(filepath.Join(root, ".formal-gates-binary-*")); err != nil || len(paths) != 0 {
		t.Fatalf("old executable files remain after the host exits: %v, %v", paths, err)
	}
}

func observeInstallCleanup(t *testing.T, root string) func(...string) {
	t.Helper()
	notification, err := windows.FindFirstChangeNotification(root, true, windows.FILE_NOTIFY_CHANGE_FILE_NAME|windows.FILE_NOTIFY_CHANGE_DIR_NAME|windows.FILE_NOTIFY_CHANGE_LAST_WRITE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { windows.FindCloseChangeNotification(notification) })
	return func(paths ...string) {
		for {
			complete := true
			journalPath := installOuterJournalPath(filepath.Join(root, "registry.json"))
			if data, err := os.ReadFile(journalPath); err == nil {
				var journal outerInstallJournal
				if err := json.Unmarshal(data, &journal); err != nil {
					t.Fatal(err)
				}
				failure, err := os.ReadFile(filepath.Join(journal.TransactionRoot, installCleanupErrorName))
				if err == nil && len(failure) != 0 {
					t.Fatalf("deferred cleanup: %s", failure)
				}
				if installCleanupObservationPending(err) {
					complete = false
				} else if err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			} else if installCleanupObservationPending(err) {
				complete = false
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
			for _, path := range paths {
				if _, err := os.Stat(path); err == nil {
					complete = false
				} else if installCleanupObservationPending(err) {
					complete = false
				} else if !os.IsNotExist(err) {
					t.Fatal(err)
				}
			}
			if complete {
				return
			}
			if event, err := windows.WaitForSingleObject(notification, windows.INFINITE); err != nil || event != windows.WAIT_OBJECT_0 {
				t.Fatalf("cleanup directory notification: %d, %v", event, err)
			}
			if err := windows.FindNextChangeNotification(notification); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func installCleanupObservationPending(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
