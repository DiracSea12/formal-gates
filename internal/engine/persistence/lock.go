package persistence

import (
	"errors"
	"sync"

	"formal-gates/internal/coordination"
)

var transactionLocks = struct {
	sync.Mutex
	byDir map[string]*sync.Mutex
}{byDir: map[string]*sync.Mutex{}}

func transactionLock(dir string) *sync.Mutex {
	transactionLocks.Lock()
	defer transactionLocks.Unlock()
	if lock, ok := transactionLocks.byDir[dir]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	transactionLocks.byDir[dir] = lock
	return lock
}

// acquireLock 获取状态目录级独占写锁（同目录
// 并发写互斥）。锁是目录级而非单个文件级：目录内 state/intent/temp
// 的全部写入都在同一把锁下互斥。实现复用 coordination.AcquirePath
// （内核文件锁：互斥由内核在各次打开的描述符上裁决，持有者死亡即自
// 动释放，锁文件持久存在）。engine 写事务是毫秒级小事务；返回释放
// 函数，释放即关闭描述符。
func (s *Store) acquireLock() (func(), error) {
	unlock, err := coordination.AcquirePath(s.lockPath(), "engine state directory")
	if err != nil {
		var held *coordination.LockHeldError
		if errors.As(err, &held) {
			return nil, &LockHeldError{Path: s.lockPath()}
		}
		return nil, err
	}
	return unlock, nil
}
