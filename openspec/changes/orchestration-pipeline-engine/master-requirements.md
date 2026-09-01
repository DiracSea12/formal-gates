# 编排流水线化（确定性引擎交权）· 主需求

> 状态：已由用户确认，进入 formal-gates 正式流程。
> 需求归属：formal-gates 自身重构。
> 方案文档：`refactor-plan/final-implementation-draft.md`（方案稿、SKILL 与参考文档冲突时以本文为准）。

## 1. 目标

1. 把 formal-gates 的编排决策从主代理收进确定性 Go 引擎；主代理只承担用户接口、受理、机械搬运、证据核实与决定呈现，不临场选择下一条 workflow 命令。
2. 两条目标等权：
   - 编排不再因主代理漏步骤、选错分支或选择性忽略并行任务而卡死；
   - 主代理不再消耗 token 推理流程下一步。
3. token 目标以结构性标准验收：主代理不读迁移表、不判断 pre/parallel、不选择 Ready 子集，宿主搬运只接收固定短指令；不设置固定百分比阈值或大规模 benchmark 门槛。

## 2. 本次交付边界

1. 完整实现方案稿当前定义的阶段 0–6：冻结契约、确定性内核、可靠动作协议、完整流程迁移、split 与三 VCS、旧逻辑删除、最终权威 canary 与安装全部属于本次交付。最终制品只保留 engine 权威路径；旧 run、旧编排入口及其迁移、兼容和回退旁路全部移除。
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
5. 包安装、卸载、hook、lifecycle、canary 与脱离 run 的快速 gate 检查属于维护/诊断面，可以保留，但不得绕过 engine 修改 workflow 状态。

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
5. 纯内存、廉价、确定性的连续变换，或能够在一个原子/幂等事务中一起恢复的操作，可留在单个 engine handler 内，由代码顺序机械保证，不要求把每条语句都持久化。若步骤涉及独立恢复/重试/超时/补偿/幂等、不可逆副作用或 UNKNOWN receipt、人/代理边界、fan-out/fan-in、独立审计/版本/证据，或必须避免重放已完成前缀，则必须拆成 Controller 跟踪的 `StepSpec`。
6. 定义的唯一 authoring 形态是封闭 Go 类型变体加 constructor 加显式节点/步骤表（ADR-001）；不引入 JSON/YAML 工作流 DSL、通用 schema 解释器、用户自定义节点插件或任意脚本/表达式扩展点。编译产物为具体结构组成、由 compiler 生成、checked-in 且字节级稳定的 canonical 制品；其步骤同样采用公共头（id/nodeID/ordinal/dependencies/kind/definitionVersion）加封闭变体 payload 的结构，变体不适用的策略字段不得存在于该步骤，不得实现为全字段平铺的单结构。每个步骤（`StepSpec`）在自身变体内包含 `id`、`nodeID`、`ordinal/dependencies`、可执行 pre/postcondition 引用、由变体派生并物化的 decision authority 与 runner kind、typed I/O codec 引用、retry/timeout、idempotency key、side-effect/receipt policy、interrupt policy、parallel group/join policy、failure-class map、definition version 与稳定 registry ID（handler/predicate/codec/reconcile）。制品不含函数、闭包、内存地址、绝对路径、当前时间或无序 map。AGENT 另须合法 `nonProgrammableReason`；可执行 HOST_ADAPTER 的 `hostBoundaryReason` 只能是 `EXTERNAL_CAPABILITY_BOUNDARY | USER_IO_TRANSPORT | AGENT_DISPATCH_API`。`MISSING_ENGINE_ADAPTER` 只是 diagnostic-only definition marker，不是 runner reason。
7. 八类非法定义拒绝结果全部保留，不因实现分层减少：不可达步骤/非法循环、无类型输入输出、仅以自然语言表达的 pre/postcondition、无幂等或 reconcile 的副作用、无 request/schema 的人工等待、无 join/failure policy 的并行组、缺合法理由的 AGENT/HOST，以及未绑定 definition version 的执行计划。局部非法组合优先由封闭类型与 constructor 消除（非法状态在正常 authoring API 下不可构造），全局图不变量由小型 closed-world compiler 校验，runtime loader 对版本绑定做最后防线；executable definition 只有在同一候选包 registry 完整、唯一解析其全部 registry ID 时才能激活。运行时只允许当前 eligible frontier，拒绝乱序、遗漏和重复 step；代理只看到当前已解锁步骤的最小输入、typed output 和 postcondition，不能决定下一步。
8. 引擎失败不得静默或动态降级给代理/LLM。固定分类为：`TRANSIENT_ENGINE_ERROR` → 机械 retry/backoff；`BUSINESS_REJECT` → 声明的业务边；`USER_ACTION_REQUIRED` → Ask/Wait；`SIDE_EFFECT_UNKNOWN` → reconcile/Wait/Operator；`INVARIANT_VIOLATION`/`BLOCKED_BUG` → 显式失败并 diagnose；只有定义中预先声明的 `AGENT_RECOVERABLE_SEMANTIC_ERROR` 可进入代理语义修复。
9. `state.json` 继续作唯一权威工作流投影；新增单调 revision/CAS、`pendingActions`、精确版本字段、按 TaskKey 派生的当前 Attempt、已确认 `granularity_review`/Batch/Subtask 计划及其 digest、持久化已确认 `requiredSources`、`AcceptanceManifest`、source↔point↔case map 及其 digest、typed `ReviewScopeMode` 和逐项 QA approved whitelist，不引事件溯源、SQLite、watchdog、OS 定时器、repo 级队列或开放式通用 Effect/Artifact 框架。StepSpec 的 side-effect/receipt 与 artifact 仅允许定义中穷举的 typed policy/schema。run envelope 同时绑定 definitionDigest 与 owning runtime 的 packageDigest/installed-target identity：packageDigest 是执行绑定而非审计附注，loader 在写入前必须校验其与实际执行 runtime 一致（或由 run 绑定的不可变 runtime sibling 执行并验证实际摘要，或持有显式 adopt receipt），不得仅凭 HandlerID 相同接受接管。三类身份职责分离：DefinitionDigest 绑定拓扑/registry ID/策略，PackageDigest 绑定实现字节与安装包，PlanDigest 绑定给定 state+observation 的决策结果；HandlerID 标识可恢复执行合同，合同不兼容变化必须晋升 ID，合同兼容的实现变化保持 ID 并由 PackageDigest 标识。
10. 动态任务走 `expectedTasks` / `TaskKey` / `TaskTransitionTable`；迁移表只管理 run-level phase。
11. `NextResult` 只有 Ready、HostAction、Ask、Wait、Operator、Complete 六类外部边界；同一 canonical Plan 的 Kind 唯一。
12. `Ready`/`HostAction` 只携薄指针和 actionID；主代理必须原样、整批搬运，不能选择或改写。每个 Ready agent action 显式绑定两个 StepSpec 边界：`ENGINE + HOST_ADAPTER + AGENT_DISPATCH_API` 的 spawn transport/SpawnReceipt，以及 `AGENT + AGENT_WORKER + nonProgrammableReason` 的 worker result；spawn 仍属于 Ready，不进入 HostAction。HostAction 是 `RESUME_AGENT | TERMINATE_AGENT | EXECUTE_ADAPTER_OPERATION` 的封闭 typed union；adapter operation 只能引用定义中注册的 operation/schema，不得成为自由 shell 通道。
13. `AskRequest` 只承载用户拥有的决定；`OperatorRequest` 只要求主代理核实证据、分类影响或处理无法自动对账的事实，Operator 本身不能替用户授权。
14. 所有主动控制先从带 freshness 的 `availableActions` 或受限 `REQUEST_*` 事件创建 pending Ask，再由 `submit` 完成。禁止无 request freshness 的自由 `USER_*` 写事件。
15. `submit` 校验 request/event/action ID、当前节点、schema、source bindings 与 digest；同 ID 同 digest 幂等返回稳定 acceptance/status，不重新发出已 ISSUED SpawnRequest；同 ID 不同 digest 硬拒绝。
16. provider 身份、bridge 安装/可用性与 lifecycle 配对分开建模。宿主是什么就绑定什么；bridge 缺失不允许把 provider 降级为 default，provider mismatch 硬拒绝。

