# 分片 checkpoint 手动两门原始材料

本次按用户授权不走正式流程。全部门派发都是新子代理、fork_turns:none；输入不包含开发者报告或既往审查结论。审查基线为 0c5400b93885e1edf1949ee1e4467a9fc44d88a8。候选结论不替代主代理逐项工程验收。

## 首次冻结对象 e030dc3815c6cf28b54fda8dbbb80f590cbecef1

两门候选均 PASS，并各报告同一纯内存指针别名 P3。主代理未据此接纳本块，独立正常公开链发现自动请求摘要遗漏 P2，已独立复现并派返修。以下是当时完整原始材料。

## 原始任务：/tmp/slice-checkpoint-complexity-input.md

[Shared reviewer contract]
用户明确授权：本次不走正式流程，手动跑两门，每次零上下文。此任务由主代理手动组装；prompts/reviewer-base.md 关于必须 CLI 组装的条件仅此处受该明确授权覆盖，其余共享审查契约完整适用。完整读取该文件与 AGENTS.md。只读，不编辑仓库，不启动正式流程，不调用付费模型、不 push。不要读取任何聊天历史、开发者报告、之前的审查/返修结果、.gates/results 或 HANDOFF 中的既往审查记录，不使用其结论。可读取全部相关源码、开发者交付测试及当前需求引用的既有权威条款；必要复现放 /tmp。
[Gate]
完整读取 gates/complexity-gate.md，逐条执行本门原文。
[Current requirement]
完整读取 /tmp/slice-receipt-implementation-contract.md 与 /tmp/slice-checkpoint-execution-brief.md，作为本块已批准需求。完整合同已获批准，执行补充明确当前 child 成功 checkpoint 交付边界及独立 default 身份；master 接纳/汇总随后交付。基于精确 promotion/native identity，child 成功为可恢复非终态 SLICE_READY；冻结完整 typed receipt/digest、费用和 owner 端点于单一事务；副本可恢复发表；普通 regular 终态语义仍适用。按合同逐项判断，不以未实施的后续块代替本块要求。
所有代码工程硬标准必须审查：正确性、质量、需求完整性、最简、耦合、可维护、高可读、优雅/设计一致、无不当硬编码/窄逻辑/过拟合/过度设计防御抽象、测试业务隔离。业务不变量、唯一归属、正常与恢复共用、跨接缝携带者、删除被替代路径/账本/标记、需求来源期望与诚实协议夹具、注释/命名纪律都适用。当前具体冻结 state.json 归属不扩为数据库迁移，不宣称通用数据库条款已满足。
[Current change]
原生 Git 完整比较：0c5400b93885e1edf1949ee1e4467a9fc44d88a8 → e030dc3815c6cf28b54fda8dbbb80f590cbecef1。比较全部交付 diff，沿每一实际改动的调用者、数据、状态和生命周期链核对所有相关入口及正常/重复/并发/恢复等业务边界，不局限单流程或预设反例。只报告本次引入或放大的范围内缺陷，未实施的其他阶段不倒灌。不要用测试绿色推定全部硬标准成立。
[Dispatch]
仓库 /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-dogfood-dsh，已冻结上述 commit，不使用漂移工作树代替快照。测试按本次具体改动与未决风险取最小充分范围；若需运行依赖 Git 或候选 binary 的仓库检查，应按仓库已有步骤在快照中准备正常前置，区分前置缺失与代码缺陷；没有具体风险不机械重复完整慢包组合。当前无独立 QA 交付测试输入，diff 中测试均为开发者交付测试。保持独立，勿派子代理。
[Result contract]
只返回手动门候选结论 PASS/FAIL/RUNTIME_ERROR，明确不是正式流程 PASS。报告须包括审查实际范围、逐需求实现证据、该门逐维结论与各代码准则核验/未验证项。每个发现含等级、具体当前位置/证据、已确认需求、从正常公开入口/常见失误出发的复现、实际后果、范围/因果/等级核验；P0/P1 单独定级理由。同根因合组但核全因果链，别在第一个表现处停止。P3 纯风格/等价表达/范围外加固不阻塞，也不得为其引新机制。共享契约无P0/P1则PASS，P2仍逐项报告供主代理按用户硬标准返修。不得将其他门职责或未验证项写成已通过。报告中不引用开发者声称通过或以前的问题。
报告写 /tmp/slice-checkpoint-complexity-e030dc3.md，不要写仓库。


## 原始返回：/tmp/slice-checkpoint-complexity-e030dc3.md

# Child checkpoint 手动复杂度门候选报告

结论：**PASS**。这是用户授权的手动复杂度门候选结论，**不是正式流程 PASS**。

冻结比较：`0c5400b93885e1edf1949ee1e4467a9fc44d88a8` → `e030dc3815c6cf28b54fda8dbbb80f590cbecef1`。P0/P1/P2：0 项；P3：1 项非阻塞代码准则建议。

## 审查输入、实际范围和独立性

完整读取本次结构化输入、`AGENTS.md`、`prompts/reviewer-base.md`、`gates/complexity-gate.md`、已批准完整实施合同及 child checkpoint 执行补充。任务块符合允许结构；手动组装由当前用户明确授权覆盖。没有读取聊天历史、开发报告、以前审查结果、`.gates/results` 内容或 HANDOFF 中的既往审查记录；没有派发代理、启动正式流程、调用付费模型或 push。

通过原生 Git 比较阅读全部 11 个交付文件，836 行新增、54 行删除，其中开发者新增测试为 405 行。实际核查的相关链包括：

- facade Start/ClaimSlice → child 路线与开发验证 → candidate promotion → Drive → 共享 native 核验 → CheckpointSlice → engine commitEffects/Store.Save。
- store Recover → engine Load → 冻结 receipt → 同一 publishSliceReceipt → Show/Status/Next/project/projectOrSummary；CLI 对 Run/Next 的现有 JSON 输出。
- owner bootstrap/baseline → overlap 观察/双边标记 → MeasureOwner → child 原子冻结或 ordinary RecordOwnerCost；peer 开放/闭合区间查找。
- ordinary regular seal、terminal publication/replay 和 lifecycle 收口；phase/validation/State JSON 编码与加载校验。

没有将尚未交付的 master 接纳、case/cost 汇总、integration、级联 reset/abort、child worktree 等内容倒灌为本块缺陷。legacy owner 查找仅核其与当前抽取的关系，未将其留存的结果文件作为历史审查输入。

运行验证使用独立本地克隆 `/tmp/slice-checkpoint-complexity-e030dc3-snapshot`，checkout 精确冻结提交；没有用当前漂移工作树替代快照，也没有编辑源仓库。必要内存语义探针仅写入该 `/tmp` 快照的 `review-probe`。

## 逐需求核查

| 本块已确认要求 | 当前实现证据与核查结果 |
| --- | --- |
| child 成功进入可恢复非终态 SLICE_READY | `protocol/slice.go:126` 在既有 validationConcluded 准入后冻结 receipt 并经现有 TransitionPhase 到 SLICE_READY；`facade.go:890` 将 child COMPLETE 边界交给 checkpoint。新增 `TestSliceChildPromotionReachesRecoverableCheckpoint` 的 default/full/custom 全部执行通过。 |
| 完整 current wave、promotion、当前 requirement/topology/route 与精确 native 身份 | `workspace.go:201` 继续持有唯一 validationConcluded；`slice.go:143` 重核 barrier/abort/validation 与 snapshot/tree；`facade.go:964` 共享 Resolve、VerifyReady、ancestor、TreeDigest。缺门、无 promotion、dirty、drift、tree mismatch、barrier、abort 的开发者测试执行通过。 |
| typed receipt 全字段和 canonical digest | `slice.go:55`–`121` 定义 receipt/最终 wave/target+result/request+event 投影；`slice.go:161` 从既有事实取值，target 按键排序、最后身份按 Revision 最大值选取。`TestSliceChildPromotionReachesRecoverableCheckpoint` 核 ID、版本信封、Git tree、路线、最终 wave、门结果、空 QA/cases、成本、最后 request/event digest，实际通过。 |
| receipt、phase、owner cost 与 owner endedAt 单一事务 | `facade.go:999` 沿 promotion 锁与既有 owner 锁观察；`slice.go:152` RecordOwner 与完整 receipt/phase 一起经一次 e.commit。commitEffects/Save 为既有唯一持久提交路径；没有先 RecordOwnerCost 再补 endpoint 的 child 路径。故障测试覆盖 commit 前、replace 后、response lost；内容与费用断言通过。 |
| sidecar 为可重建证据，不是第二权威 | `facade.go:1037` 只消费冻结值，复用 writeJSONAtomic，发布后读回 canonical digest；`Drive:859` 重入直接用同一 publisher。无发布状态、备用队列或第二恢复实现。 |
| 冻结后不重新消费当前 HEAD、不重测 owner | publisher 使用 receipt.Snapshot 的 Verify/TreeDigest。`TestSliceReceiptRemainsFrozenAcrossReplayAndLaterCommits` 和恢复测试在真实 Git 后续提交及 transcript 增长后重开/Drive，receipt 和 state 字节、成本与 endpoint 不变，实际通过。 |
| child 不独立 seal、不产生 terminal summary、不 cleanup/EndRun | `workspace.go:154` 拒绝 child SealTerminal；`facade.go:1239` 给 SLICE_READY 状态且清空 SummaryPath；Drive 终态处理仅匹配 COMPLETE/ABORTED。新增测试核 terminal 工件缺席、恢复材料保留及 lifecycle active，实际通过。 |
| Show/Status/Next/Plan 可观察 checkpoint | `engine.go:530` 由持久 phase 派生 WAIT_DEPENDENCIES，`facade.go:1276` 暴露 receipt；projectOrSummary 对非终态直接返回，CLI 现有 printValue 不需要第二套编码。查询测试实际通过；CLI接线为源码核查，未另跑已安装宿主会话。 |
| owner exact/unavailable/no binding 语义、闭合 checkpoint 不误伤后续 run | `owner_cost.go:387` 抽出既有观察/MeasureOwner，ordinary 和 child 共用；`owner_cost.go:105/114/141` 让 state receipt 提供闭合区间，沿同一 overlap 判定，不造 endpoint 文件。17/2/15/8 exact、UNAVAILABLE 零值、no binding 和缺 sidecar 后新 run 6/1/5/2 断言通过。 |
| ordinary regular 保持原有终态语义 | sealDelivered 只委托共享 verifier，再 SealTerminal；既有 terminal guard、summary 和 EndRun 仍为原路径。已执行相关 ordinary terminal 与 owner 回归，未观察 SLICE_READY 出现在普通 run。 |
| 同 child 并发只冻结一次 | promotion/owner 锁顺序与 CAS 保持既有 owner 体系；新增三 facade 并发测试核只增加一个 revision、只增加一次 owner tokens，普通测试与 race 均通过。该测试前置先完成 engine internal frontier；本结论只覆盖 checkpoint 提交窗，不将其扩大成所有既有 Drive 前置步骤的并发证明。 |

## 复杂度门原文逐维结论

1. **批准范围与边界：通过本门核查。** 实际修改集中于当前 child checkpoint、共享 native/owner 归属、状态编码与查询和对应测试。没有 master accepted map 占位，没有额外公开写命令、路线特例、merge/worktree/级联或 billing。
2. **每个新概念的需要和既有归属：通过本门核查。** 下表逐项检查；没有以“未写排除说明”推定缺陷。

