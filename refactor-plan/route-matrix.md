# 命令路由矩阵（§2.3 全列名 · 活文档）

> 义务来源：`refactor-plan/incremental-seal-plan.md` §2.3（唯一写入者与版本绑定）、§5 第 11 项
> （每阶段退出条件）。本矩阵为阶段 0/1 交付义务补漏实物（2026-08-22），阶段 2 开始按
> 阶段交付继续维护。
>
> 维护规则（用户拍板 2026-08-22）：**先覆盖后实现**——任何新公共面在代码实现落地前必须先进
> 入矩阵；实现落地后矩阵与实际公开面一致，实际入口在矩阵中缺席即缺陷。反向普查机器测试
> `internal/validate/route_matrix_test.go` 机械断言：实际 workflow 子命令注册表 ⊆ 本矩阵、
> 超出 §2.3 枚举的行必须带"计划未枚举"标注、每行必需列齐备、§2.3 枚举与测试内固定清单双向
> 一致。
>
> 阶段绑定取值（§5.11 口径）：
>
> - `legacy`：stable driver 既有 legacy runtime 语义，阶段内不变；
> - `install-bootstrap`：阶段 0 安装/registry 事务面（无 workflow writer）；
> - `Shadow/诊断`：阶段 1 只读计划/诊断能力（不写权威 workflow state）；
> - `隔离 engine`：阶段 2 只在 internal/test-only 面写隔离 engine state，不是公开 CLI；
> - `unsupported`：未实现面——必须显式拒绝或不存在，不得缺省冒充。
>
> 计划枚举取值：`计划内` = §2.3 明文枚举；`计划未枚举` = 实际存在但 §2.3 未枚举的公开入口
> （补行规则），其阶段绑定仅允许 `legacy` 或 `unsupported`。
>
> 证据缩写：`P0-JSON` = `.gates/results/phase-0-distribution-002.json`，
> `P1-JSON` = `.gates/results/phase-1-decision-kernel.json`，`CASE-nnn` 为其中 qaExecution
> 案例；`cli.go` = `internal/cli/cli.go`；白盒测试名前的包名省略 `internal/`。

## workflow 面（§2.3 第一段：全部 workflow 子命令，含 drive/submit）

全部行为 `workflow <subcommand>` 形态，经 `cli.go` 的 `workflowSubcommands` 注册表分发
（cli.go:327-356）；注册表缺席的入口走 `runWorkflow` default 分支显式拒绝
（`unknown workflow subcommand: %s`，rc=1，cli.go:318-321）。阶段 0/1 无任何 engine runtime
公开 workflow 面；阶段 2 虽已交付隔离 engine 协议内核，但没有注册新的公开 workflow 面。
legacy run 与 stable driver 的 state 仍为 legacy 格式（无版本 envelope，绑定 admission
epoch/generation）。

本主表的“阶段 0/1 绑定”列保留历史 baseline 词汇；其中 status/next/drive/submit 行虽以
`legacy` 记录历史列，当前行为已由阶段 3 candidate façade 启用，具体 runtime、writer、错误码和
证据以本文“阶段 3 公共 workflow 逐入口绑定”表为准。该表示法不回写阶段 0/1 曾提供 candidate
公共入口的事实。

