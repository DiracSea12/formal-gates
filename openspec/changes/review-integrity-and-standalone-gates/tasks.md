# Tasks

- [ ] 需求 1（污染检查）：`prompts/reviewer-base.md` 增加"第一步任务完整性检查"（允许块清
  单、块外即污染、RUNTIME_ERROR + 拒绝原因、`[Action input]` 合法输入）。
- [ ] 需求 1：五个行动审查提示词文件（qa-design / qa-review / product-review /
  start-readiness / requirements-clarification）加入同一完整性检查。
- [ ] 需求 2：新增 `pendingInheritance` 谓词与 `requireNoPendingInheritance` 硬闸，挂到
  全部继续 / 重跑入口；收敛现有零散守卫；`carry` / `requirement` / `settle-findings` 豁
  免。
- [ ] 需求 3：`prepareBoundPrompt` 加续用守卫（同一职责 + 快照不变 + 任务不变 + 无用户授
  权 → 拒绝新派发并指示恢复）；`--user-requested` 授权放行。
- [ ] 需求 4：新增 `gate run <ids...>` 命令（工作树 vs HEAD、无需求、轻量零上下文、结果仅
  展示）。
- [ ] 测试：上述四项的单元 / CLI 集成测试（状态机路径：正常流程、修复轮、采纳、中断续跑、
  单跑）。
- [ ] CHANGELOG。
