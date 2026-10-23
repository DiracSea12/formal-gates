package protocol

import (
	"slices"
	"strings"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
)

func (s *State) developmentGates() []string {
	if !s.FlowNodeEnabled("gate") {
		return nil
	}
	return slices.DeleteFunc(slices.Clone(s.SelectedGates), func(gate string) bool {
		return gate == "blackbox" || gate == "whitebox" || slices.Contains(reservedRouteGates, gate)
	})
}

// A batch gate is an ordinary validation duty against an immutable editing
// source. Each target names its contribution; parallel writers share a source.
func developmentGateWave(w *ValidationWave) bool {
	if w == nil || len(w.CheckpointTasks) == 0 || len(w.Targets) == 0 {
		return false
	}
	for _, target := range w.Targets {
		if target.BatchID == "" {
			return false
		}
	}
	return true
}

func (s *State) developmentGatesComplete(task runtime.TaskKey) bool {
	gates := s.developmentGates()
	batch, known := s.plannedBatchSource(task)
	if len(gates) == 0 || known && batch.Spike {
		return true
	}
	if s.Validation == nil {
		return false
	}
	for i := len(s.Validation.Waves) - 1; i >= 0; i-- {
		w := &s.Validation.Waves[i]
		if w.Generation != s.Validation.Generation || w.RequirementRevision != s.Validation.RequirementRevision || w.RouteDigest != s.validationRouteDigest() || !slices.Contains(w.SourceTasks, task) {
			continue
		}
		passed := true
		for _, gate := range gates {
			found := false
			for _, target := range w.Targets {
				if target.Gate != gate {
					continue
				}
				if target.BatchID == batch.ID && developmentGateTargetPassed(s, w, target) || target.BatchID == "" && !developmentGateWave(w) && w.Status == "COMPLETE" && s.validationEvidenceComplete(w) {
					found = true
				}
			}
			passed = passed && found
		}
		if passed {
			return true
		}
	}
	return false
}

func developmentGateTargetPassed(s *State, wave *ValidationWave, target *ValidationTarget) bool {
	if target.Status == "DISPOSED" || target.Status == "AUTHORIZED_SKIP" {
		return true
	}
	result := s.Results[target.ResultAction]
	if target.Status != "PASS" || result.Outcome != OutcomePass || result.Validation == nil || result.Validation.Checkpoint {
		return false
	}
	for _, finding := range s.Validation.Findings {
		if owner := s.validationFindingTarget(wave, finding); owner != nil && owner.Task == target.Task && len(finding.History) == 0 {
			return false
		}
	}
	for _, obligation := range s.validationObligationDetails(wave) {
		if obligation.target.Task == target.Task {
			return false
		}
	}
	return true
}

func (e *Engine) advanceDevelopmentGates(s *State) bool {
	if len(s.developmentGates()) == 0 || s.Validation == nil {
		return false
	}
	if current := s.currentValidationWave(); current != nil && current.Status != "COMPLETE" && current.Status != "INVALIDATED" {
		return false
	}
	tasks := s.planTasks(e.cfg.Definition)
	if !s.mergeMaster() && len(tasks) < 2 {
		return false
	}
	var pending []runtime.TaskKey
	var source CandidateEvidence
	for _, task := range tasks {
		result := s.developmentResultRecord(task)
		if result == nil || s.developmentGatesComplete(task) {
			continue
		}
		proof, frozen := s.editSourceProof(result.ActionID)
		if !frozen {
			continue
		}
		if len(pending) == 0 {
			source = proof
		}
		if proof.Identity == source.Identity && proof.Digest == source.Digest {
			pending = append(pending, task)
		}
	}
	if len(pending) == 0 {
		return false
	}
	if !s.mergeMaster() && len(pending) == 1 && !slices.ContainsFunc(tasks, func(task runtime.TaskKey) bool { return s.developmentResult(task) == nil }) {
		return false
	}
	for _, task := range tasks {
		if result := s.developmentEditResult(task); result != nil {
			if proof, frozen := s.editSourceProof(result.ActionID); frozen && proof.Identity == source.Identity && proof.Digest == source.Digest && !s.developmentChecksPassed(*result) {
				return false
			}
		}
	}
	return e.freezeValidationWaveAt(s, coverage.ScopeFull, "", pending, coverage.ValidationCandidate{Identity: source.Identity, Digest: source.Digest}, pending)
}

func (s *State) developmentGateBinding(w *ValidationWave, target *ValidationTarget) (WorkBinding, bool) {
	for _, task := range w.SourceTasks {
		result := s.developmentResultRecord(task)
		if result == nil || result.Validation.Binding.BatchID != target.BatchID {
			continue
		}
		binding := result.Validation.Binding
		binding.Task, binding.WaveID, binding.Candidate = target.Task, w.ID, w.Candidate.Identity
		binding.RequirementRevision, binding.Generation, binding.RouteScope = w.RequirementRevision, w.Generation, w.RouteDigest
		binding.WorkspaceID = ""
		return binding, true
	}
	return WorkBinding{}, false
}

func (s *State) developmentGateInput(binding WorkBinding) *GateInput {
	w := s.validationWave(binding.WaveID)
	if w == nil {
		return nil
	}
	target := w.Targets[binding.Task.String()]
	if target == nil || target.BatchID == "" {
		return nil
	}
	for _, task := range w.SourceTasks {
		result := s.developmentResultRecord(task)
		if result == nil || result.Validation.Binding.BatchID != target.BatchID {
			continue
		}
		proof, frozen := s.editSourceProof(result.ActionID)
		if !frozen || proof.Identity != w.Candidate.Identity {
			return nil
		}
		_, batches := s.gateRequirementContext(binding)
		var owned []string
		if proof.GateView != nil {
			owned = slices.Clone(proof.GateView.TestOwnedPaths)
		}
		return &GateInput{Batches: batches, Gate: target.Gate, BaseSnapshot: s.BaseSnapshot, Candidate: w.Candidate, TestOwnedPaths: owned}
	}
	return nil
}

func (s *State) developmentGateRepair() *ValidationWave {
	wave := s.currentValidationWave()
	if developmentGateWave(wave) && wave.Status == "REPAIRED" {
		return wave
	}
	return nil
}

func developmentGateScope(wave string, task runtime.TaskKey, gate string) string {
	return wave + ":" + strings.TrimPrefix(digestOfCanonical(task), "sha256:") + ":" + gate
}