| 入口 | runtime | 唯一 writer | schema/definition 版本绑定 | 允许的状态变化 | 错误码 | 是否只读 | 阶段 0 绑定 | 阶段 1 绑定 | 计划枚举 | 机器证据 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| start | legacy | stable driver legacy runtime（`validate.Start`，cli.go:358-378） | legacy state 无版本 envelope，绑定 admission epoch/generation；不迁移不回写（P0-JSON CASE-024） | 创建 run：写 `.gates/tmp/<run>/state.json`（ACTIVE）与需求工件；首次写前要求已提交 bootstrap receipt。**--split 现状**：legacy start 仍带 `--split` 拆分意向声明（cli.go:370），与 §2.3 目标态（start 不接受拆分意向，拆分唯一绑定于 start-readiness PASS 后的拓扑确认）相反，属 legacy 维持项，阶段 3/4 迁移时改写并留修订记录 | UNREGISTERED_INSTALL（无 bootstrap receipt/登记失效，写前拒绝）、LOCK_HELD、参数校验错误 | 否 | legacy | legacy | 计划内 | P0-JSON CASE-002/059/061；validate `TestWhiteboxPhase0WorkflowAdmissionPrecedesStateCreation`（CASE-042）、`TestWhiteboxPhase0Round16FirstStartRequiresCommittedBootstrapReceipt`（CASE-061） |
| show | legacy | 无（只读加载 `validate.LoadRunState`，cli.go:380-389） | 同上（读侧不引入新字段） | 无 | 加载错误文本（run 不存在/state 损坏） | 是 | legacy | legacy | 计划内 | P0-JSON CASE-020/024（installed binary show rc=0） |
| status | candidate engine（阶段 3 installed façade） | 无（只读 engine projection） | engine 六字段 envelope；终态可读 terminal summary | 无 | `UNSUPPORTED_RUN_VERSION`、`CORRUPT_STATE`、run 不存在 | 是 | legacy | legacy | 计划内 | `TestPhase3FacadeOpenTerminalSummaryReplayIsReadOnly`；阶段 3 逐入口覆盖表为当前绑定 |
| next | candidate engine（阶段 3 installed façade） | 无（只读 engine projection） | engine 六字段 envelope；终态可读 terminal summary | 无 | `UNSUPPORTED_RUN_VERSION`、`CORRUPT_STATE`、run 不存在 | 是 | legacy | legacy | 计划内 | `TestPhase3FacadeOpenTerminalSummaryReplayIsReadOnly`；阶段 3 逐入口覆盖表为当前绑定 |
| diagnose | legacy（只读 raw/envelope parser，§2.3 唯一 raw read 例外） | 无（只读诊断 `validate.DiagnoseState`，cli.go:391-411；terminal summary 回落 `validate.RunSummaryPath`） | 最小 raw parser 不要求 envelope：legacy state 报 `state has no version envelope` 建议（不迁移）；版本化 state 校验四字段 envelope | 无（fixture bytes/mode/mtime 逐字节不变） | UNSUPPORTED_RUN_VERSION（缺失/mismatch 时作为 recommendation）；malformed JSON 报 `jsonReadable:false` | 是 | legacy | legacy | 计划内 | P0-JSON CASE-023/024；validate `TestWhiteboxPhase0DiagnoseRawReadOnlyFallback`（CASE-036） |
| resume | legacy | stable driver legacy runtime（缺省 `validate.ResumeReport` 只读；`--adopt-external` → `validate.AdoptExternalChange` 写，cli.go:413-428） | legacy state 无版本 envelope | 缺省只输出 resume 报告；`--adopt-external --reason` 采信外部变更并重绑 current snapshot（写） | 采信校验错误文本 | 否（含写路径） | legacy | legacy | 计划内 | P0-JSON CASE-020/024（stable binary resume）；cli `workflow_test.go` resume 用例 |
| abort | legacy | stable driver legacy runtime（`validate.Abort`，cli.go:430-452） | legacy state 无版本 envelope；终止后写 terminal summary | 终止 run：写 `.gates/results/<run>.json`（ABORTED）并清理 `.gates/tmp/<run>` | `--user-confirm` 二段确认缺失即拒（确认前不调 validate.Abort）；终止校验错误 | 否 | legacy | legacy | 计划内 | P0-JSON CASE-020（abort 产出 terminal summary）；cli `workflow_test.go` abort 用例 |
| reset | legacy | stable driver legacy runtime（`validate.ResetRun`，cli.go:454-470） | legacy state 无版本 envelope | 重建 run（reset/rebuild 本 run 状态） | 重建校验错误文本 | 否 | legacy | legacy | 计划内 | cli `workflow_test.go` reset 用例；validate `workflow_test.go` ResetRun 覆盖 |
| requirement | legacy | stable driver legacy runtime（`validate.UpdateRequirement`，cli.go:472-496） | legacy state 无版本 envelope | 需求确认（--confirmed）与需求变化/工件集登记（写） | 需求源/工件校验错误文本 | 否 | legacy | legacy | 计划内 | P0-JSON CASE-012（真实宿主会话需求登记）；cli `workflow_test.go` requirement 用例 |
| route-candidates | legacy | 无（只读计算 `validate.RouteCandidates`，cli.go:498-507） | 同 show（读侧） | 无 | 加载/候选计算错误文本 | 是 | legacy | legacy | 计划内 | cli `workflow_test.go` route-candidates 用例；P0-JSON CASE-011（公开入口 help 面） |
| slicing | legacy | stable driver legacy runtime（`validate.RecordSlicing`，cli.go:509-529） | legacy state 无版本 envelope | 拆分决定留痕（no-split 需理由、split 需拓扑/总实例参数）；`--user-confirm` 确认（RQ 后置项：拆分绑定仍发生在本命令，属 legacy 维持，阶段 4/5 迁移） | 决定参数校验错误文本 | 否 | legacy | legacy | 计划内 | P0-JSON CASE-012（真实 subagent 走 req-clar→…→slicing→route）；cli `workflow_test.go` slicing 用例 |
| settle-findings | legacy | stable driver legacy runtime（`validate.RecordSettledFindings`，cli.go:531-545） | legacy state 无版本 envelope | 审查 findings 处置（confirm/dismiss 留痕） | 处置参数校验错误文本 | 否 | legacy | legacy | 计划内 | validate `workflow_test.go`、`phase0_round12/15_whitebox_qa_test.go` SettleFindings 覆盖 |
| route | legacy | stable driver legacy runtime（`validate.SetRoute`，cli.go:547-559） | legacy state 无版本 envelope | 路线与门选择登记（full/custom/lightweight） | 路线/门目录校验错误文本 | 否 | legacy | legacy | 计划内 | P0-JSON CASE-012；cli `workflow_test.go` route 用例 |
| route-add | legacy | stable driver legacy runtime（`validate.AddRouteGates`，cli.go:561-572） | legacy state 无版本 envelope | 追加路线门 | 门 id 校验错误文本 | 否 | legacy | legacy | 计划内 | cli `workflow_test.go` route-add 用例 |
| qa-worktree | legacy | stable driver legacy runtime（`validate.RegisterQAWorktree`，cli.go:574-584） | legacy state 无版本 envelope | 登记 QA 隔离工作区 | worktree 路径校验错误文本 | 否 | legacy | legacy | 计划内 | validate `workflow_test.go`、`qa_incremental_merge_test.go`、`phase0_round12/15_whitebox_qa_test.go` RegisterQAWorktree 覆盖 |
| prepare-gate | legacy | stable driver legacy runtime（`validate.PrepareGate`，cli.go:586-597） | legacy state 无版本 envelope | 组装门审派发：登记 dispatch 与提示词工件（不改 run 主推进位） | 未过 admission 写前拒绝（UNREGISTERED_INSTALL）；门 id/状态校验错误 | 否 | legacy | legacy | 计划内 | P0-JSON CASE-062（prepare-action 同族 admission 先于提示词落盘）；cli `workflow_test.go` prepare-gate 用例 |
| prepare-action | legacy | stable driver legacy runtime（`validate.PrepareAction`，cli.go:599-618） | legacy state 无版本 envelope | 登记 OPEN dispatch、写派发提示词文件（PromptFile/PromptHash 绑定） | UNREGISTERED_INSTALL（写前拒绝，无提示词文件残留）；action id 校验错误 | 否 | legacy | legacy | 计划内 | P0-JSON CASE-012（真实宿主 subagent 生命周期）、CASE-062；`TestWhiteboxPhase0Round16MutateRunAdmissionPrecedesPromptArtifact` |
| claim-dispatch | legacy | stable driver legacy runtime（`validate.ClaimDispatchWithProvider`，cli.go:620-638） | legacy state 无版本 envelope | 认领 dispatch（审查者/provider 身份绑定） | 身份/dispatch 状态校验错误文本 | 否 | legacy | legacy | 计划内 | P0-JSON CASE-012（真实成对 SubagentStart/Stop→lifecycle verify VERIFIED）；cli `workflow_test.go` claim-dispatch 用例 |
| record-action | legacy | stable driver legacy runtime（`validate.RecordAction`，cli.go:849-856） | legacy state 无版本 envelope | 记录 action 结果（PASS/FAIL、findings、dispatch 闭合） | 结果/findings 校验错误文本 | 否 | legacy | legacy | 计划内 | P0-JSON CASE-012；cli `workflow_test.go` record-action 用例 |
| record-gate | legacy | stable driver legacy runtime（`validate.RecordGate`，cli.go:858-873） | legacy state 无版本 envelope | 记录门审结果（status/message/findings/compared 范围） | 门审结果校验错误文本 | 否 | legacy | legacy | 计划内 | cli `workflow_test.go` record-gate 用例；P0-JSON 各 gate 记录路径 |
| qa-design | legacy | stable driver legacy runtime（`validate.RecordQADesign`，cli.go:875-904） | legacy state 无版本 envelope | 登记/修订 QA 用例集（--case-id/--remove-case/--replace-all/--per-suggestion） | 用例输入校验错误文本 | 否 | legacy | legacy | 计划内 | cli `workflow_test.go` qa-design 用例；P0-JSON qaExecution 全链 |
| qa-review | legacy | stable driver legacy runtime（`validate.RecordQAReview`，cli.go:906-922） | legacy state 无版本 envelope | 登记 QA 审查决定（逐用例判定） | 决定输入校验错误文本 | 否 | legacy | legacy | 计划内 | cli `workflow_test.go` qa-review 用例 |
| qa-execution | legacy | stable driver legacy runtime（`validate.RecordQAExecution`，cli.go:924-940） | legacy state 无版本 envelope | 登记 QA 执行结果（逐用例 outcome/oracle） | 结果输入校验错误文本 | 否 | legacy | legacy | 计划内 | cli `workflow_test.go` qa-execution 用例 |
| qa-execution-scope | legacy | stable driver legacy runtime（`validate.RecordExecutionScope`，cli.go:725-744） | legacy state 无版本 envelope | FULL/AFFECTED 范围决定留痕（AFFECTED 需用例子集与理由） | 范围决定校验错误文本 | 否 | legacy | legacy | 计划内 | cli `workflow_test.go` qa-execution-scope 用例；validate `qa_scope_incremental_test.go` |
| snapshot | legacy | stable driver legacy runtime（`validate.AdvanceSnapshot`，cli.go:746-758） | legacy state 无版本 envelope | 组间开发快照推进（--dispatch 绑定；--user-requested 显式放行黑盒门并留授权来源） | 推进前提校验错误文本 | 否 | legacy | legacy | 计划内 | cli `workflow_test.go` snapshot 用例；P1-JSON（阶段 1 批次日志 221e75b 组间快照实践） |
| cleanup | legacy | stable driver legacy runtime（`validate.CleanupTempRuns`/`CleanupTempRun`，cli.go:760-771） | legacy state 无版本 envelope | 删除终态 run 的 `.gates/tmp` 目录（`--run <id>` 可显式删指定 run） | UNREGISTERED_INSTALL（登记失效时删除被拒）；unsafe run id（`../`）拒绝 | 否 | legacy | legacy | 计划内 | P0-JSON CASE-063；`TestWhiteboxPhase0Round16CleanupRunAdmitsRunBinding`；cli `workflow_test.go` cleanup 用例。注：engine 活动 run 不可被公开 cleanup 删除的 negative test 属阶段 3+（当前无 engine run） |
| carry | legacy | stable driver legacy runtime（`validate.RecordCarry`，cli.go:942-959） | legacy state 无版本 envelope | carry 处置决定留痕（含主代理接管理由） | 处置输入校验错误文本 | 否 | legacy | legacy | 计划内 | validate `decoupled_qa_test.go`、`repro_carry_regression_test.go`；cli `workflow_carry_seal.go` |
| authorize-repair | legacy | stable driver legacy runtime（`validate.AuthorizeExtraRepair`，cli.go:773-793） | legacy state 无版本 envelope | 授权额外修复轮（轮次/QA 范围授权留痕） | 授权参数校验错误文本 | 否 | legacy | legacy | 计划内 | cli `workflow_test.go` authorize-repair 用例；P0-JSON skipAuthorizations 机制 |
| seal | legacy | stable driver legacy runtime（`validate.Seal`，cli.go:795-808） | legacy state 无版本 envelope；Seal 写 terminal summary（.gates/results/<run>.json，SEALED）并支持 squash 集成 | 终态封板：admission/identity 校验先于 git squash；写封板产物（含黑盒用例交付物） | admission 失败不产生 committed seal；门未过需 --user-requested 授权留痕 | 否 | legacy | legacy | 计划内 | P0-JSON CASE-045；`TestWhiteboxPhase0SealFencesAdmissionBeforeGitSquash`；cli `workflow_test.go` seal 用例 |
| drive | candidate engine（阶段 3 installed façade） | engine Controller/drive handler（唯一 engine writer） | engine 六字段 envelope；`definitionSource`/`definitionDigest`/package/installed identity 精确匹配 | `Observe → Decide → SelectIssued`；lightweight 自动终结并保留 terminal summary | `UNSUPPORTED_RUN_VERSION`、`REVISION_CONFLICT`、`BUSINESS_REJECT`、声明的 engine failure class | 否 | legacy | legacy | 计划内 | `TestPhase3ProtocolIntakeReceiptIsExactlyOnce`、`TestPhase3FacadeOpenTerminalSummaryReplayIsReadOnly`；阶段 3 逐入口覆盖表为当前绑定 |
| submit | candidate engine（阶段 3 installed façade） | engine submit handler（唯一 engine writer） | engine 六字段 envelope + typed event/request/freshness 绑定 | 接纳 typed event 后继续确定性决策；同 ID 同 digest 幂等 | `UNSUPPORTED_RUN_VERSION`、`STALE_REQUEST`、`DIGEST_MISMATCH`、`INVALID_EVENT`、`REVISION_CONFLICT` | 否 | legacy | legacy | 计划内 | `TestPhase3SubmitFencesUnknownKindAndProviderBeforeWrite`；阶段 3 逐入口覆盖表为当前绑定 |
| submit-result | 无（不提供第二事件写入口） | 无 | 无 | 无（拒绝路径不写任何状态） | rc=1 + `unknown workflow subcommand: submit-result` | 是（拒绝即无写入） | unsupported | unsupported | 计划未枚举 | `TestCLIWorkflowFutureRejectsSubmitAlias`；阶段 3 只允许 typed `workflow submit`，`submit-result` 是明确禁止的第二写通道；route-matrix 反向普查的 unsupported negative probe |
| future | legacy（阶段 0 冻结的 versioned future surface 契约入口，非 workflow state writer） | future writer（`validate.GenerateFutureEnvelope`/`WriteFutureState`/`DiagnoseFutureState`，cli.go:642-723；generate/view 只读，write 写 `--path` 目标文档） | envelope 跟随 checked-in `definitions/workflow.json` 制品派生（`LoadFutureDefinition`）：阶段 0 为 version "1"/digest sha256:9ec68cd7…（phase0.go 冻结常量）；阶段 1 起制品 bump 为 version "2"/digest sha256:3db87c9c…（engine `definition/identity_gen.go` 同源生成） | 无 workflow 状态变化：generate 输出/可选落盘 envelope；write 写版本化 future 文档（四字段 envelope+payload）；view 只读诊断 | UNSUPPORTED_RUN_VERSION（9 种非法 envelope 写前拒绝，目标 0 bytes）；`future submit` 等未知 action 显式拒绝 | 否（write 写目标文档；generate/view 只读） | legacy | legacy | 计划未枚举 | P0-JSON CASE-009/011/021/022/023；`TestWhiteboxPhase0Round11VersionEnvelopeRejectsBeforeWrite`（CASE-035）；cli `TestCLIWorkflowFutureRejectsSubmitAlias` |

