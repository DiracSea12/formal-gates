"""Public CLI regression: a reply to a finished run must not look accepted.
Real local checkpoints, Git and commands; workers are deterministic fixtures.
"""
from __future__ import annotations
import json
import os
import subprocess
import unittest
import test_driver as fixtures
import test_checkpoint_startup as startup
from formal_gates_dogfood.cli import run_graph


class TerminalRespondTests(unittest.TestCase):
    def setUp(self):
        startup.CheckpointStartupTests.setUp(self)

    def tearDown(self):
        startup.CheckpointStartupTests.tearDown(self)

    def rows(self):
        return startup.CheckpointStartupTests.checkpoint_rows(self, self.directory)

    def files(self):
        return startup.CheckpointStartupTests.files(self, self.directory, allow_sqlite_coordination=True)

    def public(self, command, *extra):
        return subprocess.run(
            [str(fixtures.BINARY), 'dogfood', command, '--run', str(self.directory), *extra],
            env=os.environ.copy(), text=True, capture_output=True, timeout=60)

    def reply(self, state, decision):
        return run_graph(self.directory, {'decision': decision, 'revision': state['revision']},
                         state['interrupts'][0]['id'])

    def assert_terminal_replies_rejected(self, state, consumed):
        before_files, before_rows = self.files(), self.rows()
        # Valid-looking, stale, and malformed-in-context replies must all report
        # rejection, rather than silently returning a successful state projection.
        for decision in ('approve', 'abort', 'restart-session'):
            with self.subTest(decision=decision, terminal=state['status']):
                p = self.public('respond', '--decision', decision,
                                '--revision', state['revision'],
                                '--interrupt', consumed['interrupts'][0]['id'])
                self.assertEqual(p.returncode, 2, p.stdout + p.stderr)
                self.assertEqual(p.stdout, '')
                error = json.loads(p.stderr)
                self.assertFalse(error['ok'])
                self.assertIn('RUN_TERMINAL', error['error'])
                self.assertNotIn('Traceback', p.stderr)
        for command in ('show', 'resume'):
            p = self.public(command)
            self.assertEqual(p.returncode, 0, p.stderr)
            self.assertEqual(json.loads(p.stdout)['status'], state['status'])
        self.assertEqual(before_rows, self.rows())
        self.assertEqual(before_files, self.files())

    def test_completed_run_rejects_respond_but_show_resume_stay_idempotent(self):
        first = run_graph(self.directory)
        plan = self.reply(first, 'approve')
        final = self.reply(plan, 'approve')
        self.assertEqual(final['status'], 'DEMO_COMPLETE')
        self.assert_terminal_replies_rejected(final, plan)

    def test_aborted_run_rejects_respond_but_show_resume_stay_idempotent(self):
        first = run_graph(self.directory)
        final = self.reply(first, 'abort')
        self.assertEqual(final['status'], 'ABORTED')
        self.assert_terminal_replies_rejected(final, first)


if __name__ == '__main__':
    unittest.main()
