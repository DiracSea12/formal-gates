# 阶段 3.5a：QA 覆盖契约（非独立正式阶段）

状态：计划 checkpoint，尚未执行。

3.5a 是阶段 4 的 Batch A，不新增 `RunPhase`、formal run、Seal 或另一套 QA
状态机。它冻结阶段 4 regular QA 接入前必须具备的覆盖契约，并提供一组可直接验收该
契约的公开 `coverage` 命令。

## 目标与判定边界

逐 case PASS 只能证明这些 case 各自成立，不能证明用例集合覆盖完整。3.5a 要把已确认
需求/方案中的可验证义务、验收点和用例连成有限、可核对的链，再校验设计、审查和执行
是否完整覆盖该链。

覆盖完整只表示：已确认的适用 source、对应验收点和批准用例没有缺项，且用例在当前候选
上得到完整结果。它不证明所有未知缺陷都已被发现，也不以用例数量、代码覆盖率或固定
百分比替代语义判断。

## 本 checkpoint 的 `requiredSources`

下表就是本次已确认需求中的权威交付义务，不是从正文另行提取的 inventory。正文负责解释
这些义务，Controller 只消费表中的稳定 ID 和分类。

| sourceID | 分类 | QA 适用性 | 交付义务 |
|---|---|---|---|
| `REQ-3.5A-001` | 产品需求 | QA | 已确认需求/方案自身保存有限、带稳定 ID 的 `requiredSources`；不得在确认后从自然语言转抄或推断第二份义务清单。 |
| `REQ-3.5A-002` | 产品需求 | QA | source 在确认时只分 QA/非 QA；路线与 topology 确定后由已选 QA kind 的设计者在该 kind 的 `AcceptanceManifest` 中直接记录负责的 source，所有 QA source 至少出现在一个已选 kind 的 manifest 中，非法或过期记录被拒绝。 |
| `REQ-3.5A-003` | 产品需求 | QA | source、point、case 显式双向追踪；一条 source 可对应多个 point/case，多对多合法且不设固定数量，所有已声明分支都进入完成条件。 |
| `REQ-3.5A-004` | 产品需求 | QA | 每个 kind 的 review 逐 manifest 中的 source、point、case 判定并生成不可独立编辑的 approved whitelist；缺失、未知、重复、orphan、矛盾或仅集合级 PASS 被拒绝。 |
| `REQ-3.5A-005` | 产品需求 | QA | execution 对账 expected、实际执行、合法继承和未执行集合，并绑定当前候选及相关 digest；`AUTHORIZED_SKIP` 和无证据旧结果不得充当执行 PASS。 |
| `REQ-3.5A-006` | 产品需求 | QA | 非 QA source 只能凭已确认理由、替代验证的实际 `PASS` 和证据闭环；说明、跳过或非 PASS 状态均不能通过。 |
| `REQ-3.5A-007` | 产品需求 | QA | 用可复现 fixtures 覆盖本文列出的合法路径和拒绝路径；3.5a 只冻结契约与 validator，regular E2E 留给阶段 4。 |
| `REQ-3.5A-008` | 产品需求 | QA | 3.5a 必须提供文档化的公开入口：`formal-gates coverage validate`、`formal-gates coverage project-whitelist` 和 `formal-gates coverage reconcile-execution`；入口使用 JSON 输入/输出，正常成功与常见校验失败都能从已构建的 `formal-gates` 二进制观察。 |

## 最小契约

### 1. `AcceptanceManifest`

已确认需求/方案自身以有限 `requiredSources` 作为唯一权威交付义务列表，不在确认后再从
自然语言另行转抄第二份 inventory。每项用稳定 `sourceID` 表达一条产品需求或已确认的方案
约束；“用户已确认”“技术审已通过”等流程状态不得成为 source。主代理在受理时直接编写
这些 source，用户随完整需求和方案一并确认，产品审负责语义核对列表是否承载用户意图。
Controller 只消费这份已确认列表，不自行解释自然语言或增删 source。

