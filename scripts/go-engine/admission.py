#!/usr/bin/env python3
"""Admission gate for the go-engine replacement units.

The plan (section 12.5) says a unit may only start when the previous unit's
record is present, complete, PASSing, and still describes the current source,
binary and dependency identities. It also says the frozen version must be
restarted and taken through a baseline smoke that enters by the public entry,
crosses a confirmation boundary and yields a verifiable result - `--help`, an
import or `doctor` are explicitly not substitutes.

This script is that check. It refuses on a missing record, an incomplete run, a
non-PASS result, raw evidence that is missing or altered, an unpopulated
runtime environment, a baseline that will not start, an identity that moved,
or a required native verification that is missing, failing, internally
inconsistent, or not bound to the recorded candidate's runtime identity.

Exit codes:
  0  admission granted
  1  admission refused (the reason is printed)
  2  the checker could not evaluate (missing record, unreadable input)
"""

from __future__ import annotations

import argparse
import hashlib
import json
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import runtime_check  # noqa: E402  (shared runtime verification)

# Identity that must be reproduced by the current tree for a record to still
# describe it. Each entry is a key used in identity.json.
IDENTITY_FILES = [
    "go.mod",
    "go.sum",
    "runtime/langgraph/requirements.lock",
    "runtime/langgraph/pyproject.toml",
    "prompts/reviewer-base.md",
    "scripts/harness",
]

# A record may only implicitly declare itself as bookkeeping when it lives in
# the canonical record directory.
RECORD_DIR = ".gates/results/"

# Units whose acceptance requires a real native run in addition to the common
# items (the unit's own record says so via native_verification.required; this
# map is the checker's independent knowledge, so a record that simply omits the
# section cannot wave the requirement through).
UNITS_REQUIRING_NATIVE_VERIFICATION = {"G2a"}


