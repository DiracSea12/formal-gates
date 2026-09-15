# LangGraph 开发期自狗粮驱动（dogfood-v1）

本入口从 3.5c 封板 `517de91f1b480ec969bd91385c29ce38d8bc2841` 开发。
LangGraph 是**开发工具**，用于先让程序自动执行小型开发任务，再辅助开发独立 Go 引擎。
Go 产品的普通命令、构建和安装不需要 Python。不要把本工具展示为已经完成的独立 Go 编排引擎。

## 能力与边界

当前支持本地 Git、一个非分片任务、可选择的 DSH / ZCode 原生执行适配、产品与技术审、需求及计划确认、
开发与基线 QA 覆盖设计/审查并行、候选提交、批准命令与两门并行、返修循环、证据保存和恢复。
原生会话独立；开发返修续用其实际会话。图决定下一步，模型只返回当前角色的结果。

这是小范围开发 profile，不等价于完整 3.5c/4/4a 正式 regular：当前检查命令由调用者事先提供，
QA 代理审查其覆盖而不是交付完整增量测试用例体系；尚无分片、多 VCS、在途需求修订、完整白盒
设计、门审 carry 全语义和正式安装切换。未做完的领域规则后续进入独立 Go 引擎，不能冒称已覆盖。

实际成功终态为 `DOGFOOD_COMPLETE`，确定性模拟为 `DEMO_COMPLETE`；两者都不是旧流程的
正式 Seal，也不写旧 `.gates/state.json` 或旧封板结果。不复用/修改 4a 活动 run。

## 设置（本机 macOS 已作为验证目标）

要求已有 Go、Python 3.11+、uv，以及已配置模型的所选宿主（DSH 或 ZCode）。仅在独立开发工作树中执行：

```sh
./scripts/dogfood setup
./scripts/dogfood hosts
./scripts/dogfood doctor --provider dsh
./scripts/dogfood doctor --provider zcode
```

setup 把锁定的 Python 依赖装在本工作树 `.dogfood/venv`，把 Go 开发 binary 构建在
`.dogfood/bin/formal-gates`。不修改全局包、宿主配置、稳定 formal-gates 安装或用户付款渠道。
LangGraph 及 SQLite 包版本固定在 `runtime/langgraph/requirements.lock`；不是每次启动联网更新。
`doctor --provider HOST` 只做所选宿主的原生初始化、建立/关闭探测会话，不发送模型 prompt；显示实际选中的模型、推理设置及执行文件绑定。注册或探测成功不替代真实执行验收。

DSH 使用现有 ACP profile、凭据和模型。每次子进程通过局部 patch 使用原生只读/工作区写入
sandbox，并由 DSH 自身的工具 guard/restrict 限制角色工具；不会启用副代理、旧流程 hook、
用户额外 MCP 工具或更换模型。用量缺失明确为 null，不填零。

目前实机目标是 macOS；没有宣称 Windows/Linux 安装、权限和取消行为已验证。

## 已落地的 LangGraph 多宿主接入

这部分已经是现有开发驱动的实现，不是后续 Go 引擎计划中的“再接第二个平台”。Go 开发应冻结
本轮交付基线，复用它的宿主合同与回归；不继续把执行器写死为 DSH。当前所有角色仍由同一张
LangGraph 图驱动，不是每个平台复制一套图、审批、返修或状态库。

```sh
# 名称和入口集成信息来自原有 Go internal/host 注册表。
./scripts/dogfood hosts
./scripts/dogfood doctor --provider zcode
# 相同公开入口、不同宿主；该参数在创建 run 时绑定，不能在 resume 时偷换。
./scripts/dogfood start --provider zcode --project /absolute/project \
  --requirement /absolute/requirement.md --checks /absolute/checks.json
./scripts/dogfood demo --native --provider zcode --approve-sample
./scripts/dogfood demo --native --provider dsh --approve-sample
```