## 6. 用户节点、Operator 与自动分支

### 6.1 宿主受理

1. 普通修改请求由插件提醒一次；复杂请求在完整需求/方案确认后、动手前再提醒一次。两次都不要求用户回应，不阻塞普通直接处理。
2. 用户明确提出 formal-gates 时直接进入完整受理，不在完整需求/方案确认后重复询问“是否进入正式流程”。用户可随时主动取消，但引擎不得制造重复 yes/no Ask。
3. 受理阶段逐项澄清重大需求与技术选择，并单独确认完整整合后的需求和方案。确认后如未明确模式，再选择 lightweight 或 regular；full/custom 仍在拆分决定之后选择。
4. `start` 后首个 drive 自动登记已确认 intake receipt/digest，不重复询问同一份需求确认。

### 6.2 正常 Ask

1. lightweight/regular 模式（用户尚未明确时）；lightweight 是 `start` bootstrap 例外，不能由 regular run 后期切入。
2. 高置信拆分的精确拓扑：边界、数量、依赖、并行度；低置信不拆或不确定由 engine 自动记录 no-split 理由。
3. regular 非分片 run 与每个 child 的 full/custom 路线；UI 可批量接受统一默认，但每个 child 持久化独立决定。
4. 产品审、技术审经 Operator 验证成立的每条 P0/P1/P2/P3 finding，以稳定 ID+digest 逐项 confirm/dismiss；可同屏展示但不得只存一条全局决定。Ask 已附带唯一、完整修法时，confirm 同时批准该修法，不再重复询问；只确认问题而修法仍含用户拥有的产品/重大技术选择时，必须继续澄清并确认更新后的完整需求/方案后才能修订。

### 6.3 条件 Ask 与用户主动事件

1. route-add/reselect：开发前可重选或追加；开发后只可追加普通门，不能新增 QA，不能静默移除既有义务。lightweight 与 retained merge master 拒绝普通 route-add。
2. `REQUEST_REQUIREMENT_CHANGE`：在任意 ACTIVE、非终态阶段均可提交，具体规则见第 7 节。
3. adopt-external、带精确保留/清除预览的 reset、abort。
4. 原因未知且 bindings 未变的代理中断：resume 原 attempt、fresh attempt 或 abort。
5. 黑盒用例、白盒用例/测试、merge QA 用例三类 pre-wave review 各自连续三次 FAIL 或长期不可用：重新设计、fresh review、修改需求、对精确 case-set/candidate 带理由 waiver/skip，或 abort；各自独立计数，均不计入开发后 repair wave。
6. 用户主动要求的复审规则 override、已 PASS 结果重审、fresh dispatch、同快照 QA 重跑、提前精确 Seal waiver。
7. QA/门发现的新细节无法由结构化事实唯一落入直接返修、QA artifact 返修、需求修改或作废时，生成最高优先级 `VALIDATION_DETAIL_DISPOSITION` Ask；该 Ask 覆盖“前三轮不问用户”，不委托 AI 猜根因。
8. 开发后 `wave >= 3` 仍有 obligation 时，一个 Ask 同时提供：恰好一轮额外修复、再试 QA、对精确 obligation IDs 授权 waiver、用户需求变更、abort。每个额外失败 wave 后重复同型 Ask。
9. 长期 RUNTIME_ERROR：重试对应 QA/门（scope 重新按事实判定）、精确 skip、需求变更或 abort。
10. master/child 的 reset、abort、重大需求变化和其他破坏性级联。
11. 任意 ACTIVE、非终态节点均提供 HUMAN 用户 typed 控制 Ask。用户可授权从当前节点跳转到任意合法业务节点；请求必须持久化 freshness token、request ID、requirement revision、from/to 节点、reason 与 authorization receipt。引擎改变 current 前，先 suspend/quarantine/invalidate/reconcile 受影响 Attempt、结果、候选和副作用；终止目标仍统一走 cleanup。

### 6.4 Operator/Wait/自动分支

