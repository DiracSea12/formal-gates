# QA 固定操作契约

`coverage.AcceptanceCase.operations` 与 `coverage.SharedStep.operations` 是 QA 设计
创作的有序操作载荷。它们随完整 case/shared-step 内容进入既有审查身份、manifest digest
和冻结；不是执行阶段从自然语言临时提取的命令。步骤、前置说明与 oracle 仍表达语义。

每个操作只允许一种形态，可附带原样发送的 `stdin`：

```json
{"kind":"ARGV","argv":["tool","","two words","\"quoted\""],"stdin":"input\n"}
{"kind":"SHELL","command":"printf '%s' \"$value\"\n"}
```

`ARGV` 必须提供非空数组和非空 executable，不能提供 `command`；后续参数可以是空
字符串，空格、引号和特殊字符都是该参数的原值。`SHELL` 必须提供非空 `command`，
不能提供非空 `argv`。操作不含 cwd、env、插件、条件或循环字段。完整 shell 文本可以
表达已批准的环境设置或目录切换；程序不改写文本。执行目录由 registered QA workspace
提供。程序同时从这个 registered workspace 注入 `FORMAL_GATES_WORKSPACE` 环境变量；
需要根目录的 shell 操作用 `"$FORMAL_GATES_WORKSPACE"` 引用，其他文件使用仓库相对路径。
模型不填写物理工作区路径；已批准的 `command`、`argv` 与 `stdin` 保持原文。固定物理
目录的原操作必须经原设计/审查责任修订，程序不会把它改成另一工作区。shared-step 的操作由引用它的 case
使用，载荷本身不表示全局只执行一次。

设计结果用 `files` 原 read、`testFile` 本用例文件序号和该 read 的 `testSymbol` 序号选择 Go 测试，程序按声明顺序从原全文解析并装配精确引用。已有和新写函数共用该选择；新名称只在写代码时创作，读取后不再抄写。省略选择保留原已签发引用。

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
保留原引用，其执行需要已批准 operations；新设计使用 operations，不把其他语言伪装成 Go 引用。

`coverage.ResolveCaseOperations` 只返回 `ResolvedOperation`（原操作、派生目录与 Go
精确测试名），不启动进程或判断 oracle。缺文件、缺测试、同名函数歧义、错误签名、
不支持的引用且没有操作配方，都会返回明确错误。设计合同允许尚未具备配方的部分设计，
这种设计不能由 resolver 推断操作来执行。

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
冻结。只有全部动作完整执行的 case 可以签发语义比较；Go 的无匹配/skip、进程中断或
未完成动作保留具体原始事实与未完成责任，不能产生 EXECUTED_PASS/FAIL 行。模型只
按原签发比较顺序返回 result 和 comparison 数组，不填写 case ID 或 ID 键。程序填充
原 case 身份、procedure、observation、tool、environment 并展开批准 edges；core 逐值
核原事实、当前候选和批准配方，沿原入口合并 Completed。
缺少配方的设计允许暂存，冻结前须指出具体 case 并回到原设计/审查补齐。

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

无指定修订集合的设计续作，`retainCases` / `retainSharedSteps` 选择原列表的一基序号，
不填写原 ID。程序搬运所选条目的原正文、操作和文件关联；`cases` / `sharedSteps`
只承载新创作条目，身份由程序分配。两类列表均不含的条目删除，其 case 的覆盖关系
不再保留。用例的 `sharedSteps` 选择原共享说明列表加新创作说明列表的序号。

`coverage` 按原签发验收点顺序返回数组，每项选择用例序号（保留用例在前、新用例在后）。
`[]` 清除该点关系；`null` 由程序保留仍在当前集合中的原关系，不重写点键或原 case ID。

输入含 `affectedCases` 时，按该原顺序仅返回指定用例的语义改动字段。程序保留原身份、
未改字段、未受影响用例及原覆盖；不填写 `caseId`、保留列表或覆盖映射。共同说明有变化时，
按原 manifest 中这些用例引用的说明顺序返回改动字段，未改时省略。改动省略 `files`
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
