"""Shared host boundary regressions. No paid/native model prompts."""
from pathlib import Path
import importlib
import json
import os
import shutil
import sys
import tempfile
import time
import unittest
from unittest.mock import Mock, patch
from formal_gates_harness.infra import Bridge,HarnessError,RuntimeBlocked,SessionUnavailable,atomic,canonical,file_proof,read
from formal_gates_harness.native_hosts import GoAdapter,adapter_for,execution_hosts,normalize_provider,host_catalog
# G2c: the pre-migration zcode transport is test-only reference code now; the
# production zcode execution path is the Go driver binary. The structural
# assertions below pin the reference implementation the differential
# (test_native_zcode_differential.py) compares the Go adapter against.
from reference_native_zcode import (Client,DEV_PERMISSION_MODE,advertised_models,apply_selection,
                                    handshake,materialize_source,reference,selection)

ROOT=Path(__file__).resolve().parents[3]
REF={'providerId':'original','modelId':'M'}

class HostRegistryTests(unittest.TestCase):
    def setUp(self):self.bridge=Bridge(str(ROOT/'.harness/bin/formal-gates'))
    def test_installation_aliases_remain_available(self):
        self.assertEqual(normalize_provider(self.bridge,'DeepSeek Harness'),'dsh')
        self.assertEqual(normalize_provider(self.bridge,'z-code'),'zcode')
        catalog=host_catalog(self.bridge)
        self.assertEqual({h['provider'] for h in catalog['hosts'] if h['execution_adapter']},set(execution_hosts()))
        self.assertIn('cursor',{h['provider'] for h in catalog['hosts'] if h['entry_integration']})
    def test_installation_only_is_not_a_runnable_host(self):
        self.assertEqual(normalize_provider(self.bridge,'cursor'),'cursor')
        with self.assertRaisesRegex(RuntimeBlocked,'UNSUPPORTED_NATIVE_HOST'):adapter_for('cursor')
        with self.assertRaises(HarnessError):normalize_provider(self.bridge,'unknown')
    def test_bound_host_changes_fail_before_execution(self):
        with tempfile.TemporaryDirectory() as t:
            p=Path(t)/'binary';p.write_text('original')
            binding={'provider':'zcode','files':{str(p):file_proof(p)}}
            adapter_for('zcode').validate_binding(binding)
            p.write_text('changed')
            with self.assertRaisesRegex(RuntimeBlocked,'HOST_CHANGED'):adapter_for('zcode').validate_binding(binding)
            with self.assertRaisesRegex(HarnessError,'HOST_BINDING'):adapter_for('dsh').validate_binding(binding)
    def test_zcode_segment_routes_to_the_go_driver(self):
        # G2c: both execution adapters are Go driver commands now; the package
        # keeps only each host's own side file as transport inventory.
        adapter=adapter_for('zcode')
        self.assertIs(type(adapter),GoAdapter)
        self.assertEqual(adapter.entry_argv('/bind/formal-gates',Path('/jobs/k')),
                         ['/bind/formal-gates','harness','native','/jobs/k'])
        self.assertEqual(adapter.transport_files(),('zcode_credential.mjs',))
        self.assertEqual(adapter_for('dsh').transport_files(),('native_dsh.mjs',))
    def test_claude_routes_to_the_driver_without_a_transport_plugin(self):
        adapter=adapter_for('claude')
        self.assertIs(type(adapter),GoAdapter)
        self.assertEqual(adapter.entry_argv('/bound/formal-gates',Path('/jobs/claude')),
                         ['/bound/formal-gates','harness','native','/jobs/claude'])
        self.assertEqual(adapter.transport_files(),())

    def test_codex_routes_to_the_driver_without_a_transport_plugin(self):
        adapter=adapter_for('codex')
        self.assertIs(type(adapter),GoAdapter)
        self.assertEqual(adapter.entry_argv('/bound/formal-gates',Path('/jobs/codex')),
                         ['/bound/formal-gates','harness','native','/jobs/codex'])
        self.assertEqual(adapter.transport_files(),())

    def test_zcode_capture_binds_node_and_script_through_the_bridge(self):
        node=shutil.which('node')
        if not node:self.skipTest('node is not available')
        with tempfile.TemporaryDirectory() as t:
            script=Path(t)/'zcode.cjs';script.write_text('native')
            with patch.dict(os.environ,{'FORMAL_GATES_ZCODE_SCRIPT':str(script)}):
                go_binding=adapter_for('zcode').capture()
            self.assertEqual(go_binding['provider'],'zcode')
            self.assertEqual(set(go_binding['launch']),{'executable','script'})
            self.assertEqual(go_binding['files'],
                             {go_binding['launch']['executable']:file_proof(Path(go_binding['launch']['executable'])),
                              go_binding['launch']['script']:file_proof(Path(go_binding['launch']['script']))})
            # A script with no catalogue anywhere pins no environment.
            self.assertNotIn('environment',go_binding)
    def test_same_evidence_verification_for_both_hosts(self):
        for provider in execution_hosts():
            with self.subTest(provider=provider),tempfile.TemporaryDirectory() as t:
                root=Path(t)
                files=('native-source.json','native-events.jsonl','answer.raw.txt','answer.json')
                for name in files:(root/name).write_text('{}')
                source={'provider':provider,'identity':'native','files':{n:file_proof(root/n) for n in files}}
                adapter_for(provider).verify_source(root,source)
                (root/'answer.raw.txt').write_text('changed')
                with self.assertRaisesRegex(RuntimeBlocked,'EVIDENCE_CHANGED'):adapter_for(provider).verify_source(root,source)
    def test_common_worker_store_do_not_import_dsh(self):
        base=ROOT/'runtime/langgraph/formal_gates_harness'
        for name in ('workers.py','infra.py','cli.py'):
            text=(base/name).read_text()
            self.assertNotIn('from .native_dsh import',text,name)
        # The retired Python execution paths must not come back through stale
        # modules: production dsh execution went to the Go driver binary in G2b
        # and production zcode execution (with the shared transport and the
        # entry) in G2c; the pre-migration implementations survive only as
        # tests/reference_* copies, which production code must not import.
        for module in ('native_dsh','native_zcode','native_transport','native_entry'):
            with self.subTest(module=module),self.assertRaises(ImportError):
                importlib.import_module('formal_gates_harness.'+module)
        # The workflow is still the same LangGraph graph, not one graph per host.
        text=(base/'graph.py').read_text()
        self.assertNotIn('native_zcode',text)
        self.assertNotIn("provider'] == 'dsh'",text)




class ZCodeEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.root=Path(self.tmp.name)
        atomic(self.root/'native-source.json',{'sessionId':'s','inputId':'i','selection':{'model':'bound','reasoning_effort':'high'}})
        self.answer={'status':'PASS','summary':'observed','findings':[],'check_ids':[]}
    def tearDown(self):self.tmp.cleanup()
    def events(self,wrong=None):
        start={'sessionId':'s','turnId':'t','seq':1,'type':'turn.started','payload':{'inputId':'i'}}
        end={'sessionId':'s','turnId':'t','seq':2,'type':'turn.completed','payload':{'inputId':'i','resultType':'success','response':'Done.\n```json\n'+json.dumps(self.answer)+'\n```','usage':{'inputTokens':10}}}
        if wrong:wrong(start,end)
        (self.root/'native-events.jsonl').write_text(json.dumps(start)+'\n'+json.dumps(end)+'\n')
    def test_matching_completion_yields_bound_source(self):
        self.events();source=materialize_source(self.root)
        self.assertEqual(source['provider'],'zcode');self.assertEqual(source['native_turn'],'t')
        self.assertEqual(json.loads((self.root/'answer.json').read_text()),self.answer)
        adapter_for('zcode').verify_source(self.root,source)
    def test_send_ack_or_other_input_is_not_completion(self):
        self.events(lambda a,b:b['payload'].update(inputId='other'))
        with self.assertRaisesRegex(RuntimeBlocked,'completion is missing'):materialize_source(self.root)
    def test_wrong_turn_and_session_rejected(self):
        for field in ('sessionId','turnId'):
            self.events(lambda a,b:b.update({field:'foreign'}))
            with self.subTest(field=field),self.assertRaises(RuntimeBlocked):materialize_source(self.root)
    def test_native_failure_not_laundered(self):
        self.events(lambda a,b:b.update(type='turn.failed'))
        with self.assertRaisesRegex(RuntimeBlocked,'turn failed'):materialize_source(self.root)
        self.assertFalse((self.root/'answer.json').exists())
    def test_saved_output_change_rejected(self):
        self.events();(self.root/'answer.raw.txt').write_text('forged')
        with self.assertRaisesRegex(RuntimeBlocked,'saved output differs'):materialize_source(self.root)
    def test_failed_transport_not_reconciled(self):
        self.events();atomic(self.root/'native-error.json',{'error':'actual network failure'})
        with self.assertRaisesRegex(RuntimeBlocked,'not a completed'):adapter_for('zcode').reconcile(self.root,{'exit_code':1,'error':''})
    def test_a_bound_selection_names_one_exact_provider_and_model(self):
        # A bare model name cannot be resolved against a catalogue this version
        # does not expose, so it is refused rather than matched to some provider.
        self.assertEqual(reference(json.dumps(REF)),REF)
        self.assertIsNone(reference(''))
        for value in ('M','{}',json.dumps({**REF,'extra':1}),json.dumps([REF])):
            with self.subTest(value=value),self.assertRaisesRegex(RuntimeBlocked,'ZCODE_CONFIG'):
                reference(value)

    def test_selection_is_read_from_the_host_and_never_invented(self):
        # 3.12.3 answers an unconfigured app-server with an empty catalogue and no
        # current model. That is recorded as it is; nothing is guessed or filled.
        empty={'settings':{'model':{'available':[],'current':None},'thoughtLevel':{'current':''}}}
        self.assertEqual(selection(empty),{'model':'','reasoning_effort':''})
        self.assertEqual(advertised_models(empty),[])
        bound={'settings':{'model':{'current':REF,'available':[{'ref':REF}]},'thoughtLevel':{'current':'high'}}}
        self.assertEqual(selection(bound),{'model':canonical(REF).decode(),'reasoning_effort':'high'})
        self.assertEqual(advertised_models(bound),[canonical(REF).decode()])

    def test_unadvertised_selection_is_refused_before_any_change(self):
        client=Mock(identity='root')
        snapshot={'settings':{'model':{'current':None,'available':[{'ref':REF}]},'thoughtLevel':{'current':''}}}
        with tempfile.TemporaryDirectory() as t,self.assertRaisesRegex(RuntimeBlocked,'no fallback'):
            apply_selection(client,snapshot,{'providerId':'other','modelId':'M'},'',Path(t),t)
        client.request.assert_not_called()

    def test_a_selection_the_host_does_not_apply_is_never_recorded(self):
        client=Mock(identity='root');client.request.return_value={}
        snapshot={'settings':{'model':{'current':None,'available':[{'ref':REF}]},'thoughtLevel':{'current':''}}}
        with tempfile.TemporaryDirectory() as t,self.assertRaisesRegex(RuntimeBlocked,'was not applied'):
            apply_selection(client,snapshot,REF,'',Path(t),t)
        self.assertEqual(client.request.call_args.args[0],'session/setModel')
        self.assertFalse((Path(t)/'identity.json').exists())

    def test_a_session_with_no_native_selection_and_none_bound_is_refused(self):
        client=Mock(identity='root')
        empty={'settings':{'model':{'available':[],'current':None},'thoughtLevel':{'current':''}}}
        with tempfile.TemporaryDirectory() as t,self.assertRaisesRegex(RuntimeBlocked,'selected model missing'):
            apply_selection(client,empty,None,'',Path(t),t)
        client.request.assert_not_called()

    def test_the_app_server_protocol_handshake_is_required(self):
        self.assertEqual(handshake({'protocol':{'name':'ZCode Protocol','version':1}}),
                         {'name':'ZCode Protocol','version':1})
        for protocol in ({},{'name':'ZCode Protocol','version':2},{'name':'ACP','version':1}):
            with self.subTest(protocol=protocol),self.assertRaisesRegex(RuntimeBlocked,'ZCODE_PROTOCOL'):
                handshake({'protocol':protocol})



    def test_missing_session_separate_from_model_error(self):
        for code,text,cls in ((-32004,'Session not found: old',SessionUnavailable),(-32031,'Model unavailable',RuntimeBlocked)):
            c=object.__new__(Client);c.counter=0;c.send=Mock();c.receive=Mock(return_value={'id':'native-1','error':{'code':code,'message':text}})
            with self.subTest(code=code,text=text),self.assertRaises(cls) as e:c.request('session/resume',{'sessionId':'old'})
            if cls is RuntimeBlocked:self.assertNotIsInstance(e.exception,SessionUnavailable)