| 新增或抽取概念 | 必要性、既有 owner 与最小化判断 |
| --- | --- |
| SliceReceipt + SliceTerminalReceipt | 合同明确要求非终态 typed receipt 与 canonical digest；SliceChildBinding 只有启动身份、terminal summary 是终态证据，均不能替代。放在既有 slice.go 与 State，不新造存储体系。 |
| SliceValidationReceipt/Result | 从既有 wave/ValidationTarget/WorkerResult 投影最终需要的结果；直接复用既有 candidate、target、result 类型，没有复制整份 State/Validation 的运行账本。 |
| SliceEventIdentity/RequestIdentity | 按合同携带最后 event/request 的 digest 和 Revision；从既有 Events/Decisions 派生，无 last-event/last-request 新状态或账本。 |
| State.SliceReceipt 与 Run.SliceReceipt | 一个持久权威字段、一个现有查询投影字段；公开投影是合同要求的验收面。没有因仓库内调用者少就判为无用。 |
| CheckpointSlice/verifySliceReceipt | 在既有 slice 业务 owner 接纳并在既有 load/commit 边界校验；复用 validationConcluded、RecordOwner、TransitionPhase、commit/CAS。没有第二达标规则或副本接纳状态机。 |
| verifyDelivered | 删除 sealDelivered 的重复核验主干后，ordinary seal 与 checkpoint 共用 native 步骤；使用既有 VCS provider，TreeDigest 是 Git 原生树身份。 |
| checkpointDelivered | 需要协调 native admission 与 owner 原子冻结；复用现有 promotion 和 owner 锁，未增锁体系。 |
| publishSliceReceipt 与 slice-receipt.json | 合同明确证据副本；复用现有 writeJSONAtomic，同函数负责正常发布/补发布，未造恢复队列或发布标志。 |
| observeOwnerCost | 原有 finishOwnerCost 不能直接用于 child，因为会先单独提交 Owner；抽取观察值后 ordinary 仍委托原有 RecordOwnerCost，child 同事务冻结。没有第二测量算法。 |
| Store.Envelope | 一行委托现有 expectedEnvelope，保留实际 Config 的全部 version/identity；未复制信封规则。 |
| 既有 SLICE_READY/overlap/phase/query 扩展 | 复用已定义 phase 与 owner interval 查找，不新加 phase、收齐布尔、endpoint 文件或任务代次计数。 |
| slice_checkpoint_test.go | 按被测行为组织开发者测试，使用既有 validationDriver、真实 Git、transcript 及持久事务故障点；无生产测试专用入口。 |

3. **标准库、运行环境、已装依赖：通过本门核查。** 排序/克隆使用 slices、编码使用 encoding/json、时间解析使用 time，内容身份使用既有 Git TreeDigest、内容 digest 使用既有 canonicalJSON/encoder.Digest；无新增依赖、自造哈希/锁/文件事务/账本。
4. **更短且语义等价的表达：没有发现足以构成复杂度缺陷的替代。** 必要的 native errors、准入重核、读回校验和所有正常/恢复行为均保留；没有以压缩排版或删除必要验证来要求收缩。下述 P3 是注释和内存 copy 语义建议，不声称换 receiver 是更短的实现，也不据此否决整个方案。
5. **代码量、公开/配置面、重复/过期/未使用逻辑：通过本门核查。** 新增约 431 行生产改动与 405 行测试对应合同的 typed 数据、原子接纳、两类收口及恢复验证；共享提取删除了 seal 核验和 owner 测量重复主干。没有被替代路径并存、第二 receipt/billing/恢复方案或假设性配置面。
6. **过度防御/抽象：未发现有正常使用实质后果的新增项。** receipt digest/identity 校验、发布读回和 immutable Git 对象校验均在已确认合同内。没有因恶意编辑、不可变文件/权限失败或不支持平台提出额外机制或阻塞项。

## P3 建议：JSON 回读未实现注释所称的完全内存分离

稳定消息：**[P3] CheckpointSlice 的 JSON 回读复用既有 pointer，`Detach` 注释过度承诺内存分离。**

- 当前位置：冻结提交 `internal/engine/protocol/slice.go:187` 给 `r.Cost` 赋 `state.Cost`；`:211` 注释声称将 frozen evidence 从 mutable maps/cost 分离；`:216` 解码回已填充的 `r`。该模式在当前 diff 的 checkpoint 构造处出现一次；正常 publisher 的解码只用于证据读回，不宣称 copy owner，不合并为同一问题。
- 已确认准则：注释描述当前方案；receipt 为冻结值；使用标准库和既有 idiom，不新增抽象。仓库 `validation_result.go:230` 已采用 JSON 编码后解码到零值 `next Validation` 的分离写法。
- 正常入口/可达位置：正常 child Start → 路线 → 开发 → 所选门 → PrepareCandidate → Facade.Drive → checkpointDelivered → CheckpointSlice 即执行此代码。新增真实 Git 成功测试证明该位置正常可达。本次没有从这个正常入口复现用户输出损坏，不把内存探针冒充端到端功能失败。
- 原始内存证据：`/tmp/slice-checkpoint-complexity-e030dc3-snapshot/review-probe/main.go` 使用当前真实 `SliceTerminalReceipt`/`RunCost` 类型重复上述赋值、Marshal、Unmarshal；输出：`same cost pointer after current round trip: true`、修改 ledger 后 `receipt observes mutable cost: 23`；解码到零值 receiver 时指针独立且冻结值仍为 17。
- 实际后果/因果：encoding/json 对现有非 nil pointer 对象继续填充，因此该局部回读不是完整 deep copy；注释与实际内存行为不一致。持久提交会把两处值编码进 state，随后 load 解码为独立对象；当前正常/重复/恢复测试证明已持久 receipt 不随后来计量或 HEAD 改变。未发现本块正常路径在构造后修改该共享 cost 导致错误。
- 范围与等级核验：仅当前新增 copy/comment 模式，未扩大到历史问题；不依赖手改内部 state或攻击输入。没有已证实的正常使用后果，故为 P3，不是 P2/P1，不阻塞 PASS。
- 最小建议：若保留“分离内存”约束，在 Unmarshal 前让 receiver 为零值（例如在已保存 data 后 `r = SliceTerminalReceipt{}`），或像现有 prepareValidationResult 一样解码到零值局部变量再使用；保留现有 Marshal/Unmarshal 错误处理，不加通用 clone abstraction。若仅依赖持久冻结，应把注释准确写为实际所保证的边界。这是可选建议。

## 代码工程准则逐项核验

| 准则 | 本次核验结论与证据 |
| --- | --- |
| 正确性 | 本块成功、等待/误操作、重复、查询、恢复、checkpoint 并发与 ordinary terminal 回归执行通过；静态重核跨接缝准入/CAS/publisher 路径。无 P0/P1/P2。 |
| 质量 | 错误逐层上抛，native/副本错误不清理 child；当前测试核完整内容与确切 token 值。内存 copy/comment 有上述 P3 建议，未写成无条件完美通过。 |
| 需求完整性 | 上述逐需求表覆盖当前 child 部分；master 及其他后续块不在本次交付，未判为通过。 |
| 简洁/最简 | 逐新概念核对批准需要及既有归属；无新依赖、第二状态机/队列/计量算法。未确认可证明等价且更短的机制替代。 |
| 耦合 | facade 负责 native/I/O/锁，protocol 负责准入与持久值，persistence 负责信封/提交；typed receipt 为显式接缝携带者，没有跨层读取 master/额外费用参数。 |
| 可维护 | wave/route/identity/owner/digest 沿现有 owner；同业务入口正常与恢复共用。copy 注释建议见 P3。 |
| 高可读 | 函数命名描述核验、checkpoint、发表与观察行为；typed schema 明列携带字段。未发现新增开发轮次/过程命名。 |
| 优雅/设计一致 | 复用 SLICE_READY、PhaseTransition、commitEffects、canonical digest、native tree、Owner 规则；不复制整份 State 当 receipt。属方案/源码核查，不以测试绿替代。 |
| 无不当硬编码 | `SLICE_READY`、固定 sidecar 名与空普通 QA/cases 均对应已确认契约；门集合取 SelectedGates，未写 default/full/custom 成功特例。 |
| 无窄逻辑/过拟合 | checkpoint 消费同一 validationConcluded；negative cases 使用缺失实际门/晋升、真实 Git 脏树/漂移和控制事件；未按测试编号修生产路径。 |
| 无过度设计/防御/抽象 | 复杂度逐维核查见上；没有额外状态、报告、故障公共面、队列或 billing；digest 和 readback 在合同范围。 |
| 测试与业务隔离 | 新 fault 使用既有 persistence.Config FaultInjector；无生产 test-only 分支。新增成功链用 nil runner 对真实 Git操作、work binding 与子身份由既有协议 fixture 产生；没有统一伪造 native 成功响应。 |
| 单一权威事实 | phase/receipt/cost/endpoint 在 child state 的一次 commit；sidecar 可重建，查询派生；无 master map 占位。**通用“控制状态存数据库”条款未判通过**：本合同冻结 state.json 边界，本次未迁移数据库。 |
| 正常与恢复共用 | post-commit Drive、重开后 Drive 均消费同 publisher；pre-commit 从现有 validation/native/owner路径进入同 CheckpointSlice；没有第二业务实现。 |
| 跨接缝携带者 | promotion head/tree；OwnerCost/endedAt；typed receipt/canonical digest；Events/Decisions revision/digests；phase/receipt 查询字段均显式传递。 |
| 四类计数身份不混用 | receipt 用 wave.Generation、wave.ID、target.Task/ResultAction 及 event/decision Revision 原值；未新增计数或以 retry/attempt 冒充 generation。 |
| 替代路径删除 | sealDelivered 重复 native 核验主干与 finishOwnerCost 原测量主干被抽取；child 成功不进入独立 seal/summary/cleanup；legacy 保留路径按非目标保留，没有伪称删除。 |
| 测试期望与诚实 fixture | 期望来自批准合同的 READY/空 QA/精确 Git/tree/token/不清理/恢复不重测；新增测试未修改已有期望。故障点只构造正常可达事务边界；测试前置 engine.Drive settle 的范围已单独说明。 |
| 原始持久内容与业务边 | readSliceReceipt 将 sidecar 完整 typed 值/digest 比对 Load 后 state 值；成功 tests 逐字段比对、replay 核 state/sidecar 原始字节，故障 tests 核 receipt/Owner/endpoint 同存；不只检查存在。 |
| 注释和命名 | 新名称按行为/产品概念组织；移除被替代核验说明；没有新增历史演进叙述或开发顺序码。除 P3 的 Detach 语义外未发现当前新增注释违规。 |
| 范围/报告/费用纪律 | 仅 `/tmp` 验证材料和本报告写入；未修仓库、未编造模型调用或 pass history，无收费调用、无 push。 |

## 实际执行与限制

在冻结克隆、真实 Git 和候选 binary 正常前置准备后执行：

```text
go build -o bin/formal-gates ./cmd/formal-gates
exit 0

go test ./internal/engine/facade -run 'Test(SliceCheckpoint|ConcurrentSliceCheckpoint|SliceChildPromotion|SliceReceipt|.*Terminal|.*OwnerInterval|.*OwnerCost)' -count=1
ok formal-gates/internal/engine/facade 86.109s

go test ./internal/engine/protocol ./internal/engine/vcs ./internal/cost
ok formal-gates/internal/engine/protocol 86.197s
ok formal-gates/internal/engine/vcs 3.383s
ok formal-gates/internal/cost 1.279s

go test -race ./internal/engine/facade -run 'Test(ConcurrentSliceCheckpointFreezesOwnerOnce|ConcurrentOwnerStartsSerializeAndReportTrueIntervals|VanishedPeerBeforeTerminalStillMarksBothUnavailable|TerminalPeerDoesNotOverlapLaterRun|TerminalCheckpointRecoveryDoesNotDoubleAttributeInterval|SliceReceiptRemainsFrozenAcrossReplayAndLaterCommits)' -count=1
ok formal-gates/internal/engine/facade 13.886s

git diff --check 0c5400b93885e1edf1949ee1e4467a9fc44d88a8 e030dc3815c6cf28b54fda8dbbb80f590cbecef1
exit 0; no output

go run ./review-probe
same cost pointer after current round trip: true
receipt observes mutable cost: 23
fresh receiver cost pointer independent: true; frozen total: 17
```

没有执行全量慢包组合、安装/宿主真实会话、canary、付费模型或后续 master 交接；没有将这些未执行项标为通过。没有独立 QA 用例/执行输入，不对独立 QA 完整性背书。当前所见开发者测试和源码足以判断本块新方案未引入阻塞复杂度；其覆盖结论限于实际列出的业务边界。

本门没有范围内 P0/P1，按共享契约返回 **PASS**；P3 建议留给主代理核实和用户决定，不以其引入新机制或另设阻塞。


## 原始任务：/tmp/slice-checkpoint-quality-input.md

