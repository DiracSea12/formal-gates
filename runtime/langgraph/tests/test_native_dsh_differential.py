"""Differential test for the G2b DSH native execution path.

The pre-migration DSH ACP transport is kept verbatim in `reference_native_dsh.py`
(test-only, like `reference_job.py` for G2a). This test runs the Go adapter
(`formal-gates harness native <dir>`, which dispatches through the Go registry to
internal/harness/native_dsh.go) and the reference implementation over the same
canonical native-request.json bytes, each in its OWN evidence directory, and
asserts the two are observably equal.

Comparison faces (the contract v2 value-level equivalence):

- by value, parsed and compared as JSON after normalising each side's own root
  path (and the plugin path inside profile.json): every atomically written
  evidence file (identity.json, source.json, answer.json,
  interaction-status.json, profile.json, native-request.json) and the schema
  JSON embedded in the prompt the host received (host-prompt.txt). The Go side
  renders canonical compact bytes where the reference renders default-separator
  bytes; the JSON values are equal and consumers parse by value, which is
  exactly the registered difference.
- byte for byte: the native answer text (answer.raw.txt), which is the model's
  own text on both sides.
- the error records: the same exception-family prefix and the same message,
  with one registered difference - a JSON body embedded in an error text (the
  DSH_NATIVE request refusal, a turn-end reason) is Python-shaped on the
  reference side and canonical on the Go side, so those two cases compare the
  embedded body by value and expect each side's own rendering.
- by value, parsed and compared as JSON: the outbound protocol lines the host
  echoed (host-frames.jsonl - including the session/new and session/prompt
  parameter values), the routing-only trace (protocol.jsonl), the host's record
  of frames it answered (host-replies.jsonl), and the Go command's stdout
  result line.

The two sides are deliberately asymmetric: the reference is called in-process
(reference_native_dsh.execute), while the Go side is its own process, because one
native job in one process is the Go implementation's whole shape. When the
reference raises, the test lands native-error.json with exactly the five lines
native_entry.py used, so error records compare byte for byte too.

The host is a fake ACP peer (a temporary Python script, FAKE_DSH_HOST below,
after the FAKE_APP_SERVER precedent): it mirrors what the real DSH plus the
unchanged native_dsh.mjs plugin make observable - profile.json is read from
--patch, native-source.json is bound to identity.json's session, evidence events
stream after the first user message, and every received frame and the prompt
text are echoed for the comparison.

Coverage division for one case: the timeout TEXT ('DSH_NATIVE_TIMEOUT: <method>')
is pinned here only for the reference side (a short monkeypatched
Client.default_timeout; the Go command has no timeout entry point) and for the
Go side by internal/harness/native_transport_unix_test.go
(TestTransportTimeoutText); this test covers Go's transport closure behaviour
through the early-close and non-JSON scenarios instead.
"""
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import reference_native_dsh
from formal_gates_harness.infra import HarnessError, RuntimeBlocked, SessionUnavailable, atomic, file_proof, read
from formal_gates_harness.native_hosts import GoAdapter, NativeAdapter

REPO = Path(__file__).resolve().parents[3]
BINARY = REPO / '.harness' / 'bin' / 'formal-gates'
PLUGIN_PRODUCTION = REPO / 'runtime/langgraph/formal_gates_harness/native_dsh.mjs'
PLUGIN_REFERENCE = Path(reference_native_dsh.__file__).with_suffix('.mjs')

ANSWER = {'status': 'PASS', 'summary': 'observed', 'findings': [], 'check_ids': []}
ANSWER_PARTS = ('```json\n', json.dumps(ANSWER), '\n```')
ANSWER_TEXT = ''.join(ANSWER_PARTS)
SCHEMA = {'properties': {'findings': {'type': 'array'}, 'status': {'type': 'string'}},
          'required': ['status', 'summary'], 'type': 'object'}
PROMPT = 'Review the requirement.'

# The by-value faces: every atomically written JSON document (Go renders them
# canonically, the reference too, and the values must be equal), the outbound
# protocol lines echoed by the host, the routing-only trace, and the host's
# answered-frames record. Parsing is per line for the JSONL faces, which also
# makes the comparison line-ending agnostic for the host's echo on Windows.
JSON_FILES = ('native-request.json', 'profile.json', 'identity.json', 'source.json',
              'answer.json', 'interaction-status.json')
VALUE_FILES = ('protocol.jsonl', 'host-frames.jsonl', 'host-replies.jsonl')
# The byte-for-byte faces: the raw answer text is the model's own text on both
# sides. A failure lands fewer files; the same split applies to what does land.
RAW_FILES = ('answer.raw.txt',)
ERROR_JSON_FILES = ('native-request.json', 'profile.json')
ERROR_VALUE_FILES = VALUE_FILES
# The prompt's fixed head; the schema JSON after it is compared by value.
PROMPT_HEAD = PROMPT + '\nReturn ONLY one JSON object matching this result schema:\n'


def event_record(session_id, event_type, data):
    return {'sessionId': session_id, 'event': {'type': event_type, 'data': data}}


def message_event(session_id, turn=1, step=0, usage=None):
    """An assistant/message with a non-text part between the text parts."""
    content = [{'type': 'text', 'text': ANSWER_PARTS[0]}, {'type': 'tool_use', 'id': 't-1'},
               {'type': 'text', 'text': ANSWER_PARTS[1]}, {'type': 'text', 'text': ANSWER_PARTS[2]}]
    data = {'turn': turn, 'step': step, 'message': {'content': content}}
    if usage is not None:
        data['usage'] = usage
    return event_record(session_id, 'assistant/message', data)


def end_event(session_id, turn=1, kind='completed'):
    return event_record(session_id, 'turn/end', {'turn': turn, 'reason': {'kind': kind}})


def default_events(session_id='sess-root'):
    return [message_event(session_id, usage={'inputTokens': 12, 'outputTokens': 3}),
            event_record(session_id, 'assistant/attempt', {'turn': 1, 'step': 1, 'usage': {'inputTokens': 4}}),
            end_event(session_id)]


