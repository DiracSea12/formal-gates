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
| WorkBuddy | CodeBuddy print/stream-json；请求 session UUID 后等待原生 init 确认；续用同一 session；进程退出留证；恢复观察检查原生 print 会话存储（session/store），实际续用由后续 --resume 确认 | 同 session 的成功 result 和结构化输出；SDK assistant 消息 usage，按 uuid 去重 |
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
| `workbuddyProductConfig` | WorkBuddy 原生产品配置文件 | 非空文件路径字符串；未配置时沿 `ACC_PRODUCT_CONFIG_PATH`，否则用 WorkBuddy 配置目录的 `cache/acc-product-config-v3.json` | 文件由 WorkBuddy 桌面登录/启动维护；不存在则在原生调用前拒绝，不能切换到独立 CodeBuddy 账号或复制 token |
| `workbuddyHome` | 当前进程的用户主目录 | 已存在的目录路径字符串 | 原生桌面认证按用户主目录读取，并非只按 `WORKBUDDY_CONFIG_DIR`。隔离安装可显式选择原生用户主目录；只绑定路径，不复制 HOME 或认证文件。缺失/非目录在启动前拒绝，恢复沿原绑定 |
| `zcodeExecutable` | `node` | Node 命令名或可执行文件路径字符串 | 找不到命令或不可执行时报告配置错误 |
| `zcodeScript` | 未配置 | CLI 脚本路径字符串 | 未配置时检查 `/Applications/ZCode.app/Contents/Resources/glm/zcode.cjs`；其他安装位置显式填写；缺失或目录报错 |
| `zcodeHome` | 当前进程的用户主目录 | 已存在的目录路径字符串 | ZCode 原生 CLI 从该用户主目录读取自己的 `.zcode/cli/config.json` 与认证状态；隔离安装可显式选择原生用户主目录。只绑定目录，不复制配置/凭据；缺失或非目录在启动前拒绝，恢复沿原绑定 |
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
继承设置/hooks。其原生 JSON-schema 输出依赖 `StructuredOutput`；各角色保留这个只返回
结构化结果的协议工具，不因此获得文件、命令、模型或委派能力。原生 success 若只返回
普通工具调用文本而没有合法 JSON，仍不接纳为角色结果。ZCode 使用原生 yolo 模式与角色工具 allowlist；DSH 使用原生 agent 作用域
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
CLI 可执行或本地 fixture 通过不等于真实会话或完整 regular 验收通过。未实测必须保持
未验证，不能记 PASS。某次 run 的真实集成实跑确需延期时，由用户决定并沿 engine
已有的精确 waiver/deferred/skip 合同记录；是否阻塞该 run 的 Seal 由实际处置决定，
不是本产品默认豁免。延期不免除三宿主接入实现，也不能由主代理口头记录代替引擎接纳。

### 维护者：新增和更新平台适配

平台接入以 `internal/host/host.go` 的 `Descriptor` 为唯一能力描述。公共 runtime、
settings、source/recovery、cost、lifecycle 和 hook 响应均从这一描述取能力，不分别维护
平台名单。`host` 只描述平台并解释原生数据，不依赖 engine、settings 或 cost 账本。

描述中填写 ID/别名、安装路径、可编辑配置及默认值/校验、`ReadUsage`、`CheckIdentity`、
`ObserveRecovery`，有真实来源时再提供 `ReadTiming`。需要原生 hook 时提供 `Lifecycle`
以及 `Hook.Response`/`Hook.ExitCode`；没有 hook 的平台不伪造生命周期能力。原生事件名、
载荷字段、身份探测信号和项目级 hook 限制属于平台描述，不进入公共检测流程。

#### 新平台的最少操作

在平台自己的适配文件中构造描述并实现 `nativeRuntimeAdapter`，在同一文件的 `init`
中调用一次 `registerNativePlatform(descriptor, adapter)` 并检查错误。新增平台不需要
修改公共 runtime/source/recovery/cost/settings/lifecycle 的名单或分支。已有安装描述
通过对应平台的 `registerBuiltinRuntime` 接上运行时，不复制描述。平台允许有多个专属
文件；无需为了文件数合并不同协议，也不引入动态插件、热加载或第二套调度。

