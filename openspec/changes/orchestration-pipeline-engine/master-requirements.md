# 编排流水线化（确定性引擎交权）· 主需求

> 状态：已由用户确认，进入 formal-gates 正式流程。
> 需求归属：formal-gates 自身重构。
> 方案文档：`refactor-plan/final-implementation-draft.md`（方案稿、SKILL 与参考文档冲突时以本文为准）。

## 1. 目标

1. 把 formal-gates 的编排决策从主代理收进确定性 Go 引擎；主代理只承担用户接口、受理、证据核实与决定呈现，不临场选择下一条 workflow 命令。
2. 两条目标等权：
   - 编排不再因主代理漏步骤、选错分支或选择性忽略并行任务而卡死；
   - 主代理不再消耗 token 推理流程下一步。
3. token 目标以结构性标准验收：主代理不读迁移表、不判断 pre/parallel、不选择 Ready 子集，主代理只调用固定短入口，程序消费完整签发集合；不设置固定百分比阈值或大规模 benchmark 门槛。

## 2. 本次交付边界

1. 当前产品包含确定性内核、可靠动作协议、完整流程、split 与三 VCS、安装和按实际能力取证的验证。最终制品只保留 engine 权威路径；旧 run、旧编排入口及其迁移、兼容和回退旁路全部移除。
2. VCS 后端 **Git、SVN、P4** 全部纳入 engine 交权；非分片和分片 master/child/merge 都是本次范围。
3. 宿主 **Claude Code、Codex、Cursor、DeepSeek Harness** 全部纳入验收；每个宿主须在真实环境中独立跑通 engine 唯一权威的端到端正式流程 canary。hook/lifecycle-only live canary 或 smoke 不构成交权证据。
4. 分片流程本次纳入：拆分意向与精确拓扑、child 继承与路线、`SLICE_READY`、durable child receipt、主线集成、合并 QA/合并门、主线返修、成本和批准用例汇总全部由 engine 接管。
5. 缺陷交接单 #2 **修**：取消 `workflow start` 的强制拆分意向声明与启动钉死——`start` 不接受也不冻结拆分意向；拆分绑定唯一发生在 start-readiness PASS 后的拓扑确认（split 需精确拓扑、no-split 需理由留痕），确认前用户可改变意向、无需重启或 reset，确认后不得重切（变更走用户需求变化、reset/rebuild 或 abort）。
6. 本次不承诺 Windows/macOS/Linux 支持矩阵。实现须避免无必要的单一 OS 假设并保持可移植；跨 OS 实机适配待有对应设备后处理，不支持平台发现只作 P3 建议、不阻塞本次 PASS。

## 3. 最终公共面与清理规则

### 3.1 workflow 公共入口穷举

1. `workflow start`：唯一 bootstrap 写入口；创建精确当前版本的 top-level regular、retained master 或 lightweight run。child 由 engine 内部 handler 创建，不要求宿主拼接多个 `start`。
2. `workflow drive`：不带外部事件，确定性推进到下一个外部边界。
3. `workflow submit`：唯一外部事件写入口；提交用户决定、主动控制请求、worker 结果、HostAction receipt 与 lifecycle 事件。不得保留 `drive --event`、`submit-result` 或其他第二写通道。
4. `workflow show` / `status` / `next` / `diagnose`：严格只读。`next/status` 可展示带 freshness token 的 `availableActions`；`diagnose` 可原始只读检查 current、terminal、unsupported、corrupt、missing 状态。
5. `workflow guard-preview --input <basis.json>`：公开只读、非权威的生产 guard 计算预览；schema/fixture 见 `references/guard-preview.md`。不创建 run、不写状态、不认证真实确认或任务合法性；`runtimeIntegration` 报告权威 engine 集成状态，实际签发资格由 engine 事务核验。
6. 包安装、卸载、hook、lifecycle、canary 与脱离 run 的快速 gate 检查属于维护/诊断面，可以保留，但不得绕过 engine 修改 workflow 状态。

### 3.2 必删内容

1. 删除旧公开推进命令、兼容别名、legacy mode、`authority-handoff`、旧 run 读取/迁移、legacy QA 合并态、旧 guard、旧流程 hook 门禁与废弃提醒。
2. `prepare-*`、`record-*`、`claim-dispatch`、`route`、`snapshot`、`carry`、`seal` 等仍需的能力改为 engine 内部 handler，不保留可绕迁移表的公开入口。
3. 删除公开 `workflow cleanup`。活动 run 只能经用户授权的 reset/abort；终结路径先持久化 terminal intent/可恢复 summary，再由 engine 自动清理全部登记资源并核验无残留，最后才提交 SEALED/ABORTED summary/receipt 与 Complete。
4. 严格拒绝旧 run 的 fixture/测试可保留；它们只证明拒绝，不构成兼容能力。
5. `write_block` 保留为辅助写隔离，边界保持仓库根、owner 对话和开发阶段作用域；主门禁只在 engine 内。

## 4. 旧 run、终态与本次开发兜底

1. 最终制品只接受由当前 engine 创建、且 `workflowDefinitionVersion` 与 `stateSchemaVersion` 均精确匹配的 run。缺任一字段或任一版本不匹配，正常 loader 在写入前返回 `UNSUPPORTED_RUN_VERSION`。loader 同时精确匹配 definitionSource/definitionDigest 与 owning runtime 的 packageDigest/installed-target identity（见第 5 节第 9 条）；不得仅凭版本号相同而接受定义或实现已变化的 run。
2. `show/status/next/drive/submit`，以及经 `submit` 请求的 reset/abort，均不得把旧状态读成当前状态后改写。只有 `diagnose` 可绕过正常 decoder，以原始只读方式报告路径、JSON 可读性、版本、受支持版本、summary、可安全判断的 integrity 与重建建议。
3. 不提供自动或显式 migrate、旧命令续跑、legacy mode、`controlMode` 第二值或 engine→legacy handoff。旧 run 逻辑上终止，保留原文件供诊断；用户以新 ID 创建当前版本 run。
4. 清理完成后提交的 SEALED/ABORTED summary 可供 `show/status/next` 只读回落，`next` 返回 Complete；清理中的 run 保持 `FINALIZING_CLEANUP` 并可恢复，最后一个相同 event 可幂等重试。
5. **仅限本次开发的外部兜底**：当前已安装稳定包继续驱动 `orch-engine-003`。候选包在隔离安装目录和测试项目验证；当前 run Seal 且最终候选全部权威 canary PASS 后才切换全局安装并退役兜底。兜底不进入最终产品能力。

## 5. 确定性内核与外部指令

