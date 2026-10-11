package facade

import (
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

func TestReadinessClassificationConstrainsBlockers(t *testing.T) {
	for _, category := range []string{"", "implementation-detail", "acceptance-entry"} {
		t.Run(category, func(t *testing.T) {
			d := startValidationDriver(t, nil)
			passPreDevelopmentReviews(t, d, commandPlan(), review.ProductReview)
			id := ""
			for key, p := range d.run.PendingActions {
				if p.ReviewSeries == review.StartReadiness {
					id = key
				}
			}
			if id == "" {
				t.Fatal("no readiness dispatch")
			}
			issued := d.run.PendingActions[id]
			d.receipt(id)
			event, err := protocol.NewReviewWorkerResultEvent(d.eventID(), id, DefaultProvider, "independent-original-review", protocol.ReviewResultEvidence{
				Action: review.StartReadiness, DispatchID: id, RequirementRevision: d.revision, Status: "FAIL", GranularityReview: commandPlan(),
				Findings:     []review.Finding{{Severity: "P1", Category: category, Message: "Use a native library instead of the proposed helper", SeverityRationale: "The proposed helper adds unnecessary implementation work", Requirement: "Use the simplest existing owner", NormalPath: "Technical review of the proposed helper", ActualError: "The proposed helper duplicates native capability", DeferralImpact: "Developer must remove the duplicate helper", Locations: []string{"requirements.md"}, Verification: review.FindingVerification{Scope: "checked confirmed solution", Severity: "this is an implementation choice", Causality: "helper duplicates native operation", Reproduction: "compared solution to existing owner", Classification: "implementation detail"}}},
				Verification: review.OperatorVerification{Checks: []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"}, Evidence: "checked the submitted proposed solution and reported classification"},
			})
			if err != nil {
				t.Fatal(err)
			}
			accepted, _, err := d.f.Submit(event)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			series := snapshot.State.ReviewSeries.StartReadiness
			if category == "acceptance-entry" {
				if accepted.Status != "ACCEPTED" || series.Completed != 1 || series.Effective.Status != "FAIL" {
					t.Fatal("valid category did not produce original blocker", accepted, series)
				}
			} else {
				if accepted.Status == "ACCEPTED" || series.Completed != 0 || len(series.RawCandidates) != 0 {
					t.Fatal("invalid technical category created an effective blocker", accepted.Status, series.Completed, series.Effective.Status)
				}
				rejected := snapshot.State.RejectedResults[id]
				retry := snapshot.State.Attempts[issued.Task]
				if rejected.Status != "REJECTED_INVALID_RESULT" || rejected.Task != issued.Task || rejected.AttemptID != issued.AttemptID || retry.PreviousAction != id || retry.Attempts != 1 || retry.MaxAttempts != 2 || retry.RetryExhausted {
					t.Fatal("classification rejection did not retain original finite retry", rejected, retry)
				}
				if _, exists := snapshot.State.Results[id]; exists {
					t.Fatal("invalid classification published an effective worker result")
				}
			}
			t.Logf("category=%q acceptance=%s semanticRounds=%d effective=%s rawCandidates=%d blockers=%v", category, accepted.Status, series.Completed, series.Effective.Status, len(series.RawCandidates), series.Effective.BlockingFindingIDs)
		})
	}
}
