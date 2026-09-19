"""Formatting recovery is evidence replay, not a fresh model invocation."""
from pathlib import Path
import hashlib
import json
import tempfile
import unittest
from unittest.mock import patch, Mock
from langgraph.checkpoint.sqlite import SqliteSaver
from langgraph.graph import StateGraph, START, END
from langgraph.types import Command
from formal_gates_harness.native_result import decode_result
from reference_native_dsh import reconcile_completed_output
from formal_gates_harness.infra import atomic, read, RuntimeBlocked, HarnessError, Bridge, Store
from formal_gates_harness.graph import Flow, State
from formal_gates_harness.cli import adopt_transport

PACKAGE = Path(__file__).resolve().parents[3]
BINARY = PACKAGE / '.harness/bin/formal-gates'
ANSWER = {'status':'PASS','summary':'Implemented approved change.','findings':[],'check_ids':[]}

class OutputRecovery(unittest.TestCase):
    def test_plain_and_fenced_result(self):
        for text in (json.dumps(ANSWER), 'Completed.\n\n```json\n'+json.dumps(ANSWER)+'\n```',
                     '```\n'+json.dumps(ANSWER)+'\n```\nEvidence retained.'):
            self.assertEqual(decode_result(text), ANSWER)
    def test_conflicting_or_duplicate_objects_rejected(self):
        for text in ('{"status":"FAIL","status":"PASS"}', '{"nested":{"x":1,"x":2}}',
                     '{} {}','[]','{"x":NaN}',
                     '```json\n{}\n```\n```json\n{}\n```',
                     '{"status":"FAIL"}\n```json\n'+json.dumps(ANSWER)+'\n```',
                     '```json\n{"status":"FAIL","status":"PASS"}\n```',
                     'Output: '+json.dumps(ANSWER)):
            with self.subTest(text=text), self.assertRaises(RuntimeBlocked): decode_result(text)
    def test_control_fields_still_rejected_by_go(self):
        answer=decode_result('```json\n'+json.dumps({**ANSWER,'goto':'seal'})+'\n```')
        with self.assertRaises(HarnessError):
            Bridge(str(BINARY)).call('validate-worker',{'role':'development','result':answer,'checks':[]})
    def make_evidence(self, root, reason='completed'):
        atomic(root/'native-error.json',{'error':'RuntimeBlocked: DSH_RESULT: final message is not a JSON result object'})
        atomic(root/'identity.json',{'identity':'native','workspace':'workspace','selection':{'model':'chosen','reasoning_effort':'high'}})
        atomic(root/'native-request.json',{'workspace':'workspace','model':'chosen','reasoning_effort':'high'})
        atomic(root/'native-source.json',{'sessionId':'native','turn':1,'transcriptPath':'retained-native-log'})
        raw='Already implemented.\n\n```json\n'+json.dumps(ANSWER)+'\n```'
        (root/'answer.raw.txt').write_text(raw)
        records=[{'sessionId':'native','event':{'type':'assistant/message','data':{'turn':1,'message':{'content':[{'type':'text','text':raw}]},'usage':{'inputTokens':12}}}},
                 {'sessionId':'native','event':{'type':'turn/end','data':{'turn':1,'reason':{'kind':reason}}}}]
        (root/'native-events.jsonl').write_text(''.join(json.dumps(r)+'\n' for r in records))
    def test_reconcile_preserves_original_evidence_without_host(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);self.make_evidence(root)
            originals={p:p.read_bytes() for p in root.iterdir()}
            with patch('reference_native_dsh.Client',side_effect=AssertionError('must not launch')):
                source=reconcile_completed_output(root,{'exit_code':1,'error':''})
            self.assertEqual(source['identity'],'native');self.assertEqual(source['reconciliation']['new_model_calls'],0)
            self.assertEqual(read(root/'answer.json'),ANSWER)
            for p,data in originals.items():self.assertEqual(p.read_bytes(),data)
    def test_reconcile_cannot_launder_runtime_failure(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);self.make_evidence(root,reason='errored')
            with self.assertRaises(RuntimeBlocked):reconcile_completed_output(root,{'exit_code':1,'error':''})
            self.assertFalse((root/'answer.json').exists())
            self.make_evidence(root)
            atomic(root/'native-error.json',{'error':'actual model error'})
            with self.assertRaises(RuntimeBlocked):reconcile_completed_output(root,{'exit_code':1,'error':''})
            with self.assertRaises(RuntimeBlocked):reconcile_completed_output(root,{'exit_code':130,'error':'cancelled'})
    def test_bound_raw_output_cannot_be_changed(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);self.make_evidence(root);(root/'answer.raw.txt').write_text('{}')
            with self.assertRaises(RuntimeBlocked):reconcile_completed_output(root,{'exit_code':1,'error':''})
    def test_paused_node_can_accept_reconciled_result(self):
        with tempfile.TemporaryDirectory() as tmp:
            store=Store(Path(tmp),{'spec':{'base':'base'},'revision':'revision','run_id':'same-run'},Bridge(str(BINARY)))
            flow=Flow(store,PACKAGE)
            output={'source':{'identity':'native'},'result':ANSWER,'needs_repair':False}
            flow.workers=Mock();flow.workers.run.side_effect=[RuntimeBlocked('formatting failure'),output]
            def node(state):
                ref,result=flow.invoke_worker('development',state,Path(tmp))
                return {'development':ref}
            g=StateGraph(State);g.add_node('development',node);g.add_edge(START,'development');g.add_edge('development',END)
            config={'configurable':{'thread_id':'same-run'}}
            with SqliteSaver.from_conn_string(str(Path(tmp)/'checkpoints.sqlite')) as saver:
                app=g.compile(checkpointer=saver);first=app.invoke({'run_id':'same-run'},config)
                self.assertEqual(first['__interrupt__'][0].value['kind'],'runtime')
                final=app.invoke(Command(resume={'decision':'retry','revision':'revision'}),config)
                self.assertEqual(store.get(final['development']),output)
                self.assertFalse(app.get_state(config).next)

