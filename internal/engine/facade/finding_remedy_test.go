package facade

// Finding corrections preserve the distinction between disposition and a
// requirement-confirmed remedy through submission, recovery, and replay.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

func TestFindingSeverityCorrectionKeepsRemedyConfirmation(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		name := "unconfirmed remedy remains pending"
		if confirmed {
			name = "confirmed remedy remains complete"
		}
		t.Run(name, func(t *testing.T) {
			d := startValidationDriver(t, nil)
			var id string
			for key, pending := range d.run.PendingActions {
				if pending.ReviewSeries == review.ProductReview {
					id = key
				}
			}
			if id == "" {
				t.Fatal("product review missing")
			}
			d.receipt(id)
			raw, err := protocol.NewReviewWorkerResultEvent(d.eventID(), id, d.host(), "original-source", protocol.ReviewResultEvidence{
				Action: review.ProductReview, DispatchID: id, RequirementRevision: d.revision, Status: "PASS",
				Findings: []review.Finding{{Severity: "P2", Message: "Clarify the requirement title to identify its input command behavior", NormalPath: "Read the confirmed command requirement", ActualError: "The title does not identify the command behavior"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(raw)
			verifyReviewFixture(t, d, raw)
			candidate, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlReviewFindingDecision), Choice: "confirm", Reason: "Confirm the bounded title clarification"})
			if err != nil {
				t.Fatal(err)
			}
			if _, d.run, err = submitControlWithHost(t, d.f, candidate); err != nil {
				t.Fatal(err)
			}
			if d.run.Review.Actions[0].Candidates[0].Findings[0].Revised {
				t.Fatal("finding confirmation alone completed its remedy")
			}
			if confirmed {
				path := filepath.Join(d.f.root, "requirements.md")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(strings.Replace(string(data), "### REQ-001：命令", "### REQ-001：命令输入行为", 1)), 0600); err != nil {
					t.Fatal(err)
				}
				work := make([]WorkChoice, len(d.run.RequirementWork))
				for i := range work {
					work[i] = WorkChoice{Disposition: protocol.RequirementWorkCarry, Reason: "Only the title wording changes; the complete command requirement and acceptance remain identical"}
				}
				change, err := d.f.PrepareControl(ControlInput{Request: protocol.ControlRequestRequirementChange, Requirement: &RequirementChangeInput{
					Classification: protocol.RequirementClassificationPreserved, Review: 1, Candidate: 1, Finding: 1, Work: work,
					Impact: protocol.RequirementImpact{ChangedItems: []string{"command title"}, CarryReasons: []string{"Requirement, acceptance and sources remain identical"}},
				}})
				if err != nil {
					t.Fatal(err)
				}
				if _, d.run, err = submitControlWithHost(t, d.f, change); err != nil {
					t.Fatal(err)
				}
				confirmation, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "Confirm this complete title correction delta"})
				if err != nil {
					t.Fatal(err)
				}
				if _, d.run, err = submitControlWithHost(t, d.f, confirmation); err != nil {
					t.Fatal(err)
				}
				if !d.run.Review.Actions[0].Candidates[0].Findings[0].Revised {
					t.Fatal("confirmed original remedy not complete")
				}
			}
			before := d.run.Review.Actions[0].Completed
			wantNext := d.run.Next.Kind
			correction := ControlInput{Request: protocol.ControlReviewFindingDecision, Review: 1, Candidate: 1, Finding: 1, Choice: "reseveritize", Severity: "P3", SeverityRationale: "The title clarification is editorial and has no effect on normal execution", Reason: "Correct severity only; retain the original finding and remedy"}
			request, err := d.f.PrepareControl(correction)
			if err != nil {
				t.Fatal(err)
			}
			if _, d.run, err = submitControlWithHost(t, d.f, request); err != nil {
				t.Fatal(err)
			}
			correction.Request = ""
			correction.Ask = askPosition(t, d.run, protocol.ControlReviewFindingDecision)
			decision, err := d.f.PrepareControl(correction)
			if err != nil {
				t.Fatal(err)
			}
			accepted, updated, err := submitControlWithHost(t, d.f, decision)
			if err != nil {
				t.Fatal(err)
			}
			d.run = updated
			finding := d.run.Review.Actions[0].Candidates[0].Findings[0]
			if before != 1 || d.run.Review.Actions[0].Completed != before || d.run.Review.Actions[0].EffectiveStatus != "PASS" || finding.Severity != "P3" || finding.Revised != confirmed || d.run.Next.Kind != wantNext {
				t.Fatalf("severity correction changed the remedy obligation: completed=%d status=%s finding=%+v next=%+v", d.run.Review.Actions[0].Completed, d.run.Review.Actions[0].EffectiveStatus, finding, d.run.Next)
			}
			d.reopen()
			replay, run, err := submitControlWithHost(t, d.f, decision)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(replay, accepted) || run.Review.Actions[0].Completed != before || run.Review.Actions[0].Candidates[0].Findings[0].Revised != confirmed || run.Next.Kind != wantNext {
				t.Fatalf("recovery or replay changed the remedy obligation: replay=%+v review=%+v next=%+v", replay, run.Review, run.Next)
			}
			if confirmed {
				state := driverState(t, d)
				source := state.ReviewSeries.ProductReview.RawCandidates[0]
				other := raw.Result.Review.Findings[0]
				other.Message = "Clarify the command's output description"
				other.ActualError = "The output description does not identify its result format"
				set, _, err := review.RecordCandidate(review.NewSet(), state.RunID, review.ProductReview,
					review.Dispatch{ID: source.DispatchID, RequirementRevision: source.RequirementRevision},
					review.Result{Status: "PASS", Findings: []review.Finding{other}},
					review.OperatorVerification{Checks: source.OperatorVerification.Checks, Evidence: "The distinct output-description finding differs from the input-title finding in message and observed error; the title delta cannot establish its remedy"})
				if err != nil {
					t.Fatal(err)
				}
				otherFinding := set.ProductReview.RawCandidates[0].Findings[0]
				if otherFinding.Digest == finding.Digest || state.ReviewFindingRevised(otherFinding) {
					t.Fatalf("original remedy covered a distinct finding: %+v", otherFinding)
				}
			}
		})
	}
}
