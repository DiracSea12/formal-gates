# 阶段 3.5b：成本记录与运行时派发止损（非独立正式阶段）

状态：阶段 4 前完成的 checkpoint。不新增 `RunPhase`、formal run、Seal、公开命令或第二
套账本。3.5b 本身交付 legacy owner 计量和 engine-side 纯 guard 计算核心；engine 状态接入、
派发拦截、公开投影和真实运行时验收由阶段 4 完成。未完成阶段 4 接入前不宣称当前 engine
已具备运行时止损能力。

## 先说结论

3.5b 解决两个相邻但不同的问题：

1. 把本 run 的可核对用量记全：保留现有子代理派发记录，并在能够确定区间时补记启动该 run
   的主代理（owner）用量。
2. 在引擎真正签发下一次外部派发前做有限止损，避免瞬态重试或自动补位无限继续。

成本解析和运行时止损不放在同一个模块里：`internal/cost` 只负责把 host transcript 转成
精确 `Usage` 并写入现有 `cost` 投影；引擎负责判断还能不能派发。这样成本模块可以独立
增加 host adapter，止损规则也不会反向污染解析器。

## 本 checkpoint 与阶段 4 的代码边界

3.5b 当前开发只交付两组可独立验证的代码：

1. 在现有 `internal/cost`/legacy `RunState` 上增加可选 owner 记录、启动基线、Seal/Abort
   终态差值、幂等与 `UNAVAILABLE` 处理；不改变 dispatch 计量合同。
2. 在 engine 侧增加无 I/O、无状态写入的纯 guard calculation：从 canonical external
   `TaskKey`、有限 retry policy 和已确认 basis 计算 request/retry allowance 与
   `budgetBasisDigest`，并用已知答案 fixtures 验证。该核心不读取 transcript，也不签发任务。

阶段 4 才负责把上述能力接入 engine `State`/envelope、source bridge、`IssueFromPlan`、
`refill`、全部 recovery/HostAction retry、typed Ask、`show/status/next` 和真实宿主 E2E。
3.5b 只冻结这些接入点的合同，不提前实现它们；项目/安装级 token cap 的配置读取、持久化
和运行时拦截也留在阶段 4。

## 已核实的现状

- `internal/cost` 不依赖 `validate`；`validate` 只在结果记录时调用 parser 并回填
  `RunState.Cost`，依赖方向适合保留。
- 当前 `RunCost.Dispatches` 只覆盖 gate/action 派发，主代理没有成本条目。
- `RunState` 已保存 `OwnerTranscript`/`OwnerSession`；它们由 `workflow start` 的 hook
  sidecar 捕获，具备补记 owner 用量所需的身份入口，但当前没有用量基线。
- engine 已有 `Drive`、`IssueFromPlan`、`refill`、`Attempt.MaxAttempts` 和恢复记录；
  3.5b 应接这些已有边界，不再造一套调度或重试状态机。
- 当前 `AgentStep` 的 retry 可选、`HostActionStep` 没有 retry 字段，且无声明的
  `Attempt.MaxAttempts=0` 仍会保留自动恢复路径。3.5b 不能把这个零值继续解释成无界，也
  不为每种 step 增加配置字段；阶段 4 由 engine guard 对外部 agent/host 派发应用同一个
  canonical 有限默认策略。
- 当前 engine 的 `State`/facade 与 legacy `RunState` 分开，尚未承载 cost/owner/guard 投影；
  3.5b 不假定 engine 会调用 legacy `validate`。阶段 4 必须提供唯一的 engine source bridge，
  让同一 `cost` 投影可写入、重载并展示。
- `report.cost` 是 fan-in 后的报告步骤，不是派发门；不能把它改成成本控制器。

## 交付范围

### A. 成本模块保持可维护

- 保留 `internal/cost` 作为纯解析/计量包：输入是 provider + transcript，输出是精确
  `Usage` 或明确错误；不得依赖 `validate`、engine 状态机或用户交互。
- 保留单一 `RunCost` 投影，不增加 billing store、价格库、provider fallback 或另一套
  ledger。现有 `Dispatches` 的 JSON 形状继续兼容。
