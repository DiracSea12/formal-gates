//go:build windows

package harness

// nativeProcessGroupID is unreachable on Windows: newNativeClient guards the
// call with runtime.GOOS and records a null pgid, matching the reference
// implementation's `os.getpgid(...) if os.name != 'nt' else None`.
func nativeProcessGroupID(pid int) (int, error) { return 0, nil }
