# Design

## A. scope 决策状态（RQ-003）

### A1. 状态结构

`runstate.go` 新增（按 mode 一条、最新决策覆盖；`map[string]QAExecutionScope` 由 mode 键索引，
空 mode 键对应合并 / 单派发流程）：

```go
type QAExecutionScope struct {
    Decision     string   `json:"decision"`     // "FULL" | "AFFECTED"
    Mode         string   `json:"mode"`         // blackbox | whitebox | ""
    BaseSnapshot string   `json:"baseSnapshot"` // 继承来源=上一轮权威结果快照
    CaseIDs      []string `json:"caseIds"`      // AFFECTED 子集
    Reason       string   `json:"reason,omitempty"`
    Origin       string   `json:"origin"`       // 固定 "USER"
    Source       string   `json:"source"`       // PREPARE | AUTHORIZE_REPAIR | CARRY_FORWARD
}
// RunState 新增：ExecutionScopes map[string]QAExecutionScope `json:"executionScopes"`
// RunState 新增：PriorQAExecution *QAExecutionResult `json:"priorQAExecution,omitempty"`
//   —— 修复快照推进时，若当前 QA 结果为权威（PASS/FAIL）而将被重置，先保留到该字段（含快照与
//      FAIL 用例集），供重跑识别与 AFFECTED 子集判定；RUNTIME_ERROR 不保留；新一轮
//      RecordQAExecution 记录权威结果时取代。
```

新增 `NewRunState` 初始化 `ExecutionScopes: map[string]QAExecutionScope{}`；运行摘要可按需暴露
最近一次 scope 决策（供 host/用户可见）。需求作废重置（`invalidateRequirementResults`，
workflow.go:2560）时 SHALL 一并清空 `ExecutionScopes` 与 `PriorQAExecution`（P2-丙确认：两者同
属"上一轮/历史执行上下文"，随重置对称清除）。

### A2. 记录命令

`cli.go` 新增子命令 `workflow qa-execution-scope`，`workflow.go` 新增
`RecordExecutionScope(root, packageRoot, runID, mode, decision string, caseIDs []string, reason string)`：

- `decision` ∈ {FULL, AFFECTED}；`mode` ∈ {blackbox, whitebox, ""}。
- `AFFECTED` 必须有 `caseIDs` 且非空；`FULL` 忽略 `caseIDs`。
- 校验 `BaseSnapshot`：统一为"这次重跑要继承的权威结果快照"（P1-乙确认）——prepare 路径取 B1
  谓词的 `baseSnapshot`（更早快照的上一轮结果）；`authorize-repair` 内联路径允许 `BaseSnapshot
  == 当前快照`（继承来源即当前 FAIL 结果，修复快照推进后由 `PriorQAExecution` 保留、变成"上一
  轮"）。`AFFECTED` 要求存在该权威结果（prepare 路径为更早快照结果，authorize-repair 路径为当
  前 FAIL；两者都算，不存在时只能 FULL）。
- 校验 `caseIDs` 均为该 mode 已批准用例（`ReviewStatus == PASS`），且包含该 mode 上一轮（继承
  来源快照处）全部 FAIL 用例。
- 覆盖写 `state.ExecutionScopes[mode]`，`Origin=USER`；`Source`：普通记录=PREPARE、
  authorize-repair 内联=**AUTHORIZE_REPAIR**（P2-丁确认，消除死枚举）。

## B. prepare 强制点（RQ-002/004）

### B1. 重跑检测谓词与上一轮结果保留