### 阶段 2 公开 workflow 绑定补表

此纵向补表登记原主表同名行的阶段 2 绑定；其余 runtime/writer/version/transition/error/evidence
列继续由原主表持有，避免复制后漂移。

| 入口 | 阶段 2 绑定 | 阶段 2 变化 |
| --- | --- | --- |
| start | legacy | 无；stable driver 继续拥有公开 run 创建 |
| show | legacy | 无 |
| status | unsupported | 无；仍不在公开注册表 |
| next | unsupported | 无；仍不在公开注册表 |
| diagnose | legacy | 无 |
| resume | legacy | 无 |
| abort | legacy | 无 |
| reset | legacy | 无 |
| requirement | legacy | 无 |
| route-candidates | legacy | 无 |
| slicing | legacy | 无 |
| settle-findings | legacy | 无 |
| route | legacy | 无 |
| route-add | legacy | 无 |
| qa-worktree | legacy | 无 |
| prepare-gate | legacy | 无 |
| prepare-action | legacy | 无 |
| claim-dispatch | legacy | 无 |
| record-action | legacy | 无 |
| record-gate | legacy | 无 |
| qa-design | legacy | 无 |
| qa-review | legacy | 无 |
| qa-execution | legacy | 无 |
| qa-execution-scope | legacy | 无 |
| snapshot | legacy | 无 |
| cleanup | legacy | 无 |
| carry | legacy | 无 |
| authorize-repair | legacy | 无 |
| seal | legacy | 无 |
| drive | unsupported | 无；公开 façade 属阶段 3 |
| submit | unsupported | 内部 `protocol.Engine.Submit` 已交付，但公开命令仍不存在 |
| future | legacy | 无；仍非 engine workflow writer |

