"""Differential check for the evidence verification unit (G1a-4, C4).

The pre-migration `verify_process` and the input-file loop are kept verbatim in
`reference_evidence.py` and production never imports them. Both paths are run
over constructed job directories and their refusals are compared by exception
class and message, because which class a refusal raises is part of the contract.
"""
import json
import tempfile
import unittest
from pathlib import Path

from formal_gates_harness.infra import (
    Bridge,
    HarnessError,
    RuntimeBlocked,
    Store,
    atomic,
    file_proof,
    refuse,
)
from reference_evidence import verify_input_files as reference_verify_input_files
from reference_evidence import verify_process as reference_verify_process

PACKAGE = Path(__file__).resolve().parents[3]
BINARY = PACKAGE / ".harness/bin/formal-gates"
STREAMS = ("stdout.log", "stderr.log")


def outcome(fn, *args, **kwargs):
    try:
        fn(*args, **kwargs)
        return ("ok", None)
    except BaseException as exc:  # noqa: BLE001 - the class is the point
        return (type(exc).__name__, str(exc))


class JobFixture:
    """A job directory that looks like the one the runner publishes."""

    def __init__(self, root: Path, key: str = "job-1"):
        self.key = key
        self.jobs = root / "jobs"
        self.directory = self.jobs / key
        self.directory.mkdir(parents=True)
        self.request = {"kind": "check", "argv": ["python3", "-c", "pass"], "attempt": 0}
        self.streams = {}
        (self.directory / "stdout.log").write_bytes(b"stdout\n")
        (self.directory / "stderr.log").write_bytes(b"stderr\n")
        for name in STREAMS:
            self.streams[name] = file_proof(self.directory / name)
        self.receipt = {
            "exit_code": 0,
            "error": "",
            "request_digest": None,
            "streams": dict(self.streams),
        }
        self.write()

    def write(self):
        from formal_gates_harness.infra import digest

        self.receipt["request_digest"] = digest(self.request)
        atomic(self.directory / "request.json", self.request)
        atomic(self.directory / "exit.json", self.receipt)

    def process(self, **overrides):
        record = {
            "operation": self.key,
            "directory": str(self.directory),
            "exit_code": self.receipt["exit_code"],
            "error": self.receipt["error"],
            "request_digest": self.receipt["request_digest"],
            "streams": dict(self.streams),
        }
        record.update(overrides)
        return record

    def process_without_digest(self, **overrides):
        record = self.process(**overrides)
        record.pop("request_digest", None)
        return record


