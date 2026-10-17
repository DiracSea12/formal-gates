package coordination

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPathLockWaitsForCleanupWithExclusiveAdmission(t *testing.T) {
	for _, retire := range []bool{false, true} {
		name := "retained"
		if retire {
			name = "retired"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "maintenance.lock")
			cleanup, err := openLockFile(path, true)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { cleanup.Close() })
			if _, err := cleanup.Write([]byte{0, 0}); err != nil {
				t.Fatal(err)
			}
			before, err := cleanup.Stat()
			if err != nil {
				t.Fatal(err)
			}
			// The PowerShell cleanup process locks only the business byte.
			if err := windows.LockFileEx(windows.Handle(cleanup.Fd()),
				windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
				0, 1, 0, &windows.Overlapped{}); err != nil {
				t.Fatal(err)
			}
			observer, err := openLockFile(path, false)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { observer.Close() })
			acquired := make(chan error, 1)
			releaseForeground := make(chan struct{})
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				unlock, err := AcquirePath(path, "foreground")
				acquired <- err
				if err == nil {
					defer unlock()
					<-releaseForeground
				}
			}()
			t.Cleanup(func() {
				cleanup.Close()
				close(releaseForeground)
				<-finished
			})
			// Reading the admission byte observes its kernel lock without taking
			// a competing reservation.
			for {
				select {
				case err := <-acquired:
					t.Fatalf("foreground returned while cleanup held the business byte: %v", err)
				default:
				}
				var content [1]byte
				_, err := observer.ReadAt(content[:], 1)
				if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				runtime.Gosched()
			}
			assertHeld := func() {
				t.Helper()
				unlock, err := AcquirePath(path, "another foreground")
				if err == nil {
					unlock()
					t.Fatal("another foreground acquired the admitted caller's lock")
				}
				var held *LockHeldError
				if !errors.As(err, &held) || held.Operation != "another foreground" || held.Path != path {
					t.Fatalf("foreground contention error = %v", err)
				}
			}
			assertHeld()
			if retire {
				if err := RetirePath(path); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("cleanup did not retire the canonical path: %v", err)
				}
			}
			if err := cleanup.Close(); err != nil {
				t.Fatal(err)
			}
			if err := <-acquired; err != nil {
				t.Fatalf("foreground after cleanup release: %v", err)
			}
			current, err := os.Stat(path)
			if err != nil || os.SameFile(before, current) == retire {
				t.Fatalf("foreground canonical identity (retired=%v): %v", retire, err)
			}
			assertHeld()
		})
	}
}
