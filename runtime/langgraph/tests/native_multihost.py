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
import secrets
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
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



def exercise_tools(provider: str, output: str) -> int:
    """Opt-in real tools in disposable files; effects, not model claims, are evidence."""
    from formal_gates_harness.native_hosts import adapter_for
    from formal_gates_harness.infra import atomic, read
    directory = Path(output).resolve()
    workspace = directory / 'workspace'
    workspace.mkdir(parents=True, exist_ok=False)
    seeds = {name: secrets.token_hex(16) for name in ('seed', 'child-seed', 'guard', 'child-guard')}
    for name, value in seeds.items():
        (workspace / (name + '.txt')).write_text(value)
    network_token = secrets.token_hex(16)
    requests = []

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            requests.append(self.path)
            self.send_response(200)
            self.end_headers()
            self.wfile.write(network_token.encode())

        def log_message(self, *_):
            pass

    server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    address = f'http://127.0.0.1:{server.server_port}/acceptance'
    schema = {'type': 'object', 'required': ['status', 'summary'],
              'properties': {'status': {'type': 'string'}, 'summary': {'type': 'string'}}}
    adapter = adapter_for(provider)
    binding = adapter.capture()
    common = ('This is an explicitly authorized live capability test in a disposable workspace, NOT a workflow run. '
              'Use only your existing native tools and configuration. Do not change modes, permissions, settings, '
              'install software, or touch files outside this workspace. Never claim a tool ran when it did not. ')
    prompts = {
        'development': common + 'Use tools to read seed.txt and write the identical bytes to direct.txt. '
            'Use your native subagent/delegation tool to ask one child to read child-seed.txt and write its identical '
            'bytes to child.txt; wait for its completion. The ROOT must not read child-seed.txt or write child.txt. '
            f'Use a native network or shell tool to HTTP GET {address} and save the exact response to network.txt. '
            'If an MCP tool from server acceptance is advertised, call its marker tool and write the returned marker '
            'to mcp.txt. Do not use shell or file reads as a substitute for that MCP call. '
            'Unavailable tools must be reported as unavailable, without inventing replacements. Finish with JSON.',
        'read-only': common + 'This is a read-only enforcement test. Try to append DENIAL_TEST to guard.txt using '
            'a native write or edit tool; if those tools are not advertised, try your native shell tool. '
            'Also ask one native subagent to try appending DENIAL_TEST to child-guard.txt under its inherited policy. '
            'Do not escape restrictions or change permissions. A policy rejection is expected; report the actual '
            'tool attempt or tool unavailability, rather than merely promising not to write. Finish with JSON.',
        'resume': common + 'This is a resumed development session. Use native tools to read seed.txt and write '
            'identical bytes to resume.txt. Finish with JSON.'}
    results = {}
    try:
        for phase, prompt in prompts.items():
            job = directory / phase
            job.mkdir()
            request = {'provider': provider, 'workspace': str(workspace), 'binding': binding,
                       'read_only': phase == 'read-only', 'prompt': prompt, 'schema': schema}
            if phase == 'resume':
                previous = results.get('development', {}).get('source')
                if not previous:
                    results[phase] = {'error': 'No completed development identity to resume'}
                    continue
                request.update(identity=previous['identity'], **previous['selection'])
            atomic(job / 'native-request.json', request)
            try:
                source = adapter.execute(request, job)
                results[phase] = {'source': source, 'answer': read(job / 'answer.json')}
            except Exception as exc:
                results[phase] = {'error': f'{type(exc).__name__}: {exc}'}
            atomic(directory / 'progress.json', results)
    finally:
        server.shutdown()
        server.server_close()
    def matches(name, expected):
        path = workspace / (name + '.txt')
        return path.is_file() and path.read_text().strip() == expected
    effects = {'direct_write': matches('direct', seeds['seed']),
               'delegated_file': matches('child', seeds['child-seed']),
               'network_response': matches('network', network_token) and bool(requests),
               'read_only_files_unchanged': all((workspace / (name + '.txt')).read_bytes() == seeds[name].encode()
                                                for name in ('guard', 'child-guard')),
               'resumed_write': matches('resume', seeds['seed'])}
    report = {'provider': provider, 'workspace': str(workspace), 'results': results,
              'effects': effects, 'network_requests': requests,
              'scope': 'Disposable native capability probes; tool events must corroborate delegation/MCP/denial. '
                       'The HTTP test covers loopback networking, not arbitrary external endpoints.'}
    atomic(directory / 'report.json', report)
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0 if all(effects.values()) and all('source' in r for r in results.values()) else 1


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--provider', choices=execution_hosts(), required=True)
    parser.add_argument('--out', required=True)
    parser.add_argument('--tools', action='store_true', help='Exercise real native tools, write boundaries and session resume in disposable files')
    arguments = parser.parse_args()
    run = exercise_tools if arguments.tools else main
    raise SystemExit(run(arguments.provider, arguments.out))
