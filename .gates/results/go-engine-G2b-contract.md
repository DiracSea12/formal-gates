# G2b 替换单元合同：DSH 薄适配（在已验收监督器下移植 DSH 请求、身份、结果和错误映射）

基线为 G2a（候选 `df33e9c`，链头准入已 `ADMISSION GRANTED ... for G2b`）。G2a 已终审收口；G2b 是 G2 阶段第二项。

合同依据（回原文核对）：

- §13.1 G2b 行（第 573 行）：职责为"**在已验收监督器下移植 DSH 请求、身份、结果和错误映射；ZCode 保持原实现**"；验收出口为"**新建、续用、只读约束、原生配置继承、根/子会话关联均通过**"。
- §13.1 表后正文（第 581 行）："G2d 先验一个宿主，再接另一个"——原生交互（持有、展示、投递、原调用继续）仍单列 G2d，本单元不交付答复通路。
- §12.5（第 520 行）："宿主适配器**首次接入当前累计组合**时，还须**由该宿主跑通完整原生小流程**。"
- §12.1（第 583 行）：Go 组件经"已存在的桥接方式或**同一二进制的窄内部命令**接入"。
- §12.3（第 482 行）："**同一 run、同一职责只启用一个实际实现。**"§12.2（第 478 行）："直接复用，不另写第二份。"
- §2.1：目标架构把 `internal/native/{contract,registry,result,interaction,transport}.go` 与 `adapter/{register.go,dsh.go}` 全放 Go，依赖方向"native 公共包与 runner 不导入 adapter；程序组装入口登记并注入具体实现"。
- 索引未决问题 25（用户 2026-09-18 指示方向）："新宿主适配默认直接落 Go"；建议 G2b/G2c 出口补"该 host 的 Python 执行路径退役"。索引【更正 2026-09-18（第三轮）】："G2b/G2c 的实施形态在派发前按该方向定夺。"

## 形态定夺（写给评审：为什么是窄内部命令）

被移植的对象是**一次原生调用的执行**：长期存活的宿主进程、逐行 JSON 的双向循环、`UNBOUNDED` 的回合等待（宿主自己决定回合多长）、随时可能到达的宿主反向请求（`session/request_permission` 等）。这与桥接操作"请求/应答、短调用"的前提直接冲突——与 G2a 搬进程所有权是同一类冲突。按 G2a 在索引评审问题 11 留下的判据（"凡需要跨调用存活的状态、或需要接收信号者，走内部窄命令"），本单元走 §12.1 的第二条路径：

```sh
formal-gates harness native <job-directory>
```

一个原生作业一个进程：读 `native-request.json`、按 provider 分派（Go 注册表）、执行 DSH 的 ACP 时序、写出全部证据文件、把结果 JSON 打到 stdout、失败写 `native-error.json` 并退出 1——与今日 `formal_gates_harness.native_entry` 的对外形状逐字一致。它**作为监督器的子进程**运行（`Store.job` 的 argv 从 `[sys.executable, "-m", "formal_gates_harness.native_entry", dir]` 换成 `[binary, "harness", "native", dir]`，其余参数逐字不动），"在已验收监督器下"由此成立：监督器零改动（`supervise.go` 不动、`kind` 仍不被读取）。

**Python 执行路径退役**（§12.3 单一活动实现、问题 25 方向）：`native_dsh.py` 从生产路径删除，逐字移入仅测试使用的 `tests/reference_native_dsh.py`（沿 `reference_job.py` 先例），差分对照以它为参照。**ZCode 保持原实现**：`native_zcode.py` 与 `native_transport.py` 生产路径逐字不动（`native_transport.py` 仍服务 zcode；G2c 再按同一公共边界移植）。宿主侧插件 `native_dsh.mjs` **原地不动**：它运行在 DSH 进程内（§1"宿主自身需要的运行时及小型原生插件按实际需要保留"），不属 Python 执行路径；Go 以与 `command.go` 相同的根发现规则（`FORMAL_GATES_HARNESS_ROOT`，缺失时从可执行文件向上找 `runtime/langgraph/formal_gates_harness/__main__.py`）定位它，`runtime_identity` 继续按 `.py/.mjs` 清单覆盖它。

