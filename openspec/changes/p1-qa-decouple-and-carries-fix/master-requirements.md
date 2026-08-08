# 需求：P1 QA 彻底解耦与 Carry 继承修复

## 背景

对项目功能与全流程暴力黑盒测试 + 全量文件穷尽审查后，确认两个 P1 真实运行缺陷
（正常操作可达），以及两个提示词重复维护问题。本 change 修复全部 P1，P2 仅文档化。

## 需求项

### RQ-001：QA review/design 动作彻底解耦（P1-A）

黑盒与白盒两个 QA mode 的 review 与 design 权威结果必须**完完全全解耦**，
不再共享单一动作状态。具体：

- `qa-review` 的权威结果（PASS/FAIL/RUNTIME_ERROR）按 mode（blackbox/whitebox）
  独立存储、独立判定；一个 mode 记录 review 结果 SHALL NOT 使另一 mode 的
  review 判定受影响。
- `qa-design` 的权威结果按 mode 独立存储；一个 mode 的 review FAIL 重置设计
  SHALL NOT 把另一 mode 的设计重置为 PENDING。
- 任一 mode 的 `prepare-action qa-review` 只受本 mode 的 review 状态约束；
  两个 mode 都完成设计后，可分别独立记录各自的 review，顺序不限。
- 快照黑盒门、`blackboxReviewPassed`、并行提示、record 校验等所有读
  `Actions["qa-review"]` / `Actions["qa-design"]` 的路径，全部改为按 mode 取。
- **不兼容旧 run**：不添加旧状态文件迁移、不保留仅为旧状态服务的合并 `""` 回退
  兼容路径。旧 run 状态文件不再适配。

### RQ-002：carry --main-agent 继承修复前 PASS 的 QA mode（P1-B）

`carry --main-agent` 必须能继承修复快照（pre-repair）之前已 PASS 的 QA mode，
与文档化行为一致（formal-flow 第 272-274 行、example-run 第 206-208 行）：

- 修复快照推进后，`eligibleMainCarryResults` 对 QA mode 直接取该 mode 的执行结果
  （`state.qaExecution(mode)`），不再经过只返回 current-snapshot 结果的
  `qaModeResult`/`qaModeResultKey`。
- 判定仍按 `PreRepairSnapshot` 匹配或 catalogChanged 分支；legacy 合并流程行为不变。
- 修复后，凡可能受本次改动影响的既有用例，由主代理判定受影响子集，
  用 `qa-execution-scope --decision AFFECTED --cases ...` 只重跑受影响用例，
  未受影响且已 PASS 的用例保持 PASS 继承。

### RQ-003：「任务完整性检查」块去重（P1-C）

- `ComposeActionPrompt` 像 `ComposeGatePrompt` 一样注入 `[Shared reviewer contract]`
  + reviewer-base 契约；从 product-review、qa-design、qa-review、start-readiness
  四个派发动作提示词删除内联的重复块，reviewer-base 为唯一本体。
- `requirements-clarification` 由主代理交互执行、不是独立审查者，不注入、单独斟酌。

### RQ-004：formal-flow 与 SKILL 机制重述去重（P1-D）

- formal-flow.md 删除对 SKILL 规则的机制重述（复审规则、快照要求、squash、QA scope、
  隔离工作区），只保留命令形式与一句引用；清内部编号残留（「问题 6」/RQ-014 等）。
- SKILL.md 第 4 步与 formal-flow 复审规则重复按 deadlock 需求 6 收敛。

### RQ-005：P2 文档化（不修）

- 根目录新建 `P2-BACKLOG.md`，记录全部 P2 发现项（位置+建议），加入 `.gitignore`
  不跟踪。本次不实现任何 P2。

## 非目标

- 不修复任何 P2 项（仅文档化）。
- 不做旧 run 状态兼容迁移。
- 不引入新的 QA mode、门或机制。
