"""ZCode app-server transport, reusing the original native event contract.

Only the ZCode protocol lives here; process handling, the request/reply loop,
bindings and evidence come from native_transport. No graph imports, workflow
callbacks, global configuration writes or model substitution. Only read-only
roles narrow native tools. Terminals require exact session/input/turn IDs.

The app-server speaks "ZCode Protocol v1". It applies no selection of its own: a
client names an exact provider/model reference and reads the result back.
"""
from __future__ import annotations
import json
import os
from pathlib import Path
import shutil
import time
from .infra import RuntimeBlocked, SessionUnavailable, atomic, read, canonical
from .native_transport import (Client as Transport, Reply, close_session,
                               capture_binding as capture, evidence_source, launch_environment,
                               reconcile_completed_output as reconcile, session_events,
                               transport_files, write_answer)

TRANSPORT_FILES=transport_files(Path(__file__).name)

# App-server has no read-only mode. Its native allowlist also governs MCP;
# arbitrary command/plugin executors cannot enforce this role's write boundary.
READ_ONLY_TOOLS=['Read','Glob','Grep','WebSearch','WebFetch']

# The handshake every snapshot carries. A different one means the app-server no
# longer speaks the protocol this transport was accepted against.
PROTOCOL_NAME='ZCode Protocol'
PROTOCOL_VERSION=1

# The host's own name for "this session is gone", used on resume and on any
# call against a session the host no longer holds.
SESSION_UNAVAILABLE=-32004

DEFAULT_SCRIPT='/Applications/ZCode.app/Contents/Resources/glm/zcode.cjs'

# The app-server reads its built-in provider catalogue from a file, or from one
# of two paths it derives from its own script location. A launcher may name that
# file instead; see bundled_provider_catalogue().
BUILTIN_PROVIDER_CATALOGUE_ENV='ZCODE_BUILTIN_PROVIDER_CONFIG_FILE'

# Result-framing failures only. Runtime and transport failures are never
# laundered into a successful worker result.
RESULT_FRAME_ERROR='RuntimeBlocked: NATIVE_RESULT:'


def bundled_provider_catalogue(script):
    """Name the app-server's bundled provider catalogue when the host cannot.

    The app-server resolves that catalogue from the script's own directory:
    `<script dir>/provider/zcode-builtin.json`, then a source-tree-relative
    `../../../../../config/provider/zcode-builtin.json`. The 3.12.3 bundle ships
    it at `<Resources>/config/provider/zcode-builtin.json`, which neither
    candidate reaches, so the app-server exits 1 before emitting any protocol.

    The host defines ZCODE_BUILTIN_PROVIDER_CONFIG_FILE so a launcher can name
    that file; the host still computes the *active* catalogue it maintains and
    refreshes itself, and takes ours as the bundled seed. It is returned only
    when the host's own candidates are absent, so a host whose lookup already
    works keeps its current behaviour unchanged. Nothing is written anywhere.

    This names host configuration only. It does not seed, register or select a
    provider: the host's own account configuration decides what is selectable.
    """
    directory=script.parent
    for candidate in (
        directory/'provider'/'zcode-builtin.json',
        Path(os.path.normpath(os.path.join(str(directory),'../../../../../config/provider/zcode-builtin.json'))),
    ):
        if candidate.is_file(): return None
    resources=directory.parent/'config'/'provider'/'zcode-builtin.json'
    return resources if resources.is_file() else None


def capture_binding():
    node=shutil.which('node')
    script=Path(os.environ.get('FORMAL_GATES_ZCODE_SCRIPT',DEFAULT_SCRIPT)).expanduser().resolve()
    if not node or not script.is_file(): raise RuntimeBlocked('ZCODE_UNAVAILABLE: node or app-server script missing')
    launch={'executable':str(Path(node).resolve()),'script':str(script)}
    # The catalogue is host configuration the host refreshes on its own, so it is
    # named in the launch environment but not proofed like an executed binary.
    catalogue=bundled_provider_catalogue(script)
    return capture('zcode',launch,
                   {BUILTIN_PROVIDER_CATALOGUE_ENV:str(catalogue)} if catalogue else None)


