"""Fixed non-happy regressions. Real local Git/processes, no model calls."""
from __future__ import annotations
import hashlib, json, os, tempfile, unittest
from pathlib import Path
from unittest.mock import Mock, patch
import test_driver as fixtures
from langgraph.checkpoint.sqlite import SqliteSaver
from langgraph.graph import StateGraph, START, END
from langgraph.types import Command
from formal_gates_harness.cli import run_graph, open_run
from formal_gates_harness.graph import Flow, State
from formal_gates_harness.workers import Workers
from formal_gates_harness.native_result import decode_result
from formal_gates_harness.handover import prepare_session_restart
from formal_gates_harness.infra import atomic, read, run_lock, runtime_identity, HarnessError, RuntimeBlocked, digest

class NonhappyRegressions(unittest.TestCase):
    def setUp(self):
        fixtures.DriverTests.setUp(self)
        policy=(fixtures.PACKAGE/'prompts/reviewer-base.md').read_bytes()
        (self.directory/'reviewer-base.md').write_bytes(policy)
        self.manifest.update(runtime_identity=runtime_identity(fixtures.PACKAGE,str(fixtures.BINARY)), reviewer_policy_digest=hashlib.sha256(policy).hexdigest())
        atomic(self.directory/'manifest.json',self.manifest)
        env=patch.dict(os.environ,{'FORMAL_GATES_HARNESS_ROOT':str(fixtures.PACKAGE),'FORMAL_GATES_HARNESS_BINARY':str(fixtures.BINARY)})
        env.start();self.addCleanup(env.stop)
    def tearDown(self):fixtures.DriverTests.tearDown(self)
    def approve(self,state):
        return run_graph(self.directory,{'decision':'approve','revision':state['revision']},state['interrupts'][0]['id'])
    def test_old_approval_without_interrupt_cannot_approve_plan(self):
        first=run_graph(self.directory);plan=self.approve(first)
        before=(self.directory/'checkpoints.sqlite').read_bytes()
        with self.assertRaises(HarnessError):
            run_graph(self.directory,{'decision':'approve','revision':first['revision']})
        self.assertEqual((self.directory/'checkpoints.sqlite').read_bytes(),before)
        self.assertEqual(run_graph(self.directory,advance=False)['interrupts'],plan['interrupts'])
    def test_show_works_while_writer_owns_lock_and_does_not_write(self):
        first=run_graph(self.directory);before=(self.directory/'checkpoints.sqlite').read_bytes()
        with run_lock(self.directory):
            shown=run_graph(self.directory,advance=False)
            with self.assertRaises(HarnessError):run_graph(self.directory)
        self.assertEqual(first['interrupts'],shown['interrupts'])
        self.assertEqual(before,(self.directory/'checkpoints.sqlite').read_bytes())
    def completed_check(self):
        flow=Flow(self.store,fixtures.PACKAGE)
        # Exercise a successful independent check, not the intentionally missing add.
        check=self.manifest['spec']['checks'][1]
        state={'run_id':'test','candidate':self.base,'round':0}
        result=flow.one_check(check,state,0)
        state['checks']={check['id']:self.store.put(result)}
        evidence=self.store.get(result['evidence'])
        return flow,check,state,Path(evidence['process']['directory'])
    def test_missing_stdout_cannot_reuse_pass(self):
        flow,check,state,job=self.completed_check();(job/'stdout.log').unlink()
        with self.assertRaises(RuntimeBlocked):flow.one_check(check,state,0)
    def test_explicit_retry_can_regenerate_lost_check_evidence(self):
        flow,check,state,job=self.completed_check();(job/'stdout.log').unlink()
        original=state['checks'][check['id']]
        with self.assertRaises(RuntimeBlocked):flow.one_check(check,state,0)
        result=flow.one_check(check,state,1)
        self.assertEqual(result['status'],'PASS')
        self.store.verify_evidence(result['evidence'])
        self.assertNotEqual(result['evidence'],self.store.get(original)['evidence'])
        self.assertFalse((job/'stdout.log').exists())
    def test_changed_stderr_cannot_reuse_pass(self):
        flow,check,state,job=self.completed_check();(job/'stderr.log').write_text('changed')
        with self.assertRaises(RuntimeBlocked):flow.one_check(check,state,0)
    def test_developer_fail_is_not_dropped(self):
        original=Workers.fixture
        def result(worker,role,workspace):
            if role=='development':
                (workspace/'calculator.py').write_text('def add(a,b):\n    return a+b\n')
                return {'status':'FAIL','summary':'Mandatory development work remains (fixture)', 'findings':[{'id':'DEV1','severity':'P1','message':'Remaining obligation','paths':['calculator.py']}],'check_ids':[]}
            return original(worker,role,workspace)
        with patch.object(Workers,'fixture',result):
            plan=self.approve(run_graph(self.directory));stopped=self.approve(plan)
        self.assertEqual(stopped['status'],'WAITING_USER')
        self.assertEqual(stopped['interrupts'][0]['request']['kind'],'development-findings')
        self.assertIsNone(stopped['candidate']);self.assertFalse((self.directory/'harness-result.json').exists())
    def test_run_policy_cannot_change(self):
        (self.directory/'reviewer-base.md').write_text('changed policy')
        with self.assertRaises(HarnessError):open_run(self.directory)
    def test_package_policy_is_in_runtime_identity(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);(root/'runtime/langgraph/formal_gates_harness').mkdir(parents=True);(root/'prompts').mkdir()
            (root/'runtime/langgraph/requirements.lock').write_text('pinned')
            binary=root/'binary';binary.write_bytes(b'binary')
            policy=root/'prompts/reviewer-base.md';policy.write_text('original')
            first=runtime_identity(root,str(binary));policy.write_text('changed')
            self.assertNotEqual(first,runtime_identity(root,str(binary)))
    def prior_job(self,series):
        key=digest({'series':series,'attempt':0})
        request={'kind':'fixture','argv':[os.sys.executable,'-c','print("partial transport fixture")'],'cwd':str(self.project),'stdin':'','environment':{}}
        self.store.job(key,request)
        job=self.directory/'jobs'/key
        atomic(job/'native-request.json',{'workspace':str(self.project)})
        atomic(job/'identity.json',{'identity':'old-native'})
        (job/'native-events.jsonl').write_text('{"partial":"prior finding is not a verdict"}\n')
        return job
    def test_restart_requires_settled_prior_attempt(self):
        with self.assertRaises(RuntimeBlocked):prepare_session_restart(self.store,'series',1,'development',self.project,self.project/'input')
    def test_restart_refuses_live_native_executor(self):
        job=self.prior_job('alive');atomic(job/'native-process.json',{'pid':os.getpid()})
        with self.assertRaises(RuntimeBlocked):prepare_session_restart(self.store,'alive',1,'development',self.project,self.project/'input')
    def test_restart_material_keeps_partial_evidence_not_fake_completion(self):
        job=self.prior_job('stopped');original=(job/'native-events.jsonl').read_bytes()
        bundle=prepare_session_restart(self.store,'stopped',1,'implementation-quality-gate',self.project,self.project/'input')
        self.assertEqual(bundle['previous_identity'],'old-native')
        self.assertTrue(bundle['partial_opinions_are_not_final_results'])
        self.assertEqual((job/'native-events.jsonl').read_bytes(),original)
        self.assertEqual(Path(bundle['files']['native-events.jsonl']['path']).read_bytes(),original)
    def test_restart_replay_keeps_original_partial_input(self):
        self.prior_job('immutable')
        path=self.project/'input'
        first=prepare_session_restart(self.store,'immutable',1,'development',self.project,path)
        raw=(path/'handover.json').read_bytes()
        (self.project/'calculator.py').write_text('def add(a,b):\n    return a+b\n')
        again=prepare_session_restart(self.store,'immutable',1,'development',self.project,path)
        self.assertEqual(first,again)
        self.assertEqual((path/'handover.json').read_bytes(),raw)
    def test_explicit_restart_decision_reaches_worker(self):
        flow=Flow(self.store,fixtures.PACKAGE)
        answer={'status':'PASS','summary':'fixture','findings':[],'check_ids':[]}
        output={'source':{'identity':'new-native'},'result':answer,'needs_repair':False}
        flow.workers=Mock();flow.workers.run.side_effect=[RuntimeBlocked('lost native'),RuntimeBlocked('lost native'),output]
        def node(state):
            ref,result=flow.invoke_worker('development',state,self.project);return {'development':ref}
        graph=StateGraph(State);graph.add_node('role',node);graph.add_edge(START,'role');graph.add_edge('role',END)
        with SqliteSaver.from_conn_string(str(self.directory/'role.sqlite')) as saver:
            app=graph.compile(checkpointer=saver)
            first=app.invoke({'run_id':'test'},self.config)
            self.assertIn('restart-session',first['__interrupt__'][0].value['choices'])
            final=app.invoke(Command(resume={'decision':'restart-session','revision':self.manifest['revision']}),self.config)
        self.assertEqual(self.store.get(final['development'])['source']['identity'],'new-native')
        self.assertTrue(flow.workers.run.call_args.kwargs['restart_session'])
    def test_one_unfenced_result_allowed_but_competing_results_rejected(self):
        result={'status':'PASS','summary':'complete','findings':[],'check_ids':[]}
        self.assertEqual(decode_result('Explanation.\n\n'+json.dumps(result)),result)
        with self.assertRaises(RuntimeBlocked):decode_result('Explanation.\n'+json.dumps(result)+'\n{"status":"FAIL"}')

if __name__=='__main__':unittest.main()
