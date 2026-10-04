package protocol

import (
	"reflect"
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
			if kind == "REPAIR" || kind == "QA_RETRY" {
				_, s = guardRepairFixture(t)
				request.Target = s.Validation.Waves[0].ID
				request.Binding.WaveID = request.Target
			}
			decisions := digestOfCanonical(s.Decisions)
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
				if digestOfCanonical(s.Decisions) != decisions {
					t.Fatal("revision advance changed user authorization")
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

func TestRepairChoicesRefreshAtBothCommitSeams(t *testing.T) {
	for _, seam := range []string{"advance", "rebind"} {
		t.Run(seam, func(t *testing.T) {
			_, state := guardRepairFixture(t)
			source := state.Validation.Waves[0]
			state.ensureValidationAsk(ValidationRequest{Kind: "REPAIR", Target: source.ID}, 7)
			var prior PendingAsk
			for _, ask := range state.PendingAsks {
				if ask.Validation != nil && !ask.Resolved {
					prior = ask
				}
			}
			if !prior.hasOption("retry_qa") || !prior.hasOption("waive") {
				t.Fatal("real repair fixture lacks legal original choices")
			}
			projection := source
			projection.ID = "wave-full-validation"
			projection.Scope = coverage.ScopeFull
			if source.Scope == projection.Scope {
				projection.Scope = coverage.ScopeAffected
			}
			projection.Status, projection.Repair = "COLLECTING", nil
			state.Validation.Waves = append(state.Validation.Waves, projection)
			decisions := digestOfCanonical(state.Decisions)
			if seam == "advance" {
				if !state.reconcileValidationRemedyAsks(8) {
					t.Fatal("eligibility change did not count as business advancement")
				}
			} else {
				rebindValidationAsks(state, 8)
			}
			retired := state.PendingAsks[prior.RequestID]
			if !retired.Resolved {
				t.Fatal("ineligible source QA choice received another fresh binding")
			}
			retired.Resolved = false
			if !reflect.DeepEqual(retired, prior) || digestOfCanonical(state.Decisions) != decisions {
				t.Fatal("choice replacement rewrote request or decision evidence")
			}
			pending := 0
			for _, ask := range state.PendingAsks {
				if ask.Resolved {
					continue
				}
				pending++
				if ask.hasOption("retry_qa") || !ask.hasOption("approve") || !ask.hasOption("waive") {
					t.Fatal("replacement choices disagree with actual source eligibility")
				}
				if digestOfCanonical(state.validationRequestBasis(*ask.Validation)) != ask.Validation.Digest {
					t.Fatal("replacement has a different complete decision basis")
				}
			}
			if pending != 1 {
				t.Fatal("choice replacement did not keep exactly one owner")
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
