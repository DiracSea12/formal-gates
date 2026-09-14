package protocol

import (
	"encoding/json"
	"fmt"
	"strings"

	"formal-gates/internal/engine/runtime"
)

// Incremental development changes only implementation work inside the confirmed
// requirement. Changed product requirements still use requirement-change.
type DevelopmentIncrement struct {
	Kind          string `json:"kind"`
	BatchID       string `json:"batchId,omitempty"`
	WorkspaceView string `json:"workspaceView"`
	// Preserve opaque historical request evidence without making its fields
	// available to current task authoring or execution.
	recordedJSON json.RawMessage
}

func (v *DevelopmentIncrement) UnmarshalJSON(data []byte) error {
	type fields DevelopmentIncrement
	var decoded fields
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	*v = DevelopmentIncrement(decoded)
	for key := range object {
		if key != "kind" && key != "batchId" && key != "workspaceView" {
			v.recordedJSON = append(json.RawMessage(nil), data...)
			break
		}
	}
	return nil
}

func (v DevelopmentIncrement) MarshalJSON() ([]byte, error) {
	if len(v.recordedJSON) != 0 {
		return v.recordedJSON, nil
	}
	type fields DevelopmentIncrement
	return json.Marshal(fields(v))
}

type DevelopmentIncrementReceipt struct {
	ID                  string                     `json:"id"`
	DecisionEvent       string                     `json:"decisionEvent"`
	Revision            uint64                     `json:"revision"`
	RequirementRevision string                     `json:"requirementRevision"`
	Kind                string                     `json:"kind"`
	Target              string                     `json:"target"`
	PreviousPlan        DevelopmentPlan            `json:"previousPlan"`
	PlanDigest          string                     `json:"planDigest"`
	WorkspaceView       string                     `json:"workspaceView"`
	PreviousTasks       map[string]runtime.TaskKey `json:"previousTasks,omitempty"`
	Tasks               map[string]runtime.TaskKey `json:"tasks"`
	UserDisposition     *RequirementImpactDecision `json:"userDisposition,omitempty"`
	Impact              map[string]string          `json:"impact"`
	Reason              string                     `json:"reason"`
}

