package runner

// The mid-flight question seam.  CreateRun and ReviseTask materialize their
// gates as a side effect of writing the task definition; a run whose business
// flow discovers a question DURING a task's lifetime (a failed review asking
// retry-or-abort, a worker answer the contract rejects) has no definition
// change to hang the question on — the task stays on its current generation
// until the answer decides otherwise.  OpenQuestion is that one narrow write:
// it creates a pending decisions row at the task's CURRENT generation, reusing
// insertDecision and therefore every rule Answer already gives the row for
// free (bound to the generation, invalidated by the next revision, replay and
// conflict handling, the settle-time waiting/refusal reads).  Nothing else
// about the task changes: no generation bump, no dependency invalidation —
// the caller applies those itself, through ReviseTask/RequestRevision, once
// the answer is in.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrQuestionAnswered: OpenQuestion was called for a task whose current
// generation already holds an ANSWERED question.  The ask-then-revise
// discipline (one question per generation, the revision moves the generation
// before the next ask) makes this a caller bug, and it is refused as one
// rather than silently overwriting history the answer path owns.
var ErrQuestionAnswered = errors.New("runner: task already answered a question at its current generation; revise the task before asking again")

// OpenQuestion asks one question about a task at its current generation, in
// one short transaction.  Like Answer it takes no advance lock: it schedules
// and executes nothing.  The row freezes revision/body/options/allow exactly
// as passed, exactly like a creation-time gate.  Calling it again while the
// question is still pending returns the same decision id (the ask is
// idempotent — a crashed owner re-derives the same question, not a second
// one).  A question whose generation the task has left is history: the new
// call asks at the new generation, and the old row answers nothing (the
// ordinary decisions binding rule).
func (s *Store) OpenQuestion(ctx context.Context, taskID string, c *Confirmation) (string, error) {
	if c == nil {
		return "", fmt.Errorf("runner: OpenQuestion for task %q needs a confirmation, not nil", taskID)
	}
	if err := validateConfirmationBody(taskID, c); err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() // no-op after Commit
	var generation int64
	err = tx.QueryRowContext(ctx,
		"SELECT generation FROM tasks WHERE run_id=? AND task_id=?",
		s.runID, taskID).Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("runner: no task %q in run %q", taskID, s.runID)
	}
	if err != nil {
		return "", err
	}
	var existingID, existingState string
	err = tx.QueryRowContext(ctx, `
		SELECT decision_id, state FROM decisions
		 WHERE run_id=? AND task_id=? AND generation=?`,
		s.runID, taskID, generation).Scan(&existingID, &existingState)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// No question at this generation yet: ask it.
	case err != nil:
		return "", err
	case existingState == decisionPending:
		// The same question is already standing: return it, so a re-derived
		// ask (a reopened owner, a retried executor) never forks a second
		// question about the same generation.
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return existingID, nil
	default:
		return "", fmt.Errorf("%w: task %q in run %q", ErrQuestionAnswered, taskID, s.runID)
	}
	id, err := insertDecision(ctx, tx, s.runID, taskID, generation, c)
	if err != nil {
		return "", err
	}
	return id, tx.Commit()
}
