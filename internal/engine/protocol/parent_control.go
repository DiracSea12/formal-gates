package protocol

import (
	"formal-gates/internal/engine/runtime"
)

// RequirementImpact retains the operator's complete changed and transitive
// inventory. Dispositions remain explicit user decisions in the same package.
type RequirementImpact struct {
	ChangedItems      []string `json:"changedItems"`
	TransitiveImpacts []string `json:"transitiveImpacts"`
	CarryReasons      []string `json:"carryReasons"`
}

// ChildRequirementDecision binds a child's exact work and material inventory to
// the parent's confirmation. Evidence digests identify original bytes, not a
// rewritten copy of those bytes under the new revision.
type ChildRequirementDecision struct {
	EnteredMainline      bool                      `json:"enteredMainline,omitempty"`
	ChildRunID           string                    `json:"childRunId"`
	SliceID              string                    `json:"sliceId"`
	RequirementRevision  string                    `json:"requirementRevision"`
	WorkDecisions        []RequirementWorkDecision `json:"workDecisions"`
	ReceiptDigest        string                    `json:"receiptDigest,omitempty"`
	ReceiptDisposition   string                    `json:"receiptDisposition,omitempty"`
	CandidateDigest      string                    `json:"candidateDigest,omitempty"`
	CandidateDisposition string                    `json:"candidateDisposition,omitempty"`
	RouteDigest          string                    `json:"routeDigest"`
	RouteDisposition     string                    `json:"routeDisposition"`
	Reason               string                    `json:"reason"`
}

// ParentRequirementAuthority references the existing master request and accepted
// confirmation. The local child revision always belongs to its own store.
type ParentRequirementAuthority struct {
	RequestID      string             `json:"requestId"`
	RequestDigest  string             `json:"requestDigest"`
	DecisionDigest string             `json:"decisionDigest,omitempty"`
	Pending        bool               `json:"pending"`
	Confirmation   *ConfirmedRevision `json:"confirmation,omitempty"`
}

func (s *State) ChildRequirementMaterial(work []RequirementWorkDecision) ChildRequirementDecision {
	d := ChildRequirementDecision{ChildRunID: s.RunID, SliceID: s.SliceChild.SliceID, RequirementRevision: s.RequirementRevision(), WorkDecisions: work, RouteDigest: s.validationRouteDigest()}
	if s.SliceReceipt != nil {
		d.ReceiptDigest = s.SliceReceipt.Digest
	}
	if s.Validation != nil && s.Validation.Candidate != nil {
		d.CandidateDigest = s.Validation.Candidate.Digest
	}
	return d
}

func (s *State) childRequirementDecision(childID string) (*ConfirmedRevision, *ChildRequirementDecision) {
	for i := len(s.RequirementRevisions) - 1; i >= 0; i-- {
		entry := &s.RequirementRevisions[i]
		for j := range entry.Children {
			if entry.Children[j].ChildRunID == childID {
				return entry, &entry.Children[j]
			}
		}
	}
	return nil, nil
}

