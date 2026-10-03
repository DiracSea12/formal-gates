# 公共启动与规范宿主身份：手动二门与独立验收

以下是本次实际文件的完整文本副本；每节含来源路径、字节数与SHA256，原失败不删除。审查者实际派发为fork_turns:none，只给当前需求/固定基线与改动/通用准则，未给开发报告、root oracle或既往问题。以下合同副本不是伪造的工具原始调用序列。

## installed-public-start-root-acceptance.md

来源：/tmp/installed-public-start-root-acceptance.md；bytes=10112；sha256=389c392eea8ccf12bbe59b0c254eebb692b656813c45448d4807c56b2c06599c

````text
# 公共启动与规范宿主身份：主代理独立验收

固定生产提交：137b0ba3a682cf1f9af1172ff4ebd3b708999e1e；基线6ba24cf502f9849b28bc14abb61bac8c2010076e。完整5路径593行diff SHA256 3d9ce5dc2d04cbc30703f8b05531929370fc52344e43be8c3b809d2c95a95ccf，主代理全文读完并核与开发冻结diff相同。当前结论：两名全新零上下文手动审查者分别返回复杂度PASS、实现质量五维PASS，无P0/P1/P2/P3。主代理全文读完两份报告、核原始执行、完整source/diff和独立实际安装oracle；当前块没有未解决的已证实范围内问题，接纳本块。此结论只对应下述当前需求，不表示阶段五完成。

## 已实际完成的主代理验证

- 原负控证据固定在/tmp/installed-public-start-original-evidence，不被后续重跑覆盖：当前可达的同包/同intake lightweight成功，而公共regular被lightweight-only拒绝，原有效负控退出1/工具墙钟4.536s、test3.17s。此前错误external test package造成编译失败，不当成业务负控，完整原log保留。
- 原样同一oracle在固定137b0ba新构建实际installed候选上成功：/tmp/installed-public-start-root-after.log，退出0/real6.28s，package5.649s/test3.39s。实际package ddcda1653f8110ca2e8f517473262d1662dbc6cce442501c622035191c4d80b4；receipt、registry、request/provider/base/完整Run Envelope、actualbootstrap及公共show内容核过；结果完整JSON另存/tmp/installed-public-start-root-after-observation.json。
- 3个开发者installed边界测试由主代理独立再执行：/tmp/installed-public-start-root-boundaries.log，退出0/real17.80s，package17.061s；regular真实安装/同launcher两host与scope/typedSubmit/只读/old-fence、invalid-start写前拒、nonGit light及request-only/并发/重复，均实际运行，不以编译代替。
- 初始开发fixture两处错误期望已按原owner独立核实：decision/decision.go:326规定外部全在途时WAIT/TASKS_IN_FLIGHT；coordination/lock.go:22,87规定快速同ID竞争可LOCK_HELD。原业务一成一拒及内容不变要求保留，不是为过测试削弱正确断言。IntakeReceipt实际字段IntakeDigest，编译错字段未执行行为；原错均入账。
- 原七包合并test退出1（real453.64s），不能记全绿；6包通过，lifecycle一项环境fixture失败。主代理在未改基线6ba的独立git archive上用相同原环境单项稳定复现FAIL（real0.86s/package0.474s），仅清当前测试子进程CODEX_HOME/CODEX_CLI_PATH后同基线PASS（real1.03s/package0.473s），当前lifecycle全量PASS（real1.14s/package0.607s）。源代码差异internal/lifecycle和internal/host为空，测试只清AI_AGENT和三个ZCode字段，而descriptor顺序和providerFromEnvironment确实先Codex。仅记录信号是否存在，无环境值/凭据。以上是该失败与本块无因果的独立证据，不修改超范围旧fixture，不掩盖原失败。
- 原二门派发均显式fork_turns:none；首次质量门工具因thread limit拒绝，未执行、不算门结论，待名额释放后成功派发新的quality独立任务。没有复用旧代理或喂历史问题/开发报告/主代理oracle给审查者。

- 复杂度原始实跑：3个actual installed、4个CLI writer、2个admission含11alias、8个facade相关行为及独立18命令实际安装探针通过；最初缺ignored bin、错误facade别名预期和非稳定launcher失败全部保留。质量原始实跑：3个actual installed、5个CLI writer、2个admission含11alias、12个facade行为及37命令公共链完成。其最初missing bin、unrelated ancestry错误文案预期、raw host别名等于canonical provider错误预期及不存在hooks目录的错误预期保留，按原owner纠正探针，没有修改产品或降低正确行为断言。主代理从实际request/state完整JSON核claude-code与codex/base/package/record相等，并亲读wrong-provider拒绝、正确spawn、显式collector输入与输出、lifecycle claim/verify、重复拒绝，未把skip-hooks/collector fixture记为真实hook/model。

## 按代码与工程硬要求逐项核验

| 要求 | 当前代码或执行证据及边界 |
| --- | --- |
| 正确性 | CLI 448–475只选择已确认route/base/provider，facade 484–518原验证复用；原真实installed负控转正、3边界实际执行。 |
| 质量/根因闭合 | Host事实由选定registry拥有，准入单点Lookup派生；两个resolver同实现输出，全部公共重开共opener，issue/receipt/claim/UNKNOWN/cost沿持久RunProvider，不是给每个入口补特判。 |
| 需求完整性 | 当前需求6条逐一有实际路径/断言；explicit regular、base、canonicalHost、lightweight保留、admission/intake/fence/恢复/并发/重复均核。后续桥/Controller/大阶段canary仍未交付，不以本块覆盖替代。 |
| 简洁/最简 | 两个生产文件17新增5删除的接线和内存携带者，复用Start/VCS/host.Lookup/registry; 没有必要更短等价实现的未采纳实证。 |
| 耦合 | 规范化在validate维护准入边界；engine不importhost，CLI不复制native校验/host检测，opener与cost/lifecycle边界未新增依赖。 |
| 可维护 | record→规范provider归属一处；重开从request记录精确record与原state读取，不重新探测environment。 |
| 高可读 | candidateRoute/candidateBase、Provider说明当前数据来源；帮助文字区分candidate默认lightweight与ordinary受理，test按行为。 |
| 优雅/设计一致 | 使用typed已有承载者+现owner，无第二启动/恢复/registry/状态机；不把个人排版偏好当事实缺陷。 |
| 不当硬编码 | 原Provider engine常量已替换为实际宿主规范映射；有限route值是产品语义，别名测试从host.All枚举。 |
| 窄逻辑 | 同resolver统一所有selected record，不按某一host/path/example加分支。 |
| 过拟合 | 五host全部11别名低层测试，以及真实ZCode global/Codex project、普通/child相关原native和route测试覆盖；无降低断言/固定打表回答。 |
| 过度设计 | 仅必要EngineAdmission.Provider派生字段，没有新控制状态/API/config/driver/writer。 |
| 过度防御 | 未为手改state/request/registry、恶意或不支持平台增加校验；既有Lookup错误归正常准入错误。 |
| 过度抽象 | CLI局部值与已有具体结构，测试组合原helpers；无通用化系统。 |
| 测试与业务隔离 | 新安装组合和JSON/helpers都在_test.go；生产没有test/fault/cost-source分支；root外部oracle不入编译树。 |
| 唯一控制事实 | RegistryRecord.Host→host.Lookup.ID派生值；state.RunProvider既有工作流投影。state.json沿冻结§5.9例外，不声称DB准则字面已完成。 |
| 正常与恢复共路径 | request-only的recoverStartIntent→同Start；Open加载原provider；正常与UNKNOWN的ClaimDispatch使用同明确provider回调。相关恢复值级对照实际执行。 |
| 显式跨接缝载体 | EngineAdmission.Provider→StartRequest.Provider/request.json→InitWithMetadata→State.RunProvider→Attempt责任/receiptcheck/claim/CostSourceRef；package/record/generation/lease/token、intake/base沿原字段。 |
| 四种轮次身份 | 没有生产改review.Series、TaskKey、Attempt次数或wave计数；admission generation仍只安装fence，未冒用；未穷举无改动的全部轮次。 |
| 移植等价 | 本块没有新port，复用nativeResolve/Verify/ancestor和host规范化；值保真已有协议。 |
| 被替代路径删除 | 原lightweight-only条件/文案、固定lightweight/engine赋值及忽略regular base在同处分支替换；无平行遗留路径/新账本/标记。 |
| 注释 | 新Provider注释是当前来源，test注释明确skip-hooks、typed前置、request-only合法窗口；无新增历史叙述。 |
| 命名 | 文件/fixture/function按installed start、admission、host、recovery行为命名，未混开发阶段/评审轮次代号。 |
| 诚实夹具 | 实际binary、安装事务、bootstrap/registry/Git是真；人工intake/spawn观察只作协议前置，未宣称真用户确认/真worker/lifecycle；alias fixture仅低层owner测试。 |
| 测试期望 | regular/native/Host/lightweight/fence/拒绝根据当前需求；错误WAIT/快速LOCKHELD假设按既有owner纠正，保留原日志及原副作用断言。 |
| 原始证据/持久内容 | 根负控/修后完整JSON，request/state/Envelope/intakeDigest/record/fence/bootstrap内容断言；不只核文件存在。开发失败/重试与原7包失败原raw已逐条读并保留。 |
| 正常使用 | 原根installedoracle及3行为测试实际退出0；普通/child/native/cost/lifecycle回归有原日志。 |
| 重复操作 | same-ID已建立run拒绝且state字节不变，不误删终态证据；相关原native终态/重入race通过。 |
| 并行推进 | actual installed双进程同ID一成功一拒；原claim互斥相关race通过；不声称后续group真实容量已接入。 |
| 恢复 | 合法request-only prefix与正常Start request/state值级相等、intent删；真实进程show/drive/submit重开同record；此块未穷举整个阶段副作用窗口。 |
| 性能/资源 | 多一次有限host表Lookup、两个局部值，无循环/后台资源/新工件；没有基准量化结论，测试隔离资源由现helpers/test管理。 |
| 派发/记账 | 开发子代理实现、root仅合同/oracle/记账/提交；全开发初始派发带literal worker与全硬标准；独立门无历史锚定。native宿主模型调用0，未push，正式流程未启动。 |

## 未完成边界

这不是整个阶段五完成。Controller/工作区/前置依赖/集成、merge QA/门/受影响补验、级联与终态guarantee、真实installed native桥、阶段末ZCode happy及大量non-happy、阶段六七均仍待实施和验收。root实际安装另证实helper资产缺失，是后续明确资产交付，不归此已完成启动块的事实。两个分片语义问题仍待用户决定，不默认选择答案；真实模型每个具体job需另有用户授权。

````

## installed-public-start-gate-requirements.md

来源：/tmp/installed-public-start-gate-requirements.md；bytes=6525；sha256=dc4c0903cf2ba08135923dbcd76d2a41d2cebe32071568b19b0a27aca3153c4c

````text
# 候选公共启动与宿主身份准入：当前需求

本次交付属于已授权的全部剩余阶段实施。本次不进入正式流程；主代理手动派发复杂度门和实现质量门，审查者每次零上下文。以下是当前确认需求，不是开发结果或审查结论。

## 交付与不变量

1. 唯一现有公共 `workflow start --intake-receipt` 分支支持明确的 `--route regular --base-snapshot <immutable native Git identity>`。复用已有 facade regular/native resolve、verify、ancestor 判据；原生基线只在 regular 中有意义。该分支空 route 保持既有 lightweight 默认，显式 lightweight 仍无 VCS 依赖。run 级 lightweight/regular 与后续 default/full/custom 选择是两个概念。
2. 实际被安装准入选中的 registry record 拥有 Host 事实；现有 `host.Lookup` 拥有宿主别名规范化。公共启动沿现有准入携带者把规范 provider 传给现有 StartRequest.Provider；request.json 和 state.RunProvider 保持它。不得以环境、launcher 路径、包名、CostProvider 或默认 engine 猜宿主；不得新增公开 provider/runtime/digest 参数。
3. 首次 Start、全部现有公共读写重开、已签发任务、lifecycle/cost claim、正常重开与 request-only 启动恢复沿现有 request/state 和精确准入记录保留宿主及安装身份。对这些已有能力先核对应语义；足够则不改，不增加按入口重复校验、另一套宿主检测或恢复实现。
4. 既有准入、版本、代次/lease/token 屏障、intake/artifact 绑定、重复启动、读写隔离、错误处理仍由已有归属者负责。未安装、缺失或不合法 immutable base、旧 intake、已禁用的候选 flags、安装换代的旧写入者等文档化正常使用/常见失误沿原规则拒绝。核关键持久化内容与拒绝副作用；不能为手工改写内部 request/state/registry 添加防御。
5. 允许改动面是上述现有公共 start 分支、现有 EngineAdmission 与其 resolver、直接相关的开发者测试/必要文案。现有 Provider 沿协议 issue/receipt/recovery 路径的消费者仍沿原归属。真实安装测试使用实际当前 binary、安装事务、bootstrap/registry、native Git 和公共 CLI；人工 typed intake 只作正常可达测试前置，不宣称是真实用户受理证据。
6. 验收覆盖本块有关的正常使用、重复/并发同 ID、request-only 重开、进程重开只读及继续 Drive/Submit、真实安装绑定/换代、相关原轻量和 child/普通 run 回归。测试夹具忠于实际协议方法、输入身份和已有错误语义，期望来自确认需求；不依赖私有 facade 代替 installed 公共证明。测试不能污染生产代码，不能以测试专用旁路/降低断言达成通过。

## 必须保留的当前产品裁定

- 后续 gate/QA 路线有独立“默认/default”（全部普通门，无普通 QA）、full（QA 阶段较重，适合大型任务）、custom；不得把默认命名为 custom。
- child 普通黑/白盒 QA 不进候选、不选、不执行，各片所选门仍执行。master merge QA 和 merge gate 强制；非分片普通 run 保留原语义。
- 冻结 master-requirements §5.9 对本项目明确维持 state.json 唯一控制投影，不在本块迁移数据库。
- 独立公开 reset/recovery 已被用户关闭；恢复是现有 Open/Drive 的性质。本块不开放这类命令。

## 非本块交付

Controller 自动 child 创建、独立工作区/依赖/集成、merge 验证与影响补验、级联/guarantee、完整 installed native 执行桥、全部大阶段 ZCode 实跑、最终旧面删除与发布切换，属于后续既定能力。不能拿这些尚待开发的能力作为本块已完成声明，也不能在本块重新设计它们。公共手动 child start 仍拒绝；未来内部 child 创建消费 master 的实际 provider/admission。

## 引用的现有权威原文（先引用再判断设计）

`refactor-plan/final-implementation-draft.md §2.1`：

> 最终只公开以下 workflow 入口：
> 1. `workflow start`：唯一 bootstrap 写入口。
> 2. `workflow drive`：无外部事件，推进到下一外部边界。
> 3. `workflow submit`：唯一外部事件写入口。
> 4. `workflow show`、`status`、`next`、`diagnose`：严格只读。

同文受理原文：

> 候选 `start` 通过 `--intake-receipt <path>` 或固定 host config
> 指针读取，不能自行生成回执、读取 stable run state 或用 runtime/identity/digest 参数替代；旧回执配
> 当前工件必须在首写前稳定拒绝。

`internal/validate/engine_admission.go` 类型契约：

> EngineAdmission is the immutable runtime identity selected by the stable
> install registry. The candidate workflow surface accepts this identity
> only as a launcher-derived value; callers do not choose a runtime, target
> identity, or package digest themselves.

`internal/lifecycle/lifecycle.go` 显式 claim 归属：

> Provider identity comes from the claim context, never from a launcher path plus an ambiguous working-directory heuristic.

`refactor-plan/incremental-seal-plan.md` 安装真实性原文：

> 候选安装的可用性证明必须执行安装目标中的实际 binary，并从其实际安装路径解析 `prompts/`、`gates/`、hook 和 host provider；不能执行 source worktree、固定全局 binary 或过期候选来冒充当前候选。

完整权威原文以固定待审提交中的对应文件为准。当前需求与工程规则是逐项判据；开发者测试绿色不能替代完整源码、需求/边界及工程标准核验。

## 工程硬标准

完整按固定提交 AGENTS.md 的“代码与工程准则”和“代码要求的验收执行”逐项落实：正确性、完整需求、质量/根因闭合、最简、耦合、可维护、高可读、优雅与设计一致、不当硬编码、窄逻辑、过拟合、过度设计/防御/抽象、测试与业务隔离、唯一事实归属、正常/恢复同接纳、跨接缝携带者、四种轮次身份、诚实夹具与业务 oracle、原始证据/工件内容、被替代路径删除、当前注释与域命名、性能/资源及实际记账。每项附当前源码或执行证据；不能核验的项写未验证，不用笼统 PASS 或几条窄测试替代。设计上存在更简单既有归属者/原生能力时即采用，不能以内在自洽为多余机制辩护；纯偏好/无等价实证的简化建议仍遵循 reviewer-base 的 P3 边界。

````

## installed-public-start-execution-brief.md

来源：/tmp/installed-public-start-execution-brief.md；bytes=21278；sha256=56344c2dbc012c4d295551ddf874366ff7e559241f57e8951fc637921d23a48f

````text
# 候选公共启动与宿主身份准入：实施合同

本合同由主代理依据已授权全部剩余阶段goal、既有明确公共面规格、当前原生源码与实际installed负控确定；这些常规实现无需重复用户许可，本次不走正式流程。固定基线6ba24cf502f9849b28bc14abb61bac8c2010076e；主代理核过internal/cmd/definitions/prompts/gates/manifest与d2bd49a无生产变化，收据块已逐项验收，工作树干净。后续完整bridge只读取证仍独立进行，本块已有下列精确源码引用及原生负控足够派发。此块不依赖分片并行度/前置冲突待答问题。

## 交付边界

沿唯一公共 workflow start 的 --intake-receipt 分支支持明确 regular+真实immutable BaseSnapshot，并从实际已接纳安装record派生宿主provider身份。轻量仍无VCS依赖；候选空route既有lightweight兼容默认本块保留，不把run级lightweight/regular与后续default/full/custom路线选择混为一谈。保留普通三路线/default文案以及child QA裁定；此块不重造正式受理/选择拓扑/RouteMode，也不开放public manual child启动（Controller创建稍后）。

同一实际安装身份经start、request持久化、protocol host binding、reopen/common CLI writer/query及子实例后续继承完整携带；不能在首次start填正确、随后Open或native lifecycle/cost又按环境/launcher其他host降为engine/default。先核这些后续既有能力是否已经保留正确身份，足够则不改。所有不支持/现合同错binding/缺base/旧intake拒绝仍写前稳定，沿原准入与版本屏障顺序。不能为手工篡改request新增防御校验。

此块止于实际不可变已安装Git regular可start、drive到现有真实外部边界、submit按原schema与show/status/next/guard-preview等原入口正确重开。真实模型执行桥、candidate/promotion注册适配器公共执行、Controller/workspaces/integration/merge/lifecycle/最终旧面删除是后续能力，不宣称本块全canary闭环。不加新workflow command、--provider/--runtime/identity/digest参数、辅助host config、第二launcher/bridge/宿主detect映射或test-only生产通路。

## 原文先于方案


### refactor-plan/final-implementation-draft.md:21

```text
## 2. 公共入口与控制循环

### 2.1 公共面穷举

最终只公开以下 workflow 入口：

1. `workflow start`：唯一 bootstrap 写入口。
2. `workflow drive`：无外部事件，推进到下一外部边界。
3. `workflow submit`：唯一外部事件写入口。
4. `workflow show`、`status`、`next`、`diagnose`：严格只读。
5. `workflow guard-preview --input <basis.json>`：公开只读、非权威的生产 guard 计算预览（schema/fixture 见 `references/guard-preview.md`），不创建 run、不写状态、不认证真实确认或任务合法性。3.5b 固定 `runtimeIntegration=NOT_INSTALLED`；阶段 4 才从权威 engine 状态呈现运行时集成。

安装、卸载、hook、lifecycle、canary 与脱离 run 的快速 gate 检查仍属维护面，但不能写 workflow 状态。`drive --event`、`submit-result`、公开 `cleanup` 及所有旧推进命令全部删除；旧能力若仍需要，只能成为引擎内部 handler。
```

### refactor-plan/final-implementation-draft.md:230

```text
受理确认的 typed `intakeConfirmationReceipt` 以固定 stable driver 已写入的确认状态为权威来源；
固定 launcher 从该状态和登记的 requirement/solution 工件派生回执并落到 formal run evidence，再按
原相对路径注入候选 test project。候选 `start` 通过 `--intake-receipt <path>` 或固定 host config
指针读取，不能自行生成回执、读取 stable run state 或用 runtime/identity/digest 参数替代；旧回执配
当前工件必须在首写前稳定拒绝。stable driver 不因候选 engine 阶段新增 writer 或公共入口。

若用户尚未明确，受理时选择 lightweight 或 regular。lightweight 是 bootstrap 例外，不经拆分和 full/custom 路线，登记需求后直接 Seal 并标记 unverified；regular 不能在运行中改成 lightweight。
```

### refactor-plan/incremental-seal-plan.md:126

```text
registry 不是某个项目的 `.gates/tmp`，而是由安装清单钉死的、跨 global/project scope 共用的用户级 registry root；候选验证通过显式配置使用完全不同的 registry namespace。每个安装 target、host hook、项目 root、state/resource root 和 runtime identity 都必须有 registry record，record 的 canonical paths 与 scope 是可机械校验的。阶段 0 必须先交付 admission bridge/launcher：所有文档化的 global/project 安装 target 和 host hook 都指向该 launcher，真实 immutable runtime 放在 launcher 管理的 sibling 路径，不能再暴露未登记的绝对旧 binary。首次 bootstrap 的公开入口固定为冻结 stable driver 提供的 `install --bootstrap` 维护动作；它只建立 registry/launcher record 和 bootstrap receipt，不创建 workflow state。

bootstrap/migration 必须覆盖现有 global 安装和受支持的 project roots：冻结 stable driver 先取得 install/uninstall lock，再取得 registry lock，写入 bootstrap intent，逐项登记 root/scope/host/runtime、epoch/generation、lease/token 和 canonical-path receipt；所有 record 提交成功后才允许第一次 `workflow start`。registry 不存在时允许这一次 bootstrap 创建；已有 record 缺失、冲突、registry 不可达或无法对账时，只留下可审计的 disabled/`UNREGISTERED_INSTALL` receipt 并停止，不得先创建 `.gates/tmp`。之后 `workflow start` 在创建 `.gates/tmp` 前再次 admission/校验 root；无法登记或发现 `UNREGISTERED_INSTALL` 时在写入前硬拒绝。已有 project-scope target 必须迁移到 launcher 或留下可审计的 disabled/待处理 receipt；最终切换遇到任何未登记 target、root、旧 launcher lease 或未知 scope 都只能 Operator/Wait。legacy launcher 为固定稳定驱动取得覆盖整个进程调用的 invocation lease，并在入口检查 admission epoch；engine 则在每个 intent/receipt/commit/cleanup 使用 per-operation fencing token。两者都不能通过直达 runtime sibling 绕过 registry。
```

### internal/validate/engine_admission.go:11

```text
// EngineAdmission is the immutable runtime identity selected by the stable
// install registry.  The candidate workflow surface accepts this identity
// only as a launcher-derived value; callers do not choose a runtime, target
// identity, or package digest themselves.
type EngineAdmission struct {
	RegistryPath  string
	RecordID      string
	Target        string
	PackageDigest string
	Generation    uint64
	Lease         string
	Token         string
}

// ResolveEngineAdmission reads the existing install registry and resolves the
// installed package that owns a candidate engine run.  It is deliberately
// read-only: candidates cannot bootstrap, repair, or write the stable
// registry.  Any missing, inactive, conflicting, or unverifiable record is a
// stable UNREGISTERED_INSTALL rejection before the engine state directory is
// created.
func ResolveEngineAdmission(root, packageRoot string) (EngineAdmission, error) {
	return resolveEngineAdmission(root, packageRoot, "")
}
```

### internal/validate/engine_admission.go:152

```text
	}
	packageDigest := strings.TrimSpace(record.PackageDigest)
	if packageDigest == "" {
		packageDigest = strings.TrimSpace(record.InstalledDigest)
	}
	if packageDigest == "" {
		return EngineAdmission{}, fmt.Errorf("UNREGISTERED_INSTALL: registry record %q has no package digest", record.ID)
	}
	if err := verifyEngineBootstrapReceipt(registryPath, record, packageDigest); err != nil {
		return EngineAdmission{}, err
	}
	return EngineAdmission{RegistryPath: filepath.Clean(registryPath), RecordID: record.ID, Target: filepath.Clean(record.Target), PackageDigest: packageDigest, Generation: record.Generation, Lease: record.Lease, Token: record.Token}, nil
}
```

### internal/host/host.go:180

```text
func Lookup(value string) (Descriptor, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for _, descriptor := range descriptors {
		if descriptor.ID == normalized {
			return descriptor, nil
		}
		for _, alias := range descriptor.Aliases {
			if strings.ToLower(strings.TrimSpace(alias)) == normalized {
				return descriptor, nil
			}
		}
	}
	return Descriptor{}, fmt.Errorf("unsupported host %q", value)
}
```

### internal/lifecycle/provider.go:264

```text
	lenientProjectMatch := false
	lenientRootLength := -1
	for _, record := range document.Records {
		if strings.ToLower(strings.TrimSpace(record.Status)) != "active" {
			continue
		}
		descriptor, descriptorErr := host.Lookup(record.Host)
		if descriptorErr != nil {
			continue
		}
		launcher := record.LauncherPath
		if launcher == "" && record.Canonical != nil {
			launcher = record.Canonical["launcher"]
```

### internal/cli/cli.go:439

```text
		return code, err
	}
	if hasFlag(args, "intake-receipt") {
		if hasFlag(args, "from-sealed-run") || hasFlag(args, "from-sealed-reason") {
			return 1, fmt.Errorf("candidate engine start does not support --from-sealed-run")
		}
		if hasFlag(args, "split") || hasFlag(args, "master") || hasFlag(args, "retained-overall") {
			return 1, fmt.Errorf("candidate engine start does not accept --split, --retained-overall, or --master")
		}
		if route := strings.ToLower(strings.TrimSpace(*route)); route != "" && route != "lightweight" {
			return 1, fmt.Errorf("candidate engine start only supports --route lightweight")
		}
		receipt, err := readEngineIntakeReceipt(*intakePath)
		if err != nil {
			return 1, err
		}
		runID := strings.TrimSpace(*runID)
		if runID == "" {
			return 1, fmt.Errorf("workflow start candidate requires --run-id")
		}
		candidatePackageRoot := *pkg
		if strings.TrimSpace(workflowFlag(args, "package-root", "")) == "" {
			candidatePackageRoot = ""
		}
		admission, err := validate.ResolveEngineAdmission(*root, candidatePackageRoot)
		if err != nil {
			return 1, err
		}
		_, run, err := facade.Start(facade.StartOptions{Root: *root, Request: facade.StartRequest{
			RunID: runID, Route: "lightweight", Provider: "engine", DefinitionSource: facade.DefaultDefinitionSource,
			DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: receipt,
		}, PackageRoot: *pkg, ArtifactRoot: *root, Admission: &facade.Admission{RegistryPath: admission.RegistryPath, PackageDigest: admission.PackageDigest, InstalledTargetIdentity: admission.RecordID, Generation: admission.Generation, Lease: admission.Lease, Token: admission.Token}})
		if err != nil {
			return 1, err
		}
		return printValue(streams.Stdout, run, err)
	}
	if strings.TrimSpace(*fromSealed) != "" {
```

### internal/cli/cli.go:2226

```text
	// The run request carries the exact registry record selected at start. Use
	// that identity first so one stable launcher can own several project/global
	// records; resolving by launcher alone would report every sibling as a
	// conflict. The façade is opened before the root binding check so malformed
	// copies still surface their UNSUPPORTED_RUN_VERSION barrier rather than a
	// misleading admission error.
	admission, err := validate.ResolveEngineAdmissionByRecord(request.InstalledTargetIdentity)
	if err != nil {
		return nil, true, err
	}
	if request.PackageDigest != admission.PackageDigest || request.InstalledTargetIdentity != admission.RecordID ||
		request.AdmissionGeneration != admission.Generation || request.AdmissionLease != admission.Lease || request.AdmissionToken != admission.Token {
		return nil, true, fmt.Errorf("UNREGISTERED_INSTALL: engine run identity does not match current admitted launcher")
	}
	f, err := facade.Open(root, runID)
	if err != nil {
		return nil, true, err
	}
	// Once the envelope has passed its version/identity barrier, enforce the
	// current project's target binding. This catches a run copied into another
	// project while preserving the more specific version diagnostic above.
	bound, bindingErr := validate.ResolveEngineAdmission(root, admission.Target)
	if bindingErr != nil {
		return nil, true, bindingErr
	}
```

### internal/engine/facade/facade.go:484

```text
	if strings.TrimSpace(req.Route) == "" {
		req.Route = "lightweight"
	}
	if req.Route != "lightweight" && req.Route != "regular" {
		return nil, Run{}, fmt.Errorf("engine start: unsupported route %q", req.Route)
	}
	// 分片 child 是正式 run：必须显式以 regular 启动（sliced-runs 需求 10
	// ——拆分与拆分决定记录仅限正式 run，轻量不增加任何拆分或记录）。拒绝
	// 先于任何 master 写（认领不在此时点发生）。
	if (req.MasterRunID != "" || req.SliceID != "") && req.Route != "regular" {
		return nil, Run{}, fmt.Errorf("engine start: a slice child must start on the explicit regular route")
	}
	if req.Route == "regular" && strings.TrimSpace(req.BaseSnapshot) == "" {
		return nil, Run{}, fmt.Errorf("engine start: regular route requires an immutable native base snapshot")
	}
	// regular 路线把 BaseSnapshot 从字符串变成事实（镜像 legacy workflow Start
	// 的快照接线：base 必须可复现且是原生 HEAD 的祖先或相等）。lightweight 不
	// 引入 VCS 依赖。
	var vcsProvider *vcs.Provider
	if req.Route == "regular" {
		vcsProvider = vcs.New(root, options.VCSRunner)
		current, err := vcsProvider.Resolve()
		if err != nil {
			return nil, Run{}, err
		}
		if err := vcsProvider.Verify(req.BaseSnapshot); err != nil {
			return nil, Run{}, err
		}
		req.BaseSnapshot = strings.ToLower(strings.TrimSpace(req.BaseSnapshot))
		if err := vcsProvider.IsAncestorOrEqual(req.BaseSnapshot, current); err != nil {
			return nil, Run{}, fmt.Errorf("base snapshot %s: %w", req.BaseSnapshot, err)
		}
	}
	if strings.TrimSpace(req.Provider) == "" {
		req.Provider = DefaultProvider
```

## 补充当前只读事实来源

独立固定d2取证 /tmp/installed-public-execution-contract-facts.md §2.2 与原源码确认本块已有能力：RegistryRecord.Host、host.Lookup.ID、StartRequest.Provider→State.RunProvider；normal Open/Drive/issue/explicit lifecycle BindDispatchWithProvider/cost已经保留原provider。其“原Host经CLI规范化”是一个最简建议；在现EngineAdmission维护边界返回规范Provider同样复用唯一host.Lookup映射，符合当前合同，不为一个transport字段要求新增Host第二份或改protocol/重新校验所有入口。完整真实执行桥、assets/容量/cleanup等其余取证属于后续块，不能倒灌本块，也不当功能通过证据。该固定提交没有tracked .harness/HANDOFF-STAGE5.md，主代理实际工作树读交接而非要求事实代理猜内容。

## 唯一事实、不变量、最简方案与携带者

