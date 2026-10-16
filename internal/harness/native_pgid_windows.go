//go:build windows

package harness

// A supervisor starts a new Windows console process group whose ID is its PID.
func nativeProcessGroupID(pid int) (int, error) { return pid, nil }
