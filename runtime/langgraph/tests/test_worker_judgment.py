"""Issued check identities are assembled by the Go owner from semantic judgments."""
from pathlib import Path
import hashlib
import json
import tempfile
import unittest
from unittest.mock import Mock, patch
from formal_gates_harness.infra import Bridge, HarnessError, RuntimeBlocked, Store
from formal_gates_harness.workers import Workers

PACKAGE = Path(__file__).resolve().parents[3]
BINARY = PACKAGE / '.harness/bin/formal-gates'

class WorkerJudgmentTests(unittest.TestCase):
    def test_current_materials_hide_control_bindings_and_keep_approved_qa_checks(self):
        checks = [{'id': 'program-owned-id', 'argv': ['go', 'test', './app'], 'dependencies': ['app']}]
        for role in ('technical-review', 'qa-review'):
            with self.subTest(role=role), tempfile.TemporaryDirectory() as home:
                root = Path(home)
                workspace = root / 'workspace'; workspace.mkdir()
                requirement = b'Approved requirement\n'
                (root / 'requirement.md').write_bytes(requirement)
                manifest = {'spec': {'provider': 'codex', 'checks': checks, 'base': 'program-base',
                                     'model': 'fixture-model', 'reasoning_effort': '',
                                     'requirement_digest': hashlib.sha256(requirement).hexdigest()},
                            'revision': 'program-revision'}
                store = Store(root, manifest, Bridge(str(BINARY)))
                adapter = Mock(name='labelled no-host fixture')
                adapter.name = 'codex'
                adapter.capture.return_value = {'fixture': True}
                with patch('formal_gates_harness.workers.adapter_for', return_value=adapter), \
                     patch.object(store, 'job', side_effect=RuntimeBlocked('Labelled no-host material fixture')):
                    with self.assertRaisesRegex(RuntimeBlocked, 'no-host'):
                        Workers(store, PACKAGE).run(role, workspace, {'candidate': 'program-candidate'},
                                                   feedback={'unfinished': 'Remaining original scope'})
                materials = next((workspace / '.formal-gates-input').iterdir())
                task = json.loads((materials / 'task.json').read_text())
                self.assertEqual(task['feedback'], {'unfinished': 'Remaining original scope'})
                if role == 'qa-review':
                    self.assertEqual(task['checks'], [{'argv': ['go', 'test', './app'], 'dependencies': ['app']}])
                else:
                    self.assertNotIn('checks', task)
                self.assertFalse({'profile', 'role', 'revision', 'base', 'candidate', 'control'} & task.keys())
                native = json.loads(next((root / 'jobs').glob('*/native-request.json')).read_text())
                self.assertEqual(native['materials'], str(materials))
                self.assertNotIn('program-revision', native['prompt'])
                self.assertNotIn('program-base', native['prompt'])
                finding = native['schema']['anyOf'][0]['properties']['findings']['items']
                self.assertIn('locations', finding['properties'])
                self.assertNotIn('paths', finding['properties'])

    def test_fixture_worker_uses_semantic_model_contract_and_typed_evidence(self):
        checks = [{'id': 'normal', 'argv': ['true'], 'dependencies': ['app.py']},
                  {'id': 'boundary', 'argv': ['true'], 'dependencies': ['app.py']}]
        bridge = Bridge(str(BINARY))
        schema = bridge.call('schema', {'checks': checks})['anyOf'][0]
        self.assertNotIn('check_ids', schema['properties'])
        self.assertNotIn('status', schema['properties'])
        self.assertEqual(schema['properties']['check_reviews']['anyOf'][0]['minItems'],2)
        with tempfile.TemporaryDirectory() as home:
            root = Path(home)
            manifest = {'spec': {'provider': 'fixture', 'checks': checks}, 'revision': 'confirmed', 'fixture': True}
            store = Store(root, manifest, bridge)
            workers = Workers(store, PACKAGE)
            model = workers.fixture('qa-review', root)
            self.assertNotIn('check_ids', model)
            self.assertEqual(len(model['check_reviews']),2)
            self.assertNotIn('status',model)
            output = workers.run('qa-review', root, {'candidate': 'original'})
            self.assertEqual(output['result']['check_ids'], ['normal', 'boundary'])
            self.assertEqual(output['result']['status'], 'PASS')
            self.assertEqual(output['result']['findings'], model['findings'])
            self.assertEqual(output['result']['summary'], model['summary'] + ''.join(
                '\nCheck ' + check['id'] + ': ' + model['check_reviews'][index] for index,check in enumerate(checks)))
            self.assertEqual(output['source']['model_calls'], 0)
            self.assertEqual(workers.run('qa-review', root, {'candidate': 'original'}), output)
        for result in ({**model, 'check_ids': ['normal', 'boundary']},
                       {**model, 'check_reviews': {'normal': 'real coverage review'}},
                       {**model, 'check_reviews': {'normal': 'real coverage review', 'boundary': ''}}):
            with self.assertRaises(HarnessError):
                bridge.call('validate-worker', {'role': 'qa-review', 'checks': checks, 'result': result})

if __name__ == '__main__':
    unittest.main()