class TransportAdoption(unittest.TestCase):
    def case(self):
        tmp=tempfile.TemporaryDirectory();self.addCleanup(tmp.cleanup)
        # adopt_transport resolves both the run and the snapshot, so compare like
        # with like: on macOS TMPDIR itself is a symlink (/var -> /private/var).
        base=Path(tmp.name).resolve()
        root=base/'current';snap=base/'original';run=base/'run';run.mkdir()
        paths=['runtime/langgraph/formal_gates_harness/'+n for n in ('cli.py','workers.py','native_dsh.py','native_dsh.mjs','graph.py','infra.py')]
        paths+=['runtime/langgraph/requirements.lock','.harness/bin/formal-gates','prompts/reviewer-base.md']
        for base in (root,snap):
            for rel in paths:
                p=base/rel;p.parent.mkdir(parents=True,exist_ok=True);p.write_text('original')
        (root/paths[0]).write_text('reviewed transport change')
        req=b'requirement';(run/'requirement.md').write_bytes(req)
        atomic(run/'manifest.json',{'profile':'harness-v1','runtime_identity':'before','revision':'rev','run_id':'run',
                                    'spec':{'provider':'dsh','requirement_digest':hashlib.sha256(req).hexdigest()}})
        return root,snap,run
    def invoke(self,root,snap,run):
        with patch('formal_gates_harness.cli.package_root',return_value=root), \
             patch('formal_gates_harness.cli.binary_path',return_value=str(root/'.harness/bin/formal-gates')), \
             patch('formal_gates_harness.cli.runtime_identity',side_effect=lambda p,b:'before' if p==snap else 'after'), \
             patch('formal_gates_harness.cli.Bridge') as bridge, \
             patch('formal_gates_harness.cli.Flow'), \
             patch('formal_gates_harness.cli.status_projection',return_value={'status':'WAITING_USER','interrupts':[{'request':{'kind':'runtime'}}]}):
            bridge.return_value.call.return_value={'revision':'rev'}
            return adopt_transport(run,snap,'before','after','Reviewed parser correction')
    def test_explicit_adoption_keeps_manifest_and_checkpoint_bytes(self):
        root,snap,run=self.case();before=(run/'manifest.json').read_bytes()
        result=self.invoke(root,snap,run)
        self.assertEqual(result['status'],'TRANSPORT_ADOPTED');self.assertFalse(result['state_migrated'])
        self.assertEqual((run/'manifest.json').read_bytes(),before)
    def test_graph_binary_and_dependency_change_not_allowed(self):
        for rel in ('runtime/langgraph/formal_gates_harness/graph.py','.harness/bin/formal-gates','runtime/langgraph/requirements.lock','prompts/reviewer-base.md'):
            with self.subTest(path=rel):
                root,snap,run=self.case();(root/rel).write_text('changed')
                with self.assertRaises(HarnessError):self.invoke(root,snap,run)
                self.assertFalse((run/'transport-adoption.json').exists())
    def test_active_job_not_adopted(self):
        root,snap,run=self.case();atomic(run/'jobs'/'active'/'request.json',{})
        with self.assertRaises(HarnessError):self.invoke(root,snap,run)
        self.assertFalse((run/'transport-adoption.json').exists())

if __name__=='__main__': unittest.main()
