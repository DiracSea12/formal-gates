# 需求对齐

日期：2026-07-17
状态：本文件只保留已确认且当前有效的条目；撤回、取代和决定不做的方案不保留正文。

本文档保存了对本地 diff、开发计划以及曾报告 PASS 的四门证据进行严格审查后达成的决策。它是本次 OpenSpec 变更的需求来源。

## Phase 0 - 审查收敛

用户已确认先完成审查收敛，再开发机器证据功能。只有当前变更造成，并有证据证明其违反已确认需求、可观察行为、现有门的职责或强制规则的问题，才能影响结论。范围内的真实缺陷直接返修；最小修法会改变已批准范围时交给用户决定，不得自动扩张。措辞、命名、格式、等价方案偏好、纯假设风险和未要求的加固只是建议；只剩建议必须 PASS。

Reviewer 的 finding 不直接变成需求或返修任务。主代理先去掉无证据、重复、偏好型和超范围意见；同一根因换一种说法仍算一个问题。只有确实会改变范围、验收、架构边界、公开行为或其他用户决定的问题才进入需求澄清。一次交付中，每道门最多自动完成三轮 review-repair，各门单独计数。只有独立 reviewer 返回完整正式结果后才开始一轮；主代理处理完该结果、完成已接受的范围内返修并做完必要复验后，才记为一轮。一次正式结果及其返修、复验合计一轮，不能拆开重复计数。开发自检修改、派发失败、执行中断以及没有完整正式结果的尝试都不计轮次。某道门完成三轮后，停止该门的自动审查和返修，只提交合并后的真实 blocker、证据、已尝试修法和仍未解决的原因；由用户决定修改范围或需求、延期或接受风险、明确批准该门再开一轮，或者停止交付。本阶段只修改现有规则、agent、reference、行为用例和阶段说明，不新增产品代码、字段、命令、角色、状态机或文件。

## RQ-001 - 最终门禁组成

Requirement or question: 最终交付能否组合新执行和继承的门禁结果？
Source: 用户于 2026-07-10 确认的对齐结果。
Why it matters: 每次小修复后都重新运行四门模型审查会浪费 token。
Status: confirmed
User answer: 最终交付可以组合 `FRESH_PASS` 和 `CARRIED_PASS`，报告必须逐门区分二者。
Downstream effect: 最终准入需要逐门记录类型化状态，不能把所有 PASS 记录都当作等价结果。
Document impact: 继承与最终定稿规范。
Evidence needed: 与目标 snapshot 绑定的最终门禁矩阵，以及每一门的源产物。

## RQ-002 - 独立的继承决策

Requirement or question: 修复后由谁判断此前的 PASS 是否仍然有效？
Source: 用户于 2026-07-10 确认的对齐结果。
Why it matters: 主代理选择了修复内容，不能再批准自己的重跑范围。
Status: confirmed
User answer: 主代理可以提议重跑边界，但任何继承的 PASS 都必须由新的零对话上下文 Carry-Forward Arbiter 裁决。复杂返修链由 CLI 在 reviewer 上下文之外完整校验；Arbiter 只审从源 snapshot 到目标 snapshot 的累计 diff 是否影响拟继承 gate。
Downstream effect: 不能仅凭主代理的 transition 记录推进继承。
Document impact: 继承与最终定稿规范。
Evidence needed: Arbiter 产物、匹配的 receipt、完整展开的 transition 链和逐门决策。

## RQ-003 - Reviewer 独立运行的 receipt 证据

Requirement or question: 如何证明正式 reviewer 或 Arbiter 确实独立运行过？
Source: 用户于 2026-07-10 确认的对齐结果。
Why it matters: 主代理自报的 reviewer ID 或非空 dispatch ID 不能证明独立 reviewer 实际运行并产出对应结果。
Status: confirmed
User answer: 正式四门 reviewer 和 Carry-Forward Arbiter 必须复用现有 receipt 链，绑定 dispatch registration、subagent start/stop、subagent ID、输出哈希、workflow、gate、stage 和 snapshot；自报身份不能支持 PASS。普通 CLI receipt 只证明这些记录相互一致，不声称能抵御控制本地文件和 CLI 的操作者。Host hook 只有通过 same-host live canary 后，才能额外声称生命周期由宿主自动捕获。
Downstream effect: 缺少或不匹配的 receipt 会阻断正式 PASS；不新增 trust root、provider、verifier 或不可伪造认证系统，也不限制普通 CLI 的正式 PASS。
Document impact: Reviewer 证明与隔离规范。
Evidence needed: receipt 正向用例，以及 dispatch、生命周期、reviewer ID、输出、workflow、gate、stage 或 snapshot 不匹配的拒绝用例；host 自动捕获声明需要 live canary。

## RQ-004 - 完整的证据哈希闭包

Requirement or question: PASS 之后要保护哪些证据？
Source: 用户于 2026-07-10 确认的对齐结果。
Why it matters: 只哈希顶层报告，会让它引用的 alignment、decision、context 和 verification 文件仍可被修改。
Status: confirmed
User answer: 每个正式证据依赖都属于递归验证的哈希闭包。模型只读取相关证据；机器哈希不能引发额外的模型审查。
Downstream effect: 任何被引用文件发生变化，都会使依赖它的 PASS 失效。
Document impact: 证据闭包与路径安全规范。
Evidence needed: 确定性的递归引用验证和篡改用例，以及 RQ-017 已确认的逐门 manifest 与哈希契约测试。

## RQ-005 - 仅以结构化需求为真值

Requirement or question: 是否继续兼容旧版 Markdown alignment 解析器？
Source: 用户于 2026-07-10 确认的对齐结果。
Why it matters: 全局标签匹配无法证明每个 RQ 条目都有合法字段、状态和跨轮次身份。
Status: confirmed
User answer: 逐条结构化的 alignment 和 decision 数据是唯一机器真值。删除旧版 Markdown 解析器，不提供兼容或迁移路径；旧 workflow 必须重启。
Downstream effect: 需求 PASS 会验证每个条目，并直接拒绝旧版产物。
Document impact: 需求澄清 PASS 等价性与结构化证据规范。
Evidence needed: 逐条的正反向 fixture、多条目畸形用例，以及旧产物的重启行为。

## RQ-006 - workflow run 内的证据路径

Requirement or question: 正式 PASS 证据可以存放在哪里？
Source: 用户于 2026-07-10 确认的对齐结果。
Why it matters: 任意 URI、绝对路径、仓库目标和平台相关的路径处理可能逃逸或混淆证明边界。
Status: confirmed
User answer: 正式证据必须保存在 workflow run 目录内，使用规范化相对路径，在受支持的平台上安全解析，并绑定哈希。仓库目标和 URI 不能直接证明 PASS。
Downstream effect: 拒绝路径逃逸、符号链接逃逸、跨 run 复用、绝对路径和 URI 证据。
Document impact: 证据闭包与路径安全规范。
Evidence needed: 跨平台路径测试，以及 run 目录内的正反向 fixture。

## RQ-007 - 唯一的可执行 policy 来源

Requirement or question: 如何让 `policy show` 与实际验证保持一致？
Source: 用户于 2026-07-10 确认的对齐结果。
Why it matters: 比较字段数量或关键词，不能证明展示的 policy 与可执行行为一致。
Status: confirmed
User answer: 由一份类型化 Go policy 定义同时驱动验证和 `policy show`。动态规则有稳定的 rule ID，以及正向和反向行为测试。
Downstream effect: Policy 导出是 admission 和 artifact validation 共用的同一类型化定义的投影。
Document impact: Policy 基线规范。
Evidence needed: 按 rule ID 覆盖每条已导出动态规则的接受和拒绝行为。

## RQ-009 - 非产品进度记录不增加产品机制

Requirement or question: 非权威的一次性进度记录是否需要生命周期系统或正式门禁绑定？
Source: 用户于 2026-07-10 确认的对齐结果。
Why it matters: 为非产品进度记录构建生命周期机制会增加永久复杂度。
Status: confirmed
User answer: 不为非权威进度记录增加生命周期、兼容、snapshot、准入或完成机制。真正有用的交付步骤直接写进各自负责的 proposal、design 或 tasks；来源记录本身不进入产品合同。
Downstream effect: 产品只保留有实际行为价值的阶段、验证和四门聚合要求，不增加进度记录专用机制。
Document impact: proposal、design 和 tasks 只保留实际交付要求。
Evidence needed: 产品文档和实现中不存在非权威进度记录专用机制。

## RQ-010 - reviewer 落盘材料隔离

Requirement or question: 如何隔离敏感流程文本文件，同时不向 gate reviewer 隐藏正常的当前项目信息？
Source: 用户于 2026-07-10 确认的对齐结果。
Why it matters: 之前的重跑 bundle 写出了修复过的 blocker，给 reviewer 造成偏置；而严格的文件白名单会降低审查完整性。
Status: confirmed
User answer: 所有审查相关落盘材料一律放在固定黑名单路径 `.gates/runs/<workflow-id>/restricted/` 下，没有例外。包括当前和旧的 dispatch 副本、context bundle 或 manifest、reviewer 输出、QA 结果、receipt、生命周期事件、closure、状态、报告、日志、统计、验证记录、返修记录、Carry/Arbiter 材料和主代理总结。给独立 reviewer 的实质内容只能是三类：已确认的当前需求、reviewer 当前角色、当前 diff 或开发前拟议变更。worktree、基准版本、输出位置和输出格式只能用于执行和返回结果，不得夹带结论、证据摘要或审查方向。Reviewer 直接读取当前仓库中的需求和变更文件并自行运行所需检查；按照 RQ-067，它在 run 目录内唯一可读写的例外是 CLI 生成的分配模板，而且只能改语义槽位，不能打开其中引用的机器证据。主代理和 CLI 在 reviewer 上下文之外核对前置门、状态、receipt 和其他机器证据。不得复制需求、项目规则或仓库文档到 run 目录供 reviewer 阅读，不得把前门 PASS、测试汇总、旧结论、返修链或任何其他 workflow-run 产物交给 reviewer。若现有实现要求在 `restricted/` 外保存审查材料，流程必须停止并修正实现，不能临时放宽。
Downstream effect: `SKILL.md` 是随插件分发的唯一权威规则；不能依赖目标项目不会携带的 `AGENTS.md` 或 `CLAUDE.md`。四门及其他独立 reviewer 只能读取分配的 CLI 模板并写入语义结果，不得读取任何其他 `.gates/runs/**` 产物。当前要求 reviewer 消费 bundle、前门 closure、验证汇总或其他 run-local 文件的 schema、policy、agent、reference、CLI 和测试口径必须同步删除或改为 reviewer 外部的机械处理。
Document impact: Reviewer 证明与隔离规范。
Evidence needed: 所有审查落盘路径的 `restricted/` 正反测试；只含当前需求、角色和 diff 的派发正例；包含任一 workflow-run 产物、前门结果、验证摘要、复制规则或其他附加材料的拒绝用例；以及 reviewer 不读取机器证据而主代理/CLI 仍能完成前置校验和记录的流程用例。

## RQ-012 - 可交付 snapshot 与证据闭包

Requirement or question: 写入 gate 报告是否应改变可交付 snapshot？
Source: 用户于 2026-07-10 确认的对齐结果。
Why it matters: 把生成的 gate 产物计入 `changeSnapshot` 会造成无限重跑循环；排除它们却没有其他完整性根，则会允许篡改。
Status: confirmed
User answer: `changeSnapshot` 覆盖可交付源码、测试、配置、需求和项目文档。Gate 报告、receipt、dispatch record 和 run log 不计入其中，而由独立的证据完整性标识保护。证据变化会使依赖它的 PASS 失效，但不会生成新的可交付 snapshot。具体标识使用 RQ-017 已确认的逐门独立证据闭包。
Downstream effect: 可交付内容变化使用继承或重跑逻辑；证据变化使用闭包验证，并在同一可交付 snapshot 上重新记录。
Document impact: 证据闭包、snapshot 和最终定稿规范。
Evidence needed: Snapshot 纳入/排除测试和证据篡改测试。

## 必须落实的现有规则

以下不是新需求。它们原本就是规范要求，但当前 workflow 没有真正强制执行：

- 正式 QA case 必须在实现前根据需求设计，经过独立 Design Review，并以哈希形式绑定到 development handoff。
- 开发者可以增加 case，但不能删除或弱化已批准的 case。
- 如果缺少已批准的 case set、Design Review、QA 自有的执行证据和 case 到 evidence 的绑定，QA Execution 不能记录 PASS。
- 正式 gate prompt 不得包含此前的 finding、repair explanation、target conclusion 或 directed focus。验证覆盖 reviewer 可见的全部输入，不能只检查顶层 dispatch prompt。
- 发给 reviewer 的最终全文必须由现有 CLI 从 gate/stage、需求/diff 目标、worktree、snapshot、输出路径、policy 和 context-bundle 参数直接生成；`prompt prepare` 不接受调用方手写的七字段模板。`receipt register` 是发送前唯一一次全量静态检查，同时生成包含全部静态字段、policy/check 目录和 evidence binding 的 reviewer/Carry 目录及不可变 proof。Reviewer 不再确认或复述任何静态字段，只通过 `receipt submit` 提交语义状态、理由、findings 和 locations；Carry 只通过该命令提交逐门决定和理由。发送后不能手工追加。submit 必须在落盘前拒绝未知、重复、缺失、类型错误或非法值并记录 artifact hash/proof；finalize 必须要求该 proof，机械聚合 verdict，再写最终正式 JSON、receipt 并锁定 dispatch。静态 prompt 结构只由 CLI 检查，语义 anti-anchor 判断仍由 reviewer 负责。现有 receipt registration 同时强制同一 workflow、gate、stage 最多三次已完成 review，第四次只有用户明确批准才能派发。

