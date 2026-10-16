//go:build windows

package facade

import "syscall"

func checkSupervisorAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
