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
	ID          string             `json:"id"`
	Owner       string             `json:"owner,omitempty"`
	Deliverable string             `json:"deliverable"`
	DependsOn   []string           `json:"dependsOn,omitempty"`
	Required    bool               `json:"required"`
	Status      runtime.TaskStatus `json:"status,omitempty"`
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
				entry.Status = s.TaskStatusOf(s.currentPlanTask(cd, plan, batch.ID))
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
