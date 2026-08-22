# Alignment

- 阶段切片：`incremental-seal-plan.md` 阶段 1 范围已按 ADR-001 更新（spike 前置、closed-world compiler、canonical 制品独立验收、止损规则），本阶段需求与其逐条对应，无缩小或扩大。
- 总需求：master-requirements §5.6–5.9（变体 authoring、enforcement matrix、三类身份、双摘要绑定）由本阶段实现其只读子集（定义、编译、决策计算）；写入侧（envelope 执行绑定、CAS）属阶段 2，本阶段只实现版本/digest 常量与 loader 校验的只读部分。
- ADR-001：十条阶段 1 落地要求全部纳入本阶段验收（freshness、assembly-order、round-trip、跨进程、digest 分离/敏感性、registry 完备、constructor 非法状态、mutation、止损）。
- 阶段 0 契约：不修改 stable 公开语义；`definitions/workflow.json` 从当前仅含版本信封扩展为完整拓扑时，按 bump 规则提升 workflowDefinitionVersion；stable driver 与既有 legacy run 不受影响。
- 环境缺陷登记：测试隔离修复与 stable driver 重冻结记录（main HEAD 7929891、validate/canary PASS；5373c13 从未驱动过 run）纳入本阶段交付，防止阶段 2+ 再次发生同类污染。
