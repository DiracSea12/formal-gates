# 需求案：审查收敛治理与阶段 3.5b 成本止损核心

## 背景与目标

另一台设备上的实际使用暴露出 formal-gates 在审查收敛和成本可见性上的一组问题：实现
细节被错误定为 P0/P1、同一快照的结果未收齐就开始返修、同一根因被不同审查者重复提出、
QA 用例在普通代码返修后被重复设计，以及同一输入被重复派发。其结果不是发现更多有效
问题，而是把一次开发拆成大量审查、返修和复审循环。

本需求把两项有依赖关系的工作合并受理：

1. Batch A 收紧审查登记、统一同快照返修、持久化问题归并、冻结 QA 用例并拒绝重复派发；
2. Batch B 完成阶段 3.5b checkpoint：补记 owner 成本，并提供 engine 侧纯 guard 计算核心
   及可由黑盒 QA 调用的公开只读预览入口。

Batch A 必须先完成，因为 Batch B 的 guard basis 会引用审查、QA、返修和重试形成的 canonical
外部任务槽位。若先按旧流程计算额度，随后再改变返修触发和重复派发规则，guard fixture 与
实际工作流会立刻失配。

本次目标不是用更多审查约束审查者，而是让程序只在已经证明会导致正常使用失败的情况下
阻塞，让同一快照只形成一个完整返修批次，并为后续阶段 4 的运行时派发止损提供稳定计算
核心。

## 对齐轨迹

- 用户要求先根据本项目实际实现核实另一设备反馈，不预设全部接受。
- 用户确认实施第 1、2、3、6、9、10、12、13 项；第 4 项改为由主代理判断语义重复，CLI
  持久化判断；第 5 项区分门审与 QA Review；第 7 项不向门审注入 QA 用例；第 8 项由既有
  3.5b/阶段 4 重构方案承接；第 11 项实时成本面板暂不实施。
- 用户确认门审 P2 必须返修但不复审，门审 P3 不返修并在 Seal 时展示。
- 用户确认 QA Review PASS 后，必须先吸收该轮 P2/P3 建议，再冻结用例集；P2/P3 吸收不
  触发新的 QA Review。
- 用户确认把本需求与 3.5b 合并为一个正式总需求，按有依赖关系的 Batch A、Batch B 组织。
- 用户要求 Batch B 提供正式产品入口，否则黑盒 QA 无法验收；双方确认新增
  `formal-gates workflow guard-preview --input <basis.json>`。
- 用户确认在阶段 4 尚未接入前，返修授权摘要中的 engine 累计派发额度显示
  `UNAVAILABLE`，不得把纯计算器偷偷接入 legacy 状态。
- 用户随后明确授权主代理再次逐项核实 Batch A，并要求先修订发现的偏差，再由一名全新的
  独立只读子代理完整复核 Batch B 与后续 engine 计划；主代理不得替代失败的 Batch B 复核。
- 用户进一步指出：首次受理可以确认全文，但产品审或技术审导致需求文件修改后，重新确认
  只应确认相对上一已确认 revision 的更改部分，不应要求再次确认全文。
- 本轮产品审 12 条 raw P1 经展示严重度后，用户确认其所述问题均需修订；用户同时要求核对
  finding 处置前是否已有强制展示严重等级的规则。核对确认现有文档只要求分级和逐项处置、
  未明确要求在决策请求中展示等级，用户要求补入本需求。
- 用户随后追问这 12 条是否逐条写明 P1 理由。复核确认：原结果只写了需求—AC 缺口，未逐条
  说明为何不能定为 P2；主代理用“item FAIL 所以是 P1”的整批理由属于循环论证。用户明确
  重申 P0/P1 不得乱定，定级理由必须站得住脚。
- start-readiness 随后提出并经用户确认两条 P1：`finding-6162393df5735ce0` 指出非 wave 审查
  的无效结果没有可执行的重试与耗尽终结路径；`finding-d948d18628d883e9` 指出白盒测试推进
  快照后，review-wave 缺少绑定最终含测试快照的初始化屏障。
- 新一轮 start-readiness 随后提出并经用户确认两条 P1：`finding-ae96ee58ecab6f86` 指出
  review-delta 缺少已确认 base 全文的明确所有者，用户决定由主代理在修改前保留并核对；
  `finding-b8c963ba545cecd4` 指出不同类型 FAIL 被错误统一导向开发后返修，用户要求按类型
  分流并继续遵守现有上限与授权。
- 用户随后发现主代理把只修正技术方案的 revision 错登记为 `meaning-changed`，导致已通过的
  产品审被回退。用户决定：`meaning-preserved` 不需要额外回退授权；`meaning-changed` 会
  作废既有 PASS、回退流程，主代理只能提出建议，必须取得用户明确确认后才能实施，并要求
  未来阶段 4 由 engine 强制这条边界。

## 范围

Batch A：

- 为开发后门审 P0/P1 增加完整阻塞证据合同和 CLI 强制校验。
- 为 start-readiness 增加问题分类和严重度上限，只允许三类问题阻塞开工。
- 为同一 review-ready 快照建立冻结目标集的审查轮次收集状态；全部已选 QA 执行和门一次
  准备、并行派发，所有结果收齐后只创建一份统一返修任务。
- 为开发后发现项建立稳定问题 ID，并保存主代理作出的新问题、重复归并和驳回关系。
- 重新定义门审 P2/P3、QA Review P1/P2/P3、QA 用例冻结和返修后的 QA 执行范围。
- 不向复杂度审或实现质量审注入 QA 用例；对容易被误删的非显然公开验收接口使用就地注释
  标记其需求职责。
- 生成有版本绑定的返修授权前摘要，并拒绝相同权威输入的重复派发。
- 产品审或 start-readiness 技术审导致需求修订时，由主代理在修改前保留上一已确认全文、
  生成并核对绑定前后 revision 和 finding 的完整变更包；CLI 校验候选、绑定回执，用户只确认
  变更部分，未变内容从上一已确认 revision 继承。
- 需求影响登记为 `meaning-preserved` 时不新增回退授权；登记为 `meaning-changed` 时，CLI
  必须在任何流程状态回退前取得绑定本次影响与 revision 的用户明确确认。未来阶段 4 由
  engine 的 typed Ask 和单写者状态转换强制同一规则。
- 产品审或 start-readiness finding 请求用户确认或驳回前，逐项展示严重等级；漏显时不得把
  用户回复登记为有效处置。
- 所有正式审查的 P0/P1 逐条保存可核验的定级理由；条目 FAIL 不自动等于 P1，用户处置时
  同时看到等级及其理由。
- 同步更新 formal-gates 文档、提示词和自动回归测试。

Batch B：

- 在现有单一 cost 投影中补记 owner 启动基线和 Seal/Abort 终态差值。
- 实现无 I/O、无状态写入的 engine guard 纯计算核心。
- 新增公开只读 `workflow guard-preview` 命令，让黑盒 QA 通过正式构建产物验证生产计算。
- 保持阶段 3.5b 与阶段 4 的边界：本次只交付计算核心和入口，不把 guard 接入运行时派发。
- 同步未来 engine 权威需求、最终方案、增量阶段计划和迁移矩阵，使阶段 4 继承本需求的主
  代理裁决、统一返修、QA 冻结、P2/P3 和 guard basis 语义。

## 非目标

- 不维护旧版本状态、finding 格式/摘要或旧内部调用合同的兼容层，结果登记统一遵循
  当前证据合同。当前版本仍在使用的
  流程运行时、dispatch 计量和来源不可用处理不是旧版兼容层，不因此删除或改变。
- 不增加 legacy 的用户级或项目级累计限额配置，不实现 `max_dispatches`、
  `max_elapsed_minutes` 或 token cap 的 legacy 拦截。
- 不实现 `workflow show` 或每次准备派发时的完整实时成本面板。
- 不把 guard 接入 engine `State`/envelope、source bridge、`IssueFromPlan`、`refill`、recovery、
  HostAction retry、typed Ask 或阶段 4 的 `show/status/next` 投影。
- 不向复杂度审或实现质量审的正式输入注入已批准 QA 用例、逐用例执行证据或测试实现细节。
- 不让 CLI、哈希或另一个模型自动判断两段自然语言是否语义相同；语义归并由主代理负责。
- 不为 `meaning-preserved` 增加单独的流程回退授权；需求正文仍按 REQ-016 的变更部分确认。
- 不修复门审 P3；门审 P3 只作为 Seal 前一次性展示的建议。
- 不因普通代码返修重新设计已经冻结的 QA 用例。
- 不把首次受理的全文确认改成增量确认，也不允许与产品审/技术审已处置 finding 无关的改动
  混入审查后增量确认。
- 不引入第二套 cost ledger、价格库、美元估算、动态预算或 provider fallback。
- 不读取、配置、持久化或执行项目级/安装级 token cap，不新增 RunPhase、Seal/Abort 变体、
  公开写命令或第二状态机；`report.cost` 仍只负责报告。
- 不在本 Batch 实现阶段 5 的 child/master owner 汇总；每个 run 只记录自身 owner 用量。

## 方案与技术约束

### Batch A：审查收敛治理

开发后门审发现项新增结构化阻塞证据。未通过证据门的原始报告不得进入有效 finding、
review-wave、问题账本或 guard basis；`record-gate` 返回非零，同时只允许原子写入绑定原
dispatch 的不可变 `REJECTED_INVALID_RESULT` 回执并结束该 attempt，不得写入 gate FAIL 或
其他有效流程状态。该回执必须保存被拒字段和原因，使同一逻辑 TaskKey 能按有限 retry policy
重派；不得把当前并不存在的 dispatch 原始结果字段描述成“既有审计记录”。P0/P1 必须逐项
绑定已确认需求、
正常使用场景、当前实际错误、至少一条可定位的代码/文档/测试证据、延后处理为何会导致开发
方向错误或交付失败，以及主代理对范围、严重度、因果和复现的核实记录。CLI 校验结构完整、
引用在绑定需求/快照中存在且可定位，以及状态绑定；主代理负责验证语义真实性。缺少任一字段
或引用悬空、无法定位时，该发现项不能形成有效 FAIL。

