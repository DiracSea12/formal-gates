package review

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestFindingDecisionMatrixProjectsSeverityAndBlocking(t *testing.T) {
	cases := []struct {
		name         string
		severity     string
		decision     string
		newSeverity  string
		rationale    string
		wantStatus   string
		wantBlocking int
		wantAdvisory int
		wantErr      string
	}{
		{name: "confirm keeps P1 blocking", severity: "P1", decision: "CONFIRM", wantStatus: "FAIL", wantBlocking: 1},
		{name: "confirm keeps P2 advisory", severity: "P2", decision: "CONFIRM", wantStatus: "PASS", wantAdvisory: 1},
		{name: "dismiss removes the finding", severity: "P1", decision: "DISMISS", wantStatus: "PASS"},
		{name: "invalidate finding removes it", severity: "P2", decision: "INVALIDATE_FINDING", wantStatus: "PASS"},
		{name: "re-severity P1 down to P2 turns advisory", severity: "P1", decision: "RESEVERITIZE", newSeverity: "P2", wantStatus: "PASS", wantAdvisory: 1},
		{name: "re-severity P2 up to P1 turns blocking", severity: "P2", decision: "RESEVERITIZE", newSeverity: "P1", rationale: "the defect blocks the documented entry path", wantStatus: "FAIL", wantBlocking: 1},
		{name: "re-severity P3 stays advisory", severity: "P3", decision: "RESEVERITIZE", newSeverity: "P3", wantStatus: "PASS", wantAdvisory: 1},
		{name: "re-severity to P0 without rationale is rejected", severity: "P2", decision: "RESEVERITIZE", newSeverity: "P0", wantErr: "rationale"},
		{name: "re-severity to P0 with a placeholder rationale is rejected", severity: "P2", decision: "RESEVERITIZE", newSeverity: "P0", rationale: "违反需求", wantErr: "rationale"},
		{name: "confirm cannot carry a severity", severity: "P1", decision: "CONFIRM", newSeverity: "P2", wantErr: "only for RESEVERITIZE"},
		{name: "dismiss cannot carry a rationale", severity: "P1", decision: "DISMISS", rationale: "extra", wantErr: "only for RESEVERITIZE"},
		{name: "re-severity requires a severity", severity: "P2", decision: "RESEVERITIZE", wantErr: "requires a severity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runID := "run-matrix"
			set := NewSet()
			set, candidate := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-matrix", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: tc.severity, Message: "matrix finding"}}})
			input := CorrectionInput{
				Action: ProductReview, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision,
				FindingID: candidate.Findings[0].ID, Decision: tc.decision, Severity: tc.newSeverity, SeverityRationale: tc.rationale,
				Reason: "user disposition", Source: "user", UserAuthorized: true, ExpectedEffectiveRevision: 1,
			}
			next, err := ApplyCorrection(set, runID, "rev-1", input)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected an error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			series, _ := next.Series(ProductReview)
			if series.Effective.Status != tc.wantStatus || len(series.Effective.BlockingFindingIDs) != tc.wantBlocking || len(series.Effective.AdvisoryFindingIDs) != tc.wantAdvisory {
				t.Fatalf("projection = %s blocking=%v advisory=%v", series.Effective.Status, series.Effective.BlockingFindingIDs, series.Effective.AdvisoryFindingIDs)
			}
			if series.Completed != 1 || len(series.RawCandidates) != 1 {
				t.Fatalf("disposition changed round accounting: %#v", series)
			}
			if err := Validate(runID, next); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCorrectionChainMustSupersedeTheCurrentDecision(t *testing.T) {
	runID := "run-chain"
	set := NewSet()
	set, candidate := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-chain", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "chain finding"}}})
	findingID := candidate.Findings[0].ID
	binding := CorrectionInput{
		Action: ProductReview, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision,
		FindingID: findingID, Reason: "user disposition", Source: "user", UserAuthorized: true,
	}
	first := binding
	first.Decision = "CONFIRM"
	first.Supersedes = "review-adjudication-doesnotexist"
	first.ExpectedEffectiveRevision = 1
	if _, err := ApplyCorrection(set, runID, "rev-1", first); err == nil || !strings.Contains(err.Error(), "no prior adjudication") {
		t.Fatalf("first disposition with supersedes was accepted: %v", err)
	}
	first.Supersedes = ""
	set = mustApply(t, set, runID, "rev-1", first)
	series, _ := set.Series(ProductReview)
	adjudication := series.Adjudications[0]
	if adjudication.OldValue != "UNADJUDICATED:P1" || adjudication.NewValue != "CONFIRM:P1" || !strings.HasPrefix(adjudication.ID, "review-adjudication-") {
		t.Fatalf("first adjudication values are wrong: %#v", adjudication)
	}
	wrongHop := binding
	wrongHop.Decision = "RESEVERITIZE"
	wrongHop.Severity = "P2"
	wrongHop.Supersedes = "review-correction-doesnotexist"
	wrongHop.ExpectedEffectiveRevision = seriesRevision(set, ProductReview)
	if _, err := ApplyCorrection(set, runID, "rev-1", wrongHop); err == nil || !strings.Contains(err.Error(), "must name the current decision") {
		t.Fatalf("stale supersedes was accepted: %v", err)
	}
	second := binding
	second.Decision = "RESEVERITIZE"
	second.Severity = "P2"
	second.Supersedes = adjudication.ID
	set = mustApply(t, set, runID, "rev-1", second)
	series, _ = set.Series(ProductReview)
	receipt := series.Corrections[0]
	if receipt.Supersedes != adjudication.ID || receipt.OldValue != "CONFIRM:P1" || receipt.NewValue != "RESEVERITIZE:P2" || !strings.HasPrefix(receipt.ID, "review-correction-") {
		t.Fatalf("superseding receipt values are wrong: %#v", receipt)
	}
	if series.Effective.Status != "PASS" {
		t.Fatalf("downgrade to P2 must turn the result advisory: %#v", series.Effective)
	}
	lateHop := binding
	lateHop.Decision = "DISMISS"
	lateHop.Supersedes = adjudication.ID
	lateHop.ExpectedEffectiveRevision = seriesRevision(set, ProductReview)
	if _, err := ApplyCorrection(set, runID, "rev-1", lateHop); err == nil || !strings.Contains(err.Error(), "must name the current decision") {
		t.Fatalf("superseding a non-current decision was accepted: %v", err)
	}
	third := binding
	third.Decision = "DISMISS"
	third.Supersedes = receipt.ID
	set = mustApply(t, set, runID, "rev-1", third)
	series, _ = set.Series(ProductReview)
	if series.Corrections[1].OldValue != "RESEVERITIZE:P2" || series.Corrections[1].NewValue != "DISMISS:P2" {
		t.Fatalf("dismiss over a re-severity must chain the effective values: %#v", series.Corrections[1])
	}
	if LatestDecisionID(series, candidate.ResultDigest, findingID) != series.Corrections[1].ID {
		t.Fatal("latest decision id does not follow the chain head")
	}
	if series.Effective.Status != "PASS" || len(series.Effective.AdvisoryFindingIDs) != 0 {
		t.Fatalf("dismissed finding must leave no projection: %#v", series.Effective)
	}
	if series.Completed != 1 {
		t.Fatalf("corrections must not consume rounds: %#v", series)
	}
	if err := Validate(runID, set); err != nil {
		t.Fatal(err)
	}
}

func TestEffectiveRationaleFollowsTheLatestDecision(t *testing.T) {
	runID := "run-rationale"
	set := NewSet()
	set, candidate := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-rationale", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P2", SeverityRationale: "original reviewer judgement", Message: "rationale finding"}}})
	binding := CorrectionInput{
		Action: ProductReview, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision,
		FindingID: candidate.Findings[0].ID, Reason: "user disposition", Source: "user", UserAuthorized: true,
	}
	series, _ := set.Series(ProductReview)
	if got := EffectiveFindingRationale(series, candidate.ResultDigest, candidate.Findings[0]); got != "original reviewer judgement" {
		t.Fatalf("unadjudicated rationale is wrong: %q", got)
	}
	downgrade := binding
	downgrade.Decision = "RESEVERITIZE"
	downgrade.Severity = "P3"
	set = mustApply(t, set, runID, "rev-1", downgrade)
	series, _ = set.Series(ProductReview)
	if got := EffectiveFindingRationale(series, candidate.ResultDigest, candidate.Findings[0]); got != "original reviewer judgement" {
		t.Fatalf("a rationale-free correction must keep the original rationale: %q", got)
	}
	upgrade := binding
	upgrade.Decision = "RESEVERITIZE"
	upgrade.Severity = "P1"
	upgrade.SeverityRationale = "independent blocking justification"
	upgrade.Supersedes = LatestDecisionID(series, candidate.ResultDigest, candidate.Findings[0].ID)
	set = mustApply(t, set, runID, "rev-1", upgrade)
	series, _ = set.Series(ProductReview)
	if got := EffectiveFindingRationale(series, candidate.ResultDigest, candidate.Findings[0]); got != "independent blocking justification" {
		t.Fatalf("latest rationale must win: %q", got)
	}
}

