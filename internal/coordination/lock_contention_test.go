package coordination

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestAcquirePathNeverGrantsTwoHolders 验证路径锁的互斥不变量：两个调用者同时
// 申请同一路径时，任意时刻至多一方持有；未取得锁的一方必须得到 LockHeldError，
// 而不是在锁文件刚发布的窗口内被误判为陈旧后与持有者同时进入临界区。
func TestAcquirePathNeverGrantsTwoHolders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.lock")
	var success, violations atomic.Int32
	for round := 0; round < 100; round++ {
		start := make(chan struct{})
		var wg sync.WaitGroup
		var active atomic.Int32
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				release, err := AcquirePath(path, "contended section")
				if err != nil {
					var held *LockHeldError
					if !errors.As(err, &held) {
						t.Errorf("unexpected acquisition error: %v", err)
					}
					return
				}
				success.Add(1)
				if active.Add(1) != 1 {
					violations.Add(1)
				}
				time.Sleep(time.Millisecond)
				active.Add(-1)
				release()
			}()
		}
		close(start)
		wg.Wait()
	}
	if success.Load() == 0 {
		t.Fatal("no concurrent caller acquired the lock")
	}
	if n := violations.Load(); n != 0 {
		t.Fatalf("two concurrent callers owned the same path in %d rounds", n)
	}
}
