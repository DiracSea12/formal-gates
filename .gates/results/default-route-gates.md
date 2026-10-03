# 默认路线手动两门原始材料

审查基线 d8f24aa87526572ebbbd93a5569819ebf97317ad；交付快照 66c9f4f949e405c84b367edf77645a82a6503c1a。

本次按用户指令不走正式流程。以下两份任务各由全新 `fork_turns:none` 子代理执行，分别为 `/root/default_final_complexity`、`/root/default_final_quality`；未携带历史或开发结论。结果是手动候选结论，非正式流程 PASS。主代理核实见 go-engine-stage5.md 的默认路线段。

## 原始任务：/tmp/default-final-complexity-input.md

[Shared reviewer contract]
用户明确授权：本次不走正式流程，手动跑两门，每次零上下文。此任务由主代理手动组装；prompts/reviewer-base.md 关于必须 CLI 组装的条件仅此处受该明确授权覆盖，其余共享审查契约完整适用。完整读取该文件与 AGENTS.md。只读，不编辑仓库，不启动正式流程，不调用付费模型、不 push。不要读取任何聊天历史、开发者报告、之前的审查/返修结果、.gates/results 或 HANDOFF 中的既往审查记录，不使用其结论。可读取全部相关源码、开发者交付测试及当前需求引用的既有权威条款；必要复现放 /tmp。
[Gate]
完整读取 gates/complexity-gate.md，逐条执行本门原文。
[Current requirement]
完整读取 /tmp/default-route-implementation-contract.md，作为本块已批准需求。它规定 default/full/custom 三个独立名称与身份、默认全部普通门不含 QA；candidate child 普通 QA 不进候选不选不执行，master merge 强制，legacy 只新增 default 不修改既有 full/custom/分片QA纪律。
所有代码工程硬标准必须审查：正确性、质量、需求完整性、最简、耦合、可维护、高可读、优雅/设计一致、无不当硬编码/窄逻辑/过拟合/过度设计防御抽象、测试业务隔离。业务不变量、唯一归属、正常与恢复共用、跨接缝携带者、删除被替代路径/账本/标记、需求来源期望与诚实协议夹具、注释/命名纪律都适用。当前具体冻结 state.json 归属不扩为数据库迁移，不宣称通用数据库条款已满足。
[Current change]
原生 Git 完整比较：d8f24aa87526572ebbbd93a5569819ebf97317ad → 66c9f4f949e405c84b367edf77645a82a6503c1a。比较全部交付 diff，沿每一实际改动的调用者、数据和生命周期链核对所有相关入口及正常/重复/并发/恢复等业务边界，不局限单流程或预设反例。只报告本次引入或放大的范围内缺陷，未实施的其他阶段不倒灌。不要用测试绿色推定全部硬标准成立。
[Dispatch]
仓库 /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-dogfood-dsh，已冻结上述 commit，不使用漂移工作树代替快照。测试按本次具体改动与未决风险取最小充分范围；若需运行依赖 Git 或候选 binary 的仓库检查，应按仓库已有步骤在快照中准备正常前置，区分前置缺失与代码缺陷；没有具体风险不机械重复完整慢包组合。当前无独立 QA 交付测试输入，diff 中测试均为开发者交付测试。保持独立，勿派子代理。
[Result contract]
只返回手动门候选结论 PASS/FAIL/RUNTIME_ERROR，明确不是正式流程 PASS。报告须包括审查实际范围、逐需求实现证据、该门逐维结论与各代码准则核验/未验证项。每个发现含等级、具体当前位置/证据、已确认需求、从正常公开入口/常见失误出发的复现、实际后果、范围/因果/等级核验；P0/P1 单独定级理由。同根因合组但核全因果链，别在第一个表现处停止。P3 纯风格/等价表达/范围外加固不阻塞，也不得为其引新机制。共享契约无P0/P1则PASS，P2仍逐项报告供主代理按用户硬标准返修。不得将其他门职责或未验证项写成已通过。报告中不引用开发者声称通过或以前的问题。
报告写 /tmp/default-complexity-66c9f4f.md，不要写仓库。


## 原始任务：/tmp/default-final-implementation-quality-input.md

