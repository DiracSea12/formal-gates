# 安装身份逐操作准入：主代理独立核验

状态：本块已接纳并提交6433dca282f684beb04198cf62126e969b921026。仅本块，全部阶段目标未完成。

## 身份与执行边界

原生比较 f61913c1a751465fb8c6f105145d5482aafc2379 → tree 1964ea3fc537dc24372dc5b6e3258c1653ba641b；29路径完整diff，2573行/124180字节，SHA256 127a793e4626960b9dff93383e7a5fac8cc7491dccaf02d5139684f4e60eeded。完整原生diff已读；固定解包670个源码blob相等；工作树与tree原生差异0字节。该tree已原样提交为6433dca，提交后HEAD^{tree}仍相等；测试在同内容固定tree上执行，不把tree本身当commit。

当前源码沿原 Admission/verifyAdmissionSnapshot、Store 事务与既有副作用归属完成准入；没有新通用guard/effect/registry/queue。root独立测试只在固定源码或实际临时安装上执行，不调用真实模型/宿主账户。实际ZCode大阶段矩阵未运行。开发者绿灯与门结论都不代替下表。

## 工程条件逐项当前证据

| 条件 | 主代理核实的当前证据与边界 |
| --- | --- |
| 正确性 | immutable token/boundary/different-package原始期望不变，f619已复现旧写与stale Drive错误；固定候选9项实际installed边界PASS。20项Facade正常/重复/并行/恢复与副作用窗PASS，核state等实际字节及native HEAD/index/tree；不宣称未运行路径正确。 |
| 需求完整性 | 当前12项需求逐项对源码：字段保存于StartRequest/startIntent/Facade；所有Store事务与恢复；Start prefix/失败清理；dispatch SPAWNED/UNKNOWN；native promotion；peer；child发布/master接纳；terminal intent/summary/cleanup/lifecycle；原只读入口与replay。后续组创建/集成/merge/最终切换不在本块，仍未交付。 |
| 质量与根因闭合 | 修复单位是own installation写权限的携带与唯一事务/副作用owner；非ABORT/eventID单分支。初始Drive、Save外效果、恢复补写/清理、另run的master/peer都按自己的原身份核。已执行事实仍由原intent/UNKNOWN/promotion接纳，不假撤销。 |
| 足够且最简 | Admission为原归属，原request/intent承载缺失的namespace/target/root/provider事实；Store通过一个callback隔离安装层，原coordination提供内核锁。需要这些carrier与准入窗才能满足跨HOME与held对象；没有新dependency、控制系统、hash扫描、第二bootstrap判定。 |
| 简洁性 | 八个生产文件变更围住原writer与消费者，helper直接acquire/release/write；无平行恢复/准入/兼容机制。43行共享Installation材料归原testkit，各测试不重复造registry；872行新增测试按实际安装业务窗组织，生产不承担测试安排。 |
| 耦合 | Store只接受AcquireAdmission回调，不反向import facade/validate；protocol保留原Store Save接纳。Facade提供own事实与具体VCS/lifecycle/receipt/summary写者；host.Lookup复用唯一provider别名映射。 |
| 可维护性 | 三类own request/intent重开（child/master/peer）不借caller；持久化normal/recovery同Save/Recover/Cleanup算法。锁图业务协调→registry→Store process/state，无跨Store递归。root跨进程等待时state可取、holder崩溃释放、CAS单胜者均实测。 |
| 高可读性 | 具名carrier表达namespace/target/root/provider/core token，Admission.acquire集中说明持锁边界/顺序；matchesRoot表达同目录判据，write表达实际写窗；错误区分typed stale与UNREGISTERED。新增注释描述当前约束，不追加历史叙述。 |
| 优雅与设计一致性 | 原四段持久协议、PendingPromotion、child receipt/master exact claim、terminal intent与summary继续接纳业务事实；安装准入围住既有owner，不新造业务流程。effect与result之间切换保留原native事实与intent。 |
| 无不当硬编码 | 生产身份来自resolver与持久request/intent/exact registry record；不匹配某run/action/测试token。cleanup名单为原保留文件合同，状态/error文本为既有协议值。测试输入token常量与registry实际材料一致，非生产打表。 |
| 无窄逻辑 | Store关口统一覆盖protocol写者，Save外具体owner同身份；recovery、peer、master、terminal和真正Git写窗也覆盖。不凭单个拒绝结果推断全链，root已核全调用链并执行相关业务边。 |
| 无过拟合 | unchanged own record可在无关epoch/不同包bootstrap后正常写；新代次可新建且不写旧peer；同值回放纯读；不同namespace身份分离。三个root外部oracle先定期望再修，字节未改；真实CLI不加生产hook。 |
| 无过度设计 | 不新建安装状态机、业务状态库、通用effect框架、队列、租约系统或旁账；新增概念都有明确缺失能力与原归属。state.json投影例外已批准，通用“控制状态数据库”项没有全局判PASS。 |
| 无过度防御 | 完整身份、root/provider/core token核验直接来自确认需求；没有权限/恶意手改状态/不支持平台的加固机制，不为未来多版本猜测新增回退。未做范围外测试穷举。 |
| 无过度抽象 | AcquireAdmission是Store必要依赖方向接缝，仅返回释放函数；实际effect仍用具体原owner，不开放策略/registry/plugin接口。matchesRoot复用标准库SameFile，不造路径解析框架。 |
| 测试业务隔离 | 非*_test源码检索只有testkit自身及其测试harness cmd引用testkit；实际业务cmd/Facade/Store没有新testing/testkit或INSTALLATION_TEST环境分支。subprocess入口与registry替换都位于测试，installed race通过普通stdin/安装操作。 |
| 唯一事实归属 | registry exact record唯一判当前安装active/token；request/startIntent只存原期望，不能更新/adopt新token。workflow仍冻结state.json唯一投影，不另存generation控制表或有效标记；epoch/bootstrap不冒充逐写权威。 |
| 显式跨接缝carrier | StartRequest admissionRegistryPath/admissionTarget/admissionRoot及既有package/record/provider/generation/lease/token→startIntent.Admission→Facade.admission→Store.acquire回调/dispatch闭包；master/child/peer各从自己request载入。 |
| 正常/恢复共路径 | Save/Recover/Cleanup仍原Store算法；recoverStartIntent用intent原Admission；PrepareCandidate仍PendingPromotion对账；receipt发现调用同publish/accept；terminal重试调用同commitTerminalCleanup。只寻找残留/已执行事实，不另写第二实现。 |
| 四类计数分离 | diff未修改调用轮次、任务代次、retry、业务repair计数；安装Generation与run Revision/CAS含义仍独立。不存在拿token/epoch充业务计数的新增逻辑。 |
| 替代路径删除 | 原Start整段registry长锁、unbound exported DispatchClaimBridge、无准入失败清理/盲defer intent删除、逐写verifyAdmissionBootstrap均删除；检索无该旧能力活跃调用或替代旁账。 |
| 当前注释与行为命名 | 新源码/测试名称按Installation/Recovery/Receipt/Promotion/Owner/Terminal行为；删除重复权威bootstrap说明与长锁责任旧注释；新增契约不含评审来源/轮次。既有未改过程命名不借此扩大本块。 |
| 测试期望与诚实夹具 | 接线旧negative只补其他合法身份前置；不改want。child/master保留receipt实际record/pkg/target但namespace与token不同；锁fixture结构化更新真实generation，原STALE期望不降；native Runner核真实reset/commit，CLI等到Open后的真实stdin边界。 |
| 原始证据与持久内容 | 外部before原FAIL、编译/夹具错误、后续corrected、原共享aggregate FAIL、主动中断/no-tests/SKIP均留原文件。root安装probe比较state/intent/receipt/lifecycle等完整内容；persistence cap/revision、promotion tree/index/HEAD、child/master独立文件、terminal同值summary均核内容。 |

