package review

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func verifiedByOperator() OperatorVerification {
	return OperatorVerification{
		Checks:   []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"},
		Evidence: "operator re-ran the bound dispatch and compared the recorded evidence",
	}
}

func mustRecord(t *testing.T, set Set, runID string, action Action, dispatch Dispatch, result Result) (Set, RawCandidate) {
	t.Helper()
	next, _, err := RecordCandidate(set, runID, action, dispatch, result, verifiedByOperator())
	if err != nil {
		t.Fatalf("record %s candidate: %v", action, err)
	}
	series, ok := next.Series(action)
	if !ok {
		t.Fatalf("unknown action %s", action)
	}
	return next, series.RawCandidates[len(series.RawCandidates)-1]
}

func mustApply(t *testing.T, set Set, runID, requirementRevision string, input CorrectionInput) Set {
	t.Helper()
	series, ok := set.Series(input.Action)
	if !ok {
		t.Fatalf("unknown action %s", input.Action)
	}
	input.ExpectedEffectiveRevision = series.Effective.Revision
	next, err := ApplyCorrection(set, runID, requirementRevision, input)
	if err != nil {
		t.Fatalf("apply %s %s correction: %v", input.Action, input.Decision, err)
	}
	return next
}

func TestNewSetInitializesBothSeries(t *testing.T) {
	set := NewSet()
	if err := Validate("run-new", set); err != nil {
		t.Fatalf("fresh set is inconsistent: %v", err)
	}
	for _, action := range []Action{ProductReview, StartReadiness} {
		series, ok := set.Series(action)
		if !ok {
			t.Fatalf("%s series is missing", action)
		}
		if series.Action != action || series.ActivationSource != "WORKFLOW_START" {
			t.Fatalf("%s binding is wrong: %#v", action, series)
		}
		if series.AutomaticLimit != 3 || series.CurrentLimit != 3 || series.RemainingAutomatic != 3 || series.RemainingCapacity != 3 {
			t.Fatalf("%s capacity is not flat: %#v", action, series)
		}
		if series.Completed != 0 || series.ExtraAuthorized != 0 || series.ExtraConsumed != 0 || series.NextRoundRequirement != "AUTOMATIC_CAPACITY" {
			t.Fatalf("%s projections are not flat: %#v", action, series)
		}
		if series.Authorizations == nil || series.RawCandidates == nil || series.Adjudications == nil || series.Corrections == nil {
			t.Fatalf("%s has a nil ledger: %#v", action, series)
		}
		if series.Effective.Status != "PENDING" || series.Effective.Source != "WORKFLOW_START" || len(series.Effective.BlockingFindingIDs) != 0 || len(series.Effective.AdvisoryFindingIDs) != 0 || len(series.Effective.DownstreamImpact) != 0 {
			t.Fatalf("%s effective status is not a clean pending: %#v", action, series.Effective)
		}
	}
	if _, ok := set.Series(Action("qa")); ok {
		t.Fatal("unknown action resolved to a series")
	}
}

func TestCompleteResultsCountRoundsWhileDispositionsDoNot(t *testing.T) {
	runID := "run-rounds"
	set := NewSet()
	set, candidate := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "candidate blocker"}}})
	series, _ := set.Series(ProductReview)
	if series.Completed != 1 || series.Effective.Status != "FAIL" || series.Effective.Revision != 1 {
		t.Fatalf("first complete result did not count/propagate: %#v", series)
	}
	set = mustApply(t, set, runID, "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision,
		FindingID: candidate.Findings[0].ID, Decision: "CONFIRM", Reason: "user confirms the blocker", Source: "user", UserAuthorized: true,
	})
	series, _ = set.Series(ProductReview)
	if series.Completed != 1 || series.Effective.Status != "FAIL" || series.RemainingAutomatic != 2 || series.RemainingCapacity != 2 {
		t.Fatalf("disposition changed round counting or capacity: %#v", series)
	}
	set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-2", RequirementRevision: "rev-1"}, Result{Status: "PASS"})
	series, _ = set.Series(ProductReview)
	if series.Completed != 2 || series.Effective.Status != "PASS" || series.Effective.ResultDigest == candidate.ResultDigest {
		t.Fatalf("second complete result did not reproject: %#v", series)
	}
	if err := Validate(runID, set); err != nil {
		t.Fatal(err)
	}
}

