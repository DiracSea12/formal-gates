# 代理类型定义（RQ-011 写阻断）

本文件定义 formal-gates 的代理类型（`agent_type`）。PreToolUse 的 write_block 是
辅助写隔离；它从当前 engine state 的已签发开发 Batch 派生开发开始边界，从
OwnerSession/OwnerTranscript、SpawnReceipts/lifecycle 识别 owner 对话与代理身份。
开发开始前与终态不启用写墙；活动开发期间主线程和审查类代理不得直接写代码或 run
状态，开发/返修与 QA 设计角色在其授权范围内编辑内容。业务推进、结果接纳与恢复
由统一 drive/submit 路径持有，hook 不拥有第二份流程状态。

作用域仅限承载 run 的仓库根、owner 对话和开发阶段；明确写向仓库外的目标放行。
角色判定不使用静态文件白名单，路径只限定空间边界。

派发子代理时，host 应把下列 `agent_type` 写入 PreToolUse 载荷，使 hook 能识别调用者
身份；主线程（主代理）的载荷不含 agent 身份字段。

## 写代码/测试/用例文档（放行）

| agent_type | 角色 |
|---|---|
| `development-worker` | 开发工作者：唯一的代码写入者，按已确认需求实现交付 |
| `qa-design` | QA 设计者：白盒设计独立写结构测试代码，黑盒设计写用例文档 |

## 审查类（阻断直接写入）

| agent_type | 角色 |
|---|---|
| `product-review` | 产品审（Part 1） |
| `start-readiness` | 开发前技术检查（Part 2） |
| `qa-review` | QA 审查 |
| `qa-execution` | QA 执行 |
| `carry` | 继承判定 |
| `gate-review` / 各门审查类型 | 开发后门审查 |

## DeepSeek Harness 适配

DeepSeek Harness 的原生 subagent 事件保留 run/child 身份。hook 在当前活动开发 run
中，以已签发 action 的 SpawnReceipt 与 lifecycle 原始观测反查代理职责；开发/返修
归入 development-worker，QA 设计归入 qa-design，门审归入 gate-review。
身份绑定与 UNKNOWN 对账由 drive/submit 共用路径管理，代理不自行认领或创建派发。

## 主代理（主线程）

主线程载荷无 `agent_id`/`agent_type`，判定为"主代理（主线程）"。进入开发后不得直接
写代码或改 run 状态；需求/方案编辑范围由当前 intake receipt 的确认 artifacts 派生，
编辑仍是受理或需求变更流程的一部分，不授权主线程直接改 run 状态。
