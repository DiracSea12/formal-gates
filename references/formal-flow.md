# 正式流程公共合同

本文件供用户查阅公共入口和维护者核对程序行为；角色任务由程序签发材料限定，文档不授予手工编排权限。
流程顺序由 engine/CLI 持有，[SKILL.md](../SKILL.md) 只约束受理和用户决定。本文件持有当前 workflow 的调用形式、
受理收据字段和步骤级执行边界。使用已登记安装的固定 launcher；候选隔离验证必须绑定
实际候选副本，不能借用全局 binary 或可变源码目录。

## 公共入口

workflow 只有八个入口：

| 入口 | 职责 |
| --- | --- |
| `start` | 唯一 bootstrap 写入口，绑定已确认受理与当前安装身份。 |
| `drive` | 不携外部事件，执行确定性推进与恢复，返回下一个外部边界。 |
| `submit` | 唯一外部事件写入口，接纳用户决定、主动控制、worker 结果和原生 receipt。 |
| `show` | 只读完整 run 投影。 |
| `status` | 只读状态与可用操作。 |
| `next` | 只读下一边界及 freshness。 |
| `diagnose` | 原始只读诊断状态或终态摘要。 |
| `guard-preview` | 只读、非权威的生产 guard 计算，见 [guard 合同](guard-preview.md)。 |

安装、卸载、hook、lifecycle、canary 和脱离 run 的 gate 检查属于维护/诊断面，
不能推进业务或改写 workflow 状态。正常 loader 要求版本、definition/package 摘要和
installed-target 身份精确匹配；不匹配返回 `UNSUPPORTED_RUN_VERSION`。原始文件保留
供 diagnose，当前版本使用新 run ID，不提供兼容、迁移或第二 writer。

## 启动前受理

主代理先从仓库和已确认记录解决事实问题，只有影响范围、公开行为、验收或主要架构的
未决选择才询问。无待澄清问题时明确说明。regular 的完整需求与方案写入唯一正式
UTF-8 Markdown 文件；不要求用户原始输入采用固定模板。

文件保留自然语言正文，并使用唯一 `## 需求点` 章节。最小结构为：

```markdown
# 本次需求与方案

完整目标、边界与方案正文。

## 需求点

### 归档已完成任务

#### 要求

归档命令移动已完成任务，保留未完成任务。

#### 验收条件

- 已完成任务能从归档列表读取，未完成任务仍在当前列表。

#### 来源

用户明确要求。
```

需求标题与验收条目只写内容，不分配编号。程序按正式章节中的需求块和验收条目顺序
生成唯一 REQ/AC 身份并在 `requirementDocument` 投影中携带；用户或外部材料已提供的
REQ/AC 身份原样保留，生成编号跳过这些身份。原文件正文与精确 revision 不因分配编号
改变，需求与其验收条目的关联由原章节层级确定。直接字段顺序为要求→验收条件→来源；全部改变产品/流程行为或
需要 QA 保证的义务须进入需求点。纯实现位置、代码边界与未来迁移约束留在方案正文，
由技术审、diff 和实现质量门验证，不伪装为 QA 的 AC。

**次序固定为：结构预检 → 展示完整文件与精确 revision → 用户明确确认 → start。**
启动前预检使用现有 start 的只读模式：

```bash
formal-gates workflow start --precheck --root <repo> --requirement <requirement-file>
```

输出 `content` 全文、`revision` 精确内容摘要、`requirementDocument` 结构投影，以及
程序准备的 `intakeCandidate`。`materials` 含全部选定材料的规范化全文和各自 revision；
`--solution <path>` 选择独立方案文件，缺省使用正式文件本身；可重复 `--artifact <path>`
添加需确认的材料。路径、内容摘要、方案绑定与固定 transport 均由程序组装，无需手抄。
不创建 run 或确认 receipt，不读取或写入 admission/registry。可加 `--save-candidate <文件>`
由程序保存同一完整 `intakeCandidate`；保存候选不代表确认。材料选择 flags 仅供
precheck；正常 start 只接收已确认 receipt。预检只核结构、字段、唯一性与边界；失败先修同一文件，不请求确认。修改后重新预检、
展示和确认。确认前不启动开发 run、不选择后续路线、不派发后续代理。结构预检的通过
不是产品审、技术审或 QA 的 PASS。

### Typed intake receipt