FAKE_DSH_HOST = '''#!PYTHON
"""Fake DSH ACP host: mirrors the observable behaviour of the real host plus the
unchanged native_dsh.mjs plugin for the G2b differential. Reads profile.json from
--patch, answers the ACP sequence, writes native-source.json/native-events.jsonl
after the first user message and echoes every received frame and prompt."""
import json
import os
import sys
import time
from pathlib import Path


def main():
    args = sys.argv[1:]
    patch_path = Path(args[args.index('--patch') + 1])
    overlays = json.loads(patch_path.read_text(encoding='utf-8'))
    overlay = next(o for o in overlays if 'insert' in o)
    config = overlay['insert'][0]['config']
    directory = Path(config['directory'])
    knobs_path = directory / 'fake-host-config.json'
    knobs = json.loads(knobs_path.read_text(encoding='utf-8')) if knobs_path.exists() else {}
    fail = knobs.get('fail_mode')
    if fail == 'close':
        # stdout is gone before anything is answered; stdin stays open so the
        # caller observes the output closure rather than a broken pipe.
        os.close(sys.stdout.fileno())
        for _ in sys.stdin:
            pass
        return
    frames = (directory / 'host-frames.jsonl').open('a', encoding='utf-8')
    replies = (directory / 'host-replies.jsonl').open('a', encoding='utf-8')

    def send(value):
        sys.stdout.write(json.dumps(value, ensure_ascii=False) + '\\n')
        sys.stdout.flush()

    def answer(ident, payload):
        send({'jsonrpc': '2.0', 'id': ident, **payload})

    if fail == 'nonjson':
        sys.stdout.write('this line is not json\\n')
        sys.stdout.flush()
        for _ in sys.stdin:
            pass
        return
    if fail == 'silent':
        for line in sys.stdin:
            frames.write(line)
            frames.flush()
        return
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
        if method == 'initialize':
            answer(ident, {'result': {'protocolVersion': 1,
                                      'agentCapabilities': {'sessionCapabilities': {'resume': {}, 'close': {}}}}})
        elif method in ('session/new', 'session/resume'):
            failure = knobs.get('new_error' if method == 'session/new' else 'resume_error')
            if failure:
                answer(ident, {'error': failure})
            else:
                answer(ident, {'result': {'sessionId': knobs.get('session_id', 'sess-root'),
                                          'configOptions': knobs.get('config_options', [])}})
        elif method == 'session/set_config_option':
            answer(ident, {'result': {'configOptions': knobs.get('set_config_options', [])}})
        elif method == 'session/prompt':
            # The mjs plugin records the prompt verbatim for the comparison.
            (directory / 'host-prompt.txt').write_text(params['prompt'][0]['text'], encoding='utf-8')
            for reverse in knobs.get('reverse_requests', []):
                send({'jsonrpc': '2.0', 'id': reverse.get('id', 'r1'),
                      'method': reverse['method'], 'params': reverse.get('params', {})})
            for seq in range(knobs.get('notifications', 0)):
                send({'method': 'session/update', 'params': {'seq': seq}})
                time.sleep(knobs.get('notification_delay', 0.01))
            if knobs.get('delay'):
                time.sleep(knobs['delay'])
            source = knobs.get('native_source')
            if source is None:
                identity = {'identity': knobs.get('session_id', 'sess-root')}
                marker = directory / 'identity.json'
                if marker.exists():
                    identity = json.loads(marker.read_text(encoding='utf-8'))
                source = {'sessionId': identity['identity'], 'inputId': 'in-1',
                          'turn': knobs.get('turn', 1),
                          'transcriptPath': knobs.get('transcript_path',
                                                       '/transcripts/root-session.jsonl')}
            (directory / 'native-source.json').write_text(json.dumps(source), encoding='utf-8')
            with (directory / 'native-events.jsonl').open('a', encoding='utf-8') as events:
                for record in knobs.get('events', default_events(source['sessionId'])):
                    events.write(json.dumps(record, ensure_ascii=False) + '\\n')
            failure = errors.get('session/prompt')
            if failure:
                answer(ident, {'error': failure})
            else:
                answer(ident, {'result': {'stopReason': knobs.get('prompt_stop_reason', 'end_turn')}})
        elif method == 'session/close':
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


def default_events(session_id):
    text = '```json\\n' + json.dumps({'status': 'PASS', 'summary': 'observed',
                                      'findings': [], 'check_ids': []}) + '\\n```'
    return [
        {'sessionId': session_id, 'event': {'type': 'assistant/message', 'data': {
            'turn': 1, 'step': 0, 'usage': {'inputTokens': 12, 'outputTokens': 3},
            'message': {'content': [{'type': 'text', 'text': text}]}}}},
        {'sessionId': session_id, 'event': {'type': 'turn/end', 'data': {
            'turn': 1, 'reason': {'kind': 'completed'}}}},
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


class DSHDifferential(unittest.TestCase):
    """Every scenario runs both sides over the same canonical request bytes."""

    @classmethod
    def setUpClass(cls):
        ensure_driver_binary()

    def scenario(self, knobs=None, identity='', probe=False, model='', reasoning_effort='',
                 request_extra=None, prewrite=None):
        """One shared root: workspace and host script shared, evidence dirs per side."""
        root = Path(tempfile.mkdtemp(prefix='g2b-dsh-')).resolve()
        self.addCleanup(shutil.rmtree, root, ignore_errors=True)
        workspace = root / 'ws'
        workspace.mkdir()
        host = root / 'fake-dsh-host.py'
        host.write_text(FAKE_DSH_HOST.replace('#!PYTHON', '#!' + sys.executable), encoding='utf-8')
        host.chmod(0o755)
        request = {'provider': 'dsh',
                   'binding': {'provider': 'dsh', 'launch': {'executable': str(host)},
                               'files': {str(host): file_proof(host)}},
                   'workspace': str(workspace), 'read_only': True, 'identity': identity,
                   'model': model, 'reasoning_effort': reasoning_effort,
                   'prompt': PROMPT, 'schema': SCHEMA}
        if probe:
            request['probe'] = True
        request.update(request_extra or {})
        sides = {}
        for side in ('ref', 'go'):
            directory = (root / side).resolve()
            directory.mkdir()
            atomic(directory / 'native-request.json', request)
            if knobs is not None:
                # Plain (order-preserving, non-canonical) bytes: the knobs are
                # host input scaffolding, and the embedded-JSON corpus below is
                # key-order sensitive on purpose. Both sides get the same bytes.
                (directory / 'fake-host-config.json').write_text(
                    json.dumps(knobs, ensure_ascii=False), encoding='utf-8')
            for name, data in (prewrite or {}).items():
                (directory / name).write_bytes(data)
            sides[side] = directory
        return root, workspace, request, sides['ref'], sides['go']

    # --- side runners ----------------------------------------------------
    def run_reference(self, directory, probe=False, timeout=None):
        """In-process reference call; native_entry.py's five error lines on failure."""
        request = read(directory / 'native-request.json')
        manager = (patch.object(reference_native_dsh.Client, 'default_timeout', timeout)
                   if timeout is not None else None)
        if manager is not None:
            manager.start()
        try:
            result = reference_native_dsh.execute(request, directory, probe=probe)
            return result, None
        except Exception as exc:  # noqa: BLE001 - mirrored into the error record
            detail = {'error': f'{type(exc).__name__}: {exc}'}
            if isinstance(exc, SessionUnavailable):
                detail.update(error_kind='SESSION_UNAVAILABLE', identity=exc.identity,
                              method=exc.method, native_code=exc.code)
            atomic(directory / 'native-error.json', detail)
            return None, exc
        finally:
            if manager is not None:
                manager.stop()

    def run_go(self, directory):
        env = {**os.environ, 'FORMAL_GATES_HARNESS_ROOT': str(REPO),
               'FORMAL_GATES_HARNESS_BINARY': str(BINARY)}
        return subprocess.run([str(BINARY), 'harness', 'native', str(directory)],
                              capture_output=True, env=env)

    # --- comparison ------------------------------------------------------
    def normalized(self, directory, name):
        data = (directory / name).read_bytes()
        data = data.replace(str(directory).encode(), b'<job>')
        if name == 'profile.json':
            # The reference resolves the plugin next to itself; Go resolves it
            # next to the production package. Only the .mjs basename-bearing
            # plugin path is normalised, nothing else in the file.
            data = data.replace(str(PLUGIN_REFERENCE).encode(), b'<mjs>')
            data = data.replace(str(PLUGIN_PRODUCTION).encode(), b'<mjs>')
        return data

    def jsonl_values(self, directory, name):
        """Parsed values of one JSONL file; splitlines makes this line-ending safe."""
        values = []
        for line in (directory / name).read_text(encoding='utf-8').splitlines():
            if line.strip():
                values.append(self.normalize_prompt_strings(json.loads(line)))
        return values

    def normalize_prompt_strings(self, value):
        """Rewrite the schema JSON embedded in a prompt string to one canonical
        form: the outbound session/prompt frame carries the prompt text, whose
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
                ref_value = json.loads(self.normalized(ref_dir, name))
                go_value = json.loads(self.normalized(go_dir, name))
                self.assertEqual(ref_value, go_value, f'{name}: values differ')

    def assert_prompt_equivalent(self, ref_dir, go_dir):
        """host-prompt.txt: the fixed head byte for byte, the schema by value
        (the reference renders it with Python separators, Go canonically)."""
        exists = (ref_dir / 'host-prompt.txt').exists()
        self.assertEqual(exists, (go_dir / 'host-prompt.txt').exists(),
                         'host-prompt.txt: existence differs')
        for directory in (ref_dir, go_dir):
            if not exists:
                return
            prompt = (directory / 'host-prompt.txt').read_text(encoding='utf-8')
            self.assertTrue(prompt.startswith(PROMPT_HEAD),
                            f'{directory}: prompt head differs: {prompt[:80]!r}')
            self.assertEqual(json.loads(prompt[len(PROMPT_HEAD):]), SCHEMA,
                             f'{directory}: embedded schema differs')

    def compare_values(self, ref_dir, go_dir, names):
        for name in names:
            ref_exists = (ref_dir / name).exists()
            go_exists = (go_dir / name).exists()
            self.assertEqual(ref_exists, go_exists, f'{name}: existence differs')
            if ref_exists:
                self.assertEqual(self.jsonl_values(ref_dir, name), self.jsonl_values(go_dir, name),
                                 f'{name}: values differ')

    def frames(self, directory):
        return self.jsonl_values(directory, 'host-frames.jsonl')

    def frame_with_method(self, directory, method):
        matched = [frame for frame in self.frames(directory) if frame.get('method') == method]
        self.assertEqual(len(matched), 1, f'expected exactly one {method} frame')
        return matched[0]

    def assert_success(self, ref_dir, go_dir):
        result, error = self.run_reference(ref_dir)
        self.assertIsNone(error, msg=repr(error))
        completed = self.run_go(go_dir)
        self.assertEqual(completed.returncode, 0, completed.stderr.decode('utf-8', 'replace'))
        self.compare_json_values(ref_dir, go_dir, JSON_FILES)
        self.compare_bytes(ref_dir, go_dir, RAW_FILES)
        self.assert_prompt_equivalent(ref_dir, go_dir)
        self.compare_values(ref_dir, go_dir, VALUE_FILES)
        # stdout carries the result as one canonical JSON line on the Go side
        # and native_entry's default-separator shape before - a registered
        # bounded difference, so the two results are compared by value.
        self.assertEqual(json.loads(completed.stdout), result)
        return result

    def assert_failure(self, ref_dir, go_dir, expected_ref, expected_type=RuntimeBlocked,
                       expected_go=None):
        """expected_go defaults to expected_ref; the registered Go text
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
        self.compare_json_values(ref_dir, go_dir, ERROR_JSON_FILES)
        self.assert_prompt_equivalent(ref_dir, go_dir)
        self.compare_values(ref_dir, go_dir, ERROR_VALUE_FILES)
        self.assertEqual(read(ref_dir / 'native-error.json')['error'],
                         f'{type(error).__name__}: {expected_ref}')
        record = read(go_dir / 'native-error.json')
        self.assertEqual(record['error'], f'{type(error).__name__}: {expected_go}')
        self.assertEqual(completed.stderr.decode('utf-8').strip(), record['error'])
        return error

    def assert_process_record_types(self, *directories):
        for directory in directories:
            record = read(directory / 'native-process.json')
            self.assertIsInstance(record['pid'], int)
            self.assertTrue(record['pgid'] is None or isinstance(record['pgid'], int))

    # --- 新建成功回合 ----------------------------------------------------------
    def test_new_session_success_turn(self):
        root, _workspace, _request, ref_dir, go_dir = self.scenario(
            knobs={'events': default_events()})
        result = self.assert_success(ref_dir, go_dir)
        self.assertEqual(result['provider'], 'dsh')
        self.assertEqual(result['identity'], 'sess-root')
        self.assertEqual(result['native_turn'], 1)
        self.assertEqual(read(ref_dir / 'answer.json'), ANSWER)
        # usage evidence from assistant/message and assistant/attempt
        self.assertEqual([entry['event'] for entry in result['usage_evidence']],
                         ['assistant/message', 'assistant/attempt'])
        self.assert_process_record_types(ref_dir, go_dir)
        # The echoed session/new frame carries the same parameter values on
        # both sides (the prompt text is compared byte for byte via
        # host-prompt.txt in assert_prompt_equivalent).
        for directory in (ref_dir, go_dir):
            created = self.frame_with_method(directory, 'session/new')
            self.assertEqual(created['params']['mcpServers'], [])
            self.assertNotIn('sessionId', created['params'])

    def test_resume_session_success(self):
        root, workspace, _request, ref_dir, go_dir = self.scenario(
            identity='sess-root', knobs={'events': default_events()})
        self.assert_success(ref_dir, go_dir)
        for directory in (ref_dir, go_dir):
            resume = self.frame_with_method(directory, 'session/resume')
            self.assertEqual(resume['params']['sessionId'], 'sess-root')
            self.assertEqual(resume['params']['cwd'], str(workspace))
            self.assertEqual(resume['params']['mcpServers'], [])
            methods = [frame.get('method') for frame in self.frames(directory)]
            self.assertNotIn('session/new', methods)

    def test_resume_session_unavailable(self):
        root, _workspace, _request, ref_dir, go_dir = self.scenario(
            identity='sess-root',
            knobs={'resume_error': {'code': -32004, 'message': 'Session not found: sess-root'}})
        error = self.assert_failure(
            ref_dir, go_dir,
            'DSH_SESSION_UNAVAILABLE: Session not found: sess-root', SessionUnavailable)
        self.assertEqual(error.identity, 'sess-root')
        self.assertEqual(error.method, 'session/resume')
        self.assertEqual(error.code, -32004)
        record = read(go_dir / 'native-error.json')
        # The five-key record, byte for byte.
        self.assertEqual(set(record), {'error', 'error_kind', 'identity', 'method', 'native_code'})
        self.assertEqual((record['error_kind'], record['identity'], record['method'],
                          record['native_code']),
                         ('SESSION_UNAVAILABLE', 'sess-root', 'session/resume', -32004))
        self.assertFalse((go_dir / 'identity.json').exists())

    # --- 配置继承三向 -------------------------------------------------------
    def test_config_already_configured_is_not_reset(self):
        root, _workspace, _request, ref_dir, go_dir = self.scenario(
            model='test-model', reasoning_effort='high',
            knobs={'config_options': [{'id': 'model', 'currentValue': 'test-model'},
                                      {'id': 'reasoning_effort', 'currentValue': 'high'}],
                   'events': default_events()})
        result = self.assert_success(ref_dir, go_dir)
        self.assertEqual(result['selection'],
                         {'model': 'test-model', 'reasoning_effort': 'high'})
        for directory in (ref_dir, go_dir):
            methods = [frame.get('method') for frame in self.frames(directory)]
            self.assertNotIn('session/set_config_option', methods)

    def test_config_set_option_readback_applied(self):
        root, _workspace, _request, ref_dir, go_dir = self.scenario(
            model='test-model', reasoning_effort='high',
            knobs={'config_options': [{'id': 'model'},  # currentValue absent -> null
                                      {'id': 'reasoning_effort', 'currentValue': 'high'}],
                   'set_config_options': [{'id': 'model', 'currentValue': 'test-model'},
                                          {'id': 'reasoning_effort', 'currentValue': 'high'}],
                   'events': default_events()})
        result = self.assert_success(ref_dir, go_dir)
        self.assertEqual(result['selection'],
                         {'model': 'test-model', 'reasoning_effort': 'high'})
        self.assertEqual(read(ref_dir / 'identity.json')['selection'],
                         {'model': None, 'reasoning_effort': 'high'})
        for directory in (ref_dir, go_dir):
            methods = [frame.get('method') for frame in self.frames(directory)]
            self.assertIn('session/set_config_option', methods)

    def test_config_readback_mismatch_refused(self):
        root, _workspace, _request, ref_dir, go_dir = self.scenario(
            model='test-model',
            knobs={'config_options': [{'id': 'model'}],
                   # the host does not apply the requested value
                   'set_config_options': [{'id': 'model', 'currentValue': 'other-model'}],
                   'events': default_events()})
        self.assert_failure(ref_dir, go_dir, 'DSH_CONFIG: model selection was not applied')

    # --- 回合异常 -----------------------------------------------------------
    def test_stop_reason_not_end_turn(self):
        root, _workspace, _request, ref_dir, go_dir = self.scenario(
            knobs={'prompt_stop_reason': 'max_tokens', 'events': default_events()})
        self.assert_failure(ref_dir, go_dir, 'DSH_STOP: max_tokens')

    def test_turn_end_missing(self):
        events = default_events()[:1]
        root, _workspace, _request, ref_dir, go_dir = self.scenario(knobs={'events': events})
        self.assert_failure(ref_dir, go_dir, 'DSH_RESULT: committed turn/end evidence is missing')

    def test_turn_end_kind_not_completed(self):
        events = [message_event('sess-root'), end_event('sess-root', kind='errored')]
        root, _workspace, _request, ref_dir, go_dir = self.scenario(knobs={'events': events})
        # The reason value is the same; the Go side renders it as canonical
        # JSON where the reference embeds Python's dict repr (registered
        # difference).
        self.assert_failure(ref_dir, go_dir,
                            "DSH_RESULT: turn was not completed: {'kind': 'errored'}",
                            expected_go='DSH_RESULT: turn was not completed: {"kind":"errored"}')

    def test_message_after_turn_end(self):
        events = default_events() + [message_event('sess-root', step=2)]
        root, _workspace, _request, ref_dir, go_dir = self.scenario(knobs={'events': events})
        self.assert_failure(ref_dir, go_dir, 'DSH_RESULT: message after turn ended')

    # --- 证据异常 -----------------------------------------------------------
    def test_native_source_session_mismatch(self):
        root, _workspace, _request, ref_dir, go_dir = self.scenario(
            knobs={'native_source': {'sessionId': 'other-session', 'inputId': 'in-1',
                                     'turn': 1, 'transcriptPath': ''},
                   'events': default_events()})
        self.assert_failure(ref_dir, go_dir, 'DSH_BINDING: native source does not match the session')

    def test_mixed_session_evidence(self):
        events = default_events() + [
            event_record('other-session', 'assistant/message', {'turn': 1, 'step': 9})]
        root, _workspace, _request, ref_dir, go_dir = self.scenario(knobs={'events': events})
        self.assert_failure(ref_dir, go_dir, 'BINDING: mixed session evidence')

    def test_saved_output_differs(self):
        root, _workspace, _request, ref_dir, go_dir = self.scenario(
            knobs={'events': default_events()}, prewrite={'answer.raw.txt': b'stale output\n'})
        self.assert_failure(ref_dir, go_dir, 'DSH_RESULT: saved output differs')

    # --- 交互边界 -----------------------------------------------------------
    def test_permission_request_never_answered(self):
        knobs = {'reverse_requests': [{'id': 'p-1', 'method': 'session/request_permission',
                                       'params': {'title': 'run a tool'}}],
                 'events': default_events()}
        root, _workspace, _request, ref_dir, go_dir = self.scenario(knobs=knobs)
        self.assert_failure(
            ref_dir, go_dir,
            'DSH_INTERACTION_REQUIRED: native permission needs a user decision; none was sent')
        for directory in (ref_dir, go_dir):
            # The fake host confirms it received no reply frame at all.
            replies = directory / 'host-replies.jsonl'
            self.assertEqual(replies.read_text() if replies.exists() else '',
                             '', 'a user decision was sent')

    # --- 传输边界与超时 ------------------------------------------------------
    def test_transport_output_closed_early(self):
        root, _workspace, _request, ref_dir, go_dir = self.scenario(knobs={'fail_mode': 'close'})
        self.assert_failure(ref_dir, go_dir,
                            'DSH_TRANSPORT: native ACP process closed its output')

    def test_transport_non_json_output(self):
        root, _workspace, _request, ref_dir, go_dir = self.scenario(knobs={'fail_mode': 'nonjson'})
        self.assert_failure(ref_dir, go_dir, 'DSH_TRANSPORT: non-JSON ACP output')

    def test_reference_timeout_text(self):
        # Reference side only: the Go command has no short-timeout entry point,
        # so its 'DSH_NATIVE_TIMEOUT: <method>' text is pinned by
        # internal/harness/native_transport_unix_test.go (TestTransportTimeoutText);
        # the closure cases above cover the Go transport boundary end to end.
        root, _workspace, _request, ref_dir, _go_dir = self.scenario(
            knobs={'fail_mode': 'silent'})
        _result, error = self.run_reference(ref_dir, timeout=0.2)
        self.assertIsInstance(error, RuntimeBlocked)
        self.assertEqual(str(error), 'DSH_NATIVE_TIMEOUT: initialize')

    # --- 逐消息等待 ---------------------------------------------------------
    def test_late_response_after_notifications_is_accepted(self):
        knobs = {'reverse_requests': [{'id': 'd-1', 'method': 'session/requestDiagnostics',
                                       'params': {}}],
                 'notifications': 6, 'notification_delay': 0.02, 'delay': 0.3,
                 'events': default_events()}
        root, _workspace, _request, ref_dir, go_dir = self.scenario(knobs=knobs)
        # The prompt response arrives 0.3s late while notifications keep the
        # queue non-empty: bounded=false has no total deadline, and UNBOUNDED
        # never truncates the host's own turn, so both sides still accept it.
        self.assert_success(ref_dir, go_dir)
        # The unsupported reverse request was answered once with the -32601
        # compatibility refusal and recorded as an interaction.
        for directory in (ref_dir, go_dir):
            replies = (directory / 'host-replies.jsonl').read_text().splitlines()
            self.assertEqual(len(replies), 1)
            self.assertEqual(json.loads(replies[0])['error']['code'], -32601)
            self.assertEqual(read(directory / 'interaction-status.json'),
                             [{'method': 'session/requestDiagnostics', 'status': 'unsupported-method'}])

    # --- 探针 ---------------------------------------------------------------
    def test_probe_sends_no_prompt(self):
        root, _workspace, _request, ref_dir, go_dir = self.scenario(
            probe=True, knobs={'config_options': [{'id': 'model', 'currentValue': 'probe-model'}]})
        result, error = self.run_reference(ref_dir, probe=True)
        self.assertIsNone(error, msg=repr(error))
        completed = self.run_go(go_dir)
        self.assertEqual(completed.returncode, 0)
        self.compare_json_values(ref_dir, go_dir, JSON_FILES)
        self.compare_bytes(ref_dir, go_dir, RAW_FILES)
        self.assert_prompt_equivalent(ref_dir, go_dir)
        self.compare_values(ref_dir, go_dir, VALUE_FILES)
        self.assertEqual(json.loads(completed.stdout), result)
        self.assertEqual(result['model_calls'], 0)
        self.assertEqual(result['selection'], {'model': 'probe-model'})
        for directory in (ref_dir, go_dir):
            # No prompt was sent and the probe disables only title generation.
            self.assertFalse((directory / 'host-prompt.txt').exists())
            methods = [frame.get('method') for frame in self.frames(directory)]
            self.assertNotIn('session/prompt', methods)
            overlays = read(directory / 'profile.json')
            self.assertEqual(overlays[2], {'disabled': True, 'id': 'session-title-llm'})

    # --- 错误文本内嵌 JSON ---------------------------------------------------
    def test_embedded_error_json_value(self):
        # Float, Unicode and nesting sensitive. The two sides embed the body
        # with their own rendering (the reference Python's default separators
        # and inbound key order, Go canonical compact): the VALUES must be
        # equal, which is the registered difference.
        error_body = {'code': -32031, 'message': 'Model unavailable 测试',
                      'zz': 1e2, 'aa': {'unicode': 'ü会话', 'nested': {'b': 2, 'a': [1, None, True]}}}
        knobs = {'events': default_events(), 'errors': {'session/prompt': error_body}}
        root, _workspace, _request, ref_dir, go_dir = self.scenario(knobs=knobs)
        expected_ref = 'DSH_NATIVE session/prompt: ' + json.dumps(error_body, ensure_ascii=False)
        expected_go = ('DSH_NATIVE session/prompt: '
                       + json.dumps(error_body, ensure_ascii=False, sort_keys=True,
                                    separators=(',', ':')))
        error = self.assert_failure(ref_dir, go_dir, expected_ref, expected_go=expected_go)
        # The embedded body keeps the full value on both sides: float repr,
        # raw Unicode, nesting.
        prefix = 'DSH_NATIVE session/prompt: '
        self.assertEqual(json.loads(str(error)[len(prefix):]), error_body)
        go_text = read(go_dir / 'native-error.json')['error']
        go_prefix = 'RuntimeBlocked: ' + prefix
        self.assertEqual(json.loads(go_text[len(go_prefix):]), error_body)


class DSHReconcileDifferential(unittest.TestCase):
    """Reconcile three ways, reference module against the Go bridge operation."""

    @classmethod
    def setUpClass(cls):
        ensure_driver_binary()

    LEGACY = 'RuntimeBlocked: DSH_RESULT: final message is not a JSON result object'
    RESULT_PREFIX = 'RuntimeBlocked: NATIVE_RESULT: multiple candidate objects'
    RAW = 'Already implemented.\n\n```json\n' + json.dumps(ANSWER) + '\n```'

    def evidence(self, root, original_error, *, request_model='test-model',
                 configured_model=None, workspace_override=None):
        directories = []
        # The message text must equal the saved answer bytes exactly, or the
        # recovery's write_answer would refuse its own original output.
        events = [event_record('sess-root', 'assistant/message',
                               {'turn': 1, 'step': 0, 'usage': {'inputTokens': 12},
                                'message': {'content': [{'type': 'text', 'text': self.RAW}]}}),
                  end_event('sess-root')]
        for side in ('ref', 'go'):
            directory = (root / side).resolve()
            directory.mkdir(parents=True, exist_ok=True)
            atomic(directory / 'native-error.json', {'error': original_error})
            atomic(directory / 'identity.json',
                   {'identity': 'sess-root',
                    'workspace': str(workspace_override or (root / 'ws')),
                    'selection': {'model': configured_model or request_model,
                                  'reasoning_effort': 'high'}})
            atomic(directory / 'native-request.json',
                   {'provider': 'dsh', 'workspace': str(root / 'ws'),
                    'model': request_model, 'reasoning_effort': 'high'})
            (directory / 'native-source.json').write_text(
                json.dumps({'sessionId': 'sess-root', 'turn': 1, 'transcriptPath': 'retained-native-log'}))
            (directory / 'native-events.jsonl').write_text(
                ''.join(json.dumps(event) + '\n' for event in events))
            (directory / 'answer.raw.txt').write_text(self.RAW)
            directories.append(directory)
        return directories

    def reconcile_both(self, root, original_error, **kwargs):
        ref_dir, go_dir = self.evidence(root, original_error, **kwargs)
        process = {'exit_code': 1, 'error': ''}
        try:
            reference = reference_native_dsh.reconcile_completed_output(ref_dir, process)
            reference_error = None
        except (RuntimeBlocked, HarnessError) as exc:
            reference, reference_error = None, exc
        adapter = GoAdapter('dsh')
        try:
            go_value = adapter.reconcile(go_dir, process)
            go_error = None
        except (RuntimeBlocked, HarnessError) as exc:
            go_value, go_error = None, exc
        return (ref_dir, go_dir), (reference, reference_error), (go_value, go_error)

    def assert_same_outcome(self, root, original_error, expected_error=None, **kwargs):
        directories, (reference, reference_error), (go_value, go_error) = \
            self.reconcile_both(root, original_error, **kwargs)
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

    def test_legacy_error_is_recovered(self):
        with tempfile.TemporaryDirectory(prefix='g2b-rec-') as name:
            self.assert_same_outcome(Path(name).resolve(), self.LEGACY)

    def test_native_result_prefix_is_recovered(self):
        with tempfile.TemporaryDirectory(prefix='g2b-rec-') as name:
            self.assert_same_outcome(Path(name).resolve(), self.RESULT_PREFIX)

    def test_ineligible_failure_is_refused(self):
        with tempfile.TemporaryDirectory(prefix='g2b-rec-') as name:
            self.assert_same_outcome(
                Path(name).resolve(), 'RuntimeBlocked: DSH_TRANSPORT: native ACP process closed its output',
                expected_error='DSH_RECONCILE: not a completed formatting-only failure')

    def test_workspace_change_is_refused(self):
        with tempfile.TemporaryDirectory(prefix='g2b-rec-') as name:
            root = Path(name).resolve()
            self.assert_same_outcome(
                root, self.LEGACY, workspace_override=str(root / 'elsewhere'),
                expected_error='DSH_RECONCILE: workspace changed')

    def test_model_selection_change_is_refused(self):
        with tempfile.TemporaryDirectory(prefix='g2b-rec-') as name:
            self.assert_same_outcome(
                Path(name).resolve(), self.LEGACY, configured_model='other-model',
                expected_error='DSH_RECONCILE: original model selection differs')


class DSHRegistryDifferential(unittest.TestCase):
    """The four bridge lifecycle operations against the retired Python semantics."""

    @classmethod
    def setUpClass(cls):
        ensure_driver_binary()

    def setUp(self):
        # The pre-migration dsh lifecycle semantics are NativeAdapter's methods
        # (validate_binding/verify_source never import the retired module).
        self.reference = NativeAdapter('dsh', '.native_dsh')
        self.adapter = GoAdapter('dsh')

    def temporary_root(self):
        root = Path(tempfile.mkdtemp(prefix='g2b-reg-')).resolve()
        self.addCleanup(shutil.rmtree, root, ignore_errors=True)
        return root

    def test_capture_binds_the_executable_found_on_path(self):
        root = self.temporary_root()
        bindir = root / 'bin'
        bindir.mkdir()
        fake = bindir / 'dsh'
        fake.write_text('#!/bin/sh\nexit 0\n')
        fake.chmod(0o755)
        with patch.dict(os.environ, {'PATH': str(bindir)}):
            reference = reference_native_dsh.capture_binding()
            go_value = self.adapter.capture()
        self.assertEqual(go_value, reference)
        self.assertEqual(go_value['provider'], 'dsh')
        self.assertEqual(go_value['launch']['executable'], str(fake.resolve()))
        self.assertIn(str(fake.resolve()), go_value['files'])

    def test_capture_refused_when_not_on_path(self):
        root = self.temporary_root()
        empty = root / 'empty'
        empty.mkdir()
        with patch.dict(os.environ, {'PATH': str(empty)}):
            with self.assertRaises(RuntimeBlocked) as reference:
                reference_native_dsh.capture_binding()
            with self.assertRaises(RuntimeBlocked) as go:
                self.adapter.capture()
        self.assertEqual(str(go.exception), 'DSH_UNAVAILABLE: dsh is not on PATH')
        self.assertEqual(str(reference.exception), str(go.exception))

    def test_verify_binding_refusals(self):
        root = self.temporary_root()
        bound = root / 'dsh'
        bound.write_text('#!/bin/sh\nexit 0\n')
        bound.chmod(0o755)
        with self.subTest(case='HOST_BINDING'):
            binding = {'provider': 'zcode', 'files': {}}
            with self.assertRaises(HarnessError) as reference:
                self.reference.validate_binding(binding)
            with self.assertRaises(HarnessError) as go:
                self.adapter.validate_binding(binding)
            self.assertEqual(str(go.exception), 'HOST_BINDING: provider differs')
            self.assertEqual(str(reference.exception), str(go.exception))
        with self.subTest(case='HOST_CHANGED'):
            binding = {'provider': 'dsh', 'files': {str(bound): {'sha256': '0' * 64, 'size': 1}}}
            with self.assertRaises(RuntimeBlocked) as reference:
                self.reference.validate_binding(binding)
            with self.assertRaises(RuntimeBlocked) as go:
                self.adapter.validate_binding(binding)
            self.assertEqual(str(go.exception),
                             f'HOST_CHANGED: bound native executable/script changed: {bound}')
            self.assertEqual(str(reference.exception), str(go.exception))
        with self.subTest(case='valid'):
            binding = {'provider': 'dsh',
                       'files': {str(bound): file_proof(bound)}}
            self.reference.validate_binding(binding)
            self.adapter.validate_binding(binding)

    def evidence_directory(self, root):
        directory = root / 'evidence'
        directory.mkdir()
        for name in ('native-source.json', 'native-events.jsonl', 'answer.raw.txt', 'answer.json'):
            (directory / name).write_text('{}')
        return directory

    def test_verify_source_accepts_and_refuses(self):
        root = self.temporary_root()
        directory = self.evidence_directory(root)
        proofs = {name: file_proof(directory / name)
                  for name in ('native-source.json', 'native-events.jsonl',
                               'answer.raw.txt', 'answer.json')}
        with self.subTest(case='valid'):
            source = {'provider': 'dsh', 'identity': 'sess-root', 'files': proofs}
            self.reference.verify_source(directory, source)
            self.assertEqual(self.adapter.verify_source(directory, source), None)
        with self.subTest(case='NATIVE_BINDING'):
            source = {'provider': 'zcode', 'identity': 'sess-root', 'files': proofs}
            with self.assertRaises(RuntimeBlocked) as reference:
                self.reference.verify_source(directory, source)
            with self.assertRaises(RuntimeBlocked) as go:
                self.adapter.verify_source(directory, source)
            self.assertEqual(str(go.exception),
                             'NATIVE_BINDING: missing or different provider/identity')
            self.assertEqual(str(reference.exception), str(go.exception))
        with self.subTest(case='EVIDENCE_MISSING'):
            short = {name: proof for name, proof in proofs.items() if name != 'answer.raw.txt'}
            source = {'provider': 'dsh', 'identity': 'sess-root', 'files': short}
            with self.assertRaises(RuntimeBlocked) as reference:
                self.reference.verify_source(directory, source)
            with self.assertRaises(RuntimeBlocked) as go:
                self.adapter.verify_source(directory, source)
            self.assertEqual(str(go.exception), 'EVIDENCE_MISSING: native source proofs absent')
            self.assertEqual(str(reference.exception), str(go.exception))
        with self.subTest(case='EVIDENCE_CHANGED'):
            changed = dict(proofs)
            changed['answer.raw.txt'] = {'sha256': '0' * 64, 'size': 1}
            source = {'provider': 'dsh', 'identity': 'sess-root', 'files': changed}
            with self.assertRaises(RuntimeBlocked) as reference:
                self.reference.verify_source(directory, source)
            with self.assertRaises(RuntimeBlocked) as go:
                self.adapter.verify_source(directory, source)
            self.assertEqual(str(go.exception), 'EVIDENCE_CHANGED: answer.raw.txt')
            self.assertEqual(str(reference.exception), str(go.exception))


class GoAdapterProbePath(unittest.TestCase):
    """The doctor/probe path: GoAdapter.execute itself (not the raw command)."""

    @classmethod
    def setUpClass(cls):
        ensure_driver_binary()

    def scenario_root(self):
        root = Path(tempfile.mkdtemp(prefix='g2b-adapter-')).resolve()
        self.addCleanup(shutil.rmtree, root, ignore_errors=True)
        workspace = root / 'ws'
        workspace.mkdir()
        host = root / 'fake-dsh-host.py'
        host.write_text(FAKE_DSH_HOST.replace('#!PYTHON', '#!' + sys.executable), encoding='utf-8')
        host.chmod(0o755)
        return root, workspace, host

    def request(self, host, workspace, **extra):
        request = {'provider': 'dsh',
                   'binding': {'provider': 'dsh', 'launch': {'executable': str(host)},
                               'files': {str(host): file_proof(host)}},
                   'workspace': str(workspace), 'read_only': True, 'model': '', 'reasoning_effort': ''}
        request.update(extra)
        return request

    def test_entry_argv_routes_each_host_to_its_implementation(self):
        # dsh is one native job in one Go process; zcode's entry is unchanged.
        self.assertEqual(GoAdapter('dsh').entry_argv('/bind/formal-gates', Path('/jobs/k')),
                         ['/bind/formal-gates', 'harness', 'native', '/jobs/k'])
        self.assertEqual(NativeAdapter('zcode', '.native_zcode').entry_argv('/bind/formal-gates', Path('/jobs/k')),
                         [sys.executable, '-m', 'formal_gates_harness.native_entry', '/jobs/k'])

    def test_execute_probe_returns_the_report_and_writes_the_request(self):
        root, workspace, host = self.scenario_root()
        directory = root / 'probe'
        directory.mkdir()
        (directory / 'fake-host-config.json').write_text(
            json.dumps({'config_options': [{'id': 'model', 'currentValue': 'probe-model'}]}),
            encoding='utf-8')
        request = self.request(host, workspace)
        report = GoAdapter('dsh').execute(request, directory, probe=True)
        self.assertEqual(report['provider'], 'dsh')
        self.assertEqual(report['model_calls'], 0)
        self.assertEqual(report['selection'], {'model': 'probe-model'})
        saved = read(directory / 'native-request.json')
        self.assertIs(saved['probe'], True)
        overlays = read(directory / 'profile.json')
        self.assertEqual(overlays[2], {'disabled': True, 'id': 'session-title-llm'})
        self.assertFalse((directory / 'host-prompt.txt').exists())

    def test_execute_translates_the_session_unavailable_record(self):
        root, workspace, host = self.scenario_root()
        directory = root / 'resume'
        directory.mkdir()
        (directory / 'fake-host-config.json').write_text(
            json.dumps({'resume_error': {'code': -32004, 'message': 'Session not found: sess-root'}}),
            encoding='utf-8')
        request = self.request(host, workspace, identity='sess-root')
        with self.assertRaises(SessionUnavailable) as caught:
            GoAdapter('dsh').execute(request, directory)
        # The in-process implementation raised without the type-name prefix.
        self.assertEqual(str(caught.exception), 'DSH_SESSION_UNAVAILABLE: Session not found: sess-root')
        self.assertEqual((caught.exception.identity, caught.exception.method,
                          caught.exception.code),
                         ('sess-root', 'session/resume', -32004))

    def test_execute_translates_plain_runtime_block(self):
        root, workspace, host = self.scenario_root()
        directory = root / 'stop'
        directory.mkdir()
        (directory / 'fake-host-config.json').write_text(
            json.dumps({'prompt_stop_reason': 'max_tokens', 'events': default_events()}),
            encoding='utf-8')
        request = self.request(host, workspace, prompt=PROMPT, schema=SCHEMA)
        with self.assertRaises(RuntimeBlocked) as caught:
            GoAdapter('dsh').execute(request, directory)
        self.assertEqual(str(caught.exception), 'DSH_STOP: max_tokens')

    def test_execute_without_error_record_raises_the_stderr_text(self):
        # A nonzero exit that landed no native-error.json is an entry-level
        # failure, not a typed execution refusal: the stderr text is raised as
        # a plain HarnessError.
        root, workspace, host = self.scenario_root()
        directory = root / 'no-record'
        directory.mkdir()
        request = self.request(host, workspace)
        fake = subprocess.CompletedProcess(args=[], returncode=1, stdout=b'',
                                           stderr=b'harness native requires a job directory\n')
        real_run = subprocess.run

        def selective_run(argv, *args, **kwargs):
            if len(argv) > 2 and argv[1] == 'harness' and argv[2] == 'native':
                return fake
            return real_run(argv, *args, **kwargs)

        with patch('formal_gates_harness.native_hosts.subprocess.run', side_effect=selective_run):
            with self.assertRaises(HarnessError) as caught:
                GoAdapter('dsh').execute(request, directory, probe=True)
        self.assertIs(type(caught.exception), HarnessError)
        self.assertEqual(str(caught.exception), 'harness native requires a job directory')


if __name__ == '__main__':
    unittest.main()
