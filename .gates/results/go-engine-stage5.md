# go-engine 阶段 5（Git 分片、child 与 merge 闭环）执行账

前置：阶段 4 已于 0edf3b1 收口（三轮机主复核通过）。本阶段依据
refactor-plan/incremental-seal-plan.md:373-392 与机主 2026-10-03 三轮更正
裁定（分片流程普通 QA 不选不执行；门每片各跑；路线机制照旧；记录见
memory/slice-verification-once-at-end.md）。

## 批 1：分片身份层（45b97f1）

范围：CONFIRM_TOPOLOGY split 分支补全（AC 责任映射校验＋同事务锁定
merge 路线）、child 创建（facade.Start 携 MasterRunID/SliceID，master 账
sliceID→childRunID 认领）、继承（child 不自增审查轮，相位门/账本 floor
等价放行，不伪造账本）、child 路线候选无 QA 项、merge master 不进开发段。

派发：单实施子代理（三节 7 步简洁法逐字入合同）＋两轮小修，均 fork 自
同一代理会话。无真实付费模型调用。

### 主代理独立复核

- 亲读全部 33 文件 diff（含测试）。
- 独立探针（/tmp overlay，非树内）：TestProbeChildBoundaryAfterRouteSelection
  复现 child 选完路线后边界提示停留"选路线"的缺陷 → 返修一后 PASS。
- 复核中发现第二缺陷（阶段 4 残留，非本批引入）：SELECT_ROUTE 在路线已
  绑定时仍被接纳，同 run 二次选择静默替换已生效路线，违反需求 7「路线
  只问一次」→ 返修二收口（Ask/决定双守卫、零写、Ask 存活；合法重选唯一
  通道＝需求变化勾选重走）。
- 返修二连带两处超合同明示的引擎改动（已亲验成立）：
  1. requirement.go 停等闭环守卫——pipeline 形态下绑定后只重走
     start-readiness 不重走路线的需求变化包被拒（开发计划确认搭载路线
     决定，finality 后无独立确认通道）；
  2. RedoRoute 保留 SelectedGates 作 add-only 基线（原清空会使同集重选
     被误判为开发后新增内建 QA 而拒绝）。
- 被重构既有测试 36 处逐条清单见子代理报告；抽读语义重构三例
  （CandidateMaterialization/ExplicitDisposition/DevelopedRouteRejects），
  断言未削弱，仅改走合法重走通道。

### 验证

- gofmt 空；go vet ./internal/engine/... ./internal/coordination/ 通过。
- go test ./internal/engine/... ./internal/coordination/ ./internal/harness/
  -count=1 全绿（facade 403s、protocol 83s、harness 389s 等，主代理亲跑）。
- 独立探针终态 PASS。

### 已知中间态（后续批次收口）

- merge 门/合并 QA 已绑定未执行（批 3）；SLICE_READY/回执/费用汇总（批
  2）；master 需求变化与 children 级联一致性（批 4——含认领后 master 修订
  需求导致同 id 重入被 revision 检查拒绝的占位滞留边）；分片 canary（批
  5，模型调用逐次授权）。
- split master 须在 CONFIRM_TOPOLOGY 前提交 REQUIREMENT 观察（AC 宇宙以
  冻结投影为权威）——canary 驱动注意此顺序。
- validateRouteChange 的直接二次选择防护已被 finality 守卫吸并，仅在重走
  重选路径上以基线形式生效——保持现状，不在返修轮里过度收敛。

费用：本批无真实付费调用；子代理派发 3 次（1 实施＋2 返修）。

## 批 1 返修（54a1965）：外部复核 FAIL（2 P1＋2 P2＋1 既有缺口＋1 过度限制）全数收口

复核方五探针留 /tmp（slice-main-oracle 等），主代理 overlay 亲跑全红坐实后派单代理六项返修；回收后亲读全部 13 文件 diff、重跑五探针（4 PASS＋#2 见裁定）、全量亲验。

