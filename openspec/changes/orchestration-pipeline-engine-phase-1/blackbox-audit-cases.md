我已完成对流程契约（qa-design.md）、阶段需求（master-requirements.md 九条验收）、ADR-001、reviewer-base.md 边界，以及交付公共 API 面（authoring/compiler/encoder/definition/decision/runtime/shadow、cmd/gen-definition、definitions/workflow.json v2）的核实。以下是黑盒用例集。

# 阶段 1 黑盒 QA 用例集（45 条）

**通用约定（所有用例的执行前提）**
- `$WS` = QA 隔离工作区的仓库根（阶段候选快照，HEAD=fbe51cf，交付基线 b9eb03c）。
- 公共 Go API 驱动：因包在 `internal/` 下，驱动须建于模块内——在 `$WS` 下新建临时目录 `.qa-blackbox/<用例名>/main.go`（package main，只 import 交付的公开包，不引用任何交付内 `_test.go`），从 `$WS` 执行 `go run ./.qa-blackbox/<用例名>`；用例结束后删除 `.qa-blackbox/`。产物类驱动可用 `go build -o /tmp/<名> ./.qa-blackbox/<名>` 出二进制后在任意目录执行（体现“真实产物”）。驱动自身断言失败时以非零码退出并打印观察到的事实。
- 生成器入口：在 `$WS` 根执行 `go run ./cmd/gen-definition`（文档化用法）。
- 边界遵守 qa-design.md 与 reviewer-base.md：不发明对抗性、手工改写权威状态、权限/不可变文件、不支持平台用例；构造输入一律走公开 API 或真实文件产物。

---

## A. Authoring（验收条目 1；5.8 constructor 非法状态）

**QA-B01｜六变体的 authority/runner 由变体派生，映射固定**（条目 1）
- 步骤：驱动分别用 `authoring.NewLocalStep/NewDurableStep/NewHostActionStep/NewAgentStep/NewHumanAskStep/NewParallelStep` 构造六个最小合法步骤，打印 `step.Authority()` 与 `step.RunnerKind()`。
- 判据：六对值恰为 LOCAL→ENGINE/ENGINE_LOCAL、DURABLE→ENGINE/DURABLE_ACTIVITY、HOST_ACTION→ENGINE/HOST_ADAPTER、AGENT→AGENT/AGENT_WORKER、HUMAN_ASK→HUMAN/HOST_ADAPTER、PARALLEL→ENGINE/ENGINE_LOCAL。失败信号：任一映射不符或构造报错。

**QA-B02｜变体只暴露适用字段：非法字段引用在编译期不可表示**（条目 1）
- 步骤：写三个驱动：①对 `authoring.LocalStep` 值设置 `.Join` 字段；②对 `HumanAskStep` 设置 `.Retry`/`.IO.InputCodec`；③对 `ParallelStep` 设置 `.Handler`；分别 `go build`。另写对照驱动只用各变体真实存在字段，`go build` 应成功。
- 判据：①②③编译失败，错误含 `unknown field`；对照驱动编译成功。失败信号：任一非法字段引用编译通过（说明变体暴露了不适字段）。

**QA-B03｜constructor 拒绝 local/durable 的必填缺失与非法重试/超时**（条目 1；5.8）
- 步骤：驱动逐项构造：local 缺 handler、timeout 为负、Retry.MaxAttempts=0、Backoff 为负；durable 缺 handler、缺幂等策略（零值）、缺 ReconcileID、timeout=0、Retry 必填缺失；合法对照各一。打印每个 error。
- 判据：每个非法入参返回非 nil error 且消息点名缺失义务（如 `handler id required`、`idempotency key strategy required`、`reconcile id required`、`positive timeout required`、`maxAttempts must be >= 1`）；合法对照 err==nil。失败信号：任一非法入参被接受或错误张冠李戴。

**QA-B04｜constructor 拒绝 host/agent/human/parallel 的必填缺失与非法组合**（条目 1；5.8）
- 步骤：驱动构造：host 缺 boundary（零值）、缺 operation、timeout=0；agent 缺 reason（含非法字符串 `"实现麻烦"`）、Postconditions 为空、timeout=0；human 缺 AskKind/RequestSchema/ResponseSchema、TTL=0；parallel children 去重后仅 1 个、join step 同时是 child、缺 join mode/failure mode/escalate；各配合法对照。
- 判据：全部返回非 nil error，消息含枚举合法值提示（`EXTERNAL_CAPABILITY_BOUNDARY|USER_IO_TRANSPORT|AGENT_DISPATCH_API`、`SEMANTIC_JUDGMENT|CREATIVE_IMPLEMENTATION|INDEPENDENT_REVIEW`、`ALL|ANY`、`FAIL_FAST|WAIT_ALL`）与 `at least 2 children`、`must not be a child`；对照通过。失败信号：任一非法组合构造成功。

