# go-engine K5 记录：完整 harness-v1 组合与原生交互路由（轻装）

基线＝K4 终态 `33f7707`（四审 PASS）。K5 行（方案 §13.2）：接入同一批领域函数、监督器和
两宿主适配，完成全链正常/异常/取消/归档场景。机主 2026-09-21 明示派发（"这个改完就开始
K5 吧"）。

## 合同要点（主代理定，子代理实施）

- 组合层为**唯一新层**，落 internal/harness 导入 internal/runner（内核不反向引用，无环）；
  **内核零改动**为默认，缺缝须停下上报而非绕开——实际零改动达成（git diff internal/runner
  为空），无缝上报。
- harness-v1 阶段图（方案 §1）落为内核任务图：confirm（门）→ product-review ∥
  technical-review → development ∥ (qa-design → qa-review) → candidate（candidateAdmission）
  → approve（门）→ gate-review。确认/并行汇合/候选门/返修环四机制全部走内核路径。
- Executor＝受监督原生调用：job 目录、native-request 意图、已验收监督器（`harness
  supervise`）、回执核验（VerifyProcess 证据契约）、`harness native` 承载适配器（DSH/ZCode，
  组合层不言协议）。
- **领域语义实质接入**（K4 评审点名缺口）：workerOutcome 动作映射——accept 接纳／blocked
  接纳为 BLOCKED 事实（等待，非终态、绝不 PASS）／unresolved-findings 接纳后由候选门拒绝并
  驱动返修环／continue 同调用续航（§8.1：不动任务/输入/调用记录，不占业务轮次）。
  repairAllowance 以 0 基轮次绑为 RepairPolicy；候选拒绝路由＝development 或 qa-design
  分支返修（§3 分支局部返修）。
- Takeover＝§8.2 分类序：SESSION_UNAVAILABLE→零值事实（restart 请求等 restart-session/
  abort）；可验证结果（含回执核验＋终态 worker 结果；CONTINUE 不可采纳）→Adopt 原身份；
  监督器存活（PID＋创建时间 ±2s，§8.1 整数不算数）→Observe；其余（无法证明执行过）→零值
  由用户处置，绝不盲重启。
- Cancel（§8.1）：先停派发（base context 取消内核 Run）→再对在途监督器进程组 SIGTERM
  （监督器落 130 取消回执）→全部回执落地才报告结算，未落齐不宣称完成。
- 归档：终态判定后走已验收 retention 判定路径（settlement/destination/成员准入分批/索引
  骨架）；归档不是活动状态转换；物理归档（SQLite 备份＋zip）留在已验收 retention 路径。

## 交付（8 个新文件，internal/harness，共 3,633 行含测试）

compose.go（阶段图/配置/生命周期/状态分类/规格缓存＋outcome 轨迹再推导）、
compose_execute.go（监督化执行＋回执核验＋workerOutcome 映射＋CONTINUE 续航＋held 交互
路由）、compose_drive.go（Drive 循环/分类/返修路由（幂等标记）/repairAllowance 策略/终态
判定/Cancel）、compose_takeover.go（三分类＋进程身份核验）、compose_archive.go（retention
判定路径）、compose_proc_unix.go / compose_proc_windows.go（进程组 SIGTERM／Windows 桩）、
compose_unix_test.go（四族场景）。

四族场景测试（全部确定性、真实 formal-gates 二进制监督真实原生 job 进程、真实 SQLite、
崩溃族真实进程退出；宿主假体复用适配器既有 TestScriptedPeer，无第二个 fake）：

- 正常：TestComposeNormalChainGreenEndToEnd（全链绿＋归档断言）、
  TestComposeHeldInteractionRoutedAndAnswered（held 交互路由经 NativeRespond）、
  TestComposeZcodeHostCarriesAStage（zcode 宿主承载一阶段）。
