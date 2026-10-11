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
