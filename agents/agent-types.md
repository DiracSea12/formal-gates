# 代理类型定义（RQ-011 写阻断）

本文件定义 formal-gates 的代理类型（`agent_type`）。RQ-011 的 PreToolUse 写阻断按
调用者身份（`agent_type`）判定：进入正式开发后，主代理（主线程，payload 无
`agent_id`/`agent_type`）与全部审查类代理不得写代码或直接改 run 状态；写代码只经
development-worker 派发写入，白盒测试代码/黑盒用例文档由 qa-design 写入。判定按身份、
不按文件路径（无静态文件白名单，千项目通用）。

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

## 主代理（主线程）

主线程载荷无 `agent_id`/`agent_type`，判定为"主代理（主线程）"。进入开发后不得直接
写代码或改 run 状态；唯一豁免是编辑**已登记需求/设计文档**（RQ-011 登记集，按活动
run 的 `RequirementArtifacts` 动态识别，是需求更改流程的一部分）。
