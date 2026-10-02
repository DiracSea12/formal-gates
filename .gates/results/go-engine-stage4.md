# go-engine 阶段 4 记录：Git 非分片 regular 全流程（轻装）

## 当前交付状态（2026-09-28）

119 份累计内容于 `2026-09-28T14:56:10.161075Z` 冻结，主代理逐份核对摘要一致。当前为**完整验收未通过，独立全审继续，源码保持冻结**。真正 `fork_turns:none` 的审查者已从原始规范独立审累计代码与相交流程，另发现full路线可漏白盒QA却被接纳，主代理复制公开反例独立复现。完整engine/cmd执行另因一条旧提示名称测试断言失败；真实两处置路径已独立通过，原失败仍保留。实施者仅只读核路线的全集、选择、发现与相交入口设计，尚未开始下一次源码返工。不能以此前套件通过或已修问题宣称交付完成。

当前证据索引：`/tmp/validation-root-acceptance/current-verification.md`；内容身份：`/tmp/validation-independent-freeze/frozen-hashes.json`。当前仍是 Git no-split regular typed 交付，不表示完整阶段 4、真实 provider 或安装切换完成。

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

## 第 45 轮（2026-09-24，R1 三审返修，轻装）

外部评审三审（`8182d99`）判 FAIL：1 P1＋1 P2，两项均主代理公开 API 探针复现坐实：

- **P1 完整性约束过强**：第 44 轮为防授权累计加的"未占用授权 ≤1"账本不变量，把两条历史授权被整轮作废合法释放的形态也判非法（三轮自动＋逐次授权四五轮→双作废→completed 回落 3、双授权空槽，合法性状态被拒）。修法：删账本级检查，防累计唯一闸门留在 `AppendAuthorization` 入口（已在）；为此加的假不变量篡改负测删除，替换为双释放正向测试（Validate 过、completed=3、effective PENDING、remaining=2、AUTHORIZED_EXTRA_ROUND）。
- **P2 暂存预检过窄**：暂存分支只前置了 revision 比对，证据结构错误（核实缺项、严重度 P9）仍 STAGED→回执配对失败→补正结果 RECEIPT_CONFLICT，一次填写错误要换 attempt。修法：合并出单一预检函数 `admitReviewEvidence`（revision＋双审顺序守卫＋内核 RecordCandidate 全量），暂存分支对携 Review 结果 dry-run（值拷贝调内核、只取错误——P2-5 的输入不可变契约使然）、非空即拒不暂存；配对路径的权威接纳共用同一函数（单一事实源）。
- 独立复核：两项复现探针翻绿；engine 15 包＋cli 全绿、vet/gofmt 干净；diff 亲读（6 文件＋149/−49）。模型调用 0。

教训入账（连续第三轮同型）：给账本加不变量时必须先枚举**所有合法到达路径**（本轮：多重整轮作废释放）；前置检查修一处时按"完整可接纳性"收口而不是逐错误类型打补丁。

**R1 三审返修收口，待外部评审复核。**

## 第 46 轮（2026-09-24，R2：需求变化的引擎权威化，轻装）

范围重排（主代理裁定并记档）：主需求核实后，REQ-015 的 NEW/SAME_AS/DISMISSED 归并史、RETRY_EXHAUSTED、每快照唯一返修均为「开发后」验证 wave 机制（§9.3 :180-181、:225），输入依赖 QA/门结果——**移入 R3**；R2 改为机主七项需求中 3/4 的引擎强制（规格已就绪、直建于 R1 审查系列之上）。

交付（单代理，review/protocol/facade 三包 8 改 3 增，未动 internal/validate）：

- `State.RequirementRevisions []ConfirmedRevision` 权威 revision 账本（INTAKE 回填向前兼容；引擎不持 base/diff 账本，DeltaDigest 只是指纹绑定——§7.2.1）。
- `REQUEST_REQUIREMENT_CHANGE` 控制＋typed 载荷（Base/Candidate/DeltaDigest/Source/Classification/四项 RedoScope/Reason/FindingDigest）：CAS 基线、来源↔分类强制（P1 源必 CHANGED 且必含所属 series 重走——§7.1.2 新鲜复审义务引擎强制；P2 源默认 PRESERVED 保留用户升级权——机主第 3 项；CHANGED 只机械执行勾选项，不勾不失效——机主第 4 项）。
- Phase A 屏障：未决需求变化 Ask 冻结业务签发（`CHANGE_BARRIER_ACTIVE`；refill 静默扣发以免连带拒回执类事件）。
- 内核增 `StaleSeries`（单 series stale＋product 级联 UpstreamVoid）、`HasConfirmedBlocking`（已确认阻塞查询）；`admitReviewEvidence` 落 §7.1.2 门槛：确认阻塞存在且证据绑当前 revision→拒，换 revision 自动放行，纠正路径不受限。
- façade 投影：权威 revision＋最近修订确认。
- **主代理复核发现死锁并返修**：只勾选重走 start-readiness（产品审续用，§7.2.4 carry）时，顺序守卫/双审门/phase 推进按"当前 revision 相等"判 PASS，续用结论绑旧 revision→死锁（主代理探针坐实，先于外部评审）。返修：结论有效性改 carry 感知（PASS 绑确认账本内任意 revision 即有效；F3 证据绑定不变），顺带解除 PRESERVED 后的同族死锁；四条新测试含伪造账本外 revision 与篡改 effective PASS 的负控。
- 验证：engine 15 包＋cli 全绿、vet/gofmt 干净；主代理亲读 requirement.go/carry 判定 diff；探针翻绿后删。模型调用 0。

已知残留（子代理如实披露）：Phase A 未覆盖 HostAction intent 签发（对应区段尚未接入引擎）；ConfirmedRevision 未落 FindingDigest（P1 scope 的加载期复验靠其余链约束）；USER_INITIATED+PRESERVED 可在存在未消 P0/P1 时登记（与 legacy NeedsReReview 值级同形，机主如需更严再加规则）。

**R2 收口，待外部评审。R3＝QA 管线（3.5a 覆盖契约接入、case-set FROZEN、wave）＋开发后 REQ-015 件（finding/problem ID 归并史、RETRY_EXHAUSTED、每快照唯一返修）。**

## 第 47 轮（2026-09-24，R2 评审返修，轻装）

外部评审对 R2（`74b6d4a`）判 FAIL：3 P1＋3 P2，全部主代理公开 API 探针复现坐实：

- P1-1 abort＋载荷仍登记 revision → choice 锚定（proceed/abort 封闭；abort 禁载荷、proceed 必携；外来 id 拒）。
- P1-2 只勾产品审重走却级联 void 技术审、被迫多耗一轮——**系第 46 轮派发合同的裁定错误**（StaleSeries 级联）→ 需求变化路径去级联，只刷勾选 series；纠正路径的 markUpstreamStale 保持（同 revision 上游推翻的依赖语义）；产品重过后携用技术审零新轮过门。
- P1-3 屏障未隔离结果接纳与 HostAction intent → WORKER_RESULT（含暂存）屏障期拒绝（CHANGE_BARRIER_ACTIVE、零状态、原 ID 可重交）；persistHostAction 共享落账缝统一拒（覆盖 Resume/Terminate）；观察类事件不受阻。
- P2-4 续用结论的 finding 无法纠正（stale 门按当前权威）→ 纠正实参改 series 当前 effective 绑定 revision＋账本 carry 门；精确绑定与 CAS 不变。
- P2-5 来源分类未对照真实裁决（确认 P1 可贴 P2 标签登记 PRESERVED）→ 新增内核 `FindingDecision` 有效裁决查询；P1 源 ⇔ 已确认阻塞、P2 源 ⇔ 存在且非阻塞，不符拒；升级须走真实标签。
- P2-6 revision 切换未清暂存（旧 rev 结果毒化迟到回执与新结果）→ CHANGED 同事务清除 redo scope 覆盖 series 的旧 revision 审查暂存并落 RecoveryRecord 留痕。
- 保护错误语义的两条既有测试按新语义重写（屏障期计轮→quarantine；强制技术审→携用）。六探针翻绿；engine 15 包＋cli 全绿、vet/gofmt 干净。模型调用 0。

**R2 返修收口，待外部评审复核。**
## 第 48 轮（2026-09-24，R2 收敛刀：结构修复替代逐项补丁，轻装）

机主对外部评审第五轮（4 P1＋2 P2）定调：**不要无脑打补丁，先根因分析**。主代理完成五家族根因归类（A 两段 Ask 无 typed 包绑定＝确认边界缺失；B 屏障/不变量散落调用点＝枚举式防守永远漏下一个入口；C revision 权威迁移无原子重分类；D 主代理返修合同停留在点位未升到不变量——含三处亲裁错误（级联裁定、"拒绝＝隔离"、屏障覆盖从未完整枚举）；E 测试断言补丁表面）后派发收敛刀（续用 R2 子代理，合同＝四结构改动＋净删散落检查＋测试升到端到端属性）：

- **S1 Ask 绑定 typed 决定包**：`RequirementDecisionPackage`（9 字段，BaseRevision/Reason 不入包）随 REQUEST 落账并持久化于 `PendingAsk.Package`；DECIDE 载荷必须与包逐字段相等（冲突拒绝零状态）；其余控制携包 schema 拒。闭 F1（Ask 即用户确认边界）。
- **S2 单一处置表** `admitInCurrentState`：屏障在位时观察类/在途回执/Ask 创建/需求变化与 RESET·ABORT 照常、WORKER_RESULT 持久隔离、其余一切 DECIDE 与 HostAction intent 拒绝；`admit()` 顶部与 `persistHostAction` 均经表。**删除**上轮两处散落屏障检查。闭 F3（覆盖面成为表属性；engine.go:415 的签发冻结是宿主侧调用非事件，独立机制保留）。
- **S3 持久隔离＋原子重分类**：`State.QuarantinedResults`（eventID 键，QUARANTINED 回执、事件进台账防重放）；需求决定（含放弃）落账同一事务内重分类——CHANGED 绑旧权威 revision 的隔离审查证据直接作废（scope 无关）；其余经正常接纳路径重放（错误不否决决定、落 Obsolete＋留痕）。暂存规则统一为**只认 revision 绑定**（PRESERVED 同样清除），删除上轮 scope 依赖分支。闭 F4/F5。
- **S4 范围语义统一＋来源绑处置**：scope 计算与机械执行移出分类分支（PRESERVED 可携 scope——§7.2.1 合法组合，账本不变量同步删除"PRESERVED⇒空 scope"错误规则）；来源必须绑用户有效裁决（CONFIRM/RESEVERITIZE），未裁决 finding 双向拒绝（先处置再发起）。闭 F2/F6。

交付：protocol 包 5 文件（744 增/192 删，其中源码 4 文件、测试重写携包 helper＋`abortExistingAsk`）。主代理独立复核（自建 oracle 四探针、跑毕即删）：F1 单字段分歧冲突拒绝＋一致登记；F2 账本如实＋scope 机械执行＋界外 series 不动；F3 冻结零状态＋同 Ask 解除屏障后照常可决（正控）；F4 隔离跨重启持久＋绑候选 revision 的隔离结果重放至 completed+1＋绑旧权威的作废留痕；F5 暂存清除＋迟到回执不配对＋新 revision 结果完成接纳；F6 未裁决 P1/P2 源双向拒绝。探针三轮自纠（token 取早、helper 第三参误塞 revision、回执字节冲突、trace 文案过窄）均为探针构造错误，非实现缺陷。engine 15 包＋cli 全绿、vet/gofmt 干净。模型调用 0。

子代理取舍（主代理核可）：HostAction 回执归在途回执照常（intent 冻结、回清纯账）；重放错误统一落 ObsoleteResults（结构不可纳＝死结果）；abort 同样触发重放（无 revision 变化＝全部应被纳）；屏障优先于 UNKNOWN_REQUEST（零状态等价）。已知残留：QuarantinedResults 无独立完整性校验（停车账，篡改仅能丢条目）；同 action 多条异字节隔离结果重放先到先得；审查类控制的 REQUEST 包未实现（机制已就位，按合同留后续）。

**收敛刀收口，待外部评审复核。**
## 第 49 轮（2026-09-24，R2 生命周期收口：Phase B 处置驱动，轻装）

外部评审第六轮对收敛刀（`0397c12`）判 FAIL：4 P1＋1 P2。机主质问"按根因改了为何还有问题"——根因：第 48 轮的家族归纳来自症状而非状态机接缝枚举，漏了三处（在途工作本身不是"绑旧权威的数据"枚举的一员；屏障的解除边界与重放事务性未定义；"引擎持有创建时基线"的设计声明未实现、façade 无展示侧）。其中 P1-1 一半是主代理第 48 轮的裁定错误：把暂存槽卫生问题过度泛化为"revision 变化一律作废"，被规格 §7.2.2:4 原文否定（处置只认用户勾选的重走范围）。

五项主代理全部独立复现（六探针一次全红，跑毕即删）：BUG1a 重走后旧派发完成"新鲜复审"；BUG1b PRESERVED 未勾重走隔离结果被作废 completed=0；BUG2 双 Ask 放弃第一个即重放；BUG3 旧包改载荷基线静默换基线接纳；BUG4 重放报错提交半套账；BUG5 隔离摘要含事件 ID 致重发 RECEIPT_CONFLICT。

第二刀（续用 R2 子代理，按需求变化完整生命周期五设计派发）：

- **D1 处置驱动 Phase B**：`Series.FreshFloor`＋`Attempt.IssuedRevision`（唯一两写点 issueInto/installReplacement）；`admitReviewEvidence` 两分支——重走态（PENDING＋requirement-change: 来源）只认当前权威＋派发代数 ≥ floor，否则账本成员（carry）；pre-floor 派发按声明终止（OBSOLETE_DISPATCH＋同任务重签发）；决定事务内对已声明系列身份的暂存/隔离记录直接 terminate。第 48 轮"统一暂存规则"整块删除重写，S1–S3 步标清除。
- **D2 屏障解除边界**：`requirementChangePendingExcept`——只有最后一个未决 Ask 解决才重放，否则留痕持账。
- **D3 Ask 完整绑定＋展示**：`PendingAsk.BaseRevision`（引擎落账）；三方相等（载荷＝Ask＝当前权威），不等＝新错误码 `REQUIREMENT_ASK_EXPIRED` 零状态；包/载荷增 `ImpactDigest`（§7.2.1）；façade `Run.PendingRequirementAsk`＋Next 投影 ask 边界＋Drive 对屏障冻结不报错。
- **D4 重放逐条原子**：`cloneState`（JSON 往返）快照恢复——单条失败净效果＝该条 obsolete＋留痕。
- **D5 单一接纳核心**：`admitWorkerResultRecord`（Submit 与重放共用）＋`resultPayloadDigest` 唯一摘要点（不含事件 ID）→ 同载荷换事件 ID＝DUPLICATE；`readmitWorkerResult` 与 scope 无关作废分支删除。

主代理独立复核（六探针走到底、跑毕即删）：旧派发 OBSOLETE_DISPATCH＋替换派发完成新鲜轮（completed=1@rev-2）；PRESERVED 隔离结果 carry 接纳＋留痕；双 Ask 先持后放；过期基线三方拒绝零状态＋重建通过；重放失败零半套账（completed=0/无 Results 泄漏/加载通过）；DUPLICATE 幂等。引擎 15 包＋cli 全绿、vet/gofmt 净。模型调用 0。

残留（子代理如实披露＋主代理核）：①未声明系列身份的在途 action 无法主动终止（floor＋按声明终止兜底；彻底解决需定义层 step↔series 标记，交机主）；②未勾 series 暂存证据的迟到回执若撞前提 series 重走中被拒，保留可重试；③lightweight 路由 CompleteAll 不经屏障表（引擎路由接通后复核）；④旧 run 在途 REQ Ask 无 BaseRevision 升级后按过期处理；⑤子代理报告的 carried_conclusion_reprobe_test.go 遗留不实（文件不存在，系其上下文残影，已核）。

**生命周期收口，待外部评审复核。**
## 第 50 轮（2026-09-24，R2 残留①实施：定义层系列标记与主动终止，轻装）

第 49 轮四条残留机主裁决：①立项（本轮）、②惰性配对（已批准，下轮）、**③lightweight CompleteAll 屏障检查机主裁定不管（销案，后续合同不带）**、④作废（无老 run）。

残留①交付（子代理，authoring 4＋compiler 3＋encoder 2＋protocol 6 文件，368/75）：

- authoring：`ReviewSeriesAction` 封闭两值＋`AgentSpec.ReviewSeries`（构造期校验：词汇封闭＋必须随 INDEPENDENT_REVIEW）。层次裁定：不 import review（review→encoder→authoring 成环），镜像词汇由 protocol 测试锁两包一致。
- compiler 携带＋encoder wire 双向（真实 workflow 未标记，canonical 制品 digest 零变化）；validate 补越约构造第二拦截。
- protocol：`PendingAction.ReviewSeries`（签发点唯一写：issueInto 经 reviewSeriesOf 从编译定义读取落账；installReplacement 随步继承）。系列身份以标记为权威（floor/终止/一致性）；证据声明与标记不符＝可区分拒绝零状态。
- Phase B 第三轮循环：标记入 scope 且决定前签发的在途派发主动终止（retire＋同任务重签发＋留痕，无结果不作废）；三终止路径共用 `replaceDispatch`。子代理自曝并修复双重终止缺陷（替换派发继承标记会被再终止→只终止决定前签发者）。
- 测试：主动终止＋替换完成新鲜轮、未勾选系列标记派发存活、证据不符拒绝、词汇一致锁；既有 floor/按声明终止兜底回归保留（相关测试改用未标记步名继续覆盖证据声明路径）。

主代理独立复核（三探针走到底、跑毕即删）：标记派发（零证据）被重走决定主动终止——旧 actionID 进 ObsoleteActions 带 ReplacedBy、无 ObsoleteResults 泄漏、替换完成新鲜轮 completed=1@rev-2；未勾选系列标记派发存活且顺序门语义不变；不符证据零状态拒绝、相符证据完成。探针自纠一处（MP2 末步误期：产品重走期就绪顺序门本应关着，正是残留②的立项场景）。engine 15 包＋cli 全绿、vet/gofmt 净。模型调用 0。

残留（如实）：真实 workflow 的单一 review.worker 未标记（系列归属属产品建模，接入真实双审时再标）；compiler 越约路径配对复核未立专测；已标记 action 的非审查结果不触发一致性检查（规格未要求）。
## 第 51 轮（2026-09-24，R2 残留②实施：回执无条件落账＋惰性配对，轻装）

机主批准的推荐方案落地（子代理，protocol 6 文件，315/14）：**回执是无条件观察事实，配对是惰性业务动作**——消灭"顺序门未开连回执都进不了账、须人工换事件 ID 重试"的耦合。

- 错误分界可识别化：新错误码 `REVIEW_SEQUENCE_GATE`（双审顺序守卫专用，errors.As 判别无宽匹配）；结构性拒绝照旧拒事件。
- 回执无条件落账：配对撞顺序门→回执 ACCEPTED 提交＋暂存保留＋挂起留痕（"stays pending until the product-review conclusion re-passes"）；非门类错误零状态拒绝不变。
- 惰性补配对 `pairSuspendedResults`：扫"回执 SPAWNED＋暂存＋未入账"逐条经唯一接纳核心补配对，单条失败快照恢复保持挂起。锚点两处＝产品审 effective 重新 PASS 的全部入口（worker result 接纳；DECIDE settle 后 refill 前，覆盖纠正生效与隔离重放）；空扫描零成本。重放路径同享（隔离重放撞顺序门→挂起不作废）。
- 测试：端到端主场景（重走期回执 ACCEPTED＋挂起＋无重试请求→产品新鲜轮 PASS@rev-2 同一事务自动配对 completed=2→同字节回执重发 DUPLICATE 不双计）；新 `parallelTailFixture`（双审后并列双派发槽，线性 fixture 无法同时呈现挂起槽与开门槽）；out-of-scope 子测从"回执被拒"翻为"ACCEPTED＋挂起"；两处早到/账本外守卫断言改期新码。

主代理独立复核（单探针走到底、跑毕即删，独立于子代理的 helper——显式走 pre-floor 旧尾槽 OBSOLETE_DISPATCH→替换派发 PASS 的完整舞蹈）：挂起状态三项（暂存保留/计轮不动/留痕）＋同事务配对证明（配对无独立提交事件，只能发生在产品 PASS 接纳事务）＋DUPLICATE 不双计，一次全绿。engine 15 包＋cli 全绿、vet/gofmt 净。模型调用 0。

