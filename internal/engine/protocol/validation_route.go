package protocol

import (
	"slices"

	"formal-gates/internal/engine/coverage"
)

func (e *Engine) validateRouteChange(s *State, selected []string) error {
	if !pipelineEnabled(s, e.cfg.Definition) || s.Validation == nil {
		return nil
	}
	started := false
	for _, binding := range s.DispatchBindings {
		if s.validationRole(binding.Task, e.cfg.Definition) == "engine.development.batch.v2" {
			started = true
		}
	}
	if !started {
		return nil
	}
	for _, gate := range s.SelectedGates {
		if !slices.Contains(selected, gate) {
			return validationError("development has started; route cannot remove an existing obligation")
		}
	}
	for _, gate := range selected {
		if !slices.Contains(s.SelectedGates, gate) && (slices.Contains([]string{"blackbox", "whitebox"}, gate) || slices.Contains(reservedRouteGates, gate)) {
			return validationError("development has started; route can add only ordinary gates")
		}
	}
	return nil
}

// Validation invalidation does not itself retire the unique repair duty.
// Route continuity or explicit requirement carry may retain its signed Attempt;
// THAW, replacement and disposition retire that Attempt through their owners.
func (s *State) validationRepairPending(w *ValidationWave) bool {
	if w == nil || w.Repair == nil {
		return false
	}
	members := s.repairMembers(w)
	if len(members) == 1 && members[0].task == w.Repair.Task && s.repairTaskFinished(w.Repair.Task) && s.developmentEditResult(w.Repair.Task) == nil {
		return false
	}
	if w.Status == "REPAIR" {
		return true
	}
	if w.Status != "INVALIDATED" {
		return false
	}
	_, issued := s.repairIssue(w.Repair.Task)
	for id, decision := range s.Decisions {
		request := s.PendingAsks[id].Validation
		if request != nil && decision.Choice == "approve" && decision.Revision > issued && decision.Revision > s.requirementGeneration() && (request.Kind == "THAW" || request.Kind == "QA_ARTIFACT_REPAIR" || request.Kind == "VALIDATION_DETAIL_DISPOSITION" && decision.Validation != nil && decision.Validation.Remedy == "QA_ARTIFACT_REPAIR") {
			return false
		}
	}
	if s.Validation.Candidate != nil && *s.Validation.Candidate == w.Candidate && w.RouteDigest != s.validationRouteDigest() {
		return true
	}
	for _, member := range s.repairMembers(w) {
		if s.repairTaskFinished(member.task) && s.developmentEditResult(member.task) == nil {
			return false
		}
		if !s.validationWorkCarryAuthorized("", member.task) && s.requirementRevisionForTask(member.task) != s.Validation.RequirementRevision {
			return false
		}
	}
	return true
}

// Completed source work remains usable; invalidated validation can continue
// only under the original duty's explicit carry/replacement authority.
func (s *State) repairDutyCurrent(w *ValidationWave) bool {
	return w != nil && w.Repair != nil && !s.abortRequested() && (w.Status == "REPAIRED" || s.validationRepairPending(w))
}

func (e *Engine) rebindValidationRoute(s *State) {
	if s.Validation == nil {
		return
	}
	s.carryCaseBindings()
	w := s.currentValidationWave()
	if w == nil || w.RouteDigest == s.validationRouteDigest() {
		return
	}
	if w.Status == "REPAIRED" {
		return
	}
	if w.Status != "COMPLETE" {
		w.Status = "INVALIDATED"
	}
	if s.validationRepairPending(w) {
		return
	}
	e.retireValidationWave(s, w.ID, "confirmed route changed the validation obligation")
	if s.Validation.Candidate != nil {
		e.freezeValidationWave(s, w.Scope, "", w.SourceTasks)
	}
}

// ResultAction preserves the immutable result's finding ownership when a
// settled target is projected into another wave on the same candidate.
func (s *State) validationFindingTarget(w *ValidationWave, finding *ValidationFinding) *ValidationTarget {
	if w == nil {
		return nil
	}
	if finding.WaveID == w.ID {
		return w.target(finding.Target)
	}
	for _, target := range w.Targets {
		binding, known := s.DispatchBindings[target.ResultAction]
		if known && binding.WaveID == finding.WaveID && binding.Task.String() == finding.Target {
			return target
		}
	}
	return nil
}

func (s *State) carryRouteTarget(target *ValidationTarget, prior, current *ValidationWave) {
	for _, previous := range prior.Targets {
		if !sameTarget(previous, target) || !slices.Contains([]string{"PASS", "FAIL", "DISPOSED", "AUTHORIZED_SKIP"}, previous.Status) {
			continue
		}
		s.carryValidationTarget(target, previous, current)
		return
	}
}

// manifestAtRoute projects only route context; case content and every other
// binding remain available for exact version verification.
func manifestAtRoute(manifest coverage.AcceptanceManifest, route string) coverage.AcceptanceManifest {
	if route != "" {
		manifest.Binding.RouteScope = route
	}
	return manifest
}

// The current target is a projection; original result and wave bindings remain
// untouched in their evidence records.
func (s *State) carryValidationTarget(target, previous *ValidationTarget, current *ValidationWave) bool {
	carried := *previous
	if previous.GateImpact != nil {
		impact := *previous.GateImpact
		carried.GateImpact = &impact
	}
	carried.Task = target.Task
	if previous.Execution != nil {
		report := *previous.Execution
		contract, whitelist, err := s.executionContract(target.Mode, current)
		if err != nil || report.Binding.Scope != current.Scope {
			return false
		}
		report.Binding.RequiredSourcesDigest = whitelist.RequiredSourcesDigest
		report.Binding.ManifestDigest = whitelist.ManifestDigest
		report.Binding.MapDigest = whitelist.MapDigest
		report.Binding.WhitelistDigest = whitelist.Digest
		if err := contract.ValidateResults(whitelist, report); err != nil {
			return false
		}
		carried.Execution = &report
	}
	*target = carried
	return true
}

// targetResponsibility locates the frozen duty behind a current projection or
// repair-derived verdict. Identity lookup grants no result or scope reuse.
func (s *State) targetResponsibility(wave *ValidationWave, target *ValidationTarget) (*ValidationWave, *ValidationTarget) {
	if target.GateImpact != nil {
		if source := s.validationWave(target.GateImpact.SourceWave); source != nil {
			for _, original := range source.Targets {
				if sameTarget(original, target) {
					return s.targetResponsibility(source, original)
				}
			}
		}
	}
	binding, known := s.DispatchBindings[target.ResultAction]
	if !known {
		if original := s.validationWaveForTask(target.Task); original != nil && original.ID != wave.ID {
			return s.targetResponsibility(original, original.target(target.Task.String()))
		}
		return wave, target
	}
	source := s.sourceValidationTask(binding.Task)
	for i := range s.Validation.Waves {
		original := &s.Validation.Waves[i]
		if original.Repair == nil || original.Repair.Task != source {
			continue
		}
		for _, prior := range original.Targets {
			if sameTarget(prior, target) && prior.ResultAction != target.ResultAction {
				return s.targetResponsibility(original, prior)
			}
		}
	}
	if original := s.validationWave(binding.WaveID); original != nil {
		if prior := original.target(binding.Task.String()); prior != nil {
			return original, prior
		}
	}
	return wave, target
}
