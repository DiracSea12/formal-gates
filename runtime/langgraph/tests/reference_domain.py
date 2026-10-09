"""Independent test oracle for harness domain judgments.

Production code must not import this module. Route selection, worker outcome,
check retention, repair allowance and candidate admission are compared by
branch order, decision values and refusal wording."""


def evaluate_route(checks, gates):
    """Flow.evaluate over {checks:[{id,status}], gates:[{gate,result}]}, spec order in, spec order out."""
    failed = [c["id"] for c in checks if c["status"] != "PASS"]
    findings = []
    for g in gates:
        r = g["result"]
        if r["status"] != "PASS" or any(f["severity"] != "P3" for f in r["findings"]):
            findings.append(g["gate"])
    route = "repair" if failed or findings else "seal"
    return {"failed_checks": failed, "blocking_gates": findings, "route": route}


def worker_outcome(role, status, needs_repair):
    """Flow.invoke_worker's branch order: CONTINUE, then BLOCKED, then the
    development/repair needs_repair retry; everything else is accepted."""
    if status == "CONTINUE":
        return {"action": "continue"}
    if status == "BLOCKED":
        return {"action": "blocked"}
    if role in ('development', 'repair') and needs_repair:
        return {"action": "unresolved-findings"}
    return {"action": "accept"}


def check_retention(binding, old, candidate=None, attempt=None, evidence_valid=None):
    """Flow.one_check's retention rule in two phases: eligibility (evidence_valid
    not yet known) and the verdict after verify_evidence decided."""
    if evidence_valid is None:
        if old and old["binding"] == binding and old["status"] in ("PASS", "FAIL"):
            return {"action": "verify"}
        return {"action": "run"}
    if evidence_valid:
        return {"action": "retain",
                "inherited_from": old["candidate"] if old["candidate"] != candidate else ""}
    if attempt == 0:
        return {"action": "fail"}
    return {"action": "run", "reason": "check-evidence-invalid"}


def repair_allowance(round, allowance, repair_limit):
    """Flow.repair's boundary: allowance = state.get("round_allowance",
    spec["repair_limit"]); the user is asked once round >= allowance."""
    effective = repair_limit if allowance is None else allowance
    return {"allowance": effective, "ask_user": round >= effective}


def candidate_admission(development_needs_repair, qa_review_needs_repair):
    """Flow.candidate's refusals, development first, wording verbatim."""
    if development_needs_repair:
        return {"ok": False, "kind": "binding",
                "refusal": 'Unresolved development result cannot create a verification candidate'}
    if qa_review_needs_repair:
        return {"ok": False, "kind": "binding", "refusal": "QA not approved"}
    return {"ok": True}
