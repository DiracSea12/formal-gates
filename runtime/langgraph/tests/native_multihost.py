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
        completed = subprocess.run(argv, cwd=ROOT, stdout=out, stderr=err)
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
              'next_validation': 'Verify actual registered workspace calls and their effects in the original source evidence.',
              'interrupts': state.get('interrupts', []),
              'scope': 'real native roles and public commands; isolated harness sample, not formal Seal'}
    atomic(directory / 'report.json', report)
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0 if passed else 1



def exercise_tools(provider: str, output: str) -> int:
    """Opt-in native managed tools; original bytes and service denials are evidence."""
    os.environ.update(FORMAL_GATES_HARNESS_ROOT=str(ROOT),
                      FORMAL_GATES_HARNESS_BINARY=str(ROOT / '.harness/bin/formal-gates'))
    from formal_gates_harness.native_hosts import adapter_for
    from formal_gates_harness.infra import atomic, read
    directory = Path(output).resolve()
    workspace = directory / 'workspace'
    workspace.mkdir(parents=True, exist_ok=False)
    seed = secrets.token_bytes(32)
    (workspace / 'seed.bin').write_bytes(seed)
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
    fetch = directory / 'fetch-response'
    fetch.write_text('#!' + sys.executable + '\n'
                     'from urllib.request import urlopen\nfrom pathlib import Path\n'
                     'with urlopen(' + repr(address) + ') as response:\n'
                     '    Path("network.txt").write_bytes(response.read())\n')
    fetch.chmod(0o755)
    schema = {'type': 'object', 'additionalProperties': False, 'required': ['summary'],
              'properties': {'summary': {'type': 'string'}}}
    adapter = adapter_for(provider)
    binding = adapter.capture()
    prompt = ('In this disposable workspace, locate the original seed.bin using workspace listings. '
              'Use workspace copy with its original listing/entry to copy its bytes to the new filename {name}. '
              'Finish with a brief semantic summary.')
    results = {}
    try:
        for role, name in (('development', 'direct.bin'), ('resume', 'resume.bin')):
            job = directory / role
            job.mkdir()
            request = {'provider': provider, 'workspace': str(workspace), 'binding': binding,
                       'read_only': False, 'prompt': prompt.format(name=name), 'schema': schema}
            if role == 'resume':
                previous = results.get('development', {}).get('source')
                if not previous:
                    results[role] = {'error': 'No completed development identity to resume'}
                    continue
                request.update(identity=previous['identity'], **previous['selection'])
                if provider == 'cursor': request['native_store'] = previous['native_store']
            try:
                source = adapter.execute(request, job)
                adapter.verify_source(job, source)
                results[role] = {'source': source, 'answer': read(job / 'answer.json')}
            except Exception as exc:
                results[role] = {'error': f'{type(exc).__name__}: {exc}'}
            atomic(directory / 'progress.json', results)
        fetch_result = subprocess.run([str(fetch)], cwd=workspace, capture_output=True)
        (directory / 'fetch.stdout').write_bytes(fetch_result.stdout)
        (directory / 'fetch.stderr').write_bytes(fetch_result.stderr)
        atomic(directory / 'fetch.json', {'argv': [str(fetch)], 'cwd': str(workspace),
                                         'exit_code': fetch_result.returncode})
    finally:
        server.shutdown()
        server.server_close()

    # The same managed tool service owns read-only semantics for every host.
    # Probe it with original file selections; do not ask a model to manufacture
    # an unadvertised shell/write tool or transcribe the seed bytes.
    denied_job = directory / 'read-only-check'
    denied_job.mkdir()
    atomic(denied_job / 'native-request.json', {'workspace': str(workspace), 'read_only': True})
    binary = ROOT / '.harness/bin/formal-gates'
    def workspace_call(arguments):
        request = {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/call',
                   'params': {'name': 'workspace', 'arguments': arguments}}
        completed = subprocess.run([str(binary), 'harness', 'native-tools', str(denied_job)],
                                   input=json.dumps(request).encode(), capture_output=True)
        if completed.returncode: raise RuntimeError(completed.stderr.decode())
        return json.loads(completed.stdout)['result']
    listing_reply = workspace_call({'action': 'list'})
    listing = json.loads(listing_reply['content'][0]['text'])
    source = next(entry['entry'] for entry in listing['entries'] if entry['name'] == 'seed.bin')
    denied = workspace_call({'action': 'copy', 'listing': listing['listing'], 'entry': source,
                             'name': 'denied.bin'})
    atomic(denied_job / 'denial.json', denied)

    def original_copy(role):
        path = directory / role / 'native-tool-calls.jsonl'
        if not path.exists(): return False
        for line in path.read_text().splitlines():
            call = json.loads(line)
            request = call['request']
            arguments = request.get('params', {}).get('arguments', {})
            if arguments.get('action') != 'copy': continue
            result = call['result']
            if result.get('success') is True or result.get('isError') is False: return True
        return False

    effects = {
        'direct_copy': (workspace / 'direct.bin').is_file() and (workspace / 'direct.bin').read_bytes() == seed and original_copy('development'),
        'resumed_copy': (workspace / 'resume.bin').is_file() and (workspace / 'resume.bin').read_bytes() == seed and original_copy('resume'),
        'read_only_denial': denied.get('isError') is True and not (workspace / 'denied.bin').exists() and (workspace / 'seed.bin').read_bytes() == seed,
        'program_fetch': fetch_result.returncode == 0 and (workspace / 'network.txt').is_file() and (workspace / 'network.txt').read_bytes() == network_token.encode() and bool(requests),
    }
    report = {'provider': provider, 'workspace': str(workspace), 'results': results,
              'effects': effects, 'network_requests': requests,
              'scope': 'Native managed copy and resume; the shared workspace service owns the read-only denial. The program executes the fixed loopback helper.'}
    atomic(directory / 'report.json', report)
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0 if all(effects.values()) and all('source' in r for r in results.values()) else 1


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--provider', choices=execution_hosts(), required=True)
    parser.add_argument('--out', required=True)
    parser.add_argument('--tools', action='store_true', help='Exercise native managed copy/resume, shared read-only denial and program-owned fetch')
    arguments = parser.parse_args()
    run = exercise_tools if arguments.tools else main
    raise SystemExit(run(arguments.provider, arguments.out))