- **F1/P1** child 无可执行计划：片内计划随路线决定内联确认（ADR-002 决策 1 原文出处；`DevelopmentPlanConfirmation.Proposal` 两形态不混携；载体步闩经继承结论）；开发派发断言到批次契约内容级。
- **F2/P1** 启动中断窗：完成判据统一 state.json（request.json 只是投影）；半启动残留经同一 Start 路径重建（认领同 ID 幂等）；成功路径 facade.go:724 删 intent 保证终态清理目录不被重建、重启仍被 Mkdir 拒。
- **F3/P2** 认领过 Phase A 屏障（复用 errWriteBarrier 单一闸）；**F4/P2** child 必须显式 regular（拒绝先于任何 master 写）；**F5** split Slices 逐项唯一（绑定点拒、零写）。
- **F6** 我方上轮引入的过度限制回退：§6.3.1（orchestration master-requirements:89）原文「开发前可重选或追加；开发后只可追加普通门…」——删一揽子 finality（保留 merge 拒绝）与「重走 readiness 必须同时重走路线」耦合守卫；validateRouteChange 恢复为绑定后变更唯一权威；finality 测试按 §6.3.1 语义重写＋直接通道正例。
- **探针 #2 裁定**：其"无计划路线决定也应产生开发"的期望先于设计存在，与 ADR-002「计划随路线确认」冲突；缺陷本体（合法路线后永久无任务）由携计划全程测试＋缺计划拒绝测试覆盖，探针按陈旧期望判红属预期。
- 验证：engine+coordination 全绿（主代理亲跑）；harness 满载一次 TestTransportPerMessageWaitHasNoTotalDeadline 红、隔离 -count=3 全绿＝计时 flake（无依赖路径，如实记录）；gofmt/vet 净；无过程标记。

费用：无真实付费调用；返修子代理派发 1 次。

## 批 1 补缝（683bd10）：child 计划换约纪律收敛

机主追问「是否根因级而非摞补丁」触发自查：主代理独立探针（/tmp/probe_child_plan_swap_test.go）坐实 F1 有一条缝没打穿——child 已确认计划、开发已派发后，追加普通门的路线重选携带内容不同的片内计划被静默接纳（普通 run 的逐批用户决定纪律被 child 分支提前 return 跳过）。修＝纪律收敛不分流：child 分支只留确认形态检查，prior-plan 换约比对对两种 run 形态同一条判定；连带闭合同源缝——child 计划确认的绑定 revision 此前返回空，派发绑定与 work-decision 形状校验断链，现回填确认决定时点的权威需求 revision。验证：探针 PASS（拒绝语与普通 run 同族）、protocol/facade 定向与全包绿（coordination 组合跑一次 FAIL 未捕获用例名、单独＋组合复跑两遍全绿，按满载 flike 如实记录）。教训：**新增形态的"特殊分支"每提前 return 一次，就是一条既有纪律的旁路——分支只许分流形态，不许分流纪律。**

## 批 1 二轮返修（本轮提交）：终态证据误删＋full 门链

外部复核再判 FAIL（P1 终态证据误删＋P2 full 分片门链＋2 P3 注释），主代理以复核 oracle 亲复现双红后派原子代理。修：①「run 已成立」单一谓词（state.json ∨ terminal-summary.json ∨ 发布窗 intent），Start already-exists 与 recoverStartIntent 共用——同 ID 重试只清过期 intent、终态证据零写；request-only 真半启动重建保留。②「无 QA」判据语义化（routeHasNoQAModes＝validationKinds 为空），三处 custom 模式名判据收敛为单一归属；QA-free full child 端到端到 wave COMPLETE。③两处 P3 注释失配更正。主代理独立复核：P1 oracle 以修后 facade 重建同点位 exit86 注入（首跑误注失败清理路径自纠）真进程重跑 PASS（摘要 1357 字节保留、Open 可用、重试拒）；P2 oracle 双子例 PASS；绕行 oracle 红＝其前提（MISSING_COVERAGE 卡死）已消失，按设计判；round-1 边界探针红＝其无计划选路线先于 F1 必填语义，陈旧期望。全量 engine＋coordination 亲跑绿。教训：**「无 X」类语义判据不得绑在模式/入口名上（custom≠无 QA 的充要），生命周期判据必须枚举同一事实的全部合法形态（活动态＋终态回落是 Open 侧早已承认的一组文件）。**

