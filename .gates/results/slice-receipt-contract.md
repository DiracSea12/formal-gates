# 分片收据已批准合同与实施补充

完整收据合同已由用户“那就实施”批准。下方原始合同中的“待确认”标题和 default 身份未决字样均已被用户后续决定及实施补充取代：default、full、custom 是独立身份；本次手动分块两门，不启动正式流程。state.json 边界按已批准总需求继续，无数据库迁移。此文件保存合同，不能代替实际实施或验收结论。

## 原始材料：/tmp/slice-receipt-implementation-contract.md

# 分片 checkpoint、收据与汇总实施合同（待用户确认）

基线：`d8f24aa87526572ebbbd93a5569819ebf97317ad`。本文件为只读设计产物，不表示已实施或已验收；确认后才派发实施子代理。无真实模型调用、无正式流程启动、绝不 push。

## 1. 范围和需求权威

先引原文，后给方案。

`refactor-plan/incremental-seal-plan.md:379-384`：

> - 实现 `SLICE_READY`、包含最终白盒测试的 child identity、durable receipt、case/cost 汇总；
>   dispatch 条目沿用 `<childRunID>/<dispatchID>` namespace 去重，child owner 以 `childRunID`
>   一次性幂等汇总到 master cost projection，不重新估算 child 限额。主干 owner 与 child
>   owner 仍分别保留在各自 run 的 receipt/sidecar；master 自身 `Owner` 不被 child 覆盖，且
>   不引入通用 billing schema。

`.harness/HANDOFF-STAGE5.md` 机主裁定：

> **QA 裁定（三轮更正定稿）**：分片流程普通 QA（黑盒/白盒）**不进候选、不选、不执行**；门每块每片各跑（"块"＝串行排队的分片）；路线机制照旧（逐片 full/custom）；质量面三层＝每片所选门＋合并门＋合并 QA（强制，custom 省略不延伸）。不拆的普通 run 完全不变。

> 注意：其中「包含最终白盒测试的 child identity」按 QA 裁定降权理解——分片不做普通 QA，该条以裁定为准。

当前用户补充：路线展示分三个独立选项：**默认**（全部门、不含 QA）、**full（QA阶段较重，适合大型任务）**、**custom**（自定义）。默认不可称为 custom。是否新增独立 default 程序标识由主代理另行澄清；本合同只复制既有 `RouteMode/SelectedGates` binding，不实施路线调整、不等待该标识决定。

`openspec/changes/orchestration-pipeline-engine/master-requirements.md §8.1.3-4`：

> 3. child 继承整体产品/技术审与精确拓扑，各自持久化路线；child 达标后进入可恢复的 `SLICE_READY` checkpoint，不作为独立最终 SEALED，也不提前删除恢复信息。
> 4. durable `SliceTerminalReceipt` 至少含状态、child/slice/master ID、版本、VCS snapshot/tree/digest、需求/拓扑/路线 bindings、QA/门结果、批准用例、成本和最后 request/event digest。master 只按期望 map 接纳 receipt。

`master-requirements.md §9.5`：

> 每类非幂等本地副作用采用 `intent → execute → observe/reconcile → commit`：start、prompt/artifact、blackbox/whitebox QA workspace、case mirror/materialize、ValidationCandidate freeze/promotion、Git/SVN/P4 child workspace 与集成、snapshot、Git squash、child receipt/cost、summary、cleanup、abort。

**本次交付**：成功 child 的最终 native identity 核验、`SLICE_READY` 非终态、不可变 durable receipt、master 按既有认领 map 接纳，以及批准用例/成本的幂等汇总与查询投影。当前分片普通 QA 不运行，因此批准普通 QA 用例集合合法为空，不制造用例、不写虚假 QA PASS；所选每个门仍必须完成。

**明确非目标**：child worktree 创建、master 主线集成、合并 QA/合并门执行、AffectedChildSet、主线 repair、child/master reset/abort/需求变化级联、guarantee sidecar、canary、最终文档路线收敛。收齐成功 receipt 后只呈报已收齐/等待集成边界，不借本次工作执行 merge 或终结 child。ABORTED receipt 和级联终结随后续已约定生命周期部分交付，本次不得新增第二套 abort。

## 2. 已核实的归属与可复用边界

### 2.1 状态、认领、提交

`internal/engine/protocol/protocol.go`：

> // SliceChildren 是 master 账上的分片认领事实（sliceID → childRunID，累
> // 积运行事实）。它不放进 TopologyBinding：拓扑是不可变的确认决定，
> // children 是运行期累积——一个控制事实只有一个权威来源。
> SliceChildren map[string]string `json:"sliceChildren,omitempty"`

`internal/engine/protocol/slice.go`：

> // SliceChildBinding 是 child run 状态上的继承事实：分片身份（master run 与
> // 分片名）加上继承结论绑定的需求 revision。

> if claimed := state.SliceChildren[sliceID]; claimed != "" {
>     if claimed != childRunID {
>         return nil, claimRejected("slice %q is already claimed by child run %q", sliceID, claimed)
>     }
>     // 同 id 重入：认领事实已在账，直接返回继承事实补齐缺口侧。
>     return sliceRunStartOf(state, sliceID), nil
> }

`internal/engine/protocol/engine.go`：

> // commitEffects 是一切携业务后果事务的唯一提交点

方案：沿用 engine state/envelope、现有 store Save/Recover/CAS、`ClaimSlice` 和 `errWriteBarrier`。master 接纳只信 `SliceChildren`，不得扫描目录把未认领 child 当成合法成员；不再建 child registry、收据队列或第二生命周期。本次冻结总需求 §5.9 明确 `state.json` 为 engine 唯一权威投影；本合同延续该既有归属，不扩大为数据库迁移。AGENTS 的通用数据库表述不能据此写成已满足，当前存储边界在确认时明确呈现。

