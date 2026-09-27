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
					ask.Resolved = true
					s.PendingAsks[id] = ask
					s.Decisions[id] = RecordedDecision{RequestID: id, Control: ControlValidation, Choice: "decline", Revision: 6}
				}
			}
			if s.ensureValidationAsk(request, 7) {
				t.Fatal("a real user decision was treated as display supersession")
			}
		})
	}
}
