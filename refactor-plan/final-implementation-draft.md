# formal-gates 确定性编排引擎最终实施方案

> 状态：完整需求与重大技术选择已由用户确认。
> 权威需求：`openspec/changes/orchestration-pipeline-engine/master-requirements.md`。
> 本文是实现方案，不再保留旧方案的兼容分支或待用户决定项；若实现细节与权威需求冲突，以权威需求为准。

## 1. 最终形态

formal-gates 从“主代理阅读流程规范并选择下一条命令”改成“Go 引擎计算完整下一步，主代理只机械搬运或呈现用户决定”。引擎是唯一流程权威，最终交付树不保留旧 run 迁移、旧公开推进命令、兼容别名、legacy mode 或 engine→legacy 回退。

两项目标等权：流程不能因漏步骤、选错分支或选择性忽略并行任务而卡死；主代理也不再消耗 token 推理下一步。验收采用结构性标准：主代理看不到迁移表和 pre 条件，不选择 Ready 子集，只接收固定、短小的类型化指令。

本次覆盖：

- regular 非分片、retained master、child、merge 和 lightweight；
- Git、SVN、P4 的非分片与分片全流程；
- Claude Code、Codex、Cursor、DeepSeek Harness、ZCode 的真实 adapter 与完整 canary；
- 产品审、技术审、开发、黑盒/白盒 QA、门、修复、需求变化、中断恢复、Seal/Abort；
- 严格拒绝旧 run；不承诺 Windows/macOS/Linux 支持矩阵，但实现保持可移植。

## 2. 公共入口与控制循环

### 2.1 公共面穷举

最终只公开以下 workflow 入口：

1. `workflow start`：唯一 bootstrap 写入口。
2. `workflow drive`：无外部事件，推进到下一外部边界。
3. `workflow submit`：唯一外部事件写入口。
4. `workflow show`、`status`、`next`、`diagnose`：严格只读。
5. `workflow guard-preview --input <basis.json>`：公开只读、非权威的生产 guard 计算预览（schema/fixture 见 `references/guard-preview.md`），不创建 run、不写状态、不认证真实确认或任务合法性。3.5b 固定 `runtimeIntegration=NOT_INSTALLED`；阶段 4 才从权威 engine 状态呈现运行时集成。

workflow 外保留 `coverage validate`、`coverage project-whitelist`、`coverage reconcile-execution`
三个只读 JSON 契约入口，共用 coverage 核心、无独立状态。3.5 新增 workflow 命令的最终去向
逐项见 route matrix：推进能力内收 handler/typed submit event，预览成为只读投影，不保留自由写入口。

安装、卸载、hook、lifecycle、canary 与脱离 run 的快速 gate 检查仍属维护面，但不能写 workflow 状态。`drive --event`、`submit-result`、公开 `cleanup` 及所有旧推进命令全部删除；旧能力若仍需要，只能成为引擎内部 handler。

### 2.2 主循环

```text
start -> drive -> NextResult
                  | Ready/HostAction: 宿主整批执行 -> submit receipt/result
                  | Ask:             用户选择       -> submit decision
                  | Operator:        主代理核实事实 -> submit observation
                  | Wait:            等新事实       -> drive/submit
                  | Complete:        结束

每次 submit 接纳事件后立即继续 Decide/SelectIssued，直接返回新的 NextResult。
```

`NextResult` 是 tagged union，只允许 `Ready`、`HostAction`、`Ask`、`Wait`、`Operator`、`Complete` 六种 Kind。一个 canonical Plan 只能有一个 Kind，对应之外的 payload 必须为空。一个 Ready 可包含多个任务，且必须是整个 IssuedSet。

### 2.3 主动控制

route-add/reselect、需求变化、reset、abort、adopt-external、主动复审、fresh dispatch、QA 重跑和提前 waiver 都不能恢复成自由写命令。`next/status` 暴露带 freshness token 的 `availableActions`，或 `submit` 接受受限 `REQUEST_*` 事件先创建 pending Ask；用户确认后再以 request ID 提交决定。

Operator 只能核实事实和提出候选动作，不能替用户授权。

QA 是否重跑、采用 `FULL` 还是 `AFFECTED`，只由 Controller 根据真实激活的 requirement
guarantee、当前项目/路线规则、已核实的修复类型、候选绑定和已接纳的用户例外决定。
“最终候选”本身不证明已启用 guarantee；主代理的理由文本、host override 或普通“继续”
消息不能创建 FULL、取消任务或 Seal 跳过授权。用户明确允许的一次例外须通过对应 typed Ask
绑定 run、revision、candidate、mode 和 scope；不改变后续轮次的默认规则。合法操作由宿主
自动执行，不为每个步骤增加确认。

## 3. 确定性内核

### 3.1 三段式决策

1. `Observe(state, external facts)`：只读收集 VCS、文件、宿主、lifecycle、receipt 和容量事实，生成带 source bindings 的 observation。
2. `Decide(state, observation)`：纯函数，输出字节级稳定的 canonical Plan；不得包含随机 actionID 或当前时间。
3. `SelectIssued(plan, admission)`：按固定顺序和真实容量签发动作，为签发结果分配 actionID 并持久化。

迁移表只管理少量 run-level phase；动态任务由 `expectedTasks`、`TaskKey` 和 `TaskTransitionTable` 管理。单个 task 状态为 `QUEUED / ISSUED / RUNNING / VALIDATING / TERMINAL`，先推进 task，再由完整 expected set 的 join 推进 phase。

建议的 run phase：

```text
INTAKE_REGISTERED
PRODUCT_REVIEW
TECHNICAL_REVIEW
START_READINESS
TOPOLOGY_AND_ROUTE
DEVELOPMENT_PARALLEL
SNAPSHOT_READY
POST_REVIEW
REPAIR
SLICE_READY
MAINLINE_INTEGRATION
MERGE_VALIDATION
TERMINAL
```

phase 名称可在编码时微调，但合法边、用户节点和外部可观察语义不得改变。

### 3.2 程序化优先与节点内执行计划

调研 Temporal、AWS Step Functions、Argo Workflows、Airflow、LangGraph、OpenAI Agents SDK、Google ADK 2.0 和 Microsoft Agent Framework 后，采用这些系统共同支持的成熟边界：**已知控制流放在 workflow/code/DAG，非确定性语义工作才进入 agent 节点；持久化粒度跟随恢复和副作用边界，而不是把每条语句都拆成远程步骤。** 机械强制程度并不相同：传统 durable orchestrator 通常强制已声明的图/活动边界，agent 框架多为提供能力且仍可能允许模型路由或把工具错误回给模型。formal-gates 选择其中的严格子集，并额外实施定义编译、理由枚举和“禁止动态降级给代理”。

执行责任拆成两个正交维度：

| 维度 | 枚举 | 含义 |
| --- | --- | --- |
| `DecisionAuthority` | `ENGINE` / `AGENT` / `HUMAN` | 谁拥有该步输出的判断权 |
| `RunnerKind` | `ENGINE_LOCAL` / `DURABLE_ACTIVITY` / `HOST_ADAPTER` / `AGENT_WORKER` | 该步在哪里、以何种可靠性边界执行 |

HOST 不是判断权。它只提供凭据、UI、进程或外部系统能力；HUMAN 只能通过 typed Ask；AGENT 只允许以下 `nonProgrammableReason`：

- `SEMANTIC_JUDGMENT`：证据含义、影响范围、冲突意图等无法由确定性规则完备判断；
- `CREATIVE_IMPLEMENTATION`：在已确认范围内设计或编辑代码、测试、文档；
- `INDEPENDENT_REVIEW`：需要新鲜、隔离的产品/技术/QA/门审判断。

外部边界到这两个维度的映射也固定：

| 边界/步骤 | DecisionAuthority | RunnerKind | 约束 |
| --- | --- | --- | --- |
| engine handler | `ENGINE` | `ENGINE_LOCAL` / `DURABLE_ACTIVITY` | 只走定义中的 next/failure edge |
| Ready dispatch transport | `ENGINE` | `HOST_ADAPTER` | `hostBoundaryReason=AGENT_DISPATCH_API`；回 SpawnReceipt，spawn 不属于 HostAction |
| Ready worker task | `AGENT` | `AGENT_WORKER` | 必须有合法 nonProgrammableReason；回 worker result |
| Operator | `AGENT` | `AGENT_WORKER` | 当前主代理仅返回 `SEMANTIC_JUDGMENT` typed observation，不替用户授权、不执行 adapter 动作 |
| Ask | `HUMAN` | `HOST_ADAPTER` | `hostBoundaryReason=USER_IO_TRANSPORT`，只有 request 中的合法选项 |
| HostAction | `ENGINE` | `HOST_ADAPTER` | 宿主只执行已签发参数并回 receipt |
| Wait / Complete | `ENGINE` | `ENGINE_LOCAL` | 只投影状态，不产生隐藏动作 |

“实现麻烦”不是代理理由。缺程序化 adapter 时只在开发期 diagnostic compiler mode 给未完成 definition 标记 `MISSING_ENGINE_ADAPTER`，不能临时把命令顺序交给代理。该 marker 不是 RunnerKind/hostBoundaryReason：不得生成 executable StepSpec，不得签发 Ready/HostAction；normal compile/drive 以 `BLOCKED_BUG` 和 diagnose 拒绝。最终候选的本次范围内定义不得残留。可执行 HOST_ADAPTER 的合法 `hostBoundaryReason` 只有 `EXTERNAL_CAPABILITY_BOUNDARY`、`USER_IO_TRANSPORT`、`AGENT_DISPATCH_API`。

每个有顺序语义的节点编译为 `NodeExecutionPlan`。定义的唯一 authoring 形态是封闭 Go 类型变体 + constructor + 显式节点/步骤表（`LocalStep`、`DurableStep`、`AgentStep`、`HumanAskStep`、`HostActionStep`、`ParallelStep` 等，详见 ADR-001）：变体只暴露自己适用的字段，local step 看不到 receipt/join/agent reason，human step 填不了 retry/side-effect；`DecisionAuthority`/`RunnerKind` 由变体自动派生并在制品中物化，作者不手填。编译产物是具体结构组成的 `CompiledDefinition`，其步骤同样采用公共头（id/nodeId/ordinal/dependencies/kind/definitionVersion）加封闭变体 payload（`CompiledLocalStep`、`CompiledDurableStep`、`CompiledAgentStep`、`CompiledHumanAskStep`、`CompiledHostActionStep`、`CompiledParallelStep`）的结构——变体不适用的策略字段不得存在于该步骤，不得实现为全字段平铺的单结构。每个持久化步骤（`StepSpec`）在自身变体内承载：

```text
id / nodeId / ordinal / dependencies
executable preconditions / postconditions（封闭 predicate ID）
decisionAuthority / runnerKind（编译器派生并物化）
input/output codec ID（typed I/O）
retryPolicy / timeout
idempotencyKeyStrategy
sideEffectProtocol / receiptPolicy
interruptPolicy
parallelGroup / joinPolicy
failureClassMap
definitionVersion
nonProgrammableReason | hostBoundaryReason（按变体必填）
handler / predicate / codec / reconcile 的稳定 registry ID
```

