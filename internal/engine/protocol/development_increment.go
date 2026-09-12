package protocol

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"formal-gates/internal/engine/runtime"
)

// Incremental development changes only implementation work inside the confirmed
// requirement. Changed product requirements still use requirement-change.
type DevelopmentIncrement struct {
	Kind          string           `json:"kind"`
	Batch         DevelopmentBatch `json:"batch"`
	WorkspaceView string           `json:"workspaceView"`
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
	if r.Preparation.Operation != nil {
		return nil, fmt.Errorf("DEVELOPMENT_INCREMENT: reconcile the current Git operation first")
	}
	plan := *r.Plan
	plan.Batches = append([]DevelopmentBatch(nil), r.Plan.Batches...)
	switch q.Increment.Kind {
	case "optimize":
		index := -1
		for i, b := range plan.Batches {
			if b.ID == q.Target {
				index = i
				break
			}
		}
		if index < 0 || r.Preparation.Commits[q.Target].After == "" || q.Increment.Batch.ID != q.Target {
			return nil, fmt.Errorf("DEVELOPMENT_INCREMENT: optimization must reopen a completed Batch with the same ID")
		}
		if !reflect.DeepEqual(plan.Batches[index].Dependencies, q.Increment.Batch.Dependencies) {
			return nil, fmt.Errorf("DEVELOPMENT_INCREMENT: optimization preserves dependency lineage")
		}
		plan.Batches[index] = q.Increment.Batch
	case "append":
		if q.Target != plan.Batches[len(plan.Batches)-1].ID {
			return nil, fmt.Errorf("DEVELOPMENT_INCREMENT: append targets the existing final Batch")
		}
		linked := false
		for _, id := range q.Increment.Batch.Dependencies {
			if id == q.Target {
				linked = true
			}
		}
		if !linked {
			return nil, fmt.Errorf("DEVELOPMENT_INCREMENT: appended Batch must depend on its predecessor")
		}
		plan.Batches = append(plan.Batches, q.Increment.Batch)
	default:
		return nil, fmt.Errorf("DEVELOPMENT_INCREMENT: kind must be optimize or append")
	}
	plan.RequirementRevision = r.Requirement.Revision
	plan.ConfirmedRevision = r.Requirement.Revision
	plan.Digest = ""
	if err := validateDevelopmentPlan(plan, r.Requirement.Revision); err != nil {
		return nil, err
	}
	plan.Digest = digestOfCanonical(plan)
	return &plan, nil
}

func validateDevelopmentIncrement(s *State, q RegularRequest) error {
	if _, err := incrementalPlan(s.Regular, q); err != nil {
		return err
	}
	for _, x := range s.RegularExecutions {
		if x.Claimed && (!x.Finished || x.Source == nil || x.Source.Status == "RUNNING") {
			return fmt.Errorf("DEVELOPMENT_INCREMENT: settle/checkpoint active executions before changing the plan")
		}
		for _, op := range x.Operations {
			if op.Receipt == nil {
				return fmt.Errorf("DEVELOPMENT_INCREMENT: reconcile pending task operations first")
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
		assignTask(q.Increment.Batch.ID)
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
		if q.Impact[mode+"-review"] != "unaffected" {
			reviseQA(qa)
		}
		if mode == "whitebox" {
			// Carry only the test-only artifact manifest into a workspace based on the
			// new development snapshot. Never reuse the old production tree as Q.
			if previousSnapshot != "" {
				qa.TestArtifactsFrom = previousSnapshot
				qa.TestArtifactsBase = old.DevelopmentSnapshot
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
