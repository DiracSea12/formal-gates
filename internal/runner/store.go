// Package runner is the Go engine's single-owner kernel (plan §2.1, §4.1):
// exactly one process owns one run directory and its SQLite state, computes
// ready work from persisted facts, registers execution intents in short
// transactions, executes outside any transaction, and admits results in
// another short transaction that also appends the run event.  This file owns
// directory ownership and durable state; runner.go owns the loop.
//
// K1 added the single owner, task transactions, sequential execution and
// result admission; K2 adds process confirmations (decisions.go); K3 adds
// bounded parallel dispatch in the run loop (runner.go); K4 adds local
// generations and revision scoping through Store.ReviseTask
// (revision.go), and the K4 rework builds the unified request model over
// it — user_requests (Store.RequestRevision's repair-allowance asks and
// recovery's restart asks, settled by one Store.Answer) plus the
// persisted call_observations that keep takeover facts alive across
// owners (takeover.go).  Native
// interactions arrive in later K units and are deliberately absent.
package runner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go, no CGo)
)

// ErrRunBusy is returned by Open when the directory is already owned.  The
// underlying flock is released by the kernel when the owning process dies,
// so unlike the pid/token heartbeat lock in internal/coordination there is
// no staleness to interpret and no heartbeat to miss.
var ErrRunBusy = errors.New("runner: run directory already owned by another owner")

// ErrAdvancing is returned by Run when another Run on the same Store is
// mid-advance.  Ownership has two layers: the directory flock stops other
// processes, Store.advance stops this one — so a duplicate advance is a
// caller mistake, refused here rather than silently absorbed (a nil
// return would read as "the run advanced").  Blocking instead would
// deadlock callers that release the in-flight work only after this call
// returns; the holder keeps the lock for its whole Run, and execution
// inside the lock is the advance boundary.
var ErrAdvancing = errors.New("runner: another Run is advancing this store")

// A run directory holds exactly these two files.  The lock file is created
// but never removed: deleting it would let a second opener flock a fresh
// inode while the first owner still holds the old one.
const (
	dbFileName   = "run.db"
	lockFileName = "run.lock"
)

// schemaVersion is the schema this build writes.  PRAGMA user_version
// carries it inside the database file; migration is forward-only, so a
// build that finds a newer version refuses the file instead of guessing
// how to read it.  Version 2 added the task-definition input binding
// (tasks.input_digest); version 3 added the decisions gate table with
// its allow column; version 4 added the user_requests and
// call_observations tables of the unified request model (revision.go,
// takeover.go); version 5 adds task_revisions (the revision input
// bytes, written with the generation bump, revision.go),
// task_sessions (the execution identities a granted restart clears,
// decisions.go), task_dispositions (a parked stage's standing ask
// fact, disposition.go) and the runs terminal columns (the run's
// terminal status and stop reason, below).  Every step is additive, so
// every accepted baseline — v2, v3 and v4 — migrates forward for real,
// never by refusal; the reshape of the never-accepted v4 delta happened
// while this build's version was still 4 and left no v4-stamped files
// behind, and the v5 delta was likewise reshaped in place (dispositions
// and the terminal columns joined it) while v5 was still unaccepted —
// which is why that delta's newest pieces are written additively
// idempotent, so any development-stage v5 shape steps forward cleanly.
// Version 1 databases are refused outright below.
const schemaVersion = 5

// Store is the durable state of one run plus exclusive ownership of its
// directory.  Open acquires ownership, Close releases it; between the two,
// this process is the run's single owner.
type Store struct {
	db    *sql.DB
	lock  *dirLock
	runID string // empty until CreateRun ran in this directory

	// advance serializes run advances within this process for every
	// Runner sharing this Store.  The directory flock only stops other
	// processes; without this mutex a second Runner (or a re-entrant
	// Run) on the same Store would supersede the first's in-flight
	// invocation mid-loop and re-execute its task.  It must live on the
	// Store, not the Runner, precisely because different Runner
	// instances can share one Store.  The holder keeps it for the whole
	// Run — execution happens inside the lock, so the lock is the
	// advance boundary.
	advance sync.Mutex
}