**QA-B05｜公共头/依赖/IO 段常见失误的拦截与归一化**（条目 1；5.8）
- 步骤：驱动构造：空 StepID、空 NodeID、空 DefinitionVersion；Dependencies 含空串、含自引用、含重复 `[b,a,b]`；IO 缺 InputCodec/OutputCodec、predicate 空 ID、binding 空 From。合法路径用 `[b,a,b]` 传入并打印返回的 Dependencies。
- 判据：空 ID/自引用/空引用各返回点名 error；`[b,a,b]` 被归一化为排序去重的 `[a,b]`。失败信号：未归一化（顺序/重复泄漏）或非法输入被接受。

**QA-B06｜绕过 constructor 的零值/typed-nil 步骤被 compiler 二次防线拒绝**（条目 1、2；5.8）
- 步骤：驱动一：直接用结构体字面量 `authoring.LocalStep{Header: authoring.Header{ID:"s", NodeID:"entry", DefinitionVersion:"2"}}`（IO 零值）放入 `compiler.Definition.Steps`，调 `Compile`，`errors.As` 断言 `*compiler.Error` 并打印 `Class`。驱动二：把合法 local 步骤以指针 `&step` 放入 Steps，调 `Compile`。
- 判据：驱动一返回 error 含 `input codec id required (untyped IO)`，Class==`INVARIANT_VIOLATION`；驱动二返回 `unknown step variant *authoring.LocalStep`。失败信号：零值/指针步骤被编译接受。

## B. Compiler 图不变量与 registry（验收条目 2；5.7、5.9）

**QA-B07｜definition 信封层常见失误**（条目 2）
- 步骤：驱动对最小合法定义分别做：`Compile(nil, reg)`、`Compile(def, nil)`、Version 空、EntryNode 空、Steps 空、两步同 ID；打印各 error。
- 判据：六个输入各自返回可区分的 error（`nil definition`、`nil registry`、`definition version required`、`entry node required`、`definition has no steps`、`duplicate step id`）。失败信号：panic、或任一被静默接受。

**QA-B08｜步骤版本未绑定信封被拒（八类拒绝之一）**（条目 2；5.9 mutation）
- 步骤：驱动构造单步 local 定义，Header.DefinitionVersion 填 `"1"` 而信封 Version 为 `"2"`，`Compile`。
- 判据：error 含 `unbound definition version` 与两个版本值。失败信号：版本不一致的定义被编译通过。

**QA-B09｜依赖不存在与依赖循环分别报可区分错误**（条目 2；5.9 mutation）
- 步骤：驱动一：步骤 X 依赖不存在的 `nope`。驱动二：A 依赖 B、B 依赖 A（先经 constructor，依赖合法形态）。各自 `Compile` 并打印 error。
- 判据：驱动一报 `dependency "nope" not found`（不是 cycle）；驱动二报 `dependency cycle among steps`。失败信号：报错类型混淆或被接受。

**QA-B10｜不可达步骤与入口无根被拒**（条目 2；八类）
- 步骤：驱动一：入口节点单步 a 合法 + 另加孤立步骤 z（node 非 entry、无依赖、无人依赖）。驱动二：entry 节点内唯一步骤带依赖（无根）。各自 `Compile`。
- 判据：驱动一报 `unreachable steps` 含 `z`；驱动二报 `entry node ... has no dependency-free step`。失败信号：孤立步骤或无根图被接受。

**QA-B11｜并行组四类图违规被拒**（条目 2；八类；5.9 mutation）
- 步骤：驱动基于一个合法 5 步并行定义（anchor a；children c1,c2；join j 依赖 c1,c2）依次变异再 `Compile`：①parallel 步去掉 anchor 依赖；②join 去掉对 c2 的依赖（保留 c2 的 input binding 同时删除以只命中覆盖校验）；③join 额外依赖组外步骤 o；④组外步骤 o2 依赖 c1（绕过 join 消费）。
- 判据：①报 `fan-out anchor dependency required`；②报 `does not depend on child`（fan-out coverage）；③报 `depends on ... outside children`；④报 `child ... has dependent ... other than join`。失败信号：任一变异被接受。

**QA-B12｜typed input bindings 与依赖集合必须精确相等**（条目 2）
- 步骤：驱动构造双步定义（a→b）：①b 的 Inputs 绑定来源是非依赖步骤 z；②b 依赖 a 但 Inputs 不绑定 a。各自 `Compile`。
- 判据：①报 `input binding source "z" is not a dependency`；②报 `dependency "a" has no typed input binding`。失败信号：不对称绑定被接受。

**QA-B13｜未注册 ID 在正常 Compile 路由 BLOCKED_BUG 并拒签发**（条目 2、7）
- 步骤：驱动构造最小 local 定义，registry 未注册其 codec `codec.missing`，`Compile` 后 `errors.As(*compiler.Error)` 打印 Class 与消息；再分别换用未注册 handler/predicate/schema/operation/reconciler 各跑一次。
- 判据：每例 err 非 nil，Class==`BLOCKED_BUG`，消息含 `MISSING_ENGINE_ADAPTER`、`not registered (closed world)` 与 `use diagnostic compile`。失败信号：Class 为 INVARIANT_VIOLATION 之外的错类、或编译成功产出定义。