- 异常：TestComposeRepairLoopToAllowanceApprove（FAIL→环至额度→问询→允许→完成）、
  TestComposeRepairRefusedAborts（拒绝→ABORTED 仍可归档）、
  TestComposeContinueResumesSessionWithoutRepairRounds（CONTINUE 续航不占轮次）、
  TestComposeCrashAdoptVerifiableResult／TestComposeCrashObserveLiveSupervisor（真实进程
  退出后重开）、TestComposeSessionUnavailableAsksRestart（restart-session/abort 两分支）、
  TestComposeFailNotCoveredBySiblingPass（development-fail 与双分支两子测试）。
- 取消：TestComposeCancelSettlesInFlightCalls（真实 SIGTERM、130 回执、停派发断言）。
- 归档：TestComposeArchiveRefusesNonTerminalRun（RUN_NOT_TERMINAL）＋正常族终态→retention
  全走断言。

## 亲验（主代理，最终树上）

- 亲读四个核心实现文件全文＋测试文件结构（阶段图边、executeLocal 候选门、CONTINUE 循环、
  分类序对内核 settle 顺序的处理、返修路由幂等、Takeover 三分类与 PID＋创建时间规则、
  Cancel 顺序、retention 路径走法）。
- gofmt 空、go vet ./...、go build ./...、Windows/Linux 交叉编译 OK。
- **四族场景 13/13 亲跑 PASS**（-count=1 -v，52.9s）；race 家族亲跑 PASS（另录）。
- go test ./internal/runner -count=1 ok（K1–K4 用例零改动）；go test ./internal/harness
  -count=1 ok（126.4s，全包）。
- git diff internal/runner 为空——**内核零改动确认**。模型调用 0（全部确定性假宿主）。

## 实现方偏离（主代理逐项核可并披露）

1. **候选拒绝以无回执执行错误呈现**（"candidate admission refused: …"），随后由返修修订
   接管 pending invocation；approve 门绝不在未准入 candidate 上开启。核可——内核零改动约束
   下的诚实形态；admission 事实持久化于 outcome 记录，路由/重开幂等成立。
2. **BLOCKED 呈现为 StatusBlocked**（非终态等待事实，绝不 PASS）。核可——参考分支序。
3. 无回执/未知结局统一落入 takeover 三分类。核可——§8.2 UNKNOWN 行的机制兑现。
4. repair/restart 拒绝后的 ABORTED 判定读组合层 presentation 记录（surfaced）——内核 settle
   顺序会先报过早的门等待。核可——组合层只读自己呈现过的问题，不猜外来问题。
5. 归档仅走判定路径，DatabaseLogicalSHA256 留空（物理归档属已验收 retention 路径）。核可，
   注释如实。
6. 测试 helper-process owner（TestComposeOwner）＋自带 env 的 peer wrapper，复用既有
   TestScriptedPeer 夹具。核可——无第二个 fake。

## 残余边界（如实）

- **真实宿主验收未做**：方案要求"K5 及发布门再使用真实宿主验收"，本轮全部为确定性夹具
  证据，不冒充原生实跑（评审同口径）；真实宿主/真实模型调用须机主逐次授权，另行安排。
- 物理归档（SQLite 备份＋zip 写出）不在组合层，属已验收 retention 路径（Python 侧持有文件
  系统事实）。
- 终态判定的 evaluateRoute 以空 Checks＋gate-review 结果作 seal 判定（组合层范围）；带检查
  结果的完整路由属最终入口切换阶段。
- compose-specs.json 规格缓存为组合层私有记录（原子写、崩溃可由 outcome 轨迹再推导），
  非内核绑定事实。
- K1–K4 与既有 harness 用例零改动；Python 侧零改动。

**K5 待外部评审。**最终入口切换（§13.3，LangGraph→Go）在 K5 验收后另行与机主对齐。

---

# 【真实宿主验收 2026-09-21：机主授权实跑，绿链达成（本节为实跑记录）】

## 授权与范围