新增 `qaExecutionPriorResultedBase(state, mode)`：返回这次重跑要继承的"上一轮权威执行结果"的快
照。上一轮权威结果按序取：当前 `QAExecution`（authoritative 且 `Snapshot != CurrentSnapshot`，
如 PASS 结果被保留跨门的修复快照推进）；否则 `PriorQAExecution`（修复快照推进时从被重置的权威
结果保留而来，见 A1）。RUNTIME_ERROR 不构成权威结果。该结果快照记为 `baseSnapshot`；`mode ==
""` 时直接为真，`mode != ""` 时该 mode 在上一轮结果中有用例记录。**BaseSnapshot 语义统一**
（P1-乙确认）：= "这次重跑要继承的权威结果快照"，不要求早于记录时刻的当前快照——上限
（authorize-repair）路径允许等于当前 FAIL 快照（修复快照推进后该结果由 `PriorQAExecution` 保
留、成为"上一轮"，BaseSnapshot 匹配继续成立）。

`resetSnapshotReviewSurface`（workflow.go:1803）在把 `QAExecution` 重置为 PENDING 前，若其为
权威（PASS/FAIL）且快照为旧快照，则先保留到 `PriorQAExecution`（记录快照与 FAIL 用例集）；
RUNTIME_ERROR 直接重置、不保留。`RecordQAExecution` 在记录新一轮权威结果时清空
`PriorQAExecution`（被取代）。需求修订 meaning changed / invalidateRequirementResults 一并清空。

`prepare-action qa-execution`（`PrepareAction` 的 compose 回调）在现有
`qaExecutionModeResulted` 检查之后追加：

```go
if base, ok := qaExecutionPriorResultedBase(*state, mode); ok {
    // base 是上一轮权威结果的快照（QAExecution.Snapshot 为空/已推进时取 PriorQAExecution）。
    sc, ok2 := state.ExecutionScopes[mode]
    if !ok2 || sc.BaseSnapshot != base {
        return fmt.Errorf("QA Execution rerun requires a scope decision: run `workflow qa-execution-scope --mode %s --decision FULL|AFFECTED ...` first", mode)
    }
}
```

`qaExecutionPriorResultedBase(state, mode)` 返回上一轮权威结果的快照（P2-甲确认：修复快照后
`QAExecution.Snapshot` 为空，base 必须取 `PriorQAExecution` 的快照，不得用 `QAExecution.Snapshot`）。

首次执行（无上一轮权威结果）不经过该检查，直接全量。

### B2. 需执行集与子集校验

`qaExecutionRequiredCases` 修改：在既有 mode 过滤前，若存在 `ExecutionScopes[mode]` 且
`Decision == AFFECTED` 且 `BaseSnapshot == 上一轮权威结果快照`，则需执行集 = 该 `CaseIDs` 对应
的用例；否则维持现状（全部已批准，按 mode 过滤）。

`qa-execution-scope` 记录 AFFECTED 时 SHALL 校验子集（机械约束，综合判定由 host 承担）：
`CaseIDs` 是该 mode 已批准用例的非空子集，且包含上一轮该 mode 的**全部 FAIL 用例**（取自上一轮
权威结果的 FAIL 用例集，经 `PriorQAExecution` 保留可得）。host 判定哪些既往通过用例受连带影响、
自行扩展子集，CLI 不要求用户确认子集。

### B3. 执行提示词（跑前定死子集，RQ-010）

`actionPromptDetail`（qa-execution 分支）按需执行集组装列出用例；AFFECTED 时追加一段：列出的
是本次需执行子集（派发前已由 host 综合判定定死），其余已批准用例继承自 `<BaseSnapshot>` 的
PASS、不在本次执行范围内；执行者 SHALL 只执行该子集，SHALL NOT 自行补跑/改判/判定名单外（继
承）用例，SHALL NOT 在执行中临时上报或改选（RQ-10 / P1-甲 确认：无中途上报，故无同快照重派死
端）。名单外用例的漏检风险属 AFFECTED 的接受风险，由推荐时 AI 判断与用户提醒承担（见 E）。

## C. 记录与继承（RQ-005）

### C1. QAResultRecord 来源标记

