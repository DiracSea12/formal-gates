#!/usr/bin/env python3
"""Walk a native run's raw evidence chain with the harness's own verifiers.

Usage: python native_evidence_probe.py RUN_DIRECTORY

Prints one JSON object: {"roles": [...], "checks": N, "gates": M, "terminal": "..."}.
Exits non-zero with the failing reason on stderr when any evidence does not verify.

This mirrors what the flow itself requires before it may issue a completion
certificate (graph.py seal()): every worker receipt, and every check and gate
the terminal artifact depends on, goes through Store.verify_evidence -- the
same rules that refused the identical missing evidence during the run. The
terminal artifact's own bytes are necessary but never sufficient here.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: native_evidence_probe.py RUN_DIRECTORY", file=sys.stderr)
        return 2
    root = Path(__file__).resolve().parents[2]
    sys.path.insert(0, str(root / "runtime" / "langgraph"))
    from formal_gates_harness.cli import open_run
    from formal_gates_harness.infra import read

    run = Path(sys.argv[1]).resolve()
    _, store = open_run(run)

    roles = []
    for receipt in sorted((run / "workers").glob("*.json")):
        value = store.verify_evidence(read(receipt)["ref"])
        roles.append({"role": value.get("role"),
                      "status": (value.get("result") or {}).get("status"),
                      "provider": (value.get("source") or {}).get("provider")})

    events = [json.loads(line) for line in (run / "events.jsonl").read_text().splitlines() if line.strip()]
    terminals = [e for e in events if e.get("event") == "terminal"]
    term = terminals[-1] if terminals else {}
    terminal = store.get(term["evidence"]) if term.get("evidence") else {}

    # seal() verified every check and gate evidence reference before certifying;
    # admission re-walks exactly those references, and against the spec's own
    # required ids rather than trusting the terminal's self-description.
    for entry in (terminal.get("checks") or []) + (terminal.get("gates") or []):
        store.verify_evidence(entry["evidence"])
    spec = store.manifest.get("spec") or {}
    want_checks = {c.get("id") for c in spec.get("checks") or []}
    want_gates = set(spec.get("gates") or [])
    got_checks = {c.get("id") for c in terminal.get("checks") or []}
    got_gates = {g.get("id") for g in terminal.get("gates") or []}
    if want_checks != got_checks or want_gates != got_gates:
        print(f"terminal dependencies do not match the spec: checks {sorted(got_checks)} vs {sorted(want_checks)}, "
              f"gates {sorted(got_gates)} vs {sorted(want_gates)}", file=sys.stderr)
        return 1

    print(json.dumps({"roles": roles,
                      "checks": len(got_checks),
                      "gates": len(got_gates),
                      "terminal": term.get("status")}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
