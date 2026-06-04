---
name: formal-gates
description: Proactively use before writing or modifying OpenSpec/PRD/SDD/start-readiness documents, and when the user explicitly asks for 四门流程, formal requirement clarification gate, writing-document gates, formal gate review, release/seal validation, QA gate, complexity gate, architecture-health gate, code-quality gate, GateWorkflow, gate-state/gate-workflow hooks, zero-context gate review, or formal-gates AB testing. Do not use for ordinary chat, brainstorming, light tasks, wording edits, explanations, or casual requirement discussion that is not entering document work or formal mode.
---

# Formal Gates

这是四门流程的入口。它只负责定规则、分流和交接；具体检查细节按需读 reference，别把全部内容一次塞进上下文。

普通聊天、头脑风暴、轻量解释、小改动不要启动这个 skill。需求还在随便聊时也不要启动。用户要写/改 OpenSpec、PRD、SDD、阶段文档或开工材料时，主动先走需求澄清门；正式审查、开工判断、发布/封板、四门、gate 也触发。

## 先判定是哪一种流程

- 需求澄清门：写/改 OpenSpec、PRD、SDD、阶段文档或开工材料前主动触发。读 `references/requirements-clarification-gate.md`。
- 文档/开工审查：OpenSpec、PRD、SDD、设计文档、阶段文档、能不能开工。见下面「OpenSpec/文档开工审查」节，需求澄清细节读 `references/requirements-clarification-gate.md`。
- 开发后/发布审查：已有实现、准备交付、release、seal、final QA。按下面的四门顺序走。
- 各门检查细节按需读对应 reference：QA 读 `qa-test-gate.md`，复杂度读 `complexity-gate.md`，架构读 `architecture-health-gate.md`，代码质量读 `code-quality-gate.md`，需求澄清读 `requirements-clarification-gate.md`。
- 安装、hook、canary、A/B、候选包测试、Claude/Codex/Cursor 接入：读 `references/install-and-hooks.md`。

Claude Code 是主用 host。Codex 只作为可选兼容或旧版对比路径。Cursor 通过 `.cursor/hooks.json` 或全局 `~/.cursor/hooks.json` 自动接 command hook。

## 不能碰的红线

- 主代理不得直接写非平凡代码、脚本、测试、配置或 OpenSpec 实现。必须派零上下文开发子代理。
- 任何非平凡开发前必须有 OpenSpec 文档或写入既有 OpenSpec 的 slice 文档。
- 开发子代理必须拿到 bundle/manifest、worktree、base commit 或非 git snapshot id、OpenSpec change、任务范围、禁止项、Complexity Contract 和验证要求。
- git 项目的开发子代理第一步必须回报 `git rev-parse --short HEAD`。SVN 或非 git 项目必须回报当前 `changeSnapshot`，通常由 `gate-workflow.ps1 -Action snapshot -Vcs auto` 生成。和 handoff 不一致就停止。
- dirty 或未提交的正式 snapshot，开发/审查子代理默认使用 `GateWorkflow.worktree` 指向的当前 worktree。只有已从 exact base 或同一 file-hash snapshot 准备并灌入同一 snapshot/artifact 的隔离 worktree 才能用；禁止默认从 `origin/main`、`origin/master` 或猜测 remote base 开工。
- 正式 PASS/FAIL/REVIEW 结论必须来自独立零上下文子代理，主代理不能自己判通过。
- 主代理必须审核独立 gate artifact 的观点是否有证据支撑；主代理有否决权，没有自封通过权。和独立 gate 结论有硬事实冲突时，记录证据并重派独立 gate agent 复核，不能自己改判为正式结论。
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

写/改 OpenSpec proposal/design/spec/tasks、PRD、SDD、阶段计划、开工材料时，按四步走，先读 `references/requirements-clarification-gate.md`：

