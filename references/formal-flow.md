# 正式流程公共合同

本文件供用户查阅公共入口和维护者核对程序行为；角色任务由程序签发材料限定，文档不授予手工编排权限。
流程顺序由 engine/CLI 持有，[SKILL.md](../SKILL.md) 只约束受理和用户决定。本文件持有当前 workflow 的调用形式、
受理收据字段和步骤级执行边界。使用已登记安装的固定 launcher；候选隔离验证必须绑定
实际候选副本，不能借用全局 binary 或可变源码目录。

## 公共入口

workflow 的当前入口以 `workflow --help` 和各子命令帮助为准：

| 入口 | 职责 |
| --- | --- |
| `start` | 唯一 bootstrap 写入口，绑定已确认受理与当前安装身份。 |
| `drive` | 不携外部事件，执行确定性推进与恢复，返回下一个外部边界。 |
| `submit` | 唯一外部事件写入口，接纳用户决定、主动控制、worker 结果和原生 receipt。 |
| `show` | 只读完整 run 投影。 |
| `status` | 只读状态与可用操作。 |
| `next` | 只读下一边界及 freshness；语义选择准备确认载荷。 |
| `diagnose` | 原始只读诊断状态或终态摘要。 |
| `guard-preview` | 只读、非权威的生产 guard 计算，见 [guard 合同](guard-preview.md)。 |
| `progress` | 从原事实派生进度及原输出游标。 |
| `message` | 向原 worker 提交新语义文字或原输入文件字节。 |
| `handoff` | 为原开发职责准备受限转交 Ask。 |
| `replan` | 为原需求内计划调整准备确认候选。 |

安装、卸载、hook、lifecycle、canary 和脱离 run 的 gate 检查属于维护/诊断面，
不能推进业务或改写 workflow 状态。正常 loader 要求版本、definition/package 摘要和
installed-target 身份精确匹配；不匹配返回 `UNSUPPORTED_RUN_VERSION`。原始文件保留
供 diagnose，当前版本使用新 run ID，不提供兼容、迁移或第二 writer。

## 启动前受理

主代理先从仓库和已确认记录解决事实问题，只有影响范围、公开行为、验收或主要架构的
未决选择才询问。无待澄清问题时明确说明。regular 的完整需求与方案写入唯一正式
UTF-8 Markdown 文件；不要求用户原始输入采用固定模板。

主代理只创作标题、要求、验收条件、来源和方案的语义正文。固定 Markdown 章节、
REQ/AC 身份及未修改原文由 `requirement` 程序入口持有，不手写模板或重录已存内容。

```bash
formal-gates requirement append --root <repo> --file <新文件名> \
  --title <新标题> --requirement <新要求正文> --acceptance <新验收条件> \
  --source <实际来源> --solution <完整方案正文>
formal-gates requirement show --root <repo>
formal-gates requirement edit --root <repo> --item <原需求序号> --requirement <新正文>
formal-gates requirement edit --root <repo> --item <原需求序号> \
  --condition <原验收序号> --acceptance <新验收正文>
```

当前正式文件由原 run 的 intake 绑定取得；无 run 时使用 `show` 提供的 `--document` 序号。
省略字段保留原字节，新增名称和正文可创作。返回原完整材料与精确 revision，编辑或保存
不代表用户确认。全部改变产品/流程行为及需要 QA 保证的义务须进入需求点；纯实现位置、
代码边界与未来约束留在方案正文，不伪装为 QA 的 AC。

**次序固定为：结构预检 → 展示完整文件与精确 revision → 用户明确确认 → start。**
启动前预检使用 start 的材料准备模式，保存载荷，不创建 run：

```bash
formal-gates workflow start --precheck --document <原需求文档序号>
```

