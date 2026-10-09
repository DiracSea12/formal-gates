//go:build windows

package harness

// Windows supervision terminates the recorded process. Process creation
// time is unavailable, so a recorded PID cannot establish liveness.

import (
	"os/exec"
	"syscall"
	"time"
)

// supervisorSysProcAttr is the plain spawn on Windows.
func supervisorSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

// terminateJob kills the supervisor process; without SIGTERM the supervisor
// cannot write its cancellation receipt, so the job settles as killed.
func terminateJob(cmd *exec.Cmd, _ int) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// processCreationTime is unavailable on this platform.
func processCreationTime(pid int) (time.Time, bool) {
	return time.Time{}, false
}

func signalSupervisedChild(cmd *exec.Cmd, _ bool) { _ = cmd.Process.Kill() }