`runstate.go` 的 `QAResultRecord` 新增 `Origin string`（`"executed"` 默认 / `"inherited"`）。
`RecordQAExecution` 对经执行的用例记 `executed`；AFFECTED 下未覆盖的已批准用例追加记
`inherited`（`Outcome=PASS`、`Observation="inherited PASS from <BaseSnapshot>"`）。记录本轮**权威**
结果（PASS/FAIL）时清空 `PriorQAExecution`（被本轮结果取代）；**RUNTIME_ERROR 记录不清空
`PriorQAExecution`**（P2-乙确认：运行时错误不是权威结果，不得驱逐存续的上一轮结果，下一轮重跑
的 base 仍取自 `PriorQAExecution`）。

### C2. 聚合与 seal 一致性

聚合状态只由 **executed** 用例判定：任一 executed 用例 FAIL → 整体 FAIL（进入返修）；全部
executed PASS → 整体 PASS。继承用例恒 PASS、不参与 FAIL 判定。`requireSelectedResultsResolved`
与 `selectedQAModesRecorded` 以 `QAExecution.Status/Snapshot` 判定 mode 已记录，不受继承用例
影响。

## D. 上限打包与 carry-forward（RQ-006/007）

### D1. authorize-repair 扩展

`AuthorizeExtraRepair`（`workflow.go:2056`）在既有校验（轮次上限已用尽、存在可返修阻塞项）之后
追加：当 `isSelectedQA(state)` 且当前快照存在某 mode 的权威 FAIL 执行结果（该 mode 将重跑）时，
对该 mode 要求已记录的 scope 决策（`ExecutionScopes[mode]` 存在且覆盖当前快照）——否则返回错误，
提示先记录 scope 决策。**多 mode 记录（P2-甲确认）**：每个在上限处有权威 FAIL 的被选 mode 各自
需要一份 scope 决策（黑盒/白盒各一份、可不同）；`authorize-repair` 一次交互为多个 mode 一起记录
（重复内联参数，或预先用 `qa-execution-scope` 逐个记录后一次授权）。CLI 侧 `authorize-repair` 新
参数 `--qa-scope` / `--qa-cases` / `--qa-reason`（可重复）：未预先记录 scope 时，由该命令在同一
调用内一并记录，`Source` 记 **AUTHORIZE_REPAIR**（P2-丁确认，消除死枚举；`BaseSnapshot` 取当前
FAIL 快照，见 B1）。无论是否携带 scope，`authorize-repair` 仍只授权一个额外轮次（`cycles` 恒为
1），carry-forward 不授予轮次（RQ-006）。

### D2. carry-forward 自动沿用（host 判定子集）

在 D1 的打包点，若 `ExecutionScopes[mode].Decision == AFFECTED` 且其 `Source != CARRY_FORWARD`
（即上一次是用户主动选择而非已沿用的结果，避免无限循环沿用），则不要求用户再选"全量 vs 受影响"：
host 综合判定自动沿用子集（当前该 mode 的 FAIL 用例 + host 判定受本轮修复连带影响的既往通过用
例），CLI 记录 `ExecutionScopes[mode] = {Decision: AFFECTED, Mode: mode, BaseSnapshot: 当前 FAIL
快照, CaseIDs: host 判定的子集, Source: CARRY_FORWARD}`。子集扩展由 host 自行决定、不要求用户
确认（RQ-7/8）。最近一次决策为 FULL 或从未决策时，回到 D1 的强制询问路径（与"是否授权再来一
轮"同一交互）。

## E. 推荐逻辑（RQ-008，host 行为、写入文档）

