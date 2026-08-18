# Proposal

本阶段先处理分发、安装和基线安全，保证 stable driver 可以持续驱动后续正式流程，并让候选在隔离环境中可验证、可回退。它不提前改变 workflow 决策语义，也不把最终 engine 公共面写进 stable runtime。

阶段 0 交付“固定 stable driver + 阶段候选隔离验证”的基础：安装输入可验证、入口可登记、非幂等安装副作用可恢复、版本错误在写前拒绝，且实际 installed binary 的验证证据绑定不可变候选 identity。

## 参考关系

- 切片与退出条件：`refactor-plan/incremental-seal-plan.md` 第 2、3 节。
- 详细架构：`refactor-plan/final-implementation-draft.md` 第 1–11 节。
- 总需求：`openspec/changes/orchestration-pipeline-engine/master-requirements.md`。

## 非目标

- 不在本阶段引入新的 workflow engine writer 或第二套状态机。
- 不把最终公共面、`drive/submit` 或未实现的 engine 语义提前写入 stable package。
- 不以 source-tree 单测代替 installed-binary、独立 namespace 或故障恢复验证。
