// Package coordination holds this repository's path-lock primitive: an
// exclusive cross-process lock adjudicated by the kernel on each opener's own
// descriptor (flock on POSIX, a one-byte LockFile range on Windows). A holder
// that dies has its lock released by the kernel, and a leftover lock file
// never blocks a new acquirer. Lock files are never deleted or rewritten
// when an existing lock is acquired.
package coordination

import (
	"fmt"
	"os"
	"path/filepath"
)

// AcquireRun serializes creation of a run ID at the repository root.
func AcquireRun(root, runID string) (func(), error) {
	path := filepath.Join(root, ".gates", "run-locks", runID+".lock")
	unlock, err := AcquirePath(path, "run start")
	if err != nil {
		return nil, fmt.Errorf("run %q already exists or is being started: %w", runID, err)
	}
	return unlock, nil
}

// AcquireRunWaiting 获取与 AcquireRun 同一锁对象的独占锁，持锁冲突时等待。
// 调用者在锁内按持久事实执行串行化操作。
func AcquireRunWaiting(root, runID string) (func(), error) {
	return AcquirePathWaiting(filepath.Join(root, ".gates", "run-locks", runID+".lock"), "run start")
}

// AcquirePath acquires an exclusive, cross-process lock at path and returns
// the release function. Releasing closes the descriptor, which is what frees
// the lock; a holder that exits without releasing is freed by the kernel, so
// recovery needs no staleness rules and the lock file can stay forever.
func AcquirePath(path, description string) (func(), error) {
	return acquirePathLock(path, description, false, true)
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
// 合，不能把并发竞争当失败上抛）。
func AcquirePathWaiting(path, description string) (func(), error) {
	return acquirePathLock(path, description, true, true)
}

// AcquireExistingPathWaiting acquires the same kernel lock without creating
// a directory or file. Its admitted creation owner establishes the lock file.
func AcquireExistingPathWaiting(path, description string) (func(), error) {
	return acquirePathLock(path, description, true, false)
}

// acquirePathLock shares acquisition and release across creation and existing
// callers. File contents participate in no locking decision.
func acquirePathLock(path, description string, waiting, create bool) (func(), error) {
	path = filepath.Clean(path)
	flags := os.O_RDWR
	if create {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		flags |= os.O_CREATE
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return nil, err
	}
	if create {
		if err := initializeLockFile(f); err != nil {
			_ = f.Close()
			return nil, err
		}
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
	return func() { _ = f.Close() }, nil
}
