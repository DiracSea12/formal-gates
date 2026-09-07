package validate

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type DevelopmentSubtask struct {
	ID   string `json:"id"`
	Task string `json:"task"`
}

// DevelopmentPlanBinding is the final report's contract provenance. Batch
// contracts and derived progress are already present in developmentBatches.
type DevelopmentPlanBinding struct {
	Digest                  string `json:"digest"`
	Reason                  string `json:"reason"`
	RequirementRevision     string `json:"requirementRevision"`
	TechnicalReviewDispatch string `json:"technicalReviewDispatch"`
	ReviewMasterRunID       string `json:"reviewMasterRunId,omitempty"`
}

func developmentPlanBinding(state RunState) *DevelopmentPlanBinding {
	plan := state.DevelopmentBatchPlan
	if plan == nil || plan.Version != 1 {
		return nil
	}
	return &DevelopmentPlanBinding{Digest: plan.Digest, Reason: plan.UserReason, RequirementRevision: plan.RequirementRevision, TechnicalReviewDispatch: plan.TechnicalReviewDispatch, ReviewMasterRunID: plan.ReviewMasterRunID}
}

// The editable input contains contracts only, never dispatches or progress.
type DevelopmentPlanInput struct {
	Reason   string                  `json:"reason"`
	Subtasks []DevelopmentSubtask    `json:"subtasks"`
	Batches  []DevelopmentBatchInput `json:"batches"`
}

type DevelopmentBatchInput struct {
	ID            string   `json:"id"`
	Task          string   `json:"task"`
	SubtaskIDs    []string `json:"subtaskIds"`
	DependsOn     []string `json:"dependsOn"`
	Interfaces    string   `json:"interfaces"`
	StateBoundary string   `json:"stateBoundary"`
	Risks         string   `json:"risks"`
	DoD           string   `json:"dod"`
	Validation    []string `json:"validation"`
	Recovery      string   `json:"recovery"`
	Handoff       string   `json:"handoff"`
	ContextCost   string   `json:"contextCost"`
}

