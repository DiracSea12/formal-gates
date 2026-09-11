package protocol

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/cost"
	"reflect"
	"strings"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/coverage"
)

// TaskOperationRequest is an internal call of an already attached worker. It
// never creates a task or an attempt. The operation names and QA command come
// from the workflow contract; callers cannot supply an executable at call time.
type TaskOperationRequest struct {
	RunID            string         `json:"runId"`
	ExpectedRevision uint64         `json:"expectedRevision"`
	Binding          ExecutionClaim `json:"binding"`
	RequestID        string         `json:"requestId"`
	Kind             string         `json:"kind"`
	CaseID           string         `json:"caseId,omitempty"`
	Prompt           string         `json:"prompt,omitempty"`
}

// TaskResources is bound by the prepare intent before any filesystem work.
// Paths are relative to this run's engine state directory.
type TaskResources struct {
	Root   string `json:"root"`
	Work   string `json:"work"`
	Temp   string `json:"temp"`
	Output string `json:"output"`
	Cache  string `json:"cache"`
}

func ResourcesForTask(id string) TaskResources {
	root := "resources/qa-operations/" + strings.TrimPrefix(id, "hact:task:")
	return TaskResources{Root: root, Work: root + "/work", Temp: root + "/tmp", Output: root + "/output", Cache: "resources/qa-cache"}
}

type TaskOperation struct {
	Resources        *TaskResources        `json:"resources,omitempty"`
	WorkspaceMode    string                `json:"workspaceMode,omitempty"`
	ID               string                `json:"id"`
	Request          TaskOperationRequest  `json:"request"`
	Preparation      []coverage.SharedStep `json:"preparation,omitempty"`
	Command          *coverage.Command     `json:"command,omitempty"`
	CheckpointDigest string                `json:"checkpointDigest,omitempty"`
	ClaimedRevision  uint64                `json:"claimedRevision"`
	Receipt          *TaskOperationReceipt `json:"receipt,omitempty"`
}

type TaskOperationReceipt struct {
	Artifacts         map[string]string `json:"artifacts,omitempty"`
	InputDigest       string            `json:"inputDigest,omitempty"`
	SharedWorkspace   string            `json:"sharedWorkspace,omitempty"`
	PreparationReused bool              `json:"preparationReused,omitempty"`
	ID                string            `json:"id"`
	ActionID          string            `json:"actionId"`
	RequestDigest     string            `json:"requestDigest"`
	Status            string            `json:"status"`
	Workspace         string            `json:"workspace,omitempty"`
	Output            string            `json:"output,omitempty"`
	Stderr            string            `json:"stderr,omitempty"`
	ExitCode          int               `json:"exitCode"`
	Identity          string            `json:"identity,omitempty"`
	TranscriptPath    string            `json:"transcriptPath,omitempty"`
	Message           string            `json:"message,omitempty"`
}

func TaskOperationID(q TaskOperationRequest) string {
	return "hact:task:" + strings.TrimPrefix(digestOfCanonical([]string{q.Binding.ActionID, q.RequestID}), "sha256:")
}
func TaskOperationDigest(q TaskOperationRequest) string { return digestOfCanonical(q) }

// CurrentTaskOperationBinding checks the same live action, candidate and exact
// QA revision as the outer call. It is also used immediately before recovery;
// old receipts remain readable, but never authorize a new side effect.
func CurrentTaskOperationBinding(s *State, q TaskOperationRequest) error {
	x, ok := s.RegularExecutions[q.Binding.ActionID]
	if !ok || !x.Claimed || x.Finished || x.Identity == "" {
		return fmt.Errorf("UNKNOWN_ACTION: operation requires an attached live action")
	}
	expected := x.Claim
	expected.Identity = x.Identity
	if !reflect.DeepEqual(expected, q.Binding) || q.RunID != s.RunID {
		return fmt.Errorf("INTENT_MISMATCH: operation binding differs from issued action")
	}
	r := s.Regular
	if r.Terminal != "" || r.ChangePending || r.Ask != nil || q.Binding.RequirementRevision != r.Requirement.Revision || !currentPreparationClaim(r, q.Binding) {
		return fmt.Errorf("EVENT_NOT_CURRENT: task operation is stale or paused")
	}
	if q.Binding.Task.Step == "start-readiness" && q.Binding.PrerequisiteDigest != r.Reviews["product-review"].CurrentDigest {
		return fmt.Errorf("EVENT_NOT_CURRENT: product review changed")
	}
	return nil
}