[Shared reviewer contract]
用户明确授权：本次不走正式流程，手动跑两门，每次零上下文。此任务由主代理手动组装；prompts/reviewer-base.md 关于必须 CLI 组装的条件仅此处受该明确授权覆盖，其余共享审查契约完整适用。完整读取该文件与 AGENTS.md。只读，不编辑仓库，不启动正式流程，不调用付费模型、不 push。不要读取任何聊天历史、开发者报告、之前的审查/返修结果、.gates/results 或 HANDOFF 中的既往审查记录，不使用其结论。可读取全部相关源码、开发者交付测试及当前需求引用的既有权威条款；必要复现放 /tmp。
[Gate]
完整读取 gates/implementation-quality-gate.md，逐条执行本门原文。
[Current requirement]
完整读取 /tmp/slice-receipt-implementation-contract.md 与 /tmp/slice-checkpoint-execution-brief.md，作为本块已批准需求。完整合同已获批准，执行补充明确当前 child 成功 checkpoint 交付边界及独立 default 身份；master 接纳/汇总随后交付。基于精确 promotion/native identity，child 成功为可恢复非终态 SLICE_READY；冻结完整 typed receipt/digest、费用和 owner 端点于单一事务；副本可恢复发表；普通 regular 终态语义仍适用。按合同逐项判断，不以未实施的后续块代替本块要求。
所有代码工程硬标准必须审查：正确性、质量、需求完整性、最简、耦合、可维护、高可读、优雅/设计一致、无不当硬编码/窄逻辑/过拟合/过度设计防御抽象、测试业务隔离。业务不变量、唯一归属、正常与恢复共用、跨接缝携带者、删除被替代路径/账本/标记、需求来源期望与诚实协议夹具、注释/命名纪律都适用。当前具体冻结 state.json 归属不扩为数据库迁移，不宣称通用数据库条款已满足。
[Current change]
原生 Git 完整比较：0c5400b93885e1edf1949ee1e4467a9fc44d88a8 → e030dc3815c6cf28b54fda8dbbb80f590cbecef1。比较全部交付 diff，沿每一实际改动的调用者、数据、状态和生命周期链核对所有相关入口及正常/重复/并发/恢复等业务边界，不局限单流程或预设反例。只报告本次引入或放大的范围内缺陷，未实施的其他阶段不倒灌。不要用测试绿色推定全部硬标准成立。
[Dispatch]
仓库 /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-dogfood-dsh，已冻结上述 commit，不使用漂移工作树代替快照。测试按本次具体改动与未决风险取最小充分范围；若需运行依赖 Git 或候选 binary 的仓库检查，应按仓库已有步骤在快照中准备正常前置，区分前置缺失与代码缺陷；没有具体风险不机械重复完整慢包组合。当前无独立 QA 交付测试输入，diff 中测试均为开发者交付测试。保持独立，勿派子代理。
[Result contract]
只返回手动门候选结论 PASS/FAIL/RUNTIME_ERROR，明确不是正式流程 PASS。报告须包括审查实际范围、逐需求实现证据、该门逐维结论与各代码准则核验/未验证项。每个发现含等级、具体当前位置/证据、已确认需求、从正常公开入口/常见失误出发的复现、实际后果、范围/因果/等级核验；P0/P1 单独定级理由。同根因合组但核全因果链，别在第一个表现处停止。P3 纯风格/等价表达/范围外加固不阻塞，也不得为其引新机制。共享契约无P0/P1则PASS，P2仍逐项报告供主代理按用户硬标准返修。不得将其他门职责或未验证项写成已通过。报告中不引用开发者声称通过或以前的问题。
报告写 /tmp/slice-checkpoint-quality-e030dc3.md，不要写仓库。


## 原始返回：/tmp/slice-checkpoint-quality-e030dc3.md

# 手动实现质量门候选报告

候选结论：**PASS**。这是用户授权的手动门候选结论，**不是正式流程 PASS**。

范围内未发现 P0/P1/P2。有 1 项 P3 局部注释/冗余表达建议，见下文；不构成功能阻塞。

## 输入与实际范围

任务允许块完整；手动组装已获本次用户明确授权，覆盖共享契约中的 CLI 组装条件。完整读取本次输入、AGENTS.md、prompts/reviewer-base.md、gates/implementation-quality-gate.md、已批准完整实施合同和 child checkpoint 执行补充。未读取历史聊天、开发报告、既往审查/返修结论、.gates/results 或 HANDOFF 既往审查记录。

原生 Git 比较：`0c5400b93885e1edf1949ee1e4467a9fc44d88a8 → e030dc3815c6cf28b54fda8dbbb80f590cbecef1`。完整检查 11 个交付文件，合计 836 行新增、54 行删除：facade.go、owner_cost.go、slice_checkpoint_test.go、snapshot.go；persistence/store.go；protocol/engine.go、owner_cost.go、protocol.go、review.go、slice.go、workspace.go。

执行快照由目标提交 `git archive` 建立，路径为 `/tmp/slice-quality-e030dc3.Zz1rQQ`。以下相对路径均指该冻结快照；没有用后续工作树替代提交。相关调用链覆盖 Start/ClaimSlice、development/validation/candidate promotion、protocol Drive/Plan、facade Drive/Open/Show/Status/Next、checkpoint/seal、owner 观察与 peer/summary 区间读取、state 序列化/提交/恢复以及现有 CLI JSON 投影。新增测试均作为开发者交付测试审查，不把它们视为独立 QA 交付。

本块范围止于 child 成功 checkpoint。完整合同中的 master receipt 接纳、case/cost 汇总、master accepted map 与等待 integration 的汇总投影，根据已批准执行补充留在下一块；不把这些尚未实施职责判作本块缺口。worktree、integration/merge、级联 reset/abort、ABORTED receipt、数据库迁移、canary 均不作本次实现验收。

## 维度一：需求覆盖

结论：**PASS**。先按需求逐项核对实现路径；本维度不以测试绿色作覆盖依据。

| 本块已确认要求 | 对应实现证据与判定 |
| --- | --- |
| child 成功进入可恢复非终态 SLICE_READY | `protocol/slice.go:126-234` 的 CheckpointSlice；`facade/facade.go:895-909` 按 SliceChild 选择 checkpoint。已有相位/迁移表被复用，没有新增相位。路径存在。 |
| 完整当前 wave、所选门/义务闭合、promotion 与当前需求绑定才可成功 | `protocol/slice.go:143-150` 复用 `workspace.go:200-210` 的 validationConcluded，再精确匹配 PromotedSnapshot/PromotedDigest。没有另写 child 达标算法。路径存在。 |
| 屏障、abort、缺门/缺 promotion、身份/脏树错误不发表成功收据 | protocol 原子准入拒绝屏障/abort/未收口/不同 snapshot/tree；`facade.go:964-992` 先检查 HEAD、干净工作区、baseline 祖先和树摘要，失败上抛，publisher 尚未运行。路径存在。 |
| native 核验共用普通 regular 原生交付步骤 | `facade.go:954-992` 的 sealDelivered 只调用 verifyDelivered 并 SealTerminal；child 同样调用 verifyDelivered。重复主干已被替换。路径存在。 |
| receipt、canonical digest、phase、owner cost 与 endedAt 在单事务冻结 | `facade.go:998-1031` 在现有 promotion/owner 协调锁内观察；`slice.go:152-234` 同一 state 中记录 Owner、构造 receipt、相变并调用现有 commit。没有提前单独 RecordOwnerCost 的 child 路径。路径存在。 |
| 完整 typed receipt：身份、版本、native、需求/拓扑/路线、最终验证结果、QA/cases、费用、owner、最后 request/event 身份 | `slice.go:55-116,161-209`；Envelope 来自 `persistence/store.go:75` 的既有身份配置；最终 targets 按键排序、结果从现有 Results 取值；最后记录按持久 Revision 选取。路径存在。 |
| default/full/custom 是独立路线身份，收据保留精确 binding | `slice.go:172-173` 直接携带 RouteMode/SelectedGates，无路线特例或重解释。路径存在。 |
| 分片普通 QA 不选择、不执行，空用例不是 QA PASS | `slice.go:185-186` 显式空 SelectedQA 与 ApprovedCases；最终 gate 结果单独携带，没有伪造 QA 结果。遵守当前 child 路线 owner。路径存在。 |
| 同 checkpoint 不可变；重复不重新计量，同一 child 不同 native identity 拒绝 | `slice.go:137-141` 同 identity 零提交，异 identity 拒绝；`facade.go:859-863,1008-1031` 已冻结值直接发表，不进入 owner 量取。路径存在。 |
| sidecar 是可恢复证据副本，原子发表并核完整 canonical 值 | `facade.go:1037-1075` 从 state receipt 取得值，调用既有 writeJSONAtomic，读回校验 receipt digest 与完整 receipt 的 canonical digest。没有发布状态机、备用队列或第二收据。路径存在。 |
| commit 后发布前/响应丢失恢复不重跑开发、门和 owner | `Drive:835-863` 先走既有 Recover；成功 checkpoint 从 state 重找 receipt，再调用同一 publisher。`Open:766-827` 保留 reader/writer 身份边界。路径存在。 |
| checkpoint 重入只核被冻结不可变对象，后来合法 HEAD 前进不改收据 | publisher 调 Verify(receipt.Snapshot)、TreeDigest(receipt.Snapshot)，没有 Resolve 当前 HEAD 或再次 VerifyReady。路径存在。 |
| child 可继续 Open/Show/Status/Next/Drive，不呈报普通最终完成 | `engine.go:525-534` 的 Plan/Drive 投影改为 WAIT/DEPENDENCIES；`facade.go:1239-1283` status 为 SLICE_READY、附 receipt、无 SummaryPath；`projectOrSummary` 对该非终态返回现存 state；CLI 使用现有通用 JSON 输出。路径存在。 |
| child 不能独立 SealTerminal，不写 terminal summary、不做成功终态 cleanup/EndRun | `workspace.go:154-156` 拒绝 child；`facade.go:916-948` 的 terminal publication/EndRun 只消费 COMPLETE/ABORTED，本成功路径投影为 SLICE_READY。路径存在。 |
| owner exact/UNAVAILABLE/无 binding 沿既有观察算法 | `owner_cost.go:363-414` 抽取 observeOwnerCost，沿原 baseline/overlap/MeasureOwner；无 binding 返回空值，不可核来源保留 unavailable 零值。路径存在。 |
| checkpoint 区间从 state receipt 闭合，不因非终态或 sidecar 未发布永远开放 | `owner_cost.go:76-133,141-198` 读取 state receipt 端点；原有 summary 区间算法扩展同一证据来源，没有独立 endpoint 文件或第二计量算法。路径存在。 |
| 普通 regular 终态继续核 native、保留 publication 重入和既有 owner/终态路径 | `sealDelivered` 共享 verifier 后仍 SealTerminal；finishOwnerCost 仍在 owner 锁内调用 observer 并 RecordOwnerCost；ordinary status/terminal guard/summary/EndRun 条件保留。路径存在。 |

## 维度二：实现与需求偏差

结论：**PASS**。当前改动未把 child checkpoint 偷换为普通终态，也未绕过 validationConcluded、promotion、owner 来源或持久提交 owner。成功 child 的 phase/receipt、空普通 QA 和三种路线 binding 与已批准合同一致。普通 regular 增加 native tree 对 PromotedDigest 的核对，是共用精确 native identity 要求的必要接线；原先 HEAD/ready/ancestor 校验均保留。没有观察到本次改动静默改变普通终态失败或清理行为。

## 维度三：架构与耦合

结论：**PASS**。逐项检查边界、状态生命周期、依赖、失败语义和共享路径：

- receipt 控制事实只由 child State.SliceReceipt 持有；sidecar 可重建，publisher 不决定成功，也不自行记账。没有第二个收齐/发布/费用标记。
- protocol/slice.go 负责 child checkpoint 原子准入和冻结；facade 负责 Git/磁盘/owner 观察。protocol→persistence 的 Envelope 取值复用既有依赖和精确版本 owner，没有新版本台账。
- 普通 seal 与 child checkpoint 只有一个 verifyDelivered；普通与 child owner 只有一个 observeOwnerCost；首次发表与恢复只有一个 publishSliceReceipt。既有 Store.Save/Recover/CAS 是提交与崩溃恢复路径。
- checkpoint 先取 promotion 锁、再取 owner 协调锁；没有持 child store 锁等待 owner/master 锁。state commit 内仍使用现有 store 锁/CAS。同 child checkpoint 并发通过现有协调归一。
- SLICE_READY 保留恢复状态是已确认生命周期职责；成功路径没有 terminal-summary、cleanup 或 EndRun。master 后续退役职责没有被本块自行发明。
- 错误逐层返回：native 不满足时无 checkpoint；持久提交失败时无 sidecar；发表失败时已提交值留存供同入口重试，没有补偿队列或业务旁路。

