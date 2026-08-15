# 编排流水线化重构调研报告

> 生成日期：2026-08-13
> 来源：主代理派发的三份并行子代理调研（本地编排现状深度分析、海外生态、中文生态 + 工作流引擎），全部经 web-access skill 联网核实；附录 A/B/C 保留调研原文与证据链接。
> 目标：回答「如何把 formal-gates 的编排流水线化，让编排像封死的道路，没有犯错空间」。
> 状态：调研报告，**不是已确认的需求**。需求确认与实现如需走 formal-gates 正式流程，由用户提出。

---

## 一、核心结论

业界（含 2026 年最新学术与社区实践）的共识：**编排决策不允许 LLM 碰——「模型干活，确定性门禁判完成」**。

- TRON（2026-07，Zenodo）：确定性有限状态编排器，不变式原文 "the model builds, but a deterministic gate decides done — we trust neither the agent's report nor the commit"；预注册 74 次交付运行，主配置 30/34 干净（88%），6/6 对抗性假完成 fixture 全部被 gate 拒绝，4 次非干净运行全部 fail-safe。
- Rel(AI)Build（2026-06，arXiv 2606.26924，1 万仓库 6145 份 agent 配置研究）："Governance of this layer must be deterministic and tool-agnostic — not delegated to further LLM orchestration"。
- Anthropic《Building Effective Agents》：任务歧义低到能预画决策树，就建成 workflow——准确率、可控性、成本都优于 agent。

**本项目特殊诊断**：CLI 状态机已经非常接近确定性内核（27 个子命令、完整性哈希、原子写、写阻断 hook、规范提示词写盘双验、并行阶段表、Seal 幂等续跑全部已存在）。问题不在缺机制，而在**执行环路上还站着主代理**——状态机每步之间「谁来按什么顺序调用哪个命令、怎么派发子代理」全靠主代理 LLM 临场判断。重构不是推倒重来，而是**把主代理从执行链路里抽出来，让一个确定性引擎接管环路**。

---

## 二、同类项目调研（按对位程度排序）

