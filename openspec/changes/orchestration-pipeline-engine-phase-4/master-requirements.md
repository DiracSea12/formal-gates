# 阶段 4：Git 非分片 regular 全流程需求与技术方案

## 状态与目标

本文件是阶段 4 唯一正式需求与技术方案。原受理 run `phase-4-git-regular` 已绑定并确认
此前 revision，用户已选择 full 路线。本次修订保留 full 决定；更新后的文件须按正式
需求变更流程绑定并确认精确 revision，原记录不自动成为本修订的审查或确认结果。
用户于 2026-09-07 要求“现在可以开始阶段4的开发了”，随后明确“走流程”。此次将已接受的总计划及 3.5
系列补充落为本阶段可确认、可验收的单一文件；不把开始开发的指令记成已完成正式需求确认。

2026-09-07 用户在对照审查后确认：补齐 token cap 的配置与实际止损，插件全部用户可配置项
都应方便用户手改和 AI 修改；代码/质量门统一在开发全部完成后审查，取消组间增量门审，
实现保持易于后续迭代；补齐跨产品/技术阶段复审、独立并行任务失败隔离，以及单个操作
内部固定步骤的顺序强制与恢复验收。这些是本次需求修订的来源，不替代修订后完整文件的
正式 revision 确认。

同日用户进一步明确：“阶段 4 交付后，将全局插件切换到阶段 4 的版本”。因此本阶段
Seal、主线集成及必要重验完成后执行全局发布，替代原先等到阶段 7 才首次切换的安排。
切换后还须逐项检查阶段 0、1、2、3、3.5a/b/c 及其修正交付的功能在新版中能否正常使用；
检查结果纳入发布完成条件，不能只确认版本号或插件能启动。

同日用户明确“用当前版本开发”，并要求按插件内部封闭流程的职责修改方案：顺序、
前后置条件和失败分支由插件程序控制；宿主接入随对应功能实现和验证，不把“先验证
Codex 能否拦截”作为整个引擎开发的前置条件。独立探路须有具体未决接口风险作为依据。
此前“先独立探路、再完整实现”的两批建议须据此重新评估，不直接沿用，也不因开发
驱动缺少批次入口就自动合并成一个代理；阶段 4 的多批次交付能力与验收保持不变。

阶段号指 `refactor-plan/incremental-seal-plan.md` 的增量 Seal 阶段 4，即在一个真实宿主
完成 Git no-split regular 的完整 engine 交权，对应总体方案工作包 3 的剩余完整流程迁移。
用户能够经 start/drive/submit 完成受理、审查、开发、QA、返修、最终候选验证和 Seal，
包括需求修订、明确保留已有 PASS、成本止损、中断恢复及真实执行前准入。

阶段 4 的完整能力先在不可变隔离候选安装中成立，交付后将全局插件切换到该版本。
阶段 5 的 Git child/merge、阶段 6 的 SVN/P4 与其余宿主继续按原范围开发；旧入口及
运行时兼容逻辑在阶段 4 删除，阶段 7 核对无回流并完成完整版本发布。

2026-09-08 用户明确要求“兼容代码都删掉”。阶段 4 交付包删除旧工作流公开推进命令、
别名、兼容转发分支、legacy writer/QA/轮次与授权逻辑、旧运行态 decoder/migration、
legacy mode、authority handoff 和旧流程 cleanup/门禁后门，不再延期到阶段 7。
仍承诺的正常业务能力由 engine handler 承接；安装、配置、成本解析等仍使用的通用能力
按当前合同保留，不能因其曾被旧流程调用就一并删除。当前正式开发 run 继续由已经冻结、
与候选隔离的旧 driver 完成；该冻结工具不进入阶段 4 交付包，不构成新版本的兼容通路。
历史需求、原始结果与 Seal 不改写；旧 run 仅供最小只读原始诊断，不迁移、不续写。

本次只补齐已确认的任务内部准入及操作顺序能力，不降低 AC-051/052/076：整项 HOST
认领和最后接纳结果不能替代任务内 model/声明 QA 操作的调用前认领；QA 准备、目标操作、
结果采集须有程序持有的实际调用路径，跳步或前置失败后继续必须在底层调用前拒绝。
已完成开发的 B1/B2 保留原交付事实，未完成的 B3 继续由原开发代理完成缺项和旧逻辑退役；
用户此前“不再重跑产品技术审”的决定保持，保留相应原审查结果及授权来源。

## 来源、事实与对齐

### 需求来源

- 总行为权威：`openspec/changes/orchestration-pipeline-engine/master-requirements.md`。
- 架构与细节：`refactor-plan/final-implementation-draft.md`，特别是 §4–7、§8.3、§9.1.1。
- 本阶段范围与退出条件：`refactor-plan/incremental-seal-plan.md` 的阶段 4、§2、§4–6。
- 逐入口迁移：`refactor-plan/route-matrix.md`。
- 3.5a 覆盖和保证、3.5b 审查收敛和 guard、3.5c QA 提效的替代关系：
  `references/requirements-precedence.md` 的 2026-09-07 条目；历史正式需求和 Seal 不改写。
- 正常使用、常见误操作、范围与严重度只沿用 `prompts/reviewer-base.md`。

上述文件是本文件的来源，正式受理时不另列为多个待确认 requirement artifact。
本次用户确认的“开发全部完成后统一门审”替代总计划与 ADR-002 中可选的组间增量门审。
“阶段 4 交付后全局切换”同时替代阶段 7 前一直由阶段 0 driver 驱动及最终仅切换一次的安排。
本次“使用当前版本开发”替代阶段 4 仍由阶段 0 包驱动的环境约束；宿主接入验证不再
自动构成独立首批探路。除此之外，本文沿用总需求；若实施需要改变总需求或主要技术
选择，先完成相应变更确认。

### 已核对的起点

- 3.5c sealed candidate 为 `517de91f1b480ec969bd91385c29ce38d8bc2841`。
- `.gates/results/stage-3-5c-post-integration.json` 记录集成 identity
  `9518a5c43d133794bc47e632351eaf7c142a203e`；首次核对时 main 后继提交为 `01dd5f4`。
- 上一轮已接受的总计划修订、对应契约测试和历史结果文件已原样整理为 main 后继提交
  `da23375bc2b91029c22bfadcfa4b2457b0c5022e`，作为本阶段源码基线；历史 Seal 不改写，
  不把这些后补文档与结果索引说成 3.5c 候选的实现变更。
- 新会话首次核对未发现 `.gates/tmp/*/state.json` 残留；本阶段按新受理 run 启动。
- `internal/engine/facade` 当前仅开放 lightweight，并在 start 明确拒绝 regular；
  `internal/engine/protocol` 已有单一 State、pendingActions、CAS、submit/receipt 与恢复基座。
- `internal/engine/coverage`、`internal/engine/guard` 是可复用的纯合同/计算核心，
  legacy 的审查、QA 和 cost/source 交付不是 regular engine 的既有运行时能力。

### 方案选择与未决项

沿用 Go 封闭类型与静态定义、唯一 state.json、现有持久协议和单一 cost 投影；不增加 DSL、
数据库、daemon、第二 QA 状态机或第二计量账本。Git 非分片是本阶段产品范围，不预先决定
实施本阶段时的 Batch 划分或正式 run 路线。

用户已明确选择 Codex 作为首个真实宿主，复用已有 provider、transcriptPath 和计量能力。
插件内部按封闭定义实现流程决定、操作顺序和失败处理；这些规则不交由 Codex 判断。
宿主 adapter/bridge 负责真实调用与事实回传，原生派发/恢复、模型 CLI/API 与声明的
QA 操作随对应能力实现并逐项验证。接入验证不决定是否实现固定流程；只有具体未决
接口风险才安排有明确问题与验收目标的小实验，阻塞范围限于依赖该接口的工作。
仅有 lifecycle 事后观察、提示词约束或 wrapper 自报不能证明真实入口受控；若实际
存在无法覆盖的入口，先呈现证据并处理接入方案，不得静默换宿主或降低验收。

用户已明确要求走正式流程并选择 Codex，无待澄清问题。本文件仍需在受理 run 绑定、
预检和全文展示后，取得精确 revision 确认。局部命名和类型拆分由开发决定，不追加无
实质影响的确认；已选择的 full 路线保留，调整后的开发粒度在规定节点确认。

## 技术方案

1. 在既有 authoring/compiler/definition 链中编写 regular 的封闭节点和 StepSpec，替换
   演示流程的对应职责。控制流仍为 Observe → Decide → SelectIssued；定义与 registry
   同候选绑定。协议 State 承载新的业务投影，decision 只读取确定性判断所需的视图。
2. facade/CLI 仅解析、路由、提交 typed request/event 和展示。start 是 bootstrap，drive
   推进确定性步骤，外部事实经 submit 接纳。已绑定 engine 的 run 不调用 legacy writer；
   仍需的纯校验/解析能力保留为通用能力或内收 engine handler，删除旧 writer 及兼容调用侧。
3. 受理、需求确认、模式、拓扑、路线、审查 series、覆盖与 guarantee、wave/repair、
   用户保留和 cost/guard 都落在现有 State 的有界 typed 投影中。保持原始证据、裁决、
   授权继承与有效状态分层；Controller 校验结构和机械路由，Operator 判断语义。
4. 在现有提交事务中接入结果/receipt、source resolve/backfill、usage、guard 和 refill。
   source I/O 与外部事实按现有 observation/intent 协议采集并在提交前校验绑定，未结算前
   不补位；不在无界等待中持锁，也不另外追加绕过事务的成本状态写入。