跨接缝携带者可明确定位：validationConcluded＋PromotedSnapshot/PromotedDigest → verifyDelivered 的 head/tree → CheckpointSlice；OwnerCostFacts/MeasureOwner → OwnerCost＋endedAt → 同一 checkpoint；冻结 typed receipt/digest → sidecar publisher；Events/Decisions.Revision＋digest → 最后 request/event 身份；phase/receipt → Run/Next 查询投影。当前数据库通用准则不在此被宣称已满足：本次沿已批准 state.json 权威边界实现。

## 维度四：实现正确性与质量

结论：**PASS，附 1 项 P3 建议**。安全范围内相关包、构建、并发检查均执行完毕，原始执行结果见下节。测试不替代本节静态判断。

typed schema 与事务代码以业务行为命名；新增错误返回覆盖当前正常失误，没有无关兼容分支。receipt 收集按实际 targets 和 Revision 进行，不靠 map 顺序。state 的 JSON load/save 自然携带新字段；load 与 commit 共用 verifySliceReceipt，phase exemption 随后仍受 receipt identity/digest 校验约束。sidecar 复用既有临时文件原子写 helper，不新增后台资源/缓存/日志体系；未发现本次可证实的无界循环、N+1、明显性能退化或生命周期失主工件。这里不是性能基准结论。

开发者测试使用真实临时 Git 仓库建立正常 child 路径；worker 输入来自本轮签发 Binding，所选 gate 逐项匹配，native commit/tree 和实际 app_test.go 内容被核对。断言覆盖版本、三种路线、最终 wave/结果、QA 空值、owner 已知数值、完整 sidecar/state 对等、重复 bytes、恢复后冻结值与非终态生命周期，不只断言文件存在。持久故障使用既有 FaultInjector，没有新增生产测试分支。higher-level 测试涉及真实 native、owner 锁、文件发表、Open/query 这些 facade 行为，具有该层的验收责任；没有要求独立 QA 材料来背书。

### P3：receipt 构造中的 JSON round-trip 没有实现注释声称的 detach

- 当前位置：`internal/engine/protocol/slice.go:211-218`。
- 需求依据：AGENTS.md 的最简表达和准确描述当前方案；完整合同要求冻结值不可变。这里仅指出局部表达/注释问题，不提出新冻结机制。
- 可达路径：正常 child 开发、完成所选门、promotion 后调用 facade Drive，会进入该 receipt 构造代码。该段先把 `r.Cost` 设为 `state.Cost`，然后把 JSON 解回同一个已含非 nil 指针/容器的 `r`；encoding/json 会复用它们，因而不等于注释所说的脱离 mutable maps/cost projection。
- 独立局部证据：`/tmp/slice-quality-e030dc3.Zz1rQQ/tmp/receipt_probe/main.go` 使用实际 State/RunCost/SliceTerminalReceipt 类型复现同一语句顺序，输出 `same cost pointer after roundtrip: true`，修改 state 总值后输出 `receipt total follows state mutation: 8`。
- 当前实际后果：多了一段没有实现其注释目的的序列化表达，后续维护者可能误以为当次内存值已隔离。**没有证实正常公开路径发生 durable receipt 变动**：state 持久化后，下次 load 会分别解码成本与 receipt，本次重复、并发、恢复和后续 HEAD 测试均保持冻结值。不能据局部 alias 把它定成持久化错误或 P2。
- 范围/因果/等级核验：是本 diff 新增的局部表达；证据只支持当次对象仍共享这一事实，不支持跨事务别名或费用重复主张；影响是注释/可维护性，故 P3。可考虑删除没有必要的 round-trip 并准确注释；只有确需当次隔离时才解到新零值。无需新复制框架、状态或测试旁路。

## 维度五：过拟合与窄逻辑

结论：**PASS**。checkpoint 没有按样例 child/master/slice ID、gate 名或 routeMode 特判；从当前 SliceChild、Topology、RouteMode/SelectedGates、最终 wave/targets 与既有账本投影完整值。QA 的显式空集合是当前已确认分片裁定，不是为了测试打表。native 复用本 engine 已有 Git owner，未伪造其他 VCS 支持。owner 不倒算缺失数字、不给 unavailable 来源估值。未发现测试专用生产路径、断言降级或被替代并行机制。

## 代码与工程准则逐项核验

| 准则 | 本次结论及具体依据 |
| --- | --- |
| 正确性 | 当前 child 成功/失误/重复/并发/恢复与 ordinary regression 的实现链和相关执行证据成立；无 P0/P1/P2。 |
| 质量 | typed receipt、可区分错误、事务 owner、完整持久值断言均在当前代码中；P3 detach 注释建议单列。 |
| 需求完整性 | 上述 17 项当前要求各有实现 owner；不将后续 master 职责写为已实现。 |
| 简洁性/最简 | 复用 validationConcluded、native verifier、owner observer、锁、JSON atomic helper、Store commit；新增数据是已批准 receipt 与一个 State 字段。局部 round-trip 冗余见 P3，不据此要求重做方案。 |
| 耦合 | I/O 在 facade，checkpoint 事实/准入在 protocol，持久化在 store；seam 参数明确，无跨 run 隐式 master 交接。 |
| 可维护性 | receipt schema 与 CheckpointSlice 同在 slice owner，publisher 与 observer 按职责命名；无第二状态机。P3 表达建议如实保留。 |
| 高可读 | 顺序为 load/admission/owner/receipt/phase/commit，native 和发布各自成段；没有样例编号/评审轮次命名。 |
| 优雅/设计一致 | 沿现有流程、业务 owner 与原生 Git tree 能力组织；没有以个人等价设计偏好制造 finding。 |
| 无不当硬编码 | 版本来自 Store.Envelope，ID/bindings/results/cost 来自权威 state；SLICE_READY 与空普通 QA 是已确认产品语义。 |
| 无窄逻辑/过拟合 | 遍历实际所选 targets、保留实际 RouteMode，不针对一条路线或测试 child 写分支。 |
| 无过度设计/防御/抽象 | 无发布队列、额外 endpoint 文件、billing、备用生命周期、公共故障入口、数据库迁移。必要拒绝来自已确认准入边界；P3 局部表达建议不扩大为机制要求。 |
| 测试与业务隔离 | 新增测试只在 *_test.go；持久故障使用既有注入能力；真实 Git 与正确签发 binding 可区别身份，未修改既有期望迎合实现。 |
| 唯一权威 | child State.SliceReceipt 为唯一冻结成功值，sidecar 是证据；phase/cost/端点在同一 commit。通用数据库准则未验收，受当前批准 state.json 边界限制。 |
| 正常与恢复共用 | 两条路径均 publishSliceReceipt；恢复从 Store 找已提交值，不另算费用/重跑模型或门。 |
| 四类轮次/计数区分 | receipt 仅携现有 wave Generation、ledger Revision、target/task/result 身份，不新增或混用 retry/返修计数。 |
| 注释/命名纪律 | 新名描述 checkpoint/receipt/result/identity/observation 行为；无开发批次、修复轮次等过程标记。新增注释无历史演进叙述；P3 的准确性问题单列。 |
| 替代路径删除 | sealDelivered 内重复 native 主干已经提取；成功 child 分流且 protocol SealTerminal 拒绝 child；未删除保留的 legacy 路径，也未伪造不存在的删除成果。 |
| 全流程不变量/副作用边界 | 当前冻结前失败、冻结后发布前、响应丢失、重复发表、并发收口、后续 HEAD/owner 区间案例均有具体当前实现及执行证据；不把未来 master 事务列为已核验。 |

## 实际执行证据

在冻结快照中执行，不调用付费模型，不运行 canary，不安装、不提交、不 push。

1. `go test ./internal/engine/protocol ./internal/engine/facade ./internal/cost ./internal/engine/vcs`：退出 0。
   - protocol：`ok ... 88.188s`
   - facade：`ok ... 454.541s`
   - cost：`ok ... 2.495s`
   - vcs：`ok ... 2.148s`
2. `go test -race ./internal/engine/facade -run 'Test(SliceCheckpoint|ConcurrentSliceCheckpoint|SliceChildPromotion|SliceReceiptRemains|ConcurrentOwnerStarts|TerminalPeerDoesNotOverlap)' -count=1`：退出 0，`ok ... 59.839s`。覆盖本次 checkpoint 的并发、owner 接缝及对应重复/恢复路径；没有机械重复全量慢包组合。
3. `go build -o /tmp/slice-quality-e030dc3.Zz1rQQ/bin/formal-gates ./cmd/formal-gates`：退出 0。
4. 原仓库执行指定两提交 `git diff --check`：退出 0。
5. P3 局部 oracle：首次按绝对单文件调用因 Go internal import 边界拒绝，未执行；放在同模块目录后 `go run ./tmp/receipt_probe` 退出 0，结果如 P3 所列。这是审查临时材料，不是交付测试，也不是候选功能失败。

持久/native 用例的实际断言位置：`slice_checkpoint_test.go:97`（缺门、缺 promotion、脏树/漂移/tree、屏障/abort、独立 seal 拒绝）、`:160`（unavailable owner）、`:170`（并发单次冻结）、`:213`（default/full/custom 完整 identity、typed receipt、QA 空值、state/lifecycle 保留）、`:287`（state/sidecar bytes 与重开、后续 HEAD、owner 闭合）、`:348`（FaultIntentBefore/FaultReplaceAfter/FaultCommitResponseLost 的同 receipt 恢复）。ordinary 终态与 owner 回归在相关包测试中执行。

## 未验证或不属于本门的事项

没有独立 QA 用例正文/逐用例执行证据输入；不将开发者测试绿色写成独立 QA 审查或执行 PASS。本次没有另建安装记录运行独立 CLI 端到端，CLI 的通用输出接线按源码核验；也未声称全仓全部包、性能基准、真实宿主模型、非 Git 平台或 master 后续汇总已通过。未发现需要因此扩大本块范围的具体事实问题。


## 当前冻结对象 e08c468d5b163b9fee270b0d255f929a0e629e6e

主代理独立原失败重验、三路线原生交付和各请求来源检查已执行。重新审查完整基线到本提交的两门均已结束：复杂度门 PASS（1 项 P3 等价排序复用建议）、实现质量门 PASS（无 P0/P1/P2）。两个代理分别为 /root/checkpoint_complexity_independent、/root/checkpoint_quality_independent，均 fresh fork_turns:none。主代理原始证据与逐项验收另记 go-engine-stage5.md。

## 当前原始任务：/tmp/slice-checkpoint-complexity-current-input.md

[Shared reviewer contract]
用户明确授权：本次不走正式流程，手动跑两门，每次零上下文。此任务由主代理手动组装；prompts/reviewer-base.md 关于必须 CLI 组装的条件仅此处受该明确授权覆盖，其余共享审查契约完整适用。完整读取该文件与 AGENTS.md。只读，不编辑仓库，不启动正式流程，不调用付费模型、不 push。不要读取任何聊天历史、开发者报告、之前的审查/返修结果、.gates/results 或 HANDOFF 中的既往审查记录，不使用其结论。可读取全部相关源码、开发者交付测试及当前需求引用的既有权威条款；必要复现放 /tmp。
[Gate]
完整读取 gates/complexity-gate.md，逐条执行本门原文。
[Current requirement]
完整读取 /tmp/slice-receipt-implementation-contract.md 与 /tmp/slice-checkpoint-execution-brief.md，作为本块已批准需求。完整合同已获批准，执行补充明确当前 child 成功 checkpoint 交付边界及独立 default 身份；master 接纳/汇总随后交付。基于精确 promotion/native identity，child 成功为可恢复非终态 SLICE_READY；冻结完整 typed receipt/digest、费用和 owner 端点于单一事务；副本可恢复发表；普通 regular 终态语义仍适用。按合同逐项判断，不以未实施的后续块代替本块要求。
所有代码工程硬标准必须审查：正确性、质量、需求完整性、最简、耦合、可维护、高可读、优雅/设计一致、无不当硬编码/窄逻辑/过拟合/过度设计防御抽象、测试业务隔离。业务不变量、唯一归属、正常与恢复共用、跨接缝携带者、删除被替代路径/账本/标记、需求来源期望与诚实协议夹具、注释/命名纪律都适用。当前具体冻结 state.json 归属不扩为数据库迁移，不宣称通用数据库条款已满足。
[Current change]
原生 Git 完整比较：0c5400b93885e1edf1949ee1e4467a9fc44d88a8 → e08c468d5b163b9fee270b0d255f929a0e629e6e。比较全部交付 diff，沿每一实际改动的调用者、数据、状态和生命周期链核对所有相关入口及正常/重复/并发/恢复等业务边界，不局限单流程或预设反例。只报告本次引入或放大的范围内缺陷，未实施的其他阶段不倒灌。不要用测试绿色推定全部硬标准成立。
[Dispatch]
仓库 /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-dogfood-dsh，已冻结上述 commit，不使用漂移工作树代替快照。测试按本次具体改动与未决风险取最小充分范围；若需运行依赖 Git 或候选 binary 的仓库检查，应按仓库已有步骤在快照中准备正常前置，区分前置缺失与代码缺陷；没有具体风险不机械重复完整慢包组合。当前无独立 QA 交付测试输入，diff 中测试均为开发者交付测试。保持独立，勿派子代理。
[Result contract]
只返回手动门候选结论 PASS/FAIL/RUNTIME_ERROR，明确不是正式流程 PASS。报告须包括审查实际范围、逐需求实现证据、该门逐维结论与各代码准则核验/未验证项。每个发现含等级、具体当前位置/证据、已确认需求、从正常公开入口/常见失误出发的复现、实际后果、范围/因果/等级核验；P0/P1 单独定级理由。同根因合组但核全因果链，别在第一个表现处停止。P3 纯风格/等价表达/范围外加固不阻塞，也不得为其引新机制。共享契约无P0/P1则PASS，P2仍逐项报告供主代理按用户硬标准返修。不得将其他门职责或未验证项写成已通过。报告中不引用开发者声称通过或以前的问题。
报告写 /tmp/slice-checkpoint-complexity-e08c468.md，不要写仓库。


