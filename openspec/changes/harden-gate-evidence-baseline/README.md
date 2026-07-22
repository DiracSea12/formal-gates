# harden-gate-evidence-baseline

Make formal PASS receipt-bound, context-isolated, recursively hash-bound, and
valid for the final deliverable snapshot without requiring every gate to rerun
after each small repair or requiring any particular requirement-document format.

Phase 3 converges operational scope around one host-neutral `.gates` run root
and caller-provided external VCS snapshot identities. Formal development
requires an available Git, SVN, P4, or equivalent VCS. The worker,
orchestrator, and reviewer invoke that VCS directly to inspect delivery changes
and compare exact repair snapshots. The complexity reviewer inspects native VCS
diff, stat, and changed contents. The worker submits every delivery path explicitly, adds each new delivery
file to the named VCS immediately before further edits, and adds an existing
untracked delivery file before modifying or deleting it. Before a repair, every
path it may touch is tracked and the worker uses the VCS's native state or
checkpoint facility to fix the pre-repair snapshot. The same VCS fixes the
post-repair snapshot for direct reviewer comparison. Only explicit paths may be added, and every
delivery path must be tracked and present in the complete diff before the worker
returns. The CLI only normalizes those paths and generates changed-files
evidence. If the VCS cannot reliably compare the exact snapshots, Carry is
unavailable and affected gates cannot enter a new-snapshot rerun without
terminal `RERUN_REQUIRED`. Every review role also completes a same-pattern and same-chain sweep
before returning, reports all independent related problems together, and keeps
unrelated history, other gate responsibilities, and unapproved QA cases out of
scope. The phase also removes obsolete contracts and consolidates proven
duplicate code and documentation owners repository-wide.