`hosts` 区分已有安装/hooks入口集成与实际执行适配。DSH、ZCode有本开发profile的执行适配；
Claude Code、Codex、Cursor保留已有入口信息，但未在本轮冒称已实现其LangGraph原生worker。
原名 `deepseek-harness`、`dsh` 和 `z-code` 等别名按既有注册表归一化；未知宿主和只有入口
支持的宿主在新run/worker启动前明确拒绝。一个run绑定一个执行宿主和原生模型，不做自动价格路由、
跨宿主热切换或静默模型替换。`fixture`只用于显式标记的测试/演示。

`native_hosts.py`注册执行适配；`native_entry.py`运行单次原生调用；Workers继续负责统一任务、
尝试、交接和Go结果资格校验。平台只负责执行、原生事实与结果材料，不拿图对象或改检查点。
`native_result.py`共用严格结果解码；公共Store校验适配器输出的统一文件证明，不按DSH日志结构分支。
新增适配只需新增对应模块及一项注册、配套合同/原生验证；图和工作流语义不必改动。

### ZCode 的具体传输与权限

ZCode使用已安装的原生app-server协议。默认通过PATH的node启动macOS应用内的
`/Applications/ZCode.app/Contents/Resources/glm/zcode.cjs`；其他安装位置可显式设置
`FORMAL_GATES_ZCODE_SCRIPT`。不改宿主全局配置、默认模型或付款渠道。

保留原生Read/Glob/Grep。通用执行/改文件经单个 `workspace_command` MCP工具，复用现有
`codex sandbox` 原生OS隔离（不调用Codex模型），限制为当前角色的只读或工作区写入，网络关闭。
命令仅额外允许本次调用专有的临时/缓存子目录写入，不开放共享/tmp、用户HOME或控制目录；
macOS先解析实际Git执行文件，避免系统xcrun包装器在隔离区反复写共享缓存。
不向模型授予原生无限制Bash/Write/Edit、副代理、全局工作流或自动答复用户问题的工具。
DSH原生sandbox及工具策略保持原样。多宿主不代表多操作系统：本轮实机范围是macOS。

新会话在create时挂载角色工具，不执行“空会话close后再resume配置工具”的错误路径。
原生完成必须同时匹配session、input、turn，不能用send accepted或模型散文PASS代替完成事件。
失效会话和模型不可用分开分类；后者不会触发自动创建新身份。

ZCode冷恢复可能需要完整的原生runtimeModel。恢复使用原会话绑定的相同provider/model与推理设置，
从原生目录及已有CLI配置恢复同一连接的协议、endpoint和凭据引用；不根据模型名另选供应商。
若原生catalog省略凭据，则仅在原生子进程临时环境内提供已有凭据，以env引用传入协议；
不写入prompt、native-request.json、任务材料或结果证据，工作区命令子进程会剔除该变量。
无法取得同一模型的必要配置时明确受阻，不能用另一模型凑通过。

旧run继续使用保存的旧源码和二进制；本轮增加适配文件和更新Go bridge后，不得用新运行时
强行接管旧manifest或修改其绑定摘要。测试/报告分别记录实机通过项和未覆盖范围。

可复跑实机入口（会实际调用所选宿主模型）：

```sh
.dogfood/venv/bin/python runtime/langgraph/tests/native_multihost.py \
  --provider zcode --out /absolute/new-zcode-evidence
.dogfood/venv/bin/python runtime/langgraph/tests/native_multihost.py \
  --provider dsh --out /absolute/new-dsh-evidence
```

两路调用完全相同的公开demo和图；源码只列两个已实现的执行宿主，不把可安装名单当执行名单。

## 一条命令演示

不花模型额度的确定性演示（故意引入一个加减错误，实际执行测试、返修、复用另一项 PASS）：

```sh
./scripts/dogfood demo --approve-sample
```

真实 DSH 演示（会调用你当前配置的模型）：

