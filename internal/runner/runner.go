package runner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// Executor runs one invocation of a task and returns its business result
// (execution state lives in the store, §4.2 — the two are separate).  A
// non-nil error means "no receipt": the runner stops and leaves the intent
// pending; the next Run supersedes it with a fresh invocation instead of
// trusting a result that never arrived.  The §4.3 domain workflow interface
// is deliberately not defined yet — K1 drives everything through this
// function type; later units will shape the interface from real need.
type Executor func(ctx context.Context, inv Invocation) (Result, error)

// TaskSpec declares one task at run creation: identity, display name,
// dependency edges and the input bytes the executor will receive.  Inputs
// are not persisted (§7: no payloads in the database); Run receives the
// specs again on every call, and the digest computed at registration is
// what ties an admitted result back to the exact input that produced it.
type TaskSpec struct {
	TaskID    string
	Name      string
	DependsOn []string
	Input     []byte
}

// Invocation is a registered execution intent.  The tuple
// (InvocationID, TaskID, Generation, InputDigest) is the call identity that
// admission re-verifies before accepting any result (§4.2); Input rides
// along for the executor only.
type Invocation struct {
	InvocationID string
	TaskID       string
	Generation   int64
	InputDigest  string
	Input        []byte
}

// Result is the admitted business outcome of one invocation.  K1 results
// are small and live inline in the invocations row as JSON (§7); artifacts
// and large bodies never travel through here.
type Result struct {
	Payload json.RawMessage `json:"payload"`
}

// Admission outcomes callers must be able to distinguish.
var (
	// ErrAlreadyAdmitted: this very invocation was already admitted.
	// Refusing it again changes nothing — an idempotent refusal, not a
	// crash.
	ErrAlreadyAdmitted = errors.New("runner: invocation already admitted")
	// ErrInvocationMismatch: no invocation row matches the claimed
	// identity tuple (call id, task, generation or input digest).
	ErrInvocationMismatch = errors.New("runner: invocation identity check failed")
)

// Persisted task states (§4.2).  "Ready" is computed from dependencies and
// never stored; waiting_interaction/canceled/unknown belong to later units
// and are not invented here.
const (
	taskWaitingDeps = "waiting_deps"
	taskRunning     = "running"
	taskFinished    = "finished"
)

// Persisted invocation states.  Rows are never deleted: superseded intents
// stay for audit and for recording late results.
const (
	invPending    = "pending"
	invAdmitted   = "admitted"
	invSuperseded = "superseded"
)

// eventKindAdmitted is the only K1 event kind; it is appended in the same
// transaction as the admission it describes.
const eventKindAdmitted = "invocation_admitted"

// crashHooks exists solely for crash_test.go: it lets the test process
// os.Exit at the four §13.2 injection points (after intent registration,
// after the result is obtained, before the admission commit, after it).
// Every field is nil outside those tests.
type crashHooks struct {
	afterRegisterIntent func()
	afterResult         func()
	beforeAdmitCommit   func()
	afterAdmit          func()
}

// taskRow is a task as loaded from the store.
type taskRow struct {
	taskID     string
	generation int64
}

// admittedEvent is the payload of the event written with each admission:
// enough to see which call settled with which result without re-reading
// the invocation row.
type admittedEvent struct {
	InvocationID string `json:"invocation_id"`
	TaskID       string `json:"task_id"`
	Generation   int64  `json:"generation"`
	InputDigest  string `json:"input_digest"`
	Result       Result `json:"result"`
}

// Runner drives one owner's loop over the store's run.
type Runner struct {
	store *Store
	exec  Executor
	hooks crashHooks
}

// NewRunner wires a runner to its store and executor.
func NewRunner(store *Store, exec Executor) *Runner {
	return &Runner{store: store, exec: exec}
}

