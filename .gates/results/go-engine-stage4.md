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