- 已有RegistryRecord.Host是宿主事实源，现host.Lookup(...).ID已经是lifecycle provider同值；不能复制造Host→Provider switch/表。沿EngineAdmission现有read-only解析返回必要规范身份，CLI消费该身份到已存在StartRequest.Provider、protocol宿主binding，不加第二状态字段/账本。
- package/record/generation/lease/token/base/intake仍沿原source/admission/facade/commit。现Start已有regular NativeResolve/Verify/IsAncestorOrEqual能力，公开start只补真实输入，不重做Git校验/driver/private启动。
- 已有openEngineIfPresent是全部公开engine run重开入口，按request中record ID取得精确已注册身份；续Drive/Submit/只读入口消费原run的已冻host binding，保留原Envelope/version/root-binding错误优先序。原provider/bridge准入仍由现归属持有；仅规格必需且现能力确实不足时扩它，不给每个CLI命令叠检查或为手改metadata添防御分支。
- normal/recoverStartIntent同Start owner；不会用当前环境/多host launcher扫描重定义原run.provider。已有source能力不足之处只新增必要transport字段/验证，不引新控制状态/版本旁账；若需要新增抽象，先说明既有能力为何不足。
- 原CLI写死lightweight/provider engine分支、失效“仅lightweight”帮助/注释/测试fixture根因都需被同一准入收敛替代；旧与新候选启动路径不得平行。内部未注册memory级fixture不当actual installed证明，不为保留测试green加入生产fallback。旧已安装run不迁移、不作跨包版本兼容承诺，见incremental:124。

## 原始负控与最小充分验证

主代理树外 /tmp/installed-regular-start-business_test.go 使用现PackageReceipt+实际新构建candidate+真实install/--bootstrap，候选专属home与真实Git临时project，hooks显式skip（它不是实机ZCode canary）。先以同intake/同registered package成功lightweight控制，再通过唯一public CLI要求regular+真实base。实际退出1/facade验收包4.536s，用例3.17s，返回candidate engine start only supports --route lightweight；原日志 /tmp/installed-public-start-original-failure.log，完整registry/bootstrap/intake/base/args/output /tmp/installed-public-start-original-observation.json。其第一次探针包名写错导致编译失败（无测试执行），原日志独立保留 /tmp/installed-public-start-original-compilation.log，不当负控。修复必须先在固定原生基线上原样复现实际业务负控；探针不修改，不降低期望，树内收编按行为自写，不复制过程出处。

必要验收：
1. 实际新构建不可变candidate+完整真实runtime包install/bootstrap，--intake-receipt --route regular --base-snapshot正确建立state/request/intake/content与原native identity，actual registered host provider进协议；真实未安装、陈旧intake、缺/错base、unsupported flags写前拒且无legacy/state旁写。轻量不引Git要求。
2. 现五host alias规范映射在同EngineAdmission owner验证（不用真模型），same exact record及项目/全局选择、多host共享launcher+按record重开不串台；UNAVAILABLE费用能力如实，不伪造transcript/lifecycle。
3. publicDrive/Submit/Show/Status/Next/guard-preview等现统一重开入口及normal启动中断request-only/intent重建保持原host+package+intake+nativebase；迟到/旧安装fence沿原拒绝，不向metadata补写猜测host。
4. 明确保留原child regular继承、receipt/master接纳、default/full/custom路线与ordinary terminal/current owner行为；修改面相关包/build/race足够即停止，原全包无新风险不机械重复。生成schema/定义仅实际变动要求时再动。
5. 持久工件断言内容与原Envelope/token/host/native绑定，fixture核实际method与身份；不得通过fake安装回执/统一成功runner/privateFacade/testkit声称public/installed通过。大阶段实机ZCode happy+大量nonhappy另行准备，真实模型逐次授权。

## 每次开发硬标准

正确性、质量、需求完整性、最简简洁、耦合、可维护、高可读、优雅/设计一致、无不当硬编码、无窄逻辑/过拟合、无过度设计/防御/抽象、测试业务隔离逐项是交付条件。不能凭少数绿色测试/两个门PASS宣布完成。必须先列业务不变量、唯一owner、显式跨接缝携带者与全部受影响入口并引用既有实现/准则再选择方案；新状态/抽象逐个证明规格必要及现能力不足。禁止对反例叠条件；被替代路径/账本/标记删掉，不留fallback平行机制。normal与恢复共用业务/接纳；四计数分清；真实协议及输入身份，原证据/内容/轨迹对照，期望来自需求不调低。

注释只写当前方案/约束、删除过时旧注释再写，不写演进故事；源码/测试/工具按行为和领域命名，无阶段/批次/评审/修复过程名。测试只在测试层，业务无test/fault开关或自制resolver旁路。控制状态按已批准§5.9 state.json例外，不作DB迁移/通用DB准则已通过声明。只为正常与常见误操作；P3风格/攻击/权限/不支持OS不阻塞、不凭假设加固。root实施仅合同/oracle/复核/记账/提交，业务由一子代理；开发者不派新代理、不启动正式run、不改QA/冻结需求/账本，不commit/push、不付费模型。

七步逐字：
1. 规格未要求它存在 —— 不写。
2. 仓库已有可复用的归属者 —— 复用或改造既有者，不新建。
3. 标准库可做 —— 用标准库。
4. 运行环境自带能力可做 —— 用自带能力，不自造实现、不新增依赖。
5. 已安装依赖可做 —— 用已装依赖。
6. 一行可表达 —— 一行。
7. 以上皆不可 —— 写能工作的最小实现。

更简不削减已确认验证/错误处理/安全行为。每次派发开发提示词全文。先固定原生快照，每条新交付路径显式add后写；不add整个工作区。报告完整diff/hash/新概念必要性/替代删除清单/逐项工程核验/原失败和全部执行退出码、缓存、skip、费用、未验证。连续两轮自测修复仍失败上报；结束freeze，root独立原样负控修后及准则核验，再两个fresh fork_turns:none完整diff门审，当前块接纳后继续下一块。

````

## installed-public-start-implementation-report.md

来源：/tmp/installed-public-start-implementation-report.md；bytes=18675；sha256=2e392658acb8359cef327ec3f623773f4f2db5f8c855789c6d30d2776faabd9d

````text
# 候选公共启动与宿主身份准入：开发交付记录

状态：生产代码与开发测试已冻结，构建、当前改动的installed/admission测试及三组相关race通过；六个相关包原环境全量通过，lifecycle在清除检测信号后的全量通过。原环境整条七包命令退出1，保留并明确记录下面的既有环境夹具失败。只交付当前块，不声明后续 Controller、真实执行桥或实机 canary 完成。

## 固定原生基线和输入

- 开发固定基线：`6ba24cf502f9849b28bc14abb61bac8c2010076e`；开始时工作树干净。开发者未 commit/push。主代理在开发者停止修改后固定审查对象为 `137b0ba3a682cf1f9af1172ff4ebd3b708999e1e`，5条交付内容与冻结SHA不变。
- 完整派发：`/tmp/installed-public-start-execution-brief.md`；已全文读 AGENTS.md、development-worker.md、start-readiness.md、reviewer-base.md及对应需求/源码引用。未读取其他代理结果或旧审查结论。
- 初始主代理原证据在 `/tmp/installed-public-start-original-evidence/`，本开发者未写该目录。原探针每次写 original-observation.json；开发者基线/修后各另存观察，绝不混为初始证据。

## 先引现归属，再选方案

`prompts/actions/start-readiness.md` 原文：
> 方案层面的判据是：是否有更简单的既有归属者或原生能力可用，以及它是否是足够且最简的做法。

`internal/engine/facade/facade.go` 原文：
> if req.Route == "regular" && strings.TrimSpace(req.BaseSnapshot) == "" { return nil, Run{}, fmt.Errorf("engine start: regular route requires an immutable native base snapshot") }

同一现 Start 归属已经调用 `vcsProvider.Resolve()`、`Verify(req.BaseSnapshot)`、`IsAncestorOrEqual(req.BaseSnapshot, current)`，并在首写前核完整 intake/content。`internal/host/host.go` 的 `Lookup` 原文：
> normalized := strings.ToLower(strings.TrimSpace(value))
> if descriptor.ID == normalized { return descriptor, nil }
> if strings.ToLower(strings.TrimSpace(alias)) == normalized { return descriptor, nil }

`internal/engine/protocol/engine.go` 原文：
> state.RunProvider = provider
> state.BaseSnapshot = baseSnapshot
> state.IntakeConfirmationReceipt = confirmation

业务不变量：regular 只能以可复现且祖先或相等的真实原生基线创建；lightweight 无原生依赖/基线语义；宿主来自本次精确已注册安装 record；request、protocol 及后续统一重开保持同一 package/record/fence/intake/native/host 身份；不支持输入及错 fence 在 workflow 状态写前拒绝；不新增公开手工 child 路径。

唯一事实 owner：RegistryRecord.Host；host.Lookup 只做既有规范化。EngineAdmission 负责精确安装准入；StartRequest、protocol.State 是既有携带者；VCS 校验归现 facade/VCS；Open/Drive/Submit/版本/root/fence归既有统一接纳路径。

显式跨接缝：`RegistryRecord.Host → host.Lookup(...).ID → EngineAdmission.Provider → StartRequest.Provider → InitWithMetadata → State.RunProvider`。其他携带者为既有 admission record/package/generation/lease/token、StartRequest.BaseSnapshot与IntakeConfirmationReceipt、持久 Envelope。协议加载直接反序列化原 State，不按环境或新 launcher sibling猜宿主；DispatchClaimBridge传明确provider，cost resolver精确比对ref.Provider。已有能力足够，未改这些后续 owner。

受影响入口：唯一 `workflow start --intake-receipt` 分支和它共用的 ResolveEngineAdmission/ByRecord。show/status/next/drive/submit继续走openEngineIfPresent；diagnose与guard-preview保留各自原只读归属；普通start/default/full/custom、child/receipt/native终态不新增旁路。

## 实际改动与必要性

1. EngineAdmission添加唯一必要的内存transport字段Provider，沿原准入解析在完成既有目标/绑定/bootstrap校验后调用host.Lookup。现EngineAdmission没有宿主携带字段；该字段让CLI无需重新读registry或检测环境。不新增持久控制字段、状态、数据库或映射。
2. 候选start规范化并接受空/lightweight/regular；regular传现--base-snapshot，lightweight保持空基线；沿同一facade.Start输送admission.Provider。空candidate route仍由原Start默认lightweight，普通start路线/后续default/full/custom不混用。
3. 更新同一route帮助文字；既有in-process admitted CLI fixture改为显式运输admission.Provider。该既有fixture自行准备bootstrap，不充作实际installed证明；真实installed证据来自树外原探针和新开发测试。
4. 自写三个业务测试及现准入owner别名/精确record断言。复用已有binary构建和完整PackageReceipt拷贝helpers；测试都在test层，无生产test/fault/resolver开关。

被替代路径已删除：lightweight-only候选route条件及拒绝消息、硬编码Provider:"engine"、忽略regular base的旧candidate赋值已替换在原分支；未保留第二启动机制、fallback、状态账本或标记。无schema/definition变动，无DB迁移；沿已批准§5.9 state.json例外，不能声称通用“控制状态存DB”条款已由本块满足。

## 原始执行记录

所有go test都显式-count=1，不使用测试结果缓存；Go构建依赖缓存正常使用，没有清缓存或将缓存结果冒称新执行。所有真实installed测试hooks为skip-hooks，不属于实机model/canary。付费/真实模型调用数0，费用0。

| 执行 | 退出码 | 实际耗时 | 原始日志/观察 |
| --- | --- | --- | --- |
| 固定基线原样树外probe | 1 | package5.162s/test3.11s | /tmp/installed-public-start-developer-original.log；/tmp/installed-public-start-developer-baseline-observation.json |
| 首次生产补传后原样probe | 0 | package5.607s/test4.20s | /tmp/installed-public-start-developer-repaired-probe.log；/tmp/installed-public-start-developer-repaired-observation.json |
| 开发installed测试首编译 | 1 | 编译失败，未执行测试 | /tmp/installed-public-start-acceptance-compilation.log |
| 开发installed首次行为执行 | 1 | package13.210s | /tmp/installed-public-start-acceptance-boundary-observation.log |
| 核契约后3项installed自测 | 0 | package13.108s | /tmp/installed-public-start-acceptance.log |
| 新增lightweight带base及真实重装fence边界 | 0 | package12.091s | /tmp/installed-public-start-added-boundaries.log |
| 5host全部11aliases/精确record owner自测 | 0 | package2.894s | /tmp/installed-public-start-admission.log |
| go build ./... | 0 | real1.68s | /tmp/installed-public-start-build.log |
| admission相关race | 0 | real12.37s/package5.773s | /tmp/installed-public-start-admission-race.log |
| facade child/recovery/cost相关race | 0 | real7.57s/package5.941s | /tmp/installed-public-start-facade-race.log |
| CLI普通路线/统一重开/lifecycle相关race | 0 | real17.91s/package13.594s | /tmp/installed-public-start-cli-race.log |
| 最终冻结代码原样树外probe | 0 | real6.13s/package5.129s/test3.83s | /tmp/installed-public-start-developer-final-probe.log；/tmp/installed-public-start-developer-final-observation.json |
| 原环境7个相关包全量 | 1 | real453.64s；CLI105.192s、validate426.533s、facade450.012s、acceptance32.107s、vcs4.919s、host1.145s通过；lifecycle2.219s一项失败 | /tmp/installed-public-start-packages.log |
| lifecycle清除检测信号的全量 | 0 | real2.99s/package2.441s | /tmp/installed-public-start-lifecycle-isolated.log |
| lifecycle原环境单项复现 | 1 | real1.53s/package1.147s | /tmp/installed-public-start-lifecycle-inherited.log |

首次开发测试编译误用IntakeReceipt字段（工具返回wall0.179s，未执行测试）；仅改为现有IntakeDigest，未改变业务期望。首次行为期望误把facade.Drive完成签发后的当前WAIT/TASKS_IN_FLIGHT当成READY，也误只接受并发重入already-exists而遗漏现coordination快速LOCK_HELD；核原Drive/协调owner后，断言实际边界PendingActions、WaitReason、IntakeDigest及不变身份，保留一个成功/另一拒绝的并发业务要求。生产没有为这些测试新增逻辑。两次修正后验证已通过，未继续失败循环。

相关包全量使用go test非-v；其日志不逐个显示内部skip。已知darwin环境Windows-only TestNormalizeHostPath不适用；不能将该平台分支写为已通过。其他条件skip计数未由此日志记录。当前执行环境Go1.26.4 darwin/arm64；未验证其他平台。

## 开发测试的业务依据与实际边界

- 实际完整immutable安装/bootstrap：真实新构建binary＋PackageReceipt内容，独立HOME和Git项目；request里精确registry record/fence/package/provider/native/intake与protocol内容一一比对，持久Envelope由store.Load实际验证，receipt规范digest重算。
- public regular start→show/status/next/diagnose/guard-preview只读字节对照→Drive到已签发worker边界→Submit错host拒绝字节不变→Submit同hosttyped spawn观察→protocol实际receipt身份核对。此处提交的是测试协议观察，没有执行worker、fake transcript或伪造lifecycle start/stop使用来源。
- 同launcher实际ZCode global与Codex project安装；各run仍按自身record重开/Drive，宿主不串台。真实重装换代后old-fence的show/drive/submit拒绝，state字节不变。
- 未安装、缺base、不存在base、旧intake+当前solution、候选default/full/custom及原不支持child/split/rerun参数拒绝；engine/legacy state目录均未创建，native HEAD不变。
- 无.git时空route与显式lightweight＋不合法base字符串仍成功，request.BaseSnapshot恒空，Drive为unverified Complete。
- regular正常启动与request-only+intent合法中断prefix重建的完整request/protocol对照；启动并发一成一拒，重复已成立run不变；原owner共路径，无新恢复实现。
- 五installable host全部aliases（含空白/大小写）经同admission规范化，root/project与ByRecord得到相同身份；其registry材料是owner测试夹具，不冒充真实installed/canary。
- child regular继承、receipt/master接纳、普通路线、native基线/终态、owner/cost回归由相关全量包与相关race结果承担；facade450.012s全量通过。调用轮次/任务代次/重试/业务返修计数无生产修改。

## 工程准则逐项自核

| 条目 | 当前代码/执行依据 |
| --- | --- |
| 正确性 | 最终原样installed负控转正；强断言request/state/intake/Envelope/host/base/fence内容。 |
| 需求完整性 | 本块所有明确公开输入、宿主运输、重开、恢复、拒绝与轻量保留由上述测试/现owner覆盖；后续阶段明确不作本块完成声明。 |
| 最简/简洁 | 仅2个生产文件，复用host.Lookup、facade.Start与VCS；无新公开面/检测表/状态/runner。 |
| 耦合 | host规范化留在validate维护准入边界；engine没有host import；后续命令统一openEngineIfPresent未变。 |
| 可维护性 | canonical provider只由一个归属生成；by-record和start返回同一个值，不在每个命令复制逻辑。 |
| 可读性 | Provider字段注释说明当前来源；候选route/base变量显示两种run语义；帮助文字区分候选默认与普通受理。 |
| 优雅/设计一致 | typed既有StartRequest/协议binding贯通；版本/admission/root/commit/fence归属保持；无平行恢复。 |
| 无不当硬编码 | canonical provider从host registry派生；未添加Host→Provider表或按某个反例/路径猜身份。 |
| 无窄逻辑/过拟合 | 全route输入/五host别名/不同实际scope及同record重开覆盖；树外探针原样未降低断言，自写业务测试。 |
| 无过度设计/防御/抽象 | 唯一新增生产概念是必要transport字段；未为手改metadata、攻击、权限、不支持平台加分支/状态。 |
| 测试业务隔离 | 新fixtures/helpers全部_test.go；实际安装证据与inprocess协议夹具区分；无生产test/fault开关、无模型费。 |
| 注释/命名 | 新注释只说明当前约束，文件/测试名按启动/绑定/恢复行为命名；无开发序号/来源署名/历史演进注释。 |
| 被替代机制删除 | 原候选light-only检查及engine provider赋值同处分支替换；无旧/新启动并存及遗留标记。 |

## 冻结交付

完整原生baseline→current diff：`/tmp/installed-public-start-delivery.diff`。
SHA256：`3d9ce5dc2d04cbc30703f8b05531929370fc52344e43be8c3b809d2c95a95ccf`。
`git diff <baseline> --check`通过。所有5条交付路径均由git ls-files --error-unmatch确认tracked；新测试路径先touch+逐路径git add，再编辑，没有add整个工作区。开发者未提交；主代理冻结commit后工作树干净。

逐文件SHA256：
- `internal/cli/cli.go`：`442e11043e5806a48fa723de5d67bb3107ea509120ef0abb9a0af52112f97832`
- `internal/cli/engine_writer_exclusivity_test.go`：`65c89f72e2217a48205a591406c0e1d2e4c2dcd9990fb898f796c874f02a4a74`
- `internal/engine/acceptance/installed_start_test.go`：`c5b9a8bea9f3de6908184bdf7e4d1217605042589b6fbc9ccb14934e2fff645a`
- `internal/validate/engine_admission.go`：`ea598c14a1e75b0332e4124e93e1f858863a8cbfc222cc7ee92076320cff22d5`
- `internal/validate/engine_admission_test.go`：`a7b099b98acab88d230865ad988b12406f8f0786a6a99883cae337e7798bc456`

## 实际残留与未验证

- 本块没有真实模型执行/真实lifecycle/hooks/transcript计量或installed ZCode full canary；skip-hooks明确，缺源不制造费用数据。
- 后续Controller、工作区/集成/晋升公共执行、生命周期完整链和旧公开面删除均留后续合同；本块不提供占位或新公共手工child。
- 旧已安装run不迁移，不承诺跨包兼容；真实重装fence原规则保持。
- 原环境七包合并命令退出1，不能写成全量绿；它的唯一失败来自未修改的lifecycle环境夹具，清信号后的该包全量通过，详见下节。主代理独立复核与fresh门审不由此开发子任务作结论。

## 既有生命周期环境夹具失败的独立取证

原环境全量唯一失败是 `TestProviderFromEnvironmentRecognizesZCode`（lifecycle_test.go:437）：设置ZCODE_PLUGIN_ROOT后得到codex。该测试只清AI_AGENT和3个ZCode变量；当前Codex app进程CODEX_CLI_PATH非空（仅记录是否存在，未输出实际值），原Codex adapter登记此变量，host有限registry中的Codex先于ZCode。重跑同一项在原环境独立稳定失败，排除其他测试相互影响。

原生 `git diff <baseline> -- internal/host internal/lifecycle` 为空。`go list -deps -test ./internal/lifecycle` 中本仓库依赖闭包仅host与lifecycle（含该包测试），本次全部未修改。因此这条失败的被测生产/测试代码与固定基线值级相同，非本块新增因果。主代理仍须独立复现才正式接纳此归类。

清除测试子进程host检测变量后的全量lifecycle测试退出0、无skip（-v日志）；没有修改测试期望、fixture或生产检测优先序：

```sh
env -u AI_AGENT -u CLAUDE_CODE_ENTRYPOINT -u CODEX_HOME -u CODEX_CLI_PATH -u CURSOR_TRACE_ID -u CURSOR_RUNTIME -u DSH_HOME -u DSH_PROJECT_DIR -u ZCODE_PLUGIN_ROOT -u ZCODE_PLUGIN_ID -u ZCODE_PLUGIN_NAME go test -count=1 -v ./internal/lifecycle
```

其变量集合来自现ProviderEnvironmentKeys及五个adapter.environmentKeys。仅测试进程取消环境变量，用户宿主/配置未改。本块范围不修这个既有跨宿主环境fixture，不把非本次引入问题掩作生产修复。

## 完整验证命令

```sh
go test -count=1 -overlay=/tmp/installed-regular-start-overlay.json ./internal/engine/acceptance -run '^TestInstalledPublicStartAdmitsRegularNativeRun$' -v
go test -count=1 ./internal/engine/acceptance -run '^TestInstalled(RegularStartCarriesHostThroughPublicOperations|StartRejectsInvalidInputsBeforeState|LightweightAndRegularStartRecovery)$' -v
go test -count=1 ./internal/engine/acceptance -run '^TestInstalled(RegularStartCarriesHostThroughPublicOperations|LightweightAndRegularStartRecovery)$' -v
go test -count=1 ./internal/validate -run '^Test(ResolveEngineAdmissionChoosesProjectBinding|EngineAdmissionNormalizesRegisteredHostAliases)$' -v
go build ./...
go test -count=1 ./internal/cli ./internal/validate ./internal/engine/facade ./internal/engine/acceptance ./internal/engine/vcs ./internal/host ./internal/lifecycle
go test -race -count=1 ./internal/validate -run '^Test(ResolveEngineAdmissionChoosesProjectBinding|EngineAdmissionNormalizesRegisteredHostAliases)$'
go test -race -count=1 ./internal/engine/facade -run '^Test(StartRecoversIncompleteStartIntent|CompletedRunEvidenceSurvivesSameIDStartRetry|SliceChildParallelClaimsAreMutuallyExclusive|SliceChildStartRebuildsAfterRequestOnlyPersistence|SliceChildInheritsAndSelectsRoute|CostSourceBridgeReportsMissingBindingUnavailable|CostSourceBridgeHoldsObservableHostPending)$'
go test -race -count=1 ./internal/cli -run '^Test(CLIWorkflowDefaultRouteAndHelp|LegacyWriteCommandsRejectEngineRunPerCommand|AlternatingLegacyAndEngineEntriesKeepBindingLocked|CLIInstalledHostsResolveLifecycleEventsToActiveWorkflowRoot)$'
go test -count=1 ./internal/lifecycle -run '^TestProviderFromEnvironmentRecognizesZCode$' -v
```

所有长测试完成，源码/开发测试保持冻结，不开始下一块。

原日志文件UTC完成时间（文件mtime）：
- `/tmp/installed-public-start-developer-original.log`：2026-10-03T13:26:32.610403+00:00
- `/tmp/installed-public-start-admission.log`：2026-10-03T13:33:40.548132+00:00
- `/tmp/installed-public-start-acceptance-compilation.log`：2026-10-03T13:34:02.935053+00:00
- `/tmp/installed-public-start-acceptance-boundary-observation.log`：2026-10-03T13:35:39.330142+00:00
- `/tmp/installed-public-start-acceptance.log`：2026-10-03T13:35:53.262810+00:00
- `/tmp/installed-public-start-added-boundaries.log`：2026-10-03T13:38:04.133343+00:00
- `/tmp/installed-public-start-build.log`：2026-10-03T13:39:28.371023+00:00
- `/tmp/installed-public-start-packages.log`：2026-10-03T13:47:00.343426+00:00
- `/tmp/installed-public-start-admission-race.log`：2026-10-03T13:39:39.059748+00:00
- `/tmp/installed-public-start-facade-race.log`：2026-10-03T13:39:34.263664+00:00
- `/tmp/installed-public-start-cli-race.log`：2026-10-03T13:41:03.332099+00:00
- `/tmp/installed-public-start-developer-final-probe.log`：2026-10-03T13:42:30.868333+00:00
- `/tmp/installed-public-start-lifecycle-isolated.log`：2026-10-03T13:50:43.080838+00:00
- `/tmp/installed-public-start-lifecycle-inherited.log`：2026-10-03T13:51:17.870534+00:00

````

## installed-start-complexity-137b0ba.md

来源：/tmp/installed-start-complexity-137b0ba.md；bytes=20888；sha256=2391fe8dcff8dea82616ebef41226c5eae17d575818d58e42dcb1b6181707d4b

````text
# 候选结论：PASS

固定原生 Git 比较：`6ba24cf502f9849b28bc14abb61bac8c2010076e` → `137b0ba3a682cf1f9af1172ff4ebd3b708999e1e`。本报告只是手动派发复杂度门的候选输入，不是正式关口接纳、阶段完成或 goal 完成声明。

范围内 P0/P1：0；有效 P2：0；本次没有需要列出的 P3 简化建议。没有发现由本次变更引入、可经正常公开操作或常见失误复现的范围内缺陷。按固定 `prompts/reviewer-base.md`，候选返回 PASS。

## 输入与隔离

首先读取固定提交的 `prompts/reviewer-base.md`，检查任务块完整性；任务使用允许的块类型，未提供修复历史或预期判定。随后完整读取同提交 `AGENTS.md`、`gates/complexity-gate.md`、`prompts/actions/start-readiness.md` 和 `/tmp/installed-public-start-gate-requirements.md`。采用当前需求对普通/child 路线、state.json 与 RESET 的明确裁定。没有启动正式流程或另一技术审。

源码通过 `git archive 137b0ba3a682cf1f9af1172ff4ebd3b708999e1e` 提取到 `/tmp/installed-start-complexity-137b0ba-independent/src`。完整 diff 保存于该目录上层 `logs/full.diff`，五条路径全部审查。源码取值未使用共享工作树。没有读取聊天历史、历史关口/交接/开发报告或其他审查者探针/结果，也未读取独立 QA 正文和执行证据。只在自己的 `/tmp` 区域构建、安装和保存验证材料。

实际模型调用 0，调用费用 0；无 commit/push；无宿主凭据读取。`stop-that-shit` 技能仅用于本次只读和范围边界。

## 需求原文与归属依据

固定 `refactor-plan/final-implementation-draft.md:27`：`workflow start` 是“唯一 bootstrap 写入口”；`drive`、`submit` 与只读查询的现有归属保持一致。同文 `:232` 要求候选 start 消费 typed intake，而不是生成回执或新增 runtime/identity/digest 选择面。

固定 `internal/validate/engine_admission.go:13`：EngineAdmission 是安装 registry 所选不可变运行身份，候选只能接受 launcher 派生的值。固定 `internal/host/host.go:180` 已持有宿主别名规范化。固定 `internal/lifecycle/lifecycle.go:208`：provider 来自 claim context，不能从 launcher 路径和工作目录猜测。

固定 `refactor-plan/incremental-seal-plan.md:53` 要求实际当前 installed binary 与安装路径证明。固定 `openspec/changes/orchestration-pipeline-engine/master-requirements.md:61` 维持 state.json 唯一权威投影。本块依照确认需求保持该例外，不迁移数据库。

方案判据引用固定 `prompts/actions/start-readiness.md`：必须判断“是否有更简单的既有归属者或原生能力可用，以及它是否是足够且最简的做法”；不能以内部自洽证明新增系统必要性。本审查未索要额外技术审或开发报告。

## 快速阻塞扫描

当前 installed/public regular 路径实际可执行：当前 binary 在隔离宿主 home 下成为 stable launcher，实际 `install` 和 `install --bootstrap` 后，由该 installed binary 调用 `workflow start --intake-receipt --route regular --base-snapshot <native Git commit>`。真实 native Git、registry、bootstrap、request、state 和 lifecycle claim 内容均有执行证据。

3 个交付 installed 测试实际运行通过，覆盖 regular、未安装/缺失或不存在 base/禁用 flags/旧 intake 的拒绝、lightweight 无 Git、request-only 恢复、同 ID 并发与重复启动。4 个相关旧入口隔离测试实际运行通过。没有不可达核心入口或把编译成功冒充行为通过的情况。

独立公共探针额外使用带空白的大写 `REGULAR` 和大写 native Git identity，并在环境声明 `AI_AGENT=cursor`、`CODEX_CLI_PATH=/unselected/codex` 时实际安装 zcode。持久结果仍是 registry 所选 zcode；本探针没有真实模型调用，人工 typed intake 仅是正常可达前置，不是用户真实受理证据。

## 完整变更与复杂度门逐项结论

完整 diff 为 5 文件、481 行增加、10 行删除，其中 407 行为直接相关 installed 开发者测试。生产改动仅在 `internal/cli/cli.go` 和 `internal/validate/engine_admission.go`。

| 新增或改变的概念 | 逐项判断与当前证据 |
| --- | --- |
| CLI `candidateRoute` | 对现有 `--route` 值作一次 trim/lower，允许空/lightweight/regular；未增加路线系统。空值交给现有 facade 默认 lightweight，后续 default/full/custom 完全独立。`cli.go:448`。需求内；复用现有 flag 和 `strings`；无新增依赖；未找到保持全部行为更短且更清楚的替代。 |
| CLI `candidateBase` | 仅 regular 携带已有 `--base-snapshot`；空/lightweight 不携带该值，因此不获得 VCS 依赖。`cli.go:468`。需求内；事实校验完全交给现有 facade/VCS；变量只是局部选择，不是新增控制状态。无具体等价简化发现。 |
| `EngineAdmission.Provider` | 已选 registry record 的规范宿主必须经过准入携带者抵达已存在的 `StartRequest.Provider`。`engine_admission.go:21,166`。现有结构本来没有 Host/Provider 输出，加入这一派生字段足够；没有新公开参数、宿主检测层或第二事实源。 |
| `host.Lookup(record.Host)` 与错误包装 | 完全复用现有 `host.Lookup`，不复制 alias 表、环境探测或 CostProvider。错误沿现有 `UNREGISTERED_INSTALL` 拒绝面。`engine_admission.go:166`、`host.go:180`。一次有限表查找，使用现有依赖。 |
| route 帮助文案 | 必须说明候选默认 lightweight 和 ordinary 空 route 的 regular 语义；不引入新产品流程。`cli.go:436`。 |
| `installedStartProject` 及其方法 | 测试局部组合 current binary、隔离 home/project、真实 Git、typed intake 和公共进程调用；复用已有 `repoRoot`、`buildInstalledBinaryAt`、`isolatedInstallSource` 和 `executablePath`。`installed_start_test.go:26,31`。现有 protocol harness 并非本块实际 public Start/Submit 的证明，故该 fixture 有直接验收职责。 |
| JSON/file 辅助与 bindings 断言 | 使用标准库表达 typed 输入、原始工件读取和内容比较；`bindings` 不只断言存在，而核精确 registry identity、generation/lease/token、provider/base/route/intake digest。`installed_start_test.go:143,180`。未新增 production 测试开关或旁路。 |
| resolver alias 测试和旧写入口夹具调整 | 别名从 `host.All()` 枚举，不新造第二清单；分别核普通与精确 record resolver。旧 writer fixture 将实际准入 provider 传给既有请求。`engine_admission_test.go:88`、`engine_writer_exclusivity_test.go:189`。 |

复杂度门五步：①全部新增内容在确认范围；②已有 facade/native/host/安装/恢复归属被复用；③字符串、JSON、文件、进程、哈希、比较均使用标准库或 native Git；④无新增依赖；⑤没有发现可证明在行为、副作用、错误处理和必要验证上等价的更短实现。没有新增控制阶段、控制文件、包装系统、配置或证据层。测试体积与本块列出的公开进程、安装换代、恢复/并发边界相称，不构成仅凭行数否决。

允许入口/交付物/非目标核验：完整五路径均落在公共 start、EngineAdmission/resolver 和直接相关开发者测试；没有修改 Controller、workspace、merge、完整 bridge 或大阶段实跑，也没有把这些未来能力声明为交付。

## 同根因入口、接缝和事实链

