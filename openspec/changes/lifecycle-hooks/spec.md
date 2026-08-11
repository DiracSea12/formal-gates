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

The fixed independently dispatched actions are Start Readiness, QA Design, QA
Review, Development or Repair, QA Execution, and independent Carry. Each shall
bind its prepared dispatch to the host agent identity through
`workflow claim-dispatch`. Their lifecycle verification points are:

- `workflow record-action` for Start Readiness;
- `workflow qa-design`, `workflow qa-review`, and `workflow qa-execution` for
  the corresponding QA actions;
- `workflow snapshot --dispatch <id>` for Development or Repair completion;
- `workflow carry --dispatch <id>` for independent Carry; and
- `workflow record-gate` for every discovered gate.

Main-agent Requirements Clarification, Seal, and explicit `--main-agent` Carry
do not require lifecycle proof. A retained-overall snapshot that records
already sealed slice work also remains a main-agent integration operation and
does not create a development-worker lifecycle requirement.

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

> **SUPERSEDED note (the "Codex as `UNAVAILABLE`" part):** The statement below that
> Codex SHALL be treated as `UNAVAILABLE` for lifecycle enforcement was overturned by
> the deadlock-recovery and Codex lifecycle change (fix 2b; see
> `openspec/changes/deadlock-recovery-and-codex-lifecycle/master-requirements.md`).
> Current behavior lives in `internal/lifecycle/provider_codex.go`: the installed Codex
> adapter (`codexAdapter`) is now `required=true`, so an actually installed Codex binary
> with missing or unpaired start/stop events returns `REJECTED` instead of downgrading
> to `UNAVAILABLE`. Only the lenient default provider (`defaultAdapter`, resolved when
> the Codex binary is not installed beneath a host skills path — `go test`, the portable
> canary, and local development builds) still returns `UNAVAILABLE`. The rest of RQ-005
> (capability is not inferred from waiting for an event; a required provider never
> silently downgrades) remains current.

Capability shall not be inferred from waiting for an event. Codex shall be
treated as `UNAVAILABLE` for lifecycle enforcement while still receiving the
installed lifecycle hook entries. Claude Code and Cursor shall require their
normal lifecycle events. A provider marked required shall not silently
downgrade when events are absent.

When `workflow claim-dispatch` binds the host agent identity, the lifecycle
module shall resolve the host provider automatically from the installed native
binary's canonical location and persist that provider in its own dispatch
binding. The Codex, Claude Code, and Cursor installers place their binaries in
distinct maintained roots. Workflow state does not own provider capability or
event data; later result commands ask the lifecycle module to verify the
dispatch. A directly built source binary outside a maintained host installation
uses the conservative Codex `UNAVAILABLE` policy for portable development and
package checks.

The provider is immutable for that dispatch. An event from another provider
cannot satisfy it.

### RQ-006 Provide public lifecycle capture and verification entrypoints

The CLI shall provide a host-hook entrypoint that accepts provider and event
name plus the host JSON payload on stdin, and a read-only verification
entrypoint that reports the mechanically derived lifecycle outcome for a
prepared dispatch. These commands expose only normalized status and diagnostic
facts; they do not let callers submit or override an outcome.

Start and stop events may arrive before or after the dispatch claim and may be
delivered more than once. Matching is by the dispatch-bound provider and
claimed host agent identity. A complete matching start/stop set verifies
regardless of capture order; duplicates have no additional effect.

### RQ-007 Preserve the normal-use boundary

This feature guarantees documented normal use and common operator mistakes.
Adversarial evidence tampering, malicious local edits, manual rewriting of
temporary state, permission or immutable-file failures, attack-style inputs,
and unsupported host behavior are out of scope and cannot block PASS or cause
additional hardening.

## Technical Solution

Add a standalone lifecycle package with provider adapters and a small journal
owned by the lifecycle module. The hook entrypoint captures a provider and
event name plus the host JSON payload, normalizes the host agent identity, and
stores idempotent start/stop observations in plugin-owned temporary data. A
later dispatch claim binds matching observations to the dispatch without
requiring the host event to contain formal-gates fields.

Every independently dispatched fixed action and every catalog gate owns a
prepared dispatch and claimed host agent identity. Result recording, or
snapshot recording for Development and Repair, asks the lifecycle verifier for
the dispatch outcome. For catalog gates, independent-agent and lifecycle
requirements are automatic. Codex returns `UNAVAILABLE`; Claude Code and
Cursor return `VERIFIED` only after matching start and stop observations and
otherwise return `REJECTED`.

Provider payload parsing and hook naming stay in provider-specific files.
Installer composition knows only the lifecycle hook command supplied by the
module. Removing or replacing lifecycle support shall not require redesigning
the workflow state machine or gate catalog.

## Verification

- Direct lifecycle tests cover normalization, idempotent event order, matching
  identity, missing events, mismatched identity, and provider capability.
- Workflow tests prove required-provider rejection leaves a gate pending,
  verified lifecycle permits recording, every fixed independent action checks
  at its named transition, and Codex `UNAVAILABLE` preserves the existing
  dispatch path.
- Catalog tests prove newly discovered gates inherit independent reviewer and
  lifecycle requirements.
- Installer tests prove all supported host configurations receive lifecycle
  hooks while unrelated hooks remain intact.
- Run the Go test suite, race detector, vet, build, package validation,
  portable canary, behavior evaluation, and relevant live host canaries.

The Claude Code and Cursor live procedures are:

1. install the candidate for that host with hooks enabled;
2. start a run and prepare an independent gate through that host's installed
   binary;
3. start one normal host subagent with the exact prepared task, claim the
   returned host identity, and confirm the dispatch-bound provider with
   `lifecycle verify`;
4. let the subagent finish normally;
5. run `lifecycle verify --root <repo> --run-id <id> --dispatch <id>` and then
   record its semantic result.

The canary passes only when verification reports `VERIFIED` and result
recording succeeds. Missing events, provider mismatch, or `UNAVAILABLE` on
Claude Code or Cursor fails that host canary. When a host executable is not
installed or cannot normally launch a subagent in the test environment, that
live-host canary is unavailable rather than simulated; direct provider,
installer, hook-entrypoint, and workflow tests still run.
