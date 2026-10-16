package atomicfile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestReplacePublishesCompletedFile(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"new", "regular", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			destination := filepath.Join(root, "制品😀.json")
			linked := filepath.Join(root, "link-target.json")
			original := []byte("original target\n")
			switch kind {
			case "regular":
				if err := os.WriteFile(destination, original, 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.WriteFile(linked, original, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(linked, destination); err != nil {
					t.Fatal(err)
				}
			}
			for _, content := range [][]byte{[]byte("first completed file\n"), []byte("second completed file\n")} {
				source := filepath.Join(root, "completed.json")
				if err := os.WriteFile(source, content, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := Replace(source, destination); err != nil {
					t.Fatal(err)
				}
				info, err := os.Lstat(destination)
				if err != nil || !info.Mode().IsRegular() {
					t.Fatalf("published entry = %v, %v", info, err)
				}
				if got, err := os.ReadFile(destination); err != nil || !bytes.Equal(got, content) {
					t.Fatalf("published bytes = %q, %v", got, err)
				}
				if _, err := os.Stat(source); !os.IsNotExist(err) {
					t.Fatalf("completed source remains: %v", err)
				}
				if kind == "symlink" {
					if got, err := os.ReadFile(linked); err != nil || !bytes.Equal(got, original) {
						t.Fatalf("linked target changed = %q, %v", got, err)
					}
				}
			}
		})
	}
}
