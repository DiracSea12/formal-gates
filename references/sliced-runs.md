# 任务拆分与主线集成

拆分绑定唯一发生在技术审 PASS 后：用户确认精确 topology 的边界、数量、依赖与并行度；
no-split 理由留痕。start 不预判拆分、不创建 child。调用合同见
[正式流程](formal-flow.md)，计划依据见 [Batch ADR](../refactor-plan/adr-002-development-batching-and-verification-tiers.md)。

## 拆分判据

Batch 围绕连贯能力或风险闭环，具有独立 DoD/测试、稳定责任/接口/状态边界和恢复/回滚
协议，依赖屏障与交接成本可管理。Subtask 只是同一 Batch 的连续实现步骤，不独立派发。
编号、提交数、文件数、行数或开发阶段不能成为拆分理由；大而连贯的单一 run 是正常做法。

Run 级 split 与 Batch 分组分开。确认前可调整意向，确认后不得重切；实际变化经需求变化、
reset/rebuild 或 abort 的 typed Ask。引擎机械验证已确认图，不从自然语言重新设计拆法。

## Child 与 merge

engine 从 master 的确认拓扑创建独立 child 工作区和 run，持久化 sliceID→childRunID。
child 继承整体产品/技术审和需求 revision，不重做受理与整体双审；每个 child 独立确认
default/full/custom route；default 与 full 均选择全部普通门，custom 可选任意子集（含空集）或全集。
child 的候选不含普通黑盒/白盒 QA，不选择也不执行；master 强制
merge QA 与 merge gate。容量允许时并行派发独立 child，依赖边等待由 engine 管理。

child 完整候选、适用门结果、成本和 durable receipt 固定后形成 SLICE_READY。
VCS adapter 集成 child，只有精确冲突需要语义选择时才派窄主线开发动作；adapter 核验
全部冲突解决后 resolve、commit/submit、snapshot。merge QA 在 child 开发时并发设计/
审查，最终在主线候选执行；merge gate 审集成变更、跨 child 接缝与完整需求符合性。

merge finding 由主线 worker 修复。只改 integration-owned 内容时只补 merge 双验证；
触及 child-owned 内容则在最终主线候选补该 child 的受影响义务，不回 child 开第二条
返修路径。AffectedChildSet、结果继承和 cleanup 均共用当前提交与恢复合同。

master 接收需求变化并级联受影响 child；未受影响 receipt 经精确绑定保留。reset/abort
和终态清理覆盖全部登记 child/merge 资源；不能手工创建第二个总任务、替换原基线或
绕过 engine 重拼集成顺序。Git/SVN/P4 通过同一 adapter 合同承载工作副本/client，
实际支持与验证以同一候选的 provider 证据为准。
