#!/usr/bin/env python3
"""Controls for the admission gate.

Each case mutates exactly one thing and requires admission to refuse for the
reason under test. Checking the exit code alone is not enough: a dirty worktree
or a missing prerequisite refuses everything, which would make every case pass
for the wrong reason, so each case also asserts the refusal reason.

No product source, no original acceptance record and no active run is modified.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import shutil
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import runtime_check  # noqa: E402


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


class Controls:
    def __init__(self, root: Path, record: Path, out: Path):
        self.root = root
        self.record_path = record
        self.record = json.loads(record.read_text())
        self.attempt = Path(self.record["attempt"])
        self.out = out
        self.admission = Path(__file__).resolve().parent / "admission.py"
        self.results: list[tuple[str, bool, str]] = []

    # -- helpers -------------------------------------------------------------

    def check(self, label: str, ok: bool, detail: str = "") -> None:
        self.results.append((label, ok, detail))
        print(f"  {'OK  ' if ok else 'BAD '} {label}{f': {detail}' if detail and not ok else ''}", flush=True)

    def run_admission(self, record: Path, root: Path | None = None) -> tuple[int, str]:
        argv = [sys.executable, str(self.admission), "--record", str(record), "--next-unit", "control"]
        if root is not None:
            argv += ["--root", str(root)]
        proc = subprocess.run(argv, capture_output=True, text=True)
        return proc.returncode, proc.stdout + proc.stderr

    def expect_refusal(self, label: str, record: Path, want: str, code: int = 1, root: Path | None = None) -> None:
        actual, text = self.run_admission(record, root)
        slug = re.sub(r"[^A-Za-z0-9._-]+", "-", label).strip("-")
        (self.out / f"{slug}.stdout").write_text(text)
        if actual != code:
            self.check(label, False, f"exit {actual}, expected {code}; first line: {text.splitlines()[0] if text.strip() else '(no output)'}")
            return
        if want not in text:
            self.check(label, False, f"refusal reason did not mention {want!r}; first line: {text.splitlines()[0] if text.strip() else '(no output)'}")
            return
        self.check(label, True)

    def mutated(self, name: str, dotted: str, value) -> Path:
        record = json.loads(self.record_path.read_text())
        node = record
        keys = dotted.split(".")
        for key in keys[:-1]:
            node = node[key]
        node[keys[-1]] = value
        target = self.out / f"{name}.json"
        target.write_text(json.dumps(record, indent=2) + "\n")
        return target

    def copied_attempt(
        self,
        name: str,
        *,
        drop: list[str] | None = None,
        rewrite: dict[str, str] | None = None,
        rebuild_manifest: bool = True,
    ) -> Path:
        """Copy the whole attempt, then optionally edit it.

        `rebuild_manifest` decides which class of defect the copy simulates.
        Rebuilding it models an edit that is internally consistent, so only the
        semantic checks can catch it. Leaving it stale models losing files
        during a copy, which the manifest itself must catch.
        """
        copy = self.out / f"attempt-{name}"
        if copy.exists():
            shutil.rmtree(copy)
        shutil.copytree(self.attempt, copy)
        for rel in drop or []:
            target = copy / rel
            if target.is_dir():
                shutil.rmtree(target)
            elif target.exists():
                target.unlink()
        for rel, content in (rewrite or {}).items():
            (copy / rel).write_text(content)
        if rebuild_manifest:
            self.rebuild_manifest(copy)
        return copy

    def rebuild_manifest(self, copy: Path) -> None:
        manifest = {"root": str(copy), "files": {}}
        for path in sorted(copy.rglob("*")):
            if path.is_file() and path.name != "evidence.json":
                manifest["files"][path.relative_to(copy).as_posix()] = digest(path)
        manifest["count"] = len(manifest["files"])
        (copy / "evidence.json").write_text(json.dumps(manifest, indent=2) + "\n")

    def first_command(self) -> tuple[str, dict]:
        """Return the first (item id, command) that actually ran something."""
        summary = json.loads((self.attempt / "summary.json").read_text())
        for item in summary["items"]:
            if item.get("commands"):
                return item["id"], item["commands"][0]
        raise SystemExit("no item in the attempt recorded a command; controls cannot run")

    def record_for_attempt(self, name: str, attempt: Path) -> Path:
        record = json.loads(self.record_path.read_text())
        record["attempt"] = str(attempt)
        record["attestation"] = {
            "summary_sha256": digest(attempt / "summary.json"),
            "identity_sha256": digest(attempt / "identity.json"),
            "evidence_sha256": digest(attempt / "evidence.json"),
        }
        target = self.out / f"{name}.json"
        target.write_text(json.dumps(record, indent=2) + "\n")
        return target

    def rewrite(self, attempt: Path, rel: str, transform) -> None:
        path = attempt / rel
        path.write_text(json.dumps(transform(json.loads(path.read_text())), indent=2) + "\n")

    # -- cases ---------------------------------------------------------------

    def run(self) -> int:
        if subprocess.run(["git", "status", "--porcelain"], cwd=str(self.root), capture_output=True, text=True).stdout.strip():
            self.check("precondition: candidate worktree clean", False, "commit or stash first; otherwise every case refuses for the same reason")

        code, text = self.run_admission(self.record_path)
        (self.out / "valid.stdout").write_text(text)
        self.check("ADM-PC valid record is granted", code == 0 and "ADMISSION GRANTED" in text, text.splitlines()[0] if text.strip() else "")

        self.expect_refusal("ADM-E1 missing record", self.out / "absent.json", "acceptance record is missing", code=2)
        self.expect_refusal("ADM-E2 non-PASS conclusion", self.mutated("overall-fail", "overall", "FAIL"), "recorded overall conclusion")
        self.expect_refusal("ADM-E3 tampered attempt summary", self.mutated("tampered", "attestation.summary_sha256", "0" * 64), "attempt summary changed")
        self.expect_refusal("ADM-E4 record disagrees with its attempt", self.mutated("moved", "candidate.commit", "0" * 40), "record and attempt disagree about commit")
        self.expect_refusal("ADM-E5 undeclared bookkeeping", self.mutated("undeclared", "bookkeeping", [".gates/results/some-other-file.json"]), "bookkeeping may only be")

        # Incomplete item list: the attempt summary is the authoritative result.
        # The manifest is rebuilt so only the semantic check can catch this.
        attempt = self.copied_attempt(
            "incomplete-summary",
            rewrite={"summary.json": json.dumps({**json.loads((self.attempt / "summary.json").read_text()), "items": [{"id": "identity", "required": True, "status": "NOT_RUN", "seconds": 0.0}]}, indent=2) + "\n"},
        )
        self.expect_refusal("ADM-E6 incomplete item list", self.record_for_attempt("incomplete-record", attempt), "did not run every expected item")

        # Failing required item inside an otherwise PASSing summary.
        summary = json.loads((self.attempt / "summary.json").read_text())
        attempt = self.copied_attempt(
            "failing-item",
            rewrite={"summary.json": json.dumps({**summary, "items": [dict(i, status=("FAIL" if i["id"] == "go-test-full" else i["status"])) for i in summary["items"]]}, indent=2) + "\n"},
        )
        self.expect_refusal("ADM-E7 failing required item", self.record_for_attempt("failing-item-record", attempt), "required item go-test-full is FAIL")

        # Candidate HEAD does not descend from. The consistency check passes
        # because the record is rebuilt from the edited attempt.
        attempt = self.copied_attempt(
            "unknown-candidate",
            rewrite={"identity.json": json.dumps({**json.loads((self.attempt / "identity.json").read_text()), "commit": "1" * 40}, indent=2) + "\n"},
        )
        record = self.record_for_attempt("unknown-candidate-record", attempt)
        payload = json.loads(record.read_text())
        payload["candidate"] = {**payload["candidate"], "commit": "1" * 40}
        record.write_text(json.dumps(payload, indent=2) + "\n")
        self.expect_refusal("ADM-E8 candidate HEAD does not descend from", record, "does not descend from recorded")

        # Losing steps/ while the manifest still lists them: exactly the "copied
        # the attempt but forgot the raw evidence" case.
        attempt = self.copied_attempt("missing-evidence", drop=["steps"], rebuild_manifest=False)
        self.expect_refusal("ADM-E9 raw evidence missing", self.record_for_attempt("missing-evidence-record", attempt), "raw evidence referenced by the record is missing")

        # Losing steps/ and rebuilding the manifest: the summary's own command
        # pointers no longer resolve, which is a separate refusal.
        attempt = self.copied_attempt("dropped-commands", drop=["steps"])
        self.expect_refusal("ADM-E9b command pointers unresolvable", self.record_for_attempt("dropped-commands-record", attempt), "which the manifest does not cover")

        # Raw evidence altered, manifest rebuilt to match, so only the semantic
        # exit-code comparison can catch it.
        item_id, command = self.first_command()
        flipped = "0\n" if command["exit_code"] != 0 else "1\n"
        attempt = self.copied_attempt("altered-exit", rewrite={command["exit"]: flipped})
        self.expect_refusal(
            f"ADM-E10 exit evidence contradicts the summary ({item_id}/{command['name']})",
            self.record_for_attempt("altered-exit-record", attempt),
            "exit evidence says",
        )

        # Unpopulated runtime: the record is intact and the tree matches, but the
        # locked dependencies are not installed. This is the state the earlier
        # checker granted on, while the public entry could not even start.
        self.runtime_case()

        self.smoke_case()

        failed = [label for label, ok, _ in self.results if not ok]
        print()
        if failed:
            print(f"ADMISSION CONTROLS FAILED: {len(failed)} case(s): {failed}")
            return 1
        print(f"ADMISSION CONTROLS PASSED ({len(self.results)} cases)")
        return 0

    def runtime_case(self) -> None:
        """A clone whose interpreter exists but whose locked deps are absent."""
        clone = self.out / "unpopulated"
        if clone.exists():
            shutil.rmtree(clone)
        branch = subprocess.run(["git", "rev-parse", "--abbrev-ref", "HEAD"], cwd=str(self.root), capture_output=True, text=True).stdout.strip()
        head = subprocess.run(["git", "rev-parse", "HEAD"], cwd=str(self.root), capture_output=True, text=True).stdout.strip()
        # Clone on the same branch so the branch identity still matches, then pin
        # the exact commit; a detached clone would refuse earlier and for a
        # different reason. `--local` is kept and `--no-hardlinks` is gone: git
        # never writes to an object file in place, so hardlinked objects are safe
        # here and turned this clone from 110 MB into a working-tree-sized
        # ~10 MB. The control mutates only the working tree (`git reset --hard`
        # checks files out fresh), never the shared object store.
        subprocess.run(
            ["git", "clone", "--quiet", "--local", "--branch", branch, str(self.root), str(clone)],
            check=True, capture_output=True, text=True,
        )
        subprocess.run(["git", "reset", "--hard", "--quiet", head], cwd=str(clone), check=True, capture_output=True, text=True)
        # Real driver binary, bare interpreter: the environment is unpopulated.
        (clone / ".harness" / "bin").mkdir(parents=True, exist_ok=True)
        shutil.copy2(self.root / ".harness" / "bin" / "formal-gates", clone / ".harness" / "bin" / "formal-gates")
        (clone / ".harness" / "venv" / "bin").mkdir(parents=True, exist_ok=True)
        (clone / ".harness" / "venv" / "bin" / "python").symlink_to(sys.executable)

        entry = subprocess.run(["./scripts/harness", "hosts"], cwd=str(clone), capture_output=True, text=True)
        (self.out / "unpopulated-public-entry.stdout").write_text(entry.stdout + entry.stderr)
        self.check(
            "PC-R unpopulated runtime breaks the public entry",
            entry.returncode != 0 or "ModuleNotFoundError" in (entry.stderr + entry.stdout),
            f"public entry exited {entry.returncode} without a dependency error",
        )
        runtime = runtime_check.verify(clone, clone / ".harness" / "venv" / "bin" / "python")
        self.check("PC-R runtime_check reports the environment as unusable", runtime.get("ok") is False, str(runtime))
        self.expect_refusal(
            "ADM-E11 unpopulated runtime refused",
            self.record_path,
            "installed dependencies do not match",
            root=clone,
        )

    def smoke_case(self) -> None:
        """The baseline smoke must reject a flow that stops for the user."""
        import importlib.util

        spec = importlib.util.spec_from_file_location("admission_module", self.admission)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)

        fixture = self.out / "smoke-fixture"
        if fixture.exists():
            shutil.rmtree(fixture)
        (fixture / "scripts").mkdir(parents=True)
        run = fixture / "run"
        run.mkdir(parents=True)
        (fixture / "scripts" / "harness").write_text(f'#!/bin/sh\nprintf \'{{"created_run": "{run}"}}\\n\'\n')
        (fixture / "scripts" / "harness").chmod(0o755)
        for terminal, label, expected_ok in (("WAITING_USER", "ADM-PC-smoke stalls at WAITING_USER", False), ("DEMO_COMPLETE", "ADM-PC-smoke completes", True)):
            (run / "harness-result.json").write_text(
                json.dumps(
                    {
                        "status": terminal,
                        "evidence_level": "fixture",
                        "checks": [{"id": "addition", "status": "PASS", "exit_code": 0}],
                        "gates": [{"id": "complexity-gate", "result": {"status": "PASS"}}],
                    }
                )
            )
            result = module.public_baseline_smoke(fixture)
            self.check(label, result.get("ok") is expected_ok, str(result))


def main() -> int:
    parser = argparse.ArgumentParser(description="Controls for the go-engine admission gate")
    parser.add_argument("--record", required=True, help="the real acceptance record (its attempt is used as the fixture base)")
    parser.add_argument("--root", default=None, help="candidate worktree root")
    parser.add_argument("--out", required=True, help="directory for control fixtures and logs")
    args = parser.parse_args()

    root = Path(args.root).resolve() if args.root else Path(__file__).resolve().parents[2]
    out = Path(args.out).resolve()
    out.mkdir(parents=True, exist_ok=True)
    record = Path(args.record)
    if not record.is_file():
        print(f"control base record not found: {record}", file=sys.stderr)
        return 2
    record = record.resolve() if record.is_absolute() else (root / record).resolve()
    return Controls(root=root, record=record, out=out).run()


if __name__ == "__main__":
    sys.exit(main())
