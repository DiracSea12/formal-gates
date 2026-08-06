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
```

新增 `NewRunState` 初始化 `ExecutionScopes: map[string]QAExecutionScope{}`；运行摘要可按需暴露
最近一次 scope 决策（供 host/用户可见）。

### A2. 记录命令

`cli.go` 新增子命令 `workflow qa-execution-scope`，`workflow.go` 新增
`RecordExecutionScope(root, packageRoot, runID, mode, decision string, caseIDs []string, reason string)`：

- `decision` ∈ {FULL, AFFECTED}；`mode` ∈ {blackbox, whitebox, ""}。
- `AFFECTED` 必须有 `caseIDs` 且非空；`FULL` 忽略 `caseIDs`。
- 校验 `BaseSnapshot`：取该 mode 上一轮权威结果快照（见 B1），`AFFECTED` 要求存在上一轮权威结
  果（无上一轮结果时只能 FULL）。
- 校验 `caseIDs` 均为该 mode 已批准用例（`ReviewStatus == PASS`），且包含该 mode 上一轮全部
  FAIL 用例。
- 覆盖写 `state.ExecutionScopes[mode]`，`Origin=USER`、`Source=PREPARE`。

## B. prepare 强制点（RQ-002/004）

### B1. 重跑检测谓词

新增 `qaExecutionPriorResulted(state, mode)`：返回真当且仅当
`state.QAExecution.Status ∈ {PASS, FAIL}` 且 `Snapshot != ""` 且 `Snapshot != CurrentSnapshot`，
且（`mode == ""` 时直接为真；`mode != ""` 时该 mode 在上一轮结果中有用例记录）。

`prepare-action qa-execution`（`PrepareAction` 的 compose 回调）在现有
`qaExecutionModeResulted` 检查之后追加：

```go
if qaExecutionPriorResulted(*state, mode) {
    base := state.QAExecution.Snapshot
    sc, ok := state.ExecutionScopes[mode]
    if !ok || sc.BaseSnapshot != base {
        return fmt.Errorf("QA Execution rerun requires a scope decision: run `workflow qa-execution-scope --mode %s --decision FULL|AFFECTED ...` first", mode)
    }
}
```

首次执行（无上一轮权威结果）不经过该检查，直接全量。

### B2. 需执行集

`qaExecutionRequiredCases` 修改：在既有 mode 过滤前，若存在 `ExecutionScopes[mode]` 且
`Decision == AFFECTED` 且 `BaseSnapshot == 上一轮权威结果快照`，则需执行集 = 该 `CaseIDs` 对应
的用例；否则维持现状（全部已批准，按 mode 过滤）。

### B3. 执行提示词

`actionPromptDetail`（qa-execution 分支）按需执行集组装列出用例；AFFECTED 时追加一段：列出的
是本次需执行子集，其余已批准用例继承自 `<BaseSnapshot>` 的 PASS、不在本次执行范围内，执行者
不得自行补跑或改判继承用例。

## C. 记录与继承（RQ-005）

### C1. QAResultRecord 来源标记

`runstate.go` 的 `QAResultRecord` 新增 `Origin string`（`"executed"` 默认 / `"inherited"`）。
`RecordQAExecution` 对经执行的用例记 `executed`；AFFECTED 下未覆盖的已批准用例追加记
`inherited`（`Outcome=PASS`、`Observation="inherited PASS from <BaseSnapshot>"`）。

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
提示先记录 scope 决策。CLI 侧 `authorize-repair` 新参数 `--qa-scope` / `--qa-cases` / `--qa-reason`：
未预先记录 scope 时，由该命令在同一调用内一并记录（等价一次 `qa-execution-scope` 调用）。无论
是否携带 scope，`authorize-repair` 仍只授权一个额外轮次（`cycles` 恒为 1），carry-forward 不授
予轮次（RQ-006）。

### D2. carry-forward 自动沿用

在 D1 的打包点，若 `ExecutionScopes[mode].Decision == AFFECTED` 且其 `Source != CARRY_FORWARD`
（即上一次是用户主动选择而非已沿用的结果，避免无限循环沿用），则不要求用户再选：CLI 自动记录
`ExecutionScopes[mode] = {Decision: AFFECTED, Mode: mode, BaseSnapshot: 当前 FAIL 快照,
CaseIDs: 当前该 mode FAIL 用例, Source: CARRY_FORWARD}`。host 可在 prepare 前用一次新的
`qa-execution-scope`（`Source=PREPARE`）覆盖扩展子集。最近一次决策为 FULL 或从未决策时，回到
D1 的强制询问路径（与"是否授权再来一轮"同一交互）。

## E. 推荐逻辑（RQ-008，host 行为、写入文档）

formal-flow.md / SKILL.md 写明：重跑询问时 host 依据修复 diff 评估影响面——修复窄、影响可靠有
界、不涉及共享 API / 公开行为 / 配置 / 依赖 / 跨门职责 → 推荐 AFFECTED 并展示拟重跑子集；涉
及共享面、因果不确定或无法界定 → 推荐 FULL。推荐仅作呈现，由用户拍板。

## F. 测试

`workflow_test.go` 新增（约 10–15 个用例）：首次执行不要求 scope；重跑无 scope → prepare 拒
绝；FULL 重跑需执行集=全部已批准；AFFECTED 子集校验（非空、已批准、含上一轮 FAIL）；记录校验
（恰好覆盖需执行集）；继承记录带 `inherited` 标记且不参与 FAIL 聚合；per-mode 独立；上限处
authorize-repair 打包询问（FULL/无决策强制、AFFECTED 自动 CARRY_FORWARD）；carry-forward 不授
予轮次。

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