输出 `content` 全文、`revision` 精确内容摘要、`requirementDocument` 结构投影，以及
程序准备的 `intakeCandidate`。在项目目录执行时 root 缺省为当前目录，不做项目发现。
regular 的 `--document` 复用 requirement show 发布的原需求文档序号；lightweight 复用
`--precheck --materials` 发布的普通材料序号。唯一候选可自动选取。
`start --precheck --materials` 只读发布项目普通文件的材料序号，`--solution-document <序号>`
选择独立方案，缺省使用正式文件；可重复 `--artifact-document <序号>` 添加确认材料。
`materials` 含全部选定材料的规范化全文和各自 revision。路径、内容摘要、方案绑定与
固定 transport 均由程序组装，无需手抄。预检默认把完整 candidate 存入项目所属的
`.gates/tmp/intake` 载荷目录，返回 `intake` 序号；保存候选不代表确认，不创建 run，
不读取或写入 admission/registry。材料选择 flags 仅供 precheck；正常 start 只接收
已获真实用户确认的原载荷。精确 `--requirement/--solution/--artifact` 与
`--save-candidate` 文件接口仅供人工或程序集成。预检只核结构、字段、唯一性与边界；失败先修同一文件，不请求确认。修改后重新预检、
展示和确认。确认前不启动开发 run、不选择后续路线、不派发后续代理。结构预检的通过
不是产品审、技术审或 QA 的 PASS。

### Typed intake receipt

`intakeCandidate` 只是待确认的材料绑定，不代表用户已确认。展示全部 `materials` 全文与
精确 revisions。预检已由程序保存完整原对象；等待用户明确确认后，使用
`start --route <用户已选择的 regular|lightweight> --intake <原序号>`，只有一个载荷时可省略序号，多个时程序只展示原位置供选择。
精确 `--intake-receipt` 保留给人工或程序集成。代理不抽取 JSON、不抄写字段、路径、
摘要或全文。保存候选不会启动 run；确认 receipt 是受理材料，不是模型调用记录。

程序固定 `source/authority=stable-driver` 与 `transport=stable-launcher`，登记
`requirementSource/requirementRevision`、完整 `artifacts`、选定方案的
`solutionRevision/solutionDigest` 以及 canonical `digest`。可选 `coverage` 仅表达额外
需确认的方案义务；regular 的完整 REQ/AC 投影仍来自同一已确认需求案。预检不制造
确认、选择后续路线或自动调用 start。

内容 revision 将 CRLF 规范化为 LF 后计算。路径不能重复或越出项目；start 重算实际
材料内容，拒绝与 receipt 不符的文件。首次 drive 把确认与 intake digest 恰好登记一次，
后续步骤消费该确认 revision，不再询问同一全文确认。lightweight 不要求 REQ/AC 案，
但仍使用真实已确认需求/方案材料的 typed receipt；其准备使用同一命令加
`--route lightweight`，展示完整材料而不要求 REQ/AC 结构。

## 启动与 run 控制

```bash
formal-gates workflow start --run-id <新名称> --route regular --vcs <git|svn|p4> \
  --intake <原载荷序号>

formal-gates workflow start --run-id <新名称> --route lightweight --intake <原载荷序号>

formal-gates workflow drive
formal-gates workflow show
formal-gates workflow status
formal-gates workflow next
formal-gates workflow submit --request <当前控制类型>
formal-gates workflow next --ask <原序号> --choice <真实选择>
# 展示程序返回的当前确认内容，获得用户明确确认后：
formal-gates workflow submit --confirmation-candidate <原载荷序号>
formal-gates workflow diagnose --root <repo> --run-id <id>
formal-gates workflow diagnose --path <state-or-summary.json>
```

show/status/next/drive/submit/progress/message/handoff/replan 从原 engine state 或终态摘要选择当前顶层 run。只有一个时程序自动使用；多个时只读展示程序序号，以 `--run <序号>` 选择实际目标，不按最新时间猜测，也不保存第二个当前 run 指针。程序集成可显式传 `--run-id`；它不能与 `--run` 同用。确认载荷仅从所选原项目/run 的 payloads/control 取出，接纳继续核原候选的项目、run、revision 和 request。精确 `submit --confirmation` 供人工或程序集成，仍从原候选自动绑定目标。

不可打开的顶层 run 保留在选择列表中并显示原错误；选择它仍按原准入规则拒绝，其他当前 run 可按显示序号选择。完整只读诊断使用原 `workflow diagnose` 入口。

