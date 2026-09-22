//go:build !windows

package runner

import (
	"os"
	"syscall"
)

// dirLock is an exclusive advisory flock on the run directory's lock file,
// held for the owner's whole lifetime. The kernel releases it automatically
// when the owning process dies — no heartbeat, no stale-lock recovery.
type dirLock struct {
	f *os.File
}

// lockDir creates (if needed) and exclusively locks the file at path
// without blocking.  flock conflicts are per open file description, so a
// second Open fails even from the same process.
func lockDir(path string) (*dirLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return &dirLock{f: f}, nil
}

// unlock closes the descriptor, which releases the flock.  The lock file
// itself is deliberately left in place (see store.go).
func (l *dirLock) unlock() {
	_ = l.f.Close()
}
