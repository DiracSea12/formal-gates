package protocol

import (
	"testing"

	"formal-gates/internal/engine/coverage"
)

// An unchanged obligation retains one offer while its current revision advances.
// Changed decision content supersedes the offer without consuming authorization.
func TestValidationOffersRemainAvailableUntilUserDecision(t *testing.T) {
	for _, kind := range []string{"RETRY", "QA_REVIEW", "REPAIR", "QA_RETRY", "VALIDATION_DETAIL_DISPOSITION", "THAW", "QA_ARTIFACT_REPAIR", "DISPOSE", KindDispatchBudgetDisposition} {
		t.Run(kind, func(t *testing.T) {
			s := &State{PendingAsks: map[string]PendingAsk{}, Decisions: map[string]RecordedDecision{}, Validation: &Validation{Sets: map[coverage.ReviewKind]*CaseSet{}}}
			request := ValidationRequest{Kind: kind, Target: "obligation", Mode: coverage.ReviewBlackbox}
			if !s.ensureValidationAsk(request, 1) {
				t.Fatal("initial offer not created")
			}
			var original string
			for id := range s.PendingAsks {
				original = id
			}
			for _, revision := range []uint64{3, 5} {
				if s.ensureValidationAsk(request, revision) {
					t.Fatal("revision alone replaced the obligation's offer")
				}
				rebindValidationAsks(s, revision)
				ask := s.PendingAsks[original]
				if len(s.PendingAsks) != 1 || ask.Resolved || ask.Validation.StateRevision != revision {
					t.Fatalf("current offer lost identity or revision: %+v", ask)
				}
				if len(s.Decisions) != 0 {
					t.Fatal("revision advance consumed user authorization")
				}
				if digestOfCanonical(s.validationRequestBasis(*ask.Validation)) != ask.Validation.Digest {
					t.Fatal("current revision retained a different decision basis")
				}
			}
			request.Category = "CASE_EXECUTABILITY"
			if !s.ensureValidationAsk(request, 6) {
				t.Fatal("changed decision content did not create a fresh offer")
			}
			if !s.PendingAsks[original].Resolved {
				t.Fatal("changed content left the replaced offer pending")
			}
			pending := 0
			for id, ask := range s.PendingAsks {
				if ask.Resolved {
					continue
				}
				pending++
				wantRequestDigest := digestOfCanonical(ask)
				ask.Resolved = true
				s.PendingAsks[id] = ask
				s.Decisions[id] = RecordedDecision{RequestID: id, Control: ControlValidation, Choice: "decline", Revision: 7}
				if s.requestDigest(id) != wantRequestDigest {
					t.Fatal("settlement changed the generated offer evidence")
				}
				beforeAsk, beforeDecision := digestOfCanonical(s.PendingAsks[id]), digestOfCanonical(s.Decisions[id])
				rebindValidationAsks(s, 8)
				if beforeAsk != digestOfCanonical(s.PendingAsks[id]) || beforeDecision != digestOfCanonical(s.Decisions[id]) || s.requestDigest(id) != wantRequestDigest {
					t.Fatal("commit binding rewrote resolved evidence")
				}
			}
			if pending != 1 {
				t.Fatalf("pending offers=%d, want one", pending)
			}
			if kind != "VALIDATION_DETAIL_DISPOSITION" && s.ensureValidationAsk(request, 9) {
				t.Fatal("real user decision was treated as display supersession")
			}
		})
	}
}

func TestValidationDecisionCreatesRequirementRequestEvidence(t *testing.T) {
	engine, state, _ := guardPipelineFixture(t)
	state.ensureValidationAsk(ValidationRequest{Kind: "QA_REVIEW", Mode: coverage.ReviewBlackbox, Target: state.qaReviewTarget(coverage.ReviewBlackbox)}, 7)
	var ask PendingAsk
	for _, current := range state.PendingAsks {
		if current.Validation != nil && !current.Resolved {
			ask = current
		}
	}
	if !ask.hasOption("requirement") {
		t.Fatal("validation offer does not permit requirement revision")
	}
	decision := &DecidePayload{Request: RequestID(ask.RequestID), Choice: "requirement", Validation: &ValidationDecision{Digest: ask.Validation.Digest, StateRevision: 7, Reason: "Clarify the requirement before another review"}}
	if err := engine.acceptValidationDecision(state, ask, decision, 7, 8); err != nil {
		t.Fatal(err)
	}
	requestID := "requirement:" + ask.Validation.Target + ":8"
	offer := state.PendingAsks[requestID]
	if offer.RequestID != requestID || offer.Control != ControlRequestRequirementChange || offer.BaseRevision != state.RequirementRevision() || offer.BaseGeneration != state.requirementGeneration() || !offer.hasOption("proceed") || !offer.hasOption("abort") {
		t.Fatalf("generated requirement offer = %+v", offer)
	}
	if _, external := state.Events[requestID]; external || state.requestDigest(requestID) != digestOfCanonical(offer) {
		t.Fatal("generated requirement request lost its complete offer evidence")
	}
}