残留（如实，均低危记档）：①已配对回执的**直接结果提交**撞顺序门仍整事件拒绝（本轮只把回执变为无条件；结果事件的暂存挂起未做，规格未要求）；②已确认阻塞门槛（同 revision P0/P1 拦新候选）是另一扇可开门（dismiss/降级），未纳入挂起类；③补配对对结构性坏死条目每次开门空转重试（无状态增长）。

**第 49 轮四残留全部处置完毕（①②实施、③销案、④作废）。R2 全链收口，待外部评审总复核。**
## 第 52 轮（2026-09-24，R2 第七轮五修复＋标记词汇闭环＋系统交集覆盖，轻装）

第七轮评审（对 `cf3af98`）判 FAIL：3 P1＋2 P2。机主定调"每轮修出新交集"须终结。主代理核实：行为级两项独立复现（P1-1 即第 49 轮 VP5 断言的 completed=0——当时主代理把"原子但作废"误判为修复终局，评审正确裁定应恢复接纳；P2-4 复现 `retry:` 替换在双 Ask 期即签发），P1-2/P1-3/P2-5 结构级读码坐实。根因模式＝新修复面 × 既有状态的交集无覆盖＋façade 客户端侧从未系统走查。

五修复交付（子代理，23 文件 603/157）：

- **F1 terminal 对账**：接纳核心 not-pending 分支前置回执在账即恢复接纳（Results＋证据记账＋对账留痕，携决定 requestID；无任务迁移无 refill）；无回执仍拒、Obsolete 认领优先、三路（Submit/重放/补配对）同享。审查新鲜度两分支天然使"重走中旧证据"仍被拒——处置语义不因 terminal 改变。
- **F2 标记词汇闭环**：第三值 `unaffiliated`（明确声明非开发前系列）；INDEPENDENT_REVIEW 步标记必填（constructor＋validate 双层）；canonical workflow.go 的 review.worker 标 product-review、workflow.json 重生成（envelope 冻结身份刻意不动，stage-records 已记账）；fixture 全标记改造（尾槽改名 tail.readiness/tail.product 按系列标记），证据声明身份路径的兜底回归改由 SEMANTIC_JUDGMENT 声明件驱动。
- **F3 façade token**：PendingRequirementAskFace.Token（投影时即时求值）＋Facade.Freshness 公开直通；façade 测试改走公开入口。
- **F4 屏障期零新派发身份**：replaceDispatch 遇仍有未决 Ask → 只退位＋Deferred 快照；installDeferredReplacements 锚在屏障解除事务补发（核实证据：refill 走 SelectIssued 对 ISSUED 任务无法再签发，必须显式补发）。
- **F5 理由落账**：ConfirmedRevision.Reason＋carry 留痕携（requestID, 理由）。
- **系统交集覆盖**（change_disposition_test.go，行为命名）：在途/暂存/隔离/terminal × 本系列重走/他系列重走/双系列/续用/放弃/双 Ask 可达组合逐个驱动到终局，断言 Results/Obsolete 归属＋计轮＋屏障期无新派发身份＋账本加载。

小修单（子代理自报残留②、主代理核实）：`ReviewSeriesAction.Valid()` 单一词汇判定点，constructor 与 compiler validate 共用；validate 补封闭集值校验（与 Reason 同层同模式），raw 路径伪造值拒绝正负两控入树。

主代理独立复核（五探针走到底＋façade 客户端视角两探针，跑毕即删）：terminal 恢复（completed=1＋Results＋留痕＋可加载）；双 Ask 零 retry 身份＋Deferred 在账＋解除事务补发且代数≥floor＋新鲜轮完成；理由落账（中文原文）＋carry 留痕含理由；客户端全流程（stale 拒→Freshness 刷新→公开入口提交成功）；canonical 标记与 raw 伪造值拒绝。engine 15 包＋cli 全绿、vet/gofmt 净。模型调用 0。

残留（如实）：①"could not be readmitted"作废分支在本套件内暂无可达驱动（唯一原驱动已走对账路径；屏障期确认阻塞 finding 的驱动场景未建，留待真出现时补）；②envelope 冻结身份与 canonical 制品身份并存（阶段 0 契约，已有记账）；③testkit/whitebox_qa 场景件标 unaffiliated 属必要波及。

**R2 第七轮收口，待外部评审。交集覆盖为常设要求：后续每轮新增行为面必须同时落交集测试。**
## 第 53 轮（2026-09-24，R2 第八轮四收敛：义务事实/屏障单点/签发持久/配对单点，轻装）

第八轮评审（对 `fa0092a`，范围＝14 个未推送提交全量）判 FAIL：3 P1＋1 P2，四项主代理全部独立复现（探针跑毕即删；BUG-D 探针自纠两处：尾槽字母序、决定已主动终止标记槽）。评审定调：整体方向＝收敛状态事实与接纳入口，禁止局部特判。四缺陷共性＝同一业务规则多入口：重走义务活在可被纠正覆盖的投影里；结果接纳分散四入口而屏障只盖一处；签发事实绑在会被终态清理的簿记上；补配对挂在部分事件分支。

四收敛交付（子代理，protocol 6＋review 3 文件，524/46）：

- **A 义务事实化**：`Series.FreshOwed`（StaleSeries 置位；`DischargeFreshOwed` 唯一清除点＝重走分支新鲜轮完成接纳后）；`seriesInRedoStale` 改读义务；`effectivePass` 义务感知（投影被纠正刷成旧 PASS 也不算过门）；纠正全路径不触碰义务（内核测试锁死＋篡改负控）；完整性判据入 verifyReviewIntegrity。
- **B 屏障单点**：回执配对路径在 `requirementChangePending` 时只落账观察＋挂起留痕，不 completeResult；屏障解除事务收尾扫清。ReconcileUnknownReceipt 同类直调一并收口；挂接（ATTACHED）回执纳入补配对资格。
- **C 签发持久**：`State.DispatchIssueRevisions`（写点恰两处：issueInto/installReplacement；终态清理不删），`Attempt.IssuedRevision` 字段删除（一个事实一个归属者）；terminal 对账以真实代数过 floor——校验保留不跳过。
- **D 配对收尾单点**：admit 拆 admitEvent（屏障表后）＋事务收尾单点 pairSuspendedResults（落账提交且屏障不在位时执行一次）；删除 decide/worker-result 两处分支调用点；两种产品审完成顺序终态全等（等价性负控入树）。

评审点名四组连续操作测试全部走到底入树；交集套件与全部回归未删弱。

主代理独立复核（四探针走到底、跑毕即删）：义务在纠正下存活＋路线门拒＋新鲜轮完成清偿后门开到 DEVELOPMENT_PARALLEL；屏障期回执 ACCEPTED 落账＋暂存保留＋计轮 0＋决定事务内同事务接纳；先 terminal 后交结果以真实代数（≥floor 有断言）接纳并清偿义务；回执后到路径同事务补齐挂起技术审（completed=2＋暂存清空）。探针自纠一处（线性夹具无产品审新鲜轮承载派发，换 product.again 步）。engine 15 包＋cli 全绿、vet/gofmt 净。模型调用 0。

残留（如实，①需机主定调）：①FreshOwed 清偿计"完成接纳即计轮"——新鲜轮事后被 INVALIDATE_RESULT 不复活义务（复活需在失效路径开第二写点，与"清点单一/纠正不触碰"冲突；失效后投影 PENDING＋门判仍挡住推进，实务安全，语义上有"整轮失效后旧 revision 新轮可经 carry 接纳"的窄路径）；②收尾单点只覆盖 Submit 事务，恢复 API 自身落账事务不扫描；③屏障期 ATTACHED 回执挂接的补配对无独立端到端负控（四条件叠加场景未建）。

**R2 第八轮收口，待外部评审。**
## 第 54 轮（2026-09-24，根因＋广范围：双线审计＋框架三修复，轻装）

机主定调"根因＋广范围，不要只看眼前"。本轮流程改为：**主代理接缝审计＋新鲜红队全表面审计并行**，合并去重后单一综合派发，内部清零才送外审。

双线审计合并（7 缺陷＋3 观察，全部复现）：红队 2 P1＋3 P2＋3 P3（D1 RecoverAttempt 屏障期签发、D2/D3 HostAction 回执与 RESET/ABORT 决定屏障期 settle、D3' UpstreamVoid 棘轮死锁、D4 terminal 对账不补位、O1 grant 政策无执行、O2 非需求 Ask 无 façade 面、O3 DispatchID 不绑派发）；主代理独立坐实 RISK-4（＝D1）与 RISK-1（屏障期 FAILED 回执无处置删暂存——红队负控只验"无新身份"漏掉此面），RISK-2/3 并入观察。两线互补验证了审计方法。

框架三修复（子代理，12 文件 737/285）：

- **FA 屏障闸＝提交边界属性**：全部权威写入口过同一判定（清单入报告：Submit 处置表、IssueFromPlan/RecoverAttempt/ReconcileHostAction 头部闸、persistHostAction、refill 扣发、ReconcileUnknownReceipt 挂接/配对分置）；观察照常落账，业务后果（配对/settle/结算/失败路由/新派发）屏障期落持久挂起账（HeldFailureRoutes/HeldHostSettles/FrontierSettleHeld/Deferred），解除事务统一执行。
- **FB 事务收尾单点**：commitEffects（transactionEpilogue→advanceReviewPhases→verifyReviewIntegrity→Save）为唯一提交点，一切持久化事务过路；收尾屏障感知。如实申报偏差：收尾补位为条件执行（仅当收尾确做了业务工作），否则观察类事务自动签发会击穿 Plan→Submit→Issue 驱动契约。
- **FC 上游失效可逆**：`Series.VoidedEffective` 快照＋`ReleaseUpstreamVoid` 反向原语——纠正使上游回 PASS 且无其他阻塞时，同一事务恢复下游失效前结论（原文恢复，仅 Revision 单调）；新候选重建语义不变。§7.3.3 死锁解除。
- 过程中发现并修复两缺口：terminal 对账不完成步骤（两序等价测试揭出，SpawnReceipt 增 Task 观察字段，对账执行与 completeResult 同源机械后果）；fulfilled 对账结算旁路（入同一清算账）。
- P3：grant 未达上限引擎拒；façade 泛化 PendingAsks 面（token 即时求值，需求面保留携包特化）；证据 DispatchID 必须绑定提交 actionID（跨 action 摘要碰撞结构性不可能）。

主代理独立复核（探针走到底、跑毕即删）：屏障期 RecoverAttempt 拒（CHANGE_BARRIER_ACTIVE）＋解除后照常；FAILED 回执屏障期只落观察＋暂存保留＋解除事务按规则处置（删暂存＋路由）——两序收敛；UpstreamVoid 往返解除走到底（恢复失效前结论＋拓扑确认照常）；grant 早授权拒＋DispatchID 不绑拒。TERMINAL/结果两序等价由入树结构全等测试覆盖（主代理线性构造与任务进度事件模型相性不合，弃用独立副本）。engine 15 包＋cli 全绿、vet/gofmt 净。模型调用 0（红队探针仅 /tmp overlay，仓库零改动）。

残留（如实）：①收尾补位条件执行（设计取舍已申报）；②FC 释放仅在纠正路径触发（新轮重投影走重建路径）；③FreshOwed 失效不复活仍待机主定调（第 53 轮残留①）；④红队二跑待执行（本轮收口后立即派）。

**框架收口，待红队二跑清零后送外审。**
## 第 55 轮（2026-09-24，红队二跑三缝收口：重投影解除/授权槽再绑定/补位补全，轻装）

红队二跑（对 `fa0fde8`）：上轮八项全部转绿、负控无回归；新面三 P2＋一 P3（探针 /tmp/audit-probe2）。红队根因定性："单向置位的机制补了反向出口，但出口只通向置位时的世界"。三刀合并为两收敛修法＋一条件补全（子代理，9 文件 483/66）：

- **G1（N1＋N2 合并）**：`VoidedEffective` 快照字段与逐字回放**整体删除**（过时快照即缺陷本体）；解除改为按下游当前账本重投影（与 RecordCandidate 同源的 Effective 推导，窗口内下游纠正自然纳入、被下游自判失效的候选不复活）；触发收敛到唯一提交点内单触发点 `releaseUpstreamVoidIfReleasable`（守卫：void 在位/屏障不在位/下游无 FreshOwed/上游 effectivePass 且无已确认阻塞）——纠正、新鲜轮、需求变化重走任何使条件成立的事务统一覆盖；内核旧触发删除。FreshOwed 与上游失效正交（欠的新鲜轮只能自身清偿）。
- **G2（N6）**：内核 `BindDispatchToUnoccupied`（复用 BindDispatch，无空槽无操作），接入两签发点（issueInto 按标记、installReplacement 随步继承）；授权"占用"语义＝被有效候选 endorse，服务当前派发集合；释放槽由自然重试路径（在途后继经 NEW_ATTEMPT 换新）自动绑定消费；新 grant 存在未占用授权仍拒。
- **G3（N5）**：`routingReleasedCapacity` 瞬态事实（非导出不入账），收尾补位条件扩为含路由性容量释放；观察类事务仍不自动签发。

子代理交叉跑红队探针（断言改期态）：R1–R8＋N1–N6 全部 PASS。主代理独立复核（两探针走到底、跑毕即删）：N1 全链（窗口内下游升级→上游撤销→解除后投影**如实含窗口升级**（FAIL 阻塞，非旧 PASS 回放）→窗口升级再撤销→回 PASS→拓扑开放）；N6 端到端（三轮耗尽→grant→第 4 轮入账→INVALIDATE 释放→新 grant 拒→搁浅派发经 RecoverAttempt NEW_ATTEMPT 换新自动绑槽→重试轮携授权入账 Completed=4）。探针自纠五处均构造细节（夹具步名标记、授权 ID 携带、瞬态失败不换新需走恢复入口、文案匹配）。engine 15 包＋cli 全绿、vet/gofmt 净。模型调用 0。

残留（如实）：①失效前已签出未绑定的在途后继其证据不可直接入账（O3 绑定语义下正确，须先经失败/中断换新——消费路径本身已通）；②同派发迟到重试仍被结果级身份拒（正控）；③FreshOwed 失效不复活待机主定调（沿用）。

**反向路径收口，待红队三跑清零后送外审。**
## 第 56 轮（2026-09-24，红队三跑清零：独立回归全绿＋新面无缺陷，轻装）

红队三跑（对 `47c9901`，复核轮；探针全新写于 /tmp/audit-probe3，期望态断言不复用实现方改动，仓库零改动）：

- **独立回归全绿**：Q1–Q8（屏障/挂起/UpstreamVoid/补位/早授权/绑定/四组负控）、M1/M2/M5/M6（N1/N2/N5/N6 原复现序列期望态）、H1/H2（N3/N4 负控）、facade R9（泛化面＋token round-trip）——二跑全部缺陷的修复以独立断言复验通过。
- **三新面狩猎无 P1/P2**：H3 失效排除候选不复活；H4 多 finding 纠正史按当前裁决重投影（快照方案病灶形状在此正确）；H5 FreshOwed 与上游失效正交（不越权解除）；H6 无结论系列空 digest 安全；H7 非审查派发不绑定；H8 FAIL 结果路径同样释放即补位；另两项代码级核验（routingReleasedCapacity 瞬态字段不可能跨事务泄漏、pairing 先于 release 无空转）。
- **唯一观察 H9（P3）＝第 55 轮残留①的独立坐实**：槽占用期签出、未绑定的在途派发，其真实结果不可直接入账（无授权越限拒、携授权未绑定拒），唯一消费路径＝NEW_ATTEMPT 换新重跑——与申报设计一致、有恢复入口，代价为一轮真实审查工作重跑。红队建议的接纳点惰性补绑列为 P3 建议不阻塞（与残留② lazy pairing 同性质）。

主代理独立复核：亲跑全部探针命令（protocol 20 项全 PASS＋facade R9 PASS），工作树干净、HEAD 即 `47c9901`。模型调用 0。

收敛轨迹：外审 2P1+3P2→3P1+1P2（两轮）→ 红队一跑 2P1+3P2+3P3 → 二跑 3P2+1P3 → 三跑 0P1/0P2（唯一 P3 为已知残留的独立确认）。

残留（如实，①②待机主定调）：①H9 在途后继证据不可直接入账（建议维持现状，同残留②先例）；②FreshOwed 失效不复活（沿用第 53 轮残留①，建议维持现状）。

**内部清零达成。R2 待机主送外部评审。**
## 第 57 轮（2026-09-25，机主裁决①签发侧闸：承诺模型六刀链＋红队四至八跑终点验收，轻装）

机主裁决（2026-09-24）：①"那就不合理，我不授权根本就不该能派出去"——审查系列派发额度改**签发侧强制**；②FreshOwed 失效不复活"先这样"（维持第 53 轮残留语义，结案）。由此展开六刀收敛链（全部子代理实施、主代理独立复核探针跑毕即删、逐轮亲跑复现红队发现）：

1. **e4a819c 签发闸＋授权前置化**：带系列标记的新签发过单点容量闸（issueInto 咽喉）；RoundGrantDecision 删 DispatchID、授权先落账、签发点自动绑定；早授权拒与入账背书校验保留。H9（占用期签出未绑定后继作废重跑）结构性根除。
2. **5b1aad4 独占槽**（红队四跑 P2 授权槽双绑）：越限派发只绑"无记录且不承载在途"的独占槽；容量内共用网保留。
3. **b3f8cbf 序无关承诺**（五跑 P2 漂移悬空）：容量自洽形（Completed+在途≤限，放行不绑槽——自动轮不消费用户授权）＋背书形（无未持槽在途且有干净槽才放行独占绑定）；共用形废除；换新身份继承原槽；完整性增跨槽唯一性负控。
4. **7e4bbb1 未落定承诺**（六跑 P1 terminal 对账在承诺账之外）：liveDispatch 扩为未落定谓词（回执在账∧无 Results∧未退位）；任务 terminal 化不解除承诺；隔离重放**拒绝不落定**（有据偏离，主代理认可——拒绝后新可纳结果仍可对账）；失败结果僵尸出口；状态级承诺 oracle 入树。
5. **81dab5c 对账对齐**（七跑 P1 ATTACHED 计入无出口）：terminal 对账与配对候选同判 inflightWitnessReceipt；结果类别×回执状态×任务期状态空间表入树；配对失败留痕。
6. **e1db507 承诺与回执解绑**（主代理独立探针实证 P1 追认增长）：承诺始于签发终于落定——issuedWitnessReceipt（含 UNKNOWN）定承诺存在，回执状态只分层结果准入（真实结果须观察事实、失败结果收任何签出证明）；挂接不改承诺集合，追认性增长结构性不可能。

红队四至八跑（全部主代理亲跑复现后派发修复；四至七跑探针随合同演进出零改写或注明改写）：四跑 19 项（1 P2 双绑）；五跑 10 项（1 P2 漂移＋1 P3 完整性）；六跑 11 项（1 P1 terminal 三形状＋实施方两项残留定级无新裂）；七跑 8 组（1 P1 ATTACHED 四形状）；八跑终点验收——**承诺集合单调性逐操作实证（增员必签发、减员必落定，无例外）、分层准入 52 格矩阵、落定出口穷举无死角、六轮缺陷形状独立重验全部不可达**。

**八跑遗留 1 项 P2（落在机主裁决②域内，新证据已呈报）**：redo 义务清偿后，重走前签发的 terminal 僵尸派发携旧 revision 证据经 carry 分支入账——effective 翻转为旧 PASS 并越过已确认阻塞 finding 推进 phase。与第 53 轮残留①同根因（清偿后 carry 分支不区分"未勾选重走"与"已重走已清偿"），机主 2026-09-24 已裁"先这样"；新证据为：terminal 迟到面自然可达（无须故意交旧 revision）且后果含 phase 越门（规避 INVALIDATE_RESULT 可重新关门但 phase 不回退，不完全）。维持或改判待机主。

P3 记账（不阻塞）：TERMINAL 任务无再派发路径（步骤可能永久 incomplete，活性观察）；floor 判定 pending/terminal 不对称（上项结构成因）；隔离拒绝 ObsoleteResults last-wins 覆盖（预先存在）；internal/validate 第二账本校验与引擎侧判定差异待对齐。

主代理独立复核汇总：六轮各以自建探针走到底（闸本体/早授权/授权绑定/H9 根除/并行在途/漂移翻转/省槽/继承/未落定槽/挂接交错/追认翻转/僵尸出口），全量配方（build/vet/gofmt＋engine 16 包＋cli）与红队各跑探针逐轮全绿；模型调用 0；探针全部跑毕即删（/tmp）。

