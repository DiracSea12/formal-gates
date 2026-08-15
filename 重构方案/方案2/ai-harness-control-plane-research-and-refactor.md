# AI 编程 Harness 调研与 formal-gates 控制面重构报告

> 状态：架构调研与重构建议，尚未实施
>
> 调研日期：2026-08-13
>
> 适用基线：formal-gates `ed8d4812d6aa4742a55936a5a0c466b2dfbe6781`
>
> 本报告讨论的是 Claude Code、Codex 等编程助手**上层**的 harness、工作流与控制面，
> 不是编程助手本身。
> 本文是调研和实施设计文档，不是已完成重构的声明；文中“应当”、“建议”和
> “目标系统”都是 formal-gates 的待实施方案。

## 先说结论

formal-gates 现在有很多有价值的门禁，但它还不是一个真正掌握控制权的工作流引擎。它更
接近一组“带顺序检查的命令”：程序能判断主代理刚刚调用的命令合不合法，却不能主动告诉
系统下一步只有什么、应当同时派出哪些任务、某个 worker 失联后怎样恢复、哪些迟到结果
必须永久丢弃。

所以当前最大的风险不在单条校验规则，而在控制权归属：

- 主代理要自己记住二十多个命令及其顺序。
- 主代理决定下一步、并行集合、重试、恢复、结果采信和合并。
- CLI 会拒绝部分错误动作，但不会自动完成漏掉的动作。
- 一次漏派、错序、失联或迟到回执，就可能把流程留在“没有人知道下一步该做什么”的状态。

重构的核心不是增加更多提示词，也不是再加一个更聪明的总代理，而是采用下面这条原则：

> **No LLM in the control loop。LLM 只做需要语义判断的叶子工作，不负责推进流程。**

目标系统应当由普通 Go 代码持有一张固定、版本化的流程图。任何时刻，系统根据持久状态
只能算出以下五种结果之一：

```text
DISPATCH_BATCH   有一批确定的任务可以执行
WAITING_HUMAN    正在等待一个有明确选项的人工决定
RETRY_AT         没有立即任务，但已确定何时重试
NEEDS_OPERATOR   遇到协议错误或不变量破坏，需要人工处理
COMPLETE         流程结束
```

最重要的不变量是：

```text
每个非终态，都必须有 ReadySet 或明确的 WaitReason。
```

不能再出现“没有可派任务、没有等待原因、也没有结束”的无声死端。

## 1. 为什么要做这次重构

### 1.1 现有设计解决了什么

当前实现并不是没有价值。以下能力应当保留：

- 已确认需求、需求 revision 与快照绑定。
- 提示词文件和 prompt hash 绑定，防止主代理临场改写派发内容。
- 产品审、start-readiness、QA、门审、修复和 Seal 的大量顺序守卫。
- Git/SVN/P4 原生快照与完整 diff 语义。
- 黑盒 QA 隔离 worktree、按 mode 分离的 QA 状态。
- 零上下文审查者、派发身份绑定、结果契约和严重度边界。
- 状态完整性 hash、原子文件替换、并发修改时的进程锁。
- Seal 前检查所有选中结果是否已在当前快照完成。

这些规则说明项目已经积累了大量可靠的领域知识。重构不应推倒重来，而应把它们从“每个
CLI 命令里的守卫”迁移为新 reducer、receipt validator 和 integration policy 的纯规则。

### 1.2 现有设计没有解决什么

README 当前直接把主代理画成总编排者：

- [`README.md`](../README.md) 第 21 行：`主代理（你的 AI，负责编排）`。
- [`README.md`](../README.md) 第 271 行：所有 `workflow ...` 命令由 AI 流程驱动者执行。

CLI 暴露了 `start`、`requirement`、`slicing`、`route`、`prepare-*`、`claim-dispatch`、
`record-*`、`qa-*`、`snapshot`、`carry`、`authorize-repair`、`seal` 等约 26 个独立入口：

- [`internal/cli/cli.go`](../internal/cli/cli.go) 第 179 行。

这意味着正确流程没有被封装为一条只能向前走的道路，而是给主代理一盒零件，让它每次
自己选下一件。`requireTransition` 能挡住一部分非法调用，但它只能回答“这一步不允许”，
不能回答“现在唯一合法的完整下一步集合是什么”。

### 1.3 这不是理论问题

本次调研本身就发生了一次同构事故：核心调研已经完成，但父代理仍保持 `running`，继续
等待和扩展孙代理；主代理又错误地反复调用不适用的等待工具，失败后没有熔断，也没有将
已完成结果收敛为终态。结果不是任务内容需要一天，而是控制环没有明确的完成条件、重试
预算和错误终态。

这次事故和 formal-gates 当前风险是同一种结构：

```text
有很多局部规则
    +
一个智能代理负责解释“接下来怎么办”
    =
无法证明最终一定前进或明确失败
```

因此“把编排写得更详细”不够。编排必须变成代码维护的状态机，并且每条重试、等待和终止
都有机器可判断的边界。

## 2. 本次调研怎么看这些项目

不能只按 GitHub star 或“是否多代理”来判断。对本项目真正重要的是以下问题：

1. 下一步由普通代码决定，还是由模型临场决定？
2. 状态是否持久化，进程重启后能否从事实重建？
3. claim 是否有租约、token 和单调 generation？
4. 旧 worker 的迟到结果能否永久失去写权限？
5. fan-out 的完整成员集合是否在派发前持久化？
6. 外部副作用是否通过 outbox、幂等键和不可变回执管理？
7. 修改是否在隔离 worktree 中完成？
8. 合并是否经过串行队列和 `expectedHead` 比较？
9. 重试是否有次数、时间、成本和重复循环上限？
10. 人工决定是否成为可恢复的正式状态，而不是只存在聊天记录里？

根据这些问题，调研对象分成五层：

| 层级 | 作用 | 代表项目 |
|---|---|---|
| 控制内核 | 持久状态、claim、lease、reconcile、队列、恢复 | Paperclip、Cline SDK Cron、OpenHands Automation |
| 工作流/方法层 | 固定阶段、任务拆分、复审方式、上下文隔离 | bmad-loop、LangGraph、Spec Kit、Superpowers、GSD |
| worker runtime | 运行一个代理任务并限制步数、时间和成本 | mini-swe-agent、OpenHands SDK、Cline |
| adapter/UI | 管理会话、终端、worktree、看板和人工监督 | Vibe Kanban、Cline Kanban、Claude Squad |
| 反例或局部参考 | 多代理很多，但控制仍由模型或人工决定 | AutoGen、CrewAI、Ruflo、部分 Roo/Goose 工作流 |

