package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
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
			assertQAReasonInput(t, f, mode+"-design", reason, oldReason)
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

func assertQAReasonInput(t *testing.T, f *Facade, step, reason, oldReason string) {
	t.Helper()
	r, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	x := facadeExecution(t, f, step)
	// Use arbitrary instruction bodies; only production section identifiers matter.
	r.Regular.ActionInstructions = map[string]string{
		"actions/development-worker.md": "# 通用执行约定\nFixture common.\n# 续做约定\nFixture continuation.\n# 工具说明\nFixture tools.",
		"actions/qa-review.md":          "# QA 审查\nFixture review.\n# 黑盒审查\nFixture black review.\n# 白盒改动与最终候选\nFixture white review.",
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
		if !strings.Contains(plan.prompt, string(want)) || (oldReason != "" && strings.Contains(plan.prompt, oldReason)) {
			t.Fatalf("%s revision reason: current present=%v, old present=%v", operation, strings.Contains(plan.prompt, string(want)), (oldReason != "" && strings.Contains(plan.prompt, oldReason)))
		}
	}
}

func TestQARecoveryReasonReachesCurrentTaskInput(t *testing.T) {
	for _, mode := range []string{"blackbox", "whitebox"} {
		for _, tc := range []struct{ kind, choice string }{
			{"semantic", "redesign"}, {"semantic", "fresh-review"},
			{"design", "retry"}, {"design", "redesign"},
			{"review", "retry"}, {"review", "redesign"}, {"review", "fresh-review"},
		} {
			t.Run(mode+"/"+tc.kind+"/"+tc.choice, func(t *testing.T) {
				f, _, sources, _ := retainedSourceFixture(t, mode)
				const oldReason = "fixture earlier failure: amber comparison"
				const reason = "fixture disposition: inspect violet output\nthen compare copper result"
				var failed protocol.RegularExecution
				for n := 0; n < 3; n++ {
					r, err := f.Show()
					if err != nil {
						t.Fatal(err)
					}
					if tc.kind != "design" && r.Regular.Preparation.QA[mode].Design == nil {
						design := facadeDesign(r, mode)
						design.Manifest.Cases[0].Oracle = fmt.Sprintf("fixture oracle %d", n)
						result := protocol.PreparationResult{Design: &design}
						if mode == "whitebox" {
							result.Reuse = []protocol.TestReuse{{CaseID: "WHITEBOX:C1", TestRef: "main_test.go::TestHello", CoverageJudgment: "fixture coverage", Procedure: "fixture procedure", Observation: "fixture observation", Comparison: "fixture comparison", Result: "EXECUTED_PASS"}}
						}
						designAction := facadeExecution(t, f, mode+"-design")
						job := f.codexJob(designAction.Claim.ActionID)
						if err := os.MkdirAll(job, 0700); err != nil {
							t.Fatal(err)
						}
						if err := writeJSONAtomic(filepath.Join(job, "sources.json"), sources); err != nil {
							t.Fatal(err)
						}
						facadeResult(t, f, designAction, result)
						r, err = f.Drive()
						if err != nil {
							t.Fatal(err)
						}
					}
					step := mode + "-review"
					if tc.kind == "design" {
						step = mode + "-design"
					}
					failed = facadeExecution(t, f, step)
					c := failed.Claim
					preparationSubmit(t, f, protocol.RegularEvent{Operation: "CLAIM", Claim: &c})
					src := protocol.ExecutionSource{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider, Identity: "fixture-recovery", Status: "RUNNING"}
					preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &src})
					result := protocol.PreparationResult{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider, Identity: src.Identity, RequirementRevision: c.RequirementRevision, Candidate: c.Candidate, Status: "RUNTIME_ERROR", Message: oldReason}
					if tc.kind == "semantic" {
						result.Status = "FAIL"
						result.Review = facadeReview(r, mode)
						result.Review.Scope = r.Regular.Preparation.QA[mode].ReviewScope
						result.Review.SetStatus = coverage.StatusFail
						result.Review.CaseDecisions[0].Status = coverage.StatusFail
					}
					accepted, _, err := f.submitAdapterRegular("fixture-recovery-result", protocol.RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result})
					if err != nil {
						t.Fatal(err)
					}
					wantStatus := "ACCEPTED"
					if tc.kind != "semantic" {
						wantStatus = "REJECTED_RESULT"
					}
					if accepted.Status != wantStatus {
						t.Fatalf("result status = %s, want %s", accepted.Status, wantStatus)
					}
					src.Status = "UNAVAILABLE"
					src.Reason = "fixture source settled"
					preparationSubmit(t, f, protocol.RegularEvent{Operation: "SOURCE", Source: &src})
					if _, err := f.Drive(); err != nil {
						t.Fatal(err)
					}
				}
				r, err := f.Show()
				if err != nil {
					t.Fatal(err)
				}
				a := r.Regular.Ask
				if a == nil || a.Kind != "qa-recovery" || a.QA.RetryExhausted != (tc.kind != "semantic") {
					t.Fatalf("unexpected recovery: %+v", a)
				}
				preparationSubmit(t, f, protocol.RegularEvent{Operation: "DECIDE", Decision: &protocol.RegularDecision{RequestID: a.ID, Token: a.Token, Digest: a.Digest, Choice: tc.choice, Reason: reason}})
				f, err = Open(f.root, f.runID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.Drive(); err != nil {
					t.Fatal(err)
				}
				step := string(failed.Claim.Task.Step)
				if tc.kind == "semantic" && tc.choice == "redesign" {
					step = mode + "-design"
				}
				next := facadeExecution(t, f, step)
				if tc.kind != "semantic" && (next.Claim.Task != failed.Claim.Task || next.Number != failed.Number+1) {
					t.Fatal("recovery changed exhausted TaskKey or attempt sequence")
				}
				// Historical failure receipts remain available alongside the current disposition.
				assertQAReasonInput(t, f, step, reason, "")
			})
		}
	}
}
