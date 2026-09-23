package harness

import (
	"testing"
)

// mustEvaluateRoute / mustWorkerOutcome / mustCheckRetention / mustRepairAllowance /
// mustCandidateAdmission decode a JSON payload the way the bridge would
// (StrictDecode, unknown fields rejected) and run the judgement under test.
func mustEvaluateRoute(t *testing.T, payload string) routeVerdict {
	t.Helper()
	var q evaluateRouteInput
	if err := StrictDecode([]byte(payload), &q); err != nil {
		t.Fatalf("StrictDecode payload: %v", err)
	}
	return evaluateRoute(q)
}
func mustWorkerOutcome(t *testing.T, payload string) workerAction {
	t.Helper()
	var q workerOutcomeInput
	if err := StrictDecode([]byte(payload), &q); err != nil {
		t.Fatalf("StrictDecode payload: %v", err)
	}
	return workerOutcome(q)
}
func mustCheckRetention(t *testing.T, payload string) retentionVerdict {
	t.Helper()
	var q checkRetentionInput
	if err := StrictDecode([]byte(payload), &q); err != nil {
		t.Fatalf("StrictDecode payload: %v", err)
	}
	return checkRetention(q)
}
func mustRepairAllowance(t *testing.T, payload string) allowanceVerdict {
	t.Helper()
	var q repairAllowanceInput
	if err := StrictDecode([]byte(payload), &q); err != nil {
		t.Fatalf("StrictDecode payload: %v", err)
	}
	return repairAllowance(q)
}
func mustCandidateAdmission(t *testing.T, payload string) admissionVerdict {
	t.Helper()
	var q candidateAdmissionInput
	if err := StrictDecode([]byte(payload), &q); err != nil {
		t.Fatalf("StrictDecode payload: %v", err)
	}
	return candidateAdmission(q)
}

// ---- evaluate-route --------------------------------------------------------

func TestEvaluateRouteEmptyCollectionsSeal(t *testing.T) {
	res := mustEvaluateRoute(t, `{"checks":[],"gates":[]}`)
	if res.Route != "seal" {
		t.Fatalf("empty collections must seal, got %v", res.Route)
	}
	if res.FailedChecks == nil || len(res.FailedChecks) != 0 {
		t.Fatalf("failed_checks must be an empty list, got %v", res.FailedChecks)
	}
	if len(res.BlockingGates) != 0 {
		t.Fatalf("blocking_gates must be empty, got %v", res.BlockingGates)
	}
}

func TestEvaluateRouteOnlyP3IsNotBlocking(t *testing.T) {
	res := mustEvaluateRoute(t, `{"checks":[{"id":"c1","status":"PASS"}],
		"gates":[{"gate":"complexity-gate","result":{"status":"PASS","summary":"s","check_ids":[],
		"findings":[{"id":"f1","severity":"P3","message":"m","paths":[]}]}}]}`)
	if res.Route != "seal" {
		t.Fatalf("PASS with only P3 must seal, got %v", res.Route)
	}
	if len(res.BlockingGates) != 0 {
		t.Fatalf("P3-only findings must not block, got %v", res.BlockingGates)
	}
}

func TestEvaluateRoutePassWithP2Blocks(t *testing.T) {
	res := mustEvaluateRoute(t, `{"checks":[{"id":"c1","status":"PASS"}],
		"gates":[{"gate":"complexity-gate","result":{"status":"PASS","summary":"s","check_ids":[],
		"findings":[{"id":"f1","severity":"P3","message":"m","paths":[]},
		            {"id":"f2","severity":"P2","message":"m","paths":["a"]}]}}]}`)
	if res.Route != "repair" {
		t.Fatalf("PASS with a P2 finding must repair, got %v", res.Route)
	}
	blocking := res.BlockingGates
	if len(blocking) != 1 || blocking[0] != "complexity-gate" {
		t.Fatalf("blocking gate wrong: %v", blocking)
	}
	if len(res.FailedChecks) != 0 {
		t.Fatalf("no failed checks expected, got %v", res.FailedChecks)
	}
}