- 为 dispatch 与 owner 共用 token 合计逻辑；wire 层只增加一个可选 owner 记录，不把主
  代理伪装成一个 dispatch，也不引入通用“计费对象”框架。
- legacy 的 lifecycle/validate 继续负责绑定来源和写入时机；阶段 4 的 engine adapter 负责
  把 engine receipt/event 接到同一 `cost` 投影。engine 只消费稳定的 `Usage`/projection，
  不把 host transcript 格式判断散进 engine，也不再从另一个账本读取。
- 任何无法核对的结果（缺 path、provider 未绑定、格式不支持、文件被重写导致区间不可定）
  一律 `UNAVAILABLE`，数字为零；不以估算值参与止损。

### B. 补记主代理（owner）用量

- `workflow start` 成功时，为 owner transcript 记录一个解析基线，并同时记录 provider
  身份；基线、provider 和“已完成计量”标记随同一 `RunState` 持久化，不另写账本；没有
  稳定 transcript/provider 的 run 不承诺 owner 计量。
- run 进入终态（Seal 或 Abort）时再次读取同一 transcript，记录“终态快照 − 启动基线”
  的差值为 owner 记录。主代理记录单独存放在 `RunCost` 的 owner 字段，不能混入
  `Dispatches`。
- 只有来源、身份和快照区间都一致且可复核时才记数字。缺基线、缺终态快照、provider
  不支持、文件被截断/重写或无法判断区间时，owner 记 `UNAVAILABLE`，不回退到整文件总量。
- 活动 run 之间共享同一 owner transcript 且区间重叠时，不声称能够精确拆分；相关 owner
  项标记 `UNAVAILABLE`，不重复分摊。
- 现有子代理 backfill 仍在结果记录时执行，并保持按 dispatch 幂等；owner 记录也必须
  幂等。两者都不改写已记录的 PASS/FAIL。
- owner 记录只属于产生它的 run：主干保留自己的 owner，child 在自己的 receipt/sidecar
  保留自己的 owner。3.5b 的实时止损只约束 engine 可控制的外部派发；owner 条目是终态
  报告，不参与 in-flight 硬中断。若将来要求 owner 也参与实时 token cap，必须另行增加
  明确的快照点，不能把终态差值冒充实时用量。
- 阶段 5 必须满足主需求中 master 汇总 child cost 的约束：child 明细仍留在各自 receipt/
  sidecar，master 以 `childRunID` 为键把 child owner 总量幂等并入汇总投影；主干自己的
  `Owner` 仍只表示主干 owner，不把多个 child 覆盖到单一字段，也不引入通用 billing
  ledger。

### C. 引擎中的最小运行时止损合同与纯计算核心

3.5b 实现本节所需的纯 allowance/basis calculation、输入校验和 digest fixtures；下列涉及
engine 状态、派发时序、恢复入口、Ask 或公开展示的要求是阶段 4 接入合同，不是 3.5b
当前批次的运行时实现范围。

- **派发前**：在 `Drive` 的 `IssueFromPlan` 和结果提交后的 `refill` 之前，检查本 run
  剩余的派发额度。额度由 engine Controller 以纯函数从当前 revision 的 canonical
  expected external `TaskKey` 集、已确认 route/topology、compiled definition 和有限
  retry policy 机械得出；Ready 只决定本次可签发子集，不是让主代理估工作量。计算不调用
  LLM，不接受 host、主代理、用户或 submit event 传入的额度数字。expected `TaskKey` 只能由
  closed-world definition 和已经确认、持久化的 Batch/route/topology、已批准 QA 条目、已选
  gate 或已成立 obligation 派生；代理输出不能直接新增任务槽位。
- **派发后**：engine 在同一写事务内依次完成结果/回执接纳、通过唯一 source bridge
  backfill、usage 更新、guard 判断，再决定是否 `refill`；不得先 refill、再从 legacy 路径
  补账。source bridge 按 `actionID/correlation` 解析 lifecycle sidecar：stop 尚未到达时
  保持 `COST_PENDING` 并返回现有 Wait；stop 到达后才继续回填和 guard。来源明确不可用时
  记 `UNAVAILABLE`，token cap 不可用但派发次数上限仍生效。达到项目/安装级预先配置的
  token cap 时，禁止后续自动派发；该 cap 只统计 engine 派发用量，不含 owner 终态报告。