**承诺模型终点验收通过（除机主裁决域内 1 项 P2 待表态）。R2 重新到达待外审状态。**
## 第 58 轮（2026-09-25，机主改判"修"：重走下界贯穿准入＋零上下文代码评审＋收口小修，轻装）

机主改判（2026-09-25）：第 57 轮八跑 P2 由"先这样"改为"修"。随后追加指示：修复后派**零上下文子代理**评审代码（正确性、质量、简洁、耦合、可维护、可读、优雅、需求覆盖无遗漏、硬编码/窄逻辑/过拟合、过度设计/防御/抽象、测试不污染业务）。

**第七刀（6f39355，重走下界贯穿准入）**：确认账目成员判定代数化（confirmedRevisionGeneration——登记落账代数，空账本回退按零与 floor==0 同判）；carry 分支双下界——重走系列（FreshFloor>0，清偿后留存）的证据确认代数与派发签发代数都不得早于最后一次覆盖它的重走决定；从未重走系列 carry 原样；义务期分支不变；admitReviewEvidence 唯一准入核心覆盖 pending 与 terminal 对账两路。设计权威＝effectivePass 归属注释自述意图（"每个新确认 revision 要么重走要么携原结论续用"）。红队八跑原复现序列翻转：清偿后僵尸旧 revision 证据拒（零状态）、已确认阻塞保持、phase 不推进。

主代理独立复核：自建双次重走探针——rev-2 对第一次重走合法、对第二次越界，**下界＝最后一次覆盖决定**（rev-2 拒、rev-3 同派发入账）；走查顺带覆盖被动换新承载、僵尸失败出口整链。

**红队九跑（6f39355，范围内零缺陷）**：17 新探针全过＋五套回归＋全量绿（主代理亲跑）。要点：恰等边界（gen==floor/floor+1 放行——下界是每系列 floor 非全局最新决定）；多系列交错互不冒用；四条入账路径调用图核验无旁路（RecordCandidate 唯一调用点汇聚）；清偿后纠正路径（DISMISS/RESEVERITIZE/INVALIDATE）不破坏下界；自建状态级 floor 模型 oracle 逐探针断言。范围外 P3 观察：空账本回退窗口公开可达（受理回执落账前审查轮可完成）——先于本链存在且语义等价，记账。

**零上下文代码评审（机主指示，评审范围＝本链 1a12d8a..6f39355 全量 diff）**：总评"高水准"；九维度中简洁性/耦合/可维护/可读/优雅/硬编码过拟合/过度设计/测试污染八维未发现（附证据面）。实质发现三项，全部主代理亲跑处置：
- **P2 幻影轮**（已复现坐实）：无系列标记派发（语义判断步按 §7.2.2:4 兜底以证据声明系列身份）在闸外消费系列轮容量，可把受闸自洽形放行者挤到记录侧越限拒收。定调：声明形状是既有设计、不拒收合法证据；记录侧轮额仍保护系列、被挤者经授权或整轮作废恢复。缺陷本体＝闭环注释量词过强——随小修收窄为"受闸派发之间"并在声明回退处写明交互。
- **P3 空提交**：全批被闸时 IssueFromPlan 仍空转一个 revision——随小修对齐"零状态变化不经提交"纪律（retainExpected 有增量属状态变化照常提交）。
- **P3 容量截断边界**：被闸首步挡住 SelectIssued 前缀时后续步本批不补选（活性与改前等价、非回归）——随小修入注释。历史格式"一槽多承载"（无老 run）仅记账。

**收口小修（df287e7）**：上述量词收窄＋交互注释＋空提交跳过（新测试断言字节级零变化与增量照常提交两态）＋截断边界注释。主代理复核：diff 与合同一致，配方净、16 包 ok、六套红队探针（4-9 跑）全绿。模型调用 0；探针跑毕即删。

P3 账（累计，不阻塞）：TERMINAL 任务无再派发路径（活性观察）；floor 判定历史不对称已成注释；ObsoleteResults last-wins（预先存在）；internal/validate 对齐；历史格式共享槽；空账本回退窗口。

**七刀链＋双维评审（行为红队九跑×质量零上下文）双双清零。R2 重新到达待外审状态。**
## 第 59 轮（2026-09-25，外审 FAIL 修复→第八刀→机主令收敛架构：轮次归账本＋首过闩，轻装）

外审 FAIL（1 P1＋2 P3）：P1＝canonical 定义无 start-readiness 载体步、dualReviewPass 永不开（协议测试全靠夹具注入＝过拟合）；P3 封闭集文本×2。修复 0d3514c：definition 增 review.readiness 步（deps=[review.worker]、series start-readiness、同 handler/pred/timeout）、Version "2"→"3" 同源重生成（digest d6a89c8b…）、树内 canonical 双审验收、文本对齐真实集合。主代理独立复核：自建 canonical 走查探针（产品 PASS→同事务补位 readiness→PASS→双审门开→相位推进）＋制品一致性（v3/摘要/identity_gen）＋16 包＋探针 4–9 全绿。

**红队十跑（双跑收敛）**：上下文耗尽孤儿化的原跑与重派跑在同一发现独立收敛——canonical 每系列仅一个载体步×NewReviewWorkerResultEvent outcome 恒 PASS（event.go:451）→语义 FAIL 判定轮也完成载体步→完成步永不回前沿→第二轮/授权轮/FreshOwed 重走/INVALIDATE 恢复结构性死亡（21 条探针主代理亲跑坐实＋四处代码点亲核）。

**第八刀（载体步完成＝审查账本投影）**：完成 ⟺ effectivePass∧依赖完成∧本任务有已入账语义轮；条件破裂级联 reopen；roundDispatchID 轮身份；签发闸补 UpstreamVoid 窗口；加载/提交边界投影校验。主代理复核期间亲证两真缺陷并返修：R1 任务视图重置守卫系列级过度串行化（并发 endorsed 死路）；R2 屏障解除事务补位抢走延迟替换将继承的槽→同槽双载（修＝KindDecide 先装替换再投影补位）。

**红队十一跑＋零上下文评审双收敛同一 P1（主代理三处亲证）**：投影单遍不到不动点——同系列载体间夹非载体中继步（LOCAL/HUMAN_ASK/编译序倒置）时重 PASS 事务被 verifyCarrierProjection 判 STATE_INTEGRITY_INVALID，确定性活锁无恢复出口（readiness 链无 void 补偿投影兜底）。返修＝联合不动点 settleCarriersAndFrontier＋eachSeriesLiveDispatch 共用枚举＋ackStore 注释。

**机主令（2026-09-25）：「不要一个一个补丁地一直往上落，落得越来越高很难看，要从架构，根因层面解决」**。根因定案：轮次生命周期（签发→回执→入账→轮额/新鲜义务）属审查账本，第八刀却把「该不该派下一轮」路由进 DAG 步完成态，用双向投影（完成/reopen）撬两个单调模型（步完成集、任务状态机）——三轮返修全是给错误原语打补偿。教训入记忆（勿用双向投影撞单调模型；返修第二轮即升收敛刀；派发权归属跟着事实权威走）。

**收敛刀（本轮落地，删投影层）**：①轮次＝一等任务——series.Issued 轮计数为轮号唯一权威（IssueRound 唯一调用点在 issueInto 漏斗），第 1 轮规范键、第 N>1 轮 TaskKey scope＝轮号，派发身份＝act:<任务键>，删 roundDispatchID 后缀改写；②欠轮提案归账本——mergeOwedRoundProposals 在漏斗内闸之前并入（每系列每事务至多一条，roundsIssuedTx 事务瞬态提交点清零），闸补两前置规则（已结论拒发；start-readiness 顺序门签发侧前置），提案与在途无关、并发 endorsed 两形准入不变，删 UpstreamVoid 窗口补丁与两级让位守卫；③步完成＝一次性闩——series.FirstPass 首过置位（refresh 尾部单点、无清除路径），settleFrontierSteps AGENT 分支 carrierLatched，删全部 reopen/级联/不动点/投影校验/unmarkedAgentPassRecorded（11 符号 grep 零残留），completeResult 恢复直完成。保留：七刀闸核心、installDeferredReplacements 先装顺序、testkit 受理确认。

主代理全套复核：全量（除 internal/harness 本机 docker 环境既知失败，基线 stash 复现归因）零失败；自建闩模型 oracle 六条全绿（四复活路径＋闩单调＋scope 轮跨重启回放）；探针 4–10 失败逐条定性为两族旧模型期望（多载体后继自身派发／readiness 顺序签发侧前置），无槽绑定/容量/不变量类；树内 canonical 五验收＋承诺独占槽＋屏障先装测试全绿（期望翻转仅为指令声明的闩语义）。

**零上下文评审（收敛树终审）：可合入**——九维度全 PASS；roundsIssuedTx 判定最简合理；两 P2 注释失真（owedProposedTx 幻影字段、延迟替换恢复路径失真）＋八 P3。

**收口清理批（主代理亲落——派发通道因模型配额中止不可用、机主令「继续」，偏离实施派发红线一处、如实声明）**：两 P2 注释按当前语义改写；死条件 `&& !series.FreshOwed`×2 删除（effectivePass 已含）；轮计数闸门改账本成员判定（reviewSeriesBooked，unaffiliated 不入轮账与提案账）；testkit 封闭枚举改常量比较；applyRoundScopes 补扇出约束注释。触面三包＋全量复跑全绿。

红队十二跑**因模型配额上限中止**（09-29 20:32 重置）——收敛树的独立红队未清零；其审计面由主代理亲核探针（闩单调/重启回放/四复活路径）＋零上下文评审＋树内承诺/屏障/顺序门测试覆盖，但独立对抗面缺位。P3 新账（不阻塞）：integrity 未重算 FirstPass/Issued（防篡改建议位）；Expected 残留（系列结论后未签发的兄弟载体规范任务无清理点，仅 harness "unsettled tasks" 判定面）；mergeOwedRoundProposals 无载体静默 continue（排障留痕缺）；commitEffects 释放次序（SR 自身轮亦作废的复合场景欠轮推迟，携结论重投影自愈）；被闸提案占 roundsIssuedTx（一轮推迟到下事务，各交织净效果自愈）；轮身份后缀与含":"步骤名的命名自律；probe10/4/8 旧模型期望族退役留档。

**R2 待机主定夺：收敛树内部验收全绿＋零上下文可合入，但独立红队因配额未跑完——外审重送时机待机主（配额重置后补跑十二跑，或机主授权即时送审）。模型调用：子代理配额内已耗尽，本轮后半（清理批＋复核）模型调用 0。**
## 第 60 轮（2026-09-26，红队十二跑重派（机主令用 5.3-flash）：PASS，内部清零，轻装）

机主令（2026-09-26）：红队改用 5.3-flash 派发（子代理继承会话模型）。第十二跑对收敛提交 ad695e9 终审：**范围内零阻塞项，总裁决 PASS**。18 条核心探针（/tmp/audit-probe14，主代理亲跑复证 22/22 绿）：A 闩不变量（置位唯一写点/不清位/FAIL 不闩/完成集⟺闩/篡改残余暴露面）；B 轮身份与两账分立（Issued 计签发、Completed 计有效轮、作废不回 Issued；scope 键×retry: 前缀无碰撞；替换不增 Issued）；C 提案与闸（每事务恰一条提案、被拒零残留、双授权槽并发 endorsed 互斥独占、roundsIssuedTx 唯一清理点、提案与闸同用 effectivePass 无双头漂移）；D 顺序门（签发侧扣住/解除照签/证据侧纵深）；E 边界（跨重启回放、UNKNOWN 挂接、重走迟到证据 OBSOLETE、terminal 迟到对账、testkit full、façade/cli 全绿）；F 全量回归（除 internal/harness 本机环境既知失败——该包 0d3514c→ad695e9 零 diff）。三条初跑失败均复现定性为探针草稿陈旧期望（非引擎缺陷），修正归档。范围外建议：载入边界篡改残余面（与 P3 账同族）、释放槽历史绑定形状注释。

主代理复核：探针全套亲跑绿；B1 断言诚实性抽查（逐事务账目三元组＋槽释放＋zombie 语义）。模型调用：1 次子代理（5.3-flash）。

**内部红队清零达成（十跑双收敛发现×十一跑 P1 全部修复，十二跑六面 PASS）。R2 重新到达待外审状态，待机主送审。**
## 第 61 轮（2026-09-26，外审未通过（3 P1＋2 P2）修复：聚合未决阻塞＋下游决定项＋单一权威，轻装）

机主送外审（范围 origin/codex/dogfood-dsh..HEAD 31 提交）判未通过：3 P1＋2 P2＋逐维度代码要求表。主代理逐项亲复现（/tmp/audit-probe15，五条全坐实）＋规格判定：
- **F1/P1 未裁决 P1 被下一轮 PASS 覆盖**：规格 draft:276"未处置**或已确认** P0/P1 阻塞"——现行只投影最新候选（series.go Effective 单候选），canonical 测试固化了错误形状。修＝唯一定义点聚合：全系列未被整轮排除候选的 findings 求并集（DISMISS/INVALIDATE_FINDING 跳过、未裁决按原始严重度），存在 P0/P1 ⇒ FAIL；绑定锚最新未排除候选、跨 revision 候选不互拦（与 FreshFloor/HasConfirmedBlocking 同一 revision 界）；RecordCandidate/纠正/ReleaseUpstreamVoid/integrity 全部同源。correctFinding 的 stale 门随聚合模型收敛为"未排除且绑当前 revision"（否则旧轮阻塞不可驳回，与"全部 P0/P1 被驳回时直接 PASS"矛盾）。边界亲证：全候选排除×纠正＝可区分拒绝非 panic。
- **F2/P1 需求变更缺下游工作决定项**：规格 §5 处理顺序 2/5/7＋§5.1"使下游失效"＋机主修订（未决定前不自动失效，但决定项不能缺席）。修＝载荷增第五项 `RedoDownstreamWork`（omitempty 保事件字节）：勾选＝既有 terminate 机制退位未落定下游派发＋已落账结果入既有 ObsoleteResults；不勾＝同事务 carry RecoveryRecord 留痕、接纳不变。零新账本、零第二套终止路径。
- **F3/P1 未标记 worker 自报审查系列被接纳**：AGENTS.md 单一权威。修＝**删**声明回退（engine.go 声明回退分支整个死亡，幻影轮世系终结）：审查证据只在签发标记非空且与 evidence.Action 相等时接纳；floor/终止只认标记；unaffiliated 枚举保留（步骤词汇）。依赖回退的测试翻转为拒绝负控。
- **F4/P2 过期结果丢评审证据**：ObsoleteResults 落账改复用 workerResultRecord（:1760 本就携带，如实核证）。
- **F5/P2 façade 面缺裁决身份**：ReviewActionFace 增 Candidates（ResultDigest/DispatchID/RequirementRevision/RawStatus/Excluded/Findings{ID/Digest/Severity/Message/有效裁决}）＋EffectiveRevision；内核 resultExcluded 导出为 ResultExcluded（单一定义点）；补"仅凭 face 构造 CorrectionInput 完成 DISMISS"走查。
- P3（只记账）：cloneState JSON 往返不带未导出事务瞬态（评审提出、未复现正常路径故障）。

机主红线（派发前再令）："不要哪错哪打补丁屎山越摞越高"——修法逐项过补丁判据（删机制/补全枚举/唯一定义点），已入记忆。15 文件 +419/−139；未新增事件 kind/错误码/账本/state 字段。

主代理复核：build/vet/gofmt 净；全量（除 internal/harness 本机环境）零失败；验收探针五条（/tmp/audit-probe15/accept，F1–F5 翻转形状）全绿；原缺陷形状探针如期转红；anchor 下溢边界亲证安全。**probe14 七红逐一定性**：六个＝F1 同族旧世界期望（其走查正是"下一轮 PASS 覆盖未裁决阻塞"形状——N1 转储实证聚合生效：晚到阻塞 finding 使 effective=FAIL 而闩单向保持 true），一个（A3 伪造/删除完成态 bricking）＝第 60 轮已记账的载入边界篡改 P3 残余；不变量测试（首过单写/签发计数/并发独占槽/顺序门三向/迟到证据）全绿。probe14 红项属参考探针陈旧期望，迁移随下一轮。

**外审五发现全部修复。R2 待重新送审。**

## R2 审阅返修交接（2026-09-27）

本会话继续完成需求变化与审查调度的结构修复：需求变化入口先建立屏障，同一请求后续绑定完整确认包；审查义务统一参与调度，系列内所有载体的依赖使用同一义务；审查额度耗尽生成 typed Ask；需求内容身份与确认/执行代次分别绑定，重走义务不被旧结果清偿。删除被替代的调度合并、重复签发判定及 facade Next 覆写路径。

独立复核确认的缺陷全部返修后，最终审查为 PASS；代码要求一并检查，注释问题也已处理。末轮审查复用了独立审查代理会话，并非新零上下文审查。本记录不表示阶段 4 Seal，也不表示真实宿主完整 canary 已完成。

验证证据：`go test ./internal/engine/... ./cmd/... -count=1`、8 项相关 CLI 回归、定向生命周期回归、`go vet ./internal/engine/... ./internal/cli/... ./cmd/...`、生成物新鲜度检查及 `git diff --check` 均通过；主代理另用临时独立探针验证屏障入口、原内容再次确认后的新鲜审查、额度耗尽和同系列多载体依赖，并检查负控。引擎、CLI 与生命周期输出分别留在 `/tmp/boundary-engine-tests.log`、`/tmp/boundary-cli-tests.log`、`/tmp/boundary-lifecycle-tests.log`。未重跑全仓长期 harness/validate 套件，未调用真实外部模型。

用户已授权接续 R3：QA 覆盖契约接入、FROZEN/review-ready 屏障、验证 wave 与开发后 finding/problem 归并、有限重试、唯一返修。R4 guard 和 R5 Git provider/canary/route matrix 仍属后续单元。本地提交用于将本次已审阅修复交接给新的零上下文开发代理；不推送。

## R3 开发派发（2026-09-27）

用户授权派子代理实施，代码要求沿用此前约束。已派新的零上下文开发代理，本批从 `7eb32e8` 开始；同批连续实施，不跨批复用代理记忆。另派独立验收代理从需求准备正常公开路径及恢复对照。

范围依本文件第 46 轮和增量交付计划阶段 4：QA 覆盖、冻结和候选绑定、验证 wave、开发后裁决及唯一返修、有限 attempt。3.5c 明确修订的可选建议处置与中断续跑规则优先于旧的强制吸收措辞。R3 验收到 canonical facade 的 Drive/Submit 可到达验证完成及待 promotion 的 typed 边界；不将尚未实施的真实 Git provider、guard/cost bridge 或完整 regular Seal 计作本批完成。

设计取舍依据 `prompts/actions/start-readiness.md`：“是否有更简单的既有归属者或原生能力可用，以及它是否是足够且最简的做法”；依据项目准则：“一个控制事实只有一个权威来源”、“正常执行与崩溃恢复共用同一业务处理与接纳路径”。覆盖继续归 coverage，业务状态进入现有 State/Store，调度使用 RequiredTasks 派生投影，接纳与恢复共享同一事务路径。先复用 handler 表达业务职责，只有现有能力确实无法表达时才新增契约字段。不得反复 complete/reopen 静态 DAG 来驱动动态业务，也不得保留被替代的平行调度或计数路径。

实施包括必要的 definition/facade/typed 事件接线及相关生成物；候选事实必须来自已签发 adapter intent 对应回执，不能由自由 Operator 声明冒充。共享纯需求解析和执行证据校验仅按实际接入需要抽取，legacy 保留委托入口，不产生第二 writer。持久化遵循本期具体方案 `final-implementation-draft.md` §3.3 的“state.json 继续是唯一权威工作流投影”及不引入 SQLite 的明确边界，不在本批迁移现有 Store。

验收要求覆盖 source/point/case 完整对账、真实 FAIL 和 checkpoint、建议处置与受影响复审、屏障前零执行、收齐与核实前零返修、同快照唯一 repair、纯 P2 carry、有限重试及一次追加、迟到/重启/需求变化隔离重放。相关回归按改动面执行，真实外部模型调用未授权。开工记录不表示已完成或通过验收。

### R3 实施中复核记录

另一次复用审查会话的派发被审查者按上下文独立性契约拒绝，返回 RUNTIME_ERROR，未进入代码审查；随后成功创建新的零上下文代码审查代理。独立公开 API 验收代理和代码审查代理分别工作，未据中间通过结果宣告交付。

