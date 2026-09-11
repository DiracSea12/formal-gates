# 编排流水线化（确定性引擎交权）· 主需求

> 状态：已由用户确认，进入 formal-gates 正式流程。
> 需求归属：formal-gates 自身重构。
> 方案文档：`refactor-plan/final-implementation-draft.md`（方案稿、SKILL 与参考文档冲突时以本文为准）。
> 2026-09-07 同步：3.5 系列已交付语义与后续 engine 接入合同的来源、生效范围见
> `references/requirements-precedence.md` 的同日补充；engine 接入与执行前准入仍属于后续交付。

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

1. `workflow start`：唯一 bootstrap 写入口；创建精确当前版本的 top-level 受理 run；模式尚未明确时保持未绑定，确认需求后绑定 regular 或 lightweight，regular 再按拆分决定确定是否为 retained master。child 由 engine 内部 handler 创建，不要求宿主拼接多个 `start`。
2. `workflow drive`：不带外部事件，确定性推进到下一个外部边界。
3. `workflow submit`：唯一外部事件写入口；提交用户决定、主动控制请求、worker 结果、HostAction receipt 与 lifecycle 事件。不得保留 `drive --event`、`submit-result` 或其他第二写通道。
4. `workflow show` / `status` / `next` / `diagnose`：严格只读。`next/status` 可展示带 freshness token 的 `availableActions`；`diagnose` 可原始只读检查 current、terminal、unsupported、corrupt、missing 状态。
5. `workflow guard-preview --input <basis.json>`：公开只读、非权威的生产 guard 计算预览；schema/fixture 见 `references/guard-preview.md`。不创建 run、不写状态、不认证真实确认或任务合法性；3.5b 的 `runtimeIntegration=NOT_INSTALLED`，阶段 4 才切换为权威 engine 集成状态。
6. 包安装、卸载、hook、lifecycle、canary 与脱离 run 的快速 gate 检查属于维护/诊断面，可以保留，但不得绕过 engine 修改 workflow 状态。
7. `coverage validate`、`coverage project-whitelist`、`coverage reconcile-execution` 保留为 workflow 外的只读契约校验入口；只消费 JSON 并调用同一 coverage 核心，无独立状态或 writer。3.5 新增的 workflow 入口逐项按 `refactor-plan/route-matrix.md` 内收为 handler、typed submit event 或只读投影，最终不保留第二推进通道。

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
5. **分阶段稳定驱动与全局发布**（2026-09-07 用户修订）：阶段 0–3 保留历史 driver 绑定；阶段 4 按用户“用当前版本开发”的决定，使用本阶段需求案记录的精确当前版本作为固定 driver，以新 run 承接，不让新版续写不兼容的旧状态。阶段 4 自身的 QA/canary、Seal、主线集成及必要重验完成后，把全局插件切到阶段 4 精确发布包并检查实际入口。发布成功后冻结该版本作为阶段 5–7 的 stable driver，本阶段开发驱动退出日常驱动；各阶段候选仍隔离验证，不签发自己的权威 Seal。阶段 7 全部最终证据成立后再将全局阶段 4 包升级为最终完整版本。每次安装沿用原子事务、失败回退及旧 run 不迁移合同；发布回退包不构成 workflow 运行时旁路。阶段 4 提前发布只承诺已验收的 Codex/Git no-split regular 与既有 lightweight，不提前宣称其余能力完成。

## 5. 确定性内核与外部指令

1. Go 静态迁移表是唯一流程权威；`Observe`（只读外部事实）、`Decide`（纯函数、产生完整 canonical Plan）、`SelectIssued`（按 Admission 机械裁剪）分离。
2. 每个节点编译为有序 `NodeExecutionPlan`。已知顺序、前后置条件、分支、重试、等待、fan-out/fan-in 和副作用协议都由 Controller 机械执行；不得只在代理提示词中描述顺序后期待代理自觉遵守。
3. 执行责任使用两个正交维度：`DecisionAuthority = ENGINE | AGENT | HUMAN`，`RunnerKind = ENGINE_LOCAL | DURABLE_ACTIVITY | HOST_ADAPTER | AGENT_WORKER`。HOST 只表示外部能力/执行位置，不拥有流程决定权；HUMAN 只能经 Ask。Operator 是当前主代理执行的 `AGENT + AGENT_WORKER + SEMANTIC_JUDGMENT` typed observation，不是 HUMAN 决策，也不能借 Operator 执行 engine/adapter 动作。
4. 能确定性实现的操作必须由 ENGINE 决定并通过本地 handler、durable activity 或 host adapter 程序化执行。AGENT 只允许用于 `SEMANTIC_JUDGMENT`、`CREATIVE_IMPLEMENTATION`、`INDEPENDENT_REVIEW` 三类不可程序化工作；“实现麻烦”不是理由。缺 adapter 只可在开发期 diagnostic compiler mode 标记为 `MISSING_ENGINE_ADAPTER` 技术债；该定义不得进入 executable plan、不得签发 Ready/HostAction，正常 compile/drive 必须以 `BLOCKED_BUG` 和 diagnose 拒绝。本次范围内的最终候选不得残留该 marker。
5. 纯内存、廉价、确定性的连续变换，或能够在一个原子/幂等事务中一起恢复的操作，可留在单个 engine handler 内，由代码顺序机械保证，不要求把每条语句都持久化。若步骤涉及独立恢复/重试/超时/补偿/幂等、不可逆副作用或 UNKNOWN receipt、人/代理边界、fan-out/fan-in、独立审计/版本/证据，或必须避免重放已完成前缀，则必须拆成 Controller 跟踪的 `StepSpec`。
6. 定义的唯一 authoring 形态是封闭 Go 类型变体加 constructor 加显式节点/步骤表（ADR-001）；不引入 JSON/YAML 工作流 DSL、通用 schema 解释器、用户自定义节点插件或任意脚本/表达式扩展点。编译产物为具体结构组成、由 compiler 生成、checked-in 且字节级稳定的 canonical 制品；其步骤同样采用公共头（id/nodeID/ordinal/dependencies/kind/definitionVersion）加封闭变体 payload 的结构，变体不适用的策略字段不得存在于该步骤，不得实现为全字段平铺的单结构。每个步骤（`StepSpec`）在自身变体内包含 `id`、`nodeID`、`ordinal/dependencies`、可执行 pre/postcondition 引用、由变体派生并物化的 decision authority 与 runner kind、typed I/O codec 引用、retry/timeout、idempotency key、side-effect/receipt policy、interrupt policy、parallel group/join policy、failure-class map、definition version 与稳定 registry ID（handler/predicate/codec/reconcile）。制品不含函数、闭包、内存地址、绝对路径、当前时间或无序 map。AGENT 另须合法 `nonProgrammableReason`；可执行 HOST_ADAPTER 的 `hostBoundaryReason` 只能是 `EXTERNAL_CAPABILITY_BOUNDARY | USER_IO_TRANSPORT | AGENT_DISPATCH_API`。`MISSING_ENGINE_ADAPTER` 只是 diagnostic-only definition marker，不是 runner reason。
7. 八类非法定义拒绝结果全部保留，不因实现分层减少：不可达步骤/非法循环、无类型输入输出、仅以自然语言表达的 pre/postcondition、无幂等或 reconcile 的副作用、无 request/schema 的人工等待、无 join/failure policy 的并行组、缺合法理由的 AGENT/HOST，以及未绑定 definition version 的执行计划。局部非法组合优先由封闭类型与 constructor 消除（非法状态在正常 authoring API 下不可构造），全局图不变量由小型 closed-world compiler 校验，runtime loader 对版本绑定做最后防线；executable definition 只有在同一候选包 registry 完整、唯一解析其全部 registry ID 时才能激活。运行时只允许当前 eligible frontier，拒绝乱序、遗漏和重复 step；代理只看到当前已解锁步骤的最小输入、typed output 和 postcondition，不能决定下一步。
8. 引擎失败不得静默或动态降级给代理/LLM。固定分类为：`TRANSIENT_ENGINE_ERROR` → 有限机械 retry/backoff；外部 agent/host 派发缺显式 retry policy 时使用 canonical `MaxAttempts=2`（首次执行 + 一次自动重试），compiled definition 可声明其他有限值但不得无界；额度或 retry 耗尽进入 typed Ask，不得伪装成 Wait。`BUSINESS_REJECT` → 声明的业务边；`USER_ACTION_REQUIRED` → Ask/Wait；`SIDE_EFFECT_UNKNOWN` → reconcile/Wait/Operator；`INVARIANT_VIOLATION`/`BLOCKED_BUG` → 显式失败并 diagnose；只有定义中预先声明的 `AGENT_RECOVERABLE_SEMANTIC_ERROR` 可进入代理语义修复。
9. `state.json` 继续作唯一权威工作流投影；新增单调 revision/CAS、`pendingActions`、精确版本字段、按 TaskKey 派生的当前 Attempt、已确认 `granularity_review`/Batch/Subtask 计划及其 digest、持久化已确认 `requiredSources`、`AcceptanceManifest`、source↔point↔case map 及其 digest、typed `ReviewScopeMode` 和逐项 QA approved whitelist，以及产品/技术审分层的 raw candidate、finding adjudication、correction receipt 与 effective review status，不引事件溯源、SQLite、watchdog、OS 定时器、repo 级队列或开放式通用 Effect/Artifact 框架。原始 review candidate 不删除；纠正以绑定精确 result/finding digest、revision、原因和授权来源的有界 typed receipt supersede adjudication，再由 Controller 纯函数重算有效状态与依赖失效。StepSpec 的 side-effect/receipt 与 artifact 仅允许定义中穷举的 typed policy/schema。3.5b guard 额度只由 engine Controller 从当前 revision 的 canonical expected external TaskKey、已确认 route/topology、compiled definition 和有限 retry policy 纯函数计算；主代理、host、用户和 submit event 不得传入底层数字。expected TaskKey 只能由 closed-world definition 与已确认、持久化的 Batch/route/topology、已批准 QA 条目、已选 gate 或已成立 obligation 派生；当前已确认 workflow policy 预授权的 obligation/repair slot 由 Controller 机械加入，其他新增槽位若按现有流程要求用户确认则必须先绑定对应 revision/typed Ask。普通代理结果、host receipt 或 resume 不得直接或静默扩容。计算 basis digest、结果和已用 action/Attempt 账目随同一 state 投影持久化，每次签发前重算；相同 basis 结果不一致、digest/账目不匹配或将要超发时以 `BLOCKED_BUG` 零派发，不得降级给 AI 猜测。run envelope 同时绑定 definitionDigest 与 owning runtime 的 packageDigest/installed-target identity：packageDigest 是执行绑定而非审计附注，loader 在写入前必须校验其与实际执行 runtime 一致（或由 run 绑定的不可变 runtime sibling 执行并验证实际摘要，或持有显式 adopt receipt），不得仅凭 HandlerID 相同接受接管。三类身份职责分离：DefinitionDigest 绑定拓扑/registry ID/策略，PackageDigest 绑定实现字节与安装包，PlanDigest 绑定给定 state+observation 的决策结果；HandlerID 标识可恢复执行合同，合同不兼容变化必须晋升 ID，合同兼容的实现变化保持 ID 并由 PackageDigest 标识。状态投影还须承载 §8.4.1 的保证激活/责任分配与 §8.4.2 的 shared-step、suggestion、checkpoint、Codex cost/timing 记录，沿用同一 writer；不额外建立 QA 或计量账本。
10. 动态任务走 `expectedTasks` / `TaskKey` / `TaskTransitionTable`；迁移表只管理 run-level phase。
11. `NextResult` 只有 Ready、HostAction、Ask、Wait、Operator、Complete 六类外部边界；同一 canonical Plan 的 Kind 唯一。
12. `Ready`/`HostAction` 只携薄指针和 actionID；主代理必须原样、整批搬运，不能选择或改写。每个 Ready agent action 显式绑定两个 StepSpec 边界：`ENGINE + HOST_ADAPTER + AGENT_DISPATCH_API` 的 spawn transport/SpawnReceipt，以及 `AGENT + AGENT_WORKER + nonProgrammableReason` 的 worker result；spawn 仍属于 Ready，不进入 HostAction。HostAction 是 `RESUME_AGENT | TERMINATE_AGENT | EXECUTE_ADAPTER_OPERATION` 的封闭 typed union；adapter operation 只能引用定义中注册的 operation/schema，不得成为自由 shell 通道。
13. `AskRequest` 只承载用户拥有的决定；`OperatorRequest` 只要求主代理核实证据、分类影响或处理无法自动对账的事实，Operator 本身不能替用户授权。
14. 所有主动控制先从带 freshness 的 `availableActions` 或受限 `REQUEST_*` 事件创建 pending Ask，再由 `submit` 完成。禁止无 request freshness 的自由 `USER_*` 写事件。
15. `submit` 校验 request/event/action ID、当前节点、schema、source bindings 与 digest；同 ID 同 digest 幂等返回稳定 acceptance/status，不重新发出已 ISSUED SpawnRequest；同 ID 不同 digest 硬拒绝。
16. provider 身份、bridge 安装/可用性与 lifecycle 配对分开建模。宿主是什么就绑定什么；bridge 缺失不允许把 provider 降级为 default，provider mismatch 硬拒绝。
17. 签发之后、真实调用之前，宿主 adapter/bridge 必须用现有 pendingActions、锁/CAS、intent 和 guard 核对并认领动作，绑定 actionID、TaskKey/Attempt、run/revision、candidate、provider、payload digest、scope 与精确 case set。流程规则和每项操作内部顺序由插件程序控制，宿主只适配已校验调用与事实回传，不取得流程决定权。原生派发/恢复工具、工作流模型 CLI/API 和已声明 QA 操作的正常入口均须覆盖；未签发、越界、过期、取消或已认领/消费的重复动作在底层调用前拒绝，调用数为零。合法 QA 任务内部请求沿用任务绑定，不逐请求新增 TaskKey。能力无法覆盖的宿主入口明确 unsupported，并拒绝进入承诺该保证的模式；不把提示词或结果登记拒绝当成执行前强制。阶段 4 验收首宿主、阶段 6 覆盖五宿主、阶段 7 验证最终安装态；这项 3.5c Seal 后补充的要求不算作 3.5c 已交付能力。

