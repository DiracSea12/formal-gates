"""Cursor source carriers across continuation and explicit session restart."""
from pathlib import Path
import hashlib
import tempfile
import unittest
from unittest.mock import Mock, patch
from formal_gates_harness.graph import Flow
from formal_gates_harness.infra import SessionUnavailable, atomic, read, digest
from formal_gates_harness.workers import Workers

class CursorSourceContext(unittest.TestCase):
    def flow(self):
        flow = object.__new__(Flow)
        flow.store = Mock()
        flow.spec = {'base': 'base'}
        flow.workers = Mock()
        flow.store.put.return_value = 'ref'
        flow.store.bridge.call.side_effect = lambda op, value: {'action': 'continue' if value['status'] == 'CONTINUE' else 'settle'}
        flow.ask = Mock(return_value='restart-session')
        return flow

    def result(self, identity, path, status='PASS'):
        return {'result': {'status': status, 'summary': identity}, 'needs_repair': False,
                'source': {'identity': identity, 'native_store': path}}

    def test_continue_carries_its_original_store(self):
        flow = self.flow()
        flow.workers.run.side_effect = [self.result('a', '/store/a', 'CONTINUE'), self.result('a', '/store/a')]
        with patch('formal_gates_harness.graph.evidence_feedback', return_value={}):
            flow.invoke_worker('development', {'run_id': 'r'}, Path('/workspace'))
        first, second = flow.workers.run.call_args_list
        self.assertEqual(first.args[4], '')
        self.assertNotIn('native_store', first.kwargs)
        self.assertEqual(second.args[4], 'a')
        self.assertEqual(second.kwargs['native_store'], '/store/a')

    def test_repair_carries_the_development_source(self):
        flow = self.flow()
        flow.store.get.return_value = {'source': {'identity': 'a', 'native_store': '/store/a'}}
        flow.workers.run.return_value = self.result('a', '/store/a')
        flow.invoke_worker('repair', {'run_id': 'r', 'development': 'development-ref'}, Path('/workspace'), session='a')
        self.assertEqual(flow.workers.run.call_args.kwargs['native_store'], '/store/a')
        flow.store.get.assert_called_once_with('development-ref')

    def test_other_session_source_cannot_be_used(self):
        flow = self.flow()
        flow.store.get.return_value = {'source': {'identity': 'b', 'native_store': '/store/b'}}
        flow.workers.run.return_value = self.result('a', '/store/a')
        flow.invoke_worker('repair', {'run_id': 'r', 'development': 'ref'}, Path('/workspace'), session='a')
        self.assertNotIn('native_store', flow.workers.run.call_args.kwargs)

    def test_missing_store_asks_explicit_restart_then_preserves_new_source(self):
        flow = self.flow()
        flow.workers.run.side_effect = [SessionUnavailable('native-resource-missing', identity='a', method='--resume', code=None),
                                       self.result('b', '/store/b', 'CONTINUE'), self.result('b', '/store/b')]
        with patch('formal_gates_harness.graph.evidence_feedback', return_value={}):
            flow.invoke_worker('development', {'run_id': 'r'}, Path('/workspace'), session='a')
        flow.ask.assert_called_once()
        self.assertEqual(flow.ask.call_args.args[1], ['restart-session', 'abort'])
        calls = flow.workers.run.call_args_list
        self.assertTrue(calls[1].kwargs['restart_session'])
        self.assertEqual(calls[2].args[4], 'b')
        self.assertEqual(calls[2].kwargs['native_store'], '/store/b')

    def test_worker_uses_same_series_identity_and_store_pair(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp); workspace = root/'workspace'; workspace.mkdir()
            body = b'Approved task'; (root/'requirement.md').write_bytes(body)
            package = root/'package'; (package/'prompts').mkdir(parents=True)
            (package/'prompts/reviewer-base.md').write_text('Reviewer policy')
            store = Mock(); store.directory = root
            store.manifest = {'revision': 'revision', 'spec': {'provider': 'cursor', 'model': 'model-a', 'reasoning_effort': 'high',
                'base': 'base', 'checks': [], 'requirement_digest': hashlib.sha256(body).hexdigest()}, 'host_binding': {'provider': 'cursor'}}
            store.bridge.call.side_effect = lambda op, value: {} if op == 'schema' else {'result': value['result'], 'needs_repair': False}
            store.put.return_value = 'evidence'
            series = digest({'role':'development','input':{'candidate':'base'},'feedback':None,'revision':'revision'})
            atomic(root/'native-identities'/f'{series}.json', {'identity':'latest','native_store':'/native/latest/store.db',
                                                           'workspace':str(workspace),'provider':'cursor'})
            captured = []
            def job(key, request):
                directory = root/'jobs'/key; native = read(directory/'native-request.json'); captured.append(native)
                atomic(directory/'identity.json', {'identity':native['identity'],'native_store':native['native_store'],
                                                   'workspace':str(workspace),'provider':'cursor'})
                atomic(directory/'answer.json', {'status':'PASS','summary':'finished','findings':[],'check_ids':[]})
                atomic(directory/'source.json', {'provider':'cursor','identity':native['identity'],'native_store':native['native_store']})
                return {'exit_code':0,'error':''}
            store.job.side_effect = job
            adapter = Mock(name='cursor'); adapter.name = 'cursor'; adapter.entry_argv.return_value = ['native']
            with patch('formal_gates_harness.workers.adapter_for', return_value=adapter):
                Workers(store,package).run('development',workspace,{'candidate':'base'},session='stale',native_store='/native/stale/store.db')
            self.assertEqual(captured[0]['identity'], 'latest')
            self.assertEqual(captured[0]['native_store'], '/native/latest/store.db')
            saved = read(root/'native-identities'/f'{series}.json')
            self.assertEqual((saved['identity'],saved['native_store']),('latest','/native/latest/store.db'))

if __name__ == '__main__': unittest.main()
