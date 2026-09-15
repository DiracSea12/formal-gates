"""Development-only CLI; all writes to workflow progress go through LangGraph."""
from __future__ import annotations
import argparse
import contextlib
import sqlite3
import hashlib
import importlib.metadata
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid
from langgraph.checkpoint.sqlite import SqliteSaver
from langgraph.types import Command
from .graph import Flow
from .infra import Bridge, DogfoodError, RuntimeBlocked, Store, atomic, git, read, run_lock, runtime_identity
from .native_dsh import execute as native_execute

BASELINE='517de91f1b480ec969bd91385c29ce38d8bc2841'

def package_root():
    value=os.environ.get('FORMAL_GATES_DOGFOOD_ROOT')
    if not value: raise DogfoodError('Use scripts/dogfood, or set FORMAL_GATES_DOGFOOD_ROOT')
    return Path(value).resolve()
def binary_path():
    value=os.environ.get('FORMAL_GATES_DOGFOOD_BINARY')
    if not value: raise DogfoodError('Go driver binary is not bound')
    return str(Path(value).resolve())
def doctor(root: Path):
    path=root/'.dogfood'/'probes'/uuid.uuid4().hex
    path.mkdir(parents=True)
    result=native_execute({'workspace':str(path),'access':'read-only'},path,probe=True)
    versions={name:importlib.metadata.version(name) for name in ['langgraph','langgraph-checkpoint','langgraph-checkpoint-sqlite']}
    report={'ok':True,'provider':result,'dependencies':versions,'evidence':str(path),'model_calls':0}
    atomic(path/'report.json',report);return report