下面的结论不是评判项目整体好坏，而是判断它能否解决 formal-gates 的特定控制面问题。

### 2.1 证据等级与调研限制

为了避免后续实施者把“外部项目已经实现的东西”和“本报告推导出的方案”
混在一起，本文按下列方式理解：

| 类别 | 是什么 | 可以怎样使用 |
|---|---|---|
| 本地事实 | 针对上述固定 Git 基线的源码、README 和 references 检查 | 可以直接作为本轮重构的问题证据；基线改变后需重新核对 |
| 外部事实 | 项目官方仓库、官方设计文档或官方文档中公开的机制 | 用于证明某种设计已被真实 harness 采用；不等于它适合原样移植 |
| 架构推导 | 将多个项目的机制对照 formal-gates 故障后得出的判断 | 用于说明为什么要选 reducer、outbox、lease 等；需由本项目测试验证 |
| 实施建议 | 本报告为 formal-gates 设计的模块、表结构、协议和迁移顺序 | 是后续需求/方案的输入，不是外部项目的原样实现 |

外部项目会持续变化。除非文中明确写出 commit，本次调研只能理解为“2026-08-13
前后可见的官方源码/文档快照”，不声称所有结论都绑定到某个精确外部 commit。
开始移植某项机制前，实施者应再次打开对应一手来源，并把当时的 commit 记入
ADR 或实施需求。

### 2.2 统一对比矩阵

下表只评估“对 formal-gates 控制面重构是否有直接参考价值”。“部分”通常表示项目
的某个子系统具备该能力，但不能当作整个工作流的硬保证。

| 项目 | 下一步控制权 | durable state | lease / fencing | 自动 reconcile | fan-out / fan-in | 隔离 workspace | 串行 merge queue | 在本项目中的定位 |
|---|---|---|---|---|---|---|---|---|
| Superpowers | 代理按 skill 推进 | 部分，主要是计划/上下文 | 无通用机制 | 无 | 方法层支持 | 支持 worktree | 无通用队列 | 开发方法和角色隔离 |
| LangGraph | 图与节点代码，节点内可有 LLM | checkpoint | 需应用自建 | interrupt/resume，副作用需自建 | 支持 | 无 Git 专用机制 | 无 Git 专用队列 | 借图、checkpoint 和 interrupt 语义 |
| Cline / Cron | 交互任务由模型；Cron 由代码 | 支持 | Cron 支持 token/heartbeat | Cron 支持 | 不是通用业务 DAG | 部分 | 无通用队列 | worker adapter；重点借 scheduler |
| mini-swe-agent | 线性模型循环 | trajectory，非任务账本 | 无 | 无 | 无 | 环境隔离 | 无 | 尽量薄的叶子 worker |
| OpenHands | agent / automation scheduler 分层 | event log / DB | conversation lease 支持 generation | watchdog 支持 | 部分 | sandbox/workspace | 无 formal-gates 所需 Git 队列 | event、lease、watchdog 参考 |
| Gas Town | Mayor 与各固定角色 | Beads ledger | heartbeat 有，统一 fencing 不足 | Witness/Deacon | Convoy/多 worker | 支持 worktree | Refinery 支持 | 账本、监控、worktree、merge queue |
| bmad-loop | 普通代码 | 支持恢复的状态 | 非核心能力 | 有限重试/升级 | 固定流程 | 支持 worktree | 未覆盖本项目需求 | `No LLM in the control loop` 原则 |
| Paperclip | durable control plane | DB 账本 | execution lock，部分 | process loss/orphan 恢复 | 部分 | workspace 是一等对象 | 无 formal-gates 专用队列 | durable queue、lock、进程身份和审计 |
| Spec Kit / OpenSpec / GSD | 代理按工件/命令推进 | 文件工件 | 无通用机制 | 无控制面保证 | 方法层支持 | 视工具而定 | 无通用队列 | spec/计划/上下文组织 |
| Oh-My-ClaudeCode | 固定 profile + 代理 | tracking state | owner epoch/nonce | 支持 restart recovery | 固定 profile 支持 | 专用 worktree | 串行 merger | 最接近可落地的固定 harness 参考 |
| Symphony | 单一 orchestrator authority | 部分，主要在内存 | 有限 | poll/reconcile | bounded concurrency | per-issue workspace | 无 durable Git 队列 | 借单权威、reconcile 和 stall 管理 |

这张表的直接结论是：没有一个外部项目可以整包替换 formal-gates。最可行的路线是保留
本项目的领域规则，把 bmad-loop 的确定性控制原则、Cline/OpenHands/Paperclip 的运行时
机制、Gas Town/Oh-My-ClaudeCode 的 worktree 与 merge 经验组合起来。

## 3. 重点项目调研

### 3.1 Superpowers：强方法，弱控制内核

官方仓库：<https://github.com/obra/superpowers>

Superpowers 的价值在于把开发方法拆成强约束 skill：先澄清、写计划、逐任务实现、用新鲜
上下文审查、修复后再审。它尤其适合借鉴以下做法：

- 每个实现任务交给 fresh implementer，减少上下文污染。
- 规格符合性和代码质量可以分两阶段审查。
- 任务计划要求明确文件、步骤和验证方式。
- worktree 隔离不同任务。
- skill invocation 让常见方法不靠代理临时发明。

但 Superpowers 的控制力主要来自 Markdown 指令和代理遵守。代理仍需要正确调用 skill、
正确解释当前阶段、正确决定下一任务。它没有通用的 durable lease、fencing token、事务
outbox 或 merge queue。

**结论：**保留其角色隔离、fresh context 和两阶段审查方法，但不能用它替代工作流内核。

### 3.2 LangGraph：图和 checkpoint 很好，副作用可靠性仍需自己做

官方仓库与文档：

- <https://github.com/langchain-ai/langgraph>
- <https://docs.langchain.com/oss/python/langgraph/persistence>
- <https://docs.langchain.com/oss/python/langgraph/interrupts>
- <https://docs.langchain.com/oss/python/langgraph/fault-tolerance>

LangGraph 提供 typed graph、checkpoint、interrupt/resume、retry policy，以及 `Send` 式
fan-out/fan-in。它对本项目最有价值的是“图节点和状态快照是第一等对象”，人工决定也可以
成为可恢复 interrupt。

需要特别注意：从 interrupt 恢复时，节点可能从头重新执行。checkpoint 只能说明图状态
可恢复，不等于节点里的 shell、Git、文件写入等副作用 exactly-once。外部副作用仍必须
幂等，或通过独立 task/outbox 管理。

本项目已经是 Go，而且有大量现成领域规则。直接引入 Python LangGraph 会带来第二运行时
和部署复杂度，却仍然要自己实现 lease、fencing、receipt 和 Git integration queue。