formal-flow.md / SKILL.md 写明：重跑询问时 host 依据修复 diff 评估影响面，并 SHALL 显式提醒用户
"只跑受影响"的含义（挂掉的 + 受连带影响的既往通过用例，由 AI 综合判定）。推荐由 AI **按实际情
况综合判断、稍保守**（不确定时倾向 FULL），不设机械规则：修复窄、影响可靠有界、不涉及共享
API / 公开行为 / 配置 / 依赖 / 跨门职责 → 倾向推荐 AFFECTED 并展示拟重跑子集；涉及共享面、因
果不确定或无法界定 → 倾向推荐 FULL；继承历史等实际情况纳入 AI 判断（不设"长期未执行→偏向
FULL"硬规则）。选择时 SHALL 提醒用户：名单外（继承）用例本轮不验证，被修复连带破坏可能漏检，
风险由本次选择承担（RQ-8/10）。推荐仅作呈现，由用户拍板。

## F-3. QA 用例按 mode 分开存储、黑白盒完完全全解耦（RQ-012，追加需求）

黑盒与白盒 QA SHALL **完完全全解耦**（`mode == ""` 为合并/单派发流程）：
- **用例**：`RunState.QACasesByMode map[string][]QACase`；`RecordQADesign` 只动本 mode 列表，SHALL
  NOT 触碰另一 mode（另一 mode 的 review PASS 状态与执行结果保持）。
- **执行结果**：`RunState.QAExecution` 按 mode 分开（如 `QAExecutionByMode map[string]QAExecutionResult`
  或等价）；一个 mode 的设计/执行/记录 SHALL NOT 重置/清除另一 mode 的执行结果。`RecordQADesign`
  不再无条件重置共享 `QAExecution`（workflow.go:1657 缺陷）。
- **上一轮权威结果**：`RunState.PriorQAExecution` 按 mode 分开（如 `PriorQAExecutionByMode`）；一个
  mode 记录新权威结果 SHALL NOT 清空另一 mode 的 `PriorQAExecution`（workflow.go:1910 缺陷）——
  重跑识别（`qaExecutionPriorResultedBase`）与 AFFECTED 子集按 mode 各自独立。
- **派发/设计/审查锁按 mode**：`qaReviewDispatchPrepared(mode)` 只检查该 mode 的 OPEN/CLAIMED qa-review
  派发（workflow.go:1233 缺陷）——黑盒 review 在飞不锁白盒 qa-design。
- 所有读写（`qaExecutionRequiredCases`、继承记录、scope 强制、`selectedQAModesRecorded`、seal 解析）
  按 mode 各自独立。修复"黑白盒耦合"缺陷（RQ-012 / 用户驱动）。

## F-4. 派发作废机制修复（RQ-013，追加需求，用户定解法）

`staleOpenDispatches`（workflow.go:2765）作废条件 SHALL 改为：**按 mode 区分**——把 `qa-execution`
的 mode 豁免推广到全部 target（`dispatch.Mode != "" && dispatch.Mode != mode` 时跳过，覆盖
qa-review 等），修复"白盒 review prepare 作废黑盒 review"；**prepare SHALL NOT 作废任何派发**（移除
prepare 时 `staleOpenDispatches` 调用，workflow.go:879）。**拒绝并行（默认、唯一守卫）**：`claim-dispatch`
认领同功能（同 target+mode）新派发时，若已有 **CLAIMED** 同功能派发，SHALL 拒绝（去重只对 CLAIMED，OPEN
空票不挡认领、消除死锁）；认领新派发时把同功能旧 OPEN 空票（无子代理/无开始事件）自动作废清掉。**手动终
止例外（不新增 CLI 命令）**：绝对合理理由 + 用户显式授权下，主代理**直接终结前一个同功能子代理**（用自
己的工具停掉）——生命周期捕获其 stop 事件（记录中断原因）；**认领同功能新派发时读前派发的 stop 事件 →
前派发标记 STALE**，之后才可认领该新派发。**恢复路径**：`requirePreparedDispatch` 对 STALE 但审查者已认
领/已产出结果的派发放宽记录（校验 `ReviewerIdentity` 与结果内容后接受），不重审；审查阶段快照不变，恢复
记录落当前快照、无 source-binding 冲突；非常规快照已变情形保守拒绝；防 STALE 记录与替换派发并行记录双记。

