package coordination

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestAcquirePathHolderProcess 是跨进程锁测试的子进程入口：获取 path 的
// 锁后写 ready 文件示意已持有，等到 release 文件出现后以不调用释放函
// 数的方式退出，模拟持有者异常终止后遗留的锁。
func TestAcquirePathHolderProcess(t *testing.T) {
	path := os.Getenv(lockHolderPathEnv)
	if path == "" {
		t.Skip("subprocess entry only")
	}
	if _, err := AcquirePath(path, "holder"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv(lockHolderReadyEnv), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(os.Getenv(lockHolderReleaseEnv)); err == nil {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	os.Exit(0)
}

const (
	lockHolderPathEnv    = "COORDINATION_LOCK_HOLDER_PATH"
	lockHolderReadyEnv   = "COORDINATION_LOCK_HOLDER_READY"
	lockHolderReleaseEnv = "COORDINATION_LOCK_HOLDER_RELEASE"
)

func startHolderProcess(t *testing.T, path, ready, release string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestAcquirePathHolderProcess$")
	cmd.Env = append(os.Environ(),
		lockHolderPathEnv+"="+path,
		lockHolderReadyEnv+"="+ready,
		lockHolderReleaseEnv+"="+release,
		"GORACE=atexit_sleep_ms=0",
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start holder: %v", err)
	}
	return cmd
}

func waitUntilExists(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("holder never acquired the lock")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestAcquirePathAbandonedOwnerIsReclaimed：存活子进程持锁期间，父进程
// 申请同一路径必须得到 LockHeldError；持有者异常退出（未执行释放）后，
// 同一路径立即可再次获取——持有者死亡由内核自动回收，无需任何陈旧判
// 定或恢复步骤。
func TestAcquirePathAbandonedOwnerIsReclaimed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "owner.lock")
	ready := filepath.Join(dir, "ready")
	release := filepath.Join(dir, "release")
	cmd := startHolderProcess(t, path, ready, release)
	waitUntilExists(t, ready)

	if _, err := AcquirePath(path, "reclaimer"); err == nil || !strings.Contains(err.Error(), "lock is held") {
		_ = os.WriteFile(release, nil, 0o600)
		_ = cmd.Wait()
		t.Fatalf("live holder did not block the reclaimer: %v", err)
	}

	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("holder exit: %v", err)
	}
	unlock, err := AcquirePath(path, "reclaimer")
	if err != nil {
		t.Fatalf("lock was not reclaimed after holder exit: %v", err)
	}
	unlock()
}

// TestAcquirePathRecoveryAfterOwnerExitNeverDoubleGrants：真实子进程持
// 锁后异常退出，两个恢复者并发申请同一路径。不变量：任意时刻至多一方
// 持有；至少一方成功取得；失败一方的错误必须是 LockHeldError（明确的
// 占用拒绝），双方都失败不算通过。
func TestAcquirePathRecoveryAfterOwnerExitNeverDoubleGrants(t *testing.T) {
	for round := 0; round < 300; round++ {
		dir := t.TempDir()
		path := filepath.Join(dir, "owner.lock")
		ready := filepath.Join(dir, "ready")
		release := filepath.Join(dir, "release")
		cmd := startHolderProcess(t, path, ready, release)
		waitUntilExists(t, ready)
		if err := os.WriteFile(release, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := cmd.Wait(); err != nil {
			t.Fatalf("holder exit: %v", err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var active, success, violations atomic.Int32
		errs := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				release, err := AcquirePath(path, "recovery")
				if err != nil {
					var held *LockHeldError
					if !errors.As(err, &held) {
						errs <- err
					}
					return
				}
				success.Add(1)
				if active.Add(1) > 1 {
					violations.Add(1)
				}
				time.Sleep(2 * time.Millisecond)
				active.Add(-1)
				release()
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Errorf("unexpected acquisition error: %v", err)
		}
		if success.Load() == 0 {
			t.Fatalf("round %d: no recovery caller acquired the lock", round)
		}
		if violations.Load() > 0 {
			t.Fatalf("round %d: two recovery callers simultaneously held a lock abandoned by an exited owner", round)
		}
	}
}