// Run drives the single-owner loop (§4.1) until no work remains: compute
// the next ready task from persisted facts, register the intent in a short
// transaction, execute outside any transaction, then admit the result in a
// short transaction that also appends the event.  Exactly one ready task
// runs at a time — parallelism is a later unit.  specs must cover the
// run's tasks (they carry the non-persisted inputs); a missing spec for
// ready work fails the run instead of executing blind.
func (r *Runner) Run(ctx context.Context, specs []TaskSpec) error {
	if r.store.runID == "" {
		return errors.New("runner: no run created in this directory")
	}
	byTask := make(map[string]TaskSpec, len(specs))
	for _, sp := range specs {
		byTask[sp.TaskID] = sp
	}
	if err := r.recoverPending(ctx); err != nil {
		return err
	}
	for {
		t, err := r.nextReady(ctx)
		if err != nil {
			return err
		}
		if t == nil {
			return r.checkAllFinished(ctx)
		}
		sp, ok := byTask[t.taskID]
		if !ok {
			return fmt.Errorf("runner: no spec supplied for ready task %q", t.taskID)
		}
		inv, err := r.registerIntent(ctx, t, sp.Input)
		if err != nil {
			return err
		}
		if r.hooks.afterRegisterIntent != nil {
			r.hooks.afterRegisterIntent() // §13.2 point ①
		}
		res, execErr := r.exec(ctx, inv)
		if execErr != nil {
			// No receipt: the intent stays pending so its work is
			// never silently dropped; the next Run supersedes it.
			return fmt.Errorf("runner: task %s invocation %s ended without receipt: %w",
				t.taskID, inv.InvocationID, execErr)
		}
		if r.hooks.afterResult != nil {
			r.hooks.afterResult() // §13.2 point ②
		}
		if err := r.admit(ctx, inv, res); err != nil {
			return err
		}
	}
}

// recoverPending closes out unsettled intents from a previous owner
// process: every still-pending invocation has no receipt (the owner died
// or the executor failed), so it is superseded — kept, never deleted — and
// the next registration for the same task replaces it.  Recovery touches
// only current tasks and unsettled invocations; it never replays the event
// history (§7).
func (r *Runner) recoverPending(ctx context.Context) error {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit
	if _, err := tx.ExecContext(ctx,
		"UPDATE invocations SET state=? WHERE run_id=? AND state=?",
		invSuperseded, r.store.runID, invPending); err != nil {
		return err
	}
	// A task stuck in running belongs to an unsettled intent; back to
	// waiting_deps so readiness is computed fresh from dependencies.
	if _, err := tx.ExecContext(ctx,
		"UPDATE tasks SET state=? WHERE run_id=? AND state=?",
		taskWaitingDeps, r.store.runID, taskRunning); err != nil {
		return err
	}
	return tx.Commit()
}

// nextReady returns the lowest-task_id task whose dependencies are all
// finished, or nil when nothing is ready.  Readiness is computed from
// persisted facts (§4.1), never stored; the deterministic order keeps the
// sequential kernel's behaviour reproducible.
func (r *Runner) nextReady(ctx context.Context) (*taskRow, error) {
	row := r.store.db.QueryRowContext(ctx, `
		SELECT t.task_id, t.generation
		  FROM tasks t
		 WHERE t.run_id = ? AND t.state <> ?
		   AND NOT EXISTS (
		       SELECT 1
		         FROM task_deps d
		         JOIN tasks dep ON dep.run_id = d.run_id AND dep.task_id = d.depends_on
		        WHERE d.run_id = t.run_id AND d.task_id = t.task_id AND dep.state <> ?
		   )
		 ORDER BY t.task_id
		 LIMIT 1`, r.store.runID, taskFinished, taskFinished)
	var t taskRow
	err := row.Scan(&t.taskID, &t.generation)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// checkAllFinished guards the loop's exit: no task ready but some task
// unfinished (for example a dependency cycle declared at creation) must
// not be reported as success.
func (r *Runner) checkAllFinished(ctx context.Context) error {
	var n int
	if err := r.store.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tasks WHERE run_id=? AND state<>?",
		r.store.runID, taskFinished).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("runner: %d task(s) unfinished but none ready (dependency cycle?)", n)
	}
	return nil
}

