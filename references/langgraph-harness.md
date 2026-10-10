# LangGraph 开发驱动

harness-v1 用 LangGraph 推进开发角色，Go 接纳语义结果和完成资格。`HARNESS_COMPLETE`
报告原生运行结果，`DEMO_COMPLETE` 报告确定性夹具结果；harness 不安装或启动正式 engine。

## 准备与启动

```sh
./scripts/harness setup
./scripts/harness hosts
./scripts/harness doctor --provider dsh
```

setup 建立本工作树的锁定环境并构建 Go 驱动。原生调用显式选择 `--provider`，也可由
`FORMAL_GATES_HARNESS_PROVIDER` 提供；已有 run 使用其原宿主绑定。doctor 不调用模型，
仅报告当前接入能力。

真实任务使用需求 Markdown 和原创检查配方；检查由程序执行：

```json
[{"id":"unit","argv":["python3","-m","unittest","discover"],"dependencies":["src","tests"]}]
```

```sh
./scripts/harness start --provider dsh --project /absolute/project \
  --requirement /absolute/requirement.md --checks /absolute/checks.json
./scripts/harness show --run /absolute/run
./scripts/harness respond --run /absolute/run --decision approve --save-decision /absolute/decision.json
./scripts/harness respond --run /absolute/run --decision-file /absolute/decision.json
```

展示当前需求或计划后取得用户决定，程序生成完整候选；确认后提交原文件。
图持有顺序、派发、轮次、检查、恢复和终态。完成依赖原生证据与批准检查的实际结果。
fixture 演示不调用模型：

```sh
./scripts/harness demo --approve-sample
```

## 角色材料与工具

角色通过 `workspace` 的目录、条目和读取编号访问原文件。`.formal-gates-input` 只展示
当前角色的需求、任务、审查准则及必要反馈；材料只读。门审材料含完整比较及两端原内容。
任务的身份、版本、物理路径和原生收据由程序持有。

替换使用原读取编号和行范围，只提供新内容。复制/移动使用原来源 listing/entry、目标
目录 targetListing/targetEntry 和原创新文件名；省略目标目录时使用工作区根。程序处理
原文件字节或原生重命名，仅处理文件，不递归复制目录。工具沿用原执行接纳和只读约束。

角色返回语义判断。finding 的 `locations` 只选原 read 或 listing/entry 数字；Go 验真
同一原生 source 中的工具证据后装配实际路径。QA 按批准顺序给出覆盖与 oracle 证据。
未完成工作用 `remainingWork`，运行时受阻用 `interruptionReason`；状态与结果身份由程序生成。

DSH 使用原 profile，ZCode 使用 MCP 与 toolAllowlist，Claude 使用 MCP 工具配置，
Cursor 使用每任务插件与权限钩子，Codex 使用 dynamicTools 与原工具钩子。各适配器只
处理原生注册、会话、模型选择、执行和证据传输。实际能力以本次接入及验证为准。

## 原生交互与恢复

需要用户回答的原生 SDK 交互展示问题和数字选项。原请求、选项、问题身份与完整协议
载荷保存在 pending 证据中；程序按当前选择装配候选，确认前不送出答案：

```sh
./scripts/harness native-show --run /absolute/run
./scripts/harness native-respond --run /absolute/run --question 1 --choice 1 \
  --save-decision /absolute/native-decision.json
./scripts/harness native-respond --decision-file /absolute/native-decision.json
```

多问的 `--responses` 按展示顺序提供数字选择或真正自由文字：

```json
[{"choices":[1]},{"text":"说明内容"}]
```

表单的 `--values` 按展示字段顺序提供原创值，枚举字段使用 `{"choice":1}` 数字选择；
`--cancel` 取消当前问题。提交校验原
pending 的完整绑定，再由原 SDK reply 构造器回传。陈旧候选不重新绑定到其他问题。

```sh
./scripts/harness resume --run /absolute/run
./scripts/harness respond --run /absolute/run --decision retry --save-decision /absolute/retry.json
./scripts/harness respond --run /absolute/run --decision-file /absolute/retry.json
```

恢复读取原执行事实并沿同一接纳路径继续；会话确实不可恢复时，当前问题提供
`restart-session` 或 `abort`。新会话使用程序保存的原任务、部分成果与必要交接材料。
`cancel --confirm` 只通知原 owned jobs，后续 resume 观察实际结果。

## 归档与验证

终态 run 可 `archive`，核验归档后通过 `prune --confirm` 清理；活动或未知 job 不清理。
`.harness/` 承载本地环境和私有证据，不进入发布包。

真实模型验证需当次授权：

```sh
.harness/venv/bin/python runtime/langgraph/tests/native_multihost.py --provider dsh --out /absolute/new-validation
.harness/venv/bin/python runtime/langgraph/tests/native_multihost.py --provider dsh --tools --out /absolute/new-tools
```

`--tools` 核查原生受管 copy 的实际调用、原字节、原会话 resume，以及同一工具服务的
只读拒绝。固定回环 fetch helper 由程序执行，保存真实过程与响应；模型不抄 endpoint
或脚本命令。它不声明任意 shell、子代理或外网能力。

未执行的宿主实机验证保留未验证状态。模拟测试、doctor 和其他宿主结果不能替代实际
宿主证据。宿主扩展在 `native_hosts.py` 注册薄适配器，复用同一工具、结果和恢复归属。