每条 source 在确认时只区分“需要 QA”或“非 QA 验证”，不预先指定黑盒、白盒或合并 QA。
路线与 topology 确定后，每个已选 QA kind 的设计者在该 kind 的 `AcceptanceManifest` 中直接
记录它负责的 source，并产生有限验收点集合 `U`。Controller 聚合所有已选 kind 的 manifest；
每个需要 QA 的 source 必须至少出现在一个已选 kind 的 manifest 中，非 QA source 不得出现在
任何 QA manifest。route 或 topology scope 改变时，相关 manifest、映射和下游结果失效。

每个验收点有稳定 `pointID`，并记录可观察行为和 oracle。一条 source 可以对应多个 point，
一个 point 可以对应多个 case；复杂 source 的一对多或多对多覆盖是正常形态，不设固定数量
或上限。一个 source 出现在多个 kind 的 manifest 时，各 kind 已声明的 point/case 都必须完成
并通过。

每个适用 `sourceID` 必须映射至少一个 `pointID`；确实不由 QA 验证的 source 必须显式记录
理由、替代验证方式、`PASS` 结果和证据绑定。只有说明、`PENDING`、`UNKNOWN`、`FAIL` 或
`AUTHORIZED_SKIP` 均不构成有效的非 QA 验证。每个 point 也必须回指至少一个 source。
QA agent 只能消费已确认的 `requiredSources`，不能自行增删 source；需求/方案 revision 改变
时 `requiredSources` binding 及全部下游绑定失效。

“无适用 QA 点”必须通过上述非 QA 验证处置闭环。用户授权的跳过保留为
`AUTHORIZED_SKIP`，不等同于验证通过。

### 2. source ↔ point ↔ case 双向映射

每个 QA kind 的 `sourceID`、`pointID` 与 approved case 使用一份显式映射：

- 每个需要 QA 的 source 至少出现在一个已选 kind 的 manifest；manifest 中每个 source 至少映射一个 point；
- 非 QA source 只允许有效的非 QA 验证处置，不进入 QA kind 图；
- 每个 point 至少归属一个适用 source；
- 每个适用 point 至少映射一个 approved case；
- 每个 approved case 至少归属一个适用 point；
- 黑盒与合并 QA case 必须给出文档化的公开入口、前置条件、可执行步骤和可观察 oracle；3.5a
  的覆盖契约 case 使用上述 `coverage` 命令，不依赖模块外临时 Go driver 或未文档化命令；
- 白盒 case 必须给出唯一测试引用、运行方式和可观察 oracle；两类执行绑定使用封闭变体，
  不允许携带不匹配的字段组合；
- 未覆盖 source/point、无归属 point/case、未知 ID、重复 ID/edge 或 scope 错位均拒绝。

`requiredSources` binding 绑定 run/child 与需求/方案 revision；每个 kind 的 manifest 与映射另绑定
review kind、route/topology scope。正向和反向查询都从这份映射派生，不维护第二份关系。`FULL`/`AFFECTED` 只描述本轮 review/execution
范围，不允许把旧候选结果当成当前候选结果。

Controller 的覆盖判定按固定顺序执行：遍历全部 `requiredSources`，检查每个 QA source 是否出现在
至少一个已选 kind 的 manifest；再遍历每个 manifest source 的全部 point、每个 point 的全部
case，以及每个 case 反向指向的 point/source。任一层为空、未知、重复、孤立或未通过，整体覆盖
即拒绝；不能用一个总体 PASS 代替逐层结果。它只核对已确认的 source ID，不从自然语言猜测未登记的
需求。

### 3. review 完整性

每个 kind 的 QA review 同时返回逐 manifest source、逐 point 和逐 case 决策，并列出未绑定
source/point/case。本轮 kind/scope 中每个 QA 对象恰好出现一次；未知、重复、缺失、`FAIL`、
`PENDING` 或 `UNKNOWN` 均不得进入 approved whitelist。一个适用 source 只有在全部适用
point 通过时才通过；一个 point 只有在自身 review 为 `PASS` 且全部映射 case 均获批准时
才通过。非 QA source 不交给 QA reviewer，其结果由 Controller 从已确认分类和有效的
`PASS` 替代证据投影。仅有集合级 PASS 不构成覆盖完整证据。

