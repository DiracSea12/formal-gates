---
name: formal-gates
description: Use only when the user explicitly asks for 四门流程, formal requirement clarification gate, writing-document gates, formal OpenSpec/PRD/SDD/start-readiness review, release/seal validation, QA gate, complexity gate, architecture-health gate, code-quality gate, GateWorkflow, gate-state/gate-workflow hooks, zero-context gate review, or formal-gates AB testing. Do not use for ordinary chat, brainstorming, light tasks, wording edits, explanations, or casual requirement discussion unless the user asks to enter formal mode.
---

# Formal Gates

这是四门流程的入口。它只负责定规则、分流和交接；具体检查细节按需读 reference，别把全部内容一次塞进上下文。

普通聊天、头脑风暴、轻量解释、小改动不要启动这个 skill。需求还在随便聊时也不要启动；只有用户明确要求正式澄清、正式审查、正式开工判断、正式发布/封板结论，或明确提到四门、gate、OpenSpec 审查时才用。

## 先判定是哪一种流程

- 需求澄清门：用户明确要求写文档前正式澄清/对齐需求。读 `references/requirements-clarification-gate.md`。
- 文档/开工审查：OpenSpec、PRD、SDD、设计文档、阶段文档、能不能开工。读 `references/document-writing-gates.md`。
- 开发后/发布审查：已有实现、准备交付、release、seal、final QA。按下面的四门顺序走。
- 安装、hook、canary、Codex/Claude 接入问题：读 `references/install-and-hooks.md`。

## 按需加载 reference

- QA 设计、测试证据、Final QA、White-box：读 `references/qa-test-gate.md`。
- Complexity Contract、预算、diff 形状、shrink-before-grow、`complexity_gate.py`：读 `references/complexity-gate.md`。
- 模块边界、所有权、public surface、依赖方向、状态/cache 生命周期、解耦判断：读 `references/architecture-health-gate.md`。
- 正确性、边界、测试质量、死代码、过拟合、维护性、最终代码审查：读 `references/code-quality-gate.md`。
- 写文档前正式需求澄清：读 `references/requirements-clarification-gate.md`。
- OpenSpec/PRD/SDD/阶段文档/开工审查：读 `references/document-writing-gates.md`。
- 安装、hook、canary、A/B、Claude/Codex 接入：读 `references/install-and-hooks.md`。

## 候选包和 A/B 测试

A/B 或候选包测试时，必须先记录真实来源：

- `Skill source path`: 用户给的候选包路径。
- `Copied skill path`: 实际复制到测试 workspace 的路径。

同名全局 `formal-gates` 不等于候选包。用户给了候选路径，就优先读、复制、验证这个路径；不要默默读取 `%USERPROFILE%\.codex\skills\formal-gates`、`%USERPROFILE%\.claude\skills\formal-gates` 或其它全局原版。

## 不能碰的红线

- 主代理不得直接写非平凡代码、脚本、测试、配置或 OpenSpec 实现。必须派零上下文开发子代理。
- 任何非平凡开发前必须有 OpenSpec 文档或写入既有 OpenSpec 的 slice 文档。
- 开发子代理必须拿到 bundle/manifest、worktree、base commit、OpenSpec change、任务范围、禁止项、Complexity Contract 和验证要求。
- 开发子代理第一步必须回报 `git rev-parse --short HEAD`。如果和 handoff 的 base 不一致，停止。
- dirty 或未提交的正式 snapshot，开发/审查子代理默认使用 `GateWorkflow.worktree` 指向的当前 worktree。只有已从 exact base 准备并灌入同一 snapshot/artifact 的隔离 worktree 才能用；禁止默认从 `origin/main`、`origin/master` 或猜测 remote base 开工。
- 正式 PASS/FAIL/REVIEW 结论必须来自独立零上下文子代理，主代理不能自己判通过。
- OpenSpec proposal/design/spec/tasks/start-readiness 的“可开发/可开工/通过”结论，必须先有独立零上下文 complexity review、architecture-health review、cold-water review。
- 独立门禁需要外部编排。当前 agent 如果不能再派独立子代理，不能伪造 gate PASS，也不要把整个需求当成失败实现；输出 `Gate Handoff Request`，交给主代理或外部编排者派独立 gate agent。
- 如果发现主代理直接写代码、跳过独立门禁、或自封 gate 结论，立刻停止并输出 `PROCESS_VIOLATION`。

## 四个固定 gate id