所有正式审查中的 P0/P1 还必须逐条提供 severity rationale，说明对应已确认需求、正常使用或
验收路径、实际后果，以及为何 P2 不足；P0 进一步说明为何需求无法成立、核心验收没有任何
可用路径或交付被阻塞。obligation-to-AC 条目 FAIL 只表示覆盖缺口，不能自动推出 P1/P0。
主代理必须逐条核实这些理由，批量套话或一份整批 operator-evidence 不能替代。

start-readiness finding 使用稳定分类枚举。只有“需求存在实质冲突”“模块职责或依赖方向
存在根本错误”“核心验收缺少可执行入口”可以形成 P0/P1。字段、容器、辅助函数形式、两种
合理实现之间的选择、命名排版、文档精度、fixture 细节及其他开发阶段可正常决定的内部细节
统一归为实现细节，最高 P2。P0/P1/P2/P3 的每条 finding 都必须显式声明分类；CLI 强制分类
存在性及分类与严重度矩阵，主代理核实分类是否符合事实。
非法分类或核实记录缺失采用与开发后门审相同的 `REJECTED_INVALID_RESULT` attempt 语义：
不形成有效 start-readiness 结果，但不会留下无法结束、又无法重派的 CLAIMED dispatch。

`RUNTIME_ERROR` 与 `REJECTED_INVALID_RESULT` 都绑定其逻辑派发义务，而不是只有
review-wave 成员才拥有的状态。义务仍有效且有限 attempt 尚有余量时，任何 action、QA 或 gate
都复用原 TaskKey 重派；若它属于 review-wave，还必须同时满足该 wave 仍为 `COLLECTING` 且
目标未被处置。初始有限 attempt 耗尽后义务进入 `RETRY_EXHAUSTED`，`workflow show` 必须展示
耗尽原因、已用/总 attempt 和允许的下一步，CLI 停止自动及普通重派。用户只能以非空理由和
state revision 显式为同一 TaskKey 追加一次、且仅一次单 attempt 授权；该授权更新 retry policy
及其 basis digest，但不创建新逻辑义务。追加 attempt 再失败后不得继续扩张：必需的产品审、
start-readiness 或 QA Review 保持未通过，wave 成员只能按已有显式错误处置结束收集且不得记为
PASS，用户须 abort/cleanup，或确认一份真正改变 binding/snapshot/责任的新计划后使用新 TaskKey。

每个 review-ready 快照持有一份 review-wave 投影，包含权威快照签名、所选 QA mode 和门、
预期结果集合、当前结果、问题集合和返修状态。无白盒 QA 时，开发快照和所有已选 QA mode 的
冻结 case-set 就绪后，该快照成为 review-ready。存在白盒 QA 时，开发提交后的 S1 只作为白盒
设计起点，不初始化 wave、不派 QA execution 或门；白盒设计者写入结构测试后，host 先提交并
推进到含测试的 S2，再在该快照执行 QA Review。QA Review FAIL 后的用例修改或 PASS 后的建议
吸收若改变测试代码，均须再次由 host 提交并推进快照。只有最终快照包含全部白盒测试、所有
已选 mode 的 case-set 均为 FROZEN，且 case-set version 绑定该快照时，它才成为 review-ready；
无额外修改的普通路径中该快照就是 S2。wave 只在这个最终快照初始化，全部 QA execution 与门
针对同一快照并行派发，门比较范围必须覆盖基线到该最终快照。wave 初始化时冻结完整
expected-set 及其 digest；同一
wave 不得中途增加、删除或替换目标来改变“已收齐”结论，route、QA case-set 或目标集合变化
必须使该 wave 失效并按合法新输入重建。host/scheduler 必须一次准备全部 eligible QA mode 和
门并并行派发；CLI 不得人为建立串行依赖，并必须公开尚未准备、在途和已终结目标。所有预期
结果得到 PASS/FAIL，或未解决 RUNTIME_ERROR
得到显式用户处置前保持 COLLECTING；PENDING/在途结果不能被处置为已收齐。未处置的
RUNTIME_ERROR 可在同一 wave 重派，但不能提前创建返修。用户明确 skip/处置后，该目标对本
wave 终结并视为已收齐，但不得伪装成 PASS，处置事实和理由必须保留；晚到结果只留审计记录，
不得自动重开已关闭 wave。结果收齐后，
主代理完成范围、严重度、复现和归并核实，CLI 才允许为该快照创建唯一逻辑返修任务。返修
派发发生运行错误时可以重试同一逻辑返修义务，但不能创建第二份返修计划。

每个原始或有效发现有独立稳定 `finding ID`；问题账本只为 canonical problem 分配稳定
`problem ID`，并保存对应需求、正常失败场景、受影响模块、根因、实际后果和原始证据。
主代理按需求、正常失败场景、受影响模块、根因和实际后果五个维度记录 NEW、SAME_AS 或
DISMISSED 裁决，并保存非空理由、引用和 revision：NEW 创建 problem，SAME_AS
把 finding 绑定到已存在 problem，DISMISSED 只保留 finding 审计且不创建 problem/返修义务。
OPEN/RESOLVED 等处置状态只属于 problem。CLI 继承原问题的处置状态：
与已解决 problem 归并的 finding 不再次触发返修，与已 DISMISSED finding 相同的新报告只留
审计记录；根因或实际后果不同则记录新 problem。
较晚轮次首次出现的有效 P0/P1 标记为前轮漏审。稳定 ID 用于引用和恢复，不把自由文本哈希
当成语义判断。

门审 P2 必须进入该快照的统一返修任务；即使没有 QA FAIL 或 P0/P1，纯 P2 也创建一次返修，
但不重新派门审、不增加审查轮次。门审 P3 不进入返修任务，只在 Seal 前集中展示。门审 P2
修复后必须完成最高强度开发者自测（完整构建、全量项目测试、受影响用例逐项自跑），并为
提出 P2 的门生成绑定问题 ID、修复快照和原 gate PASS 的结构化完成/carry 凭据，使该 PASS
可合法带到修复快照且不复审。其他已通过门继续使用既有影响判定：不受影响则 carry，存在与
修复改动相连的独立影响才按原规则处理，不得仅因“本轮含 P2”自动重审。若修改触及 QA 已
验证行为，只执行 AFFECTED QA；未受影响 QA 结果合法继承。

QA Review 保持独立规则。逐用例 FAIL 或集合级 P1 使 QA Review FAIL，必须修改用例并重新
审查。集合级 P2/P3 都是结构化、非阻塞建议；QA Review 总体可 PASS，但该轮全部 P2/P3 必须
由 QA 设计者独立决定具体测试方法，并通过 QA Design suggestion-absorption 路径逐条落实。
suggestion-absorption 只有在其绑定的 QA Review 已 PASS 后才可进入；
每项吸收必须引用该轮稳定 suggestion ID；合法吸收可以新增、修改或删除用例，遗漏、重复或
无关吸收被拒绝，但吸收修改不触发新的 QA Review。冻结前 CLI 必须重新执行确定性的
source/point/case 映射、unbound AC 和 approved-whitelist 完整性校验；删除或修改用例导致任一
已确认验收点失去合法批准覆盖时拒绝冻结，不得用先前集合级 PASS 掩盖。QA Review PASS 且
本轮没有 P2/P3 时立即冻结；存在建议时，只有全部 P2/P3 标记为已吸收且上述机械覆盖校验通过
后，当前 QA case-set version 才进入 FROZEN。

冻结后的 QA 用例只有在需求含义、对外行为、成功/失败判据或原用例可执行性发生变化时才
允许 THAW，并必须记录触发类别、原因和来源 revision。THAW 后受影响用例回到待设计/待审，
相关旧执行和 carry 失效；未受影响用例的批准状态继续保留，重新满足 REQ-006 后才能生成新
FROZEN version。临时环境不可用只记 RUNTIME_ERROR，不构成“原用例失去可执行性”。普通实现
返修不改变 case-set version，不重新运行 QA Design/QA Review。普通中间返修固定使用
AFFECTED：执行先前失败用例及与修改路径相关的既有通过用例，其余合法 PASS 继承；不能继续
把 FULL/AFFECTED 作为每轮都询问用户的自由选择。启用 requirement guarantee 时同样允许中间
AFFECTED，本需求在这一点上明确取代 `stage-3-5a-fix-requirement.md` 中“保证模式每次执行均
FULL”的旧规则，但 Seal 前仍必须对最终候选完成 FULL 回归。非保证路线只有在其现行项目或
route 规则要求最终 FULL 时才执行最终全量；本需求不为其他路线新增无条件 FULL。

开发后门审继续读取已确认需求、基线到当前快照的完整 diff 和自身门规则，不读取 QA 用例。
若某个公开接口的唯一调用者位于产品外部，或它专门承担文档化验收观察职责而可能被静态
分析误判为无用，只在该接口附近增加简短的 acceptance-surface 注释，引用对应 REQ/AC；
不为普通接口添加模板化注释。审查者在把接口判定为无用、建议删除或据此形成 finding 前
必须核对其需求职责，但注释本身不能替代需求、实现或测试。

