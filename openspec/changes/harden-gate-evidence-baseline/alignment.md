# 需求对齐

日期：2026-07-12
状态：RQ-039 至 RQ-041、RQ-043 至 RQ-044、RQ-046 至 RQ-058 已确认；RQ-042 已被 RQ-047 取代并撤回，RQ-045 已被 RQ-049 取代并撤回；RQ-008、RQ-013、RQ-024、RQ-031、RQ-034 已按用户决定删除；RQ-015 和重复提出的 RQ-022 已撤回

本文档保存了对本地 diff、开发计划以及曾报告 PASS 的四门证据进行严格审查后达成的决策。它是本次 OpenSpec 变更的需求来源。

## Phase 0 - 审查收敛

用户已确认先完成审查收敛，再开发机器证据功能。只有当前变更造成，并有证据证明其违反已确认需求、可观察行为、现有门的职责或强制规则的问题，才能影响结论。范围内的真实缺陷直接返修；最小修法会改变已批准范围时交给用户决定，不得自动扩张。措辞、命名、格式、等价方案偏好、纯假设风险和未要求的加固只是建议；只剩建议必须 PASS。

Reviewer 的 finding 不直接变成需求或返修任务。主代理先去掉无证据、重复、偏好型和超范围意见；同一根因换一种说法仍算一个问题。只有确实会改变范围、验收、架构边界、公开行为或其他用户决定的问题才进入需求澄清。一次交付最多自动完成三轮 review-repair。只有独立 reviewer 返回完整正式结果后才开始一轮；主代理处理完该结果、完成已接受的范围内返修并做完必要复验后，才记为一轮。一次正式结果及其返修、复验合计一轮，不能拆开重复计数。开发自检修改、派发失败、执行中断以及没有完整正式结果的尝试都不计轮次。完成三轮后停止自动审查和返修，只提交合并后的真实 blocker、证据、已尝试修法和仍未解决的原因；由用户决定修改范围或需求、延期或接受风险、明确批准再开一轮，或者停止交付。本阶段只修改现有规则、agent、reference、行为用例和阶段说明，不新增产品代码、字段、命令、角色、状态机或文件。

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
User answer: 主代理可以提议重跑边界，但最终定稿时，任何继承的 PASS 都必须由新的零对话上下文 Carry-Forward Arbiter 裁决。复杂返修链必须完整展开供其审查。
Downstream effect: 不能仅凭主代理的 transition 记录推进继承。
Document impact: 继承与最终定稿规范。
Evidence needed: Arbiter 产物、匹配的 receipt、完整展开的 transition 链和逐门决策。

## RQ-003 - Reviewer 独立运行的 receipt 证据

Requirement or question: 如何证明正式 reviewer 或 Arbiter 确实独立运行过？
Source: 用户于 2026-07-10 确认的对齐结果。
Why it matters: 主代理自报的 reviewer ID 或非空 dispatch ID 不能证明独立 reviewer 实际运行并产出对应结果。
Status: confirmed
User answer: 正式四门 reviewer 和 Carry-Forward Arbiter 必须复用现有 receipt 链，绑定 dispatch registration、subagent start/stop、subagent ID、输出哈希、workflow、gate、stage 和 snapshot；自报身份不能支持 PASS。按照后续 RQ-027，普通 CLI receipt 只证明这些记录相互一致，不声称能抵御控制本地文件和 CLI 的操作者。Host hook 只有通过 same-host live canary 后，才能额外声称生命周期由宿主自动捕获。
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
Downstream effect: 产品只保留有实际行为价值的阶段、验证和四门顺序，不增加进度记录专用机制。
Document impact: proposal、design 和 tasks 只保留实际交付要求。
Evidence needed: 产品文档和实现中不存在非权威进度记录专用机制。

## RQ-010 - reviewer 落盘材料隔离

Requirement or question: 如何隔离敏感流程文本文件，同时不向 gate reviewer 隐藏正常的当前项目信息？
Source: 用户于 2026-07-10 确认的对齐结果。
Why it matters: 之前的重跑 bundle 写出了修复过的 blocker，给 reviewer 造成偏置；而严格的文件白名单会降低审查完整性。
Status: confirmed
User answer: 将敏感流程文本放在固定路径 `.claude/gates/runs/<workflow-id>/restricted/` 下，包括此前的 verdict 和 finding、repair note、rerun 或 transition decision、carry 和 Arbiter 输入输出、主代理总结、聊天记录，以及旧 dispatch 或 context bundle。四门 reviewer 可以查看全局黑名单 `.claude/gates/runs/*/restricted/**` 以外、与任务相关的所有当前仓库材料，但不得直接读取或通过传递引用读取任何黑名单路径。当前原始证据和其他中性材料仍放在 `restricted/` 之外。RQ-010 只管理落盘文本，不决定如何验证直接编写的 dispatch prompt 文本。
Downstream effect: 四门 reviewer 的边界是跨所有 workflow run 的固定路径黑名单，而不是仓库白名单。直接或传递读取任一 `restricted/` 目录都会使审查失效，并要求更换 reviewer。
Document impact: Reviewer 证明与隔离规范。
Evidence needed: 确定性的布局检查、递归引用拒绝、禁读路径访问测试，以及 RQ-016 已确认的 receipt 一致性和 host 能力声明。

