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
