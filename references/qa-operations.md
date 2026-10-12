# QA 固定操作契约

`coverage.AcceptanceCase.operations` 与 `coverage.SharedStep.operations` 是 QA 设计
创作的有序操作载荷。它们随完整 case/shared-step 内容进入既有审查身份、manifest digest
和冻结。步骤、前置说明与 oracle 表达固定语义；完整可照做的批准用例也可暂不声明命令。
此时 `ValidationInput.commandCases` 列出缺 invocation 的用例，执行者只按注入顺序返回
`commands` 操作数组。程序把首次填入的原值绑定到当前候选、用例和原 native intent，
经 execution checkpoint 继续受管执行；绑定后不能重填，不改变批准步骤或 oracle。
程序不从自然语言猜命令，执行者也不自行运行 shell。

每个操作只允许一种形态，可附带原样发送的 `stdin`：

```json
{"kind":"ARGV","argv":["tool","","two words","\"quoted\""],"stdin":"input\n"}
{"kind":"SHELL","command":"printf '%s' \"$value\"\n"}
```

首次设计、设计续作、指定用例修订和缺 invocation 的 `commands` 使用同一原值选择。
`command`、`stdin`、每个 `argv` 参数和共享 `artifacts` 元素可填写真正新创作的字符串，
或用 `{"read":1,"from":2,"to":3}` 选择原 workspace read 的一基序号与闭区间行号。
程序从原 native LocationFacts 取得该区间的完整字节，保留换行、空格与引号，不解析
shell 参数、不去除换行、不猜路径。argv 的每个元素仍是单独参数；要选择无末尾换行
的参数或产物名，其原 read 区间本身必须含有该原值。语义提交与原结果装配共用原 native facts，
逐值核真实 read、区间及最终操作。恢复消费同一原事实，不能用新的读取替换原来源。

修改已存在 operations 时，`{"reuse":1}` 选择原操作序号，省略字段保留原值。
显式数组决定最终顺序；未选择项删除，`[]` 清空。argv 与 artifacts 的元素还可用
`{"reuse":1}` 选择该原字段的元素序号。新项没有可复用的原元素；command 与 stdin
省略时保留原值，填写时仅使用新字符串或 read 来源。切换 kind 清除不适用的原
command 或 argv，并须提供相应的新调用。最终批准与执行载荷始终只含具体原字符串。

`ARGV` 必须提供非空数组和非空 executable，不能提供 `command`；后续参数可以是空
字符串，空格、引号和特殊字符都是该参数的原值。`SHELL` 必须提供非空 `command`，
不能提供非空 `argv`。操作不含 cwd、env、插件、条件或循环字段。完整 shell 文本可以
表达已批准的环境设置或目录切换；程序不改写文本。执行目录由 registered QA workspace
提供。程序同时从这个 registered workspace 注入 `FORMAL_GATES_WORKSPACE` 环境变量；
需要根目录的 shell 操作用 `"$FORMAL_GATES_WORKSPACE"` 引用，其他文件使用仓库相对路径。
模型不填写物理工作区路径；已批准的 `command`、`argv` 与 `stdin` 保持原文。固定物理
目录的原操作必须经原设计/审查责任修订，程序不会把它改成另一工作区。
每例实际注入独立的 `FORMAL_GATES_QA_INPUT_DIR`、`FORMAL_GATES_QA_WORK_DIR`、
`FORMAL_GATES_QA_OUTPUT_DIR` 和 `TMPDIR`/`TMP`/`TEMP`。可变输入、工作、输出与临时
文件使用这些路径；默认 cwd 保留注册候选源目录，源码、依赖和工具不逐例复制。

共享步骤可声明仓库相对 `artifacts`（文件或目录）与 `independent`。普通准备在共用
源环境运行：显式路径的单一 ARGV executable、没有 kernel shebang `#!` 且无额外参数或 stdin 时，观察实际源码
及未提交输入、批准步骤、有效环境和直接工具内容，相同才复用原已完成准备。
相对 executable 路径按实际执行目录绑定；裸程序名不猜 PATH。此能力只覆盖已观察的
直接输入，不证明任意子工具或外部依赖图。SHELL、shebang 脚本、含额外参数或 stdin 等无法证明的
配方沿原批准操作真实执行，因此相同 SHELL 准备在两个用例中会执行两次；仍等待
相同准备的在途原 job，恢复已完成本例时保留原收据，不重复执行。
声明的非跟踪产物与源码输入区分；跟踪源码仍受原 immutable 检查，
不得以产物声明免检。产物缺失或变化不复用；环境仅持有摘要，私有临时路径不导致
无谓重建。`independent: true` 的准备在该例私有环境真实执行，适用于独立安装、构建
或隔离本身为目标的场景。相同准备在并行等待期间沿原 job/锁协调，不盲重派。