## 阶段 2 隔离 engine / test-only 协议面（非公开 CLI）

本表登记阶段 2 实际交付的内部面。它们只能由 Go 调用方显式构造，并把状态目录指向隔离
namespace；不在 `workflowSubcommands` 或 top-level CLI 注册表中，也不接管本 run 的 stable
legacy writer。这里的“隔离 engine”是阶段绑定，不是计划枚举中的新增公共入口。

| 内部入口 | 归属与可见性 | 唯一 writer | 版本/新鲜度绑定 | 允许的状态变化与恢复 | 拒绝/结果 | 阶段 2 绑定 | 机器证据 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `persistence.Store.Save` / `Store.Recover` | `internal/engine/persistence`；由内部调用方用显式 state directory 和 `Config.PackageDigest` 构造 | `persistence.Store` 四段协议（intent -> execute -> observe/reconcile -> commit） | writer=`engine`、state schema `1`、workflow definition `2`、definition digest `sha256:3db87c9c6f3c0321ae55aa4d8196bc935b5a603a3948025352553d8ed1b9248f`、调用方注入 package digest；revision CAS + external fingerprint | 原子保存 `state.json`；崩溃后 `clean/committed/recovered/residual` 对账；完整性摘要、目录锁与残留清扫 | `UNSUPPORTED_RUN_VERSION`、`STATE_INTEGRITY_MISMATCH`、`REVISION_CONFLICT`、`FINGERPRINT_CHANGED` | 隔离 engine | persistence `TestSaveFaultBoundariesRecoverDeterministically`、`TestRecoverRedoesInterruptedCommit`、`TestRevisionCASConflict`、`TestFingerprintBeforeWriteRejects`、`TestFingerprintAfterWriteCommitsAndReports` |
| `protocol.Engine.Submit` | `internal/engine/protocol`；统一接纳受限 request/decision、task progress、SpawnReceipt、worker result、Operator observation、HostAction receipt、lifecycle event | `protocol.Engine`，每次接纳经 `persistence.Store.Save` | event ID + canonical payload digest 幂等；request ID + 当前 freshness token；当前 task/Attempt/provider/definition registry | 更新 expected tasks、Attempt、pending action/Ask、receipt/result/observation 台账；result-before-receipt 暂存，接纳后继续 Decide/SelectIssued | 同 ID 同 digest 稳定重放；异 digest、stale freshness/task-progress Attempt、provider mismatch、未知/非当前事件拒绝且零状态变化；旧 Attempt 的 worker result 记为 `OBSOLETE_RESULT` | 隔离 engine | protocol `TestRequestIdempotentReplay`、`TestSameIDDifferentDigestHardReject`、`TestFreshnessStaleRejectedZeroChange`、`TestWorkerResultStagesBeforeReceipt`、`TestLifecycleDuplicateReplayIdempotent`、`TestFreeUserEventRejectedThroughSubmit` |
| `Engine.RecoverAttempt` / `ReconcileUnknownReceipt` / `ReconcileHostAction` | `internal/engine/protocol` 恢复方法；只处理已持久化 action/receipt/intent | `protocol.Engine`，恢复决定与记录仍经同一持久 writer | current action/Attempt、task/snapshot/responsibility bindings、lifecycle identity、expected fingerprint | 客观瞬态且 bindings 未变 resume；绑定变化或已知失效建新 Attempt；原因未知 Ask；UNKNOWN receipt 唯一 lifecycle 匹配 attach，零/多匹配 Operator；UNKNOWN HostAction 先观察再 reconcile/wait/operator，不重放副作用 | 返回封闭 `RecoveryPlan`；engine/invariant 故障不动态降级 agent，旧 Attempt 结果记 `OBSOLETE_RESULT` | 隔离 engine | protocol `TestUnknownSpawnReceiptRecoveryIsDurable`、`TestHostActionReconciliationReplay`、`TestDefinitionFailureResultsAreTerminalAndDiagnosable`；testkit `TestLateAttemptResultIsRecordedObsolete`、`TestHostActionUnknownReconcileDoesNotExecuteAgain` |
| `testkit.NewProtocolFixture` + `FaultPlan` / fake host / fake worker / fake VCS | `internal/engine/testkit`；仅供仓库内测试与 test-only harness 调用；`FaultPlan.Arm/ArmCrash(FaultPoint)` 是命名、单次触发开关 | 复用上述 engine writer；FakeVCS 是 Engine 的 external fingerprint collector，并以 project-local ledger 提供 `ApplyOnce` 持久副作用计数 | fixture package digest 固定为 `sha256:testkit`；fake provider=`fake-host`；注入点与调用产生的 current revision/receipt 台账绑定 | 覆盖持久边界、spawn attach、result-before-receipt、响应丢失、并发 submit、旧 Attempt、fingerprint、FakeVCS 与 HostAction observe/reconcile/commit；输出 snapshot、NextResult、acceptance、调用计数与 recovery report | `ErrInjected`/`ErrInjectedCrash` 为 test-only 注入结果；无 sleep/概率竞争；FakeVCS 同名操作跨重启至多执行一次 | 隔离 engine | testkit `TestFaultPlanIsNamedAndSingleTrigger`、`TestFakeHostSpawnCrashDoesNotRepeatSideEffect`、`TestSubmitResponseLossIsIdempotent`、`TestConcurrentSubmitLeavesOneCanonicalState`、`TestExternalFingerprintRecheckBeforeAndAfterWrite`、`TestHarnessHostUnknownReconcilesAtMostOnce` |
| `internal/engine/testkit/cmd/harness` | 独立 test-only 二进制；`go build -o <path> ./internal/engine/testkit/cmd/harness`；`--project-root`/`FORMAL_GATES_TEST_PROJECT` 是唯一 namespace 入口，`--scenario` 选择登记场景，event/action/request/provider/correlation/status/outcome/failure/fault 等参数只作用于该隔离项目 | 复用 `NewProtocolFixture` 的隔离 engine writer；不注册公开 `workflow drive/submit` | project root 决定 `engine-state/state.json`/`terminal-summary.json`；event ID + digest、revision、freshness、provider、Attempt 与 package envelope 均由场景报告暴露 | `smoke`/`recover`、五次 revision submit、request/decision/worker/receipt/lifecycle/operator、idempotency/freshness/CAS/concurrency/fingerprint、capacity refill、interruption/result-before-receipt、HostAction、terminal query/replay、六类 NextResult、failure routing、`full` E2E；`fault --fault <point>` 跨进程保留并恢复协议窗口 | 缺参数或非法事件 rc=1；envelope 缺字段写前拒绝；同 ID 异 digest、旧 revision/freshness/provider/Attempt 硬拒绝；terminal replay 不恢复 active state；故障点按名确定触发 | 隔离 engine | acceptance `TestAcceptanceInstalledProtocolHarness`；testkit `TestHarnessEnvelopeBarrierDoesNotCreateTarget`、`TestHarnessCapacityOneRefillsSecondEligibleTask`、`TestHarnessResultBeforeReceiptSettlesAfterRestart`、`TestHarnessTerminalReplayUsesDurableSummaryWithoutWrites` |