func TestNonSemanticStatusNeverCreatesACandidate(t *testing.T) {
	set := NewSet()
	_, _, err := RecordCandidate(set, "run-runtime", ProductReview, Dispatch{ID: "dispatch-boom", RequirementRevision: "rev-1"}, Result{Status: "RUNTIME_ERROR", Message: "transient runner failure"}, verifiedByOperator())
	if err == nil || !strings.Contains(err.Error(), "only PASS or FAIL") {
		t.Fatalf("runtime error was accepted as a candidate: %v", err)
	}
	if err := Validate("run-runtime", set); err != nil {
		t.Fatalf("rejected recording mutated the set: %v", err)
	}
	series, _ := set.Series(ProductReview)
	if len(series.RawCandidates) != 0 || series.Completed != 0 {
		t.Fatalf("runtime error built ledger state: %#v", series)
	}
}

func TestCapacityAcrossAutomaticAndAuthorizedRounds(t *testing.T) {
	runID := "run-capacity"
	set := NewSet()
	for round := 1; round <= 3; round++ {
		set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: fmt.Sprintf("dispatch-%d", round), RequirementRevision: "rev-1"}, Result{Status: "PASS"})
	}
	series, _ := set.Series(ProductReview)
	if series.Completed != 3 || series.RemainingAutomatic != 0 || series.RemainingCapacity != 0 || series.CurrentLimit != 3 || series.NextRoundRequirement != "USER_DECISION_REQUIRED" {
		t.Fatalf("automatic boundary is wrong: %#v", series)
	}
	_, _, err := RecordCandidate(set, runID, ProductReview, Dispatch{ID: "dispatch-4", RequirementRevision: "rev-1"}, Result{Status: "PASS"}, verifiedByOperator())
	if err == nil || !strings.Contains(err.Error(), "automatic round limit") {
		t.Fatalf("fourth round without authorization was accepted: %v", err)
	}
	set, err = AppendAuthorization(set, runID, ProductReview, "operator asks for a fourth semantic round", "dispatch-4")
	if err != nil {
		t.Fatal(err)
	}
	series, _ = set.Series(ProductReview)
	authorizationID := series.Authorizations[0].ID
	if !strings.HasPrefix(authorizationID, "review-authorization-") || series.CurrentLimit != 4 || series.ExtraAuthorized != 1 || series.RemainingCapacity != 1 || series.NextRoundRequirement != "AUTHORIZED_EXTRA_ROUND" {
		t.Fatalf("authorization did not extend capacity: %#v", series)
	}
	_, _, err = RecordCandidate(set, runID, ProductReview, Dispatch{ID: "dispatch-other", RequirementRevision: "rev-1", AuthorizationID: authorizationID}, Result{Status: "PASS"}, verifiedByOperator())
	if err == nil || !strings.Contains(err.Error(), "not bound to a valid review authorization") {
		t.Fatalf("result with an unbound dispatch was accepted: %v", err)
	}
	set, fourth := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-4", RequirementRevision: "rev-1", AuthorizationID: authorizationID}, Result{Status: "PASS"})
	series, _ = set.Series(ProductReview)
	if series.Completed != 4 || series.ExtraConsumed != 1 || series.RemainingCapacity != 0 || series.NextRoundRequirement != "USER_DECISION_REQUIRED" || len(series.Authorizations[0].RecordedResultDigests) != 1 {
		t.Fatalf("authorized round did not consume its slot: %#v", series)
	}
	set = mustApply(t, set, runID, "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: fourth.DispatchID, ResultDigest: fourth.ResultDigest, RequirementRevision: fourth.RequirementRevision,
		Decision: "INVALIDATE_RESULT", Invalidity: "INVALID", Reason: "whole round was misrecorded", Source: "user", UserAuthorized: true,
	})
	series, _ = set.Series(ProductReview)
	if series.Completed != 3 || series.ExtraConsumed != 0 || series.RemainingCapacity != 1 || series.NextRoundRequirement != "AUTHORIZED_EXTRA_ROUND" {
		t.Fatalf("invalidation did not recycle the authorization slot: %#v", series)
	}
	if err := Validate(runID, set); err != nil {
		t.Fatal(err)
	}
}

