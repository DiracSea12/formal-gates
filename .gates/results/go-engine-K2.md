# K2 流程确认与持久等待（轻装模式记录，2026-09-21）

方案 §13.2 K2：精确流程确认和持久等待；最小验收样例＝确认前后退出、旧 ID、重复答复、终态答复；
**没有用户决定就不执行后继**。按机主轻装口径执行（测试＋独立复核＋全量回归＋单提交）；7 步简洁法
与质量维度沿用 K1 轮（正确性/需求覆盖/可读性/简洁/可维护/架构解耦/性能/防过度设计与屎山）。
基线＝K1 收口源 `4822b8d`。

## 交付（internal/runner，新增 decisions.go 307 行＋decisions_test.go 655 行，改三文件）

| 部分 | 职责 |
| --- | --- |
| `decisions.go`（新） | 四哨兵（`ErrWaitingDecision/ErrRunFinished/ErrDecisionStale/ErrDecisionConflict`，注释互指、各对应 §6.1 一类拒绝）；`Confirmation{Options,Revision,Body}` 挂 `TaskSpec.Confirm`；`gateDecision` 懒建门（只读快查为热路径，行缺失才短事务 INSERT 行＋`decision_created` 事件同事务）；`PendingDecisions`（只列 pending）；`Answer`（单短事务固定判定序） |
| `store.go` | schemaVersion 2→3；`decisionsTable` DDL（v3 全部增量）；migrate 加 v2→v3 加法分支——**v2 是已验收基线，真迁移不拒开**（v0＝基线＋增量、v2＝仅增量，同事务落 user_version；v1 拒绝分支原样保留） |
| `runner.go` | 门检查位于 nextReady 选中＋spec 查得之后、registerIntent 之前：pending → 立即返回 `ErrWaitingDecision`（在"无就绪未完成"环检查路径之前，等待不会被误报为环）；answered → 门已消费照常执行（executor 失败重跑**不再问**）；`newInvocationID` 泛化为 `newID128`（调用与决策共用）；`inputDigest` 注释扩为三处绑定共用 |
| `crash_test.go` | 仅 TestMain 追加 `RUNNER_K2_SCENARIO` 分支（既有 envCrashPoint 路径、注入点、退出码 70–84 逐字未动） |
| `decisions_test.go`（新） | §13.2 五场景＋补充单测＋迁移测试；子进程退出码 90/91（正常）＋92+（接线） |

decisions DDL：`decision_id TEXT PK`（`newID128` 128 位随机 hex）、`run_id REFERENCES runs`、`task_id`、
`generation`、`revision`、`body`（小展示正文）、`body_digest`（sha256 绑定）、`options_json`、`created_at`、
`state CHECK IN ('pending','answered')`、`answer`/`answered_at`（NULL 风格照 invocations.result_json）、
`UNIQUE(run_id,task_id,generation)`（一任务一代次一门）。未加额外索引——全部读路径由 UNIQUE/PK 覆盖。
事件 payload 只存 id/task/generation/revision/options/choice（行是权威，不塞正文全文）。

Answer 判定序（§6.1 逐句落库）：
① run 终态（无 unfinished 任务）→ `ErrRunFinished`，**优先于幂等重放**（对照 run_graph RUN_TERMINAL 先例）；
② 行不存在或不属本 run → `ErrDecisionStale`（旧 ID；不作"当前只有一个问题"推断）；
③ 已 answered：同 choice → 从行读 answered_at 返回**原接纳回执**（零新转换零新事件）；异 choice → `ErrDecisionConflict`；
④ pending：choice ∉ options → 描述性拒绝（行保持 pending）；合法 → 消费一次（state/answer/answered_at＋`decision_answered` 事件同事务）。
Answer 不取 advance 锁（不推进调度，短写事务由 SQLite 串行化）；门定义**冻结**——行创建后 revision/body/options
以行为准，后续 Run 传不同 Confirm 不改写（悬而未决时换问题是更坏行为；合法变更走 K4 代次）。

## §13.2 验收样例测试

| 样例 | 测试 | 断言 |
| --- | --- | --- |
| 确认前退出 | `TestK2ExitBeforeDecisionWaitsThenCompletes`（真子进程 os.Exit 90） | 重开 PendingDecisions 恰 1 且 TaskID/Revision/Body/Options 与 spec 全等；a/b 零执行零 invocation；Answer→Run 后各恰 1 执行/1 admitted/0 pending、全 finished；恰 1 行 answered；事件序 created→answered→admitted×2 |
| 确认后退出 | `TestK2ExitAfterDecisionRunsSuccessorWithoutReAsk`（真子进程 os.Exit 91，Answer 后未跑后继） | 重开 pending 空、恰 1 行 answered、事件恰 created+answered（**不再问**）；invocations 仍 0；Run 直达完成、a/b 各恰 1 执行/1 admitted、行与事件零增长 |
| 旧 ID | `TestK2StaleDecisionIDsAreRefused` | 不存在 ID→`ErrDecisionStale`；另一 run 的有效 pending ID→`ErrDecisionStale`；两 run 各自行零变化、两门 id 不同 |
| 重复答复 | `TestK2AnswerReplayAndConflict` | 同 ID 同 choice→回执三字段全等（`AnsweredAt.Equal`）、行与事件零变化；同 ID 异 choice→`ErrDecisionConflict`、原答案原时间戳保持、事件零变化 |
| 终态答复 | `TestK2TerminalRunRefusesAnyAnswer` | 全部完成后重发已接纳的同 choice→`ErrRunFinished`；行与事件零变化 |