[Shared reviewer contract]
用户明确授权：本次不走正式流程，手动跑两门，每次零上下文。此任务由主代理手动组装；prompts/reviewer-base.md 关于必须 CLI 组装的条件仅此处受该明确授权覆盖，其余共享审查契约完整适用。完整读取该文件与 AGENTS.md。只读，不编辑仓库，不启动正式流程，不调用付费模型、不 push。不要读取任何聊天历史、开发者报告、之前的审查/返修结果、.gates/results 或 HANDOFF 中的既往审查记录，不使用其结论。可读取全部相关源码、开发者交付测试及当前需求引用的既有权威条款；必要复现放 /tmp。
[Gate]
完整读取 gates/implementation-quality-gate.md，逐条执行本门原文。
[Current requirement]
完整读取 /tmp/default-route-implementation-contract.md，作为本块已批准需求。它规定 default/full/custom 三个独立名称与身份、默认全部普通门不含 QA；candidate child 普通 QA 不进候选不选不执行，master merge 强制，legacy 只新增 default 不修改既有 full/custom/分片QA纪律。
所有代码工程硬标准必须审查：正确性、质量、需求完整性、最简、耦合、可维护、高可读、优雅/设计一致、无不当硬编码/窄逻辑/过拟合/过度设计防御抽象、测试业务隔离。业务不变量、唯一归属、正常与恢复共用、跨接缝携带者、删除被替代路径/账本/标记、需求来源期望与诚实协议夹具、注释/命名纪律都适用。当前具体冻结 state.json 归属不扩为数据库迁移，不宣称通用数据库条款已满足。
[Current change]
原生 Git 完整比较：d8f24aa87526572ebbbd93a5569819ebf97317ad → 66c9f4f949e405c84b367edf77645a82a6503c1a。比较全部交付 diff，沿每一实际改动的调用者、数据和生命周期链核对所有相关入口及正常/重复/并发/恢复等业务边界，不局限单流程或预设反例。只报告本次引入或放大的范围内缺陷，未实施的其他阶段不倒灌。不要用测试绿色推定全部硬标准成立。
[Dispatch]
仓库 /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-dogfood-dsh，已冻结上述 commit，不使用漂移工作树代替快照。测试按本次具体改动与未决风险取最小充分范围；若需运行依赖 Git 或候选 binary 的仓库检查，应按仓库已有步骤在快照中准备正常前置，区分前置缺失与代码缺陷；没有具体风险不机械重复完整慢包组合。当前无独立 QA 交付测试输入，diff 中测试均为开发者交付测试。保持独立，勿派子代理。
[Result contract]
只返回手动门候选结论 PASS/FAIL/RUNTIME_ERROR，明确不是正式流程 PASS。报告须包括审查实际范围、逐需求实现证据、该门逐维结论与各代码准则核验/未验证项。每个发现含等级、具体当前位置/证据、已确认需求、从正常公开入口/常见失误出发的复现、实际后果、范围/因果/等级核验；P0/P1 单独定级理由。同根因合组但核全因果链，别在第一个表现处停止。P3 纯风格/等价表达/范围外加固不阻塞，也不得为其引新机制。共享契约无P0/P1则PASS，P2仍逐项报告供主代理按用户硬标准返修。不得将其他门职责或未验证项写成已通过。报告中不引用开发者声称通过或以前的问题。
报告写 /tmp/default-quality-66c9f4f.md，不要写仓库。


## 原始返回：/tmp/default-complexity-66c9f4f.md

# 手动复杂度门候选报告

**结论：PASS。此结论不是正式流程 PASS。**

本次没有确认由当前交付引入或放大的范围内 P0/P1/P2/P3 发现。未把测试通过替代工程准则审查，也未把本门结论当作其他门或独立 QA 的结论。

## 输入完整性与实际范围

- 完整读取 `/tmp/default-final-complexity-input.md`、`AGENTS.md`、`prompts/reviewer-base.md`、`gates/complexity-gate.md`、`/tmp/default-route-implementation-contract.md`。输入由允许的结构化任务块构成；手动组装的例外授权明确，没有夹带既往审查或开发者结论。
- 使用原生 Git 比较 `d8f24aa87526572ebbbd93a5569819ebf97317ad → 66c9f4f949e405c84b367edf77645a82a6503c1a`，完整阅读全部 23 个改动文件的交付 diff（454 增、138 删）。没有用漂移工作树替代冻结提交。
- 执行与源码追踪基于目标提交的 Git archive：`/tmp/default-complexity-66c9f4f.1G7cHs`。下文交付源码位置均指该冻结提交中的路径和行号。该目录额外的 `internal/engine/facade/default_child_probe_test.go` 是本审查仅在 `/tmp` 编写的探针，不属于交付 diff。
- 沿两条公开接纳链检查：candidate `NewRouteRequestEvent → Ask → NewReviewDecideEvent → Engine.Submit → admitReviewDecision → State/Decisions → validation/guard/query → Store/重开`；legacy `workflow route → SetRoute → mutateRun → SelectedGates/SkipAuthorizations/guarantee → SaveRunState/LoadRunState`。核对所选门派发、候选/波次、QA/coverage、开发前重选、开发后追加/拒 QA、重复决定、并发 CAS、重开与恢复归属。
- 文档审查覆盖 README 中英文、SKILL、代理菜单提示、formal-flow、example-run、sliced-runs。未读取聊天历史、开发报告、既往门结论、`.gates/results` 或 HANDOFF 记录；未派子代理、调用付费模型、启动正式流程、编辑工作仓库或 push。

## 业务不变量、归属与携带者

1. 三个名称与身份独立：`default` 自动选全部普通门，`full` 保持其既有全集行为，`custom` 仍接受非空子集或全集。身份由既有 `State.RouteMode` / legacy `RunState.RouteMode` 保存；执行义务由既有 `SelectedGates` 保存。没有显示别名账本或另一份菜单控制状态。
2. candidate Ask 的 `Options/RouteGates` 钉住选择资格和候选集合，DECIDE 的 `Choice/RouteDecision` 携带所选身份及计划确认。原有候选集合锚定是顺序无关的集合相等（`review.go:568`），本次没有修改它；default 过滤时保持已接纳 DECIDE 候选表中剩余普通门的顺序（`:599–601`）。
3. `Decisions` 保留不可变决定证据，`RouteMode/SelectedGates` 是当前生效绑定。`validationKinds`、`routeHasNoQAModes`、波次目标、coverage 和 guarantee 继续读取实际所选门，不按 default 名称增加豁免旁路（`validation.go:319/338`、`validation_wave.go:208/413`、`validation_sources.go:93`、`requirement_guarantee.go:293`）。
4. 正常接纳、重放和重开复用 `Engine.Submit`、`commitEffects` 和 Store；恢复只恢复已有事务中的序列化内容（`engine.go:1010/1027/223`、`persistence/recover.go:59`）。本次未新增第二套接纳或恢复实现。
5. 本次沿用已冻结 JSON 状态归属。未迁移数据库；**没有宣称 AGENTS 的通用“控制状态存数据库”条款已满足**。

## 逐需求核验