额外返修授权采用两步协议：预览先生成绑定当前状态 revision 的完整授权摘要和摘要 digest，
不得创建或修改任何 run state、回执、revision 或授权状态；真正授权必须引用同一 digest，
状态变化后旧摘要失效。摘要固定包含当前审查
轮次、剩余自动审查轮次、已派发次数、运行时长、本轮全部 QA FAIL、有效 P0/P1、门审与
QA Review 的 P2/P3 数量及处置状态，以及预计重跑的构建/项目测试目标和 QA mode/case。
阶段 4 未接入前，engine 累计派发额度固定明确显示 `UNAVAILABLE`，不能由主代理估算。

派发前计算权威 dispatch signature，至少包含派发类型、审查目标、mode、基线、当前快照、
需求 revision、QA case-set version、catalog/prompt revision 和动作专属输入身份。相同
signature 已有 OPEN/PENDING/CLAIMED 时只返回既有 dispatch，不创建并行重复项；已有 PASS
时拒绝重复派发；已有 FAIL 时按派发类型返回既有待办，不因“相同 FAIL”创建新任务：产品审
和 start-readiness 返回 finding 处置、需求修订与重审状态，QA Review 返回 QA Design 修改及
重审状态，只有开发后 QA execution 和 gate 在 wave 收齐、主代理裁决后才进入该快照统一返修。
已 DISMISSED 或已归并到 RESOLVED problem 的原始 FAIL 不触发返修。输入实际变化形成新
signature 后，新的重审或返修仍须通过该动作现有自动轮次、attempt、review-wave/repair-wave
额度和授权检查；去重分流本身不补充额度、不代替 `authorize-repair` 或额外审查授权。用户明确
要求重审时必须记录非空理由；自动额度耗尽时还必须先取得适用的额外授权，并保留原结果、
额度消耗和 override 审计记录。RUNTIME_ERROR 或 REJECTED_INVALID_RESULT 按逻辑义务的
attempt 余量重派；只有 wave 成员额外受 `COLLECTING` 约束，耗尽后按上述
`RETRY_EXHAUSTED` 和一次性追加 attempt 规则处理。签名只用于完全相同输入的机械判断，不
替代问题语义归并。

dispatch signature 是具体派发输入的去重键，external TaskKey 是预授权逻辑义务身份：同一
义务的 RUNTIME_ERROR 重派复用 TaskKey 并消耗有限 retry；只有新的有效义务，或经确认的新
计划导致 binding、snapshot、责任发生变化时才产生新 TaskKey。raw finding、result、receipt
或普通 resume 均不产生 TaskKey。

首次受理继续展示并确认唯一正式需求文件全文。产品审或 start-readiness 技术审的 finding 经
用户处置并实际导致该文件修改时，主代理必须在首次编辑前读取并保留修改前最后一个已确认
revision 的规范化完整文本，核对其 digest 与 run state 中的 base revision 一致，再以该文本和
完整候选文件生成穷尽 review-delta。变更包列出全部新增、删除和修改 hunk、受影响的 REQ/AC
或非需求章节、来源 action/finding ID、处置决定及影响摘要。主代理负责逐 hunk 核对完整性和
来源归属；CLI 不保存、恢复或自行持有 base 全文/diff，只对完整候选执行结构预检，并校验主
代理记录的 base revision、candidate revision、变更包 digest、来源 finding 集合和逐 hunk
核实记录后写入确认回执。若主代理未在编辑前保留与 base revision 匹配的完整文本，或核对发现
不匹配，不得声称已生成 review-delta，也不得请求增量确认；必须先从可信来源恢复精确 base，
否则停止并报告无法生成。用户只确认变更包；未列出的内容从 base revision 自动继承，不重复
展示全文或要求重新确认。候选文件或处置状态变化使旧变更包失效。确认后 CLI 冻结完整
candidate revision，增量确认不降低完整文件、guarantee envelope 或后续审查的绑定强度。与
已处置产品审/技术审 finding 无关的额外 hunk 必须先移除或作为新的用户需求单独对齐。用户
可以主动要求查看全文，但这不是审查后重新确认的前置条件。

需求正文的 review-delta 确认与流程回退授权是两个边界。主代理可以直接建议并登记
`meaning-preserved`，不需要额外取得“保留流程”授权；该登记必须保留产品审 PASS，只让实际
受影响、原本未通过或尚未完成的 start-readiness 状态重做，需求正文仍按上述 review-delta
规则确认变更部分。`meaning-changed` 会作废产品审 PASS 并把流程退回产品审，主代理只能提出
建议：CLI 必须先生成只读影响预览，至少展示 base/candidate revision、被认定改变的产品目标、
范围、对外行为或验收标准、将作废的 PASS、回退位置和适用额度状态；只有绑定该预览 digest、
当前 state revision 与候选 revision 的用户明确确认，才允许原子实施回退。缺少确认、确认绑定
过期或只有主代理建议时，CLI 必须拒绝且状态不变。若同一条用户确认同时清楚覆盖 review-delta
和上述回退后果，可以复用一次确认，不要求用户重复答复。CLI 只强制授权与绑定，不自动判断
自然语言是否真的改变产品含义。

产品审或 start-readiness finding 请求用户逐项确认或驳回前，主代理必须在同一决策消息中
为每条尚未处置 finding 明确展示稳定 finding ID、严重等级、定级理由和问题摘要；同等级
finding 可以分组，但编号到等级及理由的映射必须无歧义。若漏掉任一严重等级或定级理由，
不得把随后用户回复登记为正式处置，必须补齐完整展示后重新取得确认。该规则的 canonical
owner 位于 `SKILL.md`，
`references/formal-flow.md` 保留操作顺序；CLI 的未处置 finding 投影必须始终提供 ID、severity
与定级理由，使主代理不需要从自由文本推断等级。

### Batch B：3.5b owner 计量与纯 guard

除本需求明确授权的公开只读 `guard-preview` 外，原阶段 3.5b 的当前 checkpoint、明确不做项和
阶段 4 保留验收继续有效。owner 计量继续写入现有 `RunCost` 投影，不伪装成 dispatch。
workflow start 成功时，在可取得稳定 transcript/provider 身份的情况下，把 owner 解析基线、
provider 和完成标记持久化在同一 legacy RunState；Seal 或 Abort 时读取同一来源，记录终态
快照减启动基线的差值。`RunCost.Owner` 是可选独立字段，顶层 token 总计包含 dispatch 与本
run owner 且各自只累计一次。缺来源、provider 不支持、文件截断或重写、区间无法确定、多个
活动 run 共用重叠区间时记 `UNAVAILABLE` 且数字为零。重复终态操作必须幂等，不要求读取旧版本
状态或保留旧格式/摘要兼容，计量不得改写 action、gate、QA 或 dispatch 结果。本 Batch 只记录各 run 自身
owner；child/master owner 汇总留到阶段 5。`internal/cost` 继续保持独立解析/计量包，不反向
依赖 validate 或 engine，并保留 parser 独立测试。

guard 核心位于 engine 责任范围内，但保持纯函数：输入是带 schema version 的 canonical
basis，包含 requirement、confirmed Batch/route/topology、closed-world definition、approved
QA、selected gate、established obligation/repair、external TaskKey provenance 及有限 retry
policy；输出是 distinct external TaskKey 数、request allowance、retry allowance、total
allowance 和稳定 `budgetBasisDigest`。外部集合统一覆盖 agent/host 派发，不为每种 step 新增
配置字段。默认外部动作 `MaxAttempts=2`，显式值必须大于等于 1：

- `requestAllowance = distinct TaskKey 数`；
- `retryAllowance =` 对每个 distinct TaskKey 求 `max(0, MaxAttempts-1)` 后求和；
- `totalAllowance = requestAllowance + retryAllowance`。

完全相同的重复 TaskKey 只贡献一次基础槽位和一次 retry 余量；同一 TaskKey 出现互相冲突的
retry 或 provenance 声明时拒绝输入。需要用户确认的任务扩张在新 revision 确认前不能增加
basis；已预授权 repair slot 可由 Controller 机械加入；普通 agent result、receipt 或 resume
不能增加 external TaskKey。

生产计算不得执行任何 I/O，不得读取 transcript、run state、文件或网络，不得写状态、调用
LLM 或签发任务。阶段 4
Controller 才负责从权威 state 构造 canonical basis；纯核心只验证 typed schema、规范化和
字段交叉一致性，不自行认证某个 revision 是否真实确认。缺少 closed-world definition、确认
revision 声明、允许 provenance 或完整身份的 external TaskKey 被拒绝。相同 canonical basis
必须得到相同额度和 digest；Batch、TaskKey provenance 及任何其他权威 basis 身份变化都必须
改变 digest。阶段 4 的 obligation 只能来自已完成的主代理 adjudication 和收齐 wave 后形成
的唯一返修义务，raw reviewer finding 不得扩容。

新增公开只读命令：

```text
formal-gates workflow guard-preview --input <basis.json>
```

CLI 负责读取并校验 JSON、调用生产 guard 核心并输出 JSON；不复制计算公式。命令不创建 run、
不写状态、不签发任务，也不认证输入对应真实 run、已确认 revision 或合法任务集合；它是非
权威计算预览。输出至少包含规范化 basis 摘要、各类 allowance、digest 和明确的
runtimeIntegration=`NOT_INSTALLED`，避免用户误以为阶段 4 止损已经生效。无效 schema、缺失
身份、非法 retry、冲突 TaskKey 或不可读输入必须返回非零状态和可定位错误。

### 文档与测试

