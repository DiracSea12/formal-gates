# K4 局部代次、返修额度、失效范围及调用接回（轻装模式记录，2026-09-21）

方案 §13.2 K4：局部代次、返修额度、失效范围及调用接回；最小验收样例＝失败项返修、无影响项保留；
失败不被其他 PASS 覆盖；失效会话与部分工作接手沿用已验收组件。按机主轻装口径执行；7 步简洁法＋
质量维度＋注释纪律沿用。基线＝K3 收口源 `3f5e7ad`（含 P3 性能修复 `f990b07`）。

## 交付（internal/runner：revision.go 新增 209 行＋revision_test.go 813 行＋四文件小改）

| 部分 | 职责 |
| --- | --- |
| `Store.ReviseTask(ctx, taskID, spec)` | 输入/业务范围变化的显式修订原语（§4.2 Generation；两次 Run 之间调用）。前置：`advance.TryLock` 失败→`ErrAdvancing`（与 Run/Close 同守卫）；`validateConfirmation` 拒畸形门（零残留）。单事务：目标不存在→指名错误；失效范围＝目标＋传递依赖闭包（一次读全 run 边＋Go 内存反向 BFS，按 task_id 排序确定性）；闭包内每任务 generation+1、state='waiting_deps'、result_invocation_id=NULL、旧代 pending invocation 一律 superseded（行保留）、同事务 `task_revised` 事件（payload 含 old/new generation）；**仅目标任务**重绑 input_digest |
| 门继承 | 闭包内每任务：目标带新 `spec.Confirm`→用它；否则复制该任务**最新一代** decisions 行的冻结定义（revision/body/options/allow，body 重算摘要自洽）到新代（pending＋created 事件，新 id）；皆无→无门。§6.1 请求绑定局部代次——旧答案不授权新代工作 |
| `Answer` 代次判定 | 判定序插入第③条（原③④顺延）：行找到后比对 decisions.generation 与 tasks 当前 generation，不等→`ErrDecisionStale`（旧 pending 与已答行都不可再答＝失效请求明确拒绝） |
| `decisionStatus` 当前代联结 | 两个 COUNT 均加 EXISTS 联结 tasks 同代——旧代 pending 门不再悬挂等待、旧代 refused 不再拒修订后的 run；`dispatchableTasks` 谓词本就按代匹配，无需改 |
| `insertDecision` 共享写路径 | CreateRun 物化与 ReviseTask 门继承共用（K2 已验收源码的重构，行为逐字节等价，K2 用例原样通过）；避免门写入两份 |
| verifySpecs/store.go | 输入变更拒绝文案与"generation 留待后续单元"注释改指 ReviseTask（行为不变）；schema 与 schemaVersion=3 不动（tasks.generation 与 decisions 唯一键自 K1/K2 即按代次设计） |

**额度与接回的边界归属**（注释与本文如实）：返修额度判定属领域层（§130"只统计领域返修"；internal/harness
的 repair-allowance 已有），内核提供 generation 计数事实、不做额度机制；"是否允许修订"由调用方按 §6.1
用既有确认机制把关。调用接回在内核＝旧代在途意图作废＋恢复后新代执行＋迟到结果只记录（原生会话附着属 K5/job）。

## §13.2 样例测试（revision_test.go）

| 样例 | 测试 | 断言要点 |
| --- | --- | --- |
| 失败项返修、无影响项保留 | `TestK4FailureRepairKeepsUnaffectedWork` | a 失败、b 独立成功、c join；修订 a 后 a 恰一次新代执行（gen2 admitted、effective 指向）、失败 gen1 行保留为 superseded 且 result_json 空（**失败不被 PASS 覆盖**）、b 零重执行（marker/admitted/effective/总行数/gen 全不变）、c 恰一次 gen2；事件序含 task_revised(a)、task_revised(c) payload {1,2} |
| 迟到旧代结果不覆盖 | `TestK4LateOldGenerationResultIsRecordedNotEffective` | 直调 admit 持 gen1 身份→superseded 只记录，effective 仍是 gen2 调用 |
| 失效会话/部分接手（真实子进程 os.Exit 96） | `TestK4CrashedSessionTakenOverByRevision` | child 第二个意图登记后退出；parent 重开→ReviseTask→Run：被修订任务恰一次新代执行、已完成独立任务全不变、事件 [admitted, task_revised, admitted] |
| 门跨代 | `TestK4GatesCarryAcrossGenerations` | gen1 已答 allow 完成后修订→gen2 复制门 pending（定义全同、新 id）、Confirm-free specs 同样等待零执行；答旧 ID（含下游 gated 旧 ID）→ErrDecisionStale；答新门→每代各执行一次；无门任务修订后直通 |
| 旧代 pending 门不误报 | `TestK4OldGenerationPendingGateDoesNotMisreportSettle` | gen1 门 pending 修订→gen2 复制 pending→settle 报等待（表 2 行 pending）；答 gen2 后 gen1 旧行仍在表但 decisionStatus 不再等待、no-op Run nil |
| 边界 | `TestK4ReviseTaskBoundaries` | Run 在途→ErrAdvancing 零残留、结束后成功；未知任务指名错误；畸形 Confirm 零残留；修订后旧输入 Run→verifySpecs 指名拒绝（含 ReviseTask 字样）零执行 |