| 已确认需求 / 验收项 | 当前实现和独立检查证据 | 判定 |
| --- | --- | --- |
| 1. 普通 default 的持久身份、动态全普通门、无双 QA/coverage 门槛；full 双 QA；custom 子集及全集；标签与括注 | `review.go:185/189/193/579/599/614`；`validation.go:319/338`。`review_test.go:288` 读取完整决定和选门，包含交错 QA 与三个普通门；`validation_gate_route_test.go:12` 用真实 Git 的 default/custom 执行开发、候选、所有所选门、最终 COMPLETE，并读取落盘身份和派发/成本记录；legacy `workflow_test.go:2196/2243/2269` 检查 full/custom/default 的实际选门。菜单见 `SKILL.md:74`、`agents/openai.yaml:4`、`README.md:113`、`README_EN.md:107`；CLI help 实际执行成功。 | 符合本次范围 |
| 2. candidate child default/full 均全普通门，无普通 QA；custom 子集；master merge 强制；legacy 政策不迁移 | `review.go:573` 的 child QA 拒绝和 `:562` 的 master merge 锁定原样保留；`slice_test.go:101` 对 default/full 各核绑定。既有 child/full/custom 生命周期与 master 拒绝检查已运行。本审查的 default child 探针从 facade Start 和两段路线决定出发，开发→候选→quality/security 两个实际门绑定→wave COMPLETE，重开读取 default 身份、完整选门及零 QA 集/派发；还检查 master default 被拒且持久 merge 绑定未变。legacy `catalog.go:141`、SetRoute 的 full/custom 和 AddRouteGates 没有更改分片 QA 纪律；`sliced-runs.md:36/47` 区分 candidate 与 legacy 适用面。 | 符合本次范围 |
| 3. choice/mode/candidates 锚定、default 禁手选、计划确认、路线变更共用接纳 | `review.go:185/552/568/596/610`；`development_plan.go:494` 未按 default 分流；`review_test.go:288` 对手选子集/全集、choice 失配、缺候选检查拒绝与落盘零改动；`validation_gate_route_test.go:96` 从 facade 两段控制验证缺少计划确认被拒且身份未绑定。 | 符合 |
| 4. 普通 default 完整生命周期、未调用 QA 无 QA 成本、重复/重开/并发保持一次结果 | `validation_gate_route_test.go:12` 完整普通 default 至 `Status=COMPLETE`，检查门输入身份、完整波次、落盘路线、无 QA 派发和对应费用条目、终态重复 drive 不出新工作；`review_test.go:288` 同事件并发、CAS 冲突后原事件重放、持久重开，核 revision/events 恰一次增加、稳定 acceptance、完整 RouteDecision 和 default 身份。并发新增用例另行在 race 下通过。 | 符合 |
| 5. 开发前重选、开发后追加普通门/拒 QA；route/default help 与示例可执行 | 既有 `validateRouteChange`（`validation_route.go:9`）在统一选门之后执行；`validation_route_test.go:505` 覆盖 default 重选和开发后新增普通门，full 新增 QA 被拒且持久义务不变；`:461` 包含 default 不得删除已有义务。legacy `workflow.go:1244/1252/1278/1283/1290` 保留前置、保证、skip 清理与 QA 设计清理，`workflow_test.go:2269` 核 skip 内容和开发/QA 派发边界。`cli.go:925`、`workflow_test.go:233` 和实际 binary help；`example-run.md:154` 的 default 对应命令与现有 CLI 相符。 | 符合本次范围 |
| 6. 相关 protocol/facade/validate/cli 验证、编译、并发检查、diff；无收费/正式 run/push | 见下面实际执行表。交付 diff 检查无错误；当前 binary 编译成功。没有调用外部模型、正式 engine run 或网络推送。 | 完成所列检查 |

## 复杂度门逐维结论

| 门内维度 | 当前证据与结论 |
| --- | --- |
| 方案/范围先行 | 合同允许既有路线枚举、SELECT_ROUTE、legacy SetRoute/help、直接路线说明和测试。23 个改动文件全部落在这些交付面；无技术审额外边界输入，本次仅用当前已批准合同核范围。没有吸收其他实施阶段或收据合同。 |
| 新概念：default 枚举值 | 需求显式要求与 full/custom 独立的第三个身份，不能仅用文案别名实现。新增常量 `review.go:25` 和 legacy map 项 `workflow.go:109` 是既有枚举的最小扩展；没有新类型/字段/节点/模式体系。 |
| 新选门分支 | 复用现有 `route.Gates` / catalog 候选及 `SelectedGates` 所有者；只排除已有两个内建 QA ID（定义见 `catalog.go:28` 和既有 `validationKinds`），没有复制目录发现、调度或 QA 逻辑。两条分支采用 Go 1.22 标准库 `slices.Clone/DeleteFunc`。Clone 必须保留原候选载荷/后续 skip 授权候选表，不是额外账本。 |
| 新显示/错误说明 | 只扩既有 help、schema 错误闭集和路线说明。三选项、推荐须确认、full 括注、custom 全选身份均为明确需求；没有新配置或自动替用户确认。guarantee 原因改为适用于无 QA 的实际路线，其控制判据没有更改。 |
| 新/改测试 | 全部使用已有 fixture、driver、协议构造器和 Git 助手；增加的断言对应合同规定的身份、选门、前置、完整生命周期、重复/并发/重开。没有新测试框架、依赖、生产测试开关。facade 较大改写实际补足候选至 COMPLETE 的验收边，不是新增产品层。 |
| 五步简洁法 | 第 1 步：所有生产新增内容均有当前需求归属；第 2 步：直接改造已有枚举/接纳点/状态所有者；第 3 步：集合过滤使用标准库；第 4 步：无新增依赖；第 5 步：未找到保持协议错误、原候选证据、skip 候选与必要验证均等价、且有意义地更短的新增表达。没有因“未写排除说明”推定缺陷。 |
| 代码量与文件数 | 生产逻辑只增加模式承认、QA 过滤和显示文本；其余增长集中在验收测试和七处实际路线说明。规模与本合同的双入口、三身份、切片政策和生命周期验收相称。 |
| 复用/删除/残留 | custom 原选门归属和 full 全选保持；路线文案中“custom 不能全选”的被替代说明已在实际路线说明中删除；原 facade QA-free custom 测试被同一行为的 default/custom 完整生命周期测试替代。没有并行菜单状态、QA 旁路、被替代业务路径、账本或标记遗留。冻结历史规格不属于迁移目标。 |
| 过度防御/抽象/预留 | 新校验限于显式禁手选和 mode 闭集；复用现有计划/freshness/CAS/route-change 纪律，没有为攻击性输入或假设性未来场景加参数、状态、兼容路径或机制。 |
| 方案层优先 | 没有确认一个应被否决的多余方案，所以不存在在多余方案上继续雕琢的实现建议。 |

