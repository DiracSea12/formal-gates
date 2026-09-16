"""Local artifacts and execution receipts; never a second workflow state machine."""
from __future__ import annotations
import contextlib
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import threading
import time
from typing import Any

class HarnessError(RuntimeError):
    pass
class RuntimeBlocked(HarnessError):
    pass
class SessionUnavailable(RuntimeBlocked):
    """The host explicitly rejected the persistent session identity, not a timeout."""
    def __init__(self, message, *, identity='', method='session/resume', code=None):
        super().__init__(message)
        self.identity, self.method, self.code = identity, method, code

def canonical(value: Any) -> bytes:
    """Canonical JSON bytes, produced by Go.

    These bytes are the content-addressing format (artifact references,
    operation ids, evidence digests), so the authority lives in
    `internal/harness.CanonicalJSON`; this wrapper only carries the request and
    converts the returned text back to bytes.
    """
    answered = driver_bridge().call('canonical', {'value': value})
    text = answered.get('canonical')
    if text is None:
        raise HarnessError('Go bridge returned an unusable canonical answer')
    return text.encode()
def digest(value: Any) -> str:
    answered = driver_bridge().call('digest', {'value': value})
    result = answered.get('digest')
    if not result:
        raise HarnessError('Go bridge returned an unusable digest answer')
    return result
def atomic_bytes(path: Path, body: bytes) -> None:
    """Publish already-serialised bytes atomically.

    Split out so a caller that has just produced the canonical bytes (a content
    reference, for example) can write them without asking Go to encode the same
    value a second time.
    """
    path.parent.mkdir(parents=True, exist_ok=True)
    temp = path.with_name(f".{path.name}.{os.getpid()}.{threading.get_ident()}.tmp")
    try:
        with temp.open("xb") as f:
            f.write(body); f.flush(); os.fsync(f.fileno())
        os.replace(temp, path)
        if os.name != "nt":
            fd = os.open(path.parent, os.O_RDONLY)
            try: os.fsync(fd)
            finally: os.close(fd)
    finally:
        temp.unlink(missing_ok=True)
def atomic(path: Path, value: Any) -> None:
    atomic_bytes(path, canonical(value))
def read(path: Path) -> Any:
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result: raise HarnessError(f"duplicate JSON field: {key}")
            result[key] = value
        return result
    return json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=unique)
def file_proof(path: Path) -> dict:
    """Content proof of one evidence file, computed by Go.

    The primitive lives in `internal/harness.FileProof`; this wrapper only
    formats the path for the caller-visible refusal and translates the typed
    refusal back into RuntimeBlocked. It holds no hashing rule of its own.
    """
    answered = driver_bridge().call('file-proof', {'path': str(path)})
    if answered.get('ok'):
        return answered['proof']
    refusal = answered.get('refusal')
    if not refusal:
        raise HarnessError('Go bridge returned an unusable file-proof answer')
    raise RuntimeBlocked(refusal)

def git(root: Path, *args: str) -> str:
    p = subprocess.run(["git", "-C", str(root), *args], capture_output=True, text=True)
    if p.returncode: raise HarnessError(f"git {' '.join(args[:3])}: {p.stderr.strip()}")
    return p.stdout.strip()

def _transport_bytes(envelope: Any) -> bytes:
    """Serialize one bridge request.

    This is a *transport* encoding only: compact and non-ASCII preserving, but
    deliberately not the canonical format. The canonical format is a
    content-addressing contract owned by Go, and duplicating it here would give
    it a second implementation. It must not call `canonical` either, since that
    would recurse through the very request being built.

    ensure_ascii=False is kept so behaviour is unchanged: a lone surrogate still
    fails loudly at this boundary instead of being silently substituted.
    """
    return json.dumps(envelope, ensure_ascii=False, separators=(",", ":")).encode()

class Bridge:
    def __init__(self, binary: str):
        self.binary = str(Path(binary).resolve())
    def call(self, operation: str, payload: Any) -> Any:
        p = subprocess.run([self.binary, "harness", "bridge"],
                           input=_transport_bytes({"operation": operation, "payload": payload}), capture_output=True)
        try: answer = json.loads(p.stdout)
        except (ValueError, UnicodeError) as exc:
            raise HarnessError(f"Go bridge did not return JSON: {p.stderr.decode(errors='replace')[-1000:]}") from exc
        if p.returncode or not answer.get("ok"):
            raise HarnessError(answer.get("message", "Go bridge rejected the operation"))
        return answer["value"]

