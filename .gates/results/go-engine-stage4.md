# go-engine 阶段 4 记录：Git 非分片 regular 全流程（轻装）

基线＝K5 冻结态 `dedb3fa`（G4 十审 PASS）。机主 2026-09-23 新窗口令"继续接手开发"（交接
`.harness/HANDOFF-K5-STAGE4.md` 指向阶段 4；对齐提问未获答，按既定红线取轻装、五单元框
架 R1–R5 开工，机主可随时纠偏）。批次：R1 审查系列迁移（拆 R1a 纯内核＋R1b 接线）→ R2
裁决深度与唯一返修/重试 → R3 QA 管线与 FROZEN 屏障 → R4 guard 接入与三处权威切换 → R5
Git provider＋canary＋route matrix。

## 第 41 轮（2026-09-23，R1a：pre-development 审查系列纯内核，轻装）

范围与合同要点（主代理定，子代理实施）：

- 新建 `internal/engine/review`（四源文件 1360 行＋测试 1174 行），**既有文件零改动**。
  typed 三层数据按最终方案 §4.4（RawCandidate/Adjudication/CorrectionReceipt/
  EffectiveStatus）＋双 series 封闭 Set（product-review/start-readiness 恰两条，struct 非
  map）；语义权威＝总需求 §7.3.5；值级参考＝legacy `internal/validate/predevelopment_review.go`
  （主代理先全文通读，映射表入合同：21 组函数对应、15 条值级等价清单）。
- 计轮/容量（三轮自动、授权恰好一轮、整轮失效回收）、CAS 纠正（ExpectedEffectiveRevision）、
  五决定封闭集、digest 身份链、八项 Operator 核实、全量防篡改校验、PASS→阻塞级联
  （product-review→start-readiness 单向）、MarkStale（revision 变化不清额度）。
- 明确不迁移：settle-by-message 批量层（引擎逐 finding 稳定 ID 精确绑定，§6.2.4）、
  跨域依赖失效（slicing/route/QA/gates，归 Controller 接线）、NeedsReReview 门槛
  （Controller 从 BlockingFindingIDs+Revision 派生）、CLI 旗标语义（→typed Ask）。

主代理独立复核：

- 亲读四源文件，逐函数对照 legacy（含占位理由封闭集逐字一致、INVALIDATE_RESULT 的
  DownstreamImpact/PENDING 形态、授权空槽回收语义）。
- 自建 oracle 探针四场景（探针跑毕即删，不入树）：耗尽→授权→整轮失效回收；驳回阻塞+
  确认建议→PASS；CAS 零变化（JSON 字节对照）＋四类篡改负控＋runID 不匹配拒绝；级联单向
  （product-review 纠正 PASS→阻塞级联 start-readiness、反向不级联）。全过；中途两处探针
  自身期望错反而验证了重复摘要拒绝与写入前校验。
- 亲验：build/vet/gofmt 干净、包测试 28 项全绿、全仓 `go test ./...` 除
  `internal/harness` 外全 ok；`TestSuperviseCancelGroup` 在移除新包的裸树上原样失败
  （机器既有环境失败，非本轮回归）。模型调用 0。

有界偏差（子代理披露、主代理核可）：

- 五处写入时强化（finding 形状/候选内查重/空白绑定/一授权二有效结果/对已失效结果的
  finding 纠正——legacy 允许先写脏账后在校验暴露，引擎侧写入即拒；§4.4"程序可直接拒绝
  错误 binding"授权此形）＋非语义状态显式报错（legacy 静默忽略）＋回执身份字段入口 trim。
- 级联触发条件按三处权威一致取"纠正"（合同清单原写"纠正/记录"为主代理笔误，legacy
  record 路径只打自身 DownstreamImpact 不级联，已核对采纳纠正口径）。
- digest 用 encoder.Digest（sha256: 前缀）＋引擎 canonical 编码，与 legacy 字节不同
  （引擎内确定性即可，持久化身份非宿主协议文本）。

**R1a 轻装收口；下一单元 R1b（State 嫁接＋ControlKind 扩展＋phase 门控接线）。**

## 第 42 轮（2026-09-23，R1b：审查内核嫁接协议层，轻装）

范围与落实（主代理定合同含裁定先例，子代理实施；12 修改＋3 新增、+306/-53 行）：