**QA-B14｜registry 误用：重复/空 ID 注册期拒绝、错槽/kind 与 runner 错绑解析期拒绝**（条目 2；5.7）
- 步骤：驱动：①`RegisterHandler("h",ENGINE_LOCAL)` 后再次 `RegisterHandler("h",...)`；②先 RegisterHandler("x") 再 RegisterCodec("x")；③`RegisterHandler("h","")`（非法 runner）；④注册空 ID；⑤`ResolveCodec("h")`（handler 填进 codec 槽）；⑥构造 agent 步骤但 handler 以 `ENGINE_LOCAL` runner 注册，`Compile`。
- 判据：①②报 `registry: duplicate id`；③④报 invalid runner / empty id；⑤报 `registered as handler, want codec`；⑥报 `runner ... != variant runner`（INVARIANT_VIOLATION）。失败信号：重复 ID 被接受（唯二性失守）或 kind 错绑漏到运行期。

**QA-B15｜编写顺序不泄漏：乱序引用编译后字节相同**（条目 2 归一化；5.2）
- 步骤：驱动构造同一逻辑定义两遍：A 的 Dependencies/Preconditions（含 Negated 混排）/Inputs 均按乱序与重复传入 constructor；B 全部按排序、无重复传入。两者各自 `Compile`+`encoder.Encode`，比较字节。
- 判据：两份制品字节完全相同（`bytes.Equal`/sha256 一致）。失败信号：作者书写顺序影响制品字节。

## C. Canonical 制品与生成器（验收条目 3；5.1–5.6）

**QA-B16｜生成器确定性：连跑、跨进程、跨副本产物字节一致且无环境泄漏**（条目 3；5.4）
- 步骤：`$WS` 下 `go run ./cmd/gen-definition`，记录 `definitions/workflow.json` 与 `identity_gen.go` 的 sha256；再跑一次比对；把 `$WS` 完整复制到 `/tmp/wsB`，在 wsB 根再跑一次，三方比对；在制品字节中搜索 `$WS` 绝对路径字符串与当前日期串。
- 判据：三次产物两文件 sha256 全部一致；制品不含绝对路径/时间戳。失败信号：任一次字节漂移或出现环境相关信息。

**QA-B17｜freshness：重新生成 checked-in 制品零 diff（独立于 round-trip）**（条目 3；5.1）
- 步骤：在 `$WS` 执行 `go run ./cmd/gen-definition` 后 `git -C $WS status --porcelain -- definitions internal/engine/definition/identity_gen.go`。
- 判据：输出为空（checked-in 制品与重生成逐字节一致）。失败信号：出现 M/A 状态行（人工双写或漂移）。

**QA-B18｜身份同源：外部 sha256 == 生成常量，双文件同动作产出**（条目 3）
- 步骤：`shasum -a 256 $WS/definitions/workflow.json`；驱动打印 `definition.WorkflowDefinitionVersion` 与 `definition.WorkflowDefinitionDigest`；再调 `definition.Generate("/tmp/genroot")` 并对 `/tmp/genroot` 下两文件与 `$WS` checked-in 版本逐字节 diff。
- 判据：`WorkflowDefinitionDigest == "sha256:"+shasum 十六进制`；Version==制品 `"version"` 字段=="2"；临时根两文件与 checked-in 完全一致（无人工复制的 digest 漂移）。失败信号：常量与制品字节摘要不符或双文件不同源。

**QA-B19｜assembly/注册顺序不变性**（5.2）
- 步骤：驱动：`d1 := definition.Workflow()`；`d2 := definition.Workflow()` 后将 `d2.Steps` 全排列（如倒序+旋转）；registry R1 由 `definition.Registry()` 得到，R2 按 ID 列表逆序逐条注册（列表从制品字节推导：各步骤 payload handler + 步骤 runner、codec.any.in/out、pred.review.post、reconcile.entry.persist、两个 schema、op.fan.transport）。分别 `Compile+Encode`，比较字节与 digest。
- 判据：四组组合（d1/R1、d2/R1、d1/R2、d2/R2）制品字节完全相同。失败信号：任一顺序组合改变制品。

**QA-B20｜round-trip：decode→encode 字节不变**（5.3）
- 步骤：驱动 `data := os.ReadFile("$WS/definitions/workflow.json")`；`cd := encoder.Decode(data)`；`out := encoder.Encode(cd)`；`bytes.Equal(data,out)` 并打印两侧 sha256。
- 判据：相等成立。失败信号：decode→encode 引入任何字节差异。

