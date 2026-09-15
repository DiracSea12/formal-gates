"""Review inputs and approved checks stay bound to their exact Git candidate."""
from pathlib import Path
import json
import os
import sys
import tempfile
import unittest
from unittest.mock import patch

from formal_gates_harness.feedback import materialize_comparison
from formal_gates_harness.graph import Flow
from formal_gates_harness.infra import RuntimeBlocked, git, read
import test_driver as fixtures


class ComparisonInputs(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        git(self.root, 'init', '--quiet')
        for name, body in {'edited.txt': 'before\n', 'removed.txt': 'deleted content\n',
                           'rename.txt': 'rename content\n'}.items():
            (self.root / name).write_text(body)
        (self.root / 'binary.bin').write_bytes(b'\x00old\xff')
        (self.root / 'link').symlink_to('edited.txt')
        self.commit('Baseline')
        self.base = git(self.root, 'rev-parse', 'HEAD')
        (self.root / 'edited.txt').write_text('after\n')
        (self.root / 'removed.txt').unlink()
        (self.root / 'rename.txt').rename(self.root / 'renamed.txt')
        (self.root / 'added.txt').write_text('new independent content\n')
        (self.root / 'binary.bin').write_bytes(b'\x00new\xfe')
        (self.root / 'link').unlink(); (self.root / 'link').symlink_to('added.txt')
        (self.root / 'module').mkdir()
        git(self.root, 'update-index', '--add', '--cacheinfo', '160000', self.base, 'module')
        self.commit('Candidate')
        self.candidate = git(self.root, 'rev-parse', 'HEAD')
        self.materials = self.root / '.formal-gates-input' / 'review'

    def commit(self, message):
        git(self.root, 'add', '.')
        git(self.root, '-c', 'user.name=Tests', '-c', 'user.email=tests@localhost',
            '-c', 'commit.gpgsign=false', 'commit', '--quiet', '-m', message)

    def test_complete_delta_includes_old_deleted_binary_and_renamed_content(self):
        result = materialize_comparison(self.root, self.materials, self.base, self.candidate)
        directory = Path(result['directory'])
        index = read(directory / 'comparison.json')
        self.assertEqual((index['base'], index['candidate']), (self.base, self.candidate))
        self.assertEqual((directory / 'base/edited.txt').read_text(), 'before\n')
        self.assertEqual((directory / 'candidate/edited.txt').read_text(), 'after\n')
        self.assertEqual((directory / 'base/removed.txt').read_text(), 'deleted content\n')
        self.assertEqual((directory / 'candidate/added.txt').read_text(), 'new independent content\n')
        self.assertEqual((directory / 'base/binary.bin').read_bytes(), b'\x00old\xff')
        self.assertEqual((directory / 'candidate/binary.bin').read_bytes(), b'\x00new\xfe')
        self.assertIn('GIT binary patch', (directory / 'full.diff').read_text())
        renamed = next(c for c in index['changes'] if c['status'].startswith('R'))
        self.assertEqual(renamed['base']['path'], 'rename.txt')
        self.assertEqual(renamed['candidate']['path'], 'renamed.txt')
        self.assertFalse((directory / 'base/link').is_symlink())
        self.assertEqual((directory / 'base/link').read_text(), 'edited.txt')
        module = next(c['candidate'] for c in index['changes'] if c['candidate'] and c['candidate']['path'] == 'module')
        self.assertEqual(module['mode'], '160000')
        self.assertIn('not included', module['note'])
        self.assertEqual(git(self.root, 'rev-parse', 'HEAD'), self.candidate)
        self.assertEqual(git(self.root, 'status', '--porcelain', '--untracked-files=no'), '')

    def test_retry_is_identical_and_refuses_modified_materials(self):
        first = materialize_comparison(self.root, self.materials, self.base, self.candidate)
        self.assertEqual(materialize_comparison(self.root, self.materials, self.base, self.candidate), first)
        (self.materials / 'comparison/base/removed.txt').write_text('tampered')
        with self.assertRaisesRegex(RuntimeBlocked, 'comparison material changed'):
            materialize_comparison(self.root, self.materials, self.base, self.candidate)

    def test_another_candidate_cannot_reuse_the_same_material_directory(self):
        materialize_comparison(self.root, self.materials, self.base, self.candidate)
        with self.assertRaisesRegex(RuntimeBlocked, 'comparison material changed'):
            materialize_comparison(self.root, self.materials, self.base, self.base)


class DirectApprovedChecks(unittest.TestCase):
    def setUp(self):
        fixtures.DriverTests.setUp(self)

    def tearDown(self):
        fixtures.DriverTests.tearDown(self)

    def test_nonfixture_check_runs_exact_argv_without_any_model_host_on_path(self):
        self.manifest['spec']['provider'] = 'runtime-only'
        flow = Flow(self.store, fixtures.PACKAGE)
        check = self.manifest['spec']['checks'][1]
        state = {'run_id': 'test', 'round': 0, 'candidate': self.base}
        with patch.dict(os.environ, {'PATH': '/usr/bin:/bin'}), patch.object(self.store, 'job', wraps=self.store.job) as job:
            result = flow.one_check(check, state, 0)
        self.assertEqual(job.call_args.args[1]['argv'], check['argv'])
        self.assertEqual(check['argv'][0], sys.executable)
        self.assertTrue(result)
        self.assertEqual(git(self.project, 'rev-parse', 'HEAD'), self.base)

    def test_bound_comparison_evidence_detects_later_input_changes(self):
        self.store.directory.mkdir(exist_ok=True)
        material = self.store.directory / 'comparison.txt'; material.write_text('original')
        from formal_gates_harness.infra import file_proof
        ref = self.store.put({'input_files': {str(material): file_proof(material)}})
        self.store.verify_evidence(ref)
        material.write_text('changed')
        with self.assertRaisesRegex(RuntimeBlocked, 'EVIDENCE_CHANGED'):
            self.store.verify_evidence(ref)


if __name__ == '__main__':
    unittest.main()
