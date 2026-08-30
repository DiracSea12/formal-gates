// Package coordination contains the small cross-runtime locks used by
// workflow start and admission.  The lock files deliberately use the same
// pid/token shape as the existing phase-0 and engine locks so all writers
// agree on ownership and stale-lock recovery.
package coordination

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	for {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			owner := fmt.Sprintf("pid=%d token=%d-%d\n", os.Getpid(), time.Now().UnixNano(), sequence.Add(1))
			if _, writeErr := file.WriteString(owner); writeErr != nil {
				_ = file.Close()
				_ = os.Remove(path)
				return nil, writeErr
			}
			if closeErr := file.Close(); closeErr != nil {
				_ = os.Remove(path)
				return nil, closeErr
			}
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
		if !os.IsExist(err) {
			return nil, err
		}
		if stale(path) {
			if removeErr := os.Remove(path); removeErr == nil || os.IsNotExist(removeErr) {
				continue
			}
		}
		return nil, fmt.Errorf("%s lock held: %s", description, path)
	}
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
	process, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return true
	}
	info, err := os.Stat(path)
	return err != nil || time.Since(info.ModTime()) > staleAge
}
