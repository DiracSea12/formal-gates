"""ZCode app-server transport, reusing the original native event contract.

Only the ZCode protocol lives here; process handling, the request/reply loop,
bindings and evidence come from native_transport. No graph imports, workflow
callbacks, global configuration writes or model substitution. Only read-only
roles narrow native tools. Terminals require exact session/input/turn IDs.

The app-server speaks "ZCode Protocol v1". It applies no selection of its own: a
client names an exact provider/model reference and reads the result back.

A launcher that starts the app-server itself is also the side that has to hand
over the host's account provider configuration, because the agent keeps no
registry of its own: without it the app-server reports an empty provider list
and no current model at all. `account_provider_configuration` rebuilds that
overlay from the host's own provisioning index.

Handing over the overlay is not enough to finish a turn. The same host asks its
client for the request credential before every model request and abandons the
turn when none comes back, so `respond` answers that one interaction from the
same provisioning index -- see request_auth().
"""
from __future__ import annotations
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time
from .infra import RuntimeBlocked, SessionUnavailable, atomic, read, canonical
from .native_transport import (Client as Transport, Reply, close_session,
                               capture_binding as capture, evidence_source, launch_environment,
                               reconcile_completed_output as reconcile, session_events,
                               transport_files, write_answer)

TRANSPORT_FILES=transport_files(Path(__file__).name,'zcode_credential.mjs')

# App-server has no read-only mode. Its native allowlist also governs MCP;
# arbitrary command/plugin executors cannot enforce this role's write boundary.
READ_ONLY_TOOLS=['Read','Glob','Grep','WebSearch','WebFetch']

# Unattended operator context for development/repair sessions. A headless
# session cannot inherit the operator's interactive context: every new session
# hard-defaults to 'build' (approval-gated), no host setting changes that
# default, and an unattended client cannot answer approvals. The mode is
# declared once per NEW session before its first turn; resumed sessions keep
# the mode they already have and nothing is ever changed in reaction to a
# prompt, timeout or failure.
DEV_PERMISSION_MODE='yolo'

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

# The app-server's own bootstrap recomputes the variable above from
# ~/.zcode/v2/runtime/provider/... unless this one is named as well, so a
# launcher that wants the agent to resolve a catalogue it named has to name both.
PERSONAL_PROVIDER_CONFIG_ENV='ZCODE_PERSONAL_PROVIDER_CONFIG_FILE'
PERSONAL_PROVIDER_CONFIG='provider_config.json'

# The host's own credential store. Its key names are the host's provisioning
# index for account providers and are plaintext; its values are encrypted account
# secrets, read only by zcode_credential.mjs and never recorded.
CREDENTIALS_FILE='credentials.json'
ACCOUNT_CREDENTIAL_PREFIX='account-provider:coding-plan:'
ACCOUNT_CREDENTIAL_SUFFIX=':api-key'
ACCOUNT_IDENTITY_SEPARATOR=':account:'

# Reading one credential out of that store. The host's cipher is local, so this
# needs neither keychain access nor a user decision.
CREDENTIAL_HELPER='zcode_credential.mjs'
CREDENTIAL_HELPER_ENV='FORMAL_GATES_ZCODE_CREDENTIALS'

# The host asks its client for request auth before every model request. A
# launcher that started the app-server is that client, and a turn cannot reach a
# provider without an answer. See request_auth().
RUNTIME_HEADERS_METHOD='interaction/requestProviderRuntimeHeaders'

# The account provider overlay a host delivers once provider/model are ready.
ACCOUNT_CONFIG_METHOD='provider/updateAccountConfig'
ACCOUNT_ACCESS_TYPE='zhipu-account'

# The host records its own selected plan here, as a family plus a plan kind.
SETTING_FILE='setting.json'
SETTING_FAMILY_KEY='providerFamilyDomain'
SETTING_PLANS_KEY='providerFamilyConnectionSelections'
ACCESS_FAMILY_KEY='accountType'
ACCESS_MODE_KEY='mode'

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