1. 首次 public Start：`RegistryRecord.Host` → 两个公开 resolver 共用 `resolveEngineAdmission` → `host.Lookup` → `EngineAdmission.Provider` → CLI `StartRequest.Provider`。选 record、安装真实性和 bootstrap 校验仍在既有 resolver；宿主没有从环境、包名、launcher 或 CostProvider 推断。
2. 请求/状态：facade `Start` 在已有 run/admission 锁下核准入代次，既有 regular 分支完成 Resolve/Verify/ancestor；`:645` 保存完整 request，`:679` 将 provider/base/route/intake 交给 `InitWithMetadata`，protocol `engine.go:152` 保存 `RunProvider`。没有新增保存路径。
3. 公共重开：`show/status/next/drive/submit` 经 `cli.go:2212` 的公共 owner 读取 request 的精确 installed record，核 package/generation/lease/token，再经 facade `Open` 加载既有状态；没有把当前 root 下另一宿主写回已有 run。`diagnose` 仍是既有原始只读路径，`guard-preview` 仍是独立只读计算，不变成 writer。
4. 签发与正常接纳：protocol `engine.go:724,2238` 以 `state.RunProvider` 调用同一 `issueInto`，责任进入 AttemptBindings。spawn/result/HostAction/lifecycle 仍由现有 `checkProvider` 严格比对；public wrong-host receipt 实际拒绝且 state 字节不变。
5. claim/cost 与恢复：正常 SPAWNED 的 `engine.go:1381` 和 UNKNOWN 唯一匹配恢复 `recovery.go:350` 都使用现有 ClaimDispatch 回调和 `state.RunProvider`；facade `cost_source.go:52` 通往 `lifecycle.BindDispatchWithProvider`。成本来源在 `cost_source.go:23` 核记录 provider 与 ref 一致，不以缺 bridge/计量支持退回 engine/default。
6. request-only 启动：既有 `recoverStartIntent` 只移除没有 active/terminal 事实的 durable prefix，之后走相同 Start owner；交付 installed 测试在正常可达 request-before-state 前置下验证重建 request/state 值等价、intent 删除。没有第二启动或恢复实现。
7. ordinary/lightweight/child：公共 start 的非 intake 分支没有改动；lightweight 两种 route 均无 Git；公共手动 child flags 继续拒绝。既有内部 child request/provider/admission 接口未改，普通 default/custom 与 child QA-free 所选门执行及 request-only child 重建的相关测试通过。未声称未来自动 child 创建或 merge 全流程已完成。

## AGENTS 工程交付条件逐项核验

下表的“符合”仅指本次改动及列出的相关链，未将未执行的全仓能力笼统判为通过。

| 条件 | 结论、证据和核验范围 |
| --- | --- |
| 正确性 | 符合。`cli.go:448-475` 正确接入现有 regular/base/provider；真实 installed 测试和独立 public probe 均执行通过。 |
| 质量/根因闭合 | 符合本块链。Host 来源在 resolver 单点统一，两 resolver、Start、state、issue、receipt、claim/recovery/cost 全链追踪；未通过一个个入口补分支。 |
| 需求完整性 | 本块 1–6 项有源码和执行证据：regular/base、宿主、持久/重开、拒绝/锁/换代、允许改动面、相关正常/重复/并发/恢复与回归。完整 live native bridge 等明确非目标未验证。 |
| 最简 | 符合。每个新增概念上述五步独立核验，已有 owner 被复用，没有具体值级等价且更短替代发现。 |
| 耦合 | 符合本次改动。validate 仅引入现有 host；CLI 仍组合 admission 与 facade，不把 VCS/协议/生命周期逻辑移入 CLI。 |
| 可维护 | 符合。alias 单源仍在 host，regular validation 单源仍在 facade/VCS，开/写/恢复 owners 未扩散。 |
| 高可读 | 符合。两个局部选择与一项准入派生字段直接表达需求；错误语义沿原 `UNREGISTERED_INSTALL`，测试按实际行为组织。 |
| 优雅/设计一致 | 符合此门可客观核验部分。使用既有具体数据携带者和 API，无一般化框架；不把个人写法偏好作为缺陷。 |
| 不当硬编码 | 未发现。本次去掉生产启动的常量 `Provider: "engine"`，改用事实；固定 route 字面值来自产品定义。测试列出的 hosts/routes/负向 flags 对应确认边界。 |
| 窄逻辑 | 未发现。全 resolver 统一返回规范 provider，regular 的原生校验由通用既有实现完成；没有只为某 run/host 特判。 |
| 过拟合 | 未发现。枚举 host registry 全部 11 个 installable alias 子例；public global zcode 与 project codex 证明独立记录；另有 native ancestor/divergent 原 owner 回归。 |
| 过度设计 | 未发现。没有新增状态机、宿主检测、恢复通道、数据库迁移或公开 provider/runtime/digest 面。 |
| 过度防御 | 未发现。未知 host 的现有 Lookup 错误包装为正常准入错误；未增加 request/state/registry 人工改写防御。 |
| 过度抽象 | 未发现。新增载体字段、局部值与测试 fixture；其职责逐个绑定确认需求。 |
| 测试与业务隔离 | 符合。407 行安装测试仅 `_test.go`；production 无 test-only 逻辑，installed 证明使用实际 build/install/CLI/Git。 |
| 一个事实一个权威 | 符合本次变更。Host 为 selected registry record 的事实，alias 由 host owner 派生；request 是启动载荷，RunProvider 是 state 投影；保留确认的 state.json 例外。 |
| 正常/恢复同业务与接纳 | 符合本次接线。Start request-only 重建用同一 Start；Open 后用同一 engine；正常与 UNKNOWN claim 共用 ClaimDispatch。未对全部崩溃窗口做全仓穷举。 |
| 显式跨接缝携带者 | 符合。EngineAdmission.Provider → StartRequest.Provider/request.json → State.RunProvider；安装身份为 recordID/package/generation/lease/token → Admission/StartRequest/envelope；任务责任、claim provider、cost ref 均显式传递。 |
| 四种轮次身份 | 本次未修改/混用。已有 review.Series（`review.go:124`）、runtime TaskKey、Attempt ID 与 Attempts/MaxAttempts（`protocol.go:174`）、validation waves（`validation.go:107,586`）各有归属；本次只有准入 generation 透传，不拿它计算审查/重试/返修。未穷举所有轮次业务。 |
| 移植值级语义与地道表达 | 符合本块。没有新移植；复用现有 native Resolve/Verify/ancestor 和 host Lookup；public route 规范化后由原 owner 保存原协议值。 |
| 被替代路径删除 | 符合。本次 public 启动固定 lightweight、固定 engine provider 和只允许 lightweight 的旧分支被直接替换，未留下平行启动通道。 |
| 注释当前性 | 符合新增内容。Provider 注释只描述当前归属；installed 测试注释说明当前真实 binary/协议观测边界与恢复前置。没有新增开发历史叙述。未对无关既有注释提出修复。 |
| 命名与测试组织 | 符合。新增 file/function/type 描述 installed start、provider、recovery、invalid inputs 等行为，没有评审轮次/修复编号/开发批次痕迹。 |
| 诚实夹具 | 符合本块。actual binary 在 actual launcher target 运行，真实 install/bootstrap/registry/Git；spawning 仅提交声明的 protocol observation，不冒充 host model 执行。alias registry 构造明确仅 unit 前置。 |
| 测试期望来自需求 | 符合。检查规范 host、immutable native base、lightweight 无 VCS、旧 fence 与拒绝无 state 副作用、原 route/child 裁定；未降低既有断言。 |
| 原始证据与持久内容 | 符合。保存原始 stdout/stderr/exit/time；交付 `bindings` 核 request/state/bootstrap 的内容；独立 public 工件永久保留并核 registry/request/state/claim 具体值。 |
| 正常/重复/并行/恢复边 | 已执行：public 正常与进程重开、wrong-host reject、并发同 ID 一成功、重复同 ID 不改 state、request-only 正常值等价、安装换代旧读写拒绝；child/普通回归为现有局部测试，非未来全流程宣称。 |
| 性能/资源 | 源码核验符合：仅增加一次有限 host 表 Lookup、两个局部字符串值，不增加轮询/后台资源/网络/磁盘写路径。未做性能基准，不声明量化性能改善。测试 TempDir 与进程生命周期由原 helper/测试管理。 |
| 实施派发/返修记账 | 开发实施者与主代理历史派发是否符合流程未验证，按隔离契约未读开发/交接报告。当前审查没有修改共享源码/测试/状态；报告保存所有失败、成功及未验证事项。 |
| 先复现再修/主代理独立复核 | 本审查未形成缺陷，未实施修复。此报告是候选；正式主代理复核和接纳未由本审查代称。 |

## 实际执行与原始日志

根目录：`/tmp/installed-start-complexity-137b0ba-independent`。下列每项完整 stdout/stderr 在 `logs/<名称>.stdout`、`.stderr`，精确 argv、cwd、退出码、用时在 `.json`。完整 50 个记录调用（含所有源码检索与失败路径猜测）列于 `logs/manifest.json`。计时合计 49.875 秒是各记录调用耗时之和，不是并行审查的墙钟总时长；提取另见 `logs/extract.json`（0.521 秒）。

| 名称/实际命令 | 结果/退出码/用时 |
| --- | --- |
| `installed-start`: `go test ./internal/engine/acceptance -run '^TestInstalled(RegularStartCarriesHostThroughPublicOperations|StartRejectsInvalidInputsBeforeState|LightweightAndRegularStartRecovery)$' -count=1 -v` | 3 个测试 PASS，0，19.583 秒。 |
| `writer-isolation`: `go test ./internal/cli -run '^Test(LegacyWritersCannotReachEngineRunState|LegacyWriteCommandsRejectEngineRunPerCommand|PublicCleanupCannotOperateOnActiveEngineRun|AlternatingLegacyAndEngineEntriesKeepBindingLocked)$' -count=1 -v` | 4 个测试 PASS，0，6.649 秒。 |
| `admission`: `go test ./internal/validate -run '^Test(ResolveEngineAdmissionChoosesProjectBinding|EngineAdmissionNormalizesRegisteredHostAliases)$' -count=1 -v` | 首次真实 FAIL，1，6.700 秒。完整 tar 源码未含 ignored 构建目录 bin，既有 artifact 验证拒绝，尚未进入 Provider 断言；不是以绿色覆盖该失败。 |
| `build-current-artifact`: `go build -o bin/formal-gates ./cmd/formal-gates` | PASS，0，0.740 秒；仅在自己隔离副本生成当前实际 binary。该项只作为构建证据。 |
| `admission-with-artifact`: 与首次相同准入命令 | 两项测试及全部 11 alias 子例 PASS，0，2.915 秒。仅重跑因缺构建产物失败的目标。 |
| `related-regressions`: `go test ./internal/engine/facade -run '^Test(OrdinaryGateRoutesCompleteWithoutQA|SliceChildInheritsAndSelectsRoute|SliceChildSelectedGatesExecuteToCompletion|SliceChildStartRebuildsAfterRequestOnlyPersistence|RegularStartAcceptsAncestorBaseAndRejectsDivergent|CostSourceBridgeReportsUnsupportedHostUnavailable|CostSourceBridgeHoldsObservableHostPending|CostSourceBridgeReportsMissingBindingUnavailable)$' -count=1 -v` | 8 个相关测试（含子例）PASS，0，8.546 秒。 |
| `independent-public`: `python3 /tmp/installed-start-complexity-137b0ba-independent/public_probe.py` | 审查者探针前置失败，1，0.341 秒：错误地假定 facade 常量全是字符串字面量，实际 authority/transport 引用 protocol 常量。保存 traceback；未进入 public CLI。 |
| `independent-public-executed`: 同上 | 审查者安装前置失败，1，0.826 秒：从 source binary 调用 install，维护 owner 正确拒绝非 stable launcher。完整 6 个 Git/安装调用保留在 `public-artifacts-executed/commands.json`。 |
| `independent-public-installed`: 同上，前置先把当前 binary 放到隔离 stable launcher 路径，全部安装维护从其调用 | PASS，0，3.070 秒；18 次真实 native Git/installed CLI 调用及内容断言，日志 `public-artifacts-installed/commands.json`；两个预期拒绝命令实际退出 1，不记为异常 PASS。 |

源码检索的非零调用如实保留：`file-map`/`ownership`/`rules-relevant`/`storage-event-wire` 为猜测不存在路径的退出 2，`read-native` 为猜测不存在文件的退出 1；之后通过实际文件枚举找到并读到 `internal/engine/vcs/git.go`、`internal/engine/protocol/{protocol.go,event.go,engine.go}`、`internal/lifecycle/journal.go` 与 openspec 的 master requirements。它们是检索错误，不是行为测试或编译失败。

没有编译失败，没有以空日志证明行为通过，没有任何被中断但记成成功的测试。未跑全仓测试；没有具体剩余风险需要重复上述成功检查。未执行真实模型、完整 native bridge、大阶段 canary、未来自动 child/workspace/merge/lifecycle 全链、所有平台或无关故障组合。

独立 public 工件内容结果：provider=`zcode`，route=`regular`，base=`bb78fda99a0d6f0d5e43d2bbdca565fe5fe5185a`，recordId=`zcode-global-8dab23a11a4c`。详见 `public-artifacts-installed/assertions.json`；实际 registry、bootstrap、request、state 与 lifecycle claim 均在该目录下的隔离 home/project 中保留。query、wrong-host 与 repeated-start 均核 state 原始字节未变，start/queries/drive/submit 后 native HEAD 未变。

## 返回边界

本次没有 findings，无需给不存在的问题虚构复现/延期影响或等价简化方案。所有候选判定以当前确认需求、固定 diff 与上述源码/执行为依据；PASS 不替代主代理后续独立核实和正式接纳。

````

## installed-start-quality-137b0ba.md

来源：/tmp/installed-start-quality-137b0ba.md；bytes=28031；sha256=c00376ff4883200357c1e50709126ea1611423beff3a9a12a61e7f18089a6333

````text
# 候选结果：PASS

审查者：独立手动派发的实现质量门审查者。此结果是候选输入，不代表阶段、正式门或 goal 完成。

范围内 P0：0；P1：0；P2：0。当前 diff 范围内未形成 P3 发现。没有用未实现的后续能力判失败，也没有把范围外加固要求带入验收。

## 输入与隔离

- 已先检查任务完整性：任务由允许的 `[Shared reviewer contract]`、`[Gate]`、`[Current requirement]`、`[Current change]`、`[Result contract]` 组成；未检测到开发结果或预期判定污染。
- 基线：`6ba24cf502f9849b28bc14abb61bac8c2010076e`；待审：`137b0ba3a682cf1f9af1172ff4ebd3b708999e1e`。
- reviewer-base、AGENTS、implementation-quality-gate 均从待审提交读取。当前需求完整读取 `/tmp/installed-public-start-gate-requirements.md`，副本位于证据目录 `requirements.md`。
- 原生完整比较：`git diff 6ba24cf502f9849b28bc14abb61bac8c2010076e 137b0ba3a682cf1f9af1172ff4ebd3b708999e1e`，保存于 `/tmp/installed-start-quality-137b0ba-evidence/complete.diff`。5 个变更路径与派发清单一致，481 insertions、10 deletions。
- 源码通过该提交 `git archive` 隔离提取到 `/tmp/installed-start-quality-137b0ba-evidence/source`；以下源码定位均指该固定副本，简称 **S**。仅在该副本补建 `bin/formal-gates`，未编辑受审源码或开发者测试。
- 未读取聊天历史、历史结果、交接材料、开发报告或其他审查者材料。未修改共享仓库、共享测试或正式状态，未 commit/push。检查采用 stop-that-shit 的只读边界。
- 真实模型/宿主调用：0；真实模型费用：0。执行期间 `GOPROXY=off`、`GOSUMDB=off`，未读取宿主凭据。

## 快速阻塞扫描

公共入口 `workflow start --intake-receipt ... --route regular --base-snapshot <native identity>` 有实际可执行路径。真实安装的 ZCode/Codex 开发者验收测试通过；独立 Claude alias/Codex 安装探针也从真实 launcher 执行成功。regular 到达已签发 worker 的外部边界；使用规范宿主的 spawn receipt 可由公共 submit 接纳，错误宿主稳定拒绝且 state 字节不变。没有发现核心公开入口缺失、核心判据不可观察或本次引入的用户可见回归。

独立探针输入 typed intake 是按真实工件哈希构造的正常可达前置；它不是实际用户受理确认或真实模型执行证据。

## 维度一：需求覆盖 — PASS

本维度只核实现路径存在，未以测试设计或测试绿色代替覆盖判断。

| 当前确认条目 | 对应当前实现路径 | 独立结论 |
|---|---|---|
| 1：唯一公共候选 start 支持明确 regular + immutable native base；空 route/显式 lightweight 无 VCS | S/internal/cli/cli.go:441、448、468、473 将 route/base 交给既有 Start；S/internal/engine/facade/facade.go:484 默认 lightweight，496 拒缺 base，503 调用 native Resolve/Verify/IsAncestorOrEqual；S/internal/engine/vcs/git.go:120、137、192 持有原生判据 | 路径完整；lightweight 的 base 在 CLI 置空，Git provider 只在 regular 构造 |
| 2：所选 registry record 拥有 Host，host.Lookup 规范化，既有携带者传 Provider，无新增公共身份参数 | S/internal/validate/engine_admission.go:144 在既有记录选择后，166 调 host.Lookup(record.Host)，170 返回 EngineAdmission.Provider；S/internal/cli/cli.go:473 传入既有 StartRequest.Provider；S/internal/engine/facade/facade.go:653 写 request，679 InitWithMetadata；S/internal/engine/protocol/engine.go:152 写 RunProvider | 路径完整；public start 未增加 provider/runtime/digest flags |
| 3：首次、读写重开、签发任务、lifecycle/cost、正常/request-only 恢复保持身份，足够能力不改 | S/internal/cli/cli.go:2212 共同重开入口先读 request、2237 精确按记录选准入、2240 核 digest/generation/lease/token，再 Open；S/internal/engine/facade/facade.go:762、828 重开；protocol/engine.go:724、913、1381 与 protocol/recovery.go:350 消费 RunProvider；facade/cost_source.go:21、51 复用显式 claim；facade/facade.go:456、1777 半启动恢复清理后重用 Start | 现有路径足够并仍是原归属；无按入口新增检测或第二套恢复实现 |
| 4：准入/版本/fence/intake/重复/读写隔离与错误仍由原归属拒绝 | CLI 原 flags 拒绝保留；admission 的 record/root/identity/bootstrap 屏障保留；facade.Start 的 admission snapshot、intake/artifact、run established 判据保留；共同 Open 的版本与安装屏障保留 | 对应代码未旁路；新 route 的错误由原 native/Start 校验负责 |
| 5：允许范围与真实安装证明 | 完整 diff 仅 cli、EngineAdmission/resolver 及直接测试；新 acceptance 通过当前源码构建 binary、完整包、真实 install/bootstrap/registry/Git 和 public CLI | 改动面符合边界；人工 typed intake 未当作真实受理 |
| 6：正常、同 ID 重复/并发、request-only、进程重开、安装/换代、lightweight/child/普通回归 | 原 Start/Open/coordination/provider consumer 路径保留；新开发者 tests 覆盖实际 public/install 边；相关 child/ordinary/cost 既有接口未改 | 所需实现路径存在；执行证据列在后文，未要求全 native bridge 或后续 Controller 实现 |

当前产品裁定逐项核对：

- run Route 与 review RouteMode 仍是不同字段。候选 start 只接受 lightweight/regular，后续 default/full/custom 仍由现有协议与 route owner 决定；未把 default 改名 custom。
- public 手动 child 仍在 CLI 的 split/master/retained-overall 检查拒绝；内部 child Start 接口仍存在，完整 diff 未改其 claim/topology/route 接纳。
- child 普通 QA 排除、所选门执行和 master merge-qa/merge-gate 固定仍由原协议归属；本次未改相关实现。
- state.json 例外沿冻结规格保留：S/refactor-plan/final-implementation-draft.md:165 与 S/openspec/changes/orchestration-pipeline-engine/master-requirements.md:61 明确唯一控制投影；未引入数据库迁移。
- 没有新增 reset/recovery 公共命令；恢复继续是 Start/Open/Drive 的性质。
- Controller 自动 child、workspaces/依赖/merge 验证、级联/guarantee、完整 installed native bridge、全部大阶段实跑和最终删除/发布仍非本块交付；既有 StartRequest、Admission、RunProvider、Route/RouteMode、child claim 和 store envelope 接口未被本 diff 删除或改型。

## 维度二：实现与需求偏差 — PASS

事实：新 Provider 值来自已经选中的 `record.Host`，而不是环境、包名、launcher 路径或 CostProvider；`host.Lookup` 是原有 finite registry 及别名规范化的唯一能力。CLI 仅将这个值转交现有 StartRequest。`Route` 为空仍由 Start 设为 lightweight；CLI 仅向 regular 传 base；regular 的语义来自既有 native Git owner，未重写接受祖先或 immutable identity 的规则。

持久化的身份链与业务链没有语义替换：Start 写完整 request 后 InitWithMetadata 写权威 state；共同 public Open 使用 request 中的精确安装 record，protocol 签发/接纳、正常 claim、unknown receipt claim 和 cost source 仍读 RunProvider。raw registry Host 可以是 `claude`；规范 RunProvider 是 `claude-code`，二者不是必须文本相等的事实。独立安装已实证这一语义。

既有引用核对：final-implementation-draft §2.1 的唯一 bootstrap、Drive/Submit、只读四入口；同文 typed intake 受理段落；incremental-seal-plan:53 实际安装 binary 要求；EngineAdmission 类型注释；lifecycle.go:207 显式 claim 的 provider 合同。当前变更与这些语义吻合。没有把已确认方案作为重新设计目标，也没有发现绕过已交付归属的偏差。

## 维度三：架构与耦合 — PASS

独立核对归属、依赖、公开面、状态生命周期、失败和性能形态。

| 接缝/事实 | 明确携带者与唯一当前归属 | 当前证据 |
|---|---|---|
| 新启动宿主事实 | selected RegistryRecord.Host → host.Lookup → EngineAdmission.Provider → StartRequest.Provider → State.RunProvider | admission.go:144、166、170；cli.go:473；facade.go:679；protocol/engine.go:152 |
| 已有 run 的宿主 | 已持久化 request/state；签发 Attempt.Bindings.Responsibility、provider-bearing event/receipt、CostSourceRef 与 lifecycle binding | protocol/engine.go:724、913、1306、1381、1417、1477、1608、2501；protocol/cost_source.go:59；facade/cost_source.go:21、51 |
| 安装身份/fence | registry 所选 record → 原 EngineAdmission 与 facade.Admission → request admission 字段与 store envelope；共同 Open 按 exact record 核对 | cli.go:475、2237、2240；facade.go:521、650、762；admission.go:148、163 |
| route/native base | StartRequest.Route/BaseSnapshot → State.Route/BaseSnapshot；regular 使用现有 vcs.Provider；Open 从 request.Route 恢复 VCS provider | facade.go:484、503、679、828、1411 |
| 半启动与终态 | run established/intent 仍归 facade；恢复只发现/清理持久前缀，再走 Start；terminal evidence 不被当成半启动 | facade.go:1758、1777；新 installed_start_test.go:354-373 的值级对照 |

新增的只是既有 EngineAdmission 上的派生值字段；没有新增独立控制状态、缓存、sidecar、检测器、registry 或恢复机制。host 依赖仍留在 validate 集成边界，engine 本体仍不导入 host registry。既有 request/state 的职责未交换，文件没有增加第二个可独立决策的 Host 权威。state.json 延用确认例外。

新增错误为 host.Lookup 失败时既有 `UNREGISTERED_INSTALL` 类别，发生在 public facade 首写之前；未改变可支持宿主的错误归属。新增 lookup 枚举既有有限 descriptor/alias 表，无无界工作、后台任务或资源生命周期。没有可观察的新架构性能退化证据。

child/ordinary 与未来消费者接口没有新增参数/删除字段/改型；Provider 正好沿原协议消费路径传递。相关 child 认领、并发/中断重建、gate 执行与 merge 固定绑定回归执行通过；这证明现有接口保留，不声明未来 Controller 已实现。

## 维度四：实现正确性与质量 — PASS

完整生产变更已逐行核对：CLI 的 candidateRoute 规范化和封闭枚举直接表达所需两条 bootstrap 路线；candidateBase 的单一条件仅表达 regular 才有 native base；新 Provider 从选中记录经现有 host.Lookup 得出并携带。没有测试专用生产分支、新容错旁路、额外兼容机制、死代码或遗留替代路径。

开发者交付测试质量：

- admission unit test 在行为归属的最低层核项目/global record 选择和 canonical provider，别名集由 host.All() 枚举，未复刻宿主名单。
- 新 installed_start_test.go 在更高层验证不同的真实 process/install/public 语义，因此并非无理由复制单元测试：实际当前 binary、install + bootstrap、完整包、registry、真实 Git、公共 start/read/drive/submit 都有执行。
- request/state/registry 的内容断言包括 provider、record/digest/generation/lease/token、base、route、intake 完整值及 digest；拒绝后比较 state 原始字节或断言 run 目录未生成，非只检查文件存在。
- spawn event 使用真实协议 constructor，错误宿主仅替换 provider，正例的 actionID 来自实际 pendingActions、correlation 为声明 worker，未把不同协议输入都塞给返回同值的模拟端。
- request-only 前置是 Start 明确的 request-before-state 持久前缀，并保留 intent 的完整绑定；恢复走公共 start，结果对正常 request/state 做 RunID 归一化后的完整值对照。它是正常可达中断前置，不是以破坏内部状态制造缺陷。
- 同 ID 并发真实启动两个进程，要求恰一成功，另一个只能既有 already-exists/LOCK_HELD；之后核绑定、重复拒绝与 state 不变。
- changed writer-exclusive fixture 明确传 admission.Provider，继续只测试写入口隔离；其已有 frozen Git 替身不承担新 installed/native 证明。

执行事实：3 个新 installed 测试、2 个 admission 顶层测试（含 11 alias 子项）、5 个相关 CLI 顶层测试、12 个相关 facade 顶层测试与独立 37 命令安装轨迹均实际执行。初次准备/审查者 oracle 失败也保存，见执行账。未以构建成功或空日志记行为 PASS。

命名和注释只解释当前行为/约束；新增文件、类型、函数/测试无开发阶段、审查轮次或派发码命名。测试 helper 为当前 installed fixture 的读写与操作封装，无新增生产抽象。普通模块内的几行生产改动可直接阅读和定位。测试 TemporaryDir 有生命周期清理；新生产代码不产生新的临时文件或日志。因此当前改动范围未发现资源垃圾、泄漏、无界循环、N+1 或额外热路径复制。

## 维度五：过拟合与窄逻辑 — PASS

canonical provider 由任意选中 registry record 通过现有 host.Lookup 得出，没有限定 ZCode、Codex 或某个 launcher 路径；unit 对所有 installable aliases 执行，真实 installed 正常轨迹另核 Claude alias。route 的闭集 lightweight/regular 正是本块明确要求，后续 default/full/custom 不被偷换为 bootstrap route。native base 由通用 immutable identity 与原生 ancestor 判据判断，不对某个提交/hash/样例做特判。

生产路径没有硬编码本次 run ID、测试输入、fixture 哈希或审查结论。current binary/安装 source 由测试实际构建产生，native base 由真实 Git 得到，wrong provider/control flags 是需求要求的否定 oracle。范围内非祖先/不存在/缺失/可变基线都落原 native/Start 拒绝规则；没有只修某个观测样例的窄分支。

## AGENTS 工程准则逐项验收

下表判定仅限本次变更及其同根因真实接缝；“未改变”是源码比较事实，不表示已重新验证整个既有系统。

| 条目 | 本次核验结论与当前证据 |
|---|---|
| 正确性 | PASS：admission.go:166-170、cli.go:448-475 的值传递与既有 Start native/provider 接纳一致；installed.log、public probe 的真实行为通过 |
| 质量/根因闭合 | PASS：从 raw Host、admission、public Start、request/state、issue responsibility、receipt/provider check、正常/unknown claim、cost ref 到 Open 都追踪；未发现遗漏同根因入口。protocol/engine.go:1381 与 recovery.go:350 复用同 claim callback |
| 完整需求 | PASS：维度一逐条 1-6、全部当前裁定与非目标接口列证，不以少数测试概括 |
| 最简实现 | PASS：原 facade regular/native 能力、EngineAdmission 携带者、host.Lookup 和 StartRequest.Provider 均直接复用；无新增 owner/状态/配置/流程。start-readiness 的原生/既有归属判据已核原文；未重做复杂度方案审 |
| 耦合 | PASS：host Lookup 留在 validate 集成边界，engine 沿既有 provider 值消费；无 reverse host 依赖或隐式多入口检测 |
| 可维护 | PASS：职责保持 admission、CLI 转交、facade/bootstrap、protocol 接纳和 lifecycle/cost 各自归属；调用结构未拆散紧耦合上下文 |
| 高可读 | PASS：candidateRoute/candidateBase/Provider 表达具体行为；约 20 行生产增量无需推测隐藏状态；新 tests 名称按行为组织 |
| 优雅/设计一致 | PASS：复用已有 immutable native Git、规范 host registry、精确 record reopening；对外错误类与 protocol 值合同保留，无第二种身份来源 |
| 不当硬编码 | 未发现：仅需求所定 route 闭集和已有 definition 常量；宿主名单不重复，测试 base 取实际 Git |
| 窄逻辑 | 未发现：route 条件实现需求边界，Host 为所有选中记录统一 Lookup；非祖先与可变身份真实 public 拒绝 |
| 过拟合 | 未发现：没有按 fixture/run/hash 特判；alias 全表遍历，实装证明覆盖不同 raw/canonical Host |
| 过度设计 | 未发现：无新增流程或系统；复用 original owners |
| 过度防御 | 未发现：未给手工改写 request/state/registry、权限/不可变文件或对抗性输入增加防御 |
| 过度抽象 | 未发现：一字段与既有 Lookup 调用，无新 interface/策略/框架；test helper 不进入生产 |
| 测试与业务隔离 | PASS：生产无测试专用分支；新 native/public tests 走真实 executable 与 Git，无 facade 私有调用冒充 installed 证明 |
| 一个事实一个权威 | PASS：record.Host 拥有启动 Host，host.Lookup 拥有规范化，RunProvider 拥有已建立 run 的执行绑定；request 为 durable 携带者；current spec 的 state.json 唯一投影例外保持 |
| 正常/恢复同业务与接纳 | PASS：Start:456 → recoverStartIntent:1777 → 同一 Start；SPAWNED 与 UNKNOWN attach 分别 engine.go:1381/recovery.go:350 走同 callback，result pairing 仍 commitEffects 归属；相关 recover/cost regressions 通过 |
| 跨接缝显式携带者 | PASS：维度三表列明 Route/BaseSnapshot、Provider、record/digest/fence、Attempt responsibility、receipt correlation、CostSourceRef、request 与 terminal boundary；无新隐式状态 |
| 四种轮次/代次/重试/业务返修 | 核验范围：本次没有改变轮次计数。已核现有 review.Series.Issued（review/review.go:149）、validation.Generation/WorkBinding（protocol/validation.go:23、94、422）、Attempt.ID/Attempts/MaxAttempts（protocol/protocol.go:174-190）及 ValidationWave/ValidationRepair（protocol/validation.go:106、152）的不同携带者；provider 只进入 AttemptBindings.Responsibility，不复用计数。业务返修波次的全计数规则未在本块重新完整验证。未重新跑全系统轮次组合，未宣称全链新验收 |
| 值级语义等价与地道表达 | PASS：没有重写原生 provider、恢复或规范化算法；reuse 维持接口值语义；normal/recovered request/state 完整对照而非文本结构仿制 |
| 测试期望来自需求 | PASS：新 tests 的 route/base、canonical provider、fence、intake、只读/重复/并发与错误类都来自当前需求和原接口；未更改仓库旧断言来获通过；审查者探针的 3 个错误 oracle 已按原文纠正并保留原始失败 |
| 诚实夹具/协议身份 | PASS：installed 使用当前 actual binary、native Git、real registry、actual pending action；不同 provider 必拒；人工 intake/中断前缀明确是准备材料，不伪称用户或模型证据 |
| 原始证据与关键内容 | PASS：完整 command logs + JSON exit/duration，request/state/registry/lifecycle 原工件保留；开发者测试核内容、digest 与拒绝后字节；build 空日志只记构建 |
| 相关正常/重复/并行/恢复 | PASS：installed.log、facade-regression.log 与 probe commands 18-37；同 ID 双进程、repeat、request-only、child 认领中断、unknown/cost 等相关业务边，未穷举无关组合 |
| 删除被替代路径 | PASS：public 分支固定 Provider "engine" 被 admission.Provider 替代；lightweight-only route 拒绝被需求两 route 接受替代；无并行旧 Host 来源、新 ledger 或 marker；无需删除继续服务内部 facade callers 的 DefaultProvider |
| 当前注释 | PASS：新增 Provider comment、fixture 当前边界及恢复/并发/安装说明是当前约束，无演进叙述；未添加重复权威合同 |
| 域命名/外部探针去过程化 | PASS：新名称均按 installed start/host/recovery/invalid input 行为；diff 无外部审查轮次/来源署名/快照路径探针入树 |
| 性能/资源 | 本次源码核查无退化：finite Lookup，没有新热路径扫描/分配大对象或生产临时物；test TempDir 自动归属。未做 benchmark，也没有证据支持性能缺陷 |
| 派发/返修历史纪律 | 历史是否逐步先引用、派发及删除“每轮”账本未验证：该项不能从当前固定 diff 证明，且任务禁止读开发报告/交接；当前范围未出现主代理新增实施代码的审查事实。此报告不宣称历史流程通过 |
| 如实记账/不 push | PASS（本审查行为）：所有执行失败/通过记录；真实模型调用/费用 0；未 commit/push；正式主代理后续独立复核仍由主代理负责 |