## 6. 用户节点、Operator 与自动分支

### 6.1 宿主受理

1. 普通修改请求由插件提醒一次；复杂请求在完整需求/方案确认后、动手前再提醒一次。两次都不要求用户回应，不阻塞普通直接处理。
2. 用户明确提出 formal-gates 时直接进入完整受理，不在完整需求/方案确认后重复询问“是否进入正式流程”。用户可随时主动取消，但引擎不得制造重复 yes/no Ask。
3. 受理阶段逐项澄清重大需求与技术选择；无待澄清问题也须明确说明。先将完整需求与方案直接写入唯一正式文件，启动受理 run 绑定该文件；用户尚未明确模式时，该 run 不预设为 regular。结构预检后展示全文与精确 revision，再单独取得用户确认；确认前不选择路线、不派发后续代理。确认后如未明确模式，再选择 lightweight 或 regular；full/custom 仍在拆分决定之后选择。
4. `start` 只登记受理与工件绑定，不把预检或启动当作用户确认；确认由绑定所展示文件 revision 的 typed decision 经 `submit` 登记，随后 drive 消费同一 receipt，不重复询问。阶段 3 候选验证使用固定 driver 的 intake receipt 桥接，只是开发期隔离合同，不替代最终 regular 受理链路。
5. regular 不要求用户原始输入采用 OpenSpec、PRD 或其他固定格式。AI 直接把对齐结果写入唯一正式 UTF-8 Markdown 需求案，不新增草稿或人工覆盖清单；自然语言正文保留，唯一 `## 需求点` 章节以固定 `REQ → 要求/验收条件/来源` 和唯一 `AC` 结构声明全部必须实现、改变产品/流程行为或需要 QA 保证的义务。确定性预检只校验结构、字段、唯一性与边界；通过后仍由用户确认当前完整文件及 revision，后续产品审、技术审、开发和 QA 只消费该确认 revision。纯实现位置、代码边界或未来迁移约束若不改变产品/流程行为，保留在方案正文并交由技术审、diff、代码审查或实现质量门验证，不伪装成必须 QA 的 `AC`。lightweight 沿用受理时已确认的同一需求文件，不另写需求案，也不进入 regular 的审查和 QA 链路。

### 6.2 正常 Ask

1. lightweight/regular 模式（用户尚未明确时）；lightweight 是受理阶段的初始模式选择：模式未绑定的受理 run 在需求确认后可选择它；已绑定 regular 的 run 不能后期切入。
2. 高置信拆分的精确拓扑：边界、数量、依赖、并行度；低置信不拆或不确定由 engine 自动记录 no-split 理由。
3. regular 非分片 run 与每个 child 的 full/custom 路线；UI 可批量接受统一默认，但每个 child 持久化独立决定。
4. 产品审、技术审经 Operator 验证成立的每条 P0/P1/P2/P3 finding，以稳定 ID+digest 逐项 confirm/dismiss；可同屏展示但不得只存一条全局决定。Ask 已附带唯一、完整修法时，confirm 同时批准该修法，不再重复询问；只确认问题而修法仍含用户拥有的产品/重大技术选择时，必须继续澄清并按 §7.2.1 确认完整候选的 review-delta 后才能修订。用户还可对已接纳 finding 的有效性、严重度或 adjudication 发起绑定原 result/finding digest 与当前 revision 的 typed correction Ask；原始结果保留，纠正只 supersede 裁决并重算有效状态。

### 6.3 条件 Ask 与用户主动事件

1. route-add/reselect：开发前可重选或追加；开发后只可追加普通门，不能新增 QA，不能静默移除既有义务。lightweight 与 retained merge master 拒绝普通 route-add。
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

### 6.5 配置易用性与 token 上限（2026-09-07 用户补充）

插件全部用户可配置项都须方便用户直接编辑和 AI 按授权修改：公开配置文件位置、字段
含义/类型/单位、默认值、合法示例、覆盖关系和生效时点，并在维护/诊断面展示生效值及
来源。两种编辑方式走同一校验和生效规则，无需改源码、workflow state、digest/receipt，
不需多处同步同一设置；派生配置由既有维护入口生成。常见错误定位到文件/字段并说明
修正方法。此约定包括既有设置与新 token cap，不把 engine 计算额度或授权记录变成配置。

