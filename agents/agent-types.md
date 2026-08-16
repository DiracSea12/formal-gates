# 代理类型定义（RQ-011 写阻断）

本文件定义 formal-gates 的代理类型（`agent_type`）。RQ-011 的 PreToolUse 写阻断按
调用者身份（`agent_type`）判定：以 `development-worker` 从 `PENDING` 进入 `PREPARED`
为正式开发开始边界。边界之前的产品审、技术审和文档修订不启用写阻断；边界之后，主代理（主线程，payload 无
`agent_id`/`agent_type`）与全部审查类代理不得写代码或直接改 run 状态；写代码只经
development-worker 派发写入，白盒测试代码/黑盒用例文档由 qa-design 写入。判定按身份、
不使用静态文件白名单（千项目通用）；路径只用于限定活动仓库根的空间边界。写墙只在 run
同时满足 `status=ACTIVE` 且 `development-worker.status!=PENDING` 时生效；Seal / Abort
进入终态后立即解除。
它的空间范围仅限承载该 run 的仓库根；窗口 cwd 在活动仓库内、但明确写向仓库外路径时
放行，不影响其他目录或窗口的文件修改。

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

DeepSeek Harness 的 subagent 事件只有 run/child id，没有按类型注入 `agent_type`
的宿主字段。DSH 的 PreToolUse 载荷保留子代理 id；hook 在活动 run 已进入开发后，用
lifecycle claim 绑定（identity → dispatch id）反查该派发目标：
`development-worker`/`qa-design` 放行，gate 派发归一化为 `gate-review`，其余审查
动作按原 action id 判定。因此 DSH 子代理仍应在开始工作前执行
`workflow claim-dispatch`，否则在绑定建立前无法按角色识别，会按"其余代理"处理。

## 主代理（主线程）

主线程载荷无 `agent_id`/`agent_type`，判定为"主代理（主线程）"。进入开发后不得直接
写代码或改 run 状态；唯一豁免是编辑**已登记需求/设计文档**（RQ-011 登记集，按活动
run 的 `RequirementArtifacts` 动态识别，是需求更改流程的一部分）。
