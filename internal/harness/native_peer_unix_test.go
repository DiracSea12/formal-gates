//go:build !windows

package harness

import (
	"path/filepath"
	"testing"
)

// peerExecutable is the POSIX launcher: a shell wrapper owned by the canary
// process so a supervisor-level kill terminates the whole group.
func peerExecutable(t *testing.T, dir, scriptPath string) string {
	t.Helper()
	path := filepath.Join(dir, "peer")
	if err := writePeerWrapper(path, canaryPeerWrapper("TestScriptedPeer", "FORMAL_GATES_NATIVE_PEER_SCRIPT", scriptPath)); err != nil {
		t.Fatal(err)
	}
	return path
}

// assertPeerDead waits briefly for the recorded pid to leave the table.
func assertPeerDead(t *testing.T, pid int) {
	t.Helper()
	assertNativeProcessTerminated(t, pid)
}
