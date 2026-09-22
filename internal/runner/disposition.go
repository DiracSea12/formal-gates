package runner

// The wait state's single authority (the task_dispositions table,
// store.go's v5 delta): one row per parked task answers, structurally,
// everything the flow needs to decide the next move — why the stage
// stopped (kind, reason, evidence, generation, attempt), whether the
// user has been asked (question_id), what they answered (the linked
// decisions row's answer) and where the answer drives work (the
// ResolveDisposition revision).  The lifecycle is linear and enforced:
//
//	park    Store.SetDisposition — an executor or a recovery
//	        consultation lands the row; a row that already holds a
//	        question is refused (ErrDispositionAsked): an asked park
//	        is answered or resolved, never silently replaced;
//	ask     Store.Ask — one transaction creates the pending question
//	        row and links it; while it stands, re-deriving the ask
//	        returns the same decision id;
//	answer  Store.Answer (decisions.go) settles the linked row in
//	        place; the row keeps the link, so "answered, transition
//	        pending" stays a queryable state;
//	resolve Store.ResolveDisposition — revises the target task and
//	        clears the row in one transaction, so the answer and the
//	        transition it drove cannot part ways.  ClearDisposition
//	        refuses a linked row: an asked park has exactly one exit.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrDispositionAsked: SetDisposition or ClearDisposition touched a row
// that already holds its question.  An asked park is the user's to
// answer and ResolveDisposition's to clear; replacing or dropping it
// mid-flight would decouple the standing question from the park it
// belongs to, so it is refused as the caller bug it is.
var ErrDispositionAsked = errors.New("runner: disposition already holds its question; answer and resolve it instead")

// ErrResolutionPending: ResolveDisposition was called for a row whose
// linked question has no answer yet.  The transition an unanswered
// question would drive is not authorized.
var ErrResolutionPending = errors.New("runner: disposition question is still unanswered; nothing to resolve")

// Disposition is one task's standing park fact: the whole wait state.
// QuestionID is the structural link to the asked question — empty until
// Store.Ask lands it, and the one field "was this park asked" reads
// from; the question's own answer is read through it
// (Store.DispositionQuestion).
type Disposition struct {
	TaskID     string
	Kind       string
	Generation int64
	Attempt    int
	Reason     string
	Evidence   string
	QuestionID string
}

// StandingQuestion is the linked question of one parked task, as the
// transitions read it: pending (Answered=false) while the user decides,
// settled (Answered=true, Choice) once they have.
type StandingQuestion struct {
	DecisionID string
	TaskID     string
	Answered   bool
	Choice     string
	Body       string
}

