# Proposal

被中断且未产出结果的子代理派发，**在任何受支持宿主（Claude Code / Codex / Cursor）
上**，若该宿主提供续用机制（如 Claude Code `SendMessage`、各宿主的 agent/task 恢复），
优先恢复原代理继续完成同一次派发，不首先重开；仅当续用不可用（宿主不支持、恢复失败、
生命周期无法认证）时，才标 STALE 并重开新派发 + 新零上下文代理。

这是 Phase 3，反转当前"身份用一次、中断即重开"的规则（见
`resume-takeover-hardening` RQ-015）。同一子代理完成同一次审查不共享脑子、不违独立
性；续用跑完则 start→stop 生命周期成对、可认证。省去重读任务+diff 的 token。
