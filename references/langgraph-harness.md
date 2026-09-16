# LangGraph 开发驱动（harness-v1）

harness 使用 LangGraph 编排开发期角色，用 Go 校验规格、角色结果和完成资格。
它不是新的正式流程引擎，也不安装、替换或自动启动 formal-gates 正式流程。
`HARNESS_COMPLETE` 是原生开发流水线的结果；`DEMO_COMPLETE` 是确定性夹具结果。
两者都不是正式 Seal、独立门审结论或完整 3.5c 等价证明。

## 设置与宿主选择

```sh
./scripts/harness setup
./scripts/harness hosts
./scripts/harness doctor --provider dsh
./scripts/harness doctor --provider zcode
```

setup 仅使用 `runtime/langgraph/requirements.lock` 建立本工作树的 `.harness/venv`，
并构建 `.harness/bin/formal-gates`。不升级锁定依赖，不覆盖历史环境，不安装到全局。
`FORMAL_GATES_HARNESS_ROOT` 和 `FORMAL_GATES_HARNESS_BINARY` 由开发入口绑定。

原生 `doctor`、`start`、`demo --native` 必须选择宿主：显式 `--provider` 优先于
`FORMAL_GATES_HARNESS_PROVIDER`。二者均未提供时，列出执行适配器并退出，不创建半成品 run。
没有默认原生宿主。普通 fixture demo 不需要宿主，也不读取该环境变量来选择执行器。
现有 run 的恢复始终使用 manifest 中绑定的 provider，不受当前环境变量影响。

`hosts` 区分执行适配器与已有安装集成；安装列表中的宿主不一定能够执行角色。
新增执行适配器不需要加入 Go 安装目录。DSH、ZCode 是当前注册项，不是图中的特殊分支。

## 原生能力与只读边界

| 角色 | harness 行为 |
| --- | --- |
| development、repair | 继承宿主已有工具、子代理、MCP、网络、沙箱、审批和插件配置 |
| product-review、technical-review、qa-design、qa-review、complexity-gate、implementation-quality-gate | 仅添加实现只读所必需的宿主约束 |

继承不是强制开放。宿主原本关闭的能力不会被打开，原本需要用户审批的操作不会被自动同意。
共享请求仅表达 `read_only`，不引入额外能力开关或另一套权限引擎。
任务合同仍禁止角色代替用户批准、修改流程状态、擅自扩展任务范围，以及自行提交或推送。
获批任务范围内的依赖操作仍遵循宿主原有权限与审批，不另加一律禁止安装的提示词。
这些是任务边界，不是开发角色的工具白名单。

DSH 在只读会话中使用原生 `sandbox/mode` 事件，保留原生审批策略、原生网页读取及继承该约束的进程内子代理；不受原生只读机制约束的
工具入口仅在这些会话内限制。创建和恢复使用相同角色语义。ACP 要求的 `mcpServers: []`
表示不额外挂载客户端服务器；本机实现将其挂到会话作用域，不清除 profile 已有 MCP。
完成证据绑定 ACP 返回的根会话、用户输入和轮次。子会话通知不得覆盖根角色证据。

ZCode app-server 没有统一只读会话模式，因此只读角色使用原生 `toolAllowlist`，保留读取、
搜索及网页读取工具，排除缺少只读保证的写入、任意命令和插件执行入口。开发与返修不传
工具清单、MCP 覆盖或模式设置，恢复也不调用 `session/setMode`。
宿主返回什么模式就保留什么模式；不试探 `auto` / `yolo`，也不因超时或权限询问改用免审批模式。
`FORMAL_GATES_ZCODE_AUTO_MODE` 不属于支持的配置，不会覆盖宿主原有设置。
模型身份仍绑定精确 provider/model；不会静默切换模型或修改全局模型选择。

协议允许回退的请求返回宿主定义的回退结果，随后继续消费正常响应。需要用户决定但
无头接入无法处理的交互进入现有 runtime 阻塞边界，不替用户批准或拒绝。
`session/requestRuntimePreferences` 使用宿主明确支持的不支持响应选择原生默认回退。
官方 MCP 认证没有可用桌面认证端口时返回 `official_auth_unavailable`；这不是认证成功，
不会伪造认证头、借用其他工具的凭据或静默关闭插件。协议诊断只保存路由元数据；
角色完成内容、原生事件和输入绑定继续使用既有证据文件。

