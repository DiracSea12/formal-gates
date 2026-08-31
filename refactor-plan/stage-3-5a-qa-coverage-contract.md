# 阶段 3.5a：QA 覆盖契约（非独立正式阶段）

状态：计划 checkpoint，尚未执行。

3.5a 是阶段 4 的 Batch A，不新增 `RunPhase`、formal run、Seal 或另一套 QA
状态机。它只冻结阶段 4 regular QA 接入前必须具备的覆盖契约。

## 目标与判定边界

逐 case PASS 只能证明这些 case 各自成立，不能证明用例集合覆盖完整。3.5a 要把已确认
需求/方案中的可验证义务、验收点和用例连成有限、可核对的链，再校验设计、审查和执行
是否完整覆盖该链。

覆盖完整只表示：已确认的适用 source、对应验收点和批准用例没有缺项，且用例在当前候选
上得到完整结果。它不证明所有未知缺陷都已被发现，也不以用例数量、代码覆盖率或固定
百分比替代语义判断。

## 最小契约

### 1. `AcceptanceManifest`

已确认需求/方案在原有受理确认中一并登记有限 `requiredSources`；每项用稳定 `sourceID`
指向一条实际交付义务，类型只能是产品需求或已确认的方案约束，不登记“用户已确认”、
“技术审已通过”等流程状态。主代理提出每条 source 是否由 QA 验证及理由，用户随完整需求
和方案一并确认；QA agent 和 Controller 均不得事后自行改变分类。

Controller 在 QA 设计前，从该 inventory、需求 revision、方案 revision、route 和 topology
scope 冻结本轮 `AcceptanceManifest` 与有限验收点集合 `U`。每个适用验收点有稳定
`pointID`，并记录可观察行为和 oracle。

每个适用 `sourceID` 必须映射至少一个 `pointID`；确实不由 QA 验证的 source 必须显式记录
理由、替代验证方式、`PASS` 结果和证据绑定。只有说明、`PENDING`、`UNKNOWN`、`FAIL` 或
`AUTHORIZED_SKIP` 均不构成有效的非 QA 验证。每个 point 也必须回指至少一个 source。
QA agent 只能消费已确认的 source inventory 和 manifest，不能自行增删；需求/方案 revision、
route 或 topology scope 改变时创建新绑定，旧 manifest、映射和结果失效。

“无适用 QA 点”必须通过上述非 QA 验证处置闭环。用户授权的跳过保留为
`AUTHORIZED_SKIP`，不等同于验证通过。

### 2. source ↔ point ↔ case 双向映射

`sourceID`、`pointID` 与 approved case 使用显式映射：

- 每个适用 source 至少映射一个 point，或具有有效的非 QA 验证处置；
- 每个 point 至少归属一个适用 source；
- 每个适用 point 至少映射一个 approved case；
- 每个 approved case 至少归属一个适用 point；
- 黑盒与合并 QA case 必须给出文档化的公开入口、前置条件、可执行步骤和可观察 oracle；
- 白盒 case 必须给出唯一测试引用、运行方式和可观察 oracle；两类执行绑定使用封闭变体，
  不允许携带不匹配的字段组合；
- 未覆盖 source/point、无归属 point/case、未知 ID、重复 ID/edge 或 scope 错位均拒绝。

source inventory、manifest 与映射共同绑定 run/child、review kind、需求/方案 revision、
route/topology scope。`FULL`/`AFFECTED` 只描述本轮 review/execution 范围，不允许把旧候选
结果当成当前候选结果。

### 3. review 完整性

QA review 同时返回逐适用 source、逐 point 和逐 case 决策，并列出未绑定
source/point/case。本轮 scope 中每个 QA 对象恰好出现一次；未知、重复、缺失、`FAIL`、
`PENDING` 或 `UNKNOWN` 均不得进入 approved whitelist。一个适用 source 只有在全部适用
point 通过时才通过；一个 point 只有在自身 review 为 `PASS` 且全部映射 case 均获批准时
才通过。非 QA source 不交给 QA reviewer，其结果由 Controller 从已确认分类和有效的
`PASS` 替代证据投影。仅有集合级 PASS 不构成覆盖完整证据。

approved whitelist 由 source inventory、manifest、映射、review 结果和非 QA 验证结果投影
生成，不维护第二份可独立编辑的清单。白盒 review 继续负责测试代码质量和实现引用；结构
inventory 不能替代逐 source/point 的语义审查。

### 4. execution 完整性与候选绑定

执行前冻结 expected case ID set；执行结果必须给出实际 case ID set，并按
`sourceID → pointID → caseID → result` 展开。`FULL` 必须覆盖全部 expected case；
`AFFECTED` 必须明确列出本轮执行、带有效继承证据的结果和未执行条目。一个 point 只有在
全部映射 case 对当前
`ValidationCandidate` 均为 `EXECUTED_PASS` 时才是 execution PASS。

source inventory/manifest/map/whitelist digest、候选 identity 或 expected/actual case set
不匹配时，旧结果失效；缺失、额外、无有效继承证据、`FAIL`、`PENDING`、`UNKNOWN` 和
`AUTHORIZED_SKIP` 均不得伪装成 `EXECUTED_PASS`。

## 验收 fixtures

逐项覆盖：

- 合法的 source→point→case 多对多映射；
- 未覆盖、未知、重复、缺失和 orphan source/point/case；
- 合法 ID 但 run/child、review kind、需求/方案 revision、route 或 topology scope 错位；
- 黑盒/合并 QA 与白盒执行绑定合法，错配字段组合被拒绝；
- 仅集合级 PASS、source/point/case 决策不完整或互相矛盾；
- source inventory、manifest、map、whitelist digest 或候选 identity 任一变化使旧结果失效；
- `FULL` expected/actual 不一致，以及 `AFFECTED` 的执行/继承/未执行集合不完整；
- “无适用 QA 点”的已确认分类、理由、替代验证 `PASS` 与证据绑定；
- `AUTHORIZED_SKIP` 不计为验证或 execution PASS。

完整 regular E2E、真实宿主和 VCS canary 留在阶段 4，不在 3.5a 重复实现。

## 范围和止损

- 只覆盖文档化的正常使用和常见误操作；其余边界继续服从
  `prompts/reviewer-base.md`。
- 3.5a 只交付 typed schema、validator、digest/whitelist 投影和 fixtures；不改 legacy
  `QACase` 完整结构，不新增公开命令、独立 source registry、数据库、事件溯源或通用测试
  框架；`requiredSources` 是现有已确认需求/方案工件的一部分。
- 实现保持解耦、简洁和最小充分：纯逻辑契约不接入 engine 状态机、CLI、I/O 或阶段 4
  regular QA 流程，不为未来可能性增加抽象层。
- 代码覆盖率、branch coverage、mutation、property-based test 和 fuzz 只可作为发现薄弱点的
  辅助信号；不设固定用例数、固定百分比或强制工具组合，也不把这些指标当成语义覆盖证明。

Checkpoint 完成条件是：上述契约、结构校验和 fixtures 可复现通过，并在阶段 4 任务书中
登记 regular QA/candidate 接入边界；不得表述为已完成阶段 4 或已完成 QA 全流程。
