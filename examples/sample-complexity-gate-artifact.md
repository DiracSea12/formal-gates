# Complexity Review CLI Composition

Sample-only: this page is not a formal PASS artifact. It intentionally contains no hand-authored reviewer JSON. The CLI generates the context bundle, changed-files evidence, final-send prompt, policy/check catalog, evidence bindings, reviewer template, final artifact, and receipt.

Run the generation steps from the repository root. Every input named below must already be a run-local output from its owning CLI or verification/statistics runner. `--include-working-tree` includes tracked edits; add one repeatable `--include-untracked <worktree-relative-path>` for each non-ignored untracked file to include. Unlisted untracked files and ignored files stay excluded, and the CLI writes a normalized, deduplicated, sorted path list.

```bash
bin/formal-gates artifact compose-changed-files \
  --root . --run-dir .claude/gates/runs/RUN_ID \
  --workflow-id WORKFLOW_ID --change-snapshot SNAPSHOT \
  --base-ref BASE --head-ref HEAD --include-working-tree \
  --include-untracked new-file.go \
  --output restricted/changed-files.txt

bin/formal-gates artifact compose-context-bundle \
  --root . --run-dir .claude/gates/runs/RUN_ID \
  --workflow-id WORKFLOW_ID --change-snapshot SNAPSHOT \
  --output restricted/complexity/context-bundle.json \
  --input restricted/requirements/requirements.json \
  --input restricted/changed-files.txt

bin/formal-gates complexity check --task-type <type> --worktree . --vcs auto \
  --run-dir .claude/gates/runs/RUN_ID \
  --workflow-id WORKFLOW_ID --change-snapshot SNAPSHOT \
  --output restricted/complexity/statistics.json

bin/formal-gates prompt prepare --root . \
  --output .claude/gates/runs/RUN_ID/restricted/complexity/prompt.txt \
  --gate complexity-gate \
  --current-requirement openspec/changes/CHANGE \
  --current-diff 'git diff BASE --' \
  --worktree . --change-snapshot SNAPSHOT \
  --review-artifact .claude/gates/runs/RUN_ID/restricted/complexity/review.json \
  --policy-id complexity.post-development.v2 \
  --context-bundle .claude/gates/runs/RUN_ID/restricted/complexity/context-bundle.json

bin/formal-gates receipt register \
  --provider codex --worktree . --run-dir .claude/gates/runs/RUN_ID \
  --context-bundle restricted/complexity/context-bundle.json \
  --prompt restricted/complexity/prompt.txt \
  --changed-files restricted/changed-files.txt \
  --verification restricted/verification.json \
  --complexity-statistics restricted/complexity/statistics.json \
  --artifact .claude/gates/runs/RUN_ID/restricted/complexity/review.json \
  --gate complexity-gate --workflow-id WORKFLOW_ID \
  --change-snapshot SNAPSHOT
```

Send the registered prompt verbatim. The reviewer never edits the assigned JSON; it calls `receipt submit` with one ordered status/message group per generated check and optional finding/location groups. The CLI constructs the complete nested artifact, records the submission proof, and leaves the artifact unchanged when semantic input is incomplete or invalid. After the host records the dispatch lifecycle stop, run `receipt finalize`; finalization rejects any result without the matching CLI submission proof.
