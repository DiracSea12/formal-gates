#!/usr/bin/env python3
"""Controls for the acceptance runner's own stream handling.

The stall monitor replaced a wall-clock threshold, so "no stall" is only
meaningful if the monitor actually fires. These cases drive the real streaming
method with cheap stand-in commands, so the production constants stay untouched
and no four-minute wait is needed:

  RS-1  complete silence beyond the budget      -> stall detected, child killed
  RS-2  output without a newline, then silence  -> stall detected
  RS-2b partial output resets nothing it shouldn't: a steady stream -> no stall
  RS-3  output within the budget                -> no stall, exit code preserved
  RS-4  non-zero exit with output               -> exit code reported, no stall
  RS-5  a stall must fail the item end to end    -> real runner, tiny budget

  RG-1  re-signing over a tracked record        -> accepted (the " M " case)
  RG-2  a fresh untracked record                -> accepted (the "?? " case)
  RG-3  an unrelated dirty file                 -> refused

No product source, no acceptance record and no active run is modified.
"""

from __future__ import annotations

import argparse
import datetime
import json
import shutil
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import acceptance  # noqa: E402  (the runner under test)


class Controls:
    def __init__(self, root: Path, out: Path):
        self.root = root
        self.out = out
        self.results: list[tuple[str, bool]] = []

    def check(self, label: str, ok: bool, detail: str = "") -> None:
        self.results.append((label, ok))
        print(f"  {'OK  ' if ok else 'BAD '} {label}{f': {detail}' if detail and not ok else ''}", flush=True)

    def stream_case(self, name: str, script: str, budget: float, expect_stall: bool, expect_code: int | None = None) -> None:
        attempt = self.out / f"runner-{name}"
        if attempt.exists():
            shutil.rmtree(attempt)
        attempt.mkdir(parents=True)
        runner = acceptance.Runner(
            root=self.root, unit="ctl", attempt=attempt,
            keep_going=False, stall_budget=budget,
        )
        step = acceptance.Step(runner, name)
        code, elapsed, stall, _ = step.sh_stream(["/bin/sh", "-c", script], "cmd", stall_budget=budget)
        if expect_stall:
            self.check(f"{name} stall detected", stall is not None, f"no stall reported; exit={code} after {elapsed:.1f}s")
            if stall:
                self.check(f"{name} stall names the silence", "no output for" in stall, stall)
        else:
            self.check(f"{name} no false stall", stall is None, str(stall))
            if expect_code is not None:
                self.check(f"{name} exit code preserved", code == expect_code, f"exit {code}, expected {expect_code}")

    def run_stream_cases(self) -> None:
        # A short budget keeps every case well under a second of waiting.
        self.stream_case("silent", "sleep 30", 1.0, expect_stall=True)
        # Output without a trailing newline must still count as progress observed,
        # and the silence after it must still be detected.
        self.stream_case("no-newline", "printf start; sleep 30", 1.0, expect_stall=True)
        self.stream_case("steady", "for i in 1 2 3 4 5; do echo tick \"$i\"; sleep 0.4; done", 1.5, expect_stall=False, expect_code=0)
        self.stream_case("nonzero", "echo boom; exit 3", 1.5, expect_stall=False, expect_code=3)

    def run_end_to_end_case(self) -> None:
        """A real stall in the real item must fail the run, not just be logged."""
        out = self.out / "end-to-end"
        if out.exists():
            shutil.rmtree(out)
        # main() refuses an existing attempt directory, so leave it uncreated.
        attempt = out / "attempt"
        proc = subprocess.run(
            [
                sys.executable, str(Path(__file__).resolve().parent / "acceptance.py"),
                "--unit", "ctl-stall",
                # A budget of milliseconds guarantees the go test command is
                # killed before it can print anything.
                "--stall-budget", "0.001",
                "--only", "go-test-full",
                "--attempt-dir", str(attempt),
            ],
            cwd=str(self.root), capture_output=True, text=True,
        )
        (out / "run.stdout").write_text(proc.stdout + proc.stderr)
        self.check("RS-5 a stall fails the item (runner exits non-zero)", proc.returncode != 0, f"exit {proc.returncode}")
        summary_path = attempt / "summary.json"
        overall = json.loads(summary_path.read_text())["overall"] if summary_path.is_file() else "MISSING"
        self.check("RS-5 summary records the failure", overall == "FAIL", f"overall={overall}")
        self.check("RS-5 stall is reported in the log", "STALL" in (proc.stdout + proc.stderr), (proc.stdout + proc.stderr)[-300:])

    def run_resign_cases(self) -> None:
        repo = self.out / "resign-repo"
        if repo.exists():
            shutil.rmtree(repo)
        repo.mkdir(parents=True)
        record_rel = ".gates/results/go-engine-CTL.json"
        (repo / record_rel).parent.mkdir(parents=True)

        def git(*args: str) -> subprocess.CompletedProcess:
            return subprocess.run(["git", *args], cwd=str(repo), capture_output=True, text=True)

        git("init", "-q")
        (repo / ".gitignore").write_text(".harness/\n")
        (repo / record_rel).write_text('{"unit":"CTL","overall":"FAIL"}\n')
        (repo / "tracked.txt").write_text("x\n")
        git("-c", "user.name=ctl", "-c", "user.email=ctl@localhost", "-c", "commit.gpgsign=false", "add", "-A")
        git("-c", "user.name=ctl", "-c", "user.email=ctl@localhost", "-c", "commit.gpgsign=false", "commit", "-qm", "base")

        attempt = repo / ".harness" / "attempt"
        attempt.mkdir(parents=True)
        (attempt / "summary.json").write_text(json.dumps({"unit": "CTL", "overall": "PASS", "items": []}) + "\n")
        (attempt / "identity.json").write_text(json.dumps({"commit": git("rev-parse", "HEAD").stdout.strip()}) + "\n")

        def issue(target_rel: str) -> int:
            runner = acceptance.Runner(root=repo, unit="CTL", attempt=attempt, keep_going=False)
            runner.summary = {"overall": "PASS", "items": []}
            return runner.issue_record(repo / target_rel, None)

        # The record already exists and is tracked, so git reports " M <path>".
        status = git("status", "--porcelain").stdout
        self.check("RG-0 fixture starts clean", status == "", status)
        code = issue(record_rel)
        delta = git("status", "--porcelain").stdout
        self.check("RG-1 re-signing over a tracked record is accepted", code == 0, f"exit {code}; git said {delta!r}")
        # Reset the fixture so the next case starts from a clean tree again.
        git("add", "-A")
        git("-c", "user.name=ctl", "-c", "user.email=ctl@localhost", "-c", "commit.gpgsign=false", "commit", "-qm", "resigned")

        fresh_rel = ".gates/results/go-engine-CTL-fresh.json"
        code = issue(fresh_rel)
        self.check("RG-2 a fresh untracked record is accepted", code == 0, f"exit {code}; git said {git('status', '--porcelain').stdout!r}")
        git("add", "-A")
        git("-c", "user.name=ctl", "-c", "user.email=ctl@localhost", "-c", "commit.gpgsign=false", "commit", "-qm", "fresh")

        (repo / "tracked.txt").write_text("changed\n")
        code = issue(record_rel)
        self.check("RG-3 an unrelated dirty file is refused", code != 0, f"exit {code}")

    def run(self) -> int:
        if subprocess.run(["git", "status", "--porcelain"], cwd=str(self.root), capture_output=True, text=True).stdout.strip():
            self.check("precondition: candidate worktree clean", False, "commit first; the runner cases do not read the tree but the end-to-end case does")
        else:
            self.check("precondition: candidate worktree clean", True)
        self.run_stream_cases()
        self.run_end_to_end_case()
        self.run_resign_cases()
        failed = [label for label, ok in self.results if not ok]
        print()
        if failed:
            print(f"RUNNER CONTROLS FAILED: {len(failed)} case(s): {failed}")
            return 1
        print(f"RUNNER CONTROLS PASSED ({len(self.results)} cases)")
        return 0


def main() -> int:
    parser = argparse.ArgumentParser(description="Controls for the acceptance runner's stream handling")
    parser.add_argument("--root", default=None, help="candidate worktree root")
    parser.add_argument("--out", default=None, help="directory for control fixtures and logs")
    args = parser.parse_args()
    root = Path(args.root).resolve() if args.root else Path(__file__).resolve().parents[2]
    if args.out:
        out = Path(args.out).resolve()
    else:
        stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        out = root / ".harness" / "validation" / "runner-controls" / stamp
    out.mkdir(parents=True, exist_ok=True)
    return Controls(root=root, out=out).run()


if __name__ == "__main__":
    sys.exit(main())