```sh
./scripts/dogfood demo --native --approve-sample
```

两者都会创建新的小型独立 Git 项目，不修改你的产品工作树。`--approve-sample` **仅授权命令
自身生成的固定小样例的需求和计划**；不会自动答复真实审查 blocker、运行错误或额外返修授权。
普通 `start` 没有该开关。每轮结果、真实命令及退出码、模型身份、输入和输出保存在输出的 run 目录。

## 开发实际任务

先在项目外准备完整需求文件，以及检查清单，例如：

```json
[
  {
    "id": "unit",
    "argv": ["go", "test", "./..."],
    "dependencies": []
  }
]
```

`argv` 是明确批准的实际命令，不经过 shell 字符串解释。退出码零仅代表该命令的机械判据通过，
还需独立门审。命令以用户权限在隔离副本执行，**不是不可信任意脚本的安全沙箱**；确认前应检查它。
不要把来源不明的联网/删除/凭据操作混入检查命令。

`dependencies: []` 默认绑定整个候选树。显式列出文件/目录会允许在其内容未变时复用结果，
调用者必须完整包含代码、测试、依赖与配置；不支持 glob，不猜测依赖，不承诺识别错误依赖声明。
外部工具链/依赖环境需在该 run 中保持不变，当前不是通用的环境漂移检测器。

```sh
./scripts/dogfood start \
  --project /absolute/path/to/clean-git-project \
  --requirement /absolute/path/to/task.md \
  --checks /absolute/path/to/checks.json \
  --provider dsh
```

项目必须有已提交基线且工作树干净；程序不替用户丢弃未提交内容。它克隆基线用于开发、QA 和验证，
不会在源项目开发、提交、合并或推送。终态交付在 `<run>/workspaces/development` 的候选提交。

start 停在精确需求确认处，返回 `directory`、`revision` 和 `interrupts[].id`。阅读后答复：

```sh
./scripts/dogfood respond --run /absolute/run/directory \
  --revision <returned-revision> --interrupt <returned-id> --decision approve
```

产品/技术审通过后会再次展示计划确认；正常阶段之间无需逐个派发 action。
`show --run ...` 查看当前状态；`resume --run ...` 恢复中断控制器或已保存的工作，不创建新 run。
有待确认项时，resume 只展示，不替你批准。状态/候选/goto 不是用户答复或 worker 可写字段。

```sh
./scripts/dogfood show --run /absolute/run/directory
./scripts/dogfood resume --run /absolute/run/directory
./scripts/dogfood cancel --run /absolute/run/directory --confirm
```

cancel 只向本 run 拥有的活动子进程发取消信号；保存已完成记录，不删除开发代码。不伪造终态。
恢复后按取消证据进行明确处置。审查发现当前不可变需求本身有问题时，应明确终止旧任务，
修订后另起新任务；本 profile 不通过在途改 manifest 绕过确认。

默认两轮修复后询问是否增加一轮（可在 start 用 `--repair-limit` 显式设置）；不静默跳过、
不自动换模型、不把 BLOCKED、CONTINUE 或基础设施错误当作 PASS。

## 状态、权限和恢复

LangGraph SQLite 是唯一流程状态；图中只存工件引用和当前必要字段。Go 层定义普通 JSON
合同、结果校验、原生候选操作及资格判断，不维护第二个流程状态机。
角色输入来自程序及绑定材料，不接受模型的任意状态补丁。

所有写文件工具遵守 DSH 的原生权限。只读角色不能写入；开发者仅写其开发副本。
控制器、数据库、其他角色副本不应成为 worker 的可写根。没有承诺防御宿主自身漏洞或恶意 OS 用户。

子进程有独立监督进程保存 stdout/stderr 和退出回执。控制器退出后，恢复读取同一 job；
已观察完成的 job 不再启动。创建意图后没有进程身份，或进程丢失且没有退出记录时，明确 UNKNOWN，
不冒充已完成，也不盲目重发。原生 DSH 的 ACP `end_turn` 不足以判定成功，必须有绑定的
原生 `turn/end: completed` 及合法最终结果。

