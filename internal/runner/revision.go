package runner

// K4 — local generations and revision scoping (plan §4.2, §9.2, §13.2):
// Store.ReviseTask is the explicit primitive for an input or business-scope
// change to one task.  It bumps the task's local generation, invalidates
// exactly the task plus its transitive dependents (§117: one branch's
// repair never invalidates sibling branches; a join must hold the current
// results of all its required inputs), and leaves every older row in place
// as history (§4.2: original invocation records are never overwritten — a
// late old-generation result is recorded on its superseded row only).

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
// under a live loop would strand its in-flight admission.  Whether a
// revision is allowed at all — §6.1 confirmation, repair-allowance
// counting — is the caller's domain; the kernel provides the generation
// facts and nothing else.
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
	// The target must exist; on refusal nothing is written.
	var old int64
	err = tx.QueryRowContext(ctx,
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
	return tx.Commit()
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
	// target; otherwise the task's own latest-generation frozen gate.
	if taskID == targetID && spec.Confirm != nil {
		return insertDecision(ctx, tx, runID, taskID, old+1, spec.Confirm)
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
	return insertDecision(ctx, tx, runID, taskID, old+1, &Confirmation{
		Options:  options,
		Allow:    allow,
		Revision: revision,
		Body:     []byte(body),
	})
}