// TestConcurrentlyFreedAuthorizationSlotsKeepTheLedgerValid：两条历史授权
// 被整轮作废合法释放（三轮自动→逐次授权完成第四、五轮→作废第四、第五）
// 是账本合法形态：防授权累计的闸门在新增授权入口（AppendAuthorization
// 拒绝存在未占用授权时的再授权），不在账本级。两个空槽同时在场时校验通
// 过、completed 回落、下一轮直接使用空槽授权。
func TestConcurrentlyFreedAuthorizationSlotsKeepTheLedgerValid(t *testing.T) {
	runID := "run-freed-slots"
	set := NewSet()
	for round := 1; round <= 3; round++ {
		set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: fmt.Sprintf("dispatch-%d", round), RequirementRevision: "rev-1"}, Result{Status: "PASS"})
	}
	var err error
	set, err = AppendAuthorization(set, runID, ProductReview, "fourth round required", "dispatch-4")
	if err != nil {
		t.Fatal(err)
	}
	firstGrant := mustSeries(t, set, ProductReview).Authorizations[0].ID
	set, fourth := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-4", RequirementRevision: "rev-1", AuthorizationID: firstGrant}, Result{Status: "PASS"})
	set, err = AppendAuthorization(set, runID, ProductReview, "fifth round required", "dispatch-5")
	if err != nil {
		t.Fatal(err)
	}
	secondGrant := mustSeries(t, set, ProductReview).Authorizations[1].ID
	set, fifth := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-5", RequirementRevision: "rev-1", AuthorizationID: secondGrant}, Result{Status: "PASS"})
	if series := mustSeries(t, set, ProductReview); series.Completed != 5 || series.ExtraConsumed != 2 {
		t.Fatalf("granted rounds did not consume their slots: %+v", series)
	}
	for _, candidate := range []RawCandidate{fourth, fifth} {
		set = mustApply(t, set, runID, "rev-1", CorrectionInput{
			Action: ProductReview, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision,
			Decision: "INVALIDATE_RESULT", Invalidity: "INVALID", Reason: "the round was misrecorded", Source: "user", UserAuthorized: true,
		})
	}
	series := mustSeries(t, set, ProductReview)
	if series.Completed != 3 || series.Effective.Status != "PENDING" {
		t.Fatalf("invalidations did not roll back completion and effective: %+v", series)
	}
	if series.ExtraAuthorized != 2 || series.ExtraConsumed != 0 || series.RemainingCapacity != 2 || series.NextRoundRequirement != "AUTHORIZED_EXTRA_ROUND" {
		t.Fatalf("both freed grants must be empty slots: %+v", series)
	}
	if occupied := OccupiedAuthorizationIDs(series); occupied[firstGrant] || occupied[secondGrant] {
		t.Fatalf("freed grants are still occupied: %v", occupied)
	}
	if err := Validate(runID, set); err != nil {
		t.Fatalf("two concurrently freed grants are a legal ledger form: %v", err)
	}
}

