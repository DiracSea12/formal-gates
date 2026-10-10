package protocol

import (
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
)

func (e *Engine) retireSupersededObservations(s *State, nextRevision uint64) (bool, error) {
	if s.abortRequested() || s.resetPending() || s.requirementChangePending() || !s.parentReviewReady() {
		return false, nil
	}
	ids := make([]string, 0, len(s.PendingActions))
	for id := range s.PendingActions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	changed := false
	for _, id := range ids {
		pending := s.PendingActions[id]
		attempt := s.Attempts[pending.Task]
		if attempt.Work == nil || s.validationRole(pending.Task, e.cfg.Definition) != "engine.validation.observation" || s.validationWorkOwed(*attempt.Work, e.cfg.Definition) {
			continue
		}
		_, spawned := s.SpawnReceipts[id]
		_, staged := s.StagedResults[id]
		var observed *SpawnReceiptPayload
		if !spawned && !staged && e.cfg.WorkerExecution != nil {
			var err error
			observed, err = e.cfg.WorkerExecution(pending, attempt)
			if err != nil {
				return changed, err
			}
		}
		if observed != nil && observed.Status == SpawnStatusSpawned {
			event, err := NewSpawnReceiptEvent(EventID("native-spawn:"+id), observed.ActionID, observed.Provider, observed.Correlation, observed.Status)
			if err != nil {
				return changed, err
			}
			digest, err := event.Digest()
			if err != nil {
				return changed, err
			}
			accepted, _, _, err := e.admit(event, digest, s, nextRevision-1, nextRevision, "")
			if err != nil {
				return changed, err
			}
			s.Events[string(event.ID)] = EventRecord{Digest: digest, Acceptance: accepted}
			spawned, changed = true, true
		}
		const reason = "observation responsibility was superseded"
		if !spawned && !staged && observed == nil {
			s.retireAction(id, "", reason)
			if err := s.terminalizeTask(pending.Task); err != nil {
				return changed, err
			}
			delete(s.EndedAttempts, pending.Task)
			s.routingReleasedCapacity = true
			changed = true
		} else if _, obsolete := s.ObsoleteActions[id]; !obsolete {
			s.ObsoleteActions[id] = ObsoleteAction{ActionID: id, Task: pending.Task, AttemptID: attempt.ID, Reason: reason, Bindings: attempt.Bindings, Plan: attempt.Plan}
			changed = true
		}
	}
	return changed, nil
}

// prewaveScope identifies the actual requested design inputs and the explicit
// reopening responsibility. Reading or driving the same facts preserves it.
func (s *State) prewaveScope(mode coverage.ReviewKind) string {
	set := s.Validation.Sets[mode]
	if set == nil {
		return ""
	}
	request, _ := s.designReopening(mode)
	manifest := set.Manifest
	manifest.Binding = coverage.ManifestBinding{}
	return strings.TrimPrefix(digestOfCanonical(struct {
		Design   runtime.TaskKey
		Manifest coverage.AcceptanceManifest
		Request  string
	}{set.DesignTask, manifest, request}), "sha256:")
}

func (s *State) designReopening(mode coverage.ReviewKind) (string, uint64) {
	var request string
	var revision uint64
	for id, d := range s.Decisions {
		r := s.PendingAsks[id].Validation
		if r == nil || r.Mode != mode || d.Revision <= revision {
			continue
		}
		redesign := d.Choice == "redesign" || d.Choice == "approve" && (r.Kind == "THAW" || r.Kind == "QA_ARTIFACT_REPAIR" || r.Kind == "VALIDATION_DETAIL_DISPOSITION" && d.Validation != nil && d.Validation.Remedy == "QA_ARTIFACT_REPAIR")
		if redesign {
			request, revision = id, d.Revision
		}
	}
	return request, revision
}

func (s *State) reviewTask(mode coverage.ReviewKind, generation uint64, version string, cd *compiler.CompiledDefinition) runtime.TaskKey {
	set := s.Validation.Sets[mode]
	return pipelineTask(cd, "engine.qa.review.rationale", fmt.Sprintf("%d:%s:%s:%s:%d", generation, mode, version, strings.TrimPrefix(digestOfCanonical(set.DesignTask), "sha256:"), len(set.ReviewHistory)+1))
}

// prewaveWorkCurrent decides applicability, not transport completion. An
// inapplicable dispatch keeps its receipt/identity bookkeeping until observed.
func (s *State) prewaveWorkCurrent(b WorkBinding, cd *compiler.CompiledDefinition) bool {
	role := s.validationRole(b.Task, cd)
	if role == "engine.validation.observation" {
		input := s.validationObservationInput()
		return input != nil && s.sourceValidationTask(b.Task) == observationTask(cd, input)
	}
	if role != "engine.qa.design" && role != "engine.qa.review.rationale" {
		return true
	}
	if s.Validation == nil {
		return false
	}
	if b.Generation != s.Validation.Generation && !s.validationWorkCarryAuthorized("", b.Task) {
		return false
	}
	mode := s.validationMode(b.Task)
	set := s.Validation.Sets[mode]
	source := s.sourceValidationTask(b.Task)
	if role == "engine.qa.design" {
		if set != nil && set.Status != "DRAFT" && set.Status != "THAWED" {
			return false
		}
		if set != nil && set.DesignTask == b.Task {
			return s.prewaveInputsCurrent(b)
		}
		expected := pipelineTask(cd, role, s.nextDesignScope(mode, cd))
		// Requirement carry retains the original generation in the signed task.
		parts := strings.SplitN(expected.Scope, ":", 2)
		if len(parts) == 2 {
			expected.Scope = fmt.Sprintf("%d:%s", b.Generation, parts[1])
		}
		return source == expected || s.requirementTaskOwnsPrewave(b)
	}
	if set == nil || set.Status != "REVIEW" {
		return false
	}
	original := set.Manifest
	original.Binding.RequirementRevision = b.RequirementRevision
	original = manifestAtRoute(original, b.RouteScope)
	if digestOfCanonical(original) != b.CaseVersion {
		return false
	}
	if source != s.reviewTask(mode, b.Generation, b.CaseVersion, cd) && !s.requirementTaskOwnsPrewave(b) {
		return false
	}
	return mode != coverage.ReviewWhitebox || s.sameCandidateContent(b.Candidate, set.Candidate)
}