机主于 K5 汇报后明示授权（"授权"）。范围＝组合层真实宿主绿链：product-review 走真实
DSH 绑定、其余 5 个 worker 阶段走真实 ZCode 绑定（一条链同时验证两宿主适配器实跑）；
花费限额＝过程确认按允许项作答、返修最多允许一轮、restart 询问一律 abort、held 交互取
第一选项；25 分钟上下文超时兜底。探针为 env 门控常驻测试
`internal/harness/compose_live_test.go`（`FORMAL_GATES_LIVE_ACCEPTANCE=1` 才运行，发布门
可直接复跑；无变量时零开销跳过）。真实工件目录持久化（失败现场可复查）。

## 三次实跑（总花费 12 个真实原生 turn：DSH×3、ZCode×9）

- **run 1（4 turn）**：技术审（ZCode）与产品审（DSH）成功推进；development/qa-design
  （双 ZCode 并行波）落 restart 询问被限额策略 abort。现场被 TempDir 清除，仅留日志。
- **run 2（2 turn，工件持久化后）**：技术审（ZCode）真实模型完美作答（accept/PASS）；
  product-review（DSH）turn 正常（exit=0、有身份）但无 outcome → restart 询问 → abort。
  **定因**：DSH worker 返回完全符合 schema 的答案（PASS、findings 空），但自发填了
  `check_ids:["workspace-empty","no-changes-required"]`——组合层以空已知检查集调
  ValidateWorker，任何 check id 都判 "unknown check" 拒绝。run 1 的失败同路径（推断：
  同错误形态同询问类别；工件已失，如实标注为推断）。
- **run 3（6 turn，简报补检查合同后）：绿链达成**——六个 worker 阶段各一次 turn 全部
  accept/PASS（product-review=DSH，其余=ZCode），候选准入，approve 门放行，**终态
  HARNESS_COMPLETE 断言通过**；期间一次**真实 held 交互**（interaction/requestPermission）
  经组合层路由 surfaced 并由 NativeRespond 以 allow_once 作答（原生交互路由实跑验证）。
  归档调用因探针把目的地写在 run 目录内被 retention 判定正确拒绝（"output must be
  outside the run"）——**该拒绝本身即是 destination 判定在真实状态上的验证**；探针
  目的地已改至 run 外（发布门复跑用）。归档 walk 的完整路径（settlement/成员准入/索引）
  由确定性测试覆盖（TestComposeNormalChainGreenEndToEnd），未为它再烧真实调用。

## 修复与发现

1. **修复（简报检查合同）**：stageBrief/repairBrief 增加 `authorized_checks: []` 与
   instructions（"本 run 无授权检查，check_ids 必须为空数组"）——worker 无法从结果
   schema 单独得知空集，属合同告知缺口而非模型错误。子代理实施，主代理亲核 diff，
   四族确定性场景 13/13 复跑绿。
2. **发现（交评审裁量，未擅改语义）**：被 worker 契约拒绝的答案走"无回执错误→接回
   咨询零值→restart 询问"路径——处置安全（用户决定、不盲重跑）但**问询措辞误导**
   （会话失效类问题掩盖确定性契约拒绝；§8.2 UNKNOWN 行的前提"是否执行过无法证明"
   并不成立：exit.json/answer.json 都在）。本轮以简报合同消除主因；语义修正（如
   接回咨询区分"契约拒绝"与"结局未知"）留待评审定夺。

## 实跑花费台账（如实）

| run | turn 数 | DSH | ZCode | 结果 |
|---|---|---|---|---|
| 1 | 4 | 1 | 3 | 波 2 落 restart 询问，限额 abort |
| 2 | 2 | 1 | 1 | 定因样本；restart 询问，限额 abort |
| 3 | 6 | 1 | 5 | **绿链 HARNESS_COMPLETE**＋真实 held 交互＋归档 destination 判定 |
| 合计 | **12** | 3 | 9 | — |

本会话至此模型调用 0→12（全部属本次授权的实跑；DSH 会话有 usage_evidence 落盘，
ZCode 走宿主计划计量）。

## 评审返修（首版被驳回后的全面重做，2026-09-21）