5. Git adapter 负责跟踪、提交、快照、白盒工作区、最终候选整理、promotion 与 cleanup。
   开发代理只交编辑结果和路径 manifest，审查者只读绑定的 snapshot/diff artifact。
   独立副作用采用现有 intent/reconcile，不能靠模型判断 VCS 命令是否已经成功。
   白盒改动由 TestOwnershipResolver 判定 test-owned 与生产视图是否不变；同步候选中的
   实现质量门、catalog scope 和 README，使测试质量归白盒 Review，门排除 test-owned 文件。
   需求覆盖、实现偏差、架构与耦合、实现正确性与质量四个门内维度继续显式并列。
6. 首宿主 adapter/bridge 在底层执行前对已签发动作机械认领，复用 pendingActions、
   锁/CAS、intent 和 guard。模型任务内 QA 请求继承原任务边界；真实入口矩阵列出校验点
   和零调用证据。能力缺失先拒绝承诺该保证的模式，不以新增自由 shell 通道补洞。
   流程推进及每项操作内部的固定顺序由插件的 engine/adapter 程序统一控制；宿主只执行
   已校验调用并回传真实结果。不能仅封装 formal-gates 命令，却让本工作流的同类实际
   操作通过未校验的裸入口执行；也不扩展为控制任务外工具或防御恶意改写宿主。
7. QA 用同一 coverage 合同和已确认 sources，保留共享审批、逐例 oracle、建议理由处置、
   checkpoint、白盒已有测试复用。最终候选整理完成且 case-set FROZEN 后，再建立唯一
   review-wave 并按容量并行派发。Controller 计算重跑范围及合法继承。
8. schema/definition/package/installed identity 均精确绑定。候选 schema 或定义确需改变
   时更新相应版本及生成制品；旧 generation 不迁移、不双写。已承诺的 lightweight 能力
   在当前候选重新运行，历史 engine 状态保持原安装绑定或按版本合同拒绝。
9. 审查时点、范围选择与结果接纳保持清楚的代码边界：时点由封闭定义/纯策略判断，结果
   仍走现有 TaskKey、候选绑定和 submit。当前只开放开发全部完成后的门审；未来调整时点
   可修改对应定义/策略及检查，不需要重写 CAS、动作协议或成本模块。本阶段不预建组间门审
   分支、第二状态机或用户自定义工作流配置。
10. 全部用户可配置项遵循 REQ-021 的可发现、可读、可修改约定；token cap 复用该配置方式。
    配置读取/校验与 guard 判断分开，生效值与来源明确；次数额度和授权记录仍由 engine
    派生，不把内部 state、digest 或 receipt 变成需要用户手改的配置。

### 实施依赖与验证安排

以下是方案的依赖顺序，不是已确认的 Batch 或派发清单。Part 2 技术审应评估多个独立
验收单元与开发分批，形成实际 granularity_review，并在规定节点确认；不再选择组间门审档。
宿主接入是对应功能的实现与验证工作，不自动设置“探路通过后才允许开发引擎”的屏障。
只有列明具体接口疑点、受影响功能、最小实验和通过判据后，才评估是否值得独立 spike；
若独立成批，只约束依赖该结论的后续工作，实验代码不混入交付，结论和真实证据留痕。
本次批次计划须重新核对独立交付物、依赖和交接成本；不预设两批，也不自动改单批。

| 工作顺序 | 本段可核对的结果 | 依赖与验证 |
| --- | --- | --- |
| 业务定义与受理/审查 | regular 版本绑定、唯一文件确认、模式/路线及开发前审查闭环 | 复用现有协议；类型/定义拒绝、公开 CLI 正反例、重启重放 |
| 宿主接入实现与验证 | 程序化派发/恢复/QA 调用、入口绑定与真实执行证据 | 随对应功能实现并验证；真实调用前必须完成动作校验，不以提示词或事后登记代替 |
| 开发、Git 与 QA | 开发和黑盒并发、白盒工作区、共享/逐例审批、候选和 wave | 前段绑定稳定；覆盖缺口、checkpoint 和候选变更回归 |
| 修订、返修与恢复 | 用户保留、唯一 repair、纠正、FULL/AFFECTED、控制请求和 cleanup | 复用前段结果模型；跨阶段序列与故障窗口验证 |
| 成本与宿主整合 | source/guard/refill 原子顺序、真实执行前准入、完整 regular 闭环 | guard 自签发能力启用起即生效；整合段不得补救此前可绕过的公开入口 |
| 独立收口 | 不可变隔离安装、installed binary 回归、真实 canary、Seal/集成凭据 | 不并入开发批次；最终候选身份、三环境隔离和资源对账 |

每个实现段的测试与代码同批交付；相邻段复用已冻结的接口与正常拒绝路径，不提前暴露
可绕过 guard、候选或授权屏障的 regular 分支。批内执行模块检查，批边界运行声明的
回归；正式审查只按确认的粒度与路线执行，不因上述顺序自行增加派发。

### 本阶段环境绑定

- 源码基线：`da23375bc2b91029c22bfadcfa4b2457b0c5022e`。
- 开发分支：`codex/stage-4-git-regular`。
- 开发 worktree：`/Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-stage-4`。
- 原开发驱动源码为 `77a82c863eb726468e553f1ec3589e8d09b47b5f`。用户在确认技术审
  `finding-83545576a81d976f` 后，明确要求“那这次补上，按通用方法补”。本次驱动修复
  为独立于阶段 4 候选的提交 `ee990ba931ba5cbcc8b362dc712b953f8a6819b5`，补齐通用
  开发批次登记；本环境修订确认后使用该精确修复版，不随“最新版”滚动。
- 稳定 launcher 仍为 `/Users/suxinyang/.local/bin/formal-gates`；package 仍为
  `/Users/suxinyang/.codex/skills/formal-gates`。上述修复版已在隔离 Codex 安装中验证：
  source/package digest 为 `154f7948f16a9a28547fce6f4253245acbea451d54d37089e770fbef75d3bab8`，
  installed-target digest 为 `12bfb126ee01fc89fd84c683250c00cf3521385f49060de99a614e5a0b0ac583`，
  binary SHA-256 为 `82c31c43750836c927a0a158059c27e798f3f6d7a3bf2ef012242d517cecea9d`。
  这些是待切换驱动的精确身份，不能把隔离安装写成全局已升级；切换后须用实际安装回执、
  package 与 launcher 核对同一身份，再继续正式派发。阶段 4 候选不能驱动自身流程，
  后续安装变化不得静默改变活动 run 的 driver。
- 候选验证根：`/Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-stage-4-candidate`；安装、测试项目、host config、state 和 resources 在此根下分别登记。
- 原受理 run `phase-4-git-regular` 保留原 driver/revision 绑定。当前版实际读取时因缺少
  `preDevelopmentReviewSeries` 拒绝旧状态；不得手改字段迁移或让新版续写原 run。
  后续正式开发由当前版以新 run ID 绑定本文件；旧 run 的结束/清理由原版支持的流程
  另行处理，本次文档修订不删除或伪造其终态。已有 full 选择保留，旧审查和 QA 输入
  是否适用按实际修订核对，不自动抄成新版本 PASS。
- 原驱动只支持续跑历史批次，缺少新计划登记入口。修复版通过
  `workflow slicing --batch-plan <plan.json> --user-confirm` 登记单批或多批计划，校验
  子任务完整归属、依赖、接口/状态边界、风险、DoD、验证、恢复、交接和上下文成本。
  批次与现有派发及 TaskKey 绑定，完成进度从派发记录推导；当前驱动按已登记顺序串行
  开发，批间不启动门审，最后一批仍受原有 QA 快照条件约束。用户和 AI 均可编辑输入
  JSON，登记后改变契约须经需求/方案修订、技术复审和确认再登记；中断恢复保留已完成
  批次，封板保留各批证据。格式见该修复版 `references/development-plan.md`。
  技术复审须核对这次修复的真实入口与适用粒度；本补丁不替代阶段 4 要交付的封闭执行
  引擎、批内操作顺序强制及其他验收能力，也不替用户决定本阶段最终批次安排。
- 本阶段的 Seal 由本次锁定的当前版本 driver 完成。阶段 4 全局发布成功后，从发布的精确
  post-integration package 冻结独立的阶段 4 stable driver；其绝对路径和安装身份记入发布
  receipt，阶段 5–7 的正式 run 使用该版本的 Codex/Git no-split engine 路径。

## 需求点

### REQ-001：唯一 writer 与完整 regular 公共路径

#### 要求

Git no-split regular 在候选公开 start/drive/submit 及只读投影中完整运行。
run 一次绑定 runtime、schema/definition、definition digest、package digest 和安装身份；
loader 写前核验。阶段 4 产品只提供 engine workflow；删除旧公开推进命令、别名、
兼容转发及 legacy writer/QA/旧轮次逻辑。旧 run 不迁移或续写，仅允许最小原始只读诊断。
已有且仍承诺的业务语义内收 engine handler；共享安装、配置与纯解析能力按当前合同保留。

#### 验收条件

- AC-001：从实际候选安装完成 regular bootstrap、推进、外部事件提交、只读查看；
  全程唯一 engine writer。已删除的旧命令由未知命令处理统一拒绝，不能重新进入兼容
  handler、创建旧格式 run 或绕过确认、freshness 和 scope；源码、注册表及最终安装包均无
  第二套 workflow 写入/执行路径。
- AC-002：未知/不匹配版本、定义、实现包或安装身份写入前拒绝；diagnose 可原始只读诊断，
  current/terminal/missing/corrupt 路径有明确结果；旧状态不被转换或改写。
- AC-003：本阶段完成全部保留 workflow 和维护入口的 route matrix，包含只读 coverage、
  guard-preview 及 lifecycle observation 边界；未支持的入口拒绝，现有 lightweight 能力回归。

#### 来源

engine 总需求 §3–5；增量计划阶段 4 与 §2.3；route matrix。

