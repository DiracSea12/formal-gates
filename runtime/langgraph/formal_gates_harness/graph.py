"""Replaceable LangGraph execution layer. Go owns contracts and qualification."""
from __future__ import annotations
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
from typing import Annotated, TypedDict
from langgraph.graph import StateGraph, START, END
from langgraph.types import interrupt
from .infra import HarnessError, RuntimeBlocked, SessionUnavailable, Store, atomic, digest, git, refuse
from .workers import Workers
from .feedback import evidence_feedback

def merge(left: dict | None, right: dict | None) -> dict:
    return {**(left or {}), **(right or {})}
def retain_reason(left: str | None, right: str | None) -> str:
    return left or right or ""
class State(TypedDict, total=False):
    run_id: str
    round: int
    round_allowance: int
    approval: str
    plan_approval: str
    product: str
    technical: str
    development: str
    qa_design: str
    qa_review: str
    candidate: str
    checks: Annotated[dict, merge]
    gates: Annotated[dict, merge]
    sessions: Annotated[dict, merge]
    stop_reason: Annotated[str, retain_reason]
    feedback: str
    route: str
    terminal: str
class StopRun(Exception):
    pass

class Flow:
    def __init__(self, store: Store, package: Path):
        self.store, self.package = store, package
        self.spec, self.revision = store.manifest["spec"], store.manifest["revision"]
        self.workers = Workers(store, package)
    def ask(self, kind: str, choices: list[str], **body) -> str:
        value = interrupt({"kind": kind, "revision": self.revision, "choices": choices, **body})
        if not isinstance(value, dict) or set(value) != {"decision", "revision"}:
            raise HarnessError("invalid user answer shape")
        if value["revision"] != self.revision or value["decision"] not in choices:
            raise HarnessError("stale revision or unsupported decision")
        receipt = self.store.put({"kind": kind, "answer": value, "request": body})
        self.store.event("user-decision", kind=kind, decision=value["decision"], evidence=receipt)
        if value["decision"] == "abort": raise StopRun("User declined or aborted at a decision boundary")
        return value["decision"]
    def guard(self, name, fn):
        def wrapped(state: State):
            if state.get("stop_reason"): return {}
            self.store.event("stage-start", stage=name, round=state.get("round", 0))
            try: result = fn(state)
            except StopRun as exc: result = {"stop_reason": str(exc)}
            self.store.event("stage-complete", stage=name, round=state.get("round", 0))
            return result
        return wrapped
    def baseline(self) -> Path:
        return self.store.clone(self.spec["project"], "baseline", self.spec["base"])
    def development_workspace(self) -> Path:
        return self.store.clone(self.spec["project"], "development", self.spec["base"])
    def invoke_worker(self, role: str, state: State, workspace: Path, feedback=None, session="") -> tuple[str, dict]:
        attempt = 0; continuation = 0; restart_session = False
        initial_feedback = feedback
        native_store = ''
        if session and state.get('development'):
            original = self.store.get(state['development'])['source']
            if original.get('identity') == session:
                native_store = original.get('native_store', '')
        while True:
            key = {"run_id": state["run_id"], "round": state.get("round", 0),
                   "candidate": state.get("candidate", self.spec["base"]), "continuation": continuation}
            try:
                options = {'restart_session': True} if restart_session else {}
                if native_store: options['native_store'] = native_store
                result = self.workers.run(role, workspace, key, feedback, session, attempt, **options)
            except SessionUnavailable as exc:
                # The current identity cannot recover; don't offer a known-useless retry.
                decision = self.ask('runtime', ['restart-session', 'abort'], role=role, attempt=attempt,
                                    reason=str(exc), error_kind='SESSION_UNAVAILABLE', identity=exc.identity)
                restart_session = decision == 'restart-session'
                attempt += 1; continue
            except (RuntimeBlocked, HarnessError) as exc:
                decision = self.ask("runtime", ["retry", "restart-session", "abort"], role=role, attempt=attempt, reason=str(exc))
                restart_session = decision == 'restart-session'
                attempt += 1; continue
            session = result["source"].get("identity", session)
            native_store = result['source'].get('native_store', '')
            restart_session = False
            action = self.store.bridge.call("worker-outcome", {"role": role, "status": result["result"]["status"],
                                                               "needs_repair": result["needs_repair"]})["action"]
            if action == "continue":
                continuation += 1
                feedback = {'continuation': evidence_feedback(self.store, self.store.put(result), 'unfinished-work'),
                            'initial_feedback': initial_feedback}
                continue
            if action == "blocked":
                self.ask("worker-blocked", ["retry", "abort"], role=role,
                         reason=result["result"]["summary"], evidence=self.store.put(result))
                attempt += 1; continue
            if action == "unresolved-findings":
                self.ask('development-findings', ['retry', 'abort'], role=role,
                         evidence=self.store.put(result), reason='Unresolved development findings cannot be discarded')
                feedback = {'prior_result': evidence_feedback(self.store, self.store.put(result), 'unresolved-findings'),
                            'initial_feedback': initial_feedback}
                attempt += 1
                continue
            return self.store.put(result), result
    def approval(self, state: State):
        self.ask("requirement", ["approve", "abort"], requirement=str(self.store.directory / "requirement.md"),
                 checks=self.spec["checks"], gates=self.spec["gates"], project=self.spec["project"], base=self.spec["base"])
        return {"approval": self.revision}
    def pre_review(self, role: str, field: str, state: State):
        feedback = None
        index = 0
        while True:
            ref, result = self.invoke_worker(role, {**state, "round": index}, self.baseline(), feedback)
            if not result["needs_repair"]: return {field: ref}
            self.ask("pre-review", ["retry", "abort"], role=role, evidence=ref,
                     reason="Mandatory findings remain. This immutable request cannot change in place; revise it in a new explicitly started run when needed.")
            feedback = evidence_feedback(self.store, ref, "prior-review-findings"); index += 1
    def plan_approval(self, state: State):
        self.ask("plan", ["approve", "abort"], product=state["product"], technical=state["technical"],
                 artifacts=str(self.store.directory / "artifacts"), checks=self.spec["checks"],
                 reason="Approve the reviewed requirement/plan and exact check commands before development")
        return {"plan_approval": self.revision}
    def develop(self, state: State):
        feedback = {"technical_review": evidence_feedback(self.store, state["technical"], "reviewed-technical-plan")}
        ref, result = self.invoke_worker("development", state, self.development_workspace(), feedback)
        return {"development": ref, "sessions": {"development": result["source"].get("identity", "")}}
    def design(self, state: State):
        ref, _ = self.invoke_worker("qa-design", state, self.baseline())
        return {"qa_design": ref}
    def qa_review(self, state: State):
        design_ref = state["qa_design"]; revision = 0
        while True:
            design = self.store.get(design_ref)
            ref, result = self.invoke_worker("qa-review", {**state, "round": revision}, self.baseline(), {"design": evidence_feedback(self.store, design_ref, "qa-design")})
            if not result["needs_repair"] and not design["needs_repair"]:
                return {"qa_design": design_ref, "qa_review": ref}
            self.ask("qa-design", ["retry", "abort"], role="qa-review", evidence=ref,
                     reason="Approved checks or coverage are insufficient. Final verification is not allowed.")
            revision += 1
            design_ref, _ = self.invoke_worker("qa-design", {**state, "round": revision}, self.baseline(),
                                               {"review": evidence_feedback(self.store, ref, "qa-review-findings"), "prior_design": evidence_feedback(self.store, design_ref, "prior-qa-design")})
    def candidate(self, state: State):
        refuse(self.store.bridge.call("candidate-admission", {
            "development_needs_repair": self.store.get(state['development'])['needs_repair'],
            "qa_review_needs_repair": self.store.get(state["qa_review"])["needs_repair"]}))
        op = digest({"revision": self.revision, "round": state.get("round", 0), "development": state["development"]})
        result = self.store.bridge.call("snapshot", {"root": str(self.development_workspace()), "operation_id": op})
        if state.get("round", 0) and result["candidate"] == state.get("candidate"):
            self.store.event("repair-no-change", candidate=result["candidate"])
        return {"candidate": result["candidate"]}
    def one_check(self, check: dict, state: State, attempt: int) -> dict:
        root = self.development_workspace()
        binding = self.store.bridge.call("binding", {"root": str(root), "candidate": state["candidate"],
                                                     "revision": self.revision, "check": check})["binding"]
        old_ref = state.get("checks", {}).get(check["id"])
        old = self.store.get(old_ref) if old_ref else None
        # Two bridge phases over one operation: eligibility first, then the
        # verdict once verify_evidence has decided, so the branch condition
        # lives only in Go. The original exception is re-raised on attempt 0.
        snapshot = None if old is None else {"binding": old["binding"], "status": old["status"],
                                             "candidate": old["candidate"]}
        retention = self.store.bridge.call("check-retention", {"binding": binding, "old": snapshot})
        if retention["action"] == "verify":
            exc = None
            try:
                self.store.verify_evidence(old["evidence"])
            except (HarnessError, OSError) as caught:
                exc = caught
            retention = self.store.bridge.call("check-retention", {"binding": binding, "old": snapshot,
                                                                   "candidate": state["candidate"], "attempt": attempt,
                                                                   "evidence_valid": exc is None})
            if retention["action"] == "fail":
                raise exc
            if retention["action"] == "run":
                # Only an explicit check-runtime retry reaches a new attempt.
                self.store.event('check-evidence-invalid', check=check['id'],
                                 previous=old_ref, reason=str(exc), retry_attempt=attempt)
        if retention["action"] == "retain":
            retained = {**old, "candidate": state["candidate"]}
            if retention["inherited_from"]: retained["inherited_from"] = retention["inherited_from"]
            self.store.event("check-retained", check=check["id"], status=old["status"], binding=binding)
            return retained
        operation = digest({"kind": "check", "candidate": state["candidate"], "check": check, "binding": binding, "attempt": attempt})
        workspace = self.store.clone(root, "check-" + operation[:24], state["candidate"])
        cache = workspace / ".formal-gates-cache"; cache.mkdir(exist_ok=True)
        environment = {"PYTHONDONTWRITEBYTECODE": "1", "GOCACHE": str(cache / "go-build"),
                       "GOTMPDIR": str(cache / "tmp"), "TMPDIR": str(cache / "tmp")}
        (cache / "tmp").mkdir(exist_ok=True)
        argv = check["argv"]
        # Execute the exact approved argv as the current user. The disposable
        # clone isolates candidate changes, not OS permissions or network access.
        result = self.store.job(operation, {"kind": "check", "argv": argv, "cwd": str(workspace),
                                           "stdin": "", "environment": environment})
        if result["error"] or result["exit_code"] < 0 or result["exit_code"] in (126, 127, 130):
            raise RuntimeBlocked(f"check could not execute: {result}; inspect evidence")
        if git(workspace, "rev-parse", "HEAD") != state["candidate"] or git(workspace, "status", "--porcelain", "--untracked-files=no"):
            raise RuntimeBlocked("verification changed tracked candidate files or Git HEAD")
        evidence = self.store.put({"check": check, "process": result, "candidate": state["candidate"], "binding": binding})
        self.store.event("check-complete", check=check["id"], exit_code=result["exit_code"], evidence=evidence)
        return {"id": check["id"], "candidate": state["candidate"], "binding": binding,
                "status": "PASS" if result["exit_code"] == 0 else "FAIL", "exit_code": result["exit_code"], "evidence": evidence}
    def checks(self, state: State):
        attempt = 0
        while True:
            results = {}; errors = []
            with ThreadPoolExecutor(max_workers=min(4, len(self.spec["checks"]))) as pool:
                work = {pool.submit(self.one_check, c, state, attempt): c for c in self.spec["checks"]}
                for future in as_completed(work):
                    try:
                        result = future.result(); results[result["id"]] = self.store.put(result)
                    except (RuntimeBlocked, HarnessError) as exc: errors.append(str(exc))
            if not errors: return {"checks": results}
            state = {**state, "checks": {**state.get("checks", {}), **results}}
            self.ask("check-runtime", ["retry", "abort"], reasons=errors)
            attempt += 1
    def gate(self, gate: str, state: State):
        workspace = self.store.clone(self.development_workspace(), gate + "-" + state["candidate"][:16], state["candidate"])
        ref, result = self.invoke_worker(gate, state, workspace)
        if git(workspace, "rev-parse", "HEAD") != state["candidate"] or git(workspace, "status", "--porcelain", "--untracked-files=no"):
            raise RuntimeBlocked("read-only gate changed the candidate")
        record = {"id": gate, "candidate": state["candidate"], "revision": self.revision,
                  "evidence": ref, "result": result["result"]}
        return {"gates": {gate: self.store.put(record)}}
    def evaluate(self, state: State):
        checks = [self.store.get(state["checks"][c["id"]]) for c in self.spec["checks"]]
        gate_results = {g: self.store.get(state["gates"][g])["result"] for g in self.spec["gates"]}
        verdict = self.store.bridge.call("evaluate-route", {
            "checks": [{"id": c["id"], "status": r["status"]} for c, r in zip(self.spec["checks"], checks)],
            "gates": [{"gate": g, "result": gate_results[g]} for g in self.spec["gates"]]})
        if verdict["route"] == "repair":
            by_id = {r["id"]: r for r in checks}
            ref = self.store.put({"failed_checks": [by_id[i] for i in verdict["failed_checks"]],
                                  "gate_findings": [{"gate": g, "result": gate_results[g]} for g in verdict["blocking_gates"]],
                                  "candidate": state["candidate"], "revision": self.revision})
            return {"feedback": ref, "route": "repair"}
        return {"route": "seal"}
    def repair(self, state: State):
        verdict = self.store.bridge.call("repair-allowance", {"round": state.get("round", 0),
                                                              "allowance": state.get("round_allowance"),
                                                              "repair_limit": self.spec["repair_limit"]})
        allowance = verdict["allowance"]
        if verdict["ask_user"]:
            self.ask("repair-limit", ["continue", "abort"], reason="Configured repair rounds reached; continue authorizes one more round", feedback=state["feedback"])
            allowance += 1
        feedback = evidence_feedback(self.store, state["feedback"], "failed-verification")
        ref, result = self.invoke_worker("repair", state, self.development_workspace(), feedback,
                                         state.get("sessions", {}).get("development", ""))
        return {"development": ref, "round": state.get("round", 0) + 1, "round_allowance": allowance,
                "sessions": {"development": result["source"].get("identity", "")}}
    def seal(self, state: State):
        checks = [self.store.get(state["checks"][c["id"]]) for c in self.spec["checks"]]
        gates = [self.store.get(state["gates"][g]) for g in self.spec["gates"]]
        for r in checks: self.store.verify_evidence(r["evidence"])
        for r in gates: self.store.verify_evidence(r["evidence"])
        certificate = self.store.bridge.call("certify", {"spec": self.spec, "revision": self.revision,
                                                        "root": str(self.development_workspace()), "candidate": state["candidate"],
                                                        "checks": checks, "gates": gates})
        ref = self.store.put(certificate)
        atomic(self.store.directory / "harness-result.json", certificate)
        self.store.event("terminal", status=certificate["status"], evidence=ref, candidate=state["candidate"])
        return {"terminal": ref}
    def build(self, checkpointer):
        graph = StateGraph(State)
        graph.add_node("approval", self.guard("approval", self.approval))
        graph.add_node("product", self.guard("product", lambda s: self.pre_review("product-review", "product", s)))
        graph.add_node("technical", self.guard("technical", lambda s: self.pre_review("technical-review", "technical", s)))
        graph.add_node("plan", self.guard("plan", self.plan_approval))
        graph.add_node("development", self.guard("development", self.develop))
        qa = StateGraph(State)
        qa.add_node("design", self.guard("qa-design", self.design))
        qa.add_node("review", self.guard("qa-review", self.qa_review))
        qa.add_edge(START, "design"); qa.add_edge("design", "review"); qa.add_edge("review", END)
        graph.add_node("qa", qa.compile())
        graph.add_node("candidate", self.guard("candidate", self.candidate))
        graph.add_node("checks", self.guard("checks", self.checks))
        graph.add_node("evaluate", self.guard("evaluate", self.evaluate))
        graph.add_node("repair", self.guard("repair", self.repair))
        graph.add_node("seal", self.guard("seal", self.seal))
        graph.add_edge(START, "approval")
        for before, after in (("approval", "product"), ("product", "technical"), ("technical", "plan")):
            graph.add_conditional_edges(before, lambda s, after=after: END if s.get("stop_reason") else after, [after, END])
        graph.add_conditional_edges("plan", lambda s: END if s.get("stop_reason") else ["development", "qa"], [END, "development", "qa"])
        graph.add_edge(["development", "qa"], "candidate")
        graph.add_edge("candidate", "checks")
        verify_nodes = ["checks"]
        for gate in self.spec["gates"]:
            graph.add_node(gate, self.guard(gate, lambda s, gate=gate: self.gate(gate, s)))
            graph.add_edge("candidate", gate); verify_nodes.append(gate)
        graph.add_edge(verify_nodes, "evaluate")
        graph.add_conditional_edges("evaluate", lambda s: END if s.get("stop_reason") else s["route"], [END, "repair", "seal"])
        graph.add_edge("repair", "candidate"); graph.add_edge("seal", END)
        return graph.compile(checkpointer=checkpointer)
