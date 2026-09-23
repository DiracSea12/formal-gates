"""Measured reference semantics for G2a (the pre-migration job supervisor).

Runs the pre-migration supervisor -- kept verbatim as
`runtime/langgraph/tests/reference_job.py` -- and prints the receipt it produces
for each boundary case. This is the evidence behind the "迁移前语义（已实测）"
tables in `.gates/results/go-engine-G2a-contract.md`; it exists so a reviewer can
re-measure rather than trust the prose.

Usage (from the repository root):

    PYTHONPATH="$PWD/runtime/langgraph" TMPDIR=/private/tmp \
      .harness/venv/bin/python .gates/results/g2a-probes/reference_semantics.py
"""
import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]
PYTHON = str(REPO / ".harness" / "venv" / "bin" / "python")
REFERENCE = str(REPO / "runtime" / "langgraph" / "tests" / "reference_job.py")
RUNTIME = str(REPO / "runtime" / "langgraph")


def base_env(**extra):
    env = {
        "PATH": "/usr/bin:/bin",
        "HOME": "/tmp",
        "TMPDIR": "/private/tmp",
        "PYTHONPATH": RUNTIME,
        "PYTHONDONTWRITEBYTECODE": "1",
    }
    env.update(extra)
    return env


def supervise(job, env, argv, cwd, stdin=""):
    """Run the reference supervisor once and return (supervisor_rc, receipt)."""
    request = {"kind": "check", "argv": argv, "cwd": cwd, "stdin": stdin,
               "environment": {}}
    with open(os.path.join(job, "request.json"), "w") as f:
        json.dump(request, f)
    rc = subprocess.Popen([PYTHON, REFERENCE, job], env=env,
                          stdout=subprocess.DEVNULL,
                          stderr=subprocess.DEVNULL).wait()
    receipt = None
    path = os.path.join(job, "exit.json")
    if os.path.exists(path):
        with open(path) as f:
            receipt = json.load(f)
    return rc, receipt


def show(label, rc, receipt):
    if receipt is None:
        print(f"{label:44s} supervisor_rc={rc}  NO RECEIPT")
    else:
        print(f"{label:44s} supervisor_rc={rc}  exit_code={receipt['exit_code']:>4}  "
              f"error={receipt['error']!r}")


def write(path, body, mode=0o644):
    with open(path, "w") as f:
        f.write(body)
    os.chmod(path, mode)
    return path


def section(title):
    print(f"\n--- {title} ---")


def exit_codes(work):
    section("exit code shapes")
    cases = [
        ("exits 0", ["/usr/bin/true"]),
        ("exits 7", ["/bin/sh", "-c", "exit 7"]),
        ("explicit exit 130 (not cancellation)", ["/bin/sh", "-c", "exit 130"]),
        ("child self-SIGKILL", ["/bin/sh", "-c", "kill -KILL $$"]),
        ("child self-SIGTERM", ["/bin/sh", "-c", "kill -TERM $$"]),
        ("child self-SIGINT", ["/bin/sh", "-c", "kill -INT $$"]),
    ]
    for label, argv in cases:
        job = tempfile.mkdtemp(prefix="ref-exit-")
        show(label, *supervise(job, base_env(), argv, str(work)))


def spawn_failures(work):
    section("spawn failures (explicit path)")
    noexec = write(os.path.join(work, "noexec"), "data\n", 0o644)
    txt = write(os.path.join(work, "txtscript"), "not a binary\n", 0o755)
    adir = os.path.join(work, "adir")
    os.makedirs(adir, exist_ok=True)
    cases = [
        ("argv0 missing", ["/no/such/binary/xyz"], str(work)),
        ("argv0 not executable", [noexec], str(work)),
        ("argv0 is a directory", [adir], str(work)),
        ("argv0 +x text (exec format)", [txt], str(work)),
        ("cwd missing", ["/usr/bin/true"], os.path.join(work, "nope")),
        ("cwd is a file", ["/usr/bin/true"], noexec),
    ]
    for label, argv, cwd in cases:
        job = tempfile.mkdtemp(prefix="ref-fail-")
        show(label, *supervise(job, base_env(), argv, cwd))


