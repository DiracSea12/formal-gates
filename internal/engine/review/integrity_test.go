package review

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// richLedger 构造一个覆盖 adjudication、MarkStale、授权轮、supersede 链的
// 合法账本，作为防篡改负控的基底。
func richLedger(t *testing.T) (Set, string) {
	t.Helper()
	runID := "run-integrity"
	set := NewSet()
	set, productFirst := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-p1", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "product blocker"}}})
	set = mustApply(t, set, runID, "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: productFirst.DispatchID, ResultDigest: productFirst.ResultDigest, RequirementRevision: productFirst.RequirementRevision,
		FindingID: productFirst.Findings[0].ID, Decision: "CONFIRM", Reason: "user confirms the blocker", Source: "user", UserAuthorized: true,
	})
	set = MarkStale(set, "REQUIREMENT_REVISION_INVALIDATION")
	set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-p2", RequirementRevision: "rev-2"}, Result{Status: "PASS", Findings: []Finding{{Severity: "P3", Message: "advisory only"}}})
	set, _ = mustRecord(t, set, runID, StartReadiness, Dispatch{ID: "dispatch-s1", RequirementRevision: "rev-2"}, Result{Status: "PASS"})
	var err error
	set, err = AppendAuthorization(set, runID, StartReadiness, "operator wants one more readiness review")
	if err != nil {
		t.Fatal(err)
	}
	set = BindDispatchToUnoccupied(set, runID, StartReadiness, "dispatch-s2", noDispatchInFlight)
	authorizationID := mustSeries(t, set, StartReadiness).Authorizations[0].ID
	set, readinessSecond := mustRecord(t, set, runID, StartReadiness, Dispatch{ID: "dispatch-s2", RequirementRevision: "rev-2", AuthorizationID: authorizationID}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P2", Message: "readiness concern"}}})
	set = mustApply(t, set, runID, "rev-2", CorrectionInput{
		Action: StartReadiness, DispatchID: readinessSecond.DispatchID, ResultDigest: readinessSecond.ResultDigest, RequirementRevision: readinessSecond.RequirementRevision,
		FindingID: readinessSecond.Findings[0].ID, Decision: "RESEVERITIZE", Severity: "P1", SeverityRationale: "the concern blocks the documented start path",
		Reason: "upgrade the concern", Source: "user", UserAuthorized: true,
	})
	set = mustApply(t, set, runID, "rev-2", CorrectionInput{
		Action: StartReadiness, DispatchID: readinessSecond.DispatchID, ResultDigest: readinessSecond.ResultDigest, RequirementRevision: readinessSecond.RequirementRevision,
		FindingID: readinessSecond.Findings[0].ID, Decision: "DISMISS", Supersedes: LatestDecisionID(mustSeries(t, set, StartReadiness), readinessSecond.ResultDigest, readinessSecond.Findings[0].ID),
		Reason: "retracted after re-verification", Source: "user", UserAuthorized: true,
	})
	if err := Validate(runID, set); err != nil {
		t.Fatalf("rich ledger is not consistent: %v", err)
	}
	return set, runID
}