## 维护/transport 面（§2.3 第二段：top-level 维护/registry/cutover/rollback）

分类取值：`只读`、`只写外部 observation/receipt`、`委托 engine submit`。阶段 0/1 无任何
`委托 engine submit` 入口；阶段 2 的 engine submit 只在上表 internal/test-only 面存在，本维护
面仍无入口委托它，且不得成为 workflow writer。
top-level 分发见 `cli.go` run() switch（cli.go:75-97）；未列 top-level 命令显式拒绝
（`unknown command: %s`，rc=1，cli.go:94-96）。

| 入口 | 分类 | owner | generation/token | receipt schema | 恢复入口 | 权限边界 | 阶段 0 绑定 | 阶段 1 绑定 | 计划枚举 | 机器证据 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| hook（hook decide） | 只读（stdin payload → stdout 决策 + 宿主协议 exit code） | 无状态 owner（`validate.Hook`，cli.go:1046-1078） | 无（纯函数决策，不持有 token） | 无持久化 receipt；宿主协议 JSON 响应 | 无需恢复（无写入） | 只判写目标身份：主线程/声明身份写→allow，无身份写→block（deny/exit 2）；不写 workflow state；provider 白名单校验 | legacy | legacy | 计划内 | P0-JSON CASE-012（真实宿主 allow/block 矩阵与 codex 重放）；validate `hook_test.go`、cli `hook_test.go` |
| lifecycle capture | 只写外部 observation/receipt（生命周期事件落 observation buffer） | lifecycle 事件 owner（`lifecycle.Capture`，cli.go:1093-1119） | dispatch 身份配对（SubagentStart/Stop 与已 claim dispatch 绑定） | 生命周期 observation 文件（候选/项目 namespace 内） | 无需恢复（observation 追加语义，重复事件 duplicate:true 幂等） | 只写 observation buffer，不直接改变 workflow state；无 run project 时 no-op | legacy | legacy | 计划内 | P0-JSON CASE-012（成对事件 verify VERIFIED、逐字节重放 duplicate:true）；lifecycle 包测试 |
| lifecycle verify | 只读 | 无（`lifecycle` verify 路径，cli.go:1120+） | 校验 dispatch 身份配对 | 输出验证结论（VERIFIED/REJECTED），不持久化 | 无需恢复 | 只读核验；未 claim 的 verify→REJECTED | legacy | legacy | 计划内 | P0-JSON CASE-012（未 claim verify→REJECTED）；cli `lifecycle` 相关测试 |
| canary（portable/fault-matrix/codex-hook/codex-hook-probe） | fault-matrix=只写外部 observation/receipt（fixture namespace 内演练+receipt）；portable/codex-hook=只读检查+临时证据；probe=写 --payload-dir 调试载荷 | canary owner（`validate.PortableCanary`/`InstallFaultMatrix`/`CodexHookCanary`/`CodexHookProbe`，cli.go:1135-1230） | fault fixture 绑定事务 journal 的 generation/lease/token | canary/fault report（JSON：Checks/Status、recovery receipt 字段） | fault-matrix 演练走 install 事务 journal 恢复路径 | 不拥有 workflow writer；portable 只读 installed immutable runtime；codex-hook 需真实宿主会话 | legacy | legacy | 计划内 | P0-JSON CASE-001/027（portable canary 双跑 0 FAIL）；CASE-067（`TestWhiteboxPhase0Round16CodexHookCanaryRetriesTextOnlySession`）；fault-matrix 见 install 行证据 |
| gate（gate run/report） | 只读（run 组装提示词到 stdout；report 校验 stdin 结果，显式"非正式结论、未持久化"） | 无（`validate.ComposeStandaloneGatePrompt`/`ValidateStandaloneGateResult`，cli.go:971-1044） | 无（脱离 run 的快速检查） | 无持久化（report 明示未持久化） | 无需恢复 | 脱离 run 使用，不写 run state、不产生权威门结论 | legacy | legacy | 计划内 | validate `standalone_gate_test.go`；cli `gate_test.go` |
| install | 只写外部 observation/receipt + 安装事务（复制 runtime/prompts/gates、hook/rule、pointer、registry record；无 workflow writer） | 同一 native owner（先 install/uninstall lock 再 registry lock，`validate.Install`，install.go:387+） | registry record 的 epoch/generation、lease/token；事务 journal 含 generation/lease/token | install report JSON（canonicalPaths/disjointProof/hook/rule digest/manifest）+ `.transaction.json` failure/recovery receipt | 持久 recovery journal：崩溃后重跑自动 observe/reconcile（epoch 单调递增）；任一阶段失败恢复旧安装/旧配置/旧 registry | install/package 不得拥有 workflow writer；脚本不得先于 native owner 删 release/切 pointer/写 registry；候选不得执行 bootstrap 或写 stable registry | install-bootstrap | install-bootstrap | 计划内 | P0-JSON CASE-007/008/013/015/016/018/019/029/030；`TestWhiteboxPhase0Round15InstallFaultMatrixRecoveryReceiptIdentity`（CASE-048）、`TestWhiteboxPhase0Round12SupportedRestartReconcilesCrashJournalAutomatically`（CASE-051）、`TestWhiteboxPhase0ReleaseRunsInstalledBinarySmokeBeforeCommit`（CASE-056） |
| install --bootstrap | 只写外部 observation/receipt（仅建 registry/launcher record 与 bootstrap receipt，不安装 runtime 文件、不创建 workflow state） | 冻结 stable driver（首次 bootstrap 公开入口固定于此，§2.4） | bootstrap receipt 绑定 record 的 epoch/generation、lease/token、canonical paths | `<registry>.bootstrap.json`（record 含 canonical paths、epoch/generation、stateCreated=false） | 失败/无法对账只留可审计 disabled/UNREGISTERED_INSTALL receipt 并停止 | 不创建 `.gates/tmp`、不写 workflow state；registry 不存在时仅允许这一次 bootstrap 创建 | install-bootstrap | install-bootstrap | 计划内 | P0-JSON CASE-002/003/059；`TestWhiteboxPhase0Round12BootstrapReceiptBindsExactRegistryIdentity`（CASE-040）、`TestWhiteboxPhase0Round16FirstStartRequiresCommittedBootstrapReceipt`（CASE-061）、`TestWhiteboxPhase0Round16GlobalSiblingIdentitySurvivesUpgradeAndBootstrap`（CASE-065） |
| uninstall | 只写外部 observation/receipt + 卸载事务（删除自有 runtime/hook 条目/marker 区块，registry record 转 disabled） | 同一 native owner（与 install 共享 install/uninstall lock，`validate.Uninstall`） | 同 install（epoch 单调不回退） | uninstall report + failure/recovery receipt（operation=uninstall 含 phase/observed/reconcile/recovered/generation） | 事务 journal 恢复；外部 hook/规则区外内容逐字节保留；重复卸载幂等 | 不触碰 workflow state；候选/外部内容不受损 | install-bootstrap | install-bootstrap | 计划内 | P0-JSON CASE-006/017；`TestWhiteboxPhase0Round12UninstallFaultsReconcileAndPreserveExternalContent`（CASE-050）、`TestWhiteboxPhase0Round12InstallAndUninstallUseTheSameLockOwner`（CASE-047） |
| package（validate/route-candidates/baseline） | validate/route-candidates=只读；baseline=只写外部 receipt（--output 的 baseline receipt 文件） | `validate.Package`/`PackageReceipt`/`BuildBaselineReceipt`（cli.go:100-190） | receipt 绑定 VCS identity、package/installed digest、canonical paths | package validation receipt（221 项 mode/size/sha256/realPath manifest）；baseline receipt（identity/disjoint proof） | 无需恢复（无安装变更） | 只读验证优先；Lstat/realpath 拒绝 symlink（不跟随）；无 workflow writer | install-bootstrap | install-bootstrap | 计划内 | P0-JSON CASE-001/013/026；`TestWhiteboxPhase0Round12PackageReceiptBindsStableAndInstalledIdentities`（CASE-037）、`TestWhiteboxPhase0Round12BaselineReceiptValidatesManifestPathsAndIdentityBoundaries`（CASE-038） |
| registry admission（registry admit） | 只写外部 observation/receipt（校验并落 machine-readable admission receipt） | registry lock 下校验（`validate.AdmitRegistry`，phase0.go:695-714） | 校验 record 的 epoch/generation、lease/token 与 canonical paths | `registry.json.admission.json`（Accepted/Code/Reason/recordId，拒绝时 disabled receipt） | 无状态变化；拒绝路径留 receipt 供审计 | 不改 workflow state；registry 不可达/无法对账时停止 | install-bootstrap | install-bootstrap | 计划内 | P0-JSON CASE-003/066；`TestWhiteboxPhase0Round12AdmissionRejectsInvalidRecordsBeforeNamespaceWrites`（CASE-041）、`TestWhiteboxPhase0Round16AdmissionRejectsStaleInstalledDigest`（CASE-066） |
| registry register | 内部 owner handler（registry 写入面仅归安装事务 owner；CLI 无独立 register 入口） | 安装事务 native owner（`validate.Install`/bootstrap 路径独占） | registry record 的 epoch/generation、lease/token | registry record（canonical paths/scope/runtime sibling 身份）+ bootstrap receipt | 事务 journal 恢复；crash 后 supported restart 自动对账 | 候选不得写 stable registry；绕过 launcher/lease 的直达 runtime sibling 被 admission 拒绝 | install-bootstrap | install-bootstrap | 计划内 | P0-JSON CASE-031；`TestWhiteboxPhase0Round7RegistryMutationSurfaceOwnedByInstaller`（CLI 无独立 bootstrap/register 入口、无 --registry 注入）；CASE-039（`TestWhiteboxPhase0Round12RegistryOwnerPreservesRecordsAcrossInstallAndUninstall`） |
| registry reconcile | 内部 owner handler（crash journal/未完成 intent 的对账归安装事务 owner） | 安装事务 native owner（journal reconcile 路径） | journal 的 generation/lease/token（RECOVERED 证据含全身份） | recovery receipt（observedFact/interruptedPhase/reconcileAction/recovered） | 支持的重启在新事务前自动对账；journal 清理、epoch 单调 | 不改 workflow state；对账失败留 receipt 并停止 | install-bootstrap | install-bootstrap | 计划内 | P0-JSON CASE-008/051；`TestWhiteboxPhase0Round12SupportedRestartReconcilesCrashJournalAutomatically`（CASE-051）；CASE-048 故障矩阵 |
| registry show | 只读（加载并打印 registry JSON） | 无（`validate.LoadRegistry`，cli.go:217-224） | 无（读侧） | 无（stdout 输出） | 无需恢复 | 只读；不写 registry、不写 state | legacy | legacy | 计划未枚举 | cli `runRegistry` default 分支显式拒绝未知子命令；registry 加载路径被 CASE-003（admit 拒绝路径 LoadRegistry）与 phase0 测试族间接覆盖 |
| cutover | 无（generation/authority 切换协议未实现，无公开入口） | 无 | 目标态为 SWITCHING(E+1) 下 cutover token（§2.4，阶段 7 交付） | 无 | 无 | 当前 CLI 无该命令：top-level 显式拒绝 `unknown command`（cli.go:94-96）；普通 stable/candidate start 在切换协议落地前不受 generation 拒绝面约束 | unsupported | unsupported | 计划内 | 反向普查测试断言 top-level 命令面；证据缺口：无既有 negative test（无实现面可测；唯一既有事实是 top-level switch 缺席该命令） |
| rollback | 内部 owner handler（install/uninstall 事务内 rollbackAll + journal 恢复；非公开命令） | 安装事务 native owner（install.go:387 rollbackAll） | 事务 journal 的 generation/lease/token（epoch 不回退） | failure/recovery receipt（ROLLED_BACK/recovered、stableDigest/generation） | 事务内恢复旧 runtime/pointer/config/hook/rule/registry；reconcile 后旧 stable validate/canary smoke | 不改 workflow state；generation 级 ROLLING_BACK 公开动作不存在（属阶段 7 协议，当前 CLI 无该命令，显式拒绝 unknown command） | install-bootstrap | install-bootstrap | 计划内 | P0-JSON CASE-007/017/018/019；`TestWhiteboxPhase0Round12ReleaseRollbackRestoresAllOuterIdentity`（CASE-052） |

