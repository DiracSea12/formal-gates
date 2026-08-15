# 错误处理 / 回溯 / 半截恢复 调研

> 调研时间：2026-08。用途：硬流水线（见 `RESEARCH-HARD-PIPELINE.md`）的错误与恢复语义。
> 覆盖三个问题：步骤出错怎么办（错误处理）；走错后怎么办（回溯 / 补偿）；中途被打断怎么办（半截恢复）。

## 贯穿结论

**日志是唯一真相源，状态从日志推导。**

- 错误怎么分 → 决定该重试还是该硬停；
- 回溯怎么做 → 走错后把游标退到分支点、只撤销那一段；
- 半截怎么续 → 中断后从日志尾部重放，不重头再来。

## 1. 错误处理：判定权放在「定义时的错误路由表」

业界判别「可恢复 vs 硬停」分三档（能力递增）：

1. **计数式**（Airflow / Prefect / CrewAI）：任何异常消耗一次重试名额，错误类型不参与判定。
   最弱——「走错步骤」这类必须硬停的错误，纯靠次数不够。
2. **抛出方类型化**（Temporal `nonRetryable` / LangGraph `retry_on` 白名单）：写步骤的人抛出时带类型，
   编排器按类型对号入座。**坑**：最外层再包一层普通异常会丢掉类型标志，导致被静默重试。
3. **声明式错误路由表**（AWS Step Functions）：定义时写死「错误名 → Retry(退避策略) 还是 Catch(跳转)」，
   运行时零猜测。`MaxAttempts:0` 表达「永不重试」；`States.ALL` 兜底且必须最后；
   `States.HeartbeatTimeout` 单独成名（心跳停止可走与普通超时不同的策略）。

**结论**：可恢复 / 硬停的判定权放在**定义时的声明表**，抛出方用类型对号入座，编排层只执行表、不猜测。

配套纪律：

- **硬停走专用通道**：阻塞式护栏（校验先于执行、副作用发生前拦截——OpenAI Agents SDK guardrail
  阻塞模式的语义），不是等重试耗尽再兜底。
- **编排层永不自动重试「走错」**：只有叶子基础设施调用（网络 / 超时 / 限流）可重试。
- **终态分家**：走错 = Failed（带证据、事件日志留痕）；用户中止 = Canceled（给清理机会）；
  强杀 = Terminated（无清理机会）。
- **钩子只绑终态**（Prefect / Airflow 共同教训）：`on_failure` 在重试耗尽后才触发，中间 attempt 不打扰人。
- **长时子代理用心跳**：心跳超时区分「卡死 vs 慢」；判死可配置为不重试、直接转人工。
- **重试副作用纪律**：重试前清空失败 attempt 的部分写入（LangGraph `writes.clear()`）、
  确认目标步幂等、层外加成本 / 步数硬上限（agentic 自愈会无限耗预算）。

## 2. 回溯：默认不全量回滚，「精确撤销 + 从分支点重放」

- **Saga / 补偿**（微软 Compensating Transaction、microservices.io）：每步记录「如何撤销」；
  失败时倒带逐一对已完成步骤抵消。补偿**幂等**、不要求还原原状、单点失败不等于全量回滚、
  补偿本身可续跑（resumable compensation）。
- **Temporal**：没有 undo——版本化（`GetVersion`）兼容在途执行；Reset（复制历史到 reset 点、
  丢弃其后，用当前代码重放）；RequestCancel（可跑清理逻辑）vs Terminate（强杀）。
  非确定性是硬故障（NondeterminismError），只能终止 + 重跑。
- **Step Functions**：无内置回滚；Catch → 显式补偿分支；RedriveExecution 从失败步续跑
  （同一执行、保留成功产物、事件历史追加、clientToken 幂等键）。
- **git 三层语义**：revert（逆变化提交，安全）/ reset（丢尾部，危险）/ rebase、git replay
  （变更重放到新基点）——「从分支点重放」就是 rebase 语义。
- **Airflow / Prefect**：只重跑失败任务、复用成功产物（backfill `rerun_failed_tasks`、
  Prefect retry 保留原 run ID 跳过已缓存任务——前提是结果持久化 `persist_result`）。
- **LangGraph**：`Command(resume, update, goto)` 一步完成「续跑 + 改状态 + 跳回分叉点」；
  但「回到分叉点」必须产生持久化 fork checkpoint，纯内存恢复有非持久化回滚窗口。
- **Claude Code 原生 checkpoint 不追踪 bash 改的文件** → 文件级快照必须自建（git 承担）。

**结论**：走错一步的默认处置 = **只撤销那一步（对 diff 做逆操作）+ 从分支点重放**，成功产物保留。
全量 reset 只在两种情形用：① 错误污染前置步骤（上游不可信）；② 非确定性 / 版本不兼容（历史无法重放）。
日志永不删（append-only），失败执行留在历史供审计，回溯本身也写成新日志事件。

## 3. 半截恢复：默认续跑，重开只是兜底

**恢复点两种来源**：

- **纯日志推导**：Temporal（重放到历史末尾）、Step Functions（Redrive）、Claude Code（转录 JSONL）；
- **显式 checkpoint + 日志**：Airflow（DB 里每个 task 状态）、Prefect（`persist_result`）。

**幂等是地基**：唯一 run id 启动去重（Step Functions 执行名、Temporal WorkflowIdReusePolicy）+
每类副作用配幂等键（Stripe 范式：唯一约束 / 原子 insert + 缓存响应 + 同 key 不同参数 409 +
有界保留期 + 后台回收 reaper）。

