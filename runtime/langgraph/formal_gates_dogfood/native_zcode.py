"""ZCode app-server transport, reusing the original native event contract.

No graph imports, workflow callbacks, global configuration writes or model
substitution. Tools are mounted on create/resume itself (never close an empty
session merely to configure it). Terminals require exact session/input/turn IDs.
"""
from __future__ import annotations
import json
import os
from pathlib import Path
import queue
import shutil
import subprocess
import sys
import threading
import time
from .infra import DogfoodError, RuntimeBlocked, SessionUnavailable, atomic, read, file_proof, canonical
from .native_result import decode_result

TOOL='mcp__formal-gates-workspace__workspace_command'
ALLOWED=['Read','Glob','Grep',TOOL]
DENIED=['Bash','Write','Edit','NotebookEdit','Agent','Task','Workflow','ScheduleWakeup',
        'EnterWorktree','ExitWorktree','EnterPlanMode','ExitPlanMode','WebSearch','WebFetch',
        'AskUserQuestion','CronCreate','CronUpdate','CronDelete']


def capture_binding():
    node=shutil.which('node');sandbox=shutil.which('codex')
    script=Path(os.environ.get('FORMAL_GATES_ZCODE_SCRIPT','/Applications/ZCode.app/Contents/Resources/glm/zcode.cjs')).expanduser().resolve()
    if not node or not script.is_file(): raise RuntimeBlocked('ZCODE_UNAVAILABLE: node or app-server script missing')
    if not sandbox: raise RuntimeBlocked('COMMAND_SANDBOX_UNAVAILABLE: ZCode workspace tool needs the native command sandbox')
    launch={'executable':str(Path(node).resolve()),'script':str(script),'sandbox':str(Path(sandbox).resolve())}
    return {'provider':'zcode','launch':launch,'files':{p:file_proof(Path(p)) for p in launch.values()}}


class Client:
    def __init__(self, launch, workspace, directory, environment=None):
        self.identity='';self.counter=0;self.events=[];self.messages=queue.Queue();self.directory=directory
        self.errors=(directory/'host.stderr').open('ab');self.trace=(directory/'protocol.jsonl').open('a')
        self.proc=subprocess.Popen([launch['executable'],launch['script'],'--cwd',workspace,'app-server'],
            cwd=workspace,env={**os.environ, **(environment or {})},stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=self.errors,text=True,bufsize=1)
        atomic(directory/'native-process.json',{'pid':self.proc.pid,'pgid':os.getpgid(self.proc.pid) if os.name!='nt' else None})
        self.reader=threading.Thread(target=self.collect,daemon=True);self.reader.start()
    def collect(self):
        for line in self.proc.stdout:
            self.trace.write(line);self.trace.flush()
            try:self.messages.put(json.loads(line))
            except ValueError:self.messages.put({'closed':'Invalid native protocol output'})
        self.messages.put({'closed':'Native output closed'})
    def send(self,value):
        self.proc.stdin.write(json.dumps(value,ensure_ascii=False)+'\n');self.proc.stdin.flush()
    def receive(self,timeout=60):
        try:msg=self.messages.get(timeout=timeout)
        except queue.Empty as exc: raise RuntimeBlocked('ZCODE_TIMEOUT: no native message; session is not assumed lost') from exc
        if 'closed' in msg: raise RuntimeBlocked('ZCODE_TRANSPORT: '+msg['closed'])
        if msg.get('method')=='session/requestRuntimePreferences':
            params=msg['params']
            if self.identity and params.get('sessionId')!=self.identity: raise RuntimeBlocked('ZCODE_BINDING: preferences for another session')
            self.send({'id':msg['id'],'result':{'nativeSearchEnhancementsEnabled':False,'memoryEnabled':False,
                'askUserQuestionAutoResolutionEnabled':False,'modelContextBudgetStrategy':'preflight-v1'}})
        elif msg.get('method') and msg.get('id'):
            self.send({'id':msg['id'],'error':{'code':-32601,'message':'No additional capability granted'}})
            raise RuntimeBlocked('ZCODE_INTERACTION: '+msg['method'])
        elif msg.get('method')=='session/event':
            event=msg['params'];self.events.append(event)
            with (self.directory/'native-events.jsonl').open('a') as f:f.write(json.dumps(event,ensure_ascii=False)+'\n');f.flush()
        return msg
    def request(self,method,params,timeout=60):
        self.counter+=1;ident='native-'+str(self.counter);self.send({'id':ident,'method':method,'params':params})
        deadline=time.monotonic()+timeout
        while time.monotonic()<deadline:
            msg=self.receive(max(.1,deadline-time.monotonic()))
            if msg.get('id')==ident and not msg.get('method'):
                if msg.get('error'):
                    e=msg['error'];text=e.get('message','')
                    if method=='session/resume' and e.get('code')==-32004 and text.startswith('Session not found:'):
                        raise SessionUnavailable('ZCODE_SESSION_UNAVAILABLE: '+text,identity=params.get('sessionId',''),code=e['code'])
                    raise RuntimeBlocked(f'ZCODE_NATIVE {method}: {json.dumps(e,ensure_ascii=False)}')
                return msg.get('result',{})
        raise RuntimeBlocked('ZCODE_TIMEOUT: '+method)
    def close(self):
        try:self.proc.stdin.close()
        except (OSError,BrokenPipeError):pass
        try:self.proc.wait(timeout=8)
        except subprocess.TimeoutExpired:
            self.proc.terminate()
            try:self.proc.wait(timeout=5)
            except subprocess.TimeoutExpired:self.proc.kill();self.proc.wait()
        self.reader.join(timeout=2);self.trace.close();self.errors.close()


