# K1 Go 内核首单元（轻装模式记录，2026-09-20）

方案 §13.2 K1：单所有者、任务事务、顺序执行和结果接纳；最小验收样例＝两个顺序任务，
在登记意图后、取得结果后、提交前后四处退出，恢复无漏执行、无重复接纳。按机主轻装口径执行
（测试＋独立复核＋全量回归＋单提交，不跑 acceptance/admission 仪式）；机主本轮追加指定：
`prompts/actions/development-worker.md` 的 **7 步简洁法**逐字作为实施纪律（规格未要求不写→
复用既有→标准库→运行环境自带→已装依赖→一行→最小实现；只约束写法取舍，不得借简削减验证/
错误处理），质量维度＝正确性/需求覆盖/可读性/简洁/可维护/架构解耦/性能/防过度设计与屎山。

## 交付（internal/runner，六文件 1435 行，全新包）

| 文件 | 职责 |
| --- | --- |
| `store.go` | Open（目录 flock 独占→run.db→前向版本化迁移 user_version）；DSN 沿用 sqlprobe 已验收组合（WAL/foreign_keys/busy_timeout 5s/_txlock=immediate）；schema v1：runs/tasks/task_deps/invocations/events＋一条真实读取路径索引；CreateRun 单事务 |
| `lock_unix.go` / `lock_windows.go` | 标准库 flock／x/sys LockFileEx（已交叉编译验证）。不复用 internal/coordination 的 pid/token 心跳锁——那是产品期跨命名空间合同；K1 要的是进程死亡内核自动释放（7 步法第 3 步胜出，注释记录依据） |
| `runner.go` | §4.1 循环：恢复（仅顶替 pending＋复位 running，不重放历史）→就绪计算（单条 NOT EXISTS SQL，顺序执行）→短事务登记意图（128 位随机调用 id＋sha256 输入摘要）→事务外执行（`Executor` 函数类型，错误＝无回执）→短事务接纳：身份四元组事务内核对（调用 id/run/task/代次/摘要）→ pending 接纳（结果＋任务完成＋有效结果指针＋同事务事件）；admitted 幂等拒绝；superseded 迟到结果只记录不动有效结果。原始调用记录不可覆盖 |
| `runner_test.go` | 9 用例：依赖门控、二次接纳幂等拒绝、迟到结果不改有效结果、身份伪造拒绝、executor 失败留 pending 待顶替、锁互斥、事件与接纳一一对应、约束（幽灵依赖/重复行/孤儿调用 FK/一目录一 run/user_version/WAL） |
| `crash_test.go` | §13.2 样例四注入点（①意图后②结果后③提交前④提交后），测试二进制自重执行＋真实 os.Exit（码 70–73），父进程断言：marker 副作用执行计数（①=1/②③=2/④A 不重跑）、每任务恰好一次接纳、pending 清零、superseded 保留、有效结果指向 admitted 行、事件恰 2 且与接纳一致、重开成功即证 flock 随进程死亡自动释放 |

**未引入**（7 步法第 1 条，证明防过度设计）：decisions/native_interactions 表、并行与汇合、
代次递增规则（列留位恒 1）、waiting_interaction/canceled/unknown 状态、§4.3 领域工作流接口
（K1 用 Executor 函数类型驱动；接口在后续 K 单元按真实需要成形）、任何 CLI 接线、任何
harness/产品包导入。**依赖**：仅标准库＋已锁 modernc.org/sqlite v1.36.1＋x/sys v0.30.0（仅 Windows 路径）。

## 主代理亲验（2026-09-20，最终树上）

- 亲读全部六文件：事务边界（登记/接纳各一短事务、执行在事务外）、身份复核在事务内、
  事件与接纳同事务、恢复不重放历史、注释解释取舍而非复述代码——与合同逐条对上。
- `gofmt -l internal/runner/` 空；`go vet ./...` 零输出；`go build ./...` OK；
  `GOOS=windows`/`GOOS=linux go build ./internal/runner/` 交叉编译 OK。
