# 阶段 3.5a：QA 覆盖契约（非独立正式阶段）

状态：计划 checkpoint，尚未执行。

本 checkpoint 属于阶段 4 的 Batch A，不计入增量计划的八个正式阶段，不新增
`RunPhase`、formal run、Seal 或另一套 QA 状态机。总方案的阶段编号不因此改变；
这里只冻结阶段 4 接入时必须消费的公共契约。

## 目标

阻止“用例很少但集合级 PASS”或 reviewer 漏看验收点而进入批准白名单。契约只解决
覆盖可追踪性和结果完整性，不以用例数量、百分比或一对一比例替代语义判断。

## 最小契约

### 1. 验收点清单

每个需求 revision、route 和 topology scope 在 QA 设计前冻结一个
`AcceptanceManifest`。Controller 从已确认的需求/方案工件登记并冻结该 typed manifest；已
冻结实例不可解冻或就地改写，只有需求 revision 或 scope 变化时由 Controller 创建新的
manifest。QA agent 只能消费，不能创建或改写。每个适用验收点必须有稳定 `pointID`，并记录来源、
可观察行为、oracle 以及正常使用/常见误操作范围。清单是已确认需求/方案工件的一部分；
本 checkpoint 不解析任意自然语言文档，也不另建一套需求权威源。

清单允许明确的“无适用 QA 点”结果，但必须留下理由和对应的非 QA 验证方式；用户
已授权的 skip 仍可存在，状态是 `AUTHORIZED_SKIP`，不能伪装成
`EXECUTED_PASS`。

### 2. case ↔ point 映射

QA case 通过显式映射引用一个或多个 `pointID`，一个验收点也可以由多个 case 覆盖；
映射中的 case 必须是当前 scope 已批准的 case。映射按 `run/child`、review kind、需求
revision、route/topology scope 绑定，不得依赖 agent 的口头声明。任何 FULL/AFFECTED 标记若仍由旧入口产生，只能表示本轮任务分组，
不能把旧 `ValidationCandidate` 的结果带入新候选；候选改变时按当前候选重新执行验证。
每个适用验收点至少有一个 approved case，每个 approved case 至少引用一个适用 point；
未覆盖、未知、重复或 orphan point/case 映射均为结构性拒绝。

### 3. review 结果

review 输出同时包含逐 case 决策和逐验收点的 `PointReviewDecision`。每个本次 scope
内的 approved case 恰好出现一次 case decision；该决定可以是本轮 review 的 `PASS`，也可以是
现有 `ApprovedSource=SUGGESTION_APPLIED` 的已批准证据，但必须带其来源并纳入当前 manifest/map
绑定。suggestion-apply 路径仍必须提供当前 manifest/map scope 下的逐 point 决策，不能只用集合级
`PASS` 补齐。每个 point 恰好出现一次 `PointReviewDecision`，且都必须有明确结果。一个 point 映射多个 case 时，point 的 review
通过要求其自身结果为 `PASS` 且全部 mapped case 的 case decision 均为 `PASS`；任一
point decision 或 case 为 `FAIL`、`PENDING` 或 `UNKNOWN`，该 point 及批准白名单均不得为
`PASS`。仅有集合级 PASS、缺失 point、未知 point、重复 point 或 case 不属于当前 manifest
时拒绝写入批准白名单。白盒 review 另外
负责测试代码质量和实现引用；白盒的结构性 inventory 不能替代验收点的语义 review。

### 4. 白名单、执行与候选绑定

approved whitelist 是由 `AcceptanceManifest`、case↔point 映射及其 digest 投影出的
结果，不维护第二份可独立编辑的清单。pre-wave case review（例如黑盒在 Qn 前完成的
review）绑定 manifest/map、需求 revision、route/topology scope 及 case/dispatch digest，
不把尚未存在的候选当作绑定；读取完整 Qn 的白盒 review 可同时绑定该 Qn。只有在完整
`ValidationCandidate Qn` 冻结后，QA execution 和门结果才签发为权威结果，并绑定
manifest/map/whitelist digest 与当前 Qn；digest 或候选不一致时结果失效，不能以旧结果或
工作树当前态补齐。对每个 point，execution 的 `EXECUTED_PASS` 只在其全部 mapped
approved case 均有本轮 `EXECUTED_PASS` 时成立；缺失、`FAIL`、`PENDING`、`UNKNOWN` 或
`AUTHORIZED_SKIP` 均不得伪装成 `EXECUTED_PASS`。摘要至少能展开为
`pointID → caseID → result`，并区分 `EXECUTED_PASS` 与 `AUTHORIZED_SKIP`。

## 范围和止损

- 只覆盖项目文档化的正常使用和常见误操作；对抗性输入、恶意/手工状态编辑、权限或
  不可变文件失败、未支持平台仍按 `prompts/reviewer-base.md` 作为 P3 建议，不阻塞
  PASS。
- 3.5a 只冻结 schema、映射校验、白名单投影/digest 和 fixtures；不在本 checkpoint
  改 legacy `QACase` 完整结构、不新增公开 QA 命令、数据库、事件溯源或通用测试框架。
- 不要求固定 case 数、覆盖率百分比或一对一映射；这些不能证明语义覆盖，反而会制造
  虚假门槛。

## 批次与后续接入

Batch A 由一个代理连续完成 schema、映射 validator、白名单投影/digest 和 fixtures；
这些是同一契约的内聚子任务，批内不换代理。Batch A 输出的是 Batch B 消费的 typed
contract，不新增公开命令、状态或存储。若阶段 3.5 后续还有其他 checkpoint，另行做技术
审和批次决定，不按文件数量强行加法。

阶段 4 的 Batch B 再由新的代理把契约接入 regular QA design/review/execution、
`ValidationCandidate` 和现有三轮流程；不与 Batch A 并行。Controller 将冻结的 manifest
与 map 作为 typed review input，QA design 返回显式 case↔point edges，QA review 返回逐
case 与逐 point decisions；这部分作为现有 result contract 的独立 payload 传输，不改
legacy `QACase` 完整结构，也不依赖 agent 口头补齐。接入时保留现有用户授权 skip 语义，
并把现有 approved whitelist 扩展为上述投影，避免第二套存储。

## 3.5a 验收 fixtures

至少逐项覆盖：合法多对多映射；同一点多个 case 中 point decision 与全部 case review/execution PASS
才允许 point PASS、任一点 decision 或 case 为 FAIL/PENDING/UNKNOWN 或仅有集合级 PASS 均被拒绝；无适用
QA 点的显式理由；现有 `ApprovedSource=SUGGESTION_APPLIED` case 可用其批准证据而无需新 case review，且仍需逐 point 决策；未知、重复、缺失和 orphan point/case（重复分别覆盖 manifest
pointID、重复 map edge、重复 PointReviewDecision）；合法 ID 但 `run/child`、review kind、
需求 revision、route 或 topology scope 错位时拒绝；manifest、map、whitelist 三类 digest
各自单独变化均使旧结果失效；当前候选不匹配被拒绝；pre-Qn review artifact 不得冒充
Qn execution evidence；`AUTHORIZED_SKIP` 不计为执行 PASS。完整 regular E2E、真实宿主和
VCS canary 留给阶段 4，不在 3.5a 重复实现。

Checkpoint 完成条件是：契约文档、typed schema/validator、digest/白名单投影和上述
fixtures 均可复现通过，并在阶段 4 任务书中登记接入边界；不以“已完成阶段 4”或
“已完成 QA 全流程”表述。