def selection(snapshot):
    settings=snapshot.get('settings',{});model=settings.get('model',{}).get('current')
    if not isinstance(model,dict) or set(model)!={'providerId','modelId'}:
        raise RuntimeBlocked('ZCODE_CONFIG: native selected model missing')
    return {'model':canonical(model).decode(),'reasoning_effort':settings.get('thoughtLevel',{}).get('current','')}


def exact_model(value,snapshot):
    if not value:return snapshot['settings']['model']['current']
    try:ref=json.loads(value)
    except ValueError:
        current=snapshot['settings']['model']['current']
        ref={'providerId':current['providerId'],'modelId':value}
    available=[a['ref'] for a in snapshot['settings']['model'].get('available',[])]
    if ref not in available:raise RuntimeBlocked('ZCODE_CONFIG: requested exact provider/model is not advertised; no fallback')
    return ref


def resume_model_environment(value):
    """Use only the exact selected provider's EXISTING local runtime options.

    ZCode's catalog omits built-in endpoint/auth details on cold restore. The
    adapter reads the host's documented CLI config, does not write it, and passes
    an env reference to the native runtime API. No secret enters a prompt,
    request.json, evidence record or tool subprocess environment.
    """
    try: model=json.loads(value)
    except ValueError as exc: raise RuntimeBlocked('ZCODE_CONFIG: resume needs a full provider/model reference') from exc
    path=Path(os.environ.get('FORMAL_GATES_ZCODE_CONFIG',str(Path.home()/'.zcode/cli/config.json')))
    if not path.is_file():return {},{}
    config=read(path);provider=config.get('provider',{}).get(model.get('providerId'),{})
    if not provider:return {},{}
    if provider.get('enabled') is False:raise RuntimeBlocked('ZCODE_CONFIG: selected native provider is disabled')
    options=provider.get('options',{});data={};env={}
    if provider.get('kind') in ('anthropic','openai','openai-compatible'): data['kind']=provider['kind']
    if options.get('apiFormat'): data['apiFormat']=options['apiFormat']
    if isinstance(options.get('baseURL'),str):data['baseURL']=options['baseURL']
    key=options.get('apiKey')
    if isinstance(key,str) and key:
        env['FORMAL_GATES_ZCODE_API_KEY']=key
        data['apiKeyRef']={'source':'env','name':'FORMAL_GATES_ZCODE_API_KEY'}
    elif isinstance(key,dict) and key.get('source') in ('credential','env','server-config'):
        data['apiKeyRef']=key
    return env,data


