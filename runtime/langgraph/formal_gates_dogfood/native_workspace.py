"""ZCode MCP workspace command, constrained by the existing native OS sandbox.

This is not a scheduler or a state tool. ZCode's unrestricted native write/exec
surfaces are disabled; each call runs inside the already-installed Codex command
sandbox (zero Codex model calls). Network is off. Only the role's workspace may
be writable, plus a per-invocation temporary/cache directory. The rest of the
control/evidence directory stays read-only to commands.
"""
from __future__ import annotations
import json
import os
from pathlib import Path
import signal
import shutil
import subprocess
import sys
import time
import uuid
from .infra import atomic, read

SCHEMA={'type':'object','additionalProperties':False,'properties':{
    'command':{'type':'string','minLength':1,'maxLength':100000},
    'timeout_seconds':{'type':'integer','minimum':1,'maximum':180}},'required':['command']}


def sandbox_argv(request, command, scratch=None):
    if request['access'] not in ('read-only','workspace-write'):
        raise ValueError('Invalid role access')
    # Native CLI accepts TOML inline tables, not a JSON string as a config value.
    policy='{extends=":read-only",network={enabled=false}'
    grants=[]
    if request['access']=='workspace-write': grants.append('":workspace_roots"="write"')
    if scratch is not None: grants.append(json.dumps(str(Path(scratch).resolve()))+'="write"')
    if grants: policy+=',filesystem={'+','.join(grants)+'}'
    policy+='}'
    return [request['binding']['launch']['sandbox'],'sandbox','--permission-profile','formal-gates-native',
            '-C',request['workspace'],'-c','permissions.formal-gates-native='+policy,
            '--','/bin/bash','--noprofile','--norc','-c',command]


def command_environment(directory):
    """Only command children get disposable HOME/TMP, never native model auth.

    On macOS /usr/bin/git is an xcrun shim, which may invoke xcodebuild and write
    a shared /tmp cache. Resolve the real native Git before entering the sandbox
    instead of granting a shared temporary directory write access.
    """
    scratch = (directory / 'command-cache').resolve()
    for name in ('tmp', 'home', 'cache', 'go-build'):
        (scratch / name).mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    for name in list(env):
        if any(x in name.upper() for x in ('API_KEY', 'AUTH_TOKEN', 'ACCESS_TOKEN', 'PASSWORD', 'SECRET')):
            env.pop(name, None)
    env.update(HOME=str(scratch/'home'), TMPDIR=str(scratch/'tmp'), TMP=str(scratch/'tmp'),
               TEMP=str(scratch/'tmp'), XDG_CACHE_HOME=str(scratch/'cache'),
               GOCACHE=str(scratch/'go-build'), GOTMPDIR=str(scratch/'tmp'),
               GIT_OPTIONAL_LOCKS='0', PYTHONDONTWRITEBYTECODE='1')
    git = shutil.which('git')
    if sys.platform == 'darwin' and git and Path(git).resolve() == Path('/usr/bin/git'):
        probe = subprocess.run(['/usr/bin/xcrun', '--find', 'git'], capture_output=True,
                               text=True, timeout=15, check=True)
        resolved = Path(probe.stdout.strip())
        if not resolved.is_file() or not resolved.is_absolute() or resolved == Path('/usr/bin/git'):
            raise ValueError('Native Git executable could not be resolved without the system shim')
        env['PATH'] = str(resolved.parent) + os.pathsep + env.get('PATH', '')
    return env, scratch


def main():
    directory=Path(sys.argv[1]).resolve();request=read(directory/'native-request.json')
    calls=directory/'workspace-calls';calls.mkdir(exist_ok=True)
    environment,scratch=command_environment(directory)
    active=None
    def stop(*args):
        nonlocal active
        if active is not None and active.poll() is None:
            try: os.killpg(active.pid,signal.SIGTERM)
            except ProcessLookupError: pass
        raise SystemExit(130)
    signal.signal(signal.SIGTERM,stop)
    try:
        for line in sys.stdin:
            message=json.loads(line);ident=message.get('id');method=message.get('method')
            if ident is None: continue
            try:
                if method=='initialize':
                    result={'protocolVersion':message.get('params',{}).get('protocolVersion','2024-11-05'),
                            'capabilities':{'tools':{}},'serverInfo':{'name':'formal-gates-workspace','version':'1'}}
                elif method=='tools/list':
                    result={'tools':[{'name':'workspace_command','description':
                        'Run a shell command in this role workspace using the native filesystem sandbox. '
                        'Use for reading/search/diff/tests; development roles may also edit files here. '
                        'Review roles cannot write. No network or access to controller write operations. '
                        'Use python -B for read-only checks. Return code is real; never assume success.',
                        'inputSchema':SCHEMA}]}
                elif method=='tools/call':
                    params=message['params'];a=params.get('arguments',{})
                    if params.get('name')!='workspace_command' or set(a)-{'command','timeout_seconds'}:
                        raise ValueError('Tool or fields not granted')
                    command=a.get('command');timeout=a.get('timeout_seconds',120)
                    if not isinstance(command,str) or not 0<len(command)<=100000: raise ValueError('Invalid command')
                    if isinstance(timeout,bool) or not isinstance(timeout,int) or not 1<=timeout<=180: raise ValueError('Invalid timeout')
                    key=uuid.uuid4().hex;out=calls/(key+'.stdout');err=calls/(key+'.stderr')
                    atomic(calls/(key+'.request.json'),{'command':command,'access':request['access'],'workspace':request['workspace'],'timeout_seconds':timeout})
                    env=environment.copy()
                    timed_out=False
                    with out.open('wb') as o,err.open('wb') as e:
                        active=subprocess.Popen(sandbox_argv(request,command,scratch),cwd=request['workspace'],env=env,
                                                stdout=o,stderr=e,stdin=subprocess.DEVNULL,start_new_session=True)
                        try: active.wait(timeout=timeout)
                        except subprocess.TimeoutExpired:
                            timed_out=True
                            try: os.killpg(active.pid,signal.SIGKILL)
                            except ProcessLookupError: pass
                            active.wait()
                    receipt={'exit_code':active.returncode,'timed_out':timed_out,'stdout':out.read_text(errors='replace')[-24000:],
                             'stderr':err.read_text(errors='replace')[-8000:],'stdout_path':str(out),'stderr_path':str(err)}
                    atomic(calls/(key+'.result.json'),receipt);active=None
                    result={'content':[{'type':'text','text':json.dumps(receipt,ensure_ascii=False)}],
                            'isError':receipt['exit_code']!=0 or timed_out}
                elif method=='ping': result={}
                else: raise ValueError('Method not supported')
                print(json.dumps({'jsonrpc':'2.0','id':ident,'result':result},ensure_ascii=False),flush=True)
            except Exception as exc:
                print(json.dumps({'jsonrpc':'2.0','id':ident,'error':{'code':-32000,'message':str(exc)}}),flush=True)
    finally:
        if active is not None and active.poll() is None:
            try: os.killpg(active.pid,signal.SIGKILL)
            except ProcessLookupError: pass
            active.wait()

if __name__=='__main__': main()
