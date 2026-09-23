//go:build !windows

package harness

import (
	"fmt"
	"syscall"
)

// nativeProcessGroupID reports the child's process group the way the reference
// implementation does (os.getpgid(pid)): the spawn never creates a new group,
// so the value equals the caller's own pgid. A child that already exited and
// was reaped is the observable failure; any other errno keeps its own text.
func nativeProcessGroupID(pid int) (int, error) {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return 0, fmt.Errorf("cannot read the process group of pid %d: %w", pid, err)
	}
	return pgid, nil
}