1. Go 静态迁移表是唯一流程权威；`Observe`（只读外部事实）、`Decide`（纯函数、产生完整 canonical Plan）、`SelectIssued`（按 Admission 机械裁剪）分离。
2. 每个节点编译为有序 `NodeExecutionPlan`。已知顺序、前后置条件、分支、重试、等待、fan-out/fan-in 和副作用协议都由 Controller 机械执行；不得只在代理提示词中描述顺序后期待代理自觉遵守。
3. 执行责任使用两个正交维度：`DecisionAuthority = ENGINE | AGENT | HUMAN`，`RunnerKind = ENGINE_LOCAL | DURABLE_ACTIVITY | HOST_ADAPTER | AGENT_WORKER`。HOST 只表示外部能力/执行位置，不拥有流程决定权；HUMAN 只能经 Ask。Operator 是当前主代理执行的 `AGENT + AGENT_WORKER + SEMANTIC_JUDGMENT` typed observation，不是 HUMAN 决策，也不能借 Operator 执行 engine/adapter 动作。
4. 能确定性实现的操作必须由 ENGINE 决定并通过本地 handler、durable activity 或 host adapter 程序化执行。AGENT 只允许用于 `SEMANTIC_JUDGMENT`、`CREATIVE_IMPLEMENTATION`、`INDEPENDENT_REVIEW` 三类不可程序化工作；“实现麻烦”不是理由。缺 adapter 只可在开发期 diagnostic compiler mode 标记为 `MISSING_ENGINE_ADAPTER` 技术债；该定义不得进入 executable plan、不得签发 Ready/HostAction，正常 compile/drive 必须以 `BLOCKED_BUG` 和 diagnose 拒绝。本次范围内的最终候选不得残留该 marker。
5. 纯内存、廉价、确定性的连续变换，或能够在一个原子/幂等事务中一起恢复的操作，可留在单个 engine handler 内，由代码顺序机械保证，不要求把每条语句都持久化。若步骤涉及独立恢复/重试/必要的单次操作限时/补偿/幂等、不可逆副作用或 UNKNOWN receipt、人/代理边界、fan-out/fan-in、独立审计/版本/证据，或必须避免重放已完成前缀，则必须拆成 Controller 跟踪的 `StepSpec`。例外仅限 Ready 的固定派发协议：transport 已由原 native owner、typed SpawnReceipt 和 lifecycle 事实独立持久跟踪时，可与 worker result 共用一个 `CompiledAgentStep`（见第 12 条）。
6. 定义的唯一 authoring 形态是封闭 Go 类型变体加 constructor 加显式节点/步骤表（ADR-001）；不引入 JSON/YAML 工作流 DSL、通用 schema 解释器、用户自定义节点插件或任意脚本/表达式扩展点。编译产物为具体结构组成、由 compiler 生成、checked-in 且字节级稳定的 canonical 制品；其步骤同样采用公共头（id/nodeID/ordinal/dependencies/kind/definitionVersion）加封闭变体 payload 的结构，变体不适用的策略字段不得存在于该步骤，不得实现为全字段平铺的单结构。每个步骤（`StepSpec`）在自身变体内包含 `id`、`nodeID`、`ordinal/dependencies`、可执行 pre/postcondition 引用、由变体派生并物化的 decision authority 与 runner kind、typed I/O codec 引用、retry/timeout（timeout 仅用于必要的单次操作限时，不限制执行时长）、idempotency key、side-effect/receipt policy、interrupt policy、parallel group/join policy、failure-class map、definition version 与稳定 registry ID（handler/predicate/codec/reconcile）。制品不含函数、闭包、内存地址、绝对路径、当前时间或无序 map。AGENT 另须合法 `nonProgrammableReason`；可执行 HOST_ADAPTER 的 `hostBoundaryReason` 只能是 `EXTERNAL_CAPABILITY_BOUNDARY | USER_IO_TRANSPORT | AGENT_DISPATCH_API`。`MISSING_ENGINE_ADAPTER` 只是 diagnostic-only definition marker，不是 runner reason。
7. 八类非法定义拒绝结果全部保留，不因实现分层减少：不可达步骤/非法循环、无类型输入输出、仅以自然语言表达的 pre/postcondition、无幂等或 reconcile 的副作用、无 request/schema 的人工等待、无 join/failure policy 的并行组、缺合法理由的 AGENT/HOST，以及未绑定 definition version 的执行计划。局部非法组合优先由封闭类型与 constructor 消除（非法状态在正常 authoring API 下不可构造），全局图不变量由小型 closed-world compiler 校验，runtime loader 对版本绑定做最后防线；executable definition 只有在同一候选包 registry 完整、唯一解析其全部 registry ID 时才能激活。运行时只允许当前 eligible frontier，拒绝乱序、遗漏和重复 step；代理只看到当前已解锁步骤的最小输入、typed output 和 postcondition，不能决定下一步。
8. 引擎失败不得静默或动态降级给代理/LLM。固定分类为：`TRANSIENT_ENGINE_ERROR` → 有限机械 retry/backoff；外部 agent/host 派发缺显式 retry policy 时使用 canonical `MaxAttempts=2`（首次执行 + 一次自动重试），compiled definition 可声明其他有限值但不得无界；额度或 retry 耗尽进入 typed Ask，不得伪装成 Wait。`BUSINESS_REJECT` → 声明的业务边；`USER_ACTION_REQUIRED` → Ask/Wait；`SIDE_EFFECT_UNKNOWN` → reconcile/Wait/Operator；`INVARIANT_VIOLATION`/`BLOCKED_BUG` → 显式失败并 diagnose；只有定义中预先声明的 `AGENT_RECOVERABLE_SEMANTIC_ERROR` 可进入代理语义修复。
9. `state.json` 继续作唯一权威工作流投影；新增单调 revision/CAS、`pendingActions`、精确版本字段、按 TaskKey 派生的当前 Attempt、已确认 `granularity_review`/Batch/Subtask 计划及其 digest、持久化已确认 `requiredSources`、`AcceptanceManifest`、source↔point↔case map 及其 digest、typed `ReviewScopeMode` 和逐项 QA approved whitelist，以及产品/技术审分层的 raw candidate、finding adjudication、correction receipt 与 effective review status，不引事件溯源、SQLite、watchdog、OS 定时器、repo 级队列或开放式通用 Effect/Artifact 框架。原始 review candidate 不删除；纠正以绑定精确 result/finding digest、revision、原因和授权来源的有界 typed receipt supersede adjudication，再由 Controller 纯函数重算有效状态与依赖失效。StepSpec 的 side-effect/receipt 与 artifact 仅允许定义中穷举的 typed policy/schema。3.5b guard 额度只由 engine Controller 从当前 revision 的 canonical expected external TaskKey、已确认 route/topology、compiled definition 和有限 retry policy 纯函数计算；主代理、host、用户和 submit event 不得传入底层数字。expected TaskKey 只能由 closed-world definition 与已确认、持久化的 Batch/route/topology、已批准 QA 条目、已选 gate 或已成立 obligation 派生；当前已确认 workflow policy 预授权的 obligation/repair slot 由 Controller 机械加入，其他新增槽位若按现有流程要求用户确认则必须先绑定对应 revision/typed Ask。普通代理结果、host receipt 或 resume 不得直接或静默扩容。计算 basis digest、结果和已用 action/Attempt 账目随同一 state 投影持久化，每次签发前重算；相同 basis 结果不一致、digest/账目不匹配或将要超发时以 `BLOCKED_BUG` 零派发，不得降级给 AI 猜测。run envelope 同时绑定 definitionDigest 与 owning runtime 的 packageDigest/installed-target identity：packageDigest 是执行绑定而非审计附注，loader 在写入前必须校验其与实际执行 runtime 一致（或由 run 绑定的不可变 runtime sibling 执行并验证实际摘要，或持有显式 adopt receipt），不得仅凭 HandlerID 相同接受接管。三类身份职责分离：DefinitionDigest 绑定拓扑/registry ID/策略，PackageDigest 绑定实现字节与安装包，PlanDigest 绑定给定 state+observation 的决策结果；HandlerID 标识可恢复执行合同，合同不兼容变化必须晋升 ID，合同兼容的实现变化保持 ID 并由 PackageDigest 标识。
10. 动态任务走 `expectedTasks` / `TaskKey` / `TaskTransitionTable`；迁移表只管理 run-level phase。
11. `NextResult` 只有 Ready、HostAction、Ask、Wait、Operator、Complete 六类外部边界；同一 canonical Plan 的 Kind 唯一。
12. `Ready`/`HostAction` 只携薄指针和 actionID；程序按原计划整批执行，主代理不能选择或改写。每个 Ready agent action 由同一 `CompiledAgentStep` 绑定 `AGENT + AGENT_WORKER + nonProgrammableReason` 的 worker result 合同，并由 Controller 的固定程序化派发协议承担 `ENGINE + HOST_ADAPTER + AGENT_DISPATCH_API` 的 spawn transport/SpawnReceipt 边界。两边共用同一 actionID、TaskKey 和 Attempt；transport 的完成、未知、失败与恢复由原 native owner、SpawnReceipt 和 lifecycle 事实独立跟踪，worker 完成由原 typed result 跟踪，不另建 transport StepSpec、任务、完成状态或 typed 字段。spawn 仍属于 Ready，不进入 HostAction。HostAction 是 `RESUME_AGENT | TERMINATE_AGENT | EXECUTE_ADAPTER_OPERATION` 的封闭 typed union；adapter operation 只能引用定义中注册的 operation/schema，不得成为自由 shell 通道。
13. `AskRequest` 只承载用户拥有的决定；`OperatorRequest` 只要求主代理核实证据、分类影响或处理无法自动对账的事实，Operator 本身不能替用户授权。
14. 所有主动控制先从带 freshness 的 `availableActions` 或受限 `REQUEST_*` 事件创建 pending Ask，再由 `submit` 完成。禁止无 request freshness 的自由 `USER_*` 写事件。
15. `submit` 校验 request/event/action ID、当前节点、schema、source bindings 与 digest；同 ID 同 digest 幂等返回稳定 acceptance/status，不重新发出已 ISSUED SpawnRequest；同 ID 不同 digest 硬拒绝。
    HUMAN 决定须经当前主宿主的原生真人确认；自然语言仅提出申请，模型选择或保存候选不构成批准。程序向同一宿主连接展示绑定具体操作及原材料的确认表单，真人明确选择后直接提交原对象，继续沿原 freshness 和幂等接纳路径。启动、typed 控制 Ask 与受管原生问题均遵循此合同；普通 CLI 自报确认、模型填写批准字段或确认收据、自动批准 hook、headless client 自行回答均不作为真人来源。表单无默认批准；拒绝、取消、空回答、能力缺失及取消后的迟到回答均不得执行。
16. provider 身份、bridge 安装/可用性与 lifecycle 配对分开建模。宿主是什么就绑定什么；bridge 缺失不允许把 provider 降级为 default，provider mismatch 硬拒绝。

## 6. 用户节点、Operator 与自动分支

### 6.1 宿主受理

1. 普通修改请求由插件提醒一次；复杂请求在完整需求/方案确认后、动手前再提醒一次。两次都不要求用户回应，不阻塞普通直接处理。
2. 用户明确提出 formal-gates 时直接进入完整受理，不在完整需求/方案确认后重复询问“是否进入正式流程”。用户可随时主动取消，但引擎不得制造重复 yes/no Ask。
3. 受理阶段逐项澄清重大需求与技术选择，展示完整整合后的需求和方案及精确 revision，明确 lightweight 或 regular 后，以绑定原材料和具体启动操作的原生真人确认完成受理。具体流程配置、开发计划和门集合仍在相应原 typed 确认节点确认。
4. `start` 后首个 drive 自动登记已确认 intake receipt/digest，不重复询问同一份需求确认。
5. regular 不要求用户原始输入采用 OpenSpec、PRD 或其他固定格式。AI 直接把对齐结果写入唯一正式 UTF-8 Markdown 需求案，不新增草稿或人工覆盖清单；自然语言正文保留，唯一 `## 需求点` 章节以固定 `REQ → 要求/验收条件/来源` 和唯一 `AC` 结构声明全部必须实现、改变产品/流程行为或需要 QA 保证的义务。确定性预检只校验结构、字段、唯一性与边界；通过后仍由用户确认当前完整文件及 revision，后续产品审、技术审、开发和 QA 只消费该确认 revision。纯实现位置、代码边界或未来迁移约束若不改变产品/流程行为，保留在方案正文并交由技术审、diff、代码审查或实现质量门验证，不伪装成必须 QA 的 `AC`。lightweight 不写该正式需求案。

