"""Native inheritance and protocol boundaries, without model invocations."""
from pathlib import Path
import io
import json
import os
import queue
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch

from formal_gates_harness import (cli, native_hosts as hosts)
# DSH and ZCode structural assertions pin the independent transport oracles
# used by differential checks against the Go native adapters.
import reference_native_dsh as dsh
import reference_native_transport as shared
import reference_native_zcode as zcode
from formal_gates_harness.infra import (Bridge, HarnessError, RuntimeBlocked, canonical, read,
                                        runtime_identity)

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

    def test_execution_only_host_does_not_need_installation_registration(self):
        # Runtime-only host registration uses the same GoAdapter interface.
        adapter = hosts.GoAdapter('runtime-only')
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
    def zcode_request(self, read_only, previous='', probe=False, close_error=False,
                      native_mode='build', failure='', observed=None):
        with tempfile.TemporaryDirectory() as t:
            directory = Path(t)
            client = Mock(identity='', events=[], interactions=[])
            snapshot = {'session': {'sessionId': 'root'},
                        'protocol': {'name': 'ZCode Protocol', 'version': 1},
                        'settings': {
                            'model': {'current': MODEL, 'available': [{'ref': MODEL}]},
                            'thoughtLevel': {'current': ''}, 'mode': {'current': native_mode}}}
            def respond(method, params, **kwargs):
                if observed is not None:
                    observed.append((method, params))
                if method == failure:
                    raise RuntimeBlocked('ZCODE_TIMEOUT: simulated native response timeout')
                if method == 'session/close' and close_error:
                    raise RuntimeBlocked('Native close failed')
                if method in ('session/create', 'session/resume'):
                    return snapshot
                if method == 'session/setMode':
                    # The real host confirms through settings; its projection
                    # block can be a stale snapshot.
                    return {'settings': {'mode': {'current': params['mode']}}}
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
                 patch.object(zcode, 'materialize_source', return_value={'provider': 'zcode'}):
                result = zcode.execute(request, directory, probe=probe)
            # Persisted permission evidence must state the mode actually in
            # force: the declared one for a new session (confirmed by the host
            # readback), the snapshot-reported one for a resumed session —
            # never a constant that was not applied.
            if not read_only and not probe:
                expected_mode = native_mode if previous else zcode.DEV_PERMISSION_MODE
                self.assertEqual(read(directory / 'tool-policy.json'),
                                 {'read_only': False, 'permission_mode': expected_mode})
                self.assertEqual(read(directory / 'native-source.json')['permission_mode'], expected_mode)
            constructor.assert_called_once_with(request['binding']['launch'], str(directory.resolve()), directory.resolve(), {})
            calls = [(c.args[0], c.args[1]) for c in client.request.call_args_list]
            params = next(p for method, p in calls if method in ('session/create', 'session/resume'))
            client.close.assert_called_once()
            return params, calls, result

    def test_zcode_development_new_session_declares_operator_mode_resume_keeps_its_own(self):
        forbidden = {'mode', 'toolAllowlist', 'toolDenylist', 'mcpServers', 'network', 'sandbox', 'approval', 'titleGenerationEnabled'}
        for previous in ('', 'root'):
            with self.subTest(previous=previous):
                params, calls, _ = self.zcode_request(False, previous)
                self.assertFalse(forbidden & params.keys())
                methods = [method for method, _ in calls]
                self.assertNotIn('mcp/list', methods)
                # 3.12.3 removed it; a resume or a new session must not call it.
                self.assertNotIn('workspace/readState', methods)
                # A NEW dev session declares the operator's unattended mode once,
                # before its first turn; a resumed session keeps its own mode.
                mode_calls = [p for m, p in calls if m == 'session/setMode']
                if previous:
                    self.assertEqual(mode_calls, [])
                else:
                    self.assertEqual(mode_calls, [{'sessionId': 'root', 'mode': zcode.DEV_PERMISSION_MODE}])
                    self.assertLess(methods.index('session/setMode'), methods.index('session/send'))

    def test_zcode_read_only_and_probe_sessions_never_set_a_mode(self):
        for read_only, probe in ((True, False), (False, True)):
            with self.subTest(read_only=read_only, probe=probe):
                _, calls, _ = self.zcode_request(read_only, probe=probe)
                self.assertNotIn('session/setMode', [method for method, _ in calls])

    def test_zcode_declares_operator_mode_regardless_of_reported_native_mode(self):
        # The app-server hard-defaults new headless sessions to 'build'; whatever
        # mode the host reports, a NEW dev session still declares the operator's
        # unattended mode so the default cannot gate unattended runs. A resumed
        # session is never re-declared.
        for mode in ('build', 'edit', 'plan', 'yolo', 'auto'):
            for previous in ('', 'root'):
                with self.subTest(mode=mode, previous=previous):
                    _, calls, _ = self.zcode_request(False, previous, native_mode=mode)
                    declared = [p.get('mode') for m, p in calls if m == 'session/setMode']
                    self.assertEqual(declared, [] if previous else [zcode.DEV_PERMISSION_MODE])

    def test_removed_auto_mode_environment_cannot_override_native_settings(self):
        # The removed variable has no effect: a NEW dev session declares the
        # fixed operator mode, never the value the variable used to carry, and
        # a resumed session still declares nothing.
        for mode in ('auto', 'yolo', 'autopilot'):
            with self.subTest(mode=mode), patch.dict(os.environ, {'FORMAL_GATES_ZCODE_AUTO_MODE': mode}):
                for previous in ('', 'root'):
                    _, calls, _ = self.zcode_request(False, previous)
                    declared = [p.get('mode') for m, p in calls if m == 'session/setMode']
                    self.assertEqual(declared, [] if previous else [zcode.DEV_PERMISSION_MODE])

    def test_zcode_native_timeout_never_retries_with_another_permission_mode(self):
        for method in ('session/create', 'session/resume', 'session/subscribe'):
            with self.subTest(method=method):
                calls = []
                previous = 'root' if method == 'session/resume' else ''
                with self.assertRaisesRegex(RuntimeBlocked, 'ZCODE_TIMEOUT'):
                    self.zcode_request(False, previous, failure=method, observed=calls)
                self.assertEqual(sum(m == method for m, _ in calls), 1)
                self.assertNotIn('session/setMode', [m for m, _ in calls])
                self.assertNotIn('session/send', [m for m, _ in calls])

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

    def test_zcode_binding_names_the_bundled_catalogue_the_host_cannot_reach(self):
        # ZCode 3.12.3 ships the catalogue at <Resources>/config/provider/, which
        # neither path the app-server derives from its own script location
        # reaches. The launcher names it instead.
        with tempfile.TemporaryDirectory() as t:
            resources = Path(t) / 'Resources'
            script = resources / 'glm' / 'zcode.cjs'; script.parent.mkdir(parents=True); script.write_text('native')
            catalogue = resources / 'config' / 'provider' / 'zcode-builtin.json'
            catalogue.parent.mkdir(parents=True); catalogue.write_text('{"revision": 28}')
            node = Path(t) / 'node'; node.write_text('node')
            with patch.dict(os.environ, {'FORMAL_GATES_ZCODE_SCRIPT': str(script)}), \
                 patch.object(zcode.shutil, 'which', return_value=str(node)):
                binding = zcode.capture_binding()
            # The launch names the resolved file: on macOS TMPDIR itself is a
            # symlink (/var -> /private/var), so compare canonical paths.
            self.assertEqual(binding['environment'],
                             {'ZCODE_BUILTIN_PROVIDER_CONFIG_FILE': str(catalogue.resolve()),
                              'ZCODE_PERSONAL_PROVIDER_CONFIG_FILE':
                                  str(zcode.host_data_directory() / '.zcode' / 'v2' / 'provider_config.json')})
            # Naming a host-owned catalogue is not proofing an executed binary.
            self.assertEqual(len(binding['files']), 2)

    def test_zcode_binding_leaves_a_host_that_finds_its_own_catalogue_alone(self):
        # The pre-3.12.3 layout: the app-server resolves this itself, so the
        # launcher must not override anything.
        with tempfile.TemporaryDirectory() as t:
            resources = Path(t) / 'Resources'
            script = resources / 'glm' / 'zcode.cjs'; script.parent.mkdir(parents=True); script.write_text('native')
            (script.parent / 'provider').mkdir()
            (script.parent / 'provider' / 'zcode-builtin.json').write_text('{}')
            (resources / 'config' / 'provider').mkdir(parents=True)
            (resources / 'config' / 'provider' / 'zcode-builtin.json').write_text('{}')
            node = Path(t) / 'node'; node.write_text('node')
            with patch.dict(os.environ, {'FORMAL_GATES_ZCODE_SCRIPT': str(script)}), \
                 patch.object(zcode.shutil, 'which', return_value=str(node)):
                binding = zcode.capture_binding()
            self.assertNotIn('environment', binding)

    def test_zcode_binding_claims_no_catalogue_when_the_bundle_ships_none(self):
        # Negative control: with no catalogue anywhere the binding stays silent
        # and the app-server reports its own failure rather than ours.
        with tempfile.TemporaryDirectory() as t:
            script = Path(t) / 'app.cjs'; script.write_text('native')
            node = Path(t) / 'node'; node.write_text('node')
            with patch.dict(os.environ, {'FORMAL_GATES_ZCODE_SCRIPT': str(script)}), \
                 patch.object(zcode.shutil, 'which', return_value=str(node)):
                binding = zcode.capture_binding()
            self.assertNotIn('environment', binding)
            self.assertIsNone(zcode.bundled_provider_catalogue(script))

    def test_zcode_launch_gives_the_child_the_named_catalogue(self):
        # The binding's launch environment reaches the child unchanged on a new
        # session and on a resume: the host owns its account configuration, so
        # the transport adds nothing of its own to it.
        seed = {'ZCODE_BUILTIN_PROVIDER_CONFIG_FILE': '/host/seed.json'}
        with tempfile.TemporaryDirectory() as t:
            directory = Path(t)
            binding = {'provider': 'zcode', 'launch': {'executable': 'node', 'script': 'app.cjs'},
                       'files': {}, 'environment': dict(seed)}
            for previous in ('', 'root'):
                with self.subTest(previous=previous), patch.object(zcode, 'Client') as client, \
                     patch.dict(os.environ, {'ZCODE_DATA_BASE_DIR': str(directory)}):
                    client.return_value.request.side_effect = RuntimeBlocked('probe stopped at the first request')
                    request = {'workspace': t, 'read_only': True, 'binding': binding,
                               'identity': previous, 'model': json.dumps(MODEL) if previous else ''}
                    with self.assertRaisesRegex(RuntimeBlocked, 'probe stopped'):
                        zcode.execute(request, directory, probe=True)
                launch, _workspace, _directory, environment = client.call_args.args
                self.assertEqual(launch, binding['launch'])
                self.assertEqual(environment, seed)

    # test_dsh_development_new_and_resume_have_no_permission_patch was removed
    # with the dsh execution path it mocked: the overlay shape (no permission/
    # approval/agent-loop overlays, read_only flag, mcpServers: []) is now pinned
    # by test_native_dsh_differential.py's new/resume scenarios (profile.json
    # byte for byte, the echoed session/new|resume params by value against the
    # reference) and by the Go unit test of makeDSHPatch in
    # internal/harness/native_dsh_test.go.


