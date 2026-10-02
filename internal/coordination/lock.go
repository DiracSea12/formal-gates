// Package coordination holds this repository's path-lock primitive: an
// exclusive cross-process lock adjudicated by the kernel on each opener's own
// descriptor (flock on POSIX, a one-byte LockFile range on Windows). A holder
// that dies has its lock released by the kernel, and a leftover lock file
// never blocks a new acquirer. The lock file records the owning pid and token
// as a diagnostic for humans only; it participates in no locking decision
// and is never deleted.
package coordination

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

var sequence atomic.Uint64

// AcquireRun serializes creation of a run id across the legacy and candidate
// namespaces.
func AcquireRun(root, runID string) (func(), error) {
	path := filepath.Join(root, ".gates", "run-locks", runID+".lock")
	unlock, err := AcquirePath(path, "run start")
	if err != nil {
		return nil, fmt.Errorf("run %q already exists or is being started: %w", runID, err)
	}
	return unlock, nil
}

// AcquireRunWaiting 获取与 AcquireRun 同一锁对象的独占锁，但持锁冲突时等待
// 而不是立即失败。用于跨 run 的串行化段（如 child run 在 master 账上认领分
// 片）：等待后按持久事实判定，不把并发竞争当失败上抛。
func AcquireRunWaiting(root, runID string) (func(), error) {
	return AcquirePathWaiting(filepath.Join(root, ".gates", "run-locks", runID+".lock"), "run start")
}

// AcquirePath acquires an exclusive, cross-process lock at path and returns
// the release function. Releasing closes the descriptor, which is what frees
// the lock; a holder that exits without releasing is freed by the kernel, so
// recovery needs no staleness rules and the lock file can stay forever.
func AcquirePath(path, description string) (func(), error) {
	return acquirePathLock(path, description, false)
}

// LockHeldError 表示路径锁被其他持有者占用（不可抢占）。Operation 是
// 申请方的用途描述（如 install/uninstall、registry、run start）。
type LockHeldError struct {
	Operation string `json:"operation"`
	Path      string `json:"path"`
}

func (e *LockHeldError) Error() string {
	return fmt.Sprintf("%s lock is held: %s", e.Operation, e.Path)
}

// AcquirePathWaiting 获取与 AcquirePath 同一路径、同一内核对象的独占锁，但
// 持锁冲突时等待而不是立即失败（flock 阻塞模式／LockFileEx 去掉
// FAIL_IMMEDIATELY）。持有者死亡由内核释放，等待者随之取得锁。用于需要互斥
// 而不是快速失败的临界段（owner 区间协调：并行启动的扫描＋标记段必须串行闭
// 合，不能把并发竞争当失败上抛）。诊断记录与 AcquirePath 相同。
func AcquirePathWaiting(path, description string) (func(), error) {
	return acquirePathLock(path, description, true)
}

// acquirePathLock 是两个公开获取入口共用的内部段：打开（或创建）锁文件、按
// 等待策略取得内核锁并写入持有者诊断。等待者由内核唤醒；快速失败者把持锁冲
// 突归一为 LockHeldError。诊断记录只标明当前持有者，便于人工排障；不参与任
// 何判定，写失败不影响持锁。
func acquirePathLock(path, description string, waiting bool) (func(), error) {
	path = filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var lockErr error
	if waiting {
		lockErr = lockFileBlocking(f)
	} else {
		lockErr = lockFile(f)
	}
	if lockErr != nil {
		_ = f.Close()
		if !waiting && isLockHeld(lockErr) {
			return nil, &LockHeldError{Operation: description, Path: path}
		}
		return nil, lockErr
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(fmt.Sprintf("pid=%d token=%d-%d\n", os.Getpid(), time.Now().UnixNano(), sequence.Add(1))), 0)
	return func() { _ = f.Close() }, nil
}