以下任一条件成立就必须拆成 Controller 跟踪的 StepSpec：需要独立恢复、重试、超时、补偿或幂等；含不可逆副作用或 UNKNOWN receipt；跨越人/代理边界；形成 fan-out/fan-in；需要独立审计、版本或证据；崩溃恢复时必须避免重放已完成前缀。纯内存、廉价、确定性的连续变换，或能在一个原子/幂等事务中共同恢复的操作，可保留在同一 engine handler，代码顺序本身就是机械顺序。

八类非法定义拒绝结果全部保留，拦截层按 enforcement matrix 分工——局部非法组合优先由封闭类型与 constructor 消除（Go 无编译器穷举的 sum type，“非法状态不可表示”指正常 authoring API 下不可构造；compiler 仍拒绝 nil、未知变体与零值），全局图不变量由小型 closed-world compiler 校验，runtime loader 对版本绑定做最后防线：

| 非法定义 | 主要拦截层 | 二次防线 |
| --- | --- | --- |
| 不可达 step / 非法循环 | graph compiler | property test |
| 无类型 I/O | typed constructor / codec registry | compiler |
| 自然语言-only pre/postcondition | predicate 类型 / constructor | compiler |
| 副作用无 idempotency/reconcile | durable constructor | compiler |
| human wait 无 request/schema | HumanAsk constructor | compiler |
| 并行组无 join/failure policy | parallel constructor | graph compiler（join 目标存在且覆盖完整 fan-out） |
| AGENT/HOST 缺合法 reason | 变体 constructor | compiled IR 校验 |
| 未绑定 definition version | compiler / encoder | runtime loader |

compiler 只检查它能机械证明的事情（registry ID 存在/唯一/kind 匹配、图不变量、版本绑定），不证明 predicate 语义正确——“编译成功”不替代行为测试；也不解释业务表达式。`MISSING_ENGINE_ADAPTER` 及一切未完整实现的定义只能以 diagnostic-only 形式存在：可输出诊断，不得编译为 executable plan、不得签发 Ready/HostAction、不得进入 promotion；executable definition 只有在同一候选包 registry 完整解析其全部 ID 时才能激活（definition 与二进制锁步）。运行时只执行当前 eligible frontier，拒绝乱序、遗漏和重复 step。AGENT_WORKER 只收到当前已解锁 step 的最小输入、typed output contract 和 postcondition，不能选择后续 step。HandlerID 标识可恢复执行合同而非每一版实现：合同不兼容变化必须晋升 ID，合同兼容的实现变化保持 ID、由 PackageDigest 标识。

错误不得动态降级给 agent/LLM：

| FailureClass | 唯一合法去向 |
| --- | --- |
| `TRANSIENT_ENGINE_ERROR` | 绑定未变时按有限 retry/backoff 自动恢复；外部派发缺显式策略时用 canonical `MaxAttempts=2`，耗尽后 typed Ask 或显式失败 |
| `BUSINESS_REJECT` | definition 中声明的业务边 |
| `USER_ACTION_REQUIRED` | Ask 或 Wait |
| `SIDE_EFFECT_UNKNOWN` | observe/reconcile，再到 Wait 或 Operator |
| `INVARIANT_VIOLATION` / `BLOCKED_BUG` | 显式失败并提供 diagnose |
| `AGENT_RECOVERABLE_SEMANTIC_ERROR` | 仅定义预先声明时进入 agent 语义修复 |

### 3.3 状态与并发写

`state.json` 继续是唯一权威工作流投影，使用原子替换、完整性摘要、快照和短时跨进程文件锁。新增并固定：

- `workflowDefinitionVersion` 与 `stateSchemaVersion`；
- 单调 `revision`，仅用于 CAS；
- `pendingActions[actionID]`；
- task/dispatch/Attempt 与 source bindings；
- pending Ask、available-action freshness、obligations、impact set；
- 用户确认的 `granularity_review`、Batch/Subtask 计划及其 digest（按需求/方案 revision、拆分决定和路线绑定）；
- typed `ReviewScopeMode` 与逐项 QA approved whitelist（按 review/candidate/result 绑定）；
- 已确认 `requiredSources`、`AcceptanceManifest`、source↔point↔case map 及其 digest（按需求/方案 revision、route/topology scope 绑定）；
- 显式 requirement guarantee activation、来源、完整 REQ/AC 投影和唯一 AC 主责任分配；
- shared-step 内容/版本/逐项审批、case 引用、suggestion ID 的理由处置，以及执行 checkpoint 的完整结果、未完成项和候选/需求/case/tool/environment 绑定；
- 同一 cost/guard 投影中的 dispatch/owner 来源与计量，保留 Codex transcriptPath 和原生任务 timing，checkpoint 不提前结算；
- split topology、`sliceID -> childRunID` 与 receipts；
- terminal summary 的最后 request/event receipt；
- `definitionDigest` 与 owning runtime `packageDigest` 的双重绑定与校验记录。

写事务为：读 state → Observe → Decide → 加锁重读 → 校验 revision → 重验外部 fingerprint → 持久化 intent/动作与 revision+1 → 原子保存。外部事实变化则释放锁重算，不依据过期 HEAD 或需求摘要推进。

本期不引入事件溯源、SQLite、watchdog、OS 定时器、repo 级集成队列或开放式通用 Effect/Artifact 框架。StepSpec 中的 side-effect/receipt policy 与 artifact schema 必须是定义里穷举的 typed variant，不能变成任意脚本/字符串扩展点。

三类 canonical 身份职责分离且不得混用：DefinitionDigest 绑定拓扑、handler/predicate/codec ID、策略与 join/failure 语义；PackageDigest 绑定真正的实现字节、二进制与安装包资源；PlanDigest 绑定给定 state+observation 的决策结果。PackageDigest 是 owning-runtime 执行绑定而非审计附注：loader 在写入前必须校验 envelope 的 package digest 与实际执行 runtime 的安装身份一致（或由 run 绑定的不可变 runtime sibling 执行并验证其实际摘要，或持有显式 adopt receipt），不得仅凭 HandlerID 相同接受新实现接管旧 run。

### 3.4 submit 幂等与新鲜度

每个外部事件验证 event/request/action ID、当前节点、schema、payload digest、dispatch/Attempt 与 source bindings：

- 同 ID、同 digest：返回稳定 acceptance/status，不再次签发已经 ISSUED 的 SpawnRequest；
- 同 ID、不同 digest：硬拒绝；
- 新 Attempt 已建立后的旧结果：以 `OBSOLETE_RESULT` 可见拒绝；
- result-before-receipt：暂存，lifecycle 对账成功后接纳；
- 终态最后一个相同事件：由 durable summary 幂等重放结果。

### 3.5 开发分块计划进入引擎的机械流程

开发分块沿用 ADR-002 的 Batch/Subtask 模型，不与 Run 级 `slicing`（master/child 拓扑）
混为一谈。分块的语义判断由 Part 2 技术审提出、用户确认；引擎只负责把已确认计划变成
可校验、可执行的任务绑定，不从自然语言重新猜测粒度。

固定接入顺序如下：

1. Part 2 `start-readiness` 产出结构化 `granularity_review`：记录需求/方案 revision、
   拆分决定、单一或多个 Batch、`Batch → Subtask` 映射、每个 Batch 的交付物、DoD/测试、
   依赖、接口/状态边界、风险、回滚/恢复、交接与上下文成本，以及不拆或改拆理由。
2. Controller 在用户确认后校验该计划：ID 唯一，Batch/Subtask 映射无遗漏和重复，
   TaskKey 归属明确，批间依赖无环，必需的 DoD/验证/恢复/交接字段齐全；通过后保存
   `granularity_review` 与计划摘要及其 digest，并绑定当前需求/方案、路线和拓扑。
3. 引擎把 Batch 映射为现有 TaskKey 的分组与依赖信息，不新增 Batch 独立状态机。批次完成
   由成员 task 的终态和该批已登记的 DoD/验证/交接结果派生。
4. `development-worker` 派发必须带 `batch_id`；一个 `batch_id` 只能对应一次开发派发。
   同一派发中的 `subtask_id` 只是顺序步骤，不能单独派发、不能触发换代理或 reset。
5. 前一 Batch 的成员任务、DoD、验证和交接均完成后，Controller 才把下一 Batch 放入
   eligible frontier；跨 Batch 才允许准备新的零上下文开发代理。Batch 契约、接口、依赖
   或 DoD 变化时，原计划和未完成派发失效，必须重新做 `granularity_review` 并取得用户确认。

这里的“机械”只保证结构完整、顺序、绑定和代理边界正确；是否值得拆、拆分是否符合产品
语义，仍属于技术审和用户决定，不由引擎自动优化。

2026-09-07 用户修订：代码/质量门只在本 run 全部开发 Batch 完成并满足最终 review-ready
后统一运行，取消可选组间增量门审。批内自测、批边界构建/回归与交接照常执行，不产生
组间门结果或 repair wave；开发前审查与并行黑盒用例审查保持原职责。实现把审查时点的
封闭定义/策略、范围选择和结果接纳分开，后续变更可局部迭代，不改写 CAS/动作协议/成本
模块；当前不预建组间流程或新开关。ADR-002 的旧档位规则由此次决定替代，历史原文保留。

## 4. 受理、用户节点与审查

### 4.1 宿主受理

普通修改请求只提醒一次可使用 formal-gates；复杂请求在完整需求/方案确认后、动手前再提醒一次。提醒均不需要用户回应，也不阻塞直接处理。

用户主动提出 formal-gates 后直接进入受理，不再重复问“是否进入正式流程”。主代理逐项澄清需求和重大技术选择，无待澄清问题也明确说明。先将完整需求与方案直接写入唯一正式文件，`start` 创建受理 run 并绑定文件；用户尚未明确模式时，该 run 保持模式未绑定，不预设为 regular；结构预检后展示全文与精确 revision，单独取得用户确认，再通过绑定该 revision 的 typed decision 经 `submit` 登记 intake receipt。`drive` 消费同一确认，不再次询问。确认前不选择路线、不派发后续代理，预检和启动本身不代表用户确认。

regular 受理不要求用户原始输入采用 OpenSpec、PRD 或其他固定格式。AI 直接把对齐结果写入唯一的正式 UTF-8 Markdown 需求案，不另建草稿或人工维护的覆盖清单；自然语言正文保留，`## 需求点` 下按固定 `REQ → 要求/验收条件/来源` 与唯一 `AC` 结构声明全部必须实现、会改变产品/流程行为或必须进入 QA 保证的义务。确定性预检只校验结构、字段、唯一性与边界，预检通过后仍由用户确认当前完整文件和 revision。纯实现位置、代码边界或未来迁移约束若不改变产品/流程行为，留在方案正文并由技术审、diff、代码审查或实现质量门验证，不伪装成必须 QA 的 `AC`。lightweight 沿用受理时已确认的同一需求文件，不另写需求案；完成需求登记后 Seal，不进入 regular 的审查和 QA 链路。

