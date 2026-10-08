"""Issued check identities are assembled by the Go owner from semantic judgments."""
from pathlib import Path
import tempfile
import unittest
from formal_gates_harness.infra import Bridge, HarnessError, Store
from formal_gates_harness.workers import Workers

PACKAGE = Path(__file__).resolve().parents[3]
BINARY = PACKAGE / '.harness/bin/formal-gates'

class WorkerJudgmentTests(unittest.TestCase):
    def test_fixture_worker_uses_semantic_model_contract_and_typed_evidence(self):
        checks = [{'id': 'normal', 'argv': ['true'], 'dependencies': ['app.py']},
                  {'id': 'boundary', 'argv': ['true'], 'dependencies': ['app.py']}]
        bridge = Bridge(str(BINARY))
        schema = bridge.call('schema', {'checks': checks})
        self.assertNotIn('check_ids', schema['properties'])
        self.assertEqual(set(schema['properties']['check_reviews']['properties']), {'normal', 'boundary'})
        with tempfile.TemporaryDirectory() as home:
            root = Path(home)
            manifest = {'spec': {'provider': 'fixture', 'checks': checks}, 'revision': 'confirmed', 'fixture': True}
            store = Store(root, manifest, bridge)
            workers = Workers(store, PACKAGE)
            model = workers.fixture('qa-review', root)
            self.assertNotIn('check_ids', model)
            self.assertEqual(set(model['check_reviews']), {'normal', 'boundary'})
            output = workers.run('qa-review', root, {'candidate': 'original'})
            self.assertEqual(output['result']['check_ids'], ['normal', 'boundary'])
            self.assertEqual(output['result']['status'], model['status'])
            self.assertEqual(output['result']['findings'], model['findings'])
            self.assertEqual(output['result']['summary'], model['summary'] + ''.join(
                '\nCheck ' + check['id'] + ': ' + model['check_reviews'][check['id']] for check in checks))
            self.assertEqual(output['source']['model_calls'], 0)
            self.assertEqual(workers.run('qa-review', root, {'candidate': 'original'}), output)
        for result in ({**model, 'check_ids': ['normal', 'boundary']},
                       {**model, 'check_reviews': {'normal': 'real coverage review'}},
                       {**model, 'check_reviews': {'normal': 'real coverage review', 'boundary': ''}}):
            with self.assertRaises(HarnessError):
                bridge.call('validate-worker', {'role': 'qa-review', 'checks': checks, 'result': result})

if __name__ == '__main__':
    unittest.main()
