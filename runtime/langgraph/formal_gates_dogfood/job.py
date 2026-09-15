"""One owned command, with an exit receipt independent of the runner."""
from __future__ import annotations
import os
from pathlib import Path
import signal
import subprocess
import sys
import time
from .infra import atomic, digest, read

def main(directory: Path) -> int:
    request = read(directory / "request.json")
    try: fd = os.open(directory / "owner", os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    except FileExistsError: return 2
    os.close(fd)
    atomic(directory / "process.json", {"pid": os.getpid(), "started_at": time.time()})
    started = time.time(); code = 127; error = ""; child = None
    def terminate(signum, frame):
        if child is not None:
            try: child.terminate()
            except ProcessLookupError: pass
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, terminate)
    env = os.environ.copy(); env.update(request.get("environment", {}))
    try:
        with (directory / "stdout.log").open("wb") as out, (directory / "stderr.log").open("wb") as err:
            child = subprocess.Popen(request["argv"], cwd=request["cwd"], env=env,
                                     stdin=subprocess.PIPE, stdout=out, stderr=err)
            child.communicate(request.get("stdin", "").encode())
            code = child.returncode
            out.flush(); err.flush(); os.fsync(out.fileno()); os.fsync(err.fileno())
    except KeyboardInterrupt:
        code = 130; error = "owned job was cancelled"
        if child is not None:
            try: child.wait(timeout=5)
            except subprocess.TimeoutExpired: child.kill(); child.wait()
    except Exception as exc:
        error = f"{type(exc).__name__}: {exc}"
    atomic(directory / "exit.json", {"exit_code": code, "error": error, "started_at": started,
                                     "finished_at": time.time(), "request_digest": digest(request)})
    return 0

if __name__ == "__main__":
    raise SystemExit(main(Path(sys.argv[1])))
