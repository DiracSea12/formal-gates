#!/usr/bin/env python3
"""Coarse diff-shape gate for overengineering risk.

The script is an alarm, not the judge. It separates hard failures from
review-required soft budget alarms so an agent can make a written judgment
without pretending line-count thresholds are design truth.
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from dataclasses import asdict, dataclass
from pathlib import Path


if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")
if hasattr(sys.stderr, "reconfigure"):
    sys.stderr.reconfigure(encoding="utf-8")


PRODUCTION_EXTS = {".c", ".cc", ".cpp", ".cxx", ".h", ".hpp", ".cs", ".py", ".ts", ".tsx", ".js", ".jsx"}
DOC_EXTS = {".md", ".txt", ".rst"}
TEST_HINTS = ("test", "tests", "spec", "automation")
SUSPICIOUS_TERMS = (
    "Manager",
    "Service",
    "Report",
    "Evidence",
    "Policy",
    "Registry",
    "Cache",
    "Context",
    "Provider",
    "Orchestrator",
)


FALLBACK_DEFAULTS = {
    "delete-or-consolidate": {"max_net": -1, "max_new_prod_files": 0, "max_prod_insertions": 300},
    "bugfix": {"max_net": 250, "max_new_prod_files": 0, "max_prod_insertions": 300},
    "small-feature": {"max_net": 600, "max_new_prod_files": 2, "max_prod_insertions": 700},
    "refactor": {"max_net": 200, "max_new_prod_files": 2, "max_prod_insertions": 700},
    "new-system": {"max_net": 5000, "max_new_prod_files": 20, "max_prod_insertions": 5000},
}


@dataclass
class FileChange:
    path: str
    insertions: int
    deletions: int
    status: str
    category: str
    suspicious_name: bool


def run_git(args: list[str]) -> str:
    try:
        return subprocess.check_output(["git", *args], text=True, stderr=subprocess.PIPE)
    except subprocess.CalledProcessError as exc:
        sys.stderr.write(exc.stderr)
        raise SystemExit(exc.returncode)


def parse_numstat(staged: bool) -> list[FileChange]:
    args = ["diff", "--numstat", "--cached" if staged else None]
    raw = run_git([a for a in args if a])
    status_raw = run_git(["status", "--short"])
    statuses: dict[str, str] = {}
    for line in status_raw.splitlines():
        if not line.strip():
            continue
        status = line[:2].strip()
        path = line[3:]
        if " -> " in path:
            path = path.split(" -> ", 1)[1]
        statuses[path.replace("\\", "/")] = status

    changes: list[FileChange] = []
    for line in raw.splitlines():
        parts = line.split("\t")
        if len(parts) < 3:
            continue
        ins_raw, del_raw, path_raw = parts[0], parts[1], parts[2]
        path = path_raw.replace("\\", "/")
        if " => " in path:
            path = path.split(" => ", 1)[1]
        insertions = 0 if ins_raw == "-" else int(ins_raw)
        deletions = 0 if del_raw == "-" else int(del_raw)
        ext = Path(path).suffix.lower()
        lower = path.lower()
        if ext in DOC_EXTS:
            category = "doc"
        elif any(hint in lower for hint in TEST_HINTS):
            category = "test"
        elif ext in PRODUCTION_EXTS:
            category = "production"
        else:
            category = "other"
        name = Path(path).name
        suspicious = any(term in name for term in SUSPICIOUS_TERMS)
        changes.append(FileChange(path, insertions, deletions, statuses.get(path, ""), category, suspicious))
    return changes


def parse_untracked() -> list[str]:
    raw = run_git(["ls-files", "--others", "--exclude-standard"])
    return [line.replace("\\", "/") for line in raw.splitlines() if line.strip()]


def categorize_path(path: str) -> str:
    ext = Path(path).suffix.lower()
    lower = path.lower()
    if ext in DOC_EXTS:
        return "doc"
    if any(hint in lower for hint in TEST_HINTS):
        return "test"
    if ext in PRODUCTION_EXTS:
        return "production"
    return "other"


def count_file_lines(path: str) -> int:
    try:
        with open(path, "r", encoding="utf-8", errors="ignore") as handle:
            return sum(1 for _ in handle)
    except OSError:
        return 0


def main() -> int:
    parser = argparse.ArgumentParser(description="Check diff shape against a complexity budget.")
    parser.add_argument("--task-type", required=True, choices=sorted(FALLBACK_DEFAULTS))
    parser.add_argument("--max-net", type=int)
    parser.add_argument("--max-new-prod-files", type=int)
    parser.add_argument("--max-prod-insertions", type=int)
    parser.add_argument("--staged", action="store_true")
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args()

    budget = dict(FALLBACK_DEFAULTS[args.task_type])
    budget_overrides = {
        "max_net": args.max_net is not None,
        "max_new_prod_files": args.max_new_prod_files is not None,
        "max_prod_insertions": args.max_prod_insertions is not None,
    }
    if args.max_net is not None:
        budget["max_net"] = args.max_net
    if args.max_new_prod_files is not None:
        budget["max_new_prod_files"] = args.max_new_prod_files
    if args.max_prod_insertions is not None:
        budget["max_prod_insertions"] = args.max_prod_insertions

    changes = parse_numstat(args.staged)
    untracked = parse_untracked() if not args.staged else []

    total_insertions = sum(c.insertions for c in changes)
    total_deletions = sum(c.deletions for c in changes)
    net = total_insertions - total_deletions
    untracked_prod_files = [p for p in untracked if categorize_path(p) == "production"]
    untracked_prod_insertions = sum(count_file_lines(p) for p in untracked_prod_files)
    prod_insertions = sum(c.insertions for c in changes if c.category == "production") + untracked_prod_insertions
    new_prod_files = [
        c.path
        for c in changes
        if c.category == "production" and c.status in {"A", "??"}
    ] + untracked_prod_files
    suspicious = [
        c.path
        for c in changes
        if c.suspicious_name and c.status in {"A", "??"}
    ]
    suspicious.extend(
        p for p in untracked if Path(p).suffix.lower() in PRODUCTION_EXTS and any(term in Path(p).name for term in SUSPICIOUS_TERMS)
    )

    hard_failures: list[str] = []
    review_required: list[str] = []
    warnings: list[str] = []

    if changes and net > budget["max_net"]:
        hard_failures.append(f"net diff {net} exceeds budget {budget['max_net']} for {args.task_type}")
    if prod_insertions > budget["max_prod_insertions"]:
        review_required.append(
            f"production insertions {prod_insertions} exceed budget {budget['max_prod_insertions']}"
        )
    if len(new_prod_files) > budget["max_new_prod_files"]:
        hard_failures.append(
            f"new production files {len(new_prod_files)} exceed budget {budget['max_new_prod_files']}: {', '.join(new_prod_files[:8])}"
        )
    if suspicious and args.task_type != "new-system":
        review_required.append("suspicious subsystem-like new names: " + ", ".join(suspicious[:12]))
    if untracked:
        warnings.append("untracked files present: " + ", ".join(untracked[:12]))
    if not any(budget_overrides.values()):
        warnings.append("using fallback default budget; explicit Complexity Contract budget was not passed")

    status = "FAIL" if hard_failures else ("REVIEW" if review_required else "PASS")

    payload = {
        "status": status,
        "task_type": args.task_type,
        "budget": budget,
        "budget_source": "explicit-overrides" if any(budget_overrides.values()) else "fallback-defaults",
        "budget_overrides": budget_overrides,
        "summary": {
            "insertions": total_insertions,
            "deletions": total_deletions,
            "net": net,
            "production_insertions": prod_insertions,
            "new_production_files": len(new_prod_files),
            "untracked_production_files": len(untracked_prod_files),
            "untracked_production_insertions": untracked_prod_insertions,
            "changed_files": len(changes),
            "untracked_files": len(untracked),
        },
        "failures": hard_failures,
        "review_required": review_required,
        "warnings": warnings,
        "largest_files": [
            asdict(c)
            for c in sorted(changes, key=lambda item: item.insertions + item.deletions, reverse=True)[:10]
        ],
    }

    if args.json:
        print(json.dumps(payload, indent=2, ensure_ascii=False))
    else:
        print(f"Complexity Gate: {payload['status']}")
        print(
            f"insertions={total_insertions} deletions={total_deletions} net={net} "
            f"prod_insertions={prod_insertions} changed_files={len(changes)} untracked={len(untracked)}"
        )
        print(f"budget_source={payload['budget_source']}")
        for failure in hard_failures:
            print(f"FAIL: {failure}")
        for item in review_required:
            print(f"REVIEW: {item}")
        for warning in warnings:
            print(f"WARN: {warning}")
        if changes:
            print("Largest files:")
            for c in payload["largest_files"]:
                print(f"  {c['insertions']:>5} + {c['deletions']:>5} - {c['path']} [{c['category']}]")

    if hard_failures:
        return 1
    if review_required:
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
