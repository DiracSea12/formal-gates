"""Independent oracle: cancellation semantics of the Go supervisor.

The delivered differential test signals only the supervisor *process*; production
(`Store.cancel_jobs`) signals the supervisor's whole process **group**, which also
reaches the child. This oracle exercises the production shape.

Three checks:

1. **Group SIGTERM while the supervisor is waiting.** The signal is sent only
   after POSITIVE proof that the supervisor is past handler installation - the
   child touches a marker file, and handler installation strictly precedes the
   first spawn (see `child_marker_job`). Waiting for `process.json` instead, or
   sleeping a fixed amount after it, races the startup window and is what made
   one delivered cancellation test fail under full-suite load. Two extra delays
   after the marker are probed (0.0 = as early as provably safe, 0.4 = later);
   both sides must be stable and correct in every round.
2. **`cancel_jobs` must signal only what it owns.** Two real jobs started through
   `Store.job` (the production entry point, which spawns the supervisor with
   `start_new_session=True`) plus a sentinel that is a process-group leader but is
   not a registered job. After `cancel_jobs()` both jobs must carry the
   cancellation receipt and the sentinel must survive.
3. **Startup window (informational, not pass/fail).** `process.json` is written
   before the SIGTERM handler is installed, so a group signal arriving in that
   window kills the supervisor with no receipt at all. That is the *reference*
   behaviour, inherited rather than migrated: `Store.job` maps it to
   `RuntimeBlocked(UNKNOWN_JOB)`. This check signals immediately after
   `process.json` (`marker=False`) and only reports how often each side lands in
   that window; it does not fail either side.

Usage (from the repository root):

    PYTHONPATH="$PWD/runtime/langgraph" TMPDIR=/private/tmp \
      .harness/venv/bin/python .gates/results/g2a-probes/cancel_oracle.py

Exit code 0 = checks 1 and 2 pass.
"""
import json
import os
import signal
import subprocess
import sys
import tempfile
import threading
import time
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]
BINARY = str(REPO / ".harness" / "bin" / "formal-gates")
PYTHON = str(REPO / ".harness" / "venv" / "bin" / "python")
REFERENCE = str(REPO / "runtime" / "langgraph" / "tests" / "reference_job.py")
RUNTIME = str(REPO / "runtime" / "langgraph")

ROUNDS = 10
SCOPE_ROUNDS = 3
EXTRA_DELAYS = (0.0, 0.4)
EXPECTED_CODE = 130
EXPECTED_ERROR = "owned job was cancelled"

IMPLEMENTATIONS = (
    ("REFERENCE", lambda job: [PYTHON, REFERENCE, job]),
    ("GO", lambda job: [BINARY, "harness", "supervise", job]),
)


def env():
    return {
        "PATH": "/usr/bin:/bin",
        "HOME": "/tmp",
        "TMPDIR": "/private/tmp",
        "PYTHONPATH": RUNTIME,
        "PYTHONDONTWRITEBYTECODE": "1",
        "FORMAL_GATES_HARNESS_BINARY": BINARY,
    }


def wait_for(path, timeout=10.0):
    deadline = time.time() + timeout
    while time.time() < deadline:
        if os.path.exists(path):
            return True
        time.sleep(0.01)
    return False


def spawn_supervised(cmd, marked=True):
    """Start one supervisor; return `(job, proc, marker_path_or_None)`.

    With `marked=True` the request's child touches `job/child-started` before
    exec'ing the long sleep, so the caller can wait for POSITIVE proof that the
    supervisor is already in its wait loop.

    That proof has to be the child, not `process.json`: `process.json` is written
    BEFORE the SIGTERM handler is armed - `supervise.go` writes the file and arms
    the channel only when it enters the wait loop; `reference_job.py` writes the
    file on line 31 and calls `signal.signal` on line 38. Waiting for
    `process.json`, or sleeping a fixed amount after it, therefore races that
    window, and a group signal landing inside it kills the supervisor outright
    (no receipt, `signal: terminated`). Handler arming strictly precedes the
    first spawn, so a running child is sufficient evidence.
    """
    job = tempfile.mkdtemp(prefix="gc-")
    marker = os.path.join(job, "child-started") if marked else None
    argv = (["/bin/sh", "-c", "touch " + marker + " && exec /bin/sleep 30"]
            if marked else ["/bin/sleep", "30"])
    with open(os.path.join(job, "request.json"), "w") as f:
        json.dump({"kind": "check", "argv": argv, "cwd": job,
                   "stdin": "", "environment": {}}, f)
    proc = subprocess.Popen(cmd(job), env=env(), stdout=subprocess.DEVNULL,
                            stderr=subprocess.DEVNULL, start_new_session=True)
    wait_for(os.path.join(job, "process.json"))
    return job, proc, marker


def wait_receipt(job, timeout=10.0):
    path = os.path.join(job, "exit.json")
    deadline = time.time() + timeout
    while time.time() < deadline:
        if os.path.exists(path):
            for _ in range(20):  # the writer publishes atomically; be tolerant
                try:
                    with open(path) as f:
                        return json.load(f)
                except (ValueError, FileNotFoundError):
                    time.sleep(0.01)
        time.sleep(0.02)
    return None


