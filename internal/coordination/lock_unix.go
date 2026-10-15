//go:build !windows

package coordination

import (
	"errors"
	"os"
	"syscall"
)

func openLockFile(path string, create bool) (*os.File, error) {
	flags := os.O_RDWR
	if create {
		flags |= os.O_CREATE
	}
	return os.OpenFile(path, flags, 0o600)
}

func retirePath(path string) error { return os.Remove(path) }

// lockFile 在本次打开的描述符上放置非阻塞的独占 flock。flock 冲突以
// 打开文件描述为单位：即使同一进程两次独立 open，第二次也会被拒。
func lockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// lockFileBlocking 在本次打开的描述符上放置阻塞的独占 flock：冲突时由内核
// 排队，持有者释放（含死亡）后取得锁。
func lockFileBlocking(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// A concurrent fork can retain the open file description after this close.
func releaseLockFile(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
}

// isLockHeld 报告 err 是否表示锁已被其他打开的描述符持有。
func isLockHeld(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}
