"""Reference copy of the retired shared native transport mechanics.

Test-only. Production code must not import this module. The body below is the
pre-migration implementation from formal_gates_harness/native_transport.py,
moved verbatim (G2c: with the zcode execution path, the last in-process
consumer, retired to the Go driver binary, the shared Python transport itself
left the production package). The G2c differential
(test_native_zcode_differential.py) proves the Go adapters
(internal/harness/native_transport.go behind `formal-gates harness native`)
preserve its observable behaviour; reference_native_dsh.py and
reference_native_zcode.py are built on this copy, exactly as their production
originals were built on the production module.

The original module lived inside formal_gates_harness and used package-relative
imports (`from .infra import ...`). To run from this test directory it uses the
absolute imports `formal_gates_harness.infra` and
`formal_gates_harness.native_result`; PYTHONPATH must contain
`<repo>/runtime/langgraph` (the tests set this). Everything else is kept
exactly as it was:

Mechanics every native host transport shares.

A native transport is always the same machine: spawn one host process, speak
line-delimited JSON over its stdio, keep a routing-only trace, answer the host's
own client-directed requests, and write the same four evidence files. Only the
launch arguments and the *protocol sequence* belong to the host.

An adapter supplies its launch arguments and, at most, five protocol hooks, and
inherits everything else:

- ``ident``/``envelope`` how a message is addressed and framed,
- ``respond``    how to answer a host→client request (or raise, or defer),
- ``classify``   which host error means something other than "failed",
- ``notification`` which notifications become evidence.

The defaults cover the plain ``{id, method, params}`` JSON shape, so a host that
speaks it needs no framing code at all. Anything a second host would also need
belongs here, not in an adapter.
"""
from __future__ import annotations
from dataclasses import dataclass
import json
import os
from pathlib import Path
import queue
import subprocess
import threading
import time
from formal_gates_harness.infra import HarnessError, RuntimeBlocked, atomic, read, file_proof
from formal_gates_harness.native_result import decode_result

# The evidence a native run must leave behind, in the order it is finalized.
SOURCE_FILES = ('native-source.json', 'native-events.jsonl', 'answer.raw.txt', 'answer.json')

# The host reported that a client method is not implemented. Hosts define this
# code as the compatibility answer for optional client capabilities.
UNSUPPORTED_METHOD = -32601


class _Unbounded:
    """Marker for "the host decides how long its own turn takes"."""

    def __repr__(self):
        return 'UNBOUNDED'


class Timeout(RuntimeBlocked):
    """No native message arrived in time; the session is not assumed lost."""

    def __init__(self, prefix):
        super().__init__(f'{prefix}_TIMEOUT: no native message; session is not assumed lost')


# Pass as a request timeout when no fixed budget applies. It is not an infinity:
# the host's own turn length is the only limit.
UNBOUNDED = _Unbounded()


def transport_files(*own):
    """The files one adapter's transport behaviour lives in: its own, plus this one.

    A change to the shared mechanics changes every adapter built on it, so a
    transport-adoption review has to list this module alongside the adapter.
    """
    return (Path(__file__).name, *own)


@dataclass(frozen=True)
class Reply:
    """A client-directed request's answer: the payload plus its recorded status."""

    payload: dict
    status: str


