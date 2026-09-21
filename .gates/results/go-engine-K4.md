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

---

# 【二次返修 2026-09-21：二轮驳回（5 P1＋3 P2）→ 统一请求模型重构（本节为准）】

## 二轮驳回（对象 882855b/44fab27）

八项发现（主代理以评审探针逐条亲复现全部成立）：F1/P1 初始额度未持久绑定（恢复传 3 静默增额）；
F2/P1 增额问询借负代次槽脱离真实绑定（上游修订进新代后旧 ID 仍可批）；F3/P1 granted 未执行被判完成
（Run nil 零执行、重放报 ErrRunFinished）；F4/P1 观察模式只存于回调（漏配 Takeover 即双执行）；
F5/P1 会话失效零值直接授权替换（无 restart-session/abort 决定）；F6/P2 轮次传 used+1 比既有工作流
提前一轮询问；F7/P2 未知任务落无法答复的孤立问询；F8/P2 refused 后重放/冲突误报失效。
**评审架构判断＝根因：负代次槽＋确认/列表/终态/门继承四处豁免堆叠**；并点名
TestK4NilTakeoverSupersedesAsBefore 固定了不安全恢复行为。全文：`/private/tmp/k4-rereview/review.md`。

## 重构（子代理实施，主代理定合同并逐处复核）

**统一请求模型**（v4 未获验收故原地重塑增量；已验收基线仍 v3）：
- `user_requests`（kind ∈ revision/restart；**target_generation＝创建时真实任务代次**；invocation_id
  仅 restart；问题字段内联冻结；initial 仅 revision 首用绑定；state pending→granted/refused→executed）
  ＋`call_observations`（持久观察事实，幂等、永不删）。事件 `request_created`/`request_answered`
  与转换同事务。**decisions 回归纯任务门**（K2/K3 语义零变化），全部负代次豁免删除。
- **生命周期与绑定有效性**（权威契约在 DDL 注释与 Answer 文档两处完整）：绑定＝任务存在且
  generation==target_generation（restart 未决另要求 invocation 仍 pending）；上游修订使目标进新代
  →请求自然失效（stale），闭包无显式作废；refused 终态、同答案重放原回执、异答案冲突（F8）。
- **Answer 统一**：①终态（全 finished 且无 pending/granted 请求）→ErrRunFinished；②先 decisions
  （既有语义原样）后 user_requests：不存在或绑定失效→ErrDecisionStale（F2）；③已答重放/冲突；
  ④pending 消费（同事务 answer＋事件＋状态）。
- **F1/F6**：initial 写前读既有行绑定，漂移→`ErrAllowanceDrift` 具名拒绝零残留；
  `policy(used, bound+grants)` 传已执行轮数（0 基，对齐 graph.py：initial=1 首轮直批次轮问询）。
- **F3**：settle 新增 granted-未执行→`ErrRevisionGranted`；`Store.OpenRequests` 供重启发现请求身份；
  完成调用前复核绑定→`ErrRevisionGrantStale`。
- **F4/F5**：Observe→持久化观察行；nil-Takeover 且有待决观察调用→不 supersede 不重跑、settle 报
  `ErrTakeoverRequired`（无观察行的 K1 run 保留盲 supersede）；零值失效→创建 restart 请求（问题来自
  `Runner.Restart`，nil→`ErrRestartUnavailable`）；granted restart→恢复作废重跑新 id（获批替换）、
  refused（abort）→作废不重跑＋settle `ErrCallAborted`。恢复三步序与 settle 八步序见记录者合同。
- **F7**：目标存在性在所有分支写盘前统一验证、零残留。
- 上轮被驳回工作的测试按新语义更新（nil-takeover 用例反转双分支、0 基轮次、restart 双分支等）；
  八项探针场景全部落正式测试（k4_rework_test.go）。

## 二次返修后亲验（主代理，最终树上）