阶段 3 候选隔离验证所用的 typed `intakeConfirmationReceipt` 以固定 stable driver 已写入的确认状态为权威来源；
固定 launcher 从该状态和登记的 requirement/solution 工件派生回执并落到 formal run evidence，再按
原相对路径注入候选 test project。候选 `start` 通过 `--intake-receipt <path>` 或固定 host config
指针读取，不能自行生成回执、读取 stable run state 或用 runtime/identity/digest 参数替代；旧回执配
当前工件必须在首写前稳定拒绝。stable driver 不因候选 engine 阶段新增 writer 或公共入口。
这是冻结 driver 与阶段 3 被测候选之间的开发期桥接；阶段 4 regular 受理使用上文的
start 绑定、预检、全文呈现与 typed submit 确认，不以该桥接恢复“确认后才允许创建受理 run”。

若用户尚未明确模式，在完整需求/方案确认后，通过 typed Ask 为同一受理 run 初次绑定 lightweight 或 regular。lightweight 是受理阶段的初始模式选择，不经拆分和 full/custom 路线，登记需求后直接 Seal 并标记 unverified；从未绑定模式到 lightweight 不属于 regular 转换，已绑定 regular 的 run 不能在运行中改成 lightweight。

### 4.2 正常 Ask

正常流程只在用户拥有决定权时 Ask：

1. lightweight/regular（尚未明确时）；
2. 高置信拆分建议的精确拓扑，包括边界、数量、依赖和并行度；
3. regular 非分片 run 和每个 child 的 full/custom 路线；
4. 经 Operator 核实成立的产品审/技术审每条 P0–P3 finding 的 confirm/dismiss，以及对已接纳 review finding/adjudication 的语义纠正；Ask 已附唯一完整 remedy 时 confirm 同时批准 remedy，存在多个用户拥有的修法或纠正选项时继续澄清并按 §5.1 确认完整候选的 review-delta。

child 路线可在界面批量接受统一默认，但状态中逐 child 保存独立决定。低置信不拆或不确定由引擎自动记录 no-split 理由，不制造无意义 Ask。

### 4.3 条件 Ask

以下只在事实或用户主动请求触发时出现：

- route-add/reselect；
- 任意 ACTIVE 非终态阶段的需求变化；
- adopt-external、带精确保留/清除预览的 reset、abort；
- 原因未知且 bindings 未变的中断：resume/fresh/abort；
- 产品审或技术审已经完成当前有效上限内的三轮语义审查、但按 finding 处置规则仍需新鲜复审；
- 黑盒、白盒和 merge QA 三类 pre-wave review 各自三连 FAIL 或长期不可用；
- 用户主动要求的 review override、已 PASS 复审、fresh dispatch、QA 重跑、提前精确 waiver；
- 用户主动要求纠正已接纳的产品审/技术审 finding、严重度或 adjudication；
- 开发后 QA/门 finding 经 Operator semantic adjudication 核实严重度、范围、根因与重复关系后，实际处置仍有多个合法分支时才 `VALIDATION_DETAIL_DISPOSITION` Ask；核实不是用户 Ask；
- 开发后 `wave >= 3` 的统一处置；
- 长期 RUNTIME_ERROR；
- master/child 破坏性级联。

此外，任意 ACTIVE、非终态节点都提供 HUMAN 用户 typed 控制 Ask。用户可授权从当前节点跳到任意合法业务节点；请求必须记录 freshness token、request ID、requirement revision、from/to、reason 和 authorization receipt。Controller 在跳转前先 suspend/quarantine/invalidate/reconcile 受影响 Attempt、结果、候选和副作用；终止目标仍走统一 cleanup。

正常 Seal 自动执行，不额外询问。

### 4.4 产品审和技术审

start-readiness 继承审查收敛需求 REQ-002：P0–P3 每条 finding 都须声明分类；只有需求实质冲突、模块职责或依赖方向根本错误、核心验收缺少可执行入口三类允许形成 P0/P1，开发阶段可决定的实现细节最高 P2。Controller 校验分类枚举、分类与严重度矩阵及核实记录，Operator 判断分类、正常使用后果和不能留到开发阶段处理的理由是否成立。缺分类、非法严重度或缺核实记录只形成 `REJECTED_INVALID_RESULT` 并结束 attempt，不写有效 finding/action 结果；沿原 TaskKey 有限重试。

reviewer 输出只是候选。Operator 对每条 P0/P1/P2/P3 核实需求前提、正常入口、复现、证据、因果、严重度和范围；无效或范围外候选直接丢弃。有效 finding 获得稳定 ID+digest，再逐项 Ask。Ask 同时记录 finding disposition 与 remedy authorization：若附带唯一、完整、无用户选择空间的 remedy，confirm 绑定两者 digest；若用户只确认问题或仍有多个有意义的产品/重大技术选项，先继续澄清、取得选择并按 §5.1 确认完整候选的 review-delta，不能由代理自行选修法。纯机械同步不重复 Ask。

审查状态分为三层 typed data：不可变的 `RawReviewCandidate`（绑定 action/TaskKey/dispatch/result digest 与需求 revision）、逐 finding 的 `ReviewAdjudication` 及可 supersede 它的 `ReviewCorrectionReceipt`、由 Controller 纯函数计算的 `EffectiveReviewStatus`。原始 candidate 只作证据，不得由 Operator、host 或 submit 直接写成权威 PASS/FAIL；只有完成结构/binding/完整性校验并经 Operator 核实后的 finding 才进入 adjudication。用户确认的语义纠正通过带 freshness 的 typed Ask/submit 写入 correction receipt；程序可直接拒绝错误 binding、incomplete 或 stale candidate，但不能代替用户改产品/重大技术判断。

ACTIVE、非终态 run 允许针对精确 finding/adjudication 纠正无效性、严重度或错误裁决。纠正绑定 run、action、TaskKey/dispatch/result digest、需求 revision、目标 finding digest、旧/新值、原因和授权 receipt；同一 freshness 下幂等，不同 digest 冲突拒绝。原始 candidate 与旧 adjudication 不删除，只标记被 receipt supersede；这是一组有界 typed records，仍保存在单一 `state.json` 投影中，不引入事件溯源。Controller 每次接纳 disposition/correction 后重算 effective status 和依赖失效：未处置或已确认 P0/P1 阻塞；仅 P2/P3 为 PASS+advisory；全部 P0/P1 被驳回/判无效且无其他 blocker 时直接 PASS，不签发只为改聚合状态的 fresh review。若纠正把 PASS 改为阻塞，依赖它的后续结果按依赖图 stale；若只清除错误阻塞，立即开放下一合法节点。

Controller 按 master/整体 run 和审查动作分别持久化两条独立的 `PreDevelopmentReviewSeries`：`product-review` 与 `start-readiness`。每条 series 默认允许完成三轮语义审查；只有 Operator 接纳的完整、有效 `PASS` 或 `FAIL` 结果才完成并消耗一轮，`RUNTIME_ERROR`、无效结果、未完成派发、中断以及 stale result 不计轮。前三轮是 workflow policy 预授权的上限，不要求一次性创建或提前派完；只有当前 finding 处置确实要求 fresh review，或用户按 §7 明确授权重审时，Controller 才为下一轮生成绑定当前 revision 和轮次的 distinct external `TaskKey`。

完成第 3 轮后若仍需 fresh review，Controller 不再自动签发第 4 轮，而是返回 action-specific typed Ask，展示当前完整文档 revision、已完成轮数、未闭环 finding/remedy 和风险。用户每次只能为该动作授权恰好一轮；授权形成带 request ID、reason、action、revision 与 granted round 的 receipt，并只向 expected task set 增加一个 fresh-review slot，不能预先累计，也不能转给另一动作。额外一轮后仍需复审则重复同型 Ask。单轮 TaskKey 内的瞬态失败仍单独受有限 `RetryPolicy.MaxAttempts` 约束；retry attempt 不增加语义轮数，语义轮授权也不增加 retry 余量。

两条 series 贯穿同一整体 run：需求/方案 revision 变化、impact invalidation、carry、adopt-external 和普通结果失效均不清零已完成轮数或授权记录；显式 workflow reset 或新 run 才建立新的默认额度。split child 继承 master 已完成的整体产品审/技术审，不创建自己的独立 series。`show/status/next` 必须分别显示 completed、base limit、extra grants、effective limit、remaining 和授权来源，并在到达边界时显示 Ask，不能伪装成 Wait 或 runtime error。

- P0/P1 confirm：来源记为 `REVIEW_FINDING_FIX`；修订后由新的零上下文代理读取完整最新文档进行新鲜复审，不得做增量 carry。
- P0/P1 dismiss：finding 作废；若无其他已确认 P0/P1，可接受候选结果，不因驳回项再派一轮。
- P2/P3 confirm：有界修订/修复；不因严重度本身复审。
- P2/P3 dismiss：作废。

产品修订改变技术前提时，产品复审 PASS 后再做新鲜技术审；技术修订改变产品语义时先回完整产品复审。后续提示词携带已拍板 finding ID，前提未变时不得原样重提。

`PreDevelopmentReviewSeries.completed` 从完整、有效语义审查集合投影：一轮完整审查即使 finding 后续被确认、驳回、改严重度或使 effective status 在 PASS/FAIL 间变化，仍只计一次；整轮被 correction 判为 invalid/incomplete/stale/错误绑定时才从 completed 集合排除。correction 既不生成新 TaskKey，也不消费额外 review grant 或 retry attempt。

### 4.5 配置维护

配置维护统一遵守主需求 §6.5：既有和新增用户配置均提供清楚的路径、字段/默认值/示例、
覆盖与生效说明，支持用户或 AI 编辑同一可读来源并核对有效值。配置校验/生效解析与流程
策略分离，不借配置入口修改 workflow state、计算额度或用户授权。

## 5. 任意时点的用户需求变化

`REQUEST_REQUIREMENT_CHANGE` 在 Ask、Wait、Ready、Operator、产品/技术审、路线、开发、QA/门、修复、分片、主线集成和尚未完成的 Seal intent 中都可达。SEALED/ABORTED 后则创建新 run。

处理顺序固定：

