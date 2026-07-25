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

A package with zero discovered gates SHALL be valid and the command SHALL
return `["qa"]`. This zero-gate rule applies to the shared prompt catalog, not
only to the pre-run query.

## RQ-002 - One catalog owner

Candidate discovery SHALL reuse the existing prompt catalog loader, gate ID
validation, reserved `qa` rejection, direct-file validation, and lexical order.
It SHALL not add another directory scanner, registry, manifest, or sort owner.

The shared loader SHALL consider only direct `.md` files in `gates/` as gate
candidates. It SHALL ignore unrelated non-Markdown files and ordinary
subdirectories. A direct entry whose name ends in `.md` but is not a regular
file SHALL remain invalid, as SHALL an invalid gate ID or the reserved
`qa.md` name.

## RQ-003 - Existing workflow remains unchanged

The existing run-bound `workflow route-candidates` command and route state
transitions SHALL remain unchanged in this slice. Package and CLI documentation
SHALL describe the new pre-run query distinctly from the run-bound query.
