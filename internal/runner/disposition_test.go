package runner

// The task_dispositions contract (disposition.go) and the v5 migration
// self-check over every accepted starting point (0/2/3/4): the park rows
// and the runs terminal columns are part of the still-unaccepted v5
// delta, so every baseline must step forward into the same shape and
// carry the new control facts.

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

// The park-row lifecycle one flow relies on: a park lands, the listing
// reads it back in stable order, a later park of the same task replaces
// it (the latest park decides the next move), and the clear that follows
// an applied answer leaves nothing standing — idempotently.
func TestDispositionLifecycle(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st := mustCreate(t, dir, "disp", []TaskSpec{
		{TaskID: "b", Name: "B", Input: []byte("in-b")},
		{TaskID: "a", Name: "A", Input: []byte("in-a")},
	})
	defer st.Close()

	if rows, err := st.Dispositions(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("fresh dispositions = %v (err %v), want none", rows, err)
	}
	if _, found, err := st.Disposition(ctx, "a"); err != nil || found {
		t.Fatalf("fresh disposition lookup found=%v err=%v, want absent", found, err)
	}
	first := Disposition{TaskID: "a", Kind: "runtime", Generation: 3, Attempt: 1,
		Reason: "the host refused", Evidence: "ref-1"}
	if err := st.SetDisposition(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := st.SetDisposition(ctx, Disposition{TaskID: "b", Kind: "worker-blocked",
		Generation: 1, Attempt: 2, Reason: "blocked"}); err != nil {
		t.Fatal(err)
	}
	rows, err := st.Dispositions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].TaskID != "a" || rows[1].TaskID != "b" {
		t.Fatalf("dispositions = %+v, want a then b in task order", rows)
	}
	got, found, err := st.Disposition(ctx, "a")
	if err != nil || !found {
		t.Fatalf("disposition a found=%v err=%v", found, err)
	}
	if got != first {
		t.Fatalf("disposition a = %+v, want %+v", got, first)
	}
	// The latest park wins: the same task's re-park replaces the row.
	second := Disposition{TaskID: "a", Kind: "runtime", Generation: 4, Attempt: 1,
		Reason: "the host refused again"}
	if err := st.SetDisposition(ctx, second); err != nil {
		t.Fatal(err)
	}
	if got, found, err := st.Disposition(ctx, "a"); err != nil || !found || got != second {
		t.Fatalf("re-parked disposition = %+v (found %v err %v), want %+v", got, found, err, second)
	}
	// The clear an applied answer performs is idempotent.
	for i := 0; i < 2; i++ {
		if err := st.ClearDisposition(ctx, "a"); err != nil {
			t.Fatal(err)
		}
	}
	if rows, err := st.Dispositions(ctx); err != nil || len(rows) != 1 || rows[0].TaskID != "b" {
		t.Fatalf("dispositions after clear = %v (err %v), want only b", rows, err)
	}
}

// The runs terminal columns' roundtrip: empty before any judgment, both
// facts written together by the refusal paths, and the seal paths'
// terminal-only write leaving no reason behind.
func TestRunTerminalRoundtrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st := mustCreate(t, dir, "term", []TaskSpec{{TaskID: "a", Name: "A", Input: []byte("in-a")}})
	defer st.Close()

	terminal, reason, err := st.Terminal(ctx)
	if err != nil || terminal != "" || reason != "" {
		t.Fatalf("fresh terminal = (%q,%q) err=%v, want empty", terminal, reason, err)
	}
	if err := st.SetTerminal(ctx, "ABORTED", "user declined at a decision boundary"); err != nil {
		t.Fatal(err)
	}
	terminal, reason, err = st.Terminal(ctx)
	if err != nil || terminal != "ABORTED" || reason != "user declined at a decision boundary" {
		t.Fatalf("terminal after refusal = (%q,%q) err=%v", terminal, reason, err)
	}
	if err := st.SetTerminal(ctx, "HARNESS_COMPLETE", ""); err != nil {
		t.Fatal(err)
	}
	if terminal, reason, err := st.Terminal(ctx); err != nil || terminal != "HARNESS_COMPLETE" || reason != "" {
		t.Fatalf("terminal after seal = (%q,%q) err=%v, want the new judgment", terminal, reason, err)
	}
	// A reopened owner reads the same facts from the same row.
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	if terminal, _, err := st2.Terminal(ctx); err != nil || terminal != "HARNESS_COMPLETE" {
		t.Fatalf("terminal after reopen = %q err=%v, want the persisted fact", terminal, err)
	}
}

