package validate

import (
	"strings"
	"testing"
)

// The former direct-approval path must not bypass independent review of changed
// case meaning. Optional dispositions now use the explicit reasoned operation.
func TestPerSuggestionCannotApproveChangedCases(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := beginQA(t, root, pkg, "per-suggestion-bypass")
	review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "ps-review")
	state, err := RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", []FindingInput{{Severity: "P2", Message: "optional boundary improvement"}})
	if err != nil {
		t.Fatal(err)
	}
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	before := stateBytes(t, root, state.RunID)
	_, err = RecordQADesign(root, pkg, state.RunID, dispatch, []QACaseInput{{CaseID: "CASE-002", Mode: "blackbox", Description: "changed case", Procedure: "changed command", Oracle: "new oracle"}}, "", QADesignRecordOptions{PerSuggestion: true, SuggestionIDs: []string{state.QACaseSets[""].Suggestions[0].ID}})
	if err == nil || !strings.Contains(err.Error(), "qa-suggestion") || stateBytes(t, root, state.RunID) != before {
		t.Fatal("direct approval bypass survived")
	}
}

// TestPerSuggestionRequiresRecordedP2Finding asserts a clean PASS freezes the
// case-set immediately, so there is no suggestion-absorption round to enter.
func TestPerSuggestionRequiresRecordedP2Finding(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := beginQA(t, root, pkg, "per-suggestion-no-p2")
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "ps-reviewer-clean")
	var err error
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = PrepareAction(root, pkg, state.RunID, "qa-design", "", false, "")
	if err == nil || !strings.Contains(err.Error(), "case-set is FROZEN") {
		t.Fatalf("absorption after a clean frozen review was accepted: %v", err)
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
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", []FindingInput{fixtureBlockingFinding("P1", "missing failure-path coverage", "requirements.md:1")})
	if err != nil {
		t.Fatal(err)
	}
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	_, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "absorb on FAIL", Procedure: "run the public command", Oracle: "observable success"}}, "", QADesignRecordOptions{PerSuggestion: true})
	if err == nil || !strings.Contains(err.Error(), "qa-suggestion") {
		t.Fatalf("absorption on a FAILed review was accepted: %v", err)
	}
	_, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "replace absorb", Procedure: "run the public command", Oracle: "observable success"}}, "", QADesignRecordOptions{PerSuggestion: true, ReplaceAll: true})
	if err == nil || !strings.Contains(err.Error(), "qa-suggestion") {
		t.Fatalf("absorption with --replace-all was accepted: %v", err)
	}
}