1. Phase A 立即撤销未完成 Seal intent，停止新的权威 VCS/状态写入、candidate promotion 和业务任务签发。尚未有确认后的 `ImpactSet`，不得先猜哪些任务不受影响。
2. 已签发且可安全隔离、不会提交权威副作用的计算/agent 可继续到 terminal，但结果只进入 quarantine，不推进 workflow；其余安全 suspend/terminate。此阶段暂时没有 eligible 任务不违反最大并行。
3. 用户澄清并确认完整最新版需求/方案。
4. Operator 基于旧/新完整 revision 产出 changed items、transitive impacts、affected tasks/artifacts 与 carry reasons 的 `ImpactSet`；无法证明未受影响的一律 affected。
5. Phase B terminate/stale affected/unknown Attempt；结果按 §5.2 的用户保留决定与 ImpactSet 合并处理，未获保留的 affected/unknown 结果失效。恢复或接纳明确未受影响的 quarantine 结果并写 carry receipt，SelectIssued 按更新后的 frontier 填满兼容容量。
6. 仅 `USER_INITIATED_CHANGE` 可用增量产品审/技术审：reviewer 读取完整文档作上下文，只重新判断变化及传递影响；影响无法可靠圈定时扩大为全量。
7. QA cases、路线、拆分和开发按同一 ImpactSet 更新，验证结果同时服从 §5.2 的用户保留决定；持久化 typed `ReviewScopeMode` 规定黑盒/白盒 QA 用例或测试审查首次建立基线为 `FULL`，后续新增、修改或 ImpactSet 受影响项为 `AFFECTED`；产品审、技术审和普通质量门始终为 `FULL`，并保留未来开放用户配置的类型能力。

用户需求变化不增加或清零 run 级 repair wave，也不清零产品审/技术审的 `PreDevelopmentReviewSeries`；被变化打断、未形成完整有效结果的旧 revision 审查不计语义轮，被变化打断、未形成完整 expected set 的旧 revision wave 不计开发后 wave。分片由 master 统一接收和级联：未受影响 child 可 carry，受影响 child 重建或重验；已进入主线集成时，普通增量由主线开发 worker 实现，拓扑变化才重建受影响 child/map/receipt。

### 5.1 需求内容确认与回退授权

审查来源的修订由主代理在编辑前保留并核对与上一确认 revision 匹配的完整 base，生成穷尽 review-delta 并逐 hunk 核实来源。用户只确认变更包；完整 candidate revision 绑定为新需求，未变内容继承。首次受理仍确认全文，主动查看全文不改变增量确认范围。Controller 不持有第二份 base/diff 账本，不允许无关 hunk 混入。

需求影响由 Operator 建议，Controller 不判断自然语言含义。`meaning-preserved` 不产生回退 Ask，保留产品审 PASS，只重做实际受影响、未通过或尚未完成的技术审；正文变化仍按 review-delta 确认。`meaning-changed` 在接纳 revision 转换前必须生成绑定 state revision、base/candidate revision、影响预览 digest 的 typed Ask，展示目标/范围/对外行为/验收变化、失效 PASS、回退步骤、产品审已用/剩余额度和授权状态。只有用户确认事件才能原子登记新 revision，并按 §5.2 的保留清单与影响预览处理结果：未获保留的旧产品审 PASS 置 PENDING、未获保留的受影响下游结果失效；不重置额度、不自动授权新产品审。缺失、过期或不匹配时零状态变化。一次决定明确覆盖 review-delta 与回退后果即可共用授权。阶段 4 engine 是唯一 writer，legacy 入口不得绕过校验。


### 5.2 用户决定保留已通过验证

用户在任意非终态修改需求时，有权选择保留任意已经通过的验证，包括产品审、技术审、QA 用例/测试审查、黑盒/白盒 QA 执行、普通门，以及 child/merge 验证。可逐项选择、全部保留或全部按影响规则处理；即使需求含义已变、结果被 ImpactSet 标为 affected/unknown，用户的明确保留决定仍优先，不能由 Operator 或 Controller 因影响判断而否决。SEALED/ABORTED 后沿用新建 run 的边界，不改写旧终态。

影响预览同时展示已有 PASS、建议保留/重跑项及理由；用户可覆盖建议。typed Ask 将保留清单、原结果标识及原需求/候选绑定、新需求 revision、目标候选绑定、当前 state revision 与用户确认共同登记，并与需求修订原子应用。未明确选择的结果继续按既有规则处理；一次确认可以同时覆盖需求内容、影响和保留决定。

保留意味着结果在新需求下继续有效、可满足对应推进与 Seal 条件，而非仅保留历史记录。Controller 在现有 receipt/result 模型中记录用户授权继承，保留原始 PASS 及其真实验证版本；show/status、覆盖报告和 Seal 区分实际执行 PASS 与用户授权继承的 PASS，不声称在新版本重新执行。该授权优先于因本次需求变化触发的失效、fresh review 和 child/merge 重验规则；未选择保留的验证及新增且没有对应既有 PASS 的义务仍照常处理，不能把 FAIL、PENDING 或未执行项变成 PASS。后续另一次需求或候选变化重新判定，不把本次授权扩展成永久豁免。

在途任务沿用 Phase A 的写屏障与 quarantine，不要求用户等待全部派发登记后才能修改需求。确认时尚未形成 PASS 的任务不进入保留清单；迟到结果按其原 Attempt/revision 归属处理，不覆盖新 revision 的决定。连续需求修订逐次绑定确认与继承关系。分片场景由 master 接收用户决定并级联对应 child/merge 结果；业务验证的保留不替代未完成的提交、集成或其他实际副作用。


## 6. 强制最大并行

完整 eligible frontier 由 `expectedTasks` 计算，引擎和宿主必须满足以下不变量：

1. 可用容量为 C、eligible task 为 N 时，签发恰好 `min(C,N)` 个任务；顺序固定。
2. 主代理原样搬运完整 IssuedSet，不得挑子集。
3. receipt/result 释放容量后，`submit` 自动继续推进并补满下一批。
4. 一条支路 FAIL/RUNTIME_ERROR 不压住仍独立 eligible 的其他支路。
5. adapter 无可靠 correlation 时只可串行化尚未 attach 的 start handshake；已 ACK worker 和其他兼容容量继续并行。

关键并行组：

- 开发期：开发 worker + 黑盒 QA 用例设计/审查；
- 分片开发期：各 slice 开发 + 合并 QA 跨片用例设计/审查；
- 开发实现快照 Dn/S1 后：只解锁白盒设计/authoring；白盒有效测试可复用，有变化才提交为 Qn/S2，QA Review 与已采纳建议若修改测试继续提交，直至所有 mode 的最终 case-set FROZEN 且绑定含测试快照；
- 最终 review-ready 候选就绪后：一次冻结 expected-set/digest，全部 eligible QA execution 与 selected gates 同时准备并最大并行，均绑定该最终候选；
- 合并后：合并 QA 执行 + 合并门；
- 修复后：所有受影响且依赖满足的验证再次最大 fan-out。

review-ready 表示最终候选已准备好接受 QA 执行与门审，不表示门审已通过。此前的 QA Review 审查用例与测试代码；此后的 selected gates 执行门审。review-ready 是执行屏障：之前不提前签发黑盒执行或门，也不用 provisional 结果代替最终候选验证。普通实现返修沿用 FROZEN 用例，不重新设计。

## 7. 开发后 QA、门与三轮

```text
开发快照 Dn/S1 → 白盒独立判断覆盖，复用有效测试或补测（有修改才提交）
             → QA Review / 可选建议理由处置，实际测试变动继续提交
             → 最终提交整理（含必要 squash）
             → 全部 case-set FROZEN + 绑定最终候选的 review-ready 快照
             → 冻结 expected-set/digest，QA execution 与门整批并行
             → COLLECTING 收齐 → Operator 逐条核实/归并
             → 无 obligation：提升/Seal；有 obligation：同快照唯一返修
```

开发后 raw finding 由 Operator semantic adjudication 核实需求、正常失败场景、模块、根因、后果；
保存独立 severity rationale、范围/严重度/因果/复现证据、finding/problem ID、
NEW/SAME_AS/DISMISSED 裁决及历史。Controller 只做结构完整性、引用/版本绑定和机械路由，
不判断语义真假、严重度是否成立、根因或重复关系。P1 理由须说明需求、正常路径、实际后果和
为何 P2 不足，P0 还须证明需求不成立、核心验收无可用路径或交付受阻；条目 FAIL、批量套话
不能代替逐条理由。产品/技术审 finding 的 confirm/dismiss Ask 保持原规则。

归并继承审查收敛需求 REQ-004：SAME_AS 指向已 RESOLVED problem 时不再触发返修，与已 DISMISSED finding 相同的新报告只追加审计；根因或实际后果不同可记录新问题，较晚 wave 新出现的有效 P0/P1 标记前轮漏审。错误归并通过绑定 revision 的新裁决纠正，保留原 finding、problem 和全部历史裁决。返修派发前的纠正更新同一逻辑计划；派发后新产生的 OPEN 义务进入返修所得新快照的下一 wave，不能为原快照创建第二份返修。

仅已核实 finding 的处置仍不唯一时才 Ask；核实本身属于 Operator。wave 目标未收齐、
PENDING/在途或运行错误未明确处置时不得返修。所有目标终结且 adjudication 完成后才生成
有效 obligation：QA FAIL、有效 P0/P1 与门审 P2 汇入同一快照唯一返修计划，同根因归并同一
problem。同一逻辑返修运行错误只重试同一 TaskKey，不能生成第二份返修计划。route、
case-set 或目标集变化使旧 wave 失效，不能改 expected-set 制造收齐；晚到结果只留审计，
错误的明确处置不伪装成 PASS。

- 门审 P2 必修、不复审、不增加审查轮次。纯门审 P2 worker 必须完整构建、全量测试、逐项
  自跑受影响用例，以绑定修复快照的完成/carry 凭据继承 gate PASS。
- 门审 P3 不修复，只在 Seal 前一次性 advisory；范围外 P3 同样不形成 obligation。
- QA Review P1 修订后 fresh review；可选 P2/P3 按稳定 suggestion ID 登记采纳、延期或不采纳
  及理由，不强制改用例。全处置且覆盖通过才 FROZEN，无建议则 PASS 后立即冻结；采纳改变
  共同路径、可执行性或验收含义时重审受影响部分，真实覆盖缺口不能靠建议处置绕过。
- 普通返修只 AFFECTED 执行冻结用例；本轮必需 QA 与门全部有效 PASS（含合法继承）即可 Seal，guarantee ACTIVE 不追加最终 FULL。
  纯门审 P2 且既无实际 FULL 义务、也无已绑定用户例外时，不签发 FULL；完整项目测试不等于全量
  黑盒重跑。非保证路线按自身已登记规则计算，主代理不能因“更稳妥”扩大范围。
  只有需求含义、对外行为、成功/失败判据或稳定可执行性变化才能 THAW；临时环境失败是
  RUNTIME_ERROR。合法 QA artifact 修订重新装配并冻结候选，至少重跑改过测试和共享依赖项。
- 既有三轮自动审查与额外授权上限保持；完整 expected-set 终结才计 wave，装配、pre-wave
  review、未收齐、未完成裁决、未处置错误不计，纯门审 P2 修复不计。
- 额外返修先只读 preview，Ask 展示 state revision、需求/快照/wave、完整 findings/problems、
  逐条理由/归并、历史修复/验证、QA scope、已用/剩余轮数、dispatch signature/TaskKey、
  engine allowance、owner cost 和 token cap 可用性。授权绑定摘要 digest 与 state revision，
  任一绑定变化使旧授权失效，一次只授权一轮。