### 6.2 正常 Ask

1. lightweight/regular 模式（用户尚未明确时）；lightweight 是 `start` bootstrap 例外，不能由 regular run 后期切入。
2. 高置信拆分的精确拓扑：边界、数量、依赖、并行度；低置信不拆或不确定由 engine 自动记录 no-split 理由。
3. regular 非分片 run 与每个 child 直接确认当前具体配置、开发计划和候选门集合，可选择普通门的任意子集（含空集）或全集，黑盒/白盒 QA 各按整组选取。UI 可接受命名配置，但每个 child 持久化独立决定，不提供路线档位。
4. 产品审、技术审经 Operator 验证成立的每条 P0/P1/P2/P3 finding，以稳定 ID+digest 逐项 confirm/dismiss；可同屏展示但不得只存一条全局决定。Ask 已附带唯一、完整修法时，confirm 同时批准该修法，不再重复询问；只确认问题而修法仍含用户拥有的产品/重大技术选择时，必须继续澄清并按 §7.2.1 确认完整候选的 review-delta 后才能修订。用户还可对已接纳 finding 的有效性、严重度或 adjudication 发起绑定原 result/finding digest 与当前 revision 的 typed correction Ask；原始结果保留，纠正只 supersede 裁决并重算有效状态。

### 6.3 条件 Ask 与用户主动事件

1. 任意非终态时刻，用户可明确为本次 run 切换整套命名配置、修改节点启停、QA/普通门选择或节点平台/模型/推理强度。经原 typed 确认保存当前 run 的具体配置；局部编辑保留其他当前有效 tuple、QA 与门选择，命名选择整体采用保存副本，整套配置与原单执行组选择以确认 revision 决定先后。已 claim 且仍启用的调用保留原绑定、权限和费用归属；关闭职责沿原生命周期停止并失效，晚到结果不生效。用户可从已完成或跳过的合法节点继续，原节点继续入口展示依赖失效、复用与重做处置后确认；不新增需求变化或第二路由状态机。默认文件修改不自动传播到 run，既存 child 不自动跟随父后续选择。
2. `REQUEST_REQUIREMENT_CHANGE`：在任意 ACTIVE、非终态阶段均可提交，具体规则见第 7 节。
3. adopt-external、带精确保留/清除预览的 reset、abort。
4. 原因未知且 bindings 未变的代理中断：resume 原 attempt、fresh attempt 或 abort。
5. 黑盒用例、白盒用例/测试、merge QA 用例三类 pre-wave review 各自连续三次 FAIL 或长期不可用：重新设计、fresh review、修改需求、对精确 case-set/candidate 带理由 waiver/skip，或 abort；各自独立计数，均不计入开发后 repair wave。
6. 产品审或技术审各自已经完成当前有效上限内的三轮语义审查、但按 finding 处置规则仍需完整新鲜复审：只可为该动作授权恰好一轮、修改需求/方案、按既有规则处置精确 finding/review override、reset 或 abort；每个额外完成轮后仍需复审则重复同型 Ask，不得预先累积或连带授权另一动作。
7. 用户主动要求的复审规则 override、已 PASS 结果重审、fresh dispatch、同快照 QA 重跑、提前精确 Seal waiver。
8. 开发后 QA/门 finding 先由 Operator semantic adjudication 核实严重度、范围、根因、复现和重复关系，这些判断不属于用户 Ask。只有核实后实际处置仍有多个合法分支时，才生成 `VALIDATION_DETAIL_DISPOSITION` Ask，供用户选择直接返修、合法 QA artifact 修订、需求变化或作废。
9. 开发后 `wave >= 3` 仍有 obligation 时，一个 Ask 同时提供：恰好一轮额外修复、再试 QA、对精确 obligation IDs 授权 waiver、用户需求变更、abort。每个额外失败 wave 后重复同型 Ask。
10. 长期 RUNTIME_ERROR：重试对应 QA/门（scope 重新按事实判定）、精确 skip、需求变更或 abort。
11. master/child 的 reset、abort、重大需求变化和其他破坏性级联。
12. 任意 ACTIVE、非终态节点均提供 HUMAN 用户 typed 控制 Ask。用户可授权从当前节点跳转到任意合法业务节点；请求必须持久化 freshness token、request ID、requirement revision、from/to 节点、reason 与 authorization receipt。引擎改变 current 前，先 suspend/quarantine/invalidate/reconcile 受影响 Attempt、结果、候选和副作用；终止目标仍统一走 cleanup。

### 6.4 Operator/Wait/自动分支

1. Operator 负责拆分/路线建议、finding 正常入口和证据核实、需求变更影响集、carry，以及 engine/adapter 已穷尽结构化 observe/reconcile 后仍存在多重匹配、无唯一结论或语义歧义的 receipt UNKNOWN、provider/bridge、本地 effect、VCS 冲突事实判断；常规对账不能先交 Operator，Operator 也不能提交用户决定或执行 adapter 动作。
2. Wait 只用于容量为零、任务仍在运行、依赖未满足、receipt/lifecycle 尚可能迟到或 retryable I/O；Ask 不得伪装成 Wait。
3. 客观瞬态中断且 bindings 未变、有限 retry 余量未耗尽时自动 resume，默认 MaxAttempts=2；RUNTIME_ERROR/REJECTED_INVALID_RESULT 重派复用 TaskKey。只有新的有效义务或经确认的新计划改变 binding/snapshot/责任时才生成新 TaskKey；非瞬态错误本身不新增槽位。耗尽 RETRY_EXHAUSTED 后的一次性追加 attempt 按 §8.4 用户授权，不无限重试。
4. receipt UNKNOWN 先查 lifecycle；唯一匹配自动 attach，多重/无匹配进 Operator。result-before-receipt 暂存并在对账后接纳；新 Attempt 后的旧结果以 `OBSOLETE_RESULT` 可见拒绝。
5. 无 obligation 时自动 Seal，不增加“是否 Seal”Ask。

## 7. 需求变化与开发前复审

### 7.1 来源必须分离

1. `USER_INITIATED_CHANGE`：仅此来源允许产品审/技术审使用影响集增量复审。
2. `REVIEW_FINDING_FIX`：产品审或技术审确认的 P0/P1 导致的修订，必须由全新零上下文代理对对应阶段的完整最新文档进行新鲜复审；不得通过增量 carry 减免。产品修订影响技术前提时，产品复审 PASS 后再做新鲜技术审；技术修订改变产品语义时先回完整产品复审。
3. 产品审/技术审确认的 P2/P3 仍逐项修订/修复，但不因严重度本身强制复审；P2/P3 处置驱动的需求/方案修订默认 meaning-preserved——不清审、不重审、不消耗审查轮次。修订是否实际形成含义变化由用户在修订确认时经 typed Ask 声明；主代理与引擎不得把 P2/P3 修订自行升级为需求含义变化，也不得据此触发复审。经用户声明为 meaning-changed 的修订再按其真实来源和影响进入相应复审。

### 7.2 用户主动需求变化可发生于任意非终态时刻

1. `REQUEST_REQUIREMENT_CHANGE` 在 Ask、Wait、Ready、Operator、产品/技术审、路线、开发、QA/门、修复、分片、主线合并和尚未提交完成的 Seal intent 中都必须可达；SEALED/ABORTED 后的新需求创建新 run。
2. barrier 固定分两阶段。Phase A 在尚无新确认 revision/`ImpactSet` 时，立即撤销未完成 Seal intent，并停止新的权威 VCS/状态写入、候选 promotion 和业务任务签发；可安全隔离、不会提交权威副作用的在途计算可以继续到 terminal，但结果一律 quarantine，不能推进 workflow。此时没有任何任务可被猜成“已知不受影响”；暂时无 eligible task 不违反最大并行不变量。
3. 用户先重新澄清并确认完整最新版需求/方案。只有确认后，Operator 才基于旧/新完整 revision 建立 changed items、transitive impacts、affected tasks/artifacts、carry reasons 的 `ImpactSet`；任何无法证明未受影响的任务、结果、candidate 或 receipt 一律归入 affected。`ImpactSet` 是给用户的保守建议，不是执行决定：哪些已走过的环节（含产品审/技术审、QA、门、开发与既有结果）需要重走、哪些结果续用，由用户在 typed Ask 中逐项确认或调整后由引擎机械执行；用户未决定前不得自动失效、自动重放或自动豁免任何已走过环节。
4. Phase B 机械 terminate/stale 用户确认重走范围内的在途 Attempt 和结果；对用户确认续用的项恢复/接纳 quarantine 结果并携原 revision、证据和理由 carry，随后立刻按新 eligible frontier 补满容量。旧 revision 结果可留证，但未获 carry receipt 不得推进。
5. 增量产品审/技术审读取完整最新文档作上下文，但只重新判断变化项、传递影响和 carry 是否仍成立；影响无法可靠圈定或跨共享语义时扩大到全量。
6. QA 用例、路线、拆分拓扑、开发任务和验证结果按同一 ImpactSet 增量更新。开发中受影响 worker 用新 Attempt；开发后形成新快照，QA scope 按持久化 typed `ReviewScopeMode` 决定：黑盒/白盒 QA 用例或测试审查首次建立基线时为 `FULL`，其后仅对新增、修改或 ImpactSet 受影响项使用 `AFFECTED`；产品审、技术审和普通质量门始终为 `FULL`。该标记随 review/candidate/结果持久化，并保留未来开放给用户配置的类型能力。
7. 用户需求变化本身不计 repair wave，也不清零已完成的 run 级 wave；被变化打断、未满足完整 expected set 的旧 revision wave 不计数。
8. 分片由 master 接收需求变化并级联：受影响 child 暂停、重建或重新验证，未受影响 child 可继承；child 不得私自背离 master。已进入主线集成阶段后，普通需求增量由主线开发 worker 实现；若变化本身改变拓扑，再重建受影响 child/map/receipt。

