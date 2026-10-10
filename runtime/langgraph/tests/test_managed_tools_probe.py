"""The real probe verifies managed tool effects; this fixture sends no model call."""
import contextlib
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import native_multihost
from formal_gates_harness.infra import read


class ManagedToolsProbe(unittest.TestCase):
    def exercise(self, corrupt=False):
        binary = native_multihost.ROOT / '.harness/bin/formal-gates'
        requests = []

        class FixtureAdapter:
            def capture(self):
                return {'fixture': True}

            def execute(self, request, job):
                requests.append(request)
                (job / 'native-request.json').write_text(json.dumps(request))
                self.source = {'provider': 'fixture', 'identity': 'fixture-session', 'selection': {}}
                def call(arguments):
                    wire = {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/call',
                            'params': {'name': 'workspace', 'arguments': arguments}}
                    result = subprocess.run([str(binary), 'harness', 'native-tools', str(job)],
                                            input=json.dumps(wire).encode(), capture_output=True)
                    if result.returncode: raise AssertionError(result.stderr)
                    reply = json.loads(result.stdout)['result']
                    if reply['isError']: raise AssertionError(reply)
                    return json.loads(reply['content'][0]['text'])
                listing = call({'action': 'list'})
                seed = next(entry['entry'] for entry in listing['entries'] if entry['name'] == 'seed.bin')
                name = 'resume.bin' if request.get('identity') else 'direct.bin'
                if name not in request['prompt']: raise AssertionError('fixture ignored original task')
                call({'action': 'copy', 'listing': listing['listing'], 'entry': seed, 'name': name})
                if corrupt:
                    (Path(request['workspace']) / name).write_bytes(b'wrong original bytes')
                (job / 'answer.json').write_text(json.dumps({'summary': 'Labelled fixture managed copy'}))
                return self.source

            def verify_source(self, job, source):
                if source != self.source: raise AssertionError('wrong fixture source')
                if not (job / 'native-tool-calls.jsonl').is_file(): raise AssertionError('missing actual tool calls')

        with tempfile.TemporaryDirectory() as home:
            output = Path(home) / 'probe'
            with patch('formal_gates_harness.native_hosts.adapter_for', return_value=FixtureAdapter()), contextlib.redirect_stdout(io.StringIO()):
                code = native_multihost.exercise_tools('fixture', str(output))
            report = read(output / 'report.json')
            fetch = read(output / 'fetch.json')
            denial = read(output / 'read-only-check' / 'denial.json')
            self.assertEqual(fetch['exit_code'], 0)
            self.assertTrue(denial['isError'])
            self.assertIn('read-only', denial['content'][0]['text'])
            self.assertEqual(requests[1]['identity'], 'fixture-session')
            for request in requests:
                self.assertNotIn('http://', request['prompt'])
                self.assertNotIn('shell', request['prompt'])
                self.assertNotIn('subagent', request['prompt'])
                self.assertNotIn('fetch-response', request['prompt'])
            return code, report

    def test_original_bytes_managed_copy_resume_and_program_fetch(self):
        code, report = self.exercise()
        self.assertEqual(code, 0)
        self.assertTrue(all(report['effects'].values()))
        self.assertTrue(all(value['source']['provider'] == 'fixture' for value in report['results'].values()))

    def test_wrong_original_bytes_cannot_pass_from_a_model_claim(self):
        code, report = self.exercise(corrupt=True)
        self.assertEqual(code, 1)
        self.assertFalse(report['effects']['direct_copy'])
        self.assertFalse(report['effects']['resumed_copy'])


if __name__ == '__main__':
    unittest.main()
