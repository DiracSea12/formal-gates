"""Opt-in real DSH partial-role restart through the actual Flow/run_graph path.

Other roles are explicit fixtures to isolate the interrupted role; a test-only
interrupt stops before product completion. This is NOT full independent QA.
Run: python native_partial_takeover.py development|review --out NEW_DIRECTORY
"""
from __future__ import annotations
import argparse, hashlib, json, os, signal, subprocess, sys, time
from pathlib import Path
from unittest.mock import patch
ROOT = Path(__file__).resolve().parents[3]
os.environ.update(FORMAL_GATES_HARNESS_ROOT=str(ROOT), FORMAL_GATES_HARNESS_BINARY=str(ROOT/'.harness/bin/formal-gates'), PYTHONPATH=str(ROOT/'runtime/langgraph'), LANGSMITH_TRACING='false', LANGCHAIN_TRACING_V2='false')
sys.path.insert(0, str(ROOT/'runtime/langgraph'))
from langgraph.types import interrupt
from formal_gates_harness.cli import make_run, run_graph
from formal_gates_harness.graph import Flow
from formal_gates_harness.workers import Workers, ROLES
from formal_gates_harness.infra import atomic, read, git, file_proof


def fixture(root: Path, role: str):
    root.mkdir(parents=True, exist_ok=False);project=root/'project';project.mkdir();git(project,'init','--quiet')
    if role=='development':
        a='def add(a, b):\n    raise NotImplementedError\n';b='def multiply(a, b):\n    raise NotImplementedError\n'
        req='Implement alpha.add(a,b) as integer addition and beta.multiply(a,b) as integer multiplication. Do not add dependencies or change other APIs/files. Preserve completed work when continuing an interruption.'
        checks=[{'id':'alpha','argv':[sys.executable,'-B','-c','from alpha import add;assert add(2,3)==5;assert add(-2,3)==1'],'dependencies':['alpha.py']}, {'id':'beta','argv':[sys.executable,'-B','-c','from beta import multiply;assert multiply(3,4)==12;assert multiply(-2,3)==-6'],'dependencies':['beta.py']}]
    else:
        a='def magnitude(x):\n    return x\n';b='def clamp(x, low, high):\n    return max(low, x)\n'
        req='Review alpha.magnitude(x), required to return absolute magnitude, and beta.clamp(x,low,high), required to clamp to inclusive low/high (assume low<=high). Report concrete violations with reproductions. Do not edit source. The development precondition is an explicitly labelled test fixture, not a claim these functions are correct.'
        checks=[{'id':'alpha','argv':[sys.executable,'-B','-c','from alpha import magnitude;assert magnitude(-3)==3'],'dependencies':['alpha.py']}, {'id':'beta','argv':[sys.executable,'-B','-c','from beta import clamp;assert clamp(15,0,10)==10'],'dependencies':['beta.py']}]
    (project/'alpha.py').write_text(a);(project/'beta.py').write_text(b);(project/'.gitignore').write_text('__pycache__/\n')
    git(project,'add','.');git(project,'-c','user.name=Interrupted Role Test','-c','user.email=test@localhost','-c','commit.gpgsign=false','commit','--quiet','-m','Isolated role test baseline')
    requirement=root/'requirement.md';requirement.write_text(req)
    run=make_run(ROOT,project,requirement,checks,['implementation-quality-gate'],provider='dsh')
    atomic(root/'test.json',{'role':role,'run':str(run),'source':str(project),'initial':{n:file_proof(project/n) for n in ('alpha.py','beta.py')},'base':git(project,'rev-parse','HEAD'),'other_roles':'fixture','fault':'kill actual DSH executor and descendants; supervisor stays to record actual failed exit'})
    return run