func taskCase(s *State, q TaskOperationRequest) (*coverage.AcceptanceCase, error) {
	w := currentWave(s.Regular)
	if w == nil {
		return nil, fmt.Errorf("EVENT_NOT_CURRENT: no current QA wave")
	}
	mode := strings.TrimSuffix(string(q.Binding.Task.Step), "-execution")
	allowed := false
	if q.Binding.Task.Step == "repair-worker" {
		plan := currentRepair(s.Regular)
		if plan == nil || plan.ID != q.Binding.PrerequisiteDigest || plan.Candidate != q.Binding.Candidate {
			return nil, fmt.Errorf("EVENT_NOT_CURRENT: repair plan differs")
		}
		for _, id := range plan.AffectedCases {
			if id == q.CaseID {
				allowed = true
			}
		}
		for name, qa := range s.Regular.Preparation.QA {
			for _, c := range qa.Design.Manifest.Cases {
				if c.CaseID == q.CaseID {
					mode = name
				}
			}
		}
	} else if mode == "blackbox" || mode == "whitebox" {
		for _, target := range w.Targets {
			if target.Task == q.Binding.Task {
				for _, id := range target.Expected {
					if id == q.CaseID {
						allowed = true
					}
				}
			}
		}
	} else {
		return nil, fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: QA operations require a QA execution or repair action")
	}
	if !allowed {
		return nil, fmt.Errorf("SCOPE_MISMATCH: case is not in the issued exact expected set")
	}
	qa := s.Regular.Preparation.QA[mode]
	for _, c := range qa.Design.Manifest.Cases {
		if c.CaseID == q.CaseID {
			if c.Command == nil {
				return nil, fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: case has no frozen executable declaration")
			}
			return &c, nil
		}
	}
	return nil, fmt.Errorf("SCOPE_MISMATCH: no declared case")
}

func findTaskOperation(x RegularExecution, kind, caseID string) *TaskOperation {
	var latest *TaskOperation
	for _, op := range x.Operations {
		if op.Request.Kind == kind && op.Request.CaseID == caseID && (latest == nil || op.ClaimedRevision > latest.ClaimedRevision) {
			copy := op
			latest = &copy
		}
	}
	return latest
}

