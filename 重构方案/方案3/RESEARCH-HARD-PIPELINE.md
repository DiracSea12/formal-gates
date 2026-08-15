# 硬流水线（确定性编排）调研

> 调研时间：2026-08。用途：为「把主代理（LLM 编排者）的开发流程做成确定性硬流水线」提供业界依据。
> 配套调研：错误处理 / 回溯 / 半截恢复见 `RESEARCH-ERROR-RECOVERY.md`。

## 问题背景

formal-gates 的实际运行中，几乎每条流程都会出现主代理（LLM 编排者）的编排错误，导致 CLI 流程卡死。
方案设想分两道防线：

1. **防线一**：每步结束显式告诉主代理下一步只能做什么；
2. **防线二**：主代理仍按错误步骤执行时，一律硬阻断。

核心问题：这种「封死的道路」要怎么做——像执行线性程序一样、用硬阻断 hook 覆盖全程，还是别的方案？

### 编排错误的精确分类（本项目要消灭的对象）

主代理（LLM 编排者）的编排错误归为三类，根因相同：**决策权在模型手里**。

1. **路线选择错**：流程走哪条路（轻量/正式、下一步做什么），靠模型「感觉」而不是程序查表。
2. **询问纪律错**：该不该问用户，靠模型「心情」而不是程序权限判定（allow/ask/deny）。
3. **授权推定错**：有没有被授权/完成，靠模型「脑补」（如把沉默当同意）而不是显式信号。

业界统一答案（2026-08 调研收敛）：**模型只执行程序发下的指令，所有「选路、问不问、授权有没有」都是程序状态的机械函数，不经过模型判断**。

## 术语定位

- **loop engineering**：agent 内部循环（模型自省、重试、工具循环）的工程化——**不是**本项目的问题域。
- **graph engineering**：外部步骤图 / 状态机（节点、边、编排）的工程化——**是**本项目的问题域。
- 相关叫法：deterministic orchestration（确定性编排）、guardrails（护栏）、state machine + transition table。

## 业界机制

### Claude Code 原生门禁能力

- **PreToolUse hook**：`exit 2` 或 JSON `permissionDecision: deny/allow/ask/defer` 可硬阻断；
  在权限检查之前运行，`bypassPermissions` 下也生效。旧字段 `blockPermission` 已废弃，
  改用 `decision:"block"`（≈ exit 2）或 `permissionDecision:"ask"`。
- **Stop hook**：通过 `additionalContext` 注入下一步提示（防线一的实现通道）。
- **失败即放行陷阱**：`exit 1` / `3` / `127`（命令不存在）/ 无效 JSON / hook 超时 全部**放行**；
  只有 `exit 2` 或 deny 是硬阻断。hook 判定出错的最坏情况是「没拦住」，不是「误杀」。
- **headless 模式**：`claude -p`、`--output-format json/stream-json`、`--allowedTools`、
  `--permission-mode dontAsk`、`--resume <session-id>`、`--max-turns`、`--bare`、`--input-format stream-json`。
- **已知缺陷**：deny 在 ExitPlanMode 上被忽略；deny 原因只展示给 Claude 不给用户；
  `@` 文件引用不触发 PreToolUse。安装/接线必须对照这些坑实测验证。

### 确定性编排范式（工作流引擎）

| 系统 | 机制 | 要点 |
|---|---|---|
| Airflow | 解析期 DAG 图校验 | 图在部署时定义，错了根本跑不起来 |
| Prefect | 状态机 REJECT / ABORT / WAIT | 非法迁移被状态机拒绝 |
| Temporal | 事件溯源 + 命令历史逐条比对 | 重放不一致即 NondeterminismError，历史是真相源 |
| Step Functions | ASL 部署期 InvalidDefinition | 非法状态机定义无法部署 |

共同点：**「下一步是什么」由声明/代码决定，运行时零猜测**。

### Guardrail 实践

- 门禁必须是**程序**，不是 LLM 裁判（NeMo Guardrails、OWASP LLM06 "complete mediation"、
  Anthropic "supervise what it's able to do"）。
- 93% 审批疲劳：把「下一步对不对」交给用户或模型判定 = 必然失效。
- 结论：确定性强制由代码承担，模型只做边界内的事。

