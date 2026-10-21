//go:build windows

package harness

import (
	"errors"
	"fmt"
	"formal-gates/internal/coordination"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func supervisorSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// Owned children enter their job before running. When the command exits, the
// job stops its descendants and Cmd.Wait drains their inherited output handles.
func startChild(cmd *exec.Cmd, owned bool) (settle func() error, err error) {
	if !owned {
		return nil, cmd.Start()
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	// KILL_ON_JOB_CLOSE makes the kernel stop the whole tree when the last
	// job handle dies with the owner process, so an orphaned panel service or
	// browser cannot outlive its owner.
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	var process windows.Handle
	defer func() {
		if err != nil {
			err = errors.Join(err, windows.TerminateJobObject(job, 1))
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
			if process != 0 {
				err = errors.Join(err, windows.CloseHandle(process))
			}
			err = errors.Join(err, windows.CloseHandle(job))
		}
	}()
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	process, err = windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return nil, err
	}
	if err = windows.AssignProcessToJobObject(job, process); err != nil {
		return nil, err
	}
	if err = resumeChild(uint32(cmd.Process.Pid)); err != nil {
		return nil, err
	}
	finished := make(chan error, 1)
	go func() {
		_, waitErr := windows.WaitForSingleObject(process, windows.INFINITE)
		finished <- errors.Join(waitErr, windows.TerminateJobObject(job, 1))
	}()
	return func() error {
		return errors.Join(<-finished, windows.CloseHandle(process), windows.CloseHandle(job))
	}, nil
}

func resumeChild(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	thread := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &thread); err == nil; err = windows.Thread32Next(snapshot, &thread) {
		if thread.OwnerProcessID != pid {
			continue
		}
		handle, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, thread.ThreadID)
		if err != nil {
			return err
		}
		_, err = windows.ResumeThread(handle)
		return errors.Join(err, windows.CloseHandle(handle))
	}
	return fmt.Errorf("supervised child has no primary thread: %w", err)
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

// The supervisor owns a KILL_ON_JOB_CLOSE job. Its recorded native child
// must also be gone before acknowledging the stopped tree.
func nativeProcessTreeStopped(directory string, _ int) bool {
	process, err := readJSONFile(filepath.Join(directory, "native-process.json"))
	if os.IsNotExist(err) {
		return true
	}
	if err != nil {
		return false
	}
	pid := intOf(getOf(process, "pid"))
	return pid > 0 && !coordination.ProcessAlive(pid)
}
