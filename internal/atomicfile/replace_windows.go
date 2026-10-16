//go:build windows

package atomicfile

import (
	"errors"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Replace publishes a completed file by replacing the destination directory entry.
func Replace(source, destination string) error {
	destination, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	name, err := windows.UTF16FromString(destination)
	if err != nil {
		return err
	}
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}

	// FILE_RENAME_INFO carries a NUL-terminated name; FileNameLength excludes the NUL.
	type fileRenameInformation struct {
		Flags          uint32
		RootDirectory  windows.Handle
		FileNameLength uint32
		FileName       [1]uint16
	}
	var layout fileRenameInformation
	nameLength := len(name) - 1
	buffer := make([]byte, int(unsafe.Offsetof(layout.FileName))+len(name)*2)
	info := (*fileRenameInformation)(unsafe.Pointer(&buffer[0]))
	info.Flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS
	info.FileNameLength = uint32(nameLength * 2)
	copy(unsafe.Slice(&info.FileName[0], len(name)), name)

	handle, err := windows.CreateFile(sourcePtr, windows.DELETE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return err
	}
	err = windows.SetFileInformationByHandle(handle, windows.FileRenameInfoEx,
		&buffer[0], uint32(len(buffer)))
	return errors.Join(err, windows.CloseHandle(handle))
}
