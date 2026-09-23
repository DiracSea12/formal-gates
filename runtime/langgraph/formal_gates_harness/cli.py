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
from .infra import Bridge, HarnessError, RuntimeBlocked, Store, atomic, driver_bridge, git, read, run_lock, runtime_identity
from .native_hosts import adapter_for, execution_hosts, host_catalog, normalize_provider

BASELINE='517de91f1b480ec969bd91385c29ce38d8bc2841'

def package_root():
    value=os.environ.get('FORMAL_GATES_HARNESS_ROOT')
    if not value: raise HarnessError('Use scripts/harness, or set FORMAL_GATES_HARNESS_ROOT')
    return Path(value).resolve()
def binary_path():
    value=os.environ.get('FORMAL_GATES_HARNESS_BINARY')
    if not value: raise HarnessError('Go driver binary is not bound')
    return str(Path(value).resolve())
def select_provider(bridge, explicit=None):
    value = explicit if explicit is not None else os.environ.get('FORMAL_GATES_HARNESS_PROVIDER')
    if not value or not value.strip():
        raise HarnessError('Specify --provider or FORMAL_GATES_HARNESS_PROVIDER; execution adapters: ' + ', '.join(execution_hosts()))
    provider = normalize_provider(bridge, value)
    adapter_for(provider)
    return provider

def doctor(root: Path, provider=None, model="", effort=""):
    adapter = adapter_for(select_provider(driver_bridge(), provider))
    binding = adapter.capture()
    path=root/'.harness'/'probes'/uuid.uuid4().hex
    path.mkdir(parents=True)
    result=adapter.execute({'workspace':str(path),'read_only':True,'provider':adapter.name,
                            'binding':binding, 'model':model, 'reasoning_effort':effort},path,probe=True)
    result['binding'] = binding
    versions={name:importlib.metadata.version(name) for name in ['langgraph','langgraph-checkpoint','langgraph-checkpoint-sqlite']}
    report={'ok':True,'provider':result,'dependencies':versions,'evidence':str(path),'model_calls':0}
    atomic(path/'report.json',report);return report

