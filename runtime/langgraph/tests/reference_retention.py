"""Pre-migration reference copy of the retention judgements, on the v2 payloads (G3b rework).

Test-only. Production must not import this module. The conditions, branch
order and refusal wording below are the migration-PRIOR logic of retention.py
at HEAD f287033, lifted onto the fact/chunk payload shapes the v2 Go bridge
operations take: inventories, member proofs and staleness rows travel in
CHUNK-sized batches, identities travel as small typed facts, every ordering is
carried by arrays (batch order = walk order, verify entries = the manifest's
sorted files, stale rows = current-iteration order), and a heterogeneous
identity value is normalized to None exactly where the Python wrapper
normalizes it before bridging.

The corpus in test_retention_differential.py pins these shapes against the
real Go binary call for call, so an implementation that re-wholes a payload,
reorders the branches or moves a boundary by one turns the differential red.
"""
from pathlib import PurePosixPath

TERMINAL = {'HARNESS_COMPLETE', 'DEMO_COMPLETE', 'ABORTED'}
SKIP = {'runner.lock', 'checkpoints.sqlite-wal', 'checkpoints.sqlite-shm', 'archive.json'}
KEEP = {'manifest.json', 'requirement.md', 'reviewer-base.md', 'runner.lock',
        'transport-adoption.json', 'harness-result.json', 'archive.json'}
CHUNK = 2000  # mirrors retentionChunk in internal/harness/retention.go


def _no(refusal):
    return {'ok': False, 'refusal': refusal}


def archive_inventory(entries):
    """inventory()'s admission over one batch of [{name, kind}] in walked order, SKIP first."""
    files = []
    for entry in entries:
        name, kind = entry['name'], entry['kind']
        if name in SKIP: continue
        if kind == 'symlink': return _no(f'ARCHIVE_UNSUPPORTED: symlink {name}; no files deleted')
        if kind == 'dir': continue
        if kind != 'file': return _no(f'ARCHIVE_UNSUPPORTED: non-regular entry {name}')
        files.append(name)
    return {'ok': True, 'files': files}


def archive_destination(directory, destination):
    """archive_run's is_relative_to refusal, component semantics."""
    if PurePosixPath(destination).is_relative_to(PurePosixPath(directory)):
        return _no('ARCHIVE_PATH: output must be outside the run')
    return {'ok': True}


def archive_settled(status_name, jobs):
    """_terminal's status check plus ensure_jobs_settled, order preserved.

    Each process carries the wrapper-computed pid_valid fact (isinstance int,
    bool excluded, positive) and the plain-int pid (null for a record that
    held no integer), so no CPython semantics are emulated here.
    """
    if status_name not in TERMINAL:
        return _no('RUN_NOT_TERMINAL: active or paused run cannot be archived/pruned')
    for job in jobs:
        if job['has_request'] and not job['has_exit']:
            return _no(f"RUN_UNSETTLED: {job['name']} has no exit receipt; no cleanup")
        for process in job['processes']:
            if not process['pid_valid'] or process['pid'] is None:
                return _no('RUN_UNSETTLED: invalid process identity')
            if process['probe'] == 'permission':
                return _no('RUN_UNSETTLED: process cannot be checked')
            if process['probe'] == 'alive':
                return _no(f"RUN_UNSETTLED: PID {process['pid']} still exists; no cleanup")
    return {'ok': True}


def archive_index_plan(source_directory, run_id, revision, runtime_identity,
                       status, database_logical_sha256):
    """archive_run's index skeleton; the files proofs join on the Python side."""
    return {'ok': True, 'skeleton': {'format': 'formal-gates-archive-v1',
            'source_directory': source_directory, 'run_id': run_id, 'revision': revision,
            'runtime_identity': runtime_identity, 'status': status,
            'database_logical_sha256': database_logical_sha256}}


def archive_verify(duplicates, format, status_name, names_match, entries):
    """checked_archive over facts plus one batch of entries, verdict order kept:
    duplicates, format/terminal status, member set, then per-member safety and
    typed content in the batch's array order."""
    if duplicates: return _no('ARCHIVE_INVALID: duplicate members')
    if format != 'formal-gates-archive-v1' or status_name not in TERMINAL:
        return _no('ARCHIVE_INVALID: wrong format or nonterminal run')
    if not names_match: return _no('ARCHIVE_INVALID: missing or extra members')
    for entry in entries:
        name = entry['name']
        p = PurePosixPath(name)
        if p.is_absolute() or '..' in p.parts or '\\' in name:
            return _no('ARCHIVE_INVALID: unsafe member path')
        if entry['actual'] != entry['expected']:
            return _no(f'ARCHIVE_INVALID: content differs: {name}')
    return {'ok': True}


