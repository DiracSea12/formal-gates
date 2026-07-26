# Proposal

Add lightweight runtime bindings to the existing formal workflow. Each
independent review preparation receives one unique dispatch record containing
the exact prompt hash, review attempt and wave, requirement and catalog
revisions, and immutable VCS snapshot. A returned review result identifies that
dispatch and the host reviewer or session. The CLI rejects stale, mismatched,
or reused review identities before recording a semantic result.

Remove repeated snapshot and source-binding transcription from normal operator
commands. The main agent selects the actual VCS at run start, and the CLI uses a
small native Git, SVN, or P4 resolver to obtain and verify immutable identities
for preparation, snapshot recording, result recording, Carry, and Seal. No VCS
is guessed and no second version-control model is stored.

Treat requirements and solution documents as development inputs rather than
post-development deliverables. Register and freeze the complete document set at
development start. Post-development agents may read those frozen bytes as the
acceptance standard but do not review the documents themselves. Any later
change returns to requirement clarification or change; ordinary repair cannot
rewrite the frozen set.

Make QA coverage explicit. Each approved case is either `STATIC` or `LIVE`.
The complete set and its execution must contain both fast direct-owner checks
and real public-entrypoint execution. This does not introduce dry-run behavior
or require duplicate higher-level tests for deterministic rules already covered
at their owning layer.

Make QA Review incremental at case level. A reviewer returns a decision for
each case assigned in that attempt, and the CLI stores the marker on the
existing case. After QA Design addresses failures, unchanged passing cases stay
approved; only failed, new, or changed cases require another independent QA
Review. Requirement changes use the existing incremental QA Design behavior:
unaffected retained cases keep approval, affected cases are revised and reset,
and cases for removed requirements disappear from the complete set.
