package protocol

import (
	"slices"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/runtime"
)

// FlowProgress is the read-only flow-structure projection the panel renders:
// the confirmed development plan's batches with their task status, and which
// slice children have actually entered the mainline. All facts come from the
// loaded engine state; nothing here is inferred or persisted.
type FlowProgress struct {
	// PlanConfirmed reports whether a development plan has been confirmed;
	// before that the batch list is empty and the canvas is schematic.
	PlanConfirmed        bool                       `json:"planConfirmed"`
	DevelopmentBatches   []DevelopmentBatchProgress `json:"developmentBatches,omitempty"`
	EnteredMainlineSlice []string                   `json:"enteredMainlineSlices,omitempty"`
}

// DevelopmentBatchProgress is one batch of the confirmed plan. Required is
// false for batches the engine does not require under this run's merge
// ownership; their Status stays empty and the owning slice child's own run
// supplies its progress instead.
type DevelopmentBatchProgress struct {
	Gate        *DevelopmentGateProgress `json:"gate,omitempty"`
	ID          string                   `json:"id"`
	Owner       string                   `json:"owner,omitempty"`
	Deliverable string                   `json:"deliverable"`
	DependsOn   []string                 `json:"dependsOn,omitempty"`
	Required    bool                     `json:"required"`
	Status      runtime.TaskStatus       `json:"status,omitempty"`
}

// DevelopmentGateProgress is derived from the selected ordinary gates and
// their original batch/candidate duties; it carries no control authority.
type DevelopmentGateProgress struct {
	Status       string   `json:"status"`
	Selected     []string `json:"selected"`
	Candidate    string   `json:"candidate,omitempty"`
	SourceAction string   `json:"sourceAction,omitempty"`
}

func (s *State) developmentGateProgress(task runtime.TaskKey, batch DevelopmentBatch) *DevelopmentGateProgress {
	progress := &DevelopmentGateProgress{Status: "QUEUED", Selected: s.developmentGates()}
	if batch.Spike {
		progress.Status = "NOT_APPLICABLE"
	} else if len(progress.Selected) == 0 {
		progress.Status = "DISABLED"
	} else if s.developmentGatesComplete(task) {
		progress.Status = "PASS"
	}
	if result := s.developmentResultRecord(task); result != nil {
		progress.SourceAction = result.ActionID
		if proof, frozen := s.editSourceProof(result.ActionID); frozen {
			progress.Candidate = proof.Identity
		}
	}
	if progress.Status != "QUEUED" || s.Validation == nil {
		return progress
	}
	for i := len(s.Validation.Waves) - 1; i >= 0; i-- {
		wave := &s.Validation.Waves[i]
		if !slices.Contains(wave.SourceTasks, task) || wave.Generation != s.Validation.Generation || wave.RouteDigest != s.validationRouteDigest() {
			continue
		}
		for _, target := range wave.Targets {
			if target.Kind != "GATE" || target.BatchID != "" && target.BatchID != batch.ID {
				continue
			}
			if target.Status == "FAIL" {
				progress.Status = "FAIL"
				return progress
			}
			if s.TaskStatusOf(target.Task) != runtime.TaskQueued && target.Status != "PASS" {
				progress.Status = "RUNNING"
			}
		}
		break
	}
	return progress
}

// flowProgress reads the plan and slice facts off the loaded state. Batches
// the engine does not require under this run's merge ownership are listed
// without status; a slice child's own run supplies its own progress.
func (s *State) flowProgress(cd *compiler.CompiledDefinition) FlowProgress {
	progress := FlowProgress{}
	_, plan, _ := s.confirmedDevelopmentPlan()
	if plan != nil {
		progress.PlanConfirmed = true
		for _, batch := range plan.Batches {
			entry := DevelopmentBatchProgress{
				ID:          batch.ID,
				Owner:       batch.Owner,
				Deliverable: batch.Deliverable,
				DependsOn:   slices.Clone(batch.DependsOn),
			}
			if s.developmentBatchRequired(batch) {
				entry.Required = true
				task := s.currentPlanTask(cd, plan, batch.ID)
				entry.Status = s.TaskStatusOf(task)
				entry.Gate = s.developmentGateProgress(task, batch)
			}
			progress.DevelopmentBatches = append(progress.DevelopmentBatches, entry)
		}
	}
	for sliceID, childID := range s.SliceChildren {
		if s.SliceEnteredMainline(childID) {
			progress.EnteredMainlineSlice = append(progress.EnteredMainlineSlice, sliceID)
		}
	}
	slices.Sort(progress.EnteredMainlineSlice)
	return progress
}
