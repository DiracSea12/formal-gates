# 阶段 0–3.5 当前能力逐项回归清单

本表保留各阶段原需求引用；原始结果和 Seal 不改写。当前源码为
`430d28cd10932219b2b18440ad4be252e9094003`。80 项保留能力按当前具名测试实跑通过，
1 项过渡 shadow 比较按用户明确删除兼容代码的要求退役。当前测试路径、命令、时间、
安装 identity 和逐项后继依据见 [机器记录](phase4-release-regression.json)。

旧 workflow writer、别名、状态 decoder 和 shadow 路径已删除。需求解析、成本解析、
安装维护及仍承诺的审查/QA/恢复语义使用各自当前归属者；不保留驱动旧 writer 的测试。
旧阶段只在原历史快照中的结构断言，不代表新版本仍公开旧接口。3.5c 付费模型对比按
REQ-022 不重复；其解析/计时能力已按当前代码回归。

本次 full、race、phase0whitebox、build、vet、package 与 portable 均通过。portable 是
5 类宿主的隔离安装/受理/回退演练；原生 Codex full 链路单列于 B3 原生证据，不据此
声称其他宿主支持 regular。正式 QA、Seal、集成和实际全局发布留待对应流程登记。

B3 开发 DoD 已完成。第 9 次安装态原生 full 的 Go 测试在 1800.548 秒达到
30 分钟宿主超时，结果保留为 FAIL_TIMEOUT；原 result JSON 未生成，也未补造。
原生子进程随后正常结算，原 run 在 revision 159 已签发最终保证的两个 QA 任务。
主代理沿同一安装入口、同一 run 执行这两个已签发动作，再用普通 drive 收口；
实际 terminal summary 为 revision 182、COMPLETE、SEALED，cleanup receipt 无残留。

该链路完成真实 FAIL→唯一 repair→新候选 `1d46b65457338337112caacaa82fdc8a0086df52`
→AFFECTED 重验及门 PASS→最终保证 FULL 黑盒/白盒均 PASS→promotion/Seal。
共 19 个真实流程 worker、1 个内部 model.run；另有 4 条初始 wave 中失效且未认领的
execution 记录，不能算作原生调用。这 20 个原生身份的实际 turn_context 均为 gpt-5.6-luna、max，
来自 start 前的隔离项目配置。原超时、同 run 续跑、原生转写、候选、安装身份和 Seal
分别记录于 [B3 原生证据](phase4-b3-native-evidence.json)，第 1–8 次真实失败仍保留。
这是批内开发验证，不代替阶段 4 正式 QA、Seal 或真实全局发布。
旧稳定包→通用维护版→阶段 4→原稳定包的隔离安装和双向故障回退已实际通过，
逐步 argv、身份与单调 generation 回执记录在机器清单的 crossVersionInstallation 中。