补充：等待非环错误＋重复 Run 一行一事件零 invocation（`TestK2RunReportsWaitingNotCycleError`）；越权 choice
描述性拒绝且行保持 pending、随后合法 choice 仍可接纳；executor 失败重跑不再问（恰 1 行 1 created 事件、a 重试
2 次 b 1 次、各恰 1 admitted）；PendingDecisions 不含 answered；v2→v3 迁移（终态库 DROP decisions＋
user_version=2 仿 v2 文件→重开表重建、版本 3、runID/任务状态/有效结果/admitted/事件原样、可继续 Run）。
**K1 全部既有用例（四注入点崩溃、相对路径、空 ID、忙 Close、URI 隔离等）原样全绿**——累计验收。

## 主代理亲验（2026-09-21，最终树上）

- 亲读全部 diff 与两个新文件：判定序、事务边界（建门/答复各一短事务、事件同事务）、懒建热路径、
  门检查位次（环检查之前）、migrate 三分支（0/2/1/newer）——与合同逐条对上。
- `gofmt -l internal/runner/` 空；`go vet ./...` 零输出；`go build ./...` OK；
  `GOOS=windows`/`GOOS=linux` 交叉编译 OK。
- `go test ./internal/runner/ -count=1` → ok（K1 15 项＋K2 11 项全 PASS）；`-race` → ok（2.6s）。
- 全仓 `go test ./... -count=1` 全 ok（validate 391.9s 为既有慢套件）。internal/runner 未接线
  cmd/harness/runtime（grep 证实），故无需重建二进制；Python 侧零改动、全套未重跑。
- **主代理自建三条独立探针**（copy 进树 `-run TestReviewProbe` 全 PASS 后删除）：①门挂起时顺序推进整体
  停住（独立后序任务不跑；前序自由任务先跑后停）＝K2 边界行为实证；②门定义冻结（后传不同 Confirm 不改写
  问题、越权 option 被拒、冻结 option 可答）；③run 未终态时已消费门同 choice 重放得原回执
  （①/③判定序交互：中途 a 完成后停 b 门，a 门重放非 ErrRunFinished、事件仍恰 1）。
- 模型调用 **0**。

## 未引入（7 步法报告）

无额外绑定机制（门定义冻结即答案，不加 Confirm 摘要列）；无 decisions 额外索引；无 Answer 的 advance 锁/
防御性并发机甲；无 run 空判分支（空 run 按判定序自然落入 ErrRunFinished）；无 native_interactions 预建表；
无并行门（K3）；无代次递增（K4）；无 CLI 接线。`newInvocationID`→`newID128` 为改造既有者（阶梯第 2 步）
而非新建；测试复用 markerExecutor/markerLines/countInvocations/taskState/loadEvents/recordingExecutor/
mustCreate 与 TestMain reexec 模式。

## 边界与如实声明

- **K2 调度仍是单线序列（K3 前界）**：门挂起时整个推进停在第一个被选中的门上——按 task_id 排序在门之后的
  **独立无依赖任务也等待**（探针①实证）。这是顺序调度器的语义而非缺陷；并行推进与多门共存属 K3。
- Answer 在 CreateRun 之前调用（空库）落入 ErrRunFinished 而非专门错误：调用顺序误用，不加防御分支。
- 已 answered 行的 answer/answered_at 无 NOT NULL 约束（CHECK 只限 state 值）；手改状态造成的不一致不在
  项目边界内，重放路径遇坏时间戳返回描述性错误。
- Windows 锁仍仅交叉编译验证（沿用 K1 声明）。

## 后续

K2 待外部评审；下一单元 K3（并行、汇合和成功结果保留）。

---

# 【返修 2026-09-21】外部评审驳回 → 修复（本节为准）

## 评审驳回（对象 029f2b7）

- **F1/P1**：确认要求由本次可选字段控制——Run 只在 `sp.Confirm != nil` 时查门，CreateRun 不持久化
  "任务需要确认"声明；恢复漏传 Confirm 即整体跳过门检查（含已有 pending 请求的路径），a/b 执行返回 nil。
  **主代理以评审探针亲复现**（两条子路径均成立）。
- **F2/P1**：所有合法答案都放行——门读 state 不读 choice，Answer(id,"reject") 落盘后重开 Run 仍执行
  [a b] 返回 nil，与 approve 完全同效。**亲复现成立**。
- **F3/P2**：空选项被固化成永远无法回答的问题——CreateRun 接受空 Options 并落盘，任何答案非法且无法纠正。
  **亲复现成立**。