上面的 prompt 规则与 RQ-010 相互独立。RQ-010 的路径隔离决策没有批准改变直接 dispatch prompt 的验证方式。

## RQ-014 - 早于 QA Design 的实现如何恢复

Requirement or question: 当前部分实现已经早于独立的实现前 QA Design 存在，如何让它进入正式 workflow？
Source: 由独立的零对话上下文文档审查于 2026-07-10 提出。
Why it matters: 假装新 case 早于现有代码会伪造时间顺序；删除并重写所有可复用代码，又会浪费工作且不改善行为。
Status: confirmed
User answer: 在声明四门意图时分类。如果在写代码前声明，使用正常的实现前 QA Design 流程。如果在代码已存在后声明，则启动新的正式 workflow，把现有代码视为未接受的候选实现，先进行盲态 QA Design 和独立 Design Review，再由正式 development worker 采用、修改或删除候选代码。有充分理由采用时不要求重写，也不继承旧 workflow 的 gate 或 task 完成状态。
Downstream effect: 正常的后续工作保留实现前 QA；实现后才提出四门要求时，通过干净重启 workflow 恢复，既不伪造时间顺序，也不浪费可复用代码。
Document impact: QA design admission、迁移顺序以及首个实现阶段的 QA/开发交接任务。
Evidence needed: 经过证明的盲态 case design 时间顺序，随后是正式 worker 的采用/返工记录和当前 snapshot 验证。

## 已撤回的 ID

`RQ-015` 经用户批准撤回，因为它与 RQ-010 重复。其 ID 继续保留，不得再次使用。Reviewer 直接读取当前仓库中的已批准需求，不读取 run-local 副本。

## RQ-016 - Reviewer 新鲜度与禁读路径

Requirement or question: 如何复用现有 receipt 判断 reviewer 属于本次审查，并检查其正式输入没有读取 `restricted/` 路径？
Source: 由独立的零对话上下文文档审查于 2026-07-10 提出。
Why it matters: 仅有 reviewer 自报不能把结果绑定到本次 workflow、gate、snapshot 和实际输出，也不能检查正式输入是否引用了禁读材料。
Status: confirmed
User answer: 补强现有 receipt，不另起一套零上下文机制。保留现有 dispatch registration、subagent start/stop、subagent ID 和 reviewer 产物哈希链；正式四门 PASS 必须提供与当前 workflow、role、gate、stage、snapshot、最终发送 prompt、reviewer session 和输出一致的 receipt。generation-only 模板不属于正式证据，reviewer/Carry JSON 也不反向引用 prompt。按照 RQ-010 的后续决定，prompt、receipt、事件、reviewer 输出和它们依赖的其他审查落盘材料全部保存在当前 run 的 `restricted/` 下，由主代理和 CLI 机械读取，不能作为 reviewer 输入。Reviewer 的实际提示只包含当前需求、角色和 diff；普通 CLI 不声称知道 reviewer 是否在提示之外读取了文件。Host hook 只有在 live canary 成功时，才能额外声称生命周期或文件访问由宿主自动捕获。
Downstream effect: 复用并收紧现有 receipt 和路径校验，把“CLI 可读的黑名单内机器证据”与“reviewer 只能看到的需求、角色、diff”分开；能力声明继续区分普通 CLI 一致性检查与经过 live canary 证明的 host 自动捕获，不新增 verifier、provider、trust root 或 nonce 机制。
Document impact: receipt 校验、隔离规则、host 能力声明和 canary。
Evidence needed: 黑名单内 receipt 正向用例；reviewer 提示不包含任何 run-local 路径；workflow、gate、stage、snapshot、session、输出或 restricted 布局不匹配的失败测试；host 自动捕获声明必须绑定成功的 live canary。

## RQ-017 - 证据闭包图与哈希契约

Requirement or question: 应以什么样的逐记录与最终汇总闭包模型、图边、canonical encoding 和 cycle behavior 作为规范？
Source: 由独立的零对话上下文文档审查于 2026-07-10 提出。
Why it matters: 除非各实现就哪些字段建立边，以及如何在各平台上得到同一图哈希达成一致，否则“所有传递证据”这个说法并不充分。
Status: confirmed
User answer: 使用最小的逐门证据闭包，不增加额外的最终汇总根。每条正式 PASS 的 closure manifest 包含 reviewer 输出、receipt 和该门实际使用的全部下层证据，gate record 只绑定一个 closure root。Receipt 在 closure 内引用 reviewer 输出哈希；reviewer 输出不反向引用 receipt 或 closure，因此没有自引用。后续 gate 准入和最终交付时逐门重新验证。只有结构化证据字段中的显式引用和 gate 记录要求的 receipt 形成依赖边，不从 Markdown 文本或关键词中猜测引用。每个引用必须是 workflow run 内的规范化相对路径并带哈希；缺失、篡改、路径逃逸、冲突别名或引用环都拒绝。闭包 manifest 使用版本化的 typed Go 结构和固定排序生成确定性 JSON，以 golden vector 锁定跨平台结果。
Downstream effect: 修改任一已使用证据都会使对应门的 PASS 失效，但后续门新增自己的证据不会改变此前门的闭包。最终交付逐门验证四门记录、Arbiter 证据和最终验证引用，不再维护第二层汇总根。
Document impact: 证据闭包/路径安全规范、结构化证据 schema 和 policy rule。
Evidence needed: 单一逐门 closure root；receipt 对 reviewer 输出哈希的单向引用；多层引用、PASS 后篡改、缺失节点、路径逃逸、别名冲突、引用环、后续门不影响此前闭包，以及跨平台 deterministic manifest 的正反向 golden vector。

## RQ-018 - 每确认一个澄清项就立即落盘

Requirement or question: 已确认的需求澄清答案必须在什么时候写入 alignment record？
Source: 用户于 2026-07-10 确认的对齐结果。
Why it matters: 等整轮访谈结束后再写，会让已确认决策只存在于聊天中，更容易因中断而遗漏或被错误重建。
Status: confirmed
User answer: 用户每确认、延期或将一个 RQ 条目标记为 out of scope 后，必须先在现有 alignment record 中更新该条目，再询问下一个问题。不要为每个问题创建一个文件，也不要等整轮结束后再写。该行为只需通过 requirements clarification 的 reference 或 agent 文档落地，不新增 Go 状态机、hook 或宿主级时序拦截。
Downstream effect: 后续每次澄清和文档编辑都读取最新的已落盘决策状态，不依赖聊天记忆；本项不扩大 CLI 或宿主编排边界。
Document impact: `references/requirements-clarification-gate.md` 或对应 agent 文档；不创建独立交互 capability 或专门测试任务。
Evidence needed: 文档审查确认 reference 或 agent 指令明确要求先更新当前 alignment record，再询问下一题；不要求行为 fixture 或运行时强制证明真实时序。

## RQ-020 - 正式 PASS 的统一 JSON 机器字段

Requirement or question: 是否要求所有正式 PASS 的机器校验字段统一使用 JSON？
Source: 由 `predev-review-20260710` complexity 开发前审查提出，并由用户于 2026-07-11 确认。
Why it matters: 只在部分角色使用 JSON，会继续保留多套 Markdown/JSON 解析与校验路径；但全量迁移属于破坏性格式变更，需要明确需求授权。
Status: confirmed
User answer: 所有正式 PASS 的机器校验字段统一使用类型化 JSON。人类阅读的结论和解释可以继续使用 Markdown，但 Markdown 不能满足、补全或覆盖机器要求。复用现有 `FormalGateEvidence` 方向，为不同 artifact role 使用共同 envelope 和角色专用 payload；requirements、四门 reviewer、Carry-Forward Arbiter 和 mechanical FinalExecution 都走同一结构化证据体系。旧格式不兼容、不迁移，旧 workflow 重启。
Downstream effect: `structured-json-evidence` 的全角色迁移获得明确需求来源，但实现和 tasks 必须覆盖每个角色的字段、校验、正反例以及旧格式拒绝，不能只写一个泛化迁移任务。
Document impact: 结构化 JSON evidence spec、role schema、policy、artifact validation、examples 和 canary tasks。
Evidence needed: 每种 artifact role 的合法 fixture、缺字段/错角色/Markdown-only/旧 schema 失败用例，以及现有 policy rule 的行为等价性测试。

## RQ-021 - 需求澄清交互规则的正式范围

Requirement or question: `requirements-clarification-interaction` 应只保留逐题落盘，还是把现有的提问、事实查询和复用规则也作为正式需求？
Source: 由 `predev-review-20260710` complexity 开发前审查于 2026-07-11 提出。
Why it matters: RQ-018 只明确了逐题落盘，而当前 capability 还规定先查仓库事实、一次问一个高影响问题、给建议答案、保留稳定 ID、复用已确认 alignment、区分非正式澄清与正式 PASS，以及长期记忆规则；这些内容目前缺少统一的需求来源和 task 覆盖。
Status: confirmed
User answer: 逐题落盘、先查仓库事实、一次问一个高影响问题并给出建议和理由、保留稳定 RQ ID 与状态、复用已确认 alignment、区分普通澄清与正式 PASS，以及长期记忆非权威等交互规则，只放在 requirements clarification 的 reference 或 agent 文档中。不为这些交互规则新增独立 capability、产品代码、宿主拦截或专门的自动化测试任务。已经由其他正式需求负责的结构化 RQ 字段、状态校验和 PASS 证据规则继续由原有 spec 与实现任务负责，不重复定义。
Downstream effect: 删除 `requirements-clarification-interaction` 独立 capability，并从 tasks 和重复测试 capability 中移除为这些交互指导新增产品实现或专门测试的要求；保留 reference/agent 文档中的简洁操作说明。
Document impact: requirements clarification reference/agent 和 Phase 1 的文档一致性任务。
Evidence needed: 文档审查确认 reference/agent 说明存在、没有独立产品实现任务，并确认已有正式规则仍由各自的权威 spec 所有。

## RQ-023 - Review finding 先筛选再决定是否澄清

Requirement or question: 开发前审查或其他 reviewer 提出的 finding，什么时候需要向用户澄清，什么时候可以作为范围内缺陷直接返修？
Source: 用户于 2026-07-11 要求 review 问题也属于需求澄清范围；2026-07-12 又确认 Phase 0 必须先过滤吹毛求疵、重复和超范围意见。
Why it matters: Reviewer finding 只是独立意见，不会自动成为用户需求或返修任务；但主代理也不能借“实现细节”擅自增加机制、改变边界或替用户取舍。
Status: confirmed
User answer: 主代理先核实 finding 是否由当前变更造成、是否违反已确认需求或可观察行为、是否有具体证据，并合并同一根因。无证据、措辞/命名/格式、等价方案偏好、未要求的加固和纯假设风险不进入澄清，也不能阻断；已确认范围内的真实缺陷可以直接返修。只有解决方案会改变范围、验收、架构边界、公开行为或其他用户决定时，才创建稳定 RQ ID，向用户说明问题、影响、最小修法和建议；确认一个立即落盘一个，再修改正式需求或代码。
Downstream effect: 需求澄清只接收真正需要用户决定的问题；主代理不得把 reviewer 偏好升级成需求，也不得绕过用户处理范围变化。
Document impact: requirements clarification reference/agent 指令、主代理 finding 过滤和开发前审查返工顺序。
Evidence needed: 行为用例同时证明建议型 finding 不阻断、范围内真实缺陷进入返修、范围变化先逐项确认并落盘。

## RQ-025 - 最终四门不依赖非权威进度状态

Requirement or question: 最终四门的 snapshot、独立执行和最终聚合是否应依赖非权威进度状态？
Source: 针对 `files.3cbe97e3e99c` 的零对话上下文 complexity 开发前审查提出。
Why it matters: 如果最终四门依赖进度勾选，非权威状态会错误地改变 snapshot 或制造循环顺序。
Status: confirmed
User answer: 不依赖。每个实现阶段在完成实际可交付内容后固定 snapshot，四门对同一 snapshot 独立运行并可按任意完成顺序记录，全部 target-bound 结果齐备后再执行 finalization；非权威进度状态不构成前置条件或证据。
Downstream effect: 四门独立准入和最终聚合只由可交付 snapshot、验证和 gate 证据决定。
Document impact: proposal、design 和每阶段完成规则。
Evidence needed: 四门与 finalization 不读取或要求非权威进度状态。

## RQ-026 - JSON 格式层与领域校验的所有权

Requirement or question: `structured-json-evidence` 是否只负责 envelope、角色分发和格式拒绝，把 requirements、policy、closure、receipt、carry 与 finalization 语义交给各自 capability？
Source: 针对 `files.45473e1a6d81` 的零对话上下文 complexity 开发前审查提出。
Why it matters: JSON capability 若重复实现每个领域规则，会与“每个 concern 一个权威 owner”冲突。
Status: confirmed
User answer: 各司其职。`structured-json-evidence` 只负责 envelope 和 role payload 的结构解析、格式拒绝与类型分发；requirements、policy、closure、receipt、carry 和 finalization 等领域语义必须由各自唯一的 domain owner 校验，JSON 层禁止重复实现。
Downstream effect: decoder 只产生类型化结构并分发，领域 validator 返回校验结果，workflow coordinator 再决定是否写状态；不新增重复规则或万能校验层。
Document impact: structured JSON spec、design 和 Phase 1 的 decoder/domain validation tasks。
Evidence needed: 格式层和各领域 validator 的单向调用与无重复规则检查。

