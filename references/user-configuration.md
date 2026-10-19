# 用户与项目设置

一套命名配置同时保存六项主流程开关、黑盒/白盒 QA 的整组选择、普通门子集，以及
七个执行组各自的平台、模型和推理强度、五项自动轮数上限。程序初始只提供内建“默认”：主流程全部可用，
不选普通黑盒/白盒 QA，普通门全部选取；各执行组沿 run 宿主与全局模型/推理强度。
其他名称由用户提供或复制时自动生成。并发、token cap、认证和原生路径不打包进命名配置。

当前提供配置库与生效合同的 CLI 入口。图形面板、MCP `configure` 与 `formal-gates config`
在第二阶段接入，尚不可调用；受理选择现在通过对话和 `settings` 完成。

## 配置库与编辑

通过同一个 `settings` 入口查看、命名保存、修改、启用、重命名和删除；只传原选项序号或语义值，
程序编码配置文件。未写 `--configuration` 时操作当前启用的配置。保存复制选中配置，
不会自动启用；已有名称用 `set` 修改。`show` 同时展示名称、来源、当前启用项、流程、
QA、执行组、轮数与 `revisions`；`show --gates` 读取当前已注册 runtime 的普通门目录。
内建“默认”不可编辑、覆盖、删除或改名；需要修改时先 `save` 为另名副本。

`save` 不传 `--name` 时自动使用“原名 副本”，重名依次使用“原名 副本 2”、“原名 副本 3”。
名字在同一个双层锁内按合并配置库避重并保存，命令输出展示实际创建名。
显式空白名字或已有名字仍拒绝；复制内建“默认”同样适用，不改变启用指针或任何 run 绑定。

```bash
formal-gates settings show --root <repo>
formal-gates settings save --root <repo> --scope user --name '<用户命名>' --configuration 1
formal-gates settings save --root <repo> --configuration '<用户命名>'
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
formal-gates settings rename --root <repo> --scope project --configuration '<用户命名>' --name '<新名字>'
formal-gates settings delete --root <repo> --scope project --configuration '<新名字>'
```

`--gate` 可重复，保存普通门任意子集；`--all-gates` 保存 `Gates=nil`，表示全部普通门
（包括将来新增），与显式枚举当前全部门不同；`--no-gates` 保存空集。合并门不在普通门勾选项中。
质量门职责整体关闭时，普通门、分片门、合并门、返修成员门均不派发。
QA 模式按整组选取；`flowNodes` 的 `qa` 总开关关闭时优先显示两组均关闭，
即使文件中还留有 QA 模式。用 `--qa-mode` 启用任一组时会规整总开关与模式字段，
关闭最后一组同时关闭 QA 总开关；`--flow-node qa` 是两组的总开关。一次 `set` 只编辑一个类别。
`remove --execution-node` 删除该组覆盖值，恢复继承。节点开关与 QA 选择不查询原生模型；
模型/effort 使用原生选项验证，不能凭空补造型号或强度。

命名配置和启用名称保存在既有用户/项目 `config.json` 的 `configurations` 与
`configuration` 字段。用户可手改；同一 loader 校验名称、结构、节点和选择值。
项目同名配置覆盖用户同名配置，项目启用名称覆盖用户启用名称。创建、复制或改名时，
新名字不能与合并视图中已有名字重复；文件内遮蔽内建“默认”的条目加载时忽略。
编辑写入 `--scope` 指定层（缺省 project）；删除、改名时须选配置的源层，仅改变该层；
目标层与合并视图中的来源层不符时拒绝且不写入，即使目标层有被遮蔽的同名配置。
例如项目层 A 改为 B 后，用户层被遮蔽的 A 重新浮出，按名字 A 读取它属于预期行为。
删除自定义配置时，该文件中指向它的启用项回落到内建“默认”；改名同时更新该文件中的对应指针。

“默认配置”指启用指针 `configuration` 的目标，不等于内建“默认”。
`settings use` 缺省写项目层，只重写指针，不复制跨层配置，且校验目标在合并配置库中存在。
悬空指针不静默回落；可在其他配置内容合法时，直接执行 `settings use --configuration <有效名称>`
修复指针，即使此前 `show` 因指针悬空失败。坏 JSON、未知字段等内容问题仍须先修正。
保存、编辑和 use 都不自动改变任何已有 run 的绑定名。

