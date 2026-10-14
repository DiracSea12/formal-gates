//go:build windows

package definition

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

// replaceFileW 绑定 Windows 原生文件替换调用；replaceCompletedFile 对已存在
// 的目标调用 ReplaceFileW，目标不存在时调用 os.Rename。
var replaceFileW = syscall.NewLazyDLL("kernel32.dll").NewProc("ReplaceFileW")

const (
	windowsSharingViolation = syscall.Errno(32)
	windowsLockViolation    = syscall.Errno(33)
)

func replaceCompletedFile(source, destination string) error {
	if _, err := os.Stat(destination); os.IsNotExist(err) {
		return os.Rename(source, destination)
	} else if err != nil {
		return err
	}
	destinationPtr, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	sourcePtr, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	// 仅共享/锁冲突错误进行有界重试；其他调用错误直接返回。
	const (
		maxAttempts = 200
		retryDelay  = 2 * time.Millisecond
	)
	for attempt := 0; ; attempt++ {
		result, _, callErr := replaceFileW.Call(
			uintptr(unsafe.Pointer(destinationPtr)),
			uintptr(unsafe.Pointer(sourcePtr)),
			0,
			0,
			0,
			0,
		)
		if result != 0 {
			return nil
		}
		errno := callErr
		if errno == syscall.Errno(0) {
			errno = syscall.EINVAL
		}
		if attempt < maxAttempts && (errno == windowsSharingViolation || errno == windowsLockViolation) {
			time.Sleep(retryDelay)
			continue
		}
		return errno
	}
}
