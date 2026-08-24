//go:build windows

package definition

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

// replaceCompletedFile 在 Windows 上用 ReplaceFileW 原子替换已存在的目标文件。
// os.Rename 遇到"读者仍持有旧句柄"的目标文件会以 Access is denied 失败
//（writeGenerated 的并发原子性测试在 Windows 上稳定触发）；ReplaceFileW
// 走 POSIX 语义替换，允许替换期间存在打开的读句柄。
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
	// 并发读者反复 open/close 目标文件时，替换调用仍可能撞上瞬时共享冲突。
	// 带界重试只消化 sharing/lock violation；替换一旦落位仍是原子换页，
	// 不改变"读者只见完整旧字节或完整新字节"的保证。
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