class Client:
    """One host process and the JSON request/reply loop over its stdio."""

    # Routing-only trace: never configuration or authentication payloads.
    trace_fields = ('id', 'method')
    # Transport failures are prefixed so evidence names the transport, not the host.
    transport_prefix = ''
    request_prefix = ''
    # How long one request waits, and whether that budget is total or per message.
    default_timeout = 60
    bounded = True
    # The session lifecycle call every host exposes under the same name.
    close_method = 'session/close'
    # Reader key and wording: hosts name their own "output ended" signal.
    closed_key = 'closed'
    closed_message = 'Native output closed'
    invalid_message = 'Invalid native protocol output'

    def __init__(self, argv, workspace, directory, environment=None):
        self.directory = Path(directory)
        self.identity = ''
        self.counter = 0
        self.events = []
        self.interactions = []
        self.messages = queue.Queue()
        self.errors = (self.directory / 'host.stderr').open('ab')
        self.trace = (self.directory / 'protocol.jsonl').open('a', encoding='utf-8')
        self.proc = subprocess.Popen(
            list(argv), cwd=workspace, env={**os.environ, **(environment or {})},
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=self.errors,
            text=True, bufsize=1)
        atomic(self.directory / 'native-process.json',
               {'pid': self.proc.pid, 'pgid': os.getpgid(self.proc.pid) if os.name != 'nt' else None})
        self.reader = threading.Thread(target=self.collect, daemon=True)
        self.reader.start()

    # --- host hooks ------------------------------------------------------
    def ident(self):
        """The id this host expects on a request. The default is a local counter."""
        self.counter += 1
        return 'native-%d' % self.counter

    def envelope(self, value):
        """Protocol constants every outgoing message carries."""
        return value

    def frame(self, ident, method, params):
        return self.envelope({'id': ident, 'method': method, 'params': params})

    def respond(self, method, params):
        """Answer a host→client request, or return None for the default refusal."""
        return None

    def classify(self, method, error, params):
        """Turn a host error into a specific exception, or return None to default."""
        return None

    def notification(self, message):
        """Record a host notification. The default keeps none."""

    def timeout_error(self, method):
        return RuntimeBlocked(f'{self.request_prefix}_TIMEOUT: {method}')

    def blocked(self, text):
        prefix = f'{self.transport_prefix}: ' if self.transport_prefix else ''
        return RuntimeBlocked(prefix + text)

    # --- transport -------------------------------------------------------
    def collect(self):
        try:
            for line in self.proc.stdout:
                try:
                    message = json.loads(line)
                except ValueError:
                    self.messages.put({self.closed_key: self.invalid_message})
                    return
                self.trace.write(json.dumps({k: message[k] for k in self.trace_fields if k in message}) + '\n')
                self.trace.flush()
                self.messages.put(message)
        finally:
            self.messages.put({self.closed_key: self.closed_message})

    def send(self, value):
        self.proc.stdin.write(json.dumps(value, ensure_ascii=False) + '\n')
        self.proc.stdin.flush()

    def closure(self, message):
        """The reason this transport cannot continue, or ''."""
        for key in ('closed', 'transport_error'):
            if key in message:
                return message[key]
        return ''

    def receive(self, wait=60):
        try:
            message = self.messages.get(timeout=wait)
        except queue.Empty as exc:
            raise Timeout(self.request_prefix) from exc
        closed = self.closure(message)
        if closed:
            raise self.blocked(closed)
        if message.get('method') and 'id' in message:
            self.answer(message)
        else:
            self.notification(message)
        return message

    def answer(self, message):
        """Reply to a host request and record what was answered."""
        method = message['method']
        reply = self.respond(method, message.get('params') or {})
        if reply is None:
            reply = Reply({'error': {'code': UNSUPPORTED_METHOD,
                                     'message': 'Client method not supported'}}, 'unsupported-method')
        self.send(self.envelope({'id': message['id'], **reply.payload}))
        self.interactions.append({'method': method, 'status': reply.status})
        atomic(self.directory / 'interaction-status.json', self.interactions)

    def wait(self, timeout, deadline):
        """Seconds to block for the next message; ``None`` waits without limit."""
        if timeout is None or timeout is UNBOUNDED:
            return None
        if deadline is None:
            return timeout
        return max(.1, deadline - time.monotonic())

    def request(self, method, params, timeout=None):
        timeout = self.default_timeout if timeout is None else timeout
        ident = self.ident()
        self.send(self.frame(ident, method, params))
        deadline = None if timeout is UNBOUNDED or not self.bounded else time.monotonic() + timeout
        while True:
            # A bounded request must not outlive its total budget: continuous
            # notifications keep the queue non-empty, so per-message waits alone
            # would never starve and a response arriving past the deadline would
            # still be accepted.
            if deadline is not None and time.monotonic() >= deadline:
                raise self.timeout_error(method)
            try:
                message = self.receive(self.wait(timeout, deadline))
            except Timeout as exc:
                raise self.timeout_error(method) from exc
            if message.get('id') == ident and not message.get('method'):
                if message.get('error'):
                    error = message['error']
                    raise self.classify(method, error, params) or RuntimeBlocked(
                        f'{self.request_prefix} {method}: {json.dumps(error, ensure_ascii=False)}')
                return message.get('result', {})

    def close(self):
        try:
            self.proc.stdin.close()
        except (OSError, BrokenPipeError):
            pass
        try:
            self.proc.wait(timeout=8)
        except subprocess.TimeoutExpired:
            self.proc.terminate()
            try:
                self.proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.proc.kill()
                self.proc.wait()
        self.reader.join(timeout=2)
        self.trace.close()
        self.errors.close()


