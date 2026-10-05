"""Native startup carries observed selections or the caller's optional overrides."""
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from formal_gates_harness import cli, native_hosts
from formal_gates_harness.infra import HarnessError, RuntimeBlocked, git, read


class NativeStartupSelection(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        for relative in ('prompts/reviewer-base.md', 'runtime/langgraph/requirements.lock'):
            path = self.root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('Bound test input\n')
        self.project = self.root / 'project'
        self.project.mkdir()
        git(self.project, 'init', '--quiet')
        (self.project / 'content.txt').write_text('Committed source\n')
        git(self.project, 'add', 'content.txt')
        git(self.project, '-c', 'user.name=Tests', '-c', 'user.email=tests@localhost',
            '-c', 'commit.gpgsign=false', 'commit', '--quiet', '-m', 'Baseline')
        self.requirement = self.root / 'requirement.md'
        self.requirement.write_text('Preserve the committed content.\n')
        self.registry = patch.dict(native_hosts._ADAPTERS,
                                   {'test-runtime': native_hosts.GoAdapter('test-runtime')})
        self.registry.start()
        self.addCleanup(self.registry.stop)
        self.binding = {'provider': 'test-runtime', 'launch': {'executable': '/native'}, 'files': {}}

    def create(self, probe, model='', effort=''):
        with patch.object(cli, 'doctor', return_value={'provider': probe}) as doctor:
            directory = cli.make_run(self.root, self.project, self.requirement,
                                     [{'id': 'content', 'argv': ['true'], 'dependencies': ['content.txt']}],
                                     ['complexity-gate'], provider='test-runtime', model=model, effort=effort)
        doctor.assert_called_once_with(self.root, 'test-runtime', model, effort)
        manifest = read(directory / 'manifest.json')
        self.assertEqual(manifest['host_binding'], self.binding)
        return manifest['spec']

    def test_unobserved_probe_preserves_optional_overrides(self):
        probe = {'selection': {}, 'capabilities': {'selection_observed': False}, 'binding': self.binding}
        for model, effort in (('', ''), ('chosen-model', 'high')):
            with self.subTest(model=model):
                spec = self.create(probe, model, effort)
                self.assertEqual((spec['model'], spec['reasoning_effort']), (model, effort))

    def test_observed_probe_keeps_native_selection_contract(self):
        for capabilities in ({}, {'selection_observed': True}):
            with self.subTest(capabilities=capabilities):
                spec = self.create({'selection': {'model': 'native-model', 'reasoning_effort': 'medium'},
                                    'capabilities': capabilities, 'binding': self.binding})
                self.assertEqual((spec['model'], spec['reasoning_effort']), ('native-model', 'medium'))
                with self.assertRaisesRegex(HarnessError, 'did not advertise a selected model'):
                    self.create({'selection': {}, 'capabilities': capabilities, 'binding': self.binding})

    def test_failed_probe_cannot_create_a_run(self):
        with patch.object(cli, 'doctor', side_effect=RuntimeBlocked('native failure')):
            with self.assertRaisesRegex(RuntimeBlocked, 'native failure'):
                cli.make_run(self.root, self.project, self.requirement,
                             [{'id': 'content', 'argv': ['true'], 'dependencies': []}],
                             ['complexity-gate'], provider='test-runtime')
        self.assertFalse((self.root / '.harness' / 'runs').exists())

    def test_native_demo_uses_the_same_default_selection(self):
        probe = {'selection': {}, 'capabilities': {'selection_observed': False}, 'binding': self.binding}
        with patch.object(cli, 'doctor', return_value={'provider': probe}):
            directory = cli.make_demo(self.root, native=True, provider='test-runtime')
        manifest = read(directory / 'manifest.json')
        self.assertEqual((manifest['spec']['model'], manifest['spec']['reasoning_effort']), ('', ''))
        self.assertFalse(manifest['fixture'])