1. Operator 负责拆分/路线建议、finding 正常入口和证据核实、需求变更影响集、carry，以及 engine/adapter 已穷尽结构化 observe/reconcile 后仍存在多重匹配、无唯一结论或语义歧义的 receipt UNKNOWN、provider/bridge、本地 effect、VCS 冲突事实判断；常规对账不能先交 Operator，Operator 也不能提交用户决定或执行 adapter 动作。
2. Wait 只用于容量为零、任务仍在运行、依赖未满足、receipt/lifecycle 尚可能迟到或 retryable I/O；Ask 不得伪装成 Wait。
3. 客观瞬态中断且 bindings 未变时自动 resume；任务/snapshot/责任变化或已知非瞬态原因时自动开新 Attempt 并使旧 Attempt stale；未知原因才 Ask。
4. receipt UNKNOWN 先查 lifecycle；唯一匹配自动 attach，多重/无匹配进 Operator。result-before-receipt 暂存并在对账后接纳；新 Attempt 后的旧结果以 `OBSOLETE_RESULT` 可见拒绝。
5. 无 obligation 时自动 Seal，不增加“是否 Seal”Ask。

## 7. 需求变化与开发前复审

### 7.1 来源必须分离

1. `USER_INITIATED_CHANGE`：仅此来源允许产品审/技术审使用影响集增量复审。
2. `REVIEW_FINDING_FIX`：产品审或技术审确认的 P0/P1 导致的修订，必须由全新零上下文代理对对应阶段的完整最新文档进行新鲜复审；不得通过增量 carry 减免。产品修订影响技术前提时，产品复审 PASS 后再做新鲜技术审；技术修订改变产品语义时先回完整产品复审。
3. 产品审/技术审确认的 P2/P3 仍逐项修订/修复，但不因严重度本身强制复审；若修订实际形成其他语义变化，再按其真实来源和影响进入相应复审。

### 7.2 用户主动需求变化可发生于任意非终态时刻

1. `REQUEST_REQUIREMENT_CHANGE` 在 Ask、Wait、Ready、Operator、产品/技术审、路线、开发、QA/门、修复、分片、主线合并和尚未提交完成的 Seal intent 中都必须可达；SEALED/ABORTED 后的新需求创建新 run。
2. barrier 固定分两阶段。Phase A 在尚无新确认 revision/`ImpactSet` 时，立即撤销未完成 Seal intent，并停止新的权威 VCS/状态写入、候选 promotion 和业务任务签发；可安全隔离、不会提交权威副作用的在途计算可以继续到 terminal，但结果一律 quarantine，不能推进 workflow。此时没有任何任务可被猜成“已知不受影响”；暂时无 eligible task 不违反最大并行不变量。
3. 用户先重新澄清并确认完整最新版需求/方案。只有确认后，Operator 才基于旧/新完整 revision 建立 changed items、transitive impacts、affected tasks/artifacts、carry reasons 的 `ImpactSet`；任何无法证明未受影响的任务、结果、candidate 或 receipt 一律归入 affected。
4. Phase B 机械 terminate/stale 受影响或 unknown 的在途 Attempt 和结果；对明确未受影响项恢复/接纳 quarantine 结果并携原 revision、证据和理由 carry，随后立刻按新 eligible frontier 补满容量。旧 revision 结果可留证，但未获 carry receipt 不得推进。
5. 增量产品审/技术审读取完整最新文档作上下文，但只重新判断变化项、传递影响和 carry 是否仍成立；影响无法可靠圈定或跨共享语义时扩大到全量。
6. QA 用例、路线、拆分拓扑、开发任务和验证结果按同一 ImpactSet 增量更新。开发中受影响 worker 用新 Attempt；开发后形成新快照，QA scope 按持久化 typed `ReviewScopeMode` 决定：黑盒/白盒 QA 用例或测试审查首次建立基线时为 `FULL`，其后仅对新增、修改或 ImpactSet 受影响项使用 `AFFECTED`；产品审、技术审和普通质量门始终为 `FULL`。该标记随 review/candidate/结果持久化，并保留未来开放给用户配置的类型能力。
7. 用户需求变化本身不计 repair wave，也不清零已完成的 run 级 wave；被变化打断、未满足完整 expected set 的旧 revision wave 不计数。
8. 分片由 master 接收需求变化并级联：受影响 child 暂停、重建或重新验证，未受影响 child 可继承；child 不得私自背离 master。已进入主线集成阶段后，普通需求增量由主线开发 worker 实现；若变化本身改变拓扑，再重建受影响 child/map/receipt。

### 7.3 产品审/技术审 finding

1. worker finding 是候选。Operator 对 P0–P3 逐项核实需求前提、正常入口、复现、证据、因果、严重度和范围；无效或范围外候选直接丢弃，不形成用户义务。
2. 每个 Ask 必须区分“问题 disposition”和“修法 authorization”。若 finding 已附唯一、完整、无用户选择空间的修法，confirm 同时绑定 finding digest 与 remedy digest；若用户只确认问题成立，或 remedy 仍有多个有意义的产品/重大技术选项，则进入需求/方案澄清，取得选择并再次确认更新后的完整整合文本后才能写入。纯机械措辞、引用或无歧义同步不制造重复 Ask。
3. P0/P1 confirm 且修法已获授权 → `REVIEW_FINDING_FIX` → 修订 → 对应阶段完整新鲜复审。dismiss → finding 作废；若没有其他已确认 P0/P1，候选结果可接纳为 PASS，不为驳回项另派一轮。
4. P2/P3 confirm 且修法已获授权 → 有界修订/修复且不因级别本身复审；dismiss → 作废。已拍板 stable finding ID 在后续提示词中注入，前提未变化时不得原样重提。

## 8. 拆分、并行、开发后审查与修复

### 8.1 拆分与 child 生命周期