**结论：**借它的静态图、checkpoint、interrupt 和 retry 语义，不建议把它作为当前 Go
项目的直接运行时依赖。

### 3.3 Cline：运行时成熟，Cron 子系统的 lease/fencing 最值得借

官方仓库：<https://github.com/cline/cline>

Cline 作为交互式 coding harness，提供会话持久化、审批、checkpoint、loop detection、
有限 retry、worktree/session 管理和结构化终态。模型仍然会决定工具调用和任务推进，因此
它本身不是 formal-gates 所需的固定业务 DAG。

更值得借鉴的是 Cline SDK 的 scheduling/Cron 机制：

- SQLite 保存 durable run queue。
- reconciler 根据 schedule 规格与数据库事实补齐任务。
- `BEGIN IMMEDIATE` 一类事务边界原子 claim due run。
- run 带 `claimToken`、`claimUntilAt` 和 attempt。
- heartbeat 延长 claim。
- 完成、失败或改期时用 `WHERE run_id AND claim_token` 条件更新。
- 已过期 worker 即使稍后返回，也因为 token 不匹配而失去写权限。

这正是当前 `PreparedDispatch` 缺少的能力。

**结论：**Cline 可以做 worker/session adapter；其 Cron store 的 claim、heartbeat、reconcile
和 token 条件更新可直接指导 formal-gates 的 scheduler 设计。

### 3.4 mini-swe-agent：叶子 worker 应当这么简单

官方仓库：<https://github.com/SWE-agent/mini-swe-agent>

mini-swe-agent 的核心是很小的线性循环：模型响应、提取动作、执行 shell、把结果加入消息
历史，然后继续。它提供 step、wall time、cost、连续格式错误上限，并保存 trajectory；
环境可以换成 Docker、Podman、Singularity 或 bubblewrap。

它没有 durable task ledger、DAG、fan-in/fan-out、lease 或 merge queue。这不是缺陷，而是
清楚展示了 worker runtime 和 workflow kernel 应当分开：worker 只负责一张 ticket。

**结论：**Claude Code、Codex、Cline、mini-swe-agent 都应通过统一 adapter 作为可替换的
叶子 worker，而不是拥有 formal-gates 的流程推进权。

### 3.5 OpenHands：event log、conversation lease 和 automation scheduler

官方项目：

- <https://github.com/OpenHands/OpenHands>
- <https://github.com/OpenHands/software-agent-sdk>
- <https://github.com/OpenHands/automation>

OpenHands SDK 的 EventLog 提供持久事件、事件 ID、父子树、append 去重、文件锁和并发同步；
conversation lease 使用 TTL、owner instance ID 和单调 generation，支持 takeover 并防止
旧 owner 继续写。这些都比“记录 worker 是否启动”更接近可靠控制面。

OpenHands Automation 又把 scheduler、dispatcher 和 watchdog 分开：数据库行经条件更新从
PENDING 进入 RUNNING，watchdog 回收超时任务，callback 和 watchdog 通过状态条件与受影响
行数解决竞态。在 PostgreSQL 下可用 `FOR UPDATE SKIP LOCKED` 并发 claim。

局限是 OpenHands 的 dynamic workflow 仍可能由父 agent 现场生成流程；automation 也不是
formal-gates 业务 DAG，而且部分部署模式没有统一 attempt epoch。

**结论：**借 event log、lease takeover、watchdog 和条件更新；不要让 dynamic agent
workflow 取代固定 formal-gates 图。

### 3.6 Gas Town：任务账本、监控和 merge queue 很强，Mayor 不能照搬

官方仓库与设计文档：

- <https://github.com/gastownhall/gastown>
- <https://github.com/gastownhall/gastown/blob/main/docs/design/architecture.md>
- <https://github.com/gastownhall/gastown/blob/main/docs/concepts/polecat-lifecycle.md>
- <https://github.com/gastownhall/gastown/blob/main/docs/concepts/heartbeats.md>

值得借鉴的机制：

- Beads 作为 durable work ledger。
- Convoy 跟踪一批相关工作。
- Witness/Deacon 监控 worker、僵死任务和系统健康。
- polecat 的 identity、sandbox、session 生命周期分离。
- 每个 worker 使用隔离 worktree，不直接写 main。
- Refinery 采用类似 Bors 的串行 merge queue，可批量验证并二分坏变更。

不能照搬的部分：

- Mayor 仍是智能全局协调者。
- “hook 上有工作就推进”的 Propulsion Principle 不能作为正确性证明。
- heartbeat 说明进程还活着，不等于它仍拥有当前 generation 的写权限。
- worker 自报完成不能成为唯一事实来源。

**结论：**借 durable ledger、watchdog、生命周期分层、worktree 和 Refinery；全局决策必须由
确定性 reducer 代替 Mayor。

### 3.7 bmad-loop：最重要的是明确把 LLM 移出控制环

官方仓库：<https://github.com/bmad-code-org/bmad-loop>

bmad-loop 明确提出 “No LLM in the control loop”。普通代码负责选择 story、执行 gate、
计算 retry budget 和判断完成；状态写盘后可以恢复；失败进入有限重试或 typed escalation；
开发使用 worktree 隔离。

它没有覆盖 formal-gates 全部 QA、审查和跨 VCS 语义，但方向最接近本次重构目标。

**结论：**把这条原则写入架构约束：模型可以判断需求或代码，但模型不能选择 workflow
transition。

### 3.8 Paperclip：durable control plane 的重要参考

官方仓库：<https://github.com/paperclipai/paperclip>

Paperclip 面向更通用的 agent company，但控制面有大量可复用思想：

- 数据库存储 wakeup/heartbeat queue，并合并重复唤醒。
- issue checkout 和 execution lock 原子化。
- 保存进程 PID 与启动身份，区分旧进程和 PID 复用。
- 检测 process loss、orphan execution，并进入明确恢复路径。
- 状态转换、审批、预算与审计可持久化。
- workspace 和 agent run 是一等对象。

它没有替 formal-gates 定义 Git `expectedHead` 集成栅栏，也不拥有本项目的审查语义。

**结论：**借 durable queue、execution lock、process identity、恢复和审计，不照搬其组织模型。

### 3.9 Spec Kit、OpenSpec、GSD：适合工件组织，不足以保证控制正确

官方项目：

- Spec Kit：<https://github.com/github/spec-kit>
- OpenSpec：<https://github.com/Fission-AI/OpenSpec>
- GSD：<https://github.com/gsd-build/get-shit-done>

GSD 的上述官方仓库已于 2026-06-26 归档并转为只读。它仍是这些设计的一手历史
来源，但不应作为“目前仍在活跃迭代的控制面底座”。

