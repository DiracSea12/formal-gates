package runner

// K2 — exact process confirmations and durable waiting (plan §6.1, §7).
// This file owns the decisions table: the lazily materialized gate in
// front of a task, its pending/answered lifecycle, and the Answer entry
// point a user's reply travels through.  The run loop only consults the
// gate; it never interprets an answer itself — without a persisted user
// decision the gated task and its successors never execute.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Answer-side sentinels.  Each corresponds to one §6.1 refusal class;
// together with ErrWaitingDecision they let callers tell every refusal
// apart without string matching.
var (
	// ErrWaitingDecision: Run stopped because the selected task's
	// decision gate is pending — the gated work, and everything behind
	// it, waits for Store.Answer to succeed.
	ErrWaitingDecision = errors.New("runner: task is waiting for a user decision")
	// ErrRunFinished: the run reached its terminal state and accepts no
	// answer at all, not even an idempotent replay of an already
	// accepted one — the same precedence run_graph gives RUN_TERMINAL.
	// Contrast ErrDecisionStale (the request itself is invalid) and
	// ErrDecisionConflict (the request exists but was answered
	// otherwise).
	ErrRunFinished = errors.New("runner: finished run does not accept a decision answer")
	// ErrDecisionStale: no decision row with this id exists in this run —
	// an old id from an earlier run or a request that never existed.
	// A pending question elsewhere never substitutes for the addressed
	// one.  Contrast ErrRunFinished (the run itself refuses everything)
	// and ErrDecisionConflict (this request exists but was answered
	// otherwise).
	ErrDecisionStale = errors.New("runner: unknown decision id in this run")
	// ErrDecisionConflict: the decision exists and was already answered
	// with a different choice (§6.1 "same id, different answer"); the
	// stored answer stands.  Contrast ErrDecisionStale (no such request)
	// and ErrRunFinished (the run itself is terminal).
	ErrDecisionConflict = errors.New("runner: decision already answered with a different choice")
)

// Persisted decision states.  A row is created pending and consumed once:
// pending→answered is the only transition, so a gate never re-asks — an
// executor failure after the answer means the answer stands and the retry
// runs straight through.
const (
	decisionPending  = "pending"
	decisionAnswered = "answered"
)

// Event kinds for the two decision transitions; each is appended in the
// same transaction as the transition it describes (§7 events).
const (
	eventKindDecisionCreated  = "decision_created"
	eventKindDecisionAnswered = "decision_answered"
)

// Confirmation is the gate declaration on a TaskSpec: what the user may
// choose, which requirement revision the question belongs to, and the
// small display body shown with it.  §6.1's binding is persisted when the
// gate first materializes — run, task and generation on the decisions
// row; revision, body digest and options as its columns — and the row
// then freezes the question: a later Run passing different Confirm fields
// does not rewrite it (swapping the question while it hangs pending would
// be the worse failure; legitimate changes arrive with a generation bump
// in a later unit).
type Confirmation struct {
	Options  []string
	Revision string
	Body     []byte
}

// DecisionView is one pending decision as shown to a user.
type DecisionView struct {
	DecisionID string
	TaskID     string
	Revision   string
	Body       string
	Options    []string
}

// AnswerReceipt is the acceptance record of one answer.  Replaying the
// same choice on the same decision returns this exact receipt again
// (§6.1) with no new transition or event.
type AnswerReceipt struct {
	DecisionID string
	Choice     string
	AnsweredAt time.Time
}

// decisionEvent is the events payload of both decision transitions:
// creation carries the question as asked (revision and options),
// consumption carries the chosen answer.
type decisionEvent struct {
	DecisionID string   `json:"decision_id"`
	TaskID     string   `json:"task_id"`
	Generation int64    `json:"generation"`
	Revision   string   `json:"revision,omitempty"`
	Options    []string `json:"options,omitempty"`
	Choice     string   `json:"choice,omitempty"`
}

// gateDecision materializes the decision gate of a ready gated task and
// reports whether it is still pending.  An existing row is authoritative
// and only read — that read is the hot path of every re-run after the
// gate exists; a missing row is created pending together with its
// decision_created event in one short transaction, exactly like every
// other transition-and-event pair in this kernel.  The same gate
// therefore has exactly one row and one creation event no matter how
// often Run reaches it, in how many owner processes.
func (s *Store) gateDecision(ctx context.Context, t *taskRow, c *Confirmation) (bool, error) {
	var state string
	err := s.db.QueryRowContext(ctx,
		"SELECT state FROM decisions WHERE run_id=? AND task_id=? AND generation=?",
		s.runID, t.taskID, t.generation).Scan(&state)
	if err == nil {
		return state == decisionPending, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	id, err := newID128()
	if err != nil {
		return false, err
	}
	optionsJSON, err := json.Marshal(c.Options)
	if err != nil {
		return false, err
	}
	eventJSON, err := json.Marshal(decisionEvent{
		DecisionID: id,
		TaskID:     t.taskID,
		Generation: t.generation,
		Revision:   c.Revision,
		Options:    c.Options,
	})
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() // no-op after Commit
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO decisions
			(decision_id, run_id, task_id, generation, revision, body,
			 body_digest, options_json, created_at, state)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		id, s.runID, t.taskID, t.generation, c.Revision, string(c.Body),
		inputDigest(c.Body), string(optionsJSON), nowUTC(), decisionPending); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO events (run_id, kind, payload_json) VALUES (?,?,?)",
		s.runID, eventKindDecisionCreated, eventJSON); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// PendingDecisions lists this run's unanswered decisions, ordered by task
