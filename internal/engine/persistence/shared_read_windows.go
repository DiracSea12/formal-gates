package persistence

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// Contenders must allow the owner to delete its lock while they read it.
// os.Open omits FILE_SHARE_DELETE on Windows, which can strand a released lock.
func openSharedFile(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}

// Windows may temporarily deny replacement while an existing snapshot reader
// is closing. Retry only those sharing errors, with a fixed deadline.
func replaceStateFile(source, target string) error {
	deadline := time.Now().Add(time.Second)
	for {
		err := os.Rename(source, target)
		if err == nil || (!errors.Is(err, syscall.ERROR_ACCESS_DENIED) && !errors.Is(err, syscall.Errno(32))) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(time.Millisecond)
	}
}
