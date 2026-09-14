package facade

import (
	"encoding/json"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
)

func TestQARevisionReasonReachesCurrentDesignAndResume(t *testing.T) {
	for _, mode := range []string{"blackbox", "whitebox"} {
		t.Run(mode, func(t *testing.T) {
			f, _, _, _ := retainedSourceFixture(t, mode)
			const oldReason = "revise documented command path"
			const reason = "fixture revision: compare the violet output\nthen inspect the copper result"
			before, err := f.Show()
			if err != nil {
				t.Fatal(err)
			}
			if got := before.Regular.Preparation.QA[mode].DesignGap; got != oldReason {
				t.Fatalf("first revision reason lost: %q", got)
			}
			facadeChoice(t, f, protocol.RegularRequest{Action: "qa-revise", Target: mode, Meaning: "stable-execution", Reason: reason})
			f, err = Open(f.root, f.runID)
			if err != nil {
				t.Fatal(err)
			}
			r, err := f.Show()
			if err != nil {
				t.Fatal(err)
			}
			x := facadeExecution(t, f, mode+"-design")
			// Use arbitrary instruction bodies; only production section identifiers matter.
			r.Regular.ActionInstructions = map[string]string{
				"actions/development-worker.md": "# 通用执行约定\nFixture common.\n# 续做约定\nFixture continuation.\n# 工具说明\nFixture tools.",
				"actions/qa-design.md":          "# QA 设计\nFixture design.\n# 黑盒设计\nFixture black.\n# 白盒设计\nFixture white.\n# 白盒覆盖判断\nFixture coverage.",
			}
			for _, operation := range []string{"execute", "resume"} {
				claim := x.Claim
				claim.Operation = operation
				if operation == "resume" {
					claim.Identity = "fixture-existing-session"
				}
				plan, err := f.prepareNativeActionInput(r, claim, x, t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				if err := plan.preflight(claim, codexRuntimeAdapter{}); err != nil {
					t.Fatal(err)
				}
				want, _ := json.Marshal(reason)
				if !strings.Contains(plan.prompt, string(want)) || strings.Contains(plan.prompt, oldReason) {
					t.Fatalf("%s revision reason: current present=%v, old present=%v", operation, strings.Contains(plan.prompt, string(want)), strings.Contains(plan.prompt, oldReason))
				}
			}
		})
	}
}

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
	for _, s := range []string{"previousResultRejection", "FAIL needs a complete QA review", "public interface missing"} {
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
