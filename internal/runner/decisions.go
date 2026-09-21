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
	// it, waits for Store.Answer to succeed.  Store.RequestRevision
	// returns it when the repair allowance asks the user (revision.go).
	ErrWaitingDecision = errors.New("runner: task is waiting for a user decision")
	// ErrDecisionRefused: the gate's decision was answered with a choice
	// other than the one that allows execution.  The refusal is a
	// durable fact of the decisions row, not a Run-time opinion: the
	// gated task and its successors do not execute, and re-running
	// reproduces the same refusal from the stored answer every time.
	// Contrast ErrWaitingDecision (no answer yet — still askable).  A
	// REFUSED revision inquiry is never this: its non-positive generation
	// slot is not the run refusing, so decisionStatus does not count it.
	ErrDecisionRefused = errors.New("runner: decision was answered without allowing execution")
	// ErrRunFinished: the run reached its terminal state and accepts no
	// answer at all, not even an idempotent replay of an already
	// accepted one — the same precedence run_graph gives RUN_TERMINAL.
	// A run holding a pending revision inquiry is not terminal: the open
	// question is unfinished business (RequestRevision, revision.go).
	// Contrast ErrDecisionStale (the request itself is invalid) and
	// ErrDecisionConflict (the request exists but was answered
	// otherwise).
	ErrRunFinished = errors.New("runner: finished run does not accept a decision answer")
	// ErrDecisionStale: no decision row with this id exists in this run —
	// an old id from an earlier run or a request that never existed — or
	// the row belongs to a generation its task has since left: a revision
	// invalidated the request (§6.1), and the old answer authorizes
	// nothing of the new generation's work.  Revision inquiries live
	// below every task generation and are the one exception: linked to a
	// live (pending or granted) request they are answerable, not stale.
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

// validateConfirmation rejects the three malformed gate declarations on a
// task spec (the TaskSpec adapter of validateConfirmationBody).
func validateConfirmation(sp TaskSpec) error {
	return validateConfirmationBody(sp.TaskID, sp.Confirm)
}

// validateConfirmationBody rejects the three malformed confirmation
// declarations that, once persisted, could never be repaired through the
// answer path: no options (no answer would ever be legal), no allowing
// choice (no answer could ever authorize execution) and an allowing
// choice outside the options (a choice that cannot be answered would have
// to authorize execution).  CreateRun and the revision paths call this
// for every declaration BEFORE writing anything, so a refusal leaves the
// directory (or the revision) empty for the corrected attempt — a
// malformed declaration must not become durable state.
func validateConfirmationBody(taskID string, c *Confirmation) error {
	if c == nil {
		return nil
	}
	switch {
	case len(c.Options) == 0:
		return fmt.Errorf("runner: task %q declares a confirmation without options; a question with no choices could never be answered", taskID)
	case c.Allow == "":
		return fmt.Errorf("runner: task %q declares a confirmation without an allowing choice; no answer could ever authorize its execution", taskID)
	case !slices.Contains(c.Options, c.Allow):
		return fmt.Errorf("runner: task %q declares allowing choice %q outside its confirmation options %v", taskID, c.Allow, c.Options)
	}
	return nil
}

// materializeDecisions writes every declared gate as a pending decisions
// row plus its decision_created event, inside the run's CREATION
// transaction — the gate exists exactly when the run does, so "this task
// needs a user decision" survives even an owner that dies before the
// first Run (§13.2).  generation matches the generation CreateRun gives
// the task itself (1); a revision-bumped task gets its own new gate from
// ReviseTask (revision.go).  Called only from CreateRun; Answer and the
// run loop never write gates.
func materializeDecisions(ctx context.Context, tx *sql.Tx, runID string, specs []TaskSpec) error {
	for _, sp := range specs {
		if sp.Confirm == nil {
			continue
		}
		if _, err := insertDecision(ctx, tx, runID, sp.TaskID, 1, sp.Confirm); err != nil {
			return err
		}
	}
	return nil
}