func TestRevisionConflictLeavesTheLedgerUntouched(t *testing.T) {
	runID := "run-cas"
	set := NewSet()
	set, candidate := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-cas", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "cas finding"}}})
	snapshot, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	input := CorrectionInput{
		Action: ProductReview, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision,
		FindingID: candidate.Findings[0].ID, Decision: "DISMISS", Reason: "user rejects the blocker", Source: "user", UserAuthorized: true,
		ExpectedEffectiveRevision: 1 + 5,
	}
	if _, err := ApplyCorrection(set, runID, "rev-1", input); err == nil || !strings.Contains(err.Error(), "correction conflict") {
		t.Fatalf("stale revision was accepted: %v", err)
	}
	after, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(snapshot) {
		t.Fatal("conflicting correction mutated the set")
	}
	if err := Validate(runID, set); err != nil {
		t.Fatal(err)
	}
}

func TestWholeResultInvalidationRecyclesTheRoundAndKeepsEvidence(t *testing.T) {
	runID := "run-invalidate"
	set := NewSet()
	set, candidate := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-invalidate", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "candidate blocker"}}})
	set = mustApply(t, set, runID, "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision,
		FindingID: candidate.Findings[0].ID, Decision: "DISMISS", Reason: "user rejects the false blocker", Source: "user", UserAuthorized: true,
	})
	series, _ := set.Series(ProductReview)
	if series.Completed != 1 || series.Effective.Status != "PASS" || len(series.Adjudications) != 1 {
		t.Fatalf("dismiss did not clear the blocker: %#v", series)
	}
	set = mustApply(t, set, runID, "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision,
		Decision: "INVALIDATE_RESULT", Invalidity: "MISBOUND", Reason: "operator verified a whole-result misbinding", Source: "user", UserAuthorized: true,
	})
	series, _ = set.Series(ProductReview)
	if series.Completed != 0 || series.Effective.Status != "PENDING" || series.Effective.ResultDigest != candidate.ResultDigest || series.Effective.RawStatus != "FAIL" {
		t.Fatalf("invalidation did not return the projection to pending: %#v", series.Effective)
	}
	if len(series.RawCandidates) != 1 || len(series.Adjudications) != 1 || len(series.Corrections) != 1 {
		t.Fatalf("invalidation removed evidence: %#v", series)
	}
	if err := Validate(runID, set); err != nil {
		t.Fatal(err)
	}
	repeat := CorrectionInput{
		Action: ProductReview, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision,
		Decision: "INVALIDATE_RESULT", Invalidity: "STALE", Reason: "second attempt", Source: "user", UserAuthorized: true,
		ExpectedEffectiveRevision: series.Effective.Revision,
	}
	if _, err := ApplyCorrection(set, runID, "rev-1", repeat); err == nil || !strings.Contains(err.Error(), "already invalidated") {
		t.Fatalf("double invalidation was accepted: %v", err)
	}
	findingHop := CorrectionInput{
		Action: ProductReview, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision,
		FindingID: candidate.Findings[0].ID, Decision: "DISMISS", Reason: "late finding hop", Source: "user", UserAuthorized: true,
		ExpectedEffectiveRevision: series.Effective.Revision,
	}
	if _, err := ApplyCorrection(set, runID, "rev-1", findingHop); err == nil || !strings.Contains(err.Error(), "invalidated as a whole") {
		t.Fatalf("finding correction on an excluded result was accepted: %v", err)
	}
}

