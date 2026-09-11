//go:build !windows

package facade

import (
	"errors"
	"os/exec"
	"syscall"
)

// Codex may be a launcher whose child is the actual native executable. Giving
// this invocation its own process group keeps recovery/abort bound to both.
func isolateCodexProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return stopCodexProcess(cmd.Process.Pid) }
}
func codexProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(-pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
func stopCodexProcess(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
