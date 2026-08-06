# QA 执行重跑的全量/受影响决策（CLI 强制）

Date: 2026-08-07
Status: confirmed
Route: (formal 流程内确认)

## 背景

用户为 formal-gates 提一项流程改进：QA 执行需要**重跑**（新快照下已存在更早快照的权威执行
结果）时，host 必须在进入执行前询问用户"全量重跑还是只跑受影响的用例"并给出推荐；该询问由
CLI 强制，防止主代理遗漏。白盒 QA 沿用同一规则。共享审查轮次上限用尽后，轮次授权（"是否继
续 / 授权再来一轮"）始终由 CLI 强制；scope 决策在同一个用户交互中打包询问，但此前已选
AFFECTED 时不再重复问、直接沿用。

初提需求已由主代理逐维拷问澄清、用户逐项确认完整需求与技术方案，并选择正式流程。本需求文档
登记已确认的需求与技术方案，作为本 run 的验收输入与唯一事实来源。

## 术语

- **QA 执行重跑**：某 mode 的 QA 执行需要在**新快照**下再次派发——该 mode 已存在**更早快照**
  的权威执行结果（PASS 或 FAIL）。首次执行（无更早结果）不是重跑。
- **scope 决策**：用户对一次 QA 执行重跑选择的执行范围：`FULL`（全量重跑该 mode 全部已批准
  用例）或 `AFFECTED`（只重跑受影响用例，其余已批准用例继承上一轮 PASS）。
- **受影响用例**：本轮修复直接触及的用例（修复 diff 触及的用户可见行为链）、上一轮该 mode 的
  FAIL 用例（必须重跑、不可继承），以及 host 判定需要重跑的相关用例。
- **继承**：AFFECTED 下未被重跑的已批准用例，沿用其上一轮权威结果的 PASS，记录继承来源快照
  （`BaseSnapshot`）。

## Complete confirmed requirements

1. **首次执行不问、默认全量。** 某 mode 的 QA 首次执行（该 mode 无更早快照的权威结果）
   SHALL 不要求 scope 决策，需执行集为全部已批准用例（按 mode 过滤，现状不变）。

2. **重跑强制询问 scope 决策。** 某 mode 的 QA 执行重跑时，host 在进入执行前 SHALL 询问用户
   "全量重跑 vs 只跑受影响"并给出推荐；CLI SHALL 强制——`prepare-action qa-execution` 前该
   mode 必须已记录覆盖本次重跑的 scope 决策（`BaseSnapshot` 匹配上一轮权威结果快照），否则
   拒绝 prepare。黑盒与白盒各自独立、按 mode 记录与校验。

3. **scope 决策命令与状态。** 新增命令
   `workflow qa-execution-scope --mode <blackbox|whitebox|""> --decision FULL|AFFECTED
   [--cases <id,...>] [--reason '<...>']`。RunState 新增 `QAExecutionScope`（按 mode 一条、
   最新决策覆盖）：`Decision`、`Mode`、`BaseSnapshot`（继承来源=上一轮权威结果快照）、
   `CaseIDs`（AFFECTED 子集）、`Reason`、`Origin`（固定 USER）、`Source`
   （PREPARE / AUTHORIZE_REPAIR / CARRY_FORWARD）。

4. **需执行集与 AFFECTED 校验。** 需执行集：FULL / 首次 → 全部已批准（按 mode 过滤）；AFFECTED
   → 记录的 `CaseIDs` 子集。CLI SHALL 校验 AFFECTED 子集是已批准用例的非空子集，且必须包含
   上一轮该 mode 的**全部 FAIL 用例**（FAIL 不可继承）。

5. **记录与继承。** `RecordQAExecution` SHALL 校验结果恰好覆盖需执行集（既有
   `len(results)==len(required)` 约束保持）；AFFECTED 下未覆盖的已批准用例 SHALL 记录为"继承
   自 `<BaseSnapshot>` 的 PASS"（执行/继承来源标记，供审计与聚合区分）；聚合状态（任一**经执行**
   用例 FAIL → 整体 FAIL）与 seal 解析（该 mode 在当前快照已记录）保持一致。

6. **上限后轮次授权始终 CLI 强制。** 共享审查轮次上限用尽后，每一轮额外修复都必须显式
   `authorize-repair` 授权（CLI 拒绝无授权继续）；carry-forward SHALL NOT 自动授予轮次。

7. **上限处打包询问 scope 决策。** `authorize-repair` 时，若某 mode 的 QA 需要重跑（QA 选中且
   当前快照有该 mode 的权威 FAIL 结果），CLI SHALL 在同一交互中要求该 mode 的 scope 决策，按
   最近一次 scope 决策分流：
   - 最近一次为 **AFFECTED** → 不再询问，CLI 自动沿用（记录 `Source=CARRY_FORWARD` 的 scope：
     `BaseSnapshot`=当前 FAIL 快照、`CaseIDs`=当前该 mode 的 FAIL 用例；host 可在 prepare 前用
     新 scope 覆盖扩展子集）；
   - 最近一次为 **FULL** 或从未决策 → 与"是否授权再来一轮"一起询问（host 给推荐）。
   "沿用"只作用于上限处；正常重跑每次仍询问。

8. **推荐逻辑（host，写入文档）。** 修复范围窄、影响可靠有界、且不涉及共享 API / 公开行为 /
   配置 / 依赖 / 跨门职责 → 推荐 **AFFECTED**（并展示拟重跑的子集）；涉及共享面、因果不确定
   或无法界定 → 推荐 **FULL**。镜像既有 carry 判定原则。

## 非目标

- 不做通用"每阶段可选"机制；其改动规模评估随本 run 需求文档落档（见 design.md 附录），不实施。
- 不改动门审 / carry / 继承判定机制；本需求只约束 QA 执行的执行范围与轮次授权时的打包询问。
- 不改变首次执行的覆盖语义（仍要求全量覆盖全部已批准用例）。
- 不自动授予修复轮次；`authorize-repair` 始终要求用户显式授权。
- Seal 不做任何远端推送（既有核实结论，本 run 不涉及）。

## 涉及文件（技术方案落地范围）

- 文档：SKILL.md、references/formal-flow.md、prompts/actions/qa-execution.md、CHANGELOG.md
- 代码：internal/validate/workflow.go、internal/validate/runstate.go、internal/cli/cli.go
- 测试：internal/validate/workflow_test.go、internal/cli/workflow_test.go
- 安装：变更后重新安装已安装的 skill（~/.claude/skills/formal-gates）