**反过度设计约束（派发前自查后收紧，2026-09-19）**：Go 原生代码**全部落 `internal/harness` 单包**，不新建 `internal/native` 包、不建 `NativeOperations` 接口与装配注入、`cmd/formal-gates` 与 `supervise.go` 零改动——单实现接口＋单调用点注入正是方案 §15 明文反对的"为假设中的调用方预建体系"，且本链既定形态就是单包（C1–M3 桥接操作、G2a 监督器皆然）。G2c 的 `zcode.go` 落同包即得"每平台一个薄文件"属性；将来 runner 需要拆包时作为独立单元再拆。序列化同理只保真**可观察**字节：出站协议行、`protocol.jsonl`、stdout 结果改用 canonical 紧凑形（JSON 等价、消费者按值解析，差分按值比对，登记有界差异）；逐字节复现仅保留两处模型/证据可观察面——错误文本内嵌 JSON 与 prompt 文本。

## 职责边界

| 归属 | 内容 |
| --- | --- |
| **Go（新，全部落 `internal/harness` 单包）** | `native_transport.go`：共享传输机械（宿主进程派生、逐行读循环、trace、交互回执、有界/逐消息/UNBOUNDED 等待、关闭语义、类型化错误）；`native_dsh.go`：DSH 薄适配（ACP 信封与时序、`selection`、`make_patch`、`observe`、`write_answer`、`evidence_source`、`capture_binding`、`validate_binding`、`verify_source`、`reconcile`、错误分类、provider 分派）；`pydump.go`：`PyDumpsRaw`（保序再编码器，仅此一个导出）；`harness native <dir>` 窄命令入口；4 个新桥接操作 `native-capture`／`native-verify-binding`／`native-verify-source`／`native-reconcile` |
| **Python（改）** | `native_hosts.py`：注册表增加 Go 适配项（`dsh` → Go；提供 `entry_argv(binary, dir)`、`execute`（探针路径：写 `native-request.json` 后派生同一 Go 命令、解析 stdout、把 `native-error.json` 翻译回异常）、`capture`／`validate_binding`／`verify_source`／`reconcile` 走桥接操作、`transport_files()` 声明 `('native_dsh.mjs',)`）；`workers.py:120` 一行 argv 改经 `adapter.entry_argv(...)`；`cli.py::doctor` 无改动面（经注册表自动走 Go） |
| **Python（不动）** | zcode 全链（`native_zcode.py`、`native_transport.py`、`zcode_credential.mjs`）；`native_entry.py`（仍服务 zcode）；`Store.job`／`cancel_jobs`／监督协议；prompt/task 材料构造与 `native-request.json` 写入（workers）；结果消费、`validate-worker`、`verify_evidence` 派发（经注册表）；`graph.py`；`native_dsh.mjs` |
| **明确不在本单元** | 原生交互答复通路（G2d：`session/request_permission` 仍如实报 `DSH_INTERACTION_REQUIRED`）；zcode 的任何移植或重构（G2c）；`Store.job` 其余逻辑；`internal/runner`／调度（G4） |

## 接口

```sh
formal-gates harness native <job-directory>
```