## 当前原始任务：/tmp/slice-checkpoint-quality-current-input.md

[Shared reviewer contract]
用户明确授权：本次不走正式流程，手动跑两门，每次零上下文。此任务由主代理手动组装；prompts/reviewer-base.md 关于必须 CLI 组装的条件仅此处受该明确授权覆盖，其余共享审查契约完整适用。完整读取该文件与 AGENTS.md。只读，不编辑仓库，不启动正式流程，不调用付费模型、不 push。不要读取任何聊天历史、开发者报告、之前的审查/返修结果、.gates/results 或 HANDOFF 中的既往审查记录，不使用其结论。可读取全部相关源码、开发者交付测试及当前需求引用的既有权威条款；必要复现放 /tmp。
[Gate]
完整读取 gates/implementation-quality-gate.md，逐条执行本门原文。
[Current requirement]
完整读取 /tmp/slice-receipt-implementation-contract.md 与 /tmp/slice-checkpoint-execution-brief.md，作为本块已批准需求。完整合同已获批准，执行补充明确当前 child 成功 checkpoint 交付边界及独立 default 身份；master 接纳/汇总随后交付。基于精确 promotion/native identity，child 成功为可恢复非终态 SLICE_READY；冻结完整 typed receipt/digest、费用和 owner 端点于单一事务；副本可恢复发表；普通 regular 终态语义仍适用。按合同逐项判断，不以未实施的后续块代替本块要求。
所有代码工程硬标准必须审查：正确性、质量、需求完整性、最简、耦合、可维护、高可读、优雅/设计一致、无不当硬编码/窄逻辑/过拟合/过度设计防御抽象、测试业务隔离。业务不变量、唯一归属、正常与恢复共用、跨接缝携带者、删除被替代路径/账本/标记、需求来源期望与诚实协议夹具、注释/命名纪律都适用。当前具体冻结 state.json 归属不扩为数据库迁移，不宣称通用数据库条款已满足。
[Current change]
原生 Git 完整比较：0c5400b93885e1edf1949ee1e4467a9fc44d88a8 → e08c468d5b163b9fee270b0d255f929a0e629e6e。比较全部交付 diff，沿每一实际改动的调用者、数据、状态和生命周期链核对所有相关入口及正常/重复/并发/恢复等业务边界，不局限单流程或预设反例。只报告本次引入或放大的范围内缺陷，未实施的其他阶段不倒灌。不要用测试绿色推定全部硬标准成立。
[Dispatch]
仓库 /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-dogfood-dsh，已冻结上述 commit，不使用漂移工作树代替快照。测试按本次具体改动与未决风险取最小充分范围；若需运行依赖 Git 或候选 binary 的仓库检查，应按仓库已有步骤在快照中准备正常前置，区分前置缺失与代码缺陷；没有具体风险不机械重复完整慢包组合。当前无独立 QA 交付测试输入，diff 中测试均为开发者交付测试。保持独立，勿派子代理。
[Result contract]
只返回手动门候选结论 PASS/FAIL/RUNTIME_ERROR，明确不是正式流程 PASS。报告须包括审查实际范围、逐需求实现证据、该门逐维结论与各代码准则核验/未验证项。每个发现含等级、具体当前位置/证据、已确认需求、从正常公开入口/常见失误出发的复现、实际后果、范围/因果/等级核验；P0/P1 单独定级理由。同根因合组但核全因果链，别在第一个表现处停止。P3 纯风格/等价表达/范围外加固不阻塞，也不得为其引新机制。共享契约无P0/P1则PASS，P2仍逐项报告供主代理按用户硬标准返修。不得将其他门职责或未验证项写成已通过。报告中不引用开发者声称通过或以前的问题。
报告写 /tmp/slice-checkpoint-quality-e08c468.md，不要写仓库。


## 当前完整原始返回：/tmp/slice-checkpoint-complexity-e08c468.md

# 手动复杂度门候选报告

候选结论：**PASS**。这是手动门候选结论，不是正式流程 PASS。范围内未确认 P0/P1/P2；有 1 项 P3 等价收缩建议，见文末。

## 输入、冻结身份与实际范围

- 完整读取 `/tmp/slice-checkpoint-complexity-current-input.md` 的全部结构块、`AGENTS.md`、冻结版本 `prompts/reviewer-base.md` 和 `gates/complexity-gate.md`。
- 完整读取批准需求 `/tmp/slice-receipt-implementation-contract.md` 与其执行补充 `/tmp/slice-checkpoint-execution-brief.md`。按补充，本块边界为 child 成功 checkpoint；master 接纳/汇总在后续块，未将其未实施认定为本块遗漏。
- 原生比较基线 `0c5400b93885e1edf1949ee1e4467a9fc44d88a8` → 当前 `e08c468d5b163b9fee270b0d255f929a0e629e6e`，完整阅读 14 个文件的全部 diff。995 行增加、57 行删除；生产改动 10 个文件，开发者测试改动 4 个文件。
- 只读源码依据来自指定提交。执行环境是 `/tmp/slice-complexity-e08c468-snapshot` 的独立本地 Git 克隆、detached HEAD 精确指向该提交，没有用原工作区漂移文件代替快照。未读取聊天历史、开发者报告、以往审查/返修结论或 `.gates/results`/HANDOFF 的历史审查记录。
- 沿 `CLI workflow show/status/next/drive/submit → facade Open/Drive/project → protocol Plan/Drive/CheckpointSlice/SealTerminal → commitEffects/Store.Save/Recover`，以及 candidate promotion、validation wave、owner startup/close/peer lookup、State JSON copy/query 的链条核查。覆盖正常、缺门/未晋升/脏树/漂移、重复、同 child 并发、提交中断及 checkpoint 后合法 HEAD 前进。
- 不启动正式流程，不派子代理，不做真实模型调用、canary、安装、提交或 push；仓库未编辑。

## 本块逐项需求核查

| 已确认要求 | 当前实现与原始验证依据 | 本次结论 |
| --- | --- | --- |
| child 成功只进入可恢复非终态 `SLICE_READY` | `protocol/slice.go:138-247` 在既有相位图上提交 receipt 和 READY；`workspace.go:155` 拒绝 child `SealTerminal`；`facade.go:895-940` 按 child 身份分流，只有 COMPLETE/ABORTED 进入摘要与 EndRun | 本块已实现；无 child 独立成功终态路径 |
| 沿用最终 validation/promotion 准入，不重写 child 达标规则 | `slice.go:155-162` 调用 `errWriteBarrier`、`validationConcluded` 并重核 snapshot/tree；`workspace.go:202` 的既有谓词消费当前 wave、需求、route、promotion；当前负向测试拒缺门、缺晋升、barrier、abort | 无平行准入 owner |
| exact promoted native identity、干净工作区、基线祖先与原生 tree | `facade.go:964-992` 共用 Resolve/VerifyReady/IsAncestorOrEqual/TreeDigest；seal 和 checkpoint 都调用；`vcs/git.go:137-173` 用不可变 commit/tree 对象 | 复用既有 Git provider，无新哈希体系 |
| receipt、phase、OwnerCost 与 endedAt 单事务冻结 | `facade.go:998-1031` 复用 promotion 锁与 owner 锁；`owner_cost.go:389-414` 只返回观察值；`slice.go:164-246` 在一个 state 中 RecordOwner、构造/冻结 receipt、相变并 `e.commit`；提交走 `engine.go:226-259` 的既有 commitEffects | 未先提交 OwnerCost 再 checkpoint；无额外 endpoint 文件或一次性标记 |
| 完整 typed receipt/schema/digest/bindings | `slice.go:54-117`：状态、三类 ID、精确 Envelope、snapshot/tree/digest、需求、拓扑、RouteMode/SelectedGates、最终 wave/结果、QA 选择事实/批准 cases、RunCost、owner 端点、最后 request/event；`persistence/store.go:75` 从已有 envelope owner 取值；`slice.go:223-232` 用 JSON 标准库隔离 mutable payload | 字段职责覆盖本块需求；没有复制完整 State |
| QA 为空表示未选择，不制造 QA PASS | `slice.go:197-198` 明确空数组/空映射；合法 child 路线由既有 `validationKinds/routeHasNoQAModes` 和 route admission 保证不含普通 QA；新增正常测试断言非 nil 空集合、每个 selected gate 对应最终 wave 结果 | 符合机主裁定；不倒灌普通 QA |
| 最后 event/request 从唯一既有账本取最高已接纳 revision | `slice.go:213-221` 从 Events 与 Decisions 按 revision 取最大；`requestDigest:126-132` 外部请求复用 Event digest，内部请求复用完整 PendingAsk offer、排除 settlement flag；测试覆盖外部、validation retry、review grant、recovery 和生成需求 offer | 无新增 last-record 状态或 event ledger；未依赖 map 顺序 |
| 发表副本可恢复，仅消费冻结值 | `facade.go:1037-1075` 共用 publisher：验证冻结 commit/tree、核 canonical digest，沿既有 `writeJSONAtomic` 写 sidecar 并读回核完整 typed 值；`Drive:859-864` 重入直达同 publisher | sidecar 仅证据；无发布状态机、备用队列或第二条恢复业务 |
| 重入不重新量取，合法 HEAD 前进不改冻结身份 | checkpoint 先检查持久 receipt；READY Drive 只发表；publisher 对 receipt.Snapshot 做 Verify/TreeDigest，不用当前 HEAD；开发者 replay/reopen 与中断测试断言 state/receipt bytes、成本、endpoint 不变 | 本块幂等与恢复闭合；具体故障窗口见执行记录 |
| state/恢复信息与 child lifecycle 保留 | READY 不是 TERMINAL；`projectOrSummary:1195` 对该状态直接返回；正常测试断言 intake/validation/results/dispatch bindings/receipt 保留、无 terminal-summary/intent、ActiveRunIDs 仍含 child | 未提前清理 state 或 EndRun |
| query/Plan 解释 checkpoint | `engine.go:530-531` 把 READY 投影为 WAIT dependencies；`facade.go:1240-1283` 返回 SLICE_READY、receipt、空 SummaryPath；Show/Status/Next 用同一 project；CLI 直接编码该 Run/Next | 无新增 public 写命令或第二投影状态 |
| owner exact/unavailable/无绑定与 peer 闭区间 | `observeOwnerCost` 保留原 MeasureOwner/overlap 语义；`owner_cost.go:105-120,147-195` 在已有查找 owner 内认识 state receipt 的端点，发表前即闭合；新增测试已知答案 17 input=2 hit+15 miss、8 output，unavailable 零数字，无绑定字段为空，并核后续新 run 6 input/2 output | 不因非终态把已闭区间当永久开放；未新增计量算法 |
| 普通 regular 终态仍走既有语义 | `sealDelivered:954-959` 只换为共享 verifier，再调用原 SealTerminal；原终态摘要/cleanup/EndRun 分支保留；owner observer 抽出后普通路径仍 RecordOwnerCost | 静态无偏离；原 ordinary native/promotion/publication/replay 回归包含于 façade 包执行 |
| 本块仅 child 成功 checkpoint，不实施 master 接纳/汇总 | diff 不含 master accepted map、cost import helper、case projection、integration/merge/cascade/guarantee | 边界守住；这些后续工作不在本次验收结论中 |

