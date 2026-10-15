package worker

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/requirement"
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
	var err error
	result.RequirementResults, err = assemblePointEvidence(batch.RequirementPoints, value.Requirements)
	if err != nil {
		return nil, err
	}
	return result, nil
}
func assemblePointEvidence(ids []string, evidence []string) ([]requirement.PointResult, error) {
	if len(evidence) != len(ids) {
		return nil, fmt.Errorf("completion must follow the issued requirement-point order")
	}
	results := []requirement.PointResult{}
	for index, id := range ids {
		if strings.TrimSpace(evidence[index]) == "" {
			return nil, fmt.Errorf("requirement completion requires actual evidence")
		}
		results = append(results, requirement.PointResult{PointID: id, Status: protocol.OutcomePass, Evidence: evidence[index]})
	}
	if err := requirement.ValidatePointResults(ids, results); err != nil {
		return nil, err
	}
	return results, nil
}
func assembleRequirementDecisions(points []requirement.Point, values []RequirementDecision, findingCount int) ([]protocol.RequirementDecision, error) {
	if len(values) != len(points) {
		return nil, fmt.Errorf("review must follow the issued requirement-point order")
	}
	result := []protocol.RequirementDecision{}
	for index, value := range values {
		item := protocol.RequirementDecision{PointID: points[index].ID, Status: value.Status, Evidence: value.Evidence}
		for _, choice := range value.Findings {
			if choice < 1 || choice > findingCount {
				return nil, fmt.Errorf("requirement review selects original findings once")
			}
			item.FindingIndexes = append(item.FindingIndexes, choice-1)
		}
		result = append(result, item)
	}
	if err := protocol.ValidateRequirementDecisions(points, result, findingCount); err != nil {
		return nil, err
	}
	return result, nil
}
func proposesVerificationGoals(input protocol.WorkerInput) bool {
	return input.Review != nil || input.Handler == "engine.development.plan.v1"
}