### REQ-002：regular 定义、确定性推进与外部边界

#### 要求

所有本阶段流程步骤进入封闭定义及同候选 registry，沿用既有编译、状态和动作协议。
代理只承担语义判断、创造性实现和独立审查；确定性工作由 engine/adapter 执行。
NextResult 只为 Ready/HostAction/Ask/Wait/Operator/Complete。Ready 同时绑定 spawn
transport 与 worker result；HostAction 只使用原有三类 union，无任意命令执行扩展。
单个操作内部已知的先后顺序、前后置条件与失败分支也由 engine/adapter 程序执行，不能只
把整项操作交给子代理后依赖提示词遵守。可原子恢复的纯步骤保留在一个 handler 内；需要
独立副作用恢复或跨宿主执行的步骤用既有 StepSpec/intent 跟踪。主代理和子代理都只能执行
当前已解锁步骤，不能跳步、倒序、重复已完成的副作用或在失败前置后继续下游操作。

#### 验收条件

- AC-004：regular 公开路径实际触达六种边界，同 state/observation/definition 产生相同
  canonical Plan；容量 C 和 eligible N 下签发恰好 min(C,N)，同功能在途派发约束成立。
  逐项核对 Ready 同时绑定 spawn transport 与 worker result，HostAction 仅使用既有三类 union，
  不提供任意命令执行通道；派发中代理只承担语义判断、创造性实现和独立审查，
  本阶段确定性工作实际由 engine/adapter 执行。
- AC-005：乱序、缺依赖、未知步骤、重复签发及未绑定 registry 的计划被拒绝；沿用八类
  非法定义 fixtures。本阶段 executable 路径不含未实现 adapter marker 或假成功 handler。
- AC-006：相同 event ID/digest 重放不增 revision、不重复调用；冲突 digest 拒绝。
  中断/UNKNOWN receipt 先对账，不重复 spawn；已完成前缀不重放副作用。
  用例修订使轮次 A 失效后，即使代码快照和需求版本不变，新轮次 B 的结果也须按
  当前任务/轮次精确绑定接纳并推进；A 的迟到结果只留原审计，不能吞掉 B 的合法结果。
- AC-069：从 regular 公开路径并行运行 QA execution 与多个门；一个任务 FAIL 或
  RUNTIME_ERROR 后，其他依赖与候选绑定未失效的独立任务继续，不能提前返修或停止兄弟任务。
  容量释放且 source/guard 条件满足时自动补满待执行任务；需要串行握手时仅限制未 attach
  的启动，已 ACK worker 和其他兼容容量继续并行，不把同一 QA 内的用例隔离当作此项证据。
- AC-076：逐项验收本阶段每个已声明有固定顺序的操作，包括真实 Git 提交/快照的
  “校验 manifest/范围 → track → commit → snapshot”和宿主 QA 操作的准备、目标操作、
  结果采集；这两个例子不替代其他操作的逐项核对。每项合法顺序实际完成；
  主代理或子代理请求跳步、倒序、重复已完成步骤、前置失败后继续时，在底层操作前拒绝，
  被拒步骤调用数为零。每个独立持久步骤的中断恢复只续未完成部分，已成功提交不重复提交；
  对原子 handler 验证内部调用顺序与失败短路。仅有大阶段顺序测试、提示词或结果登记拒绝
  不构成本项证据；复用现有 StepSpec/动作绑定，不为每条内部语句新建 TaskKey 或账本。

#### 来源

engine 总需求 §5；最终方案 §2–3、§6、§9；ADR-001。

### REQ-003：单一文件受理、需求确认与模式绑定

#### 要求

完整需求与方案先写唯一正式文件，start 绑定受理 run，结构预检后展示全文和精确 revision，
再由 typed submit 记录用户确认。未明确模式时保持未绑定，确认后初次选择 regular 或
lightweight；后者沿用同一文件并标 unverified。已绑定 regular 不转 lightweight。
确认前不选路线、不派发后续任务；start 不接受或冻结 split 意向。

#### 验收条件

- AC-007：预检失败、文件变化、缺失/过期/不匹配确认均不能进入后续派发；合法确认与
  文件 revision、预检/投影绑定一致。任意多个 requirement artifact 不能替代唯一正式文件。
  实跑 start 绑定受理 run → 结构预检 → 展示完整需求与方案全文及精确 revision →
  typed submit 接纳确认；确认前保持路线未绑定，不允许提前选择路线。
- AC-008：未绑定模式的受理 run 可在确认后初次选择两种模式；regular 不可转轻量。
  只有 start-readiness PASS 后绑定 no-split 理由，启动阶段改变拆分意向无需重建。
  初选 lightweight 后仍绑定受理时的同一需求文件，并明确显示 unverified。

#### 来源

engine 总需求 §6.1；最终方案 §4.1；增量计划阶段 4 的 regular 受理条款。

### REQ-004：路线、需求保证与覆盖来源的原子激活

#### 要求

regular 在技术审和拓扑确认后选择 full/custom；确认前无路线绑定。State 显式持久化
RequirementGuaranteeActivation、来源和完整 REQ/AC 投影。保证只在单文件结构预检、
用户确认、投影冻结并选择 full 或含至少一个 QA 类型的 custom 后原子激活。
多个 QA 类型以并集覆盖；无 QA、lightweight、明确 waiver 保持各自非保证语义。

#### 验收条件

- AC-009：full/custom+QA 的合法组合显式激活；缺前置条件、额外 artifact 或试图从
  route/文件形状推断激活均被拒绝，失败无部分写入。
  分别验证 full/custom 只能在技术审 PASS 和拓扑确认后绑定；缺任一前置时，
  不仅不得激活保证，也不得提前绑定路线。
- AC-010：逐 REQ/AC 展示来源、批准用例、执行/授权继承与保证状态；所有 AC 必须闭环，
  非保证路线不能输出完整需求保证 PASS；多个 QA 类型覆盖可合法合并。

#### 来源

engine 总需求 §8.4.1；增量计划阶段 4 的 guarantee activation 条款；3.5a-fix。

### REQ-005：开发前审查、分类及语义纠正

#### 要求

产品审后技术审，reviewer 只产原始候选。Operator 逐条核实范围、严重度、原因和正常路径
证据，用户处置有效 finding。技术审 P0–P3 每项必填分类，只有需求实质冲突、模块职责/
依赖方向根本错误、核心验收缺可执行入口三类可形成 P0/P1，实现细节最高 P2。
State 分别保留 RawReviewCandidate、Adjudication/CorrectionReceipt 和派生的有效状态。
产品修订改变技术前提时，先完成产品新鲜复审，再做新鲜技术审；技术修订改变产品语义时，
先回完整产品复审，再按受影响技术前提完成技术审。新鲜复审使用全新的零上下文代理读取
完整最新文档，不以复用原审查会话或局部 diff 代替。明确保留既有 PASS 的例外按 REQ-015。

#### 验收条件

- AC-011：缺分类、非法实现细节 P0/P1、错误绑定或不完整候选只形成 rejected receipt
  并结束 attempt；三类合法阻塞经核实和用户处置进入正确路径，不能由 Controller 猜语义。
  初次受理实跑产品审通过后才进入技术审；产品审尚未通过时不能提前派发技术审。
- AC-012：每条有效 finding 的 Ask 包含问题、正常路径后果、定级理由和 remedy；唯一完整
  remedy 可随 confirm 一起绑定，有实质选择则补充澄清。P0/P1 确认修订后 fresh review，
  驳回且无其余 blocker 直接推进；P2/P3 处置不因等级本身触发复审。
- AC-013：用户可通过绑定 finding/result digest、revision、旧新值和理由的纠正改变有效
  状态；原候选与历史不删除。清除误阻塞立即推进，新增有效阻塞使依赖 stale，冲突纠正拒绝。
- AC-070：分别验收产品修订影响技术前提、技术修订改变产品语义的跨阶段链路；未获明确
  保留时，前者产品复审 PASS 后才做新鲜技术审，后者先回完整产品复审再处理受影响技术审。
  对需要新鲜复审的动作实际派发新的零上下文代理，沿用各动作已有轮数/授权，不跳过或清零；
  用户明确保留的结果按 REQ-015 生效。
  逐次核对需要新鲜复审的产品审和技术审均读取完整最新文档，不以局部 diff 替代完整输入。

#### 来源

最终方案 §4.4；审查收敛需求 REQ-002 及开发前语义纠正合同。

### REQ-006：独立计数、有限重试与明确重审

#### 要求

产品/技术审各自持久化 PreDevelopmentReviewSeries，默认前三轮语义审查；只在确需
fresh review 时派发，超限经 action-specific Ask 一次增加一轮。三类 pre-wave QA review
series、开发后 repair wave、TaskKey MaxAttempts 与上述语义轮数独立。
无效结果和运行错误默认同 TaskKey 最多两次；追加 attempt 绑定当前 revision 与理由。
相同 OPEN signature 续用、相同 PASS 不自动重派；用户明确重审保留原结果且遵守额度。

本阶段黑盒、白盒 pre-wave review 各自连续三次 FAIL 或长期不可用时，返回 typed Ask，
提供重新设计、fresh review、修改需求、对精确 case-set/candidate 带理由 waiver/skip
或 abort；不计入开发后 wave，merge QA 的接入仍留阶段 5。开发后首次完整 expected-set
终结且裁决完成计 wave 1，后续需要复审的修复快照各计一轮；装配、pre-wave、未收齐、
未处置错误或未完成裁决不计，纯门审 P2 修复不计。wave >= 3 仍有 obligation 时，一个
Ask 同时提供恰好一轮额外修复、再试 QA、对精确 obligation IDs 授权 waiver、用户需求
变更和 abort；每个额外失败 wave 后重复同型 Ask，授权不预先累积。

