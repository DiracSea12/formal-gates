package protocol

import (
	"formal-gates/internal/engine/coverage"
	"testing"
)

func TestUserSameSnapshotNewWaveResultEngine(t *testing.T) {
	for _, lateFirst := range []bool{true, false} {
		name := "new-first"
		if lateFirst {
			name = "old-first"
		}
		t.Run(name, func(t *testing.T) {
			e := verificationEngine(t)
			s, _ := e.Load()
			original := currentWave(s.State.Regular).ID
			old := currentExecution(t, e, "blackbox-execution")
			claimAndAttach(t, e, old)
			regularChoice(t, e, RegularRequest{Action: "qa-revise", Target: "blackbox", Meaning: "stable-execution", Reason: "revise documented command path while application code is unchanged"}, "confirm")
			dx := currentExecution(t, e, "blackbox-design")
			claimAndAttach(t, e, dx)
			s, _ = e.Load()
			design := testDesign(s.State.Regular, "BLACKBOX")
			design.Manifest.Cases[0].Steps = "invoke through the revised documented command path"
			dr := preparationResult(dx)
			dr.Design = &design
			if a := submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &dr}); a.Status != "ACCEPTED" {
				t.Fatal("revised design rejected", a)
			}
			sourceUnavailable(t, e, dx)
			rx := currentExecution(t, e, "blackbox-review")
			claimAndAttach(t, e, rx)
			rr := preparationResult(rx)
			rr.Review = &coverage.ReviewResult{Binding: design.Manifest.Binding, Scope: coverage.ScopeAffected, CaseDecisions: []coverage.ReviewDecision{{ID: "BLACKBOX:C1", Status: coverage.StatusPass}}}
			if a := submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &rr}); a.Status != "ACCEPTED" {
				t.Fatal("revised review rejected", a)
			}
			sourceUnavailable(t, e, rx)
			// Protocol-level stress: retain even the candidate identity, in addition to
			// unchanged application code. Git side effects are explicit existing fixtures.
			gitReceipt(t, e, "Q1")
			s, _ = e.Load()
			w := currentWave(s.State.Regular)
			if w.ID == original || w.Candidate.Candidate.Identity != "Q1" || s.State.Regular.Verification.Waves[0].Invalidated == "" {
				t.Fatal("two-wave precondition missing")
			}
			var current RegularExecution
			for _, x := range s.State.RegularExecutions {
				if x.Claim.Task.Step == "blackbox-execution" && currentVerificationClaim(s.State.Regular, x.Claim) && !x.Finished {
					current = x
				}
			}
			if current.Claim.ActionID == "" || current.Claim.ActionID == old.Claim.ActionID || current.Claim.Task == old.Claim.Task {
				t.Fatal("no distinct current-wave task")
			}
			claimAndAttach(t, e, current)
			oldResult := verificationResult(old)
			oldResult.Cases = []CaseExecution{executedCase("BLACKBOX:C1", "PASS")}
			newResult := verificationResult(current)
			newResult.Cases = []CaseExecution{executedCase("BLACKBOX:C1", "PASS")}
			submitOld := func() {
				a := submitRegular(t, e, RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &oldResult})
				if a.Status != "REJECTED_RESULT" {
					t.Fatalf("old result not rejected: %+v", a)
				}
				sourceUnavailable(t, e, old)
			}
			if lateFirst {
				submitOld()
			}
			recordQAOperations(t, e, current, newResult.Cases)
			a := submitRegular(t, e, RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &newResult})
			if a.Status != "ACCEPTED" {
				t.Fatalf("current result not accepted: %+v", a)
			}
			sourceUnavailable(t, e, current)
			if !lateFirst {
				submitOld()
			}
			s, _ = e.Load()
			w = currentWave(s.State.Regular)
			if !s.State.RegularExecutions[current.Claim.ActionID].Finished || w.Results["blackbox"].ActionID != current.Claim.ActionID || w.Results["blackbox"].Status != "PASS" {
				t.Fatal("current result not registered")
			}
			if len(s.State.Regular.Verification.Waves[0].Results) != 0 || s.State.Regular.Verification.Waves[0].Invalidated == "" {
				t.Fatal("old history was overwritten")
			}
			for _, x := range s.State.RegularExecutions {
				if x.Claim.Task.Step == "implementation-quality-gate" && currentVerificationClaim(s.State.Regular, x.Claim) && !x.Finished {
					claimAndAttach(t, e, x)
					r := verificationResult(x)
					submitRegular(t, e, RegularEvent{Operation: "VERIFICATION_RESULT", VerificationResult: &r})
					sourceUnavailable(t, e, x)
				}
			}
			s, _ = e.Load()
			w = currentWave(s.State.Regular)
			if !w.Complete {
				t.Fatal("current wave did not complete after all results settled")
			}
			t.Logf("same candidate Q1; waves=%d; old invalidated=%q; current QA=%s; taskFinished=%v; waveComplete=%v; old late quarantined=%v", len(s.State.Regular.Verification.Waves), s.State.Regular.Verification.Waves[0].Invalidated, w.Results["blackbox"].Status, s.State.RegularExecutions[current.Claim.ActionID].Finished, w.Complete, s.State.RegularExecutions[old.Claim.ActionID].VerificationResult.Quarantined)
		})
	}
}
