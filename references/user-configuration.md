# 用户配置

regular 在创建 run 时读取 UTF-8 JSON 文件，字段逐项合并：

1. 内置默认值。
2. 用户/安装级 `~/.config/formal-gates/config.json`。
3. 项目级 `<project>/.gates/config.json`，覆盖同名字段。

文件可直接用文本编辑器或 AI 编辑。文件不存在时继承前一级；删除字段可恢复继承。
保存后先运行 `formal-gates workflow diagnose --root <project> --config`：输出
`values`、每个字段的 `sources` 与两个候选 `files`，不创建或修改 run。

| 字段 | 类型与合法值 | 默认 | 含义 |
| --- | --- | --- | --- |
| `tokenCap` | 正整数，单位 token | 未配置 | 本 run 的 engine 派发输入加输出 token 达到该值时停止自动调用 |
| `qaCapacity` | 1–64 整数 | 4 | 同一 run 同时运行的 QA prepare/target/collect 操作上限；超出留在 pending，完成后继续消费 |
| `capacity` | 1–64 整数 | 4 | 一次决策最多签发的任务数，仍受前置与 guard 限制 |
| `codexExecutable` | 非空命令名或可执行文件绝对路径 | `codex` | 原生 Codex bridge 可执行文件；选择 regular 时检查可用性 |
| `model` | 非空模型标识 | 宿主默认 | 传给原生 CLI 的模型选项 |
| `reasoningEffort` | `minimal/low/medium/high/xhigh/max/ultra` | 宿主默认 | 传给原生 CLI；具体模型是否支持由原生 CLI 检验，失败消耗有限执行 attempt |

例如只调整项目 cap 和容量：

```json
{
  "tokenCap": 200000,
  "capacity": 2
}
```

`null`、拼错字段、字符串型数字、负数、非整数及非法枚举会报错，错误包含文件、字段和
修正说明。错误不会把 cap 变成未配置。model/effort 传给所选宿主；本配置不承诺任意模型支持
任意 effort。原生运行时错误与语义审查轮次分别记账。

生效值和来源持久化在 `workflow show` 的 `regular.settings`。文件编辑影响后续新 run，
已经存在的 run 和重启恢复使用原值，因此不会因配置编辑清空计量、提高次数或取消止损。
`regular.guard.tokenCapStatus` 分别显示 `UNCONFIGURED`、`AVAILABLE` 或 `UNAVAILABLE`；
没有 transcript 必须有明确诊断原因，不伪装为已测得零用量。owner 会话用量不计入派发 cap。

次数额度由已确认需求、定义、路线、拓扑、计划与有限用户授权计算，不是可编辑配置。
`workflow submit` 的 `continue`、`authorize-retry` 和 `authorize-round` 先产生 typed Ask，
再用当前 token/digest 确认；不能通过配置文件直接添加授权。

其他已有用户输入继续使用原归属：

| 输入 | 位置/修改方法 | 生效边界 |
| --- | --- | --- |
| 单一正式需求及方案 | `workflow start --requirement` 指定的项目相对 Markdown 文件 | 预检后确认精确 revision；运行中通过 requirement-change/apply-requirement 重新确认 |
| 验证清单 | 包的 `references/local-validation.md` | 按本地项目清单运行；不作为可执行 JSON 配置 |
| 门与审查文本 | 安装包 `gates/*.md`、`prompts/` | 安装内容的一部分；开发源码后走既有安装事务刷新，不手改受保护安装身份 |
| 宿主接入与安装位置 | `install --help` 的 host/scope/project/binary-target 等参数，宿主配置由安装器维护 | 沿用 registry/bootstrap 和安装事务；不能手改 registry/receipt/state 冒充用户配置 |
| Codex 登录与会话根 | 原生 Codex 登录、`CODEX_HOME`（未设时 `~/.codex`） | 使用原生认证和 transcript；审查调用忽略用户 config、禁用工具和嵌套代理 |

配置诊断不修改安装包、registry、状态、receipt、生成定义或任何 digest。修改安装参数使用
安装命令；修改需求使用受理/修订流程；修改运行参数使用这里列出的 JSON 文件。

### 三宿主原生接入（阶段 4a）

WorkBuddy、ZCode、DeepSeek Harness（`dsh`）已注册同一 regular runtime adapter
入口。安装提供平台技能入口；执行器消费 engine 已签发的动作，沿现有结果 writer、
任务操作、批准、恢复与终态清理运行，不另建调度或账本。平台差异在各自适配文件中。
MCP 仅是工具传输手段：WorkBuddy/ZCode 使用绑定的 stdio 工具服务；DSH 注册原生工具，
直接调用现有 CLI 任务操作边界。

| 宿主 | 原生启动、续用与结束 | 结果与计量来源 |
|---|---|---|
| WorkBuddy | CodeBuddy print/stream-json；请求 session UUID 后等待原生 init 确认；续用同一 session；进程退出留证；恢复观察使用 ACP session/load | 同 session 的成功 result 和结构化输出；SDK assistant 消息 usage，按 uuid 去重 |
| ZCode | 自带 app-server NDJSON；宿主分配 sessionId；session/resume 保留身份，完成后 session/close；首次取得身份后重载同会话绑定工具，再发送输入 | 只接受匹配 session/input/turn 的成功 turn.completed，accepted 不是完成；session/usage 累计快照，不重复相加 |
| DSH | 已有 ACP profile 加本次 action 工具配置；session/new 或 session/resume，完成后 session/close | 原生 session/event 的 assistant/message 和 completed turn/end；仅 ACP end_turn 不算成功；assistant 及真实 LLM compaction usage 按 seq 去重 |

