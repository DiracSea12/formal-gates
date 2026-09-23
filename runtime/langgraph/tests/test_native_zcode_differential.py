"""Differential test for the G2c ZCode native execution path.

The pre-migration ZCode app-server transport is kept verbatim in
`reference_native_zcode.py` (test-only, built on `reference_native_transport`,
like `reference_native_dsh.py` for G2b). This test runs the Go adapter
(`formal-gates harness native <dir>`, which dispatches through the Go registry
to internal/harness/native_zcode.go) and the reference implementation over the
same native-request.json contract, each in its OWN evidence directory, and
asserts the two are observably equal.

Comparison faces (the G2c contract's value-level equivalence, same discipline
as test_native_dsh_differential.py):

- by value, parsed and compared as JSON after normalising each side's own root
  path (and the per-side fake app-server path inside the request's binding):
  every atomically written evidence file (native-request.json, identity.json,
  tool-policy.json, native-source.json, account-provider.json, source.json,
  answer.json, interaction-status.json). Both sides render atomics canonically;
  the values must be equal.
- byte for byte: the native answer text (answer.raw.txt), which is the model's
  own text on both sides.
- the prompt: the fixed head byte for byte, the schema JSON embedded after it
  by value (the reference renders it with Python separators, Go canonically -
  the registered bounded difference, same as G2b). One more registered side
  asymmetry in the same face: the Go side presents each object's
  additionalProperties key last instead of first (2026-09 GLM serving echoed
  the leading directive keyword back into answers; the value is unchanged,
  pinned by internal/harness/native_zcode_test.go and
  native_zcode_unix_test.go).
- by value, parsed and compared as JSON per line: the outbound protocol lines
  the host echoed (host-frames.jsonl - including the session/create and
  session/send parameter values; the reference sends default-separator bytes,
  Go canonical), the routing-only trace (protocol.jsonl), the host's record of
  frames it answered (host-replies.jsonl), the committed notification evidence
  (native-events.jsonl), and the Go command's stdout result line (the embedded
  binding normalised to each side's own directory).
- the error records: the same exception-family prefix and the same message,
  with the registered differences - a JSON body embedded in an error text (the
  ZCODE_PROTOCOL refusal, the account-config refusal) is Python-shaped on the
  reference side and canonical on the Go side, so those cases compare the
  embedded body by value and expect each side's own rendering.
- the setModel host contract (probed 2026-09-19 against the host updated that
  morning): the method's params schema rejects a string model as invalid
  params, an object without options.reasoningLevel is refused by the registry,
  and an accepted object is confirmed with the options in the readback and
  moves thoughtLevel with it. The fake mirrors that contract for both sides;
  both sides already send the one accepted shape (the object with an
  advertised options.reasoningLevel), so no comparison-layer difference is
  needed. One registered side asymmetry: when the snapshot advertises no
  models at all (an empty provider registry, where the real host's setModel
  cannot apply anything), the Go adapter refuses before firing the frame with
  the transport's own "not advertised; no fallback" text, while the frozen
  reference still sends it - pinned by the Go-side-only scenario below, same
  discipline as the applied-credential half.
- the permission-mode contract (2026-09-20, the drop of the forced
  setMode(yolo) that predated the G2d interaction hold): a request without
  permission_mode switches nothing on the Go side - the session runs the
  host's own default mode and the evidence records what the host itself
  reported - while the frozen reference still declares yolo on every new
  non-read-only session (its era predates the drop). The registered
  divergence is pinned per side by the default-mode scenario below; the
  differentially comparable scenarios below request permission_mode='yolo'
  explicitly, the one value where the frozen reference and the Go explicit
  path coincide frame for frame. An explicit permission_mode the host does
  not apply is refused on both sides through the same readback contract
  (the mismatch scenario below). Resumed and read-only sessions never
  switch modes on either side.

The two sides are deliberately asymmetric processes: the reference is called
in-process (reference_native_zcode.execute), while the Go side is its own
process, because one native job in one process is the Go implementation's
whole shape. When the reference raises, the test lands native-error.json with
exactly the five lines native_entry.py used, so error records compare too.

The host is a fake app-server (FAKE_ZCODE_HOST below, after the FAKE_DSH_HOST
precedent): it mirrors what the real app-server makes observable - the
snapshot handshake (protocol block, session.sessionId, settings with
model.current/available, thoughtLevel and mode), session/setMode answering
through settings while its projection block stays a stale snapshot (the real
host's observed shape; settings is the authoritative confirmation),
session/setModel/setThoughtLevel readbacks, provider/updateAccountConfig
receipts, the session/send ack followed by the session/event notification
stream keyed by inputId/turnId/seq, client-directed reverse requests
(interaction/requestProviderRuntimeHeaders, interaction/requestPermission),
and the resume refusal with the host's own -32004 "Session not found:" text.

Coverage division for the faces the relocation itself makes unequal: the
reference executes its credential helper next to itself, where none exists, so
a runtime-headers request it receives is always answered "no provisioned
credential" - the differentially comparable case - while the applied-secret
half of that interaction is pinned by internal/harness/
native_zcode_unix_test.go (TestZcodeExecuteRuntimeHeadersCredential) and by
the Go-side-only scenario below. The same applies to the turn budget: the
reference's 900-second bound is a hardcoded literal without a patchable
attribute (the G2b dsh precedent patched Client.default_timeout instead), so
the reference-side case below pins the per-request timeout text and the Go
side's two turn-wait ends are pinned by TestZcodeExecuteBoundedWait.
"""
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import reference_native_zcode
from formal_gates_harness.infra import HarnessError, RuntimeBlocked, SessionUnavailable, atomic, file_proof, read
from formal_gates_harness.native_hosts import GoAdapter

REPO = Path(__file__).resolve().parents[3]
BINARY = REPO / '.harness' / 'bin' / 'formal-gates'

ANSWER = {'status': 'PASS', 'summary': 'observed', 'findings': [], 'check_ids': []}
# additionalProperties rides at the top level and inside an array item: the Go
# side presents it last in each object (the registered asymmetry in the module
# docstring), the reference keeps its dict order; every comparison face below
# stays by value.
SCHEMA = {'additionalProperties': False,
          'properties': {'findings': {'items': {'additionalProperties': True, 'type': 'object'},
                                      'type': 'array'},
                         'status': {'type': 'string'}},
          'required': ['status', 'summary'], 'type': 'object'}
PROMPT = 'Review the requirement.'
PROMPT_HEAD = PROMPT + '\nReturn ONLY one JSON object matching this schema:\n'
ANSWER_TEXT = 'Done.\n```json\n' + json.dumps(ANSWER) + '\n```'
CANONICAL_REF = json.dumps({'providerId': 'p1', 'modelId': 'm1'}, sort_keys=True,
                           separators=(',', ':'))

# The by-value faces (see the module docstring for the rendering differences).
# source.json is compared by its own assertion: three of its four file proofs
# cover byte-identical evidence, while the native-events.jsonl proof covers
# each side's own rendering of the registered serialization difference.
JSON_FILES = ('native-request.json', 'identity.json', 'tool-policy.json', 'native-source.json',
              'account-provider.json', 'answer.json', 'interaction-status.json')
SOURCE_FILE = 'source.json'
VALUE_FILES = ('protocol.jsonl', 'host-frames.jsonl', 'host-replies.jsonl', 'native-events.jsonl')
RAW_FILES = ('answer.raw.txt',)

SETTINGS = {'model': {'current': {'providerId': 'p1', 'modelId': 'm1'},
                      'available': [{'ref': {'providerId': 'p1', 'modelId': 'm1'},
                                     'reasoning': {'levels': [{'value': 'high'}, {'value': 'low'}],
                                                   'defaultLevel': 'low'}}]},
            'thoughtLevel': {'current': 'low'},
            'mode': {'current': 'default'}}
