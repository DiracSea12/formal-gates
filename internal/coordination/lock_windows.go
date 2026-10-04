//go:build windows

package coordination

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockFile 对首字节放置独占的非阻塞区域锁
// （FAIL_IMMEDIATELY 等价参考实现的 msvcrt.locking(LK_NBLCK, 1)：锁
// 1 字节、被持有即立刻失败）。
func lockFile(f *os.File) error {
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

// lockFileBlocking 与 lockFile 同一范围，但去掉 FAIL_IMMEDIATELY：冲突时由
// 内核排队，持有者释放后取得锁。
func lockFileBlocking(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK,
		0, 1, 0, &windows.Overlapped{})
}

// initializeLockFile belongs to the creation entry, never kernel acquisition.
func initializeLockFile(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() != 0 {
		return nil
	}
	_, err = f.WriteAt([]byte("0"), 0)
	return err
}