1. `start` 不声明、不冻结拆分意向；拆分绑定唯一发生在 start-readiness PASS 后的拓扑确认：高置信要拆时 Ask 精确拓扑（边界、数量、依赖、并行度），低置信不拆或不确定由 engine 自动记录 no-split 理由。拓扑确认即绑定点——确认前用户可改变意向（无需需求变化、reset 或 abort），确认后不得重切，变更走用户需求变化（ImpactSet 级联）、reset/rebuild 或 abort。
2. master 确认拓扑后，Controller 固定执行：ENGINE_LOCAL 持久化 child-creation intent、分配 ID、创建 PREPARING child state 并注入 bindings → 每个 child 的 VCS adapter 独立 StepSpec 创建/对账 workspace/client → ENGINE 校验全部 typed receipts/identity → 原子提交 `sliceID → childRunID` map 并解锁 child。不得让本地 handler 越过 adapter 创建 VCS workspace，也不得把顺序交给宿主猜。
3. child 继承整体产品/技术审与精确拓扑，各自持久化路线；child 达标后进入可恢复的 `SLICE_READY` checkpoint，不作为独立最终 SEALED，也不提前删除恢复信息。
4. durable `SliceTerminalReceipt` 至少含状态、child/slice/master ID、版本、VCS snapshot/tree/digest、需求/拓扑/路线 bindings、QA/门结果、批准用例、成本和最后 request/event digest。master 只按期望 map 接纳 receipt。
5. 合并 QA 跨片用例的设计和审查与各 slice 开发并行。master 收齐 receipts 后由 engine VCS adapter 按固定 topology 顺序完成 Git/SVN/P4 集成，并在主线 snapshot 前物化已批准的 merge cases/其他 VCS 内最终 artifact；只有实际内容冲突需要语义判断时才派主线开发 worker，且它只编辑冲突内容，后续 resolved 校验、resolve/add、提交和 snapshot 仍由 adapter 执行。
6. 合并后始终并行执行合并 QA 与合并门。冲突处置或主线返修后，Controller 以 topology ownership、VCS diff 和每个 child receipt 的 validation view 机械计算 `AffectedChildSet`：若只改 integration-owned glue/跨片接缝且所有 child view 精确相等，只重跑合并 QA/合并门；若改变某个 child-owned 内容或无法证明其 view 未变，则在同一最终主线候选上额外补跑该 child 受影响的黑盒、白盒和普通门义务。仍不回 child、不重新 Seal child、不重复路线或澄清；所有当前 eligible 验证最大并行。无法唯一确定处置时先生成最高优先级 `VALIDATION_DETAIL_DISPOSITION` Ask；处置完成后，`wave >= 3` 才生成三轮统一 Ask。
7. child abort 产生 ABORTED receipt 并阻塞 master；master abort/reset/重大需求变化按已授权策略级联，不得遗留 orphan child。master 最终 Seal 汇总 child cases/cost 并清理 checkpoint。

### 8.2 强制并行不变量

1. `expectedTasks` 计算完整 eligible frontier；canonical Plan 必须包含所有依赖已满足的任务。
2. 若可用容量为 C、eligible task 为 N，`SelectIssued` 必须按固定顺序签发恰好 `min(C,N)` 个任务；不得留空容量、选择性忽略或把并行组退化为主代理决定的串行。
3. 主代理必须搬运整个 IssuedSet，不能挑子集。任一 receipt/result 释放容量后，`submit` 立即推进并补满下一批，无需主代理另想起调用。
4. 一个并行支路 FAIL/RUNTIME_ERROR 不得停止仍独立 eligible 的其他支路；只有依赖或 snapshot 已失效时才能阻断。
5. adapter 若因无可靠 correlation 必须串行化启动握手，只能限制未 attach 的 start；已 ACK worker 继续并行，engine 仍须填充其他兼容容量并给出可观察 WaitReason。
6. 开发期并行组至少含开发 worker 与黑盒 QA 设计/审查；分片时另含合并 QA 用例设计/审查。`DevelopmentSnapshot Dn` 冻结后，whitebox authoring/design/review 与所有只依赖生产 validation view 的黑盒执行和 selected gates 并行；这些生产验证先作为 provisional result。完整 `Qn` 冻结后，白盒执行与尚未运行或被判受影响的验证立即进入 frontier；可机械证明 validation view 未变的 provisional result 直接成为 `Qn` 的权威证据。修复轮和 merge 验证同样对全部实际 eligible 任务最大化并行。
7. “最大并行”只适用于依赖已满足且结果有机会成为权威证据的任务。投机结果本身不计 wave；只有 `ValidationViewReuseReceipt` 证明其声明输入在 `Dn` 与 `Qn` 间精确等价后才能进入 `Qn` expected set。无法证明、分类 UNKNOWN 或输入实际变化时，只使受影响任务 stale/rerun，不让代理凭语义口头 carry。

### 8.3 白盒隔离候选、统一验证与提升