def run_phase(root: Path, role: str, resume: bool):
    test=read(root/'test.json');run=Path(test['run']);target='development' if role=='development' else 'implementation-quality-gate'
    original=Workers.run
    def selected(self, task, workspace, key_data, feedback=None, session='', attempt=0, **options):
        if task==target:return original(self,task,workspace,key_data,feedback,session,attempt,**options)
        result={'status':'PASS','summary':'Explicit non-target role fixture; no model review claimed.','findings':[], 'check_ids':[c['id'] for c in self.spec['checks']] if task in ('qa-design','qa-review') else []}
        return {'role':task,'result':result,'needs_repair':False,'source':{'provider':'fixture','identity':'test-'+task,'model_calls':0},'revision':self.store.manifest['revision'],'input':key_data}
    marker='ROLE_PARTIAL_DEV' if role=='development' else 'ROLE_PARTIAL_REVIEW'
    steps=('First implement alpha.add, leaving beta.py unchanged. Then run bash `printf "'+marker+'\\n"; sleep 120`. Only after the wait implement beta.multiply.' if role=='development' else 'First inspect alpha.py and state a concrete finding with a reproducing input in a normal assistant text message. Then run bash `printf "'+marker+'\\n"; sleep 120`. Only after the wait inspect beta.py and give the full review.')
    instruction=ROLES[target]+' This is an authorized interruption test. '+steps+' This synchronization is only for the FIRST session: if handover.json is provided, do NOT repeat the wait; retain prior work/findings, verify them, and complete remaining scope.'
    def observed(self,state):
        interrupt({'kind':'test-role-complete','revision':self.revision,'choices':[], 'note':'Harness stops here; no full workflow completion is claimed'})
        return {}
    stop_method='candidate' if role=='development' else 'evaluate'
    with patch.object(Workers,'run',selected), patch.dict(ROLES,{target:instruction}), patch.object(Flow,stop_method,observed):
        state=run_graph(run,advance=False) if resume else run_graph(run)
        if resume:
            pending=state['interrupts'];assert len(pending)==1 and pending[0]['request']['kind']=='runtime',state
            assert pending[0]['request']['role']==target,state
            state=run_graph(run,{'decision':'restart-session','revision':state['revision']},pending[0]['id'])
        else:
            while state['interrupts'] and state['interrupts'][0]['request']['kind'] in ('requirement','plan'):
                state=run_graph(run,{'decision':'approve','revision':state['revision']},state['interrupts'][0]['id'])
    atomic(root/('after-restart.json' if resume else 'after-interruption.json'),state)
    print(json.dumps({'pid':os.getpid(),'status':state['status'],'interrupts':state['interrupts']},ensure_ascii=False),flush=True)


def descendants(pid: int):
    rows=[]
    for line in subprocess.check_output(['ps','-axo','pid=,ppid='],text=True).splitlines():
        try:a,b=map(int,line.split());rows.append((a,b))
        except ValueError:pass
    owned={pid}
    while True:
        next_owned=owned|{a for a,b in rows if b in owned}
        if next_owned==owned:return owned
        owned=next_owned