| 项目 | 关键机制 | 对本项目的借鉴 |
|---|---|---|
| [@agentfare/forge](https://github.com/MjxUpUp/Forge)（最对位，Go 实现、中文作者） | 退出码契约（`BLOCKED:` 非 0 硬阻断 / `ADVISORY:` 0 软信号，按退出码而非文案行动）；验收标准可执行化（`task start --accept "go test ./... :: PASS"`，`verify-acceptance` 实跑并记录 deterministic 证据）；cheat-scan 机械扫描 `@ts-ignore`/空 catch/`if(false)`/纯注释修复（此前靠 LLM 子代理判断、每轮抓不同子集，抽成确定性扫描后一次判准）；read-before-edit 硬阻断（编辑本会话未 Read 过的现存源文件直接 BLOCKED）；监控**文件**而非工具（file-sentinel 对比 git 状态，防 `node -e writeFile` 绕道）；三层纵深防御 Hook；高危命令 HITL（`forge hazard confirm` 登记 5 分钟限时标记）；审查由独立只读子 agent 执行 | 门禁判定必须机器可判；「agent 自述完成」永远降级为不可信输入 |
| [peaks-cli](https://github.com/SquabbyZ/peaks-cli)（→[peaks-loop](https://github.com/SquabbyZ/peaks-loop)） | PreToolUse hook 在权限检查**之前**拦截（连跳过权限标志都绕不过）；`permissionDecision: "deny"` + exit 2 阻塞语义；SOP 每个 phase 挂「可检查条件」（file-exists / grep / 命令退出码）；peaks-mut 变异测试防假绿（默认杀灭率≥80%、弱断言≤5%）；peaks-context 由 CLI 强制采集上下文；gate bypass 一次性、记原因、每 SOP 每项目上限 3 次 | hook 是真硬约束；紧急放行必须有次数上限；「技能=流程的大脑；CLI=流程的骨节」 |
| [cairn](https://www.npmjs.com/package/@jh-cairn/cairn)（中文作者，仓库在极狐 GitLab） | 跨会话状态机 + **推进动词唯一化**（只有 `cairn next` 能前进，输出 DECIDE/WAIT/STUCK 三类）；append-only 事件流审计（events.jsonl 每条带链式 hash）；核心命令仅三个（start/next/status） | 流程只从一个入口前进，主代理无处「即兴发挥」 |
| [@squadkit/squad](https://www.npmjs.com/package/@squadkit/squad) | JSON recipe 定义 agent DAG（name/prompt/dependsOn/model/allowedTools/maxBudgetUsd/timeoutSec）→ 拓扑排序 → 每层并行 spawn headless `claude -p` → 每个 agent 产物写入 `.squad/runs/<ts>/artifacts/<name>.md` → 后续 prompt 内联引用前置产物 | 确定性流水线 + 文件交接的最小完整实现，直接对应 formal-gates 想做的事 |
| Auto-Orchestrate（[GitHub](https://github.com/riba-tshepo/Auto-Orchestrate)） | 单一命令驱动 11 阶段混合流水线；**阶段单调性**（只能推进到下一个未完成阶段，AUTO-001..007）；8 个人门禁=文件轮询（`gate-pending-<id>.json` / `gate-approval-<id>.json`，人写文件即放行）；checkpoint.json（schema 版本化 + 原子写）崩溃恢复与断点续跑；每阶段落 stage-receipt.json；约束 orchestrator 只许委派不许亲自实现；no-auto-commit | 阶段单调性正是「封死的道路」的写法；收据化产出 |
| [task-master](https://github.com/eyaltoledano/claude-task-master)（28k⭐） | MCP server（36 工具按 core/standard/all 分级）+ CLI；PRD → parse-prd 生成任务 JSON 状态文件 → 依赖图 → `next_task`/`set_task_status` 带依赖阻断校验的确定性状态迁移 | 状态存文件、迁移走带校验的工具——主代理无法「假装」状态 |
| [spec-kit](https://github.com/github/spec-kit)（127k⭐，GitHub 官方） | 固定阶段序列（constitute → specify → clarify → plan → tasks → analyze → implement → converge）；每阶段产出版本化 artifact 文件作为下一阶段唯一输入；analyze 跨产物一致性检查、converge 对照 spec/plan/tasks 审计实现差距 | artifact 即契约——阶段之间不靠对话记忆，靠文件 |
| [OpenSpec](https://github.com/Fission-AI/OpenSpec)（65k⭐） | `/opsx:propose` → changes/ 目录（proposal+specs+design+tasks）→ 人审 → apply → archive；刻意不做 rigid phase gates（自称 fluid not rigid） | 与 spec-kit 构成「重门禁 vs 轻门禁」两极 |
| [wow-harness→Flowness](https://github.com/NatureBlueee/wow-harness)（中文作者） | 核心主张「指令遵从率约 20%，Hook 执行率 100%」；8-Gate 状态机（★=独立审查者，工具清单物理上没有 Edit/Write）；「分离判断与执行」（lead skill 只判 Gate 转移不写代码，harness-dev 只执行已冻结计划）；**「Agent 不拥有 flow，flow 临时组装 agent」**；工作状态持久化为 Objects/Events/Obligations/Judgments/Evidence/History；Assurance Kernel（执行→独立审查→定向返工→重新验收）；「Human out of the session, never out of the constitution」；完成多态 **"Built ≠ Integrated ≠ Activated ≠ Accepted"** | 主代理降级为 flow 的一个临时组件；完成判定多态 |
| [claude-orchestrate](https://github.com/midego1/claude-orchestrate) | 插件市场分发；evidence-gated verification——原话 "a sub-agent's 'done!' is never trusted"；两级验证器（fast haiku 初筛 + deep sonnet 对照 spec 深验）+ 硬重试预算 + 全局派发上限 | 验证器必须对照验收标准给出证据；重试有预算上限 |
| [claude-code-workflow-orchestration](https://github.com/barkain/claude-code-workflow-orchestration) | PreToolUse hook 拦截主代理直接执行工具、强制走 delegate；PostToolUse Python 脚本 Ruff/Pyright 硬阻断验证 | hooks 是「主代理乱来」的官方拦截点（注意它用的是软 nudge 而非硬 block） |
| [Ruflo](https://github.com/ruvnet/ruflo)（67.8k⭐，2026 增长最快） | CLI + MCP server + 27 hooks + 33 插件；**merge 前五道 CI 门禁**（lint/tsc/单测下限/smoke/**witness manifest 校验**）+ semver 强制；每次 merge `ruflo sign` 生成签名 witness manifest，安装后 `ruflo verify` 校验「安装字节与审计足迹一致」，不匹配禁止使用 | 与 formal-gates 的 Seal 几乎同构——加密清单证明替代「主代理声称状态正确」 |

**参考类（不对位，仅模式借鉴）**：claude-squad（8.3k⭐）与 Vibe Kanban（27.8k⭐，**已宣布关停**）的 git worktree 每任务隔离 + 人审查后才推送；OpenHands（84k⭐）event-sourced runtime（容器死掉可从事件日志重建状态）、2026-05 Enterprise Agent Control Plane；Goose（53k⭐）extensions（确定性脚本）+ recipes（YAML 工作流 DSL）作为一等扩展；Factory Droids 默认 read-only、需主动提升 autonomy；GitHub Agent HQ——agent 只产出 draft PR 等可审 artifact、永不自合。**关停信号**：纯「外层工作台」形态竞争不过 agent 内置能力，编排层必须自己掌握状态机与门禁。

**官方底座（一手文档核实）**：
- Claude Code Hooks：17 个事件；**exit code 2 = 阻断并把 stderr 回灌给 Claude**（exit 1 不阻断——官方原话警告「如果你想强制策略，用 exit 2」）；hooks 在 subagent 内同样触发；支持政策钩子。
- Agent Teams：任务三态 + 依赖（未完成依赖不可认领）+ 文件锁；TeammateIdle/TaskCreated/TaskCompleted 钩子 exit 2 即质量门禁；**已知限制：无法 resume、每会话一个 team、不可嵌套**——确定性流水线更稳的底座是 subagents + hooks + 文件状态 + worktree 隔离，而非动态组队。
- Anthropic [multi-agent research system](https://www.anthropic.com/engineering/built-multi-agent-research-system)（2025-06 工程博客）：plan 持久化到外部 Memory（防上下文截断失忆）；subagent 返回结构化产物；生产可靠性原话 "We combine the adaptability of AI agents … **with deterministic safeguards like retry logic and regular checkpoints**"；**end-state evaluation + 离散 checkpoint 校验**（不逐步验中间过程）；**subagent 直接写文件系统避免「传话游戏」**。

---

## 三、工作流引擎通用模式（Temporal / LangGraph / Prefect / Dapr）

| 模式 | 机制 | 迁移到 agent 编排的要点 |
|---|---|---|
| 持久化状态 + 事件溯源 + 重放（durable execution） | Temporal/Cadence event history + replay：worker 崩溃后从最后完成点重放，**不重跑已完成的 activity**；OpenAI 内部用 Temporal 跑 Codex（生产级）；2025-07 Temporal 与 OpenAI 合作发布 Agents SDK 集成 | 确定性 workflow（agent 循环/分支/目标检查）+ 非确定性 activity（每次 LLM 调用都是独立 activity、结果进 history）；崩溃后**重放已记录的 LLM 决策而不是重做决策**；对本地 CLI 项目，SQLite/JSONL 状态文件 + 幂等重放可实现同样语义，无需重型引擎 |
| 显式状态机 + checkpoint（FSM） | LangGraph StateGraph（checkpointer 每 super-step 边界持久化，interrupt_before + Command(resume) + 时间旅行）；Prefect orchestration policies（规则阻止非法状态转移，官方定位「agents as state machines rather than static DAGs」） | 中文社区实测（金融风控 17 次卡死案例）：**LLM 只做结构化字段抽取，所有决策由硬编码状态机按字段组合跳转**；FSM 解决回环；局限：分支超 20 状态爆炸 |
| 人工节点 = 持久化挂起 + 外部信号恢复 | 所有成熟引擎一致：挂起零计算占用、状态落盘、外部信号（task token / signal / interrupt+resume）精确唤醒到挂起点、自动审计；Step Functions Standard 支持人工暂停最长 1 年 | 反模式：阻塞式等待占用 worker、人工任务无超时 |
| saga 补偿 + 幂等 | 部分失败时按逆序执行补偿（每步声明 action + compensation）；重试与补偿本身都必须幂等（exactly-once activity / step ID 去重） | agent 世界「回滚」最现实形态 = git revert + worktree/隔离区；补偿数据与已执行补偿清单要持久化 |
| 确定性控制面与 LLM 分层（2025-2026 学术支撑） | TRON / Rel(AI)Build / Buildplane 同一结论：LLM 不能放在控制路径上；Elasticsearch 实践：LLM 只做意图抽取，确定性策略层构造受治理查询 | 「最坏情况是策略少触发几条，而不是无界查询执行」 |

**反例**：Airflow 静态 DAG、无环、sensor 占 worker 槽——batch 调度器不适合 agent 循环；HumanLayer 关灯工厂实验失败（RL 只奖励「编译通过」，try-catch 包住出错行，架构成本数月后爆发）——「'有人同意了'不等于'这件事应该发生'」。

---

## 四、本项目现状诊断（本地深度分析结论）

### 已确定性化的资产（引擎的现成底座）

- **CLI 命令面**：27 个子命令覆盖 run 生命周期（start/show/resume/abort/reset/cleanup）、需求（requirement）、拆分/路线（route-candidates/slicing/settle-findings/route）、派发（prepare-action/prepare-gate/claim-dispatch/record-action/record-gate）、QA（qa-worktree/qa-design/qa-review/qa-execution/qa-execution-scope）、收尾（snapshot/carry/authorize-repair/seal）、外围（install/hook decide/lifecycle/gate run/canary）。
- **状态与持久化**：`.gates/tmp/<run-id>/state.json` 唯一临时状态，sha256 完整性 + 严格 schema + 进程锁 + 原子写；状态机全在 `workflow_transition.go`（每操作一条 requireTransition）；规范派发提示词写盘即验 hash + 认领兜底再验；lifecycle 绑定与中断原因记录；seal/abort ledger + 黑盒用例物化。
- **写阻断 hook**：活动 run 下主代理/审查类代理写代码或 run 状态被拒（按真实写目标判定）。
- **并行阶段表** `parallelStageTable`：已算出每阶段「应并行任务集」，但目前**只 stderr 提醒、不强制**——这是引擎化的最大现成资产。
- **成本计量**：转写解析，仅展示。

### 脆弱点 = 主代理在环路中的 7 个执行点

1. **spawn 派发**（最容易手抖：手写提示词、认领时序）——prepare→claim→spawn→record 链路每一环已有 CLI 校验，唯独 spawn 由主代理执行；CLI 保证「文件未被篡改」，保证不了「主代理只发薄启动消息、没口头补充锚定内容」。
2. **start 的 base/current 语义取值**（CLI 只验形式，base 该取「交付前基线」还是 HEAD 仍是 LLM 决策；常规情形其实无需决策）。
3. **主代理结果核查**（复现路径/正常使用边界/因果——全流程最大单一 LLM 判断点，无第二层监督）。
4. **carry 的 INHERIT/RERUN 判定**（因果链推断，CLI 只验形式完整性）。
5. **QA scope 推荐**（FULL vs AFFECTED 及子集）。
6. **abort/reset 的 flag 由主代理代传**——机器无法验证「用户真的确认过」（半确定性天花板）。
7. **上下文紧张反复重启**（纯纪律，无机制防御）。

已由确定性守卫封死的坑（不再脆弱）：快照时序（AdvanceSnapshot 拒空快照）、需求 revision 漂移硬阻断、claim 时序（放宽为「源快照是 HEAD 祖先」）、派发提示词篡改（双 hash 验）。

---

## 五、重构方案：formal-gates engine（确定性编排引擎）

在现有 CLI 上加一个引擎主循环 `formal-gates engine run`，推进入口唯一化（cairn 式）。改造后：

```
engine run ──读 state.json──→ 动作表查下一步（复用 parallelStageTable + requireTransition）
    │
    ├─ 该派发子代理 → 机械执行 prepare→claim→headless spawn（薄消息=规范文件路径）→收 receipt 文件→record
    ├─ 该等人 → 写 pending-human-decision 文件挂起；用户经 CLI 子命令/文件信号放行（不再由主代理代传）
    ├─ 该跑门禁 → 跑确定性谓词（命令退出码/grep/文件存在），认文件不认话
    └─ 失败 → fail-closed 停在 checkpoint；engine run 重跑即幂等续跑
```

### 设计要点

1. **派发全自动**——prepare→claim→spawn→record 由引擎机械执行，主代理彻底退出这条链路。脆弱点 1、6 直接消失；手写派发提示词类错误物理上不可能发生。
2. **人门文件化**——用户确认（settle、route、snapshot 放行）改为状态文件信号，主代理无法「擅自替你确认」（Auto-Orchestrate 文件轮询式）。
3. **产出即 receipt**——子代理结果全部落盘为文件，门禁只认文件；主上下文只收摘要 + 引用，同时解决提示词漂移与上下文爆炸（Anthropic 官方附录同款结论）。
4. **LLM 保留区（有界、受控）**：
   - 需求澄清/确认——用户面对面对话，本来就该用户拍板；
   - 审查判定——已是独立零上下文审查者，设计核心，不动；
   - 结果核查——从「主代理即时判断」改为**引擎固定派发的 verifier + 证据契约**（BLOCKED/ADVISORY 式退出码，Forge 式）；
   - carry 判定——用 `PreRepairSnapshot..CurrentSnapshot` 的 diff 路径做粗筛，只命中候选路径时才需要 LLM 判定；
   - start 非常规情形（接管/拆分/差异化路线）——引擎询问用户，而不是 LLM 猜。
5. **失败语义 fail-closed**——宁可升级/卡住，也不关闭没做完的活（TRON 范本）；checkpoint + 幂等续跑复用现有 resume 基础。
6. **加固项**：
   - abort/reset 改要求**用户口令 token**（CLI 记录来源，机器代传不了）；
   - 命令入口**版本自检**（二进制版本与 skill 文档/状态 schema 版本匹配，堵 PATH 旧二进制坑）；
   - Seal 增加 **witness manifest**（对齐 Ruflo sign/verify，产物签名清单 + verify 校验）；
   - （可选）cheat-scan 机械扫描（`@ts-ignore`/空 catch/纯注释修复）。
7. **主代理新角色**——用户接口 + 开发 worker。skill 文档相应改写：**flow 拥有编排，agent 被 flow 临时组装**（Flowness 原则）；「Built ≠ Integrated ≠ Activated ≠ Accepted」完成多态作为阶段推进判定的语义基础。

### 落地分三步（复用现有资产，几乎不新造机制）

- **第一步：引擎接管派发环路**——收益最大，纯资产复用（parallelStageTable 从「提醒」升级为「强制」），脆弱点 1/6/7 消失。保留手工模式作兜底（canary 已有验证路径）。
- **第二步：人门文件化 + 决策结构化**——start 语义、QA scope、carry、核查 verifier 全部改为引擎询问用户或结构化预计算。
- **第三步：加固**——token 确认、版本自检、witness manifest、（可选）cheat-scan。

### 不建议动的部分

需求澄清/确认（必须用户面对面）、产品审/技术审的判定本身（独立零上下文审查者是设计核心）、Seal/squash/ledger/成本合并（已完全确定性且刚修过持久化缺陷）、门文件自由格式（项目明确禁止注册表/元数据）。

---

## 六、待确认事项

1. 方案方向是否认可（引擎接管环路、主代理降级）；
2. 是否从第一步（引擎派发环路）开始做；
3. 实现是否走 formal-gates 正式流程（由用户提出）。

---

## 附录 A：本地编排现状深度分析（子代理报告原文）

（只读分析，全部结论以 2026-08-13 仓库事实为准）

### A.1 编排架构地图：全流程控制点清单

流程九步定义唯一持有在 `SKILL.md`（命令形式在 `references/formal-flow.md`），执行机制唯一持有在 Go CLI（`internal/validate/workflow*.go`）。控制点逐一定性如下。

**阶段 0：受理（start 之前）**

| 控制点 | 保证方式 | 证据 |
|---|---|---|
| 触发提醒 / 大需求二次强调 | **纯 LLM**（主代理自觉，无任何机制） | `SKILL.md`「开发受理流程」、全局 CLAUDE.md |
| 需求澄清拷问、确认、写需求文件 | **纯 LLM**（CLI 不参与；requirements-clarification 动作已被收窄为"受理登记"） | `prompts/actions/requirements-clarification.md` |
| 决定是否进入正式流程 | **纯 LLM + 用户**（无 CLI） | `SKILL.md` 第 42-48 行 |
| 需求文件内容 → revision hash | **确定性**（sha256，行尾规范化） | `runstate.go` `RequirementRevision` |

**阶段 1-3：start / 需求登记 / 拆分与路线**

| 控制点 | 保证方式 | 证据 |
|---|---|---|
| `--split yes/no` 强制声明、与 retained-overall/master 的一致性 | **确定性**（start 拒绝） | `workflow.go` `Start` 104-139 行 |
| base/current 快照的祖先/相等关系 | **确定性** | `Start` 161-183 行 |
| base/current **语义取值**（base=交付前基线还是 HEAD） | **LLM**（记忆里"start 一次设对"的坑；CLI 只验形式） | `Start` |
| requirements-clarification PASS 才能 `--confirmed` | **确定性** | `workflow.go` `UpdateRequirement` 565-569 行 |
| 需求文档改动后 revision 漂移硬阻断（每个流程命令入口都查） | **确定性** | `requireCurrentDefinitions`（workflow.go 1615-1635） |
| 漂移后的三分支分类（preserved/changed） | **LLM 判语义**，CLI 强制后果（changed 作废全部结果；开发后 preserved 必须带 `--confirmed`） | `UpdateRequirement` 528-560 行 |
| 重绑前必须记录在途派发结果（防死锁） | **确定性** | `requireNoUnrecordedInFlightDispatch` |
| 拆分建议内容（理由/怎么拆/并行性/改拆后果） | **LLM 生成**；CLI 只验形式（no-split 必须留原因、split≥2、切片拓扑与 master 一致） | `RecordSlicing` |
| 拆分决定与 start 声明互相校验 | **确定性** | `RecordSlicing` 693-702 行 |
| 切片继承整体审的输入一致性（需求 revision、目录 revision、product/start-readiness 提示词 hash 全匹配） | **确定性**（01e2f42 新增） | `requireMatchingInheritedReviewInputs` |
| 路线选择（full/custom/lightweight、逐切片） | **用户拍板**，CLI 验选择合法性；路线**推荐**是 LLM | `SetRoute` |

**阶段 4：开发之前（产品审 Part1 → start-readiness Part2）**

| 控制点 | 保证方式 | 证据 |
|---|---|---|
| Part1 先于 Part2 | **确定性**（transition） | `requireStartReadinessTransition` |
| P0/P1/P2/P3 分级、仅 P2/P3 可 PASS、FAIL 必须含 P0/P1 | **确定性** | `validateSemanticResult` |
| 复审规则：确认的 P0/P1 → 置"需重审"标记、重审前 PASS 被拒；驳回作废 | **确定性** | `enforceReviewRule`、`RecordSettledFindings` |
| 用户逐项处置发现项（confirm/dismiss） | **用户拍板**；发现项 message 必须精确出现在已记录结果里 | `settle()` 880-881 行 |
| 已拍板清单注入下次派发（审查者不再重提） | **确定性**（提示词组装） | `workflow_prompt.go` `actionPromptDetail` |
| 增量审查 `--scope` 逐项判定（全判、PASS 项不可改判） | **确定性** | `recordReviewItems` |
| **审查判断本身（需求是否合理/方案是否最小充分）** | **LLM**（独立零上下文审查者） | `prompts/actions/product-review.md`、`start-readiness.md` |

**阶段 5-6：开发与快照**

| 控制点 | 保证方式 | 证据 |
|---|---|---|
| 开发子代理只做已确认范围 | **提示词约束**（LLM）+ 写入隔离靠 VCS 快照门 | `development-worker.md` |
| 黑盒 QA 隔离工作区恒等于基线 | **确定性**（登记时原生标识==基线 + 注入需求 hash 校验） | `RegisterQAWorktree` |
| claim 与提交时序 | **确定性放宽**：开发派发认领接受"源快照是 HEAD 祖先" | `ClaimDispatch` 990-999 行 |
| 快照必须真实前进（防"没提交就快照"） | **确定性**（`currentSnapshot == state.CurrentSnapshot` 即拒） | `AdvanceSnapshot` 1411 行 |
| 快照黑盒门（开发完成 且 黑盒 qa-review PASS 双边） | **确定性**；用户显式 `--user-requested` 可放行并记录 | `AdvanceSnapshot` 1439-1447 行 |
| 工作树干净才记录快照 | **确定性** | `vcs.go` `verifySnapshotReady` |
| **开发内容本身** | **LLM**（development-worker） | |

**阶段 7-8：开发后审查与修复**

| 控制点 | 保证方式 | 证据 |
|---|---|---|
| 审查者输入=CLI 组装提示词（零上下文） | **确定性组装** + 污染检查是 **LLM 自判**（"是否构成锚定由你自行判断，不做机械化块校验"） | `reviewer-base.md` 第一步 |
| 门必须报告 `compared` 快照对，与要求不符结果被丢弃 | **确定性** | `RecordGate` 1357-1362 行 |
| 冻结需求产物不得作为发现项位置 | **确定性** | `rejectFrozenArtifactFindings` |
| QA 用例真增量（--case-id/--remove-case/--replace-all、语义重复检测、review 派发准备后锁定） | **确定性** | `workflow_qa.go` `RecordQADesign` |
| 用例集充分性 | **LLM**（qa-review set-level 判定，明确"不设机械化质量下限"） | `qa-review.md` |
| 白盒用例↔测试绑定 | **确定性校验形式**（非空/1:1），存在性/对应性靠 qa-review + qa-execution | `runstate.go` QACase.Test 注释 |
| QA 执行重跑必须先记 scope 决策（prepare 前强制） | **确定性** | `PrepareAction` 101-107 行 |
| scope 推荐（FULL vs AFFECTED）与 AFFECTED 子集 | **LLM**（host 综合判定+推荐，稍保守）；子集派发前定死、执行者不得改 | `qa-execution.md`、`qaExecutionAffectedScope` |
| 每个被选 mode 都必须记录执行（防静默跳过） | **确定性** | `selectedQAModesRecorded`、`requireSelectedResultsResolved` |
| 审查轮次上限（3 次 + authorize-repair 每次恰好 1 轮） | **确定性** | `effectiveReviewWaveLimit`、`AuthorizeExtraRepair` |
| CARRY_FORWARD 沿用规则 | **确定性**（条件判定在 CLI） | `bundleRerunScopes` |
| **继承判定 INHERIT/RERUN** | **LLM**（主代理快捷或独立 carry 派发）；CLI 验"全部 eligible 门都判、理由必填" | `RecordCarry` |
| 主代理对修复内容的核查、派发前的便宜构建/测试 | **LLM 执行**（命令由 SKILL 规定） | `formal-flow.md` 修复流程 |
| **主代理对审查结果的核查（需求匹配、正常使用边界、复现路径）** | **纯 LLM 判断**（SKILL 明确定义为"编排层面的核查"，CLI 只做结构校验） | `SKILL.md`「结果校验与修复上限」 |

**阶段 9：Seal**

| 控制点 | 保证方式 | 证据 |
|---|---|---|
| 前后两次原生标识一致 | **确定性** | `Seal` 287-309 行 |
| 每个被选结果 PASS 或已授权跳过；FAIL 跳过须轮次耗尽或 SEAL-USER | **确定性** | `authorizeSealSkips`、`requireSelectedResultsResolved` |
| Git 多提交自动 squash（工作树干净、--squash-message 必填） | **确定性**（VCS 操作自动执行） | `Seal` 312-336 行 |
| 黑盒用例落盘回主干 | **确定性**（CLI 物化，不经代理手抄） | `materializeBlackboxCases` |
| 终态先持久化、summary 失败可幂等续跑、cleanup 只在 summary 存在后删 temp | **确定性**（01e2f42 修复的核心） | `completeTerminalRun`、`CleanupTempRuns` |

**跨阶段不变量**

| 控制点 | 保证方式 | 证据 |
|---|---|---|
| run 状态只能 CLI 写（sha256 StateIntegrity + schema 严格 + 文件锁 + 原子写） | **确定性** | `runstate.go` `SaveRunState`/`LoadRunState`、`acquireStateLock` |
| 派发提示词唯一权威=规范文件（写盘即验 hash + 认领兜底再验） | **确定性** | `writeCanonicalPromptFile`、`verifyCanonicalPromptFile` |
| 审查者身份绑定与生命周期配对（start+stop 事件、身份不可复用） | **确定性** | `ClaimDispatch`、`requireLifecycleVerification`、`journal.go` |
| 中断原因自动记录 + 续用三分支强制 | **确定性** | `requireResumeInterrupted` |
| 同功能并行去重、OPEN 空票作废、STALE 恢复记录防双记 | **确定性** | `enforceSameFunctionDedup`、`requirePreparedDispatch` |
| 主代理/审查类代理写阻断（活动 run 下） | **确定性 hook**（按 agent_type + 写目标判定，CLI 命令与登记需求文档豁免） | `write_block.go` |
| abort/reset 需用户级 flag | **半确定性**：CLI 硬要 flag，但 flag 由主代理代传，机器无法验证"用户真的确认过" | `runWorkflowAbort`/`runWorkflowReset` |
| 并行调度 | **半确定性**：CLI 按硬编码阶段表计算"应并行集"并在 stderr 提醒（带冷却），实际并行派发是 LLM 行为 | `parallel.go` |
| 成本计量 | **确定性**（转写解析，仅展示） | `internal/cost/` |

### A.2 脆弱点清单：记忆失误类别 → 代码路径 → 现有防线

1. **手写派发提示词（曾填错 revision/SHA 致 RUNTIME_ERROR）**——防：`prepare-*` 把完整提示词写 `.gates/tmp/<run-id>/prompts/<dispatch-id>.md` 并记录 hash，写盘即验、`claim-dispatch` 兜底再验。**残余脆弱**：CLI 保证"文件未被篡改"，保证不了"主代理真的只发薄启动消息、没在口头补充锚定内容"——仍靠 `reviewer-base.md` 污染自检（LLM 自判）+ SKILL 纪律。流水线化后此点可完全消除。
2. **保险性 abort / 误中止 master run**——防：`workflow abort` 无 `--user-confirm` 直接拒绝；reset 同理需 `--user-approve`。**残余脆弱**：flag 是主代理代传的，"确认"的真伪机器不可验——这是当前 abort 防护的天花板。
3. **快照时序（先 adopt-external 导致快照无事可记）**——已由确定性守卫封死（`AdvanceSnapshot` 硬性要求 `currentSnapshot != state.CurrentSnapshot`）。
4. **start 的 base/current 一次设对**——防：祖先/相等校验、`--split` 声明与 master 交叉校验、切片与 master 全量一致性校验（01e2f42）。**残余脆弱**：base 语义取值仍是 LLM 决策。
5. **需求文档改动后必须重确认（revision 过期）**——已完全确定性化（hash 漂移后第一个依赖需求的命令即硬阻断并指引 `requirement --meaning`；仅 `--confirmed` 不能更新已变更 revision）。
6. **测试/临时操作污染仓库**——防：活动 run 下 write-block hook（按真实写目标判定）；QA 隔离工作区 + `.gates/tmp` gitignored。**残余脆弱**：无活动 run 时无保护。
7. **上下文紧张反复重启**——无任何机制防御，纯 LLM 纪律。
8. **claim 时序（须在开发提交前认领）**——已放宽为"开发派发认领接受源快照是 HEAD 祖先"，不再硬失败。
9. **PATH 旧二进制（缺新 flag）**——无运行时版本自检；schema 严格校验能拦部分旧二进制，反向靠必填字段兜底。残余纯操作纪律。
10. **长会话每次 CLI 操作前核对**——纯纪律；abort/reset 的 flag 门是唯一机器兜底。

另两个固有 LLM 判断点：**结果核查**（SKILL「结果校验与修复上限」，全流程最大的单一 LLM 判断点，无第二层机制监督）与**继承判定**（carry 的 INHERIT/RERUN 是因果链推断，CLI 只验形式完整性）。

### A.3 已可机器校验但主代理未被强制走的环节

1. **并行派发**：`parallelStageTable` 已算出每阶段"应并行任务集"，目前只 stderr 提醒，不强制。一个 runner 完全可以直接按该表自动 prepare+claim 全部待办。
2. **派发执行本身**：prepare → claim → spawn → record 每一环都已有确定性 CLI 校验，唯独"spawn 子代理"由主代理 LLM 执行。若由 runner 代劳（读规范文件生成薄消息/直接调用 Agent 工具），整条链路即可无人干预。
3. **QA scope 决策**：CLI 已强制"重跑前必须记录决策"，但决策内容是 LLM。FAIL 用例集是确定性的，"受影响的既往通过用例"需判断。
4. **主代理结果核查**：SKILL 规定必须做，无 CLI 校验点——但它是固定步骤，可形式化为标准派发。
5. **start 的常规情形**：`start --current-snapshot` 不传即取 HEAD、base 取当前值，常规情形无需 LLM 决策；接管/重审情形才有语义选择。

### A.4 复用资产清单（流水线 runner 的现成底座）

`requireTransition` 状态机、`mutateRun`（锁+完整性）、`writeCanonicalPromptFile`/`verifyCanonicalPromptFile`、`lifecycle` 日志与中断原因、`parallelStageTable`、`internal/cost` 转写解析、VCS resolver（git/svn/p4 统一接口）、`write_block` hook、install/managed-rules/canary 三件套。

---

## 附录 B：海外生态调研（子代理报告原文）

**调研方式说明**：全部联网操作经 web-access skill 处理。初始 CDP 浏览器通道因需人工授权超时失败，用户授权后恢复；静态层（GitHub API、raw.githubusercontent、直接 curl）与 CDP 浏览器层并用。少数目标仍有障碍（developers.openai.com 被 Cloudflare 403 拒绝、Amp 官方文档为 SPA 无法抓取正文、openai.com 旧博客 404）。星数与活跃度均来自 2026-08-13 GitHub API 实测。

### B.1 项目逐个梳理（详见正文「二」的汇总表）

构建在 Claude Code 之上的第三方开源项目（核心同类）：Ruflo（67.8k⭐，五道 CI 门禁 + witness manifest sign/verify）、claude-squad（8.3k⭐，worktree 隔离 + 人审查后推送）、Vibe Kanban（27.8k⭐，已关停）、spec-kit（126.9k⭐，artifact 即契约）、OpenSpec（64.7k⭐，fluid 轻门禁）、task-master（28k⭐，状态文件 + 带校验迁移）、@squadkit/squad（JSON recipe DAG）、Auto-Orchestrate（阶段单调性 + 文件人门禁 + receipt）、claude-orchestrate（证据门禁）、barkain/claude-code-workflow-orchestration（hooks 强制委派）、claude-code-router（36.6k⭐，本地控制面）、Orchestra、Symphony、claude-manifests、ORC、claude-agentic-framework 等。

独立平台 / 官方机制（参考类，已按口径降权）：opcode（196.9k⭐，独立终端 agent 非 CC 编排层；plan/build 双 agent）、OpenHands（83.9k⭐，event-sourced runtime；2026-05 Enterprise Agent Control Plane；2026-06 Agent Canvas 反向驱动 Claude Code/Codex/Gemini CLI）、Goose（52.8k⭐，extensions + recipes + MCP）、Factory Droids（闭源，默认 read-only）、Sourcegraph Amp（闭源，thread 即 checkpoint、Oracle 第二意见、每次修改后增量验证）、GitHub Agent HQ（agent 只产出 draft PR 永不自合）、Anthropic multi-agent research system（plan 持久化外部 Memory + 确定性重试/checkpoint + end-state evaluation + subagent 直写文件系统）、Claude Code Hooks/Agent Teams/AGENTS.md、OpenAI Codex hooks/exec policy/auto-review。

### B.2 被核实为主流的 5 个核心架构模式

**模式一：文件化/版本化状态 + checkpoint + 断点续跑（幂等状态机）**——证据：Anthropic 官方博客（"deterministic safeguards like retry logic and regular checkpoints"）；Auto-Orchestrate（checkpoint.json 原子写 + schema 版本 + 自动续）；OpenHands（event-sourced runtime）；Amp（thread 即 checkpoint）；task-master（状态文件 + 带校验迁移工具）。要点：状态是文件/事件日志而非 LLM 记忆；每次迁移可校验；任何中断可从最近状态恢复——"流程卡死"与"时序错乱"的对症解。

**模式二：强制验证门禁（CI 式 gate，不信任"done!"）**——证据：Ruflo 五门禁 + 签名清单；claude-orchestrate（"a sub-agent's 'done!' is never trusted"）；Anthropic（CitationAgent 终检 + end-state 校验）；Codex（sandbox 审批路由 reviewer agent 自动审）；GitHub Agent HQ（agent 永不自合）；spec-kit（analyze/converge 一致性审计）。要点：产出必须通过**确定性脚本**才能进入下一阶段或写回主分支；LLM 自述完成不算数。

**模式三：工具/权限面最小化作为行为约束**——证据：Claude Code subagents 的 `tools` allowlist（社区最佳实践给 reviewer 只配 Read/Grep/Glob）；Factory Droids 默认 read-only；opencode plan agent 拒绝编辑；Auto-Orchestrate auditor 只读。要点：**权限范围就是行为契约**——reviewer 物理上改不了代码，主代理也就无法"顺手改掉审查对象"。

**模式四：文件 artifact 交接代替对话交接**——证据：Anthropic 官方附录（"Subagent output to a filesystem to minimize the game of telephone"）；squadkit 产物目录 + dependsOn；Auto-Orchestrate stage-receipt.json；spec-kit/OpenSpec 文件工作流；Agent Teams mailbox/task 均为校验过的 JSON 文件。要点：阶段间唯一契约是落盘文件，防"传话失真"与上下文爆炸。

**模式五：隔离工作区（worktree/沙箱）作为并行安全底座**——证据：claude-squad 与 Vibe Kanban（每任务 git worktree）；Codex worktrees；OpenHands 沙箱；社区大规模迁移案例（750k 行迁移按 worktree fan-out + 独立只读 reviewer，99.8% 测试通过）。要点：隔离是并行的前提，也是"误改污染主状态"的物理防线。

**补充模式（底座能力）**：hooks 作为全生命周期可编程拦截点——Claude Code 官方 17 事件 + exit 2 硬阻断 + subagent 内同样生效 + 政策钩子，是第三方在 CC 上做确定性门禁的官方通道。

### B.3 对 formal-gates 的直接建议（海外视角）

1. 把流程状态从主代理记忆下沉到版本化状态文件（Auto-Orchestrate + task-master + OpenHands）；状态迁移只允许通过带校验的工具，阶段单调性——禁止跳步/回跳；主代理只读状态、提议动作，动作合法性由确定性代码判定。
2. 每阶段产出强制落盘为 receipt/artifact，gate 只认文件不认话；门禁脚本通过才允许状态推进；主上下文只回传摘要 + 文件引用。
3. 用 hooks 做硬门禁而非提示词软约束（官方 exit 2 语义）；"状态未达 X 不允许 Edit/写主分支"、"审查未过不允许派发下一阶段"。
4. 验证器分级 + 证据门禁（claude-orchestrate）：快速廉价校验先筛，深验后审，重试设硬预算；reviewer 子代理用 tools allowlist 锁成只读。
5. 人门禁用文件信号而非聊天内对话。
6. Seal 对齐 Ruflo witness manifest：发布/merge 对产物做签名清单、安装后 verify 校验——密码学证明替代"主代理声称状态正确"。
7. 底座选择：Agent Teams 仍 experimental 且限制多，确定性流水线更稳的底座是 **subagents + hooks + 文件状态 + worktree 隔离**。
8. 借鉴但防坑：官方自己承认"编码任务并行度低、多 agent 协调差"；Ruflo 的共识拓扑对单项目编排过度——先做单机确定性流水线（squadkit 尺度）。

**未经一手核实的结论清单（诚实标注）**：Amp 架构细节（docs 为 SPA，仅二手来源）；Codex AGENTS.md workflows 2026 现状（developers.openai.com 403，依据 2026 官方镜像文档目录推断已并入 skills/hooks/exec-policy）；GitHub Agent HQ 细节（公告页 URL 已 404，依据多家媒体报道）；Factory Droids 细节（官网为 SPA，依据搜索汇总 + docs.factory.ai droid-exec 页面存在性确认）。

---

## 附录 C：中文生态 + 工作流引擎调研（子代理报告原文）

**调研方法说明（可靠性标注）**：全部搜索/抓取经 web-access skill 流程执行。CDP 浏览器代理在首轮连接超时，后续全部用 WebSearch + curl/GitHub API 完成。以下来源中：(a) cairn 的仓库在极狐 GitLab 需要登录，细节来自 npm/Socket 页面二手摘要，**未核实原文**；(b) "Claude Code Workflow 的 JS 编排 DSL"细节来自社区逆向分析文章（TokenRollAI），**非官方文档**；(c) 大厂平台信息来自官方渠道页面检索摘要，部分含营销成分，已尽量取官方文档页。

### C.1 大厂平台的企业流水线/门禁机制（简要，只提炼设计层可借鉴点）

- **阿里通义灵码 + 云效**：流水线插入「通义灵码-代码评审」原子节点（单元测试之后、制品上传之前）；阈值即失败（"高危建议命中≥1条"流水线直接失败并告警）；"**CI 失败时将 approve 降级为 comment**"——AI 静态评审的通过结论不能越过 CI/构建/测试的真实状态。[阿里云开发者社区](https://developer.aliyun.com/article/1680815)
- **百度 Comate**：内部「9 阶段流水线」每阶段硬性门禁且"通过条件可计算、可自动验证、不通过不执行"，失败后自动分析→修复→重试（上限 5 轮）。（CSDN 转载文章，非百度官方原文）
- **腾讯 CodeBuddy**：企业版内嵌 SAST、质量门禁；CLI（`codebuddy -p`）嵌入 CI 流水线做自动化审查；NPC 围绕门禁闭环（构建挂掉→自动定位改码重跑直到过门禁）；**门禁结果回流为持久化"质量记忆"**。
- **字节 Trae**：企业版主打知识库/Skills/审计日志/命令黑名单，门禁设计弱于阿里/腾讯——印证门禁目前更多靠企业自建流水线而非平台内建。
- **华为 CodeArts**：门禁设计最成体系——**租户/项目/任务三级门禁**（优先级租户级最高）；门禁项 4 类（致命/严重/一般/提示问题数，阈值固定"≤"语义）；Pipeline 统一"准出条件"（规则→策略→阶段准出）；Repo 多层级上库门禁 + 人工审核（权责分离）+ 合并请求流水线门禁。[CodeArts 门禁配置](https://support.huaweicloud.com/intl/zh-cn/usermanual-codecheck/codecheck_01_1004.html)
- **蚂蚁 CodeFuse**：CodeFuse-Query——TL 把反复出现的 bug 写成分析 Query 固化为代码规则，**上线到 CodeReview/CI 阶段作为"卡点"**（规则上线后该类 bug 不再复发，月调用超百万次）。**失败模式 → 规则 → 卡点**的沉淀闭环。

**大厂共性**：门禁 = 「可计算的通过条件 + 流水线/CI 强制 + 阈值即失败 + 人工审核并列（权责分离）」。

### C.2 中文社区「构建在 Claude Code 之上的第三方开源编排/门禁项目」

- **wow-harness（→Flowness）**："指令遵从率约 20%，Hook 执行率 100%"；8-Gate 状态机（★=独立审查者，工具清单物理上没有 Edit/Write）；"分离判断与执行"；"Agent 不拥有 flow，flow 临时组装 agent"；Assurance Kernel（执行→独立审查→定向返工→重新验收）；"Human out of the session, never out of the constitution"；"Built ≠ Integrated ≠ Activated ≠ Accepted"。[腾讯云社区深研](https://cloud.tencent.cn/developer/article/2659906)、[Flowness 仓库](https://github.com/NatureBlueee/wow-harness)
- **@agentfare/forge**：最接近的对位项目（详见正文汇总表）：退出码契约、验收标准可执行化、cheat-scan、read-before-edit、file-sentinel、HITL、独立只读审查、task-complete 门禁强制 ReviewPassed 前置；引用 "Code-as-Harness"（arXiv:2605.18747）："termination should be governed by verification rather than by model confidence"。[Forge 仓库](https://github.com/MjxUpUp/Forge)、[npm](https://www.npmjs.com/package/@agentfare/forge)
- **peaks-cli（→peaks-loop）**：hook 在权限检查之前拦截（连跳过权限标志都绕不过）；phase 挂可检查条件（file-exists/grep/退出码）不满足就拦下 `git push`；peaks-mut 变异测试防假绿；gate bypass 一次性+记原因+限 3 次。[peaks-cli](https://github.com/SquabbyZ/peaks-cli)、[peaks-loop](https://github.com/SquabbyZ/peaks-loop)
- **cairn**：跨会话状态机；门禁（状态流转前置条件、失败回灌自纠正环）；append-only 事件流审计（events.jsonl 链式 hash）；核心命令仅 start/next/status，`cairn next` 是唯一推进动词（输出 DECIDE/WAIT/STUCK）。[npm](https://www.npmjs.com/package/@jh-cairn/cairn)
- **strict-flow / trellis-hgl / ai-coding-template**（轻量门禁类）：三阶段（Brainstorm→Plan→Verify）门禁 skill"证据验证永不跳过"；trellis-hgl 决策落盘仓库文件（prd/design/implement）而非系统提示词，多重 review gate 固定顺序（spec-review→code-review→architecture-review→merge-review）；ai-coding-template 8 阶段 + Phase Gate（/check-gate、/approve-gate、/next-phase，每阶段检查必需产出物/质量检查/审批状态）+ 第三方模型作独立评审方。[strict-flow](https://github.com/dave-wind/strict-flow)、[trellis-hgl](https://github.com/LonelyHerbivore/Trellis-Herbivore)、[ai-coding-template](https://github.com/oowanghuan/ai-coding-template)
- **cc-flow / claude-code-best / TokenRoll 研究**（流水线 DSL 类）：cc-flow 工作流可视化/CLI 生成器；TokenRollAI 对 Claude Code Workflow 的逆向分析揭示：**Workflow 本质是主模型生成受限 JS 编排脚本（agent/pipeline/parallel/phase 原语），由本地 runtime 执行，子代理经 StructuredOutput 返回 schema 校验的 JSON，交接发生在 JS 层而非 agent 间"聊天"**——厂商自己的 Workflow 特性就是"确定性 runtime 编排 + 结构化交接"路线。[cc-flow](https://github.com/s-hiraoku/cc-flow)、[TokenRoll 研究](https://github.com/TokenRollAI/claude-code-workflow-research/blob/main/share-article.md)
- **subagent-driven-development / yubao**：每任务派发全新 subagent + 两阶段审查门禁（规格合规→代码质量，不通过循环修复）+ 独立任务 worktree 隔离并行；yubao worktree + task 状态闭环管理（claim/list/release/done + 文件范围声明）。[subagent-driven-development](https://github.com/wan-huiyan/subagent-driven-development)、[yubao](https://www.npmjs.com/package/yubao)

**社区实践文章（防呆/门禁方法论）**：
- **Spec Coding 四道门禁**（2026-02）：① Prompt 前规格校验（缺节即拒）② 生成后 diff 审查（只碰声明的文件、越界即硬停）③ 测试证据 + mutation 检查（改坏代码后测试必须失败，专抓假测试）④ 人工签字放行（具名人读 diff 引用具体行号；绕行需双人批准 + 审计表 + 后续工单）。"门禁 1-3 机器能查，也就能被机器糊弄，只有门禁 4 抓得住'技术正确但行为悄悄变了'"。[原文](https://spec-coding.dev/zh/blog/quality-gates-for-ai-assisted-development-specs)
- **AI 研发工作流五道生产门禁**（CSDN agent 社区）：需求门（业务正确性翻译成机器可判的不变量）→ 上下文门（按依赖提供上下文 + 记录来源哈希）→ 实现门（限制可修改路径）→ 验证门（自动化验证先于人工 review）→ 发布门（高风险决策人类签字 + 证据 + 范围 + 回滚条件）。核心句："**把生成能力放进工程系统，而不是让工程系统为生成能力让路**"。[原文](https://agent.csdn.net/6a34dce3662f9a54cb81dbeb.html)
- **UC Berkeley 20 企业案例 + LangChain 2025 报告**：可靠性是头号挑战（37.9%）；80% 采用预定义静态工作流；74.2% 用 Human-in-the-loop 主导评估；Jason Lemkin 案例（明确冻结指令后第 9 天 AI 仍删生产库）——"**'有人同意了'不等于'这件事应该发生'**"。[伪 Agent 拖垮公司](https://dbaplus.cn/news-73-6742-1.html)
- **ofox 三层防御**（2026）：settings.json deny 规则 + PreToolUse hook 正则二次过滤 + Git worktrees 每 session 独立工作区；hooks 防呆核心语义：`exit 2` 才阻塞、`exit 1` 会被无视；permissions 优先级 deny→ask→allow。[原文](https://ofox.ai/zh/blog/claude-code-security-hooks-permissions-worktrees-2026/)
- **subagent 编排踩坑**（[issue #86085](https://github.com/anthropics/claude-code/issues/86085)）：最严重的是**生命周期信号缺口**——subagent 挂起等待时父级收到的 completed 与真正完成无法区分，terminal event 会丢失（一次会话浪费约 1.2M token）；结论：编排器**不能信任 agent 的完成报告**，需要独立真值。[ofox 嵌套 subagent 实战](https://ofox.ai/zh/blog/claude-code-nested-subagents-2026/)

### C.3 工作流引擎通用模式（详见正文「三」的汇总表）

模式 1 持久化状态 + 事件溯源 + 重放（durable execution）；模式 2 显式状态机 + checkpoint（FSM graph）；模式 3 人工节点 = 持久化挂起 + 外部信号恢复；模式 4 saga 补偿 + 幂等；模式 5 确定性控制面与 LLM 分层（TRON / Rel(AI)Build / Buildplane / Elasticsearch）；模式 6 官方"workflow 优先"原则与审批机制（Anthropic Building Effective Agents、Codex execpolicy Starlark 规则引擎、Airflow 反例）。[Temporal + OpenAI 集成](https://www.infoq.com/news/2025/09/temporal-aiagent/)、[dataraum ADR](https://github.com/dataraum/dataraum/blob/main/docs/adr/0004-agent-tier-boundary.md)、[LangGraph checkpointers](https://docs.langchain.com/oss/python/langgraph/checkpointers)、[Prefect orchestration policies](https://github.com/PrefectHQ/prefect/blob/main/src/prefect/server/orchestration/policies.py)、[TRON](https://zenodo.org/records/21613792)、[Rel(AI)Build](https://arxiv.org/abs/2606.26924)、[Anthropic Building Effective Agents](https://www.anthropic.com/engineering/building-effective-agents)

### C.4 中文生态视角的 5 个要点

1. **确定性控制面：模型干活，门禁判完成——编排决策不允许 LLM 碰**（TRON 不变式 + Forge 退出码契约 + wow-harness 分离判断与执行 + cairn 推进动词唯一化；Anthropic"能预画决策树就建 workflow"）。
2. **Hook/机制层 100% 执行 vs 指令层约 20% 遵从：把流程规则下沉为程序强制**（CLAUDE.md 是软指令，Hook 是硬约束；PreToolUse 在权限检查之前拦截；exit 2/deny 才是真阻断；监控文件而非工具；独立审查者物理无写入工具）。
3. **流程状态必须持久化在 LLM 上下文之外，且"完成"信号不能来自 agent 自述**（Temporal 重放已记录的 LLM 决策；issue #86085 生命周期信号缺口；"Built ≠ Integrated ≠ Activated ≠ Accepted"完成多态）。
4. **门禁必须防伪造：可机检谓词 + 反作弊 + 独立审查 + 人工签字 + 绕行审计**（CodeArts 阈值门禁、peaks guards、mutation 测试、Forge cheat-scan、spec-coding 门禁 4；LLM 判定只作 advisory，deterministic 事实作 hard gate）。
5. **人工节点与失败语义按引擎惯例设计：挂起零占用 + fail-closed + 幂等补偿**（挂起+信号恢复+审计；宁可升级不关掉没做完的活；对不可逆动作做幂等 key + 逆序补偿；"人在会话之外、但绝不离开宪法"）。

**落地提示（对 formal-gates 的直接含义）**：作为 Claude Code 之上的本地编排项目，无需引入 Temporal 级重型引擎——中文社区验证过的组合是「仓库内/本地状态文件（JSON/YAML/JSONL，可链式 hash）+ PreToolUse/Stop hooks 硬门禁 + 确定性 gate 脚本 + 独立只读审查子代理 + 可执行验收谓词」。但 Temporal 的两个思想必须保留：**状态与 LLM 上下文分离且可重放**（断点续跑时从记录重放而非重新决策）、**所有判定出口走确定性 gate 而非模型自信度**。Airflow 式静态 DAG 与"agent 说完了"式收尾都是反模式。

**未核实项汇总**：cairn 细节（npm/Socket 二手来源，原文仓库需登录）；Claude Code Workflow JS DSL 细节（社区逆向文章）；CodeBuddy 内部 94% 评审参与度等厂商自报数字；Comate 9 阶段流水线来自 CSDN 转载文章非百度官方原文。其余关键结论（TRON、Rel(AI)Build、Forge、peaks-cli、Flowness、spec-coding、Temporal/LangGraph 文档等）均取自原文或官方文档页面。
