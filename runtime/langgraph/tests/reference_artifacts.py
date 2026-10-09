"""Independent oracle for content-addressed Store.put / Store.get behavior.

Test-only; production code must not import this module. The reference uses
sha256(canonical(value)) with the Go-backed canonical encoder. Differential
checks compare references, non-finite-number refusals, path layout,
publish-once checks, and refusal ordering.
"""
import hashlib
import json

from formal_gates_harness.infra import HarnessError, atomic_bytes, canonical


class ReferenceStore:
    def __init__(self, directory, manifest, bridge):
        self.directory, self.manifest, self.bridge = directory, manifest, bridge

    def put(self, value):
        body = canonical(value); ref = hashlib.sha256(body).hexdigest()
        path = self.directory / "artifacts" / f"{ref}.json"
        # Write the bytes already produced: re-deriving them inside atomic()
        # would encode the same value a second time, across a second process.
        if not path.exists(): atomic_bytes(path, body)
        return ref

    def get(self, ref):
        if len(ref) != 64 or any(c not in "0123456789abcdef" for c in ref):
            raise HarnessError("invalid artifact reference")
        body = (self.directory / "artifacts" / f"{ref}.json").read_bytes()
        if hashlib.sha256(body).hexdigest() != ref:
            raise HarnessError("artifact integrity mismatch")
        return json.loads(body)