`intakeCandidate` 只是待确认的材料绑定，不代表用户已确认。展示全部 `materials` 全文与
精确 revisions。预检的 `--save-candidate <文件>` 已由程序保存完整对象；等待用户明确
确认后，将原文件交给 `start --intake-receipt`。代理不抽取 JSON、不抄写字段、路径、
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
formal-gates workflow start --root <repo> --package-root <installed-package> \
  --run-id <id> --route regular --vcs <git|svn|p4> \
  [--base-snapshot <native-base-identity>] --intake-receipt <receipt.json>

formal-gates workflow start --root <repo> --package-root <installed-package> \
  --run-id <id> --route lightweight --intake-receipt <receipt.json>

formal-gates workflow drive --root <repo> --run-id <id>
formal-gates workflow show --root <repo> --run-id <id>
formal-gates workflow status --root <repo> --run-id <id>
formal-gates workflow next --root <repo> --run-id <id>
formal-gates workflow submit --root <repo> --run-id <id> < <request-choices.json>
formal-gates workflow next --root <repo> --run-id <id> --decision <choices.json> \
  --save-candidate <confirmation.json>
# 展示程序返回的当前确认内容，获得用户明确确认后：
formal-gates workflow submit --confirmation <confirmation.json>
formal-gates workflow diagnose --root <repo> --run-id <id>
formal-gates workflow diagnose --path <state-or-summary.json>
```

start/show/status/next/drive/submit 默认展示当前语义问题、短选择、必要确认材料及实际执行边界，
不输出内部派发材料或 adapter 指令。程序调用者需要完整原投影时显式使用 `--machine`；
`--save-candidate` 文件仍保存完整原对象，默认展示不改变其接纳绑定。precheck 的完整材料与 revision 输出不裁减。

regular 使用原生 VCS 基线；启动不声明 split 意向。唯一正式文件同时承载需求和方案时，
单一 artifact 的确认绑定需求保证，后续文件观测不自行改变其激活事实。未选 mode 时先取得用户选择，
lightweight 只留明确 unverified 记录，不开发、不快照、不做 QA/门验证。regular 不能
在开发中降为 lightweight。

`submit` 的 stdin 只接真实语义请求，`request` 选择当前控制类型；程序创建 REQUEST
Ask，不直接授权。决定使用 `next --decision --save-candidate`：`ask` 是 show 中当前
未决 Ask 的一基序号，`choice` 是真实用户选择，`reason` 只填写必要理由。程序保存完整
Event、原 request/token、当前 revision 和展示材料。默认只展示当前确认所需的语义内容，确认后 `submit --confirmation`
接纳原候选文件；陈旧候选沿原 freshness 检查拒绝，不能重新绑定到另一个 Ask。
完整确认文件提供原项目和 run 目标；显式提供 `--root` 或 `--run-id` 时只核对一致性。
同 Event 的精确重放由原接纳台账处理，不重放副作用。

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

产品/技术审各自有限语义轮；有效审查计轮，修订不清零。P0/P1 确认修订后完整新鲜复审；
P2/P3 逐项处置并修订，不因级别本身复审。审查修订先生成穷尽 review-delta，用户确认
变更包；meaning-preserved 保留适用 PASS，meaning-changed 明示失效、回退和容量后授权。
用户主动变化以 ImpactSet 逐项决定重走/续用，UNKNOWN 按受影响建议处理。

技术审形成 `granularityReview`：Batch→Subtask、依赖、接口、状态边界、DoD、验证、
恢复与交接。用户确认计划与精确 split 拓扑或 no-split 理由之后，选择 full/custom。
Batch 是现有任务的分组，不新增生命周期；一个 Batch 使用一个新开发会话，Subtask
不能独立派发。TaskKey、Attempt、语义审查轮与业务返修轮分别计数。

## 开发与快照

开发只消费确认任务；黑盒在基线隔离区并发设计/审查，不接收实现、白盒或开发解释。
设计失败沿原动作的有限 attempt/语义轮或 typed Ask 及时处理。黑盒未 PASS 不挡开发，
但挡最终候选。批内正常返回、重试和恢复沿用原身份，替换必须有恢复失败事实和签发。

开发者返回语义完成情况；程序捕获原生差异并校验范围与 postcondition，
adapter 执行 track→commit→snapshot。白盒独立
审查覆盖并编写/复用测试，只有测试变化推进快照。全部 FROZEN case-set 和交付文件在
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