**QA-B21｜只改实现不改 ID/语义：definition digest 不变，制品无实现字节**（条目 3；5.5）
- 步骤：复制 `$WS` 到 `/tmp/wsImpl`；在 wsImpl 对 `internal/engine/runtime/batch.go` 追加一行注释（实现字节变化、ID 与定义语义不变）；在 wsImpl 根跑 `go run ./cmd/gen-definition`；比对两副本制品 sha256；并在制品文本中 grep `internal/`、`func `、`.go`。
- 判据：制品 sha256 与原副本一致；制品不含任何 Go 实现引用（只有封闭 registry ID）。失败信号：实现编辑改变 definition digest（说明实现字节泄入制品）。注：package digest 变化的另一半属安装事务批次，本阶段无公开面，不作判据。

**QA-B22｜digest 语义敏感性：六维改动必变**（5.6）
- 步骤：驱动用公开 API 构造六对最小定义（同一 registry 注册全部用到的 ID），每对只差一个维度：①增/删一条依赖；②durable retry MaxAttempts 2→3；③agent reason SEMANTIC_JUDGMENT→INDEPENDENT_REVIEW；④human requestSchema s1→s1b；⑤local handler h1→h1b（同 runner）；⑥parallel join Mode ALL→ANY。每对分别 `Compile+Encode+Digest`；附对照：同一定义编译两次 digest 相等。
- 判据：六对 digest 两两不同；对照相等。失败信号：任一语义维度改动后 digest 不变。

**QA-B23｜decode 严格拒绝被篡改/非规范制品**（条目 3）
- 步骤：驱动以 checked-in 字节为基线做七种篡改后调 `encoder.Decode`：①文档后追加第二个 JSON 对象；②信封加未知键 `"extra":1`；③某步骤 payload 加未知键；④`writer` 改为 `"validate"`；⑤某 payload 置 `null`；⑥给 HUMAN_ASK 步骤挂 `io` 块/删去 LOCAL 步骤的 `io` 块；⑦把某 LOCAL 步骤 `kind` 改为 `HUMAN_ASK`（payload 不动）。
- 判据：①报 `trailing content`；②③报 unknown field（外层/内层 DisallowUnknownFields）；④报 `envelope writer` 不符；⑤报 `payload must be a ... object, got null`；⑥报 `must not carry an io block`/`requires an io block`；⑦报 payload 与 kind 不符。全部拒绝、无静默归一化。失败信号：任一篡改被接受或被“修复”解码。

## D. 决策核心（验收条目 4）

**QA-B24｜RunPhase 迁移表：合法边放行、非法边拒绝、终止闭包完备、表副本可变不影响权威**（条目 4）
- 步骤：驱动：`PhaseTransition(INTAKE_REGISTERED,PRODUCT_REVIEW)`、`(POST_REVIEW,REPAIR)`、`(REPAIR,SNAPSHOT_READY)` 应 nil；`(INTAKE_REGISTERED,TECHNICAL_REVIEW)`（跳步）、`(SNAPSHOT_READY,DEVELOPMENT_PARALLEL)`（回退）、自环、`(TERMINAL,INTAKE_REGISTERED)` 应 error；遍历全部 phase 断言非 TERMINAL 者 `PhaseCanTransition(p,TERMINAL)==true`、TERMINAL 无出边；取 `PhaseTransitionTable()` 副本改写其元素后再取一次并复查 `PhaseCanTransition`。
- 判据：如上全部成立；非法边 error 含 `illegal phase transition`；改写副本后权威查询不变。失败信号：跳步/回退被放行或静态表被调用方污染。

**QA-B25｜TaskKey 构造校验与规范字符串**（条目 4）
- 步骤：驱动：`NewTaskKey("n","s","")`→`"n/s"`；带 scope→`"n/s/sc"`；`NewTaskKey("","s","")`、`("n","","")` 应 error；同一键两次 String() 相等。
- 判据：如上；error 含 `requires node and step`。失败信号：空段键被接受或字符串形态不稳定。

**QA-B26｜TaskTransitionTable 只前进：合法七边放行、回退/跳步/自环拒绝**（条目 4）
- 步骤：驱动：对 `TaskTransitionTable()` 七条合法边逐条 `TaskTransition` 应 nil；`TERMINAL→QUEUED/RUNNING`、`RUNNING→ISSUED`（回退）、`QUEUED→VALIDATING`（跳步）、`ISSUED→VALIDATING`（跳过 RUNNING）、各自环应 error。
- 判据：七条全过、其余全拒且 error 含 `illegal task transition`。失败信号：任务可回拨或跳状态。

**QA-B27｜State 构造与 CompleteStep 运行时边界四类拒绝**（条目 4）
- 步骤：驱动以 `definition.Workflow()+definition.Registry()` 编译真实定义：`NewState("","ACTIVE类合法phase")`、`NewState("2","BOGUS")` 应 error；state 版本 `"1"` 时 `CompleteStep("entry.parse")` 应报版本不符；合法 state 上完成不在定义中的 `"nope"`、重复完成 `entry.parse`、先完成 `review.worker`（依赖未齐）各应报 `not in definition`/`already completed`/`dependency ... not completed`；按序完成 `entry.parse` 应成功且 `Completed` 有序。
- 判据：四类拒绝各自消息可区分；合法路径成功。失败信号：乱序/重复/未知完成被接受。