非 wave 与 wave action/QA/gate 共用无效结果义务生命周期：RUNTIME_ERROR 或
REJECTED_INVALID_RESULT 结束原 attempt；拒绝回执记录字段/原因，不写有效 finding、
PASS/FAIL/problem/basis。有效义务复用 TaskKey，默认 MaxAttempts=2；wave 重派还须
COLLECTING 且目标未处置。耗尽进入 RETRY_EXHAUSTED，展示原因与已用/总次数，停止普通和
自动重派。用户绑定 state revision、非空理由只能一次性追加一个 attempt；它只改变 retry
policy 与 budgetBasisDigest，不增加基础槽位。再次失败只能既有显式错误处置、abort/cleanup，
或确认真正改变 binding/snapshot/责任的新计划，不能无限授权。

dispatch signature 绑定具体输入，TaskKey 绑定逻辑义务；相同 OPEN 续用，相同有效 PASS
signature 不自动重复派发。用户明确要求已 PASS 复审时，经绑定当前输入的 typed Ask/submit
登记非空理由和 override 授权，保留原最终结果与审计记录；有自动额度时正常消耗，耗尽时先
取得适用的额外审查授权，需要额外返修仍绑定最新摘要。授权重审形成对应的新审查义务，
不能借 override 重置语义轮次、repair wave 或 retry 额度。guard 基础 source 为 closed-world definition、确认 requirement/
Batch/route/topology、批准 QA、所选 gate；finding 派生 source 仅来自 adjudication 完成且
wave 收齐后的有效 obligation/唯一 repair TaskKey。预授权 repair 机械加入，需确认的扩张先
确认 revision。raw finding、普通 result/receipt/resume 不扩容。3.5b 只纯计算与只读预览；
阶段 4 才接 State/source bridge/IssueFromPlan/refill/retry/typed Ask，将修复摘要 engine
allowance 从 UNAVAILABLE 切换为权威 projection，guard-preview 的 runtimeIntegration 改用
权威 engine 集成状态；owner cost 与 token cap 的 UNAVAILABLE 各按来源/配置独立判断。

阶段 4 同时读取并持久化项目/安装级 token cap 的生效值和来源。已结算派发用量达到上限
时，次数余额不能放行下一次自动签发/refill/恢复重试；返回现有预算处置 Ask，有限授权
不永久关闭 cap。owner 用量仅报告；重启保留 cap/用量，配置变更按公开规则生效。未配置
或来源明确不可计量时 unavailable，次数限制仍生效；错误配置明确拒绝，不静默取消限制。

获准重试的 RUNTIME_ERROR 必须能沿同一 TaskKey 完成“签发新 Attempt → 实际执行 → 接纳
完整结果”；不得出现允许签发却因旧 RUNTIME_ERROR 拒绝结果的死端。旧错误与 Attempt
留痕，已有权威 PASS/FAIL 不被重试覆盖，也不以 Seal 跳过替代合法重试完成。

### 7.1 完整候选与晚提升

每轮先冻结只含实现的 `DevelopmentSnapshot Dn`。选择白盒时，由 VCS adapter 从精确 `Dn` 创建已登记的独立 authoring workspace；白盒设计 agent 可新增/修改/删除被 `TestOwnershipResolver` 证明为 test-owned 的测试、辅助代码、fixture 与 case 内容并返回 manifest，Controller 校验后由 adapter track/open、commit/submit，冻结原生不可变的 `ValidationCandidate Qn`。`Qn` 必须同时包含实现、白盒测试代码及所有要纳入 VCS 的已批准最终 artifact。未选白盒时，Controller 仍在验证前物化这些 artifact，并令 `Qn` 等于装配后的实现候选。

VCS adapter 输出 `Dn→Qn` 全部 added/modified/deleted/renamed 路径与内容摘要；`TestOwnershipResolver` 结合项目/测试框架配置、路径/命名规则、测试发现和生产构建输入分类。名字带 `test` 不是充分条件，名字不带 `test` 也不自动排除；只有全部差异均 test-owned，且生产代码、运行配置、生产依赖、构建输入与 provider 元数据投影相等，才证明 production validation view 未变，UNKNOWN 按受影响处理。

白盒 Review 独占测试质量，测试变化（包括采纳建议引起的变化）先由 adapter 提交；最终 FROZEN case-set
绑定含全部测试的 review-ready Qn 后才允许 QA execution 和门。门不接收 QA 用例正文、
逐用例执行证据或测试实现细节，仍审基线到最终快照完整 diff。合法 THAW 后的候选替换使
旧 wave 失效，新的 expected-set 只绑定新候选；普通实现返修保持冻结用例，按 AFFECTED 执行。

完整 wave 无 obligation 后，adapter 才把整个 `Qn` 提升到主线。Git 能 fast-forward 时保留同一 identity；SVN/P4 或其他 Git integration 产生最终身份 `F` 时，只有 typed `CandidatePromotionReceipt` 证明 source/target identity、预期 target base、完整路径与 provider 元数据、operation id、无冲突，以及 `validatedDigest(Qn) == finalDigest(F)`，整组结果才可映射到 `F`。这不是逐门 carry。若出现冲突、转换、目标漂移、UNKNOWN 或 digest 不等价，先 reconcile，再把实际结果冻结成新候选并重跑受影响验证。

覆盖契约先把同一次需求/方案确认中登记的有限 `requiredSources` 与当前 revision、
route/topology scope 一起冻结，再生成有限验收点集合 `AcceptanceManifest`，并用
source↔point↔case 映射建立双向追踪。标准需求案适配器直接把每个 `REQ` 投影为
`RequiredSource(PRODUCT_REQUIREMENT, QA)`、每个 `AC` 投影为 `AcceptancePoint`、已设计 QA case
投影为 `AcceptanceCase`，关系只由 `CoverageEdge` 表达；标准需求案中的全部 `AC` 都必须有
approved case，不允许使用“非 QA 验证处置”绕过。其他合法 source 才可按自身协议使用显式
非 QA 处置。review 必须显式返回完整 `SourceDecisions`、`PointDecisions`、`CaseDecisions` 和未绑定
集合；不得由 case PASS 自动推导或补写 source/point PASS，三层全部逐项 PASS 后才能生成
whitelist。仅有集合级 PASS 不足以通过。execution 冻结 expected case ID set，并列出
实际执行、合法继承和未执行条目；`FULL`/`AFFECTED` 都必须完整对账。source inventory/
manifest/map/whitelist digest 与当前 `ValidationCandidate` 精确绑定，摘要展开为
`sourceID → pointID → caseID → result`，`AUTHORIZED_SKIP` 不等同 `EXECUTED_PASS`。
Controller/validator 负责结构校验，覆盖率、mutation、property-based test 和 fuzz 只作辅助
信号，不设固定阈值。

### 7.2 保证模式、QA 复用与同次执行进度

保证模式按主需求 §8.4.1 显式激活：同一 State 原子保存 activation、来源及经过预检和用户
确认的单一正式需求投影；full 或 custom+QA 按所选 QA 类型并集覆盖，custom 无 QA、lightweight
及 waiver 明确不提供保证，额外 requirement artifact 在保证模式 QA 设计前拒绝。不能根据
当前路线或文件外观补造 activation。逐 REQ/AC 展示审批、执行、责任和保证状态，标准需求案
的 AC 不使用非 QA 处置绕过。首次必要 FULL、普通返修 AFFECTED 与 §5.2 用户授权继承分别留痕。

3.5c 的 shared-step 与 case 继续共用既有覆盖核心：共享内容审一次、每条 case 独立判断，
共享变更使全部引用者失效，单条变更只影响相关部分。白盒先独立列行为/分支/oracle，再读
已有测试的输入与真实断言；有效测试绑定复用并独立执行，缺口或错误才补测，有测试变化才
提交并推进快照。黑盒设计/审查继续使用不含本次实现与白盒测试的隔离输入。

批量仅组织同一 QA 派发内部工作：复用同候选构建和非验收目标的准备，独立用例从干净副本
开始，有序依赖显式声明，逐条保存真实输出；属于验收目标的安装/构建/宿主调用本轮实跑。
允许构造前置状态，不预造目标结果；新增/修改短脚本以已知错误样例核对关键断言。

checkpoint 作为现有 task result 的进度事件经 submit 写入同一 State，不新增公共写命令或
QA 状态机。保存完整 PASS/FAIL 及证据、pendingCases、中断原因、reuse/invalidation reasons，
绑定候选、需求、case/shared-step、tool/environment；半途条目保持未完成。checkpoint 不终结
dispatch 或增加 TaskKey/attempt；绑定未变时补齐剩余项，运行错误后沿同 TaskKey 有限重试。
最终 expected-set 汇总保留 FAIL，拒绝缺失/重复/未知条目，授权跳过不作为执行 PASS。

阶段 4 的 source bridge 复用 internal/cost 的解析与投影：Codex 按原生 response ID 去重，
保留 transcriptPath 与 task_started 到 task_complete/turn_aborted 的 timing（包括工具等待，
排除 turn 间空闲）；checkpoint 不提前固定未完整的 dispatch cost，续跑完成后结算完整来源。
缺来源或完整计时边界仍 unavailable/省略 timing，不补零，不扩展其他 provider。该合同消费
3.5c 已交付能力，不要求在后续每个阶段重复其模型 benchmark。


## 8. 分片、主线合并与 VCS

### 8.1 生命周期

`start` 不接受也不冻结拆分意向（取消 `--split` 强制声明）。拆分绑定唯一发生在 start-readiness PASS 后的拓扑确认：高置信要拆时 Ask 精确拓扑，低置信不拆或不确定由 engine 自动记录 no-split 理由。拓扑确认即绑定点——确认前用户可改变意向、无需重启或 reset；确认后不得重切，变更走用户需求变化、reset/rebuild 或 abort。

master 确认拓扑后，Controller 执行固定 child-creation plan：ENGINE_LOCAL 持久化 intent、分配 childRunID、创建 PREPARING child state 并注入 bindings；每个 Git/SVN/P4 workspace/client 再由 VCS adapter 独立 StepSpec 创建/对账；ENGINE 校验全部 typed receipts/identity 后，才原子提交 `sliceID -> childRunID` map 并解锁 child。child 继承完整产品/技术审和拓扑，各自持久化 full/custom 路线。

child 达标后进入可恢复 `SLICE_READY`，不独立最终 SEALED、不删除恢复信息。durable receipt 至少包含：

- 状态、child/slice/master ID；
- workflow/state schema version；
- 已包含白盒测试的最终 child VCS identity、tree/validation digest 与 candidate-promotion receipt；
- requirement/topology/route bindings；
- QA/门结果、批准 cases、cost；
- 最后 request/event digest 和幂等结果。

master 只按持久 map 接受预期 receipt，不扫描任意 sidecar 猜测 child 完成。child abort 产生 ABORTED receipt 并阻塞 master；master 的 abort/reset/重大需求变化按用户授权级联，不留 orphan。

