# 设计：P1 QA 彻底解耦与 Carry 继承修复

## 概述

本 change 修复两个 P1 运行缺陷与两个提示词重复维护问题，P2 仅文档化。
核心是让 QA review/design 动作结果按 mode 完全独立，并修正 carry 主代理
继承的取数入口。

## P1-A：QA review/design 按 mode 解耦

### 现状（已逐行确认）

- `RunState.Actions["qa-review"]`、`Actions["qa-design"]` 是单一共享 `ActionResult`。
- `RecordQAReview`（workflow.go:1804）写共享 `Actions["qa-review"]=PASS/FAIL`；
  白盒 review prepare 时 `requireTransition`（:3299）看到共享状态 PASS 即拒绝 → 死锁。
- 隐藏耦合：黑盒 review FAIL 时 `RecordQAReview`（:1806）把共享
  `Actions["qa-design"]=PENDING`，同样卡住白盒。
- `qa-execution` 已按 mode 解耦（`QAExecutionByMode`），可仿照。

### 目标结构

在 `RunState` 新增按 mode 存储的 review/design 结果：

```go
// 按 QA 派发 mode 分开存储 qa-review 权威结果；空 mode 键对应合并/单派发流程。
QAReviewByMode map[string]ActionResult `json:"qaReviewByMode,omitempty"`
// 按 QA 派发 mode 分开存储 qa-design 权威结果；语义同 QAReviewByMode。
QADesignByMode map[string]ActionResult  `json:"qaDesignByMode,omitempty"`
```

辅助方法（仿 `qaExecution`/`setQAExecution`）：

- `state.qaReview(mode) ActionResult` / `state.setQAReview(mode, ActionResult)`
- `state.qaDesign(mode) ActionResult` / `state.setQADesign(mode, ActionResult)`

空 mode（合并/单派发流程）用 `""` 键，是设计内形态；不为旧状态做迁移。

### 读写点改造清单（逐处核对）

| 位置 | 现状 | 改后 |
|---|---|---|
| workflow.go:1701 `RecordQADesign` 成功 | `Actions["qa-design"]=PASS`、`Actions["qa-review"]=PENDING` | `setQADesign(mode, PASS)`、`setQAReview(mode, PENDING)` |
| workflow.go:1620 `RecordQADesign` RUNTIME_ERROR 路径 | 共享重置 | 按 mode |
| workflow.go:1804 `RecordQAReview` 成功 | `Actions["qa-review"]=status` | `setQAReview(mode, status)` |
| workflow.go:1806 FAIL 路径 | `Actions["qa-design"]=PENDING` | `setQADesign(mode, PENDING)` |
| workflow.go:1735 RUNTIME_ERROR | `Actions["qa-review"]=RUNTIME_ERROR` | `setQAReview(mode, ...)` |
| workflow.go:3296 `requireTransition` qa-review 前置 | `Actions["qa-design"].Status` | `state.qaDesign(target).Status` |
| workflow.go:3299 权威结果判定 | `Actions["qa-review"].Status` | `state.qaReview(target).Status`（target=mode） |
| workflow.go:156/160 `blackboxReviewPassed` | `Actions["qa-review"]` | `state.qaReview("blackbox")`（兼容 legacy "qa" 合并态） |
| workflow.go:1875 `RecordQAExecution` 空集放行 | `Actions["qa-review"].Status` | `state.qaReview(dispatch.Mode).Status` |
| workflow.go:3388 快照门 | `Actions["qa-review"].Status` | 按 blackbox mode |
| workflow.go:970/989 `actionPromptDetail` | 读 review FAIL | 按 mode |
| workflow.go:496/506 `SetRoute` 废弃设计 | 共享重置 | 按 mode |
| workflow.go:1683 `RecordQADesign` FAIL 检查 | `Actions["qa-review"].Status` | 按 mode |
| parallel.go:66 并行提示 | `Actions["qa-review"].Status` | 按 blackbox mode |
| canary.go:173 单跑 | 合并 `""` | 保持 `""`（canary 单跑语义） |
| runstate.go:258 `pendingRequirementActions` | 初始化共享 | 初始化为空 map 或按需初始化 |
| runner.go:158/186 结果契约 | 读 review | 按 mode（如需要） |

### requireTransition 的 target 传递

`PrepareAction` 目前只对 `qa-design` 传 `target=mode`（:785-786），`qa-review`
传 `target=""`。改为 `qa-review` 也传 mode，使 `requireTransition` 能按 mode 判定。

## P1-B：carry --main-agent 继承

### 现状

`eligibleMainCarryResults`（workflow.go:2202）对 QA mode 调 `qaModeResult(state, mode)`，
而 `qaModeResultKey`（:3767）只返回 `Snapshot == CurrentSnapshot` 的结果；修复快照后
CurrentSnapshot=S2，S1 的白盒 PASS 取不到，落到 `""` 回退（空）→ 永不 eligible。

### 修法

```go
result := state.qaExecution(qaDispatchMode(id))  // 直取该 mode，不要求 current snapshot
if result.Status != "PASS" { continue }
eligible := state.PreRepairSnapshot != "" && result.Snapshot == state.PreRepairSnapshot
eligible = eligible || (catalogChanged && result.Snapshot == state.CurrentSnapshot)
```

保留 legacy 合并流程：单派发 mode 为 `""`，`state.qaExecution("")` 即取合并结果，
行为不变。核查 `priorQAExecution` 回退路径以兼容旧状态中"结果被 reset 到 prior"
的情形。

### 影响面核实

- 改动只放宽 carry 的取数（能取到 S1 PASS），不改变重跑判定逻辑；
  因此不会导致原本 PASS 的项被强制重跑。
- 已 PASS 项可被 carry 继承而非重跑，符合文档意图。
- 重跑支持：`qa-execution-scope --decision AFFECTED --cases <ids>` 只重跑受影响
  子集，未受影响保持 PASS 继承（已有机制，黑盒测试 SC11 已验证）。

## P1-C：共享契约注入

- `runner.go:ComposeActionPrompt`（:146）加入 `[Shared reviewer contract]\n` +
  `catalog.Base`，与 `ComposeGatePrompt` 一致。
- 从 product-review.md、qa-design.md、qa-review.md、start-readiness.md 删除
  内联「任务完整性检查」块。
- requirements-clarification 不注入（主代理交互执行），单独斟酌。

## P1-D：formal-flow 去重

- formal-flow.md：机制重述缩为一句「决策本体见 SKILL 第 N 步」，只留命令形式；
  清「问题 6」「RQ-014」等内部编号残留；快照要求行 117 与 204 合并。
- SKILL.md 第 4 步复审规则收敛为对 formal-flow 的指引。

## P2 文档化

- 根目录新建 `P2-BACKLOG.md`：按领域记录全部 P2（位置+建议）。
- `.gitignore` 增加 `P2-BACKLOG.md`，不跟踪。

## 验证策略

- 单元/白盒：新增双 mode 独立 review/design 顺序用例、carry 继承用例。
- 黑盒：端到端双 mode 流程（先设计两 mode → 分别 review → 均 PASS → snapshot →
  exec → seal）；修复轮 carry 继承；AFFECTED 子集重跑。
- 修复后由主代理判定受影响既有用例，AFFECTED 子集重跑，未受影响保持 PASS。