**QA-B28｜State 的 TransitionPhase/TransitionTask 按静态表执行**（条目 4）
- 步骤：驱动：合法 state `TransitionPhase(DEVELOPMENT_PARALLEL→SNAPSHOT_READY)` 成功后再 `→INTAKE_REGISTERED` 应 error；`TransitionTask(NewTaskKey(...),ISSUED)` 成功→`RUNNING` 成功→再 `ISSUED` 应 error；`TransitionTask` 传无效键应 error；断言 `Phase`/`Tasks` 实际被更新。
- 判据：如上；非法迁移后 state 字段不变。失败信号：State 包装绕过静态表。

**QA-B29｜Observe 校验与收集器顺序不变性**（条目 4）
- 步骤：驱动实现 `Collector` 接口的桩（只返回事实，不写盘）：①来源枚举非法（`"BOGUS"`）；②事实 Source 与收集器不符；③两收集器产生同 (Source,Key) 重复；④空 Key；⑤nil collector；⑥收集器返回 error；各应报错。合法路径：两个收集器（VCS/FILE 各 2-3 条事实）以两种相反顺序传入两次 `Observe`，比较 `CanonicalBytes`/`Digest`；空收集器列表观察打印字节。
- 判据：①-⑥均报含来源名的 error；两种顺序 canonical 字节与 digest 相同；空观察字节为 `"facts": []` 形态。失败信号：矛盾事实被汇入或收集器顺序泄漏进字节。

**QA-B30｜Decide 对不一致输入的确定性错误**（条目 4）
- 步骤：驱动编译真实定义后：`Decide(nil,...)`、`Decide(state,nil)`；state 版本与定义不符；直接构造 `State{Completed:["nope"]}`；`Completed` 含重复；`Completed=[某步]` 但其依赖未含——各调 `Decide`。
- 判据：各返回可区分 error（nil、版本不符、`not in definition`、`duplicated`、`before dependency`），绝不返回空 Plan。失败信号：不一致 state 产出静默结果。

**QA-B31｜eligible frontier 完整且按 ordinal 固定排序**（条目 4）
- 步骤：驱动：初始 `Decide` 打印 Frontier；完成 `entry.parse` 后再 `Decide` 打印 Frontier。
- 判据：初始恰为 `[{entry.parse,entry,0,LOCAL}]`；之后恰为 `[{entry.persist,entry,1,DURABLE},{fan.split,fan,3,PARALLEL}]`（完整、按 ordinal 升序）。失败信号：frontier 缺项、多项或顺序漂移。

**QA-B32｜Kind 优先级阶梯与 Wait 原因：READY→ASK→WAIT(TASKS_IN_FLIGHT)→HOST_ACTION→COMPLETE**（条目 4）
- 步骤：驱动对真实定义走完整流程并逐步打印 Next：完成 `entry.parse`+`entry.persist` → 应 `READY`（tasks 恰为 `review/review.worker`）；`TransitionTask` 该键→ISSUED 再 Decide → 应 `ASK`（steps=[ask.decide]）；完成 `ask.decide` 再 Decide → 应 `WAIT/TASKS_IN_FLIGHT`；完成 `fan.split` → 应 `HOST_ACTION`（steps=[fan.transport]）；最后完成全部剩余步骤再 Decide → 应 `COMPLETE` 且 `Validate()` nil。
- 判据：五段输出与上述完全一致（agent 先于 ask、已签发任务退出可签发集、全完成进终态）。失败信号：任一段 Kind 或 payload 与阶梯不符（如 Ask 伪装 Wait、已签发任务重复入 Ready）。

**QA-B33｜Plan 字节级稳定：重复调用与 map 插入序无关**（条目 4）
- 步骤：驱动：同一 state+observation 连续两次 `Decide`，比较 `CanonicalBytes` 与 `Digest`；构造两个逻辑等价 State（Tasks map 以相反插入序填充同样的键值），比较 State `CanonicalBytes`；把两次打印的 PlanDigest 输出，驱动跑两遍（两个进程）比对。
- 判据：两两字节/摘要一致，跨进程一致。失败信号：map 遍历序或重复调用引入字节差异。

**QA-B34｜NextResult 六类 Kind tagged-union 误用被 Validate 拒绝**（条目 4）
- 步骤：驱动手工构造 `NextResult`：Kind 为空串/`"READY"` 但 Ready 为 nil/Kind=ASK 但同时设 Ask 与 Ready/Kind=WAIT 但 Wait 与 Complete 并存，各调 `Validate()`。
- 判据：四种均报错且消息指明 payload presence 不符；`Validate` 对合法单 payload 组合返回 nil。失败信号：多 payload 或缺 payload 通过校验。

