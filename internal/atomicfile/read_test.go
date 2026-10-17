package atomicfile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadFilePreservesBytesAndMissingError(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "收据😀.json")
	want := []byte("{\"result\":\"原始字节\"}\r\n\x00")
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	if actual, err := ReadFile(path); err != nil || !bytes.Equal(actual, want) {
		t.Fatalf("read bytes = %q, %v; want %q", actual, err, want)
	}
	missing := filepath.Join(t.TempDir(), "missing.json")
	_, err := ReadFile(missing)
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || pathErr.Op != "open" || pathErr.Path != missing || !os.IsNotExist(err) {
		t.Fatalf("missing file error = %#v, want original open path and not-exist error", err)
	}
}