func incrementalPlan(r *RegularState, q RegularRequest) (*DevelopmentPlan, error) {
	if r == nil || r.Mode != "regular" || r.Terminal != "" || r.ChangePending || r.Plan == nil || r.Preparation == nil || r.Topology != "no-split" {
		return nil, fmt.Errorf("DEVELOPMENT_INCREMENT: active prepared no-split regular run required")
	}
	if q.Increment == nil || q.Meaning != "preserved" || strings.TrimSpace(q.Increment.WorkspaceView) == "" {
		return nil, fmt.Errorf("DEVELOPMENT_INCREMENT: exact workspace, increment and preserved requirement meaning required")
	}
	if len(q.Increment.recordedJSON) != 0 {
		return nil, fmt.Errorf("DEVELOPMENT_INCREMENT: only kind, batchId and workspaceView are accepted; task definitions must come from the confirmed plan")
	}
	if r.Plan.ConfirmedRevision != r.Requirement.Revision {
		return nil, fmt.Errorf("DEVELOPMENT_INCREMENT: confirm the current development plan before selecting a Batch")
	}
	if r.Preparation.Operation != nil {
		return nil, fmt.Errorf("DEVELOPMENT_INCREMENT: reconcile the current Git operation first")
	}
	plan := *r.Plan
	plan.Batches = append([]DevelopmentBatch(nil), r.Plan.Batches...)
	index := -1
	for i, b := range plan.Batches {
		if b.ID == q.Increment.BatchID {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, fmt.Errorf("DEVELOPMENT_INCREMENT: batchId must reference the confirmed plan; new task definitions require plan-change or requirement-change")
	}
	switch q.Increment.Kind {
	case "optimize":
		if r.Preparation.Commits[q.Target].After == "" || q.Increment.BatchID != q.Target {
			return nil, fmt.Errorf("DEVELOPMENT_INCREMENT: optimization must reopen a completed Batch with the same ID")
		}
	case "append":
		if index != len(plan.Batches)-1 || index == 0 || q.Target != plan.Batches[index-1].ID {
			return nil, fmt.Errorf("DEVELOPMENT_INCREMENT: append selects the confirmed final Batch and its predecessor")
		}
		if _, done := r.Preparation.Batches[q.Increment.BatchID]; done {
			return nil, fmt.Errorf("DEVELOPMENT_INCREMENT: append requires a pending confirmed Batch")
		}
		linked := false
		for _, id := range plan.Batches[index].Dependencies {
			if id == q.Target {
				linked = true
			}
		}
		if !linked {
			return nil, fmt.Errorf("DEVELOPMENT_INCREMENT: appended Batch must depend on its predecessor")
		}
	default:
		return nil, fmt.Errorf("DEVELOPMENT_INCREMENT: kind must be optimize or append")
	}
	plan.RequirementRevision = r.Requirement.Revision
	plan.ConfirmedRevision = r.Requirement.Revision
	if err := validateDevelopmentPlan(plan, r.Requirement.Revision); err != nil {
		return nil, err
	}
	// Selecting an already confirmed Batch does not change the plan bytes or
	// invalidate independent baseline tasks bound to that plan.
	return &plan, nil
}

func validateDevelopmentIncrement(s *State, q RegularRequest) error {
	if _, err := incrementalPlan(s.Regular, q); err != nil {
		return err
	}
	for id, x := range s.RegularExecutions {
		// Baseline QA owns a separate workspace and does not depend on the
		// implementation candidate. Preserve its live claim when its contract
		// is retained; all candidate-dependent work still conflicts.
		independent := (x.Claim.Task.Step == "blackbox-design" || x.Claim.Task.Step == "blackbox-review") && (q.Impact["blackbox-review"] == "" || q.Impact["blackbox-review"] == "unaffected")
		if independent {
			continue
		}
		if x.Claimed && (!x.Finished || x.Source == nil || x.Source.Status == "RUNNING") {
			return fmt.Errorf("DEVELOPMENT_INCREMENT: %s depends on changed inputs or shares the development workspace; settle/checkpoint it first", id)
		}
		for _, op := range x.Operations {
			if op.Receipt == nil {
				return fmt.Errorf("DEVELOPMENT_INCREMENT: reconcile pending operation for conflicting action %s first", id)
			}
		}
	}
	return nil
}

func incrementalImpactItems(r *RegularState, q RegularRequest) []ImpactItem {
	result := []ImpactItem{}
	for _, item := range requirementImpactItems(r) {
		if item.ID == "development" || r.Reviews[item.ID] != nil {
			continue
		}
		if q.Increment.Kind == "optimize" && item.ID == "batch:"+q.Target {
			continue
		}
		// A future implementation is not the old validation candidate. Keeping a
		// case design/review does not claim its old execution passed on new bytes.
		if r.Verification != nil {
			if _, ok := r.Verification.Effective[item.ID]; ok {
				item.CanKeep = false
				item.Evidence += "; execution is bound to the previous candidate"
			}
		}
		if item.ID == "blackbox-review" && !r.Preparation.QA["blackbox"].Frozen && r.Preparation.QA["blackbox"].ReviewWaiver == nil {
			item.CanKeep = true
			item.Evidence += "; baseline QA contract and unfinished work may be retained; no execution PASS is inferred"
		}
		result = append(result, item)
	}
	return result
}

func decideDevelopmentIncrement(s *State, a *RegularAsk, d RegularDecision, eventID string, revision uint64) error {
	q := a.Request
	if err := validateDevelopmentIncrement(s, q); err != nil {
		return err
	}
	r := s.Regular
	// Reuse the same typed disposition contract without opening a requirement
	// change or overwriting its authorization. No second decision mechanism.
	authority := *r
	if len(a.ImpactItems) > 0 {
		if err := recordRequirementImpact(&authority, a, d, eventID, revision); err != nil {
			return err
		}
	} else {
		if len(d.Dispositions) > 0 {
			return fmt.Errorf("USER_IMPACT_REQUIRED: no unlisted disposition is allowed")
		}
		authority.RequirementImpact = &RequirementImpactDecision{RequestID: a.ID, DecisionEvent: eventID, RequirementRevision: r.Requirement.Revision, Candidate: r.Candidate, Revision: revision, Choices: map[string]string{}}
	}
	// The Ask precedes the user's choices. Only explicitly delegated items may
	// be classified in the response; fixed choices stay authoritative.
	impact := map[string]string{}
	for id, value := range q.Impact {
		impact[id] = value
	}
	for id, value := range d.Impact {
		if authority.RequirementImpact.Choices[id] != "delegate" {
			return fmt.Errorf("USER_IMPACT_CONFLICT: %s was not delegated", id)
		}
		impact[id] = value
	}
	q.Impact = impact
	if err := resolveRequirementImpact(&authority, &q); err != nil {
		return err
	}
	for _, item := range a.ImpactItems {
		if !item.CanKeep && q.Impact[item.ID] == "unaffected" {
			return fmt.Errorf("DEVELOPMENT_INCREMENT: %s has no applicable PASS to carry", item.ID)
		}
	}
	if err := validateDevelopmentIncrement(s, q); err != nil {
		return err
	}
	plan, err := incrementalPlan(r, q)
	if err != nil {
		return err
	}
	old := r.Preparation
	encoded, err := json.Marshal(old)
	if err != nil {
		return err
	}
	var next Preparation
	if err = json.Unmarshal(encoded, &next); err != nil {
		return err
	}
	if next.BatchTasks == nil {
		next.BatchTasks = map[string]runtime.TaskKey{}
	}
	for _, b := range r.Plan.Batches {
		key, exists := old.BatchTasks[b.ID]
		if result, ok := old.Batches[b.ID]; ok {
			if x, ok := s.RegularExecutions[result.ActionID]; ok {
				key = x.Claim.Task
				exists = true
			}
		}
		if current := nextBatch(r); current != nil && current.ID == b.ID {
			key = batchTask(old, b.ID)
			exists = true
		}
		if exists {
			next.BatchTasks[b.ID] = key
		}
	}
	previousPlan := *r.Plan
	r.PreparationHistory = append(r.PreparationHistory, *old)
	r.Plan = plan
	r.Preparation = &next
	next.PlanDigest = plan.Digest
	next.BindingDigest = preparationBinding(r)
	next.Candidate = nil
	retireUnclaimedOnChange(s, a.ID)
	previousTasks := map[string]runtime.TaskKey{}
	tasks := map[string]runtime.TaskKey{}
	assignTask := func(id string) {
		// A completed Batch's optimization is new authorized work, not a retry
		// of its accepted PASS. The decision also distinguishes repeated requests
		// whose plan text is identical. Handoff alone keeps an unfinished TaskKey.
		key := preparationTask(&next, "development-worker", id+"-increment-"+strings.TrimPrefix(digestOfCanonical(a.ID), "sha256:"))
		next.BatchTasks[id] = key
		tasks[id] = key
	}
	reopen := func(id string) {
		previousTasks[id] = next.BatchTasks[id]
		assignTask(id)
		delete(next.Batches, id)
		delete(next.Commits, id)
	}
	if q.Increment.Kind == "optimize" {
		reopen(q.Target)
	} else {
		assignTask(q.Increment.BatchID)
	}
	for id := range old.Commits {
		if q.Increment.Kind == "optimize" && id == q.Target {
			continue
		}
		if q.Impact["batch:"+id] != "unaffected" {
			reopen(id)
		}
	}
	for mode, qa := range next.QA {
		previousSnapshot := qa.Snapshot
		previousReviewed := qa.Frozen && qa.Review != nil && qa.Whitelist != nil && qa.ReviewWaiver == nil && !qa.NeedsRevision
		if q.Impact[mode+"-review"] != "unaffected" {
			reviseQA(qa)
		}
		if mode == "whitebox" {
			// Carry only the test-only artifact manifest into a workspace based on the
			// new development snapshot. Never reuse the old production tree as Q.
			if previousSnapshot != "" && previousReviewed {
				qa.TestArtifactsFrom = previousSnapshot
				qa.TestArtifactsBase = old.DevelopmentSnapshot
			} else {
				qa.TestArtifactsFrom = ""
				qa.TestArtifactsBase = ""
			}
			qa.AuthoringBase = ""
			qa.Snapshot = ""
			delete(next.Resources, "whitebox")
		}
	}
	invalidateVerification(s, "incremental development: "+a.ID)
	classifyVerificationChange(r, q, revision)
	r.DevelopmentIncrements = append(r.DevelopmentIncrements, DevelopmentIncrementReceipt{ID: a.ID, DecisionEvent: eventID, Revision: revision, RequirementRevision: r.Requirement.Revision, Kind: q.Increment.Kind, Target: q.Target, PreviousPlan: previousPlan, PlanDigest: plan.Digest, WorkspaceView: q.Increment.WorkspaceView, PreviousTasks: previousTasks, Tasks: tasks, UserDisposition: authority.RequirementImpact, Impact: q.Impact, Reason: q.Reason})
	return nil
}