start/show/status/next/drive/submit 默认展示当前语义问题、短选择、必要确认材料及实际执行边界，
不输出内部派发材料或 adapter 指令。程序调用者需要完整原投影时显式使用 `--machine`；
语义 next/replan 默认在原 run 的 payloads/control 目录保存完整原对象并公布
`confirmationCandidate` 序号；纯 show/status/next 不写载荷。默认展示不改变接纳绑定。precheck 的完整材料与 revision 输出不裁减。

regular 未显式提供 `--base-snapshot` 时，由原 VCS provider Resolve 取得实际不可变基线，
再交既有 Start 强校验；显式精确 base 仍供人工或程序集成。regular 使用原生 VCS 基线；启动不声明 split 意向。唯一正式文件同时承载需求和方案时，
单一 artifact 的确认绑定需求保证，后续文件观测不自行改变其激活事实。实际 start 必须显式传入用户已选择的 regular/lightweight；未明确时先取得用户选择。预检缺省核正式需求结构，不因此选择启动路线。
lightweight 只留明确 unverified 记录，不开发、不快照、不做 QA/门验证。regular 不能
在开发中降为 lightweight。

`submit --request` 选择当前控制类型；程序创建 REQUEST Ask，不直接授权。
决定使用 `next --ask --choice`：默认展示的 `ask` 先编号当前 run
未决 Ask，再按子 run 的稳定顺序编号已建立的当前子流程 Ask；程序绑定原目标，`--machine` 保留本地投影。
`choice` 是真实用户选择，`reason` 只填写必要理由。程序保存完整
Event、原 request/token、当前 revision 和展示材料。默认只展示当前确认所需的语义内容，确认后 `submit --confirmation-candidate`
接纳原候选文件；陈旧候选沿原 freshness 检查拒绝，不能重新绑定到另一个 Ask。
完整确认文件提供原项目和 run 目标；显式提供 `--root` 或 `--run-id` 时只核对一致性。
同 Event 的精确重放由原接纳台账处理，不重放副作用。

运行中的 `businessNodes` 按当前 run 与已建立 child 的同一顺序编号真实职责。
`next --node <序号>` 只准备从该职责继续的原请求；提交保存的候选，或以
`submit --node <序号>` 创建原 Ask 后，
用户通过原 Ask 选择目标处置并给出理由。程序携带 from/to、原工作与副作用身份、
revision 和 freshness，用户不抄 run ID 或工作清单。缺少的输入仍由原职责准备，
终止选择仍走统一 cleanup；节点继续不会授权跳过必做门。

路线的候选门全集、计划绑定、审查候选和 finding、reset 保留与清理范围、需求变更的
原工作身份与 child 原材料、验证摘要与 revision 均由程序读取同一权威状态。模型仅
选择短序号或业务枚举、解释语义变化，不复述身份、摘要、路径前缀或原对象。
`gates`、review/candidate/finding、QA cases/obligations 均是一基序号；split 的 `owners`
按当前确认需求中 AC 的原顺序选择，0 表示 master-merge，正整数选择分片。开发提案、
需求与所有必须展示的原文由候选对象完整提供；不得把预览或保存当作用户批准。

主动 reset/abort 和需求变化先创建请求，再展示原 Ask 对应完整候选并等待真实确认。
活动 run 修订先冻结推进，`requirement` 仅填写含义分类、重走业务项、实际影响解释及
逐项 CARRY/REDO 选择；程序从正式需求文件取全文/revision，并装配原 work/child 身份。
引擎签发的 recovery、fresh、retry、额外审查和 skip/waiver 只决定其当前 Ask，不自己
创建相应授权。Operator 核实事实和建议，不能替用户授权。

## 原生运行时派发

Next 只返回 Ready、HostAction、Ask、Wait、Operator、Complete 六类边界。普通
`workflow drive` 在本 run 选定的平台自动派发已签发任务、收取原始结果和继续推进，
到真实用户决定、具体执行阻塞或完成时返回；不在各平台之间混派。控制 submit 接纳后
使用同一消费者推进，worker receipt/result 接纳不递归启动消费者。

