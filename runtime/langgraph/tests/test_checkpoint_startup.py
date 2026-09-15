"""ZCode F1 regression: inspect interrupted startup without creating state.

Uses the public Go/Python entrypoint and real SQLite/Git/processes. No models,
no historical run changes, and no new checkpoint-integrity policy.
"""
from __future__ import annotations
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3
import subprocess
import sys
import unittest
import uuid
from unittest.mock import patch
import test_driver as fixtures
from formal_gates_dogfood.infra import atomic, runtime_identity


class CheckpointStartupTests(unittest.TestCase):
    def setUp(self):
        fixtures.DriverTests.setUp(self)
        policy = (fixtures.PACKAGE / 'prompts/reviewer-base.md').read_bytes()
        (self.directory / 'reviewer-base.md').write_bytes(policy)
        self.manifest.update(
            runtime_identity=runtime_identity(fixtures.PACKAGE, str(fixtures.BINARY)),
            reviewer_policy_digest=hashlib.sha256(policy).hexdigest())
        atomic(self.directory / 'manifest.json', self.manifest)
        env = patch.dict(os.environ, {
            'FORMAL_GATES_DOGFOOD_ROOT': str(fixtures.PACKAGE),
            'FORMAL_GATES_DOGFOOD_BINARY': str(fixtures.BINARY),
            'PYTHONPATH': str(fixtures.PACKAGE / 'runtime/langgraph')})
        env.start()
        self.addCleanup(env.stop)

    def tearDown(self):
        fixtures.DriverTests.tearDown(self)

    def public(self, command, directory):
        return subprocess.run(
            [str(fixtures.BINARY), 'dogfood', command, '--run', str(directory)],
            env=os.environ.copy(), capture_output=True, text=True, timeout=60)

    def files(self, directory, allow_sqlite_coordination=False):
        # WAL-mode readers may create -wal/-shm coordination files even with
        # mode=ro (SQLite's documented behavior). Compare logical rows too;
        # never use immutable=1, which would hide a concurrent writer's WAL.
        ignored = {'checkpoints.sqlite-wal', 'checkpoints.sqlite-shm'} if allow_sqlite_coordination else set()
        return {str(p.relative_to(directory)): hashlib.sha256(p.read_bytes()).hexdigest()
                for p in directory.rglob('*') if p.is_file()
                and str(p.relative_to(directory)) not in ignored}

    def checkpoint_rows(self, directory):
        connection = sqlite3.connect((directory / 'checkpoints.sqlite').as_uri() + '?mode=ro', uri=True)
        try:
            return {table: connection.execute(f'SELECT * FROM {table} ORDER BY rowid').fetchall()
                    for table in ('checkpoints', 'writes')}
        finally:
            connection.close()

    def assert_read_error(self, directory, code):
        before = self.files(directory)
        result = self.public('show', directory)
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertEqual(result.stdout, '')
        self.assertNotIn('Traceback', result.stderr)
        error = json.loads(result.stderr)
        self.assertFalse(error['ok'])
        self.assertIn(code, error['error'])
        self.assertIn('resume', error['error'])
        self.assertEqual(before, self.files(directory), 'show changed the run')

    def assert_resume_requires_approval(self, directory):
        result = self.public('resume', directory)
        self.assertEqual(result.returncode, 0, result.stderr)
        state = json.loads(result.stdout)
        self.assertEqual(state['status'], 'WAITING_USER')
        self.assertEqual(state['interrupts'][0]['request']['kind'], 'requirement')
        self.assertFalse(list((directory / 'workers').glob('*.json')))
        self.assertFalse(list((directory / 'jobs').glob('*')))
        self.assertFalse((directory / 'dogfood-result.json').exists())
        rows_before = self.checkpoint_rows(directory)
        before = self.files(directory, allow_sqlite_coordination=True)
        shown = self.public('show', directory)
        self.assertEqual(shown.returncode, 0, shown.stderr)
        self.assertEqual(json.loads(shown.stdout)['interrupts'], state['interrupts'])
        self.assertEqual(rows_before, self.checkpoint_rows(directory), 'show changed checkpoint rows')
        self.assertEqual(before, self.files(directory, allow_sqlite_coordination=True),
                         'show changed the database or application files')

    def test_public_show_missing_checkpoint_does_not_create_state(self):
        self.assertFalse((self.directory / 'checkpoints.sqlite').exists())
        self.assert_read_error(self.directory, 'CHECKPOINT_READ_ERROR')
        self.assertFalse((self.directory / 'checkpoints.sqlite').exists())
        self.assert_resume_requires_approval(self.directory)

    def test_created_database_without_schema_is_read_only_and_recoverable(self):
        # A real SQLite file, interrupted before SqliteSaver.setup creates tables.
        with sqlite3.connect(self.directory / 'checkpoints.sqlite') as connection:
            connection.execute('PRAGMA user_version=1')
        self.assert_read_error(self.directory, 'CHECKPOINT_NOT_INITIALIZED')
        self.assert_resume_requires_approval(self.directory)

    def test_process_exit_after_real_make_run_before_first_checkpoint(self):
        run_id = 'test-startup-gap-' + uuid.uuid4().hex
        directory = fixtures.PACKAGE / '.dogfood/runs' / run_id
        self.assertFalse(directory.exists())
        self.addCleanup(shutil.rmtree, directory, True)
        parameters = {
            'project': str(self.project),
            'requirement': str(self.directory / 'requirement.md'),
            'checks': self.manifest['spec']['checks'],
            'gates': self.manifest['spec']['gates'],
            'run_id': run_id}
        code = '''import json, os, sys
from pathlib import Path
from formal_gates_dogfood.cli import make_run, package_root
p = json.loads(sys.argv[1])
run = make_run(package_root(), Path(p['project']), Path(p['requirement']),
               p['checks'], p['gates'], provider='fixture', fixture=True,
               run_id=p['run_id'])
print(json.dumps({'run': str(run)}), flush=True)
os._exit(73)
'''
        created = subprocess.run([sys.executable, '-c', code, json.dumps(parameters)],
                                 env=os.environ.copy(), capture_output=True,
                                 text=True, timeout=60)
        self.assertEqual(created.returncode, 73, created.stderr)
        self.assertEqual(Path(json.loads(created.stdout)['run']), directory)
        self.assertTrue((directory / 'manifest.json').exists())
        self.assertFalse((directory / 'checkpoints.sqlite').exists())
        self.assert_read_error(directory, 'CHECKPOINT_READ_ERROR')
        self.assert_resume_requires_approval(directory)


if __name__ == '__main__':
    unittest.main()
