# Design

## A. 审查子代理污染检查（RQ-001~004）

### A1. 共享契约第一步完整性检查

`prompts/reviewer-base.md`（`internal/validate/catalog.go:49` 读取为 catalog.Base，仅门审
经 `ComposeGatePrompt` 注入，`internal/validate/runner.go:35`）增加"第一步任务完整性检
查"：枚举允许块清单、规定块结构之外任何文本即污染、拒绝协议（RUNTIME_ERROR + 拒绝原
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
- 已选门 / action 提示词或需求产物变化未判定的结果（既有 `gatePromptChanged` /
  `semanticResultRecorded` 守卫所覆盖的情形）。

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
置命令、豁免硬闸。

## C. 续用强制（RQ-007~009）

### C1. prepareBoundPrompt 守卫

在 `prepareBoundPrompt`（`workflow.go:769`，gate 与 action 共用的派发入口）新增守卫：目
标 T 存在 CLAIMED 且未出结果、源快照 == 当前快照、当前组装提示词 hash == 派发
PromptHash 的派发，且未带 `--user-requested` → 拒绝并返回"恢复原代理（身份 X，派发 D）
继续同一派发"。快照已变、职责不同、任务已变（hash 不同）任一成立则放行新派发。

### C2. 用户授权放行

复用既有 `--user-requested`"只有用户可破例、来源记入 ReviewOverrides"模式（`cli.go:333`
、`enforceReviewRule`，`workflow.go:1145`）。宿主确认无法恢复原代理时，主代理向用户呈
现并取得 `--user-requested` 授权后才可放行新派发。

### C3. 生命周期审计

续用保持同一身份，`VerifyDispatch`（`lifecycle.go:213`）start / stop 配对认证不变；
`ResolveClaimIdentity`（`lifecycle.go:179`）认领语义不变。不新增续用标记字段。

## D. 门单跑（RQ-010~011）

### D1. gate run 命令

新增 CLI 命令（如 `gate run <ids...>`）：完全脱离 run 状态，复用既有门文件 +
`prompts/reviewer-base.md` 组装单跑提示词；配套结果校验 / 展示路径，不写 run 状态、不做
claim / 生命周期。单跑提示词 `[Current change]` 描述工作树 vs HEAD（未提交改动）、无需求
块；`[Shared reviewer contract]` 复用，故污染检查自动生效。

### D2. 组装

复用 `ComposeGatePrompt` 的块结构（`runner.go:23`），以单跑路由替代 run 路由：base =
HEAD、current = 工作树（由 host / 审查者经 `git diff HEAD` 检视未提交改动）、无需求块、
无 dispatch 绑定。

### D3. 结果处理

单跑结果仅校验结果契约并展示给用户，不持久化、不进入 run 状态、不占用审查轮次上限。