## 代码工程准则逐项核验

| 准则 | 代码或执行证据及判定 |
| --- | --- |
| 正确性 | 统一接纳过滤后再校验变更并保存身份（`review.go:596–615`）；双入口拒绝手选并保持候选来源。上述相关行为与负控执行通过；未发现当前差异违反。 |
| 质量与需求完整性 | 上表逐项给出实现/执行证据；QA 缺席仍保留所有普通门、计划与候选闭环。没有把默认名称当成跳过验证的通行证。 |
| 最简、简洁性 | 一个必需枚举值、既有分支与标准库过滤，无额外归属者/状态/依赖。Clone 的必要性已核原候选落账和 skip 清理用途。 |
| 耦合 | enum 与原接纳点相邻；后续只读现有 RouteMode/SelectedGates；protocol 未引入 validate 反向依赖，未改变层界。 |
| 可维护性 | 新规则聚合在既有 route 接纳；QA 义务仍聚合在 `validationKinds/isSelectedQA`，无需在下游每个动作添加 default 分支。 |
| 高可读性 | default/full/custom 对应直接条件、含义明确的命名和具体错误；文档明确普通、candidate child、master、legacy 的适用面。 |
| 优雅/设计一致性 | 按既有 API、Ask/DECIDE、计划和持久化形态扩展，不引入新的确认节点或成本旁路。 |
| 无不当硬编码 | 新过滤的 blackbox/whitebox 是已有协议内建 QA 标识；普通门来自动态候选，不使用预设普通门列表或测试门名。 |
| 无窄逻辑/过拟合 | 同一候选过滤用于所有普通 default 和 child default；后续义务只读实际选门。测试含交错 QA、多个普通门、动态增加目录门，未按某个反例打补丁。 |
| 无过度设计/防御/抽象 | 本门逐维检查如上，未发现新冗余体系或假设场景加固。 |
| 测试与业务隔离 | 交付新增断言仅在 `_test.go`；新增生产代码无测试分支、测试数据、环境测试开关或旁路。 |
| 唯一控制事实 | 生效绑定沿用 RouteMode/SelectedGates；Ask/Decision 是请求/决定证据而非另一个生效菜单控制层；QA 仍以实际选门为唯一判据。JSON 与数据库条款的冻结例外已明确，未作通用数据库合规结论。 |
| 正常/恢复共用 | `Engine.Submit` 的事件账本幂等在新事件接纳前执行；Store 保存同一字段，Recover 只提交已验证暂存内容。重开与重放实际通过；本次没有新恢复业务分支。 |
| 跨接缝携带者 | `RequestPayload.Options/RouteGates → PendingAsk → DecidePayload.Choice/Review.Route → Decisions/RouteMode/SelectedGates → WorkBinding.RouteScope/ValidationWave/Store`；default 原始 Gates 和身份的落盘断言通过。 |
| 四种计数不混用 | 未新增/改动调用轮次、任务代次、重试次数、业务返修计数；重复/并发测试直接核 revision/events，不冒用业务轮次。 |
| 参考能力与值级语义 | 在合同引用的原选门入口增加值；原 full/custom、legacy full/custom/route-add、child/master 控制不替换。不是移植/重写，未引入第二套同名能力。 |
| 测试期望来源/不降断言 | 新期望逐项来自合同；custom 既有无 QA 用例改成参数化完整生命周期，保留并加强所选门和 QA 缺席断言，未把原失败期望降低为成功。 |
| 原始证据/持久工件内容 | 审查原 diff/源码及本次执行输出；测试读取 actual RouteMode、SelectedGates、Decisions、Ask resolved、revision/event 数、Gate 输入与 DispatchBindings，而非只断言返回成功或文件存在。 |
| 诚实协议夹具 | event 构造器与实际 Ask/DECIDE、receipt/result 方法及输入 Binding/ActionID 一致。门输入逐个核其 Gate 身份；真实 Git 普通 default 流程使用现有生产 VCS runner。测试里的 worker PASS 是协议流程材料，不冒充真实模型对产品的独立审查结论。 |
| 业务边覆盖 | default 普通全流程、缺确认/错候选/手选的零写负控、同事件并发/重复/重开、开发后添加与拒绝 QA、child/master 入口已核。没有只沿新增 if 分支组织结论。 |
| 删除替代机制 | 未引入新机制并行维护；同一 facade 行为测试替换扩展及失效菜单说明删除已核。没有需删除的第二账本。 |
| 注释/命名纪律 | 新/改注释描述当前身份、选门和覆盖判据；新增常量、测试函数按 route/行为/重放/并发命名，没有开发轮次/批次/返修来源标记。已改 lightweight 测试名移除 Restore 过程命名；未把已有未改注释倒灌成当前发现。 |
| 派发与报告事实 | 本门只读仓库；未实施产品代码，未重复受理/触发正式流程，未调用真实模型或 push。下表保留实际失败与未验证项。主代理独立复核属于后续主代理职责，本报告不声称已经代主代理完成。 |

