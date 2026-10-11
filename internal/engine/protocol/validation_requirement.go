package protocol

import (
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
	"path/filepath"
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
	if s.validationRole(key, cd) == "engine.development.batch.v4" {
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
	case "engine.merge.resolve.v3":
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
		case "engine.development.batch.v4", "engine.repair.v5":
			v.DevelopmentSnapshot = ""
			v.AuthoringWorkspace = ""
		case "engine.qa.design.v3", "engine.qa.review.rationale.v2":
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
	assembly := s.RequirementAssemblyReceipt(prior, current)
	if prior.Candidate != current.Candidate && assembly == "" || slices.Contains(s.requiredWorkTasks(), target.Task) {
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
		if target.Kind == "QA" && (prior.Scope != current.Scope && assembly == "" || !sameWaveCases(prior, current, target.Mode)) {
			return false
		}
		if assembly != "" {
			if previous.Status != "PASS" {
				return false
			}
			carried := *previous
			if previous.Execution != nil {
				report := *previous.Execution
				report.Binding.Candidate, report.Binding.Scope = current.Candidate, current.Scope
				report.Records = slices.Clone(previous.Execution.Records)
				for i := range report.Records {
					row := &report.Records[i]
					if row.Result != coverage.ExecutedPass {
						return false
					}
					if row.Provenance != coverage.ProvenanceInherited {
						row.InheritedFromCandidate = prior.Candidate.Identity
					}
					row.Provenance, row.ReuseEvidence = coverage.ProvenanceInherited, assembly
				}
				carried.Execution = &report
			}
			carried.RequiredCases, carried.ScopeReason = nil, assembly
			return s.carryValidationTarget(target, &carried, current)
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

// RequirementArtifactPath is the confirmed intake artifact, never a guessed
// documentation path or a production file selected by its name.
func (s *State) RequirementArtifactPath() string {
	if s.IntakeConfirmationReceipt == nil {
		return ""
	}
	source := filepath.Clean(s.IntakeConfirmationReceipt.RequirementSource)
	if filepath.IsAbs(source) {
		source, _ = filepath.Rel(s.NativeRoot, source)
	}
	for _, artifact := range s.IntakeConfirmationReceipt.Artifacts {
		if filepath.Clean(filepath.FromSlash(artifact.Path)) == source {
			return artifact.Path
		}
	}
	return ""
}

func (s *State) confirmedRequirementCarry() bool {
	if s.Validation == nil || len(s.RequirementRevisions) == 0 {
		return false
	}
	revision := s.RequirementRevisions[len(s.RequirementRevisions)-1]
	if revision.Source != RequirementSourceUserInitiated || revision.Revision != s.Validation.RequirementRevision || revision.RequestID == "" || len(revision.WorkDecisions) == 0 || len(revision.DecidedScope) != 0 {
		return false
	}
	for _, choice := range revision.WorkDecisions {
		if choice.Disposition != RequirementWorkCarry {
			return false
		}
	}
	return true
}

// RequirementAssemblySource identifies only the first artifact assembly after
// this exact user confirmation. An already assembled or repaired candidate is
// owned by its own receipt and cannot borrow that authorization.
func (s *State) RequirementAssemblySource() string {
	if s.NativeRoot == "" || !s.confirmedRequirementCarry() || s.RequirementArtifactPath() == "" {
		return ""
	}
	prior := s.currentValidationWave()
	if prior == nil || prior.Status != "COMPLETE" || s.Validation.Candidate != nil && *s.Validation.Candidate != prior.Candidate {
		return ""
	}
	current := &ValidationWave{Coverage: s.coverageContract(s.Validation.Candidate), Versions: map[coverage.ReviewKind]string{}}
	for _, mode := range s.validationKinds() {
		if !s.caseSetReady(mode) {
			return ""
		}
		current.Versions[mode] = s.Validation.Sets[mode].Version
		if !sameWaveCases(prior, current, mode) {
			return ""
		}
	}
	for action, receipt := range s.HostActionReceipts {
		if receipt.Status != HostActionStatusExecuted && receipt.Status != HostActionStatusReconciled {
			continue
		}
		intent, exists := s.hostIntent(action)
		params, err := candidateParameters(intent)
		if !exists || err != nil || params.Stage != "REVIEW_READY" {
			continue
		}
		proof, err := decodeCandidateEvidence(receipt)
		if err == nil && proof.Identity == prior.Candidate.Identity && proof.Digest == prior.Candidate.Digest && proof.ParametersDigest == intent.PayloadDigest && params.RequirementRevision != s.Validation.RequirementRevision {
			return prior.Candidate.Identity
		}
	}
	return ""
}

// RequirementAssemblyReceipt derives the exact source-to-candidate mapping from
// the existing candidate receipt and the user's current per-work decisions.
func (s *State) RequirementAssemblyReceipt(prior, current *ValidationWave) string {
	if prior == nil || current == nil || prior.Candidate == current.Candidate || !s.confirmedRequirementCarry() || prior.RouteDigest != current.RouteDigest || !sameTaskSet(prior.SourceTasks, current.SourceTasks) {
		return ""
	}
	path := s.RequirementArtifactPath()
	if path == "" {
		return ""
	}
	for _, mode := range s.validationKinds() {
		if !sameWaveCases(prior, current, mode) {
			return ""
		}
	}
	for action, receipt := range s.HostActionReceipts {
		if receipt.Status != HostActionStatusExecuted && receipt.Status != HostActionStatusReconciled {
			continue
		}
		intent, exists := s.hostIntent(action)
		params, err := candidateParameters(intent)
		if !exists || err != nil || params.Stage != "REVIEW_READY" || params.Base != prior.Candidate.Identity || params.RequirementRevision != current.RequirementRevision || params.Generation != current.Generation || !params.Finalize {
			continue
		}
		proof, err := decodeCandidateEvidence(receipt)
		if err != nil || proof.ParametersDigest != intent.PayloadDigest || proof.Identity != current.Candidate.Identity || proof.Digest != current.Candidate.Digest {
			continue
		}
		document := false
		valid := true
		for _, change := range proof.Changes {
			if change.Path == path {
				document = true
				continue
			}
			if params.BlackboxManifest != nil && change.Path == ".gates/results/"+s.RunID+".blackbox-cases.md" || params.MergeManifest != nil && change.Path == ".gates/results/"+s.RunID+".merge-cases.md" {
				continue
			}
			valid = false
		}
		if valid && document {
			return action
		}
	}
	return ""
}