## 默认路线交付（76f9f0d、66c9f4f）

机主新增并确认：默认独立叫“默认/default”，选全部普通门、不含黑盒/白盒 QA；full 加“QA 阶段较重，适合大型任务”括注；custom 可选子集或全集并保留身份。推荐仍须确认，lightweight 是独立 start 模式。candidate child 普通 QA 不进候选、不选、不执行，各片仍跑所选门、master 强制合并验证；legacy 本块仅扩 default，不改变既有 full/custom/child QA。

本块按机主要求不走正式流程；使用已批准 `/tmp/default-route-implementation-contract.md` 与 `prompts/actions/development-worker.md`。实施代理 `/root/default_route_implementation` 为 fresh fork_turns:none，每次实施/返修指令携带全部代码准则。主代理只独立复核、记账与提交，无 push。基线 d8f24aa→最终 66c9f4f，完整 23 文件 diff；生产只有既有枚举、接纳点的动态过滤、help 和保证理由，未新增状态、配置或执行旁路。

### 实际验证和失败记录

- 开发初次新增并发用例误期望两个首次提交均成功；按真实 CAS 修正为一次接纳、合法 REVISION_CONFLICT 及原事件幂等重放。新真实 Git 完整链首次误用 reopen 后丢失的 fake runner，改用现有真实 Git 夹具；没有改生产纪律。
- 开发编译、定向四包、相关 race 通过。完整 protocol/facade/validate/CLI 组合 exit1：前三者中 protocol 73.133s、facade 428.803s、CLI 127.612s 通过，validate 461.557s 仅 metadata 小写 lightweight 词检查失败。修提示词形，不改该期望；相关 package/metadata/保证复核22.436s通过。
- 两条 qastructure 的既有 full/custom 精确文案期望按新需求增加 default，保留全部受理、确认与不自行触发断言，并加强三独立选项/推荐确认/child-master 边界。受影响 tagged 消费者分别2.761s、11.474s通过；保留原始失败 `/tmp/default-route-tagged-route-before.log`。完整慢包组合未重跑，不把历史 exit1 写为通过。
- 主代理独立 oracle `/tmp/default-route-business_test.go`：真实 Git regular 默认路线，交错双 QA 与 audit-local/readability-local，真实开发提交、候选、两个精确门输入、晋升、COMPLETE及终态 bytes 保持。d8 基线实际执行失败（不认识 default），交付实际执行通过2.20s；日志 `/tmp/default-route-baseline-check.log`、`/tmp/default-route-current-check.log`。首个基线 overlay 路径未规范化导致 no tests to run，未计为负控；修正后才取得执行证据。
- 主代理独立元数据负控实际失败；最终66上普通检查及相关 tagged 五用例通过2.308s：metadata、默认/full/custom菜单、start只接受lightweight及枚举；日志 `/tmp/default-route-final-metadata-check.log`。亲读完整生产、文档与交付测试 diff；新文案期望来自已批准三值要求，未降低断言。
- 初次不可变76手动两门各 PASS附同一P3词形项。其完整慢包分别出现10分钟超时、archive缺Git/bin前置失败，如实保留，不归因产品、不记整包通过。词形及需求期望调整后，最终66全新零上下文 complexity与implementation-quality各 PASS，无范围内发现。两份完整任务与返回存 `default-route-gates.md`。最终门实际覆盖当前相关路线包、child/master、并发 race、构建、help；质量门另在正常Git检出构建候选binary并通过 package validate。