#### 验收条件

- AC-014：有效完整语义审查只计一轮；runtime error、无效/未完成/stale 结果不计语义轮。
  需求变更、carry、adopt-external 不清零 series 或已完成 wave；纠正不凭空消耗新轮次。
- AC-015：第 4 轮需对应动作授权，不能转给其他动作或累计预授权；retry 不改变语义轮。
  wave/非 wave 无效结果均终结 attempt，耗尽明确 Ask/RETRY_EXHAUSTED，不伪装 Wait。
  无效结果和运行错误默认同 TaskKey 最多两次 attempt；未经追加授权不能启动第三次。
  追加 attempt 必须绑定当前 revision 和非空理由，缺失或过期绑定不能放行。
- AC-016：同 PASS 自动派发去重；明确重审缺理由、授权或适用额度时拒绝。合法重审保留
  原结果并消耗正确额度；一次追加 attempt 再失败不扩张，不通过新 TaskKey 绕过 retry。
  相同 OPEN signature 续用原派发，不重复创建派发或消耗新的 attempt。
- AC-058：黑盒、白盒 pre-wave review 各自三连 FAIL 或长期不可用进入对应 Ask，选项与绑定完整。
  两类计数互不挪用、不计开发后 wave；waiver/skip 只作用于授权的 case-set/candidate，
  不伪装执行 PASS，运行错误 attempt 与语义审查计数独立。
- AC-059：完整开发后 wave 按规则计轮，第 3 轮后仍有义务时阻止未授权的额外返修并展示统一 Ask。
  验证五类处置选项、每次只授权一轮及额外失败后再次 Ask；未收齐、错误/裁决未处置、
  需求变化打断的 wave 和纯 P2 修复不误计，已有完成轮数不因需求变化清零。

#### 来源

engine 总需求 §6.3、§8.4；最终方案 §4.3–4.4、§7；审查收敛需求 REQ-010；3.5b 有限 retry 合同。

### REQ-007：开发计划、隔离与 Git 责任

#### 要求

技术审形成 granularity_review，按既有 Batch 判据确认映射、依赖、DoD、验证、恢复和
交接。Batch 是现有 TaskKey 的分组，不新增生命周期。开发按已确认 Batch 派发，子步骤
不各自换代理。开发开始与基线隔离的黑盒设计/审查并发；Git 写入由 adapter 执行。
每个 Batch 使用全新的零上下文开发代理，批内连续、批间不复用会话；跨批只通过已提交
产物和任务书交接，任务书只写不能从仓库恢复的决策理由、已知问题/债务及下一任务。
Batch 契约、接口、依赖或 DoD 改变时，旧计划与未完成派发失效，暂停受影响开发，重新
进行 granularity_review 并取得用户确认后才可按新计划派发。
插件内部的开发代理续用与替换由 engine/adapter 在真实调用前强制执行，不再仅依赖宿主
纪律。同一已确认 Batch 的未完成开发绑定原生代理身份；正常返回、子步骤切换、自测失败、
RUNTIME_ERROR 或增加 retry attempt 都不自行解除该绑定。原代理可恢复时只签发同身份
继续；替换必须先由 adapter 实际尝试恢复或查询该身份，取得明确不可恢复的原生失败事实，
再由 engine 校验旧执行已停止且无未决副作用，才签发同一 Batch 的替换动作。
瞬时网络错误、限流、超时、原因未知及主代理填写的理由都不等于不可恢复；这些情况按
既有有限恢复、Wait/Operator/Ask 路径处置，不能自动 fresh spawn。原代理只是达到本派发
自测修复上限时，保留原身份和已有阻塞，按既有额度与授权规则继续，不靠换会话重置上限。
批次身份、恢复事实、替换决定及次数沿用现有 State、TaskKey/Attempt、execution、
HostAction intent/receipt 和生命周期记录；不另建代理状态机或语义分类器。主代理不能
通过修改任务编号、失败分类、重试次数或重复登记相同计划，给未完成的同一批次换身份。
本条保证覆盖插件内部派发、恢复、重试和结果接纳；插件外直接调用 Codex 工具不在本次
补充范围。下一批仍须在上一批完成 DoD、验证及交接后，使用全新零上下文代理。
代码/质量门统一在本 run 全部开发 Batch 完成后进行；批内自测、批边界已声明的构建/回归
和交接继续执行，但不派发组间代码/质量门审，不把批边界验证记成正式门结果或 repair wave。
开发前产品/技术审、与开发并行的黑盒用例设计/审查，以及开发后的白盒设计/审查仍按各自
职责执行；“开发完成后再审”针对本次取消的组间代码/质量门审。

#### 验收条件

- AC-017：缺映射、重复 ID、循环依赖或不完整 DoD 的计划不能进入开发；前 Batch
  完成成员结果、自测/回归及交接后才解锁下一 Batch；不以一次组间门审作为解锁前置。
  首次开发前核对已确认的 granularity_review，含 Batch 映射、依赖、DoD、验证、
  恢复及交接安排；未完成初次计划确认不能进入开发。
- AC-018：开发者只取得已确认范围和任务，黑盒不读实现/白盒/开发解释；黑盒未 PASS
  不阻止开发开始，快照仍遵守两侧完成屏障。派发消费 engine 生成的规范薄指针。
  实跑开发与基线隔离的黑盒设计/审查并发，不能仅以“开发不等待黑盒 PASS”作为并发证据；
  白盒设计/审查在全部开发完成后开始，开发未完成时不得提前执行。
- AC-019：Git adapter 实跑 status/diff/untracked、track、commit、snapshot 及相关 ancestry
  核验；代理返回路径 manifest，adapter 拒绝越界或未解决状态，副作用以 intent/reconcile 恢复。
- AC-060：Batch 契约、接口、依赖或 DoD 变化使旧计划与未完成派发失效，重新粒度审及用户确认前不按新计划开发。
  新计划绑定当前需求/方案 revision、路线和拓扑；旧确认不能放行变更后的计划。
- AC-061：每批实际派发全新零上下文代理，同一开发会话不跨批复用，Subtask 不单独换代理或 reset。
  跨批交接只使用已提交产物和规范任务书；任务书不复制可从仓库恢复的内容。
  实跑同批次正常返回后继续、瞬时中断恢复和自测失败后的合法续做，均保持原生身份；
  实际尝试在同批未完成时直接换人、伪称不可恢复、把自测失败登记成运行错误、增加
  retry attempt、改任务编号或重复登记相同计划，插件均在调用前拒绝，新代理调用数为零，
  原身份、已有产物、有效结果及额度不被重置。实际原生恢复/查询明确证明该身份不可恢复，
  且旧执行已停止、未决副作用已对账时，才允许一次绑定本批和恢复证据的替换；缺证据、
  证据错绑/过期或重复消费均拒绝，重放不重复创建代理。分别保留真实续用与不可恢复
  替换正例的原生身份及调用证据；只提交模拟身份或结果被拒不算真实调用前验收。
- AC-071：多 Batch run 在中途批次完成时仅推进已声明自测/回归与交接，不签发代码/质量门。
  全部开发完成后，仍须按 REQ-011 完成候选和 QA 准备屏障，才统一签发所选门；门比较原
  基线到最终候选完整 diff。批边界结果不冒充门 PASS，也不计 repair wave。

#### 来源

engine 总需求 §8.5；增量计划 §2.3.1、阶段 4 与阶段内分批规则；ADR-002。

### REQ-008：3.5a 覆盖合同在 engine 的完整闭环

#### 要求

同一次需求/方案确认登记 requiredSources；State 与派发/结果共同绑定 inventory、
manifest、source↔point↔case map、whitelist digest 和当前 ValidationCandidate。
标准 REQ/AC 无损适配既有 coverage 类型，每个 AC 有批准 QA case；只对其他合法 source
允许其协议支持的显式非 QA 处置。必须逐 source/point/case 审查及逐例执行。

#### 验收条件

- AC-020：合法多对多映射可通过；未知、重复、遗漏、orphan 和缺 approved case 的 AC
  被拒绝。只有 case 决策、自动猜 source/point 决策、集合级 PASS 不能补齐覆盖。
  requiredSources 在同一次需求/方案确认中登记；除 REQ/AC 必须有批准 QA case 外，
  对其他合法 source 实跑其协议支持的显式非 QA 处置正例，不把该例外用于 AC。
- AC-021：digest、白名单或 candidate 不符使不再适用的结果失效；逐例记录真实 procedure、
  observation 和 comparison。skipped、模板/占位证据、无执行的授权 PASS 不计执行通过，
  环境不可用记 RUNTIME_ERROR；合法用户保留通过独立继承 receipt 满足对应义务。
  逐项核对 State、派发和结果共同绑定的 inventory、manifest、source↔point↔case map、
  whitelist digest 和 ValidationCandidate，缺失或失配不能接纳为当前有效覆盖结果。

#### 来源

engine 总需求 §8.4.1；3.5a 覆盖合同、3.5a-fix；增量计划阶段 4 覆盖接入。

### REQ-009：共享步骤、建议处置与白盒复用

#### 要求

同一 State 保存 shared-step 审批、case 引用及建议处置。共享准备审一次，逐例判断 oracle。
QA Review 的可选 P2/P3 按稳定 ID 采纳、延期或不采纳并记录理由；真实验收缺口继续阻塞。
白盒先独立判断覆盖，再核查、绑定并独立运行有效已有测试，只补缺口。