def capture_binding(provider, launch, environment=None):
    """Bind a provider to the exact binaries it will execute.

    Only executed files are proofed; host-owned configuration a launcher merely
    points at is named in ``environment`` instead, so a normal host refresh is
    not mistaken for the host changing underneath us.
    """
    binding = {'provider': provider, 'launch': dict(launch),
               'files': {value: file_proof(Path(value)) for value in launch.values()}}
    if environment:
        binding['environment'] = dict(environment)
    return binding


def launch_environment(binding, *extra):
    """Merge the bound launch environment with any additional child variables."""
    merged = dict(binding.get('environment') or {})
    for item in extra:
        merged.update(item or {})
    return merged


def session_events(directory, identity):
    """Yield this session's evidence events, rejecting mixed-session evidence."""
    with (directory / 'native-events.jsonl').open() as file:
        for line in file:
            event = json.loads(line)
            if event.get('sessionId') != identity:
                raise RuntimeBlocked('BINDING: mixed session evidence')
            yield event


def write_answer(directory, text, prefix):
    """Keep the native bytes, then decode them into the business result."""
    raw = directory / 'answer.raw.txt'
    if raw.exists():
        if raw.read_text() != text:
            raise RuntimeBlocked(f'{prefix}_RESULT: saved output differs')
    else:
        raw.write_text(text)
    result = decode_result(text)
    atomic(directory / 'answer.json', result)
    return result


def evidence_source(directory, provider, identity, **extra):
    """The receipt every host returns, with proofs for the four evidence files."""
    return {'provider': provider, 'identity': identity, **extra,
            'files': {name: file_proof(directory / name) for name in SOURCE_FILES}}


def reconcile_completed_output(directory, process, prefix, eligible, observe):
    """Recover only a completed native turn that the result parser rejected.

    The original process record is never rewritten, no host is started and no
    model is called; the caller must still accept the recovered business result.
    """
    error_path = directory / 'native-error.json'
    original = read(error_path).get('error', '') if error_path.exists() else ''
    # Only a result-framing failure of an otherwise completed process can be
    # replayed. A runtime or transport failure is never laundered into a result.
    if (process.get('exit_code') != 1 or process.get('error')
            or not error_path.exists() or not eligible(original)):
        raise RuntimeBlocked(f'{prefix}_RECONCILE: not a completed formatting-only failure')
    source = observe()
    source['reconciliation'] = {'kind': 'completed-output-format', 'original_exit_code': 1,
                                'original_error': original, 'new_model_calls': 0}
    atomic(directory / 'source.json', source)
    return source


def close_session(client, identity, timeout=10):
    """Ask the host to close a session; a failed close never hides a real result."""
    if not identity:
        return
    try:
        client.request(client.close_method, {'sessionId': identity}, timeout=timeout)
    except (HarnessError, OSError):
        pass
