//go:build !windows

package harness

// Process groups isolate supervised children for cancellation. Recorded
// supervisors are identified by PID and creation time.

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

// terminateJob signals the supervisor's whole process group with SIGTERM —
// the supervisor's own cancellation path, which settles the child and writes
// the deterministic 130 receipt. Signalling a group that already exited is a
// no-op.
func terminateJob(cmd *exec.Cmd, pgid int) {
	if pgid > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		return
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
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

func signalSupervisedChild(cmd *exec.Cmd, kill bool) {
	signal := syscall.SIGTERM
	if kill {
		signal = syscall.SIGKILL
	}
	_ = syscall.Kill(-cmd.Process.Pid, signal)
}
