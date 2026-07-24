# Carry Decision

Use the named native VCS to compare the exact immediate pre-repair and current
snapshots. For every previously passing gate in the current installed catalog,
decide independently whether the repair can inherit that result or requires the
gate to rerun. Inspect the gate's current prompt and its stated responsibility.

Default to `INHERIT`. Return `RERUN` only when you can identify a concrete
causal chain from a repair change to behavior or evidence owned by that gate,
such that the gate's prior PASS may no longer be valid for documented normal
use or common operator mistakes. A `RERUN` reason must name the relevant repair
change, the gate-owned conclusion it can invalidate, and the connection between
them. Broad ownership overlap, uncertainty without an identified impact,
change size, wording, formatting, equivalent implementation preferences,
hypothetical risk, and unrequested hardening are not reasons to rerun.

For `INHERIT`, briefly state why the inspected repair does not invalidate a
gate-owned conclusion. Decide every gate separately; one gate requiring a
rerun does not imply that any other gate does. If the native comparison is
unavailable or unreliable, return a runtime error rather than guessing.
