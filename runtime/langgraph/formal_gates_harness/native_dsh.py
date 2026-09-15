"""DSH ACP transport. No graph imports and no workflow state authority.

Uses existing native sandbox/tool restrictions. A normal ACP stopReason is not
proof of a successful turn: committed native turn/end evidence is required.
"""
from __future__ import annotations
import json
import os
from pathlib import Path
import queue
import shutil
import subprocess
import threading
from .infra import HarnessError, RuntimeBlocked, SessionUnavailable, atomic, read, file_proof

TRANSPORT_FILES = ('native_dsh.py', 'native_dsh.mjs')

class Client:
    def __init__(self, argv: list[str], workspace: str, directory: Path):
        self.directory = directory
        self.err = (directory / 'host.stderr').open('ab')
        self.trace = (directory / 'protocol.jsonl').open('a', encoding='utf-8')
        self.proc = subprocess.Popen(argv, cwd=workspace, stdin=subprocess.PIPE,
                                     stdout=subprocess.PIPE, stderr=self.err, text=True, bufsize=1)
        atomic(directory / 'native-process.json', {'pid': self.proc.pid, 'pgid': os.getpgid(self.proc.pid) if os.name != 'nt' else None})
        self.messages = queue.Queue(); self.counter = 0
        self.reader = threading.Thread(target=self._read, daemon=True); self.reader.start()
    def _read(self):
        try:
            for line in self.proc.stdout:
                try:
                    message = json.loads(line)
                    self.trace.write(json.dumps({k:message[k] for k in ('id','method') if k in message})+'\n'); self.trace.flush()
                    self.messages.put(message)
                except ValueError: self.messages.put({'transport_error': 'non-JSON ACP output'})
        finally: self.messages.put({'transport_error': 'native ACP process closed its output'})
    def send(self, value):
        self.proc.stdin.write(json.dumps(value, ensure_ascii=False) + '\n'); self.proc.stdin.flush()
    def request(self, method, params, startup_timeout=40):
        self.counter += 1; number = self.counter
        self.send({'jsonrpc':'2.0','id':number,'method':method,'params':params})
        while True:
            try: value = self.messages.get(timeout=startup_timeout)
            except queue.Empty as exc: raise RuntimeBlocked(f'DSH_STARTUP: no response to {method}') from exc
            if 'transport_error' in value: raise RuntimeBlocked(value['transport_error'])
            if 'method' in value and 'id' in value:
                if value['method'] == 'session/request_permission':
                    raise RuntimeBlocked('DSH_INTERACTION_REQUIRED: native permission needs a user decision; none was sent')
                else:
                    self.send({'jsonrpc':'2.0','id':value['id'],'error':{'code':-32601,'message':'Client method not supported'}})
            elif value.get('id') == number:
                if 'error' in value:
                    error = value['error']; message = error.get('message', 'unknown error')
                    # Narrow host error classification: never treat timeouts/model errors
                    # as proof that a session was destroyed.
                    missing = message.startswith(('Invalid params: session is not resumable:', 'Session not found:'))
                    if method == 'session/resume' and error.get('code') in (-32602, -32004) and missing:
                        raise SessionUnavailable(f'DSH_SESSION_UNAVAILABLE: {message}',
                                                 identity=params.get('sessionId', ''), code=error.get('code'))
                    raise RuntimeBlocked(f'DSH_NATIVE {method}: {message}')
                return value.get('result', {})
    def close(self):
        try: self.proc.stdin.close()
        except (OSError, BrokenPipeError): pass
        try: self.proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.proc.terminate()
            try: self.proc.wait(timeout=5)
            except subprocess.TimeoutExpired: self.proc.kill(); self.proc.wait()
        self.reader.join(timeout=2)
        self.err.close(); self.trace.close()

def selection(raw):
    return {o['id']:o.get('currentValue') for o in raw.get('configOptions', [])}

