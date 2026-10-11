package protocol

import (
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
	"slices"
	"strings"
)

// sourceValidationTask follows the existing requirement confirmation ledger.
// Redo changes the execution identity while preserving the business duty.
func (s *State) sourceValidationTask(key runtime.TaskKey) runtime.TaskKey {
	for i := len(s.RequirementRevisions) - 1; i >= 0; i-- {
		entry := s.RequirementRevisions[i]
		for j, choice := range entry.WorkDecisions {
			if choice.Disposition == RequirementWorkRedo && replacementWorkTask(entry, j) == key {
				return s.sourceValidationTask(choice.Work.Task)
			}
		}
	}
	return key
}
func (s *State) validationRequiredTask(key runtime.TaskKey, wave *ValidationWave, cd *compiler.CompiledDefinition) runtime.TaskKey {
	if s.validationRole(key, cd) == "engine.development.batch.v3" {
		return key
	}
	for action, pending := range s.PendingActions {
		if !s.prewaveDispatchCurrent(action, cd) || !s.sameValidationDuty(s.DispatchBindings[action], wave) {
			continue
		}
		if pending.Task.Step == key.Step && s.validationMode(pending.Task) == s.validationMode(key) && sameRequiredDuty(s.validationRole(key, cd), s.sourceValidationTask(pending.Task), key) {
			return pending.Task
		}
	}
	for task, attempt := range s.EndedAttempts {
		if !s.endedWorkCurrent(attempt, cd) || attempt.Work != nil && !s.sameValidationDuty(*attempt.Work, wave) {
			continue
		}
		if task.Step == key.Step && s.validationMode(task) == s.validationMode(key) && sameRequiredDuty(s.validationRole(key, cd), s.sourceValidationTask(task), key) {
			return task
		}
	}
	for _, required := range s.requiredWorkTasks() {
		current := true
		for _, binding := range s.DispatchBindings {
			if binding.Task == required {
				current = s.prewaveWorkCurrent(binding, cd) && s.sameValidationDuty(binding, wave)
				break
			}
		}
		if !current || required.Step != key.Step {
			continue
		}
		source := s.sourceValidationTask(required)
		if s.validationMode(source) == s.validationMode(key) {
			role := s.validationRole(key, cd)
			if sameRequiredDuty(role, source, key) {
				return required
			}
		}
	}
	return key
}

// sameValidationDuty distinguishes a carried frozen target from a new scope
// on the same candidate. It does not grant requirement carry authority.
func (s *State) sameValidationDuty(binding WorkBinding, current *ValidationWave) bool {
	if binding.WaveID == "" || current == nil {
		return true
	}
	original := s.validationWave(binding.WaveID)
	if original == nil || original.Candidate != current.Candidate {
		return false
	}
	target := original.target(binding.Task.String())
	if target == nil {
		target = original.target(s.sourceValidationTask(binding.Task).String())
	}
	return target != nil && (target.Kind != "QA" || original.Scope == current.Scope && sameWaveCases(original, current, target.Mode))
}
func sameRequiredDuty(role string, source, current runtime.TaskKey) bool {
	switch role {
	case "engine.gate.v2":
		_, sourceScope, _ := strings.Cut(source.Scope, ":")
		_, currentScope, _ := strings.Cut(current.Scope, ":")
		return sourceScope == currentScope
	case "engine.merge.resolve.v2":
		return source.Scope == current.Scope
	}
	return true
}

func (e *Engine) invalidateValidationWork(s *State, choices []RequirementWorkDecision) {
	if s.Validation == nil {
		return
	}
	v := s.Validation
	invalidated := false
	for _, choice := range choices {
		if choice.Disposition != RequirementWorkRedo {
			continue
		}
		key := s.sourceValidationTask(choice.Work.Task)
		if s.validationRole(key, e.cfg.Definition) != "" {
			invalidated = true
		}
		switch s.validationRole(key, e.cfg.Definition) {
		case "engine.development.batch.v3", "engine.repair.v3":
			v.DevelopmentSnapshot = ""
			v.AuthoringWorkspace = ""
		case "engine.qa.design.v2", "engine.qa.review.rationale.v2":
			if set := v.Sets[s.validationMode(key)]; set != nil {
				var affected []string
				for _, c := range set.Manifest.Cases {
					affected = append(affected, c.CaseID)
				}
				set.reopen(affected)
			}
		}
	}
	if !invalidated {
		return
	}
	v.Candidate = nil
	if wave := s.currentValidationWave(); wave != nil && wave.Status != "COMPLETE" {
		wave.Status = "INVALIDATED"
	}
}

