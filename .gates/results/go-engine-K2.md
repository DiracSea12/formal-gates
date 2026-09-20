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
