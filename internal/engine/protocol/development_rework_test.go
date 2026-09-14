package protocol

import (
	"reflect"
	"strings"
	"testing"
)

func TestDevelopmentReworkUsesConfirmedPendingIncrement(t *testing.T) {
	for _, target := range []string{"", "B1"} {
		t.Run("target="+target, func(t *testing.T) {
			e := preparationEngine(t, "whitebox")
			gitReceipt(t, e, "base")
			completeBatch(t, e, "B1")
			gitReceipt(t, e, "first")
			completeBatch(t, e, "B2")
			gitReceipt(t, e, "second")
			gitReceipt(t, e, "whitebox")
			x := currentExecution(t, e, "whitebox-design")
			claimAndAttach(t, e, x)
			recordCoverage(t, e, x)
			snap, _ := e.Load()
			design := testDesign(snap.State.Regular, "WHITEBOX")
			failed := preparationResult(x)
			failed.Status = "FAIL"
			failed.Message = "fixture production returned red; expected blue; evidence: trial.log"
			failed.Design = &design
			failed.Reuse = []TestReuse{{CaseID: "WHITEBOX:C1", TestRef: "main_test.go::TestHello", CoverageJudgment: "exact output", Procedure: "fixture trial", Observation: "red", Comparison: "red differs from blue", Result: "EXECUTED_FAIL"}}
			submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &failed})
			sourceUnavailable(t, e, x)
			before, _ := e.Load()
			oldFailure := before.State.RegularExecutions[x.Claim.ActionID]
			regularChoice(t, e, RegularRequest{Action: "development-increment", Target: "B1", Meaning: "preserved", Reason: "repair first batch only", Increment: &DevelopmentIncrement{Kind: "optimize", BatchID: "B1", WorkspaceView: "fixture view"}, Impact: map[string]string{"batch:B2": "unaffected", "whitebox-review": "affected"}}, "confirm")
			blocked, _ := e.Load()
			p := blocked.State.Regular.Preparation
			key := p.BatchTasks["B1"]
			if p.Blocked != "PRODUCTION_FAILURE" || p.ReworkReason != failed.Message || p.Commits["B1"].After != "" || p.Commits["B2"].After != "second" {
				t.Fatal("increment did not retain failure and exact pending work")
			}
			// Exercise the public request path from the already-confirmed in-flight state.
			if target != "" {
				for _, bad := range []string{"B2", "missing"} {
					q := RegularRequest{Action: "development-rework", Target: bad, Reason: "conflicting selection"}
					ev, _ := NewRegularEvent(EventID("reject-"+bad), RegularEvent{Operation: "REQUEST", ExpectedRevision: blocked.Revision, Request: &q})
					if _, err := e.Submit(ev, fingerprintOf(t, e)); err == nil || !strings.Contains(err.Error(), "pending Batch B1") {
						t.Fatalf("conflict accepted: %v", err)
					}
				}
				// A run-wide requirement-change Ask may pause the selected repair.
				// Cancel it through the existing decision without changing the requirement.
				regularChoice(t, e, RegularRequest{Action: "requirement-change", Origin: "USER_INITIATED_CHANGE", Reason: "pause for engine repair; requirement unchanged"}, "cancel")
				resumed, _ := e.Load()
				if resumed.State.Regular.ChangePending || !reflect.DeepEqual(resumed.State.Regular.Requirement, blocked.State.Regular.Requirement) || !reflect.DeepEqual(resumed.State.Regular.DevelopmentIncrements, blocked.State.Regular.DevelopmentIncrements) || resumed.State.Regular.Preparation.Blocked != "PRODUCTION_FAILURE" {
					t.Fatal("cancelling pause changed requirement, increment or production failure")
				}
			}
			q := RegularRequest{Action: "development-rework", Target: target, Reason: "resume the already confirmed repair"}
			submitRegular(t, e, RegularEvent{Operation: "REQUEST", Request: &q})
			ask, _ := e.Load()
			if ask.State.Regular.Preparation.Blocked != "PRODUCTION_FAILURE" {
				t.Fatal("request alone released failure")
			}
			a := ask.State.Regular.Ask
			submitRegular(t, e, RegularEvent{Operation: "DECIDE", Decision: &RegularDecision{RequestID: a.ID, Token: RegularFreshness(ask.Revision, a.ID), Digest: a.Digest, Choice: "confirm"}})
			opened, _ := e.Load()
			next := currentExecution(t, e, "development-worker")
			if next.Claim.Task != key || next.Claim.Operation != "spawn" || next.Number != 1 || next.Claim.Identity != "" {
				t.Fatalf("confirmed increment lineage changed: %+v", next)
			}
			if opened.State.Regular.Preparation.Blocked != "" || opened.State.Regular.Preparation.ReworkReason != failed.Message || opened.State.Regular.Preparation.Rework != 1 {
				t.Fatal("rework lost production reason")
			}
			if !reflect.DeepEqual(opened.State.RegularExecutions[x.Claim.ActionID], oldFailure) || !reflect.DeepEqual(opened.State.Regular.DevelopmentIncrements, blocked.State.Regular.DevelopmentIncrements) || !reflect.DeepEqual(opened.State.Regular.Preparation.Batches["B2"], before.State.Regular.Preparation.Batches["B2"]) || !reflect.DeepEqual(opened.State.Regular.Preparation.Commits["B2"], before.State.Regular.Preparation.Commits["B2"]) {
				t.Fatal("unrelated PASS, failure evidence or increment receipt changed")
			}
			completeBatch(t, e, "B1")
			gitReceipt(t, e, "repaired")
			gitReceipt(t, e, "new-whitebox")
			after, _ := e.Load()
			if after.State.Regular.Preparation.Commits["B2"].After != "second" || after.State.Regular.Preparation.Candidate != nil {
				t.Fatal("unrelated batch reopened or failure declared PASS")
			}
			currentExecution(t, e, "whitebox-design")
		})
	}
}