1. 每次开发或生产代码返修先冻结只含当轮实现的不可变 `DevelopmentSnapshot Dn`。选择白盒时，Controller 令 VCS adapter 从精确 `Dn` 创建并登记独立的 whitebox authoring workspace（Git worktree/临时分支、SVN working copy/候选路径、P4 client/workspace/候选分支）；白盒设计代理可新增、修改或删除由 `TestOwnershipResolver` 证明为 test-owned 的结构测试、测试辅助文件、fixture 和对应 case 内容，返回 manifest，不执行任何 VCS 写操作。
2. `TestOwnershipResolver` 由项目/测试框架配置、目录/命名规则、测试发现结果及生产构建输入共同程序化判定；文件名包含 `test` 既非必要也非充分条件。VCS adapter 提供 `Dn → Qn` 每个 added/modified/deleted/renamed 路径及内容摘要。只有全部差异均为 test-owned，且生产代码、运行配置、生产依赖、构建输入和 provider 元数据投影完全相等，才签发 test-only/equal-production-view 证明；任何 UNKNOWN 一律按生产 view 受影响处理。
3. Controller 校验 manifest、测试引用、改动范围和 postcondition 后，由 adapter 程序化 track/add/open、commit/submit 并冻结包含 `Dn + 白盒测试 + 所有需纳入 VCS 的已批准最终 artifact` 的原生不可变 `ValidationCandidate Qn`。未选择白盒时 `Qn = Dn + 需物化的最终 artifact`。所有交付路径必须在 `Qn` 中，不能留在主工作树或临时 workspace 里等待 Seal 临时捞取。
4. 白盒 qa-review 读取完整 `Qn` 并独占测试代码质量、用例与实现引用的审查责任；实现质量门不审 test-owned 文件。FAIL 时回白盒设计形成新的 `Qn+1`，不计开发后 wave。若白盒 agent 需要改变生产代码或无法证明 test-only，该改变必须转交开发/repair 路径形成新 `Dn`，不能伪装成白盒测试增量。
5. 每项可提前执行的黑盒/门在签发时声明精确 validation view。黑盒 view 至少绑定被测生产代码或运行制品、运行配置、生产依赖、工具/环境；实现质量门 view 只含生产实现及其需求/方案上下文，明确排除 test-owned 文件。`ValidationViewReuseReceipt` 记录 `Dn/Qn` identity、VCS 差异、resolver/version、旧/新 view digest 与受影响任务集：digest 相等则把 provisional result 机械绑定到 `Qn`，不同或 UNKNOWN 则只重跑受影响项。该判定由 Controller 完成，不交给 agent。
6. 同一 wave 的权威 expected set 仍统一归属于最终 `Qn`：它可以由在 `Qn` 上直接运行的白盒/受影响验证结果，以及经精确等价 receipt 映射的 `Dn` provisional result 共同组成；工作树当前态、无 receipt 的旧候选结果或语义口头声明一律拒收。
7. repair 先按影响修订实现或白盒测试，再重新装配并冻结新的完整候选；生产修订从新的 `Dn` 重建/对账白盒 workspace，测试修订仍由独立白盒设计链完成。`QA_ARTIFACT_REPAIR` 虽不形成开发 `RepairObligation`，也必须执行同一候选替换不变量：旧 `Qn` 不再 join，修改后的用例/测试/fixture/helper/config 先 fresh review，由 adapter 物化并冻结 `Qn+1`，再按 `Qn → Qn+1` 差异重算 reuse/affected。改过的测试至少重跑自身；公共测试组件变化时重跑全部依赖测试；UNKNOWN 按受影响，只有 validation view 精确相等的旧结果可凭 receipt 沿用。每个 child 也遵循同一规则，且 `SLICE_READY` receipt 必须绑定已经包含白盒测试的最终 child identity；master 不得从遗留 QA workspace 补捞测试。
8. 完整 wave 无 obligation 后，adapter 才把整个 `Qn` 提升到主线，不得在末尾重新播放一份未经验证的测试补丁。优先保持同一原生 identity；若 Git/SVN/P4 产生新的最终 identity `F`，只有 `CandidatePromotionReceipt` 机械证明 `validatedDigest(Qn) == finalDigest(F)`（含 provider 特有的 mode/property/filetype/view 语义）时，整组证据才可映射到 `F`。promotion 出现内容冲突、转换、目标漂移、UNKNOWN 或任何 digest 不等价时不得 Seal；先 observe/reconcile，必要时把实际 `F` 冻结为新候选并按真实影响重跑验证。

### 8.4 发现项、QA scope 与三轮

1. 黑盒 case review、白盒 test/case review、merge QA case review 共用一份确定性的 `PreWaveReviewPolicy`，但按 `run/child + review kind + requirement revision + route/topology scope` 分别计连续语义 FAIL。首次建立审查基线的 scope 固定为 `FULL`；后续新增、修改或 ImpactSet 受影响项的审查 scope 固定为 `AFFECTED`。每次审查与 QA execution 均逐项核对白名单：缺失、未知、重复条目或仅有总体 PASS 一律拒绝，只有每项明确 `PASS` 才能进入持久化 approved whitelist。第 1、2 次 FAIL 自动重新设计并使用 fresh reviewer；第 3 次 FAIL 或长期不可用才 Ask：fresh redesign、重试/fresh review、用户需求变化、对精确 case-set/candidate 的带理由 waiver/skip，或 abort。PASS 关闭/重置该 series，RUNTIME_ERROR 不累计；这些尝试不计开发后 wave。case review 的集合级 P2 建议按 apply=resolved 吸收：按建议实现的用例修订视同已批准（与建议的关联留痕）、不因 P2 建议本身触发新 review 轮；超出建议内容的自拟修订仍需 fresh review。
QA 覆盖把同一次需求/方案确认中的有限 `requiredSources` 直接作为唯一权威交付义务列表，不从自然语言另行转抄第二份 inventory。source 在确认时只分 QA/非 QA；路线与 topology 确定后，由各已选 QA kind 的设计者直接在该 kind 的 `AcceptanceManifest` 中记录负责的 source，Controller 聚合校验每个 QA source 至少出现在一个已选 kind 的 manifest，非 QA source 只能使用带 PASS 证据的替代验证处置。一个 source 可映射多个 point/case；source↔point↔case 显式双向追踪且关系只保存一份、不设固定数量。每个 kind 的 review 返回逐 manifest source、逐 point、逐 case 决策和未绑定条目；多 kind 出现时全部已声明分支均须通过，仅集合级 PASS 不足以通过。execution 冻结 expected case ID set，并列出实际执行、合法继承和未执行条目；`FULL`/`AFFECTED` 都必须完整对账。`requiredSources` binding/manifest/map/whitelist digest 与当前 `ValidationCandidate` 精确绑定，摘要展开为 `sourceID → reviewKind → pointID → caseID → result`，`AUTHORIZED_SKIP` 不等同 `EXECUTED_PASS`。Controller/validator 负责逐 source→point→case 的结构完整性校验；覆盖率、mutation、property-based test 和 fuzz 只作辅助信号，不设固定阈值。3.5a 还必须提供文档化公开入口 `formal-gates coverage validate`、`coverage project-whitelist`、`coverage reconcile-execution`，统一使用 JSON 输入/输出验证该契约；入口是薄适配层，不接入 workflow 状态机。3.5a 本次确认的权威 source ID 与 QA 分类直接列在 `refactor-plan/stage-3-5a-qa-coverage-contract.md` 和 `refactor-plan/stage-3-5a-solution.md`，两表合为一份逻辑列表，不再派生其他清单。
2. QA/门发现开发后新细节时，Controller 只根据 FailureClass、producing task kind、artifact ownership 和 typed receipt 计算合法处置，不让 Operator/其他 AI 判根因。唯一分支机械执行；多个合法分支或 UNKNOWN 时生成 `VALIDATION_DETAIL_DISPOSITION` Ask，固定选项为 `DIRECT_REPAIR`（直接返修）、`QA_ARTIFACT_REPAIR`（修用例/测试）、`REQUEST_REQUIREMENT_CHANGE`（按需求修改重走流程）和 `DISMISS`（作废）。
3. pending disposition 期间不形成 `RepairObligation`，对应 expected result 记为 `AWAITING_DISPOSITION`，当前 batch 不计 wave。`DIRECT_REPAIR` 才形成 obligation 并使本轮可结算；`QA_ARTIFACT_REPAIR` 使受影响 QA 证据与旧 candidate binding stale，完成 design/review 后冻结新候选、至少重跑改过的测试并按依赖扩大，再在新候选 join；artifact 修订本身不增加开发返修轮，替代候选完成时该 logical wave 只计一次。`REQUEST_REQUIREMENT_CHANGE` 进入用户主动需求变化 barrier；`DISMISS` 不形成 obligation。该 Ask 的优先级高于 `wave < 3` 自动返修规则。
4. 处置唯一确定或用户选择直接返修后，经正常入口与范围核实的 QA FAIL、gate P0/P1/P2/P3 形成 `RepairObligation`。P2/P3 gate 结果仍可为 PASS，但 obligation 独立存在；范围外 P3 只留建议，不形成 obligation。
5. 开发后首次在同一完整 `ValidationCandidate` 上的 expected set 全部终态为 wave 1，每个修复候选的完整 expected set 各计一轮；白盒候选装配/pre-wave review、provisional result、`AWAITING_DISPOSITION`、PENDING、RUNTIME_ERROR、缺回执和被需求变化打断的不完整 wave 不计数。
6. `wave < 3` 且有已完成处置的 obligation 时自动派开发 worker 修复，不询问用户；仅 P2/P3 也相同。黑盒/白盒 QA review 与 execution 遵循持久化 `ReviewScopeMode` 的 `FULL` 首次基线、`AFFECTED` 后续受影响项规则；产品审、技术审和普通质量门始终 `FULL`。
7. `wave >= 3` 且仍有 obligation 时生成一个统一 Ask：一轮额外修复、再试 QA、逐 obligation waiver、用户需求变化或 abort。用户选择额外轮只授权一轮；仍失败则重复同型 Ask。
8. RUNTIME_ERROR 不算 finding、不计 wave；安全重试自动进行，长期不可恢复才 Ask 重试/skip/change/abort。
9. 正常 Seal 自动执行。PENDING 不能 waiver；FAIL/RUNTIME_ERROR/P2/P3 obligation 的 waiver 必须绑定当前 validation candidate、request、精确 obligation ID 和理由，后续候选不沿用。

