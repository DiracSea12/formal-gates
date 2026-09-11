package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
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

func TestLockReleaseWhileContenderReads(t *testing.T) {
	store := newTestStore(t)
	unlock, err := store.acquireLock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	reader, err := openSharedFile(store.lockPath())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	unlock()
	closed := make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = reader.Close()
		close(closed)
	}()
	defer func() { <-closed }()
	next, err := store.acquireLock()
	if err != nil {
		t.Fatal(err)
	}
	next()
}

func TestLockDoesNotReclaimOwnerReplacedDuringProcessProbe(t *testing.T) {
	store := newTestStore(t)
	old := []byte("pid=123 token=old\n")
	next := []byte(fmt.Sprintf("pid=%d token=next\n", os.Getpid()))
	if err := os.WriteFile(store.lockPath(), old, 0600); err != nil {
		t.Fatal(err)
	}
	stale := lockIsStaleWithProcessProbe(store.lockPath(), func(int) bool {
		// The old process releases, a new writer acquires, then the old
		// process exits before the liveness query returns.
		if err := os.Remove(store.lockPath()); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store.lockPath(), next, 0600); err != nil {
			t.Fatal(err)
		}
		return false
	})
	if stale {
		t.Fatal("a new live owner's lock was classified as stale")
	}
}

func TestSaveWhileSnapshotReaderIsOpen(t *testing.T) {
	store := newTestStore(t)
	tx := Transaction{ExpectedFingerprint: "stable", CollectFingerprint: staticFingerprint("stable"), Content: map[string]int{"v": 1}}
	if _, err := store.Save(tx); err != nil {
		t.Fatal(err)
	}
	reader, err := openSharedFile(store.statePath())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	closed := make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = reader.Close()
		close(closed)
	}()
	defer func() { <-closed }()

	tx.ExpectedRevision = 1
	tx.Content = map[string]int{"v": 2}
	if _, err := store.Save(tx); err != nil {
		t.Fatalf("snapshot reader prevented atomic replacement: %v", err)
	}
	current, err := store.Load()
	if err != nil || current.Revision != 2 {
		t.Fatalf("new snapshot unavailable: %+v %v", current, err)
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

// TestLockStaleByGarbage：不完整的 owner 行超过创建宽限期后可回收；
// 新鲜的半写入内容属于正在取得锁的创建者，不能立即视为崩溃残留。
func TestLockStaleByGarbage(t *testing.T) {
	store := newTestStore(t)
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, content := range []string{"", "garbage", "pid=abc", "pid=-1", "pid=1 pid=2"} {
		if err := os.WriteFile(store.lockPath(), []byte(content), 0o600); err != nil {
			t.Fatalf("write lock: %v", err)
		}
		old := time.Now().Add(-lockStaleAge - time.Second)
		if err := os.Chtimes(store.lockPath(), old, old); err != nil {
			t.Fatal(err)
		}
		unlock, err := store.acquireLock()
		if err != nil {
			t.Fatalf("acquire over garbage lock %q: %v", content, err)
		}
		unlock()
	}
}

func TestLockPreservesUnpublishedOwner(t *testing.T) {
	for _, partial := range []string{"", "pid="} {
		t.Run(fmt.Sprintf("bytes-%d", len(partial)), func(t *testing.T) {
			store := newTestStore(t)
			first, err := os.OpenFile(store.lockPath(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			defer first.Close()
			if _, err := first.WriteString(partial); err != nil {
				t.Fatal(err)
			}
			identity, err := first.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if unlock, err := store.acquireLock(); err == nil {
				unlock()
				t.Fatal("second writer acquired before the first owner published its PID")
			} else {
				var held *LockHeldError
				if !errors.As(err, &held) {
					t.Fatal(err)
				}
			}
			current, err := os.Stat(store.lockPath())
			if err != nil || !os.SameFile(identity, current) {
				t.Fatal("contender replaced the first creator's inode")
			}
		})
	}
}

// Independent test-binary processes exercise the same Store.Save file lock as
// simultaneous installed CLI commands. Every successful commit must survive;
// only CAS conflicts and an occupied lock may be retried by the callers.
func TestSaveConcurrentProcessesPreserveEveryCommit(t *testing.T) {
	const workers, commits = 6, 40
	if dir := os.Getenv("FORMAL_GATES_TEST_COMMIT_DIRECTORY"); dir != "" {
		store, err := NewStore(dir, Config{PackageDigest: testPackageDigest})
		if err != nil {
			t.Fatal(err)
		}
		for n := 0; n < commits; n++ {
			committed := false
			for retry := 0; retry < 2000; retry++ {
				snap, err := store.Load()
				if err != nil {
					t.Fatal(err)
				}
				var values map[string]int
				if err := json.Unmarshal(snap.Content, &values); err != nil {
					t.Fatal(err)
				}
				key := os.Getenv("FORMAL_GATES_TEST_COMMIT_WORKER") + ":" + strconv.Itoa(n)
				values[key] = os.Getpid()
				_, err = store.Save(Transaction{ExpectedRevision: snap.Revision, ExpectedFingerprint: "stable", CollectFingerprint: staticFingerprint("stable"), Content: values})
				if err == nil {
					committed = true
					break
				}
				var held *LockHeldError
				var conflict *RevisionConflictError
				if !errors.As(err, &held) && !errors.As(err, &conflict) {
					t.Fatalf("commit %s failed outside CAS/lock contention: %v", key, err)
				}
				time.Sleep(time.Millisecond)
			}
			if !committed {
				t.Fatal("bounded contention retries exhausted")
			}
		}
		return
	}
	store := newTestStore(t)
	if _, err := store.Save(Transaction{ExpectedFingerprint: "stable", CollectFingerprint: staticFingerprint("stable"), Content: map[string]int{}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	done := make(chan error, workers)
	for i := 0; i < workers; i++ {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSaveConcurrentProcessesPreserveEveryCommit$")
		cmd.Env = append(os.Environ(), "FORMAL_GATES_TEST_COMMIT_DIRECTORY="+store.dir, "FORMAL_GATES_TEST_COMMIT_WORKER="+strconv.Itoa(i))
		go func() {
			out, err := cmd.CombinedOutput()
			if err != nil {
				err = fmt.Errorf("child writer: %w: %s", err, out)
			}
			done <- err
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	snap, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]int
	if err := json.Unmarshal(snap.Content, &values); err != nil {
		t.Fatal(err)
	}
	if snap.Revision != 1+workers*commits || len(values) != workers*commits {
		t.Fatalf("lost or duplicate commits: revision=%d values=%d", snap.Revision, len(values))
	}
	pids := map[int]bool{}
	for i := 0; i < workers; i++ {
		for n := 0; n < commits; n++ {
			key := fmt.Sprintf("%d:%d", i, n)
			pid, ok := values[key]
			if !ok {
				t.Fatalf("missing successful commit %s", key)
			}
			pids[pid] = true
		}
	}
	if len(pids) != workers || pids[os.Getpid()] {
		t.Fatalf("not independent writer processes: %v", pids)
	}
	if _, err := os.Stat(store.intentPath()); !os.IsNotExist(err) {
		t.Fatalf("completed writers left intent: %v", err)
	}
	t.Logf("%d independent native processes committed %d distinct values at revision %d; no intent remains", len(pids), len(values), snap.Revision)
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