func TestAppendAuthorizationRequiresReasonAndBindsFirstDispatch(t *testing.T) {
	runID := "run-authorize"
	set := NewSet()
	if _, err := AppendAuthorization(set, runID, ProductReview, "   ", "dispatch-1"); err == nil || !strings.Contains(err.Error(), "non-empty reason") {
		t.Fatalf("blank reason was accepted: %v", err)
	}
	if _, err := AppendAuthorization(set, runID, ProductReview, "reason", "  "); err == nil || !strings.Contains(err.Error(), "dispatch binding") {
		t.Fatalf("blank dispatch was accepted: %v", err)
	}
	set, err := AppendAuthorization(set, runID, ProductReview, "operator asks for more evidence", "dispatch-1")
	if err != nil {
		t.Fatal(err)
	}
	// 用户每次只授权恰好一轮：存在未占用授权时不得预先累计下一轮。
	if _, err := AppendAuthorization(set, runID, ProductReview, "operator asks again", "dispatch-2"); err == nil || !strings.Contains(err.Error(), "unconsumed extra-round authorization") {
		t.Fatalf("accumulating an unconsumed authorization was accepted: %v", err)
	}
	series, _ := set.Series(ProductReview)
	firstID := series.Authorizations[0].ID
	set, _, err = RecordCandidate(set, runID, ProductReview, Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1", AuthorizationID: firstID}, Result{Status: "PASS"}, verifiedByOperator())
	if err != nil {
		t.Fatal(err)
	}
	// 占用后可再授权下一轮：身份随序号区分、绑定各自的派发。
	set, err = AppendAuthorization(set, runID, ProductReview, "operator asks for more evidence", "dispatch-2")
	if err != nil {
		t.Fatal(err)
	}
	series, _ = set.Series(ProductReview)
	first, second := series.Authorizations[0], series.Authorizations[1]
	if first.ID != firstID || first.ID == second.ID || first.DispatchIDs[0] != "dispatch-1" || second.DispatchIDs[0] != "dispatch-2" {
		t.Fatalf("authorization identity or first binding is wrong: %#v", series.Authorizations)
	}
	if err := Validate(runID, set); err != nil {
		t.Fatal(err)
	}
}

func TestBindDispatchAttachesToFirstUnoccupiedAuthorization(t *testing.T) {
	runID := "run-bind"
	set := NewSet()
	for round := 1; round <= 3; round++ {
		set, _ = mustRecord(t, set, runID, StartReadiness, Dispatch{ID: fmt.Sprintf("dispatch-%d", round), RequirementRevision: "rev-1"}, Result{Status: "PASS"})
	}
	var err error
	set, err = AppendAuthorization(set, runID, StartReadiness, "runtime retry needs a new dispatch", "dispatch-4a")
	if err != nil {
		t.Fatal(err)
	}
	set, err = BindDispatch(set, runID, StartReadiness, "dispatch-4b")
	if err != nil {
		t.Fatal(err)
	}
	series, _ := set.Series(StartReadiness)
	authorization := series.Authorizations[0]
	if len(authorization.DispatchIDs) != 2 || authorization.DispatchIDs[0] != "dispatch-4a" || authorization.DispatchIDs[1] != "dispatch-4b" {
		t.Fatalf("retry dispatch was not bound to the same authorization: %#v", authorization)
	}
	set, _ = mustRecord(t, set, runID, StartReadiness, Dispatch{ID: "dispatch-4b", RequirementRevision: "rev-1", AuthorizationID: authorization.ID}, Result{Status: "PASS"})
	if _, err := BindDispatch(set, runID, StartReadiness, "dispatch-5"); err == nil || !strings.Contains(err.Error(), "no unoccupied") {
		t.Fatalf("binding to an occupied authorization was accepted: %v", err)
	}
	if err := Validate(runID, set); err != nil {
		t.Fatal(err)
	}
}

// TestBindDispatchToUnoccupiedConsumesReleasedSlots：整轮失效释放授权后，
// 签发点的自动再绑定把新派发绑上空槽（无空槽时无操作）——释放槽由自然
// 签发/重试路径消费。
func TestBindDispatchToUnoccupiedConsumesReleasedSlots(t *testing.T) {
	runID := "run-rebind"
	set := NewSet()
	for round := 1; round <= 3; round++ {
		set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: fmt.Sprintf("dispatch-%d", round), RequirementRevision: "rev-1"}, Result{Status: "PASS"})
	}
	set, err := AppendAuthorization(set, runID, ProductReview, "one extra round", "dispatch-4")
	if err != nil {
		t.Fatal(err)
	}
	series, _ := set.Series(ProductReview)
	authorizationID := series.Authorizations[0].ID
	set, fourth := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-4", RequirementRevision: "rev-1", AuthorizationID: authorizationID}, Result{Status: "PASS"})
	// 占用中的授权：签发点再绑定是无操作。
	set = BindDispatchToUnoccupied(set, runID, ProductReview, "dispatch-5")
	series, _ = set.Series(ProductReview)
	if got := series.Authorizations[0].DispatchIDs; len(got) != 1 {
		t.Fatalf("an occupied authorization must not rebind: %#v", got)
	}
	// 整轮失效释放：签发点自动绑上新派发。
	set = mustApply(t, set, runID, "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: fourth.DispatchID, ResultDigest: fourth.ResultDigest, RequirementRevision: fourth.RequirementRevision,
		Decision: "INVALIDATE_RESULT", Invalidity: "INVALID",
		Reason: "wrong document binding", Source: "user", UserAuthorized: true,
	})
	set = BindDispatchToUnoccupied(set, runID, ProductReview, "dispatch-5")
	series, _ = set.Series(ProductReview)
	if got := series.Authorizations[0].DispatchIDs; len(got) != 2 || got[1] != "dispatch-5" {
		t.Fatalf("the released slot was not consumed at issuance: %#v", got)
	}
	if err := Validate(runID, set); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizationEndorsesAtMostOneValidResult(t *testing.T) {
	runID := "run-endorse"
	set := NewSet()
	for round := 1; round <= 3; round++ {
		set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: fmt.Sprintf("dispatch-%d", round), RequirementRevision: "rev-1"}, Result{Status: "PASS"})
	}
	var err error
	set, err = AppendAuthorization(set, runID, ProductReview, "one extra round", "dispatch-a")
	if err != nil {
		t.Fatal(err)
	}
	set, err = BindDispatch(set, runID, ProductReview, "dispatch-b")
	if err != nil {
		t.Fatal(err)
	}
	series, _ := set.Series(ProductReview)
	authorizationID := series.Authorizations[0].ID
	set, first := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-a", RequirementRevision: "rev-1", AuthorizationID: authorizationID}, Result{Status: "PASS"})
	_, _, err = RecordCandidate(set, runID, ProductReview, Dispatch{ID: "dispatch-b", RequirementRevision: "rev-1", AuthorizationID: authorizationID}, Result{Status: "PASS"}, verifiedByOperator())
	if err == nil || !strings.Contains(err.Error(), "already endorses a valid result") {
		t.Fatalf("double endorsement was accepted: %v", err)
	}
	set = mustApply(t, set, runID, "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: first.DispatchID, ResultDigest: first.ResultDigest, RequirementRevision: first.RequirementRevision,
		Decision: "INVALIDATE_RESULT", Invalidity: "STALE", Reason: "bound revision moved on", Source: "user", UserAuthorized: true,
	})
	set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-b", RequirementRevision: "rev-1", AuthorizationID: authorizationID}, Result{Status: "PASS"})
	series, _ = set.Series(ProductReview)
	if series.Completed != 4 || series.ExtraConsumed != 1 {
		t.Fatalf("freed slot was not reusable: %#v", series)
	}
	if err := Validate(runID, set); err != nil {
		t.Fatal(err)
	}
}