**评审结论（`/private/tmp/k5-review/review.md`，候选 `e193b37` vs 基线 `33f7707`）：FAIL，
8 P1＋1 P2**——R1 业务 FAIL 可跑成 COMPLETE 且末端 FAIL 无可执行返修；R2 材料/冻结候选/
检查集/harness-result.json 全缺；R3 候选不存在即可批准；R4 返修简报不携带当轮失败材料；
R5 abort 重开后丢失；R6 恢复调用不受管理；R7 重开不核绑定；R8 无物理归档；R9（P2）契约
拒绝被误标会话失效。评审核心判断：**参考实现 `runtime/langgraph/formal_gates_harness/graph.py`
（293 行）就在仓库里，K5 没有完整保留其流程结构与业务语义——"照抄都要抄错"**。机主追加
两条红线：①不要打表式修测试，要把流程图整个逻辑搞清楚搞顺；②不要让人一眼看出照抄
LangGraph（项目用于求职）。

### 重做设计（主代理定合同，子代理实施）

**验收判据＝值级语义等价＋地道 Go 双面等价**：既不丢 graph.py 的业务语义（节点序、条件
边、材料传递、终态），也不复刻其文本/词法形状（不含 Python 惯例、注释不引参考出处）。
核心映射：

- **ask-task 模式**：每个用户决策＝`<task>:ask` 后缀任务（无依赖，业务任务依赖之）——
  开放问题挂链＝中断编码，问题在内核 decisions 表持久化（复用 K2/K4 全套 §6.1 规则），
  ask 任务上的 ReviseTask＝问题答案驱动流转。终态判定＝record.json 终态字段 AND
  harness-result.json 文件（两者缺一不算完成）。
- **三账本一记录**：revisions.jsonl（字节载体）、asks.jsonl（幂等标记）、answers.jsonl
  （写前 WAL）＋ binding.json（R7 重开核对：RunID/需求/项目/基线逐字段命名报错）＋
  record.json（会话与终态）＋ dispositions/（停驻说明）＋ outcomes/（每 (task,generation)）。
- **内核唯一新缝**：`Store.OpenQuestion`（88 行）——当前代次挂待决问题、挂起期间幂等、
  已答返 ErrQuestionAnswered，复用 insertDecision/Answer 既有规则。接回咨询六分类
  （pending 问→observe；session-refused→零值；可验证结果→Adopt；契约拒绝→运行时问询
  retry/restart-session/abort；PID＋创建时间存活→observe；无证据→零值问用户）。
- **两条修订通道**：结构重试环（pre-review/qa-design/development-findings/blocked/
  runtime/check-runtime）走裸 ReviseTask 不占额度；业务返修**仅** evaluate→repair→
  RequestRevision 走 0 基 repairAllowance 额度策略（graph.py:291 route 语义），返修简报
  在完成时从 durable evaluate 反馈包派生（R4）。
- **收据重放**：内容寻址 {role,brief,revision,generation} 摘要→崩溃后重放不重花钱；
  CONTINUE 同会话续航；SESSION_UNAVAILABLE→session-refused 标记→K4 restart 请求；
  契约拒绝（exit-0＋answer.json 违反 worker 契约）→运行时问询（R9 语义修正落地）。
- **物理归档**：VACUUM INTO 一致备份＋成员准入批判定＋索引骨架＋写后重读验证＋临时文件
  rename 发布＋ARCHIVE_EXISTS 拒绝覆盖（R8）。
- **取消**：recordedLiveJobs 按 PID＋创建时间核验扫描含前任的活监督器，回执齐才报结算。
- **QA 配对检查**（graph.py:136 语义）：qa-review 汇合处同时检查 review **与** design 的
  needs_repair。

交付 9 个 workflow*.go（≈4,900 行含测试）替换 compose 核心；旧 compose_*.go 六文件删除。

### 主代理亲验与修出的真 bug

1. **语义亲读**：workflow.go/workflow_worker.go/workflow_route.go/workflow_drive.go/
   workflow_verify.go/workflow_takeover.go 六文件逐文件对照 graph.py 确认映射完整。
