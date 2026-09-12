package protocol

import (
	"fmt"
	"strings"

	"formal-gates/internal/engine/runtime"
)

// ExecutorTarget describes who will do the work, not who owns the Batch. Native
// targets use their captured platform context; external targets are an explicit
// user choice, never an automatic fallback or a claim of native platform support.
type ExecutorTarget struct {
	Kind     string              `json:"kind"`
	Provider string              `json:"provider"`
	Label    string              `json:"label"`
	Context  *HostRuntimeContext `json:"context,omitempty"`
}

type ExecutorHandoff struct {
	To            ExecutorTarget `json:"to"`
	WorkspaceView string         `json:"workspaceView"`
}

type ExecutorHandoffReceipt struct {
	ID                  string          `json:"id"`
	Task                runtime.TaskKey `json:"task"`
	BatchID             string          `json:"batchId"`
	FromAction          string          `json:"fromAction"`
	FromProvider        string          `json:"fromProvider"`
	FromExecutor        *ExecutorTarget `json:"fromExecutor,omitempty"`
	FromIdentity        string          `json:"fromIdentity,omitempty"`
	To                  ExecutorTarget  `json:"to"`
	WorkspaceView       string          `json:"workspaceView"`
	RequirementRevision string          `json:"requirementRevision"`
	Candidate           string          `json:"candidate"`
	Revision            uint64          `json:"revision"`
	DecisionEvent       string          `json:"decisionEvent"`
	Reason              string          `json:"reason"`
}

// validateExecutorHandoff has no process or filesystem side effects. The facade
// captures and rechecks the real workspace and target capability before submit.
func validateExecutorHandoff(s *State, q RegularRequest) error {
	r := s.Regular
	if r == nil || r.Mode != "regular" || r.ChangePending || r.Preparation == nil || r.Plan == nil || r.Terminal != "" {
		return fmt.Errorf("EXECUTOR_HANDOFF: an active regular development batch is required")
	}
	x, ok := s.RegularExecutions[q.Target]
	if !ok || x.Finished || x.Claim.Task.Step != "development-worker" || !currentPreparationClaim(r, x.Claim) {
		return fmt.Errorf("EXECUTOR_HANDOFF: target must be the current unfinished development action")
	}
	if q.Handoff == nil || strings.TrimSpace(q.Handoff.WorkspaceView) == "" {
		return fmt.Errorf("EXECUTOR_HANDOFF: an exact workspace view and executor target are required")
	}
	to := q.Handoff.To
	if strings.TrimSpace(to.Provider) == "" || strings.TrimSpace(to.Label) == "" {
		return fmt.Errorf("EXECUTOR_HANDOFF: provider and actual executor label are required")
	}
	switch to.Kind {
	case "native":
		if to.Context == nil || to.Context.Provider != to.Provider || to.Context.Executable == "" {
			return fmt.Errorf("EXECUTOR_HANDOFF: native capability must be captured by its platform adapter")
		}
	case "external":
		if to.Context != nil {
			return fmt.Errorf("EXECUTOR_HANDOFF: external execution must not claim a native runtime context")
		}
	default:
		return fmt.Errorf("EXECUTOR_HANDOFF: executor kind must be native or external")
	}
	if r.Preparation.Operation != nil {
		return fmt.Errorf("EXECUTOR_HANDOFF: reconcile the current Git operation first")
	}
	// Every observed side effect stays with its old execution. A stopped owner
	// may be handed off without inventing a failed model call or erasing a result.
	for _, prior := range s.RegularExecutions {
		if prior.Claim.Task != x.Claim.Task || !prior.Claimed {
			continue
		}
		if prior.Source == nil || prior.Source.Status == "RUNNING" {
			return fmt.Errorf("EXECUTOR_HANDOFF: stop and settle the previous executor before handing off")
		}
		for _, op := range prior.Operations {
			if op.Receipt == nil {
				return fmt.Errorf("EXECUTOR_HANDOFF: reconcile pending task operations first")
			}
		}
	}
	return nil
}

