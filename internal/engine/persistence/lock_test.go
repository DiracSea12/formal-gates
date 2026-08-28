package persistence

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestLockMutualExclusionSameDirectory：锁被持有期间，同一目录的第二个
// 写者（独立 Store 实例）立即被 LockHeldError 拒绝；释放后可继续。
func TestLockMutualExclusionSameDirectory(t *testing.T) {
	dir := t.TempDir()
	holder, err := NewStore(dir, Config{PackageDigest: testPackageDigest})
	if err != nil {
		t.Fatalf("holder store: %v", err)
	}
	unlock, err := holder.acquireLock()
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// 直接抢锁：被拒。
	contender, err := NewStore(dir, Config{PackageDigest: testPackageDigest})
	if err != nil {
		t.Fatalf("contender store: %v", err)
	}
	if _, err := contender.acquireLock(); err == nil {
		t.Fatal("second acquire on held lock succeeded")
	} else {
		var held *LockHeldError
		if !errors.As(err, &held) {
			t.Fatalf("second acquire error is %T, want *LockHeldError", err)
		}
	}

	// 经 Save 抢锁：同样被拒，且不写状态。
	_, saveErr := contender.Save(Transaction{
		ExpectedRevision:    0,
		ExpectedFingerprint: "fp",
		CollectFingerprint:  staticFingerprint("fp"),
		Content:             map[string]any{"v": 1},
	})
	var held *LockHeldError
	if !errors.As(saveErr, &held) {
		t.Fatalf("save under held lock error is %v, want *LockHeldError", saveErr)
	}
	if _, err := os.Stat(contender.statePath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state written under held lock: %v", err)
	}

	unlock()
	if _, err := contender.Save(Transaction{
		ExpectedRevision:    0,
		ExpectedFingerprint: "fp",
		CollectFingerprint:  staticFingerprint("fp"),
		Content:             map[string]any{"v": 1},
	}); err != nil {
		t.Fatalf("save after release: %v", err)
	}
}

// TestLockStaleByAge：持有进程存活但锁文件超过阈值未刷新，视为失联可
// 抢占（崩溃后持有者不再刷新的场景由此覆盖）。
func TestLockStaleByAge(t *testing.T) {
	store := newTestStore(t)
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := time.Now().Add(-lockStaleAge - time.Minute)
	if err := os.WriteFile(store.lockPath(), fmt.Appendf(nil, "pid=%d\n", os.Getpid()), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	if err := os.Chtimes(store.lockPath(), stale, stale); err != nil {
		t.Fatalf("age lock: %v", err)
	}
	unlock, err := store.acquireLock()
	if err != nil {
		t.Fatalf("acquire aged lock: %v", err)
	}
	unlock()
}

// TestLockStaleByGarbage：锁内容不可解析（半个字节、非 pid 格式）视为
// 崩溃残留，可抢占。
func TestLockStaleByGarbage(t *testing.T) {
	store := newTestStore(t)
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, content := range []string{"", "garbage", "pid=abc", "pid=-1", "pid=1 pid=2"} {
		if err := os.WriteFile(store.lockPath(), []byte(content), 0o600); err != nil {
			t.Fatalf("write lock: %v", err)
		}
		unlock, err := store.acquireLock()
		if err != nil {
			t.Fatalf("acquire over garbage lock %q: %v", content, err)
		}
		unlock()
	}
}

// TestLockStaleByDeadProcess：持有进程已退出（真实死亡进程），锁可抢占。
func TestLockStaleByDeadProcess(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "0")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn child process: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait child: %v", err)
	}
	store := newTestStore(t)
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(store.lockPath(), fmt.Appendf(nil, "pid=%d\n", pid), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	unlock, err := store.acquireLock()
	if err != nil {
		t.Fatalf("acquire dead-holder lock: %v", err)
	}
	unlock()
}

// TestFreshLockIsNotStale：当前进程持有、ModTime 新鲜的锁不可抢占。
func TestFreshLockIsNotStale(t *testing.T) {
	store := newTestStore(t)
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(store.lockPath(), fmt.Appendf(nil, "pid=%d\n", os.Getpid()), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	if lockIsStale(store.lockPath()) {
		t.Fatal("fresh lock of live holder judged stale")
	}
}
