"""Explicit terminal-run retention: verified archive, preview, then local pruning.

No background deletion and no pruning of a running/paused/unknown invocation.
An archive is evidence, not another scheduler. A small tombstone retains status
while the full source, checkpoints (SQLite backup), jobs and artifacts live in ZIP.

The retention *rules* live in `internal/harness` (G3b): member admission,
destination/settlement/index/verify/stale/projection judgements and the prune
admission/bind/plan rules. This module keeps the filesystem facts and effects —
locking, hashing, the SQLite backup, zip reading and writing, process probing
(os.kill) and the deletions themselves — and raises the HarnessError family
directly from the refusal a judgement returns.
"""
from __future__ import annotations
import contextlib
import hashlib
import json
import os
from pathlib import Path
import sqlite3
import tempfile
import zipfile
from .infra import HarnessError, atomic, canonical, driver_bridge, read, run_lock

MARKER = 'archive.json'


def _decide(answer: dict) -> dict:
    """Raise the HarnessError family for a typed retention refusal."""
    if answer.get('ok'):
        return answer
    refusal = answer.get('refusal')
    if not refusal:
        raise HarnessError('Go bridge returned an unusable retention answer')
    raise HarnessError(refusal)


def stream_proof(stream):
    h = hashlib.sha256(); size = 0
    for block in iter(lambda: stream.read(1024 * 1024), b''):
        h.update(block); size += len(block)
    return {'sha256': h.hexdigest(), 'size': size}


def proof(path):
    with Path(path).open('rb') as stream:
        return stream_proof(stream)


def db_proof(path):
    # Hash logical contents, not WAL page layout or reader-created -shm files.
    with contextlib.closing(sqlite3.connect(Path(path).resolve().as_uri() + '?mode=ro', uri=True)) as c:
        h = hashlib.sha256()
        for line in c.iterdump(): h.update(line.encode()); h.update(b'\n')
        return h.hexdigest()


def inventory(directory):
    # The filesystem walk and the hashing stay here; the admission rule (SKIP
    # before the symlink refusal, directories skipped, non-regular refused) is
    # owned by archive-inventory and runs over the walked order.
    walked, paths = [], {}
    for p in sorted(directory.rglob('*')):
        name = p.relative_to(directory).as_posix()
        kind = ('symlink' if p.is_symlink() else 'dir' if p.is_dir()
                else 'file' if p.is_file() else 'other')
        walked.append({'name': name, 'kind': kind}); paths[name] = p
    answered = _decide(driver_bridge().call('archive-inventory', {'entries': walked}))
    entries = {}
    for name in answered['files']:
        entries[name] = proof(paths[name])
    return entries


def _probe(pid):
    # The liveness probe (os.kill) stays here. A pid that is not a positive
    # int is never probed: Go refuses it first, exactly as the pre-migration
    # isinstance check ran before os.kill.
    if not isinstance(pid, int) or pid <= 0: return 'dead'
    try: os.kill(pid, 0)
    except ProcessLookupError: return 'dead'
    except PermissionError: return 'permission'
    return 'alive'


def _jobs_facts(directory):
    # Job-tree projection for archive-settled: iterdir order, the request/exit
    # receipts and each recorded pid with its probe outcome.
    jobs = []
    root = directory / 'jobs'
    for job in root.iterdir() if root.exists() else ():
        if not job.is_dir(): continue
        processes = []
        for name in ('process.json', 'native-process.json'):
            record = job / name
            if not record.exists(): continue
            pid = read(record).get('pid')
            processes.append({'pid': pid, 'probe': _probe(pid)})
        jobs.append({'name': job.name, 'has_request': (job / 'request.json').exists(),
                     'has_exit': (job / 'exit.json').exists(), 'processes': processes})
    return jobs