def runtime_model(catalog, model, effort, local=None):
    """Rehydrate the exact native catalog entry, using credential REFERENCES only.

    ZCode cold-resume otherwise creates a deferred model adapter even though its
    UI lists the model. Only the exact native configuration is reused; no global selection
    is changed. Never invent a provider endpoint or substitute a model.
    """
    providers=catalog.get('modelCatalog',{}).get('providers',[])
    matches=[p for p in providers if p.get('providerId')==model['providerId']]
    if len(matches)!=1: raise RuntimeBlocked('ZCODE_CONFIG: exact native provider catalog entry is missing')
    original=matches[0]
    keys=('providerId','kind','apiFormat','label','source','baseURL','apiKeyRequired','headers',
          'providerOptions','logoUrl','modelsDevProviderId')
    provider={k:original[k] for k in keys if k in original}
    models=[m for m in original.get('models',[]) if m.get('modelId')==model['modelId']]
    if len(models)!=1 or original.get('disabledReason') or models[0].get('disabledReason'):
        raise RuntimeBlocked('ZCODE_CONFIG: bound native model unavailable')
    provider['models']=models
    local=local or {}
    if local.get('kind'):
        provider['kind']=local['kind']
        provider.pop('apiFormat',None)
        if local.get('apiFormat'): provider['apiFormat']=local['apiFormat']
    if not provider.get('baseURL') and local.get('baseURL'): provider['baseURL']=local['baseURL']
    credential=original.get('apiKeyRef') or local.get('apiKeyRef')
    if credential:
        if credential.get('source') not in ('credential','env','server-config'):
            raise RuntimeBlocked('ZCODE_CONFIG: native credential reference cannot be reused safely')
        provider['apiKey']=credential
    from .infra import digest
    record={'revision':'formal-gates-'+digest({'provider':provider,'model':model,'effort':effort}),
            'generatedAt':int(time.time()*1000),'model':model,'provider':provider}
    if effort:record['thoughtLevel']=effort
    return record


def materialize_source(directory):
    binding=read(directory/'native-source.json');identity=binding['sessionId'];input_id=binding['inputId']
    turn='';end=None;seq=-1
    with (directory/'native-events.jsonl').open() as f:
        for line in f:
            event=json.loads(line)
            if event.get('sessionId')!=identity: raise RuntimeBlocked('ZCODE_BINDING: mixed sessions')
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
    text=end['payload'].get('response','');raw=directory/'answer.raw.txt'
    if raw.exists() and raw.read_text()!=text:raise RuntimeBlocked('ZCODE_RESULT: saved output differs')
    if not raw.exists():raw.write_text(text)
    result=decode_result(text);atomic(directory/'answer.json',result)
    source={'provider':'zcode','identity':identity,'native_turn':turn,'input_id':input_id,
            'selection':binding['selection'],'usage_evidence':end['payload'].get('usage'),
            'files':{name:file_proof(directory/name) for name in ('native-source.json','native-events.jsonl','answer.raw.txt','answer.json')}}
    atomic(directory/'source.json',source);return source


def reconcile_completed_output(directory,process):
    # Only result-framing errors can be replayed; runtime/transport failures are
    # never laundered into a successful worker result.
    e=read(directory/'native-error.json') if (directory/'native-error.json').exists() else {}
    if process.get('exit_code')!=1 or process.get('error') or not e.get('error','').startswith('RuntimeBlocked: NATIVE_RESULT:'):
        raise RuntimeBlocked('ZCODE_RECONCILE: not a completed formatting-only failure')
    source=materialize_source(directory)
    source['reconciliation']={'kind':'completed-output-format','original_exit_code':1,'original_error':e['error'],'new_model_calls':0}
    atomic(directory/'source.json',source);return source