Spec Kit 的 constitution、spec、plan、tasks、implement 工件链，以及 condition、loop、
fan-out/fan-in、run/resume/status 概念适合组织 formal-gates 的需求和流程定义。

OpenSpec 的 change folder、artifact DAG、delta spec 和 archive 适合需求演进，但它明确强调
fluid/iterative、避免 rigid phase gates，与“封死的流程道路”不是同一目标。

GSD 的 fresh context、dependency waves、两阶段 review 和 Markdown 计划对执行质量有帮助，
但状态和推进主要依赖提示词/文件约定。

**结论：**把它们放在 specification/method 层，不承担 claim、lease、fencing 和 merge。

### 3.10 Kilo/OpenCode、Oh-My-ClaudeCode、Symphony

相关项目：

- Kilo Code：<https://github.com/Kilo-Org/kilocode>
- OpenCode：<https://github.com/anomalyco/opencode>
- Oh-My-ClaudeCode：<https://github.com/Yeachan-Heo/oh-my-claudecode>
- Symphony：<https://github.com/openai/symphony>

Kilo/OpenCode 的新 event core 使用 SQLite durable event log、aggregate sequence、事务内
event + projection，并在 replay 时检查 sequence/owner/divergence；SessionRunCoordinator
按 session key 串行，同一 key 合并 wake，不同 key 并发。这适合作为本项目事件存储和
per-run 串行 reducer 的参考。

Oh-My-ClaudeCode 有固定 `RALPLAN -> EXECUTION -> RALPH -> QA` profile、canonical hash、
tracking revision、owner epoch/nonce/进程启动身份、专用 merger worktree、持久 worker SHA、
串行 merge 和 restart recovery。它证明“固定 pipeline + owner fencing + dedicated merger”
可以在 coding harness 中落地。

Symphony 强调单一 orchestrator authority，以及 `poll -> reconcile -> validate -> dispatch`、
bounded concurrency/retry/stall 和 per-issue workspace。但主要调度状态在内存，重启依赖
tracker/filesystem 恢复，因此不能直接作为 durable 内核。

**结论：**事件序列、owner epoch、per-key 串行执行、固定 profile 和 dedicated merger 都
值得吸收；不能依赖内存 timer 或会话状态作为唯一真相。

### 3.11 其他交互式 harness 的边界

Cline Kanban、Vibe Kanban、Claude Squad、Roo Code、Goose、Continue、Crush、Aider、
Plandex 等提供了任务看板、parent/child session、checkpoint、recipe、worktree、终端、权限
或上下文压缩。这些是很有价值的 UI 和 worker 能力，但它们通常允许模型或人决定下一工具、
下一任务和恢复方式。

AutoGen、CrewAI、Ruflo 一类多代理框架擅长角色协作、通信、memory 和 swarm。代理越多不等
于控制越可靠；如果多个模型共同决定控制流，反而扩大不可预测状态空间。

**结论：**可以接入这些项目作为 backend 或 UI，但不能让它们成为流程事实源。

## 4. 对 formal-gates 的源码诊断

### 4.1 缺少唯一的 `Plan(state)`

当前没有一个核心函数能回答：

```go
Plan(state) (ReadySet, *WaitReason)
```

`requireTransition` 只在调用某个操作后检查前置条件。公开 CLI 又允许调用者自由选择操作，
因此系统不能保证调用者一定尝试正确的下一步。

目标应当反过来：外部调用者不再提交“我要执行 qa-review”，而是请求系统 advance；系统
自己计算当前所有 ready 节点，并创建不可变 tickets。

### 4.2 并行只是提醒，不是调度

[`internal/validate/parallel.go`](../internal/validate/parallel.go) 第 125 行的 `ParallelAdvice`
只返回阶段、应并行任务、当前在途数和提醒文案。第 139 行的 `ParallelAdviceFor` 是只读计算。

[`internal/cli/cli.go`](../internal/cli/cli.go) 第 550 行又明确说明提醒只写 stderr，不创建
dispatch、不写生命周期事件、不记录完整 expected member set。

正常故障链如下：

1. snapshot 后理论上应派出黑盒 QA、白盒 QA 和所有选中门。
2. 主代理只派了其中一部分。
3. CLI 输出提醒，但主代理忽略或没有正确解析 stderr。
4. 已派任务全部完成。
5. Seal 在 [`workflow_carry_seal.go`](../internal/validate/workflow_carry_seal.go) 第 588 行
   发现还有 PENDING，拒绝结束。
6. resume 不会事务性补派缺失成员，于是流程只能等主代理重新理解现场。

目标系统必须在 fan-out 时一次事务中写入完整 `expected_set` 和全部 outbox items。只要事务
成功，就不存在“忘了其中一个成员”；只要事务失败，就一个都不算派出。

### 4.3 CLAIMED 没有真正的 lease

[`internal/validate/runstate.go`](../internal/validate/runstate.go) 第 156 行的 `PreparedDispatch`
有 ID、target、attempt、reviewer、status 和各种 hash/snapshot 绑定，但没有：

- `lease_deadline`
- `lease_epoch`
- `lease_token`
- 持久 heartbeat
- owner generation

同功能去重依赖生命周期 stop 原因。若 worker 消失但 stop hook 没有落盘，旧 CLAIMED 会被
认为仍在途；新派发 claim 又会在 [`workflow.go`](../internal/validate/workflow.go) 第 1780 行
被拒绝。系统没有一个纯事实事件能自动表示“这次 lease 已过期，epoch 递增并重派”。

目标不是简单增加超时字段，而是规定所有写操作都必须带当前 token：

```sql
UPDATE attempts
SET heartbeat_at = ?, lease_deadline = ?
WHERE attempt_id = ?
  AND lease_epoch = ?
  AND lease_token = ?
  AND status IN ('CLAIMED', 'RUNNING');
```

受影响行数为 0 就表示 owner 已经失效，worker 必须停止，不得继续提交结果。

### 4.4 STALE 不是统一的 fencing

当前 STALE 结果在同功能替换派发仍为 OPEN/CLAIMED 时会被拒绝；但替换派发完成后，这个
局部条件不再成立。不同 action 又有不同的“已有权威结果”守卫，因此可能出现旧回执覆盖新
回执，或者不同节点行为不一致。

正确做法不是继续给每个 action 补特例，而是统一规定：

```text
receipt.attempt_id、lease_epoch、lease_token
必须全部等于节点当前有效 attempt。
```

旧 epoch 无论新 attempt 是 OPEN、RUNNING 还是 COMPLETED，都永久没有推进状态的资格。

### 4.5 整份 JSON + 陈旧锁不能承担新的并发控制面

