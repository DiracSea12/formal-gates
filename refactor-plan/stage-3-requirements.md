# 阶段 3：最小纵向 engine 闭环——阶段需求

状态：沿用已确认的重构总需求与增量阶段计划；本文件是阶段 3 formal-gates run 的阶段化需求入口。

> 本文的“阶段 3”专指 `incremental-seal-plan.md` 的“最小纵向 engine 闭环”，不是
> `final-implementation-draft.md` §11 中同名的“完整流程迁移”阶段；后者仍属于后续交付。

## 权威来源与边界

- 阶段切片、环境模型和退出条件以 `refactor-plan/incremental-seal-plan.md` 第 2、3 节为准。
- 公共循环、NextResult、runtime 选择、版本屏障和终结清理以
  `refactor-plan/final-implementation-draft.md` §1–§3、§9–§10 为准。
- 总体行为契约仍由 `openspec/changes/orchestration-pipeline-engine/master-requirements.md`
  持有；阶段 2 已封板的持久协议与恢复内核是本阶段输入，不在本阶段重做。
- ADR-001（typed authoring + 编译式 canonical 制品）与 ADR-002（开发分批与验证分层）继续有效。

## 目标

在不改变固定 stable driver 的 legacy 公开语义、且不产生第二个 workflow 写入权威的前提下，
交付第一条可独立验证、可 Seal 的 engine 端到端路径：engine lightweight run 从 start 经需求
登记、内部终结清理到 Complete；公开 façade 按 run envelope 选择整条 engine 或 legacy runtime，
同一 run 不跨 runtime。

## 范围与验收

1. 实现候选 engine 的 `workflow start`、`drive`、`submit`、`show`、`status`、`next`、
   `diagnose` 路由；`start` 是唯一 bootstrap 写入口，`submit` 是 start 后唯一外部事件写入口。
   runtime 选择不新增用户参数：候选 façade 从当前已安装并通过 admission 的 registry/launcher
   记录解析 target identity/package digest；用户不得通过 `--engine`、手工 identity/digest
   参数选择 runtime。缺失、冲突或未登记的 admission 在首次写入前返回
   `UNREGISTERED_INSTALL`。候选 installed target 的 `start` 固定创建 engine envelope，stable
   driver target 的 `start` 保持 legacy，envelope 写入 owning runtime/package identity 供后续
   façade 固定选择。
   宿主必须在 start 前完成需求与方案确认，并把受理阶段生成的 typed
   `intakeConfirmationReceipt` 作为 `StartRequest` 的必填绑定输入；该回执包含确认来源、
   `requirementSource`、`requirementRevision`、完整登记工件集 `(path, revision)` 及其
   `solutionRevision/solutionDigest`。`start` 校验回执后，将这些字段与 `definitionSource`、
   `definitionDigest`、owning runtime 的 `packageDigest` 和 `installedTargetIdentity` 一并写入
   envelope；按路径排序后的 canonical 记录生成 `intakeDigest`，不得再提交第二次需求确认事件。
   回执的 authority/transport 固定如下：阶段 0 冻结的 stable driver 仍只写既有的确认状态；固定
   launcher 从该已确认 formal run 状态和登记集工件派生不可变的 `intakeConfirmationReceipt.json`，
   并把登记集中的当前 requirement/solution 工件按原相对路径注入候选 test project；候选启动唯一
   通过 `--intake-receipt <path>`（或等价的固定 host config 指针）读取该回执，不自行生成回执，也
   不接受 runtime、identity 或 digest 参数。候选 `start`
   以 test project 中当前注入工件重算 `(path, revision)`、solution binding 和完整登记集后与回执
   精确比较；旧回执配当前工件、缺失回执或任一 binding 不一致均在首写前返回
   `INVALID_INTAKE_CONFIRMATION`，零写入。确认状态读取、回执派生、工件注入和候选启动由固定
   launcher 串接，候选不得读取 stable run state 或驱动正式 run；stable driver 不因本阶段新增
   writer 或公共入口。
2. 跑通 lightweight 的“start 前已确认 intake → `start` → 首次 `drive` 自动登记 intake
   receipt/digest → engine 自动终结清理 → `Complete`”，并在 terminal summary 标记
   `unverified=true`。对 engine run 调用旧的
   `workflow requirement --confirmed` 或公开 `workflow cleanup` 必须明确拒绝且零写入；这两条
   旧命令只在 stable legacy 回归中验证其原有语义，不能操作 engine run。
3. 在 engine protocol 的 whitebox/test-only sequence harness 中覆盖 `Ask`、`Ready`、
   `HostAction`、`Wait`、`Operator`、`Complete` 六类 `NextResult` 外部边界，每次事件接纳后
   继续确定性决策并返回下一边界。阶段 3 的 installed-candidate 黑盒只验证 lightweight 可公开
   到达的 `Complete`、façade 读写、旧入口/版本/身份负向和 legacy 回归；regular 才能建立的
   其余五类公开路径留到阶段 4，不得为本阶段新增 regular 或公开 fixture。