阶段 4 接入项目/安装级 token cap 的读取、校验和生效值/来源持久化。已结算 engine 派发
用量达到上限，即使次数余额未耗尽也停止自动签发/refill/恢复重试并返回有限预算处置 Ask；
owner 终态报告不计入。重启不丢限制或清零用量，配置变更按公开生效规则处理。未配置或
来源明确不可计量时 token cap unavailable，次数限制仍生效；配置错误不得静默关闭 cap。

## 7. 需求变化与开发前复审

### 7.1 来源必须分离

1. `USER_INITIATED_CHANGE`：仅此来源允许产品审/技术审使用影响集增量复审。
2. `REVIEW_FINDING_FIX`：产品审或技术审确认的 P0/P1 导致的修订，必须由全新零上下文代理对对应阶段的完整最新文档进行新鲜复审；不得通过增量 carry 减免。产品修订影响技术前提时，产品复审 PASS 后再做新鲜技术审；技术修订改变产品语义时先回完整产品复审。
3. 产品审/技术审确认的 P2/P3 仍逐项修订/修复，但不因严重度本身强制复审；若修订实际形成其他语义变化，再按其真实来源和影响进入相应复审。

### 7.2 用户主动需求变化可发生于任意非终态时刻

1. `REQUEST_REQUIREMENT_CHANGE` 在 Ask、Wait、Ready、Operator、产品/技术审、路线、开发、QA/门、修复、分片、主线合并和尚未提交完成的 Seal intent 中都必须可达；SEALED/ABORTED 后的新需求创建新 run。
2. barrier 固定分两阶段。Phase A 在尚无新确认 revision/`ImpactSet` 时，立即撤销未完成 Seal intent，并停止新的权威 VCS/状态写入、候选 promotion 和业务任务签发；可安全隔离、不会提交权威副作用的在途计算可以继续到 terminal，但结果一律 quarantine，不能推进 workflow。此时没有任何任务可被猜成“已知不受影响”；暂时无 eligible task 不违反最大并行不变量。
3. 用户先重新澄清并确认完整最新版需求/方案。只有确认后，Operator 才基于旧/新完整 revision 建立 changed items、transitive impacts、affected tasks/artifacts、carry reasons 的 `ImpactSet`；任何无法证明未受影响的任务、结果、candidate 或 receipt 一律归入 affected。
4. Phase B 机械 terminate/stale 受影响或 unknown 的在途 Attempt；结果合并 ImpactSet 与 §7.2.2 的用户保留决定处理，未获保留的受影响/unknown 结果失效。对明确未受影响项恢复/接纳 quarantine 结果并携原 revision、证据和理由 carry，随后按更新后的 eligible frontier 补满容量。旧 revision 结果须有有效 carry 或用户授权继承凭据才能推进。
5. 增量产品审/技术审读取完整最新文档作上下文，但只重新判断变化项、传递影响和 carry 是否仍成立；影响无法可靠圈定或跨共享语义时扩大到全量。
6. QA 用例、路线、拆分拓扑和开发任务按同一 ImpactSet 增量更新；验证结果同时服从 §7.2.2 的用户保留决定。开发中受影响 worker 用新 Attempt；开发后形成新快照，QA scope 按持久化 typed `ReviewScopeMode` 决定：黑盒/白盒 QA 用例或测试审查首次建立基线时为 `FULL`，其后仅对新增、修改或 ImpactSet 受影响项使用 `AFFECTED`；产品审、技术审和普通质量门始终为 `FULL`。该标记随 review/candidate/结果持久化，并保留未来开放给用户配置的类型能力。
7. 用户需求变化本身不计 repair wave，也不清零已完成的 run 级 wave；被变化打断、未满足完整 expected set 的旧 revision wave 不计数。
8. 分片由 master 接收需求变化并级联：受影响 child 暂停、重建或重新验证，未受影响 child 可继承；child 不得私自背离 master。已进入主线集成阶段后，普通需求增量由主线开发 worker 实现；若变化本身改变拓扑，再重建受影响 child/map/receipt。

### 7.2.1 需求内容确认与回退授权

审查来源的修订由主代理在编辑前保留并核对与上一确认 revision 匹配的完整 base，生成穷尽 review-delta 并逐 hunk 核实来源。用户只确认变更包；完整 candidate revision 绑定为新需求，未变内容继承。首次受理仍确认全文，主动查看全文不改变增量确认范围。Controller 不持有第二份 base/diff 账本，不允许无关 hunk 混入。

需求影响由 Operator 建议，Controller 不判断自然语言含义。`meaning-preserved` 不产生回退 Ask，保留产品审 PASS，只重做实际受影响、未通过或尚未完成的技术审；正文变化仍按 review-delta 确认。`meaning-changed` 在接纳 revision 转换前必须生成绑定 state revision、base/candidate revision、影响预览 digest 的 typed Ask，展示目标/范围/对外行为/验收变化、建议失效 PASS、回退步骤、产品审已用/剩余额度和授权状态。只有用户确认事件才能原子登记新 revision，并按 §7.2.2 的保留清单处理结果：未获保留的旧产品审 PASS 置 PENDING、未获保留的受影响下游结果失效；不重置额度、不自动授权新产品审。缺失、过期或不匹配时零状态变化。一次决定明确覆盖 review-delta、回退后果与保留清单即可共用授权。阶段 4 engine 是唯一 writer，legacy 入口不得绕过校验。

### 7.2.2 用户决定保留已通过验证

用户在任意非终态修改需求时，可逐项或全部保留已有 PASS，涵盖产品/技术审、QA 用例/测试审查与执行、普通门以及 child/merge 验证；即使 ImpactSet 为 affected/unknown 或需求含义改变，明确保留决定仍优先。typed Ask 将保留清单、原结果与旧需求/候选绑定、新需求与目标候选、state revision 和确认来源一并登记，与需求修订原子应用；未选项沿用既有影响规则。

保留结果继续满足相应推进、覆盖、QA 和 Seal 条件；原始 PASS 与实际验证版本不改写，报告区分实际执行与用户授权继承。该次授权优先于本次需求变化触发的失效、fresh review 和 child/merge 重验，但不能把 FAIL、PENDING、未执行或新增且无既有 PASS 的义务变为通过。后续需求或候选变化重新判定，SEALED/ABORTED 不改写。

在途任务沿用 Phase A 与 quarantine，不要求用户等待全部派发完成才能修改需求；确认时尚无 PASS 的任务不在保留清单中，迟到结果不能覆盖新决定。连续修订逐次绑定。master 统一接收并级联 child/merge 保留决定，业务验证继承不代替实际提交、集成或其他未完成副作用。具体 receipt 与展示合同见最终方案 §5.2。

### 7.3 产品审/技术审 finding

start-readiness 的分类与严重度合同继承审查收敛需求 REQ-002（AC-005–008）：P0–P3 每项必填分类，只有需求实质冲突、模块职责或依赖方向根本错误、核心验收缺少执行入口三类可形成 P0/P1，实现细节最高 P2。Controller 校验分类枚举、严重度矩阵及核实记录，Operator 核实分类、正常使用影响和不能留到开发阶段处理的理由；缺失或非法时只登记 `REJECTED_INVALID_RESULT` 并终结 attempt，不写有效 finding/action 结果，仍可沿原 TaskKey 有限重试。

1. worker finding 是候选。Operator 对 P0–P3 逐项核实需求前提、正常入口、复现、证据、因果、严重度和范围；无效或范围外候选直接丢弃，不形成用户义务。
2. 每个 Ask 必须区分“问题 disposition”和“修法 authorization”。若 finding 已附唯一、完整、无用户选择空间的修法，confirm 同时绑定 finding digest 与 remedy digest；若用户只确认问题成立，或 remedy 仍有多个有意义的产品/重大技术选项，则进入需求/方案澄清，取得选择并按 §7.2.1 确认完整候选的 review-delta 后才能写入。纯机械措辞、引用或无歧义同步不制造重复 Ask。
3. P0/P1 confirm 且修法已获授权 → `REVIEW_FINDING_FIX` → 修订 → 对应阶段完整新鲜复审。dismiss 或经 correction 判为无效 → finding 作废；若没有其他未处置或已确认 P0/P1，`EffectiveReviewStatus` 直接投影为 PASS，不为驳回项另派一轮。纠正若把 PASS 变为阻塞，则按依赖图使后续结果 stale；只清除错误阻塞时立即开放下一合法节点。
4. P2/P3 confirm 且修法已获授权 → 有界修订/修复且不因级别本身复审；dismiss → 作废。已拍板 stable finding ID 在后续提示词中注入，前提未变化时不得原样重提。
5. Controller 按 master/整体 run 和 action kind 分别持久化 `product-review`、`start-readiness` 两条 `PreDevelopmentReviewSeries`。每条默认允许三个 completed semantic rounds；只有 Operator 接纳的有效完整审查计一轮，finding 后续 confirm/dismiss/严重度纠正或 effective PASS/FAIL 变化不新增、删除或重复该轮，只有整轮被 correction 判定为 invalid/incomplete/stale/错误绑定时才从 completed 投影排除。RUNTIME_ERROR、invalid/incomplete/interrupted/stale result 不计。前三轮是 workflow policy 预授权的 fresh-review 上限，只在 finding 处置确需复审或用户按 §8.4 明确授权重审时，按当前 revision/round 生成 distinct `TaskKey`，不预先派满。需求/方案 revision、ImpactSet invalidation、carry、adopt-external 和普通失效不重置 series；显式 reset 或新 run 才重建。达到上限后 typed Ask 每次只向对应 action 的 expected set 增加一个带 request ID/reason/revision 的 fresh-review slot；correction 不生成 TaskKey、不消费额外 review grant 或 retry attempt。split child 继承 master 整体审查，不另建 series；`show/status/next` 分动作公开 completed/base/extra/effective/remaining、raw/adjudication/correction/effective 状态与授权来源。

