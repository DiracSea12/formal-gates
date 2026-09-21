# K3 并行、汇合和成功结果保留（轻装模式记录，2026-09-21）

方案 §13.2 K3：并行、汇合和成功结果保留；最小验收样例＝一个任务失败或等待，独立任务继续；
两个确认乱序答复；汇合只使用齐备结果。按机主轻装口径执行；7 步简洁法＋质量维度沿用
（本轮起合同额外明写注释纪律：权威出处一处、调用点短注释、不堆叠——P3 教训）。
基线＝K2 收口源 `70731b4`（含 P3 注释精简 `d8e3a36`）。

## 交付（internal/runner：runner.go 重构＋parallel_test.go 新增＋三处小改）

| 部分 | 职责 |
| --- | --- |
| `Runner.Parallelism int` | 本地有界并发（§4.1，注释明言无分布式租约/选主）；**零值/≤1＝顺序执行，K1 行为不变**；`limit := max(r.Parallelism, 1)` |
| `readyTasks`/`readyBatch` | 就绪集＝原 nextReady SQL 去 LIMIT 1（同 NOT EXISTS 依赖门控、ORDER BY task_id）；readyBatch 逐候选过滤：本轮已派发跳过／缺 spec 沿用 K1 报错／decisionGate nil 入派发集、ErrWaitingDecision/ErrDecisionRefused **跳过不派发**、其他错误上抛。nextReady 保留为薄包装（K1/K2 测试直接调用，签名语义不变） |
| 派发循环 | 就绪集按 task_id 序派发至上限，每任务一个 goroutine 走 `runTask`＝既有 §4.1 全路径（registerIntent 短事务→事务外执行→admit 短事务，四个崩溃注入点原样挪入）；完成经无缓冲 channel 回收、腾槽即重算就绪集。executor 错误包装为无回执失败记入 execErrs、**不中断兄弟**；非 executor 错误记 fatal、停止派发、排空在途后 join 返回（零值路径与 K1 立即返回逐字等价）。`attempted` map 保证在途/无回执任务本 Run 内不重派（失败任务保持 K1 语义：下次 Run 顶替重试） |
| `settle` | 排空后终态判定固定序：①executor 失败（errors.Join 聚合）→②存在 pending decisions→`ErrWaitingDecision`→③存在 answered 且 answer≠allow→`ErrDecisionRefused`→④未完成无就绪→既有停滞/环错误→⑤nil。**门不再打断有独立工作的推进**（§4.1"某任务等待确认时，不阻塞无依赖的其他分支"）——K2 记录披露的边界就此正式反转；在 K1/K2 既有用例的依赖链形态下可观测行为不变 |
| `decisionStatus` | decisions.go 新增单查询双 COUNT 子查询，供 settle 读持久行（不依赖本轮遍历史） |

无新表、无迁移、无新事件种类、无取消传播、无 worker-pool 类型；新增运行时状态仅
`attempted map[string]bool` 与 `workerResult{err,noReceipt}`。worker 间无共享可变状态
（hooks 只在 Run 前设置、运行期只读）；advance 锁不变（同时仍只有一个 Run）。

## §13.2 样例测试（parallel_test.go）