def parent(root: Path, role: str):
    run=fixture(root,role);target='development' if role=='development' else 'implementation-quality-gate'
    args=[sys.executable,__file__,role,'--out',str(root),'--phase']
    with (root/'first.stdout').open('wb') as out,(root/'first.stderr').open('wb') as err:
        old=subprocess.Popen(args+['start'],stdout=out,stderr=err,env=os.environ.copy())
        deadline=time.monotonic()+420;job=None
        while time.monotonic()<deadline and old.poll() is None:
            events=run/'events.jsonl'
            if events.exists():
                for line in events.read_text().splitlines():
                    e=json.loads(line)
                    if e['event']=='worker-start' and e['role']==target:
                        possible=run/'jobs'/e['operation'];trace=possible/'protocol.jsonl'
                        if trace.exists() and (possible/'native-process.json').exists():
                            marker='ROLE_PARTIAL_DEV' if role=='development' else 'ROLE_PARTIAL_REVIEW'
                            if any(marker in l and ('tool_call' in l or 'toolCall' in l) for l in trace.read_text(errors='replace').splitlines()):job=possible
            if job:break
            time.sleep(.2)
        if not job:
            if old.poll() is None:old.terminate()
            raise AssertionError('Native partial boundary not observed; inspect first.stderr, not counted as a pass')
        native=read(job/'native-process.json');identity=read(job/'identity.json');workspace=Path(identity['workspace'])
        before={n:file_proof(workspace/n) for n in ('alpha.py','beta.py')}
        native_events=(job/'native-events.jsonl').read_text()
        text='\n'.join(p.get('text','') for l in native_events.splitlines() for p in json.loads(l).get('event',{}).get('data',{}).get('message',{}).get('content',[]) if p.get('type')=='text')
        if role=='development':
            initial=read(root/'test.json')['initial'];assert before['alpha.py']!=initial['alpha.py'] and before['beta.py']==initial['beta.py']
        else:
            assert 'alpha' in text.lower(), 'Partial finding was not durably recorded'
        assert not (job/'answer.json').exists()
        owned=descendants(native['pid'])
        for pid in sorted(owned,reverse=True):
            try:os.kill(pid,signal.SIGKILL)
            except ProcessLookupError:pass
        old.wait(timeout=60)
        failure=read(job/'exit.json');assert failure['exit_code']!=0
        atomic(root/'fault.json',{'native_processes_killed':sorted(owned),'previous_identity':identity['identity'],'job':str(job),'workspace':str(workspace),'before':before,'prior_public_text':text,'exit':failure,'controller_pid':old.pid,'final_output_absent_at_kill':True})
    with (root/'restart.stdout').open('wb') as out,(root/'restart.stderr').open('wb') as err:
        fresh=subprocess.run(args+['resume'],stdout=out,stderr=err,env=os.environ.copy(),timeout=420)
    assert fresh.returncode==0,'Restart did not finish; inspect restart.stderr'
    after=read(root/'after-restart.json');assert after['interrupts'][0]['request']['kind']=='test-role-complete',after
    records=[]
    for receipt in (run/'workers').glob('*.json'):
        value=read(run/'artifacts'/(read(receipt)['ref']+'.json'))
        if value.get('role')==target:records.append(value)
    assert len(records)==1,records
    value=records[0];source=value['source'];assert source['identity']!=identity['identity']
    assert source['handover']['previous_identity']==identity['identity']
    request=read(run/'jobs'/source['job']/'native-request.json');assert request['identity']==''
    if role=='development':
        assert file_proof(workspace/'alpha.py')==before['alpha.py'],'Completed file rewritten'
        code='from alpha import add;from beta import multiply;assert add(2,3)==5;assert add(-2,3)==1;assert multiply(-2,3)==-6;assert multiply(3,4)==12'
        assert value['result']['status']=='PASS'
    else:
        assert all(file_proof(workspace/n)==proof for n,proof in before.items())
        paths=[p for f in value['result']['findings'] for p in f['paths']]
        assert value['result']['status']=='FAIL' and any('alpha.py' in p for p in paths) and any('beta.py' in p for p in paths),value
        code='from alpha import magnitude;from beta import clamp;assert magnitude(-3)!=3;assert clamp(15,0,10)!=10'
    check=subprocess.run([sys.executable,'-B','-c',code],cwd=workspace,capture_output=True,text=True);assert check.returncode==0,check.stderr
    report={'test_passed':True,'role':role,'run':str(run),'old_identity':identity['identity'],'new_identity':source['identity'],'new_session_not_resumed':True,'old_executor_killed':True,'partial_work_preserved':True,'same_run':after['run_id']==read(run/'manifest.json')['run_id'],'result_status':value['result']['status'],'handover_generated_by_product':True,'independent_assertions_passed':True,'scope':'actual Flow/run_graph and native target role; other roles are fixtures; harness stops before workflow completion'}
    atomic(root/'report.json',report);print(json.dumps(report,ensure_ascii=False),flush=True)

if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('role',choices=['development','review']);parser.add_argument('--out',type=Path,required=True);parser.add_argument('--phase',choices=['start','resume']);a=parser.parse_args()
    if a.phase:run_phase(a.out.resolve(),a.role,a.phase=='resume')
    else:parent(a.out.resolve(),a.role)