主代理已亲自复现并交回的正常路径缺陷：typed candidate 参数未进入共同结构化参数校验；已知中断恢复丢失业务重试身份并绕过同 TaskKey 上限；纯门审 P2 修复后错误增加验证轮数；未知 affected case 被接纳；同 AC 被黑盒/白盒共同覆盖时停滞；最终候选丢失白盒测试工件；需求变化 CARRY 同时保留原任务并派发新任务；已有白盒工件在 CARRY 后丢失不可变候选引用。复现输出保留于 `/tmp/qa-pipeline-root-public-path.log`、`/tmp/qa-pipeline-recovery-behavior.log`、`/tmp/qa-pipeline-root-p2-carry.log`、`/tmp/qa-pipeline-root-unknown-case.log`、`/tmp/qa-pipeline-root-shared-ac.log`、`/tmp/qa-pipeline-root-whitebox.log`、`/tmp/qa-pipeline-root-carry.log`、`/tmp/qa-pipeline-root-whitebox-carry.log`。这些是实施中失败证据，最终修复状态需以本记录后续验收为准。

实施中运行：整包 CLI 回归通过（`/tmp/qa-pipeline-cli-tests.log`，86.146s）；coverage 与标准需求解析定向测试通过；legacy QA efficiency 初次因并行实施尚缺函数而编译失败，完成后定向重跑通过（`/tmp/qa-pipeline-legacy-efficiency.log` 与 `-final.log`）。首轮 engine/cmd 整包回归失败，包含生产定义替换后演示图测试夹具耦合及真实失败分类等回归（`/tmp/qa-pipeline-engine-tests.log`），尚待整体复跑。临时恢复探针一轮被测试夹具 definition 版本混用阻断（`/tmp/qa-pipeline-root-recovery-final.log`），未将该结果冒充恢复行为通过。

### R3 交付与最终复核（2026-09-27）

本批实现完成，独立公开接口验收 PASS，独立代码审查及最后 detail remedy 补审 PASS。新开发、验收和代码审查代理均以零上下文开始；各自后续在同批会话持续返修/复核。主代理亲自核实原始失败及修后结果。当前没有未闭合的范围内实证缺陷；本结论只覆盖本批协议交付，不是整个阶段 4 的 Seal。

实际交付：canonical regular 入口接入确认的需求/方案来源、source/point/case 覆盖、逐项审查、建议处置、FROZEN 和含白盒测试工件的 review-ready 候选；并行验证 wave、finding/problem 裁决史、同快照统一 repair、纯门审 P2 carry、AFFECTED 与最终 FULL；有限 attempt、一次追加、checkpoint 续跑；需求变化对原签发工作的 CARRY/REDO；typed detail remedy。候选由精确绑定 intent/receipt 提供，真实 provider 仍在后续范围。

结构收敛：coverage 持有结构校验与 whitelist 投影；纯需求解析移入共享 requirement 包，legacy 委托调用；业务事实进入现有 State/Store。尝试计数使用现有 Attempt；正常接纳与恢复使用同一身份/接纳路径。wave 保存冻结覆盖与审查材料以核对其原始派发结果，当前需求投影不覆盖原证据。通用步骤测试图移入 testkit，生产 definition 只承载当前业务职责。未保留独立可写 whitelist、平行重试计数、候选专用签发、Next 覆写或伪造 PASS 验证路径。

已闭合的问题包括：候选参数契约不一致；业务恢复重试身份丢失；无效结果/缺 Review 绕过有限重试；P2 修复错误计轮；未知 affected case 和缺逐项自跑证据被接受；双 QA 共用 AC 被拒；候选丢白盒测试及删除；CARRY 重复派发、丢已审工件、原在途 review/execution/gate 身份失配（含公开 DECIDE panic）；确认方案 source 与非 QA 验证接入缺失；detail requirement Ask 选项与既有确认接口不一致。末项修正复用既有 proceed/abort 常量，同一 Ask 补包、确认/取消与屏障继续使用原路径。

| 用户代码要求 | 本批结论及依据 |
|---|---|
| 正确性、全部范围内需求点 | 通过有限验收；19 项公开 facade 场景加独立恢复、在途 CARRY、缺审查证据与 detail requirement 分流复核；排除项见下文 |
| 质量、简洁、优雅、可读、可维护 | 独立审查通过；优先既有归属，领域模块按职责组织，新增大字面量已分行，无额外通用平台 |
| 低耦合、单一权威 | 通过；当前状态与冻结签发证据分开，原始材料不被重绑覆盖，whitelist 只计算投影 |
| 无不当硬编码、窄逻辑、过拟合 | 未发现未解决的范围内问题；补齐多 source、多 QA、原始签发身份及正常需求变化，未针对样例 ID 放行 |
| 无过度设计、防御、抽象 | 独立审查未发现阻塞；未新增 registry、独立确认、数据库或通用框架 |
| 从根因解决、不叠补丁 | 通过；移除被替代路径，覆盖/签发/恢复/接纳保持既有归属 |
| 测试不污染业务 | 通过；测试图在 testkit；候选夹具按实际输入身份装配并断言测试内容/删除，生产代码无验收探针分支 |
| 不改期望迎合实现、诚实证据 | 重试测试按已确认“终结 attempt、同 TaskKey 重派”语义调整；持久化测试补齐实际 Review，原接纳/重启断言保留。独立 P2 oracle 原先误要求初次不计轮，按主需求 §8.4.5 更正为初次计 1、P2 修复不增加，并保留说明 |

最终验证与限制：
- 主代理完整执行 engine/cmd 包回归，除 4 条仍使用裸 PASS 代替 Review 的旧白盒夹具外均通过；两次失败输出保留在 `/tmp/qa-pipeline-engine-final.log`、`/tmp/qa-pipeline-engine-verified.log`。修正夹具后，主代理执行最后所有受影响包 `go test ./internal/engine/protocol ./internal/engine/facade ./internal/engine/whitebox_qa -count=1`，全部通过（45.474s / 15.524s / 16.058s），原始输出 `/tmp/qa-pipeline-changed-packages-final.log`。因此不把先前整包命令的非零退出记为成功。
- CLI 整包最终运行通过，110.067s，`/tmp/qa-pipeline-cli-final.log`。共享需求解析与 legacy QA efficiency 定向回归通过，未重跑全仓长期 harness/validate 套件。
- 独立公开 facade 最终集合 19 项通过；主代理在冻结 wave 修正后再跑同套，40.846s，`/tmp/qa-pipeline-root-acceptance-verified.log`。覆盖矩阵在 `/tmp/pipeline-acceptance-0sjt2ua2/acceptance-status.md`。
- 主代理恢复身份/有限额度、容量变化、INVARIANT/BLOCKED 终态、缺 Review、在途 CARRY 和 detail 取消路径复核均通过；原始输出分别为 `/tmp/qa-pipeline-root-recovery-verified.log`、`/tmp/qa-pipeline-root-failure-capacity.log`、`/tmp/qa-pipeline-root-missing-review-verified.log`、`/tmp/qa-pipeline-root-inflight-carry-verified.log`、`/tmp/qa-pipeline-root-detail-requirement-verified.log`。在树保留缺 Review 2+1、source/nonQA、双 QA 内容及删除、CARRY 三种在途职责、同一 requirement Ask proceed/abort 等回归。
- 在途 CARRY 修前失败保留 `/tmp/qa-pipeline-root-inflight-review-carry.log`、`-inflight-wave-carry.log`、`-inflight-gate-carry.log`；detail requirement 修前失败保留 `/tmp/qa-pipeline-root-detail-requirement.log` 和 `/tmp/validation-requirement-remedy/before.log`。开发收尾有一次测试字段名编译错误，保留 `/tmp/qa-pipeline-carry-fixture-tests.log`，修后定向通过；不计作生产逻辑缺陷。
- 最终 `go vet ./internal/engine/... ./internal/cli/... ./internal/requirement/... ./cmd/...`、55 个变更 Go 文件 gofmt 检查、`git diff --check` 通过。definition 生成物新鲜度包含在 definition 包回归中。

未执行 R4 guard/cost bridge、R5 真实 Git provider/promotion、真实外部模型/宿主 canary、进程 kill 全矩阵或 Seal。测试中 adapter/worker 是输入身份明确的协议夹具。当前交付到验证完成和待 promotion 的明确边界；没有推送。

## 验证生命周期返修合同（2026-09-27）

用户明确要求将全部代码要求作为硬标准并立即返修。当前基线为 `3b8bb58`。上一次“从根因解决、不叠补丁”的通过结论被本次公开路径反例推翻，不再作为验收依据。全仓回归另有 validate 的阶段记录 identity 不匹配和 harness 超时；须分别核验，不混同为四项业务问题。

设计依据原文：AGENTS.md“调用轮次、任务代次、重试次数、业务返修轮次是四个概念，各有权威事实源，不得互相冒用或合并计数”；“正常执行与崩溃恢复共用同一业务处理与接纳路径”；start-readiness.md“是否有更简单的既有归属者或原生能力可用，以及它是否是足够且最简的做法”。据此，开发先核对任务身份、Ask 生命周期、QA 完成条件和角色输入四处归属及受影响入口，复用已有事实表达修正，不预设新增计数器、账本或通用框架。沿用本期确认的 state.json 唯一状态投影。

硬验收条件：
- 白盒测试实现变化而 manifest 不变时，每次合法设计返修可派发；任务代次、有限 attempt 和语义审查额度分别受既有规则约束。
- 当前仍有效的 RETRY 等授权请求可在独立分支推进后得到新鲜展示和批准；旧请求不能授权，新请求不会被自动替代记录当作已处置；一次追加额度不扩张。
- finding 驳回不消除独立 QA FAIL；合法 QA 更正/重跑或显式处置沿现有路径闭合，没有未解决 QA 失败的候选才能完成。不得为方便要求无意义生产返修。
- gate 输入满足主需求 §8.3.5 的隔离；各角色仅携带其职责需要的现有契约信息，QA 所需覆盖与执行绑定仍完整。
- 逐项核验全部代码要求：正确性、需求完整性、质量、简洁、耦合、维护性、可读性、设计一致性、不当硬编码/窄逻辑/过拟合、过度设计/防御/抽象、测试与业务隔离。每项须给当前实现或测试证据；没有验证不能填通过。
- 删除被替代路径；跨正常执行、必要的重复/并行与恢复边界验证同一业务不变量；测试命名去过程化，诚实失败记录，不反改需求期望。

派发给同一开发代理连续返工，设计事实先由主代理核验再实施；独立审查不接收开发者通过结论。主代理承担独立复现、代码与标准验收、记账。禁止 push，禁止真实模型/宿主 canary。候选来源参数建议尚未证实缺陷，不纳入功能扩展。

### 验证生命周期返修与独立复核

本次主代理把代码要求的验收执行写入 AGENTS.md：功能通过不能替代逐项工程核验；设计先明确业务不变量、事实归属和相关入口；新增状态/抽象须解释既有能力不足；被替代路径须删除；未验证不得记通过。开发代理在方案经主代理核验后实施，主代理没有直接修改业务源码。

已实现并通过公开路径核实的归属修正：
1. 设计义务身份从已接纳的有效 design Results 推导，manifest 版本、ReviewHistory 和 Attempt 分别保留内容、语义审查和执行重试的含义。
2. 自动 Ask 是否已有用户处置以 Decisions 为事实源；自动替代的 Resolved 记录不阻止新鲜授权，旧 token/revision 仍拒绝，同 TaskKey 只追加一次。
3. finding 驳回与 QA 执行义务分别核算；未解决 QA FAIL 保持待处置，授权更正走设计/审查/新候选/重跑，显式豁免保留 DISPOSED、原因和原始失败证据。
4. ValidationInputs 根据既有 handler 投影；门保留需求和候选绑定，移除 QA 用例正文，QA 保留覆盖和执行所需材料。
5. 所有集合重开入口集中到 CaseSet.reopen，失效当前审批、保留历史审批，累积尚未消化的影响；只有新审查接纳后消化影响，建议 DEFER/DECLINE 不能覆盖重新设计义务。
6. 白盒审批历史补充原签发 Candidate 关联，内容等价只复用已有已接纳 candidate 回执 Identity/Digest；不把 TestRef 相同当作测试实现未变，不增加第二摘要账本。
7. gate carry 集中判定：同候选保留真实 PASS/DISPOSED；新候选只有真实 PASS 或该目标已完成的 P2 修复可继承 PASS，其余保持待执行。

第 5–7 项来自本次全新零上下文审查；主代理逐项独立复现后交回开发。其中白盒探针用真实 tree 摘要和实际 go test 证明共享测试修改导致原用例 PASS→FAIL；不能以不同 native identity 单独推定内容变化。多条建议覆盖未完成影响的问题由同一重开归属一并修复。测试前置曾有 manifest oracle 与执行描述不一致，已要求真实提交 exit-1 oracle 后再观察 exit-2，原断言未降低。

另两项全仓失败已查明并处理：历史阶段记录错误绑定当前 definition，现从阶段 1 sealed ref 核实历史 version 2/digest，并保留当前制品的独立复算；harness 命中 ignored 的旧本地二进制，其公开 supervise 命令不受支持。主代理核对普通文件/无链接后备份到 `/tmp/validation-obligation-oracle/local-binary-before-rebuild`，按当前源码构建本地 .harness/bin/formal-gates。HEAD 与基线的隔离源码构建均通过取消/退出码对照；未修改 harness 运行逻辑或增加超时，未修改固定全局安装。

独立审查结论 PASS，覆盖 7eb32e8 到本次冻结工作区的完整交付源码、开发者测试及必要文档；按合同排除 .gates/results 正文和独立 whitebox_qa 用例正文。原始独立探针在 `/tmp/validation-contract-review.Jp8kso/`。主代理另外合并九个公开路径探针独立执行，全 PASS（17.499s），含并行授权/重开、QA artifact 更正完整链、门输入、第三次白盒设计、checkpoint、P2 AFFECTED/最终 FULL 以及三项新增反例；日志 `/tmp/validation-obligation-oracle/final-independent-probes.log`。

| 硬标准 | 核验结果与证据 |
|---|---|
| 正确性、需求完整性 | 原始四项及复审三类公开反例均已修复，失败证据先留存；最终独立 probe PASS，独立审查核对覆盖/冻结/wave/裁决/有限重试/需求变化接缝 |
| 质量、简洁、低耦合 | 复用 Results、Decisions、ReviewHistory、HostActionReceipts；领域变换集中，无新增通用状态机、缓存或影响分析平台 |
| 维护性、可读性、设计一致性 | 重开、冻结、继承有明确归属；正常接纳与恢复共用原路径；实际新建字段只为不可变审查证据关联原候选 |
| 无不当硬编码、窄逻辑、过拟合 | 未按 case/gate 名称放行；实际正负内容、不同身份同内容、三类自动授权、多建议及同/异候选处置均有行为对照 |
| 无过度设计、防御、抽象 | 未建立平行账本或兜底旁路；仅增加已有领域不变量所需判据与候选证据关联 |
| 测试不污染业务、夹具诚实 | 模拟树与子进程测试仅在测试文件；生产不识别探针；真实内容变化可使 TestNegative 失败，不同 native identity 同内容仍能继承 |
| 根因修正、删除被替代路径 | 删除内容版本充当设计身份、Resolved充当决定、零problem直接完成、无条件QA正文、各处手写重开和无资格gate carry等路径 |
| 历史准确性与验证纪律 | 阶段记录绑定实际sealed提交；保留当前生成物检查；失败、未执行与验证范围分别记录 |

测试与范围：全仓 `go test ./... -count=1` 已退出 0（CLI 127.781s、harness 420.231s、validate 431.302s），原始日志 `/tmp/validation-obligation-oracle/full-suite.log`。该命令在最初四项修订冻结时启动，之后新增集合/继承/carry修正另做最终受影响包验证，不把较早全仓执行冒充最终所有源码的同次执行。最终 14 个变更 Go 文件 gofmt 检查和 git diff --check 通过。最终受影响包与 vet 完成结果接下条记录。没有 push，没有真实模型/宿主 canary，没有实际 provider/promotion 或整阶段 Seal 声明。

最终冻结后的受影响包 `go test ./internal/engine/protocol ./internal/engine/facade ./internal/engine/whitebox_qa ./internal/cli -count=1` 全 PASS（63.177s / 39.791s / 17.723s / 105.100s），日志 `/tmp/validation-obligation-oracle/final-affected-packages.log`。最终 `go vet ./internal/engine/... ./internal/validate/... ./cmd/...` 退出 0（`final-vet.log`）；历史记录单测由主代理独立复跑 PASS（`stage-records-verified.log`）。本次验收所发现的范围内缺陷和工程准则违反均已闭合；此结论保留上述明确的交付及验证范围。


## 模式隔离、路线变更与跨候选继承返修

2026-09-28 用户要求重新派子代理返修，并重申全部工程准则是硬标准。普通授权范围继续，不启动 formal-gates，不调用真实模型或宿主，不 push。起点 `9122eec`，工作区干净。

前条“范围内缺陷和工程准则违反均已闭合”被随后独立公开反例推翻，不再作为当前验收结论。零上下文审查 `7eb32e8..9122eec` 得到两项 P1（黑盒 design/review 收到白盒材料、追加普通门遗漏新目标）与一项 P2（继承只查来源候选紧邻返修，遗漏中间影响）。主代理亲读需求、源码、探针并独立重跑三项，均复现；checkpoint/retry 保留 FAIL 对照通过。原始报告 `/tmp/independent-validation-audit/report.md`，主代理执行日志 `root-recheck.log`。

返修单独派给 `validation_invariants_repair`（零会话历史，完整提供需求、硬准则、失败证据）；主代理不写业务实现。实施前要求并审核 `/tmp/validation-invariants-repair/design.md`，按真实 owner、入口、跨接缝字段及正负对照决定修改。

方案依据先于实现：

- 3.5c REQ-001/AC-002：“黑盒输入中没有本次实现、白盒测试或开发者解释”；现有 `executionContract` 已按 mode 裁剪。统一复用局部契约投影，设计/审查/执行各自只接本 mode 所需材料；repair 继续获得必要完整契约。
- master §6.3.1：“开发后只可追加普通门”；§8.3.6：“route/case-set/目标集变化使旧 wave 失效，不能中途修改集合制造收齐”。RouteMode/SelectedGates 仍是唯一路线控制事实；Wave 保留原冻结目标和证据。以已有 ResultAction 查原 finding owner，不复制 finding/history；已形成合法唯一 repair 保留原 owner/TaskKey/预算，修后候选运行新增门。
- AGENTS：“跨接缝传递的上下文必须显式列出携带者”。WorkBinding 与 CandidateParameters 分别承载 worker 与 adapter 已签发输入；当前签发记录没有 route scope，新增签发路线快照用于核对旧 case version，不能无条件接纳旧载荷。它们是既有签发证据字段，不另建控制账本。
- 3.5c AC-008 的受影响进度失效与 master §8.3.7 的 AFFECTED 执行，要求沿实际 candidate/RepairSource 变化链核全部影响。复用原 wave/repair evidence，不新增累计影响状态，不一律禁止合法历史继承。

主代理实施前/过程正向对照：普通门追加不打断已签发 QA review；重复同路线决定不变更冻结 wave；两次都只影响 invalid 时 normal 可跨两次返修继承；已签发 repair 在路线追加后仍完成并向新候选派新增门。另独立确认已收齐 FAIL 未裁决时追加 gate 缺口。材料 `/tmp/validation-root-oracle/`；尚无最终完成声明，后续记录仅依据冻结后实际验收。


### 返修结果与有界验收

本次实际实施集中于 `protocol` 原归属：`modeCoverageContract` 合并输入/审查/执行的模式契约裁剪；`rebindValidationRoute` 在路线确认事务及既有推进路径重绑，保留原冻结目标/证据；`validationFindingTarget` 用 ResultAction/DispatchBindings 定位唯一原 finding；`validationRepairPending` 区分仍有效的 route-only 返修与 THAW/需求 REDO；`validInheritance` 核完整 candidate/RepairSource 变化链。WorkBinding 与 CandidateParameters 各携 RouteScope，`manifestAtRoute` 共用签发版本核对。原重复裁剪正文、直接按当前 wave ID 判断 finding 的相关业务入口、只核紧邻一次 repair 的继承路径已替换，没有平行控制账本。