// TestRemainingCapacityAdmitsOnlyWhatTheLedgerBacks：remaining 必须与准入
// 规则一致——自动轮被整轮失效回收后，completed 回到自动上限内但授权仍被
// 占用时，下一轮依旧需要新授权，remaining 不得显示还有免授权空位。期望值
// 全部按账本事实独立写出，不经生产投影函数。
func TestRemainingCapacityAdmitsOnlyWhatTheLedgerBacks(t *testing.T) {
	runID := "run-remaining"
	set := NewSet()
	set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1"}, Result{Status: "PASS"})
	if series := mustSeries(t, set, ProductReview); series.RemainingCapacity != 2 {
		t.Fatalf("after one automatic round remaining = %d, want 2", series.RemainingCapacity)
	}
	set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-2", RequirementRevision: "rev-1"}, Result{Status: "PASS"})
	set, third := mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-3", RequirementRevision: "rev-1"}, Result{Status: "PASS"})
	// 三个自动轮完成、无授权：remaining 0，下一轮须用户决定。
	if series := mustSeries(t, set, ProductReview); series.RemainingCapacity != 0 || series.NextRoundRequirement != "USER_DECISION_REQUIRED" {
		t.Fatalf("exhausted automatic capacity = %+v", series)
	}
	var err error
	set, err = AppendAuthorization(set, runID, ProductReview, "fourth round required", "dispatch-4")
	if err != nil {
		t.Fatal(err)
	}
	// 空槽授权可立即使用：remaining 1。
	if series := mustSeries(t, set, ProductReview); series.RemainingCapacity != 1 || series.NextRoundRequirement != "AUTHORIZED_EXTRA_ROUND" {
		t.Fatalf("granted capacity = %+v", series)
	}
	authorizationID := mustSeries(t, set, ProductReview).Authorizations[0].ID
	set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-4", RequirementRevision: "rev-1", AuthorizationID: authorizationID}, Result{Status: "PASS"})
	if series := mustSeries(t, set, ProductReview); series.RemainingCapacity != 0 || series.Completed != 4 {
		t.Fatalf("authorized round did not consume its slot: %+v", series)
	}
	// 关键态：失效一个自动轮（历史轮，不碰授权轮）——completed 3、授权仍
	// 占用、无空槽：准入要求新授权，remaining 必须是 0 而不是 1。
	set = mustApply(t, set, runID, "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: third.DispatchID, ResultDigest: third.ResultDigest, RequirementRevision: third.RequirementRevision,
		Decision: "INVALIDATE_RESULT", Invalidity: "INVALID", Reason: "the round was misrecorded", Source: "user", UserAuthorized: true,
	})
	series := mustSeries(t, set, ProductReview)
	if series.Completed != 3 || series.CurrentLimit != 4 || series.RemainingCapacity != 0 || series.NextRoundRequirement != "USER_DECISION_REQUIRED" {
		t.Fatalf("recycled automatic round inflated remaining: %+v", series)
	}
	// remaining=0 与准入互证：无新授权的第五轮必须被拒。
	_, _, err = RecordCandidate(set, runID, ProductReview, Dispatch{ID: "dispatch-5", RequirementRevision: "rev-1"}, Result{Status: "PASS"}, verifiedByOperator())
	if err == nil || !strings.Contains(err.Error(), "automatic round limit") {
		t.Fatalf("a round without capacity or authorization was accepted: %v", err)
	}
	if err := Validate(runID, set); err != nil {
		t.Fatal(err)
	}
}

