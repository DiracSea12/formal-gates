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

// AcquirePath acquires an exclusive, cross-process lock at path and returns
// the release function. Releasing closes the descriptor, which is what frees
// the lock; a holder that exits without releasing is freed by the kernel, so
// recovery needs no staleness rules and the lock file can stay forever.
func AcquirePath(path, description string) (func(), error) {
	path = filepath.Clean(path)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		if isLockHeld(err) {
			return nil, &LockHeldError{Operation: description, Path: path}
		}
		return nil, err
	}
	// 诊断记录：锁内容只标明当前持有者，便于人工排障；不参与任何判
	// 定，写失败不影响持锁。
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(fmt.Sprintf("pid=%d token=%d-%d\n", os.Getpid(), time.Now().UnixNano(), sequence.Add(1))), 0)
	return func() { _ = f.Close() }, nil
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
	path = filepath.Clean(path)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFileBlocking(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	// 诊断记录：锁内容只标明当前持有者，便于人工排障；不参与任何判
	// 定，写失败不影响持锁。
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(fmt.Sprintf("pid=%d token=%d-%d\n", os.Getpid(), time.Now().UnixNano(), sequence.Add(1))), 0)
	return func() { _ = f.Close() }, nil
}