### 阶段 2 维护/transport 绑定补表

| 入口 | 阶段 2 绑定 | 阶段 2 变化 |
| --- | --- | --- |
| hook | legacy | 无；不委托 engine submit |
| lifecycle capture | legacy | 无；仍只写外部 observation buffer |
| lifecycle verify | legacy | 无 |
| canary | legacy | 无 |
| gate | legacy | 无 |
| install | install-bootstrap | 无 |
| install --bootstrap | install-bootstrap | 无 |
| uninstall | install-bootstrap | 无 |
| package | install-bootstrap | 无 |
| registry admission | install-bootstrap | 无 |
| registry register | install-bootstrap | 无 |
| registry reconcile | install-bootstrap | 无 |
| registry show | legacy | 无 |
| cutover | unsupported | 无 |
| rollback | install-bootstrap | 无；仍为 installer 内部 owner handler |

## 阶段 0/1 绑定结论（§5.11 一句式）

- 阶段 0：公开 workflow 面全部为 `legacy`（stable driver 语义不变，`--split` 维持现状）；新增
  能力全部落在维护面 `install-bootstrap`（安装/registry 事务、bootstrap、journal 恢复、
  future 契约常量）；`status`/`next`/`drive`/`submit` 显式 `unsupported`。唯一 workflow
  writer 仍是 stable legacy runtime；install/bootstrap 无 workflow writer。