## 8. 拆分、并行、开发后审查与修复

### 8.1 拆分与 child 生命周期

1. `start` 不声明、不冻结拆分意向；拆分绑定唯一发生在 start-readiness PASS 后的拓扑确认：高置信要拆时 Ask 精确拓扑（边界、数量、依赖、并行度），低置信不拆或不确定由 engine 自动记录 no-split 理由。拓扑确认即绑定点——确认前用户可改变意向（无需需求变化、reset 或 abort），确认后不得重切，变更走用户需求变化（ImpactSet 级联）、reset/rebuild 或 abort。
2. master 确认拓扑后，Controller 固定执行：ENGINE_LOCAL 持久化 child-creation intent、分配 ID、创建 PREPARING child state 并注入 bindings → 每个 child 的 VCS adapter 独立 StepSpec 创建/对账 workspace/client → ENGINE 校验全部 typed receipts/identity → 原子提交 `sliceID → childRunID` map 并解锁 child。不得让本地 handler 越过 adapter 创建 VCS workspace，也不得把顺序交给宿主猜。
3. child 继承整体产品/技术审与精确拓扑，各自持久化路线；child 达标后进入可恢复的 `SLICE_READY` checkpoint，不作为独立最终 SEALED，也不提前删除恢复信息。
4. durable `SliceTerminalReceipt` 至少含状态、child/slice/master ID、版本、VCS snapshot/tree/digest、需求/拓扑/路线 bindings、QA/门结果、批准用例、成本和最后 request/event digest。master 只按期望 map 接纳 receipt。
5. 合并 QA 跨片用例的设计和审查与各 slice 开发并行。master 收齐 receipts 后由 engine VCS adapter 按固定 topology 顺序完成 Git/SVN/P4 集成，并在主线 snapshot 前物化已批准的 merge cases/其他 VCS 内最终 artifact；只有实际内容冲突需要语义判断时才派主线开发 worker，且它只编辑冲突内容，后续 resolved 校验、resolve/add、提交和 snapshot 仍由 adapter 执行。
6. 合并后始终并行执行合并 QA 与合并门。冲突处置或主线返修后，Controller 以 topology ownership、VCS diff 和每个 child receipt 的 validation view 机械计算 `AffectedChildSet`：若只改 integration-owned glue/跨片接缝且所有 child view 精确相等，只重跑合并 QA/合并门；若改变某个 child-owned 内容或无法证明其 view 未变，则在同一最终主线候选上额外补跑该 child 受影响的黑盒、白盒和普通门义务。仍不回 child、不重新 Seal child、不重复路线或澄清；所有当前 eligible 验证最大并行。Operator 核实完成后处置仍不唯一时才生成 `VALIDATION_DETAIL_DISPOSITION` Ask；处置完成后，`wave >= 3` 才生成三轮统一 Ask。
7. child abort 产生 ABORTED receipt 并阻塞 master；master abort/reset/重大需求变化按已授权策略级联，不得遗留 orphan child。master 最终 Seal 汇总 child cases/cost 并清理 checkpoint。

### 8.2 强制并行不变量

1. `expectedTasks` 计算完整 eligible frontier；canonical Plan 必须包含所有依赖已满足的任务。
2. 若可用容量为 C、eligible task 为 N，`SelectIssued` 必须按固定顺序签发恰好 `min(C,N)` 个任务；不得留空容量、选择性忽略或把并行组退化为主代理决定的串行。
3. 主代理必须搬运整个 IssuedSet，不能挑子集。任一 receipt/result 释放容量后，`submit` 立即推进并补满下一批，无需主代理另想起调用。
4. 一个并行支路 FAIL/RUNTIME_ERROR 不得停止仍独立 eligible 的其他支路；只有依赖或 snapshot 已失效时才能阻断。
5. adapter 若因无可靠 correlation 必须串行化启动握手，只能限制未 attach 的 start；已 ACK worker 继续并行，engine 仍须填充其他兼容容量并给出可观察 WaitReason。
6. 开发期并行组含开发 worker 与黑盒 QA 设计/审查，分片另含 merge QA 设计/审查。开发快照 Dn（S1）只解锁白盒设计；有测试变化时提交形成 Qn（S2），QA Review 和已采纳建议引起的测试变化继续提交，无测试变化不为新增交付物推进快照。最终提交整理（含必要 squash）完成、全部 mode 最终 case-set 为 FROZEN、版本绑定最终含测试快照后才 review-ready，再一次冻结 expected-set/digest 并准备全部 eligible QA execution 与门并行派发。
7. 最大并行仅适用于依赖已满足的任务；review-ready 屏障之前不签发 QA execution 或门，不以 provisional 结果替代最终快照验证。修复与 merge 遵循同一规则。

### 8.3 白盒隔离候选、统一验证与提升

1. 每次开发或生产代码返修先冻结只含当轮实现的不可变 `DevelopmentSnapshot Dn`。选择白盒时，Controller 令 VCS adapter 从精确 `Dn` 创建并登记独立的 whitebox authoring workspace（Git worktree/临时分支、SVN working copy/候选路径、P4 client/workspace/候选分支）；白盒设计代理可新增、修改或删除由 `TestOwnershipResolver` 证明为 test-owned 的结构测试、测试辅助文件、fixture 和对应 case 内容，返回 manifest，不执行任何 VCS 写操作。
2. `TestOwnershipResolver` 由项目/测试框架配置、目录/命名规则、测试发现结果及生产构建输入共同程序化判定；文件名包含 `test` 既非必要也非充分条件。VCS adapter 提供 `Dn → Qn` 每个 added/modified/deleted/renamed 路径及内容摘要。只有全部差异均为 test-owned，且生产代码、运行配置、生产依赖、构建输入和 provider 元数据投影完全相等，才签发 test-only/equal-production-view 证明；任何 UNKNOWN 一律按生产 view 受影响处理。
3. Controller 校验 manifest、测试引用、改动范围和 postcondition 后，由 adapter 程序化 track/add/open、commit/submit 并冻结包含 `Dn + 白盒测试 + 所有需纳入 VCS 的已批准最终 artifact` 的原生不可变 `ValidationCandidate Qn`。未选择白盒时 `Qn = Dn + 需物化的最终 artifact`。所有交付路径必须在 `Qn` 中，不能留在主工作树或临时 workspace 里等待 Seal 临时捞取。
4. 白盒 qa-review 读取完整 `Qn` 并独占测试代码质量、用例与实现引用的审查责任；实现质量门不审 test-owned 文件。FAIL 时回白盒设计形成新的 `Qn+1`，不计开发后 wave。若白盒 agent 需要改变生产代码或无法证明 test-only，该改变必须转交开发/repair 路径形成新 `Dn`，不能伪装成白盒测试增量。
5. 黑盒与门只在最终 review-ready 快照签发；门比较基线到最终候选的完整 diff，不接收 QA 用例正文、逐用例证据或测试实现细节。实现质量门排除 test-owned 文件，测试质量归白盒 QA Review。
6. 同一 wave 的 expected-set/digest 在最终 Qn 冻结，包含全部所选 QA execution 与门；未收齐、存在 PENDING/在途或运行错误未明确处置时为 COLLECTING，不得返修。route/case-set/目标集变化使旧 wave 失效，不能中途修改集合制造收齐。晚到结果仅留审计，不重开 wave；显式错误处置不伪装成 PASS。
7. 普通实现返修保持 QA FROZEN，只装配新实现候选并按固定 AFFECTED 子集执行。只有需求含义、对外行为、成功/失败判据或稳定可执行性变化才允许 THAW；环境暂不可用记 RUNTIME_ERROR。合法 QA artifact 修订使旧 Qn 失效，由独立设计/审查链提交实际测试变化（含采纳建议引起的变化），全部冻结后形成新 review-ready Qn+1，至少重跑修改测试与共享依赖影响项。child 的 SLICE_READY receipt 绑定最终含测试候选，master 不从遗留 workspace 补捞。
8. 完整 wave 无 obligation 后，adapter 才把整个 `Qn` 提升到主线，不得在末尾重新播放一份未经验证的测试补丁。优先保持同一原生 identity；若 Git/SVN/P4 产生新的最终 identity `F`，只有 `CandidatePromotionReceipt` 机械证明 `validatedDigest(Qn) == finalDigest(F)`（含 provider 特有的 mode/property/filetype/view 语义）时，整组证据才可映射到 `F`。promotion 出现内容冲突、转换、目标漂移、UNKNOWN 或任何 digest 不等价时不得 Seal；先 observe/reconcile，必要时把实际 `F` 冻结为新候选并按真实影响重跑验证。

### 8.4 发现项、QA scope 与三轮