| 阶段与原需求 | 当前回归映射 | 隔离候选本次 | 实际全局本次 |
| --- | --- | --- | --- |
| 0 · [RQ-0-01 基线 identity](../openspec/changes/orchestration-pipeline-engine-phase-0/master-requirements.md) | [条目 H0-01](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 0 · [RQ-0-02 稳定驱动冻结](../openspec/changes/orchestration-pipeline-engine-phase-0/master-requirements.md) | [条目 H0-02](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 0 · [RQ-0-03 package 安全校验](../openspec/changes/orchestration-pipeline-engine-phase-0/master-requirements.md) | [条目 H0-03](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 0 · [RQ-0-04 registry admission](../openspec/changes/orchestration-pipeline-engine-phase-0/master-requirements.md) | [条目 H0-04](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 0 · [RQ-0-05 安装事务](../openspec/changes/orchestration-pipeline-engine-phase-0/master-requirements.md) | [条目 H0-05](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 0 · [RQ-0-06 故障恢复](../openspec/changes/orchestration-pipeline-engine-phase-0/master-requirements.md) | [条目 H0-06](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 0 · [RQ-0-07 版本与诊断](../openspec/changes/orchestration-pipeline-engine-phase-0/master-requirements.md) | [条目 H0-07](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 0 · [RQ-0-08 候选验证](../openspec/changes/orchestration-pipeline-engine-phase-0/master-requirements.md) | [条目 H0-08](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 0 · [RQ-0-09 文档与证据优先级](../openspec/changes/orchestration-pipeline-engine-phase-0/master-requirements.md) | [条目 H0-09](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 0 · [RQ-0-10 计划与方案一致性审查](../openspec/changes/orchestration-pipeline-engine-phase-0/master-requirements.md) | [条目 H0-10](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 1 · [范围与验收 1：Authoring](../openspec/changes/orchestration-pipeline-engine-phase-1/master-requirements.md) | [条目 H1-01](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 1 · [范围与验收 2：Closed-world compiler](../openspec/changes/orchestration-pipeline-engine-phase-1/master-requirements.md) | [条目 H1-02](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 1 · [范围与验收 3：Canonical 制品](../openspec/changes/orchestration-pipeline-engine-phase-1/master-requirements.md) | [条目 H1-03](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 1 · [范围与验收 4：决策核心](../openspec/changes/orchestration-pipeline-engine-phase-1/master-requirements.md) | [条目 H1-04](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 1 · [范围与验收 5：十条独立验收](../openspec/changes/orchestration-pipeline-engine-phase-1/master-requirements.md) | [条目 H1-05](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 1 · [范围与验收 6：只读对照](../openspec/changes/orchestration-pipeline-engine-phase-1/master-requirements.md) | [条目 H1-06](phase4-release-regression.json) | 退役（显式兼容删除） | 尚未全局发布 |
| 1 · [范围与验收 7：MISSING_ENGINE_ADAPTER](../openspec/changes/orchestration-pipeline-engine-phase-1/master-requirements.md) | [条目 H1-07](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 1 · [范围与验收 8：环境缺陷修复](../openspec/changes/orchestration-pipeline-engine-phase-1/master-requirements.md) | [条目 H1-08](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 1 · [范围与验收 9：Legacy 回归](../openspec/changes/orchestration-pipeline-engine-phase-1/master-requirements.md) | [条目 H1-09](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 2 · [范围与验收 1：版本 envelope](../openspec/changes/orchestration-pipeline-engine-phase-2/master-requirements.md) | [条目 H2-01](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 2 · [范围与验收 2：持久化基座](../openspec/changes/orchestration-pipeline-engine-phase-2/master-requirements.md) | [条目 H2-02](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 2 · [范围与验收 3：提交协议](../openspec/changes/orchestration-pipeline-engine-phase-2/master-requirements.md) | [条目 H2-03](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 2 · [范围与验收 4：统一接纳](../openspec/changes/orchestration-pipeline-engine-phase-2/master-requirements.md) | [条目 H2-04](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 2 · [范围与验收 5：失败分类路由与恢复](../openspec/changes/orchestration-pipeline-engine-phase-2/master-requirements.md) | [条目 H2-05](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 2 · [范围与验收 6：确定性故障注入](../openspec/changes/orchestration-pipeline-engine-phase-2/master-requirements.md) | [条目 H2-06](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 2 · [范围与验收 7：隔离与回归](../openspec/changes/orchestration-pipeline-engine-phase-2/master-requirements.md) | [条目 H2-07](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 2 · [范围与验收 8：负向测试](../openspec/changes/orchestration-pipeline-engine-phase-2/master-requirements.md) | [条目 H2-08](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 2 · [范围与验收 9：envelope PackageDigest 执行绑定](../openspec/changes/orchestration-pipeline-engine-phase-2/master-requirements.md) | [条目 H2-09](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 2 · [范围与验收 10：交付义务](../openspec/changes/orchestration-pipeline-engine-phase-2/master-requirements.md) | [条目 H2-10](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 2 · [范围与验收 11：成比例性影响评估](../openspec/changes/orchestration-pipeline-engine-phase-2/master-requirements.md) | [条目 H2-11](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3 · [范围与验收 1：实现候选 engine 的 workflow start、drive、submit、show、status、next、 diagnose 路由；start 是唯一 bo](../refactor-plan/stage-3-requirements.md) | [条目 H3-01](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3 · [范围与验收 2：跑通 lightweight 的“start 前已确认 intake → start → 首次 drive 自动登记 intake receipt/digest → engine 自动终结清理](../refactor-plan/stage-3-requirements.md) | [条目 H3-02](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3 · [范围与验收 3：在 engine protocol 的 whitebox/test-only sequence harness 中覆盖 Ask、Ready、 HostAction、Wait、Oper](../refactor-plan/stage-3-requirements.md) | [条目 H3-03](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3 · [范围与验收 4：engine loader 在任何写入前精确校验 workflowDefinitionVersion、stateSchemaVersion、 definitionSource、defin](../refactor-plan/stage-3-requirements.md) | [条目 H3-04](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3 · [范围与验收 5：由固定 stable driver 驱动本次 formal-gates run；candidate 只能在隔离安装目录、test project、 host config、state/resource](../refactor-plan/stage-3-requirements.md) | [条目 H3-05](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3 · [范围与验收 6：通过确定性测试覆盖：六类边界由 whitebox/test-only harness 覆盖，候选黑盒按第 3 条的 lightweight 范围执行；旧 requirement/cleanup](../refactor-plan/stage-3-requirements.md) | [条目 H3-06](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3 · [范围与验收 7：批次计划、Batch → Subtask 映射及代理边界由 Part 2 技术审的 granularity_review 给出并 留痕；一个 Batch 对应一次开发代理派发，Batch 内 ](../refactor-plan/stage-3-requirements.md) | [条目 H3-07](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5a · [1. AcceptanceManifest](../refactor-plan/stage-3-5a-qa-coverage-contract.md) | [条目 H3_5a-01](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5a · [2. source ↔ point ↔ case 双向映射](../refactor-plan/stage-3-5a-qa-coverage-contract.md) | [条目 H3_5a-02](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5a · [3. review 完整性](../refactor-plan/stage-3-5a-qa-coverage-contract.md) | [条目 H3_5a-03](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5a · [4. execution 完整性与候选绑定](../refactor-plan/stage-3-5a-qa-coverage-contract.md) | [条目 H3_5a-04](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5a-fix · [REQ-001：需求案与结构化需求点合一](../refactor-plan/stage-3-5a-fix-requirement.md) | [条目 H3_5a-fix-01](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5a-fix · [REQ-002：需求案正文保留自然语言，需求点采用固定 Markdown 结构](../refactor-plan/stage-3-5a-fix-requirement.md) | [条目 H3_5a-fix-02](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5a-fix · [REQ-003：AI 生成、程序预检、用户确认](../refactor-plan/stage-3-5a-fix-requirement.md) | [条目 H3_5a-fix-03](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5a-fix · [REQ-004：预检接入现有确认机制，不增加用户环节](../refactor-plan/stage-3-5a-fix-requirement.md) | [条目 H3_5a-fix-04](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5a-fix · [REQ-005：复用公开流程与既有覆盖协议](../refactor-plan/stage-3-5a-fix-requirement.md) | [条目 H3_5a-fix-05](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5a-fix · [REQ-006：每个已确认需求点必须完成 QA 覆盖与执行闭环](../refactor-plan/stage-3-5a-fix-requirement.md) | [条目 H3_5a-fix-06](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5a-fix · [REQ-007：产品审和技术审各自具有三轮自动上限](../refactor-plan/stage-3-5a-fix-requirement.md) | [条目 H3_5a-fix-07](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5a-fix · [REQ-008：审查状态可通过正式裁决纠正](../refactor-plan/stage-3-5a-fix-requirement.md) | [条目 H3_5a-fix-08](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-001：P0/P1 只接受完整且经主代理核实的阻塞证据](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-01](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-002：start-readiness 只有三类问题可以阻塞开工](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-02](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-003：同一快照收齐结果后只创建一份统一返修](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-03](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-004：问题语义归并由主代理判断并由 CLI 持久化](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-04](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-005：门审 P2 返修不复审，门审 P3 只在 Seal 展示](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-05](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-006：QA Review P2/P3 吸收完成后冻结用例集](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-06](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-007：普通代码返修不重新设计冻结 QA 用例](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-07](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-008：门审不接收 QA 用例并保护非显然验收接口](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-08](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-009：额外返修授权前展示完整且绑定版本的摘要](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-09](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-010：相同权威输入不重复派发](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-10](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-011：流程规则文档与自动回归必须和程序一致](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-11](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-012：owner 成本在现有单一投影中精确或明确不可用](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-12](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-013：engine guard 纯计算得到有限且稳定的派发额度](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-13](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-014：guard-preview 提供可黑盒验收的公开只读入口](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-14](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-015：未来 engine 接入计划继承本需求的新语义](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-15](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-016：产品审或技术审修订后只确认变更部分](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-16](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-017：用户处置审查 finding 前必须看到严重等级和理由](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-17](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-018：所有正式 P0/P1 必须逐条证明定级成立](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-18](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b · [REQ-019：会回退流程的需求影响登记必须由用户授权](../refactor-plan/review-convergence-and-stage-3-5b-requirement.md) | [条目 H3_5b-19](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b-runtime · [A. 成本模块保持可维护](../refactor-plan/stage-3-5b-runtime-guardrails.md) | [条目 H3_5b-runtime-01](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b-runtime · [B. 补记主代理（owner）用量](../refactor-plan/stage-3-5b-runtime-guardrails.md) | [条目 H3_5b-runtime-02](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5b-runtime · [C. 引擎中的最小运行时止损合同与纯计算核心](../refactor-plan/stage-3-5b-runtime-guardrails.md) | [条目 H3_5b-runtime-03](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5c · [REQ-001：共同说明复用与逐条增量审查](../refactor-plan/stage-3-5c-requirement.md) | [条目 H3_5c-01](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5c · [REQ-002：批量真实执行与状态隔离](../refactor-plan/stage-3-5c-requirement.md) | [条目 H3_5c-02](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5c · [REQ-003：构造前置状态与最小执行脚本](../refactor-plan/stage-3-5c-requirement.md) | [条目 H3_5c-03](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5c · [REQ-004：同次未完成执行的进度保存与受控续跑](../refactor-plan/stage-3-5c-requirement.md) | [条目 H3_5c-04](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5c · [REQ-005：白盒独立覆盖判断与已有测试复用](../refactor-plan/stage-3-5c-requirement.md) | [条目 H3_5c-05](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5c · [REQ-006：QA 可选建议按理由处置后冻结](../refactor-plan/stage-3-5c-requirement.md) | [条目 H3_5c-06](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5c · [REQ-007：最终候选先确定、最后验证、Seal 固化](../refactor-plan/stage-3-5c-requirement.md) | [条目 H3_5c-07](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5c · [REQ-008：隔离交付与阶段 4 同义接入](../refactor-plan/stage-3-5c-requirement.md) | [条目 H3_5c-08](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5c · [REQ-009：正确性与实际重复成本对比](../refactor-plan/stage-3-5c-requirement.md) | [条目 H3_5c-09](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
| 3.5c · [REQ-010：修复历史 checkpoint 检查的比较范围](../refactor-plan/stage-3-5c-requirement.md) | [条目 H3_5c-10](phase4-release-regression.json) | PASS（当前实跑） | 尚未全局发布 |