// insertDecision writes one pending decisions row at generation plus its
// decision_created event, inside the caller's transaction, and returns the
// minted decision id — the single write path for gates, shared by
// CreateRun's materialization (above), ReviseTask's generation carry and
// RequestRevision's inquiry slot (revision.go).  The row freezes
// revision/body/options/allow exactly as passed.
func insertDecision(ctx context.Context, tx *sql.Tx, runID, taskID string, generation int64, c *Confirmation) (string, error) {
	id, err := newID128()
	if err != nil {
		return "", err
	}
	optionsJSON, err := json.Marshal(c.Options)
	if err != nil {
		return "", err
	}
	eventJSON, err := json.Marshal(decisionEvent{
		DecisionID: id,
		TaskID:     taskID,
		Generation: generation,
		Revision:   c.Revision,
		Options:    c.Options,
		Allow:      c.Allow,
	})
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO decisions
			(decision_id, run_id, task_id, generation, revision, body,
			 body_digest, options_json, allow, created_at, state)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		id, runID, taskID, generation, c.Revision, string(c.Body),
		inputDigest(c.Body), string(optionsJSON), c.Allow, nowUTC(), decisionPending); err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx,
		"INSERT INTO events (run_id, kind, payload_json) VALUES (?,?,?)",
		runID, eventKindDecisionCreated, eventJSON)
	return id, err
}