- `protocol.State` 增 `ReviewSeries review.Set`/`Topology *TopologyBinding`/`RouteMode`/`SelectedGates`；缺字段旧 run 补 NewSet 前向兼容、残缺投影按篡改拒。commit 前与 load 后同点接 `review.Validate`＋phase-账本一致性（新拒绝码 REVIEW_EVIDENCE_REJECTED/REVIEW_DECISION_REJECTED/STATE_INTEGRITY_INVALID）。
- 事件变体（先例＝HostActionReceipt 的变体指针）：`WorkerResultPayload.Review *ReviewResultEvidence`（仅随 Outcome=PASS；RUNTIME_ERROR 是普通失败结果不建 candidate）；`DecidePayload.Review` 四分支 tagged union（RoundGrant/Correction/Topology/Route），Correction 分支直接复用 `review.CorrectionInput`（json 标签即持久化契约，不建平行 wire）。canonical/digest 纳入变体、`omitempty` 保既有事件字节不变。
- ControlKind 封闭集扩四值：REVIEW_ROUND_GRANT（grant/decline 固定项）、REVIEW_FINDING_DECISION（option id=`<decision>[:<findingID>]` 一致性锚）、CONFIRM_TOPOLOGY、SELECT_ROUTE；UserAuthorized 恒真（DECIDE 即用户决定）、Source=`engine-decide:<requestID>`。RESET/ABORT/RECOVER_ATTEMPT 零改动。
- 双审顺序守卫（start-readiness 证据前置 product-review effective PASS）、拓扑双条件＋不得重切、路线拓扑前置、custom 非空真子集按全集序归一。
- phase 门控五边沿静态表放行（commit 同事务评估，条件不满足静默留置）；一致性校验用单调 residue（受理回执/当前 revision 完成轮/拓扑路线绑定）——第一版按当前 effective 重算被返修环证伪（合法新阻塞轮使 effective 回落而静态表无回退边），子代理如实报告并改 residue；已知残余：账本恰有未裁决 FAIL 轮时精细伪造更晚 phase 不可分辨（后续持久化 effective 轨迹可收紧；对抗性篡改属 P3 边界）。
- façade `Run` 投影增审查面（分动作 completed/base/extra/effectiveLimit/remaining/nextRoundRequirement、三账计数、effective、拓扑/路线）；regular 路线解锁保留至后续刀。
- testkit/whitebox_qa fixture 初始化 phase 改 INTAKE_REGISTERED（诚实初始化点：新不变量下空账本撑不起 DEVELOPMENT_PARALLEL，附原因注释，断言未动）。

主代理独立复核：

- 亲读 `protocol/review.go` 全文＋protocol.go/event.go/engine.go/review.go（CorrectionInput json 标签）全部 diff；fixture 改动逐处核对为机械初始化点变更。
- 自建 oracle 探针（跑毕即删）：split 拓扑（两片＋并行度）＋乱序 custom 路线（归一到全集序断言）＋合法到达 DEVELOPMENT_PARALLEL 后磁盘抹账本（floor 回落）在 Load 拒——全过；中途一次 WAIT 失败系探针误用（readiness 动作由首结果接纳事务 refill 自动签发），反证同事务补位行为。
- 亲验：build/vet/gofmt 干净；`./internal/engine/...` 16 包 ok（protocol 18.8s 含新序列/负控/篡改全套）；`./internal/cli`（83s）＋`./internal/validate`（390s）回归零失败；internal/harness 环境失败同裸树（第 41 轮已归因，不计回归）。模型调用 0。

**R1（a＋b）轻装收口：开发前审查段（双审系列账本→拓扑→路线→phase 门控）在 engine 内权威化。下一单元 R2（REQ-015 裁决深度、无效结果有限重试、每快照唯一返修）。**

## 机主七项 Regular 需求变更（2026-09-23，同日落实）

机主令关闭宿主下放战线（其精神已随 go-engine 链落地，见记忆），并下达七项 Regular 需求。逐项处置：

