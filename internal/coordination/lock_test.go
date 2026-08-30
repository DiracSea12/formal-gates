package coordination

import (
	"fmt"
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
	if _, err := AcquirePath(path, "test"); err == nil || !strings.Contains(err.Error(), "lock held") {
		t.Fatalf("second lock error = %v", err)
	}
	unlock()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("lock remained after release: %v", err)
	}
	second, err := AcquirePath(path, "test")
	if err != nil {
		t.Fatal(err)
	}
	second()
}

func TestAcquirePathUnderstandsLegacyPIDOnlyLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.lock")
	if err := os.WriteFile(path, []byte(fmt.Sprintf("pid=%d\n", os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquirePath(path, "admission"); err == nil || !strings.Contains(err.Error(), "lock held") {
		t.Fatalf("legacy lock was not respected: %v", err)
	}
}
