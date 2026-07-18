# Complexity Review JSON

Sample-only: this abbreviated document is not a formal PASS artifact.

The reviewer writes a schema-version-2 JSON file directly. Replace every evidence path and hash with a run-local verified value and include all checks exported for the selected policy.

```json
{
  "schemaVersion": 2,
  "artifactRole": "COMPLEXITY_REVIEW",
  "workflowId": "workflow-id",
  "changeSnapshot": "snapshot-id",
  "gate": "complexity-gate",
  "stage": "",
  "verdict": "PASS",
  "payload": {
    "contextBundle": {"path": "context-bundle.json", "sha256": "<lowercase-sha256>"},
    "reviewPolicyId": "complexity.post-development.v2",
    "checks": [
      {"id": "review.prompt-fields", "status": "PASS", "message": "checked", "evidenceRefs": [], "findings": []},
      {"id": "review.prompt-semantics", "status": "PASS", "message": "checked", "evidenceRefs": [], "findings": []},
      {"id": "complexity.statistics", "status": "PASS", "message": "fresh statistics", "evidenceRefs": [{"path": "statistics.json", "sha256": "<lowercase-sha256>"}], "findings": []}
    ],
    "changedFiles": {"path": "changed-files.txt", "sha256": "<lowercase-sha256>"},
    "verification": {"path": "verification.txt", "sha256": "<lowercase-sha256>"}
  }
}
```

The abbreviated check list illustrates placement only. A recordable artifact contains every required `complexity.*` check exactly once. The exact final-send prompt is not reviewer payload evidence; `receipt register --prompt` binds it externally and the receipt/closure validation chain revalidates its bytes.
