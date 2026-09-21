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
// non-nil error means "no receipt": the invocation stays pending and the
// task is left to the next Run, which supersedes it with a fresh
// invocation instead of trusting a result that never arrived; sibling
// tasks are not interrupted.  The §4.3 domain workflow interface
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
	// Takeover, when non-nil, is consulted during recovery for every
	// still-pending invocation a previous owner left behind and no
	// restart request already covers (see recoverPending): adopt its
	// verifiable result under the original call identity, observe it
	// while its original supervisor lives (a fact persisted to
	// call_observations), or return the zero outcome — the dead-session
	// fact — which asks for a replacement through a restart request
	// instead of authorizing one.  nil — the default — keeps K1's
	// recovery byte for byte for runs without a persisted observation:
	// every pending invocation is superseded and the task re-registered.
	// A run whose call WAS observed reports ErrTakeoverRequired to a
	// nil-consult owner rather than retrying blind.  K5 wires the
	// accepted supervisor/adapter components here; the kernel defines
	// only this narrow seam (takeover.go).
	Takeover Takeover
	// Restart is the question asked when a takeover consultation
	// reports a dead or invalidated session (the zero TakeoverOutcome):
	// typically Options ["restart-session","abort"] with Allow
	// "restart-session" (§8.2/§8.3).  It is frozen on the pending
	// restart request the fact creates; only an allowing answer lets
	// recovery void the dead call and re-run under a fresh invocation
	// id, an aborting one voids it and blocks the task for good.  nil
	// with a dead-session verdict is a named refusal,
	// ErrRestartUnavailable — the kernel never treats the fact alone as
	// authorization to replace.
	Restart *Confirmation
	// Parallelism is the local bounded concurrency of §4.1: how many
	// ready tasks may execute at once.  The zero value — anything ≤ 1 —
	// runs one task at a time, the K1 behaviour; no distributed lease or
	// leader election is involved.
	Parallelism int
}

// NewRunner wires a runner to its store and executor.
func NewRunner(store *Store, exec Executor) *Runner {
	return &Runner{store: store, exec: exec}
}

// Run drives the single-owner loop (§4.1) until no work remains: compute
// the ready set from persisted facts, dispatch up to Parallelism tasks at
// once, and run each through the §4.1 path — register the intent in a
// short transaction, execute outside any transaction, then admit the
// result in a short transaction that also appends the event.  Every
// completion frees its slot and triggers a fresh ready-set computation,
// so a join starts as soon as all its inputs are finished (§3.1) and one
// admission never waits for its batch.  specs must cover the run's tasks
// with the inputs the run was created under; a changed input or a
// missing spec for ready work fails the run instead of executing blind.
// Gates no longer interrupt the loop while independent work remains:
// once everything dispatchable has drained, settle reports an executor
// failure, a still-pending question (ErrWaitingDecision), a granted-
// but-unexecuted revision (ErrRevisionGranted), a refused one
// (ErrDecisionRefused), an aborted restart (ErrCallAborted) or — with a
// takeover consult — a call still pending under observation
// (ErrCallObserved) in that order — see dispatchableTasks for the gate's
// read semantics.
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
	limit := max(r.Parallelism, 1)
	done := make(chan workerResult)
	// Tasks this Run has already dispatched, marked synchronously at
	// dispatch: registerIntent moves the durable state to running only
	// on the worker goroutine, so between dispatch and that commit a
	// refill query would hand the same task back — the map closes that
	// window.  An admitted task is finished in the store and never comes
	// back; the map's remaining job is in-flight and no-receipt work,
	// which keep their K1 semantics: the unsettled intent is superseded
	// by the NEXT Run, never retried inside this one.
	attempted := map[string]bool{}
	var execErrs []error
	var fatal error
	inflight := 0
	for fatal == nil || inflight > 0 {
		if fatal == nil {
			// Only the free slots' worth of dispatchable work is loaded
			// per round (see readyBatch).
			batch, err := r.readyBatch(ctx, byTask, attempted, limit-inflight)
			if err != nil {
				fatal = err
				continue
			}
			for inflight < limit && len(batch) > 0 {
				t := batch[0]
				batch = batch[1:]
				attempted[t.taskID] = true
				input := byTask[t.taskID].Input
				inflight++
				go func() { done <- r.runTask(ctx, t, input) }()
			}
		}
		if inflight == 0 {
			return r.settle(ctx, execErrs)
		}
		res := <-done
		inflight--
		if res.err == nil {
			continue // admitted; the store already knows
		}
		if res.noReceipt {
			execErrs = append(execErrs, res.err)
		} else if fatal == nil {
			fatal = res.err
		}
	}
	// A non-executor failure (store trouble, refused specs) aborts the
	// run; sibling executor failures ride along so no lost receipt is
	// silently dropped.
	return errors.Join(append([]error{fatal}, execErrs...)...)
}

// workerResult is one dispatched task's outcome on the Run loop's
// channel.
type workerResult struct {
	err error
	// noReceipt marks err as an executor failure — no result ever
	// arrived.  Recorded, not fatal: independent tasks keep running.
	noReceipt bool
}