def host_data_directory():
    """The base directory the host resolves its own configuration against."""
    configured=os.environ.get('ZCODE_DATA_BASE_DIR','').strip()
    return Path(configured) if configured else Path.home()


def provider_environment(script):
    """The provider configuration variables a launcher must name together.

    Naming only the catalogue is not enough: the app-server's own bootstrap
    recomputes it from a path it derives under the host data directory unless the
    personal provider config is named too. Naming both keeps the catalogue the
    launcher chose, which is what lets the launcher know the revision the agent
    answers with -- see builtin_provider_revision(). Returns None when the host
    resolves its own catalogue, so such a host keeps its current behaviour.
    """
    catalogue=bundled_provider_catalogue(script)
    if not catalogue: return None
    personal=host_data_directory()/'.zcode'/'v2'/PERSONAL_PROVIDER_CONFIG
    return {BUILTIN_PROVIDER_CATALOGUE_ENV:str(catalogue),
            PERSONAL_PROVIDER_CONFIG_ENV:str(personal)}


def pinned_catalogue(binding):
    """The catalogue a launcher pinned this binding to, or '' for a host's own."""
    return (binding.get('environment') or {}).get(BUILTIN_PROVIDER_CATALOGUE_ENV,'')


def capture_binding():
    node=shutil.which('node')
    script=Path(os.environ.get('FORMAL_GATES_ZCODE_SCRIPT',DEFAULT_SCRIPT)).expanduser().resolve()
    if not node or not script.is_file(): raise RuntimeBlocked('ZCODE_UNAVAILABLE: node or app-server script missing')
    launch={'executable':str(Path(node).resolve()),'script':str(script)}
    # The catalogue is host configuration the host refreshes on its own, so it is
    # named in the launch environment but not proofed like an executed binary.
    return capture('zcode',launch,provider_environment(script))


def credential_store():
    """The host's own account credential store, as the host resolves it."""
    override=os.environ.get(CREDENTIAL_HELPER_ENV,'').strip()
    if override: return Path(override)
    return host_data_directory()/'.zcode'/'v2'/CREDENTIALS_FILE


def request_auth(provider):
    """The request credential the host asks its client for, or None.

    The app-server holds no account credential it can spend on its own: in host
    mode its provider runtime headers port reports
    ``shouldRefreshBeforeModelRequest() === true`` unconditionally, so before every
    model request it asks the client for the request auth and abandons the turn
    when the reply carries none. A launcher that spawned the app-server is that
    client, so supplying the credential the host itself provisioned is part of
    starting it, not a substitution of its choice.

    Only this account's own provisioned value is read, through the host's cipher,
    which is local (see zcode_credential.mjs): no keychain access and no user
    decision are involved. The value is never returned into evidence -- answers go
    through Client.answer, which records the method and the status only -- and is
    never logged, written or reported by this transport.
    """
    if not provider: return None
    node=shutil.which('node')
    helper=Path(__file__).with_name(CREDENTIAL_HELPER)
    if not node or not helper.is_file(): return None
    try:
        done=subprocess.run([node,str(helper),provider,str(credential_store())],
                            stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=60)
    except (OSError,subprocess.SubprocessError):
        return None
    return done.stdout.strip() or None


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
        if method==RUNTIME_HEADERS_METHOD:
            # The one interaction with a machine answer: the credential this host
            # provisioned for the provider it is about to call. Answered from the
            # host's own index rather than refused, because in host mode the host
            # cannot reach a provider without it. When the host has no such
            # credential the reply says so and the host reports the failure itself,
            # so a missing credential is never laundered into a denied turn.
            provider=params.get('providerId') or ''
            secret=request_auth(provider)
            if not secret:
                return Reply({'result':{'headersApplied':False,
                                        'errorMessage':'no provisioned request credential for '+provider}},
                             'provider-runtime-headers-unavailable')
            return Reply({'result':{'headersApplied':True,'requestAuth':{'apiKey':secret}}},
                         'provider-runtime-headers-applied')
        if method in ('interaction/requestPermission','interaction/requestUserInput'):
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
    """The exact native selection the host reports, or None if it reports none.

    A configured host reports the selection together with the options bound to it,
    the reasoning level among them, so only the reference is taken: the identity
    this transport binds -- and hands back to a later run -- is exactly one
    provider and one model. A selection the host does not report stays absent
    rather than being filled in from its defaults.
    """
    current=((snapshot.get('settings') or {}).get('model') or {}).get('current')
    if not isinstance(current,dict): return None
    ref={key:current.get(key) for key in ('providerId','modelId')}
    return ref if all(isinstance(value,str) and value for value in ref.values()) else None


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


