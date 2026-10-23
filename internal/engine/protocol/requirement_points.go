package protocol

import (
	"reflect"
	"slices"
	"strings"

	"formal-gates/internal/engine/review"
	"formal-gates/internal/requirement"
)

func (s *State) requirementPoints() ([]requirement.Point, []SourcePoint) {
	document := requirement.FrozenRequirementProjection{}
	if s.Validation != nil {
		document = s.Validation.Document
	}
	points, links := requirement.ProjectPoints(document)
	if inventory := s.confirmedCoverage(); inventory != nil {
		for _, point := range inventory.Points {
			points = append(points, requirement.Point{ID: point.PointID, Text: point.ObservableBehavior, Oracle: point.Oracle})
		}
		links = append(links, inventory.Links...)
	}
	return points, links
}

// Responsibility is projected from the confirmed topology and plan. The full
// document remains available for regression and implementation-quality review.
func (s *State) responsibilityPoints(owner string) []requirement.Point {
	points, _ := s.requirementPoints()
	if owner == "" {
		return points
	}
	var ids []string
	for id, responsible := range s.Topology.ACOwners {
		if responsible == owner {
			ids = append(ids, id)
		}
	}
	_, plan, _ := s.confirmedDevelopmentPlan()
	if plan != nil {
		for _, batch := range plan.Batches {
			if batch.Owner == owner {
				ids = append(ids, batch.RequirementPoints...)
			}
		}
	}
	return requirement.SelectPoints(points, ids)
}

func (s *State) validatePlanRequirementCoverage(plan *GranularityReview, complete bool) error {
	points, _ := s.requirementPoints()
	known := map[string]bool{}
	for _, point := range points {
		known[point.ID] = true
	}
	covered := map[string]bool{}
	for _, batch := range plan.Batches {
		seen := map[string]bool{}
		for _, id := range batch.RequirementPoints {
			if !known[id] || seen[id] {
				return validationError("batch requirement point is unknown or duplicate: " + id)
			}
			seen[id], covered[id] = true, true
		}
		if !slices.Equal(batch.RequirementPoints, requirement.PointIDs(requirement.SelectPoints(points, batch.RequirementPoints))) {
			return validationError("batch requirement points must retain the confirmed point order")
		}
	}
	if complete {
		owner := ""
		if s.SliceChild != nil {
			owner = s.SliceChild.SliceID
		}
		for _, point := range s.responsibilityPoints(owner) {
			if !covered[point.ID] {
				return validationError("development plan omits confirmed requirement point: " + point.ID)
			}
		}
	}
	return nil
}

func (s *State) workerRequirementPoints(input WorkerInput) []requirement.Point {
	points, _ := s.requirementPoints()
	if input.Validation != nil && input.Validation.Development != nil {
		return requirement.SelectPoints(points, input.Validation.Development.Batch.RequirementPoints)
	}
	if input.Validation != nil && input.Validation.Gate != nil {
		points, _ := s.gateRequirementContext(input.Validation.Binding)
		return points
	}
	owner := ""
	if s.SliceChild != nil {
		owner = s.SliceChild.SliceID
	}
	return s.responsibilityPoints(owner)
}

// Intermediate gates review only the original completed contributions. Final
// gates retain the full confirmed responsibility, including unchanged points.
func (s *State) gateRequirementContext(binding WorkBinding) ([]requirement.Point, []DevelopmentBatch) {
	wave := s.validationWave(binding.WaveID)
	return s.gateRequirementContextAt(binding, wave, wave.target(binding.Task.String()))
}

func (s *State) gateRequirementContextAt(binding WorkBinding, wave *ValidationWave, target *ValidationTarget) ([]requirement.Point, []DevelopmentBatch) {
	points, _ := s.requirementPoints()
	var batches []DevelopmentBatch
	if binding.BatchID != "" {
		if wave != nil && target != nil && target.BatchID != "" {
			for _, task := range wave.SourceTasks {
				if batch, known := s.plannedBatchSource(task); known && batch.ID == binding.BatchID {
					batches = append(batches, batch)
					break
				}
			}
		}
		source := s.Results[binding.PlanRequest].Validation
		if len(batches) == 0 && source != nil && source.GranularityReview != nil {
			for _, batch := range source.GranularityReview.Batches {
				if batch.ID == binding.BatchID {
					batches = append(batches, batch)
					break
				}
			}
		}
	} else if wave != nil && len(wave.CheckpointTasks) > 0 {
		for _, task := range wave.CheckpointTasks {
			if batch, known := s.plannedBatchSource(task); known {
				batches = append(batches, batch)
			}
		}
	}
	if len(batches) > 0 {
		var ids []string
		for _, batch := range batches {
			ids = append(ids, batch.RequirementPoints...)
		}
		return requirement.SelectPoints(points, ids), batches
	}
	owner := ""
	if s.SliceChild != nil {
		owner = s.SliceChild.SliceID
	} else if target != nil {
		owner = target.SliceID
	}
	return s.responsibilityPoints(owner), nil
}