// settle is the drained loop's terminal judgment, in fixed order: an
// executor failure (aggregated over every failed task), then a
// still-pending question — a current-generation gate OR a valid-binding
// pending user request (ErrWaitingDecision), then a granted-but-
// unexecuted revision (ErrRevisionGranted: approved work nobody
// completed), then an answered-without-allowing gate
// (ErrDecisionRefused), then a refused restart — the user aborted
// (ErrCallAborted: the run must not claim success), then the takeover
// facts — a still-pending invocation under a configured consult
// (ErrCallObserved) or a persisted observation without one
// (ErrTakeoverRequired), then the unfinished-but-nothing-ready
// stagnation check, otherwise nil.  §4.1: the wait is reported only
// after no independent work is left.
func (r *Runner) settle(ctx context.Context, execErrs []error) error {
	if len(execErrs) > 0 {
		return errors.Join(execErrs...)
	}
	waiting, refused, err := r.store.decisionStatus(ctx)
	if err != nil {
		return err
	}
	reqWaiting, grantedRevision, aborted, err := r.store.requestStatus(ctx)
	if err != nil {
		return err
	}
	if waiting || reqWaiting {
		return ErrWaitingDecision
	}
	if grantedRevision {
		return ErrRevisionGranted
	}
	if refused {
		return ErrDecisionRefused
	}
	if aborted {
		return ErrCallAborted
	}
	if r.Takeover != nil {
		// At settle time a pending invocation can only be one kept under
		// observation or awaiting a restart decision — this Run's own
		// no-receipt failures returned above, and every other pending
		// was adopted or asked about in recovery.
		var pending int
		if err := r.store.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM invocations WHERE run_id=? AND state=?",
			r.store.runID, invPending).Scan(&pending); err != nil {
			return err
		}
		if pending > 0 {
			return ErrCallObserved
		}
	} else if observed, err := r.observedPendingCount(ctx); err != nil {
		return err
	} else if observed > 0 {
		return ErrTakeoverRequired
	}
	return r.checkAllFinished(ctx)
}

// readyBatch loads at most want — the loop's free slots — dispatchable
// tasks, minus anything already dispatched this Run (attempted; see Run
// for why the durable state alone cannot express that).  The rows
// arrive already gate-open, so the remaining Go-side filter is spec
// coverage.  A missing spec stays the K1 refusal, with one narrowed
// surface: it now fires only for tasks that reached the dispatch window,
// so a gate-blocked task — not dispatchable until its gate answers — no
// longer triggers it.  Waiting or refused gates are skipped, not errors
// — their durable rows are what settle reports once the run has drained
// (§4.1: one task's wait never blocks independent branches).
func (r *Runner) readyBatch(ctx context.Context, byTask map[string]TaskSpec, attempted map[string]bool, want int) ([]taskRow, error) {
	batch, err := r.dispatchableTasks(ctx, want)
	if err != nil {
		return nil, err
	}
	var out []taskRow
	for _, t := range batch {
		if attempted[t.taskID] {
			continue
		}
		if _, ok := byTask[t.taskID]; !ok {
			return nil, fmt.Errorf("runner: no spec supplied for ready task %q", t.taskID)
		}
		out = append(out, t)
	}
	return out, nil
}

// runTask is one dispatched task's whole §4.1 path on its own goroutine:
// register the execution intent in a short transaction, run the executor
// outside any transaction, then admit the result in a short transaction.
// An executor failure is reported as noReceipt: the intent stays pending
// so its work is never silently dropped, and the next Run supersedes it —
// sibling tasks are not interrupted.
func (r *Runner) runTask(ctx context.Context, t taskRow, input []byte) workerResult {
	inv, err := r.registerIntent(ctx, &t, input)
	if err != nil {
		return workerResult{err: err}
	}
	if r.hooks.afterRegisterIntent != nil {
		r.hooks.afterRegisterIntent() // §13.2 point ①
	}
	res, execErr := r.exec(ctx, inv)
	if execErr != nil {
		return workerResult{
			noReceipt: true,
			err: fmt.Errorf("runner: task %s invocation %s ended without receipt: %w",
				t.taskID, inv.InvocationID, execErr),
		}
	}
	if r.hooks.afterResult != nil {
		r.hooks.afterResult() // §13.2 point ②
	}
	if err := r.admit(ctx, inv, res); err != nil {
		return workerResult{err: err}
	}
	return workerResult{}
}

// verifySpecs checks the caller's specs against the persisted task
// definitions: every spec must name an existing task, and its input digest
// must equal the bound one.  A definition's digest moves only through
// Store.ReviseTask (revision.go) — bumping the generation and invalidating
// the dependents — so a restarted owner can never settle old work with
// inputs the run did not declare.
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
			return fmt.Errorf("runner: input for task %q differs from the run's definition; changing a task's input goes through Store.ReviseTask, which bumps its generation and invalidates its dependents", sp.TaskID)
		}
	}
	return nil
}

