# Minimal self-PASS block demo

Formal PASS cannot be created from chat, Markdown labels, or a developer claim. An independent QA executor first writes complete results and case binding; the main agent writes only the five-reference schema-version-2 `QA_EXECUTION` envelope. The workflow command validates approved cases, QA results, binding, hashes, snapshot, and evidence closure before state replacement.

```bash
formal-gates artifact validate --root . --file .claude/gates/runs/wf/qa-execution.json \
  --gate qa-test-gate --stage Execution --workflow-id wf --change-snapshot snapshot

formal-gates workflow record-stage --worktree . --run-dir .claude/gates/runs/wf \
  --gate qa-test-gate --stage Execution --mode formal --verdict PASS \
  --artifact .claude/gates/runs/wf/qa-execution.json \
  --workflow-id wf --change-snapshot snapshot
```

The command rejects missing approved cases or QA-owned results, incomplete case coverage, failed results, wrong bindings, unsafe paths or hashes, stale snapshots, schema version 1, unknown fields, and Markdown-only evidence without changing authoritative state. QA Execution does not require a second reviewer or receipt.
