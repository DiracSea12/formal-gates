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
// The K4 rework builds the repair-allowance control chain over the
// primitive as one family of user_requests (store.go): Store.RequestRevision
// (below) turns "ask at the allowance, wait durably, execute only what the
// answer granted" into API, with the initial allowance durably bound on
// the request rows and every count derived from them.

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
		if err == nil {
			// The byte carrier behind the rebind above: the input the new
			// generation is bound to persists in the same transaction as
			// the bump (task_revisions, store.go), so a reopened owner
			// re-sends exactly these bytes instead of re-deriving them
			// from a second write that a crash could strand.  Dependents
			// keep their inputs and write no row — that asymmetry is what
			// lets readers tell "this task was itself revised" from "an
			// upstream closure bumped it".
			_, err = tx.ExecContext(ctx, `
				INSERT INTO task_revisions (run_id, task_id, generation, input)
				VALUES (?,?,?,?)`,
				runID, taskID, old+1, spec.Input)
		}
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
	// target, otherwise the task's own latest-generation frozen gate.
	if taskID == targetID && spec.Confirm != nil {
		_, err := insertDecision(ctx, tx, runID, taskID, old+1, spec.Confirm)
		return err
	}
	var revision, body, optionsJSON, allow string
	err = tx.QueryRowContext(ctx, `
		SELECT revision, body, options_json, allow
		  FROM decisions
		 WHERE run_id=? AND task_id=?
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

// Request kinds and states of the user_requests table — the CHECK
// values of its DDL.  The table's DDL comment (store.go) is the single
// authoritative statement of the request lifecycle and its binding
// rule; requestActiveSQL below is the one composed form of that rule
// every reader shares.
const (
	kindRevision = "revision"
	kindRestart  = "restart"
)

// Request states; see the block above.
const (
	reqPending  = "pending"
	reqGranted  = "granted"
	reqExecuted = "executed"
	reqRefused  = "refused"
)

// requestActiveSQL is THE single definition of an active request — the
// one composed form of the user_requests binding rule (the table's DDL
// comment in store.go is the authoritative statement): a pending or
// granted row whose task still sits at the row's target_generation and,
// while a restart is unresolved, whose invocation is still pending.
// Every reader that must treat dead history as dead composes this
// fragment against the row alias r — RequestRevision's open-inquiry and
// completion checks and its grants count, OpenRequests,
// PendingDecisions, requestStatus, Answer's terminal exemption, and
// recovery's nil-consult supersede exclusion (runner.go) — no site
// hand-writes the rule.  A row that fails it authorizes nothing,
// blocks nothing and is listed nowhere.
const requestActiveSQL = `r.state IN ('` + reqPending + `','` + reqGranted + `')
	AND EXISTS (SELECT 1 FROM tasks t
	            WHERE t.run_id = r.run_id AND t.task_id = r.task_id
	              AND t.generation = r.target_generation)
	AND (r.kind <> '` + kindRestart + `' OR EXISTS (SELECT 1 FROM invocations i
	            WHERE i.invocation_id = r.invocation_id AND i.state = '` + invPending + `'))`

// Event kinds for the request transitions that carry one; each is
// appended in the same transaction as the transition it describes (§7
// events).  Direct approval is born executed and transitions nothing, so
// it writes no event.
const (
	eventKindRequestCreated  = "request_created"
	eventKindRequestAnswered = "request_answered"
)

// requestEvent is the events payload of both request transitions:
// creation carries the question's identity (request, kind, task, target
// generation), the answer carries the chosen choice.
type requestEvent struct {
	RequestID        string `json:"request_id"`
	Kind             string `json:"kind"`
	TaskID           string `json:"task_id"`
	TargetGeneration int64  `json:"target_generation"`
	Choice           string `json:"choice,omitempty"`
}

// ErrRevisionPending: the task already holds an unresolved revision
// request — one open inquiry per task.  Answer it (or complete a granted
// one with the next RequestRevision call) before requesting another.
var ErrRevisionPending = errors.New("runner: task already has an unresolved revision request")

// ErrAllowanceDrift: RequestRevision was called with an initial allowance
// different from the one the task's request rows already bound.  Unlike
// Run's inputs, the initial allowance has no digest to re-verify per
// call, so the FIRST use binds it durably and every later call must
// re-send the bound value — raising the allowance is a user decision
// (a grant), never a recovery argument.
var ErrAllowanceDrift = errors.New("runner: repair allowance drifts: increases go through the user, not the recovery argument")

// ErrRevisionGrantStale: raised only by the live-grant completion path's
// invariant guard; the selection is already requestActiveSQL-filtered
// (target_generation = the current generation), so the guard only names
// intra-transaction drift.  A grant whose binding broke is dead history in
// ANY task state — refuses, raises or authorizes nothing (RequestRevision).
var ErrRevisionGrantStale = errors.New("runner: the granted revision no longer matches the task's current generation; the old authorization must not act on the new scope")

// ErrRevisionGranted: the drained loop found a revision the user allowed
// but nobody executed yet.  The grant executes nothing by itself — the
// revised input reaches execution only through the completion call, so
// the caller re-sends it through RequestRevision, and only then may the
// run reach a terminal state.  Store.OpenRequests surfaces the request's
// identity to a recovering owner.
var ErrRevisionGranted = errors.New("runner: a granted revision awaits its completion call (re-send the revised input through RequestRevision)")

// RepairPolicy is the narrow domain seam of the repair allowance: given
// the business rounds this task has ALREADY consumed (used, 0-based — the
// same completed-rounds number the accepted graph.py repair() hands its
// repair-allowance) and the effective allowance (the bound initial plus
// every user grant so far), it reports whether the user must be asked
// (true) or the revision may be approved directly (false).  The kernel
// hosts no allowance rule of its own — K5 wires
// domain.repairAllowance's semantics here (round >= allowance asks; a
// null allowance takes the spec's repair limit; raising the allowance is
// a flow decision made through a user grant).
type RepairPolicy func(round, allowance int) bool

// TaskGeneration is the kernel's authoritative read of one task's current
// local generation — the counter every revision of the task's scope bumps
// (its own revision, or an upstream's dependent closure carrying it).
// Readers use it to decide whether a durable fact recorded at an older
// generation still describes the CURRENT scope: a fact from a superseded
// generation is history and must not drive new transitions, however
// fresh the file that carries it looks.
func (s *Store) TaskGeneration(ctx context.Context, taskID string) (int64, error) {
	var generation int64
	err := s.db.QueryRowContext(ctx,
		"SELECT generation FROM tasks WHERE run_id=? AND task_id=?",
		s.runID, taskID).Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("runner: no task %q in run %q", taskID, s.runID)
	}
	return generation, err
}

// RevisionInput is one persisted task-revision row (task_revisions,
// store.go): the input bytes a revision bound, at the generation the
// bump gave the task.
type RevisionInput struct {
	TaskID     string
	Generation int64
	Input      []byte
}

// RevisionInputs lists this run's persisted revision inputs, ordered by
// (task, generation) — the byte carrier behind tasks.input_digest.  The
// LAST row of a task names the bytes its current generation is bound
// to: a dependent bumped by an upstream closure keeps its input and
// writes no row, so its earlier row still holds the live bytes.  Only
// revision targets ever appear (creation inputs are re-derived from the
// run's creation binding), so a task's rows also answer "was this task
// itself revised".
func (s *Store) RevisionInputs(ctx context.Context) ([]RevisionInput, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT task_id, generation, input
		  FROM task_revisions
		 WHERE run_id=?
		 ORDER BY task_id, generation`, s.runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RevisionInput
	for rows.Next() {
		var row RevisionInput
		if err := rows.Scan(&row.TaskID, &row.Generation, &row.Input); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// RequestRevision is the executable control chain over ReviseTask: one
// business revision of taskID, gated by the repair allowance.
// exhaustion is the question asked when the allowance is reached;
// policy is the domain's ask-or-approve rule; initialAllowance binds on
// first use and must be re-sent unchanged on every later call
// (ErrAllowanceDrift otherwise — increases arrive as user grants,
// never as a larger argument).  The request rows and their whole
// lifecycle are the user_requests table's (store.go — the single
// authoritative statement); what this call itself does:
//
//   - the task must exist in this run — a named refusal before ANY
//     branch writes, so a mistyped id leaves zero residue;
//   - a task holding an ACTIVE pending request is refused with
//     ErrRevisionPending (one open inquiry per task; requestActiveSQL
//     decides activity, so a dead request never holds the entry);
//   - a task holding an ACTIVE granted request treats this call as its
//     completion: the revision (ReviseTask's transaction body, input
//     bytes included — task_revisions) and the request's move to
//     executed land in one transaction.  The grant executes nothing by
//     itself; the revised input travels in this completion call.  A
//     granted request whose binding an upstream revision broke is dead
//     history in ANY task state — it blocks no new request, raises no
//     allowance below and authorizes nothing — so the call falls
//     straight through to that allowance verdict;
//   - otherwise policy(used, bound+grants) decides.  used is the count
//     of executed revision rounds, 0-based, so initial=1 permits the
//     first round and asks on the second (the accepted boundary);
//     grants counts the answer-carrying requests that may still act —
//     executed ones, or granted ones with a whole binding — so a dead
//     grant raises nothing.  false approves directly — one transaction
//     writes the request as executed (no question was asked) and
//     executes the revision.  true asks — one transaction writes the
//     request as pending, bound to the task's CURRENT generation, plus
//     a request_created event, and returns ErrWaitingDecision;
//     Store.Answer settles it.
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
	// The target must exist before ANY branch's write — a mistyped id
	// must not persist an unanswerable question.
	var generation int64
	err := s.db.QueryRowContext(ctx,
		"SELECT generation FROM tasks WHERE run_id=? AND task_id=?",
		s.runID, taskID).Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("runner: no task %q in run %q", taskID, s.runID)
	}
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit
	// The initial allowance binds on first use; every later call must
	// re-send the bound value.  All revision rows carry it, so one
	// aggregate read both checks the drift and yields the bound value.
	var rows int
	var bound sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*), MIN(initial)
		  FROM user_requests
		 WHERE run_id=? AND task_id=? AND kind=?`,
		s.runID, taskID, kindRevision).Scan(&rows, &bound); err != nil {
		return err
	}
	if rows > 0 {
		if !bound.Valid || bound.Int64 != int64(initialAllowance) {
			return fmt.Errorf("%w: task %q in run %q bound %d, passed %d",
				ErrAllowanceDrift, taskID, s.runID, bound.Int64, initialAllowance)
		}
	} else {
		bound.Int64, bound.Valid = int64(initialAllowance), true
	}
	// One open inquiry per task, and the completion call for a granted
	// one — both see ACTIVE requests only (requestActiveSQL): a request
	// whose binding an upstream revision broke is dead history and must
	// not hold the task's repair entry.
	var pendingReqs, grantedReqs int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN r.state=? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN r.state=? THEN 1 ELSE 0 END), 0)
		  FROM user_requests r
		 WHERE r.run_id=? AND r.task_id=? AND r.kind=? AND `+requestActiveSQL,
		reqPending, reqGranted, s.runID, taskID, kindRevision).Scan(&pendingReqs, &grantedReqs); err != nil {
		return err
	}
	if pendingReqs > 0 {
		return fmt.Errorf("%w: task %q in run %q", ErrRevisionPending, taskID, s.runID)
	}
	if grantedReqs > 0 {
		// The completion call: the user already allowed this revision —
		// and only for the scope it was granted for (the selection is
		// activity-filtered, so the guard is an invariant, not a path).
		var grantedID string
		var target int64
		if err := tx.QueryRowContext(ctx, `
			SELECT r.request_id, r.target_generation
			  FROM user_requests r
			 WHERE r.run_id=? AND r.task_id=? AND r.kind=? AND r.state=? AND `+requestActiveSQL+`
			 LIMIT 1`,
			s.runID, taskID, kindRevision, reqGranted).Scan(&grantedID, &target); err != nil {
			return err
		}
		if generation != target {
			return fmt.Errorf("%w: task %q in run %q granted at generation %d, now %d",
				ErrRevisionGrantStale, taskID, s.runID, target, generation)
		}
		if err := s.reviseScope(ctx, tx, taskID, spec); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE user_requests SET state=?, resolved_at=? WHERE request_id=? AND state=?",
			reqExecuted, nowUTC(), grantedID, reqGranted); err != nil {
			return err
		}
		return tx.Commit()
	}
	// The allowance verdict: used is 0-based executed rounds, grants are
	// the user's answered increases that may still act (a dead grant
	// never counted), allowance is the bound initial.
	var used, grants int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN r.state=? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN r.answer IS NOT NULL
		                          AND (r.state=? OR `+requestActiveSQL+`) THEN 1 ELSE 0 END), 0)
		  FROM user_requests r
		 WHERE r.run_id=? AND r.task_id=? AND r.kind=?`,
		reqExecuted, reqExecuted, s.runID, taskID, kindRevision).Scan(&used, &grants); err != nil {
		return err
	}
	if !policy(used, int(bound.Int64)+grants) {
		// Direct approval below the allowance: no question, one row.
		reqID, err := newID128()
		if err != nil {
			return err
		}
		now := nowUTC()
		optionsJSON, allow, revision, body := questionFields(exhaustion)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_requests
				(request_id, run_id, task_id, kind, target_generation, invocation_id,
				 options_json, allow, revision, body, initial,
				 state, answer, answered_at, created_at, resolved_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,NULL,NULL,?,?)`,
			reqID, s.runID, taskID, kindRevision, generation, nil,
			optionsJSON, allow, revision, body, bound.Int64,
			reqExecuted, now, now); err != nil {
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
	if _, err := insertUserRequest(ctx, tx, s.runID, kindRevision,
		taskID, generation, "", exhaustion, &bound.Int64); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return ErrWaitingDecision
}