def make_patch(directory: Path, read_only: bool, probe=False) -> Path:
    patch = [
        {'insert':[{'id':'harness-native','name':str(Path(__file__).with_suffix('.mjs')),
                    'inject':['tools', *(['sandboxPolicy'] if read_only else [])],
                    'config':{'directory':str(directory), 'read_only':read_only}}]},
        {'id':'acp','inject':['acpAppStartup','harnessNativeReady']},
    ]
    if probe:
        patch.append({'id':'session-title-llm','disabled':True})
    path = directory / 'profile.json'; atomic(path, patch); return path


from .native_result import decode_result

def observe(directory: Path, identity: str) -> dict:
    source = read(directory / 'native-source.json')
    if source.get('sessionId') != identity or not isinstance(source.get('turn'), int):
        raise RuntimeBlocked('DSH_BINDING: native source does not match the session')
    answer = ''; ended = False; usage = []
    with (directory / 'native-events.jsonl').open() as file:
        for line in file:
            record = json.loads(line); event = record['event']; data = event.get('data', {})
            if record.get('sessionId') != identity: raise RuntimeBlocked('DSH_BINDING: mixed session evidence')
            if event['type'] == 'compaction/summary': continue
            if data.get('turn') != source['turn']: continue
            if event['type'] == 'assistant/message':
                if ended: raise RuntimeBlocked('DSH_RESULT: message after turn ended')
                if isinstance(data.get('usage'),dict): usage.append({'turn':source['turn'],'step':data.get('step'),'event':'assistant/message','usage':data['usage']})
                answer = ''.join(part.get('text','') for part in data.get('message',{}).get('content',[]) if part.get('type')=='text')
            elif event['type'] == 'assistant/attempt':
                # Preserve native usage, never infer or fill a missing counter with zero.
                if data.get('usage'): usage.append({'turn':source['turn'],'step':data.get('step'),'event':'assistant/attempt','usage':data['usage']})
            elif event['type'] == 'turn/end':
                if ended or data.get('reason',{}).get('kind') != 'completed':
                    raise RuntimeBlocked(f"DSH_RESULT: turn was not completed: {data.get('reason')}")
                ended = True
    if not ended: raise RuntimeBlocked('DSH_RESULT: committed turn/end evidence is missing')
    # Keep original bytes; reconciliation never rewrites the native evidence.
    raw = directory / 'answer.raw.txt'
    if raw.exists():
        if raw.read_text() != answer:
            raise RuntimeBlocked('DSH_RESULT: saved final output differs from native evidence')
    else:
        raw.write_text(answer)
    result = decode_result(answer)
    atomic(directory / 'answer.json', result)
    return {'provider':'dsh','identity':identity,'native_turn':source['turn'],
            'transcript':source.get('transcriptPath',''),'usage_evidence':usage or None,
            'files': {name: file_proof(directory / name) for name in
                      ('native-source.json', 'native-events.jsonl', 'answer.raw.txt', 'answer.json')}}


def reconcile_completed_output(directory: Path, process: dict) -> dict:
    """Recover only a completed native turn rejected by the old formatting parser.

    Original process exit/error records remain unchanged. No host is started and
    the Go role validator must still accept the recovered business result.
    """
    legacy_error = 'RuntimeBlocked: DSH_RESULT: final message is not a JSON result object'
    error_path = directory / 'native-error.json'
    if process.get('exit_code') != 1 or process.get('error') or not error_path.exists():
        raise RuntimeBlocked('DSH_RECONCILE: not a completed formatting-only failure')
    original_error=read(error_path).get('error','')
    if original_error != legacy_error and not original_error.startswith('RuntimeBlocked: NATIVE_RESULT:'):
        raise RuntimeBlocked('DSH_RECONCILE: failure is not eligible for formatting recovery')
    identity = read(directory / 'identity.json')
    request = read(directory / 'native-request.json')
    if identity['workspace'] != request['workspace']:
        raise RuntimeBlocked('DSH_RECONCILE: workspace changed')
    for option in ('model', 'reasoning_effort'):
        if request.get(option) and identity.get('selection', {}).get(option) != request[option]:
            raise RuntimeBlocked('DSH_RECONCILE: original model selection differs')
    source = observe(directory, identity['identity'])
    source['selection'] = identity.get('selection', {})
    source['reconciliation'] = {'kind': 'completed-output-format', 'original_exit_code': 1,
                                'original_error': original_error, 'new_model_calls': 0}
    atomic(directory / 'source.json', source)
    return source

