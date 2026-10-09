"""Differential check for artifact publication against an independent oracle.

The oracle in reference_artifacts.py is test-only. Both implementations cover
the contract's required cases, comparing exact exception classes and messages.
The two business refusals must be exactly HarnessError rather than RuntimeBlocked,
which assertRaises(HarnessError) cannot distinguish from its parent class.

Each implementation publishes into its own tree, so both execute their writes
and the published bytes can be compared. OS error messages are normalized by
replacing each tree's root with <root>, preserving comparison across distinct roots.
"""
import base64
import hashlib
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from formal_gates_harness.infra import Bridge, HarnessError, RuntimeBlocked, Store

from reference_artifacts import ReferenceStore

PACKAGE = Path(__file__).resolve().parents[3]
BINARY = PACKAGE / ".harness/bin/formal-gates"


def normalise(message, root):
    """Make an OS error message comparable across two different artifact roots."""
    return message.replace(str(root), "<root>")


def outcome(fn, *args, root, **kwargs):
    """Return (exception_type_or_None, normalised_message_or_None).

    The type is the exact class, so callers can assert `is HarnessError` and
    exclude subclasses such as RuntimeBlocked.
    """
    try:
        fn(*args, **kwargs)
        return (None, None)
    except BaseException as exc:  # noqa: BLE001 - the class is the point
        return (type(exc), normalise(str(exc), root))