白盒 authoring workspace 由 Git adapter 从精确 DevelopmentSnapshot Dn 创建并登记。
白盒设计只能新增、修改或删除 TestOwnershipResolver 证明为 test-owned 的结构测试、
测试辅助文件、fixture 和 case 内容，返回 manifest，不执行 VCS 写操作。Resolver 综合
项目/框架配置、路径/命名、测试发现与生产构建输入，文件名含 test 既非必要也非充分条件。
adapter 提供 Dn → Qn 全部增删改名及内容摘要；只有全部差异 test-owned 且生产代码、
运行配置、生产依赖、构建输入和 provider 元数据投影完全相等，才证明生产视图未变。
需改生产代码或无法证明 test-only 时转开发/repair 形成新 Dn，UNKNOWN 按生产视图受影响处理。
白盒 Review 读取完整 Qn，独占测试代码质量、用例与实现引用的审查；实现质量门排除全部
test-owned 文件，不按测试由开发者还是独立 QA 编写来分配审查责任。

#### 验收条件

- AC-022：单 case 修改只使实际相关项失效；shared-step 修改使全部引用者失效。
  未变审批可复用，但错误 oracle 仍被逐例发现。
  多个 case 引用同一未变共享准备时只完成一次共享审批，各 case 仍独立判断 oracle。
- AC-023：可选建议全部有合法理由处置且覆盖完整时可 FROZEN；缺理由/未知 ID/未处置
  不能放行。采纳改变共同路径、可执行性或含义时重审受影响部分，验收缺口不得降为建议。
- AC-024：白盒复用测试有独立覆盖判断和真实运行；全绿但错误断言/漏测的样例被识别。
  有测试修改则提交到候选，无修改不强制造文件或推进快照。
  已有有效测试覆盖充分时复用并运行，不重复建设同一覆盖；确有缺口时只补该缺口。
- AC-062：白盒只可在绑定 Dn 的独立工作区提交经程序证明的 test-owned 改动，生产视图变化或 UNKNOWN 转开发/repair。
  验证含 test 名称的生产文件不能获 test-only 证明、不含 test 名称的合法测试辅助文件
  可正确归属，以及增删改名、生产配置/依赖/构建输入/provider 元数据均参与判定。
  白盒代理返回改动 manifest，只编辑授权测试文件，不执行 VCS 写操作；
  经归属和生产视图核验后的跟踪、提交由 Git adapter 执行。
- AC-063：候选的白盒 Review 承担全部测试代码质量审查，实现质量门排除全部 test-owned 文件。
  验证开发者已有测试和独立 QA 测试均遵守同一归属；候选提示词、catalog scope 和说明
  一致，门内需求覆盖、实现偏差、架构与耦合、实现正确性与质量仍显式并列。
  白盒 Review 读取完整 Qn，逐项核对用例与实现引用，并审查相应测试代码质量。

#### 来源

engine 总需求 §8.3、§8.4.2；最终方案 §7.1、完整流程迁移条款；3.5c REQ-001、REQ-005、REQ-006。

### REQ-010：QA 批量实跑与 checkpoint 续跑

#### 要求

同一 QA TaskKey 内复用非验收目标的构建/准备，独立用例干净副本、有序场景明确依赖，
各自有真实输出。可构造高成本前态，待测操作和作为目标的安装/宿主调用仍实跑。
checkpoint 经现有 progress/result submit 保存完整 PASS/FAIL、未完成项和中断原因，
绑定候选、需求、case/shared-step、tool/environment，不新建 QA 状态机或逐例额度。

#### 验收条件

- AC-025：同批一条失败只阻止其依赖，独立项继续且状态隔离；构造前态不预置待测成功。
  本轮安装/真实宿主验收不以复用代替；执行脚本遇已知错误样例能报告失败。
  同一 QA TaskKey 的多个用例实际复用非验收目标的构建/准备，保留可核对的复用记录
  和各自真实输出；作为验收目标的操作继续逐项实跑。
- AC-026：中断后完整条目保留且 FAIL 不丢，未完成不 PASS，绑定未变时只补剩余项。
  候选/需求/case/shared/tool/environment 变化精确失效，复用或失效原因可查。
  checkpoint 同时保存中断原因，续跑可查原原因及已完成、未完成条目。
- AC-027：缺失、重复、未知条目被识别；最终 expected-set 对账后才能形成有效结果。
  checkpoint 不增加 attempt，实际错误重派沿同 TaskKey 有限 retry；续跑不提前固定 cost。

#### 来源

engine 总需求 §8.4.2；3.5c REQ-002、REQ-003、REQ-004；增量计划阶段 4 checkpoint。

### REQ-011：review-ready、唯一 wave 与候选冻结

#### 要求

开发完成、必要白盒测试已提交、所有选定 case-set FROZEN，最终提交整理（含必要 squash）
先于最后必需验证。冻结 review-ready candidate 后一次建立 expected-set/digest，按容量
并行准备 eligible QA execution 和门。门只读基线到最终候选的完整 diff，不读 QA 用例正文
或执行证据。收齐 wave、处置错误并完成逐条核实/归并后才可返修。
黑盒批准用例、白盒测试和其他应纳入 VCS 的最终交付物，均须在 ValidationCandidate
冻结前物化并纳入 identity；未选择白盒也须装配开发快照与这些交付物。明确设计为 VCS
外的产物须有独立 digest 与 summary 绑定。Seal 不从遗留工作区临时补捞交付文件，
也不在最后验证后向候选追加交付内容。

#### 验收条件

- AC-028：缺开发结果、白盒提交、FROZEN 或 candidate binding 均不能提前派发执行/门。
  最终 expected-set 唯一且拒绝漏项、重复冻结和输入漂移；各已选目标按容量共同推进。
  核对各门审查实现的差异范围为原基线至最终候选的完整 diff，不向门提供独立 QA 用例正文或执行证据；
  wave 收齐、错误处置、逐条核实和归并全部完成后才允许创建返修。
- AC-029：最后必需验证与 Seal 绑定同一候选；promotion 改变原生 identity 时，只能按 AC-068 的精确等价 receipt 映射整组证据。
  Seal 不再次 squash 或追加候选内容。
  后续代码/测试/候选变化重新判定证据，不凭字节相似直接沿用旧候选全量 PASS。
- AC-064：有白盒和无白盒路线均在最终验证前物化全部 VCS 内交付物，并将 VCS 外产物的 digest 绑定到 summary。
  黑盒批准用例、测试或其他交付路径漏入候选时不能放行；验证完成后才追加交付物会使
  原候选证据重新按影响规则处理，不能在 Seal 时直接补入。

#### 来源

engine 总需求 §8.3–8.4；3.5b review-wave；3.5c REQ-007；requirements-precedence 最新顺序。

### REQ-012：Operator 裁决、归并纠正与唯一返修

#### 要求

Operator 逐条核实 raw findings 的范围、严重度、因果、复现和证据并裁决
NEW/SAME_AS/DISMISSED，保留稳定 finding/problem ID 与历史。Controller 不猜真假或重复。
同快照唯一 repair 汇总 QA FAIL、有效 P0/P1 和门审 P2；门审 P3 留 Seal advisory。
RESOLVED/DISMISSED 不重复返修；错误归并允许带 revision 的新裁决纠正。

#### 验收条件

- AC-030：wave 未收齐、错误未处置或核实/归并不完整不能创建 repair；同快照不能创建
  第二份逻辑返修。raw finding/result/receipt 本身不能增加 obligation/TaskKey 额度。
  逐项对账唯一 repair 的成员集合，完整纳入 QA FAIL、有效 P0/P1 和门审 P2；
  门审 P3 只保留为 Seal advisory，不进入返修成员集合。
- AC-031：已解决/驳回问题不重复开 repair；误归并在派发前更新同一计划，在派发后形成
  OPEN 义务进入修复所得新候选的下一 wave，保留原裁决和纠正历史。
  归并、返修和纠正前后保留可追溯的稳定 finding/problem ID，不因处置重新生成问题身份。
- AC-032：仅核实后存在多个合法且属用户选择的处理分支才 Ask；事实核实本身为 Operator。
  正常范围外项只作 P3，不能由其阻止 PASS 或增加返修。

#### 来源

审查收敛需求 REQ-004、REQ-015；engine 总需求 §7.2.1、§8.3–8.4；增量计划阶段 4。

### REQ-013：修复范围、纯 P2 carry 与最终保证

#### 要求

Controller 从项目/路线规则、修复类型和候选计算 QA scope；guarantee ACTIVE 本身不产生封板前额外 FULL。普通返修保持
FROZEN 并以固定 AFFECTED 执行/继承；仅需求含义、公开行为、判据或稳定可执行性变化可
合法 THAW。纯门审 P2 修复必须构建、全量项目测试和受影响用例逐项自跑，carry 原 gate
PASS，不复审、不增加 wave；无其他 FULL 义务时不自动派 FULL。

审查输入范围与 QA 执行范围分别绑定。State 持久化 typed ReviewScopeMode，并随
review/candidate/result 保留：黑盒/白盒用例或测试审查首次建立基线为 FULL，后续合法
增量只对新增、修改或 ImpactSet 受影响项使用 AFFECTED；产品审、技术审和普通质量门
始终使用 FULL 绑定输入。仅 USER_INITIATED_CHANGE 可让产品/技术审在完整最新文档上下文
中作影响集增量判断，不把输入缩成局部 diff。需要新鲜复审时读取完整绑定输入；
纯 P2 carry 与 REQ-015 的用户明确保留不伪装为一次新审查。

#### 验收条件

- AC-033：FULL 的 expected/actual 和 AFFECTED 的执行/继承/未执行集合完整对账；普通
  返修不解冻，临时环境错误走 RUNTIME_ERROR。QA_ARTIFACT_REPAIR 改测试/用例后绑定新
  候选，至少 fresh review/执行改动部分，并重算其他验证影响。
  普通返修固定采用 AFFECTED 执行/继承；仅需求含义、公开行为、判据或稳定可执行性
  发生变化时允许 THAW，其他原因不得解冻。