func TestEvaluateRouteFailedCheckAndOrderKept(t *testing.T) {
	res := mustEvaluateRoute(t, `{"checks":[{"id":"aaa","status":"PASS"},{"id":"zzz","status":"FAIL"},{"id":"bbb","status":"FAIL"}],
		"gates":[{"gate":"implementation-quality-gate","result":{"status":"PASS","summary":"s","check_ids":[],"findings":[]}},
		         {"gate":"complexity-gate","result":{"status":"FAIL","summary":"s","check_ids":[],
		          "findings":[{"id":"f1","severity":"P1","message":"m","paths":[]}]}}]}`)
	if res.Route != "repair" {
		t.Fatalf("failed check + FAIL gate must repair, got %v", res.Route)
	}
	failed := res.FailedChecks
	if len(failed) != 2 || failed[0] != "zzz" || failed[1] != "bbb" {
		t.Fatalf("failed_checks must keep input order, got %v", failed)
	}
	blocking := res.BlockingGates
	if len(blocking) != 1 || blocking[0] != "complexity-gate" {
		t.Fatalf("blocking_gates wrong: %v", blocking)
	}
}

// ---- worker-outcome --------------------------------------------------------

func TestWorkerOutcomeContinueWinsOverNeedsRepair(t *testing.T) {
	res := mustWorkerOutcome(t, `{"role":"development","status":"CONTINUE","needs_repair":true}`)
	if res != workerContinue {
		t.Fatalf("CONTINUE must win over needs_repair, got %v", res)
	}
}

func TestWorkerOutcomeBlockedWinsOverNeedsRepair(t *testing.T) {
	res := mustWorkerOutcome(t, `{"role":"repair","status":"BLOCKED","needs_repair":true}`)
	if res != workerBlocked {
		t.Fatalf("BLOCKED must win over needs_repair, got %v", res)
	}
}

func TestWorkerOutcomeDevelopmentNeedsRepairRetries(t *testing.T) {
	for _, role := range []string{"development", "repair"} {
		res := mustWorkerOutcome(t, `{"role":"`+role+`","status":"PASS","needs_repair":true}`)
		if res != workerUnresolvedFindings {
			t.Fatalf("%s with needs_repair must be unresolved-findings, got %v", role, res)
		}
	}
}

func TestWorkerOutcomeOtherRoleNeedsRepairAccepted(t *testing.T) {
	for _, role := range []string{"qa-review", "complexity-gate", "product-review"} {
		res := mustWorkerOutcome(t, `{"role":"`+role+`","status":"FAIL","needs_repair":true}`)
		if res != workerAccept {
			t.Fatalf("%s with needs_repair must be accepted, got %v", role, res)
		}
	}
}

func TestWorkerOutcomeCleanResultAccepted(t *testing.T) {
	res := mustWorkerOutcome(t, `{"role":"development","status":"PASS","needs_repair":false}`)
	if res != workerAccept {
		t.Fatalf("clean result must be accepted, got %v", res)
	}
}

// ---- check-retention --------------------------------------------------------

func TestCheckRetentionPhaseOneEligibility(t *testing.T) {
	cases := []struct {
		name, payload, want string
	}{
		{"no old record", `{"binding":"b","old":null}`, "run"},
		{"binding mismatch", `{"binding":"b","old":{"binding":"other","status":"PASS","candidate":"c1"}}`, "run"},
		{"unsettled status", `{"binding":"b","old":{"binding":"b","status":"BLOCKED","candidate":"c1"}}`, "run"},
		{"pass eligible", `{"binding":"b","old":{"binding":"b","status":"PASS","candidate":"c1"}}`, "verify"},
		{"fail eligible", `{"binding":"b","old":{"binding":"b","status":"FAIL","candidate":"c1"}}`, "verify"},
	}
	for _, c := range cases {
		res := mustCheckRetention(t, c.payload)
		if res.Action != c.want {
			t.Fatalf("%s: want %v, got %v", c.name, c.want, res.Action)
		}
	}
}

func TestCheckRetentionRetainSameCandidateNoInheritance(t *testing.T) {
	res := mustCheckRetention(t, `{"binding":"b","old":{"binding":"b","status":"PASS","candidate":"c1"},
		"candidate":"c1","attempt":0,"evidence_valid":true}`)
	if res.Action != "retain" {
		t.Fatalf("valid evidence must retain, got %v", res.Action)
	}
	if res.InheritedFrom != "" {
		t.Fatalf("same candidate must not record inheritance, got %v", res.InheritedFrom)
	}
}