过程中的错误据实记录：主代理同意的首版 route 重冻方案使用 FULL，开发者自查与零上下文独立审查均发现，它会把已经终结的 AFFECTED/INHERITED QA 错当最终 FULL 通过。这是本轮修改引入的回归，不能归为原 `9122eec` 缺陷。主代理另写公开探针，在恢复错误行的负控中复现 FAIL（`/tmp/validation-root-oracle/scope-negative-control.log`）。修复为保留原 `w.Scope`，沿用既有 AFFECTED 完成后创建 FULL 的路径；不增加旁路。审查者实跑确认最终 FULL 先 COLLECTING，提交当前候选全部实际执行后才 COMPLETE。其原失败和修后日志分别为 `/tmp/validation-change-review/original.log`、`final.log`。主代理首次 scope 探针运行与开发者一行修改交错，取得的是修后 PASS，单独保存 `scope-first-run.log`，不冒充负控。

另有新增测试准备错误：退役 action 的 SpawnReceipt 应按既有协议拒绝，不能用默认成功 helper；runtime-error fixture 必须携 FailureClass。均修正测试准备并继续断言旧结果仅入 ObsoleteResults、新 wave 不受其影响。既有测试期望未降低。临时 oracle 入树按行为重命名，去掉来源前缀和不必要匿名函数；重复复制的对照删除，原验收断言保留。

| 硬标准 | 主代理核验与证据；结论仅限本次返修及直接影响面 |
|---|---|
| 正确性、需求完整性、实现质量 | 三原反例修后通过；追加门覆盖设计/审查/候选装配在途、COLLECTING、原FAIL裁决、REPAIR/retry、COMPLETE及late result；最终FULL不得借继承绕过的独立负控修后通过。已证实问题已解决。 |
| 简洁性、耦合、单一权威 | 复用已有State、wave、repair、DispatchBindings、ResultAction、Attempt；模式裁剪/原finding归属各一处；无新规则框架、缓存、累计影响表或副本finding账本。静态逐处检查未发现范围内违反。 |
| 可维护性、可读性、设计一致性 | 新函数按业务行为命名；路线只变验证义务集合，不改旧ExpectedDigest、不把DISPOSED变PASS、不撤销已签发合法repair；签发上下文显式携带，原历史签名保留。对应正负执行通过。 |
| 无不当硬编码、窄逻辑、过拟合 | 生产未出现特定caseID/测试名/候选ID放行；QA模式沿既有封闭业务枚举；跨多次repair首边/末边受影响拒绝，全部未影响及中途普通门追加仍接受。 |
| 无过度设计、防御、抽象 | 两个RouteScope字段分别补worker与adapter签发上下文；直接小函数复用既有能力，没有通用兼容层或第二状态机。未为未要求的provider/数据库迁移加实现。 |
| 测试隔离与夹具诚实 | 新增四个行为测试文件；生产无测试开关/fixture依赖。模拟adapter只建立typed前置，实际Submit/Drive/Open、接纳与obsolete持久证据被核内容。未将模拟回执声称为真实provider验证。 |
| 正常/恢复、重复/并行边界 | 重复同路线不变wave/预算；普通门追加保留在途review和已签发repair；staged/receipted/terminal晚到只审计；THAW取消不复活旧repair；worker/adapter仍经原共同接纳函数。相关测试通过。 |
| 轮次分离、旧路径清理 | 未改变设计/语义review计数；repair保留原TaskKey、1/2预算和extra授权；同candidate重冻不重复Counted；被替代的三类局部路径已删除，静态检查未见平行实现。 |

最终证据：

- 开发者原始复现 `/tmp/validation-invariants-repair/baseline.log` 三FAIL、一正向PASS；最终原四探针、主代理七项对照各PASS，见 `frozen-original-probes.log`、`frozen-root-controls.log`。
- 主代理合并11个独立公开探针重新执行全部PASS，25.395s，`/tmp/validation-root-oracle/final-probes.log`。包含未影响case跨两次repair合法继承、原finding唯一归属、既有repair不丢以及已终结增量结果不得升级FULL。
- 最终生产源码 `go test ./internal/engine/facade ./internal/engine/protocol -count=1` PASS，82.608s/80.407s，`/tmp/validation-invariants-repair/frozen-core-regression.log`。此后仅测试匿名函数改局部值并删除一段重复对照；相应目标单测另跑PASS（`test-cleanup.log`），没有再改生产源码。
- 零上下文独立代理 `validation_change_review` 未读先前报告/交接/问题清单，从当前需求与本次diff独立审查，最终PASS、无未解决范围内P0/P1/P2；含两条独立scope探针的一次针对性回归PASS，51.116s。报告 `/tmp/validation-change-review/report.md`。
- 补充回归：八个有测试核心包以-count=1通过、requirement无测试（`/tmp/validation-invariants-repair/final-regression.log`）；CLI/已有whitebox_qa包分别109.555s/23.325s通过（`/tmp/validation-root-oracle/integration-regression.log`）。这些命令在scope单行修正前启动，记为中间态补充证据，不冒充最终全仓执行；scope修正后的核心与针对性测试见上。
- 最终 `go vet ./internal/engine/... ./internal/cli/...` 退出0（`/tmp/validation-root-oracle/vet.log`）；变更Go文件格式与git diff --check通过。

本次验收完成的是三个已证实缺陷及其直接状态/证据衔接，包含本轮引入回归的修复。没有执行全仓go test ./...、race、真实provider/模型/宿主canary、guard/cost bridge、数据库迁移、完整Seal或安装切换，不为这些范围背书；不把“检查范围未发现违规”扩大为代码永久无缺陷。无push。

### 完整当前阶段复核与语义返工验收

用户质疑上段验收范围过窄。主代理重新派发零上下文完整审查，比较 `7eb32e8..3f15055`；结论为两项 P1、两项 P2，主代理全部经公开 facade 路径独立复现。上段局部验收不能证明整个当前阶段达标。完整初审原文与证据：`/tmp/typed-delivery-review.p8FKPb/report.md`、`root-recheck.log`、`root-control-recheck.log`、`root-barrier-recheck.log`。

返工前引用 AGENTS 的单一事实归属、正常/恢复共用接纳、四类计数分离及 start-readiness 的最简既有归属者判据。实施代理先提交 `/tmp/validation-semantics-design.md`，主代理核查后实施。修后独立审查范围固定为 `7eb32e8` 到最终工作树，包含本次全部未提交修改；没有缩为最后返修 diff。仍是已授权的 Git 非拆分 regular typed QA/validation 交付。

| 已证实问题 | 最终处理与证据 |
|---|---|
| P1：PASS 门附 P2，完成修复仍再生成同一问题返修 | PASS/FAIL 共用原 Repair.Problems/Evidence 完成依据，ResultAction 携 repair 身份；保留原 findings/history。原独立反例及另一门 PASS/DISPOSED、AFFECTED→FULL 对照通过。 |
| P1：需求变化屏障内裁决观察仍结算旧 wave、增加 Counted | typed 观察只保存原事实；case 重开/冻结、绑定投影与 wave 结算集中于既有 advanceValidation，签发前也经同一路径。两重屏障、取消后一次恢复、确认 redo、建议和 inventory 观察均有实际对照。 |
| P2：累计审查历史误作连续 FAIL 额度 | 从 ReviewHistory 尾部派生连续失败；历史长度只用于身份；额外授权绑定当前语义审查，签发后由原 dispatch/carry 保留权利。成功重置、第二段失败、已授权审查需求续用及重复事件通过。 |
| P2：DISPOSE 被迟到 retry 结果撤销 | 精确 TaskKey 下调用既有 retireAction，清理重试资格；staged/receipted/terminal/runtime-error 四种交付顺序均留原结果审计，目标保持 DISPOSED，其他并行 QA 可完成。 |
| P2：多对多 checkpoint 只完成部分 edge，却隐藏整个待完成 case | 完整交付审查新增发现，主代理独立复现；PendingCases 直接按冻结 whitelist 的 ReviewKind/SourceID/PointID/CaseID 与原 records 对账。独立反例及三段提交、PASS/FAIL、重复事件和 Open 对照通过。 |

过程记录：将冻结移至统一推进后曾出现 refill 早于收尾而重复签发 review，已改为 refill 在签发前调用同一 advanceValidation，并验证实际 FROZEN/history，而非只检查没有 Ask。实施中曾新增“已退役 action 首个迟到 spawn receipt 接纳为 OBSOLETE”，核需求后确认非必要，完整撤回生产分支及旧 route 测试期望变更。最终协议仍以 UNKNOWN_ACTION 拒绝该 receipt 且零写入，迟到 WorkerResult 独立入原 ObsoleteResults。主代理探针据既有协议修正便捷 helper 假设，保留并加强处置、原载荷唯一审计、不重派断言。另一个跨候选工具/环境继承候选经需求及参考实现核对撤回：3.5c 新绑定规则只针对同次进度，不能扩大为跨轮新限制；未为它修改生产。

| 用户代码要求 | 主代理核验及完整独立审查结论 |
|---|---|
| 正确性、质量 | 五项有修前公开复现与修后执行；无剩余范围内 P0/P1/P2。 |
| 需求完整性 | 独立审查按 15 组核对整个当前交付，从 inventory、逐项审批、候选、并行、wave、问题归并、返修、重试、需求/route 控制至恢复，非仅五项反例。 |
| 简洁性 | 复用原 State、ReviewHistory、Repair、DispatchBindings、Attempt、whitelist/records；未新增持久字段、完成账本、框架或依赖。 |
| 耦合、事实归属 | 观察与推进分离；授权在签发处判定，接纳核原 dispatch/carry；退役复用已有执行生命周期。 |
| 可维护性 | 正常、配对、重开/恢复沿同一接纳与推进路径；删除接纳阶段重复上限算法和观察内嵌业务推进。 |
| 可读性、注释 | 业务行为命名，无修复编号或测试来源；重试默认值注释已改为与当前 agent 默认两次的实现一致。 |
| 优雅、设计一致性 | 保持既有 typed events、事务、TaskKey/Ask/receipt，不引平行机制；无必要的回执公开扩展已撤回。 |
| 不当硬编码 | 无 case ID、探针名、候选 ID 特判；三次连续失败来自需求。 |
| 窄逻辑、过拟合 | PASS/FAIL 同类语义合并；全部 typed 观察入口、真实交付顺序、连续成功/失败及多对多映射均核对，未只在复现行兜底。 |
| 过度设计、防御、抽象 | 不增加通用框架、历史完成缓存或未来平台加固；撤回未获需求支持的跨轮限制候选。 |
| 测试期望与夹具 | 原 route 测试最终未改；测试实际 FROZEN/历史、精确绑定、原始持久记录、重复事件与副作用；不把模拟 adapter 说成真实 provider 验证。 |
| 测试不污染业务 | 新测试均在 facade 的行为测试文件；生产不依赖 fixture、测试目录、临时路径或专用开关。 |
| 被替代路径删除 | 删除 qaReviewLimit、以历史总数判失败额度、freezeReviewed、观察直接 settle/reopen/freeze/carry、按任一 CaseID 记录删除 pending 的错误路径；没有并存的替代账本。 |

最终证据与实际边界：

- 主代理最终 12 组公开入口独立探针全部 PASS，10.263s：`/tmp/validation-semantics-oracle/root-delivery.log`；组合 overlay 和测试源位于同目录。新增 checkpoint 独立修前/修后为 `root-checkpoint-before.log`、`root-checkpoint-final.log`。最后仅修改重试默认值注释，不改变执行结果。
- 原四项最终 facade/protocol 全包 PASS，80.140s/62.283s：`/tmp/validation-semantics-final-packages.log`。执行早于最后 checkpoint 投影改动，后者追加已核的定向独立反例及分段 PASS/FAIL 回归，不冒称最终全仓运行。
- 实施报告 `/tmp/validation-semantics-delivery.md` 逐项列正常/重复/并行/恢复覆盖、原始日志、中间版本与范围限制。
- 零上下文完整交付审查 `/tmp/typed-validation-acceptance-fyp_ugmq/acceptance-report.md` 最终 PASS，含 15 组需求与全部工程要求逐项证据。其相关 facade/protocol/coverage/definition/decision/encoder/acceptance/testkit/parser 执行及最后 checkpoint 独立复核均保存原始日志；已撤回候选的需求依据单列。
- 主代理核对审查记录中 33 个文件与工作树身份一致；收尾注释由实施代理修改并交审查者核对。`git diff --check` 通过。

当前验收覆盖整个已授权 typed 阶段。真实 provider/promotion、guard/cost bridge、真实宿主 canary、完整 Seal、数据库迁移、split/retained merge 属后续交付；本次未执行全仓/race/安装切换或真实模型调用。工作区保留修改，未 commit/push。

### 验收结论撤回与重新返工

2026-09-28：撤回上一节“无剩余范围内 P0/P1/P2”和完整当前阶段验收通过的结论。后续独立审查发现两个正常公开路径缺陷，主代理已独立复现：未决的 VALIDATION_DETAIL_DISPOSITION 仍允许返修结果接纳和候选推进；需求确认指定 gate REDO、QA CARRY 时，同一候选的旧 gate PASS 抵消 REDO，QA 则被重复派发。此前测试通过和审查者 PASS 均不足以支持该验收结论。

审查原证据位于 `/tmp/validation-review.1mKxjm/`。主代理另建公开入口对照，覆盖两次连续需求变更中的全部 QA/gate REDO/CARRY 选择及重开，原始失败日志为 `/tmp/validation-root-acceptance/before.log`。返修进行中的处置交付顺序结果保存为同目录 `disposition-during.log`，不冒充修前基线或最终验收。

重新返工以 `7eb32e8` 到当前工作树的完整已授权 typed QA/validation 交付及全部工程准则为边界；上述反例仅为已证实失败，不能代替完整需求核对。实施由独立子代理承担；完成后另派零上下文审查，主代理独立核实失败、修后结果及逐项工程证据。此记录不表示返工已完成，不启动正式流程，不改变后续阶段的排除范围。

首次返工后已冻结源码，主代理启动 `independent_delivery_review`，实际参数 `fork_turns: none`。派发仅含完整当前需求、工程标准、累积比较范围与结果契约，未提供历史问题、修复说明、通过预期或指定关注点。主代理同时独立执行 engine 全包和 validate 回归，全部退出 0；记录在 `/tmp/validation-root-acceptance/frozen-regression-before-review-rework.log`，其中 validate 为 477.636s。该快照仍有以下公开路径失败，因此不予验收：

- 独立审查提出、主代理另写探针复现：全部逐项 PASS、省略可选诊断字段 SetStatus 后仍重复派审。`review-status-before.log`。
- 主代理自行设计跨流程探针复现：AFFECTED 中有继承结果，追加门后进行需求确认，QA CARRY / gate REDO 被投影成 FULL，未全量执行即 COMPLETE。`scope-before.log`。
- 独立审查提出、主代理核公开前置并独立执行复现：REVIEW_READY 在途时授权 THAW、修改并审查新用例，旧候选回执被拒且永久阻塞新候选。`thaw-before.log`。

上述日志和主代理探针均在 `/tmp/validation-root-acceptance/`。实施代理已亲跑三条失败，当前只读分析事实归属、scope 语义和过时副作用接纳；审查尚未收尾，源码仍冻结。绿测没有覆盖并消除这些反例，不能替代需求与代码工程验收。


后续完整审查还确认合法 gate-only custom 路线会卡住；主代理独立执行完整完成路径复现，并在白盒相同 manifest/路径但测试实现已换的场景独立复现旧产物误用。连同 optional review verdict、AFFECTED carry、候选过时回执，实施已按原事实归属统一返工。设计/实施报告 `/tmp/validation-rework-design.md`、`/tmp/validation-rework-delivery.md` 保留原始失败与本次变更。

第二次冻结后实际新建 `final_independent_delivery_review`，`fork_turns: none`，未向其提供旧问题、修复结论、探针或旧报告，范围仍 `7eb32e8` 到整个工作树及全部工程准则。主代理 22 组独立 oracle、engine 全包、CLI、vet 均通过；但该新审查提出 master §6.3.5 pre-wave 三 FAIL 后精确 waiver/skip 的控制入口缺失，主代理读原文和 handler 并独立执行公开路径再次证实。日志 `/tmp/validation-root-acceptance/independent-control-surface-before.log`。此时继续不予验收，完整审查仍在进行，实施代理先只读核完整用户处置义务、事实归属和最简设计；没有宣称第二次冻结已经完成。汇总工作报告 `/tmp/validation-root-acceptance/acceptance-report.md` 明确标为不予验收。

### 完整控制返工冻结后的再次独立审查：仍拒收

- 新审查者 `whole_delivery_zero_context_review` 以 `fork_turns: none` 创建，审查 `7eb32e8..HEAD` 加整个工作树及未跟踪开发者测试。只提供完整需求、工程标准和当前交付边界，不提供旧发现或通过结论。
- 主代理冻结正文核对 83 文件；合并独立 oracle 29 组通过（203.852s），engine 全包及 CLI 通过，vet/gofmt/diff-check 通过。这些不是完整交付通过的证据。
- 新审查发现且主代理已独立复现四项失败：在途白盒旧审查可冻结新测试产物；candidate 的 BLOCKED_BUG 终止失败后自动重发；业务 spawn 瞬态耗尽无 typed 处置 Ask；testkit failure-routing 手造 WAIT 报告而实际 Plan 为 ASK。
- 当前不予验收；正确性、需求完整性、跨接缝责任归属和夹具/报告诚实仍有已证实违反。源码保持冻结，实施代理只读完善统一根因方案，完整复核和最终报告后返工。未 commit/push，未真实模型/provider调用，未启动新 formal-gates run。
- 原始日志、逐项工程表与所有中间状态记录：`/tmp/validation-root-acceptance/acceptance-report.md`。新审查证据目录：`/tmp/whole-delivery-review.iJzlbk/`。


### 后续返工及当前完整审查：尚未验收

主代理持续拒收有范围内功能/工程缺口的版本。第四次冻结的新零上下文审查确认首次review范围、candidate完成补位、逐case白盒内容复用证据表达及终态提前发布四项P2；全部经主代理公开路径独立复现，再按现有归属统一返工。终态保留Drive唯一清理写归属、公共发布共用summary判据；candidate效果统一走原held清算；review复用原历史/receipt，没有新增平行账本。详细原始日志及设计在 `/tmp/validation-root-acceptance/acceptance-report.md`、`/tmp/validation-rework-design.md`。

第五次冻结核对91文件正文不变。主代理40组组合oracle、engine/CLI相关整包、真实Ask载荷及TestRef绑定对照通过，vet/格式检查通过。新审查 `whole_scope_independent_audit` 用 `fork_turns: none` 创建，范围仍为完整 `7eb32e8..HEAD` 加整个工作树/开发者测试，未提供旧问题或通过预期。该审查仍在进行；确认source-point关系被遗漏却生成白名单已由主代理独立执行复现，尚未修复，因此不能宣称当前交付通过。跨轮继承额外工具/环境限制候选因缺乏需求依据撤回，没有为它添加代码。

用户再次强调禁止粗暴叠补丁。实施者当前仅只读分析完整映射链与执行子集规则；源码保持冻结，待完整审查及最简方案核定后继续返工。原执行证据：`/tmp/validation-root-acceptance/publication-frozen-oracles.log`、`publication-frozen-packages.log`、`audit-candidates-before.log`；新审查证据目录 `/tmp/independent-delivery-audit.J4dJ2f`。未commit/push，未调用真实provider/模型，未启动正式流程。

### 当前94文件冻结复核：未通过，继续按业务责任归属返工

确认关系与AFFECTED宿主固定执行子集已由实施代理按原owner修复，旧point-only校验与孤立继承查询删除。主代理核对94份正文不变、17文件完整差异，engine/CLI整包与vet/格式/diff-check均通过。42组独立oracle首跑有1组缺新host确认前置，临时副本只补显式业务步骤、断言不变后该组通过；首跑失败及重跑分别保留，不能改写成一次全绿。

新完整审查`fresh_complete_delivery_audit`实际`fork_turns:none`，没有收到旧问题/结论，当前又确认跨流程P2：开发有限重试耗尽后的未完成责任漏出RequirementWork，用户无法逐项CARRY/REDO；需求修订自动新开开发，完成后旧RETRY Ask仍残留。主代理独立复现`/tmp/validation-root-acceptance/exhausted-requirement-before.log`；另自建正常/重开×CARRY/REDO完整合同oracle，修前四分支都因责任漏项失败，`exhausted-work-contract-before.log`。旧Ask可decline规避，未夸为完全不可继续。

