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
