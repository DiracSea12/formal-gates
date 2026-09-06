# Requirements precedence

This is the complete stage-0 document inventory. Every listed requirement
source has one explicit status and priority; there is no catch-all or unnamed
OpenSpec source. “Priority” is the priority within this implementation scope,
not a new severity rule. The project boundary and P0–P3 interpretation remain
owned by `prompts/reviewer-base.md`.

| Status | Priority | Document | Authority in this work |
| --- | --- | --- | --- |
| current-authority | P0 | `openspec/changes/orchestration-pipeline-engine-phase-0/master-requirements.md` | Complete stage-0 requirements and acceptance contract. |
| current-authority | P0 | `refactor-plan/stage-0-requirements.md` | Confirmed stage-0 scope, gates, and normal-use boundary. |
| current-authority | P0 | `refactor-plan/stage-0-solution.md` | Confirmed implementation choices for this stage. |
| current-authority | P1 | `openspec/changes/orchestration-pipeline-engine/master-requirements.md` | Parent engine requirements, used where they do not conflict with the stage-0 slice. |
| current-authority | P1 | `refactor-plan/incremental-seal-plan.md` | Confirmed sequencing, isolation, and exit conditions. |
| reference | P0 | `openspec/changes/orchestration-pipeline-engine-phase-0/alignment.md` | Alignment evidence; it cannot override the two confirmed requirement documents. |
| reference | P0 | `openspec/changes/orchestration-pipeline-engine-phase-0/design.md` | Design rationale and constraints; implementation choices above win on conflict. |
| reference | P0 | `openspec/changes/orchestration-pipeline-engine-phase-0/proposal.md` | Proposal context only; not a public semantic source. |
| reference | P0 | `openspec/changes/orchestration-pipeline-engine-phase-0/tasks.md` | Task evidence only; it does not create a requirement or public entrypoint. |
| reference | P1 | `refactor-plan/final-implementation-draft.md` | Later architecture reference; later-stage behavior does not flow backward into stage 0. |
| orthogonal | P2 | `openspec/changes/blackbox-parallel-seal-squash-qa-mode/master-requirements.md` | Parallel/seal/squash QA scope; not part of this repair. |
| orthogonal | P2 | `openspec/changes/deadlock-recovery-and-codex-lifecycle/master-requirements.md` | Lifecycle/deadlock scope; only shared constraints are references. |
| orthogonal | P2 | `openspec/changes/fix-existing-defects/master-requirements.md` | Separate defect-remediation scope. |
| orthogonal | P2 | `openspec/changes/host-rule-management-and-codex-hook/master-requirements.md` | Host-rule and hook scope outside the stage-0 authority unless explicitly cited above. |
| orthogonal | P2 | `openspec/changes/p1-qa-decouple-and-carries-fix/master-requirements.md` | QA/carry scope outside this repair. |
| orthogonal | P2 | `openspec/changes/qa-execution-rerun-scope/master-requirements.md` | QA rerun scope outside this repair. |
| orthogonal | P2 | `openspec/changes/runtime-review-guards/master-requirements.md` | Runtime-review scope outside this repair. |
| orthogonal | P2 | `openspec/changes/sliced-runs-confirmation-and-qa-refactor/master-requirements.md` | Sliced-run and QA scope outside this repair. |
| orthogonal | P2 | `openspec/changes/two-phase-pre-development-review/master-requirements.md` | Pre-development-review scope outside this repair. |
| orthogonal | P2 | `P2-FIX-REQUIREMENT.md` | Root-level P2 historical/parallel defect scope; does not redefine stage-0 priority. |
| orthogonal | P2 | `QA-INCREMENTAL-ISOLATION-REQUIREMENT.md` | Root-level QA isolation scope; stage-0 uses it only as a non-conflicting reference. |
| superseded | P2 | `TRIGGER-MODEL-REQUIREMENT.md` | Retained for traceability; its intake yes/no wording is superseded by the current on-request trigger contract. |
| superseded | P2 | `TRIGGER-MODEL-V2-REQUIREMENT.md` | Retained for traceability; its intake yes/no wording is superseded by the current on-request trigger contract. |
| superseded | P3 | `openspec/changes/universal-modification-intake/master-requirements.md` | Retained for traceability; its universal-intake precedence and retired command surface are superseded by the current-authority rows. |
| historical | P3 | `CHANGELOG.md` and archived gate results | Historical evidence only; never a source for new public semantics. |