func (e *Engine) claimTaskOperation(s *State, q TaskOperationRequest, revision uint64) error {
	if q.ExpectedRevision != revision {
		return fmt.Errorf("STALE_FRESHNESS: operation revision differs")
	}
	if strings.TrimSpace(q.RequestID) == "" {
		return fmt.Errorf("task operation requires requestId")
	}
	if err := CurrentTaskOperationBinding(s, q); err != nil {
		return err
	}
	e.refreshRegular(s)
	if s.Regular.Guard.Status != "READY" {
		return fmt.Errorf("%s: task operation blocked before invocation", s.Regular.Guard.Status)
	}
	x := s.RegularExecutions[q.Binding.ActionID]
	id := TaskOperationID(q)
	if _, ok := x.Operations[id]; ok {
		return fmt.Errorf("DUPLICATE_ACTION: internal call already claimed")
	}
	for _, op := range x.Operations {
		if op.Receipt == nil && (q.CaseID == "" || op.Request.CaseID == "" || q.CaseID == op.Request.CaseID) {
			return fmt.Errorf("UNKNOWN: reconcile pending internal operation %s before another call", op.ID)
		}
	}
	op := TaskOperation{ID: id, Request: q, ClaimedRevision: revision + 1}
	switch q.Kind {
	case "whitebox.coverage":
		if q.Binding.Task.Step != "whitebox-design" || q.CaseID != "" || WhiteboxCoverageReady(x) {
			return fmt.Errorf("PREREQUISITE_FAILED: coverage judgment belongs once to whitebox design")
		}
		var judgment struct {
			CoverageJudgment string `json:"coverageJudgment"`
			BehaviorOracles  []struct {
				Behavior string `json:"behavior"`
				Oracle   string `json:"oracle"`
			} `json:"behaviorOracles"`
		}
		if json.Unmarshal([]byte(q.Prompt), &judgment) != nil || strings.TrimSpace(judgment.CoverageJudgment) == "" || len(judgment.BehaviorOracles) == 0 {
			return fmt.Errorf("coverage requires independent behavior and oracle judgments before existing tests")
		}
		for _, row := range judgment.BehaviorOracles {
			if strings.TrimSpace(row.Behavior) == "" || strings.TrimSpace(row.Oracle) == "" {
				return fmt.Errorf("coverage behavior/oracle cannot be empty")
			}
		}
	case "model.run":
		if strings.TrimSpace(q.Prompt) == "" || q.CaseID != "" {
			return fmt.Errorf("model.run requires prompt and no caseId")
		}
	case "qa.prepare", "qa.target", "qa.collect":
		if q.Prompt != "" {
			return fmt.Errorf("QA operation accepts only its frozen case declaration")
		}

		active := 0
		for _, execution := range s.RegularExecutions {
			for _, pending := range execution.Operations {
				if pending.Receipt == nil && strings.HasPrefix(pending.Request.Kind, "qa.") {
					active++
				}
			}
		}
		if limit := s.Regular.Settings.Values.QACapacity; limit > 0 && active >= limit {
			return fmt.Errorf("CAPACITY_QUEUED: %d QA operations are running (limit %d)", active, limit)
		}
		c, err := taskCase(s, q)
		if err != nil {
			return err
		}
		op.Command = c.Command
		op.WorkspaceMode = c.WorkspaceMode
		for _, qa := range s.Regular.Preparation.QA {
			ownsCase := false
			for _, declared := range qa.Design.Manifest.Cases {
				if declared.CaseID == c.CaseID {
					ownsCase = true
				}
			}
			if !ownsCase {
				continue
			}
			for _, id := range c.SharedSteps {
				for _, step := range qa.Design.Manifest.SharedSteps {
					if step.ID == id {
						op.Preparation = append(op.Preparation, step)
					}
				}
			}
		}

		// Independent cases may advance while another target is running.
		// Explicit shared-step references conservatively serialize their
		// mutable workspace use; concurrent prepares use the preparation lock.
		for _, execution := range s.RegularExecutions {
			for _, pending := range execution.Operations {
				if pending.Request.Binding.Candidate != q.Binding.Candidate {
					continue
				}
				if pending.Receipt != nil || (q.Kind == "qa.prepare" && pending.Request.Kind == "qa.prepare") {
					continue
				}
				for _, a := range op.Preparation {
					for _, b := range pending.Preparation {
						if a.ID == b.ID && !a.Independent && !b.Independent {
							return fmt.Errorf("RESOURCE_QUEUED: case %s waits for shared step %s held by case %s operation %s", q.CaseID, a.ID, pending.Request.CaseID, pending.ID)
						}
					}
				}
			}
		}
		checkpoint := currentWave(s.Regular).Checkpoints[targetName(q.Binding.Task.Step)]
		op.CheckpointDigest = checkpoint.Digest
		for _, completed := range checkpoint.Cases {
			if completed.CaseID == q.CaseID {
				return fmt.Errorf("DUPLICATE_ACTION: case %s is already completed in the current checkpoint", q.CaseID)
			}
		}
		if prior := findTaskOperation(x, q.Kind, q.CaseID); prior != nil {
			allowed := false
			if q.Kind == "qa.prepare" && checkpoint.Digest != "" && prior.CheckpointDigest != checkpoint.Digest {
				// An accepted checkpoint may explicitly invalidate changed tool or
				// environment evidence. It reopens only its named remaining cases,
				// and this operation consumes that exact checkpoint once.
				remaining := false
				for _, id := range checkpoint.Remaining {
					if id == q.CaseID {
						remaining = true
					}
				}
				allowed = remaining && qaInvalidatedSince(currentWave(s.Regular), targetName(q.Binding.Task.Step), q.CaseID, prior.CheckpointDigest)
			} else if q.Kind != "qa.prepare" {
				prepare := findTaskOperation(x, "qa.prepare", q.CaseID)
				allowed = prepare != nil && prior.ClaimedRevision < prepare.ClaimedRevision
			}
			if !allowed {
				return fmt.Errorf("DUPLICATE_ACTION: QA stage already claimed")
			}
		}
		previous := ""
		if q.Kind == "qa.target" {
			previous = "qa.prepare"
		}
		if q.Kind == "qa.collect" {
			previous = "qa.target"
		}
		if previous != "" {
			prior := findTaskOperation(x, previous, q.CaseID)
			if prior == nil || prior.Receipt == nil || prior.Receipt.Status != "COMPLETED" {
				return fmt.Errorf("PREREQUISITE_FAILED: %s must complete before %s", previous, q.Kind)
			}
		}
		if q.Kind == "qa.prepare" {
			for _, dep := range c.Dependencies {
				target := qaVerificationTarget(s.Regular, q.Binding)
				if target != nil && target.Inherited[dep].Status == "PASS" {
					continue
				}
				completed := false
				for _, row := range checkpoint.Cases {
					if row.CaseID == dep && row.Status == "PASS" {
						completed = true
					}
				}
				if completed {
					continue
				}
				return fmt.Errorf("PREREQUISITE_FAILED: case %s requires an accepted PASS for dependent case %s; collected exit evidence still needs semantic judgment", q.CaseID, dep)
			}
		}
	default:
		return fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: unsupported task operation %s", q.Kind)
	}
	if x.Operations == nil {
		x.Operations = map[string]TaskOperation{}
	}
	if q.Kind == "qa.prepare" {
		resources := ResourcesForTask(id)
		op.Resources = &resources
	}
	x.Operations[id] = op
	s.RegularExecutions[q.Binding.ActionID] = x
	s.PendingHostActions[id] = HostActionIntent{ActionID: id, Operation: HostActionExecuteAdapterOperation, Step: q.Binding.Task.Step, Adapter: &AdapterHostAction{Operation: authoring.OperationID(q.Kind), Schema: "schema.task.operation", Params: map[string]any{"request": q}}, PayloadDigest: TaskOperationDigest(q), Correlation: q.Binding.Identity, Revision: revision + 1}
	return nil
}