## RQ-027 - Reviewer receipt 的实际信任边界

Requirement or question: Reviewer receipt 应追求抵御本地控制者的不可伪造认证，还是在所有 CLI 可用的前提下校验现有审查生命周期和证据一致性？
Source: 针对 `files.ca7f5e44bd40` 的零对话上下文 architecture 开发前审查提出。
Why it matters: 本地可写 receipt 不能自证可信；但生产 trust root、认证通道和 provider 支持会显著扩大集成范围。
Status: confirmed
User answer: 不新增 verifier、provider 框架或外部认证系统。复用现有 receipt 链校验 dispatch、子代理开始/结束、子代理 ID、artifact hash、workflow、gate、stage 和 snapshot 的一致性；所有能运行 CLI 的环境都可以正式 PASS。有 host hook 时可额外证明生命周期事件由宿主自动捕获；无 hook 时仍可用，但不得声称能抵御拥有本地文件和 CLI 写权限的恶意操作者。本项目只保证按文档正常使用和常见误操作下的正确性；除非用户明确要求安全加固，不负责防御成套证据篡改、恶意本地修改、手工改写内部状态或必须先违反正常流程才能构造的输入，这类场景不得阻断 PASS 或触发开发。
Downstream effect: 删除注入式 `AttestationVerifier`、`UNSUPPORTED_HOST_RECEIPT`、生产 trust root、一次性 nonce 和不支持 host 只能 advisory 的设计；直接收紧现有 receipt 校验和诚实的能力声明。
Document impact: reviewer receipt/isolation spec、proposal、design、structured JSON spec 和 Phase 2 receipt/host-capability tasks。
Evidence needed: 所有 CLI 的现有 receipt 正向流程，以及正常使用中可能出现的 provider-required lifecycle、ID、hash、workflow/gate/stage/snapshot 不匹配；hook 能力只在 live canary 成功时声称。成套恶意篡改和手工改写内部状态不属于验收范围。

## RQ-028 - 模块与依赖方向

Requirement or question: 是否保持现有 `internal/validate`/`internal/cli` 包边界，只规定 CLI -> workflow coordinator -> decoder/policy/domain validators -> existing state writer 的单向调用，而不拆新 package？
Source: 针对 `files.ca7f5e44bd40` 的零对话上下文 architecture 开发前审查提出。
Why it matters: 没有依赖方向会让 policy、role dispatch、domain validation 和 state 写入互相调用；拆成大量新包又可能过度设计。
Status: confirmed
User answer: 保持现有 `internal/cli` 和 `internal/validate` 包边界，不新拆 package、interface 或架构层。`internal/cli` 只解析命令和输出结果；现有 `workflow.go` / `gate_state.go` 组织调用并写权威状态；JSON decoder 只产生类型化数据；policy 是只读输入；各 domain validator 只返回校验结果，不反向调用 CLI 或写状态。
Downstream effect: 通过修改现有文件的职责实现单向依赖，不引入新的 coordinator framework 或分包工程。
Document impact: design 的 ownership/dependency map 和实现任务边界。
Evidence needed: 具体 owner、调用方向和禁止反向依赖的设计检查。

## RQ-029 - 删除 OpenSpec 专用绑定

Requirement or question: Formal-gates 是否应把 OpenSpec task checkbox 提升为核心证据机制，并为其新增 selector、provenance、授权和 snapshot 特判？
Source: 针对 `files.ca7f5e44bd40` 的零对话上下文 architecture 开发前审查提出原始方案；用户于 2026-07-11 在高星 OpenSpec 项目调研后重新裁决。
Why it matters: Formal-gates 必须能用于没有 OpenSpec 的项目。把 `tasks.md` checkbox 提升为正式 PASS 证据，会迫使其他需求格式引入无业务价值的 OpenSpec 文件和专用机制，也会把进度显示错误地变成核心准入状态。
Status: confirmed
User answer: 删除所有绑定 OpenSpec 的专用机制。四门和最终交付只绑定通用需求目标、当前代码 snapshot、验证结果和 gate 证据。OpenSpec、PRD、SDD、issue 或普通 Markdown 只是可选的需求文档格式；task checkbox 仅是可选、非权威的进度信息。没有 task 文件时直接跳过，不生成 task evidence map。不得新增 task selector policy、task provenance map、checkbox 独占授权、`PRE_FINAL`/`FINAL` task map、checkbox snapshot 规范化或 raw task status binding。机器 artifact 字段使用通用的 `Document impact:`，不得要求 `OpenSpec impact:`。
Downstream effect: 从 proposal、design、spec、tasks、validator、CLI、测试和文档中删除全部 OpenSpec task evidence、checkbox 授权和 checkbox snapshot 专用机制；现有四门、需求澄清、snapshot、验证和 finalization 核心流程保持文档格式无关。
Document impact: requirements artifact 通用字段、requirement document adapter、proposal、design、删除 task evidence accounting spec、清理 evidence closure 和 structured JSON evidence、tasks 以及所有相关实现和测试。
Evidence needed: 非 OpenSpec 的 PRD/普通 Markdown 项目能够完整运行正式流程；OpenSpec task checkbox 缺失、未勾选或过期都不影响 gate PASS；仓库中不存在 task evidence selector/map、checkbox 授权或 checkbox snapshot 特判的产品机制；机器校验接受 `Document impact:` 且不要求 `OpenSpec impact:`。

## RQ-030 - 状态写入的最小保证

Requirement or question: 本次 change 对本地 workflow 状态写入必须提供哪些最小保证？
Source: 针对 `files.5c28b5a5ac58` 的零对话上下文 architecture 与后续 complexity 开发前审查提出。
Why it matters: 无效证据不能污染已有状态；直接覆盖状态文件时若写入中断，可能留下不完整 JSON。
Status: confirmed
User answer: 只做两项。第一，所有输入和证据验证通过后才能写状态，任何验证失败都必须保持原状态逐字节不变。第二，修改现有状态写入函数，先写同目录临时文件，完整写入成功后再替换正式文件；直接复用现有 package 内的做法，不新增抽象或子系统。
Downstream effect: 收紧现有验证与写入顺序，并把 `writeGateState` 从直接覆盖改为临时文件写完后替换。
Document impact: design、evidence closure spec 和现有状态写入实现任务。
Evidence needed: 验证拒绝前后状态逐字节一致；临时文件写入或替换失败时原状态仍可读取；成功写入后状态是完整合法 JSON。

## RQ-032 - JSON wire contract 与跨平台路径语法的具体程度

Requirement or question: 开发前文档是否需要固定正式输出的 schema 版本、字段与类型、unknown/duplicate field 行为、确定性 JSON bytes、SHA-256 编码及跨平台逻辑路径语法？
Source: 针对 `files.5c28b5a5ac58` 的零对话上下文 architecture 开发前审查提出。
Why it matters: 只写“typed/deterministic JSON”会让不同实现产生不同字节和哈希；写死全部 wire contract 又增加文档和实现约束。
Status: confirmed
User answer: AI 不直接编写任何正式 JSON 或语义 JSON。现有 Go CLI 权威生成、解析和校验严格 JSON 合同及确定性字节；reviewer/Carry 通过 `receipt submit` 提交有序语义标量，requirements 与 QA execution owner 通过 compose 命令提交带 1-based 位置的语义标量。证据路径统一为 workflow 内使用 `/` 的相对路径，拒绝绝对路径、`..` 和符号链接逃逸，并覆盖 Windows 与 macOS/Linux 路径样本。不要求第二套独立实现，不新增通用 canonical JSON 库、跨语言协议或额外抽象。JSON 解码、字段校验、位置映射、路径归一化、哈希和证据关系检查全部由现有 Go CLI 静态执行；AI 只处理机器不能替代的语义判断。
Downstream effect: 收紧现有 JSON decoder、提交位置映射、composition proof 和 domain validator，由 CLI 输出确定性的正式 artifact。正式 owner 不再手写或解析静态 JSON，但静态校验不能代替四门的语义判断。
Document impact: design、structured JSON spec、evidence closure spec 和 Phase 1 JSON/closure tasks。
Evidence needed: CLI 拥有的机械 JSON 输出有固定 bytes/hash golden vector；未知、重复、错类型字段和非法路径拒绝用例；位置化标量提交的多项成功、缺失、重复、越界、空值和非法枚举用例；Windows 与 macOS/Linux 路径样本；AI handoff 只消费 CLI 结果和语义审查所需材料。

## RQ-033 - 四门专属机器 judgment 字段

Requirement or question: RQ-020 的“所有正式 PASS 机器字段统一 JSON”是否包括 QA、complexity、architecture、code-quality 各自目前会影响 PASS 的专属 judgment 字段？
Source: 针对 `files.0b657a81a197` 的零对话上下文 complexity 开发前审查提出；用户于 2026-07-11 在 SARIF、GitHub Checks、SonarQube、Semgrep 和 OPA 的实现调研后确认。
Why it matters: 若 reviewer payload 只有机器绑定而没有各门专属判断结果，validator 只能继续读取 Markdown 或放弃现有校验。
Status: confirmed
User answer: 四门使用共享的严格 envelope、统一的 `checks[]` 结果和少量真正必要的专属证据引用，不为每个审查维度新增顶层 JSON 字段。每个 check 包含稳定 `id`、`status`、只用于说明的 `message`、`evidenceRefs` 和必要的 `findings`/`locations`；check ID 来自现有 Go policy catalog。QA 仅额外保留 approved case set、case-to-artifact binding 和 QA-owned evidence；四门共用 changed files、verification 和 context bundle。这些引用是 CLI 校验的 machine-only binding，只能作为输出格式中的固定值，不能让 reviewer 读取其文件或内容。Receipt 由 gate closure 绑定，不放进 reviewer payload。requirements、Carry-Forward Arbiter 和 FinalExecution 继续使用各自 typed payload。不得引入完整 SARIF、OPA、Sonar 指标引擎、自由 `properties` 扩展袋或新的通用 judgment 框架。
Downstream effect: CLI 拒绝缺失、未知或重复 check ID 和未知状态；`NOT_APPLICABLE` 只允许 policy 明确批准的 check 并要求理由；任一 `REVIEW`、`FAIL` 或 `BLOCKED` 都不能聚合为 PASS；只有所有必查项恰好出现一次并为 `PASS` 或允许的 `NOT_APPLICABLE`，且证据闭包和门禁前置条件有效时，才接受顶层 PASS。删除当前草案中 architecture、code-quality 和 complexity 不断扩张的专属 judgment 顶层字段，以及不能由 CLI 重算或 receipt 证明的 reviewer 自报布尔字段。proposal、design、structured JSON spec、四门 agent/reference、Go validator、测试、canary 和示例必须一次改成同一口径，不保留旧字段、旧 Markdown 准入或兼容解析。
Document impact: structured JSON spec、design、agent/reference 模板和 Phase 1 reviewer migration tasks。
Evidence needed: 每门必查 check catalog、合法聚合、缺失/未知/重复 ID、非法状态、非法 `NOT_APPLICABLE` 和顶层 verdict 不一致用例；repo-wide 检查确认旧专属 judgment 字段不再承担机器合同，旧 Markdown 字段校验和兼容路径已删除。

## RQ-035 - QA Design/Design Review 的 JSON 角色表示

Requirement or question: 已确认的 QA `Design`、独立 `Design Review`、必要时 `Design Rework` 和 `White-box Adequacy` 应如何进入统一 JSON evidence 体系？
Source: 针对 `files.e2676cb77217` 的零对话上下文 complexity 开发前审查提出。
Why it matters: 当前封闭 schema 只允许 `Execution`/`FinalExecution`，但 QA admission 又要求 hash-bound Design 和独立 Design Review；实现者会被迫现场发明 stage/role。
Status: confirmed
User answer: 不把每个 QA 动作都升级成独立 JSON role 或 gate。`Design` 只生成 case set 并由现有 designer receipt 绑定，不记录 PASS；`Design Review` 复用统一 reviewer envelope 和 `checks[]`，绑定被审 case set 的哈希、独立 reviewer receipt 和最终接受的 case set；`Design Rework` 不设独立机器 role，case 修改后哈希变化使旧 review 失效，必须重新进行 Design Review。`Execution` 继续使用现有 `qa-test-gate` 并绑定 approved case、QA-owned evidence 和 case-to-result 对应关系；`FinalExecution` 继续作为机械收尾，不新增 QA 判断。曾讨论的 `White-box Adequacy` 即使将来实际需要，也只能复用同一 QA reviewer payload 和 `checks[]`，不能新增 gate 或专用框架；RQ-063 已进一步确认本次 Phase 2 不实现它。
Downstream effect: QA admission 只验证 case set、designer receipt、独立 Design Review、approved case hash 和 development handoff 的绑定；删除为 Design Rework 或每个 QA 阶段新增独立 struct、validator、状态迁移和 role 的方案。Design Review 保留开发前 snapshot，但开发后的 QA Execution 可按同一 workflow 和完全相同的 case-set hash 引用其不可变 closure；普通实现改动不会要求重审 case，case、oracle 或 Case ID 变化才自动要求重新 review，静态记录与真实开发顺序一致。
Document impact: structured JSON spec、QA design admission spec、design 和 Phase 2 QA tasks。
Evidence needed: case set 与 designer receipt 绑定、独立 Design Review 接受、handoff 使用准确 approved case hash、case 修改使旧 review 失效、Execution 使用批准链路，以及禁止新增 Design Rework 机器 role 的文档和实现检查；Phase 2 不出现 White-box 新增实现。

