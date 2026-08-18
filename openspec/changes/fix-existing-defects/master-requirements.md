# 修复现有实现缺陷（白盒测试代码进快照 / 未跟踪文件检测 / 删除 legacy "qa"）

Date: 2026-08-15
Status: confirmed
Route: (formal 流程内确认)

## 背景

formal-gates「编排流水线化」重构审查时，审查者在审视现状过程中顺带发现 4 个现有实现缺陷。
主代理已逐条对照源码核实，判定其中 1 个属有意设计、
不修（缺陷 2），其余 3 个属实、需修（缺陷 1 / 3 / 4）。改动触及 formal-gates 自身的核心快照
与 QA 逻辑，经用户确认走 formal-gates 正式流程。

初提需求已由主代理逐维拷问澄清、用户逐项确认完整需求与技术方案，并选择正式流程。本需求文档
登记已确认的需求与技术方案，作为本 run 的验收输入与唯一事实来源。

## 术语

- **白盒 QA**：开发后由 QA 设计者在主工作区编写**结构测试代码**（`prompts/actions/qa-design.md`
  规定的白盒模式），用于结构验证。
- **快照推进**：白盒测试代码写入后，把含测试代码的提交冻结为新的不可变快照（`workflow
  snapshot` 记录），使后续 qa-review / qa-execution 在该快照上派发、Seal 交付物包含测试代码。
- **legacy "qa"**：旧目录把 QA 当门登记时留下的保留名 `legacyQAID = "qa"`，CLI 将其当作内置
  合并态 QA 识别（黑白盒混成一次派发），绕过黑盒隔离 worktree 与白盒开发后时序。

## Complete confirmed requirements

1. **白盒测试代码必须进最终快照（方案 A：提交后快照推进）。** 白盒 QA 设计者在主工作区写
   结构测试代码并记录 qa-design 后，该测试代码 SHALL 被提交并推进快照到包含它的新快照 S2，
   qa-review / qa-execution 在 S2 上派发，Seal 后的交付物 SHALL 包含白盒测试代码。修复「白盒
   核心交付物（结构测试代码）静默丢失、既不被 gate 审查也不进最终交付」的缺陷。
   - 时序：开发提交 → 快照 S1 → 白盒写测试代码 + 记录 qa-design（测试代码未提交）→ 提交测试
     代码 → 快照推进到含测试代码的 S2 → qa-review / qa-execution 在 S2 派发 → Seal 含测试代码。
   - 实现取向：测试代码由 **host** 提交；快照推进**复用现有 snapshot 机制**、新增白盒测试代码
     推进路径，**不再要求 development-worker 派发**（白盒设计阶段无开发工作者派发可引用）。

2. **未跟踪文件检测并明确报错（冻结语义前修掉）。** 快照就绪的脏检查（`vcs.go` `VerifyReady`）
   SHALL 检测**未跟踪且未忽略**的文件，存在时明确报错，强制 `git add` 或显式报错，不让「新增
   交付文件漏了 git add」静默通过。已 ignore 的 `.gates/tmp/` 等目录不受影响。修复「快照身份取
   HEAD + 脏检查 `--untracked-files=no`，导致漏 add 的新交付文件静默丢失、无任何告警」的缺陷。

3. **删除 legacy "qa" 兼容。** SHALL 删除 `legacyQAID` 常量、`isQAMode` 对 `"qa"` 的识别、三处
   `isSelected(legacyQAID)` 特判，并移除锁定测试 `TestLegacyQAModeCarryRebindsQAExecution`；
   保留 gate 保留名（`qa` / `blackbox` / `whitebox` / `merge-qa`）的校验。修复「legacy "qa"
   合并态绕过黑盒隔离与白盒时序」的缺陷。新 run 的正常路线（`selectedQAModes`）已只返回
   blackbox / whitebox，删除兼容不影响正常使用。

## 非目标

- **缺陷 2（split 决定被提前到 start）不动。** 该行为是「需求 4」的有意设计（启动即暴露「忘带
  retained-overall」），其副作用是拆分决定提前到无依据时点；经确认本 run **不修改** split 时机。
- 不改变黑盒 / 白盒 QA 的既有用例设计、审查、执行、继承、Seal 语义，除上述三项修复外不改动
  其它快照 / QA / 门逻辑。
- Seal 不做任何远端推送（既有核实结论，本 run 不涉及）。

## 涉及文件（技术方案落地范围）

- 代码：
  - `internal/validate/vcs.go`（缺陷 4：脏检查改检测未跟踪且未忽略文件）
  - `internal/validate/workflow.go`（缺陷 1：白盒测试代码快照推进路径；缺陷 3：legacy 特判）
  - `internal/validate/workflow_qa.go`（缺陷 1：白盒记录；缺陷 3：`isQAMode` / 三处特判）
  - `internal/validate/workflow_carry_seal.go`（缺陷 1：快照推进路径）
  - `internal/validate/catalog.go`（缺陷 3：`legacyQAID` 常量）
- 测试：
  - `internal/validate/workflow_test.go`（缺陷 1：移除 `workflowFixture` 把测试写进 baseline 的假象，
    改为覆盖真实白盒测试代码提交路径；缺陷 3：移除 `TestLegacyQAModeCarryRebindsQAExecution`）
  - `internal/validate/repro_carry_regression_test.go`（缺陷 3：确认无 spurious `Gates["qa"]` 条目，
    随 legacy 删除后的断言一致性）
- 安装：变更后重新安装已安装的 skill（`~/.claude/skills/formal-gates`）。

## 前置处置（本仓库自身未跟踪文件）

本仓库当前工作树存在未跟踪文件（`TRIGGER-MODEL-REQUIREMENT.md`、`TRIGGER-MODEL-V2-REQUIREMENT.md`、
`refactor-plan/` 目录），缺陷 4 落地后 formal-gates 跑自身快照 / 封板的脏检查会撞上这些文件。启动正式
流程前须先处置：提交或加入 gitignore，使工作树在需要干净检查时不含未跟踪且未忽略的文件。