## 实际执行、失败与边界

| 执行 | 结果 |
| --- | --- |
| `git diff --check d8f24aa... 66c9f4f...` | 通过，无输出。 |
| `go test ./internal/engine/protocol ./internal/engine/facade -run 'Route\|SliceChild\|SplitBinding\|DevelopmentPlan' -count=1` | 通过；Go 报告 protocol 4.758s、facade 55.323s。 |
| 初次 validate 的较宽 `-run 'Route\|RequirementGuaranteeRouteArtifactAndRevisionBehavior\|SkillStepFourEntersAfterConfirmation\|OpenAIDefaultPromptStatesModel\|TwoSpeedScheduling'` | 整体失败，30.123s：额外带入既有历史 Git 引用检查，Git archive 没有 `.git`，该项因前置缺失报错。该历史检查不属于本次路线实现判据；未把它解释为当前代码缺陷或以其历史内容形成结论。 |
| 收窄后的 validate 14 个当前相关用例（catalog/default/custom/full/lightweight/guarantee/menu/两条调度/route-add/legacy child/弃黑盒设计） | 通过，16.293s；精确用例列表保存在下段。没有重复完整慢包组合。 |
| `go test ./internal/cli -run 'WorkflowDefaultRouteAndHelp\|WorkflowUsesDispatchesKindsAndNativeSnapshots' -count=1` | 通过，6.031s。 |
| `go test -race ./internal/engine/protocol -run '^TestDefaultRouteBindsOrdinaryGatesAcrossReplayAndConcurrentDecisions$' -count=1` | 通过，1.973s，无 race 报告。 |
| `go build -o /tmp/default-complexity-66c9f4f.1G7cHs/bin/formal-gates ./cmd/formal-gates`；随后 `bin/formal-gates workflow route --help` | 编译及 help 执行通过；help 显示独立 default/full/custom/lightweight 和要求的 full 括注。未测量编译总耗时。 |
| 临时 default child 生命周期探针 | 首次失败 2.365s：探针在 child Start 前重开了使用注入 Git runner 的 master，Open 使用生产 runner，导致临时目录 Git 前置缺失；这是探针准备错误。改为先用 Store 读取 master 持久事实、保留原 runner 完成 child Start；探针随后通过，1.835s。交付生产源码未作修复修改。 |

收窄 validate 的精确用例：`TestPackageRouteCandidatesUsePromptCatalog`、`TestDefaultRouteSelectsEveryDiscoveredOrdinaryGate`、`TestCustomRouteAcceptsWholeCandidateList`、`TestBlackboxWhiteboxOptionalityInRoute`、`TestRouteModesAndInvalidModeMessage`、`TestSetRouteLightweightSelectsZeroGates`、`TestStartRouteDeclarationAcceptsOnlyLightweightOrEmpty`、`TestRequirementGuaranteeRouteArtifactAndRevisionBehavior`、`TestSkillStepFourEntersAfterConfirmation`、`TestOpenAIDefaultPromptStatesModel`、`TestTwoSpeedSchedulingBothPathsReachDevelopment`、`TestRouteAdditionAfterCompletedWaveReviewsOnlyAddedGate`、`TestSplitDecisionAllowsPerSliceRouteConfirmation`、`TestFastPathDesignDiscardedWhenRouteOmitsBlackbox`。

未执行/不背书：完整 `go test ./...`、全包 race、go vet、完整安装/canary 包组合；没有未决的本次风险要求这些组合。未重跑持久化每一个故障注入崩溃窗口，本次只执行 route 重开/重放并核恢复源码的共用归属，不能把它表述为全部崩溃窗口测试通过。没有独立 QA 交付材料，也没有执行真实付费 QA/审查；本报告只评价当前交付的实现复杂度与列出的工程核验，不宣称产品真实模型端到端验收或其他门通过。

## 发现与候选结果

发现列表为空；不存在需逐 finding 定级、端到端复现及因果核验的 P0/P1/P2 项，也没有借纯风格偏好要求新增机制。

**手动复杂度门候选结果：PASS；非正式流程 PASS。**


## 原始返回：/tmp/default-quality-66c9f4f.md

# 实现质量手动门候选报告

结论：**PASS（手动门候选结论，不是正式流程 PASS）**。

未发现本次引入或放大的范围内 P0/P1/P2 缺陷。无待登记 finding；不以测试绿色替代下面的代码与工程准则核验。

## 输入、独立性与实际范围