def advertised_reasoning(snapshot,ref):
    """The reasoning levels the host itself advertises for one exact reference.

    `session/setModel` takes an object and refuses one without
    `options.reasoningLevel`, and the level has to be one of this model's own
    values. The host already reports both, so they are read from it: a level this
    transport picked itself would either be refused or silently describe a
    capability the model does not have. The fallback to the first advertised value
    mirrors the host's own default when it reports no default at all.
    """
    for entry in ((snapshot.get('settings') or {}).get('model') or {}).get('available') or []:
        if not isinstance(entry,dict) or entry.get('ref')!=ref: continue
        reasoning=entry.get('reasoning') if isinstance(entry.get('reasoning'),dict) else {}
        levels=[level['value'] for level in reasoning.get('levels') or []
                if isinstance(level,dict) and isinstance(level.get('value'),str) and level['value']]
        default=reasoning.get('defaultLevel')
        return {'levels':levels,'default':default if default in levels else (levels[0] if levels else '')}
    return {'levels':[],'default':''}


def bound_model(snapshot,wanted,effort):
    """The exact model selection to apply, carrying the reasoning level with it.

    A bound selection names one exact provider/model; the level is not part of
    that identity, so it comes from the request when one was asked for and from the
    host's own advertised values otherwise. A model that advertises no levels is
    left exactly as it was bound.
    """
    if not wanted: return wanted
    reported=advertised_reasoning(snapshot,wanted)
    level=effort or reported['default']
    return {**wanted,**({'options':{'reasoningLevel':level}} if level else {})}


def builtin_provider_revision(catalogue):
    """The revision the app-server answers with for this catalogue.

    It is `<catalogue revision>` plus the sha256 of the catalogue's resolved path,
    not of its contents. ProviderRegistryService abandons an account refresh whose
    basedOnZCodeBuiltinRevision differs from it, which is why a launcher that hands
    over account configuration has to own the path the agent resolves.
    """
    resolved=os.path.abspath(str(Path(catalogue).expanduser()))
    try:
        document=json.loads(Path(resolved).read_text())
    except (OSError,ValueError) as exc:
        raise RuntimeBlocked(f'ZCODE_ACCOUNT_CONFIG: cannot read the named provider catalogue {resolved}') from exc
    revision=document.get('revision') if isinstance(document,dict) else None
    if not isinstance(revision,(int,str)) or isinstance(revision,bool) or str(revision)=='':
        raise RuntimeBlocked(f'ZCODE_ACCOUNT_CONFIG: the named provider catalogue has no revision: {resolved}')
    return f'zcode-builtin:{revision}:{hashlib.sha256(resolved.encode()).hexdigest()}'


def catalogue_account_rules(catalogue):
    """Every account provider the named catalogue declares, mapped to its access rule."""
    try:
        rules=json.loads(Path(catalogue).read_text())['config']['providerConfigRules']['providerRules']
    except (OSError,ValueError,KeyError,TypeError) as exc:
        raise RuntimeBlocked(f'ZCODE_ACCOUNT_CONFIG: cannot read provider rules from {catalogue}') from exc
    declared={}
    for rule in rules if isinstance(rules,list) else []:
        if not isinstance(rule,dict): continue
        provider=rule.get('providerId');access=(rule.get('config') or {}).get('access')
        if isinstance(provider,str) and provider and isinstance(access,dict) \
                and access.get('type')==ACCOUNT_ACCESS_TYPE:
            declared[provider]=access
    return declared


