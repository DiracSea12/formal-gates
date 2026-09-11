package protocol

import "testing"

func TestBlackboxDesignGapIsSemanticFailureAndDoesNotFreeze(t *testing.T) {
	e := preparationEngine(t, "blackbox", "whitebox")
	gitReceipt(t, e, "git-base")
	dev := currentExecution(t, e, "development-worker")
	claimAndAttach(t, e, dev)
	for n := 1; n <= 3; n++ {
		x := currentExecution(t, e, "blackbox-design")
		claimAndAttach(t, e, x)
		s, _ := e.Load()
		d := testDesign(s.State.Regular, "BLACKBOX")
		d.Manifest.Cases = nil
		d.Manifest.Edges = nil
		d.CoverageJudgment = "Public command arguments remain unspecified for this acceptance behavior"
		v := PreparationResult{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, Identity: "native-" + x.Claim.ActionID, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, Status: "FAIL", Message: "Need the public command contract; not implementation source", Design: &d}
		submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &v})
		sourceUnavailable(t, e, x)
		s, _ = e.Load()
		q := s.State.Regular.Preparation.QA["blackbox"]
		if len(s.State.Regular.PreparationErrors) != 0 || q.ConsecutiveFailures != n || q.Design != nil || q.Frozen || q.DesignGap == "" {
			t.Fatalf("gap misclassified or frozen: %+v", q)
		}
		if s.State.RegularExecutions[dev.Claim.ActionID].Finished {
			t.Fatal("independent developer was stopped")
		}
		if n < 3 {
			if s.State.Regular.Ask != nil || q.NeedsRevision {
				t.Fatal("bounded semantic redesign did not advance")
			}
			next := currentExecution(t, e, "blackbox-design")
			if next.Claim.Task == x.Claim.Task {
				t.Fatal("failure consumed a runtime retry instead of design revision")
			}
		} else if s.State.Regular.Ask == nil || s.State.Regular.Ask.Kind != "qa-recovery" || s.State.Regular.Ask.QA.ConsecutiveFailures != 3 || s.State.Regular.Ask.QA.RetryExhausted {
			t.Fatalf("missing semantic recovery: %+v", s.State.Regular.Ask)
		}
	}
}