class Client(Transport):
    """ZCode's client-directed replies and error classification."""

    transport_prefix='ZCODE_TRANSPORT'
    request_prefix='ZCODE_NATIVE'

    def __init__(self,launch,workspace,directory,environment=None):
        super().__init__([launch['executable'],launch['script'],'--cwd',workspace,'app-server'],
                         workspace,directory,environment)

    def respond(self,method,params):
        if method=='session/requestRuntimePreferences':
            # ZCode defines this response as its native compatibility fallback:
            # the host then keeps its own preferences instead of ours.
            return Reply({'error':{'code':-32601,'message':'Use native runtime preferences fallback'}},
                         'native-default-fallback')
        if method=='interaction/requestOfficialMcpAuthHeaders':
            # No desktop authentication port is available on this connection.
            return Reply({'result':{'ok':False,'reason':'official_auth_unavailable'}},
                         'official_auth_unavailable')
        if method in ('interaction/requestPermission','interaction/requestUserInput',
                      'interaction/requestProviderRuntimeHeaders'):
            raise RuntimeBlocked('ZCODE_INTERACTION_REQUIRED: '+method+' needs a native interactive client; no user decision was sent')
        return None

    def classify(self,method,error,params):
        if (method=='session/resume' and error.get('code')==SESSION_UNAVAILABLE
                and error.get('message','').startswith('Session not found:')):
            return SessionUnavailable('ZCODE_SESSION_UNAVAILABLE: '+error['message'],
                                      identity=params.get('sessionId',''),code=error['code'])
        return None

    def notification(self,message):
        if message.get('method')!='session/event': return
        event=message.get('params')
        # Child sessions are separate evidence; only the bound root is recorded.
        if not self.identity or not isinstance(event,dict) or event.get('sessionId')!=self.identity: return
        self.events.append(event)
        with (self.directory/'native-events.jsonl').open('a') as f:f.write(json.dumps(event,ensure_ascii=False)+'\n');f.flush()


def reference(value):
    """The exact provider/model reference a bound selection names.

    A bare model name cannot be resolved: this version exposes no catalogue to
    resolve it against, so borrowing another provider's identity is not possible
    and would be a substitution anyway.
    """
    if not value: return None
    try: ref=json.loads(value)
    except ValueError as exc:
        raise RuntimeBlocked('ZCODE_CONFIG: a bound selection must name a provider and a model') from exc
    if not isinstance(ref,dict) or set(ref)!={'providerId','modelId'}:
        raise RuntimeBlocked('ZCODE_CONFIG: a bound selection must name exactly one provider and model')
    return ref


def selected_model(snapshot):
    """The exact native selection the host reports, or None if it reports none."""
    model=((snapshot.get('settings') or {}).get('model') or {}).get('current')
    return model if isinstance(model,dict) and set(model)=={'providerId','modelId'} else None


def selection(snapshot):
    """The recorded selection: the host's own reported values, never our wish."""
    model=selected_model(snapshot)
    thoughtLevel=(snapshot.get('settings') or {}).get('thoughtLevel') or {}
    return {'model':canonical(model).decode() if model else '','reasoning_effort':thoughtLevel.get('current') or ''}


def advertised_models(snapshot):
    """Every exact reference the host currently advertises to this client."""
    available=((snapshot.get('settings') or {}).get('model') or {}).get('available') or []
    return [canonical(entry['ref']).decode() for entry in available
            if isinstance(entry,dict) and isinstance(entry.get('ref'),dict)
            and set(entry['ref'])=={'providerId','modelId'}]


def apply_selection(client,snapshot,wanted,effort,directory,workspace):
    """Apply the bound selection and read the host back; never substitute.

    A host may silently ignore a setting it does not support (an unavailable
    thought level is dropped with a warning), so every value is confirmed from
    the host's own report before it is recorded. Nothing is persisted to the
    workspace's last-used selection.
    """
    configured=selection(snapshot)
    if wanted:
        advertised=advertised_models(snapshot)
        if advertised and canonical(wanted).decode() not in advertised:
            raise RuntimeBlocked('ZCODE_CONFIG: requested exact provider/model is not advertised; no fallback')
        if configured['model']!=canonical(wanted).decode():
            changed=client.request('session/setModel',{'sessionId':client.identity,'model':wanted,
                                                       'persistAsWorkspaceLastUsed':False})
            if changed.get('settings'):configured=selection(changed)
            if configured['model']!=canonical(wanted).decode():
                raise RuntimeBlocked('ZCODE_CONFIG: requested selection was not applied')
    elif not configured['model']:
        raise RuntimeBlocked('ZCODE_CONFIG: native selected model missing')
    if effort and configured['reasoning_effort']!=effort:
        changed=client.request('session/setThoughtLevel',{'sessionId':client.identity,'thoughtLevel':effort,
                                                          'persistAsWorkspaceLastUsed':False})
        if changed.get('settings'):configured=selection(changed)
        if configured['reasoning_effort']!=effort:
            raise RuntimeBlocked('ZCODE_CONFIG: reasoning setting was not applied')
    atomic(directory/'identity.json',{'identity':client.identity,'provider':'zcode',
                                      'workspace':workspace,'selection':configured})
    return configured


def handshake(snapshot):
    """The protocol both sides agreed on, or a refusal to guess."""
    protocol=snapshot.get('protocol') or {}
    if protocol.get('name')!=PROTOCOL_NAME or protocol.get('version')!=PROTOCOL_VERSION:
        raise RuntimeBlocked(f'ZCODE_PROTOCOL: the app-server does not speak {PROTOCOL_NAME} '
                             f'v{PROTOCOL_VERSION}: '+json.dumps(protocol,ensure_ascii=False))
    return protocol