func DecodeGranularity(input protocol.WorkerInput, value *Granularity, paths []string, facts []LocationFact, completed bool) (*protocol.GranularityReview, error) {
	if value == nil {
		return nil, nil
	}
	result := &protocol.GranularityReview{Reason: value.Reason, VerificationTier: value.VerificationTier, CheckpointReason: value.CheckpointReason}
	batches := value.Batches
	if input.ApprovedPlan != nil && !completed {
		original := input.ApprovedPlan
		result.Reason, result.VerificationTier, result.CheckpointReason = original.Reason, original.VerificationTier, original.CheckpointReason
		for _, group := range original.CheckpointGroups {
			result.CheckpointGroups = append(result.CheckpointGroups, slices.Clone(group))
		}
		change := value.changes
		if change.Reason != nil {
			result.Reason = *change.Reason
		}
		if change.VerificationTier != nil {
			result.VerificationTier = *change.VerificationTier
		}
		if change.CheckpointReason != nil {
			result.CheckpointReason = *change.CheckpointReason
		}
		if change.CheckpointGroups != nil {
			result.CheckpointGroups = nil
		}
		if change.VerificationTier != nil && result.VerificationTier != "CHECKPOINT" {
			if change.CheckpointGroups == nil {
				result.CheckpointGroups = nil
			}
			if change.CheckpointReason == nil {
				result.CheckpointReason = ""
			}
		}
		if change.Batches == nil || len(batches) > 0 {
			batches = nil
			for i := range original.Batches {
				batches = append(batches, Batch{Reuse: i + 1, changes: &BatchChange{}})
			}
			seen := map[int]bool{}
			for _, item := range value.Batches {
				if item.Reuse == 0 {
					if item.Remove {
						return nil, fmt.Errorf("batch removal selects an original confirmed batch")
					}
					batches = append(batches, item)
					continue
				}
				if item.Reuse < 1 || item.Reuse > len(original.Batches) || seen[item.Reuse] {
					return nil, fmt.Errorf("batch change selects each original confirmed batch at most once")
				}
				seen[item.Reuse] = true
				if item.Remove {
					raw, err := json.Marshal(item.changes)
					if err != nil {
						return nil, err
					}
					if string(raw) != "{}" {
						return nil, fmt.Errorf("removed batch cannot also change content")
					}
				}
				batches[item.Reuse-1] = item
			}
			batches = slices.DeleteFunc(batches, func(item Batch) bool { return item.Remove })
		}
	}
	ids := []string{}
	for index, item := range batches {
		id := fmt.Sprintf("batch-%d", index+1)
		if input.ApprovedPlan != nil && item.Reuse == 0 {
			id = contentID("batch", item, index)
		}
		if item.Reuse != 0 {
			if input.ApprovedPlan == nil || item.Reuse < 1 || item.Reuse > len(input.ApprovedPlan.Batches) {
				return nil, fmt.Errorf("batch reuse selects an original confirmed batch")
			}
			id = input.ApprovedPlan.Batches[item.Reuse-1].ID
		}
		ids = append(ids, id)
	}
	for _, group := range value.CheckpointGroups {
		selected, err := selectIDs(group, ids)
		if err != nil {
			return nil, err
		}
		result.CheckpointGroups = append(result.CheckpointGroups, selected)
	}
	for index, item := range batches {
		batch := protocol.DevelopmentBatch{Deliverable: item.Deliverable, PublicEntry: item.PublicEntry, Interfaces: item.Interfaces, StateBoundary: item.StateBoundary, Risk: item.Risk, Rollback: item.Rollback, Recovery: item.Recovery, Handoff: item.Handoff, ContextCost: item.ContextCost, Spike: item.Spike}
		reused := item.Reuse != 0
		if reused {
			batch = input.ApprovedPlan.Batches[item.Reuse-1]
			change := *item.changes
			change.Owner, change.DependsOn, change.Subtasks, change.DoD, change.Checks, change.Artifacts, change.NewArtifacts, change.Route, change.Requirements = nil, nil, nil, nil, nil, nil, nil, nil, nil
			raw, err := json.Marshal(change)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(raw, &batch); err != nil {
				return nil, err
			}
		} else {
			batch.Route = nil
		}
		batch.ID = ids[index]
		var err error
		if completed {
			if reused {
				return nil, fmt.Errorf("completed repair uses current native artifacts, not a carried batch")
			}
			if len(batches) != 1 || paths == nil || item.Artifacts != nil || item.NewArtifacts != nil {
				return nil, fmt.Errorf("completed repair artifacts require original single-batch native paths")
			}
			batch.Artifacts = slices.Clone(paths)
		} else if !reused || item.changes.Artifacts != nil || item.changes.NewArtifacts != nil {
			batch.Artifacts = nil
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
		if !reused || item.changes.DependsOn != nil {
			batch.DependsOn, err = selectIDs(item.DependsOn, ids)
			if err != nil {
				return nil, err
			}
		}
		if !reused || item.changes.Requirements != nil {
			points := requirement.PointIDs(input.RequirementPoints)
			batch.RequirementPoints, err = selectIDs(item.Requirements, points)
			if err != nil {
				return nil, err
			}
			batch.RequirementPoints = requirement.PointIDs(requirement.SelectPoints(input.RequirementPoints, batch.RequirementPoints))
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
		if !reused {
			for i, sub := range item.Subtasks {
				if sub.Reuse != 0 || sub.Remove {
					return nil, fmt.Errorf("new subtask cannot select an original item")
				}
				batch.Subtasks = append(batch.Subtasks, protocol.DevelopmentSubtask{ID: fmt.Sprintf("%s-task-%d", batch.ID, i+1), Description: sub.Description})
			}
			for i, dod := range item.DoD {
				if dod.Reuse != 0 || dod.Remove {
					return nil, fmt.Errorf("new criterion cannot select an original item")
				}
				batch.DoD = append(batch.DoD, protocol.DevelopmentCriterion{ID: fmt.Sprintf("criterion-%d", i+1), Description: dod.Description})
			}
		} else {
			if item.changes.Subtasks != nil {
				batch.Subtasks, err = reviseSubtasks(batch.Subtasks, item.Subtasks, batch.ID)
				if err != nil {
					return nil, err
				}
			}
			if item.changes.DoD != nil {
				batch.DoD, err = reviseCriteria(batch.DoD, item.DoD)
				if err != nil {
					return nil, err
				}
			}
		}
		if !reused || item.changes.Checks != nil {
			batch.Checks = nil
			if input.Handler == "engine.repair.v7" {
				for _, check := range input.ApprovedChecks {
					if check.Purpose == protocol.CheckFullBuild || check.Purpose == protocol.CheckFullTests || check.Purpose == protocol.CheckEdit {
						batch.Checks = append(batch.Checks, check)
					}
				}
			}
			for i, check := range item.Checks {
				selected := protocol.DevelopmentCheck{ID: fmt.Sprintf("%s-check-%d", batch.ID, i+1), Purpose: check.Purpose, Command: check.Command, Goal: check.Goal}
				if proposesVerificationGoals(input) && check.Command != "" {
					return nil, fmt.Errorf("plan preparation reuses an approved check or proposes a verification goal")
				}
				if check.Goal != "" {
					if !proposesVerificationGoals(input) || check.Reuse != 0 {
						return nil, fmt.Errorf("plan preparation declares a missing verification goal without a reused check")
					}
				} else if check.Reuse != 0 {
					if check.Reuse < 1 || check.Reuse > len(input.ApprovedChecks) || check.Command != "" || check.Purpose != "" || check.Goal != "" {
						return nil, fmt.Errorf("check reuse selects only an original approved check")
					}
					selected = input.ApprovedChecks[check.Reuse-1]
				} else {
					for _, approved := range input.ApprovedChecks {
						if approved.Command == selected.Command {
							return nil, fmt.Errorf("approved command requires its original check selection")
						}
					}
					if selected.Command == "" || input.Handler == "engine.repair.v7" && len(input.ApprovedChecks) > 0 && check.Purpose != "" {
						return nil, fmt.Errorf("repair full checks come from the confirmed plan; author only new local commands")
					}
				}
				if !slices.ContainsFunc(batch.Checks, func(existing protocol.DevelopmentCheck) bool {
					return existing.Purpose == selected.Purpose && existing.Command == selected.Command && existing.Goal == selected.Goal
				}) {
					batch.Checks = append(batch.Checks, selected)
				}
			}
		}
		if item.Route != nil {
			gates := slices.Clone(input.AvailableRouteCandidates)
			selected, err := selectIDs(item.Route.Selected, gates)
			if err != nil {
				return nil, err
			}
			batch.Route = &protocol.RouteDecision{Gates: gates, Selected: selected}
		}
		result.Batches = append(result.Batches, batch)
	}
	return result, nil
}

func assembleDesign(input protocol.WorkerInput, value *Design, facts []LocationFact) (*Manifest, error) {
	issued := input.Validation.Manifest
	caseIDs, stepIDs := []string{}, []string{}
	for _, item := range issued.Cases {
		caseIDs = append(caseIDs, item.CaseID)
	}
	for _, item := range issued.SharedSteps {
		stepIDs = append(stepIDs, item.ID)
	}
	removedCases, err := selectIDs(value.RemoveCases, caseIDs)
	if err != nil {
		return nil, err
	}
	removedSteps, err := selectIDs(value.RemoveSharedSteps, stepIDs)
	if err != nil {
		return nil, err
	}
	if value.Replace && (len(removedCases) > 0 || len(removedSteps) > 0) {
		return nil, fmt.Errorf("replacement cannot also remove individual items")
	}
	result := &Manifest{Replace: value.Replace, RemoveCases: removedCases, RemoveSharedSteps: removedSteps, Coverage: map[string][]string{}}
	for index, item := range value.SharedSteps {
		id := ""
		if item.Step != 0 {
			selected, err := selectIDs([]int{item.Step}, stepIDs[:len(issued.SharedSteps)])
			if err != nil {
				return nil, err
			}
			id = selected[0]
		}
		step := SharedStep{Artifacts: item.Artifacts, Independent: item.Independent, ID: id, Procedure: item.Procedure, Operations: item.Operations, Files: item.Files}
		if item.Step != 0 {
			original := issued.SharedSteps[item.Step-1]
			step = SharedStep{ID: original.ID, Artifacts: slices.Clone(original.Artifacts), Independent: original.Independent, Procedure: original.Procedure, Operations: slices.Clone(original.Operations)}
			if err := applySharedStepChange(&step, *item.changes, facts); err != nil {
				return nil, err
			}
		} else {
			if item.Procedure == "" {
				return nil, fmt.Errorf("new shared step requires a procedure")
			}
			if err := applySharedStepChange(&step, *item.changes, facts); err != nil {
				return nil, err
			}
		}
		if item.Step == 0 {
			item.Operations, item.Artifacts = step.Operations, step.Artifacts
			id = contentID("shared", struct {
				Binding any
				Item    any
			}{issued.Binding, item}, index)
			step.ID = id
		}
		result.SharedSteps = append(result.SharedSteps, step)
		if !slices.Contains(stepIDs, id) {
			stepIDs = append(stepIDs, id)
		}
	}
	selectedCaseIDs := slices.Clone(caseIDs)
	if value.Replace {
		selectedCaseIDs = nil
	}
	for index, item := range value.Cases {
		id := ""
		if item.Case != 0 {
			selected, err := selectIDs([]int{item.Case}, caseIDs)
			if err != nil {
				return nil, err
			}
			id = selected[0]
		}
		steps, err := selectIDs(item.SharedSteps, stepIDs)
		if err != nil {
			return nil, err
		}
		entry := Case{CaseID: id, Files: item.Files, Operations: item.Operations, Scenario: item.Scenario, SharedSteps: steps, PublicEntry: item.PublicEntry, Preconditions: item.Preconditions, Steps: item.Steps, Oracle: item.Oracle, Test: item.Test}
		if item.Case != 0 {
			original := issued.Cases[item.Case-1]
			entry = Case{CaseID: original.CaseID, Operations: slices.Clone(original.Operations), Scenario: original.Scenario, SharedSteps: slices.Clone(original.SharedSteps), PublicEntry: original.PublicEntry, Preconditions: original.Preconditions, Steps: original.Steps, Oracle: original.Oracle}
			if err := applyCaseChange(&entry, *item.changes, stepIDs, facts); err != nil {
				return nil, err
			}
		} else {
			if item.Scenario == "" || item.Oracle == "" {
				return nil, fmt.Errorf("new case requires a scenario and oracle")
			}
			if err := applyCaseChange(&entry, *item.changes, stepIDs, facts); err != nil {
				return nil, err
			}
		}
		if item.Case == 0 {
			item.Operations = entry.Operations
			id = contentID("case", struct {
				Binding any
				Item    any
			}{issued.Binding, item}, index)
			entry.CaseID = id
		}
		result.Cases = append(result.Cases, entry)
		if !slices.Contains(selectedCaseIDs, id) {
			selectedCaseIDs = append(selectedCaseIDs, id)
		}
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

// Columns are paired 1-based Unicode positions on the first and last selected
// lines, inclusive of CR/LF characters. Omitted columns retain complete lines.
func sourceReadText(facts []LocationFact, read, from, to int, fromColumn, toColumn *int) (string, error) {
	if read < 1 || read > len(facts) || (facts[read-1].Kind != "read" && facts[read-1].Kind != "requirement" && facts[read-1].Kind != "material") {
		return "", fmt.Errorf("source requires an original published read")
	}
	lines := strings.SplitAfter(string(facts[read-1].Content), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if from < 1 || to < from || to > len(lines) {
		return "", fmt.Errorf("source selects lines from the original read")
	}
	selected := lines[from-1 : to]
	if fromColumn == nil && toColumn == nil {
		return strings.Join(selected, ""), nil
	}
	if fromColumn == nil || toColumn == nil {
		return "", fmt.Errorf("source columns require both fromColumn and toColumn")
	}
	columnByte := func(line string, column int) (int, error) {
		for index := range line {
			column--
			if column == 0 {
				return index, nil
			}
		}
		return 0, fmt.Errorf("source column is outside the original line")
	}
	start, err := columnByte(selected[0], *fromColumn)
	if err != nil {
		return "", err
	}
	last := len(selected) - 1
	end, err := columnByte(selected[last], *toColumn)
	if err != nil {
		return "", err
	}
	_, width := utf8.DecodeRuneInString(selected[last][end:])
	end += width
	if last == 0 {
		if end <= start {
			return "", fmt.Errorf("source columns select an inclusive ordered range")
		}
		return selected[0][start:end], nil
	}
	selected[0], selected[last] = selected[0][start:], selected[last][:end]
	return strings.Join(selected, ""), nil
}