class ArtifactPublicationDifferential(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name).resolve()
        # Independent trees: one per implementation.
        self.live_root = self.root / "live"
        self.reference_root = self.root / "reference"
        for base in (self.live_root, self.reference_root):
            (base / "artifacts").mkdir(parents=True, exist_ok=True)
        self.bridge = Bridge(str(BINARY))
        self.store = Store(self.live_root, {}, self.bridge)
        self.reference = ReferenceStore(self.reference_root, {}, self.bridge)
        # ReferenceStore.put resolves canonical() through the global
        # driver_bridge(); pin it to the same binary so the comparison is
        # against one implementation, not two builds.
        self._env = patch.dict(os.environ,
                               {"FORMAL_GATES_HARNESS_BINARY": str(BINARY)})
        self._env.start()
        self.addCleanup(self._env.stop)

    def tearDown(self):
        self._tmp.cleanup()

    def artifact(self, base, ref):
        return base / "artifacts" / f"{ref}.json"

    def publish(self, value):
        """Publish through both implementations and prove reference + bytes agree."""
        live_ref = self.store.put(value)
        reference_ref = self.reference.put(value)
        self.assertEqual(live_ref, reference_ref,
                         f"reference diverged for {value!r}")
        self.assertEqual(self.artifact(self.live_root, live_ref).read_bytes(),
                         self.artifact(self.reference_root, reference_ref).read_bytes(),
                         f"published bytes diverged for {value!r}")
        return live_ref

    def mutate(self, ref, raw):
        """Apply the same on-disk change to both trees, so states stay comparable."""
        for base in (self.live_root, self.reference_root):
            self.artifact(base, ref).write_bytes(raw)

    def compare_put(self, value):
        want = outcome(self.reference.put, value, root=self.reference_root)
        got = outcome(self.store.put, value, root=self.live_root)
        self.assertEqual(want, got, f"put diverged for {value!r}")
        return got

    def compare_get(self, ref):
        want = outcome(self.reference.get, ref, root=self.reference_root)
        got = outcome(self.store.get, ref, root=self.live_root)
        self.assertEqual(want, got, f"get diverged for ref {ref!r}")
        return got

    # -- acceptance: reference and bytes agree ----------------------------

    def test_reference_and_bytes_agree_unicode_empty_nested(self):
        for value in ({"result": "PASS"}, {}, {"nested": [1, "日本語", {"k": True}]},
                      {"empty": []}):
            with self.subTest(value=value):
                ref = self.publish(value)
                # The artifact bytes are exactly what Go produced for this value.
                canonical_text = self.bridge.call(
                    "artifact-plan", {"value": value}).get("canonical")
                self.assertEqual(self.artifact(self.live_root, ref).read_bytes(),
                                 canonical_text.encode())
                self.assertEqual(self.store.get(ref), value)
                self.assertEqual(self.reference.get(ref), value)

    def test_published_trees_are_byte_identical(self):
        # The live write is genuinely exercised here: each side writes its own
        # tree, so neither can satisfy the other's put by having the file already.
        for value in ({"a": "<>&\u2028\u2029"}, {"n": 10 ** 20, "f": 0.0001},
                      {"deep": {"x": [{"y": None}, "café"]}}):
            with self.subTest(value=value):
                ref = self.publish(value)
                self.assertTrue(self.artifact(self.live_root, ref).exists())
                self.assertTrue(self.artifact(self.reference_root, ref).exists())

    def test_numeric_fidelity_seven_vs_seven_float(self):
        ref_int = self.publish({"x": 7})
        ref_float = self.publish({"x": 7.0})
        self.assertNotEqual(ref_int, ref_float,
                            "7 and 7.0 must produce distinct references")
        self.assertEqual(self.store.get(ref_int), {"x": 7})
        self.assertEqual(self.store.get(ref_float), {"x": 7.0})

    def test_non_finite_numbers_refused_on_both_paths(self):
        for value in ({"x": float("nan")}, {"x": float("inf")}):
            with self.subTest(value=value):
                want = self.compare_put(value)
                # The refusal is a bridge error, not a business HarnessError:
                # the message is the decode failure, identical on both paths.
                self.assertIsNotNone(want[0])

    def test_repeat_put_does_not_rewrite(self):
        value = {"result": "PASS"}
        ref = self.publish(value)
        live = self.artifact(self.live_root, ref)
        reference = self.artifact(self.reference_root, ref)
        live_before = (live.stat().st_ino, live.stat().st_mtime_ns)
        reference_before = (reference.stat().st_ino, reference.stat().st_mtime_ns)
        self.store.put(value)
        self.reference.put(value)
        self.assertEqual((live.stat().st_ino, live.stat().st_mtime_ns), live_before)
        self.assertEqual((reference.stat().st_ino, reference.stat().st_mtime_ns),
                         reference_before)
        self.assertEqual(self.store.get(ref), value)

    def test_corrupt_existing_not_repaired_on_reput(self):
        value = {"result": "PASS"}
        ref = self.publish(value)
        # Corrupt both bound contents; M3 never repairs a publish-once file.
        self.mutate(ref, b"{}")
        # Re-putting the same value must NOT overwrite the corrupt bytes.
        self.store.put(value)
        self.reference.put(value)
        self.assertEqual(self.artifact(self.live_root, ref).read_bytes(), b"{}")
        self.assertEqual(self.artifact(self.reference_root, ref).read_bytes(), b"{}")
        # And get must now report an integrity refusal, on both paths.
        want = self.compare_get(ref)
        self.assertIs(want[0], HarnessError)
        self.assertEqual(want[1], "artifact integrity mismatch")

    def test_two_distinct_values_publish_independently(self):
        a = self.publish({"a": 1})
        b = self.publish({"b": 2})
        self.assertNotEqual(a, b)
        self.assertEqual(self.store.get(a), {"a": 1})
        self.assertEqual(self.store.get(b), {"b": 2})
        self.assertEqual(self.reference.get(a), {"a": 1})
        self.assertEqual(self.reference.get(b), {"b": 2})

    def test_publish_failure_leaves_no_artifact_or_tmp(self):
        value = {"result": "PASS"}
        # Stub the atomic rename so publish fails midway (atomic_bytes is
        # unchanged by M3, but the matrix requires the net behaviour).
        with patch("formal_gates_harness.infra.os.replace",
                   side_effect=OSError("boom")):
            for store, base in ((self.store, self.live_root),
                                (self.reference, self.reference_root)):
                with self.subTest(kind=type(store).__name__):
                    with self.assertRaises(OSError):
                        store.put(value)
        # No final artifact file (put never committed) and no stray .tmp file.
        for base in (self.live_root, self.reference_root):
            with self.subTest(tree=base.name):
                artifacts = base / "artifacts"
                self.assertEqual(list(artifacts.glob("*.tmp")), [])
                self.assertEqual(list(artifacts.glob("*.json")), [])

    # -- get normal path ---------------------------------------------------

    def test_get_normal_path_returns_decoded(self):
        value = {"role": "qa", "nested": {"x": [1, 2, 3]}}
        ref = self.publish(value)
        self.assertEqual(self.store.get(ref), value)
        self.assertEqual(self.reference.get(ref), value)

    # -- malformed reference matrix ----------------------------------------

    def test_malformed_reference_matrix(self):
        cases = [
            "", "a" * 63, "a" * 65, "A" * 64,
            "g" * 64, "a b" * 32, "é" * 32, "a" * 62 + "é",
        ]
        for ref in cases:
            with self.subTest(ref=ref):
                want = self.compare_get(ref)
                # Class-identity: exactly HarnessError, not a subclass, with the
                # message byte-for-byte unchanged.
                self.assertIs(want[0], HarnessError)
                self.assertEqual(want[1], "invalid artifact reference")

    def test_valid_reference_missing_file_is_native_os_error(self):
        ref = "a" * 64
        want = self.compare_get(ref)
        self.assertEqual(want[0].__name__, "FileNotFoundError")
        self.assertNotEqual(want[0], HarnessError)

    def test_valid_reference_path_is_directory_is_native_os_error(self):
        ref = "a" * 64
        for base in (self.live_root, self.reference_root):
            self.artifact(base, ref).mkdir(parents=True)
        want = self.compare_get(ref)
        self.assertEqual(want[0].__name__, "IsADirectoryError")

    # -- tampering ---------------------------------------------------------

    def test_content_rewritten_to_other_json_is_integrity_refusal(self):
        ref = self.publish({"result": "PASS"})
        self.mutate(ref, b'{"result":"FAIL"}')
        want = self.compare_get(ref)
        self.assertIs(want[0], HarnessError)
        self.assertEqual(want[1], "artifact integrity mismatch")

    def test_content_rewritten_to_non_utf8_is_integrity_not_unicode(self):
        ref = self.publish({"result": "PASS"})
        # Must be an integrity refusal, never UnicodeDecodeError, on both paths.
        self.mutate(ref, b"\xff\xfe\x00not-json")
        want = self.compare_get(ref)
        self.assertIs(want[0], HarnessError)
        self.assertEqual(want[1], "artifact integrity mismatch")

    def test_artifact_with_duplicate_fields_decodes_last_wins(self):
        # The stored body carries duplicate keys; the reference is the sha256 of
        # those raw bytes so get must accept and json.loads last-wins.
        raw = b'{"a":1,"a":2}'
        ref = hashlib.sha256(raw).hexdigest()
        self.mutate(ref, raw)
        self.assertEqual(self.store.get(ref), {"a": 2})
        self.assertEqual(self.reference.get(ref), {"a": 2})

    def test_empty_body_with_its_sha256_is_json_decode_error(self):
        ref = hashlib.sha256(b"").hexdigest()
        self.mutate(ref, b"")
        want = self.compare_get(ref)
        self.assertEqual(want[0].__name__, "JSONDecodeError")

    # -- class-identity proof ---------------------------------------------

    def test_refusal_class_is_exactly_harness_error(self):
        # A business refusal must be HarnessError and NOT RuntimeBlocked, whose
        # assertRaises(HarnessError) would silently accept. Assert identity.
        ref = "A" * 64  # invalid format
        self.assertIs(outcome(self.store.get, ref, root=self.live_root)[0], HarnessError)
        self.assertIs(outcome(self.reference.get, ref, root=self.reference_root)[0],
                      HarnessError)
        # And it is never the subclass RuntimeBlocked.
        self.assertIsNot(outcome(self.store.get, ref, root=self.live_root)[0],
                         RuntimeBlocked)


if __name__ == "__main__":
    unittest.main()
