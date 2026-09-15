"""Native inheritance and protocol boundaries, without model invocations."""
from pathlib import Path
import io
import json
import os
import queue
import shutil
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

from formal_gates_harness import cli, native_dsh as dsh, native_zcode as zcode, native_hosts as hosts
from formal_gates_harness.infra import Bridge, HarnessError, RuntimeBlocked, read, runtime_identity

ROOT = Path(__file__).resolve().parents[3]
MODEL = {'providerId': 'original', 'modelId': 'M'}


class RegistryAndSelection(unittest.TestCase):
    def test_nested_execution_code_is_part_of_runtime_identity(self):
        with tempfile.TemporaryDirectory() as t:
            root = Path(t)
            for name in ('runtime/langgraph/requirements.lock', 'prompts/reviewer-base.md',
                         'binary', 'runtime/langgraph/formal_gates_harness/adapters/new.py'):
                path = root / name; path.parent.mkdir(parents=True, exist_ok=True); path.write_text('original')
            before = runtime_identity(root, str(root / 'binary'))
            (root / 'runtime/langgraph/formal_gates_harness/adapters/new.py').write_text('changed')
            self.assertNotEqual(runtime_identity(root, str(root / 'binary')), before)

    def test_nested_adapter_declares_transport_without_cli_changes(self):
        module = SimpleNamespace(__file__=str(Path(hosts.__file__).parent / 'adapters/new.py'))
        with patch.object(hosts.importlib, 'import_module', return_value=module):
            self.assertEqual(hosts.NativeAdapter('new', '.adapters.new').transport_files(), ('adapters/new.py',))

    def test_execution_only_host_does_not_need_installation_registration(self):
        adapter = hosts.NativeAdapter('runtime-only', dsh.__name__)
        with patch.dict(hosts._ADAPTERS, {'runtime-only': adapter}):
            bridge = Mock()
            self.assertEqual(hosts.normalize_provider(bridge, 'runtime-only'), 'runtime-only')
            bridge.call.assert_not_called()
            bridge.call.return_value = []
            self.assertIn('runtime-only', {h['provider'] for h in hosts.host_catalog(bridge)['hosts']})
            spec = {'version': 1, 'project': '/project', 'base': 'a' * 40,
                    'requirement_digest': 'b' * 64, 'checks': [{'id': 'test', 'argv': ['true'], 'dependencies': []}],
                    'gates': ['complexity-gate'], 'provider': 'runtime-only', 'model': '',
                    'reasoning_effort': '', 'repair_limit': 1}
            result = Bridge(str(ROOT / '.harness/bin/formal-gates')).call('validate-spec', spec)
            self.assertEqual(result['spec']['provider'], 'runtime-only')

    def test_explicit_provider_wins_environment(self):
        bridge = Mock()
        with patch.dict(os.environ, {'FORMAL_GATES_HARNESS_PROVIDER': 'zcode'}):
            self.assertEqual(cli.select_provider(bridge, 'dsh'), 'dsh')
            self.assertEqual(cli.select_provider(bridge), 'zcode')
        bridge.call.assert_not_called()

    def test_missing_provider_creates_no_native_sample_or_probe(self):
        with tempfile.TemporaryDirectory() as t, patch.dict(os.environ, {}, clear=True), \
             patch.object(cli, 'binary_path', return_value='unused'), patch.object(cli, 'Bridge'):
            for action in (lambda: cli.make_demo(Path(t), native=True), lambda: cli.doctor(Path(t))):
                with self.assertRaisesRegex(HarnessError, 'execution adapters:'):
                    action()
                self.assertEqual(list(Path(t).iterdir()), [])

    def test_fixture_demo_does_not_select_a_native_host(self):
        with tempfile.TemporaryDirectory() as t, patch.object(cli, 'make_run') as create, \
             patch.dict(os.environ, {'FORMAL_GATES_HARNESS_PROVIDER': 'nonexistent'}):
            cli.make_demo(Path(t))
            self.assertEqual(create.call_args.kwargs.get('provider', create.call_args.args[5] if len(create.call_args.args) > 5 else None), 'fixture')


