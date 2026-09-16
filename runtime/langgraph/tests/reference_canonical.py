"""Reference implementation of the canonical encoding as it stood before C3.

Test-only. Production code must not import this module. The bodies below are the
implementations from `formal_gates_harness/infra.py` at baseline C2, unchanged.
"""
import hashlib
import json


def canonical(value) -> bytes:
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()


def digest(value) -> str:
    return hashlib.sha256(canonical(value)).hexdigest()