// carryCaseBindings changes the projection only; prior review records retain
// their original revision. Invalidated or semantically changed cases reopen.
func (s *State) carryCaseBindings() bool {
	contract := s.coverageContract(nil)
	changed := false
	for mode, set := range s.Validation.Sets {
		binding := coverage.ManifestBinding{SourceBinding: contract.RequiredSources.Binding, ReviewKind: mode, RouteScope: s.validationRouteDigest(), TopologyScope: digestOfCanonical(s.Topology)}
		if set.Manifest.Binding == binding {
			continue
		}
		changed = true
		set.Manifest.Binding = binding
		if err := s.validateManifest(mode, set.Manifest); err != nil {
			var affected []string
			for _, c := range set.Manifest.Cases {
				affected = append(affected, c.CaseID)
			}
			set.reopen(affected)
		}
		set.Version = digestOfCanonical(set.Manifest)
		if set.Review != nil {
			copy := *set.Review
			copy.Binding = set.Manifest.Binding
			set.Review = &copy
		}
		set.Paths = slices.Clone(set.Paths)
	}
	return changed
}

// A retry preserves the confirmed logical work and its signed binding.
func (s *State) validationCarryAuthorized(action string) bool {
	binding := s.DispatchBindings[action]
	return s.validationWorkCarryAuthorized(action, binding.Task)
}
func (s *State) validationWorkCarryAuthorized(action string, task runtime.TaskKey) bool {
	for i := len(s.RequirementRevisions) - 1; i >= 0; i-- {
		for _, choice := range s.RequirementRevisions[i].WorkDecisions {
			if action != "" && choice.Work.ActionID == action || task != (runtime.TaskKey{}) && choice.Work.Task == task {
				return choice.Disposition == RequirementWorkCarry
			}
		}
	}
	return false
}

// Cross-revision reuse follows the confirmed per-dispatch decisions. Candidate
// equality cannot discharge a replacement task explicitly authorized as REDO.
func (s *State) carryRequirementTarget(target *ValidationTarget, prior, current *ValidationWave) bool {
	if prior.Candidate != current.Candidate || slices.Contains(s.requiredWorkTasks(), target.Task) {
		return false
	}
	for _, previous := range prior.Targets {
		if !sameTarget(previous, target) || !slices.Contains([]string{"PASS", "FAIL", "DISPOSED", "AUTHORIZED_SKIP"}, previous.Status) {
			continue
		}
		_, responsibility := s.targetResponsibility(prior, previous)
		if !s.validationWorkCarryAuthorized(responsibility.ResultAction, responsibility.Task) {
			continue
		}
		if target.Kind == "QA" && (prior.Scope != current.Scope || !sameWaveCases(prior, current, target.Mode)) {
			return false
		}
		return s.carryValidationTarget(target, previous, current)
	}
	return false
}

func (s *State) carryRequirementTargets(current *ValidationWave) bool {
	changed := false
	for _, target := range current.Targets {
		if target.Status != "PENDING" && target.Status != "RUNTIME_ERROR" && target.Status != "RETRY_EXHAUSTED" {
			continue
		}
		prior, previous := s.targetResponsibility(current, target)
		if prior == current || prior.Candidate != current.Candidate || !slices.Contains([]string{"PASS", "FAIL", "DISPOSED", "AUTHORIZED_SKIP"}, previous.Status) {
			continue
		}
		if prior.Generation != current.Generation {
			changed = s.carryRequirementTarget(target, prior, current) || changed
		} else if target.Kind != "QA" || prior.Scope == current.Scope && sameWaveCases(prior, current, target.Mode) {
			changed = s.carryValidationTarget(target, previous, current) || changed
		}
	}
	return changed
}