### 7.2.1 需求内容确认与回退授权

审查来源的修订由程序按上一已确认 receipt 保存并取回与 revision 匹配的完整需求与方案 base，生成穷尽 review-delta；主代理逐 hunk 核实语义与来源。用户只确认变更包；完整 candidate revision 绑定为新需求，未变内容继承。首次受理仍确认全文，主动查看全文不改变增量确认范围。Controller 不持有第二份 base/diff 账本，不允许无关 hunk 混入。

需求影响由 Operator 建议，Controller 不判断自然语言含义。`meaning-preserved` 不产生回退 Ask，保留产品审 PASS，只重做实际受影响、未通过或尚未完成的技术审；正文变化仍按 review-delta 确认。`meaning-changed` 在接纳 revision 转换前必须生成绑定 state revision、base/candidate revision、影响预览 digest 的 typed Ask，展示目标/范围/对外行为/验收变化、失效 PASS、回退步骤、产品审已用/剩余额度和授权状态。只有用户确认事件才能原子登记新 revision、作废旧产品审 PASS、置产品审 PENDING 并使下游失效；不重置额度、不自动授权新产品审。缺失、过期或不匹配时零状态变化。一次决定明确覆盖 review-delta 与回退后果即可共用授权。engine 是唯一 writer，其他入口不得绕过校验。

### 7.3 产品审/技术审 finding

1. worker finding 是候选。Operator 对 P0–P3 逐项核实需求前提、正常入口、复现、证据、因果、严重度和范围；无效或范围外候选直接丢弃，不形成用户义务。
2. 每个 Ask 必须区分“问题 disposition”和“修法 authorization”。若 finding 已附唯一、完整、无用户选择空间的修法，confirm 同时绑定 finding digest 与 remedy digest；若用户只确认问题成立，或 remedy 仍有多个有意义的产品/重大技术选项，则进入需求/方案澄清，取得选择并按 §7.2.1 确认完整候选的 review-delta 后才能写入。纯机械措辞、引用或无歧义同步不制造重复 Ask。
3. P0/P1 confirm 且修法已获授权 → `REVIEW_FINDING_FIX` → 修订 → 对应阶段完整新鲜复审。dismiss 或经 correction 判为无效 → finding 作废；若没有其他未处置或已确认 P0/P1，`EffectiveReviewStatus` 直接投影为 PASS，不为驳回项另派一轮。纠正若把 PASS 变为阻塞，则按依赖图使后续结果 stale；只清除错误阻塞时立即开放下一合法节点。
4. P2/P3 confirm 且修法已获授权 → 有界修订/修复且不因级别本身复审；dismiss → 作废。已拍板 stable finding ID 在后续提示词中注入，前提未变化时不得原样重提。
5. Controller 按 master/整体 run 和 action kind 分别持久化 `product-review`、`start-readiness` 两条 `PreDevelopmentReviewSeries`。每条默认允许三个 completed semantic rounds；只有 Operator 接纳的有效完整审查计一轮，finding 后续 confirm/dismiss/严重度纠正或 effective PASS/FAIL 变化不新增、删除或重复该轮，只有整轮被 correction 判定为 invalid/incomplete/stale/错误绑定时才从 completed 投影排除。RUNTIME_ERROR、invalid/incomplete/interrupted/stale result 不计。前三轮是 workflow policy 预授权的 fresh-review 上限，只在 finding 处置确需复审时按当前 revision/round 生成 distinct `TaskKey`，不预先派满。需求/方案 revision、ImpactSet invalidation、carry、adopt-external 和普通失效不重置 series；显式 reset 或新 run 才重建。达到上限后 typed Ask 每次只向对应 action 的 expected set 增加一个带 request ID/reason/revision 的 fresh-review slot；correction 不生成 TaskKey、不消费额外 review grant 或 retry attempt。split child 继承 master 整体审查，不另建 series；`show/status/next` 分动作公开 completed/base/extra/effective/remaining、raw/adjudication/correction/effective 状态与授权来源。

## 8. 拆分、并行、开发后审查与修复

### 8.1 拆分与 child 生命周期

1. `start` 不声明、不冻结拆分意向；拆分绑定唯一发生在 start-readiness PASS 后的拓扑确认：高置信要拆时 Ask 精确拓扑（边界、数量、依赖、并行度），低置信不拆或不确定由 engine 自动记录 no-split 理由。拓扑确认即绑定点——确认前用户可改变意向（无需需求变化、reset 或 abort），确认后不得重切，变更走用户需求变化（ImpactSet 级联）、reset/rebuild 或 abort。
2. master 确认拓扑后，Controller 固定执行：ENGINE_LOCAL 持久化 child-creation intent、分配 ID、创建 PREPARING child state 并注入 bindings → 每个 child 的 VCS adapter 独立 StepSpec 创建/对账 workspace/client → ENGINE 校验全部 typed receipts/identity → 原子提交 `sliceID → childRunID` map 并解锁 child。不得让本地 handler 越过 adapter 创建 VCS workspace，也不得把顺序交给宿主猜。
3. child 继承整体产品/技术审与精确拓扑，各自持久化已确认的具体配置与门集合；child 只开发并执行所选普通门，不选择、不执行普通黑盒/白盒 QA。child 达标后进入可恢复的 `SLICE_READY` checkpoint，不作为独立最终 SEALED，也不提前删除恢复信息。
4. durable `SliceTerminalReceipt` 至少含状态、child/slice/master ID、版本、VCS snapshot/tree/digest、需求/拓扑/路线 bindings、普通门结果、成本和最后 request/event digest；`SelectedQA` 与 `ApprovedCases` 为空，明确未选择普通 QA。master 只按期望 map 接纳 receipt。
5. 分片 QA 集中在 master 合并后执行；merge QA 的批准用例覆盖完整已确认需求与方案中适用的 QA 义务，其设计和审查与各 slice 开发并行。master 收齐 receipts 后由 engine VCS adapter 按固定 topology 顺序完成 Git/SVN/P4 集成，并在主线 snapshot 前物化已批准的 merge cases/其他 VCS 内最终 artifact；只有实际内容冲突需要语义判断时才派主线开发 worker，且它只编辑冲突内容，后续 resolved 校验、resolve/add、提交和 snapshot 仍由 adapter 执行。
6. 合并后并行执行所选合并 QA 与质量门审查职责。冲突处置或主线返修后，Controller 以 topology ownership、VCS diff 和每个 child receipt 的 validation view 机械计算 `AffectedChildSet`：若只改 integration-owned glue/跨片接缝且所有 child view 精确相等，只重跑启用的合并 QA/合并门；若改变某个 child-owned 内容或无法证明其 view 未变，则在同一最终主线候选上额外补跑该 child 受影响的普通门义务。仍不回 child、不重新 Seal child、不重复路线或澄清；所有当前 eligible 验证最大并行。Operator 核实完成后处置仍不唯一时才生成 `VALIDATION_DETAIL_DISPOSITION` Ask；处置完成后，`wave >= 3` 才生成三轮统一 Ask。
7. child abort 产生 ABORTED receipt 并阻塞 master；master abort/reset/重大需求变化按已授权策略级联，不得遗留 orphan child。master 最终 Seal 绑定集中 QA 的批准用例和执行结果，汇总 child receipts/cost 并清理 checkpoint；整体需求保证以完整 receipt map 和当前主线验证证据为准。

### 8.2 强制并行不变量

1. `expectedTasks` 计算完整 eligible frontier；canonical Plan 必须包含所有依赖已满足的任务。
2. 若可用容量为 C、eligible task 为 N，`SelectIssued` 必须按固定顺序签发恰好 `min(C,N)` 个任务；不得留空容量、选择性忽略或把并行组退化为主代理决定的串行。
3. 程序必须消费整个 IssuedSet，主代理不能选择子集。任一 receipt/result 释放容量后，`submit` 立即推进并补满下一批，无需主代理另想起调用。
4. 一个并行支路 FAIL/RUNTIME_ERROR 不得停止仍独立 eligible 的其他支路；只有依赖或 snapshot 已失效时才能阻断。
5. adapter 若因无可靠 correlation 必须串行化启动握手，只能限制未 attach 的 start；已 ACK worker 继续并行，engine 仍须填充其他兼容容量并给出可观察 WaitReason。
6. 普通非分片开发期并行组含开发 worker 与黑盒 QA 设计/审查；分片开发期为各 child 开发与 master merge QA 设计/审查。普通非分片 run 选择白盒时，开发快照 Dn（S1）只解锁白盒设计；提交测试形成 Qn（S2），QA Review 和 P2/P3 吸收引起的测试变化继续提交。全部 mode 最终 case-set 为 FROZEN、版本绑定最终含测试快照后才 review-ready，再一次冻结 expected-set/digest 并准备全部 eligible QA execution 与门并行派发。
7. 最大并行仅适用于依赖已满足的任务；review-ready 屏障之前不签发 QA execution 或门，不以 provisional 结果替代最终快照验证。修复与 merge 遵循同一规则。

### 8.3 白盒隔离候选、统一验证与提升

普通黑盒/白盒 QA 的候选与审查义务适用于非分片 run；child 只冻结完整开发候选与适用普通门结果，分片集中 QA 遵循 §8.1。

