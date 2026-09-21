//go:build !windows

package harness

// POSIX process plumbing for the composition: the supervisor is spawned as
// its own process group so a cancellation can signal the whole supervised
// tree (supervisor, native job, host) exactly the way Store.cancel_jobs
// does, and a recorded supervisor PID is re-identified by its creation time
// read back from the platform's own process table (ps lstart), because a
// bare integer proves nothing after pid reuse (§8.1).

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// supervisorSysProcAttr puts the supervisor in a fresh process group.
func supervisorSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// jobProcessGroup reads the spawned supervisor's process group id.
func jobProcessGroup(cmd *exec.Cmd) int {
	if cmd.Process == nil {
		return 0
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return 0
	}
	return pgid
}

// terminateJob signals the supervisor's whole process group with SIGTERM —
// the supervisor's own cancellation path, which settles the child and writes
// the deterministic 130 receipt. Signalling a group that already exited is a
// no-op.
func terminateJob(job *liveJob) {
	if job.pgid > 0 {
		_ = syscall.Kill(-job.pgid, syscall.SIGTERM)
		return
	}
	if job.cmd != nil && job.cmd.Process != nil {
		_ = job.cmd.Process.Signal(syscall.SIGTERM)
	}
}

// processCreationTime reads one PID's creation time from the platform's
// process table. ps reports it in the machine's local time, so it is parsed
// in time.Local before comparing against the recorded epoch. The empty
// result reports "unverifiable", never a guess.
func processCreationTime(pid int) (time.Time, bool) {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=").Output()
	if err != nil {
		return time.Time{}, false
	}
	created, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", strings.TrimSpace(string(out)), time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return created, true
}
