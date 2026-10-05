package persistence

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

// Cleanup retires the checkpoint payload alongside state while retaining the
// audit files. Every completed removal prefix resumes through the same owner.
func TestTerminalCleanupRetiresCheckpointPayload(t *testing.T) {
	for _, removed := range []string{"", stateFileName, "slice-receipt.json"} {
		t.Run("removed="+removed, func(t *testing.T) {
			dir := t.TempDir()
			files := map[string][]byte{
				stateFileName:                  []byte("active state"),
				"slice-receipt.json":           []byte(`{"digest":"checkpoint","receipt":{"snapshot":"accepted"}}`),
				"request.json":                 []byte(`{"runId":"child"}`),
				"terminal-summary.intent.json": []byte(`{"status":"ABORTED","runId":"child"}`),
			}
			for name, body := range files {
				if err := os.WriteFile(filepath.Join(dir, name), body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if removed != "" {
				if err := writeCleanupEvidence(filepath.Join(dir, "cleanup.intent.json"), map[string]any{"status": "requested"}); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(dir, removed)); err != nil {
					t.Fatal(err)
				}
			}
			store, err := NewStore(dir, Config{PackageDigest: testPackageDigest})
			if err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			errs := make(chan error, 2)
			for range 2 {
				wg.Add(1)
				go func() { defer wg.Done(); errs <- store.CleanupTerminal() }()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{stateFileName, "slice-receipt.json", "cleanup.intent.json"} {
				if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
					t.Fatalf("owned payload remains: %s %v", name, err)
				}
			}
			for _, name := range []string{"request.json", "terminal-summary.intent.json"} {
				body, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil || !bytes.Equal(body, files[name]) {
					t.Fatalf("audit content changed: %s %v", name, err)
				}
			}
			path := filepath.Join(dir, "cleanup.receipt.json")
			before, err := os.ReadFile(path)
			if err != nil || string(before) != "{\"entries\":[],\"residue\":false,\"status\":\"reconciled\"}\n" {
				t.Fatalf("cleanup evidence: %s %v", before, err)
			}
			stat, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			store, err = NewStore(dir, Config{PackageDigest: testPackageDigest})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.CleanupTerminal(); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			afterStat, statErr := os.Stat(path)
			if err != nil || statErr != nil || !bytes.Equal(before, after) || !reflect.DeepEqual(stat.ModTime(), afterStat.ModTime()) {
				t.Fatal("completed cleanup replay refreshed evidence", err, statErr)
			}
		})
	}
}