设计结果用单一 `test: {read, symbol}` 选择原全文 read 及其发布的 Go 符号序号，程序按声明顺序解析、装配精确引用并将原路径关联到用例文件。已有和新写函数共用该选择；新名称只在写代码时创作，读取后不再抄写。省略选择保留原已签发引用。
白盒新用例选择 Go 测试或声明非 Go 的 approved operations，并提供 oracle；`publicEntry`、`preconditions`、`steps` 只属于黑盒与 merge 形状。测试选择自动关联原文件，`files` 可补充其它实际测试材料。


Go 白盒测试可以只保留精确引用 `<仓库相对路径>_test.go::TestName`。标准库 resolver
读取指定候选文件，解析 Go 源码，核对该文件中唯一的同名包级测试函数及
`func(*testing.T)` 签名，支持普通、别名和 dot import。测试名遵守 Go 的 `Test` 命名
规则。文件目录确定 package 工作目录，固定 invocation 为：

```text
go test -timeout=0 -count=1 -json -run ^<精确测试名>$ .
```

测试名经 `regexp.QuoteMeta` 转义。该协议只接受完整包级测试，不接受裸函数名、`#`
分隔符、子测试路径或其他语言的引用，也不搜索其他文件来猜测目标。Go 精确引用不能
同时声明 case operations；共享准备操作仍由 shared-step 单独持有。已签发的其他不透明 testRef
保留原引用，其执行需要已声明或首次执行期绑定的 operations；新设计使用 operations，
不把其他语言伪装成 Go 引用。

`coverage.ResolveCaseOperations` 只返回 `ResolvedOperation`（原操作、派生目录与 Go
精确测试名），不启动进程或判断 oracle。缺文件、缺测试、同名函数歧义、错误签名、
不支持的引用且没有操作配方，都会返回明确错误。设计合同允许尚未具备配方的部分设计，
这种设计不能由 resolver 推断操作来执行；批准后由 execution checkpoint 填实际 invocation。

执行与接纳方须保留真实 exit、stdout/stderr 原始证据。对 Go invocation，须核到
`GoTestName` 对应的实际 run 和 pass/fail 事件；无匹配或 skip 不能当作执行 PASS。
普通操作的非零 exit 可以是失败用例的预期观察，零 exit 也不能代替 oracle 比较。
resolver 不负责调度、实际执行、receipt 接纳或语义比较。

程序执行沿原 `op.check`、HostActionIntent/Receipt、supervisor 与统一观察/接纳入口。
QA 参数只绑定当前冻结 ExecutionBinding、case、操作序号、来源、原批准操作或 Go
引用，以及 registered workspace；共享操作按 case 引用顺序形成前缀，同一个 case
的多个 source/point 关系只运行一遍。只执行 Required 与 Pending 的交集，Completed
和 Inherited 不重新运行。已完结的原始 receipt 只观察和接纳，不重跑命令到 PASS。

Go 源文件解析由原生 adapter 在实际启动前完成，核心计划只处理批准载荷。原始请求、
退出码和 stdout/stderr 证明留在原 check 证据目录，实际 executable 内容身份及实际
继承环境摘要由 supervisor 记录；目录路径不能充当环境身份。终态保留这些不可变证据，
进程结束与工作区清理由原 HostAction/资源入口处理。

`ValidationInput.executionFacts` 是原 receipts 的只读投影，并随原 worker material
冻结。缺 invocation 时只签发命令填写职责；全部动作完整执行的 case 才能签发语义比较。
Go 的无匹配/skip、进程中断或
未完成动作保留具体原始事实与未完成责任，不能产生 EXECUTED_PASS/FAIL 行。模型只
按原签发比较顺序返回 result 和 comparison 数组，不填写 case ID 或 ID 键。程序填充
原 case 身份、procedure、observation、tool、environment 并展开批准 edges；core 逐值
核原事实、当前候选和批准配方，沿原入口合并 Completed。
共享复用回执的 `preparationSourceAction` 显式引用原执行 action；`preparationInput`
与 `preparationArtifacts` 持有原输入/产物身份。原 request、Directory、参数摘要、双流、
tool/environment 和执行身份不重绑为本例，executionFacts 也引用该原 action。
复用不产生第二次进程执行，不自动赋予语义 PASS。

