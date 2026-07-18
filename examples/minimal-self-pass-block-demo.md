# 最小自报 PASS 阻断示例

聊天结论、Markdown 标签和开发者自报都不能产生正式 PASS。独立 QA 执行者先产出完整结果和 case binding，主代理只写包含批准 case、Design Review 闭包、QA 结果、binding、changed files 和 verification 六个引用的 schema-version-2 `QA_EXECUTION`；workflow 命令在原子替换状态前校验完整批准链、hash、snapshot 和证据闭包。

```bash
formal-gates artifact validate --root . --file .claude/gates/runs/wf/restricted/qa-execution.json \
  --gate qa-test-gate --stage Execution --workflow-id wf --change-snapshot snapshot

formal-gates workflow record-stage --worktree . --run-dir .claude/gates/runs/wf \
  --gate qa-test-gate --stage Execution --mode formal --verdict PASS \
  --artifact .claude/gates/runs/wf/restricted/qa-execution.json \
  --workflow-id wf --change-snapshot snapshot
```

缺少 Design 或 Design Review receipt、case hash 改变、缺少 QA 自有结果、case 覆盖不完整、结果失败、binding 错误、路径或 hash 不对、snapshot 过期、schema version 1、未知字段或仅有 Markdown 时，命令拒绝且不改变权威状态。QA Execution 不需要第二个 reviewer 或 receipt。
