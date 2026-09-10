# 拆分能力边界

阶段 4 regular 仅支持 Codex/Git/no-split；split、retained master、merge QA 和切片复用
按后续阶段接入，本版本没有旧 split 命令或兼容 writer。不能将无入口的旧流程当作可用能力。

当前一个 run 可以包含多个已确认 Batch。Batch 围绕独立可验收交付物、接口与责任边界、
风险闭环和可恢复交接；Subtask 是同一 Batch 内共享上下文的连续步骤。编号、commit 数、
文件数或层次本身不能证明需要拆分。计划和实际入口见
[开发计划](development-plan.md)与[regular 使用合同](engine-regular.md)。
