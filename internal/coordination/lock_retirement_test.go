package coordination

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Blocking in the native call proves this waiter has opened the holder's file.
// It exposes a real native boundary without a test hook in the lock owner.
func waitForBlockedPathLock() {
	for {
		stack := make([]byte, 64*1024)
		stack = stack[:runtime.Stack(stack, true)]
		if strings.Contains(string(stack), "formal-gates/internal/coordination.lockFileBlocking(") {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRetiredPathWaitersUseCanonicalLock(t *testing.T) {
	for _, acquire := range []struct {
		name string
		lock func(string, string) (func(), error)
	}{{"create", AcquirePathWaiting}, {"existing", AcquireExistingPathWaiting}} {
		t.Run(acquire.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "owner.lock")
			release, err := AcquirePath(path, "owner")
			if err != nil {
				t.Fatal(err)
			}
			var active atomic.Int32
			result := make(chan error, 1)
			go func() {
				unlock, err := acquire.lock(path, "waiter")
				if err != nil {
					result <- err
					return
				}
				defer unlock()
				if active.Add(1) != 1 {
					result <- errors.New("old waiter entered business beside the canonical holder")
					return
				}
				defer active.Add(-1)
				peer, err := AcquirePath(path, "canonical assertion")
				if err == nil {
					peer()
					result <- errors.New("waiter did not hold the canonical file")
					return
				}
				var held *LockHeldError
				if !errors.As(err, &held) {
					result <- err
					return
				}
				result <- nil
			}()
			waitForBlockedPathLock()
			if err := RetirePath(path); err != nil {
				release()
				t.Fatal(err)
			}
			current, err := AcquirePath(path, "current owner")
			if err != nil {
				release()
				t.Fatal(err)
			}
			active.Store(1)
			release()
			waitForBlockedPathLock()
			select {
			case err := <-result:
				current()
				t.Fatalf("waiter returned during canonical business: %v", err)
			default:
			}
			active.Store(0)
			current()
			if err := <-result; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRetiredExistingPathWaiterDoesNotRecreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.lock")
	release, err := AcquirePath(path, "owner")
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		unlock, err := AcquireExistingPathWaiting(path, "existing waiter")
		if err == nil {
			unlock()
		}
		result <- err
	}()
	waitForBlockedPathLock()
	if err := RetirePath(path); err != nil {
		release()
		t.Fatal(err)
	}
	release()
	if err := <-result; !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retired existing waiter: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retired path recreated: %v", err)
	}
}