运行期间不要更新 driver 源码、依赖或重建 binary。每个 run 记录运行时摘要（包含实际审查规则），并保存绑定规则副本；不匹配就拒绝恢复，
不自动迁移。当前版本要求保留原工作树与二进制，不声称实现了自动多版本部署。重新执行 setup
会重建 binary，可能改变摘要；旧 run 的重读/恢复仍需其已绑定运行时，不能把重新 setup 当成恢复步骤。

开发期确需修复原生传输时，唯一例外是显式 `adopt-transport`：先完整保留原运行时，核对原/新
摘要并审阅精确差异。只允许 `native_dsh.py`、`workers.py` 和 CLI 的已审阅传输修复；图、持久化、
Go binary、依赖锁及其他运行时文件必须字节不变，当前 run 必须仅因 runtime 错误暂停，且无活动或
未决 job。该入口不证明业务语义自动兼容，调用者仍须审查差异并运行回归；不适用于图、需求或权限策略迁移。

```sh
./scripts/dogfood adopt-transport --run /absolute/run/directory \
  --snapshot /absolute/preserved/runtime-root \
  --from-runtime <exact-original-digest> --to-runtime <exact-reviewed-target-digest> \
  --reason "reviewed transport fix and validation evidence"
```

它另存 `transport-adoption.json`，不手改 manifest 或 LangGraph 检查点；随后通过原 respond/resume
入口继续。原运行时快照必须包含 `runtime/langgraph/formal_gates_dogfood`、依赖锁和原
`.dogfood/bin/formal-gates`，不能只填一个摘要作为证明。

结果格式允许一个完整 JSON 对象，或说明文字和唯一 JSON 代码块。多个结果、重复字段、代码块外
另有结构化内容、非法角色字段仍被拒绝。恢复旧解析器的格式失败时，必须核对同一原生会话、工作区、
模型选择及 `turn/end: completed`；仅重新解析保留的完成证据，不调用模型，不覆盖旧退出码和错误。
角色结果仍须通过 Go 合同校验，不能把模型/宿主实际失败洗成 PASS。

临时副本及证据不自动过期；结束后可按下文归档/回收。不得把手工清理活动 run 当作正常恢复。

### 首次检查点前中断（ZCode 红队 F1）

run 目录和 manifest 已创建、但首个检查点尚未保存时，`show` 仍保持只读，不补造数据库或审批记录。
无法打开检查点时，返回退出码 2 和 stderr JSON 错误 `CHECKPOINT_READ_ERROR`，保留实际 SQLite
原因及同一 run 的 `resume` 提示；数据库已建立但表尚未初始化时，返回 `CHECKPOINT_NOT_INITIALIZED`。
不再输出未捕获的 Python traceback，也不把不可读状态伪装为 PENDING 或 PASS。

确认属于首次启动中断后，使用 `./scripts/dogfood resume --run /absolute/run/directory`，由原来的
图执行入口初始化并停在需求确认点。`show` 本身不推进、不授予批准，也不需要重建 run。
其他读库错误同样如实报告；错误提示不是数据库损坏已被修复或状态具备防篡改能力的声明。
这里“只读”指不写业务记录或推进图；已有 WAL 数据库的读连接仍可能创建 SQLite 的 `-wal/-shm`
协调文件。验证同时核对 checkpoint/writes 表内容与业务文件，不能把协调文件等同于新的审批或状态。

### 紧急人工处置边界（2026-09-15 用户决定）