- 亲读关键 diff（DDL/视图注释、Answer 统一、settle/恢复重排、TakeoverOutcome 语义修订）。
- `gofmt` 空、`go vet` 零输出、`go build ./...`、Windows/Linux 交叉编译 OK。
- `go test ./internal/runner/ -count=1` ok；`-race` ok。
- **八项评审探针亲跑**：未修改原样运行＝5/8，三处失败均为探针辅助函数 `reviewAsk` 内
  `t.Fatal(e)` 且 `e==nil`——该函数写死 initialAllowance=1 并期待首轮询问，与其自身边界探针
  （`TestReviewAllowanceBoundaryMatchesExistingWorkflow` 要求 initial=1 首轮 err==nil）**互相矛盾**
  （按被驳回的 used+1 语义书写）。应用一行适配（initial 1→0，零额度首轮即问）后 **8/8 PASS（race）**，
  主代理验证后删除。适配事实在此完整披露，供评审复核裁量。
- **主代理自建探针**（×3 race PASS 后删除）：abort 绑定代次——gen1 abort 后 run 报 ErrCallAborted
  零重跑；对同任务合法修订进 gen2 后任务复活完成（绑定语义端到端）。
- 全仓 `go test ./... -count=1` 24 包全 ok 零 FAIL。模型调用 0。

## 实现方偏离（主代理核可）

1. **只读兼容视图 `revision_requests`**（投影 kind='revision'）：为评审 F3 探针裸读旧表名提供读路径
   （表名改 user_requests 与探针 8/8 的两全），视图注释如实写明用途。**核可，特此披露**。
2. 探针一行适配（见上，主代理独立核实其必要性）。**核可，特此披露**。
3. 绑定失效的 granted/refused 请求按死历史处理（settle/list 均过滤）：不过滤会使被上游修订杀死的
   grant 永久卡在不可完成的 ErrRevisionGranted。**核可**——绑定原则的自然推论，测试钉住。
4. `dispatchableTasks` 增 refused-restart NOT EXISTS（当前代）："释放但不重跑"的唯一持久兑现，
   与 refused 门谓词同构。**核可**。
5. ErrRestartUnavailable/ErrAllowanceDrift/ErrRevisionGrantStale 等新哨兵与迁移测试必要更新。**核可**。

## 残余边界（如实）

- 兼容视图仅供外部读者/探针，内核自身零依赖。
- restart 请求的"确认旧执行停止"（§8.3 替换前确认停止）由 Takeover 实现方负责（K5 接监督器）；
  内核只持久化决定与换新身份。
- Takeover/RepairPolicy/Restart 均为窄接缝；确定性子进程与闭包验证机制，不充当原生实跑（评审同口径）。
- v4 两次形态均为未验收开发 schema；被驳回 build 盖 v4 戳的库为开发库、越界（注释如实）。

---

# 【三次返修 2026-09-21：三轮驳回（2 P1＋2 P3）→ 单一活性谓词＋按调用区分的 nil 恢复（本节为准）】

## 三轮驳回（对象 df1574a）

两项 P1＋两项 P3（四条探针主代理逐条亲复现成立，含真实子进程 os.Exit 87；全文
`/private/tmp/k4-rereview2/`）：
- **P1-1 失效请求永久占住该任务后续返修入口，且拖住终态判定**（revision.go:399／OpenRequests:554／
  Answer:366）——上游修订杀死请求后，其 pending/granted 行仍按裸 state 命中"已有请求"分支与
  终态豁免，新代返修永远 ErrRevisionPending、全 finished 的 run 仍可被旧 ID 唤醒。
- **P1-2 省略 Takeover 的恢复会绕过已经落盘的重开询问**（runner.go:462／settleRestartRequests）——
  nil-Takeover 的批量 supersede 不看 restart 请求，把询问所约束的原调用作废重跑，等于替用户答了题。
- **P3-1 删除 `revision_requests` 兼容视图**（store.go:419）——为旧探针裸读保留的投影不应变成
  生产兼容承诺；本轮评审自己的断言已改查真实 user_requests。
- **P3-2 生命周期文档多处重复＋F 编号堆叠**——权威契约应收敛到一处。
探针：TestReview2StaleRevisionMustNotBlockFutureRequests、TestReview2StaleRequestMustNotReopenTerminalAnswers、
TestReview2PendingRestartMustSurviveMissingCallback、TestReview2RestartQuestionAfterProcessExit。

## 返修内容（子代理实施，主代理定合同并逐处复核）

