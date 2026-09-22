package runner

// A task's standing park fact (the task_dispositions table, store.go's
// v5 delta): why one stage's invocation stopped without a receipt and
// what evidence names it.  The composition's executors park — a BLOCKED
// answer, an answer the worker contract rejects, a mandatory-findings
// review, a check that could not execute, an inert route guard — and the
// transitions between runs turn the standing row into its question,
// apply the answer's retry, and clear it.  One row per task: the latest
// park wins, because only the CURRENT park decides the next move.

import (
	"context"
	"database/sql"
	"errors"
)

// Disposition is one task's standing park fact.  TaskID, Generation,
// Attempt and Kind are exactly the inputs of the disposition id the
// placed question freezes into its body (the composition's
// dispositionIDOf) — the row and its question therefore share one
// identity with no second record anywhere.
type Disposition struct {
	TaskID     string
	Kind       string
	Generation int64
	Attempt    int
	Reason     string
	Evidence   string
}

// SetDisposition lands (or replaces) one task's park fact — one row per
// task, the latest park wins.  Deliberately no advance lock: executors
// call it from inside a Run (execution happens outside any transaction),
// the park precedes any ask, and SQLite serializes the short write with
// every other one here — the same contract Store.SetSession carries.
func (s *Store) SetDisposition(ctx context.Context, d Disposition) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_dispositions
			(run_id, task_id, kind, generation, attempt, reason, evidence)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT (run_id, task_id) DO UPDATE SET
			kind=excluded.kind, generation=excluded.generation,
			attempt=excluded.attempt, reason=excluded.reason,
			evidence=excluded.evidence`,
		s.runID, d.TaskID, d.Kind, d.Generation, d.Attempt, d.Reason, d.Evidence)
	return err
}

// ClearDisposition removes one task's park fact — its question's answer
// has been applied (or the park was superseded by a later revision).
// Idempotent: clearing an already-clear task is the state asked for.
func (s *Store) ClearDisposition(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM task_dispositions WHERE run_id=? AND task_id=?",
		s.runID, taskID)
	return err
}

// Disposition reads one task's standing park fact; found=false when the
// task is not parked.
func (s *Store) Disposition(ctx context.Context, taskID string) (Disposition, bool, error) {
	var d Disposition
	err := s.db.QueryRowContext(ctx, `
		SELECT task_id, kind, generation, attempt, reason, evidence
		  FROM task_dispositions
		 WHERE run_id=? AND task_id=?`,
		s.runID, taskID).
		Scan(&d.TaskID, &d.Kind, &d.Generation, &d.Attempt, &d.Reason, &d.Evidence)
	if errors.Is(err, sql.ErrNoRows) {
		return Disposition{}, false, nil
	}
	if err != nil {
		return Disposition{}, false, err
	}
	return d, true, nil
}

// Dispositions lists every standing park fact of the run, ordered by
// task id — the transitions' stable iteration order.
func (s *Store) Dispositions(ctx context.Context) ([]Disposition, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT task_id, kind, generation, attempt, reason, evidence
		  FROM task_dispositions
		 WHERE run_id=?
		 ORDER BY task_id`, s.runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Disposition
	for rows.Next() {
		var d Disposition
		if err := rows.Scan(&d.TaskID, &d.Kind, &d.Generation, &d.Attempt, &d.Reason, &d.Evidence); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