1. 每次开发或生产代码返修先冻结只含当轮实现的不可变 `DevelopmentSnapshot Dn`。选择白盒时，Controller 令 VCS adapter 从精确 `Dn` 创建并登记独立的 whitebox authoring workspace（Git worktree/临时分支、SVN working copy/候选路径、P4 client/workspace/候选分支）；白盒设计代理可新增、修改或删除已由 `TestOwnershipResolver` 证明为 test-owned 的结构测试、测试辅助文件、fixture 和对应 case 内容，以及声明测试用途但程序归属 UNKNOWN、仅作为隔离独立 review 输入的测试材料，返回 manifest；已知生产文件不得编辑，白盒设计代理不执行任何 VCS 写操作。
2. `TestOwnershipResolver` 由项目/测试框架配置、目录/命名规则、测试发现结果及生产构建输入共同程序化判定；文件名包含 `test` 既非必要也非充分条件。源文件、路径身份、版本、manifest 与最终证书由程序处理，UNKNOWN 文件的语义归属由独立白盒 review 判断，FROZEN/PASS 结论绑定精确 candidate、paths、manifest 与 history。VCS adapter 提供 `Dn → Qn` 每个 added/modified/deleted/renamed 路径及内容摘要。只有全部差异均为 test-owned（UNKNOWN 须持该独立 review 结论），且生产代码、运行配置、生产依赖、构建输入和 provider 元数据投影完全相等，才签发 test-only/equal-production-view 证明；程序核验完整差异归属批准 artifacts 且每个路径 Entry 精确相等后才采纳，已知生产文件不可覆盖，未获该证明的 UNKNOWN 仍按生产 view 受影响处理。
3. Controller 校验 manifest、测试引用、改动范围和 postcondition 后，由 adapter 程序化 track/add/open、commit/submit 并冻结包含 `Dn + 白盒测试 + 所有需纳入 VCS 的已批准最终 artifact` 的原生不可变 `ValidationCandidate Qn`。未选择白盒时 `Qn = Dn + 需物化的最终 artifact`。所有交付路径必须在 `Qn` 中，不能留在主工作树或临时 workspace 里等待 Seal 临时捞取。
4. 白盒 qa-review 读取完整 `Qn`，独占测试代码质量、用例与实现引用的审查责任，并独立判断 UNKNOWN 文件的语义归属；实现质量门不审 test-owned 文件。FAIL 时回白盒设计形成新的 `Qn+1`，不计开发后 wave。改变已知生产文件，或 UNKNOWN 文件未获该 review 证明为 test-owned 时，相应改动必须转交开发/repair 路径形成新 `Dn`，不能伪装成白盒测试增量。
5. 黑盒与门只在最终 review-ready 快照签发；门比较基线到最终候选的完整 diff，不接收 QA 用例正文、逐用例证据或测试实现细节。实现质量门排除 test-owned 文件，测试质量归白盒 QA Review。
6. 同一 wave 的 expected-set/digest 在最终 Qn 冻结，包含全部所选 QA execution 与门；未收齐、存在 PENDING/在途或运行错误未明确处置时为 COLLECTING，不得返修。route/case-set/目标集变化使旧 wave 失效，不能中途修改集合制造收齐。晚到结果仅留审计，不重开 wave；显式错误处置不伪装成 PASS。
7. 普通实现返修保持 QA FROZEN，只装配新实现候选并按固定 AFFECTED 子集执行。只有需求含义、对外行为、成功/失败判据或稳定可执行性变化才允许 THAW；环境暂不可用记 RUNTIME_ERROR。合法 QA artifact 修订使旧 Qn 失效，由独立设计/审查链提交测试与建议吸收变化，全部冻结后形成新 review-ready Qn+1，至少重跑修改测试与共享依赖影响项。child 的 SLICE_READY receipt 绑定最终完整候选，master 不从遗留 workspace 补捞。
8. 完整 wave 无 obligation 后，adapter 才把整个 `Qn` 提升到主线，不得在末尾重新播放一份未经验证的测试补丁。优先保持同一原生 identity；若 Git/SVN/P4 产生新的最终 identity `F`，只有 `CandidatePromotionReceipt` 机械证明 `validatedDigest(Qn) == finalDigest(F)`（含 provider 特有的 mode/property/filetype/view 语义）时，整组证据才可映射到 `F`。promotion 出现内容冲突、转换、目标漂移、UNKNOWN 或任何 digest 不等价时不得 Seal；先 observe/reconcile，必要时把实际 `F` 冻结为新候选并按真实影响重跑验证。

### 8.4 发现项、QA scope 与三轮

1. 普通非分片的黑盒、白盒与 master 的 merge QA Review 共用 PreWaveReviewPolicy 与逐项 approved whitelist；P1 修改后 fresh review，集合级 P2/P3 以稳定 suggestion ID 吸收并留来源、不复审。全部建议吸收且确定性覆盖通过才 FROZEN；无建议则 PASS 后立即冻结。首次 FULL，合法增量 AFFECTED；普通代码返修不重新设计。既有 pre-wave 语义轮数上限保留，与运行错误 attempt、开发后 wave 分开计数。
实际选择 QA 模式时，QA 覆盖先把同一次需求/方案确认中登记的有限 `requiredSources` 与当前 revision、route/topology scope 一起冻结，再生成有限验收点集合 `AcceptanceManifest`，并用 source↔point↔case 映射建立双向追踪。标准需求案适配器把每个 `REQ` 投影为 `RequiredSource(PRODUCT_REQUIREMENT, QA)`、每个 `AC` 投影为 `AcceptancePoint`、已设计 QA case 投影为 `AcceptanceCase`，关系只由 `CoverageEdge` 表达；标准需求案中的全部 `AC` 都必须有 approved case，不允许以“非 QA 验证处置”绕过，其他合法 source 才可按自身协议使用显式非 QA 处置。review 必须显式返回完整 `SourceDecisions`、`PointDecisions`、`CaseDecisions` 和未绑定集合，不得由 case PASS 自动推导或补写 source/point PASS；三层全部逐项 PASS 后才能生成 whitelist，仅有集合级 PASS 不足以通过。execution 冻结 expected case ID set，并列出实际执行、合法继承和未执行条目；`FULL`/`AFFECTED` 都必须完整对账。source inventory/manifest/map/whitelist digest 与当前 `ValidationCandidate` 精确绑定，摘要展开为 `sourceID → pointID → caseID → result`，`AUTHORIZED_SKIP` 不等同 `EXECUTED_PASS`。Controller/validator 负责结构校验；覆盖率、mutation、property-based test 和 fuzz 只作辅助信号，不设固定阈值。
2. 开发后 finding 由 Operator semantic adjudication 按需求、正常失败场景、模块、根因、后果逐条核实，保存 raw finding、独立 severity rationale、范围/严重度/因果/复现证据、稳定 finding/problem ID、NEW/SAME_AS/DISMISSED 裁决及历史。Controller 只校验完整性、引用和版本绑定、机械路由，不自动判断语义真假、严重度、根因或重复关系。P1 理由须说明已确认需求、正常路径、实际后果及为何 P2 不足；P0 还须证明需求无法成立、核心验收无可用路径或交付受阻。条目 FAIL 不自动成为 P0/P1，整批套话不能替代逐条理由。
3. 仅已核实 finding 的处置仍不唯一才 Ask；核实本身属于 Operator。pending disposition 不形成返修 obligation。QA artifact 修订须满足 THAW 条件并重新冻结候选；需求变化遵循 §7.2.1 授权，DISMISSED 只留历史。
4. wave 收齐且 adjudication 完成后，QA FAIL、有效 P0/P1 与门审 P2 归入同一快照唯一 RepairObligation/统一返修计划。同根因归并 canonical problem，同一快照不得第二次逻辑返修。门审 P2 必修但不复审、不增加审查轮次，纯 P2 以完整构建、全量测试、受影响项逐一自跑及结构化完成/carry 凭据继承 gate PASS；门审 P3 不返修，只在 Seal 前一次性 advisory，范围外 P3 同样不进入 obligation。
5. 首次完整 review-ready expected-set 终结计 wave 1，后续需复审的修复快照各计一轮。装配/pre-wave review、PENDING、未处置错误、缺回执、未完成裁决、需求变化打断的 wave 不计；纯门审 P2 修复不计轮，既有三轮及额外授权上限保留。
6. 普通返修固定 AFFECTED 执行冻结用例；有效原 FULL 与当前实际执行、合法继承须覆盖全部当前批准义务，本轮必需验证全部有效通过后完成，不因 requirement guarantee 追加最终 FULL。首次必要 FULL、新增/受影响或重开义务、来源绑定及显式例外权限保持。需要重审的产品/技术/普通门读取完整绑定输入；门审 P2 用 carry，不复审。
7. 额度用尽后额外返修 Ask 展示完整绑定摘要：state revision、需求/快照/wave、全部 findings/problems（归并与定级理由）、历史修复/验证、QA scope、已用/剩余轮数、dispatch signature/TaskKey、engine allowance、owner cost、token cap 可用性。预览只读；授权绑定摘要 digest 与 state revision，变化后拒绝旧授权，每次只授权一轮。
8. 非 wave 与 wave 的 action/QA/gate 共用无效结果义务生命周期：RUNTIME_ERROR/REJECTED_INVALID_RESULT 终结原 attempt，后者保存被拒字段/原因，不写有效 finding/PASS/FAIL/problem/basis。义务有效时复用 TaskKey 按有限 MaxAttempts 重派，默认 2；wave 还须 COLLECTING 且目标未处置。耗尽进入 RETRY_EXHAUSTED，展示原因、已用/总 attempts，停止普通与自动重派。用户绑定 state revision 和非空理由只能追加一次单 attempt，改变 retry policy/digest、不增加基础槽位；再次失败不得扩张，须按已有错误处置、abort/cleanup 或确认真实改变绑定/快照/责任的新计划结束或重建。
9. dispatch signature 绑定具体输入，TaskKey 绑定逻辑义务；相同 OPEN 派发续用，相同有效 PASS signature 不重复派发。普通 finding/result/receipt/resume 不新增 TaskKey。各流程复用同一 finding/problem ID、归并、唯一返修、P3 advisory 和 QA 冻结语义。