func TestWholeResultInvalidationRejectsFindingLevelFields(t *testing.T) {
	runID := "run-invalidate-shape"
	set := NewSet()
	set, candidate := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-shape", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "shape finding"}}})
	base := CorrectionInput{
		Action: ProductReview, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision,
		Decision: "INVALIDATE_RESULT", Invalidity: "INVALID", Reason: "user invalidates the round", Source: "user", UserAuthorized: true,
		ExpectedEffectiveRevision: 1,
	}
	withFinding := base
	withFinding.FindingID = candidate.Findings[0].ID
	if _, err := ApplyCorrection(set, runID, "rev-1", withFinding); err == nil || !strings.Contains(err.Error(), "does not take a finding id") {
		t.Fatalf("invalidation with a finding id was accepted: %v", err)
	}
	withSeverity := base
	withSeverity.Severity = "P2"
	if _, err := ApplyCorrection(set, runID, "rev-1", withSeverity); err == nil || !strings.Contains(err.Error(), "does not take severity or supersedes") {
		t.Fatalf("invalidation with a severity was accepted: %v", err)
	}
	withSupersedes := base
	withSupersedes.Supersedes = "review-adjudication-doesnotexist"
	if _, err := ApplyCorrection(set, runID, "rev-1", withSupersedes); err == nil || !strings.Contains(err.Error(), "does not take severity or supersedes") {
		t.Fatalf("invalidation with supersedes was accepted: %v", err)
	}
	withBadInvalidity := base
	withBadInvalidity.Invalidity = "WRONG"
	if _, err := ApplyCorrection(set, runID, "rev-1", withBadInvalidity); err == nil || !strings.Contains(err.Error(), "must classify invalidity") {
		t.Fatalf("invalidation with an unknown invalidity was accepted: %v", err)
	}
}