// Open takes exclusive ownership of dir (created if missing), opens run.db
// and brings its schema to this build's version.  A second Open on the same
// directory — from any process, including this one — fails with an error
// wrapping ErrRunBusy.
func Open(dir string) (*Store, error) {
	// Resolve once at the entry so the lock file and the database URI
	// agree on one absolute location: a relative dir would otherwise
	// encode its first segment into the URI's authority slot
	// ("file://child/run.db") and break the database open.  Abs also
	// makes every spelling of the same directory ("child", "./child")
	// map to one lock target.
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	lock, err := lockDir(filepath.Join(abs, lockFileName))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrRunBusy, abs, err)
	}
	db, err := sql.Open("sqlite", dsn(filepath.Join(abs, dbFileName)))
	if err != nil {
		lock.unlock()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		lock.unlock()
		return nil, fmt.Errorf("runner: migrate %s: %w", abs, err)
	}
	runID, err := readRunID(db)
	if err != nil {
		db.Close()
		lock.unlock()
		return nil, err
	}
	return &Store{db: db, lock: lock, runID: runID}, nil
}

// Close releases the database and, with it, directory ownership — but only
// when no Run is mid-advance on this Store.  It joins the same advance
// guard Run uses: a successful TryLock means no executor is in flight, so
// the database is closed first, the directory lock released second, and
// the mutex released last (held in between so no Run can start on a
// half-closed Store).  When an advance is in flight Close refuses with
// ErrAdvancing and leaves the database and the directory lock untouched —
// blocking instead would deadlock an executor that calls Close itself.
func (s *Store) Close() error {
	if !s.advance.TryLock() {
		return ErrAdvancing
	}
	defer s.advance.Unlock()
	err := s.db.Close()
	s.lock.unlock()
	return err
}

// CreateRun persists the run definition in one transaction: the runs row,
// one tasks row per spec (all starting waiting_deps at generation 1 —
// Store.ReviseTask is the only writer that ever bumps a generation,
// revision.go), the dependency edges, and every declared confirmation
// gate as a pending decisions row plus its decision_created event
// (materializeDecisions) — the gate exists exactly when the run does, so
// the requirement survives owners that never even ran once.  Malformed
// gate declarations are refused before any write.  A directory holds
// exactly one run; a second CreateRun is refused.
func (s *Store) CreateRun(ctx context.Context, runID string, specs []TaskSpec) error {
	if s.runID != "" {
		return fmt.Errorf("runner: directory already holds run %q", s.runID)
	}
	// Refuse before the write transaction: an empty id would insert a
	// runs row the in-memory sentinel cannot represent, silently
	// poisoning the directory for every later corrected attempt.
	if runID == "" {
		return errors.New("runner: run id must not be empty")
	}
	// Same before-the-write rule for the gate declarations: a malformed
	// confirmation (no options, no allowing choice, allowing choice not
	// offered) would persist as a question that can never be answered
	// or never authorize anything, with no answer-path repair — refuse
	// it here so the directory stays empty for the corrected attempt.
	for _, sp := range specs {
		if err := validateConfirmation(sp); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO runs (run_id, schema_version, created_at) VALUES (?,?,?)",
		runID, schemaVersion, nowUTC()); err != nil {
		return err
	}
	for _, sp := range specs {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO tasks (run_id, task_id, name, input_digest, state, generation) VALUES (?,?,?,?,?,1)",
			runID, sp.TaskID, sp.Name, inputDigest(sp.Input), taskWaitingDeps); err != nil {
			return err
		}
	}
	for _, sp := range specs {
		for _, dep := range sp.DependsOn {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO task_deps (run_id, task_id, depends_on) VALUES (?,?,?)",
				runID, sp.TaskID, dep); err != nil {
				return err
			}
		}
	}
	if err := materializeDecisions(ctx, tx, runID, specs); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.runID = runID
	return nil
}

// dsn mirrors the repository's established SQLite DSN (the go-engine
// sqlprobe): WAL for crash-safe commits, foreign keys enforced per
// connection, a busy timeout so WAL checkpoints never fail fast, and
// immediate write transactions so a short writer never upgrades
// mid-transaction into SQLITE_BUSY.  The path travels through url.URL so
// URI-significant characters in the directory name are percent-encoded
// ('#' would otherwise start a URI fragment and make two distinct run
// directories silently share one database).  The query string is a fixed
// constant with no user input and is appended verbatim: url.Values.Encode
// would escape the parentheses of the _pragma=... options and change how
// modernc parses them.
func dsn(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	return u.String() +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)" +
		"&_txlock=immediate"
}