## RQ-011 - 类型化 Arbiter verdict

Requirement or question: Carry-Forward Arbiter 必须作出什么决策？
Source: 用户于 2026-07-10 确认的对齐结果。
Why it matters: 通用的最终 PASS 无法表明审查了哪些继承门，也无法表明被拒绝的链路应从哪里重跑。
Status: confirmed
User answer: Arbiter 对每个继承门分别作出 `ACCEPT_CARRY`、`RERUN_REQUIRED` 或 `BLOCKED` 决策。拒绝时要指出最早需要重跑的 gate。主代理不能覆盖该结果。
Downstream effect: 除非所有继承门都被接受，否则阻断最终 seal；重跑从最早被拒绝的 gate 开始，并在目标 snapshot 上继续运行所有下游 gate。
Document impact: 继承与最终定稿规范。
Evidence needed: 逐门 Arbiter 决策和机器推导的汇总结果。

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

`RQ-015` 经用户批准撤回，因为它与 RQ-010 重复。其 ID 继续保留，不得再次使用。已批准的当前需求仍可在固定 `restricted/` 路径之外读取。

## RQ-016 - Reviewer 新鲜度与禁读路径

Requirement or question: 如何复用现有 receipt 判断 reviewer 属于本次审查，并检查其正式输入没有读取 `restricted/` 路径？
Source: 由独立的零对话上下文文档审查于 2026-07-10 提出。
Why it matters: 仅有 reviewer 自报不能把结果绑定到本次 workflow、gate、snapshot 和实际输出，也不能检查正式输入是否引用了禁读材料。
Status: confirmed
User answer: 补强现有 receipt，不另起一套零上下文机制。保留现有 dispatch registration、subagent start/stop、subagent ID 和 reviewer 产物哈希链；正式四门 PASS 必须提供与当前 workflow、role、gate、stage、snapshot、dispatch、reviewer session 和输出一致的 receipt。正式输入及其传递引用不得进入 `.claude/gates/runs/*/restricted/**`，路径判断覆盖规范化路径和符号链接；Carry-Forward Arbiter 仍可读取完整返修链。普通 CLI 可以正式 PASS，但只能证明 receipt、输入和产物的一致性，不得声称能抵御控制本地文件和 CLI 的操作者，也不得声称知道 reviewer 在受控输入之外实际读取了什么。Host hook 只有在 live canary 成功时，才能额外声称生命周期或文件访问由宿主自动捕获。
Downstream effect: 复用并收紧现有 receipt 和路径校验；能力声明区分普通 CLI 一致性检查与经过 live canary 证明的 host 自动捕获，不新增 verifier、provider、trust root 或 nonce 机制。
Document impact: receipt 校验、隔离规则、host 能力声明和 canary。
Evidence needed: 现有 receipt 正向用例；workflow、gate、stage、snapshot、session、输出或路径不匹配的失败测试；host 自动捕获声明必须绑定成功的 live canary。

## RQ-017 - 证据闭包图与哈希契约

Requirement or question: 应以什么样的逐记录与最终汇总闭包模型、图边、canonical encoding 和 cycle behavior 作为规范？
Source: 由独立的零对话上下文文档审查于 2026-07-10 提出。
Why it matters: 除非各实现就哪些字段建立边，以及如何在各平台上得到同一图哈希达成一致，否则“所有传递证据”这个说法并不充分。
Status: confirmed
User answer: 使用最小的逐门证据闭包，不增加额外的最终汇总根。按照后续 RQ-040，每条正式 PASS 的 closure manifest 包含 reviewer 输出、receipt 和该门实际使用的全部下层证据，gate record 只绑定一个 closure root。Receipt 在 closure 内引用 reviewer 输出哈希；reviewer 输出不反向引用 receipt 或 closure，因此没有自引用。后续 gate 准入和最终交付时逐门重新验证。只有结构化证据字段中的显式引用和 gate 记录要求的 receipt 形成依赖边，不从 Markdown 文本或关键词中猜测引用。每个引用必须是 workflow run 内的规范化相对路径并带哈希；缺失、篡改、路径逃逸、冲突别名或引用环都拒绝。闭包 manifest 使用版本化的 typed Go 结构和固定排序生成确定性 JSON，以 golden vector 锁定跨平台结果。
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

## RQ-019 - 继承裁决顺序

Requirement or question: 当下游 gate 依赖提议继承的前置 gate 时，应在什么时点进行继承裁决？
Source: 于 2026-07-10 的对齐审查期间提出。
Why it matters: 如果只在下游 gate 被正式准入后才判断继承，这些 gate 可能依赖从未被有效继承的前置结果；如果过于频繁地重复裁决，又会失去节省 token 的意义。
Status: confirmed
User answer: 实现修改完成并确定目标 snapshot 后，主代理先提出继承范围；在运行第一个依赖继承结果的下游 gate 之前，调用一次新的 Carry-Forward Arbiter。Arbiter 接受后，先正式准入被继承的 gate，再按固定顺序运行必须重跑的 gate。只要后续没有改变目标 snapshot，最终交付直接复用这次 Arbiter 决策，不再次调用模型。若后续 gate 引发代码或其他可交付内容修改并产生新 snapshot，旧裁决失效，必须针对新 snapshot 重新裁决。主代理不能自行批准继承，复杂返修链仍须完整展开。
Downstream effect: 任何 fresh 下游 gate 都只依赖已经由 Arbiter 接受的继承前置条件；通常每个稳定 snapshot 只调用一次 Arbiter，同时避免最终阶段重复裁决。
Document impact: 继承最终定稿 design、admission rule 和 rerun sequencing。
Evidence needed: fresh-only、下游准入前接受或拒绝继承、snapshot 不变时复用裁决、snapshot 改变时裁决失效，以及 multi-hop repair chain 的正反向流程。

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