# A host that advertises m9 next to its current m1: the wanted reference is
# advertised, so the readback after setModel is what decides.
SETTINGS_WITH_M9 = {'model': {'current': {'providerId': 'p1', 'modelId': 'm1'},
                              'available': [{'ref': {'providerId': 'p1', 'modelId': 'm1'},
                                             'reasoning': {'levels': [{'value': 'high'}],
                                                           'defaultLevel': 'high'}},
                                            {'ref': {'providerId': 'p1', 'modelId': 'm9'},
                                             'reasoning': {'levels': [{'value': 'high'}],
                                                           'defaultLevel': 'high'}}]},
                    'thoughtLevel': {'current': 'low'},
                    'mode': {'current': 'default'}}
# A host whose current model differs from the wanted one, both advertised with
# levels: the normal setModel path (frame shape plus the readback contract).
SETTINGS_APPLY = {'model': {'current': {'providerId': 'p1', 'modelId': 'm0'},
                            'available': [{'ref': {'providerId': 'p1', 'modelId': 'm0'},
                                           'reasoning': {'levels': [{'value': 'high'}, {'value': 'low'}],
                                                         'defaultLevel': 'low'}},
                                          {'ref': {'providerId': 'p1', 'modelId': 'm1'},
                                           'reasoning': {'levels': [{'value': 'high'}, {'value': 'low'}],
                                                         'defaultLevel': 'low'}}]},
                  'thoughtLevel': {'current': 'low'},
                  'mode': {'current': 'default'}}

RUNTIME_HEADERS = 'interaction/requestProviderRuntimeHeaders'


FAKE_ZCODE_HOST = '''#!PYTHON
"""Fake ZCode app-server: mirrors the observable behaviour of the real host for
the G2c differential. Speaks the plain {id, method, params} shape on stdio,
answers the snapshot sequence with settings as the authoritative confirmation
(the projection block stays a stale snapshot, the real host's shape), streams
session/event notifications after the accepted input, asks the client-directed
reverse requests the knobs name, and echoes every received frame, every reply
frame and the prompt text for the comparison. Its evidence directory is its own
location, so the two sides never share a directory."""
import json
import sys
from pathlib import Path


def main():
    directory = Path(__file__).resolve().parent
    knobs_path = directory / 'fake-host-config.json'
    knobs = json.loads(knobs_path.read_text(encoding='utf-8')) if knobs_path.exists() else {}
    fail = knobs.get('fail_mode')
    if fail in ('nonjson', 'silent'):
        if fail == 'nonjson':
            sys.stdout.write('this line is not json\\n')
            sys.stdout.flush()
        with (directory / 'host-frames.jsonl').open('a', encoding='utf-8') as frames:
            for line in sys.stdin:
                frames.write(line)
                frames.flush()
        return
    frames = (directory / 'host-frames.jsonl').open('a', encoding='utf-8')
    replies = (directory / 'host-replies.jsonl').open('a', encoding='utf-8')

    def send(value):
        sys.stdout.write(json.dumps(value, ensure_ascii=False) + '\\n')
        sys.stdout.flush()

    def answer(ident, payload):
        send({'id': ident, **payload})

    for reverse in knobs.get('early_reverse_requests', []):
        send({'id': reverse.get('id', 'r-early'), 'method': reverse['method'],
              'params': reverse.get('params', {})})
    session_id = knobs.get('session_id', 's-1')
    settings = dict(knobs.get('settings') or DEFAULT_SETTINGS)

    def snapshot():
        return {'protocol': knobs.get('protocol', {'name': 'ZCode Protocol', 'version': 1}),
                'session': {'sessionId': session_id}, 'settings': settings}

    def settings_reply(reply_settings=None):
        # The real host confirms through settings; its projection block can be
        # a stale snapshot (sessionId "unknown", pre-request values).
        return {'session': {'sessionId': 'unknown'},
                'settings': settings if reply_settings is None else reply_settings}

    for line in sys.stdin:
        line = line.rstrip('\\n')
        frames.write(line + '\\n')
        frames.flush()
        message = json.loads(line)
        method = message.get('method')
        ident = message.get('id')
        if not method:
            # A frame without a method is a reply to one of our own requests.
            replies.write(line + '\\n')
            replies.flush()
            continue
        params = message.get('params') or {}
        errors = knobs.get('errors') or {}
        if method in ('session/create', 'session/resume'):
            failure = knobs.get('resume_error') if method == 'session/resume' else knobs.get('create_error')
            if failure:
                answer(ident, {'error': failure})
            else:
                answer(ident, {'result': snapshot()})
        elif method == 'provider/updateAccountConfig':
            reply = dict(knobs.get('account_reply') or {'status': 'received', 'providerCount': None})
            if reply.get('providerCount') is None:
                reply['providerCount'] = len(params.get('providers') or {})
            answer(ident, {'result': reply})
        elif method == 'session/setMode':
            applied = knobs.get('setMode_mode')
            settings['mode'] = {'current': applied if applied is not None else params.get('mode')}
            answer(ident, {'result': settings_reply()})
        elif method == 'session/setModel':
            # The 2026-09-19 host contract, probed: the params schema rejects a
            # model that is not an object, an object without
            # options.reasoningLevel is refused by the registry, and an
            # accepted object is confirmed with the options in the readback
            # and moves the thought level with it.
            model = params.get('model')
            override = knobs.get('setModel_settings')
            if not isinstance(model, dict):
                answer(ident, {'error': {'code': -32602,
                                         'message': 'Invalid params — model: Invalid input: '
                                                    'expected object, received '
                                                    + ('string' if isinstance(model, str) else type(model).__name__)}})
            elif not isinstance(model.get('options'), dict) or not model['options'].get('reasoningLevel'):
                answer(ident, {'error': {'code': -32603,
                                         'message': 'Reasoning level is required for '
                                                    + f"{model.get('providerId')}/{model.get('modelId')}"}})
            elif override is not None:
                # The host answers a settings block that does not reflect the
                # request: the readback is what the client must trust.
                answer(ident, {'result': settings_reply(override)})
            else:
                settings['model'] = dict(settings.get('model') or {}, current=dict(model))
                settings['thoughtLevel'] = {'current': model['options']['reasoningLevel']}
                answer(ident, {'result': settings_reply()})
        elif method == 'session/setThoughtLevel':
            override = knobs.get('setThoughtLevel_settings')
            if override is None:
                settings['thoughtLevel'] = {'current': params.get('thoughtLevel')}
                answer(ident, {'result': settings_reply()})
            else:
                answer(ident, {'result': settings_reply(override)})
        elif method == 'session/subscribe':
            answer(ident, {'result': knobs.get('subscribe_reply', {'sessionId': session_id})})
        elif method == 'session/send':
            (directory / 'host-prompt.txt').write_text(params.get('content', ''), encoding='utf-8')
            for reverse in knobs.get('reverse_requests', []):
                send({'id': reverse.get('id', 'r-1'), 'method': reverse['method'],
                      'params': reverse.get('params', {})})
            failure = errors.get('session/send')
            if failure:
                answer(ident, {'error': failure})
            else:
                answer(ident, {'result': knobs.get('send_reply', {'sessionId': session_id,
                                                                  'accepted': True})})
                for record in knobs.get('events',
                                        default_events(session_id, 't-1', params.get('inputId'))):
                    send({'method': 'session/event', 'params': record})
        elif method == 'session/close':
            failure = errors.get('session/close')
            if failure:
                answer(ident, {'error': failure})
            else:
                answer(ident, {'result': {}})
            break
        else:
            failure = errors.get(method)
            if failure:
                answer(ident, {'error': failure})
            else:
                answer(ident, {'result': {}})
    frames.close()
    replies.close()


DEFAULT_SETTINGS = ''' + repr(SETTINGS) + '''


def default_events(session_id, turn_id, input_id):
    response = 'Done.\\n```json\\n' + json.dumps({'status': 'PASS', 'summary': 'observed',
                                                 'findings': [], 'check_ids': []}) + '\\n```'
    return [
        {'sessionId': session_id, 'seq': 1, 'type': 'turn.started', 'turnId': turn_id,
         'payload': {'inputId': input_id}},
        {'sessionId': session_id, 'seq': 2, 'type': 'assistant/message', 'turnId': turn_id,
         'payload': {'inputId': 'other-input'}},
        {'sessionId': session_id, 'seq': 3, 'type': 'turn.completed', 'turnId': turn_id,
         'payload': {'inputId': input_id, 'resultType': 'success', 'response': response,
                     'usage': {'inputTokens': 9, 'outputTokens': 2}}},
    ]


if __name__ == '__main__':
    main()
'''