### 2.2 最终验证与原生交付

`internal/engine/protocol/workspace.go`：

> return w != nil && w.Status == "COMPLETE" && s.PromotedSnapshot != "" && s.PromotedDigest == w.Candidate.Digest

`internal/engine/facade/facade.go:946` 现有核验主干：

```go
head, err := f.vcs.Resolve()
if err != nil {
    return err
}
if head != snap.State.PromotedSnapshot {
    return fmt.Errorf("engine terminal: native VCS identity %s does not match the promoted snapshot %s", head, snap.State.PromotedSnapshot)
}
if err := f.vcs.VerifyReady(); err != nil {
    return err
}
if err := f.vcs.IsAncestorOrEqual(snap.State.BaseSnapshot, head); err != nil {
    return err
}
return f.engine.SealTerminal(head, emptyFingerprint())
```

`internal/engine/runtime/phase.go`：

> // PhaseSliceReady：child 可恢复 checkpoint（child 不独立 SEALED，
> // 总需求 §8.1.3）。
> PhaseSliceReady RunPhase = "SLICE_READY"

`internal/engine/protocol/engine.go`：

> // SealTerminal 公开入口进入，Drive 只呈报 COMPLETE 边界。

方案：把现有 native 交付核验提成一个明确命名的共享步骤，返回已核验 identity；普通 run 仍调用 `SealTerminal`，child 经 checkpoint 接纳入口调用同一 `validationConcluded` 准入并相变 `SLICE_READY`。`SealTerminal` 本身拒绝 child 独立终态调用，以免从别的入口绕过。不复制 Resolve/VerifyReady/ancestor 判定，不创建另一套 seal 业务。child 使用已冻结、已验证并经现有 candidate promotion 绑定的最终身份，不用 DevelopmentSnapshot 替代；树身份直接复用 Git `TreeDigest`（原生 tree ID 即当前 digest），不加新哈希体系。QA 裁定下不生成白盒身份，但开发 worker 正常交付的测试文件如果已在候选中，仍随该完整身份保留。

### 2.3 dispatch 与 owner 成本

`internal/cost/cost.go`：

> // Numbers come exclusively from host transcript parsing; an unverifiable source is
> // marked unavailable and never receives a fabricated value.

```go
func Merge(run *RunCost, ns string, other *RunCost) {
    if run == nil || other == nil {
        return
    }
    if run.Dispatches == nil {
        run.Dispatches = map[string]DispatchCost{}
    }
    for id, entry := range other.Dispatches {
        key := id
        if ns != "" {
            key = ns + "/" + id
        }
        Record(run, key, entry)
    }
}
```

`internal/cost/owner.go`：

```go
func RecordOwner(run *RunCost, entry OwnerCost) {
    if run == nil || run.Owner != nil {
        return
    }
    run.Owner = &entry
    run.addTokens(entry.TotalInputTokens, entry.InputCacheHitTokens, entry.InputCacheMissTokens, entry.OutputTokens)
}
```

`internal/engine/facade/owner_cost.go`：

> // engine 的区间操作段（启动：扫描＋自身绑定＋peer 标记；终态：重扫＋自身标
> // 记＋peer 标记＋量取）全部在锁内执行且冲突时等待

> // finishOwnerCost 是 engine 的终态观察点：在 lockOwnerCost 内重扫两个命名空间

`internal/engine/protocol/protocol.go`：

> // 终态差值只作报告（Cost.Owner），不参与 in-flight guard。

方案：dispatch 直接 `cost.Merge(master.Cost, childRunID, receipt.Cost)`；不改 namespace，不重算 child allowance。owner 由现有 baseline/MeasureOwner/重叠判定量取，不调用 `RecordOwner(master.Cost, childOwner)`。master 自己 `Cost.Owner` 保留。child owner 与区间端点在 child checkpoint 同一事务冻结，receipt 携带原值。master 接纳新 receipt 时才一次性把 child owner 的 token 各项加入总计；幂等权威是 master 已接纳 receipt 的 `childRunID + digest`，与其插入同一事务。不额外建通用 owner ledger/billing schema；如需成本包 helper，仅增加专用于已接纳 child 成本的最小累加操作，幂等条件留在唯一 receipt 接纳 owner，不再并行维护一次性标记。

现有 `finishOwnerCost` 的 I/O 量取和 `RecordOwnerCost` 提交目前在同一函数；只抽出必要的“观察/量取值”供普通终态和 child checkpoint 共用。child 不能先单独 RecordOwnerCost 再另写 endedAt，否则中断会丢冻结端点。checkpoint 重入从已冻结 receipt 取成本和端点，绝不重新量取。

`internal/engine/protocol/guard_budget.go` 当前 `dispatchTokenTotal/tokenCapStatus` 遍历 `Cost.Dispatches`；导入的 child dispatch 仍是精确派发成本，按既有总 dispatch 成本口径纳入 master token projection，owner 仍不纳入。不得据此改 child guard 或再估 child 限额；master 的 TaskKey/Attempt allowance 继续从 master 自身派发账本派生。验证导入成本不会伪造 master spawn/attempt 或造成账本不一致。

## 3. 业务不变量与最小持久数据