## RQ-022 - 当前需求理由与 reviewer 隔离边界

Requirement or question: 已由用户确认的当前需求文档，能否向 reviewer 展示解释需求原因的中性事实，即使这些事实源于过去暴露的问题？
Source: 由针对 `files.45473e1a6d81` 的新鲜零对话上下文 complexity 开发前审查提出。
Why it matters: proposal、design 和 alignment 需要说明需求为何存在，但四门 reviewer 又不得接触旧 verdict、finding、repair note、期望结论或定向关注材料。如果不区分“当前已批准的需求理由”和“历史审查材料”，当前需求文档本身可能触发污染规则。
Status: withdrawn
User answer: 用户指出该边界此前已经确认，不应作为新问题重复询问。经核对，RQ-010 已允许 reviewer 阅读当前需求和中性当前材料，现有 prompt 规则也已允许中性需求、验收标准和范围，同时禁止旧 finding、repair explanation、目标结论和 directed focus；因此本项不产生新需求。
Downstream effect: 保留 RQ-022 编号和撤回原因；按现有规则把 proposal/design 改成中性当前问题陈述，并修正 reviewer isolation spec 的过宽措辞。
Document impact: proposal、design 和 reviewer receipt/isolation spec 的既有规则修正。
Evidence needed: 文档审查确认当前批准需求和验收标准仍可读取，而历史 verdict、逐条 finding、repair narrative、目标结论和 directed focus 仍被禁止。

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

Requirement or question: 最终四门的 snapshot 和执行顺序是否应依赖非权威进度状态？
Source: 针对 `files.3cbe97e3e99c` 的零对话上下文 complexity 开发前审查提出。
Why it matters: 如果最终四门依赖进度勾选，非权威状态会错误地改变 snapshot 或制造循环顺序。
Status: confirmed
User answer: 不依赖。每个实现阶段在完成实际可交付内容后固定 snapshot，再按正式顺序运行四门和 finalization；非权威进度状态不构成前置条件或证据。
Downstream effect: 最终顺序只由可交付 snapshot、验证和 gate 证据决定。
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
Evidence needed: 所有 CLI 的现有 receipt 正向流程，以及正常使用中可能出现的 lifecycle、ID、hash、workflow/gate/stage/snapshot 不匹配；hook 能力只在 live canary 成功时声称。成套恶意篡改和手工改写内部状态不属于验收范围。

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

Requirement or question: 开发前文档是否需要固定 schema 版本、字段与类型、unknown/duplicate field 行为、canonical JSON bytes、SHA-256 编码及跨平台逻辑路径语法？
Source: 针对 `files.5c28b5a5ac58` 的零对话上下文 architecture 开发前审查提出。
Why it matters: 只写“typed/deterministic JSON”会让不同实现产生不同字节和哈希；写死全部 wire contract 又增加文档和实现约束。
Status: confirmed
User answer: 为正式证据规定一个由现有 Go CLI 权威解析和校验的严格 JSON 合同。按照后续 RQ-050 和 RQ-052，reviewer 和 requirements owner 直接写自己的语义 JSON；每种证据固定字段和类型，拒绝未知字段、重复字段和错误类型，但不把 JSON key 顺序当作输入准入条件。只有 receipt、closure、state 和 mechanical finalization 等 CLI 自己生成的机械材料，才使用标准库和 typed Go struct 产生固定输出顺序与确定性字节，并以少量 golden test 锁定。证据路径统一为 workflow 内使用 `/` 的相对路径，拒绝绝对路径、`..` 和符号链接逃逸，并覆盖 Windows 与 macOS/Linux 路径样本。不要求第二套独立实现，不新增通用 canonical JSON 库、跨语言协议或额外抽象。JSON 解码、字段校验、路径归一化、哈希和证据关系检查全部由现有 Go CLI 静态执行，不消耗 AI token；AI 只处理代码、需求和 reviewer 判断等机器不能替代的语义审查。
Downstream effect: 收紧现有 JSON decoder 和 domain validator，由 CLI 输出确定性的成功或失败结果；语义 JSON 的等价 key 顺序都能进入同一类型化校验，CLI 拥有的机械输出仍保持确定性字节。正式 reviewer 不再承担手工解析大段 JSON 的工作，但静态校验不能代替四门的语义判断。
Document impact: design、structured JSON spec、evidence closure spec 和 Phase 1 JSON/closure tasks。
Evidence needed: CLI 拥有的机械 JSON 输出有固定 bytes/hash golden vector；语义 JSON 的不同 key 顺序都被接受；未知、重复、错类型字段和非法路径拒绝用例；Windows 与 macOS/Linux 路径样本；AI handoff 只消费 CLI 结果和语义审查所需材料，不要求手工读取完整机器证据 JSON。

