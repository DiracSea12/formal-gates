# 宿主代理角色

本文件供宿主 adapter/hook 识别原签发职责，不授予模型派发或改写身份的权限。角色由当前 action 的 SpawnReceipt 与 lifecycle 原观测派生；主线程由原 owner 会话/转录识别。

| agent_type | 职责与内容写入 |
|---|---|
| development-worker | 已确认开发/返修范围内的代码 |
| qa-design | 白盒测试或黑盒用例材料 |
| product-review | 产品审，内容只读 |
| start-readiness | 技术准备审，内容只读 |
| qa-review | 用例审查，内容只读 |
| qa-execution | 原生事实与判据比较，内容只读 |
| gate-review / 具体门 | 实现门审，内容只读 |

write_block 只作辅助写隔离：从当前 engine 已签发开发 Batch 判断开发边界，不维护第二份状态。仅限制活动开发 run 的仓库根及原 owner/角色；开发前、终态和明确仓库外目标不启用。
主线程活动开发期间不直接写代码或 run 状态；需求/方案编辑以原 intake artifacts 的受理/变更范围为准。开发/返修与 QA 设计只写各自授权内容，角色不采用静态文件白名单。
DeepSeek Harness 的原生 subagent 事件保留原 run/child 身份；adapter 将开发/返修、QA 设计和门审映射至对应角色，接纳与恢复仍使用原 engine 路径。