func TestHistoricalInvalidationKeepsTheCurrentEffectiveResult(t *testing.T) {
	runID := "run-historical"
	set := NewSet()
	set, first := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-first", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "first blocker"}}})
	set, second := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-second", RequirementRevision: "rev-1"}, Result{Status: "PASS", Findings: []Finding{{Severity: "P2", Message: "current advisory"}}})
	series, _ := set.Series(ProductReview)
	beforeRevision := series.Effective.Revision
	set = mustApply(t, set, runID, "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: first.DispatchID, ResultDigest: first.ResultDigest, RequirementRevision: first.RequirementRevision,
		Decision: "INVALIDATE_RESULT", Invalidity: "INVALID", Reason: "user invalidates the historical result", Source: "user", UserAuthorized: true,
	})
	series, _ = set.Series(ProductReview)
	if series.Completed != 1 || len(series.RawCandidates) != 2 || len(series.Corrections) != 1 || !resultExcluded(series, first.ResultDigest) {
		t.Fatalf("historical invalidation did not preserve evidence while removing one round: %#v", series)
	}
	if series.Effective.Status != "PASS" || series.Effective.ResultDigest != second.ResultDigest || series.Effective.Revision != beforeRevision+1 {
		t.Fatalf("historical invalidation overwrote the current effective result: %#v", series.Effective)
	}
	staleHop := CorrectionInput{
		Action: ProductReview, DispatchID: first.DispatchID, ResultDigest: first.ResultDigest, RequirementRevision: first.RequirementRevision,
		FindingID: first.Findings[0].ID, Decision: "DISMISS", Reason: "late correction of a historical finding", Source: "user", UserAuthorized: true,
		ExpectedEffectiveRevision: series.Effective.Revision,
	}
	if _, err := ApplyCorrection(set, runID, "rev-1", staleHop); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("finding correction on a historical result was accepted: %v", err)
	}
	set = mustApply(t, set, runID, "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: second.DispatchID, ResultDigest: second.ResultDigest, RequirementRevision: second.RequirementRevision,
		FindingID: second.Findings[0].ID, Decision: "CONFIRM", Reason: "current result stays adjudicated", Source: "user", UserAuthorized: true,
	})
	if err := Validate(runID, set); err != nil {
		t.Fatal(err)
	}
}

