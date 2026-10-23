//go:build windows

package validate

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestHookReadsStateDuringPublication(t *testing.T) {
	root := t.TempDir()
	writeCurrentHookFixture(t, root, "hook-publication", true, false, "", "owner")
	path := filepath.Join(root, ".gates", "engine", "hook-publication", "state.json")
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := windows.CreateFile(name, windows.DELETE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(publisher)
	state, err := readHookEngineState(path)
	if err != nil || state.RunID != "hook-publication" || state.OwnerSession != "owner" {
		t.Fatalf("published state with its publisher open: state=%+v error=%v", state, err)
	}
}