1. 黑盒、白盒和 merge QA Review 共用 PreWaveReviewPolicy 与逐项 approved whitelist；P1 修改后 fresh review。确属可选的 P2/P3 按稳定 suggestion ID 记录采纳、延期或不采纳及理由，不强制改用例；全部处置且确定性覆盖通过才 FROZEN，无建议则 PASS 后立即冻结。采纳改变共同路径、可执行性或验收含义时重审受影响部分；真实覆盖缺口、错误 oracle 和不可执行步骤不能靠建议处置获批。首次 FULL，合法增量 AFFECTED；普通代码返修不重新设计。既有 pre-wave 语义轮数上限保留，与运行错误 attempt、开发后 wave 分开计数。此规则不替代门审 P2/P3 或产品/技术审处置规则。
QA 覆盖先把同一次需求/方案确认中登记的有限 `requiredSources` 与当前 revision、route/topology scope 一起冻结，再生成有限验收点集合 `AcceptanceManifest`，并用 source↔point↔case 映射建立双向追踪。标准需求案适配器把每个 `REQ` 投影为 `RequiredSource(PRODUCT_REQUIREMENT, QA)`、每个 `AC` 投影为 `AcceptancePoint`、已设计 QA case 投影为 `AcceptanceCase`，关系只由 `CoverageEdge` 表达；标准需求案中的全部 `AC` 都必须有 approved case，不允许以“非 QA 验证处置”绕过，其他合法 source 才可按自身协议使用显式非 QA 处置。review 必须显式返回完整 `SourceDecisions`、`PointDecisions`、`CaseDecisions` 和未绑定集合，不得由 case PASS 自动推导或补写 source/point PASS；三层全部逐项 PASS 后才能生成 whitelist，仅有集合级 PASS 不足以通过。execution 冻结 expected case ID set，并列出实际执行、合法继承和未执行条目；`FULL`/`AFFECTED` 都必须完整对账。source inventory/manifest/map/whitelist digest 与当前 `ValidationCandidate` 精确绑定，摘要展开为 `sourceID → pointID → caseID → result`，`AUTHORIZED_SKIP` 不等同 `EXECUTED_PASS`。Controller/validator 负责结构校验；覆盖率、mutation、property-based test 和 fuzz 只作辅助信号，不设固定阈值。
2. 开发后 finding 由 Operator semantic adjudication 按需求、正常失败场景、模块、根因、后果逐条核实，保存 raw finding、独立 severity rationale、范围/严重度/因果/复现证据、稳定 finding/problem ID、NEW/SAME_AS/DISMISSED 裁决及历史。Controller 只校验完整性、引用和版本绑定、机械路由，不自动判断语义真假、严重度、根因或重复关系。P1 理由须说明已确认需求、正常路径、实际后果及为何 P2 不足；P0 还须证明需求无法成立、核心验收无可用路径或交付受阻。条目 FAIL 不自动成为 P0/P1，整批套话不能替代逐条理由。
3. 仅已核实 finding 的处置仍不唯一才 Ask；核实本身属于 Operator。pending disposition 不形成返修 obligation。QA artifact 修订须满足 THAW 条件并重新冻结候选；需求变化遵循 §7.2.1 授权，DISMISSED 只留历史。
4. wave 收齐且 adjudication 完成后，QA FAIL、有效 P0/P1 与门审 P2 归入同一快照唯一 RepairObligation/统一返修计划。同根因归并 canonical problem，同一快照不得第二次逻辑返修。门审 P2 必修但不复审、不增加审查轮次，纯 P2 以完整构建、全量测试、受影响项逐一自跑及结构化完成/carry 凭据继承 gate PASS；门审 P3 不返修，只在 Seal 前一次性 advisory，范围外 P3 同样不进入 obligation。
5. 首次完整 review-ready expected-set 终结计 wave 1，后续需复审的修复快照各计一轮。装配/pre-wave review、PENDING、未处置错误、缺回执、未完成裁决、需求变化打断的 wave 不计；纯门审 P2 修复不计轮，既有三轮及额外授权上限保留。
6. 普通返修固定 AFFECTED 执行冻结用例；本轮必需 QA 与门全部有效 PASS（含合法继承）即可 Seal，不因 requirement guarantee 追加封板前 FULL；首次必要 FULL、新增/受影响义务及来源绑定仍保留，FAIL、PENDING、未完成或无效结果仍阻止推进。需要重审的产品/技术/普通门读取完整绑定输入；门审 P2 用 carry，不复审。
7. 额度用尽后额外返修 Ask 展示完整绑定摘要：state revision、需求/快照/wave、全部 findings/problems（归并与定级理由）、历史修复/验证、QA scope、已用/剩余轮数、dispatch signature/TaskKey、engine allowance、owner cost、token cap 可用性。预览只读；授权绑定摘要 digest 与 state revision，变化后拒绝旧授权，每次只授权一轮。
8. 非 wave 与 wave 的 action/QA/gate 共用无效结果义务生命周期：RUNTIME_ERROR/REJECTED_INVALID_RESULT 终结原 attempt，后者保存被拒字段/原因，不写有效 finding/PASS/FAIL/problem/basis。义务有效时复用 TaskKey 按有限 MaxAttempts 重派，默认 2；wave 还须 COLLECTING 且目标未处置。耗尽进入 RETRY_EXHAUSTED，展示原因、已用/总 attempts，停止普通与自动重派。用户绑定 state revision 和非空理由只能追加一次单 attempt，改变 retry policy/digest、不增加基础槽位；再次失败不得扩张，须按已有错误处置、abort/cleanup 或确认真实改变绑定/快照/责任的新计划结束或重建。
9. dispatch signature 绑定具体输入，TaskKey 绑定逻辑义务；相同 OPEN 派发续用，相同有效 PASS signature 不自动重复派发。用户明确要求重审时，经绑定当前输入的 typed Ask/submit 登记非空理由及 override 授权，保留原最终结果；有自动额度时正常消耗，耗尽时先取得适用的额外审查授权，需要额外返修仍绑定最新摘要。授权重审生成相应新审查义务，不重置或绕过语义轮次、repair wave 与 retry 额度；普通 finding/result/receipt/resume 不新增 TaskKey。阶段 5–7 继承同一 finding/problem ID、归并、唯一返修、P3 advisory 和 QA 冻结语义。
10. 归并与纠正继承审查收敛需求 REQ-004（AC-013–017）：与 RESOLVED problem 归并的 finding 不再触发返修，与 DISMISSED finding 相同的新报告只留审计；根因或实际后果不同可建新问题，较晚 wave 新出现的有效 P0/P1 标记前轮漏审。错误归并可通过绑定 revision 的新裁决纠正，保留原 finding/problem/裁决历史；返修派发前更新同一逻辑计划，派发后新产生的 OPEN 义务进入返修所得新快照的下一 wave，不能为原快照创建第二份返修。

### 8.4.1 需求覆盖保证的激活与分片责任

阶段 4 在同一 State 中接管持久化 `RequirementGuaranteeActivation`、来源与完整需求投影；只有单一正式需求文件经过结构预检、用户确认、投影冻结，并选择 full 或至少一个 QA 类型的 custom，才能原子激活保证。不得从路线、artifact 数量或文件形状临时猜测 activation；额外 requirement artifact 在保证模式 QA 设计前拒绝。custom 无 QA、lightweight 和显式 waiver 保留各自非保证状态，不能输出完整闭环 PASS。多个 QA 类型以覆盖并集判定，标准需求案的全部 AC 必须进入批准用例与最终候选执行闭环，逐 REQ/AC 展示绑定、审查、执行与保证状态。

阶段 5 由 retained master 持有完整投影，在切片 QA 设计前冻结每个 AC 恰有一个主责任范围（child 或 master merge）的分配；遗漏、重复、未知或 revision 不一致均拒绝。child 只声明自身责任完成，receipt 保存批准用例及需求/类型/AC 绑定；用例以 scope + QA 类型 + case ID 唯一标识。任一责任 child 选择 custom 无 QA 或 waived 时，整体明确 not-guaranteed。保证启用时，master 汇总去重全部 child/merge 批准用例，逐 AC 汇总合并候选上的完整有效结果；首次必要 FULL 保留，不能遗漏 child/merge 的新增或受影响义务，本轮必需验证全部有效 PASS 后不追加最终 FULL。普通返修仍 AFFECTED，用户明确保留既有 PASS 的例外按 §7.2.2 处理，不能伪称新候选实跑。

### 8.4.2 3.5c QA 复用、进度与 Codex 计量

共同准备/操作说明以稳定 shared-step 引用保存并审查一次，每个 case 仍有独立 oracle、差异步骤和覆盖映射；共同说明变化使全部引用者审批失效，单条 case 变化只影响相关条目。白盒先独立列出行为、分支和 oracle，再核对已有测试的输入及真实断言，有效测试绑定复用并独立执行，只为缺口或错误修改测试；无测试变化不要求新文件或新快照。黑盒设计/审查仍隔离于本次实现、白盒测试及开发者解释。

同一 QA 派发可复用候选构建和非验收目标的准备步骤，独立用例使用干净副本，有序依赖显式声明；失败只停止依赖操作，逐例保留真实命令与输出。构建、安装、启动或宿主交互本身属于验收时仍须本轮实跑；前置状态可构造，目标成功结果不能预造。短执行脚本沿用现有工具，新增/修改的关键判据用已知错误样例核验，同版本未变脚本不重复完整审批。

同次执行进度存入现有结果/Attempt 机制：完整 PASS/FAIL、证据、未完成项、中断原因、复用与失效理由分别保留，绑定候选、需求 revision、case/shared-step 版本、执行工具和相关环境。checkpoint 不终结 dispatch、不增加 TaskKey/attempt，完整绑定未变时只补未完成项；运行错误终结 attempt，重派沿用有限 retry。最终合并保留 FAIL，缺失/重复/未知条目拒绝，授权跳过不等于 PASS，未齐不能整体通过。