def native_verification_problem(unit: str, record: dict, root: Path) -> str:
    """'' when any required native verification is present, consistent and real.

    A hand-filled PASS field proves nothing: the named report is re-read from
    disk, its business result (passed/provider/roles) is checked, and the run's
    own manifest must bind the recorded candidate's runtime identity.
    """
    native = record.get("native_verification")
    required_by_unit = unit in UNITS_REQUIRING_NATIVE_VERIFICATION
    if not native:
        return "record carries no native verification section" if required_by_unit else ""
    if not (native.get("required") or required_by_unit):
        return ""

    def problem(text: str) -> str:
        return f"native verification: {text}"

    if native.get("status") != "PASS":
        return problem(f"status is {native.get('status')!r}")
    evidence_rel = (native.get("evidence_dir") or "").strip()
    run_rel = (native.get("run") or "").strip()
    report_rel = (native.get("report") or "").strip()
    if not (evidence_rel and run_rel and report_rel):
        return problem("must name evidence_dir, run and report")

    def resolve(rel: str) -> Path:
        path = Path(rel)
        return path if path.is_absolute() else root / path

    evidence_dir, run_dir = resolve(evidence_rel), resolve(run_rel)
    report_path = Path(report_rel)
    # The report lives inside the evidence directory it belongs to.
    report = report_path if report_path.is_absolute() else evidence_dir / report_path
    if not evidence_dir.is_dir():
        return problem(f"evidence directory is missing: {evidence_rel}")
    if not run_dir.is_dir():
        return problem(f"run directory is missing: {run_rel}")
    if not report.is_file():
        return problem(f"run report is missing: {report_rel}")
    try:
        findings = json.loads(report.read_text())
    except ValueError as exc:
        return problem(f"run report is not valid JSON: {exc}")
    if findings.get("passed") is not True:
        return problem(f"run report says passed={findings.get('passed')!r}")
    if findings.get("provider") != native.get("provider"):
        return problem(f"run report provider {findings.get('provider')!r} differs from the record's {native.get('provider')!r}")
    roles = [r for r in (findings.get("roles") or []) if isinstance(r, dict)]
    if not roles or any(r.get("status") != "PASS" for r in roles):
        return problem("run report has roles that are not PASS")
    recorded_roles = native.get("roles_completed")
    if recorded_roles is not None and recorded_roles != len(roles):
        return problem(f"run report has {len(roles)} roles, the record says {recorded_roles}")
    if str(resolve(str(findings.get("run") or ""))) != str(run_dir):
        return problem("run report points at a different run directory than the record")
    manifest_path = run_dir / "manifest.json"
    if not manifest_path.is_file():
        return problem(f"run manifest is missing: {manifest_path}")
    try:
        manifest = json.loads(manifest_path.read_text())
    except ValueError as exc:
        return problem(f"run manifest is not valid JSON: {exc}")
    recorded_identity = (record.get("candidate") or {}).get("runtime_identity")
    if manifest.get("runtime_identity") != recorded_identity:
        return problem(
            f"run binds runtime identity {str(manifest.get('runtime_identity'))[:12]}…, "
            f"not the recorded candidate's {str(recorded_identity)[:12]}…"
        )
    return ""


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as fh:
        for block in iter(lambda: fh.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def fail(message: str, code: int = 1) -> int:
    print(f"ADMISSION REFUSED: {message}")
    return code


def verify_evidence(attempt_dir: Path, summary: dict) -> str | None:
    """Recompute the raw-evidence manifest and bind it to the item results.

    Returns a refusal reason, or None when the evidence is complete and intact.
    """
    manifest_path = attempt_dir / "evidence.json"
    if not manifest_path.is_file():
        return "attempt carries no raw-evidence manifest (evidence.json)"
    try:
        manifest = json.loads(manifest_path.read_text())
    except ValueError as exc:
        return f"raw-evidence manifest is not valid JSON: {exc}"
    recorded = manifest.get("files")
    if not isinstance(recorded, dict) or not recorded:
        return "raw-evidence manifest lists no files"

    actual: dict[str, str] = {}
    for path in sorted(attempt_dir.rglob("*")):
        if not path.is_file() or path.name == "evidence.json":
            continue
        actual[path.relative_to(attempt_dir).as_posix()] = sha256_file(path)

    missing = sorted(set(recorded) - set(actual))
    if missing:
        return f"raw evidence referenced by the record is missing: {missing[:6]}{' …' if len(missing) > 6 else ''}"
    altered = sorted(name for name in recorded if actual.get(name) != recorded[name])
    if altered:
        return f"raw evidence was altered after the run: {altered[:6]}{' …' if len(altered) > 6 else ''}"
    unexpected = sorted(set(actual) - set(recorded))
    if unexpected:
        return f"attempt directory holds files the record does not cover: {unexpected[:6]}"

    # The summary's own pointers must resolve, and the recorded exit code must
    # match the exit file the command actually produced.
    for item in summary.get("items", []):
        for command in item.get("commands", []):
            name = command.get("name")
            for key in ("stdout", "stderr", "exit"):
                rel = command.get(key)
                if rel is None:
                    continue
                if rel not in recorded:
                    return f"item {item.get('id')} command {name} references {rel}, which the manifest does not cover"
                if not (attempt_dir / rel).is_file():
                    return f"item {item.get('id')} command {name} lost its {key} evidence ({rel})"
            exit_rel = command.get("exit")
            if exit_rel and (attempt_dir / exit_rel).is_file():
                recorded_code = (attempt_dir / exit_rel).read_text().strip()
                if recorded_code != str(command.get("exit_code")):
                    return (
                        f"item {item.get('id')} command {name} exit evidence says {recorded_code!r} "
                        f"but the summary says {command.get('exit_code')!r}"
                    )
    return None


def public_baseline_smoke(root: Path) -> dict:
    """Take the frozen version through the public entry to a verifiable result.

    This is deliberately a flow and not a `--help`/import/`doctor` probe: it must
    start the harness, cross the fixture approval boundary and reach the
    deterministic terminal state.
    """
    entry = root / "scripts" / "harness"
    if not entry.is_file():
        return {"ok": False, "detail": f"public entry is missing: {entry}"}
    proc = subprocess.run(
        [str(entry), "demo", "--approve-sample"],
        cwd=str(root), capture_output=True, text=True,
    )
    if proc.returncode != 0:
        tail = (proc.stderr or proc.stdout).strip().splitlines()[-4:]
        return {"ok": False, "detail": f"`scripts/harness demo --approve-sample` exited {proc.returncode}: {' / '.join(tail)}"}
    created = None
    for line in proc.stdout.splitlines():
        line = line.strip()
        if line.startswith("{") and "created_run" in line:
            created = json.loads(line)["created_run"]
            break
    if not created:
        return {"ok": False, "detail": "the baseline smoke reported no created run"}
    result_path = Path(created) / "harness-result.json"
    if not result_path.is_file():
        return {"ok": False, "detail": f"the baseline smoke produced no harness-result.json in {created}"}
    result = json.loads(result_path.read_text())
    if result.get("status") != "DEMO_COMPLETE":
        return {"ok": False, "detail": f"the baseline smoke ended at {result.get('status')!r}, want DEMO_COMPLETE"}
    failing = [
        c.get("id")
        for c in result.get("checks", [])
        if c.get("status") != "PASS" or c.get("exit_code") != 0
    ]
    if failing:
        return {"ok": False, "detail": f"the baseline smoke had non-PASS checks: {failing}"}
    failing_gates = [g.get("id") for g in result.get("gates", []) if g.get("result", {}).get("status") != "PASS"]
    if failing_gates:
        return {"ok": False, "detail": f"the baseline smoke had non-PASS gates: {failing_gates}"}
    return {"ok": True, "detail": f"{Path(created).name} reached DEMO_COMPLETE", "run": created}


def main() -> int:
    parser = argparse.ArgumentParser(description="Verify a go-engine acceptance record before starting the next unit")
    parser.add_argument("--record", required=True, help="path to the machine-readable acceptance record")
    parser.add_argument("--root", default=None, help="candidate worktree root (default: repository containing this script)")
    parser.add_argument("--next-unit", default=None, help="unit that admission is being requested for (reporting only)")
    args = parser.parse_args()

    root = Path(args.root).resolve() if args.root else Path(__file__).resolve().parents[2]
    record_path = Path(args.record).resolve()
    if not record_path.is_file():
        return fail(f"acceptance record is missing: {record_path}", 2)
    try:
        record = json.loads(record_path.read_text())
    except ValueError as exc:
        return fail(f"acceptance record is not valid JSON: {exc}", 2)

    unit = record.get("unit")
    if not unit:
        return fail("acceptance record has no unit id", 2)
    if record.get("overall") != "PASS":
        return fail(f"{unit}: recorded overall conclusion is {record.get('overall')!r}", 1)

    attempt = record.get("attempt")
    if not attempt:
        return fail(f"{unit}: record does not point at an attempt directory", 2)
    attempt_dir = Path(attempt)
    summary_path = attempt_dir / "summary.json"
    identity_path = attempt_dir / "identity.json"
    if not summary_path.is_file():
        return fail(f"{unit}: attempt summary is missing: {summary_path}", 1)
    if not identity_path.is_file():
        return fail(f"{unit}: attempt identity file is missing: {identity_path}", 1)

    recorded_digest = (record.get("attestation") or {}).get("summary_sha256")
    if not recorded_digest:
        return fail(f"{unit}: record carries no attestation digest for the attempt summary", 1)
    actual_digest = sha256_file(summary_path)
    if recorded_digest != actual_digest:
        return fail(f"{unit}: attempt summary changed after the record was written ({actual_digest[:12]} != {recorded_digest[:12]})", 1)

    summary = json.loads(summary_path.read_text())
    if summary.get("overall") != "PASS":
        return fail(f"{unit}: attempt summary conclusion is {summary.get('overall')!r}", 1)
    items = summary.get("items", [])
    expected = summary.get("expected_items", [])
    if not items:
        return fail(f"{unit}: attempt summary contains no item results", 1)
    if expected and [i["id"] for i in items] != expected:
        return fail(f"{unit}: attempt did not run every expected item (ran {[i['id'] for i in items]}, expected {expected})", 1)
    for item in items:
        if item.get("required", True) and item.get("status") != "PASS":
            return fail(f"{unit}: required item {item['id']} is {item.get('status')}", 1)

    evidence_problem = verify_evidence(attempt_dir, summary)
    if evidence_problem:
        return fail(f"{unit}: {evidence_problem}", 1)

    identity = json.loads(identity_path.read_text())
    candidate = record.get("candidate") or {}
    if not candidate:
        return fail(f"{unit}: record carries no candidate identity", 2)

    # The record and the attempt must agree about the candidate. Without this,
    # editing the record's commit alone would let a moved candidate through,
    # because the file/identity checks below read the attempt, not the record.
    for key, attempt_value in (
        ("commit", identity.get("commit")),
        ("branch", identity.get("branch")),
        ("runtime_identity", identity.get("runtime_identity")),
        ("driver_binary_sha256", identity.get("driver_binary_sha256")),
    ):
        if candidate.get(key) != attempt_value:
            return fail(
                f"{unit}: record and attempt disagree about {key} "
                f"({str(candidate.get(key))[:12]} != {str(attempt_value)[:12]})",
                1,
            )

    # -- current tree must still equal the recorded candidate -----------------
    def git(*a: str) -> str:
        return subprocess.run(["git", *a], cwd=str(root), capture_output=True, text=True).stdout.strip()

    if git("status", "--porcelain"):
        return fail(f"{unit}: candidate worktree is dirty; the record cannot describe uncommitted state", 1)
    current_commit = git("rev-parse", "HEAD")
    recorded_commit = candidate.get("commit")
    if not recorded_commit:
        return fail(f"{unit}: record does not pin the candidate commit", 2)
    if current_commit != recorded_commit:
        # The record is bookkeeping committed on top of the candidate it
        # describes, so HEAD may be ahead by exactly the declared bookkeeping
        # files and nothing else.
        ancestor = subprocess.run(
            ["git", "merge-base", "--is-ancestor", recorded_commit, current_commit],
            cwd=str(root), capture_output=True, text=True,
        )
        if ancestor.returncode != 0:
            return fail(f"{unit}: candidate moved (HEAD {current_commit[:12]} does not descend from recorded {recorded_commit[:12]})", 1)
        changed = [p for p in git("diff", "--name-only", f"{recorded_commit}..{current_commit}").splitlines() if p.strip()]
        record_rel = record_path.relative_to(root).as_posix() if record_path.is_relative_to(root) else str(record_path)
        # Only the canonical record location may declare itself implicitly.
        # Otherwise a record placed anywhere would auto-allow its own path.
        declared = set()
        if record_rel.startswith(RECORD_DIR):
            declared.add(record_rel)
        for entry in record.get("bookkeeping") or []:
            if not isinstance(entry, str) or entry.startswith("/") or ".." in entry.split("/"):
                return fail(f"{unit}: record declares an unusable bookkeeping path: {entry!r}", 2)
            declared.add(entry)
        if not declared:
            return fail(f"{unit}: record declares no bookkeeping paths, yet HEAD is ahead of the candidate", 1)
        unexpected = [p for p in changed if p not in declared]
        if unexpected:
            return fail(f"{unit}: candidate moved after the record was written (changed: {unexpected}; bookkeeping may only be {sorted(declared)})", 1)
        print(f"note: HEAD is ahead of the recorded candidate by bookkeeping only ({sorted(declared)})")
    current_branch = git("rev-parse", "--abbrev-ref", "HEAD")
    if candidate.get("branch") and current_branch != candidate.get("branch"):
        return fail(f"{unit}: candidate branch changed ({current_branch} != {candidate.get('branch')})", 1)

    for rel in IDENTITY_FILES:
        expected_digest = (identity.get("files") or {}).get(rel)
        if not expected_digest:
            return fail(f"{unit}: record does not pin {rel}", 1)
        path = root / rel
        if not path.is_file():
            return fail(f"{unit}: {rel} is missing from the candidate", 1)
        digest = sha256_file(path)
        if digest != expected_digest:
            return fail(f"{unit}: {rel} changed since the record was written", 1)

    binary = root / ".harness" / "bin" / "formal-gates"
    if not binary.is_file():
        return fail(f"{unit}: driver binary is missing: {binary}", 1)
    if sha256_file(binary) != identity.get("driver_binary_sha256"):
        return fail(f"{unit}: driver binary changed since the record was written", 1)

    # -- the runtime must actually work, not merely be pinned -----------------
    venv_python = root / ".harness" / "venv" / "bin" / "python"
    if not venv_python.is_file():
        return fail(f"{unit}: candidate runtime is missing: {venv_python}", 1)
    # "Can this environment run the harness at all" is checked before "is it the
    # same interpreter", so an unpopulated environment reports the actionable
    # reason instead of a path difference.
    runtime = runtime_check.verify(root, venv_python)
    if not runtime.get("ok"):
        return fail(
            f"{unit}: installed dependencies do not match {runtime_check.LOCK_RELATIVE_PATH} "
            f"(missing={runtime.get('missing')}, mismatched={runtime.get('mismatched')}, errored={runtime.get('errored')})",
            1,
        )
    interpreter = runtime_check.interpreter_identity(venv_python)
    if interpreter.get("error"):
        return fail(f"{unit}: candidate interpreter is unusable: {interpreter['error']}", 1)
    recorded_interpreter = identity.get("interpreter") or {}
    if recorded_interpreter.get("executable") and interpreter.get("executable") != recorded_interpreter.get("executable"):
        return fail(
            f"{unit}: interpreter changed ({interpreter.get('executable')} != {recorded_interpreter.get('executable')})",
            1,
        )

    probe = (
        "import sys,json;sys.path.insert(0,str(__import__('pathlib').Path.cwd()/'runtime'/'langgraph'));"
        "from pathlib import Path;from formal_gates_harness.infra import runtime_identity;"
        f"print(runtime_identity(Path.cwd(), {str(binary)!r}))"
    )
    proc = subprocess.run([str(venv_python), "-c", probe], cwd=str(root), capture_output=True, text=True)
    if proc.returncode != 0:
        return fail(f"{unit}: could not recompute runtime identity: {proc.stderr.strip().splitlines()[-1] if proc.stderr.strip() else 'exit ' + str(proc.returncode)}", 1)
    current_runtime = proc.stdout.strip().splitlines()[-1]
    if current_runtime != identity.get("runtime_identity"):
        return fail(f"{unit}: runtime identity changed ({current_runtime[:12]} != {str(identity.get('runtime_identity'))[:12]})", 1)

    # -- and it must start through the public entry ---------------------------
    smoke = public_baseline_smoke(root)
    if not smoke["ok"]:
        return fail(f"{unit}: public baseline smoke failed: {smoke['detail']}", 1)

    # -- a unit that requires native verification must pass it here, not in prose
    native = native_verification_problem(unit, record, root)
    if native:
        return fail(f"{unit}: {native}", 1)

    target = f" for {args.next_unit}" if args.next_unit else ""
    print(f"ADMISSION GRANTED: {unit} is PASS, complete, and still matches the current candidate{target}")
    print(f"  commit            {current_commit}")
    print(f"  runtime_identity  {current_runtime}")
    print(f"  driver_sha256     {identity.get('driver_binary_sha256')}")
    print(f"  interpreter       {interpreter.get('version')} at {interpreter.get('executable')}")
    print(f"  installed deps    {runtime.get('pinned_count')} pinned, {runtime.get('installed_fingerprint')[:12]}")
    print(f"  baseline smoke    {smoke['detail']}")
    print(f"  evidence files    {len(json.loads((attempt_dir / 'evidence.json').read_text()).get('files', {}))}")
    print(f"  attempt           {attempt_dir}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
