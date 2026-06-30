# Sample Complexity Gate Artifact

Sample-only: this file is not a formal PASS artifact.

Do not record this file directly with `formal-gates gate record` or `formal-gates workflow record-stage`. First replace every `<...>` placeholder with project-specific evidence, point every artifact path at a real file, and ensure the context bundle exists on disk. Validators are expected to reject placeholder-filled copies.

Gate: complexity-gate
Verdict: PASS
Mode: formal
Workflow id: <workflow-id>
Change snapshot: <snapshot-id>
OpenSpec change: <change-name>
Worktree: <project>
Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id: <independent-reviewer-id>
Context bundle: <project>/.claude/gates/context/<workflow-id>-bundle.zip sha256=<bundle-sha256>
Dispatch prompt artifact: <project>/.claude/gates/artifacts/<workflow-id>-dispatch-prompt.md sha256=<dispatch-prompt-sha256>
No-anchor prompt: YES
Script result: <formal-gates complexity check result or explicit not-applicable reason>
Diff shape judgment: <focused diff shape judgment>
Budget/expansion status: <development-time budget history and whether independent expansion approval was used>
Budget expansion approval: <only when expansion was approved: artifact path sha256=<approval-sha256>>
Impact surface health: <public/runtime/config impact assessment>
Public/config surface: <none or exact public/config changes>
New concepts: <none or exact concepts introduced>
Minimum sufficient implementation: <why this is the smallest sufficient implementation, or remaining concern>
Shrink opportunities: <none or exact simplification candidates>
Decision evidence: <artifact paths and commands reviewed>
Changed files artifact: <project>/.claude/gates/artifacts/<workflow-id>-changed-files.txt
Verification artifact: <project>/.claude/gates/artifacts/<workflow-id>-developer-self-test.txt

gate_route:
  workflow_id: "<workflow-id>"
  change_snapshot: "<snapshot-id>"
  next_action: proceed
  rework_owner: none
  rerun_from: none