def make_run(root: Path,project: Path,requirement: Path,checks: list,gates: list,provider='dsh',
             model='',effort='',repair_limit=2,run_id='',fixture=False,selected=None):
    project=project.resolve();requirement=requirement.resolve()
    if not (project/'.git').exists(): raise DogfoodError('A local Git repository is required')
    if git(project,'rev-parse','--show-toplevel')!=str(project): raise DogfoodError('Use the repository root')
    # The frozen base is committed content; do not silently discard local edits.
    if git(project,'status','--porcelain','--untracked-files=normal'):
        raise DogfoodError('Commit or separately preserve project changes before start; source project will not be modified')
    body=requirement.read_bytes()
    if not body.strip(): raise DogfoodError('The requirement file is empty')
    if len(body)>2*1024*1024: raise DogfoodError('Requirement exceeds 2 MiB')
    bridge=Bridge(binary_path())
    base=bridge.call('resolve-snapshot',{'root':str(project)})['candidate']
    if provider=='dsh':
        selected=selected or doctor(root)['provider']['selection']
        model=model or selected.get('model','');effort=effort or selected.get('reasoning_effort','')
        if not model: raise DogfoodError('Native DSH did not advertise a selected model')
    spec={'version':1,'project':str(project),'base':base,'requirement_digest':hashlib.sha256(body).hexdigest(),
          'checks':checks,'gates':gates,'provider':provider,'model':model,'reasoning_effort':effort,'repair_limit':repair_limit}
    validated=bridge.call('validate-spec',spec)
    run_id=run_id or time.strftime('dogfood-%Y%m%d-%H%M%S-')+uuid.uuid4().hex[:8]
    if not run_id or len(run_id)>96 or any(c not in 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_' for c in run_id):
        raise DogfoodError('Invalid run id')
    directory=root/'.dogfood'/'runs'/run_id
    directory.mkdir(parents=True,exist_ok=False)
    manifest={'run_id':run_id,'profile':'dogfood-v1','baseline':BASELINE,'created_at':time.time(),
              **validated,'runtime_identity':runtime_identity(root,binary_path()),'fixture':fixture}
    policy = (root / 'prompts/reviewer-base.md').read_bytes()
    manifest['reviewer_policy_digest'] = hashlib.sha256(policy).hexdigest()
    (directory / 'reviewer-base.md').write_bytes(policy)
    (directory/'requirement.md').write_bytes(body);atomic(directory/'manifest.json',manifest)
    return directory

def open_run(path: str | Path):
    root=package_root();directory=Path(path).resolve()
    manifest=read(directory/'manifest.json')
    if manifest.get('profile')!='dogfood-v1': raise DogfoodError('Not a dogfood-v1 run; legacy state is not supported')
    current_identity = runtime_identity(root,binary_path())
    if manifest.get('runtime_identity') != current_identity:
        receipt_path = directory / 'transport-adoption.json'
        receipt = read(receipt_path) if receipt_path.exists() else {}
        if (receipt.get('from_runtime') != manifest.get('runtime_identity') or
            receipt.get('to_runtime') != current_identity or receipt.get('revision') != manifest['revision']):
            raise DogfoodError('RUNTIME_CHANGED: use the bound runtime; transport fixes require explicit adopt-transport')
    if hashlib.sha256((directory/'requirement.md').read_bytes()).hexdigest()!=manifest['spec']['requirement_digest']:
        raise DogfoodError('Requirement bytes changed after confirmation binding')
    if manifest.get('reviewer_policy_digest') != hashlib.sha256((directory/'reviewer-base.md').read_bytes()).hexdigest():
        raise DogfoodError('Bound reviewer policy changed or is missing')
    bridge=Bridge(binary_path());checked=bridge.call('validate-spec',manifest['spec'])
    if checked['revision']!=manifest['revision']:raise DogfoodError('Spec revision does not match manifest')
    return root,Store(directory,manifest,bridge)


def adopt_transport(directory: Path, snapshot: Path, before: str, after: str, reason: str):
    """Explicit dev-only adoption, not a graph/state migration or a model tool.

    Exact runtime identities and a preserved source snapshot are required. The
    graph, persistence, Go binary, dependency lock and other runtime files must
    be byte-identical. The caller reviews the listed transport changes.
    """
    root = package_root(); binary = binary_path(); directory = directory.resolve(); snapshot = snapshot.resolve()
    if not reason.strip(): raise DogfoodError('Transport adoption requires a reason')
    with run_lock(directory):
        manifest = read(directory / 'manifest.json')
        if manifest.get('profile') != 'dogfood-v1' or manifest.get('runtime_identity') != before:
            raise DogfoodError('Original runtime identity/profile mismatch')
        if runtime_identity(snapshot, str(snapshot / '.dogfood/bin/formal-gates')) != before:
            raise DogfoodError('Preserved runtime snapshot does not match the original')
        if runtime_identity(root, binary) != after:
            raise DogfoodError('Reviewed target runtime identity does not match current files')
        def inventory(base):
            paths = [p for p in (base / 'runtime/langgraph/formal_gates_dogfood').glob('*')
                     if p.is_file() and p.suffix in ('.py', '.mjs')]
            paths += [base / 'runtime/langgraph/requirements.lock', base / '.dogfood/bin/formal-gates', base / 'prompts/reviewer-base.md']
            return {str(p.relative_to(base)): hashlib.sha256(p.read_bytes()).hexdigest() for p in paths}
        old, new = inventory(snapshot), inventory(root)
        allowed = {'runtime/langgraph/formal_gates_dogfood/' + p for p in ('native_dsh.py', 'workers.py', 'cli.py')}
        changed = {p for p in old.keys() | new.keys() if old.get(p) != new.get(p)}
        if old.keys() != new.keys() or not changed or not changed <= allowed:
            raise DogfoodError('Adoption is limited to reviewed transport/CLI fixes; graph, Go and dependencies must be unchanged')
        if any(not (p.parent / 'exit.json').exists() for p in (directory / 'jobs').glob('*/request.json')):
            raise DogfoodError('Cannot adopt while any native job is active or unresolved')
        bridge = Bridge(binary)
        if (hashlib.sha256((directory / 'requirement.md').read_bytes()).hexdigest() != manifest['spec']['requirement_digest'] or
            bridge.call('validate-spec', manifest['spec'])['revision'] != manifest['revision']):
            raise DogfoodError('Business inputs changed')
        # Checkpoint open/inspection, not an invoke or a state write.
        store = Store(directory, manifest, bridge)
        with SqliteSaver.from_conn_string(str(directory / 'checkpoints.sqlite')) as saver:
            app = Flow(store, root).build(saver)
            current = status_projection(store, app, {'configurable': {'thread_id': manifest['run_id']}})
            if current['status'] != 'WAITING_USER' or any(p['request']['kind'] != 'runtime' for p in current['interrupts']):
                raise DogfoodError('Adoption requires a runtime-only paused run')
        receipt = {'kind': 'transport-fix-adoption', 'from_runtime': before, 'to_runtime': after,
                   'revision': manifest['revision'], 'reason': reason, 'snapshot': str(snapshot),
                   'changed': {p: {'before': old[p], 'after': new[p]} for p in sorted(changed)}}
        target = directory / 'transport-adoption.json'
        if target.exists() and read(target) != receipt:
            raise DogfoodError('Different transport adoption already recorded')
        atomic(target, receipt)
        return {'status': 'TRANSPORT_ADOPTED', 'receipt': str(target), 'changes': sorted(changed),
                'state_migrated': False, 'model_calls': 0}

def status_projection(store,app,config):
    snapshot=app.get_state(config,subgraphs=True);state=snapshot.values or {};pending=[]
    def visit(snap):
        for task in snap.tasks:
            for item in task.interrupts:
                if not any(p['id']==item.id for p in pending):pending.append({'id':item.id,'request':item.value})
            if hasattr(task.state,'tasks'):visit(task.state)
    visit(snapshot)
    terminal=store.get(state['terminal']) if state.get('terminal') else None
    failure_path=store.directory/'runner-error.json'
    return {'run_id':store.manifest['run_id'],'directory':str(store.directory),
            'profile':'dogfood-v1','revision':store.manifest['revision'],'candidate':state.get('candidate'),
            'status':terminal['status'] if terminal else ('ABORTED' if state.get('stop_reason') else ('WAITING_USER' if pending else 'PENDING')),
            'round':state.get('round',0),'next':list(snapshot.next),'interrupts':pending,
            'terminal':terminal,'stop_reason':state.get('stop_reason',''),
            'last_runner_error':read(failure_path) if failure_path.exists() else None}

@contextlib.contextmanager
def read_checkpointer(path: Path):
    # Uses the pinned SqliteSaver serializer, not a second state implementation.
    # No schema creation or write lock for a progress query.
    connection = None
    try:
        connection = sqlite3.connect(path.resolve().as_uri() + '?mode=ro', uri=True,
                                     check_same_thread=False, timeout=5)
        connection.execute('PRAGMA query_only=ON')
        tables = {r[0] for r in connection.execute("SELECT name FROM sqlite_master WHERE type='table'")}
        if not {'checkpoints', 'writes'} <= tables:
            raise DogfoodError(
                'CHECKPOINT_NOT_INITIALIZED: checkpoint tables are not initialized. '
                'show is read-only; use resume on this same run to continue an interrupted startup.')
        connection.execute('BEGIN')
        saver = SqliteSaver(connection)
        saver.is_setup = True  # pinned sqlite checkpointer 3.1.1; setup otherwise writes DDL
        yield saver
    except sqlite3.Error as exc:
        # Missing first checkpoint is a recoverable startup gap, not an excuse
        # for a read command to initialize or recreate authoritative state.
        raise DogfoodError(
            f'CHECKPOINT_READ_ERROR: cannot read {path}: {exc}. '
            'show did not advance the run. If startup stopped before its first '
            'checkpoint, use resume on this same run; do not recreate it.') from exc
    finally:
        if connection is not None:
            try:
                connection.rollback()
            finally:
                connection.close()

def run_graph(directory: str | Path,answer=None,interrupt_id='',advance=True):
    root,store=open_run(directory);config={'configurable':{'thread_id':store.manifest['run_id']},'recursion_limit':10000}
    if answer is not None and not interrupt_id:
        raise DogfoodError('An exact interrupt ID is required; a revision alone cannot identify a user decision')
    if not advance:
        with read_checkpointer(store.directory/'checkpoints.sqlite') as saver:
            return status_projection(store, Flow(store,root).build(saver), config)
    with run_lock(store.directory):
        with SqliteSaver.from_conn_string(str(store.directory/'checkpoints.sqlite')) as saver:
            app=Flow(store,root).build(saver)
            current=status_projection(store,app,config)
            if not advance or current['status'] in ('DOGFOOD_COMPLETE','DEMO_COMPLETE','ABORTED'):return current
            pending=current['interrupts']
            if pending:
                if answer is None:return current
                if not isinstance(answer,dict) or set(answer)!={'decision','revision'}:
                    raise DogfoodError('Answer accepts only decision and revision; state/goto patches are not allowed')
                chosen=[p for p in pending if p['id']==interrupt_id]
                if len(chosen)!=1:raise DogfoodError('Select exactly one current interrupt id')
                item=chosen[0]
                if answer['revision']!=item['request']['revision'] or answer['decision'] not in item['request']['choices']:
                    raise DogfoodError('Stale revision or invalid decision; run was not advanced')
                value=Command(resume={item['id']:answer})
            else:
                if answer is not None:raise DogfoodError('No pending user decision')
                value=None if app.get_state(config).values else {'run_id':store.manifest['run_id'],'round':0,'round_allowance':store.manifest['spec']['repair_limit']}
            try:
                app.invoke(value,config,durability='sync')
                (store.directory/'runner-error.json').unlink(missing_ok=True)
            except KeyboardInterrupt:
                # Worker supervisors survive controller loss and preserve exact job receipts.
                store.event('runner-interrupted',reason='Controller stopped; use resume to reattach. Use cancel for explicit task cancellation.')
                raise
            except Exception as exc:
                atomic(store.directory/'runner-error.json',{'error':f'{type(exc).__name__}: {exc}','at':time.time()})
                raise
            return status_projection(store,app,config)

def make_demo(root: Path,native=False):
    directory=root/'.dogfood'/'samples'/(('native-' if native else 'fixture-')+uuid.uuid4().hex[:12]);directory.mkdir(parents=True)
    project=directory/'project';project.mkdir()
    subprocess.run(['git','init','--quiet',str(project)],check=True)
    (project/'.gitignore').write_text('__pycache__/\n')
    (project/'calculator.py').write_text('def add(a, b):\n    raise NotImplementedError\n')
    (project/'untouched.py').write_text('def label():\n    return "unchanged"\n')
    (project/'check_add.py').write_text('from calculator import add\nassert add(2,3)==5\nassert add(-2,3)==1\nassert add(0,0)==0\nassert add(2,-3)==-1\nprint("add checks passed")\n')
    (project/'check_untouched.py').write_text('from untouched import label\nassert label()=="unchanged"\nprint("unchanged checks passed")\n')
    requirement=directory/'requirement.md'
    requirement.write_text('# Add integer addition\n\nImplement calculator.add(a,b) returning the sum of two integers, including negative and zero values. '
        'No type coercion or validation is required. Preserve untouched.label() returning "unchanged". '
        'Do not add other APIs, edit the verification files, add dependencies, or change unrelated files.\n\n'
        'Acceptance: check_add.py covers positive, negative and zero inputs; check_untouched.py verifies the unrelated module. '
        'This is a small isolated native-driver validation, not the formal-gates product release.\n')
    git(project,'add','.');git(project,'-c','user.name=Dogfood Test','-c','user.email=dogfood@localhost','-c','commit.gpgsign=false','commit','--quiet','-m','Native dogfood sample baseline')
    checks=[{'id':'addition','argv':[sys.executable,'check_add.py'],'dependencies':['calculator.py','check_add.py']},
            {'id':'unchanged','argv':[sys.executable,'check_untouched.py'],'dependencies':['untouched.py','check_untouched.py']}]
    return make_run(root,project,requirement,checks,['complexity-gate','implementation-quality-gate'],
                    provider='dsh' if native else 'fixture',fixture=not native)

def main():
    parser=argparse.ArgumentParser(prog='formal-gates dogfood',description='Optional LangGraph development driver, not the legacy formal Seal route')
    commands=parser.add_subparsers(dest='command',required=True)
    commands.add_parser('doctor',help='Probe DSH without sending a model prompt')
    adoption = commands.add_parser('adopt-transport', help='Explicit dev-only transport fix; requires preserved runtime and exact identities')
    adoption.add_argument('--run', required=True); adoption.add_argument('--snapshot', required=True)
    adoption.add_argument('--from-runtime', required=True); adoption.add_argument('--to-runtime', required=True)
    adoption.add_argument('--reason', required=True)
    start=commands.add_parser('start');start.add_argument('--project',required=True);start.add_argument('--requirement',required=True);start.add_argument('--checks',required=True)
    start.add_argument('--provider',choices=['dsh'],default='dsh');start.add_argument('--model',default='');start.add_argument('--reasoning-effort',default='')
    start.add_argument('--gates',nargs='+',default=['complexity-gate','implementation-quality-gate']);start.add_argument('--repair-limit',type=int,default=2);start.add_argument('--run-id',default='')
    for name in ('show','resume','respond','cancel'):
        sub=commands.add_parser(name);sub.add_argument('--run',required=True)
        if name=='respond':
            sub.add_argument('--decision',required=True);sub.add_argument('--revision',required=True);sub.add_argument('--interrupt',required=True)
        if name=='cancel':sub.add_argument('--confirm',action='store_true',required=True)
    demo=commands.add_parser('demo',help='Create isolated sample; fixture by default, --native uses real DSH')
    demo.add_argument('--native',action='store_true');demo.add_argument('--approve-sample',action='store_true',help='Approve this command-created sample only; never bypass runtime or review blockers')
    args=parser.parse_args()
    try:
        root=package_root()
        if args.command=='doctor':result=doctor(root)
        elif args.command=='adopt-transport':
            result=adopt_transport(Path(args.run),Path(args.snapshot),args.from_runtime,args.to_runtime,args.reason)
        elif args.command=='start':
            directory=make_run(root,Path(args.project),Path(args.requirement),read(Path(args.checks)),args.gates,args.provider,args.model,args.reasoning_effort,args.repair_limit,args.run_id)
            result=run_graph(directory)
        elif args.command=='demo':
            directory=make_demo(root,args.native);print(json.dumps({'created_run':str(directory)}),flush=True)
            result=run_graph(directory)
            while args.approve_sample and result['interrupts']:
                if len(result['interrupts'])!=1 or result['interrupts'][0]['request']['kind'] not in ('requirement','plan'):break
                result=run_graph(directory,{'decision':'approve','revision':result['revision']},result['interrupts'][0]['id'])
        elif args.command=='cancel':
            _,store=open_run(args.run);store.cancel_jobs();result={'status':'CANCEL_REQUESTED','run_id':store.manifest['run_id'],
                'note':'Only owned child jobs were signalled. Use resume to observe cancelled jobs and resolve their explicit interruption.'}
        else:
            answer={'decision':args.decision,'revision':args.revision} if args.command=='respond' else None
            result=run_graph(args.run,answer,getattr(args,'interrupt',''),advance=args.command!='show')
        print(json.dumps(result,ensure_ascii=False,indent=2));return 0
    except KeyboardInterrupt:return 130
    except (DogfoodError,OSError,ValueError) as exc:
        print(json.dumps({'ok':False,'error':str(exc)},ensure_ascii=False),file=sys.stderr);return 2

if __name__=='__main__':raise SystemExit(main())