def catalogue_account_providers(catalogue):
    """Every account provider the named catalogue declares, in its own order."""
    return list(catalogue_account_rules(catalogue))


def host_selected_account_provider(catalogue,provisioned):
    """The provisioned account provider the host itself has selected, or ''.

    The host records its plan choice as a family and a plan kind, and the
    catalogue declares each account provider's own family and mode, so the two are
    matched on those declared fields -- never on the spelling of a provider id. The
    setting is host user-interface state that the agent does not depend on, so a
    missing or unreadable one means only that this host expresses no preference.
    """
    if not provisioned: return ''
    try:
        setting=json.loads((host_data_directory()/'.zcode'/'v2'/SETTING_FILE).read_text())
    except (OSError,ValueError):
        return ''
    if not isinstance(setting,dict): return ''
    family=setting.get(SETTING_FAMILY_KEY)
    plans=setting.get(SETTING_PLANS_KEY)
    kind=(plans or {}).get(family,{}).get('kind') if isinstance(plans,dict) and isinstance(family,str) else None
    if not isinstance(family,str) or not isinstance(kind,str): return ''
    declared=catalogue_account_rules(catalogue)
    for provider in provisioned:
        access=declared.get(provider) or {}
        if access.get(ACCESS_FAMILY_KEY)==family and access.get(ACCESS_MODE_KEY)==kind: return provider
    return ''


def provisioned_account_providers(catalogue):
    """Account providers the host's own credential store says are provisioned.

    The store's key names are the host's provisioning index and are plaintext
    (`account-provider:coding-plan:<provider>:account:<identity>:api-key`); its
    values are encrypted account secrets, which are never read. Only providers the
    named catalogue also declares are reported, so the overlay cannot name a
    provider the agent has no configuration for.
    """
    store=host_data_directory()/'.zcode'/'v2'/CREDENTIALS_FILE
    if not store.is_file(): return []
    try:
        index=json.loads(store.read_text())
    except (OSError,ValueError) as exc:
        raise RuntimeBlocked(f'ZCODE_ACCOUNT_CONFIG: cannot read the host credential store {store}') from exc
    if not isinstance(index,dict):
        raise RuntimeBlocked(f'ZCODE_ACCOUNT_CONFIG: the host credential store is not an index: {store}')
    entries=[key for key in index if isinstance(key,str)
             and key.startswith(ACCOUNT_CREDENTIAL_PREFIX) and key.endswith(ACCOUNT_CREDENTIAL_SUFFIX)]
    if not entries: return []
    known=catalogue_account_providers(catalogue);provisioned=[]
    for key in entries:
        body=key[len(ACCOUNT_CREDENTIAL_PREFIX):-len(ACCOUNT_CREDENTIAL_SUFFIX)]
        provider=body.split(ACCOUNT_IDENTITY_SEPARATOR)[0]
        if provider in known and provider not in provisioned: provisioned.append(provider)
    return provisioned