## RQ-033 - 四门专属机器 judgment 字段

Requirement or question: RQ-020 的“所有正式 PASS 机器字段统一 JSON”是否包括 QA、complexity、architecture、code-quality 各自目前会影响 PASS 的专属 judgment 字段？
Source: 针对 `files.0b657a81a197` 的零对话上下文 complexity 开发前审查提出；用户于 2026-07-11 在 SARIF、GitHub Checks、SonarQube、Semgrep 和 OPA 的实现调研后确认。
Why it matters: 若 reviewer payload 只有 dispatch/receipt，没有各门专属判断结果，validator 只能继续读取 Markdown 或放弃现有校验。
Status: confirmed
User answer: 四门使用共享的严格 envelope、统一的 `checks[]` 结果和少量真正必要的专属证据引用，不为每个审查维度新增顶层 JSON 字段。每个 check 包含稳定 `id`、`status`、只用于说明的 `message`、`evidenceRefs` 和必要的 `findings`/`locations`；check ID 来自现有 Go policy catalog。QA 仅额外保留 approved case set、case-to-artifact binding 和 QA-owned evidence；complexity 仅额外保留新鲜 statistics-only script result；四门共用 changed files、verification 和 context bundle。Receipt 仍是四门必需证据，但按照 RQ-040 由 gate closure 绑定，不放进 reviewer payload。requirements、Carry-Forward Arbiter 和 FinalExecution 的操作数据形状确实不同，继续使用各自 typed payload。不得引入完整 SARIF、OPA、Sonar 指标引擎、自由 `properties` 扩展袋或新的通用 judgment 框架。
Downstream effect: CLI 拒绝缺失、未知或重复 check ID 和未知状态；`NOT_APPLICABLE` 只允许 policy 明确批准的 check 并要求理由；任一 `REVIEW`、`FAIL` 或 `BLOCKED` 都不能聚合为 PASS；只有所有必查项恰好出现一次并为 `PASS` 或允许的 `NOT_APPLICABLE`，且证据闭包和门禁前置条件有效时，才接受顶层 PASS。删除当前草案中 architecture、code-quality 和 complexity 不断扩张的专属 judgment 顶层字段，以及不能由 CLI 重算或 receipt 证明的 reviewer 自报布尔字段。proposal、design、structured JSON spec、四门 agent/reference、Go validator、测试、canary 和示例必须一次改成同一口径，不保留旧字段、旧 Markdown 准入或兼容解析。
Document impact: structured JSON spec、design、agent/reference 模板和 Phase 1 reviewer migration tasks。
Evidence needed: 每门必查 check catalog、合法聚合、缺失/未知/重复 ID、非法状态、非法 `NOT_APPLICABLE` 和顶层 verdict 不一致用例；repo-wide 检查确认旧专属 judgment 字段不再承担机器合同，旧 Markdown 字段校验和兼容路径已删除。

## RQ-035 - QA Design/Design Review 的 JSON 角色表示

Requirement or question: 已确认的 QA `Design`、独立 `Design Review`、必要时 `Design Rework` 和 `White-box Adequacy` 应如何进入统一 JSON evidence 体系？
Source: 针对 `files.e2676cb77217` 的零对话上下文 complexity 开发前审查提出。
Why it matters: 当前封闭 schema 只允许 `Execution`/`FinalExecution`，但 QA admission 又要求 hash-bound Design 和独立 Design Review；实现者会被迫现场发明 stage/role。
Status: confirmed
User answer: 不把每个 QA 动作都升级成独立 JSON role 或 gate。`Design` 只生成 case set 并由现有 designer receipt 绑定，不记录 PASS；`Design Review` 复用统一 reviewer envelope 和 `checks[]`，绑定被审 case set 的哈希、独立 reviewer receipt 和最终接受的 case set；`Design Rework` 不设独立机器 role，case 修改后哈希变化使旧 review 失效，必须重新进行 Design Review。`Execution` 继续使用现有 `qa-test-gate` 并绑定 approved case、QA-owned evidence 和 case-to-result 对应关系；`FinalExecution` 继续作为机械收尾，不新增 QA 判断；`White-box Adequacy` 仅在实际需要时运行，并复用同一 QA reviewer payload 和 `checks[]`，不新增 gate 或专用框架。
Downstream effect: QA admission 只验证 case set、designer receipt、独立 Design Review、approved case hash 和 development handoff 的绑定；删除为 Design Rework 或每个 QA 阶段新增独立 struct、validator、状态迁移和 role 的方案。Case hash 变化自动要求重新 review，静态记录与真实开发顺序一致。
Document impact: structured JSON spec、QA design admission spec、design 和 Phase 2 QA tasks。
Evidence needed: case set 与 designer receipt 绑定、独立 Design Review 接受、handoff 使用准确 approved case hash、case 修改使旧 review 失效、Execution 使用批准链路、可选 White-box Adequacy 复用 QA reviewer payload，以及禁止新增 Design Rework 机器 role 的文档和实现检查。

## RQ-036 - Reviewer 必须看到当前已确认的用户决策