// rewindToV rewinds a current-build file to an accepted baseline's
// shape: every table added after that version is dropped, the terminal
// columns are emptied, and the stamp resets — the runs columns
// themselves stay, because the v5 step adds them additively-idempotent,
// which is exactly the reshape this rewind exercises.
func rewindToV(t *testing.T, dir string, version int) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	drops := []string{"task_revisions", "task_sessions", "task_dispositions"}
	if version < 4 {
		drops = append(drops, "user_requests", "call_observations")
	}
	if version < 3 {
		drops = append(drops, "decisions")
	}
	for _, table := range drops {
		if _, err := db.Exec("DROP TABLE " + table); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("UPDATE runs SET terminal=NULL, stop_reason=NULL"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestControlStateMigration steps every accepted baseline — a fresh
// file (0) and rewound v2, v3 and v4 files — forward to this build's v5:
// the version stamps, and the delta's control-state pieces (the park
// rows and the runs terminal columns) work on every stepped database,
// with the v5 byte-carrier and session facts intact alongside them.
func TestControlStateMigration(t *testing.T) {
	ctx := context.Background()
	for _, version := range []int{0, 2, 3, 4} {
		name := map[int]string{0: "fresh", 2: "v2", 3: "v3", 4: "v4"}[version]
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			specs := []TaskSpec{{TaskID: "a", Name: "A", Input: []byte("in-a")}}
			// The fresh case exercises the whole chain (baseline plus every
			// delta) in the first Open; the rewound cases re-open a file an
			// accepted build could have written.
			st := mustCreate(t, dir, "v5-mig", specs)
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}
			if version != 0 {
				rewindToV(t, dir, version)
			}
			st2, err := Open(dir)
			if err != nil {
				t.Fatalf("open after the %s step: %v", name, err)
			}
			defer st2.Close()
			var stamped int
			if err := st2.db.QueryRow("PRAGMA user_version").Scan(&stamped); err != nil {
				t.Fatal(err)
			}
			if stamped != schemaVersion {
				t.Fatalf("user_version = %d, want %d", stamped, schemaVersion)
			}
			if st2.runID != "v5-mig" {
				t.Fatalf("run id = %q, want v5-mig", st2.runID)
			}
			// Every v5 capability works on the stepped database.
			d := Disposition{TaskID: "a", Kind: "runtime", Generation: 1, Attempt: 1, Reason: "r"}
			if err := st2.SetDisposition(ctx, d); err != nil {
				t.Fatal(err)
			}
			if got, found, err := st2.Disposition(ctx, "a"); err != nil || !found || got != d {
				t.Fatalf("stepped disposition = %+v (found %v err %v)", got, found, err)
			}
			if rows, err := st2.Dispositions(ctx); err != nil || len(rows) != 1 {
				t.Fatalf("stepped dispositions = %v (err %v)", rows, err)
			}
			if err := st2.ClearDisposition(ctx, "a"); err != nil {
				t.Fatal(err)
			}
			if err := st2.SetTerminal(ctx, "TEST_COMPLETE", "because"); err != nil {
				t.Fatal(err)
			}
			if terminal, reason, err := st2.Terminal(ctx); err != nil ||
				terminal != "TEST_COMPLETE" || reason != "because" {
				t.Fatalf("stepped terminal = (%q,%q) err=%v", terminal, reason, err)
			}
			if err := st2.ReviseTask(ctx, "a", TaskSpec{TaskID: "a", Input: []byte("in-a-2")}); err != nil {
				t.Fatal(err)
			}
			if rows, err := st2.RevisionInputs(ctx); err != nil || len(rows) != 1 {
				t.Fatalf("stepped revision inputs = %v (err %v)", rows, err)
			}
			if err := st2.SetSession(ctx, "a", "identity-1"); err != nil {
				t.Fatal(err)
			}
			if identity, err := st2.Session(ctx, "a"); err != nil || identity != "identity-1" {
				t.Fatalf("stepped session = %q err=%v", identity, err)
			}
		})
	}
}