func decideExecutorHandoff(s *State, q RegularRequest, id, eventID string, revision uint64) error {
	if err := validateExecutorHandoff(s, q); err != nil {
		return err
	}
	r := s.Regular
	x := s.RegularExecutions[q.Target]
	from := x.Executor
	if from == nil {
		from = &ExecutorTarget{Kind: "native", Provider: x.Claim.Provider, Label: x.Claim.Provider, Context: x.Claim.Context}
	}
	receipt := ExecutorHandoffReceipt{
		ID: id, Task: x.Claim.Task, BatchID: nextBatch(r).ID,
		FromAction: q.Target, FromProvider: x.Claim.Provider, FromExecutor: from, FromIdentity: x.Identity,
		To: q.Handoff.To, WorkspaceView: q.Handoff.WorkspaceView,
		RequirementRevision: r.Requirement.Revision, Candidate: x.Claim.Candidate,
		Revision: revision, DecisionEvent: eventID, Reason: q.Reason,
	}
	r.ExecutorHandoffs = append(r.ExecutorHandoffs, receipt)
	x.Finished = true
	x.SupersededBy = id
	s.RegularExecutions[q.Target] = x
	delete(s.PendingActions, q.Target)
	s.Tasks[x.Claim.Task] = runtime.TaskTerminal
	at := s.Attempts[x.Claim.Task]
	at.RetryExhausted = false
	s.Attempts[x.Claim.Task] = at
	if r.Preparation.ExtraAttempts == nil {
		r.Preparation.ExtraAttempts = map[string]int{}
	}
	// The explicit decision authorizes exactly one replacement execution. Old
	// attempts and costs remain counted; this is not a reset of retry history.
	r.Preparation.ExtraAttempts[x.Claim.Task.String()]++
	return nil
}

func handoffReceipt(r *RegularState, id string) *ExecutorHandoffReceipt {
	for i := len(r.ExecutorHandoffs) - 1; i >= 0; i-- {
		if r.ExecutorHandoffs[i].ID == id {
			return &r.ExecutorHandoffs[i]
		}
	}
	return nil
}

func isExternalExecution(x RegularExecution) bool {
	return x.Executor != nil && x.Executor.Kind == "external"
}

// bindDeveloperExecutor is the sole continuation/transfer selector. A handoff
// starts a new native session (possibly on another provider), while an ordinary
// continuation retains the last executor, context and identity of this Batch.
func bindDeveloperExecutor(s *State, x *RegularExecution) {
	prior, exists := latestDeveloper(s, x.Claim.Task)
	var transfer *ExecutorHandoffReceipt
	for i := len(s.Regular.ExecutorHandoffs) - 1; i >= 0; i-- {
		h := &s.Regular.ExecutorHandoffs[i]
		if h.Task == x.Claim.Task {
			old := s.RegularExecutions[h.FromAction]
			if !exists || prior.Number <= old.Number {
				transfer = h
			}
			break
		}
	}
	if transfer != nil {
		target := transfer.To
		x.PreviousAction = transfer.FromAction
		x.Executor = &target
		x.ExecutorHandoffID = transfer.ID
		x.Claim.Provider = target.Provider
		x.Claim.Context = target.Context
	} else if exists {
		x.PreviousAction = prior.Claim.ActionID
		x.Executor = prior.Executor
		x.ExecutorHandoffID = prior.ExecutorHandoffID
		x.Claim.Provider = prior.Claim.Provider
		x.Claim.Context = prior.Claim.Context
		if prior.Identity != "" {
			x.Claim.Operation = "resume"
			x.Claim.Identity = prior.Identity
			x.Identity = prior.Identity
		}
	}
	if isExternalExecution(*x) {
		x.Claim.Operation = "external"
	}
}

func claimExternalExecutor(s *State, x *RegularExecution, c ExecutionClaim) error {
	if !isExternalExecution(*x) || x.Claim.Task.Step != "development-worker" ||
		c.Operation != "external" || x.Claim.Operation != "external" || x.Claimed || x.Finished || c.Context != nil || c.Identity != x.Claim.Identity {
		return fmt.Errorf("EXECUTOR_HANDOFF: external claim requires an explicitly assigned current developer")
	}
	h := handoffReceipt(s.Regular, x.ExecutorHandoffID)
	if h == nil || h.Task != c.Task || h.To.Provider != c.Provider {
		return fmt.Errorf("EXECUTOR_HANDOFF: external executor has no matching user decision")
	}
	x.Claimed = true
	if x.Identity == "" {
		x.Identity = "external:" + strings.TrimPrefix(digestOfCanonical(struct{ Run, Handoff string }{s.RunID, h.ID}), "sha256:")
	}
	// An engine-issued identity is explicitly NOT an observed platform session.
	// No native lifecycle receipt, transcript, token count or PASS is fabricated.
	x.Source = &ExecutionSource{ActionID: c.ActionID, AttemptID: c.AttemptID,
		Provider: c.Provider, Identity: x.Identity, Status: "UNAVAILABLE",
		Reason: "User-assigned external developer: " + x.Executor.Label + "; native transcript and usage are not exposed"}
	return nil
}