Requirement or question: 所有 formal reviewer 是否必须收到与本次审查相关的当前已确认用户决策，并不得把已定决策当作待选问题重新提出？
Source: 用户于 2026-07-11 要求检查，经现有 agent、zero-context 和 reviewer-isolation 文档对照发现。
Why it matters: 当前规则允许 reviewer 读取 current approved requirements、acceptance criteria 和 `User request and acceptance criteria`，但没有要求 context bundle 覆盖所有相关已确认决策。Reviewer 因此可能重复质疑用户已经选定的方向。直接提供包含旧 finding 来源和 repair 历史的 alignment 又可能造成锚定污染。
Status: confirmed
User answer: 所有 formal reviewer 必须看到与当前审查相关的全部已确认用户决策，否则可能重复提出已决问题。复用现有 `Context bundle` 和 `User request and acceptance criteria` 字段，要求它们引用已吸收这些决策的当前批准需求文档；不新增字段、artifact 类型或状态机。Reviewer 可以指出决策冲突、文档遗漏或实现不符，但不得仅因为自己偏好不同就重新打开已确认决策。
Downstream effect: 派工前必须检查现有需求文档已覆盖所有相关确认决策；遗漏会影响判断的决策时，reviewer 返回 BLOCKED，不出具完整结论。
Document impact: 所有 formal reviewer agent 的现有 prompt 字段说明、zero-context handoff 指引、reviewer isolation spec、Document Readiness 和 Phase 2 reviewer-context tasks。
Evidence needed: 一个当前需求 bundle 覆盖已确认决策的正向用例，以及遗漏影响审查的已确认决策时不得出具完整结论的用例。

## RQ-037 - 开发期复杂度预算与事后复杂度门隔离

Requirement or question: 开发期的数字预算、预算检查和反复杂度批准，是否必须从事后 `complexity-gate` 的 reviewer 输入和 PASS artifact 中隔离？
Source: 用户于 2026-07-11 要求检查，经对照 `artifact.go`、complexity agent/reference 和负向测试发现。
Why it matters: 事后复杂度门应独立判断当前实现是否最小足够。如果 reviewer 看到“开发期预算已通过”或扩容批准，容易把过程合规错当成复杂度合理。
Status: confirmed
User answer: 开发期的 handoff、数字预算报告、预算扩容申请和反复杂度批准必须与事后 `complexity-gate` 隔离。复用现有 `restricted/` 黑名单和 evidence reference 校验，不新增隔离系统或关键词扫描器。事后 reviewer 只接受对当前 diff 重新生成的 statistics-only JSON 报告；其 `budget` 必须缺失、`budget_source` 必须是 `none`、所有 budget override 必须是 `false`。
Downstream effect: 删除“换个字段便可把预算历史带入事后 artifact”的口子；事后 verdict 只根据需求、当前 diff、QA 证据和无预算统计报告判断。
Document impact: complexity agent/reference、reviewer 隔离规范、structured complexity payload 和对应 validator 测试。
Evidence needed: 事后 complexity 只接受新鲜的 statistics-only 报告，其 JSON 中没有 budget 且 `budget_source=none`；直接或递归引用开发期预算材料时拒绝事后 complexity PASS。

## RQ-038 - OpenSpec 分阶段实现

Requirement or question: 本次 OpenSpec 是否拆成多个独立实现阶段，而不是将已有 diff 修复和所有后续 hardening 挤进一个大 diff？
Source: 用户于 2026-07-11 明确指定。
Why it matters: 一次实现所有 capability 会扩大 diff、混淆问题修复与新机制，也会使测试、回滚和四门判断变得困难。
Status: confirmed
User answer: OpenSpec 必须分阶段实现，不得把全部 capability 挤进一次开发。按照后续 RQ-047 的最终排法，Phase 1 是统一 JSON cutover 并吸收当前 requirements/policy 修复，Phase 2 是 review chain，Phase 3 是 convergence；每阶段都必须独立实现、验证和审查。当前开发前审查只准入即将交接的阶段；后续阶段的合同必须在自己交接前基于当时的 snapshot 补齐并重新通过开发前审查，但未就绪的后续阶段不阻断更早阶段。
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
User answer: 使用一个逐门 closure root。Reviewer 先产生不含 receipt 或 closure 反向引用的完整 JSON 输出；CLI 对其精确字节计算哈希，再生成绑定该哈希的 receipt；closure manifest 包含 reviewer 输出、receipt 和其他全部下层证据。Gate record 只保存 closure manifest 的一个根哈希。Reviewer 输出哈希只是 receipt 和 closure 内部用于绑定具体文件的普通条目，不是第二个顶层根。不要定义“算哈希时忽略某字段”的特殊规则。
Downstream effect: RQ-017 中“gate record 单独保存 reviewer 输出哈希”的部分由本决定取代。Reviewer payload 删除 receipt 和 closure 反向引用；gate closure 成为唯一逐门完整性根。
Document impact: design 的 evidence/receipt 顺序、structured JSON spec、reviewer receipt spec、closure spec 和新的 Phase 1 tasks。
Evidence needed: 明确的单向生成顺序和一个逐门 closure root；reviewer 输出、receipt 或任一下层证据篡改都会失败；不存在字段排除式 canonical hash、自引用或额外顶层哈希 fixture。

## RQ-041 - Role 与领域校验同阶段启用