def ensure_driver_binary():
    """Build the driver from the current sources once, then bind the bridge."""
    if not BINARY.exists():
        subprocess.run(['go', 'build', '-o', str(BINARY), './cmd/formal-gates'],
                       cwd=str(REPO), check=True)
    os.environ['FORMAL_GATES_HARNESS_ROOT'] = str(REPO)
    os.environ['FORMAL_GATES_HARNESS_BINARY'] = str(BINARY)


class ZCodeDifferential(unittest.TestCase):
    """Every scenario runs both sides over the same request contract."""

    @classmethod
    def setUpClass(cls):
        ensure_driver_binary()

    def scenario(self, knobs=None, identity='', probe=False, model='', reasoning_effort='',
                 read_only=True, environment=None, env=None, prewrite=None, root=None,
                 permission_mode=''):
        """One shared root: workspace and knobs shared, evidence dirs per side.

        The binding names a per-side copy of the fake app-server as the launch
        script (the argv is fixed [executable, script, --cwd, workspace,
        app-server], so the script's own location is the only per-side channel
        the host observes); the comparison normalises each side's directory out
        of the embedded paths. Pass `root` to place the scenario inside a tree
        that already holds shared fixtures (an account catalogue).
        `permission_mode` lands in the request only when named: the Go adapter
        switches a NEW non-read-only session to it and verifies the host
        readback, while the frozen reference ignores the field (the registered
        divergence in the module docstring).
        """
        if root is None:
            root = Path(tempfile.mkdtemp(prefix='g2c-zcode-')).resolve()
            self.addCleanup(shutil.rmtree, root, ignore_errors=True)
        workspace = root / 'ws'
        workspace.mkdir(exist_ok=True)
        self.env = env or {}
        sides = {}
        for side in ('ref', 'go'):
            # The evidence directories live one level apart and share the same
            # basename, so the turn's input id ('formal-gates-<basename>') is
            # identical on both sides and the proofs of the evidence files it
            # names stay comparable. Each side's directory path still differs,
            # which is what the comparison normalises out.
            directory = (root / f'{side}-side' / 'job').resolve()
            directory.mkdir(parents=True, exist_ok=True)
            host = directory / 'fake-app-server.py'
            host.write_text(FAKE_ZCODE_HOST.replace('#!PYTHON', '#!' + sys.executable), encoding='utf-8')
            host.chmod(0o755)
            binding = {'provider': 'zcode',
                       'launch': {'executable': sys.executable, 'script': str(host)},
                       'files': {str(host): file_proof(host),
                                 sys.executable: file_proof(Path(sys.executable))}}
            if environment:
                binding['environment'] = dict(environment)
            request = {'provider': 'zcode', 'binding': binding, 'workspace': str(workspace),
                       'read_only': read_only, 'identity': identity, 'model': model,
                       'reasoning_effort': reasoning_effort, 'prompt': PROMPT, 'schema': SCHEMA}
            if permission_mode:
                request['permission_mode'] = permission_mode
            if probe:
                request['probe'] = True
            atomic(directory / 'native-request.json', request)
            if knobs is not None:
                # Plain (order-preserving, non-canonical) bytes: the knobs are
                # host input scaffolding. Both sides get the same bytes.
                (directory / 'fake-host-config.json').write_text(
                    json.dumps(knobs, ensure_ascii=False), encoding='utf-8')
            for name, data in (prewrite or {}).items():
                (directory / name).write_bytes(data)
            sides[side] = directory
        return root, workspace, sides['ref'], sides['go']

    def account_fixture(self, root, providers=('p1',)):
        """A pinned catalogue plus a host data directory with provisioned keys."""
        data = root / 'host-data'
        v2 = data / '.zcode' / 'v2'
        v2.mkdir(parents=True, exist_ok=True)
        rules = [{'providerId': provider,
                  'config': {'access': {'type': 'zhipu-account', 'accountType': 'bigmodel',
                                        'mode': 'coding-plan'}}} for provider in providers]
        catalogue = root / 'zcode-builtin.json'
        catalogue.write_text(json.dumps({'revision': 7,
                                         'config': {'providerConfigRules': {'providerRules': rules}}}),
                             encoding='utf-8')
        store = v2 / 'credentials.json'
        store.write_text(json.dumps(
            {f'account-provider:coding-plan:{provider}:account:u1:api-key': 'enc:v1:never-read'
             for provider in providers}), encoding='utf-8')
        return data, catalogue, v2

    # --- side runners ----------------------------------------------------
    def run_reference(self, directory, probe=False, timeout=None):
        """In-process reference call; native_entry.py's five error lines on failure."""
        request = read(directory / 'native-request.json')
        managers = [patch.dict(os.environ, self.env)]
        if timeout is not None:
            managers.append(patch.object(reference_native_zcode.Client, 'default_timeout', timeout))
        for manager in managers:
            manager.start()
        try:
            result = reference_native_zcode.execute(request, directory, probe=probe)
            return result, None
        except Exception as exc:  # noqa: BLE001 - mirrored into the error record
            detail = {'error': f'{type(exc).__name__}: {exc}'}
            if isinstance(exc, SessionUnavailable):
                detail.update(error_kind='SESSION_UNAVAILABLE', identity=exc.identity,
                              method=exc.method, native_code=exc.code)
            atomic(directory / 'native-error.json', detail)
            return None, exc
        finally:
            for manager in managers:
                manager.stop()

    def run_go(self, directory):
        env = {**os.environ, **self.env,
               'FORMAL_GATES_HARNESS_ROOT': str(REPO), 'FORMAL_GATES_HARNESS_BINARY': str(BINARY)}
        return subprocess.run([str(BINARY), 'harness', 'native', str(directory)],
                              capture_output=True, env=env)

    # --- comparison ------------------------------------------------------
    def normalized(self, directory, name):
        return (directory / name).read_bytes().replace(str(directory).encode(), b'<job>')

    def normalize_paths(self, value, directory):
        """Rewrite one side's own directory (as a value or an object key) out of
        every embedded string."""
        if isinstance(value, str):
            return value.replace(str(directory), '<job>')
        if isinstance(value, list):
            return [self.normalize_paths(item, directory) for item in value]
        if isinstance(value, dict):
            return {key.replace(str(directory), '<job>') if isinstance(key, str) else key:
                    self.normalize_paths(item, directory) for key, item in value.items()}
        return value

    def jsonl_values(self, directory, name):
        """Parsed values of one JSONL file; splitlines makes this line-ending safe."""
        values = []
        for line in (directory / name).read_text(encoding='utf-8').splitlines():
            if line.strip():
                values.append(self.normalize_prompt_strings(
                    self.normalize_paths(json.loads(line), directory)))
        return values

    def normalize_prompt_strings(self, value):
        """Rewrite the schema JSON embedded in a prompt string to one canonical
        form: the outbound session/send frame carries the prompt text, whose
        embedded schema the reference renders with Python separators and Go
        canonically (the registered difference; the values are equal)."""
        if isinstance(value, str) and value.startswith(PROMPT_HEAD):
            try:
                schema = json.loads(value[len(PROMPT_HEAD):])
            except ValueError:
                return value
            return PROMPT_HEAD + json.dumps(schema, ensure_ascii=False,
                                            sort_keys=True, separators=(',', ':'))
        if isinstance(value, list):
            return [self.normalize_prompt_strings(item) for item in value]
        if isinstance(value, dict):
            return {key: self.normalize_prompt_strings(item) for key, item in value.items()}
        return value

    def compare_bytes(self, ref_dir, go_dir, names):
        for name in names:
            ref_exists = (ref_dir / name).exists()
            go_exists = (go_dir / name).exists()
            self.assertEqual(ref_exists, go_exists, f'{name}: existence differs')
            if ref_exists:
                self.assertEqual(self.normalized(ref_dir, name), self.normalized(go_dir, name),
                                 f'{name}: bytes differ after normalisation')

    def compare_json_values(self, ref_dir, go_dir, names):
        """Value-level evidence comparison: parse each side's normalised bytes."""
        for name in names:
            ref_exists = (ref_dir / name).exists()
            go_exists = (go_dir / name).exists()
            self.assertEqual(ref_exists, go_exists, f'{name}: existence differs')
            if ref_exists:
                self.assertEqual(json.loads(self.normalized(ref_dir, name)),
                                 json.loads(self.normalized(go_dir, name)),
                                 f'{name}: values differ')

    def compare_values(self, ref_dir, go_dir, names):
        for name in names:
            ref_exists = (ref_dir / name).exists()
            go_exists = (go_dir / name).exists()
            self.assertEqual(ref_exists, go_exists, f'{name}: existence differs')
            if ref_exists:
                self.assertEqual(self.jsonl_values(ref_dir, name), self.jsonl_values(go_dir, name),
                                 f'{name}: values differ')

    def assert_prompt_equivalent(self, ref_dir, go_dir):
        """host-prompt.txt: the fixed head byte for byte, the schema by value
        (the reference renders it with Python separators, Go canonically)."""
        exists = (ref_dir / 'host-prompt.txt').exists()
        self.assertEqual(exists, (go_dir / 'host-prompt.txt').exists(),
                         'host-prompt.txt: existence differs')
        if not exists:
            return
        for directory in (ref_dir, go_dir):
            prompt = (directory / 'host-prompt.txt').read_text(encoding='utf-8')
            self.assertTrue(prompt.startswith(PROMPT_HEAD),
                            f'{directory}: prompt head differs: {prompt[:80]!r}')
            self.assertEqual(json.loads(prompt[len(PROMPT_HEAD):]), SCHEMA,
                             f'{directory}: embedded schema differs')

    def frames(self, directory):
        return self.jsonl_values(directory, 'host-frames.jsonl')

    def frame_with_method(self, directory, method):
        matched = [frame for frame in self.frames(directory) if frame.get('method') == method]
        self.assertEqual(len(matched), 1, f'expected exactly one {method} frame')
        return matched[0]

    def assert_process_record_types(self, *directories):
        for directory in directories:
            record = read(directory / 'native-process.json')
            self.assertIsInstance(record['pid'], int)
            self.assertTrue(record['pgid'] is None or isinstance(record['pgid'], int))

    def assert_receipts_equivalent(self, reference, go_value, ref_dir, go_dir):
        """Two returned receipts (the stdout result line): equal by value with
        each side's own directory normalised out, the same file-proof keys, the
        byte-identical evidence's proofs equal, and the native-events.jsonl
        proof - which covers each side's own rendering of the registered
        serialization difference - checked against each side's own bytes."""
        ref_rest = self.normalize_paths(
            {key: item for key, item in reference.items() if key != 'files'}, ref_dir)
        go_rest = self.normalize_paths(
            {key: item for key, item in go_value.items() if key != 'files'}, go_dir)
        self.assertEqual(ref_rest, go_rest)
        if 'files' not in reference:
            self.assertNotIn('files', go_value)
            return
        self.assertEqual(set(reference['files']), set(go_value['files']))
        for name, proof in reference['files'].items():
            if name != 'native-events.jsonl':
                self.assertEqual(proof, go_value['files'][name], f'{name}: proof differs')
        for directory, value in ((ref_dir, reference), (go_dir, go_value)):
            self.assertEqual(value['files']['native-events.jsonl'],
                             file_proof(directory / 'native-events.jsonl'))

    def assert_source_equivalent(self, ref_dir, go_dir):
        """source.json by value through the receipt comparison, plus the Go
        side's whole receipt verifying through the bridge."""
        self.assertTrue((ref_dir / SOURCE_FILE).exists())
        ref_value = json.loads(self.normalized(ref_dir, SOURCE_FILE))
        go_value = json.loads(self.normalized(go_dir, SOURCE_FILE))
        self.assert_receipts_equivalent(ref_value, go_value, ref_dir, go_dir)
        GoAdapter('zcode').verify_source(go_dir, json.loads((go_dir / SOURCE_FILE).read_text()))

    def assert_success(self, ref_dir, go_dir):
        result, error = self.run_reference(ref_dir)
        self.assertIsNone(error, msg=repr(error))
        completed = self.run_go(go_dir)
        self.assertEqual(completed.returncode, 0, completed.stderr.decode('utf-8', 'replace'))
        self.compare_json_values(ref_dir, go_dir, JSON_FILES)
        self.assert_source_equivalent(ref_dir, go_dir)
        self.compare_bytes(ref_dir, go_dir, RAW_FILES)
        self.assert_prompt_equivalent(ref_dir, go_dir)
        self.compare_values(ref_dir, go_dir, VALUE_FILES)
        self.assert_process_record_types(ref_dir, go_dir)
        # stdout carries the result as one canonical JSON line on the Go side
        # and native_entry's default-separator shape before - a registered
        # bounded difference; the receipts compare under the same discipline.
        self.assert_receipts_equivalent(result, json.loads(completed.stdout), ref_dir, go_dir)
        return result

    def assert_failure(self, ref_dir, go_dir, expected_ref, expected_type=RuntimeBlocked,
                       expected_go=None):
        """expected_go defaults to expected_ref; the registered Go rendering
        differences (a JSON body embedded in the message rendered canonically
        instead of Python-shaped) pass it explicitly."""
        if expected_go is None:
            expected_go = expected_ref
        result, error = self.run_reference(ref_dir)
        self.assertIsNone(result)
        self.assertIsInstance(error, expected_type)
        self.assertEqual(str(error), expected_ref)
        completed = self.run_go(go_dir)
        self.assertEqual(completed.returncode, 1)
        self.compare_json_values(ref_dir, go_dir, JSON_FILES)
        # A refused run lands no source receipt on either side.
        self.assertFalse((ref_dir / SOURCE_FILE).exists())
        self.assertFalse((go_dir / SOURCE_FILE).exists())
        self.assert_prompt_equivalent(ref_dir, go_dir)
        self.compare_values(ref_dir, go_dir, VALUE_FILES)
        self.assertEqual(read(ref_dir / 'native-error.json')['error'],
                         f'{type(error).__name__}: {expected_ref}')
        record = read(go_dir / 'native-error.json')
        self.assertEqual(record['error'], f'{type(error).__name__}: {expected_go}')
        self.assertEqual(completed.stderr.decode('utf-8').strip(), record['error'])
        return error

    # --- 新建成功回合 ------------------------------------------------------
    def test_new_session_success_turn(self):
        # An explicitly requested permission_mode: the one differentially
        # comparable value (see the module docstring) - the frozen reference
        # forces yolo on every new session, the Go adapter sets exactly what
        # the request names, and 'yolo' is where the two coincide.
        root, workspace, ref_dir, go_dir = self.scenario(knobs={}, read_only=False,
                                                         permission_mode='yolo')
        result = self.assert_success(ref_dir, go_dir)
        self.assertEqual(result['provider'], 'zcode')
        self.assertEqual(result['identity'], 's-1')
        self.assertEqual(result['native_turn'], 't-1')
        self.assertEqual(result['selection'],
                         {'model': CANONICAL_REF, 'reasoning_effort': 'low'})
        self.assertEqual(read(ref_dir / 'answer.json'), ANSWER)
        self.assertEqual(result['usage_evidence'], {'inputTokens': 9, 'outputTokens': 2})
        # The requested mode was declared once and the host's settings readback
        # (not the stale projection) is the recorded mode in force.
        self.assertEqual(read(ref_dir / 'tool-policy.json'),
                         {'read_only': False, 'permission_mode': 'yolo'})
        self.assertEqual(read(ref_dir / 'native-source.json')['permission_mode'], 'yolo')
        for directory in (ref_dir, go_dir):
            methods = [frame.get('method') for frame in self.frames(directory)]
            self.assertEqual(methods.count('session/setMode'), 1)
            self.assertEqual(self.frame_with_method(directory, 'session/setMode')['params']['mode'],
                             'yolo')
            self.assertLess(methods.index('session/setMode'), methods.index('session/send'))
            created = self.frame_with_method(directory, 'session/create')
            self.assertEqual(created['params']['persistence'], 'immediate')
            self.assertNotIn('sessionId', created['params'])
            self.assertNotIn('toolAllowlist', created['params'])
            self.assertNotIn('titleGenerationEnabled', created['params'])
            subscribed = self.frame_with_method(directory, 'session/subscribe')
            self.assertEqual(subscribed['params']['deliveryKind'], 'desktop-continuous')
        # The persisted receipt equals the returned one (the worker store reads
        # source.json after the process exits) and verifies through the bridge.
        persisted = read(ref_dir / 'source.json')
        GoAdapter('zcode').verify_source(ref_dir, persisted)
        self.assertEqual({k: v for k, v in result.items() if k != 'files'},
                         {k: v for k, v in persisted.items() if k != 'files'})

    def test_default_new_session_runs_the_hosts_own_mode(self):
        # Registered divergence (the module docstring): without a
        # permission_mode the Go adapter switches nothing - the session runs
        # the host's own mode, recorded from the snapshot the host reported -
        # while the frozen reference still declares yolo on every new
        # non-read-only session. Both complete the same turn; the mode
        # evidence differs by design.
        root, workspace, ref_dir, go_dir = self.scenario(knobs={}, read_only=False)
        result, error = self.run_reference(ref_dir)
        self.assertIsNone(error, msg=repr(error))
        self.assertEqual(read(ref_dir / 'tool-policy.json'),
                         {'read_only': False, 'permission_mode': 'yolo'})
        ref_methods = [frame.get('method') for frame in self.frames(ref_dir)]
        self.assertEqual(ref_methods.count('session/setMode'), 1)
        self.assertEqual(self.frame_with_method(ref_dir, 'session/setMode')['params']['mode'],
                         'yolo')
        completed = self.run_go(go_dir)
        self.assertEqual(completed.returncode, 0, completed.stderr.decode('utf-8', 'replace'))
        go_result = json.loads(completed.stdout)
        self.assertEqual((go_result['identity'], go_result['native_turn'],
                          go_result['usage_evidence']),
                         (result['identity'], result['native_turn'], result['usage_evidence']))
        self.assertEqual(read(go_dir / 'answer.json'), ANSWER)
        # The default path sends no setMode frame at all and records whatever
        # the host itself reported (the fake host's snapshot says 'default').
        go_methods = [frame.get('method') for frame in self.frames(go_dir)]
        self.assertNotIn('session/setMode', go_methods)
        self.assertEqual(read(go_dir / 'tool-policy.json'),
                         {'read_only': False, 'permission_mode': 'default'})
        self.assertEqual(read(go_dir / 'native-source.json')['permission_mode'], 'default')

    def test_read_only_role_narrows_the_native_toolbox(self):
        root, workspace, ref_dir, go_dir = self.scenario(knobs={}, read_only=True)
        self.assert_success(ref_dir, go_dir)
        self.assertEqual(read(ref_dir / 'tool-policy.json'),
                         {'read_only': True,
                          'allowed': ['Read', 'Glob', 'Grep', 'WebSearch', 'WebFetch']})
        for directory in (ref_dir, go_dir):
            methods = [frame.get('method') for frame in self.frames(directory)]
            self.assertNotIn('session/setMode', methods)
            created = self.frame_with_method(directory, 'session/create')
            self.assertEqual(created['params']['toolAllowlist'],
                             ['Read', 'Glob', 'Grep', 'WebSearch', 'WebFetch'])
            self.assertNotIn('permission_mode', read(directory / 'native-source.json'))

    # --- resume -------------------------------------------------------------
    def test_resume_session_success_keeps_its_own_mode(self):
        knobs = {'session_id': 's-resumed'}
        root, workspace, ref_dir, go_dir = self.scenario(knobs=knobs, identity='s-resumed',
                                                         read_only=False)
        self.assert_success(ref_dir, go_dir)
        # A resumed session is never re-declared: the snapshot's own mode is
        # the mode recorded as in force.
        self.assertEqual(read(ref_dir / 'tool-policy.json'),
                         {'read_only': False, 'permission_mode': 'default'})
        for directory in (ref_dir, go_dir):
            methods = [frame.get('method') for frame in self.frames(directory)]
            self.assertNotIn('session/setMode', methods)
            self.assertNotIn('session/create', methods)
            resumed = self.frame_with_method(directory, 'session/resume')
            self.assertEqual(resumed['params']['sessionId'], 's-resumed')

    def test_resume_session_unavailable_is_the_five_key_record(self):
        knobs = {'resume_error': {'code': -32004, 'message': 'Session not found: s-1'}}
        root, workspace, ref_dir, go_dir = self.scenario(knobs=knobs, identity='s-1')
        error = self.assert_failure(ref_dir, go_dir,
                                    'ZCODE_SESSION_UNAVAILABLE: Session not found: s-1',
                                    SessionUnavailable)
        self.assertEqual(error.identity, 's-1')
        self.assertEqual(error.method, 'session/resume')
        self.assertEqual(error.code, -32004)
        for directory in (ref_dir, go_dir):
            record = read(directory / 'native-error.json')
            # The five-key record, value for value.
            self.assertEqual(set(record),
                             {'error', 'error_kind', 'identity', 'method', 'native_code'})
            self.assertEqual((record['error_kind'], record['identity'], record['method'],
                              record['native_code']),
                             ('SESSION_UNAVAILABLE', 's-1', 'session/resume', -32004))
            self.assertFalse((directory / 'identity.json').exists())

    # --- setMode 边界 -------------------------------------------------------
    def test_setmode_readback_mismatch_is_refused(self):
        # An explicitly requested permission_mode whose host readback differs
        # is refused on both sides: the Go adapter verifies the readback
        # against the requested mode, the frozen reference against its own
        # yolo declaration - the same knob defeats both. (Without an explicit
        # mode the Go side would not send setMode at all; that registered
        # divergence is the default-mode scenario above.)
        root, workspace, ref_dir, go_dir = self.scenario(knobs={'setMode_mode': 'build'},
                                                         read_only=False,
                                                         permission_mode='yolo')
        self.assert_failure(ref_dir, go_dir, 'ZCODE_CONFIG: permission mode was not applied')
        for directory in (ref_dir, go_dir):
            # The one setMode frame carried the requested mode; the refusal
            # lands before any turn binding or policy evidence.
            self.assertEqual(self.frame_with_method(directory, 'session/setMode')['params']['mode'],
                             'yolo')
            self.assertFalse((directory / 'native-source.json').exists())
            self.assertFalse((directory / 'tool-policy.json').exists())

    # --- 选型拒绝分支 --------------------------------------------------------
    def test_unadvertised_selection_is_refused_before_any_change(self):
        wanted = json.dumps({'providerId': 'p2', 'modelId': 'm2'})
        root, workspace, ref_dir, go_dir = self.scenario(model=wanted)
        self.assert_failure(ref_dir, go_dir,
                            'ZCODE_CONFIG: requested exact provider/model is not advertised; no fallback')
        for directory in (ref_dir, go_dir):
            methods = [frame.get('method') for frame in self.frames(directory)]
            self.assertNotIn('session/setModel', methods)

    def test_a_selection_the_host_does_not_apply_is_never_recorded(self):
        knobs = {'settings': SETTINGS_WITH_M9,
                 'setModel_settings': {'model': SETTINGS['model'],
                                       'thoughtLevel': {'current': 'low'}}}
        root, workspace, ref_dir, go_dir = self.scenario(
            knobs=knobs, model=json.dumps({'providerId': 'p1', 'modelId': 'm9'}))
        self.assert_failure(ref_dir, go_dir, 'ZCODE_CONFIG: requested selection was not applied')
        for directory in (ref_dir, go_dir):
            self.frame_with_method(directory, 'session/setModel')

    def test_set_model_applies_the_requested_reference(self):
        # The normal setModel path under the probed 2026-09-19 contract: the
        # frame carries the exact reference with the advertised default level,
        # and the host's readback confirms the options and moves the thought
        # level with them, so setThoughtLevel is not asked again.
        root, workspace, ref_dir, go_dir = self.scenario(
            knobs={'settings': SETTINGS_APPLY},
            model=json.dumps({'providerId': 'p1', 'modelId': 'm1'}))
        result = self.assert_success(ref_dir, go_dir)
        self.assertEqual(result['selection'],
                         {'model': CANONICAL_REF, 'reasoning_effort': 'low'})
        for directory in (ref_dir, go_dir):
            frame = self.frame_with_method(directory, 'session/setModel')
            self.assertEqual(frame['params']['model'],
                             {'providerId': 'p1', 'modelId': 'm1',
                              'options': {'reasoningLevel': 'low'}})
            self.assertIs(frame['params']['persistAsWorkspaceLastUsed'], False)
            methods = [item.get('method') for item in self.frames(directory)]
            self.assertNotIn('session/setThoughtLevel', methods)

    def test_go_side_nothing_advertised_is_refused_before_set_model(self):
        # Go side only, registered in the module docstring: a snapshot that
        # advertises no models at all is an empty provider registry on the
        # 2026-09-19 host, where setModel cannot apply anything (the state the
        # failing G2c run jobs were in). The Go adapter refuses with its own
        # text before firing the frame; the frozen reference still sends it.
        empty = {'model': {'current': {'providerId': 'p1', 'modelId': 'm0'}, 'available': []},
                 'thoughtLevel': {'current': 'low'}, 'mode': {'current': 'default'}}
        _root, _workspace, _ref_dir, go_dir = self.scenario(
            knobs={'settings': empty}, model=json.dumps({'providerId': 'p1', 'modelId': 'm1'}))
        completed = self.run_go(go_dir)
        self.assertEqual(completed.returncode, 1)
        record = read(go_dir / 'native-error.json')
        self.assertEqual(record['error'],
                         'RuntimeBlocked: ZCODE_CONFIG: requested exact provider/model '
                         'is not advertised; no fallback')
        methods = [item.get('method') for item in self.frames(go_dir)]
        self.assertNotIn('session/setModel', methods)
        self.assertFalse((go_dir / 'source.json').exists())

    def test_missing_native_selection_with_none_bound_is_refused(self):
        empty = {'model': {'current': None, 'available': []}, 'thoughtLevel': {'current': ''}}
        root, workspace, ref_dir, go_dir = self.scenario(knobs={'settings': empty})
        self.assert_failure(ref_dir, go_dir, 'ZCODE_CONFIG: native selected model missing')
        for directory in (ref_dir, go_dir):
            methods = [frame.get('method') for frame in self.frames(directory)]
            self.assertNotIn('session/setModel', methods)

    def test_malformed_bound_selection_is_refused_before_launch(self):
        root, workspace, ref_dir, go_dir = self.scenario(model='M')
        self.assert_failure(ref_dir, go_dir,
                            'ZCODE_CONFIG: a bound selection must name a provider and a model')
        for directory in (ref_dir, go_dir):
            # Refused before the host process was ever started.
            self.assertFalse((directory / 'host-frames.jsonl').exists())

    def test_reasoning_setting_not_applied_is_refused(self):
        # On the 2026-09-19 host setModel moves the thought level with the
        # options it accepts, so this scenario defeats both readbacks: the
        # setModel readback reports the level unapplied (the override), which
        # is what sends the client to setThoughtLevel, whose own readback
        # refuses it in turn.
        knobs = {'setModel_settings': {'model': SETTINGS['model'],
                                       'thoughtLevel': {'current': 'low'}},
                 'setThoughtLevel_settings': {'model': SETTINGS['model'],
                                              'thoughtLevel': {'current': 'low'}}}
        root, workspace, ref_dir, go_dir = self.scenario(
            knobs=knobs, model=json.dumps({'providerId': 'p1', 'modelId': 'm1'}),
            reasoning_effort='max')
        self.assert_failure(ref_dir, go_dir, 'ZCODE_CONFIG: reasoning setting was not applied')
        for directory in (ref_dir, go_dir):
            self.frame_with_method(directory, 'session/setThoughtLevel')

    def test_protocol_mismatch_embeds_the_protocol_by_value(self):
        protocol = {'name': 'Other Protocol', 'version': 1, 'zz': 1e2, 'aa': 'ü会话'}
        root, workspace, ref_dir, go_dir = self.scenario(knobs={'protocol': protocol})
        expected_ref = ('ZCODE_PROTOCOL: the app-server does not speak ZCode Protocol v1: '
                        + json.dumps(protocol, ensure_ascii=False))
        expected_go = ('ZCODE_PROTOCOL: the app-server does not speak ZCode Protocol v1: '
                       + json.dumps(protocol, ensure_ascii=False, sort_keys=True,
                                    separators=(',', ':')))
        error = self.assert_failure(ref_dir, go_dir, expected_ref, expected_go=expected_go)
        prefix = 'ZCODE_PROTOCOL: the app-server does not speak ZCode Protocol v1: '
        self.assertEqual(json.loads(str(error)[len(prefix):]), protocol)
        go_text = read(go_dir / 'native-error.json')['error']
        self.assertEqual(json.loads(go_text[len('RuntimeBlocked: ' + prefix):]), protocol)

    # --- 探针 ---------------------------------------------------------------
    def test_probe_sends_no_prompt(self):
        knobs = {'early_reverse_requests': [
            {'id': 'auth-1', 'method': 'interaction/requestOfficialMcpAuthHeaders', 'params': {}}]}
        root, workspace, ref_dir, go_dir = self.scenario(knobs=knobs, probe=True, read_only=True)
        result, error = self.run_reference(ref_dir, probe=True)
        self.assertIsNone(error, msg=repr(error))
        completed = self.run_go(go_dir)
        self.assertEqual(completed.returncode, 0)
        self.compare_json_values(ref_dir, go_dir, JSON_FILES)
        self.compare_bytes(ref_dir, go_dir, RAW_FILES)
        self.assert_prompt_equivalent(ref_dir, go_dir)
        self.compare_values(ref_dir, go_dir, VALUE_FILES)
        self.assert_receipts_equivalent(result, json.loads(completed.stdout), ref_dir, go_dir)
        self.assertEqual(result['provider'], 'zcode')
        self.assertEqual(result['model_calls'], 0)
        self.assertEqual(result['native_tools'], 'not_verified')
        self.assertEqual(result['mcp_auth'], 'unavailable')
        self.assertEqual(result['protocol'], {'name': 'ZCode Protocol', 'version': 1})
        for directory in (ref_dir, go_dir):
            methods = [frame.get('method') for frame in self.frames(directory)]
            self.assertNotIn('session/send', methods)
            created = self.frame_with_method(directory, 'session/create')
            self.assertIs(created['params']['titleGenerationEnabled'], False)
            # The reverse request was answered with the unavailable desktop
            # port and recorded as the interaction it is.
            replies = self.jsonl_values(directory, 'host-replies.jsonl')
            self.assertEqual(replies, [{'id': 'auth-1', 'result': {'ok': False,
                                                                   'reason': 'official_auth_unavailable'}}])
            self.assertEqual(read(directory / 'interaction-status.json'),
                             [{'method': 'interaction/requestOfficialMcpAuthHeaders',
                               'status': 'official_auth_unavailable'}])

    # --- 账号供给 ------------------------------------------------------------
    def test_account_delivery_accepted_is_recorded_by_value(self):
        root = Path(tempfile.mkdtemp(prefix='g2c-acc-')).resolve()
        self.addCleanup(shutil.rmtree, root, ignore_errors=True)
        data, catalogue, v2 = self.account_fixture(root)
        environment = {'ZCODE_BUILTIN_PROVIDER_CONFIG_FILE': str(catalogue),
                       'ZCODE_PERSONAL_PROVIDER_CONFIG_FILE': str(v2 / 'provider_config.json')}
        _root, _workspace, ref_dir, go_dir = self.scenario(
            knobs={}, read_only=False, model=CANONICAL_REF, environment=environment,
            env={'ZCODE_DATA_BASE_DIR': str(data)}, root=root, permission_mode='yolo')
        self.assert_success(ref_dir, go_dir)
        revision = 'zcode-builtin:7:' + hashlib.sha256(str(catalogue).encode()).hexdigest()
        self.assertEqual(read(ref_dir / 'account-provider.json'),
                         {'catalogue': str(catalogue), 'based_on': revision,
                          'providers': ['p1'], 'current': 'p1', 'current_source': 'requested',
                          'status': 'received', 'provider_count': 1,
                          'source': 'host credential index'})
        for directory in (ref_dir, go_dir):
            # The overlay is delivered before the first snapshot and carries
            # the revision the agent will answer with.
            methods = [frame.get('method') for frame in self.frames(directory)]
            self.assertLess(methods.index('provider/updateAccountConfig'),
                            methods.index('session/create'))
            delivered = self.frame_with_method(directory, 'provider/updateAccountConfig')
            self.assertEqual(delivered['params']['basedOnZCodeBuiltinRevision'], revision)

    def test_account_delivery_refused_is_reported_with_the_body_by_value(self):
        root = Path(tempfile.mkdtemp(prefix='g2c-accr-')).resolve()
        self.addCleanup(shutil.rmtree, root, ignore_errors=True)
        data, catalogue, v2 = self.account_fixture(root)
        environment = {'ZCODE_BUILTIN_PROVIDER_CONFIG_FILE': str(catalogue),
                       'ZCODE_PERSONAL_PROVIDER_CONFIG_FILE': str(v2 / 'provider_config.json')}
        _root, _workspace, ref_dir, go_dir = self.scenario(
            knobs={'account_reply': {'status': 'rejected', 'providerCount': 1}},
            environment=environment, env={'ZCODE_DATA_BASE_DIR': str(data)}, root=root)
        body = {'status': 'rejected', 'providerCount': 1}
        expected_ref = ('ZCODE_ACCOUNT_CONFIG: the host did not accept the account provider '
                        'configuration: ' + json.dumps(body, ensure_ascii=False, sort_keys=True))
        expected_go = ('ZCODE_ACCOUNT_CONFIG: the host did not accept the account provider '
                       'configuration: ' + json.dumps(body, ensure_ascii=False, sort_keys=True,
                                                      separators=(',', ':')))
        self.assert_failure(ref_dir, go_dir, expected_ref, expected_go=expected_go)
        prefix = 'ZCODE_ACCOUNT_CONFIG: the host did not accept the account provider configuration: '
        go_text = read(go_dir / 'native-error.json')['error']
        self.assertEqual(json.loads(go_text[len('RuntimeBlocked: ' + prefix):]), body)
        for directory in (ref_dir, go_dir):
            self.assertFalse((directory / 'account-provider.json').exists())

    def test_a_host_that_resolves_its_own_catalogue_is_never_given_one(self):
        root, workspace, ref_dir, go_dir = self.scenario(knobs={})
        self.assert_success(ref_dir, go_dir)
        for directory in (ref_dir, go_dir):
            methods = [frame.get('method') for frame in self.frames(directory)]
            self.assertNotIn('provider/updateAccountConfig', methods)
            self.assertFalse((directory / 'account-provider.json').exists())

    # --- 反向请求 ------------------------------------------------------------
    def test_runtime_headers_reverse_request_without_a_credential(self):
        knobs = {'reverse_requests': [
            {'id': 'hdr-1', 'method': RUNTIME_HEADERS, 'params': {'providerId': 'p-absent'}}]}
        root, workspace, ref_dir, go_dir = self.scenario(knobs=knobs, read_only=False,
                                                         permission_mode='yolo')
        # No credential is provisioned for p-absent, so both sides answer the
        # one machine-answered interaction the same honest way. (The
        # credential-applied half needs the production helper next to the
        # production package, which the relocated reference cannot execute; it
        # is pinned by internal/harness/native_zcode_unix_test.go and by the
        # Go-side-only scenario below. The explicit permission_mode keeps the
        # mode faces comparable; the default-mode divergence has its own
        # scenario above.)
        self.assert_success(ref_dir, go_dir)
        for directory in (ref_dir, go_dir):
            replies = self.jsonl_values(directory, 'host-replies.jsonl')
            self.assertEqual(replies, [{'id': 'hdr-1',
                                        'result': {'headersApplied': False,
                                                   'errorMessage': 'no provisioned request credential for p-absent'}}])
            self.assertEqual(read(directory / 'interaction-status.json'),
                             [{'method': RUNTIME_HEADERS,
                               'status': 'provider-runtime-headers-unavailable'}])

    def test_permission_request_is_never_answered(self):
        # G2d registered divergence: the frozen reference still refuses (its
        # era predates the held-interaction contract); the Go binary holds the
        # request - the reverse-read LBi shape, a requestId plus options that
        # carry their own response objects - never answers it, and the same
        # turn still completes.
        knobs = {'reverse_requests': [
            {'id': 'perm-1', 'method': 'interaction/requestPermission',
             'params': {'requestId': 'req-1', 'sessionId': 's-1', 'turnId': 't-1',
                        'toolCallId': 'tc-1', 'toolName': 'Bash',
                        'reason': 'run a tool', 'riskLevel': 'medium',
                        'input': {'command': 'ls'},
                        'options': [{'optionId': 'allow_once', 'kind': 'allow_once',
                                     'name': 'Allow once',
                                     'response': {'decision': 'allow',
                                                  'reason': 'Approved once'}}]}}]}
        root, workspace, ref_dir, go_dir = self.scenario(knobs=knobs)
        result, error = self.run_reference(ref_dir)
        self.assertIsNone(result)
        self.assertEqual(
            str(error),
            'ZCODE_INTERACTION_REQUIRED: interaction/requestPermission needs a native '
            'interactive client; no user decision was sent')
        completed = self.run_go(go_dir)
        self.assertEqual(completed.returncode, 0, completed.stderr.decode('utf-8', 'replace'))
        for directory in (ref_dir, go_dir):
            # The fake host confirms it received no reply frame at all.
            replies = directory / 'host-replies.jsonl'
            self.assertEqual(replies.read_text() if replies.exists() else '',
                             '', 'a user decision was sent')
        # The held request outlived the turn unanswered: invalidated once the
        # transport closed, so its old option ids are no longer authorized.
        pendings = read(go_dir / 'interaction-pending.json')
        self.assertEqual([record['request_id'] for record in pendings], ['req-1'])
        self.assertEqual(pendings[0]['status'], 'invalidated')
        self.assertEqual(pendings[0]['summary'], 'Bash: run a tool')

    def test_go_side_applied_credential_reaches_only_the_protocol_reply(self):
        # Go side only: the applied-credential reply shape (the reference
        # cannot execute the production helper from its tests location). The
        # secret reaches the host - the party that asked for it - and no
        # evidence file.
        if not shutil.which('node'):
            self.skipTest('node is not available')
        root = Path(tempfile.mkdtemp(prefix='g2c-hdr-')).resolve()
        self.addCleanup(shutil.rmtree, root, ignore_errors=True)
        data, _catalogue, v2 = self.account_fixture(root)
        secret = 'TEST-PLAINTEXT-KEY'
        (v2 / 'credentials.json').write_text(json.dumps(
            {'account-provider:coding-plan:p1:account:u1:api-key': secret}), encoding='utf-8')
        knobs = {'reverse_requests': [{'id': 'hdr-1', 'method': RUNTIME_HEADERS,
                                       'params': {'providerId': 'p1'}}]}
        _root, _workspace, _ref_dir, go_dir = self.scenario(
            knobs=knobs, env={'ZCODE_DATA_BASE_DIR': str(data)}, root=root)
        completed = self.run_go(go_dir)
        self.assertEqual(completed.returncode, 0, completed.stderr.decode('utf-8', 'replace'))
        replies = self.jsonl_values(go_dir, 'host-replies.jsonl')
        self.assertEqual(replies, [{'id': 'hdr-1', 'result': {'headersApplied': True,
                                                             'requestAuth': {'apiKey': secret}}}])
        self.assertEqual(read(go_dir / 'interaction-status.json'),
                         [{'method': RUNTIME_HEADERS,
                           'status': 'provider-runtime-headers-applied'}])
        for name in ('protocol.jsonl', 'native-events.jsonl', 'identity.json', 'tool-policy.json',
                     'native-source.json', 'source.json', 'answer.json', 'answer.raw.txt',
                     'native-request.json'):
            self.assertNotIn(secret, (go_dir / name).read_text(encoding='utf-8'),
                             f'the credential leaked into {name}')

    # --- 传输边界与超时 ------------------------------------------------------
    def test_transport_non_json_output(self):
        root, workspace, ref_dir, go_dir = self.scenario(knobs={'fail_mode': 'nonjson'})
        self.assert_failure(ref_dir, go_dir, 'ZCODE_TRANSPORT: Invalid native protocol output')

    def test_reference_timeout_text(self):
        # Reference side only, the G2b precedent: the short budget is injected
        # through the patchable per-request default (the reference's 900-second
        # turn budget is a hardcoded literal without such an attribute). The Go
        # side's turn-wait ends are pinned by internal/harness/
        # native_zcode_unix_test.go (TestZcodeExecuteBoundedWait: the receive
        # timeout text and ZCODE_TIMEOUT at the deadline) and its per-request
        # timeout text by internal/harness/native_transport_unix_test.go.
        root, workspace, ref_dir, _go_dir = self.scenario(knobs={'fail_mode': 'silent'})
        _result, error = self.run_reference(ref_dir, timeout=0.2)
        self.assertIsInstance(error, RuntimeBlocked)
        self.assertEqual(str(error), 'ZCODE_NATIVE_TIMEOUT: session/create')


