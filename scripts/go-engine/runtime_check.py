#!/usr/bin/env python3
"""Runtime dependency verification shared by the acceptance and admission tools.

`requirements.lock` pins versions but proves nothing about what is actually
installed. Both tools need the same answer to "can this interpreter actually run
the harness?", so the logic lives here rather than being written twice.

Nothing here installs anything or changes host configuration.
"""

from __future__ import annotations

import hashlib
import json
import subprocess
from pathlib import Path

LOCK_RELATIVE_PATH = "runtime/langgraph/requirements.lock"


def read_lock(root: Path) -> dict[str, str]:
    """Return the pinned distributions as {normalised name: version}."""
    path = root / LOCK_RELATIVE_PATH
    pinned: dict[str, str] = {}
    for line in path.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if "==" not in line:
            continue
        name, _, version = line.partition("==")
        pinned[normalise(name)] = version.strip()
    return pinned


def normalise(name: str) -> str:
    """PEP 503 normalisation, so `ruamel.yaml` and `ruamel-yaml` agree."""
    return name.strip().lower().replace("_", "-").replace(".", "-")


def interpreter_identity(python: Path) -> dict:
    """Report which interpreter would actually run the harness."""
    program = (
        "import json,platform,sys;"
        "print(json.dumps({'executable':sys.executable,'version':platform.python_version(),"
        "'prefix':sys.prefix,'base_prefix':sys.base_prefix}))"
    )
    proc = subprocess.run([str(python), "-c", program], capture_output=True, text=True)
    if proc.returncode != 0:
        return {"error": (proc.stderr or proc.stdout).strip() or f"exit {proc.returncode}"}
    try:
        return json.loads(proc.stdout.strip().splitlines()[-1])
    except ValueError as exc:
        return {"error": f"unparseable interpreter identity: {exc}"}


def installed_versions(python: Path, names: list[str]) -> dict[str, str | None]:
    """Return the installed version of each named distribution, or None."""
    program = (
        "import json,sys\n"
        "from importlib.metadata import version, PackageNotFoundError\n"
        "out = {}\n"
        "for name in json.loads(sys.argv[1]):\n"
        "    try:\n"
        "        out[name] = version(name)\n"
        "    except PackageNotFoundError:\n"
        "        out[name] = None\n"
        "    except Exception as exc:\n"
        "        out[name] = 'ERROR:' + type(exc).__name__\n"
        "print(json.dumps(out))\n"
    )
    proc = subprocess.run([str(python), "-c", program, json.dumps(names)], capture_output=True, text=True)
    if proc.returncode != 0:
        return {name: f"ERROR:{(proc.stderr or '').strip().splitlines()[-1] if proc.stderr.strip() else 'exit'}" for name in names}
    try:
        return json.loads(proc.stdout.strip().splitlines()[-1])
    except ValueError:
        return {name: "ERROR:unparseable" for name in names}


def verify(root: Path, python: Path) -> dict:
    """Compare the pinned lock against what is actually installed."""
    result: dict = {"lock_path": LOCK_RELATIVE_PATH, "python": str(python)}
    if not python.is_file():
        result["error"] = f"interpreter is missing: {python}"
        return result
    pinned = read_lock(root)
    installed = installed_versions(python, sorted(pinned))
    missing = sorted(name for name in pinned if installed.get(name) is None)
    mismatched = sorted(
        f"{name}: lock {pinned[name]} != installed {installed.get(name)}"
        for name in pinned
        if installed.get(name) is not None and installed.get(name) != pinned[name]
    )
    errored = sorted(
        f"{name}: {installed[name]}"
        for name in pinned
        if isinstance(installed.get(name), str) and str(installed[name]).startswith("ERROR:")
    )
    fingerprint = hashlib.sha256(
        "\n".join(f"{name}=={installed.get(name)}" for name in sorted(pinned)).encode()
    ).hexdigest()
    result.update(
        {
            "pinned_count": len(pinned),
            "missing": missing,
            "mismatched": mismatched,
            "errored": errored,
            "installed_fingerprint": fingerprint,
            "ok": not missing and not mismatched and not errored,
        }
    )
    return result
