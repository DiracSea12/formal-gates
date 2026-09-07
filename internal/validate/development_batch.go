package validate

import (
	"fmt"
	"strings"
)

// DevelopmentBatchPlan binds the confirmed work contract. Dispatch records are
// the only progress authority; Current/Status remain solely for old run decoding.
type DevelopmentBatchPlan struct {
	Digest              string `json:"digest,omitempty"`
	UserReason          string `json:"userReason,omitempty"`
	RequirementRevision string `json:"requirementRevision,omitempty"`
	// Keep the legacy zero value on the wire: integrity is checked before migration.
	Current                 int                      `json:"current"`
	Batches                 []DevelopmentBatchRecord `json:"batches"`
	Version                 int                      `json:"version,omitempty"`
	TechnicalReviewDispatch string                   `json:"technicalReviewDispatch,omitempty"`
	ReviewMasterRunID       string                   `json:"reviewMasterRunId,omitempty"`
}

type DevelopmentBatchRecord struct {
	ID              string               `json:"id"`
	Task            string               `json:"task"`
	DoD             string               `json:"dod"`
	Status          string               `json:"status,omitempty"`
	DispatchID      string               `json:"dispatchId,omitempty"`
	Snapshot        string               `json:"snapshot,omitempty"`
	Verification    string               `json:"verification,omitempty"`
	Handoff         string               `json:"handoff,omitempty"`
	Subtasks        []DevelopmentSubtask `json:"subtasks,omitempty"`
	DependsOn       []string             `json:"dependsOn,omitempty"`
	Interfaces      string               `json:"interfaces,omitempty"`
	StateBoundary   string               `json:"stateBoundary,omitempty"`
	Risks           string               `json:"risks,omitempty"`
	Validation      []string             `json:"validation,omitempty"`
	Recovery        string               `json:"recovery,omitempty"`
	HandoffContract string               `json:"handoffContract,omitempty"`
	ContextCost     string               `json:"contextCost,omitempty"`
	TaskKey         string               `json:"taskKey,omitempty"`
}