// TestTransformsKeepTheCallerInputSetImmutable：四个账目变换都是值进值出——
// 变换后输入 Set 逐字节不变且仍通过完整性校验（调用方输入账本不可被内核
// 改写）。
func TestTransformsKeepTheCallerInputSetImmutable(t *testing.T) {
	runID := "run-immutable"
	snapshot := func(t *testing.T, set Set) string {
		t.Helper()
		data, err := json.Marshal(set)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	assertUntouched := func(t *testing.T, input Set, before string, transform string) {
		t.Helper()
		if after := snapshot(t, input); after != before {
			t.Fatalf("%s mutated the caller input set", transform)
		}
		if err := Validate(runID, input); err != nil {
			t.Fatalf("input set no longer validates after %s: %v", transform, err)
		}
	}

	// 授权轮已被占用：再授权与 finding 级纠正都不得改写输入。
	occupied := NewSet()
	for round := 1; round <= 3; round++ {
		occupied, _ = mustRecord(t, occupied, runID, ProductReview, Dispatch{ID: fmt.Sprintf("dispatch-%d", round), RequirementRevision: "rev-1"}, Result{Status: "PASS"})
	}
	var err error
	occupied, err = AppendAuthorization(occupied, runID, ProductReview, "fourth round required", "dispatch-4")
	if err != nil {
		t.Fatal(err)
	}
	authorizationID := mustSeries(t, occupied, ProductReview).Authorizations[0].ID
	occupied, fourth := mustRecord(t, occupied, runID, ProductReview, Dispatch{ID: "dispatch-4", RequirementRevision: "rev-1", AuthorizationID: authorizationID}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P2", Message: "advisory concern"}}})

	before := snapshot(t, occupied)
	_, err = AppendAuthorization(occupied, runID, ProductReview, "fifth round required", "dispatch-5")
	if err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, occupied, before, "AppendAuthorization")

	before = snapshot(t, occupied)
	_, err = ApplyCorrection(occupied, runID, "rev-1", CorrectionInput{
		Action: ProductReview, DispatchID: fourth.DispatchID, ResultDigest: fourth.ResultDigest, RequirementRevision: fourth.RequirementRevision,
		FindingID: fourth.Findings[0].ID, Decision: "CONFIRM", Reason: "user confirms the advisory", Source: "user", UserAuthorized: true,
		ExpectedEffectiveRevision: mustSeries(t, occupied, ProductReview).Effective.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, occupied, before, "ApplyCorrection")

	// 未占用授权上的派发重绑与授权候选记录：授权账本的元素写回不得落进
	// 调用方的底层数组。
	sparse := NewSet()
	for round := 1; round <= 3; round++ {
		sparse, _ = mustRecord(t, sparse, runID, StartReadiness, Dispatch{ID: fmt.Sprintf("dispatch-%d", round), RequirementRevision: "rev-1"}, Result{Status: "PASS"})
	}
	sparse, err = AppendAuthorization(sparse, runID, StartReadiness, "retry needs a fresh dispatch", "dispatch-4a")
	if err != nil {
		t.Fatal(err)
	}
	sparse, err = BindDispatch(sparse, runID, StartReadiness, "dispatch-4b")
	if err != nil {
		t.Fatal(err)
	}

	before = snapshot(t, sparse)
	_, err = BindDispatch(sparse, runID, StartReadiness, "dispatch-4c")
	if err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, sparse, before, "BindDispatch")

	before = snapshot(t, sparse)
	_, _, err = RecordCandidate(sparse, runID, StartReadiness, Dispatch{ID: "dispatch-4b", RequirementRevision: "rev-1", AuthorizationID: authorizationIDOf(sparse, StartReadiness)}, Result{Status: "PASS"}, verifiedByOperator())
	if err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, sparse, before, "RecordCandidate")
}

