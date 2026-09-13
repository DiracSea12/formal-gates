package protocol

import (
	"strings"
	"testing"
)

func TestWhiteboxTestFilesResultContract(t *testing.T) {
	for _, tc := range []struct {
		name     string
		files    []string
		accepted bool
	}{
		{"missing", nil, false},
		{"absolute", []string{"/probe"}, false},
		{"outside", []string{"../probe"}, false},
		{"duplicate", []string{"probe", "probe"}, false},
		{"explicit-empty", []string{}, true},
		{"exact-file", []string{"probe"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := preparationEngine(t, "whitebox")
			gitReceipt(t, e, "base")
			completeBatch(t, e, "B1")
			gitReceipt(t, e, "D1")
			completeBatch(t, e, "B2")
			gitReceipt(t, e, "D2")
			gitReceipt(t, e, "workspace")
			x := currentExecution(t, e, "whitebox-design")
			claimAndAttach(t, e, x)
			recordCoverage(t, e, x)
			loaded, _ := e.Load()
			design := testDesign(loaded.State.Regular, "WHITEBOX")
			design.TestFiles = tc.files
			// A symbolic reference does not encode any path.
			design.Manifest.Cases[0].TestRef = "runner registered assertion"
			result := preparationResult(x)
			result.Design = &design
			result.Reuse = []TestReuse{{CaseID: "WHITEBOX:C1", TestRef: design.Manifest.Cases[0].TestRef, CoverageJudgment: "exact command output", Procedure: "execute assertion", Observation: "PASS", Comparison: "exact result", Result: "EXECUTED_PASS"}}
			got := submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result})
			want := "REJECTED_RESULT"
			if tc.accepted {
				want = "ACCEPTED"
			}
			if got.Status != want {
				t.Fatalf("status=%s, want %s", got.Status, want)
			}
			if tc.accepted {
				loaded, _ = e.Load()
				if loaded.State.Regular.Preparation.QA["whitebox"].Design.TestFiles == nil {
					t.Fatal("explicit testFiles lost on reload")
				}
			}
		})
	}
}

func TestUnchangedDeclaredTestsCannotWaiveIndependentReview(t *testing.T) {
	e := preparationEngine(t, "whitebox")
	gitReceipt(t, e, "base")
	completeBatch(t, e, "B1")
	gitReceipt(t, e, "D1")
	completeBatch(t, e, "B2")
	gitReceipt(t, e, "D2")
	gitReceipt(t, e, "workspace")
	completeQA(t, e, "whitebox", "design")
	gitReceipt(t, e, "D2") // no edits in the whitebox workspace
	loaded, _ := e.Load()
	p := loaded.State.Regular.Preparation
	q := p.QA["whitebox"]
	x := currentExecution(t, e, "whitebox-review")
	ask := &RegularAsk{QA: &QARecoveryBinding{Mode: "whitebox", Task: x.Claim.Task, RequirementRevision: loaded.State.Regular.Requirement.Revision, Candidate: "D2", CaseSetDigest: digestOfCanonical(q.Design)}}
	for _, choice := range []string{"skip", "waiver"} {
		if err := decideQARecovery(loaded.State, ask, RegularDecision{Choice: choice, Reason: "fixture request"}, loaded.Revision); err == nil || !strings.Contains(err.Error(), "test-only review") {
			t.Fatalf("%s bypassed file review: %v", choice, err)
		}
	}
	if WhiteboxPromotionReady(p) {
		t.Fatal("zero diff approved unreviewed testFiles")
	}
	completeQA(t, e, "whitebox", "review")
	loaded, _ = e.Load()
	if !WhiteboxPromotionReady(loaded.State.Regular.Preparation) {
		t.Fatal("existing independent review did not approve zero-change tests")
	}
	q = loaded.State.Regular.Preparation.QA["whitebox"]
	reviseQA(q)
	if WhiteboxPromotionReady(loaded.State.Regular.Preparation) {
		t.Fatal("new design inherited file approval")
	}
}