// normalizeLegacyDevelopmentPlan performs the one-way compatibility migration
// in memory. Completed legacy records become completed development dispatches;
// afterwards the plan contains only its immutable task text and binding.
func normalizeLegacyDevelopmentPlan(state *RunState) error {
	plan := state.DevelopmentBatchPlan
	if plan == nil {
		return nil
	}
	if plan.Version == 1 {
		if plan.RequirementRevision != state.RequirementRevision {
			return fmt.Errorf("development batch plan requirement revision is stale")
		}
		return nil
	}
	if strings.TrimSpace(plan.Digest) == "" || strings.TrimSpace(plan.UserReason) == "" || len(plan.Batches) < 2 {
		return fmt.Errorf("legacy development batch plan is incomplete")
	}
	if plan.RequirementRevision == "" {
		plan.RequirementRevision = state.RequirementRevision
	}
	if plan.RequirementRevision != state.RequirementRevision {
		return fmt.Errorf("legacy development batch plan is bound to requirement revision %s, current revision is %s", plan.RequirementRevision, state.RequirementRevision)
	}
	if state.Actions["development-worker"].Status == "BATCH_PENDING" {
		state.Actions["development-worker"] = ActionResult{Status: developmentPending}
	}
	if state.Dispatches == nil {
		state.Dispatches = map[string]PreparedDispatch{}
	}
	seenIDs := map[string]bool{}
	for index := range plan.Batches {
		batch := &plan.Batches[index]
		batch.ID = strings.TrimSpace(batch.ID)
		batch.Task = strings.TrimSpace(batch.Task)
		batch.DoD = strings.TrimSpace(batch.DoD)
		if batch.ID == "" || batch.Task == "" || batch.DoD == "" || seenIDs[batch.ID] {
			return fmt.Errorf("legacy development batch %d has invalid id, task, or definition of done", index+1)
		}
		seenIDs[batch.ID] = true
		status := strings.ToUpper(strings.TrimSpace(batch.Status))
		dispatch, found := legacyBatchDispatch(*state, *batch)
		if status == "COMPLETE" {
			if !found {
				id := fmt.Sprintf("legacy-development-batch-%d", index+1)
				if _, exists := state.Dispatches[id]; exists {
					return fmt.Errorf("cannot restore completed legacy batch %s: dispatch id %s is already used", batch.ID, id)
				}
				dispatch = PreparedDispatch{ID: id, Target: "development-worker", TargetKind: "action", Attempt: index + 1, RequirementRevision: state.RequirementRevision, CatalogRevision: state.CatalogRevision, SourceSnapshot: legacyBatchSource(*state, index), Status: "COMPLETED"}
			}
			dispatch.BatchID = batch.ID
			dispatch.Status = "COMPLETED"
			dispatch.CompletionSnapshot = batch.Snapshot
			dispatch.Verification = batch.Verification
			dispatch.Handoff = batch.Handoff
			state.Dispatches[dispatch.ID] = dispatch
		} else if found {
			dispatch.BatchID = batch.ID
			state.Dispatches[dispatch.ID] = dispatch
		}
		// Remove the obsolete progress copy. These fields remain in the decoder
		// solely so an old state can be migrated without losing evidence.
		batch.Status = ""
		batch.DispatchID = ""
		batch.Snapshot = ""
		batch.Verification = ""
		batch.Handoff = ""
	}
	plan.Current = 0
	seenIncomplete := false
	for _, batch := range plan.Batches {
		dispatch, found := developmentDispatchForBatch(*state, batch.ID)
		complete := found && dispatch.Status == "COMPLETED"
		if complete && seenIncomplete {
			return fmt.Errorf("legacy development batch %s is complete after an incomplete batch", batch.ID)
		}
		if !complete {
			seenIncomplete = true
		}
	}
	return nil
}

func legacyBatchDispatch(state RunState, batch DevelopmentBatchRecord) (PreparedDispatch, bool) {
	if batch.DispatchID != "" {
		dispatch, ok := state.Dispatches[batch.DispatchID]
		return dispatch, ok
	}
	return developmentDispatchForBatch(state, batch.ID)
}

func legacyBatchSource(state RunState, index int) string {
	if index == 0 {
		return state.BaseSnapshot
	}
	previous := state.DevelopmentBatchPlan.Batches[index-1]
	if previous.Snapshot != "" {
		return previous.Snapshot
	}
	return state.CurrentSnapshot
}

func developmentDispatchForBatch(state RunState, batchID string) (PreparedDispatch, bool) {
	var selected PreparedDispatch
	found := false
	for _, dispatch := range state.Dispatches {
		if dispatch.TargetKind != "action" || dispatch.Target != "development-worker" || dispatch.BatchID != batchID {
			continue
		}
		// Attempt is scoped to TaskKey. An explicit replacement can start at 1
		// after a higher-numbered attempt on the old key has become STALE.
		live := dispatch.Status == "OPEN" || dispatch.Status == "CLAIMED"
		selectedLive := selected.Status == "OPEN" || selected.Status == "CLAIMED"
		if !found || dispatch.Status == "COMPLETED" || (selected.Status != "COMPLETED" &&
			((live && !selectedLive) || (live == selectedLive && dispatch.Attempt > selected.Attempt))) {
			selected, found = dispatch, true
		}
	}
	return selected, found
}

func activeDevelopmentBatch(state RunState) (int, DevelopmentBatchRecord, bool) {
	if state.DevelopmentBatchPlan == nil {
		return 0, DevelopmentBatchRecord{}, false
	}
	for index, batch := range state.DevelopmentBatchPlan.Batches {
		dispatch, ok := developmentDispatchForBatch(state, batch.ID)
		if !ok || dispatch.Status != "COMPLETED" {
			return index, batch, true
		}
	}
	return len(state.DevelopmentBatchPlan.Batches), DevelopmentBatchRecord{}, false
}

