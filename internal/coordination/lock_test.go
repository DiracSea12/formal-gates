package coordination

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquirePathMutualExclusionAndRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.lock")
	unlock, err := AcquirePath(path, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquirePath(path, "test"); err == nil || !strings.Contains(err.Error(), "lock is held") {
		t.Fatalf("second lock error = %v", err)
	}
	unlock()
	// 释放即关闭描述符：锁文件持久保留（永不删除），锁本身立即可再
	// 次获取。
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file did not persist after release: %v", err)
	}
	second, err := AcquirePath(path, "test")
	if err != nil {
		t.Fatal(err)
	}
	second()
}