Case 与 shared-step 的可选 `files` 精确列出该测试材料的仓库相对文件路径；它与正文、
操作及关联一同参与原 manifest 的审查和摘要。只有 case 自身及其实际引用的 shared-step
材料进入签发范围，交付 `paths` 不是测试归属证明。`<相对文件>::<不透明测试符号>`
直接表达单个文件关联；其他不透明引用不猜测文件或测试框架语法。

原生 Go 输入图仅在明确 Go 配置或精确 Go 测试引用时作为可选事实；明确生产输入不能
由模型声明覆盖。非 Go 的既有/新增文件归属由原白盒设计声明关联，再由原独立 QA Review
读取代码、配置及签发的源/候选差异逐项判断。`QA_ARTIFACT` 只冻结隔离工作区的真实
identity 和完整 raw diff 供这次审查，不提前声称 test-only 或生产等价。每条 case/shared-step
的 PASS 与非空实际 reason 同时覆盖其材料归属及生产未污染；不确定或属于生产则 FAIL
回原设计责任，不产生新的查询、角色或审查轮。

原审查在 exact candidate/manifest 上接受、case set FROZEN 后，`REVIEW_READY` 才结合
该语义判断与程序核验的源、文件关联、mode/blob 及完整 diff 装配生产等价和 GateView。
这份证据是绑定的独立判断加机械身份核验，不是完整原生依赖图证明；未审/FAIL 材料不能
提升、排除质量门或获得这些旗标。删除与 rename 每个实际存在端点仍逐值核验。

无指定修订集合的设计续作只提交必要增量：省略的 case/shared-step 及原覆盖保留。
`cases` 条目的 `case`、`sharedSteps` 条目的 `step` 选择原列表的一基序号以修改该项；
不选择时是新增，稳定 ID 由程序分配。`removeCases` / `removeSharedSteps` 明确选择
原序号删除，`replace: true` 明确替换整个集合，不能同时逐项删除。删除 case 同时移除
其覆盖边；悬空共享引用仍拒绝。用例的 `sharedSteps` 选择原共享说明加新增说明的序号。

`coverage` 按原签发验收点顺序返回，每项选择原用例加新用例的序号。`[]` 清除该点
关系；`null` 保留原关系。合并后的完整 Manifest/Version 给独立审查、恢复和覆盖校验
消费，不建立第二份草稿或审批账。已审返修没有实质新增、修改、删除或真实测试材料
改动时拒收；重复摘要或重列原缺口不是修复。未受影响的原批准仍按完整内容与候选
视图继承，初次设计与未完草稿登记不等于覆盖 PASS。

输入含 `affectedCases` 时，按该原顺序仅返回指定用例的语义改动字段。程序保留原身份、
未改字段、未受影响用例及原覆盖；不填写 `caseId`、保留列表或覆盖映射。共同说明有变化时，
按原 manifest 中这些用例引用的说明顺序返回改动字段，未改时省略。该指定项可只返回
`remove: true` 明确删除，不能同时修改其语义字段；悬空引用和覆盖缺口仍须修正。改动省略 `files`
保留原文件关联，明确提交 `[]` 清除它。受影响部分仍由原独立审查验收。

## 冻结验证职责的语义判断

`engine.validation.observation` 将当前波次尚未完成的 ADJUDICATE、EXECUTION_SCOPE 与
GATE_IMPACT 投影为一个签发包，使用原生只读 worker 材料和原 WORKER_RESULT 接纳路径。
模型按输入顺序返回 findings/scopes/gateImpacts 判断数组。发现关联与受影响用例使用
签发列表的一基序号表达语义选择；程序回填原身份、kind、波次、历史位置、新问题 ID、
状态和绑定，不要求模型填写 ID 或重复原对象。
程序先核完整包的当前输入身份，再在同一事务逐项调用原 observation validator；任何
一项非法不会留下包内部分判断。原 typed Operator observation API 同样保留。

EXECUTION_SCOPE 按 `approvedCases` 的一基序号选择受影响用例并说明实际理由。程序将
所选项与原 `mustExecute` 合并，再核验最终非空集合；不要求模型重列必跑项。未纳入
最终集合的用例须有真实、未受影响的原始 PASS。GATE_IMPACT 只根据该门
原始 PASS 和当前原生范围安排 INHERIT/RERUN，不提供门审结论；实际门审输入保持其
原有 QA 隔离。判断包的有限职责、TaskKey 和重试策略由原波次、输入摘要、步骤声明与
Attempt 拥有；拓扑、路线、额外额度、豁免和处置仍通过原 typed Ask 请求用户确认。