- **重试**：复用编译步骤的 `RetryPolicy` 和持久化 `Attempt.MaxAttempts`，并让
  `IssueFromPlan`、`refill`、`ResumeAgent`、`RecoveryNewAttempt` 和 HostAction retry
  共用同一 guard/Attempt 计数。外部 agent/host 派发没有显式 retry 时使用 canonical
  engine 默认 `MaxAttempts=2`（首次执行 + 一次自动重试）；compiled definition 中显式的
  有限策略可替代该默认，但不得声明无界。只有已分类的 `TRANSIENT_ENGINE_ERROR` 才按该
  策略自动 retry/backoff。`MaxAttempts` 统一表示该逻辑动作的总执行次数，额外 retry 余量
  为 `max(0, MaxAttempts-1)`；阶段 4 同步修正现有计数、恢复分支和测试，不再保留
  `0=无界`。绑定未变的瞬态恢复消耗该余量；绑定、快照或责任变化的 `NEW_ATTEMPT` 走既有
  新计划槽位，不冒充同绑定 retry，但仍经过派发 guard。配额、欠费、权限、参数等错误不得
  自动循环，也不得静默换 provider/model。
- **到达边界**：额度或 retry 耗尽后必须返回 typed Ask（有限继续、重试、跳过或 abort），
  继续执行只能由用户明确授权一次有限增量并留痕。“继续”固定解锁当前 canonical Ready
  batch（仍按既有 admission capacity 裁剪）；“重试”固定增加当前逻辑动作的一个 retry
  slot，二者都不接受自由数字，也不能预先累积。Wait 只用于容量为零、任务仍运行、依赖
  未满足、receipt/lifecycle 可能迟到或
  retryable I/O；若现有控制枚举没有预算处置项，只增加一个
  `DISPATCH_BUDGET_DISPOSITION` typed control，不新增生命周期或公开旁路。
- **持久化位置**：阶段 4 把额度、已用派发次数、token 对账状态和阻断原因作为 engine
  现有 `state.json`/envelope 的同一 `cost/guard` 投影保存，与 `Attempt`/`RecoveryRecord`
  同一写协议。投影同时保存计算所用的 requirement/route/topology/definition/expected-task
  digest（合成一个 `budgetBasisDigest`）和计算结果；每次 `IssueFromPlan`/`refill` 前从权威
  state 重算并核对。输入相同却结果不同、digest 不匹配、已用数大于可证明签发数或将要超发
  时，返回 `BLOCKED_BUG`/diagnose 且本次零派发，不让 host 或主代理选择一个数字继续。
  3.5b 不要求当前 legacy `RunState` 与 engine 双写，也不建第二状态机；`report.cost` 仍只做
  报告。
- **输入扩张**：当前已确认 workflow policy 已预授权、且由 Controller 从已成立 obligation
  机械派生的 repair slot 可以自动加入 basis。需求、Batch、route/topology 或批准集合变化若
  按现有流程本就要求用户确认，则必须先绑定对应 revision/typed Ask，并展示新增槽位及额度
  影响；确认前旧 basis 继续生效。普通 worker/reviewer result、host receipt 或一次 resume
  不能直接或静默增加 basis。
- **语义审查轮**：阶段 4 接管 regular 后，产品审和技术审按 master run + action kind 各自
  持久化 `PreDevelopmentReviewSeries`。当前 finding 处置确需 fresh review 时，前三个完成轮
  对应的 distinct TaskKey 由 workflow policy 逐个预授权；达到三轮后，只有 action-specific
  typed Ask 可再增加一个 fresh-review slot。只有有效 PASS/FAIL 完成语义轮；runtime/
  invalid/incomplete/interrupted/stale 不计，revision/invalidation 不清零，split child 不另开
  series。该 slot 是新的逻辑 TaskKey，不是当前 TaskKey 的 retry；每个 TaskKey 内仍独立按
  有限 `MaxAttempts` 计算瞬态 retry 余量。