_DRIVER_BRIDGE = None
def driver_bridge() -> Bridge:
    """The single driver binary this process talks to.

    Resolved here rather than by importing the CLI: this module is the base
    layer, and reaching into `cli` for the path would drag in the graph, the
    workers and LangGraph just to build a bridge object. Production exports the
    binding; a direct import (a unit test, for example) falls back to the
    location scripts/harness and the test fixtures already use, so there is
    still exactly one lookup rule rather than a second mechanism.
    """
    global _DRIVER_BRIDGE
    if _DRIVER_BRIDGE is None:
        binary = os.environ.get('FORMAL_GATES_HARNESS_BINARY')
        if not binary:
            candidate = Path(__file__).resolve().parents[3] / '.harness' / 'bin' / 'formal-gates'
            if not candidate.is_file():
                raise HarnessError(
                    'Go driver binary is not bound: set FORMAL_GATES_HARNESS_BINARY, '
                    'or start through scripts/harness')
            binary = str(candidate)
        _DRIVER_BRIDGE = Bridge(binary)
    return _DRIVER_BRIDGE

@contextlib.contextmanager
def run_lock(directory: Path):
    directory.mkdir(parents=True, exist_ok=True)
    f = (directory / "runner.lock").open("a+b")
    try:
        if os.name == "nt":
            import msvcrt
            f.seek(0); f.write(b"0"); f.flush(); f.seek(0)
            try: msvcrt.locking(f.fileno(), msvcrt.LK_NBLCK, 1)
            except OSError as exc: raise HarnessError("RUN_BUSY: another runner owns this run") from exc
        else:
            import fcntl
            try: fcntl.flock(f.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError as exc: raise HarnessError("RUN_BUSY: another runner owns this run") from exc
        yield
    finally:
        f.close()

class Store:
    def __init__(self, directory: Path, manifest: dict, bridge: Bridge):
        self.directory, self.manifest, self.bridge = directory, manifest, bridge
        self._event_lock = threading.Lock()
        self.directory.mkdir(parents=True, exist_ok=True)
    def put(self, value: Any) -> str:
        body = canonical(value); ref = hashlib.sha256(body).hexdigest()
        path = self.directory / "artifacts" / f"{ref}.json"
        # Write the bytes already produced: re-deriving them inside atomic()
        # would encode the same value a second time, across a second process.
        if not path.exists(): atomic_bytes(path, body)
        return ref
    def get(self, ref: str) -> Any:
        if len(ref) != 64 or any(c not in "0123456789abcdef" for c in ref):
            raise HarnessError("invalid artifact reference")
        body = (self.directory / "artifacts" / f"{ref}.json").read_bytes()
        if hashlib.sha256(body).hexdigest() != ref: raise HarnessError("artifact integrity mismatch")
        return json.loads(body)
    def verify_process(self, process: dict) -> None:
        key = process.get('operation', '')
        directory = self.directory / 'jobs' / key
        if not key or Path(process.get('directory', '')).resolve() != directory.resolve():
            raise HarnessError('EVIDENCE_BINDING: job is outside this run')
        receipt = read(directory / 'exit.json')
        request = read(directory / 'request.json')
        if receipt.get('request_digest') != digest(request):
            raise HarnessError('EVIDENCE_BINDING: request digest differs')
        for field in ('exit_code', 'error', 'request_digest', 'streams'):
            if receipt.get(field) != process.get(field):
                raise HarnessError(f'EVIDENCE_BINDING: process {field} differs')
        proofs = receipt.get('streams', {})
        if set(proofs) != {'stdout.log', 'stderr.log'}:
            raise RuntimeBlocked('EVIDENCE_MISSING: stdout/stderr proofs absent')
        for name, proof in proofs.items():
            if file_proof(directory / name) != proof:
                raise RuntimeBlocked(f'EVIDENCE_CHANGED: {name}')

    def verify_evidence(self, ref: str) -> Any:
        value = self.get(ref)
        for name, proof in value.get('input_files', {}).items():
            if file_proof(Path(name)) != proof:
                raise RuntimeBlocked(f'EVIDENCE_CHANGED: comparison input {name}')
        if 'process' in value:
            self.verify_process(value['process'])
        source = value.get('source', {})
        if source.get('provider') not in (None, 'fixture'):
            from .native_hosts import adapter_for
            self.verify_process(source['process'])
            directory = self.directory / 'jobs' / source['job']
            adapter_for(source['provider']).verify_source(directory, source)
        return value

    def event(self, event: str, **fields: Any) -> None:
        record = {"at": time.time(), "event": event, **fields}
        with self._event_lock:
            with (self.directory / "events.jsonl").open("ab") as f:
                f.write(canonical(record) + b"\n"); f.flush()
            print(json.dumps(record, ensure_ascii=False), file=sys.stderr, flush=True)
    def clone(self, source: str | Path, name: str, candidate: str) -> Path:
        if not name or any(c not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_" for c in name):
            raise HarnessError("invalid workspace name")
        root = self.directory / "workspaces" / name
        receipt = root.with_suffix(".ready.json")
        expected = {"source": str(Path(source).resolve()), "base": candidate}
        if receipt.exists():
            if read(receipt) != expected: raise HarnessError("workspace binding changed")
            return root
        if root.exists():
            if not (root / ".git").is_dir() or git(root, "rev-parse", "HEAD") != candidate:
                raise RuntimeBlocked(f"PARTIAL_WORKSPACE: inspect {root}; no automatic destructive cleanup")
        else:
            root.parent.mkdir(parents=True, exist_ok=True)
            p = subprocess.run(["git", "clone", "--quiet", "--no-local", "--single-branch", "--no-tags", "--no-checkout", "--", str(source), str(root)], capture_output=True, text=True)
            if p.returncode: raise RuntimeBlocked(f"clone failed: {p.stderr.strip()}")
            git(root, "checkout", "--quiet", "--detach", candidate)
        exclude = root / ".git" / "info" / "exclude"
        with exclude.open("a") as f: f.write("\n.formal-gates-input/\n.formal-gates-cache/\n__pycache__/\n")
        atomic(receipt, expected)
        return root
    def job(self, key: str, request: dict) -> dict:
        """Reattach to the exact job. Only an explicit new attempt gets a new key."""
        directory = self.directory / "jobs" / key
        directory.mkdir(parents=True, exist_ok=True)
        intent = directory / "request.json"
        owner = None
        if intent.exists():
            if read(intent) != request: raise HarnessError("job id reused with different input")
        else:
            atomic(intent, request)
            self.event("job-start", operation=key, kind=request["kind"])
            with (directory / "wrapper.stderr").open("ab") as err:
                owner = subprocess.Popen([sys.executable, "-m", "formal_gates_harness.job", str(directory)],
                                         stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=err,
                                         start_new_session=True, close_fds=True)
        receipt = directory / "exit.json"
        start = time.monotonic()
        while not receipt.exists():
            process = directory / "process.json"
            if owner is not None and owner.poll() is not None and not receipt.exists():
                raise RuntimeBlocked(f"UNKNOWN_JOB: {key} exited without receipt; no blind replay")
            if process.exists():
                pid = read(process)["pid"]
                try: os.kill(pid, 0)
                except ProcessLookupError:
                    if not receipt.exists():
                        raise RuntimeBlocked(f"UNKNOWN_JOB: {key} exited without receipt; inspect evidence before retry")
                except PermissionError: pass
            elif time.monotonic() - start > 10:
                raise RuntimeBlocked(f"UNKNOWN_START: {key} has an intent but no process identity; not launched twice")
            time.sleep(0.1)
        if owner is not None: owner.wait()
        result = read(receipt)
        if result.get("request_digest") != digest(request): raise HarnessError("job receipt/input mismatch")
        record = {**result, "directory": str(directory), "operation": key}
        self.verify_process(record)
        return record
    def cancel_jobs(self) -> None:
        for process in (self.directory / "jobs").glob("*/process.json"):
            if (process.parent / "exit.json").exists(): continue
            pid = read(process)["pid"]
            if os.name != "nt":
                try:
                    if os.getpgid(pid) == pid: os.killpg(pid, signal.SIGTERM)
                except ProcessLookupError: pass

def runtime_identity(root: Path, binary: str) -> str:
    h = hashlib.sha256()
    package = root / "runtime" / "langgraph" / "formal_gates_harness"
    for p in sorted(package.rglob("*")):
        if p.is_file() and p.suffix in (".py", ".mjs"):
            h.update(p.relative_to(package).as_posix().encode()); h.update(p.read_bytes())
    h.update((root / "runtime" / "langgraph" / "requirements.lock").read_bytes())
    h.update(b'\x00reviewer-policy\x00')
    h.update((root / 'prompts' / 'reviewer-base.md').read_bytes())
    with open(binary, "rb") as f:
        for block in iter(lambda: f.read(1024 * 1024), b""): h.update(block)
    return h.hexdigest()
