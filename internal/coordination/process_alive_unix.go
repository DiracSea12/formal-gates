//go:build !windows

package coordination

import (
	"os"
	"syscall"
)

// ProcessAlive reports whether pid currently names a process that this
// runtime can signal.  The zero signal is a non-destructive liveness probe on
// Unix-like systems.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