洞5暂不加固：保留操作系统文件所有者直接操作 SQLite 的现状，作为紧急时的人工处置口子，
不是正常代理工作流。此决定不新增状态补丁命令、不放宽代理权限、不自动迁移或修改任何旧 run。
紧急人工改动前应停止对应运行、保存一致备份及修改记录；之后人工核对确认点、候选和证据，不能
仅凭 `show` 的输出认定改动后状态仍有效。当前没有数据库防篡改或与 events 台账交叉回验的保证。
洞12暂不处理：DSH 的 read-only/workspace-write 原生沙箱、审批策略及角色工具配置保持不变。

## 验证和导出

```sh
go test ./internal/dogfood
FORMAL_GATES_DOGFOOD_ROOT="$PWD" \
FORMAL_GATES_DOGFOOD_BINARY="$PWD/.dogfood/bin/formal-gates" \
PYTHONPATH="$PWD/runtime/langgraph" .dogfood/venv/bin/python \
  -m unittest discover -s runtime/langgraph/tests -v
```

开发结果可以只读导出供审核，不自动合并源项目：

```sh
git -C /absolute/run/directory/workspaces/development \
  diff --binary <manifest-base>..<completed-candidate> > change.patch
```

实跑/失败和限制以本次开发证据为准；模拟测试不是模型 QA、官方 formal-gates Seal 或发布证明。

## 补充行为边界（2026-09-15 五项复核）

### 终态上的 respond

`DOGFOOD_COMPLETE`、`DEMO_COMPLETE` 和 `ABORTED` 不再接受新答复。`respond` 返回退出码 2
及 stderr JSON 错误 `RUN_TERMINAL`，明确说明答复没有应用。`show` 和不带答复的 `resume`
仍可读取终态，不启动新 job、不重写完成结果。这不是重新打开已结束 run 的入口。

### retry、restart-session 与 close

`retry` 的含义是对同一逻辑任务重试并优先续用原生会话；它不是重置会话。执行进程退出、
控制器失联并不等于持久会话消失。只有原生会话确实不存在或无法恢复时，操作者才在对应的
运行错误确认点选择 `restart-session`，沿用工作区和任务而更换执行会话。原生 `session/resume`
明确返回会话不存在/不可恢复时，错误现在归类为 `SESSION_UNAVAILABLE`，该确认点只提供
`restart-session` 与 `abort`；提交 `retry` 会被拒绝。网络、启动超时、模型配置等其他错误
不会被猜测成会话消失，仍保留原有 retry。新会话必须明确授权，不会自动改身份。
连续多次失败后换会话，会携带同一任务较早尝试的部分证据，不只携带最后一次初始化错误。

不能把不同宿主的 close 当成同一契约。本机 DSH 0.1.5-rc.1 的空会话在 close 后可恢复；
带真实完成 turn 的 DSH 会话在 close、退出进程之后，也能以同一 ID 开始第二 turn 并保留上下文。
本机 ZCode 的无模型 turn 空会话（含 `persistence=immediate`）close 后实测返回 -32004；
但有一轮已完成对话的会话可以在新进程恢复同一 ID，随后遇到的是模型可用性错误 -32031，
不是 Session not found。两者不能概括成“close 总是删除历史”。未来 ZCode 适配必须分别验证
会话物化、close 后恢复及模型恢复；本工具的业务 worker 目前仍仅接入 DSH。

### feedback 和存储保留

summary、发现说明和增量设计仍作为必要的业务材料传递，但现在使用带来源的 `evidence-data/v1`
及外层 `untrusted-feedback/v1` 包装，记录原工件摘要、角色、需求 revision 和数据摘要，明确
`authority=none`。task 的角色、候选、检查命令和控制能力由程序独立组装，绝不与 feedback 合并。
角色提示明确说明其中“用户已批准”“跳过检查”等文字不是新指令或授权。持续返修只携带原始
输入和最近一次相关反馈，旧记录保存在工件中，不再递归嵌套整个历史。
这不是通用的语义防注入承诺：模型仍可能判断错误，仍需要原有权限限制、Go 合同和验证流程。

### 终态归档与回收