## RQ-036 - Reviewer 必须看到当前已确认的用户决策

Requirement or question: 所有 formal reviewer 是否必须收到与本次审查相关的当前已确认用户决策，并不得把已定决策当作待选问题重新提出？
Source: 用户于 2026-07-11 要求检查，经现有 agent、zero-context 和 reviewer-isolation 文档对照发现。
Why it matters: 当前规则允许 reviewer 读取 current approved requirements、acceptance criteria 和 `User request and acceptance criteria`，但没有要求 context bundle 覆盖所有相关已确认决策。Reviewer 因此可能重复质疑用户已经选定的方向。直接提供包含旧 finding 来源和 repair 历史的 alignment 又可能造成锚定污染。
Status: confirmed
User answer: 所有 formal reviewer 必须看到与当前审查相关的全部已确认用户决策，否则可能重复提出已决问题。当前批准需求必须吸收这些决定，并通过 `Current requirement` 直接提供或定位；`contextBundle` 只用于 CLI 机械校验，不是 reviewer 输入。Reviewer 可以指出决策冲突、文档遗漏或实现不符，但不得仅因为自己偏好不同就重新打开已确认决策。
Downstream effect: 派工前必须检查现有需求文档已覆盖所有相关确认决策；遗漏会影响判断的决策时，reviewer 返回 BLOCKED，不出具完整结论。
Document impact: 所有 formal reviewer agent 的现有 prompt 字段说明、zero-context handoff 指引、reviewer isolation spec、Document Readiness 和 Phase 2 reviewer-context tasks。
Evidence needed: 一个当前需求 bundle 覆盖已确认决策的正向用例，以及遗漏影响审查的已确认决策时不得出具完整结论的用例。

## RQ-038 - OpenSpec 分阶段实现

Requirement or question: 本次 OpenSpec 是否拆成多个独立实现阶段，而不是将已有 diff 修复和所有后续 hardening 挤进一个大 diff？
Source: 用户于 2026-07-11 明确指定。
Why it matters: 一次实现所有 capability 会扩大 diff、混淆问题修复与新机制，也会使测试、回滚和四门判断变得困难。
Status: confirmed
User answer: OpenSpec 必须分阶段实现，不得把全部 capability 挤进一次开发。Phase 1 是统一 JSON cutover 并吸收当前 requirements/policy 修复，Phase 2 是 review chain，Phase 3 是 convergence；每阶段都必须独立实现、验证和审查。当前开发前审查只准入即将交接的阶段；后续阶段的合同必须在自己交接前基于当时的 snapshot 补齐并重新通过开发前审查，但未就绪的后续阶段不阻断更早阶段。
Downstream effect: 每阶段有独立范围、验收、review snapshot 和开发前准入；后续阶段不会默认进入 Phase 1，也不会因尚未轮到详细设计而阻断 Phase 1。
Document impact: proposal 的交付策略、design 的 migration/delivery 顺序、tasks 的 phase 分组和每阶段开发交接。
Evidence needed: 三阶段任务清单，以及每阶段独立实现、验证、snapshot 和 review 的证据。

## RQ-039 - JSON 切换与配套口径由同一阶段完成

Requirement or question: 删除 Markdown 机器准入的阶段，是否必须同时更新 validator、agent、reference、example、canary、fixture 和相关操作文档，而不能把这些口径迁移留到后续阶段？
Source: 新鲜零对话上下文 complexity 开发前审查于 2026-07-11 对当前批准 bundle 提出。
Why it matters: 切换阶段必须一次改齐机器入口和所有实际生产/消费面，否则会进入既不满足新口径、又没有旧入口的中间状态。
Status: confirmed
User answer: 选择一次切换。按照 RQ-047 更新后的编号，Phase 1 删除 Markdown 机器准入时，必须同时更新 validator、agent、reference、example、canary、fixture 和相关操作说明；允许在该阶段内分提交实现，但不能形成新旧两个可交付口径。最终 convergence phase 只做全量验证、残留扫描和最终核对。
Downstream effect: 所有影响实际读写和使用口径的迁移归新的 Phase 1，最终 convergence phase 不再重复承担格式迁移。
Document impact: proposal delivery phases、design migration、structured JSON spec 和新的 Phase 1/final convergence tasks。
Evidence needed: 一个阶段内完成机器入口和所有实际生产/消费面的同口径切换；后续阶段没有重复迁移任务，也不存在混合格式中间状态。

## RQ-040 - Reviewer 输出与 receipt 哈希的生成顺序

Requirement or question: Reviewer JSON、receipt 和 evidence closure 应如何绑定，才能避免 reviewer 输出引用 receipt 哈希、receipt 又引用 reviewer 输出哈希的自引用循环？
Source: 新鲜零对话上下文 complexity 开发前审查于 2026-07-11 对当前批准 bundle 和现有 receipt 实现提出。
Why it matters: 当前 structured JSON spec 把 `reviewerReceipt` 放进 reviewer payload，而 receipt 必须绑定 reviewer 输出哈希。两者都要求对方先完成，现有 design 的“顶层报告单独绑定”也没有明确生成顺序，开发者将被迫现场发明特殊哈希规则。
Status: confirmed
User answer: 使用一个逐门 closure root。RQ-067 进一步确认：CLI 先生成包含全部静态字段和 policy check 目录的 reviewer judgment 目录，reviewer 只通过 CLI 提交语义状态、理由、findings 和 locations；submit 生成完整 PENDING JSON 并把其 hash 原子提交到现有 dispatch proof，finalize 复验后机械聚合 verdict，并生成最终正式 JSON。CLI 对最终字节计算哈希，再生成绑定该哈希的 receipt；closure manifest 包含最终 reviewer artifact、receipt（其现有 dispatch 依赖绑定 submitted hash）和其他全部下层证据。Gate record 只保存 closure manifest 的一个根哈希。不要定义“算哈希时忽略某字段”的特殊规则。
Downstream effect: RQ-017 中“gate record 单独保存 reviewer 输出哈希”的部分由本决定取代。Reviewer judgment 和最终 artifact 都不含 receipt 或 closure 反向引用；静态字段由 CLI 生成，gate closure 成为唯一逐门完整性根。
Document impact: design 的 evidence/receipt 顺序、structured JSON spec、reviewer receipt spec、closure spec 和新的 Phase 1 tasks。
Evidence needed: 明确的单向生成顺序和一个逐门 closure root；reviewer 输出、receipt 或任一下层证据篡改都会失败；不存在字段排除式 canonical hash、自引用或额外顶层哈希 fixture。

## RQ-041 - Role 与领域校验同阶段启用

Requirement or question: 分阶段迁移 JSON 时，一个 role 或 stage 的结构、领域 validator、policy 和测试是否必须在同一阶段完成？
Source: 新鲜零对话上下文 complexity 开发前审查于 2026-07-11 对当前批准 bundle 提出。
Why it matters: JSON 基础阶段不能提前定义后续阶段才会实现的 QA Design Review、White-box Adequacy 或 Carry role。
Status: confirmed
User answer: 只修改任务分组，不为开发中间态增加产品逻辑。按照 RQ-047 更新后的编号，Phase 1 完整交付 requirements、QA Execution、complexity、architecture、code-quality 和 mechanical FinalExecution 的统一 JSON 格式、validator、policy 与测试，并删除 Carry Arbiter、Design Review 和 White-box Adequacy 的提前占位。Phase 2 实现 Design Review 和 Carry Arbiter 时，在各自任务中同时完成 JSON 内容、validator、policy 和正反向测试。统一 envelope 不变。RQ-063 已确认 Phase 2 不实现 White-box Adequacy；将来若重新纳入，仍遵守同阶段完整交付规则。
Downstream effect: 每个业务功能在哪个阶段开发，就在该阶段从结构到语义和测试完整交付。不要增加 disabled role、临时错误码、占位 payload、兼容路径或其他中间态产品行为。
Document impact: proposal delivery phases、design role activation、structured JSON spec 和新的 Phase 1/2 tasks。
Evidence needed: 每个 role/stage 的结构、policy、validator 和测试属于同一阶段；阶段任务不存在结构与语义分离，也没有为开发中间态新增产品规则。

## RQ-043 - 严格 JSON 合同必须在开发前闭合

Requirement or question: 既然 JSON 要拒绝所有未知字段，开发前文档是否必须把 envelope、evidence reference、check、finding、location 和每个 role/stage payload 的字段、类型、必填性与枚举完整定义？
Source: 第二轮新鲜零对话上下文 complexity 开发前审查于 2026-07-11 对当前批准 bundle 提出。
Why it matters: 当前文档只完整列出 envelope 和部分高层字段，却要求所有层级 `unknown field` 都拒绝。`finding`、`location`、stage-specific QA payload 和 requirements/Arbiter/FinalExecution 的完整内部形状仍留给开发者决定，无法提前写全正反向测试，也会隐藏实现范围。
Status: confirmed
User answer: 以现有 Markdown 的机器字段为迁移基线，统一换成一个 JSON envelope，不重新发明一套业务字段。Complexity、architecture 和 code-quality 三个判断型 reviewer 共用同一个 reviewer payload；不同判断字段映射成稳定的 `checks[]` ID。Requirements、QA Execution 和 mechanical FinalExecution 也使用同一个 envelope，但各自只保留确实需要的专用 payload，不把非 reviewer 数据硬塞进 `checks[]` 或万能属性袋。QA Execution 的最终分工由 RQ-059 明确为独立 QA 执行者产出结果与 binding、主代理和 CLI 机械核对。按照 RQ-047 更新后的编号，Phase 1 迁移当时已经存在并实际启用的 requirements、QA Execution、complexity、architecture、code-quality 和 mechanical FinalExecution；以后实现 Design Review、White-box Adequacy、Carry-Forward Arbiter 等角色或 stage 时，在各自 phase 同时定义其 JSON 内容、领域校验、policy 和正反向测试，不提前增加占位字段或禁用角色。文档用现有 Markdown 字段到 JSON 字段或 check ID 的明确映射闭合本阶段合同；Go typed struct 是唯一执行来源，不新增独立 JSON Schema、代码生成框架或第二套 schema 来源。
Downstream effect: design 和 `structured-json-evidence` spec 必须给出新的 Phase 1 现有字段封闭映射、类型、必填/省略规则、枚举和 stage 约束；三个判断型 reviewer 共用一个结构，QA Execution 使用独立的五引用机械 payload。Phase 2 及以后只在业务功能实际启用的 phase 扩展对应 role/stage，不得让 Phase 1 预实现未来角色，也不得让后续 phase 反过来破坏 Phase 1 已交付角色的统一 envelope。
Document impact: design wire contract、structured JSON spec、各 domain spec 的 payload 所有权和新的 Phase 1/2 tasks。
Evidence needed: 每个嵌套对象和 role/stage 都能从文档直接写出合法 fixture、缺字段、未知字段、错类型和错 stage fixture；实现不需要自行发明字段，同时不存在第二份 schema 来源。

## RQ-044 - QA JSON 与业务能力必须属于同一 phase

Requirement or question: JSON 基础阶段是否应提前定义 Design Review、增强后的 approved-case admission 或 White-box Adequacy 字段，再等下一阶段实现其业务校验？
Source: 第二轮新鲜零对话上下文 complexity 开发前审查的第三条 finding，并由用户在确认 RQ-043 时特别要求“对应的 phase 不要漏”。
Why it matters: 如果 JSON 基础阶段声称完成封闭 QA schema，却包含下一阶段才能验证的字段，要么暗中提前实现后续范围，要么交付一套没有完整语义的占位格式。
Status: confirmed
User answer: 不提前定义。按照 RQ-047 更新后的编号和 RQ-059 的最终分工，Phase 1 只把当前已启用的 QA Execution 及其 approved case set、QA-owned results、case-result binding、changed files 和 verification 迁入专用机械 payload。Design Review 在 Phase 2 与 JSON 内容、policy、validator 和正反向测试同时完成；Phase 1 不注册 disabled stage，也不保留占位字段。RQ-063 已确认 Phase 2 不实现 White-box Adequacy。
Downstream effect: Phase 1 QA 合同可以独立实现和验收；Phase 2 只为本阶段实际增加的 Design Review 扩展对应 stage，统一 envelope 不变。
Document impact: design 的 role/stage 表、structured JSON spec、QA design/admission spec 和新的 Phase 1/2 tasks。
Evidence needed: Phase 1 对未来 QA stage/字段明确拒绝；Phase 2 的 Design Review 结构、policy、validator 和测试位于同一任务，且不增加 White-box role、stage 或占位字段。

## RQ-046 - JSON 切换阶段的实际改动面必须可数清