阶段 4 source bridge 复用 `internal/cost` 的单一投影，保留 Codex transcriptPath、按原生 response ID 去重的请求 token，以及 task_started 到 task_complete/turn_aborted 的 timing；包括工具等待、排除 turn 间空闲。checkpoint 不提前固定不完整派发用量，续跑完成后用完整来源结算。缺来源/完整计时边界时使用既有 unavailable/省略 timing 语义，不补零；其他 provider 不因 3.5c 被扩展。3.5c 已交付的实测记录不变成后续每阶段重复运行模型 benchmark 的要求。

### 8.5 开发分批与开发后统一门审

1. **批次计划义务**：受理阶段只确认需求与方案，不冻结批次计划。Part 2 `start-readiness` 技术审一并输出结构化 `granularity_review`，先判断单一 Batch 还是多个 Batch，再登记 `Batch → Subtask` 映射、依赖/接口/状态边界、风险、DoD/测试、回滚/恢复、交接与上下文成本及不拆/改拆理由。显式批次计划在拆分决定与路线确认时一并产生，经同一次用户决定确认并留痕，于开发开始前登记为任务清单的组织结构；允许"单批 + 理由"退化形态。批次划分依据依赖链、独立交付物/验收和风险：依赖链（消费链）必须串行；零耦合支线可并行；共享文件、接口或语义的工作面不并行。接口冻结点优先作为批边界。编号、commit 数、文件数、行数、阶段/层/角色/时间段本身不得作为大块依据。
2. **批粒度**：批是任务单元，单批一个内聚关注点——语义判据为"能用一句话说清本批 diff 即一批"；批内提交保持原子、可独立构建。批内每次编辑后执行项目声明的低成本确定性检查，批边界执行该开发计划指定的回归命令；每批终态可判——树可构建、声明的回归通过、自测留痕、任务清单逐批勾选。
3. **按具体风险安排探路**（2026-09-07 用户修订）：固定流程按插件封闭定义直接实现，宿主接入随对应功能实现与验证，不因更换宿主就自动安排独立首批 spike。确有未决接口或技术风险时，先列明疑点、受影响工作、最小实验和通过判据，再依批次判据决定是否独立成批；必要实验须先于依赖其结论的工作，不阻止无依赖工作推进。独立 spike 的结论与真实证据留痕，实验代码不进入交付。已完成阶段的历史 spike 要求和记录不追溯修改；开发驱动缺少批次入口不能作为合并批次或削减产品多批次能力的依据。
4. **审查时点**（2026-09-07 用户修订，替代 ADR-002 的可选组间增量门审）：批内自测和批边界声明的构建/回归始终执行，代码/质量门统一在本 run 全部开发 Batch 完成、最终候选满足 review-ready 后运行，不再选择或签发组间门审。批边界检查不形成正式门 PASS 或 repair wave；开发前产品/技术审、并行黑盒用例设计/审查与开发后白盒准备链路保持各自职责。split 属 run 拆分机制（见 8.1），不是验证档位。审查时点与范围、结果接纳在既有封闭定义/策略和协议中保持独立代码边界，便于以后修改规则；本阶段不预建未启用的组间流程、第二状态机或自定义工作流开关。
5. **不变量**：批次不改变 run 边界、公共入口、Seal 语义或强制并行不变量；不产生额外受理、双审或 Seal 轮次。
6. **引擎义务与机械接入**：用户确认 `granularity_review` 后，Controller 将其绑定到需求/方案 revision、拆分决定和路线，并把 Batch、Subtask、TaskKey 映射及批间依赖登记为现有任务计划的结构化信息。引擎只校验 ID 唯一、映射无遗漏/重复、依赖无环、TaskKey 归属明确，以及 DoD/验证/恢复/交接字段齐全；它不从自然语言重新判断是否应该拆分。`batch_id` 对应一次 `development-worker` 派发；`subtask_id` 只是该派发内的顺序步骤，不得单独派发或换代理；只有前一 Batch 的成员任务终态、DoD、验证和交接完成后，才解锁下一 Batch。Batch 契约、接口、依赖或 DoD 改变时，旧计划失效，必须重新进行 `granularity_review` 并取得用户确认。Batch 不是独立状态机、receipt 或生命周期，完成状态仍从成员 task 状态派生。
7. **批次代理隔离**（2026-08-23 用户补充；2026-09-07 补充插件内强制）：每个开发批次派发全新的零上下文开发代理（薄启动 + 规范文件），同一开发代理会话不跨批复用；reset 粒度为批次边界（第 2 条的内聚关注点：批内连续、批间不连续）。批次任务书只写不可从仓库恢复的信息（决策理由、已知问题/债务、下一任务），不复制可从 VCS/代码/测试推导的内容。包级 SKILL 与流程参考文档同步持有规则；插件内由 engine/adapter 在真实调用前强制。未完成同批绑定原生开发身份，正常返回、自测失败或 retry 不解除绑定；原代理可恢复则续用，仅 adapter 实际恢复/查询取得明确不可恢复事实并完成旧执行及副作用对账后，才允许同批替换。瞬时失败、未知原因及主代理理由均不能代替证据；更改任务编号、失败分类或重复计划不得绕过。复用现有 State、TaskKey/Attempt、execution、HostAction 与生命周期，不另建代理状态机。阶段 4 的具体调用前拒绝与真实正反例验收以 REQ-007 / AC-061 为准；本次不扩展到插件外直接调用 Codex 工具。

## 9. 程序化 VCS、本地副作用与恢复

1. Git/SVN/P4 的 provider 探测、根与身份解析、status/diff/untracked 识别、base/current/ancestry、workspace/client 创建、显式 track/add/open/reopen、resolve 状态验证、merge/integrate、commit/submit、snapshot、Git squash、已授权 reset/rollback、cleanup 和崩溃对账全部由 VCS adapter 程序化执行；宿主和代理不得自行拼 VCS 命令或决定其顺序。
2. 开发/修复代理只编辑已授权内容并返回声明式路径 manifest、语义说明和 typed result；Controller 校验路径、改动范围与 postcondition 后，才由 adapter 执行 track/add/commit/snapshot。确定性构建/测试可由 engine runner 执行；需要语义设计或实现的测试仍可作为 AGENT 步骤。
3. 集成固定为：adapter 发起并观察精确冲突 → 仅在需要语义选择时让主线开发代理修改冲突内容 → adapter 验证全部冲突已解决 → adapter 执行 resolve/add/open、commit/submit、snapshot 与确定性验证。VCS 异常只能按 retry、Wait、Operator 或显式失败处理，不得降级为代理猜测。
4. reviewer/QA 接收 Controller 生成并绑定 provider、base/current、definition version 和 digest 的只读 snapshot/diff artifact；不得要求 reviewer 自行推断当前 VCS 状态或执行 VCS 写操作。凭据或宿主能力边界可产生 typed HostAction，但宿主不能重排或自造命令。
5. 每类非幂等本地副作用采用 `intent → execute → observe/reconcile → commit`：start、prompt/artifact、blackbox/whitebox QA workspace、case mirror/materialize、ValidationCandidate freeze/promotion、Git/SVN/P4 child workspace 与集成、snapshot、Git squash、child receipt/cost、summary、cleanup、abort。
6. 外部事实仍是旧状态时可执行；已是精确预期状态时提交结果；冲突进入 Operator；retryable I/O 进入 Wait。绝不盲目重复 squash、spawn、submit 或集成。
7. 黑盒批准用例、白盒测试代码及其他 VCS 内最终交付物必须在 `ValidationCandidate` 冻结前物化并纳入其 identity；若明确设计为 VCS 外产物，必须有独立 digest 和 summary 绑定。Seal 不得在共同验证完成后再向候选追加交付文件。
8. Git 基线→当前超过一条提交时，在最终 `ValidationCandidate` 冻结及最后一轮必需验证之前完成 squash，消息由 engine 稳定派生，不新增用户 Ask；SVN/P4 不 squash。QA 与门结果绑定整理完成的不可变候选，Seal 检查同一 candidate identity、固化结果并物化既有流程产物，不再 squash 或改变候选身份。最后验证后候选变化仍按既有影响判定、AFFECTED 与真实新增/受影响义务处理，不凭内容相似直接继承通过。
9. Controller 持久化 typed resource registry，登记每个 blackbox/whitebox/child/merge workspace、临时 ref/branch、SVN working copy/候选路径、P4 client/pending changelist 及其 owner/identity/cleanup state。Seal/Abort 先持久化可恢复 intent/summary，再由 adapter 清理并逐项 observe；只有 `WorkspaceCleanupReceipt` 证明全部应回收资源无登记残留后才进入 Complete。promotion UNKNOWN 必须先对账，不能靠重复 merge 或静默 rollback 清理。
10. 检测到活动 run 的外部漂移时，adapter 先冻结精确 observed external identity、lineage、只读 diff 与 `ExternalAdoptionPreview`，在用户对该 identity 授权前不得把它写成 current。adopt-external 获批后保留原 run base，把该不可变外部 identity 设为新的 current/development source，并写 `ExternalAdoptionReceipt`（旧/新 identity、preview/user request/ImpactSet digest、失效与 carry 清单）。受影响或 UNKNOWN 的 Attempt/result/candidate/waiver/promotion intent/child receipt-map stale，明确未受影响项才 carry；未完成 wave 不计、已完成 run 级 wave 不清零。若外部变化改变需求 artifact 的含义，必须转入 `REQUEST_REQUIREMENT_CHANGE` 完整确认；lineage/identity 不唯一时进入 Operator/Wait/reset/abort，不能猜。分片只级联受影响 child 与 merge bindings。