- 评审全文与探针：`/private/tmp/k2-review/review.md` 及 `snapshot/internal/runner/k2_review_test.go`。
  评审同时确认：K1 收尾三项（相对路径/空 ID/在途 Close）复验通过；确认前后真实进程退出、旧 ID、重放/冲突、
  终态答复、真旧库 v2→v3 迁移（用 4822b8d 代码生成现场）均通过。

## 修复（子代理实施，主代理定合同并逐处复核）

- **F1 门持久化**：门改在 **CreateRun 事务内物化**（materializeDecisions——每个带 Confirm 的 spec 落
  pending decisions 行＋decision_created 事件，与 runs/tasks/deps 同事务）；Run 的门检查改为**无条件纯读**
  `decisionGate(ctx, t)`：无行＝无门照常执行／pending＝ErrWaitingDecision／answered＝按 F2 语义。
  `sp.Confirm` 在 Run 中完全不读（字段注释改写：创建时声明并持久化，恢复 specs 可省略，持久定义权威、
  不做事后差异校验）；"创建后未跑即退出"由建库即有门天然覆盖。懒建 gateDecision 改造为纯读并更名。
- **F2 类型化拒绝语义**：`Confirmation` 增 **`Allow string`**（授权执行的选项；不硬编码英文词、不建规则
  引擎）；decisions 表加 `allow TEXT NOT NULL`；门读到 answered 行时 `answer == allow` 才放行，否则返回
  新导出哨兵 **`ErrDecisionRefused`**——受控任务与后继零执行，行不变，重跑/重开读同一持久答案稳定复现
  拒绝。`decisionEvent` 增 Allow 字段（created 事件随问题带上）。Answer 判定序①②③④一字未动。
- **F3 创建前校验**：CreateRun 在 BeginTx 之前逐 spec `validateConfirmation`，拒绝三种畸形（空 Options／
  空 Allow／Allow∉Options），各自描述性错误；拒绝后零残留，修正后同目录重试正常。
- **schema 处置**：029f2b7 的 v3 未获验收、已验收基线仍是 v2，故**直接修订 v3 增量**（decisions DDL 加
  allow 列）而不升 v4；迁移机制不变（v0＝基线＋增量／v2→v3 仅增量／v1 拒开／>3 拒开），注释写明依据。

新增测试：F1 两条子路径（未跑即重开／等待态重开，恢复用去 Confirm 的 specs→均等待零执行，Answer 后仍用
无 Confirm specs 完成链）；F2 拒绝跨重开稳定阻断（两次 Run 均 ErrDecisionRefused 零执行零 invocation，
行 answered reject、事件恰 created+answered；对照 approve 正常完成）＋**子进程样例**（k2Child "refused"：
跑到等待、Answer("reject") 后真实退出码 93，parent 重开 Run 得拒绝、零执行、不再问）；F3 三变体零残留
（重开无 runID、runs/decisions 计 0）＋修正重试。k2Child 退出码 90/91/93＝正常场景、92+＝接线错误。

## 返修后亲验（主代理，最终树上）

- 亲读全部 diff（恰 internal/runner 四文件；runner_test.go/crash_test.go 的 K1 部分零改动）：
  物化/校验位置（BeginTx 前＋创建事务内）、decisionGate 三分支、sp.Confirm 不再被 Run 读取、
  v3 原地修订注释——与合同逐条对上。
- `gofmt` 空、`go vet` 零输出、`go build ./...`、Windows/Linux 交叉编译 OK。
- `go test ./internal/runner/ -count=1` ok（K1＋K2 全量含三个新场景）；`-race` ok。
- 全仓 `go test ./... -count=1` 全 ok（validate 409.7s 慢套件照跑）。
- **评审探针改造后亲验**（F1/F2 补规格新增的必填 Allow 字段，断言保持评审原样；跑 `-run TestReviewK2`
  全 PASS 后删除）：F1 两条子路径＝Confirm-free Run 得 ErrWaitingDecision、执行 []、pending 1；
  F2＝reject 落盘重开 Run 得 ErrDecisionRefused、执行 []；F3＝CreateRun 拒绝（该探针以"创建被拒"为
  通过路径，即修复语义本身）。另加两条主代理补充检查（同文件同跑）：门在 CreateRun 后、任何 Run 前即
  持久存在（pending=1）；拒绝后的重放语义（同 choice 返原回执、异 choice 得 ErrDecisionConflict——
  拒绝无法被异答案翻案）。
- 模型调用 0。

## 残余边界（如实）

- 拒绝是稳定终态：K2 无"撤销拒绝/重新提问"路径（合法变更走 K4 代次）。Run 对已拒任务每次返回
  ErrDecisionRefused，任务行留在未完成状态。
- 恢复时**新增** Confirm（原声明无门）被忽略（无行＝无门）——持久定义权威、不做双向差异校验，与评审
  给出的"读取原定义或拒绝"中的前者一致，已注释声明。
- K2 仍是单线调度：门挂起/拒绝时整个推进停在第一个被选中的门（原披露边界不变，并行属 K3）。
- v3 只存在过未验收形态，开发期 v3 库不作兼容对象（注释已声明）。

