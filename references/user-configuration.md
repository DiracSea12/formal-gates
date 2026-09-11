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
| `model` | 非空模型标识 | Codex 默认 | 传给原生 CLI 的模型选项 |
| `reasoningEffort` | `minimal/low/medium/high/xhigh/max/ultra` | Codex 默认 | 传给原生 CLI；具体模型是否支持由原生 CLI 检验，失败消耗有限执行 attempt |

例如只调整项目 cap 和容量：

```json
{
  "tokenCap": 200000,
  "capacity": 2
}
```

`null`、拼错字段、字符串型数字、负数、非整数及非法枚举会报错，错误包含文件、字段和
修正说明。错误不会把 cap 变成未配置。model/effort 传给 Codex；本配置不承诺任意模型支持
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