## 10. 测试与验收覆盖（口径 A，从严）

阶段 4 同时验收单个操作内部的固定顺序：Git 的 manifest/范围校验 → track → commit →
snapshot，以及已声明 QA 操作的准备 → 目标操作 → 结果采集。合法路径真实执行；主代理或
子代理跳步、倒序、重复已完成步骤或在前置失败后继续时，底层操作前拒绝且调用数为零。
中断后按既有 StepSpec/intent 只恢复未完成步骤，原子 handler 的内部顺序与失败短路也须
验证；不以提示词、大阶段顺序或事后结果拒绝替代，不逐内部语句增加 TaskKey。
配置易用性、token 到顶但次数仍有余额的止损与重启保持、跨产品/技术复审的顺序/新代理，
以及一条并行任务 FAIL/RUNTIME_ERROR 后其他独立任务继续并自动补位，均由阶段 4 的公开
路径和真实首宿主验收。精确条目见阶段 4 正式需求 AC-069–076。

以下每一项都要求可复现测试/QA 证据，不能用 smoke 或口头保证代替：

阶段 4 全局切换后的发布核验还须按该阶段 REQ-022/AC-079–081，逐项检查阶段 0、1、2、3、
3.5a/fix/b/c 已交付且仍承诺提供的能力。原需求/交付来源、当前入口、用例、实际结果与发布
身份逐项绑定；迁移能力验证 engine 对应路径，已替代/延期/未验证单列，历史 PASS 不代替
新版实跑。Seal 前隔离验证，切换后通过实际全局入口复验；内部合同与破坏性安装用例使用
同一发布包的相应隔离环境。存在范围内回归阻塞或缺少必需证据时，不完成发布或启用后续
driver，按既有发布失败合同回退；不以此提前要求阶段 5/6 的新能力。

QA 覆盖契约的独立 fixtures 还必须证明：合法 source→point→case 多对多映射；标准需求案 `REQ/AC/case` 到既有 `RequiredSource/AcceptancePoint/AcceptanceCase/CoverageEdge` 的无损适配；标准需求案任一 `AC` 缺 approved case 或试图以非 QA 处置绕过时拒绝；其他 source 无适用 QA 点时的显式理由；未知/重复/缺失/orphan source/point/case、仅 case 决策、自动推导 source/point 决策和集合级 PASS 被拒绝；source inventory/manifest/map/whitelist digest 或当前 `ValidationCandidate` 不匹配时旧结果失效；`FULL` expected/actual 不一致与 `AFFECTED` 执行/继承/未执行集合不完整被拒绝；以及 `AUTHORIZED_SKIP` 不计为执行 PASS。该 checkpoint 只冻结契约与 validator，regular E2E 留在阶段 4。

1. **迁移表与用户节点**：每条合法边、pre 拒绝、trigger 分类、Ask/Operator/Wait/HostAction/Ready/Complete、主动 `REQUEST_*`、终态与回退；正常入口不存在遗漏用户节点。
2. **节点内执行计划**：封闭变体 constructor 非法状态拒绝、closed-world compiler 图校验（可达性/循环/依赖/join 覆盖/版本绑定/registry ID 完备性）、合法/非法 reason、typed I/O、可执行 pre/postcondition、固定顺序、乱序/遗漏/重复拒绝、精确 frontier、崩溃后不重放已完成前缀，以及纯原子 handler 不被过度拆分。
3. **失败分类与禁止降级**：每类 engine/business/user/UNKNOWN/invariant/semantic error 只进入声明边；engine/adapter 故障绝不动态改派 AGENT；diagnostic-only `MISSING_ENGINE_ADAPTER` 不可编译/签发并稳定进入 `BLOCKED_BUG` diagnose；最终定义扫描证明本次范围内没有该 marker。
4. **并行**：eligible N、capacity C 时恰好签发 min(N,C)，整批搬运与及时 refill；白盒最终 FROZEN case-set/review-ready 屏障之后全部 QA execution/门并行，COLLECTING 时不得返修。
5. **任务与派发**：expected set 漏派/重复、TaskKey、Attempt、旧 Attempt、result-before-receipt、UNKNOWN/lifecycle 对账、同 actionID 不重复 spawn、重复 submit 不重放 SpawnRequest；3.5b guard 对相同 definition/state/route/topology/expected-task basis 重复计算得到相同额度/digest，外部派发无显式策略时按 `MaxAttempts=2`，显式 `MaxAttempts=n` 只允许 `n-1` 次额外 retry；0/1/N 个 external TaskKey 与 `MaxAttempts=1/2/n` 的已知答案 fixtures 直接调用生产 calculation 核对精确结果，增加一个合法 distinct TaskKey 或一个有限 retry 余量只各增加一个槽位，重复 TaskKey 不增加。basis、持久结果或 action/Attempt 账目不一致时以 `BLOCKED_BUG` 零派发。没有 closed-world、已确认计划或当前 workflow policy 预授权 obligation 来源的 TaskKey 被拒绝；按现有流程要求用户确认却未绑定对应 revision/typed Ask 的 task-set 扩张被拒绝，已预授权 repair slot 不重复询问用户。预算 Ask 的继续只解锁一个按既有 admission capacity 裁剪的 canonical Ready batch，重试只增加当前逻辑动作的一个 retry slot，重复 submit 不重复增加，host/主代理/用户提交底层额度字段被拒绝。
6. **需求变化**：`USER_INITIATED_CHANGE` 在每个非终态 phase 可达；Phase A 全局权威写屏障与 quarantine、确认后 ImpactSet、unknown=affected、Phase B 精确恢复/carry/refill、split cascade、pre-Seal 取消；与 `REVIEW_FINDING_FIX` 完整新鲜复审不可混淆。
7. **开发前发现项**：P0–P3 验证、逐 ID settle、finding/remedy 双绑定、raw candidate/adjudication/correction/effective status 分层、错误 finding/严重度/裁决的 typed 纠正、原始证据保留与 supersede、全部 blocker 驳回后无空审查直接 PASS、纠正的 CAS/freshness/幂等与下游精确 stale、多个用户选项时重新确认完整文本、P0/P1 完整新鲜复审、P2/P3 修复不因级别复审、dismiss、混合严重度和前提变化；产品/技术两个 `PreDevelopmentReviewSeries` 各自前三轮自动、只有有效完整审查计数、finding 后续裁决不重复/回退计轮且整轮 invalid 才排除、revision/invalidation 不清零、上限后每次只授权一个 action-specific fresh-review TaskKey、correction 不生成 TaskKey 或消耗授权、split child 不另开 series，并与同 TaskKey 的 `MaxAttempts` retry 分离。
8. **开发后三轮**：验证 §8.3–8.4 的最终候选、逐条 Operator adjudication/P0/P1 rationale、稳定 finding/problem ID 与历史、wave 收集/唯一返修、门审 P2 carry 与 P3 advisory、QA Review 可选 P2/P3 理由处置/FROZEN/THAW、普通返修 AFFECTED/全部必需验证有效 PASS 后直接 Seal、完整授权摘要，以及非 wave/wave 无效结果、RETRY_EXHAUSTED 和一次性追加 attempt。
9. **分片**：拓扑绑定唯一时点（start 无拆分意向声明；start-readiness 后确认、确认前可改、确认后不重切）、topology、child creation/map、继承、逐 child 路线、包含白盒测试的 child 最终 identity、`SLICE_READY`、durable receipts/cases/cost、master 等待、adapter 集成与语义冲突窄代理边界、合并 QA 设计/审查并行、主线 repair 的 AffectedChildSet 与精确补验、级联终态。
10. **错误与恢复**：invalid event、retryable I/O、fatal corruption、deadline 重入、provider/bridge/mismatch、HostAction、adopt-external 完整转换、候选 freeze/reuse/promotion/cleanup 与所有本地副作用崩溃窗口；promotion 后 receipt 前恢复不得重复集成。
11. **旧 run 与旧入口**：缺版本和任一版本不匹配稳定 `UNSUPPORTED_RUN_VERSION`；`diagnose` 原始只读；无迁移、legacy 续跑、handoff、旧公开命令、兼容别名或 cleanup 删除后门。
12. **VCS 程序化契约与 3×2**：三种 provider 的探测/status/diff/track/integrate/commit/snapshot、whitebox workspace、完整候选 freeze、identity-preserving 或 digest-equivalent promotion、resource registry cleanup、冲突窄代理边界、typed artifact、UNKNOWN reconcile 和禁止代理写 VCS；Git、SVN、P4 各跑通非分片与分片 master/child/merge，共六个权威单元格。
13. **宿主**：DSH、Claude Code、Codex、Cursor、ZCode 各自在真实环境跑一次 Git 非分片完整正式流程，覆盖产品审、技术审、开发/黑盒设计并行、whitebox workspace、最终 FROZEN case-set 与 review-ready 屏障、完整候选上的 QA/门并行、精确 promotion、至少一次 FAIL→repair→重审、receipt/result、一次中断恢复、无残留 cleanup 和 Seal。
14. **最小 canary 数**：五宿主 Git 非分片与 VCS 3×2 可复用一个 Git 非分片单元格，故最低十条完整 canary；不要求五宿主×三 VCS×两种拆分的笛卡尔积。全部权威结果必须绑定同一份已删除旧运行时逻辑的最终候选 revision 和实际安装副本。
15. **黑盒边界**：文档化正常入口、常见误操作和已知恢复/崩溃窗口；对抗输入、恶意/手工状态编辑、权限/不可变文件和未支持 OS 只作 P3、不阻塞。
16. **canonical 制品与身份**：compiler 同一生成动作产出 `definitions/workflow.json` 与期望身份常量，禁止人工双写；authoring source 重新生成 checked-in 制品字节无 diff（独立于 round-trip 的 freshness CI）、任意 assembly 顺序同字节、decode→encode 字节不变、跨进程/重复构建同字节；只改 handler 实现不改 ID 与定义语义时 definition digest 不变而 package digest 变，改变 dependency/policy/reason/schema ID/handler ID/join 语义时 definition digest 必变；registry 对每个 ID 精确解析一次，缺失、重复或 kind 不匹配拒绝；packageDigest 执行绑定（含 runtime sibling 实际摘要校验与 adopt receipt 路径）有正反向测试。

