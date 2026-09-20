"""Explicit terminal-run retention: verified archive, preview, then local pruning.

No background deletion and no pruning of a running/paused/unknown invocation.
An archive is evidence, not another scheduler. A small tombstone retains status
while the full source, checkpoints (SQLite backup), jobs and artifacts live in ZIP.

The retention *rules* live in `internal/harness` (G3b): member admission,
destination/settlement/skeleton/verify/stale/projection judgements and the prune
admission/bind/marker/plan rules. This module keeps the filesystem facts and
effects — locking, hashing, the SQLite backup, zip reading and writing, process
probing (os.kill) and the deletions themselves — and raises the HarnessError
family directly from the refusal a judgement returns.

Inventories and proofs never cross the bridge whole: they travel in batches of
CHUNK entries, or as small facts (bools, counts, strings), so a terminal run
with tens of thousands of workspace files stays far inside the bridge's fixed
request ceiling. The one large payload that remains is canonical(index) when
the manifest is written — a single copy of the index, the capacity envelope
the format has always had. Order is carried by arrays: batches follow the
walk, verify entries follow the manifest's sorted files, and the first
refusing batch decides.
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
CHUNK = 2000  # must equal retentionChunk in internal/harness/retention.go


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
    # owned by archive-inventory and runs over the walked order, CHUNK entries
    # per call. The global order is the batch order: a refusing batch raises
    # through _decide before any later batch is judged or hashed.
    walked, paths = [], {}
    for p in sorted(directory.rglob('*')):
        name = p.relative_to(directory).as_posix()
        kind = ('symlink' if p.is_symlink() else 'dir' if p.is_dir()
                else 'file' if p.is_file() else 'other')
        walked.append({'name': name, 'kind': kind}); paths[name] = p
    entries = {}
    for start in range(0, len(walked), CHUNK):
        answered = _decide(driver_bridge().call('archive-inventory',
                                                {'entries': walked[start:start + CHUNK]}))
        for name in answered['files']:
            entries[name] = proof(paths[name])
    return entries


def _probe(pid):
    # The liveness probe (os.kill) stays here. A pid that is not a positive
    # int is never probed: archive-settled refuses it first (pid_valid), so
    # _probe only needs a dead verdict to fill the fact. A JSON true is not
    # an integer pid — the owner's ruling, not mechanical CPython emulation.
    if not isinstance(pid, int) or isinstance(pid, bool) or pid <= 0: return 'dead'
    try: os.kill(pid, 0)
    except ProcessLookupError: return 'dead'
    except PermissionError: return 'permission'
    return 'alive'


def _jobs_facts(directory):
    # Job-tree projection for archive-settled: iterdir order, the request/exit
    # receipts and each recorded pid with its probe outcome. pid travels as a
    # plain int (null when the record held no integer) plus the pid_valid
    # fact, so the typed payload never carries a heterogeneous identity.
    jobs = []
    root = directory / 'jobs'
    for job in root.iterdir() if root.exists() else ():
        if not job.is_dir(): continue
        processes = []
        for name in ('process.json', 'native-process.json'):
            record = job / name
            if not record.exists(): continue
            pid = read(record).get('pid')
            integral = isinstance(pid, int) and not isinstance(pid, bool)
            processes.append({'pid': pid if integral else None,
                              'pid_valid': integral and pid > 0, 'probe': _probe(pid)})
        jobs.append({'name': job.name, 'has_request': (job / 'request.json').exists(),
                     'has_exit': (job / 'exit.json').exists(), 'processes': processes})
    return jobs


def checked_archive(path):
    """Read-only standalone verification, independent of the originating runtime."""
    path = Path(path).resolve()
    try:
        with zipfile.ZipFile(path) as z:
            names = z.namelist()
            index = json.loads(z.read('archive-manifest.json'))
            # Natural accesses keep the wrapped KeyError for a corrupt
            # manifest; a non-string format/status normalizes to None so the
            # typed facts stay typed and the refusal is still Go's verdict.
            status_name = index['status']['status']; files = index['files']
            raw_format = index.get('format')
            facts = {'duplicates': len(names) != len(set(names)),
                     'format': raw_format if isinstance(raw_format, str) else None,
                     'status_name': status_name if isinstance(status_name, str) else None,
                     'names_match': set(names) == {'archive-manifest.json'} | {'run/' + n for n in files}}
            # Facts first, one small call (an empty-files archive is judged
            # here too); then the member proofs in CHUNK-sized batches, each
            # carrying the same facts so the verdict order stays: duplicates,
            # format/terminal status, member set, then per-member safety and
            # content in manifest (sorted) order. A listed member that is not
            # a zip member already fails names_match, so every batch member
            # opens; reading a member with an unsafe name is harmless because
            # no extraction ever happens here.
            _decide(driver_bridge().call('archive-verify', {**facts, 'entries': []}))
            ordered = sorted(files)
            for start in range(0, len(ordered), CHUNK):
                entries = []
                for name in ordered[start:start + CHUNK]:
                    with z.open('run/' + name) as f:
                        entries.append({'name': name, 'actual': stream_proof(f),
                                        'expected': files[name]})
                _decide(driver_bridge().call('archive-verify', {**facts, 'entries': entries}))
        return index
    except (zipfile.BadZipFile, KeyError, ValueError, OSError) as exc:
        raise HarnessError(f'ARCHIVE_INVALID: {exc}') from exc


def archive_status(path):
    index = checked_archive(path)
    # The payload's natural key accesses keep today's raw KeyError order for
    # a corrupt manifest: status, then source_directory, then files.
    answered = _decide(driver_bridge().call('archive-projection', {
        'status': index['status'], 'source_directory': index['source_directory'],
        'file_count': len(index['files']), 'archive': str(Path(path).resolve()),
        'archive_proof': None, 'marker': None, 'directory': ''}))
    return answered['status']


def archived_status(directory):
    marker = read(directory / MARKER)
    archive = marker['archive']
    archive_proof = proof(archive)      # today compares this before opening the zip
    marker_proof = marker['archive_proof']  # today's right-hand key access, same order
    # Pre-check phase (no status yet): ARCHIVE_CHANGED fires before any zip
    # work, exactly where the pre-migration comparison sat.
    _decide(driver_bridge().call('archive-projection', {
        'archive': str(Path(archive).resolve()), 'archive_proof': archive_proof,
        'marker': {'archive_proof': marker_proof}}))
    index = checked_archive(archive)
    # The marker projection below uses natural key accesses, so a corrupt
    # tombstone keeps today's raw KeyError; the index keys above keep theirs
    # through the payload construction (status, source_directory, files), and
    # cleanup_complete keeps the marker.get default.
    answered = _decide(driver_bridge().call('archive-projection', {
        'status': index['status'], 'source_directory': index['source_directory'],
        'file_count': len(index['files']), 'archive': str(Path(archive).resolve()),
        'archive_proof': archive_proof, 'directory': str(directory),
        'marker': {'run_id': marker['run_id'], 'revision': marker['revision'],
                   'cleanup_complete': marker.get('cleanup_complete', False)}}))
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
        # Refuse an existing destination before any inventory, hashing or
        # backup work — the pre-migration position of this guard.
        if destination.exists(): raise HarnessError('ARCHIVE_EXISTS: use a new destination; not overwritten')
        original = inventory(directory)
        logical_db = db_proof(directory / 'checkpoints.sqlite')
        destination.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='.archive-', dir=destination.parent) as tmp:
            temp = Path(tmp); backup = temp / 'checkpoints.sqlite'
            with contextlib.closing(sqlite3.connect((directory / 'checkpoints.sqlite').as_uri() + '?mode=ro', uri=True)) as src:
                with contextlib.closing(sqlite3.connect(backup)) as dst: src.backup(dst)
            if db_proof(backup) != logical_db: raise HarnessError('ARCHIVE_CHANGED: database changed while backing up')
            files = dict(original); files['checkpoints.sqlite'] = proof(backup)
            # The skeleton (every small index fact) is assembled by Go; the
            # files proofs join it here and cross the bridge exactly once
            # more, inside canonical(index) when the manifest is written.
            skeleton = _decide(driver_bridge().call('archive-index-plan', {
                'source_directory': str(directory), 'run_id': store.manifest['run_id'],
                'revision': store.manifest['revision'], 'runtime_identity': store.manifest['runtime_identity'],
                'status': status, 'database_logical_sha256': logical_db}))['skeleton']
            index = {**skeleton, 'files': files}
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
    # Set drift travels as two small facts; the live logical database hash is
    # a fact only the sqlite reader here can produce.
    _decide(driver_bridge().call('archive-stale', {
        'has_new': bool(set(current) - set(expected)),
        'has_missing': bool(set(expected) - set(current)), 'allow_missing': allow_missing}))
    live = db_proof(directory / 'checkpoints.sqlite') if 'checkpoints.sqlite' in current else None
    logical = index['database_logical_sha256']
    entries = []
    for name, value in current.items():
        if name == 'checkpoints.sqlite':
            entries.append({'name': name, 'kind': 'database', 'actual_db': live,
                            'expected_db': logical if isinstance(logical, str) else None})
        else:
            entries.append({'name': name, 'kind': 'file',
                            'actual_proof': value, 'expected_proof': expected[name]})
    for start in range(0, len(entries), CHUNK):
        _decide(driver_bridge().call('archive-stale', {'entries': entries[start:start + CHUNK]}))
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
        # A non-string index value normalizes to None: a foreign manifest can
        # never match this run's strings, and the payload stays typed. The
        # accesses themselves stay natural (raw KeyError for a corrupt index).
        source_directory = index['source_directory']; index_run_id = index['run_id']
        index_revision = index['revision']
        _decide(driver_bridge().call('prune-bind', {
            'directory': str(directory), 'run_id': store.manifest['run_id'],
            'revision': store.manifest['revision'],
            'source_directory': source_directory if isinstance(source_directory, str) else None,
            'index_run_id': index_run_id if isinstance(index_run_id, str) else None,
            'index_revision': index_revision if isinstance(index_revision, str) else None}))
        status = _terminal(store, package)
        if marker:  # today's truthiness gate; natural key order preserved
            marker_archive = marker['archive']; marker_archive_proof = marker['archive_proof']
            _decide(driver_bridge().call('prune-marker', {
                'archive': str(archive), 'archive_proof': proof(archive),
                'marker_archive': marker_archive, 'marker_archive_proof': marker_archive_proof}))
        current = _compare_live(directory, index, allow_missing=marker is not None)
        # names+sizes travel once, in current order: one ~80-byte row per file
        # keeps a 30k-file run around 2.4 MB, inside the request ceiling, and
        # no proof rides along.
        answered = _decide(driver_bridge().call('prune-plan', {
            'entries': [{'name': name, 'size': value['size']} for name, value in current.items()],
            'run_id': store.manifest['run_id'], 'revision': store.manifest['revision'],
            'terminal_status': status['status'], 'archive': str(archive),
            'marker_exists': marker is not None}))
        report, remove = answered['report'], answered['remove']
        if not confirm: return report
        if marker is None:
            marker = {**answered['marker'], 'archive_proof': proof(archive)}
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