func (s *State) prewaveDispatchCurrent(action string, cd *compiler.CompiledDefinition) bool {
	b, ok := s.DispatchBindings[action]
	return !ok || s.prewaveWorkCurrent(b, cd)
}

func (e *Engine) retirePrewaveResult(s *State, record WorkerResult) error {
	b := s.DispatchBindings[record.ActionID]
	s.retireAction(record.ActionID, "", "pre-wave responsibility was superseded")
	delete(s.EndedAttempts, b.Task)
	if err := s.terminalizeTask(b.Task); err != nil {
		return err
	}
	s.archiveObsoleteResult(record)
	s.routingReleasedCapacity = true
	return nil
}

// A requirement replacement retains the original duty in WorkDecisions, while
// its issuance revision binds which accepted design and reopening it observed.
func (s *State) requirementTaskOwnsPrewave(b WorkBinding) bool {
	if s.requirementRevisionForTask(b.Task) == "" {
		return false
	}
	return s.prewaveInputsCurrent(b)
}

func (s *State) prewaveInputsCurrent(b WorkBinding) bool {
	var issued uint64
	for action, binding := range s.DispatchBindings {
		if binding.Task == b.Task && s.DispatchIssueRevisions[action] > issued {
			issued = s.DispatchIssueRevisions[action]
		}
	}
	if issued == 0 {
		return false
	}
	mode := s.validationMode(b.Task)
	set := s.Validation.Sets[mode]
	if set != nil {
		for action, result := range s.Results {
			if result.Validation != nil && result.Validation.Binding.Task == set.DesignTask && s.DispatchIssueRevisions[action] > issued {
				return false
			}
		}
	}
	if _, reopened := s.designReopening(mode); reopened > issued {
		return false
	}
	return true
}

// validationWorkOwed reads the existing development, target and repair owners.
// A carried target retains its original wave binding while the current wave
// may project that exact Task; candidate equality alone grants no authority.
func (s *State) validationWorkOwed(b WorkBinding, cd *compiler.CompiledDefinition) bool {
	if s.Validation == nil || b.Generation != s.Validation.Generation && !s.validationWorkCarryAuthorized("", b.Task) {
		return false
	}
	if s.validationRole(b.Task, cd) == "engine.validation.observation" {
		input := s.validationObservationInput()
		return input != nil && s.sourceValidationTask(b.Task) == observationTask(cd, input)
	}
	if s.validationRole(b.Task, cd) == "engine.repair.verify" {
		context, current := s.repairComparisonContext(b.Task, cd)
		return current && context.binding == b && s.repairComparisonResult(b.Task) == nil
	}
	if s.validationRole(b.Task, cd) == "engine.development.batch" {
		_, _, _, current := s.developmentBatch(b.Task, cd)
		return current && s.developmentResult(b.Task) == nil
	}
	if b.WaveID == "" {
		return true
	}
	if _, target := s.repairGateTarget(b.Task); target != nil {
		_, current := s.repairGateBinding(b.Task, s.repairWork())
		return current && (target.Status == "PENDING" || target.Status == "RUNTIME_ERROR" || target.Status == "RETRY_EXHAUSTED")
	}
	if s.validationRole(b.Task, cd) == "engine.repair" {
		for _, work := range s.repairWork() {
			if work.task == b.Task && work.wave.ID == b.WaveID {
				return work.result == nil
			}
		}
		return false
	}
	target := s.validationTargetCurrent(b)
	return target != nil && (target.Status == "PENDING" || target.Status == "RUNTIME_ERROR" || target.Status == "RETRY_EXHAUSTED")
}

// A projected target retains its exact signed task and frozen duty. Reuse across
// requirement revisions or generations additionally needs confirmed carry.
func (s *State) validationTargetCurrent(b WorkBinding) *ValidationTarget {
	if s.Validation == nil {
		return nil
	}
	wave, current := s.validationWave(b.WaveID), s.currentValidationWave()
	if wave == nil {
		return nil
	}
	if wave != current || wave.Status != "COLLECTING" || b.Generation != s.Validation.Generation || b.RequirementRevision != s.Validation.RequirementRevision {
		unchanged := b.RequirementRevision == s.Validation.RequirementRevision && b.Generation == s.Validation.Generation
		if !unchanged && !s.validationWorkCarryAuthorized("", b.Task) || !s.sameValidationDuty(b, current) {
			return nil
		}
	}
	if wave != current {
		if current == nil || current.Status != "COLLECTING" {
			return nil
		}
		return current.Targets[b.Task.String()]
	}
	return wave.Targets[b.Task.String()]
}