Requirement or question: 分阶段迁移 JSON 时，一个 role 或 stage 的结构、领域 validator、policy 和测试是否必须在同一阶段完成？
Source: 新鲜零对话上下文 complexity 开发前审查于 2026-07-11 对当前批准 bundle 提出。
Why it matters: JSON 基础阶段不能提前定义后续阶段才会实现的 QA Design Review、White-box Adequacy 或 Carry role。
Status: confirmed
User answer: 只修改任务分组，不为开发中间态增加产品逻辑。按照 RQ-047 更新后的编号，Phase 1 完整交付 requirements、QA Execution、complexity、architecture、code-quality 和 mechanical FinalExecution 的统一 JSON 格式、validator、policy 与测试，并删除 Carry Arbiter、Design Review 和 White-box Adequacy 的提前占位。Phase 2 实现 Design Review、White-box Adequacy 或 Carry Arbiter 时，在各自任务中同时完成 JSON 内容、validator、policy 和正反向测试。统一 envelope 不变。
Downstream effect: 每个业务功能在哪个阶段开发，就在该阶段从结构到语义和测试完整交付。不要增加 disabled role、临时错误码、占位 payload、兼容路径或其他中间态产品行为。
Document impact: proposal delivery phases、design role activation、structured JSON spec 和新的 Phase 1/2 tasks。
Evidence needed: 每个 role/stage 的结构、policy、validator 和测试属于同一阶段；阶段任务不存在结构与语义分离，也没有为开发中间态新增产品规则。

## RQ-042 - Phase 1 的验收状态与 JSON 切换边界

Requirement or question: Phase 1 只修当前 diff 已暴露的 requirements PASS 和 policy 行为时，应如何验收并复用到 Phase 2，才能既不提前拉入全量 JSON 切换，也不写一套马上被删除的临时代码？
Source: 第二轮新鲜零对话上下文 complexity 开发前审查于 2026-07-11 对当前批准 bundle 提出。
Why it matters: 最终 requirements capability 只接受 typed JSON，但 JSON 全面切换属于 Phase 2。当前 Phase 1 tasks 又要求完成 requirements PASS parity。若直接继续堆 Markdown 专用校验，Phase 2 会删除它；若 Phase 1 提前切 requirements JSON，则会产生混合格式或暗中吸收 Phase 2 范围。
Status: withdrawn
User answer: 本项原先定义独立的当前格式修复阶段；用户随后以 RQ-047 明确取消该阶段，并把 requirements PASS 与 typed policy 修复并入统一 JSON 的新 Phase 1。旧方案不再保留为可选或兼容路径。
Downstream effect: 用户随后通过 RQ-047 改为把当前 diff 的 requirements/policy 修复并入统一 JSON 切换阶段，因此本项不再是现行阶段合同，也不保留当前格式的独立交付阶段。
Document impact: 由 RQ-047 统一取代。
Evidence needed: 由 RQ-047 的新 Phase 1 验收证据取代。

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
User answer: 不提前定义。按照 RQ-047 更新后的编号和 RQ-059 的最终分工，Phase 1 只把当前已启用的 QA Execution 及其 approved case set、QA-owned results、case-result binding、changed files 和 verification 迁入专用机械 payload。Design Review 和可选 White-box Adequacy 都在 Phase 2 各自与 JSON 内容、policy、validator 和正反向测试同时完成；Phase 1 不注册 disabled stage，也不保留占位字段。
Downstream effect: Phase 1 QA 合同可以独立实现和验收；Phase 2 增加真实业务能力时再扩展对应 stage，统一 envelope 不变。
Document impact: design 的 role/stage 表、structured JSON spec、QA design/admission spec 和新的 Phase 1/2 tasks。
Evidence needed: Phase 1 对未来 QA stage/字段明确拒绝；Phase 2 每个新增 QA 能力的结构、policy、validator 和测试位于同一任务。

## RQ-045 - 非权威进度状态的 snapshot 例外

Requirement or question: 是否应为非权威进度状态定义专门的 snapshot 例外？
Source: 第二轮新鲜零对话上下文 complexity 开发前审查的第四条 finding。
Why it matters: 专门例外会把非产品材料永久提升成产品规则，并与“普通 tracked 文档按普通 snapshot 处理”产生额外分支。
Status: withdrawn
User answer: RQ-049 取代本项。产品不识别来源记录，因此也不定义专门 snapshot 例外；有用的实际交付要求直接写入其所属文档。
Downstream effect: 所有 tracked 可交付文档遵循普通 snapshot 规则；生成证据遵循 closure 规则；不增加第三类特殊材料。
Document impact: 由 RQ-049 的减法统一处理。
Evidence needed: 产品 capability、design 和 tasks 中没有专用 snapshot 例外。

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
Downstream effect: RQ-042 的独立当前格式阶段被取代；RQ-038 的分阶段原则保留，但第一阶段定义改为统一 JSON 切换加当前 diff 修复。所有 phase 编号、capability applicability、tasks 和验收口径必须同步前移。
Document impact: alignment 中旧 phase 决定、proposal、design、所有带 phase applicability 的 specs 和 tasks。
Evidence needed: 新 Phase 1 同时通过 requirements/policy 回归、严格 JSON 合同、全生产/消费面切换、closure/path/state 测试和独立 review；不存在独立 Markdown 修复交付或临时 schema-v1。

## RQ-048 - JSON 基础阶段不提前放 Carry 专用矩阵字段