## 复杂度门逐维结论

| 本门原文维度 | 核查结果与依据 |
| --- | --- |
| start-readiness/批准实施边界 | 生产改动均落在执行补充列出的 façade/protocol/persistence native identity、owner 与 query 接缝；无路线重构、worktree/integration 或 master 接纳。未发现范围变化，继续本门。 |
| 1. 需求之外的内容 | 新 receipt、typed final evidence、状态字段、查询字段、checkpoint/publisher 都有合同直接职责。新增测试文件组织为 slice checkpoint 行为；其它测试增加 request evidence 断言。 |
| 2. 仓库已有可复用 owner | 沿用 SliceChild/Topology、validationConcluded、PromotedSnapshot/Digest、existing phase、commitEffects/Save/Recover、Git Provider、owner coordination/MeasureOwner/RecordOwner、writeJSONAtomic、canonicalJSON/encoder.Digest。只发现 target key 排序未复用既有 helper，列 P3。 |
| 3. 标准库/运行环境能力 | receipt 深拷贝使用 encoding/json；列表复制/排序使用 slices；时间解析使用 time；native tree 直接 Git rev-parse。无自造 canonical/hash/deep-copy 框架。 |
| 4. 已安装依赖 | go.mod/go.sum 未变；没有新增依赖，也未新建可被现有依赖替代的组件。 |
| 5. 更短且行为等价实现 | `sortedTargetKeys(wave)` 可替换 `slice.go:204-209` 的临时收集排序；具体等价性与替代见 P3。未确认其它必须增加机制的缩减方案。 |
| 代码量/文件数与需求相称 | 主要新增生产体量是完整 receipt schema、事务投影和副本发表；现有 owner 观察抽出并复用，无整个 pipeline 重写。开发者测试主要覆盖三条路线、native identities、atomic owner、replay 和故障窗口。 |
| 新概念和公开面必要性 | Receipt wrapper 为 canonical digest 与 typed payload 的接缝；validation/result/event/request 子类型只承载最终证据；Store.Envelope 是精确已有 envelope 的读取面。CheckpointSlice 为 facade 已核 native proof 的提交入口，未新增 CLI 写命令/配置/阶段。 |
| 重复/过期/未使用逻辑 | sealDelivered 原 native 核验主干已被共享步骤替换；原 owner 量取主干由 observeOwnerCost 单持有；child 不走普通成功 seal/summary 路径。legacy 参考路径未被本任务替代，保留合理。P3 排序是唯一确认的等价重复。 |
| 过度防御/假设场景 | receipt digest/native object/readback、load/commit integrity 是批准持久证据和恢复要求；没有围绕恶意状态编辑、权限失败或未来平台增加机制。 |
| 方案优先 | 成功值落现有 child state，sidecar 从其重建，owner 观察沿原归属；没有另建系统后再证明其自洽。无需以未实施 master 工作否决当前方案。 |

新持久概念逐项核对：`State.SliceReceipt` 保存成功 checkpoint 的唯一值；`SliceReceipt` 包住值与 digest；`SliceTerminalReceipt` 保存合同要求的 payload；`SliceValidationReceipt` 和 `SliceValidationResult` 排除 wave 的运行中控制状态但保留最终身份/结果；`SliceEventIdentity` 与 `SliceRequestIdentity` 保存已有账本的最终证据；`Run.SliceReceipt` 为只读查询。SliceChildBinding 仅是启动绑定，terminal summary 属终态，ValidationWave 是运行中状态，这些既有结构均不能完整替代当前成功非终态证据；代码没有为这些字段另建控制 owner。

## 各项工程硬标准核验

| 代码准则 | 当前证据及结论 |
| --- | --- |
| 正确性 | 准入、native proof、同事务冻结、单 publisher 的调用链一致；负向、重复、owner、恢复测试执行如下。未用测试绿代替静态业务因果核查。 |
| 质量 | 错误向上返回；失败前不发表 receipt；读回核 typed content/digest；已有 Store.Save/CAS 负责事务。未发现范围内质量缺陷。 |
| 需求完整性 | 上表逐项覆盖 child 块；master 汇总及非目标明确不验收。 |
| 最简/简洁性 | 复用既有 owner/原生能力；有 1 项非阻塞 P3 排序收缩，故不写无条件“完全最简”。 |
| 耦合性 | façade 负责 Git/IO/观察，protocol 负责准入与唯一事务，persistence 负责提交；跨接缝只传 head/tree、OwnerCost/endedAt 或完整 receipt/digest，未传 guard 下层字段。 |
| 可维护性 | native 核验和 owner 量取只有一个主干；正常/恢复发表同函数；未出现并行账本/替代路径共存。 |
| 高可读性 | 结构按证据职责命名；事务分为准入→证据投影→相变→commit，publisher 从 frozen value 出发；错误明确定位 snapshot/tree/receipt。 |
| 优雅/设计一致性 | 风格沿既有 facade/protocol 分层与 commitEffects 单 owner；JSON copy 和 existing canonical digest 复用已有编码语义。没有以个人风格偏好否决等价表达。 |
| 无不当硬编码 | 固定 SLICE_READY/COMPLETE、receipt 文件名和 QA 空集合是已批准产品/协议词汇；run/slice/master/revision/topology/gates/native identity/成本从事实投影，未按测试 ID 打表。 |
| 无窄逻辑/过拟合 | head/tree 与 wave bindings 为通用值检查，default/full/custom 走同一管线；request evidence 区分 external/generated 的真实归属，不列举 retry 个例；peer closure 识别合法 receipt 形态。 |
| 无过度设计/防御/抽象 | 无新生命周期、registry、receipt queue、发布状态机、费用 ledger 或锁体系；typed schema 与完整性核验直接对应合同。P3 重复排序如实保留为建议。 |
| 测试与业务隔离 | 故障注入使用既有 persistence.Config/FaultPoint，仅在测试构造 store；没有新测试专用生产分支、公开故障面或绕过 admission 的生产路径。 |
| 一个控制事实一个权威 | READY/receipt/cost/endpoint 同 child State 提交；sidecar 从冻结值派生；last identities 从 Events/Decisions/PendingAsks 派生，无第二 authority。当前按批准 state.json 边界核查，**未验收/未宣称“控制状态存数据库”的通用条款已满足**。 |
| 正常与恢复共用 | Drive 正常 checkpoint 后、READY 重入和 Recover 后都调用同 publisher；恢复不重做 native 达标或 owner 量取。 |
| 跨接缝携带者 | final wave/promotion → head/tree → CheckpointSlice；OwnerCost+endedAt → 同 commit；frozen receipt+digest → publisher；phase/receipt → project。会话/run、需求、route、wave、generation 已在既有 binding 中携带，未新加另一身份计数。 |
| 四类轮次事实不混用 | receipt 只复制 Validation.Generation、WaveID 和 WorkerResult/Task binding；不新计 invocation/retry/repair round，不用成本重算额度。 |
| 移植/语义等价 | seal 主干保留 Resolve/VerifyReady/ancestor，并将 tree 校验纳入共同 delivery；owner 原观察算法保留，只把落账留给不同收口事务。没有移植另一套业务实现。 |
| 删除被替代路径/账本/标记 | 原 sealDelivered 中重复 native 段已删除；原 finishOwnerCost 中量取段替换为 observer 调用；child 成功不再走 SealTerminal；没有遗留新旧 publisher/owner ledger 并存。 |
| 测试期望来自需求 | 新测试断言 READY 非终态、空 QA、exact native/tree、完整 schema、持续 ActiveRunID、sidecar/state 不变、一次 OwnerCost；原测试只增强 request evidence 断言，未降低期望。 |
| 诚实协议夹具 | child 正常链用真实 Git；candidate materialize helper 核 op.candidate 的实际参数/输入绑定，再读取实际 HEAD/tree；请求测试区分是否有外部 event，核完整 original offer 与决定 digest。未用统一返回成功覆盖不同 native identity。 |
| 持久内容原始证据 | `readSliceReceipt` 比较 sidecar 全 typed 值与 state 并重算 digest；正常测试逐字段核 schema/成本/门结果；replay 比较 state 与 sidecar bytes；中断测试核提交后 receipt/Cost.Owner/endpoint 同在。 |
| 全流程与副作用边界 | 覆盖真实 Start→开发→门→promotion→Drive，提交前/replace 后/response lost、发表后重复 Drive、合法 HEAD 前进、缺 sidecar 重建、同 child 三 façade 并发；没有只沿函数结构断言。 |
| 注释纪律 | 新改注释描述当前 owner、checkpoint、冻结值、恢复和格式；未新增开发历史叙述或旧/新机制演进记录。 |
| 命名纪律 | 新生产/测试名称按 slice checkpoint、receipt、generated request、owner 的行为组织；无开发批次/轮次/Fix 编号。开发 Batch 与业务 QA 词汇在既有正式域含义中使用，不误判为过程标记。 |
| 派发/记账/付费纪律 | 本人仅只读审查与 /tmp 验证；没有实施代码、真实付费调用、提交或 push。开发派发历史不属本次独立输入，未从未读历史推定合规。 |

## 实际执行与未验证边界

- 原生构建：`GOPROXY=off go build -o /tmp/slice-complexity-e08c468-formal-gates ./cmd/formal-gates`，退出 0；未运行该候选 binary 的 workflow 写操作。
- `git diff --check 0c5400b93885e1edf1949ee1e4467a9fc44d88a8 e08c468d5b163b9fee270b0d255f929a0e629e6e`，退出 0。
- `GOPROXY=off go test ./internal/engine/protocol ./internal/engine/facade ./internal/cost ./internal/engine/vcs`，退出 0：protocol 59.413s、facade 511.806s、cost 3.552s、vcs 3.773s。原始输出 `/tmp/slice-complexity-e08c468-packages.log`。这次完整 façade 包包含原 ordinary regular native/promotion/publication/replay 测试。
- `GOPROXY=off go test -race ./internal/engine/facade -run 'Test(Slice|ConcurrentSlice|ConcurrentOwner|TerminalPeer|TerminalCheckpointRecovery)' -count=1`，退出 0，`ok formal-gates/internal/engine/facade 109.871s`。原始输出 `/tmp/slice-complexity-e08c468-race.log`。
- 测试命令均禁用依赖网络下载，使用已装本地依赖，没有付费模型调用。未运行全仓库慢包组合、独立 QA 或 canary；未核 master 接纳/汇总/并发跨 child/integration/abort cascade，这些为后续块。没有将其它门职责或上述未验证项写成已通过。
- 快照源码保持 clean；最终精确 HEAD 在执行记录中核对。

## P3 发现：复用已有 validation target 顺序归属

- 身份：`CPL-TARGET-ORDER`；等级：**P3**，不阻塞 PASS。
- 当前位置：`internal/engine/protocol/slice.go:204-209` 新增 key 收集和 `slices.Sort`；现有等价 owner 为 `internal/engine/protocol/validation.go:597-604` 的 `sortedTargetKeys`，已有 gate、control、execution、guard 等调用。
- 已确认依据：AGENTS 最简顺序“仓库已有可复用的归属者 → 复用或改造既有者”，复杂度门第 2 与第 5 项。不是功能准入或需求字段缺失。
- 正常公开路径：合法 child 经 Start→路线确认→开发结果→所选 gate 结果→PrepareCandidate→Drive，进入 CheckpointSlice；每次构造 final validation receipt 都执行这段重复排序。当前新增真实 Git 三路线测试能到达该路径。
- 当前实际后果：同一排序职责重复 5 行，只增加维护体量；未确认用户行为、错误处理、持久值或费用后果。因此不能定为 P2/P1。
- 具体替代：删除 `keys := ...`、map 收集与 `slices.Sort(keys)`，将循环改为 `for _, key := range sortedTargetKeys(wave) { ... }`，保留整个循环体。
- 等价核验：两者都只读取同一个 `wave.Targets`，为其 string keys 分配独立 slice 并按升序排序；`sort.Strings` 与 `slices.Sort` 对 string 的序相同，空集合都返回空 key 集；循环中的 target/result 读取和 append 完全不动；没有写副作用、错误分支、验证或安全行为被删。现有 receipt 的结果顺序和 canonical digest 因而不变。
- 范围/因果/等级复核：重复是本次新增；依据直接在同 protocol 包可复用；没有引出新机制或加固；只作等价收缩建议，按共享契约 P3 不引新审查返修循环。