- AC-034：纯 P2 以结构化完成/carry receipt 保留原比较范围且不伪装重审；所有路线在本轮必需
  QA 与门全部有效 PASS（含合法继承）后即可 Seal，不追加封板前强制全量重跑。
  首次必要 FULL、正常 AFFECTED、新增/受影响义务与来源绑定仍保留；FAIL、PENDING、
  未完成和无效结果仍阻止推进，不将历史 PASS 冒充当前实跑。
  纯门审 P2 修复实跑构建、全量项目测试及受影响用例逐项自跑，缺项不能 carry；
  不增加 wave，没有其他 FULL 义务时不自动派 FULL。
- AC-035：主代理理由、final 标记、host override 或普通“继续”不能新增 FULL、取消或
  Seal 跳过授权；合法一次例外绑定 run/revision/candidate/mode/scope，不改变默认规则。
- AC-065：QA Review 首次 FULL、后续合法增量 AFFECTED 的范围随候选与结果持久化，不能拿 execution scope 代替审查范围。
  产品/技术审与普通门始终取得完整绑定输入；普通门重审比较原基线到当前候选的完整 diff，
  不缩成返修 diff；用户需求变化的增量判断和纯 P2 carry/明确保留分别沿用各自合同。

#### 来源

engine 总需求 §7.2、§8.4；最终方案 §5、§7；3.5b P2 carry/AFFECTED 与 3.5c 冻结规则。

### REQ-014：任意非终态需求变化与影响授权

#### 要求

REQUEST_REQUIREMENT_CHANGE 在任意非终态可达。Phase A 撤销未完成 Seal intent、停止
权威写入/promotion/新业务签发；可安全隔离的任务可结束但结果 quarantine，其余安全
suspend/terminate，不先猜不受影响项。完整新需求确认与 ImpactSet 后 Phase B 处理。
meaning-preserved 无回退 Ask、保留产品审 PASS；meaning-changed 必须先有绑定影响
预览、state/base/candidate revision、回退后果与额度的用户授权。

审查 finding 驱动的修订沿用 review-delta：主代理在编辑前保留并核对与上一确认 revision
匹配的完整 base，对完整候选生成穷尽变更并逐 hunk 核实来源，不允许夹带无关修改。
用户只确认变更包，完整 candidate revision 登记为新需求，未变内容继承；首次受理仍
确认全文，用户主动查看全文不扩大本次增量确认范围。meaning-preserved 的正文变化
也须确认 review-delta；Controller 只保存核验摘要和确认绑定，不新增 base/diff 账本。

#### 验收条件

- AC-036：在 Ready/Ask/Wait/Operator、开发、QA、repair 及未完成 Seal intent 发起修订
  均立即建立写屏障；在途任务不阻止受理，迟到结果不能推进旧节点或覆盖新决定。
  Phase A 实际撤销未完成 Seal intent，不预先猜测不受影响任务；可安全隔离的在途任务
  可结束但结果进入 quarantine，其余任务安全 suspend/terminate。
- AC-037：合法确认原子登记新 revision、用户保留及其余影响处理；缺失/过期/不匹配
  授权零状态变化。review-delta、影响和保留可共用一次完整决定；不得保留 legacy 旁路入口。
  meaning-preserved 不出现回退 Ask，保留产品审 PASS；meaning-changed 在授权前展示
  影响预览、state/base/candidate revision、回退后果和剩余额度，缺少上述内容不能按该授权应用。
- AC-038：未保留 affected/unknown 结果失效，明确未受影响结果可 carry；只有
  USER_INITIATED_CHANGE 可对产品/技术审使用增量判断，finding 修订遵守新鲜复审。
  需求变化不重置 series/wave，也不自动授权下一轮产品审。
- AC-066：审查修订以匹配上一确认 revision 的完整 base 生成穷尽 review-delta，逐 hunk 来源核实后只确认变更包。
  缺失/错误 base、漏列变更或无关 hunk 未完成核实与相应授权时不能登记新需求；合法确认
  绑定完整新 revision，未改内容继承，meaning-preserved 不跳过正文变更确认。
  用户主动查看完整文件后，确认范围仍是本次 review-delta，不因此扩大为其他未修改内容。

#### 来源

engine 总需求 §7；最终方案 §5、§5.1；增量计划阶段 4 requirement impact 迁移。

### REQ-015：用户明确保留已有 PASS

#### 要求

用户修改需求可全选或逐项保留已有产品/技术审、QA 审查/执行和普通门 PASS，即使
meaning-changed 且 affected/unknown 仍以用户选择优先。receipt 绑定原 PASS/需求/候选、
新需求/目标候选、state revision 与明确清单，并与修订原子应用。保留可满足推进、覆盖
和 Seal，不只是留档；不将 FAIL、PENDING 或未执行变 PASS。

#### 验收条件

- AC-039：全选、部分选、无保留、affected/unknown 保留及连续修订可从公开路径推进至
  Seal；保留优先于本次需求变化的失效/fresh review，新增无原 PASS 的义务仍验证。
  核对保留 receipt 同时绑定原 PASS/需求/候选、新需求/目标候选、state revision 及
  明确保留清单，并与本次修订原子应用；任一绑定缺失或不匹配均不能部分保留。
- AC-040：显示和 Seal 分别展示原始实际执行与用户授权继承；原证据可追溯、不伪造新
  版本执行。未选按影响规则处理，在途未形成 PASS 项不可选，迟到结果不覆盖清单。
- AC-041：授权仅适用本次绑定，后续独立变化重新判定；不替代尚未完成的提交、promotion
  或其他副作用，终态只能新建 run。child/merge 扩展留阶段 5。

#### 来源

engine 总需求 §7.2.2；最终方案 §5.2；增量计划阶段 4 用户保留验收。

### REQ-016：主动控制、外部漂移与资源恢复

#### 要求

所有用户控制由带 freshness 的 availableActions/REQUEST 生成 typed Ask 再 submit。
覆盖合法节点跳转、route-add/reselect、adopt-external、reset、abort 和未知原因中断；
不能自由 USER 事件写状态。adopt-external 绑定精确 observed identity/lineage/diff 与
预览，保留原 base，按 ImpactSet 处理结果和副作用。终结统一自动 cleanup。
route-add/reselect 在开发前可重选或追加；开发开始后只可追加普通门，不能新增 QA，
不能静默移除已有义务。lightweight 拒绝普通 route-add；retained merge master 的拒绝
语义由阶段 5 承接。路线/case-set/目标集改变须使受影响的旧 wave 失效，不得中途修改
expected-set 来制造收齐。

#### 验收条件

- AC-042：过期、错误 request/节点/revision 的主动控制拒绝；合法跳转先隔离/失效/对账
  受影响 attempt、结果、candidate 和副作用，reset 展示精确保留/清除范围。
  逐项经 availableActions/REQUEST 生成 typed Ask 再 submit 完成控制；
  自由 USER 事件直接写状态的请求被拒绝。
- AC-043：外部漂移先冻结预览，授权后仅接纳绑定 identity；需求正文变化转需求流程。
  affected/unknown 结果与 intent stale，未完成 wave 不计轮、已完成额度不清零。
  adopt-external 同时绑定 observed identity、lineage 和 diff，应用后保留原 base。
- AC-044：abort/reset/terminal cleanup 使用登记资源和可恢复 intent；未知 receipt 先
  对账，清理未完保持 FINALIZING_CLEANUP，完成后 summary/receipt 可只读 replay。
  公开 cleanup 不能删除活动 engine run，不留本阶段登记资源或未处置 UNKNOWN。
- AC-067：公开控制路径允许开发前重选/追加、开发后追加普通门，拒绝开发后新增 QA、移除既有义务及 lightweight 普通 route-add。
  合法追加重新绑定目标义务和 wave，旧目标集不能被原地缩减为已收齐，过期路线授权拒绝。
  分别改变 case-set 或目标集，核对受影响旧 wave 失效，不能通过原地修改 expected-set
  把旧 wave 变成已收齐。

#### 来源

engine 总需求 §3–5、§6.3、§7、§8.3；最终方案 §4.3、§8.3、§9；增量计划阶段 4 控制与恢复。

### REQ-017：guard 权威来源与有限派发

#### 要求

复用 3.5b guard，Controller 仅从 closed-world definition、已确认 route/topology/Batch、
批准 QA、选定门及成立的 obligation 派生 expected TaskKey 和有限 retry basis。
需确认的扩张先绑定 typed Ask，预授权 repair 按既有 policy 机械加入；外部不得提交底层
额度。State 保存单一 basis digest、计算结果及 action/attempt 账目，签发前重算。
项目/安装级 token cap 经 REQ-021 的配置入口读取、校验并持久化生效值与来源；只计 engine
派发用量，不含 owner 终态报告。可计量用量达到 cap 时，即使 request/attempt 尚有余额也
停止后续自动派发并返回预算处置 Ask。未配置或来源明确不可计量时展示 token cap unavailable，
次数限制仍生效；配置错误不得静默等同未配置。配置修改的生效时点须公开说明，已启动 run
采用的值可查，重启不清零用量或丢失限制。有限继续仍按现有授权执行，不把一次继续变成永久关闭 cap。

#### 验收条件

- AC-045：合法基础、已完成 adjudication/wave 后的 obligation 和确认计划正确计算；
  raw finding、普通 receipt/result/resume 不扩容，追加 retry 只改当前逻辑动作 policy/digest。
- AC-046：basis 重算不一致、账目不符或将要超发以 BLOCKED_BUG 零派发；达到额度为
  typed Ask。继续仅放开一个按现有 capacity 裁剪的 Ready batch，不能无限授权。
- AC-047：show/status/next 展示权威 cost/guard/basis，运行时集成状态与实际一致。
  guard-preview 仍只读、非权威，不以输入 JSON 认证真实确认或增加 run 额度。