func (e *Engine) acceptTaskOperation(s *State, v TaskOperationReceipt) error {
	x, ok := s.RegularExecutions[v.ActionID]
	if !ok {
		return fmt.Errorf("UNKNOWN_ACTION: task receipt action missing")
	}
	op, ok := x.Operations[v.ID]
	if !ok || op.Receipt != nil {
		return fmt.Errorf("DUPLICATE_ACTION: task receipt has no pending operation")
	}
	if v.RequestDigest != TaskOperationDigest(op.Request) {
		return fmt.Errorf("INTENT_MISMATCH: task receipt binding differs")
	}
	if v.Status != "COMPLETED" && v.Status != "FAILED" {
		return fmt.Errorf("task receipt status must be COMPLETED or FAILED")
	}
	if v.Status == "FAILED" && strings.TrimSpace(v.Message) == "" {
		return fmt.Errorf("failed task operation needs diagnostic message")
	}
	if op.Request.Kind == "model.run" {
		entry := cost.DispatchCost{Target: string(x.Claim.Task.Step), Kind: "task-operation", Source: cost.SourceUnavailable, TranscriptPath: v.TranscriptPath}
		if v.TranscriptPath != "" {
			if err := checkTranscriptIdentity(v.TranscriptPath, v.Identity); err != nil {
				return err
			}
			usage, err := cost.ParseTranscript(x.Claim.Provider, v.TranscriptPath)
			if err != nil {
				return fmt.Errorf("COST_PENDING: task model source: %w", err)
			}
			entry.Source = cost.SourceTranscript
			entry.InputCacheHitTokens = usage.InputCacheHitTokens
			entry.InputCacheMissTokens = usage.InputCacheMissTokens
			entry.TotalInputTokens = usage.InputCacheHitTokens + usage.InputCacheMissTokens
			entry.OutputTokens = usage.OutputTokens
			if timing, err := cost.CodexTaskTimingAfter(v.TranscriptPath, time.Time{}); err == nil {
				entry.Timing = &timing
			}
		}
		cost.Record(s.Cost, v.ID, entry)
	}
	if op.Request.Kind == "qa.prepare" && v.Workspace != "" && s.Regular.Preparation != nil {
		if s.Regular.Preparation.Resources == nil {
			s.Regular.Preparation.Resources = map[string]string{}
		}
		s.Regular.Preparation.Resources[op.ID] = v.Workspace
		if v.SharedWorkspace != "" {
			s.Regular.Preparation.Resources["shared:"+v.InputDigest] = v.SharedWorkspace
		}
	}
	op.Receipt = &v
	x.Operations[v.ID] = op
	s.RegularExecutions[v.ActionID] = x
	intent := s.PendingHostActions[v.ID]
	s.HostActionReceipts[v.ID] = HostActionReceipt{ActionID: v.ID, Operation: HostActionExecuteAdapterOperation, Step: x.Claim.Task.Step, AdapterOperation: authoring.OperationID(op.Request.Kind), Provider: x.Claim.Provider, Correlation: x.Identity, PayloadDigest: intent.PayloadDigest, Status: HostActionStatusExecuted, AdapterEvidence: &AdapterEvidence{Values: map[string]any{"receipt": v}}, Digest: digestOfCanonical(v)}
	delete(s.PendingHostActions, v.ID)
	return nil
}

// WhiteboxCoverageReady checks the ordered receipt, not final prose claiming that
// coverage was considered earlier. The adapter cannot expose tests before it.
func WhiteboxCoverageReady(x RegularExecution) bool {
	for _, op := range x.Operations {
		if op.Request.Kind == "whitebox.coverage" && op.Receipt != nil && op.Receipt.Status == "COMPLETED" {
			return true
		}
	}
	return false
}