Requirement or question: 新 Phase 1 的全新鲜 FinalExecution 行是否还需要 `resultKind`、`sourceSnapshot` 和 `targetSnapshot`，还是等 Phase 2 真正实现 Carry 时再加入这些字段？
Source: 第三轮新鲜零对话上下文 complexity 开发前审查的第二条 finding。
Why it matters: Phase 1 已有 envelope snapshot 和逐门封存证据绑定；在只能 `FRESH_PASS` 时重复保存 source/target/result kind 没有新增判断价值，却提前发布了 Carry 才需要的形状。
Status: confirmed
User answer: Phase 1 不提前保存 Carry 专用信息。按照后续 RQ-053，Mechanical FinalExecution 的 `gateMatrix` 仍有四个固定 gate 行，但每行只包含 `gate` 和 `gateEvidence`；`gateEvidence` 引用该门已封存且不再变化的完整证据包。当前 snapshot 直接使用 envelope 的 `changeSnapshot`，CLI 重新验证证据包内的 gate、PASS、workflow 和 snapshot 必须与之匹配。Phase 2 实现 Carry Arbiter 时，再在同一任务扩展矩阵行为 `FRESH_PASS`/`CARRIED_PASS`、source/target snapshot 和 carry decision，并补齐校验与测试。
Downstream effect: Phase 1 删除恒定的 `resultKind` 和重复的 source/target snapshot，不损失 snapshot 约束；Carry 的完整矩阵形状只在真正有继承语义时出现。
Document impact: structured JSON spec、design FinalExecution payload 和新的 Phase 1/2 tasks。
Evidence needed: Phase 1 没有 Carry 专用字段；Phase 2 在实现继承时一次性补齐矩阵类型、校验和测试。

## RQ-049 - 保留具体要求，不提来源文件

Requirement or question: 产品文档是否可以保留开发计划中的具体有效要求，但不提来源文件本身，也不为来源文件建立 capability、snapshot 例外、scenario 或 task？
Source: 第三轮新鲜零对话上下文 complexity 开发前审查的第三条 finding。
Why it matters: 来源材料可能包含有用的阶段和验收内容，但文件本身没有运行时行为或长期产品价值；直接写入产品规则会造成永久特殊分支。
Status: confirmed
User answer: 可以保留其中具体、有实际价值的内容，但不要在产品文档中提到来源文件本身。删除该文件专用的 snapshot 例外、capability requirement、scenario 和 task；分阶段实施、每阶段独立收尾、最终四门顺序等真实要求继续写在各自负责的 proposal、design 和 tasks 中。
Downstream effect: 产品合同只表达行为和交付要求，不表达这些要求来自哪份一次性材料，也不增加任何专用实现。
Document impact: proposal、design、evidence closure spec、tasks 和 alignment 中的来源文件表述。
Evidence needed: OpenSpec bundle 不出现来源文件名称或专用行为；具体有效的阶段、验收和四门要求仍完整存在。

## RQ-050 - Reviewer JSON 的写入责任

Requirement or question: Reviewer 的语义判断由谁写入 JSON，CLI 在这条链路中负责什么？
Source: 第四轮新鲜零对话上下文 complexity 开发前审查的第一条 finding，用户于 2026-07-11 确认最小处理。
Why it matters: 文档同时声称 reviewer 负责语义判断且 CLI 是唯一 producer，字面上互相冲突；但这只需澄清所有权，不需要新机制。
Status: confirmed
User answer: Reviewer 直接写自己的结构化 JSON 结论。CLI 不生成或改写 reviewer 的语义判断；它负责权威的静态解析、类型与领域校验、聚合核对和状态登记。删除“CLI 是唯一 producer”的过强表述；不新增生成命令、中间格式或转换层。CLI 自己拥有的 receipt、closure、state 和 mechanical finalization 等确定性材料仍由 CLI 生成。
Downstream effect: Reviewer artifact 的作者与机器准入者分开；reviewer 提供语义结果，现有 CLI 只判断该 JSON 能否成为正式证据并登记状态。
Document impact: design 的 JSON 所有权、structured JSON spec 的 producer/reader 表述和 Phase 1 reviewer migration task。
Evidence needed: Reviewer 可直接产生受严格合同约束的 JSON；CLI 静态拒绝非法结构或语义；实现不增加 artifact generator、中间 schema 或转换流程。

## RQ-051 - 删除重复的 reviewer 输入字段

Requirement or question: 共享 reviewer payload 是否需要同时保留 `inputManifest` 和 `contextBundle`？
Source: 第四轮新鲜零对话上下文 complexity 开发前审查的第三条 finding，用户于 2026-07-11 确认。
Why it matters: 当前实际派发中两个字段指向同一份带哈希的输入清单；同时保留会重复公共字段、校验和闭包边，没有独立判断价值。
Status: confirmed
User answer: 删除 `inputManifest`，只保留 `contextBundle`。`contextBundle` 直接引用带哈希的初始输入清单；按照后续 RQ-054，该清单是 CLI 可静态校验的严格 JSON，其每个输入文件都进入证据闭包。Reviewer 后续用于具体判断的额外证据继续放在相应 `check.evidenceRefs[]` 中。不新增替代字段或另一套 manifest 机制。
Downstream effect: Reviewer payload 只有一个初始输入集合引用，隔离和闭包校验不再对同一文件建立重复边。
Document impact: design 的共享 payload 和字段迁移表、structured JSON spec 的 reviewer payload 合同。
Evidence needed: 合法 reviewer JSON 只要求 `contextBundle`；`inputManifest` 作为未知字段被拒绝；实际 evidence references 仍被递归验证。

