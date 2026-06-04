#!/usr/bin/env python
"""Coarse diff-shape gate for overengineering risk.

The script is an alarm, not the judge. It separates hard failures from
review-required soft budget alarms so an agent can make a written judgment
without pretending line-count thresholds are design truth.
"""

from __future__ import print_function

import argparse
import io
import json
import os
import subprocess
import sys


PY2 = sys.version_info[0] == 2
if PY2:
    text_type = unicode  # noqa: F821  # pylint: disable=undefined-variable
else:
    text_type = str


PRODUCTION_EXTS = set([".c", ".cc", ".cpp", ".cxx", ".h", ".hpp", ".cs", ".py", ".ts", ".tsx", ".js", ".jsx"])
DOC_EXTS = set([".md", ".txt", ".rst"])
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


class FileChange(object):
    def __init__(self, path, insertions, deletions, status, category, suspicious_name):
        self.path = path
        self.insertions = insertions
        self.deletions = deletions
        self.status = status
        self.category = category
        self.suspicious_name = suspicious_name

    def to_dict(self):
        return {
            "path": self.path,
            "insertions": self.insertions,
            "deletions": self.deletions,
            "status": self.status,
            "category": self.category,
            "suspicious_name": self.suspicious_name,
        }


def as_text(value):
    if value is None:
        return u""
    if PY2 and isinstance(value, str):
        return value.decode("utf-8", "replace")
    if isinstance(value, text_type):
        return value
    return text_type(value)


def emit(value):
    value = as_text(value)
    if PY2:
        sys.stdout.write(value.encode("utf-8"))
        sys.stdout.write("\n")
    else:
        print(value)


def normalize_path(path):
    return path.replace("\\", "/")


def basename(path):
    return os.path.basename(normalize_path(path))


def path_ext(path):
    return os.path.splitext(path)[1].lower()


def worktree_path(worktree, relative_path):
    return os.path.join(worktree, relative_path.replace("/", os.sep))


def run_command(command, worktree, allow_failure=False):
    try:
        return subprocess.check_output(
            command,
            cwd=worktree,
            stderr=subprocess.PIPE,
            universal_newlines=True,
        )
    except subprocess.CalledProcessError as exc:
        if allow_failure:
            return ""
        stderr = getattr(exc, "stderr", None) or getattr(exc, "output", None) or ""
        if stderr:
            sys.stderr.write(as_text(stderr).encode("utf-8") if PY2 else as_text(stderr))
        raise SystemExit(exc.returncode)
    except OSError:
        if allow_failure:
            return ""
        raise


def run_git(args, worktree, allow_failure=False):
    return run_command(["git"] + args, worktree, allow_failure=allow_failure)


def run_svn(args, worktree, allow_failure=False):
    return run_command(["svn"] + args, worktree, allow_failure=allow_failure)


def detect_vcs(worktree, requested):
    if requested != "auto":
        return requested
    if run_git(["rev-parse", "--is-inside-work-tree"], worktree, allow_failure=True).strip().lower() == "true":
        return "git"
    if run_svn(["info"], worktree, allow_failure=True).strip():
        return "svn"
    return "none"


def categorize_path(path):
    ext = path_ext(path)
    lower = path.lower()
    if ext in DOC_EXTS:
        return "doc"
    if any(hint in lower for hint in TEST_HINTS):
        return "test"
    if ext in PRODUCTION_EXTS:
        return "production"
    return "other"


def make_file_change(path, insertions, deletions, status):
    path = normalize_path(path)
    category = categorize_path(path)
    suspicious = any(term in basename(path) for term in SUSPICIOUS_TERMS)
    return FileChange(path, insertions, deletions, status, category, suspicious)


def parse_numstat_git(worktree, staged):
    args = ["diff", "--numstat"]
    if staged:
        args.append("--cached")
    raw = run_git(args, worktree)
    status_raw = run_git(["status", "--short"], worktree)
    statuses = {}
    for line in status_raw.splitlines():
        if not line.strip():
            continue
        status = line[:2].strip()
        path = line[3:]
        if " -> " in path:
            path = path.split(" -> ", 1)[1]
        statuses[normalize_path(path)] = status

    changes = []
    for line in raw.splitlines():
        parts = line.split("\t")
        if len(parts) < 3:
            continue
        ins_raw, del_raw, path_raw = parts[0], parts[1], parts[2]
        path = normalize_path(path_raw)
        if " => " in path:
            path = path.split(" => ", 1)[1]
        insertions = 0 if ins_raw == "-" else int(ins_raw)
        deletions = 0 if del_raw == "-" else int(del_raw)
        changes.append(make_file_change(path, insertions, deletions, statuses.get(path, "")))
    return changes


def parse_untracked_git(worktree):
    raw = run_git(["ls-files", "--others", "--exclude-standard"], worktree)
    return [normalize_path(line) for line in raw.splitlines() if line.strip()]


def parse_svn_status(worktree):
    raw = run_svn(["status"], worktree)
    statuses = {}
    untracked = []
    for line in raw.splitlines():
        if not line.strip():
            continue
        status = line[0]
        path = line[8:].strip() if len(line) > 8 else line[1:].strip()
        if not path:
            continue
        path = normalize_path(path)
        if status == "?":
            untracked.append(path)
        elif status in set(["A", "M", "D", "R", "C", "!", "~"]):
            statuses[path] = status
    return statuses, untracked