def make_run(root: Path,project: Path,requirement: Path,checks: list,gates: list,provider=None,
             model='',effort='',repair_limit=2,run_id='',fixture=False):
    project=project.resolve();requirement=requirement.resolve()
    if not (project/'.git').exists(): raise HarnessError('A local Git repository is required')
    if git(project,'rev-parse','--show-toplevel')!=str(project): raise HarnessError('Use the repository root')
    # The frozen base is committed content; do not silently discard local edits.
    if git(project,'status','--porcelain','--untracked-files=normal'):
        raise HarnessError('Commit or separately preserve project changes before start; source project will not be modified')
    body=requirement.read_bytes()
    if not body.strip(): raise HarnessError('The requirement file is empty')
    if len(body)>2*1024*1024: raise HarnessError('Requirement exceeds 2 MiB')
    bridge=driver_bridge()
    base=bridge.call('resolve-snapshot',{'root':str(project)})['candidate']
    provider=normalize_provider(bridge, provider) if fixture else select_provider(bridge, provider)
    if provider=='fixture' and not fixture: raise HarnessError('Fixture workers require the labelled test/demo path')
    binding=None
    if provider!='fixture':
        adapter_for(provider)  # Fail before creating a run for installation-only hosts.
        probe=doctor(root,provider,model,effort)['provider']; selected=probe['selection']; binding=probe['binding']
        model=selected.get('model',model);effort=selected.get('reasoning_effort',effort)
        if not model: raise HarnessError(f'Native {provider} did not advertise a selected model')
    spec={'version':1,'project':str(project),'base':base,'requirement_digest':hashlib.sha256(body).hexdigest(),
          'checks':checks,'gates':gates,'provider':provider,'model':model,'reasoning_effort':effort,'repair_limit':repair_limit}
    validated=bridge.call('validate-spec',spec)
    run_id=run_id or time.strftime('harness-%Y%m%d-%H%M%S-')+uuid.uuid4().hex[:8]
    if not run_id or len(run_id)>96 or any(c not in 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_' for c in run_id):
        raise HarnessError('Invalid run id')
    directory=root/'.harness'/'runs'/run_id
    directory.mkdir(parents=True,exist_ok=False)
    manifest={'run_id':run_id,'profile':'harness-v1','baseline':BASELINE,'created_at':time.time(),
              **validated,'runtime_identity':runtime_identity(root,binary_path()),'fixture':fixture}
    if binding is not None: manifest['host_binding'] = binding
    policy = (root / 'prompts/reviewer-base.md').read_bytes()
    manifest['reviewer_policy_digest'] = hashlib.sha256(policy).hexdigest()
    (directory / 'reviewer-base.md').write_bytes(policy)
    (directory/'requirement.md').write_bytes(body);atomic(directory/'manifest.json',manifest)
    return directory

def open_run(path: str | Path):
    root=package_root();directory=Path(path).resolve()
    manifest=read(directory/'manifest.json')
    if manifest.get('profile')!='harness-v1': raise HarnessError('Not a harness-v1 run; legacy state is not supported')
    current_identity = runtime_identity(root,binary_path())
    if manifest.get('runtime_identity') != current_identity:
        receipt_path = directory / 'transport-adoption.json'
        receipt = read(receipt_path) if receipt_path.exists() else {}
        if (receipt.get('from_runtime') != manifest.get('runtime_identity') or
            receipt.get('to_runtime') != current_identity or receipt.get('revision') != manifest['revision']):
            raise HarnessError('RUNTIME_CHANGED: use the bound runtime; transport fixes require explicit adopt-transport')
    if hashlib.sha256((directory/'requirement.md').read_bytes()).hexdigest()!=manifest['spec']['requirement_digest']:
        raise HarnessError('Requirement bytes changed after confirmation binding')
    if manifest.get('reviewer_policy_digest') != hashlib.sha256((directory/'reviewer-base.md').read_bytes()).hexdigest():
        raise HarnessError('Bound reviewer policy changed or is missing')
    bridge=driver_bridge();checked=bridge.call('validate-spec',manifest['spec'])
    if checked['revision']!=manifest['revision']:raise HarnessError('Spec revision does not match manifest')
    return root,Store(directory,manifest,bridge)


def adopt_transport(directory: Path, snapshot: Path, before: str, after: str, reason: str):
    """Explicit dev-only adoption, not a graph/state migration or a model tool.

    Exact runtime identities and a preserved source snapshot are required. The
    graph, persistence, Go binary, dependency lock and other runtime files must
    be byte-identical. The caller reviews the listed transport changes.
    """
    root = package_root(); binary = binary_path(); directory = directory.resolve(); snapshot = snapshot.resolve()
    if not reason.strip(): raise HarnessError('Transport adoption requires a reason')
    with run_lock(directory):
        manifest = read(directory / 'manifest.json')
        if manifest.get('profile') != 'harness-v1' or manifest.get('runtime_identity') != before:
            raise HarnessError('Original runtime identity/profile mismatch')
        if runtime_identity(snapshot, str(snapshot / '.harness/bin/formal-gates')) != before:
            raise HarnessError('Preserved runtime snapshot does not match the original')
        if runtime_identity(root, binary) != after:
            raise HarnessError('Reviewed target runtime identity does not match current files')
        def inventory(base):
            paths = [p for p in (base / 'runtime/langgraph/formal_gates_harness').rglob('*')
                     if p.is_file() and p.suffix in ('.py', '.mjs')]
            paths += [base / 'runtime/langgraph/requirements.lock', base / '.harness/bin/formal-gates', base / 'prompts/reviewer-base.md']
            return {str(p.relative_to(base)): hashlib.sha256(p.read_bytes()).hexdigest() for p in paths}
        old, new = inventory(snapshot), inventory(root)
        transport = adapter_for(manifest['spec']['provider']).transport_files()
        allowed = {'runtime/langgraph/formal_gates_harness/' + p for p in (*transport, 'workers.py', 'cli.py')}
        changed = {p for p in old.keys() | new.keys() if old.get(p) != new.get(p)}
        if old.keys() != new.keys() or not changed or not changed <= allowed:
            raise HarnessError('Adoption is limited to reviewed transport/CLI fixes; graph, Go and dependencies must be unchanged')
        if any(not (p.parent / 'exit.json').exists() for p in (directory / 'jobs').glob('*/request.json')):
            raise HarnessError('Cannot adopt while any native job is active or unresolved')
        bridge = Bridge(binary)
        if (hashlib.sha256((directory / 'requirement.md').read_bytes()).hexdigest() != manifest['spec']['requirement_digest'] or
            bridge.call('validate-spec', manifest['spec'])['revision'] != manifest['revision']):
            raise HarnessError('Business inputs changed')
        # Checkpoint open/inspection, not an invoke or a state write.
        store = Store(directory, manifest, bridge)
        with SqliteSaver.from_conn_string(str(directory / 'checkpoints.sqlite')) as saver:
            app = Flow(store, root).build(saver)
            current = status_projection(store, app, {'configurable': {'thread_id': manifest['run_id']}})
            if current['status'] != 'WAITING_USER' or any(p['request']['kind'] != 'runtime' for p in current['interrupts']):
                raise HarnessError('Adoption requires a runtime-only paused run')
        receipt = {'kind': 'transport-fix-adoption', 'from_runtime': before, 'to_runtime': after,
                   'revision': manifest['revision'], 'reason': reason, 'snapshot': str(snapshot),
                   'changed': {p: {'before': old[p], 'after': new[p]} for p in sorted(changed)}}
        target = directory / 'transport-adoption.json'
        if target.exists() and read(target) != receipt:
            raise HarnessError('Different transport adoption already recorded')
        atomic(target, receipt)
        return {'status': 'TRANSPORT_ADOPTED', 'receipt': str(target), 'changes': sorted(changed),
                'state_migrated': False, 'model_calls': 0}

def native_pending_interactions(store):
    """Run-level read-only listing of held native interactions (G2d item 2).

    Each job directory's interaction-pending.json is scanned and only records
    still pending are open questions; delivered, observed and invalidated
    holds are evidence. Nothing is written and no graph interrupt is
    fabricated: the operator answers through native-respond while the graph
    keeps its own pause semantics.
    """
    collected=[]
    for path in sorted((store.directory/'jobs').glob('*/interaction-pending.json')):
        records=read(path)
        if not isinstance(records,list):continue
        for record in records:
            if isinstance(record,dict) and record.get('status')=='pending':
                # Body keys (toolName/input/toolCall/prompt/questions) are the Go-side
                # adapter's choice; the saved record is passed through verbatim, minus
                # the lifecycle bookkeeping, instead of being re-listed here.
                projected={key:value for key,value in record.items()
                           if key not in ('status','connection','updated_at')}
                projected['job']=path.parent.name
                collected.append(projected)
    return collected

def status_projection(store,app,config):
    snapshot=app.get_state(config,subgraphs=True);state=snapshot.values or {};pending=[]
    def visit(snap):
        for task in snap.tasks:
            for item in task.interrupts:
                if not any(p['id']==item.id for p in pending):pending.append({'id':item.id,'request':item.value})
            if hasattr(task.state,'tasks'):visit(task.state)
    visit(snapshot)
    native=native_pending_interactions(store)
    terminal=store.get(state['terminal']) if state.get('terminal') else None
    failure_path=store.directory/'runner-error.json'
    return {'run_id':store.manifest['run_id'],'directory':str(store.directory),
            'profile':'harness-v1','revision':store.manifest['revision'],'candidate':state.get('candidate'),
            'status':terminal['status'] if terminal else ('ABORTED' if state.get('stop_reason') else ('WAITING_USER' if pending or native else 'PENDING')),
            'round':state.get('round',0),'next':list(snapshot.next),'interrupts':pending,'native_interactions':native,
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
            raise HarnessError(
                'CHECKPOINT_NOT_INITIALIZED: checkpoint tables are not initialized. '
                'show is read-only; use resume on this same run to continue an interrupted startup.')
        connection.execute('BEGIN')
        saver = SqliteSaver(connection)
        saver.is_setup = True  # pinned sqlite checkpointer 3.1.1; setup otherwise writes DDL
        yield saver
    except sqlite3.Error as exc:
        # Missing first checkpoint is a recoverable startup gap, not an excuse
        # for a read command to initialize or recreate authoritative state.
        raise HarnessError(
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
        raise HarnessError('An exact interrupt ID is required; a revision alone cannot identify a user decision')
    if (store.directory / 'archive.json').exists():
        from .retention import archived_status
        if answer is not None:
            raise HarnessError('RUN_TERMINAL: archived run does not accept an answer')
        return archived_status(store.directory)
    if not advance:
        with read_checkpointer(store.directory/'checkpoints.sqlite') as saver:
            return status_projection(store, Flow(store,root).build(saver), config)
    with run_lock(store.directory):
        if (store.directory / 'archive.json').exists():
            from .retention import archived_status
            if answer is not None: raise HarnessError('RUN_TERMINAL: archived run')
            return archived_status(store.directory)
        with SqliteSaver.from_conn_string(str(store.directory/'checkpoints.sqlite')) as saver:
            app=Flow(store,root).build(saver)
            current=status_projection(store,app,config)
            if current['status'] in ('HARNESS_COMPLETE','DEMO_COMPLETE','ABORTED'):
                if answer is not None:
                    raise HarnessError(
                        f"RUN_TERMINAL: {current['status']} does not accept a user decision; "
                        'the answer was not applied. Use show to inspect this run.')
                return current
            pending=current['interrupts']
            if pending:
                if answer is None:return current
                if not isinstance(answer,dict) or set(answer)!={'decision','revision'}:
                    raise HarnessError('Answer accepts only decision and revision; state/goto patches are not allowed')
                chosen=[p for p in pending if p['id']==interrupt_id]
                if len(chosen)!=1:raise HarnessError('Select exactly one current interrupt id')
                item=chosen[0]
                if answer['revision']!=item['request']['revision'] or answer['decision'] not in item['request']['choices']:
                    raise HarnessError('Stale revision or invalid decision; run was not advanced')
                value=Command(resume={item['id']:answer})
            else:
                if answer is not None:raise HarnessError('No pending user decision')
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

def make_demo(root: Path,native=False,provider=None):
    if native: provider = select_provider(driver_bridge(), provider)
    directory=root/'.harness'/'samples'/(('native-' if native else 'fixture-')+uuid.uuid4().hex[:12]);directory.mkdir(parents=True)
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
    git(project,'add','.');git(project,'-c','user.name=Harness Test','-c','user.email=harness@localhost','-c','commit.gpgsign=false','commit','--quiet','-m','Native harness sample baseline')
    checks=[{'id':'addition','argv':[sys.executable,'check_add.py'],'dependencies':['calculator.py','check_add.py']},
            {'id':'unchanged','argv':[sys.executable,'check_untouched.py'],'dependencies':['untouched.py','check_untouched.py']}]
    return make_run(root,project,requirement,checks,['complexity-gate','implementation-quality-gate'],
                    provider=provider if native else 'fixture',fixture=not native)

def main():
    parser=argparse.ArgumentParser(prog='formal-gates harness',description='Optional LangGraph development driver, not the legacy formal Seal route')
    commands=parser.add_subparsers(dest='command',required=True)
    commands.add_parser('hosts',help='List native execution adapters and existing host integrations')
    health=commands.add_parser('doctor',help='Probe a native host without sending a model prompt')
    health.add_argument('--provider'); health.add_argument('--model',default=''); health.add_argument('--reasoning-effort',default='')
    archive = commands.add_parser('archive', help='Verified ZIP of a terminal run; never deletes source')
    archive.add_argument('--run', required=True); archive.add_argument('--output', required=True)
    prune = commands.add_parser('prune', help='Preview terminal cleanup; --confirm deletes only after archive verification')
    prune.add_argument('--run', required=True); prune.add_argument('--archive', required=True)
    prune.add_argument('--confirm', action='store_true')
    inspect_archive = commands.add_parser('archive-show', help='Verify/read archived status without the old graph runtime')
    inspect_archive.add_argument('--archive', required=True)
    adoption = commands.add_parser('adopt-transport', help='Explicit dev-only transport fix; requires preserved runtime and exact identities')
    adoption.add_argument('--run', required=True); adoption.add_argument('--snapshot', required=True)
    adoption.add_argument('--from-runtime', required=True); adoption.add_argument('--to-runtime', required=True)
    adoption.add_argument('--reason', required=True)
    start=commands.add_parser('start');start.add_argument('--project',required=True);start.add_argument('--requirement',required=True);start.add_argument('--checks',required=True)
    start.add_argument('--provider',help='Execution host; overrides FORMAL_GATES_HARNESS_PROVIDER');start.add_argument('--model',default='');start.add_argument('--reasoning-effort',default='')
    start.add_argument('--gates',nargs='+',default=['complexity-gate','implementation-quality-gate']);start.add_argument('--repair-limit',type=int,default=2);start.add_argument('--run-id',default='')
    for name in ('show','resume','respond','cancel'):
        sub=commands.add_parser(name);sub.add_argument('--run',required=True)
        if name=='respond':
            sub.add_argument('--decision',required=True);sub.add_argument('--revision',required=True);sub.add_argument('--interrupt',required=True)
        if name=='cancel':sub.add_argument('--confirm',action='store_true',required=True)
    demo=commands.add_parser('demo',help='Create isolated sample; fixture by default, --native uses the selected execution host')
    demo.add_argument('--provider'); demo.add_argument('--native',action='store_true');demo.add_argument('--approve-sample',action='store_true',help='Approve this command-created sample only; never bypass runtime or review blockers')
    args=parser.parse_args()
    try:
        root=package_root()
        if args.command=='hosts':result=host_catalog(driver_bridge())
        elif args.command=='doctor':result=doctor(root,args.provider,args.model,args.reasoning_effort)
        elif args.command=='archive':
            from .retention import archive_run
            result=archive_run(args.run,args.output)
        elif args.command=='prune':
            from .retention import prune_run
            result=prune_run(args.run,args.archive,args.confirm)
        elif args.command=='archive-show':
            from .retention import archive_status
            result=archive_status(args.archive)
        elif args.command=='adopt-transport':
            result=adopt_transport(Path(args.run),Path(args.snapshot),args.from_runtime,args.to_runtime,args.reason)
        elif args.command=='start':
            directory=make_run(root,Path(args.project),Path(args.requirement),read(Path(args.checks)),args.gates,args.provider,args.model,args.reasoning_effort,args.repair_limit,args.run_id)
            result=run_graph(directory)
        elif args.command=='demo':
            directory=make_demo(root,args.native,args.provider);print(json.dumps({'created_run':str(directory)}),flush=True)
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
    except (HarnessError,OSError,ValueError) as exc:
        print(json.dumps({'ok':False,'error':str(exc)},ensure_ascii=False),file=sys.stderr);return 2

if __name__=='__main__':raise SystemExit(main())
