package protocol

import (
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
	"slices"
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
func (s *State) validationRequiredTask(key runtime.TaskKey, cd *compiler.CompiledDefinition) runtime.TaskKey {
	for _, pending := range s.PendingActions {
		if pending.Task.Step == key.Step && s.validationMode(pending.Task) == s.validationMode(key) && (s.validationRole(key, cd) != "engine.gate" || lastScopePart(s.sourceValidationTask(pending.Task).Scope) == lastScopePart(key.Scope)) {
			return pending.Task
		}
	}
	for _, required := range s.requiredWorkTasks() {
		if required.Step != key.Step {
			continue
		}
		source := s.sourceValidationTask(required)
		if s.validationMode(source) == s.validationMode(key) {
			role := s.validationRole(key, cd)
			if role != "engine.gate" || lastScopePart(source.Scope) == lastScopePart(key.Scope) {
				return required
			}
		}
	}
	return key
}
func lastScopePart(scope string) string {
	for i := len(scope) - 1; i >= 0; i-- {
		if scope[i] == ':' {
			return scope[i+1:]
		}
	}
	return scope
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
		case "engine.development", "engine.repair":
			v.Development = nil
			v.DevelopmentSnapshot = ""
			v.AuthoringWorkspace = ""
		case "engine.qa.design", "engine.qa.review":
			if set := v.Sets[s.validationMode(key)]; set != nil {
				set.Status = "DRAFT"
				set.Candidate = ""
				set.AffectedCases = nil
				for _, c := range set.Manifest.Cases {
					set.AffectedCases = append(set.AffectedCases, c.CaseID)
				}
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
func (s *State) carryCaseBindings() {
	contract := s.coverageContract(nil)
	for mode, set := range s.Validation.Sets {
		set.Manifest.Binding = coverage.ManifestBinding{SourceBinding: contract.RequiredSources.Binding, ReviewKind: mode, RouteScope: s.validationRouteDigest(), TopologyScope: digestOfCanonical(s.Topology)}
		if err := s.validateManifest(mode, set.Manifest); err != nil {
			set.Status = "DRAFT"
			set.AffectedCases = nil
			for _, c := range set.Manifest.Cases {
				set.AffectedCases = append(set.AffectedCases, c.CaseID)
			}
		}
		set.Version = digestOfCanonical(set.Manifest)
		if set.Review != nil {
			copy := *set.Review
			copy.Binding = set.Manifest.Binding
			set.Review = &copy
		}
		set.Paths = slices.Clone(set.Paths)
	}
}

func (s *State) validationCarryAuthorized(action string) bool {
	for i := len(s.RequirementRevisions) - 1; i >= 0; i-- {
		for _, choice := range s.RequirementRevisions[i].WorkDecisions {
			if choice.Work.ActionID == action {
				return choice.Disposition == RequirementWorkCarry
			}
		}
	}
	return false
}