**QA-B35｜SelectIssued 容量语义、固定顺序、确定性 actionID 与落账契约**（条目 4）
- 步骤：驱动构造自定义三步定义（local a；agent b、c 依赖 a，registry 备齐）并完成 a 得 READY plan（tasks=[b,c]）：capacity=0/1/5 三次签发（各配记录型 ActionStore）；capacity=-1；对 Kind=WAIT 的 plan 签发；store 传 nil；store 返回 error。打印 IssuedSet 顺序与 ActionID。
- 判据：capacity=0 → 空集（仍调用 PersistIssued）；capacity=1 → 恰 `[b]`（plan 顺序取前 k）；capacity=5 → `[b,c]` 依 plan 顺序；负容量/非 READY/nil store/持久化失败各报对应 error；ActionID 形如 `act:n/b` 且两次相同输入相同。失败信号：跳过前 k 挑选子集、空容量签发、或 actionID 不确定。

**QA-B36｜Batch 完成状态从成员 task 派生**（条目 4；ADR-002）
- 步骤：驱动构造 `Batch{Tasks:[k1,k2]}`：statuses 空 map / 仅 k1 TERMINAL / 双 TERMINAL / 含未知键，各调 `Complete`；空成员批 `Complete(nil)`。
- 判据：false/false/true/false；空批 true。失败信号：派生规则偏离全称定义。

## E. 只读 Shadow（验收条目 6）

**QA-B37｜Shadow 对真实构造 legacy state 的完整报告内容**（条目 6）
- 步骤：驱动在 `/tmp/qa-proj` 写出 `.gates/tmp/r1/state.json`（现有格式：runId=r1、status=ACTIVE、actions 中 requirements-clarification与product-review 均 PASS、start-readiness 未记录、selectedGates=[gate.review,gate.build]、VCS/FILE 可映射字段赋值、gates 空），调 `shadow.Run(Options{Root:"/tmp/qa-proj",RunID:"r1",OutputDir:"/tmp/qa-out"})`，打印 Report 关键字段；另在 shell 侧 `shasum -a 256` 该 state.json。
- 判据：projectedPhase==`START_READINESS`；projectedCompleted==[]；Facts 恰含非空 VCS（vcs/baseSnapshot/currentSnapshot）与 FILE（四个 revision）事实且按 (Source,Key) 排序；unavailableSources 恰为 HOST/LIFECYCLE/RECEIPT/CAPACITY 四项带原因；prediction.frontier==[{entry.parse,entry,0,LOCAL}]、nextKind==WAIT、nextReason==ENGINE_INTERNAL、definitionDigest==`definition.WorkflowDefinitionDigest`；observedStateSha256==`"sha256:"+shasum` 结果；报告写入 OutputDir。失败信号：任一投影字段、事实集或摘要不符。

**QA-B38｜Shadow 只读性与幂等：观测对象零改动、两次运行报告逐字节相同**（条目 6）
- 步骤：记录 `/tmp/qa-proj/.gates/tmp` 全树列表与 state.json 的 sha256+mtime；用 37 的输入连跑两次 `shadow.Run`（同 OutputDir 与不同 OutputDir 各一组）；cmp 两次报告文件字节与 reportDigest；复查 tmp 树与 mtime；再以 `OutputDir:""` 跑第三次，检查默认目录 `<root>/.gates/shadow/r1.shadow.json` 生成；报告 JSON 中 grep `/tmp/qa-proj` 绝对路径。
- 判据：两次报告字节相同、digest 相同；被观测 state 的字节、mtime、目录树零变化；默认输出落 `.gates/shadow/`；报告不含绝对 root 路径（observedStatePath 为相对形态）。失败信号：`.gates/tmp` 出现新文件/被改写，或报告字节不确定。

**QA-B39｜五类 legacy 状态的 phase 投影与三类判定覆盖**（条目 6）
- 步骤：驱动构造五个状态文件并分别 Run：f1 ACTIVE 全空（应 INTAKE_REGISTERED，actual=`action:requirements-clarification`）；f2 动作全 PASS 且 gate.review 待执行（SNAPSHOT_READY，actual=`gate:gate.review`）；f3 gate.review PASS、gate.build FAIL（POST_REVIEW，actual=`gate:gate.build:repair`）；f4 动作与门全 PASS（projected TERMINAL，prediction COMPLETE，actual=`seal`）；f5 status=SEALED（TERMINAL，MATCH）。
- 判据：f1/f2/f3 投影 phase 如上且 verdict==INCOMPARABLE（预测为 WAIT）；f4 verdict==MISMATCH；f5 verdict==MATCH 且 actual boundary==TERMINAL。三类判定值全部出现且各有正确归属。失败信号：投影错位或 MATCH/MISMATCH 方向颠倒。

**QA-B40｜Shadow 常见操作失误防线**（条目 6）
- 步骤：驱动分别以：RunID=`a/b`、`..\x`、空串；Root 空串；RunID 指向不存在目录；state.json 写为非法 JSON；state 内 runId=`other` 与请求不符；status=`PAUSED`——各调 `Run`。
- 判据：分别报 `must not contain path separators`、`run id required`、`root required`、read 失败、`JSON is invalid`、`does not match`、`not ACTIVE/SEALED/ABORTED`；全部不产生输出文件。失败信号：路径分隔 run id 被接受（telemetry 逃出输出目录）或错误状态被静默观测。

