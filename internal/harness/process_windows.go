//go:build windows

package harness

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

func supervisorSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// ApplyNativeCommandLine preserves approved CMD text through its native parser.
func ApplyNativeCommandLine(cmd *exec.Cmd) {
	argv := cmd.Args
	if len(argv) != 5 || !strings.EqualFold(filepath.Base(argv[0]), "cmd.exe") || argv[1] != "/D" || argv[2] != "/S" || argv[3] != "/C" {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CmdLine = syscall.EscapeArg(argv[0]) + ` /D /S /C "` + argv[4] + `"`
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