源码定位（固定源码与当前工作树内容相同）：

- `internal/engine/facade/facade.go:68,378,411,627,666,790,1058,1433,1483,1786,1822,1828,1848,1883,1947`。
- `internal/engine/facade/cost_source.go:52`；`snapshot.go:91,149,168`；`owner_cost.go:325`；`slice_receipt.go` 原handoff/discovery/accept归属。
- `internal/engine/persistence/store.go:37,89,134,195,278`；`recover.go:58`。
- `internal/validate/engine_admission.go:34,44,61,180,217`；`internal/cli/cli.go:590,2212`。
- `internal/engine/facade/installation_test.go:92,118,180,215,240,307,331,355,419,468,631,672,747,811,855`；`internal/engine/acceptance/installed_start_test.go:207,291,328,417`实际进程测试；`internal/engine/testkit/installation.go`测试材料归属。

## 执行与限制

root原始证据目录 /tmp/operation-admission-root-candidate/root-oracles、root-focused，当前probe SHA与670源码blob核验在root-focused/integrity.json。9项installed PASS46.639587s，20项Facade PASS20.234197s，exact当前源harness PASS42.551664s，构建PASS1.297736s。wrapper与Go包耗时分开记账，父/子测试不重复统计。

共享harness原aggregate FAIL由预置driver产生（无可核VCS revision）；未改测试期望。其exact测试原业务因果链未被本块修改、也不调用Facade admission。固定f619和当前源各独立构建exact PASS，不能证明原失败根因，也不等于全harness PASS。阶段7实际退役/最终矩阵仍须完成。此前persistence/protocol/CLI、whitebox/validate包绿是较早整体候选，不能凭文件未改就称完整最终候选全套通过。最终完整Facade及installed公共四项证据另保留；live前置明确SKIP，真实调用/费用0。