K1/K2/K3 全部既有用例零改动（runner_test/decisions_test/parallel_test/scalebound numstat 为空；
crash_test 仅 TestMain 追加 K4 分支；退出码 96 避开 70–95 已用值）。

## 主代理亲验（2026-09-21，最终树上）

- 亲读 revision.go 全文（闭包 BFS、门继承两分支、supersede 范围）与 decisions/runner/store diff
  （判定序重排、EXISTS 联结、insertDecision 等价重构、文案更新）。
- `gofmt` 空、`go vet` 零输出、`go build ./...`、Windows/Linux 交叉编译 OK。
- `go test ./internal/runner/ -count=1` ok（42 项 PASS）；`-race` ok。
- 全仓 `go test ./... -count=1` 24 包全 ok 零 FAIL（validate 慢套件照跑）。
- **主代理两条独立探针**（copy 进树跑后删除）：①菱形图 a→(b,c)→d 修订 b——失效恰为 {b,d}（a/c 执行
  计数与 effective 全不变，b/d 各恰两次）×3 race PASS；②同任务连续两次修订——gen 3/3、admitted 2/2
  （gen2 被跨过从未执行）、task_revised 事件恰 4、全 finished。**首轮探针②期望值写错（误算 admitted=3）**，
  亲核日志后更正为 2 并复跑通过——实现无误，探针期望错误，如实记录。
- 模型调用 **0**。

## 实现方披露的偏离（主代理核可）

1. `materializeDecisions` 重构为调用共享 `insertDecision`（已验收源码、行为等价、K2 用例原样过）——核可。
2. 样例 3 任务命名与合同行文互换（顺序派发按 task_id，被修订任务须后派发才能让独立任务先完成）——
   断言语义逐项一致，核可。
3. Answer 文档编号 1–5 重排、store.go 三处注释改写——无行为变化，核可。

## 边界与如实声明

- 修订不自动产生成功终态；被修订任务照常走门/执行/接纳。
- 修订动作本身的用户把关（§6.1 确认）与额度判定（§130）在调用方/领域层——内核不做。
- 旧代 decisions 行永久保留为历史（Answer 拒、decisionStatus 不计、就绪谓词不匹配）；无清理路径（归档回收属后续）。
- `PendingDecisions` 未加代次过滤：旧代 pending 行会出现在列表中（Answer 时被 ErrDecisionStale 拒）——
  列表语义＝"历史全部未答请求"，调用方以 Answer 回执为准；未按合同外需求加过滤。
- Windows 锁仍仅交叉编译验证；internal/runner 仍未接线 CLI。

## 后续

K4 待外部评审；下一单元 K5（完整 harness-v1 组合与原生交互路由）——派发前须与机主对齐（G4 期同款前置）。

---

# 【返修 2026-09-21：外部评审驳回 → 修复（本节为准）】

## 评审驳回（对象 b5a5020）

- **F1/P1**：返修额度没有可执行控制链——只有 ReviseTask 原语，无额度配置、业务返修计数、耗尽待答、
  答复后放行；全仓无调用方；代次不能当业务轮数（一次修订多个下游一起加代、业务范围变化≠领域返修）。
- **F2/P1**：调用接回未实现——k4Child 在 executor 启动前退出，未建立原生调用/失效会话/部分成果；
  普通 Run 恢复先把 pending 全部 superseded。
- **F3/P2**：PendingDecisions 不滤旧代失效请求——修订重开列表两个问题、旧问题答即 stale、run 完成后
  仍残留。**主代理以评审探针亲复现成立**。
- 评审全文与探针：`/private/tmp/k4-review/review.md`、`snapshot/internal/runner/k4_review_test.go`。
  同时确认：菱形闭包/独立保留/事务边界通过；K3 性能 P3 修复复验（1000 任务 0.627s）。
- **记录更正（评审指出）**：首版记录把"executor 失败注入的返修样例"描述为覆盖"失败不被其他 PASS
  覆盖"——过当。该样例只证明执行失败路径的返修与历史保留；**领域 FAIL/返修资格验证属领域层（K5）**，
  本节更正。

## 修复（子代理实施，主代理定合同并逐处复核）