// schemaV2 is the accepted K1 baseline table set (plan §7), kept verbatim
// because both the fresh-file and the v2→v3 migration paths build on it.
// The v3 delta (decisions) is decisionsTable below; native_interactions
// belongs to a later K unit.  Columns follow real read paths — nothing
// speculative.
var schemaV2 = []string{
	// One row per run.  schema_version records the version the run was
	// created under; user_version is the file-level migration marker.
	`CREATE TABLE runs (
		run_id         TEXT PRIMARY KEY,
		schema_version INTEGER NOT NULL,
		created_at     TEXT NOT NULL
	)`,
	// One row per task.  input_digest binds the task DEFINITION to the
	// input declared at CreateRun — Run refuses any spec whose digest
	// differs, so a restarted owner cannot settle old work with new
	// inputs (a changed input reaches execution only through
	// Store.ReviseTask's new generation, revision.go).
	// invocations.input_digest plays a different, per-call role below: it
	// records what one specific call ran with and is part of the
	// admission identity.  state is execution state (§4.2); "ready" is
	// computed from dependencies and never stored.  generation is the
	// task's local revision counter, bumped only by Store.ReviseTask.
	// result_invocation_id points at the one admitted invocation whose
	// result is effective — a late result never moves it.
	`CREATE TABLE tasks (
		run_id               TEXT NOT NULL REFERENCES runs (run_id),
		task_id              TEXT NOT NULL,
		name                 TEXT NOT NULL,
		input_digest         TEXT NOT NULL,
		state                TEXT NOT NULL,
		generation           INTEGER NOT NULL DEFAULT 1,
		result_invocation_id TEXT REFERENCES invocations (invocation_id),
		PRIMARY KEY (run_id, task_id)
	)`,
	// Dependency edges, unique per (task, dependency) by the primary
	// key.  Both ends reference tasks, so a dangling edge cannot be
	// persisted.
	`CREATE TABLE task_deps (
		run_id     TEXT NOT NULL,
		task_id    TEXT NOT NULL,
		depends_on TEXT NOT NULL,
		PRIMARY KEY (run_id, task_id, depends_on),
		FOREIGN KEY (run_id, task_id)    REFERENCES tasks (run_id, task_id),
		FOREIGN KEY (run_id, depends_on) REFERENCES tasks (run_id, task_id)
	)`,
	// The call record (§4.2: original invocation records are never
	// overwritten).  input_digest here is the CALL-level digest — what
	// this specific invocation ran with, re-verified at admission as
	// part of the identity tuple — as opposed to the task-definition
	// binding in tasks.input_digest above.  Only state and result_json
	// ever change, each in one forward transition: pending→admitted,
	// pending→superseded, and result_json filled once when the result
	// arrives — even late.
	`CREATE TABLE invocations (
		invocation_id TEXT PRIMARY KEY,
		run_id        TEXT NOT NULL,
		task_id       TEXT NOT NULL,
		generation    INTEGER NOT NULL,
		input_digest  TEXT NOT NULL,
		registered_at TEXT NOT NULL,
		state         TEXT NOT NULL,
		result_json   TEXT,
		FOREIGN KEY (run_id, task_id) REFERENCES tasks (run_id, task_id)
	)`,
	// Recovery scans a run's unsettled (pending) invocations; identity
	// checks go through the primary key.
	`CREATE INDEX idx_invocations_run_state ON invocations (run_id, state)`,
	// The append-only run journal.  Admission events land here in the
	// same transaction as the admission itself (§4.1).  seq is the
	// autoassigned primary key — never reused, because events are never
	// deleted or rewritten.
	`CREATE TABLE events (
		seq          INTEGER PRIMARY KEY,
		run_id       TEXT NOT NULL REFERENCES runs (run_id),
		kind         TEXT NOT NULL,
		payload_json TEXT NOT NULL
	)`,
}

