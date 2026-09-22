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

// TestLockLeftoverFileNeverBlocks：持有者异常终止后遗留的锁文件（无论
// 内容是什么、无论多久未更新）不阻塞下一个写者——互斥由内核在各自打
// 开的描述符上裁决，锁文件内容只是诊断记录。真实持有者的阻塞与死亡
// 释放在跨进程 Save 互斥测试中覆盖。
func TestLockLeftoverFileNeverBlocks(t *testing.T) {
	store := newTestStore(t)
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cmd := exec.Command("/bin/sleep", "0")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn child process: %v", err)
	}
	deadPID := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait child: %v", err)
	}
	aged := time.Now().Add(-time.Hour)
	for _, content := range []string{
		"", "garbage", "pid=abc", "pid=-1",
		fmt.Sprintf("pid=%d\n", deadPID),     // 已终止的持有者
		fmt.Sprintf("pid=%d\n", os.Getpid()), // 形似存活持有者的内容
	} {
		if err := os.WriteFile(store.lockPath(), []byte(content), 0o600); err != nil {
			t.Fatalf("write lock: %v", err)
		}
		if err := os.Chtimes(store.lockPath(), aged, aged); err != nil {
			t.Fatalf("age lock: %v", err)
		}
		unlock, err := store.acquireLock()
		if err != nil {
			t.Fatalf("leftover lock %q blocked acquisition: %v", content, err)
		}
		unlock()
	}
}
