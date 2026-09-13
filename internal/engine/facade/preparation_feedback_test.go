package facade

import (
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
)

func TestPreparationRetryFeedbackIsBoundAndRoleSpecific(t *testing.T) {
	f := facadePreparation(t, []string{"blackbox"})
	r, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	x := facadeExecution(t, f, "blackbox-design")
	dev := facadeExecution(t, f, "development-worker")
	failed := protocol.PreparationResult{ActionID: x.Claim.ActionID, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, Status: "FAIL", Message: "public interface missing"}
	r.Regular.PreparationErrors = []protocol.PreparationError{{Result: failed, Reason: "FAIL needs a complete QA review and concrete gap"}}
	other := failed
	other.ActionID = dev.Claim.ActionID
	other.Message = "private developer output"
	r.Regular.PreparationErrors = append(r.Regular.PreparationErrors, protocol.PreparationError{Result: other, Reason: "private developer error"})
	prompt, _, err := preparationPrompt(r, x.Claim)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"previousResultRejection", "FAIL needs a complete QA review", "public interface missing", "用例充分性、需求/AC 映射与全套覆盖由独立 QA 审查判断"} {
		if !strings.Contains(prompt, s) {
			t.Fatalf("missing feedback %q", s)
		}
	}
	if strings.Contains(prompt, "private developer output") || strings.Contains(prompt, "private developer error") {
		t.Fatal("blackbox received developer evidence")
	}
	r.Regular.PreparationErrors[0].Result.Candidate = "stale-candidate"
	prompt, _, err = preparationPrompt(r, x.Claim)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, `"previousResultRejection":`) {
		t.Fatal("stale failure crossed candidate binding")
	}
}