`SKILL.md` 只保留稳定的严重度、P0/P1 定级理由、finding 处置前等级与理由展示、返修触发、QA 冻结、审查后增量确认
和 Seal 规则；
`references/formal-flow.md` 持有 review-wave、统一返修、问题归并、授权摘要和命令细节；
requirements-clarification、product-review、start-readiness、qa-review、qa-design、qa-execution、carry、development-worker、
reviewer-base 及门提示词与 CLI 结果合同保持一致，README/README_EN 不得继续保留冲突的旧
P2/P3、QA 重跑、复审、全文重新确认或 requirement impact 授权说明。项目
`AGENTS.md` 只保留项目专属事实纪律、子代理执行边界和对 formal-gates 的引用，不复制可由
CLI 强制或已由 formal-gates 文档持有的流程细则；重复、冲突和已由 CLI 强制的过程描述必须
删除，不能通过继续叠加新规则来“保持一致”。

未来 engine 计划同步属于本需求的文档完成项，不提前实现阶段 4：权威总需求与最终方案补入
开发后 Operator adjudication、问题账本、review-wave、唯一返修、QA 冻结、dispatch signature、
无效结果义务生命周期、review-ready 快照屏障、授权摘要及 gate P2/P3 新语义；3.5b 子计划、
增量阶段计划和 route matrix 同步 `guard-preview`、`meaning-changed` 用户 Ask 及阶段 4 迁移
合同。阶段 4 engine 作为单一 writer：`meaning-preserved` 不产生用户 Ask 并保留产品审 PASS；
`meaning-changed` 未收到绑定当前 revision 和回退影响的用户确认事件时，不得接纳需求 revision
转换或签发新的产品审任务。ADR-001/002 不变；历史
stage record 不回写，只在实施完成后追加 checkpoint。

自动测试必须直接验证生产状态转换和生产 guard 计算；已知答案测试不得在测试中复制公式。
CLI 黑盒测试必须通过正式构建入口验证 `guard-preview`，不能用 `go test` 代替产品入口。

## 失败路径与恢复

- 正式 P0/P1 缺少合格逐条定级理由、门审 P0/P1 缺证据或 start-readiness 分类非法时，记录
  命令返回非零；有效 gate/action、
  finding、wave、problem 和 guard basis 不发生部分写入，只原子追加
  `REJECTED_INVALID_RESULT` 回执并结束失败 attempt，使同一 TaskKey 可以按有限 attempt 重派；
  该恢复不依赖义务属于 review-wave。
- attempt 耗尽后义务进入 `RETRY_EXHAUSTED`；用户至多追加一次单 attempt，仍失败则必需 action/
  QA Review 保持未通过，wave 成员只能显式终结收集而不能产生 PASS，后续只能终止 run 或通过
  已确认的新计划形成新 TaskKey。
- review-wave 尚有未解决结果时，创建返修任务失败并列出缺失目标；未处置 RUNTIME_ERROR 或
  REJECTED_INVALID_RESULT 在余量内可以用相同 signature 重派，用户明确处置后仅解除“未收齐”
  状态、不得产生虚假 PASS，晚到结果不得自动重开 wave。
- 存在白盒 QA 时，S1 上准备 QA execution 或门被拒绝；只有含最终白盒测试、绑定全部 FROZEN
  case-set 的 review-ready 快照才能初始化 wave。白盒测试或 case-set 再变化时旧 wave 失效。
- 主代理错误归并可以通过带 revision/CAS 的新裁决纠正，原始 finding、problem 和旧裁决保留；
  返修派发前更新同一逻辑计划，派发后新成立的义务进入下一快照 wave；不能手改 state.json。
- QA case-set 冻结后无合法 THAW 原因的设计修改被拒绝；合法 THAW 后受影响用例及旧执行失效，
  重新设计、审查并满足冻结条件后才能执行，临时环境错误不触发 THAW。
- 授权摘要生成后状态变化，实际授权因 digest/revision 过期而失败，需重新生成完整摘要。
- review-delta 生成后候选文件、finding 处置或 base revision 变化时，确认因绑定过期被拒绝；
  主代理用编辑前保留且 digest 匹配的 base 全文重新核对并生成穷尽变更包，CLI 重新预检完整
  候选和校验绑定，不回退成强制全文确认。
- `meaning-changed` 缺少绑定影响预览的用户确认，或确认时的 state/base/candidate revision 已
  变化时，登记返回非零且不得作废任何 PASS、回退流程或消耗审查额度；重新生成预览后再由
  用户决定。`meaning-preserved` 不走这项回退授权，并保留产品审 PASS。
- 产品审或技术审 finding 的用户处置请求漏显任一严重等级时，不调用 `settle-findings`；补齐
  finding ID、等级、逐条定级理由和摘要后重新请求用户决定，先前信息不完整的回复不作为
  正式确认来源。
- 重复派发被拒绝不改变已有权威结果；相同 FAIL 按 action/target 返回既有待办，不跨类型创建
  统一返修。输入变化或用户 override 需要新派发时，继续消耗适用的现有额度，额度耗尽则先走
  对应授权；override 只增加有理由的重审记录，不能补充额度或替代授权。
- owner 成本不可核对时记 `UNAVAILABLE`，不以零值冒充实际无消耗，也不阻断已有结果记录。
- guard-preview 输入错误时零计算、零状态写入并返回非零；内部计算不一致视为实现错误，不能
  回退到主代理估算。

## 考虑过但不采用

- 不采用“需求、场景、模块、后果自由文本直接哈希即自动认定重复”：措辞变化会漏合并，
  过度规范化会错合并；主代理语义判断加持久化 alias 更符合现有责任边界。
- 不在本次再建一套 legacy 累计限额：3.5b/阶段 4 已定义 engine guard，重复实现会形成双权威。
- 不向门审注入整套 QA 用例和执行证据：这会扩大上下文并让实现质量审重新解释测试设计；
  非显然验收接口用局部需求注释保护即可。
- 不在 QA Review 刚 PASS 时立刻冻结：同轮 P2/P3 尚未吸收会使冻结状态与已知建议冲突。
- 不在产品审/技术审每次修订后重新要求确认全文：上一已确认 revision 已是有效确认基础，
  重新确认只需覆盖穷尽且版本绑定的变更部分。
- 不修门审 P3：P3 不应启动开发任务，Seal 汇总足以保留信息。
- 不提前实现阶段 4 运行时拦截或成本面板：本批只交付可独立验证的 3.5b 核心和公开预览。

## 需求点

### REQ-001：P0/P1 只接受完整且经主代理核实的阻塞证据

#### 要求

开发后门审的每条 P0/P1 必须绑定已确认需求、正常使用场景、当前实际错误、至少一条可定位
的代码/文档/测试证据、延后处理为何会导致开发方向错误或交付失败，以及主代理对范围、
严重度、因果和复现的核实记录。CLI 必须在形成有效 FAIL 前机械验证字段完整、引用有效且
核实记录存在；缺少任一字段时不得把该 finding 记为阻塞。

#### 验收条件

- AC-001：P0/P1 finding 缺少任一必需阻塞证据字段，或代码/文档/测试引用不能在绑定需求产物与快照中解析到实际存在且可定位的对象时，`record-gate` 返回非零，有效 finding 和 gate FAIL 均不写入；命令只原子追加绑定 dispatch/attempt/TaskKey 的 `REJECTED_INVALID_RESULT` 回执、保存被拒字段、无效引用与原因并结束该 attempt，不进入 review-wave、问题账本或 guard basis。同一逻辑义务随后可复用 TaskKey、消耗有限 retry 重派，不会被旧 CLAIMED dispatch 永久阻塞。
- AC-002：完整 P0/P1 记录保留所有结构化字段、来源位置、绑定需求 revision、原始 finding 和主代理核实记录，并能由 `workflow show` 读取。
- AC-003：P2/P3 不被强制填写 P0/P1 阻塞证据，也不能仅因字段为空被提升为阻塞项。
- AC-004：主代理核实记录不允许由审查者原始结论或普通 finding 文本隐式代替。

#### 来源

用户确认实施原反馈第 1 项，并要求避免实现细节错误启动返修和复审。

### REQ-002：start-readiness 只有三类问题可以阻塞开工

#### 要求

start-readiness finding 必须声明问题分类。只有需求实质冲突、模块职责或依赖方向根本错误、
核心验收缺少执行入口可以形成 P0/P1；开发阶段可决定的实现细节最高 P2。

#### 验收条件

- AC-005：任一 start-readiness finding 未声明问题分类时，记录返回非零且不写有效 action/finding；P0/P1 声明的分类不是三种允许阻塞分类之一时同样拒绝；两者都只按 AC-001 写入 rejected-result 回执并结束 attempt，使同一 TaskKey 可有限重派。
- AC-006：字段、容器、辅助函数、合理实现选择、命名排版、文档精度或 fixture 细节声明为 P0/P1 时，CLI 拒绝并指出最高只允许 P2。
- AC-007：三种允许分类只有在逐项引用需求/方案、说明正常使用影响、说明为何不能留到开发阶段决定，并具备主代理独立分类与严重度核实记录时，才能形成 P0/P1 并阻塞开工；开发前 finding 不机械套用 REQ-001 的“当前实现错误”字段。
- AC-008：提示词和结果合同要求审查者为 P0/P1/P2/P3 每条 finding 显式输出分类并先判断正常使用后果，不得用具体实现偏好替代三类阻塞边界；P2/P3 缺少分类也按 AC-005 拒绝。

#### 来源

用户确认继续优化原反馈第 2 项。

### REQ-003：同一快照收齐结果后只创建一份统一返修

#### 要求

同一 review-ready 快照所选 QA 执行和全部门构成一个 review wave。所有必需结果解决前不得
派发返修；结果收齐并经主代理核实、归并后，每个 review-ready 快照最多创建一份逻辑返修
任务，包含该轮全部有效问题。

#### 验收条件