class SharedTransportBoundary(unittest.TestCase):
    """One boundary serves every host; an adapter declares protocol, not mechanics."""

    def test_every_adapter_declares_the_shared_module_it_depends_on(self):
        # Host-specific Python inventories contain each host's side file;
        # shared native mechanics are bound through the Go driver.
        self.assertEqual(hosts.adapter_for('zcode').transport_files(), ('zcode_credential.mjs',))
        self.assertEqual(hosts.adapter_for('dsh').transport_files(), ('native_dsh.mjs',))
        # The reference copies still declare the shared module they depend on.
        self.assertIn('reference_native_transport.py', zcode.TRANSPORT_FILES)
        self.assertIn('reference_native_zcode.py', zcode.TRANSPORT_FILES)
        self.assertIn('reference_native_transport.py', dsh.TRANSPORT_FILES)

    def test_each_adapter_supplies_protocol_only(self):
        # If an adapter defines one of these again, the logic stopped being shared.
        for module in (zcode, dsh):
            with self.subTest(module=module.__name__):
                for name in ('collect', 'send', 'receive', 'answer', 'request', 'wait', 'close', 'frame'):
                    self.assertNotIn(name, vars(module.Client))

    def test_an_adapter_declares_its_envelope_not_its_loop(self):
        dsh_client = object.__new__(dsh.Client); dsh_client.counter = 0
        self.assertEqual(dsh_client.frame(dsh_client.ident(), 'session/new', {}),
                         {'jsonrpc': '2.0', 'id': 1, 'method': 'session/new', 'params': {}})
        zcode_client = object.__new__(zcode.Client); zcode_client.counter = 0
        self.assertEqual(zcode_client.frame(zcode_client.ident(), 'session/create', {}),
                         {'id': 'native-1', 'method': 'session/create', 'params': {}})

    def test_a_long_native_turn_is_not_cut_off_by_our_own_budget(self):
        # Go transport checks cover the DSH per-message wait; this case checks
        # the independent ZCode transport oracle's wait policy.
        client = object.__new__(zcode.Client)
        self.assertEqual(client.wait(40, None), 40)           # per-message, as ACP bounds startup
        self.assertIsNone(client.wait(shared.UNBOUNDED, None))  # the host owns the turn length


