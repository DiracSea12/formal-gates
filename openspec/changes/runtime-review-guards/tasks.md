# Tasks

- [x] Add compact prepared-dispatch state with generated IDs, prompt hashes,
  attempts, waves, source bindings, and reviewer identity history.
- [x] Bind QA Review and every gate result to one open dispatch and reject
  missing, stale, mismatched, completed, or reused reviewer identities.
- [x] Add a lightweight dispatch-claim transition that reserves the host
  reviewer/session identity before results are accepted, including when an
  earlier dispatch is interrupted without returning a result.
- [x] Remove repeated operator-supplied source binding fields where prepared
  state is the existing owner.
- [x] Add native Git, SVN, and P4 snapshot resolvers selected by the run's VCS;
  use them in preparation, snapshot, result, Carry, and Seal transitions.
- [x] Register, hash, freeze, and validate a format-neutral set of requirement
  and solution artifacts at development start.
- [x] Exclude frozen requirement artifacts from post-development review targets
  while retaining them as acceptance input.
- [x] Add `STATIC` and `LIVE` QA case kinds and require both in every complete
  QA set and execution.
- [x] Add per-case QA Review outcomes, preserve approval only for exact
  unchanged cases retained by incremental QA Design, and compose retries from
  failed, new, or changed cases plus accepted coverage summaries.
- [x] Update the QA Review prompt and result contract so reviewers return one
  decision for every assigned case and do not reopen accepted cases.
- [x] Update SKILL, READMEs, prompts, help, examples, metadata, and changelog
  without restoring evidence-heavy subsystems or adding dry-run behavior.
- [x] Add focused state, CLI, resolver, prompt, package, and behavior coverage at
  the lowest owning layers.
- [x] Run full tests, race tests, vet, build, package validation, portable
  canary, behavior evaluation, diff checks, QA Execution, and every full-route
  review gate.
