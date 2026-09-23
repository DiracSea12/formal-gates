// requirement_test.go 覆盖需求修订重走的内核原语：单 series stale 的刷新
// 形状与上游级联、账本与计轮不动，以及"已确认阻塞"查询按裁决与需求
// revision 的判定边界。
package review

import (
	"slices"
	"testing"
)

// TestStaleSeriesRefreshesOneActionAndCascadesUpstreamVoid：对 product-review
// 执行单 series stale——自身刷为 PENDING、来源与代数前移、投影清空、下游
// 影响打标，四本账与计轮不动；start-readiness 仅被级联置 UpstreamVoid
// PENDING（结论失效），账本与计轮同样不动。对 start-readiness 执行则不触
// 碰 product-review。
func TestStaleSeriesRefreshesOneActionAndCascadesUpstreamVoid(t *testing.T) {
	set := NewSet()
	set, productRound := mustRecord(t, set, "run-req", ProductReview,
		Dispatch{ID: "dispatch-product-1", RequirementRevision: "rev-1"},
		Result{Status: "PASS", Message: "clean"})
	set, _ = mustRecord(t, set, "run-req", StartReadiness,
		Dispatch{ID: "dispatch-readiness-1", RequirementRevision: "rev-1"},
		Result{Status: "PASS", Message: "ready"})
	productRevision := set.ProductReview.Effective.Revision
	readinessRevision := set.StartReadiness.Effective.Revision

	staled := StaleSeries(set, ProductReview, "requirement-change:req-1")
	product := staled.ProductReview
	if product.Effective.Status != "PENDING" || product.Effective.Source != "requirement-change:req-1" || product.Effective.Revision != productRevision+1 {
		t.Fatalf("staled product effective = %+v", product.Effective)
	}
	if len(product.Effective.BlockingFindingIDs) != 0 || len(product.Effective.AdvisoryFindingIDs) != 0 {
		t.Fatalf("staled product retains finding projections: %+v", product.Effective)
	}
	if !slices.Equal(product.Effective.DownstreamImpact, downstreamImpact(ProductReview)) {
		t.Fatalf("staled product downstream impact = %v", product.Effective.DownstreamImpact)
	}
	// 证据与结论指针保留（PENDING 指向其来源证据），账本与计轮不动。
	if product.Effective.ResultDigest != productRound.ResultDigest || len(product.RawCandidates) != 1 || product.Completed != 1 {
		t.Fatalf("staled product ledger changed: effective=%+v candidates=%d completed=%d", product.Effective, len(product.RawCandidates), product.Completed)
	}
	cascaded := staled.StartReadiness
	if cascaded.Effective.Status != "PENDING" || !cascaded.UpstreamVoid {
		t.Fatalf("product stale must cascade start-readiness into upstream void: %+v void=%v", cascaded.Effective, cascaded.UpstreamVoid)
	}
	if cascaded.Effective.Revision != readinessRevision+1 || len(cascaded.RawCandidates) != 1 || cascaded.Completed != 1 {
		t.Fatalf("cascaded readiness ledger or counter changed: %+v", cascaded)
	}

	// 反向：stale start-readiness 不触碰 product-review。
	onlyReadiness := StaleSeries(set, StartReadiness, "requirement-change:req-2")
	if onlyReadiness.ProductReview.Effective.Status != "PASS" || onlyReadiness.ProductReview.Effective.Revision != productRevision {
		t.Fatalf("stale start-readiness must not touch product-review: %+v", onlyReadiness.ProductReview.Effective)
	}
	if onlyReadiness.StartReadiness.Effective.Status != "PENDING" || onlyReadiness.StartReadiness.UpstreamVoid {
		t.Fatalf("staled readiness = %+v void=%v", onlyReadiness.StartReadiness.Effective, onlyReadiness.StartReadiness.UpstreamVoid)
	}
	// 刷新后的账本必须整体通过防篡改校验（PENDING 持有下游影响表是合法形态）。
	if err := Validate("run-req", staled); err != nil {
		t.Fatalf("staled set must stay valid: %v", err)
	}
	if err := Validate("run-req", onlyReadiness); err != nil {
		t.Fatalf("single-action staled set must stay valid: %v", err)
	}
}