1. child 的成功边界只能是 `SLICE_READY`；有最终有效完整 wave、当前需求 binding、已绑定候选 promotion，以及 native identity/干净工作区/基线祖先核验才可到达。缺门结果、obligation 未闭合、promotion 未绑定、漂移、需求屏障或 abort 决定均不得发表成功 receipt。
2. child `SLICE_READY` 的 state、需求/路线/验证/派发/恢复信息仍在；不写独立 SEALED/COMPLETE terminal summary，不调用 terminal cleanup 或 lifecycle.EndRun。既有完成计划的 KindComplete 必须在 facade 和查询投影处解释为 checkpoint，不能继续展示最终完成。
3. checkpoint 冻结后的同一份 receipt 内容不可变。child phase、receipt 值及 owner 区间终点/成本在一个 engine commit 中确立；文件只为证据副本。发布失败允许重试，只从被冻结值写出，不靠重新运行开发/门/计量来恢复。
4. master 只接纳精确 map 中的 `sliceID→childRunID`，且 master ID、需求 revision、拓扑、路线、版本与最终身份/结果 bindings 自洽。错 child、错 master、错 revision 或同 child 不同 digest 是零汇总拒绝；同 child 同 digest 是零增量重入。
5. master 已接纳记录、批准 case projection、dispatch merge、child owner 累加同一事务；不存在“先加钱后登记”或“先标已收再加钱”的跨写窗。master 自己 Owner 不被覆盖。missing/unavailable 原样保留，不能用 child 总计倒推出缺失派发或 owner 数字。
6. 全部 expected receipt 已收齐由拓扑及认领 map 派生，不保存第二个收齐布尔；本次只等待后续 integration。不同 child 并行提交不能覆盖对方，相同 child 重复/响应丢失不得重复汇总。
7. 批准用例以已接纳 receipt 的来源 child ID 与原 case ID 关联；master 自己用例与 child 用例不混为一个 CaseSet，不人为改 expected cases。当前 QA 裁定为空集合；空即明确“未选择普通QA”，不表示执行 PASS。保留 schema 的批准用例载荷职责，但不为当前空集合另造 QA 流程。

最小新增结构（具体命名由行为定，不带开发批次/轮次）：

| 结构 | 必需内容与唯一归属 | 既有能力为何不足 |
| --- | --- | --- |
| typed `SliceTerminalReceipt` | child state 冻结 READY 值；child/slice/master ID，state/definition/package 等现有 envelope 版本绑定，native snapshot/tree/digest，需求/topology/RouteMode/SelectedGates，最终 wave/门结果与QA选择事实，批准 cases，child RunCost、owner interval端点、最后 request/event digest | SliceChildBinding 只有启动身份；terminal summary 是终态证据，不能用于非终态 checkpoint |
| child 上一个可选 receipt 字段 | 保存该唯一冻结值及其 canonical digest；phase与值同事务。sidecar `slice-receipt.json` 是可重建证据副本 | 现有 phase 已有 SLICE_READY，缺冻结成功证据；没有另增 phase、发布状态机或单独 intent 标志 |
| master 已接纳 receipt map | 按 childRunID 保存已接纳值与 digest；每项须对应 SliceChildren 的期望身份。成本/cases及去重全从这项接纳派生 | SliceChildren 只证明认领，不能证明完成或约束重复成本接纳；不另造 accepted-owner map/成本 sidecar 队列 |

收据身份字段复用 envelope、`PromotedSnapshot/PromotedDigest`、TopologyBinding、SliceChildBinding、Validation/CaseSet/ValidationWave 与既有 Events/Decisions；只投影最终需要的 typed 结果，不把整份 State 当 receipt，不持有第二份业务状态。最后 event 由既有 `Events[eventID].Acceptance.Revision` 的最大已接纳 revision 确定，再取该条 `Digest`；最后 request 由既有 `RecordedDecision.Revision/RequestID/EventID` 及对应事件记录确定。不用 map 遍历顺序猜“最后”，不新增最后记录状态或 event ledger。`internal/engine/protocol/event.go:979-985` 已证明 Acceptance 携带 Revision/RequestID；`protocol.go:239-254` 已证明 RecordedDecision 携带 Revision/RequestID/EventID，EventRecord 携带 Digest/Acceptance。

## 4. 顺序、跨接缝携带者与恢复

### child 收口

1. 正常 Drive 运行现有 validation/candidate pipeline；已达达标边界时，沿共享 native 核验步骤取得精确 head/tree。
2. 在现有 owner 协调锁内复用 owner overlap 观察和量取。engine checkpoint handler 在同一提交中重核 barrier、abort、child binding、validationConcluded 与精确 identity，记录 child Owner、区间端点、typed receipt/digest 并相变 SLICE_READY。
3. 从已落账 receipt 原子物化 child 自己 stateDir 的 receipt sidecar；读回核对 digest 与完整内容；native 核验针对收据冻结的不可变 snapshot/tree，不用后来的工作区 HEAD 重新判断 checkpoint 是否成立。失败不删除 state，不终态，后续 Drive/Open 恢复直接补发布同一值，不重新执行达标判定或量取。
4. 将这个 sidecar 值交给 master 的单一接纳入口。child Drive 可执行直接交接；master Drive 同样从已认领 child 的冻结 receipt 找到未接纳事实并调用该入口。恢复只做找事实/补交接，不另外实现接纳或计算成本。

### master 接纳

- 接缝携带者是完整 typed receipt + canonical digest；不接收调用方编造的成本增量、收齐标记或底层 guard 字段。
- master facade 沿 `ClaimSlice` 的 master run 协调方式序列化跨 run 交接；不要在持有 child store 锁时等待 master run/owner 锁。底层仍经 master engine commit/CAS 原子接纳。不同 child 与同 child 并发都走此一入口。
- 接纳入口先校验 exact map、版本/bindings/结果，先查已接纳 childRunID：同 digest 零变更返回，不同 digest 拒绝；新值时登记 receipt，并在同一 commit 调现有 cost.Merge、累加 child owner、形成 case projection。
- master 收到响应后丢失、child发表后尚未交接、master提交后响应丢失，都由再发现+同入口重入收敛，不重放已完成副作用。
- child 接纳时冻结 owner 区间后，`engineOverlapPeers/enginePeerIntervalClosed` 及 summary overlap 查找应认识 checkpoint receipt 的闭合端点；不能因 child state 非终态便把已冻结区间永久视为开放。沿现有 owner interval owner 扩展合法证据形态；无独立 endpoint 文件、无第二套测量算法。