// decisionsTable is the entire v3 delta: the process-confirmation gate of
// plan §6.1/§7 — one row per (run, task, generation), created pending by
// CreateRun's materialization (decisions.go) and consumed once by
// Store.Answer.  revision/body_digest/options_json/allow freeze the
// question at creation: the row, not a later Run's Confirm fields, defines
// the gate.  allow is the choice that authorizes execution and is NOT
// NULL: an answered row always carries a comparable verdict, and a gate
// without one could never open.  answer and answered_at stay NULL until
// the single pending→answered transition, in the style of
// invocations.result_json.  The allow column is part of the v3 delta
// itself, not a v4 step (see schemaVersion); databases stamped v3 by
// the unaccepted build are development-only and out of scope.
const decisionsTable = `CREATE TABLE decisions (
	decision_id  TEXT PRIMARY KEY,
	run_id       TEXT NOT NULL REFERENCES runs (run_id),
	task_id      TEXT NOT NULL,
	generation   INTEGER NOT NULL,
	revision     TEXT NOT NULL,
	body         TEXT NOT NULL,
	body_digest  TEXT NOT NULL,
	options_json TEXT NOT NULL,
	allow        TEXT NOT NULL,
	created_at   TEXT NOT NULL,
	state        TEXT NOT NULL CHECK (state IN ('pending','answered')),
	answer       TEXT,
	answered_at  TEXT,
	UNIQUE (run_id, task_id, generation)
)`

// userRequestsTable is the reshaped v4 delta: one row per user request,
// both kinds sharing ONE lifecycle — this comment is its single
// complete and authoritative statement; every other doc block that
// touches requests (RequestRevision, Answer, the takeover seams)
// describes only its own step and points here:
//
//   - a request is born pending — a revision ask when the repair
//     allowance is reached (Store.RequestRevision), a restart ask when a
//     takeover consultation reports a dead or invalidated session
//     (takeover.go); direct approval below the allowance is born executed
//     with no answer and no question ever asked;
//   - pending → granted (Answer chose the row's allow) or refused (any
//     other offered choice), exactly once, in Store.Answer's transaction
//     together with answer, answered_at and the request_answered event;
//   - granted → executed by the completing transition: the revision's
//     completion RequestRevision call (revision and transition commit
//     together), or recovery voiding the invalidated invocation a
//     granted restart asked about (the approved replacement);
//   - refused is terminal; an already-answered request replays its
//     original receipt on the same choice and refuses a different one as
//     a conflict, exactly like an answered decisions row;
//   - binding validity = the task exists and tasks.generation ==
//     target_generation (the REAL generation at creation, never a
//     synthetic slot), plus — while a restart is unresolved — its
//     invocation_id still pending.  An upstream revision that moves the
//     task into a new generation invalidates the request by itself; no
//     closure ever voids requests explicitly.  A pending or granted row
//     with a whole binding is ACTIVE — the one rule every reader
//     composes through requestActiveSQL (revision.go): an invalid
//     binding is stale for every purpose, answering it, listing it,
//     counting it or acting on it, so dead history never blocks,
//     authorizes or outlives its scope.
//
// target_generation/invocation_id/initial are the persisted identity the
// recovery argument cannot be allowed to drift from: initial binds the
// repair allowance at first use (revision rows only; RequestRevision
// refuses a different value), invocation_id names the dead call a
// restart ask is about.  options_json/allow/revision/body freeze the
// question on the row, in the style of the decisions gate.  All counts
// the policy needs are derived from these rows, never stored as
// counters.  task_id carries no foreign key on purpose: existence is
// checked before any write, and a request must outlive binding checks
// against tasks.generation.
const userRequestsTable = `CREATE TABLE user_requests (
	request_id       TEXT PRIMARY KEY,
	run_id           TEXT NOT NULL REFERENCES runs (run_id),
	task_id          TEXT NOT NULL,
	kind             TEXT NOT NULL CHECK (kind IN ('revision','restart')),
	target_generation INTEGER NOT NULL,
	invocation_id    TEXT,
	options_json     TEXT NOT NULL,
	allow            TEXT NOT NULL,
	revision         TEXT NOT NULL,
	body             TEXT NOT NULL,
	initial          INTEGER,
	state            TEXT NOT NULL CHECK (state IN ('pending','granted','refused','executed')),
	answer           TEXT,
	answered_at      TEXT,
	created_at       TEXT NOT NULL,
	resolved_at      TEXT
)`

// callObservationsTable is the v4 delta's persisted observation fact:
// one row per invocation a takeover consultation judged still working
// under its original supervisor (takeover.go).  The row is idempotent
// (INSERT OR IGNORE) and never deleted: it outlives the invocation's
// own settlement, and it is read only while that invocation is still
// pending — an owner without a Takeover consult must not blindly
// supersede a call a previous owner observed in flight, and the run
// reports ErrTakeoverRequired instead of retrying it.
const callObservationsTable = `CREATE TABLE call_observations (
	invocation_id TEXT PRIMARY KEY REFERENCES invocations (invocation_id),
	run_id        TEXT NOT NULL,
	task_id       TEXT NOT NULL,
	observed_at   TEXT NOT NULL
)`