### 主代理逐项工程验收

| 硬标准 | 当前代码/执行依据 |
| --- | --- |
| 正确性 | review.go 的同一 SELECT_ROUTE 接纳过滤后仍走计划/候选/choice/freshness/变更校验；独立真实 Git 完整交付与原生负控通过。 |
| 质量 | 核持久 mode/SelectedGates/决定、真实门身份、波次与摘要字节；相关编译、race、包校验和原失败消费者均有执行。 |
| 需求完整性 | 三独立身份、default全普通门、full括注、custom全集、推荐确认、lightweight、candidate child/master及legacy边界分别有实现/断言；两门逐项映射。 |
| 简洁性 | 一个要求中的枚举值；已有接纳者＋标准库 Clone/DeleteFunc，无新策略/状态/配置。 |
| 耦合性 | 下游QA/coverage/wave/guard继续消费SelectedGates；未新增模式名豁免或跨层依赖。 |
| 可维护性 | 普通门来自当前候选全集，动态新增门及独立非内建门名实际进入default；无固定普通门清单。 |
| 高可读性 | 模式常量、直接分支、具体禁止手选错误，文档明确每种身份与适用面。 |
| 优雅/设计一致性 | 沿原Ask/DECIDE、计划、当前绑定、投影与持久化链，未引入额外确认或恢复。 |
| 无不当硬编码 | 只排除已定义两个内建QA协议ID；普通门名称和数量不固定。 |
| 无窄逻辑 | 实际选门作为单一QA义务语义；普通、child及legacy各既有owner全范围适用。 |
| 无过拟合 | 混排QA、多普通门、动态门、custom子集/全集、child/master、拒绝、追加及重开/并发有需求来源证据，不按反例分支。 |
| 无过度设计 | 没有新增控制事实、旁路、节点、菜单账、费用账或发布机制。 |
| 无过度防御 | 仅文档化路线选择及常见手选/锚定/计划失误；没有增加对抗性状态防御。 |
| 无过度抽象 | 原两个运行时各扩既有接纳点，无新增helper/interface/框架/依赖。 |
| 测试业务隔离 | 新测试只在_test.go；没有测试数据、故障开关或生产测试分支。 |
| 唯一归属 | 当前路线仍在RouteMode/SelectedGates，历史决定仍在Decisions，查询只读投影；按已确认state.json边界实施，通用数据库条款不记通过。 |
| 正常/恢复共用与携带者 | Ask.Options/RouteGates→DECIDE.Choice/Route/plan→Decisions/当前字段→validation/store/Open；同事件并发及重开重放字节不变，同Submit/CAS路径。 |
| 诚实夹具/不降期望 | 真Git、实际Binding/Gate身份；按真实CAS允许冲突但核一次业务写；tagged文案改动有新需求原文并保留原验收能力。 |
| 删除替代路径/标记 | 原custom单门测试由同一两模式完整链替代；错误custom不能全选说明已替换；无并行机制/账本/开发标记。 |
| 注释命名 | 修改项描述当前路线/约束，新增行为测试无阶段/返修命名；未添加历史叙事。 |

结论仅为默认路线本块开发验收闭合，非正式流程 PASS或阶段5退出。剩余 checkpoint/收据、master接纳汇总、worktree/integration/merge、级联/guarantee、installed公共面与实机ZCode仍待后续。

机主另授权 active goal 推进剩余阶段5–7，每大阶段结束必须实际ZCode happy path＋大量业务non-happy path。全量构建/测试、隔离安装、版本/原生身份、故障恢复/清理及逐次真实模型授权要求纳入阶段退出；本块没有真实外部模型调用。不得用本块两门或窄测试代替阶段退出。

## 分片成功 checkpoint（e030dc3、e08c468）