class NativeRequests(unittest.TestCase):
    def zcode_request(self, read_only, previous='', probe=False, close_error=False):
        with tempfile.TemporaryDirectory() as t:
            directory = Path(t)
            client = Mock(identity='', events=[], interactions=[])
            snapshot = {'session': {'sessionId': 'root'}, 'settings': {
                'model': {'current': MODEL, 'available': [{'ref': MODEL}]},
                'thoughtLevel': {'current': ''}}}
            def respond(method, params, **kwargs):
                if method == 'session/close' and close_error:
                    raise RuntimeBlocked('Native close failed')
                if method in ('workspace/readState', 'session/create', 'session/resume'):
                    return snapshot
                if method == 'session/subscribe':
                    return {'sessionId': 'root'}
                if method == 'session/send':
                    client.events.append({'sessionId': 'root', 'type': 'turn.completed',
                                          'payload': {'inputId': params['inputId']}})
                    return {'sessionId': 'root', 'accepted': True}
                return {}
            client.request.side_effect = respond
            request = {'workspace': t, 'read_only': read_only, 'identity': previous,
                       'model': json.dumps(MODEL), 'prompt': 'Current task', 'schema': {},
                       'binding': {'provider': 'zcode', 'launch': {'executable': '/bound/node', 'script': '/bound/host'}}}
            with patch.object(zcode, 'Client', return_value=client) as constructor, \
                 patch.object(zcode, 'resume_model_environment', return_value=({}, {})), \
                 patch.object(zcode, 'runtime_model', return_value={}), \
                 patch.object(zcode, 'materialize_source', return_value={'provider': 'zcode'}):
                result = zcode.execute(request, directory, probe=probe)
            constructor.assert_called_once_with(request['binding']['launch'], str(directory.resolve()), directory.resolve(), {})
            calls = [(c.args[0], c.args[1]) for c in client.request.call_args_list]
            params = next(p for method, p in calls if method in ('session/create', 'session/resume'))
            client.close.assert_called_once()
            return params, calls, result

    def test_zcode_development_new_and_resume_inherit_native_configuration(self):
        forbidden = {'mode', 'toolAllowlist', 'toolDenylist', 'mcpServers', 'network', 'sandbox', 'approval', 'titleGenerationEnabled'}
        for previous in ('', 'root'):
            with self.subTest(previous=previous):
                params, calls, _ = self.zcode_request(False, previous)
                self.assertFalse(forbidden & params.keys())
                self.assertNotIn('session/setMode', [m for m, _ in calls])
                self.assertNotIn('mcp/list', [m for m, _ in calls])

    def test_zcode_only_read_only_roles_narrow_tools_on_new_and_resume(self):
        for previous in ('', 'root'):
            params, _, _ = self.zcode_request(True, previous)
            self.assertEqual(params['toolAllowlist'], zcode.READ_ONLY_TOOLS)
            self.assertTrue({'WebFetch', 'WebSearch'} <= set(params['toolAllowlist']))
            self.assertFalse({'Bash', 'Write', 'Edit', 'Agent', 'Task', 'Workflow'} & set(params['toolAllowlist']))
            self.assertNotIn('mcpServers', params)

    def test_probe_disables_only_its_own_title_generation_and_does_not_prompt(self):
        params, calls, report = self.zcode_request(False, probe=True)
        self.assertIs(params['titleGenerationEnabled'], False)
        self.assertNotIn('session/send', [m for m, _ in calls])
        self.assertEqual(report['model_calls'], 0)
        self.assertEqual(report['native_tools'], 'not_verified')
        self.assertEqual(report['mcp_auth'], 'not_verified')

    def test_probe_does_not_hide_native_close_errors(self):
        with self.assertRaisesRegex(RuntimeBlocked, 'Native close failed'):
            self.zcode_request(False, probe=True, close_error=True)

    def test_zcode_binding_never_looks_for_another_host_cli(self):
        with tempfile.TemporaryDirectory() as t:
            node = Path(t) / 'node'; node.write_text('node')
            script = Path(t) / 'app.cjs'; script.write_text('native')
            with patch.dict(os.environ, {'FORMAL_GATES_ZCODE_SCRIPT': str(script)}), \
                 patch.object(zcode.shutil, 'which', return_value=str(node)) as which:
                binding = zcode.capture_binding()
            which.assert_called_once_with('node')
            self.assertEqual(set(binding['launch']), {'executable', 'script'})
            self.assertEqual(len(binding['files']), 2)

    def test_dsh_development_new_and_resume_have_no_permission_patch(self):
        for previous in ('', 'root'):
            with self.subTest(previous=previous), tempfile.TemporaryDirectory() as t:
                directory = Path(t)
                client = Mock()
                def respond(method, params, **kwargs):
                    if method == 'initialize':
                        return {'protocolVersion': 1, 'agentCapabilities': {'sessionCapabilities': {'resume': {}, 'close': {}}}}
                    if method in ('session/new', 'session/resume'):
                        return {'sessionId': 'root', 'configOptions': []}
                    return {'stopReason': 'end_turn'}
                client.request.side_effect = respond
                request = {'workspace': t, 'read_only': False, 'identity': previous,
                           'prompt': 'Current task', 'schema': {},
                           'binding': {'provider': 'dsh', 'launch': {'executable': '/bound/dsh'}}}
                with patch.object(dsh, 'Client', return_value=client) as constructor, \
                     patch.object(dsh, 'observe', return_value={'provider': 'dsh'}):
                    dsh.execute(request, directory)
                self.assertEqual(constructor.call_args.args[0][0], '/bound/dsh')
                overlays = read(directory / 'profile.json')
                self.assertFalse({'agent-loop', 'sandbox-policy', 'permission', 'approval', 'session-title-llm'} & {p.get('id') for p in overlays})
                self.assertIs(overlays[0]['insert'][0]['config']['read_only'], False)
                params = next(c.args[1] for c in client.request.call_args_list if c.args[0] in ('session/new', 'session/resume'))
                self.assertEqual(params['mcpServers'], [])  # ACP's additive, required transport field.