- 退出码 **0**：`native-request.json` 的 `provider` 经分派、执行完成，结果 JSON（`source` 或探针报告）打到 **stdout**（单行、canonical 字节；消费者只做值解析——有界差异）。
- 退出码 **1**：任何执行期异常——**一律**落盘 `native-error.json` `{"error": "<TypeName>: <msg>"}` 并把 `error` 文本打到 stderr；`SessionUnavailable` 时另带 `error_kind: "SESSION_UNAVAILABLE"`、`identity`、`method`、`native_code`——与 `native_entry.py` 逐字段一致。`native-request.json` 缺失/畸形同样走此路（`FileNotFoundError`/`JSONDecodeError` 文本）；`provider` 不在 Go 注册表 → `RuntimeBlocked: UNSUPPORTED_NATIVE_HOST: <provider>; execution adapters: dsh`。
- 退出码 **2**：`<job-directory>` 参数缺失（镜像 `supervise` 的入口检查；`native_entry` 对应情形是 `IndexError` 裸栈、同为入口自身失败类，不写 `native-error.json`）。
- 非确定/不可达字段访问沿用 Python 语义复现：非探针缺 `prompt`/`schema` → `KeyError: 'prompt'`（不可达：workers 恒写全、探针不读）。

桥接操作（走既有信封与 `refuse()` 类型化拒绝约定）：

| 操作 | 请求 | 应答 |
| --- | --- | --- |
| `native-capture` | `{provider}` | `{binding}`（`dsh` 在 PATH 上：`{"provider","launch":{"executable":<resolved>},"files":{<resolved>: proof}}`）；不在 PATH → 拒绝 `DSH_UNAVAILABLE: dsh is not on PATH` |
| `native-verify-binding` | `{provider, binding}` | `{ok:true}`；provider 不符 → 拒绝 `HOST_BINDING: provider differs`（HarnessError 类）；文件证明不符 → `HOST_CHANGED: bound native executable/script changed: <name>` |
| `native-verify-source` | `{provider, directory, source}` | `{ok:true}`；四件套缺失/不符 → `EVIDENCE_MISSING: native source proofs absent`／`EVIDENCE_CHANGED: <name>`；provider/identity 不符 → `NATIVE_BINDING: missing or different provider/identity` |
| `native-reconcile` | `{provider, directory, process}` | `{source}`（带 `reconciliation` 块）或不合格拒绝 `DSH_RECONCILE: not a completed formatting-only failure` |

## 迁移前语义（以 `native_dsh.py`＋`native_transport.py` 现网实现为准，差分实测钉住）

### ACP 时序（`execute`）

| 步 | 调用 | 参数/判据（逐字） |
| --- | --- | --- |
| 1 | 派生宿主 | `argv = [executable, "--profile", "acp", "--patch", str(patch)]`，`cwd=workspace`，`env=自身环境`（不加不减）；`native-process.json` 原子写 `{"pid","pgid"}`（pgid＝自身进程组——派生不建新组；Windows 下 `pgid: null`） |
| 2 | `initialize` | `{"protocolVersion":1,"clientCapabilities":{}}`；应答须 `protocolVersion==1` 且 `sessionCapabilities` 含 `resume` 与 `close`，否则 `DSH_CAPABILITY: ACP v1 resume/close required` |
| 3 | `session/new`／`session/resume` | `{"cwd":workspace,"mcpServers":[]}`（＋`sessionId` 当有 identity）；`configOptions` → `selection`（`{o.id: o.currentValue}`，缺 `currentValue` 为 `None/nil`）；应答无/异 identity → `DSH_BINDING: missing or changed identity`；随后原子写 `identity.json`（`identity/workspace/selection` 三键） |
| 4 | `session/set_config_option`（条件） | 仅当 `model`／`reasoning_effort` 非空且≠已配置值；`{"sessionId","configId","value"}`；回读 `selection` 更新后仍≠请求值 → `DSH_CONFIG: <name> selection was not applied` |
| 5（探针） | `session/close` 后返回 | `{"provider":"dsh","executable":<resolved>,"capabilities":<sessionCapabilities>,"selection":<configured>,"model_calls":0}`；**不发 prompt** |
| 6 | `session/prompt` | prompt ＝ `request.prompt + "\nReturn ONLY one JSON object matching this result schema:\n" + json.dumps(request.schema, ensure_ascii=False)`（**Python 默认分隔符 `, `/`: `、键序＝native-request.json 的文件序**）；`{"sessionId","prompt":[{"type":"text","text":<prompt>}]}`、超时 `UNBOUNDED`（无总预算、无限等）；应答 `stopReason != "end_turn"` → `DSH_STOP: <stopReason>` |
| 7 | `session/close` → `observe` | 成功后 `source.json` 原子写（含 `selection`）；`finally` 中仍附着时补发 `session/close`（10s 超时、吞错）并收尾进程 |