func cloneSet(t *testing.T, base Set) Set {
	t.Helper()
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var out Set
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func reforgedAdjudicationID(a Adjudication) string {
	identity := a
	identity.ID = ""
	return "review-adjudication-" + shortDigest(digestValue(identity))
}

func reforgedCorrectionID(c CorrectionReceipt) string {
	identity := c
	identity.ID = ""
	return "review-correction-" + shortDigest(digestValue(identity))
}

func TestValidateRejectsEachTamperClass(t *testing.T) {
	base, runID := richLedger(t)
	cases := []struct {
		name   string
		mutate func(set *Set, runID string)
		marker string
	}{
		{
			name:   "forged authorization identity",
			mutate: func(set *Set, _ string) { set.StartReadiness.Authorizations[0].Reason = "edited after the fact" },
			marker: "forged identity",
		},
		{
			name: "duplicate candidate digest",
			mutate: func(set *Set, _ string) {
				last := set.ProductReview.RawCandidates[len(set.ProductReview.RawCandidates)-1]
				set.ProductReview.RawCandidates = append(set.ProductReview.RawCandidates, last)
			},
			marker: "recorded twice",
		},
		{
			name: "re-forged supersedes points at another target",
			mutate: func(set *Set, _ string) {
				correction := set.StartReadiness.Corrections[0]
				correction.Supersedes = set.ProductReview.Adjudications[0].ID
				correction.ID = reforgedCorrectionID(correction)
				set.StartReadiness.Corrections[0] = correction
			},
			marker: "does not supersede the prior decision",
		},
		{
			name:   "completed projection inflated",
			mutate: func(set *Set, _ string) { set.ProductReview.Completed++ },
			marker: "candidate projection",
		},
		{
			name: "effective projection edited",
			mutate: func(set *Set, _ string) {
				set.ProductReview.Effective.AdvisoryFindingIDs = append(set.ProductReview.Effective.AdvisoryFindingIDs, "finding-forged")
			},
			marker: "does not match the raw/adjudication projection",
		},
		{
			name: "authorization over-endorsement with a forged candidate",
			mutate: func(set *Set, runID string) {
				series := &set.StartReadiness
				authorization := &series.Authorizations[0]
				duplicate := series.RawCandidates[len(series.RawCandidates)-1]
				duplicate.DispatchID = "dispatch-s2-retry"
				duplicate.ResultDigest = candidateDigest(runID, duplicate)
				duplicate.ID = "review-result-" + shortDigest(duplicate.ResultDigest)
				authorization.RecordedResultDigests = append(authorization.RecordedResultDigests, duplicate.ResultDigest)
				series.RawCandidates = append(series.RawCandidates, duplicate)
				refresh(series)
			},
			marker: "backs more than one valid result",
		},
		{
			name:   "tampered raw evidence",
			mutate: func(set *Set, _ string) { set.ProductReview.RawCandidates[0].Findings[0].Message = "tampered evidence" },
			marker: "finding identity",
		},
		{
			name: "duplicated decision revision",
			mutate: func(set *Set, _ string) {
				series := &set.StartReadiness
				duplicate := series.Adjudications[0]
				duplicate.Reason = "duplicate revision entry"
				duplicate.ID = reforgedAdjudicationID(duplicate)
				series.Adjudications = append(series.Adjudications, duplicate)
			},
			marker: "identity is invalid or duplicated",
		},
		{
			name: "downstream impact attached to a passing series",
			mutate: func(set *Set, _ string) {
				set.ProductReview.Effective.DownstreamImpact = downstreamImpact(ProductReview)
			},
			marker: "downstream impact is inconsistent",
		},
		{
			name: "authorization receipt dropped its recorded result",
			mutate: func(set *Set, _ string) {
				set.StartReadiness.Authorizations[0].RecordedResultDigests = []string{}
			},
			marker: "absent from its authorization receipt",
		},
		{
			name:   "nil ledger",
			mutate: func(set *Set, _ string) { set.ProductReview.Adjudications = nil },
			marker: "four ledgers",
		},
		{
			name: "adjudication old value edited and identity re-forged",
			mutate: func(set *Set, _ string) {
				adjudication := set.StartReadiness.Adjudications[0]
				adjudication.OldValue = "UNADJUDICATED:P0"
				adjudication.ID = reforgedAdjudicationID(adjudication)
				set.StartReadiness.Adjudications[0] = adjudication
			},
			marker: "old/new values do not match",
		},
		{
			name:   "upstream void claimed while effective stays PASS",
			mutate: func(set *Set, _ string) { set.StartReadiness.UpstreamVoid = true },
			marker: "upstream void",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tampered := cloneSet(t, base)
			tc.mutate(&tampered, runID)
			err := Validate(runID, tampered)
			if err == nil || !strings.Contains(err.Error(), tc.marker) {
				t.Fatalf("expected a %q rejection, got %v", tc.marker, err)
			}
		})
	}
}

func TestValidateRejectsAnUnboundOrPartialSet(t *testing.T) {
	if err := Validate("run-partial", Set{}); err == nil || !strings.Contains(err.Error(), "action binding") {
		t.Fatalf("zero set was accepted: %v", err)
	}
	set := NewSet()
	set.StartReadiness.Action = ProductReview
	if err := Validate("run-partial", set); err == nil || !strings.Contains(err.Error(), "action binding") {
		t.Fatalf("cross-bound series was accepted: %v", err)
	}
}

