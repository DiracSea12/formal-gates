"""Explicit terminal-run retention: verified archive, preview, then local pruning.

No background deletion and no pruning of a running/paused/unknown invocation.
An archive is evidence, not another scheduler. A small tombstone retains status
while the full source, checkpoints (SQLite backup), jobs and artifacts live in ZIP.
"""
from __future__ import annotations
import contextlib
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import sqlite3
import tempfile
import zipfile
from .infra import DogfoodError, Store, atomic, canonical, read, run_lock

TERMINAL = {'DOGFOOD_COMPLETE', 'DEMO_COMPLETE', 'ABORTED'}
MARKER = 'archive.json'
KEEP = {'manifest.json', 'requirement.md', 'reviewer-base.md', 'runner.lock',
        'transport-adoption.json', 'dogfood-result.json', MARKER}
SKIP = {'runner.lock', 'checkpoints.sqlite-wal', 'checkpoints.sqlite-shm', MARKER}


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
    entries = {}
    for p in sorted(directory.rglob('*')):
        name = p.relative_to(directory).as_posix()
        if name in SKIP: continue
        if p.is_symlink(): raise DogfoodError(f'ARCHIVE_UNSUPPORTED: symlink {name}; no files deleted')
        if p.is_dir(): continue
        if not p.is_file(): raise DogfoodError(f'ARCHIVE_UNSUPPORTED: non-regular entry {name}')
        entries[name] = proof(p)
    return entries


def ensure_jobs_settled(directory):
    for job in (directory / 'jobs').iterdir() if (directory / 'jobs').exists() else ():
        if not job.is_dir(): continue
        if (job / 'request.json').exists() and not (job / 'exit.json').exists():
            raise DogfoodError(f'RUN_UNSETTLED: {job.name} has no exit receipt; no cleanup')
        for name in ('process.json', 'native-process.json'):
            p = job / name
            if not p.exists(): continue
            pid = read(p).get('pid')
            if not isinstance(pid, int) or pid <= 0: raise DogfoodError('RUN_UNSETTLED: invalid process identity')
            try: os.kill(pid, 0)
            except ProcessLookupError: pass
            except PermissionError as exc: raise DogfoodError('RUN_UNSETTLED: process cannot be checked') from exc
            else: raise DogfoodError(f'RUN_UNSETTLED: PID {pid} still exists; no cleanup')


def checked_archive(path):
    """Read-only standalone verification, independent of the originating runtime."""
    path = Path(path).resolve()
    try:
        with zipfile.ZipFile(path) as z:
            names = z.namelist()
            if len(names) != len(set(names)): raise DogfoodError('ARCHIVE_INVALID: duplicate members')
            index = json.loads(z.read('archive-manifest.json'))
            if index.get('format') != 'formal-gates-archive-v1' or index['status']['status'] not in TERMINAL:
                raise DogfoodError('ARCHIVE_INVALID: wrong format or nonterminal run')
            if set(names) != {'archive-manifest.json', *('run/' + n for n in index['files'])}:
                raise DogfoodError('ARCHIVE_INVALID: missing or extra members')
            for name, expected in index['files'].items():
                p = PurePosixPath(name)
                if p.is_absolute() or '..' in p.parts or '\\' in name:
                    raise DogfoodError('ARCHIVE_INVALID: unsafe member path')
                with z.open('run/' + name) as f:
                    if stream_proof(f) != expected: raise DogfoodError(f'ARCHIVE_INVALID: content differs: {name}')
        return index
    except (zipfile.BadZipFile, KeyError, ValueError, OSError) as exc:
        raise DogfoodError(f'ARCHIVE_INVALID: {exc}') from exc


def archive_status(path):
    index = checked_archive(path)
    return {**index['status'], 'archived': True, 'archive': str(Path(path).resolve()),
            'archive_verified': True, 'source_directory': index['source_directory'],
            'file_count': len(index['files']), 'model_calls': 0}


def archived_status(directory):
    marker = read(directory / MARKER)
    if proof(marker['archive']) != marker['archive_proof']:
        raise DogfoodError('ARCHIVE_CHANGED: retained archive missing or altered')
    result = archive_status(marker['archive'])
    if (result['run_id'] != marker['run_id'] or result['revision'] != marker['revision'] or
        result['source_directory'] != str(directory)):
        raise DogfoodError('ARCHIVE_BINDING: tombstone does not match archive')
    return {**result, 'cleanup_complete': marker.get('cleanup_complete', False)}