### 传输与等待语义（`Client`）

| 语义 | 值 |
| --- | --- |
| 信封 | `{"jsonrpc":"2.0","id":<int 计数从 1>,"method":...,"params":...}`；应答按 `id` 匹配且无 `method` |
| 默认超时 | 40s；`bounded=False` ⇒ **逐消息**等待、**无总截止**（G2a 加的总截止检查只作用于 bounded；DSH 不变） |
| 宿主反向请求 | `session/request_permission` → 立即 `RuntimeBlocked: DSH_INTERACTION_REQUIRED: native permission needs a user decision; none was sent`（**不答复、不重试**）；其余 → `-32601 "Client method not supported"`（status `unsupported-method`），逐次原子追加 `interaction-status.json` |
| 错误分类 | `session/resume` 且 `code ∈ {-32602,-32004}` 且 message 前缀 `Invalid params: session is not resumable:` 或 `Session not found:` → `SessionUnavailable("DSH_SESSION_UNAVAILABLE: <msg>", identity=params.sessionId, code)`；其余错误 → `RuntimeBlocked("DSH_NATIVE <method>: " + json.dumps(error, ensure_ascii=False))` |
| 关闭 | stdout EOF → `RuntimeBlocked: DSH_TRANSPORT: native ACP process closed its output`；非 JSON 行 → `DSH_TRANSPORT: non-JSON ACP output`（读循环终止）；**任一入站消息含键 `closed` 或 `transport_error` 即按关闭处理**（继承行为，如实复现） |
| 超时 | `RuntimeBlocked: DSH_NATIVE_TIMEOUT: <method>`（逐消息超时与（不存在的）总截止共用此文） |
| trace | `protocol.jsonl` 逐行只记 `{id?, method?}` 路由字段、永不记录载荷；序列化用 canonical 紧凑形（Python 侧为默认分隔符——**有界差异**：trace 是调试件、不入 `SOURCE_FILES`、无消费者，差分按值比对） |
| 收尾 | stdin 关→等 8s→SIGTERM→等 5s→SIGKILL；读线程 join 2s；文件句柄关闭 |

### `observe`（结果映射）

- `native-source.json` 的 `sessionId` 必须＝identity 且 `turn` 为 int，否则 `DSH_BINDING: native source does not match the session`。
- 遍历 `native-events.jsonl`：sessionId 混杂 → `BINDING: mixed session evidence`；跳过 `compaction/summary` 与 `data.turn != source.turn`；`assistant/message`（回合结束后再出现 → `DSH_RESULT: message after turn ended`；`usage` 为 dict 则记 usage 证据；answer＝全部 `type=='text'` part 的 `text` 连接）；`assistant/attempt`（有 `usage` 才记）；`turn/end`（重复或 `reason.kind != 'completed'` → `DSH_RESULT: turn was not completed: <reason>`；否则 ended）。
- 无 `turn/end` → `DSH_RESULT: committed turn/end evidence is missing`。
- `write_answer`：`answer.raw.txt` 已存在则逐字节比对（不同 → `DSH_RESULT: saved output differs`），否则写入；`answer.json` ＝ Go `FrameResult` 对原文的裁决（拒绝原文即 `NATIVE_RESULT:` 前缀 refusal 透传）。
- 收据 `evidence_source`：`{"provider":"dsh","identity","native_turn","transcript","usage_evidence"(或 None),"files":{四件 proof}}`；`execute` 另加 `selection` 后落 `source.json`。

### `reconcile`

