package protocol

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/engine/coverage"
)

func (s *State) validationRequestBasis(r ValidationRequest) json.RawMessage {
	var detail any
	switch r.Kind {
	case "RETRY":
		if intent, ok := s.PendingHostActions[r.Target]; ok && intent.Adapter != nil && intent.Adapter.Operation == "op.candidate" {
			_, _, max := s.candidateFailureRoute(intent)
			detail = struct {
				Intent      HostActionIntent
				Failures    []HostActionReceipt
				MaxAttempts int
			}{intent, s.candidateFailures(intent.Correlation), max}
			break
		}
		for key, a := range s.EndedAttempts {
			if key.String() == r.Target {
				var set *CaseSet
				if s.Validation != nil {
					set = s.Validation.Sets[r.Mode]
				}
				detail = struct {
					Attempt   Attempt
					Rejection RejectedResult
					CaseSet   *CaseSet
				}{a, s.RejectedResults[a.ActionID], set}
			}
		}
	case "QA_REVIEW":
		if s.Validation != nil {
			detail = s.Validation.Sets[r.Mode]
		}
	default:
		detail = s.Validation
	}
	r.Digest = ""
	r.StateRevision = 0
	r.Summary = nil
	value, _ := json.Marshal(struct {
		Request                              ValidationRequest
		Kind, Target                         string
		Mode                                 coverage.ReviewKind
		Detail                               any
		EngineAllowance, OwnerCost, TokenCap string
	}{r, r.Kind, r.Target, r.Mode, detail, "UNAVAILABLE", "UNAVAILABLE", "UNAVAILABLE"})
	return value
}
func (s *State) ensureValidationAsk(r ValidationRequest, nextRevision uint64) bool {
	var options []AskOption
	r, options = s.validationOffer(r)
	r.Summary = s.validationRequestBasis(r)
	r.Digest = digestOfCanonical(r.Summary)
	r.StateRevision = nextRevision
	var superseded []string
	for id, a := range s.PendingAsks {
		if a.Control != ControlValidation || a.Validation == nil || a.Validation.Kind != r.Kind || a.Validation.Target != r.Target {
			continue
		}
		if _, decided := s.Decisions[id]; decided && a.Validation.Digest == r.Digest && r.Kind != "VALIDATION_DETAIL_DISPOSITION" {
			return false
		}
		if a.Resolved {
			continue
		}
		if a.Validation.Digest == r.Digest && a.Validation.StateRevision >= nextRevision-1 {
			return false
		}
		superseded = append(superseded, id)
	}
	for _, id := range superseded {
		a := s.PendingAsks[id]
		a.Resolved = true
		s.PendingAsks[id] = a
	}
	id := fmt.Sprintf("validation:%s:%s:%d", r.Kind, r.Target, nextRevision)
	s.PendingAsks[id] = PendingAsk{RequestID: id, Control: ControlValidation, Validation: &r, Options: options}
	return true
}

// reconcileWorkerRetryAsks retires offers whose original duty has ended.
// Decisions and failure evidence remain immutable; host retries retain their
// separate intent owner.
func (e *Engine) reconcileWorkerRetryAsks(s *State) bool {
	current := map[string]bool{}
	for key, a := range s.EndedAttempts {
		if s.endedWorkCurrent(a, e.cfg.Definition) && a.RetryExhausted && !a.AwaitingResult {
			current[key.String()] = true
		}
	}
	changed := false
	for id, ask := range s.PendingAsks {
		r := ask.Validation
		if ask.Resolved || r == nil || r.Kind != "RETRY" || current[r.Target] {
			continue
		}
		if intent, ok := s.PendingHostActions[r.Target]; ok && intent.Adapter != nil && intent.Adapter.Operation == "op.candidate" {
			continue
		}
		ask.Resolved = true
		s.PendingAsks[id] = ask
		changed = true
	}
	return changed
}

