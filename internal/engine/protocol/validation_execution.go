package protocol

import (
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/engine/coverage"
)

func executionCaseIDs(w coverage.ApprovedWhitelist) []string {
	var ids []string
	for _, edge := range w.Entries {
		if !slices.Contains(ids, edge.CaseID) {
			ids = append(ids, edge.CaseID)
		}
	}
	slices.Sort(ids)
	return ids
}

func requiredExecutionCases(wave *ValidationWave, target *ValidationTarget, whitelist coverage.ApprovedWhitelist) []string {
	if wave.Scope == coverage.ScopeFull {
		return executionCaseIDs(whitelist)
	}
	return target.RequiredCases
}

func (s *State) executionScopePending() string {
	wave := s.currentValidationWave()
	if wave == nil || wave.Status != "COLLECTING" || wave.Scope != coverage.ScopeAffected {
		return ""
	}
	var modes []string
	for _, key := range sortedTargetKeys(wave) {
		target := wave.Targets[key]
		if target.Kind == "QA" && target.Status == "PENDING" && len(target.RequiredCases) == 0 {
			modes = append(modes, string(target.Mode))
		}
	}
	if len(modes) == 0 {
		return ""
	}
	return fmt.Sprintf("submit EXECUTION_SCOPE observation for wave %s, modes %s: host-selected nonempty approved cases including every prior FAIL, with reason", wave.ID, strings.Join(modes, ", "))
}

func (s *State) acceptExecutionScope(subject string, o *ValidationObservation) error {
	wave := s.currentValidationWave()
	if wave == nil || wave.ID != subject || wave.Status != "COLLECTING" || wave.Scope != coverage.ScopeAffected || s.Validation.RequirementRevision != s.RequirementRevision() || wave.RouteDigest != s.validationRouteDigest() || s.requirementChangePending() {
		return validationError("execution scope needs the current collecting AFFECTED wave")
	}
	var target *ValidationTarget
	for _, candidate := range wave.Targets {
		if candidate.Kind == "QA" && candidate.Mode == o.Mode {
			target = candidate
			break
		}
	}
	if target == nil || target.Status != "PENDING" || len(target.RequiredCases) != 0 || strings.TrimSpace(o.Reason) == "" {
		return validationError("execution scope needs an undecided QA target and a reason")
	}
	_, whitelist, err := s.executionContract(o.Mode, wave)
	if err != nil {
		return err
	}
	approved := executionCaseIDs(whitelist)
	ids := slices.Clone(o.AffectedCases)
	slices.Sort(ids)
	if len(ids) == 0 {
		return validationError("AFFECTED requires a nonempty approved subset")
	}
	for i, id := range ids {
		if !slices.Contains(approved, id) || i > 0 && ids[i-1] == id {
			return validationError("execution scope contains a duplicate or unapproved case: " + id)
		}
	}
	if _, err := s.executionInheritance(wave, target.Mode, whitelist, ids, o.Reason); err != nil {
		return err
	}
	target.RequiredCases, target.ScopeReason = ids, o.Reason
	s.routingReleasedCapacity = true
	return nil
}

// executionBaseline selects the latest authoritative mode result. A checkpoint
// or transport error cannot replace it, and a later FAIL cannot inherit an older PASS.
func (s *State) executionBaseline(current *ValidationWave, mode coverage.ReviewKind) (*ValidationWave, *ValidationTarget) {
	index := slices.IndexFunc(s.Validation.Waves, func(w ValidationWave) bool { return w.ID == current.ID })
	for i := index - 1; i >= 0; i-- {
		old := &s.Validation.Waves[i]
		if !sameWaveCases(old, current, mode) {
			return nil, nil
		}
		for _, target := range old.Targets {
			result := s.Results[target.ResultAction].Validation
			if result != nil && result.Execution != nil && !result.Checkpoint && target.Mode == mode && target.Execution != nil && slices.Contains([]string{"PASS", "FAIL", "DISPOSED"}, target.Status) && target.Execution.Binding.Candidate == old.Candidate {
				if old.Generation != current.Generation && !s.validationWorkCarryAuthorized(target.ResultAction, target.Task) {
					return nil, nil
				}
				return old, target
			}
		}
	}
	return nil, nil
}