仅回收"进程 `exit_code==1`、`error` 为空、存在 `native-error.json` 且其 `error` 等于 `RuntimeBlocked: DSH_RESULT: final message is not a JSON result object` 或以 `RuntimeBlocked: NATIVE_RESULT:` 开头"的完成态格式失败：核对 workspace 未变、原选型未变，重走 `observe`，`source` 附 `reconciliation` 四键（`kind/original_exit_code/original_error/new_model_calls`）落 `source.json`。不合格 → `DSH_RECONCILE: not a completed formatting-only failure`；workspace/选型变动 → `DSH_RECONCILE: workspace changed`／`DSH_RECONCILE: original model selection differs`。不改写任何原始记录。

### 序列化形状（证据文件的字节合同）

| 文件 | 形状 |
| --- | --- |
| 一切 `atomic` 落盘 | 规范字节（排序键、紧凑）——复用 C3 `CanonicalJSON`，与 Python `atomic()` 逐字节同 |
| `answer.raw.txt` | 原文字节（UTF-8） |
| 出站协议行 | canonical 紧凑形（排序键、保留非 ASCII）——与 Python 侧默认分隔符形 JSON 等价；宿主按值解析，差分按值比对（**有界差异**） |
| `protocol.jsonl` | 同上（有界差异，不入证据四件、无消费者） |
| 错误文本内嵌 JSON | `json.dumps(error, ensure_ascii=False)` 默认分隔符、**键序保持入站原序**——Go 侧以有序再编码器复现（含浮点 repr、Unicode 转义规则），差分用例钉住 |
| prompt 内嵌 schema | 同上；`native-request.json` 为规范序（排序）故键序＝排序序 |

## 不变量

1. **`native-request.json` 形状不变**（workers 侧写入逻辑零改动）；探针目录新增一份 `native-request.json`（`probe: true`）——探针与作业从此共用同一输入合同（有界差异，见"待评审"）。
2. **四件证据与收据字节形状不变**（`SOURCE_FILES` 集合、`identity.json`/`source.json`/`native-error.json` 字段、规范序列化）。
3. **错误文本逐字复现**（上两表全部条目；含 `SessionUnavailable` 的 `native-error.json` 四附加键）。
4. **ZCode 路径零改动**（`native_zcode.py`/`native_transport.py` 逐字节不动；其全部既有测试原样通过）。
5. **单一活动实现**（§12.3）：dsh 的生产执行只余 Go；`native_dsh.py` 删除、逐字移入 `tests/reference_native_dsh.py`，生产代码不得导入。
6. **监督器零改动**：`supervise.go` 不动；"同一 request 只改 `kind` 不改变行为"的结构性证据继续成立；native 作业的 `request.json`/`owner`/`exit.json`/流证明语义不变（argv 一行除外）。
7. **只读约束经宿主原生机制**：`make_patch` 对 `read_only` 注入 `sandboxPolicy`、对探针另禁 `session-title-llm`；工具面收敛与根会话证据全部由 `native_dsh.mjs`（零改动）在宿主内实施；适配器不引入第二套权限引擎。
8. **取消语义不变**：Go 窄命令不装自定义信号处理（与 `native_entry` 一致），组取消由监督器结算（`exit.json 130`）。
9. **不新增 go.mod 依赖**；Go 原生代码全部落 `internal/harness`（无新包、无新接口、无装配注入、`cmd/formal-gates` 零改动）；`supervise.go` 零改动（同包直用其未导出助手，不新增导出面）。

## 必需用例

**差分对照**（`tests/test_native_dsh_differential.py`，参照 `tests/reference_native_dsh.py` 逐字实现）：假 ACP 宿主为 Python 脚本（同 `FAKE_APP_SERVER` 先例），镜像真 DSH＋mjs 的可观察行为（读 `profile.json` 的 `config.directory`、按 `identity.json` 写 `native-source.json`、流式追加 `native-events.jsonl`、回显收到的 prompt/参数到记录文件）。两侧各用**独立目录**，派生前把同一份 `native-request.json`（规范字节）写入两目录；比对前把各自根归一为 `<root>`。

