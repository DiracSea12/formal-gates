"""Real terminal archive/prune tests; only NEW disposable runs are removed."""
from __future__ import annotations
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3
import tempfile
import unittest
import uuid
import zipfile
from unittest.mock import patch
import test_driver as fixtures
from formal_gates_harness.cli import run_graph
from formal_gates_harness.infra import atomic, read, run_lock, runtime_identity, Store, HarnessError
from formal_gates_harness.retention import archive_run, prune_run, archive_status, checked_archive, proof, db_proof


class RetentionTests(unittest.TestCase):
    def setUp(self):
        fixtures.DriverTests.setUp(self)
        self.run_id = 'retention-test-' + uuid.uuid4().hex
        old = self.directory
        self.directory = fixtures.PACKAGE / '.harness/runs' / self.run_id
        shutil.copytree(old, self.directory)
        self.addCleanup(shutil.rmtree, self.directory, True)
        policy = (fixtures.PACKAGE / 'prompts/reviewer-base.md').read_bytes()
        (self.directory / 'reviewer-base.md').write_bytes(policy)
        self.manifest.update(run_id=self.run_id, runtime_identity=runtime_identity(fixtures.PACKAGE, str(fixtures.BINARY)),
                             reviewer_policy_digest=hashlib.sha256(policy).hexdigest())
        atomic(self.directory / 'manifest.json', self.manifest)
        self.archive = self.root / 'archives' / 'run.zip'
        env = patch.dict(os.environ, FORMAL_GATES_HARNESS_ROOT=str(fixtures.PACKAGE),
                         FORMAL_GATES_HARNESS_BINARY=str(fixtures.BINARY),
                         PYTHONPATH=str(fixtures.PACKAGE / 'runtime/langgraph'))
        env.start(); self.addCleanup(env.stop)

    def tearDown(self): fixtures.DriverTests.tearDown(self)

    def stopped(self):
        first = run_graph(self.directory)
        return run_graph(self.directory, {'decision': 'abort', 'revision': first['revision']}, first['interrupts'][0]['id'])

    def completed(self):
        state = run_graph(self.directory)
        for _ in range(2):
            state = run_graph(self.directory, {'decision': 'approve', 'revision': state['revision']}, state['interrupts'][0]['id'])
        self.assertEqual(state['status'], 'DEMO_COMPLETE')
        return state

    def test_complete_run_archive_prune_and_standalone_inspection(self):
        state = self.completed()
        source_manifest = (self.directory / 'manifest.json').read_bytes()
        result = archive_run(self.directory, self.archive)
        self.assertFalse(result['source_deleted'])
        self.assertTrue((self.directory / 'checkpoints.sqlite').exists())
        index = checked_archive(self.archive)
        self.assertIn('checkpoints.sqlite', index['files'])
        self.assertTrue(any(n.startswith('jobs/') for n in index['files']))
        self.assertTrue(any(n.startswith('artifacts/') for n in index['files']))
        self.assertTrue(any(n.startswith('workspaces/development/') for n in index['files']))
        with zipfile.ZipFile(self.archive) as z:
            backup = self.root / 'offline.sqlite'; backup.write_bytes(z.read('run/checkpoints.sqlite'))
        self.assertEqual(db_proof(backup), db_proof(self.directory / 'checkpoints.sqlite'))
        preview = prune_run(self.directory, self.archive)
        self.assertEqual(preview['status'], 'PRUNE_PREVIEW')
        self.assertFalse((self.directory / 'archive.json').exists())
        report = prune_run(self.directory, self.archive, True)
        self.assertEqual(report['status'], 'PRUNED')
        self.assertGreater(report['removed_bytes'], 0)
        for name in ('jobs', 'artifacts', 'checkpoints.sqlite', 'workspaces'):
            self.assertFalse((self.directory / name).exists(), name)
        self.assertEqual((self.directory / 'manifest.json').read_bytes(), source_manifest)
        for advance in (False, True):
            shown = run_graph(self.directory, advance=advance)
            self.assertEqual(shown['status'], state['status']); self.assertTrue(shown['archived'])
            self.assertEqual(shown['candidate'], state['candidate'])
        self.assertFalse((self.directory / 'checkpoints.sqlite').exists())
        with self.assertRaises(HarnessError):
            run_graph(self.directory, {'decision': 'approve', 'revision': state['revision']}, 'old-interrupt')
        again = prune_run(self.directory, self.archive, True)
        self.assertEqual(again['removed_files'], 0)
        self.assertEqual(archive_status(self.archive)['status'], state['status'])

    def test_pending_run_and_busy_run_cannot_archive(self):
        run_graph(self.directory)
        with self.assertRaisesRegex(HarnessError, 'RUN_NOT_TERMINAL'):
            archive_run(self.directory, self.archive)
        self.assertFalse(self.archive.exists())
        with run_lock(self.directory):
            with self.assertRaisesRegex(HarnessError, 'RUN_BUSY'): archive_run(self.directory, self.archive)

    def test_archive_does_not_overwrite_output(self):
        self.stopped(); self.archive.parent.mkdir(); self.archive.write_bytes(b'owned existing file')
        with self.assertRaisesRegex(HarnessError, 'ARCHIVE_EXISTS'): archive_run(self.directory, self.archive)
        self.assertEqual(self.archive.read_bytes(), b'owned existing file')

    def test_archive_cannot_be_inside_source_run(self):
        self.stopped()
        with self.assertRaisesRegex(HarnessError, 'ARCHIVE_PATH'):
            archive_run(self.directory, self.directory / 'archive.zip')

    def test_bad_archive_never_prunes(self):
        self.stopped(); archive_run(self.directory, self.archive)
        self.archive.write_bytes(b'broken archive')
        with self.assertRaisesRegex(HarnessError, 'ARCHIVE_INVALID'):
            prune_run(self.directory, self.archive, True)
        self.assertTrue((self.directory / 'checkpoints.sqlite').exists())
        self.assertFalse((self.directory / 'archive.json').exists())

    def test_changed_source_after_archive_refuses_prune(self):
        self.stopped(); archive_run(self.directory, self.archive)
        (self.directory / 'new-important-note.txt').write_text('must retain')
        with self.assertRaisesRegex(HarnessError, 'ARCHIVE_STALE'):
            prune_run(self.directory, self.archive, True)
        self.assertTrue((self.directory / 'checkpoints.sqlite').exists())
        self.assertEqual((self.directory / 'new-important-note.txt').read_text(), 'must retain')

    def test_lost_or_live_job_prevents_archive(self):
        self.stopped(); job = self.directory / 'jobs' / 'unresolved'; job.mkdir(parents=True)
        atomic(job / 'request.json', {'kind': 'test'})
        with self.assertRaisesRegex(HarnessError, 'RUN_UNSETTLED'):
            archive_run(self.directory, self.archive)
        atomic(job / 'exit.json', {'exit_code': 0})
        atomic(job / 'native-process.json', {'pid': os.getpid()})
        with self.assertRaisesRegex(HarnessError, 'RUN_UNSETTLED'):
            archive_run(self.directory, self.archive)
        self.assertFalse(self.archive.exists())

    def test_symlink_is_not_followed_or_deleted(self):
        self.stopped(); external = self.root / 'must-keep'; external.write_text('sentinel')
        (self.directory / 'outside').symlink_to(external)
        with self.assertRaisesRegex(HarnessError, 'ARCHIVE_UNSUPPORTED'):
            archive_run(self.directory, self.archive)
        self.assertEqual(external.read_text(), 'sentinel')

    def test_interrupted_prune_retries_from_verified_archive(self):
        self.stopped()
        (self.directory / 'extra').mkdir(); (self.directory / 'extra/a').write_text('A')
        (self.directory / 'extra/b').write_text('B')
        archive_run(self.directory, self.archive)
        original = Path.unlink
        def failing(path, *args, **kwargs):
            if path == self.directory / 'extra/b': raise OSError('injected deletion failure')
            return original(path, *args, **kwargs)
        with patch.object(Path, 'unlink', failing):
            with self.assertRaises(OSError): prune_run(self.directory, self.archive, True)
        marker = read(self.directory / 'archive.json')
        self.assertFalse(marker['cleanup_complete'])
        self.assertEqual(run_graph(self.directory, advance=False)['status'], 'ABORTED')
        final = prune_run(self.directory, self.archive, True)
        self.assertEqual(final['status'], 'PRUNED')
        self.assertTrue(read(self.directory / 'archive.json')['cleanup_complete'])
        self.assertFalse((self.directory / 'extra').exists())

    def test_edited_archive_after_prune_fails_read(self):
        self.stopped(); archive_run(self.directory, self.archive); prune_run(self.directory, self.archive, True)
        self.archive.write_bytes(b'not the archived evidence')
        with self.assertRaisesRegex(HarnessError, 'ARCHIVE_CHANGED'):
            run_graph(self.directory, advance=False)
        self.assertFalse((self.directory / 'checkpoints.sqlite').exists())


if __name__ == '__main__': unittest.main()
