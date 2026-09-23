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


def verify_process(jobs_root, process):
    """Reference: the implementation from `Store.verify_process` at baseline C3.

    Uses the reference digest and file proof so the comparison is against the
    pre-migration behaviour end to end, not against a mixture of old and new
    primitives.
    """
    from pathlib import Path
    from formal_gates_harness.infra import HarnessError, RuntimeBlocked, read
    from reference_canonical import digest as reference_digest

    key = process.get('operation', '')
    directory = jobs_root / key
    if not key or Path(process.get('directory', '')).resolve() != directory.resolve():
        raise HarnessError('EVIDENCE_BINDING: job is outside this run')
    receipt = read(directory / 'exit.json')
    request = read(directory / 'request.json')
    if receipt.get('request_digest') != reference_digest(request):
        raise HarnessError('EVIDENCE_BINDING: request digest differs')
    for field in ('exit_code', 'error', 'request_digest', 'streams'):
        if receipt.get(field) != process.get(field):
            raise HarnessError(f'EVIDENCE_BINDING: process {field} differs')
    proofs = receipt.get('streams', {})
    if set(proofs) != {'stdout.log', 'stderr.log'}:
        raise RuntimeBlocked('EVIDENCE_MISSING: stdout/stderr proofs absent')
    for name, proof in proofs.items():
        if file_proof(directory / name) != proof:
            raise RuntimeBlocked(f'EVIDENCE_CHANGED: {name}')


def verify_input_files(input_files, label='comparison input '):
    """Reference: the input-file loop from `Store.verify_evidence` at baseline C3."""
    from pathlib import Path
    from formal_gates_harness.infra import RuntimeBlocked

    for name, proof in input_files.items():
        if file_proof(Path(name)) != proof:
            raise RuntimeBlocked(f'EVIDENCE_CHANGED: {label}{name}')