- AC-009：只有当前快照为 review-ready 且全部已选 QA mode 的 case-set 均为 FROZEN 时才能初始化 wave；初始化时冻结全部已选 QA execution 与门组成的 expected-set/digest，host/scheduler 一次准备全部 eligible 目标并并行派发，CLI 不引入目标间串行依赖并显示尚未准备、在途和已终结目标。同一 wave 中增删改目标被拒绝或使旧 wave 失效。任一目标仍为未准备、PENDING、在途、未处置 RUNTIME_ERROR/REJECTED_INVALID_RESULT 或 RETRY_EXHAUSTED 时，创建返修任务失败并列出未解决目标；PENDING/在途不能被处置为已收齐。用户明确处置错误后该目标对本 wave 终结但不得记为 PASS，处置事实和理由可审计，晚到结果不得自动重开 wave。
- AC-010：同一快照结果收齐后，主代理必须先对每份 finding 完成范围、严重度、复现核实和 NEW/SAME_AS/DISMISSED 裁决，并完成 canonical problem 归并；缺少任一核实或裁决时创建返修任务失败。完成后，全部 QA FAIL、有效 P0/P1 和门审 P2 一次进入同一返修任务。
- AC-011：同一快照已经创建逻辑返修任务后，再次创建被拒绝；返修运行错误只能重试同一逻辑义务。
- AC-012：存在白盒 QA 时，开发快照 S1 不初始化 wave；白盒测试先由 host 提交并推进到 S2 后执行 QA Review，后续 FAIL 修改或 PASS 后建议吸收造成的测试变化继续提交并推进快照。只有包含最终白盒测试且绑定全部 FROZEN case-set 的最终快照才成为 review-ready 并初始化唯一 wave，QA execution 与门都绑定该快照且门覆盖基线到该快照；无额外修改时最终快照即 S2。不同 review-ready 快照各自拥有独立 wave，旧快照结果不能被当成新快照已收齐。

#### 来源

用户确认实施原反馈第 3 项，并要求消除碎片化返修循环；用户随后确认
`finding-d948d18628d883e9`，要求补齐白盒测试进入最终快照后的 wave 初始化屏障。

### REQ-004：问题语义归并由主代理判断并由 CLI 持久化

#### 要求

每份原始/有效 finding 和每个 canonical problem 分别具有稳定 ID。主代理根据需求、正常失败
场景、受影响模块、根因和实际后果判断 NEW、SAME_AS 或 DISMISSED；CLI 保存裁决、引用和
理由，并据此阻止同一 problem 重复触发返修。

#### 验收条件

- AC-013：每份原始/有效 finding 持久化独立 finding ID；每次 NEW、SAME_AS 或 DISMISSED 裁决都保存主代理对需求、正常失败场景、受影响模块、根因和实际后果五个维度的判断、引用与非空理由；NEW 创建带稳定 problem ID、上述五维、证据及 OPEN/RESOLVED 状态的 canonical problem；SAME_AS 绑定既有 problem 并保留两份 finding 证据但只维持一个返修义务；DISMISSED 不创建 problem。
- AC-014：与已解决 problem 归并的新 finding 不再次触发返修；与已 DISMISSED finding 相同的新报告只增加审计记录，不创建 problem。
- AC-015：根因或实际后果不同的 finding 可以记录为新问题；较晚 wave 的新 P0/P1 标记前轮漏审并正常阻塞。
- AC-016：CLI 不根据自由文本 hash 自动决定语义重复；缺少主代理裁决时不能静默归并。
- AC-017：错误归并可通过带 revision 冲突检查的新裁决纠正，原始 finding、problem 和历史裁决保持可追溯；返修派发前的纠正更新同一逻辑计划，派发后才产生的新 OPEN 义务进入返修所得新快照的下一 wave，不得为原快照创建第二份返修。

#### 来源

用户询问并确认由主代理判断重复；本方案保留 CLI 的确定性持久化和执行门控责任。

### REQ-005：门审 P2 返修不复审，门审 P3 只在 Seal 展示

#### 要求

门审 P2 是必须处理的问题，可以单独创建一次统一返修，但修复后不重新派门审、不增加审查
轮次。门审 P3 不进入返修任务，也不阻塞 Seal；Seal 前集中展示全部尚未处置 P3。

#### 验收条件

- AC-018：只有门审 P2 时生成一份返修任务；开发者完成完整构建、全量项目测试和受影响用例逐项自跑后，为提出 P2 的门生成绑定问题 ID、修复快照和原 gate PASS 的完成/carry 凭据，不要求该门重审即可把 PASS 合法带到修复快照，且 completed review-wave、extra review-wave 及等价审查轮次计数均保持不变。其他已通过门按既有影响判定 carry 或处理，不因 P2 自动重审；QA 只执行 AFFECTED，未受影响结果继承。
- AC-019：QA FAIL 或有效 P0/P1 已触发返修时，同轮门审 P2 合并进入同一任务，不单独派发第二份返修。
- AC-020：只有门审 P3 时不创建返修、不增加 review wave，并允许在其他必需结果通过后进入 Seal。
- AC-021：Seal 输出一次性列出全部未处置门审 P3，展示本身不改变 PASS 或要求逐项询问。
- AC-022：原先被记为 P2、经证据核实实际影响正确性的问题只有在补齐 REQ-001 全部阻塞证据和主代理独立复现记录后才可重定级为 P1，并按 P1 规则阻塞。

#### 来源

用户明确决定“门的 P2 返修但不复审，P3 不做处理，Seal 时展示”。

### REQ-006：QA Review P2/P3 吸收完成后冻结用例集

#### 要求

QA Review 的逐用例 FAIL 和集合级 P1 继续阻塞并要求重审。集合级 P2/P3 都是结构化、非阻塞
建议；总体 PASS 后由 QA 设计者独立选择具体测试方法并按稳定 suggestion ID 逐条吸收，但吸收
修改不触发新的 QA Review。只有 PASS 且全部 P2/P3 吸收完成，case-set version 才冻结。

#### 验收条件

- AC-023：逐用例 FAIL 或集合级 P1 使该 mode 的 QA Review FAIL，设计修改后必须重新审查。
- AC-024：集合级 P2/P3 不把 QA Review 总体改为 FAIL，但都能以稳定 ID 和处置状态记录。
- AC-025：存在未吸收 P2/P3 时 case-set 不能进入 FROZEN，也不能用于声称 QA 设计已经最终稳定；QA Review PASS 且建议集合为空时立即生成 FROZEN version，不要求空吸收轮。
- AC-026：只有绑定的 QA Review 已 PASS 时才允许进入 suggestion-absorption；QA 设计者独立决定具体测试方法，通过 suggestion-absorption 修改该轮 P2/P3 时，每项吸收引用稳定 suggestion ID，新增、修改或删除用例均保留建议来源并直接获批，不派新的 QA Review；冻结前必须重新通过完整 source/point/case、unbound AC 和 approved-whitelist 机械校验。
- AC-027：绑定的 QA Review PASS、所有 P2/P3 逐条吸收且覆盖完整性重算通过后生成新的冻结 case-set version；缺少或失效的 PASS、遗漏、重复、引用无关 suggestion、重复吸收，或导致任一已确认验收点失去合法覆盖的修改均被拒绝。

#### 来源

用户确认 QA Review 使用独立规则，并最终决定“PASS 且 P2/P3 返修完成后冻结”。

### REQ-007：普通代码返修不重新设计冻结 QA 用例

#### 要求

冻结用例只有需求含义、对外行为、成功/失败判据或可执行性变化时才解冻。普通代码返修只
执行受影响用例和相关既有通过用例；启用 requirement guarantee 时最终候选在 Seal 前完成
全量回归，非保证路线只按其现行项目或 route 规则决定是否最终 FULL。

#### 验收条件

- AC-028：需求、对外行为、判据和可执行性均未变化时，代码快照变化不改变冻结 case-set version，也不允许重新派 QA Design/QA Review。
- AC-029：四种合法变化可以带明确类别、理由和来源 revision 解冻对应 mode；THAW 后受影响用例回到待设计/待审，相关旧执行和 carry 失效，未受影响批准状态保留，重新满足 REQ-006 后才能生成新 FROZEN version；其他理由被拒绝。
- AC-030：普通中间返修固定使用 AFFECTED，子集必须包含先前失败用例及主代理根据修复路径判断相关的既有通过用例；其余合法 PASS 继承，不再逐轮询问 FULL/AFFECTED。
- AC-031：启用 requirement guarantee 的中间返修同样使用 AFFECTED，本条明确取代旧 3.5a guarantee 的“每次执行均 FULL”要求；最终 Seal 前必须在最终候选上记录 FULL PASS。非保证路线仅在其现行项目/route 规则要求时执行最终 FULL，不新增无条件全量回归。
- AC-032：原用例因稳定验收条件变化而确实不可执行时，可以由 QA 设计者独立选择测试方法并解冻修订；一次性环境不可用只记 RUNTIME_ERROR，不构成 THAW。

#### 来源

用户确认优化原反馈第 6 项，避免普通实现返修反复触发 QA 设计和用例复审。

### REQ-008：门审不接收 QA 用例并保护非显然验收接口

#### 要求

复杂度审和实现质量审不注入 QA 用例或逐用例执行证据。对承担文档化验收职责、但在仓库内
可能看不到普通调用者的公开接口，使用局部 acceptance-surface 注释引用需求职责，防止被
误判为多余接口。

#### 验收条件

- AC-033：开发后门审正式 prompt 不包含已批准 QA 用例正文、逐用例执行证据或测试实现细节。
- AC-034：承担非显然公开验收职责的接口在代码附近具有简短需求/AC 引用，门审提示词要求在判定其无用、建议删除或据此形成 finding 前核对该职责。
- AC-035：普通内部接口不因本要求批量增加模板化注释；注释不能替代对应需求、测试或实现。