## 当前完整原始返回：/tmp/slice-checkpoint-quality-e08c468.md

# 手动实现质量门候选报告

结论：**PASS**。这是本次人工门审的候选结论，不是 formal-gates 正式流程 PASS。未发现本次引入或放大的范围内 P0、P1、P2。没有待主代理复现的 finding。

## 输入与实际审查范围

- 审查身份：原生冻结 commit `e08c468d5b163b9fee270b0d255f929a0e629e6e`；完整比较基线 `0c5400b93885e1edf1949ee1e4467a9fc44d88a8`。
- 完整读取 `/tmp/slice-checkpoint-quality-current-input.md`、仓库 `AGENTS.md`、`prompts/reviewer-base.md`、`gates/implementation-quality-gate.md`、完整已批准合同 `/tmp/slice-receipt-implementation-contract.md` 和补充 `/tmp/slice-checkpoint-execution-brief.md`。输入块有效；手动组装按本次明确授权适用。
- 完整检查原生 diff 的 14 个文件（995 行增加、57 行删除）。追踪 protocol 的 Plan/Drive、validation/promotion/phase、commitEffects/加载完整性、Ask/Event/Decision 身份，facade 的 Drive/Open/Show/Status/Next/projectOrSummary、native Git、sidecar、owner 测量与 peer overlap，以及持久化、成本、CLI JSON 投影和开发者测试夹具。
- 本块仅 child 成功 checkpoint。master 接纳、master 成本/cases 汇总、integration 与级联生命周期为执行补充明确留给后续块的工作，不作为本块缺失项。
- 未读取聊天历史、开发者交付报告、既往门审/返修结果、`.gates/results` 内容或 HANDOFF 中的审查记录；未使用其他审查者结论。只写本报告与 `/tmp` 构建产物，没有修改仓库、派发子代理、启动正式流程、付费调用、提交或 push。

## 逐需求实现证据

| 本块已确认要求 | 当前实现路径与审查结论 |
| --- | --- |
| 成功 child 仅进入 SLICE_READY，不能独立 seal | `protocol/slice.go:138` 的 CheckpointSlice 仅接纳 regular child；`:155` 复用 errWriteBarrier、abort 与 validationConcluded；`:233` 经既有合法相位边进入 SLICE_READY。`protocol/workspace.go:153` 的 SealTerminal 拒绝 child。覆盖存在且语义一致。 |
| 每个所选门、当前需求/拓扑/路线及 promotion 绑定有效 | `protocol/workspace.go:201` 复用既有 validationConcluded；`protocol/slice.go:172` 从当前 wave 投影结果，按 target key 排序并携带原结果；`:182` 携带当前 requirement、Topology、RouteMode、SelectedGates。缺门/缺 promotion 等拒绝路径见开发者测试 `slice_checkpoint_test.go:156`。 |
| 首次核精确 promoted snapshot/tree、clean workspace 与 base ancestry | `facade/facade.go:964` 的 verifyDelivered 为 seal/checkpoint 共享 owner，逐项核 Resolve、PromotedSnapshot、VerifyReady、IsAncestorOrEqual、TreeDigest/PromotedDigest；checkpoint 在 promotion 锁与 owner 协调锁内调用该函数。 |
| receipt/digest、owner 原值和 endpoint 与相位同一事务冻结 | `protocol/slice.go:164` 调已有 cost.RecordOwner；`:173` 构造 typed receipt；`:224` 按权威 JSON 编码深拷贝；`:232` 存 SliceReceipt/digest；`:246` 只提交一次。没有先 RecordOwnerCost 后另写 checkpoint 的路径。 |
| 完整 durable typed receipt，空 QA 明确未选择 | `protocol/slice.go:54` 的 schema 携带状态、三方 ID、完整 Envelope、native snapshot/tree/digest、需求/拓扑/路线、最终 wave/targets/results、成本、owner 区间及最后 request/event 身份。`:197` 明确空 SelectedQA/ApprovedCases，未制造 QA PASS。Envelope 由 `persistence/store.go:70` 从现有版本 owner 提供。 |
| 最后 request/event 按 revision 与权威来源取值 | `protocol/slice.go:213` 使用 EventRecord.Acceptance.Revision；`:218` 使用 RecordedDecision.Revision。`:126` 外部 request 从 Events 取 canonical digest，generated request 从原 PendingAsk offer 取值并排除 settlement 标记。`engine.go:250` 每次事件提交保存一条 acceptance，CAS revision 是排序事实；没有新增“最后记录”账本。 |
| 副本原子发表、读回完整 typed 内容；失败不终态 | `facade/facade.go:1037` 的 publishSliceReceipt 从 state-owned receipt 发表，验证 canonical digest 与不可变 snapshot/tree，经既有 writeJSONAtomic 写入并读回校验；错误直接返回。sidecar 不是控制状态，无发布状态机或备用队列。 |
| 重复 Drive/Open 恢复从同一冻结值补发表，不重新判门/计量 | `facade/facade.go:859` 的 READY 重入先补发表并投影；`:1008` 的 checkpointDelivered 仅在 receipt 缺席时核验/观察/提交。Open 的原读路径与 Drive 的 store.Recover 保持原归属，发表使用同一函数。native 重入核被冻结对象，不重新核后来 HEAD。 |
| READY 可 Open/Show/Status/Next/Drive；无成功终态摘要、cleanup/EndRun | `protocol/engine.go:530` 将 READY 投影为等待；`facade/facade.go:1240` 状态为 SLICE_READY，`:1276` 投影 receipt 并清空 SummaryPath。Show/Status/Next 经 projectOrSummary。Drive 的终态收尾仅在 COMPLETE/ABORTED 状态执行（`:916`），成功 READY 不进入。 |
| owner exact/unavailable/无 binding 保持原语义；closed child 不永久开放 | `facade/owner_cost.go:388` 的 observeOwnerCost 复用 overlap/MeasureOwner 算法；ordinary finishOwnerCost 仍用同一观察并调用 RecordOwnerCost。`:114` 从 state receipt 识别 child endpoint；`:141` 的区间相交查询消费同一 receipt，无新 endpoint 文件或算法。 |
| 普通 regular 继续原 terminal/publication 语义 | sealDelivered 是共享 verifier 到原 SealTerminal 的薄调用；Drive 的终态摘要、snapshot guard、owner 收口与 lifecycle.EndRun 保持原入口。相关 dirty/drift、publication/replay、squash 与 owner 回归已独立运行。 |

## 五个质量维度的独立结论

1. **需求覆盖：PASS。** 上表逐项核对本块实现路径，没有发现已批准 child 要求缺少入口或明确错误路径。此维度只以源码路径核对覆盖，未以测试绿色代替覆盖判断。
2. **实现与需求偏差：PASS。** child 与 ordinary regular 在同一 native verifier 后分流；child 复用现有 validationConcluded；READY 与独立最终 COMPLETE 有不同投影和副作用边界。收据 QA 为空符合裁定，route identity 原样携带 default/full/custom。重入消费 frozen receipt，没有重执行达标判断或 owner 测量。
3. **架构与耦合：PASS。** checkpoint 业务归 protocol/slice.go；native/owner I/O 归 facade；版本身份归 persistence；相位/成本/receipt 经 commitEffects/CAS 单一提交。state receipt 是成功事实，sidecar 是可重建副本。复用 promotion/owner 协调锁，顺序为 promotion → owner → store 提交，没有持 child store 锁等待跨 run 协调锁。没有新增 master placeholder、收据队列、owner ledger、endpoint 文件、公开写命令或第二生命周期。
4. **实现正确性与质量：PASS。** 当前 diff 的拒绝、重复、并发、恢复、深拷贝、canonical digest、成本原值与查询/终态副作用边界都已按业务链核对。相关执行结果见下节。错误保持可辨且向上传递；sidecar 临时文件由既有 helper 退役。没有确认到新增资源泄漏或可观察的性能退化。新增测试由正常 child 流程产生可达前置，并断言原始持久内容与具体值，未引入生产测试分支。
5. **过拟合与窄逻辑：PASS。** 实现循环消费当前 wave targets 和 state binding，不按 fixture gate 名、固定 child ID、某次 revision 或固定成本值分支。空普通 QA 是已确认业务规则；成功相位、sidecar 文件名与 Envelope 是协议词汇。default/full/custom 走同一路径；没有为某个反例另设旁路。

## 代码工程准则逐项核验

以下结论仅针对完整当前 diff 及上列因果链；功能验证不代替工程核验。

| 硬标准 | 结论与当前证据 |
| --- | --- |
| 正确性 | 本块通过；admission、native identity、commit、replay/publication、owner interval 与 ordinary 回归证据见上表和执行记录。 |
| 质量 | 本块通过；持久边界错误向上传递，snapshot/tree 与 receipt digest 都核值；没有确认到资源卫生缺陷。 |
| 需求完整性 | 本块通过；逐需求表覆盖本块全部 child 条目；master 后续边界明确排除。 |
| 最简/无过度设计 | 本块通过；复用 state 字段、commitEffects、现有 phase、native verifier、owner 观察和原子写 helper。receipt/digest、typed result 和 Envelope 取值满足批准数据职责，没有规格外配置/系统。 |
| 耦合 | 本块通过；协议只消费外部 identity/owner 值，Git/transcript I/O 未进入协议；副本和事实源分离。 |
| 可维护性 | 本块通过；checkpoint、publisher、shared verifier、owner observer 按各自职责拆分；没有两份正常/恢复业务。 |
| 高可读 | 本块通过；新增类型字段按 receipt 域概念命名，准入、投影、冻结和提交顺序直接可读，成本/端点含义在 owner 处声明。 |
| 优雅/设计一致 | 本块通过；这里按已有 owner 和项目规则判定，未把等价表达偏好升级为缺陷。JSON 深拷贝沿用仓库 cloneState 的权威编码做法。 |
| 无不当硬编码 | 本块通过；身份、版本、需求、拓扑、路线、门、费用均从权威状态取得，固定词汇来自已批准契约。 |
| 无窄逻辑/过拟合 | 本块通过；任意合法 wave target/绑定值走同一代码，不按被观察样例定行为。 |
| 无过度防御/抽象 | 本块通过；仅新增所需 receipt schema、checkpoint admission、投影/发表与 store Envelope 读取，没有新通用 billing、异常恢复框架或队列。 |
| 测试与业务隔离 | 本块通过；故障注入复用既有 persistence.FaultPoint，生产代码没有 test-only 分支或新故障公开面。 |
| 唯一事实源 | 本块通过；phase/cost/receipt/endpoint 属同一 State，sidecar 无控制权。**通用“控制状态存数据库”条款未被本块满足或验证**；按已批准 state.json 边界审查，没有扩大迁移。 |
| 正常与恢复共用 | 本块通过；同一 publishSliceReceipt 消费持久 frozen value，恢复只发现/重发；ordinary 和 child 共用观察/核验函数。 |
| 跨接缝携带者/计数区分 | 本块通过；wave/promotion/requirement/topology/route → receipt，owner value/endedAt → 同提交，Event/Decision revision+digest → 最后身份，phase+receipt → query。没有把 retry、generation、调用轮次或业务返修计数互相冒用。 |
| 删除被替代路径/账本/标记 | 本块通过；sealDelivered 内完整 native 核验已抽到唯一 verifyDelivered；child 成功 COMPLETE → SealTerminal/终态收尾路径已被分流，SealTerminal 自身也拒 child。legacy 保留路径未被本任务替代；没有新增并行账本或替代标记残留。 |
| 诚实测试、需求来源期望 | 本块通过；新 facade 用例使用真实 Git root/commit/tree，从开发到所选门再 promotion；既有 frozenGitRepo 按命令与 rev 实参区分 tree/commit。generated request oracle 对完整未解决 offer 计算 canonical digest，断言 request/event identity；没有统一 mock 成功值。 |
| 全流程/副作用恢复边 | 本块通过；新用例覆盖正常三路线、等待/误操作、并发 checkpoint、state/receipt bytes 冻结、合法后来 commit、commit 前及 commit 后副本前中断，和 closed endpoint 在副本缺失时的 owner 判定。未把独立 QA 测试当本门输入。 |
| 注释/命名纪律 | 本块通过；新增源码/测试名称描述 child checkpoint、receipt、request evidence 或 owner 行为；新增注释写当前契约，未见开发批次/轮次身份或历史对比叙述。旧 cost/phase owner 注释已在涉及处替换为当前语义。 |
| 值级语义与对外协议保真 | 本块通过；普通 run 仍原 SealTerminal/publication；child 增加的是批准 READY/receipt 投影，QA 裁定和 route identity 不被静默改写。 |

