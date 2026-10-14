// requirement_test.go 覆盖需求修订重走的内核原语：单 series stale 只刷新
// 用户勾选的那条（不跨 series 级联——上游失效级联只属于纠正路径），以及
// "已确认阻塞"查询与 finding 有效裁决查询按裁决与需求 revision 的判定
// 边界。
package review

import (
	"encoding/json"
	"slices"
	"testing"
)

// TestStaleSeriesRefreshesOnlyTheCheckedAction：需求修订重走只刷新被勾选
// 的 series——PENDING、来源与代数前移、投影清空、下游影响打标，四本账与
// 计轮不动；另一条 series 的结论原样保留（未勾选＝续用，其 PASS 由协议
// 层的 carry 判定继续有效）。上游失效级联（UpstreamVoid）不属于本路径，
// 只由纠正路径（ApplyCorrection→markUpstreamStale）触发。
func TestStaleSeriesRefreshesOnlyTheCheckedAction(t *testing.T) {
	set := NewSet()
	set, productRound := mustRecord(t, set, "run-req", ProductReview,
		Dispatch{ID: "dispatch-product-1", RequirementRevision: "rev-1"},
		Result{Status: "PASS", Message: "clean"})
	set, readinessRound := mustRecord(t, set, "run-req", StartReadiness,
		Dispatch{ID: "dispatch-readiness-1", RequirementRevision: "rev-1"},
		Result{Status: "PASS", Message: "ready"})
	productRevision := set.ProductReview.Effective.Revision
	readinessRevision := set.StartReadiness.Effective.Revision

	staled := StaleSeries(set, ProductReview, "requirement-change:req-1", 5)
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
	// 未勾选的 readiness 不被触碰：结论、账本、UpstreamVoid 全部原样。
	carried := staled.StartReadiness
	if carried.Effective.Status != "PASS" || carried.Effective.Revision != readinessRevision || carried.UpstreamVoid ||
		len(carried.RawCandidates) != 1 || carried.Completed != 1 {
		t.Fatalf("an unchecked series must be carried untouched: %+v void=%v", carried.Effective, carried.UpstreamVoid)
	}

	// 反向：stale start-readiness 不触碰 product-review。
	onlyReadiness := StaleSeries(set, StartReadiness, "requirement-change:req-2", 7)
	if onlyReadiness.ProductReview.Effective.Status != "PASS" || onlyReadiness.ProductReview.Effective.Revision != productRevision {
		t.Fatalf("stale start-readiness must not touch product-review: %+v", onlyReadiness.ProductReview.Effective)
	}
	if onlyReadiness.StartReadiness.Effective.Status != "PENDING" || onlyReadiness.StartReadiness.Effective.ResultDigest != readinessRound.ResultDigest {
		t.Fatalf("staled readiness = %+v", onlyReadiness.StartReadiness.Effective)
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
	reconfirmed := StaleSeries(downgraded, ProductReview, "requirement-change:req-5", 9)
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

// TestFindingDecisionReportsEffectiveAdjudication：FindingDecision 返回
// finding 的所属动作、有效裁决与有效严重度（未裁决时裁决为空、严重度保
// 持原始值）；整轮失效排除后不可查。
func TestFindingDecisionReportsEffectiveAdjudication(t *testing.T) {
	set := NewSet()
	set, round := mustRecord(t, set, "run-owner", ProductReview,
		Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1"},
		Result{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "core blocker"}}})
	digest := round.Findings[0].Digest
	action, decision, severity, ok := FindingDecision(set, digest)
	if !ok || action != ProductReview || decision != "" || severity != "P1" {
		t.Fatalf("FindingDecision = %s,%q,%q,%v want unadjudicated P1", action, decision, severity, ok)
	}
	if _, _, _, ok := FindingDecision(set, "sha256:unknown"); ok {
		t.Fatal("an unknown digest must not be locatable")
	}
	confirmed := mustApply(t, set, "run-owner", "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: "dispatch-1", RequirementRevision: "rev-1",
		ResultDigest: round.ResultDigest, FindingID: round.Findings[0].ID,
		Decision: "CONFIRM", Reason: "user confirms", Source: "engine-decide:req-1", UserAuthorized: true,
	})
	if _, decision, severity, ok := FindingDecision(confirmed, digest); !ok || decision != "CONFIRM" || severity != "P1" {
		t.Fatalf("confirmed finding decision = %q/%q ok=%v", decision, severity, ok)
	}
	invalidated := mustApply(t, confirmed, "run-owner", "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: "dispatch-1", RequirementRevision: "rev-1",
		ResultDigest: round.ResultDigest,
		Decision:     "INVALIDATE_RESULT", Invalidity: "STALE",
		Reason: "the round is stale", Source: "engine-decide:req-2", UserAuthorized: true,
	})
	if _, _, _, ok := FindingDecision(invalidated, digest); ok {
		t.Fatal("a finding inside an invalidated round must not be adjudicable")
	}
}

