package main

import (
	"os"
	"path/filepath"
	"testing"
)

// expectedChecks is the contract of this probe. A silently missing check is a
// failure, not a pass, so the list is asserted rather than counted.
var expectedChecks = []string{
	"open-file-database",
	"schema-and-pragmas",
	"foreign-key-rejected",
	"transaction-commit-and-rollback",
	"wal-coordination-files",
	"online-backup-consistent",
	"wal-reader-not-blocked-by-open-writer",
	"busy-reported-then-resolved",
}

func TestProbeChecksAllPass(t *testing.T) {
	report, err := Run(t.TempDir())
	if err != nil {
		t.Fatalf("probe run: %v", err)
	}
	seen := map[string]CheckResult{}
	for _, c := range report.Checks {
		if _, dup := seen[c.ID]; dup {
			t.Fatalf("duplicate check id %q", c.ID)
		}
		seen[c.ID] = c
	}
	for _, id := range expectedChecks {
		c, ok := seen[id]
		if !ok {
			t.Fatalf("expected check %q was not reported", id)
		}
		if c.Status != "PASS" {
			t.Errorf("check %q: status=%s error=%s", id, c.Status, c.Error)
		}
	}
	if len(seen) != len(expectedChecks) {
		t.Errorf("reported %d checks, contract has %d", len(seen), len(expectedChecks))
	}
	if !report.OK || len(report.Failed) != 0 {
		t.Fatalf("report not OK: failed=%v", report.Failed)
	}
	if report.SQLiteVersion == "" {
		t.Error("sqlite version was not reported")
	}
}

func TestProbeIsReproducibleAcrossDirectories(t *testing.T) {
	first, err := Run(filepath.Join(t.TempDir(), "a"))
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := Run(filepath.Join(t.TempDir(), "b"))
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(first.Checks) != len(second.Checks) {
		t.Fatalf("check counts differ: %d vs %d", len(first.Checks), len(second.Checks))
	}
	for i := range first.Checks {
		if first.Checks[i].ID != second.Checks[i].ID || first.Checks[i].Status != second.Checks[i].Status {
			t.Errorf("check %d differs: %+v vs %+v", i, first.Checks[i], second.Checks[i])
		}
	}
	if first.SQLiteVersion != second.SQLiteVersion {
		t.Errorf("sqlite version differs: %q vs %q", first.SQLiteVersion, second.SQLiteVersion)
	}
}

func TestProbeRefusesUnusableDirectory(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(filepath.Join(blocker, "child")); err == nil {
		t.Fatal("expected an error when the probe directory cannot be created")
	}
}