func TestCheckRetentionRetainMovedCandidateInherits(t *testing.T) {
	res := mustCheckRetention(t, `{"binding":"b","old":{"binding":"b","status":"FAIL","candidate":"old-head"},
		"candidate":"new-head","attempt":3,"evidence_valid":true}`)
	if res.Action != "retain" {
		t.Fatalf("valid evidence must retain, got %v", res.Action)
	}
	if res.InheritedFrom != "old-head" {
		t.Fatalf("moved candidate must record inheritance, got %v", res.InheritedFrom)
	}
}

func TestCheckRetentionInvalidEvidenceFirstAttemptFails(t *testing.T) {
	res := mustCheckRetention(t, `{"binding":"b","old":{"binding":"b","status":"PASS","candidate":"c1"},
		"candidate":"c1","attempt":0,"evidence_valid":false}`)
	if res.Action != "fail" {
		t.Fatalf("invalid evidence on attempt 0 must fail, got %v", res.Action)
	}
}

func TestCheckRetentionInvalidEvidenceRetryReruns(t *testing.T) {
	for _, attempt := range []string{"1", "2"} {
		res := mustCheckRetention(t, `{"binding":"b","old":{"binding":"b","status":"PASS","candidate":"c1"},
			"candidate":"c2","attempt":`+attempt+`,"evidence_valid":false}`)
		if res.Action != "run" {
			t.Fatalf("invalid evidence on attempt %s must rerun, got %v", attempt, res.Action)
		}
		if res.Reason != "check-evidence-invalid" {
			t.Fatalf("reason wrong: %v", res.Reason)
		}
	}
}

// ---- repair-allowance -------------------------------------------------------

func TestRepairAllowanceNullUsesSpecDefault(t *testing.T) {
	res := mustRepairAllowance(t, `{"round":0,"allowance":null,"repair_limit":2}`)
	if res.Allowance != 2 || res.AskUser {
		t.Fatalf("default allowance wrong: %v", res)
	}
}

func TestRepairAllowanceBoundaryAtEqualityAsks(t *testing.T) {
	res := mustRepairAllowance(t, `{"round":2,"allowance":null,"repair_limit":2}`)
	if res.Allowance != 2 || !res.AskUser {
		t.Fatalf("round == allowance must ask, got %v", res)
	}
}

func TestRepairAllowanceOverrideBeatsSpecLimit(t *testing.T) {
	res := mustRepairAllowance(t, `{"round":2,"allowance":3,"repair_limit":2}`)
	if res.Allowance != 3 || res.AskUser {
		t.Fatalf("explicit allowance must beat spec limit, got %v", res)
	}
	res = mustRepairAllowance(t, `{"round":3,"allowance":3,"repair_limit":2}`)
	if !res.AskUser {
		t.Fatalf("reached overridden allowance must ask, got %v", res)
	}
}

// ---- candidate-admission ----------------------------------------------------

func TestCandidateAdmissionFourCombinations(t *testing.T) {
	cases := []struct {
		payload, wantRefusal string
	}{
		{`{"development_needs_repair":true,"qa_review_needs_repair":true}`,
			"Unresolved development result cannot create a verification candidate"},
		{`{"development_needs_repair":true,"qa_review_needs_repair":false}`,
			"Unresolved development result cannot create a verification candidate"},
		{`{"development_needs_repair":false,"qa_review_needs_repair":true}`, "QA not approved"},
		{`{"development_needs_repair":false,"qa_review_needs_repair":false}`, ""},
	}
	for _, c := range cases {
		res := mustCandidateAdmission(t, c.payload)
		if c.wantRefusal == "" {
			if !res.Admitted {
				t.Fatalf("clean records must admit, got %v", res)
			}
			continue
		}
		if res.Admitted {
			t.Fatalf("expected refusal, got %v", res)
		}
		if res.Kind != "binding" {
			t.Fatalf("refusal kind wrong: %v", res.Kind)
		}
		if res.Refusal != c.wantRefusal {
			t.Fatalf("refusal wording wrong: got %q want %q", res.Refusal, c.wantRefusal)
		}
	}
}
