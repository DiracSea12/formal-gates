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