retained master 在 child QA 设计前按主需求 §8.4.1 冻结完整 REQ/AC 投影与每个 AC 的唯一
主责任范围（child 或 master merge）。child 继承完整 revision/manifest，只报告自身责任；
receipt 保存批准 case 的 scope、QA 类型、case ID 与 AC 绑定。遗漏/重复/未知责任或 revision
不一致阻止继续；任一责任 child 选择 custom 无 QA 或 waived 时，整体明确 not-guaranteed。

### 8.2 合并路径

```mermaid
flowchart TD
    C["全部预期 child receipts"] --> I["engine VCS adapter 按拓扑集成"]
    MQD["合并 QA 用例设计与审查（与 slice 开发并行）"] --> I
    I --> IC{"需要语义冲突处理?"}
    IC -- "是" --> CR["主线开发 agent 只编辑冲突内容"]
    CR --> CV["adapter 验证 resolved 并继续提交"]
    CV --> MS
    IC -- "否" --> MS["物化 merge artifact 后冻结主线 snapshot"]
    MS --> AC{"child validation view 受影响?"}
    AC -- "否" --> MQE["合并 QA 执行"]
    AC -- "否" --> MG["合并门"]
    AC -- "是或 UNKNOWN" --> CRV["最终主线候选补跑受影响 child 义务"]
    CRV --> MQE
    CRV --> MG
    MQE --> MJ["merge join"]
    MG --> MJ
    MJ --> MUD{"Operator 核实后处置仍不唯一?"}
    MUD -- "否" --> MO{"存在 obligation?"}
    MUD -- "是" --> MVA["核实后处置 Ask"]
    MVA -- "直接返修 / 作废" --> MO
    MVA -- "合法 THAW 后修 QA artifact" --> MQR["修订并按 QA Review/可选建议理由处置规则冻结"]
    MQR --> MFC["adapter 冻结新主线候选 Mn+1"]
    MFC --> MRS["重算 reuse/affected；至少重跑改过的测试"]
    MRS --> MRJ["affected/UNKNOWN 重跑；其余精确复用"]
    MRJ --> MJ
    MVA -- "按需求修改重走流程" --> MRC["requirement-change barrier"]
    MO -- "否" --> FS["master Seal"]
    MO -- "是，wave < 3" --> MR["主线开发 worker 修复"]
    MR --> MS
    MO -- "是，wave >= 3" --> MA["统一用户 Ask"]
    MA -- "额外一轮" --> MR
    MA -- "再试 QA" --> MQE
    MA -- "需求变化" --> MRC
    MA -- "精确 waiver / abort" --> ME["重新判定或终止"]
```

合并后始终跑合并 QA 与合并门。Operator 完成新细节语义核实后实际处置仍不唯一时，才执行 `VALIDATION_DETAIL_DISPOSITION` Ask；用户选直接返修后才由主线开发 worker 修复，不回 child、不重新 Seal child、不重复路线或澄清。用户选修 QA artifact 时，必须在主线物化修改并冻结新候选，至少 fresh review/重跑改过的 merge 测试或用例，共享组件变化时扩大到所有依赖项；随后重新计算 merge gate、merge QA 和 child obligation 的 reuse/affected，不能直接回旧候选执行。每次冲突处置/主线 repair 后，Controller 用 topology ownership、VCS diff 和 child receipt 的 validation view 计算 `AffectedChildSet`：只改 integration-owned glue/跨片接缝且 child view 全部精确相等时，仍只重跑合并双验证；改变 child-owned 内容或 UNKNOWN 时，在同一最终主线候选上额外补跑对应 child 受影响的黑盒、白盒与普通门义务，再与合并双验证 join。合并流程使用同一 wave 计数规则：处置完成后 `wave >= 3` 才作三轮 Ask。

保证启用时，master 汇总去重所有 child/merge 的批准用例，逐 AC 核对主责任、批准用例及
执行结果。首次必要 FULL 与 AffectedChildSet 修复补验仍保留；本轮必需义务全部有效 PASS
后即可 Seal，不因保证激活额外重跑全部用例。§5.2 明确保留的已有
PASS 通过独立授权继承凭据满足相应条件，不伪称新候选执行，也不替代实际主线集成。

### 8.3 VCS adapter

统一接口程序化覆盖 provider 探测、根/身份解析、status/diff/untracked 识别、base/current/ancestry、child workspace/client、显式 track/add/open/reopen、resolve 状态、integrate、commit/submit、snapshot、final identity、已授权 reset/rollback、cleanup 和 crash reconcile：

- Git：worktree/branch、集成和最终候选冻结/必需验证前的提交整理（含必要 squash）；
- SVN：working copy、revision 与集成，不 squash；
- P4：client/workspace、changelist 与集成，不 squash。

开发/修复 agent 只编辑已授权内容，返回声明式路径 manifest、语义说明和 typed result；Controller 校验后由 adapter 执行 VCS 写操作。确定性构建/测试也优先由 engine runner 执行；只有测试设计或实现本身需要语义创造时才成为 AGENT step。

冲突路径严格固定：adapter 发起 integrate 并输出精确冲突集 → 若需要语义选择，主线开发 agent 只修改冲突内容 → adapter 验证 resolved → adapter 执行 resolve/add/open、commit/submit、snapshot 和确定性验证。agent 不运行 VCS 写命令、不决定集成顺序，也不把冲突退回 child。

reviewer/QA 读取 Controller 生成的只读 snapshot/diff artifact；artifact 绑定 provider、base/current、definition version 和 digest。reviewer 不自行推断 VCS 当前态、不执行 VCS 写操作。凭据/外部能力可通过 typed HostAction 执行，但宿主只能执行已签发参数，不能自造或重排命令。

whitebox authoring workspace、完整候选 freeze 和候选 promotion 也属于同一 VCS adapter 契约。promotion receipt 的 canonical validation view 对 Git 覆盖 tree/blob/mode 与相关引用，对 SVN 覆盖 path@revision、内容与 properties，对 P4 覆盖 depot paths、submitted changelist、filetypes 和 view；只比较工作区字节或只贴 `test-only` 标签均不足以把验证映射到最终身份。

检测外部漂移时，adapter 先冻结 observed identity、lineage、只读 diff 和 `ExternalAdoptionPreview`。adopt-external Ask 必须绑定该精确 identity；授权后保留原 run base，把外部 identity 设为新 current/development source，并写含旧/新 identity、request、preview、ImpactSet、invalidations/carry 的 `ExternalAdoptionReceipt`。affected/UNKNOWN Attempt、result、candidate、waiver、promotion intent 与 child receipt/map stale；明确未受影响项才 carry，未完成 wave 不计、已完成 run wave 不清零。需求 artifact 的语义变化转入 `REQUEST_REQUIREMENT_CHANGE`，lineage/identity 不唯一进入 Operator/Wait/reset/abort；分片只级联受影响 child 与 merge bindings。

所有非幂等 VCS 操作使用 intent/reconcile 协议，adapter 返回结构化 facts，流程层不得解析人类文本猜结果。adapter 错误只进入预声明的 retry、Wait、Operator 或显式失败；绝不改派 agent 猜测。

## 9. 宿主动作、中断与本地副作用

### 9.1 HostAction

spawn 只走 Ready。HostAction 是封闭 typed union：`RESUME_AGENT | TERMINATE_AGENT | EXECUTE_ADAPTER_OPERATION`；最后一类只引用 definition 注册的 adapter operation 和参数 schema，不能携自由 shell。每个动作先持久化 pending intent，再交给宿主。公共 receipt 字段为 actionID、operation、payload digest、宿主/provider、correlation 和 status；resume/terminate 另带 lifecycle evidence，adapter operation 另带其 schema 规定的结构化 observation/identity evidence。

provider identity、bridge installed/available 和 lifecycle paired status 分开记录。宿主是什么就绑定什么；bridge 缺失不得降级成 default。已安装桥但事件缺失先 Wait/对账，最终可 REJECTED/Operator；provider mismatch 硬拒绝。

中断与 UNKNOWN 分开处理：

- 客观瞬态且 bindings 未变：在该 Attempt 的有限 retry 余量内自动 resume；外部派发无显式
  策略时使用 canonical `MaxAttempts=2`，耗尽后进入 typed Ask；
- 新的有效义务或经确认的新计划改变 binding/snapshot/责任时，才使用新 TaskKey 和计划槽位，
  旧 Attempt terminate/stale；非瞬态错误本身不生成新义务，也不能借 fresh attempt 扩容；
- bindings 未变但原因未知：非开发任务保持既有 Ask；同批未完成开发只进入有限恢复、
  Wait/Operator/Ask 处置，不能由主代理直接选择 fresh 绕过批内身份连续；
- receipt UNKNOWN：先查 lifecycle，唯一匹配自动 attach，多重/无匹配进 Operator，绝不盲目 respawn。

开发代理的身份连续按阶段 4 REQ-007 / AC-061 强制：未完成的同一已确认 Batch 绑定
原生开发身份；同批继续和合法 retry 默认恢复该身份。正常返回、自测失败、主代理提交的
RUNTIME_ERROR 或替换理由不构成不可恢复事实。adapter 实际恢复/查询原身份取得明确
不可恢复事实后，engine 核验 Batch/任务/原身份与恢复证据绑定、旧执行已停止、未决副作用
已对账及既有额度，才签发可消费一次的替换动作。瞬时失败或 UNKNOWN 进入现有有界恢复
和处置路径，不自动换人。重放不新增代理，跨批则核验前批 DoD/验证/交接后签发新身份。
使用现有 execution、TaskKey/Attempt、HostAction intent/receipt 和生命周期持久记录；不增
第二套代理状态机。插件内部的直接 spawn、retry、重新编号与重复计划都经过相同检查，
拒绝须发生在原生调用前。此补充不负责拦截插件外直接调用 Codex 工具。

### 9.1.1 真实执行前准入

流程路线、解锁条件、操作内部顺序和失败分支由插件的 engine/adapter 程序统一控制，
不由宿主或代理重新决定。宿主适配层只负责执行已校验调用并回传真实事实；跨宿主复用
同一封闭流程，分别验证具体调用实现。固定操作应由程序封装完整步骤，不要求 AI 自行
拼接底层命令。该控制规则直接实现，不以“先探路确认宿主能否约束 AI”为成立前提。

宿主接入随派发、恢复、模型调用和 QA 等对应功能实现并实测，验证新会话身份、完成
结果、拒绝传播及中断后的任务对账。只有存在具体未决接口风险时，才按总需求 §8.5
安排有界实验，是否独立成批按交付/验收/恢复边界判断；不自动阻塞整个引擎开发。
实现前后都不能靠未受控的真实调用补齐流程；下面的覆盖要求和零调用验收保持不变。

签发与结果接纳之间必须有执行前校验。宿主通过已有 adapter/bridge，在调用原生子代理 API、
启动工作流模型 CLI/API 请求或执行已声明 QA 操作之前，向引擎核对并认领当前签发动作。
校验绑定 actionID、TaskKey/Attempt、run/revision、candidate、provider、payload digest、
scope 与精确 case set；使用现有 `pendingActions`、锁/CAS、intent 和 guard，不建立第二账本
或常驻守护进程。一个 QA 任务内的模型请求沿用该任务绑定，不逐请求创造 TaskKey。

