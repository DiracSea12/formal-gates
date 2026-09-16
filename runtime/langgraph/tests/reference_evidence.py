"""Reference implementation of evidence content proofs as it stood before the migration.

Test-only. Production code must not import this module. The body below is the
implementation from `formal_gates_harness/infra.py` at baseline C1, unchanged.
"""
import hashlib

from formal_gates_harness.infra import RuntimeBlocked


def file_proof(path):
    try:
        body = path.read_bytes()
    except OSError as exc:
        raise RuntimeBlocked(f'EVIDENCE_MISSING: {path}') from exc
    return {'sha256': hashlib.sha256(body).hexdigest(), 'size': len(body)}
