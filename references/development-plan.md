# 开发计划

本版本计划由 start-readiness 的 granularity_review 提交，并由用户对 planDigest 确认。
不再提供 slicing 或 snapshot 兼容命令。完整 typed schema 见
[准备结果与 DevelopmentPlan](engine-regular.md#准备结果覆盖设计与逐项审查)。

```json
{"requirementRevision":"<current revision>","rationale":"One coherent deliverable","batches":[{"id":"B1","task":"Implement the confirmed CLI behavior","subtasks":[{"id":"S1","task":"Implement and verify output"}],"dependencies":[],"interfaces":"Command argv and stdout","stateBoundary":"No persisted state","risks":"Incorrect output","definitionOfDone":"All current AC pass","validation":"Build, project tests and actual CLI oracle","recovery":"Restore original Git baseline","handoff":"No unresolved decisions","contextCost":"One shared implementation context"}]}
```

技术审逐 Batch 给依赖、接口、状态边界、DoD、验证、恢复、交接和上下文成本。Subtask
是批内连续步骤，不另建派发。用户确认 REQUEST(topology, planDigest, reason) 后，再选择
route。Batch 依赖仅指向前序批，按已确认顺序开发；跨 Batch 使用新原生代理，批内续用。

开发经 PREPARATION_RESULT 返回实际 manifest、每个 Subtask 验证、handoff 和状态。
CONTINUE/FAIL/RUNTIME_ERROR 都不解除未完成 Batch 的原生身份；可恢复时必须恢复，只有
真实不可恢复事实才可签发替换。完成 DoD 后由 adapter track/commit/snapshot，才能解锁
下一批。计划契约变化使用 plan-change 并重新技术审、确认；旧记录保留但不冒充新计划完成。