def archive_stale_facts(has_new, has_missing, allow_missing):
    """_compare_live's set drift as two facts: new files first, then missing."""
    if has_new: return _no('ARCHIVE_STALE: new files appeared; no deletion')
    if not allow_missing and has_missing: return _no('ARCHIVE_STALE: source files missing')
    return {'ok': True}


def archive_stale_entries(entries):
    """_compare_live's per-name comparison over one batch, current-iteration
    order; the database row compares the logical-hash strings."""
    for entry in entries:
        if entry['kind'] == 'database':
            equal = entry['actual_db'] == entry['expected_db']
        else:
            equal = entry['actual_proof'] == entry['expected_proof']
        if not equal: return _no(f"ARCHIVE_STALE: source differs: {entry['name']}")
    return {'ok': True}


def archive_projection_precheck(archive_proof, marker):
    """archived_status's pre-zip comparison; marker is None or carries archive_proof."""
    if marker is None: return {'ok': True}
    if archive_proof != marker['archive_proof']:
        return _no('ARCHIVE_CHANGED: retained archive missing or altered')
    return {'ok': True}


def archive_projection(status, source_directory, file_count, archive, marker, directory):
    """archive_status / archived_status's full projection, binding and merge.

    marker is None or the wrapper-built {run_id, revision, cleanup_complete}
    (natural key accesses keep the raw KeyError on the caller's side).
    """
    projection = {**status, 'archived': True, 'archive': archive,
                  'archive_verified': True, 'source_directory': source_directory,
                  'file_count': file_count, 'model_calls': 0}
    if marker is None: return {'ok': True, 'status': projection}
    if (status.get('run_id') != marker['run_id'] or status.get('revision') != marker['revision']
            or source_directory != directory):
        return _no('ARCHIVE_BINDING: tombstone does not match archive')
    return {'ok': True, 'status': {**projection, 'cleanup_complete': marker['cleanup_complete']}}


def prune_admission(runs_dir, directory, directory_name, run_id, archive):
    """prune_run's eligibility: parent must be runs_dir, leaf must be the run id."""
    if PurePosixPath(directory).parent != PurePosixPath(runs_dir) or directory_name != run_id:
        return _no("PRUNE_PATH: only this installation's run directories are eligible")
    if PurePosixPath(archive).is_relative_to(PurePosixPath(directory)):
        return _no('ARCHIVE_PATH: archive must be outside the run')
    return {'ok': True}


def prune_bind(directory, run_id, revision, source_directory, index_run_id, index_revision):
    """prune_run's index binding; non-string index values arrive as None."""
    if (source_directory != directory or index_run_id != run_id or index_revision != revision):
        return _no('ARCHIVE_BINDING: wrong run/revision')
    return {'ok': True}


def prune_marker(archive, archive_proof, marker_archive, marker_archive_proof):
    """prune_run's tombstone-to-archive binding (the `if marker and ...` guard)."""
    if archive != marker_archive or archive_proof != marker_archive_proof:
        return _no('ARCHIVE_BINDING: use the original cleanup archive')
    return {'ok': True}


def prune_plan(entries, run_id, revision, terminal_status, archive, marker_exists):
    """prune_run's removal plan over name+size rows, current order. The plan's
    archive_proof is merged by the wrapper, which owns the hashing."""
    sizes = {entry['name']: entry['size'] for entry in entries}
    remove = sorted((name for name in sizes if name not in KEEP),
                    key=lambda name: (-name.count('/'), name))
    report = {'status': 'PRUNE_PREVIEW', 'run_id': run_id, 'terminal_status': terminal_status,
              'archive': archive, 'files_to_remove': len(remove),
              'bytes_to_remove': sum(sizes[name] for name in remove), 'model_calls': 0}
    planned = None
    if not marker_exists:
        planned = {'format': 'formal-gates-prune-v1', 'run_id': run_id, 'revision': revision,
                   'archive': archive, 'cleanup_complete': False}
    return {'ok': True, 'report': report, 'remove': remove, 'marker': planned}