func TestStoredProjectionsMatchFullRecomputation(t *testing.T) {
	runID := "run-reproject"
	assertRecomputed := func(t *testing.T, set Set) {
		t.Helper()
		if err := Validate(runID, set); err != nil {
			t.Fatalf("incremental evolution produced an inconsistent ledger: %v", err)
		}
		for _, action := range []Action{ProductReview, StartReadiness} {
			series := mustSeries(t, set, action)
			if series.Completed != completedRounds(series) ||
				series.ExtraConsumed != occupiedExtraAuthorizations(series) ||
				series.CurrentLimit != series.AutomaticLimit+len(series.Authorizations) ||
				series.RemainingAutomatic != max(0, series.AutomaticLimit-series.Completed) ||
				series.NextRoundRequirement != nextRoundRequirement(series) {
				t.Fatalf("%s cached capacity diverged from a full recomputation", action)
			}
			// remaining 的期望在测试内独立算出：自动剩余轮数 + 未占用授权数
			//（与准入规则对齐），不调用生产投影函数。
			occupied := map[string]bool{}
			for _, candidate := range series.RawCandidates {
				if candidate.AuthorizationID != "" && !resultExcluded(series, candidate.ResultDigest) {
					occupied[candidate.AuthorizationID] = true
				}
			}
			unoccupied := 0
			for _, authorization := range series.Authorizations {
				if !occupied[authorization.ID] {
					unoccupied++
				}
			}
			if want := max(0, series.AutomaticLimit-series.Completed) + unoccupied; series.RemainingCapacity != want {
				t.Fatalf("%s remaining capacity = %d, want %d (automatic %d + unoccupied grants %d)", action, series.RemainingCapacity, want, max(0, series.AutomaticLimit-series.Completed), unoccupied)
			}
			if len(series.Effective.DownstreamImpact) != 0 && !slices.Equal(series.Effective.DownstreamImpact, downstreamImpact(action)) {
				t.Fatalf("%s downstream marker is not the canonical table", action)
			}
			if series.Effective.Status == statusPending {
				continue
			}
			candidate, ok := candidateByDigest(series, series.Effective.ResultDigest)
			if !ok {
				t.Fatalf("%s effective result is missing from the ledger", action)
			}
			expected := Effective(series, candidate, series.Effective.Revision)
			expected.DownstreamImpact = series.Effective.DownstreamImpact
			if !reflect.DeepEqual(expected, series.Effective) {
				t.Fatalf("%s stored effective %#v differs from the reprojection %#v", action, series.Effective, expected)
			}
		}
	}
	set := NewSet()
	set, product := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-p1", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P2", Message: "advisory defect"}}})
	assertRecomputed(t, set)
	set = mustApply(t, set, runID, "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: product.DispatchID, ResultDigest: product.ResultDigest, RequirementRevision: product.RequirementRevision,
		FindingID: product.Findings[0].ID, Decision: "RESEVERITIZE", Severity: "P1", SeverityRationale: "the defect blocks the documented entry path",
		Reason: "upgrade to blocking", Source: "user", UserAuthorized: true,
	})
	assertRecomputed(t, set)
	set, readiness := mustRecord(t, set, runID, StartReadiness, Dispatch{ID: "dispatch-s1", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P2", Message: "readiness concern"}}})
	assertRecomputed(t, set)
	set = mustApply(t, set, runID, "rev-1", CorrectionInput{
		Action: StartReadiness, DispatchID: readiness.DispatchID, ResultDigest: readiness.ResultDigest, RequirementRevision: readiness.RequirementRevision,
		FindingID: readiness.Findings[0].ID, Decision: "DISMISS", Reason: "concern retracted", Source: "user", UserAuthorized: true,
	})
	assertRecomputed(t, set)
	set = MarkStale(set, "IMPACT_INVALIDATION")
	assertRecomputed(t, set)
	set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-p2", RequirementRevision: "rev-2"}, Result{Status: "PASS"})
	assertRecomputed(t, set)
	var err error
	set, err = AppendAuthorization(set, runID, ProductReview, "one more round needed")
	if err != nil {
		t.Fatal(err)
	}
	assertRecomputed(t, set)
	set = BindDispatchToUnoccupied(set, runID, ProductReview, "dispatch-p3", noDispatchInFlight)
	authorizationID := mustSeries(t, set, ProductReview).Authorizations[0].ID
	set, extra := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-p3", RequirementRevision: "rev-2", AuthorizationID: authorizationID}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "extra-round blocker"}}})
	assertRecomputed(t, set)
	set = mustApply(t, set, runID, "rev-2", CorrectionInput{
		Action: ProductReview, DispatchID: extra.DispatchID, ResultDigest: extra.ResultDigest, RequirementRevision: extra.RequirementRevision,
		Decision: "INVALIDATE_RESULT", Invalidity: "INCOMPLETE", Reason: "the round never covered the full scope", Source: "user", UserAuthorized: true,
	})
	assertRecomputed(t, set)
}

func TestSetSerializesDeterministically(t *testing.T) {
	base, runID := richLedger(t)
	first, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip := Set{}
	if err := json.Unmarshal(first, &roundtrip); err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(roundtrip)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("set serialization is not deterministic across a round trip")
	}
	if !reflect.DeepEqual(base, roundtrip) {
		t.Fatal("set round trip lost or changed data")
	}
	if err := Validate(runID, roundtrip); err != nil {
		t.Fatalf("round-tripped set is inconsistent: %v", err)
	}
}