材料、action/TaskKey/Attempt、候选、作用域、配方和原生身份由程序原样组装并在实际
调用前核验；角色只填写其语义结果。普通开发和返修的未完成交付沿原任务、原会话和
剩余职责继续，不当作失败重试。ADJUDICATE、受影响用例和门影响判断沿原签发的语义
职责执行；程序不猜 NEW/SAME_AS、受影响范围或用户批准。缺少配方、UNKNOWN 或无法
核实的原执行事实保留具体阻塞，不能靠模型自述、合成 transcript 或其他宿主证据推进。

同一原 job 的 owner、request、process、exit 和 native source 承载正常执行与恢复事实。
完整原结果只观察并通过原 submit 接纳；原 source 丢响应时先 reconcile，不能盲目重派。
spawn、result、lifecycle 和 HostAction receipt 绑定同一 action、provider 与 Attempt。

维护时可给 drive/submit 加 `--single-step`，沿同一业务处理与接纳路径推进后返回，
不自动消费后续原生任务。该选项不授予手工扩大 payload 或绕过执行前核验的能力。
受管 native/supervisor 程序入口与宿主工具前 hook 的覆盖分别取证；没有实机证据的
宿主裸派发/恢复/模型工具入口不宣称具备完整执行前准入保证。

已释放的分片沿各自原 run 消费同一条程序路径，父流程收取原分片收据，合并后集中 QA。
子流程的实际 Ask 连同原 requestId、选项和原因展示在父输出 children 中，用户仍决定
子流程路线或实际无法推进的处置。技术准备审的已接受原计划直接展示在 review.readinessProposal，
确认时审阅该完整计划及原 revision，不重新抄写计划摘要。拆分计划的 batch.owner
明确提出分片标识或主线合并职责；拓扑确认采用这些原标识，程序逐片验证其完整检查和
片内依赖，再从原已确认计划投影 child 的路线确认载荷。子计划摘要与确认字段由程序
产生，主线完整验证只采用主线职责的检查，child 不新增技术审或 QA。

## 开发之前

先独立产品审，再独立技术审；fresh review 读取完整最新确认文档。raw candidate、
Operator 逐项核实、用户 finding/remedy 决定与 effective status 分层保存。请求用户
处置前，逐项展示问题、正常路径错误、后果、建议、ID、等级及定级理由；不完整展示
不能作为授权。严重度和项目范围唯一以 [审查契约](../prompts/reviewer-base.md) 为准。

worker 完成后，`reviewVerifications` 展示独立核实待办及其原始完整材料。
主代理读取原材料、逐 finding 独立核实后，用 `next --review-result <原序号>`，
按展示的 checks 顺序逐项追加 `--review-evidence <实际核实证据>`；每条原 finding
依次使用 `--review-finding-valid true|false --review-finding-reason <实际证据>`。
需要更正等级时再给该项的 `--review-finding-severity` 与 `--review-finding-severity-rationale`；
整结果无效或不完整使用 `--review-invalid-reason`。程序绑定原 result，不由模型抄身份。
核实完成直接提交该程序候选；这一步是 Operator 事实，不请求用户授权。
只有接纳的有效完整审查计轮；有效 finding 随后各有原 Ask，`next --ask --choice` 自动绑定该项。

产品/技术审各自有限语义轮；有效审查计轮，修订不清零。P0/P1 确认修订后完整新鲜复审；
P2/P3 逐项处置并修订，不因级别本身复审。审查修订先生成穷尽 review-delta，用户确认
变更包；meaning-preserved 保留适用 PASS，meaning-changed 明示失效、回退和容量后授权。
用户主动变化以 ImpactSet 逐项决定重走/续用，UNKNOWN 按受影响建议处理。

技术审形成 `granularityReview`：Batch→Subtask、依赖、接口、状态边界、DoD、验证、
恢复与交接。审查者只选择既有检查调用或提出验证目标，不填写 command/parts；缺少调用由开发者准备，程序绑定并执行。
用户确认计划与精确 split 拓扑或 no-split 理由之后，确认 default/full/custom。
非分片 run 的 default 选择全部普通门、不含黑盒/白盒 QA；full 选择全部普通门与双 QA
（QA 阶段较重，适合大型任务）；custom 选择当前候选的非空子集或全集，保留 custom 身份。
推荐不代替用户确认；每个 child 独立确认，child 候选不含普通 QA，default/full 均选全部普通门。
Batch 是现有任务的分组，不新增生命周期；程序为每个开发 Batch 派发全新的零上下文代理，开发会话不跨批复用，Subtask 不能独立派发。
批次任务书只写仓库无法恢复的决策理由、已知问题/债务及下一任务，不复制可由 VCS、代码或测试推导的内容。TaskKey、Attempt、语义审查轮与业务返修轮分别计数。