17. **保证模式迁移**：覆盖显式 activation/原子冻结、单文件及额外 artifact 拒绝、full/custom+QA 多类型并集、custom 无 QA/lightweight/waiver 非保证展示、逐 REQ/AC 结果；分片 AC 唯一主责任、receipt 全局 case 身份、合并完整用例义务，首次必要 FULL 与普通 AFFECTED 保留，不因保证激活追加最终 FULL。用户保留按 §7.2.2 另验全部/部分/无保留、affected/unknown、连续修订和迟到结果，不改写原证据或把 FAIL/未执行项变 PASS。
18. **3.5c 迁移**：共享说明单次审批与引用失效、逐 case 独立 oracle、可选建议三种理由处置与真实缺口拒绝、白盒有效测试复用/错误断言识别/无变化不推进快照、批量隔离与真实验收操作、checkpoint 保存完整 PASS/FAIL 和续跑绑定/最终对账；Codex 原生请求去重、task timing、checkpoint 不提前固定成本、缺失来源语义均从 engine 公开路径验证。
19. **执行前准入**：按最终方案 §9.1.1/§12.1 第 14 项，从真实宿主入口证明未签发、越界、取消/过期和重复消费请求零调用，合法 AFFECTED/真实 FULL 义务/一次用户例外/resume 可实际执行；能力缺失明确拒绝。RUNTIME_ERROR 获准重试后能完整登记结果，保留旧错误与既有 PASS/FAIL，不以 Seal 跳过替代正常恢复。
20. **审查治理迁移**：阶段 4 从 engine 公开路径验证 §7.3 的全严重度分类必填、实现细节 P0/P1 拒绝及合法三类阻塞；§8.4 的 RESOLVED/DISMISSED 不重复返修、错误归并在派发前后进入正确计划/wave；相同 PASS 自动去重、明确重审的理由/授权/额度校验和原结果保留。缺少合法分类时不得写有效结果；未获重审授权或额度不足时不得签发重审，阶段 5–7 继承相同语义。

## 11. 验收判定

1. 方案稿全部确定性闸门、节点内执行计划顺序/失败分类、强制并行不变量和用户节点覆盖测试通过。
2. 六个 VCS×split 单元格、五宿主 same-host canary、独立 QA 全部 PASS；每项有可复现事件、状态、snapshot 和最终 Seal/receipt 证据。
3. QA FAIL、有效 P0/P1 与门审 P2 obligation 按规则闭环；门审 P3 仅 Seal advisory，QA Review 可选 P2/P3 在 FROZEN 前完成理由处置，范围外项不阻塞。
4. 最终候选不存在旧 run 兼容、迁移、legacy mode、旧公开推进入口、第二写入口、cleanup 后门或 engine→legacy 回退。
5. 正常流程中不存在本可程序化却交给代理的操作，也不存在 engine/VCS adapter 故障后动态降级给代理的路径。
6. 阶段 4 按其范围完成 Seal/集成/必要重验后先发布全局；本节最终全部能力则必须在阶段 7 final-release Seal 与全部权威 canary 绑定同一最终候选后，才从阶段 4 全局版本升级并退役阶段 4 固定驱动包。
7. 删除或改写仍断言已废弃受理/复审/轻量路线文本的旧测试；最终 `go test ./...` 必须在不恢复旧逻辑、旧文案或兼容入口的前提下 PASS。当前基线中这类旧断言的失败属于待开发清理事实，不得以回填过时行为消绿。

## 12. 非目标

1. headless adapter、随机 chaos、大规模 benchmark。
2. 事件溯源、SQLite、watchdog、OS 定时器、repo 级集成队列/分片合流 CAS。
3. 本次承诺 Windows/macOS/Linux 适配矩阵，或把不支持平台升级成阻塞项。
4. 对抗性输入、恶意或手工状态编辑的绝对防御。
5. 旧 run 兼容、状态迁移、旧命令续跑或双权威回退。

## 13. 用户已确认的决策记录

- 用户主动提出 formal-gates 后不重复询问是否进入；插件只非阻塞提醒一次或复杂需求时两次。
- 完整需求/方案仍须单独确认；未明确的模式在需求确认后绑定，lightweight 是受理阶段的初始模式选择，full/custom 在拆分决定后确认。
- 所有用户选择能力保留为 typed Ask/available action，经唯一 `submit` 入口落账。
- 产品/技术审 P0/P1 修订做完整新鲜复审；P2/P3 逐项询问并修复，但不因级别本身复审。
- 产品审和技术审各自最多自动完成三轮语义审查；只有有效 PASS/FAIL 计轮，revision 变化不清零。仍需第 4 轮时由 typed Ask 逐动作、逐轮授权；授权增加一个 fresh-review TaskKey，不增加该任务的 retry 次数，split child 不另开额度。
- 只有用户主动需求变化使用影响集增量复审，且可发生在任意非终态时刻；不计也不重置 run 级 wave。
- 开发后 Operator 核实完成且 wave 收齐后，QA FAIL、有效 P0/P1 和门审 P2 只形成唯一返修；门审 P2 carry 不复审，门审 P3 只作 Seal advisory。仅已核实 finding 的处置仍不唯一才 Ask；额外返修绑定完整摘要。
- 用户选择合法 THAW 后修 QA artifact 必须形成新候选；QA Review 可选 P2/P3 按理由处置，采纳改变共同路径/可执行性/验收含义时重审受影响部分，其余适用修改 fresh review，改过的测试至少重跑自身，共享 helper/fixture/harness/config 变化时扩大到所有依赖测试。未受影响结果只有经精确 view receipt 才沿用，不能回旧候选 join；普通、child 与 merge 主线相同。
- QA review/execution scope follows persisted typed `ReviewScopeMode`: first baseline FULL, then AFFECTED only for new, modified, or ImpactSet-affected items; product review, technical review, and ordinary quality gates remain FULL. 长期 RUNTIME_ERROR 可选择再试 QA。
- 并行由引擎最大化强制执行，不允许主代理选择性忽略可派任务。
- 白盒独立判断覆盖、审查并运行有效已有测试，仅缺口或错误补测，有测试修改才提交；最终 case-set FROZEN 且绑定最终含测试快照后，全部 QA execution 与门一次准备并行。普通返修不解冻 QA；实现质量门不审测试，测试质量归白盒 Review。
- Seal/Abort 必须由 adapter 清理全部登记的 Git/SVN/P4 临时 workspace/client/ref/changelist，核验无残留后才 Complete。
- 合并 QA/合并门失败只派主线开发 worker 修复；只改 integration-owned 内容时仍只重跑合并双验证，若改到 child-owned 内容则在最终主线候选补跑该 child 受影响义务，不回 child。
- Git/SVN/P4 的分片和非分片都支持并验收；OS 不作本次承诺，只保持实现通用性。
- 节点内部已知顺序也必须由 Controller 机械钉死；采用业界常见的 durable workflow/activity 边界，只在需要独立恢复、副作用、并行或审计时拆持久 StepSpec，不机械拆碎纯原子计算。
- 所有可程序化动作都由 ENGINE 决定；代理仅负责语义判断、创造性实现和独立审查，VCS 写操作与集成顺序全部由 Git/SVN/P4 adapter 执行，且引擎失败不得动态降级给代理。
- 旧 run/旧命令不兼容、不迁移、不续跑；只有 raw diagnose，失败后以当前版本新建。
- 历史决定：当时的稳定安装仅保障 `orch-engine-003` 开发完成，最终候选隔离验证并在 Seal/全部 canary PASS 后安装。当前驱动与发布时间已由 §4 第 5 项的 2026-09-07 分阶段驱动/发布修订替代。
- 步骤模型采用 typed Go authoring（封闭变体 + constructor + 显式表）→ 小型 closed-world compiler → 单一 canonical encoder → compiler 生成 `definitions/workflow.json` 与身份常量的形态（ADR-001）；拒绝扁平万能 StepSpec、通用 DSL/schema 解释层、完全删除定义编译器与定义制品，以及为实现对照建两套架构。
- HandlerID 只在执行合同或恢复兼容边界变化时晋升，合同兼容的实现变化保持 ID、由 PackageDigest 标识；executable definition 与二进制 registry 锁步激活；packageDigest 是 owning-runtime 执行绑定而非审计附注。
