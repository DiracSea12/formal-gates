package runner

// K4 rework — the narrow takeover seam (plan §8.2/§8.3): when a new owner
// opens a run whose previous owner died with a call in flight, whether
// that call is dead is a domain fact the kernel cannot derive from the
// durable rows alone.  Runner.Takeover is the single consultation point;
// nil — the default — applies K1's supersede per call, keeping every
// invocation that carries a durable constraint (see recoverPending).
//
// The consultation's facts are split from their authorization: an
// Observe verdict is PERSISTED (call_observations, store.go) so the
// next owner cannot lose it by omitting the optional consult, and the
// dead-session verdict (the zero outcome) does not authorize
// replacement by itself — it asks, through a restart request, and only
// an allowing answer replaces the execution identity.  The requests
// live in user_requests under the lifecycle documented on that table
// (store.go).

import (
	"context"
	"errors"
	"fmt"
)

// ErrCallObserved: recovery kept a still-pending invocation under
// observation because its original supervisor is alive — no second
// executor was started.  Advance again once the call settles.  Only a
// run with a Takeover consult can report this, and an executor failure
// always outranks it (settle's fixed order).
var ErrCallObserved = errors.New("runner: original invocation still in flight and kept under observation")

// ErrTakeoverRequired: a previous owner observed this call in flight
// (the durable call_observations row) and this owner configured no
// Takeover consult — blindly superseding a call another owner vouched
// for is exactly the unsafe retry this closes, so nothing is
// superseded, nothing re-runs, and the run reports this instead
// (settle).
var ErrTakeoverRequired = errors.New("runner: a previous owner observed this call in flight and this owner has no Takeover consult; refusing to retry it blind")

// ErrCallAborted: a restart request was answered with a choice other
// than restart-session — the user chose abort.  The invalidated
// invocation is voided and its task durably out of the ready set; the
// run must not claim success, and every later Run reproduces this from
// the stored answer (settle ⑤).
var ErrCallAborted = errors.New("runner: a restart request was refused (abort); the run must not claim success")

// ErrRestartUnavailable: a takeover consultation reported a dead or
// invalidated session, which now requires a user decision — but
// Runner.Restart is not configured to ask it.  Recovery refuses to
// proceed (nothing is voided, nothing re-runs) instead of silently
// treating the fact as authorization to replace the call.
var ErrRestartUnavailable = errors.New("runner: takeover judged a session dead but Runner.Restart is not configured to ask the user about replacing it")

// TakeoverOutcome is one consultation's verdict on a pending invocation
// left behind by a previous owner — a FACT about the call, never an
// authorization by itself:
//
//   - Adopt: the original call verifiably produced Result — admit it
//     under the original call identity, never re-execute;
//   - Observe: the original supervisor is still working — the fact is
//     persisted durably (call_observations), the invocation kept
//     pending and its task out of the ready set, so no second executor
//     starts while the original lives, whichever owner reopens next;
//   - the zero value: the session is dead or invalidated — the fact
//     creates a pending restart request (Runner.Restart's question) and
//     this Run neither voids nor re-runs anything.  Only an allowing
//     answer (restart-session) voids the call and frees the task for a
//     fresh registration under a new invocation id; an aborting answer
//     voids it and blocks the task for good.
type TakeoverOutcome struct {
	// Adopt: the original call verifiably produced Result — admit it
	// under the original call identity, never re-execute.
	Adopt  bool
	Result Result // valid when Adopt is set
	// Observe: the original supervisor is still working — keep the
	// invocation pending and its task out of the ready set, so no
	// second executor starts while the original lives.
	Observe bool
}

// Takeover consults the fate of one pending invocation.  K5 implements it
// over the accepted supervisor/adapter components; consultations run
// outside any transaction (they may do I/O), one per pending invocation,
// in deterministic (task, generation) order.
type Takeover func(ctx context.Context, inv Invocation) (TakeoverOutcome, error)