**中断不可能正好落在步骤边界**：心跳 / 看门狗超时判死（Temporal 心跳、Airflow zombie task、
Celery `acks_late`），判死后重发，副作用必须可重入（at-least-once）。

**「必须重头再来」的条件写死**：恢复点缺失 / 确定性被破坏 / 超过保留窗口。

**Claude Code 相关**（直接影响主代理恢复）：

- `--resume` 重载 JSONL 转录；上下文耗尽时插入压缩摘要（`isCompactSummary`），摘要即显式
  checkpoint；`PreCompact` / `PostCompact` hook 是官方落盘钩子。
- **坑**：resume 不重放 hook；hook 注入消息 fork 了 `parentUuid` 链会静默得到残缺历史；
  resume 后 hook payload 的 session_id 会错位。
- → **编排器自己的状态必须由 hook 在事件发生时落盘，恢复时只读自己的事件日志，
  不靠读 LLM 转录重建**。

## 4. 上层生态与 AI 框架的实现对照（2026-08 补充）

| 本调研的机制 | 上层项目/框架的实现 |
|---|---|
| 完成/授权不推断 | claude-orchestrator 的 JSON 哨兵契约（`ORCHESTRATOR_RESULT: SUCCESS/BLOCKED/NEEDS_RETRY_CONTEXT`，DONE 必须验证命令确认）；LangGraph `Command(resume=<类型化值>)`（approve/reject/edit/respond）——静默 ≠ 同意 |
| 半截恢复 | brimstone 的 durable bead 文件（重启/限流可续）+ watchdog 僵尸检测；LangGraph checkpointer 每 superstep 落盘，resume 时节点**从顶重跑**——副作用必须幂等（放 interrupt 后或独立节点、upsert 代替 insert），interrupt 按索引严格匹配；MetaGPT 类型化 Document 在 ProjectRepo 持久化、交接处 schema 校验 |
| 询问纪律 | LangGraph `interrupt_before` = 编译期声明暂停点（对应「必问点清单」的机械实现）；MetaGPT 固定流水线（BY_ORDER）里没有「问不问」的选项 |
| 错误路由 | LangGraph RetryPolicy/error_handler（重试策略与补偿解耦）；MetaGPT schema 交接校验（错误在类型化接口处被拦，幻觉/返修率约减半） |
| 状态外置 | 全部一致：checkpointer / 任务文件 / bead 文件 / ProjectRepo——模型不持有「走到哪了」 |

诚实的差异：MetaGPT 的 SOP 遵守仍靠提示词（角色可能越界，靠流水线结构 + schema 交接 + 执行反馈兜底）；上层编排项目（orchestrator/brimstone）与 LangGraph 的图把遵守层放进程序。formal-gates 的目标是后者强度。

## 合并结论（五条）

1. **事件日志（append-only）唯一真相源**：每步转出即追加幂等事件；恢复点 = 日志尾部最后一个成功步骤，自动推导。
2. **每步一张声明式错误路由表**：错误名写死可重试 / 硬停；策略违规默认硬停转人工，阻塞式护栏拦在副作用前。
3. **走错一步 = 精确撤销那一步 + 从分支点重放**（补偿幂等 + 文件 diff 级事件日志支撑精确撤销）；
   全量 reset 是污染前置 / 确定性破坏时的最后手段。
4. **默认续跑、重开兜底**：唯一 run id + 幂等键 + 心跳判死 + 有界保留期。
5. **编排器状态由 hook 在事件发生时落盘**，不靠 resume / 转录重建。

## 出处

- 微软补偿事务模式：https://learn.microsoft.com/en-us/azure/architecture/patterns/compensating-transaction
- microservices.io Saga：https://microservices.io/patterns/data/saga.html
- Temporal：versioning https://docs.temporal.io/develop/go/workflows/versioning 、
  determinism https://github.com/temporalio/claude-temporal-plugin/blob/main/skills/temporal-developer/references/core/determinism.md 、
  error handling https://docs.temporal.io/best-practices/error-handling
- AWS Step Functions：error handling https://docs.aws.amazon.com/step-functions/latest/dg/concepts-error-handling.html 、
  RedriveExecution https://docs.aws.amazon.com/step-functions/latest/apireference/API_RedriveExecution.html
- Airflow backfill / rerun：https://airflow.apache.org/docs/apache-airflow/stable/core-concepts/backfill.html
- Prefect retry flow runs：https://docs.prefect.io/v3/how-to-guides/workflows/retry-flow-runs
- Stripe 幂等请求：https://docs.stripe.com/api/idempotent_requests
- LangGraph fault tolerance：https://docs.langchain.com/oss/python/langgraph/fault-tolerance
- OpenAI Agents SDK guardrails：https://openai.github.io/openai-agents-python/guardrails/
- Claude Code：checkpointing https://code.claude.com/docs/en/checkpointing 、
  sessions https://code.claude.com/docs/en/sessions 、hooks https://code.claude.com/docs/en/hooks
- LangGraph interrupts：https://docs.langchain.com/oss/javascript/langgraph/interrupts
- LangGraph checkpointers：https://docs.langchain.com/oss/python/langgraph/checkpointers
- claude-orchestrator：https://github.com/dhananjay-kaushik/claude-orchestrator
- brimstone：https://github.com/bread-wood/brimstone
- MetaGPT Software Development Workflow：https://deepwiki.com/FoundationAgents/MetaGPT/5-software-development-workflow
