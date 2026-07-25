# Requirements Alignment

Date: 2026-07-26
Status: confirmed

## RQ-001 - Stateless candidate discovery

The CLI SHALL expose a read-only command with this public shape:

```text
formal-gates package route-candidates --root <package>
```

The command SHALL return an ordered JSON array containing `qa` first followed
by every currently discovered gate ID in lexical order. It SHALL not require a
repository root, requirement file, run ID, VCS snapshot, or workflow state and
SHALL not write files.

## RQ-002 - One catalog owner

Candidate discovery SHALL reuse the existing prompt catalog loader, gate ID
validation, reserved `qa` rejection, direct-file validation, and lexical order.
It SHALL not add another directory scanner, registry, manifest, or sort owner.

## RQ-003 - Existing workflow remains unchanged

The existing run-bound `workflow route-candidates` command and route state
transitions SHALL remain unchanged in this slice. Package and CLI documentation
SHALL describe the new pre-run query distinctly from the run-bound query.