- AC-072：通过真实配置入口设置 token cap；派发次数尚有余额而已结算派发 token 达到上限
  时，IssueFromPlan/refill 和恢复/重试路径均停止自动调用并展示原因及有限处置 Ask；owner
  用量不混入 cap。合法授权只放开本次允许的 batch/attempt，重复提交不重复放开。
- AC-073：重启后生效 cap、来源、已用量和阻断状态保持一致；修改配置可按公开生效规则
  核对新旧 run 的实际值，不清零已有用量。未配置/来源明确不可用仍受次数限制；格式、字段
  或值错误有可定位错误，不静默取消 cap，也不把未收到 lifecycle stop 当成明确不可用。

#### 来源

engine 总需求 §5.9、§8.4；3.5b guard/REQ-013；增量计划阶段 4 运行时止损。

### REQ-018：source bridge、Codex 用量与原子 refill

#### 要求

复用 internal/cost 的唯一投影。submit 同一写事务按接纳结果/receipt → source
resolve/backfill → usage → guard → refill 排序；lifecycle stop 未到达为 COST_PENDING/
Wait，source 明确不可用为 UNAVAILABLE，仍受 request/attempt guard 限制。
保留 Codex transcriptPath、原生 response ID 去重和 task_started 至 complete/aborted
timing，含工具等待、不含 turn 间空闲。owner 终态差值只报告，不参与 in-flight guard。

#### 验收条件

- AC-048：结果先到、stop 后到、重放和崩溃恢复均不能在 cost/guard 前补位；source
  暂未就绪保持 Wait，明确不可用后仍按次数上限派发，不从 legacy writer 旁路补账。
  验证同一写事务严格按接纳结果/receipt → source resolve/backfill → usage → guard →
  refill 排序，覆盖事务失败和恢复窗口，不以单独检查“未提前 refill”替代完整顺序与原子性验证。
- AC-049：Codex 重复请求记录去重、transcriptPath 与 timing 回归正确；checkpoint 不
  固定不完整用量，续跑终结后完整结算，缺来源/计时边界沿用 unavailable/省略语义。
  不扩展其他 provider，也不把 3.5c 前后模型 benchmark 变为本阶段重复验收。
  owner 终态差值只进入报告，不参与 in-flight guard；派发止损核对不混入 owner 差值。
- AC-050：额外返修 Ask 含完整 findings/problems、核实/归并理由、修复/验证历史、QA scope、
  TaskKey/signature、已用/剩余额度和 cost/guard 可用性，授权绑定摘要 digest/revision；
  engine allowance 来自权威投影，owner cost/token cap 各自的 unavailable 不互相替代。

#### 来源

engine 总需求 §8.4；最终方案 §7；3.5b owner/source 及 3.5c Codex 计量；增量计划阶段 4。

### REQ-019：首个真实宿主执行前准入

#### 要求

封闭流程、步骤顺序、前后置条件与失败分支由插件程序定义和执行；不同宿主只适配
实际调用与事实回传，不重新决定流程。宿主接入随对应功能实现并验证创建新会话、
完成结果识别、拒绝传播及中断后任务对账；无需先证明“固定流程能否成立”。
adapter/bridge 在实际原生派发/恢复、工作流模型 CLI/API 和声明 QA 操作之前认领
已签发动作，核对 actionID、TaskKey/Attempt、run/revision/candidate、provider、payload
digest、scope 和精确 case set，复用 pendingActions/CAS/intent/guard。未签发、越界、
过期、取消、重复认领/消费均在底层调用前拒绝；任务内合法请求沿用原绑定，不逐请求建
TaskKey。provider、bridge 可用性和 lifecycle 配对分开，缺 bridge 不降级 provider。

#### 验收条件

- AC-051：逐项记录首宿主真实入口、前置校验位置和绑定字段；合法派发、恢复与 QA
  可完成，未签发/越界/重放/过期/取消及 provider 不符路径有底层调用数为零的真实证据。
  分别实跑新会话创建、完成结果识别及拒绝传播；核对封闭流程的顺序、前后置条件与
  失败分支由插件决定，宿主只执行已校验调用并回传事实。任务内合法请求沿用原动作绑定，
  不逐请求创建 TaskKey。
- AC-052：认领并发、调用前后中断和 UNKNOWN 窗口不导致重复底层调用；未覆盖入口明确
  unsupported 并拒绝承诺保证的模式。mock、wrapper 自报、提示词或事后登记拒绝不替代验收。
  分别核对 provider 可用性、bridge 可用性和 lifecycle 配对；缺 bridge 时拒绝相应保证
  模式，但不得把已识别的 provider 降为其他 provider 或默认不可用结论。
- AC-053：RUNTIME_ERROR 经合法有限 retry/用户追加 attempt 后能完整登记权威结果；
  原错误可查，非法执行尝试不污染 PASS、不增加语义轮、不要求 Seal 跳过。

#### 来源

engine 总需求 §5.17；最终方案 §9.1、§9.1.1；增量计划阶段 4 首宿主准入与合法重试。

### REQ-020：候选 promotion、隔离 canary 与阶段 4 全局发布

#### 要求

本阶段正式流程由“本阶段环境绑定”锁定的当前版本 stable driver 执行；候选只是被测对象。阶段开发 worktree、
候选安装/测试项目/host config/state/resource registry 与稳定环境互相隔离，候选从
已提交快照构建，运行输入不软链到开发或稳定目录。Git whitebox workspace、完整 freeze
与 promotion 的 receipt 包含 canonical tree/blob/mode/ref validation view 和恢复状态。
本阶段 Seal 后集成主线，关联 sealed 与 post-integration identity，按影响重建/重验后将
全局插件切到阶段 4 发布包。顺序固定为：阶段范围 QA/canary PASS → 本阶段固定 driver Seal →
主线集成/必要重验 → 既有安装事务执行全局切换 → 实际全局入口 smoke/准入核验及 REQ-022
历史功能回归 → 发布完成。
切换覆盖 runtime、hook/config、managed-rule、pointer 与 registry；只发布已验证的精确
安装包，不从未提交 worktree 安装。沿用现有 journal/reconcile、单调 generation 和失败
回退，不增加第二套发布机制，不等待或迁移活动旧 run。

阶段 4 发布成功后，新的全局入口和阶段 5–7 正式 run 使用已发布阶段 4 版本；后续正式
开发用冻结的阶段 4 driver，不让阶段 5/6/7 候选驱动自己的 run。本阶段开发驱动退出日常驱动，
发布前旧稳定包只按既有安装失败回退合同保留；旧 run 不交给新版本续写。阶段 4 仅承诺
已验收的 Codex/Git 非分片 regular 和既有 lightweight 能力，未支持的 engine 路径明确
拒绝。用户入口文档与实际发布能力同步，完整五宿主/三 VCS/split 仍留后续阶段；旧入口和运行时兼容逻辑本阶段删除。

被测 engine 的 regular run 只有在完整 wave 收齐且无 obligation 后，才由 adapter 把
整个已验证 Qn 提升到主线，不在末尾重新播放未经验证的测试补丁。优先保持同一原生
identity；Git 集成生成新 identity F 时，CandidatePromotionReceipt 必须机械证明
source/target identity、预期 target base、全部路径及 provider 元数据、operation ID、
无冲突与 validatedDigest(Qn) == finalDigest(F)，才允许整组证据映射到 F，不逐门猜测
carry。内容冲突、转换、目标漂移、UNKNOWN 或摘要不等价时不得 Seal；先 observe/reconcile，
必要时将实际 F 冻结为新候选，按真实影响与保证要求重验。此为被测 regular 的 promotion
合同；本阶段正式交付仍按固定 driver Seal 后再集成的顺序执行。

#### 验收条件

- AC-054：真实 installed binary 完成 Git no-split full canary，覆盖产品/技术审、并发
  开发和黑盒设计、白盒设计/复用/审查、候选 freeze、QA/门 wave、至少一次真实
  FAIL → repair → 新候选 → 重验 → Seal，以及需求保留、guard 和首宿主准入正反例。
- AC-055：VCS/whitebox/promotion 中断可恢复；namespace 的 canonical path、Lstat/realpath、
  source/package/installed identity 及 disjoint proof 齐全。最终验证绑定实际安装产物，
  固定 driver 正常入口 smoke 成立，本 run Seal 前不替换其 driver，也不拿候选签发本 run 的权威 Seal。
- AC-056：构建、全量项目测试、race、phase0whitebox、vet、package validation、portable
  canary 和 diff 检查通过；正式 QA/选定 gates 完成本阶段范围内验证，P3 按规则一次性提示。
- AC-057：登记资源 cleanup、适用 fencing/lease 的 stale token 与并发窗口、候选
  registry/transaction receipt 及 UNKNOWN 处置成立；阶段记录保存全部身份/来源/证据。
  Seal/主线集成凭据关联精确后继基线，若集成改变树或包则按计划重建/重验。
- AC-068：完整 wave 无义务后才提升整个已验证候选；同 identity 可直接绑定，新 identity 只有精确等价 promotion receipt 才可映射整组证据。
  验证 Git identity 改变但完整 validation view 等价的正例；冲突、转换、目标漂移、
  UNKNOWN 或摘要不等价不能沿用原结果 Seal，须对账并按实际候选重新判断验证义务。