#### 来源

用户否决向门审提供 QA 用例，并提出通过注释避免验收接口被误删。

### REQ-009：额外返修授权前展示完整且绑定版本的摘要

#### 要求

额外返修授权必须先生成不修改状态的完整摘要，实际授权引用摘要 digest。摘要展示当前流程
成本、整轮问题和预计重跑范围；阶段 4 未接入前不得伪造 engine 累计额度。

#### 验收条件

- AC-036：授权预览包含当前审查轮次、剩余自动轮次、派发次数、运行时长、本轮全部 QA FAIL、有效 P0/P1、门审与 QA Review P2/P3 数量和处置状态，以及预计重跑的构建/项目测试目标和 QA mode/case。
- AC-037：预览是完全只读计算，不创建或修改 run state、回执、授权记录或 state revision，也不增加授权次数或 repair wave；实际授权必须绑定预览 digest 和生成预览时读取的 state revision。
- AC-038：预览后状态变化使旧 digest 失效，实际授权失败并要求重新生成摘要。
- AC-039：阶段 4 未接入时 engine 累计派发额度显示 `UNAVAILABLE`，不能显示主代理估算值或虚假的剩余额度。

#### 来源

用户确认实施原反馈第 9 项，并确认 3.5b 尚未运行时接入时的 `UNAVAILABLE` 展示边界。

### REQ-010：相同权威输入不重复派发

#### 要求

CLI 根据派发类型、目标、mode、基线、当前快照、需求 revision、QA case-set version、
catalog/prompt revision 和动作专属输入身份生成权威 signature，并按已有在途状态或经主代理
裁决后的有效最终结果决定返回既有派发、拒绝、返修或允许重试。

#### 验收条件

- AC-040：完全相同 signature 已有 OPEN/PENDING/CLAIMED 时只返回既有 dispatch，不创建并行重复项；已有 PASS 时再次准备失败且不创建新 dispatch，拒绝信息明确说明已有最终结果，不要求重建已正常清理的 QA 工作区。
- AC-041：完全相同 signature 已有 FAIL 时不创建新 dispatch，并按派发类型返回既有合法待办：product-review/start-readiness 返回 finding 处置、需求修订与对应重审状态；QA Review 返回 QA Design 修改、case-set 版本变化与对应重审状态；只有开发后 QA execution/gate 的有效 FAIL 在所属 review-wave 收齐且主代理完成核实、裁决和归并后，才指向该快照统一返修。已 DISMISSED 或已归并到 RESOLVED problem 的原始 FAIL 不触发任何新任务。上述分流不得增加或重置自动轮次、attempt、review-wave/repair-wave 额度，也不得替代适用的用户授权。
- AC-042：RUNTIME_ERROR 或 REJECTED_INVALID_RESULT 结束当前 attempt 后，只要逻辑义务仍有效且有限 attempt 尚有余量，action、QA 或 gate 都允许用新 dispatch 复用原 TaskKey 并保留前次错误；该重试不要求义务属于 wave，wave 成员才额外要求 wave 仍为 COLLECTING 且目标未处置。余量耗尽时进入可展示的 RETRY_EXHAUSTED 并拒绝普通重派；用户可用非空理由和 state revision 为同一 TaskKey 追加一次且仅一次单 attempt，更新 retry policy/basis digest 但不创建新义务。追加 attempt 仍失败后，必需 action/QA Review 保持未通过；wave 成员只能显式结束收集且不得记为 PASS，必须终止 run 或由已确认的新计划改变 binding/snapshot/责任后形成新 TaskKey。处置后晚到结果只留审计记录。
- AC-043：用户明确重审必须提交非空原因并形成 override 审计记录，不能覆盖或删除原最终结果；若该动作仍有自动额度则正常消耗，自动额度耗尽时必须先取得适用的额外审查授权。需要额外返修时仍须使用绑定最新摘要 digest/state revision 的 `authorize-repair`，重复派发或 override 不能绕过任一上限与授权。
- AC-044：signature 覆盖派发类型、目标、mode、基线、快照、需求 revision、QA case-set version、catalog/prompt revision 和动作专属输入身份；任一实际变化时形成不同 signature，并按新输入正常判断。

#### 来源

用户确认实施原反馈第 10 项，限制同一内容反复交给新代理；用户随后确认
`finding-6162393df5735ce0`，要求非 wave 审查也具备可执行的无效结果重试与耗尽终结路径；
用户确认 `finding-b8c963ba545cecd4` 后要求不同类型 FAIL 分流，并明确分流、重审或返修不得
绕过现有上限与授权。

### REQ-011：流程规则文档与自动回归必须和程序一致

#### 要求

更新 formal-gates 的技能文档、流程 reference、审查提示词和项目代理规则，删除重复或冲突
描述，并用自动测试覆盖本需求的关键状态不变量。

#### 验收条件

- AC-045：`SKILL.md`、`references/formal-flow.md`、README/README_EN、start-readiness、qa-review、qa-design、qa-execution、carry、development-worker、reviewer-base 和相关 gate prompt 对严重度、P0/P1 定级理由、finding 处置前等级与理由展示、统一返修、QA 冻结、AFFECTED/FULL、P2/P3 及 requirement impact 回退授权规则描述一致；从非权威重复持有者删除重复、冲突和已由 CLI 强制的过程描述，每条规则仍保留一个明确的人类可读 canonical owner，prompt 只保留代理执行所需行为。
- AC-046：项目 `AGENTS.md` 不复制 CLI 已强制或 formal-gates 已持有的流程细则，只保留项目专属事实纪律、执行边界和引用。
- AC-047：自动测试覆盖 P0/P1 非法结果或定级理由不足只形成 rejected receipt，product-review/start-readiness/QA Review 等非 wave 义务与 wave 义务都可按同 TaskKey 有限重派，attempt 耗尽进入 RETRY_EXHAUSTED、只允许一次单 attempt 授权、再次失败不产生 PASS 或新派发；obligation item FAIL 不自动升级 P1；start-readiness 全严重度分类存在性/分类证据/上限及相同恢复；白盒 S1 不初始化 wave、测试提交/QA Review/建议吸收后最终 FROZEN case-set 与快照绑定、最终 review-ready 快照只初始化一个 expected-set 且全部 eligible QA execution/门并行、门覆盖最终测试代码；未处置/已处置 RUNTIME_ERROR/REJECTED_INVALID_RESULT 与晚到结果、wave 收集屏障、主代理核实归并屏障、每 review-ready 快照唯一返修、finding/problem 五维裁决与理由持久化/归并纠错、新 P0/P1、门审 P2 完整自测/carry 且对应门不复审也不增加轮次、其他门不因 P2 自动复审、门审 P3、门 prompt 不注入 QA 用例、acceptance-surface、QA Review 无建议立即冻结、PASS 前禁止建议吸收、P2/P3 增删改吸收后的覆盖重算与冻结、THAW 后重审、普通返修固定 AFFECTED 与 guarantee 最终 FULL、完全只读授权摘要 digest/重跑范围失效、未处置 finding 投影包含 ID/severity/severity rationale、相同 FAIL 按 product-review/start-readiness、QA Review、开发后 QA execution/gate 分流且不产生重复派发或跨类型返修、输入变化/override 不绕过现有额度与授权、`meaning-preserved` 保留产品审 PASS 且无额外回退授权、`meaning-changed` 缺少或使用过期用户确认时零状态变化、在途及最终结果重复派发和 Seal 行为。
- AC-048：原反馈中依赖 legacy 累计限额的测试不在本 Batch 伪造实现；对应运行时止损验收保留给 3.5b/阶段 4 边界。
- AC-049：所有必需结果通过、门审 P2 已修复且只剩门审 P3 时，Seal 正常执行并展示 P3。

#### 来源

用户确认实施原反馈第 12、13 项，并明确第 8、11 项不在 Batch A 重复实现。

### REQ-012：owner 成本在现有单一投影中精确或明确不可用

#### 要求

在能确定 owner transcript/provider 和启动到终态区间时，记录 owner 用量差值；无法精确核对
时记录 UNAVAILABLE。owner 与 dispatch 分开、终态记录幂等，并继续使用现有单一 cost 投影
和 token 合计逻辑；新增元数据持久化于同一 legacy RunState，不要求旧版本状态可读，
也不为旧 finding 格式、摘要或旧内部调用合同保留兼容分支。

#### 验收条件

- AC-050：workflow start 在来源稳定时把 owner 基线、provider 身份和完成标记持久化于同一 legacy RunState，Seal/Abort 记录同一来源的终态差值，不包含启动前历史用量；当前版本正常生成的状态可读，不要求读取旧版本状态，且不保留仅为旧 finding 格式/摘要或旧内部调用合同服务的兼容分支。
- AC-051：缺基线、缺终态来源、不支持 provider、文件截断重写或重叠活动区间时 owner 状态为 UNAVAILABLE、数字为零且不写估算 token。
- AC-052：重复 Seal/Abort 或恢复后再次终态处理不重复累计 owner 用量，也不改写 dispatch 明细或任何 action、gate、QA 最终结果。
- AC-053：owner 记录位于现有 RunCost 的可选独立字段，不伪装成 dispatch、不增加第二套 ledger；顶层 token 总计精确包含 dispatch 与本 run owner 且各自只计一次，child/master owner 汇总留到阶段 5。
- AC-054：保留当前版本的 dispatch 计量、缺失/不可解析处理和汇总行为，不要求旧版本格式或调用合同兼容；`internal/cost` 不反向依赖 validate/engine，parser 可独立测试，`report.cost` 继续只负责报告。