When a later stage changes a current contract, it must add a dated inventory
entry identifying the superseded source. The stage-0 implementation must not
add later-stage workflow behavior to stable `SKILL.md`, README files, or action
prompts.

## 2026-09-07：3.5 系列之后的总计划同步

上表是阶段 0 的历史受理清单，其优先级只适用于该阶段。后续重构以 engine 主需求为
总行为权威、最终方案为架构说明、增量计划为阶段/环境/退出合同、route matrix 为逐入口
迁移合同；阶段 0 清单不能反向覆盖以下后续已确认语义。具体正式 run 仍绑定自己的精确
确认 revision，不把工作区文档更新当成旧 run 或旧 Seal 的新确认。

| 来源 | 当前用途与替代范围 | 交付状态 |
| --- | --- | --- |
| `refactor-plan/stage-3-5a-qa-coverage-contract.md`、`stage-3-5a-solution.md` | 覆盖核心及三个只读 coverage 入口；替代原计划“不新增公开命令”的范围描述 | coverage checkpoint 已 Seal；regular engine 尚待接入 |
| `refactor-plan/stage-3-5a-fix-requirement.md` | 单一正式需求、REQ/AC 投影、显式保证激活、唯一 AC 主责任与最终合并 FULL，以及开发前审查轮数/纠正；后续 QA scope 按审查收敛与 3.5c 的明确变更更新 | legacy 已交付；阶段 4/5 接管，旧 formal requirement 保留历史绑定 |
| `refactor-plan/review-convergence-and-stage-3-5b-requirement.md`、`stage-3-5b-runtime-guardrails.md` | Operator 裁决、有限 retry、唯一返修、门审 P2 carry/P3 advisory、普通返修 AFFECTED/保证最终 FULL、owner cost 与只读 guard；其“QA 可选建议必须吸收”由下一行替代 | 合并 run 已 Seal；原授权跳过不改写，engine State/source bridge/guard 待阶段 4 |
| `refactor-plan/stage-3-5c-requirement.md`（正式需求）；`stage-3-5c-qa-efficiency.md`（计划来源） | 当前同步其共享步骤、可选建议三种理由处置、白盒有效测试复用、绑定 checkpoint、Codex 原生去重/来源/任务 timing，以及最终候选在最后必需验证前整理、Seal 不再 squash；替代上述旧 QA 规则及 Seal 尾部整理顺序 | 3.5c 已 Seal；阶段 4 沿同一语义接管 |
| engine 主需求 §6.1、最终方案 §4.1；当前项目 `SKILL.md` 受理规则 | 唯一文件先绑定受理 run、预检、展示全文/revision、用户确认；未明确模式的受理 run 保持未绑定，确认后初次选择 lightweight/regular，沿用同一文件，已绑定 regular 后不能转 lightweight；替代总方案“确认后才能 start”的 regular 受理顺序 | 未来 engine 受理合同；阶段 3 固定 driver receipt 桥接仍是开发期隔离合同 |
| engine 主需求 §7.2.2、最终方案 §5.2 | 用户明确保留已有 PASS 优先于本次需求变化产生的失效/fresh review/最终 FULL/child 重验；原证据、授权继承与新执行分别展示 | 后续阶段 4/5 合同，不属于 3.5c sealed candidate 的既有能力 |
| engine 主需求 §5.17、最终方案 §9.1.1、增量计划阶段 4/6/7 | 3.5c Seal 后补充的真实执行前准入、零调用验收与合法重试完成；不以旧 guard-preview 或事后拒绝代替 | 首宿主/五宿主/最终安装态分别待验收 |

候选与验证顺序以 3.5b AC-009/AC-012 和 3.5c REQ-007（AC-013/AC-014）为准：
最终提交整理（含必要 squash）→ 最终候选冻结、全部 FROZEN case-set 绑定并达到
review-ready → 必需 QA execution 与门并行 → Seal 固化同一候选。review-ready 是
接受执行/门审的就绪状态，不是门审 PASS；此前的 QA Review 审查用例与测试代码。
上述顺序替代总方案中白盒设计/审查期间提前执行黑盒或门、以及 Seal 尾部 squash 的旧描述。
历史正式需求、阶段记录正文与 Seal 结果保持原绑定；当前状态由 stage-records 的同日索引
追加汇总。八个正式阶段编号、单一 writer 和最终一次全局切换保持。