func (s *State) executionReuseAllowed(current, baseline *ValidationWave, mode coverage.ReviewKind, caseID string) bool {
	index := slices.IndexFunc(s.Validation.Waves, func(w ValidationWave) bool { return w.ID == current.ID })
	next := current
	for i := index - 1; i >= 0; i-- {
		old := &s.Validation.Waves[i]
		if !sameWaveCases(old, current, mode) {
			return false
		}
		if old.Candidate != next.Candidate {
			repairs, repairOnly := s.candidateRepairChanges(old, next)
			if !repairOnly || len(repairs) == 0 {
				return false
			}
			for _, work := range repairs {
				if slices.Contains(work.result.Validation.Development.AffectedCases, caseID) {
					return false
				}
			}
		}
		if old.ID == baseline.ID {
			return true
		}
		next = old
	}
	return false
}

// executionInheritance projects original PASS evidence for the complement of
// the fixed execution set. It is shared by host scope admission, input and results.
func (s *State) executionInheritance(wave *ValidationWave, mode coverage.ReviewKind, whitelist coverage.ApprovedWhitelist, required []string, reason string) ([]coverage.ExecutionRecord, error) {
	var inherited []coverage.ExecutionRecord
	for _, edge := range whitelist.Entries {
		if slices.Contains(required, edge.CaseID) {
			continue
		}
		row, err := s.inheritedExecutionRecord(wave, mode, edge, "", reason)
		if err != nil {
			return nil, err
		}
		inherited = append(inherited, row)
	}
	return inherited, nil
}

func (s *State) inheritedExecutionRecord(wave *ValidationWave, mode coverage.ReviewKind, edge coverage.WhitelistEntry, origin, reason string) (coverage.ExecutionRecord, error) {
	empty := coverage.ExecutionRecord{}
	baseline, target := s.executionBaseline(wave, mode)
	if wave.Scope != coverage.ScopeAffected || baseline == nil {
		return empty, validationError("case has no prior authority and must execute: " + edge.CaseID)
	}
	matches := func(row coverage.ExecutionRecord) bool {
		return row.ReviewKind == edge.ReviewKind && row.SourceID == edge.SourceID && row.PointID == edge.PointID && row.CaseID == edge.CaseID
	}
	latest := slices.IndexFunc(target.Execution.Records, matches)
	if latest < 0 || target.Execution.Records[latest].Result != coverage.ExecutedPass {
		return empty, validationError("case lacks a latest authoritative PASS and must execute: " + edge.CaseID)
	}
	if origin == "" {
		origin = baseline.Candidate.Identity
		if origin == wave.Candidate.Identity {
			origin = target.Execution.Records[latest].InheritedFromCandidate
		}
	}
	if origin == "" || origin == wave.Candidate.Identity {
		return empty, validationError("case has no distinct prior candidate evidence and must execute: " + edge.CaseID)
	}
	index := slices.IndexFunc(s.Validation.Waves, func(w ValidationWave) bool { return w.ID == wave.ID })
	for i := index - 1; i >= 0; i-- {
		source := &s.Validation.Waves[i]
		if source.Candidate.Identity != origin {
			continue
		}
		if !s.executionReuseAllowed(wave, source, mode, edge.CaseID) {
			break
		}
		for _, prior := range source.Targets {
			result := s.Results[prior.ResultAction].Validation
			if prior.Mode != mode || prior.Execution == nil || result == nil || result.Execution == nil || result.Checkpoint || source.Generation != wave.Generation && !s.validationWorkCarryAuthorized(prior.ResultAction, prior.Task) {
				continue
			}
			for _, row := range prior.Execution.Records {
				if !matches(row) || row.Result != coverage.ExecutedPass {
					continue
				}
				row.Provenance, row.InheritedFromCandidate, row.ReuseEvidence = coverage.ProvenanceInherited, origin, reason
				return row, nil
			}
		}
	}
	return empty, validationError("case has no unaffected original PASS and must execute: " + edge.CaseID)
}

func (s *State) carryExecutionScope(target *ValidationTarget, previous, current *ValidationWave) {
	if target.Kind != "QA" || current.Scope != coverage.ScopeAffected || slices.Contains(s.requiredWorkTasks(), target.Task) {
		return
	}
	original, old := s.targetResponsibility(current, target)
	if original == current {
		for _, prior := range previous.Targets {
			if prior.Mode == target.Mode && prior.Kind == "QA" {
				original, old = s.targetResponsibility(previous, prior)
				break
			}
		}
	}
	if original == current || original.Scope != current.Scope || original.Candidate != current.Candidate || !sameWaveCases(original, current, target.Mode) || original.Generation != current.Generation && !s.validationWorkCarryAuthorized(old.ResultAction, old.Task) {
		return
	}
	target.RequiredCases = slices.Clone(old.RequiredCases)
	target.ScopeReason = old.ScopeReason
}