现在提供显式的 `archive`、`archive-show`、`prune`。只处理 `DOGFOOD_COMPLETE`、
`DEMO_COMPLETE` 或 `ABORTED` 的 run；活动、暂停、有未完成作业或仍存在原生执行进程时拒绝。
不自动过期，不在运行途中删证据，也不清理外部 DSH/ZCode 会话、其他项目或全局缓存。

```sh
# 完整归档，不删除原现场。ZIP 目标必须位于 run 目录外，已有文件不覆盖。
./scripts/dogfood archive --run /absolute/run --output /absolute/archive/run.zip
# 独立验证每个归档成员并读取终态，不依赖该 run 的旧图运行时。
./scripts/dogfood archive-show --archive /absolute/archive/run.zip
# 默认仅预览；核对后显式确认。
./scripts/dogfood prune --run /absolute/run --archive /absolute/archive/run.zip
./scripts/dogfood prune --run /absolute/run --archive /absolute/archive/run.zip --confirm
```

归档包含完整的当前可用源码工作区（含 Git 和未提交成果）、jobs、artifacts、事件及检查点。
SQLite 使用一致备份 API 保存；逐文件核对 SHA-256，数据库比较逻辑内容而不是 WAL/SHM 页布局。
这是归档完整性校验，不改变洞5的数据库信任边界；不宣称防御可同时改归档与摘要的文件所有者。

`prune` 在核对归档确属同一 run、原数据没有变化后才删除 jobs、artifacts、checkpoints、
工作区等已归档现场，留下 manifest、需求、规则、终态结果和小型归档记录。清理中断可用同一命令
继续；未核对的新增文件不会被递归删除。`show`/无答复 `resume` 仍能核对归档并读终态，
不会重建检查点或重开任务。归档丢失/改变时明确报错。归档可以用标准 ZIP 工具解压离线查证，
它不是把一个已结束 run 重新变为活动 run 的迁移接口。

归档可放在其他磁盘以释放本机容量；若仍存本机，它仍占压缩后的空间，不等于数据凭空消失。
生产旧 run 没有在本次实施中被批量清理。旧版本 run 不迁移到新图运行时，仍遵守前述版本绑定。

可重复验证：`test_retention.py` 覆盖归档/预览/清理/中断恢复/错归档/数据变化/活动作业；
`test_session_feedback.py` 覆盖失效分类、错误操作拒绝、多次失败交接与反馈来源/控制隔离。
原生验证入口：`python runtime/langgraph/tests/native_missing_session.py --out NEW_DIRECTORY`，
会发送一次 DSH 模型提示，验证不存在的身份经显式换会话续接，以及恶意 feedback 的限定场景。

## 未来独立 Go 引擎

普通 Go 业务函数和 JSON 合同不依赖 LangGraph。暂时的图、检查点与 Python 进程都位于
`runtime/langgraph`，以后可以用 Go 执行内核替换，但不在本次顺便编写通用 DSL、双引擎同步或旧
检查点兼容层。独立 Go 版本通过相同业务与故障回归后，仅切换新 run；旧 run 使用原运行时完成。

## 2026-09-15 本机真实闭环记录

原 run `dogfood-20260915-122947-9202e5d9` 已经到达 `DOGFOOD_COMPLETE`，候选
`74b0cc9a4fb6084f8cd35258b1c93946e1bff825`。使用 DSH 原生选中的
`deepseek-official/deepseek-v4-flash`、`high`。产品审、技术审、开发、QA 设计/审查及两门均有原生
会话证据；两条批准的实际 Python 检查通过。此前开发的说明文字加 JSON 代码块导致解析器拒收，
本次通过明确的传输修复和原完成证据重新接纳；没有重做开发、重开 run 或重跑已有效审查。
该小样例实现一次通过，真实模型返修并未由本样例覆盖；返修/增量保留另有确定性测试。