授权：完整收据合同已批准；本块先交付 child 成功 checkpoint，下一块完成 master 接纳/汇总。当前原生基线 0c5400b93885e1edf1949ee1e4467a9fc44d88a8 → e08c468d5b163b9fee270b0d255f929a0e629e6e，14 文件、995 行新增/57 行删除。实施子代理使用完整 development-worker 提示词和全部硬标准；主代理没有修改业务代码，无 push。

### 原始证据与实际执行

- 开发先在原代码三路线真实 Git 链复现 child 错误 TERMINAL/COMPLETE，原日志 /tmp/slice-checkpoint-original-failure.log；主代理独立完整链在 0c 基线实际失败 /tmp/child-checkpoint-before.log（4.43s），未将 overlay 未执行计为负控。
- e030 初次完整相关包通过：protocol65.794s、facade412.186s、cost2.082s、vcs2.308s、persistence13.386s。开发首次 unused import 编译错误仅保留工具输出；lifecycle fixture 初次读取 legacy namespace 失败后改用实际 ActiveRunIDs；最终 race 一次 native Git rev-parse segmentation fault，原日志 /tmp/slice-checkpoint-race-native-git-failure.log，冻结代码重跑22.660s PASS，不包装为 race 通过的首次运行。
- 主代理完整 receipt 原生 oracle e030 实际 PASS6.529s；原生门/脏树/漂移/Owner/replay/普通终态相关十用例 PASS23.778s。主代理亲读完整生产和交付测试 diff。
- e030 两个 fork_turns:none 手动门均给 PASS 候选及一个 P3 纯内存别名。主代理未接纳：独立实际自动 RETRY→授权→PASS→promotion→checkpoint 正常链复现 LastRequest.RequestDigest 为空，违反已批准 receipt 条款。P2 的 root 失败日志 /tmp/child-request-evidence-before.log（2.599s），开发原样复现 /tmp/child-request-evidence-reproduced.log（3.160s）。修补根因是 Events/PendingAsks 两个既有正常请求来源，而非 RETRY 特判。
- e08 修补前后完整 diff SHA-256 6fcec3bc0b6f081496baa92d859f654f700437fd1b873270e48eda0cae719cc7。开发来源2.285s、checkpoint17.420s、未改原 probe5.081s、race18.900s、build0.82s均 PASS；全仓或未改慢包未机械重跑。完整开发报告 /tmp/child-request-evidence-implementation-report.md。
- 主代理 e08 未改原探针实际 PASS3.111s（/tmp/child-request-evidence-root-after.log）；三路线真实原生完整收据/字节/后续 HEAD/丢副本复建 oracle PASS6.434s（/tmp/child-checkpoint-root-final.log）；外部、自动 validation、review grant grant/decline、attempt recovery 与 generated requirement 来源五用例 PASS3.041s（/tmp/child-request-evidence-root-sources.log）。
- 主代理 e08 并发收口、后来 HEAD/owner 变化、receipt 副本丢失、intent_before/replace_after/commit_response_lost 三提交故障窗口及普通 terminal replay/peer 五用例实际 PASS12.781s（/tmp/child-checkpoint-root-recovery-final.log）。断言读取 state、sidecar 全内容和相同 bytes，不只检查存在。
- 主代理追加实际 Git gate FAIL→核实 P2→repair completion→新候选→promotion→checkpoint 检查，PASS3.755s（/tmp/child-repaired-gate-evidence-root.log）。收据保留真实 repair completion 作为终门义务证据，child 原始 FAIL 未改写；后续 master 接纳须沿该既有收口语义。
- P3 别名原生内存 probe 证实非零 receiver 保留 Cost pointer；没有正常持久损坏复现，维持 P3。与当前授权构造处同改为已有标准库零值 receiver，不新增 clone helper 或另开偏好返修。

### 主代理逐项工程核验