func hasCompletedDevelopmentBatch(state RunState) bool {
	if state.DevelopmentBatchPlan == nil {
		return false
	}
	for _, batch := range state.DevelopmentBatchPlan.Batches {
		if dispatch, ok := developmentDispatchForBatch(state, batch.ID); ok && dispatch.Status == "COMPLETED" {
			return true
		}
	}
	return false
}

func developmentBatchPromptDetail(state RunState) string {
	index, batch, ok := activeDevelopmentBatch(state)
	if !ok {
		return ""
	}
	plan := state.DevelopmentBatchPlan
	detail := fmt.Sprintf("Previously confirmed legacy serial development batch %d/%d (%s). Work only on this batch; do not begin a later batch.\n\nTask:\n%s\n\nDefinition of done:\n%s", index+1, len(plan.Batches), batch.ID, batch.Task, batch.DoD)
	if plan.Version == 1 {
		detail = fmt.Sprintf("Confirmed development batch %d/%d (%s), plan %s. Work only on this batch. Subtasks are ordered steps within this dispatch, not separate agent tasks.\n\nTask:\n%s\n\nDefinition of done:\n%s", index+1, len(plan.Batches), batch.ID, plan.Digest, batch.Task, batch.DoD)
		for i, subtask := range batch.Subtasks {
			detail += fmt.Sprintf("\n%d. [%s] %s", i+1, subtask.ID, subtask.Task)
		}
		detail += fmt.Sprintf("\n\nDependencies: %s\nInterfaces: %s\nState boundary: %s\nRisks: %s\nValidation:\n%s\nRecovery: %s\nHandoff contract: %s\nContext cost: %s", strings.Join(batch.DependsOn, ", "), batch.Interfaces, batch.StateBoundary, batch.Risks, strings.Join(batch.Validation, "\n"), batch.Recovery, batch.HandoffContract, batch.ContextCost)
		for _, dependency := range batch.DependsOn {
			if index > 0 && dependency == plan.Batches[index-1].ID {
				continue
			}
			if predecessor, found := developmentDispatchForBatch(state, dependency); found {
				detail += fmt.Sprintf("\n\nDependency %s handoff:\n%s", dependency, predecessor.Handoff)
			}
		}
	}
	if index > 0 {
		if previous, found := developmentDispatchForBatch(state, plan.Batches[index-1].ID); found && strings.TrimSpace(previous.Handoff) != "" {
			detail += "\n\nPrevious batch handoff:\n" + strings.TrimSpace(previous.Handoff)
		}
	}
	return detail
}

func requireDevelopmentBatchDispatch(state RunState, dispatch PreparedDispatch, batch DevelopmentBatchRecord) error {
	if dispatch.BatchID != batch.ID {
		return fmt.Errorf("dispatch %q is not bound to current development batch %s", dispatch.ID, batch.ID)
	}
	return nil
}

type DevelopmentBatchSnapshotOptions struct {
	UserRequested bool
	Reason        string
}

func AdvanceDevelopmentBatch(root, packageRoot, runID, dispatchID, batchID, verification, handoff string, options ...DevelopmentBatchSnapshotOptions) (RunState, error) {
	input := developmentBatchSnapshotInput{BatchID: strings.TrimSpace(batchID), Verification: strings.TrimSpace(verification), Handoff: strings.TrimSpace(handoff)}
	if input.Verification == "" || input.Handoff == "" {
		return RunState{}, fmt.Errorf("a batch snapshot requires --verification and --handoff")
	}
	option := DevelopmentBatchSnapshotOptions{}
	if len(options) > 0 {
		option = options[0]
	}
	return advanceSnapshot(root, packageRoot, runID, dispatchID, option.UserRequested, option.Reason, input)
}

