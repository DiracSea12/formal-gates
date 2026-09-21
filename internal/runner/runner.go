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
// are not persisted (§7: no payloads in the database); their sha256 is —
// CreateRun binds it on the task row and every Run verifies its specs
// against that binding before touching anything, so a restarted owner
// cannot drift from the created run.
type TaskSpec struct {
	TaskID    string
	Name      string
	DependsOn []string
	Input     []byte
	// Confirm declares this task gated behind a persisted user decision
	// (§6.1).  It is a creation-time declaration, persisted with the run:
	// CreateRun validates it and materializes the pending decision row
	// and its decision_created event in the run's creation transaction.
	// Run never reads this field — the gate and its answer are consulted
	// on the decisions rows alone — so recovery specs may omit it and
	// still meet exactly the same gate: the persisted definition is
	// authoritative, and no after-the-fact diffing against it is done.
	// See decisions.go.
	Confirm *Confirmation
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
// run's tasks with the inputs the run was created under; a changed input
// or a missing spec for ready work fails the run instead of executing
// blind.  A task whose persisted decision gate is still pending stops Run
// with ErrWaitingDecision — its work waits for Store.Answer, and no
// successor runs without that user decision; a gate answered with anything
// but its allowing choice stops Run with ErrDecisionRefused, just as
// durably.  Gates are read from the decisions rows alone; the specs'
// Confirm fields are ignored here.
func (r *Runner) Run(ctx context.Context, specs []TaskSpec) error {
	if r.store.runID == "" {
		return errors.New("runner: no run created in this directory")
	}
	// Take the Store's advance boundary.  The directory flock stops
	// other processes; this stops another Run sharing this Store from
	// superseding an in-flight invocation mid-loop.  A duplicate
	// advance is a caller mistake — refuse it (see ErrAdvancing)
	// rather than silently absorb a nil "success", and never block:
	// waiting would deadlock callers that release the in-flight work
	// only after this call returns.
	if !r.store.advance.TryLock() {
		return ErrAdvancing
	}
	defer r.store.advance.Unlock()
	// Spec verification precedes every recovery write and all
	// execution: a restarted owner must not settle old work with new
	// inputs, and a refused run must not disturb unsettled intents
	// either.
	if err := r.verifySpecs(ctx, specs); err != nil {
		return err
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
		// Every ready task consults its persisted decision gate before
		// any intent is registered, and the gate is READ, never built
		// here: CreateRun materialized it, so a run exists with all its
		// gates or not at all.  A pending row stops Run with
		// ErrWaitingDecision immediately (this sits ahead of the
		// no-ready-task exit, so a waiting gate can never be misreported
		// as a cycle); an answered row either clears the gate — the
		// stored answer equals the row's allowing choice, execution
		// proceeds exactly as ungated work and an executor failure plus
		// retry never re-asks — or refuses it durably
		// (ErrDecisionRefused).  A task with no row was never declared
		// gated and runs.  The specs' Confirm fields are deliberately
		// not consulted: recovery that omits them meets the same gate
		// (§13.2 — no successor executes without a user decision).
		if err := r.store.decisionGate(ctx, t); err != nil {
			return err
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

// verifySpecs checks the caller's specs against the persisted task
// definitions: every spec must name an existing task, and its input digest
// must equal the one bound at CreateRun.  K1 refuses changed inputs
// outright — re-evaluation with a new generation is a later unit's rule.
func (r *Runner) verifySpecs(ctx context.Context, specs []TaskSpec) error {
	for _, sp := range specs {
		var bound string
		err := r.store.db.QueryRowContext(ctx,
			"SELECT input_digest FROM tasks WHERE run_id=? AND task_id=?",
			r.store.runID, sp.TaskID).Scan(&bound)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("runner: spec names task %q which is not part of run %q", sp.TaskID, r.store.runID)
		}
		if err != nil {
			return err
		}
		if got := inputDigest(sp.Input); got != bound {
			return fmt.Errorf("runner: input for task %q differs from the run's definition; K1 refuses input changes (re-evaluation arrives in a later unit)", sp.TaskID)
		}
	}
	return nil
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
	id, err := newID128()
	if err != nil {
		return Invocation{}, err
	}
	inv := Invocation{
		InvocationID: id,
		TaskID:       t.taskID,
		Generation:   t.generation,
		InputDigest:  inputDigest(input),
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

// inputDigest is the sha256 of a byte input, hex encoded.  One function
// serves every digest binding in the schema: tasks.input_digest pins the
// task DEFINITION to what CreateRun declared, invocations.input_digest
// records what one call actually ran with (part of the admission
// identity), and decisions.body_digest pins the confirmation body the
// question was asked with.
func inputDigest(input []byte) string {
	sum := sha256.Sum256(input)
	return hex.EncodeToString(sum[:])
}

// newID128 mints a random 128-bit hex id.  Random ids keep identity
// meaningful across owner processes without any counter state to maintain
// or recover; both invocations and decisions use them.
func newID128() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