def execute(request: dict, directory: Path, probe=False) -> dict:
    directory.mkdir(parents=True, exist_ok=True)
    binding = request.get('binding') or capture_binding()
    executable = binding['launch']['executable']
    workspace = str(Path(request['workspace']).resolve())
    patch = make_patch(directory, request['read_only'], probe)
    client = Client([executable,'--profile','acp','--patch',str(patch)],workspace,directory)
    identity = request.get('identity',''); attached = False; configured = {}
    try:
        initialize = client.request('initialize',{'protocolVersion':1,'clientCapabilities':{}})
        capabilities = initialize.get('agentCapabilities',{}).get('sessionCapabilities',{})
        if initialize.get('protocolVersion') != 1 or 'resume' not in capabilities or 'close' not in capabilities:
            raise RuntimeBlocked('DSH_CAPABILITY: ACP v1 resume/close required')
        # ACP requires the additional server list. DSH mounts it in the agent
        # scope; an empty list does not remove servers from the native profile.
        params = {'cwd':workspace,'mcpServers':[]}
        if identity: params['sessionId'] = identity
        created = client.request('session/resume' if identity else 'session/new', params)
        observed_identity = created.get('sessionId', identity)
        if not observed_identity or (identity and observed_identity != identity): raise RuntimeBlocked('DSH_BINDING: missing or changed identity')
        identity = observed_identity; attached = True; configured = selection(created)
        atomic(directory / 'identity.json', {'identity':identity,'workspace':workspace,'selection':configured})
        for name, requested in [('model',request.get('model','')),('reasoning_effort',request.get('reasoning_effort',''))]:
            if requested and requested != configured.get(name):
                changed = client.request('session/set_config_option',{'sessionId':identity,'configId':name,'value':requested})
                configured.update(selection(changed))
                if configured.get(name) != requested: raise RuntimeBlocked(f'DSH_CONFIG: {name} selection was not applied')
        if probe:
            client.request('session/close',{'sessionId':identity}); attached = False
            return {'provider':'dsh','executable':str(Path(executable).resolve()),'capabilities':capabilities,'selection':configured,'model_calls':0}
        prompt = request['prompt'] + '\nReturn ONLY one JSON object matching this result schema:\n' + json.dumps(request['schema'],ensure_ascii=False)
        response = client.request('session/prompt',{'sessionId':identity,'prompt':[{'type':'text','text':prompt}]},startup_timeout=None)
        if response.get('stopReason') != 'end_turn': raise RuntimeBlocked(f"DSH_STOP: {response.get('stopReason')}")
        # close flushes the native source. It does not delete persisted session history.
        client.request('session/close',{'sessionId':identity}); attached = False
        source = observe(directory, identity); source['selection'] = configured
        atomic(directory / 'source.json',source)
        return source
    finally:
        if attached:
            try: client.request('session/close',{'sessionId':identity},startup_timeout=10)
            except (HarnessError,OSError): pass
        client.close()



def capture_binding():
    executable = shutil.which('dsh')
    if not executable: raise RuntimeBlocked('DSH_UNAVAILABLE: dsh is not on PATH')
    path = Path(executable).resolve()
    return {'provider':'dsh','launch':{'executable':str(path)},'files':{str(path):file_proof(path)}}
