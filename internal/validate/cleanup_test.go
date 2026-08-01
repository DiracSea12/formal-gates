package validate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupTempRunsSweepsTerminatedOnly(t *testing.T) {
	root := t.TempDir()
	makeTempRun(t, root, "sealed-run", "SEALED")
	makeTempRun(t, root, "aborted-run", "ABORTED")
	makeTempRun(t, root, "active-run", "ACTIVE")
	if err := os.MkdirAll(filepath.Join(cleanRoot(root), ".gates", "tmp", "no-state-run"), 0o700); err != nil {
		t.Fatal(err)
	}

	result, err := CleanupTempRuns(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Cleaned; len(got) != 2 || !contains(got, "sealed-run") || !contains(got, "aborted-run") {
		t.Fatalf("cleaned = %v, want sealed-run and aborted-run", got)
	}
	if got := result.Active; len(got) != 1 || got[0] != "active-run" {
		t.Fatalf("active = %v, want [active-run]", got)
	}
	if got := result.Unknown; len(got) != 1 || got[0] != "no-state-run" {
		t.Fatalf("unknown = %v, want [no-state-run]", got)
	}
	for _, id := range []string{"sealed-run", "aborted-run"} {
		if _, err := os.Stat(RunDir(root, id)); !os.IsNotExist(err) {
			t.Fatalf("%s temp dir still exists", id)
		}
	}
	for _, id := range []string{"active-run", "no-state-run"} {
		if _, err := os.Stat(RunDir(root, id)); err != nil {
			t.Fatalf("%s temp dir must be left alone: %v", id, err)
		}
	}
}

func TestCleanupTempRunExplicitDeletesActive(t *testing.T) {
	root := t.TempDir()
	makeTempRun(t, root, "abandoned-run", "ACTIVE")

	deleted, err := CleanupTempRun(root, "abandoned-run")
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("expected the run temp dir to be deleted")
	}
	if _, err := os.Stat(RunDir(root, "abandoned-run")); !os.IsNotExist(err) {
		t.Fatal("temp dir still exists after explicit cleanup")
	}
}

func TestCleanupTempRunRejectsUnsafeNames(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"", ".", "..", "a/b", "a\\b", "../escape"} {
		if _, err := CleanupTempRun(root, id); err == nil {
			t.Fatalf("unsafe run id %q must be rejected", id)
		}
	}
}

func TestCleanupTempRunMissingIsNotError(t *testing.T) {
	root := t.TempDir()
	deleted, err := CleanupTempRun(root, "never-existed")
	if err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Fatal("missing run must report deleted=false")
	}
}

func TestCleanupNeverTouchesResults(t *testing.T) {
	root := t.TempDir()
	makeTempRun(t, root, "sealed-run", "SEALED")
	results := filepath.Join(cleanRoot(root), ".gates", "results")
	if err := os.MkdirAll(results, 0o700); err != nil {
		t.Fatal(err)
	}
	summary := filepath.Join(results, "sealed-run.json")
	if err := os.WriteFile(summary, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := CleanupTempRuns(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(summary); err != nil {
		t.Fatalf("results summary must survive cleanup: %v", err)
	}
}

func makeTempRun(t *testing.T, root, runID, status string) {
	t.Helper()
	state := NewRunState(runID, "formal", "req.md", "rev", "git", "base", "cur", "baseprompt", "cat", true, []string{"complexity-gate"}, nil)
	state.Status = status
	if err := SaveRunState(root, state); err != nil {
		t.Fatal(err)
	}
}