[`internal/validate/workflow.go`](../internal/validate/workflow.go) 第 1538 行的 `mutateRun`
在锁内读取整份 `RunState`，修改后整体重写。第 1574 行的锁超过 30 秒可被删除接管，但锁
没有 owner token、进程启动身份或 state revision。

可能的正常故障：

1. A 获得锁，读取状态，某个操作意外超过 30 秒。
2. B 认为锁陈旧，删除锁，读取旧状态并写入自己的结果。
3. A 恢复，用自己更早读取的整份 projection 覆盖 B 的更新。
4. A 的 defer 还可能删除 B 当前使用的同一路径锁。

原子 rename 能防止半个 JSON 文件，但不能防止 lost update；state integrity hash 能检测手工
改写，但不是 compare-and-swap。

个人单机项目不需要上分布式数据库。SQLite WAL 加短事务已经足够：单个 reducer 事务按
run/version 更新，事件、projection 和 outbox 同时提交。

### 4.6 worker 结果仍由主代理翻译和采信

worker 提示词要求结构化 JSON，但 `record-action`、`record-gate` 等入口让主代理把结果重新
翻译为 `--status`、`--message`、`--finding` 等 flags，见 [`internal/cli/cli.go`](../internal/cli/cli.go)
第 576 行。

这条路径存在三类风险：

- 漏掉 worker 返回的字段或 finding。
- 把 A dispatch 的结果记到 B dispatch。
- 主代理自行选择哪些结果值得采信。

目标系统应让 worker 输出不可变 typed receipt。adapter 原样保存原始回执，schema validator
先验证协议绑定，独立 semantic validator 再验证事实和项目边界；只有 validator receipt
可以产生推进事件。主代理只展示结果，不转录结果。

### 4.7 分片合并没有 integration queue

[`references/sliced-runs.md`](sliced-runs.md) 第 35 行明确说并行确认是提示词/流程层保证，CLI
只记录建议；第 38 行开始的合并、冲突解决和 snapshot 也由主代理执行。

多个修改 worker 即使各自在 worktree 正确完成，也不能同时写主分支。目标系统需要 repo
级串行 integration queue：

```text
queue item:
  run_id
  node_id
  artifact_commit
  expected_head
  integration_policy
  lease_epoch / token
```

Integrator 是唯一拥有主分支写权限的角色。只有当前 HEAD 等于 `expected_head` 时才集成；
否则产生 `HEAD_DRIFTED` 或 `REBASE_REQUIRED` typed event，由固定策略处理，而不是让主代理
临场决定。

## 5. 目标架构

### 5.1 总体结构

```text
用户 / 主代理 UI
        |
        | typed human event / status query
        v
+--------------------------------------------------+
| Deterministic Workflow Kernel                    |
|                                                  |
|  Event -> Reducer -> Projection + Next Effects   |
|               |              |                   |
|               |              +-> Durable Outbox  |
|               +-> WaitReason / DecisionRequest   |
+--------------------------------------------------+
        |                         |
        | immutable ticket        | integration item
        v                         v
Worker Adapters             Serial Integrator
Claude/Codex/Cline/...       expectedHead CAS
        |
        | immutable receipt
        v
Independent Validator
```

主代理从 orchestrator 降级为 UI/host adapter。这样并不是减少 AI 能力，而是把 AI 放在它擅长
的位置：理解需求、写代码、分析 diff、设计用例；顺序、重试和归属由程序处理。

### 5.2 权限边界

主代理可以：

- 提交初始需求和用户确认。
- 展示 DecisionRequest，并原样提交用户选择。
- 查询状态、启动或停止 supervisor。
- 展示 validator 产生的发现项和最终结果。

主代理不可以：

- 选择下一节点或调用任意内部 transition。
- 自己组合并行任务集合。
- 手写或修改 ticket prompt。
- 决定 runtime retry 或 recovery generation。
- 直接把 worker 输出记为 PASS/FAIL。
- 修改 workflow projection。
- 直接合并 worker 分支。

worker 可以：

- 读取一张不可变 ticket。
- 在 ticket 授权的 workspace/capability 范围内工作。
- 定期 heartbeat。
- 提交一次或重复提交相同的 immutable receipt。

worker 不可以派发下一个 worker，也不能推进 workflow state。

### 5.3 reducer

核心 API 建议为：

```go
type Reduction struct {
    State   RunProjection
    Effects []Effect
}

func Reduce(state RunProjection, event Event) (Reduction, error)
func Plan(state RunProjection, now time.Time) PlanResult
```

`Reduce` 必须是纯函数：相同 state + event 得到逐字节等价的结果。它不执行 shell、不读 Git、
不启动代理、不写文件。所有副作用被描述为 Effect，事务性写入 outbox 后由 effect handler
执行。

`Plan` 只允许返回：

```go
type PlanResult struct {
    Kind       PlanKind
    Ready      []TicketSpec
    Wait       *WaitReason
    RetryAt    *time.Time
    Operator   *OperatorReason
}
```

ReadySet 排序必须稳定，避免同一状态因 map 遍历顺序生成不同派发。

### 5.4 为什么选这套组合

下面是实施时最容易被重新争论的几个选型。先在这里记清理由，避免后续人只看到
模块名，不知道它们各自在解决什么。

**为什么是 Go + SQLite WAL，而不是直接换 LangGraph 或 Temporal？**

当前项目已经用 Go 持有大量成熟领域规则，而且是个人单机工具。SQLite 的短事务、WAL、
CAS 式更新和唯一约束已足以解决丢更新、重复 claim 和崩溃恢复。LangGraph 会引入第二
运行时，但不会自动解决 Git/shell 副作用。Temporal 的 durable execution 更强，但引入 server、
worker 和运维边界对当前规模过重。先把业务图和协议做对，未来真的需要跨机执行时仍可以
替换 scheduler/store，而不重写 reducer。

**为什么 reducer 必须是纯函数？**

因为“崩溃后能不能重放”和“同一个状态会不会偶尔走不同的路”只能在无副作用的规则
层可靠测试。如果 reducer 里顺手启动代理、写 Git 或取当前时间，重放就会再次产生副作用。
因此 reducer 只描述要做什么，outbox handler 才真正去做。

**为什么既要 event log，又要 projection？**

event log 是“发生过什么”的审计事实，projection 是“现在是什么状态”的快速索引。只有
projection 时，状态出错后无法解释来路；每次只重放 event 则日常查询与 claim 不必要地复杂。
两者在同一个 SQLite 事务中更新，既可审计，又可高效调度。

**为什么不追求 exactly-once？**