func completeIntermediateDevelopmentBatch(state *RunState, dispatch PreparedDispatch, snapshot string, input developmentBatchSnapshotInput) error {
	index, batch, ok := activeDevelopmentBatch(*state)
	if !ok {
		return fmt.Errorf("no active legacy development batch plan is registered")
	}
	if index >= len(state.DevelopmentBatchPlan.Batches)-1 {
		return fmt.Errorf("development batch %s is final; record it with the ordinary workflow snapshot command", batch.ID)
	}
	if input.BatchID != batch.ID {
		return fmt.Errorf("batch id %q is not the current development batch %s", input.BatchID, batch.ID)
	}
	if err := requireDevelopmentBatchDispatch(*state, dispatch, batch); err != nil {
		return err
	}
	dispatch.CompletionSnapshot = snapshot
	dispatch.Verification = input.Verification
	dispatch.Handoff = input.Handoff
	state.Dispatches[dispatch.ID] = dispatch
	completeDispatch(state, dispatch.ID)
	state.CurrentSnapshot = snapshot
	state.Actions["development-worker"] = ActionResult{Status: developmentPending}
	return nil
}

func guardFinalDevelopmentBatchSnapshot(state RunState, dispatchID string) error {
	index, batch, ok := activeDevelopmentBatch(state)
	if !ok {
		return nil
	}
	if index < len(state.DevelopmentBatchPlan.Batches)-1 {
		return fmt.Errorf("development batch %s is not final; record it with workflow snapshot --batch-id %s", batch.ID, batch.ID)
	}
	dispatch, ok := state.Dispatches[strings.TrimSpace(dispatchID)]
	if !ok {
		return fmt.Errorf("unknown dispatch %q", strings.TrimSpace(dispatchID))
	}
	return requireDevelopmentBatchDispatch(state, dispatch, batch)
}

func completeFinalDevelopmentBatch(state *RunState, dispatchID, snapshot string, inputs ...developmentBatchSnapshotInput) {
	dispatch, ok := state.Dispatches[dispatchID]
	if !ok || dispatch.BatchID == "" {
		return
	}
	dispatch.CompletionSnapshot = snapshot
	dispatch.Verification = "final development snapshot recorded; QA and gate evidence is stored in the run summary"
	dispatch.Handoff = "post-development QA and technical gates"
	if len(inputs) > 0 && inputs[0].BatchID != "" {
		dispatch.Verification = inputs[0].Verification
		dispatch.Handoff = inputs[0].Handoff
	}
	state.Dispatches[dispatchID] = dispatch
}

func developmentBatchSummary(state RunState) []DevelopmentBatchRecord {
	if state.DevelopmentBatchPlan == nil {
		return nil
	}
	result := make([]DevelopmentBatchRecord, 0, len(state.DevelopmentBatchPlan.Batches))
	for _, spec := range state.DevelopmentBatchPlan.Batches {
		record := spec
		record.Status = "PENDING"
		if dispatch, ok := developmentDispatchForBatch(state, spec.ID); ok {
			record.DispatchID = dispatch.ID
			record.TaskKey = dispatch.TaskKey
			if dispatch.Status == "COMPLETED" {
				record.Status = "COMPLETE"
			} else if dispatch.Status == "OPEN" || dispatch.Status == "CLAIMED" {
				record.Status = "PREPARED"
			} else {
				record.Status = dispatch.Status
			}
			record.Snapshot = dispatch.CompletionSnapshot
			record.Verification = dispatch.Verification
			record.Handoff = dispatch.Handoff
		}
		result = append(result, record)
	}
	return result
}

func invalidateLegacyDevelopmentPlan(state *RunState) {
	state.DevelopmentBatchPlan = nil
	for id, dispatch := range state.Dispatches {
		if dispatch.BatchID != "" {
			dispatch.BatchID = ""
			dispatch.CompletionSnapshot = ""
			dispatch.Verification = ""
			dispatch.Handoff = ""
			state.Dispatches[id] = dispatch
		}
	}
}