## F. MISSING_ENGINE_ADAPTER 契约（验收条目 7）

**QA-B41｜diagnostic-only 模式：marker 物化、可诊断、不可编码/不可决策**（条目 7）
- 步骤：驱动以 QA-B13 的未注册 codec 定义：`CompileDiagnostic` → 断言返回 DiagnosticResult、`Definition.MissingEngineAdapter==true`、Diagnostics==[{step,ref,want:codec}]；随后对带 marker 的 cd 依次调 `encoder.Encode`、`decision.Decide(合法state,空obs,cd)`。
- 判据：CompileDiagnostic 成功且诊断三元组正确；Encode 报 `MISSING_ENGINE_ADAPTER marker; diagnostic-only definitions must not become the canonical artifact`；Decide 报同类错误（经 Encode 拒绝），不产出 Plan——即结构上不可能签发。失败信号：marker 定义可编码成制品或可进入 executable plan。

**QA-B42｜最终候选无该技术债：shipped 制品 marker 为零、正常编译可达**（条目 7）
- 步骤：`grep -c "MISSING_ENGINE_ADAPTER" $WS/definitions/workflow.json $WS/internal/engine/definition/identity_gen.go`；在 `$WS` 跑 `go run ./cmd/gen-definition`（成功即证明 shipped 定义在正常 Compile 下全 ID 可解析）。
- 判据：两个文件计数均为 0；生成器退出码 0。失败信号：制品/身份常量含 marker，或正常编译因未注册 ID 失败。

## G. 环境隔离与 legacy 兼容（验收条目 8、9）

**QA-B43｜文档化入口全程不触碰用户级安装/registry 路径**（条目 8 可黑盒观察部分）
- 步骤：执行前记录 `~/.formal-gates` 与 `~/.local/bin/formal-gates`（及其符号链接目标）的存在性、全树文件列表与逐文件 sha256；随后在真实 HOME 下执行本用例集全部文档化入口：`go run ./cmd/gen-definition`、`go build` 出的驱动二进制（编译/编码/决策各一）、shadow 驱动二进制（root 指向 /tmp 项目）；执行后再次采集清单与摘要比对。
- 判据：前后清单与摘要完全一致（或两时刻均不存在该路径）。失败信号：任何文档化入口在用户级路径创建/修改文件（阶段 0 故障注入缺陷的回归面）。注：launcher smoke 与重冻结留证由主代理持有，不在本用例登记。

**QA-B44｜v2 制品保持 phase-0 文档化读取器可解析（信封兼容承诺）**（条目 3、9）
- 步骤：驱动调 `validate.LoadFutureDefinition($WS)`（阶段 0 冻结的制品读取入口），打印返回的 WorkflowVersion/SchemaVersion/Digest 与 error；同时驱动打印 `definition.WorkflowDefinitionDigest`。
- 判据：err==nil（尤其不是 `*UnsupportedRunVersionError`）；WorkflowVersion==`"2"`、SchemaVersion==`"1"`、Digest==definition digest（即 v2 信封沿 stateSchemaVersion/version 字段被 legacy 读取器按未来候选正确识别）。失败信号：v2 制品被 legacy 读取器拒绝或字段缺失。

## H. 复杂度止损（验收条目 5.10）

**QA-B45｜新增普通业务节点零改 compiler core：纯公开 API 扩展节点并编译**（条目 2；5.10）
- 步骤：驱动：取 `definition.Workflow()` 的 Steps 追加一个新普通 local 步骤（`report.audit`，node `report`，deps+bindings 指向 `report.cost`，新 handler `engine.report.audit` 在驱动自建 registry 中以 ENGINE_LOCAL 注册，其余 ID 沿用）；`Compile+Encode+Decode` 全链执行；随后 `git -C $WS status --porcelain -- internal/engine/compiler internal/engine/encoder internal/engine/authoring`。
- 判据：编译、编码、解码全部成功，新制品含新步骤且 digest 与原制品不同；`internal/engine/compiler|encoder|authoring` 零改动（工作区无 diff，驱动目录除外）。失败信号：新增普通节点要求修改 compiler/encoder/authoring 源码才能编译（止损触发）。

---

## 覆盖核对

- 条目 1（Authoring）：B01–B06；条目 2（compiler/八类拒绝/分层拦截）：B06–B15、B23、B45；条目 3（canonical 制品）：B16–B23、B44；条目 4（决策核心）：B24–B36；条目 5 十条独立验收：5.1→B17、5.2→B15/B19、5.3→B20、5.4→B16、5.5→B21、5.6→B22、5.7→B14、5.8→B03–B06、5.9→B08/B09/B11/B13、5.10→B45；条目 6（Shadow）：B37–B40；条目 7（MISSING_ENGINE_ADAPTER）：B13/B41/B42；条目 8（隔离）：B43；条目 9（legacy 回归，库面切片）：B44。
- 全部用例以文档化产品入口实际使用产品（`go run ./cmd/gen-definition`、`go build` 产物、公开 Go API 驱动、真实文件制品外部观察），不以内嵌测试运行为步骤或证据；未发明对抗性/手工改状态/权限/不支持平台用例；未登记 CLI 用例（归主代理）。
---