#### 来源

用户确认把阶段 3.5b 成本控制 checkpoint 与本需求合并实施。

### REQ-013：engine guard 纯计算得到有限且稳定的派发额度

#### 要求

engine 提供无 I/O、无状态写入的生产 guard 计算。它从 canonical external TaskKey、确认的
basis 身份和有限 retry policy 计算基础槽位、retry 余量、总额度与稳定 digest，不接受任意
主代理估算或无界重试。

#### 验收条件

- AC-055：0、1、N 个 distinct external TaskKey 与 MaxAttempts=1、2、n 的已知输入按 `requestAllowance=distinct TaskKey 数`、`retryAllowance=Σ max(0, MaxAttempts-1)`、`totalAllowance=requestAllowance+retryAllowance` 得到精确稳定结果；测试直接调用生产计算。
- AC-056：新增一个 distinct TaskKey 恰好增加一个基础槽位，完全相同的重复 TaskKey 对基础槽位和 retry 均只贡献一次；单个有限 MaxAttempts 增加 1 恰好增加一个 retry slot。
- AC-057：未显式提供 retry 的 external task 使用 canonical MaxAttempts=2；MaxAttempts 小于 1 或冲突声明被拒绝。
- AC-058：相同 canonical basis 得到相同 budgetBasisDigest；requirement、confirmed Batch/route/topology、definition、approved QA、selected gate、established obligation/repair、TaskKey provenance 或 retry policy 任一 basis 身份变化时 digest 变化。
- AC-059：缺少 closed-world definition、确认 revision 声明、允许 provenance 或完整身份的 external TaskKey 被拒绝；生产纯核心不执行文件、transcript、状态、网络或其他 I/O，不写任何状态、不签发任务，也不认证确认事实。阶段 4 Controller 才从权威 state 构造 basis；需确认的扩张在确认前不得扩容，预授权 repair 可机械加入，普通 finding/result/receipt/resume 不得增加 TaskKey。同一逻辑义务的 RUNTIME_ERROR/REJECTED_INVALID_RESULT 重派复用 TaskKey 并消耗 retry；一次性用户追加 attempt 只更新该 TaskKey 的有限 retry policy 并改变 basis digest，不增加基础槽位；新的有效义务或经确认新计划导致 binding/snapshot/责任变化时才产生新 TaskKey。

#### 来源

阶段 3.5b 既有重构计划及用户确认的合并实施决定。

### REQ-014：guard-preview 提供可黑盒验收的公开只读入口

#### 要求

正式 CLI 提供 `formal-gates workflow guard-preview --input <basis.json>`，通过生产 guard 核心
计算并输出 JSON。该入口只用于确定性、非权威的计算预览和验收，不创建 run、不写状态、不
认证真实 run、确认状态或任务合法性，也不声称阶段 4 已接入运行时止损。

#### 验收条件

- AC-060：对结构合法 basis.json，正式构建产物输出规范化 basis 摘要、request allowance、retry allowance、total allowance 和 budgetBasisDigest，退出状态为零；成功路径不创建 run、不创建或修改任何 run state，该成功只代表计算输入自洽，不认证真实 run、确认状态或任务合法性。
- AC-061：输出明确包含 runtimeIntegration=`NOT_INSTALLED`，不得让用户误认为 IssueFromPlan、refill 或 retry 已受 guard 拦截。
- AC-062：文件不可读、JSON/schema 非法、身份缺失、retry 非法或 TaskKey 冲突时返回非零和可定位错误，且不创建或修改 run state。
- AC-063：黑盒 QA 能仅通过文档化命令、输入 fixture 和 JSON 输出判定计算正确性，不执行仓库内已有测试套件作为主要证据。
- AC-064：CLI 只调用生产 guard 计算，不复制 allowance 或 digest 算法。

#### 来源

用户补充“Batch B 要提供入口，不然黑盒没法测”，并确认本命令形式。

### REQ-015：未来 engine 接入计划继承本需求的新语义

#### 要求

本需求实施时同步未来 engine 的权威需求、最终方案、3.5b 子计划、增量阶段计划和迁移矩阵，
使后续阶段 4 不会重新引入旧的开发后定级、P2/P3、QA 冻结或 guard basis 规则。这里只更新
未来接入合同，不提前实现 engine State、source bridge、运行时 guard 或 typed Ask。

#### 验收条件

- AC-065：engine 权威总需求与最终方案的穷举公共面加入 `workflow guard-preview`，并明确把开发后主代理核实建模为 Operator semantic adjudication，保存 raw finding、逐条 severity rationale、范围/严重度/因果/复现证据、finding/problem ID、NEW/SAME_AS/DISMISSED 裁决及历史；Controller 只做完整性、版本绑定和机械路由，不自动判断语义真假、根因、严重度是否成立或重复。
- AC-066：engine 计划加入非 wave 与 wave 共用的无效结果义务生命周期、RETRY_EXHAUSTED 及一次性追加 attempt，白盒最终 FROZEN case-set 与 review-ready 快照屏障、review-wave 收集屏障、每 review-ready 快照唯一返修、门审 P2 carry 且不复审、门审 P3 Seal advisory、QA Review P2/P3 吸收后冻结、合法 THAW、dispatch signature 和绑定 digest 的授权摘要。
- AC-067：对于开发后 QA/门 finding，engine 的 Ask 计划明确严重度、范围、根因和重复关系核实属于 Operator 而非用户 Ask；只有已核实 finding 的处置分支仍不唯一时才可请求用户决定，额外返修 Ask 必须展示绑定版本的完整摘要。既有产品/技术审查 finding 的确认或驳回 Ask 规则保持不变；需求影响为 `meaning-preserved` 时不产生用户 Ask 并保留产品审 PASS，`meaning-changed` 则必须由 engine 在接纳 revision 转换前发出绑定影响预览的 typed Ask，只有用户确认事件才能授权回退。
- AC-068：对于由 finding/返修派生的增量 basis source，3.5b 子计划和阶段 4 只接受主代理 adjudication 完成、wave 收齐后形成的有效 obligation/唯一返修 TaskKey；基础 basis source 继续采用 REQ-013 所列来源，raw reviewer finding、普通 result/receipt/resume 不得扩容。计划同时写清：`guard-preview.runtimeIntegration` 在阶段 3.5b 固定为 `NOT_INSTALLED`、阶段 4 切换为权威 engine 状态；修复授权摘要中的 engine allowance 在阶段 4 改用权威 projection；owner 成本和 token cap 的 `UNAVAILABLE` 继续按各自数据源或配置条件独立存在。
- AC-069：`incremental-seal-plan.md` 与 `route-matrix.md` 增加上述能力的阶段 4 迁移合同、`guard-preview` 只读入口和 requirement impact 授权迁移，同时保持 engine run 的单一 writer；阶段 4 接入后 legacy 写入口不得成为第二 writer，也不得绕过 engine 对 `meaning-changed` 用户确认事件的校验。
- AC-070：ADR-001/002 不因本需求改写；历史 `stage-records.md` 不追溯修改，在 Batch A/B 实施与验证完成后、最终候选确认或 Seal 前追加本次 checkpoint；阶段 5–7 继承同一 finding/problem ID、归并、唯一返修、P3 advisory 和 QA 冻结语义。本次实现 diff 若提前加入 engine State、source bridge、运行时 guard、typed Ask 或其他阶段 4 写路径，则本 AC 失败。

#### 来源

用户要求独立核实主代理定级能否接入 engine，以及本需求完成后是否必须修改后续计划；核实
确认现有计划只部分承接并存在 gate P3、根因裁决等直接冲突，用户随后确认一并修改。

### REQ-016：产品审或技术审修订后只确认变更部分

#### 要求

首次受理仍确认唯一正式需求文件全文。产品审或 start-readiness 技术审的 finding 经用户处置
并导致需求文件修订时，主代理必须在编辑前保留 digest 与上一已确认 revision 一致的完整原文，
据此生成并逐 hunk 核实穷尽、可审计的 review-delta；CLI 不保存 base 全文或 diff，只预检完整
候选并校验、记录确认绑定。用户只确认该变更包，未变内容自动继承，同时完整候选文件绑定为
新的完整 revision。

#### 验收条件

- AC-071：首次受理继续展示唯一 canonical 正式需求文件和精确 revision 并取得全文确认；run 只能有一个可被确认并作为 requirement source 的正式需求文件，其他绑定验收附件不得成为第二个需求源或另行要求用户确认；本要求不把初次确认改成增量确认。
- AC-072：产品审或 start-readiness finding 经用户确认并实际导致需求文件变化时，主代理必须在首次编辑前读取并保留上一已确认 revision 的规范化完整文本，确认其 digest 匹配 run state 后生成全部新增、删除、修改 hunk 及必要上下文；重新确认只展示该穷尽变更包，不要求用户再次确认未变全文。变更包逐项标明受影响 REQ/AC 或非需求章节、来源 action/finding ID、处置决定和影响摘要；主代理逐 hunk 核对完整性与来源归属。
- AC-073：CLI 只有在收到主代理逐 hunk 核实记录后才写入 review-delta 确认回执；回执绑定上一已确认 base revision、完整 candidate revision、穷尽变更包 digest、来源 finding 集合和用户确认来源。候选文件、base revision、finding 处置或核实记录变化后旧回执失效且确认返回非零；CLI 不把 base 全文或 diff 持久化进 run state，也不创建第二份需求源。
- AC-074：增量确认前 CLI 对完整候选文件执行与全文确认相同的结构预检，并校验候选 digest、base/candidate revision、变更包 digest、来源 finding 和主代理核实记录之间的绑定；确认后冻结完整 candidate revision 和 guarantee envelope，未变内容从 base 继承，后续产品审、技术审、QA 和开发读取完整新 revision。主代理未在编辑前保留 digest 匹配的完整 base 文本、无法恢复精确 base 或未完成逐 hunk 核实时，CLI 拒绝增量确认，不允许用摘要、REQ/AC 投影或猜测内容代替。
- AC-075：主代理发现与已处置产品审/技术审 finding 无关的额外 hunk 时，不得通过 review-delta 静默确认；必须移除或作为新用户需求单独对齐。`SKILL.md`、`formal-flow.md`、requirements-clarification/product-review/start-readiness 提示词、README/README_EN、未来 engine 计划和自动回归测试均采用同一责任边界，并覆盖编辑前 base 捕获与 digest 核对、多 finding 合并、删除/移动、过期变更包、缺失核实记录、无关 hunk 拒绝及用户自愿查看全文不改变确认范围。

