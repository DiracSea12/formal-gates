# Design

## A. 审查子代理污染检查（RQ-001~004）

### A1. 共享契约第一步完整性检查

`prompts/reviewer-base.md`（`internal/validate/catalog.go:49` 读取为 catalog.Base，仅门审
经 `ComposeGatePrompt` 注入，`internal/validate/runner.go:35`）增加"第一步任务完整性检
查"：检查任务块结构之外是否有主代理擅自夹带的锚定信息（暴露先前工作 / 修复、破坏零上下
文），由审查者自行判断有无锚定、不做机械化块校验；发现即拒绝（RUNTIME_ERROR + 拒绝原
因）。审查流程重排为 ①完整性检查 → ②读需求 → ③VCS 比较 → ④审查 → ⑤返回。

### A2. 行动类审查提示词同样加入

`ComposeActionPrompt`（`internal/validate/runner.go:45`）不注入 catalog.Base，各行动审查
用自己的提示词文件。在 qa-design / qa-review / product-review / start-readiness /
requirements-clarification 五个提示词文件（`prompts/actions/*.md`）加入同样的完整性检查
（优先修改既有文件、不新增）。开发代理与 qa-execution 不加。

### A3. 合法输入（[Action input]）

契约写明：`[Action input]` 内的内容（qa-design 的既有用例集、product-review /
start-readiness 的已拍板发现项，注入点为 `actionPromptDetail`，`workflow.go:873`）为合法
输入、不算污染。

## B. 继承硬闸（RQ-005~006）

### B1. pendingInheritance 谓词

新增 `pendingInheritance(state)`：返回真当且仅当存在任一未处理继承判定：
- 需求产物变更未重新分类（复用既有 `requirementArtifactsChanged`，`workflow.go:2133` 供
  `requireCurrentDefinitions` 使用）；
- 存在旧快照 PASS 结果待 Carry 决策（复用既有 `eligibleCarryGates`，`workflow.go:2476`）；
- 已选门 / action 提示词或需求产物变化、且该目标**已有记录结果**（PASS/FAIL）未判定的情
  形（复用既有 `gatePromptChanged` / `semanticResultRecorded` 守卫所覆盖的情形）；仅在存
  在已记录结果时触发——开发前无记录结果、不触发、不阻塞。

### B2. requireNoPendingInheritance 硬闸

新增 `requireNoPendingInheritance`，挂到全部继续 / 重跑入口（prepare-gate、
prepare-action、claim-dispatch、record-gate、record-action、snapshot、seal、qa-*、
authorize-repair 等）。收敛现有零散守卫——`PrepareGate`"awaiting a Carry decision"
（`workflow.go:718`）、`RecordGate`"requires a Carry decision before rerun"
（`workflow.go:1238`）、`requireCurrentDefinitions` 需求产物变更阻塞（`workflow.go:2139`）
——为一致语义。

### B3. 处置命令豁免

`carry`（`RecordCarry`，`workflow.go:1700`）、`requirement`（`UpdateRequirement`，
`workflow.go:332`）、`settle-findings`（`RecordSettledFindings`，`workflow.go:617`）为处
置命令、豁免硬闸。第三类待判情形（已有记录结果的门/动作提示词内容变化）的处置由**主代
理直接完成并记录理由**（`carry --main-agent` 路径），该入口在存在受影响记录结果时可用
（含未发生修复的中途修改场景）；开发前无记录结果、不触发、不阻塞。

## C. 续用强制（RQ-007~009）

### C1. prepareBoundPrompt 守卫

在 `prepareBoundPrompt`（`workflow.go:769`，gate 与 action 共用的派发入口）新增守卫，按
RQ-007 三分支判定（目标 T 存在 CLAIMED 且未出结果）：
- **客观原因 + 未变 → 强制续用**：该派发已记录中断原因 = 客观因素（API 瞬时原因 429/503/
  402 等），且源快照 == 当前快照、当前任务内容 hash == 派发记录的任务内容 hash（门比较目
  录相关内容 hash `composedGatePromptHash`，action 比较 catalog 对应 action 提示词 hash，
  均不含派发编号、不含 `[Dispatch]` 块）→ 拒绝并返回"恢复原代理（身份 X，派发 D）继续同
  一派发"（消息明示：一切判定条件未变 + 中断为客观瞬时原因，须恢复原代理而非重开）。
- **有变化 → 不拦**：快照 / 任务 / 需求 / 方式 / 意图任一变化 → SHALL NOT 拦截，放行新派发。
- **未变 + 无原因 → 强制询问用户**：条件未变但无已记录中断原因（含"未知"）→ CLI 强制询问
  用户决策（不允许自动续用、也不允许自动放行），用户决定续用或经 `--user-requested` 重开。

### C2. 用户授权放行

复用既有 `--user-requested`"只有用户可破例、来源记入 ReviewOverrides"模式（`cli.go:333`
、`enforceReviewRule`，`workflow.go:1145`）。宿主确认无法恢复原代理时，主代理向用户呈
现并取得 `--user-requested` 授权后才可放行新派发。

### C3. 生命周期审计

续用保持同一身份，`VerifyDispatch`（`lifecycle.go:213`）start / stop 配对认证不变；
`ResolveClaimIdentity`（`lifecycle.go:179`）认领语义不变。**中断原因自动记录（RQ-013）**：
`lifecycle capture` 扩展从宿主 stop/error 事件提取原因字段（含 HTTP 错误码），写入派发；
Claude Code 与 Codex 适配，Cursor 可行时适配。宿主未提供原因 → 记录"未知"。被中断派发的
生命周期校验 SHALL 接受"start 事件 + 已记录中断原因"作为中断凭证（而非 REJECTED）。

