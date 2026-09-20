"""Pre-migration reference copy of the ten retention judgements (G3b).

Test-only. Production must not import this module. The bodies below are the
migration-PRIOR logic of retention.py at HEAD f287033 — inventory admission,
the archive destination/settlement/index/verify/stale/projection rules and the
prune admission/bind/plan rules — copied verbatim and lifted onto the payload
shape the Go bridge operations take. Conditions, branch order and refusal
wording are the contract the Go ops must reproduce.

One deliberate, documented divergence: this reference keeps CPython's
isinstance(int) semantics for pids (a JSON true would pass); the Go op refuses
it. The corpus below avoids boolean pids so the differential pins everything
else byte for byte.
"""
from pathlib import PurePosixPath

TERMINAL = {'HARNESS_COMPLETE', 'DEMO_COMPLETE', 'ABORTED'}
SKIP = {'runner.lock', 'checkpoints.sqlite-wal', 'checkpoints.sqlite-shm', 'archive.json'}
KEEP = {'manifest.json', 'requirement.md', 'reviewer-base.md', 'runner.lock',
        'transport-adoption.json', 'harness-result.json', 'archive.json'}


def _no(refusal):
    return {'ok': False, 'refusal': refusal}


def archive_inventory(entries):
    """inventory()'s admission over [{name, kind}] in walked order, SKIP first."""
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
    """_terminal's status check plus ensure_jobs_settled, order preserved."""
    if status_name not in TERMINAL:
        return _no('RUN_NOT_TERMINAL: active or paused run cannot be archived/pruned')
    for job in jobs:
        if job['has_request'] and not job['has_exit']:
            return _no(f"RUN_UNSETTLED: {job['name']} has no exit receipt; no cleanup")
        for process in job['processes']:
            pid = process['pid']
            if not isinstance(pid, int) or pid <= 0:
                return _no('RUN_UNSETTLED: invalid process identity')
            probe = process['probe']
            if probe == 'permission':
                return _no('RUN_UNSETTLED: process cannot be checked')
            if probe == 'alive':
                return _no(f'RUN_UNSETTLED: PID {pid} still exists; no cleanup')
    return {'ok': True}


def archive_index_plan(destination_exists, source_directory, run_id, revision,
                       runtime_identity, status, files, database_logical_sha256, checkpoints_proof):
    """archive_run's existence refusal plus index assembly."""
    if destination_exists:
        return _no('ARCHIVE_EXISTS: use a new destination; not overwritten')
    merged = dict(files); merged['checkpoints.sqlite'] = checkpoints_proof
    return {'ok': True, 'index': {'format': 'formal-gates-archive-v1',
            'source_directory': source_directory, 'run_id': run_id, 'revision': revision,
            'runtime_identity': runtime_identity, 'status': status, 'files': merged,
            'database_logical_sha256': database_logical_sha256}}


def archive_verify(names, index=None, member_proofs=None):
    """checked_archive in two phases: names only, then the manifest rules."""
    if index is None:
        if len(names) != len(set(names)): return _no('ARCHIVE_INVALID: duplicate members')
        return {'ok': True}
    if index.get('format') != 'formal-gates-archive-v1' or index['status']['status'] not in TERMINAL:
        return _no('ARCHIVE_INVALID: wrong format or nonterminal run')
    if set(names) != {'archive-manifest.json', *('run/' + n for n in index['files'])}:
        return _no('ARCHIVE_INVALID: missing or extra members')
    for name, expected in index['files'].items():
        p = PurePosixPath(name)
        if p.is_absolute() or '..' in p.parts or '\\' in name:
            return _no('ARCHIVE_INVALID: unsafe member path')
        if member_proofs.get(name) != expected:
            return _no(f'ARCHIVE_INVALID: content differs: {name}')
    return {'ok': True, 'index': index}


def archive_stale(current, files, allow_missing, database_logical_sha256, live_database_logical_sha256):
    """_compare_live's drift rules; the live logical db hash is a Python fact."""
    if set(current) - set(files): return _no('ARCHIVE_STALE: new files appeared; no deletion')
    if not allow_missing and set(current) != set(files): return _no('ARCHIVE_STALE: source files missing')
    for name, value in current.items():
        if name == 'checkpoints.sqlite':
            equal = live_database_logical_sha256 == database_logical_sha256
        else: equal = value == files[name]
        if not equal: return _no(f'ARCHIVE_STALE: source differs: {name}')
    return {'ok': True}


def archive_projection(index, archive, marker, directory, archive_proof=None):
    """archive_status / archived_status, two phases like archive-verify.

    Without an index this is archived_status's pre-check: ARCHIVE_CHANGED must
    fire before any zip is opened. With the verified index it is the
    projection, the binding check and the cleanup_complete merge.
    """
    if index is None:
        if marker is None: return {'ok': True}
        if archive_proof != marker['archive_proof']:
            return _no('ARCHIVE_CHANGED: retained archive missing or altered')
        return {'ok': True}
    status = {**index['status'], 'archived': True, 'archive': archive,
              'archive_verified': True, 'source_directory': index['source_directory'],
              'file_count': len(index['files']), 'model_calls': 0}
    if marker is None: return {'ok': True, 'status': status}
    if archive_proof != marker['archive_proof']:
        return _no('ARCHIVE_CHANGED: retained archive missing or altered')
    if (status['run_id'] != marker['run_id'] or status['revision'] != marker['revision'] or
        status['source_directory'] != directory):
        return _no('ARCHIVE_BINDING: tombstone does not match archive')
    return {'ok': True, 'status': {**status, 'cleanup_complete': marker.get('cleanup_complete', False)}}


def prune_admission(runs_dir, directory, directory_name, run_id, archive):
    """prune_run's eligibility: parent must be runs_dir, leaf must be the run id."""
    if PurePosixPath(directory).parent != PurePosixPath(runs_dir) or directory_name != run_id:
        return _no("PRUNE_PATH: only this installation's run directories are eligible")
    if PurePosixPath(archive).is_relative_to(PurePosixPath(directory)):
        return _no('ARCHIVE_PATH: archive must be outside the run')
    return {'ok': True}


def prune_bind(directory, run_id, revision, source_directory, index_run_id, index_revision):
    """prune_run's index binding to this run, revision and directory."""
    if (source_directory != directory or index_run_id != run_id or index_revision != revision):
        return _no('ARCHIVE_BINDING: wrong run/revision')
    return {'ok': True}


def prune_plan(marker, archive, archive_proof, current, files, database_logical_sha256,
               live_database_logical_sha256, allow_missing, run_id, revision, terminal_status, directory):
    """prune_run's marker binding, staleness, removal plan and report."""
    if marker and (marker['archive'] != archive or marker['archive_proof'] != archive_proof):
        return _no('ARCHIVE_BINDING: use the original cleanup archive')
    stale = archive_stale(current, files, allow_missing, database_logical_sha256,
                          live_database_logical_sha256)
    if not stale['ok']: return stale
    remove = sorted((n for n in current if n not in KEEP), key=lambda n: (-n.count('/'), n))
    report = {'status': 'PRUNE_PREVIEW', 'run_id': run_id, 'terminal_status': terminal_status,
              'archive': archive, 'files_to_remove': len(remove),
              'bytes_to_remove': sum(current[n]['size'] for n in remove), 'model_calls': 0}
    planned = None
    if marker is None:
        planned = {'format': 'formal-gates-prune-v1', 'run_id': run_id, 'revision': revision,
                   'archive': archive, 'archive_proof': archive_proof, 'cleanup_complete': False}
    return {'ok': True, 'report': report, 'remove': remove, 'marker': planned}
