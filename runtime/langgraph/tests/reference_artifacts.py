"""Reference implementation of Store.put / Store.get as they stood before M3.

Test-only. Production code must not import this module. The bodies below are the
pre-migration implementations from formal_gates_harness/infra.py (baseline M2),
unchanged. The content-addressing rule (ref = sha256(canonical(value))) lived
there verbatim; at that baseline canonical() was already backed by Go, so this
reference calls the same Go-backed canonical and therefore yields byte-identical
references and the same non-finite-number behaviour as the migrated unit. The
point of the differential is to prove the Python adapter plumbing (path layout,
publish-once check, the two-call refusal ordering) preserves the behaviour.
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