| 接缝 | 携带者 |
| --- | --- |
| validation → native核验 → checkpoint | 当前最终 wave、PromotedSnapshot/PromotedDigest、native head/tree、当前 requirement/topology/route |
| owner测量 → checkpoint提交 | transcript/provider/start baseline、overlap事实、OwnerCost与本次区间 endedAt；锁内提交冻结 |
| child持久值 → sidecar | 已冻结 typed receipt/digest，原子写入并读回核验 |
| child → master | master/slice/child ID + 完整 receipt/digest；master expected map作唯一准入 |
| crash/Open/Drive → 接纳 | 从持久child receipt及master accepted map发现差额，仍调用同一接纳函数 |
| 查询/报告 | child phase及receipt；master accepted map、Cost与批准cases投影；无额外状态源 |

## 5. 全部当前受影响入口与删除要求

- `protocol.Drive/Plan` 的 regular complete/checkpoint呈报、phase/query投影；`workspace.go` 的 validationConcluded 与 SealTerminal child准入保护。
- `facade.Drive` 的 child checkpoint收口、共享 native交付核验、receipt发表及master接纳；普通 `sealDelivered` 复用同一步核验。
- `facade.Open`/既有 store.Recover 后 Drive 的重入；master Drive 重找认领 child receipt；Start 同ID保留既有成立谓词，不引入新的“run是否存在”规则。
- `Show/Status/Next/project/projectOrSummary` 展示 child checkpoint及master receipt进度；CLI/encoder 如依赖这些返回形态则只做必要接线，不新增 public 写命令。
- `finishOwnerCost`/ownerOverlap peer查找及 protocol owner提交；普通终态保持原有行为，child使用同一观察算法、端点冻结事实。
- `protocol/slice.go` 作为认领/receipt接纳的业务 owner、State字段/复制和必要完整性校验；现有 commitEffects/persistence 回收恢复共用。
- `internal/cost` 既有 Merge/Record与最小child owner累加；guard派生仅核导入成本影响，不另估额度；master自己Owner/report必须保持。
- 当前相关 facade/protocol/cost 行为测试；必要 native Git identity测试。不动 legacy `internal/validate` 的公开路径、sidecar机制或冻结规格。

删除/替换清单：提取共享 native核验后删除 sealDelivered 内被替换的重复核验主干；移除 child沿普通 COMPLETE进入SealTerminal/terminal-summary/terminal cleanup的执行路径；不存在则不伪造删除成果。不得新增 receipt发布与恢复两套业务、成本聚合另存标记、test-only生产入口、legacy到engine补账或通用 billing。当前 legacy slice cost sidecar 属保留参考，未被本任务替代，不能借收敛名义删它。修改过时 owner/phase/成本契约注释时先删旧再写当前语义，不保留“后来引入/批次”字样。

## 6. 验收与原始证据

这些是确认后的实施/独立复核计划，本只读阶段未执行，不写 PASS。所有 fixture 断言实际输入 child/master/slice identity、native方法及参数；不同输入必须有区别，不用统一返回成功掩盖身份缺陷。测试期望来自上述已确认规则，不迎合实现；正常可达前置可复用现有 validationDriver，不手改状态制造攻击场景。

1. **正常成功**：完整 child 开发→每个所选门→candidate promotion→Drive，断言 phase=SLICE_READY、最终snapshot/tree等于实际 Git内容、receipt版本/需求/topology/route/门结果/批准cases/cost/request-event digest全文值正确；state/恢复信息仍在，无独立终态摘要、无清理child。用当前full/custom路径覆盖不同所选门；默认路线如另行落地，沿同一binding通用覆盖，不加特例。
2. **正常等待/常见失误**：少一个所选门、未绑定promotion、候选native漂移/脏树、错master/slice/child/revision等正常误交接，零success receipt/零cost增量；范围只测收据准入，不扩reset/级联。
3. **重复/响应丢失**：重复child Drive/发表、同receipt两次交接、master已提交后响应丢失再交接，持久receipt字节/digest/端点不变；master accepted项只有一条，namespaced dispatch数/token总值、child owner贡献只一次；同child不同digest拒绝且持久内容不变。
4. **并行**：两个已认领child使用同名dispatchID并行交接，保留两个`<childRunID>/<dispatchID>`；master已接纳两个child，批准case来源保留；相同child并发交接只一项。检验master自己的dispatch/Owner保持，各类token数准确；不检查无关规模组合。
5. **中断恢复对照**：在checkpoint commit前、commit后sidecar前、sidecar后master接纳前、master commit后响应前逐个中断，Open/Drive恢复后与不中断轨迹的phase、receipt完整内容、native identity、cases、dispatch和owner合计相等。复用现有持久事务故障注入/真实进程窗口，恢复不重新执行模型或门，不重新测owner。
6. **owner与成本语义**：child owner精确来源、UNAVAILABLE、无binding分别保留原语义；checkpoint后相同transcript新run的区间判定识别闭合端点，master owner原值不变。子费用导入后master guard仍能按自己的TaskKey/Attempt账本derive，无token估算、无重复master spawn；Owner仍不参与token cap。
7. **普通run回归**：现有regular正常终态native核验与publication重入测试通过，普通run不出现SLICE_READY，不因共享核验/owner抽取丢验证或改变终态清理行为。主代理已有基线测试结果只作事实，不代替本次改后验证。
8. **原始持久内容**：每个关键点读取state envelope、child sidecar与master state，断言具体字段/集合/数值/摘要；不能只断言文件存在或HTTP/CLI成功。成本已知答案应逐项算出，Case为空应显式断言空及未选择状态。