启动外部 CLI、写文件和执行 Git 无法与 SQLite 作一个原子事务。进程可能在外部操作
已成功、但“成功”尚未写回数据库时崩溃。实际可靠的做法是允许重放，再用幂等键、fencing
和对外部事实的 reconciliation 判断“是否已经做过”。这比在文档上声称 exactly-once 但无法
证明更诚实、也更安全。

**为什么 worker 结果还要独立 validator？**

worker 有能力写代码，不代表它对“自己是否完成”的判断就是权威事实。一个已超时的 worker
可能返回旧结果，一个正常 worker 也可能漏字段、误读快照或自报 PASS。schema validator 先判断
“这是不是当前 ticket 的合法回执”，semantic validator 再判断“回执里的事实和结论是否成立”。
这两步不能再交给主代理凭对话记忆转录。

### 5.5 建议的数据表

初期使用一个 `.gates/control.db` 或 run-scope SQLite 数据库，开启 WAL。建议至少包含：

| 表 | 作用 |
|---|---|
| `workflow_definitions` | 版本化静态图及其 hash |
| `runs` | run 身份、graph version、当前 projection version、终态 |
| `nodes` | 每个节点的状态、generation、输入/输出 artifact |
| `dependencies` | 静态或实例化后的依赖边 |
| `attempts` | attempt、epoch、token、lease、owner、retry 信息 |
| `events` | append-only typed events，按 run 单调 sequence |
| `outbox` | 待执行副作用及幂等键 |
| `receipts` | worker 原始回执、hash、协议验证结果 |
| `validator_receipts` | 独立 validator 的权威结果 |
| `artifacts` | spec、prompt、commit、日志等内容寻址元数据 |
| `human_decisions` | 可选项、artifact hash、回答、是否已消费 |
| `workspaces` | worktree 路径、base、owner、清理状态 |
| `integration_queue` | 串行合并项、expected head、artifact commit |

events 是可回放事实，projection 是加速查询的派生状态。每次事务应当：

1. 校验 expected projection version。
2. append event。
3. 运行 reducer 更新 projection。
4. 插入唯一 idempotency key 的 outbox effects。
5. 一次提交。

### 5.6 ticket 协议

每张 ticket 至少包含：

```json
{
  "schemaVersion": 1,
  "ticketId": "...",
  "runId": "...",
  "nodeId": "...",
  "attemptId": "...",
  "leaseEpoch": 3,
  "leaseToken": "random-secret-token",
  "leaseDeadline": "2026-08-13T12:00:00Z",
  "workflowDefinitionHash": "sha256:...",
  "requirementRevision": "sha256:...",
  "sourceSnapshot": "...",
  "expectedHead": "...",
  "promptHash": "sha256:...",
  "inputArtifacts": [],
  "resultContract": "development-receipt/v1",
  "workspace": {"id": "...", "path": "..."},
  "capabilities": ["read-repo", "write-worktree", "run-tests"]
}
```

`leaseToken` 不应写进公开日志。receipt 可以引用 token hash 或通过受保护的本地 IPC 提交。

### 5.7 receipt 与结果分类

worker receipt 需要绑定 ticket 的所有关键字段，并包含原始结果、artifact hash、修改文件、
commit、测试和运行统计。kernel 将结果分为：

| 类型 | 处理 |
|---|---|
| `SUCCESS` | 进入 validator 或 integration |
| `SEMANTIC_FAIL` | 按静态图进入 repair/decision 分支 |
| `RUNTIME_RETRYABLE` | 在预算内创建新 attempt/epoch |
| `PROTOCOL_ERROR` | 不推进，进入 NEEDS_OPERATOR 或有限协议重试 |
| `STALE_RESULT` | 记录审计事件，幂等丢弃 |
| `DUPLICATE_RESULT` | 内容相同则返回已有 receipt；不同则冲突告警 |

不要承诺 exactly-once。实际可实现且可靠的目标是：

```text
at-least-once delivery
+ idempotency key
+ fencing token
+ immutable receipt
+ reconciliation
```

### 5.8 lease、heartbeat 与 takeover

一次 attempt 的生命周期建议为：

```text
READY -> CLAIMED -> RUNNING -> RECEIPT_PENDING -> VALIDATING -> terminal
```

claim 必须原子完成：只有一个 scheduler/worker 能把 READY 更新为 CLAIMED，并生成随机 token。
heartbeat 只能延长相同 epoch/token 的 lease。

lease 过期后 reconciler 追加 `ATTEMPT_LEASE_EXPIRED`，递增节点 generation，旧 token 永久失效，
再按 retry policy 创建新 attempt。旧 worker 即使仍在运行也不能 heartbeat、提交 receipt 或
触发 cleanup/merge。

### 5.9 fan-out / fan-in

fan-out 不应当动态问模型“还需要几个审查者”。graph 在当前路线和 gate catalog 下实例化
确定成员：

```text
review_generation = 4
expected = {
  qa:blackbox,
  qa:whitebox,
  gate:complexity,
  gate:implementation-quality,
  gate:merge
}
```

generation 与 expected set 在同一事务持久化。fan-in 只计算当前 generation 的结果：

```text
received_current == expected_current
```

旧 generation 结果只保留审计，不进入 received set。expected set 非空而无 READY/RUNNING
成员时属于不变量错误，必须进入 `NEEDS_OPERATOR`，不能安静等待。

### 5.10 人工决定

所有必须由用户拍板的事项都成为 durable `DecisionRequest`：

- 需求与方案确认。
- 是否拆分、切片边界、full/custom 路线。
- 产品审 P0/P1 发现项处置。
- QA 重跑 FULL/AFFECTED。
- 额外修复轮授权。
- 外部 HEAD 漂移、协议破坏或手动取消。

请求至少包含 request ID、允许选择、解释、绑定 artifact hash、创建时间和 consumed 状态。
用户回答必须引用 request ID 和 artifact hash，防止对旧版本方案的回答被应用到新版本。

## 6. 固定业务流程建议

正式路线可以实例化为下列静态图。并行关系由图定义，不由主代理记忆：

```text
INTAKE
  -> REQUIREMENT_CONFIRMATION [human]
  -> PRODUCT_REVIEW [worker + validator]
  -> START_READINESS [worker + validator]
  -> SLICING_DECISION [human]
  -> ROUTE_DECISION [human]
  -> PREPARE_WORKSPACES
  -> parallel {
       DEVELOPMENT
       BLACKBOX_QA_DESIGN
       BLACKBOX_QA_REVIEW
     }
  -> DEVELOPMENT_SNAPSHOT
  -> review fan-out {
       BLACKBOX_QA_EXECUTION
       WHITEBOX_QA_DESIGN -> WHITEBOX_QA_REVIEW -> WHITEBOX_QA_EXECUTION
       SELECTED_GATES...
     }
  -> REVIEW_JOIN
  -> if blocking findings {
       REPAIR_AUTHORIZATION? -> REPAIR -> SNAPSHOT -> review fan-out
     }
  -> INTEGRATION_QUEUE? [sliced runs]
  -> SEAL
```