当前正确性、需求完整性、业务责任与attempt生命周期接缝仍未验收。实施代理仅获只读根因定案授权：原EndedAttempts/WorkDecisions应共同保有未完成业务责任；续用不重置额度与身份，重做关闭旧控制归属且保原始证据；不能只清提示或按generation打补丁。完整审查仍收尾，源码保持冻结。汇总及原始执行路径见`/tmp/validation-root-acceptance/acceptance-report.md`，新审查证据`/tmp/complete-delivery-audit.aeAjiF/`。未commit/push、未调用真实provider/模型、未新启正式流程。


### 当前95文件冻结复核：未通过，继续完整合同返工

需求修订中的耗尽欠责已按原Attempt/WorkDecision/role归属统一修订，未新增控制字段；删除分散retry筛法、重复Queued退休和手工progress路径。主代理完成14文件最终差异复核，44组组合oracle、engine全包/CLI和vet退出0，日志保存在`/tmp/validation-root-acceptance/obligation-frozen-*`。

新代理`whole_contract_fresh_audit`以`fork_turns: none`启动，仅获原始完整需求、代码准则和累计交付边界；新全审发现终态最后相同事件清理后无法replay，以及缺少确认的granularity/Batch计划仍派开发。主代理独立复制公开入口probe亲跑，两项均失败（`terminal-plan-before.log`）。当前未通过，完整审查继续，实施代理先只读核统一归属及最简设计。绿测与前序根因修复不替代遗漏需求；尚未重新宣称交付完成。

### 当前持续返工：未冻结，未通过

完整原合同复核后，实施正在接通确认的 Batch/Subtask 计划及共同责任查询、原生恢复输入、稳定事件收据与终态重放、QA 逐项理由和 R11 累计设计。主代理已独立复现、核最简设计并分别验证正常/需求变更/重开/原生恢复等相关边界；中途绿测不构成完整交付结论。完整过程证据、失败与setup纠正分别记录于 `/tmp/validation-root-acceptance/current-verification.md`。

最新尚未解决的问题：多个历史 repair 明确 REDO 后分别完成、实际物化，单值 RepairSource 只记录最后 P2，使之前新完成的 P1 也被错误免去门审。实施者自建公开流程，主代理独立执行确认失败：`/tmp/validation-root-acceptance/mixed-repair-source-before.log`。批准从数据归属修正：以候选已有的完整 SourceTasks 引用集合替换冻结波的单值字段；原 Results 持证据、WorkDecisions 持授权，全部消费者按实际新增来源共同核继承/计数/影响范围，删除单值及旧推断，不新增平行账本。尚未宣称修复完成。

新独立审查者 `independent_contract_audit` 实际以 `fork_turns: none` 创建，仅收到原始需求、完整累积交付边界及所有工程准则，未收到旧问题/探针/通过结论。已独立整理35组业务要求与12项工程核验，尚未读取变化中的实现；冻结后开始完整审查。正确性、需求完整性、简洁性、耦合、可维护/可读性、设计一致、硬编码/窄逻辑/过拟合、过度设计/防御/抽象、测试隔离及不叠补丁均仍是验收条件，未以局部测试通过替代逐项核验。

工作区保留返工，未commit/push、未调用真实模型/provider，未启动新正式流程。

### 107文件冻结后的完整独立审查：未通过

`independent_contract_audit`已完成35业务组和12工程项，报告`/tmp/independent-contract-audit.xPBk54/final-audit.md`。实际以零历史上下文启动，原始需求先独立成图，源码冻结后全审；未给旧finding或通过预期。主代理全文核读并亲跑四条正常公开反例，确认：接受后裁决无法按版本纠正（P1）；已解决problem的SAME_AS重复返修（P2）；未变shared审批禁止继承（P2）；纯P2生产修复对其他已过gate缺影响判定仍自动carry（P2）。另完整回归发现acceptance注册夹具和15个whitebox测试前置/版本预期未完成合同迁移（P2）。

主代理62组独立组合oracle PASS221.946s、每事件完整收据/零写对照PASS28.889s、vet通过；engine+CLI完整回归exit1，facade419.323s/protocol151.171s等通过不能覆盖失败项。各项主代理原始复现与正确/错误fixture修法的反向验证保存在`/tmp/validation-root-acceptance/current-verification.md`，没有把编译、局部绿测或临时overlay当作仓库修复完成。

当前E1正确性/适用需求完整、E3跨接缝设计完整、E4问题解决事实归属、E7无窄逻辑、E9测试期望与执行、E11契约回归均未通过。完整报告（包括全部工程未过项与未验证边界）已交实施者，只读设计统一finding/problem/repair生命周期、shared审批投影与独立gate影响事实，并按真实协议迁移夹具；不准只放宽一个状态条件、无证据carry或恢复测试专用业务旁路。源码维持冻结，待最简方案核准再返工；尚未验收完成。


## 115 文件冻结独立验收：仍未通过，统一返工待实施

冻结时间2026-09-28T13:13:11.338340Z。主代理核对115文件正文/内容身份无差异；完整范围仍为7eb32e8至HEAD及全部工作树/未跟踪修改和相交流程。新的零历史审查者实际fork_turns=none，只读原始需求与准则建图，未接收历史问题列表；完整地图仍在收口，不提前报告全审通过。

主代理已独立验证三条业务根因：

1. 完整逐用例EXECUTED_FAIL被强制要求额外severity finding，否则形成REJECTED_INVALID_RESULT/RUNTIME_ERROR并机械重试。3.5b AC081明确QA逐例FAIL不是severity finding；无finding合法QA失败同时缺少核实→canonical problem→repair输入通道。原公开四对照日志 `/tmp/validation-root-acceptance/execution-fail-current/before.log` FAIL5.988s。
2. 用户明确DISMISS已签repair后，当前义务已取消但波次停ADJUDICATING；暂存结果保审计，决定后迟到结果却被拒为invalid且缺obsolete原文。共同settlement的issued早退混淆签发输入冻结与当前义务关闭。Root三交付顺序原oracle及新四对照均复现；`current-semantic-regressions/late-disposition-before.log` FAIL5.421s。
3. 已完成纯P2修复被需求逐项REDO后，旧最终FULL的Counted被当前Results/repairWork重新解释而由false变true；新候选本身仍false，提前消耗共享轮数。`current-semantic-regressions/before.log`两类最小回归组合FAIL14.058s并保存完整State，`final-wave-facts.json`为摘要。

执行事实：

- 53份加法overlay、72个顶层独立组整体退出1，245.974s；两条真实回归及一个Root旧白盒收据夹具缺新必需GateView。后者单独复制补实际测试工件的归属证明、保所有原断言后PASS6.010s，原整套FAIL不能改记PASS。
- `go test ./internal/engine/... ./cmd/... -count=1`退出1：facade399.798s，仓库原DISMISS及mixed-severity多repair redo测试失败；protocol130.214s、acceptance38.413s、whitebox37.454s、testkit37.635s及其余包通过。
- CLI完整PASS176.955s；相交validate用例PASS60.565s；重复Acceptance/原State完整内容4组PASS24.740s；vet及diff--check退出0。它们不替代上述失败或工程逐项验收。

返工方案先核原文与既有实现，方向已核定：原ValidationFinding具名原始证据变体引用实际QA execution失败，复用同一History/Problem/Repair；不伪造P0–P3、不另建target裁决账本。共同settlement区分签发输入、现存授权义务、已发生计数，删除全函数issued早退及COMPLETE反复改写历史的混同路径。全部正常/重复/并行/held/recovery相交入口须同行核查，原断言不削弱；仍待完整新审查收齐后统一解冻实施。

工程结论当前不能通过：正确性与完整性存在上列明确失败；设计一致性、跨流程义务处理、历史事实维护、四类计数要求尚未满足。简洁性、耦合、可维护/可读、硬编码/窄逻辑/过拟合、过度设计/防御/抽象、替代删除、测试真实性和业务隔离仍须对最终返修内容逐项复核，不由已绿测试代签。真实provider/host、regular installed CLI、guard/cost bridge、Seal/install、DB、split/retained/merge的既定后续状态不变，当前typed失败不能借这些边界豁免。没有commit、push、真实模型/网络调用或新formal run。

## 路线语义收口与全交付独立审查处置（2026-09-25）

机主令确认其他 AI 多轮处理后的状态再继续。核对结论：交付冻结于 119 文件身份，两项未决（facade 旧提示断言、full 缺白盒仍被接纳），完整独立验收未收口；/tmp 证据止于 09-28 23:14，其后无活动。

主代理独立复现两项未决并核实规格出处（sliced-runs master-requirements 需求9、formal-flow:124-127）后派发实施代理：

1. validateRouteDecision 补全约束：full 必含 blackbox+whitebox 内建 QA；merge 不进正常选择（拒绝零写）。实施代理对缺 QA 的 full 夹具采"转 custom、义务集不变"策略（偏离合同字面、保真测试主题；单一 helper routeDecisionForGates，回退路径明确）。
2. facade 旧提示断言迁移到 VALIDATION_DETAIL_DISPOSITION 共同入口（生产无改动；两 remedy 全旅程断言保留）。
3. 独立审查 F2：删除 custom"真子集"规格外限制（规格只要求非空、⊆候选、不含 merge），引擎层与 legacy validate 层（workflow.go 同源规则）贯穿同修；1 元候选表无合法路线的死端消除。

主代理独立复核：自建路线 oracle 双向通过（完备 full 接纳且 SelectedGates 恰为双 QA＋门；缺白盒/含 merge 载荷在 Submit 边界拒绝零写、pending 请求存活可再决定）；engine+cmd 全量 exit 0（facade 318s）；cli 91.6s、validate 421.6s；vet/gofmt/diff-check 通过。

零上下文全交付独立审查（未接收任何旧发现）：15 组工程核验 13 组全过，关键不变量（门输入隔离、处置后迟到结果、AFFECTED 强制重跑、路线删减、retry 一次性、canonical 制品）均未击穿。发现处置：

- F1（P2）SELECT_ROUTE 候选门全集由 DECIDE 载荷自带，引擎无"已发现门"权威。主代理裁定为已定架构边界的残余：pending Ask 的 Options 同样来自客户端 REQUEST_CONTROL（engine.go 落账处），门目录发现按 event.go 契约在协议层外，绑定清单只增加两份客户端载荷间的一致性检查而非独立权威。留机主裁决是否在宿主/provider 接入时以 catalog digest 交叉核验。
- F2 已修（上第 3 项）。F3 记账滞后由本条补齐。F4 promotion 未装（豁免范围、如实投影）、F5 AFFECTED 后无条件最终 FULL（过严非违规）、F6 RedoRoute 清空 SelectedGates 走用户授权通道（与 validateRouteChange"不可移除"意图存在张力，特此明示）——作边界/观察项入账。审查报告 /tmp/independent-full-delivery-review-20260925/report.md。

预存问题入账：qastructure tagged 套件 TestStableHelpAndDocsRejectFutureCommandTokens 因 .dogfood/closeout-20260915-0844 快照内 venv 符号链接失败（09-15 起存在、与本轮无关），快照卫生留待处置。

边界不变：未 push、未真实模型/provider/宿主 canary、Seal/安装切换/DB/split-merge 仍属后续交付。

## 问题收口：F1 门全集钉绑＋qastructure 夹具根因修复（2026-09-25，机主令"先把问题收了，再开始 R4"）

- **F1（P2）收口 `c4fd0da`**：修法＝复用 PendingAsk 既有 typed 绑定模式（Package 逐字段相等/BaseRevision 三方相等/Validation digest 同型先例）——SELECT_ROUTE 请求创建时把候选门全集钉进 Ask（RouteGates，形状规则提取为 validateRouteGateCandidates 单一定义点，请求侧与决定侧共用）；DECIDE 候选表与钉住全集集合相等（sameGateSet 顺序无关），少列/多列任一门＝可区分拒绝零写、Ask 存活。façade PendingAskFace 增全集只读投影（用户所见＝引擎持久事实）。schema 层刻意不校验该字段（已接纳事件重放稳定性）。主代理独立复核：diff 亲读＋自建路线 oracle 三探针（完备 full 接纳；缺陷载荷零写拒；钉绑探针：façade 投影可见全集、少列拒绝零写 Ask 存活、修正决定接纳）全过；oracle 过程自纠一处（计划确认摘要绑载荷字节，打乱顺序的候选表先撞摘要不匹配——引擎行为正确）。
- **qastructure 预存失败根因修复（同提交）**：生产 Package() 在有 .git 根上由 Git 动态解析本地数据排除（.dogfood 已正确排除）；测试夹具 faultCopyPackage 复制进无 .git 临时目录时手工硬编码排除表（已漂移两次：$tmp、.harness，这次漏 .dogfood）。根因修法＝夹具改在源仓库上按生产口径枚举包输入（git ls-files --cached --others --exclude-standard＋生产无条件排除的 .git/.gates/.harness——实施代理核实 .gates 下有 109 个 tracked 台账文件须排除，主代理独立确认），排除表漂移类问题永久消除。tagged 套件 425s 全绿。
- 验证：engine+cli+validate 全量（前节）＋三处定向负控＋vet/gofmt/diff-check 全过。

下一单元：R4 guard 接入与三处权威切换（合同 /tmp/r4-contract.md，原文出处已核：incremental-seal-plan 阶段 4 节、draft §7:372-392、REQ-013 AC-059、REQ-015 AC-068/069、cost-metering 设计）。

## R4：guard 接入与三处权威切换（2026-09-25，实施→零上下文审查→返修收口）

合同 /tmp/r4-contract.md（原文出处：incremental-seal-plan 阶段 4 节、draft §7:372-392、REQ-013 AC-055/058/059、REQ-015 AC-068/069、cost-metering design.md）。零上下文开发代理实施，设计文档 /tmp/r4-implementation-design.md。

交付：guard 运行时集成（guardBasis 构造单点从权威 State——closed-world definition/确认 requirement/Batch/route/topology/批准 QA/所选门/established obligations+repairs（adjudication 完成＋wave 收齐准入）/预授权 repair 机械加入；签发闸在 issueInto；GUARD_ALLOWANCE_BLOCKED 与 BLOCKED_BUG 三形；GuardBudget 投影提交点单一 writer＋加载边界重算校验）；cost source 三值桥（protocol typed 引用＋Config 注入，facade 桥做 lifecycle-sidecar 查找、解析归 internal/cost；PENDING 滞留复用 StagedResults＋COST_PENDING acceptance，来源落定同配对路径补齐；UNAVAILABLE 零数字条目照常完成；nil bridge 行为不变）；三处权威切换（GuardRuntimeIntegration 常量权威化、EngineAllowance 读 Guard 投影且 OwnerCost/TokenCap 独立判定、engine run 对 legacy writer 不可达负控）；REPAIR Ask 摘要按 draft:372-375 补全（专用 repairAskFace）。State 增 Guard/Cost 投影，façade Run/TerminalSummary 透出。

零上下文独立审查（/tmp/r4-independent-review-20260925/report.md）判不通过：1 P1＋2 P2＋7 P3，三项主发现主代理亲跑探针复现坐实。返修（同实施代理，根因收敛）：
- F1/P1 闸位移到 issueInto 一切写账之前（原在 TransitionTask 写 s.Tasks 之后，拒绝留幽灵 ISSUED、epilogue 吞错落盘、自环迁移致永久死锁）；补公开漏斗负控（IssueFromPlan 拒绝→重开 store 字节不变）。
- F2/P2 UsedRetries=Σ max(0, 执行数−1)（AC-055 同口径；原 ΣAttempts 把 request 槽计入，耗尽态显示 2/1 超额、欠账态显示满额而闸放行）。
- F3/P3 bridge 三值映射补完整（VerifyDispatch Outcome=Unavailable（LifecycleRequired=false 的 lenient provider）→UNAVAILABLE；原一律 PENDING 致 COST_PENDING 永久滞留）。
- P3 四修：预授权未签发 repair 槽计入额度（显示余量）；AllowanceSummary 单一格式化者；automaticValidationWaveLimit 常量单点；BatchSource 核 scope∈确认计划批次任务集。
- P3 三记账不修：删除式篡改绕过 verifyGuardBudget（对抗性编辑类，外层 store 摘要已挡）；COST_PENDING 无显式 Wait 信号（计划原文即「返回 Wait」，已满足）；终止/隔离路径漏记计量（语义待定，随 owner 源接线一并定）。

主代理复核：评审三探针修后全绿零 FINDING；自建探针（cost 两态＋guard 篡改 BLOCKED_BUG＋路线全集钉绑×4）全过；全量 engine+cli+validate+cmd 17 包 exit 0；vet/gofmt/diff-check 过。实施代理如实披露：owner 终态差值未接线测量（engine intake 无 owner transcript 绑定字段，合同未定义该 wire，投影位就位、UNAVAILABLE 独立判定）——留机主裁定是否定义 owner 源字段（建议随 R5 宿主接线定）。

R4 收口。下一单元：R5（Git provider＋whitebox workspace＋promotion＋cleanup＋真实 canary＋route matrix）。

## R4 判不返回修（2026-09-25 机主审查：1 P1＋1 P1＋2 P2＋需求未完成项）

机主接手审查（含零上下文独立子代理）判 ccd1b15 不通过，四项均机主亲复现、主代理随后亲跑坐实：
- F1/P1 ResumeAgent 直通 persistHostAction，不经 issueInto 闸不耗 attempt——同一 QA 任务连续四次恢复均接纳、UsedRetries 恒 0（3.5b:114 五入口共用 guard/Attempt 原文）。
- F2/P1 成本链路未接通：engine 正常路径从不建立 bridge 所读的 lifecycle sidecar（BeginRun/BindDispatch 无 engine 调用点），宿主 start 事件 Capture roots=[]、结果未等 stop 即 ACCEPTED 记 UNAVAILABLE——COST_PENDING 真实不可达；现有 cost_source_test 手写 sidecar 绕过了缺失的生产边界（夹具不诚实）。正控（显式 BindDispatchWithProvider）证明桥本身可用。
- F3/P2 预计额度按已签发事实计算，漏已确认未签发批次（三批只计两批，完成前驱后额度静默 2→3）；**根因在原 R4 实施合同（§3.A5 把额度事实源定为已签发现存事实，与 3.5b §C「额度来自已确认 canonical expected external TaskKey 集，Ready 只决定本次可签发子集」相反）**。
- F4/P2 路线校验只拒字面 "merge"，实际合并目标 merge-gate/merge-qa（catalog.go:28-31 命名权威）均可入选。
- 需求未完成项：owner 启动基线与终态差值、token cap 配置读取与拦截均未接线，固定 UNAVAILABLE 不能证明能力完成（3.5b:35/:207-208、REQ-012）。

机主代码要求判词：正确性/需求完整性/设计一致性/耦合与事实归属/窄逻辑/测试忠实性/「不粗暴摞补丁」不通过——普通签发加了 guard 而恢复入口分离、额度以 repair 特例补槽而非完整授权全集、额度偏差在实施合同里就已出现。

主代理验收教训（本条核心）：**R4 收口结论被推翻的根因是主代理复核探针为注入式机制验证（自造 resolver/手写绑定），未打真实接链条——机制单元绿≠链路通**。此前"facade 测试因无 sidecar 而走 UNAVAILABLE 分支"被当作测试环境差异合理化，恰恰掩盖了生产写入入口缺失。此后凡"接线/集成"类交付，验收探针必须走公开链（Start→Drive→receipt→真实 sidecar 写入→事件），注入式夹具只作单元补充。

返修合同 /tmp/r4b-rework-contract.md（A 完整授权全集模型重建／B 统一执行入口＋唯一 DISPATCH_BUDGET_DISPOSITION／C claim 回调接通 lifecycle sidecar／D 保留字全集／E owner 基线终态差值／F token cap 配置与拦截／G replacementFold 收敛），已派实施代理。报告 /tmp/current-review-report.md、/tmp/current-independent-review.md。

## R4b 返修交付与零上下文复审（2026-09-25）

R4b 按 /tmp/r4b-rework-contract.md 七面落地（完整授权全集五桶单一枚举／统一执行入口＋唯一 DISPATCH_BUDGET_DISPOSITION／claim 回调接通 lifecycle sidecar／保留字全集三值／owner 基线终态差值／token cap 配置与漏斗拦截／replacementFold 收敛），guard_runtime_test.go 六项公开链测试入树。机主四探针亲验：三绿＋ReservedRoute 正确翻转（admission 拒绝零写入，探针自身无绿路径）。主代理旧探针全绿，全量 17 包 exit 0。

