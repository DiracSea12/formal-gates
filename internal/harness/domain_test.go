package harness

import (
	"testing"
)

// mustEvaluateRoute / mustWorkerOutcome / mustCheckRetention / mustRepairAllowance /
// mustCandidateAdmission decode a JSON payload the way the bridge would
// (StrictDecode, unknown fields rejected) and run the judgement under test.
func mustEvaluateRoute(t *testing.T, payload string) map[string]any {
	t.Helper()
	var q evaluateRouteInput
	if err := StrictDecode([]byte(payload), &q); err != nil {
		t.Fatalf("StrictDecode payload: %v", err)
	}
	return evaluateRoute(q)
}
func mustWorkerOutcome(t *testing.T, payload string) map[string]any {
	t.Helper()
	var q workerOutcomeInput
	if err := StrictDecode([]byte(payload), &q); err != nil {
		t.Fatalf("StrictDecode payload: %v", err)
	}
	return workerOutcome(q)
}
func mustCheckRetention(t *testing.T, payload string) map[string]any {
	t.Helper()
	var q checkRetentionInput
	if err := StrictDecode([]byte(payload), &q); err != nil {
		t.Fatalf("StrictDecode payload: %v", err)
	}
	return checkRetention(q)
}
func mustRepairAllowance(t *testing.T, payload string) map[string]any {
	t.Helper()
	var q repairAllowanceInput
	if err := StrictDecode([]byte(payload), &q); err != nil {
		t.Fatalf("StrictDecode payload: %v", err)
	}
	return repairAllowance(q)
}
func mustCandidateAdmission(t *testing.T, payload string) map[string]any {
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
	if res["route"] != "seal" {
		t.Fatalf("empty collections must seal, got %v", res["route"])
	}
	if res["failed_checks"].([]string) == nil || len(res["failed_checks"].([]string)) != 0 {
		t.Fatalf("failed_checks must be an empty list, got %v", res["failed_checks"])
	}
	if len(res["blocking_gates"].([]string)) != 0 {
		t.Fatalf("blocking_gates must be empty, got %v", res["blocking_gates"])
	}
}

func TestEvaluateRouteOnlyP3IsNotBlocking(t *testing.T) {
	res := mustEvaluateRoute(t, `{"checks":[{"id":"c1","status":"PASS"}],
		"gates":[{"gate":"complexity-gate","result":{"status":"PASS","summary":"s","check_ids":[],
		"findings":[{"id":"f1","severity":"P3","message":"m","paths":[]}]}}]}`)
	if res["route"] != "seal" {
		t.Fatalf("PASS with only P3 must seal, got %v", res["route"])
	}
	if len(res["blocking_gates"].([]string)) != 0 {
		t.Fatalf("P3-only findings must not block, got %v", res["blocking_gates"])
	}
}

func TestEvaluateRoutePassWithP2Blocks(t *testing.T) {
	res := mustEvaluateRoute(t, `{"checks":[{"id":"c1","status":"PASS"}],
		"gates":[{"gate":"complexity-gate","result":{"status":"PASS","summary":"s","check_ids":[],
		"findings":[{"id":"f1","severity":"P3","message":"m","paths":[]},
		            {"id":"f2","severity":"P2","message":"m","paths":["a"]}]}}]}`)
	if res["route"] != "repair" {
		t.Fatalf("PASS with a P2 finding must repair, got %v", res["route"])
	}
	blocking := res["blocking_gates"].([]string)
	if len(blocking) != 1 || blocking[0] != "complexity-gate" {
		t.Fatalf("blocking gate wrong: %v", blocking)
	}
	if len(res["failed_checks"].([]string)) != 0 {
		t.Fatalf("no failed checks expected, got %v", res["failed_checks"])
	}
}

func TestEvaluateRouteFailedCheckAndOrderKept(t *testing.T) {
	res := mustEvaluateRoute(t, `{"checks":[{"id":"aaa","status":"PASS"},{"id":"zzz","status":"FAIL"},{"id":"bbb","status":"FAIL"}],
		"gates":[{"gate":"implementation-quality-gate","result":{"status":"PASS","summary":"s","check_ids":[],"findings":[]}},
		         {"gate":"complexity-gate","result":{"status":"FAIL","summary":"s","check_ids":[],
		          "findings":[{"id":"f1","severity":"P1","message":"m","paths":[]}]}}]}`)
	if res["route"] != "repair" {
		t.Fatalf("failed check + FAIL gate must repair, got %v", res["route"])
	}
	failed := res["failed_checks"].([]string)
	if len(failed) != 2 || failed[0] != "zzz" || failed[1] != "bbb" {
		t.Fatalf("failed_checks must keep input order, got %v", failed)
	}
	blocking := res["blocking_gates"].([]string)
	if len(blocking) != 1 || blocking[0] != "complexity-gate" {
		t.Fatalf("blocking_gates wrong: %v", blocking)
	}
}

// ---- worker-outcome --------------------------------------------------------

func TestWorkerOutcomeContinueWinsOverNeedsRepair(t *testing.T) {
	res := mustWorkerOutcome(t, `{"role":"development","status":"CONTINUE","needs_repair":true}`)
	if res["action"] != "continue" {
		t.Fatalf("CONTINUE must win over needs_repair, got %v", res["action"])
	}
}

