//go:build windows

package harness

import (
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

func supervisorSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// Console break reaches the supervisor's cancellation handler and receipt path.
func terminateJob(_ *exec.Cmd, pgid int) error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(pgid))
}

func processCreationTime(pid int) (time.Time, bool) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return time.Time{}, false
	}
	defer windows.CloseHandle(handle)
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, created.Nanoseconds()), true
}

func signalSupervisedChild(cmd *exec.Cmd, kill bool) {
	if kill {
		_ = cmd.Process.Kill()
		return
	}
	_ = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid))
}