| 硬标准 | 当前代码/执行证据 |
| --- | --- |
| 正确性 | facade.verifyDelivered 共用于 ordinary seal 与 child checkpoint；promotion HEAD/tree/clean/ancestry 均原生核验；独立原负控及三路线完整 receipt 通过。 |
| 质量 | 持久 receipt/digest/phase/费用/endpoint 同 commit；构建、相关包/race、原生原失败与恢复证据俱全，首次失败如实保留。 |
| 当前块需求完整性 | SliceTerminalReceipt 全字段；SLICE_READY 非终态、无 summary/EndRun/提前 cleanup；selectedQA/cases 真实为空；最后请求两个来源；三路线、拒绝与恢复有内容断言。master 整体合同尚待下一块，未记完成。 |
| 最简/简洁（保留 P3 等价表达建议） | 复用当前 validationConcluded、promotion、已有 phase、Events/PendingAsks、canonical digest、owner 观察和 atomic writer；只增加需求明定 typed 冻结 receipt，没有新发布/计量状态机。 |
| 耦合 | native 观察归 facade/VCS；资格/冻结归 protocol/CAS；owner 归原 owner lock；publisher 只消费冻结值。两种请求来源解析留在 receipt 所属层，无要求其他调用者发伪事件。 |
| 可维护 | normal/recovery 同 frozen value/publisher，后来 HEAD 不改交付；请求读取以已有 owner 为依据，不按 kind/ID 前缀；批准 schema 字段自然参与 canonical digest。 |
| 高可读 | 小的共享 verifyDelivered 与 checkpoint/publish 直读主干；类型明示完整 identity/validation/route/request/cost，注释说明当前事务/恢复约束。 |
| 优雅/设计一致 | SLICE_READY 沿当前相位合法推进；owner observation 与普通终态同算法；零值 JSON receiver 沿现 prepareValidationResult，未搭复制框架。 |
| 无不当硬编码 | 不固定门数量/名字、请求前缀或 RETRY 处置；validation targets 来自真实当前 wave；native identity 与 owner 原来源如实冻结。 |
| 无窄逻辑 | 正常/重复/并发/发表恢复/closed peer/后续 native commit 贯穿；外部、validation、review grant、attempt recovery、generated requirement 请求来源全核归属。 |
| 无过拟合 | root oracle 与基线真实失效；完整 offer 期望由独立 JSON+SHA 算出，不把 Validation.Digest 或非空字符串替代 request digest；原 probe 未为过测试修改。 |
| 无过度设计 | 没有额外 owner/cost ledger、receipt queue、扫描成员表、clone/发布状态机、节点/数据库迁移；未提前增加 master/集成占位字段。 |
| 无过度防御 | 检查只为已要求身份、业务准入和正常 I/O/恢复，不对恶意改状态/权限/无支持平台添加机制。 |
| 无过度抽象 | 当前两个层各保自己的小函数和明定 DTO；既有 standard library 与 native Git，无新接口层或依赖。 |
| 测试业务隔离 | 夹具与 oracle 在 _test.go 或树外 overlay；生产没有测试分支或新增 public fault 开关，恢复复用既有 FaultInjector。 |
| 唯一事实/携带者 | wave/promotion→native proof→CheckpointSlice；OwnerCost+closed interval 同 commit；Events/PendingAsks+Decisions→LastRequest；state SliceReceipt→sidecar/query，副本可再建。state.json 按已批准例外继续，不把通用数据库条款写通过。 |
| 正常/恢复共用 | 冻结再发表，同 checkpoint 与 publisher；三 fault windows、repeat/parallel/late HEAD 均实际断言全值/byte 稳定，无恢复重测 owner 或重新跑门。 |
| 诚实夹具/期望 | 真 Git、实际 action/task/candidate、完整 request offer、精确已知 token；source-owner unit 明确不冒称全 child lifecycle；没有降低原断言。 |
| 替代删除 | 原 seal native 重复段改共享；child 独立成功 seal 拒绝；单 Events 摘要与非零 receipt 解码、过时注释已删除，无平行路径或账本。 |
| 命名/注释 | 新源码/测试按 slice checkpoint/request 行为命名，无阶段/修复/审查轮次标记；注释只说当前身份/约束。 |