4. engine loader 在任何写入前精确校验 `workflowDefinitionVersion`、`stateSchemaVersion`、
   `definitionSource`、`definitionDigest`、owning-runtime `packageDigest` 和
   `installedTargetIdentity`；缺失或不匹配稳定返回 `UNSUPPORTED_RUN_VERSION`，仅
   `diagnose` 可使用最小 raw/envelope parser 只读报告。`start` 同时拒绝缺失或与当前已确认
   requirement/solution 的 source、revision、digest、完整 artifact 集不一致的
   `intakeConfirmationReceipt`，返回稳定的确认绑定错误且零写入；旧回执复用的负向场景通过
   stable launcher 注入当前工件、显式传入旧回执路径复现。本阶段不引入 wall-clock
   expiry；“失效”只表示上述 binding 不一致，`ConfirmedAt` 等时间字段不改变有效性。
5. 由固定 stable driver 驱动本次 formal-gates run；candidate 只能在隔离安装目录、test project、
   host config、state/resource namespace 中由实际 installed binary 验证，不能驱动自己的正式
   run 或 Seal。验证覆盖 façade/runtime 选择、engine 状态 namespace、终结 summary（含
   `unverified`）、自动 cleanup、legacy 回归和稳定环境不污染。
6. 通过确定性测试覆盖：六类边界由 whitebox/test-only harness 覆盖，候选黑盒按第 3 条的
   lightweight 范围执行；旧 `requirement`/`cleanup` 入口不能绕过 engine handler，公开 cleanup 不能
   删除活动 engine run；缺失、binding 不一致或篡改的 `intakeConfirmationReceipt` 在 start 首次写入前拒绝；
   start 绑定的 requirement revision、登记工件集、solution binding 和 `intakeDigest` 在首次
   drive 时原样落成 intake receipt，且不产生第二次确认；同一 run 不可跨 runtime，
   `definitionSource`/`definitionDigest`/`packageDigest`/`installedTargetIdentity` 任一不匹配
   均在写前返回 `UNSUPPORTED_RUN_VERSION`；terminal replay 只读且返回 `Complete`，版本屏障
   发生在首个写入前；候选 run 对所有未迁移旧 workflow 写入口（至少 `resume`、`abort`、
   `reset`、`route`、`route-add`、`slicing`、`settle-findings` 等）统一显式拒绝且零写入，
   stable run 仍保持 legacy 语义；candidate `start` 不接受 `--split`、`--retained-overall`
   或 `--master`。
7. 批次计划、`Batch → Subtask` 映射及代理边界由 Part 2 技术审的 `granularity_review` 给出并
   留痕；一个 Batch 对应一次开发代理派发，Batch 内 Subtask 不换代理。

## 已确认的阶段内 `granularity_review` 记录

本阶段在拆分决定与路线确认时登记为**单一 Batch**（不是 run split）：
`phase-3-engine-vertical-loop`。划分依据是依赖链和共享边界，而不是代码行数：S1–S5
共同修改或依赖未冻结的 façade、admission/runtime 选择、`state.json`、`Store/CAS`、typed
protocol、envelope 与 terminal cleanup；各段不能独立验收或回滚，拆成多个 Batch 会制造
交接和重复验证成本。

| Batch | Subtask（按顺序） | 交付边界 |
| --- | --- | --- |
| `phase-3-engine-vertical-loop` | S1 façade/admission/runtime 选择；S2 `StartRequest`、intake receipt、canonical digest、envelope；S3 `drive`/`submit` 与六类 `NextResult`；S4 terminal cleanup、`Complete`、replay；S5 installed 隔离、legacy 回归与 negative fixtures | 一条可在 installed candidate 上验证的 lightweight engine vertical loop |

代理边界固定为：本 Batch 派发一个全新的零上下文开发代理；S1–S5 在同一代理上下文连续
完成，批内不换代理、不 reset；跨 Batch 才重新派发代理，交接只依赖已提交产物和批次任务书。
本 Batch 采用 full 验证档位，选定 `blackbox`、`whitebox`、`complexity-gate`、
`implementation-quality-gate`；批边界执行 `go build ./...`、`go vet ./...`、
`go test ./...`、installed-candidate harness、package validation、portable canary、
stable-driver smoke 及 digest/namespace disjoint、registry/fencing、cleanup receipt
检查。若实现发现改变 Batch 契约、接口、依赖或 DoD，必须暂停并重新进行
`granularity_review`，不得因提交数量自行新增 Batch。

## 非目标

- 不在本阶段完成 regular、full/custom、split/child/merge、SVN/P4 或五宿主完整迁移。
- 不删除 stable driver 的 legacy 入口，不做最终全局切换或 stable 退役。
- 不新增公开 `drive --event`、`submit-result`、公开 cleanup 或第二写通道。
- 不把范围外对抗性输入、权限/不可变文件失败或不受支持平台扩展成阻塞验收项。

## 阶段退出条件

阶段 3 必须在一次完整 formal-gates run 内完成已绑定 Batch 的开发、候选 installed binary 验证、
legacy 回归、独立审查、QA、必要修复和 Seal；六类边界、版本写前屏障、runtime 选择、自动清理
和禁止旧入口绕过的证据均绑定候选 identity；其中六类边界的证据层级按第 3 条执行。另须满足
增量封板计划共同退出条件中适用于本阶段的 package validation、portable canary、stable-driver
smoke、source/installed/package digest 与 host-path namespace disjoint proof、registry/fencing、
无未完成 intent/UNKNOWN receipt/cleanup 残留及 route-matrix 逐入口覆盖；范围内未通过项不得以
口头说明替代。