func (e *Engine) ensureRetryAsks(s *State, nextRevision uint64) bool {
	changed := e.reconcileWorkerRetryAsks(s)
	for key, a := range s.EndedAttempts {
		if s.endedWorkCurrent(a, e.cfg.Definition) && a.RetryExhausted && !a.AwaitingResult {
			request := ValidationRequest{Kind: "RETRY", Target: key.String()}
			if a.Work != nil && a.Work.WaveID == "" && (s.validationRole(key, e.cfg.Definition) == "engine.qa.design" || s.validationRole(key, e.cfg.Definition) == "engine.qa.review.rationale") {
				request.Mode = s.validationMode(key)
			}
			changed = s.ensureValidationAsk(request, nextRevision) || changed
		}
	}
	return changed
}

// Pending disposition suspends execution without replacing an already issued
// Attempt. Request and decision records remain the authority for that pause.
func (s *State) validationDispositionBlocked(w *ValidationWave) bool {
	if w == nil {
		return false
	}
	var latest RecordedDecision
	for id, ask := range s.PendingAsks {
		if ask.Control != ControlValidation || ask.Validation == nil || ask.Validation.Kind != "VALIDATION_DETAIL_DISPOSITION" || ask.Validation.Target != w.ID {
			continue
		}
		if !ask.Resolved {
			return true
		}
		if decision := s.Decisions[id]; decision.Revision > latest.Revision {
			latest = decision
		}
	}
	if latest.Revision != 0 {
		return latest.Choice != "approve"
	}
	return false
}

func (s *State) validationDispatchBlocked(action string) bool {
	binding, ok := s.DispatchBindings[action]
	if !ok {
		return false
	}
	for _, work := range s.repairWork() {
		if work.task == binding.Task {
			return s.validationDispositionBlocked(work.wave)
		}
	}
	return false
}