本机证据在 `.dogfood/evidence/`，原生 job 的 `native-events.jsonl`、会话身份、实际命令与输出在
该 run 的 `jobs/`。原始失败日志保留；成功不等价于完整 3.5c 的 QA/Seal，也不表示 Windows/Linux
已实测。`.dogfood` 为本地开发内容，不进入不可变发布包的摘要扫描；真实包路径中的 symlink
仍然拒绝。


## 非正常路径修复与中途换会话

用户确认必须携带当前 `--interrupt`，即使只有一个待确认项也不能省略。同一需求 revision
可能对应需求确认、计划确认等不同对象；旧批准命令不能用在新确认点。`show` 通过 SQLite
只读快照查询，不获取推进锁；它显示已持久化的进度，并不把“已签发”冒称为正在执行。

检查执行的 stdout/stderr 保存内容摘要和大小；复用、缓存回执读取及最终资格判断重新核对原始
证据。缺失或变化时阻塞，不能只凭元数据继承 PASS。开发或返修返回未解决的 FAIL/P1/P2 时进入
明确处置，不能被后续检查或门审 PASS 覆盖掉。

若真正的开发或审查执行被中断，先 `show` 确认对应运行错误。`retry` 仍按原会话恢复。
确实需要全新执行会话时，在该错误的精确确认对象上显式选择：

```sh
./scripts/dogfood respond --run /absolute/run/directory \
  --revision <current-revision> --interrupt <current-interrupt-id> \
  --decision restart-session
```

这不是重建 run、换工作区或清除原结果。程序先检查旧调用已有退出回执且原生执行进程不再存活，
再为同一角色提供已绑定需求、现有工作树、未提交 diff、原角色事件与工具记录。新原生会话使用
新身份；结果记录指向前一次执行。无退出记录或旧执行者仍活着时拒绝启动第二个写入者。
未接受的原发现只是部分进度，新审查者需要复核，不能将未完成审查当成 PASS。

没有保存的模型思考无法恢复；证据超过当前 4 MiB 交接上限时明确要求处置，不悄悄截断后宣称
完整接手。本实现不承诺 PID 复用、机器断电或宿主服务端失联情况下自动确定外部副作用完成情况。
已有历史 run 不自动迁移到这个版本；须使用它绑定的旧运行时。`restart-session` 不是在途升级入口。

### 可复跑的原生中断验收

下列测试会真实调用 DSH；每项在两个函数/文件的前半段取证后，终止实际 DSH 执行进程与其后代，
再通过同一 `Flow/run_graph` 的 `restart-session` 路径使用新会话继续。其他角色明确用夹具构造，
并在目标角色完成后以测试中断停止，因此不冒充完整独立 QA 或正式 Seal。

```sh
PYTHONPATH="$PWD/runtime/langgraph" .dogfood/venv/bin/python \
  runtime/langgraph/tests/native_partial_takeover.py development \
  --out "$PWD/.dogfood/native-partial-development-new"
PYTHONPATH="$PWD/runtime/langgraph" .dogfood/venv/bin/python \
  runtime/langgraph/tests/native_partial_takeover.py review \
  --out "$PWD/.dogfood/native-partial-review-new"
```

输出目录必须不存在。测试会保存中断前的文件摘要、公开部分发现、旧进程退出、旧/新会话身份、
真实调用日志和独立断言。开发测试要求已完成文件保持不变，另一函数被补完；审查测试要求保留并
复核第一处缺陷，再发现第二处，源文件不变。普通单元测试不会触发这些付费模型调用。


本轮回归包括此前 15 项故障场景（修复后均通过）、39 项 Python 整套回归及证据丢失后显式重试的
定向补测。开发和审查均在真实 DSH 执行中断后使用新原生会话验证。审查的“未完成”以已有部分
公开发现但尚未给出最终结论为准；它不等于保证旧代理从未读取其他文件，新审查者也可必要地复核
已有材料。原始模型回复、夹具自身的格式校正和测试停止点如实保留在本次开发证据中。
