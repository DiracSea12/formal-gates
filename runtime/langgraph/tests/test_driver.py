"""Offline regression for the development driver. No native model calls."""
from __future__ import annotations
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch
from langgraph.checkpoint.sqlite import SqliteSaver
from langgraph.types import Command
from formal_gates_dogfood.graph import Flow
from formal_gates_dogfood.infra import Bridge, DogfoodError, RuntimeBlocked, Store, atomic, digest, git, read, run_lock
from formal_gates_dogfood.native_dsh import observe

PACKAGE=Path(__file__).resolve().parents[3]
BINARY=PACKAGE/'.dogfood/bin/formal-gates'

class DriverTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.root=Path(self.temp.name)
        self.project=self.root/'project';self.project.mkdir();git(self.project,'init','--quiet')
        (self.project/'calculator.py').write_text('def add(a,b):\n    raise NotImplementedError\n')
        (self.project/'untouched.py').write_text('def label():\n    return "unchanged"\n')
        (self.project/'check_add.py').write_text('from calculator import add\nassert add(2,3)==5\n')
        (self.project/'check_untouched.py').write_text('from untouched import label\nassert label()=="unchanged"\n')
        git(self.project,'add','.')
        git(self.project,'-c','user.name=Tests','-c','user.email=tests@localhost','-c','commit.gpgsign=false','commit','--quiet','-m','Baseline')
        self.base=git(self.project,'rev-parse','HEAD');body=b'Implement add. Preserve label. Do not change verification scripts.'
        checks=[{'id':'addition','argv':[sys.executable,'check_add.py'],'dependencies':['calculator.py','check_add.py']},
                {'id':'unchanged','argv':[sys.executable,'check_untouched.py'],'dependencies':['untouched.py','check_untouched.py']}]
        self.bridge=Bridge(str(BINARY));spec={'version':1,'project':str(self.project),'base':self.base,
            'requirement_digest':hashlib.sha256(body).hexdigest(),'checks':checks,'gates':['complexity-gate','implementation-quality-gate'],
            'provider':'fixture','model':'','reasoning_effort':'','repair_limit':2}
        validated=self.bridge.call('validate-spec',spec)
        self.directory=self.root/'run';self.directory.mkdir();(self.directory/'requirement.md').write_bytes(body)
        self.manifest={**validated,'run_id':'test','fixture':True,'profile':'dogfood-v1'}
        self.store=Store(self.directory,self.manifest,self.bridge)
        self.config={'configurable':{'thread_id':'test'}}
    def tearDown(self):self.temp.cleanup()
    def test_complete_failure_repair_and_inheritance(self):
        with SqliteSaver.from_conn_string(str(self.directory/'checkpoints.sqlite')) as saver:
            app=Flow(self.store,PACKAGE).build(saver)
            state=app.invoke({'run_id':'test','round':0},self.config,durability='sync')
            self.assertEqual(state['__interrupt__'][0].value['kind'],'requirement')
            for expected in ['plan',None]:
                state=app.invoke(Command(resume={'decision':'approve','revision':self.manifest['revision']}),self.config,durability='sync')
                if expected:self.assertEqual(state['__interrupt__'][0].value['kind'],expected)
            result=self.store.get(state['terminal'])
            self.assertEqual(result['status'],'DEMO_COMPLETE');self.assertFalse(result['full_3_5c_equivalence'])
            self.assertEqual(state['round'],1)
            events=[json.loads(line) for line in (self.directory/'events.jsonl').read_text().splitlines()]
            self.assertEqual(len([e for e in events if e['event']=='check-complete' and e['check']=='addition']),2)
            self.assertEqual(len([e for e in events if e['event']=='check-complete' and e['check']=='unchanged']),1)
            self.assertTrue(any(e['event']=='check-retained' and e['check']=='unchanged' for e in events))
            before=len(events);app.invoke(None,self.config,durability='sync')
            self.assertEqual(len((self.directory/'events.jsonl').read_text().splitlines()),before)
            self.assertEqual(git(self.project,'rev-parse','HEAD'),self.base)
            self.assertEqual(git(self.project,'status','--porcelain'),'')
    def test_confirmation_survives_reopen_and_rejects_wrong_revision(self):
        path=str(self.directory/'checkpoints.sqlite')
        with SqliteSaver.from_conn_string(path) as saver:
            app=Flow(self.store,PACKAGE).build(saver)
            out=app.invoke({'run_id':'test'},self.config,durability='sync')
            self.assertIn('__interrupt__',out)
        with SqliteSaver.from_conn_string(path) as saver:
            app=Flow(self.store,PACKAGE).build(saver)
            with self.assertRaises(DogfoodError):app.invoke(Command(resume={'decision':'approve','revision':'stale'}),self.config)
            self.assertFalse((self.directory/'workspaces/development').exists())
    def test_abort_never_runs_workers(self):
        with SqliteSaver.from_conn_string(str(self.directory/'checkpoints.sqlite')) as saver:
            app=Flow(self.store,PACKAGE).build(saver);app.invoke({'run_id':'test'},self.config)
            state=app.invoke(Command(resume={'decision':'abort','revision':self.manifest['revision']}),self.config)
            self.assertTrue(state['stop_reason']);self.assertNotIn('terminal',state)
            self.assertFalse((self.directory/'workers').exists())
    def test_artifact_digest_and_duplicate_json(self):
        ref=self.store.put({'value':1});self.assertEqual(self.store.get(ref),{'value':1})
        (self.directory/'artifacts'/f'{ref}.json').write_text('{"value":2}')
        with self.assertRaises(DogfoodError):self.store.get(ref)
        path=self.root/'duplicate.json';path.write_text('{"role":"review","role":"write"}')
        with self.assertRaises(DogfoodError):read(path)
    def test_one_runner_per_run(self):
        with run_lock(self.directory):
            with self.assertRaises(DogfoodError):
                with run_lock(self.directory):pass
    def test_unknown_job_is_not_relaunched(self):
        key='f'*64;directory=self.directory/'jobs'/key;directory.mkdir(parents=True)
        request={'kind':'check','argv':[sys.executable,'-c','raise Exception("must not launch")'],'cwd':str(self.root),'stdin':'','environment':{}}
        atomic(directory/'request.json',request)
        with patch('formal_gates_dogfood.infra.time.monotonic',side_effect=[0,11]):
            with self.assertRaises(RuntimeBlocked):self.store.job(key,request)
        self.assertFalse((directory/'process.json').exists())