复杂度门与质量门均为各自新建fork_turns:none，完整固定原生比较及当前需求；两份报告已全文读，原始执行identity/exit/失败证据已核。两门均PASS，无范围内P0/P1/P2/P3。这些结果与上表独立源码核验、不可变before/after以及所有受影响owner的实际边界证据共同支持本块接纳，不能代替上表。

质量门固定源副本完整facade/persistence/protocol/acceptance/CLI五package分别PASS498.876/14.660/75.303/65.087/124.924s；整组exit1因validate的两项缺bin前置及三项缺归档外历史材料。其自身补构建后两项准入复查PASS3.005553s，原exit1不改。root在两门结束后于真实repo独立实跑三项历史材料检查PASS3.593969s，核原始错误确为隔离前置，未把历史材料输入任何活跃门。测试、检查的SKIP/no-tests/失败前置仍各自保留，不称原整组或全仓PASS。

资产升级独立未接纳：root另从冻结旧be6a787源码实建原launcher，于临时HOME执行原公开codex global install/bootstrap成功；正常升级同6433源码tree包实际exit1 0.092956s，原owner拒绝新ZCode host及两个MJS manifest parts。189个HOME文件内容、registry原字节均不变，新release未出现。来源 /tmp/native-assets-root-upgrade-be6-1964ea3；没有读取/动用户实际全局安装，skip-hooks不冒称完整hostcanary。此项不因准入块通过而销案；候选维护接手的架构选择仍待用户裁定。

本块无已证实未闭合范围内功能/工程违反。资产升级、child组规则与冲突裁定、后续创建/集成/merge/cascade/guarantee及ZCode阶段矩阵仍待完成；没有阶段5/6/7完成或goal完成结论。未push。

## 原始证据归档

相邻 operation-admission-evidence.json.gz 保存逐文件原字节、SHA256、argv/exit/timing、完整diff与三份不可变probe。全部失败/跳过/前置错误保留，base64 round-trip已核；无正式run/Seal。
