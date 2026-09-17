"""Reference implementation of the pre-migration job supervisor (formal_gates_harness.job).

Test-only. Production code must not import this module. The body below is the
pre-migration implementation from formal_gates_harness/job.py (baseline M3),
moved verbatim so the G2a differential can prove the Go supervisor
(`internal/harness.Supervise`) preserves its external behaviour byte for byte.

The original module was spawned as `python -m formal_gates_harness.job <dir>` and
relied on a package-relative import (`from .infra import ...`). To run as an
independent process from this test directory it uses the absolute import
`formal_gates_harness.infra`; PYTHONPATH must contain
`<repo>/runtime/langgraph` (the differential test sets this). The `main(Path)`
signature and the `if __name__ == "__main__"` guard are preserved exactly so both
sides of the differential run as their own process - the supervisor's whole point
is process ownership, which an in-process call cannot demonstrate.
"""
from __future__ import annotations
import os
from pathlib import Path
import signal
import subprocess
import sys
import time
from formal_gates_harness.infra import atomic, digest, read, file_proof

def main(directory: Path) -> int:
    request = read(directory / "request.json")
    try: fd = os.open(directory / "owner", os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    except FileExistsError: return 2
    os.close(fd)
    atomic(directory / "process.json", {"pid": os.getpid(), "started_at": time.time()})
    started = time.time(); code = 127; error = ""; child = None
    def terminate(signum, frame):
        if child is not None:
            try: child.terminate()
            except ProcessLookupError: pass
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, terminate)
    env = os.environ.copy(); env.update(request.get("environment", {}))
    try:
        with (directory / "stdout.log").open("wb") as out, (directory / "stderr.log").open("wb") as err:
            child = subprocess.Popen(request["argv"], cwd=request["cwd"], env=env,
                                     stdin=subprocess.PIPE, stdout=out, stderr=err)
            child.communicate(request.get("stdin", "").encode())
            code = child.returncode
            out.flush(); err.flush(); os.fsync(out.fileno()); os.fsync(err.fileno())
    except KeyboardInterrupt:
        code = 130; error = "owned job was cancelled"
        if child is not None:
            try: child.wait(timeout=5)
            except subprocess.TimeoutExpired: child.kill(); child.wait()
    except Exception as exc:
        error = f"{type(exc).__name__}: {exc}"
    atomic(directory / "exit.json", {"exit_code": code, "error": error, "started_at": started,
                                     "finished_at": time.time(), "request_digest": digest(request),
                                     "streams": {name: file_proof(directory / name)
                                                 for name in ('stdout.log', 'stderr.log')}})
    return 0

if __name__ == "__main__":
    raise SystemExit(main(Path(sys.argv[1])))