## 开发与快照

开发只消费确认任务；选择黑盒时，在基线隔离区并发设计/审查，不接收实现、白盒或开发解释。
设计失败沿原动作的有限 attempt/语义轮或 typed Ask 及时处理。黑盒未 PASS 不挡开发，
但挡最终候选。批内正常返回、重试和恢复沿用原身份，替换必须有恢复失败事实和签发。

开发者返回语义完成情况；程序捕获原生差异并校验范围与 postcondition，
adapter 执行 track→commit→snapshot。选择白盒时，独立
审查覆盖并编写/复用测试，只有测试变化推进快照。全部已选 QA 的 FROZEN case-set 和交付文件在
ValidationCandidate 冻结前纳入其身份；Git 多提交压缩在最后必需验证前完成，Seal 不
追加文件或产生新候选。详见 [VCS 边界](vcs-snapshots.md)。

## 开发后审查

冻结唯一 expected-set，在容量内并行推进 QA execution 与所有已选门；一项 FAIL 或
运行错误不停止兄弟任务，每项 source/usage/guard 结算后及时 refill。门读取基线到
最终候选完整 diff，不接收独立 QA 的用例正文或执行证据。

QA 按 approved whitelist、固定步骤和判据执行，准备→目标→采集消费相同 intent、CAS
与 guard；共同准备、依赖和 checkpoint 只按已批准合同复用。工具或环境变化先提交精确
invalidated checkpoint，再完成原动作未完成部分，未执行项不得 PASS。首次 QA review/
execution 为 FULL，后续按持久化 ReviewScopeMode 使用 AFFECTED；普通质量门始终 FULL。

所有目标终结、错误处置和逐 finding 裁决结束后，以 NEW/SAME_AS/DISMISSED 归并 canonical
problems，只创建一个 repair。RUNTIME_ERROR/无效结果沿同 TaskKey 有限重试；
RETRY_EXHAUSTED 进入对应 Ask，授权只增加当前精确动作的一轮或一次 attempt。

## 继承判定、修复授权与 Seal

普通返修固定只跑受影响 QA 子集 AFFECTED；需要继承的结果须精确 view receipt 证明。
有效原 FULL、本候选 AFFECTED 的实际执行与合法继承须完整覆盖当前批准的 QA、门和
开发来源；本轮必需义务全部有效通过后即可完成，不自动追加最终 FULL。
首次必要 FULL、新增或受影响义务、用例重开及原来源绑定仍须核验；缺失、失效或
未完成结果阻止交付。显式 waiver/skip 沿既有授权处置，不能作为执行 PASS。
门审 P2 修复以真实构建、测试和 carry 凭据保留门 PASS，不增加门复审轮；门审 P3 仅
Seal advisory。QA Review 可选 P2/P3 在 FROZEN 前按 suggestion ID 与理由处置。

三次连续 QA FAIL/长期不可用进入 qa-recovery Ask；可选 fresh review、重新设计、需求
变化、精确 skip/waiver 或 abort。额外 repair 绑定完整当前摘要、义务和授权，不能积累
未来授权或为兄弟任务增加额度。

promotion 固定同一候选 tree/blob/mode/ref 与需求/QA/验证绑定，核对等价 receipt。
终结先持久化可恢复 intent/summary，再清理全部登记 workspace/client/ref/changelist；
无残留 receipt 后提交终态与 Complete。清理中保持可恢复状态，不能提前宣称 SEALED。
正式 QA、Seal、主线集成和全局切换分别记录。分片见 [拆分与主线](sliced-runs.md)。

## 成本计量