# FAKE_APP_SERVER and ZCodeSuccessReceiptTests were removed with the production
# zcode execution path: the whole in-process success receipt (source.json
# persisted and equal to the returned receipt, tool-policy permission evidence,
# verify_source through the registry) is now pinned end to end - against the Go
# command, not just the reference - by test_native_zcode_differential.py
# (test_new_session_success_turn) and, for the reference alone, by the same
# file's value comparisons.


FLOODING_APP_SERVER='''
import json,sys,time
for line in sys.stdin:
    message=json.loads(line)
    if 'id' in message and 'method' in message:
        for i in range(30):
            print(json.dumps({'method':'session/event','params':{'sessionId':'other','type':'stream','seq':i}}),flush=True)
            time.sleep(0.03)
        print(json.dumps({'id':message['id'],'result':{'late':True}}),flush=True)
        break
'''


class BoundedRequestDeadlineTests(unittest.TestCase):
    def test_notifications_cannot_outlive_the_total_budget(self):
        # The shared request loop once only starved on an empty queue, so a host
        # that kept streaming notifications could push a bounded request past
        # its deadline and still have the late response accepted.
        with tempfile.TemporaryDirectory() as t:
            root=Path(t);fake=root/'flood-server.py';fake.write_text(FLOODING_APP_SERVER)
            workspace=root/'ws';workspace.mkdir();directory=root/'job';directory.mkdir()
            client=Client({'executable':sys.executable,'script':str(fake)},str(workspace),directory)
            try:
                started=time.monotonic()
                with self.assertRaisesRegex(RuntimeBlocked,'ZCODE_NATIVE_TIMEOUT'):
                    client.request('session/ping',{},timeout=0.05)
                self.assertLess(time.monotonic()-started,5)
            finally:
                client.close()


if __name__=='__main__':unittest.main()