这些 id 不能改名，脚本、hook、artifact、GateWorkflow 都必须用它们：

- `qa-test-gate`
- `complexity-gate`
- `architecture-health-gate`
- `code-quality-gate`

skill 名可以是 `formal-gates`，但机器识别的四个 gate id 必须保持不变。

## 开发后正式顺序

正式 release/seal 顺序必须完整，不是“四个 gate 跑完就封板”：

1. `qa-test-gate` Stage=`Design`：设计测试用例和 oracle。需要 QA 细节时读 `references/qa-test-gate.md`。
2. `qa-test-gate` Stage=`Design Review`：审用例，决定 `ACCEPT / REWORK / DROP / SPLIT / MERGE`。
3. `qa-test-gate` Stage=`Design Rework`：只改用例和 oracle，直到可执行。
4. 初始 `Verification Run`：QA 拥有或监督验证，不用开发自测冒充。
5. `qa-test-gate` Stage=`Execution`：把测试结果和 artifact 绑定到已审用例。
6. `complexity-gate`：先看有没有做大、净增是否合理、有没有新系统味。需要预算和 diff 规则时读 `references/complexity-gate.md`。
7. `architecture-health-gate`：再看模块边界、所有权、public surface、依赖方向、状态/cache 生命周期。需要架构细节时读 `references/architecture-health-gate.md`。
8. `code-quality-gate`：最后看正确性、边界、测试质量、死代码、过拟合和可维护性。需要代码质量细节时读 `references/code-quality-gate.md`。
9. 最终 `Verification Run`：在最终 diff/snapshot 上重跑必要验证，并把 accepted attempt artifact 聚合给 `gate-workflow.ps1 -Action record-final-verification`。
10. `qa-test-gate` Stage=`FinalExecution`：优先由 `record-final-verification -RecordFinalQa` 生成并记录最终 QA 结论，不要用手写 `record-stage FinalExecution` 冒充最终验证聚合。
11. `qa-test-gate` Stage=`White-box Adequacy`：需要时补看内部风险覆盖。
12. Final seal decision：只能在上面证据齐全且 snapshot 未变化时做。

复杂度没过，不准进入架构门。架构没过，不准进入代码质量门。不要用“代码还行”掩盖范围做大，也不要用“架构更完整”掩盖过度工程。

Feature Developer self-test 不是 gate stage。`Execution` 是下游 gate 前的 QA 执行，`FinalExecution` 是最终验证后的 QA 执行；两个 stage 不能混用。

任何实现改动都会让旧 snapshot 的 downstream PASS 失效。改了代码、脚本、测试、配置、OpenSpec 或 gate artifact 后，必须刷新 `changeSnapshot`，按阻塞 gate 指定的 `rerun_from` 重跑，不能复用旧 PASS。

用户已经授权正式 run 时，按上面顺序连续推进。只因为真实 blocker、gate 失败、缺机器元数据、预算扩张、snapshot 变化、破坏性/共享状态动作未授权、或需求不清而停下。

## OpenSpec/文档开工审查

OpenSpec proposal/design/spec/tasks、PRD、SDD、阶段计划、开工判断，先读 `references/requirements-clarification-gate.md`，再读 `references/document-writing-gates.md`。

开工审查看的是“能不能开发”，不是逐句改作文。会导致开发方向、架构边界、验收口径出错的问题必须拦；不影响开工的措辞和小问题只记录风险，不要无限打回。

## GateWorkflow 最小信息

正式流程必须有结构化 `GateWorkflow`，至少包含：

- `workflowId`
- `changeSnapshot`
- `worktree` 或 `statePath`
- 当前 `gate`
- QA gate 或 manifest 扩展 gate 的当前 `stage`。`complexity-gate`、`architecture-health-gate`、`code-quality-gate` 没有内置 stage 时可以省略。

`GateWorkflow.gate` 必须是四个固定 gate id，或 manifest 定义的扩展 gate。free-text `WorkflowId=... ChangeSnapshot=...` 只能算提示，不是正式记录。

自定义扩展 gate 必须带 `manifestPath`，并在 manifest 的 `stages.<gate-id>` 里定义依赖。例如：`GateWorkflow={"gate":"security-gate","workflowId":"...","changeSnapshot":"...","worktree":"...","manifestPath":"gate-manifest.json"}`。

Manifest 只能定义扩展 gate，不能定义或覆盖 `qa-test-gate`、`complexity-gate`、`architecture-health-gate`、`code-quality-gate`。四个内置 gate 的顺序是固定流程，不允许用 manifest 改写。

