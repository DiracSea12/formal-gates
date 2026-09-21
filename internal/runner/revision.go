package runner

// K4 — local generations and revision scoping (plan §4.2, §9.2, §13.2):
// Store.ReviseTask is the explicit primitive for an input or business-scope
// change to one task.  It bumps the task's local generation, invalidates
// exactly the task plus its transitive dependents (§117: one branch's
// repair never invalidates sibling branches; a join must hold the current
// results of all its required inputs), and leaves every older row in place
// as history (§4.2: original invocation records are never overwritten — a
// late old-generation result is recorded on its superseded row only).
//
// The K4 repair adds the repair-allowance control chain over the
// primitive: Store.RequestRevision (below) turns "ask at the allowance,
// wait durably, execute only what the answer granted" into API.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// eventKindTaskRevised is appended once per bumped task, in the same
// transaction as the bump it describes.
const eventKindTaskRevised = "task_revised"

// taskRevisedEvent is the events payload of one generation bump.
type taskRevisedEvent struct {
	TaskID        string `json:"task_id"`
	OldGeneration int64  `json:"old_generation"`
	NewGeneration int64  `json:"new_generation"`
}

// ReviseTask revises one task's input or business scope (§4.2 Generation)
// between two Runs.  The invalidation scope is the task plus its transitive
// dependents — nothing else: sibling branches keep their finished results
// and generations (§117), and a join task's new generation waits, through
// the ordinary dependency gate, until every upstream current-generation
// result exists.  In one transaction, every task of that scope:
//
//   - bumps its generation by one, returns to waiting_deps and clears its
//     effective result — a revision never produces a success terminal by
//     itself; the task walks gates, execution and admission again;
//   - keeps every older row as history: old-generation pending intents
//     turn superseded (an invalidated session's in-flight work is void; a
//     late result is recorded on the superseded row, never effective),
//     and a task_revised event lands with the bump;
//   - rebinds tasks.input_digest to spec.Input for the TARGET task only —
//     dependents keep their inputs, their business scope changed;
//   - carries its gate to the new generation (§6.1 binds each request to
//     the local generation, so an old answer authorizes nothing): the
//     target uses spec.Confirm when declared, every other task copies the
//     frozen definition of its latest-generation decisions row; with
//     neither there is no gate.
//
// ReviseTask joins the same advance guard as Run and Close: a Run
// mid-advance is refused with ErrAdvancing, because bumping a generation
// under a live loop would strand its in-flight admission.  ReviseTask
// stays the bare primitive: whether a revision is allowed at all is the
// caller's domain, and the repair-allowance control chain built over it
// is Store.RequestRevision (below).
func (s *Store) ReviseTask(ctx context.Context, taskID string, spec TaskSpec) error {
	if !s.advance.TryLock() {
		return ErrAdvancing
	}
	defer s.advance.Unlock()
	// Before the write transaction, CreateRun's zero-residue rule: a
	// malformed gate declaration must not become durable state.
	if err := validateConfirmation(spec); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit
	if err := s.reviseScope(ctx, tx, taskID, spec); err != nil {
		return err
	}
	return tx.Commit()
}

// reviseScope is the whole revision write inside the caller's transaction
// (see ReviseTask for the contract): the target must exist — a named
// refusal that rolls back everything — and every task of the
// invalidation scope bumps, resets, rebinds and re-gates.  Shared by
// ReviseTask and RequestRevision's approval paths.
func (s *Store) reviseScope(ctx context.Context, tx *sql.Tx, taskID string, spec TaskSpec) error {
	// The target must exist; on refusal nothing is written.
	var old int64
	err := tx.QueryRowContext(ctx,
		"SELECT generation FROM tasks WHERE run_id=? AND task_id=?",
		s.runID, taskID).Scan(&old)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("runner: no task %q in run %q", taskID, s.runID)
	}
	if err != nil {
		return err
	}
	// Deterministic order (task id) keeps the event sequence reproducible.
	scope, err := dependentsClosure(ctx, tx, s.runID, taskID)
	if err != nil {
		return err
	}
	for _, id := range scope {
		if err := reviseOneTask(ctx, tx, s.runID, taskID, id, spec); err != nil {
			return err
		}
	}
	return nil
}

