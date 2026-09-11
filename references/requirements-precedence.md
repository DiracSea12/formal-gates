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
追加汇总。八个正式阶段编号与单一 writer 保持；全局发布时间以后述阶段 4 发布修订为准。

## 2026-09-07：阶段 4 对照审查后的用户修订

本次用户明确确认的修订落在
`openspec/changes/orchestration-pipeline-engine-phase-4/master-requirements.md`，并同步到
engine 主需求、最终方案和增量计划。完整阶段需求的精确 revision 确认仍由原受理流程完成。

- 代码/质量门自阶段 4 起统一在本 run 全部开发完成、最终候选 review-ready 后执行，替代
  主需求 §8.5、增量计划阶段内分批规则与 ADR-002 中可选的组间增量门审；历史 ADR 与 Seal
  原文保留。批内自测、批边界构建/回归、开发前审查和并行黑盒用例审查不被此替代。
  实现保持审查时点、范围和结果接纳的清楚代码边界，便于后续迭代，不提前开放其他审查模式。
- token cap 的配置、生效值持久化、到顶停止自动调用及重启保持，补齐原 3.5b 留给阶段 4
  的接入；插件全部用户可配置项须方便手改与 AI 修改，共用校验及可查的有效值/来源。
- 跨产品/技术阶段新鲜复审的顺序、独立并行任务失败后继续与补位、单个操作内部步骤的
  顺序强制与中断恢复，成为阶段 4 明确验收项。固定步骤由 engine/adapter 执行，主代理与
  子代理越序请求在底层调用前拒绝；不以提示词或事后结果校验替代。

该修订不把待实现的能力声明为当前全局插件已具备；阶段 4 仍验收首宿主，阶段 5/6
扩展 split、其他 VCS/宿主。后续用户追加的阶段 4 全局发布时间见下一节。

## 2026-09-07：阶段 4 交付后全局发布

用户明确要求阶段 4 交付后将全局插件切到阶段 4 版本，替代总需求、最终方案和增量计划
中“阶段 7 才首次切换”“始终由阶段 0 包驱动”和“最终一次全局切换”的安排。

阶段 4 按原顺序完成隔离 QA/canary、本阶段固定 driver Seal、主线集成与必要重验，然后执行
既有全局安装事务、实际入口检查并记录发布 receipt。阶段 4 的精确发布包冻结为阶段 5–7
的正式 driver；阶段 5/6 的候选不自动发布，阶段 7 完成最终验证与 Seal 后再升级全局。
阶段 4 的 Seal 前验收使用隔离安装演练，实际发布检查属于交付后记录，不倒写已有 QA/Seal。

发布失败回退到该次切换前的稳定包，沿用单调 generation、旧 run 不迁移与单一 writer；
未完成阶段 4 发布不得声称已启用新 driver。历史阶段需求、ADR、stage-records 与 Seal
保留原绑定，当前正式需求仍由原受理流程确认更新后的精确 revision。

同日用户补充：切换后还须检查阶段 0、1、2、3、3.5 的功能是否都能正常使用。阶段 4
REQ-022/AC-079–081 将其落为发布完成条件：按原需求及交付记录逐项列明当前承载入口、
用例、执行结果和证据，涵盖 3.5a/fix/b/c；Seal 前隔离验证、切换后实际全局入口复验。
明确替代/延期/未验证项据实单列，历史 Seal 或授权跳过不等同于新版实跑 PASS。仍承诺
提供的功能出现范围内回归阻塞或缺少必需证据，不完成发布、不启用后续 driver，沿既有
发布失败合同回退。内部合同和破坏性安装用例的环境边界以 REQ-022 为准，历史记录不改写。

## 2026-09-07：阶段 4 固定流程、宿主接入与当前版本驱动

用户在本轮解释核对后要求修改需求与方案：插件的 engine/adapter 程序负责封闭流程、
操作内部顺序、前后置条件和失败分支；宿主只适配真实调用与事实回传。此处澄清既有
程序化责任，不削弱 REQ-002/AC-076 与 REQ-019/AC-051–053 的真实顺序、零调用和恢复验收。
宿主接入随对应功能实现并验证，具体未决接口风险才安排有界实验；替代总需求 §8.5
及增量计划中“探路必须首批”的规定和阶段 4 将宿主探路固定排在业务开发之前的安排。此前两批建议须
重新评估，不能自动沿用或因开发工具缺入口而自动合并；不删除产品多批次交付要求。

用户另明确“用当前版本开发”：阶段 4 使用本阶段需求案记录的精确当前版本作为固定
driver，替代此前“阶段 0 包驱动阶段 4”的环境规定。原 run 不由新版续写或手改迁移，
后续以新 run ID 绑定修订文件；旧结果保留原证据，不自动变成新 run PASS。阶段 0–3
的历史 driver/Seal 及已完成的 spike 记录保持原状；阶段 4 交付后全局切换、历史功能
逐项回归，以及阶段 5–7 使用冻结阶段 4 driver 的安排保持不变。
