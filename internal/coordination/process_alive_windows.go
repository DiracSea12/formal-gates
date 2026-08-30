//go:build windows

package coordination

import "syscall"

const (
	processQueryLimitedInformation = 0x1000
	processStillActive             = 259
)

// ProcessAlive uses the Windows process handle API because os.Process.Signal
// with signal 0 is not a portable liveness probe on Windows.  Access denied
// is treated conservatively as alive: an active owner must never be reclaimed
// merely because this process cannot query its exit code.
func ProcessAlive(pid int) bool {
	if pid <= 0 || uint64(pid) > uint64(^uint32(0)) {
		return false
	}
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return err == syscall.ERROR_ACCESS_DENIED
	}
	defer syscall.CloseHandle(handle)
	var exitCode uint32
	if err := syscall.GetExitCodeProcess(handle, &exitCode); err != nil {
		return true
	}
	return exitCode == processStillActive
}
