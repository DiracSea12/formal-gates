//go:build windows

package atomicfile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestReadFileDuringPublication(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"new", "replacement"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "exit.json")
			if name == "replacement" {
				if err := os.WriteFile(path, []byte("{\"exit_code\":1}\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			want := []byte("{\"exit_code\":0,\"error\":\"\",\"request_digest\":\"receipt-proof\"}\n")
			tmp, err := os.CreateTemp(filepath.Dir(path), ".exit.json.tmp")
			if err != nil {
				t.Fatal(err)
			}
			tmpName := tmp.Name()
			defer os.Remove(tmpName)
			if _, err := tmp.Write(want); err != nil {
				tmp.Close()
				t.Fatal(err)
			}
			if err := tmp.Sync(); err != nil {
				tmp.Close()
				t.Fatal(err)
			}
			if err := tmp.Close(); err != nil {
				t.Fatal(err)
			}
			sourceName, err := windows.UTF16PtrFromString(tmpName)
			if err != nil {
				t.Fatal(err)
			}
			handle, err := windows.CreateFile(sourceName, windows.DELETE,
				windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
				nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
			if err != nil {
				t.Fatal(err)
			}
			publisher := os.NewFile(uintptr(handle), tmpName)
			defer publisher.Close()
			if err := os.Rename(tmpName, path); err != nil {
				t.Fatalf("publish completed receipt: %v", err)
			}
			if _, err := os.ReadFile(path); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
				t.Fatalf("reader without shared delete = %v, want ERROR_SHARING_VIOLATION", err)
			}
			for i := 0; i < 2; i++ {
				if actual, err := ReadFile(path); err != nil || !bytes.Equal(actual, want) {
					t.Fatalf("published bytes with open publisher = %q, %v; want %q", actual, err, want)
				}
			}
			if err := publisher.Close(); err != nil {
				t.Fatal(err)
			}
			if actual, err := ReadFile(path); err != nil || !bytes.Equal(actual, want) {
				t.Fatalf("published bytes after close = %q, %v; want %q", actual, err, want)
			}
		})
	}
}

func TestReadFileReportsDeniedRead(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "exit.json")
	if err := os.WriteFile(path, []byte("{\"exit_code\":0}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, 0,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	_, err = ReadFile(path)
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || pathErr.Op != "open" || pathErr.Path != path || !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("denied read error = %#v, want original open path and sharing violation", err)
	}
}