- 完整读取本次结构化任务、`AGENTS.md`、`prompts/reviewer-base.md`、`gates/implementation-quality-gate.md` 和 `/tmp/default-route-implementation-contract.md`。
- 原生 Git 比较：`d8f24aa87526572ebbbd93a5569819ebf97317ad` → `66c9f4f949e405c84b367edf77645a82a6503c1a`，审阅全部 23 个交付文件的 diff（454 行增加、138 行删除）。
- 读取和执行基于独立冻结检出 `/tmp/default-quality-66c9f4f-snapshot`，HEAD 为指定提交。执行后 `git status --short` 为空；构建产物处于忽略目录，交付源码未改。
- 审阅全部生产变更：protocol 的路线枚举、schema、接纳与注释，legacy 的 SetRoute、保证理由，CLI help；全部相关 README、SKILL、代理提示和三个流程参考文档；diff 中全部测试按开发者交付测试审阅。
- 沿实际调用链读取 Ask/DECIDE、开发计划确认、路线变更/重绑定、QA 模式与 coverage、wave/guard 投影、事务与加载/重开、legacy transition/route-add/状态保存及相关测试夹具。追踪正常选择、错误选择、重复决定、并存 Ask、并发决定、重开、开发后追加、终态重复推进和 child/master 边界。
- 未读取聊天历史、开发者报告、旧审查/返修结果、`.gates/results` 或 HANDOFF 既往记录；未派子代理、未启动正式流程、未调用付费模型、未 push、未编辑原仓库。
- 下列代码位置均指冻结提交内的路径与行号，非漂移工作树。

## 逐需求实现证据

| 验收项 | 判定与当前实现证据 |
| --- | --- |
| 1. 普通 default 独立身份、动态全普通门、无 QA；full 双 QA；custom 子集/全集；标签 | 覆盖。`internal/engine/protocol/review.go:24` 新增独立 default 枚举；`:183` 拒绝 default 的手工 Selected；`:600` 从当前确认候选排除 blackbox/whitebox，保留其余候选顺序；`:614` 保存原 mode 与实际 SelectedGates。full 仍在 `:579` 强制双 QA，custom 分支保留非空且可全选。`validation.go:319` 由实际 SelectedGates 得出 QA 模式；`validation_wave.go:207` 与 `validation_sources.go:92` 的无 QA coverage 处理不判断 default 名称。`SKILL.md:75`、README 中英文路线段、`agents/openai.yaml:4` 展示三个独立选项、full 括注及需要用户确认的推荐。新增测试核持久身份和完整普通门列表；既有 custom 全选、full 绑定测试通过。 |
| 2. candidate child 普通 QA 排除，default/full 全普通门，custom 子集；master 强制；legacy 仅新增 default | 覆盖。`protocol/engine.go:1135` 的 child Ask 拒 QA和 `review.go:572` 的决定拒 QA对三个 mode 共用；default/full 的选择分别排除 QA/全选，child 候选本来只有普通门。master 的 `review.go:541` 同事务绑定 merge，`:562` 及 `engine.go:1140` 拒常规路线。`protocol/slice_test.go:101` 逐 mode 断言 default/full 全列表与无 QA；child 继承/子集/执行和并发认领相关测试通过。legacy `validate/workflow.go:1252` 仅插入 default 分支，full/custom/候选/route-add/切片 QA 逻辑无修改；其相关回归通过。文档在声明 child 无普通 QA 时明确限定 candidate engine，`references/sliced-runs.md:47` 将仍可选黑盒工作区的说明限定 legacy。 |
| 3. choice/mode/candidates Ask 锚定，拒 default 手工 subset；计划与变更共用接纳 | 覆盖。`protocol/engine.go:1180` freshness、`:1186` Ask 选项合法性；`review.go:552` choice 与 mode 一致，`:570` 对 Ask 候选集合核等；`:597` 调用既有计划确认，`:610` 调用既有 validateRouteChange。Ask/候选匹配保留既有集合语义，未另建显示或菜单状态。`protocol/review_test.go:288` 对 subset/手工全集、choice 不符、缺候选拒绝，并核 state 字节不变；`facade/validation_gate_route_test.go:96` 拒绝未确认计划并核未落 route。 |
| 4. 普通 default 开发至 COMPLETE，全所选门，未调 QA 无 QA 成本；重复/重开/并发 | 覆盖且执行。`facade/validation_gate_route_test.go:12` 在真实隔离 Git 仓通过 façade 提交开发、冻结候选、逐门结果、候选晋升与重开，断言 wave targets、每个实际 Gate 身份、COMPLETE、持久 route/列表、无 QA Sets、无 QA DispatchBindings 与 QA cost dispatch、终态重复推进不生新工作。`protocol/review_test.go:288` 走公开事件入口，核并发接受只增加一次 revision/Event、同事件重放稳定、重开后 state 字节和 Decisions/PendingAsk 身份。该并发测试单独 race 通过。 |
| 5. 开发前重选、开发后追加普通门/拒新增 QA；route/default help、示例 | 覆盖且相关验证通过。`protocol/validation_route.go:9` 的既有规则只读实际选择；`review.go:610` 三个 mode 均经过它，未开 default 旁路。`facade/validation_route_test.go:505` 测重复确认 default、开发后新增普通门、持久 default 身份与拒 full 新增 QA；其他路线重选/在途工作/候选/返修/结果退役测试覆盖共用链。legacy `SetRoute` 仍经过 `requireTransition` 和 `mutateRun`；`AddRouteGates` 保留原有追加与 QA 禁止规则。`cli/cli.go:925` help、`cli/workflow_test.go:233` 实际 default 命令均验证。`references/example-run.md:151` 分清推荐 default 与示例显式选 full，给出的对应 default 命令由 CLI 测试验证。 |
| 6. 改动面验证、编译、必要 race、diff；授权限制 | 满足本次手动审查执行范围。protocol/facade/validate/cli 的相关测试、default 并发 race、源码构建、原生 diff 检查、按已有构建前置执行的包校验全部成功。没有正式 run、付费调用或 push。具体命令及限制列于末节。 |

## 本门五个维度独立结论

### 一、需求覆盖：无发现