### 8.5 开发分批与组间验证

1. **批次计划义务**：受理阶段只确认需求与方案，不冻结批次计划。Part 2 `start-readiness` 技术审一并输出结构化 `granularity_review`，先判断单一 Batch 还是多个 Batch，再登记 `Batch → Subtask` 映射、依赖/接口/状态边界、风险、DoD/测试、回滚/恢复、交接与上下文成本及不拆/改拆理由。显式批次计划在拆分决定与路线确认时一并产生，经同一次用户决定确认并留痕，于开发开始前登记为任务清单的组织结构；允许"单批 + 理由"退化形态。批次划分依据依赖链、独立交付物/验收和风险：依赖链（消费链）必须串行；零耦合支线可并行；共享文件、接口或语义的工作面不并行。接口冻结点优先作为批边界。编号、commit 数、文件数、行数、阶段/层/角色/时间段本身不得作为大块依据。
2. **批粒度**：批是任务单元，单批一个内聚关注点——语义判据为"能用一句话说清本批 diff 即一批"；批内提交保持原子、可独立构建。批内每次编辑后执行项目声明的低成本确定性检查，批边界执行该开发计划指定的回归命令；每批终态可判——树可构建、声明的回归通过、自测留痕、任务清单逐批勾选。
3. **探路优先**：含不确定性边界的探路工作必须作为首批 spike：结论留痕，代码不进入交付。
4. **验证档位**（随批次计划一并选定并留痕，依据与已拒绝替代见 ADR-002）：批内自测为固定底座（永远启用）；默认为开发完成后单次全量门禁；确有需要时增设组间快照 + 增量审查 checkpoint（冻结中间 `DevelopmentSnapshot`，复用 `ReviewScopeMode` 与 carry 继承判定：未受影响 PASS 直接沿用，只对增量 diff 审查/执行；组间轮不计 repair wave）。档位依据结构性信号：预期生产逻辑是否多模块扩散、独立验收单元数；不依据总行数或批数。split 属 run 拆分机制（见 8.1），不是验证档位，不在此选择。
5. **不变量**：批次不改变 run 边界、公共入口、Seal 语义或强制并行不变量；不产生额外受理、双审或 Seal 轮次。
6. **引擎义务与机械接入**：用户确认 `granularity_review` 后，Controller 将其绑定到需求/方案 revision、拆分决定和路线，并把 Batch、Subtask、TaskKey 映射及批间依赖登记为现有任务计划的结构化信息。引擎只校验 ID 唯一、映射无遗漏/重复、依赖无环、TaskKey 归属明确，以及 DoD/验证/恢复/交接字段齐全；它不从自然语言重新判断是否应该拆分。`batch_id` 对应一次 `development-worker` 派发；`subtask_id` 只是该派发内的顺序步骤，不得单独派发或换代理；只有前一 Batch 的成员任务终态、DoD、验证和交接完成后，才解锁下一 Batch。Batch 契约、接口、依赖或 DoD 改变时，旧计划失效，必须重新进行 `granularity_review` 并取得用户确认。Batch 不是独立状态机、receipt 或生命周期，完成状态仍从成员 task 状态派生。
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

QA 覆盖契约的独立 fixtures 还必须证明：权威 `requiredSources` 中的 QA source 未出现在任何已选 kind manifest 时被拒绝；单 kind/多 kind manifest 覆盖、复杂 source→多个 point/case、多对多映射、无适用 QA 点的确认分类与替代验证 PASS 证据；未选 kind/非 QA source 出现在 manifest、未知/重复/缺失/orphan source/point/case 和集合级 PASS 被拒绝；`requiredSources` binding/manifest/map/whitelist digest 或当前 `ValidationCandidate` 不匹配时旧结果失效；`FULL` expected/actual 不一致与 `AFFECTED` 执行/继承/未执行集合不完整被拒绝；`AUTHORIZED_SKIP` 不计为执行 PASS。三个公开 `coverage` 命令还必须能从已构建二进制对合法输入返回成功、对上述常见校验失败返回可观察的非零结果和稳定 JSON 错误。该 checkpoint 只冻结契约与公开验收入口，regular E2E 留在阶段 4。