1. 产品审以需求合理性/方向为第一优先（措辞闭合默认 P3）——改 `prompts/actions/product-review.md`（审查顺序段重写＋已拍板例外条款收紧）。
2. 代码质量门加性能/资源垃圾/可读性一等判据——改 `gates/implementation-quality-gate.md` 维度四（可观察的过度性能消耗与无节制缓存/临时文件/日志按影响定级可达 P1/P2；晦涩难懂不因"能跑通"豁免；假设性担忧仍 P3）。
3. P2/P3 修完不得经"需求更改"升级触发重审——总需求 §7.1.3 修订：P2/P3 处置驱动修订默认 meaning-preserved（不清审/不重审/不耗轮次），含义变化只能由用户 typed Ask 声明；product-review.md 同步。**引擎侧 enforcement 归 R2**（修订分类 typed Ask＋stale 政策按用户声明走，堵 invalidateRequirementResults 的自动升级路径）。
4. 需求变化后重走范围的决定权全在用户——总需求 §7.2.3/§7.2.4 修订：ImpactSet 降为保守建议，重走/续用逐项由用户 typed Ask 确认后引擎机械执行，未决定前不自动失效/重放/豁免。**引擎侧归 R2/R4**（requirement-change 控制流）。
5. QA 非 happy path 充足覆盖——改 `prompts/actions/qa-design.md`（每验收点必须有失败/误用/边界类用例，正向-only＝覆盖缺失）＋`qa-review.md`（集合级判定：正向-only 阻塞）。引擎侧随 R3（QA 覆盖契约把非 happy path 纳入结构校验）。
6. 三轮上限＋超限逐轮用户确认——**已有**：legacy `preDevelopmentReviewAutomaticLimit=3`＋逐轮 `--user-requested`；engine R1a/R1b 已落 `automaticRoundLimit=3`＋`REVIEW_ROUND_GRANT` typed Ask（每次恰好一轮）。无需改动。
7. 提示词是否用上——**核实：在用**。`LoadPromptCatalog`（internal/validate/catalog.go）从磁盘读 prompts/reviewer-base.md＋actions/*.md＋gates/*.md，内容入 CatalogRevision 摘要；拼接骨架在代码里（设计如此）。故 1/2/5 直接改 .md 即生效。

注：本次六处文档 edits 由主代理直接完成（需求案/提示词为内容物非实施代码）；引擎侧 3/4/5 的 enforcement 并入 R2/R3 合同，走"引擎固定流程＋typed Ask"，不留 AI 可绕过/遗忘的口子。

## 第 43 轮（2026-09-23，R1 外部评审返修，轻装）

外部评审对 `dedb3fa..28979c9`（R1a＋R1b）判 FAIL：3 P1＋2 P2。处置按红线走全链：

- **先复现**：主代理逐项读码＋公开入口探针独立复现五项（choice↔payload 不一致落盘／RUNTIME_ERROR 携证据计轮推阶段／旧 revision 证据接纳并完成任务／判重摘要漏 Review 致换内容误判 DUPLICATE／grant 缺载荷 panic）——全部坐实，评审结论成立。
- **派发返修**（单代理、改动面限定 protocol 包、修法裁定入合同）：F1 拓扑/路线选项锚定（choice id 恰为 no-split|split／full|custom 且与载荷一致）；F2 `Event.Validate` 强制 Review⇒Outcome=PASS（失败/崩溃的派发不是语义裁决；completeResult 记账点经 Validate 前置后仅 PASS 可达）＋修正假阳性负测（原用非法 failure class 因错误原因被拒）；F3 证据需求 revision 必须等于受理绑定，stale 拒绝；F4 结果身份摘要纳入 Review（与事件级 canonical 同一 wire）；F5 grant 缺分支的前置门（顺删 switch 内不可达检查）。新增负测六条＋split/custom 正向，全部公开入口。
- **独立复核**：主代理五项复现探针重跑全部翻绿（两处探针口径修正系探针假设错——F2 修复正确落在 schema 层、F5 错误文案为"roundGrant decision branch"，均非残留缺陷）；亲读返修 diff 逐项核对；engine 15 包全绿、cli 回归通过。子代理自报负控（stash 修复后新测试逐项压制缺陷）。模型调用 0。

**R1 返修收口，待外部评审复核。R2（含机主七项中 3/4 的引擎强制）仍待开工令。**

## 第 44 轮（2026-09-23，R1 全量重审返修，轻装）

外部评审全量重审（`dedb3fa..9fb7a7c`，27 文件）判 FAIL：2 P1＋5 P2。处置全链：

- **先复现**：主代理七项公开入口/公开 API 探针全部坐实——①重阻塞后选路线仍接纳并进 DEVELOPMENT_PARALLEL；②确认技术审 P3 建议复活被级联 void 的 PASS；③未消费授权可累计（extraAuthorized=2）；④自动轮整轮失效回收后 remaining=1 与"须新授权"准入矛盾；⑤内核变换改写调用方输入账本（共享切片回写）；⑥旧 revision 结果先到 STAGED 毒化暂存堵死回执与正确结果；⑦投影缺授权来源（读码核实）。
- **派发返修**（单代理，三包）：P1-1 SELECT_ROUTE 双 PASS 前置＋推进纵深；P1-2 内核持久 `UpstreamVoid`（markUpstreamStale 置位、重投影压回 PENDING、新候选清位，integrity 校验）；P2-3 未占用授权存在即拒再授权（integrity ≤1）；P2-4 RemainingCapacity=max(0, 自动上限-completed)+未占用授权数（与准入精确对齐，测试期望独立算术）；P2-5 四变换入口账本切片 clone-on-write＋包文档所有权契约；P2-6 staging 对携 Review 结果前置 revision 比对（共用单一事实源）；P2-7 ReviewGrantFace{ID,Reason,Occupied}。
- **独立复核**：七探针重跑全绿（P1-1 一次"失败"系探针用了 fatal 助手而修复正确在接纳层拒绝，修探针口径后绿）；亲读内核 diff 逐点核对；engine 15 包全绿＋cli 回归通过＋vet/gofmt 干净。子代理自报逐项负控（禁用修复→测试红→恢复绿）。模型调用 0。
- 评审方法教训入账：连续操作不变量（阻塞后门控、void 存活性）与切片所有权须在合同显式列为验收面；"用生产投影验证生产投影"检不出共同错误，期望须独立推导。

**R1 二次返修收口，待外部评审复核。**