| 用例 | 判据 |
| --- | --- |
| 新建成功回合 | 两侧 `identity.json`/`source.json`/`answer.raw.txt`/`answer.json`/`profile.json`/`interaction-status.json` 归一后逐字节相同；`protocol.jsonl` 与 stdout 按值等价；假宿主回显的 `session/new`/`prompt` **参数值**相同、其中 **prompt 文本逐字节**相同 |
| 续用（resume） | `session/resume` 带 `sessionId`；成功与 `SessionUnavailable`（`-32004`＋`Session not found:`）两向：错误侧 `native-error.json` 四键逐字相同、退出码 1 |
| 配置继承 | `model`/`reasoning_effort` 已配置（不重设）／未配置（`set_config_option` 后回读）／回读不符（`DSH_CONFIG`）三向 |
| 回合异常 | `stopReason != end_turn`（`DSH_STOP`）；`turn/end` 缺失／`kind != completed`／消息在 end 之后（三条 `DSH_RESULT`） |
| 证据异常 | `native-source.json` 会话不符（`DSH_BINDING`）；`native-events.jsonl` 混会话（`BINDING: mixed session evidence`）；`answer.raw.txt` 已存在且不同（`saved output differs`） |
| 交互边界 | 宿主反向 `session/request_permission` → 两侧同文本 `DSH_INTERACTION_REQUIRED`、均不答复（假宿主回显收到答复次数为 0） |
| 传输边界 | 宿主 stdout 提前关闭（`DSH_TRANSPORT: native ACP process closed its output`）；输出非 JSON 行（`non-JSON ACP output`）；超时（假宿主静默 >0.2s，`DSH_NATIVE_TIMEOUT: initialize`） |
| 逐消息等待 | 通知持续到达时响应晚到仍被接受（`bounded=false` 不设总截止）；`UNBOUNDED` prompt 不被本方预算截断 |
| 探针 | probe=true：不发 prompt（假宿主回显计数 0）、返回报告含 `model_calls:0`、`profile.json` 含 `session-title-llm` 禁用项 |
| 错误文本内嵌 JSON | 宿主错误体含浮点/Unicode/嵌套/键序敏感载荷：两侧 `native-error.json` 的 `error` 逐字节相同 |
| 收据与回归 | `reconcile` 合格（`NATIVE_RESULT:` 旧错）回收、不合格拒绝、workspace/选型变动拒绝；`native-capture`/`native-verify-binding`/`native-verify-source` 与注册表既有断言（`HOST_CHANGED`/`HOST_BINDING`/`EVIDENCE_CHANGED`/`EVIDENCE_MISSING`/`NATIVE_BINDING`） |
| 非确定字段 | `pid`/`pgid`/时间戳只断言存在与类型 |

**Go 单元测试**（`internal/harness/native_dsh_test.go` 等）：时序逐步断言（initialize 能力缺失、identity 变更、`selection` 解析含 `currentValue` 缺失）、`make_patch` 形状（无 `agent-loop/sandbox-policy/permission/approval` 等覆盖项、`mcpServers:[]` 附加语义）、trace 永不含载荷、`write_answer` 幂等、`capture` 的 PATH 语义（找不到 → `DSH_UNAVAILABLE`）。

**既有回归**：全套 Python 套件（`test_native_hosts.py` 注册表断言按新注册事实更新——dsh 的 transport 声明只剩 `native_dsh.mjs`、Python 模块结构性断言收窄到 zcode＋reference；`test_native_inheritance.py` 中 dsh 专属用例移入差分/Go 侧后原文本归档）；`test_job_supervision_differential.py`、`test_driver.py`、`native_missing_session.py`（经注册表走 Go 路径后仍须通过）、doctor-dsh（探针 0 模型调用）。

**负控制**：对差分实施至少两处定向变异（去掉 prompt 尾部换行/改动一条错误文本前缀），确认差分**真失败**后才放行。