验证按改动面选 `go test ./internal/engine/protocol ./internal/engine/facade ./internal/cost`；native接线触及VCS则加该包；并发接纳/owner接缝做相关包race。完成原生构建、`git diff --check`；全量仅所选路线/仓库当前要求或真实横切风险要求时补，不重复已通过且不受影响的检查。不收费、不运行canary。

逐项工程验收报告必须附对应当前代码/测试证据：正确性、质量、简洁、耦合、维护、可读、优雅、需求无遗漏、设计一致性、无不当硬编码、无窄逻辑/过拟合、无过度设计/防御/抽象、测试与业务隔离。功能绿不能替代此报告。不得通过修改期望、削弱断言、生产test分支或并行机制叠加获得通过；未验证项明确写未验证。

## 7. 派发必须逐字携带的简洁法

`prompts/actions/development-worker.md` 原文：

> 只实现已确认的需求和交付给你的范围。实现范围由交付给你的规格说明确定；审查
> 流程、QA 用例和流程状态由编排层负责，冻结的需求与方案产物是验收输入——这三
> 类都不是你的修改目标。不要发明需求，也不要添加规格未要求的公开 API、配置、
> 报告、执行器、状态、证据、清理或兼容路径。
> 新增一个概念之前，先按以下顺序找更简单的做法，任一步成立即采用它、不再往下：
>
> 1. 规格未要求它存在 —— 不写。
> 2. 仓库已有可复用的归属者 —— 复用或改造既有者，不新建。
> 3. 标准库可做 —— 用标准库。
> 4. 运行环境自带能力可做 —— 用自带能力，不自造实现、不新增依赖。
> 5. 已安装依赖可做 —— 用已装依赖。
> 6. 一行可表达 —— 一行。
> 7. 以上皆不可 —— 写能工作的最小实现。
>
> 此顺序只约束写法的取舍；已确认需求要求的行为照常实现，不得借"更简"之名削减验证、
> 错误处理或安全行为。只提供 CLI 接受的语义值。

`prompts/actions/start-readiness.md` 原文：

> 方案层面的判据是：是否有更简单的既有归
> 属者或原生能力可用，以及它是否是足够且最简的做法。即使某个多余的系统、状态、流程、
> 配置、证据或抽象在内部设计上自洽，也要否决它。

实施子代理返回须声明未引入规格外概念；每一实际新增状态/抽象逐项说明既有owner为何不足，并列删除清单、测试实际执行/跳过/失败、持久证据路径与残留限制。实施代码由子代理完成；主代理只合同、派发、独立复核、记账与提交。

## 8. 已决事项与未核实点

- 已决：QA按机主裁定；child非终态；reuse现有native交付核验/validationConcluded；namespace直接复用cost.Merge；owner不覆盖master；收据接纳与汇总同master事务；没有新增通用billing/第二生命周期；范围止于等待integration。没有需要用户另选的重大架构分支。
- 路线default标识待主代理澄清，为另一项工作，不改变本合同读取/携带RouteMode与SelectedGates的方式。
- 只读设计尚未实施验证：完整receipt字段的encoder/copy wiring、checkpoint native发布窗口具体故障点、当前candidate cleanup是否回收了未来必须保留的child资源。实施须在现有owner内核实这些接缝，只补本次成功checkpoint必需部分；若需扩大worktree/级联/integration范围或改变已确认语义，先向主代理上报，不偷偷扩合同。
- 本合同规定沿用master总dispatch token口径，child额度不重估；这是当前guard遍历全部Dispatches的事实所导出的最小选择。如果机主希望master token cap排除child费用，应作为明确的成本语义选择单独确认，不能由实施者静默改口径。

确认对象为本文件完整范围、最简方案和验收合同；确认后才实施。


## 原始材料：/tmp/slice-checkpoint-execution-brief.md

# Child checkpoint 实施派发合同

## 已批准范围与派发边界

本文件补充 `/tmp/slice-receipt-implementation-contract.md`，该完整合同已获机主“那就实施”确认。合同中的待确认措辞和 default 标识未决说明不再适用：default 是独立路线身份，default/full/custom 已按已批准路线合同交付。本块只实施完整合同的 child 成功 checkpoint 部分；master 接纳与汇总为下一块，由同一开发者在本块双门和主代理复核后继续。完整合同的业务规则不得因拆块丢失。

本次开发不走正式流程；实施由子代理完成；主代理只派发、独立复核、记账、提交。无真实付费模型调用、无 push。不增 public 写命令、不扩 worktree/integration/merge/cascade/guarantee。

## 参考原文先于选择

当前 `internal/engine/facade/facade.go`：

```go
if plan != nil && plan.Next.Kind == decision.KindComplete {
    current, loadErr := f.engine.Load()
    if loadErr != nil { return Run{}, loadErr }
    if _, aborted := current.State.AbortDecision(); !aborted {
        if err := f.sealDelivered(); err != nil { return Run{}, err }
    }
}
```

`sealDelivered` 的既有归属：Resolve → 精确 PromotedSnapshot → VerifyReady → IsAncestorOrEqual → SealTerminal。完整合同 §2.2 引用的原文仍有效，应抽取共用 native 核验，按 child 身份选择 checkpoint，普通 regular 仍 SealTerminal；无并行核验或第二套 seal。

当前 `internal/engine/protocol/workspace.go`：

```go
w := s.currentValidationWave()
return w != nil && w.Status == "COMPLETE" && s.PromotedSnapshot != "" && s.PromotedDigest == w.Candidate.Digest
```

