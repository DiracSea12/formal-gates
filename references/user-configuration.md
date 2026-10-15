# 用户与项目设置

模型通过 `settings show` 的原字段序号和 `settings set/remove --field <序号>` 修改配置，
只填写新的语义值；程序保留其他字段并编码原配置文件。用户仍可人工编辑。启动时按字段覆盖，优先级从低到高：默认值、用户
`~/.config/formal-gates/config.json`、项目 `.gates/token-cap.json` 的既有有限 cap、
项目 `.gates/config.json`。旧 cap 输入只接受 `engineDispatchMaxTotalTokens`；新配置
使用 `tokenCap`。同一 loader 校验所有输入，非法、未知字段或 null 明确报错，不静默
禁用额度。删除字段表示继承。

```bash
formal-gates settings show --root <repo>
formal-gates settings set --root <repo> --scope project --field <原序号> --value <新值>
formal-gates settings remove --root <repo> --scope project --field <原序号>
```

| 字段 | 默认与含义 |
| --- | --- |
| `capacity` | 16；1–64 个在途任务。达到上限排队，不判 FAIL。 |
| `qaCapacity` | 4；1–64 个在途 QA 任务，仍受总并发限制。独立其他任务继续推进。 |
| `model` | 未设置则不覆盖原生 CLI 的选择；指定时直接传原模型 ID，不自动换模型。 |
| `reasoningEffort` | 未设置则不覆盖；可写 minimal/low/medium/high/xhigh/max/ultra，由所选宿主核其实际支持。 |
| `tokenCap` | 未设置为 unavailable；非负 token 数。0 停止自动治理调用，继续须原 typed 额度处置。 |

原生路径也是平面 JSON 字段，由对应 adapter 读取：

| 宿主 | 字段与默认 |
| --- | --- |
| Claude | `claudeExecutable`: claude；`claudeHome`: 当前用户 HOME。 |
| Codex | `codexExecutable`: codex；`codexHome`: 当前用户 HOME。 |
| Cursor | `cursorExecutable`: cursor-agent；`cursorHome`: 当前用户 HOME。 |
| DSH | `dshExecutable`: dsh。 |
| ZCode | `zcodeExecutable`: node；`zcodeScript`: 原环境指定或标准 app-server；`zcodeHome`: 原宿主数据 HOME。 |
| WorkBuddy | `workbuddyExecutable`: codebuddy（PATH 不存在时核标准 macOS app bundle）；`workbuddyHome`: 原用户 HOME；`workbuddyProductConfig`: 原桌面 product config。 |

原生 home 留空继承当前宿主环境；显式字段须非空。路径配置只传入对应子进程，不改
进程全局环境，不复制认证文件。WorkBuddy 默认配置根是原 `.workbuddy`；product config
通常位于其 `cache/acc-product-config-v3.json`，安装位置仍为 `.codebuddy/skills`。
WorkBuddy CLI 的 effort 合同为 minimal/low/medium/high/xhigh/max，模型实际支持另由
宿主决定；`deepseek-v4.1-flash` 的官方默认 effort 为 high。

有效值与来源在 `show` 可查。启动时冻结到原 `state.json`，child 继承该冻结值；恢复
不重读修改后的配置，不重置用量、次数、授权或并发。动态 token cap/授权仍由原
cost/guard owner 持有，设置展示只投影其当前有效值。修改配置作用于后续新 run。

运行中的临时执行选择使用从主流程目录派生的执行组，不修改上述用户或项目默认文件。先查询目标
节点与平台的原生选项；节点可写 `show` 的 `executionNodes` 序号、节点 ID 或完整描述。
模型和 effort 可写查询返回的序号或原名称；不需要抄请求 ID、摘要或执行绑定。

```bash
formal-gates workflow execution-options --root <repo> --execution-node development --platform workbuddy
formal-gates workflow execution-options --root <repo> --execution-node development --platform workbuddy --model 2
formal-gates workflow submit --root <repo> --execution-node development --platform workbuddy --model 2 --effort high
formal-gates workflow submit --root <repo> --execution-node development --effort medium
formal-gates workflow submit --root <repo> --execution-node qa-blackbox --platform codex --model 2 --effort high
formal-gates workflow submit --root <repo> --execution-node qa-whitebox --platform workbuddy --model 1 --effort medium
```

同平台只给模型或 effort 时，其他项沿用该节点当前有效选择。改变平台时只传同时
指定的项，未给的模型或 effort 保留省略参数，由新平台原生默认执行；选项查询中的
默认值只展示原生报告的事实，不补造可选项或参数。
临时指令提交完整的 typed 选择后立即返回；正在运行的消费进程继续推进，或随后使用
`workflow drive` 推进。`workflow next` 搭配相同语义参数可先查看完整候选。

选择只作用于该节点以后尚未取得 native owner 的调用；Ready 且未 claim 的调用也可
调整，程序会重取当前材料。已 claim 的调用及其恢复保留原平台、模型、effort、权限
与费用归属。缺少原生程序、adapter、模型或 effort 时，查询或提交直接返回具体问题
和原可选值，不安装、换模型或降档；无效提交不改变既有决定，其他独立任务继续。
重启读取 run 已保存的选择与调用事实。新 child 继承父创建计划当时的选择；已存在
child 不跟随父后续改变，要调整它时用 `--run` 先选中该 child。

`flowNodes` 仍展示产品审查、开发前技术审查、开发、QA、普通/合并门、返修六项流程
开关，执行顺序不变。`executionNodes` 将 QA 展开为 `qa-blackbox`（黑盒 QA）和
`qa-whitebox`（白盒 QA）；两组各自选择平台、模型和 effort，每组内部的设计、独立
审查与执行共用该组选择。其余执行组沿用流程节点 ID。合并 QA 和辅助任务沿本次
run 冻结默认值；已保存的共享 QA 选择仍按原值读取。