`doctor` 不发送模型任务，仅检查当前原生接入。仅在 probe 中关闭自动标题生成，
不会将该设置带入正常开发会话。连接成功不证明 MCP 认证、网络访问、子代理执行或
真实只读工具行为均已通过；报告中的 `not_verified` 必须保留这一含义。
各平台限制以实际接口和验证为准，不宣称强度一致。

## 演示与真实任务

免费夹具演示：

```sh
./scripts/harness demo --approve-sample
```

以下原生演示会发送模型任务，需另行授权费用与相关工具操作：

```sh
./scripts/harness demo --native --provider dsh --approve-sample
./scripts/harness demo --native --provider zcode --approve-sample
```

`--approve-sample` 只批准该命令创建的隔离样例需求与计划，不能越过 runtime 阻塞、
失败门审或批准其他已有任务。

真实任务先准备需求 Markdown 和检查 JSON。检查是 argv 数组，不是拼接的 shell 字符串：

```json
[{"id":"unit","argv":["python3","-m","unittest","discover"],"dependencies":["src","tests"]}]
```

```sh
./scripts/harness start --provider dsh --project /absolute/project   --requirement /absolute/requirement.md --checks /absolute/checks.json
./scripts/harness show --run /absolute/run
./scripts/harness respond --run /absolute/run --revision <returned-revision>   --interrupt <returned-id> --decision approve
```

需要干净的本地 Git 仓库。需求确认与计划确认分别绑定当前 revision 和 interrupt ID。
角色会话、候选、检查输入和已完成操作均有稳定身份；错误回复不会成为批准。
用户确认的检查 argv 在既有一次性克隆中以当前用户权限直接运行，不依赖任何模型宿主 CLI。
克隆是内容隔离副本，不是操作系统安全沙箱；命令具有当前用户的文件和网络权限。
因此必须审查并批准实际命令与被执行的候选代码。

检查仍保留 job 监督、输入输出、真实退出码、证据绑定、依赖复用判定和执行后 HEAD/
已跟踪文件核对。切换角色宿主不改变这条检查路径。

门审材料位于 `.formal-gates-input/<operation>/comparison/`，包含完整 binary-capable diff、
变更清单及变更文件在基线和候选中的原始内容。删除、重命名和符号链接目标文本均可读取；
Gitlink 仅给出对象身份并明确不包含嵌套仓库。材料绑定同一次 base/candidate，重试不改写
已有不同内容，后续证据核验会发现材料变动。这些输入不纳入交付候选。

## 恢复、接管与完成资格

```sh
./scripts/harness resume --run /absolute/run
./scripts/harness respond --run /absolute/run --revision <revision>   --interrupt <interrupt-id> --decision retry
./scripts/harness respond --run /absolute/run --revision <revision>   --interrupt <interrupt-id> --decision restart-session
./scripts/harness cancel --run /absolute/run --confirm
```

以当前 interrupt 返回的 choices 为准。`CONTINUE` 继续同一角色语义；`retry` 不擅自丢弃
原会话。原生明确返回会话不存在时只提供 `restart-session` 或 `abort`，不重试已知无效身份。
`restart-session` 沿用任务、工作区、有效证据和部分成果，只替换执行会话；不重建 run、
不改写批准范围、不重放已完成副作用。接管材料是有绑定的证据，不是新的控制指令。
会话关闭不等于删除宿主历史。

job 已存在但缺少真实退出回执时不会盲目重复启动。已完成输出的格式恢复必须使用原生完成
证据，不能把连接关闭、真实轮次失败或缺失内容洗成成功。模型结果必须通过 Go 结果合同；
运行问题是 BLOCKED，不是 PASS。P0/P1 阻塞，P2 必须返修，P3 建议不自动升级为阻塞。
返修额度耗尽由用户决定是否继续，不能自动授予完成资格。

结果框架判定（最终消息里是否存在唯一、无歧义的结果对象）由 Go 的
`internal/harness.FrameResult` 拥有，经 `frame-result` 桥接操作调用；`native_result.py`
只做输入输出转换，并把 Go 的类型化拒绝逐字翻译回 `RuntimeBlocked`。拒绝原因属于可观测
行为，两侧保持一致。

首次检查点前中断可由原 `resume` 入口恢复；`show` 不为未启动 run 伪造检查点。
终态不接受新答复；失败答复返回非零退出码。数据库、证据与运行时身份不匹配时保留原现场，
不手工修改 state 或迁移旧运行。