先按六条验收逐项核实现路径，再核必要文档入口。default、full、custom 各有独立保存身份；普通、child、master、legacy、CLI 均有对应路径。未发现合同明确要求但缺失的功能入口或错误处理。此维度结论来自上述源码映射，不将测试质量或执行结果当作路径存在的替代依据。

### 二、实现与需求偏差：无发现

QA/coverage、wave、guard 仍读取 SelectedGates；default 的新行为仅在既有选门归属点形成实际选择。custom 全选仍为 custom；lightweight 仍只能作为独立 start 声明；full 普通 run 的双 QA 验证、child 的候选拒 QA和 master merge 锁定均未被改写。Ask 集合锚定、计划确认、revision/freshness、skip 清理与路线变更纪律仍在共用接纳链内。legacy 没有随 candidate 政策改变分片 QA。

### 三、架构与耦合：无发现

本次唯一新增产品概念是需求直接要求的 default 路线值；没有新增持久字段、模块、缓存、状态体系、流程节点、显示别名、恢复实现或 QA 旁路。protocol 与 legacy 分别在各自已有 SetRoute/接纳归属中做过滤，两者具有不同的现有运行边界，不需要为这一小分支引入跨层机制。

控制事实和跨接缝携带者明确如下：

- 用户所见选项/候选的确认事实由 PendingAsk.Options/RouteGates 携带；DECIDE.Choice/Review.Route 接纳后进入不可变 Decisions 历史。
- 当前路线身份/义务由 State.RouteMode/SelectedGates 携带；facade/query 是只读投影。验证模式、wave targets 与 guard 的路线和门身份从该事实派生。
- 计划确认由 RouteDecision.DevelopmentPlan 携带，来源是 readiness 结果或 child 内联提案；既有确认归属负责关联需求 revision、拓扑与计划摘要。
- 调用重放身份由 Events 的事件 digest/Acceptance 携带；业务提交共用 `engine.go:227` 的 commitEffects/Store.Save。重开由 `engine.go:97` load 与 façade Open 读取同一状态，后续 Submit/Drive 使用同一业务规则。
- legacy default 共用 `workflow.go:2440` 的 mutateRun/状态锁/LoadRunState/SaveRunState；其 route-add 与 skip/保证更新保留既有归属。

没有被替换却继续生效的生产路径、第二份账本或新退役责任。已有路线重绑定由 `validation_route.go:59` 统一执行；未为了 default 另建生命周期。状态仍沿用当前冻结的 state.json 方案，**没有审定“控制状态已存数据库”，也没有把该通用条款记为通过**。

### 四、实现正确性与质量：无发现

普通门集合从实际候选动态计算，使用标准库 Clone/DeleteFunc；零散 QA 项之间的普通门顺序得以保留，full/custom 既有分支与校验保留。新分支没有删除错误处理或改变持久化提交顺序。只在路线决定的非热路径做一次列表过滤；没有证据表明出现性能下降、资源泄漏、无界循环或新产物垃圾。

开发者测试放置在实际行为归属层：protocol 核 schema/锚定/持久重放/CAS；facade 额外核计划、实际派发、候选与终态行为；legacy validate 核候选、skip 与保证；CLI 核用户入口/help。更高层覆盖具有不同正常行为，不是重复逐条实现断言。默认路线的全流程测试使用真实隔离 Git 身份和树；逐门取对应 target.Task，再断言 input.Gate.Gate 并携实际 Binding 返回，未用同值替身掩盖输入身份。持久断言覆盖 route、列表、Decisions、PendingAsk、revision 和 state 字节，非仅成功返回或文件存在。

原 custom 单门测试由 default/custom 参数化生命周期测试替代，保留 custom 子集行为并增加终态、实际身份与副作用断言；未降低关键断言。新增期望分别来自全普通门、显式确认、独立身份与 QA 排除需求。生产代码不引用测试 fixture、探针状态或测试特殊分支。

### 五、过拟合与窄逻辑：无发现

实现未固定普通门名称/数量或测试样例列表；任意已确认普通候选均由同一过滤路径选中。排除 blackbox/whitebox 是合同明确指定的内建 QA 身份，与仓库现有 validationKinds/isQAMode 语义相符，不是样例硬编码。child/full/default 的行为来自 run 身份与统一候选约束；无按测试 case、旧反例或开发轮次编写的分支。

## 工程硬标准逐项核验