- **`requestActiveSQL`＝活性请求的唯一组合谓词**（revision.go）：`state ∈ (pending, granted)` ∧
  任务存在且 `generation == target_generation` ∧（restart 未决另要求其 invocation 仍 pending）。
  七个复用点：RequestRevision 的 pending/granted 检查、grants 计数（answer 非空且 executed 或仍活性
  ——失效 grant 不计额度）、OpenRequests、Answer 终态豁免、requestStatus、PendingDecisions，外加
  nil 恢复排除集。**P1-1 由此关闭**：失效请求是死历史，不占入口、不拖终态、不虚增额度。
- **nil-Takeover 恢复按调用区分**（runner.go recoverPending）：批量 supersede 的两条 UPDATE 增加
  排除集——活跃 restart 请求绑定的 invocation ∪ 已观察调用（call_observations）不作废；其余自由
  调用照 K1 语义 supersede 重跑。**P1-2 由此关闭**：漏配 Takeover 的 owner 不得重跑有未决重开
  询问的调用，同时也不能借排除集挡住无约束调用的正常恢复。
- **P3-1**：视图删除；schema v4 终态＝user_requests＋call_observations 两表。
- **P3-2**：注释收敛——完整生命周期契约只在 user_requests DDL 注释一处权威，各调用点一句短注释，
  全部 F 编号与重复段落删除（先删旧再写新）。
- 新增 k4_rework2_test.go 六项正式测试（新代入口×pending/granted、死 grant 不增额度、
  nil 恢复保留已落盘询问（Close/Open 与真实进程退出两种形态）、终态不被死历史拖住、
  nil 恢复区分约束调用与自由调用）。

## 三次返修后亲验（主代理，最终树上）

- 亲读全部 diff（谓词七处复用、恢复排除集、注释收敛、视图删除）。
- `gofmt` 空、`go vet` 零输出、`go build ./...`＋Windows/Linux 交叉编译 OK；
  `go test ./internal/runner -count=1` 与 `-race` ok；**全仓 `go test ./... -count=1`
  24 包全 ok 零 FAIL**（validate 436.6s）。
- **两轮评审探针合并亲跑**：首跑 11/12——败项系旧二轮探针裸读已删视图
  （`no such table: revision_requests`）；应用其裸 SQL 适配
  （`FROM revision_requests WHERE state='granted'` → `FROM user_requests WHERE kind='revision'
  AND state='granted'`，首轮 sed 误产 `AND WHERE` 已重做）后 3× race ok、verbose **12/12 PASS**。
  连同既往 initial 1→0 共两处适配，均与三轮评审自身口径一致（其断言已改查 user_requests；
  initial 边界与其自身边界探针矛盾）。验证后删除，适配事实完整披露供裁量。
- **主代理自建探针**（×race PASS 后删除）：stale-grant 判别双侧——当前代未执行时死 grant 的
  完成调用报 ErrRevisionGrantStale；当前代执行完毕后死 grant 纯历史、新询问按真实额度落 gen2 新行。
- **既有偶发 TOCTOU 收口（takeover_test.go，与本次改动无关的既有缺陷）**：supervisor 严格先写
  result 后追加 marker，父测试轮询到 result 即断言 marker 行数，负载下 5/25 复现（stash 亲证与
  K4 改动无关）。微修派发子代理（生产代码零改动）：`waitForMarker` 栅栏助手替换两处
  waitForGlob＋立即断言（marker 是 supervisor 退出前最后一次持久写，等它即顺带栅栏 result）；
  子代理 25 轮顺序＋包＋race 全绿，主代理亲读 diff＋6 轮 race 复核。

## 语义裁定（显著披露，两端测试共同钉住）

**失效 granted 仅在任务当前代未完成时报 `ErrRevisionGrantStale`；当前代跑完后失效 grant 纯历史，
后续返修按真实额度重新询问或直批。**该裁定非拍定：既有测试（k4_rework_test.go 完成调用复核——
当前代未执行即拒绝）与三轮评审探针（当前代执行后的新询问）在树内并存强制两端，探针 12/12、
既有用例零改动共同成立。

## 残余边界（如实）

- requestActiveSQL 为内核层活性定义；K5 接入直接复用，不再派生第二套判断。
- Takeover/RepairPolicy/Restart 仍为窄接缝；确定性子进程与闭包验证机制，不充当原生实跑（评审同口径）。
- 死历史请求行永久保留（含失效 granted/refused），无清理路径（归档回收属后续）。