- 阶段 1：公开 workflow 面维持全部 `legacy`（CASE-018 候选 legacy 回归）；新增能力为只读
  `Shadow/诊断`（engine decision/shadow 不经任何公开 CLI 入口写权威 state——Shadow harness
  为候选安装内测试入口，`TestShadowReadOnlyObservesWithoutWriting`）；`future` envelope 跟随
  制品 bump 至 version "2"。不存在第二个状态写入权威。

## 阶段 2 绑定结论（§5.11 一句式）

- 阶段 2：所有既有公开 workflow/维护入口维持阶段 1 绑定；公开 `workflow drive`、
  `workflow submit` 仍显式 `unsupported`。新增 writer 仅为隔离 state directory 内的
  `protocol.Engine`/`persistence.Store`，经 internal API 或 test-only harness 调用，不是公开
  CLI，也不接管 stable legacy run。阶段 3–7 绑定留待对应阶段更新。

## 阶段 3 绑定结论（§5.11 一句式）

- 阶段 3 的 runtime 选择由已安装并通过 admission 的 target identity/package digest 决定，不新增
  用户 runtime、identity 或 digest 参数：候选 façade 从 admission/launcher registry 解析 installed
  target 的 identity/package digest，`start` 创建带 owning engine/package envelope 的
  engine run；stable driver target 的 `start` 继续创建 legacy run。engine façade 后续只按 envelope
  选择并拒绝跨 runtime。
- 候选 engine 公共面启用 `start`、`drive`、`submit`、`show`、`status`、`next`、`diagnose`；
  `start` 前置为已确认 intake，首次 `drive` 自动登记 intake receipt/digest。lightweight terminal
  summary 标记 `unverified=true`。旧 `requirement`、公开 `cleanup` 及其他未迁移旧 workflow 写入口
  在 engine run 上明确 unsupported/zero-write，在 stable legacy run 上维持原有语义；不引入第二个
  writer 或第二次需求确认。

## 阶段 3 公共 workflow 逐入口绑定（实现前冻结）

本表是上方阶段 0/1 主表和阶段 2 补表在阶段 3 的权威覆盖；同名入口在本表出现时，以本表
的 candidate/stable 分流为准。每个新增或改变的公共入口在实现前必须具备完整列，避免只写
阶段结论而无法冻结验收判据。

