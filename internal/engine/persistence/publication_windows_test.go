//go:build windows

package persistence

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/atomicfile"

	"golang.org/x/sys/windows"
)

func openPublishedState(t *testing.T, path string, access uint32) *os.File {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(handle), path)
	t.Cleanup(func() { file.Close() })
	return file
}

func TestLoadDuringStatePublication(t *testing.T) {
	store := newTestStore(t)
	saveOne(t, store, map[string]any{"value": "original"})
	content := []byte(`{"value":"published","padding":"完整状态"}`)
	doc := newDocument(store.cfg, 2, contentDigestOf(content), content)
	tempName, err := store.writeTempDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	publisher := openPublishedState(t, filepath.Join(store.dir, tempName), windows.DELETE)
	if err := store.executeCommit(tempName); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(store.statePath()); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("reader without shared delete = %v, want sharing violation", err)
	}
	for observation := 0; observation < 2; observation++ {
		snapshot, err := store.Load()
		if err != nil || snapshot.Revision != 2 || snapshot.ContentDigest != doc.ContentDigest || !bytes.Equal(snapshot.Content, content) {
			t.Fatalf("published state = %+v, %v; want complete revision 2 content %s", snapshot, err, content)
		}
	}
	if err := publisher.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Load()
	if err != nil || snapshot.Revision != 2 || !bytes.Equal(snapshot.Content, content) {
		t.Fatalf("state after publisher closed = %+v, %v", snapshot, err)
	}
}

func TestStateCommitAndRecoveryKeepOpenReadersOnCompleteSnapshots(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		name := "save"
		if recovery {
			name = "recover"
		}
		t.Run(name, func(t *testing.T) {
			store := newTestStore(t)
			saveOne(t, store, map[string]any{"value": "original", "padding": "原始完整状态"})
			original, err := atomicfile.ReadFile(store.statePath())
			if err != nil {
				t.Fatal(err)
			}
			reader := openPublishedState(t, store.statePath(), windows.GENERIC_READ)
			if recovery {
				store.cfg.FaultInjector = func(point FaultPoint) error {
					if point == FaultExecuteBefore {
						return &InjectedCrashError{Point: point}
					}
					return nil
				}
			}
			content := map[string]any{"value": "committed", "padding": "替换完整状态"}
			tx := Transaction{ExpectedRevision: 1, ExpectedFingerprint: "fp", CollectFingerprint: staticFingerprint("fp"), Content: content}
			result, err := store.Save(tx)
			if recovery {
				if !errors.Is(err, ErrInjectedCrash) {
					t.Fatalf("interrupted save = %+v, %v", result, err)
				}
				reopened, err := NewStore(store.dir, Config{PackageDigest: testPackageDigest})
				if err != nil {
					t.Fatal(err)
				}
				store = reopened
				report, err := store.Recover()
				if err != nil || report.Outcome != RecoveryRecovered || report.Revision != 2 {
					t.Fatalf("recover while original reader open = %+v, %v", report, err)
				}
			} else if err != nil || result.Revision != 2 {
				t.Fatalf("save while original reader open = %+v, %v", result, err)
			}
			oldBytes, err := io.ReadAll(reader)
			if err != nil || !bytes.Equal(oldBytes, original) {
				t.Fatalf("original open reader changed bytes = %q, %v; want %q", oldBytes, err, original)
			}
			wantContent := []byte(`{"padding":"替换完整状态","value":"committed"}`)
			wantDocument, err := canonicalJSON(newDocument(store.cfg, 2, contentDigestOf(wantContent), wantContent))
			if err != nil {
				t.Fatal(err)
			}
			current, err := atomicfile.ReadFile(store.statePath())
			if err != nil || !bytes.Equal(current, wantDocument) {
				t.Fatalf("current complete state bytes = %q, %v; want %q", current, err, wantDocument)
			}
			if _, err := store.Save(tx); !errors.As(err, new(*RevisionConflictError)) {
				t.Fatalf("repeated stale save = %v, want revision conflict", err)
			}
			report, err := store.Recover()
			if err != nil || report.Outcome != RecoveryClean || report.Revision != 2 {
				t.Fatalf("repeated recovery = %+v, %v", report, err)
			}
			after, err := atomicfile.ReadFile(store.statePath())
			if err != nil || !bytes.Equal(after, wantDocument) {
				t.Fatalf("repeated operations changed committed bytes = %q, %v", after, err)
			}
			assertStateDirExact(t, store.dir, stateFileName, lockFileName)
		})
	}
}
