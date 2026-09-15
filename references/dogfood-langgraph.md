# LangGraph 开发期自狗粮驱动（dogfood-v1）

本入口从 3.5c 封板 `517de91f1b480ec969bd91385c29ce38d8bc2841` 开发。
LangGraph 是**开发工具**，用于先让程序自动执行小型开发任务，再辅助开发独立 Go 引擎。
Go 产品的普通命令、构建和安装不需要 Python。不要把本工具展示为已经完成的独立 Go 编排引擎。

## 能力与边界

当前支持本地 Git、一个非分片任务、DSH 原生代理、产品与技术审、需求及计划确认、
开发与基线 QA 覆盖设计/审查并行、候选提交、批准命令与两门并行、返修循环、证据保存和恢复。
原生会话独立；开发返修续用其实际会话。图决定下一步，模型只返回当前角色的结果。

这是小范围开发 profile，不等价于完整 3.5c/4/4a 正式 regular：当前检查命令由调用者事先提供，
QA 代理审查其覆盖而不是交付完整增量测试用例体系；尚无分片、多 VCS、在途需求修订、完整白盒
设计、门审 carry 全语义和正式安装切换。未做完的领域规则后续进入独立 Go 引擎，不能冒称已覆盖。

实际成功终态为 `DOGFOOD_COMPLETE`，确定性模拟为 `DEMO_COMPLETE`；两者都不是旧流程的
正式 Seal，也不写旧 `.gates/state.json` 或旧封板结果。不复用/修改 4a 活动 run。

## 设置（本机 macOS 已作为验证目标）

要求已有 Go、Python 3.11+、uv，以及已经配置模型的 DSH。仅在独立开发工作树中执行：

```sh
./scripts/dogfood setup
./scripts/dogfood doctor
```

setup 把锁定的 Python 依赖装在本工作树 `.dogfood/venv`，把 Go 开发 binary 构建在
`.dogfood/bin/formal-gates`。不修改全局包、宿主配置、稳定 formal-gates 安装或用户付款渠道。
LangGraph 及 SQLite 包版本固定在 `runtime/langgraph/requirements.lock`；不是每次启动联网更新。
`doctor` 只做 DSH 原生初始化、建立/关闭探测会话，不发送模型 prompt；显示实际选中的模型与能力。

DSH 使用现有 ACP profile、凭据和模型。每次子进程通过局部 patch 使用原生只读/工作区写入
sandbox，并由 DSH 自身的工具 guard/restrict 限制角色工具；不会启用副代理、旧流程 hook、
用户额外 MCP 工具或更换模型。用量缺失明确为 null，不填零。

目前实机目标是 macOS；没有宣称 Windows/Linux 安装、权限和取消行为已验证。

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

临时副本及证据目前保留以方便自狗粮调试；不自动删除整个现场。不得把手工清理活动 run 当作正常恢复。

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