// id for a stable display.  Answered decisions are history, not open
// questions, and never appear.
func (s *Store) PendingDecisions(ctx context.Context) ([]DecisionView, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT decision_id, task_id, revision, body, options_json
		  FROM decisions
		 WHERE run_id=? AND state=?
		 ORDER BY task_id`, s.runID, decisionPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DecisionView
	for rows.Next() {
		var v DecisionView
		var optionsJSON string
		if err := rows.Scan(&v.DecisionID, &v.TaskID, &v.Revision, &v.Body, &optionsJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(optionsJSON), &v.Options); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Answer settles one decision with a user's choice, enforcing the §6.1
// answer rules inside a single short transaction and in a fixed order:
//
//  1. a terminal run (all its tasks finished) refuses everything, even
//     the idempotent replay of an already accepted answer — mirroring
//     the precedence run_graph gives RUN_TERMINAL;
//  2. an id matching no row of THIS run is stale (an old id, another
//     run's id, or a request that never existed); no "there is only one
//     open question" inference ever substitutes a different target;
//  3. an already-answered decision replays exactly: the same choice
//     returns the original receipt with zero new transitions and
//     events, a different choice is a conflict that leaves the stored
//     answer standing;
//  4. a pending decision accepts only one of its stored options — any
//     other choice is refused descriptively with the row still pending —
//     and a valid choice is consumed exactly once: state, answer,
//     answered_at and the decision_answered event commit together.
//
// Answer deliberately takes no advance lock: it schedules and executes
// nothing, and its short write transaction is serialized by SQLite like
// every other write here.
func (s *Store) Answer(ctx context.Context, decisionID, choice string) (AnswerReceipt, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AnswerReceipt{}, err
	}
	defer tx.Rollback() // no-op after Commit
	// ① Terminal runs answer nothing, and are checked before anything
	// about the request: "finished" is a fact about the run, not the
	// request, so it outranks even the idempotent replay below.
	var unfinished int
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tasks WHERE run_id=? AND state<>?",
		s.runID, taskFinished).Scan(&unfinished); err != nil {
		return AnswerReceipt{}, err
	}
	if unfinished == 0 {
		return AnswerReceipt{}, ErrRunFinished
	}
	// ② The request must exist in this run.
	var (
		taskID, state, optionsJSON string
		generation                 int64
		answer, answeredAt         sql.NullString
	)
	err = tx.QueryRowContext(ctx, `
		SELECT task_id, generation, state, options_json, answer, answered_at
		  FROM decisions
		 WHERE decision_id=? AND run_id=?`, decisionID, s.runID).
		Scan(&taskID, &generation, &state, &optionsJSON, &answer, &answeredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AnswerReceipt{}, ErrDecisionStale
	}
	if err != nil {
		return AnswerReceipt{}, err
	}
	if state == decisionAnswered {
		// ③ Same choice replays the original receipt; a different one is
		// a conflict.  Either way nothing is written.
		if !answer.Valid || answer.String != choice {
			return AnswerReceipt{}, ErrDecisionConflict
		}
		at, err := time.Parse(time.RFC3339, answeredAt.String)
		if err != nil {
			return AnswerReceipt{}, fmt.Errorf("runner: decision %s answered_at %q unreadable: %w",
				decisionID, answeredAt.String, err)
		}
		return AnswerReceipt{DecisionID: decisionID, Choice: choice, AnsweredAt: at}, nil
	}
	// ④ Pending (the CHECK constraint admits only pending and answered):
	// the row's frozen options alone are valid choices.
	var options []string
	if err := json.Unmarshal([]byte(optionsJSON), &options); err != nil {
		return AnswerReceipt{}, err
	}
	if !slices.Contains(options, choice) {
		return AnswerReceipt{}, fmt.Errorf("runner: choice %q is not an offered option of decision %s (options: %v)",
			choice, decisionID, options)
	}
	now := nowUTC()
	eventJSON, err := json.Marshal(decisionEvent{
		DecisionID: decisionID,
		TaskID:     taskID,
		Generation: generation,
		Choice:     choice,
	})
	if err != nil {
		return AnswerReceipt{}, err
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE decisions SET state=?, answer=?, answered_at=? WHERE decision_id=?",
		decisionAnswered, choice, now, decisionID); err != nil {
		return AnswerReceipt{}, err
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO events (run_id, kind, payload_json) VALUES (?,?,?)",
		s.runID, eventKindDecisionAnswered, eventJSON); err != nil {
		return AnswerReceipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return AnswerReceipt{}, err
	}
	at, err := time.Parse(time.RFC3339, now)
	if err != nil {
		return AnswerReceipt{}, err
	}
	return AnswerReceipt{DecisionID: decisionID, Choice: choice, AnsweredAt: at}, nil
}
