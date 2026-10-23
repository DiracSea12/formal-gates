//go:build windows

package facade

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"formal-gates/internal/atomicfile"

	"golang.org/x/sys/windows"
)

func TestStateObserversReadOriginalRunDuringPublication(t *testing.T) {
	root := t.TempDir()
	transcript := filepath.Join(root, "owner.jsonl")
	writeTranscript(t, transcript, []string{intervalBaseLine})
	f := startIntervalRun(t, root, "publication-owner", transcript)
	path := filepath.Join(f.stateDir, "state.json")
	original, err := atomicfile.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := Diagnose(path, "", "")
	if err != nil || !baseline.JSONReadable || baseline.Integrity != "readable" {
		t.Fatalf("original diagnostic = %+v, %v", baseline, err)
	}
	started := time.Now().UTC().Format(time.RFC3339Nano)
	observe := func(t *testing.T) {
		t.Run("diagnose", func(t *testing.T) {
			for observation := 0; observation < 2; observation++ {
				report, err := Diagnose(path, "", "")
				if err != nil || !reflect.DeepEqual(report, baseline) {
					t.Fatalf("published diagnostic changed original run = %+v, %v", report, err)
				}
			}
		})
		t.Run("open peer interval", func(t *testing.T) {
			for observation := 0; observation < 2; observation++ {
				peers, resolvable := engineOverlapPeers(root, "observer", transcript)
				if !resolvable || !reflect.DeepEqual(peers, []string{f.runID}) {
					t.Fatalf("published open peer interval = %v, resolvable=%t; want original peer %s", peers, resolvable, f.runID)
				}
			}
		})
		t.Run("closed peer intervals", func(t *testing.T) {
			for observation := 0; observation < 2; observation++ {
				if engineSummaryOverlap(root, "observer", transcript, started) {
					t.Fatal("active peer without a closed receipt was treated as unreadable closed-interval overlap")
				}
			}
		})
	}
	t.Run("before publication", observe)

	tmp, err := os.CreateTemp(f.stateDir, ".published-state-*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	tempName := tmp.Name()
	t.Cleanup(func() { os.Remove(tempName) })
	if _, err := tmp.Write(original); err != nil {
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
	name, err := windows.UTF16PtrFromString(tempName)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.DELETE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	publisher := os.NewFile(uintptr(handle), tempName)
	t.Cleanup(func() { publisher.Close() })
	if err := atomicfile.Replace(tempName, path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(path); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("reader without shared delete = %v, want sharing violation", err)
	}
	t.Run("open publication handle", observe)
	if err := publisher.Close(); err != nil {
		t.Fatal(err)
	}
	t.Run("after publication", observe)
	after, err := atomicfile.ReadFile(path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatalf("observers changed original run bytes = %q, %v", after, err)
	}
}
