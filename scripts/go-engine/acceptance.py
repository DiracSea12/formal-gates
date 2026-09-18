#!/usr/bin/env python3
"""Go-engine acceptance runner.

Contract (plan section 12.5):

* every invocation writes into a brand new attempt directory; nothing is ever
  overwritten, so a later green run cannot erase an earlier red one;
* each required item records its own command, raw stdout/stderr and exit code;
* each required item is asserted against an expected *business* result, not
  just "the command exited 0";
* the process exit code is the aggregate conclusion: 0 only when every required
  item PASSed.

It never sends model prompts. Native host checks are limited to `doctor`, which
does not spend model calls.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import queue
import shutil
import subprocess
import sys
import threading
import time
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import runtime_check  # noqa: E402  (shared runtime verification)

GO_TEST_BACKSTOP_TIMEOUT = "60m"
# A run is judged by whether it makes progress, not by how fast the machine is.
# Empirical basis (recorded in the B0 record): internal/validate holds 785
# serial top-level tests, the slowest single test measured 5.76s, and the
# package finishes in ~378-404s here but took 900.6s on an independent, slower
# environment for the same candidate. A wall-clock threshold therefore encodes
# machine speed rather than correctness. What it must still catch is a stall,
# so the runner watches the event stream and fails when nothing arrives for
# STALL_BUDGET_SECONDS - a hung test emits nothing regardless of machine speed.
STALL_BUDGET_SECONDS = 240.0
# Per-package durations are recorded as observations to make an environment
# slowdown visible. They do not fail the item.
SQLITE_PROBE_CHECKS = [
    "open-file-database",
    "schema-and-pragmas",
    "foreign-key-rejected",
    "transaction-commit-and-rollback",
    "wal-coordination-files",
    "online-backup-consistent",
    "wal-reader-not-blocked-by-open-writer",
    "busy-reported-then-resolved",
]
# Skips that are understood and justified. Anything else makes the item FAIL.
ALLOWED_SKIPS = {
    (
        "formal-gates/internal/validate",
        "TestNormalizeHostPath",
    ): "Windows-only path normalisation; skipped on darwin with an explicit reason",
}


def utc_stamp() -> str:
    return datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as fh:
        for block in iter(lambda: fh.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


class Step:
    """One required acceptance item."""

    def __init__(self, run: "Runner", sid: str, required: bool = True):
        self.run = run
        self.id = sid
        self.required = required
        self.dir = run.attempt / "steps" / sid
        self.dir.mkdir(parents=True, exist_ok=False)
        self.notes: list[str] = []
        self.problems: list[str] = []
        self.commands: list[dict] = []
        self.seconds = 0.0

    def note(self, text: str) -> None:
        self.notes.append(text)

    def problem(self, text: str) -> None:
        self.problems.append(text)

    def require(self, condition: bool, message: str) -> bool:
        if not condition:
            self.problem(message)
        return condition

    def sh(self, args: list[str], name: str, env: dict | None = None, cwd: Path | None = None) -> subprocess.CompletedProcess:
        merged = dict(os.environ)
        if env:
            merged.update(env)
        started = time.monotonic()
        proc = subprocess.run(
            args,
            cwd=str(cwd or self.run.root),
            env=merged,
            capture_output=True,
            text=True,
        )
        elapsed = time.monotonic() - started
        (self.dir / f"{name}.stdout").write_text(proc.stdout)
        (self.dir / f"{name}.stderr").write_text(proc.stderr)
        (self.dir / f"{name}.exit").write_text(f"{proc.returncode}\n")
        (self.dir / f"{name}.cmd").write_text("\n".join(args) + "\n")
        self.commands.append(
            {
                "name": name,
                "argv": args,
                "exit_code": proc.returncode,
                "seconds": round(elapsed, 3),
                "stdout": f"steps/{self.id}/{name}.stdout",
                "stderr": f"steps/{self.id}/{name}.stderr",
                "exit": f"steps/{self.id}/{name}.exit",
            }
        )
        detail = f"{name}: exit={proc.returncode} ({elapsed:.1f}s)"
        if proc.returncode != 0:
            tail = (proc.stderr or proc.stdout).strip().splitlines()[-8:]
            detail += " | " + " / ".join(tail) if tail else ""
        self.run.log(f"  {self.id} {detail}")
        return proc

    def sh_stream(
        self,
        args: list[str],
        name: str,
        env: dict | None = None,
        stall_budget: float | None = None,
        cwd: Path | None = None,
    ) -> tuple[int, float, str | None, str]:
        """Run a command, streaming its output to a file, and watch for a stall.

        Returns (exit_code, seconds, stall_description, stdout_text).

        The output is drained by a reader thread into a queue so the timing loop
        is never blocked by `readline()`. A blocking read would make the stall
        check unreachable exactly when it matters - when the child goes silent -
        and would also reset the progress clock once output resumed, hiding the
        silence entirely. Progress is any bytes arriving, not any complete line,
        so a child that emits a partial line and then wedges is still detected.
        """
        merged = dict(os.environ)
        if env:
            merged.update(env)
        budget = stall_budget if stall_budget is not None else STALL_BUDGET_SECONDS
        out_path = self.dir / f"{name}.stdout"
        err_path = self.dir / f"{name}.stderr"
        started = time.monotonic()
        blocks: "queue.Queue[bytes | None]" = queue.Queue()
        stall: str | None = None

        with out_path.open("wb") as out_fh, err_path.open("wb") as err_fh:
            proc = subprocess.Popen(
                args, cwd=str(cwd or self.run.root), env=merged,
                stdout=subprocess.PIPE, stderr=err_fh, bufsize=0,
            )
            fd = proc.stdout.fileno()

            def pump() -> None:
                try:
                    while True:
                        block = os.read(fd, 65536)
                        if not block:
                            break
                        blocks.put(block)
                except OSError:
                    pass
                finally:
                    blocks.put(None)

            reader = threading.Thread(target=pump, daemon=True)
            reader.start()

            last_progress = started
            eof = False
            while not eof:
                remaining = budget - (time.monotonic() - last_progress)
                if remaining <= 0:
                    silent_for = time.monotonic() - last_progress
                    stall = (
                        f"{args[0]} produced no output for {silent_for:.0f}s "
                        f"(budget {budget:.0f}s); the run is stalled rather than merely slow"
                    )
                    proc.kill()
                    break
                try:
                    block = blocks.get(timeout=min(remaining, 1.0))
                except queue.Empty:
                    continue
                if block is None:
                    eof = True
                    break
                out_fh.write(block)
                out_fh.flush()
                last_progress = time.monotonic()

            # Drain whatever the child managed to emit so the log is complete.
            while True:
                try:
                    block = blocks.get_nowait()
                except queue.Empty:
                    break
                if block is None:
                    break
                out_fh.write(block)

            if proc.poll() is None:
                proc.kill()
            code = proc.wait()

        elapsed = time.monotonic() - started
        reader.join(timeout=2.0)
        (self.dir / f"{name}.exit").write_text(f"{code}\n")
        (self.dir / f"{name}.cmd").write_text("\n".join(args) + "\n")
        self.commands.append(
            {
                "name": name,
                "argv": args,
                "exit_code": code,
                "seconds": round(elapsed, 3),
                "stdout": f"steps/{self.id}/{name}.stdout",
                "stderr": f"steps/{self.id}/{name}.stderr",
                "exit": f"steps/{self.id}/{name}.exit",
                "stall_budget_seconds": budget,
            }
        )
        self.run.log(f"  {self.id} {name}: exit={code} ({elapsed:.1f}s)" + (f" STALL: {stall}" if stall else ""))
        return code, elapsed, stall, out_path.read_text(errors="replace")

    def finish(self) -> dict:
        if not self.required:
            status = "NOT_REQUIRED"
        elif self.problems:
            status = "FAIL"
        else:
            status = "PASS"
        self.run.log(f"  -> {self.id}: {status}" + (f" ({len(self.problems)} problem(s))" if self.problems else ""))
        for p in self.problems:
            self.run.log(f"       ! {p}")
        return {
            "id": self.id,
            "required": self.required,
            "status": status,
            "seconds": round(self.seconds, 3),
            "commands": self.commands,
            "notes": self.notes,
            "problems": self.problems,
        }


class Runner:
    def __init__(self, root: Path, unit: str, attempt: Path, keep_going: bool, only: set[str] | None = None, stall_budget: float | None = None):
        self.root = root
        self.unit = unit
        self.attempt = attempt
        self.keep_going = keep_going
        self.only = only
        self.stall_budget = stall_budget
        self.results: list[dict] = []
        self.log_lines: list[str] = []
        self.summary: dict = {}

    def log(self, text: str) -> None:
        self.log_lines.append(text)
        print(text, flush=True)

    def write_log(self) -> None:
        (self.attempt / "run.log").write_text("\n".join(self.log_lines) + "\n")

    # -- steps ---------------------------------------------------------------

    def identity(self) -> dict:
        step = Step(self, "identity")
        started = time.monotonic()
        git = lambda *a: subprocess.run(["git", *a], cwd=str(self.root), capture_output=True, text=True).stdout.strip()
        venv_python = self.root / ".harness" / "venv" / "bin" / "python"
        binary = self.root / ".harness" / "bin" / "formal-gates"
        step.require(venv_python.is_file(), f"missing candidate runtime: {venv_python}")
        step.require(binary.is_file(), f"missing driver binary: {binary}")

        data = {
            "unit": self.unit,
            "attempt": str(self.attempt),
            "recorded_at": datetime.now(timezone.utc).isoformat(),
            "root": str(self.root),
            "branch": git("rev-parse", "--abbrev-ref", "HEAD"),
            "commit": git("rev-parse", "HEAD"),
            "worktree_clean": git("status", "--porcelain") == "",
            "files": {},
            "toolchain": {},
        }
        for rel in ("go.mod", "go.sum", "runtime/langgraph/requirements.lock", "runtime/langgraph/pyproject.toml", "prompts/reviewer-base.md", "scripts/harness"):
            p = self.root / rel
            if p.is_file():
                data["files"][rel] = sha256_file(p)
        if binary.is_file():
            data["driver_binary_sha256"] = sha256_file(binary)
        for name, argv in (
            ("go", ["go", "version"]),
            ("python", [str(venv_python), "-V"]),
            ("node", ["node", "-v"]),
            ("dsh", ["dsh", "--version"]),
        ):
            try:
                proc = subprocess.run(argv, capture_output=True, text=True, cwd=str(self.root))
                data["toolchain"][name] = (proc.stdout or proc.stderr).strip().splitlines()[0] if (proc.stdout or proc.stderr).strip() else ""
            except FileNotFoundError:
                data["toolchain"][name] = "NOT_FOUND"
        # runtime_identity is owned by the harness; ask it rather than reimplementing.
        if venv_python.is_file():
            probe = (
                "import sys,json;sys.path.insert(0,str(__import__('pathlib').Path.cwd()/'runtime'/'langgraph'));"
                "from pathlib import Path;from formal_gates_harness.infra import runtime_identity;"
                f"print(json.dumps(runtime_identity(Path.cwd(), {str(binary)!r})))"
            )
            proc = subprocess.run([str(venv_python), "-c", probe], cwd=str(self.root), capture_output=True, text=True)
            (self.attempt / "steps" / "identity" / "runtime-identity.stdout").write_text(proc.stdout)
            (self.attempt / "steps" / "identity" / "runtime-identity.stderr").write_text(proc.stderr)
            if proc.returncode == 0 and proc.stdout.strip():
                data["runtime_identity"] = proc.stdout.strip().splitlines()[-1].strip('"')
            else:
                step.problem(f"could not compute runtime_identity: exit={proc.returncode}")
        # A lock file pins versions; it does not prove anything is installed.
        # Record the interpreter that would actually run the harness and the
        # installed distribution set, so admission can refuse an unpopulated
        # environment instead of granting on a lock file's say-so.
        if venv_python.is_file():
            interpreter = runtime_check.interpreter_identity(venv_python)
            data["interpreter"] = interpreter
            if interpreter.get("error"):
                step.problem(f"could not query the candidate interpreter: {interpreter['error']}")
            installed = runtime_check.verify(self.root, venv_python)
            data["installed"] = installed
            step.require(not installed.get("missing"), f"locked dependencies are not installed: {installed.get('missing')}")
            step.require(not installed.get("mismatched"), f"installed versions differ from the lock: {installed.get('mismatched')}")
            step.require(not installed.get("errored"), f"could not read installed versions: {installed.get('errored')}")
            step.note(f"interpreter {interpreter.get('version')} at {interpreter.get('executable')}")
            step.note(f"{installed.get('pinned_count')} pinned distributions all installed")

        step.require(bool(data["commit"]), "could not resolve HEAD commit")
        step.require(data["worktree_clean"], "worktree was already dirty before the acceptance run")
        (self.attempt / "identity.json").write_text(json.dumps(data, indent=2) + "\n")
        step.seconds = time.monotonic() - started
        self.results.append(step.finish())
        self.identity_data = data
        return data

    def go_vet(self) -> None:
        step = Step(self, "go-vet")
        started = time.monotonic()
        proc = step.sh(["go", "vet", "./..."], "vet")
        step.require(proc.returncode == 0, "go vet reported problems")
        step.seconds = time.monotonic() - started
        self.results.append(step.finish())

    def go_test_full(self) -> None:
        step = Step(self, "go-test-full")
        started = time.monotonic()
        code, elapsed, stall, stdout = step.sh_stream(
            ["go", "test", "-count=1", "-timeout", GO_TEST_BACKSTOP_TIMEOUT, "-json", "./..."],
            "go-test",
            stall_budget=self.stall_budget if self.stall_budget is not None else STALL_BUDGET_SECONDS,
        )
        # No second copy of the stream. `go-test.stdout` already *is* the -json
        # stream that `go-test` produced, and nothing ever read a `go-test.jsonl`:
        # it cost 1.8 MB per attempt - the largest file in the attempt after the
        # probe binaries - to duplicate bytes that were already recorded.
        packages: dict[str, dict] = {}
        skipped: list[dict] = []
        failed_tests: list[dict] = []
        passes = 0
        for line in stdout.splitlines():
            line = line.strip()
            if not line.startswith("{"):
                continue
            try:
                event = json.loads(line)
            except ValueError:
                continue
            pkg = event.get("Package")
            action = event.get("Action")
            test = event.get("Test")
            if not pkg:
                continue
            if test:
                if action == "pass":
                    passes += 1
                elif action == "fail":
                    failed_tests.append({"package": pkg, "test": test})
                elif action == "skip":
                    skipped.append({"package": pkg, "test": test})
                continue
            entry = packages.setdefault(pkg, {"package": pkg, "action": "unknown", "seconds": 0.0})
            if action in ("pass", "fail", "skip"):
                entry["action"] = action
                entry["seconds"] = round(float(event.get("Elapsed") or 0.0), 3)
            elif action == "output" and event.get("Output", "").startswith("panic: test timed out"):
                step.problem(f"{pkg} printed a Go test timeout panic (backstop {GO_TEST_BACKSTOP_TIMEOUT})")

        failures = [p for p in packages.values() if p["action"] == "fail"]

        step.require(stall is None, stall or "")
        step.require(code == 0, f"go test ./... exited {code}")
        step.require(not failures, f"packages failed: {[p['package'] for p in failures]}")
        step.require(passes > 0, "no test pass events were observed")
        step.require(len(packages) > 0, "no package results were parsed")
        for s in skipped:
            key = (s["package"], s["test"])
            if key not in ALLOWED_SKIPS:
                step.problem(f"unexplained skip: {s['package']}:{s['test']}")
        for key, reason in ALLOWED_SKIPS.items():
            step.note(f"allowed skip {key[0]}:{key[1]} - {reason}")

        summary = {
            "exit_code": code,
            "seconds": round(elapsed, 3),
            "stall_budget_seconds": self.stall_budget if self.stall_budget is not None else STALL_BUDGET_SECONDS,
            "backstop_timeout": GO_TEST_BACKSTOP_TIMEOUT,
            "stall": stall,
            "test_pass_events": passes,
            "failed_tests": failed_tests,
            "skipped": skipped,
            "packages": sorted(packages.values(), key=lambda p: p["package"]),
            "note": (
                "Package durations are observations, not thresholds: a fixed "
                "wall-clock ceiling would encode machine speed. The pass condition "
                "is exit 0, no failed package, no unexplained skip and no stall."
            ),
        }
        (self.attempt / "steps" / "go-test-full" / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
        step.note(f"{len(packages)} packages, {passes} test pass events, {elapsed:.0f}s wall")
        step.seconds = time.monotonic() - started
        self.results.append(step.finish())

    def sqlite_probe(self) -> None:
        step = Step(self, "sqlite-probe")
        started = time.monotonic()
        probe_dir = self.attempt / "steps" / "sqlite-probe" / "evidence"
        probe_dir.mkdir(parents=True, exist_ok=True)
        # The probe binaries are build products, not evidence. Two of them are
        # ~9 MB each, which made them ~80% of a whole attempt. They are built
        # under .harness/tmp (outside the attempt, so the manifest never lists
        # them) and removed when the item is done; what stays in the evidence is
        # the probe report, the streams, and the size/digest recorded below.
        scratch = self.root / ".harness" / "tmp" / f"sqlprobe-{self.attempt.name}"
        scratch.mkdir(parents=True, exist_ok=True)
        for mode in ("1", "0"):
            binary = scratch / f"sqlprobe-cgo{mode}"
            build = step.sh(
                ["go", "build", "-o", str(binary), "./scripts/go-engine/sqlprobe"],
                f"build-cgo{mode}",
                env={"CGO_ENABLED": mode},
            )
            if not step.require(build.returncode == 0, f"CGO_ENABLED={mode} build failed"):
                continue
            step.note(
                f"CGO_ENABLED={mode}: probe binary {binary.stat().st_size} bytes, "
                f"sha256 {sha256_file(binary)[:16]}... (build product, not retained; "
                "rebuild from the pinned commit with this CGO_ENABLED to reproduce)"
            )
            run = step.sh([str(binary), "-dir", str(probe_dir / f"run-cgo{mode}")], f"run-cgo{mode}", env={"CGO_ENABLED": mode})
            report_path = probe_dir / f"run-cgo{mode}" / "probe-report.json"
            if not step.require(report_path.is_file(), f"CGO_ENABLED={mode} produced no probe report"):
                continue
            report = json.loads(report_path.read_text())
            shutil.copy2(report_path, probe_dir / f"report-cgo{mode}.json")
            seen = {c["id"]: c for c in report.get("checks", [])}
            step.require(report.get("ok") is True, f"CGO_ENABLED={mode} probe reported ok=false: {report.get('failed')}")
            step.require(run.returncode == 0, f"CGO_ENABLED={mode} probe exited {run.returncode}")
            for cid in SQLITE_PROBE_CHECKS:
                step.require(cid in seen, f"CGO_ENABLED={mode} probe did not report check {cid}")
                if cid in seen:
                    step.require(seen[cid]["status"] == "PASS", f"CGO_ENABLED={mode} check {cid} was {seen[cid]['status']}: {seen[cid].get('error','')}")
            expected_cgo = mode == "1"
            step.require(report.get("built_with_cgo") is expected_cgo, f"CGO_ENABLED={mode} probe reported built_with_cgo={report.get('built_with_cgo')}")
            step.note(f"CGO_ENABLED={mode}: sqlite {report.get('sqlite_version')}, {len(report.get('checks', []))} checks all PASS")
        unit = step.sh(["go", "test", "-count=1", "./scripts/go-engine/sqlprobe"], "unit-test")
        step.require(unit.returncode == 0, "sqlprobe unit tests failed")
        shutil.rmtree(scratch, ignore_errors=True)
        step.seconds = time.monotonic() - started
        self.results.append(step.finish())

    def python_unittest(self) -> None:
        step = Step(self, "python-unittest")
        started = time.monotonic()
        venv_python = self.root / ".harness" / "venv" / "bin" / "python"
        resolved_tmp = subprocess.run(
            [str(venv_python), "-c", "from pathlib import Path;import tempfile;print(Path(tempfile.gettempdir()).resolve())"],
            capture_output=True, text=True,
        ).stdout.strip()
        env = {
            "PYTHONDONTWRITEBYTECODE": "1",
            "PYTHONPATH": str(self.root / "runtime" / "langgraph"),
            "TMPDIR": resolved_tmp,
        }
        proc = step.sh([str(venv_python), "-m", "unittest", "discover", "-s", "runtime/langgraph/tests", "-v"], "unittest", env=env)
        combined = proc.stdout + proc.stderr
        ran = [ln for ln in combined.splitlines() if ln.startswith("Ran ")]
        count = 0
        if ran:
            try:
                count = int(ran[-1].split()[1])
            except (IndexError, ValueError):
                count = 0
        step.require(proc.returncode == 0, f"python unittest exited {proc.returncode}")
        step.require("OK" in combined.splitlines()[-3:], "python unittest did not report OK")
        step.require(count > 0, "python unittest reported no test count")
        step.note(f"Ran {count} tests")
        (self.attempt / "steps" / "python-unittest" / "summary.json").write_text(json.dumps({"exit_code": proc.returncode, "tests": count}, indent=2) + "\n")
        step.seconds = time.monotonic() - started
        self.results.append(step.finish())

    def demo_fixture(self) -> None:
        step = Step(self, "demo-fixture")
        started = time.monotonic()
        proc = step.sh(["./scripts/harness", "demo", "--approve-sample"], "demo")
        step.require(proc.returncode == 0, f"demo exited {proc.returncode}")
        created = None
        for line in proc.stdout.splitlines():
            line = line.strip()
            if line.startswith("{") and "created_run" in line:
                created = json.loads(line)["created_run"]
                break
        if not step.require(created is not None, "demo did not report created_run"):
            step.seconds = time.monotonic() - started
            self.results.append(step.finish())
            return
        step.note(f"run {Path(created).name}")
        result_path = Path(created) / "harness-result.json"
        manifest_path = Path(created) / "manifest.json"
        if not step.require(result_path.is_file(), "demo run produced no harness-result.json"):
            step.seconds = time.monotonic() - started
            self.results.append(step.finish())
            return
        result = json.loads(result_path.read_text())
        manifest = json.loads(manifest_path.read_text())
        step.note(f"status={result.get('status')} evidence_level={result.get('evidence_level')}")
        # Business assertions, not just exit codes: a run that stopped waiting for
        # the user must not be accepted as a completed fixture flow.
        step.require(result.get("status") == "DEMO_COMPLETE", f"terminal status was {result.get('status')!r}, want DEMO_COMPLETE")
        step.require(result.get("evidence_level") == "fixture", f"evidence_level was {result.get('evidence_level')!r}, want fixture")
        step.require(result.get("profile") == "harness-v1", f"profile was {result.get('profile')!r}")
        step.require(bool(result.get("candidate")), "result carried no candidate identity")
        step.require(manifest.get("runtime_identity") is not None, "manifest carried no runtime identity")
        checks = result.get("checks", [])
        step.require(len(checks) > 0, "result carried no checks")
        for c in checks:
            step.require(c.get("status") == "PASS", f"check {c.get('id')} was {c.get('status')}")
            step.require(c.get("exit_code") == 0, f"check {c.get('id')} exited {c.get('exit_code')}")
        gates = result.get("gates", [])
        step.require(len(gates) > 0, "result carried no gates")
        for g in gates:
            step.require(g.get("result", {}).get("status") == "PASS", f"gate {g.get('id')} was {g.get('result', {}).get('status')}")
        audit = {
            "run": created,
            "status": result.get("status"),
            "evidence_level": result.get("evidence_level"),
            "candidate": result.get("candidate"),
            "revision": result.get("revision"),
            "runtime_identity": manifest.get("runtime_identity"),
            "checks": [{"id": c.get("id"), "status": c.get("status"), "exit_code": c.get("exit_code")} for c in checks],
            "gates": [{"id": g.get("id"), "status": g.get("result", {}).get("status")} for g in gates],
        }
        (self.attempt / "steps" / "demo-fixture" / "assertions.json").write_text(json.dumps(audit, indent=2) + "\n")
        step.seconds = time.monotonic() - started
        self.results.append(step.finish())

    def doctor(self, provider: str) -> None:
        step = Step(self, f"doctor-{provider}")
        started = time.monotonic()
        proc = step.sh(["./scripts/harness", "doctor", "--provider", provider], "doctor")
        step.require(proc.returncode == 0, f"doctor exited {proc.returncode}")
        try:
            report = json.loads(proc.stdout)
        except ValueError as exc:
            step.problem(f"doctor output was not JSON: {exc}")
            report = {}
        if report:
            step.require(report.get("ok") is True, "doctor reported ok != true")
            step.require(report.get("model_calls") == 0, f"doctor spent {report.get('model_calls')} model calls")
            step.note(f"binding recorded for {provider}; model_calls=0")
        step.seconds = time.monotonic() - started
        self.results.append(step.finish())

    def tree_clean(self) -> None:
        step = Step(self, "tree-clean-after")
        started = time.monotonic()
        proc = subprocess.run(["git", "status", "--porcelain", "--untracked-files=normal"], cwd=str(self.root), capture_output=True, text=True)
        dirty = proc.stdout.strip()
        step.require(proc.returncode == 0, "git status failed")
        step.require(dirty == "", f"worktree was modified by the acceptance run:\n{dirty}")
        (self.attempt / "steps" / "tree-clean-after" / "status.txt").write_text(proc.stdout)
        step.seconds = time.monotonic() - started
        self.results.append(step.finish())

    # -- driver --------------------------------------------------------------

    def run_all(self) -> int:
        self.log(f"unit={self.unit} root={self.root}")
        self.log(f"attempt={self.attempt}")
        if self.only:
            self.log(f"DIAGNOSTIC MODE: running only {sorted(self.only)}; no record can be issued")
        plan = [
            ("identity", self.identity, True),
            ("go-vet", self.go_vet, True),
            ("go-test-full", self.go_test_full, True),
            ("sqlite-probe", self.sqlite_probe, True),
            ("python-unittest", self.python_unittest, True),
            ("demo-fixture", self.demo_fixture, True),
            ("doctor-dsh", lambda: self.doctor("dsh"), True),
            ("doctor-zcode", lambda: self.doctor("zcode"), True),
            ("tree-clean-after", self.tree_clean, True),
        ]
        for sid, fn, _ in plan:
            if self.only is not None and sid not in self.only:
                continue
            if self._blocked():
                break
            fn()
        summary = {
            "unit": self.unit,
            "attempt": str(self.attempt),
            "recorded_at": datetime.now(timezone.utc).isoformat(),
            "diagnostic_only": sorted(self.only) if self.only else None,
            "go_test_backstop_timeout": GO_TEST_BACKSTOP_TIMEOUT,
            "stall_budget_seconds": STALL_BUDGET_SECONDS,
            "items": self.results,
            "failed_items": [r["id"] for r in self.results if r["status"] == "FAIL"],
            "overall": "PASS" if not any(r["status"] == "FAIL" for r in self.results) and len(self.results) == len(self.expected_ids()) else "FAIL",
            "expected_items": self.expected_ids(),
        }
        (self.attempt / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
        self.write_log()
        self.log(f"overall={summary['overall']} failed={summary['failed_items']}")
        self.write_evidence_manifest()
        self.summary = summary
        return 0 if summary["overall"] == "PASS" else 1

    def write_evidence_manifest(self) -> dict:
        """Hash every raw artefact so a later copy cannot silently lose it.

        The manifest binds the actual commands, stdout/stderr and exit codes.
        Admission recomputes it, so "the summary says PASS" is never enough on
        its own: the files the summary refers to must still be there and intact.
        """
        digests: dict[str, str] = {}
        for path in sorted(self.attempt.rglob("*")):
            if not path.is_file() or path.name == "evidence.json":
                continue
            digests[path.relative_to(self.attempt).as_posix()] = sha256_file(path)
        manifest = {"root": str(self.attempt), "files": digests, "count": len(digests)}
        (self.attempt / "evidence.json").write_text(json.dumps(manifest, indent=2) + "\n")
        return manifest

    def issue_record(self, record_out: Path, previous_baseline: str | None) -> int:
        """Write the tracked record, only for a PASSing attempt.

        The record is bookkeeping written *after* the run, so the run itself
        cannot pin it. We therefore require the post-record worktree delta to be
        exactly this file, which keeps the record honest about what it changed.
        """
        identity = json.loads((self.attempt / "identity.json").read_text())
        if self.only:
            self.log("refusing to issue a record from a diagnostic-only run")
            return 2
        if self.summary.get("overall") != "PASS":
            self.log("refusing to issue a record: the run did not PASS")
            return 1
        record = {
            "unit": self.unit,
            "attempt": str(self.attempt),
            "recorded_at": datetime.now(timezone.utc).isoformat(),
            "overall": self.summary["overall"],
            "previous_accepted_baseline": previous_baseline,
            "candidate": {
                "commit": identity.get("commit"),
                "branch": identity.get("branch"),
                "runtime_identity": identity.get("runtime_identity"),
                "driver_binary_sha256": identity.get("driver_binary_sha256"),
                "files": identity.get("files"),
            },
            "items": [
                {"id": i["id"], "required": i["required"], "status": i["status"], "seconds": i["seconds"]}
                for i in self.summary["items"]
            ],
            "budget": {
                "go_test_backstop_timeout": GO_TEST_BACKSTOP_TIMEOUT,
                "stall_budget_seconds": STALL_BUDGET_SECONDS,
                "note": "The regression is judged by result and progress, not by wall-clock speed.",
            },
            "attestation": {
                "summary_sha256": sha256_file(self.attempt / "summary.json"),
                "identity_sha256": sha256_file(self.attempt / "identity.json"),
                "evidence_sha256": sha256_file(self.attempt / "evidence.json")
                if (self.attempt / "evidence.json").is_file()
                else None,
                "evidence_file_count": len(
                    json.loads((self.attempt / "evidence.json").read_text()).get("files", {})
                )
                if (self.attempt / "evidence.json").is_file()
                else 0,
            },
            # Files that may legitimately be committed on top of the candidate
            # without invalidating it. The narrative companion is added by hand
            # when it exists; admission refuses anything outside this list.
            "bookkeeping": [
                record_out.relative_to(self.root).as_posix()
                if record_out.is_relative_to(self.root)
                else str(record_out)
            ],
            "run_command": f"python3 scripts/go-engine/acceptance.py --unit {self.unit}",
        }
        record_out.parent.mkdir(parents=True, exist_ok=True)
        record_out.write_text(json.dumps(record, indent=2) + "\n")

        proc = subprocess.run(
            ["git", "status", "--porcelain", "--untracked-files=normal"],
            cwd=str(self.root), capture_output=True, text=True,
        )
        relative = record_out.relative_to(self.root).as_posix() if record_out.is_relative_to(self.root) else str(record_out)
        # Compare whole status lines without trimming the leading space: a
        # modified tracked file reports " M <path>", and stripping it would turn
        # a legitimate re-sign into a false "the candidate moved" failure.
        unexpected = []
        for line in proc.stdout.split("\n"):
            if not line:
                continue
            code, path = line[:2], line[3:]
            if path != relative or code not in ("??", " M", "M ", "A ", "AM"):
                unexpected.append(line)
        if unexpected:
            self.log("record written to " + str(record_out) + " but the worktree delta is unexpected:")
            for line in unexpected:
                self.log(f"  {line}")
            self.log("refusing to claim a clean candidate")
            return 1
        self.log(f"record written to {record_out} (worktree delta: {proc.stdout.strip() or 'none'})")
        return 0

    def expected_ids(self) -> list[str]:
        all_ids = [
            "identity",
            "go-vet",
            "go-test-full",
            "sqlite-probe",
            "python-unittest",
            "demo-fixture",
            "doctor-dsh",
            "doctor-zcode",
            "tree-clean-after",
        ]
        if self.only is None:
            return all_ids
        return [i for i in all_ids if i in self.only]

    def _blocked(self) -> bool:
        return any(r["status"] == "FAIL" for r in self.results) and not self.keep_going


def main() -> int:
    parser = argparse.ArgumentParser(description="Go-engine acceptance runner")
    parser.add_argument("--unit", required=True, help="acceptance unit id, e.g. B0")
    parser.add_argument("--root", default=None, help="candidate worktree root (default: repository containing this script)")
    parser.add_argument("--attempt-dir", default=None, help="explicit attempt directory (must not exist)")
    parser.add_argument("--keep-going", action="store_true", help="run every item even after a failure (used to classify, not to accept)")
    parser.add_argument("--record-out", default=None, help="write the tracked machine-readable record here, only when the run PASSes")
    parser.add_argument("--previous-baseline", default=None, help="unit id this candidate was built on (recorded, not verified)")
    parser.add_argument(
        "--stall-budget",
        type=float,
        default=None,
        help="seconds without output that count as a stall (default: the production budget)",
    )
    parser.add_argument(
        "--only",
        default=None,
        help="diagnostic only: comma-separated item ids to run. A run restricted this way can never issue a record.",
    )
    args = parser.parse_args()
    only = {s.strip() for s in args.only.split(",") if s.strip()} if args.only else None
    if only and args.record_out:
        print("--only is a diagnostic mode and cannot issue an acceptance record", file=sys.stderr)
        return 2

    root = Path(args.root).resolve() if args.root else Path(__file__).resolve().parents[2]
    if args.attempt_dir:
        attempt = Path(args.attempt_dir).resolve()
    else:
        base = root / ".harness" / "validation" / "acceptance" / args.unit
        attempt = base / f"{utc_stamp()}-{os.getpid()}"
    if attempt.exists():
        print(f"refusing to reuse attempt directory {attempt}", file=sys.stderr)
        return 2
    attempt.mkdir(parents=True)

    runner = Runner(root=root, unit=args.unit, attempt=attempt, keep_going=args.keep_going, only=only, stall_budget=args.stall_budget)
    try:
        code = runner.run_all()
    except Exception as exc:  # a crash must still be a non-zero conclusion
        runner.log(f"runner crashed: {exc!r}")
        runner.write_log()
        (attempt / "summary.json").write_text(
            json.dumps({"unit": args.unit, "attempt": str(attempt), "overall": "FAIL", "crash": repr(exc)}, indent=2) + "\n"
        )
        return 1
    if code == 0 and args.record_out:
        return runner.issue_record(Path(args.record_out).resolve(), args.previous_baseline)
    if code != 0:
        runner.log("no record was issued: the run did not PASS")
    return code


if __name__ == "__main__":
    sys.exit(main())
