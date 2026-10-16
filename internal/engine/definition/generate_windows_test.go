//go:build windows

package definition

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// openGeneratedReader permits rename while retaining the reader's original file.
func openGeneratedReader(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}

func TestWriteGeneratedPreservesOpenReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "制品😀.json")
	stale := []byte("{\"stale\":true}\n")
	fresh := []byte("{\"fresh\":true}\n")
	if err := writeGenerated(path, stale); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	reader, err := openGeneratedReader(path)
	if err != nil {
		t.Fatalf("open generated artifact: %v", err)
	}
	defer reader.Close()

	if err := writeGenerated(path, fresh); err != nil {
		t.Fatalf("replace generated artifact with open reader: %v", err)
	}
	oldBytes, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read original artifact: %v", err)
	}
	if !bytes.Equal(oldBytes, stale) {
		t.Fatalf("open reader = %q, want original bytes %q", oldBytes, stale)
	}
	newBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replaced artifact: %v", err)
	}
	if !bytes.Equal(newBytes, fresh) {
		t.Fatalf("new reader = %q, want replacement bytes %q", newBytes, fresh)
	}
}