def execute(request,directory,probe=False):
    directory=Path(directory).resolve();directory.mkdir(parents=True,exist_ok=True)
    workspace=str(Path(request['workspace']).resolve());binding=request.get('binding') or capture_binding()
    launch=binding['launch'];access=request.get('access','read-only')
    if access not in ('read-only','workspace-write'):raise RuntimeBlocked('ZCODE_ACCESS: unsupported role access')
    # All writes/execution pass through an OS-sandboxed, role-scoped command.
    # Native Read/Glob/Grep remain available; unrestricted mutation tools do not.
    env=[{'name':'PYTHONPATH','value':str(Path(__file__).resolve().parent.parent)}]
    server={'name':'formal-gates-workspace','command':sys.executable,
            'args':['-m','formal_gates_dogfood.native_workspace',str(directory)],'env':env,
            'isolation':'workspace','protocolVersion':'legacy','timeoutMs':185000}
    location={'workspacePath':workspace,'workspaceKey':workspace}
    params={'workspace':location,'toolAllowlist':ALLOWED,'toolDenylist':DENIED,'mcpServers':[] if probe else [server]}
    previous=request.get('identity','')
    if previous: params['sessionId']=previous
    else:params.update(persistence='immediate',mode='yolo',titleGenerationEnabled=False)
    environment={};local={}
    if previous:
        if not request.get('model'): raise RuntimeBlocked('ZCODE_CONFIG: resume requires the bound model selection')
        environment,local=resume_model_environment(request['model'])
    client=Client(launch,workspace,directory,environment);attached=False
    try:
        # Load this workspace's native model catalog before restoring a historic
        # session. This is a read, not provider registration or credential copying.
        if not probe:
            # Warm and verify the exact scoped server before a model turn. A slow
            # MCP launch must not leave a developer with only Read/Glob/Grep.
            ready=client.request('mcp/list',{'workspace':location,'mcpServers':[server],'mode':'connect'},timeout=90)
            state=ready.get('statuses',{}).get('formal-gates-workspace',{})
            atomic(directory/'workspace-readiness.json',{'status':state.get('status'),'tool_count':state.get('toolCount')})
            if state.get('status')!='connected' or state.get('toolCount')!=1:
                raise RuntimeBlocked('ZCODE_TOOLS_UNAVAILABLE: role-scoped workspace tool did not connect; no prompt sent')
        catalog=client.request('workspace/readState', {'workspace':location})
        if previous:
            if not request.get('model'): raise RuntimeBlocked('ZCODE_CONFIG: resume requires the bound model selection')
            chosen=exact_model(request['model'],catalog)
            params['runtimeModel']=runtime_model(catalog,chosen,request.get('reasoning_effort',''),local)
            atomic(directory/'runtime-materialization.json',{'model':chosen,'from':'native catalog and existing native configuration',
                'credential_handling':'ephemeral child environment reference; never stored in run files','global_config_changed':False})
        snap=client.request('session/resume' if previous else 'session/create',params)
        identity=snap.get('session',{}).get('sessionId','')
        if not identity or previous and identity!=previous:raise RuntimeBlocked('ZCODE_BINDING: missing or changed identity')
        client.identity=identity;attached=True;configured=selection(snap)
        atomic(directory/'identity.json',{'identity':identity,'provider':'zcode','workspace':workspace,'selection':configured})
        wanted=exact_model(request.get('model',''),snap)
        if wanted!=snap['settings']['model']['current']:
            changed=client.request('session/setModel',{'sessionId':identity,'model':wanted,'persistAsWorkspaceLastUsed':False})
            if changed.get('settings'): configured=selection(changed)
            if json.loads(configured['model'])!=wanted:raise RuntimeBlocked('ZCODE_CONFIG: requested selection was not applied')
        effort=request.get('reasoning_effort','')
        if effort and effort!=configured['reasoning_effort']:
            changed=client.request('session/setThoughtLevel',{'sessionId':identity,'thoughtLevel':effort,'persistAsWorkspaceLastUsed':False})
            if changed.get('settings'):configured=selection(changed)
            if configured['reasoning_effort']!=effort:raise RuntimeBlocked('ZCODE_CONFIG: reasoning setting was not applied')
        atomic(directory/'identity.json',{'identity':identity,'provider':'zcode','workspace':workspace,'selection':configured})
        atomic(directory/'tool-policy.json',{'access':access,'allowed':ALLOWED,'denied':DENIED,
               'execution':'native command sandbox','network':False})
        if probe:return {'provider':'zcode','selection':configured,'binding':binding,'model_calls':0,
            'capabilities':{'new_session':True,'resume':True,'read_only':True,'workspace_write':True,'native_completion':True}}
        if previous: client.request('session/setMode',{'sessionId':identity,'mode':'yolo'})
        subscribed=client.request('session/subscribe',{'sessionId':identity,'deliveryKind':'desktop-continuous','includeSnapshot':False})
        if subscribed.get('sessionId')!=identity:raise RuntimeBlocked('ZCODE_BINDING: subscription identity differs')
        input_id='formal-gates-'+directory.name
        atomic(directory/'native-source.json',{'sessionId':identity,'inputId':input_id,'selection':configured})
        prompt=request['prompt']+'\nUse Read/Glob/Grep or the workspace_command tool for actual work. '
        prompt+='Native Bash/Write/Edit are not granted. workspace_command executes shell in the role-scoped OS sandbox; use python -B for read-only checks. '
        prompt+='Do not request plan mode, tools or settings changes. Return ONLY one JSON object matching this schema:\n'+json.dumps(request['schema'],ensure_ascii=False)
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
        if attached:
            try:client.request('session/close',{'sessionId':client.identity},timeout=10)
            except (DogfoodError,OSError):pass
        client.close()