### 8.5 开发分批与组间验证

1. **批次计划义务**：受理阶段只确认需求与方案，不冻结批次计划。Part 2 `start-readiness` 技术审一并输出结构化 `granularity_review`，先判断单一 Batch 还是多个 Batch，再登记 `Batch → Subtask` 映射、依赖/接口/状态边界、风险、DoD/测试、回滚/恢复、交接与上下文成本及不拆/改拆理由。显式批次计划在拆分决定与路线确认时一并产生，经同一次用户决定确认并留痕，于开发开始前登记为任务清单的组织结构；允许"单批 + 理由"退化形态。批次划分依据依赖链、独立交付物/验收和风险：依赖链（消费链）必须串行；零耦合支线可并行；共享文件、接口或语义的工作面不并行。接口冻结点优先作为批边界。编号、commit 数、文件数、行数、阶段/层/角色/时间段本身不得作为大块依据。
2. **批粒度**：批是任务单元，单批一个内聚关注点——语义判据为"能用一句话说清本批 diff 即一批"；批内提交保持原子、可独立构建。批内每次编辑后执行项目声明的低成本确定性检查，批边界执行该开发计划指定的回归命令；审查代理只选择已有调用或提出验证目标，缺少调用由开发代理准备，程序按原批义务绑定并实际执行；完整构建、全量测试及声明检查通过后才完成批次；每批终态可判——树可构建、声明的回归通过、自测留痕、任务清单逐批勾选。
3. **探路优先**：含不确定性边界的探路工作必须作为首批 spike：结论留痕，代码不进入交付。
4. **验证档位**（随批次计划一并选定并留痕，依据与已拒绝替代见 ADR-002）：批内自测为固定底座（永远启用）；默认为开发完成后单次全量门禁；确有需要时增设组间快照 + 增量审查 checkpoint（冻结中间 `DevelopmentSnapshot`，复用 `ReviewScopeMode` 与 carry 继承判定：未受影响 PASS 直接沿用，只对增量 diff 审查/执行；组间轮不计 repair wave）。档位依据结构性信号：预期生产逻辑是否多模块扩散、独立验收单元数；不依据总行数或批数。split 属 run 拆分机制（见 8.1），不是验证档位，不在此选择。
5. **不变量**：批次不改变 run 边界、公共入口、Seal 语义或强制并行不变量；不产生额外受理、双审或 Seal 轮次。
6. **引擎义务与机械接入**：用户确认 `granularity_review` 后，Controller 将其绑定到需求/方案 revision、拆分决定和路线，并把 Batch、Subtask、TaskKey 映射及批间依赖登记为现有任务计划的结构化信息。引擎只校验 ID 唯一、映射无遗漏/重复、依赖无环、TaskKey 归属明确，以及 DoD/验证/恢复/交接字段齐全；它不从自然语言重新判断是否应该拆分。`batch_id` 对应一次 `development-worker` 派发；`subtask_id` 只是该派发内的顺序步骤，不得单独派发或换代理；只有前一 Batch 的成员任务终态、DoD、验证和交接完成后，才解锁下一 Batch。Batch 契约、接口、依赖或 DoD 改变时，旧计划失效，必须重新进行 `granularity_review` 并取得用户确认。Batch 不是独立状态机、receipt 或生命周期，完成状态仍从成员 task 状态派生。
程序从已确认需求投影共享有序需求点，携完整父要求、验收及来源，供开发、普通 Reviewer 与 QA 使用；计划逐点关联负责批次，开发完成逐点交本批职责或贡献的实际依据，普通门按当前完整职责及本门维度逐点独立核实，失败关联原 finding。编号、原文、revision、身份绑定和机械编排由程序持有。未选择 QA 也必须覆盖开发与普通审阅职责，但不能声称 QA 实测；逐点核齐只证明各点均有结果，不替代代码质量、正确性、可读性、简洁性、解耦、可维护性、冗余、过度设计/防御/抽象与硬编码审查。返修只承担其已确认受影响义务，需求变化与恢复复用同一投影、接纳及既有生命周期。
7. **批次代理隔离**（宿主纪律，2026-08-23 用户补充）：每个开发批次派发全新的零上下文开发代理（薄启动 + 规范文件），同一开发代理会话不跨批复用；reset 粒度为批次边界（第 2 条的内聚关注点：批内连续、批间不连续）。批次任务书只写不可从仓库恢复的信息（决策理由、已知问题/债务、下一任务），不复制可从 VCS/代码/测试推导的内容。该纪律由包级 SKILL 与流程参考文档持有并对外生效，引擎不新增状态或语义分类器。

## 9. 程序化 VCS、本地副作用与恢复

1. Git/SVN/P4 的 provider 探测、根与身份解析、status/diff/untracked 识别、base/current/ancestry、workspace/client 创建、显式 track/add/open/reopen、resolve 状态验证、merge/integrate、commit/submit、snapshot、Git squash、已授权 reset/rollback、cleanup 和崩溃对账全部由 VCS adapter 程序化执行；宿主和代理不得自行拼 VCS 命令或决定其顺序。
2. 开发/修复代理只编辑已授权内容并返回声明式路径 manifest、语义说明和 typed result；Controller 校验路径、改动范围与 postcondition 后，才由 adapter 执行 track/add/commit/snapshot。确定性构建/测试可由 engine runner 执行；需要语义设计或实现的测试仍可作为 AGENT 步骤。
3. 集成固定为：adapter 发起并观察精确冲突 → 仅在需要语义选择时让主线开发代理修改冲突内容 → adapter 验证全部冲突已解决 → adapter 执行 resolve/add/open、commit/submit、snapshot 与确定性验证。VCS 异常只能按 retry、Wait、Operator 或显式失败处理，不得降级为代理猜测。
4. reviewer/QA 接收 Controller 生成并绑定 provider、base/current、definition version 和 digest 的只读 snapshot/diff artifact；不得要求 reviewer 自行推断当前 VCS 状态或执行 VCS 写操作。凭据或宿主能力边界可产生 typed HostAction，但宿主不能重排或自造命令。
5. 每类非幂等本地副作用采用 `intent → execute → observe/reconcile → commit`：start、prompt/artifact、blackbox/whitebox QA workspace、case mirror/materialize、ValidationCandidate freeze/promotion、Git/SVN/P4 child workspace 与集成、snapshot、Git squash、child receipt/cost、summary、cleanup、abort。
6. 外部事实仍是旧状态时可执行；已是精确预期状态时提交结果；冲突进入 Operator；retryable I/O 进入 Wait。绝不盲目重复 squash、spawn、submit 或集成。
7. 黑盒批准用例、白盒测试代码及其他 VCS 内最终交付物必须在 `ValidationCandidate` 冻结前物化并纳入其 identity；若明确设计为 VCS 外产物，必须有独立 digest 和 summary 绑定。Seal 不得在共同验证完成后再向候选追加交付文件。
8. Git 基线→当前超过一条提交时 Seal 自动 squash，消息由 engine 稳定派生，不新增用户 Ask；SVN/P4 不 squash。
9. Controller 持久化 typed resource registry，登记每个 blackbox/whitebox/child/merge workspace、临时 ref/branch、SVN working copy/候选路径、P4 client/pending changelist 及其 owner/identity/cleanup state。Seal/Abort 先持久化可恢复 intent/summary，再由 adapter 清理并逐项 observe；只有 `WorkspaceCleanupReceipt` 证明全部应回收资源无登记残留后才进入 Complete。promotion UNKNOWN 必须先对账，不能靠重复 merge 或静默 rollback 清理。
10. 检测到活动 run 的外部漂移时，adapter 先冻结精确 observed external identity、lineage、只读 diff 与 `ExternalAdoptionPreview`，在用户对该 identity 授权前不得把它写成 current。adopt-external 获批后保留原 run base，把该不可变外部 identity 设为新的 current/development source，并写 `ExternalAdoptionReceipt`（旧/新 identity、preview/user request/ImpactSet digest、失效与 carry 清单）。受影响或 UNKNOWN 的 Attempt/result/candidate/waiver/promotion intent/child receipt-map stale，明确未受影响项才 carry；未完成 wave 不计、已完成 run 级 wave 不清零。若外部变化改变需求 artifact 的含义，必须转入 `REQUEST_REQUIREMENT_CHANGE` 完整确认；lineage/identity 不唯一时进入 Operator/Wait/reset/abort，不能猜。分片只级联受影响 child 与 merge bindings。

## 10. 测试与验收覆盖（口径 A，从严）

以下每一项都要求可复现测试/QA 证据，不能用 smoke 或口头保证代替：

