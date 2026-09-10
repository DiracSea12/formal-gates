package coordination

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestAcquirePathPreservesUnpublishedOwnerAndRecoversExpiredCreation(t *testing.T) {
	for _, partial := range []string{"", "pid="} {
		t.Run(fmt.Sprintf("bytes-%d", len(partial)), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "creating.lock")
			first, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
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
			if unlock, err := AcquirePath(path, "competitor"); err == nil {
				unlock()
				t.Fatal("competitor acquired before the first owner published its PID")
			} else if !strings.Contains(err.Error(), "lock held") {
				t.Fatal(err)
			}
			current, err := os.Stat(path)
			if err != nil || !os.SameFile(identity, current) {
				t.Fatal("competitor replaced the first owner's inode")
			}
			// A process that died during creation leaves no PID to query. Its
			// unpublished lock remains recoverable after the existing age limit.
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
			old := time.Now().Add(-staleAge - time.Second)
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
			unlock, err := AcquirePath(path, "recovery")
			if err != nil {
				t.Fatal(err)
			}
			unlock()
		})
	}
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