def path_search(work):
    section("PATH search (bare argv0): does the search continue past a candidate "
            "that exists but cannot execute?")

    def mk(kind, name, tag, code=0):
        d = os.path.join(work, tag)
        os.makedirs(d, exist_ok=True)
        target = os.path.join(d, name)
        if kind == "nonexec":
            write(target, "data\n", 0o644)
        elif kind == "dir":
            os.makedirs(target, exist_ok=True)
        elif kind == "enoexec":
            write(target, "no shebang\n", 0o755)
        elif kind == "good":
            write(target, f"#!{PYTHON}\nimport sys; sys.exit({code})\n", 0o755)
        return d

    scenarios = [
        ("A nonexec, then good",          "tA", ["nonexec", "good"], 41),
        ("B directory, then good",        "tB", ["dir", "good"], 42),
        ("W enoexec, then good",          "tW", ["enoexec", "good"], 44),
        ("G only nonexec",                "tG", ["nonexec"], 0),
        ("H only directory",              "tH", ["dir"], 0),
        ("L only enoexec",                "tL", ["enoexec"], 0),
        ("M only nonexistent dir",        "tM", ["missing"], 0),
        ("N nonexec, then missing",       "tN", ["nonexec", "missing"], 0),
        ("O missing, then nonexec",       "tO", ["missing", "nonexec"], 0),
        ("P directory, then enoexec",     "tP", ["dir", "enoexec"], 0),
        ("Q enoexec, then directory",     "tQ", ["enoexec", "dir"], 0),
        ("S enoexec, then nonexec",       "tS", ["enoexec", "nonexec"], 0),
        ("T enoexec, then missing",       "tT", ["enoexec", "missing"], 0),
        ("U missing, then enoexec",       "tU", ["missing", "enoexec"], 0),
    ]
    for label, name, kinds, code in scenarios:
        entries = []
        for i, kind in enumerate(kinds):
            entries.append(os.path.join(work, "missing-%s-%d" % (name, i))
                           if kind == "missing" else mk(kind, name, "%s-%d" % (name, i), code))
        job = tempfile.mkdtemp(prefix="ref-path-")
        show(label, *supervise(job, base_env(PATH=":".join(entries)),
                              [name], str(work)))

    section("PATH resolution side and fallback")
    tools = mk("good", "pathonly", "pathonly", 51)
    job = tempfile.mkdtemp(prefix="ref-path-")
    show("bare argv0 only in request PATH (request env wins)",
         *supervise(job, base_env(PATH="/usr/bin:/bin"), ["pathonly"], str(work)))
    # the request environment carries PATH; the supervisor's own PATH must not decide
    request_env = {"PATH": tools}
    receipt_job = tempfile.mkdtemp(prefix="ref-path-")
    request = {"kind": "check", "argv": ["pathonly"], "cwd": str(work),
               "stdin": "", "environment": request_env}
    with open(os.path.join(receipt_job, "request.json"), "w") as f:
        json.dump(request, f)
    rc = subprocess.Popen([PYTHON, REFERENCE, receipt_job],
                          env=base_env(PATH="/usr/bin:/bin"),
                          stdout=subprocess.DEVNULL,
                          stderr=subprocess.DEVNULL).wait()
    show("bare argv0 found via the REQUEST env PATH",
         rc, json.load(open(os.path.join(receipt_job, "exit.json"))))

    job = tempfile.mkdtemp(prefix="ref-path-")
    env = base_env()
    env.pop("PATH", None)
    show("no PATH anywhere -> defpath fallback", *supervise(job, env, ["ls"], str(work)))


def owner_and_cancel():
    section("owner lock")
    job = tempfile.mkdtemp(prefix="ref-owner-")
    request = {"kind": "check", "argv": ["/usr/bin/true"], "cwd": job,
               "stdin": "", "environment": {}}
    with open(os.path.join(job, "request.json"), "w") as f:
        json.dump(request, f)
    write(os.path.join(job, "owner"), "")
    rc = subprocess.Popen([PYTHON, REFERENCE, job], env=base_env(),
                          stdout=subprocess.DEVNULL,
                          stderr=subprocess.DEVNULL).wait()
    print(f"pre-existing owner{'':27s} supervisor_rc={rc}  "
          f"exit.json={os.path.exists(os.path.join(job, 'exit.json'))}  "
          f"process.json={os.path.exists(os.path.join(job, 'process.json'))}")


def main():
    work = Path(tempfile.mkdtemp(prefix="g2a-ref-"))
    try:
        exit_codes(work)
        spawn_failures(work)
        path_search(work)
        owner_and_cancel()
    finally:
        import shutil
        shutil.rmtree(work, ignore_errors=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