- **主代理实时性**：owner transcript 只能在已有快照点参与后续派发判断，不能宣称对正在
  生成的当前主代理消息做 in-flight 硬中断；外部派发的预检查仍必须生效。

## 限额如何定、用户看到什么

- **谁计算**：只由 engine Controller 计算；compiler/definition 决定哪些 step 是外部派发及其
  retry policy，已确认计划提供 canonical expected `TaskKey` 集。主代理、host、用户和 AI
  都不是计算权威。
- **怎么算**：request 额度 = 当前已确认计划中 distinct external `TaskKey` 槽位数 + 各逻辑
  动作 `max(0, MaxAttempts-1)` 的有限瞬态 retry 余量。外部派发未显式声明时按 canonical
  `MaxAttempts=2` 计算。绑定/快照/责任变化形成新计划时，只按新 basis 重算尚未消费的槽位，
  不沿用旧 retry 池，也不重复赠送已消费槽位。不同任务的差异来自 route/topology/任务集，
  不是让用户猜一个统一数字。AI 提出的拆分或路线建议只有在按现有流程确认并持久化后才进入
  计算；当前 workflow policy 已预授权的 obligation/repair slot 则由 Controller 机械加入，
  不另加一次用户确认。
- **算错怎么办**：canonical calculation、basis digest、已用 action/Attempt 账目三者必须
  一致；不一致按 engine bug 硬停，不能降级为 AI 估算、host 传值或用户填写数字。生产实现
  还必须用已知答案 fixtures 直接核对公式及增量不变量，不再为此复制第二套运行时计算器。
- token cap 不是按本次任务临场估算；只接受项目/安装级维护配置。没有配置时，界面明确
  “token cap unavailable”，仍用 request/attempt 硬止损，不因不可计量而放行更多调用。
- 阶段 4 接入后，`start`/`show`/`next` 只展示引擎已算出的预计派发数、剩余派发额度、
  retry 余量、token cap 可用性、计算 basis 摘要和“系统计算、不可手填”；不展示伪精确
  美元账单。美元换算、动态预算、自动校准均不在本 checkpoint。
- 达到边界时显示已用/上限、触发原因和可选用户动作（继续当前 Ready batch、重试当前逻辑
  动作一次、跳过或 abort）；Ready batch 仍按既有 admission capacity 裁剪。每次授权都明确、
  有限、可审计。

## 与阶段 4–7 的承接

- **阶段 4（Git 非分片 regular）**：实现唯一 engine source bridge，把 cost/guard 投影
  接入现有 `State`/envelope、`submit` 和 `show/status/next`；按“接纳 → source resolve /
  backfill → usage → guard → refill”顺序运行，source 未就绪时返回 Wait。把 3.5b 已实现的
  owner baseline/final snapshot 接到 engine run（owner 只作终态报告）；在真实候选上验证
  一次额度耗尽后只生成 typed Ask、一次用户授权后只解锁一个经 admission 裁剪的 Ready
  batch 或一个 retry slot。所有自动恢复入口都必须走同一 guard；外部派发缺显式策略时
  使用 canonical `MaxAttempts=2`，不得无界。
- **阶段 5（Git 分片）**：child 沿用同一 guard 合同；master 只在收齐 child receipt 后
  汇总 dispatch cost 和 child owner cost，dispatch 使用 `<childRunID>/<dispatchID>`、owner
  使用 `childRunID` 去重；不重新解释或重算 child 限额。owner 仍按 run 保留，master 自身
  `Owner` 不被 child 覆盖，不引入通用 billing schema。
- **阶段 6（SVN/P4 与宿主）**：各 adapter 只补 provider identity、transcript/usage
  能力和 `UNAVAILABLE` 证据；不为每个 host 另造预算算法。没有可靠 usage 的 host 仍受
  request/attempt 止损。
- **阶段 7（唯一权威与文档切换）**：确认唯一 engine writer 后，统一更新
  `SKILL.md`、`references/formal-flow.md`、cost-metering 说明、阶段记录、README/目录及
  测试文档，删除“成本永远只展示、永不参与后续派发”的过时表述；保留“精确或
  unavailable、无估算、无第二账本”的边界。

## 3.5b 当前 checkpoint 验收