- AC-077：Seal 前在隔离安装目标演练阶段 4 发布顺序、精确包安装、切换后检查和失败回退；
  验证 runtime/hook/config/managed-rule/pointer/registry 一致，旧 generation 不能继续写入，
  失败能恢复原稳定包且 generation 不倒退。该演练不得覆盖真实全局安装。Seal/集成/必要
  重验后实际执行全局发布，发布 receipt 记录 sealed/post-integration/package/installed
  identity、切换事务和真实全局入口检查；后者属于交付后的发布核验，不伪装成 Seal 前 QA。
  在隔离目标中保留活动旧 run 演练发布，核对全局切换不等待旧 run 结束、不迁移旧状态；
  旧 run 仍保留原版本绑定，不能交给新版续写。
- AC-078：阶段 4 发布后通过实际全局入口核对发布身份、Codex/Git no-split engine 路由及
  执行前准入，原生入口不得绕回 legacy；阶段 5–7 固定 driver 绑定同一已发布包。安装态
  说明明确受支持能力和未支持入口，阶段 5/6 候选不自动升级全局。Seal 前以隔离目标验证
  此合同，实际全局检查、REQ-022 回归结果与 driver 绑定记入发布 receipt；缺少该记录
  不能宣称发布完成或启用后续正式 driver。
  发布后核对本阶段旧开发驱动退出日常驱动，旧稳定包仅按既有安装失败回退合同保留。

#### 来源

增量计划 §1–2、阶段 4、§4–6；engine 总需求 §4、§8.3、§9；最终方案 §7.1、§8.3；
阶段 3.5c Seal/集成凭据；2026-09-07 用户关于阶段 4 交付后全局切换的明确决定。

### REQ-021：全部用户配置方便手改和 AI 修改

#### 要求

插件全部用户可配置项（含既有配置及本阶段 token cap）有完整、易查的配置说明：实际文件
位置、字段含义、类型/单位、默认值、合法值/最小示例、作用范围、覆盖优先级和生效时点。
用户能用普通文本编辑器修改，AI 在用户授权后能通过同一配置文件修改；两种方式使用同一
校验和生效规则。沿用已有明确的配置载体，新增配置使用简单可读格式，不要求用户或 AI
修改源码、编译制品、workflow state、digest 或 receipt，也不要求同一设置在多处重复维护。
安装或宿主管理的派生文件由受支持维护入口生成；文档明确可编辑来源与派生产物的区别。
通过现有维护/诊断面可以核对实际生效值、来源及配置错误，常见拼写/类型/值错误定位到
文件和字段并给出修正提示。配置便利不把 engine 派生的次数额度或用户授权记录开放为手填值。

#### 验收条件

- AC-074：按公开配置入口逐项核对全部用户可配置项，说明中的位置、字段、默认值、覆盖
  关系与生效时点均可验证；既有配置及 token cap 均能找到实际生效值和来源。无须查看生产
  源码或修改内部状态才能找到、理解和调整一个公开设置。
  说明还须逐项给出类型/单位、合法值、最小示例和作用范围；明确可编辑来源与派生产物，
  并实跑受支持维护入口生成安装/宿主管理的派生文件。
- AC-075：同一合法配置变更分别经用户直接编辑、AI 按用户指令编辑后得到相同有效值和
  行为，未修改设置保持原值；不需同步多个副本或计算内部摘要。常见错误返回准确文件/字段
  与可操作修正提示，按说明修正即可恢复；新增配置与已有配置遵守同一便利性约定。
  核对公开可编辑配置不包含 engine 派生的次数额度或用户授权记录，不能通过手填配置新增授权。

#### 来源

2026-09-07 用户明确补充：“本插件所有配置都要方便，无论是用户手改还是 ai 改”；
3.5b 项目/安装级 token cap 合同；本文件 REQ-017 的配置与派发止损边界。

### REQ-022：全局切换后逐项检查阶段 0–3.5 的功能

#### 要求

阶段 4 全局切换后，要回答“前面做过的功能在现在安装的版本里还能不能正常使用”。从
各阶段正式需求、修正需求、`refactor-plan/stage-records.md` 和对应 `.gates/results/`
结果提取逐项清单，覆盖阶段 0、1、2、3、3.5a、3.5a-fix、3.5b、3.5c。至少覆盖下表，
但不能用表中的概括替代原需求的逐项核对：

| 来源阶段 | 必查功能范围 |
| --- | --- |
| 0 | 安装/维护入口、registry/bootstrap、hook/config/managed-rule 绑定、隔离安装、事务恢复与失败回退 |
| 1 | 封闭定义与编译校验、确定性计划、版本/registry 绑定、调度与非法定义诊断；原 shadow 能力的当前归属 |
| 2 | 锁/CAS 与原子持久化、事件/receipt 接纳、幂等与过期结果拒绝、Attempt 生命周期、中断和 UNKNOWN 对账 |
| 3 | 已有 lightweight 闭环、start/drive/submit/show/status/next/diagnose、版本拒绝、唯一 writer、终态重放及资源清理 |
| 3.5a 与修正 | 单一需求文件、REQ/AC 投影、source→point→case 覆盖、白名单与保证激活、开发前审查计数及纠正 |
| 3.5b | finding 裁决/归并、唯一返修、P2 carry/P3 advisory、有限重试、guard-preview、owner/source 与成本口径 |
| 3.5c | 共享步骤和建议处置、白盒有效测试复用、批量执行与 checkpoint 续跑、Codex 用量去重/timing、最终候选整理顺序 |

每项记录原需求/交付证据、阶段 4 的实际入口或内部承载位置、测试用例、当前执行结果与
证据。已由 engine 替代的旧路径，要验证新版中承担同一功能的路径，不要求恢复旧命令或
续写旧 run。明确被后续用户决定替代、仍按原计划留待阶段 5/6、或历史上授权跳过的项目，
分别写明状态和来源；“以前 Seal 过”“旧版本 PASS”与“本次没测”均不能算新版实跑 PASS。
历史授权跳过不自动豁免新版仍承诺提供的功能。仍承诺保留的能力失效就是回归问题，不能
仅标为 unsupported 隐去；如果实际发现范围冲突，要逐项呈现并按需求变化处理。

同一清单先在 Seal 前的隔离安装候选上验证；全局切换后，公开功能通过实际全局插件入口
在独立测试项目中再执行，绑定当前发布包和实际安装身份，不调用旧 driver 或开发目录的
binary 代替。内部纯合同用绑定同一发布源码/包的现有 fixtures 复验，并通过公开行为验证
相应接入；不为测试新增公共接口。卸载、安装故障注入等会破坏全局环境的用例，在同一
发布包的隔离安装目标执行并注明环境；真实全局的入口、绑定与正常使用检查仍须完成。
复用已有用例和共享步骤，保留逐项结果；不额外要求重做 3.5c 的付费性能对比。

#### 验收条件

- AC-079：有覆盖上述全部阶段及修正的逐项功能回归表，每项可追溯到原需求/交付来源、
  新版承载路径与用例；明确区分当前实跑结果、已替代、后续阶段待交付和未验证。不得按
  “整个阶段已 Seal”批量填 PASS，不得因旧入口退出而遗漏仍须保留的功能。
  历史授权跳过的项目单独标明该状态和授权来源，不混为当前实跑通过或新版自动豁免。
- AC-080：Seal 前隔离候选验证及切换后的同表复验有分开的真实执行记录；后者的公开
  功能来自实际全局插件，内部 fixtures 与破坏性安装用例按上述环境边界执行并准确标注。
  结果绑定精确发布/安装身份；既有用例可复用，历史结果不能冒充本次执行。全部仍承诺
  提供的功能须有当前有效证据，不能以启动 smoke、版本号或局部抽样代替逐项覆盖。
- AC-081：发布 receipt 附回归表及各项证据，并用明确结论列出正常、失败、未验证和有
  依据的替代/延期项。仍承诺提供的功能存在回归阻塞或缺少必需证据时，不宣称全局发布
  完成，不启用阶段 5–7 新 driver；按既有发布失败回退合同恢复稳定安装，保留失败证据，
  修复后重新绑定候选并执行受影响的必需验证。范围外 P3 建议不阻塞；不倒写历史 Seal/PASS。
  实际发现范围冲突时逐项呈现冲突及来源，进入需求变化处理，不用失败回退或 unsupported
  标记代替用户对范围的决定。

#### 来源

2026-09-07 用户明确补充：切换到阶段 4 完成版本后，检查阶段 0、1、2、3、3.5 的功能
是否都已能正常使用；各阶段需求、修正需求、阶段记录及原结果为清单来源。

## 验证与完成条件

沿用 `references/local-validation.md` 的项目命令：`go test ./...`、
`go test -race ./internal/validate ./internal/cli`、
`go test -tags=phase0whitebox ./internal/validate`、`go vet ./...`、构建实际 binary、
package validate、portable canary、暂存和未暂存 diff 检查。新并发协议用受影响 engine
包的 race/故障窗口验证补足；不是每个局部编辑后重复全套。

黑盒验收经实际候选公开入口，白盒验证状态/协议和拒绝分支；纯内核 fixtures 复用现有
known-answer、确定性和异常序列。正式 QA 覆盖本文全部 REQ/AC，执行证据绑定当前候选；
其中发布合同先在隔离环境验收，AC-077–081 要求的实际全局检查按其明确时点在 Seal 后
另行执行并记录，不作为 Seal 前已经完成的事实。阶段边界不以“接口已搭好”“测试替身
通过”或历史 legacy PASS 代替完整 regular 交权。

本阶段源码交付需满足增量计划共同退出条件和本文 Seal 前 AC，并取得独立 Seal/集成凭据；
随后必须按 REQ-020、REQ-022 完成真实全局切换、发布核验与历史功能逐项回归，才可宣称
阶段 4 全局发布完成并启用新的后续阶段 driver。Git split、SVN/P4、其他宿主与阶段 7
最终完整版本不在本阶段实现，
不能因阶段 4 提前发布而宣称这些能力已交付。实际开发分批和正式路线在各自确认节点记录；本文件的
受理状态不构成这些确认。