- `go test ./internal/runner/ -count=1` → ok（1.347s，含四注入点崩溃用例）。
- 全仓 `go test ./...` 与 Python 全套见索引当轮（Python 侧零改动，跑一次作累计记录）。
- 模型调用 **0**（确定性 executor，无宿主、无原生）。

## 边界与如实声明

- `crashHooks`（四个 nil 函数字段）是崩溃测试唯一注入口：①③两点无法从 Executor 回调到达，
  这是命中 §13.2 四注入点的诚实途径；生产恒 nil。
- `Run(ctx, specs)` 每次重传任务 specs：§7 规定正文不入库、库中只有输入摘要，恢复进程必须
  重新提供输入；缺 spec 的就绪任务报错而非盲跑。
- Windows 锁仅交叉编译验证，未真机运行（项目边界内）。
- K1 的 executor 是进程内函数；跨进程监督与原生接回属后续 K 单元（K1 的"执行"语义＝函数调用，
  崩溃恢复以重跑＋顶替覆盖，恰好一次接纳由事务保证）。

## 后续

K2（精确流程确认与持久等待）在本候选上加能力；G3b 返修正待外部评审复核。

---

# 【返修 2026-09-21】外部评审驳回 → 修复（本节为准）

## 评审驳回（对象 6697cb2）

- **F1/P1**：恢复未核对任务原输入——A 完成、B 中断后以变更输入恢复，保留旧 A、执行新 B 仍报成功
  （缺任务定义的持久绑定与恢复核对）。**主代理以评审探针亲复现**（三条 FAIL 之一）。
- **F2/P2**：同 Store 双 Runner 并发 Run——后者无条件 recoverPending，把前者在途调用标 superseded
  并重跑同任务（两次 Run 均 nil、执行 2 次）。**亲复现成立**。
- **F3/P2**：SQLite URI 未编码路径——`run#one`/`run#two` 的 `#` 被当 fragment，两目录锁独立却连
  同一数据库。**亲复现成立**。
- 评审全文与探针：`/private/tmp/k1-review/review.md` 及同目录 `snapshot/internal/runner/review_test.go`。

## 修复（子代理实施，主代理定合同并逐处复核）

- **F1**：`tasks.input_digest TEXT NOT NULL`（任务**定义**绑定；与 invocations 的调用级摘要角色
  分开，DDL 注释互指）；**schema v2**——v1 拒开（"predates task input binding; K1 development
  schema — recreate the run"：未发布、摘要无法回填，重建即诚实）；`Run` 在 **TryLock 之后、
  recoverPending 之前** `verifySpecs`：逐 spec 核对任务存在与摘要，不一致指名拒绝；被拒的 Run
  连恢复写入都不做。
- **F2**：`Store.advance sync.Mutex`（必须挂 Store——挡不同 Runner 实例共享 Store；目录 flock 挡
  跨进程，两层注释写明）。`Run` 以 TryLock 取锁全程持有，取不到返回**导出哨兵 `ErrAdvancing`**
  ——主代理复核裁定：评审给的选项是"串行化**或拒绝**"，静默 nil 退让不在其中且掩盖调用失误；
  亲核探针只断言执行计数，显式拒绝同样通过；阻塞串行会死锁"先返回再释放"的调用方结构，故取拒绝。
- **F3**：`dsn` 用 `url.URL{Scheme:"file", Path: filepath.ToSlash(path)}` 标准百分号编码（`#`→`%23`）；
  查询串为固定常量原样拼接（不过 Values.Encode，避免括号转义改变 modernc 解析）。

新增正式测试：输入变更全生命周期拒绝（首跑/部分完成后/未变更恢复/v1 拒开）、同 Store 并发推进
拒绝（双 Runner 与同 Runner 重入，第二个 Run 得 ErrAdvancing、执行恰 1、首完成后可正常取锁）、
URI 字符目录隔离。

## 返修后亲验（主代理，最终树上）