// recordObservation persists one Observe fact idempotently: the row is
// never deleted and re-recording an invocation is a no-op, so any number
// of owners may observe the same call across reopens.
func (r *Runner) recordObservation(ctx context.Context, inv Invocation) error {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO call_observations (invocation_id, run_id, task_id, observed_at)
		VALUES (?,?,?,?)`,
		inv.InvocationID, r.store.runID, inv.TaskID, nowUTC()); err != nil {
		return err
	}
	return tx.Commit()
}

// observedPendingCount reports how many still-pending invocations of
// this run carry a call_observations row — the durable "someone vouched
// for this call" fact.  Rows whose invocation has settled are history
// and do not count.
func (r *Runner) observedPendingCount(ctx context.Context) (int, error) {
	var n int
	err := r.store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		  FROM call_observations o
		  JOIN invocations i ON i.invocation_id = o.invocation_id
		 WHERE i.run_id=? AND i.state=?`,
		r.store.runID, invPending).Scan(&n)
	return n, err
}

// settleRestartRequests is recovery step 1 (recoverPending, runner.go):
// the pending invocations an existing restart request is about are
// settled by the user's stored answer alone — no takeover capability
// needed, the decision is already made.  Granted: void the invalidated
// invocation, move the request to executed and free the task — the
// approved replacement runs under a fresh invocation id in this Run's
// ordinary loop.  Refused (abort): void the invocation and free the
// task's state, never re-run it — the ready predicate keeps it out and
// settle reports ErrCallAborted.  Pending: keep everything and wait.
func (r *Runner) settleRestartRequests(ctx context.Context) error {
	rows, err := r.store.db.QueryContext(ctx, `
		SELECT rr.request_id, rr.state, rr.invocation_id, rr.task_id
		  FROM user_requests rr
		  JOIN invocations i ON i.invocation_id = rr.invocation_id
		 WHERE rr.run_id=? AND rr.kind=? AND i.state=?
		 ORDER BY rr.task_id, rr.target_generation`,
		r.store.runID, kindRestart, invPending)
	if err != nil {
		return err
	}
	defer rows.Close()
	type restartCase struct {
		requestID, state, invocationID, taskID string
	}
	var cases []restartCase
	for rows.Next() {
		var c restartCase
		if err := rows.Scan(&c.requestID, &c.state, &c.invocationID, &c.taskID); err != nil {
			return err
		}
		cases = append(cases, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, c := range cases {
		switch c.state {
		case reqGranted:
			if err := r.replaceRestartedInvocation(ctx, c.requestID, c.invocationID, c.taskID); err != nil {
				return err
			}
		case reqRefused:
			// The user aborted: void the dead call, release the task's
			// state, and leave it to the ready predicate and settle.
			if err := r.voidInvocation(ctx, Invocation{
				InvocationID: c.invocationID,
				TaskID:       c.taskID,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// replaceRestartedInvocation performs the approved replacement (step 1's
// granted branch) in one short transaction: the invalidated invocation
// is superseded (row kept), the restart request moves to executed, and
// the task returns to waiting_deps so this Run's loop registers and runs
// the replacement under a NEW invocation id.
func (r *Runner) replaceRestartedInvocation(ctx context.Context, requestID, invocationID, taskID string) error {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit
	if _, err := tx.ExecContext(ctx,
		"UPDATE invocations SET state=? WHERE invocation_id=? AND run_id=? AND state=?",
		invSuperseded, invocationID, r.store.runID, invPending); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE user_requests SET state=?, resolved_at=? WHERE request_id=? AND state=?",
		reqExecuted, nowUTC(), requestID, reqGranted); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE tasks SET state=? WHERE run_id=? AND task_id=? AND state=?",
		taskWaitingDeps, r.store.runID, taskID, taskRunning); err != nil {
		return err
	}
	return tx.Commit()
}

// takeoverPending is recoverPending's consulted branch (Runner.Takeover,
// recovery step 3): every still-pending invocation a restart request is
// NOT already about is settled per its verdict in one short transaction
// each — Adopt admits the ORIGINAL invocation through the shared admit
// path (identity re-verified, event appended, task finished, zero
// re-execution), Observe persists the observation fact and keeps the row
// pending with its task running (the ready predicate excludes it), and
// the zero outcome — a dead or invalidated session — creates a pending
// restart request (Runner.Restart's question) without voiding or
// re-running anything in this Run.
func (r *Runner) takeoverPending(ctx context.Context) error {
	rows, err := r.store.db.QueryContext(ctx, `
		SELECT invocation_id, task_id, generation, input_digest
		  FROM invocations i
		 WHERE i.run_id=? AND i.state=?
		   AND NOT EXISTS (SELECT 1
		                     FROM user_requests rr
		                    WHERE rr.invocation_id = i.invocation_id
		                      AND rr.kind = ?)
		 ORDER BY task_id, generation`,
		r.store.runID, invPending, kindRestart)
	if err != nil {
		return err
	}
	var invs []Invocation
	for rows.Next() {
		// Input never rides along: it is not persisted (§7) and the
		// consultation judges identity, not payload.
		var inv Invocation
		if err := rows.Scan(&inv.InvocationID, &inv.TaskID, &inv.Generation, &inv.InputDigest); err != nil {
			rows.Close()
			return err
		}
		invs = append(invs, inv)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close() // the consultations may do I/O; no read is held open
	for _, inv := range invs {
		out, err := r.Takeover(ctx, inv)
		if err != nil {
			return fmt.Errorf("runner: takeover consultation for invocation %s: %w", inv.InvocationID, err)
		}
		switch {
		case out.Adopt:
			if err := r.admit(ctx, inv, out.Result); err != nil {
				return fmt.Errorf("runner: adopting invocation %s: %w", inv.InvocationID, err)
			}
		case out.Observe:
			// The fact outlives this owner; the call stays pending and
			// its task stays running and out of the ready set.
			if err := r.recordObservation(ctx, inv); err != nil {
				return err
			}
		default: // dead or invalidated session: a fact, not an authorization
			if err := r.createRestartRequest(ctx, inv); err != nil {
				return err
			}
		}
	}
	return nil
}

// createRestartRequest asks, durably, whether the dead or invalidated
// invocation may be replaced: one pending restart request bound to the
// task's current generation and the invocation itself, carrying
// Runner.Restart's frozen question and its request_created event.  The
// invocation stays pending and the task running — this Run voids and
// re-runs nothing; the stored answer decides at the next recovery.
// Runner.Restart must be a valid confirmation, checked before any write
// (zero residue).
func (r *Runner) createRestartRequest(ctx context.Context, inv Invocation) error {
	if r.Restart == nil {
		return fmt.Errorf("%w: invocation %s of task %q",
			ErrRestartUnavailable, inv.InvocationID, inv.TaskID)
	}
	if err := validateConfirmationBody(inv.TaskID, r.Restart); err != nil {
		return err
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit
	var generation int64
	if err := tx.QueryRowContext(ctx,
		"SELECT generation FROM tasks WHERE run_id=? AND task_id=?",
		r.store.runID, inv.TaskID).Scan(&generation); err != nil {
		return err
	}
	if _, err := insertUserRequest(ctx, tx, r.store.runID, kindRestart,
		inv.TaskID, generation, inv.InvocationID, r.Restart, nil); err != nil {
		return err
	}
	return tx.Commit()
}

// voidInvocation supersedes one dead session's pending invocation and
// frees its task for re-registration, in one short transaction.  The row
// is kept for the late-result path (§4.2).
func (r *Runner) voidInvocation(ctx context.Context, inv Invocation) error {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit
	if _, err := tx.ExecContext(ctx,
		"UPDATE invocations SET state=? WHERE invocation_id=? AND run_id=? AND state=?",
		invSuperseded, inv.InvocationID, r.store.runID, invPending); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE tasks SET state=? WHERE run_id=? AND task_id=? AND state=?",
		taskWaitingDeps, r.store.runID, inv.TaskID, taskRunning); err != nil {
		return err
	}
	return tx.Commit()
}