class ProtocolInteractions(unittest.TestCase):
    def client(self, directory):
        client = object.__new__(zcode.Client)
        client.counter = 0; client.identity = 'root'; client.send = Mock()
        client.messages = queue.Queue(); client.events = []; client.interactions = []
        client.trace = io.StringIO()
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
        for method in ('interaction/requestPermission', 'interaction/requestUserInput'):
            with self.subTest(method=method), tempfile.TemporaryDirectory() as t:
                client = self.client(t)
                client.messages.put({'id': 0, 'method': method, 'params': {}})
                with self.assertRaisesRegex(RuntimeBlocked, 'INTERACTION_REQUIRED'):
                    client.receive()
                client.send.assert_not_called()

    def test_a_provider_without_a_provisioned_credential_is_reported_not_invented(self):
        # The reference has no credential helper next to its module, so it must
        # report no provisioned credential. Go credential delivery is tested separately.
        with tempfile.TemporaryDirectory() as t:
            client = self.client(t)
            client.messages.put({'id': 'headers', 'method': zcode.RUNTIME_HEADERS_METHOD,
                                 'params': {'providerId': 'account:absent'}})
            client.receive()
            reply = client.send.call_args[0][0]
            self.assertIs(reply['result']['headersApplied'], False)
            self.assertNotIn('apiKey', reply['result'])
            self.assertEqual(reply['result']['errorMessage'],
                             'no provisioned request credential for account:absent')
            self.assertIn({'method': zcode.RUNTIME_HEADERS_METHOD,
                           'status': 'provider-runtime-headers-unavailable'},
                          json.loads((Path(t) / 'interaction-status.json').read_text()))

    def test_child_notifications_do_not_enter_root_evidence(self):
        with tempfile.TemporaryDirectory() as t:
            client = self.client(t)
            for identity in ('child', 'root'):
                client.messages.put({'method': 'session/event', 'params': {'sessionId': identity, 'type': 'turn.completed'}})
                client.receive()
            self.assertEqual([e['sessionId'] for e in client.events], ['root'])
            self.assertNotIn('child', (Path(t) / 'native-events.jsonl').read_text())

    def test_transport_metadata_does_not_log_native_auth_payloads(self):
        # Routing metadata omits native authentication payloads. Both Go transports
        # have independent checks of this same boundary.
        for module in (zcode,):
            with self.subTest(module=module.__name__):
                client = object.__new__(module.Client)
                client.trace = io.StringIO(); client.messages = queue.Queue()
                client.proc = Mock(stdout=io.StringIO(json.dumps({'id': 1, 'result': {'Authorization': 'TEST_SECRET'}}) + '\n'))
                client.collect()
                self.assertNotIn('TEST_SECRET', client.trace.getvalue())
                self.assertEqual(client.messages.get()['result']['Authorization'], 'TEST_SECRET')

    # test_dsh_permission_request_does_not_send_a_user_decision was removed with
    # the dsh execution path it exercised: the no-reply permission refusal is now
    # pinned end to end by the interaction-boundary scenario of
    # test_native_dsh_differential.py (identical DSH_INTERACTION_REQUIRED text
    # on both sides, and the fake host records zero answered frames) and by the
    # respond-hook tests of internal/harness/native_transport_unix_test.go
    # (TestTransportRequestPermissionIsNeverAnswered) and
    # internal/harness/native_dsh_unix_test.go (TestDSHExecutePermissionNeverAnswered).


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