// SetDisposition lands one task's park fact — one row per task, the
// latest park wins.  Deliberately no advance lock: executors call it
// from inside a Run (execution happens outside any transaction), the
// park precedes any ask, and SQLite serializes the short write with
// every other one here — the same contract Store.SetSession carries.
// A row that already holds its question is refused (ErrDispositionAsked):
// the asked park is answered and resolved, never re-parked underneath a
// standing or settled question.
func (s *Store) SetDisposition(ctx context.Context, d Disposition) error {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO task_dispositions
			(run_id, task_id, kind, generation, attempt, reason, evidence, question_id)
		VALUES (?,?,?,?,?,?,?,NULL)
		ON CONFLICT (run_id, task_id) DO UPDATE SET
			kind=excluded.kind, generation=excluded.generation,
			attempt=excluded.attempt, reason=excluded.reason,
			evidence=excluded.evidence
		WHERE task_dispositions.question_id IS NULL`,
		s.runID, d.TaskID, d.Kind, d.Generation, d.Attempt, d.Reason, d.Evidence)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		var linked string
		if err := s.db.QueryRowContext(ctx,
			"SELECT question_id FROM task_dispositions WHERE run_id=? AND task_id=?",
			s.runID, d.TaskID).Scan(&linked); err == nil && linked != "" {
			return fmt.Errorf("%w: task %q in run %q", ErrDispositionAsked, d.TaskID, s.runID)
		}
		return fmt.Errorf("runner: disposition for task %q did not land", d.TaskID)
	}
	return nil
}

// ClearDisposition removes one task's park fact when it never asked:
// the row a resolve clears is removed by ResolveDisposition itself, in
// the same transaction as the revision the answer drove, so a linked
// row — asked, answered or not — is refused here (ErrDispositionAsked)
// rather than dropped out from under its question.
func (s *Store) ClearDisposition(ctx context.Context, taskID string) error {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM task_dispositions WHERE run_id=? AND task_id=? AND question_id IS NULL",
		s.runID, taskID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		var linked string
		if err := s.db.QueryRowContext(ctx,
			"SELECT question_id FROM task_dispositions WHERE run_id=? AND task_id=?",
			s.runID, taskID).Scan(&linked); err == nil && linked != "" {
			return fmt.Errorf("%w: task %q in run %q", ErrDispositionAsked, taskID, s.runID)
		}
	}
	return nil
}

// Ask opens the standing disposition's question and links it, in one
// short transaction: the row must stand, at the task's current
// generation, and the question lands on questionTask at ITS current
// generation through the same single write path every decisions row
// uses (insertDecision) — so the pending question is held out of
// dispatch and surfaced by the ordinary gate reads while the park's own
// invocation holds the chain behind it.  Like Answer it takes no
// advance lock: it schedules and executes nothing.  Idempotent while
// the linked question stands — a re-derived ask (a reopened owner, a
// retried transition) returns the same decision id instead of forking a
// second question.  A linked question that already carries an answer is
// refused with ErrQuestionAnswered: a fresh question for a fresh park
// becomes askable only after ResolveDisposition retired the lineage.
func (s *Store) Ask(ctx context.Context, taskID, questionTask string, c *Confirmation) (string, error) {
	if c == nil {
		return "", fmt.Errorf("runner: Ask for task %q needs a confirmation, not nil", taskID)
	}
	if err := validateConfirmationBody(taskID, c); err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() // no-op after Commit
	var parkedGeneration int64
	var linked sql.NullString
	err = tx.QueryRowContext(ctx,
		"SELECT generation, question_id FROM task_dispositions WHERE run_id=? AND task_id=?",
		s.runID, taskID).Scan(&parkedGeneration, &linked)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("runner: no standing disposition for task %q in run %q; a question without its park is refused", taskID, s.runID)
	}
	if err != nil {
		return "", err
	}
	var current int64
	err = tx.QueryRowContext(ctx,
		"SELECT generation FROM tasks WHERE run_id=? AND task_id=?",
		s.runID, taskID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("runner: no task %q in run %q", taskID, s.runID)
	}
	if err != nil {
		return "", err
	}
	if current != parkedGeneration {
		return "", fmt.Errorf("runner: disposition of task %q parks generation %d, the task is at %d; a stale park asks nothing", taskID, parkedGeneration, current)
	}
	if linked.Valid && linked.String != "" {
		var state string
		err = tx.QueryRowContext(ctx,
			"SELECT state FROM decisions WHERE decision_id=? AND run_id=?",
			linked.String, s.runID).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("runner: disposition of task %q links unknown decision %s", taskID, linked.String)
		}
		if err != nil {
			return "", err
		}
		if state == decisionPending {
			if err := tx.Commit(); err != nil {
				return "", err
			}
			return linked.String, nil
		}
		return "", fmt.Errorf("%w: task %q in run %q", ErrQuestionAnswered, taskID, s.runID)
	}
	var questionGeneration int64
	err = tx.QueryRowContext(ctx,
		"SELECT generation FROM tasks WHERE run_id=? AND task_id=?",
		s.runID, questionTask).Scan(&questionGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("runner: no question task %q in run %q", questionTask, s.runID)
	}
	if err != nil {
		return "", err
	}
	id, err := insertDecision(ctx, tx, s.runID, questionTask, questionGeneration, c)
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE task_dispositions SET question_id=? WHERE run_id=? AND task_id=?",
		id, s.runID, taskID); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

// ResolveDisposition applies the parked task's settled transition: the
// revision of targetTask (ReviseTask's whole contract — scope bump,
// invalidation closure, superseded intents, the gate carry) lands in
// one transaction with the row's removal, so an applied answer can
// never stand without the transition it drove, and a crash between the
// two is impossible.  When the row holds its question, the question
// must already be answered (ErrResolutionPending otherwise) and the
// question task's generation is retired in the same transaction — the
// answered row stays where it is as history, and the question task's
// next generation is clean for the next Ask.  A row without a question
// (a transition that asked nobody) resolves the same way, minus the
// retirement.  Joins the advance guard like ReviseTask: it revises.
func (s *Store) ResolveDisposition(ctx context.Context, taskID, targetTask string, spec TaskSpec) error {
	if !s.advance.TryLock() {
		return ErrAdvancing
	}
	defer s.advance.Unlock()
	if err := validateConfirmation(spec); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit
	var linked sql.NullString
	err = tx.QueryRowContext(ctx,
		"SELECT question_id FROM task_dispositions WHERE run_id=? AND task_id=?",
		s.runID, taskID).Scan(&linked)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("runner: no standing disposition for task %q in run %q; nothing to resolve", taskID, s.runID)
	}
	if err != nil {
		return err
	}
	questionTask := ""
	if linked.Valid && linked.String != "" {
		var state string
		err = tx.QueryRowContext(ctx,
			"SELECT state, task_id FROM decisions WHERE decision_id=? AND run_id=?",
			linked.String, s.runID).Scan(&state, &questionTask)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("runner: disposition of task %q links unknown decision %s", taskID, linked.String)
		}
		if err != nil {
			return err
		}
		if state != decisionAnswered {
			return fmt.Errorf("%w: task %q in run %q", ErrResolutionPending, taskID, s.runID)
		}
	}
	if err := s.reviseScope(ctx, tx, targetTask, spec); err != nil {
		return err
	}
	if questionTask != "" {
		// Retire the question lineage: the answered question keeps its
		// row at its generation as history, the question task moves one
		// generation past it (nothing is carried, nothing depends on the
		// task's input), and its unsettled intents — if any — turn
		// superseded exactly as a revision supersedes them.
		var old int64
		if err := tx.QueryRowContext(ctx,
			"SELECT generation FROM tasks WHERE run_id=? AND task_id=?",
			s.runID, questionTask).Scan(&old); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks
			   SET generation=?, state=?, result_invocation_id=NULL
			 WHERE run_id=? AND task_id=?`,
			old+1, taskWaitingDeps, s.runID, questionTask); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE invocations SET state=?
			 WHERE run_id=? AND task_id=? AND generation=? AND state=?`,
			invSuperseded, s.runID, questionTask, old, invPending); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM task_dispositions WHERE run_id=? AND task_id=?",
		s.runID, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// Disposition reads one task's standing park fact; found=false when the
// task is not parked.
func (s *Store) Disposition(ctx context.Context, taskID string) (Disposition, bool, error) {
	var d Disposition
	var linked sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT task_id, kind, generation, attempt, reason, evidence, question_id
		  FROM task_dispositions
		 WHERE run_id=? AND task_id=?`,
		s.runID, taskID).
		Scan(&d.TaskID, &d.Kind, &d.Generation, &d.Attempt, &d.Reason, &d.Evidence, &linked)
	if errors.Is(err, sql.ErrNoRows) {
		return Disposition{}, false, nil
	}
	if err != nil {
		return Disposition{}, false, err
	}
	d.QuestionID = linked.String
	return d, true, nil
}