命令/路径配置使用 `host.StringConfig`，由 `Values.NativeString` 读取。默认值、字段名
和校验仅由平台提供，公共配置仍保留现有扁平 JSON 格式和创建 run 时的有效值快照。
随适配补充该平台的协议样本、测试与使用说明；若发布新的安装目标，还需在 manifest
声明该目标及真实验证状态。这是平台支持声明，不是另一套运行时注册。

传输合同位于 `internal/engine/facade/runtime_context.go` 与 `runtime_execute.go`：

| 平台提供 | 仅负责的内容 |
|---|---|
| `capture` / `validate` | 捕获并校验原生可执行文件、环境与平台配置；恢复仍使用原绑定 |
| `turn(nativeTurn)` | 执行一个原生回合，处理本平台启动/续用/等待及原生结果；不判断下一阶段 |
| `source` / `transcript` / `readIdentity` | 返回真实计量来源或解释已落盘身份，不接纳工作流结果 |
| `probe(nativeProbe)` | 原生恢复查询的方法名、原始响应或真实错误；不自行批准替换或重试 |
| `runModel` / `observeModel` | 本平台的无工具模型调用与已完成调用观察 |

流式平台可嵌入 `streamRuntimeAdapter`，复用资源绑定、来源定位和默认来源采集；用 `runNativeStreamProcess` 保存进程与原始输出，以 `completeStreamModel`/
`observeStreamModel` 处理已有模型回执。不复制这些公共实现，也不自建结果/成本 writer。

`nativeTurn` 只携带本次调用输入，不提供 `Facade`、`Run` 或可写工作流对象。宿主确认
原生身份后调用 `Attached`，随后才提交模型输入；需要引擎任务工具时经 `Operation`
或已绑定的 CLI 工具入口转交。角色允许读写什么由公共 `nativeRoleAccess` 决定，平台
只翻译成自己的工具名、配置和原生权限。`NoTools` 表示白盒覆盖判断：
保留只读工作区工具与 shell，不开放写入或引擎任务操作。Codex 的 `config/read`
解释由其适配层内部处理，其他平台无需实现空的 `toolConfig` 方法。
公共执行器统一完成 CLAIM、白盒前置、结果接纳、恢复衔接和取消；平台不实现
`execute`、`finish`、`reconcile`，也不逐个连接这些工作流步骤。

#### 调用产物与恢复

`nativeTurn.Job` 是公共执行器分配的本次原生调用目录。公共合同只约定实际输入
`prompt.txt`/`result-schema.json`、进程回执 `process.json` 和成功原生答案 `answer.json`。
平台的请求、配置、日志和其他私有产物均放在这个目录内，具体文件名和子目录由平台决定。
进程与来源 helper 负责保存可恢复证据；平台不改 run state，不把请求身份当成已观察身份。

白盒只读覆盖回合使用独立 `coverage/` 调用目录，正式编写使用另一调用目录，并续用
同一个原生身份。公共恢复根据目录和既有 coverage 操作回执识别前置/正式答案，不搬运
平台文件、不维护私有文件清单，也不因平台增加文件而修改公共收尾。中断后先观察原进程
与原答案；已有有效 coverage 不再次执行，也不能作为正式设计结果接纳。取消定位实际
在途调用，终态仍按原资源归属统一清理，正式来源与结果沿原合同保留。

#### 已有平台更新与验证

在既定能力合同内，协议字段、配置、结果格式、用量格式或私有产物布局变化，只修改
该平台的适配、测试和说明，不同步修改其他平台或公共流程。解析器保留仍有效的旧来源
读取能力，例如升级后同一会话的旧计量前缀。真正新增公共能力时才说明合同变化，不能
把扩充公共核心当作每次接入或平台升级的惯常步骤。

最小接入范例是 `runtime_platform_test.go`：一次注册覆盖配置、生命周期、身份、恢复和
计量；模拟协议及私有产物布局升级，未适配版本必须失败，更新平台解释器后保留同身份
续用和旧来源。`runtime_platform_entry_test.go` 通过公开 Start/ExecuteNative/Open
验证进程、身份、结果、计量和中断后采集；`runtime_platform_cycle_test.go` 验证开发
CONTINUE/续用、公共白盒前置与编写自动衔接，私有文件隔离且不覆盖。
`runtime_boundary_test.go` 防止适配器重新依赖工作流所有者或公共代码恢复平台名单。
这些都是本地假平台测试，不是真实宿主支持或独立审查的证明。