source 就绪才用原生 transcript 结算同一 cost 投影；未就绪 Wait，明确不可用记
unavailable，不估算、不造第二账本。DSH、ZCode 按绑定的原生会话/完成记录解析实际
usage；不可计量 token 显示 null 并保留 reason，不能用零代替。guard 由当前 definition、route/topology、已确认
计划、expected TaskKey 与有限 retry policy 纯函数计算；host、主代理和用户不提交额度。
同一未完 dispatch 的继续与恢复用原会话累计值更新该条并按差额入总计，完成与重复提交
保持幂等；续次 source 不可用保留已计量前缀，并以 unavailable 标明当前来源资格。

Codex 原生 response ID 去重，任务墙钟包含工具等待、不含 turn 间空闲。owner 终态区间
差值只报告，不参与 in-flight guard；多 run 区间重叠或基线不可靠时不猜数字。child
cost 按 childRunID/dispatchID 汇总，保留主线与 child 原 owner 来源。

## 执行中的反馈与进度

`workflow show/status` 显示当前 worker 序号、语义进度和补充消息原文/投递状态。
`workflow message --worker <序号> --text <原文>` 只给已启动、尚未完成的同一 action
追加临时消息，不改变确认范围、TaskKey、Attempt 或额度。Codex 使用原 thread/turn 的
`turn/steer`；其他宿主排队到下一次合法的同身份调用，不接入第二个活动 writer。
PENDING、SENDING、ACCEPTED、FAILED 是原队列的投递事实；缺原生 ack 的 FAILED 保留
“投递结果未知”原因与原文，不表示宿主确定没有收到，也不自动重发。动作关闭、替换
或取消时，未投递消息明确结束并留证；任务 PASS 不能删除这些消息。

`workflow progress` 只读派生经过时间、真实运行/未启动/
受阻、当前准备/命令/原生等待/结果处理及区间、新结果、准备执行/复用和实际原生调用。
完成、失败和取消/替代分开；没有 pending 本身不证明完成。native-process 的调用数不是
计费模型请求数。缺时钟/用量源显示 unavailable，不填零或相加并行区间。`--machine`
保留完整原事实定位，普通显示不输出整份 workflow 材料。`--save-progress` 显式保存原
完整进度载荷并公布 `progress` 序号，后续 `--after <原序号>` 读取原 revision；
`--after 0` 选唯一载荷，多个则展示原位置，`--cursors` 只读列出原载荷。
原文件/API 的 `--after-file/--after-revision` 保留。设置见[用户与项目设置](user-configuration.md)。

## 原需求内调整计划与转交

`workflow replan --work <原序号> --work-disposition <CARRY|REDO> --work-reason <理由>
--changed-item <实际改变> --transitive-impact <影响> --carry-reason <保留理由>` 只准备当前未终结
regular 的计划重审：正式需求字节/revision 不变，product/topology 结论保留，逐项 work
仍由用户选择 carry/redo 并给理由。按各子命令帮助选择原 work/child 的序号和新的影响解释，不写固定 JSON 外壳；
程序保存完整 RequirementChangeInput，省略 classification/redo。原 `submit --confirmation-candidate` 确认后才重做
start-readiness；优化或增加 Batch 必须由新技审生成完整计划并经 route/plan 再确认。
不能通过自由任务正文绕过。普通相同需求的重复提交继续拒绝。

`workflow handoff --worker <开发者序号> --reason <用户理由>` 请求原 typed recovery Ask；
用户选择 fresh 后仍须观察、原 terminate receipt 与合法新执行边界。宿主不变，保留
原 TaskKey、工作区、已做工作、历史费用和原证据；主动转交不制造 transient failure，
不消费业务返修或故障 retry，但实际新用量和原 token guard 继续生效。UNKNOWN 仍先
对账，不能据请求盲目重开。确认与观察期间独立合法工作继续推进。

## 当前角色材料与 WorkBuddy

启动材料只含当前职责、约束、当前候选与必要语义输入。固定规则选择本角色章节，
只声明可用的工具操作；QA 执行只交待比较用例及其共享步骤、原操作事实。完整原需求
仍由原 immutable WorkerMaterial 绑定，通过现有 workspace 的 `requirement` 操作按需
读取原全文或行范围；不把正文重复灌入每次启动消息，不建索引或强制读取闸门。
原始结构化材料仍由引擎校验与接纳，模型不编写机械身份或直接改 workflow state。