零上下文复审（/tmp/r4b-independent-review/report.md）：**有条件通过**——范围内无 P0/P1，四修复面与两需求项真实落地；3 P2（F1 恢复 resume/换新不经 token cap；F2 UNKNOWN 回执 claim 以 Correlation 猜测身份且 attach 不回写→永久 PENDING；F3 owner 重叠只扫 engine 命名空间漏 legacy ACTIVE）＋6 P3（F4 extendBudgetRetry 无一次性；F5 桶 3 裁决过滤不一致假阳性 BLOCKED_BUG 待核；F6 桶 4 枚举镜像重复；F7 恢复边界静默转 terminate；F8 无终态 EndRun；F9 advanceRecoveries 错误 wedge 待核）。三探针（/tmp/r4b-probe-repo）主代理亲跑全部失败于所证断言＝复现坐实。

## R4c 返修（三 P2＋同面收口＋边界补刀，2026-09-25）

按机主标准 P2 不留账过夜，三 P2 立即开刀（合同 /tmp/r4c-repair-contract.md）。主代理派发前事实核验另坐实一项**新发现（同族根因）**：结果提交的同事务 refill 撞 cap 时整个 Submit 以 GUARD_ALLOWANCE_BLOCKED 拒绝——观察事实丢弃（cost=0、结果未落账、无 Ask 落地、重投必再拒＝永久 wedge；TokenCap 运行中不可变无逃生口），探针 TestProbeResultSurvivesCapRefillBlock 复现后并入合同（补充合同当日送达实施代理）。另一时序疑点排除：复审探针 1 中 qa.design 系 cap 越界前已签发的在途派发（要求提交事务即签发），漏斗无时序缺陷。

实施（同 R4b 代理，报告要点）：
- **F1＋F7＋wedge**：新增单一判定 guardEffectBudget（retry 余量＋token cap 两维），公开 ResumeAgent、恢复 resume、换新 install 三执行创建点共用；被阻＝幂等落 Ask＋不创建效果（事务收尾内返回 nil，不嵌套 commit）；显式 fresh 决定保持 terminate（用户路线非预算边界）；resume 类边界不再静默转 terminate。refill 内 GUARD_ALLOWANCE_BLOCKED 降级为零签发（与修订屏障扣发同形，观察事实必须接纳；BLOCKED_BUG 仍否决提交）。continue 一次性消耗推导扩为三凭据族（治理签发代数/resume intent 建立代数/EXECUTED 回执接纳代数），全从既有持久账本派生、仍收 budgetContinueGranted 单点；含一处 off-by-one 修正（`<=`→`<`：授权同事务的签发也消耗，原边界可一次 continue 解锁两批）。旧 guardResumeBudget 删净。
- **F2**：UNKNOWN 回执不在回执接纳时 claim；ReconcileUnknownReceipt 唯一匹配 attach 同事务内以解析身份调同一 ClaimDispatch 回调（legacy resolveClaimIdentity「以观察到的身份解析、拒绝绑错」先例）；零/多匹配不 claim＝诚实 UNAVAILABLE；SPAWNED 维持回执时 claim。
- **F3**：ownerOverlap 补扫 legacy 平坦形状（status/ownerTranscript/canonical 相等、读失败＝重叠），逐字镜像 validate/cost.go beginOwnerCost；不跨代互写。已知边界：legacy run 后启动时 legacy 侧看不见 engine run（冻结侧盲区）。
- **F4**：retryGrantUsed 一般化为 validationGrantUsed(kind, choice, target) 单一定义点，预算 retry 与 RETRY 两通道各自一次性（跨通道先后为用户显式授权非预累积，注释写明）。
- **F5（证实可达→修）**：构造性证明 wave 带已签返修→路线重绑 INVALIDATED→carry 未裁决 finding 下 deriveGuardBudget 假阳性 BLOCKED_BUG（修复前树上实测复现）；修法＝「已成立返修」判定收敛 establishedRepairWaves 单一归属（Repair 经裁决定案即持久授权事实，回归不解散），回归测试入树。
- **F8**：lifecycle.EndRun（幂等移除 active 标记）＋facade 终态收口调用；终态后晚到事件归属无 run。
- **F6/F9（排除，未改码）**：桶 4 与 projectValidationTasks 合一需让 guard 依赖 decision view 或改 Ready 语义（大改记账不动刀；镜像漂移 fail-closed）；F9 wedge 需恢复记录 current 且任务出 basis 同时成立，公开链上治理签发必经漏斗、重走使旧工作非 current，未找到公开可达序列（手工改写已有单测覆盖）。
- 树内新增 dispatch_budget_boundary_test.go 六项公开链测试（cap 后恢复落 Ask、结果跨 cap 边界存活、continue 一次性负控、attach 后按解析身份精确计量、legacy 重叠 unavailable、终态 EndRun）；两项既有测试按 F7 边界模型重塑（保留各自命名不变量，非打表）。

主代理独立复核（全部亲跑）：自建六探针（/tmp/r4c-verify，overlay 真仓库）全绿——wedge（cost=21 落账＋零签发＋Ask）、公开 ResumeAgent cap（拒＋Ask＋零 intent；continue 解锁恰一）、continue 一次性（两个被阻恢复一次授权只放行一个）、budget retry 一次性（二次授权拒且额度不变）、EndRun（标记移除＋晚到事件无归属）、attach 后计量（ACCEPTED 精确 5/1/2）；复审三探针二绿＋F3 载荷 unavailable 零数字（断言写反无绿路径，同 ReservedRoute 型）；机主六探针用例五绿＋ReservedRoute admission 拒绝与 R4b 基线逐字一致；guard_runtime 六项、dispatch_budget_boundary 六项、F5 回归全绿；全量 internal+cmd 23 包 exit 0。关键面 diff 亲读（统一判定、三凭据消耗、refill 降级、attach claim、双命名空间、validationGrantUsed、establishedRepairWaves、EndRun）无分离路径回潮。

R4c 收口。下一单元：R5（Git provider status/diff/track/commit/snapshot/squash＋whitebox workspace＋promotion＋cleanup＋真实 canary FAIL→repair→重验→Seal［须机主逐次授权真实模型调用］＋route matrix）。

## R4 判二（2026-09-30 机主复核 e356598：2 P1＋2 P2，跨状态/跨入口规则未闭合）

机主复核（含零上下文独立子代理）判 e356598 不通过，四项均机主亲跑复现、主代理 overlay 五探针亲跑坐实（/tmp/r4d-repro-overlay.json 四红组）：
- F1/P1 恢复记账与准入用两套任务查找：guardEffectBudget/agentHostIntent 能在 EndedAttempts（AwaitingResult）找到目标任务，accountResumeExecution 却先查 PendingActions——TERMINAL 清理后 EXECUTED 回执不计数，Attempts 恒 0，第二次恢复放行（有限止损在受支持正常状态失效）。
- F2/P1 同任务一次追加拆两通道各一次：预算 retry 与 RETRY/approve 各自按 kind 判重，先后行使 Max 2→4；R4c 合同把"两通道各自一次性"写成合理设计（错误），被机主以 3.5b:169-173 原文否定——约束对象是逻辑任务不是 Ask 名称。
- F3/P2 owner 重叠只标后启动者：first 启动布尔已定，second 启动只标自己，first 终态报精确 15/8（legacy beginOwnerCost 的双边持久标记语义在移植中丢失）。
- F4/P2 不可靠用量下 cap 宣称 AVAILABLE：deriveGuardBudget 只看配置在否，unavailable 条目零占位参与 dispatchTokenTotal（seal-plan:280/3.5b:112-114「来源不可用 token cap 不可用但派发次数上限仍生效」未贯穿）。
机主判词：增加共同函数不等于统一业务规则——统一的是"查哪份事实"，不是"都有个函数"；恢复准入/记账不同查找规则、两种重试入口各自维护一次性限制是关键证据。

返修合同 /tmp/r4d-repair-contract.md，同实施代理落地：
- **F1**：State.workerActionTarget 单一任务身份解析（PendingActions→EndedAttempts by ActionID+AwaitingResult），guardEffectBudget/agentHostIntent/accountResumeExecution 三消费者共用，原双段内联扫描与记账短路径删净；记账按条目 ID 定位写进承载账本。
- **F2**：State.retryExtensionGranted 统一谓词（RETRY/approve ∨ 预算/retry 任一通道已授即拒），extendBudgetRetry、RETRY approve 扩展分支、RETRY offer 选项裁剪三入口收敛，validationGrantUsed 降为底层扫描；attempt.go 注释按需求原文改写。
- **F3**：Engine.MarkOwnerOverlapped 幂等入口（engine 自有事务＋CAS、无绑定无操作）＋facade markOverlappingEnginePeers（engineOverlapPeers/legacyOwnerOverlap 拆分组合）；写失败上抛并清理自身 stateDir（镜像 legacy beginOwnerCost 失败即中止启动；CAS 重试被否——掩盖争用且不保证成功）。禁直改 peer state.json（破坏 contentDigest）。
- **F4**：State.tokenCapStatus 单点（配置缺失∨任一条目 unavailable＝UNAVAILABLE，否则按已知总量 AVAILABLE/EXCEEDED）；tokenCapExceeded 保持已知总量下界闸语义（已知达 cap 即超；未达不闸）；两函数分工各单点陈述，allowanceFaces 第三处重述改为消费 tokenCapStatus。

主代理独立复核全绿：自建四探针（/tmp/r4d-verify overlay，红态先行——ended 计数 0→1＋拒绝后账本不变、跨通道两方向〔RETRY 先授→预算 retry 决定被拒〕、双边重叠〔peer 补写持久化＋first 先终态也 unavailable〕、cap 可信度两态〔已知<上限不闸＋混合可靠性不宣称 AVAILABLE〕）＋复审五探针全绿＋树内五组跨状态/跨入口测试亲跑绿＋R4c 六探针无回归＋全量 23 包 rc=0。关键 diff 亲读：workerActionTarget/retryExtensionGranted 调用点清单与报告一致、无第三判重口。实施代理披露：TestTokenCapConfigurationStates 一个子测试的夹具由 DefaultProvider（不可计量，旧实现宣称 EXCEEDED）改 meteredProvider 零用量 transcript（已知总量真实达 cap）——断言不变、夹具诚实化。

R4d 收口。下一单元：R5（Git provider＋whitebox workspace＋promotion＋cleanup＋真实 canary［须机主逐次授权真实模型调用］＋route matrix）。

## R4 判三（2026-09-30 机主复核 b856a8b：两项 P2，owner 区间协调根因）

机主复核（含零上下文子代理）判 b856a8b 两项 P2，均亲跑复现、主代理坐实：
- 回归 P2：共享 owner 的并行启动失败——Start 先落 state（对 peer 可见）再开 peer 标记、最后才写 request.json；peer 处 bootstrap 时 facade.Open 读不到 request.json 报 INVALID_INTAKE_CONFIRMATION，己方 Start 失败（探针 8 对 7 败）。
- 累计 P2：legacy↔engine 重叠区间仍报精确——engine 对 legacy 只启动时单向读；legacy beginOwnerCost 只扫 .gates/tmp；两侧终态均不重扫。legacy 先启动：legacy 错报 60/4；engine 先启动：双边重复归属同一笔 60/4。
机主约束原文：「修复应明确区间协调归属，覆盖初始化、活动和终态生命周期；不宜在 Open 失败处简单忽略或补重试」「不得为修此问题直接跨 writer 写另一 runtime 的控制文件；应复用共同协调能力并由各自合法 writer 接纳事实」。判词：owner 的局部互标机制没有实现完整区间不变量；R4d 记账的「legacy 冻结侧盲区不属本刀」被推翻。

返修合同 /tmp/r4e-repair-contract.md（D1 身份就绪先于 state 可见／D2 区间操作统一进 lockOwnerCost 共同协调锁／D3 engine 启动＋终态双观察点／D4 TerminalSummary 增区间载荷／D5 legacy 由其合法 writer 只读接纳），同实施代理落地。两处如实偏差（核可）：
- coordination.AcquirePathWaiting（阻塞模式，同路径同一内核锁对象）——legacy 锁是 try-lock，同锁串行必须等待式；归 coordination 内核所有，legacy 快速失败语义不变。
- legacy 终态 state 是不可见瞬态（completeTerminalRun→DeleteRun 删 run 目录），留存工件＝.gates/results/<run-id>.json——validate.RunSummary 增 OwnerTranscript＋StartedAt（D4 的 legacy 对称半边），engine 终态重扫经留存摘要判定区间相交。

主代理独立复核全绿：自建四探针（红态先行——并行 6 对全成＋双方终态 UNAVAILABLE；精确性负控〔留存终态 peer 不阻断，基线排除前段用量，差值恰 5/2〕保持绿；两向 vanish 交错〔legacy 先消失经 StartedAt 规则、engine 先消失经摘要相交〕＋双向跨 writer 字节守卫）＋机主三探针（并行 8/8、mixed 两顺序双方 unavailable、cross-namespace）＋R4c 六探针＋R4d 四探针（一处自建探针 map 迭代选 action 的 flake 为探针缺陷，按任务身份定位后 6/6 稳定）＋树内跨入口五组＋全量 23 包 rc=0＋vet/gofmt 净。关键 diff 亲读：request.json 先行落盘、锁段归属、终态重扫（检出先经引擎事务持久再量取）、摘要载荷、legacy 双扫只读。

边界如实：留存摘要被手工删除则消失 peer 不可检出（留存边界）；peer 处非 owner 事务时 store 锁快速失败＝可重试错误；不可解析端点按相交处理（只假 UNAVAILABLE 不假精确）。R4e 收口；机主已令开下一阶段（R5）。

## R5a 交付：Git provider 与快照/promotion/登记接线＋route matrix（2026-09-30）

阶段 4 剩余公开面第一刀（合同 /tmp/r5a-contract.md），同实施代理落地：

- **internal/engine/vcs 包**（值级移植 legacy internal/validate/vcs.go）：Provider 全集 Status/VerifyReady/Resolve/Verify/IsAncestorOrEqual/CommitCount/SquashToBase/TrackAll/Commit/DiffNames；Runner 注入可替换，protocol 层零 exec。runtimeGitStatusDirs 排除清单语义＝「由 CLI 自身创建/维护，不是交付文件」：engine 自建路径恰三条（.gates/engine/、.gates/tmp/、.gates/run-locks/），legacy 既有 qa-isolation/slices/results 平移，token-cap.json 定性排除（engine 只读不建，是否入库归宿主 .gitignore 决定）。
- **facade.Start regular 分支 BaseSnapshot 事实校验**（镜像 legacy workflow.go:195-215）：Resolve→Verify（基础身份仍在）→IsAncestorOrEqual（工作区未落后于基线）；VCSRunner 经 StartOptions 注入。
- **终态快照边界 terminalSnapshotGuard**：COMPLETE+regular 多 commit 拒绝（区间提交由候选晋升入口组织，终态是最后防线）＋摘要落盘后双 resolve 核对原生身份未动；无 provider 明确报错不静默跳过。
- **PrepareCandidate 候选组织**（镜像 legacy PrepareCandidate）：工作区不净拒绝（与 verifySnapshotReady 同语义）；区间单提交＝确认当前 HEAD，多提交需 squash 消息、squash 后重解析绑定；track/commit 是宿主侧动作不自动执行。
- **RegisterQAWorktree 登记**（镜像 legacy）：worktree resolve==BaseSnapshot＋隔离区内需求产物哈希==登记修订（"sha256:"+hex 语言，CRLF→LF 规范化——Windows checkout 不改工件身份）；物理建区归宿主。State 增 QAWorktree/PromotedSnapshot 字段；路线重绑弃黑盒与黑盒用例集审过 FROZEN 两处清空登记（镜像 legacy 两个清空点）。
- **RecordCandidatePromotion/RecordQAWorktree 引擎落账**：晋升前置＝run 活动期（终态拒绝再改写），同值幂等/异值可重组再绑（镜像 legacy CurrentSnapshot 自由移动）；晋升绑定收口 op.candidate 步骤并解除 validation 边界晋升等待（原 "candidate promotion provider is not installed" 占位消息替换为真实等待语义）。
- **route matrix 负测**（internal/cli/engine_writer_exclusivity_test.go）：遍历 workflowLegacyWriteEntry 注册表逐命令负表（31 条目：非零退出＋UNSUPPORTED_ENGINE_ENTRY＋engine state 字节不变）、cleanup 拒绝、交替入口绑定不可改。

主代理自建四真仓探针（/tmp/r5a-verify，overlay 不入树，红态先行）抓到一项真实缺陷：runtimeGitStatusDirs 镜像 legacy 清单时漏 .gates/engine/（engine 自建运行时目录）——真仓无 .gitignore 时 VerifyReady 恒败、PrepareCandidate 不可用。当日小刀返修：排除清单按「engine 自建路径恰三条」语义枚举补 .gates/engine/；同刀根因修 RecordCandidatePromotion 前置（原「最终 wave COMPLETE＋一次性绑定」与 legacy CurrentSnapshot 自由移动语义冲突，改活动期＋可重组再绑），树内新增 TestPrepareCandidateChainPassesWithoutGitignore。

主代理独立复核（全部亲跑）：四真仓探针（Start 校验/PrepareCandidate 含无 .gitignore 链/RegisterQAWorktree 真哈希/终态 guard）全绿；树内 route matrix 全表、vcs 单测、cli 套件、TestLegacyWritersCannotReachEngineRunState＋TestLegacyWriteCommandsRejectEngineRunPerCommand 全绿；全量 internal+cmd rc=0（24 包 ok，vcs 新增）。关键 diff 亲读：排除清单语义、promotion 前置、两处登记清空点、guard 双 resolve。

如实边界三项待机主裁定：
1. 真实宿主 Git 非分片 full canary（FAIL→repair→新候选→重验→Seal）未入本刀——真实模型调用须机主逐次授权。
2. 既有缺口：regular engine run 无法经 Drive 推进到 COMPLETE（validation agent step 完成机制未接，旧占位已换成真实晋升等待语义）——canary 前须补。
3. legacy internal/validate/vcs.go 的 runtimeGitStatusDirs 同样不含 .gates/engine/（混代真仓下 legacy VerifyReady 同族盲区）——实施代理披露，机主裁定是否另开刀（legacy 冻结侧）。

R5a 收口。R5 剩余：真实 canary（授权后）＋RESET/RECOVERY 公开 typed contract 评估（plan :366）。

## R5b 返修：regular run 终态完成机制＋legacy 混代排除（2026-10-01）

机主三项裁定（①真实 canary 授权②修 COMPLETE 缺口③legacy 盲区随刀修）后的第二刀。既有缺口根因定案（主代理逐条亲核）：六个验证期 agent 步（development.worker/qa.design/qa.review/qa.execution/validation.gate/development.repair）没有任何完成机制——单次 PASS 被 engine.go:2036 守卫正确拒绝作为过程完成证据（任务跨 wave 反复签发），但过程级完成事实缺失；六步未完成→frontier→externalPresent→永久 Wait TASKS_IN_FLIGHT。且 Drive 终态相变只在结构上不可达的 WaitEngineInternal 分支内（全步完成后 decide 返回 KindComplete 走 default 直接 return，相变不发生）。R5a 测试里捏造 Run{Status:"COMPLETE"} 面自证了缺口。

返修合同 /tmp/r5b-contract.md（五面＋legacy 一行），原子代理落地：
- **D1** State.validationConcluded 单一谓词（workspace.go，与 promotionPending 同处）：前置门与 advanceValidation 管线门同形；收口＝currentValidationWave().Status==COMPLETE ∧ PromotedSnapshot != ""——只消费 settleValidationWave 与 RecordCandidatePromotion 两个既有权威，零新状态。
- **D2** settle 规则集 AGENT 分支两类：验证期步（validationHandler≠""）⟺管线收口；其余非载体步⟺载体首过闩。规则集权威注释同步改写（删被证伪旧句）。
- **D3** Drive 循环收敛点：advanceValidation 后、提交判定前跑 settleFrontierSteps，步数变化并入提交条件——wave COMPLETE 与晋升绑定两事实无论先后落在哪个事务，下一次 Drive 都收口（不枚举事务接缝）。
- **D4** 终态相变唯一入口：switch 增 KindComplete case（非 TERMINAL→TransitionPhase+commit+continue；已 TERMINAL→只读重入）。
- **D5** 删除被替代的 WaitEngineInternal 特判段（原 engine.go:571-591）；不可达构造性论证：settle 与 decide 的就绪判定共用同一 decisionView.StepSatisfied，decide 时 frontier 不可能含引擎内部未完成步；旧相变条件严格弱于 decide:313 权威判定，语义只增强。
- **D6** legacy internal/validate/vcs.go runtimeGitStatusDirs 增 .gates/engine/（混代真仓防误拦）＋注释混代语义。
- **实施代理一处核可偏差**：迟到结果对账路径（admitWorkerResultRecord）补与主接纳路径同形 validationRole=="" 守卫（原路径会让单次迟到 PASS 完成验证期步，与规则矛盾；同缝贯穿）。
- 树内测试：改造晋升测试走真实 COMPLETE（删捏造面）；新增交错序收敛、双负控（仅晋升不终态／wave 完成未晋升仍等待）、终态重入只读（revision＋状态字节＋摘要字节三断言）；legacy 正负控。