- cost parser 仍可独立测试；engine/validate 不反向进入 parser 包。
- 子代理 dispatch 与主代理 owner 各有成功、缺失、不可解析和重复记录测试；owner 差值不
  把启动前历史计入，也不在重启/重复 Seal 时重复累计。
- 纯 guard calculation 无 I/O、无状态写入、不依赖 `internal/cost` transcript adapter；相同
  canonical basis 得到稳定 allowance 和 `budgetBasisDigest`。
- 已知答案 fixtures 覆盖 0/1/N 个 external TaskKey 与 `MaxAttempts=1/2/n` 的精确结果；新增
  一个合法 distinct TaskKey 恰好增加一个基础槽位，重复 TaskKey 不增加，单个有限
  `MaxAttempts` 增加 1 恰好增加一个 retry slot。测试直接调用生产 calculation，不复制公式。
- 未绑定 closed-world definition 或已确认 Batch/route/topology/QA/gate/obligation 来源的
  external `TaskKey` 被拒绝；现有流程要求用户确认却未取得对应 revision 的 task-set 扩张不得
  增加额度，已由当前 workflow policy 预授权的 obligation/repair slot 不重复询问用户。
- 本 checkpoint 不修改 engine `State`/envelope，不接入 source bridge、`IssueFromPlan`、
  `refill`、recovery、HostAction retry、typed Ask 或公开展示；这些属于阶段 4。

## 阶段 4 保留验收

- source bridge 覆盖 result-before-stop、stop-before-result、UNAVAILABLE 和重复回填；
  `IssueFromPlan`、`refill`、`ResumeAgent`、`RecoveryNewAttempt`、HostAction retry、
  重启恢复和 `Attempt.MaxAttempts` 均证明达到边界后不会多发；transient 只按显式有限策略
  或 canonical `MaxAttempts=2` 重试，配额/欠费/权限/参数错误不循环。
- `MaxAttempts=1` 不产生 retry，`MaxAttempts=n` 只产生 `n-1` 个额外 retry；绑定变化的
  新 Attempt 使用新计划槽位；额度耗尽是 Ask，lifecycle/receipt 尚未到达才是 Wait。
- 相同 definition/state/route/topology/expected-task 输入重复计算得到相同额度和
  `budgetBasisDigest`；任一输入变化必改变 basis。伪造持久额度、漏计/重复 TaskKey、并行
  refill/CAS 竞争、重启后账目不一致都以零派发的 `BLOCKED_BUG` 拒绝。
- host/主代理/用户提交底层额度字段一律拒绝；预算 Ask 的“继续”恰好解锁一个 Ready batch，
  “重试”恰好解锁一个 retry slot，重复 submit 不重复增加。
- 产品审/技术审分别证明前三个 completed semantic rounds 可按需生成 distinct TaskKey；第 4
  轮前必须 Ask，单次授权只增加对应 action 的一个 fresh-review slot，revision/invalidation
  不重置，split child 不另开 series；PASS/FAIL 与 runtime/invalid/incomplete/stale 的计轮边界
  以及它和 `MaxAttempts` retry 的独立性均有已知序列测试。
- 分片 master 对 child owner 以 childRunID 幂等汇总，重复 receipt 不重复累计；主干 owner
  仍独立记录。owner 终态报告不参与实时 guard。
- engine 额度/guard 与 cost 投影在同一 `state.json` 写协议中持久化；legacy
  旧状态无新字段时仍能按既有兼容规则读取。
- 用户授权继续、重试、跳过和 abort 的路径分别留痕；不改写既有 PASS/FAIL。这里的“重试”
  指同一 provider/model 下按既有绑定恢复，不是动态换 provider/model。
- 使用 fake host/receipt，不调用真实付费 API；真实宿主 canary 和阶段 5–7 文档同步按上节
  逐阶段验收。

## 明确不做

不做 AI 估算 token、不做统一全局固定“大数”、不做 USD 价格数据库、月度账单、多租户
预算、provider 路由/fallback、动态 profile、watchdog/定时器、通用 billing DSL、新
`RunPhase`、新的公开写命令，亦不把 owner 终态报告扩展成 in-flight 硬中断，不把 reviewer
变成可写测试或可写实现的角色。
