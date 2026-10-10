"""Current question/runtime identities are program-owned; confirmation retains CAS."""
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from formal_gates_harness.cli import prepare_answer, prepare_adoption, require_decision_run
from formal_gates_harness.infra import HarnessError, atomic

class DecisionAssemblyTests(unittest.TestCase):
    def test_answer_uses_exact_current_question_and_revision(self):
        state={'interrupts':[{'id':'opaque-question','request':{'revision':'opaque-revision','choices':['approve','abort']}}]}
        with patch('formal_gates_harness.cli.run_graph',return_value=state) as current:
            candidate=prepare_answer('run','approve')
        self.assertEqual(candidate,{'directory':str(Path('run').resolve()),'interrupt':'opaque-question','answer':{'decision':'approve','revision':'opaque-revision'}})
        current.assert_called_once_with('run',advance=False)
        with patch('formal_gates_harness.cli.run_graph',return_value=state):
            with self.assertRaises(HarnessError):prepare_answer('run','not-offered')
            with self.assertRaises(HarnessError):prepare_answer('run','approve',2)

    def test_adoption_reads_manifest_and_current_runtime(self):
        with tempfile.TemporaryDirectory() as home:
            directory=Path(home);atomic(directory/'manifest.json',{'runtime_identity':'original-runtime'})
            with patch('formal_gates_harness.cli.package_root',return_value=directory), patch('formal_gates_harness.cli.binary_path',return_value='binary'), patch('formal_gates_harness.cli.runtime_identity',return_value='target-runtime'), patch('formal_gates_harness.cli.adopt_transport',return_value={'from_runtime':'original-runtime','to_runtime':'target-runtime','snapshot':str((directory/'snapshot').resolve())}) as preview:
                candidate=prepare_adoption(directory,directory/'snapshot','Reviewed actual transport changes')
            self.assertEqual(candidate['from_runtime'],'original-runtime')
            self.assertEqual(candidate['to_runtime'],'target-runtime')
            self.assertEqual(candidate['snapshot'],str((directory/'snapshot').resolve()))

    def test_saved_candidate_cannot_be_applied_to_another_run(self):
        with self.assertRaises(HarnessError):require_decision_run('other-run',{'directory':str(Path('original-run').resolve())})