## I. 前置修复批行为面（2026-08-22 补充，QA-B46~B52，对应 post-seal-audit H1-H6 与 QA-B13 修复）

**QA-B46｜自 join 并行步双层拒绝**（H1/条目 2）
- 步骤：驱动①经 `authoring.NewParallelStep` 构造 `Join.JoinStep == 自身 ID` 的并行步；②绕过 constructor 以合法形态结构体字面量构造同款（deps/children/join 齐备、join==自身）放入 `compiler.Definition.Steps` 调 `Compile`。
- 判据：①constructor 返回非 nil error 含 `join step must be outside the parallel group`；②compiler 二次防线同样拒绝（同类消息）。失败信号：任一层接受自 join 定义（审计 H1 复现形态回归）。

**QA-B47｜并行组归属排他**（H2/条目 2）
- 步骤：驱动构造两个 PARALLEL 步共享同一 children 集合（同 join）；再构造共享同一 join 但 children 不同的变体；各调 `Compile`。
- 判据：两形态均拒绝，消息含 `parallel group ownership is exclusive` 或同义可区分文案。失败信号：同一 child/join 被多组声明仍编译通过。

**QA-B48｜字符集约束两层一致且决策端确定性失败**（H4/条目 4）
- 步骤：驱动①`runtime.NewTaskKey("n/a","s","")`、`("n","s/x","")`、`("n","s","sc\\x")` 等段内含 `/` 与 `\` 的键；②`authoring.NewLocalStep` 以 `StepID:"a/b"`、`NodeID:"x/y"`、`StepID:"a\\b"` 构造；③以原始结构体（绕过 constructor 校验）构造含 `/` ID 的步骤喂 `decision.Decide`（经 compiled 定义或直接，取实际可行路径）。
- 判据：①②均拒绝且消息含字符约束说明（两层文案一致拒 `/` 与 `\`）；③确定性返回错误（含 task key 约束），不再静默产出可碰撞键。失败信号：任一层接受段内分隔符（审计 H4 碰撞形态回归）。

**QA-B49｜生成器写出完整性与无残留（本机平台）**（H5/条目 3）
- 步骤：驱动在临时根并发跑 `definition.Generate` 20 轮（同内容）同时读者循环持续读取两交付物；结束后检查目录无 `.gen-definition-*` 临时残留；`stat` 两文件权限与内容（与 checked-in 逐字节一致）。
- 判据：读者全程只观测到完整合法内容（JSON 可解析、Go 源完整）、无撕裂；零临时残留；权限 0644（本机 Unix）。失败信号：撕裂读、残留临时文件或内容不完整（审计 H5 回归；Windows rename 行为已按用户决定延期）。

**QA-B50｜shadow 拒绝父目录引用 runID**（H6/条目 6）
- 步骤：驱动以 `RunID:".."` 调 `shadow.Run`（root 指向临时项目）；检查返回错误与输出目录文件数。
- 判据：返回非 nil error 含 `must not be a parent-directory reference`；零输出文件、被观测 root 零改动。失败信号：`..` 被接受或观测/输出路径越出预期（审计 H6 回归）。

**QA-B51｜durable retry 严格 decode 与结构二次校验**（H3/条目 3）
- 步骤：驱动以 checked-in 制品字节为基线做五种篡改后调 `encoder.Decode`：①删除某 DURABLE payload 的 `retry` 键；②`retry` 置 null；③复制某步骤整个条目造成重复 step id；④复制造成重复 ordinal；⑤把某步骤 dependency 改为不存在 id。
- 判据：①②报 `requires a retry object`；③④⑤分别报重复/悬空的可区分错误；全部拒绝、re-encode 不发生静默补默认值。失败信号：零值接受或静默归一（审计 H3 回归）。

**QA-B52｜BLOCKED_BUG 提示语完整（六槽位）**（QA-B13 修复/条目 7）
- 步骤：驱动对 codec/handler/predicate/schema/operation/reconciler 六类 ID 各构造一个未注册引用的最小定义，正常 `Compile`，`errors.As` 取 `*compiler.Error` 打印 Class 与完整消息。
- 判据：六例 Class 均 `BLOCKED_BUG`，消息同时含 `MISSING_ENGINE_ADAPTER`、`not registered (closed world)`、`use diagnostic compile`。失败信号：任一槽位缺提示（QA-B13 原 FAIL 形态回归）。

**覆盖核对（补充节）**：H1→B46、H2→B47、H4→B48、H5→B49、H6→B50、H3→B51、QA-B13→B52；至此审计 7 项修复全部有独立黑盒用例。