| 样例 | 测试 | 断言要点 |
| --- | --- | --- |
| 失败不阻塞＋结果保留 | `TestK3FailureDoesNotBlockIndependentTasks` | a 失败（真实执行后无回执）、b 独立、c join：Run 返含 boom 错误；b admitted+finished+marker 1；a 恰 1 pending 0 admitted；c 零执行零登记。重开换健康 executor：Run nil；a marker 2（失败+重试）、**b 仍 1（成功保留，effective invocation 跨重开不变）**、c 恰 1；各任务恰一次 admitted |
| 等待不阻塞 | `TestK3WaitingDoesNotBlockIndependentTasks` | a 门 pending、b 独立：Run 仅执行接纳 b（invocations 总数 1），返回 ErrWaitingDecision；a 零登记（门先于登记）。Answer 后 Run 全完成 calls=[b,a] |
| 两确认乱序答复 | `TestK3DecisionsAnsweredOutOfOrder` | a、b 各带门、c join：首 Run 双门 pending 零执行；**先答 b**→Run 仅 b 完成、仍等待、c 零登记（半齐备不汇合）；**再答 a**→Run nil、calls=[b,a,c]、各恰一次 admitted |
| 一次接纳不等同批 | `TestK3AdmissionDoesNotWaitForTheBatch` | parallelism 2，a 阻塞 b 秒完：hook+channel 同步断言 b 已 admitted+durable（1 事件、finished+effective）**同时** a 仍 running+1 pending、c 零登记；释放后 Run nil、三任务各恰一次 |
| 有界并行 | `TestK3ParallelismIsBounded` | parallelism 2 两 executor channel 会合证明重叠（10s 守卫）；零值与 -3 原子重入计数＋2ms 窗口证明不重叠 |
| 崩溃恢复（真实 os.Exit 94） | `TestK3CrashDuringParallelRunRecovers` | parallelism 2、a 秒完、b executor 永阻塞不写 marker、c join；afterAdmit 在首个接纳提交后退出。父重开：崩溃点 a 恰 1 admitted+marker 1；恢复后 a/b/c 各 marker 1、恰 1 admitted、0 pending、全 finished+effective、3 个 admitted 事件与 effective 一致；b 崩溃时"已登记未回执/未登记"两种时点都不被断言区分 |

K1/K2 全部既有用例零改动原样全绿（runner_test.go/decisions_test.go 零 diff；crash_test.go 仅
TestMain 追加 5 行；退出码 70–73/90/91/93 与 80–84/92 未动，K3 子码 94 正常/95+ 接线）。

## 主代理亲验（2026-09-21，最终树上）

- 亲读 runner.go 全文逐路径推演（fatal 与 settle 出口分流、无 goroutine 泄漏、attempted 单
  goroutine 访问、hooks 时序、settle 优先序）；亲读 parallel_test.go 协调点与三个小文件 diff。
- `gofmt` 空、`go vet` 零输出、`go build ./...`、Windows/Linux 交叉编译 OK。
- `go test ./internal/runner/ -count=1` ok；`-race` ok；`-run TestK3 -count=3` ok（崩溃场景稳定）。
- 全仓 `go test ./... -count=1` 24 包全 ok 零 FAIL（validate 慢套件照跑）。
- **主代理三条独立探针**（copy 进树 `-count=3 -race`＋`-count=1 -v` 全 PASS 后删除）：
  ①三任务 parallelism 2 高水位恰为 2（上限达到且不超）；②settle 优先序一角——上游 executor 失败
  ＋下游门 pending → 返失败而非等待；③对角——一门 refused 一门 pending → 返等待（可行动优先），
  答复等待门后下次 Run 浮现 ErrDecisionRefused。
- 模型调用 **0**。

## 实现方披露的偏离（主代理核可）

1. settle ②③ 读 decisions 持久行（decisionStatus）而非就绪集遍历内存标记——与内存标记在全部
   既有形态下等价；差异仅"门任务本轮从未就绪"的人造情形（报持久等待事实而非环错误），与 §4.1
   "待处理决定"表述一致。**主代理核可**（探针③同源验证）。
2. 非 executor 错误（存储层）记 fatal、排空后 join 返回——零值路径与 K1 逐字等价，不泄漏
   goroutine。**核可**。
3. nextReady 保留为薄包装（K1/K2 测试直接调用不许改）。**核可**。

## 边界与如实声明

- settle 优先序为固定设计：失败＞等待＞拒绝＞停滞。等待（可行动）优先于拒绝（终态）：mixed 态下
  先报等待，答复后下次 Run 浮现拒绝（探针③钉住）。
- 同一时刻仍只有一个 Run（advance 锁）；并行仅指 Run 内多个就绪任务并发执行。同一任务不会并发
  执行（attempted＋任务状态门控）。
- executor 失败的任务在本 Run 内不重试（保持 K1 顶替语义），跨 Run 重试。
- Windows 锁仍仅交叉编译验证；internal/runner 仍未接线 CLI。
- gate pending 的任务即使上游未完成也会被 settle 报为等待（持久事实优先）——§4.1 一致，特此披露。

## 后续

K3 待外部评审；下一单元 K4（局部代次、返修额度、失效范围及调用接回）。