class ProtocolInteractions(unittest.TestCase):
    def client(self, directory):
        client = object.__new__(zcode.Client)
        client.counter = 0; client.identity = 'root'; client.send = Mock()
        client.messages = queue.Queue(); client.events = []; client.interactions = []
        client.directory = Path(directory)
        return client

    def test_optional_interactions_do_not_lose_the_following_response(self):
        with tempfile.TemporaryDirectory() as t:
            client = self.client(t)
            for method in ('session/requestRuntimePreferences', 'interaction/requestOfficialMcpAuthHeaders', 'optional/future-method'):
                client.messages.put({'id': method, 'method': method, 'params': {}})
            client.messages.put({'id': 'native-1', 'result': {'healthy': True}})
            self.assertEqual(client.request('workspace/readState', {}), {'healthy': True})
            replies = [c.args[0] for c in client.send.call_args_list][1:]
            self.assertEqual(replies[0]['error']['code'], -32601)
            self.assertEqual(replies[1]['result'], {'ok': False, 'reason': 'official_auth_unavailable'})
            self.assertEqual(replies[2]['error']['code'], -32601)
            self.assertIn({'method': 'interaction/requestOfficialMcpAuthHeaders', 'status': 'official_auth_unavailable'}, read(Path(t) / 'interaction-status.json'))

    def test_required_user_decisions_are_neither_approved_nor_denied(self):
        for method in ('interaction/requestPermission', 'interaction/requestUserInput', 'interaction/requestProviderRuntimeHeaders'):
            with self.subTest(method=method), tempfile.TemporaryDirectory() as t:
                client = self.client(t)
                client.messages.put({'id': 0, 'method': method, 'params': {}})
                with self.assertRaisesRegex(RuntimeBlocked, 'INTERACTION_REQUIRED'):
                    client.receive()
                client.send.assert_not_called()

    def test_child_notifications_do_not_enter_root_evidence(self):
        with tempfile.TemporaryDirectory() as t:
            client = self.client(t)
            for identity in ('child', 'root'):
                client.messages.put({'method': 'session/event', 'params': {'sessionId': identity, 'type': 'turn.completed'}})
                client.receive()
            self.assertEqual([e['sessionId'] for e in client.events], ['root'])
            self.assertNotIn('child', (Path(t) / 'native-events.jsonl').read_text())

    def test_transport_metadata_does_not_log_native_auth_payloads(self):
        for module, collect in ((zcode, 'collect'), (dsh, '_read')):
            client = object.__new__(module.Client)
            client.trace = io.StringIO(); client.messages = queue.Queue()
            client.proc = Mock(stdout=io.StringIO(json.dumps({'id': 1, 'result': {'Authorization': 'TEST_SECRET'}}) + '\n'))
            getattr(client, collect)()
            self.assertNotIn('TEST_SECRET', client.trace.getvalue())
            self.assertEqual(client.messages.get()['result']['Authorization'], 'TEST_SECRET')

    def test_dsh_permission_request_does_not_send_a_user_decision(self):
        client = object.__new__(dsh.Client)
        client.counter = 0; client.messages = queue.Queue(); client.send = Mock()
        client.messages.put({'id': 'permission', 'method': 'session/request_permission', 'params': {}})
        with self.assertRaisesRegex(RuntimeBlocked, 'INTERACTION_REQUIRED'):
            client.request('session/prompt', {})
        self.assertEqual(client.send.call_count, 1)  # Only our outgoing request.