未签发、越出用例范围、过期、已取消、已认领/消费的重复动作，或没有合法流程义务的请求，
必须在启动前拒绝，实际子代理/模型/QA 操作调用数为零。恢复已有任务走签发的 resume 与
既有 UNKNOWN 对账协议，不把拒绝重新解释为一次 fresh dispatch。已在合法任务内完成的
结果保持原绑定；拒绝一次越界请求不得污染既有 PASS、消耗审查轮次或迫使用户跳过 Seal。

受支持宿主的正常文档化入口必须覆盖原生派发工具、恢复入口和能发起该工作流模型/QA
操作的命令入口；通过实际工具调用前 hook 或宿主执行器统一接入。不能只包住 formal-gates
命令，同时保留主代理可直接发起同类工作流操作的裸入口。矩阵逐项列出入口、前置校验点、
参数绑定和实际拒绝证据；仅有 result/receipt 登记拒绝或提示词要求不算执行前强制。

宿主无法覆盖其中某入口时，应明确报告该执行前准入能力不支持，并拒绝进入承诺该保证的
执行模式；不得回退为模型自律或宣称已完成适配。保证范围是已编码的流程规则和验证过的
宿主入口，不包含模型语义判断永远正确；影响分析仍由既有 Operator 核实。普通任务外工具
操作、恶意用户改写宿主/状态等不扩入本项目范围。

### 9.2 本地副作用

start、prompt/artifact、blackbox/whitebox QA worktree、case mirror/materialize、完整候选 freeze/promotion、Git/SVN/P4 workspace/集成/提交、snapshot、Git squash、child receipt/cost、terminal summary、cleanup、abort 全部采用 StepSpec 与：

```text
persist intent -> execute -> observe/reconcile -> commit result
```

外部事实仍为旧状态时执行；已精确满足预期时只提交结果；冲突进 Operator；retryable I/O 进 Wait。不得盲目重复 spawn、squash 或集成。

批准的黑盒 cases、白盒测试和其他 VCS 内交付 artifact 在 `ValidationCandidate` 冻结前物化。Git 从 base 到 current 超过一条提交时，在最终候选冻结及最后一轮必需验证之前自动 squash，消息由 engine 稳定派生，不新增 Ask；SVN/P4 不 squash。QA 与门结果绑定整理完成的不可变候选；Seal 检查同一 candidate identity、固化结果并物化既有流程产物，不再 squash 或改变候选身份。最后验证后候选变化仍按既有影响判定、AFFECTED 与真实新增/受影响义务处理，不凭内容相似直接继承通过；§7.1 的精确 promotion/reuse 合同保持。

Controller 维护持久化 typed resource registry，登记每个 blackbox/whitebox/child/merge workspace、Git 临时 ref/branch、SVN working copy/候选路径、P4 client/pending changelist 及 cleanup state。Seal/Abort 固定执行：持久化 terminal intent 与可恢复 summary → adapter 逐项 cleanup → observe/reconcile → 写 `WorkspaceCleanupReceipt` → 核验无应回收登记残留 → Complete。cleanup 未完成时保持 `FINALIZING_CLEANUP`，不能先对外宣布 Seal 完成。promotion UNKNOWN 必须先对账，不能重复 merge 或未授权 rollback。

## 10. 旧 run、终态与诊断

正常 loader 先最小读取版本 envelope。缺 `workflowDefinitionVersion`、缺 `stateSchemaVersion` 或任一不精确匹配，均返回 `UNSUPPORTED_RUN_VERSION`，且绝不写状态。

不实现 migrate、兼容读取、旧命令续跑、legacy mode、`controlMode` 第二值或 authority handoff。旧 run 文件原样保留供只读诊断，用户使用新 ID 创建当前版本 run。

`diagnose` 绕过正常 decoder，只读报告路径、JSON 可读性、检测到的版本、当前支持版本、summary、可安全判断的 integrity 和重建建议。它不修复、不迁移、不清理。

Seal/Abort 终结路径先写 durable intent/可恢复 summary，再自动清理登记的临时资源；只有 cleanup receipt 完整且无残留后才提交 SEALED/ABORTED summary/receipt，`show/status/next` 可从 summary 回落，`next` 返回 Complete。活动 run 的 reset/abort 必须由 available action → Ask → submit 授权；不保留公开 cleanup 后门。

## 11. 实施顺序

### 阶段 0：冻结契约

- 把权威需求、本方案、SKILL 和所有参考文档统一成同一语义。
- 为最终公共命令面、旧 run 拒绝、用户节点、StepSpec/失败分类、parallel frontier、产品/技术审独立三轮、开发后三轮和 split receipts 建 contract fixtures。
- 冻结当前正常使用中仍需由内部 handler 承接的业务语义，不冻结旧命令形态或兼容行为。
- 阶段 0 的分发事务先由冻结 stable driver 调用 `install --bootstrap` 建立 registry；bootstrap 不创建 workflow state。安装、admission bridge、Go installer、Shell/PowerShell 共享同一 native owner，按 `switched -> installed-path post-switch/pre-commit smoke -> atomic runtime/pointer/config+registry commit -> journal committed` 顺序执行，失败由同一 journal 对账并恢复旧 runtime/config/registry。

### 阶段 1：纯决策内核

- 开工前定稿 ADR-001 并完成六种代表性 step（engine local、durable side effect、host action、agent task、human ask、parallel/join）的小型 compiler spike，确认 compiled IR、registry 与 canonical encoder 边界；spike 不进入 production。
- 实现版本 envelope、RunPhase、TaskKey、TaskTransitionTable、Observe/Decide/SelectIssued 和 NextResult 校验。
- 按 ADR-001 实现封闭类型变体 authoring、closed-world compiler（registry 解析、图不变量、归一化、authority/runner 派生）、单一 canonical encoder、compiler 生成的 `definitions/workflow.json` 与身份常量、eligible-step runtime。
- canonical 制品验收：authoring source 重新生成 checked-in 制品字节无 diff（独立于 round-trip 的 freshness CI）、任意 assembly 顺序同字节、decode→encode 字节不变、跨进程/重复构建同字节、definition/package digest 分离（只改实现不改 ID 时 definition digest 不变）、语义变化必改变 definition digest、registry 完备性、constructor 非法状态、mutation tests 与复杂度止损规则（新增普通业务节点不得要求修改 compiler core）。
- 用 golden traces/property tests 覆盖合法边、非法事件、step 顺序/遗漏/重复、非终态无空结果和 canonical Plan 字节稳定。
- 先以只读 shadow 比较 eligible frontier、完整 fan-out、依赖顺序和最终投影；telemetry 不写权威 state。

### 阶段 2：可靠写入与动作协议

- 实现 file lock、revision CAS、external fingerprint 重验、pendingActions 和 submit 幂等。
- 实现 SpawnReceipt、worker result、Ask/Operator event、lifecycle event 的统一接纳。
- 实现 failure-class 路由、UNKNOWN、中断、旧 Attempt、result-before-receipt 和本地 effect 对账，测试 engine 故障不会动态降级为 agent。

### 阶段 3：完整流程迁移

> 本节沿用总方案的能力分层编号；交付顺序以 `incremental-seal-plan.md` 为准。增量计划的
> “阶段 3”是先证明最小纵向 engine 闭环，本节所述完整流程迁移在该闭环 Seal 后的后续阶段
> 才执行，不属于当前阶段 3 Batch。

- 迁移 intake、产品/技术审、start-readiness、路线、两阶段需求变化、开发、白盒独立 authoring/最终候选整理与冻结、review-ready 后 QA execution 与门并行、精确 reuse、repair、精确 promotion、adopt-external 和 Seal cleanup；同步修改 `gates/implementation-quality-gate.md`、catalog scope 与 README，移除实现质量门的测试代码审查责任，测试质量只由白盒 review 承担；门内维度保持显式命名且互不压制（需求覆盖、实现-需求偏差、架构与耦合、实现正确性与质量并列，架构与耦合不回退为无名段落）。
- 强制完整 frontier 与自动 refill。
- 迁移 lightweight 例外、产品/技术审各自的 `PreDevelopmentReviewSeries` 与开发后三轮规则；当前 legacy `internal/validate` 的同语义实现只作过渡，engine 接管后不得保留第二权威计数或旁路授权。

### 阶段 4：split 与三 VCS

- 拆分绑定唯一发生在 start-readiness PASS 后的拓扑确认：`start` 无拆分意向声明，确认前意向可变、确认后不重切。
- engine 内建 child、map、SLICE_READY、receipt、case/cost 汇总和级联。
- 实现 Git/SVN/P4 非分片及分片 adapter 的完整机械面和只读 diff artifact。
- 实现合并 QA 预设计、adapter 主线集成、语义冲突窄 agent 边界、合并双验证和主线返修循环。

### 阶段 5：删除旧逻辑

本节为总体工作包编号。按用户 2026-09-08 要求，本工作包的旧工作流兼容逻辑删除
提前到增量阶段 4 完成，阶段 7 只核对后续开发未重新引入，不再保留过渡执行路径。

- 将仍需能力内收为 handler 后，删除所有旧公开推进命令和别名。
- 删除旧 run decoder/migration、legacy mode、authority handoff、legacy QA、旧 guard/hook 门禁、公开 cleanup 与废弃提醒。
- 删除或按当前契约重写仍断言旧受理模型、旧复审文案和旧 lightweight 语义的测试；禁止为了让旧测试变绿而恢复已废弃文本或行为。
- 扫描 CLI 注册表、源代码、文档和测试，证明无第二权威路径。

### 阶段 6：最终 canary 与安装

本节为总体工作包编号，对应增量阶段 7。2026-09-07 用户已把首次全局发布提前到增量
阶段 4：该阶段 QA/canary、本阶段固定 driver Seal、主线集成及必要重验完成后，执行既有原子
安装事务并核验实际全局入口。阶段 4 发布包冻结为阶段 5–7 的稳定 driver，后续候选继续
隔离验证。下面是全局从阶段 4 升级到最终完整版本的要求，不再禁止阶段 4 提前发布。

阶段 4 的开发驱动按用户同日“用当前版本开发”的决定，以阶段 4 需求案记录的精确版本
固定；这不是阶段 4 候选自驱动。当前版不兼容原阶段 0 driver 的活动状态，后续使用新
run ID，不手改迁移、不把旧结果自动记为新 run PASS；阶段 0–3 的历史绑定不改写。

增量阶段 4 的首次发布还必须完成阶段 0、1、2、3、3.5a/fix/b/c 的逐项功能回归，详见
阶段 4 REQ-022/AC-079–081。清单关联旧需求和新版承载入口；Seal 前验证隔离安装，切换后
通过真实全局入口复验并绑定发布身份，内部合同/破坏性安装用例按该需求的隔离边界执行。
迁移、替代、延期和未验证状态据实记录；历史 Seal/PASS 不替代本次证据。仍承诺提供的
功能存在回归阻塞或缺必需证据时，不能完成发布或启用新的稳定 driver，沿既有失败合同回退。