func TestFindingDecisionUsesTheCurrentBindingAndLatestUserDisposition(t *testing.T) {
	for _, action := range []Action{ProductReview, StartReadiness} {
		for _, originalDecision := range []string{"DISMISS", "CONFIRM"} {
			t.Run(string(action)+"/"+originalDecision, func(t *testing.T) {
				const runID = "run-finding-disposition"
				finding := Finding{Severity: "P2", Category: "acceptance-entry", Message: "documented input boundary", Locations: []string{"requirements.md:5"}}
				oldOnly := Finding{Severity: "P3", Category: "implementation-detail", Message: "original wording"}
				set, original := mustRecord(t, NewSet(), runID, action, Dispatch{ID: "original-review", RequirementRevision: "rev-1"}, Result{Status: "PASS", Findings: []Finding{finding, oldOnly}})
				apply := func(candidate RawCandidate, decision, severity, reason string) {
					t.Helper()
					series, _ := set.Series(action)
					set = mustApply(t, set, runID, candidate.RequirementRevision, CorrectionInput{Action: action, DispatchID: candidate.DispatchID, RequirementRevision: candidate.RequirementRevision, ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID, Decision: decision, Severity: severity, Reason: reason, Source: "user", UserAuthorized: true, Supersedes: LatestDecisionID(series, candidate.ResultDigest, candidate.Findings[0].ID)})
				}
				apply(original, originalDecision, "", "original premise checked")
				// The lookup uses the series' own effective content binding.
				if owner, decision, severity, ok := FindingDecision(set, original.Findings[0].Digest); !ok || owner != action || decision != originalDecision || severity != "P2" {
					t.Fatalf("original bound disposition = %s/%s/%s/%v", owner, decision, severity, ok)
				}
				set = StaleSeries(set, action, "requirement-change:current", 9)
				set, current := mustRecord(t, set, runID, action, Dispatch{ID: "current-review", RequirementRevision: "rev-2"}, Result{Status: "PASS", Findings: []Finding{finding}})
				if current.Findings[0].Digest != original.Findings[0].Digest || current.Findings[0].ID != original.Findings[0].ID {
					t.Fatal("content identity must remain stable across requirement bindings")
				}
				if owner, decision, severity, ok := FindingDecision(set, current.Findings[0].Digest); !ok || owner != action || decision != "" || severity != "P2" {
					t.Fatalf("current raw finding borrowed an original disposition: %s/%s/%s/%v", owner, decision, severity, ok)
				}
				if _, _, _, ok := FindingDecision(set, original.Findings[1].Digest); ok {
					t.Fatal("an original-only finding cannot drive the current binding")
				}
				if facts := SettledFindings(set); len(facts) != 1 || facts[0].Decision != originalDecision || facts[0].RequirementRevision != "rev-1" {
					t.Fatalf("historical settled context was lost: %+v", facts)
				}
				currentDecision := "CONFIRM"
				if originalDecision == "CONFIRM" {
					currentDecision = "DISMISS"
				}
				apply(current, currentDecision, "", "current premise checked")
				if owner, decision, severity, ok := FindingDecision(set, current.Findings[0].Digest); !ok || owner != action || decision != currentDecision || severity != "P2" {
					t.Fatalf("current decision was replaced by the original one: %s/%s/%s/%v", owner, decision, severity, ok)
				}
				set, repeated := mustRecord(t, set, runID, action, Dispatch{ID: "repeated-review", RequirementRevision: "rev-2"}, Result{Status: "PASS", Findings: []Finding{finding}})
				apply(repeated, "DISMISS", "", "repeated premise checked")
				apply(current, "RESEVERITIZE", "P3", "latest current correction")
				before, err := json.Marshal(set)
				if err != nil {
					t.Fatal(err)
				}
				check := func(value Set) {
					t.Helper()
					owner, decision, severity, ok := FindingDecision(value, current.Findings[0].Digest)
					facts := SettledFindings(value)
					if !ok || owner != action || decision != "RESEVERITIZE" || severity != "P3" || len(facts) != 1 || facts[0].Decision != decision || facts[0].EffectiveSeverity != severity || facts[0].Reason != "latest current correction" || facts[0].RequirementRevision != "rev-2" {
						t.Fatalf("current lookup and latest settled source differ: %s/%s/%s/%v, %+v", owner, decision, severity, ok, facts)
					}
				}
				check(set)
				var reopened Set
				if err := json.Unmarshal(before, &reopened); err != nil {
					t.Fatal(err)
				}
				check(reopened)
				after, _ := json.Marshal(set)
				if string(after) != string(before) {
					t.Fatal("finding queries changed the original ledger")
				}
				set = mustApply(t, set, runID, "rev-2", CorrectionInput{Action: action, DispatchID: current.DispatchID, RequirementRevision: current.RequirementRevision, ResultDigest: current.ResultDigest, Decision: "INVALIDATE_RESULT", Invalidity: "STALE", Reason: "current round excluded", Source: "user", UserAuthorized: true})
				if _, decision, severity, ok := FindingDecision(set, findingDigest(finding)); !ok || decision != "DISMISS" || severity != "P2" {
					t.Fatalf("excluded correction still supplies current disposition: %s/%s/%v", decision, severity, ok)
				}
			})
		}
	}
}
