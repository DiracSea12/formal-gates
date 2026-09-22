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
// （崩溃可回收文件锁：持有者死亡或锁文件超过阈值未刷新即可抢占，
// 持锁期间由心跳刷新）。engine 写事务是毫秒级小事务，心跳在事务
// 生命周期内不会实际触发；返回释放函数，释放即删除锁文件。
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