def parse_svn_diff(worktree, statuses):
    raw = run_svn(["diff"], worktree)
    counts = {}
    current = None

    for line in raw.splitlines():
        if line.startswith("Index: "):
            current = normalize_path(line[len("Index: "):].strip())
            counts.setdefault(current, {"insertions": 0, "deletions": 0})
            continue
        if not current:
            continue
        if line.startswith("+++") or line.startswith("---"):
            continue
        if line.startswith("+"):
            counts[current]["insertions"] += 1
        elif line.startswith("-"):
            counts[current]["deletions"] += 1

    changes = []
    for path, values in counts.items():
        changes.append(make_file_change(path, values["insertions"], values["deletions"], statuses.get(path, "M")))
    for path, status in statuses.items():
        if path not in counts:
            changes.append(make_file_change(path, 0, 0, status))
    return changes


def count_file_lines(path):
    try:
        with io.open(path, "r", encoding="utf-8", errors="ignore") as handle:
            return sum(1 for _ in handle)
    except (IOError, OSError):
        return 0


def main():
    parser = argparse.ArgumentParser(description="Check diff shape against a complexity budget.")
    parser.add_argument("--task-type", required=True, choices=sorted(FALLBACK_DEFAULTS))
    parser.add_argument("--max-net", type=int)
    parser.add_argument("--max-new-prod-files", type=int)
    parser.add_argument("--max-prod-insertions", type=int)
    parser.add_argument("--staged", action="store_true")
    parser.add_argument("--json", action="store_true")
    parser.add_argument("--worktree", default=os.getcwd())
    parser.add_argument("--vcs", choices=("auto", "git", "svn"), default="auto")
    args = parser.parse_args()
    worktree = os.path.abspath(args.worktree)

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

    vcs = detect_vcs(worktree, args.vcs)
    manual_review_reason = ""
    if vcs == "git":
        changes = parse_numstat_git(worktree, args.staged)
        untracked = parse_untracked_git(worktree) if not args.staged else []
    elif vcs == "svn":
        statuses, svn_untracked = parse_svn_status(worktree)
        changes = parse_svn_diff(worktree, statuses)
        untracked = [] if args.staged else svn_untracked
        if args.staged:
            manual_review_reason = "SVN has no staged index; --staged was ignored for SVN complexity review"
    else:
        changes = []
        untracked = []
        manual_review_reason = "no git or svn working copy detected; provide manual diff evidence for complexity review"

    total_insertions = sum(c.insertions for c in changes)
    total_deletions = sum(c.deletions for c in changes)
    net = total_insertions - total_deletions
    untracked_prod_files = [p for p in untracked if categorize_path(p) == "production"]
    untracked_prod_insertions = sum(count_file_lines(worktree_path(worktree, p)) for p in untracked_prod_files)
    prod_insertions = sum(c.insertions for c in changes if c.category == "production") + untracked_prod_insertions
    new_prod_files = [
        c.path
        for c in changes
        if c.category == "production" and c.status in set(["A", "??"])
    ] + untracked_prod_files
    suspicious = [
        c.path
        for c in changes
        if c.suspicious_name and c.status in set(["A", "??"])
    ]
    suspicious.extend(
        p for p in untracked if path_ext(p) in PRODUCTION_EXTS and any(term in basename(p) for term in SUSPICIOUS_TERMS)
    )

    hard_failures = []
    review_required = []
    warnings = []

    if changes and net > budget["max_net"]:
        hard_failures.append("net diff {0} exceeds budget {1} for {2}".format(net, budget["max_net"], args.task_type))
    if prod_insertions > budget["max_prod_insertions"]:
        review_required.append(
            "production insertions {0} exceed budget {1}".format(prod_insertions, budget["max_prod_insertions"])
        )
    if len(new_prod_files) > budget["max_new_prod_files"]:
        hard_failures.append(
            "new production files {0} exceed budget {1}: {2}".format(
                len(new_prod_files),
                budget["max_new_prod_files"],
                ", ".join(new_prod_files[:8]),
            )
        )
    if suspicious and args.task_type != "new-system":
        review_required.append("suspicious subsystem-like new names: " + ", ".join(suspicious[:12]))
    if untracked:
        warnings.append("untracked files present: " + ", ".join(untracked[:12]))
    if not any(budget_overrides.values()):
        warnings.append("using fallback default budget; explicit Complexity Contract budget was not passed")
    if manual_review_reason:
        review_required.append(manual_review_reason)

    status = "FAIL" if hard_failures else ("REVIEW" if review_required else "PASS")

    payload = {
        "status": status,
        "vcs": vcs,
        "worktree": worktree,
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
            c.to_dict()
            for c in sorted(changes, key=lambda item: item.insertions + item.deletions, reverse=True)[:10]
        ],
    }

    if args.json:
        emit(json.dumps(payload, indent=2, ensure_ascii=False))
    else:
        emit("Complexity Gate: {0}".format(payload["status"]))
        emit(
            "insertions={0} deletions={1} net={2} prod_insertions={3} changed_files={4} untracked={5}".format(
                total_insertions,
                total_deletions,
                net,
                prod_insertions,
                len(changes),
                len(untracked),
            )
        )
        emit("budget_source={0}".format(payload["budget_source"]))
        for failure in hard_failures:
            emit("FAIL: {0}".format(failure))
        for item in review_required:
            emit("REVIEW: {0}".format(item))
        for warning in warnings:
            emit("WARN: {0}".format(warning))
        if changes:
            emit("Largest files:")
            for change in payload["largest_files"]:
                emit("  {0:>5} + {1:>5} - {2} [{3}]".format(
                    change["insertions"],
                    change["deletions"],
                    change["path"],
                    change["category"],
                ))

    if hard_failures:
        return 1
    if review_required:
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
