"""Role inputs and native transport; no graph imports or next-step authority."""
from __future__ import annotations
import hashlib
import json
from pathlib import Path
import time
from .infra import HarnessError, RuntimeBlocked, SessionUnavailable, Store, atomic, digest, file_proof, read

from .feedback import BOUNDARY, materialize_comparison, task_input
from .native_hosts import adapter_for

ROLES = {
    "product-review": "Assess requirement scope and concrete acceptance.",
    "technical-review": "Assess feasibility and the smallest implementation plan; state the plan in summary.",
    "development": "Implement the approved requirement within its scope.",
    "repair": "Resolve the supplied mandatory findings and failed checks within the original scope.",
    "qa-design": "Assess each approved check's coverage and oracle from the baseline workspace. Missing implementation on the baseline is expected; a real coverage gap needs a mandatory finding.",
    "qa-review": "Review the supplied QA design for coverage and feasibility of each approved check. Missing implementation on the baseline is expected.",
    "complexity-gate": "Review the complete supplied change for unnecessary complexity and changes unrelated to the approved requirement.",
    "implementation-quality-gate": "Review the complete supplied change for correctness and maintainability using actual code and assertions.",
}

class Workers:
    def __init__(self, store: Store, package: Path):
        self.store, self.package = store, package
        self.spec = store.manifest['spec']
    def run(self, role: str, workspace: Path, key_data: dict,
            feedback: dict | None = None, session: str = '', attempt: int = 0, restart_session: bool = False, native_store: str = '') -> dict:
        if role not in ROLES: raise HarnessError('unknown role')
        series = digest({'role':role,'input':key_data,'feedback':feedback,'revision':self.store.manifest['revision']})
        key = digest({'series':series,'attempt':attempt})
        identity_receipt = self.store.directory/'native-identities'/f'{series}.json'
        if restart_session:
            session = ''; native_store = ''
        elif identity_receipt.exists():
            # The graph may replay with its original session argument after a
            # replacement failed. The same-series receipt is the latest identity,
            # not the stale argument captured before that replacement started.
            latest = read(identity_receipt)
            if latest.get('workspace') and Path(latest['workspace']).resolve() != workspace.resolve():
                raise HarnessError('SESSION_BINDING: latest identity belongs to a different workspace')
            if latest.get('provider', self.spec['provider']) != self.spec['provider']:
                raise HarnessError('SESSION_BINDING: provider changed')
            session = latest['identity']
            native_store = latest.get('native_store', '')
            if not isinstance(session, str) or not session:
                raise HarnessError('SESSION_BINDING: latest identity is empty')
        receipt = self.store.directory/'workers'/f'{key}.json'
        if receipt.exists(): return self.store.verify_evidence(read(receipt)['ref'])
        self.store.event('worker-start',role=role,operation=key,attempt=attempt)
        input_files = {}
        if self.spec['provider']=='fixture':
            result=self.fixture(role,workspace)
            source={'provider':'fixture','model_calls':0,'identity':'fixture-'+role}
        else:
            adapter = adapter_for(self.spec['provider'])
            binding = self.store.manifest.get('host_binding') or adapter.capture()
            adapter.validate_binding(binding)
            materials=workspace/'.formal-gates-input'/key; materials.mkdir(parents=True,exist_ok=True)
            requirement=materials/'requirement.md'
            requirement_body = (self.store.directory/'requirement.md').read_bytes()
            if requirement.exists() and requirement.read_bytes() != requirement_body:
                raise RuntimeBlocked('Saved requirement material changed')
            if not requirement.exists(): requirement.write_bytes(requirement_body)
            policy=materials/'reviewer-base.md'
            bound_policy = self.store.directory / 'reviewer-base.md'
            policy_body = (bound_policy if bound_policy.exists() else self.package/'prompts/reviewer-base.md').read_bytes()
            expected_policy = self.store.manifest.get('reviewer_policy_digest')
            if expected_policy and hashlib.sha256(policy_body).hexdigest() != expected_policy:
                raise RuntimeBlocked('Bound reviewer policy changed')
            if policy.exists() and policy.read_bytes() != policy_body:
                raise RuntimeBlocked('Saved reviewer material changed')
            if not policy.exists(): policy.write_bytes(policy_body)
            handover = None
            if restart_session:
                from .handover import prepare_session_restart
                handover = prepare_session_restart(self.store, series, attempt, role, workspace, materials)
            bound_task = task_input(role=role, revision=self.store.manifest['revision'], base=self.spec['base'],
                                   candidate=key_data.get('candidate', ''), checks=self.spec['checks'], feedback=feedback)
            task = {'feedback': bound_task['feedback']['content'] if bound_task['feedback'] else None}
            model_checks = self.spec['checks'] if role in ('qa-design', 'qa-review') else []
            if model_checks:
                task['checks'] = [{'argv': check['argv'], 'dependencies': check['dependencies']} for check in model_checks]
            if role in ('complexity-gate', 'implementation-quality-gate'):
                comparison = materialize_comparison(workspace, materials, self.spec['base'], key_data['candidate'])
                task['comparison'] = {'directory': 'comparison', 'index': 'comparison.json', 'diff': 'full.diff'}
                input_files = {str(Path(comparison['directory']) / name):proof for name, proof in comparison['files'].items()}
            task_path = materials / 'task.json'
            if task_path.exists() and read(task_path) != task:
                raise RuntimeBlocked('Saved task material changed')
            if not task_path.exists(): atomic(task_path, task)
            prompt=(ROLES[role]+'\n'
                +'Read the current requirement, task and reviewer contract in .formal-gates-input using workspace selections. '
                +'Gate reviews use the supplied full comparison and both original versions. '
                +'Report semantic findings with locations selecting original workspace read or listing/entry numbers. '
                +'QA roles return check_reviews in approved order; other roles return an empty array. '
                +'For unfinished work use remainingWork; for an actual runtime interruption use interruptionReason. '
                +BOUNDARY)
            if handover:
                prompt += '\nRead the supplied handover, verify completed work and finish the remaining original scope.'
            directory=self.store.directory/'jobs'/key;directory.mkdir(parents=True,exist_ok=True)
            native={'provider':adapter.name, 'binding':binding, 'workspace':str(workspace),'read_only':role not in ('development','repair'),
                'identity':session,'model':self.spec['model'],'reasoning_effort':self.spec['reasoning_effort'],'materials':str(materials),'prompt':prompt,'schema':self.store.bridge.call('schema', {'checks':model_checks})}
            if adapter.name == 'cursor' and session: native['native_store'] = native_store
            path=directory/'native-request.json'
            if path.exists():
                original=read(path)
                # Replay the original call even when it attached a session ID
                # before the workflow result could be checkpointed.
                if {k:v for k,v in original.items() if k not in ('identity', 'native_store')}!={k:v for k,v in native.items() if k not in ('identity', 'native_store')}:
                    raise HarnessError('native input changed for the same operation')
            else: atomic(path,native)
            process=self.store.job(key,{'kind':'native','argv':adapter.entry_argv(self.store.bridge.binary,str(directory)),
                'cwd':str(workspace),'stdin':'','environment':{'LANGSMITH_TRACING':'false','LANGCHAIN_TRACING_V2':'false'}})
            identity=directory/'identity.json'
            if identity.exists(): atomic(identity_receipt,read(identity))
            if process['exit_code']!=0 or not (directory/'answer.json').exists():
                error_record = read(directory/'native-error.json') if (directory/'native-error.json').exists() else {}
                if error_record.get('error_kind') == 'SESSION_UNAVAILABLE':
                    raise SessionUnavailable(error_record['error'], identity=error_record.get('identity', session),
                                             method=error_record.get('method', 'session/resume'), code=error_record.get('native_code'))
                try:
                    source = adapter.reconcile(directory, process)
                    self.store.event('native-output-reconciled', role=role, operation=key,
                                     identity=source['identity'], new_model_calls=0)
                except (HarnessError, OSError, ValueError, KeyError) as exc:
                    error=read(directory/'native-error.json')['error'] if (directory/'native-error.json').exists() else process.get('error','No native answer')
                    self.store.put({'process':process,'error':error,'reconciliation_error':str(exc)})
                    raise RuntimeBlocked(f'NATIVE_BLOCKED: {error}. Evidence: {directory}') from exc
            result=read(directory/'answer.json');source={**read(directory/'source.json'),'job':key,'process':process}
            if source.get('provider') != adapter.name:
                raise RuntimeBlocked('NATIVE_BINDING: result provider differs from task')
            if handover:
                source['handover'] = {'mode': 'new-session', 'previous_job': handover['previous_job'],
                                      'previous_identity': handover['previous_identity']}
                if source['identity'] == handover['previous_identity']:
                    raise RuntimeBlocked('New-session takeover reused the old native identity')
            if hashlib.sha256(requirement.read_bytes()).hexdigest()!=self.spec['requirement_digest']:
                raise RuntimeBlocked('worker modified its bound requirement material')
            if any(file_proof(Path(name)) != proof for name, proof in input_files.items()):
                raise RuntimeBlocked('worker modified its bound comparison material')
        judgment={'role':role,'result':result,'checks':self.spec['checks']}
        if self.spec['provider'] != 'fixture':
            judgment.update(nativeDirectory=str(directory), nativeSource=source)
        checked=self.store.bridge.call('validate-worker',judgment)
        output={'result':checked['result'],'needs_repair':checked['needs_repair'],'source':source,
                'role':role,'input':key_data,'input_files':input_files,'revision':self.store.manifest['revision']}
        ref=self.store.put(output);atomic(receipt,{'ref':ref})
        self.store.event('worker-complete',role=role,operation=key,status=checked['result']['status'],evidence=ref)
        return output
    def fixture(self,role: str,workspace: Path) -> dict:
        if not self.store.manifest.get('fixture'): raise HarnessError('Fixture workers are restricted to the explicitly labelled demo')
        checks=['Deterministic coverage/oracle fixture; NOT an independent review.' for c in self.spec['checks']]
        if role=='development':
            time.sleep(.15);(workspace/'calculator.py').write_text('def add(a, b):\n    return a - b  # intentional demo defect\n')
        if role=='repair':(workspace/'calculator.py').write_text('def add(a, b):\n    return a + b\n')
        return {'summary':f'Deterministic {role} fixture; NOT an independent model review.',
                'findings':[],'check_reviews':checks if role in ('qa-design','qa-review') else []}