WorkBuddy 用原 codebuddy/cbc print-mode CLI 和同一 engine MCP 工具。
安装提供 `.codebuddy/skills/formal-gates` 与 CODEBUDDY.md，不写 global hook。启动前
核原 CLI 能力与原用户 product config；续用先观察原 session store，再由 `--resume`
证明原身份。启动、等待、双流、取消、观察恢复、语义结果和费用沿共用原 owner。
CLI 会话的短 cwd 只承载原生存储，文件操作仍在已绑定的实际 workspace，取消与恢复
清理该原资源。用量只累计原 assistant UUID 的 input/output/cache-read，summary 不重复
相加；缺必需类别仍 unavailable。安装/本地 peer 通过不等于付费现场整流程通过，真实
调用须使用用户已授权的模型和额度。

## 语义结果与原生问答

各宿主向当前 worker 声明同一个 `submit_semantics` 工具。模型只提交当前角色的语义值、
枚举或原材料序号，SDK 传对象、程序编码规范结果；原 tool call、材料和宿主终态共同
证明接纳。最终普通消息不解析为 JSON 结果。同一次提交只允许精确同值重放，原任务
续用使用其当前 material 和执行实例；恢复观察原事实，不重新提交。原会话、轮次、
权限和费用继续由原 owner 持有。

原 read 的 `from/to` 选择完整行；可成对加 `fromColumn/toColumn`，表示首/末所选行的
1-based Unicode 字符列、两端包含，CR/LF 也计作原字符。`parts` 按顺序拼接这些原选择
与新创作分隔文字；程序保留原字节，不解析命令或裁剪空白。

`native show` 展示原问题，`native respond` 选择原问题/选项序号，使用
`--question`、`--choice`、`--response-question`、`--response-choice` 或新 `--response-text`；
表单以 `--form-field` 加 `--form-choice`/`--form-value` 输入新语义。程序默认保存完整
原候选并公布 `decision` 序号；获得真实确认后 `native respond --question <原问题序号>
--decision <原载荷序号>`，唯一载荷可用 `--confirm`。`--decisions` 只读列原问题候选。
候选和原输入属于原项目、run、native job 与 request，不选最新、不跨请求搬运。

已有原文由程序直接读取 stdin：`native respond --question <原序号> --prepare-text`
保存原输入问题的 bytes；表单同时选 `--form-field`，多输入问题选 `--response-question`。
随后 `--text-payload <原序号>` 读取同一原问题/字段的 bytes 准备候选，确认仍沿原
NativeRespond 接纳。`--texts` 只读列原文本。精确 `--save-decision/--decision-file`、
`--response-text-file/--form-value-file` 保留给人工或程序集成。

`workflow message --text` 输入新创作的语义转达；原文使用 `--worker <原序号>
--prepare-text` 从 stdin 保存原 bytes，不投递。`--texts` 列同一原 action/attempt 载荷，
`--text-payload <原序号>` 沿既有消息队列投递所选原文。`--text-payload 0` 可选唯一载荷，
多个则展示原位置。精确 `--text-file` 保留给人工或程序集成。没有原 chat API 的源证明时，
不能将模型转述声称为原文零复制。

`gate run --root <repo>` 发布当前包发现的门序号；`gate run --gate <原序号>` 选原门，
精确 gate ID 仅供人工或程序集成。脱离 run 的 `gate report --root <repo> --review <原序号>` 以具体 finding、证据和位置 flags
构造结果，例如 `--review-complete` 或 `--finding-severity`/`--finding-message`；原比较、
路径和总体状态由程序取得。gate run 由原 VCS provider Resolve 和 CompareWorkingTree
捕获实际 base、before/after 原内容、mode、未跟踪与删除文件，并直接发布
`[Native comparison]` 与 `locationFacts` 序号。正文行定位使用 after，删除项使用 before；
binary 以明确 base64 表达原 bytes。reviewer 不执行固定 VCS 命令或抄随机 context 路径；
原 context 文件仅作为同一结果接纳的身份材料保存。`--decision`、stdin typed JSON、精确 ID/path 接口保留给
人工或程序集成，不是模型编写模板的入口。完整参数见各命令 `--help`。
