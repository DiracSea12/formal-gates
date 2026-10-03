package protocol

import (
	"testing"

	"formal-gates/internal/engine/coverage"
)

// Superseding a stale display does not consume a user's authorization decision.
func TestValidationOffersRemainAvailableUntilUserDecision(t *testing.T) {
	for _, kind := range []string{"RETRY", "QA_REVIEW", "REPAIR"} {
		t.Run(kind, func(t *testing.T) {
			s := &State{PendingAsks: map[string]PendingAsk{}, Decisions: map[string]RecordedDecision{}, Validation: &Validation{Sets: map[coverage.ReviewKind]*CaseSet{}}}
			request := ValidationRequest{Kind: kind, Target: "obligation", Mode: coverage.ReviewBlackbox}
			for _, revision := range []uint64{1, 3, 5} {
				if !s.ensureValidationAsk(request, revision) {
					t.Fatalf("current offer suppressed at revision %d", revision)
				}
				pending := 0
				for _, ask := range s.PendingAsks {
					if !ask.Resolved {
						pending++
						if ask.Validation.StateRevision != revision {
							t.Fatal("pending offer retained stale revision")
						}
					}
				}
				if pending != 1 {
					t.Fatalf("pending offers=%d, want one", pending)
				}
				if s.ensureValidationAsk(request, revision+1) {
					t.Fatal("unchanged state replaced its current offer")
				}
			}
			for id, ask := range s.PendingAsks {
				if !ask.Resolved {
					wantRequestDigest := digestOfCanonical(ask)
					ask.Resolved = true
					s.PendingAsks[id] = ask
					s.Decisions[id] = RecordedDecision{RequestID: id, Control: ControlValidation, Choice: "decline", Revision: 6}
					if s.requestDigest(id) != wantRequestDigest {
						t.Fatal("settlement changed the generated validation offer evidence")
					}
				}
			}
			if s.ensureValidationAsk(request, 7) {
				t.Fatal("a real user decision was treated as display supersession")
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