## 发现项

无范围内 P0/P1/P2 发现；无当前 diff 的 P3 建议。不存在需要填写需求、正常公开复现、实际错误、定级理由和同根因影响范围的 finding。

初次 isolation preparation 与 reviewer oracle 的失败不作为产品 finding：失败均有具体原始记录；同一受审源码未修改后满足需求的实际行为已执行通过。

## 实际执行与原始证据

全部执行工作目录 S。完整 argv、cwd、exitCode、elapsedSeconds、raw log 路径在对应 `.json` 中。日志均是原始完整 stdout/stderr，未删 FAIL。所有 go tests 设 `-count=1`，没有用缓存或 build 冒充行为执行。

| 执行组 | 退出码 | 秒 | 完整原始日志（目录前缀 E=/tmp/installed-start-quality-137b0ba-evidence） | 判读 |
|---|---:|---:|---|---|
| admission 初次 | 1 | 6.794 | E/admission.log | 原始 FAIL：archive 不含 ignored bin；target package completeness 的既有验证拒绝，尚未进入 provider 行为 |
| current fixed binary build | 0 | 1.056 | E/fixed-binary-build.log | build 成功；空日志，仅构建证据 |
| admission 补齐 binary | 0 | 2.431 | E/admission-with-binary.log | 实际两个顶层测试/11 alias 子项 PASS |
| installed 新测试 | 0 | 20.804 | E/installed.log | 3 个顶层测试实际 PASS |
| CLI writer/read-preview 隔离 | 0 | 7.725 | E/cli-writers.log | 5 个顶层测试实际 PASS |
| facade 相关回归 | 0 | 10.472 | E/facade-regression.log | 12 个顶层测试及对应子项实际 PASS |
| 独立 public probe 首次 | 1 | 5.144 | E/public-probe.log；E/public-probe/01.log 至 17.log | reviewer oracle 错把无共同祖先的错误文本要求成 `not an ancestor`；实际 exit 1 的 native ancestry 拒绝符合需求，源码未改 |
| 独立 public probe 原始第二轨迹 | 1 | 3.623 | E/public-probe-verified.log；E/public-probe-verified/01.log 至 18.log | reviewer oracle 错要求 raw registry.Host 等于 canonical provider；实际 request/state 的 `claude-code` 已正确，registry 为已安装的 `claude` |
| 独立 probe 第一次续接 | 1 | 0.047 | E/public-probe-continued.log | reviewer 错假定 installed 包有 hooks/ 目录；package.go requiredDirs 与 actual hook owner 并无此目录，skip-hooks 为明确前置 |
| 独立 probe 正确续接 | 0 | 3.855 | E/public-probe-completed.log；E/public-probe-verified/19.log 至 37.log | 37 命令完整轨迹最后 PASS；继续同一已建立 run，未重做已通过前缀 |

独立 probe 完整 argv、输入 JSON、每命令期望/实际 exit 与耗时：`E/public-probe-verified/commands.jsonl`、`NN.stdin.json`、`NN.log`。保留了先前三个错误 oracle 的源码：`E/public_probe_initial.py`、`E/public_probe_registered_host_oracle.py`、`E/public_probe_layout_oracle.py`；最终续接源码为 `E/public_probe_continue.py`。这些仅是本审查者 /tmp 材料，没有进仓库。

独立 probe 关键实际工件：

- `E/public-probe-verified/application/.gates/engine/claude-native/request.json` 与 `state.json`；第二宿主同目录下 `codex-native/request.json` 与 `state.json`。
- `E/public-probe-verified/host-home/.formal-gates/registry.json` 与 `.bootstrap.json`。
- `E/public-probe-verified/application/.gates/tmp/claude-native/lifecycle/` 中真实 claim/event 工件。
- `E/public-probe-verified/bindings-summary.json`：canonical `claude-code`，claim 的 run/dispatch/identity 全值，lifecycle `VERIFIED` 且 start/stop 均已观察，second provider `codex`，native HEAD 仍为 `1f4d2f4733d889cf531affcb0ee0927df8bf906f`。
- 第 24 个命令的错误 provider receipt 返回 `PROVIDER_MISMATCH`，第 25 个正确 receipt 被接纳；脚本在拒绝后比较 state 原字节。第 29 个重复 start 稳定 already-exists 且 state 不变。

检查时遇到路径定位失误：早期 rg 试读了不存在的 `requirements/master-requirements.md`、`internal/lifecycle/store.go`、`internal/lifecycle/claude.go`、`internal/engine/runtime/runtime.go`、`Makefile`、`internal/validate/install_admission.go`；随后以现有 tracked 路径定位原文/归属。它们是只读检索错误，不是构建、行为或产品缺陷，也未记 PASS。末次查找 `Repair*Round/Count` 样式未命中导致 rg exit 1，随后按实际 ValidationWave/ValidationRepair 类型归属核对；同样没有将空结果记行为 PASS。

## 未验证与限制

- 未做全仓测试、benchmark、跨平台运行、真实模型执行或自动安装 hook 生效验证；本块安装前置明确 skip-hooks，独立 lifecycle capture 是正常公开观察输入。
- 对 installed 成本端到端精确 token 数未新建实装 transcript 源；已追踪真实 public spawn → canonical lifecycle claim，且现有 facade 的正常 COST_PENDING→精确 usage、UNKNOWN attach→claim/usage 回归实际执行通过。此差异不宣称完整 installed native 执行桥完成。
- request-only restored value equivalence 和同 ID 并发由当前交付测试真实 public binary 执行；本审查未新建另一套重复恢复 probe。
- 未重跑完整大阶段、Controller/workspaces/merge/lifecycle 后续全功能；仅核本变更未破坏其现有接口与本块相关 child/ordinary/cost 回归。
- 受审历史派发/逐次授权/返修账本无法从当前 diff 推断，未读取禁止材料求证；候选 PASS 仅根据当前代码、当前需求和实际相关执行。

## 实际顶层命令原文


admission：

```text
go test ./internal/validate -run 'Test(ResolveEngineAdmissionChoosesProjectBinding|EngineAdmissionNormalizesRegisteredHostAliases)$' -count=1 -v
```

fixed-binary-build：

```text
go build -o bin/formal-gates ./cmd/formal-gates
```

admission-with-binary：

```text
go test ./internal/validate -run 'Test(ResolveEngineAdmissionChoosesProjectBinding|EngineAdmissionNormalizesRegisteredHostAliases)$' -count=1 -v
```

installed：

```text
go test ./internal/engine/acceptance -run '^TestInstalled' -count=1 -v
```

cli-writers：

```text
go test ./internal/cli -run 'Test(LegacyWritersCannotReachEngineRunState|RepairPreviewProjectsEngineRun|LegacyWriteCommandsRejectEngineRunPerCommand|PublicCleanupCannotOperateOnActiveEngineRun|AlternatingLegacyAndEngineEntriesKeepBindingLocked)$' -count=1 -v
```

facade-regression：

```text
go test ./internal/engine/facade -run 'Test(OrdinaryGateRoutesCompleteWithoutQA|DefaultRouteRequiresConfirmedDevelopmentPlan|SliceChildStartValidations|SliceChildInheritsAndSelectsRoute|SliceChildParallelClaimsAreMutuallyExclusive|SliceChildClaimSurvivesInterruptedStart|SliceChildStartRebuildsAfterRequestOnlyPersistence|SliceChildRequiresExplicitRegularRoute|SliceChildSelectedGatesExecuteToCompletion|SplitTopologyBindsACOwnersAndLocksMergeRoute|CostPublicChainHoldsPendingThenRecordsExactUsage|UnknownReceiptAttachesMeteredIdentityAndRecordsUsage)$' -count=1 -v
```

public-probe：

```text
python3 /tmp/installed-start-quality-137b0ba-evidence/public_probe.py
```

public-probe-verified：

```text
python3 /tmp/installed-start-quality-137b0ba-evidence/public_probe.py
```

public-probe-continued：

```text
python3 /tmp/installed-start-quality-137b0ba-evidence/public_probe_continue.py
```

public-probe-completed：

```text
python3 /tmp/installed-start-quality-137b0ba-evidence/public_probe_continue.py
```

以上进程耗时累计 61.951 秒（包含并发组的逐进程时间，不是墙钟时间）；自隔离提取记录创建至本报告写出约 1044.2 秒。真实模型调用 0、费用 0。

````

## index.json

来源：/tmp/installed-public-start-original-evidence/index.json；bytes=778；sha256=423db0f29c835fca629d250ddce731bdac4acffc921c6d544b753a613a8fb38f

````text
{
  "installed-regular-start-business_test.go": {
    "sha256": "0c6514f122cfd15070ff6e13bf3fb1742c672c5699421b62e8b680d65faa3b25",
    "bytes": 6784
  },
  "installed-regular-start-overlay.json": {
    "sha256": "647a8b712702575d91a2d31b3a57ce55fa2813c2adfd68d009b611316e3aafe1",
    "bytes": 211
  },
  "installed-public-start-original-failure.log": {
    "sha256": "375470e5d1c93ff88392df5c28473683d53498dd856c5e0f5814d8ec1ce0be8b",
    "bytes": 632
  },
  "installed-public-start-original-compilation.log": {
    "sha256": "0a90d33759b74142d3c1c60c07c3375b194f78960300258be7777b0761135a53",
    "bytes": 785
  },
  "installed-public-start-original-observation.json": {
    "sha256": "78768115fccb184f1e9711a87f8266d11009610c8ec5a92d1da47f8081c617f9",
    "bytes": 7862
  }
}
````

## installed-public-start-root-after.log

来源：/tmp/installed-public-start-root-after.log；bytes=3659；sha256=7c4198f04caa9bd6eee06bfef12881ed4385dea7f2592778f7fb924359dc42c0

````text
=== RUN   TestInstalledPublicStartAdmitsRegularNativeRun
    installed_regular_start_business_test.go:99: immutable actual package=ddcda1653f8110ca2e8f517473262d1662dbc6cce442501c622035191c4d80b4 record=zcode-global-56184d2663f9 host=zcode; public regular result="{\n  \"runId\": \"native-review\",\n  \"runtime\": \"engine\",\n  \"route\": \"regular\",\n  \"status\": \"ACTIVE\",\n  \"revision\": 2,\n  \"envelope\": {\n    \"writer\": \"engine\",\n    \"stateSchemaVersion\": \"1\",\n    \"workflowDefinitionVersion\": \"6\",\n    \"definitionSource\": \"definitions/workflow.json\",\n    \"definitionDigest\": \"sha256:1eabbb1117e67d433dd41c2db8684e1589084ebf8bfa2ac8879b3939445c7d55\",\n    \"packageDigest\": \"ddcda1653f8110ca2e8f517473262d1662dbc6cce442501c622035191c4d80b4\",\n    \"installedTargetIdentity\": \"zcode-global-56184d2663f9\"\n  },\n  \"baseSnapshot\": \"23f57940e7bbcf82043172a8cacb490fad276068\",\n  \"intakeConfirmationReceipt\": {\n    \"source\": \"stable-driver\",\n    \"authority\": \"stable-driver\",\n    \"transport\": \"stable-launcher\",\n    \"requirementSource\": \"requirements.md\",\n    \"requirementRevision\": \"0068b0eb338f1a47a1af833bfc2afa058dff30ad3551865b24108e187eaaed5a\",\n    \"artifacts\": [\n      {\n        \"path\": \"design.md\",\n        \"revision\": \"f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002\"\n      },\n      {\n        \"path\": \"requirements.md\",\n        \"revision\": \"0068b0eb338f1a47a1af833bfc2afa058dff30ad3551865b24108e187eaaed5a\"\n      }\n    ],\n    \"solutionRevision\": \"f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002\",\n    \"solutionDigest\": \"sha256:f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002\",\n    \"digest\": \"sha256:0e417adf92de1b3df5d95848895dc968419bdfde8630ad8c311eea5c04a22e82\"\n  },\n  \"next\": {\n    \"Kind\": \"WAIT\",\n    \"Ready\": null,\n    \"HostAction\": null,\n    \"Ask\": null,\n    \"Wait\": {\n      \"reason\": \"ENGINE_INTERNAL\"\n    },\n    \"Operator\": null,\n    \"Complete\": null\n  },\n  \"review\": {\n    \"actions\": [\n      {\n        \"action\": \"product-review\",\n        \"completed\": 0,\n        \"baseLimit\": 3,\n        \"extraAuthorized\": 0,\n        \"effectiveLimit\": 3,\n        \"remaining\": 3,\n        \"nextRoundRequirement\": \"AUTOMATIC_CAPACITY\",\n        \"grants\": [],\n        \"rawResults\": 0,\n        \"adjudications\": 0,\n        \"corrections\": 0,\n        \"effectiveStatus\": \"PENDING\",\n        \"effectiveSource\": \"WORKFLOW_START\",\n        \"candidates\": [],\n        \"effectiveRevision\": 0\n      },\n      {\n        \"action\": \"start-readiness\",\n        \"completed\": 0,\n        \"baseLimit\": 3,\n        \"extraAuthorized\": 0,\n        \"effectiveLimit\": 3,\n        \"remaining\": 3,\n        \"nextRoundRequirement\": \"AUTOMATIC_CAPACITY\",\n        \"grants\": [],\n        \"rawResults\": 0,\n        \"adjudications\": 0,\n        \"corrections\": 0,\n        \"effectiveStatus\": \"PENDING\",\n        \"effectiveSource\": \"WORKFLOW_START\",\n        \"candidates\": [],\n        \"effectiveRevision\": 0\n      }\n    ],\n    \"requirementRevision\": \"0068b0eb338f1a47a1af833bfc2afa058dff30ad3551865b24108e187eaaed5a\"\n  },\n  \"requirementWork\": [],\n  \"summaryPath\": \"/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/003/.gates/engine/native-review/terminal-summary.json\"\n}\n"
--- PASS: TestInstalledPublicStartAdmitsRegularNativeRun (3.39s)
PASS
ok  	formal-gates/internal/engine/acceptance	5.649s
real 6.28
user 1.22
sys 1.38

````

## installed-public-start-root-after-observation.json

来源：/tmp/installed-public-start-root-after-observation.json；bytes=11012；sha256=7955fa9522d32bbdbde26a4b0d74b463ab0d7085b094b0de417f7cd13ed05149

````text
{
  "record": {
    "id": "zcode-global-56184d2663f9",
    "target": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.zcode/skills/formal-gates",
    "launcherPath": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.local/bin/formal-gates",
    "scope": "global",
    "host": "zcode",
    "hookConfig": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.zcode/cli/config.json",
    "skipHooks": true,
    "projectRoot": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001",
    "stateRoot": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.gates",
    "resourceRoot": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.formal-gates-resources",
    "runtimeSibling": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.zcode/skills",
    "vcsIdentity": "package:ddcda1653f8110ca2e8f517473262d1662dbc6cce442501c622035191c4d80b4",
    "packageDigest": "ddcda1653f8110ca2e8f517473262d1662dbc6cce442501c622035191c4d80b4",
    "installedDigest": "7a46573dbf14d2cf8969e5a5754e76495cadeaec58715b836abe80a17fa271b9",
    "canonicalPaths": {
      "hookConfig": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.zcode/cli/config.json",
      "launcher": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.local/bin/formal-gates",
      "projectRoot": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001",
      "resourceRoot": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.formal-gates-resources",
      "runtimeSibling": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.zcode/skills",
      "stateRoot": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.gates",
      "target": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.zcode/skills/formal-gates"
    },
    "status": "active",
    "generation": 1,
    "lease": "lease-1",
    "token": "token-1791035377186026000"
  },
  "bootstrap": {
    "operation": "bootstrap",
    "accepted": true,
    "registry": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.formal-gates/registry.json",
    "epoch": 1,
    "packageDigest": "ddcda1653f8110ca2e8f517473262d1662dbc6cce442501c622035191c4d80b4",
    "vcsIdentity": "package:ddcda1653f8110ca2e8f517473262d1662dbc6cce442501c622035191c4d80b4",
    "sourceRoot": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/002",
    "sourceLstat": {
      "path": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/002",
      "realPath": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/002",
      "mode": 493,
      "size": 1120,
      "kind": "directory"
    },
    "records": [
      {
        "id": "zcode-global-56184d2663f9",
        "target": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.zcode/skills/formal-gates",
        "launcherPath": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.local/bin/formal-gates",
        "scope": "global",
        "host": "zcode",
        "hookConfig": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.zcode/cli/config.json",
        "skipHooks": true,
        "projectRoot": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001",
        "stateRoot": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.gates",
        "resourceRoot": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.formal-gates-resources",
        "runtimeSibling": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.zcode/skills",
        "vcsIdentity": "package:ddcda1653f8110ca2e8f517473262d1662dbc6cce442501c622035191c4d80b4",
        "packageDigest": "ddcda1653f8110ca2e8f517473262d1662dbc6cce442501c622035191c4d80b4",
        "installedDigest": "7a46573dbf14d2cf8969e5a5754e76495cadeaec58715b836abe80a17fa271b9",
        "canonicalPaths": {
          "hookConfig": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.zcode/cli/config.json",
          "launcher": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.local/bin/formal-gates",
          "projectRoot": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001",
          "resourceRoot": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.formal-gates-resources",
          "runtimeSibling": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.zcode/skills",
          "stateRoot": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.gates",
          "target": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/001/.zcode/skills/formal-gates"
        },
        "status": "active",
        "generation": 1,
        "lease": "lease-1",
        "token": "token-1791035377186026000"
      }
    ],
    "stateCreated": false,
    "observedAt": "2026-10-03T13:49:37.442496Z"
  },
  "intake": {
    "source": "stable-driver",
    "authority": "stable-driver",
    "transport": "stable-launcher",
    "requirementSource": "requirements.md",
    "requirementRevision": "0068b0eb338f1a47a1af833bfc2afa058dff30ad3551865b24108e187eaaed5a",
    "artifacts": [
      {
        "path": "requirements.md",
        "revision": "0068b0eb338f1a47a1af833bfc2afa058dff30ad3551865b24108e187eaaed5a"
      },
      {
        "path": "design.md",
        "revision": "f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002"
      }
    ],
    "solutionRevision": "f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002",
    "solutionDigest": "sha256:f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002"
  },
  "base": "23f57940e7bbcf82043172a8cacb490fad276068",
  "args": [
    "workflow",
    "start",
    "--root",
    "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/003",
    "--run-id",
    "native-review",
    "--route",
    "regular",
    "--base-snapshot",
    "23f57940e7bbcf82043172a8cacb490fad276068",
    "--intake-receipt",
    "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/004/intake.json"
  ],
  "output": "{\n  \"runId\": \"native-review\",\n  \"runtime\": \"engine\",\n  \"route\": \"regular\",\n  \"status\": \"ACTIVE\",\n  \"revision\": 2,\n  \"envelope\": {\n    \"writer\": \"engine\",\n    \"stateSchemaVersion\": \"1\",\n    \"workflowDefinitionVersion\": \"6\",\n    \"definitionSource\": \"definitions/workflow.json\",\n    \"definitionDigest\": \"sha256:1eabbb1117e67d433dd41c2db8684e1589084ebf8bfa2ac8879b3939445c7d55\",\n    \"packageDigest\": \"ddcda1653f8110ca2e8f517473262d1662dbc6cce442501c622035191c4d80b4\",\n    \"installedTargetIdentity\": \"zcode-global-56184d2663f9\"\n  },\n  \"baseSnapshot\": \"23f57940e7bbcf82043172a8cacb490fad276068\",\n  \"intakeConfirmationReceipt\": {\n    \"source\": \"stable-driver\",\n    \"authority\": \"stable-driver\",\n    \"transport\": \"stable-launcher\",\n    \"requirementSource\": \"requirements.md\",\n    \"requirementRevision\": \"0068b0eb338f1a47a1af833bfc2afa058dff30ad3551865b24108e187eaaed5a\",\n    \"artifacts\": [\n      {\n        \"path\": \"design.md\",\n        \"revision\": \"f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002\"\n      },\n      {\n        \"path\": \"requirements.md\",\n        \"revision\": \"0068b0eb338f1a47a1af833bfc2afa058dff30ad3551865b24108e187eaaed5a\"\n      }\n    ],\n    \"solutionRevision\": \"f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002\",\n    \"solutionDigest\": \"sha256:f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002\",\n    \"digest\": \"sha256:0e417adf92de1b3df5d95848895dc968419bdfde8630ad8c311eea5c04a22e82\"\n  },\n  \"next\": {\n    \"Kind\": \"WAIT\",\n    \"Ready\": null,\n    \"HostAction\": null,\n    \"Ask\": null,\n    \"Wait\": {\n      \"reason\": \"ENGINE_INTERNAL\"\n    },\n    \"Operator\": null,\n    \"Complete\": null\n  },\n  \"review\": {\n    \"actions\": [\n      {\n        \"action\": \"product-review\",\n        \"completed\": 0,\n        \"baseLimit\": 3,\n        \"extraAuthorized\": 0,\n        \"effectiveLimit\": 3,\n        \"remaining\": 3,\n        \"nextRoundRequirement\": \"AUTOMATIC_CAPACITY\",\n        \"grants\": [],\n        \"rawResults\": 0,\n        \"adjudications\": 0,\n        \"corrections\": 0,\n        \"effectiveStatus\": \"PENDING\",\n        \"effectiveSource\": \"WORKFLOW_START\",\n        \"candidates\": [],\n        \"effectiveRevision\": 0\n      },\n      {\n        \"action\": \"start-readiness\",\n        \"completed\": 0,\n        \"baseLimit\": 3,\n        \"extraAuthorized\": 0,\n        \"effectiveLimit\": 3,\n        \"remaining\": 3,\n        \"nextRoundRequirement\": \"AUTOMATIC_CAPACITY\",\n        \"grants\": [],\n        \"rawResults\": 0,\n        \"adjudications\": 0,\n        \"corrections\": 0,\n        \"effectiveStatus\": \"PENDING\",\n        \"effectiveSource\": \"WORKFLOW_START\",\n        \"candidates\": [],\n        \"effectiveRevision\": 0\n      }\n    ],\n    \"requirementRevision\": \"0068b0eb338f1a47a1af833bfc2afa058dff30ad3551865b24108e187eaaed5a\"\n  },\n  \"requirementWork\": [],\n  \"summaryPath\": \"/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun372828215/003/.gates/engine/native-review/terminal-summary.json\"\n}\n"
}
````

## installed-public-start-root-boundaries.log

来源：/tmp/installed-public-start-root-boundaries.log；bytes=488；sha256=334193441e61aa1b26708dc971e1c6614abd2ee9372b60c7f435099057ae32b3

````text
=== RUN   TestInstalledRegularStartCarriesHostThroughPublicOperations
--- PASS: TestInstalledRegularStartCarriesHostThroughPublicOperations (8.11s)
=== RUN   TestInstalledStartRejectsInvalidInputsBeforeState
--- PASS: TestInstalledStartRejectsInvalidInputsBeforeState (3.80s)
=== RUN   TestInstalledLightweightAndRegularStartRecovery
--- PASS: TestInstalledLightweightAndRegularStartRecovery (3.75s)
PASS
ok  	formal-gates/internal/engine/acceptance	17.061s
real 17.80
user 4.74
sys 5.71

````

## installed-start-root-baseline-lifecycle-environment.log

来源：/tmp/installed-start-root-baseline-lifecycle-environment.log；bytes=262；sha256=bbc42ecced25c987ba6ead9e88384484a1231d5609f9bddb5550f62ecea201e9

````text
=== RUN   TestProviderFromEnvironmentRecognizesZCode
    lifecycle_test.go:437: ZCODE_PLUGIN_ROOT resolved to "codex"
--- FAIL: TestProviderFromEnvironmentRecognizesZCode (0.00s)
FAIL
FAIL	formal-gates/internal/lifecycle	0.474s
FAIL
real 0.86
user 0.44
sys 0.28

````

## installed-start-root-baseline-lifecycle-isolated.log

来源：/tmp/installed-start-root-baseline-lifecycle-isolated.log；bytes=192；sha256=7a37f84d560eafa273b3a88ccaadafb529ee973d1ad44d5ea73cc046d02d2b7d

````text
=== RUN   TestProviderFromEnvironmentRecognizesZCode
--- PASS: TestProviderFromEnvironmentRecognizesZCode (0.00s)
PASS
ok  	formal-gates/internal/lifecycle	0.473s
real 1.03
user 0.18
sys 0.22

````

## installed-start-root-current-lifecycle-isolated.log

来源：/tmp/installed-start-root-current-lifecycle-isolated.log；bytes=73；sha256=8ab71f2dd13558f64f47d9b87143727f304d65506ef9fe758d0350caa2e50407

````text
ok  	formal-gates/internal/lifecycle	0.607s
real 1.14
user 0.20
sys 0.28

````

## installed-start-root-environment-signals.json

来源：/tmp/installed-start-root-environment-signals.json；bytes=300；sha256=d1b94434444673b2a24fad62a87aa8c69824d94d44c7741003fe98e6d5d9c77a

````text
{
  "AI_AGENT": false,
  "CLAUDE_CODE_ENTRYPOINT": false,
  "CODEX_HOME": false,
  "CODEX_CLI_PATH": true,
  "CURSOR_TRACE_ID": false,
  "CURSOR_RUNTIME": false,
  "DSH_HOME": false,
  "DSH_PROJECT_DIR": false,
  "ZCODE_PLUGIN_ROOT": false,
  "ZCODE_PLUGIN_ID": false,
  "ZCODE_PLUGIN_NAME": false
}

````

## admission-with-binary.json

来源：/tmp/installed-start-quality-137b0ba-evidence/admission-with-binary.json；bytes=468；sha256=f71f24d3c0fff87afcfa2372df5e6f302428cb79800a58c1cf1cb78308f23cf9

````text
{
  "command": [
    "go",
    "test",
    "./internal/validate",
    "-run",
    "Test(ResolveEngineAdmissionChoosesProjectBinding|EngineAdmissionNormalizesRegisteredHostAliases)$",
    "-count=1",
    "-v"
  ],
  "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/source",
  "exitCode": 0,
  "elapsedSeconds": 2.431,
  "log": "/private/tmp/installed-start-quality-137b0ba-evidence/admission-with-binary.log",
  "realModelCalls": 0,
  "realModelCost": 0
}

````

## admission-with-binary.log

来源：/tmp/installed-start-quality-137b0ba-evidence/admission-with-binary.log；bytes=2242；sha256=4fcb431d94501b8ae20d5fab8256dab1286a8a663ada0cdc22805128198f9f4f