def account_provider_configuration(catalogue,wanted=None):
    """Rebuild the account provider overlay the host delivers, or None.

    A launcher-started app-server keeps no provider registry of its own, so the
    side that starts it has to hand one over. This reports the account state the
    host already holds -- its own provisioning index -- and never grants an
    entitlement the host does not have, so a host with no provisioned account
    provider stays empty and the transport reports that as it is.
    """
    providers=provisioned_account_providers(catalogue)
    if not providers: return None
    # The preferred provider is the one that was asked for, else the plan this host
    # itself selected, and only then the order its own credential index lists.
    named=wanted.get('providerId') if isinstance(wanted,dict) else None
    if named in providers: current, chosen = named, 'requested'
    else:
        selected=host_selected_account_provider(catalogue,providers)
        current, chosen = (selected,'host-selected-plan') if selected else (providers[0],'provisioning-order')
    base=builtin_provider_revision(catalogue)
    return {'based_on':base,'providers':providers,'current':current,'current_source':chosen,
            'configuration':{
                'revision':f'account-config:{base}:{",".join(providers)}',
                'basedOnZCodeBuiltinRevision':base,
                'providers':{provider:{'access':{'type':ACCOUNT_ACCESS_TYPE,'entitled':True}}
                             for provider in providers},
                'states':{provider:{'availability':'available','entitled':True,
                                    'current':provider==current} for provider in providers}}}


def deliver_account_providers(client,catalogue,wanted,directory):
    """Hand the host's account provider configuration to the app-server.

    Only a pinned catalogue is accepted: without it the launcher does not own the
    path whose hash is the revision, and a delivery whose basedOnZCodeBuiltinRevision
    is even slightly off is silently ignored by the agent rather than refused. The
    answer is read back and recorded, because a launcher that cannot fill the
    registry has to be visible as that and not as a missing model later on.
    """
    delivered=account_provider_configuration(catalogue,wanted)
    if not delivered: return None
    reply=client.request(ACCOUNT_CONFIG_METHOD,delivered['configuration'])
    status=reply.get('status') if isinstance(reply,dict) else None
    count=reply.get('providerCount') if isinstance(reply,dict) else None
    if status not in ('received','unchanged') or count!=len(delivered['providers']):
        raise RuntimeBlocked('ZCODE_ACCOUNT_CONFIG: the host did not accept the account '
                             'provider configuration: '+json.dumps(reply,ensure_ascii=False,sort_keys=True))
    atomic(directory/'account-provider.json',{'catalogue':catalogue,'based_on':delivered['based_on'],
                                              'providers':delivered['providers'],'current':delivered['current'],
                                              'current_source':delivered['current_source'],
                                              'status':status,'provider_count':count,
                                              'source':'host credential index'})
    return delivered


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
            changed=client.request('session/setModel',{'sessionId':client.identity,
                                                       'model':bound_model(snapshot,wanted,effort),
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
        # The app-server starts with an empty provider registry and only the side
        # that launched it can fill it, so the account provider configuration goes
        # first: a snapshot taken before it reports no current model at all.
        catalogue=pinned_catalogue(binding)
        if catalogue: deliver_account_providers(client,catalogue,wanted,directory)
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
        if not request['read_only']:
            # Evidence states the mode actually in force, never the constant: a
            # NEW dev/repair session declares the operator mode and records the
            # host's readback; a resumed session records the mode the host's own
            # snapshot says it kept, and is never switched to match a constant.
            if previous:
                mode_in_force=((snapshot.get('settings') or {}).get('mode') or {}).get('current','')
            else:
                changed=client.request('session/setMode',{'sessionId':identity,'mode':DEV_PERMISSION_MODE})
                # settings is the host's own confirmation; the projection block
                # in this response can be a stale snapshot (observed with
                # sessionId "unknown" and the pre-set mode on the real host).
                mode_in_force=((changed.get('settings') or {}).get('mode') or {}).get('current','')
                if mode_in_force!=DEV_PERMISSION_MODE:raise RuntimeBlocked('ZCODE_CONFIG: permission mode was not applied')
            atomic(directory/'tool-policy.json',{'read_only':False,'permission_mode':mode_in_force})
        input_id='formal-gates-'+directory.name
        turn_binding={'sessionId':identity,'inputId':input_id,'selection':configured}
        if not request['read_only']:turn_binding['permission_mode']=mode_in_force
        atomic(directory/'native-source.json',turn_binding)
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
        source=materialize_source(directory)
        atomic(directory/'source.json',source)
        return source
    finally:
        if attached:close_session(client,client.identity)
        client.close()
