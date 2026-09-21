package runner

// K2 — exact process confirmations and durable waiting (plan §6.1, §7):
// the decisions table gates a task behind a persisted user decision,
// materialized by CreateRun and settled by Store.Answer.  The
// persistence contract is documented on Confirmation,
// materializeDecisions and the gate read of dispatchableTasks (runner.go).

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
	// ErrDecisionRefused: the gate's decision was answered with a choice
	// other than the one that allows execution.  The refusal is a
	// durable fact of the decisions row, not a Run-time opinion: the
	// gated task and its successors do not execute, and re-running
	// reproduces the same refusal from the stored answer every time.
	// Contrast ErrWaitingDecision (no answer yet — still askable).
	ErrDecisionRefused = errors.New("runner: decision was answered without allowing execution")
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

// Persisted decision states.  A row is created pending (by CreateRun's
// materialization) and consumed once: pending→answered is the only
// transition, so a gate never re-asks — an executor failure after the
// answer means the answer stands and the retry runs straight through (or,
// after a refusing answer, is refused again, just as durably).
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
// choose, which choice authorizes executing the gated task, which
// requirement revision the question belongs to, and the small display
// body shown with it.  It is a creation-time declaration, persisted with
// the run: CreateRun validates it (see validateConfirmation) and
// materializes the pending decisions row plus its decision_created event
// in the run's creation transaction — run, task, generation, revision,
// body digest, options and allow choice all freeze on that row, which
// then defines the gate.  The frozen row is authoritative, so a later
// Run's Confirm fields change nothing on the ground; see
// dispatchableTasks in runner.go for how the gate is read.
type Confirmation struct {
	// Options are the choices the user may answer with.  Must be
	// non-empty: a question with no choices could never be answered and
	// would deadlock the gate forever.
	Options []string
	// Allow is the one option that authorizes executing the gated task
	// (§6.1 "whether execution is allowed").  Must be non-empty and one
	// of Options.  Any other answered choice leaves the gate closed for
	// good — see ErrDecisionRefused.  It is stored as a plain choice,
	// not a hard-coded English word, because the vocabulary belongs to
	// the declaring run, not this kernel.
	Allow    string
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
// creation carries the question as asked (revision, options and the
// allowing choice), consumption carries the chosen answer.
type decisionEvent struct {
	DecisionID string   `json:"decision_id"`
	TaskID     string   `json:"task_id"`
	Generation int64    `json:"generation"`
	Revision   string   `json:"revision,omitempty"`
	Options    []string `json:"options,omitempty"`
	Allow      string   `json:"allow,omitempty"`
	Choice     string   `json:"choice,omitempty"`
}

// validateConfirmation rejects the three malformed gate declarations
// that, once persisted, could never be repaired through the answer path:
// no options (no answer would ever be legal), no allowing choice (no
// answer could ever authorize execution) and an allowing choice outside
// the options (a choice that cannot be answered would have to authorize
// execution).  CreateRun calls this for every spec BEFORE writing
// anything, so a refusal leaves the directory empty for the corrected
// attempt — a malformed declaration must not become durable state.
func validateConfirmation(sp TaskSpec) error {
	c := sp.Confirm
	if c == nil {
		return nil
	}
	switch {
	case len(c.Options) == 0:
		return fmt.Errorf("runner: task %q declares a confirmation without options; a question with no choices could never be answered", sp.TaskID)
	case c.Allow == "":
		return fmt.Errorf("runner: task %q declares a confirmation without an allowing choice; no answer could ever authorize its execution", sp.TaskID)
	case !slices.Contains(c.Options, c.Allow):
		return fmt.Errorf("runner: task %q declares allowing choice %q outside its confirmation options %v", sp.TaskID, c.Allow, c.Options)
	}
	return nil
}

// materializeDecisions writes every declared gate as a pending decisions
// row plus its decision_created event, inside the run's CREATION
// transaction — the gate exists exactly when the run does, so "this task
// needs a user decision" survives even an owner that dies before the
// first Run (§13.2).  generation matches the generation CreateRun gives
// the task itself (1); when later units introduce re-evaluation, a
// bumped task carries its own new gate.  Called only from CreateRun;
// Answer and the run loop never write gates.
func materializeDecisions(ctx context.Context, tx *sql.Tx, runID string, specs []TaskSpec) error {
	for _, sp := range specs {
		if sp.Confirm == nil {
			continue
		}
		c := sp.Confirm
		id, err := newID128()
		if err != nil {
			return err
		}
		optionsJSON, err := json.Marshal(c.Options)
		if err != nil {
			return err
		}
		eventJSON, err := json.Marshal(decisionEvent{
			DecisionID: id,
			TaskID:     sp.TaskID,
			Generation: 1,
			Revision:   c.Revision,
			Options:    c.Options,
			Allow:      c.Allow,
		})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO decisions
				(decision_id, run_id, task_id, generation, revision, body,
				 body_digest, options_json, allow, created_at, state)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			id, runID, sp.TaskID, 1, c.Revision, string(c.Body),
			inputDigest(c.Body), string(optionsJSON), c.Allow, nowUTC(), decisionPending); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO events (run_id, kind, payload_json) VALUES (?,?,?)",
			runID, eventKindDecisionCreated, eventJSON); err != nil {
			return err
		}
	}
	return nil
}

// decisionStatus reports whether the run holds any still-pending gate and
// any answered-without-allowing one.  The drained run loop reports these
// as its reason for stopping (settle in runner.go); reading the durable
// rows rather than this Run's visit history keeps the answer identical
// for every owner.  The ready query (dispatchableTasks in runner.go)
// excludes gated tasks by this same rule, so what it skips and what this
// reports never disagree.
func (s *Store) decisionStatus(ctx context.Context) (waiting, refused bool, err error) {
	var pending, refusedCount int
	err = s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM decisions WHERE run_id=? AND state=?),
			(SELECT COUNT(*) FROM decisions WHERE run_id=? AND state=? AND answer<>allow)`,
		s.runID, decisionPending, s.runID, decisionAnswered).
		Scan(&pending, &refusedCount)
	return pending > 0, refusedCount > 0, err
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
