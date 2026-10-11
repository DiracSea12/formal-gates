package facade

import (
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"testing"
)

func TestReviewRationaleIsCarriedFromOriginalApproval(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "reopen"}[reopen], func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			id, in := d.action("engine.qa.design.v3")
			m := simpleManifest(in)
			m.SharedSteps = []coverage.SharedStep{{ID: "setup", Procedure: "Create a clean command workspace"}}
			m.Cases[0].SharedSteps = []string{"setup"}
			m.Cases[1].SharedSteps = []string{"setup"}
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
			id, in = d.action("engine.qa.review.rationale.v2")
			r := approvedReview(m)
			r.SetStatus = coverage.StatusFail
			r.CaseDecisions[0].Status = coverage.StatusFail
			r.PointDecisions[0].Status = coverage.StatusFail
			r.SourceDecisions[0].Status = coverage.StatusFail
			reason := "公共入口与参数已核实\n前置和环境足够；步骤顺序可执行；证据支持oracle；边界只验当前命令。  "
			shared := "Create workspace before either case; both reference this exact setup.\n  "
			r.CaseDecisions[1].Reason = reason
			r.SharedStepDecisions = []coverage.ReviewDecision{{ID: "setup", Status: coverage.StatusPass, Reason: shared}}
			first := id
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Review: &r})
			if reopen {
				d.reopen()
			}
			id, in = d.action("engine.qa.design.v3")
			m.Cases[0].Oracle = "exits zero and reports accepted input"
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
			id, in = d.action("engine.qa.review.rationale.v2")
			if len(in.InheritableReviews) != 1 || in.InheritableReviews[0].Reason != reason || in.InheritableReviews[0].ID != "invalid" {
				t.Fatalf("input lost exact original approval: %+v", in.InheritableReviews)
			}
			next := approvedReview(m)
			next.Scope = coverage.ScopeAffected
			next.CaseDecisions[1] = in.InheritableReviews[0]
			next.CaseDecisions[1].Inherited = true
			next.SharedStepDecisions = []coverage.ReviewDecision{{ID: "setup", Status: coverage.StatusPass, Reason: shared}}
			next.CaseDecisions[1].Reason += " changed"
			d.receipt(id)
			a := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "changed inherited reason", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &next}}})
			if a.Status != "REJECTED_INVALID_RESULT" {
				t.Fatal("rewritten inherited rationale was admitted")
			}
			retry, ri := d.action("engine.qa.review.rationale.v2")
			if ri.Binding.Task != in.Binding.Task {
				t.Fatal("invalid rationale escaped original retry budget")
			}
			next.CaseDecisions[1] = ri.InheritableReviews[0]
			next.CaseDecisions[1].Inherited = true
			d.business(retry, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &next})
			d.reopen()
			s, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			raw := s.State.Results[first].Validation.Review
			if raw.CaseDecisions[1].Reason != reason || raw.SharedStepDecisions[0].Reason != shared || len(s.State.Validation.Sets[coverage.ReviewBlackbox].ReviewHistory) != 2 {
				t.Fatal("raw rationale changed across retry/reopen")
			}
		})
	}
}