#### 来源

用户明确要求产品审或技术审修改需求后只重新确认更改部分，不再全文确认；用户确认
`finding-ae96ee58ecab6f86` 后指定 base 原文保留、变更包生成和逐 hunk 核实由主代理负责，CLI
只承担候选预检、绑定校验和回执持久化。

### REQ-017：用户处置审查 finding 前必须看到严重等级和理由

#### 要求

产品审或 start-readiness 的未处置 finding 请求用户确认或驳回前，主代理必须展示每条 finding
的稳定 ID、当前严重等级、逐条定级理由和问题摘要；同等级项目可以分组，但映射必须无歧义。
漏显任一等级或理由时不得登记处置，必须补齐后重新取得用户决定。

#### 验收条件

- AC-076：`workflow show` 及供主代理消费的未处置 product-review/start-readiness finding 投影始终逐条包含稳定 finding ID、当前有效 severity、severity rationale 和 message；重定级后展示当前有效等级及其理由，同时保留历史裁决审计。
- AC-077：`SKILL.md` 作为 canonical owner 明确要求在调用 `settle-findings` 前，在同一决策消息中为每条未处置 finding 展示 ID、严重等级、逐条定级理由和摘要；`references/formal-flow.md` 写清操作顺序、允许同等级无歧义分组，并明确漏显等级或理由时先前回复不能作为正式处置来源。
- AC-078：自动回归验证未处置 finding 投影不丢 ID/severity/severity rationale、重定级后展示当前等级及理由，并对 `SKILL.md` 与 `formal-flow.md` 的该规则做一致性检查；本要求不新增第二套 finding 账本或由 CLI 推断主代理自然语言是否充分展示。

#### 来源

用户指出本轮处置请求漏显严重等级，并要求核对现有规则；核对确认规则未明确写出后，用户要求
补充。

### REQ-018：所有正式 P0/P1 必须逐条证明定级成立

#### 要求

产品审、start-readiness、QA Review 集合级 finding 和开发后门审的每条 P0/P1 都必须带独立、
可核验的 severity rationale。条目审查 FAIL、字段齐全或审查者写了 P1/P0 都不能代替定级证明；
只有实际后果达到对应定义时才允许形成 P0/P1。

#### 验收条件

- AC-079：每条 P0/P1 finding 独立保存非空 severity rationale，绑定 finding ID、当前 severity、需求 revision、dispatch/result 和证据位置；整批共用套话、仅复述严重度定义、仅写“item FAIL”或“违反需求”均不合格。
- AC-080：P1 rationale 必须指出对应已确认需求、正常使用或验收路径、遗漏或错误造成的实际后果，以及为何存在该后果时 P2 仍不足；P0 还必须说明为何需求无法成立、核心验收无任何可用路径或交付被阻塞。开发后门审继续额外满足 REQ-001；开发前审查不虚构“当前实现错误”。
- AC-081：`record-action`、QA Review 集合级 finding 记录和 `record-gate` 在 P0/P1 缺少合格 severity rationale 时返回非零，不写有效 finding、FAIL 或 review-wave/problem/guard basis，只形成绑定原 attempt 的 rejected-result 回执，并对非 wave 与 wave 义务统一按 AC-042 的有限重派、RETRY_EXHAUSTED 和一次性追加 attempt 规则恢复。QA execution 的逐用例 FAIL 不是 severity finding，不受本字段约束。
- AC-082：产品审 obligation-to-AC 条目的 FAIL 只表示存在覆盖缺口，不自动决定 finding severity 或 action 总体 FAIL；实际后果只达到 P2/P3 时，该 finding 按 P2/P3 记录且产品审总体保持 PASS，只有具备 AC-080 理由的 P0/P1 才使该轮 FAIL。
- AC-083：主代理记录前必须逐条核实 severity rationale 的需求匹配、正常路径、后果和 P0/P1 阈值，不能用一份批量 `operator-evidence` 替代逐条理由；`SKILL.md`、`formal-flow.md`、reviewer-base、product-review、start-readiness、qa-review、相关 gate prompt、README/README_EN、未来 engine Operator adjudication 计划及自动回归测试采用同一规则。

#### 来源

用户在确认本轮产品审问题后追问每条 P1 是否有站得住脚的理由；核对发现原结果没有逐条说明
为何不能定为 P2，用户重申 P0/P1 不得乱定；用户随后确认 `finding-6162393df5735ce0`，要求
定级理由不合格形成的 rejected result 在非 wave 审查中也有完整恢复和耗尽终结路径。

### REQ-019：会回退流程的需求影响登记必须由用户授权

#### 要求

`meaning-preserved` 不增加额外回退授权，并保留已经通过的产品审；`meaning-changed` 会作废
既有 PASS 并回退流程，主代理只能提出建议，CLI 必须在任何状态变化前取得绑定影响预览的
用户明确确认。阶段 4 接入后由 engine typed Ask 和单写者状态转换强制同一边界。

#### 验收条件

- AC-084：`meaning-preserved` 不要求独立的 requirement-impact 用户授权，登记后保留产品审 PASS，只把实际受影响、原本未通过或尚未完成的 start-readiness 状态置为待做；需求正文变化仍须按 REQ-016 只确认 review-delta，不得把“无需回退授权”解释为无需确认变更内容。
- AC-085：`meaning-changed` 实施前先生成完全只读的影响预览，至少包含 base/candidate requirement revision、被认定改变的产品目标/范围/对外行为/验收标准、将作废的 PASS、回退步骤、产品审已用/剩余额度和适用授权状态；预览不修改 run state、结果、额度或 revision。
- AC-086：CLI 只有收到绑定影响预览 digest、生成预览时的 state revision、base/candidate revision 和明确回退后果的用户确认来源，才允许原子登记 `meaning-changed`；合法确认被接受时，同一原子转换必须登记 candidate revision、作废绑定旧产品含义的 product-review PASS、把 product-review 置为 PENDING，并使依赖该结果的下游状态失效；该转换不重置或增加审查/派发额度，也不自行授权下一轮产品审。缺少确认、只有主代理建议、绑定不匹配或确认过期时返回非零，任何 PASS、流程阶段、额度和 requirement state 均保持不变。
- AC-087：同一条用户确认明确覆盖完整 review-delta 和影响预览时，可同时满足 REQ-016 的变更内容确认与本要求的回退授权，不得要求用户重复确认；只确认文字变更但未展示回退后果时不能授权 `meaning-changed`。
- AC-088：CLI 不自动判断自然语言是否真正改变产品含义；主代理负责提出 `meaning-preserved` 或 `meaning-changed` 建议，CLI 负责影响展示、用户授权绑定和状态转换门控。文档与提示词不得把主代理建议写成用户决定。
- AC-089：未来 engine 权威需求、最终方案、阶段 4 增量计划和迁移矩阵把 `meaning-changed` 建模为 engine typed Ask：engine 是需求 revision 转换的单一 writer，未收到绑定当前状态和影响预览的用户确认事件时不得回退或签发新产品审；`meaning-preserved` 不产生该 Ask 并保留产品审 PASS。本 Batch 只更新计划，不提前实现阶段 4 写路径。

#### 来源

用户发现主代理把技术方案修订误登记为 `meaning-changed` 后明确决定：不回退流程的
`meaning-preserved` 不需要额外确认；会回退流程的 `meaning-changed` 必须由用户确认，主代理
最多提出建议，并要求未来由 engine 强制。

## Batch、依赖与完成定义

Batch A 对应 REQ-001 至 REQ-011 及 REQ-016 至 REQ-019，先交付审查收敛、QA 冻结、审查后增量确认、requirement impact 回退授权、finding 等级展示和 P0/P1 定级证明行为。Batch B 对应 REQ-012 至
REQ-014，在 Batch A 的任务/返修语义稳定后交付 owner cost 与 guard 核心。REQ-015 是两批
完成时共同承担的未来计划同步项，不单独构成第三个实现 Batch。两者是否进一步拆成 retained
master 与切片 run，由 start-readiness 的粒度审查按正式流程判定；本需求只确认依赖顺序，
不提前选择 full/custom 路线。

完成定义：所有本需求 AC 均须由已批准 QA 用例覆盖，并在最终候选上验证通过；文档同步类 AC
可以把可执行文档或 route-matrix 核对写入对应 QA 用例的 procedure 与 oracle，但不能用该核对
替代 QA 用例；
所选门均通过；Batch A 与 Batch B 的公开命令、状态迁移、文档和测试一致，未来 engine 计划
不再保留与本需求冲突的旧合同；最终 diff 不包含阶段 4 运行时接入、legacy 累计限额或实时
成本面板。

## 确认状态与权威来源

本文件不保存可变确认状态。是否已确认以及后续流程可以消费的 revision，由 formal-gates
run state 持有；产品审、技术审、开发和 QA 只能读取用户确认后的当前 revision。