- **F1 额度控制链**：schema v4 加法迁移新表 `revision_requests`（pending/granted/executed/refused；
  **计数全部行派生**：used=executed、grants=决策关联的 granted∪executed；initial 每次重传不持久化）。
  `RepairPolicy func(round, allowance int) bool` 窄政策接口（镜像 domain.repairAllowance 的
  round>=allowance 语义，K5 接真函数）。`Store.RequestRevision(ctx, taskID, spec, exhaustion,
  policy, initial)`：额度内直批（单事务 executed 行＋修订）；耗尽则单事务落**问询门**（decisions
  非正代槽 0,-1,-2,…——永不匹配任务代、UNIQUE 保每任务至多一个未决问询、已答留史）＋pending 请求，
  返回 ErrWaitingDecision；`Answer` 消费问询时同事务结算请求（allow→granted、否则 refused）；
  granted 后的下一次 RequestRevision 即完成调用（输入字节 §7 不入库故 grant 不即执行）。
  Answer ③代次检查对联结 pending/granted 请求的问询豁免 stale；①终局检查对存在 pending 问询的
  run 不报 ErrRunFinished（问询＝未竟业务）；decisionStatus：pending＝当前代∪未决问询、
  refused＝仅当前代（婉拒修订≠run 被拒）。ReviseTask 事务体重构为共享 `reviseScope`。
- **F2 接回**：`Runner.Takeover`（`TakeoverOutcome{Adopt+Result | Observe | 零值=死/失效}`，
  nil＝K1 语义逐字节不变）。recoverPending 分叉：按 (task,generation) 序**事务外**逐个咨询 pending
  invocation——Adopt→`admit` 原身份接纳零重执行；Observe→保留 pending、任务保持 running（就绪谓词
  排除）；零值→`voidInvocation` 短事务作废、重跑注册**新 invocation id**（§8.3 最新身份）。settle 在
  refused 后插入哨兵 `ErrCallObserved`（仅 Takeover 非空可触发）。三个真实子进程监督器场景（退出码
  98/99）：结果已存在→纳原调用（原 id admitted、marker 恰一次、gen 不变）；仍在工作→观察（零重派
  零执行、完成后再 Run 收原结果）；会话失效＋部分进度工件→作废重跑（新 id≠旧、读部分进度完成）。
- **F3**：PendingDecisions 加当前代过滤（当前代 ∪ 代 0 未决问询）——修订后恰一问、完成后列表空。

## 返修后亲验（主代理，最终树上）

- 亲读全部 diff：takeover.go/revision.go（RequestRevision 状态机、槽位 MIN-1、reviseScope）、
  decisions.go（①③⑤扩展与豁免规则）、runner.go（Takeover 字段/recoverPending 分叉/settle）、
  store.go v4、迁移测试最小必要修改（schemaVersion 常量＋补 DROP，意图原样）。
- `gofmt` 空、`go vet` 零输出、`go build ./...`、Windows/Linux 交叉编译 OK。
- `go test ./internal/runner/ -count=1` ok；`-race` ok。
- **评审探针三用例 copy 进树 `-run TestReviewK4` 3/3 PASS**（主代理亲跑后删除）：待答列表恰一问/
  完成后空；菱形跨重开 a=1,b=2,c=1,d=2；额度 API 可配置（gen=5 探针硬断言照过）。
- **主代理两条自建探针**（×3 race PASS 后删除）：①同任务二次问询取新槽（拒后再问恰一问、拒后
  no-op Run 得 nil 非 refused、grant 完成调用代次 2）；②被观察调用不阻塞独立分支（b finished、
  a 保持 pending 零接纳、Run 得 ErrCallObserved）。
- 全仓 `go test ./... -count=1` 24 包全 ok 零 FAIL。模型调用 0。

## 实现方偏离（主代理核可）

1. `TestK2MigratesV2ToV3` 三处最小必要修改（schemaVersion 常量化＋补 DROP revision_requests）——
   合同"测试不许改"与"v4 真迁移"冲突，取后者，测试意图保留并加强。**核可**。
2. 问询槽序列 0,-1,-2,…（同任务二次问询不撞已答 UNIQUE 行）。**核可**。
3. Answer ①终局扩展（pending 问询使 run 可答）。**核可**——否则 finished run 上问询永不可答。
4. reviseOneTask 门携带查询加 `generation>0`（自测发现问询行会污染门继承）。**核可**。
5. `validateConfirmationBody` 拆分共用＋耗尽问询 nil 且需问时具名拒绝。**核可**。

## 残余边界（如实）

- 返修计数粒度＝**任务级**（revision_requests 按任务计数；领域若需 run 级轮次，K5 由调用方聚合）。
- Takeover/RepairPolicy 是窄接缝，K5 才接已验收监督器/适配组件与 domain.repairAllowance 真函数；
  本轮测试以确定性闭包/子进程监督器验证机制，不充当原生实跑（评审同口径）。
- initial 每次重传（与 specs 重传同先例）；授权增量只经用户 grant 或更大 initial，别无加额路径。
- 旧代 decisions 行与已答问询行永久保留为历史；无清理路径（归档回收属后续）。
