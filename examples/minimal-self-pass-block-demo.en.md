# Minimal self-PASS block demo

Formal PASS cannot be created from chat, Markdown labels, or a developer claim. An independent QA executor supplies only the approved case's 1-based position plus semantic outcome, procedure, observation, and oracle result scalars. The CLI generates Case and Execution IDs, QA-owned results, case binding, and the complete `QA_EXECUTION` that references the approved cases, Design Review closure, changed files, and verification. The workflow command validates the approved chain, hashes, snapshot, and evidence closure before state replacement.

```bash
formal-gates artifact compose-qa-owned-evidence --root . \
  --run-dir .claude/gates/runs/wf --workflow-id wf \
  --change-snapshot snapshot --approved-case-set restricted/qa-cases.md \
  --case 1 --outcome PASS \
  --procedure '<procedure>' --observation '<observation>' \
  --oracle-result '<oracle-result>' \
  --output-dir restricted/qa-execution

formal-gates artifact compose-qa-execution --root . \
  --run-dir .claude/gates/runs/wf --workflow-id wf \
  --change-snapshot snapshot --output restricted/qa-execution.json \
  --approved-case-set restricted/qa-cases.md \
  --design-review restricted/closures/design-review.json \
  --qa-owned-results restricted/qa-execution/qa-results.json \
  --case-result-binding restricted/qa-execution/case-result-binding.json \
  --changed-files restricted/changed-files.txt \
  --verification restricted/verification.json

formal-gates artifact validate --root . --file .claude/gates/runs/wf/restricted/qa-execution.json \
  --gate qa-test-gate --stage Execution --workflow-id wf --change-snapshot snapshot

formal-gates workflow record-stage --worktree . --run-dir .claude/gates/runs/wf \
  --gate qa-test-gate --stage Execution --mode formal --verdict PASS \
  --artifact .claude/gates/runs/wf/restricted/qa-execution.json \
  --workflow-id wf --change-snapshot snapshot
```

The command rejects missing Design or Design Review receipts, changed case hashes, missing QA-owned results, incomplete case coverage, failed results, wrong bindings, unsafe paths or hashes, stale snapshots, unknown fields, and Markdown-only evidence without changing authoritative state. QA Execution does not require a second reviewer or receipt.