只消费这个准入 owner，不另写 child 达标规则。`runtime.PhaseSliceReady` 已存在，无须新相位。

当前 `internal/engine/facade/owner_cost.go`：

```go
release, err := lockOwnerCost(f.root)
if err != nil { return ownerInterval{}, err }
defer release()
// 重扫 overlap、MeasureOwner 后
if err := f.engine.RecordOwnerCost(entry, emptyFingerprint()); err != nil {
    return ownerInterval{}, err
}
return interval, nil
```

观察/量取复用该 owner；child cost、endedAt 与 checkpoint 一个 commit 冻结，不能先 RecordOwnerCost 再写 checkpoint。普通终态不得因抽取丢 overlap 或计量语义。peer 区间判定复用既有查找 owner，认可 state 中已冻结 receipt 的端点；不建立另一个 endpoint 文件、锁体系或算法。

## 事实、不变量与携带者

- 成功 child 唯一事实：child state 上的 SLICE_READY 与一份 typed receipt/digest；完整字段职责按完整合同 §3。`slice-receipt.json` 仅为可重建证据。
- 完整验证仍来自既有当前 wave、promotion binding、requirement/topology/route。没有普通 QA 时为空用例/明确未选择，不写 QA PASS。
- native identity：首次核 actual promoted head/tree、干净工作区和祖先；重入只核被冻结的不可变对象，不重新消费后来的 HEAD。
- owner：沿 owner 协调锁观察/量取后，receipt、phase、cost、interval 端点原子冻结；同 checkpoint 重入不重新测量。
- seam carrying：final wave/promotion → 共享 native核验 → protocol checkpoint；OwnerCost+interval → 同 checkpoint；frozen receipt/digest → sidecar publisher；Events/Decisions revision+digest → 最后 request/event 身份；child phase/receipt → 查询投影。
- `SealTerminal` 必须拒绝 child 独立 seal。SLICE_READY 不触发 terminal-summary、terminal cleanup、EndRun；仍可 Open、Show、Next、Drive。
- 本块不接受 master receipt、不汇总 master case/cost、不新增 master map 占位；sidecar 按当前 child 状态持久化，master 交接在下一块接线。同一个完整 receipt schema 届时复用，不写第二收据。

## 受影响入口、删除与验收

核对 protocol.Drive/Plan/phase/query、SealTerminal、facade.Drive/Open/project/projectOrSummary、owner observe/peer interval、State copy/validate/encoder、immutable Git identity读取及既有 sidecar原子写 helper。沿真实调用链处理完成投影，不只修改产生 KindComplete 的一个分支。

删除被抽取的 native核验重复段、child 独立终态的执行路径和过时 owner/phase 注释；不删除未被本任务替代的 legacy 路径。不得添加 test-only 生产分支/故障公共面/发布状态机/备用 receipt 队列。

验收以完整合同 §6 中本块适用条目执行：

1. 用正常真实 Git child 开发→每个所选门→promotion→SLICE_READY；full/custom/default 沿同一 binding，核 receipt 全内容、state 保留、无终态摘要/清理。
2. 缺门、无 promotion、脏树/漂移、屏障/abort 不发表成功 receipt；直接 SealTerminal child 被拒。
3. 重复 Drive/重开/发表 bytes、digest、cost、endpoint 不变；checkpoint 后外部合法 HEAD 前进不能倒改冻结身份。
4. 使用既有内部故障点或真实进程副作用边界，核 commit 前、commit 后 sidecar 前、发表后重复 Drive 的正常/恢复轨迹一致；不改内部 state 造阻塞项。
5. Owner exact/unavailable/无binding 的需求语义，closed checkpoint 后新 run overlap 判定；共享提取后的 ordinary regular 终态回归；相关并行边做 race。
6. 编译与改动真实影响包，持久工件断言内容。原始失败先保存后修，断言由需求和协议导出，不为过测试更改。完成后冻结代码，报告准确执行、失败、跳过与未验证；不要自行提交。

## 每次派发硬标准

完整读取 `AGENTS.md`、`prompts/actions/development-worker.md`、完整已批准合同。正确性、质量、需求完整性、简洁性、耦合性、可维护、高可读、优雅和设计一致、无不当硬编码、窄逻辑、过拟合、过度设计/防御/抽象、测试业务隔离是交付条件。正常/重复/并行/恢复相关业务边必须贯穿同一 owner。注释只写当前方案；名称不带开发顺序；替代路径、账本、标记不能并存。报告每一项当前代码/执行证据，功能绿不能替代工程验收。

必须按开发提示词原文执行：

1. 规格未要求它存在 —— 不写。
2. 仓库已有可复用的归属者 —— 复用或改造既有者，不新建。
3. 标准库可做 —— 用标准库。
4. 运行环境自带能力可做 —— 用自带能力，不自造实现、不新增依赖。
5. 已安装依赖可做 —— 用已装依赖。
6. 一行可表达 —— 一行。
7. 以上皆不可 —— 写能工作的最小实现。

“更简”不得削减验证、错误处理或安全行为。先引既有参考原文再选设计，每个新增状态/抽象说明已有 owner 为何不足；不要用反例条件分支叠补丁。按已确认总需求 state.json 边界实施，不迁移数据库，也不宣称通用数据库条款通过。


## 原始材料：/tmp/master-receipt-execution-brief.md

# Master receipt 接纳实施补充合同

## 授权与范围

与已批准的 `/tmp/slice-receipt-implementation-contract.md` 合读。该完整合同的待确认措辞不再适用；default 为独立身份，child checkpoint 部分的冻结实现是本块消费的现有能力。本块补齐完整合同的 master expected-map 接纳、批准 cases 与费用幂等汇总、双向发现/恢复及查询；止于等待集成。不实施 child worktree、创建重构、组容量、主线 integration、merge QA/门、级联、ABORTED receipt、guarantee、canary 或安装公开面，不提前引入这些块的占位状态。