def group_signal(cmd, rounds, delay, marked=True):
    """Group-signal the supervisor once it is provably in its wait loop.

    `marked=True` waits for the child's marker before signalling, then waits
    `delay` more seconds. `marked=False` signals as soon as `process.json`
    appears - used only to MEASURE the startup window (check 3).
    """
    outcomes = []
    for _ in range(rounds):
        job, proc, marker = spawn_supervised(cmd, marked)
        if marker is not None and not wait_for(marker):
            outcomes.append(("marker-timeout", None, None))
            proc.kill()
            proc.wait()
            continue
        if delay:
            time.sleep(delay)
        try:
            os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
        except ProcessLookupError:
            pass
        rc = proc.wait()
        receipt = wait_receipt(job)
        if receipt is None:
            outcomes.append((rc, None, None))
        else:
            outcomes.append((rc, receipt["exit_code"], receipt["error"]))
    return outcomes


def check_group_signal():
    print(f"--- group SIGTERM after the child is provably running: "
          f"{ROUNDS} rounds per extra delay ---")
    ok = True
    for label, cmd in IMPLEMENTATIONS:
        for delay in EXTRA_DELAYS:
            outcomes = group_signal(cmd, ROUNDS, delay)
            codes = sorted({code for _, code, _ in outcomes})
            stable = len(codes) == 1 and codes[0] is not None
            correct = outcomes[0][1:] == (EXPECTED_CODE, EXPECTED_ERROR)
            print(f"  {label:10s} extra_delay={delay:<5} distinct exit_codes={codes}  "
                  f"{'STABLE' if stable else '*** UNSTABLE ***'}  "
                  f"receipt={outcomes[0][1:]!r}")
            if not stable:
                for rc, code, error in outcomes:
                    print(f"      round: supervisor_rc={rc} exit_code={code} "
                          f"error={error!r}")
            ok &= stable and correct
    return ok


def check_cancel_scope():
    print(f"--- cancel_jobs signals only owned jobs ({SCOPE_ROUNDS} rounds) ---")
    sys.path.insert(0, RUNTIME)
    os.environ["FORMAL_GATES_HARNESS_BINARY"] = BINARY
    from formal_gates_harness.infra import Bridge, Store

    ok = True
    for round_index in range(SCOPE_ROUNDS):
        root = Path(tempfile.mkdtemp(prefix="g2a-cancel-"))
        store = Store(root, {}, Bridge(BINARY))
        sentinel = subprocess.Popen(["/bin/sleep", "60"], start_new_session=True)
        is_leader = os.getpgid(sentinel.pid) == sentinel.pid

        results = {}
        markers = {key: root / ("child-started-" + key) for key in ("jobA", "jobB")}

        def start(key):
            # The child records that it is running before exec'ing the sleep, so
            # cancellation can be sent only once the supervisor is provably past
            # handler installation (see spawn_supervised).
            argv = ["/bin/sh", "-c",
                    "touch %s && exec /bin/sleep 30" % markers[key]]
            try:
                results[key] = store.job(key, {"kind": "check",
                                               "argv": argv,
                                               "cwd": str(root), "stdin": "",
                                               "environment": {}})
            except Exception as exc:  # noqa: BLE001 - the probe reports any failure
                results[key] = f"{type(exc).__name__}: {exc}"

        threads = [threading.Thread(target=start, args=(key,))
                   for key in ("jobA", "jobB")]
        for thread in threads:
            thread.start()
        child_running = all(wait_for(str(markers[key]), timeout=15)
                            for key in ("jobA", "jobB"))

        store.cancel_jobs()
        for thread in threads:
            thread.join(timeout=30)
        joined = all(not thread.is_alive() for thread in threads)

        round_ok = is_leader and child_running and joined
        detail = []
        for key in ("jobA", "jobB"):
            record = results.get(key)
            good = (isinstance(record, dict)
                    and record.get("exit_code") == EXPECTED_CODE
                    and record.get("error") == EXPECTED_ERROR)
            shown = (f"{key}={record.get('exit_code')}/{record.get('error')!r}"
                     if isinstance(record, dict) else f"{key}={record!r}")
            detail.append(shown + ("" if good else " FAIL"))
            round_ok &= good

        survived = sentinel.poll() is None
        round_ok &= survived
        sentinel.kill()
        sentinel.wait()
        print(f"  round {round_index + 1}: leader={is_leader} "
              f"children_running={child_running} joined={joined} "
              f"sentinel_survived={survived} | {' '.join(detail)} "
              f"-> {'ok' if round_ok else 'FAIL'}")
        ok &= round_ok
    return ok


def report_startup_window(rounds=6):
    print(f"--- startup window (informational): group signal immediately after "
          f"process.json, {rounds} rounds ---")
    print("    no receipt here is the REFERENCE behaviour, inherited not migrated:")
    print("    process.json is written before the SIGTERM handler is installed, and")
    print("    Store.job already maps a missing receipt to RuntimeBlocked(UNKNOWN_JOB).")
    print("    This is also why every check above waits for the child's marker: a")
    print("    signal in this window makes the supervisor die with no receipt at all.")
    for label, cmd in IMPLEMENTATIONS:
        outcomes = group_signal(cmd, rounds, 0.0, marked=False)
        none_count = sum(1 for _, code, _ in outcomes if code is None)
        codes = sorted({code for _, code, _ in outcomes if code is not None})
        print(f"  {label:10s} no-receipt={none_count}/{rounds}  "
              f"receipt_codes={codes or '-'}")


def main():
    first = check_group_signal()
    second = check_cancel_scope()
    report_startup_window()
    ok = first and second
    print(f"\n=== cancellation: {'PASS' if ok else 'FAIL'} ===")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
