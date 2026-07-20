# 最小自报 PASS 阻断示例

聊天结论、Markdown 标签和开发者自报都不能产生正式 PASS。独立 QA 执行者按批准 case 的 1-based 位置只提交 outcome、procedure、observation 和 oracle result 标量；CLI 生成 Case/Execution ID、QA-owned results、case binding 和引用批准 case、Design Review 闭包、changed files、verification 的完整 `QA_EXECUTION`。workflow 命令在原子替换状态前校验完整批准链、hash、snapshot 和证据闭包。

```bash
formal-gates artifact compose-qa-owned-evidence --root . \
  --run-dir .claude/gates/runs/wf --workflow-id wf \
  --change-snapshot snapshot --approved-case-set restricted/qa-cases.md \
  --case 1 --outcome PASS \
  --procedure '<执行步骤>' --observation '<观察结果>' \
  --oracle-result '<oracle 判断>' \
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

缺少 Design 或 Design Review receipt、case hash 改变、缺少 QA 自有结果、case 覆盖不完整、结果失败、binding 错误、路径或 hash 不对、snapshot 过期、未知字段或仅有 Markdown 时，命令拒绝且不改变权威状态。QA Execution 不需要第二个 reviewer 或 receipt。