class AccountProviderConfiguration(unittest.TestCase):
    """The account provider overlay a launcher has to hand the app-server.

    A launcher-started app-server keeps no provider registry of its own, so
    without this overlay it reports an empty provider list and no current model.
    Every case below is derived from the host's own provisioning index, never
    invented, and the revision has to match the agent's exactly or the agent
    discards the delivery without saying so.
    """

    ACCOUNT = 'account:bigmodel-individual-coding-plan'
    TEAM = 'account:bigmodel-team-coding-plan'
    TEMPLATE = 'zai-api'

    def catalogue(self, root, providers=(ACCOUNT, TEAM), revision=28, modes=None, family='bigmodel'):
        """A built-in catalogue holding the given account providers."""
        modes = modes or {}
        rules = []
        for provider in providers:
            rules.append({'providerId': provider, 'providerName': provider,
                          'config': {'builtinModelIds': ['GLM-5.3'],
                                     'access': {'type': 'zhipu-account',
                                                'mode': modes.get(provider, 'individual-coding-plan'),
                                                'accountType': family}}})
        rules.append({'providerId': 'template:zai-api', 'providerName': 'Z.AI API',
                      'config': {'access': {'type': 'api-key', 'apiKey': 'k'}}})
        path = Path(root) / 'zcode-builtin.json'
        path.write_text(json.dumps({'schemaVersion': 1, 'revision': revision,
                                    'config': {'providerConfigRules': {'templateRules': [],
                                                                       'providerRules': rules},
                                               'modelConfigRules': {}}}))
        return str(path)

    def credentials(self, root, keys):
        """A host credential store holding the given key names."""
        store = Path(root) / '.zcode' / 'v2' / 'credentials.json'
        store.parent.mkdir(parents=True, exist_ok=True)
        store.write_text(json.dumps({key: 'enc:v1:secret' for key in keys}))
        return str(root)

    def setting(self, root, family='bigmodel', kind='individual-coding-plan'):
        """A host setting recording the plan the host itself selected."""
        path = Path(root) / '.zcode' / 'v2' / 'setting.json'
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps({'providerFamilyDomain': family,
                                    'providerFamilyConnectionSelections': {family: {'kind': kind}}}))
        return str(root)

    def account_key(self, provider, identity='77971787107705163'):
        return f'account-provider:coding-plan:{provider}:account:{identity}:api-key'

    def test_revision_is_the_catalogue_revision_and_the_hash_of_its_path(self):
        # The agent answers with sha256(path.resolve(catalogue)), not of the bytes:
        # the same catalogue at a different path is a different revision.
        import hashlib
        with tempfile.TemporaryDirectory() as t:
            first = self.catalogue(Path(t))
            self.assertEqual(zcode.builtin_provider_revision(first),
                             'zcode-builtin:28:' + hashlib.sha256(os.path.abspath(first).encode()).hexdigest())
            moved = Path(t) / 'elsewhere' / 'zcode-builtin.json'
            moved.parent.mkdir(); shutil.copy(first, moved)
            self.assertNotEqual(zcode.builtin_provider_revision(first),
                                zcode.builtin_provider_revision(str(moved)))

    def test_unreadable_or_revisionless_catalogue_is_refused(self):
        with tempfile.TemporaryDirectory() as t:
            missing = str(Path(t) / 'absent.json')
            with self.assertRaisesRegex(RuntimeBlocked, 'cannot read'):
                zcode.builtin_provider_revision(missing)
            blank = Path(t) / 'blank.json'; blank.write_text('{"config": {}}')
            with self.assertRaisesRegex(RuntimeBlocked, 'no revision'):
                zcode.builtin_provider_revision(str(blank))

    def test_only_providers_the_host_provisioned_and_the_catalogue_declares(self):
        with tempfile.TemporaryDirectory() as t:
            catalogue = self.catalogue(Path(t), providers=(self.ACCOUNT, self.TEAM))
            data = self.credentials(t, [self.account_key(self.TEAM),
                                        self.account_key(self.ACCOUNT),
                                        'oauth:bigmodel:access_token',
                                        self.account_key('account:not-in-catalogue'),
                                        'account-provider:coding-plan:' + self.ACCOUNT + ':identity'])
            with patch.dict(os.environ, {'ZCODE_DATA_BASE_DIR': data}):
                # The store's own order is kept; unrelated and unknown keys are not
                # account provisioning entries and are left out.
                self.assertEqual(zcode.provisioned_account_providers(catalogue),
                                 [self.TEAM, self.ACCOUNT])

    def test_a_host_without_provisioned_accounts_delivers_nothing(self):
        with tempfile.TemporaryDirectory() as t:
            catalogue = self.catalogue(Path(t))
            data = self.credentials(t, ['oauth:bigmodel:access_token'])
            with patch.dict(os.environ, {'ZCODE_DATA_BASE_DIR': data}):
                self.assertEqual(zcode.provisioned_account_providers(catalogue), [])
                self.assertIsNone(zcode.account_provider_configuration(catalogue))

    def test_overlay_reports_the_host_state_and_grants_no_entitlement_it_lacks(self):
        with tempfile.TemporaryDirectory() as t:
            catalogue = self.catalogue(Path(t))
            data = self.credentials(t, [self.account_key(self.TEAM), self.account_key(self.ACCOUNT)])
            with patch.dict(os.environ, {'ZCODE_DATA_BASE_DIR': data}):
                delivered = zcode.account_provider_configuration(
                    catalogue, {'providerId': self.ACCOUNT, 'modelId': 'GLM-5.3'})
            configuration = delivered['configuration']
            self.assertEqual(configuration['basedOnZCodeBuiltinRevision'], delivered['based_on'])
            self.assertEqual(set(configuration['providers']), {self.ACCOUNT, self.TEAM})
            for provider, value in configuration['providers'].items():
                self.assertEqual(value, {'access': {'type': 'zhipu-account', 'entitled': True}})
                # The agent requires a boolean current for every entitled account
                # provider, and exactly one of them is the preferred selection.
                self.assertEqual(configuration['states'][provider],
                                 {'availability': 'available', 'entitled': True,
                                  'current': provider == self.ACCOUNT})

    def test_a_requested_provider_that_is_not_provisioned_is_not_preferred(self):
        with tempfile.TemporaryDirectory() as t:
            catalogue = self.catalogue(Path(t))
            data = self.credentials(t, [self.account_key(self.TEAM)])
            with patch.dict(os.environ, {'ZCODE_DATA_BASE_DIR': data}):
                delivered = zcode.account_provider_configuration(
                    catalogue, {'providerId': self.ACCOUNT, 'modelId': 'GLM-5.3'})
            self.assertEqual(delivered['current'], self.TEAM)
            self.assertEqual(delivered['current_source'], 'provisioning-order')

    def test_the_plan_the_host_itself_selected_is_preferred(self):
        # The host records family plus plan kind; the catalogue declares each
        # provider's own accountType and mode, so the two are matched on those
        # fields rather than on the order the credential store happens to use.
        modes = {self.TEAM: 'team-coding-plan', self.ACCOUNT: 'individual-coding-plan'}
        for kind, expected in (('individual-coding-plan', self.ACCOUNT),
                               ('team-coding-plan', self.TEAM)):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as t:
                catalogue = self.catalogue(Path(t), modes=modes)
                data = self.credentials(t, [self.account_key(self.TEAM), self.account_key(self.ACCOUNT)])
                self.setting(data, 'bigmodel', kind)
                with patch.dict(os.environ, {'ZCODE_DATA_BASE_DIR': data}):
                    delivered = zcode.account_provider_configuration(catalogue)
                self.assertEqual(delivered['current'], expected)
                self.assertEqual(delivered['current_source'], 'host-selected-plan')

    def test_a_host_setting_for_another_family_is_left_alone(self):
        with tempfile.TemporaryDirectory() as t:
            catalogue = self.catalogue(Path(t))
            data = self.credentials(t, [self.account_key(self.TEAM), self.account_key(self.ACCOUNT)])
            self.setting(data, 'zai', 'individual-coding-plan')
            with patch.dict(os.environ, {'ZCODE_DATA_BASE_DIR': data}):
                delivered = zcode.account_provider_configuration(catalogue)
            self.assertEqual(delivered['current'], self.TEAM)
            self.assertEqual(delivered['current_source'], 'provisioning-order')

    def test_a_setting_that_expresses_no_usable_plan_is_not_guessed_at(self):
        for setting in ({}, {'providerFamilyDomain': 'bigmodel'},
                        {'providerFamilyDomain': 'bigmodel', 'providerFamilyConnectionSelections': []}):
            with self.subTest(setting=setting), tempfile.TemporaryDirectory() as t:
                catalogue = self.catalogue(Path(t))
                data = self.credentials(t, [self.account_key(self.TEAM)])
                path = Path(data) / '.zcode' / 'v2' / 'setting.json'
                path.parent.mkdir(parents=True, exist_ok=True); path.write_text(json.dumps(setting))
                with patch.dict(os.environ, {'ZCODE_DATA_BASE_DIR': data}):
                    delivered = zcode.account_provider_configuration(catalogue)
                self.assertEqual(delivered['current'], self.TEAM)

    def execute(self, directory, binding, wanted=MODEL, reply=None, **kwargs):
        """Run a probe against a mock host, returning the methods it was sent."""
        calls = []
        client = Mock(identity='', events=[], interactions=[])
        snapshot = {'session': {'sessionId': 'root'},
                    'protocol': {'name': 'ZCode Protocol', 'version': 1},
                    'settings': {'model': {'current': MODEL, 'available': [{'ref': MODEL}]},
                                 'thoughtLevel': {'current': ''}}}

        def respond(method, params, **_kwargs):
            calls.append((method, params))
            if method == zcode.ACCOUNT_CONFIG_METHOD:
                if reply is not None:
                    return reply
                return {'receivedRevision': params['revision'],
                        'providerCount': len(params['providers']), 'status': 'received'}
            if method in ('session/create', 'session/resume'):
                return snapshot
            return {}

        client.request.side_effect = respond
        request = {'workspace': str(directory), 'read_only': False, 'identity': '',
                   'model': json.dumps(wanted) if wanted else '', 'prompt': 'Current task',
                   'schema': {}, 'binding': binding}
        with patch.object(zcode, 'Client', return_value=client), \
             patch.object(zcode, 'materialize_source', return_value={'provider': 'zcode'}):
            zcode.execute(request, directory, probe=True, **kwargs)
        return calls

    def test_the_overlay_precedes_the_first_snapshot(self):
        with tempfile.TemporaryDirectory() as t:
            directory = Path(t) / 'evidence'; directory.mkdir()
            catalogue = self.catalogue(Path(t))
            data = self.credentials(t, [self.account_key(self.ACCOUNT)])
            binding = {'provider': 'zcode', 'launch': {'executable': 'node', 'script': 'app.cjs'},
                       'files': {}, 'environment': {'ZCODE_BUILTIN_PROVIDER_CONFIG_FILE': catalogue}}
            with patch.dict(os.environ, {'ZCODE_DATA_BASE_DIR': data}):
                calls = self.execute(directory, binding)
            self.assertEqual([method for method, _ in calls],
                             [zcode.ACCOUNT_CONFIG_METHOD, 'session/create', 'session/close'])
            method, params = calls[0]
            self.assertEqual(params['basedOnZCodeBuiltinRevision'],
                             zcode.builtin_provider_revision(catalogue))
            self.assertEqual(set(params['providers']), {self.ACCOUNT})
            recorded = read(directory / 'account-provider.json')
            self.assertEqual(recorded['providers'], [self.ACCOUNT])
            self.assertEqual(recorded['source'], 'host credential index')

    def test_a_host_that_resolves_its_own_catalogue_is_never_given_one(self):
        # Without a pinned catalogue the launcher does not own the path whose hash
        # is the revision, so it cannot deliver and must not guess.
        with tempfile.TemporaryDirectory() as t:
            directory = Path(t) / 'evidence'; directory.mkdir()
            catalogue = self.catalogue(Path(t))
            data = self.credentials(t, [self.account_key(self.ACCOUNT)])
            binding = {'provider': 'zcode', 'launch': {'executable': 'node', 'script': 'app.cjs'}, 'files': {}}
            with patch.dict(os.environ, {'ZCODE_DATA_BASE_DIR': data}):
                calls = self.execute(directory, binding)
            self.assertEqual([method for method, _ in calls], ['session/create', 'session/close'])
            self.assertFalse((directory / 'account-provider.json').exists())

    def test_a_refused_delivery_is_reported_and_not_silently_ignored(self):
        with tempfile.TemporaryDirectory() as t:
            directory = Path(t) / 'evidence'; directory.mkdir()
            catalogue = self.catalogue(Path(t))
            data = self.credentials(t, [self.account_key(self.ACCOUNT)])
            binding = {'provider': 'zcode', 'launch': {'executable': 'node', 'script': 'app.cjs'},
                       'files': {}, 'environment': {'ZCODE_BUILTIN_PROVIDER_CONFIG_FILE': catalogue}}
            for reply in ({}, {'status': 'received', 'providerCount': 0},
                          {'status': 'rejected', 'providerCount': 1}):
                with self.subTest(reply=reply), patch.dict(os.environ, {'ZCODE_DATA_BASE_DIR': data}), \
                     self.assertRaisesRegex(RuntimeBlocked, 'did not accept'):
                    self.execute(directory, binding, reply=reply)