// SyncParentRequirement is used under the facade's parent→child write window by
// normal submission and Drive recovery. It applies the same requirement handler
// as local confirmations, without inventing a second child user decision.
func (e *Engine) SyncParentRequirement(parent *State, input Observation) error {
	e = e.observing(input)
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if s.SliceChild == nil || s.Phase == runtime.PhaseTerminal {
		return nil
	}
	child, found := parent.CreationChildByRun(s.RunID)
	if !found || parent.SliceChildren[s.SliceChild.SliceID] != s.RunID && s.Phase != runtime.PhasePreparing {
		return validationError("child has no current parent membership")
	}
	if !parent.preparedChild(child, s) {
		return validationError("child creation origin differs from its master")
	}
	if _, aborted := parent.AbortDecision(); aborted {
		return e.CancelChild(parent, input)
	}
	changed := false
	pending := false
	for id, ask := range parent.PendingAsks {
		if ask.Control != ControlRequestRequirementChange || ask.Resolved {
			continue
		}
		authority := &ParentRequirementAuthority{RequestID: id, RequestDigest: parent.requestDigest(id), Pending: true}
		if digestOfCanonical(s.SliceChild.ParentChange) != digestOfCanonical(authority) {
			s.SliceChild.ParentChange = authority
			changed = true
		}
		pending = true
		break
	}
	if !pending {
		entry, choice := parent.childRequirementDecision(s.RunID)
		if entry != nil && entry.Revision == parent.RequirementRevision() && (s.SliceChild.ParentChange == nil || s.SliceChild.ParentChange.RequestID != entry.RequestID || s.SliceChild.ParentChange.Pending) {
			decision := parent.Decisions[entry.RequestID]
			authority := &ParentRequirementAuthority{RequestID: entry.RequestID, RequestDigest: parent.requestDigest(entry.RequestID), DecisionDigest: parent.Events[decision.EventID].Digest, Confirmation: entry}
			if s.RequirementRevision() != entry.Revision {
				confirmed := parent.PendingAsks[entry.RequestID].Package
				if confirmed == nil {
					return validationError("parent confirmation has no accepted requirement package")
				}
				pkg := RequirementDecisionPackage{Intake: confirmed.Intake, Coverage: confirmed.Coverage, CandidateRevision: confirmed.CandidateRevision, DeltaDigest: confirmed.DeltaDigest, ImpactDigest: confirmed.ImpactDigest, Source: confirmed.Source, Classification: confirmed.Classification, FindingDigest: confirmed.FindingDigest, WorkDecisions: choice.WorkDecisions, RedoRoute: choice.RouteDisposition == RequirementWorkRedo}
				payload := &RequirementDecisionPayload{Intake: pkg.Intake, Coverage: pkg.Coverage, BaseRevision: s.RequirementRevision(), CandidateRevision: pkg.CandidateRevision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, FindingDigest: pkg.FindingDigest, WorkDecisions: pkg.WorkDecisions, RedoRoute: pkg.RedoRoute, Reason: choice.Reason}
				s.SliceChild.ParentChange = authority
				ask := PendingAsk{Control: ControlRequestRequirementChange, BaseRevision: s.RequirementRevision(), BaseGeneration: s.requirementGeneration(), Package: &pkg}
				document := ""
				if parent.validationRequirementCurrent() {
					document = parent.Validation.Document.Content
				}
				if err := e.admitRequirementDecision(s, ask, entry.RequestID, &DecidePayload{Choice: requirementProceedOption, Requirement: payload}, revision+1, input, document); err != nil {
					return err
				}
				if choice.ReceiptDisposition == RequirementWorkRedo {
					if err := s.reopenSliceCheckpoint(); err != nil {
						return err
					}
				}
				if choice.CandidateDisposition == RequirementWorkRedo && s.Validation != nil {
					s.Validation.Candidate = nil
				}
			}
			s.SliceChild.ParentChange = authority
			changed = true
		} else if s.SliceChild.ParentChange != nil && s.SliceChild.ParentChange.Pending {
			s.SliceChild.ParentChange = nil
			changed = true
			e.replayQuarantinedResults(s, "parent-change-declined", revision+1, input)
		}
	}
	if !pending && !s.resetPending() && s.Phase != runtime.PhaseSliceReady && parent.validationRequirementCurrent() && parent.RequirementRevision() == s.RequirementRevision() && s.Validation != nil && !s.validationRequirementCurrent() {
		observation := &OperatorObservationPayload{Subject: s.RequirementRevision(), Validation: &ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: s.RequirementRevision(), Document: parent.Validation.Document.Content}}
		if err := e.acceptValidationObservation(s, observation, revision, revision+1); err != nil {
			return err
		}
		changed = true
	}
	reviewPending := !parent.PreDevelopmentReviewsSatisfied()
	if reviewPending != s.SliceChild.ParentReviewPending {
		s.SliceChild.ParentReviewPending = reviewPending
		changed = true
	}
	if changed {
		_, err = e.commit(s, revision, input)
	}
	return err
}

func (s *State) childReceiptCarried(r SliceReceipt) bool {
	entry, choice := s.childRequirementDecision(r.Receipt.ChildRunID)
	return entry != nil && entry.Revision == s.RequirementRevision() && choice.ReceiptDigest == r.Digest && choice.ReceiptDisposition == RequirementWorkCarry
}
func (s *State) parentReviewReady() bool {
	return s.SliceChild == nil || !s.SliceChild.ParentReviewPending
}
