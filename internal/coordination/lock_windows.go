//go:build windows

package coordination

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockFile 先确保首字节存在，再对首字节放置独占的非阻塞区域锁
// （FAIL_IMMEDIATELY 等价参考实现的 msvcrt.locking(LK_NBLCK, 1)：锁
// 1 字节、被持有即立刻失败）。
func lockFile(f *os.File) error {
	if _, err := f.WriteAt([]byte("0"), 0); err != nil {
		return err
	}
	err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &windows.Overlapped{})
	if err != nil {
		return err
	}
	return nil
}

// isLockHeld 报告 err 是否表示锁已被其他句柄持有。
func isLockHeld(err error) bool {
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