| 准则 | 结论与证据/限制 |
| --- | --- |
| 正确性 | 范围内未发现缺陷；选门、拒绝、派发、持久身份和重复/重开/并发证据见验收 1—5。 |
| 质量 | 校验、错误语义和提交归属保留；开发者测试检查实际输入/输出/持久内容。 |
| 需求完整性 | 六条验收逐条映射完成，普通/child/master/legacy/CLI 都有路径。 |
| 最简与简洁性 | 复用既有路线枚举和接纳者；新增过滤使用标准库；无新机制、状态或依赖。未重做方案复杂度门。 |
| 耦合 | 选择→实际义务→验证/guard 的依赖方向不变；无显示名控制 QA的隐式协调。 |
| 可维护性 | 新语义与已有 full/custom 集中在原选门位置；不另维护 QA豁免或恢复规则。 |
| 高可读性 | 三个 mode 分支直接表达各自选择；错误消息描述实际限制；无晦涩表达或隐藏副作用。 |
| 优雅性/设计一致性 | 以可核验的一致性评价：新增 mode 沿原 Ask/DECIDE、计划、变更、事务和投影路径处理；不凭个人偏好提出替代设计。 |
| 无不当硬编码 | 普通门动态发现/显式候选驱动；QA排除值属于已确认协议身份。 |
| 无窄逻辑/过拟合 | 候选集合全范围处理，不按某组门、调用样例、测试名或历史反例分支。 |
| 无过度设计/防御/抽象 | 没有新增状态、缓存、helper 体系、额外确认节点或防御机制；未扩大到数据库迁移。 |
| 测试与业务隔离 | 本次生产变更不依赖测试；开发者 fixture 仅位于测试文件，真实 Binding 与候选身份被核对。 |
| 唯一控制事实归属 | 当前 route/义务仍由现有持久 State 持有，Ask 与 Decisions 分别承载确认前输入/历史证据；不并行维护第二份可写选择。数据库通用条款未验证且不宣称满足。 |
| 正常/恢复共用 | 读取/重开后继续同一 Submit/Drive 和 Store 接纳；实际 reopen 与事件重放验证通过。未新增恢复业务实现。 |
| 轮次/代次/重试/返修计数独立 | 本次未新增或改用这些计数；并发默认决定验证核事件/revision，未以其替代业务返修或任务代次。 |
| 跨接缝携带者 | Ask→DECIDE→Decisions/RouteMode/SelectedGates→验证/guard→Store/Open 的携带者已逐项核对；计划、需求 revision、Task Binding 和终态仍走原链。 |
| 删除被替代路径/账本/标记 | 无生产路径/账本/标记替换；default 新模式并非对 custom 的别名。原 custom 单门测试已被参数化完整生命周期测试替代，没有两份并行测试机制。 |
| 注释纪律 | 本次新增/修改的代码注释描述当前路线、形态约束与实际 QA判据；无新增历史演进叙事或多份权威契约。 |
| 命名纪律 | 新变量、mode、测试函数描述产品路线或被测行为；无开发阶段/审查轮次/返修编号痕迹；slice/default/full/custom 属当前业务词汇。 |
| 测试期望来源 | QA 排除、动态普通门、custom 全选身份、一次接纳、未确认计划拒绝来自合同/既有公开纪律；未为绿色结果更改规则或使用测试专用生产分支。 |
| 原始证据与诚实夹具 | 核指定原生 diff、当前代码、实际测试输出；读取持久内容与实际 Gate/Binding/候选身份。未引用开发者声称通过。 |
| 全流程/副作用边界 | façade 生命周期覆盖开发→候选→全部所选门→晋升→COMPLETE和各阶段 reopen；protocol 覆盖拒绝零写、同事件一次性及并发。共用路线变更测试涵盖在途设计/审查/返修/候选的连续性与退役。 |
| 性能与资源卫生 | 新代码仅增加路线选择时有界 O(n) 过滤，无新文件/日志/缓存创建；测试临时仓/文件由 testing 的 TempDir 生命周期持有。未运行基准测试，未宣称性能提升。 |
| 值级语义/协议保真 | 本次非跨语言移植或重写；full/custom 原值与协议字段保留，只增加 default 合法值；不改既有外部 method/消息形状。 |
| 派发/真实模型调用/push | 本次是独立只读审查；未实施业务代码、未另派代理、未调用真实付费模型、未 push。既往开发派发流程不在本次 gate 可独立验证的输入范围内，不为其背书。 |

## 实际执行

工作目录为冻结快照。先按 README 已有步骤构建 `bin/formal-gates`，再执行依赖候选 binary 的包校验；Git 前置由冻结提交的正常检出提供。

1. `go build -o bin/formal-gates ./cmd/formal-gates`：exit 0。
2. `go test ./internal/engine/protocol ./internal/engine/facade -run 'Test(DefaultRoute|OrdinaryGateRoutes|SliceChild|ReviewSequenceThroughTopologyAndRoute|SplitTopology|CustomRoute|Route|DevelopedRoute|ReadinessRedoOnBoundRoute)' -count=1`：protocol PASS（3.449s）、facade PASS（31.001s）。
3. `go test ./internal/validate ./internal/cli -run 'Test(DefaultRoute|RouteModes|StartRoute|SetRouteLightweight|CustomRouteAcceptsWhole|BlackboxWhiteboxOptionality|MergeGateRegistered|RouteCandidates|SplitDecisionAllowsPerSlice|FullRouteDesigns|FastPathDesignDiscarded|RequirementGuaranteeRouteArtifactAndRevisionBehavior|Skill|OpenAIDefaultPromptStatesModel|FormalFlowDeclaresLightweightRoute|CLIWorkflowDefaultRouteAndHelp|CLIRouteCandidates)' -count=1`：validate PASS（14.722s）、cli PASS（1.834s）。
4. `go test -race ./internal/engine/protocol -run '^TestDefaultRouteBindsOrdinaryGatesAcrossReplayAndConcurrentDecisions$' -count=1`：PASS（1.864s）。
5. `git diff --check d8f24aa87526572ebbbd93a5569819ebf97317ad 66c9f4f949e405c84b367edf77645a82a6503c1a`：无输出，exit 0。
6. `bin/formal-gates package validate --root .`：`PASS formal-gates package validation`，exit 0。输出附完整包条目清单较长，工具展示有截断；结论依据实际退出码及明确校验结论，不把清单截断内容当作逐文件代码审定。
7. 冻结快照 `git status --short`：无输出。

没有测试失败，也未因前置缺失把检查失败误判为产品缺陷。未机械运行全仓慢测试、全部 race、portable canary或所有安装/宿主组合；改动未触及这些路径且本次未发现需要扩展执行的具体风险。未做进程强杀/磁盘写入中断注入；恢复结论限定为已执行的持久重开、重放和相关路线生命周期测试，加上当前共用接纳代码核查。未运行真实付费 QA/门模型，未将模拟 worker 的业务结果描述为真实模型评审结果。独立 QA 用例充分性及正式流程记账不是本门职责，本报告未给其 PASS。