// Dispositions lists every standing park fact of the run, ordered by
// task id — the transitions' stable iteration order.
func (s *Store) Dispositions(ctx context.Context) ([]Disposition, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT task_id, kind, generation, attempt, reason, evidence, question_id
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
		var linked sql.NullString
		if err := rows.Scan(&d.TaskID, &d.Kind, &d.Generation, &d.Attempt, &d.Reason, &d.Evidence, &linked); err != nil {
			return nil, err
		}
		d.QuestionID = linked.String
		out = append(out, d)
	}
	return out, rows.Err()
}

// DispositionQuestion reads the linked question of one task's standing
// park: found=false when the task is not parked or its park has not
// been asked.  The question's settlement is read through the link, so
// "asked" and "answered" are two reads of the same row pair, never a
// match against the question's display text.
func (s *Store) DispositionQuestion(ctx context.Context, taskID string) (StandingQuestion, bool, error) {
	var q StandingQuestion
	var state string
	var answer sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT d.decision_id, d.task_id, d.state, d.answer, d.body
		  FROM task_dispositions t
		  JOIN decisions d ON d.decision_id = t.question_id
		 WHERE t.run_id=? AND t.task_id=?`,
		s.runID, taskID).Scan(&q.DecisionID, &q.TaskID, &state, &answer, &q.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return StandingQuestion{}, false, nil
	}
	if err != nil {
		return StandingQuestion{}, false, err
	}
	q.Answered = state == decisionAnswered
	q.Choice = answer.String
	return q, true, nil
}
