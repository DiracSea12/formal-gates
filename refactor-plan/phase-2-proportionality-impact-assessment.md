# 阶段 2：封板后小额修复正门成比例性影响评估

## 1. 评估范围

本文件交付阶段 2 需求范围 11，只评估重构完成后的目标架构对“已 SEALED 的交付出现小额
修复时，重新走正式入口是否成比例”的影响。不评估当前 legacy CLI 的便利程度，也不改变
任何 engine、路线、审查或 QA 行为。

事实边界：

- 目标公共写面只有 `workflow start`、无事件的 `workflow drive` 和唯一外部事件写入口
  `workflow submit`；主动控制不是自由写命令，而是从带 freshness 的 `availableActions` 或
  受限 `REQUEST_*` 事件建立 pending Ask，再用 request ID + 当前 freshness token 提交决定
  （总需求 §3.1、§5.14-15；`refactor-plan/final-implementation-draft.md` §2.1-2.3）。
- `availableActions` 由 `next/status` 只读暴露并携 freshness token；submit 还校验 event/request/
  action ID、payload digest、当前节点、Attempt 与 source bindings（总需求 §3.1、§5.14-15；
  同方案 §2.3、§3.4）。
- SEALED/ABORTED 后的新需求必须创建新 run；旧 run 不重开、不迁移、不用旧命令续跑
  （总需求 §7.2.1；`refactor-plan/final-implementation-draft.md` §5、§10）。
- 现有目标文档没有定义新 run 的跨 run 审查结果继承协议。目标规则明确写的是：QA case/test review 首次
  建立基线为 `FULL`，之后新增/修改/ImpactSet 受影响项才为 `AFFECTED`；产品审、技术审和
  普通质量门始终 `FULL`（总需求 §7.2.6；同方案 §5、§7）。因此阶段 1 审计提出的“新 run
  继承已确认需求工件、审查限定 delta”是待评估设想，不能当作已交付或已确认行为。

## 2. 分项影响

| 目标机制 | 对小额修复成本的影响 | 对风险的影响 | 成比例性判断 |
| --- | --- | --- | --- |
| 受限 submit 事件 | 相比自由写命令，主动修复控制须先建立 pending Ask 并取得 request ID/token，再提交 typed 决定；REQUEST 事件路径还多一次 typed request。相同 ID 重试不重复推进，可降低失败重跑成本 | 封闭事件种类、schema/current-node/provider/Attempt/source binding 校验阻止越权、错节点或错 Attempt 写入；同 ID 异 digest 硬拒绝，响应丢失可幂等恢复 | 对单次小修的固定交互成本为负面；对失败重试和审计可靠性为正面。它收紧“怎么写”，不减少新 run 的审查与验证工作量 |
| 带 freshness token 的 `availableActions` | 授权必须基于当前 `next/status` 投影；若 host 尚未持有当前投影就要先读取。并发推进或其他已接纳事件会使旧 token 失效，用户/host 必须重读并重提；活跃并发时重试次数上升 | stale 决定在零状态变化下拒绝，避免把旧节点、旧 requirement revision 或旧 action 的授权应用到新状态 | 低并发且已有当前投影时新增成本较小；高并发时会放大小修的协调成本，但该成本直接对应防止 stale 授权的风险 |
| SEALED 后创建新 run | 产生不可消除的固定成本：重新绑定 base/current/definition/package 身份，重新登记需求/方案和路线，建立本 run 的候选、receipt 与验证基线。现有目标规则不保证跨 run carry；新 run 的 QA 首次基线为 FULL，产品审/技术审/普通门也始终 FULL | 保持 sealed identity、历史 verdict 和 cleanup receipt 不被后续改动污染；修复拥有独立 base、candidate、审查结果与 Seal，可明确区分原交付和修复交付 | 是当前成比例性的主要负担。修复越小，固定成本占比越高；但它同时消除了“重开 sealed run 后旧证据是否仍有效”的高风险歧义 |

## 3. 组合后的典型路径

### 3.1 需求语义不变的单点实现修复

即使改动只有一处，SEALED 后仍创建新 run。旧需求/方案文档可以作为新 run 的输入材料，
但当前目标协议没有授权自动继承旧 run 的确认、review PASS、QA whitelist、candidate 或 gate
结果。受限 submit 与 freshness 主要增加控制交互；真正占主导的固定成本是新 run 身份建立、
首次 FULL QA 基线和始终 FULL 的产品/技术/普通门。

结论：在当前已确认目标规则下，这类修复的正门成本相对改动规模可能明显偏高；不能依据
“语义未变”自行缩成 delta-only review，因为该跨 run 继承规则尚不存在。

### 3.2 修复过程中状态发生并发推进

host 从 `availableActions` 取得 token 后，若其他事件先被接纳，旧决定返回 stale，需重读并
按新状态重新提交。重试本身不会重复 side effect 或污染 state；成本表现为额外往返，而不是
不确定回滚。

结论：对小修的时间成本不利，但与“不得把旧授权应用到新事实”直接对应，风险收益清晰。

### 3.3 提交成功但响应丢失

同 event ID + 同 digest 的 submit 返回稳定 acceptance，不重复落账，也不再次签发已 ISSUED
的 SpawnRequest。外部副作用仍由独立的 intent/receipt/reconcile 协议约束；小修流程虽需遵循
typed event 形态，却减少了人工判断“到底写没写成功”和重复推进的恢复成本。

结论：这是受限 submit 对成比例性的正向部分；异常路径的恢复成本下降，尤其适合低频但
高影响的 Seal/receipt/HostAction 边界。

## 4. 总体结论

- **成本影响：负面且主要是固定成本。** 新 run 的身份、确认与首次验证基线不会随 diff
  变小；受限 submit 和 freshness 另增加少量控制往返。
- **风险影响：显著正面。** sealed 证据不被改写，stale/错节点/重复事件被稳定拒绝，响应
  丢失和恢复具有幂等 receipt。
- **成比例性结论：风险上成比例，工时上未自动成比例。** 对真正小额、低风险修复，当前
  目标架构的完整新 run 固定成本可能超过改动本身；但已确认设计选择优先保存 sealed identity
  与证据边界，尚未给出跨 run delta-only 的受限继承协议。

## 5. 非决策与残留问题

- 本评估不新增“重开 sealed run”、自由写命令、跳过 freshness、公开 cleanup、直接修改状态
  或其他豁免入口。
- lightweight 是“登记后未验证 Seal”的路线，不提供已验证小额代码修复的替代证据，不能由
  本评估扩张成修复捷径。
- 若未来要让工时也按 diff 成比例，需要另行确认跨 run 可继承对象、精确绑定、失效条件、
  首次/后续 `ReviewScopeMode` 和不可继承项。现有需求没有这些规则；在正式变更前，一律按
  新 run 的现行 FULL 基线理解。