class ZCodeReconcileDifferential(unittest.TestCase):
    """Reconcile both ways: the reference function against the Go bridge operation."""

    @classmethod
    def setUpClass(cls):
        ensure_driver_binary()

    RESULT_PREFIX = 'RuntimeBlocked: NATIVE_RESULT: multiple candidate objects'
    RAW = 'Already implemented.\n\n```json\n' + json.dumps(ANSWER) + '\n```'

    def evidence(self, root, original_error):
        directories = []
        for side in ('ref', 'go'):
            directory = (root / f'{side}-side' / 'job').resolve()
            directory.mkdir(parents=True, exist_ok=True)
            atomic(directory / 'native-error.json', {'error': original_error})
            atomic(directory / 'native-source.json',
                   {'sessionId': 's-1', 'inputId': 'in-1',
                    'selection': {'model': '{"modelId":"m1","providerId":"p1"}',
                                  'reasoning_effort': 'low'}})
            events = [
                {'sessionId': 's-1', 'seq': 1, 'type': 'turn.started', 'turnId': 't-1',
                 'payload': {'inputId': 'in-1'}},
                {'sessionId': 's-1', 'seq': 2, 'type': 'turn.completed', 'turnId': 't-1',
                 'payload': {'inputId': 'in-1', 'resultType': 'success', 'response': self.RAW,
                             'usage': {'inputTokens': 4}}},
            ]
            (directory / 'native-events.jsonl').write_text(
                ''.join(json.dumps(event) + '\n' for event in events), encoding='utf-8')
            # The message text must equal the saved answer bytes exactly, or
            # the recovery's write_answer would refuse its own original output.
            (directory / 'answer.raw.txt').write_text(self.RAW, encoding='utf-8')
            directories.append(directory)
        return directories

    def reconcile_both(self, root, original_error):
        ref_dir, go_dir = self.evidence(root, original_error)
        process = {'exit_code': 1, 'error': ''}
        try:
            reference = reference_native_zcode.reconcile_completed_output(ref_dir, process)
            reference_error = None
        except (RuntimeBlocked, HarnessError) as exc:
            reference, reference_error = None, exc
        adapter = GoAdapter('zcode')
        try:
            go_value = adapter.reconcile(go_dir, process)
            go_error = None
        except (RuntimeBlocked, HarnessError) as exc:
            go_value, go_error = None, exc
        return (ref_dir, go_dir), (reference, reference_error), (go_value, go_error)

    def assert_same_outcome(self, root, original_error, expected_error=None):
        directories, (reference, reference_error), (go_value, go_error) = \
            self.reconcile_both(root, original_error)
        ref_dir, go_dir = directories
        self.assertIs((reference_error is None), (go_error is None),
                      f'ref={reference_error!r} go={go_error!r}')
        if expected_error is None:
            self.assertIsNone(reference_error)
            self.assertIsNone(go_error)
            self.assertEqual(reference, go_value)
            self.assertEqual((ref_dir / 'source.json').read_bytes(),
                             (go_dir / 'source.json').read_bytes())
            self.assertEqual(read(ref_dir / 'answer.json'), ANSWER)
            self.assertEqual(read(ref_dir / 'source.json')['reconciliation'],
                             {'kind': 'completed-output-format', 'original_exit_code': 1,
                              'original_error': original_error, 'new_model_calls': 0})
        else:
            self.assertEqual(str(reference_error), expected_error)
            self.assertIs(go_error.__class__, reference_error.__class__)
            self.assertEqual(str(go_error), expected_error)
            self.assertFalse((ref_dir / 'source.json').exists())
            self.assertFalse((go_dir / 'source.json').exists())

    def test_native_result_prefix_is_recovered(self):
        with tempfile.TemporaryDirectory(prefix='g2c-rec-') as name:
            self.assert_same_outcome(Path(name).resolve(), self.RESULT_PREFIX)

    def test_transport_failure_is_never_laundered(self):
        with tempfile.TemporaryDirectory(prefix='g2c-rec-') as name:
            self.assert_same_outcome(
                Path(name).resolve(),
                'RuntimeBlocked: ZCODE_TRANSPORT: Invalid native protocol output',
                expected_error='ZCODE_RECONCILE: not a completed formatting-only failure')


if __name__ == '__main__':
    unittest.main()
