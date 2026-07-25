# Requirements Alignment

Date: 2026-07-26
Status: confirmed

## RQ-001 - Main-agent small-repair judgment

After a repair snapshot is frozen, the main agent MAY skip the independent
Carry agent only when it has inspected the immediate pre-repair-to-current
native VCS comparison and can bound the repair such that no previously passing
selected verification can be affected.

No fixed line, file, or token threshold SHALL define a small repair. A change
that affects shared APIs, public behavior, configuration, dependencies,
cross-gate ownership, or any uncertain causal chain SHALL use independent Carry.

## RQ-002 - One Carry state owner

The shortcut SHALL extend the existing `workflow carry` transition and reuse
the same eligibility, snapshot rebinding, persistence, and review-wave
completion logic. It SHALL not add another workflow command or parallel repair
state machine.

The shortcut SHALL require a non-empty main-agent reason and SHALL record a
structured decision origin, the immediate source snapshot, the target snapshot,
and the reason.

## RQ-003 - Inherit every prior PASS

When the shortcut is selected, every previously passing selected discovered
gate and a previously passing selected QA Execution SHALL be inherited onto the
current repair snapshot. Their semantic results and non-blocking findings SHALL
remain intact. Results that were FAIL, RUNTIME_ERROR, PENDING, empty, or not
selected SHALL not be inherited.

Only the non-passing selected results SHALL remain for rerun or existing
runtime-error handling.

## RQ-004 - Independent Carry remains available

When the shortcut is not justified, the workflow SHALL dispatch independent
Carry exactly as before. Independent Carry SHALL continue to inspect only
previously passing selected discovered gates and return per-gate `INHERIT` or
`RERUN`; it SHALL not decide QA inheritance.

The main-agent shortcut is all-or-nothing for eligible PASS results. If any
prior PASS may require rerun, the main agent SHALL use independent Carry rather
than selectively self-approving results.
