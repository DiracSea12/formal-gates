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
   → 记录的 `CaseIDs` 子集。AFFECTED 子集由 **host 综合判定**：必须包含上一轮该 mode 的**全部
   FAIL 用例**（FAIL 不可继承），并 SHALL 考虑受本轮修复连带影响的**既往通过用例**——不能机械只
   取 FAIL 用例。CLI SHALL 机械校验：子集是已批准用例的非空子集、且包含上一轮该 mode 全部 FAIL
   用例。

5. **记录与继承。** `RecordQAExecution` SHALL 校验结果恰好覆盖需执行集（既有
   `len(results)==len(required)` 约束保持）；AFFECTED 下未覆盖的已批准用例 SHALL 记录为"继承
   自 `<BaseSnapshot>` 的 PASS"（执行/继承来源标记，供审计与聚合区分）；聚合状态（任一**经执行**
   用例 FAIL → 整体 FAIL）与 seal 解析（该 mode 在当前快照已记录）保持一致。

6. **上限后轮次授权始终 CLI 强制。** 共享审查轮次上限用尽后，每一轮额外修复都必须显式
   `authorize-repair` 授权（CLI 拒绝无授权继续）；carry-forward SHALL NOT 自动授予轮次。

7. **上限处打包询问 scope 决策。** `authorize-repair` 时，若某 mode 的 QA 需要重跑（QA 选中且
   当前快照有该 mode 的权威 FAIL 结果），CLI SHALL 在同一交互中要求该 mode 的 scope 决策，按
   最近一次 scope 决策分流：
   - 最近一次为 **AFFECTED** → 不再询问"全量 vs 受影响"，CLI 自动沿用：host 综合判定自动沿用
     子集（当前该 mode 的 FAIL 用例 + 受连带影响的既往通过用例），记录 `Source=CARRY_FORWARD`
     的 scope（`BaseSnapshot`=当前 FAIL 快照、`CaseIDs`=host 判定的子集）；**子集扩展由 host 自
     行决定、不要求用户确认**；
   - 最近一次为 **FULL** 或从未决策 → 与"是否授权再来一轮"一起询问（host 给推荐）。
   "沿用"只作用于上限处；正常重跑每次仍询问。

8. **推荐逻辑与显式提醒（host，写入文档）。** 重跑询问时 host 给推荐，并 SHALL **显式提醒**用户
   "只跑受影响"的含义：包括上一轮**挂掉的**用例 + host（AI）判定可能受本轮修复**连带影响**的
   既往通过用例，子集由 host 综合判定、不要求用户逐项确认。推荐由 AI **按实际情况综合判断**、
   **稍保守**——不确定时倾向 **FULL**；供参考的判据：修复范围窄、影响可靠有界、且不涉及共享
   API / 公开行为 / 配置 / 依赖 / 跨门职责 → 倾向推荐 **AFFECTED**；涉及共享面、因果不确定或
   无法界定 → 倾向推荐 **FULL**。选择时 SHALL 提醒用户：名单外（继承）用例本轮不验证，若被修复
   连带破坏可能漏检，风险由本次选择承担。镜像既有 carry 判定原则。

9. **上一轮权威执行结果跨修复快照存续。** 某 mode 的 QA 执行在上一快照产出权威结果（PASS 或
   FAIL）后，修复快照推进 SHALL NOT 抹掉该结果：其快照与 FAIL 用例集 SHALL 存续，供重跑识别
   （RQ-2）、AFFECTED 子集判定（RQ-4）与上限 carry-forward（RQ-7）使用，直到被新一轮权威执行
   结果取代。RUNTIME_ERROR 不构成权威结果、不触发重跑识别。

10. **子集跑前定死、执行中不临时改。** AFFECTED 需执行子集在派发前由 host 综合判定定死（RQ-4）；
    执行者 SHALL 只执行该子集，SHALL NOT 自行判定/补跑/改判名单外（继承）用例，SHALL NOT 在执行
    中临时上报或改选。名单外用例可能被修复破坏的风险属 AFFECTED 的**接受风险**，由推荐时的 AI
    判断承担（不确定则倾向 FULL，见 RQ-8），并在选择时提醒用户。

11. **qa-design 可在 review 开始前反复补全（追加需求）。** 某 mode 的 QA 设计记录后、该 mode 的
    qa-review 派发**尚未准备**（无 OPEN/CLAIMED 的 review 派发）时，SHALL 允许再次调用 `qa-design`
    追加/更新用例集（保留既有已批准用例、增量补全，同增量修订机制）；只有该 mode 的 qa-review 派
    发准备后设计才锁定。修复"一次性调用记录全部用例、首条即误定死"的易错点（用户驱动：逐条/分次记
    录是正常使用，不应被当成设计完成）。

12. **QA 用例按 mode 分开存储、黑白盒完完全全解耦（追加需求，用户确认）。** 黑盒与白盒 QA 的用例、
    执行结果、上一轮权威结果与派发/设计/审查锁 SHALL **各自独立、互不耦合**：
    - **用例**按 mode 分开存储（`QACasesByMode`）；`qa-design` 记录轮只动本 mode 用例，SHALL NOT
      替换/清除另一 mode 的用例（含 review PASS 状态）。
    - **执行结果按 mode 独立**（`QAExecution` 按 mode 分开存储）：一个 mode 的设计/执行/记录 SHALL NOT
      清除或影响另一 mode 的执行结果。
    - **上一轮权威结果按 mode 独立**（`PriorQAExecution` 按 mode 分开）：一个 mode 记录新的权威结果
      SHALL NOT 清空另一 mode 的上一轮权威结果——重跑识别与 AFFECTED 子集按 mode 各自独立成立。
    - **派发/设计/审查锁按 mode**：`qaReviewDispatchPrepared` 等锁 SHALL 只按该 mode 的派发判定——黑盒
      review 在飞不锁白盒 qa-design，反之亦然。
    修复"黑白盒耦合：白盒设计清黑盒执行结果、一个 mode 记录清另一 mode 重跑依据、黑盒 review 锁白盒
    设计"的缺陷（用户驱动）。

