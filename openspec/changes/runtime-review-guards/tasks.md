# Tasks

- [ ] Add compact prepared-dispatch state with generated IDs, prompt hashes,
  attempts, waves, source bindings, and reviewer identity history.
- [ ] Bind QA Review and every gate result to one open dispatch and reject
  missing, stale, mismatched, completed, or reused reviewer identities.
- [ ] Add a lightweight dispatch-claim transition that reserves the host
  reviewer/session identity before results are accepted, including when an
  earlier dispatch is interrupted without returning a result.
- [ ] Remove repeated operator-supplied source binding fields where prepared
  state is the existing owner.
- [ ] Add native Git, SVN, and P4 snapshot resolvers selected by the run's VCS;
  use them in preparation, snapshot, result, Carry, and Seal transitions.
- [ ] Register, hash, freeze, and validate a format-neutral set of requirement
  and solution artifacts at development start.
- [ ] Exclude frozen requirement artifacts from post-development review targets
  while retaining them as acceptance input.
- [ ] Add `STATIC` and `LIVE` QA case kinds and require both in every complete
  QA set and execution.
- [ ] Update SKILL, READMEs, prompts, help, examples, metadata, and changelog
  without restoring evidence-heavy subsystems or adding dry-run behavior.
- [ ] Add focused state, CLI, resolver, prompt, package, and behavior coverage at
  the lowest owning layers.
- [ ] Run full tests, race tests, vet, build, package validation, portable
  canary, behavior evaluation, diff checks, QA Execution, and every full-route
  review gate.
