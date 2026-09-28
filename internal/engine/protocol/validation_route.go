package protocol

import (
	"slices"

	"formal-gates/internal/engine/coverage"
)

func (e *Engine) validateRouteChange(s *State, selected []string) error {
	if !pipelineEnabled(s, e.cfg.Definition) || s.Validation == nil {
		return nil
	}
	started := s.Validation.Development != nil
	for _, binding := range s.DispatchBindings {
		if s.validationRole(binding.Task, e.cfg.Definition) == "engine.development" {
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
		if !slices.Contains(s.SelectedGates, gate) && slices.Contains([]string{"blackbox", "whitebox", "merge"}, gate) {
			return validationError("development has started; route can add only ordinary gates")
		}
	}
	return nil
}

// A route change invalidates validation completion, not a repair already
// derived from collected and adjudicated evidence on that candidate.
func (s *State) validationRepairPending(w *ValidationWave) bool {
	return w != nil && w.Repair != nil && (w.Status == "REPAIR" || w.Status == "INVALIDATED" && s.Validation.Candidate != nil && *s.Validation.Candidate == w.Candidate && w.RouteDigest != s.validationRouteDigest())
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
	w.Status = "INVALIDATED"
	if s.validationRepairPending(w) {
		return
	}
	e.retireValidationWave(s, w.ID, "confirmed route changed the validation obligation")
	if s.Validation.Candidate != nil {
		e.freezeValidationWave(s, w.Scope)
	}
}

// ResultAction preserves the immutable result's finding ownership when a
// settled target is projected into another wave on the same candidate.
func (s *State) validationFindingTarget(w *ValidationWave, finding *ValidationFinding) *ValidationTarget {
	if w == nil {
		return nil
	}
	if finding.WaveID == w.ID {
		return w.Targets[finding.Target]
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
		if previous.Kind != target.Kind || previous.Mode != target.Mode || previous.Gate != target.Gate || !slices.Contains([]string{"PASS", "FAIL", "DISPOSED"}, previous.Status) {
			continue
		}
		carried := *previous
		carried.Task = target.Task
		if previous.Execution != nil {
			report := *previous.Execution
			_, whitelist, err := s.executionContract(target.Mode, current)
			if err != nil {
				return
			}
			report.Binding.RequiredSourcesDigest = whitelist.RequiredSourcesDigest
			report.Binding.ManifestDigest = whitelist.ManifestDigest
			report.Binding.MapDigest = whitelist.MapDigest
			report.Binding.WhitelistDigest = whitelist.Digest
			report.Binding.Scope = current.Scope
			carried.Execution = &report
		}
		*target = carried
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