- 亲读全部改动 diff 与最终落位（ErrAdvancing/verifySpecs/schema v2/dsn 编码）。
- `gofmt` 空、`go vet` 零输出、`go build ./...`、`GOOS=windows`/`GOOS=linux` 交叉编译 OK。
- `go test ./internal/runner/ -count=1` ok；`go test -race` ok。
- 评审探针（copy 进树跑 `-run TestReview` 后删除，不入仓库）：**三用例全部 PASS**
  （F2 探针日志如实显示第二个 Run 得 `ErrAdvancing`、执行恰 1——探针只断言计数，与"拒绝"选项相容）。
- 全仓 `go test ./...` 与 Python 全套见索引当轮。模型调用 0。

## 残余边界（如实）

- 四注入点中"取得结果未提交"仍会重跑（进程内 Executor 的已披露边界；恰好一次接纳由事务保证，
  原生副作用恰好一次属后续 K 单元的宿主恢复验收——评审亦如此认定）。
- v1 拒开是有意决定（未发布开发 schema，无迁移路径）。
- Windows 锁仍仅交叉编译验证。

---

# 【二轮复审 2026-09-21：PASS＋三个 P2 修复（本节为准）】

完整复审（对象 `8226fa5`，同时重查相对 `464aad1` 的完整实现）：上轮三项确认关闭；
**结论 PASS（无 P0/P1，按 reviewer-base 规则）**，保留三个独立复现的 P2 修复项
（`/private/tmp/k1-review-v2/review.md`，三项均主代理以探针亲复现）。评审同时认可：
职责划分/事务边界/可读性尚好、无新过度设计、v1 拒开处置正确、进程内 Executor 重执行边界沿用上轮认定。

- **N1/P2 相对路径回归**：URI 编码前未转绝对路径，`Open("child")` 报
  `SQL logic error: out of memory`（child 落进 URI authority 位；旧版成功）。
  修复：Open 入口 `filepath.Abs` 一次，锁与数据库共用同一条绝对路径再编码；错误文案随改引绝对路径。
- **N2/P2 空 run ID**：`CreateRun(ctx,"",…)` 成功但与内存哨兵冲突，纠正重试写第二个 run，
  重开报 `database holds more than one run`。修复：写事务前一个 if 拒绝
  （`runner: run id must not be empty`）；拒绝后有效 ID 重试正常。
- **N3/P2 在途 Close**：Close 不参与推进守卫——executor 在途时 Close 释放目录锁，第二个 Store
  可接手重跑（执行 2 次，原 Run 最后报 database is closed）。修复：Close 以 `advance.TryLock()`
  加入同一守卫——忙碌返回 `ErrAdvancing`（db 与目录锁原样不动）；成功路径 db.Close() →
  lock.unlock() → advance.Unlock()（期间互斥自持，防半关 Store 上启动新 Run）；不阻塞
  （executor 自调 Close 会死锁，评审点名）。

新增正式测试：相对路径解析（隔离 cwd＋绝对写法同目标）、空 ID 无毒化重试、在途 Close 保持
所有权（ErrAdvancing＋二次 Open 得 ErrRunBusy＋释放后 Run 完成）。

## 二轮修复后亲验（主代理，最终树上）

- 亲读 diff（恰 store.go 三个修复点＋runner_test.go 三个用例；runner.go 零改动）。
- `gofmt` 空、`go vet` 零输出、`go build ./...`、Windows/Linux 交叉编译 OK。
- `go test ./internal/runner/ -count=1` ok（1.196s）；`-race` ok（2.227s）。
- **两轮评审探针共七项**（v1 三项＋v2 三项断言＋DSN shapes 日志；v2 的 review_test.go 与 v1
  逐字节相同故只取一份）copy 进树 `-run TestReview` **全部 PASS**，验后删除（ls 确认六正式文件）。
- 全仓 `go test ./...` 全 ok（见索引当轮）；Python 侧零改动（评审自跑 335 项通过）。
- 模型调用 0。