func seriesRevision(set Set, action Action) int {
	series, ok := set.Series(action)
	if !ok {
		return 0
	}
	return series.Effective.Revision
}

func TestFindingCorrectionRequiresTheCurrentRequirementRevision(t *testing.T) {
	runID := "run-stale-revision"
	set := NewSet()
	set, candidate := mustRecord(t, set, runID, StartReadiness, Dispatch{ID: "dispatch-old-revision", RequirementRevision: "old-rev"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P2", Message: "readiness concern"}}})
	input := CorrectionInput{
		Action: StartReadiness, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision,
		FindingID: candidate.Findings[0].ID, Decision: "DISMISS", Reason: "late user correction", Source: "user", UserAuthorized: true,
		ExpectedEffectiveRevision: seriesRevision(set, StartReadiness),
	}
	if _, err := ApplyCorrection(set, runID, "new-rev", input); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("finding correction across a requirement revision change was accepted: %v", err)
	}
	set = MarkStale(set, "REQUIREMENT_REVISION_INVALIDATION")
	set = mustApply(t, set, runID, "new-rev", CorrectionInput{
		Action: StartReadiness, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision,
		Decision: "INVALIDATE_RESULT", Invalidity: "STALE", Reason: "user invalidates the prior-revision result", Source: "user", UserAuthorized: true,
	})
	series, _ := set.Series(StartReadiness)
	if series.Completed != 0 || len(series.RawCandidates) != 1 || series.Effective.Status != "PENDING" {
		t.Fatalf("prior-revision invalidation did not return the series to pending: %#v", series)
	}
	if err := Validate(runID, set); err != nil {
		t.Fatal(err)
	}
}

func TestUpstreamBlockingStalesStartReadinessOneWay(t *testing.T) {
	runID := "run-cascade"
	set := NewSet()
	set, productCandidate := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-product", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P2", Message: "advisory defect"}}})
	set, _ = mustRecord(t, set, runID, StartReadiness, Dispatch{ID: "dispatch-readiness", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P2", Message: "readiness concern"}}})
	// PASS→PASS 的纠正不级联
	set = mustApply(t, set, runID, "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: productCandidate.DispatchID, ResultDigest: productCandidate.ResultDigest, RequirementRevision: productCandidate.RequirementRevision,
		FindingID: productCandidate.Findings[0].ID, Decision: "CONFIRM", Reason: "advisory confirmed as P2", Source: "user", UserAuthorized: true,
	})
	if series, _ := set.Series(StartReadiness); series.Effective.Status != "PASS" || series.Effective.Revision != 1 {
		t.Fatalf("advisory confirmation cascaded downstream: %#v", series.Effective)
	}
	// PASS→阻塞 的纠正级联：start-readiness 刷 PENDING，账本与计轮不动
	set = mustApply(t, set, runID, "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: productCandidate.DispatchID, ResultDigest: productCandidate.ResultDigest, RequirementRevision: productCandidate.RequirementRevision,
		FindingID: productCandidate.Findings[0].ID, Decision: "RESEVERITIZE", Severity: "P1", SeverityRationale: "the defect blocks the documented entry path",
		Supersedes: LatestDecisionID(mustSeries(t, set, ProductReview), productCandidate.ResultDigest, productCandidate.Findings[0].ID),
		Reason:     "upgrade to blocking", Source: "user", UserAuthorized: true,
	})
	product, _ := set.Series(ProductReview)
	downstream, _ := set.Series(StartReadiness)
	if product.Effective.Status != "FAIL" || !strings.EqualFold(product.Effective.DownstreamImpact[0], "start-readiness") {
		t.Fatalf("blocking product review did not mark its own downstream table: %#v", product.Effective)
	}
	if downstream.Effective.Status != "PENDING" || downstream.Effective.Revision != 2 || !strings.HasPrefix(downstream.Effective.Source, "UPSTREAM_CORRECTION:review-") {
		t.Fatalf("upstream correction did not stale start-readiness: %#v", downstream.Effective)
	}
	if len(downstream.Effective.BlockingFindingIDs) != 0 || len(downstream.Effective.AdvisoryFindingIDs) != 0 || !strings.EqualFold(downstream.Effective.DownstreamImpact[0], "slicing") {
		t.Fatalf("staled start-readiness kept projections or lost its impact marker: %#v", downstream.Effective)
	}
	if downstream.Completed != 1 || len(downstream.RawCandidates) != 1 {
		t.Fatalf("upstream correction mutated the downstream ledger: %#v", downstream)
	}
	if err := Validate(runID, set); err != nil {
		t.Fatal(err)
	}
	// 反向不级联：start-readiness 自身的纠正不触碰 product-review
	before, err := json.Marshal(product)
	if err != nil {
		t.Fatal(err)
	}
	set = mustApply(t, set, runID, "rev-1", CorrectionInput{
		Action: StartReadiness, DispatchID: "dispatch-readiness", ResultDigest: downstream.RawCandidates[0].ResultDigest, RequirementRevision: "rev-1",
		FindingID: downstream.RawCandidates[0].Findings[0].ID, Decision: "RESEVERITIZE", Severity: "P1", SeverityRationale: "the readiness concern blocks the start path",
		Reason: "downstream turns blocking on its own", Source: "user", UserAuthorized: true,
	})
	after, err := json.Marshal(mustSeries(t, set, ProductReview))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("start-readiness correction touched the product-review series")
	}
	if err := Validate(runID, set); err != nil {
		t.Fatal(err)
	}
}