// recoverPending closes out unsettled intents from a previous owner
// process, in a fixed order:
//
//  1. pending invocations an existing restart request is about are
//     settled by the stored answer alone (settleRestartRequests,
//     takeover.go) — a granted one voids its dead call and frees the
//     task for this Run's replacement, a refused one voids it and
//     leaves the task durably out of the ready set;
//  2. with Takeover nil — every K1–K4 default — the K1 blind supersede
//     runs verbatim, unless a persisted call_observations row marks a
//     pending invocation as observed in flight by a previous owner:
//     then nothing is superseded and nothing re-runs, and settle
//     reports ErrTakeoverRequired (F4);
//  3. with a consult, each remaining pending invocation is judged
//     outside any transaction (takeoverPending, takeover.go): Adopt,
//     Observe (the fact persisted), or the dead-session fact that
//     creates a restart request.
//
// Recovery touches only current tasks and unsettled invocations; it
// never replays the event history (§7).
func (r *Runner) recoverPending(ctx context.Context) error {
	if err := r.settleRestartRequests(ctx); err != nil {
		return err
	}
	if r.Takeover != nil {
		return r.takeoverPending(ctx)
	}
	observed, err := r.observedPendingCount(ctx)
	if err != nil {
		return err
	}
	if observed > 0 {
		// A previous owner vouched for this call; a nil consult must
		// not blind-supersede it.  Leave everything as it is — settle
		// reports ErrTakeoverRequired.
		return nil
	}
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

// dispatchableTasks returns up to want unfinished tasks that may start
// now, in task_id order — the ready set the loop dispatches from, asked
// of the store in one query.  Readiness is computed from persisted facts
// (§4.1), never stored; the deterministic order keeps dispatch
// reproducible.  Beyond "dependencies all finished", two predicates.
//
// The state predicate, t.state = waiting_deps: a task this Run
// dispatched that has reached its intent is running in the store
// (registerIntent moved it there — in flight or ended without a
// receipt), and an admitted task is finished; both stay out until the
// NEXT Run's recoverPending resets the unsettled one, preserving the K1
// rule that an unsettled intent is superseded, never retried inside
// this one.  The dispatch-to-register window before that commit is
// closed by Run's attempted map, which marks synchronously at dispatch.
//
// The gate NOT EXISTS is the authoritative read semantics of the
// decisions rows — the same rule decisionStatus reports from in
// settle — with three branches:
//
//   - no row: the run never declared this task gated, so there is no
//     gate and the task runs like any other;
//   - pending: the question stands unanswered — excluded, and nothing
//     behind the gate executes until Store.Answer succeeds;
//   - answered: the stored answer IS the gate's verdict.  The row's
//     allow choice lets the task through like ungated work; any other
//     stored choice is a durable refusal — excluded, stably across
//     reopens and re-runs because every read consults the same stored
//     answer.
//
// The restart NOT EXISTS is the same rule on the request side: a task
// whose current generation holds a REFUSED restart (the user aborted
// the replacement) is durably out of the ready set, the request-side
// mirror of a refused gate.  A pending restart keeps its task running
// (already excluded by the state predicate); a granted one was executed
// by recovery before the loop began.
//
// The query reads the decisions and user_requests rows alone, never the
// specs' Confirm fields, which is what makes a recovery that omits them
// meet the same gate (see TaskSpec.Confirm).
func (r *Runner) dispatchableTasks(ctx context.Context, want int) ([]taskRow, error) {
	rows, err := r.store.db.QueryContext(ctx, `
		SELECT t.task_id, t.generation
		  FROM tasks t
		 WHERE t.run_id = ? AND t.state = ?
		   AND NOT EXISTS (
		       SELECT 1
		         FROM task_deps d
		         JOIN tasks dep ON dep.run_id = d.run_id AND dep.task_id = d.depends_on
		        WHERE d.run_id = t.run_id AND d.task_id = t.task_id AND dep.state <> ?
		   )
		   AND NOT EXISTS (
		       SELECT 1
		         FROM decisions g
		        WHERE g.run_id = t.run_id AND g.task_id = t.task_id AND g.generation = t.generation
		          AND (g.state = ? OR (g.state = ? AND g.answer <> g.allow))
		   )
		   AND NOT EXISTS (
		       SELECT 1
		         FROM user_requests rr
		        WHERE rr.run_id = t.run_id AND rr.task_id = t.task_id
		          AND rr.kind = ? AND rr.state = ? AND rr.target_generation = t.generation
		   )
		 ORDER BY t.task_id
		 LIMIT ?`,
		r.store.runID, taskWaitingDeps, taskFinished, decisionPending, decisionAnswered,
		kindRestart, reqRefused, want)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []taskRow
	for rows.Next() {
		var t taskRow
		if err := rows.Scan(&t.taskID, &t.generation); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// nextReady returns the lowest-task_id task that may start now, or nil
// when nothing can — the sequential kernel's selector, kept for the K1/K2
// tests that drive it directly; the run loop works on whole batches
// (readyBatch).
func (r *Runner) nextReady(ctx context.Context) (*taskRow, error) {
	ts, err := r.dispatchableTasks(ctx, 1)
	if err != nil || len(ts) == 0 {
		return nil, err
	}
	return &ts[0], nil
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
