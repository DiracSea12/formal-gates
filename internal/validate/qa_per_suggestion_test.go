package validate

import (
	"strings"
	"testing"
)

// TestPerSuggestionAbsorbApprovesCasesWithoutNewReview drives the full flow:
// a qa-review round PASSes while recording a P2 set-level suggestion, then a
// qa-design record with PerSuggestion absorbs it — the added and modified cases
// are recorded approved (SUGGESTION_APPLIED provenance) without any new
// qa-review dispatch, and the mode's review result stays PASS with the P2
// finding preserved as absorption provenance.
func TestPerSuggestionAbsorbApprovesCasesWithoutNewReview(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := beginQA(t, root, pkg, "per-suggestion-absorb")
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "ps-reviewer")
	var err error
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", []FindingInput{{Severity: "P2", Message: "suggest an additional boundary case"}})
	if err != nil {
		t.Fatal(err)
	}
	if review := state.qaReview(""); review.Status != "PASS" {
		t.Fatalf("review result after P2-only round=%#v", review)
	}
	// 吸收轮：新增一个用例 + 实质修改既有黑盒用例 + 原样重交既有白盒用例（无实质吸收，
	// 不打溯源）。
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{
		{CaseID: "CASE-002", Mode: "blackbox", Description: "public workflow succeeds at the boundary", Procedure: "run the documented public CLI against a built snapshot", Oracle: "observable output succeeds"},
		{Mode: "blackbox", Description: "boundary case absorbed from the P2 suggestion", Procedure: "run the public command at the boundary", Oracle: "observable boundary behavior"},
		{CaseID: "CASE-001", Mode: "whitebox", Description: "direct rules pass", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"},
	}, "", QADesignRecordOptions{PerSuggestion: true})
	if err != nil {
		t.Fatalf("per-suggestion absorption was rejected: %v", err)
	}
	for _, testCase := range state.qaModeCases("") {
		if testCase.ReviewStatus != "PASS" {
			t.Fatalf("case %s is not approved after absorption: %#v", testCase.ID, testCase)
		}
	}
	byID := map[string]QACase{}
	for _, testCase := range state.qaModeCases("") {
		byID[testCase.ID] = testCase
	}
	if byID["CASE-002"].ApprovedSource != "SUGGESTION_APPLIED" {
		t.Fatalf("modified case provenance=%q want SUGGESTION_APPLIED", byID["CASE-002"].ApprovedSource)
	}
	if byID["CASE-003"].ApprovedSource != "SUGGESTION_APPLIED" {
		t.Fatalf("absorbed case provenance=%q want SUGGESTION_APPLIED", byID["CASE-003"].ApprovedSource)
	}
	// 原样重交（无实质吸收）不打溯源，状态保持 review 批准。
	if byID["CASE-001"].ApprovedSource != "" || byID["CASE-001"].ReviewStatus != "PASS" {
		t.Fatalf("unchanged resubmission was stamped as absorbed: %#v", byID["CASE-001"])
	}
	// 吸收轮不重置 review：P2 建议留作溯源，权威结果保持 PASS（不派新 qa-review）。
	if review := state.qaReview(""); review.Status != "PASS" || len(review.Findings) == 0 {
		t.Fatalf("absorption reset the review result: %#v", review)
	}
	// 派发提示词里的用例展示带溯源（读文档即知为何不经 qa-review 即已批准）。
	if rendered := formatQACase(byID["CASE-003"], true); !strings.Contains(rendered, "review status: PASS (SUGGESTION_APPLIED)") {
		t.Fatalf("rendered absorbed case lacks provenance: %s", rendered)
	}
}

// TestPerSuggestionRequiresRecordedP2Finding asserts the absorption flag is
// rejected when the mode's latest qa-review PASSed without any recorded P2
// set-level finding (nothing to absorb), and when the review FAILed.
func TestPerSuggestionRequiresRecordedP2Finding(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := beginQA(t, root, pkg, "per-suggestion-no-p2")
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "ps-reviewer-clean")
	var err error
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	_, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "unbacked absorb", Procedure: "run the public command", Oracle: "observable success"}}, "", QADesignRecordOptions{PerSuggestion: true})
	if err == nil || !strings.Contains(err.Error(), "requires at least one recorded P2 set-level finding") {
		t.Fatalf("absorption without a recorded P2 finding was accepted: %v", err)
	}
}

// TestPerSuggestionRejectedOnFailedReviewOrReplaceAll asserts --per-suggestion
// is rejected when the latest review result is FAIL (P2 absorption only rides a
// PASS review) and when combined with --replace-all (whole-set replacement
// discards approved cases and is not absorption).
func TestPerSuggestionRejectedOnFailedReviewOrReplaceAll(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := beginQA(t, root, pkg, "per-suggestion-fail")
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "ps-reviewer-fail")
	var err error
	// 各用例 PASS + 集合级 P1 覆盖遗漏 → 审查动作 FAIL。
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", []FindingInput{{Severity: "P1", Message: "missing failure-path coverage"}})
	if err != nil {
		t.Fatal(err)
	}
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	_, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "absorb on FAIL", Procedure: "run the public command", Oracle: "observable success"}}, "", QADesignRecordOptions{PerSuggestion: true})
	if err == nil || !strings.Contains(err.Error(), "latest qa-review result to be PASS") {
		t.Fatalf("absorption on a FAILed review was accepted: %v", err)
	}
	_, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "replace absorb", Procedure: "run the public command", Oracle: "observable success"}}, "", QADesignRecordOptions{PerSuggestion: true, ReplaceAll: true})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined with --replace-all") {
		t.Fatalf("absorption with --replace-all was accepted: %v", err)
	}
}