func mustSeries(t *testing.T, set Set, action Action) Series {
	t.Helper()
	series, ok := set.Series(action)
	if !ok {
		t.Fatalf("unknown action %s", action)
	}
	return series
}

func TestRecordingABlockingRoundMarksOnlyItsOwnSeries(t *testing.T) {
	runID := "run-record-impact"
	set := NewSet()
	set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-clean", RequirementRevision: "rev-1"}, Result{Status: "PASS"})
	set, _ = mustRecord(t, set, runID, StartReadiness, Dispatch{ID: "dispatch-ready", RequirementRevision: "rev-1"}, Result{Status: "PASS"})
	set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-blocking", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "late blocker"}}})
	product := mustSeries(t, set, ProductReview)
	if product.Effective.Status != "FAIL" || len(product.Effective.DownstreamImpact) != len(downstreamImpact(ProductReview)) {
		t.Fatalf("PASS→FAIL record did not mark the product table: %#v", product.Effective)
	}
	downstream := mustSeries(t, set, StartReadiness)
	if downstream.Effective.Status != "PASS" || downstream.Effective.Revision != 1 || len(downstream.Effective.DownstreamImpact) != 0 {
		t.Fatalf("record must not restale the other series by itself: %#v", downstream.Effective)
	}
	if err := Validate(runID, set); err != nil {
		t.Fatal(err)
	}
}

func TestMarkStaleResetsBothSeriesAndKeepsAccounting(t *testing.T) {
	runID := "run-mark-stale"
	set := NewSet()
	set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-product", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "product blocker"}}})
	set, _ = mustRecord(t, set, runID, StartReadiness, Dispatch{ID: "dispatch-readiness", RequirementRevision: "rev-1"}, Result{Status: "PASS"})
	set = MarkStale(set, "REQUIREMENT_REVISION_INVALIDATION")
	for _, action := range []Action{ProductReview, StartReadiness} {
		series := mustSeries(t, set, action)
		if series.Effective.Status != "PENDING" || series.Effective.Source != "REQUIREMENT_REVISION_INVALIDATION" || series.Effective.Revision != 2 {
			t.Fatalf("%s was not staled: %#v", action, series.Effective)
		}
		if len(series.Effective.BlockingFindingIDs) != 0 || len(series.Effective.AdvisoryFindingIDs) != 0 {
			t.Fatalf("%s kept finding projections after stale: %#v", action, series.Effective)
		}
		if !slices.Equal(series.Effective.DownstreamImpact, downstreamImpact(action)) {
			t.Fatalf("%s lost its stale impact marker: %#v", action, series.Effective)
		}
		if series.Completed != 1 || len(series.RawCandidates) != 1 {
			t.Fatalf("%s accounting moved on stale: %#v", action, series)
		}
	}
	if err := Validate(runID, set); err != nil {
		t.Fatal(err)
	}
	set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-fresh", RequirementRevision: "rev-2"}, Result{Status: "PASS"})
	product := mustSeries(t, set, ProductReview)
	if product.Effective.Status != "PASS" || product.Effective.RequirementRevision != "rev-2" || product.Completed != 2 {
		t.Fatalf("fresh revision did not reopen the series: %#v", product)
	}
}