approved whitelist 由 `requiredSources`、manifest、映射、review 结果和非 QA 验证结果投影
生成，不维护第二份可独立编辑的清单。聚合覆盖只有在每个需要 QA 的 source 至少出现在一个已选
kind 的 manifest、且所有出现该 source 的 kind 分支都通过时才成立。白盒 review 继续负责测试代码质量
和实现引用；结构校验不能替代逐 source/point 的语义审查。

### 4. execution 完整性与候选绑定

执行前冻结 expected case ID set；执行结果必须给出实际 case ID set，并按
`sourceID → pointID → caseID → result` 展开。`FULL` 必须覆盖全部 expected case；
`AFFECTED` 必须明确列出本轮执行、带有效继承证据的结果和未执行条目。一个 point 只有在
全部映射 case 对当前
`ValidationCandidate` 均为 `EXECUTED_PASS` 时才是 execution PASS。

`requiredSources`/manifest/map/whitelist digest、候选 identity 或 expected/actual case set
不匹配时，旧结果失效；缺失、额外、无有效继承证据、`FAIL`、`PENDING`、`UNKNOWN` 和
`AUTHORIZED_SKIP` 均不得伪装成 `EXECUTED_PASS`。

## 验收 fixtures

逐项覆盖：

- 合法的 source→point→case 多对多映射；
- 一条复杂 source 合法映射多个 point/case，且不设置固定数量；
- 权威 `requiredSources` 中的 QA source 未出现在任何已选 QA kind 的 manifest、未知、重复、缺失和 orphan
  source/point/case；
- 合法 ID 但 run/child、review kind、需求/方案 revision、route 或 topology scope 错位；
- 单 kind 与多 kind manifest 覆盖、未选 kind manifest、非 QA source 出现在 QA manifest，以及
  route/topology 改变使旧 manifest 失效；
- 黑盒/合并 QA 与白盒执行绑定合法，错配字段组合被拒绝；
- 仅集合级 PASS、source/point/case 决策不完整或互相矛盾；
- `requiredSources`、manifest、map、whitelist digest 或候选 identity 任一变化使旧结果失效；
- `FULL` expected/actual 不一致，以及 `AFFECTED` 的执行/继承/未执行集合不完整；
- “无适用 QA 点”的已确认分类、理由、替代验证 `PASS` 与证据绑定；
- `AUTHORIZED_SKIP` 不计为验证或 execution PASS。

完整 regular E2E、真实宿主和 VCS canary 留在阶段 4，不在 3.5a 重复实现。

## 范围和止损

- 只覆盖文档化的正常使用和常见误操作；其余边界继续服从
  `prompts/reviewer-base.md`。
- 3.5a 交付 typed schema、validator、digest/whitelist 投影、fixtures，以及一个只做 JSON
  编解码和函数转发的薄 `coverage` CLI 适配层；不改 legacy `QACase` 完整结构，不新增
  独立 source registry、数据库、事件溯源或通用测试框架；`requiredSources` 就是现有已确认
  需求/方案工件中的权威交付义务列表，不另建第二份 source 清单或自然语言解析器。
- 实现保持解耦、简洁和最小充分：覆盖契约核心仍是纯逻辑包，CLI 只负责公开验收入口，不接入
  engine 状态机或阶段 4 regular QA 流程，不为未来可能性增加抽象层。
- 代码覆盖率、branch coverage、mutation、property-based test 和 fuzz 只可作为发现薄弱点的
  辅助信号；不设固定用例数、固定百分比或强制工具组合，也不把这些指标当成语义覆盖证明。

Checkpoint 完成条件是：上述契约、结构校验、fixtures 和公开 `coverage` 命令可复现通过，并在阶段 4 任务书中
登记 regular QA/candidate 接入边界；不得表述为已完成阶段 4 或已完成 QA 全流程。
