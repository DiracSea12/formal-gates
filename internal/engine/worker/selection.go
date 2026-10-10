package worker

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"

	"formal-gates/internal/engine/protocol"
)

func selectIDs(indices []int, ids []string) ([]string, error) {
	result := []string{}
	for _, index := range indices {
		if index < 1 || index > len(ids) {
			return nil, fmt.Errorf("selection %d is outside the issued choices", index)
		}
		id := ids[index-1]
		if slices.Contains(result, id) {
			return nil, fmt.Errorf("selection repeats choice %d", index)
		}
		result = append(result, id)
	}
	return result, nil
}
func findingStatus(values []Finding) string {
	for _, item := range values {
		if item.Severity == "P0" || item.Severity == "P1" {
			return protocol.OutcomeFail
		}
	}
	return protocol.OutcomePass
}
func executionCaseIDs(input *protocol.ValidationInput) []string {
	ids := []string{}
	for _, fact := range input.ExecutionFacts {
		if slices.Contains(input.RequiredCases, fact.CaseID) && slices.Contains(input.PendingCases, fact.CaseID) && !slices.Contains(ids, fact.CaseID) {
			ids = append(ids, fact.CaseID)
		}
	}
	return ids
}
func repairCaseIDs(input *protocol.ValidationInput) []string {
	ids := []string{}
	if input.Repair == nil || input.Contract == nil {
		return ids
	}
	for _, manifest := range input.Contract.Manifests {
		for _, item := range manifest.Cases {
			if !slices.Contains(ids, item.CaseID) {
				ids = append(ids, item.CaseID)
			}
		}
	}
	return ids
}
func observations(values []*Observation, ids []string) (map[string]Observation, error) {
	if len(values) != len(ids) {
		return nil, fmt.Errorf("comparisons must follow the issued case order")
	}
	result := map[string]Observation{}
	for index, value := range values {
		if value != nil {
			result[ids[index]] = *value
		}
	}
	return result, nil
}
func assembleCompletion(value *Completion, batch *protocol.DevelopmentBatch) (*protocol.DevelopmentCompletion, error) {
	if value == nil {
		return nil, nil
	}
	if batch == nil || len(value.DoD) != len(batch.DoD) {
		return nil, fmt.Errorf("completion must follow the issued criteria order")
	}
	result := &protocol.DevelopmentCompletion{DoD: map[string]string{}, Handoff: value.Handoff}
	for index, item := range batch.DoD {
		result.DoD[item.ID] = value.DoD[index]
	}
	return result, nil
}
func DecodeGranularity(input protocol.WorkerInput, value *Granularity, paths []string, facts []LocationFact, completed bool) (*protocol.GranularityReview, error) {
	if value == nil {
		return nil, nil
	}
	result := &protocol.GranularityReview{Reason: value.Reason, VerificationTier: value.VerificationTier, CheckpointReason: value.CheckpointReason}
	ids := []string{}
	for index := range value.Batches {
		ids = append(ids, fmt.Sprintf("batch-%d", index+1))
	}
	for _, group := range value.CheckpointGroups {
		selected, err := selectIDs(group, ids)
		if err != nil {
			return nil, err
		}
		result.CheckpointGroups = append(result.CheckpointGroups, selected)
	}
	for index, item := range value.Batches {
		batch := protocol.DevelopmentBatch{Deliverable: item.Deliverable, PublicEntry: item.PublicEntry, Interfaces: item.Interfaces, StateBoundary: item.StateBoundary, Risk: item.Risk, Rollback: item.Rollback, Recovery: item.Recovery, Handoff: item.Handoff, ContextCost: item.ContextCost, Spike: item.Spike}
		batch.ID = ids[index]
		batch.Route = nil
		var err error
		if completed {
			if len(value.Batches) != 1 || paths == nil || item.Artifacts != nil || item.NewArtifacts != nil {
				return nil, fmt.Errorf("completed repair artifacts require original single-batch native paths")
			}
			batch.Artifacts = slices.Clone(paths)
		} else {
			for _, location := range item.Artifacts {
				if location.Line != 0 || location.RequirementLine != 0 {
					return nil, fmt.Errorf("artifact selects an original file, not a source line")
				}
			}
			batch.Artifacts, err = LocationPaths(item.Artifacts, facts, input.RequirementDocument)
			if err != nil {
				return nil, err
			}
			for _, name := range item.NewArtifacts {
				for _, fact := range facts {
					if slices.Contains(fact.Paths, name) {
						return nil, fmt.Errorf("existing artifact requires its original location selection")
					}
				}
				batch.Artifacts = append(batch.Artifacts, name)
			}
		}
		batch.DependsOn, err = selectIDs(item.DependsOn, ids)
		if err != nil {
			return nil, err
		}
		if item.Owner != nil {
			if *item.Owner < 0 {
				return nil, fmt.Errorf("owner must be a nonnegative slice choice")
			}
			if *item.Owner == 0 {
				batch.Owner = "master-merge"
			} else {
				batch.Owner = fmt.Sprintf("slice-%d", *item.Owner)
			}
		}
		for i, sub := range item.Subtasks {
			batch.Subtasks = append(batch.Subtasks, protocol.DevelopmentSubtask{ID: fmt.Sprintf("%s-task-%d", batch.ID, i+1), Description: sub.Description})
		}
		for i, dod := range item.DoD {
			batch.DoD = append(batch.DoD, protocol.DevelopmentCriterion{ID: fmt.Sprintf("criterion-%d", i+1), Description: dod.Description})
		}
		if input.Handler == "engine.repair" {
			for _, check := range input.ApprovedChecks {
				if check.Purpose == protocol.CheckFullBuild || check.Purpose == protocol.CheckFullTests {
					batch.Checks = append(batch.Checks, check)
				}
			}
		}
		for i, check := range item.Checks {
			selected := protocol.DevelopmentCheck{ID: fmt.Sprintf("%s-check-%d", batch.ID, i+1), Purpose: check.Purpose, Command: check.Command}
			if check.Reuse != 0 {
				if check.Reuse < 1 || check.Reuse > len(input.ApprovedChecks) || check.Command != "" || check.Purpose != "" {
					return nil, fmt.Errorf("check reuse selects only an original approved check")
				}
				selected = input.ApprovedChecks[check.Reuse-1]
			} else {
				for _, approved := range input.ApprovedChecks {
					if approved.Command == check.Command {
						return nil, fmt.Errorf("approved command requires its original check selection")
					}
				}
				if check.Command == "" || input.Handler == "engine.repair" && len(input.ApprovedChecks) > 0 && check.Purpose != "" {
					return nil, fmt.Errorf("repair full checks come from the confirmed plan; author only new local commands")
				}
			}
			if !slices.ContainsFunc(batch.Checks, func(existing protocol.DevelopmentCheck) bool {
				return existing.Purpose == selected.Purpose && existing.Command == selected.Command
			}) {
				batch.Checks = append(batch.Checks, selected)
			}
		}
		if item.Route != nil {
			gates := slices.Clone(input.AvailableRouteCandidates)
			selected, err := selectIDs(item.Route.Selected, gates)
			if err != nil {
				return nil, err
			}
			batch.Route = &protocol.RouteDecision{Mode: item.Route.Mode, Gates: gates, Selected: selected}
		}
		result.Batches = append(result.Batches, batch)
	}
	return result, nil
}

