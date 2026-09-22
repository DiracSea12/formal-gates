//go:build !windows

package coordination

import (
	"errors"
	"os"
	"syscall"
)

// lockFile 在本次打开的描述符上放置非阻塞的独占 flock。flock 冲突以
// 打开文件描述为单位：即使同一进程两次独立 open，第二次也会被拒。
func lockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// isLockHeld 报告 err 是否表示锁已被其他打开的描述符持有。
func isLockHeld(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}
