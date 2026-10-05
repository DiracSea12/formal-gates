package runner

// Session source references travel atomically with their native UUID through
// the existing session owner, including supported database-format upgrades.
import (
	"context"
	"testing"
)

func TestSessionSourceMigrationAndReplacement(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateRun(ctx, "source", []TaskSpec{{TaskID: "a", Input: []byte("input")}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSessionSource(ctx, "a", "original", "original-job"); err != nil {
		t.Fatal(err)
	}
	// Construct the supported format whose session row contains only UUID.
	if _, err := store.db.Exec("ALTER TABLE task_sessions DROP COLUMN source_job"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("PRAGMA user_version=5"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity, source, err := store.SessionSource(ctx, "a")
	if err != nil || identity != "original" || source != "" {
		t.Fatalf("migration %q %q %v", identity, source, err)
	}
	if err := store.SetSessionSource(ctx, "a", "replacement", "replacement-job"); err != nil {
		t.Fatal(err)
	}
	identity, source, err = store.SessionSource(ctx, "a")
	if err != nil || identity != "replacement" || source != "replacement-job" {
		t.Fatalf("replacement %q %q %v", identity, source, err)
	}
	if err := store.SetSession(ctx, "a", "another-host"); err != nil {
		t.Fatal(err)
	}
	identity, source, err = store.SessionSource(ctx, "a")
	if err != nil || identity != "another-host" || source != "" {
		t.Fatalf("unpaired source retained %q %q %v", identity, source, err)
	}
}