// decisionStatus reports whether the run holds any still-pending gate and
// any answered-without-allowing one — counting only rows at their task's
// CURRENT generation: a revision leaves old-generation rows behind as
// history, and a stale row must neither hold the drained loop in a
// permanent wait nor refuse a revised run.  One deliberate exception on
// the pending side: a pending revision inquiry (a non-positive generation
// slot linked to a pending request, revision.go) counts as waiting — the
// allowance question is an open gate for the run.  A REFUSED inquiry
// never counts as refusal: declining a revision is not the run refusing
// (ErrDecisionRefused stays about current-generation task gates).  The
// ready query's generation-matched gate predicate (dispatchableTasks in
// runner.go) agrees by construction — inquiry slots never match a task
// generation.  The drained run loop reports these as its reason for
// stopping (settle in runner.go); reading the durable rows rather than
// this Run's visit history keeps the answer identical for every owner.
func (s *Store) decisionStatus(ctx context.Context) (waiting, refused bool, err error) {
	var pending, refusedCount int
	err = s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM decisions d
			  WHERE d.run_id=? AND d.state=?
			    AND (EXISTS (SELECT 1 FROM tasks t
			                  WHERE t.run_id=d.run_id AND t.task_id=d.task_id
			                    AND t.generation=d.generation)
			         OR (d.generation<=0 AND EXISTS (SELECT 1 FROM revision_requests rr
			                  WHERE rr.run_id=d.run_id AND rr.decision_id=d.decision_id
			                    AND rr.state='pending')))),
			(SELECT COUNT(*) FROM decisions d
			  WHERE d.run_id=? AND d.state=? AND d.answer<>d.allow
			    AND EXISTS (SELECT 1 FROM tasks t
			                 WHERE t.run_id=d.run_id AND t.task_id=d.task_id
			                   AND t.generation=d.generation))`,
		s.runID, decisionPending, s.runID, decisionAnswered).
		Scan(&pending, &refusedCount)
	return pending > 0, refusedCount > 0, err
}

// PendingDecisions lists this run's unanswered decisions, ordered by task
// id for a stable display.  The same current-generation rule as Answer
// and decisionStatus applies: a revision invalidates an old question, so
// it is history an answer can no longer reach — answering it is
// ErrDecisionStale and it must not be presented as askable.  Live
// revision inquiries (non-positive generation slots linked to pending
// requests) are listed alongside current-generation gates.  Answered
// decisions are history, not open questions, and never appear.
func (s *Store) PendingDecisions(ctx context.Context) ([]DecisionView, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT decision_id, task_id, revision, body, options_json
		  FROM decisions d
		 WHERE d.run_id=? AND d.state=?
		   AND (EXISTS (SELECT 1 FROM tasks t
		                 WHERE t.run_id=d.run_id AND t.task_id=d.task_id
		                   AND t.generation=d.generation)
		        OR (d.generation<=0 AND EXISTS (SELECT 1 FROM revision_requests rr
		                 WHERE rr.run_id=d.run_id AND rr.decision_id=d.decision_id
		                   AND rr.state='pending')))
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
//  1. a terminal run (all its tasks finished, no pending revision
//     request — an open inquiry is unfinished business) refuses
//     everything, even the idempotent replay of an already accepted
//     answer — mirroring the precedence run_graph gives RUN_TERMINAL;
//  2. an id matching no row of THIS run is stale (an old id, another
//     run's id, or a request that never existed); no "there is only one
//     open question" inference ever substitutes a different target;
//  3. a row whose generation no longer equals its task's current one is
//     refused as stale the same way — a revision invalidated the request
//     (§6.1 binds it to the local generation), so the old row, pending
//     or answered, authorizes nothing of the new generation's work; the
//     one exception is a revision inquiry (revision.go): its
//     non-positive slot never equals any task generation by design, and
//     linked to a live pending or granted request it is answerable;
//  4. an already-answered decision replays exactly: the same choice
//     returns the original receipt with zero new transitions and
//     events, a different choice is a conflict that leaves the stored
//     answer standing;
//  5. a pending decision accepts only one of its stored options — any
//     other choice is refused descriptively with the row still pending —
//     and a valid choice is consumed exactly once: state, answer,
//     answered_at and the decision_answered event commit together.  When
//     the row is a revision inquiry, its request settles in the same
//     transaction: the allowing choice grants the revision (the caller's
//     next RequestRevision call executes it), any other choice refuses
//     it and the task is untouched.
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
	// request, so it outranks even the idempotent replay below.  A
	// pending revision inquiry keeps the run answerable — the open
	// question is itself unfinished business.
	var unfinished int
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tasks WHERE run_id=? AND state<>?",
		s.runID, taskFinished).Scan(&unfinished); err != nil {
		return AnswerReceipt{}, err
	}
	if unfinished == 0 {
		var inquiries int
		if err := tx.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM revision_requests WHERE run_id=? AND state=?",
			s.runID, reqPending).Scan(&inquiries); err != nil {
			return AnswerReceipt{}, err
		}
		if inquiries == 0 {
			return AnswerReceipt{}, ErrRunFinished
		}
	}
	// ② The request must exist in this run.
	var (
		taskID, state, optionsJSON, allow string
		generation                        int64
		answer, answeredAt                sql.NullString
	)
	err = tx.QueryRowContext(ctx, `
		SELECT task_id, generation, state, options_json, answer, answered_at, allow
		  FROM decisions
		 WHERE decision_id=? AND run_id=?`, decisionID, s.runID).
		Scan(&taskID, &generation, &state, &optionsJSON, &answer, &answeredAt, &allow)
	if errors.Is(err, sql.ErrNoRows) {
		return AnswerReceipt{}, ErrDecisionStale
	}
	if err != nil {
		return AnswerReceipt{}, err
	}
	// ③ The request must still belong to the task's current generation:
	// a revision (§6.1) invalidated anything older, answered or not —
	// except a revision inquiry, which never matches a task generation
	// and is answerable while its request is live.
	var current int64
	if err := tx.QueryRowContext(ctx,
		"SELECT generation FROM tasks WHERE run_id=? AND task_id=?",
		s.runID, taskID).Scan(&current); err != nil {
		return AnswerReceipt{}, err
	}
	if generation != current {
		var linked int
		if err := tx.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM revision_requests WHERE run_id=? AND decision_id=? AND state IN (?,?)",
			s.runID, decisionID, reqPending, reqGranted).Scan(&linked); err != nil {
			return AnswerReceipt{}, err
		}
		if linked == 0 {
			return AnswerReceipt{}, ErrDecisionStale
		}
	}
	if state == decisionAnswered {
		// ④ Same choice replays the original receipt; a different one is
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
	// ⑤ Pending (the CHECK constraint admits only pending and answered):
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
	// A revision inquiry's answer settles its request in the same
	// transaction: the allowing choice grants the revision (the caller's
	// next RequestRevision call executes it); any other choice refuses it.
	reqState := reqRefused
	if choice == allow {
		reqState = reqGranted
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE revision_requests SET state=?, resolved_at=? WHERE run_id=? AND decision_id=? AND state=?",
		reqState, now, s.runID, decisionID, reqPending); err != nil {
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