func authorizationIDOf(set Set, action Action) string {
	series, ok := set.Series(action)
	if !ok || len(series.Authorizations) == 0 {
		return ""
	}
	return series.Authorizations[0].ID
}

func TestDuplicateResultDigestIsRejected(t *testing.T) {
	runID := "run-duplicate"
	set := NewSet()
	result := Result{Status: "PASS", Message: "clean", Findings: []Finding{{Severity: "P2", Message: "advisory"}}}
	set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1"}, result)
	_, _, err := RecordCandidate(set, runID, ProductReview, Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1"}, result, verifiedByOperator())
	if err == nil || !strings.Contains(err.Error(), "already recorded") {
		t.Fatalf("duplicate digest was accepted: %v", err)
	}
	set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-2", RequirementRevision: "rev-1"}, result)
	series, _ := set.Series(ProductReview)
	if series.Completed != 2 {
		t.Fatalf("a distinct dispatch must be a distinct result: %#v", series)
	}
}

func TestFindingIdentityDerivesFromTheTenContentFields(t *testing.T) {
	base := Finding{
		Severity: "P2", SeverityRationale: "rationale", Category: "correctness", Requirement: "REQ-1",
		NormalPath: "documented entry", ActualError: "wrong result", DeferralImpact: "core path stays broken",
		Verification: FindingVerification{Scope: "in scope", Severity: "checked", Causality: "traced", Reproduction: "re-ran", Classification: "defect"},
		Message:      "broken entry", Locations: []string{"entry.go:1"},
	}
	digest := findingDigest(base)
	if id := "finding-" + shortDigest(digest); len(id) != len("finding-")+16 {
		t.Fatalf("finding id is not a 16-hex short digest: %s", id)
	}
	mutated := base
	mutated.Severity = "P3"
	if findingDigest(mutated) == digest {
		t.Fatal("severity does not participate in the finding digest")
	}
	mutated = base
	mutated.Locations = append([]string{}, base.Locations...)
	mutated.Locations = append(mutated.Locations, "other.go:2")
	if findingDigest(mutated) == digest {
		t.Fatal("locations do not participate in the finding digest")
	}
	mutated = base
	mutated.Verification.Causality = "changed"
	if findingDigest(mutated) == digest {
		t.Fatal("verification does not participate in the finding digest")
	}
	emptyLocations := base
	emptyLocations.Locations = nil
	explicitlyEmpty := base
	explicitlyEmpty.Locations = []string{}
	if findingDigest(emptyLocations) != findingDigest(explicitlyEmpty) {
		t.Fatal("empty and missing locations must digest identically")
	}
}

func TestMalformedOrRepeatedFindingsAreRejectedAtRecord(t *testing.T) {
	runID := "run-findings"
	set := NewSet()
	_, _, err := RecordCandidate(set, runID, ProductReview, Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P9", Message: "bad severity"}}}, verifiedByOperator())
	if err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("invalid severity was accepted: %v", err)
	}
	_, _, err = RecordCandidate(set, runID, ProductReview, Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P1"}}}, verifiedByOperator())
	if err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("empty message was accepted: %v", err)
	}
	_, _, err = RecordCandidate(set, runID, ProductReview, Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "same"}, {Severity: "P1", Message: "same"}}}, verifiedByOperator())
	if err == nil || !strings.Contains(err.Error(), "repeats finding identity") {
		t.Fatalf("identical content twice was accepted: %v", err)
	}
	set, _ = mustRecord(t, set, runID, ProductReview, Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1"}, Result{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "same"}, {Severity: "P2", Message: "same"}}})
	series, _ := set.Series(ProductReview)
	if len(series.RawCandidates[0].Findings) != 2 {
		t.Fatalf("distinct severities must remain distinct findings: %#v", series.RawCandidates[0])
	}
}

