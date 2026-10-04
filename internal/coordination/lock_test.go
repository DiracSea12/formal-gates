package coordination

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAcquirePathMutualExclusionAndRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.lock")
	unlock, err := AcquirePath(path, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unlock)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := ""
	if runtime.GOOS == "windows" {
		want = "0"
	}
	if string(contents) != want {
		t.Fatalf("new lock contents = %q, want %q", contents, want)
	}
	_, err = AcquirePath(path, "test")
	var held *LockHeldError
	if !errors.As(err, &held) || held.Operation != "test" || held.Path != path {
		t.Fatalf("second lock error = %v", err)
	}
	unlock()
	// 释放即关闭描述符：锁文件持久保留（永不删除），锁本身立即可再
	// 次获取。
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("lock file did not persist after release: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("release replaced the lock file")
	}
	second, err := AcquirePath(path, "test")
	if err != nil {
		t.Fatal(err)
	}
	second()
	afterContents, err := os.ReadFile(path)
	if err != nil || string(afterContents) != string(contents) {
		t.Fatalf("repeated acquisition changed lock contents: %q, err=%v", afterContents, err)
	}
}

func TestPathLockAcquisitionPreservesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.lock")
	contents := "pid=123 token=record\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, acquire := range []func(string, string) (func(), error){AcquirePath, AcquirePathWaiting, AcquireExistingPathWaiting} {
		for repeat := 0; repeat < 2; repeat++ {
			release, err := acquire(path, "existing file")
			if err != nil {
				t.Fatal(err)
			}
			release()
			got, err := os.ReadFile(path)
			if err != nil || string(got) != contents {
				t.Fatalf("acquisition changed existing contents: %q, err=%v", got, err)
			}
			after, err := os.Stat(path)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("acquisition replaced the existing file: %v", err)
			}
		}
	}
}

func TestAcquireExistingPathWaitingDoesNotCreateLock(t *testing.T) {
	for _, missing := range []string{"file", "directory"} {
		t.Run(missing, func(t *testing.T) {
			dir := t.TempDir()
			if missing == "directory" {
				dir = filepath.Join(dir, "missing")
			}
			path := filepath.Join(dir, "shared.lock")
			release, err := AcquireExistingPathWaiting(path, "existing file")
			if err == nil {
				release()
				t.Fatal("acquired a missing lock file")
			}
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("missing lock error = %v", err)
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("existing acquisition created the lock file: %v", err)
			}
			if missing == "directory" {
				if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("existing acquisition created the parent directory: %v", err)
				}
			}
		})
	}
}