func assembleDesign(input protocol.WorkerInput, value *Design) (*Manifest, error) {
	issued := input.Validation.Manifest
	caseIDs, stepIDs := []string{}, []string{}
	for _, item := range issued.Cases {
		caseIDs = append(caseIDs, item.CaseID)
	}
	for _, item := range issued.SharedSteps {
		stepIDs = append(stepIDs, item.ID)
	}
	retainedCases, err := selectIDs(value.RetainCases, caseIDs)
	if err != nil {
		return nil, err
	}
	retainedSteps, err := selectIDs(value.RetainSharedSteps, stepIDs)
	if err != nil {
		return nil, err
	}
	result := &Manifest{RetainCases: retainedCases, RetainSharedSteps: retainedSteps, Coverage: map[string][]string{}}
	for index, item := range value.SharedSteps {
		id := contentID("shared", struct {
			Binding any
			Item    any
		}{issued.Binding, item}, index)
		result.SharedSteps = append(result.SharedSteps, SharedStep{ID: id, Procedure: item.Procedure, Operations: item.Operations, Files: item.Files})
		stepIDs = append(stepIDs, id)
	}
	selectedCaseIDs := slices.Clone(retainedCases)
	for index, item := range value.Cases {
		id := contentID("case", struct {
			Binding any
			Item    any
		}{issued.Binding, item}, index)
		steps, err := selectIDs(item.SharedSteps, stepIDs)
		if err != nil {
			return nil, err
		}
		result.Cases = append(result.Cases, Case{CaseID: id, Files: item.Files, Operations: item.Operations, Scenario: item.Scenario, SharedSteps: steps, PublicEntry: item.PublicEntry, Preconditions: item.Preconditions, Steps: item.Steps, Oracle: item.Oracle, TestRef: &item.TestRef, TestFile: item.TestFile})
		selectedCaseIDs = append(selectedCaseIDs, id)
	}
	if len(value.Coverage) != len(issued.Points) {
		return nil, fmt.Errorf("coverage must follow issued acceptance-point order")
	}
	for index, choices := range value.Coverage {
		if choices == nil {
			continue
		}
		ids, err := selectIDs(choices, selectedCaseIDs)
		if err != nil {
			return nil, err
		}
		result.Coverage[issued.Points[index].PointID] = ids
	}
	return result, nil
}

func contentID(kind string, value any, index int) string {
	body, _ := json.Marshal(struct {
		Value any
		Index int
	}{value, index})
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%s-%x", kind, sum[:12])
}