// TestHasConfirmedBlockingRequiresConfirmedBlockingSeverity：未被裁决的
// P1 只阻塞 effective、不构成确认；CONFIRM 或升到 P0/P1 的 RESEVERITIZE
// 才置位；DISMISS 或降到 P2/P3 清除；整轮失效同样清除。effective 绑定
// 前移到新 revision 后，旧 revision 下确认的阻塞不再计数。
func TestHasConfirmedBlockingRequiresConfirmedBlockingSeverity(t *testing.T) {
	set := NewSet()
	set, round := mustRecord(t, set, "run-blocking", ProductReview,
		Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1"},
		Result{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "unadjudicated blocker"}}})
	series, _ := set.Series(ProductReview)
	if series.Effective.Status != "FAIL" {
		t.Fatalf("raw P1 must block effective: %+v", series.Effective)
	}
	if HasConfirmedBlocking(series) {
		t.Fatal("an unadjudicated P1 is not a confirmed blocking finding")
	}

	confirmed := mustApply(t, set, "run-blocking", "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: "dispatch-1", RequirementRevision: "rev-1",
		ResultDigest: round.ResultDigest, FindingID: round.Findings[0].ID,
		Decision: "CONFIRM", Reason: "user confirms the blocker", Source: "engine-decide:req-1", UserAuthorized: true,
	})
	series, _ = confirmed.Series(ProductReview)
	if !HasConfirmedBlocking(series) {
		t.Fatal("a confirmed P1 must be a confirmed blocking finding")
	}

	dismissed := mustApply(t, confirmed, "run-blocking", "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: "dispatch-1", RequirementRevision: "rev-1",
		ResultDigest: round.ResultDigest, FindingID: round.Findings[0].ID,
		Decision: "DISMISS", Supersedes: series.Adjudications[0].ID,
		Reason: "user rejects the finding", Source: "engine-decide:req-2", UserAuthorized: true,
	})
	series, _ = dismissed.Series(ProductReview)
	if HasConfirmedBlocking(series) {
		t.Fatal("a dismissed finding is not blocking")
	}

	upgraded := mustApply(t, dismissed, "run-blocking", "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: "dispatch-1", RequirementRevision: "rev-1",
		ResultDigest: round.ResultDigest, FindingID: round.Findings[0].ID,
		Decision: "RESEVERITIZE", Severity: "P1", SeverityRationale: "the defect blocks the documented path",
		Supersedes: series.Corrections[0].ID, Reason: "user re-traces the impact", Source: "engine-decide:req-3", UserAuthorized: true,
	})
	series, _ = upgraded.Series(ProductReview)
	if !HasConfirmedBlocking(series) {
		t.Fatal("a re-severitized P1 is a confirmed blocking finding")
	}

	downgraded := mustApply(t, upgraded, "run-blocking", "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: "dispatch-1", RequirementRevision: "rev-1",
		ResultDigest: round.ResultDigest, FindingID: round.Findings[0].ID,
		Decision: "RESEVERITIZE", Severity: "P2", SeverityRationale: "impact limited to an optional path",
		Supersedes: series.Corrections[1].ID, Reason: "user downgrades after re-verification", Source: "engine-decide:req-4", UserAuthorized: true,
	})
	series, _ = downgraded.Series(ProductReview)
	if HasConfirmedBlocking(series) {
		t.Fatal("a P2 is not blocking regardless of confirmation")
	}

	// 需求 revision 前移：在新 revision 记一轮新候选后，effective 绑定离
	// 开 rev-1，旧 revision 下确认的阻塞不再计数。
	reconfirmed := StaleSeries(downgraded, ProductReview, "requirement-change:req-5")
	reconfirmed, _ = mustRecord(t, reconfirmed, "run-blocking", ProductReview,
		Dispatch{ID: "dispatch-2", RequirementRevision: "rev-2"},
		Result{Status: "PASS", Message: "fresh round on the changed requirement"})
	series, _ = reconfirmed.Series(ProductReview)
	if series.Effective.RequirementRevision != "rev-2" || HasConfirmedBlocking(series) {
		t.Fatalf("a fresh round at the new revision must clear the confirmed-blocking view: %+v", series.Effective)
	}

	// 整轮失效清除阻塞：确认 P1 后 INVALIDATE_RESULT，finding 随其证据失效。
	invalidated := mustApply(t, upgraded, "run-blocking", "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: "dispatch-1", RequirementRevision: "rev-1",
		ResultDigest: round.ResultDigest,
		Decision:     "INVALIDATE_RESULT", Invalidity: "INVALID",
		Reason: "the round's evidence is invalid", Source: "engine-decide:req-6", UserAuthorized: true,
	})
	series, _ = invalidated.Series(ProductReview)
	if HasConfirmedBlocking(series) {
		t.Fatal("an invalidated round cannot keep a confirmed blocking finding")
	}
}

// TestFindingOwnerLocatesActiveEvidence：FindingOwner 只在未被整轮失效排除
// 的证据中定位 finding；排除后同一 digest 不再可绑。
func TestFindingOwnerLocatesActiveEvidence(t *testing.T) {
	set := NewSet()
	set, round := mustRecord(t, set, "run-owner", ProductReview,
		Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1"},
		Result{Status: "FAIL", Findings: []Finding{{Severity: "P2", Message: "advisory defect"}}})
	digest := round.Findings[0].Digest
	if action, ok := FindingOwner(set, digest); !ok || action != ProductReview {
		t.Fatalf("FindingOwner = %s,%v want ProductReview,true", action, ok)
	}
	if _, ok := FindingOwner(set, "sha256:unknown"); ok {
		t.Fatal("an unknown digest must not be locatable")
	}
	invalidated := mustApply(t, set, "run-owner", "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: "dispatch-1", RequirementRevision: "rev-1",
		ResultDigest: round.ResultDigest,
		Decision:     "INVALIDATE_RESULT", Invalidity: "STALE",
		Reason: "the round is stale", Source: "engine-decide:req-1", UserAuthorized: true,
	})
	if _, ok := FindingOwner(invalidated, digest); ok {
		t.Fatal("a finding inside an invalidated round must not be locatable")
	}
}
