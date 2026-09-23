"""Independent oracle: does the Go supervisor reproduce the reference's PATH search?

Compares the Go supervisor (`formal-gates harness supervise`) against the
pre-migration reference over the scenarios that decide WHICH program runs when a
bare `argv[0]` is resolved through `PATH`. Each side gets its own job directory;
`pid`/`started_at`/`finished_at` are dropped before comparing.

This oracle is deliberately independent of the delivered differential test: it was
written from the measured reference behaviour recorded by `reference_semantics.py`.

Usage (from the repository root):

    PYTHONPATH="$PWD/runtime/langgraph" TMPDIR=/private/tmp \
      .harness/venv/bin/python .gates/results/g2a-probes/path_search_oracle.py

Exit code 0 means every scenario agrees; 1 means at least one diverges.
"""
import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]
BINARY = str(REPO / ".harness" / "bin" / "formal-gates")
PYTHON = str(REPO / ".harness" / "venv" / "bin" / "python")
REFERENCE = str(REPO / "runtime" / "langgraph" / "tests" / "reference_job.py")
RUNTIME = str(REPO / "runtime" / "langgraph")
NONDETERMINISTIC = ("started_at", "finished_at")


def env_for(path):
    return {
        "PATH": path,
        "HOME": "/tmp",
        "TMPDIR": "/private/tmp",
        "PYTHONPATH": RUNTIME,
        "PYTHONDONTWRITEBYTECODE": "1",
        "FORMAL_GATES_HARNESS_BINARY": BINARY,
    }


def receipt(job):
    path = os.path.join(job, "exit.json")
    if not os.path.exists(path):
        return None
    with open(path) as f:
        value = json.load(f)
    for key in NONDETERMINISTIC:
        value.pop(key, None)
    return value


def run_pair(argv, cwd, path):
    go_job = tempfile.mkdtemp(prefix="or-go-")
    ref_job = tempfile.mkdtemp(prefix="or-ref-")
    request = {"kind": "check", "argv": argv, "cwd": cwd, "stdin": "",
               "environment": {}}
    for job in (go_job, ref_job):
        with open(os.path.join(job, "request.json"), "w") as f:
            json.dump(request, f)
    env = env_for(path)
    go_rc = subprocess.Popen([BINARY, "harness", "supervise", go_job], env=env,
                             stdout=subprocess.DEVNULL,
                             stderr=subprocess.DEVNULL).wait()
    ref_rc = subprocess.Popen([PYTHON, REFERENCE, ref_job], env=env,
                              stdout=subprocess.DEVNULL,
                              stderr=subprocess.DEVNULL).wait()
    return (go_rc, receipt(go_job)), (ref_rc, receipt(ref_job))


def build(work):
    def make(kind, name, tag, code=0):
        directory = os.path.join(work, tag)
        os.makedirs(directory, exist_ok=True)
        target = os.path.join(directory, name)
        if kind == "nonexec":
            with open(target, "w") as f:
                f.write("data\n")
            os.chmod(target, 0o644)
        elif kind == "dir":
            os.makedirs(target, exist_ok=True)
        elif kind == "enoexec":
            with open(target, "w") as f:
                f.write("no shebang\n")
            os.chmod(target, 0o755)
        elif kind == "good":
            with open(target, "w") as f:
                f.write(f"#!{PYTHON}\nimport sys; sys.exit({code})\n")
            os.chmod(target, 0o755)
        return directory

    scenarios = []
    for label, name, kinds, code in (
        ("A nonexec, then good",      "tA", ["nonexec", "good"], 41),
        ("B directory, then good",    "tB", ["dir", "good"], 42),
        ("V nonexec, then good (2)",  "tV", ["nonexec", "good"], 43),
        ("W enoexec, then good",      "tW", ["enoexec", "good"], 44),
        ("X directory, then good (2)", "tX", ["dir", "good"], 45),
        ("G only nonexec",            "tG", ["nonexec"], 0),
        ("H only directory",          "tH", ["dir"], 0),
        ("L only enoexec",            "tL", ["enoexec"], 0),
        ("M only nonexistent dir",    "tM", ["missing"], 0),
        ("N nonexec, then missing",   "tN", ["nonexec", "missing"], 0),
        ("O missing, then nonexec",   "tO", ["missing", "nonexec"], 0),
        ("P directory, then enoexec", "tP", ["dir", "enoexec"], 0),
        ("Q enoexec, then directory", "tQ", ["enoexec", "dir"], 0),
        ("S enoexec, then nonexec",   "tS", ["enoexec", "nonexec"], 0),
        ("T enoexec, then missing",   "tT", ["enoexec", "missing"], 0),
        ("U missing, then enoexec",   "tU", ["missing", "enoexec"], 0),
    ):
        entries = []
        for index, kind in enumerate(kinds):
            if kind == "missing":
                entries.append(os.path.join(work, "missing-%s-%d" % (name, index)))
            else:
                entries.append(make(kind, name, "%s-%d" % (name, index), code))
        scenarios.append((label, name, ":".join(entries)))
    return scenarios


def main():
    work = Path(tempfile.mkdtemp(prefix="g2a-oracle-"))
    diverged = 0
    scenarios = build(work)
    try:
        for label, name, path in scenarios:
            (go_rc, go), (ref_rc, ref) = run_pair([name], str(work), path)
            if go is None or ref is None:
                print(f"{label:28s} DIVERGE  missing receipt  go={go} ref={ref}")
                diverged += 1
                continue
            if go == ref and go_rc == ref_rc:
                print(f"{label:28s} agree    exit_code={go['exit_code']:>4}  "
                      f"error={go['error']!r}")
                continue
            diverged += 1
            print(f"{label:28s} DIVERGE")
            print(f"    GO  (rc={go_rc}): {go}")
            print(f"    REF (rc={ref_rc}): {ref}")
    finally:
        shutil.rmtree(work, ignore_errors=True)
    print(f"\n=== PATH search: {diverged} divergence(s) of {len(scenarios)} ===")
    return 1 if diverged else 0


if __name__ == "__main__":
    sys.exit(main())