// dependentsClosure returns taskID plus every task transitively depending
// on it, sorted by task id — the revision's invalidation scope.  One read
// of the run's edges; the closure itself is a BFS over their reverse
// direction, in memory.
func dependentsClosure(ctx context.Context, tx *sql.Tx, runID, taskID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx,
		"SELECT task_id, depends_on FROM task_deps WHERE run_id=?", runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	dependents := map[string][]string{}
	for rows.Next() {
		var task, dep string
		if err := rows.Scan(&task, &dep); err != nil {
			return nil, err
		}
		dependents[dep] = append(dependents[dep], task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	closure := []string{taskID}
	seen := map[string]bool{taskID: true}
	for i := 0; i < len(closure); i++ { // BFS on the growing slice
		for _, next := range dependents[closure[i]] {
			if !seen[next] {
				seen[next] = true
				closure = append(closure, next)
			}
		}
	}
	slices.Sort(closure)
	return closure, nil
}

// reviseOneTask bumps one task of the revision scope inside the caller's
// transaction (see ReviseTask for the contract).
func reviseOneTask(ctx context.Context, tx *sql.Tx, runID, targetID, taskID string, spec TaskSpec) error {
	var old int64
	if err := tx.QueryRowContext(ctx,
		"SELECT generation FROM tasks WHERE run_id=? AND task_id=?",
		runID, taskID).Scan(&old); err != nil {
		return err
	}
	var err error
	if taskID == targetID {
		_, err = tx.ExecContext(ctx, `
			UPDATE tasks
			   SET generation=?, state=?, result_invocation_id=NULL, input_digest=?
			 WHERE run_id=? AND task_id=?`,
			old+1, taskWaitingDeps, inputDigest(spec.Input), runID, taskID)
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE tasks
			   SET generation=?, state=?, result_invocation_id=NULL
			 WHERE run_id=? AND task_id=?`,
			old+1, taskWaitingDeps, runID, taskID)
	}
	if err != nil {
		return err
	}
	// Old-generation intents are void — in flight or without receipt,
	// their rows stay for the late-result path (§4.2).
	if _, err := tx.ExecContext(ctx,
		"UPDATE invocations SET state=? WHERE run_id=? AND task_id=? AND generation=? AND state=?",
		invSuperseded, runID, taskID, old, invPending); err != nil {
		return err
	}
	eventJSON, err := json.Marshal(taskRevisedEvent{
		TaskID:        taskID,
		OldGeneration: old,
		NewGeneration: old + 1,
	})
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO events (run_id, kind, payload_json) VALUES (?,?,?)",
		runID, eventKindTaskRevised, eventJSON); err != nil {
		return err
	}
	// Gate carry to the new generation: a newly declared Confirm on the
	// target; otherwise the task's own latest-generation frozen gate —
	// task generations only (>=1), never a revision inquiry's
	// non-positive slot, which is not this task's gate.
	if taskID == targetID && spec.Confirm != nil {
		_, err := insertDecision(ctx, tx, runID, taskID, old+1, spec.Confirm)
		return err
	}
	var revision, body, optionsJSON, allow string
	err = tx.QueryRowContext(ctx, `
		SELECT revision, body, options_json, allow
		  FROM decisions
		 WHERE run_id=? AND task_id=? AND generation>0
		 ORDER BY generation DESC LIMIT 1`,
		runID, taskID).Scan(&revision, &body, &optionsJSON, &allow)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // never gated: the new generation is ungated too
	}
	if err != nil {
		return err
	}
	var options []string
	if err := json.Unmarshal([]byte(optionsJSON), &options); err != nil {
		return err
	}
	_, err = insertDecision(ctx, tx, runID, taskID, old+1, &Confirmation{
		Options:  options,
		Allow:    allow,
		Revision: revision,
		Body:     []byte(body),
	})
	return err
}

// Request states for revision_requests; a request is born pending
// (waiting for the user) or executed (direct approval below the
// allowance) and resolves at most once — granted by an allowing answer,
// then executed by the completion call, or refused by a declining one.
// Rows are never deleted: every count the policy needs is derived from
// them.
const (
	reqPending  = "pending"
	reqGranted  = "granted"
	reqExecuted = "executed"
	reqRefused  = "refused"
)

// ErrRevisionPending: the task already holds an unresolved revision
// request — one open inquiry per task.  Answer it (or complete a granted
// one with the next RequestRevision call) before requesting another.
var ErrRevisionPending = errors.New("runner: task already has an unresolved revision request")

// RepairPolicy is the narrow domain seam of the repair allowance: given
// the business round this revision would be (used+1) and the effective
// allowance (the caller's initial plus every user grant so far), it
// reports whether the user must be asked (true) or the revision may be
// approved directly (false).  The kernel hosts no allowance rule of its
// own — K5 wires domain.repairAllowance's semantics here (round >=
// allowance asks; a null allowance takes the spec's repair limit; raising
// the allowance is a flow decision made by passing a larger initial).
type RepairPolicy func(round, allowance int) bool

// RequestRevision is the executable control chain over ReviseTask: one
// business revision of taskID, gated by the repair allowance.  exhaustion
// is the question asked when the allowance is reached; policy is the
// domain's ask-or-approve rule; initialAllowance is re-sent every call —
// like Run's specs — so increments arrive either as user grants or as a
// larger initial, and nothing but request rows is persisted.
//
// State machine (every count derived from revision_requests rows):
//
//   - an existing pending request is a named refusal, ErrRevisionPending
//     (one open inquiry per task);
//   - an existing granted request is COMPLETED by this call: the revision
//     (reviseScope — ReviseTask's transaction body) and the request's move
//     to executed land in one transaction.  The new input travels in this
//     call because input bytes are never persisted (§7): the grant itself
//     executes nothing;
//   - otherwise policy(used+1, initial+grants) decides.  false approves
//     directly — one transaction writes the request as executed
//     (decision_id NULL: no question was asked) and executes the revision.
//     true asks — one transaction writes the request as pending plus a
//     decision row in the task's non-positive generation slot and returns
//     ErrWaitingDecision.  An inquiry slot never matches a task
//     generation (tasks start at 1), so it gates nothing by itself; each
//     new inquiry takes the next free slot (0, -1, -2, ...) because the
//     decisions UNIQUE (run, task, generation) keeps answered inquiries
//     as history.  Store.Answer settles the inquiry in the same
//     transaction that consumes the decision: the allowing choice grants
//     the request (this call then completes it), any other choice
//     refuses it and the task is untouched.
//
// RequestRevision joins the same advance guard as Run and ReviseTask.
func (s *Store) RequestRevision(ctx context.Context, taskID string, spec TaskSpec, exhaustion *Confirmation, policy RepairPolicy, initialAllowance int) error {
	if !s.advance.TryLock() {
		return ErrAdvancing
	}
	defer s.advance.Unlock()
	// ReviseTask's before-the-write rules, extended to the exhaustion
	// question: a malformed declaration must not become durable state.
	if err := validateConfirmation(spec); err != nil {
		return err
	}
	if err := validateConfirmationBody(taskID, exhaustion); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit
	var pendingReqs, grantedReqs, used, grants int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN state='pending'  THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN state='granted'  THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN state='executed' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN decision_id IS NOT NULL
		                          AND state IN ('granted','executed') THEN 1 ELSE 0 END), 0)
		  FROM revision_requests
		 WHERE run_id=? AND task_id=?`,
		s.runID, taskID).Scan(&pendingReqs, &grantedReqs, &used, &grants); err != nil {
		return err
	}
	switch {
	case pendingReqs > 0:
		return fmt.Errorf("%w: task %q in run %q", ErrRevisionPending, taskID, s.runID)
	case grantedReqs > 0:
		// The completion call: the user already allowed this revision.
		if err := s.reviseScope(ctx, tx, taskID, spec); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE revision_requests SET state=?, resolved_at=? WHERE run_id=? AND task_id=? AND state=?",
			reqExecuted, nowUTC(), s.runID, taskID, reqGranted); err != nil {
			return err
		}
		return tx.Commit()
	}
	if !policy(used+1, initialAllowance+grants) {
		// Direct approval below the allowance: no question, one row.
		id, err := newID128()
		if err != nil {
			return err
		}
		now := nowUTC()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO revision_requests
				(request_id, run_id, task_id, state, decision_id, created_at, resolved_at)
			VALUES (?,?,?,?,?,?,?)`,
			id, s.runID, taskID, reqExecuted, nil, now, now); err != nil {
			return err
		}
		if err := s.reviseScope(ctx, tx, taskID, spec); err != nil {
			return err
		}
		return tx.Commit()
	}
	// The allowance is reached: ask durably before revising anything.
	if exhaustion == nil {
		return fmt.Errorf("runner: task %q reached its repair allowance but no exhaustion confirmation was supplied to ask the user", taskID)
	}
	// The next free inquiry slot below every task generation.
	var slot int64
	if err := tx.QueryRowContext(ctx,
		"SELECT COALESCE(MIN(generation), 1) FROM decisions WHERE run_id=? AND task_id=? AND generation<=0",
		s.runID, taskID).Scan(&slot); err != nil {
		return err
	}
	slot--
	reqID, err := newID128()
	if err != nil {
		return err
	}
	decisionID, err := insertDecision(ctx, tx, s.runID, taskID, slot, exhaustion)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO revision_requests
			(request_id, run_id, task_id, state, decision_id, created_at)
		VALUES (?,?,?,?,?,?)`,
		reqID, s.runID, taskID, reqPending, decisionID, nowUTC()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return ErrWaitingDecision
}
