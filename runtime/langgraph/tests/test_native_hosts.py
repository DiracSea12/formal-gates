"""Shared host boundary regressions. No paid/native model prompts."""
from pathlib import Path
import json
import os
import queue
import tempfile
import unittest
from unittest.mock import Mock,patch
from formal_gates_harness.infra import Bridge,HarnessError,RuntimeBlocked,SessionUnavailable,atomic,file_proof
from formal_gates_harness.native_hosts import adapter_for,execution_hosts,normalize_provider,host_catalog
from formal_gates_harness.native_zcode import materialize_source,exact_model,Client

ROOT=Path(__file__).resolve().parents[3]

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
    def test_model_provider_never_silently_substituted(self):
        ref={'providerId':'original','modelId':'M'}
        snap={'settings':{'model':{'current':ref,'available':[{'ref':ref}]}}}
        self.assertEqual(exact_model('M',snap),ref)
        with self.assertRaisesRegex(RuntimeBlocked,'no fallback'):
            exact_model(json.dumps({'providerId':'other','modelId':'M'}),snap)

    def test_resume_uses_native_config_without_persisting_secret(self):
        from formal_gates_harness.native_zcode import resume_model_environment,runtime_model
        model={'providerId':'bound-provider','modelId':'M'}
        with tempfile.TemporaryDirectory() as t:
            path=Path(t)/'host.json'
            original={'provider':{'bound-provider':{'enabled':True,'kind':'anthropic','options':{'baseURL':'https://native.example/v1','apiKey':'FAKE_TEST_ONLY'},'models':{'M':{}}}}}
            atomic(path,original);before=path.read_bytes()
            with patch.dict(os.environ,FORMAL_GATES_ZCODE_CONFIG=str(path)):
                env,local=resume_model_environment(json.dumps(model))
            catalog={'modelCatalog':{'providers':[{'providerId':'bound-provider','kind':'openai-compatible','source':'builtin','models':[{'modelId':'M'}]}]}}
            value=runtime_model(catalog,model,'high',local)
            self.assertEqual(env['FORMAL_GATES_ZCODE_API_KEY'],'FAKE_TEST_ONLY')
            self.assertNotIn('FAKE_TEST_ONLY',json.dumps(value))
            self.assertEqual(value['provider']['kind'],'anthropic')
            self.assertNotIn('apiFormat',value['provider'])
            self.assertEqual(value['provider']['apiKey']['source'],'env')
            self.assertEqual(value['provider']['baseURL'],'https://native.example/v1')
            self.assertEqual(path.read_bytes(),before)
    def test_missing_catalog_model_cannot_materialize(self):
        from formal_gates_harness.native_zcode import runtime_model
        with self.assertRaisesRegex(RuntimeBlocked,'catalog entry'):
            runtime_model({'modelCatalog':{'providers':[]}}, {'providerId':'gone','modelId':'M'},'high')



    def test_missing_session_separate_from_model_error(self):
        for code,text,cls in ((-32004,'Session not found: old',SessionUnavailable),(-32031,'Model unavailable',RuntimeBlocked)):
            c=object.__new__(Client);c.counter=0;c.send=Mock();c.receive=Mock(return_value={'id':'native-1','error':{'code':code,'message':text}})
            with self.subTest(code=code),self.assertRaises(cls) as e:c.request('session/resume',{'sessionId':'old'})
            if cls is RuntimeBlocked:self.assertNotIsInstance(e.exception,SessionUnavailable)

if __name__=='__main__':unittest.main()