```sh
go test ./internal/engine/facade -run '^TestNativePlatform|^TestNativeRoleAccess|^TestStreamCoverageRecovery' -count=1
go test ./internal/host ./internal/settings ./internal/cost ./internal/lifecycle
```

平台协议变化只补跑其受影响测试；公共合同变化才扩大回归。不为新增平台复制整套大型
验收，不为字段更新重跑未受影响的真实模型调用。安装/完整 regular 的支持声明仍须依据
相应实际验证，缺失能力不能用通用 fallback 或主代理手工转交代替。

普通 `workflow drive` 默认持续执行，显式 `--continuous` 仍兼容，并沿用本 run 创建时冻结的配置；
不需要另一份 executor 配置。`capacity` 仍是 engine 的签发容量，不是主代理可在
消费过程中擅自提高的并发/次数预算。WorkBuddy、ZCode、DSH 与 Codex 通过同一
已注册平台合同进入该入口。缺少原生能力或原绑定不可恢复时，返回明确错误，
不会改选另一宿主。外部 ChatGPT executor 必须通过正式交接获得独立的绑定，
不能冒充某个原生平台，也不会被持续入口自动领取或启动。


### 选择宿主和变更执行者

初始宿主来自当前项目适用的 registry 安装记录，workflow start 不接受自行指定的
provider。使用已有 install 的 host/scope/project 选择宿主，先检查其原生可执行路径及
配置；同一个 run 继续使用已冻结设置。不能编辑 state/request/registry 来换宿主。

活动开发批次需要跨平台时，使用 engine-regular 的 executor-handoff 用户确认入口，
指定目标规范 provider 和实际 executor label。目标必须通过统一能力合同；仅安装了
Claude Code 等宿主的技能，不表示它已经具备 regular 原生运行适配。能力不够就拒绝目标，
不会改用 Codex 或 ChatGPT。external ChatGPT 是显式授权的外部开发模式，不是原生支持。

已完成批次的优化和追加使用 development-increment，而不是修改 capacity、清零计数或
伪造开发失败。这些控制无新的配置文件或隐藏执行预算；保留/重跑范围以该次用户 typed Ask
决定为准。持续消费停止不代表取消原生任务，恢复仍先对账；取消走原有 abort 及清理。


WorkBuddy 原生配置目录沿应用的 `WORKBUDDY_CONFIG_DIR`，未设时为 `~/.workbuddy`；
适配器将原生 `CODEBUDDY_CONFIG_DIR` 绑定到同一目录，避免误用独立 CodeBuddy 的登录。
首次捕获的原生产品文件路径随 action 固定，恢复重新检查其可用性，不读取另一账号或
把凭证写进 state。原生登录失败返回 `WORKBUDDY_AUTH`，不是持续等待或语义 PASS。

`workbuddyHome` 只选择 WorkBuddy 原生子进程的用户主目录；其 MCP 引擎工具保留启动方的
安装 HOME、USERPROFILE 和 XDG_CONFIG_HOME，不能因此改用另一份 registry 或 run。
未设置该字段时沿当前用户主目录；设置后仍由 WorkBuddy 读取和验证自己的登录，不保证
文件存在就等于认证有效。已领取动作使用其原 context，不能通过后来编辑配置来替换原生
身份或认证目录；修正配置按正常新授权和恢复合同处理，不手改已有 run 的 settings/context。

### 原生角色工具限制

WorkBuddy 角色回合使用 `--tools default`，以 `--disallowedTools` 禁止原生委派、
改变引擎工作区及非写入角色的编辑/写入和退出只读模式操作。审查角色仍使用 `dontAsk`，
Bash 不预先授权，由宿主逐命令判断；读取、搜索及其他未禁止的默认工具保留。
ZCode 角色创建、同身份恢复及绑定工具后的恢复均传 `toolDenylist`，不发送角色工具白名单；
审查与覆盖判断使用原生 `plan`。DSH 通过现有工具插件传 `deniedTools`，
使用 SDK 的 scoped `restrict({deny: ...})` 和 guard，保留其默认工具及原生 sandbox。
三个宿主的覆盖判断均保留只读 shell；只有冻结 QA 执行禁止直接 Bash，沿已有操作边界执行。
文本辅助模型没有角色工作区职责，仍使用显式空工具集；它不是角色默认工具策略。
这些是内部角色映射，不新增用户配置或权限系统。真实模型宿主验收的延期状态不因此改变。
