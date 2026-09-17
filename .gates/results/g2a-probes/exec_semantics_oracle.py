"""Independent oracle: execution-semantics corners the delivered test does not cover.

`path_search_oracle.py` settles WHICH candidate runs (the D1 defect). This one
covers the corners that decide the RECEIPT for cases neither that oracle nor the
delivered differential test exercises:

* a child that dies by its OWN signal (SIGTERM/SIGINT/SIGKILL/SIGHUP) - the
  `-N` exit codes. `-15` matters most: it is the very signal used for
  cancellation, so `waitChild`'s grace window is exactly where a wrong answer
  would hide;
* an EMPTY PATH component - POSIX execvp reads it as the current directory, and
  the Go implementation special-cases it (`dir == ""` -> `./name`);
* request-env PATH PRECEDENCE over the supervisor's own PATH when the same bare
  name exists on both sides;
* the defpath fallback for a name that exists nowhere;
* explicit paths (containing a separator), present and missing.

Written from the measured reference behaviour in `reference_semantics.py`. Each
side gets its own job directory; every path embedded in a request (argv, cwd, the
PATH used by the child) is SHARED, so the receipt - including the exact error text
- is directly comparable.

Usage (from the repository root):

    PYTHONPATH="$PWD/runtime/langgraph" TMPDIR=/private/tmp \
      .harness/venv/bin/python .gates/results/g2a-probes/exec_semantics_oracle.py

Exit code 0 means every case agrees; 1 means at least one diverges.
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


def proc_env(path=None, with_path=True):
    env = {
        "HOME": "/tmp",
        "TMPDIR": "/private/tmp",
        "PYTHONPATH": RUNTIME,
        "PYTHONDONTWRITEBYTECODE": "1",
        "FORMAL_GATES_HARNESS_BINARY": BINARY,
    }
    if with_path:
        env["PATH"] = path if path is not None else "/usr/bin:/bin"
    return env


def receipt(job):
    path = os.path.join(job, "exit.json")
    if not os.path.exists(path):
        return None
    with open(path) as f:
        value = json.load(f)
    for key in NONDETERMINISTIC:
        value.pop(key, None)
    return value


def run_pair(argv, cwd, request_env=None, path=None, with_path=True):
    go_job = tempfile.mkdtemp(prefix="ex-go-")
    ref_job = tempfile.mkdtemp(prefix="ex-ref-")
    request = {"kind": "check", "argv": argv, "cwd": cwd, "stdin": "",
               "environment": request_env or {}}
    for job in (go_job, ref_job):
        with open(os.path.join(job, "request.json"), "w") as f:
            json.dump(request, f)
    env = proc_env(path, with_path)
    go_rc = subprocess.Popen([BINARY, "harness", "supervise", go_job], env=env,
                             stdout=subprocess.DEVNULL,
                             stderr=subprocess.DEVNULL).wait()
    ref_rc = subprocess.Popen([PYTHON, REFERENCE, ref_job], env=env,
                              stdout=subprocess.DEVNULL,
                              stderr=subprocess.DEVNULL).wait()
    return (go_rc, receipt(go_job)), (ref_rc, receipt(ref_job))


def tool(directory, name, code):
    """A working `#!/bin/sh` tool that exits with `code`."""
    os.makedirs(directory, exist_ok=True)
    target = os.path.join(directory, name)
    with open(target, "w") as f:
        f.write("#!/bin/sh\nexit %d\n" % code)
    os.chmod(target, 0o755)
    return target


def main():
    work = Path(tempfile.mkdtemp(prefix="g2a-exec-"))
    cases = []
    try:
        # --- child dies by its own signal: the -N receipt -------------------
        for signame, number in (("TERM", 15), ("INT", 2), ("KILL", 9), ("HUP", 1)):
            cases.append((
                "child self-SIG%s -> -%d" % (signame, number),
                ["/bin/sh", "-c", "kill -%s $$" % signame], str(work), {}, "/usr/bin:/bin", True,
            ))
        cases.append(("explicit exit 130 -> 130 (not cancellation)",
                      ["/bin/sh", "-c", "exit 130"], str(work), {}, "/usr/bin:/bin", True))

        # --- empty PATH component means the current directory ---------------
        only = os.path.join(str(work), "only")
        tool(only, "cwdtool", 61)

        cases.append(("PATH ':' alone, tool in cwd", ["cwdtool"], only, {}, ":", True))
        cases.append(("PATH leading colon", ["cwdtool"], only, {}, ":" + only, True))
        cases.append(("PATH trailing colon", ["cwdtool"], only, {}, only + ":", True))
        cases.append(("PATH empty components only", ["cwdtool"], only, {}, "::", True))

        # a blocked candidate ahead of the empty component, then a good one in cwd
        blocked = os.path.join(str(work), "blocked")
        os.makedirs(blocked, exist_ok=True)
        with open(os.path.join(blocked, "cwdtool"), "w") as f:
            f.write("data\n")
        os.chmod(os.path.join(blocked, "cwdtool"), 0o644)
        cases.append(("blocked dir, then empty component (cwd)",
                      ["cwdtool"], only, {}, blocked + ":", True))

        # --- request PATH wins over the supervisor's own PATH ---------------
        req_tools = os.path.join(str(work), "reqtools")
        proc_tools = os.path.join(str(work), "proctools")
        tool(req_tools, "pref", 71)
        tool(proc_tools, "pref", 72)
        cases.append(("request PATH wins over process PATH",
                      ["pref"], str(work), {"PATH": req_tools}, proc_tools, True))
        cases.append(("process PATH used when request has none",
                      ["pref"], str(work), {}, proc_tools, True))

        # --- defpath fallback ------------------------------------------------
        cases.append(("no PATH anywhere -> defpath finds ls",
                      ["ls"], str(work), {}, None, False))
        cases.append(("no PATH anywhere -> defpath misses a bogus name",
                      ["definitely-not-a-tool-xyz"], str(work), {}, None, False))

        # --- explicit paths (a separator makes it a single candidate) --------
        explicit = os.path.join(str(work), "explicit_tool")
        with open(explicit, "w") as f:
            f.write("#!/bin/sh\nexit 77\n")
        os.chmod(explicit, 0o755)
        cases.append(("explicit relative ./tool runs",
                      ["./explicit_tool"], str(work), {}, "/usr/bin:/bin", True))
        cases.append(("explicit relative ./missing",
                      ["./no_such_explicit"], str(work), {}, "/usr/bin:/bin", True))
        cases.append(("explicit absolute missing",
                      ["/no/such/binary/xyz"], str(work), {}, "/usr/bin:/bin", True))

        diverged = 0
        for label, argv, cwd, request_env, path, with_path in cases:
            (go_rc, go), (ref_rc, ref) = run_pair(argv, cwd, request_env, path, with_path)
            if go is None or ref is None:
                diverged += 1
                print("%-46s DIVERGE  missing receipt  go=%r ref=%r" % (label, go, ref))
                continue
            if go == ref and go_rc == ref_rc:
                print("%-46s agree    exit_code=%4d  error=%r"
                      % (label, go["exit_code"], go["error"]))
                continue
            diverged += 1
            print("%-46s DIVERGE" % label)
            print("    GO  (rc=%d): %s" % (go_rc, go))
            print("    REF (rc=%d): %s" % (ref_rc, ref))
    finally:
        shutil.rmtree(work, ignore_errors=True)
    print("\n=== execution semantics: %d divergence(s) of %d ===" % (diverged, len(cases)))
    return 1 if diverged else 0


if __name__ == "__main__":
    sys.exit(main())
