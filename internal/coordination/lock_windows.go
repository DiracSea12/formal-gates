//go:build windows

package coordination

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func openLockFile(path string, create bool) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	disposition := uint32(windows.OPEN_EXISTING)
	if create {
		disposition = windows.OPEN_ALWAYS
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, disposition, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}

// Closing the POSIX delete handle removes the visible name while the caller's
// independent lock handle still excludes old openers from protected business.
func retirePath(path string) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(name, windows.DELETE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return &os.PathError{Op: "remove", Path: path, Err: err}
	}
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS)
	err = windows.SetFileInformationByHandle(handle, windows.FileDispositionInfoEx,
		(*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags)))
	return errors.Join(err, windows.CloseHandle(handle))
}

// Go callers reserve byte 1 before taking the business lock at byte 0.
// Admission conflicts fail immediately; the internal install cleanup process
// holds only byte 0, so an admitted caller waits for its protected work.
func lockFile(f *os.File) error {
	if err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &windows.Overlapped{Offset: 1}); err != nil {
		return err
	}
	return windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK,
		0, 1, 0, &windows.Overlapped{})
}

// isLockHeld 报告 err 是否表示锁已被其他句柄持有。
func isLockHeld(err error) bool {
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

// lockFileBlocking waits for the same ranges as lockFile.
func lockFileBlocking(f *os.File) error {
	if err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK,
		0, 1, 0, &windows.Overlapped{Offset: 1}); err != nil {
		return err
	}
	return windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK,
		0, 1, 0, &windows.Overlapped{})
}

func releaseLockFile(f *os.File) { _ = f.Close() }