def checked_archive(path):
    """Read-only standalone verification, independent of the originating runtime."""
    path = Path(path).resolve()
    try:
        with zipfile.ZipFile(path) as z:
            names = z.namelist()
            _decide(driver_bridge().call('archive-verify', {'names': names}))
            index = json.loads(z.read('archive-manifest.json'))
            # Today's access order, kept here so a missing key still surfaces
            # as the wrapped KeyError before any Go verdict on the manifest.
            index['status']['status']; index['files']
            wanted = {'run/' + n for n in index['files']}
            member_proofs = {}
            for name in names:
                if name in wanted:
                    with z.open(name) as f: member_proofs[name[len('run/'):]] = stream_proof(f)
            answer = _decide(driver_bridge().call(
                'archive-verify', {'names': names, 'index': index, 'member_proofs': member_proofs}))
        return answer['index']
    except (zipfile.BadZipFile, KeyError, ValueError, OSError) as exc:
        raise HarnessError(f'ARCHIVE_INVALID: {exc}') from exc


def archive_status(path):
    index = checked_archive(path)
    index['source_directory']  # today reads this directly; keep the raw KeyError
    answered = _decide(driver_bridge().call('archive-projection', {
        'index': index, 'archive': str(Path(path).resolve()), 'marker': None, 'directory': ''}))
    return answered['status']


def archived_status(directory):
    marker = read(directory / MARKER)
    archive = marker['archive']
    archive_proof = proof(archive)      # today compares this before opening the zip
    marker['archive_proof']             # today's right-hand key access, same order
    # Pre-check phase (no index): ARCHIVE_CHANGED fires before any zip work,
    # exactly where the pre-migration comparison sat.
    _decide(driver_bridge().call('archive-projection', {
        'archive': str(Path(archive).resolve()), 'archive_proof': archive_proof,
        'marker': marker, 'directory': str(directory)}))
    index = checked_archive(archive)
    answered = _decide(driver_bridge().call('archive-projection', {
        'index': index, 'archive': str(Path(archive).resolve()),
        'archive_proof': archive_proof, 'marker': marker, 'directory': str(directory)}))
    return answered['status']


def _terminal(store, package):
    # Deferred imports avoid cycles; do not call run_graph under its own lock.
    from .cli import read_checkpointer, status_projection
    from .graph import Flow
    if (store.directory / MARKER).exists(): return archived_status(store.directory)
    with read_checkpointer(store.directory / 'checkpoints.sqlite') as saver:
        status = status_projection(store, Flow(store, package).build(saver),
                                   {'configurable': {'thread_id': store.manifest['run_id']}})
    # Terminal-set and job-settlement refusals share one call so their order
    # (status first, then jobs) stays structural.
    _decide(driver_bridge().call('archive-settled',
                                 {'status_name': status['status'], 'jobs': _jobs_facts(store.directory)}))
    # Verify all accepted evidence, not just its terminal status string.
    for receipt in (store.directory / 'workers').glob('*.json'):
        store.verify_evidence(read(receipt)['ref'])
    terminal = status.get('terminal') or {}
    for result in [*terminal.get('checks', []), *terminal.get('gates', [])]:
        store.verify_evidence(result['evidence'])
    return status


def archive_run(directory, destination):
    from .cli import open_run
    package, store = open_run(directory); directory = store.directory
    destination = Path(destination).resolve()
    _decide(driver_bridge().call('archive-destination',
                                 {'directory': str(directory), 'destination': str(destination)}))
    with run_lock(directory):
        if (directory / MARKER).exists(): raise HarnessError('RUN_ARCHIVED: use the retained archive')
        status = _terminal(store, package)
        original = inventory(directory)
        logical_db = db_proof(directory / 'checkpoints.sqlite')
        destination.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='.archive-', dir=destination.parent) as tmp:
            temp = Path(tmp); backup = temp / 'checkpoints.sqlite'
            with contextlib.closing(sqlite3.connect((directory / 'checkpoints.sqlite').as_uri() + '?mode=ro', uri=True)) as src:
                with contextlib.closing(sqlite3.connect(backup)) as dst: src.backup(dst)
            if db_proof(backup) != logical_db: raise HarnessError('ARCHIVE_CHANGED: database changed while backing up')
            files = dict(original); files['checkpoints.sqlite'] = proof(backup)
            index = _decide(driver_bridge().call('archive-index-plan', {
                'destination_exists': destination.exists(), 'source_directory': str(directory),
                'run_id': store.manifest['run_id'], 'revision': store.manifest['revision'],
                'runtime_identity': store.manifest['runtime_identity'], 'status': status,
                'files': files, 'database_logical_sha256': logical_db,
                'checkpoints_proof': files['checkpoints.sqlite']}))['index']
            target = temp / 'run.zip'
            with zipfile.ZipFile(target, 'w', zipfile.ZIP_DEFLATED, compresslevel=6) as z:
                for name in files:
                    z.write(backup if name == 'checkpoints.sqlite' else directory / name, 'run/' + name)
                z.writestr('archive-manifest.json', canonical(index))
            checked_archive(target)
            _compare_live(directory, index, allow_missing=False)
            with target.open('rb') as f: os.fsync(f.fileno())
            os.link(target, destination)  # publish without overwriting an existing file
            fd = os.open(destination.parent, os.O_RDONLY)
            try: os.fsync(fd)
            finally: os.close(fd)
        return {'status': 'ARCHIVED', 'run_id': store.manifest['run_id'],
                'archive': str(destination), 'archive_proof': proof(destination),
                'file_count': len(files), 'source_bytes': sum(p['size'] for p in original.values()),
                'source_deleted': False, 'model_calls': 0}