class EvidenceVerificationDifferential(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name).resolve()
        self.jobs = self.root / "jobs"
        self.jobs.mkdir(parents=True, exist_ok=True)
        self.bridge = Bridge(str(BINARY))
        self.store = Store(self.root, {}, self.bridge)

    def tearDown(self):
        self._tmp.cleanup()

    def compare_process(self, fixture, record):
        want = outcome(reference_verify_process, self.jobs, record)
        got = outcome(self.store.verify_process, record)
        self.assertEqual(want, got, f"verify_process diverged for {record}")
        return got

    # -- acceptance ---------------------------------------------------------

    def test_valid_receipt_passes_on_both_paths(self):
        fixture = JobFixture(self.root)
        self.assertEqual(self.compare_process(fixture, fixture.process()), ("ok", None))

    def test_numerically_equal_numbers_are_equal(self):
        """The reference compares parsed values, where 1 and 1.0 are the same."""
        fixture = JobFixture(self.root)
        record = fixture.process(exit_code=0.0)
        self.assertEqual(self.compare_process(fixture, record), ("ok", None))

    # -- binding refusals ---------------------------------------------------

    def test_request_digest_mismatch(self):
        fixture = JobFixture(self.root)
        fixture.request["attempt"] = 7
        atomic(fixture.directory / "request.json", fixture.request)
        got = self.compare_process(fixture, fixture.process())
        self.assertEqual(got, ("HarnessError", "EVIDENCE_BINDING: request digest differs"))

    def test_process_field_mismatch(self):
        fixture = JobFixture(self.root)
        self.assertEqual(
            self.compare_process(fixture, fixture.process(exit_code=1)),
            ("HarnessError", "EVIDENCE_BINDING: process exit_code differs"),
        )
        self.assertEqual(
            self.compare_process(fixture, fixture.process(error="boom")),
            ("HarnessError", "EVIDENCE_BINDING: process error differs"),
        )
        self.assertEqual(
            self.compare_process(fixture, fixture.process(streams={"stdout.log": {}})),
            ("HarnessError", "EVIDENCE_BINDING: process streams differs"),
        )

    def test_missing_request_digest_in_process_record(self):
        fixture = JobFixture(self.root)
        got = self.compare_process(fixture, fixture.process_without_digest())
        self.assertEqual(got, ("HarnessError", "EVIDENCE_BINDING: process request_digest differs"))

    def test_job_outside_this_run_is_a_binding_refusal(self):
        fixture = JobFixture(self.root)
        record = fixture.process(directory=str(self.root / "elsewhere"))
        self.assertEqual(
            self.compare_process(fixture, record),
            ("HarnessError", "EVIDENCE_BINDING: job is outside this run"),
        )

    # -- missing and changed evidence ---------------------------------------

    def test_proof_set_is_wrong(self):
        fixture = JobFixture(self.root)
        fixture.receipt["streams"] = {"stdout.log": fixture.streams["stdout.log"]}
        fixture.write()
        got = self.compare_process(fixture, fixture.process(streams=fixture.receipt["streams"]))
        self.assertEqual(got, ("RuntimeBlocked", "EVIDENCE_MISSING: stdout/stderr proofs absent"))

    def test_stream_content_changed(self):
        fixture = JobFixture(self.root)
        (fixture.directory / "stdout.log").write_bytes(b"tampered\n")
        got = self.compare_process(fixture, fixture.process())
        self.assertEqual(got, ("RuntimeBlocked", "EVIDENCE_CHANGED: stdout.log"))

    def test_stream_deleted_is_missing_evidence_not_changed(self):
        """A stream that cannot be read names the path, not the stream."""
        fixture = JobFixture(self.root)
        (fixture.directory / "stderr.log").unlink()
        got = self.compare_process(fixture, fixture.process())
        self.assertEqual(got[0], "RuntimeBlocked")
        self.assertTrue(got[1].startswith("EVIDENCE_MISSING: "), got)

    def test_first_wrong_proof_in_document_order_is_named(self):
        """Both streams are wrong, so the order decides which one is reported.

        Receipts are published through the canonical encoding, which sorts keys,
        so the document order is stderr.log before stdout.log. Both paths must
        name the same one; reporting a different stream is a behaviour change
        even though the verdict is the same.
        """
        fixture = JobFixture(self.root)
        (fixture.directory / "stdout.log").write_bytes(b"one\n")
        (fixture.directory / "stderr.log").write_bytes(b"two\n")
        got = self.compare_process(fixture, fixture.process())
        self.assertEqual(got, ("RuntimeBlocked", "EVIDENCE_CHANGED: stderr.log"))

    # -- input files --------------------------------------------------------

    def test_input_files_pass_and_fail(self):
        first = self.root / "comparison-input.txt"
        first.write_bytes(b"content\n")
        intact = {str(first): file_proof(first)}
        changed = {str(first): {"sha256": "0" * 64, "size": 1}}
        missing = {str(self.root / "absent.txt"): {"sha256": "0" * 64, "size": 0}}

        for files in (intact, changed, missing):
            with self.subTest(kind=sorted(files)[0]):
                reference = outcome(reference_verify_input_files, files)
                migrated = outcome(lambda: refuse(self.bridge.call(
                    "verify-files",
                    {"files": [{"name": str(k), "expected": v} for k, v in files.items()],
                     "label": "comparison input "},
                )))
                self.assertEqual(reference[0], migrated[0], f"{reference} vs {migrated}")
                if reference[0] != "ok":
                    self.assertEqual(reference[1], migrated[1], f"{reference} vs {migrated}")

    def test_refusal_family_maps_to_exception_class(self):
        self.assertIsInstance(error_of(lambda: refuse({"ok": False, "kind": "binding", "refusal": "x"})), HarnessError)
        self.assertIsInstance(error_of(lambda: refuse({"ok": False, "kind": "missing", "refusal": "x"})), RuntimeBlocked)
        self.assertIsInstance(error_of(lambda: refuse({"ok": False, "kind": "changed", "refusal": "x"})), RuntimeBlocked)
        self.assertIsNone(error_of(lambda: refuse({"ok": True})))


def error_of(fn):
    try:
        fn()
        return None
    except BaseException as exc:  # noqa: BLE001
        return exc


if __name__ == "__main__":
    unittest.main()
