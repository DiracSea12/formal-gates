# 正式流程公共合同

流程顺序由 [SKILL.md](../SKILL.md) 持有。本文件唯一持有当前 workflow 的调用形式、
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

### REQ-001：归档已完成任务

#### 要求

归档命令移动已完成任务，保留未完成任务。

#### 验收条件

- AC-001：已完成任务能从归档列表读取，未完成任务仍在当前列表。

#### 来源

用户明确要求。
```

REQ/AC 身份唯一，直接字段顺序为要求→验收条件→来源；全部改变产品/流程行为或
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
不创建 run 或落盘 receipt，不读取或写入 admission/registry。材料选择 flags 仅供
precheck；正常 start 只接收已确认 receipt。预检只核结构、字段、唯一性与边界；失败先修同一文件，不请求确认。修改后重新预检、
展示和确认。确认前不启动开发 run、不选择后续路线、不派发后续代理。结构预检的通过
不是产品审、技术审或 QA 的 PASS。

### Typed intake receipt

`intakeCandidate` 只是待确认的材料绑定，不代表用户已确认。展示全部 `materials` 全文与
精确 revisions，等待用户明确确认之后，才将返回的 `intakeCandidate` 整体保存为 UTF-8
JSON receipt，并交给 `start --intake-receipt`。保存是机械抽取完整对象，不重新填写固定
字段、路径或摘要。确认 receipt 是受理材料，不是模型调用记录。

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
formal-gates workflow submit --root <repo> --run-id <id> < <typed-event.json>
formal-gates workflow diagnose --root <repo> --run-id <id>
formal-gates workflow diagnose --path <state-or-summary.json>
```

regular 使用原生 VCS 基线；启动不声明 split 意向。唯一正式文件同时承载需求和方案时，
单一 artifact 的确认绑定需求保证，后续文件观测不自行改变其激活事实。未选 mode 时先取得用户选择，
lightweight 只留明确 unverified 记录，不开发、不快照、不做 QA/门验证。regular 不能
在开发中降为 lightweight。

`submit` 的输入是当前协议 schema 的 typed Event，事件身份与 Kind 对应的 payload
成对匹配。主动控制先创建 REQUEST Ask，再从 show/next 取当前 request ID、选项、
freshness token 和绑定 digest，经 DECIDE 登记用户实际决定；不能用自由 USER 写事件。
同 event ID 同 digest 重放返回稳定接纳，不重放副作用；同 ID 不同 digest 拒绝。

reset、abort、adopt-external、路线调整、精确 waiver/skip 和 HUMAN 合法节点跳转均走
当前 `availableActions` 与 typed Ask。跳转先处理受影响在途工作、结果、候选和副作用，
终止目标统一 cleanup。Operator 核实事实与建议，不替用户授权。活动 run 的内容修订
先冻结受影响推进，确认完整新 revision 后逐项决定重走/续用；不猜未确认影响。

## 原生运行时派发

Next 只返回 Ready、HostAction、Ask、Wait、Operator、Complete 六类边界。主代理原样、
整批搬运已签发 action，不手造 TaskKey、Attempt、wave、额度或独立任务。drive 执行
程序拥有的机械动作和原生适配器调用；宿主只负责原生执行、观测、恢复和证据传输。

外部原生执行结果通过同一 submit 接纳路径提交：

```bash
formal-gates workflow submit --root <repo> --run-id <id> \
  --worker-material <issued-material-reference.json> \
  --native-directory <original-evidence-directory> \
  --native-source <original-native-source.json> --event-id <result-event-id>
```

使用 engine 签发的材料引用与原始证据，不能把模型自述、合成 transcript 或其他宿主
证据当作完成记录。spawn、result、lifecycle 与 HostAction receipt 绑定同一 action、
provider 和 Attempt。UNKNOWN 先 observe/reconcile；唯一匹配 attach，不能盲目重派。
正常执行和恢复共用任务提交与业务接纳路径，恢复只找执行事实。

## 开发之前

先独立产品审，再独立技术审；fresh review 读取完整最新确认文档。raw candidate、
Operator 逐项核实、用户 finding/remedy 决定与 effective status 分层保存。请求用户
处置前，逐项展示问题、正常路径错误、后果、建议、ID、等级及定级理由；不完整展示
不能作为授权。严重度和项目范围唯一以 [审查契约](../prompts/reviewer-base.md) 为准。

产品/技术审各自有限语义轮；有效审查计轮，修订不清零。P0/P1 确认修订后完整新鲜复审；
P2/P3 逐项处置并修订，不因级别本身复审。审查修订先生成穷尽 review-delta，用户确认
变更包；meaning-preserved 保留适用 PASS，meaning-changed 明示失效、回退和容量后授权。
用户主动变化以 ImpactSet 逐项决定重走/续用，UNKNOWN 按受影响建议处理。

技术审形成 `granularity_review`：Batch→Subtask、依赖、接口、状态边界、DoD、验证、
恢复与交接。用户确认计划与精确 split 拓扑或 no-split 理由之后，选择 full/custom。
Batch 是现有任务的分组，不新增生命周期；一个 Batch 使用一个新开发会话，Subtask
不能独立派发。TaskKey、Attempt、语义审查轮与业务返修轮分别计数。

## 开发与快照

开发只消费确认任务；黑盒在基线隔离区并发设计/审查，不接收实现、白盒或开发解释。
设计失败沿原动作的有限 attempt/语义轮或 typed Ask 及时处理。黑盒未 PASS 不挡开发，
但挡最终候选。批内正常返回、重试和恢复沿用原身份，替换必须有恢复失败事实和签发。

开发者返回声明式交付路径 manifest、语义说明和 typed result。adapter 校验范围与
postcondition 后执行 track→commit→snapshot；代理不自行决定 VCS 写顺序。白盒独立
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
开启 requirement guarantee 的最终候选 Seal 前须 FULL；必需 QA/门全部有效 PASS
（含合法继承）后才可 Seal。
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
unavailable，不估算、不造第二账本。guard 由当前 definition、route/topology、已确认
计划、expected TaskKey 与有限 retry policy 纯函数计算；host、主代理和用户不提交额度。

Codex 原生 response ID 去重，任务墙钟包含工具等待、不含 turn 间空闲。owner 终态区间
差值只报告，不参与 in-flight guard；多 run 区间重叠或基线不可靠时不猜数字。child
cost 按 childRunID/dispatchID 汇总，保留主线与 child 原 owner 来源。

### 拆分计划确认的程序预览

在唯一的 `CONFIRM_TOPOLOGY` 拆分决定提交前，用 `workflow next --root <项目> --run-id <run> --review-decision <JSON文件或->` 预览同一 `ReviewDecisionPayload`。草稿提供拓扑或路线的语义选择；程序返回完整计划、需求/方案与拓扑/路线绑定的确认摘要及装配后的决定载荷。此读取不修改状态，显式错误的确认值会被拒绝。展示精确计划并取得该决定的确认后，将返回载荷用于原 `workflow submit`；不得把 readiness PASS 当作执行批准。普通路线沿同一入口预览；child 的内联提案仍使用原确认契约。