func TestOperatorVerificationIsAClosedCheckSet(t *testing.T) {
	complete := verifiedByOperator()
	set := NewSet()
	_, _, err := RecordCandidate(set, "run-verify", ProductReview, Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1"}, Result{Status: "PASS"}, OperatorVerification{Checks: complete.Checks, Evidence: "  "})
	if err == nil || !strings.Contains(err.Error(), "non-empty evidence") {
		t.Fatalf("empty evidence was accepted: %v", err)
	}
	_, _, err = RecordCandidate(set, "run-verify", ProductReview, Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1"}, Result{Status: "PASS"}, OperatorVerification{Checks: complete.Checks[:7], Evidence: "e"})
	if err == nil || !strings.Contains(err.Error(), "missing checks") {
		t.Fatalf("incomplete checks were accepted: %v", err)
	}
	_, _, err = RecordCandidate(set, "run-verify", ProductReview, Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1"}, Result{Status: "PASS"}, OperatorVerification{Checks: append(append([]string{}, complete.Checks...), "binding"), Evidence: "e"})
	if err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("duplicated checks were accepted: %v", err)
	}
	_, _, err = RecordCandidate(set, "run-verify", ProductReview, Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1"}, Result{Status: "PASS"}, OperatorVerification{Checks: append(append([]string{}, complete.Checks...), "root-cause"), Evidence: "e"})
	if err == nil || !strings.Contains(err.Error(), "not one of the eight") {
		t.Fatalf("unknown check was accepted: %v", err)
	}
	next, candidate := func() (Set, RawCandidate) {
		next, _, err := RecordCandidate(set, "run-verify", ProductReview, Dispatch{ID: "dispatch-1", RequirementRevision: "rev-1"}, Result{Status: "PASS"}, complete)
		if err != nil {
			t.Fatalf("complete verification was rejected: %v", err)
		}
		series, _ := next.Series(ProductReview)
		return next, series.RawCandidates[0]
	}()
	set = next
	if !slices.IsSorted(candidate.OperatorVerification.Checks) {
		t.Fatalf("checks are not stored sorted: %#v", candidate.OperatorVerification)
	}
	if err := Validate("run-verify", set); err != nil {
		t.Fatal(err)
	}
}
