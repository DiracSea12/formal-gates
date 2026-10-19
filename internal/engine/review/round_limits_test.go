package review

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestConfiguredLimitsAreRecordedAtSeriesCreation(t *testing.T) {
	set := NewSetWithLimits(1, 64, 7)
	if !reflect.DeepEqual(set.ProductReview.LimitHistory, []LimitInterval{{Value: 1, Revision: 7}}) || !reflect.DeepEqual(set.StartReadiness.LimitHistory, []LimitInterval{{Value: 64, Revision: 7}}) {
		t.Fatal("series creation recorded a default interval instead of its applied value")
	}
	if err := Validate("run", set); err != nil {
		t.Fatal(err)
	}
}

func TestRoundLimitsBoundAutomaticCapacity(t *testing.T) {
	for _, limit := range []int{1, 3, 64} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			set := SetLimit(NewSet(), ProductReview, limit, 1)
			for n := 0; n < limit; n++ {
				set, _ = mustRecord(t, set, "run", ProductReview, Dispatch{ID: fmt.Sprint(n), RequirementRevision: "req"}, Result{Status: "PASS"})
			}
			if _, _, err := RecordCandidate(set, "run", ProductReview, Dispatch{ID: "extra", RequirementRevision: "req"}, Result{Status: "PASS"}, verifiedByOperator()); err == nil {
				t.Fatal("an automatic round exceeded the configured limit")
			}
			if err := Validate("run", set); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRoundLimitsKeepReviewSeriesIndependent(t *testing.T) {
	set := SetLimit(NewSet(), ProductReview, 1, 1)
	set = SetLimit(set, StartReadiness, 64, 1)
	set, _ = mustRecord(t, set, "run", ProductReview, Dispatch{ID: "product", RequirementRevision: "req"}, Result{Status: "PASS"})
	for n := 0; n < 5; n++ {
		set, _ = mustRecord(t, set, "run", StartReadiness, Dispatch{ID: fmt.Sprint(n), RequirementRevision: "req"}, Result{Status: "PASS"})
	}
	if set.ProductReview.RemainingAutomatic != 0 || set.StartReadiness.RemainingAutomatic != 59 {
		t.Fatal("review series shared their automatic capacity")
	}
}

func TestAutomaticAdmissionsCannotMultiplyTheSameCapacity(t *testing.T) {
	set := NewSet()
	for n := 0; n < 4; n++ {
		set = AdmitRound(set, ProductReview, fmt.Sprint(n), 1)
	}
	for n := 0; n < 3; n++ {
		set, _ = mustRecord(t, set, "run", ProductReview, Dispatch{ID: fmt.Sprint(n), RequirementRevision: "req"}, Result{Status: "PASS"})
	}
	if _, _, err := RecordCandidate(set, "run", ProductReview, Dispatch{ID: "3", RequirementRevision: "req"}, Result{Status: "PASS"}, verifiedByOperator()); err == nil {
		t.Fatal("duplicate automatic capacity snapshots created an extra slot")
	}
}

func TestRoundLimitIntervalsPreserveExecutedAndIssuedRounds(t *testing.T) {
	set := NewSet()
	for n := 0; n < 3; n++ {
		set, _ = mustRecord(t, set, "run", ProductReview, Dispatch{ID: fmt.Sprint(n), RequirementRevision: "req"}, Result{Status: "PASS"})
	}
	set = SetLimit(set, ProductReview, 5, 10)
	set = AdmitRound(set, ProductReview, "fourth", 11)
	set = SetLimit(set, ProductReview, 1, 12)
	set, fourth := mustRecord(t, set, "run", ProductReview, Dispatch{ID: "fourth", RequirementRevision: "req"}, Result{Status: "PASS"})
	if set.ProductReview.Completed != 4 || set.ProductReview.RemainingAutomatic != 0 {
		t.Fatal("lowering the limit revoked an issued automatic round")
	}
	if _, _, err := RecordCandidate(set, "run", ProductReview, Dispatch{ID: "ungranted", RequirementRevision: "req"}, Result{Status: "PASS"}, verifiedByOperator()); err == nil {
		t.Fatal("lowering the limit allowed a new automatic round")
	}
	set = mustApply(t, set, "run", "req", CorrectionInput{
		Action: ProductReview, DispatchID: fourth.DispatchID, ResultDigest: fourth.ResultDigest, RequirementRevision: "req",
		Decision: "INVALIDATE_RESULT", Invalidity: "INVALID", Reason: "result belongs to the wrong input", Source: "user", UserAuthorized: true,
	})
	if set.ProductReview.Completed != 3 || set.ProductReview.DispatchRounds["fourth"].Revision != 11 {
		t.Fatal("whole-result invalidation changed the original execution identity")
	}
	if err := Validate("run", set); err != nil {
		t.Fatal("historical automatic rounds were rejudged using the reduced limit", err)
	}
	broken := cloneSet(t, set)
	execution := broken.ProductReview.DispatchRounds["1"]
	execution.Completed = 3
	broken.ProductReview.DispatchRounds["1"] = execution
	if err := Validate("run", broken); err == nil {
		t.Fatal("interval validation dropped the historical over-limit check")
	}
}

func TestRoundLimitChangesRetainAuthorizationAndReplacementIdentity(t *testing.T) {
	set := SetLimit(NewSet(), ProductReview, 1, 1)
	set, _ = mustRecord(t, set, "run", ProductReview, Dispatch{ID: "first", RequirementRevision: "req"}, Result{Status: "PASS"})
	var err error
	set, err = AppendAuthorization(set, "run", ProductReview, "review revised requirement")
	if err != nil {
		t.Fatal(err)
	}
	set = mustBind(t, set, "run", ProductReview, "second", noDispatchInFlight)
	set = AdmitRound(set, ProductReview, "second", 2)
	authorization := set.ProductReview.Authorizations[0].ID
	set = SetLimit(set, ProductReview, 64, 3)
	set = SetLimit(set, ProductReview, 1, 4)
	set = BindReplacementDispatch(set, "run", ProductReview, "second", "replacement")
	if set.ProductReview.DispatchRounds["replacement"] != set.ProductReview.DispatchRounds["second"] {
		t.Fatal("replacement acquired a new admission instead of its original round")
	}
	data, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	var restored Set
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored, set) {
		t.Fatal("restoration changed round identities or intervals")
	}
	restored, _ = mustRecord(t, restored, "run", ProductReview, Dispatch{ID: "replacement", RequirementRevision: "req", AuthorizationID: authorization}, Result{Status: "PASS"})
	if restored.ProductReview.Completed != 2 || restored.ProductReview.ExtraConsumed != 1 {
		t.Fatal("an already authorized round lost its independent slot")
	}
	if err := Validate("run", restored); err != nil {
		t.Fatal(err)
	}
}
