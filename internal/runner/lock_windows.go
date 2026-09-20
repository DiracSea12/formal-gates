//go:build windows

package runner

import (
	"os"

	"golang.org/x/sys/windows"
)

// dirLock is the Windows counterpart of the flock-based ownership lock:
// LockFileEx over the first byte of the lock file, FAIL_IMMEDIATELY so a
// second owner gets an immediate busy-style error instead of blocking.  As
// on Unix, the kernel releases the lock when the owning process exits,
// which is the whole point of ownership here.
type dirLock struct {
	f *os.File
}

// lockDir creates (if needed) and exclusively locks the file at path
// without blocking.
func lockDir(path string) (*dirLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &windows.Overlapped{}); err != nil {
		f.Close()
		return nil, err
	}
	return &dirLock{f: f}, nil
}

// unlock releases the byte-range lock and closes the handle.  The lock file
// itself is deliberately left in place (see store.go).
func (l *dirLock) unlock() {
	_ = windows.UnlockFileEx(windows.Handle(l.f.Fd()), 0, 1, 0, &windows.Overlapped{})
	_ = l.f.Close()
}
