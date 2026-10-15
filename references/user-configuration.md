# 用户与项目设置

一套命名配置同时保存六项主流程开关、黑盒/白盒 QA 的整组选择、普通门子集，以及
七个执行组各自的平台、模型和推理强度。程序初始只提供“默认”：主流程全部可用，
不选普通黑盒/白盒 QA，普通门全部选取；各执行组沿 run 宿主与全局模型/推理强度。
其他名称由用户提供。并发、token cap、认证和原生路径不打包进命名配置。

通过同一个 `settings` 入口查看、命名保存、修改、启用和删除；只传原选项序号或语义值，
程序编码配置文件。未写 `--configuration` 时操作当前启用的配置。保存复制选中配置，
不会自动启用；已有名称用 `set` 修改。`show` 同时展示名称、来源、当前启用项、流程、
QA 和执行组选项；`show --gates` 读取当前已注册 runtime 的普通门目录。

```bash
formal-gates settings show --root <repo>
formal-gates settings save --root <repo> --scope user --name '<用户命名>' --configuration 1
formal-gates settings set --root <repo> --configuration '<用户命名>' --flow-node 1 --enabled false
formal-gates settings set --root <repo> --configuration '<用户命名>' --qa-mode blackbox --enabled true
formal-gates settings show --root <repo> --gates
formal-gates settings set --root <repo> --configuration '<用户命名>' --gate <原序号>
formal-gates settings set --root <repo> --configuration '<用户命名>' --all-gates
formal-gates settings set --root <repo> --configuration '<用户命名>' --no-gates
formal-gates settings execution-options --root <repo> --configuration '<用户命名>' --execution-node qa-whitebox --platform workbuddy
formal-gates settings set --root <repo> --configuration '<用户命名>' --execution-node qa-whitebox --platform workbuddy --model 1 --effort high
formal-gates settings remove --root <repo> --configuration '<用户命名>' --execution-node qa-whitebox
formal-gates settings use --root <repo> --configuration '<用户命名>'
formal-gates settings delete --root <repo> --scope project --configuration '<用户命名>'
```

`--gate` 可重复，保存普通门任意子集；`--all-gates` 随实际目录选全部普通门，
`--no-gates` 选择空集。QA 模式单独按整组选取。`remove --execution-node` 删除该组
覆盖值，恢复继承。节点开关与 QA 选择不查询原生模型；模型/effort 查询与修改复用
运行中选择的同一原生选项验证。改变平台时不携带旧平台未同时指定的模型或 effort。

命名配置和启用名称保存在既有用户/项目 `config.json` 的 `configurations` 与
`configuration` 字段。用户可手改；同一 loader 校验名称、结构、节点和选择值。
项目同名配置覆盖用户同名配置，项目启用名称覆盖用户启用名称。跨 scope 启用时，
程序先将选中配置的完整内容保存到目标 scope，再启用名称，
使用户配置可在其他项目独立使用；同 scope 和内置默认直接引用原值。删除只移除所选 scope
的保存值；同名较低层值重新显现，“默认”的覆盖值也可删除并恢复继承。删除当前
启用的自定义配置时，该 scope 的启用项回到“默认”。配置内容与来源在新 run 启动时
冻结；后续默认文件变更不自动传播到已运行 run。

`workflow start --configuration <名称或原序号>` 只选择本次启动的配置，不改保存的启用项。
省略该参数时使用 `settings use` 已启用的配置；显式启动节点开关优先于配置中的流程节点选择。
运行中使用同一组语义选项请求更改当前 run，程序从保存配置取真实副本，或基于本次当前有效选择只改指定项：

```bash
formal-gates workflow start --root <repo> --run-id <本次ID> --route regular --intake <原序号> --configuration '<用户命名>'
formal-gates workflow submit --root <repo> --configuration '<用户命名>'
formal-gates workflow submit --root <repo> --flow-node repair --enabled false
formal-gates workflow submit --root <repo> --qa-mode whitebox --enabled true
formal-gates workflow submit --root <repo> --no-gates
formal-gates workflow submit --root <repo> --request SELECT_ROUTE --gate <候选原序号>
formal-gates workflow next --root <repo> --ask <原序号> --choice approve
formal-gates workflow submit --root <repo> --confirmation-candidate <原序号>
```

配置请求使用原 SELECT_ROUTE Ask：展示完整配置、候选门全集和程序生成的原 continuation 影响预览，
明确确认后才生效。更改不写默认文件；同一 run 的整套配置与单执行组选择按确认 revision 生效。
仅改指定节点时保留其他当前 tuple、QA 与门选择；明确选择命名配置时整体采用其保存值。
产品/技术审在途或初次拓扑、路线尚未确认时可改本次配置，不代替原必需的拓扑与开发计划确认。

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

全局设置的有效值与来源在 `show` 可查。启动时冻结到原 `state.json`，child 继承该冻结值；恢复
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

本次 run 可在任意非终态时刻明确选择另一套已保存配置，或修改流程节点、QA/普通门
选择和执行组 tuple；每次经原 typed 确认记录到该 run。流程配置与门集合确认直接展示
具体选项，不提供路线档位。拆分拓扑确认后，原开发计划和具体门集合仍须确认；普通
run 与每个 child 分别落账，child 不选择或执行普通 QA，主线保留合并验证职责。
已 claim 的调用及其恢复保留原执行绑定和费用归属；默认文件与既存 child 不自动跟随。
从已完成或跳过的合法节点继续时，沿现有节点继续入口先展示复用、失效与重新执行的
具体处置，再由用户确认；不会把启用节点或读取命名配置当作需求变更。