本次不走正式流程。开发代码由子代理完成，主代理独立复核、两门、记账与提交。无真实模型调用、无 push，开发者不自行提交。具体代码基线由派发给定。

## 参考原文先于实现选择

已批准总需求 §8.1.4：

> durable `SliceTerminalReceipt` 至少含状态、child/slice/master ID、版本、VCS snapshot/tree/digest、需求/拓扑/路线 bindings、QA/门结果、批准用例、成本和最后 request/event digest。master 只按期望 map 接纳 receipt。

当前 `protocol/protocol.go`：

> `SliceChildren` 是 master 账上的分片认领事实（sliceID → childRunID，累积运行事实）。它不放进 `TopologyBinding`：拓扑是不可变的确认决定，children 是运行期累积——一个控制事实只有一个权威来源。

当前 `protocol/slice.go`：

```go
type SliceReceipt struct {
    Receipt SliceTerminalReceipt `json:"receipt"`
    Digest string `json:"digest"`
}
func (r SliceTerminalReceipt) CanonicalDigest() string { return digestOfCanonical(r) }
```

当前 `facade/facade.go` 的 `publishSliceReceipt` 只消费 state 冻结值，核 immutable snapshot/tree、原子发表证据并读回完整摘要。沿这一 schema/publisher 交接，不引第二份收据、发布状态机、queue 或 registry。

当前 `facade/facade.go` 的 master 协调归属：

```go
unlock, err := coordination.AcquireRunWaiting(root, req.MasterRunID)
if err != nil { return nil, err }
defer unlock()
master, err := Open(root, req.MasterRunID)
```

沿该 master run 锁和 engine CAS 序列化跨 run 接纳；child 成功冻结/发表锁段结束后才进入 master 接纳，不持 child store、owner 或 promotion 锁等待 master；master 发现路径不持 master 锁再等待 child run 锁。不要另建锁体系或每入口各一套接纳业务。

当前 `internal/cost/cost.go`：

```go
func Merge(run *RunCost, ns string, other *RunCost) {
    // incoming dispatch entries under "<ns>/<dispatch-id>"
    for id, entry := range other.Dispatches {
        key := id
        if ns != "" { key = ns + "/" + id }
        Record(run, key, entry)
    }
}
```

当前 `cost.RecordOwner` 在本 run Owner 已存在时不再记录，并会设置本 run Owner；不能拿它把 child Owner 写成 master Owner。直接复用 Merge；child Owner 各类 token 只在新 receipt 接纳事务中累加，采用成本 owner 内最小 helper 或等价既有表达。不要累加 receipt 总计代替逐 dispatch+child owner，更不能重估额度或数字。

当前 `protocol/validation_wave.go`：

```go
if s.RouteMode == routeModeMerge {
    message = "the retained master run waits for its slice children; merge verification runs after the slices complete"
}
```

沿该已存在等待边界呈报从 expected topology/claim/accepted receipt 派生的进度；全齐后仍等待后续 integration，不触发 ordinary seal 或执行未交付的 merge。无第二个 all-ready bool。

当前 `protocol/workspace.go:195–210` 的既有成功归属：

> 收口只消费两个既有权威：终 wave COMPLETE（obligations 全解，由 settleValidationWave 单点定义）＋候选晋升已绑定且内容与该 wave 已验证最终候选相等。

当前 `protocol/validation_wave.go` 的 `carryGateResult`：

```go
if covered {
    target.Status, target.ResultAction = "PASS", completion.ActionID
    return
}
```

已确认产品规则允许纯 P2 修复以 repair completion 结清门义务、不复审，允许明确的 candidate-scoped 处置；因此 receipt 的终 target 不一定指向当前 wave 的原始 gate result。接纳继续消费当前 loader 已校验的 child 冻结事实和这条原收口归属，不能另造“每条 Result 必须是同 wave fresh gate PASS”的算法，也不能把 raw FAIL、carry 或被明确处置的原结果改成伪 PASS。验收须包含合法 repair/carry 后的 child 交接；直接全新 PASS 不是唯一正常来源。

## 业务不变量、唯一事实与接缝

1. master 已接纳 receipt map 以 childRunID 保存完整冻结值/digest，是成功接纳与汇总幂等的唯一事实。必须逐一匹配 topology 中期望 slice 及 SliceChildren 的精确 pair，不能只比较条数或去重后的 child ID 数，不能通过目录扫描扩大成员资格。
2. 新 receipt 校验 status、child/slice/master、当前 requirement/topology、既有 envelope 版本和最终 wave/route/native/result 的自洽绑定。复用已有校验/领域值；不另造成功算法、通用签名、安全 schema 或手工复刻验证内核。错身份/陈旧收据/同 child 不同 digest 在成本变化前明确拒绝。相同 digest 的已接纳值零写返回。
3. receipt 插入及其 cases 载荷、namespaced dispatch merge、child owner token 累加必须同一 master commit。accepted receipt 本身已承载按 child 关联的批准 cases；查询可从它派生，不为当前合法空集合再建平行持久 CaseSet。master 本身的 cases 和 Owner 不被 child 覆盖。
4. 当前 child 普通 QA 不选不执行，SelectedQA 和 ApprovedCases 合法为空。所选门义务仍须按既有收口规则结清。不得为了汇总测试编造普通 QA PASS 或不符合已批准政策的 cases。
5. master 的 TaskKeys、spawns、attempt/retry allowance 只来自 master 自身账本；导入 child dispatch 不制造 master task/receipt。guard 总 dispatch token 口径沿已批准合同，owner 不进入 token cap；child guard 不重算。
6. child Drive 发表后交接与 master Drive 从已认领 child 找事实必须调用同一接纳入口；publication/recovery、已提交响应丢失、同 child 重复和不同 child 并发都通过该入口闭合。发现无 state 或尚无 receipt 的认领 child 是正常等待，不能补造成功。
7. 不消费后来的 HEAD 改写 child 冻结事实。master 所消费值须来自通过既有 loader 校验的 child state，sidecar只是同 publisher 产生的 evidence；不以 sidecar 存在替代 identity/完整内容核验。不能由调用方携成本增量、all-ready flag、guard 基础数字。

