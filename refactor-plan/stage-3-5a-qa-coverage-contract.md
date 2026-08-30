# 阶段 3.5a：QA 覆盖契约（非独立正式阶段）

状态：计划 checkpoint，尚未执行。

3.5a 是阶段 4 的 Batch A，不新增 `RunPhase`、formal run、Seal 或另一套 QA
状态机。它只冻结阶段 4 regular QA 接入前必须具备的覆盖契约。

## 目标与判定边界

逐 case PASS 只能证明这些 case 各自成立，不能证明用例集合覆盖完整。3.5a 要把“当前
范围内必须验证什么”变成有限、可核对的验收点集合，再校验设计、审查和执行是否完整
覆盖该集合。

覆盖完整只表示：已确认需求/方案登记的适用验收点没有缺项，且其批准用例在当前候选上
得到完整结果。它不证明所有未知缺陷都已被发现，也不以用例数量、代码覆盖率或固定百分比
替代语义判断。

## 最小契约

### 1. `AcceptanceManifest`

Controller 在 QA 设计前，从已确认的需求 revision、route 和 topology scope 冻结本轮有限
验收点集合 `U`。每个适用验收点有稳定 `pointID`，并记录来源、可观察行为和 oracle。
QA agent 只能消费该 manifest，不能自行增删验收点；需求 revision 或 scope 改变时创建新
manifest，旧绑定失效。

“无适用 QA 点”必须显式记录理由及对应的非 QA 验证方式。用户授权的跳过保留为
`AUTHORIZED_SKIP`，不等同于执行通过。

### 2. case ↔ point 双向映射

approved case 与 `pointID` 使用显式多对多映射：

- 每个适用 point 至少映射一个 approved case；
- 每个 approved case 至少归属一个适用 point；
- case 必须给出可执行的公开入口、前置条件和可观察 oracle；
- 未覆盖 point、无归属 case、未知 ID、重复 ID/edge 或 scope 错位均拒绝。

映射绑定 run/child、review kind、需求 revision、route/topology scope。`FULL`/`AFFECTED`
只描述本轮 review/execution 范围，不允许把旧候选结果当成当前候选结果。

### 3. review 完整性

review 同时返回逐 case 决策和逐 point `PointReviewDecision`，并列出未绑定 point/case。
本轮 scope 中每个对象恰好出现一次；未知、重复、缺失、`FAIL`、`PENDING` 或 `UNKNOWN`
均不得进入 approved whitelist。一个 point 只有在自身 review 为 `PASS` 且全部映射 case
均获批准时才通过。仅有集合级 PASS 不构成覆盖完整证据。

approved whitelist 由 manifest、映射和 review 结果投影生成，不维护第二份可独立编辑的
清单。白盒 review 继续负责测试代码质量和实现引用；结构 inventory 不能替代逐验收点的
语义审查。

### 4. execution 完整性与候选绑定

执行前冻结 expected case ID set；执行结果必须给出实际 case ID set，并按
`pointID → caseID → result` 展开。`FULL` 必须覆盖全部 expected case；`AFFECTED` 必须明确
列出本轮执行、合法继承和未执行条目。一个 point 只有在全部映射 case 对当前
`ValidationCandidate` 均为 `EXECUTED_PASS` 时才是 execution PASS。

manifest/map/whitelist digest、候选 identity 或 expected/actual case set 不匹配时，旧结果
失效；缺失、额外、`FAIL`、`PENDING`、`UNKNOWN` 和 `AUTHORIZED_SKIP` 均不得伪装成
`EXECUTED_PASS`。

## 验收 fixtures

逐项覆盖：

- 合法多对多映射；
- 未覆盖、未知、重复、缺失和 orphan point/case；
- 合法 ID 但 run/child、review kind、需求 revision、route 或 topology scope 错位；
- 仅集合级 PASS、point/case 决策不完整或互相矛盾；
- manifest、map、whitelist digest 或候选 identity 任一变化使旧结果失效；
- `FULL` expected/actual 不一致，以及 `AFFECTED` 的执行/继承/未执行集合不完整；
- “无适用 QA 点”的显式理由；
- `AUTHORIZED_SKIP` 不计为 execution PASS。

完整 regular E2E、真实宿主和 VCS canary 留在阶段 4，不在 3.5a 重复实现。

## 范围和止损

- 只覆盖文档化的正常使用和常见误操作；其余边界继续服从
  `prompts/reviewer-base.md`。
- 3.5a 只交付 typed schema、validator、digest/whitelist 投影和 fixtures；不改 legacy
  `QACase` 完整结构，不新增公开命令、数据库、事件溯源或通用测试框架。
- 代码覆盖率、branch coverage、mutation、property-based test 和 fuzz 只可作为发现薄弱点的
  辅助信号；不设固定用例数、固定百分比或强制工具组合，也不把这些指标当成语义覆盖证明。

Checkpoint 完成条件是：上述契约、结构校验和 fixtures 可复现通过，并在阶段 4 任务书中
登记 regular QA/candidate 接入边界；不得表述为已完成阶段 4 或已完成 QA 全流程。
