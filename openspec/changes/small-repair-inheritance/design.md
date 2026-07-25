# Design

Preserve a prior QA PASS at its old snapshot when a repair snapshot advances,
just as passing discovered gates are retained for Carry eligibility. Normal QA
Execution remains preparable because the retained result is not authoritative
for the new snapshot.

Add a main-agent mode to `workflow carry`, with a required reason and current
live snapshot. The mode does not use agent source bindings because no Carry
prompt was dispatched. Under the existing run lock it:

1. validates the repaired snapshot and Carry transition;
2. collects prior PASS QA and discovered-gate results;
3. records `INHERIT` decisions with a structured main-shortcut origin;
4. rebinds those results to the repair snapshot;
5. runs the existing review-wave completion check.

Extract shared Carry application logic only where it removes real duplication.
Independent Carry keeps its exact prompt and per-gate validation contract.

Use direct state-machine tests for eligibility, audit fields, QA inheritance,
non-PASS rerun scope, and fallback behavior. Add only parser-specific CLI
coverage at the CLI layer.