def _terminal(store, package):
    # Deferred imports avoid cycles; do not call run_graph under its own lock.
    from .cli import read_checkpointer, status_projection
    from .graph import Flow
    if (store.directory / MARKER).exists(): return archived_status(store.directory)
    with read_checkpointer(store.directory / 'checkpoints.sqlite') as saver:
        status = status_projection(store, Flow(store, package).build(saver),
                                   {'configurable': {'thread_id': store.manifest['run_id']}})
    if status['status'] not in TERMINAL:
        raise DogfoodError('RUN_NOT_TERMINAL: active or paused run cannot be archived/pruned')
    ensure_jobs_settled(store.directory)
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
    if destination.is_relative_to(directory): raise DogfoodError('ARCHIVE_PATH: output must be outside the run')
    with run_lock(directory):
        if (directory / MARKER).exists(): raise DogfoodError('RUN_ARCHIVED: use the retained archive')
        status = _terminal(store, package)
        if destination.exists(): raise DogfoodError('ARCHIVE_EXISTS: use a new destination; not overwritten')
        original = inventory(directory)
        logical_db = db_proof(directory / 'checkpoints.sqlite')
        destination.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='.archive-', dir=destination.parent) as tmp:
            temp = Path(tmp); backup = temp / 'checkpoints.sqlite'
            with contextlib.closing(sqlite3.connect((directory / 'checkpoints.sqlite').as_uri() + '?mode=ro', uri=True)) as src:
                with contextlib.closing(sqlite3.connect(backup)) as dst: src.backup(dst)
            if db_proof(backup) != logical_db: raise DogfoodError('ARCHIVE_CHANGED: database changed while backing up')
            files = dict(original); files['checkpoints.sqlite'] = proof(backup)
            index = {'format': 'formal-gates-archive-v1', 'source_directory': str(directory),
                     'run_id': store.manifest['run_id'], 'revision': store.manifest['revision'],
                     'runtime_identity': store.manifest['runtime_identity'], 'status': status,
                     'files': files, 'database_logical_sha256': logical_db}
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
    current = inventory(directory); expected = index['files']
    if set(current) - set(expected): raise DogfoodError('ARCHIVE_STALE: new files appeared; no deletion')
    if not allow_missing and set(current) != set(expected): raise DogfoodError('ARCHIVE_STALE: source files missing')
    for name, value in current.items():
        if name == 'checkpoints.sqlite':
            equal = db_proof(directory / name) == index['database_logical_sha256']
        else: equal = value == expected[name]
        if not equal: raise DogfoodError(f'ARCHIVE_STALE: source differs: {name}')
    return current


def prune_run(directory, archive, confirm=False):
    from .cli import open_run
    package, store = open_run(directory); directory = store.directory
    # Only runs created by this installation, never an arbitrary project root.
    if directory.parent != (package / '.dogfood/runs').resolve() or directory.name != store.manifest['run_id']:
        raise DogfoodError('PRUNE_PATH: only this installation\'s run directories are eligible')
    archive = Path(archive).resolve()
    if archive.is_relative_to(directory): raise DogfoodError('ARCHIVE_PATH: archive must be outside the run')
    with run_lock(directory):
        marker_path = directory / MARKER
        marker = read(marker_path) if marker_path.exists() else None
        index = checked_archive(archive)
        if (index['source_directory'] != str(directory) or index['run_id'] != store.manifest['run_id'] or
            index['revision'] != store.manifest['revision']):
            raise DogfoodError('ARCHIVE_BINDING: wrong run/revision')
        status = _terminal(store, package)
        if marker and (marker['archive'] != str(archive) or marker['archive_proof'] != proof(archive)):
            raise DogfoodError('ARCHIVE_BINDING: use the original cleanup archive')
        current = _compare_live(directory, index, allow_missing=marker is not None)
        remove = sorted((n for n in current if n not in KEEP), key=lambda n: (-n.count('/'), n))
        report = {'status': 'PRUNE_PREVIEW', 'run_id': store.manifest['run_id'], 'terminal_status': status['status'],
                  'archive': str(archive), 'files_to_remove': len(remove),
                  'bytes_to_remove': sum(current[n]['size'] for n in remove), 'model_calls': 0}
        if not confirm: return report
        if marker is None:
            marker = {'format': 'formal-gates-prune-v1', 'run_id': store.manifest['run_id'],
                      'revision': store.manifest['revision'], 'archive': str(archive),
                      'archive_proof': proof(archive), 'cleanup_complete': False}
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