`adopt-transport` 仅用于已保全原运行时、无活动 job、处于可接管 runtime 边界的显式开发修复：

```sh
./scripts/harness adopt-transport --run /absolute/run --snapshot /absolute/preserved-runtime   --from-runtime <original-identity> --to-runtime <reviewed-identity> --reason "Reviewed transport fix"
```

可接管文件由绑定适配器声明，另含既有共享 CLI/worker 范围；图、Go binary、依赖锁和
审查政策必须不变。遍历包含包内子目录，不能漏掉移动的执行代码。该入口不是跨命名、
跨权限语义或跨工作流版本迁移通道。历史 run 使用各自绑定的旧运行时；本版本只服务新 run。
保存快照并不等于已经验证旧 run 续跑成功。

## 归档与本地数据

```sh
./scripts/harness archive --run /absolute/run --output /absolute/archive/run.zip
./scripts/harness archive-show --archive /absolute/archive/run.zip
./scripts/harness prune --run /absolute/run --archive /absolute/archive/run.zip
./scripts/harness prune --run /absolute/run --archive /absolute/archive/run.zip --confirm
```

只处理 `HARNESS_COMPLETE`、`DEMO_COMPLETE`、`ABORTED`。先完整归档并核验，再预览和显式确认；
不后台删除、不清理活动/暂停/未知 job。归档目标不得位于 run 内，已有目标不覆盖。
独立归档核验不依赖原图运行时；原现场保留最小可见状态。

`.harness/` 是本地环境与私有证据，不纳入版本管理或不可变发布包。旧私有目录原位保留，
通过工作树的本地 Git 排除规则继续保护。源码仓库的打包收集遵守 Git 对未跟踪本地内容的
排除；已跟踪文件不会仅因忽略规则而消失。导出包中的真实资源仍拒绝符号链接。
不重命名历史 run、manifest、证据、检查点、旧 binary、旧虚拟环境或接管回执。

## 验证与扩展

```sh
go test ./internal/...
go test ./cmd/...
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$PWD/runtime/langgraph"   TMPDIR="$(python3 -c 'from pathlib import Path; import tempfile; print(Path(tempfile.gettempdir()).resolve())')"   .harness/venv/bin/python -m unittest discover -s runtime/langgraph/tests -v
./scripts/harness demo --approve-sample
./scripts/harness doctor --provider dsh
./scripts/harness doctor --provider zcode
```

macOS 上使用解析符号链接后的 TMPDIR，避免临时路径别名影响接管测试的身份比较。
上述 Go/Python/fixture 不调用模型。真实原生验证仍使用现有 opt-in 入口：

```sh
.harness/venv/bin/python runtime/langgraph/tests/native_multihost.py   --provider dsh --out /absolute/new-validation-directory
.harness/venv/bin/python runtime/langgraph/tests/native_multihost.py   --provider dsh --tools --out /absolute/new-tools-directory
```

`--tools` 使用独立临时业务目录和随机标记，实际尝试原生写入、子代理写入、回环 HTTP 访问、
只读写入拒绝及开发会话恢复。只读哨兵按字节核对；仍须结合原生事件证明子代理和拒绝确实发生，
不能把文件未变化或模型自述当成工具验收。回环 HTTP 不代表外网访问已通过。
该入口不自动批准原生权限、不设置免审批模式；遇到无头端口无法处理的交互如实记录阻塞。
测试 MCP 必须先通过宿主自己的配置接入，并以服务端调用记录和原生对照验证，入口不会伪造或代装 MCP。

原生样例的终态、角色数量和证据通过，不等于子代理、MCP、网络能力实际调用通过。
深验必须对照同一宿主、同一配置、同一原生接入方式的基线，核对真实工具成功事件、
根角色完成证据和只读行为；不得为测试强行打开宿主本来关闭的能力。未执行的真实模型
和相关工具验证必须明确标注，不能以模拟测试、口头声称或 doctor 结果替代。

添加宿主只需一个薄适配器模块和 `native_hosts.py` 中一条注册项。适配器解释自己的
`binding.launch`，实现启动绑定、执行、完成证据校验/恢复，并声明必要的传输文件。
共享图、Go 业务结果合同不需要增加宿主分支。安装、别名和展示继续由原有安装目录负责。