2. **出处注释清理**：41 处"参考实现的…"式注释（求职红线）→ 派回原代理清理至 0，亲核。
3. **评审探针亲自移植**（k5_review2_test.go，9 探针；只改机械面，期望值零改；移植适配
   逐处注释披露：RepairBrief 改门审驱动、ArchiveCreatesArtifact 删无 Git 子项、
   RecoveredHeld 收敛到观察扫描集断言、GateRepair 双门 FAIL、PrematureApproval 改早期
   问题面断言）。**修出三个真 bug**：
   - **QA 配对缺失**（真实缺陷）：parkOnResult 只查 qa-review 自身 needs_repair，漏
     design 侧（graph.py:136）→ 子代理修＋回归测试 TestWorkflowQADesignFailReviewPassAsksDesignRetry。
   - **evaluate 反馈包空列表**（P1 级）：evaluateRoute 返回 `[]string` 而 listOf 只断言
     `[]any`→failed_checks/gate_findings 恒 null——返修路由照常触发但修复轮拿到的持久化
     失败材料是空的（R4 同类，四族测试无一断言包内容故漏网）。
   - **归档清单同型 bug**：archiveInventoryOp 的 files 同为 `[]string`→"成功"归档只剩
     run.db 备份、丢弃全部成员文件（既有测试只断言 FileCount≠0，为 1 也过）。
   后两处同根因（typed-slice 与 listOf 域不匹配），单子代理一次修复（消费点正确断言，
   净 +8 行）；ArchiveCreatesArtifact 探针顺势**加强**为成员数 >1（披露：超出评审原文
   表面，匹配"真实验证过的工件"意图）。
4. **探针移植自身的修正**（如实）：RepairLimit=0 被 ValidateSpec 拒（合法下限 1）→用 1；
   Cancel 夹具监督器 Process.Release 后成僵尸、kill(pid,0) 恒真→断言改收据证据
   （exit.json＋130 取消码，与既有取消族测试同一口径）；NoWrite 条目补 Status/Summary。

### 验证（全绿）

9/9 探针亲跑 PASS；harness 全包 -count=1（166s）＋-race（368s）ok；runner 包 ok；
gofmt/vet 干净；windows 交叉编译＋本机构建 OK；全仓 `go test ./... -count=1` 全部包 ok。
模型调用 0（本返修零真实宿主调用；实跑证据见上节 run 3 绿链）。

### 实现方偏离（核可披露）

候选拒绝＝致命停而非问询（每个可拒路径已在 worker 环内问过）；BLOCKED＝用户停驻；
终态判定 record+文件双证据；归档留物理层文件；夹具复用假宿主 peer；探针三处机械适配
如上逐处注释。

## 二轮评审返修（六发现全修＋三方输入的架构收敛，2026-09-22）

**二轮评审（`/private/tmp/k5-review2/review.md`，候选 `a65f6a7`）：FAIL，5 P1＋1 P2**——主图已确认补齐
（正向探针过），但异常边与恢复路径未对齐：R1 重开重复消费旧 evaluate 判断→并发返修＋提前 COMPLETE；
R2 任务逻辑名与目录名混用→门审处置/恢复权限中断；R3 check-runtime 问询 []string 过编码边界崩；R4 答
restart-session 后仍带旧身份；R5 接回把检查/门审当普通 worker→丢领域结果；R6 计划确认引用的工件
不存在。机主取回第二意见（外部 AI）纠正了主代理的错误前提——**参考实现并非"没有恢复能力"**：
infra.py Store.job 按原 key 接回（intent 同则不重启监督器、无收据拒绝盲跑）、test_driver.py:153 真实
杀 controller 断言副作用恰一次、handover.py 未结算拒启第二写入者、workers.py 收据按 key 复用、
graph.py 恢复重放走同一业务函数——R1/R4/R5 的正确归因是"Go 造了第二套实现/权威在文件"，不是
"参考没有"。主代理逐条亲核五处引文确认后按三方收敛执行。