def _compare_live(directory, index, allow_missing):
    current = inventory(directory)
    # The live logical database hash is a fact only the sqlite reader here can
    # produce; archive-stale compares it against the index expectation.
    live = db_proof(directory / 'checkpoints.sqlite') if 'checkpoints.sqlite' in current else None
    _decide(driver_bridge().call('archive-stale', {
        'current': current, 'files': index['files'], 'allow_missing': allow_missing,
        'database_logical_sha256': index['database_logical_sha256'],
        'live_database_logical_sha256': live}))
    return current


def prune_run(directory, archive, confirm=False):
    from .cli import open_run
    package, store = open_run(directory); directory = store.directory
    # Only runs created by this installation, never an arbitrary project root.
    _decide(driver_bridge().call('prune-admission', {
        'runs_dir': str((package / '.harness/runs').resolve()), 'directory': str(directory),
        'directory_name': directory.name, 'run_id': store.manifest['run_id'],
        'archive': str(Path(archive).resolve())}))
    archive = Path(archive).resolve()
    with run_lock(directory):
        marker_path = directory / MARKER
        marker = read(marker_path) if marker_path.exists() else None
        index = checked_archive(archive)
        index['source_directory']; index['run_id']; index['revision']
        _decide(driver_bridge().call('prune-bind', {
            'directory': str(directory), 'run_id': store.manifest['run_id'],
            'revision': store.manifest['revision'],
            'source_directory': index['source_directory'], 'index_run_id': index['run_id'],
            'index_revision': index['revision']}))
        status = _terminal(store, package)
        if marker: marker['archive']; marker['archive_proof']  # today's truthiness gate and order
        current = inventory(directory)
        live = db_proof(directory / 'checkpoints.sqlite') if 'checkpoints.sqlite' in current else None
        answered = _decide(driver_bridge().call('prune-plan', {
            'marker': marker, 'archive': str(archive), 'archive_proof': proof(archive),
            'current': current, 'files': index['files'],
            'database_logical_sha256': index['database_logical_sha256'],
            'live_database_logical_sha256': live, 'allow_missing': marker is not None,
            'run_id': store.manifest['run_id'], 'revision': store.manifest['revision'],
            'terminal_status': status['status'], 'directory': str(directory)}))
        report, remove = answered['report'], answered['remove']
        if not confirm: return report
        if marker is None:
            marker = answered['marker']
            atomic(marker_path, marker)  # crash recovery marker BEFORE the first deletion
        # Delete only verified inventory entries. Never recurse through an unknown tree.
        for name in remove: (directory / name).unlink()
        for p in sorted(directory.rglob('*'), key=lambda p: len(p.parts), reverse=True):
            if p.is_dir() and not p.is_symlink():
                try: p.rmdir()
                except OSError: pass
        for name in ('checkpoints.sqlite-wal', 'checkpoints.sqlite-shm'):
            (directory / name).unlink(missing_ok=True)
        marker['cleanup_complete'] = True; atomic(marker_path, marker)
        return {**report, 'status': 'PRUNED', 'removed_files': len(remove),
                'removed_bytes': report['bytes_to_remove'], 'source_run_resumable': False,
                'note': 'Terminal status remains readable; complete evidence is in the verified ZIP.'}