## D. 门单跑（RQ-010~011）

### D1. gate run 命令

新增 CLI 命令 `gate run <ids...>`（组装并返回单跑提示词）与 `gate report`（接收审查者返
回的结果、校验结果契约并展示）：完全脱离 run 状态，复用既有门文件 + `prompts/reviewer-base.md`
组装单跑提示词；不写 run 状态、不做 claim / 生命周期。单跑提示词 `[Current change]` 描
述工作树 vs HEAD（未提交改动）、无需求块；`[Shared reviewer contract]` 复用，故污染检查
自动生效。

### D2. 组装

复用 `ComposeGatePrompt` 的块结构（`runner.go:23`），以单跑路由替代 run 路由：base =
HEAD、current = 工作树完整改动（**默认含未跟踪新文件**；审查者经当前 VCS 原生命令智能检
视完整 diff 内容：git 用 `git status` + `git diff`、svn 用 `svn status` + `svn diff`、
p4 用 `p4 opened` + `p4 diff`）、无需求块、无 dispatch 绑定。用户可指定逻辑范围（显式传
入审查范围/路径时按指定范围审查）。

### D3. 结果处理

`gate report` 仅校验结果契约并展示给用户，不持久化、不进入 run 状态、不占用审查轮次上
限；展示明确标注"脱离 run 的快速检查、非正式结论、未持久化"。

## R. 修复清单（Step 8 修复轮；本 run 门审查与设计讨论确认的缺陷与修订）

1. **P1-1 处置死锁**：已记录 FAIL 的门/action 结果，其提示词内容变化后进入待决继承，但
   `carry --main-agent` 只处理 PASS 结果（`eligibleMainCarryResults` 过滤 Status==PASS）、
   FAIL 结果无法处置 → 待决永不清除、run 永久卡死（基线行为 FAIL 门可直接重派，属回归）。
   修复：FAIL 结果也必须可处置——主代理经 `carry --main-agent` 记录"重跑"决定（接受 FAIL
   结果、记 RERUN），或 FAIL 结果提示词变化时放行直接重派（重派即处置）。
2. **P1-2 开发派发授权丢失**：`prepareDevelopmentAction` 硬编码 `userRequested=false`，丢弃
   CLI 的 `--user-requested`。修复：把 `userRequested` 透传给 `prepareBoundPrompt`，使开发
   /修复派发可用用户显式授权重开。
3. **qa-execution 按 mode 分流**：设计（blackbox-parallel 需求）"执行按 mode 分流"，实现
   `qaExecutionRequiredCases` 未按派发 mode 过滤、合并为一个派发。修复：qa-execution 按派
   发 mode 过滤需执行集（黑盒/白盒各自独立派发、并行执行）。
4. **质量门提示词加"审实现是否与需求有偏差"**：`gates/implementation-quality-gate.md` 增
   加对实现与需求偏差的审查——不仅核对本 diff 的需求覆盖，还审改动触及的实现是否偏离应有
   的设计语义（含既有设计如"执行按 mode 分流"）。
5. **续用拦截消息明确**：拦截时消息明示"一切判定条件未变 + 中断为 API 瞬时原因，须恢复原
   代理而非重开"，书面准确、无歧义。
6. **全部 CLI 拦截点信息清楚**：检查所有拦截/报错点，消息给清楚（说明为什么拦、如何处置），
   避免主代理或后续代理误解。
7. **RQ-013 中断原因自动记录 + 续用规则修订**：见 RQ-007/008/013 与本节 C1/C3（CLI 经生命
   周期 hook 自动记录中断原因，适配 Claude Code/Codex/Cursor 可行时；续用仅当"API 瞬时原因
   + 一切未变"时强制）。

8. **波次/seal 校验按 mode 拆分**（质量门 wave 2 P1）：`reviewWaveRecorded` /
   `completeReviewWaveIfReady` / Seal 的 `requireSelectedResultsResolved` 目前只检查共享
   `QAExecution` 合并结果，不要求每个选中 mode 已记录执行——单个 mode 记录 PASS 即可完成波
   次并 seal，另一 mode 被静默跳过。修复：波次完成与 seal 前按选中 mode 校验各 mode 均已记
   录执行。
9. **黑盒 QA 4 条 oracle 纠正**（CASE-008/009/010/011）：oracle 期望"记录继承决定后同目标
   prepare 放行（出新派发）"，但 INHERIT（保留旧结论）后结果在当前快照仍权威、既有权威结
   果守卫正确地阻止重跑；需求产物编辑在开发期间本就冻结、编辑须先重新分类。纠正这 4 条用
   例的预期判据：INHERIT → 不重跑、权威守卫拦下；RERUN → 重置、可重派；需求产物变化无记
   录结果时仍受冻结守卫约束。
10. **单跑提示词明示"无需求块属设计"**（质量门 P2-1）：`ComposeStandaloneGatePrompt` 的
    `[Current change]` 明示单跑无 `[Current requirement]` 块属设计意图，勿因缺需求块报
    RUNTIME_ERROR。
11. **payloadHTTPErrorCode 误分类修复**（质量门 P2-2）：命名字段空时扫描全部字符串标量把
    含 429/500/502/503/504/529 子串的非 HTTP 文本误分类为客观 API 原因。修复：限定独立数
    字 token 或专用错误码字段匹配。
