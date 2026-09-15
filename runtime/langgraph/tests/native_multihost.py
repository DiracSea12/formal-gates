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


def main(provider: str, output: str) -> int:
    directory = Path(output).resolve()
    directory.mkdir(parents=True, exist_ok=False)
    os.environ.update(FORMAL_GATES_DOGFOOD_ROOT=str(ROOT),
                      FORMAL_GATES_DOGFOOD_BINARY=str(ROOT / '.dogfood/bin/formal-gates'),
                      PYTHONPATH=str(ROOT / 'runtime/langgraph'))
    sys.path.insert(0, str(ROOT / 'runtime/langgraph'))
    from formal_gates_dogfood.cli import open_run
    from formal_gates_dogfood.infra import atomic, read
    argv = [str(ROOT / 'scripts/dogfood'), 'demo', '--native', '--provider', provider, '--approve-sample']
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
    if run_path:
        _, store = open_run(run_path)
        for receipt in sorted((Path(run_path) / 'workers').glob('*.json')):
            item = store.verify_evidence(read(receipt)['ref'])
            roles.append({'role': item['role'], 'provider': item['source']['provider'],
                          'identity': item['source']['identity'], 'status': item['result']['status']})
    passed = (completed.returncode == 0 and state.get('status') == 'DOGFOOD_COMPLETE'
              and len(roles) >= 7 and all(r['provider'] == provider for r in roles))
    report = {'passed': passed, 'provider': provider, 'run': run_path, 'exit_code': completed.returncode,
              'status': state.get('status'), 'candidate': state.get('candidate'), 'roles': roles,
              'interrupts': state.get('interrupts', []),
              'scope': 'real native roles and public commands; isolated dogfood sample, not formal Seal'}
    atomic(directory / 'report.json', report)
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0 if passed else 1


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--provider', choices=['dsh', 'zcode'], required=True)
    parser.add_argument('--out', required=True)
    arguments = parser.parse_args()
    raise SystemExit(main(arguments.provider, arguments.out))