class ReasoningLevelBinding(unittest.TestCase):
    """A bound model has to carry a reasoning level the host itself advertised.

    3.12.3 refuses an object model selection without options.reasoningLevel, and
    the level must be one of that model's own advertised values. Neither is
    guessed here: the level comes from the request when one was asked for and from
    the host's own report otherwise.
    """

    REF = {'providerId': 'account:bigmodel-individual-coding-plan', 'modelId': 'GLM-5.3'}

    def entry(self, **extra):
        return {'ref': dict(self.REF), 'label': 'GLM-5.3', **extra}

    def levels(self, levels=('low', 'high', 'max'), default='max'):
        reasoning = {'levels': [{'value': v, 'label': v} for v in levels]}
        if default is not None:
            reasoning['defaultLevel'] = default
        return reasoning

    def apply(self, available, wanted=REF, effort=''):
        """Apply a selection against a host that echoes back what it was given."""
        calls = []
        client = Mock(identity='root')
        snapshot = {'settings': {'model': {'current': None, 'available': available},
                                 'thoughtLevel': {'current': ''}}}

        applied = {'model': None, 'thoughtLevel': ''}

        def settings():
            return {'model': {'current': applied['model'], 'available': available},
                    'thoughtLevel': {'current': applied['thoughtLevel']}}

        def respond(method, params, **_kwargs):
            calls.append((method, params))
            if method == 'session/setModel':
                applied['model'] = params['model']
            elif method == 'session/setThoughtLevel':
                applied['thoughtLevel'] = params['thoughtLevel']
            return {'settings': settings()}

        client.request.side_effect = respond
        with tempfile.TemporaryDirectory() as t:
            configured = zcode.apply_selection(client, snapshot, wanted, effort, Path(t), t)
        return calls, configured

    def bound_model(self, calls):
        return next(params['model'] for method, params in calls if method == 'session/setModel')

    def test_the_hosts_own_default_is_bound_and_never_invented(self):
        calls, configured = self.apply([self.entry(reasoning=self.levels())])
        self.assertEqual(self.bound_model(calls)['options'], {'reasoningLevel': 'max'})
        self.assertEqual(configured['model'], canonical(self.REF).decode())

    def test_a_requested_effort_is_bound_with_the_model(self):
        calls, _ = self.apply([self.entry(reasoning=self.levels())], effort='low')
        self.assertEqual(self.bound_model(calls)['options'], {'reasoningLevel': 'low'})

    def test_the_first_advertised_value_stands_in_for_a_missing_default(self):
        for default in (None, 'unlisted'):
            with self.subTest(default=default):
                calls, _ = self.apply([self.entry(reasoning=self.levels(default=default))])
                self.assertEqual(self.bound_model(calls)['options'], {'reasoningLevel': 'low'})

    def test_levels_are_read_from_the_entry_that_names_this_exact_reference(self):
        other = {'ref': {'providerId': 'account:other', 'modelId': 'M'},
                 'reasoning': self.levels(default='high')}
        calls, _ = self.apply([other, self.entry(reasoning=self.levels(default='low', levels=('low',)))])
        self.assertEqual(self.bound_model(calls)['options'], {'reasoningLevel': 'low'})
        self.assertEqual(zcode.advertised_reasoning({'settings': {'model': {'available': [other]}}}, self.REF),
                         {'levels': [], 'default': ''})

    def test_a_model_with_no_advertised_levels_is_bound_exactly_as_given(self):
        calls, _ = self.apply([self.entry()])
        self.assertEqual(self.bound_model(calls), self.REF)
        self.assertEqual(zcode.bound_model({'settings': {'model': {'available': [self.entry()]}}},
                                           self.REF, ''), self.REF)

    def test_an_unadvertised_model_is_still_refused_before_any_change(self):
        calls = []
        client = Mock(identity='root')
        snapshot = {'settings': {'model': {'current': None, 'available': [self.entry(reasoning=self.levels())]},
                                 'thoughtLevel': {'current': ''}}}
        client.request.side_effect = lambda method, params, **_: calls.append(method)
        with tempfile.TemporaryDirectory() as t, self.assertRaisesRegex(RuntimeBlocked, 'no fallback'):
            zcode.apply_selection(client, snapshot, {'providerId': 'account:other', 'modelId': 'M'},
                                  '', Path(t), t)
        self.assertEqual(calls, [])


if __name__ == '__main__':
    unittest.main()