### Claude Code 上层生态（编排/工作量类项目，2026-08 查证）

建立在 Claude Code 之上的编排/工作量项目按解法分四派，共同点：**把编排挪出主代理的自由会话**。

| 派别 | 代表项目 | 机制 |
|---|---|---|
| 任务驱动 | [claude-task-master](https://github.com/eyaltoledano/claude-task-master) | PRD→程序生成任务文件（依赖/优先级/顺序），主代理只「读下一个任务、执行、更新状态」，程序持队列 |
| 状态机/流水线 | [claude-orchestrator](https://github.com/dhananjay-kaushik/claude-orchestrator) | 严格 Execution Contract：plan 交互、run 走 `claude -p` headless；JSON 哨兵 `ORCHESTRATOR_RESULT: SUCCESS/BLOCKED/NEEDS_RETRY_CONTEXT`，**DONE 绝不从输出推断、必须验证命令确认** |
| 状态机/流水线 | [brimstone](https://github.com/bread-wood/brimstone) | 五阶段流水线（plan→research→design→scope→impl）逐段 headless + 隔离 worktree + durable 状态文件（bead files，重启/限流可续）+ watchdog 僵尸检测 |
| 图驱动 | [claude-flow](https://github.com/ruvnet/claude-flow) | 节点+边定义成图，框架遍历执行，agent 是节点执行者；headless workers 并行 |
| 护栏 | [claude-guardrails](https://github.com/dwarvesf/claude-guardrails) / [agent-guardrails](https://github.com/roboticforce/agent-guardrails) / [Rulebricks](https://explore.market.dev/ecosystems/python/projects/claude-code-guardrails) | PreToolUse 拦截→决策表 allow/deny/ask（带条件逻辑，如 `rm -rf node_modules` 允许、别处拒绝）→审计日志；静态 deny 规则硬阻断 |
| 护栏 | [logi-cmd/agent-guardrails](https://github.com/logi-cmd/agent-guardrails) | 「task briefs」声明允许触碰的文件范围，检测范围违规 |
| 治理 | [lisa](https://github.com/codyswanngt/lisa) | 护栏+规则+skills+hooks 做成对项目自身的治理框架 |

### AI 工程框架（LangGraph / LangChain / MetaGPT，2026-08 查证）

三个框架是「graph engineering」的正面教材：**编排错误没有发生入口，因为没有「自由会话主代理」这个角色**。

- **LangGraph**：StateGraph = 声明式迁移表——节点与边是代码，执行是确定性图遍历，LLM 只在节点内被调用；没有边就没有路径，「跳步」物理上不可能。`interrupt()` = 编译期声明的必问点（`interrupt_before/After` 编译选项），只有宿主发 `Command(resume=<值>)` 才能继续，resume 值是类型化的（approve/reject/edit/respond）——「问」和「答」都是程序通道。checkpointer 每个 superstep 落盘，`getStateHistory` = 事件日志 + 时间旅行，模型不持有「走到哪了」。纪律被编码成硬规则：不把 interrupt 包进 try/catch、resume 按索引严格匹配、非确定性循环用条件边代替。
- **LangChain**：LCEL 声明式链（`.pipe()` 组合），流水线是程序、模型填节点输出；LangSmith 全量 trace = 观测侧事件日志；v1 的编排职责整体下沉到 LangGraph。
- **MetaGPT**：「Code = SOP(Team)」——角色 + 固定 SOP 流水线（需求→PRD→设计→任务→代码→测试），每段产出类型化 Document、交接处 schema 校验（官方：幻觉/返修率约减半）；消息池 pub-sub，角色用 watch 列表声明订阅，「谁下一步」由订阅规则决定；react 模式显式分 BY_ORDER（固定顺序）/ REACT（LLM 自选）/ PLAN_AND_ACT。已知弱点：SOP 遵守仍靠提示词，角色可能越界——兜底是流水线结构 + schema 交接 + 执行反馈，不是硬阻断。

## 收敛原则（五条）

1. **声明式迁移表 + 独立校验器**（Step Functions / Airflow 模式）。
2. **append-only 事件日志 = 状态真相源**（Temporal 模式）。
3. **每步状态机 + 合法迁移白名单**。
4. **幂等重跑**：唯一 run id、重跑先清下游（Airflow `tasks clear` 模式）。
5. **分层失败 + 心跳 + 强制终止**：终止必须有，防永远卡死。

**警示**：声明本身可能错——漏了一条边，主代理仍可能走到非法步骤。
该场景的兜底见错误/恢复调研：非法步骤的检测、阻断与恢复。

## 四件套机制（落地方案）

1. **状态文件 / 事件日志**（append-only）；
2. **迁移表**：当前状态 → 允许的下一步集合；
3. **唯一门命令**：每次迁移前由 CLI 校验前置条件（代码里检查，不靠 LLM 自觉）；
4. **PreToolUse 墙**（阻断非法工具调用）+ **Stop 注入**（提示下一步）。

**形式 A**：零 hook 驱动（线性程序执行，流程代码本身就是驱动，天然无歧义）。
**形式 B**：双 hook 交互会话（PreToolUse 阻断 + Stop 注入下一步，保留人机对话便利）。

**本方案建议（2026-08）**：硬流水线以**程序驱动（形式 A）**为核心原则——模型执行程序发下的指令，不持有「下一步」选择权。上层生态与 AI 框架（见上两节）一致收敛于此。上层实现补足三个机制：

- **完成/授权不推断**：显式哨兵（`ORCHESTRATOR_RESULT` / `Command(resume=<类型化值>)`），程序里没有「推断完成/同意」的分支——没有显式信号就是未完成/未授权；
- **必问点 = 编译期声明**：interrupt-before 的等价物，暂停点写死在流程定义里，不是模型临场决定；
- **每步独立 headless 会话**：程序按迁移表启动每一步（`claude -p`），程序不启动的步骤不存在，「跳步」物理不可能。

少 hook 含义：程序驱动后模型不持有选择权，hook 只剩窄判定面（决策委托 CLI 状态机）。

## 同令牌纪律

状态名 = 门条件名 = 注入的下一步文本，必须从**同一个源**生成
（对应 GitHub required-check 的教训：检查名与定义不一致即静默失效），避免对不上号。

## 对本项目可迁移要点

- 门禁判定放在代码 / CLI 里；主代理只执行门命令，不自行判断下一步。
- 每步转出即追加事件日志；下一步只从迁移表推导。
- 非法迁移：硬阻断（exit 2 / deny），阻断信息必须包含「现在该做什么」。
- 全部机制对照 Claude Code hooks / headless 的坑安装验证（失败即放行、已知缺陷）。

## 出处

- Temporal 事件历史与确定性：https://docs.temporal.io/encyclopedia/event-history
- Step Functions ASL 错误处理：https://docs.aws.amazon.com/step-functions/latest/dg/concepts-error-handling.html
- Prefect 状态机：https://docs.prefect.io/v3/concepts/states
- Anthropic Claude Code hooks：https://code.claude.com/docs/en/hooks
- OWASP LLM06（complete mediation）：https://owasp.org/www-project-top-10-for-large-language-model-applications/
- NeMo Guardrails：https://github.com/NVIDIA/NeMo-Guardrails
- Anthropic 关于「supervise what it's able to do」：https://www.anthropic.com/engineering/building-effective-agents
- claude-task-master：https://github.com/eyaltoledano/claude-task-master
- claude-orchestrator：https://github.com/dhananjay-kaushik/claude-orchestrator
- brimstone：https://github.com/bread-wood/brimstone
- claude-flow：https://github.com/ruvnet/claude-flow
- Awesome Claude Code - Orchestrators：https://mintlify.wiki/hesreallyhim/awesome-claude-code/tooling/orchestrators
- LangGraph interrupts：https://docs.langchain.com/oss/javascript/langgraph/interrupts
- LangGraph checkpointers：https://docs.langchain.com/oss/python/langgraph/checkpointers
- LangGraph v1：https://docs.langchain.com/oss/javascript/releases/langgraph-v1
- MetaGPT Core Multi-Agent Framework：https://deepwiki.com/FoundationAgents/MetaGPT/2-core-multi-agent-framework
- MetaGPT Software Development Workflow：https://deepwiki.com/FoundationAgents/MetaGPT/5-software-development-workflow