### 返修结构（四阶段，每阶段亲验后进下一个）

- **Phase 0 探针与约束先行**：评审独立探针原样入树（k5_independent_test.go，机械搬运＋出处头，
  期望零改）＝7 FAIL 基线；派发不变量/轨迹对照框架（k5_invariants_test.go：单活写入者、evaluate
  单驱动、COMPLETE 零未结算、结果绑候选、重建换身份五断言＋中断/不中断轨迹对照＋行为对照表），
  先于修复落地防打表。
- **Phase 1 发现全修**：R2/R3（dispositions 枚举读正文逻辑名、jobs 路径三处补 pathSafe、问询构造
  统一过 canonicalBody；顺带修出第 4 处混用 relJobPath——带冒号任务的回执验证一直被静默跳过）；
  R1/R4/R5＝单一接纳链（收据→按类型核验→领域结果→统一接纳，live 与恢复共用：settleCheckResult/
  finishGate/settleWorkerResult；adoptable 第四套接纳删除；幻影 turn 目录一并消除）；R1 消费绑定＝
  内核世代前移 ∨ task_revisions 字节绑定 ref 双事实合取；R4＝内核 granted restart 行单权威，
  答案事务内同清 task_sessions；R6＝计划确认指向真实 outcomes 记录。
- **Phase 2 六账本回库删除（反屎山主线）**：内核 v5（task_revisions 修订输入字节、task_sessions
  会话、task_dispositions 停驻、runs.terminal 终态；0/2/3/4 全起点加法步进自检）；revisions/
  answers/asks 三个 jsonl、dispositions/ 目录、record.json 全删；R4 投影桥（granted→record 清会话）
  删除；outcomes "哪个结果有效"从 glob 排序改内核事实定代（无旧代回退）；路由答案判读从账本影子
  改 decisions 行（TaskQuestion/AnsweredQuestions 只读出口）。文件系统只余载荷与证据（jobs/
  outcomes/artifacts/events）。顺带修出两个存量 bug：停驻不盖代次戳（同 attempt 跨代静默重放旧答案）、
  takeover 运行时问题答后再问一次。
- **Phase 4 屎山审计**：700 符号两轮普查仅 1 孤儿（stageTaskOf）；14 个死机制名零命中；七项控制
  事实单一权威逐项 grep 佐证；行数 4,898 vs 返修前 4,906——**删六账本＋第四接纳的减量恰好抵消内核
  表＋接纳链的增量，净 -8 行而架构严格变简**；可读性四问（做到哪/为何等/为何有效/重开为何不重做）
  通读核验可答，头注补文件地图（注释级）。

### 验证

评审探针 9/9（含正控）；不变量/轨迹对照全绿（两条中断轨迹与对照逐项一致）；harness 全包 -count=1
多轮绿（330-350s）＋race 绿；runner -race 绿；vet/gofmt 干净；全仓 -count=1 全包 ok；交叉编译未回退
（build ok）。已知偶发：全包 9 轮中 1 次失败、无现场（失败轮未留 -v 输出），后续 8 轮皆绿，未定位
——如实交评审观察。模型调用 0（全程零真实宿主调用）。

### 偏离与发现披露

第二意见的"业务图可读性＋ask 状态简化"收缩为审计内注释级核验（评审原话"无需围绕纯风格建议再展
开独立返工"；深夜段全绿树上做结构重组风险大于收益，结构问题如出现单独再派）——四问核验通过，
仅补头注文件地图。存量 bug 修复两处（代次戳、双问）无既有测试依赖旧形态（grep 佐证）。测试助手
同源化（账本读→内核读）三处，断言强度不减反增（内核读覆盖任意世代）。

## 三轮评审返修（完整业务过程收敛，2026-09-22）

