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
		for key, a := range s.EndedAttempts {
			if key.String() == r.Target {
				detail = struct {
					Attempt   Attempt
					Rejection RejectedResult
				}{a, s.RejectedResults[a.ActionID]}
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
	r.Summary = s.validationRequestBasis(r)
	r.Digest = digestOfCanonical(r.Summary)
	r.StateRevision = nextRevision
	var superseded []string
	for id, a := range s.PendingAsks {
		if a.Control != ControlValidation || a.Validation == nil || a.Validation.Kind != r.Kind || a.Validation.Target != r.Target {
			continue
		}
		if _, decided := s.Decisions[id]; decided && a.Validation.Digest == r.Digest {
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
	s.PendingAsks[id] = PendingAsk{RequestID: id, Control: ControlValidation, Validation: &r, Options: []AskOption{{ID: "approve", Label: "Approve one bound action"}, {ID: "decline", Label: "Keep the obligation blocked"}}}
	return true
}
func (e *Engine) ensureRetryAsks(s *State, nextRevision uint64) bool {
	changed := false
	for key, a := range s.EndedAttempts {
		if a.Business && a.RetryExhausted && !s.retryGrantUsed(key) {
			changed = s.ensureValidationAsk(ValidationRequest{Kind: "RETRY", Target: key.String()}, nextRevision) || changed
		}
	}
	return changed
}

func (e *Engine) acceptValidationRequest(s *State, r *ValidationRequest, nextRevision uint64) (*ValidationRequest, error) {
	if r == nil || s.Validation == nil {
		return nil, validationError("validation request needs an active pipeline and typed payload")
	}
	if r.Kind != "THAW" && r.Kind != "QA_ARTIFACT_REPAIR" && r.Kind != "DISPOSE" && r.Kind != "VALIDATION_DETAIL_DISPOSITION" {
		return nil, validationError("retry and extra-round offers are generated from engine obligations")
	}
	copy := *r
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
		seen := map[string]bool{}
		for _, remedy := range copy.Remedies {
			if seen[remedy] || !slices.Contains([]string{"REPAIR", "QA_ARTIFACT_REPAIR", "REQUEST_REQUIREMENT_CHANGE", "DISMISS"}, remedy) {
				return nil, validationError("invalid or duplicate remedy")
			}
			if remedy == "REPAIR" && wave.Repair == nil {
				return nil, validationError("implementation repair requires a verified problem")
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
	if p.Choice != "approve" || d == nil || strings.TrimSpace(d.Reason) == "" || d.StateRevision != revision || r.StateRevision != revision || d.Digest != r.Digest || digestOfCanonical(s.validationRequestBasis(*r)) != r.Digest {
		return validationError("authorization needs the current state revision, exact summary digest and non-empty reason")
	}
	switch r.Kind {
	case "RETRY":
		found := false
		for key, a := range s.EndedAttempts {
			if key.String() == r.Target {
				if !a.RetryExhausted || s.retryGrantUsed(key) {
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
		if s.Validation == nil || s.Validation.Sets[r.Mode] == nil {
			return validationError("QA review obligation no longer exists")
		}
	case "REPAIR":
		w := s.currentValidationWave()
		if w == nil || w.ID != r.Target || !s.validationRepairPending(w) || w.ExtraAuthorized {
			return validationError("repair offer is no longer current")
		}
		w.ExtraAuthorized = true
	case "THAW", "QA_ARTIFACT_REPAIR":
		set := s.Validation.Sets[r.Mode]
		if set == nil {
			return validationError("case-set missing")
		}
		if err := reopenCases(set, r.AffectedCases); err != nil {
			return err
		}
		set.Status = "THAWED"
		s.Validation.Candidate = nil
		if w := s.currentValidationWave(); w != nil {
			w.Status = "INVALIDATED"
			e.retireValidationWave(s, w.ID, "authorized QA artifact repair")
		}
	case "DISPOSE":
		w := s.currentValidationWave()
		t := w.Targets[r.Target]
		t.Status = "DISPOSED"
		t.DispositionReason = d.Reason
		delete(s.EndedAttempts, t.Task)
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
			for _, finding := range s.Validation.Findings {
				if target := s.validationFindingTarget(w, finding); target != nil {
					target.Status = "DISPOSED"
					target.DispositionReason = d.Reason
				}
			}
			w.Status = "ADJUDICATING"
			e.settleValidationWave(s)
			e.retireValidationWave(s, w.ID, "user dismissed the repair obligation: "+d.Reason)
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