func TestWorkerOutcomeBlockedWinsOverNeedsRepair(t *testing.T) {
	res := mustWorkerOutcome(t, `{"role":"repair","status":"BLOCKED","needs_repair":true}`)
	if res["action"] != "blocked" {
		t.Fatalf("BLOCKED must win over needs_repair, got %v", res["action"])
	}
}

func TestWorkerOutcomeDevelopmentNeedsRepairRetries(t *testing.T) {
	for _, role := range []string{"development", "repair"} {
		res := mustWorkerOutcome(t, `{"role":"`+role+`","status":"PASS","needs_repair":true}`)
		if res["action"] != "unresolved-findings" {
			t.Fatalf("%s with needs_repair must be unresolved-findings, got %v", role, res["action"])
		}
	}
}

func TestWorkerOutcomeOtherRoleNeedsRepairAccepted(t *testing.T) {
	for _, role := range []string{"qa-review", "complexity-gate", "product-review"} {
		res := mustWorkerOutcome(t, `{"role":"`+role+`","status":"FAIL","needs_repair":true}`)
		if res["action"] != "accept" {
			t.Fatalf("%s with needs_repair must be accepted, got %v", role, res["action"])
		}
	}
}

func TestWorkerOutcomeCleanResultAccepted(t *testing.T) {
	res := mustWorkerOutcome(t, `{"role":"development","status":"PASS","needs_repair":false}`)
	if res["action"] != "accept" {
		t.Fatalf("clean result must be accepted, got %v", res["action"])
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
		if res["action"] != c.want {
			t.Fatalf("%s: want %v, got %v", c.name, c.want, res["action"])
		}
	}
}

func TestCheckRetentionRetainSameCandidateNoInheritance(t *testing.T) {
	res := mustCheckRetention(t, `{"binding":"b","old":{"binding":"b","status":"PASS","candidate":"c1"},
		"candidate":"c1","attempt":0,"evidence_valid":true}`)
	if res["action"] != "retain" {
		t.Fatalf("valid evidence must retain, got %v", res["action"])
	}
	if res["inherited_from"] != "" {
		t.Fatalf("same candidate must not record inheritance, got %v", res["inherited_from"])
	}
}

func TestCheckRetentionRetainMovedCandidateInherits(t *testing.T) {
	res := mustCheckRetention(t, `{"binding":"b","old":{"binding":"b","status":"FAIL","candidate":"old-head"},
		"candidate":"new-head","attempt":3,"evidence_valid":true}`)
	if res["action"] != "retain" {
		t.Fatalf("valid evidence must retain, got %v", res["action"])
	}
	if res["inherited_from"] != "old-head" {
		t.Fatalf("moved candidate must record inheritance, got %v", res["inherited_from"])
	}
}

func TestCheckRetentionInvalidEvidenceFirstAttemptFails(t *testing.T) {
	res := mustCheckRetention(t, `{"binding":"b","old":{"binding":"b","status":"PASS","candidate":"c1"},
		"candidate":"c1","attempt":0,"evidence_valid":false}`)
	if res["action"] != "fail" {
		t.Fatalf("invalid evidence on attempt 0 must fail, got %v", res["action"])
	}
}

func TestCheckRetentionInvalidEvidenceRetryReruns(t *testing.T) {
	for _, attempt := range []string{"1", "2"} {
		res := mustCheckRetention(t, `{"binding":"b","old":{"binding":"b","status":"PASS","candidate":"c1"},
			"candidate":"c2","attempt":`+attempt+`,"evidence_valid":false}`)
		if res["action"] != "run" {
			t.Fatalf("invalid evidence on attempt %s must rerun, got %v", attempt, res["action"])
		}
		if res["reason"] != "check-evidence-invalid" {
			t.Fatalf("reason wrong: %v", res["reason"])
		}
	}
}

// ---- repair-allowance -------------------------------------------------------

func TestRepairAllowanceNullUsesSpecDefault(t *testing.T) {
	res := mustRepairAllowance(t, `{"round":0,"allowance":null,"repair_limit":2}`)
	if res["allowance"] != 2 || res["ask_user"] != false {
		t.Fatalf("default allowance wrong: %v", res)
	}
}

func TestRepairAllowanceBoundaryAtEqualityAsks(t *testing.T) {
	res := mustRepairAllowance(t, `{"round":2,"allowance":null,"repair_limit":2}`)
	if res["allowance"] != 2 || res["ask_user"] != true {
		t.Fatalf("round == allowance must ask, got %v", res)
	}
}

func TestRepairAllowanceOverrideBeatsSpecLimit(t *testing.T) {
	res := mustRepairAllowance(t, `{"round":2,"allowance":3,"repair_limit":2}`)
	if res["allowance"] != 3 || res["ask_user"] != false {
		t.Fatalf("explicit allowance must beat spec limit, got %v", res)
	}
	res = mustRepairAllowance(t, `{"round":3,"allowance":3,"repair_limit":2}`)
	if res["ask_user"] != true {
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
			if res["ok"] != true {
				t.Fatalf("clean records must admit, got %v", res)
			}
			continue
		}
		if res["ok"] != false {
			t.Fatalf("expected refusal, got %v", res)
		}
		if res["kind"] != "binding" {
			t.Fatalf("refusal kind wrong: %v", res["kind"])
		}
		if res["refusal"] != c.wantRefusal {
			t.Fatalf("refusal wording wrong: got %q want %q", res["refusal"], c.wantRefusal)
		}
	}
}
