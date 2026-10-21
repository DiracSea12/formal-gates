//go:build windows

package harness

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestPeerDeathAssertionRejectsLiveProcess(t *testing.T) {
	if os.Getenv("FORMAL_GATES_ASSERT_LIVE_PEER") == "1" {
		assertPeerDead(t, os.Getpid())
		return
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestPeerDeathAssertionRejectsLiveProcess$")
	cmd.Env = append(os.Environ(), "FORMAL_GATES_ASSERT_LIVE_PEER=1")
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "still running after native termination") {
		t.Fatalf("live process did not fail death assertion: %v\n%s", err, output)
	}
}

func TestPeerDeathAssertionAcceptsExitedProcess(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	assertPeerDead(t, cmd.Process.Pid)
}

// peerExecutable is the Windows launcher: a .cmd the command interpreter can
// run directly (the client execs it as the "executable"), which re-execs this
// test binary with only the helper test selected.
func peerExecutable(t *testing.T, dir, scriptPath string) string {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	peer := filepath.Join(dir, "peer.cmd")
	body := fmt.Sprintf("@set \"FORMAL_GATES_NATIVE_PEER_SCRIPT=%s\"\r\n@%s -test.run=^TestScriptedPeer$ -- %%*\r\n",
		scriptPath, binary)
	if err := os.WriteFile(peer, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return peer
}

// assertPeerDead waits briefly for the recorded pid to leave the table.
func assertPeerDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
		if err != nil {
			if err == windows.ERROR_INVALID_PARAMETER {
				return // pid is gone
			}
			t.Fatalf("open peer pid %d: %v", pid, err)
		}
		result, waitErr := windows.WaitForSingleObject(handle, 0)
		windows.CloseHandle(handle)
		if waitErr == nil && result == windows.WAIT_OBJECT_0 {
			return
		}
		if waitErr == nil && result == uint32(windows.WAIT_TIMEOUT) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		t.Fatalf("wait for peer pid %d: result %d, error %v", pid, result, waitErr)
	}
	t.Fatalf("pid %d still running after native termination", pid)
}