Manifest 扩展 gate 会绑定 manifest hash。扩展 gate 的前置 gate 也必须用同一个 `-ManifestPath` 记录，旧记录或没有 `manifestHash` 的记录不能满足扩展 gate admission。给既有流程新增 manifest 后，要按该 manifest 重新记录前置 gate，不能复用旧内置 PASS。

`singleGateAuthorized=true` 只允许显式单门 advisory review。它不是正式四门 PASS，不能记录、复用或推进 release/seal。

缺这些信息时，不能记录正式 PASS。最多只能做 advisory review，也就是“单次参考意见”，不能当作四门结论。

正式 gate artifact 必须写明：

```text
Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id:
Context bundle: <bundle-path> sha256=<bundle-sha256>
No-anchor prompt: YES
```

`Reviewer agent id` 不能是空值或占位符。`Context bundle` 必须是存在的文件并带 sha256；机器会校验 hash。

implementation 后的复杂度、架构、代码质量正式 PASS 还必须写明：

```text
Changed files artifact: <path>
Verification artifact: <path>
```

也可以用 `Raw diff artifact` 代替 `Changed files artifact`，用 `Developer self-test artifact` 代替 `Verification artifact`。这些 artifact 路径都必须存在。

`qa-test-gate` 正式 PASS 还必须写明：

```text
Approved case set:
QA-owned evidence:
Case-to-artifact binding:
```

每个正式 gate 输出都必须带机器可读路线：

```yaml
gate_route:
  workflow_id: ""
  change_snapshot: ""
  next_action: proceed | rework | blocked | seal
  rework_owner: none | implementation | tests | architecture | qa-cases | scope
  rerun_from: none | qa-design | qa-verification | qa-execution | complexity | architecture | code-quality | final-verification
```

`REVIEW`、`FAIL`、`BLOCKED` 不能路由到 `proceed`。

最小机器检查命令：

```powershell
pwsh <formal-gates>\scripts\gate-workflow.ps1 -Action verify-admission -Worktree <repo> -Gate <gate-id> -WorkflowId <id> -ChangeSnapshot <snapshot>
pwsh <formal-gates>\scripts\gate-workflow.ps1 -Action record-stage -Worktree <repo> -Gate <gate-id> -Verdict PASS -Artifact <artifact> -Actor <reviewer> -WorkflowId <id> -ChangeSnapshot <snapshot>
```

`qa-test-gate` 正式记录必须加 `-Mode formal -Stage Execution`；最终 QA 用 `-Mode formal -Stage FinalExecution`。不要直接照抄泛化命令漏掉 stage。

Claude/Codex skill 镜像和 hook 不是同一份时，不能声称“同一套 formal-gates 正在生效”。必须写清楚实际运行路径和 hash。

## Gate Handoff Request

当前 agent 不能派独立审查代理时，按这个模板交接：

```text
Gate Handoff Request
Reason:
Skill source path:
Copied skill path:
WorkflowId:
Change snapshot:
Worktree:
Base commit:
OpenSpec change:
Required independent gates:
Artifacts to provide:
Forbidden context:
Continue after:
```

`Required independent gates` 至少列出 QA、复杂度、架构、代码质量需要谁审。`Forbidden context` 写明不要给子代理塞主代理结论、怀疑点、上一轮 findings 或期望答案。主代理拿到独立 artifact 后，才能继续下一步。

## 零上下文不是空上下文

派子代理时必须给足本地事实：

- bundle/manifest 路径和 SHA；
- worktree 和 base commit；
- OpenSpec change、任务范围、禁止改文件、禁止扩张项；
- 相关 spec/design/tasks/case/diff/evidence artifact；
- 输出模板和必须运行的验证。

prompt 禁止塞主代理结论、上一轮 findings、刚修了什么、期望答案或怀疑点。要验证已知问题时，必须明说这是定向复核，不准冒充零上下文审查。

## 输出口径

- 没有完整门证据：只能说 `focused evidence pending full gate`。
- 没有独立 gate artifact：只能 `blocked` 或 `CONDITIONAL_PASS`，不能 formal PASS。
- 发现需求被无授权收窄：输出 `REQUIREMENTS_SCOPE_MISMATCH`。
- 流程被污染：输出 `PROCESS_VIOLATION`。
- hook 或 gate-state 只证明顺序和记录，不证明代码质量；质量结论仍要看独立 gate artifact。