@unittest.skipUnless(shutil.which('node'), 'Node is needed to test the native JS hook')
class RootSessionHook(unittest.TestCase):
    def test_root_binding_and_role_scoped_restrictions(self):
        script = r'''
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {pathToFileURL} from 'node:url';
const {apply}=await import(pathToFileURL(process.argv[1]));
const root=process.argv[2];
for (const readOnly of [false,true]) {
  const directory=root+'/'+readOnly;fs.mkdirSync(directory);
  const handlers=new Map(), guards=[], restrictions=[];
  const policy={overrideOf:session=>session.mode};
  const ctx={tools:{guard:f=>{guards.push(f);return ()=>{};},schemas:()=>['Read','Bash','Write','WebFetch','custom_mcp'].map(name=>({name}))},
    get:name=>name==='sandboxPolicy'?policy:undefined,effect:()=>{},provide:()=>{},
    on:(name,fn)=>handlers.set(name,[...(handlers.get(name)||[]),fn])};
  const emit=(name,...args)=>{for(const fn of handlers.get(name)||[])fn(...args);};
  const agent=id=>({session:{header:{id},append(type,data){assert.equal(type,'sandbox/mode');this.mode=data.mode;}},
    ctx:{get:()=>policy,tools:{restrict:value=>restrictions.push(value)}}});
  apply(ctx,{directory,read_only:readOnly});
  const main=agent('root'),child=agent('child');
  emit('agent/created',{agent:main});emit('agent/created',{agent:child});
  assert.equal(guards.length,readOnly?1:0);assert.equal(restrictions.length,readOnly?2:0);
  if(readOnly){assert.equal(main.session.mode,'read-only');assert.equal(child.session.mode,'read-only');
    for(const name of ['web_fetch','web_search','subagent','subagent_fork'])assert.equal(guards[0]({name}),undefined);
    assert.equal(guards[0]({name:'WebFetch'}),undefined);assert.ok(guards[0]({name:'Write'}));assert.ok(guards[0]({name:'custom_mcp'}));}
  else assert.equal(main.session.mode,undefined);
  fs.writeFileSync(directory+'/identity.json',JSON.stringify({identity:'root'}));
  emit('agent/inbox/claimed',{agent:child,message:{id:'child-input',source:{kind:'user'}},turn:1});
  assert.ok(!fs.existsSync(directory+'/native-source.json'));
  emit('agent/inbox/claimed',{agent:main,message:{id:'root-input',source:{kind:'user'}},turn:2});
  const original=fs.readFileSync(directory+'/native-source.json','utf8');
  emit('agent/inbox/claimed',{agent:main,message:{id:'child-notice',source:{kind:'agent-message'}},turn:3});
  emit('agent/inbox/claimed',{agent:child,message:{id:'child-input',source:{kind:'user'}},turn:4});
  emit('session/event',child.session,{type:'turn/end',data:{turn:4,reason:'completed'}});
  assert.equal(fs.readFileSync(directory+'/native-source.json','utf8'),original);
  assert.ok(!fs.existsSync(directory+'/native-events.jsonl'));
  emit('session/event',main.session,{type:'turn/end',data:{turn:2,reason:'completed'}});
  assert.ok(fs.existsSync(directory+'/native-events.jsonl'));
}
'''
        with tempfile.TemporaryDirectory() as t:
            completed = subprocess.run(['node', '--input-type=module', '-e', script,
                                        str(ROOT / 'runtime/langgraph/formal_gates_harness/native_dsh.mjs'), t],
                                       text=True, capture_output=True)
            self.assertEqual(completed.returncode, 0, completed.stderr)


if __name__ == '__main__':
    unittest.main()
