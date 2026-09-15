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

class DogfoodError(RuntimeError):
    pass
class RuntimeBlocked(DogfoodError):
    pass

def canonical(value: Any) -> bytes:
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()
def digest(value: Any) -> str:
    return hashlib.sha256(canonical(value)).hexdigest()
def atomic(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temp = path.with_name(f".{path.name}.{os.getpid()}.{threading.get_ident()}.tmp")
    try:
        with temp.open("xb") as f:
            f.write(canonical(value)); f.flush(); os.fsync(f.fileno())
        os.replace(temp, path)
        if os.name != "nt":
            fd = os.open(path.parent, os.O_RDONLY)
            try: os.fsync(fd)
            finally: os.close(fd)
    finally:
        temp.unlink(missing_ok=True)
def read(path: Path) -> Any:
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result: raise DogfoodError(f"duplicate JSON field: {key}")
            result[key] = value
        return result
    return json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=unique)
def git(root: Path, *args: str) -> str:
    p = subprocess.run(["git", "-C", str(root), *args], capture_output=True, text=True)
    if p.returncode: raise DogfoodError(f"git {' '.join(args[:3])}: {p.stderr.strip()}")
    return p.stdout.strip()

class Bridge:
    def __init__(self, binary: str):
        self.binary = str(Path(binary).resolve())
    def call(self, operation: str, payload: Any) -> Any:
        p = subprocess.run([self.binary, "dogfood", "bridge"],
                           input=canonical({"operation": operation, "payload": payload}), capture_output=True)
        try: answer = json.loads(p.stdout)
        except (ValueError, UnicodeError) as exc:
            raise DogfoodError(f"Go bridge did not return JSON: {p.stderr.decode(errors='replace')[-1000:]}") from exc
        if p.returncode or not answer.get("ok"):
            raise DogfoodError(answer.get("message", "Go bridge rejected the operation"))
        return answer["value"]

@contextlib.contextmanager
def run_lock(directory: Path):
    directory.mkdir(parents=True, exist_ok=True)
    f = (directory / "runner.lock").open("a+b")
    try:
        if os.name == "nt":
            import msvcrt
            f.seek(0); f.write(b"0"); f.flush(); f.seek(0)
            try: msvcrt.locking(f.fileno(), msvcrt.LK_NBLCK, 1)
            except OSError as exc: raise DogfoodError("RUN_BUSY: another runner owns this run") from exc
        else:
            import fcntl
            try: fcntl.flock(f.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError as exc: raise DogfoodError("RUN_BUSY: another runner owns this run") from exc
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
        if not path.exists(): atomic(path, value)
        return ref
    def get(self, ref: str) -> Any:
        if len(ref) != 64 or any(c not in "0123456789abcdef" for c in ref):
            raise DogfoodError("invalid artifact reference")
        body = (self.directory / "artifacts" / f"{ref}.json").read_bytes()
        if hashlib.sha256(body).hexdigest() != ref: raise DogfoodError("artifact integrity mismatch")
        return json.loads(body)
    def event(self, event: str, **fields: Any) -> None:
        record = {"at": time.time(), "event": event, **fields}
        with self._event_lock:
            with (self.directory / "events.jsonl").open("ab") as f:
                f.write(canonical(record) + b"\n"); f.flush()
            print(json.dumps(record, ensure_ascii=False), file=sys.stderr, flush=True)
    def clone(self, source: str | Path, name: str, candidate: str) -> Path:
        if not name or any(c not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_" for c in name):
            raise DogfoodError("invalid workspace name")
        root = self.directory / "workspaces" / name
        receipt = root.with_suffix(".ready.json")
        expected = {"source": str(Path(source).resolve()), "base": candidate}
        if receipt.exists():
            if read(receipt) != expected: raise DogfoodError("workspace binding changed")
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
            if read(intent) != request: raise DogfoodError("job id reused with different input")
        else:
            atomic(intent, request)
            self.event("job-start", operation=key, kind=request["kind"])
            with (directory / "wrapper.stderr").open("ab") as err:
                owner = subprocess.Popen([sys.executable, "-m", "formal_gates_dogfood.job", str(directory)],
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
        if result.get("request_digest") != digest(request): raise DogfoodError("job receipt/input mismatch")
        return {**result, "directory": str(directory), "operation": key}
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
    for p in sorted((root / "runtime" / "langgraph" / "formal_gates_dogfood").glob("*")):
        if p.is_file() and p.suffix in (".py", ".mjs"):
            h.update(p.name.encode()); h.update(p.read_bytes())
    h.update((root / "runtime" / "langgraph" / "requirements.lock").read_bytes())
    with open(binary, "rb") as f:
        for block in iter(lambda: f.read(1024 * 1024), b""): h.update(block)
    return h.hexdigest()