QA 覆盖契约的独立 fixtures 还必须证明：合法 source→point→case 多对多映射；标准需求案 `REQ/AC/case` 到既有 `RequiredSource/AcceptancePoint/AcceptanceCase/CoverageEdge` 的无损适配；标准需求案任一 `AC` 缺 approved case 或试图以非 QA 处置绕过时拒绝；其他 source 无适用 QA 点时的显式理由；未知/重复/缺失/orphan source/point/case、仅 case 决策、自动推导 source/point 决策和集合级 PASS 被拒绝；source inventory/manifest/map/whitelist digest 或当前 `ValidationCandidate` 不匹配时旧结果失效；`FULL` expected/actual 不一致与 `AFFECTED` 执行/继承/未执行集合不完整被拒绝；以及 `AUTHORIZED_SKIP` 不计为执行 PASS。覆盖契约、validator 和正常 regular 执行路径共同取证。

1. **迁移表与用户节点**：每条合法边、pre 拒绝、trigger 分类、Ask/Operator/Wait/HostAction/Ready/Complete、主动 `REQUEST_*`、终态与回退；正常入口不存在遗漏用户节点。
2. **节点内执行计划**：封闭变体 constructor 非法状态拒绝、closed-world compiler 图校验（可达性/循环/依赖/join 覆盖/版本绑定/registry ID 完备性）、合法/非法 reason、typed I/O、可执行 pre/postcondition、固定顺序、乱序/遗漏/重复拒绝、精确 frontier、崩溃后不重放已完成前缀，以及纯原子 handler 不被过度拆分。
3. **失败分类与禁止降级**：每类 engine/business/user/UNKNOWN/invariant/semantic error 只进入声明边；engine/adapter 故障绝不动态改派 AGENT；diagnostic-only `MISSING_ENGINE_ADAPTER` 不可编译/签发并稳定进入 `BLOCKED_BUG` diagnose；最终定义扫描证明本次范围内没有该 marker。
4. **并行**：eligible N、capacity C 时恰好签发 min(N,C)，程序整批消费与及时 refill；白盒最终 FROZEN case-set/review-ready 屏障之后全部 QA execution/门并行，COLLECTING 时不得返修。
5. **任务与派发**：expected set 漏派/重复、TaskKey、Attempt、旧 Attempt、result-before-receipt、UNKNOWN/lifecycle 对账、同 actionID 不重复 spawn、重复 submit 不重放 SpawnRequest；3.5b guard 对相同 definition/state/route/topology/expected-task basis 重复计算得到相同额度/digest，外部派发无显式策略时按 `MaxAttempts=2`，显式 `MaxAttempts=n` 只允许 `n-1` 次额外 retry；0/1/N 个 external TaskKey 与 `MaxAttempts=1/2/n` 的已知答案 fixtures 直接调用生产 calculation 核对精确结果，增加一个合法 distinct TaskKey 或一个有限 retry 余量只各增加一个槽位，重复 TaskKey 不增加。basis、持久结果或 action/Attempt 账目不一致时以 `BLOCKED_BUG` 零派发。没有 closed-world、已确认计划或当前 workflow policy 预授权 obligation 来源的 TaskKey 被拒绝；按现有流程要求用户确认却未绑定对应 revision/typed Ask 的 task-set 扩张被拒绝，已预授权 repair slot 不重复询问用户。预算 Ask 的继续只解锁一个按既有 admission capacity 裁剪的 canonical Ready batch，重试只增加当前逻辑动作的一个 retry slot，重复 submit 不重复增加，host/主代理/用户提交底层额度字段被拒绝。
6. **需求变化**：`USER_INITIATED_CHANGE` 在每个非终态 phase 可达；Phase A 全局权威写屏障与 quarantine、确认后 ImpactSet、unknown=affected、Phase B 精确恢复/carry/refill、split cascade、pre-Seal 取消；与 `REVIEW_FINDING_FIX` 完整新鲜复审不可混淆。
7. **开发前发现项**：P0–P3 验证、逐 ID settle、finding/remedy 双绑定、raw candidate/adjudication/correction/effective status 分层、错误 finding/严重度/裁决的 typed 纠正、原始证据保留与 supersede、全部 blocker 驳回后无空审查直接 PASS、纠正的 CAS/freshness/幂等与下游精确 stale、多个用户选项时重新确认完整文本、P0/P1 完整新鲜复审、P2/P3 修复不因级别复审、dismiss、混合严重度和前提变化；产品/技术两个 `PreDevelopmentReviewSeries` 各自前三轮自动、只有有效完整审查计数、finding 后续裁决不重复/回退计轮且整轮 invalid 才排除、revision/invalidation 不清零、上限后每次只授权一个 action-specific fresh-review TaskKey、correction 不生成 TaskKey 或消耗授权、split child 不另开 series，并与同 TaskKey 的 `MaxAttempts` retry 分离。
8. **开发后三轮**：验证 §8.3–8.4 的最终候选、逐条 Operator adjudication/P0/P1 rationale、稳定 finding/problem ID 与历史、wave 收集/唯一返修、门审 P2 carry 与 P3 advisory、QA Review P2/P3 吸收/FROZEN/THAW、普通返修 AFFECTED/全部当前必需义务有效通过后完成、完整授权摘要，以及非 wave/wave 无效结果、RETRY_EXHAUSTED 和一次性追加 attempt。
9. **分片**：拓扑绑定唯一时点（start 无拆分意向声明；start-readiness 后确认、确认前可改、确认后不重切）、topology、child creation/map、继承、逐 child 路线、child 最终完整候选 identity、普通门路线与空的 QA 字段、`SLICE_READY`、durable receipts/cost、master 等待、adapter 集成与语义冲突窄代理边界、集中 merge QA 的完整需求覆盖与设计/审查并行、主线 repair 的 AffectedChildSet 与普通门精确补验、级联终态。
10. **错误与恢复**：invalid event、retryable I/O、fatal corruption、必要单次操作限时后的恢复、provider/bridge/mismatch、HostAction、adopt-external 完整转换、候选 freeze/reuse/promotion/cleanup 与所有本地副作用崩溃窗口；promotion 后 receipt 前恢复不得重复集成。
11. **旧 run 与旧入口**：缺版本和任一版本不匹配稳定 `UNSUPPORTED_RUN_VERSION`；`diagnose` 原始只读；无迁移、legacy 续跑、handoff、旧公开命令、兼容别名或 cleanup 删除后门。
12. **VCS 程序化契约与 3×2**：三种 provider 的探测/status/diff/track/integrate/commit/snapshot、whitebox workspace、完整候选 freeze、identity-preserving 或 digest-equivalent promotion、resource registry cleanup、冲突窄代理边界、typed artifact、UNKNOWN reconcile 和禁止代理写 VCS；Git、SVN、P4 各跑通非分片与分片 master/child/merge，共六个权威单元格。
13. **宿主资格**：DSH、Claude Code、Codex、Cursor、ZCode 的受管原生 adapter 与实际宿主能力分别取证；正常正式流程只使用本 run 冻结默认或已确认执行组选择绑定的平台。额度或能力不可用的宿主如实 skip/unverified，不发模型请求、不计为 PASS，不把安装成功视为真实模型或工具前拦截证明。
14. **执行证据归属**：源码、输入、candidate、provider/VCS 和原始 receipt/source 必须明确对应。验证仅因受影响行为、具体失败或未决风险扩大；仍有效的原证据沿原归属复用，不因 revision 变化重复全部完整流程，不用不同能力或输入的记录冒充当前结果。
15. **黑盒边界**：文档化正常入口、常见误操作和已知恢复/崩溃窗口；对抗输入、恶意/手工状态编辑、权限/不可变文件和未支持 OS 只作 P3、不阻塞。
16. **canonical 制品与身份**：compiler 同一生成动作产出 `definitions/workflow.json` 与期望身份常量，禁止人工双写；authoring source 重新生成 checked-in 制品字节无 diff（独立于 round-trip 的 freshness CI）、任意 assembly 顺序同字节、decode→encode 字节不变、跨进程/重复构建同字节；只改 handler 实现不改 ID 与定义语义时 definition digest 不变而 package digest 变，改变 dependency/policy/reason/schema ID/handler ID/join 语义时 definition digest 必变；registry 对每个 ID 精确解析一次，缺失、重复或 kind 不匹配拒绝；packageDigest 执行绑定（含 runtime sibling 实际摘要校验与 adopt receipt 路径）有正反向测试。

## 11. 验收判定

1. 方案稿全部确定性闸门、节点内执行计划顺序/失败分类、强制并行不变量和用户节点覆盖测试通过。
2. VCS、宿主与独立 QA 的适用义务由实际来源完整对账；执行项保留事件、状态、snapshot 和 receipt 证据，未执行及额度不可用项如实标记，不冒称 PASS。
3. QA FAIL、有效 P0/P1 与门审 P2 obligation 按规则闭环；门审 P3 仅 Seal advisory，QA Review P2/P3 在 FROZEN 前吸收，范围外项不阻塞。
4. 最终候选不存在旧 run 兼容、迁移、legacy mode、旧公开推进入口、第二写入口、cleanup 后门或 engine→legacy 回退。
5. 正常流程中不存在本可程序化却交给代理的操作，也不存在 engine/VCS adapter 故障后动态降级给代理的路径。
6. 安装只使用已授权的当前候选与原事务 owner；实际 post-switch/pre-commit smoke、receipt 与恢复资格成立后提交安装，失败沿同一 journal 恢复。
7. 删除或改写仍断言已废弃受理/复审/轻量路线文本的旧测试；最终 `go test -timeout=0 ./...` 必须在不恢复旧逻辑、旧文案或兼容入口的前提下 PASS。不得为了通过测试回填过时行为。

## 12. 非目标