未结束 run 绑定的配置可编辑、复制，不可删除或改名；即使删除后会浮出同名低层值，
正规删除入口仍拒绝。绑定名单只计引擎能正常打开的未结束 run，child 归入 master，
打不开或损坏的 run 不计入。若在文件中移除遮蔽层，同名低层值浮出后 run 仍按名字现读。

### 写入修订校验

`settings show` 返回用户层与项目层文件内容的 SHA256，位于 `revisions.user` 和
`revisions.project`；文件不存在时为 `""`，不是字段级差异。写入时可重复传
`--revision scope=sha256`，缺失文件使用 `--revision 'user='` 或 `--revision 'project='`。

```bash
formal-gates settings set --root <repo> --configuration '<用户命名>' --round-limit repair --value 5 \
  --revision 'user=<所读user哈希>' --revision 'project=<所读project哈希>'
```

所有 settings 写操作共用 `editOverlay`：按用户层→项目层顺序持有双 overlay 内核锁，
在锁内读取并比对携带的两层修订，直到写入完成才释放；
任一不符拒绝写入，重新读取后再决定如何保存；不提供字段 diff。未携带修订时不作该比对。
要保护整个所读合并视图，须同时携带两层哈希，不能只带写入层的哈希。

## 自动轮数上限

`roundLimits` 随命名配置保存，五个键均缺省为 3、范围 1–64；1 表示第一个自动轮后
再加轮须用户授权。`show.roundLimits` 展示原序号、有效值及是否使用默认值。

| 键 | 计数口径 |
| --- | --- |
| `product-review` | 产品审查轮数；跑满后每加一轮须授权。 |
| `tech-review` | 开发前技术审查轮数，独立于产品审查。 |
| `qa-blackbox` | 黑盒设计↔审查返工的连续 FAIL 上限；一轮为一次设计返工，达到上限锁定等待用户决定。 |
| `qa-whitebox` | 白盒设计审查同口径；合并 QA 的设计审查上限跟随此值。 |
| `repair` | 最大自动返修轮数，实际计入“验证波次数 + 每批业务返修轮次（按门链深度计）”，不只是 wave 数；验证波次与返修轮次各计一。 |

```bash
formal-gates settings set --root <repo> --configuration '<用户命名>' --round-limit product-review --value 5
formal-gates settings remove --root <repo> --configuration '<用户命名>' --round-limit product-review
```

`remove --round-limit` 恢复该键默认 3，不是禁用职责。上限按（值，生效序号）追加区间账；
series/wave 创建时记当时值，再跑一轮时读最新值。降低上限不倒判已完成轮次，已授权轮不收回；
历史合法性按各轮执行时的上限核对。当前格式在创建时写齐区间账，派发记录原轮身份，
重试与替换沿用原轮的上限和授权，不在加载时补旧账。

## 全局标量设置

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
| `panelIdleTimeout` | 300 秒；整数 0–86400，0 表示不超时。供受理、运行中与配置库管理面板共用，非 worker 超时。 |
| `panelSelection` | true；布尔值，只决定受理选择是否默认弹面板，不替代启动确认。 |

两项 panel 设置已加入全局标量编辑路径，不属于命名配置；也可按字段名编辑：

```bash
formal-gates settings set panelIdleTimeout 600 --root <repo> --scope project
formal-gates settings set panelSelection false --root <repo> --scope project
formal-gates settings remove panelSelection --root <repo> --scope project
```

面板消费这些设置将在第二阶段接入：超时按页面最后一次用户操作计算，丢弃未保存修改并关窗；
受理超时将使用启用指针目标，但仍须 `confirm_start` 真人确认。关闭 panelSelection 时受理改走
对话选择，不禁用用户主动呼出面板的能力。当前修改这两项不会打开窗口或启动 run。

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

运行执行所需的全局标量有效值与来源在 `show` 可查。启动时冻结到原 `state.json`，child 继承该冻结值；恢复
不重读修改后的配置，不重置用量、次数、授权或并发。动态 token cap/授权仍由原
cost/guard owner 持有，设置展示只投影其当前有效值。这些执行默认值的修改作用于后续新 run，
不等于下述命名配置的现读语义；panel 字段是面板设置，不控制 worker 的运行与恢复。

