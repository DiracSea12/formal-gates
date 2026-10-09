"""Verify archive, read-back, preview and pruning of a terminal run with 30,000 files.

Fact/chunk payloads keep requests within the bridge input ceiling. The real
workflow exercises the complete retention chain and prints elapsed time."""
from __future__ import annotations
import hashlib
import os
from pathlib import Path
import shutil
import time
import unittest
import uuid
from unittest.mock import patch
import test_driver as fixtures
from formal_gates_harness.cli import run_graph
from formal_gates_harness.infra import atomic, runtime_identity
from formal_gates_harness.retention import (archive_run, prune_run, archive_status,
                                            archived_status, checked_archive)

MANY = 30000


class RetentionScaleTests(unittest.TestCase):
    def setUp(self):
        fixtures.DriverTests.setUp(self)
        self.run_id = 'retention-scale-' + uuid.uuid4().hex
        old = self.directory
        self.directory = fixtures.PACKAGE / '.harness/runs' / self.run_id
        shutil.copytree(old, self.directory)
        self.addCleanup(shutil.rmtree, self.directory, True)
        policy = (fixtures.PACKAGE / 'prompts/reviewer-base.md').read_bytes()
        (self.directory / 'reviewer-base.md').write_bytes(policy)
        self.manifest.update(run_id=self.run_id,
                             runtime_identity=runtime_identity(fixtures.PACKAGE, str(fixtures.BINARY)),
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
        return run_graph(self.directory, {'decision': 'abort', 'revision': first['revision']},
                         first['interrupts'][0]['id'])

    def test_thirty_thousand_files_full_retention_chain(self):
        state = self.stopped()
        # The reviewer's reproduction: a dependency-shaped tree of small files
        # under the development workspace, exactly the "normal use" load.
        root = self.directory / 'workspaces/development/node_modules/example-package/dist'
        root.mkdir(parents=True)
        for i in range(MANY):
            (root / f'module-{i:05d}.js').write_bytes(b'export {};\n')
        started = time.monotonic()
        result = archive_run(self.directory, self.archive)
        index = checked_archive(self.archive)
        shown = archive_status(self.archive)
        preview = prune_run(self.directory, self.archive)
        report = prune_run(self.directory, self.archive, confirm=True)
        after = archived_status(self.directory)
        elapsed = time.monotonic() - started

        self.assertEqual(result['status'], 'ARCHIVED')
        self.assertEqual(result['file_count'], 30006)
        self.assertEqual(len(index['files']), 30006)
        self.assertTrue(any(n.startswith('workspaces/development/node_modules/') for n in index['files']))
        self.assertEqual(shown['status'], state['status'])
        self.assertTrue(shown['archived'])
        self.assertEqual(shown['file_count'], 30006)
        self.assertEqual(preview['status'], 'PRUNE_PREVIEW')
        self.assertGreater(preview['files_to_remove'], 0)
        self.assertEqual(preview['files_to_remove'], report['removed_files'])
        self.assertEqual(report['status'], 'PRUNED')
        self.assertGreater(report['removed_bytes'], 0)
        self.assertEqual(after['status'], state['status'])
        self.assertTrue(after['cleanup_complete'])
        self.assertEqual(after['file_count'], 30006)
        # The pruned source keeps only the tombstone set; the tree is gone.
        self.assertFalse((self.directory / 'checkpoints.sqlite').exists())
        self.assertFalse((self.directory / 'workspaces').exists())
        self.assertTrue((self.directory / 'manifest.json').exists())
        print(f'\n30k-file retention chain (archive+verify+status+preview+prune+readback): {elapsed:.1f}s')
        self.assertLess(elapsed, 240)  # generous tripwire; the target is 90s


if __name__ == '__main__':
    unittest.main()
