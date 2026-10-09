"""Compare comparison materials with an independent oracle on the same real Git repository.

The oracle uses reference_canonical. Production uses the Go comparison-plan
bridge. Separate material directories let both paths generate their full output."""
from pathlib import Path
import json
import tempfile
import unittest

from formal_gates_harness.feedback import materialize_comparison
from formal_gates_harness.infra import RuntimeBlocked, git, driver_bridge
import reference_comparison


class ComparisonMaterialDifferential(unittest.TestCase):
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
        (self.root / 'café.txt').write_text('café before\n')
        (self.root / '新建.txt').write_text('new unicode before\n')
        self.commit('Baseline')
        self.base = git(self.root, 'rev-parse', 'HEAD')
        (self.root / 'edited.txt').write_text('after\n')
        (self.root / 'removed.txt').unlink()
        (self.root / 'rename.txt').rename(self.root / 'renamed.txt')
        (self.root / 'added.txt').write_text('new independent content\n')
        (self.root / 'binary.bin').write_bytes(b'\x00new\xfe')
        (self.root / 'link').unlink(); (self.root / 'link').symlink_to('added.txt')
        (self.root / 'café.txt').write_text('café after\n')
        (self.root / '新建.txt').write_text('new unicode after\n')
        (self.root / '新增.txt').write_text('added unicode file\n')
        (self.root / 'module').mkdir()
        git(self.root, 'update-index', '--add', '--cacheinfo', '160000', self.base, 'module')
        self.commit('Candidate')
        self.candidate = git(self.root, 'rev-parse', 'HEAD')
        self.ref_materials = self.root / '.ref-input' / 'review'
        self.new_materials = self.root / '.new-input' / 'review'

    def commit(self, message):
        git(self.root, 'add', '.')
        git(self.root, '-c', 'user.name=Tests', '-c', 'user.email=tests@localhost',
            '-c', 'commit.gpgsign=false', 'commit', '--quiet', '-m', message)

    def _compare_directories(self, ref_dir, new_dir):
        ref_files = sorted(p.relative_to(ref_dir).as_posix() for p in ref_dir.rglob('*') if p.is_file())
        new_files = sorted(p.relative_to(new_dir).as_posix() for p in new_dir.rglob('*') if p.is_file())
        self.assertEqual(ref_files, new_files, 'material file sets differ')
        for rel in ref_files:
            a = (ref_dir / rel).read_bytes()
            b = (new_dir / rel).read_bytes()
            self.assertEqual(a, b, 'material bytes differ: ' + rel)

    def _run_both(self, base, candidate):
        ref = reference_comparison.materialize_comparison(self.root, self.ref_materials, base, candidate)
        new = materialize_comparison(self.root, self.new_materials, base, candidate)
        return ref, new

    def test_full_delta_is_byte_identical_everywhere(self):
        ref, new = self._run_both(self.base, self.candidate)
        self.assertEqual(ref['base'], new['base'])
        self.assertEqual(ref['candidate'], new['candidate'])
        # file sets (material names) must match; proofs are content-based.
        self.assertEqual(set(ref['files']), set(new['files']))
        self._compare_directories(Path(ref['directory']), Path(new['directory']))
        # comparison.json must be byte-for-byte identical.
        ref_index = (Path(ref['directory']) / 'comparison.json').read_bytes()
        new_index = (Path(new['directory']) / 'comparison.json').read_bytes()
        self.assertEqual(ref_index, new_index)
        # changes identical: order, null sides, gitlink note, material names.
        ref_changes = json.loads(ref_index)['changes']
        new_changes = json.loads(new_index)['changes']
        self.assertEqual(ref_changes, new_changes)
        # Spot-check the interesting shapes survived the round trip.
        renamed = next(c for c in ref_changes if c['status'].startswith('R'))
        self.assertEqual(renamed['base']['path'], 'rename.txt')
        self.assertEqual(renamed['candidate']['path'], 'renamed.txt')
        self.assertEqual(renamed['base']['material'], 'base/rename.txt')
        added = next(c for c in ref_changes if c['status'] == 'A' and c['candidate']['path'] == 'added.txt')
        self.assertIsNone(added['base'])
        self.assertEqual(added['candidate']['material'], 'candidate/added.txt')
        module = next(c['candidate'] for c in ref_changes if c['candidate'] and c['candidate']['path'] == 'module')
        self.assertEqual(module['mode'], '160000')
        self.assertIn('not included', module['note'])
        self.assertNotIn('material', module)
        # Unicode path material names and bytes.
        new_dir = Path(new['directory'])
        self.assertEqual((new_dir / 'base' / 'café.txt').read_text(), 'café before\n')
        self.assertEqual((new_dir / 'candidate' / '新增.txt').read_text(), 'added unicode file\n')

    def test_comparison_plan_bridge_matches_reference_changes(self):
        # Produce a reference comparison.json to diff the bridge plan against.
        reference_comparison.materialize_comparison(self.root, self.ref_materials, self.base, self.candidate)
        raw = git(self.root, 'diff', '--no-ext-diff', '--no-textconv', '--find-renames',
                  '--raw', '--no-abbrev', '-z', self.base, self.candidate, '--')
        plan = driver_bridge().call('comparison-plan',
                                    {'base': self.base, 'candidate': self.candidate,
                                     'diff': 'full.diff', 'raw': raw})
        ref_index = (self.ref_materials / 'comparison' / 'comparison.json').read_bytes()
        ref_changes = json.loads(ref_index)['changes']
        self.assertEqual(plan['changes'], ref_changes)
        index = json.loads(plan['index'])
        self.assertEqual(index['base'], self.base)
        self.assertEqual(index['candidate'], self.candidate)
        self.assertEqual(index['diff'], 'full.diff')
        self.assertEqual(index['changes'], ref_changes)

    def test_empty_change_is_still_byte_identical(self):
        # diff an object against itself: the raw stream is empty.
        ref, new = self._run_both(self.candidate, self.candidate)
        ref_index = (Path(ref['directory']) / 'comparison.json').read_bytes()
        new_index = (Path(new['directory']) / 'comparison.json').read_bytes()
        self.assertEqual(ref_index, new_index)
        self.assertEqual(json.loads(ref_index)['changes'], [])

    def test_tampered_material_refuses_in_both_with_same_wording(self):
        reference_comparison.materialize_comparison(self.root, self.ref_materials, self.base, self.candidate)
        materialize_comparison(self.root, self.new_materials, self.base, self.candidate)
        (self.ref_materials / 'comparison' / 'base' / 'removed.txt').write_text('tampered')
        (self.new_materials / 'comparison' / 'base' / 'removed.txt').write_text('tampered')
        with self.assertRaisesRegex(RuntimeBlocked, 'comparison material changed') as ref_ctx:
            reference_comparison.materialize_comparison(self.root, self.ref_materials, self.base, self.candidate)
        with self.assertRaisesRegex(RuntimeBlocked, 'comparison material changed') as new_ctx:
            materialize_comparison(self.root, self.new_materials, self.base, self.candidate)
        self.assertEqual(str(ref_ctx.exception), str(new_ctx.exception))

    def test_repeat_call_is_identical(self):
        first = materialize_comparison(self.root, self.new_materials, self.base, self.candidate)
        second = materialize_comparison(self.root, self.new_materials, self.base, self.candidate)
        self.assertEqual(first, second)
        rf = reference_comparison.materialize_comparison(self.root, self.ref_materials, self.base, self.candidate)
        rs = reference_comparison.materialize_comparison(self.root, self.ref_materials, self.base, self.candidate)
        self.assertEqual(rf, rs)


if __name__ == '__main__':
    unittest.main()
