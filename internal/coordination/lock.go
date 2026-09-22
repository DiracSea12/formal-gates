// Package coordination holds this repository's path-lock primitive.  The
// lock file records the owning pid and a unique token; that pid/token shape
// is the lock-file format contract, and it is what ownership verification
// and stale-lock recovery interpret.
package coordination

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	staleAge     = 10 * time.Minute
	heartbeatAge = time.Minute
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
	return func() {
		unlock()
		_ = os.Remove(filepath.Dir(path))
		_ = os.Remove(filepath.Dir(filepath.Dir(path)))
	}, nil
}

// AcquirePath acquires an exclusive, crash-reclaimable lock at path.
func AcquirePath(path, description string) (func(), error) {
	path = filepath.Clean(path)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	for {
		owner := fmt.Sprintf("pid=%d token=%d-%d\n", os.Getpid(), time.Now().UnixNano(), sequence.Add(1))
		temp, err := os.CreateTemp(dir, ".lock-*")
		if err != nil {
			return nil, err
		}
		tempName := temp.Name()
		_, writeErr := temp.WriteString(owner)
		if writeErr == nil {
			writeErr = temp.Close()
		} else {
			_ = temp.Close()
		}
		if writeErr != nil {
			_ = os.Remove(tempName)
			return nil, writeErr
		}
		// 锁内容先在临时文件中完整写好，再以硬链接一次性发布到锁路径：锁路径
		// 上的文件从可见那一刻起就带完整 pid/token，不存在"已创建但内容为空"
		// 的窗口，竞争申请者因此不会把刚发布的锁判为陈旧并删除。
		linkErr := os.Link(tempName, path)
		_ = os.Remove(tempName)
		if linkErr == nil {
			stop := make(chan struct{})
			go heartbeat(path, owner, stop)
			return func() {
				close(stop)
				data, readErr := os.ReadFile(path)
				if readErr == nil && string(data) == owner {
					_ = os.Remove(path)
				}
			}, nil
		}
		if !os.IsExist(linkErr) {
			return nil, linkErr
		}
		if stale(path) {
			if removeErr := os.Remove(path); removeErr == nil || os.IsNotExist(removeErr) {
				continue
			}
		}
		return nil, &LockHeldError{Operation: description, Path: path}
	}
}

// LockHeldError 表示路径锁被存活持有者占用（不可抢占）。Operation 是
// 申请方的用途描述（如 install/uninstall、registry、run start）。
type LockHeldError struct {
	Operation string `json:"operation"`
	Path      string `json:"path"`
}

func (e *LockHeldError) Error() string {
	return fmt.Sprintf("%s lock is held: %s", e.Operation, e.Path)
}

func heartbeat(path, owner string, stop <-chan struct{}) {
	ticker := time.NewTicker(heartbeatAge)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			data, err := os.ReadFile(path)
			if err != nil || string(data) != owner {
				return
			}
			_ = os.Chtimes(path, time.Now(), time.Now())
		case <-stop:
			return
		}
	}
}

func stale(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	fields := strings.Fields(string(data))
	if (len(fields) != 1 && len(fields) != 2) || !strings.HasPrefix(fields[0], "pid=") {
		return true
	}
	if len(fields) == 2 && !strings.HasPrefix(fields[1], "token=") {
		return true
	}
	pid, err := strconv.Atoi(strings.TrimPrefix(fields[0], "pid="))
	if err != nil || pid <= 0 {
		return true
	}
	if !ProcessAlive(pid) {
		return true
	}
	info, err := os.Stat(path)
	return err != nil || time.Since(info.ModTime()) > staleAge
}
