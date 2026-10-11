//go:build !windows

package harness

import (
	"fmt"
	"syscall"
)

// nativeProcessGroupID reads the child's current process group. Native spawn
// inherits the caller's group; lookup errors preserve the OS error text.
func nativeProcessGroupID(pid int) (int, error) {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return 0, fmt.Errorf("cannot read the process group of pid %d: %w", pid, err)
	}
	return pgid, nil
}