## F-5. 并行检测与提醒（RQ-014，追加需求）

CLI SHALL **在会改变派发状态的工作流命令执行时自动检测并行性并发送提醒**（prepare-action / prepare-gate /
claim-dispatch / record-* / qa-* / snapshot / seal 等；读状态的 show 不触发；不依赖主代理主动跑检查），并
在生命周期 hook（子代理启动/停止）时触发检查。**可并行集合 = 按流程规则当前阶段应当并行的任务集**——
规则集在 Go 内**硬编码为解耦可扩展的"阶段→应并行任务"数据表**（如开发后阶段 = 黑盒 QA 执行 + 白盒 QA +
各已选门；SKILL.md / formal-flow.md 仅为参考；流程/规则常改，该表独立于逻辑、易改；规则集不依赖是否已
prepare）。当前在途并行数 = 已认领/在途派发数。若可并行集合非空且当前并行数为 0、或并行数小于可并行集
大小（如可并行 3 只并行 2），SHALL 在 **stderr** 显式提醒主代理（"可并行 X 项（列表），当前并行 Y 项，
建议补足"；不污染 stdout 的机器 JSON；**带冷却/去重**避免刷屏）。实现 SHALL **便宜**（只读、增量计算、
不重复扫描大状态）并**注意生命周期**（检测只读、不中断/不干扰在途子代理与派发状态、无副作用）。涉及实现
面含 `internal/lifecycle` 与安装 hook（hook 触发检查需读取 run 状态）。

**可观察验收（产品审 P2 补定义）**：
- **提醒必含内容**：stderr 提醒文本 SHALL 必含"可并行 N 项"（N=可并行集大小）、当前在途并行数
  "当前并行 M 项"（M=已认领在途派发数）与"建议补足"，并可列出可并行任务名（如
  "QA 执行（blackbox）"、"门审查（quality）"）；缺任一项即视为未触发有效提醒。
- **触发节奏/冷却**：状态改变命令成功后与生命周期 hook 落盘后各触发一次检查；同一签名（阶段+可并
  行集+在途数）的提醒在冷却窗口（默认 60s，测试可覆盖）内不重复连发，冷却窗口过后或签名变化后立
  即重新提醒。验收可观测：连续执行两条状态改变命令，首条后 stderr 出现提醒、同签名第二条在冷却窗
  口内不再出现；冷却窗口过后同签名再次出现。
- **原因如何取得**：可并行集与在途数均从 run 状态的只读计算得出——可并行集 = 阶段表对该阶段命中的
  应并行任务（已过滤掉已出结果/无需再派发的任务），在途数 = 该可并行集中 Status==CLAIMED 的派发数；
  不依赖 prepare 与否，不扫描 diff / transcript / 大状态。

## F-2. qa-design 反复补全（RQ-011，追加需求）

`requireTransition`（workflow.go:2672）的 `qa-design` 分支原守卫
"`qa-design.Status == PASS && qa-review.Status != PASS` → 拒绝重记录"改为：**仅当该 mode 存在
OPEN/CLAIMED 的 qa-review 派发时**才拒绝重记录（review 已开始，设计锁定）；qa-review 为 PENDING
（无派发）或 PASS/FAIL 后均允许重记录（复用既有增量修订机制：保留已批准用例、追加/更新其余）。
CLI `qa-design` 记录重复调用即追加/更新（现有 `RecordQADesign` 的 priorByKey 保留逻辑已支持）。
验收：设计记录部分用例后、review 派发前可继续调用 `qa-design` 补全；review 派发准备后重记录被拒。

## F. 测试