1. 需求澄清：跑需求澄清门，带上确认答案、未决问题、draft 状态。`DRAFT_BLOCKED` 时文档只能 draft/未封板。
2. 架构形状：用 `architecture-health-gate` 的标准看文档（职责分层、所有权、依赖方向、是否无谓引入 public API/cache/manager/service），但不要求实现证据，只判边界是否可落地。
3. 复杂度与范围：用 `complexity-gate` 的 Stop Smells 看范围。proposal/spec 是需求源；plan/tasks/Contract 可拆分或约束交付，但不得改写或收窄用户原始需求。聚焦实现须标 `slice`/`partial` 并列出未覆盖项。发现未授权收窄，输出 `REQUIREMENTS_SCOPE_MISMATCH`（附原始需求、被收窄处、位置、应采取行动）。
4. 冷水开工审查：标准是“开发能否无方向错误地推进”，不是逐句改作文。会导致开发方向、架构边界、验收口径出错的问题必须拦；不影响开工的措辞和小问题只记录风险，不要无限打回。

正式文档工作记录：

```text
Document Writing Gates
Document/change:
Gate 1 需求澄清: PASS / DRAFT_BLOCKED / SKIPPED_BY_USER
  User answers captured:
  Open questions:
  Draft/seal status:
Gate 2 架构形状: PASS / REVIEW / FAIL
Gate 3 复杂度与范围: PASS / REVIEW / FAIL
Gate 4 冷水开工: PASS / REVIEW / FAIL
Verdict: DRAFT_ONLY / READY_FOR_ZERO_CONTEXT_REVIEW / BLOCKED
Required next action:
```

`READY_FOR_ZERO_CONTEXT_REVIEW` 不是开发批准。正式 OpenSpec/开工结论仍需独立零上下文的复杂度、架构、冷水审查。

## GateWorkflow 最小信息

正式流程必须有结构化 `GateWorkflow`，至少包含：

- `workflowId`
- `changeSnapshot`
- `worktree` 或 `statePath`
- 当前 `gate`
- QA gate 或 manifest 扩展 gate 的当前 `stage`。`complexity-gate`、`architecture-health-gate`、`code-quality-gate` 没有内置 stage 时可以省略。

`GateWorkflow.gate` 必须是四个固定 gate id，或 manifest 定义的扩展 gate。free-text `WorkflowId=... ChangeSnapshot=...` 只能算提示，不是正式记录。

缺这些信息时，不能记录正式 PASS，最多只能做 advisory review（单次参考意见），不能当作四门结论。`singleGateAuthorized=true` 的显式单门也只是 advisory，不能记录、复用或推进 release/seal。

正式 PASS 必须由独立零上下文子代理产出 artifact，并用 `gate-workflow.ps1 record-stage` 机器记录。artifact 必备字段（机器会校验，缺失或占位符被 hook 拦截）：

- 所有门：`Zero-context reviewer: YES`、`Independent agent: YES`、`Reviewer agent id:`（非空非占位）、`Context bundle:`（存在的文件 + sha256）、`No-anchor prompt: YES`、`gate_route:`（含 workflow_id/change_snapshot/next_action/rework_owner/rerun_from；REVIEW/FAIL/BLOCKED 不能路由到 proceed）。
- 复杂度/架构/代码质量门附加：`Changed files artifact`（或 `Raw diff artifact`）+ `Verification artifact`（或 `Developer self-test artifact`），路径必须存在。
- `qa-test-gate` 附加：`Approved case set` + `QA-owned evidence` + `Case-to-artifact binding`。

字段的完整模板、`gate_route` 取值、记录/校验命令、PowerShell 前缀、manifest 扩展 gate 规则、双装口径，见 `references/install-and-hooks.md`。`qa-test-gate` 记录别漏 `-Mode formal -Stage Execution`（最终 QA 用 `FinalExecution`）。

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
Snapshot id:
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
- worktree 和 base commit 或非 git snapshot id；
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