2. headless adapter、随机 chaos、大规模 benchmark。
3. 事件溯源、SQLite、watchdog、OS 定时器、repo 级集成队列/分片合流 CAS。
4. 本次承诺 Windows/macOS/Linux 适配矩阵，或把不支持平台升级成阻塞项。
5. 对抗性输入、恶意或手工状态编辑的绝对防御。
6. 旧 run 兼容、状态迁移、旧命令续跑或双权威回退。

## 13. 用户已确认的决策记录

- 用户主动提出 formal-gates 后不重复询问是否进入；插件只非阻塞提醒一次或复杂需求时两次。
- 完整需求/方案仍须单独确认；lightweight 是 start bootstrap 例外，具体开发计划和门集合在拆分决定后确认。
- 所有用户选择能力保留为 typed Ask/available action，经唯一 `submit` 入口落账。
- 用户可自由组合产品审、开发前技术审、初始开发、QA、质量门审查和业务返修六项主要职责，保持所选职责原有相对顺序与并行关系。QA 按设计、独立审查、执行整条链运行；黑盒、白盒各按整组选取，运行模式由原 SelectedGates 持有，关闭 QA 职责也关闭 merge QA。启动及运行中配置入口从同源定义目录展示并绑定明确用户选择，state 只保存当前这一份主流程选择，恢复不重读默认文件；关闭职责不暗跑、不伪造 PASS。初始开发与业务返修分别选择，关闭返修仍保留真实失败与义务。质量门审查独立控制普通门、固定合并门与返修成员门，关闭时不建立门目标、不补写门 PASS；child 继承创建时选择，receipt 保留真实门集合和原结果，主线仅在门职责启用时准备 child 补门目标。
- 运行中的临时执行选择从主流程目录派生，黑盒 QA 与白盒 QA 各自可选平台、模型和推理强度，同组设计、独立审查与执行共用该组配置；其余主要职责分别选择，辅助任务及合并 QA 不新增公开选择。流程开关与顺序不变。选择沿原 Decisions 持久化，只更新该组未 claim 的调用；已 claim 调用及其恢复保留原 Attempt 绑定。child 继承创建时快照，既存 child 不随父后续选择改变；已保存的共享 QA 选择及其请求重放保持原语义。
- 产品/技术审 P0/P1 修订做完整新鲜复审；P2/P3 逐项询问并修复，但不因级别本身复审。
- 产品审和技术审各自最多自动完成三轮语义审查；只有有效 PASS/FAIL 计轮，revision 变化不清零。仍需第 4 轮时由 typed Ask 逐动作、逐轮授权；授权增加一个 fresh-review TaskKey，不增加该任务的 retry 次数，split child 不另开额度。
- 只有用户主动需求变化使用影响集增量复审，且可发生在任意非终态时刻；不计也不重置 run 级 wave。
- 开发后 Operator 核实完成且 wave 收齐后，QA FAIL、有效 P0/P1 和门审 P2 只形成唯一返修；门审 P2 carry 不复审，门审 P3 只作 Seal advisory。仅已核实 finding 的处置仍不唯一才 Ask；额外返修绑定完整摘要。
- 用户选择合法 THAW 后修 QA artifact 必须形成新候选；QA Review P2/P3 吸收不复审，其余适用修改 fresh review，改过的测试至少重跑自身，共享 helper/fixture/harness/config 变化时扩大到所有依赖测试。未受影响结果只有经精确 view receipt 才沿用，不能回旧候选 join；适用于普通非分片 QA 与 master merge QA。
- QA review/execution scope follows persisted typed `ReviewScopeMode`: first baseline FULL, then AFFECTED only for new, modified, or ImpactSet-affected items; product review, technical review, and ordinary quality gates remain FULL. 长期 RUNTIME_ERROR 可选择再试 QA。
- 并行由引擎最大化强制执行，不允许主代理选择性忽略可派任务。
- 白盒独立编写、审查并提交测试；最终 case-set FROZEN 且绑定最终含测试快照后，全部 QA execution 与门一次准备并行。普通返修不解冻 QA；实现质量门不审测试，测试质量归白盒 Review。
- Seal/Abort 必须由 adapter 清理全部登记的 Git/SVN/P4 临时 workspace/client/ref/changelist，核验无残留后才 Complete。
- 分片保留合并后集中 QA：child 只开发并执行所选普通门，不选择、不执行普通黑盒/白盒 QA；master 在 QA 与质量门审查职责各自启用时保留相应 merge QA 与 merge gate，启用的集中 QA 覆盖完整需求。
- 合并 QA/合并门失败只派主线开发 worker 修复；只改 integration-owned 内容时仍只重跑启用的合并验证，若改到 child-owned 内容则在最终主线候选补跑该 child 受影响的普通门义务，不回 child。
- Git/SVN/P4 的分片和非分片都支持并验收；OS 不作本次承诺，只保持实现通用性。
- 节点内部已知顺序也必须由 Controller 机械钉死；采用业界常见的 durable workflow/activity 边界，只在需要独立恢复、副作用、并行或审计时拆持久 StepSpec，不机械拆碎纯原子计算。
- 所有可程序化动作都由 ENGINE 决定；代理仅负责语义判断、创造性实现和独立审查，VCS 写操作与集成顺序全部由 Git/SVN/P4 adapter 执行，且引擎失败不得动态降级给代理。
- 旧 run/旧命令不兼容、不迁移、不续跑；只有 raw diagnose，失败后以当前版本新建。
- 当前稳定安装仅保障 `orch-engine-003` 开发完成；最终候选隔离验证并在 Seal/全部 canary PASS 后安装。
- 步骤模型采用 typed Go authoring（封闭变体 + constructor + 显式表）→ 小型 closed-world compiler → 单一 canonical encoder → compiler 生成 `definitions/workflow.json` 与身份常量的形态（ADR-001）；拒绝扁平万能 StepSpec、通用 DSL/schema 解释层、完全删除定义编译器与定义制品，以及为实现对照建两套架构。
- HandlerID 只在执行合同或恢复兼容边界变化时晋升，合同兼容的实现变化保持 ID、由 PackageDigest 标识；executable definition 与二进制 registry 锁步激活；packageDigest 是 owning-runtime 执行绑定而非审计附注。

## 使用能力与原生材料约束

- 活动用户补充消息归同一已签发 action/attempt 的原队列，保留原文与真实投递状态；
  不新增任务/轮次、不改变确认范围。Codex 使用原 thread/turn steer，其他宿主下一次
  合法同身份调用消费。缺 ack 留未知投递事实、不得自动重发；终态/替换也明确处置。
- 可解释进度由唯一 state 和原生 process/exit/source/cost/receipt 派生：经过时间、
  完成/失败/取消替代、真实运行/未启动/受阻、当前环节时长、revision 增量结果、
  准备复用、实际调用及重复理由。取消/退出 pending 不冒称完成；并行区间不相加，
  缺源 unavailable。准备复用只读取原批准 CheckEvidence，不新建缓存或计数账。
- 用户与项目设置统一手改入口、字段覆盖与有效来源；模型、effort、总/QA 并发、
  token cap 和原生路径在启动一次冻结到 state，child 继承，恢复不重读也不清预算。
  动态费用/授权仍归既有 cost/guard；适配器只消费本平台路径与参数，不能改全局环境。
- 命名配置由主流程节点选择、黑盒/白盒整组选择、普通门子集和七个执行组各自的
  平台/模型/推理强度构成，不打包并发、额度、认证或原生路径。初始只提供“默认”：
  主流程全部可用、不选普通 QA、全选普通门、tuple 同平台继承 run 全局默认，换平台
  未指定项留原生默认。其他名称由用户自取。保存、查看、修改、启用、删除和原生
  选项查询沿原 settings 入口；用户/项目 scope、同一 loader、同一路径锁和原子写
  继续持有配置，LLM 仅填写原序号与语义值，不生成 JSON 或机械身份。新 run 只冻结
  当时选中配置及来源，命名集合不进入 run；`start --configuration` 可按原名称或序号只选本次配置，
  不修改保存的启用项，省略则采用已启用配置，显式启动节点开关优先。运行中明确配置修改沿原 typed 控制与
  节点继续接纳，既有 Attempt、费用、恢复和 child 快照归属不变。主流程与执行组
  目录同源，普通门仍可选择任意子集；具体选择代替路线档位。
- 不变需求的优化/新增 Batch 使用显式用户主动计划重审：精确原 revision 保持，
  product/topology 保留，原完整确认与 carry/redo 仍生效；新 start-readiness 生成新
  计划，route/plan 确认后开发。普通重复提交不开放，不接受自拟自由任务定义。
- 同宿主主动开发交接沿原 recovery 的观察、terminate receipt 与 fresh 路径；原
  工作区、TaskKey、未完成工作和历史费用保留，不制造故障、不增加业务返修/retry。
  原 token guard 和 UNKNOWN 先对账继续约束新执行，不设第二个交接状态机。
- 所有角色的规则、动态输入、结果 schema 和工具说明只交当前职责必需内容；黑白
  专用规则不串角色，QA 比较不灌入已保留结果或无关用例。完整原需求与证据仍在原
  绑定材料可得，可由现有读操作按需读取；无新索引、强制读取闸门或第二材料系统。
- WorkBuddy 提供安装、可编程原生启动/续用、等待、采集、取消、身份观察恢复和
  原用量，沿共用生命周期/工具/费用核心，只在薄 adapter 保留平台差异。session store
  不存在先离线拒绝；不可由帮助或模拟推断付费现场通过。真实调用只用用户授权额度
  与原模型选择；所有执行不设时限；必要的单次操作限时可用。