// registerIntent records, in one short transaction, that this owner is
// about to execute the task: a fresh pending invocation row carrying the
// identity tuple (task, generation, input digest) plus the task's move to
// running.  If the process dies here, recovery finds the pending row with
// no receipt and supersedes it.
func (r *Runner) registerIntent(ctx context.Context, t *taskRow, input []byte) (Invocation, error) {
	id, err := newInvocationID()
	if err != nil {
		return Invocation{}, err
	}
	sum := sha256.Sum256(input)
	inv := Invocation{
		InvocationID: id,
		TaskID:       t.taskID,
		Generation:   t.generation,
		InputDigest:  hex.EncodeToString(sum[:]),
		Input:        input,
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return Invocation{}, err
	}
	defer tx.Rollback() // no-op after Commit
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO invocations
			(invocation_id, run_id, task_id, generation, input_digest, registered_at, state)
		VALUES (?,?,?,?,?,?,?)`,
		inv.InvocationID, r.store.runID, inv.TaskID, inv.Generation,
		inv.InputDigest, nowUTC(), invPending); err != nil {
		return Invocation{}, err
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE tasks SET state=? WHERE run_id=? AND task_id=?",
		taskRunning, r.store.runID, t.taskID); err != nil {
		return Invocation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Invocation{}, err
	}
	return inv, nil
}

// admit settles one invocation in a single short transaction (§4.1, §4.2).
// The identity tuple — call id, task, generation and input digest — is
// re-verified inside the transaction before anything is written:
//
//   - pending    → admit: mark the invocation admitted with its result,
//     finish the task and point its effective result at this invocation,
//     and append the run event in the same transaction;
//   - admitted   → ErrAlreadyAdmitted (idempotent refusal; nothing
//     changes);
//   - superseded → the result is late: record it on the row, never touch
//     the newer invocation's admitted effective result.
func (r *Runner) admit(ctx context.Context, inv Invocation, res Result) error {
	resultJSON, err := json.Marshal(res)
	if err != nil {
		return fmt.Errorf("runner: marshal result for invocation %s: %w", inv.InvocationID, err)
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit
	var state string
	err = tx.QueryRowContext(ctx, `
		SELECT state FROM invocations
		 WHERE invocation_id=? AND run_id=? AND task_id=? AND generation=? AND input_digest=?`,
		inv.InvocationID, r.store.runID, inv.TaskID, inv.Generation, inv.InputDigest).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvocationMismatch
	}
	if err != nil {
		return err
	}
	switch state {
	case invAdmitted:
		return ErrAlreadyAdmitted
	case invSuperseded:
		if _, err := tx.ExecContext(ctx,
			"UPDATE invocations SET result_json=? WHERE invocation_id=? AND state=?",
			resultJSON, inv.InvocationID, invSuperseded); err != nil {
			return err
		}
		return tx.Commit()
	default: // invPending
		eventJSON, err := json.Marshal(admittedEvent{
			InvocationID: inv.InvocationID,
			TaskID:       inv.TaskID,
			Generation:   inv.Generation,
			InputDigest:  inv.InputDigest,
			Result:       res,
		})
		if err != nil {
			return fmt.Errorf("runner: marshal admission event for invocation %s: %w", inv.InvocationID, err)
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE invocations SET state=?, result_json=? WHERE invocation_id=?",
			invAdmitted, resultJSON, inv.InvocationID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE tasks SET state=?, result_invocation_id=? WHERE run_id=? AND task_id=?",
			taskFinished, inv.InvocationID, r.store.runID, inv.TaskID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO events (run_id, kind, payload_json) VALUES (?,?,?)",
			r.store.runID, eventKindAdmitted, eventJSON); err != nil {
			return err
		}
		if r.hooks.beforeAdmitCommit != nil {
			r.hooks.beforeAdmitCommit() // §13.2 point ③
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if r.hooks.afterAdmit != nil {
			r.hooks.afterAdmit() // §13.2 point ④
		}
		return nil
	}
}

// newInvocationID mints a random 128-bit id.  Random ids keep invocation
// identity meaningful across owner processes without any counter state to
// maintain or recover.
func newInvocationID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