WorkBuddy 用量名称为 `workbuddy`（别名 `codebuddy`、`cbc`）；读取
`message.usage.input_tokens`、`output_tokens` 和可选 `cache_read_input_tokens`。
缓存写入量沿既有成本口径不进入已定价类别。DSH 使用原生 inputTokens/outputTokens/
cacheReadTokens，输入未命中量为 inputTokens 减 cacheReadTokens；reasoning 不再重复相加。
ZCode 使用累计 inputTokens/outputTokens/cacheReadTokens，并要求 modelRequestCount 大于零，
避免把无后端的全零 fallback 当成真实计量。混合会话、冲突/回退、缺失必要字段或来源
均报告 unavailable 及原因。owner 基线与同身份增量结算继续由既有 cost 实现处理。

适配器在 engine 执行目录保存原生流和进程回执，将同身份计量来源累积到
`executions/native-sources/<provider>`；它们是结果来源证据，不是新的用户配置或账本。
工作、临时与输出资源沿现有准备分配和清理，认证由宿主原生配置持有，不复制凭证。
绑定包含首次捕获的可执行文件、环境及脚本路径；恢复不能因后来修改配置而换宿主身份。

### 新宿主可编辑选项

以下选项使用本文件开头的相同用户/项目 JSON 合并与 run 快照。不会自动安装 CLI。
未在 PATH 发现默认命令时检查已知安装位置；显式指定路径不存在会报错，不偷偷换命令。

| 字段 | 默认值 | 单位与范围 | 错误与使用说明 |
|---|---|---|---|
| `workbuddyExecutable` | `codebuddy` | 命令名或可执行文件路径字符串 | 默认命令不在 PATH 时检查 `/Applications/WorkBuddy.app/Contents/Resources/app.asar.unpacked/cli/bin/codebuddy` |
| `zcodeExecutable` | `node` | Node 命令名或可执行文件路径字符串 | 找不到命令或不可执行时报告配置错误 |
| `zcodeScript` | 未配置 | CLI 脚本路径字符串 | 未配置时检查 `/Applications/ZCode.app/Contents/Resources/glm/zcode.cjs`；其他安装位置显式填写；缺失或目录报错 |
| `dshExecutable` | `dsh` | 命令名或可执行文件路径字符串 | 默认命令不在 PATH 时检查 `~/.local/bin/dsh`；DSH_HOME 沿宿主环境，未设时 `~/.dsh` |

这些字符串无数值单位；显式空值、空白、null、非字符串拒绝。可执行路径不经 shell
解析，不可把命令参数拼进路径。`model` 未设时保留宿主默认，显式值原样选择，不自动换模型。
`reasoningEffort` 仍使用统一枚举：WorkBuddy 支持 minimal/low/medium/high/xhigh/max，
ultra 返回 WORKBUDDY_CONFIG；ZCode/DSH 通过原生配置方法设置，宿主拒绝时保留实际错误。
ZCode/DSH 显式模型选择沿原生当前 provider；无法观察 provider 时拒绝，避免静默改用其他服务。

### 工具边界与恢复

开发/返修/白盒设计按角色获得本地作者工具；读者只获得读工具；产品审查、启动就绪及
冻结 QA 不开放本地作者工具。QA 与嵌套模型继续走引擎授权任务操作。白盒覆盖判断先执行
无工具回合，结果提交成功后才开放作者工具；中断时复用保存的覆盖结果。
WorkBuddy 使用进程级 bypassPermissions、明确角色工具列表、严格单个 MCP 配置并禁用
继承设置/hooks。ZCode 使用原生 yolo 模式与角色工具 allowlist；DSH 使用原生 agent 作用域
工具限制及现有权限服务。没有另建权限框架。

内部入口 `workflow drive --operation-mcp --action-id <id> --native-identity <id>`
提供 stdio 工具服务；或 `--operation-native <request.json>` 处理单次直接工具请求。
二者均需 `--root`、`--run-id` 及已领取动作，不能与 `--operation-request`、`--resume`
或彼此组合。默认关闭，native-identity 默认空；缺失/不匹配绑定被拒绝。它们不领取或
调度新动作，stdout 为机器结果，错误由 stderr/工具错误返回。

工具参数为 `{requestId, kind, caseId, prompt, resume?}`。requestId 必须非空且稳定；
resume 默认 false（布尔值），只有恢复已中断的同一操作时设 true，并保留原 requestId、
kind/caseId/prompt。引擎检查原记录、观察结果和进程，不用新 ID 重跑。这里的 resume 是
工具请求字段，区别于驱动自身的 `--resume`。MCP 与 DSH 直接工具复用同一分发函数。

### 验证状态与维护入口

平台命令构造、原生协议、身份/结果拒绝、用量解析、安装和公共契约使用本地 fixture 验证。
已确认 CLI 可执行不等于真实会话或完整 regular 验收通过。困难/耗时的平台真实集成实跑
按用户决定延期，**未验证，不记 PASS，也不作为本轮 Seal 阻塞**；正式验收变更由主代理
记录，本说明不改正式需求或替引擎作 Seal 决定。延期仅限实跑，不免除三宿主接入实现。

维护入口是 `internal/engine/facade/runtime_context.go` 的 nativeRuntimeAdapter，
平台方法、配置、原生结果与生命周期观察在 runtime_adapter/bridge/result_<host> 文件。
新增宿主复用既有 host 安装登记、任务操作及 cost parser，公共流程不增加平台特判。