func (e *Engine) acceptValidationRequest(s *State, r *ValidationRequest, nextRevision uint64) (*ValidationRequest, error) {
	if r == nil || s.Validation == nil {
		return nil, validationError("validation request needs an active pipeline and typed payload")
	}
	if r.Kind != "THAW" && r.Kind != "QA_ARTIFACT_REPAIR" && r.Kind != "DISPOSE" && r.Kind != "QA_RETRY" && r.Kind != "VALIDATION_DETAIL_DISPOSITION" {
		return nil, validationError("retry and extra-round offers are generated from engine obligations")
	}
	copy := *r
	if copy.Kind == "QA_RETRY" {
		w := s.currentValidationWave()
		if w == nil || w.Status == "INVALIDATED" || w.Status == "REPAIRED" {
			return nil, validationError("QA retry requires the current fixed validation candidate")
		}
		copy.Target = w.ID
		copy.Binding = WorkBinding{RequirementRevision: w.RequirementRevision, Generation: w.Generation, Candidate: w.Candidate.Identity, WaveID: w.ID}
	}
	copy, _ = s.validationOffer(copy)
	copy.Summary = s.validationRequestBasis(copy)
	copy.Digest = digestOfCanonical(copy.Summary)
	copy.StateRevision = nextRevision
	if copy.Kind == "THAW" || copy.Kind == "QA_ARTIFACT_REPAIR" || copy.Kind == "VALIDATION_DETAIL_DISPOSITION" && slices.Contains(copy.Remedies, "QA_ARTIFACT_REPAIR") {
		if !slices.Contains([]string{"REQUIREMENT_MEANING", "EXTERNAL_BEHAVIOR", "SUCCESS_FAILURE_CRITERIA", "CASE_EXECUTABILITY"}, copy.Category) {
			return nil, validationError("THAW category is not a stable semantic/executability change")
		}
		if s.Validation.Sets[copy.Mode] == nil {
			return nil, validationError("unknown case-set")
		}
	}
	if copy.Kind == "VALIDATION_DETAIL_DISPOSITION" {
		wave := s.currentValidationWave()
		if !s.validationDispositionReady(wave) || wave.ID != copy.Target || len(copy.Remedies) < 2 {
			return nil, validationError("detail disposition requires adjudicated findings and multiple concrete remedies")
		}
		copy.Binding.WaveID = wave.ID
		copy.Binding.Candidate = wave.Candidate.Identity
		available := s.validationObligations(wave)
		if len(copy.Obligations) == 0 {
			copy.Obligations = available
		}
		for _, id := range copy.Obligations {
			if !slices.Contains(available, id) {
				return nil, validationError("detail remedy names an obligation outside this wave")
			}
		}
		seen := map[string]bool{}
		for _, remedy := range copy.Remedies {
			if seen[remedy] || !slices.Contains([]string{"REPAIR", "QA_ARTIFACT_REPAIR", "REQUEST_REQUIREMENT_CHANGE", "DISMISS"}, remedy) {
				return nil, validationError("invalid or duplicate remedy")
			}
			if remedy == "REPAIR" && !s.candidateRepairPending(wave) {
				return nil, validationError("implementation repair requires current executable work")
			}
			seen[remedy] = true
		}
	}
	if copy.Kind == "DISPOSE" {
		w := s.currentValidationWave()
		if w == nil || w.Status != "COLLECTING" {
			return nil, validationError("error disposition requires a collecting wave")
		}
		t := w.Targets[copy.Target]
		if t == nil || (t.Status != "RUNTIME_ERROR" && t.Status != "RETRY_EXHAUSTED") {
			return nil, validationError("only an unresolved runtime/invalid-result target can be disposed")
		}
	}
	copy.Summary = s.validationRequestBasis(copy)
	copy.Digest = digestOfCanonical(copy.Summary)
	return &copy, nil
}
func (e *Engine) acceptValidationDecision(s *State, ask PendingAsk, p *DecidePayload, revision, nextRevision uint64) error {
	if ask.Control != ControlValidation || ask.Validation == nil {
		return validationError("decision is not for a validation request")
	}
	if p.Choice == "decline" {
		if p.Validation != nil {
			return validationError("decline has no typed action")
		}
		return nil
	}
	r := ask.Validation
	d := p.Validation
	if d == nil || strings.TrimSpace(d.Reason) == "" || d.StateRevision != revision || r.StateRevision != revision || d.Digest != r.Digest || digestOfCanonical(s.validationRequestBasis(*r)) != r.Digest {
		return validationError("authorization needs the current state revision, exact summary digest and non-empty reason")
	}
	if p.Choice == "requirement" {
		id := fmt.Sprintf("requirement:%s:%d", r.Target, nextRevision)
		s.PendingAsks[id] = PendingAsk{RequestID: id, Control: ControlRequestRequirementChange, BaseRevision: s.RequirementRevision(), BaseGeneration: s.requirementGeneration(), Options: []AskOption{{ID: requirementProceedOption, Label: "Confirm revised requirement"}, {ID: requirementAbortOption, Label: "Keep current requirement"}}}
		return nil
	}
	if p.Choice == "abort" {
		return nil
	}
	if p.Choice == "retry_qa" || r.Kind == "QA_RETRY" {
		return e.retryValidationQA(s, ask, d)
	}
	if p.Choice == "waive" {
		return e.waiveValidation(s, ask, d)
	}
	switch r.Kind {
	case "RETRY":
		if intent, ok := s.PendingHostActions[r.Target]; ok && intent.Adapter != nil && intent.Adapter.Operation == "op.candidate" {
			route, used, max := s.candidateFailureRoute(intent)
			if route.Action != RecoveryResumeAttempt || used < max || s.candidateRetryGranted(intent.Correlation) {
				return validationError("candidate retry is not an unused exhausted responsibility")
			}
			return nil
		}
		if p.Choice == "redesign" {
			set := s.Validation.Sets[r.Mode]
			if set == nil || set.DesignTask != r.Binding.Task {
				return validationError("redesign source is no longer current")
			}
			for key := range s.EndedAttempts {
				if key.String() == r.Target {
					delete(s.EndedAttempts, key)
				}
			}
			set.reopen(nil)
			return nil
		}
		found := false
		for key, a := range s.EndedAttempts {
			if key.String() == r.Target {
				if !s.endedWorkCurrent(a, e.cfg.Definition) || !a.RetryExhausted || s.retryGrantUsed(key) {
					return validationError("retry already extended or not exhausted")
				}
				a.MaxAttempts++
				a.RetryExhausted = false
				s.EndedAttempts[key] = a
				found = true
			}
		}
		if !found {
			return validationError("retry obligation no longer exists")
		}
	case "QA_REVIEW":
		set := s.Validation.Sets[r.Mode]
		if set == nil || r.Target != s.qaReviewTarget(r.Mode) {
			return validationError("QA review obligation no longer exists")
		}
		if p.Choice == "redesign" {
			set.reopen(nil)
		} else if p.Choice == "approve" {
			set.Status = "REVIEW"
			if len(set.ReviewHistory) > 0 {
				set.Candidate = set.ReviewHistory[len(set.ReviewHistory)-1].Candidate
			}
		} else {
			return validationError("unsupported pre-wave remedy")
		}
	case "REPAIR":
		work := s.nextRepairWork()
		if work == nil || work.wave.ID != r.Target || work.task != r.Binding.Task || s.repairExtraAuthorized(work) {
			return validationError("repair offer is no longer current")
		}
		if work.task == work.wave.Repair.Task {
			work.wave.ExtraAuthorized = true
		}
	case "THAW", "QA_ARTIFACT_REPAIR":
		set := s.Validation.Sets[r.Mode]
		if set == nil {
			return validationError("case-set missing")
		}
		if err := reopenCases(set, r.AffectedCases); err != nil {
			return err
		}
		set.Status = "THAWED"
		for _, work := range s.repairWork() {
			if work.result == nil {
				if err := s.retireRepairWork(work, "authorized QA artifact repair"); err != nil {
					return err
				}
			}
		}
		s.Validation.Candidate = nil
		if w := s.currentValidationWave(); w != nil {
			w.Status = "INVALIDATED"
			e.retireValidationWave(s, w.ID, "authorized QA artifact repair")
		}
	case "DISPOSE":
		copy := *d
		copy.Obligations = []string{r.Target}
		return e.waiveValidation(s, ask, &copy)
	case "VALIDATION_DETAIL_DISPOSITION":
		if !slices.Contains(r.Remedies, d.Remedy) {
			return validationError("selected remedy is not in the bound offer")
		}
		w := s.currentValidationWave()
		if !s.validationDispositionReady(w) || w.ID != r.Target {
			return validationError("detail obligation is no longer current")
		}
		switch d.Remedy {
		case "REPAIR":
			if !s.candidateRepairPending(w) {
				return validationError("implementation repair requires current executable work")
			}
		case "QA_ARTIFACT_REPAIR":
			set := s.Validation.Sets[r.Mode]
			if err := reopenCases(set, r.AffectedCases); err != nil {
				return err
			}
			set.Status = "THAWED"
			s.Validation.Candidate = nil
			w.Status = "INVALIDATED"
			e.retireValidationWave(s, w.ID, "authorized QA artifact repair")
		case "REQUEST_REQUIREMENT_CHANGE":
			id := fmt.Sprintf("requirement:%s:%d", w.ID, nextRevision)
			s.PendingAsks[id] = PendingAsk{RequestID: id, Control: ControlRequestRequirementChange, BaseRevision: s.RequirementRevision(), BaseGeneration: s.requirementGeneration(), Options: []AskOption{{ID: requirementProceedOption, Label: "Confirm revised requirement"}, {ID: requirementAbortOption, Label: "Keep current requirement"}}}
		case "DISMISS":
			copy := *d
			if len(copy.Obligations) == 0 {
				copy.Obligations = slices.Clone(r.Obligations)
			}
			return e.waiveValidation(s, ask, &copy)
		}

	default:
		return validationError("unknown validation control")
	}
	return nil
}
func (e *Engine) retireValidationWave(s *State, id, reason string) {
	for action, b := range s.DispatchBindings {
		if b.WaveID == id {
			if s.liveDispatch(action) {
				s.retireAction(action, "", reason)
			}
			delete(s.EndedAttempts, b.Task)
		}
	}
}