func ReadDevelopmentPlan(path string) (*DevelopmentPlanInput, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	var input DevelopmentPlanInput
	if err := decoder.Decode(&input); err != nil {
		return nil, fmt.Errorf("development plan: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("development plan must contain exactly one JSON object")
	}
	return &input, nil
}

// bindDevelopmentPlan runs inside the same atomic mutation as slicing. A
// changed contract follows the existing meaning-changed requirement/review path.
func bindDevelopmentPlan(root string, state *RunState, input *DevelopmentPlanInput, confirmed bool) error {
	if input == nil {
		return nil
	}
	if !confirmed {
		return fmt.Errorf("development plan requires --user-confirm with the slicing/route decision")
	}
	if state.RetainedOverall {
		return fmt.Errorf("register development batches in each implementing slice, not the retained overall run")
	}
	if state.DevelopmentBatchPlan != nil {
		return fmt.Errorf("development plan is already bound; revise the requirement/solution with --meaning changed, repeat technical review and confirm the replacement plan")
	}
	if developmentStarted(*state) {
		return fmt.Errorf("development plan must be registered before the first development dispatch")
	}
	if !state.RequirementConfirmed || !actionPassedOrAbsent(*state, "product-review") || !actionPassedOrAbsent(*state, "start-readiness") {
		return fmt.Errorf("confirmed requirements and passed product/technical reviews are required before development plan registration")
	}
	plan, err := validateDevelopmentPlan(*input)
	if err != nil {
		return err
	}
	plan.RequirementRevision = state.RequirementRevision
	plan.TechnicalReviewDispatch = state.Actions["start-readiness"].DispatchID
	if inheritedReview(*state, "start-readiness") {
		master, err := LoadRunState(root, state.Slicing.MasterRunID)
		if err != nil {
			return err
		}
		plan.TechnicalReviewDispatch = master.Actions["start-readiness"].DispatchID
		plan.ReviewMasterRunID = master.RunID
	}
	state.DevelopmentBatchPlan = plan
	return nil
}

func validateDevelopmentPlan(input DevelopmentPlanInput) (*DevelopmentBatchPlan, error) {
	if strings.TrimSpace(input.Reason) == "" || len(input.Batches) == 0 || len(input.Subtasks) == 0 {
		return nil, fmt.Errorf("development plan requires reason, subtasks and at least one batch (a single batch is valid)")
	}
	tasks := map[string]DevelopmentSubtask{}
	for _, task := range input.Subtasks {
		if strings.TrimSpace(task.ID) == "" || task.ID != strings.TrimSpace(task.ID) || strings.TrimSpace(task.Task) == "" {
			return nil, fmt.Errorf("every subtask needs an id without surrounding whitespace and a task description")
		}
		if _, exists := tasks[task.ID]; exists {
			return nil, fmt.Errorf("duplicate subtask id %q", task.ID)
		}
		tasks[task.ID] = task
	}
	plan := &DevelopmentBatchPlan{Version: 1, UserReason: input.Reason, Digest: digestValue(input)}
	owners := map[string]string{}
	prior := map[string]bool{}
	for _, batch := range input.Batches {
		if strings.TrimSpace(batch.ID) == "" || batch.ID != strings.TrimSpace(batch.ID) || prior[batch.ID] {
			return nil, fmt.Errorf("batch id %q is empty, duplicated or contains surrounding whitespace", batch.ID)
		}
		for _, field := range []struct{ name, value string }{{"task", batch.Task}, {"interfaces", batch.Interfaces}, {"stateBoundary", batch.StateBoundary}, {"risks", batch.Risks}, {"dod", batch.DoD}, {"recovery", batch.Recovery}, {"handoff", batch.Handoff}, {"contextCost", batch.ContextCost}} {
			if strings.TrimSpace(field.value) == "" {
				return nil, fmt.Errorf("batch %s requires %s (describe why none if not applicable)", batch.ID, field.name)
			}
		}
		if len(batch.SubtaskIDs) == 0 || len(batch.Validation) == 0 {
			return nil, fmt.Errorf("batch %s requires subtaskIds and validation", batch.ID)
		}
		for _, check := range batch.Validation {
			if strings.TrimSpace(check) == "" {
				return nil, fmt.Errorf("batch %s has an empty validation entry", batch.ID)
			}
		}
		deps := map[string]bool{}
		for _, dep := range batch.DependsOn {
			// This driver has one development worker at a time. Requiring a
			// topological file order rejects cycles and missing dependencies and
			// makes its execution order explicit to both humans and agents.
			if !prior[dep] || deps[dep] {
				return nil, fmt.Errorf("batch %s dependency %q must name a distinct earlier batch; order batches before their consumers", batch.ID, dep)
			}
			deps[dep] = true
		}
		record := DevelopmentBatchRecord{ID: batch.ID, Task: batch.Task, DoD: batch.DoD, DependsOn: batch.DependsOn, Interfaces: batch.Interfaces, StateBoundary: batch.StateBoundary, Risks: batch.Risks, Validation: batch.Validation, Recovery: batch.Recovery, HandoffContract: batch.Handoff, ContextCost: batch.ContextCost}
		for _, id := range batch.SubtaskIDs {
			task, exists := tasks[id]
			if !exists {
				return nil, fmt.Errorf("batch %s references unknown subtask %q", batch.ID, id)
			}
			if owner, exists := owners[id]; exists {
				return nil, fmt.Errorf("subtask %s is already assigned to batch %s", id, owner)
			}
			owners[id] = batch.ID
			record.Subtasks = append(record.Subtasks, task)
		}
		prior[batch.ID] = true
		plan.Batches = append(plan.Batches, record)
	}
	for _, task := range input.Subtasks {
		if _, exists := owners[task.ID]; !exists {
			return nil, fmt.Errorf("subtask %s is not assigned to any batch", task.ID)
		}
	}
	return plan, nil
}