最终全新零上下文手动复杂度门与实现质量门均 PASS，未发现 P0/P1/P2。主代理读完整返回并核原始执行输出：复杂度门 protocol59.413s/facade511.806s/cost3.552s/vcs3.773s、race109.871s与构建/diff-check通过；质量门原整包 exit143 主动中止、未记全包通过，随后 facade47.316s/race17.784s通过，三包为 Go cache、非重跑，protocol83.616s来自被中止组合。所有原始输入/返回/输出副本在 slice-checkpoint-gates.md。复杂度门排序 helper 复用建议经主代理源值核对是 P3 等价表达（没有行为后果），只记录，不能据此写“完全最简”或不存在一切建议。本块已核的范围内功能和工程条件闭合，可以推进 master 接纳；这不是整个收据合同完成、正式流程 PASS 或阶段5退出。没有收费 native 模型/安装副本/ZCode canary。费用：本块实施一次、返修一次，另有四个零上下文门派发；本记录不把协作代理当实际 ZCode 调用证据。


## 主运行冻结收据接纳（d2bd49a）

授权：已批准完整收据合同与全部剩余阶段 goal；本次不走正式流程、每块二门每次零上下文。实施代码由开发子代理完成，使用完整 development-worker 提示词与全部硬标准；主代理亲读完整原生diff/测试、独立原生负控与修后核验，再手动新二门及逐项验收。没有push或真实收费ZCode调用。

原生冻结 f546a8cb8bdb900a14e7a99cdc9b5a3b8b50861e → d2bd49aa0d5a4a333de5c9505d36495e533beaf1，9文件734+/10-；diff SHA-256 2cd42a1c9dee02140e0a65a370a68240f5cd3274f82b428266b60cc222cc0ad7。完整事实/两门原返回/原日志/独立输入证据文本与主代理逐项表在 master-receipt-gates.md。新增State.SliceReceipts为完整值接纳与幂等唯一事实，cases在原载荷；原cost.Merge namespaced dispatch与最小AddChildOwner同commit，不覆盖masterOwner、原spawn/attempt、不重估child guard。child锁释放后handoff，master只沿exact claims discovery；两入口共用原Recover+AcceptSliceReceipt，历史自完整性与当前资格分开，全部齐仍等待integration。

开发原生正常公开链负控FAIL2.759s；一次测试draft编译失败、两多提交Gitfixture漏squashmessageFAIL19.657s如实保留；随后behavior21.384s、扩展22.559s、最终22.815s、原protocol44.729s/cost1.638s、相关facade45.962s、race50.306s/最终并发8.642s及构建/diff-check均退出0。没有调低原断言或生产测试分支。

root独立原生费用负控FAIL2.637s；实际SPAWNED+lifecycle+parser已知答案负控FAIL2.979s，先核child真实输入与Owner再见master未导入。一次overlay未执行、provider夹具错误、等待投影/summary路径误猜如实记账，不冒充产品失败。最终3独立行为PASS7.203s：精确38input/7hit/31miss/19out，dispatch tokens32不含Owner；原Envelope/完整receipt/cases/QA/cost、原主账保留、缺片非终态、查询/reopen重放全字节不变；合法P2repair原FAIL仍FAIL且master正确接纳。root并发/exactpair拒绝/三个持久故障/需求三scope变更/ordinary终态边界PASS19.010s。