lightweight 仍可作为另一张非常短的静态图：

```text
START -> REQUIREMENT_RECORD -> SEAL_WITHOUT_VALIDATION
```

它不是普通图里靠大量 skip 拼出的特例。

## 7. 迁移方案

这次重构不适合一次性替换。建议按 P0/P1/P2 推进，每一步都能独立验证和回退。

### P0：先拿走主代理的自由编排权

目标：先消灭错序入口和无声死端，暂不改 worker backend。

1. 把现有流程整理为版本化静态 graph。
2. 实现纯 `Plan`/`Reduce`，先以内存结构工作。
3. 为所有可达状态做 model/property tests。
4. 新增唯一公开推进接口：

   ```text
   formal-gates workflow advance --json
   ```

5. 输出只允许五种 PlanResult。
6. `prepare-*`、`claim-dispatch`、`record-*` 暂时保留，但只允许 advance 内部调用。
7. 旧 CLI 可增加 compatibility mode，用于现有 run；新 run 默认拒绝直接调用内部命令。

P0 完成后，即使状态仍存 JSON，主代理也不能自行选择任意下一步。

**P0 退出条件：**

- 新 run 只有一个公开推进入口，直接调用内部 transition 会被拒绝。
- 对每个已枚举非终态，`Plan` 都返回 ready、wait、retry 或 operator 中的一种，不存在空结果。
- 同一 fixture 连续运行 `Plan` 产生完全一致且排序稳定的 ticket specs。
- 现有关键 transition/Seal 语义已被 characterization tests 锁定，差异都有明确变更说明。
- 制造“漏派一个并行门”的历史故障时，planner 会补出缺失 ready item 或进入明确错误，
  不会无声等待。

### P1：建立 durable control plane

目标：解决崩溃恢复、重复投递、失联 worker 和迟到结果。

1. 引入 SQLite WAL。
2. 先做双写：现有 state JSON + events/projection，持续比较 divergence。
3. event replay 能从空库重建 projection。
4. events、projection、outbox 同事务提交。
5. 建立唯一 scheduler/reconciler。
6. 引入 attempt、lease deadline、epoch、token 和 heartbeat。
7. 所有 receipt、heartbeat、cancel、cleanup 都做 token fencing。
8. supervisor 重启后扫描：

   - 未投递 outbox
   - READY/CLAIMED/RUNNING attempts
   - 过期 lease
   - 已生成但未验证 receipt
   - 待消费 human decision

9. 定义 bounded retry：runtime 次数、总 wall time、成本和重复工具循环上限。

P1 完成后，kill supervisor 不应导致人工猜测下一步。

**P1 退出条件：**

- 从空 projection 重放 event log，可重建与运行中完全一致的状态。
- 在 outbox 插入、worker 启动、receipt 提交等故障点强制杀进程后，重启可自动继续。
- 两个 scheduler 竞争只有一个能 claim；旧 epoch 的 heartbeat、receipt、cancel 和 cleanup 全部失效。
- 重复投递相同 effect/receipt 不改变最终状态；同一幂等键内容不同则进入冲突告警。
- 每种自动重试都有可查询的次数、时间或成本上限；超限后进入 `NEEDS_OPERATOR`，不继续循环。

### P2：控制执行质量和 Git 集成

目标：让多 worker 并行不会破坏主分支，也不需要主代理采信结果。

1. 所有 worker 输出 JSON Schema typed receipt。
2. adapter 原样摄入，主代理不再转录 flags。
3. 独立 validator 验证协议、范围、事实、严重度和复现。
4. 每个修改节点使用独立 worktree 和 capability scope。
5. 建立 repo 级串行 integration queue。
6. integration item 使用 `expectedHead` CAS。
7. 记录 artifact provenance：ticket -> receipt -> validator -> commit -> merge。
8. 增加 dashboard，但 dashboard 只能显示/提交 human decisions，不能绕过 reducer。
9. 支持 Claude Code、Codex、Cline、OpenHands、mini-swe 等 adapter。

**P2 退出条件：**

- 主代理不再把 worker 文本翻译成 `record-*` flags；原始 receipt 和其 hash 可完整追溯。
- 缺字段、绑定错误、超范围修改、迟到或重复结果都能被协议层稳定分类，不能推进状态。
- 两个并行修改 worker 不能直接写 main；只有 integrator 能按队列和 `expectedHead` 集成。
- 主分支漂移、合并冲突和集成后验证失败均产生 typed event，不由主代理现场编排。
- 任意一个支持的 worker adapter 都可在不改 workflow definition/reducer 的前提下替换。

## 8. 如何验证重构真的解决问题

普通 happy-path 单元测试不够。必须证明控制面在随机顺序、崩溃和重复投递下仍保持不变量。

### 8.1 planner/reducer 测试

- 所有可达非终态都有 ReadySet 或 WaitReason。
- 同一 state 多次 `Plan` 输出完全一致。
- ReadySet 排序稳定。
- 非法顺序永远不会生成 ticket。
- fan-out expected set 完整且不可在同 generation 变化。
- fan-in 只接受当前 generation。
- lightweight 和 formal 图不会串线。

建议用基于属性的随机事件序列和小规模状态空间遍历，而不仅是手写几个例子。

### 8.2 crash/restart 注入

在以下位置 kill supervisor，然后重启：

- event append 前后。
- projection 更新前后。
- outbox 插入前后。
- worker launch 前后。
- worker 完成、receipt 提交、validator 完成前后。
- lease 刚过期时。
- human decision 写入或消费前后。
- integration merge 前后。

预期结果：不丢 ticket、不重复推进、旧 epoch 不污染新状态、outbox 可重放，最终到 COMPLETE
或明确 NEEDS_OPERATOR。

### 8.3 并发测试

- 两个 scheduler 同时 claim 同一 attempt。
- 两个 worker 重复提交相同 receipt。
- takeover 后旧 worker 提交不同 receipt。
- 多个 QA/门以随机顺序完成。
- 两个 integration item 同时看到同一个 HEAD。

预期：单 owner、duplicate 幂等、stale 永拒、projection 不丢更新、merge 单占用。

### 8.4 协议测试

- malformed JSON。
- ticket/attempt ID 缺失或错误。
- epoch/token 错误。
- requirement revision、source snapshot、prompt hash 错误。
- 未知 result status。
- 修改超出 capability scope。

