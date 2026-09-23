package review

import (
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
	set, err = AppendAuthorization(set, runID, ProductReview, "operator asks for more evidence", "dispatch-2")
	if err != nil {
		t.Fatal(err)
	}
	series, _ := set.Series(ProductReview)
	first, second := series.Authorizations[0], series.Authorizations[1]
	if first.ID == second.ID || first.DispatchIDs[0] != "dispatch-1" || second.DispatchIDs[0] != "dispatch-2" {
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
