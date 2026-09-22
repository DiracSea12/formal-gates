package harness

// The five domain judgements below are plain functions over their
// payloads: the caller passes checks/gates in
// spec order and the returned ids and gate names keep that order, so Python
// only dispatches on the answer and never re-derives a condition. No judgement
// here validates its inputs — they are projections of records Go already
// validated (validate-worker, one_check results) — and NeedsRepair stays the
// single owner of the "gate result is not acceptable" condition.

// evaluateRouteInput summarizes the verification records for the route
// judgement. Result reuses WorkerResult, so the blocking condition is exactly
// NeedsRepair rather than a second copy of it.
type evaluateRouteInput struct {
	Checks []routeCheck `json:"checks"`
	Gates  []routeGate  `json:"gates"`
}
type routeCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}
type routeGate struct {
	Gate   string       `json:"gate"`
	Result WorkerResult `json:"result"`
}

// evaluateRoute decides repair-or-seal: any check that is not PASS or any gate
// result that NeedsRepair routes to repair. The id/gate-name projections let
// Python pick the full records without shipping them back and forth.
func evaluateRoute(q evaluateRouteInput) map[string]any {
	failed := []string{}
	for _, c := range q.Checks {
		if c.Status != "PASS" {
			failed = append(failed, c.ID)
		}
	}
	blocking := []string{}
	for _, g := range q.Gates {
		if NeedsRepair(g.Result) {
			blocking = append(blocking, g.Gate)
		}
	}
	route := "seal"
	if len(failed) > 0 || len(blocking) > 0 {
		route = "repair"
	}
	return map[string]any{"failed_checks": failed, "blocking_gates": blocking, "route": route}
}

// workerOutcomeInput is the payload for the worker-outcome judgement: what the
// invoke_worker loop should do with one validated worker result.
type workerOutcomeInput struct {
	Role        string `json:"role"`
	Status      string `json:"status"`
	NeedsRepair bool   `json:"needs_repair"`
}

// workerOutcome keeps the reference branch order. CONTINUE and BLOCKED are
// status facts and win over the needs_repair retry, which exists only for
// development/repair because their unresolved findings cannot be dropped.
func workerOutcome(q workerOutcomeInput) map[string]any {
	action := "accept"
	switch {
	case q.Status == "CONTINUE":
		action = "continue"
	case q.Status == "BLOCKED":
		action = "blocked"
	case (q.Role == "development" || q.Role == "repair") && q.NeedsRepair:
		action = "unresolved-findings"
	}
	return map[string]any{"action": action}
}

// checkRetentionInput serves both retention phases over one operation. Phase 1
// (evidence_valid absent) judges whether the old record is even eligible for
// re-verification; phase 2 (evidence_valid present) turns the outcome of
// verify_evidence into retain/fail/rerun. `old` is the three-field projection
// of the previous record, null when there is none.
type checkRetentionInput struct {
	Binding       string        `json:"binding"`
	Old           *retentionOld `json:"old"`
	Candidate     string        `json:"candidate"`
	Attempt       int           `json:"attempt"`
	EvidenceValid *bool         `json:"evidence_valid"`
}
type retentionOld struct {
	Binding   string `json:"binding"`
	Status    string `json:"status"`
	Candidate string `json:"candidate"`
}

// checkRetention mirrors the incremental-check rule: a matching binding with a
// settled PASS/FAIL outcome is worth re-verifying; evidence that still holds is
// retained (recording the previous candidate when the candidate moved), invalid
// evidence on the first attempt is the original failure, and only an explicit
// retry reaches a new run.
func checkRetention(q checkRetentionInput) map[string]any {
	if q.EvidenceValid == nil {
		if q.Old != nil && q.Old.Binding == q.Binding && (q.Old.Status == "PASS" || q.Old.Status == "FAIL") {
			return map[string]any{"action": "verify"}
		}
		return map[string]any{"action": "run"}
	}
	if *q.EvidenceValid {
		inherited := ""
		if q.Old != nil && q.Old.Candidate != q.Candidate {
			inherited = q.Old.Candidate
		}
		return map[string]any{"action": "retain", "inherited_from": inherited}
	}
	if q.Attempt == 0 {
		return map[string]any{"action": "fail"}
	}
	return map[string]any{"action": "run", "reason": "check-evidence-invalid"}
}

// repairAllowanceInput is the payload for the repair-limit judgement. A null
// allowance means the spec default (repair_limit); the increment after the user
// continues is a flow decision and stays in Python.
type repairAllowanceInput struct {
	Round       int  `json:"round"`
	Allowance   *int `json:"allowance"`
	RepairLimit int  `json:"repair_limit"`
}

// repairAllowance reports the effective allowance and whether the round has
// reached it. The boundary is >=: a round equal to the allowance already asks.
func repairAllowance(q repairAllowanceInput) map[string]any {
	allowance := q.RepairLimit
	if q.Allowance != nil {
		allowance = *q.Allowance
	}
	return map[string]any{"allowance": allowance, "ask_user": q.Round >= allowance}
}

// candidateAdmissionInput carries the two needs_repair flags the candidate node
// refuses on.
type candidateAdmissionInput struct {
	DevelopmentNeedsRepair bool `json:"development_needs_repair"`
	QAReviewNeedsRepair    bool `json:"qa_review_needs_repair"`
}

// candidateAdmission refuses to snapshot a candidate out of unresolved
// records. The refusal is a typed business outcome inside the value
// so Python's refuse() raises the HarnessError family;
// development is checked first, so its wording wins when both refuse.
func candidateAdmission(q candidateAdmissionInput) map[string]any {
	if q.DevelopmentNeedsRepair {
		return map[string]any{"ok": false, "kind": "binding",
			"refusal": "Unresolved development result cannot create a verification candidate"}
	}
	if q.QAReviewNeedsRepair {
		return map[string]any{"ok": false, "kind": "binding", "refusal": "QA not approved"}
	}
	return map[string]any{"ok": true}
}
