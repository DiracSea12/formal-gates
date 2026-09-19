"""Differential test for the G2a job supervisor.

The pre-migration supervisor (formal_gates_harness.job) is kept verbatim in
`reference_job.py` and is run as its own process. This test runs the Go supervisor
(`formal-gates harness supervise`) and the reference implementation as two
independent processes, each over its OWN job directory, and asserts the receipts
and raw stream bytes are byte-for-byte identical (after normalising the
non-deterministic fields started_at / finished_at and replacing each side's shared
temp root with `<root>`).

The two sides must never share a job directory: the second writer would silently
skip its write and the comparison would degenerate. The job directories differ,
but every path embedded in the request (argv, cwd, helper files, the PATH used by
the child) is SHARED between the two sides so the observable receipt - including the
exact error text - is identical.
"""
import json
import os
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]
BINARY = str(REPO / ".harness" / "bin" / "formal-gates")
VENV_PYTHON = str(REPO / ".harness" / "venv" / "bin" / "python")
RUNTIME = str(REPO / "runtime" / "langgraph")
REFERENCE = str(REPO / "runtime" / "langgraph" / "tests" / "reference_job.py")


def make_env(overrides=None, drop=None):
    env = {
        "PATH": "/usr/bin:/bin",
        "HOME": "/tmp",
        "TMPDIR": "/private/tmp",
        "PYTHONPATH": RUNTIME,
        "PYTHONDONTWRITEBYTECODE": "1",
        "FORMAL_GATES_HARNESS_BINARY": BINARY,
    }
    if overrides:
        env.update(overrides)
    if drop:
        for k in drop:
            env.pop(k, None)
    return env


def read_receipt(job):
    with open(os.path.join(job, "exit.json")) as f:
        return json.load(f)


def read_raw(job, name):
    with open(os.path.join(job, name), "rb") as f:
        return f.read()


def normalize(receipt, root):
    r = dict(receipt)
    r.pop("started_at", None)
    r.pop("finished_at", None)
    if isinstance(r.get("error"), str) and root:
        r["error"] = r["error"].replace(root, "<root>")
    return r


def normalize_bytes(data, root):
    if root:
        return data.replace(root.encode(), b"<root>")
    return data


def wait_until(predicate, timeout=10.0):
    deadline = time.time() + timeout
    while time.time() < deadline:
        if predicate():
            return True
        time.sleep(0.02)
    return False