## 执行组

节点可写 `settings show` 的 `executionNodes` 序号、节点 ID 或完整描述；用
`settings execution-options` 查询目标平台的原生选项。模型和 effort 可写返回的序号或原名称；
通过 `settings set --execution-node` 保存到绑定配置，不需要抄请求 ID、摘要或执行绑定。

同平台只给模型或 effort 时，其他项沿用该节点当前有效选择。改变平台时只传同时
指定的项，未给的模型或 effort 保留省略参数，由新平台原生默认执行；选项查询中的
默认值只展示原生报告的事实，不补造可选项或参数。
缺少原生程序、adapter、模型或 effort 时，查询或保存返回具体问题和原可选值，
不安装、换模型或降档；无效保存不改变库内原值。

`flowNodes` 展示产品审查、开发前技术审查、开发、QA、普通/合并门、返修六项流程
开关，执行顺序不变。`executionNodes` 将 QA 展开为 `qa-blackbox`（黑盒 QA）和
`qa-whitebox`（白盒 QA）；两组各自选择平台、模型和 effort，每组内部的设计、独立
审查与执行共用该组选择。其余执行组沿用流程节点 ID。合并 QA 的执行选择优先跟随
启用的白盒组，否则跟随黑盒组，两者都关则不执行合并 QA；不提供共享 `qa` 执行组覆盖。

## 绑定与运行中生效

启动用 `confirm_start.configuration` 选择保存配置（省略取启用指针），不改变指针；
run 固定绑定这个名字，child 继承名字并同样现读，不能运行中换绑。执行行为由保存配置的启用节点决定，
详见[启动前受理](formal-flow.md#启动前受理)。初始路线仍确认精确拓扑与开发计划，
不逐 run/分片选择门、QA 或节点；批准时从库当前值重算。

配置库是节点、QA、门集合、执行选择和轮数上限的唯一权威。启动记录初始应用值，
以后每次派发评估按绑定名读取当前值；变化及字段级新值、未完成工作的复用/重做处置记入
按时间排列的应用值历史，派发保存实际使用值。历史是应用记录，不是用户决定快照，也不伪造批准。
同名配置被多个 run 绑定时，编辑它影响所有这些 run；保存到另一名字则不影响本次 run。

保存即生效指下一次派发评估，不是写入瞬间。已签发未领取派发作废或重取材料并改绑新值；
已领取调用（含恢复）保留原平台、模型、effort、权限与费用归属，按原配置跑完。
关闭节点时记录“待生效/关闭中”，待已领取调用结束才关闭，不撤回调用；此后不再派该职责。
配置字段相关待决 Ask 批准时按库当前值重算，选项失效则作废重发；不另设运行中配置或执行选择 Ask。

父流程 claims 未定、正在对账分片输入时，改变验证路线的配置推迟评估；
分片已经进入主线则在其应用值历史记“已越过”，不实际应用，剩余义务由 master 的合并 QA/门/返修承接。
已完成或跳过节点的重新执行仍须原节点继续、reset 或需求变更确认；编辑配置不等于回退。

读库瞬态失败可沿用上次应用值并在下次评估重试；持续读取失败、坏 JSON/未知字段、
绑定名不存在、启用指针悬空或没有任何启用且仍欠派发的节点时，须停止新派发并通过
Next/Show 报告具体原因，不静默换配置。已领取结果仍正常接纳。修好库、补回绑定名、
用 `settings use` 修复指针或恢复有未完成义务的节点后，再 `workflow drive` 评估恢复。
文件 I/O 首次失败的引擎评估沿用上次应用值；下一次评估仍失败即停止新派发，失败事实跨重新打开运行保留。同一次评估内部的多次读取不累计为多次失败；Next/Show 只读报告原因，不写状态。读库成功后自动清除失败事实并恢复，无需额外授权。

只支持当前格式 run：创建时写齐绑定名、初始应用历史和轮数区间账。同版本、同安装身份内
支持正常中断恢复与精确事件重放，保留原调用的生命周期、费用、权限及真实批准决定的工作来源与轮次授权。
旧格式状态和跨安装重装或升级的接管不受支持，沿现有版本与身份检查明确拒绝，不迁移、不删除原文件。
