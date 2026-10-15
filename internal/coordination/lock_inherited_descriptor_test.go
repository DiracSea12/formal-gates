//go:build !windows

package coordination

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestLockReleaseDoesNotWaitForInheritedDescriptor(t *testing.T) {
	for _, acquire := range []struct {
		name string
		lock func(*os.File) error
	}{{"immediate", lockFile}, {"waiting", lockFileBlocking}} {
		t.Run(acquire.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "owner.lock")
			owner, err := openLockFile(path, true)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { releaseLockFile(owner) })
			if err := acquire.lock(owner); err != nil {
				t.Fatal(err)
			}
			// Dup shares the open file description inherited between fork and exec.
			fd, err := syscall.Dup(int(owner.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			inherited := os.NewFile(uintptr(fd), path)
			defer inherited.Close()
			releaseLockFile(owner)
			release, err := AcquirePath(path, "after release")
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			releaseLockFile(owner)
			peer, err := AcquirePath(path, "current holder probe")
			if err == nil {
				peer()
				t.Fatal("repeated release unlocked the current holder")
			}
			var held *LockHeldError
			if !errors.As(err, &held) {
				t.Fatal(err)
			}
		})
	}
}