Requirement or question: JSON 切换阶段的“一次切换所有生产和消费面”如何在开发前得到不会过期的实际文件范围，并在完成时检查遗漏？
Source: 第二轮新鲜零对话上下文 complexity 开发前审查的第五条 finding。
Why it matters: 当前任务只说更新“every producer and consumer”，但没有列出哪些代码、agent、reference、canary、example 和 test 属于范围。开发代理可能只改主解析器，漏掉内嵌生成器或操作文档，仍误以为 JSON 切换已经完成。
Status: confirmed
User answer: 不在当前文档写死会被后续改动淘汰的文件清单。按照 RQ-047 更新后的编号，在新的 Phase 1 开发前审查和开发交接之前，针对当时固定的实际仓库 snapshot 运行静态扫描，把精确文件清单写进现有 Phase 1 交接材料；Phase 1 完成时对完成 snapshot 重跑扫描，任何新增或残留的生产/消费面都必须迁移、删除或说明不适用。不要新增运行时 manifest、JSON 证据、产品代码、CLI 命令或长期维护文件。
Downstream effect: 新 Phase 1 的开发前审查获得可数清的当前范围，完成扫描同时覆盖该阶段自己新增的入口，不依赖提前冻结的旧清单。
Document impact: 新 Phase 1 tasks 的盘点时点、交接范围和验收说明。
Evidence needed: Phase 1 开发前固定 snapshot 上生成并经审查确认的实际范围清单，以及 Phase 1 完成 snapshot 上无未解释旧机器字段或解析入口的仓库级残留扫描。

## RQ-047 - 当前修复并入统一 JSON 第一阶段

Requirement or question: 是否删除独立的当前格式修复阶段，把 requirements PASS 和 policy 的当前 diff 修复并入统一 JSON 切换阶段，使该阶段成为新的 Phase 1，后续阶段依次前移？
Source: 第三轮新鲜零对话上下文 complexity 开发前审查的第一条 finding。
Why it matters: Phase 1 文档说只使用当前格式且不预埋 JSON，但当前本地 diff 已经无条件调用 JSON evidence 校验，并包含 schema-v1 类型、policy 输出和 Markdown-only 拒绝测试。现有 Phase 1 tasks 没有授权或列出这项减法，开发者无法同时满足代码现状和阶段合同。
Status: confirmed
User answer: 删除独立的当前格式修复阶段，不先删除半成品 JSON 再重写。新的 Phase 1 完成原 JSON foundation/cutover 的全部范围，并在同一最终格式中吸收原 Phase 1 的 requirements PASS 和 typed policy 修复及其回归测试；不得保留临时 schema-v1 或混合 Markdown/JSON 可交付状态。原 Phase 3 成为新的 Phase 2，原 Phase 4 成为新的 Phase 3。
Downstream effect: 第一阶段是统一 JSON 切换加当前 diff 修复。所有 phase 编号、capability applicability、tasks 和验收口径必须保持一致。
Document impact: alignment 中旧 phase 决定、proposal、design、所有带 phase applicability 的 specs 和 tasks。
Evidence needed: 新 Phase 1 同时通过 requirements/policy 回归、严格 JSON 合同、全生产/消费面切换、closure/path/state 测试和独立 review；不存在独立 Markdown 修复交付或临时 schema-v1。

## RQ-048 - JSON 基础阶段不提前放 Carry 专用矩阵字段

Requirement or question: 新 Phase 1 的全新鲜 FinalExecution 行是否还需要 `resultKind`、`sourceSnapshot` 和 `targetSnapshot`，还是等 Phase 2 真正实现 Carry 时再加入这些字段？
Source: 第三轮新鲜零对话上下文 complexity 开发前审查的第二条 finding。
Why it matters: Phase 1 已有 envelope snapshot 和逐门封存证据绑定；在只能 `FRESH_PASS` 时重复保存 source/target/result kind 没有新增判断价值，却提前发布了 Carry 才需要的形状。
Status: confirmed
User answer: Phase 1 不提前保存 Carry 专用信息。Mechanical FinalExecution 的 `gateMatrix` 有四个固定 gate 行，每行只包含 `gate` 和 `gateEvidence`；`gateEvidence` 引用该门已封存且不再变化的完整证据包。当前 snapshot 直接使用 envelope 的 `changeSnapshot`，CLI 重新验证证据包内的 gate、PASS、workflow 和 snapshot 必须与之匹配。Phase 2 实现 Carry Arbiter 时，在同一任务扩展矩阵行为 `FRESH_PASS`/`CARRIED_PASS`、source/target snapshot 和 carry decision，并补齐校验与测试。
Downstream effect: Phase 1 删除恒定的 `resultKind` 和重复的 source/target snapshot，不损失 snapshot 约束；Carry 的完整矩阵形状只在真正有继承语义时出现。
Document impact: structured JSON spec、design FinalExecution payload 和新的 Phase 1/2 tasks。
Evidence needed: Phase 1 没有 Carry 专用字段；Phase 2 在实现继承时一次性补齐矩阵类型、校验和测试。

## RQ-049 - 保留具体要求，不提来源文件

Requirement or question: 产品文档是否可以保留开发计划中的具体有效要求，但不提来源文件本身，也不为来源文件建立 capability、snapshot 例外、scenario 或 task？
Source: 第三轮新鲜零对话上下文 complexity 开发前审查的第三条 finding。
Why it matters: 来源材料可能包含有用的阶段和验收内容，但文件本身没有运行时行为或长期产品价值；直接写入产品规则会造成永久特殊分支。
Status: confirmed
User answer: 可以保留其中具体、有实际价值的内容，但不要在产品文档中提到来源文件本身。删除该文件专用的 snapshot 例外、capability requirement、scenario 和 task；分阶段实施、每阶段独立收尾、四门独立审查和最终聚合等真实要求继续写在各自负责的 proposal、design 和 tasks 中。
Downstream effect: 产品合同只表达行为和交付要求，不表达这些要求来自哪份一次性材料，也不增加任何专用实现。
Document impact: proposal、design、evidence closure spec、tasks 和 alignment 中的来源文件表述。
Evidence needed: OpenSpec bundle 不出现来源文件名称或专用行为；具体有效的阶段、验收和四门要求仍完整存在。

## RQ-051 - 删除重复的 reviewer 输入字段

Requirement or question: 共享 reviewer payload 是否需要同时保留 `inputManifest` 和 `contextBundle`？
Source: 第四轮新鲜零对话上下文 complexity 开发前审查的第三条 finding，用户于 2026-07-11 确认。
Why it matters: 当前实际派发中两个字段指向同一份带哈希的输入清单；同时保留会重复公共字段、校验和闭包边，没有独立判断价值。
Status: confirmed
User answer: 删除 `inputManifest`，只保留 `contextBundle`。`contextBundle` 直接引用 CLI 可静态校验的严格 JSON 初始输入清单，其每个输入文件都进入证据闭包。`contextBundle` 和 `check.evidenceRefs[]` 都是 reviewer 输出格式中的 machine-only binding，不作为 reviewer 阅读材料。不新增替代字段或另一套 manifest 机制。
Downstream effect: Reviewer payload 只有一个初始输入集合引用，隔离和闭包校验不再对同一文件建立重复边。
Document impact: design 的共享 payload 和字段迁移表、structured JSON spec 的 reviewer payload 合同。
Evidence needed: 合法 reviewer JSON 只要求 `contextBundle`；`inputManifest` 作为未知字段被拒绝；实际 evidence references 仍被递归验证。

## RQ-052 - JSON 输入不按 key 顺序准入

Requirement or question: 语义 owner 的有序标量提交是否还需要额外的 JSON key 顺序准入规则？
Source: 第五轮新鲜零对话上下文 Phase 1 complexity 开发前审查的第三条 finding，用户于 2026-07-11 确认。
Why it matters: JSON 对象的 key 顺序没有语义；拒绝顺序不同但字段完全相同的输入，需要额外的顺序感知解析和测试，却不提高准入正确性。
Status: confirmed
User answer: 不存在 AI 编写 JSON 的生产入口，因此不设置语义 JSON key 顺序规则。CLI 按 1-based 位置和固定语义值顺序校验必填性、重复、越界、类型、静态投影和领域语义。Receipt 哈希 CLI finalize 后的正式 reviewer artifact；所有 CLI 生成的正式 artifact、receipt、closure、state 和 mechanical FinalExecution 保留固定输出顺序和确定性字节。
Downstream effect: 删除无判断价值的顺序敏感准入规则，同时保留严格 schema、模板 proof、精确输出哈希和全部 CLI 正式产物的确定性。
Document impact: RQ-032 的 canonical JSON 边界、design 的 envelope 描述、structured JSON spec 的输入准入和 Phase 1 closure task。
Evidence needed: 等价的定位后标量提交生成相同正式结果；缺失、重复或越界位置失败；CLI 自有机械文件的 golden bytes/hash 稳定。

## RQ-053 - FinalExecution 直接引用每门的封存证据包

Requirement or question: Phase 1 FinalExecution 应该引用可变状态文件中的 gate record，还是直接引用每门已封存的完整证据包？
Source: 新鲜零对话上下文 Phase 1 architecture 开发前审查的第一条 finding，用户于 2026-07-11 确认。
Why it matters: Gate record 实际位于会被 FinalExecution 继续修改的状态文件中。FinalExecution 若引用该文件的哈希，写入自身后哈希会立即失效；若额外生成 gate-record 文件，则增加一类无必要 artifact。
Status: confirmed
User answer: 每个 FinalExecution 矩阵行只保存 `gate` 和 `gateEvidence`。`gateEvidence` 是该门已封存完整证据包的路径和哈希；CLI 通过它重新验证该门、PASS verdict、workflow、当前 snapshot、receipt 和全部下层证据。不引用会继续变化的状态文件，不新增独立 gate-record 文件。
Downstream effect: FinalExecution 对四门的引用稳定且无自引用；状态文件可以继续记录 FinalExecution，而不会使已引用的门证据失效。
Document impact: RQ-048 的矩阵字段、design 的 FinalExecution 数据流、structured JSON 和 carry/finalization spec。
Evidence needed: 四个矩阵行只有 `gate` 与 `gateEvidence`；每个引用都能重验该门的封存证据包；记录 FinalExecution 不会改变任一被引用哈希；不存在额外 gate-record artifact。

## RQ-054 - Context bundle 逐文件校验

Requirement or question: `contextBundle` 是只保护输入清单文件本身，还是必须让 CLI 逐个校验清单中的输入文件？
Source: 新鲜零对话上下文 Phase 1 architecture 开发前审查的第二条 finding，用户于 2026-07-11 确认。
Why it matters: 只校验清单文件的哈希，不能发现清单所列文件已被修改，无法兑现“审查输入变化使 PASS 失效”的已确认保证。继续使用未定义的普通文本，又会迫使实现者现场发明解析规则。
Status: confirmed
User answer: `contextBundle` 引用的唯一初始输入清单使用严格 JSON，包含 `bundleVersion`、`workflowId`、`changeSnapshot` 和 `inputs[]`。`inputs[]` 的每项使用现有 `EvidenceRef` 的 `path` 和 `sha256`。CLI 根据调用方提供的源路径生成清单、路径和 hash，再静态校验清单与每个输入文件并纳入证据闭包；AI 不手写、解析或读取清单。Bundle 路径和内容不进入 reviewer 实质输入。
Downstream effect: `contextBundle` 仍是 reviewer payload 中唯一的初始输入集合引用，但它所列每个文件都成为可机械复验的闭包依赖。
Document impact: design 的 reviewer 输入数据流、structured JSON 的 context bundle 合同、evidence closure 的图边和 Phase 1 closure task。
Evidence needed: 合法 context bundle 和全部输入通过；未知/重复字段、workflow/snapshot 不匹配、重复路径、缺失文件或哈希不匹配失败；修改任一输入后对应 PASS 闭包失效。

## RQ-055 - 并行审查由外部凭证区分

Requirement or question: 并行运行多个审查任务时，是否需要 reviewer 在自己的 JSON 中填写 `reviewSessionId` 才能防止结果混淆？
Source: 新鲜零对话上下文 Phase 1 architecture 开发前审查的第三条 finding，用户于 2026-07-12 确认。
Why it matters: Reviewer 自报的会话 ID 不能证明真实执行者，而且该 ID 可能在宿主捕获启动/结束事件时才确定。强迫 reviewer 提前填写，会导致猜测、事后改写输出或额外握手。
Status: confirmed
User answer: 从 reviewer payload 删除 `reviewSessionId`，不新增替代身份字段。外部 receipt 使用系统生成的唯一 dispatch ID、宿主捕获的实际 subagent ID、独立输出文件路径与精确哈希，并绑定 workflow、gate、stage 和 snapshot。并行的未完成 dispatch 必须使用不同输出路径；如果同一路径对应多个未完成 dispatch，直接拒绝而不是选一个或混合。
Downstream effect: 并行审查依靠宿主实际生命周期和精确产物绑定，不依赖 reviewer 自报身份；路径冲突时 fail closed。
Document impact: design 的共享 reviewer payload 和 receipt 数据流、structured JSON 字段表、reviewer receipt/isolation 的并行区分规则。
Evidence needed: 两个并行 reviewer 使用不同 dispatch/输出路径时各自生成唯一匹配 receipt；支持生命周期的 provider 还需不同 subagent 和不交叉的 lifecycle；错误输出哈希或共用路径失败；`reviewSessionId` 作为未知 reviewer 字段被拒绝。

## RQ-056 - 公开 JSON 合同与 CLI 内部文件的边界