携带者：child `SliceReceipt` 全值/digest → publication → master 接纳；master topology+SliceChildren → exact expected pair；现有 envelope/current requirement 与 receipt bindings → 准入；accepted receipt map → cases/progress/query；receipt Cost → Merge/child owner 最小累加 → master 同事务投影；Open/Drive → 发现已成立事实 → 同接纳入口。

当前 `protocol/requirement.go` 的 `admitRequirementDecision` 在已授权 RedoTopology 时执行 `state.Topology = nil`、在 RedoRoute 时执行 `state.RouteMode = ""`；这是正常用户需求变化入口，不是损坏状态。新增 accepted receipt 的完整性核验只能核其冻结内容、摘要和当时的接纳身份；新接纳/当前就绪资格才按当前有效 requirement/topology/map 判断。必须核对 Phase A/B、重开和查询仍能处理合法的上下文变更，陈旧 receipt 不得被当成当前 all-ready；不能将合法旧证据判为 StateIntegrity 错误，从而阻塞需求决定。沿原 scope/binding owner 处理必要失效，不在本块展开尚未交付的 child 级联、重新创建或计量路径。

## 受影响入口与删除要求

核查 protocol slice 接纳/load/commit/Plan、State/Run typed fields、facade child Drive 和 master Drive、Open 后恢复、Show/Status/Next/query、Cost.Merge 与 guard 帐本；沿所有实际调用者核查正常/重复/并行/恢复，不只使一个交接分支通过。

不改上块的 native 核验/成功 schema 语义，除非有本块事实证明必须调整且保持已批准需求；不得叠第二套接纳、master 重测 child owner、独立 accepted-owner map、第二 case 账本或扫描者。替代的等待说明/路径/注释先删旧后写当前语义，未被替代的 legacy 路径不动。

## 可执行验收

先在修前冻结快照用真实正常公开链证明未接纳，再实施。按照完整合同 §6 中本块适用项目：

- 从真实 Git child 开发、门、promotion、checkpoint 得到完整 receipt，child Drive 直接交接及 master Drive 重找交接均可；读取 master 原始 envelope/content 精确核 accepted map、每个来源 child、原 dispatch/cases/cost 数值。
- 缺一片、尚未认领、认领而尚未成立/未ready 的 child 都等待，不集成、不 seal；wrong master/slice/child/revision、不同 digest 零接纳/零汇总拒绝。不手工编辑内部 state 来造阻塞证据。
- 两 child 的同名 dispatchID 并发交接保持 `<childRunID>/<dispatchID>` 两条；same child 并发、重复、丢回应只一次；master 自己 dispatch/Owner 保持。token hit/miss/input/output 用原 transcript 已知答案逐项算出，UNAVAILABLE/无 owner 保持事实。
- 在 sidecar 后交接前、master commit 前/后响应前的现有副作用窗口中断，重开后的 receipt/cases/cost/进度同正常轨迹；恢复不重测 owner，不再跑模型/门。采用既有 FaultInjector/内部测试 seams，不加生产 fault 开关或第二恢复流程。
- 导入费用后 guard 不造 master spawn/attempt，正常加载与预算派生仍一致；全齐只等待 integration，重复 Show/Next/Drive 不新发开发或 ordinary终态；普通 non-split 完成与上块 child receipt 重放仍回归。
- 构建、相关范围包、并发/跨 run 锁相关 race、native diff-check。验证按真实风险收窄；不重复无关慢包。报告每条源码/执行证据、原始日志、全部失败/跳过/未验证及持久内容断言。

## 每次派发硬标准

完整读取 AGENTS.md、prompts/actions/development-worker.md、prompts/actions/start-readiness.md 和完整合同。正确性、质量、需求完整、简洁、耦合、可维护、高可读、优雅/设计一致、无不当硬编码、窄逻辑、过拟合、过度设计/防御/抽象、测试业务隔离均为交付条件；功能绿不能替代逐项报告。从不变量和唯一事实 owner 闭合根因，不围绕个别反例叠条件分支。显式携带上下文、正常/恢复同接纳、删除被替代路径/账本/标记；注释只描述当前方案，名称以行为/领域命名，无开发阶段/顺序/评审轮次痕迹；夹具断言实际方法/输入身份，期望来自需求。

新增概念之前按开发提示词原文逐一选择，任一步成立即采用：

1. 规格未要求它存在 —— 不写。
2. 仓库已有可复用的归属者 —— 复用或改造既有者，不新建。
3. 标准库可做 —— 用标准库。
4. 运行环境自带能力可做 —— 用自带能力，不自造实现、不新增依赖。
5. 已安装依赖可做 —— 用已装依赖。
6. 一行可表达 —— 一行。
7. 以上皆不可 —— 写能工作的最小实现。

更简不得削减已确认验证/错误/安全行为。每个新增状态或抽象说明现有能力为何不足，先引参考原文再给设计。按已确认 state.json 边界实施，不迁移数据库也不宣称通用数据库条款通过。连续两轮自测修复后仍失败时遵 development-worker 上报，不自行不断叠补丁。实施完冻结，不自行开始下一块。
