"""Persisted role evidence for an explicitly authorized replacement session.

This does not select a next task, approve findings, or reconstruct hidden model
memory. A failed invocation must be settled before a second writer is started.
"""
from __future__ import annotations
import os
from pathlib import Path
from .infra import DogfoodError, RuntimeBlocked, atomic, digest, git, read, file_proof


def prepare_session_restart(store, series: str, attempt: int, role: str,
                            workspace: Path, materials: Path) -> dict:
    if attempt < 1:
        raise DogfoodError('SESSION_RESTART: an interrupted prior attempt is required')
    previous = digest({'series': series, 'attempt': attempt - 1})
    job = store.directory / 'jobs' / previous
    if not (job / 'exit.json').exists():
        raise RuntimeBlocked('SESSION_RESTART: prior invocation is unresolved; no second writer')
    process = {**read(job / 'exit.json'), 'directory': str(job), 'operation': previous}
    store.verify_process(process)
    original = read(job / 'native-request.json')
    if Path(original['workspace']).resolve() != workspace.resolve():
        raise DogfoodError('SESSION_RESTART: workspace changed')
    # Native transport records the actual DSH PID. A receipt from a killed Python
    # wrapper is not enough if that wrapper left an executing DSH child behind.
    live = job / 'native-process.json'
    if live.exists():
        pid = read(live)['pid']
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            pass
        except PermissionError as exc:
            raise RuntimeBlocked('SESSION_RESTART: cannot verify prior native process ended') from exc
        else:
            raise RuntimeBlocked('SESSION_RESTART: prior native process is still alive')
    prior_result = store.directory / 'workers' / f'{previous}.json'
    if prior_result.exists():
        result = store.get(read(prior_result)['ref'])['result']['status']
        if result != 'BLOCKED':
            raise DogfoodError('SESSION_RESTART: prior role result was already accepted')
    identity = read(job / 'identity.json') if (job / 'identity.json').exists() else {}
    saved = materials / 'handover.json'
    if saved.exists():
        bundle = read(saved)
        expected = {'role': role, 'revision': store.manifest['revision'],
                    'previous_job': previous, 'workspace': str(workspace)}
        if any(bundle.get(k) != v for k, v in expected.items()):
            raise DogfoodError('SESSION_RESTART: saved handover binding changed')
        for entry in [*bundle['files'].values(), bundle['diff']]:
            if entry.get('available', True):
                proof = {k: entry[k] for k in ('sha256', 'size')}
                if file_proof(Path(entry['path'])) != proof:
                    raise RuntimeBlocked('SESSION_RESTART: saved handover evidence changed')
        # A replay must not replace the original input with work performed later.
        return bundle
    bundle = {'kind': 'interrupted-role-handover', 'role': role,
              'revision': store.manifest['revision'], 'previous_job': previous,
              'previous_identity': identity.get('identity', ''),
              'workspace': str(workspace), 'head': git(workspace, 'rev-parse', 'HEAD'),
              'git_status': git(workspace, 'status', '--porcelain'),
              'partial_opinions_are_not_final_results': True,
              'original_exit_code': process['exit_code'], 'files': {}}
    materials.mkdir(parents=True, exist_ok=True)
    # Preserve the original evidence. Copy into this role's input workspace so
    # it does not need write/access grants to the controller's job directory.
    for name in ('native-events.jsonl', 'protocol.jsonl', 'answer.raw.txt'):
        source = job / name
        if not source.exists():
            bundle['files'][name] = {'available': False}
            continue
        body = source.read_bytes()
        destination = materials / ('previous-' + name)
        # No silent clipping: excessive logs remain an explicit recovery gap.
        if len(body) > 4 * 1024 * 1024:
            raise RuntimeBlocked('SESSION_RESTART: prior role evidence exceeds 4 MiB; explicit review required')
        destination.write_bytes(body)
        bundle['files'][name] = {'available': True, 'path': str(destination), **file_proof(source)}
    diff = materials / 'previous-uncommitted.diff'
    diff.write_text(git(workspace, 'diff', '--binary', 'HEAD'))
    bundle['diff'] = {'path': str(diff), **file_proof(diff)}
    atomic(materials / 'handover.json', bundle)
    return bundle