## 出口判据映射（§13.1）

| 出口 | 离线证据 | 原生证据（需机主授权，§12.5） |
| --- | --- | --- |
| 新建 | 差分"新建成功回合"＋Go 单测时序 | 真实 DSH 新建会话完成角色 |
| 续用 | 差分 resume 两向＋`native_missing_session.py` | 真实续用/身份失效路径 |
| 只读约束 | patch 形状＋mjs 零改动＋RootSessionHook 原样 | 只读角色原生事件无写入口、`sandbox/mode` 生效 |
| 原生配置继承 | 差分配置三向＋探针 selection | 真实运行 model/effort 选型经宿主回读 |
| 根/子会话关联 | observe 身份/回合绑定＋混会话拒绝＋子事件不进根证据 | 根角色完成证据绑定根会话 |

原生定向验收执行 `runtime/langgraph/tests/native_multihost.py --provider dsh`（§12.5"由该宿主跑通完整原生小流程"）。**未获机主明确授权前不消费任何真实模型调用**；受阻时本单元保持未验收（§14.3），不改标不适用。

## 受影响调用方

- `workers.py:120`（argv 一行）；`native_hosts.py`（注册表）；`cli.py::doctor`（行为经注册表变化、代码不动）；`infra.py::verify_evidence`（经注册表，代码不动）
- `internal/harness/command.go`（`native` 分支）；`internal/harness/bridge.go`（4 个新操作分派）；`cmd/formal-gates/main.go` **零改动**
- 测试：`test_native_hosts.py`／`test_native_inheritance.py` 的 dsh 段落按新归属更新；新增 `tests/reference_native_dsh.py`、`tests/test_native_dsh_differential.py`、Go 侧 `internal/native/**`
- 记录工具：`admission.py` 的 `UNITS_REQUIRING_NATIVE_VERIFICATION` 加入 `G2b`（改后必跑全套准入控制项）

## 交付物

- `internal/harness/{pydump.go,native_transport.go,native_dsh.go}`＋各自测试（`RunNative` 入口并入其中，`bridge.go`/`command.go` 各加分派；无新包、无接口、无 `cmd` 改动）
- `runtime/langgraph/formal_gates_harness/native_hosts.py`（GoAdapter）、`workers.py`（一行）、`native_dsh.py` 删除
- `runtime/langgraph/tests/reference_native_dsh.py`（逐字）＋`test_native_dsh_differential.py`＋既有测试更新
- `scripts/go-engine/admission.py`（G2b 入列原生必需单元）

## 待评审的判断（不是遗漏）

1. **mjs 原地不动**：它随 `runtime_identity` 继续被身份覆盖；若评审认为应随适配器迁往 Go 侧（如 `internal/harness/` 原生文件旁），会同时改变身份清单与 adopt-transport 的库存口径，建议作为 G2c 一并定夺。
2. **探针目录新增 `native-request.json`**：让探针与作业共用同一输入合同；代价是 `doctor` 的证据目录多一个文件（无人消费该目录的文件集合断言）。
3. **`transport_files()` 对 dsh 只声明 `native_dsh.mjs`**：Go 侧传输在二进制内、不属 Python 包库存；adopt-transport 对 dsh 的可采纳面因此收窄为 mjs——Go 传输修复本就被"Go 不得变"的采纳前提排除，这与既有设计一致。
4. **不可达错误文本**（缺 `prompt`/`schema`、`native-request.json` 缺失）：按 Python `KeyError`/`FileNotFoundError` 文本复现并注明不可达论证（沿 G2a 先例）；若评审认为应结构性拒绝，需指认由哪一侧。
5. **错误文本内嵌 JSON 的键序/浮点保真**：以有序再编码器复现 Python `json.dumps`；这是本单元"类型边界静默收窄"的最高风险点（历史第 7 次同型缺陷即来自把动态语义换成静态形状），差分语料须含浮点、Unicode、嵌套与键序敏感载荷。
