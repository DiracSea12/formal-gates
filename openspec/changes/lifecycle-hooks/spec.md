# Lifecycle Hook Verification

## Goal

Restore host subagent lifecycle hooks as a small, replaceable module that lets
formal-gates mechanically enforce independent gate execution where the host
provides usable lifecycle events, without restoring the removed receipt,
closure, policy, or evidence-graph systems.

## Requirements

### RQ-001 Install lifecycle hooks for every supported host

The native installer shall configure `SubagentStart` and `SubagentStop` for
Claude Code and Codex, and `subagentStart` and `subagentStop` for Cursor, while
preserving unrelated hook entries. The lifecycle hook command shall use the
installed native formal-gates binary and a provider-specific adapter.

### RQ-002 Keep lifecycle integration decoupled

Lifecycle event models, provider normalization, persistence, and verification
shall live outside the workflow implementation. Workflow result recording
shall depend on a small lifecycle verification interface and shall not parse
host payloads or own lifecycle journal formats.

### RQ-003 Keep the workflow fixed and gates extensible

The set and semantics of built-in workflow actions remain fixed. The catalog
is the only extension point: every discovered gate automatically requires a
new independent reviewer identity and inherits lifecycle verification without
provider-specific or gate-specific workflow changes.

Fixed workflow actions that require independent agents shall retain their
existing explicit rules. Main-agent Requirements Clarification, Seal, and
explicit `--main-agent` Carry do not require lifecycle proof.

### RQ-004 Decide lifecycle compliance mechanically

AI agents shall not decide, assert, or override lifecycle compliance. Before a
result that requires lifecycle verification is recorded, the program shall
derive one of these outcomes:

- `VERIFIED`: the required start and stop events are present, refer to the
  claimed identity, and satisfy identity isolation rules;
- `REJECTED`: a provider with required lifecycle support has missing,
  inconsistent, or disallowed lifecycle evidence;
- `UNAVAILABLE`: the selected provider does not expose usable lifecycle events
  and the existing dispatch and reviewer-identity checks remain authoritative.

A rejected result shall leave its workflow target pending. A target that is
never recorded remains pending and blocks Seal through the existing workflow
rules.

### RQ-005 Use provider-owned capability policy

Capability shall not be inferred from waiting for an event. Codex shall be
treated as `UNAVAILABLE` for lifecycle enforcement while still receiving the
installed lifecycle hook entries. Claude Code and Cursor shall require their
normal lifecycle events. A provider marked required shall not silently
downgrade when events are absent.

### RQ-006 Preserve the normal-use boundary

This feature guarantees documented normal use and common operator mistakes.
Adversarial evidence tampering, malicious local edits, manual rewriting of
temporary state, permission or immutable-file failures, attack-style inputs,
and unsupported host behavior are out of scope and cannot block PASS or cause
additional hardening.

## Technical Solution

Add a standalone lifecycle package with provider adapters and a small journal
owned by the lifecycle module. The hook entrypoint captures a provider and
event name plus the host JSON payload, normalizes the agent identity and any
dispatch correlation, and stores idempotent start/stop observations under the
active run's temporary data.

Workflow dispatches retain their existing dispatch and reviewer identity
ownership. Result recording asks the lifecycle verifier for the dispatch
outcome. For catalog gates, reviewer requirement and lifecycle enforcement are
automatic. Codex returns `UNAVAILABLE`; Claude Code and Cursor return
`VERIFIED` only after matching start and stop observations and otherwise return
`REJECTED`.

Provider payload parsing and hook naming stay in provider-specific files.
Installer composition knows only the lifecycle hook command supplied by the
module. Removing or replacing lifecycle support shall not require redesigning
the workflow state machine or gate catalog.

## Verification

- Direct lifecycle tests cover normalization, idempotent event order, matching
  identity, missing events, mismatched identity, and provider capability.
- Workflow tests prove required-provider rejection leaves a gate pending,
  verified lifecycle permits recording, and Codex `UNAVAILABLE` preserves the
  existing dispatch path.
- Catalog tests prove newly discovered gates inherit independent reviewer and
  lifecycle requirements.
- Installer tests prove all supported host configurations receive lifecycle
  hooks while unrelated hooks remain intact.
- Run the Go test suite, race detector, vet, build, package validation,
  portable canary, behavior evaluation, and relevant live host canaries.