// taskRevisionsTable is half of the v5 delta: one row per accepted
// task revision, written in the SAME transaction as the generation bump
// it rode (revision.go).  tasks.input_digest binds a task definition to
// its input; the bytes themselves live here so a reopened owner can
// re-send exactly the input the current generation is bound to — input
// bytes are no longer external to the kernel for revisions.  Two
// deliberate shape rules, both load-bearing for readers
// (Store.RevisionInputs):
//
//   - only the revision's TARGET writes a row: dependents bumped by the
//     invalidation closure keep their inputs and write none, so a row
//     also answers "was this task itself ever revised";
//   - creation inputs never appear: they are re-derived from the run's
//     immutable creation binding, exactly as before v5.
const taskRevisionsTable = `CREATE TABLE task_revisions (
	run_id     TEXT NOT NULL,
	task_id    TEXT NOT NULL,
	generation INTEGER NOT NULL,
	input      BLOB NOT NULL,
	PRIMARY KEY (run_id, task_id, generation),
	FOREIGN KEY (run_id, task_id) REFERENCES tasks (run_id, task_id)
)`

// taskSessionsTable is the other half of the v5 delta: the execution
// identity a task's next native call resumes — absent means a fresh
// session.  One row per task, remembered when a worker turn settles
// (Store.SetSession, called by the executor mid-Run, so these writes
// take no advance lock) and cleared by a granted restart answer in the
// SAME transaction as the grant (Store.answerRequest) — the single
// authority for "which session this task resumes next", formerly a
// JSON file the composition projected the kernel's restart grants
// onto.
const taskSessionsTable = `CREATE TABLE task_sessions (
	run_id   TEXT NOT NULL,
	task_id  TEXT NOT NULL,
	identity TEXT NOT NULL,
	PRIMARY KEY (run_id, task_id),
	FOREIGN KEY (run_id, task_id) REFERENCES tasks (run_id, task_id)
)`

// taskDispositionsTable is the v5 delta's control-state piece: one row
// per task, the LATEST park fact standing for it (an upsert replaces a
// consumed or superseded park) — the single authority for "this stage
// is parked, and why", which decides the flow's next move between runs.
// The row's (task, generation, attempt, kind) is exactly the identity
// the placed question freezes into its body (the disposition id,
// disposition.go), so row and question share one id with no second
// record anywhere.  Written by the executors mid-Run (Store.SetDisposition,
// no advance lock — the park precedes any ask), read by the transitions
// (Store.Dispositions/Disposition), and cleared when the ask's answer
// has been applied (Store.ClearDisposition).  Formerly a JSON file per
// task under the run directory's dispositions tree.
const taskDispositionsTable = `CREATE TABLE IF NOT EXISTS task_dispositions (
	run_id     TEXT NOT NULL,
	task_id    TEXT NOT NULL,
	kind       TEXT NOT NULL,
	generation INTEGER NOT NULL,
	attempt    INTEGER NOT NULL,
	reason     TEXT NOT NULL,
	evidence   TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (run_id, task_id),
	FOREIGN KEY (run_id, task_id) REFERENCES tasks (run_id, task_id)
)`

// runsTerminalColumns is the v5 delta's run-level piece: the terminal
// status the flow itself reached and why it stopped there, on the runs
// row (terminal empty = the run never reached a terminal).  Written by
// the seal and refusal paths through Store.SetTerminal, read by
// Store.Terminal (the archive's settlement judgement and the terminal
// judgment's record half).  ADD COLUMN has no IF NOT EXISTS form, so
// migrate adds each column only when absent — the same in-place-reshape
// idempotence taskDispositionsTable carries.
var runsTerminalColumns = [][2]string{
	{"terminal", "TEXT"},
	{"stop_reason", "TEXT"},
}