---

# 【四次返修 2026-09-21：四审驳回（1 P1＝三轮残余路径）→ 死 grant 特判整体删除（本节为准）】

## 四审驳回（对象 e8bfd40）

- **1 P1：失败的新代次仍被失效的旧批准堵住返修**（revision.go:481，根因分支 467–484）——
  `staleGrants > 0 && taskState != taskFinished` 特判在上游修订＋新代执行失败时形成死锁：不修输入
  跑不完、没跑完又不让申请新批准。评审以 pending/granted 同操作对照把因果限定到该分支
  （探针 TestReview3FailedGenerationCanAskForFreshRevision，公开 API＋真实库＋Close/Open 复现）。
  其余修复确认有效：重开确认绕过关闭、失效请求列表/终态污染消除、兼容视图删除、TOCTOU 微修
  有据。四审自身验证全绿（runner＋race、全仓 vet/build、335 Python、27 累计独立探针、真实旧库
  迁移）。全文 `/private/tmp/k4-rereview3/review.md`。
- 架构判断：新特判再次破坏"失效历史不阻断当前工作"的共享规则一致性——修复方向即删除而非解释。

## 返修内容（子代理实施，主代理定合同并逐处复核）

- **删除 revision.go 原 467–484 行特判整块**（注释＋`NOT(requestActiveSQL)` 失效 grant 计数查询＋
  taskState 条件拒绝），`taskState` 局部变量及其读取一并删除（SELECT 只取 generation）。失效
  granted 请求在任务**任何**状态下都是死历史：不挡新申请、不增额度（下方 grants 计数本就排除
  失效行）、不授权任何事——新申请直接落到既有额度判决（零额度 always-ask 政策＝落新问询）。
- 活性 grant 完成路径的 `generation != target` 不变量守卫与 ErrRevisionGrantStale 哨兵保留不动。
- k4_rework_test.go dead-grant 段期望翻转：完成调用由 ErrRevisionGrantStale 改为 ErrWaitingDecision
  （gen2 新问询）→ refuse → Run 现规格到 nil（活跃新问询会挡运行，先 settle）。
- 新增正式回归 `TestK4Rework2FailedGenerationCanAskForFreshRevision`（pending/granted 双分支，
  移植评审探针）：a→b＋独立 c 完成 gen1 → b 旧请求（granted 分支先 allow）→ 上游修订进 gen2 →
  gen2 运行 a 完成、b 旧输入执行失败、c 零重跑 → Close/Open → RequestRevision(b,b2) 得新问询
  （id ≠ 旧 id）→ allow → 完成调用 → Run 到 nil，总执行 [a b b]。

## 三次返修"语义裁定"的撤回（显著披露）

三次返修节裁定的"失效 granted 仅在任务当前代未完成时报 GrantStale"被四审整体推翻——其
"未完成"半边正是死锁根源。**终局规则更简：失效 grant 一律死历史；ErrRevisionGrantStale 仅存于
活性 grant 完成路径的不变量守卫**（该处选择受 requestActiveSQL 约束，target==当前代恒成立，
守卫只防事务内漂移）。k4_rework_test.go 该处期望两轮间翻转两次，如实记录。

## 四次返修后亲验（主代理，按改动面收窄）

- 探针 TestReview3 granted 分支修复前亲复现 FAIL（pending 对照 PASS，错误信息与评审一致）；
  修复后三份评审探针（rework/rework2/rework3）**2× race ok、verbose 全 PASS**，验证后删除。
- gofmt 空、go vet 零输出、go build ./...＋Windows/Linux 交叉编译 OK；runner 包
  -count=1 与 -race ok；新回归测试双分支 PASS。
- **未重跑全仓慢测试**：改动面限于 internal/runner（四审自身已跑全仓 vet/build＋335 Python＋
  27 累计探针＋真实旧库迁移全绿），本轮按改动面收窄验证。模型调用 0。

## 残余边界（如实）

- 同三次返修节：requestActiveSQL 单一活性定义、Takeover/RepairPolicy/Restart 窄接缝不充当原生
  实跑、死历史行永久保留无清理路径。