class NativeEvidenceTests(unittest.TestCase):
    def case(self,reason='completed',answer='{}',ended=True):
        temp=tempfile.TemporaryDirectory();path=Path(temp.name)
        atomic(path/'native-source.json',{'sessionId':'native','turn':1})
        events=[{'sessionId':'native','event':{'type':'assistant/message','data':{'turn':1,'step':0,'usage':{'inputTokens':12},'message':{'content':[{'type':'text','text':answer}]}}}}]
        if ended:events.append({'sessionId':'native','event':{'type':'turn/end','data':{'turn':1,'reason':{'kind':reason}}}})
        (path/'native-events.jsonl').write_text(''.join(json.dumps(e)+'\n' for e in events))
        return temp,path
    def test_end_turn_is_not_enough(self):
        for reason in ('aborted','blocked','errored'):
            temp,path=self.case(reason=reason)
            try:
                with self.assertRaises(RuntimeBlocked):observe(path,'native')
            finally:temp.cleanup()
    def test_missing_native_end_rejected(self):
        temp,path=self.case(ended=False)
        try:
            with self.assertRaises(RuntimeBlocked):observe(path,'native')
        finally:temp.cleanup()
    def test_native_json_and_usage(self):
        temp,path=self.case(answer='{"status":"PASS"}')
        try:
            result=observe(path,'native');self.assertEqual(result['usage_evidence'][0]['usage']['inputTokens'],12)
            self.assertEqual(read(path/'answer.json'),{'status':'PASS'})
        finally:temp.cleanup()
    def test_session_mismatch_rejected(self):
        temp,path=self.case()
        try:
            with self.assertRaises(RuntimeBlocked):observe(path,'another')
        finally:temp.cleanup()

@unittest.skipIf(os.name=='nt','POSIX controller-loss test; Windows is not claimed verified')
class RecoveryTests(unittest.TestCase):
    def test_controller_exit_does_not_repeat_completed_side_effect(self):
        with tempfile.TemporaryDirectory() as name:
            root=Path(name);key='1'*64;effect=root/'effects.txt'
            payload="import time; from pathlib import Path; time.sleep(.8); p=Path(%r); p.write_text(p.read_text()+'effect\\n' if p.exists() else 'effect\\n')"%str(effect)
            request={'kind':'check','argv':[sys.executable,'-c',payload],'cwd':str(root),'stdin':'','environment':{}}
            code='from pathlib import Path; from formal_gates_dogfood.infra import Store,Bridge; Store(Path(%r),{},Bridge(%r)).job(%r,%r)'%(str(root),str(BINARY),key,request)
            with (root/'controller.stderr').open('w') as err:
                child=subprocess.Popen([sys.executable,'-c',code],stdout=subprocess.DEVNULL,stderr=err)
                process=root/'jobs'/key/'process.json'
                deadline=time.monotonic()+15
                while not process.exists() and child.poll() is None and time.monotonic()<deadline:time.sleep(.05)
                self.assertTrue(process.exists(),'job supervisor never started')
                child.kill();child.wait(timeout=5)
                result=Store(root,{},Bridge(str(BINARY))).job(key,request)
            self.assertEqual(result['exit_code'],0);self.assertEqual(effect.read_text().splitlines(),['effect'])
            result2=Store(root,{},Bridge(str(BINARY))).job(key,request)
            self.assertEqual(result['operation'],result2['operation']);self.assertEqual(effect.read_text().splitlines(),['effect'])

if __name__=='__main__':unittest.main()