Requirement or question: Receipt、封存证据包、状态文件和最终验证记录的每个 JSON 字段，是否都必须升级成用户可见的公开合同？
Source: 新鲜零对话上下文 Phase 1 architecture 开发前审查的第四条 finding 所提公共面问题，用户于 2026-07-12 确认。
Why it matters: 将 CLI 自己生成和读取的运行文件全部公开化，会增加大量字段表、兼容压力和无必要的外部承诺；但 reviewer、requirements 和公开 CLI 输出仍需要明确合同。
Status: confirmed
User answer: 只把真正的语义标量参数和公开输出当作公开合同。Reviewer/Carry/QA Design 的 submit 参数、requirements 与 QA Execution 的位置化 compose 参数、context bundle 和 FinalExecution 的可见输出必须完整闭合；`policy show --format json` 作为公开命令输出，也必须固定格式和 rule/check ID。所有正式 artifact、receipt、每门封存证据包、状态文件和最终验证记录都由 CLI 生成；文档规定必要保证和所有权，Go typed struct 和行为测试决定内部字段，不将每个字段变成用户手写兼容承诺。旧 workflow 在格式切换后直接重启。
Downstream effect: 公共面仅保留 AI 需要填写的语义槽位、调用方需要提供的源路径和可读输出；CLI 内部运行文件仍由类型化代码和测试约束，不扩张 AI 手写 schema 或兼容层。
Document impact: design 的 public/internal 所有权、structured JSON 的合同范围、closure/state/final verification 的内部文件声明，以及 policy 公开输出合同。
Evidence needed: 公开输入/输出有封闭字段和正反测试；内部文件无用户手写入口、无兼容承诺且仍满足不可变证据、原子状态和静态复验等行为保证；旧 workflow 被拒绝并重启。

## RQ-057 - Phase 1 公共 JSON 删除 route

Requirement or question: Phase 1 公共 JSON 是否还需要 `route` 对象以及 `nextAction`、`reworkOwner`、`rerunFrom` 三个字段？
Source: 用户于 2026-07-12 在 Phase 1 开发前对齐中确认。
Why it matters: 这些字段重复表达 `artifactRole` 和 `verdict` 已能确定的当前动作，并提前发布了 Phase 2 才有意义的返工归属和重跑边界，增加公共合同、校验和测试成本。
Status: confirmed
User answer: 删除整个 `route` 对象，不保留 `nextAction`、`reworkOwner` 或 `rerunFrom`，也不改名迁移。CLI 根据 `artifactRole + verdict` 推导 Phase 1 行为；非 PASS 结果不写正式 PASS 状态。具体返工说明放在 findings 和人类报告中。重跑边界等到 Phase 2 真正实现 Carry Arbiter 和 transition 时再定义。
Downstream effect: Phase 1 envelope 只保留顶层 `verdict`；旧 `gate_route` 动作、归属和重跑字段在 JSON 切换时直接删除。任何输入中的 `route` 都按未知字段拒绝。Requirements、QA Execution 和 FinalExecution 只有合法 PASS 才能进入各自操作路径；只有真正的 reviewer artifact 可以用其他 verdict 保留结果和问题说明。
Document impact: design、structured JSON spec、requirements PASS spec、evidence closure spec 和 Phase 1 tasks。
Evidence needed: 合法 Phase 1 fixture 不含 `route`；含 `route` 或三个旧字段的 fixture 被未知字段校验拒绝；各 `artifactRole + verdict` 的准入行为测试证明非 PASS 不写正式 PASS 状态；Phase 1 不实现或猜测重跑边界。

## RQ-058 - 三种操作型通过证明只允许 PASS

Requirement or question: `REQUIREMENTS_PASS`、`QA_EXECUTION` 和 `FINAL_EXECUTION` 是否需要支持 `REVIEW`、`FAIL` 或 `BLOCKED`？
Source: 新鲜零对话上下文 Phase 1 complexity 开发前审查的第一条 finding，用户于 2026-07-12 确认。
Why it matters: 这三种文件只在需求确认完成、QA 执行证据完整通过或最终收尾成功时生成。若允许非 PASS，开发者还必须为没有审查判断的操作型 payload 发明失败字段和路由。
Status: confirmed
User answer: 不支持。`REQUIREMENTS_PASS`、`QA_EXECUTION` 和 `FINAL_EXECUTION` 只接受顶层 `PASS`。需求没有确认完时继续澄清，不生成 `REQUIREMENTS_PASS`；QA 结果不完整或有失败时不生成可记录的 `QA_EXECUTION`；最终条件没有满足时 CLI 报错，不生成 `FINAL_EXECUTION`。Complexity、architecture 和 code-quality reviewer 仍可输出 `REVIEW`、`FAIL` 或 `BLOCKED`。不新增字段、失败枚举或状态。
Downstream effect: 三个操作型 role 的严格校验在 payload 解析前拒绝非 PASS；其成功字段不扩展失败取值。Reviewer 的非 PASS 仍可携带 checks、findings 和人类说明，但不能写正式 PASS 状态。
Document impact: design 的 role/verdict 规则、structured JSON spec 和 Phase 1 role-dispatch tests。
Evidence needed: 三个操作型 role 的非 PASS fixture 全部拒绝且不生成文件、不改状态；三个当前 reviewer role 的合法非 PASS fixture仍可作为审查结果通过结构和领域校验，但不能记录 PASS。

## RQ-059 - QA 执行证据由主代理机械核对

Requirement or question: 开发后的 QA Execution 已由独立 QA 执行者产出批准用例、QA 自有结果和 case binding 后，是否还需要另派一个零上下文 QA reviewer 审查执行证据？
Source: 用户于 2026-07-15 明确确认：“审测试用例必须要子代理没问题，审执行证据属于流程的问题，流程问题都是主代理调度”。
Why it matters: 如果 QA Execution 只为防止执行者自封 PASS，再增加一个 reviewer 会重复检查主代理和 CLI 已能机械验证的 case、hash、snapshot 和 binding，增加 token、时间、receipt 和失败面；同时把流程调度错误地包装成第二次质量审查。
Status: confirmed
User answer: 测试用例的设计审查必须继续由独立子代理完成。开发后的 QA 执行仍由独立于开发者的 QA 执行者运行并产出 QA 自有证据，但执行证据的完整性、hash、snapshot、case binding、准入和正式记录属于流程问题，由主代理调用 CLI 机械核对和调度，不再派第二个 QA reviewer，也不要求 QA Execution reviewer receipt。主代理不得借此重写用例、增加 QA 判断或把开发者自测冒充 QA 自有执行。
Downstream effect: 将 Phase 1 `qa.execution.v2` 从 receipt-bound `QA_REVIEW` 改为主代理调度的机械 `QA_EXECUTION`，直接绑定 approved case set、QA-owned results、case-to-result binding、changed files 和 verification。删除 QA Execution 的 reviewer dispatch、review checks 和 receipt 要求；complexity、architecture、code-quality reviewer 以及测试用例 Design Review 的独立子代理和 receipt 规则保持不变。
Document impact: QA gate agent/reference、post-development artifacts、structured JSON、policy baseline、QA design admission、reviewer receipt/isolation、design、tasks、examples、canary 和 CLI/validator tests。
Evidence needed: 独立 QA 执行证据和完整 binding 通过时，主代理调用 CLI compose 生成并记录机械 `QA_EXECUTION`；主代理不写 envelope、路径、hash 或 binding。缺 case、失败结果、错误 hash、旧 snapshot、错误 binding 或开发者自测替代 QA 证据时拒绝且不改状态；QA Execution 不要求 reviewer dispatch/receipt；测试用例 Design Review 和其余三门 reviewer 仍要求独立子代理证据。

## RQ-061 - 调度优化延后为 Phase 2.5

Requirement or question: 是否把原调度优化阶段移到现有 Phase 2 之后，并使用 Phase 2 的实际开发和四门运行作为 Phase 2.5 的样本？
Source: 用户于 2026-07-17 明确要求：“你就把 phase1.5 改成 2.5”。
Why it matters: 需要用一次自然运行确认并行调度、返修影响判断和逐门继承记录是否覆盖真实使用，不为比较方案重复运行整套门。
Status: confirmed
User answer: 该调度优化阶段命名为 Phase 2.5，并排在现有 Phase 2 之后。Phase 2 自然产生的开发、QA 和四门运行作为一次实际样本；不为实验重复跑另一套门。样本要分开记录开发前 QA Design/Design Review/Design Rework、候选开发与开发后 QA Execution，并汇总各阶段实际耗时、完成的 review-repair 轮次、去重后的 blocker、snapshot 变化和宿主能够可靠提供的 token 数据。材料只使用现有 run-local restricted 记录，不新增产品字段、统计命令、历史数据库或自动选择器。RQ-064 已进一步确认开发前 QA 样例循环与候选开发并行；RQ-065 已确认开发前三个检查并行、开发后 QA Execution 与三门并行，以及返修后的逐门重跑/继承判断。
Downstream effect: Phase 2.5 使用自然样本补齐并实现已确认的并行调度、返修影响判断和逐门继承/重跑记录；现有 Phase 3 继续位于其后。
Document impact: proposal、design、tasks 和 alignment；删除旧的调度选择口径，不新增专属 policy baseline。
Evidence needed: Phase 2 自然运行的简短事实汇总、并行启动/完成与返修影响记录，以及确认后补齐并通过开发前检查的精确实现合同。

## RQ-062 - Phase 2 每轮耗时与次数留痕

Requirement or question: Phase 2 应如何记录 QA、review、repair 和等待，才能在 Phase 2.5 判断实际耽搁了多久、发生了多少轮？
Source: 用户于 2026-07-17 启动 Phase 2 时明确要求每跑一轮都留痕统计耗时和次数。
Why it matters: 只记录最终 PASS 或聊天感受，无法区分时间耗在 QA Design、Design Review、Design Rework、QA Execution、四门 reviewer、返修、重新验证、工具故障还是等待，也无法为 Phase 2.5 的并行调度和逐门重跑判断提供本项目事实。
Status: confirmed
User answer: 从 Phase 2 启动时开始维护一份 run-local restricted 台账。每个完整 QA/review/repair 轮次立即记录稳定编号、阶段或 gate、开始和结束时间、耗时、完整正式结果、去重后的 blocker 数与根因、接受的返修、复验、snapshot 前后变化、重跑次数和宿主能够可靠提供的 token。开发前 QA Design/Design Review/Design Rework、开发后 QA Execution 和四门 reviewer 必须分开统计。派发失败、网络或工具故障、中断、排队和纯等待也记录耗时及原因，但按照现有轮次定义不计为完成的 review-repair 轮次。记录使用现有 `.gates/runs/<workflow-id>/restricted/`，不新增产品 schema、CLI 字段、统计命令、数据库、自动策略或 PASS 条件。
Downstream effect: Phase 2.5 使用这份台账和正式产物做事实汇总；缺少宿主 token 指标时如实写 unavailable，不得估算或让缺失指标阻断 Phase 2。
Document impact: alignment、Phase 2 run-local restricted 台账和 Phase 2.5 样本汇总；不修改产品 spec。
Evidence needed: 每个完成轮次当场更新台账；Phase 2 结束时总次数、总耗时、等待/故障耗时和 snapshot 重跑链可由明细直接复核。

## RQ-063 - Phase 2 是否实现 White-box Adequacy

Requirement or question: Phase 2 是否现在实现可选的 `White-box Adequacy` reviewer stage？
Source: Phase 2 合同刷新于 2026-07-17 发现 tasks 只写“如果实际需要”，当前没有决定是否进入本阶段。
Why it matters: 实现它会增加一个正式 reviewer stage、policy、check catalog、validator、receipt 和正反测试；不实现则 Phase 2 只交付已明确需要的 Design Review、restricted 隔离和 Carry。该选择直接改变开发范围和后续 QA 循环成本。
Status: confirmed
User answer: Phase 2 先不实现 White-box Adequacy。
Downstream effect: Phase 2 删除 White-box Adequacy 的可选实现任务和待实现占位口径，只交付已确认的 Design Review、restricted 隔离和 Carry。以后出现黑盒无法证明的具体正常使用缺口时，再单独对齐是否实现；当前不保留 disabled role、占位字段或半套实现。
Document impact: Phase 2 tasks、QA design/admission spec、structured JSON applicability、policy catalog 和测试范围。
Evidence needed: Phase 2 文档和开发 diff 不包含 White-box Adequacy 的新增 role、stage、payload、policy、validator、receipt 或测试。

## RQ-064 - QA 用例循环与候选开发并行