class JobSupervisionDifferential(unittest.TestCase):
    def setUp(self):
        # One shared work tree holds every path the request embeds, so both sides
        # observe identical argv/cwd/error text.
        self.work = tempfile.mkdtemp(prefix="g2a-work-")
        # helper files for spawn-error cases
        self.noexec = os.path.join(self.work, "noexec")
        with open(self.noexec, "w") as f:
            f.write("x")
        os.chmod(self.noexec, 0o644)
        self.txt = os.path.join(self.work, "txtscript")
        with open(self.txt, "w") as f:
            f.write("not a binary\n")
        os.chmod(self.txt, 0o755)
        self.tools = os.path.join(self.work, "tools")
        os.makedirs(self.tools, exist_ok=True)
        self.mytool = os.path.join(self.tools, "mytool")
        with open(self.mytool, "w") as f:
            f.write("#!/bin/sh\necho hi\n")
        os.chmod(self.mytool, 0o755)

    def tearDown(self):
        import shutil
        shutil.rmtree(self.work, ignore_errors=True)

    def run_both(self, request, go_env=None, ref_env=None, session=False):
        go_job = tempfile.mkdtemp(prefix="g2a-go-")
        ref_job = tempfile.mkdtemp(prefix="g2a-ref-")
        for d in (go_job, ref_job):
            with open(os.path.join(d, "request.json"), "w") as f:
                json.dump(request, f)
        go = subprocess.Popen(
            [BINARY, "harness", "supervise", go_job],
            env=go_env or make_env(), stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL, start_new_session=session,
        )
        ref = subprocess.Popen(
            [VENV_PYTHON, REFERENCE, ref_job],
            env=ref_env or make_env(), stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL, start_new_session=session,
        )
        return go_job, ref_job, go, ref

    def compare(self, go_job, ref_job, expect_rc=0):
        go_rec = read_receipt(go_job)
        ref_rec = read_receipt(ref_job)
        g = normalize(go_rec, self.work)
        r = normalize(ref_rec, self.work)
        self.assertEqual(
            g, r,
            msg=f"receipt mismatch:\n GO:  {go_rec}\n REF: {ref_rec}",
        )
        # exactly six fields, exactly two stream proofs
        self.assertEqual(set(go_rec.keys()),
                         {"exit_code", "error", "started_at", "finished_at",
                          "request_digest", "streams"})
        self.assertEqual(set(go_rec["streams"].keys()), {"stdout.log", "stderr.log"})
        # request_digest equal on both sides and well-formed
        self.assertEqual(go_rec["request_digest"], ref_rec["request_digest"])
        self.assertRegex(go_rec["request_digest"], r"^[0-9a-f]{64}$")
        # raw stream bytes identical (root normalised)
        for name in ("stdout.log", "stderr.log"):
            gb = normalize_bytes(read_raw(go_job, name), self.work)
            rb = normalize_bytes(read_raw(ref_job, name), self.work)
            self.assertEqual(gb, rb, msg=f"stream {name} differs")
        return go_rec, ref_rec

    def check(self, name, request, go_env=None, ref_env=None, expect_rc=0):
        with self.subTest(name):
            go_job, ref_job, go, ref = self.run_both(request, go_env, ref_env)
            self.assertEqual(go.wait(), expect_rc)
            self.assertEqual(ref.wait(), expect_rc)
            self.compare(go_job, ref_job, expect_rc)

    def test_exit_zero(self):
        self.check("exit0", {"kind": "check", "argv": ["/usr/bin/true"],
                             "cwd": self.work, "stdin": "", "environment": {}})

    def test_exit_seven(self):
        self.check("exit7", {"kind": "check", "argv": ["/bin/sh", "-c", "exit 7"],
                             "cwd": self.work, "stdin": "", "environment": {}})

    def test_explicit_exit_130_not_cancellation(self):
        self.check("exit130", {"kind": "check", "argv": ["/bin/sh", "-c", "exit 130"],
                               "cwd": self.work, "stdin": "", "environment": {}})
        go_job, ref_job, go, ref = self.run_both(
            {"kind": "check", "argv": ["/bin/sh", "-c", "exit 130"],
             "cwd": self.work, "stdin": "", "environment": {}})
        go.wait(); ref.wait()
        for job in (go_job, ref_job):
            self.assertEqual(read_receipt(job)["error"], "")

    def test_child_sigkill(self):
        self.check("sigkill", {"kind": "check", "argv": ["/bin/sh", "-c", "kill -9 $$"],
                               "cwd": self.work, "stdin": "", "environment": {}})

    def test_stdout_stderr_bytes(self):
        cases = [
            ("plain", ["/bin/sh", "-c", "printf OUT; printf ERR 1>&2"]),
            ("no-newline", ["/bin/sh", "-c", "printf X"]),
            ("multibyte", ["/bin/sh", "-c", "printf '\\xe6\\x97\\xa5\\xe6\\x9c\\xac'"]),
            ("large", ["/bin/sh", "-c",
                       "/usr/bin/head -c 100000 /dev/zero | /usr/bin/tr '\\0' 'A'"]),
        ]
        for label, argv in cases:
            with self.subTest(label):
                go_job, ref_job, go, ref = self.run_both(
                    {"kind": "check", "argv": argv, "cwd": self.work,
                     "stdin": "", "environment": {}})
                go.wait(); ref.wait()
                self.compare(go_job, ref_job)
                size = len(read_raw(go_job, "stdout.log"))
                if label == "large":
                    self.assertEqual(size, 100000)
                elif label not in ("no-newline",):
                    self.assertGreater(size + len(read_raw(go_job, "stderr.log")), 0)

    def test_stdin_passthrough(self):
        payloads = ["", "hello\nworld", "日本語\nmulti-byte", "Z" * 70000]
        for p in payloads:
            with self.subTest(len=len(p)):
                go_job, ref_job, go, ref = self.run_both(
                    {"kind": "check", "argv": ["/bin/cat"], "cwd": self.work,
                     "stdin": p, "environment": {}})
                go.wait(); ref.wait()
                self.compare(go_job, ref_job)
                self.assertEqual(read_raw(go_job, "stdout.log").decode(), p)

    def test_environment_merge(self):
        script = ["/bin/sh", "-c", "printf '%s' \"$MYVAR\""]
        with self.subTest("override"):
            go_job, ref_job, go, ref = self.run_both(
                {"kind": "check", "argv": script, "cwd": self.work, "stdin": "",
                 "environment": {"MYVAR": "overridden"}},
                go_env=make_env({"MYVAR": "base"}),
                ref_env=make_env({"MYVAR": "base"}))
            go.wait(); ref.wait()
            self.compare(go_job, ref_job)
            self.assertEqual(read_raw(go_job, "stdout.log").decode(), "overridden")
        with self.subTest("add"):
            go_job, ref_job, go, ref = self.run_both(
                {"kind": "check", "argv": script, "cwd": self.work, "stdin": "",
                 "environment": {"MYVAR": "fresh"}})
            go.wait(); ref.wait()
            self.compare(go_job, ref_job)
            self.assertEqual(read_raw(go_job, "stdout.log").decode(), "fresh")
        with self.subTest("default-empty"):
            go_job, ref_job, go, ref = self.run_both(
                {"kind": "check", "argv": script, "cwd": self.work, "stdin": "",
                 "environment": {}},
                go_env=make_env({"MYVAR": "fromparent"}),
                ref_env=make_env({"MYVAR": "fromparent"}))
            go.wait(); ref.wait()
            self.compare(go_job, ref_job)
            self.assertEqual(read_raw(go_job, "stdout.log").decode(), "fromparent")

    def test_cwd_effective(self):
        self.check("cwd", {"kind": "check", "argv": ["/bin/sh", "-c", "pwd"],
                           "cwd": self.work, "stdin": "", "environment": {}})
        go_job, ref_job, go, ref = self.run_both(
            {"kind": "check", "argv": ["/bin/sh", "-c", "pwd"],
             "cwd": self.work, "stdin": "", "environment": {}})
        go.wait(); ref.wait()
        # macOS resolves /var -> /private/var; both sides observe the same resolved
        # cwd, so the comparison passes. Assert the raw bytes are identical.
        self.assertEqual(read_raw(go_job, "stdout.log"), read_raw(ref_job, "stdout.log"))

    def test_spawn_errors(self):
        cases = [
            ("argv0-missing", ["/no/such/binary/xyz"],
             "FileNotFoundError: [Errno 2] No such file or directory: '/no/such/binary/xyz'"),
            ("argv0-notexec", [self.noexec],
             "PermissionError: [Errno 13] Permission denied: '%s'" % self.noexec),
            ("argv0-dir", [self.work],
             "PermissionError: [Errno 13] Permission denied: '%s'" % self.work),
            ("argv0-textfile", [self.txt],
             "OSError: [Errno 8] Exec format error: '%s'" % self.txt),
            ("cwd-missing", ["/usr/bin/true", self.work + "/nope"],
             "FileNotFoundError: [Errno 2] No such file or directory: '%s'" % (self.work + "/nope")),
            ("cwd-file", ["/usr/bin/true", self.noexec],
             "NotADirectoryError: [Errno 20] Not a directory: '%s'" % self.noexec),
        ]
        for label, argv, want_err in cases:
            with self.subTest(label):
                # cwd-missing / cwd-file carry cwd as the second argv element's
                # companion: the request's cwd field is what matters.
                if label.startswith("cwd-"):
                    cwd = argv[1]
                    argv0 = ["/usr/bin/true"]
                else:
                    cwd = self.work
                    argv0 = argv
                go_job, ref_job, go, ref = self.run_both(
                    {"kind": "check", "argv": argv0, "cwd": cwd,
                     "stdin": "", "environment": {}})
                go.wait(); ref.wait()
                rec_g, rec_r = self.compare(go_job, ref_job)
                self.assertEqual(rec_g["exit_code"], 127)
                self.assertEqual(rec_g["error"], want_err)

    def test_bare_argv0_in_request_path(self):
        # The tool lives ONLY in the request's PATH; the parent env must not see it.
        go_job, ref_job, go, ref = self.run_both(
            {"kind": "check", "argv": ["mytool"], "cwd": self.work, "stdin": "",
             "environment": {"PATH": self.tools}})
        go.wait(); ref.wait()
        self.compare(go_job, ref_job)
        self.assertEqual(read_raw(go_job, "stdout.log").decode(), "hi\n")

    def test_bare_argv0_no_path_falls_back_to_defpath(self):
        # No PATH anywhere -> /bin:/usr/bin fallback. Parent env has no PATH.
        go_job, ref_job, go, ref = self.run_both(
            {"kind": "check", "argv": ["ls"], "cwd": self.work, "stdin": "",
             "environment": {}},
            go_env=make_env(drop=["PATH"]), ref_env=make_env(drop=["PATH"]))
        go.wait(); ref.wait()
        self.compare(go_job, ref_job)

    def test_owner_exists(self):
        go_job = tempfile.mkdtemp(prefix="g2a-go-")
        ref_job = tempfile.mkdtemp(prefix="g2a-ref-")
        marker = os.path.join(self.work, "MARKER")
        request = {"kind": "check", "argv": ["/bin/sh", "-c", "touch " + marker],
                   "cwd": self.work, "stdin": "", "environment": {}}
        for d in (go_job, ref_job):
            with open(os.path.join(d, "request.json"), "w") as f:
                json.dump(request, f)
            with open(os.path.join(d, "owner"), "w") as f:
                f.write("")
        go = subprocess.Popen([BINARY, "harness", "supervise", go_job],
                              env=make_env(), stdout=subprocess.DEVNULL,
                              stderr=subprocess.DEVNULL)
        ref = subprocess.Popen([VENV_PYTHON, REFERENCE, ref_job],
                               env=make_env(), stdout=subprocess.DEVNULL,
                               stderr=subprocess.DEVNULL)
        self.assertEqual(go.wait(), 2)
        self.assertEqual(ref.wait(), 2)
        for job in (go_job, ref_job):
            self.assertFalse(os.path.exists(os.path.join(job, "exit.json")),
                             "exit.json must not be written when owner exists")
            self.assertFalse(os.path.exists(os.path.join(job, "process.json")),
                             "process.json must not be written when owner exists")
        self.assertFalse(os.path.exists(marker), "child must not run when owner exists")

    def test_bare_argv0_continues_past_blocking_candidates(self):
        # D1: a bare argv[0] must resolve via the merged PATH walk, continuing
        # past a non-executable / directory / bad-format candidate to the first
        # one that actually executes. Both sides must run the working candidate.
        scenarios = [
            ("nonexec_then_good", "nonexec"),
            ("dir_then_good", "dir"),
            ("enoexec_then_good", "enoexec"),
        ]
        for label, blocker in scenarios:
            with self.subTest(label):
                base = tempfile.mkdtemp(prefix="g2a-path-")
                blocked = os.path.join(base, "blocked")
                good = os.path.join(base, "good")
                os.makedirs(blocked, exist_ok=True)
                os.makedirs(good, exist_ok=True)
                tool_blocked = os.path.join(blocked, "tool")
                tool_good = os.path.join(good, "tool")
                if blocker == "nonexec":
                    with open(tool_blocked, "w") as f:
                        f.write("data\n")
                    os.chmod(tool_blocked, 0o644)
                elif blocker == "dir":
                    os.makedirs(tool_blocked, exist_ok=True)
                elif blocker == "enoexec":
                    with open(tool_blocked, "w") as f:
                        f.write("no shebang\n")
                    os.chmod(tool_blocked, 0o755)
                with open(tool_good, "w") as f:
                    f.write("#!/bin/sh\necho ran-the-good-one\n")
                os.chmod(tool_good, 0o755)
                path = blocked + ":" + good
                request = {"kind": "check", "argv": ["tool"], "cwd": base,
                           "stdin": "", "environment": {"PATH": path}}
                go_job, ref_job, go, ref = self.run_both(request)
                self.assertEqual(go.wait(), 0)
                self.assertEqual(ref.wait(), 0)
                g, r = self.compare(go_job, ref_job)
                self.assertEqual(g["exit_code"], 0)
                self.assertEqual(g["error"], "")
                self.assertEqual(read_raw(go_job, "stdout.log").decode(),
                                 "ran-the-good-one\n")

    def test_cancel(self):
        # D2: production cancellation signals the supervisor's whole process
        # group (Store.cancel_jobs uses os.killpg), which also reaches the child.
        # The receipt must be a deterministic 130 / "owned job was cancelled".
        # Wait for POSITIVE proof EACH child is running (each supervisor's child
        # touches its OWN marker before exec'ing the long sleep) rather than for
        # process.json: process.json is written before the SIGTERM handler is
        # armed (reference_job.py line 31 then line 38; Supervise likewise), so a
        # group signal landing in that window kills the supervisor outright with no
        # receipt. A running child can only exist after the handler is installed.
        # The two markers are independent so each side is proven safe on its own -
        # a single shared marker would only prove one side and let the other be
        # signalled while still in its startup window.
        go_marker = os.path.join(self.work, "child-started-go")
        ref_marker = os.path.join(self.work, "child-started-ref")

        def make_request(marker):
            return {"kind": "check",
                    "argv": ["/bin/sh", "-c", "touch " + marker + " && exec /bin/sleep 30"],
                    "cwd": self.work, "stdin": "", "environment": {}}

        go_job = tempfile.mkdtemp(prefix="g2a-go-")
        ref_job = tempfile.mkdtemp(prefix="g2a-ref-")
        for d, marker in ((go_job, go_marker), (ref_job, ref_marker)):
            with open(os.path.join(d, "request.json"), "w") as f:
                json.dump(make_request(marker), f)
        go = subprocess.Popen([BINARY, "harness", "supervise", go_job],
                              env=make_env(), stdout=subprocess.DEVNULL,
                              stderr=subprocess.DEVNULL, start_new_session=True)
        ref = subprocess.Popen([VENV_PYTHON, REFERENCE, ref_job],
                               env=make_env(), stdout=subprocess.DEVNULL,
                               stderr=subprocess.DEVNULL, start_new_session=True)
        self.assertTrue(wait_until(lambda: os.path.exists(go_marker)),
                        "go child marker never appeared")
        self.assertTrue(wait_until(lambda: os.path.exists(ref_marker)),
                        "ref child marker never appeared")

        def group_signal(proc):
            # Mirror Store.cancel_jobs: signal the process group, not just the
            # process. start_new_session=True makes each supervisor its own pgrp.
            os.killpg(os.getpgid(proc.pid), signal.SIGTERM)

        group_signal(go)
        group_signal(ref)
        self.assertEqual(go.wait(), 0)
        self.assertEqual(ref.wait(), 0)
        for job in (go_job, ref_job):
            rec = read_receipt(job)
            self.assertEqual(rec["exit_code"], 130)
            self.assertEqual(rec["error"], "owned job was cancelled")
            self.assertEqual(set(rec.keys()),
                             {"exit_code", "error", "started_at", "finished_at",
                              "request_digest", "streams"})
            self.assertEqual(set(rec["streams"].keys()),
                             {"stdout.log", "stderr.log"})

    def test_cancel_does_not_touch_unowned_process(self):
        # D2 scope: cancellation must only affect jobs it owns. Start two real
        # jobs through Store.job (production entry point, start_new_session=True)
        # plus a sentinel that is a process-group leader but NOT a registered
        # job; after cancel_jobs both jobs carry the cancellation receipt and the
        # sentinel survives.
        import threading
        sys.path.insert(0, RUNTIME)
        os.environ["FORMAL_GATES_HARNESS_BINARY"] = BINARY
        from formal_gates_harness.infra import Bridge, Store
        from pathlib import Path as _Path

        root = _Path(tempfile.mkdtemp(prefix="g2a-cscope-"))
        store = Store(root, {}, Bridge(BINARY))
        sentinel = subprocess.Popen(["/bin/sleep", "60"], start_new_session=True)
        self.assertTrue(os.getpgid(sentinel.pid) == sentinel.pid,
                        "sentinel must be a process-group leader")

        markers = {key: os.path.join(str(root), "child-started-" + key)
                   for key in ("jobA", "jobB")}
        results = {}
        threads = []
        for key in ("jobA", "jobB"):
            def start(k=key):
                try:
                    results[k] = store.job(k, {"kind": "check",
                                              "argv": ["/bin/sh", "-c",
                                                       "touch " + markers[k] +
                                                       " && exec /bin/sleep 30"],
                                              "cwd": str(root), "stdin": "",
                                              "environment": {}})
                except Exception as exc:  # noqa: BLE001 - report any failure
                    results[k] = f"{type(exc).__name__}: {exc}"
            threads.append(threading.Thread(target=start))
        for t in threads:
            t.start()
        # Wait for POSITIVE proof BOTH children are running (each touches its own
        # marker before exec'ing the long sleep) instead of waiting for
        # process.json plus a fixed sleep: process.json is written before the
        # SIGTERM handler is armed, so signalling in that window kills the
        # supervisor with no receipt. A running child can only exist after the
        # handler is installed.
        deadline = time.time() + 15
        while time.time() < deadline and \
                not all(os.path.exists(m) for m in markers.values()):
            time.sleep(0.05)
        self.assertTrue(all(os.path.exists(m) for m in markers.values()),
                        "both children must start before cancel_jobs")
        store.cancel_jobs()
        for t in threads:
            t.join(timeout=30)

        for key in ("jobA", "jobB"):
            rec = results.get(key)
            self.assertIsInstance(rec, dict, msg=f"{key} not a receipt: {rec!r}")
            self.assertEqual(rec.get("exit_code"), 130)
            self.assertEqual(rec.get("error"), "owned job was cancelled")
        self.assertIsNone(sentinel.poll(), "sentinel must survive cancellation")
        sentinel.kill()
        sentinel.wait()

    def test_native_form_shape(self):
        # The native entry form: argv is the literal native entrypoint, carrying the
        # job directory. Because that directory differs per side, request_digest
        # legitimately differs, so this case asserts SHAPE equality (exit 1, empty
        # error, native-error.json written, streams present) rather than an identical
        # digest - matching the contract's "same shape" requirement. G2c: the
        # entrypoint is the Go narrow command itself (the Python
        # formal_gates_harness.native_entry module was deleted with the last
        # in-process adapter); an unknown provider still lands the typed refusal.
        native_req = {"provider": "bogus-provider-xyz"}
        request = {"kind": "native",
                   "argv": [BINARY, "harness", "native", "<JOB>"],
                   "cwd": self.work, "stdin": "",
                   "environment": {"FORMAL_GATES_HARNESS_BINARY": BINARY,
                                   "TMPDIR": "/private/tmp"}}
        # Run Go and reference, substituting each job dir into the argv.
        for label, binary, args in (
            ("go", BINARY, lambda j: [BINARY, "harness", "supervise", j]),
            ("ref", VENV_PYTHON, lambda j: [VENV_PYTHON, REFERENCE, j]),
        ):
            job = tempfile.mkdtemp(prefix="g2a-native-")
            req = dict(request)
            req["argv"] = [BINARY, "harness", "native", job]
            with open(os.path.join(job, "request.json"), "w") as f:
                json.dump(req, f)
            with open(os.path.join(job, "native-request.json"), "w") as f:
                json.dump(native_req, f)
            p = subprocess.Popen(args(job), env=make_env(),
                                  stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            self.assertEqual(p.wait(), 0, msg=label)
            rec = read_receipt(job)
            self.assertEqual(rec["exit_code"], 1)
            self.assertEqual(rec["error"], "")
            self.assertTrue(os.path.exists(os.path.join(job, "native-error.json")),
                            "%s: native-error.json not written" % label)
            self.assertEqual(set(rec["streams"].keys()), {"stdout.log", "stderr.log"})


if __name__ == "__main__":
    unittest.main()
