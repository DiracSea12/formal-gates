//go:build windows

package harness

// Windows process plumbing for the composition. There is no process group
// and no SIGTERM here: the supervisor is spawned plainly, cancellation
// terminates the recorded process, and a recorded PID's creation time is not
// verifiable through a stable system interface — the takeover consult
// therefore never reports Observe on this platform (it falls to the user
// disposition instead of guessing). The kernel and the composition's
// judgement paths are otherwise identical.

import (
	"os/exec"
	"syscall"
	"time"
)

// supervisorSysProcAttr is the plain spawn on Windows.
func supervisorSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

// jobProcessGroup has no meaning on Windows; 0 disables group signalling.
func jobProcessGroup(cmd *exec.Cmd) int {
	return 0
}

// terminateJob kills the supervisor process; without SIGTERM the supervisor
// cannot write its cancellation receipt, so the job settles as killed.
func terminateJob(job *liveJob) {
	if job.cmd != nil && job.cmd.Process != nil {
		_ = job.cmd.Process.Kill()
	}
}

// processCreationTime is unverifiable on this platform by the rule above.
func processCreationTime(pid int) (time.Time, bool) {
	return time.Time{}, false
}