1. **迁移表与用户节点**：每条合法边、pre 拒绝、trigger 分类、Ask/Operator/Wait/HostAction/Ready/Complete、主动 `REQUEST_*`、终态与回退；正常入口不存在遗漏用户节点。
2. **节点内执行计划**：封闭变体 constructor 非法状态拒绝、closed-world compiler 图校验（可达性/循环/依赖/join 覆盖/版本绑定/registry ID 完备性）、合法/非法 reason、typed I/O、可执行 pre/postcondition、固定顺序、乱序/遗漏/重复拒绝、精确 frontier、崩溃后不重放已完成前缀，以及纯原子 handler 不被过度拆分。
3. **失败分类与禁止降级**：每类 engine/business/user/UNKNOWN/invariant/semantic error 只进入声明边；engine/adapter 故障绝不动态改派 AGENT；diagnostic-only `MISSING_ENGINE_ADAPTER` 不可编译/签发并稳定进入 `BLOCKED_BUG` diagnose；最终定义扫描证明本次范围内没有该 marker。
4. **并行**：eligible N、capacity C 时恰好签发 `min(N,C)`；整批搬运、释放容量立即补位、固定顺序无饥饿、支路失败不压制独立 sibling；`Dn` 后白盒 authoring/review 与生产 view 黑盒/门并行，`Qn` 后用精确 view digest 将未受影响 provisional result 纳入 expected set，只重跑受影响项，白盒 execution 与剩余任务立即 fan-out。
5. **任务与派发**：expected set 漏派/重复、TaskKey、Attempt、旧 Attempt、result-before-receipt、UNKNOWN/lifecycle 对账、同 actionID 不重复 spawn、重复 submit 不重放 SpawnRequest。
6. **需求变化**：`USER_INITIATED_CHANGE` 在每个非终态 phase 可达；Phase A 全局权威写屏障与 quarantine、确认后 ImpactSet、unknown=affected、Phase B 精确恢复/carry/refill、split cascade、pre-Seal 取消；与 `REVIEW_FINDING_FIX` 完整新鲜复审不可混淆。
7. **开发前发现项**：P0–P3 验证、逐 ID settle、finding/remedy 双绑定、多个用户选项时重新确认完整文本、P0/P1 完整新鲜复审、P2/P3 修复不因级别复审、dismiss、混合严重度和前提变化。
8. **开发后三轮**：白盒隔离 authoring、新测试识别、test-only/production-view 等价证明、provisional result 精确复用、完整候选 freeze/review/execution、精确 promotion；实现质量 gate definition/catalog/README 均排除 test-owned 代码且白盒 review 独占测试质量、门内维度保持显式命名并列（架构与耦合不弱化）；三类 pre-wave review 独立三连 FAIL 升级、集合级 P2 建议按 apply=resolved 吸收（建议实现即视同批准、自拟扩展仍需 fresh review）；验证细节唯一处置自动、歧义/UNKNOWN 时最高优先级 `VALIDATION_DETAIL_DISPOSITION` Ask 及四种决定转移；`QA_ARTIFACT_REPAIR` 必须冻结替代候选、至少 fresh review/重跑改过的测试、按共享依赖扩大并重新计算 reuse/affected 后 join，普通与 merge 路径一致；P2/P3-only 自动修复且派发任务含完整自测要求；ReviewScopeMode 首次基线 FULL、后续新增/修改/ImpactSet 受影响项 AFFECTED，产品审/技术审/普通质量门始终 FULL；`wave < 3` 自动、`wave >= 3` 统一 Ask；候选装配、待处置、RUNTIME_ERROR/不完整 wave 不计数。
9. **分片**：拓扑绑定唯一时点（start 无拆分意向声明；start-readiness 后确认、确认前可改、确认后不重切）、topology、child creation/map、继承、逐 child 路线、包含白盒测试的 child 最终 identity、`SLICE_READY`、durable receipts/cases/cost、master 等待、adapter 集成与语义冲突窄代理边界、合并 QA 设计/审查并行、主线 repair 的 AffectedChildSet 与精确补验、级联终态。
10. **错误与恢复**：invalid event、retryable I/O、fatal corruption、deadline 重入、provider/bridge/mismatch、HostAction、adopt-external 完整转换、候选 freeze/reuse/promotion/cleanup 与所有本地副作用崩溃窗口；promotion 后 receipt 前恢复不得重复集成。
11. **旧 run 与旧入口**：缺版本和任一版本不匹配稳定 `UNSUPPORTED_RUN_VERSION`；`diagnose` 原始只读；无迁移、legacy 续跑、handoff、旧公开命令、兼容别名或 cleanup 删除后门。
12. **VCS 程序化契约与 3×2**：三种 provider 的探测/status/diff/track/integrate/commit/snapshot、whitebox workspace、完整候选 freeze、identity-preserving 或 digest-equivalent promotion、resource registry cleanup、冲突窄代理边界、typed artifact、UNKNOWN reconcile 和禁止代理写 VCS；Git、SVN、P4 各跑通非分片与分片 master/child/merge，共六个权威单元格。
13. **宿主**：DSH、Claude Code、Codex、Cursor 各自在真实环境跑一次 Git 非分片完整正式流程，覆盖产品审、技术审、开发/黑盒设计并行、whitebox workspace 与生产验证并行、test-only/view 等价复用、完整候选、白盒与受影响验证、精确 promotion、至少一次 FAIL→repair→重审、receipt/result、一次中断恢复、无残留 cleanup 和 Seal。
14. **最小 canary 数**：四宿主 Git 非分片与 VCS 3×2 可复用一个 Git 非分片单元格，故最低九条完整 canary；不要求四宿主×三 VCS×两种拆分的笛卡尔积。全部权威结果必须绑定同一份已删除旧运行时逻辑的最终候选 revision 和实际安装副本。
15. **黑盒边界**：文档化正常入口、常见误操作和已知恢复/崩溃窗口；对抗输入、恶意/手工状态编辑、权限/不可变文件和未支持 OS 只作 P3、不阻塞。
16. **canonical 制品与身份**：compiler 同一生成动作产出 `definitions/workflow.json` 与期望身份常量，禁止人工双写；authoring source 重新生成 checked-in 制品字节无 diff（独立于 round-trip 的 freshness CI）、任意 assembly 顺序同字节、decode→encode 字节不变、跨进程/重复构建同字节；只改 handler 实现不改 ID 与定义语义时 definition digest 不变而 package digest 变，改变 dependency/policy/reason/schema ID/handler ID/join 语义时 definition digest 必变；registry 对每个 ID 精确解析一次，缺失、重复或 kind 不匹配拒绝；packageDigest 执行绑定（含 runtime sibling 实际摘要校验与 adopt receipt 路径）有正反向测试。

