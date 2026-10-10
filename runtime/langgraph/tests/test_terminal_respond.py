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
from formal_gates_harness.cli import run_graph
from formal_gates_harness.infra import atomic


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
            [str(fixtures.BINARY), 'harness', command, '--run', str(self.directory), *extra],
            env=os.environ.copy(), text=True, capture_output=True)

    def reply(self, state, decision):
        return run_graph(self.directory, {'decision': decision, 'revision': state['revision']},
                         state['interrupts'][0]['id'])

    def assert_terminal_replies_rejected(self, state, consumed):
        before_files, before_rows = self.files(), self.rows()
        # Valid-looking, stale, and malformed-in-context replies must all report
        # rejection, rather than silently returning a successful state projection.
        for decision in ('approve', 'abort', 'restart-session'):
            with self.subTest(decision=decision, terminal=state['status']):
                candidate = self.root / 'answer.json'
                atomic(candidate, {'directory':str(self.directory.resolve()),'interrupt': consumed['interrupts'][0]['id'], 'answer': {'decision': decision, 'revision': state['revision']}})
                p = self.public('respond', '--decision-file', str(candidate))
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

    def test_saved_decision_confirms_original_question_without_rebinding(self):
        first=run_graph(self.directory)
        candidate=self.root/'prepared-answer.json'
        prepared=self.public('respond','--decision','approve','--save-decision',str(candidate))
        self.assertEqual(prepared.returncode,0,prepared.stderr)
        body=json.loads(candidate.read_text())
        self.assertEqual(body['directory'],str(self.directory.resolve()))
        self.assertEqual(body['interrupt'],first['interrupts'][0]['id'])
        self.assertEqual(body['answer'],{'decision':'approve','revision':first['revision']})
        accepted=self.public('respond','--decision-file',str(candidate))
        self.assertEqual(accepted.returncode,0,accepted.stderr)
        self.assertEqual(json.loads(accepted.stdout)['interrupts'][0]['request']['kind'],'plan')
        before_files,before_rows=self.files(),self.rows()
        repeated=self.public('respond','--decision-file',str(candidate))
        self.assertEqual(repeated.returncode,2,repeated.stdout+repeated.stderr)
        self.assertIn('current interrupt id',json.loads(repeated.stderr)['error'])
        self.assertEqual(before_rows,self.rows())
        self.assertEqual(before_files,self.files())

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
