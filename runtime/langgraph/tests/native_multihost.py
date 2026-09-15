"""Opt-in real-host conformance through the public LangGraph driver.

Runs a NEW isolated native sample, approves only its requirement and plan,
records runtime/review blockers without bypassing them, verifies native evidence.
This sends model requests. Usage:
  python native_multihost.py --provider zcode --out NEW_DIRECTORY
  python native_multihost.py --provider dsh --out ANOTHER_NEW_DIRECTORY
"""
from __future__ import annotations
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'runtime/langgraph'))
from formal_gates_harness.native_hosts import execution_hosts


def main(provider: str, output: str) -> int:
    directory = Path(output).resolve()
    directory.mkdir(parents=True, exist_ok=False)
    os.environ.update(FORMAL_GATES_HARNESS_ROOT=str(ROOT),
                      FORMAL_GATES_HARNESS_BINARY=str(ROOT / '.harness/bin/formal-gates'),
                      PYTHONPATH=str(ROOT / 'runtime/langgraph'))
    from formal_gates_harness.cli import open_run
    from formal_gates_harness.infra import atomic, read
    argv = [str(ROOT / 'scripts/harness'), 'demo', '--native', '--provider', provider, '--approve-sample']
    with (directory / 'stdout.log').open('w') as out, (directory / 'stderr.log').open('w') as err:
        completed = subprocess.run(argv, cwd=ROOT, stdout=out, stderr=err, timeout=1800)
    text = (directory / 'stdout.log').read_text().strip()
    values = []
    decoder = json.JSONDecoder()
    while text:
        value, end = decoder.raw_decode(text)
        values.append(value)
        text = text[end:].strip()
    state = values[-1] if values else {}
    run_path = state.get('directory') or (values[0].get('created_run') if values else None)
    roles = []
    interactions = []
    if run_path:
        _, store = open_run(run_path)
        for receipt in sorted((Path(run_path) / 'workers').glob('*.json')):
            item = store.verify_evidence(read(receipt)['ref'])
            roles.append({'role': item['role'], 'provider': item['source']['provider'],
                          'identity': item['source']['identity'], 'status': item['result']['status'],
                          'evidence': read(receipt)['ref'], 'source_files': item['source'].get('files', {})})
        for path in sorted((Path(run_path) / 'jobs').glob('*/interaction-status.json')):
            interactions.append({'evidence': str(path), 'requests': read(path)})
    passed = (completed.returncode == 0 and state.get('status') == 'HARNESS_COMPLETE'
              and len(roles) >= 7 and all(r['provider'] == provider for r in roles))
    report = {'passed': passed, 'provider': provider, 'run': run_path, 'exit_code': completed.returncode,
              'status': state.get('status'), 'candidate': state.get('candidate'), 'roles': roles,
              'interactions': interactions,
              'capability_validation': {'subagents': 'not_verified', 'mcp': 'not_verified',
                                        'network': 'not_verified', 'read_only_tools': 'not_verified'},
              'next_validation': 'Use the bound native source files to verify successful authorized tool calls and their effects; flow completion alone is insufficient.',
              'interrupts': state.get('interrupts', []),
              'scope': 'real native roles and public commands; isolated harness sample, not formal Seal'}
    atomic(directory / 'report.json', report)
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0 if passed else 1


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--provider', choices=execution_hosts(), required=True)
    parser.add_argument('--out', required=True)
    arguments = parser.parse_args()
    raise SystemExit(main(arguments.provider, arguments.out))