13. **派发作废按 mode 区分、拒绝同功能子代理并行（默认去重、可手动终止例外）、已产出结果可补记录（追加需求，用户确认）。**
    - **作废机制（按 mode 区分 + prepare 不再作废）**：`staleOpenDispatches` 的作废 SHALL **按 mode 区分**——同
      target 不同 mode 的派发互不作废（与 `qa-execution` 既有豁免一致，扩展到 `qa-review` 等所有 action/gate，
      修复"白盒 review prepare 把黑盒 review 作废"）；**prepare（生成任务票）SHALL NOT 作废任何派发**（移除
      prepare 时 staleOpenDispatches 调用）。
    - **拒绝并行（默认、唯一守卫）**：CLI SHALL 默认拒绝**同一功能同时存在两个已派发子代理**——同一
      action/gate/mode 已有 **CLAIMED（已认领/在途）** 派发时，再次**认领**同功能新派发被拒（去重只对 CLAIMED；
      未认领的 OPEN 空票不挡认领，死锁消除）；认领新派发时把同功能旧 OPEN 空票（无子代理/无开始事件）自动
      作废清掉。
    - **手动终止例外**：**绝对合理理由**（用户显式授权）下，主代理**直接终结前一个同功能子代理**（用自己的
      工具停掉该子代理，无需新增 CLI 命令）——生命周期捕获其 stop 事件（记录中断原因）；**认领同功能新派发
      时读前派发的 stop 事件 → 前派发标记 STALE**，之后才可认领该新派发。
    - **恢复路径（先决）**：审查者已产出结果、派发被作废时，记录入口（`qa-review` / `record-gate` /
      `record-action` 等）SHALL 仍可接受该结果（校验审查者身份与结果内容后记录），不重审——已返回的结果
      可直接用，不需要重派审查者。审查阶段快照不变，恢复记录落当前快照、无 source-binding 冲突；非常规
      快照已变情形保守拒绝。
    修复"白盒 prepare 把黑盒 review 作废（不作 mode 区分）、审查者已返回结果却记录不进去、被迫重派审查者"
    的编排失误（用户驱动）。

14. **并行检测与提醒（追加需求，用户确认）。** 当 run 处于允许并行派发的阶段（如开发后多门 / 多 mode
    审查并行）时，CLI SHALL **在每次 workflow 命令执行时自动检测并行性并发送提醒**（主代理推进流程必然反复
    运行 workflow 命令，检测不依赖主代理主动跑检查），并在**生命周期 hook（子代理启动/停止）时触发检查**：
    检测当前是否可并行、可并行哪些派发；**可并行集合 = 按流程规则当前阶段应当并行的任务集**——规则集在
    Go 内**硬编码为一张解耦可扩展的"阶段→应并行任务"数据表**（如开发后阶段 = 黑盒 QA 执行 + 白盒 QA +
    各已选门；规则文件 SKILL.md / formal-flow.md 仅为参考；流程/规则经常修改，故该表独立于逻辑、易改）；
    规则集不依赖是否已 prepare。触发面：**会改变派发状态的工作流命令**（prepare-action / prepare-gate /
    claim-dispatch / record-* / qa-* / snapshot / seal 等）+ 生命周期 hook（子代理启停）；读状态的 show 不
    触发。若**当前没有并行派发**、或**并行数量不足**（当前在途并行数小于可并行集大小，如可并行 3 个只并行
    了 2 个），CLI SHALL **在 stderr 显式提醒主代理**（提示可并行哪些、当前并行数量、未并行/数量不足的原因，
    不污染 stdout 的机器 JSON；**带冷却/去重**，避免连发刷屏），促使主代理补足并行。实现 SHALL **注意性能——
    检测要便宜**（只读、增量、不重复扫描大状态）；SHALL **注意生命周期**（检测不干扰/不中断在途子代理与派
    发状态、无副作用）。修复"主代理长期串行、未充分利用可并行派发"的编排低效（用户驱动）。

## 非目标

- 不做通用"每阶段可选"机制；其改动规模评估随本 run 需求文档落档（见 design.md 附录），不实施。
- 不改动门审 / carry / 继承判定机制本身。本 run 的范围已从最初的"QA 执行的执行范围与轮次授权
  打包询问"随追加需求扩展为：QA 执行全量/受影响决策与轮次授权打包、qa-design 反复补全、QA 用例/
  执行结果/上一轮结果/派发-设计-审查锁按 mode 完全解耦、派发作废机制修复（prepare 不作废、按
  mode 作废、同功能去重与手动终止例外、STALE 恢复）、并行检测与提醒。门审 / carry / 继承判定 /
  Seal 的既有机制与语义均不改变。
- 不改变首次执行的覆盖语义（仍要求全量覆盖全部已批准用例）。
- 不自动授予修复轮次；`authorize-repair` 始终要求用户显式授权。
- Seal 不做任何远端推送（既有核实结论，本 run 不涉及）。

## 涉及文件（技术方案落地范围）

- 文档：SKILL.md、references/formal-flow.md、prompts/actions/qa-execution.md、CHANGELOG.md
- 代码：internal/validate/workflow.go、internal/validate/runstate.go、internal/cli/cli.go
- 测试：internal/validate/workflow_test.go、internal/cli/workflow_test.go
- 安装：变更后重新安装已安装的 skill（~/.claude/skills/formal-gates）