// migrate applies forward-only schema versioning via PRAGMA user_version:
// 0 means a fresh file (create the baseline plus every delta and stamp it),
// 2, 3 and 4 are accepted baselines (take the additive steps up to this
// build's version — real migrations, never refusals), the current version
// means nothing to do, version 1 is refused outright, and anything larger
// means a newer runner wrote this database and must not be touched by this
// build.
func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version == schemaVersion {
		return nil
	}
	switch version {
	case 0, 2, 3, 4:
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback() // no-op after Commit
		if version == 0 {
			// A fresh file needs the whole baseline, not just the deltas.
			for _, stmt := range schemaV2 {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
		}
		if version < 3 {
			if _, err := tx.Exec(decisionsTable); err != nil {
				return err
			}
		}
		if version < 4 {
			if _, err := tx.Exec(userRequestsTable); err != nil {
				return err
			}
			if _, err := tx.Exec(callObservationsTable); err != nil {
				return err
			}
		}
		if version < 5 {
			if _, err := tx.Exec(taskRevisionsTable); err != nil {
				return err
			}
			if _, err := tx.Exec(taskSessionsTable); err != nil {
				return err
			}
			if _, err := tx.Exec(taskDispositionsTable); err != nil {
				return err
			}
			for _, column := range runsTerminalColumns {
				has, err := txHasColumn(tx, "runs", column[0])
				if err != nil {
					return err
				}
				if has {
					continue // an in-place v5 reshape already carried it
				}
				if _, err := tx.Exec(fmt.Sprintf(
					"ALTER TABLE runs ADD COLUMN %s %s", column[0], column[1])); err != nil {
					return err
				}
			}
		}
		// user_version is a transactional header write: a crash
		// mid-migration leaves the file at its previous version again.
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
			return err
		}
		return tx.Commit()
	case 1:
		// Version 1 predates the task input binding.  K1 never
		// shipped, so v1 files exist only in development, and the
		// missing digests cannot be backfilled from anything the
		// file knows — the honest move is to refuse and recreate.
		return errors.New("schema version 1 predates task input binding; K1 development schema — recreate the run")
	default:
		return fmt.Errorf("schema version %d is newer than supported version %d", version, schemaVersion)
	}
}

// txHasColumn reports whether table already carries column, inside an
// open migration transaction.  table comes only from this package's
// constants.  ADD COLUMN has no IF NOT EXISTS spelling, so the v5
// terminal columns go through this guard — an unaccepted-delta reshape
// may already have left them on the file.
func txHasColumn(tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, ctype string
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// ---- the run's terminal fact (the v5 delta's runs columns) -------------------
//
// What the flow itself concluded — the terminal status it reached and why
// it stopped there.  Two operations, one owner per transition: the seal
// and refusal paths write both columns at once (Store.SetTerminal, one
// short transaction; the reason is diagnostic, so the seal paths record
// none), and the archive's settlement judgement plus the terminal
// judgment's record half read them back (Store.Terminal) — never from a
// file a crash could strand between the judgment and its record.

// SetTerminal records the run's terminal status and the reason it stopped
// there, in one short transaction on the runs row.  An empty stopReason
// records none (the seal completions have no stop reason — only the
// user-refusal paths do).  Deliberately no advance lock: the judgment
// that produces a terminal happens between runs or at drain, never
// mid-advance.
func (s *Store) SetTerminal(ctx context.Context, terminal, stopReason string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE runs SET terminal=?, stop_reason=? WHERE run_id=?",
		terminal, stopReason, s.runID)
	return err
}

// Terminal reads the run's terminal status and stop reason; an empty
// terminal means the run never reached one.
func (s *Store) Terminal(ctx context.Context) (terminal, stopReason string, err error) {
	var terminalV, reasonV sql.NullString
	err = s.db.QueryRowContext(ctx,
		"SELECT terminal, stop_reason FROM runs WHERE run_id=?",
		s.runID).Scan(&terminalV, &reasonV)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return terminalV.String, reasonV.String, err
}

// readRunID returns the single run this directory holds, or "" for a fresh
// database.  A directory holds exactly one run by construction (CreateRun
// refuses a second), so more than one row is corruption we refuse to guess
// about.
func readRunID(db *sql.DB) (string, error) {
	rows, err := db.Query("SELECT run_id FROM runs")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var runID string
	if !rows.Next() {
		return "", nil
	}
	if err := rows.Scan(&runID); err != nil {
		return "", err
	}
	if rows.Next() {
		return "", errors.New("runner: database holds more than one run")
	}
	return runID, rows.Err()
}

// nowUTC is the timestamp format used for every persisted instant.
func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