两新fork_turns:none门独立固定d2：复杂度PASS无P0/P1/P2/P3，protocol96.277s/facade471.366s/cost2.389s/race44.980s/build/diff-check退出0。质量五维分别PASS无P0/P1/P2/P3，behavior47.684s、cost0.752s/protocol94.400s、race29.816s、child/ordinary回归22.137s、屏障放弃变更探针3.006s及build/diff-check0；探针首次Ask遗漏abort选项正确INVALID_CHOICE，FAIL2.369s保留。无运行中止，count=1非测试结果缓存；非-v完整包不能证明逐项无skip。

主代理逐项核正确性/质量/当前需求完整性/最简简洁/耦合维护可读/优雅设计一致/无不当硬编码窄逻辑过拟合/无过度设计防御抽象/测试业务隔离，另核唯一事实/显式携带者/四计数/normal恢复同接纳/协议保真/诚实夹具/原始内容/替代删除/注释命名；每项当前源码与执行证据见完整表。state.json为批准§5.9例外，不宣称通用数据库条款实现；master当前无普通development plan，原Guard尚未开启，不冒称真实merge预算已验证。没有未解决范围内已证实问题，接纳当前收据块，继续后续工作。

这不是阶段5退出或正式流程PASS：独立child工作区/Controller创建、组容量/依赖、native主线integration、mergeQA/gate/受影响补门、组级需求变化/abort/guarantee/cleanup、实际installed公共面及ZCode大量non-happy实跑仍未完成，阶段6–7随后推进。真实native模型调用0、本块live canary0。协作代理不当作实机ZCode或付费模型证据。


## 公共启动与规范宿主身份（137b0ba）

固定6ba24cf→137b0ba3a682cf1f9af1172ff4ebd3b708999e1e，5路径481+/10-，生产仅17+/5-；完整593行diff SHA256 3d9ce5dc2d04cbc30703f8b05531929370fc52344e43be8c3b809d2c95a95ccf。候选公共regular沿既有Start/native base；RegistryRecord.Host经现host.Lookup.ID由EngineAdmission.Provider携带至原request/State.RunProvider，所有重开复用现opener，无新状态/driver/重复宿主检测。lightweight和default/full/custom原产品裁定保留。

主代理先核真实安装负控FAIL：同包lightweight成功、regular被拒；原样oracle修后actual installed PASS real6.28s/pkg5.649s/test3.39s，package ddcda1653f8110ca2e8f517473262d1662dbc6cce442501c622035191c4d80b4；再独立3个安装边界PASS real17.80s/pkg17.061s。核完整request/state/Envelope/receipt/bootstrap/base/provider、拒绝无写/readonly/重复/同ID并发/request-only恢复，未用存在性替代内容。完整原始失败、错误期望纠正及日志存 installed-start-gates.md。

开发七包整组原退出1/real453.64s，6包PASS而lifecycle受继承CODEX_CLI_PATH环境失败；root固定原6ba归档同环境FAIL、只清测试进程两个Codex信号后原基线与当前lifecycle PASS，相关源码无改动。此失败入账，不写全量绿、不扩展修无关fixture。开发相关race/build通过。

手动两门均新fork_turns:none：复杂度PASS无阻塞或P3；质量五维PASS无阻塞或P3。各自固定137b隔离source，实际installed/host别名/CLI writer/相关facade及独立18、37命令实际公共安装探针；最初missing bin与探针不受原spec支持的预期原失败如实保留。首次质量派发因thread limit未执行，名额释放后另派新的零上下文代理，非复用。主代理全文报告+source/diff+原执行复核、逐项工程要求当前代码证据完整见上文；没有未解决范围内已证实问题，接纳本块。

state.json仍按已批准§5.9例外；实际模型调用0/费用0，skip-hooks、人工typed intake/spawn和collector观察不冒充真宿主。未push，未启动正式流程。阶段五仍待Controller/工作区/依赖/容量/集成/mergeQA及门/受影响补门/组级生命周期/actual installed bridge和ZCode大量正常与非正常实跑，阶段六七尚未完成。root实际安装发现两条helper资产缺失，作为下一必要安装交付继续，不能据本块PASS宣称整个阶段通过。