| 入口 | runtime | 唯一 writer | schema/definition 版本绑定 | 允许的状态变化 | 错误码 | 是否只读 | 阶段 3 绑定 | 计划枚举 | 机器证据 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| start（candidate target） | candidate engine（由 admission/launcher registry 的 installed-target identity/package digest 选择） | engine bootstrap handler（唯一 bootstrap writer） | 阶段 0 冻结 stable driver 仍只写既有确认状态；固定 launcher 从已确认 formal run 状态和登记集工件派生 `intakeConfirmationReceipt.json`，并按原相对路径注入 candidate test project；`StartRequest` 仅通过 `--intake-receipt <path>`/固定 host config 指针取得 typed receipt；写入 `workflowDefinitionVersion`、`stateSchemaVersion`、`definitionSource`、`definitionDigest`、owning `packageDigest`、`installedTargetIdentity`、requirement/solution bindings 与 `intakeDigest` | 只从 admission registry 解析 owning identity；以当前注入工件重算 binding，校验确认回执并创建 engine envelope 与 ACTIVE run；缺失/旧回执、工件变更或其他 binding 不一致首写前零写入；不接受 runtime/identity/digest 参数和 `--split`/`--retained-overall`/`--master`，不写第二次确认事件；stable driver 不新增 writer/公共入口 | `UNREGISTERED_INSTALL`、`INVALID_INTAKE_CONFIRMATION`、`UNSUPPORTED_RUN_VERSION`、`LOCK_HELD`、参数校验错误 | 否 | candidate installed target 创建 engine run；stable target 仍走下行 legacy 行 | 计划内 | 阶段 3 start admission/intake/identity/receipt-transport/zero-write fixtures |
| start（stable target） | legacy | stable driver legacy runtime | 既有 legacy state/admission epoch/generation（不迁移） | 创建 legacy run，语义保持阶段 0/1/2 | 既有 `UNREGISTERED_INSTALL`、`LOCK_HELD`、参数校验错误 | 否 | legacy 回归对照，不进入 engine namespace | 计划内 | P0-JSON CASE-002/059/061 |
| drive（candidate target） | candidate engine | engine Controller/drive handler（内部状态 writer；不接收外部事件） | loader 写前精确校验六字段 envelope：版本、`definitionSource`/`definitionDigest`、owning `packageDigest`/`installedTargetIdentity` | `Observe → Decide → SelectIssued`；首次 drive 原样登记 intake receipt/digest；lightweight 先写可恢复 terminal intent，再 cleanup/核对无残留，最后提交 `unverified=true` terminal summary；六类边界由 whitebox/test-only harness 覆盖 | `UNSUPPORTED_RUN_VERSION`、`REVISION_CONFLICT`、`BUSINESS_REJECT`、声明的 engine failure class | 否 | 无 `--event` 第二通道；同一 run runtime 固定 | 计划内 | 阶段 3 drive/receipt/CAS/cleanup fixtures |
| submit（candidate target） | candidate engine | engine submit handler（start 后唯一外部事件 writer） | typed event/receipt + request/action ID、freshness、当前节点、source binding 与 digest；禁止 `drive --event`/`submit-result` | 接纳用户决定、worker result、HostAction/lifecycle receipt 后继续确定性决策；同 ID 同 digest 幂等 | `UNSUPPORTED_RUN_VERSION`、`STALE_REQUEST`、`DIGEST_MISMATCH`、`INVALID_EVENT`、`REVISION_CONFLICT` | 否 | 不新增第二 workflow writer | 计划内 | 阶段 3 submit 幂等/旧 Attempt/receipt fixtures |
| show（candidate target） | candidate engine | 无（只读 engine projection） | 同 drive 的六字段 envelope；终态可读 terminal summary | 无 | `UNSUPPORTED_RUN_VERSION`、`CORRUPT_STATE`、run 不存在 | 是 | 只读查看 engine state/summary | 计划内 | 阶段 3 show/terminal replay fixtures |
| status（candidate target） | candidate engine | 无（只读 engine projection） | 同 show；可展示 freshness token 与 `availableActions` | 无 | `UNSUPPORTED_RUN_VERSION`、`CORRUPT_STATE`、run 不存在 | 是 | 新增公开只读面，不写状态 | 计划内 | 阶段 3 status/read-only fixtures |
| next（candidate target） | candidate engine | 无（只读 engine projection） | 同 show；返回 canonical Plan 的唯一 `NextResult` kind | 无 | `UNSUPPORTED_RUN_VERSION`、`CORRUPT_STATE`、run 不存在 | 是 | lightweight installed candidate 公共路径返回 `Complete`；六类 union contract 由 whitebox/test-only harness 覆盖，regular 公共路径留阶段 4 | 计划内 | 阶段 3 六类 NextResult/terminal replay fixtures |
| diagnose（candidate 或 legacy） | candidate engine / legacy raw parser | 无（只读 raw/envelope parser） | candidate 报告六字段 envelope 缺失/mismatch；legacy 允许最小 raw parser，不迁移 | 无；fixture bytes/mtime/path 不变 | `UNSUPPORTED_RUN_VERSION` 作为 recommendation；malformed JSON 报 `jsonReadable:false` | 是 | 唯一 raw read 例外，不得绕过 decoder 写入 | 计划内 | 阶段 3 version/diagnose zero-write fixtures；P0-JSON CASE-023/024 |
| requirement（engine run） | candidate engine | 无（显式 unsupported handler） | 不加载或改写 engine envelope | 无；拒绝前后 state/receipt/mtime 零变化 | `UNSUPPORTED_ENGINE_ENTRY`（稳定错误） | 是（拒绝即零写入） | 旧命令不能再次确认或绕过 intake binding | 计划内 | 阶段 3 old-requirement zero-write negative |
| requirement（stable legacy run） | legacy | stable driver legacy runtime | 既有 legacy state/admission 绑定 | 需求确认/变化与工件登记保持原有 legacy 语义 | 既有需求源/工件校验错误 | 否 | 仅 legacy 回归对照 | 计划内 | P0-JSON CASE-012；cli workflow requirement tests |
| cleanup（engine run） | candidate engine | 无（显式 unsupported handler；清理由 engine terminal handler 承担） | 不加载或改写 engine envelope | 无；不得删除活动 engine run | `UNSUPPORTED_ENGINE_ENTRY`（稳定错误） | 是（拒绝即零写入） | 公开 cleanup 不能成为第二清理通道；终结由 intent→execute→observe/reconcile→commit 完成 | 计划内 | 阶段 3 public-cleanup zero-write negative |
| cleanup（stable legacy run） | legacy | stable driver legacy runtime | 既有 legacy state/admission 绑定 | 终态 `.gates/tmp` 清理保持原有语义 | 既有 admission/unsafe run-id 错误 | 否 | legacy 回归对照 | 计划内 | P0-JSON CASE-063 |

### 阶段 3 未迁移旧入口的 engine 分流

`resume`、`abort`、`reset`、`route`、`route-add`、`slicing`、`settle-findings`、`qa-worktree`、
`prepare-*`、`claim-dispatch`、`record-*`、`snapshot`、`carry`、`authorize-repair`、`seal` 等
未在本阶段启用 engine handler 的入口，先按 run envelope 分流：命中 candidate engine run 时统一返回
`UNSUPPORTED_ENGINE_ENTRY` 且 state/receipt/mtime 零变化；命中 stable legacy run 时继续走既有 legacy
语义。它们不得因 run ID 相同而落到另一 runtime，也不得成为 engine 的第二 writer。

### 阶段 3 六类边界的证据层级

阶段 3 的 installed-candidate 黑盒只把 lightweight 公共路径验到 `Complete`，并验证上述 façade、
版本/身份、旧入口和隔离负向；`Ask`、`Ready`、`HostAction`、`Wait`、`Operator` 五类只有通过
whitebox/test-only sequence harness 验证。阶段 4 regular 公共路径建立后，再把这五类扩展为候选
公共黑盒路径。