Requirement or question: QA Design、Design Review、Design Rework 是否可以与候选开发并行，以及双方必须保持什么隔离？
Source: 用户于 2026-07-17 指出 QA 样例来自开发文档、开发代码不修改文档，并明确要求当前 Phase 2 也并行进行。
Why it matters: 两边都从同一版冻结需求独立工作时，等待样例批准后才写代码没有技术依赖，只会把 QA 循环的全部耗时叠加到开发耗时上。
Status: confirmed
User answer: 可以并行。QA Design、Design Review、Design Rework 和候选开发都以同一版冻结需求为输入；QA 不看实现、diff、开发者自测或开发说明，开发者不看 QA 草稿、Design Review 结论或返修记录。Design Review 只返修样例、Case ID、oracle 和证据路径，不擅自改变需求。样例批准前可以产生候选代码，但不能宣称正式验收通过；若 QA 暴露出真正的需求歧义，则只暂停受影响的开发切片并向用户澄清。开发前的三个检查并行运行。开发后 QA Execution、Complexity、Architecture、Code Quality 四门也并行运行。
Downstream effect: 本项取代 RQ-061 中“Phase 2 保持当前正式顺序”的开发前串行含义，不改变各门的正式 PASS 条件。开发后每轮如果发生返修，由新的独立零上下文子代理直接比较本轮返修前后的 VCS snapshot，逐门判断哪些门必须重跑、哪些门可以继承；不把当前工作区其他本地改动带入判断。主代理只负责调度和记录，不能自审决定继承。Phase 2.5 不再比较 A/B/C，也不新增实验运行、自动选择器或额外审查层。
Document impact: proposal、design、tasks、SKILL、README、QA gate reference/agent、development worker 和行为夹具。
Evidence needed: 黑盒 case 覆盖并行启动、双向盲态、批准前不得正式验收，以及批准后可直接采用、修改或删除候选代码而无需重写。

## 完成决策

## RQ-065 - Phase 2.5 调度方案

Requirement or question: 开发前和开发后的审查如何并行，返修后哪些门重跑？
Source: 用户于 2026-07-19 明确确认。
Why it matters: 固定并行能减少等待；返修后逐门判断能避免无脑重跑，也不能由主代理自行继承结果。
Status: confirmed
User answer: 开发前三个检查并行运行。开发后 QA Execution、Complexity、Architecture、Code Quality 四门并行运行。每轮结束后如果有返修，返修时开新的独立零上下文子代理，直接比较本轮返修前后的 VCS snapshot，逐门判断哪些门需要重跑、哪些门可以继承；不把当前工作区其他本地改动带入判断。不用重跑的门继承原结果。主代理负责调度，不能自审继承合理性。用户于 2026-07-20 进一步确认：本轮发现的三个问题按工作量拆分，小型的 gate-state 并发写丢更新和 handoff custom run-dir 传播错误在 Phase 2.5 修复；VCS 原生返修快照比较在 Phase 3 完成。用户于 2026-07-23 进一步确认：若 active workflow 已有旧 snapshot 的 post-development gate PASS，在新 snapshot 上为该 gate `receipt register` 或机械 QA Execution composition 前，必须已有该 target 的 terminal Carry transition 明确对该 gate 作出 RERUN（现有枚举为 `RERUN_REQUIRED`）；`ACCEPT_CARRY`、`BLOCKED` 或无决定都拒绝新 snapshot 重跑，只有无该 gate 旧 PASS 的首次执行可直接开始。worker 返修期间，编排 AI 可以并行准备 source-closure 选择、context inputs 和不可变 command shape，但 Carry registration/dispatch/judgment 必须等 worker 固定修后 VCS snapshot 且 CLI 生成精确 transition 后才开始，不使用 mutable future ref、等待中的 Arbiter 或两阶段裁决。
Downstream effect: Phase 2.5 补齐并行调度、返修影响判断、逐门继承/重跑记录、共享 state 的跨进程串行提交和 handoff 已解析 run-dir 的端到端传播；不实现 A/B/C 选择、不新增自动选择器或额外 reviewer 层。Phase 3 由 worker 和 reviewer 使用现场 VCS 固定并比较返修前后 snapshot；formal-gates 复验 prompt、receipt、transition 和裁决记录；CLI 在新 snapshot 的 gate register 和 QA Execution composition 前机械要求对应 terminal `RERUN_REQUIRED`，并允许编排 AI 仅并行准备不可变输入，禁止提前 Carry judgment。
Document impact: proposal、design、tasks、四门调度说明、返修影响判断和 Carry 证据绑定。
Evidence needed: 同一冻结需求和 snapshot 上的并行派发、独立结果、并发 `record-stage` 不丢 state、custom run-dir handoff 可生成并复验，以及返修后基于 VCS snapshot 原生比较的逐门重跑或继承决定。

## RQ-066 - 取消门间 PASS 顺序和串行返修后缀

Requirement or question: Phase 2.5 并行运行四门后，是否仍保留门与门之间的 PASS 准入顺序、Carry 前缀限制和“从最早失败门起全部重跑”的串行机制？
Source: 用户于 2026-07-19 明确要求：“那就取消这个机制，相关过期冗余逻辑删干净。”
Why it matters: 四门 reviewer 只读取同一份已确认需求和同一 target snapshot，不读取其他门的结论。继续要求按 QA、Complexity、Architecture、Code Quality 顺序记录，会把已经确认的并行模型重新串行化；继续使用 Carry 前缀和最早重跑边界，也无法表达某一门重跑而其他不受影响门继承的逐门决定。
Status: confirmed
User answer: 取消开发前检查和开发后四门之间的 PASS 准入顺序。各检查或 gate 在同一冻结需求、workflow 和 snapshot 上独立并行运行并可独立记录；固定 gate 顺序只可保留为确定性的展示或最终矩阵顺序，不能再作为 gate-to-gate 准入条件。FinalExecution 仍须机械验证四门都有当前 target-bound 的 fresh PASS 或经本轮独立 Carry Arbiter 接受的 CARRIED PASS。每次返修产生新 snapshot 后，新的独立零上下文 Carry Arbiter 对每个可继承的既有 PASS gate 分别决定 `ACCEPT_CARRY`、`RERUN_REQUIRED` 或 `BLOCKED`；删除只能继承固定前缀、由最早失败门派生统一重跑边界、以及强制该门之后全部重跑的逻辑和字段。未获继承的 gate 自己重跑；一个 gate 的重跑不能自动迫使其他经 Arbiter 接受的 gate 重跑。相关过期代码、schema 字段、validator 分支、文档和测试必须删除干净，不保留无行为价值的兼容路径。
Downstream effect: Phase 2.5 将 post-development policy 的 gate-to-gate prerequisites 改为四门独立准入，将 start-readiness 的三个独立结论（complexity、architecture、cold-water）改为并行收集后统一判定 ready，并把 Carry 从前缀/最早边界模型改为逐门组合。QA Design、Design Review、Design Rework 仍按各自的 case-set/review/rework 依赖链运行，但可与候选开发重叠；它们不是这里所说的三个 start-readiness checks。Requirements PASS、同 workflow/snapshot/hash 绑定、独立 reviewer、receipt/closure 完整性和 FinalExecution 的全四门聚合保持不变。
Document impact: alignment、proposal、design、tasks、policy baseline、Carry/finalization spec、SKILL、README、各 gate/reference/agent 调度说明、Go policy/validator/state/CLI 实现及其最低层直接测试。
Evidence needed: 正常公开 CLI 流程证明四门可在任意顺序独立记录、缺任一门时 FinalExecution 仍拒绝、混合 Carry 决定可让受影响门重跑而保留其他门的 target-bound 继承、旧 prefix/earliest-rerun/downstream-suffix 字段和行为已不存在；并行 receipt/output 仍保持逐派发独立绑定。

## RQ-067 - 所有静态内容必须由脚本生成

Requirement or question: 正式 workflow 中的静态字段、目录、路径、哈希、绑定和机械聚合能否由 AI 手写或复述？
Source: 用户于 2026-07-19 明确要求：“所有的静态相关的内容都必须脚本生成，禁止ai手写”。
Why it matters: 只要要求 AI 复述已由机器确定的字段或静态检查结果，就会出现语义审查已经完成、却因漏写静态文本而作废的假返修；同时重复了 CLI 已能确定性完成的工作。
Status: confirmed
User answer: 禁止 AI 从零手写完整正式 JSON，也禁止 AI 手写或复述 schema/version、role、workflow/snapshot、gate/stage、policy/check 目录、路径/hash/binding、receipt/closure/state、验证外壳和机械 verdict 等静态内容。CLI 必须先生成静态目录或在 finalize 时机械合成最终 artifact；reviewer/Carry 只通过 CLI 提交语义状态、理由、findings、locations 或 decision。纯静态 prompt 复核不得保留为 reviewer check；语义 anti-anchor 判断仍由 reviewer 负责。CLI 必须拒绝 AI 改写静态投影。
Downstream effect: Receipt registration 生成 reviewer judgment 目录，`receipt submit` 从语义值机械生成完整嵌套 JSON 并原子记录 hash/proof，finalize 要求该 proof 后聚合 verdict，再写最终正式 JSON 和 receipt。每种 role 只接受自己的语义字段，reviewer/Carry 混入 QA Design values 或其他跨 role 字段时必须在写 artifact/dispatch 前拒绝。同一 open、未 finalize dispatch 已有合法 `SemanticSubmissionSHA` 且当前 artifact 字节哈希严格匹配时，调用者可用现有 `receipt submit` 全量重提交该角色的全部语义；CLI 必须从原静态 catalog 重建 projection、重跑全部验证并原子更新 artifact 与 dispatch proof。不得新增 reset/reopen 命令或状态；artifact 被手改、SHA 缺失或不匹配、dispatch 已 finalize、输入不完整或其他验证失败时，两者字节保持不变。Registration 只接受角色专用的源路径并固定生成 check ID/binding，调用方不得提交通用 ID/path 或 gate/path 小语法。QA Design 只提交每个生成位置的七个有序语义值，CLI 写入标题、Case ID、字段名、分隔符和末尾换行并记录 submission proof；Design finalize 后的 Rework 通过新的 Design registration/submit 产物完成，不手工编辑 case set。Requirements 只提交 alignment/global/dimension 位置化标量，QA Execution 只提交 case position/outcome/procedure/observation/oracle result 标量；CLI 生成 RQ/DIM/Case/Execution ID、JSON 结构、引用和 binding。旧的 AI JSON producer、editable template、通用 binding/transition mini-language 和直接编辑正式 artifact 的指令、样例和生产入口全部删除，不保留兼容路径。
Document impact: alignment、proposal、design、tasks、structured JSON/policy/receipt specs、SKILL、README、agents、references、CLI、validator、tests 和行为夹具。
Evidence needed: 公开 CLI 正例证明目录、嵌套 artifact、submission proof 和最终 artifact 的所有静态内容均由脚本生成；AI 只提交位置化语义标量；reviewer、Carry 和 QA Design 可在同一 open、未 finalize dispatch 上凭匹配的 `SemanticSubmissionSHA` 全量重提交并得到从原 catalog 重建的原子替换；未知/重复/缺失/越界/空值/错误类型/非法 location/跨 role 字段、手改 artifact、SHA 缺失或不匹配及已 finalize dispatch 在落盘前拒绝且 artifact/proof/source 字节不变；直接编辑 JSON 或缺少 submit proof 时拒绝；正式流程不再因 AI 漏写静态结构而失败。

## RQ-069 - tracked 自动识别与 untracked 主动提交

Requirement or question: Phase 3 是否需要自动判断工作区中哪些文件属于本次需求，还是由 worker 明确提交全部交付路径，并确保这些路径都已纳入现场 VCS？
Source: 用户于 2026-07-20 询问是否需要自动识别，并确认：“好，按你说的来”。
Why it matters: 未跟踪文件可能是需求文件，也可能是工作区垃圾，程序无法可靠猜测用户意图；交付文件若一直未进入 VCS，返修时也无法用原生 VCS 准确区分修前和修后状态。
Status: confirmed
User answer: 不开发自动判断“哪些文件属于本次需求”的分类系统。worker/编排 AI 使用现场可用的 Git、SVN、P4 或等价 VCS 生成完整交付 diff，并显式提交本次全部交付路径。worker 创建交付文件后必须立即将该明确路径加入现场 VCS；要修改或删除原本未跟踪但属于本次交付的文件，必须先将该路径加入现场 VCS；已有跟踪文件正常修改。只能加入明确的本次交付路径，禁止 `git add .`、`git add -A` 或等价的全工作区操作。worker 返回前，交付路径不得残留 untracked/unversioned 文件，完整 VCS diff 必须包含全部交付内容；无关未跟踪文件不触碰。CLI 的 `artifact compose-changed-files` 只校验仓库相对路径、拒绝 `.gates`、排序和去重，再生成 changed-files artifact 及 composition proof；它不解析 diff、不扫描 VCS、不读取或保存项目内容。
Downstream effect: reviewer 和 worker 直接使用现场 VCS 检查代码变化，CLI 生成的 changed-files 是独立的路径证据。worker 在返回前核对交付路径和完整 VCS diff，不把无关未跟踪文件纳入。
Document impact: alignment、proposal、design、Phase 3 tasks、development worker、structured evidence spec、CLI 和直接行为测试。
Evidence needed: 正常公开流程证明声明路径被规范化生成且重复提交被去重，绝对路径、越界路径、反斜杠、控制字符和 `.gates` 路径被拒绝；未声明的无关 untracked 文件不因工作区数量而被扫描或纳入。

## RQ-070 - VCS 可变与无 VCS 工作区

