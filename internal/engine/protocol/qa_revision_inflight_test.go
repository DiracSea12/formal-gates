package protocol

import (
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
)

// Exercise public requests and result receipts with fixture contracts. No prompt
// text or installed gate file participates in the behavior under test.
func TestQARevisionWaitsForIssuedResults(t *testing.T) {
	for _, mode := range []string{"blackbox", "whitebox"} {
		for _, kind := range []string{"design", "review-pass", "review-fail"} {
			t.Run(mode+"/"+kind, func(t *testing.T) {
				e := preparationEngine(t, mode)
				gitReceipt(t, e, "git-base")
				if mode == "whitebox" {
					completeBatch(t, e, "B1")
					gitReceipt(t, e, "dev-1")
					completeBatch(t, e, "B2")
					gitReceipt(t, e, "dev-2")
					gitReceipt(t, e, "whitebox-base")
				}
				step := "design"
				if kind != "design" {
					completeQA(t, e, mode, "design")
					if mode == "whitebox" {
						gitReceipt(t, e, "tests-snapshot")
					}
					step = "review"
				}
				x := currentExecution(t, e, mode+"-"+step)
				claimAndAttach(t, e, x)
				if mode == "whitebox" && step == "design" {
					recordCoverage(t, e, x)
				}
				before, _ := e.Load()
				initialRevision := before.State.Regular.Preparation.QA[mode].Revision
				result := preparationResult(x)
				design := testDesign(before.State.Regular, strings.ToUpper(mode))
				if step == "design" {
					result.Design = &design
					if mode == "whitebox" {
						result.Manifest = []string{"main_test.go"}
						result.Reuse = []TestReuse{{CaseID: "WHITEBOX:C1", TestRef: "main_test.go::TestHello", CoverageJudgment: "exact output", Procedure: "go test -run TestHello", Observation: "hello", Comparison: "matches", Result: "EXECUTED_PASS"}}
					}
				} else {
					q := before.State.Regular.Preparation.QA[mode]
					result.Review = &coverage.ReviewResult{Binding: q.Design.Manifest.Binding, Scope: q.ReviewScope, SourceDecisions: []coverage.ReviewDecision{{ID: before.State.Regular.Requirement.Path, Status: coverage.StatusPass}}, PointDecisions: []coverage.ReviewDecision{{ID: strings.ToUpper(mode) + ":AC-001", Status: coverage.StatusPass}}, CaseDecisions: []coverage.ReviewDecision{{ID: strings.ToUpper(mode) + ":C1", Status: coverage.StatusPass}}}
					if kind == "review-fail" {
						result.Status = "FAIL"
						result.Message = "older review found a different gap"
						result.Review.SetStatus = coverage.StatusFail
						result.Review.CaseDecisions[0].Status = coverage.StatusFail
					}
				}
				const reason = "confirmed revision: compare the actual output"
				for _, why := range []string{"earlier revision request", reason} {
					regularChoice(t, e, RegularRequest{Action: "qa-revise", Target: mode, Reason: why}, "confirm")
				}
				waiting, _ := e.Load()
				q := waiting.State.Regular.Preparation.QA[mode]
				if !q.NeedsRevision || q.Revision != initialRevision || q.UserInstructions != reason {
					t.Fatalf("issued binding or revision lost before result: %+v", q)
				}
				if a := submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result}); a.Status != "ACCEPTED" {
					t.Fatal(a)
				}
				sourceUnavailable(t, e, x)
				if mode == "whitebox" && step == "design" {
					gitReceipt(t, e, "tests-snapshot")
				}
				next := currentExecution(t, e, mode+"-design")
				after, _ := e.Load()
				q = after.State.Regular.Preparation.QA[mode]
				if q.Revision != initialRevision+1 || q.NeedsRevision || q.Frozen || q.Design != nil || q.PreviousDesign == nil || q.UserInstructions != reason || next.Claim.Task == x.Claim.Task {
					t.Fatalf("confirmed revision did not continue: %+v", q)
				}
				expected := result
				if step == "design" {
					merged, _, err := mergeQADesign(nil, *result.Design, mode, nil)
					if err != nil {
						t.Fatal(err)
					}
					expected.Design = &merged
				}
				old := after.State.RegularExecutions[x.Claim.ActionID]
				if !old.Finished || digestOfCanonical(old.PreparationResult) != digestOfCanonical(expected) || old.Identity != result.Identity || after.State.Results[x.Claim.ActionID].Outcome != result.Status {
					t.Fatal("late result or native source binding lost")
				}
				if kind == "review-fail" && q.DesignGap != result.Message {
					t.Fatal("current review feedback lost beside user instructions")
				}
				if step == "review" && !reflect.DeepEqual(q.RetainedReview, result.Review) {
					t.Fatal("review evidence lost")
				}
				if mode == "whitebox" && (q.AuthoringBase != "tests-snapshot" || next.Claim.Candidate != "tests-snapshot" || len(q.PreviousDesign.TestFiles) != 1) {
					t.Fatal("test snapshot not retained")
				}
				if step == "design" && q.PreviousDesignActionID != x.Claim.ActionID {
					t.Fatal("design source provenance lost")
				}
				// The revision is consumed once, and an actual increment clears its reason.
				completeQA(t, e, mode, "design")
				settled, _ := e.Load()
				q = settled.State.Regular.Preparation.QA[mode]
				if q.UserInstructions != "" || q.Revision != initialRevision+1 {
					t.Fatal("revision repeated or reason not consumed")
				}
			})
		}
	}
}