## 11. 验收判定

1. 方案稿全部确定性闸门、节点内执行计划顺序/失败分类、强制并行不变量和用户节点覆盖测试通过。
2. 六个 VCS×split 单元格、四宿主 same-host canary、独立 QA 全部 PASS；每项有可复现事件、状态、snapshot 和最终 Seal/receipt 证据。
3. 任何范围内 FAIL/P0/P1/P2/P3 obligation 按已确认三轮规则闭环；范围外项不阻塞。
4. 最终候选不存在旧 run 兼容、迁移、legacy mode、旧公开推进入口、第二写入口、cleanup 后门或 engine→legacy 回退。
5. 正常流程中不存在本可程序化却交给代理的操作，也不存在 engine/VCS adapter 故障后动态降级给代理的路径。
6. 当前流程 Seal 且全部权威 canary 绑定同一最终候选后，才切换全局安装并退役开发兜底。
7. 删除或改写仍断言已废弃受理/复审/轻量路线文本的旧测试；最终 `go test ./...` 必须在不恢复旧逻辑、旧文案或兼容入口的前提下 PASS。当前基线中这类旧断言的失败属于待开发清理事实，不得以回填过时行为消绿。

## 12. 非目标

1. 修复缺陷交接单 #2。
2. headless adapter、随机 chaos、大规模 benchmark。
3. 事件溯源、SQLite、watchdog、OS 定时器、repo 级集成队列/分片合流 CAS。
4. 本次承诺 Windows/macOS/Linux 适配矩阵，或把不支持平台升级成阻塞项。
5. 对抗性输入、恶意或手工状态编辑的绝对防御。
6. 旧 run 兼容、状态迁移、旧命令续跑或双权威回退。

## 13. 用户已确认的决策记录

- 用户主动提出 formal-gates 后不重复询问是否进入；插件只非阻塞提醒一次或复杂需求时两次。
- 完整需求/方案仍须单独确认；lightweight 是 start bootstrap 例外，full/custom 在拆分决定后确认。
- 所有用户选择能力保留为 typed Ask/available action，经唯一 `submit` 入口落账。
- 产品/技术审 P0/P1 修订做完整新鲜复审；P2/P3 逐项询问并修复，但不因级别本身复审。
- 只有用户主动需求变化使用影响集增量复审，且可发生在任意非终态时刻；不计也不重置 run 级 wave。
- 开发后 QA FAIL 与范围内 gate P0–P3 在处置已明确时，前三轮自动修复；仅 P2/P3 也不问用户；但若结构化事实无法唯一确定“直接返修 / 修 QA artifact / 按需求修改重走流程 / 作废”，必须先让用户决定，这条规则优先于前三轮静默返修。用户可明确选需求修改流程或直接返修；`wave >= 3` 的统一询问在处置完成后才适用。
- 用户选择修 QA artifact 后必须生成包含修改的新候选；改过的测试至少 fresh review 并重跑自身，共享 helper/fixture/harness/config 变化时扩大到所有依赖测试。未受影响结果只有经精确 view receipt 才沿用，不能回旧候选 join；普通、child 与 merge 主线相同。
- QA review/execution scope follows persisted typed `ReviewScopeMode`: first baseline FULL, then AFFECTED only for new, modified, or ImpactSet-affected items; product review, technical review, and ordinary quality gates remain FULL. 长期 RUNTIME_ERROR 可选择再试 QA。
- 并行由引擎最大化强制执行，不允许主代理选择性忽略可派任务。
- 开发后白盒在独立 workspace 编写/审查测试，同时黑盒和只依赖生产 view 的门立即执行；最终候选冻结后以 VCS 差异、test ownership 与 production-view digest 机械复用未受影响结果，只重跑受影响项。实现质量门不审测试代码，测试质量归白盒 review。分片开发期间并行设计/审查合并 QA 用例。
- Seal/Abort 必须由 adapter 清理全部登记的 Git/SVN/P4 临时 workspace/client/ref/changelist，核验无残留后才 Complete。
- 合并 QA/合并门失败只派主线开发 worker 修复；只改 integration-owned 内容时仍只重跑合并双验证，若改到 child-owned 内容则在最终主线候选补跑该 child 受影响义务，不回 child。
- Git/SVN/P4 的分片和非分片都支持并验收；OS 不作本次承诺，只保持实现通用性。
- 节点内部已知顺序也必须由 Controller 机械钉死；采用业界常见的 durable workflow/activity 边界，只在需要独立恢复、副作用、并行或审计时拆持久 StepSpec，不机械拆碎纯原子计算。
- 所有可程序化动作都由 ENGINE 决定；代理仅负责语义判断、创造性实现和独立审查，VCS 写操作与集成顺序全部由 Git/SVN/P4 adapter 执行，且引擎失败不得动态降级给代理。
- 旧 run/旧命令不兼容、不迁移、不续跑；只有 raw diagnose，失败后以当前版本新建。
- 当前稳定安装仅保障 `orch-engine-003` 开发完成；最终候选隔离验证并在 Seal/全部 canary PASS 后安装。
- 步骤模型采用 typed Go authoring（封闭变体 + constructor + 显式表）→ 小型 closed-world compiler → 单一 canonical encoder → compiler 生成 `definitions/workflow.json` 与身份常量的形态（ADR-001）；拒绝扁平万能 StepSpec、通用 DSL/schema 解释层、完全删除定义编译器与定义制品，以及为实现对照建两套架构。
- HandlerID 只在执行合同或恢复兼容边界变化时晋升，合同兼容的实现变化保持 ID、由 PackageDigest 标识；executable definition 与二进制 registry 锁步激活；packageDigest 是 owning-runtime 执行绑定而非审计附注。