## 独立执行与持久内容核验

- `go build -o /tmp/slice-checkpoint-quality-formal-gates-e08c468 ./cmd/formal-gates`：退出 0。
- `git diff --check 0c5400b93885e1edf1949ee1e4467a9fc44d88a8 e08c468d5b163b9fee270b0d255f929a0e629e6e`：退出 0。
- 初始组合 `go test ./internal/engine/protocol ./internal/engine/facade ./internal/cost ./internal/engine/vcs ./internal/engine/persistence`：protocol 已返回 `ok`（83.616s）。完整 facade 在普通 host-scope 场景仍持续运行约 5 分半时主动停止该进程组，退出 143；**完整 facade 包未完成，未记通过，也未将人工中止解释为代码缺陷**。
- 随后按实际风险收窄独立执行：

  `go test ./internal/engine/facade -run 'TestSlice|TestConcurrentSlice|TestTerminalCompletion|TestTerminalDriveReplay|TestTerminalSealingRejectsDirtyTreeAndDriftUntilReorganized|TestTerminalPublicationRejectsDriftAfterSealVerification|TestTerminalCheckpointDriftRejectsPublicationUntilContentRestored|TestConcurrentOwnerStarts|TestVanishedPeer|TestTerminalPeer|TestTerminalCheckpointRecovery|TestCandidatePromotionSquashesMultiCommitRangeAndCompletesTerminal' -count=1`

  返回 `ok formal-gates/internal/engine/facade 47.316s`，退出 0。
- `go test -race ./internal/engine/facade -run 'TestConcurrentSliceCheckpointFreezesOwnerOnce|TestSliceReceiptRemainsFrozenAcrossReplayAndLaterCommits|TestConcurrentOwnerStartsSerializeAndReportTrueIntervals|TestTerminalCheckpointRecoveryDoesNotDoubleAttributeInterval' -count=1`：`ok`，17.784s，退出 0。
- `go test ./internal/cost ./internal/engine/vcs ./internal/engine/persistence`：三包 `ok (cached)`，退出 0。明确为本机 Go cache 结果，未伪称重新跑完三包。
- 读取开发者测试原文并执行相关用例：`slice_checkpoint_test.go:294` 核完整 identity/envelope/topology/route，`:305` 核 final wave/result 绑定，`:309` 核显式空 QA/cases 与完整 cost，`:312` 核最后 request/event，`:331` 读 sidecar 并核 typed 全文与 state 值相等，`:377` 核两份持久字节不变，`:395` 核 closed child 不误伤后来 owner，`:409` 核 checkpoint 事务中断后冻值/成本/端点不分裂。没有只用“文件存在”背书关键工件。

## 未验证项与边界

完整 facade 包、仓库全量套件、全包 race 未完成或未执行；本块相关 facade/native/owner 与并发接缝已按上列范围完成。没有重新执行独立 QA，未核 master 接纳/汇总、integration、级联终结、真实模型/provider 付费调用或不受支持平台。此类未验证项不写成通过，也不倒灌为本块缺陷。

## 原始执行证据索引

[本次原始执行证据索引](/tmp/slice-checkpoint-quality-e08c468-evidence/index.md)。原命令未将终端输出落文件，现按原工具回传完整块保存；被中止 facade 的原生 Go testlog 另按原文件完整复制。protocol 的 ok 来自被中止整包 session，不存在独立 protocol 重跑或独立退出码。


## 主代理核原始执行输出

复杂度门相关包与 race 原 stdout：

```text
ok  	formal-gates/internal/engine/protocol	59.413s
ok  	formal-gates/internal/engine/facade	511.806s
ok  	formal-gates/internal/cost	3.552s
ok  	formal-gates/internal/engine/vcs	3.773s
ok  	formal-gates/internal/engine/facade	109.871s
```

### 质量门原始执行证据索引

# 本次执行原始证据索引

原命令未将 stdout/stderr 重定向到文件。本目录的 `*.tool-output.jsonl` 逐块保存本次对话中原工具完整返回文本，保留原 chunk ID、session ID、退出码、output 及时间；它们是原始工具回传块副本，不是重新运行产生的终端日志，也没有将摘要扩写为日志。

| 执行 | 原始工具回传副本 | 说明 |
| --- | --- | --- |
| 被中止的整包组合 | [whole-packages.tool-output.jsonl](/tmp/slice-checkpoint-quality-e08c468-evidence/whole-packages.tool-output.jsonl) | session 33277 的所有已返回块按时间保存；组合命令最终退出 143，停止前仅返回 protocol 的 ok 行。没有 facade 整包通过输出。 |
| facade 原生测试操作日志 | [whole-packages-facade.native-testlog.txt](/tmp/slice-checkpoint-quality-e08c468-evidence/whole-packages-facade.native-testlog.txt) | 原 `/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/go-build557623923/b145/testlog.txt` 仍存在，直接复制完整文件。它是 Go 原生 open/stat 操作 testlog，不是测试通过结论或 stdout/stderr。 |
| 停止整包命令 | [whole-packages-stop.tool-output.jsonl](/tmp/slice-checkpoint-quality-e08c468-evidence/whole-packages-stop.tool-output.jsonl) | 原 `kill -TERM -- -53038` 工具返回。 |
| facade 定向 | [facade-targeted.tool-output.jsonl](/tmp/slice-checkpoint-quality-e08c468-evidence/facade-targeted.tool-output.jsonl) | 原 session 18077 的启动与完成块。 |
| facade race | [facade-race.tool-output.jsonl](/tmp/slice-checkpoint-quality-e08c468-evidence/facade-race.tool-output.jsonl) | 原 session 98749 的启动与完成块。 |
| protocol | [protocol.tool-output.jsonl](/tmp/slice-checkpoint-quality-e08c468-evidence/protocol.tool-output.jsonl) | 整包组合 session 33277 中包含 protocol ok 的完整原返回块；没有另跑一次 protocol，也没有独立 protocol 进程退出码。 |
| 构建 | [build.tool-output.jsonl](/tmp/slice-checkpoint-quality-e08c468-evidence/build.tool-output.jsonl) | 原 session 23774 启动与完成块；命令无 stdout/stderr。对应原产物仍为 `/tmp/slice-checkpoint-quality-formal-gates-e08c468`。 |
| 缓存三包 | [cached-packages.tool-output.jsonl](/tmp/slice-checkpoint-quality-e08c468-evidence/cached-packages.tool-output.jsonl) | 原完整三行输出，全部明确为 cached。 |
| diff --check | [diff-check.tool-output.jsonl](/tmp/slice-checkpoint-quality-e08c468-evidence/diff-check.tool-output.jsonl) | 原退出 0、空输出回传。 |

## 对应原始命令

```text
go test ./internal/engine/protocol ./internal/engine/facade ./internal/cost ./internal/engine/vcs ./internal/engine/persistence
kill -TERM -- -53038
go test ./internal/engine/facade -run 'TestSlice|TestConcurrentSlice|TestTerminalCompletion|TestTerminalDriveReplay|TestTerminalSealingRejectsDirtyTreeAndDriftUntilReorganized|TestTerminalPublicationRejectsDriftAfterSealVerification|TestTerminalCheckpointDriftRejectsPublicationUntilContentRestored|TestConcurrentOwnerStarts|TestVanishedPeer|TestTerminalPeer|TestTerminalCheckpointRecovery|TestCandidatePromotionSquashesMultiCommitRangeAndCompletesTerminal' -count=1
go test -race ./internal/engine/facade -run 'TestConcurrentSliceCheckpointFreezesOwnerOnce|TestSliceReceiptRemainsFrozenAcrossReplayAndLaterCommits|TestConcurrentOwnerStartsSerializeAndReportTrueIntervals|TestTerminalCheckpointRecoveryDoesNotDoubleAttributeInterval' -count=1
go build -o /tmp/slice-checkpoint-quality-formal-gates-e08c468 ./cmd/formal-gates
go test ./internal/cost ./internal/engine/vcs ./internal/engine/persistence
git diff --check 0c5400b93885e1edf1949ee1e4467a9fc44d88a8 e08c468d5b163b9fee270b0d255f929a0e629e6e
```

本次补交仅保存既有输出和既有原生测试日志，没有审查、重跑测试或补造输出。


### 原工具回传副本：build.tool-output.jsonl

```json
{"i":1,"chunk_id":"4dd612","wall_time_seconds":1.001916375,"session_id":23774,"original_token_count":0,"output":""}
{"i":1,"chunk_id":"b47be1","wall_time_seconds":0.000007333,"exit_code":0,"original_token_count":0,"output":""}
```

### 原工具回传副本：cached-packages.tool-output.jsonl

```json
{"i":2,"chunk_id":"0ff854","wall_time_seconds":0.142069208,"exit_code":0,"original_token_count":36,"output":"ok  \tformal-gates/internal/cost\t(cached)\nok  \tformal-gates/internal/engine/vcs\t(cached)\nok  \tformal-gates/internal/engine/persistence\t(cached)\n"}
```

### 原工具回传副本：diff-check.tool-output.jsonl

```json
{"i":2,"chunk_id":"c2a03f","wall_time_seconds":0.000007875,"exit_code":0,"original_token_count":0,"output":""}
```

### 原工具回传副本：facade-race.tool-output.jsonl

```json
{"chunk_id":"42a01b","wall_time_seconds":1.003879125,"session_id":98749,"original_token_count":0,"output":""}
{"i":1,"chunk_id":"8e703c","wall_time_seconds":0.0000085,"exit_code":0,"original_token_count":13,"output":"ok  \tformal-gates/internal/engine/facade\t17.784s\n"}
```

### 原工具回传副本：facade-targeted.tool-output.jsonl

```json
{"i":1,"chunk_id":"467fd7","wall_time_seconds":1.001807083,"session_id":18077,"original_token_count":0,"output":""}
{"i":0,"chunk_id":"b48f14","wall_time_seconds":0.000011667,"exit_code":0,"original_token_count":13,"output":"ok  \tformal-gates/internal/engine/facade\t47.316s\n"}
```

### 原工具回传副本：protocol.tool-output.jsonl

```json
{"i":1,"chunk_id":"865033","wall_time_seconds":5.002846334,"session_id":33277,"original_token_count":13,"output":"ok  \tformal-gates/internal/engine/protocol\t83.616s\n"}
```

### 原工具回传副本：whole-packages-stop.tool-output.jsonl

```json
{"chunk_id":"995d4c","wall_time_seconds":0.000009709,"exit_code":0,"original_token_count":0,"output":""}
```

### 原工具回传副本：whole-packages.tool-output.jsonl

```json
{"i":0,"chunk_id":"011ad9","wall_time_seconds":1.001943,"session_id":33277,"original_token_count":0,"output":""}
{"i":0,"chunk_id":"58b175","wall_time_seconds":5.003521625,"session_id":33277,"original_token_count":0,"output":""}
{"i":3,"chunk_id":"9fd109","wall_time_seconds":5.001163917,"session_id":33277,"original_token_count":0,"output":""}
{"i":1,"chunk_id":"865033","wall_time_seconds":5.002846334,"session_id":33277,"original_token_count":13,"output":"ok  \tformal-gates/internal/engine/protocol\t83.616s\n"}
{"i":1,"chunk_id":"932907","wall_time_seconds":5.002105291,"session_id":33277,"original_token_count":0,"output":""}
{"i":0,"chunk_id":"69b086","wall_time_seconds":5.003265958,"session_id":33277,"original_token_count":0,"output":""}
{"i":1,"chunk_id":"b4384f","wall_time_seconds":5.002936125,"session_id":33277,"original_token_count":0,"output":""}
{"i":0,"chunk_id":"b00179","wall_time_seconds":0.000008833,"exit_code":143,"original_token_count":0,"output":""}
```