`workflow_test.go` 新增（约 12–18 个用例）：首次执行不要求 scope；重跑无 scope → prepare 拒
绝；FULL 重跑需执行集=全部已批准；AFFECTED 子集校验（非空、已批准、含上一轮 FAIL）；记录校验
（恰好覆盖需执行集）；继承记录带 `inherited` 标记且不参与 FAIL 聚合；per-mode 独立；上限处
authorize-repair 打包询问（FULL/无决策强制、AFFECTED 自动 CARRY_FORWARD；多 mode 各自一份、一次
交互一起记录）；carry-forward 不授予轮次；修复快照推进时权威 FAIL 结果保留到 `PriorQAExecution`
（FAIL 用例集可重跑识别）、RUNTIME_ERROR 不保留、新一轮权威记录取代且 **RUNTIME_ERROR 记录不
清空 `PriorQAExecution`**（P2-乙确认）。**既有测试适配（P2-丙确认）**：更新
`internal/validate/workflow_test.go` / `internal/cli/workflow_test.go` 中"推进修复快照后重新派发
qa-execution 而未记录 scope"的既有用例（如 `TestRepairUsesNativeSnapshotAndPreparedCarryBinding`
等）——改为先记录 FULL scope 决策或用断言验证新拒绝行为。

**RQ-13/14 新增测试（P1 确认补齐）：**
- RQ-13：prepare 不作废（同功能多次 prepare 既有票不 STALE）；白盒 review prepare 不作废黑盒 review
  （mode 区分）；认领时同功能去重拒绝（已有活动派发时再认领新派发被拒）；手动终止路径（前子代理 stop
  事件记录后认领同功能新派发 → 前派发 STALE、新派发可认领）；STALE 已产出结果可记录（校验身份+内容、
  与 source-binding/lifecycle 协调、防双记）。既有 prepare-时作废相关测试适配：`standalone_gate_test.go`
  （TestResumeGuardForcesResumeOrUserAuthorizationForGates:142、TestResumeGuardAllowsNewDispatchWhenTaskContentMoved:162）、
  `repro_carry_regression_test.go`（TestReproDevelopmentWorkerUserRequested:125）等。
- RQ-14：规则驱动可并行集计算（开发后 = 黑盒 QA 执行 + 白盒 QA + 已选门）；可并行集非空且并行数不足时
  stderr 提醒（含可并行列表、当前数、原因）；并行充分时不提醒；生命周期 hook 触发检查不干扰在途子代理。

## 附录：把流程每个阶段做成可选的改动规模评估（交付物，不实施）

**结论先行**：已有可选性机制已覆盖大部分价值；通用"每阶段可选"是一次需要专门拆分的重构，估算
核心 800–1500 LOC、测试 1500–3000 LOC，不建议本次实施。

- **已存在的可选性**：路线选择（full/custom）让 QA/门可选；seal 跳过（SEAL/SEAL-USER）、快照
  黑盒门放行（`--user-requested`）、carry 继承判定、review 轮次上限授权、产品审/技术审的
  `--user-requested` 重审，都是"按用户授权跳过/降级"的既有机制（workflow.go 中 user-requested
  相关 83 处）。
- **仍硬性**：需求澄清登记、product-review、start-readiness、拆分决定、路线、开发+快照。
- **通用化改动面**：`requireTransition`（workflow.go:2672）12 个操作 × 每操作 2–4 个前置全部改
  为"PASS **或** 显式跳过（带溯源）"；下游读 `Actions[...]` 的几十处判断（seal 解析、review 波
  次、carry 资格、QA 执行范围）都要把"跳过"当满足处理；需先定义语义（跳过快照→门无 diff 可
  看；跳过产品审→开发无产品校验；跳过 seal→run 无终态）。测试组合矩阵随 skip 组合膨胀。
- **估算**：核心 ~800–1500 LOC（workflow.go / runstate.go / cli.go），测试 ~1500–3000 LOC，
  加文档与已安装 skill 重装；需按独立验证边界拆分多个 run。
- **本次关系**：需求 2 的 scope 决策是针对性小机制，与通用可选性正交；本次不引入通用机制。