## RQ-052 - JSON 输入不按 key 顺序准入

Requirement or question: Reviewer 和 requirements owner 直接写的语义 JSON 是否必须按固定 key 顺序才能通过？
Source: 第五轮新鲜零对话上下文 Phase 1 complexity 开发前审查的第三条 finding，用户于 2026-07-11 确认。
Why it matters: JSON 对象的 key 顺序没有语义；拒绝顺序不同但字段完全相同的输入，需要额外的顺序感知解析和测试，却不提高准入正确性。
Status: confirmed
User answer: 不把 key 顺序作为语义 JSON 的准入条件。Reviewer 和 requirements owner 的 JSON 可以使用任意 key 顺序；CLI 仍严格校验字段集合、类型、必填性、重复字段和领域语义。Receipt 直接哈希 reviewer 实际输出字节，不先做 canonicalization。只有 CLI 自己生成的 receipt、closure、state 和 mechanical FinalExecution 等机械文件保留固定输出顺序和确定性字节。
Downstream effect: 删除无判断价值的顺序敏感准入规则，同时保留严格 schema、精确输出哈希和 CLI 机械产物的确定性。
Document impact: RQ-032 的 canonical JSON 边界、design 的 envelope 描述、structured JSON spec 的输入准入和 Phase 1 closure task。
Evidence needed: 语义等价但 key 顺序不同的 reviewer/requirements JSON 都通过；未知或重复字段仍失败；CLI 自有机械文件的 golden bytes/hash 仍稳定。

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
User answer: 保留原有保证。不新增第二份清单；把 `contextBundle` 已经引用的唯一初始输入清单改成严格 JSON，包含 `bundleVersion`、`workflowId`、`changeSnapshot` 和 `inputs[]`。`inputs[]` 的每项使用现有 `EvidenceRef` 的 `path` 和 `sha256`。CLI 静态校验清单与每个输入文件，并将它们纳入证据闭包；不新增 CLI 命令，不让 AI 解析清单。
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
Evidence needed: 两个并行 reviewer 使用不同 dispatch/subagent/输出路径时各自生成唯一匹配 receipt；交叉 lifecycle、错误输出哈希或共用路径失败；`reviewSessionId` 作为未知 reviewer 字段被拒绝。

## RQ-056 - 公开 JSON 合同与 CLI 内部文件的边界

Requirement or question: Receipt、封存证据包、状态文件和最终验证记录的每个 JSON 字段，是否都必须升级成用户可见的公开合同？
Source: 新鲜零对话上下文 Phase 1 architecture 开发前审查的第四条 finding 所提公共面问题，用户于 2026-07-12 确认。
Why it matters: 将 CLI 自己生成和读取的运行文件全部公开化，会增加大量字段表、兼容压力和无必要的外部承诺；但 reviewer、requirements 和公开 CLI 输出仍需要明确合同。
Status: confirmed
User answer: 只把真正的外部输入和公开输出当作公开合同。Reviewer、requirements、QA Execution、context bundle 和 FinalExecution 的字段必须完整闭合；`policy show --format json` 作为公开命令输出，也必须固定格式和 rule/check ID。Receipt、每门封存证据包、状态文件和最终验证记录是只由 CLI 生成和读取的 run-local 内部文件；文档规定它们的必要保证和所有权，Go typed struct 和行为测试决定内部字段，不将每个字段变成外部兼容承诺。旧 workflow 在格式切换后直接重启。
Downstream effect: 公共面仅保留用户或 reviewer 需要生产/消费的结构；CLI 内部运行文件仍由类型化代码和测试约束，但不为了形式上的严谨扩张公开 schema 或兼容层。
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
Evidence needed: 独立 QA 执行证据和完整 binding 通过时，主代理可用机械 `QA_EXECUTION` artifact 记录 PASS；缺 case、失败结果、错误 hash、旧 snapshot、错误 binding 或开发者自测替代 QA 证据时拒绝且不改状态；QA Execution 不要求 reviewer dispatch/receipt；测试用例 Design Review 和其余三门 reviewer 仍要求独立子代理证据。

## 完成决策

Open questions: none

Dropped question IDs: RQ-008, RQ-013, RQ-024, RQ-031, RQ-034
Dropped question approval: YES
Dropped question reason: RQ-008、RQ-013、RQ-024 和 RQ-034 因用户删除全部 OpenSpec 专用机制而删除；RQ-031 只是在已确认的 reviewer receipt 校验之外继续区分不实施的机制，没有新增决策价值。用户于 2026-07-11 明确要求不再保留已决定不做的方案正文，以避免对后续 reviewer 和开发代理造成语义污染。
Blocking gaps: Phase 0 没有待用户决定的问题。
Downstream permission: Phase 0 已由用户封板。用户于 2026-07-12 明确授权对当前 snapshot 运行 Phase 1 开发前检查；该授权不等于实现交接，只有当前 snapshot 的 complexity、architecture-health 和 cold-water 开发前审查全部通过后，才可准备 Phase 1 实现交接。