Requirement or question: 正式开发和审查证据应直接依赖 Git，还是允许调用方使用 Git、SVN、P4 等不同 VCS；没有 VCS 时是否支持正式流程？
Source: 用户于 2026-07-20 指出管理工具可能是 Git、SVN、P4，甚至没有，并要求调查 Codex、Claude Code 等上层项目的做法后再决定；同日后续确认正式流程必须使用 VCS。
Why it matters: 若核心合同写死 Git 的专用概念，SVN 和 P4 无法使用；若为了统一后端而自行重做版本管理，又会显著扩大 Phase 3。该决定影响 VCS 责任边界、无 VCS 行为、CLI 参数和支持范围。
Status: confirmed
User answer: 正式开发、四门、Carry 和 seal 必须存在可用 VCS；worker、编排 AI 和 reviewer 直接调用现场 Git、SVN、P4 或等价工具查看原生 diff、stat 和文件内容，并提供外部 snapshot 标识。formal-gates 保留外部 VCS 元数据、workflow snapshot、静态证据和裁决记录。无 VCS 时在开发前明确停止并要求先初始化或接入 VCS。
Downstream effect: 不暴露内建 VCS acquisition flags、第二个 snapshot 入口或内部 diff 命令。无法可靠生成修前到修后 diff 时不得提议 Carry；受影响门不能在没有 terminal `RERUN_REQUIRED` 的情况下进入新 snapshot 重跑。
Document impact: alignment、proposal、design、Phase 3 tasks、CLI、worker handoff、Carry/finalization spec 和直接行为测试。
Evidence needed: 正常公开流程证明 Git、SVN、P4 的 snapshot 可由 worker 和 reviewer 直接用于交付及返修比较；无 VCS 在开发前被明确拒绝。

## RQ-072 - 宿主无关的 gate 运行目录

Requirement or question: Claude Code、Codex 和 Cursor 是否应各自维护一份 gate 运行证据，还是共用项目根目录下唯一的 `.gates` 目录？
Source: 用户于 2026-07-20 提出直接改为 `.gates` 并回复“可以”确认。
Why it matters: Gate 证据属于项目 workflow，不属于某个 AI 宿主；所有宿主必须看到同一套运行真值。
Status: confirmed
User answer: Claude Code、Codex 和 Cursor 共用项目根目录下唯一的 `.gates` 运行目录。新 workflow 只使用 `.gates/runs/**`，其他正式流程临时内容也位于 `.gates/**`。`.claude`、`.codex`、`.cursor` 只继续存放各宿主必需的 skill、配置和 hook。
Downstream effect: 所有 run path、默认目录、路径隔离、snapshot 排除、changed-files 排除、CLI 报错、文档、agent 规则和测试统一切换为 `.gates`；不维护两套 gate 运行真值。
Document impact: alignment、proposal、design、Phase 3 tasks、相关 spec、SKILL、README、agents、references、CLI、validator、installer/canary 路径及测试。
Evidence needed: 三种宿主的正常公开流程都只向 `.gates` 读写 gate 运行证据；各宿主仍从自身要求的安装和 hook 目录加载。

## RQ-073 - 正式流程必须使用 VCS

Requirement or question: 没有 VCS 的工作区是否可以进入正式开发、四门、Carry 和 seal？
Source: 用户于 2026-07-20 提出“或者不支持无 vcs”，并在收到建议后回复“可以不支持无 vcs”。
Why it matters: 无 VCS 却要产生可验证的修改前后 diff，必然引入全量备份或修改前登记、扫描、哈希、漏登记处理和清理，实际上是再做一套简化版本管理，与 Phase 3 的核心目标不成比例。
Status: confirmed
User answer: Phase 3 不支持无 VCS 的正式开发、四门、Carry 和 seal 流程。必须先有可生成完整交付 diff 的 Git、SVN、P4 或等价 VCS；formal-gates 不调用这些 VCS、不内建多后端适配器或 Git 特例，也不提供备份或 best-effort 降级。普通文档、安装和非正式校验能力不受影响。
Downstream effect: 正式 handoff 通过 `--vcs` 拒绝空值和 `none`。
Document impact: alignment、proposal、design、Phase 3 tasks、CLI、worker handoff 和测试。
Evidence needed: 无 VCS 的正式入口在开发前明确拒绝；VCS 正常公开流程由 worker 和 reviewer 直接比较外部 snapshot。

## RQ-075 - 全仓库过时、冗余和重复真值收敛

Requirement or question: Phase 3 是否应在既定开发之外，同时检索并分析整个项目的代码和文档，删除过时或无行为价值的内容、更新与当前行为冲突的说明，并把同一功能的多处独立维护逻辑收敛为一个权威 owner？
Source: 用户于 2026-07-20 在确认 Phase 3 完整开发内容后，明确要求新增该项并与 Phase 3 一起处理。
Why it matters: 只增加新行为而不清理旧口径、重复常量、重复验证和冲突文档，会让同一规则继续分散演化，并在下一次修改时重新不一致。该收敛必须基于实际重复或过时证据，不得以优雅为名扩张产品范围。
Status: confirmed
User answer: Phase 3 对整个当前仓库执行一次代码和文档收敛审计，与当期开发一起处理已证实过时、冗余、与当前行为冲突或重复维护同一真值的内容。同一功能、路径规则、schema/policy 投影、校验规则或文档合同只保留一个权威 owner，其他位置调用、生成或引用该 owner。新 workflow 统一使用 `.gates` 和现场 VCS snapshot。宿主必须分开的安装配置、有独立行为的平台分支、最低层直接测试和仅形式相似但责任不同的逻辑不得为追求更少文件而强行合并。若最小收敛会改变已确认的公开行为或扩大范围，必须返回需求澄清，不得自动重设计。
Downstream effect: 开发前产生全仓库重复 owner、过时口径和文档冲突清单；实现阶段先复用或建立唯一 owner，再删除其他独立实现和过时文档。开发前复杂度评估和开发后 Complexity Gate 必须核对该清理是实际减少真值和逻辑，而不是把重复隐藏在新框架中。
Document impact: alignment、proposal、design、tasks、所有受影响 spec、SKILL、README、agents、references、manifest、CLI、validator、installer/canary、examples 和测试。
Evidence needed: 仓库级搜索和 ownership 检查没有遗留被取代词汇或可达旧路径；每个合并点能指向唯一 owner 和现有正常公开行为覆盖；删除后格式、单测、race、vet、行为、打包、canary、严格 OpenSpec 和支持平台路径测试通过；不要求对具有不同职责的正常分支做无价值合并。

## RQ-076 - 交付文件必须先纳入现场 VCS

Requirement or question: worker 新建交付文件，或要修改、删除原本未跟踪但属于交付的文件时，应如何保证开发 diff 和后续返修 diff 都能由现场 VCS 区分？
Source: Phase 3 OpenSpec 映射与全仓库收敛审计于 2026-07-20 发现该正常入口缺口。
Why it matters: 对已经存在但未进入 VCS 的文件，若先修改或删除、后报名路径，VCS 无法恢复修改前内容。
Status: confirmed
User answer: worker 创建交付文件后立即加入现场 VCS；修改或删除原本未跟踪的交付文件前先加入现场 VCS，并由外部 VCS 保存后续比较所需的状态。只能加入明确的本次交付路径，不能执行 `git add .`、`git add -A` 或等价全量操作。返修前后状态由外部 VCS 原生 snapshot 区分；无法可靠保存或比较时 Carry 不可用，受影响门不能在没有 terminal `RERUN_REQUIRED` 的情况下进入新 snapshot 重跑。worker 返回前，所有交付路径都必须已被 VCS 跟踪且出现在完整交付 diff 中，无关未跟踪文件保持不变。
Downstream effect: changed-files composition 继续只处理路径标量；worker 提示词负责先跟踪、再修改或删除以及返回前核对，formal-gates 不保存项目内容或版本历史。
Document impact: alignment、proposal、design、Phase 3 tasks、development worker、SKILL、complexity/reference、CLI 和直接行为测试。
Evidence needed: 正常 worker 流程中新增、原本未跟踪后修改或删除的交付路径均先进入现场 VCS，完整 diff 覆盖全部交付路径；全工作区 add 和返回时残留交付 untracked 被拒绝或停止，formal-gates 无内容捕获入口。

## RQ-078 - 审查必须查完整相关问题链

Requirement or question: 任一道会产出审查结论的门发现一个问题后，是否必须继续调查本次需求引入的同类问题，以及同一因果、行为、数据、依赖或架构链上的其他问题，并在同一轮一次性报告？
Source: 用户于 2026-07-22 明确要求：不只复杂度门，每一道门都不能只报冰山一角导致不断返修。
Why it matters: 只完成固定检查项或发现第一个 blocker，并不能保证同一变更的其他受影响位置已被检查；问题会在下一轮逐个暴露，造成重复返修和不完整的门结论。
Status: confirmed
User answer: 每一道会产出问题判断的门，在发现 blocker、失败、concern 或 decision gap 后，必须在该门允许读取的需求和变更范围内横向查找同类问题，纵向追踪同一条相关链，直到没有新的本次变更相关问题；同一根因的多个表现合并为一个 finding 并列出全部受影响位置或用例，所有独立问题在同一结果中报告。不得扩展到无关历史问题，也不得越过其他门的职责；QA Execution 不得临时发明未批准用例，Carry Arbiter 仍只负责逐门判断是否可继承既有 PASS。
Downstream effect: 所有审查 agent 提示词和总入口统一包含相关问题链完整性规则；每轮结果、finding 合并和剩余风险说明必须符合该规则。复杂度门仍保持方案层先完整扫描，方案失败后不审依赖该方案的代码细节；这不增加 CLI 字段、审查角色、状态或额外审查系统。
Document impact: alignment、proposal、design、tasks、reviewer-receipt-and-isolation spec、SKILL、requirements/QA/complexity/architecture/code-quality/cold-water agent prompts 和直接提示词校验。
Evidence needed: 正常公开需求和变更输入下，每个审查角色都能在同一结果中报告同类实例及同链后果；同根因位置被合并但不漏列；无关历史问题、其他门职责和未批准 QA 用例不被纳入；方案失败时方案层问题完整、依赖代码检查明确阻塞。

## RQ-079 - Native 安装默认配置宿主 hook

Requirement or question: `formal-gates install` 安装选定宿主时，hook 应默认配置还是要求用户额外选择？
Source: 用户于 2026-07-23 明确确认：省略 hook 相关参数时同时安装 runtime/skill 和宿主 native hooks；跳过 hooks 必须显式使用 `--skip-hooks`。
Why it matters: 默认只复制 runtime 会形成看似完整但缺少宿主 hook 的正常安装，且旧 `--configure-hooks` 把完整安装错误地表现为额外 opt-in。
Status: confirmed
User answer: Native installer 对 `claude`、`codex`、`cursor` 以及 `both` 选择的每个目标，在 global/project scope 默认复制 runtime/skill 并配置该宿主真实支持的 native hooks。只有显式 `--skip-hooks` 才跳过 hook 写入且保持既有 hook config 不变。删除公开 `--configure-hooks` flag，不保留兼容别名。默认 merge 必须保留非 formal-gates hook；hook 配置失败时整个 install 命令失败且不得输出成功安装声明。
Downstream effect: 复用现有 installer 和 hook merge owner，只反转默认选项并更新 CLI、bootstrap、manifest、canary、测试和安装说明；不新增第二个 installer、hook schema 或兼容路径。
Document impact: alignment、proposal、design、Phase 3 tasks、reviewer receipt/isolation spec、README 中英文、install-and-hooks reference、CLI、bootstrap、manifest、installer、portable canary 和测试。
Evidence needed: 各支持宿主和 scope 的默认安装配置其真实支持的 hooks；`--skip-hooks` 只安装 runtime 且不改 hook config；非 formal-gates hook 保留；旧 flag 被拒绝；hook config 错误使公开命令失败且没有成功声明。

## RQ-080 - Codex 不提供子代理生命周期事件时的回执规则

Requirement or question: Codex 没有可用的 `SubagentStart` / `SubagentStop` 事件时，是否阻止开发、审查或回执收口？
Source: 用户于 2026-07-23 明确确认：Codex 确实不提供这些事件，不能因此不让开发。
Why it matters: 要求宿主不存在的事件会让正常 Codex 工作流永久无法收口，并诱导安装无效 hook 或伪造事件。
Status: confirmed
User answer: Codex 默认安装仍写入现有 formal-gates `PreToolUse`、`SubagentStart`、`SubagentStop` hooks，但 receipt register/finalize/validate/closure 不要求 Codex 实际产生 start/stop 事件。Codex 的精确 prompt 绑定、CLI semantic submission/proof、artifact/hash、dispatch 和 closure 等非生命周期校验全部保留。Claude Code 和 Cursor 等实际支持生命周期事件的 provider 继续要求真实 start/stop。不得增加替代 agent tracking、session manager、事件模拟、兼容别名或手工捕获 fallback，也不改变既有 hook 安装和 merge 行为。
Downstream effect: 只在现有 installer、receipt finalize/validate、closure 和 preflight owner 中增加 provider capability 分支；不增加新系统或新状态。
Document impact: alignment、proposal、design、tasks、reviewer receipt/QA Design specs、README 中英文、SKILL、install-and-hooks reference、manifest、example、CLI/validator/tests。
Evidence needed: Codex 零 lifecycle event 可完成并验证 receipt；所有非生命周期错配仍拒绝；Claude Code/Cursor 缺失或错配 lifecycle 仍拒绝；Codex 默认安装仍写入完整既有 hook 集合，且既有 hook merge 行为不变。

Open questions: none
Blocking gaps: none for the current decisions in RQ-069, RQ-070, RQ-072, RQ-073, RQ-075, RQ-076, RQ-078, RQ-079, and RQ-080.
Downstream permission: Phase 0 已封板，Phase 1 已完成并推送，Phase 2 已封板于提交 `a3c28a6`，Phase 2.5 已封板并提交于 `fe72689`。Phase 3 已获准正式起草并进入开发。
