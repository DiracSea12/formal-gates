"""DSH ACP transport. No graph imports and no workflow state authority.

Uses existing native sandbox/tool restrictions. A normal ACP stopReason is not
proof of a successful turn: committed native turn/end evidence is required.

Only the ACP protocol lives here; process handling, the request/reply loop,
bindings and evidence come from native_transport.
"""
from __future__ import annotations
import json
from pathlib import Path
import shutil
from .infra import RuntimeBlocked, SessionUnavailable, atomic, read
from .native_result import decode_result
from .native_transport import (Client as Transport, UNBOUNDED, close_session,
                               capture_binding as capture, evidence_source,
                               reconcile_completed_output as reconcile,
                               session_events, transport_files, write_answer)

TRANSPORT_FILES = transport_files(Path(__file__).name, 'native_dsh.mjs')

# ACP bounds startup; a prompt waits for the host to finish its own turn.
STARTUP_TIMEOUT = 40


class Client(Transport):
    """ACP's JSON-RPC envelope, client-directed replies and error classification."""

    transport_prefix = 'DSH_TRANSPORT'
    request_prefix = 'DSH_NATIVE'
    default_timeout = STARTUP_TIMEOUT
    # The host decides when a turn ends, so no request carries a total budget.
    bounded = False
    # ACP reports a dead reader as a transport error rather than a clean close.
    closed_key = 'transport_error'
    closed_message = 'native ACP process closed its output'
    invalid_message = 'non-JSON ACP output'

    def ident(self):
        self.counter += 1
        return self.counter

    def envelope(self, value):
        return {'jsonrpc': '2.0', **value}

    def respond(self, method, params):
        if method == 'session/request_permission':
            # A user decision belongs to the user; never approve or deny it here.
            raise RuntimeBlocked('DSH_INTERACTION_REQUIRED: native permission needs a user decision; none was sent')
        return None

    def classify(self, method, error, params):
        # Narrow classification: never treat a timeout or a model error as proof
        # that a session was destroyed.
        message = error.get('message', 'unknown error')
        missing = message.startswith(('Invalid params: session is not resumable:', 'Session not found:'))
        if method == 'session/resume' and error.get('code') in (-32602, -32004) and missing:
            return SessionUnavailable(f'DSH_SESSION_UNAVAILABLE: {message}',
                                      identity=params.get('sessionId', ''), code=error.get('code'))
        return None


def selection(raw):
    return {o['id']: o.get('currentValue') for o in raw.get('configOptions', [])}


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


def observe(directory: Path, identity: str) -> dict:
    """Turn committed native turn evidence into the bound source receipt."""
    source = read(directory / 'native-source.json')
    if source.get('sessionId') != identity or not isinstance(source.get('turn'), int):
        raise RuntimeBlocked('DSH_BINDING: native source does not match the session')
    answer = ''; ended = False; usage = []
    for record in session_events(directory, identity):
        event = record['event']; data = event.get('data', {})
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
    write_answer(directory, answer, 'DSH')
    return evidence_source(directory, 'dsh', identity, native_turn=source['turn'],
                           transcript=source.get('transcriptPath',''), usage_evidence=usage or None)


def reconcile_completed_output(directory: Path, process: dict) -> dict:
    """Recover only a completed native turn rejected by the old formatting parser.

    Original process exit/error records remain unchanged. No host is started and
    the Go role validator must still accept the recovered business result.
    """
    legacy_error = 'RuntimeBlocked: DSH_RESULT: final message is not a JSON result object'
    def recover():
        identity = read(directory / 'identity.json'); request = read(directory / 'native-request.json')
        if identity['workspace'] != request['workspace']:
            raise RuntimeBlocked('DSH_RECONCILE: workspace changed')
        for option in ('model', 'reasoning_effort'):
            if request.get(option) and identity.get('selection', {}).get(option) != request[option]:
                raise RuntimeBlocked('DSH_RECONCILE: original model selection differs')
        source = observe(directory, identity['identity']); source['selection'] = identity.get('selection', {})
        return source
    return reconcile(directory, process, 'DSH',
                     lambda original: original == legacy_error
                     or original.startswith('RuntimeBlocked: NATIVE_RESULT:'),
                     recover)


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
        response = client.request('session/prompt',{'sessionId':identity,'prompt':[{'type':'text','text':prompt}]},timeout=UNBOUNDED)
        if response.get('stopReason') != 'end_turn': raise RuntimeBlocked(f"DSH_STOP: {response.get('stopReason')}")
        # close flushes the native source. It does not delete persisted session history.
        client.request('session/close',{'sessionId':identity}); attached = False
        source = observe(directory, identity); source['selection'] = configured
        atomic(directory / 'source.json',source)
        return source
    finally:
        if attached: close_session(client, identity)
        client.close()


def capture_binding():
    executable = shutil.which('dsh')
    if not executable: raise RuntimeBlocked('DSH_UNAVAILABLE: dsh is not on PATH')
    return capture('dsh', {'executable': str(Path(executable).resolve())})