主代理独立复核（全部亲跑）：自建四探针（/tmp/r5b-verify overlay）红绿证明——stash 修复后两终态探针红（ACTIVE/WAIT 卡死形态与诊断一致）＋legacy 红＋负控绿，恢复后全绿；diff 亲读五面与合同一致；CompleteStep 全仓调用点清单验收（PASS-完成步仅两处均已守卫、过程级规则 settle 单点、op.candidate 归晋升事务）；树内新旧测试亲跑全绿；全量 internal+cmd rc=0（24 包）。

R5b 收口（提交见本条 git log）。canary（机主已授权真实模型调用）为下一单元：K5 实跑模式（env 门控）驱动 engine regular run，FAIL 走真实种子缺陷→裁决→返修→新候选→重验→终态。

## R5 判审（2026-10-01 机主复核 2c550a8：2 P1＋1 P2 未过）→ R5d 返修收口

机主复核（含两名零上下文子代理、采用项均亲复现）判 2c550a8 未过，三组均主代理探针亲复现坐实（/tmp/r5d-verify 四探针＋机主 /tmp/r5-owner-recovery_test.go）：
- F1/P1 晋升副作用/授权边界：需求变化屏障期 PrepareCandidate 仍 squash＋登记；终态 run 误调晋升报拒绝但 Git 历史已被改写——副作用先执行、接纳后判断，且屏障不在接纳条件。
- F2/P1 候选身份未贯穿：晋升后脏树/身份漂移（含区间计数 1 的漂移）仍 COMPLETE；guard 拒时底层已 TERMINAL，关闭错误提示要求的重新晋升路径。PromotedSnapshot 非空被当收口事实、与已验证候选无绑定。
- F3/P2 owner 终态落盘与量取端点分离：first 持久化 TERMINAL 无摘要中断→peer 扫描把其当已闭合排除→双方精确同记一笔。engine 侧扫描只认 ACTIVE state.json、engine 终态摘要从不参与相交。
- 另点名：frozenGitRunner 对任意 identity 同值（base==HEAD 报 count=1）违反夹具忠实；注释过程代号（「D4 载荷」「D 系不变量」）。

返修合同 /tmp/r5d-repair-contract.md（规格原文全钉：master-requirements §7.2 Phase A、incremental-seal-plan:205 intent→execute→observe/reconcile→commit、legacy workflow_carry_seal.go:327-380 双身份核对、guardrails :82/:84），原子代理落地：
- **D1 晋升副作用协议**：State 增 PendingPromotion（意图）＋PromotedDigest（内容身份）；admitPromotion 单点接纳（活动/无屏障/无 abort/wave COMPLETE——晋升是解除 promotion-wait 的动作，collect 中拒绝）；OpenPromotion 意图先行（准入失败零 Git 副作用；同 target 幂等/异 target 覆盖）；RecordCandidatePromotion 原子重检＋须消费意图（无第二入口）＋digest==currentWave.Candidate.Digest 内容绑定；需求变化 REQUEST 落账同事务撤销意图；PrepareCandidate 重排 Resolve→Open→vcs 执行→观察（TreeDigest）→Record；execute/commit 屏障竞窗的错误文案写明恢复路径（窗口 legacy 同形不可全消）。
- **D2 候选身份贯穿**：内容身份约定＝原生树身份（vcs.TreeDigest＝rev-parse ^{tree}）；validationConcluded 增 digest 绑定；Engine.SealTerminal 公开终态相变入口（delivered==PromotedSnapshot＋validationConcluded，幂等/屏障/abort）；engine.Drive KindComplete 相变收窄 lightweight；facade sealDelivered 三核验（身份相等/树净/祖先）先于相变，失败 run 保持活动、重组织敞开；投影 COMPLETE 单一权威＝phase TERMINAL。
- **D3 owner 终态端点共用可恢复边界**：engine peer 可排除条件单一化（非终态∨终态但 terminal-summary 缺失/端点不可解析＝区间开放）；新增 engineSummaryOverlap 相交扫描（镜像 legacySummaryOverlap）；validate 侧 beginEngineOwnerCost＋engineOwnerOverlap 同形同步（偏离核可：合同只点名 begin，完成侧同洞一并闭）。修复形态：first 终态无摘要→peer 判开放→双方 unavailable，不再双精确。
- **D4** 注释域语言化＋acquirePathLock 收敛（AcquirePath/Waiting 共享 internals）。
- **D5 夹具诚实**：frozenGitRunner 改 frozenGitRepo 链仓（argv 精确、逐提交唯一身份/树、区间计数＝链距、squash 保树、未知身份报错）；cli 排他测试同形；materialize 真仓用真实 HEAD＋树摘要、冻结场景 per-freeze 链上推进（偏离核可：常答下约 20 个多代候选场景因同身份被引擎折叠不可表示，链仓是满足 argv 精确＋身份区分的最简形态）。

语义更新（非打表）：collect 中晋升＝拒＋零副作用（交错序测试重写）；内容漂移重绑＝拒（恢复＝恢复已验证内容）；TestPrepareCandidateChainPassesWithoutGitignore 改走完验证管线再晋升。

主代理独立复核（全部亲跑）：自建八探针红绿——复现时四红（屏障晋升成功/终态拒绝仍改写 HEAD/脏树 COMPLETE/漂移 COMPLETE）＋F3 探针红；修复后全绿（含内容保持型漂移 squash 重组织恢复、reset--hard 恢复路径）；R5b 三探针按新语义更新后绿；关键 diff 亲读（接纳单点/意图消费/内容绑定/Phase A 撤销/SealTerminal/sealDelivered 先于相变/投影单一权威/双侧 owner 扫描同形/链仓夹具 argv 精确）；engineSummaryOverlap 摘要留存注释与 commitTerminalCleanup 事实核对一致；全量 internal+cmd rc=0（24 包）。

R5d 收口（提交见 git log）。canary 驱动器（R5c 交付的未跟踪文件）待与 R5d 语义对齐（REVIEW_READY 物化改直做＋树摘要约定＋intake/QAWorktree 两套修订语言疑点核判）后实跑——真实模型调用机主已授权。

## R5c/R5e：canary 驱动器交付＋修订语言统一（b9d1eb1，实跑待执行）

**R5c**（合同 /tmp/r5c-canary-contract.md）：internal/harness/engine_canary_live_test.go——engine 首个真实宿主执行桥（//go:build !windows＋env 门 FORMAL_GATES_ENGINE_CANARY=1，无 env 零花费 SKIP）。公开链全走 facade API（Start/Drive/Submit/DevelopmentPlan/PrepareCandidate）；八类派发 handler 各自角色 prompt＋最小诚实 schema＋typed 事件组装（映射表见实施报告）；行为命令由宿主桥在真实候选上真实执行（checkout 波候选→go build→逐例跑）、只读判断回合裁决——命令真实、门真实 FAIL、读写纪律不破；限额＝单派发 3 轮/repair 1 次/40 分钟/300 轮驱动上限/未预期边界即败 dump；每轮真实 usage 入日志。FAIL 种子＝frob.go 预置接受未知 flag（需求把开发范围钉在 analytics.go、flag 契约留给 QA）→blackbox 用例＋regression 门真实发现→裁决 NEW＋SAME_AS→真实返修→新候选→受影响重验→晋升（多提交真实 squash）→终态。

**R5e**（合同 /tmp/r5e-align-contract.md）：canary 实施暴露的 R5a 缺陷核判与修——修订语言权威＝裸 hex（legacy RequirementRevision runstate.go:1246、legacy 隔离区校验 workflow_prompt.go:344、engine intake 校验 facade.go:1428-1448 三处同语言），RegisterQAWorktree 的 sha256: 前缀是缺陷（带 ArtifactRoot 的合法 intake 与工作区登记互斥）；改裸 hex 逐字镜像＋注释纠错。canary 驱动器对齐 R5d 语义：REVIEW_READY/各 Stage 物化直做 git＋回执（identity=真实 HEAD、digest=真实树身份 rev-parse ^{tree}；candidateDigest 自造路径哈希整删）、PrepareCandidate 唯一调用点＝promotion-wait Operator fact（多提交带真实 squash message）、collect 路径零调用（grep 自查）。验证：RegisterQAWorktree 用例绿、无 env SKIP、19 包全绿。

主代理复核：diff 亲读（裸 hex 镜像、canary 三触点）；定向测试亲跑。实跑（真实模型调用，机主已授权）＝下一动作：FORMAL_GATES_ENGINE_CANARY=1，证据目录 FORMAL_GATES_ENGINE_CANARY_DIR 留存。

## R5 canary 实跑：真实宿主全链绿（2026-10-01，机主 09-25 授权真实模型调用）

十一跑闭环（runs 1-11，证据 .harness/validation/engine-canary-*，含逐派发 jobs 材料/原生事件/交互应答/凭据链），第九跑起逐缝修复驱动器，第十一跑全链绿：998.58s、11 次真实派发、747,995 tokens。全链＝产品审 PASS（八维核验真实引用）→就绪审 PASS＋开发计划→开发批次（真写 analytics.go＋接线，真跑 go build）→QA 设计（AC-001/AC-002 六用例含非 SUCCESS）→用例审→第一波执行（未知 flag 用例真实 EXECUTED_FAIL）＋回归门（真实 FAIL 带发现）→裁决（门发现 NEW 立问题＋执行失败 SAME_AS 并同根因）→返修（真实可写回合修 flag 处理）→REVIEW_READY 物化提交→修复后候选 AFFECTED 波（六用例全过＋门过）→同候选 FULL 终验波（执行重跑、门 PASS 沿候选携带；AFFECTED 与 FULL 是同一候选上的两笔不同义务，freezeValidationWave 波身份含 scope）→晋升等待 fact→PrepareCandidate 真实 squash→终态 COMPLETE。终态断言全过：晋升快照==HEAD==efff4f01、区间单组织提交、双文件在 diff、终候选上未知 flag 真被拒（重新构建探针）、terminal-summary 重放 Drive 字节不变、无悬挂 ask、成本账覆盖全部 11 派发、返修授权恰一次。

驱动器九缝（全为主代理亲复现后单代理小修，提交 bc89971）：①WAIT/TASKS_IN_FLIGHT＝已签发待宿主执行，路由进派发；②schema 字面量须纯 JSON 化（canonical 编码器闭类型集拒 []string）；③测试二进制居 build cache，可执行文件回溯找不到 harness root→凭据空，须 FORMAL_GATES_HARNESS_ROOT 指真仓；④审查指令须逐名列八核验维（否则诚实模型拒答）；⑤夹具需求须诚实声明基线缺口及授权闭环（否则真实评审员抓自相矛盾诚实 FAIL——引擎派发→typed 证据→审查接纳链在真实条件下工作的强证据）；⑥合并边界消息按实际绑定状态分流（Topology 已绑与否）；⑦挂起写权限交互须 watcher 应答（K5 面，原生 answer 文件一次性消费）；⑧系列身份三值闭集（enums.go:40-53），unaffiliated 非空≠开发前系列派发，按成员路由；⑨执行失败裁决禁带 severity/SeverityRationale/Verification.Severity（执行失败是观察到的事实，severity 属其并入的问题）。

引擎零缺陷：十一跑中引擎每次拒绝都与其规则一致（REJECTED_INVALID_RESULT 两起均为驱动器事件组装错形）。真实花费总账：30 回合、1,605,342 tokens（runs 1-3 零回合环境/形状败、run 8 挂起零回合、其余为接缝定位与终验）。树内 harness 套件绿（352s）。

R5 剩余：RESET/RECOVERY 公开 typed contract 评估（plan :366）。

## R5 收尾：RESET/RECOVERY 公开 typed contract 评估销案（2026-10-02，机主裁定采纳）

机主裁定「可以」，采纳建议＝**引擎不提供 reset**；RECOVERY 一并销案。评估依据＝legacy ResetRun 全文（workflow.go:887-960）逐用途机制对照：

- reset 三用途逐一被引擎原生机制吸收：①文档变了重登记→run 内需求变化流程（REQUEST_REQUIREMENT_CHANGE：停写屏障＋复审新鲜度门槛，R5d）；②验证状态烂了全重走→引擎事实全事务化、验证按 wave 收敛（FAIL→返修→AFFECTED→FULL 只重验受影响面，canary 实证），legacy reset 的「清卡住派发」土壤不存在；③开发成果保留、其余重做→abort＋新 run 覆盖不了此件，但该组合在引擎语义下不成立（开发完好不需整体重验；需求变了 legacy reset 自己也作废开发）。
- 审计差异记入考量：legacy reset 同 run ID 原地抹史；abort＋新 run 是两段完整留痕区间（abort 终态摘要＋owner 区间闭合，R4e/R5d 形状）——对关口核原始证据的项目是优点。
- 现状已安全且响亮：engine 公开面对 reset/recovery 控制显式拒绝（engine.go:1081、:1152）；route matrix 31 条目已含 workflow reset 对 engine run 拦死（UNSUPPORTED_ENGINE_ENTRY、状态字节不变）；legacy run 自身的 reset 不动（冻结侧）。
- RECOVERY：崩溃恢复＝重开 facade 续 Drive（R5b/R5d 已验，canary 终态重放字节不变），是恢复性质不是命令；ControlRecovery 词汇仅作为 abort choice 的合法载体（abort.go:17）。
- 诚实边界：结论由机制推导得出，未考 dogfood 历史中 reset 的实际使用频率。

R5 全部收口（Git provider、whitebox 登记、晋升协议、终态完成机制、owner 区间、真实 canary 绿链、route matrix、RESET/RECOVERY 销案）。

## R5f 返修：机主复核 5df762d 未过（2 P1＋2 P2＋验收/费用缺口）→ 收口（2026-10-02）

五发现全部主代理亲复现坐实（机主探针 overlay 亲跑红态）：①终态落盘、摘要发布前中断→恢复路径旧 terminalSnapshotGuard 弱判据（ancestor/count/发布期不变）放行 amended HEAD 为 COMPLETE——两套终态判据并存是根因；②屏障接纳后 squash 仍改写历史、Record 才拒——意图未贯穿副作用执行；③soft reset 后 commit 前中断→重入以 base 覆盖未决意图＋VerifyReady 拒——中间态无恢复；④canary 代码固定补写 source/point PASS（run 144748 答卷仅 caseDecisions 6 条核实）；⑤被拒真调用不入账（run 122152 三次 104,091 tokens 亲核）。

返修合同 /tmp/r5f-engine-contract.md＋/tmp/r5f-canary-contract.md（规格原文全钉：final-implementation-draft:299 Phase A、legacy Seal 双 resolve、stage-3-5a QA review 三层条款、review.go:170-186 full 路线语义、legacy canary 白盒机械），两代理并行落地：
- **终态判据统一**：sealDelivered 对 regular 未 abort 的 run 在 KindComplete 一律执行（SealTerminal 已 TERMINAL 幂等）——首达＝核验＋相变、中断重入＝同一核验先于摘要发布（漂移即错不发布）；terminalSnapshotGuard 收敛为纯发布窗口身份恒等，删分叉前置判据，交付核验单点＝sealDelivered。
- **晋升 intent 贯穿**：内容前置门（StagedTreeDigest＝git write-tree 的净树须等于终 wave 候选 digest，先于任何 Git 写——干净区即 HEAD 树，中间态即候选树，单一规则覆盖两形态；漂移零副作用拒）；意图存活重入＝续行不重开（消除覆盖）；squash/续行 commit 后接纳被撤销（屏障/活动期）＝SoftReset 回意图目标补偿（组织保树→工作区干净，净零历史改写；digest/schema 拒不补偿）；reset 完成 commit 未执行中间态以前置门识别后 Commit 完成、绑定原意图。新增 vcs 操作 StagedTreeDigest/SoftReset（Runner 直通镜像既有形态）。
- **canary**：三层审查逐层取模型判定（sourceDecisions/pointDecisions/caseDecisions 全覆盖形状检查＋逐字组装，删除固定补写）；路线改 full（读 Ask 钉住 RouteGates，Mode full/Selected 空）；白盒面（worktree add --detach at BaseSnapshot→RegisterQAWorktree→白盒 design 可写回合真实写 Go 测试、TestRef-only 形态、QA_ARTIFACT 回执前 assertTestOnlyArtifact 对真实 diff 验证 test-only 主张、白盒执行在登记区跑 go test）；逐调用费用入账（zcodeExecute 返回即记，含被拒）；gofmt。
- 树内新增四行为测试（检查点漂移拒发布/屏障净零/中断续行绑定组织头/漂移前置拒）；既有测试机械适配逐处披露（guard 多 commit 断言改由 sealDelivered 检查点拒绝、completePromotionPipeline 增 runner 注入位等）。

主代理独立复核（全部亲跑）：diff 亲读（补偿分支/前置门单规则/续行路径/白盒桥三引擎原文核据：validation_result.go:341 白盒 count==2＋Development、contract.go:655-668 TestRef-only、validation_candidate.go AUTHORING/QA_ARTIFACT）；机主探针红绿——修复后 4/5 绿，第 5 个（TestRootPromotionResetCommitRecovery）日志示 retry error=nil＝修复生效，其末断言写于坏世界（成功消费意图后 pending=""），判陈旧断言非缺陷，树内 TestPromotionResumeAfterResetInterruptionBindsOrganizedHead 持正确语义；夹具自洽核（commit 步清 pendingTree）；全量 internal+cmd rc=0（24 包）＋vet/gofmt 干净。

**费用更正**：真实总账修正为 33 次调用、1,709,433 tokens（原报 30/1,605,342 漏 run 122152 三次被拒调用 104,091——机主复核发现，主代理对全部证据目录 source.json 重核一致；驱动器已改逐调用入账）。full canary 重跑（full 路线＋白盒面＋三层审查）待机主授权真实模型调用。

## full canary 实跑绿链（2026-10-02，机主授权「用 zcode 重跑」）＋R5f 后两驱动器缝

R5f 修复后 full 路线重跑三回合（runs 12-14，证据 .harness/validation/engine-canary-2026100[2]T*）：run 12 挂于白盒设计回合真实跑 `go build` 留下的 `frob` 二进制被 `git add --all` 卷入 QA 工件区间——**assertTestOnlyArtifact 诚实回执拦截**（引擎每阶段参数本就携带精确声明 Paths，validation_candidate.go:188-234 亲核）；修＝物化只提交声明路径＋clean 构建残留。run 13 挂于模型把 authored 文件报成绝对路径（darwin 符号链接前缀 /var↔/private/var 加剧）与引擎相对名词表不符；修＝canaryWorkspacePath 归一（双前缀 EvalSymlinks＋工作区内校验＋文件存在校验——模型必须报其所写）。

**run 14 绿链（PASS 1901.26s，16 派发 17 次真实调用含 1 次被拒全入账，1,475,763 tokens）**：产品审→就绪审→开发批次→黑盒设计→**白盒设计（可写回合，11 次模型请求真实撰写 Go 验收测试）**→黑盒用例审（三层答案两次 shape 重试后过、被拒调用入账）→白盒用例审（三层判定真实：source 1＋point 2＋case 8 各带真实理由，答卷亲核）→第一波黑盒/白盒双执行真实 FAIL（白盒＝登记工作区真跑 go test）→回归门真实 FAIL→裁决→真实返修（317k tokens）→修复后候选 AFFECTED 双执行＋门过→同候选 FULL 终验波双执行→squash 晋升→终态 COMPLETE（promoted=1df559d7，全部终态断言过：晋升==HEAD、单组织提交、终候选 flag 真被拒、摘要重放字节不变、成本账覆盖全派发、返修授权恰一次）。**计划 :367 的 full canary 验收（真实宿主＋FAIL→repair→新候选→重验→Seal＋白盒＋三层审查）至此以完整形态满足。**

本授权环真实花费：三回合合计 31 次调用、2,747,965 tokens（run12 684,067＋run13 588,135＋run14 1,475,763；全部逐调用入账，无漏账——费用修复经实跑验证）。引擎零缺陷：run12/13 的拦截均来自驱动器自己的诚实回执检查（test-only 区间、路径词表），引擎链每次接纳/拒绝与规则一致。canary 累计（runs 1-14）真实总花费 64 次调用、4,457,398 tokens。
