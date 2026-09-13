package protocol

import "testing"

func TestBlackboxDesignerCannotSubmitCoverageVerdict(t *testing.T) {
	for _, status := range []string{"PASS", "FAIL"} {
		t.Run(status, func(t *testing.T) {
			e := preparationEngine(t, "blackbox", "whitebox")
			gitReceipt(t, e, "git-base")
			dev := currentExecution(t, e, "development-worker")
			claimAndAttach(t, e, dev)
			x := currentExecution(t, e, "blackbox-design")
			claimAndAttach(t, e, x)
			s, _ := e.Load()
			d := testDesign(s.State.Regular, "BLACKBOX")
			v := preparationResult(x)
			v.Status = status
			v.Design = &d
			v.Message = "author coverage opinion"
			got := submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &v})
			if got.Status != "REJECTED_RESULT" {
				t.Fatal(got)
			}
			s, _ = e.Load()
			q := s.State.Regular.Preparation.QA["blackbox"]
			if q.Design != nil || q.Frozen || q.ConsecutiveFailures != 0 {
				t.Fatal("author verdict became review outcome")
			}
			if s.State.RegularExecutions[dev.Claim.ActionID].Finished {
				t.Fatal("independent development stopped")
			}
		})
	}
}