func (s *State) gateTargetBinding(wave *ValidationWave, target *ValidationTarget) WorkBinding {
	if binding, known := s.DispatchBindings[target.ResultAction]; known && binding.Task == target.Task {
		return binding
	}
	if binding, member := s.repairGateBinding(target.Task, s.repairWork()); member {
		return binding
	}
	if target.BatchID != "" {
		if binding, known := s.developmentGateBinding(wave, target); known {
			return binding
		}
	}
	return WorkBinding{WaveID: wave.ID, Task: target.Task}
}

// Gate reuse retains the original completed responsibility, including the
// contribution contracts. Equal point IDs do not equate a contribution to final review.
func (s *State) sameGateResponsibility(prior *ValidationWave, previous *ValidationTarget, current *ValidationWave, target *ValidationTarget) bool {
	if target.Kind != "GATE" {
		return true
	}
	sourceWave, source := s.targetResponsibility(prior, previous)
	if !sameTarget(source, target) {
		return false
	}
	sourceBinding, targetBinding := s.gateTargetBinding(sourceWave, source), s.gateTargetBinding(current, target)
	if (len(sourceWave.CheckpointTasks) > 0) != (len(current.CheckpointTasks) > 0) || (sourceBinding.BatchID != "") != (targetBinding.BatchID != "") {
		return false
	}
	_, sourceBatches := s.gateRequirementContextAt(sourceBinding, sourceWave, source)
	currentPoints, currentBatches := s.gateRequirementContextAt(targetBinding, current, target)
	if !reflect.DeepEqual(sourceBatches, currentBatches) {
		return false
	}
	if result := s.Results[source.ResultAction].Validation; result != nil {
		expected := requirement.PointIDs(currentPoints)
		original := make([]string, len(result.RequirementResults))
		for i, decision := range result.RequirementResults {
			original[i] = decision.PointID
		}
		slices.Sort(expected)
		slices.Sort(original)
		if !slices.Equal(expected, original) {
			return false
		}
	}
	return true
}

// Required repair points are projected from the original adjudicated findings
// and their exact gate or QA evidence, without assigning meaning to prose.
func (s *State) repairRequirementPoints(input *RepairInput) []requirement.Point {
	points, _ := s.requirementPoints()
	var ids []string
	for _, finding := range input.Findings {
		resolution, known := findingResolutionAt(&finding, 0)
		if !known || resolution.Decision == "DISMISSED" || !slices.Contains(input.Obligation.Problems, resolution.ProblemID) {
			continue
		}
		if finding.ExecutionFailure != nil {
			for _, failure := range input.ExecutionFailures {
				if failure.FindingID == finding.ID {
					for _, record := range failure.Records {
						ids = append(ids, record.PointID)
					}
				}
			}
			continue
		}
		wave := s.validationWave(finding.WaveID)
		if wave == nil {
			continue
		}
		target := wave.target(finding.Target)
		if target == nil {
			continue
		}
		source := s.Results[target.ResultAction].Validation
		if source == nil {
			continue
		}
		for index, raw := range source.Findings {
			if validationFindingID(finding.WaveID, finding.Target, index, raw) != finding.ID {
				continue
			}
			for _, decision := range source.RequirementResults {
				if slices.Contains(decision.FindingIndexes, index) {
					ids = append(ids, decision.PointID)
				}
			}
		}
	}
	return requirement.SelectPoints(points, ids)
}

func validationFindingID(wave, target string, index int, raw review.Finding) string {
	return digestOfCanonical(struct {
		Wave, Target string
		Index        int
		Raw          review.Finding
	}{wave, target, index, raw})
}

func validateImplementedPoints(points []string, results []requirement.PointResult) error {
	if err := requirement.ValidatePointResults(points, results); err != nil {
		return validationReject(err)
	}
	for _, result := range results {
		if result.Status != "PASS" || strings.TrimSpace(result.Evidence) == "" {
			return validationError("completion requires evidence of every assigned point contribution")
		}
	}
	return nil
}

// RequirementDecision links a gate's independent point judgment to its own
// findings. Finding severity and adjudication retain their existing authority.
type RequirementDecision struct {
	PointID        string `json:"pointId"`
	Status         string `json:"status"`
	Evidence       string `json:"evidence"`
	FindingIndexes []int  `json:"findingIndexes,omitempty"`
}

func ValidateRequirementDecisions(points []requirement.Point, decisions []RequirementDecision, findingCount int) error {
	results := make([]requirement.PointResult, len(decisions))
	for i, decision := range decisions {
		if strings.TrimSpace(decision.Evidence) == "" {
			return validationError("requirement point judgment needs its independent evidence")
		}
		results[i] = requirement.PointResult{PointID: decision.PointID, Status: decision.Status, Evidence: decision.Evidence}
		seen := []int{}
		for _, index := range decision.FindingIndexes {
			if index < 0 || index >= findingCount || slices.Contains(seen, index) {
				return validationError("requirement decision references an unknown or duplicate gate finding")
			}
			seen = append(seen, index)
		}
		if decision.Status == "FAIL" && len(seen) == 0 {
			return validationError("failed requirement point needs its gate finding")
		}
	}
	if err := requirement.ValidatePointResults(requirement.PointIDs(points), results); err != nil {
		return validationReject(err)
	}
	return nil
}