// questionFields freezes a confirmation onto request-row columns; a nil
// confirmation (direct approval without a question) yields empty fields,
// a row that was never a question.
func questionFields(c *Confirmation) (optionsJSON, allow, revision, body string) {
	if c == nil {
		return "[]", "", "", ""
	}
	b, err := json.Marshal(c.Options)
	if err != nil {
		return "[]", c.Allow, c.Revision, string(c.Body)
	}
	return string(b), c.Allow, c.Revision, string(c.Body)
}

// insertUserRequest writes one pending request row plus its
// request_created event, inside the caller's transaction, and returns
// the minted request id — the single write path for asks, shared by
// RequestRevision and the restart asks recovery creates (takeover.go).
// An empty invocationID is stored NULL (revision asks name no call);
// direct approval writes its own executed row in place.
func insertUserRequest(ctx context.Context, tx *sql.Tx, runID, kind, taskID string, targetGeneration int64, invocationID string, c *Confirmation, initial *int64) (string, error) {
	id, err := newID128()
	if err != nil {
		return "", err
	}
	optionsJSON, allow, revision, body := questionFields(c)
	eventJSON, err := json.Marshal(requestEvent{
		RequestID:        id,
		Kind:             kind,
		TaskID:           taskID,
		TargetGeneration: targetGeneration,
	})
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO user_requests
			(request_id, run_id, task_id, kind, target_generation, invocation_id,
			 options_json, allow, revision, body, initial,
			 state, answer, answered_at, created_at, resolved_at)
		VALUES (?,?,?,?,?,NULLIF(?,''),?,?,?,?,?,?,NULL,NULL,?,NULL)`,
		id, runID, taskID, kind, targetGeneration, invocationID,
		optionsJSON, allow, revision, body, initial,
		reqPending, nowUTC()); err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx,
		"INSERT INTO events (run_id, kind, payload_json) VALUES (?,?,?)",
		runID, eventKindRequestCreated, eventJSON)
	return id, err
}

// RequestView is one open request as shown to a recovering owner: the
// durable identity (request id, kind, task, target generation) and the
// state it was left in.
type RequestView struct {
	RequestID  string
	Kind       string
	TaskID     string
	Generation int64
	State      string
}

// OpenRequests lists this run's ACTIVE requests — pending and granted
// whose binding is still whole (requestActiveSQL) — ordered by creation
// then id for a stable display, so an owner that reopened the directory
// can discover what the previous one was waiting for (or what the user
// approved but nobody executed yet) and act on it through Answer and
// RequestRevision.  A request an upstream revision invalidated is dead
// history: it is never recoverable work, exactly as it is never
// answerable.
func (s *Store) OpenRequests(ctx context.Context) ([]RequestView, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.request_id, r.kind, r.task_id, r.target_generation, r.state
		  FROM user_requests r
		 WHERE r.run_id=? AND `+requestActiveSQL+`
		 ORDER BY r.created_at, r.request_id`,
		s.runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RequestView
	for rows.Next() {
		var v RequestView
		if err := rows.Scan(&v.RequestID, &v.Kind, &v.TaskID, &v.Generation, &v.State); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// requestStatus is the request-side companion of decisionStatus: the
// drained loop's request facts, in settle's order —
//
//   - waiting: an ACTIVE pending request (requestActiveSQL — binding
//     whole, an unresolved restart about a still-pending invocation)
//     holds the run at ErrWaitingDecision;
//   - grantedRevision: an ACTIVE granted-but-unexecuted revision —
//     approved work nobody completed yet (ErrRevisionGranted); one
//     whose binding an upstream revision broke is dead history;
//   - aborted: a refused restart bound to its task's current
//     generation — the user chose abort, and the run must not claim
//     success (ErrCallAborted).
func (s *Store) requestStatus(ctx context.Context) (waiting, grantedRevision, aborted bool, err error) {
	var pending, granted, refused int
	err = s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM user_requests r
			  WHERE r.run_id=? AND r.state=? AND `+requestActiveSQL+`),
			(SELECT COUNT(*) FROM user_requests r
			  WHERE r.run_id=? AND r.kind=? AND r.state=? AND `+requestActiveSQL+`),
			(SELECT COUNT(*) FROM user_requests r
			  WHERE r.run_id=? AND r.kind=? AND r.state=?
			    AND EXISTS (SELECT 1 FROM tasks t
			                WHERE t.run_id=r.run_id AND t.task_id=r.task_id
			                  AND t.generation=r.target_generation))`,
		s.runID, reqPending,
		s.runID, kindRevision, reqGranted,
		s.runID, kindRestart, reqRefused).
		Scan(&pending, &granted, &refused)
	return pending > 0, granted > 0, refused > 0, err
}