这些都不能推进 workflow。协议错误应进入明确结果，而不是让主代理“看起来差不多就记了”。

### 8.5 本项目边界

验证仍遵守 [`prompts/reviewer-base.md`](../prompts/reviewer-base.md) 的既有项目边界：保证文档
化需求的正常使用和常见操作失误；对抗性输入、恶意编辑、手工篡改状态、权限/不可变文件
失败、不支持平台等只作 P3 建议，不阻塞 PASS。

控制面测试要重点覆盖正常崩溃、重复消息、并发和常见误操作；不需要为了防恶意数据库篡改
把个人项目做成复杂分布式系统。

## 9. 需要保留、替换和删除什么

### 保留并迁移

- transition 和 Seal 领域规则。
- requirement revision、catalog/prompt hash、source snapshot 绑定。
- VCS resolver 和 snapshot/diff 语义。
- QA mode 隔离、blackbox worktree 和用例增量语义。
- reviewer-base 边界、finding/severity 规则和 semantic validators。
- cost parser、provider/lifecycle adapter 中可观察外部事实的部分。

### 替换

- `RunState JSON + mutateRun + 自制陈旧锁` -> SQLite event/projection transaction。
- `ParallelAdvice` -> graph fan-out + durable expected set/outbox。
- `ResumeReport/reset` 人工恢复 -> reconciler + typed operator state。
- `PreparedDispatch status` -> node/attempt/lease/epoch/token。
- 主代理转录结果 -> immutable receipt + independent validator。
- 提示词约定合并 -> serial integration queue + expectedHead CAS。

### 最终删除或内部化

- 主代理可直接访问的 `prepare-*`、`claim-dispatch`、`record-*`。
- 让主代理选择 runtime retry/recovery 的入口。
- 仅靠 stderr 提醒并行不足的正确性机制。
- 没有 owner token 的陈旧锁接管。
- worker 或主代理直接写主分支的路径。

## 10. 实施时的模块边界建议

建议在 Go 内逐步形成以下包，不必一开始过度抽象：

```text
internal/workflow/definition   静态图、节点类型、版本/hash
internal/workflow/reducer      Event -> Projection + Effects
internal/workflow/planner      Projection -> PlanResult
internal/control/store         SQLite event/projection/outbox transaction
internal/control/scheduler     claim、lease、heartbeat、retry、reconcile
internal/control/protocol      ticket/receipt schemas 与验证
internal/control/workspace     worktree 生命周期和 capability scope
internal/control/integration   repo 级 merge queue、expectedHead CAS
internal/adapters              claude/codex/cline/openhands/mini-swe
```

现有 `internal/validate` 不要立即拆光。P0 可以先从中调用纯规则；等 reducer 测试稳定后，再按
责任迁移，避免同时改业务规则和持久化模型。

## 11. 明确不做什么

- 不用另一个 LLM 代替当前主代理做全局协调。
- 不以更多 Markdown 指令作为流程正确性的主要保证。
- 不追求 exactly-once 外部副作用。
- 不在第一阶段引入 Kubernetes、消息队列或多节点数据库。
- 不因为 LangGraph 有 checkpoint 就假设 shell/Git 副作用可安全重放。
- 不让 worker 自报完成直接推进流程。
- 不为了保留旧 CLI 永久维护两套控制权；兼容层只用于迁移现有 run。

## 12. 推荐的第一批实施任务

进入编码前，应先形成正式需求和架构决策记录。第一批代码任务建议严格限制为：

1. 写出 v1 静态 graph 和节点/事件枚举，不接 worker。
2. 实现纯 `Reduce`/`Plan`。
3. 把现有 transition/Seal 规则作为 characterization tests 固定下来。
4. 遍历小规模可达状态，证明无声死端不存在。
5. 新增只读 `workflow plan --json`，与旧流程 shadow 对比。
6. 在若干真实历史 run fixture 上比较旧状态与新 planner 输出。

这批任务通过后，再进入 SQLite/outbox。不要第一步就同时改状态库、scheduler、worker adapter
和 merge queue，否则出现偏差时无法判断是领域图错了，还是持久化/并发实现错了。

## 13. 最终判断

formal-gates 的现有问题不是规则太少，而是**规则只在主代理选择动作之后生效**。真正可靠
的流水线需要让系统先算出唯一合法动作，再让 worker 去执行；AI 负责语义，普通代码负责
控制。

重构成功后的使用体验应当是：用户仍然和主代理对话，但主代理不再“记流程”。它只是把
用户决定提交给 kernel，把 kernel 发出的 ticket 交给 worker，再把持久状态展示给用户。
即使主代理换会话、worker 崩溃、结果重复、回执迟到或 supervisor 重启，系统也能从数据库
事实恢复，并且只会继续、明确等待或明确报错，不会无声卡死。

这就是“封死的道路”的工程含义：不是不允许失败，而是每一种失败都有唯一、可持久化、
可测试的出口。

## 14. 主要一手来源

- Superpowers：<https://github.com/obra/superpowers>
- LangGraph persistence：<https://docs.langchain.com/oss/python/langgraph/persistence>
- LangGraph interrupts：<https://docs.langchain.com/oss/python/langgraph/interrupts>
- LangGraph fault tolerance：<https://docs.langchain.com/oss/python/langgraph/fault-tolerance>
- Cline：<https://github.com/cline/cline>
- mini-swe-agent：<https://github.com/SWE-agent/mini-swe-agent>
- OpenHands：<https://github.com/OpenHands/OpenHands>
- OpenHands SDK：<https://github.com/OpenHands/software-agent-sdk>
- OpenHands Automation：<https://github.com/OpenHands/automation>
- Gas Town：<https://github.com/gastownhall/gastown>
- Gas Town architecture：<https://github.com/gastownhall/gastown/blob/main/docs/design/architecture.md>
- bmad-loop：<https://github.com/bmad-code-org/bmad-loop>
- Paperclip：<https://github.com/paperclipai/paperclip>
- GitHub Spec Kit：<https://github.com/github/spec-kit>
- OpenSpec：<https://github.com/Fission-AI/OpenSpec>
- GSD（已于 2026-06-26 归档）：<https://github.com/gsd-build/get-shit-done>
- Kilo Code：<https://github.com/Kilo-Org/kilocode>
- OpenCode：<https://github.com/anomalyco/opencode>
- Oh-My-ClaudeCode：<https://github.com/Yeachan-Heo/oh-my-claudecode>
- Symphony：<https://github.com/openai/symphony>
- Temporal workflow execution（可靠工作流语义参考）：<https://docs.temporal.io/workflow-execution>
- Temporal activity definition（副作用边界参考）：<https://docs.temporal.io/activity-definition>
