# Complexity Review CLI Composition

Sample-only: this page is not a formal PASS artifact. It intentionally contains no hand-authored reviewer JSON. The CLI generates the context bundle, changed-files evidence, final-send prompt, policy/check catalog, evidence bindings, reviewer template, final artifact, and receipt.

Run the generation steps from the repository root. Every input named below must already be a run-local output from its owning CLI or verification runner. Use the external VCS directly to inspect the complete delivery diff, its native stat output, and changed contents, and explicitly pass every delivery path to the CLI-owned changed-files composer. formal-gates records workflow snapshots, static evidence, and decisions.

```bash
bin/formal-gates artifact compose-changed-files \
  --root . --run-dir .gates/runs/RUN_ID \
  --workflow-id WORKFLOW_ID --change-snapshot EXTERNAL_VCS_SNAPSHOT \
  --path internal/a.go --path README.md \
  --output restricted/changed-files.txt

bin/formal-gates artifact compose-context-bundle \
  --root . --run-dir .gates/runs/RUN_ID \
  --workflow-id WORKFLOW_ID --change-snapshot EXTERNAL_VCS_SNAPSHOT \
  --output restricted/complexity/context-bundle.json \
  --input restricted/requirements/requirements.json \
  --input restricted/changed-files.txt

bin/formal-gates prompt prepare --root . \
  --output .gates/runs/RUN_ID/restricted/complexity/prompt.txt \
  --gate complexity-gate \
  --current-requirement openspec/changes/CHANGE \
  --current-diff '<external VCS command that emits the complete delivery diff>' \
  --worktree . --change-snapshot EXTERNAL_VCS_SNAPSHOT \
  --review-artifact .gates/runs/RUN_ID/restricted/complexity/review.json \
  --policy-id complexity.post-development.v2 \
  --context-bundle .gates/runs/RUN_ID/restricted/complexity/context-bundle.json

bin/formal-gates receipt register \
  --provider codex --worktree . --run-dir .gates/runs/RUN_ID \
  --context-bundle restricted/complexity/context-bundle.json \
  --prompt restricted/complexity/prompt.txt \
  --changed-files restricted/changed-files.txt \
  --verification restricted/verification.json \
  --artifact .gates/runs/RUN_ID/restricted/complexity/review.json \
  --gate complexity-gate --workflow-id WORKFLOW_ID \
  --change-snapshot EXTERNAL_VCS_SNAPSHOT
```

Send the registered prompt verbatim. The reviewer never edits the assigned JSON; it calls `receipt submit` with one ordered status/message group per generated check and optional finding/location groups. The CLI constructs the complete nested artifact, records the submission proof, and leaves the artifact unchanged when semantic input is incomplete or invalid. Run `receipt finalize` after submission and, on a lifecycle-capable provider, after its real dispatch stop event. Codex has no usable subagent lifecycle event, so it keeps every non-lifecycle finalization check without requiring or synthesizing one.