**三轮评审（`/private/tmp/k5-review3/review.md`，候选 `464a68f`）：FAIL，3 P1＋2 P2**——上轮探针全部
确认通过、账本回库被认可为实质改善；新发现集中在**恢复时的非成功结果、多次调用身份、多次问询**：
R1 不同失败共用 `@0:0:runtime` 问题身份→旧 retry 答复自动批准新调用（P1）；R2 恢复的门审 BLOCKED
直调 finishGate（只写结果不分类）→被 evaluate 当业务返修自动修代码（P1）；R3 观察器只记 task、
latestTurnDir 跨 invocation 取最大 turn→旧调用 turn-02 压过当前调用 turn-01，权限请求无法呈现
（P1）；R4 已知 FAIL/exit-127/已完成 CONTINUE 全被折叠成"会话丢失"（P2）；R5 不变量助手把内容
摘要当判断身份，同内容两判断合法返修被误报（P2）。评审架构判断：**共用结果写入函数≠共用完整
业务处理过程**——非成功→用户决定的转换仍在 live/consult/route 三处分别解释。

### 修复（两代理并行：核心收敛＋判据修正，均主代理亲验）

- **完整分类进三入口**：settleWorkerResult/finishGate/settleCheckResult 各自补全
  continue（parkContinue：记会话身份＋park "continue"，route 自动再武装续做）/
  blocked（parkWithResult→retry/abort，R2 的同一待处置分支）/
  unresolved-findings/verdict（防篡改后接纳）。live 循环、**收据重放**（旧重放路径会把
  BLOCKED/CONTINUE 当判决接纳——一并修复）、takeover adoptTurn 三路全部经此；admission 三值
  （adopted/parked/none）把业务含义传回 consult：接纳＝Adopt 原身份、等待处置＝Observe＋standing
  disposition（route 放置问题）、继续＝Observe＋continue park、真正未知＝零值问询——四种区别
  贯穿（R2/R4）。
- **R1 身份合同**：失败唯一身份＝(task, invocation generation, turn/attempt, kind)，park/openRuntimeQuestion
  单点盖章，问询正文携带；同一失败重放复用同一答复，不同失败换代次发新问题（实证两次恢复的问题
  分别 `product-review@1:1:runtime`/`@3:1:runtime`，无新答复时不再自动第三次调用）。
- **R3 身份贯穿**：observed 改 task→invocationID 钉定（consult 本就知道），watchedDirs/liveObservations
  只跟随该 invocation 的最新 turn；latestTurnDir 退出生产路径（跨 invocation 最大 turn 不可作
  "当前调用"选择器）；Cancel 保留全树扫描＋身份核验。
- **R5 判据修正**：k5EvaluateSingleDriverFindings 改按判断代次计——每 ref 的消费修订行数 ≤ 发布过
  该 ref 的判断数（鸽笼：行超判断必有判断双消费；同内容多判断各走各轮合法），与产品消费绑定同构。
- **P3 落实**：删除 routeRepair 恒死的 `generation>outcome.Generation && repairDriven(ref)` 防重分支
  及 repairDriven（latestOutcome 读内核当前代次后该分支不可达；消费绑定即代次事实本身）；全扫
  "SAME/idempotent"夸大注释改到行为满足（adoptGateTurn/openRuntimeQuestion/placeQuestion）。
- **偏离披露**：①测试夹具假 host 改为同时应答 session/new 与 session/resume（恢复续做在新
  invocation 的 attempt-1 上合法 resume 记住的会话，旧假设会挂起恢复调用；评审探针期望零改动）；
  ②check-runtime 场景同时呈现 check 处置问题＋一个从未启动调用的零值 restart 询问（内核 K4 设计
  行为，探针断言均满足）；③contractRejected 扩为"任何收据未能框出可接纳业务答复"（与 live
  parkTurnFailure 对齐）；④续做再武装走 revision（内核无恢复 pending invocation 的接续缝，业务
  身份经 task_sessions 保留，实证续做调用 resume 原会话走到 HARNESS_COMPLETE）。

### 验证

TestReview3 全绿（5 测试类，期望零改动）；四族回归全绿；harness 全包 317s 绿＋race 绿；runner
race 绿；vet/gofmt 干净；全仓零 FAIL；windows 交叉编译 OK。模型调用 0。

