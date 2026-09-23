"""End-to-end capacity boundary for publication and read-back (G1b, M3 repair).

M3's acceptance finding was a capacity *asymmetry*, not a wrong helper: every
unit was correct on its own, but publication accepted an artifact that read-back
could not carry, because the read request put the artifact body on the wire while
sharing one fixed input ceiling with every other bridge operation. Helper-level
tests cannot see that; only the real Store.put / Store.get pair and the real
worker receipt re-entry can.

These tests therefore bound the pair, not the helpers:

* read-back covers the largest artifact that publication actually accepts, found
  from the transport's own serialiser rather than from a constant copied here;
* the read request does not grow with the artifact, so the two ends cannot drift
  apart again by an expansion factor;
* a real worker completes, writes its receipt, and the same worker can re-enter
  it - which is the user-visible consequence the finding reported.
"""
import hashlib
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from formal_gates_harness import infra
from formal_gates_harness.infra import Bridge, Store
from formal_gates_harness.workers import Workers

PACKAGE = Path(__file__).resolve().parents[3]
BINARY = PACKAGE / ".harness/bin/formal-gates"

# The ceiling every bridge operation shares, as the transport documents it.
WIRE_LIMIT = 8 * 1024 * 1024

# The size the M3 acceptance finding used for its counterexample.
FINDING_SIZE = 6400 * 1024


def request_size(operation, payload):
    """Bytes this operation would actually put on the wire."""
    return len(infra._transport_bytes({"operation": operation, "payload": payload}))


def publish_payload(value):
    return {"value": value}


class MeasuringBridge(Bridge):
    """A real bridge that also records the size of each request it sends.

    Measurements come from the production bridge call, not from a second copy of
    the payload shape, so a test cannot accidentally agree with itself.
    """

    def __init__(self, binary):
        super().__init__(binary)
        self.requests = []

    def call(self, operation, payload):
        self.requests.append((operation, len(infra._transport_bytes(
            {"operation": operation, "payload": payload}))))
        return super().call(operation, payload)

    def sizes_for(self, operation):
        return [size for name, size in self.requests if name == operation]


class ArtifactCapacityBoundary(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name).resolve()
        self.bridge = MeasuringBridge(str(BINARY))

    def tearDown(self):
        self._tmp.cleanup()

    def store(self, name="run"):
        return Store(self.root / name, {}, self.bridge)

    def artifact(self, store, ref):
        return store.directory / "artifacts" / f"{ref}.json"

    @staticmethod
    def largest_publishable_summary():
        """Largest single-string value whose publish request still fits.

        Derived by bisection on the transport's own serialiser, so it tracks the
        real ceiling instead of a constant that would rot.
        """
        low, high = 0, WIRE_LIMIT
        while low < high:
            mid = (low + high + 1) // 2
            size = request_size("artifact-plan", publish_payload({"summary": "x" * mid}))
            if size <= WIRE_LIMIT:
                low = mid
            else:
                high = mid - 1
        return low

    # -- the required closure: read covers what publication accepts ---------

    def test_read_back_covers_the_largest_publishable_artifact(self):
        store = self.store("at-limit")
        size = self.largest_publishable_summary()
        value = {"summary": "x" * size}
        # The value really is at the ceiling: one more byte would not be accepted.
        self.assertLessEqual(request_size("artifact-plan", publish_payload(value)), WIRE_LIMIT)
        self.assertGreater(
            request_size("artifact-plan", publish_payload({"summary": "x" * (size + 1)})),
            WIRE_LIMIT)

        ref = store.put(value)
        body = self.artifact(store, ref).read_bytes()
        self.assertGreater(len(body), 8 * 1024 * 1024 - 1024)

        # The whole point: what put() accepted, get() returns.
        self.assertEqual(store.get(ref), value)

    def test_read_back_covers_the_finding_size_end_to_end(self):
        # The exact counterexample from the acceptance finding, on the real pair.
        store = self.store("finding-size")
        value = {"summary": "x" * FINDING_SIZE}
        ref = store.put(value)
        body = self.artifact(store, ref).read_bytes()
        self.assertEqual(hashlib.sha256(body).hexdigest(), ref)
        self.assertEqual(store.get(ref), value)

    # -- why it cannot drift again ------------------------------------------

    def test_read_request_does_not_grow_with_the_artifact(self):
        # The asymmetry was structural: the read request carried the body, so it
        # grew with the artifact while sharing a fixed ceiling. Bind that.
        bridge = self.bridge
        small = self.store("small")
        large = self.store("large")
        small_ref = small.put({"summary": "x" * 1024})
        large_ref = large.put({"summary": "x" * FINDING_SIZE})

        self.assertEqual(small.get(small_ref), {"summary": "x" * 1024})
        self.assertEqual(large.get(large_ref), {"summary": "x" * FINDING_SIZE})

        read_sizes = set(bridge.sizes_for("artifact-binding"))
        self.assertEqual(len(read_sizes), 1,
                         f"read request size depends on the artifact: {sorted(read_sizes)}")
        self.assertLess(max(read_sizes), 4096,
                        "the read request should be a fixed, small envelope")

    def test_read_accepts_an_artifact_larger_than_the_publish_ceiling(self):
        # Not a publishable input - written straight to disk - but it shows the
        # read side is size-independent rather than merely re-tuned to today's
        # publish ceiling.
        store = self.store("above-limit")
        size = WIRE_LIMIT + 1024 * 1024
        body = json.dumps({"summary": "x" * size}, separators=(",", ":")).encode()
        self.assertGreater(len(body), WIRE_LIMIT)
        ref = hashlib.sha256(body).hexdigest()
        path = self.artifact(store, ref)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(body)

        self.assertEqual(store.get(ref), {"summary": "x" * size})

    # -- the user-visible consequence: completed work can be resumed ---------

    def test_worker_completion_receipt_reentry_at_finding_size(self):
        manifest = {"revision": "r" * 64, "fixture": True,
                    "spec": {"provider": "fixture", "checks": []}}
        store = Store(self.root / "worker-run", manifest, self.bridge)
        workspace = self.root / "workspace"
        workspace.mkdir()
        worker = Workers(store, PACKAGE)
        key = {"run_id": "capacity-boundary", "round": 0, "candidate": "c" * 40}
        result = {"status": "PASS", "summary": "r" * FINDING_SIZE,
                  "findings": [], "check_ids": []}

        with patch.object(worker, "fixture", return_value=result) as fixture:
            first = worker.run("technical-review", workspace, key)
            receipt = next((store.directory / "workers").glob("*.json"))
            record = json.loads(receipt.read_text())
            before = receipt.read_bytes()
            # Re-entry reads the published receipt back through Store.get.
            replayed = worker.run("technical-review", workspace, key)

        self.assertEqual(fixture.call_count, 1,
                         "re-entry must resume the receipt, not re-run the worker")
        self.assertEqual(receipt.read_bytes(), before, "re-entry rewrote the receipt")
        self.assertEqual(first, replayed)
        self.assertEqual(store.get(record["ref"]), first)


if __name__ == "__main__":
    unittest.main()
