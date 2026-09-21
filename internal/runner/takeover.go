package runner

// K4 repair, F2 — the narrow takeover seam (plan §8.3): when a new owner
// opens a run whose previous owner died with a call in flight, whether
// that call is dead is a domain fact the kernel cannot derive from the
// durable rows alone.  Runner.Takeover is the single consultation point;
// nil — the default — keeps K1's recovery semantics byte for byte.

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

// TakeoverOutcome is one consultation's verdict on a pending invocation
// left behind by a previous owner.  The zero value is the dead or
// invalidated session: void it and re-run — K1's semantics.
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

// takeoverPending is recoverPending's consulted branch (Runner.Takeover):
// every still-pending invocation of a previous owner is settled per its
// verdict in one short transaction each — Adopt admits the ORIGINAL
// invocation through the shared admit path (identity re-verified, event
// appended, task finished, zero re-execution), Observe keeps it pending
// with its task running (the ready predicate excludes it), and the zero
// outcome supersedes the row — kept, never deleted — freeing the task
// for a fresh registration under a new invocation id (§8.3: the retry
// carries the newest native identity, not the invalidated one).
func (r *Runner) takeoverPending(ctx context.Context) error {
	rows, err := r.store.db.QueryContext(ctx, `
		SELECT invocation_id, task_id, generation, input_digest
		  FROM invocations
		 WHERE run_id=? AND state=?
		 ORDER BY task_id, generation`,
		r.store.runID, invPending)
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
			// Kept pending; the task stays running and out of the ready set.
		default: // dead or invalidated session: void it, row kept
			if err := r.voidInvocation(ctx, inv); err != nil {
				return err
			}
		}
	}
	return nil
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