func TestConfirmedBlockingProjectionBindsTheRequirementRevision(t *testing.T) {
	runID := "run-confirmed"
	set := NewSet()
	set, candidate := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-confirm", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "confirmed requirement defect"}}})
	set = mustApply(t, set, runID, "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision,
		FindingID: candidate.Findings[0].ID, Decision: "CONFIRM", Reason: "user confirms the requirement defect", Source: "user", UserAuthorized: true,
	})
	series := mustSeries(t, set, ProductReview)
	// Controller 的换版复审门槛由这两个投影字段派生：阻塞 finding 非空且
	// revision 仍是绑定值。
	if len(series.Effective.BlockingFindingIDs) != 1 || series.Effective.RequirementRevision != "rev-1" {
		t.Fatalf("confirmed blocker projection is not derivable: %#v", series.Effective)
	}
	// 内核不把守门槛本身：同 revision 的再次记录是允许的，投影跟随最新证据
	set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-again", RequirementRevision: "rev-1"}, Result{Status: "PASS"})
	series = mustSeries(t, set, ProductReview)
	if series.Completed != 2 || series.Effective.Status != "PASS" {
		t.Fatalf("same-revision record was gated by the kernel: %#v", series)
	}
	set = MarkStale(set, "REQUIREMENT_REVISION_INVALIDATION")
	set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-changed", RequirementRevision: "rev-2"}, Result{Status: "PASS"})
	series = mustSeries(t, set, ProductReview)
	if series.Effective.RequirementRevision != "rev-2" || series.Completed != 3 {
		t.Fatalf("changed revision did not become the bound revision: %#v", series)
	}
	if err := Validate(runID, set); err != nil {
		t.Fatal(err)
	}
}

func TestCorrectionRequiresUserAuthorizationReasonAndSource(t *testing.T) {
	runID := "run-auth"
	set := NewSet()
	set, candidate := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-auth", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "auth finding"}}})
	binding := CorrectionInput{
		Action: ProductReview, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision,
		FindingID: candidate.Findings[0].ID, Decision: "DISMISS", Reason: "reason", Source: "source", UserAuthorized: true, ExpectedEffectiveRevision: 1,
	}
	if _, err := ApplyCorrection(set, runID, "rev-1", func() CorrectionInput { c := binding; c.UserAuthorized = false; return c }()); err == nil || !strings.Contains(err.Error(), "user authorization") {
		t.Fatalf("unauthorized correction was accepted: %v", err)
	}
	if _, err := ApplyCorrection(set, runID, "rev-1", func() CorrectionInput { c := binding; c.Reason = ""; return c }()); err == nil || !strings.Contains(err.Error(), "non-empty reason") {
		t.Fatalf("reasonless correction was accepted: %v", err)
	}
	if _, err := ApplyCorrection(set, runID, "rev-1", func() CorrectionInput { c := binding; c.Source = ""; return c }()); err == nil || !strings.Contains(err.Error(), "non-empty reason") {
		t.Fatalf("sourceless correction was accepted: %v", err)
	}
	if _, err := ApplyCorrection(set, runID, "rev-1", func() CorrectionInput { c := binding; c.Action = Action("qa-review"); return c }()); err == nil || !strings.Contains(err.Error(), "unknown pre-development review action") {
		t.Fatalf("unknown action was accepted: %v", err)
	}
	if _, err := ApplyCorrection(set, runID, "rev-1", func() CorrectionInput { c := binding; c.FindingID = "finding-doesnotexist"; return c }()); err == nil || !strings.Contains(err.Error(), "not part of the bound result") {
		t.Fatalf("unknown finding was accepted: %v", err)
	}
	if _, err := ApplyCorrection(set, runID, "rev-1", func() CorrectionInput { c := binding; c.Decision = "ACCEPT"; return c }()); err == nil || !strings.Contains(err.Error(), "CONFIRM, DISMISS") {
		t.Fatalf("unknown decision was accepted: %v", err)
	}
}