````text
=== RUN   TestResolveEngineAdmissionChoosesProjectBinding
--- PASS: TestResolveEngineAdmissionChoosesProjectBinding (0.23s)
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude-code
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude_code
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/codex/codex
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/cursor/cursor
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/dsh
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek-harness
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek_harness
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/zcode
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/z-code
--- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases (0.91s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude (0.07s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude-code (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude_code (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/codex/codex (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/cursor/cursor (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/dsh (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek-harness (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek_harness (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/zcode (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/z-code (0.08s)
PASS
ok  	formal-gates/internal/validate	1.475s

````

## admission.json

来源：/tmp/installed-start-quality-137b0ba-evidence/admission.json；bytes=456；sha256=1feb4d3a192bd2ed38aa3d998ef5a4019f20858ede441514f20639e8dc1363c2

````text
{
  "command": [
    "go",
    "test",
    "./internal/validate",
    "-run",
    "Test(ResolveEngineAdmissionChoosesProjectBinding|EngineAdmissionNormalizesRegisteredHostAliases)$",
    "-count=1",
    "-v"
  ],
  "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/source",
  "exitCode": 1,
  "elapsedSeconds": 6.794,
  "log": "/private/tmp/installed-start-quality-137b0ba-evidence/admission.log",
  "realModelCalls": 0,
  "realModelCost": 0
}

````

## admission.log

来源：/tmp/installed-start-quality-137b0ba-evidence/admission.log；bytes=7112；sha256=105e9d958b82636b54bf17aa812284e23c766abbbd6cf665622a12ad68716ab3

````text
=== RUN   TestResolveEngineAdmissionChoosesProjectBinding
    engine_admission_test.go:58: resolve /var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestResolveEngineAdmissionChoosesProjectBinding3844778486/002: UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-quality-137b0ba-evidence/source
--- FAIL: TestResolveEngineAdmissionChoosesProjectBinding (0.08s)
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude
    engine_admission_test.go:127: host " CLAUDE ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-quality-137b0ba-evidence/source
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude-code
    engine_admission_test.go:127: host " CLAUDE-CODE ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-quality-137b0ba-evidence/source
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude_code
    engine_admission_test.go:127: host " CLAUDE CODE ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-quality-137b0ba-evidence/source
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/codex/codex
    engine_admission_test.go:127: host " CODEX ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-quality-137b0ba-evidence/source
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/cursor/cursor
    engine_admission_test.go:127: host " CURSOR ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-quality-137b0ba-evidence/source
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/dsh
    engine_admission_test.go:127: host " DSH ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-quality-137b0ba-evidence/source
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek
    engine_admission_test.go:127: host " DEEPSEEK ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-quality-137b0ba-evidence/source
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek-harness
    engine_admission_test.go:127: host " DEEPSEEK-HARNESS ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-quality-137b0ba-evidence/source
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek_harness
    engine_admission_test.go:127: host " DEEPSEEK HARNESS ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-quality-137b0ba-evidence/source
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/zcode
    engine_admission_test.go:127: host " ZCODE ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-quality-137b0ba-evidence/source
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/z-code
    engine_admission_test.go:127: host " Z-CODE ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-quality-137b0ba-evidence/source
--- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases (0.07s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude-code (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude_code (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/codex/codex (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/cursor/cursor (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/dsh (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek-harness (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek_harness (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/zcode (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/z-code (0.00s)
FAIL
FAIL	formal-gates/internal/validate	0.918s
FAIL

````

## cli-writers.json

来源：/tmp/installed-start-quality-137b0ba-evidence/cli-writers.json；bytes=572；sha256=c71bc7c2871eca7ffed7fbfa175c2095cfb39e3efef2ba05dbad76791ae836af

````text
{
  "command": [
    "go",
    "test",
    "./internal/cli",
    "-run",
    "Test(LegacyWritersCannotReachEngineRunState|RepairPreviewProjectsEngineRun|LegacyWriteCommandsRejectEngineRunPerCommand|PublicCleanupCannotOperateOnActiveEngineRun|AlternatingLegacyAndEngineEntriesKeepBindingLocked)$",
    "-count=1",
    "-v"
  ],
  "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/source",
  "exitCode": 0,
  "elapsedSeconds": 7.725,
  "log": "/private/tmp/installed-start-quality-137b0ba-evidence/cli-writers.log",
  "realModelCalls": 0,
  "realModelCost": 0
}

````

## cli-writers.log

来源：/tmp/installed-start-quality-137b0ba-evidence/cli-writers.log；bytes=643；sha256=6451d9c53640e82a594eb4eb730222c4b1103406ba2f3cff66dda1ded5d657f4

````text
=== RUN   TestLegacyWritersCannotReachEngineRunState
--- PASS: TestLegacyWritersCannotReachEngineRunState (0.56s)
=== RUN   TestRepairPreviewProjectsEngineRun
--- PASS: TestRepairPreviewProjectsEngineRun (0.26s)
=== RUN   TestLegacyWriteCommandsRejectEngineRunPerCommand
--- PASS: TestLegacyWriteCommandsRejectEngineRunPerCommand (0.51s)
=== RUN   TestPublicCleanupCannotOperateOnActiveEngineRun
--- PASS: TestPublicCleanupCannotOperateOnActiveEngineRun (0.28s)
=== RUN   TestAlternatingLegacyAndEngineEntriesKeepBindingLocked
--- PASS: TestAlternatingLegacyAndEngineEntriesKeepBindingLocked (0.22s)
PASS
ok  	formal-gates/internal/cli	3.364s

````

## facade-regression.json

来源：/tmp/installed-start-quality-137b0ba-evidence/facade-regression.json；bytes=886；sha256=802037d40c2ac5b2766269ad7af0d2ad642dfa50437823a2c0bb85a2482c4eff

````text
{
  "command": [
    "go",
    "test",
    "./internal/engine/facade",
    "-run",
    "Test(OrdinaryGateRoutesCompleteWithoutQA|DefaultRouteRequiresConfirmedDevelopmentPlan|SliceChildStartValidations|SliceChildInheritsAndSelectsRoute|SliceChildParallelClaimsAreMutuallyExclusive|SliceChildClaimSurvivesInterruptedStart|SliceChildStartRebuildsAfterRequestOnlyPersistence|SliceChildRequiresExplicitRegularRoute|SliceChildSelectedGatesExecuteToCompletion|SplitTopologyBindsACOwnersAndLocksMergeRoute|CostPublicChainHoldsPendingThenRecordsExactUsage|UnknownReceiptAttachesMeteredIdentityAndRecordsUsage)$",
    "-count=1",
    "-v"
  ],
  "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/source",
  "exitCode": 0,
  "elapsedSeconds": 10.472,
  "log": "/private/tmp/installed-start-quality-137b0ba-evidence/facade-regression.log",
  "realModelCalls": 0,
  "realModelCost": 0
}

````

## facade-regression.log

来源：/tmp/installed-start-quality-137b0ba-evidence/facade-regression.log；bytes=4007；sha256=cba86a695a968d12baed223520d8d059b6872bab791066723dc3be5f77fda547

````text
=== RUN   TestUnknownReceiptAttachesMeteredIdentityAndRecordsUsage
--- PASS: TestUnknownReceiptAttachesMeteredIdentityAndRecordsUsage (0.23s)
=== RUN   TestCostPublicChainHoldsPendingThenRecordsExactUsage
--- PASS: TestCostPublicChainHoldsPendingThenRecordsExactUsage (0.16s)
=== RUN   TestSplitTopologyBindsACOwnersAndLocksMergeRoute
=== RUN   TestSplitTopologyBindsACOwnersAndLocksMergeRoute/missing-owner
=== RUN   TestSplitTopologyBindsACOwnersAndLocksMergeRoute/duplicate
=== RUN   TestSplitTopologyBindsACOwnersAndLocksMergeRoute/unknown-owner
=== RUN   TestSplitTopologyBindsACOwnersAndLocksMergeRoute/unknown-ac
=== RUN   TestSplitTopologyBindsACOwnersAndLocksMergeRoute/master-merge_primary_responsibility_binds
--- PASS: TestSplitTopologyBindsACOwnersAndLocksMergeRoute (1.31s)
    --- PASS: TestSplitTopologyBindsACOwnersAndLocksMergeRoute/missing-owner (0.20s)
    --- PASS: TestSplitTopologyBindsACOwnersAndLocksMergeRoute/duplicate (0.20s)
    --- PASS: TestSplitTopologyBindsACOwnersAndLocksMergeRoute/unknown-owner (0.20s)
    --- PASS: TestSplitTopologyBindsACOwnersAndLocksMergeRoute/unknown-ac (0.22s)
    --- PASS: TestSplitTopologyBindsACOwnersAndLocksMergeRoute/master-merge_primary_responsibility_binds (0.25s)
=== RUN   TestSliceChildStartValidations
=== RUN   TestSliceChildStartValidations/master_missing
=== RUN   TestSliceChildStartValidations/master_not_split-bound
=== RUN   TestSliceChildStartValidations/master_not_ACTIVE
=== RUN   TestSliceChildStartValidations/slice_outside_the_topology
=== RUN   TestSliceChildStartValidations/claim_held_by_another_child_run
=== RUN   TestSliceChildStartValidations/intake_revision_mismatch
=== RUN   TestSliceChildStartValidations/base_snapshot_mismatch
=== RUN   TestSliceChildStartValidations/both_identity_fields_are_required
--- PASS: TestSliceChildStartValidations (0.84s)
    --- PASS: TestSliceChildStartValidations/master_missing (0.00s)
    --- PASS: TestSliceChildStartValidations/master_not_split-bound (0.25s)
    --- PASS: TestSliceChildStartValidations/master_not_ACTIVE (0.30s)
    --- PASS: TestSliceChildStartValidations/slice_outside_the_topology (0.00s)
    --- PASS: TestSliceChildStartValidations/claim_held_by_another_child_run (0.06s)
    --- PASS: TestSliceChildStartValidations/intake_revision_mismatch (0.00s)
    --- PASS: TestSliceChildStartValidations/base_snapshot_mismatch (0.00s)
    --- PASS: TestSliceChildStartValidations/both_identity_fields_are_required (0.00s)
=== RUN   TestSliceChildInheritsAndSelectsRoute
--- PASS: TestSliceChildInheritsAndSelectsRoute (0.44s)
=== RUN   TestSliceChildParallelClaimsAreMutuallyExclusive
--- PASS: TestSliceChildParallelClaimsAreMutuallyExclusive (0.31s)
=== RUN   TestSliceChildClaimSurvivesInterruptedStart
--- PASS: TestSliceChildClaimSurvivesInterruptedStart (0.36s)
=== RUN   TestSliceChildStartRebuildsAfterRequestOnlyPersistence
--- PASS: TestSliceChildStartRebuildsAfterRequestOnlyPersistence (0.33s)
=== RUN   TestSliceChildRequiresExplicitRegularRoute
--- PASS: TestSliceChildRequiresExplicitRegularRoute (0.30s)
=== RUN   TestSliceChildSelectedGatesExecuteToCompletion
=== RUN   TestSliceChildSelectedGatesExecuteToCompletion/full
=== RUN   TestSliceChildSelectedGatesExecuteToCompletion/custom
--- PASS: TestSliceChildSelectedGatesExecuteToCompletion (1.10s)
    --- PASS: TestSliceChildSelectedGatesExecuteToCompletion/full (0.56s)
    --- PASS: TestSliceChildSelectedGatesExecuteToCompletion/custom (0.54s)
=== RUN   TestOrdinaryGateRoutesCompleteWithoutQA
=== RUN   TestOrdinaryGateRoutesCompleteWithoutQA/default
=== RUN   TestOrdinaryGateRoutesCompleteWithoutQA/custom
--- PASS: TestOrdinaryGateRoutesCompleteWithoutQA (2.96s)
    --- PASS: TestOrdinaryGateRoutesCompleteWithoutQA/default (1.53s)
    --- PASS: TestOrdinaryGateRoutesCompleteWithoutQA/custom (1.43s)
=== RUN   TestDefaultRouteRequiresConfirmedDevelopmentPlan
--- PASS: TestDefaultRouteRequiresConfirmedDevelopmentPlan (0.22s)
PASS
ok  	formal-gates/internal/engine/facade	9.065s

````

## fixed-binary-build.json

来源：/tmp/installed-start-quality-137b0ba-evidence/fixed-binary-build.json；bytes=356；sha256=8f9af2ba3cc16a8b28aa30ee5a577840d16793f92268da52a30ee4e4a04a5824

````text
{
  "command": [
    "go",
    "build",
    "-o",
    "bin/formal-gates",
    "./cmd/formal-gates"
  ],
  "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/source",
  "exitCode": 0,
  "elapsedSeconds": 1.056,
  "log": "/private/tmp/installed-start-quality-137b0ba-evidence/fixed-binary-build.log",
  "realModelCalls": 0,
  "realModelCost": 0
}

````

## fixed-binary-build.log

来源：/tmp/installed-start-quality-137b0ba-evidence/fixed-binary-build.log；bytes=0；sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

````text

````

## installed.json

来源：/tmp/installed-start-quality-137b0ba-evidence/installed.json；bytes=383；sha256=d23a2e2a4c9ca58fcc9d87cbe325d1075ff526e3e800aefba98e6d351669e14d

````text
{
  "command": [
    "go",
    "test",
    "./internal/engine/acceptance",
    "-run",
    "^TestInstalled",
    "-count=1",
    "-v"
  ],
  "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/source",
  "exitCode": 0,
  "elapsedSeconds": 20.804,
  "log": "/private/tmp/installed-start-quality-137b0ba-evidence/installed.log",
  "realModelCalls": 0,
  "realModelCost": 0
}

````

## installed.log

来源：/tmp/installed-start-quality-137b0ba-evidence/installed.log；bytes=458；sha256=1938c279390b9d8bd8b2d1e631953b80f00e50e9dba725a60ef9ddebb5ca2e71

````text
=== RUN   TestInstalledRegularStartCarriesHostThroughPublicOperations
--- PASS: TestInstalledRegularStartCarriesHostThroughPublicOperations (8.81s)
=== RUN   TestInstalledStartRejectsInvalidInputsBeforeState
--- PASS: TestInstalledStartRejectsInvalidInputsBeforeState (3.13s)
=== RUN   TestInstalledLightweightAndRegularStartRecovery
--- PASS: TestInstalledLightweightAndRegularStartRecovery (3.45s)
PASS
ok  	formal-gates/internal/engine/acceptance	16.819s

````

## public-probe-completed.json

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-completed.json；bytes=370；sha256=10b4a0b88ecd24b42122abc8929d70df66a90def57a4025e876e338c0bba37b8

````text
{
  "command": [
    "python3",
    "/tmp/installed-start-quality-137b0ba-evidence/public_probe_continue.py"
  ],
  "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/source",
  "exitCode": 0,
  "elapsedSeconds": 3.855,
  "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-completed.log",
  "realModelCalls": 0,
  "realModelCost": 0
}

````

## public-probe-completed.log

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-completed.log；bytes=11315；sha256=c6072edbe9a6f57d3c2251078343150007fd719b572a19a330fe4c2076006880

````text
{"index": 19, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "show", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.136, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/19.log", "expectedExitCode": 0}
{"index": 20, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "status", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.075, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/20.log", "expectedExitCode": 0}
{"index": 21, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "next", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.075, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/21.log", "expectedExitCode": 0}
{"index": 22, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "diagnose", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.008, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/22.log", "expectedExitCode": 0}
{"index": 23, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "drive", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.143, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/23.log", "expectedExitCode": 0}
{"index": 24, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "submit", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 1, "elapsedSeconds": 0.075, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/24.log", "expectedExitCode": 1}
{"index": 25, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "submit", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.096, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/25.log", "expectedExitCode": 0}
{"index": 26, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "lifecycle", "capture", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--provider", "claude", "--event", "SubagentStart"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.015, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/26.log", "expectedExitCode": 0}
{"index": 27, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "lifecycle", "capture", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--provider", "claude", "--event", "SubagentStop"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.015, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/27.log", "expectedExitCode": 0}
{"index": 28, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "lifecycle", "verify", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native", "--dispatch", "act:review/review.worker"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.009, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/28.log", "expectedExitCode": 0}
{"index": 29, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/intake.json", "--route", "regular", "--base-snapshot", "1f4d2f4733d889cf531affcb0ee0927df8bf906f"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 1, "elapsedSeconds": 0.045, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/29.log", "expectedExitCode": 1}
{"index": 30, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "install", "--source", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/install-source", "--host", "codex", "--scope", "project", "--project", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--binary-target", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "--force", "--skip-hooks"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 2.353, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/30.log", "expectedExitCode": 0}
{"index": 31, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "install", "--source", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/install-source", "--host", "codex", "--scope", "project", "--project", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--binary-target", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "--force", "--skip-hooks", "--bootstrap"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.22, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/31.log", "expectedExitCode": 0}
{"index": 32, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "codex-native", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/intake.json", "--route", "regular", "--base-snapshot", "1f4d2f4733d889cf531affcb0ee0927df8bf906f"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.167, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/32.log", "expectedExitCode": 0}
{"index": 33, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "show", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.081, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/33.log", "expectedExitCode": 0}
{"index": 34, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "drive", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.077, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/34.log", "expectedExitCode": 0}
{"index": 35, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "show", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "codex-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.076, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/35.log", "expectedExitCode": 0}
{"index": 36, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "drive", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "codex-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.132, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/36.log", "expectedExitCode": 0}
{"index": 37, "command": ["git", "rev-parse", "HEAD"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.015, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/37.log", "expectedExitCode": 0}
PASS: actual installed alias host, native admission, public reopen/drive/submit, lifecycle claim, independent rejection side effects, sibling host identity

````

## public-probe-continued.json

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-continued.json；bytes=370；sha256=cb1dc2ae751bb564e810625c646dfd2a93ac1dd6a77fb9a5a0dc5818092898fc

````text
{
  "command": [
    "python3",
    "/tmp/installed-start-quality-137b0ba-evidence/public_probe_continue.py"
  ],
  "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/source",
  "exitCode": 1,
  "elapsedSeconds": 0.047,
  "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-continued.log",
  "realModelCalls": 0,
  "realModelCost": 0
}

````

## public-probe-continued.log

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-continued.log；bytes=2741；sha256=09aa349d696186e5f7fefb34b374905ebb7f32e7bd6bea5880737557f054fb59

````text
Traceback (most recent call last):
  File "/tmp/installed-start-quality-137b0ba-evidence/public_probe_continue.py", line 58, in <module>
    request,state=check_binding('claude-native','claude-code')
                  ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/tmp/installed-start-quality-137b0ba-evidence/public_probe_continue.py", line 52, in check_binding
    for name in ['prompts','gates','hooks']: assert (Path(rec['target'])/name).is_dir(),(rec,name)
                                                    ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
AssertionError: ({'id': 'claude-global-e25a68aeac05', 'target': '/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.claude/skills/formal-gates', 'launcherPath': '/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates', 'scope': 'global', 'host': 'claude', 'hookConfig': '/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.claude/settings.json', 'skipHooks': True, 'projectRoot': '/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home', 'stateRoot': '/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.gates', 'resourceRoot': '/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.formal-gates-resources', 'runtimeSibling': '/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.claude/skills', 'vcsIdentity': 'package:284d0f07088af5a84bcc0b08d274af0d4f67a182bac80ee1c29e66f06e7cedd7', 'packageDigest': '284d0f07088af5a84bcc0b08d274af0d4f67a182bac80ee1c29e66f06e7cedd7', 'installedDigest': 'a2da9d95fe0177bc17b815fc55698801d0525f4688caa2ca4c2303ca1fae0514', 'canonicalPaths': {'hookConfig': '/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.claude/settings.json', 'launcher': '/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates', 'projectRoot': '/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home', 'resourceRoot': '/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.formal-gates-resources', 'runtimeSibling': '/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.claude/skills', 'stateRoot': '/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.gates', 'target': '/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.claude/skills/formal-gates'}, 'status': 'active', 'generation': 1, 'lease': 'lease-1', 'token': 'token-1791036157162740000'}, 'hooks')

````

## public-probe-verified.json

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified.json；bytes=360；sha256=7ae4055156a32fc0cb5a376176e3dcc9f00840d1cca19e1e323563f667b0f819

````text
{
  "command": [
    "python3",
    "/tmp/installed-start-quality-137b0ba-evidence/public_probe.py"
  ],
  "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/source",
  "exitCode": 1,
  "elapsedSeconds": 3.623,
  "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified.log",
  "realModelCalls": 0,
  "realModelCost": 0
}

````

## public-probe-verified.log

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified.log；bytes=8982；sha256=fde1ad57bbe487d502dddfa1fe87056bdd955806174d4e989f595ba1f71bc89b

````text
{"index": 1, "command": ["git", "init", "-q"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.18, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/01.log", "expectedExitCode": 0}
{"index": 2, "command": ["git", "config", "user.name", "Independent application"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.022, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/02.log", "expectedExitCode": 0}
{"index": 3, "command": ["git", "config", "user.email", "application@example.invalid"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.041, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/03.log", "expectedExitCode": 0}
{"index": 4, "command": ["git", "add", "."], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.03, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/04.log", "expectedExitCode": 0}
{"index": 5, "command": ["git", "commit", "-qm", "Application"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.038, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/05.log", "expectedExitCode": 0}
{"index": 6, "command": ["git", "rev-parse", "HEAD"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.025, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/06.log", "expectedExitCode": 0}
{"index": 7, "command": ["git", "symbolic-ref", "--short", "HEAD"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.025, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/07.log", "expectedExitCode": 0}
{"index": 8, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "not-installed", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/intake.json", "--route", "regular", "--base-snapshot", "1f4d2f4733d889cf531affcb0ee0927df8bf906f"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 1, "elapsedSeconds": 1.032, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/08.log", "expectedExitCode": 1}
{"index": 9, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "install", "--source", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/install-source", "--host", "claude", "--scope", "global", "--binary-target", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "--force", "--skip-hooks"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 1.051, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/09.log", "expectedExitCode": 0}
{"index": 10, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "install", "--source", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/install-source", "--host", "claude", "--scope", "global", "--binary-target", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "--force", "--skip-hooks", "--bootstrap"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.247, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/10.log", "expectedExitCode": 0}
{"index": 11, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "missing-base", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/intake.json", "--route", "regular"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 1, "elapsedSeconds": 0.052, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/11.log", "expectedExitCode": 1}
{"index": 12, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "mutable-base", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/intake.json", "--route", "regular", "--base-snapshot", "HEAD"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 1, "elapsedSeconds": 0.091, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/12.log", "expectedExitCode": 1}
{"index": 13, "command": ["git", "checkout", "--orphan", "unrelated", "-q"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.019, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/13.log", "expectedExitCode": 0}
{"index": 14, "command": ["git", "commit", "-qm", "Unrelated application"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.028, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/14.log", "expectedExitCode": 0}
{"index": 15, "command": ["git", "rev-parse", "HEAD"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.016, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/15.log", "expectedExitCode": 0}
{"index": 16, "command": ["git", "checkout", "-q", "main"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.02, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/16.log", "expectedExitCode": 0}
{"index": 17, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "unrelated-base", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/intake.json", "--route", "regular", "--base-snapshot", "da011a5db74513f11d4585322e9f773e622dc657"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 1, "elapsedSeconds": 0.138, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/17.log", "expectedExitCode": 1}
{"index": 18, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/intake.json", "--route", "regular", "--base-snapshot", "1f4d2f4733d889cf531affcb0ee0927df8bf906f"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.173, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/18.log", "expectedExitCode": 0}
Traceback (most recent call last):
  File "/tmp/installed-start-quality-137b0ba-evidence/public_probe.py", line 80, in <module>
    request,state=check_binding('claude-native','claude-code')
                  ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/tmp/installed-start-quality-137b0ba-evidence/public_probe.py", line 52, in check_binding
    assert rec['host']==provider
           ^^^^^^^^^^^^^^^^^^^^^
AssertionError

````

## public-probe.json

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe.json；bytes=351；sha256=f315874085b2980cb01fcb5ca27182dc35fe4eec34feecc0fcd7dcd52a6fb7b6

````text
{
  "command": [
    "python3",
    "/tmp/installed-start-quality-137b0ba-evidence/public_probe.py"
  ],
  "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/source",
  "exitCode": 1,
  "elapsedSeconds": 5.144,
  "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe.log",
  "realModelCalls": 0,
  "realModelCost": 0
}

````

## public-probe.log

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe.log；bytes=8360；sha256=d9ad6694ec730a498a83292bcb529cc9a0eaafee56bd0901ac703484392fe63a

````text
{"index": 1, "command": ["git", "init", "-q"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "exitCode": 0, "elapsedSeconds": 0.187, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/01.log", "expectedExitCode": 0}
{"index": 2, "command": ["git", "config", "user.name", "Independent application"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "exitCode": 0, "elapsedSeconds": 0.015, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/02.log", "expectedExitCode": 0}
{"index": 3, "command": ["git", "config", "user.email", "application@example.invalid"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "exitCode": 0, "elapsedSeconds": 0.015, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/03.log", "expectedExitCode": 0}
{"index": 4, "command": ["git", "add", "."], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "exitCode": 0, "elapsedSeconds": 0.017, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/04.log", "expectedExitCode": 0}
{"index": 5, "command": ["git", "commit", "-qm", "Application"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "exitCode": 0, "elapsedSeconds": 0.024, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/05.log", "expectedExitCode": 0}
{"index": 6, "command": ["git", "rev-parse", "HEAD"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "exitCode": 0, "elapsedSeconds": 0.015, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/06.log", "expectedExitCode": 0}
{"index": 7, "command": ["git", "symbolic-ref", "--short", "HEAD"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "exitCode": 0, "elapsedSeconds": 0.016, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/07.log", "expectedExitCode": 0}
{"index": 8, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "--run-id", "not-installed", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/intake.json", "--route", "regular", "--base-snapshot", "3ac4674afb0314365f90069588eb6e1c51754920"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "exitCode": 1, "elapsedSeconds": 2.969, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/08.log", "expectedExitCode": 1}
{"index": 9, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/host-home/.local/bin/formal-gates", "install", "--source", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/install-source", "--host", "claude", "--scope", "global", "--binary-target", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/host-home/.local/bin/formal-gates", "--force", "--skip-hooks"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "exitCode": 0, "elapsedSeconds": 1.01, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/09.log", "expectedExitCode": 0}
{"index": 10, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/host-home/.local/bin/formal-gates", "install", "--source", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/install-source", "--host", "claude", "--scope", "global", "--binary-target", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/host-home/.local/bin/formal-gates", "--force", "--skip-hooks", "--bootstrap"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "exitCode": 0, "elapsedSeconds": 0.216, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/10.log", "expectedExitCode": 0}
{"index": 11, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "--run-id", "missing-base", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/intake.json", "--route", "regular"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "exitCode": 1, "elapsedSeconds": 0.05, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/11.log", "expectedExitCode": 1}
{"index": 12, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "--run-id", "mutable-base", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/intake.json", "--route", "regular", "--base-snapshot", "HEAD"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "exitCode": 1, "elapsedSeconds": 0.073, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/12.log", "expectedExitCode": 1}
{"index": 13, "command": ["git", "checkout", "--orphan", "unrelated", "-q"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "exitCode": 0, "elapsedSeconds": 0.018, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/13.log", "expectedExitCode": 0}
{"index": 14, "command": ["git", "commit", "-qm", "Unrelated application"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "exitCode": 0, "elapsedSeconds": 0.025, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/14.log", "expectedExitCode": 0}
{"index": 15, "command": ["git", "rev-parse", "HEAD"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "exitCode": 0, "elapsedSeconds": 0.015, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/15.log", "expectedExitCode": 0}
{"index": 16, "command": ["git", "checkout", "-q", "main"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "exitCode": 0, "elapsedSeconds": 0.017, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/16.log", "expectedExitCode": 0}
{"index": 17, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "--run-id", "unrelated-base", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/intake.json", "--route", "regular", "--base-snapshot", "8fee2c39141a03fca5e790037d552193a9d6d02b"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/application", "exitCode": 1, "elapsedSeconds": 0.121, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe/17.log", "expectedExitCode": 1}
Traceback (most recent call last):
  File "/tmp/installed-start-quality-137b0ba-evidence/public_probe.py", line 77, in <module>
    start('unrelated-base',other,expected=1,contains='not an ancestor')
  File "/tmp/installed-start-quality-137b0ba-evidence/public_probe.py", line 41, in start
    return cli(*args,expected=expected,contains=contains)
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/tmp/installed-start-quality-137b0ba-evidence/public_probe.py", line 35, in cli
    return call([B,*a],inp=inp,expected=expected,contains=contains,cwd=cwd)
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/tmp/installed-start-quality-137b0ba-evidence/public_probe.py", line 30, in call
    if contains: assert contains in out,(contains,out)
                        ^^^^^^^^^^^^^^^
AssertionError: ('not an ancestor', 'base snapshot 8fee2c39141a03fca5e790037d552193a9d6d02b: cannot resolve git ancestry of 8fee2c39141a03fca5e790037d552193a9d6d02b: git: exit status 1\n')

````

## admission-with-artifact.json

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/admission-with-artifact.json；bytes=380；sha256=687c6e6c0a31ed7ffc8a698368b1e452b6fb27635fda0bf55dbed264c6a54baa

````text
{
  "command": [
    "go",
    "test",
    "./internal/validate",
    "-run",
    "^Test(ResolveEngineAdmissionChoosesProjectBinding|EngineAdmissionNormalizesRegisteredHostAliases)$",
    "-count=1",
    "-v"
  ],
  "cwd": "/tmp/installed-start-complexity-137b0ba-independent/src",
  "exit_code": 0,
  "elapsed_seconds": 2.915488083846867,
  "actual_model_calls": 0,
  "cost": 0
}
````

## admission-with-artifact.stderr

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/admission-with-artifact.stderr；bytes=0；sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

````text

````

## admission-with-artifact.stdout

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/admission-with-artifact.stdout；bytes=2242；sha256=f4b86a50c75ba77ddd4c413d98e5a21605e342bc149ec300ab8237d259ea7bd5

````text
=== RUN   TestResolveEngineAdmissionChoosesProjectBinding
--- PASS: TestResolveEngineAdmissionChoosesProjectBinding (0.26s)
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude-code
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude_code
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/codex/codex
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/cursor/cursor
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/dsh
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek-harness
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek_harness
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/zcode
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/z-code
--- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases (0.93s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude-code (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude_code (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/codex/codex (0.09s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/cursor/cursor (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/dsh (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek-harness (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek_harness (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/zcode (0.08s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/z-code (0.08s)
PASS
ok  	formal-gates/internal/validate	2.155s

````

## admission.json

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/admission.json；bytes=380；sha256=1d9e729d8f2e14e5f9a98c584d51dc9980f936f5b3076130adae4d7155f9d8f4

````text
{
  "command": [
    "go",
    "test",
    "./internal/validate",
    "-run",
    "^Test(ResolveEngineAdmissionChoosesProjectBinding|EngineAdmissionNormalizesRegisteredHostAliases)$",
    "-count=1",
    "-v"
  ],
  "cwd": "/tmp/installed-start-complexity-137b0ba-independent/src",
  "exit_code": 1,
  "elapsed_seconds": 6.699920707847923,
  "actual_model_calls": 0,
  "cost": 0
}
````

## admission.stderr

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/admission.stderr；bytes=0；sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

````text

````

## admission.stdout

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/admission.stdout；bytes=7148；sha256=ffdbdd36cb901e7f57eea51f3801b2bd1c0999376675f6df29b95d4e4ca4f20d

````text
=== RUN   TestResolveEngineAdmissionChoosesProjectBinding
    engine_admission_test.go:58: resolve /var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestResolveEngineAdmissionChoosesProjectBinding1953090789/002: UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-complexity-137b0ba-independent/src
--- FAIL: TestResolveEngineAdmissionChoosesProjectBinding (0.05s)
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude
    engine_admission_test.go:127: host " CLAUDE ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-complexity-137b0ba-independent/src
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude-code
    engine_admission_test.go:127: host " CLAUDE-CODE ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-complexity-137b0ba-independent/src
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude_code
    engine_admission_test.go:127: host " CLAUDE CODE ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-complexity-137b0ba-independent/src
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/codex/codex
    engine_admission_test.go:127: host " CODEX ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-complexity-137b0ba-independent/src
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/cursor/cursor
    engine_admission_test.go:127: host " CURSOR ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-complexity-137b0ba-independent/src
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/dsh
    engine_admission_test.go:127: host " DSH ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-complexity-137b0ba-independent/src
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek
    engine_admission_test.go:127: host " DEEPSEEK ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-complexity-137b0ba-independent/src
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek-harness
    engine_admission_test.go:127: host " DEEPSEEK-HARNESS ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-complexity-137b0ba-independent/src
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek_harness
    engine_admission_test.go:127: host " DEEPSEEK HARNESS ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-complexity-137b0ba-independent/src
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/zcode
    engine_admission_test.go:127: host " ZCODE ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-complexity-137b0ba-independent/src
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/z-code
    engine_admission_test.go:127: host " Z-CODE ": admission={RegistryPath: RecordID: Target: Provider: PackageDigest: Generation:0 Lease: Token:} err=UNREGISTERED_INSTALL: candidate target identity cannot be verified: UNREGISTERED_INSTALL: registered target is not an installed artifact: formal-gates source is incomplete; missing bin under /private/tmp/installed-start-complexity-137b0ba-independent/src
--- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases (0.06s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude-code (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude_code (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/codex/codex (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/cursor/cursor (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/dsh (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek-harness (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek_harness (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/zcode (0.00s)
    --- FAIL: TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/z-code (0.00s)
FAIL
FAIL	formal-gates/internal/validate	0.625s
FAIL

````

## independent-public-executed.json

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public-executed.json；bytes=278；sha256=541194098d1f46abe6486606f24cf16e5d9ed17a5d24520bcb08ef83bacf4a62

````text
{
  "command": [
    "python3",
    "/tmp/installed-start-complexity-137b0ba-independent/public_probe.py"
  ],
  "cwd": "/tmp/installed-start-complexity-137b0ba-independent/src",
  "exit_code": 1,
  "elapsed_seconds": 0.8264743331819773,
  "actual_model_calls": 0,
  "cost": 0
}
````

## independent-public-executed.stderr

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public-executed.stderr；bytes=1152；sha256=bdeb1a792b93baede79117ba4f93fb5c9ffeb68d21beb7742b5f0f660752fb80

````text
Traceback (most recent call last):
  File "/tmp/installed-start-complexity-137b0ba-independent/public_probe.py", line 33, in <module>
    call('install',[root/'src/bin/formal-gates',*install])
  File "/tmp/installed-start-complexity-137b0ba-independent/public_probe.py", line 16, in call
    assert (proc.returncode==0)==(want==0),result
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
AssertionError: {'label': 'install', 'argv': ['/tmp/installed-start-complexity-137b0ba-independent/src/bin/formal-gates', 'install', '--source', '/tmp/installed-start-complexity-137b0ba-independent/src', '--host', 'zcode', '--scope', 'global', '--binary-target', '/tmp/installed-start-complexity-137b0ba-independent/public-artifacts-executed/home/.local/bin/formal-gates', '--force', '--skip-hooks'], 'cwd': '/tmp/installed-start-complexity-137b0ba-independent/public-artifacts-executed/project', 'exit_code': 1, 'elapsed_seconds': 0.5463127917610109, 'stdout': '', 'stderr': 'UNREGISTERED_INSTALL: install maintenance must run through stable launcher /private/tmp/installed-start-complexity-137b0ba-independent/public-artifacts-executed/home/.local/bin/formal-gates\n'}

````

## independent-public-executed.stdout

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public-executed.stdout；bytes=0；sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

````text

````

## independent-public-installed.json

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public-installed.json；bytes=277；sha256=b4bb149366b098b5f37015c05dc5f34f03826c9135fdd930fe7234e53888f7a5

````text
{
  "command": [
    "python3",
    "/tmp/installed-start-complexity-137b0ba-independent/public_probe.py"
  ],
  "cwd": "/tmp/installed-start-complexity-137b0ba-independent/src",
  "exit_code": 0,
  "elapsed_seconds": 3.070054749958217,
  "actual_model_calls": 0,
  "cost": 0
}
````

## independent-public-installed.stderr

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public-installed.stderr；bytes=0；sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

````text

````

## independent-public-installed.stdout

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public-installed.stdout；bytes=168；sha256=5599e7e35a153af436897448ea0684560ca018a408f9f5958d0d06ea324bf779

````text
{"status": "PASS", "publicCommands": 18, "artifacts": "/tmp/installed-start-complexity-137b0ba-independent/public-artifacts-installed", "realModelCalls": 0, "cost": 0}

````

## independent-public.json

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public.json；bytes=278；sha256=fae502cd9a6920bebd4360fd1a06857f50e012260993b0f40846554c740392ca

````text
{
  "command": [
    "python3",
    "/tmp/installed-start-complexity-137b0ba-independent/public_probe.py"
  ],
  "cwd": "/tmp/installed-start-complexity-137b0ba-independent/src",
  "exit_code": 1,
  "elapsed_seconds": 0.3409428750164807,
  "actual_model_calls": 0,
  "cost": 0
}
````

## independent-public.stderr

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public.stderr；bytes=580；sha256=5d539370df1aeb6eea83925777e579e5b0b36af711af12826f3b5fe5217e4e64

````text
Traceback (most recent call last):
  File "/tmp/installed-start-complexity-137b0ba-independent/public_probe.py", line 28, in <module>
    for key,name in [('source','DefaultIntakeSource'),('authority','DefaultIntakeAuthority'),('transport','DefaultIntakeTransport')]:intake[key]=re.search(name+r'\s*=\s*"([^"]+)"',text).group(1)
                                                                                                                                                 ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
AttributeError: 'NoneType' object has no attribute 'group'

````

## independent-public.stdout

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public.stdout；bytes=0；sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

````text

````

## installed-start.json

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/installed-start.json；bytes=427；sha256=db6a88fabe6dd985474c3e78a765e307c5465d6189004a36d295f70c34b233d0

````text
{
  "command": [
    "go",
    "test",
    "./internal/engine/acceptance",
    "-run",
    "^TestInstalled(RegularStartCarriesHostThroughPublicOperations|StartRejectsInvalidInputsBeforeState|LightweightAndRegularStartRecovery)$",
    "-count=1",
    "-v"
  ],
  "cwd": "/tmp/installed-start-complexity-137b0ba-independent/src",
  "exit_code": 0,
  "elapsed_seconds": 19.582926083821803,
  "actual_model_calls": 0,
  "cost": 0
}
````

## installed-start.stderr

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/installed-start.stderr；bytes=0；sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

````text

````

## installed-start.stdout

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/installed-start.stdout；bytes=458；sha256=cb01a580e7c2d8320c6578348f9422dc4119e0fe7fa97814cb35fb7bf2358233

````text
=== RUN   TestInstalledRegularStartCarriesHostThroughPublicOperations
--- PASS: TestInstalledRegularStartCarriesHostThroughPublicOperations (8.88s)
=== RUN   TestInstalledStartRejectsInvalidInputsBeforeState
--- PASS: TestInstalledStartRejectsInvalidInputsBeforeState (2.85s)
=== RUN   TestInstalledLightweightAndRegularStartRecovery
--- PASS: TestInstalledLightweightAndRegularStartRecovery (3.65s)
PASS
ok  	formal-gates/internal/engine/acceptance	15.858s

````

## regression-names.json

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/regression-names.json；bytes=570；sha256=4192c13fa2fabf1fc46c758ee2b63728edb1639aefaa9d6d4777c32d0ef918b8

````text
{
  "command": [
    "rg",
    "-n",
    "^func Test",
    "internal/engine/facade/sliced_runs_test.go",
    "internal/engine/facade/validation_gate_route_test.go",
    "internal/engine/facade/snapshot_wiring_test.go",
    "internal/engine/protocol/recovery_lifecycle_test.go",
    "internal/engine/facade/validation_lifecycle_test.go",
    "internal/engine/acceptance/installed_harness_test.go"
  ],
  "cwd": "/tmp/installed-start-complexity-137b0ba-independent/src",
  "exit_code": 0,
  "elapsed_seconds": 0.018540082965046167,
  "actual_model_calls": 0,
  "cost": 0
}
````

## regression-names.stderr

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/regression-names.stderr；bytes=0；sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

````text

````

## regression-names.stdout

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/regression-names.stdout；bytes=5393；sha256=9a9ecccf5f856620adfb35589d5f20b06ea2177f2e13483595454cef166a25fa

````text
internal/engine/facade/validation_gate_route_test.go:12:func TestOrdinaryGateRoutesCompleteWithoutQA(t *testing.T) {
internal/engine/facade/validation_gate_route_test.go:96:func TestDefaultRouteRequiresConfirmedDevelopmentPlan(t *testing.T) {
internal/engine/facade/validation_lifecycle_test.go:48:func TestTestOnlyRedesignKeepsIndependentTaskAndReviewIdentity(t *testing.T) {
internal/engine/facade/validation_lifecycle_test.go:115:func TestRetryOfferSurvivesIndependentProgressAndReopen(t *testing.T) {
internal/engine/facade/validation_lifecycle_test.go:173:func TestDismissedFindingPreservesFailedQAEvidenceUntilDisposition(t *testing.T) {
internal/engine/facade/validation_lifecycle_test.go:268:func TestGateInputExcludesQAMaterialsAcrossReopen(t *testing.T) {
internal/engine/facade/sliced_runs_test.go:210:func TestSplitTopologyBindsACOwnersAndLocksMergeRoute(t *testing.T) {
internal/engine/facade/sliced_runs_test.go:273:func TestSplitBindingRequiresNoPendingRouteAsk(t *testing.T) {
internal/engine/facade/sliced_runs_test.go:294:func TestSliceChildStartValidations(t *testing.T) {
internal/engine/facade/sliced_runs_test.go:372:func TestSliceChildInheritsAndSelectsRoute(t *testing.T) {
internal/engine/facade/sliced_runs_test.go:445:func TestSliceChildBoundaryFollowsRouteSelection(t *testing.T) {
internal/engine/facade/sliced_runs_test.go:470:func TestSliceChildParallelClaimsAreMutuallyExclusive(t *testing.T) {
internal/engine/facade/sliced_runs_test.go:503:func TestSliceChildClaimSurvivesInterruptedStart(t *testing.T) {
internal/engine/facade/sliced_runs_test.go:534:func TestSliceChildConfirmsInheritedPlanIntoDevelopmentDispatch(t *testing.T) {
internal/engine/facade/sliced_runs_test.go:576:func TestSliceChildRouteWithoutPlanIsRejected(t *testing.T) {
internal/engine/facade/sliced_runs_test.go:609:func TestSliceChildStartRebuildsAfterRequestOnlyPersistence(t *testing.T) {
internal/engine/facade/sliced_runs_test.go:652:func TestSliceClaimIsFrozenByRequirementBarrier(t *testing.T) {
internal/engine/facade/sliced_runs_test.go:686:func TestSliceChildRequiresExplicitRegularRoute(t *testing.T) {
internal/engine/facade/sliced_runs_test.go:707:func TestSplitTopologyRequiresDistinctSliceNames(t *testing.T) {
internal/engine/facade/sliced_runs_test.go:728:func TestSliceChildPlanSwapRequiresPerBatchUserDecisions(t *testing.T) {
internal/engine/facade/sliced_runs_test.go:818:func TestSliceChildSelectedGatesExecuteToCompletion(t *testing.T) {
internal/engine/acceptance/installed_harness_test.go:69:func TestAcceptanceInstalledProtocolHarness(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:104:func TestRegularStartAcceptsAncestorBaseAndRejectsDivergent(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:166:func TestCandidatePromotionSquashesMultiCommitRangeAndCompletesTerminal(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:293:func TestCandidatePromotionNoOpConfirmsSingleCommit(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:337:func TestPrepareCandidateRejectsUncommittedChanges(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:375:func TestRegisterQAWorktreeValidatesIdentityAndArtifacts(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:418:func TestQAWorktreeClearsWhenRouteDropsBlackbox(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:479:func TestQAWorktreeClearsWhenBlackboxSetFreezes(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:506:func TestPrepareCandidateChainPassesWithoutGitignore(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:597:func TestPromotionFrozenByRequirementChangeBarrierLeavesHistoryUntouched(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:640:func TestTerminalRunRejectsPromotionWithoutTouchingHistory(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:678:func TestTerminalSealingRejectsDirtyTreeAndDriftUntilReorganized(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:726:func TestPromotionIntentSurvivesExecuteCrashAndReentryConverges(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:786:func TestTerminalCheckpointDriftRejectsPublicationUntilContentRestored(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:862:func TestTerminalPublicationRejectsDriftAfterSealVerification(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:948:func TestRequirementChangeAdmissionWaitsForThePromotionWindow(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:1074:func TestAbortDecisionAdmissionWaitsForThePromotionWindow(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:1173:func TestPromotionFrozenByAbortDecisionLeavesHistoryUntouched(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:1223:func TestPromotionResumeAfterResetInterruptionBindsOrganizedHead(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:1301:func TestPromotionResumeAfterCommitFailureBindsOrganizedHead(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:1368:func TestPromotionRejectsUserWorkspaceChangesUntilCleaned(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:1437:func TestPromotionBindsParkedOrganizationAfterBarrierRelease(t *testing.T) {
internal/engine/facade/snapshot_wiring_test.go:1504:func TestPromotionContentDriftWithLiveIntentRejectsBeforeGitWrites(t *testing.T) {

````

## related-regressions.json

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/related-regressions.json；bytes=651；sha256=87352b4c956be2cfb3c28f54126b85523d4305541d2dad7c8cf5214f577cbf88

````text
{
  "command": [
    "go",
    "test",
    "./internal/engine/facade",
    "-run",
    "^Test(OrdinaryGateRoutesCompleteWithoutQA|SliceChildInheritsAndSelectsRoute|SliceChildSelectedGatesExecuteToCompletion|SliceChildStartRebuildsAfterRequestOnlyPersistence|RegularStartAcceptsAncestorBaseAndRejectsDivergent|CostSourceBridgeReportsUnsupportedHostUnavailable|CostSourceBridgeHoldsObservableHostPending|CostSourceBridgeReportsMissingBindingUnavailable)$",
    "-count=1",
    "-v"
  ],
  "cwd": "/tmp/installed-start-complexity-137b0ba-independent/src",
  "exit_code": 0,
  "elapsed_seconds": 8.545681208837777,
  "actual_model_calls": 0,
  "cost": 0
}
````

## related-regressions.stderr

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/related-regressions.stderr；bytes=0；sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

````text

````

## related-regressions.stdout

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/related-regressions.stdout；bytes=2443；sha256=1e3f2248dea97100985d609a978a839d685f4b1ae86c0ad9c43a9c8d02e3113c

````text
=== RUN   TestCostSourceBridgeReportsUnsupportedHostUnavailable
--- PASS: TestCostSourceBridgeReportsUnsupportedHostUnavailable (0.00s)
=== RUN   TestCostSourceBridgeHoldsObservableHostPending
--- PASS: TestCostSourceBridgeHoldsObservableHostPending (0.00s)
=== RUN   TestCostSourceBridgeReportsMissingBindingUnavailable
--- PASS: TestCostSourceBridgeReportsMissingBindingUnavailable (0.00s)
=== RUN   TestSliceChildInheritsAndSelectsRoute
--- PASS: TestSliceChildInheritsAndSelectsRoute (0.44s)
=== RUN   TestSliceChildStartRebuildsAfterRequestOnlyPersistence
--- PASS: TestSliceChildStartRebuildsAfterRequestOnlyPersistence (0.34s)
=== RUN   TestSliceChildSelectedGatesExecuteToCompletion
=== RUN   TestSliceChildSelectedGatesExecuteToCompletion/full
=== RUN   TestSliceChildSelectedGatesExecuteToCompletion/custom
--- PASS: TestSliceChildSelectedGatesExecuteToCompletion (1.14s)
    --- PASS: TestSliceChildSelectedGatesExecuteToCompletion/full (0.59s)
    --- PASS: TestSliceChildSelectedGatesExecuteToCompletion/custom (0.55s)
=== RUN   TestRegularStartAcceptsAncestorBaseAndRejectsDivergent
=== RUN   TestRegularStartAcceptsAncestorBaseAndRejectsDivergent/head_as_base
=== RUN   TestRegularStartAcceptsAncestorBaseAndRejectsDivergent/divergent_base
=== RUN   TestRegularStartAcceptsAncestorBaseAndRejectsDivergent/malformed_base
=== RUN   TestRegularStartAcceptsAncestorBaseAndRejectsDivergent/foreign_snapshot
=== RUN   TestRegularStartAcceptsAncestorBaseAndRejectsDivergent/ancestor_base
--- PASS: TestRegularStartAcceptsAncestorBaseAndRejectsDivergent (0.82s)
    --- PASS: TestRegularStartAcceptsAncestorBaseAndRejectsDivergent/head_as_base (0.18s)
    --- PASS: TestRegularStartAcceptsAncestorBaseAndRejectsDivergent/divergent_base (0.08s)
    --- PASS: TestRegularStartAcceptsAncestorBaseAndRejectsDivergent/malformed_base (0.03s)
    --- PASS: TestRegularStartAcceptsAncestorBaseAndRejectsDivergent/foreign_snapshot (0.06s)
    --- PASS: TestRegularStartAcceptsAncestorBaseAndRejectsDivergent/ancestor_base (0.17s)
=== RUN   TestOrdinaryGateRoutesCompleteWithoutQA
=== RUN   TestOrdinaryGateRoutesCompleteWithoutQA/default
=== RUN   TestOrdinaryGateRoutesCompleteWithoutQA/custom
--- PASS: TestOrdinaryGateRoutesCompleteWithoutQA (3.07s)
    --- PASS: TestOrdinaryGateRoutesCompleteWithoutQA/default (1.63s)
    --- PASS: TestOrdinaryGateRoutesCompleteWithoutQA/custom (1.44s)
PASS
ok  	formal-gates/internal/engine/facade	7.005s

````

## writer-isolation.json

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/writer-isolation.json；bytes=463；sha256=556bc084229f0dde92eb16990b28a6f829679e689bc6e14672699c4d65bd270c

````text
{
  "command": [
    "go",
    "test",
    "./internal/cli",
    "-run",
    "^Test(LegacyWritersCannotReachEngineRunState|LegacyWriteCommandsRejectEngineRunPerCommand|PublicCleanupCannotOperateOnActiveEngineRun|AlternatingLegacyAndEngineEntriesKeepBindingLocked)$",
    "-count=1",
    "-v"
  ],
  "cwd": "/tmp/installed-start-complexity-137b0ba-independent/src",
  "exit_code": 0,
  "elapsed_seconds": 6.648605625145137,
  "actual_model_calls": 0,
  "cost": 0
}
````

## writer-isolation.stderr

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/writer-isolation.stderr；bytes=0；sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855

````text

````

## writer-isolation.stdout

来源：/tmp/installed-start-complexity-137b0ba-independent/logs/writer-isolation.stdout；bytes=545；sha256=1ebc22ff4f80a2ab370114300d311af320460d58160a24410d19936ee4c4403e

````text
=== RUN   TestLegacyWritersCannotReachEngineRunState
--- PASS: TestLegacyWritersCannotReachEngineRunState (0.88s)
=== RUN   TestLegacyWriteCommandsRejectEngineRunPerCommand
--- PASS: TestLegacyWriteCommandsRejectEngineRunPerCommand (0.59s)
=== RUN   TestPublicCleanupCannotOperateOnActiveEngineRun
--- PASS: TestPublicCleanupCannotOperateOnActiveEngineRun (0.24s)
=== RUN   TestAlternatingLegacyAndEngineEntriesKeepBindingLocked
--- PASS: TestAlternatingLegacyAndEngineEntriesKeepBindingLocked (0.22s)
PASS
ok  	formal-gates/internal/cli	2.545s

````

## commands.jsonl

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/commands.jsonl；bytes=19708；sha256=2b2debe213c795bf04c45a75122bfbb4f7e3799ff1fc17781be88370c51657a3

````text
{"index": 1, "command": ["git", "init", "-q"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.18, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/01.log", "expectedExitCode": 0}
{"index": 2, "command": ["git", "config", "user.name", "Independent application"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.022, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/02.log", "expectedExitCode": 0}
{"index": 3, "command": ["git", "config", "user.email", "application@example.invalid"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.041, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/03.log", "expectedExitCode": 0}
{"index": 4, "command": ["git", "add", "."], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.03, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/04.log", "expectedExitCode": 0}
{"index": 5, "command": ["git", "commit", "-qm", "Application"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.038, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/05.log", "expectedExitCode": 0}
{"index": 6, "command": ["git", "rev-parse", "HEAD"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.025, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/06.log", "expectedExitCode": 0}
{"index": 7, "command": ["git", "symbolic-ref", "--short", "HEAD"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.025, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/07.log", "expectedExitCode": 0}
{"index": 8, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "not-installed", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/intake.json", "--route", "regular", "--base-snapshot", "1f4d2f4733d889cf531affcb0ee0927df8bf906f"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 1, "elapsedSeconds": 1.032, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/08.log", "expectedExitCode": 1}
{"index": 9, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "install", "--source", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/install-source", "--host", "claude", "--scope", "global", "--binary-target", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "--force", "--skip-hooks"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 1.051, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/09.log", "expectedExitCode": 0}
{"index": 10, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "install", "--source", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/install-source", "--host", "claude", "--scope", "global", "--binary-target", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "--force", "--skip-hooks", "--bootstrap"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.247, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/10.log", "expectedExitCode": 0}
{"index": 11, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "missing-base", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/intake.json", "--route", "regular"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 1, "elapsedSeconds": 0.052, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/11.log", "expectedExitCode": 1}
{"index": 12, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "mutable-base", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/intake.json", "--route", "regular", "--base-snapshot", "HEAD"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 1, "elapsedSeconds": 0.091, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/12.log", "expectedExitCode": 1}
{"index": 13, "command": ["git", "checkout", "--orphan", "unrelated", "-q"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.019, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/13.log", "expectedExitCode": 0}
{"index": 14, "command": ["git", "commit", "-qm", "Unrelated application"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.028, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/14.log", "expectedExitCode": 0}
{"index": 15, "command": ["git", "rev-parse", "HEAD"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.016, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/15.log", "expectedExitCode": 0}
{"index": 16, "command": ["git", "checkout", "-q", "main"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.02, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/16.log", "expectedExitCode": 0}
{"index": 17, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "unrelated-base", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/intake.json", "--route", "regular", "--base-snapshot", "da011a5db74513f11d4585322e9f773e622dc657"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 1, "elapsedSeconds": 0.138, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/17.log", "expectedExitCode": 1}
{"index": 18, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/intake.json", "--route", "regular", "--base-snapshot", "1f4d2f4733d889cf531affcb0ee0927df8bf906f"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.173, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/18.log", "expectedExitCode": 0}
{"index": 19, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "show", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.136, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/19.log", "expectedExitCode": 0}
{"index": 20, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "status", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.075, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/20.log", "expectedExitCode": 0}
{"index": 21, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "next", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.075, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/21.log", "expectedExitCode": 0}
{"index": 22, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "diagnose", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.008, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/22.log", "expectedExitCode": 0}
{"index": 23, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "drive", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.143, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/23.log", "expectedExitCode": 0}
{"index": 24, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "submit", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 1, "elapsedSeconds": 0.075, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/24.log", "expectedExitCode": 1}
{"index": 25, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "submit", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.096, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/25.log", "expectedExitCode": 0}
{"index": 26, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "lifecycle", "capture", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--provider", "claude", "--event", "SubagentStart"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.015, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/26.log", "expectedExitCode": 0}
{"index": 27, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "lifecycle", "capture", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--provider", "claude", "--event", "SubagentStop"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.015, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/27.log", "expectedExitCode": 0}
{"index": 28, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "lifecycle", "verify", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native", "--dispatch", "act:review/review.worker"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.009, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/28.log", "expectedExitCode": 0}
{"index": 29, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/intake.json", "--route", "regular", "--base-snapshot", "1f4d2f4733d889cf531affcb0ee0927df8bf906f"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 1, "elapsedSeconds": 0.045, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/29.log", "expectedExitCode": 1}
{"index": 30, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "install", "--source", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/install-source", "--host", "codex", "--scope", "project", "--project", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--binary-target", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "--force", "--skip-hooks"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 2.353, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/30.log", "expectedExitCode": 0}
{"index": 31, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "install", "--source", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/install-source", "--host", "codex", "--scope", "project", "--project", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--binary-target", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "--force", "--skip-hooks", "--bootstrap"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.22, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/31.log", "expectedExitCode": 0}
{"index": 32, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "start", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "codex-native", "--intake-receipt", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/intake.json", "--route", "regular", "--base-snapshot", "1f4d2f4733d889cf531affcb0ee0927df8bf906f"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.167, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/32.log", "expectedExitCode": 0}
{"index": 33, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "show", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.081, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/33.log", "expectedExitCode": 0}
{"index": 34, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "drive", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "claude-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.077, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/34.log", "expectedExitCode": 0}
{"index": 35, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "show", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "codex-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.076, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/35.log", "expectedExitCode": 0}
{"index": 36, "command": ["/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/host-home/.local/bin/formal-gates", "workflow", "drive", "--root", "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "--run-id", "codex-native"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.132, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/36.log", "expectedExitCode": 0}
{"index": 37, "command": ["git", "rev-parse", "HEAD"], "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application", "exitCode": 0, "elapsedSeconds": 0.015, "log": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/37.log", "expectedExitCode": 0}

````

## 17.log

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/17.log；bytes=148；sha256=2ebb034bcc13c1ea782905a01d12e720d13647a7c3371a7e2b872325c1ca0d08

````text
base snapshot da011a5db74513f11d4585322e9f773e622dc657: cannot resolve git ancestry of da011a5db74513f11d4585322e9f773e622dc657: git: exit status 1

````

## 18.log

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/18.log；bytes=2917；sha256=f2a545126b888c511ef0c16c5dafb5216cf22823912456548a2caff7bc747f8b

````text
{
  "runId": "claude-native",
  "runtime": "engine",
  "route": "regular",
  "status": "ACTIVE",
  "revision": 2,
  "envelope": {
    "writer": "engine",
    "stateSchemaVersion": "1",
    "workflowDefinitionVersion": "6",
    "definitionSource": "definitions/workflow.json",
    "definitionDigest": "sha256:1eabbb1117e67d433dd41c2db8684e1589084ebf8bfa2ac8879b3939445c7d55",
    "packageDigest": "284d0f07088af5a84bcc0b08d274af0d4f67a182bac80ee1c29e66f06e7cedd7",
    "installedTargetIdentity": "claude-global-e25a68aeac05"
  },
  "baseSnapshot": "1f4d2f4733d889cf531affcb0ee0927df8bf906f",
  "intakeConfirmationReceipt": {
    "source": "stable-driver",
    "authority": "stable-driver",
    "transport": "stable-launcher",
    "requirementSource": "requirements.md",
    "requirementRevision": "bd3189cd7f4aff44ce3fd07bb574de4754e89faf48f25af93d7401f129b6e8fe",
    "artifacts": [
      {
        "path": "design.md",
        "revision": "bae265c1f03d66ecca8cd72383843e304b608a2cccbcca56372d8f461da43c22"
      },
      {
        "path": "requirements.md",
        "revision": "bd3189cd7f4aff44ce3fd07bb574de4754e89faf48f25af93d7401f129b6e8fe"
      }
    ],
    "solutionRevision": "bae265c1f03d66ecca8cd72383843e304b608a2cccbcca56372d8f461da43c22",
    "solutionDigest": "sha256:bae265c1f03d66ecca8cd72383843e304b608a2cccbcca56372d8f461da43c22",
    "digest": "sha256:7752ebe0468d227dbc02861296706c10760fa4a10807d0d95657c55b9047998b"
  },
  "next": {
    "Kind": "WAIT",
    "Ready": null,
    "HostAction": null,
    "Ask": null,
    "Wait": {
      "reason": "ENGINE_INTERNAL"
    },
    "Operator": null,
    "Complete": null
  },
  "review": {
    "actions": [
      {
        "action": "product-review",
        "completed": 0,
        "baseLimit": 3,
        "extraAuthorized": 0,
        "effectiveLimit": 3,
        "remaining": 3,
        "nextRoundRequirement": "AUTOMATIC_CAPACITY",
        "grants": [],
        "rawResults": 0,
        "adjudications": 0,
        "corrections": 0,
        "effectiveStatus": "PENDING",
        "effectiveSource": "WORKFLOW_START",
        "candidates": [],
        "effectiveRevision": 0
      },
      {
        "action": "start-readiness",
        "completed": 0,
        "baseLimit": 3,
        "extraAuthorized": 0,
        "effectiveLimit": 3,
        "remaining": 3,
        "nextRoundRequirement": "AUTOMATIC_CAPACITY",
        "grants": [],
        "rawResults": 0,
        "adjudications": 0,
        "corrections": 0,
        "effectiveStatus": "PENDING",
        "effectiveSource": "WORKFLOW_START",
        "candidates": [],
        "effectiveRevision": 0
      }
    ],
    "requirementRevision": "bd3189cd7f4aff44ce3fd07bb574de4754e89faf48f25af93d7401f129b6e8fe"
  },
  "requirementWork": [],
  "summaryPath": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application/.gates/engine/claude-native/terminal-summary.json"
}

````

## 24.log

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/24.log；bytes=88；sha256=6d879d29d95bc8319fbc5a05ff42ac195bb552552a5f8740c779f625f78f4d21

````text
PROVIDER_MISMATCH: provider "codex" != run binding "claude-code" (no default downgrade)

````

## 25.log

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/25.log；bytes=5762；sha256=5f1b50a5776cbcfa839a491d73c45ed516bd1d331947a3ce084a5ba548cc1f4d

````text
{
  "acceptance": {
    "eventId": "installed-claim",
    "kind": "SPAWN_RECEIPT",
    "status": "ACCEPTED",
    "revision": 6,
    "actionId": "act:review/review.worker"
  },
  "next": {
    "Kind": "WAIT",
    "Ready": null,
    "HostAction": null,
    "Ask": null,
    "Wait": {
      "reason": "TASKS_IN_FLIGHT"
    },
    "Operator": null,
    "Complete": null
  },
  "run": {
    "attempts": {
      "review/review.worker": {
        "business": true,
        "actionId": "act:review/review.worker",
        "task": "review/review.worker",
        "step": "review.worker",
        "id": "att:review/review.worker:5",
        "bindings": {
          "task": "review/review.worker",
          "snapshot": "sha256:dd2283a550e9ff83260ecb2b5904f68f7bfde44f83fe15fede816ca608718d79",
          "responsibility": "claude-code"
        },
        "plan": {
          "planDigest": "sha256:badb229936babbf959981ab3c21d9690fdd52c906a9d2a04c57f2665fffda1bf",
          "definitionDigest": "sha256:1eabbb1117e67d433dd41c2db8684e1589084ebf8bfa2ac8879b3939445c7d55",
          "stateDigest": "sha256:1f73305067ff3bda6e59132cfafe495b481efc195d8f5008346dc4bf4858915c",
          "observationDigest": "sha256:dd2283a550e9ff83260ecb2b5904f68f7bfde44f83fe15fede816ca608718d79"
        },
        "maxAttempts": 2
      }
    },
    "pendingActions": {
      "act:review/review.worker": {
        "actionId": "act:review/review.worker",
        "task": "review/review.worker",
        "step": "review.worker",
        "attemptId": "att:review/review.worker:5",
        "reviewSeries": "product-review"
      }
    },
    "runId": "claude-native",
    "runtime": "engine",
    "route": "regular",
    "status": "ACTIVE",
    "revision": 6,
    "envelope": {
      "writer": "engine",
      "stateSchemaVersion": "1",
      "workflowDefinitionVersion": "6",
      "definitionSource": "definitions/workflow.json",
      "definitionDigest": "sha256:1eabbb1117e67d433dd41c2db8684e1589084ebf8bfa2ac8879b3939445c7d55",
      "packageDigest": "284d0f07088af5a84bcc0b08d274af0d4f67a182bac80ee1c29e66f06e7cedd7",
      "installedTargetIdentity": "claude-global-e25a68aeac05"
    },
    "baseSnapshot": "1f4d2f4733d889cf531affcb0ee0927df8bf906f",
    "intakeConfirmationReceipt": {
      "source": "stable-driver",
      "authority": "stable-driver",
      "transport": "stable-launcher",
      "requirementSource": "requirements.md",
      "requirementRevision": "bd3189cd7f4aff44ce3fd07bb574de4754e89faf48f25af93d7401f129b6e8fe",
      "artifacts": [
        {
          "path": "design.md",
          "revision": "bae265c1f03d66ecca8cd72383843e304b608a2cccbcca56372d8f461da43c22"
        },
        {
          "path": "requirements.md",
          "revision": "bd3189cd7f4aff44ce3fd07bb574de4754e89faf48f25af93d7401f129b6e8fe"
        }
      ],
      "solutionRevision": "bae265c1f03d66ecca8cd72383843e304b608a2cccbcca56372d8f461da43c22",
      "solutionDigest": "sha256:bae265c1f03d66ecca8cd72383843e304b608a2cccbcca56372d8f461da43c22",
      "digest": "sha256:7752ebe0468d227dbc02861296706c10760fa4a10807d0d95657c55b9047998b"
    },
    "intakeReceipt": {
      "confirmation": {
        "source": "stable-driver",
        "authority": "stable-driver",
        "transport": "stable-launcher",
        "requirementSource": "requirements.md",
        "requirementRevision": "bd3189cd7f4aff44ce3fd07bb574de4754e89faf48f25af93d7401f129b6e8fe",
        "artifacts": [
          {
            "path": "design.md",
            "revision": "bae265c1f03d66ecca8cd72383843e304b608a2cccbcca56372d8f461da43c22"
          },
          {
            "path": "requirements.md",
            "revision": "bd3189cd7f4aff44ce3fd07bb574de4754e89faf48f25af93d7401f129b6e8fe"
          }
        ],
        "solutionRevision": "bae265c1f03d66ecca8cd72383843e304b608a2cccbcca56372d8f461da43c22",
        "solutionDigest": "sha256:bae265c1f03d66ecca8cd72383843e304b608a2cccbcca56372d8f461da43c22",
        "digest": "sha256:7752ebe0468d227dbc02861296706c10760fa4a10807d0d95657c55b9047998b"
      },
      "intakeDigest": "sha256:7752ebe0468d227dbc02861296706c10760fa4a10807d0d95657c55b9047998b",
      "revision": 3
    },
    "next": {
      "Kind": "WAIT",
      "Ready": null,
      "HostAction": null,
      "Ask": null,
      "Wait": {
        "reason": "TASKS_IN_FLIGHT"
      },
      "Operator": null,
      "Complete": null
    },
    "review": {
      "actions": [
        {
          "action": "product-review",
          "completed": 0,
          "baseLimit": 3,
          "extraAuthorized": 0,
          "effectiveLimit": 3,
          "remaining": 3,
          "nextRoundRequirement": "AUTOMATIC_CAPACITY",
          "grants": [],
          "rawResults": 0,
          "adjudications": 0,
          "corrections": 0,
          "effectiveStatus": "PENDING",
          "effectiveSource": "WORKFLOW_START",
          "candidates": [],
          "effectiveRevision": 0
        },
        {
          "action": "start-readiness",
          "completed": 0,
          "baseLimit": 3,
          "extraAuthorized": 0,
          "effectiveLimit": 3,
          "remaining": 3,
          "nextRoundRequirement": "AUTOMATIC_CAPACITY",
          "grants": [],
          "rawResults": 0,
          "adjudications": 0,
          "corrections": 0,
          "effectiveStatus": "PENDING",
          "effectiveSource": "WORKFLOW_START",
          "candidates": [],
          "effectiveRevision": 0
        }
      ],
      "requirementRevision": "bd3189cd7f4aff44ce3fd07bb574de4754e89faf48f25af93d7401f129b6e8fe"
    },
    "requirementWork": [],
    "summaryPath": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application/.gates/engine/claude-native/terminal-summary.json"
  }
}

````

## 26.stdin.json

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/26.stdin.json；bytes=132；sha256=885d123fe5846a3187b78a02c2b0ab7ec701c5c2cb04d5d5a84c17af80f8fc29

````text
{"agent_id": "application-worker", "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application"}
````

## 26.log

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/26.log；bytes=231；sha256=9c6192f0a373b3036bfedf43a4a3ca1361694711ce4ca4df0f28bd9ec359ed51

````text
{
  "provider": "claude-code",
  "event": "subagent_start",
  "identity": "application-worker",
  "duplicate": false,
  "roots": [
    "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application"
  ]
}

````

## 27.stdin.json

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/27.stdin.json；bytes=132；sha256=885d123fe5846a3187b78a02c2b0ab7ec701c5c2cb04d5d5a84c17af80f8fc29

````text
{"agent_id": "application-worker", "cwd": "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application"}
````

## 27.log

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/27.log；bytes=230；sha256=66dec636ec064e946c94b628293031de221aa5e91ba866b81cd4a640b22cdc2b

````text
{
  "provider": "claude-code",
  "event": "subagent_stop",
  "identity": "application-worker",
  "duplicate": false,
  "roots": [
    "/private/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/application"
  ]
}

````

## 28.log

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/28.log；bytes=245；sha256=2baa34eaf0e34b2f39e0373575cca637ba3b055153fd45e55ff0367daed4f691

````text
{
  "outcome": "VERIFIED",
  "provider": "claude-code",
  "dispatchId": "act:review/review.worker",
  "identity": "application-worker",
  "startObserved": true,
  "stopObserved": true,
  "diagnostic": "matching start and stop events observed"
}

````

## 29.log

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/29.log；bytes=53；sha256=f6f9ca48d3cd625439b5cce6f2aaaff3516213a5e90018f6b2fd3b2d7ee4c4f3

````text
run "claude-native" already exists in legacy runtime

````

## bindings-summary.json

来源：/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/bindings-summary.json；bytes=658；sha256=e2cc20bc6af9867413c53791d53a05d45b2b6dac8e517ed59e868b501e2360d9

````text
{
  "provider": "claude-code",
  "base": "1f4d2f4733d889cf531affcb0ee0927df8bf906f",
  "claim": {
    "runId": "claude-native",
    "dispatchId": "act:review/review.worker",
    "provider": "claude-code",
    "identity": "application-worker"
  },
  "lifecycle": {
    "outcome": "VERIFIED",
    "provider": "claude-code",
    "dispatchId": "act:review/review.worker",
    "identity": "application-worker",
    "startObserved": true,
    "stopObserved": true,
    "diagnostic": "matching start and stop events observed"
  },
  "otherProvider": "codex",
  "nativeHead": "1f4d2f4733d889cf531affcb0ee0927df8bf906f",
  "realModelCalls": 0,
  "realModelCost": 0
}

````

## installed-public-start-acceptance-boundary-observation.log

来源：/tmp/installed-public-start-acceptance-boundary-observation.log；bytes=4044；sha256=1a8cc89de9b326eac8178e2d6d3c1ba304dcc6e7c5feebd36c12c39f449c949d

````text
=== RUN   TestInstalledRegularStartCarriesHostThroughPublicOperations
    installed_start_test.go:212: regular drive did not reach the external worker boundary: {RecoveryRecords:[] lastEvent:<nil> Termination:<nil> ValidationDecisions:map[] ValidationRequests:map[] NotGuaranteed:false ValidationInputs:map[] Attempts:map[review/review.worker:{Business:true AwaitingResult:false PreviousAction: Work:<nil> IssuedAction:{ActionID:act:review/review.worker Task:review/review.worker Step:review.worker} ID:att:review/review.worker:5 Bindings:{Task:review/review.worker Snapshot:sha256:dd2283a550e9ff83260ecb2b5904f68f7bfde44f83fe15fede816ca608718d79 Responsibility:zcode} Plan:{PlanDigest:sha256:badb229936babbf959981ab3c21d9690fdd52c906a9d2a04c57f2665fffda1bf DefinitionDigest:sha256:1eabbb1117e67d433dd41c2db8684e1589084ebf8bfa2ac8879b3939445c7d55 StateDigest:sha256:1f73305067ff3bda6e59132cfafe495b481efc195d8f5008346dc4bf4858915c ObservationDigest:sha256:dd2283a550e9ff83260ecb2b5904f68f7bfde44f83fe15fede816ca608718d79} Attempts:0 MaxAttempts:2 RetryExhausted:false}] PendingActions:map[act:review/review.worker:{ActionID:act:review/review.worker Task:review/review.worker Step:review.worker AttemptID:att:review/review.worker:5 ReviewSeries:product-review}] RejectedResults:map[] Validation:<nil> DispatchBindings:map[] AdapterIntents:map[] EndedAttempts:map[] RunID:native-review Runtime:engine Route:regular Status:ACTIVE Revision:5 Envelope:{Writer:engine StateSchemaVersion:1 WorkflowDefinitionVersion:6 DefinitionSource:definitions/workflow.json DefinitionDigest:sha256:1eabbb1117e67d433dd41c2db8684e1589084ebf8bfa2ac8879b3939445c7d55 PackageDigest:95de98ebd6200a36afc9d2951a09b11f7d3448d62936080d32dd84c6945a24b8 InstalledTargetIdentity:zcode-global-a0084cbbc419} BaseSnapshot:035839d79f7cdc044f8c68695637efd3bc4add07 PromotedSnapshot: QAWorktree: IntakeConfirmationReceipt:0x3b34cbf0000 IntakeReceipt:0x3b34cbf2000 Next:{Kind:WAIT Ready:<nil> HostAction:<nil> Ask:<nil> Wait:0x3b34cc966c0 Operator:<nil> Complete:<nil>} AvailableActions:[] Review:{Actions:[{Action:product-review Completed:0 BaseLimit:3 ExtraAuthorized:0 EffectiveLimit:3 Remaining:3 NextRoundRequirement:AUTOMATIC_CAPACITY Grants:[] RawResults:0 Adjudications:0 Corrections:0 EffectiveStatus:PENDING EffectiveSource:WORKFLOW_START Candidates:[] EffectiveRevision:0} {Action:start-readiness Completed:0 BaseLimit:3 ExtraAuthorized:0 EffectiveLimit:3 Remaining:3 NextRoundRequirement:AUTOMATIC_CAPACITY Grants:[] RawResults:0 Adjudications:0 Corrections:0 EffectiveStatus:PENDING EffectiveSource:WORKFLOW_START Candidates:[] EffectiveRevision:0}] Topology:<nil> RouteMode: SelectedGates:[] RequirementRevision:b58eb991037330fa3129bfa8ee3ef917c0081f001222309434126e551c27b166 LastRequirementChange:<nil>} RequirementWork:[] PendingRequirementAsk:<nil> PendingAsks:[] Cost:<nil> SliceReceipt:<nil> SliceChildren:map[] SliceReceipts:map[] GuardBudget:<nil> SummaryPath:/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledRegularStartCarriesHostThroughPublicOperations3288269816/002/.gates/engine/native-review/terminal-summary.json Unverified:false}
--- FAIL: TestInstalledRegularStartCarriesHostThroughPublicOperations (3.53s)
=== RUN   TestInstalledStartRejectsInvalidInputsBeforeState
--- PASS: TestInstalledStartRejectsInvalidInputsBeforeState (3.28s)
=== RUN   TestInstalledLightweightAndRegularStartRecovery
    installed_start_test.go:361: concurrent start: exit status 1
        {
          "code": "LOCK_HELD",
          "message": "run start lock is held: /var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledLightweightAndRegularStartRecovery63243440/002/.gates/run-locks/native-concurrent.lock",
          "operation": "run start",
          "path": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledLightweightAndRegularStartRecovery63243440/002/.gates/run-locks/native-concurrent.lock"
        }
--- FAIL: TestInstalledLightweightAndRegularStartRecovery (4.12s)
FAIL
FAIL	formal-gates/internal/engine/acceptance	13.210s
FAIL

````

## installed-public-start-acceptance-compilation.log

来源：/tmp/installed-public-start-acceptance-compilation.log；bytes=316；sha256=a8c11afccee233f506ab9b9c177a982339b3071eeeb872e3dcfb265359ec94a1

````text
# formal-gates/internal/engine/acceptance_test [formal-gates/internal/engine/acceptance.test]
internal/engine/acceptance/installed_start_test.go:211:134: driven.IntakeReceipt.Digest undefined (type *facade.IntakeReceipt has no field or method Digest)
FAIL	formal-gates/internal/engine/acceptance [build failed]
FAIL

````

## installed-public-start-acceptance.log

来源：/tmp/installed-public-start-acceptance.log；bytes=458；sha256=a02bf910670d5b8bd9a8954d6a19603935ccc43239be2d0c80108a6d68ead565

````text
=== RUN   TestInstalledRegularStartCarriesHostThroughPublicOperations
--- PASS: TestInstalledRegularStartCarriesHostThroughPublicOperations (5.49s)
=== RUN   TestInstalledStartRejectsInvalidInputsBeforeState
--- PASS: TestInstalledStartRejectsInvalidInputsBeforeState (2.68s)
=== RUN   TestInstalledLightweightAndRegularStartRecovery
--- PASS: TestInstalledLightweightAndRegularStartRecovery (3.39s)
PASS
ok  	formal-gates/internal/engine/acceptance	13.108s

````

## installed-public-start-added-boundaries.log

来源：/tmp/installed-public-start-added-boundaries.log；bytes=330；sha256=9979ac01d1a49952a3e59acbe41d86c6dc52ec1bfc7b7ebe698845fe00d34aa3

````text
=== RUN   TestInstalledRegularStartCarriesHostThroughPublicOperations
--- PASS: TestInstalledRegularStartCarriesHostThroughPublicOperations (7.26s)
=== RUN   TestInstalledLightweightAndRegularStartRecovery
--- PASS: TestInstalledLightweightAndRegularStartRecovery (3.60s)
PASS
ok  	formal-gates/internal/engine/acceptance	12.091s

````

## installed-public-start-admission-race.log

来源：/tmp/installed-public-start-admission-race.log；bytes=73；sha256=023b35b22c2521eff039d68385735fe499e0e0d8aa28d2bfae02f25f21315413

````text
ok  	formal-gates/internal/validate	5.773s
real 12.37
user 8.29
sys 1.78

````

## installed-public-start-admission.log

来源：/tmp/installed-public-start-admission.log；bytes=2242；sha256=cd9ed190d74fd6eea4b79cb27fc28c39bbc0a7fab9e3d381f0afc0693449a53f

````text
=== RUN   TestResolveEngineAdmissionChoosesProjectBinding
--- PASS: TestResolveEngineAdmissionChoosesProjectBinding (0.48s)
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude-code
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude_code
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/codex/codex
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/cursor/cursor
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/dsh
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek-harness
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek_harness
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/zcode
=== RUN   TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/z-code
--- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases (1.23s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude (0.10s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude-code (0.10s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/claude-code/claude_code (0.10s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/codex/codex (0.10s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/cursor/cursor (0.10s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/dsh (0.10s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek (0.10s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek-harness (0.10s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/deepseek-harness/deepseek_harness (0.10s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/zcode (0.10s)
    --- PASS: TestEngineAdmissionNormalizesRegisteredHostAliases/zcode/z-code (0.10s)
PASS
ok  	formal-gates/internal/validate	2.894s

````

## installed-public-start-build.log

来源：/tmp/installed-public-start-build.log；bytes=29；sha256=a634dca47a59197bc1b8e926333a14711c9aec925b3435d59f6e86e88dac32ae

````text
real 1.68
user 0.99
sys 0.36

````

## installed-public-start-cli-race.log

来源：/tmp/installed-public-start-cli-race.log；bytes=69；sha256=cc238119604e95905a621bea6fb245dcd4352c4662f84590e3c87c0b9beb3454

````text
ok  	formal-gates/internal/cli	13.594s
real 17.91
user 8.59
sys 4.64

````

## installed-public-start-developer-final-probe.log

来源：/tmp/installed-public-start-developer-final-probe.log；bytes=3660；sha256=41ff5fac789d461e15d055ca1e61de97fa6c8c67fb196c4e6ebdadcc6217aaa7

````text
=== RUN   TestInstalledPublicStartAdmitsRegularNativeRun
    installed_regular_start_business_test.go:99: immutable actual package=ddcda1653f8110ca2e8f517473262d1662dbc6cce442501c622035191c4d80b4 record=zcode-global-d0c291c21dc5 host=zcode; public regular result="{\n  \"runId\": \"native-review\",\n  \"runtime\": \"engine\",\n  \"route\": \"regular\",\n  \"status\": \"ACTIVE\",\n  \"revision\": 2,\n  \"envelope\": {\n    \"writer\": \"engine\",\n    \"stateSchemaVersion\": \"1\",\n    \"workflowDefinitionVersion\": \"6\",\n    \"definitionSource\": \"definitions/workflow.json\",\n    \"definitionDigest\": \"sha256:1eabbb1117e67d433dd41c2db8684e1589084ebf8bfa2ac8879b3939445c7d55\",\n    \"packageDigest\": \"ddcda1653f8110ca2e8f517473262d1662dbc6cce442501c622035191c4d80b4\",\n    \"installedTargetIdentity\": \"zcode-global-d0c291c21dc5\"\n  },\n  \"baseSnapshot\": \"fd341ff1536a00d943df938ea814ff096f514a57\",\n  \"intakeConfirmationReceipt\": {\n    \"source\": \"stable-driver\",\n    \"authority\": \"stable-driver\",\n    \"transport\": \"stable-launcher\",\n    \"requirementSource\": \"requirements.md\",\n    \"requirementRevision\": \"0068b0eb338f1a47a1af833bfc2afa058dff30ad3551865b24108e187eaaed5a\",\n    \"artifacts\": [\n      {\n        \"path\": \"design.md\",\n        \"revision\": \"f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002\"\n      },\n      {\n        \"path\": \"requirements.md\",\n        \"revision\": \"0068b0eb338f1a47a1af833bfc2afa058dff30ad3551865b24108e187eaaed5a\"\n      }\n    ],\n    \"solutionRevision\": \"f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002\",\n    \"solutionDigest\": \"sha256:f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002\",\n    \"digest\": \"sha256:0e417adf92de1b3df5d95848895dc968419bdfde8630ad8c311eea5c04a22e82\"\n  },\n  \"next\": {\n    \"Kind\": \"WAIT\",\n    \"Ready\": null,\n    \"HostAction\": null,\n    \"Ask\": null,\n    \"Wait\": {\n      \"reason\": \"ENGINE_INTERNAL\"\n    },\n    \"Operator\": null,\n    \"Complete\": null\n  },\n  \"review\": {\n    \"actions\": [\n      {\n        \"action\": \"product-review\",\n        \"completed\": 0,\n        \"baseLimit\": 3,\n        \"extraAuthorized\": 0,\n        \"effectiveLimit\": 3,\n        \"remaining\": 3,\n        \"nextRoundRequirement\": \"AUTOMATIC_CAPACITY\",\n        \"grants\": [],\n        \"rawResults\": 0,\n        \"adjudications\": 0,\n        \"corrections\": 0,\n        \"effectiveStatus\": \"PENDING\",\n        \"effectiveSource\": \"WORKFLOW_START\",\n        \"candidates\": [],\n        \"effectiveRevision\": 0\n      },\n      {\n        \"action\": \"start-readiness\",\n        \"completed\": 0,\n        \"baseLimit\": 3,\n        \"extraAuthorized\": 0,\n        \"effectiveLimit\": 3,\n        \"remaining\": 3,\n        \"nextRoundRequirement\": \"AUTOMATIC_CAPACITY\",\n        \"grants\": [],\n        \"rawResults\": 0,\n        \"adjudications\": 0,\n        \"corrections\": 0,\n        \"effectiveStatus\": \"PENDING\",\n        \"effectiveSource\": \"WORKFLOW_START\",\n        \"candidates\": [],\n        \"effectiveRevision\": 0\n      }\n    ],\n    \"requirementRevision\": \"0068b0eb338f1a47a1af833bfc2afa058dff30ad3551865b24108e187eaaed5a\"\n  },\n  \"requirementWork\": [],\n  \"summaryPath\": \"/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun2630980074/003/.gates/engine/native-review/terminal-summary.json\"\n}\n"
--- PASS: TestInstalledPublicStartAdmitsRegularNativeRun (3.83s)
PASS
ok  	formal-gates/internal/engine/acceptance	5.129s
real 6.13
user 1.52
sys 1.49

````

## installed-public-start-developer-original.log

来源：/tmp/installed-public-start-developer-original.log；bytes=632；sha256=59a627d2a792ef4f3a7fe5f4d67e9e23d1dddfb4583d976ea58144c388f51ae1

````text
=== RUN   TestInstalledPublicStartAdmitsRegularNativeRun
    installed_regular_start_business_test.go:99: immutable actual package=b379ed8447fe09a6d1a59dfb49ecd3892f86c7c8f26b86979cd733423e9de471 record=zcode-global-1232b45970e6 host=zcode; public regular result="candidate engine start only supports --route lightweight\n"
    installed_regular_start_business_test.go:116: normal installed public regular start was rejected: exit status 1
        candidate engine start only supports --route lightweight
--- FAIL: TestInstalledPublicStartAdmitsRegularNativeRun (3.11s)
FAIL
FAIL	formal-gates/internal/engine/acceptance	5.162s
FAIL

````

## installed-public-start-developer-repaired-probe.log

来源：/tmp/installed-public-start-developer-repaired-probe.log；bytes=3631；sha256=428aa6b3ebad62e4f60b5bb928432245be4013ca13d12b3eff53717c8cb70640

````text
=== RUN   TestInstalledPublicStartAdmitsRegularNativeRun
    installed_regular_start_business_test.go:99: immutable actual package=e3c3cde6336b6a4fd73dbf90a4270b0abb6725a342efa12363441edec260acb2 record=zcode-global-02b2a798d160 host=zcode; public regular result="{\n  \"runId\": \"native-review\",\n  \"runtime\": \"engine\",\n  \"route\": \"regular\",\n  \"status\": \"ACTIVE\",\n  \"revision\": 2,\n  \"envelope\": {\n    \"writer\": \"engine\",\n    \"stateSchemaVersion\": \"1\",\n    \"workflowDefinitionVersion\": \"6\",\n    \"definitionSource\": \"definitions/workflow.json\",\n    \"definitionDigest\": \"sha256:1eabbb1117e67d433dd41c2db8684e1589084ebf8bfa2ac8879b3939445c7d55\",\n    \"packageDigest\": \"e3c3cde6336b6a4fd73dbf90a4270b0abb6725a342efa12363441edec260acb2\",\n    \"installedTargetIdentity\": \"zcode-global-02b2a798d160\"\n  },\n  \"baseSnapshot\": \"79fccebb4b84860e56346c583ebd3ff3e9ebe3e0\",\n  \"intakeConfirmationReceipt\": {\n    \"source\": \"stable-driver\",\n    \"authority\": \"stable-driver\",\n    \"transport\": \"stable-launcher\",\n    \"requirementSource\": \"requirements.md\",\n    \"requirementRevision\": \"0068b0eb338f1a47a1af833bfc2afa058dff30ad3551865b24108e187eaaed5a\",\n    \"artifacts\": [\n      {\n        \"path\": \"design.md\",\n        \"revision\": \"f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002\"\n      },\n      {\n        \"path\": \"requirements.md\",\n        \"revision\": \"0068b0eb338f1a47a1af833bfc2afa058dff30ad3551865b24108e187eaaed5a\"\n      }\n    ],\n    \"solutionRevision\": \"f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002\",\n    \"solutionDigest\": \"sha256:f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002\",\n    \"digest\": \"sha256:0e417adf92de1b3df5d95848895dc968419bdfde8630ad8c311eea5c04a22e82\"\n  },\n  \"next\": {\n    \"Kind\": \"WAIT\",\n    \"Ready\": null,\n    \"HostAction\": null,\n    \"Ask\": null,\n    \"Wait\": {\n      \"reason\": \"ENGINE_INTERNAL\"\n    },\n    \"Operator\": null,\n    \"Complete\": null\n  },\n  \"review\": {\n    \"actions\": [\n      {\n        \"action\": \"product-review\",\n        \"completed\": 0,\n        \"baseLimit\": 3,\n        \"extraAuthorized\": 0,\n        \"effectiveLimit\": 3,\n        \"remaining\": 3,\n        \"nextRoundRequirement\": \"AUTOMATIC_CAPACITY\",\n        \"grants\": [],\n        \"rawResults\": 0,\n        \"adjudications\": 0,\n        \"corrections\": 0,\n        \"effectiveStatus\": \"PENDING\",\n        \"effectiveSource\": \"WORKFLOW_START\",\n        \"candidates\": [],\n        \"effectiveRevision\": 0\n      },\n      {\n        \"action\": \"start-readiness\",\n        \"completed\": 0,\n        \"baseLimit\": 3,\n        \"extraAuthorized\": 0,\n        \"effectiveLimit\": 3,\n        \"remaining\": 3,\n        \"nextRoundRequirement\": \"AUTOMATIC_CAPACITY\",\n        \"grants\": [],\n        \"rawResults\": 0,\n        \"adjudications\": 0,\n        \"corrections\": 0,\n        \"effectiveStatus\": \"PENDING\",\n        \"effectiveSource\": \"WORKFLOW_START\",\n        \"candidates\": [],\n        \"effectiveRevision\": 0\n      }\n    ],\n    \"requirementRevision\": \"0068b0eb338f1a47a1af833bfc2afa058dff30ad3551865b24108e187eaaed5a\"\n  },\n  \"requirementWork\": [],\n  \"summaryPath\": \"/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun2819322867/003/.gates/engine/native-review/terminal-summary.json\"\n}\n"
--- PASS: TestInstalledPublicStartAdmitsRegularNativeRun (4.20s)
PASS
ok  	formal-gates/internal/engine/acceptance	5.607s

````

## installed-public-start-facade-race.log

来源：/tmp/installed-public-start-facade-race.log；bytes=77；sha256=aee055d185d9e05751634aef5f1c539117ae02b93bb2f68ddd69ae41cdd7f82c

````text
ok  	formal-gates/internal/engine/facade	5.941s
real 7.57
user 1.57
sys 0.44

````

## installed-public-start-lifecycle-inherited.log

来源：/tmp/installed-public-start-lifecycle-inherited.log；bytes=262；sha256=ca1d31d60189553e79b9370d117678402e5946c062330e88e795863836717221

````text
=== RUN   TestProviderFromEnvironmentRecognizesZCode
    lifecycle_test.go:437: ZCODE_PLUGIN_ROOT resolved to "codex"
--- FAIL: TestProviderFromEnvironmentRecognizesZCode (0.00s)
FAIL
FAIL	formal-gates/internal/lifecycle	1.147s
FAIL
real 1.53
user 0.16
sys 0.20

````

## installed-public-start-lifecycle-isolated.log

来源：/tmp/installed-public-start-lifecycle-isolated.log；bytes=5744；sha256=9f24561e021583d1e7140926c6b8fb6fc87b3976b300a8ac0db4900ae2923ce0

````text
=== RUN   TestLifecycleCaptureAndVerification
--- PASS: TestLifecycleCaptureAndVerification (0.04s)
=== RUN   TestCursorCorrelatesNormalStopWithoutIdentityAndDerivesDistinctRoots
--- PASS: TestCursorCorrelatesNormalStopWithoutIdentityAndDerivesDistinctRoots (0.06s)
=== RUN   TestLifecycleRequiredProviderRejectsMissingOrMismatchedEvents
--- PASS: TestLifecycleRequiredProviderRejectsMissingOrMismatchedEvents (0.01s)
=== RUN   TestLifecycleUnclaimedObservationsRetireWithRun
--- PASS: TestLifecycleUnclaimedObservationsRetireWithRun (0.01s)
=== RUN   TestLifecycleHookDefinitionsOwnProviderEventsAndCommands
=== RUN   TestLifecycleHookDefinitionsOwnProviderEventsAndCommands/claude-code
=== RUN   TestLifecycleHookDefinitionsOwnProviderEventsAndCommands/codex
=== RUN   TestLifecycleHookDefinitionsOwnProviderEventsAndCommands/cursor
=== RUN   TestLifecycleHookDefinitionsOwnProviderEventsAndCommands/deepseek-harness
=== RUN   TestLifecycleHookDefinitionsOwnProviderEventsAndCommands/zcode
--- PASS: TestLifecycleHookDefinitionsOwnProviderEventsAndCommands (0.00s)
    --- PASS: TestLifecycleHookDefinitionsOwnProviderEventsAndCommands/claude-code (0.00s)
    --- PASS: TestLifecycleHookDefinitionsOwnProviderEventsAndCommands/codex (0.00s)
    --- PASS: TestLifecycleHookDefinitionsOwnProviderEventsAndCommands/cursor (0.00s)
    --- PASS: TestLifecycleHookDefinitionsOwnProviderEventsAndCommands/deepseek-harness (0.00s)
    --- PASS: TestLifecycleHookDefinitionsOwnProviderEventsAndCommands/zcode (0.00s)
=== RUN   TestLifecycleFactoryRegistryCoversEveryInstallableHost
--- PASS: TestLifecycleFactoryRegistryCoversEveryInstallableHost (0.00s)
=== RUN   TestZCodeToolHooksCorrelateAgentCallLifecycle
--- PASS: TestZCodeToolHooksCorrelateAgentCallLifecycle (0.03s)
=== RUN   TestLifecycleStopCaptureStoresTranscriptPath
--- PASS: TestLifecycleStopCaptureStoresTranscriptPath (0.01s)
=== RUN   TestLifecycleCursorAndPathlessStopsStayWithoutTranscriptPath
--- PASS: TestLifecycleCursorAndPathlessStopsStayWithoutTranscriptPath (0.03s)
=== RUN   TestLifecycleStopCaptureStoresInterruptionReason
--- PASS: TestLifecycleStopCaptureStoresInterruptionReason (0.03s)
=== RUN   TestPayloadHTTPErrorCodeStandaloneToken
=== RUN   TestPayloadHTTPErrorCodeStandaloneToken/standalone_code_in_prose
=== RUN   TestPayloadHTTPErrorCodeStandaloneToken/dedicated_error_code_field
=== RUN   TestPayloadHTTPErrorCodeStandaloneToken/code_at_string_edge
=== RUN   TestPayloadHTTPErrorCodeStandaloneToken/substring_inside_longer_number_ignored
=== RUN   TestPayloadHTTPErrorCodeStandaloneToken/substring_inside_word_ignored
=== RUN   TestPayloadHTTPErrorCodeStandaloneToken/code_as_part_of_decimal_ignored
--- PASS: TestPayloadHTTPErrorCodeStandaloneToken (0.00s)
    --- PASS: TestPayloadHTTPErrorCodeStandaloneToken/standalone_code_in_prose (0.00s)
    --- PASS: TestPayloadHTTPErrorCodeStandaloneToken/dedicated_error_code_field (0.00s)
    --- PASS: TestPayloadHTTPErrorCodeStandaloneToken/code_at_string_edge (0.00s)
    --- PASS: TestPayloadHTTPErrorCodeStandaloneToken/substring_inside_longer_number_ignored (0.00s)
    --- PASS: TestPayloadHTTPErrorCodeStandaloneToken/substring_inside_word_ignored (0.00s)
    --- PASS: TestPayloadHTTPErrorCodeStandaloneToken/code_as_part_of_decimal_ignored (0.00s)
=== RUN   TestLifecycleInterruptedDispatchVerification
--- PASS: TestLifecycleInterruptedDispatchVerification (0.02s)
=== RUN   TestLifecycleUninstalledProviderIsUnavailable
--- PASS: TestLifecycleUninstalledProviderIsUnavailable (0.03s)
=== RUN   TestLifecycleInstalledCodexRequiresPairedEvents
--- PASS: TestLifecycleInstalledCodexRequiresPairedEvents (0.01s)
=== RUN   TestProviderFromExecutable
=== RUN   TestProviderFromExecutable/cursor
=== RUN   TestProviderFromExecutable/default
=== RUN   TestProviderFromExecutable/default#01
=== RUN   TestProviderFromExecutable/claude-code
=== RUN   TestProviderFromExecutable/codex
--- PASS: TestProviderFromExecutable (0.00s)
    --- PASS: TestProviderFromExecutable/cursor (0.00s)
    --- PASS: TestProviderFromExecutable/default (0.00s)
    --- PASS: TestProviderFromExecutable/default#01 (0.00s)
    --- PASS: TestProviderFromExecutable/claude-code (0.00s)
    --- PASS: TestProviderFromExecutable/codex (0.00s)
=== RUN   TestProviderFromEnvironmentRecognizesZCode
--- PASS: TestProviderFromEnvironmentRecognizesZCode (0.00s)
=== RUN   TestClaudeCaptureResolvesActiveRunAboveSubagentWorkingDirectory
--- PASS: TestClaudeCaptureResolvesActiveRunAboveSubagentWorkingDirectory (0.03s)
=== RUN   TestCursorCaptureStagesForAllActiveWorkspaceRoots
--- PASS: TestCursorCaptureStagesForAllActiveWorkspaceRoots (0.04s)
=== RUN   TestCodexCLIRolloutBinding
--- PASS: TestCodexCLIRolloutBinding (0.02s)
=== RUN   TestDeepSeekLifecycleCaptureAndVerification
--- PASS: TestDeepSeekLifecycleCaptureAndVerification (0.02s)
=== RUN   TestProviderFromExecutableDistinguishesGlobalDshInstall
--- PASS: TestProviderFromExecutableDistinguishesGlobalDshInstall (0.00s)
=== RUN   TestCurrentProviderKeepsProjectDshLenientUnderDshEnvironment
--- PASS: TestCurrentProviderKeepsProjectDshLenientUnderDshEnvironment (0.00s)
=== RUN   TestProviderSelectionUsesSpecificRegistryBinding
--- PASS: TestProviderSelectionUsesSpecificRegistryBinding (0.00s)
=== RUN   TestProviderFromExecutableDistinguishesGlobalZCodeInstall
--- PASS: TestProviderFromExecutableDistinguishesGlobalZCodeInstall (0.00s)
=== RUN   TestCurrentProviderKeepsProjectZCodeLenient
--- PASS: TestCurrentProviderKeepsProjectZCodeLenient (0.00s)
=== RUN   TestCurrentProviderKeepsProjectZCodeStableLauncherLenient
--- PASS: TestCurrentProviderKeepsProjectZCodeStableLauncherLenient (0.00s)
PASS
ok  	formal-gates/internal/lifecycle	2.441s
real 2.99
user 0.21
sys 0.32

````

## installed-public-start-original-compilation.log

来源：/tmp/installed-public-start-original-compilation.log；bytes=785；sha256=0a90d33759b74142d3c1c60c07c3375b194f78960300258be7777b0761135a53

````text
# formal-gates/internal/engine/acceptance [formal-gates/internal/engine/acceptance.test]
/tmp/installed-regular-start-business_test.go:18:28: undefined: repoRoot
/tmp/installed-regular-start-business_test.go:23:15: undefined: executablePath
/tmp/installed-regular-start-business_test.go:24:2: undefined: buildInstalledBinaryAt
/tmp/installed-regular-start-business_test.go:25:12: undefined: isolatedInstallSource
/tmp/installed-regular-start-business_test.go:55:2: undefined: runCandidate
/tmp/installed-regular-start-business_test.go:56:2: undefined: runCandidate
/tmp/installed-regular-start-business_test.go:83:14: undefined: runCandidate
/tmp/installed-regular-start-business_test.go:136:11: undefined: runCandidate
FAIL	formal-gates/internal/engine/acceptance [build failed]
FAIL

````

## installed-public-start-original-failure.log

来源：/tmp/installed-public-start-original-failure.log；bytes=632；sha256=375470e5d1c93ff88392df5c28473683d53498dd856c5e0f5814d8ec1ce0be8b

````text
=== RUN   TestInstalledPublicStartAdmitsRegularNativeRun
    installed_regular_start_business_test.go:99: immutable actual package=b379ed8447fe09a6d1a59dfb49ecd3892f86c7c8f26b86979cd733423e9de471 record=zcode-global-7a99053ce7c6 host=zcode; public regular result="candidate engine start only supports --route lightweight\n"
    installed_regular_start_business_test.go:116: normal installed public regular start was rejected: exit status 1
        candidate engine start only supports --route lightweight
--- FAIL: TestInstalledPublicStartAdmitsRegularNativeRun (3.17s)
FAIL
FAIL	formal-gates/internal/engine/acceptance	4.536s
FAIL

````

## installed-public-start-packages.log

来源：/tmp/installed-public-start-packages.log；bytes=487；sha256=c8c079916933897475accf91e6e3c026054823b3be4fd697c46bad2f5eba3a73

````text
ok  	formal-gates/internal/cli	105.192s
ok  	formal-gates/internal/validate	426.533s
ok  	formal-gates/internal/engine/facade	450.012s
ok  	formal-gates/internal/engine/acceptance	32.107s
ok  	formal-gates/internal/engine/vcs	4.919s
ok  	formal-gates/internal/host	1.145s
--- FAIL: TestProviderFromEnvironmentRecognizesZCode (0.00s)
    lifecycle_test.go:437: ZCODE_PLUGIN_ROOT resolved to "codex"
FAIL
FAIL	formal-gates/internal/lifecycle	2.219s
FAIL
real 453.64
user 435.32
sys 217.89

````

## installed-public-start-original-compilation.log

来源：/tmp/installed-public-start-original-evidence/installed-public-start-original-compilation.log；bytes=785；sha256=0a90d33759b74142d3c1c60c07c3375b194f78960300258be7777b0761135a53

````text
# formal-gates/internal/engine/acceptance [formal-gates/internal/engine/acceptance.test]
/tmp/installed-regular-start-business_test.go:18:28: undefined: repoRoot
/tmp/installed-regular-start-business_test.go:23:15: undefined: executablePath
/tmp/installed-regular-start-business_test.go:24:2: undefined: buildInstalledBinaryAt
/tmp/installed-regular-start-business_test.go:25:12: undefined: isolatedInstallSource
/tmp/installed-regular-start-business_test.go:55:2: undefined: runCandidate
/tmp/installed-regular-start-business_test.go:56:2: undefined: runCandidate
/tmp/installed-regular-start-business_test.go:83:14: undefined: runCandidate
/tmp/installed-regular-start-business_test.go:136:11: undefined: runCandidate
FAIL	formal-gates/internal/engine/acceptance [build failed]
FAIL

````

## installed-public-start-original-failure.log

来源：/tmp/installed-public-start-original-evidence/installed-public-start-original-failure.log；bytes=632；sha256=375470e5d1c93ff88392df5c28473683d53498dd856c5e0f5814d8ec1ce0be8b

````text
=== RUN   TestInstalledPublicStartAdmitsRegularNativeRun
    installed_regular_start_business_test.go:99: immutable actual package=b379ed8447fe09a6d1a59dfb49ecd3892f86c7c8f26b86979cd733423e9de471 record=zcode-global-7a99053ce7c6 host=zcode; public regular result="candidate engine start only supports --route lightweight\n"
    installed_regular_start_business_test.go:116: normal installed public regular start was rejected: exit status 1
        candidate engine start only supports --route lightweight
--- FAIL: TestInstalledPublicStartAdmitsRegularNativeRun (3.17s)
FAIL
FAIL	formal-gates/internal/engine/acceptance	4.536s
FAIL

````

## installed-public-start-original-observation.json

来源：/tmp/installed-public-start-original-evidence/installed-public-start-original-observation.json；bytes=7862；sha256=78768115fccb184f1e9711a87f8266d11009610c8ec5a92d1da47f8081c617f9

````text
{
  "record": {
    "id": "zcode-global-7a99053ce7c6",
    "target": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.zcode/skills/formal-gates",
    "launcherPath": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.local/bin/formal-gates",
    "scope": "global",
    "host": "zcode",
    "hookConfig": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.zcode/cli/config.json",
    "skipHooks": true,
    "projectRoot": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001",
    "stateRoot": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.gates",
    "resourceRoot": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.formal-gates-resources",
    "runtimeSibling": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.zcode/skills",
    "vcsIdentity": "package:b379ed8447fe09a6d1a59dfb49ecd3892f86c7c8f26b86979cd733423e9de471",
    "packageDigest": "b379ed8447fe09a6d1a59dfb49ecd3892f86c7c8f26b86979cd733423e9de471",
    "installedDigest": "1608eb6c3ffa86572a8438ab4c1b47da83a1c94932f2148a11cee39b29a23073",
    "canonicalPaths": {
      "hookConfig": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.zcode/cli/config.json",
      "launcher": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.local/bin/formal-gates",
      "projectRoot": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001",
      "resourceRoot": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.formal-gates-resources",
      "runtimeSibling": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.zcode/skills",
      "stateRoot": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.gates",
      "target": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.zcode/skills/formal-gates"
    },
    "status": "active",
    "generation": 1,
    "lease": "lease-1",
    "token": "token-1791033155278761000"
  },
  "bootstrap": {
    "operation": "bootstrap",
    "accepted": true,
    "registry": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.formal-gates/registry.json",
    "epoch": 1,
    "packageDigest": "b379ed8447fe09a6d1a59dfb49ecd3892f86c7c8f26b86979cd733423e9de471",
    "vcsIdentity": "package:b379ed8447fe09a6d1a59dfb49ecd3892f86c7c8f26b86979cd733423e9de471",
    "sourceRoot": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/002",
    "sourceLstat": {
      "path": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/002",
      "realPath": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/002",
      "mode": 493,
      "size": 1120,
      "kind": "directory"
    },
    "records": [
      {
        "id": "zcode-global-7a99053ce7c6",
        "target": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.zcode/skills/formal-gates",
        "launcherPath": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.local/bin/formal-gates",
        "scope": "global",
        "host": "zcode",
        "hookConfig": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.zcode/cli/config.json",
        "skipHooks": true,
        "projectRoot": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001",
        "stateRoot": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.gates",
        "resourceRoot": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.formal-gates-resources",
        "runtimeSibling": "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.zcode/skills",
        "vcsIdentity": "package:b379ed8447fe09a6d1a59dfb49ecd3892f86c7c8f26b86979cd733423e9de471",
        "packageDigest": "b379ed8447fe09a6d1a59dfb49ecd3892f86c7c8f26b86979cd733423e9de471",
        "installedDigest": "1608eb6c3ffa86572a8438ab4c1b47da83a1c94932f2148a11cee39b29a23073",
        "canonicalPaths": {
          "hookConfig": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.zcode/cli/config.json",
          "launcher": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.local/bin/formal-gates",
          "projectRoot": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001",
          "resourceRoot": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.formal-gates-resources",
          "runtimeSibling": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.zcode/skills",
          "stateRoot": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.gates",
          "target": "/private/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/001/.zcode/skills/formal-gates"
        },
        "status": "active",
        "generation": 1,
        "lease": "lease-1",
        "token": "token-1791033155278761000"
      }
    ],
    "stateCreated": false,
    "observedAt": "2026-10-03T13:12:35.531182Z"
  },
  "intake": {
    "source": "stable-driver",
    "authority": "stable-driver",
    "transport": "stable-launcher",
    "requirementSource": "requirements.md",
    "requirementRevision": "0068b0eb338f1a47a1af833bfc2afa058dff30ad3551865b24108e187eaaed5a",
    "artifacts": [
      {
        "path": "requirements.md",
        "revision": "0068b0eb338f1a47a1af833bfc2afa058dff30ad3551865b24108e187eaaed5a"
      },
      {
        "path": "design.md",
        "revision": "f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002"
      }
    ],
    "solutionRevision": "f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002",
    "solutionDigest": "sha256:f1c8b8a9c129567476a14decac310a8dfc4184d02678aa7ddd55d5683119d002"
  },
  "base": "ddf80456d2ef777b63075e8eb52fb159df80896b",
  "args": [
    "workflow",
    "start",
    "--root",
    "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/003",
    "--run-id",
    "native-review",
    "--route",
    "regular",
    "--base-snapshot",
    "ddf80456d2ef777b63075e8eb52fb159df80896b",
    "--intake-receipt",
    "/var/folders/d_/drx086s144qb8g4b0yzfy9cm0000gn/T/TestInstalledPublicStartAdmitsRegularNativeRun3378432367/004/intake.json"
  ],
  "output": "candidate engine start only supports --route lightweight\n"
}
````

## installed-regular-start-business_test.go

来源：/tmp/installed-public-start-original-evidence/installed-regular-start-business_test.go；bytes=6784；sha256=0c6514f122cfd15070ff6e13bf3fb1742c672c5699421b62e8b680d65faa3b25

````text
package acceptance_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/validate"
)

func TestInstalledPublicStartAdmitsRegularNativeRun(t *testing.T) {
	root, err := filepath.Abs(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	candidate := executablePath(filepath.Join(home, ".local", "bin", "formal-gates"))
	buildInstalledBinaryAt(t, root, "./cmd/formal-gates", candidate)
	source := isolatedInstallSource(t, root, candidate)
	project := t.TempDir()
	runGit := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = project
		data, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %v\n%s", args, e, data)
		}
		return strings.TrimSpace(string(data))
	}
	write := func(path, content string) string {
		t.Helper()
		if e := os.WriteFile(filepath.Join(project, path), []byte(content), 0600); e != nil {
			t.Fatal(e)
		}
		sum := sha256.Sum256([]byte(content))
		return hex.EncodeToString(sum[:])
	}
	reqRevision := write("requirements.md", "# Native workflow\nREQ-001: Verify the tracked application through the documented regular workflow.\nAC-001: Native base identity is preserved at admission.\n")
	solutionRevision := write("design.md", "# Solution\nUse the installed controller and its native Git provider.\n")
	write(".gitignore", ".gates/\n.formal-gates/\n.zcode/\n")
	write("app.go", "package main\nfunc main() {}\n")
	runGit("init", "-q")
	runGit("config", "user.name", "Native workflow test")
	runGit("config", "user.email", "workflow@example.invalid")
	runGit("add", ".")
	runGit("commit", "-qm", "Application input")
	base := runGit("rev-parse", "HEAD")
	runCandidate(t, candidate, home, project, "install", "--source", source, "--host", "zcode", "--scope", "global", "--binary-target", candidate, "--force", "--skip-hooks")
	runCandidate(t, candidate, home, project, "install", "--source", source, "--host", "zcode", "--scope", "global", "--binary-target", candidate, "--bootstrap", "--skip-hooks")
	registryPath := filepath.Join(home, ".formal-gates", "registry.json")
	registry, e := validate.LoadRegistry(registryPath)
	if e != nil || len(registry.Records) != 1 {
		t.Fatalf("registry not established: %v records=%d", e, len(registry.Records))
	}
	record := registry.Records[0]
	if record.Host != "zcode" || record.LauncherPath == "" || record.PackageDigest == "" {
		t.Fatalf("incomplete actual installation: %+v", record)
	}
	bootstrap, e := os.ReadFile(registryPath + ".bootstrap.json")
	if e != nil {
		t.Fatal(e)
	}
	var receipt validate.BootstrapReceipt
	if e = json.Unmarshal(bootstrap, &receipt); e != nil || !receipt.Accepted || receipt.PackageDigest != record.PackageDigest {
		t.Fatalf("bootstrap content: %v %+v", e, receipt)
	}
	intake := facade.IntakeConfirmationReceipt{Source: facade.DefaultIntakeSource, Authority: facade.DefaultIntakeAuthority, Transport: facade.DefaultIntakeTransport, RequirementSource: "requirements.md", RequirementRevision: reqRevision, Artifacts: []facade.IntakeArtifact{{Path: "requirements.md", Revision: reqRevision}, {Path: "design.md", Revision: solutionRevision}}, SolutionRevision: solutionRevision, SolutionDigest: "sha256:" + solutionRevision}
	intakePath := filepath.Join(t.TempDir(), "intake.json")
	data, e := json.Marshal(intake)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(intakePath, data, 0600); e != nil {
		t.Fatal(e)
	}
	positive := runCandidate(t, candidate, home, project, "workflow", "start", "--root", project, "--run-id", "record-only", "--route", "lightweight", "--intake-receipt", intakePath)
	var light facade.Run
	if e = json.Unmarshal(positive, &light); e != nil {
		t.Fatalf("lightweight control: %v\n%s", e, positive)
	}
	if light.Runtime != "engine" || light.Route != "lightweight" || light.Envelope.PackageDigest != record.PackageDigest || light.Envelope.InstalledTargetIdentity != record.ID {
		t.Fatalf("actual installed control binding: %+v", light)
	}
	if got := runGit("rev-parse", "HEAD"); got != base {
		t.Fatalf("record-only start changed native history: %s != %s", got, base)
	}
	regularArgs := []string{"workflow", "start", "--root", project, "--run-id", "native-review", "--route", "regular", "--base-snapshot", base, "--intake-receipt", intakePath}
	cmd := exec.Command(candidate, regularArgs...)
	cmd.Dir = project
	cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "AI_AGENT=", "CLAUDE_CODE_ENTRYPOINT=", "CODEX_HOME=", "CODEX_CLI_PATH=", "CURSOR_TRACE_ID=", "CURSOR_RUNTIME=", "DSH_HOME=", "DSH_PROJECT_DIR=")
	result, e := cmd.CombinedOutput()
	t.Logf("immutable actual package=%s record=%s host=%s; public regular result=%q", record.PackageDigest, record.ID, record.Host, result)
	observation := struct {
		Record    validate.RegistryRecord          `json:"record"`
		Bootstrap json.RawMessage                  `json:"bootstrap"`
		Intake    facade.IntakeConfirmationReceipt `json:"intake"`
		Base      string                           `json:"base"`
		Args      []string                         `json:"args"`
		Output    string                           `json:"output"`
	}{record, bootstrap, intake, base, regularArgs, string(result)}
	observed, encodeErr := json.MarshalIndent(observation, "", "  ")
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if saveErr := os.WriteFile("/tmp/installed-public-start-original-observation.json", observed, 0600); saveErr != nil {
		t.Fatal(saveErr)
	}
	if e != nil {
		t.Fatalf("normal installed public regular start was rejected: %v\n%s", e, result)
	}
	var regular facade.Run
	if e = json.Unmarshal(result, &regular); e != nil {
		t.Fatalf("regular result: %v\n%s", e, result)
	}
	if regular.Runtime != "engine" || regular.Route != "regular" || regular.BaseSnapshot != base || regular.Envelope.PackageDigest != record.PackageDigest || regular.Envelope.InstalledTargetIdentity != record.ID {
		t.Fatalf("regular public admission lost native or install binding: %+v", regular)
	}
	request, e := os.ReadFile(filepath.Join(project, facade.EngineNamespace, "native-review", "request.json"))
	if e != nil {
		t.Fatal(e)
	}
	var admitted facade.StartRequest
	if e = json.Unmarshal(request, &admitted); e != nil {
		t.Fatal(e)
	}
	if admitted.Provider != "zcode" || admitted.BaseSnapshot != base {
		t.Fatalf("actual host/native request not preserved: %+v", admitted)
	}
	shown := runCandidate(t, candidate, home, project, "workflow", "show", "--root", project, "--run-id", "native-review")
	var reopened facade.Run
	if e = json.Unmarshal(shown, &reopened); e != nil || reopened.Envelope != regular.Envelope || reopened.BaseSnapshot != base {
		t.Fatalf("installed reopen lost binding: %v %+v", e, reopened)
	}
}

````

## installed-regular-start-overlay.json

来源：/tmp/installed-public-start-original-evidence/installed-regular-start-overlay.json；bytes=211；sha256=647a8b712702575d91a2d31b3a57ce55fa2813c2adfd68d009b611316e3aafe1

````text
{"Replace": {"/Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-dogfood-dsh/internal/engine/acceptance/installed_regular_start_business_test.go": "/tmp/installed-regular-start-business_test.go"}}
````

## 证据文件索引

````json
[
  {
    "path": "/tmp/installed-public-start-root-acceptance.md",
    "bytes": 10112,
    "sha256": "389c392eea8ccf12bbe59b0c254eebb692b656813c45448d4807c56b2c06599c"
  },
  {
    "path": "/tmp/installed-public-start-gate-requirements.md",
    "bytes": 6525,
    "sha256": "dc4c0903cf2ba08135923dbcd76d2a41d2cebe32071568b19b0a27aca3153c4c"
  },
  {
    "path": "/tmp/installed-public-start-execution-brief.md",
    "bytes": 21278,
    "sha256": "56344c2dbc012c4d295551ddf874366ff7e559241f57e8951fc637921d23a48f"
  },
  {
    "path": "/tmp/installed-public-start-implementation-report.md",
    "bytes": 18675,
    "sha256": "2e392658acb8359cef327ec3f623773f4f2db5f8c855789c6d30d2776faabd9d"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba.md",
    "bytes": 20888,
    "sha256": "2391fe8dcff8dea82616ebef41226c5eae17d575818d58e42dcb1b6181707d4b"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba.md",
    "bytes": 28031,
    "sha256": "c00376ff4883200357c1e50709126ea1611423beff3a9a12a61e7f18089a6333"
  },
  {
    "path": "/tmp/installed-public-start-original-evidence/index.json",
    "bytes": 778,
    "sha256": "423db0f29c835fca629d250ddce731bdac4acffc921c6d544b753a613a8fb38f"
  },
  {
    "path": "/tmp/installed-public-start-root-after.log",
    "bytes": 3659,
    "sha256": "7c4198f04caa9bd6eee06bfef12881ed4385dea7f2592778f7fb924359dc42c0"
  },
  {
    "path": "/tmp/installed-public-start-root-after-observation.json",
    "bytes": 11012,
    "sha256": "7955fa9522d32bbdbde26a4b0d74b463ab0d7085b094b0de417f7cd13ed05149"
  },
  {
    "path": "/tmp/installed-public-start-root-boundaries.log",
    "bytes": 488,
    "sha256": "334193441e61aa1b26708dc971e1c6614abd2ee9372b60c7f435099057ae32b3"
  },
  {
    "path": "/tmp/installed-start-root-baseline-lifecycle-environment.log",
    "bytes": 262,
    "sha256": "bbc42ecced25c987ba6ead9e88384484a1231d5609f9bddb5550f62ecea201e9"
  },
  {
    "path": "/tmp/installed-start-root-baseline-lifecycle-isolated.log",
    "bytes": 192,
    "sha256": "7a37f84d560eafa273b3a88ccaadafb529ee973d1ad44d5ea73cc046d02d2b7d"
  },
  {
    "path": "/tmp/installed-start-root-current-lifecycle-isolated.log",
    "bytes": 73,
    "sha256": "8ab71f2dd13558f64f47d9b87143727f304d65506ef9fe758d0350caa2e50407"
  },
  {
    "path": "/tmp/installed-start-root-environment-signals.json",
    "bytes": 300,
    "sha256": "d1b94434444673b2a24fad62a87aa8c69824d94d44c7741003fe98e6d5d9c77a"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/admission-with-binary.json",
    "bytes": 468,
    "sha256": "f71f24d3c0fff87afcfa2372df5e6f302428cb79800a58c1cf1cb78308f23cf9"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/admission-with-binary.log",
    "bytes": 2242,
    "sha256": "4fcb431d94501b8ae20d5fab8256dab1286a8a663ada0cdc22805128198f9f4f"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/admission.json",
    "bytes": 456,
    "sha256": "1feb4d3a192bd2ed38aa3d998ef5a4019f20858ede441514f20639e8dc1363c2"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/admission.log",
    "bytes": 7112,
    "sha256": "105e9d958b82636b54bf17aa812284e23c766abbbd6cf665622a12ad68716ab3"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/cli-writers.json",
    "bytes": 572,
    "sha256": "c71bc7c2871eca7ffed7fbfa175c2095cfb39e3efef2ba05dbad76791ae836af"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/cli-writers.log",
    "bytes": 643,
    "sha256": "6451d9c53640e82a594eb4eb730222c4b1103406ba2f3cff66dda1ded5d657f4"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/facade-regression.json",
    "bytes": 886,
    "sha256": "802037d40c2ac5b2766269ad7af0d2ad642dfa50437823a2c0bb85a2482c4eff"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/facade-regression.log",
    "bytes": 4007,
    "sha256": "cba86a695a968d12baed223520d8d059b6872bab791066723dc3be5f77fda547"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/fixed-binary-build.json",
    "bytes": 356,
    "sha256": "8f9af2ba3cc16a8b28aa30ee5a577840d16793f92268da52a30ee4e4a04a5824"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/fixed-binary-build.log",
    "bytes": 0,
    "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/installed.json",
    "bytes": 383,
    "sha256": "d23a2e2a4c9ca58fcc9d87cbe325d1075ff526e3e800aefba98e6d351669e14d"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/installed.log",
    "bytes": 458,
    "sha256": "1938c279390b9d8bd8b2d1e631953b80f00e50e9dba725a60ef9ddebb5ca2e71"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-completed.json",
    "bytes": 370,
    "sha256": "10b4a0b88ecd24b42122abc8929d70df66a90def57a4025e876e338c0bba37b8"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-completed.log",
    "bytes": 11315,
    "sha256": "c6072edbe9a6f57d3c2251078343150007fd719b572a19a330fe4c2076006880"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-continued.json",
    "bytes": 370,
    "sha256": "cb1dc2ae751bb564e810625c646dfd2a93ac1dd6a77fb9a5a0dc5818092898fc"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-continued.log",
    "bytes": 2741,
    "sha256": "09aa349d696186e5f7fefb34b374905ebb7f32e7bd6bea5880737557f054fb59"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified.json",
    "bytes": 360,
    "sha256": "7ae4055156a32fc0cb5a376176e3dcc9f00840d1cca19e1e323563f667b0f819"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified.log",
    "bytes": 8982,
    "sha256": "fde1ad57bbe487d502dddfa1fe87056bdd955806174d4e989f595ba1f71bc89b"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe.json",
    "bytes": 351,
    "sha256": "f315874085b2980cb01fcb5ca27182dc35fe4eec34feecc0fcd7dcd52a6fb7b6"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe.log",
    "bytes": 8360,
    "sha256": "d9ad6694ec730a498a83292bcb529cc9a0eaafee56bd0901ac703484392fe63a"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/admission-with-artifact.json",
    "bytes": 380,
    "sha256": "687c6e6c0a31ed7ffc8a698368b1e452b6fb27635fda0bf55dbed264c6a54baa"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/admission-with-artifact.stderr",
    "bytes": 0,
    "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/admission-with-artifact.stdout",
    "bytes": 2242,
    "sha256": "f4b86a50c75ba77ddd4c413d98e5a21605e342bc149ec300ab8237d259ea7bd5"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/admission.json",
    "bytes": 380,
    "sha256": "1d9e729d8f2e14e5f9a98c584d51dc9980f936f5b3076130adae4d7155f9d8f4"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/admission.stderr",
    "bytes": 0,
    "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/admission.stdout",
    "bytes": 7148,
    "sha256": "ffdbdd36cb901e7f57eea51f3801b2bd1c0999376675f6df29b95d4e4ca4f20d"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public-executed.json",
    "bytes": 278,
    "sha256": "541194098d1f46abe6486606f24cf16e5d9ed17a5d24520bcb08ef83bacf4a62"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public-executed.stderr",
    "bytes": 1152,
    "sha256": "bdeb1a792b93baede79117ba4f93fb5c9ffeb68d21beb7742b5f0f660752fb80"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public-executed.stdout",
    "bytes": 0,
    "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public-installed.json",
    "bytes": 277,
    "sha256": "b4bb149366b098b5f37015c05dc5f34f03826c9135fdd930fe7234e53888f7a5"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public-installed.stderr",
    "bytes": 0,
    "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public-installed.stdout",
    "bytes": 168,
    "sha256": "5599e7e35a153af436897448ea0684560ca018a408f9f5958d0d06ea324bf779"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public.json",
    "bytes": 278,
    "sha256": "fae502cd9a6920bebd4360fd1a06857f50e012260993b0f40846554c740392ca"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public.stderr",
    "bytes": 580,
    "sha256": "5d539370df1aeb6eea83925777e579e5b0b36af711af12826f3b5fe5217e4e64"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/independent-public.stdout",
    "bytes": 0,
    "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/installed-start.json",
    "bytes": 427,
    "sha256": "db6a88fabe6dd985474c3e78a765e307c5465d6189004a36d295f70c34b233d0"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/installed-start.stderr",
    "bytes": 0,
    "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/installed-start.stdout",
    "bytes": 458,
    "sha256": "cb01a580e7c2d8320c6578348f9422dc4119e0fe7fa97814cb35fb7bf2358233"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/regression-names.json",
    "bytes": 570,
    "sha256": "4192c13fa2fabf1fc46c758ee2b63728edb1639aefaa9d6d4777c32d0ef918b8"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/regression-names.stderr",
    "bytes": 0,
    "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/regression-names.stdout",
    "bytes": 5393,
    "sha256": "9a9ecccf5f856620adfb35589d5f20b06ea2177f2e13483595454cef166a25fa"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/related-regressions.json",
    "bytes": 651,
    "sha256": "87352b4c956be2cfb3c28f54126b85523d4305541d2dad7c8cf5214f577cbf88"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/related-regressions.stderr",
    "bytes": 0,
    "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/related-regressions.stdout",
    "bytes": 2443,
    "sha256": "1e3f2248dea97100985d609a978a839d685f4b1ae86c0ad9c43a9c8d02e3113c"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/writer-isolation.json",
    "bytes": 463,
    "sha256": "556bc084229f0dde92eb16990b28a6f829679e689bc6e14672699c4d65bd270c"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/writer-isolation.stderr",
    "bytes": 0,
    "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  },
  {
    "path": "/tmp/installed-start-complexity-137b0ba-independent/logs/writer-isolation.stdout",
    "bytes": 545,
    "sha256": "1ebc22ff4f80a2ab370114300d311af320460d58160a24410d19936ee4c4403e"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/commands.jsonl",
    "bytes": 19708,
    "sha256": "2b2debe213c795bf04c45a75122bfbb4f7e3799ff1fc17781be88370c51657a3"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/17.log",
    "bytes": 148,
    "sha256": "2ebb034bcc13c1ea782905a01d12e720d13647a7c3371a7e2b872325c1ca0d08"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/18.log",
    "bytes": 2917,
    "sha256": "f2a545126b888c511ef0c16c5dafb5216cf22823912456548a2caff7bc747f8b"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/24.log",
    "bytes": 88,
    "sha256": "6d879d29d95bc8319fbc5a05ff42ac195bb552552a5f8740c779f625f78f4d21"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/25.log",
    "bytes": 5762,
    "sha256": "5f1b50a5776cbcfa839a491d73c45ed516bd1d331947a3ce084a5ba548cc1f4d"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/26.stdin.json",
    "bytes": 132,
    "sha256": "885d123fe5846a3187b78a02c2b0ab7ec701c5c2cb04d5d5a84c17af80f8fc29"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/26.log",
    "bytes": 231,
    "sha256": "9c6192f0a373b3036bfedf43a4a3ca1361694711ce4ca4df0f28bd9ec359ed51"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/27.stdin.json",
    "bytes": 132,
    "sha256": "885d123fe5846a3187b78a02c2b0ab7ec701c5c2cb04d5d5a84c17af80f8fc29"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/27.log",
    "bytes": 230,
    "sha256": "66dec636ec064e946c94b628293031de221aa5e91ba866b81cd4a640b22cdc2b"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/28.log",
    "bytes": 245,
    "sha256": "2baa34eaf0e34b2f39e0373575cca637ba3b055153fd45e55ff0367daed4f691"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/29.log",
    "bytes": 53,
    "sha256": "f6f9ca48d3cd625439b5cce6f2aaaff3516213a5e90018f6b2fd3b2d7ee4c4f3"
  },
  {
    "path": "/tmp/installed-start-quality-137b0ba-evidence/public-probe-verified/bindings-summary.json",
    "bytes": 658,
    "sha256": "e2cc20bc6af9867413c53791d53a05d45b2b6dac8e517ed59e868b501e2360d9"
  },
  {
    "path": "/tmp/installed-public-start-acceptance-boundary-observation.log",
    "bytes": 4044,
    "sha256": "1a8cc89de9b326eac8178e2d6d3c1ba304dcc6e7c5feebd36c12c39f449c949d"
  },
  {
    "path": "/tmp/installed-public-start-acceptance-compilation.log",
    "bytes": 316,
    "sha256": "a8c11afccee233f506ab9b9c177a982339b3071eeeb872e3dcfb265359ec94a1"
  },
  {
    "path": "/tmp/installed-public-start-acceptance.log",
    "bytes": 458,
    "sha256": "a02bf910670d5b8bd9a8954d6a19603935ccc43239be2d0c80108a6d68ead565"
  },
  {
    "path": "/tmp/installed-public-start-added-boundaries.log",
    "bytes": 330,
    "sha256": "9979ac01d1a49952a3e59acbe41d86c6dc52ec1bfc7b7ebe698845fe00d34aa3"
  },
  {
    "path": "/tmp/installed-public-start-admission-race.log",
    "bytes": 73,
    "sha256": "023b35b22c2521eff039d68385735fe499e0e0d8aa28d2bfae02f25f21315413"
  },
  {
    "path": "/tmp/installed-public-start-admission.log",
    "bytes": 2242,
    "sha256": "cd9ed190d74fd6eea4b79cb27fc28c39bbc0a7fab9e3d381f0afc0693449a53f"
  },
  {
    "path": "/tmp/installed-public-start-build.log",
    "bytes": 29,
    "sha256": "a634dca47a59197bc1b8e926333a14711c9aec925b3435d59f6e86e88dac32ae"
  },
  {
    "path": "/tmp/installed-public-start-cli-race.log",
    "bytes": 69,
    "sha256": "cc238119604e95905a621bea6fb245dcd4352c4662f84590e3c87c0b9beb3454"
  },
  {
    "path": "/tmp/installed-public-start-developer-final-probe.log",
    "bytes": 3660,
    "sha256": "41ff5fac789d461e15d055ca1e61de97fa6c8c67fb196c4e6ebdadcc6217aaa7"
  },
  {
    "path": "/tmp/installed-public-start-developer-original.log",
    "bytes": 632,
    "sha256": "59a627d2a792ef4f3a7fe5f4d67e9e23d1dddfb4583d976ea58144c388f51ae1"
  },
  {
    "path": "/tmp/installed-public-start-developer-repaired-probe.log",
    "bytes": 3631,
    "sha256": "428aa6b3ebad62e4f60b5bb928432245be4013ca13d12b3eff53717c8cb70640"
  },
  {
    "path": "/tmp/installed-public-start-facade-race.log",
    "bytes": 77,
    "sha256": "aee055d185d9e05751634aef5f1c539117ae02b93bb2f68ddd69ae41cdd7f82c"
  },
  {
    "path": "/tmp/installed-public-start-lifecycle-inherited.log",
    "bytes": 262,
    "sha256": "ca1d31d60189553e79b9370d117678402e5946c062330e88e795863836717221"
  },
  {
    "path": "/tmp/installed-public-start-lifecycle-isolated.log",
    "bytes": 5744,
    "sha256": "9f24561e021583d1e7140926c6b8fb6fc87b3976b300a8ac0db4900ae2923ce0"
  },
  {
    "path": "/tmp/installed-public-start-original-compilation.log",
    "bytes": 785,
    "sha256": "0a90d33759b74142d3c1c60c07c3375b194f78960300258be7777b0761135a53"
  },
  {
    "path": "/tmp/installed-public-start-original-failure.log",
    "bytes": 632,
    "sha256": "375470e5d1c93ff88392df5c28473683d53498dd856c5e0f5814d8ec1ce0be8b"
  },
  {
    "path": "/tmp/installed-public-start-packages.log",
    "bytes": 487,
    "sha256": "c8c079916933897475accf91e6e3c026054823b3be4fd697c46bad2f5eba3a73"
  },
  {
    "path": "/tmp/installed-public-start-original-evidence/installed-public-start-original-compilation.log",
    "bytes": 785,
    "sha256": "0a90d33759b74142d3c1c60c07c3375b194f78960300258be7777b0761135a53"
  },
  {
    "path": "/tmp/installed-public-start-original-evidence/installed-public-start-original-failure.log",
    "bytes": 632,
    "sha256": "375470e5d1c93ff88392df5c28473683d53498dd856c5e0f5814d8ec1ce0be8b"
  },
  {
    "path": "/tmp/installed-public-start-original-evidence/installed-public-start-original-observation.json",
    "bytes": 7862,
    "sha256": "78768115fccb184f1e9711a87f8266d11009610c8ec5a92d1da47f8081c617f9"
  },
  {
    "path": "/tmp/installed-public-start-original-evidence/installed-regular-start-business_test.go",
    "bytes": 6784,
    "sha256": "0c6514f122cfd15070ff6e13bf3fb1742c672c5699421b62e8b680d65faa3b25"
  },
  {
    "path": "/tmp/installed-public-start-original-evidence/installed-regular-start-overlay.json",
    "bytes": 211,
    "sha256": "647a8b712702575d91a2d31b3a57ce55fa2813c2adfd68d009b611316e3aafe1"
  }
]
````
