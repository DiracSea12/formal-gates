# Sample Complexity Gate Artifact

Sample-only: this file is not a formal PASS artifact. Replace every placeholder with project-specific evidence, and ensure the context bundle path exists on disk before recording gate state.

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
Script result: <complexity_gate.py result or explicit not-applicable reason>
Diff shape judgment: <focused diff shape judgment>
Impact surface health: <public/runtime/config impact assessment>
Public/config surface: <none or exact public/config changes>
New concepts: <none or exact concepts introduced>
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