## 四轮评审返修（边上下文贯穿，按参考规则表修复，2026-09-22）

**四轮评审（`/private/tmp/k5-review4/review.md`，候选 `20183f9`）：FAIL，2 P1＋4 P2**——前三轮全部
原场景确认通过；机主定调本轮方法论："节点和箭头对了，边上的上下文答错，流程仍然是错的；修复经常
只覆盖眼前症状，没有把同一条业务规则贯穿到底"（实证：三轮 R4 的续做走 revision 恰造成本轮 R5 的
不变量误报）。发现：R1（P1）BLOCKED→retry 静默新会话（四角色全中——rememberSession 只在 accepted
后跑、gate 每次空身份起）；R6（P1）已完成 run 重开再执行检查（route 先于终态判定＋检查轮次惰性
跨终态同步）；R2（P2）CONTINUE 恢复保会话丢 unfinished-work 反馈；R3（P2）门审比较材料生成了但
task.json 无 comparison 字段；R4（P2）repairRounds 把停驻 repair 结果计为完成轮次；R5（P2）不变量
把每个修订引用行当业务消费。

### 修复方法：先产规则表（合同的脊柱），修复对规则不对探针

从 graph.py invoke_worker（66-104）逐条抽出九行规则表（每条边携带什么上下文、什么只能一次），
六发现各自映射到违反行。四个混用概念显式分离：**调用次数（turn）/任务代次（generation）/重试次数
（attempt）/业务返修轮次**——业务轮次唯一权威＝内核 user_requests 业务 revision 行（RequestRevision
落、state=executed；与内核额度 used 同一聚合，新增只读出口 Store.RevisionRounds）；普通 retry/continue
走裸 ReviseTask 不落该表，天然可分。

- **会话单一更新点**：settleWorkerResult/finishGate 顶部 rememberSession（分类之前，对齐 graph.py:85
  无条件更新）；两类 retry 显式分界——新审查轮（pre-review/qa-design/gate 返修重审）显式清会话，
  同一工作的 retry（worker-blocked/runtime/continue）不清（R1）。
- **续做反馈单一组装**：continuationFeedback（unfinished-work 包装＋initial_feedback）live 循环与
  恢复再武装共用；parkContinue 记 parked outcome 供重组装（R2）。
- **终态边界**：Drive 顶部判 runs.terminal，先于任何 route/revise/launch；检查轮次同步
  （advanceCheckRounds）移到返修落定时刻，routeCheckBriefs 只余无操作复查（R6）。
- **门审材料接入**：comparison 目录＋逐文件 proof 写进 task.json（对齐参考 workers.py），重放一致性
  由既有 saved-material 相等检查保证（R3）。
- **业务轮次读点**：repairRounds→RevisionRounds；不变量消费计数改 k5BusinessRevisionInputs
  （task_revisions JOIN executed user_requests），裸 ReviseTask 绑同 ref 不算业务行（反例入伪造测试）
  （R4/R5）。
- **P3**：finishGate/executeGate 的不可达 CONTINUE 分支删除（合同只许 development/repair CONTINUE）。
- **夹具诚实性**（评审点名假宿主 new/resume 同回 s-1 掩盖 R1）：held-after-retry 补 resume 应答步；
  两处树内测试改按**实际方法＋输入身份**断言；k5RestartAnsweredFindings 改走内核事件全序（events.seq
  无墙钟）修掉对合法"重启后新结果记身份"的误报。

### 验证（主代理亲跑）

TestReview4 全绿（7 测试类含正控，期望零改动）；五族回归零回退（234.7s）；harness 全包 370.7s 绿；
race（Review4/Review3/Independent）279.2s 绿；runner race 绿；vet/gofmt 干净；全仓零 FAIL。模型调用 0。
零 schema 变更（全部复用 v5 既有表）。偏离四项如实（新轮次显式清会话是允许中的选择；不变量判据
重写超 R5 字面系会话记录扩展的必然后果；两处函数内减量；未做真实宿主验收）。