- 从清理完成的同一最终 candidate revision 构建隔离安装副本。
- 完成全部权威 canary；中途修复会产生新 revision 时，所有最终证据统一重跑，不拼接旧候选结果。
- 阶段 7 正式 run 由冻结的阶段 4 driver 驱动，最终候选不得驱动或 Seal 自己。
- final-release run 本轮必需验证全部有效 PASS（含合法继承）并 Seal 后，将全局阶段 4 包升级为最终版，实际入口检查通过后退役阶段 4 固定驱动包；每次切换失败都沿既有安装事务恢复该次发布前的稳定包。

## 12. 测试与 canary 矩阵

### 12.1 自动测试

1. 每条迁移边、pre 拒绝、Ask/Operator/Wait/Ready/HostAction/Complete。
2. 八类非法定义按 enforcement matrix 拒绝：constructor/变体层（untyped I/O、非法 reason、自然语言-only pre/postcondition、无幂等/reconcile 副作用、无 request/schema human wait）、closed-world compiler 层（非法图/不可达/循环、join/failure 覆盖、版本绑定、registry ID 完备性）；compiler 生成的 canonical 制品通过 freshness（重新生成无 diff）、assembly-order、round-trip 与跨进程确定性测试，definition/package digest 分离且语义变化必变 definition digest。
3. runtime 拒绝 step 乱序/遗漏/重复；崩溃只恢复未完成边界，不重放已完成副作用；原子纯 handler 不被过度拆分。
4. 每个 FailureClass 只走声明边，engine/adapter 故障不动态降级 agent；AGENT 只出现于三种合法 nonProgrammableReason；最终本次范围内定义不存在 `MISSING_ENGINE_ADAPTER`。
5. stable TaskKey、完整 expected set、`min(C,N)`、自动 refill、分支失败不压兄弟任务。
6. 同 action/event/request 幂等、不同 digest 拒绝、并发 submit、旧 Attempt 和 result-before-receipt。
7. 任意非终态节点触发用户需求变化，含 Phase A 权威写屏障/quarantine、确认后 ImpactSet、unknown=affected、Phase B 恢复/refill、增量复审和 wave 不误计；覆盖全部保留、部分保留、无显式保留、affected/unknown PASS 的用户保留、连续修订和迟到结果；验证授权继承能通过对应 QA/Seal 条件且原始证据不改写，FAIL/未执行项不能被保留为 PASS。
8. 产品/技术审 P0–P3 confirm/dismiss、finding/remedy 授权绑定、raw candidate/adjudication/correction/effective status 分层、错误 finding/严重度/裁决的 typed 纠正、原始证据保留和 supersede、全部 blocker 驳回后无空审查直接 PASS、纠正导致的下游精确 stale；P0/P1 新鲜复审、P2/P3 无级别强制复审；两个动作分别只自动完成前三轮，只有有效完整审查计轮，finding 后续裁决不重复/回退计轮、整轮 invalid 才排除，revision/invalidation 不清零，达到上限后 typed Ask 每次只增加一个 action-specific fresh-review TaskKey，split child 不另开 series，并证明该语义轮次与同 TaskKey 的 `MaxAttempts` retry 完全独立。
9. 三类 pre-wave review、逐条 Operator adjudication/P0/P1 rationale、finding/problem ID 与归并历史、FROZEN/review-ready 屏障、wave 收集与唯一返修、门审 P2 carry 不复审/P3 advisory、QA Review 可选 P2/P3 理由处置与合法 THAW、非 wave/wave 无效结果/RETRY_EXHAUSTED/一次性追加 attempt、dispatch signature 和完整授权摘要均有可复现测试；阶段 5–7 使用同一语义。
10. 拓扑绑定唯一时点（start 无拆分意向声明；start-readiness 后确认、确认前可改、确认后不重切）、split map、包含白盒测试的 child 最终 identity、SLICE_READY、durable receipt、child abort、级联、case/cost 汇总、adapter 集成、语义冲突窄 agent 边界、AffectedChildSet 与主线精确补验。
11. Git/SVN/P4 的 provider/status/diff/track/integrate/commit/snapshot、whitebox workspace、candidate freeze/reuse/promotion、adopt-external、resource cleanup、typed artifacts、错误对账，以及代理无法执行 VCS 写操作。
12. 版本缺失/不匹配严格拒绝；diagnose 原始只读；终态 summary 回落。
13. 正常入口与常见误操作黑盒；范围外对抗输入、手工状态编辑、权限/不可变文件和未支持 OS 只作 P3 建议。
14. 执行前准入通过真实宿主入口验证：纯 P2、未启用 guarantee 且无用户例外时请求 FULL；
    仅改 final/reason/override；扩大已签发 case set；不带 actionID 直接调用原生派发工具或
    工作流模型命令；重放已消费动作；使用取消/过期 Attempt。各反例均在底层调用前拒绝，
    以调用计数/宿主记录证明零执行，不能只断言结果登记失败。正例覆盖合法 AFFECTED、
    有首次必要 FULL 义务、绑定的一次用户例外和合法 resume；它们必须能实际运行。
    RUNTIME_ERROR 获批重试须能接纳结果且保留原错误，PASS/FAIL 重放保护和已有通过结果
    不受非法尝试污染。执行前能力缺失须明确拒绝；测试不另建真实预算或语义真伪证明器。

15. 主需求 §8.4.1 的保证激活、单文件、非保证分支、逐 AC 唯一主责任、合并完整用例义务与有效 PASS，以及用户授权继承优先关系均有正反向证据；不能以普通 AffectedChildSet 测试代替保证模式验收。
16. §7.2 的共享审批/引用失效、可选建议处置、白盒已有测试复用与错误断言识别、干净副本批量执行、checkpoint 完整结果/未完成/绑定变化/有限续跑、Codex 来源/请求去重/任务计时/最终结算从 engine 路径验证；对应 contract fixture 和既有 parser 测试复用，不增加每阶段模型 benchmark。
17. regular 受理验证 start 绑定与预检不产生确认，全文/revision 对应的用户决定只经 submit 登记；确认前不得推进路线或后续派发，过期 revision 拒绝。
18. 阶段 4 从 engine 公开路径验收审查收敛需求 REQ-002、REQ-004 和 REQ-010：全严重度缺分类/实现细节 P0/P1 拒绝及合法三类阻塞；RESOLVED/DISMISSED 不重复返修、错误归并在派发前后进入正确计划/wave；相同 PASS 自动去重、用户明确重审的理由/授权/额度及原结果保留，缺少授权或额度时拒绝。阶段 5–7 沿用同一合同。

### 12.2 确定性崩溃测试

阶段 4 须从真实首宿主操作入口验证节点内部顺序强制：Git manifest/范围校验、track、
commit、snapshot 及已声明 QA 的准备、目标操作、采集各自依次执行。主代理或子代理跳步、
倒序、重复已完成步骤或前置失败后继续时在底层调用前拒绝；中断仅恢复未完成步骤。
原子 handler 保留内部程序顺序并验证失败短路，跨持久边界沿既有 StepSpec/intent 对账；
不能只证明阶段切换正确或提示词列出了操作步骤。详见阶段 4 AC-076。

用 fake host/VCS adapter 在每个持久 StepSpec 边界逐点注入：intent 前后、spawn 后 attach 前、result 先于 receipt、submit 成功但响应丢失、临时文件 sync/replace 前后、两个并发 submit、旧 Attempt 迟到、whitebox workspace 创建后登记前、candidate freeze/promotion 后 receipt 前、track/commit/integrate 后 receipt 前、Git squash 后 state 保存前、child SLICE_READY 后 master 集成前、语义冲突编辑后 resolved 校验前、cleanup 每个资源后 registry 提交前、merge QA/门记录前。

### 12.3 权威完整 canary

VCS 六格全部跑通：

| VCS | 非分片 full | 分片 master/child/merge full |
| --- | --- | --- |
| Git | 必须 | 必须 |
| SVN | 必须 | 必须 |
| P4 | 必须 | 必须 |

五个宿主各自在真实环境跑一次 Git 非分片 full：产品审 → start-readiness → 开发与黑盒设计并行 → 实现 snapshot → whitebox workspace/design/review 与可选建议理由处置 → 最终提交整理（含必要 squash）→ 全部 case-set FROZEN 并绑定最终完整候选 → review-ready 后黑盒/白盒执行与门并行 → 真实或注入 FAIL → repair/整理并冻结新候选 → 按 AFFECTED 与真实新增/受影响义务重验 → promotion 中断/恢复 → 无残留 cleanup → Seal。每次保存 provider/bridge、SpawnRequest/Receipt、worker result、revision、candidate/reuse/promotion/cleanup receipt、结果和 Seal 证据。

允许一个宿主 Git 非分片 canary 与 VCS 六格中的 Git 非分片重叠，因此最低是 10 个完整 canary，而不是 30 个笛卡尔积。hook-only、lifecycle-only、lightweight 或 smoke 均不能替代任何完整 canary。

## 13. 完成判定

阶段 4 的范围内版本按增量计划先行发布；以下条件同时成立后才把全局阶段 4 版本升级为最终完整版本：

1. 相同 state+observation 的 canonical Plan 字节稳定，非终态无静默死端。
2. 每个有顺序语义的节点由封闭变体定义编译出的 NodeExecutionPlan/engine handler 机械执行，乱序、遗漏、重复和未声明失败边不可发生；活动 run 的 DefinitionDigest 与 PackageDigest 双绑定经 loader 校验。
3. 可程序化动作不落入 agent；engine/VCS adapter 故障不动态降级 agent；AGENT/HOST 均有合法、可审计理由。
4. 并行任务无漏派、无选择性忽略、无容量闲置；未形成完整候选的验证不会被错误标为 eligible，完整候选就绪后全部验证立即并发。
5. HostAction、本地副作用和所有确定性崩溃窗可恢复或明确进入 Operator。
6. 用户节点、Operator 核实后处置仍不唯一的 Ask、任意时点需求变化、审查来源规则和三轮规则全部有可复现证据。
7. Git/SVN/P4 六格与五宿主 canary 全部绑定同一最终 candidate revision/安装副本并 PASS。
8. 最终树不存在旧 run 兼容、迁移、旧公开入口、双写入口、legacy 回退或 cleanup 后门。
9. 独立产品审、技术审、QA 和所有选定门按 formal-gates 完成，当前开发 run Seal。
10. `go test ./...` 在不恢复过时受理/复审/lightweight 断言的前提下全部 PASS；基线中已确认的旧文本断言已删除或改写为当前 engine 契约。

实现中的字段名、包拆分和内部 phase 名称可以在不改变上述可观察语义的前提下由开发者确定；不再存在需要用户补选的产品或路线开放项。