def materialize_source(directory):
    """Turn committed native turn evidence into the bound source receipt."""
    binding=read(directory/'native-source.json');identity=binding['sessionId'];input_id=binding['inputId']
    turn='';end=None;seq=-1
    for event in session_events(directory,identity):
        number=event.get('seq')
        if not isinstance(number,int) or number<0: raise RuntimeBlocked('ZCODE_BINDING: invalid sequence')
        if number<=seq:continue
        seq=number;p=event.get('payload',{})
        if p.get('inputId')!=input_id:continue
        if event['type']=='turn.started':
            if turn or not event.get('turnId'):raise RuntimeBlocked('ZCODE_BINDING: duplicate/missing start')
            turn=event['turnId']
        if event['type'] in ('turn.failed','turn.completed'):
            if not turn or turn!=event.get('turnId') or end is not None: raise RuntimeBlocked('ZCODE_BINDING: terminal mismatch')
            if event['type']!='turn.completed' or p.get('resultType')!='success':raise RuntimeBlocked('ZCODE_EXECUTION: turn failed')
            end=event
    if end is None:raise RuntimeBlocked('ZCODE_RESULT: matching native completion is missing')
    write_answer(directory,end['payload'].get('response',''),'ZCODE')
    return evidence_source(directory,'zcode',identity,native_turn=turn,input_id=input_id,
                           selection=binding['selection'],usage_evidence=end['payload'].get('usage'))


def reconcile_completed_output(directory,process):
    # Only result-framing errors can be replayed; runtime/transport failures are
    # never laundered into a successful worker result.
    return reconcile(directory,process,'ZCODE',
                     lambda original: original.startswith(RESULT_FRAME_ERROR),
                     lambda: materialize_source(directory))


def execute(request,directory,probe=False):
    directory=Path(directory).resolve();directory.mkdir(parents=True,exist_ok=True)
    workspace=str(Path(request['workspace']).resolve());binding=request.get('binding') or capture_binding()
    previous=request.get('identity','');wanted=reference(request.get('model',''))
    effort=request.get('reasoning_effort','')
    location={'workspacePath':workspace,'workspaceKey':workspace}
    # A new session is persisted immediately; a resume addresses the recorded one.
    params={'workspace':location,'sessionId':previous} if previous else {'workspace':location,'persistence':'immediate'}
    if request['read_only']:params['toolAllowlist']=READ_ONLY_TOOLS
    if probe and not previous:params['titleGenerationEnabled']=False
    client=Client(binding['launch'],workspace,directory,launch_environment(binding));attached=False
    try:
        snapshot=client.request('session/resume' if previous else 'session/create',params)
        protocol=handshake(snapshot)
        identity=(snapshot.get('session') or {}).get('sessionId','')
        if not identity or previous and identity!=previous:raise RuntimeBlocked('ZCODE_BINDING: missing or changed identity')
        client.identity=identity;attached=True
        atomic(directory/'identity.json',{'identity':identity,'provider':'zcode','workspace':workspace,
                                          'selection':selection(snapshot)})
        configured=apply_selection(client,snapshot,wanted,effort,directory,workspace)
        if request['read_only']:
            atomic(directory/'tool-policy.json',{'read_only':True,'allowed':READ_ONLY_TOOLS})
        if probe:
            client.request('session/close',{'sessionId':identity});attached=False
            return {'provider':'zcode','protocol':protocol,'selection':configured,'binding':binding,
                'model_calls':0,'interactions':client.interactions,'native_tools':'not_verified',
                'mcp_auth':'unavailable' if any(i['status']=='official_auth_unavailable' for i in client.interactions) else 'not_verified'}
        subscribed=client.request('session/subscribe',{'sessionId':identity,'deliveryKind':'desktop-continuous','includeSnapshot':False})
        if subscribed.get('sessionId')!=identity:raise RuntimeBlocked('ZCODE_BINDING: subscription identity differs')
        input_id='formal-gates-'+directory.name
        atomic(directory/'native-source.json',{'sessionId':identity,'inputId':input_id,'selection':configured})
        prompt=request['prompt']+'\nReturn ONLY one JSON object matching this schema:\n'+json.dumps(request['schema'],ensure_ascii=False)
        ack=client.request('session/send',{'sessionId':identity,'inputId':input_id,'content':prompt})
        if ack.get('sessionId')!=identity or not ack.get('accepted'):raise RuntimeBlocked('ZCODE_BINDING: input not accepted')
        position=0;terminal=False;deadline=time.monotonic()+900
        while not terminal and time.monotonic()<deadline:
            while position<len(client.events):
                event=client.events[position];position+=1
                if event.get('sessionId')!=identity:raise RuntimeBlocked('ZCODE_BINDING: event from another session')
                if event.get('payload',{}).get('inputId')==input_id and event['type'] in ('turn.completed','turn.failed'):terminal=True
            if not terminal:client.receive(min(120,max(1,deadline-time.monotonic())))
        if not terminal:raise RuntimeBlocked('ZCODE_TIMEOUT: turn incomplete; inspect the original invocation')
        client.request('session/close',{'sessionId':identity});attached=False
        # Finalize proofs only after native shutdown messages have been drained.
        return materialize_source(directory)
    finally:
        if attached:close_session(client,client.identity)
        client.close()
