# formal-gates 分阶段 Seal 开发计划

> 状态：开发计划已确认采用“固定稳定插件驱动 + 阶段候选隔离验证 + 最终一次全局切换”。
> 总需求基线：`openspec/changes/orchestration-pipeline-engine/master-requirements.md`。
> 总体目标架构：`refactor-plan/final-implementation-draft.md`。
> 本文只定义交付切片、阶段环境和退出条件，不启动任何 formal-gates run；每个阶段开始前仍须独立完成该阶段的需求受理、方案确认与路线选择。

## 1. 计划目标

本次重构不以一次性替换整个插件为实施方式。开发拆成八个顺序阶段，每个阶段都完整走一遍 formal-gates 流程，并产生可审计的 Seal 结果。阶段 N 只有在通过全部范围内验证并 Seal 后，才能成为阶段 N+1 的源码基线。

每个中间阶段必须同时满足：

1. 当前固定稳定插件仍能按既有文档化入口正常工作，能够继续驱动后续阶段的 formal-gates 流程。
2. 本阶段候选在隔离安装和隔离测试项目中可正常使用，不依赖开发 worktree 中尚未提交的内容。
3. 本阶段没有把未完成的公共面、文档或状态格式泄漏到固定稳定插件。
4. 若下一阶段开发失败，可回到最近一个已 Seal 的源码和候选包，不需要修复半安装的全局插件。
5. 最终阶段以前不替换全局稳定安装、不退役旧运行时，也不把早期阶段的 canary 证据拼接成最终交付证据。

自狗粮硬规则：从阶段 0 启动到增量阶段 7 的最终 Seal 完成之前，所有阶段正式 run 的 `workflow start`、需求登记、开发/审查/QA 编排和 Seal 都由阶段 0 冻结的基线版本项目安装（固定 stable driver）执行。候选 binary 只能在候选隔离测试项目中作为被测对象运行，不能驱动自己的 formal-gates run、写稳定 registry 或签发权威 Seal；最终 Seal 的 receipt 也必须由基线 stable driver 写出。只有最终 Seal、主线集成/promotion receipt 和最终 canary 全部成立后，才允许把全局入口切换到候选。

## 2. 环境模型

### 2.1 三类隔离环境

每个阶段使用三种相互独立的环境：

| 环境 | 用途 | 约束 |
| --- | --- | --- |
| 固定稳定驱动环境 | 驱动本次重构各阶段的 formal-gates 流程 | 使用阶段 0 冻结的基线版本项目安装；`prompts/`、`gates/` 和其他运行输入不得软链接回开发工作区；直到阶段 7 最终 Seal 完成前保持不变 |
| 阶段开发 worktree | 编辑、提交和审查本阶段源码 | 基于上一阶段 sealed commit 创建；只承载本阶段授权范围；不得直接充当权威候选安装 |
| 候选验证环境 | 安装并运行本阶段候选插件 | 从已提交的候选快照复制或打包到独立目录；使用独立测试项目、host home/config、状态目录和资源登记；候选是被测对象，不得成为正式 run 的 driver 或 Seal writer；不得读取开发 worktree 或固定稳定环境的可变内容 |

开发 worktree 是必要的源码边界，但不是完整的运行隔离。候选插件必须从一个不可变提交构建并复制安装，不能让候选安装中的 `prompts/`、`gates/`、二进制或文档继续指向正在编辑的 worktree。候选还必须把 host hook/config、managed-rule、`DSH_HOME`、state root 和 resource registry 纳入同一隔离 namespace，并在启动前证明这些 canonical paths 与稳定环境、开发 worktree 不重叠。

建议的逻辑布局如下，实际绝对路径在各阶段受理时确定：

```text
<stable-root>/formal-gates/                 # 冻结的稳定驱动插件
<source-root>/formal-gates/                 # 主线与总需求文档
<worktree-root>/stage-00-distribution/      # 阶段开发 worktree
<worktree-root>/stage-01-kernel/
...
<candidate-root>/stage-00/install/          # 从候选提交复制的不可变安装
<candidate-root>/stage-00/test-project/     # 候选黑盒测试项目
<candidate-root>/stage-00/state/            # 候选独立状态与证据
<candidate-root>/stage-00/host-home/        # 候选 host 配置、hook、managed-rule、DSH_HOME
<candidate-root>/stage-00/resources/        # 候选 workspace/client/resource registry
```

Git 阶段使用 linked worktree；SVN 阶段使用独立 working copy；P4 阶段使用独立 client/workspace。它们都遵循同一条规则：开发区、候选安装和黑盒测试项目不能是同一个可变目录。

候选安装的可用性证明必须执行安装目标中的实际 binary，并从其实际安装路径解析 `prompts/`、`gates/`、hook 和 host provider；不能执行 source worktree、固定全局 binary 或过期候选来冒充当前候选。安装清单至少记录 source identity、package digest、installed-target digest、每个 host/config/state/resource canonical path 及其不重叠证明；`Lstat`/realpath 检查发现 symlink 回开发区或稳定区时，候选注册直接拒绝。

### 2.2 阶段基线与推进

阶段 0–6 固定执行以下推进顺序；最终全局切换也必须以主线集成后的 canonical identity 为输入，阶段 7 的唯一例外见下文：

```text
上一阶段 post-integration canonical identity（首阶段为冻结的初始 VCS identity）
    -> 创建本阶段分支与开发 worktree
    -> 用阶段 0 冻结的基线版本（固定 stable driver）启动本阶段 formal-gates run
    -> 开发、审查、QA、修复
    -> 从已提交候选构建隔离安装
    -> 在隔离测试项目验证新增能力和既有正常路径
    -> 本阶段 Seal
    -> 记录 sealed commit、包摘要和证据
    -> 集成到主线并取得 promotion/integration receipt
    -> 如集成改变树或包摘要，基于 post-integration identity 重建/重验候选
    -> 以 post-integration canonical identity 创建下一阶段 worktree
```

一个阶段未 Seal 时，不得让下一阶段基于其未完成 worktree 开发。阶段 worktree 在 Seal、主线集成和证据核对完成前保留；完成后按正式流程清理。sealed candidate identity 与 post-integration identity 必须分别记录并以 promotion/integration receipt 关联；若集成产生新的提交、revision 或 changelist，下一阶段绑定后者而不是猜测前者仍等价。

候选隔离测试可以执行候选自己的 `workflow` surface 来验证功能，但这类 run、receipt 和 summary 只能落在候选 namespace，不能被基线 stable driver 的正式 run 读取为权威状态；阶段 7 最终 Seal 之前，所有 formal-gates 受理、审查、QA、修复和 Seal 仍回到基线 stable driver。

阶段 0–6 使用上面的通用顺序；阶段 7 是唯一的最终发布顺序例外：前置开发、审查和候选验证可以先完成，但不得先 Seal；final-release run 在集成期间保持 ACTIVE，先把已审计树集成到主线并取得 post-integration canonical identity，再由基线 stable driver 通过受支持的 `workflow resume --adopt-external --reason` receipt 绑定该 identity，重建最终候选、运行全部最终 canary 和切换前 QA，最后才执行阶段 7 唯一的 final-release Seal。前置验证结果只能作为 provisional evidence，不能与 post-integration 结果拼接成最终证据；若 run 无法安全 rebind，必须新建 final-release run，且不得继承前置验证结果。

### 2.3 唯一写入者与版本绑定

过渡期间虽然 legacy runtime 与 engine runtime 同时存在于代码库，但同一个 run 只能有一个权威写入者：

1. façade 只能在 `workflow start` 或最小读取版本 envelope 时选择整条 runtime。
2. 新建的版本化 engine/candidate run 创建后永久绑定 writer、`stateSchemaVersion` 和 `workflowDefinitionVersion`；阶段 0 的 stable driver 与既有 legacy run 继续使用当前 state 格式和写入语义。
3. 禁止同一 run 按子命令在 legacy 与 engine 之间回退、翻译或双写。
4. engine run 不由 legacy 命令续跑；legacy run 不由 engine 改写。
5. 过渡 façade 只是开发期脚手架，最终阶段必须删除。

每阶段开始候选升级验证前，候选环境中不得存在需要由下一 definition/schema 继续写入的活动 run。严格版本拒绝只在这一前提下不会破坏正常恢复。

每个阶段还必须生成一份命令路由矩阵，逐项覆盖当前和最终公共面的全部 workflow 子命令：`start`、`show`、`status`、`next`、`diagnose`、`resume`、`abort`、`reset`、`requirement`、`route-candidates`、`slicing`、`settle-findings`、`route`、`route-add`、`qa-worktree`、`prepare-gate`、`prepare-action`、`claim-dispatch`、`record-action`、`record-gate`、`qa-design`、`qa-review`、`qa-execution`、`qa-execution-scope`、`snapshot`、`cleanup`、`carry`、`authorize-repair`、`seal` 以及新 `drive/submit`。矩阵逐项明确 runtime、唯一 writer、schema/definition 版本、允许的状态变化、错误码和是否只读。保留的兼容入口只能调用对应 engine handler；不支持的入口必须显式拒绝；任何直接写 state、绕过 `submit` 或公开删除活动 run 的路径都必须有 negative test。`diagnose` 的 raw read 是唯一例外，不得被当作 workflow writer。

同一矩阵还要覆盖 top-level 维护/transport 面：`hook`、`lifecycle capture/verify`、`canary`、`gate`、`install`、`uninstall` 和 `package`，以及 registry `admission/register/reconcile`、cutover、rollback 的受支持维护动作或内部 owner handler。逐项标明它是只读、只写外部 observation/receipt，还是委托 engine `submit`；生命周期事件可以写 observation buffer，但不得直接改变 workflow state，install/package 不得拥有 workflow writer。每个 registry/cutover 动作都必须记录 owner、generation/token、receipt schema、恢复入口和权限边界。所有这些入口都必须经过 registry launcher/lease 或 per-operation token，并有“绕过 submit、绕过 freshness、绕过 scope/host 隔离”的 negative tests。

矩阵的 `start` 行不得引入任何拆分意向声明（无 `--split` 参数）：`start` 不接受也不冻结拆分意向；拆分绑定唯一发生在 start-readiness PASS 后的拓扑确认——split 需精确拓扑、no-split 需理由留痕，确认前用户可改变意向、确认后不得重切（变更走用户需求变化、reset/rebuild 或 abort）。阶段 4/5 的 no-split/split 出口都以该拓扑确认时点为准，不得恢复启动时声明，也不得用宽泛的“拆分决定”替代该唯一绑定点。

### 2.4 全局安装切换与活动 run fencing

固定稳定插件跨阶段继续驱动 formal-gates，因此最终切换不是单个测试项目的局部操作。每次候选升级或最终全局切换前，必须从登记的所有项目 root、host home、state/resource root 和安装 scope 建立可复核的活动 run inventory；不能只扫描当前候选项目的 `.gates/tmp`。

registry 不是某个项目的 `.gates/tmp`，而是由安装清单钉死的、跨 global/project scope 共用的用户级 registry root；候选验证通过显式配置使用完全不同的 registry namespace。每个安装 target、host hook、项目 root、state/resource root 和 runtime identity 都必须有 registry record，record 的 canonical paths 与 scope 是可机械校验的。阶段 0 必须先交付 admission bridge/launcher：所有文档化的 global/project 安装 target 和 host hook 都指向该 launcher，真实 immutable runtime 放在 launcher 管理的 sibling 路径，不能再暴露未登记的绝对旧 binary。首次 bootstrap 的公开入口固定为冻结 stable driver 提供的 `install --bootstrap` 维护动作；它只建立 registry/launcher record 和 bootstrap receipt，不创建 workflow state。

bootstrap/migration 必须覆盖现有 global 安装和受支持的 project roots：冻结 stable driver 先取得 install/uninstall lock，再取得 registry lock，写入 bootstrap intent，逐项登记 root/scope/host/runtime、epoch/generation、lease/token 和 canonical-path receipt；所有 record 提交成功后才允许第一次 `workflow start`。registry 不存在时允许这一次 bootstrap 创建；已有 record 缺失、冲突、registry 不可达或无法对账时，只留下可审计的 disabled/`UNREGISTERED_INSTALL` receipt 并停止，不得先创建 `.gates/tmp`。之后 `workflow start` 在创建 `.gates/tmp` 前再次 admission/校验 root；无法登记或发现 `UNREGISTERED_INSTALL` 时在写入前硬拒绝。已有 project-scope target 必须迁移到 launcher 或留下可审计的 disabled/待处理 receipt；最终切换遇到任何未登记 target、root、旧 launcher lease 或未知 scope 都只能 Operator/Wait。legacy launcher 为固定稳定驱动取得覆盖整个进程调用的 invocation lease，并在入口检查 admission epoch；engine 则在每个 intent/receipt/commit/cleanup 使用 per-operation fencing token。两者都不能通过直达 runtime sibling 绕过 registry。

切换协议是显式的、只前进的 generation 状态机（`authority` 为 stable 或 candidate）：

```text
OPEN(E, stable)
  -> DRAINING(E, stable)       # 禁止新 stable lease，已有 lease 可受控排空
  -> SWITCHING(E+1, candidate)  # 排空完成后才推进 generation，只有 cutover token 可写
  -> OPEN(E+2, candidate)
```

候选安装、pointer/hook/config/rule/registry staged record 提交和 post-switch/pre-commit smoke 都在 `SWITCHING` 下由带 cutover token 的同一 owner 执行；普通 stable/candidate `start` 一律拒绝。`post-switch` 特指 release/installed target 已切换而公共 pointer/config/registry 尚未提交，smoke 必须从实际 installed path 启动；只有 smoke 通过后才共同提交 runtime 与 registry。失败回退不能恢复任何旧 generation，而是继续前进：

```text
SWITCHING(E+1, candidate)
  -> ROLLING_BACK(E+2, stable)  # 恢复旧 pointer/config，并用 rollback token 做旧包 smoke
  -> OPEN(E+3, stable)
```

因此已 fence 的 token 永久 stale；回退后重新开放的是新 generation 的 stable lease，不是旧 token。候选在全局切换前不得创建活动 run；若 smoke 或崩溃恢复发现意外活动候选 run，先阻断并 reconcile，不能静默回退。

每个状态转换都遵循以下锁与排空规则：

1. writer 先取得 registry admission lock、校验 token/lease，再取得单 run state lock，按相反顺序释放；不能先持有 state lock 再等待 registry lock。
2. cutover 先取得 install/uninstall lock，再在短临界区取得 registry lock 写入状态；不得持有 registry lock 等待活动 run 排空或等待外部 smoke。`DRAINING` 期间只拒绝新的 stable lease，已有 lease 可以完成或正式 abort；排空超时进入 Wait。
3. 排空确认 zero stable leases、active runs、未完成 intent、UNKNOWN receipt 和 host/lifecycle attempts 后，才推进到 `SWITCHING(E+1)`。推进后所有旧 token 返回稳定的 `STALE_FENCING_TOKEN` 并进入 reconcile；inventory 后才开始的旧 writer 不得取得许可。
4. 只有 `OPEN(E+2, candidate)` 并再次核对全 inventory 后，才允许 candidate 首次 `start`。必须有并发故障测试覆盖：排空前的旧 lease、排空后的旧 start、切换后迟到的旧 commit、candidate smoke 失败、进程崩溃恢复以及回退 generation 仍单调递增。

若 registry 无法穷举、出现竞态或有未对账状态，切换进入 Operator/Wait，不得继续安装或静默降级。

## 3. 八个正式阶段

### 阶段 0：分发安全与基线冻结

目标是先保证后续开发不会污染当前稳定插件，而不是提前改变 workflow 语义。

范围：

- 建立 legacy 正常行为 characterization、package validation、portable canary 和安装 smoke 基线。
- 在任何阶段 worktree 创建前记录初始 Git commit、SVN revision 或 P4 changelist，以及固定稳定 binary/package/installed-target digest、host hook/config 和 managed-rule canonical paths；这组 identity 是阶段 0 的不可变起点。
- 把当前稳定驱动插件冻结成不可变复制品，取消稳定安装中 `prompts/`、`gates/` 对开发工作区的 live symlink；`package validation` 增加 `Lstat`/realpath 和 digest 检查，不能用会跟随 symlink 的 `os.Stat` 代替。
- 在复制稳定驱动的同时由冻结 stable driver 调用一次性 `install --bootstrap`：从冻结的已安装 artifact（而不是当前 worktree）登记所有已知 global/project target、host hook、root、state/resource root 和 runtime sibling，提交 bootstrap receipt 后才允许首个 workflow state 写入；现有 target 要么迁移为 launcher 管理的 target，要么留下 machine-readable disabled/`UNREGISTERED_INSTALL` receipt，不能以人工“已检查”替代。之后所有文档化入口都必须经过 bridge；固定稳定驱动的用户语义不变，但未包装的旧绝对 binary 不再是受支持入口，候选不得执行 bootstrap 或写 stable registry。
- 把 runtime、prompts、gates、host hook/config、managed rule、release/current pointer 和 registry record 纳入同一安装事务：同一 native owner 先获取 install/uninstall lock，再获取 registry lock，写入包含旧/新 runtime、pointer/config、registry digest、generation/token 的持久 recovery journal，准备 sibling 临时目录和备份，复制并校验完整 manifest，切换 release/installed target（journal=`switched`），从实际 installed path 执行 post-switch/pre-commit smoke，最后原子提交 pointer/config 与 registry；任一复制、hook、rule、pointer、registry 或 smoke 失败都恢复旧安装、旧配置和旧 registry。
- 对每个 intent 前后、删除/替换前后、hook JSON 解析失败、managed-rule 写失败、pointer/registry 换位失败、进程崩溃重启和 post-switch/pre-commit smoke 失败注入故障，证明旧稳定包和旧 registry 仍可用；`install.command`、`install.ps1`、Go installer 和 admission bridge 必须遵循同一事务协议并能从 journal 恢复。脚本不得先于 native owner 删除 release、切换 pointer 或写 registry，失败必须留下可关联的 failure/recovery receipt。
- 建立阶段候选目录、测试项目、状态/证据目录和包摘要记录格式。
- 建立 requirements-precedence/supersession 清单，扫描所有旧 OpenSpec/root requirements 和 plan 文档，标记当前权威、正交、已 supersede 与历史项；本阶段不删除历史文件，但不允许旧公共入口文档继续冒充本重构的当前契约。
- 最终公共契约只冻结在设计文档和 fixtures；本阶段不提前把运行时 `SKILL.md`、README 或 prompts 改成尚未实现的 `drive/submit` 语义。
- 固定未来版本化 engine/candidate surface 的 schema/definition 版本常量、来源、definition digest 和 bump 规则，建立该 surface 在缺失/不匹配时写前返回 `UNSUPPORTED_RUN_VERSION` 的 fixtures；阶段 0 的 stable driver 与既有 legacy run 不因缺少新字段而拒绝正常写入，也不迁移或回写旧状态。与此同时固定 `diagnose` 的最小 envelope/raw parser、terminal summary 版本回落和只读边界。

Seal 后状态：插件公开行为仍是当前 legacy，固定稳定驱动环境与开发 worktree 已经真正隔离，安装失败可回到旧稳定包，后续阶段可安全修改 prompts、gates 和安装内容。

### 阶段 1：纯决策内核与只读 Shadow

目标是建立确定性计划计算能力，但不写权威 workflow 状态。

范围：

- 开工前定稿 ADR-001（typed Go authoring + 编译式 canonical 定义制品）并完成六种代表性 step（engine local、durable side effect、host action、agent task、human ask、parallel/join）的小型 compiler spike，确认 compiled IR、registry 与 canonical encoder 边界；spike 不进入 production。
- 按封闭类型变体 + constructor + 显式节点/步骤表定义 `RunPhase`、`TaskKey`、`TaskTransitionTable`、`NodeExecutionPlan`、`StepSpec` 与 `NextResult`。
- 实现 closed-world definition compiler（registry 解析、全局图不变量、归一化、authority/runner 派生、canonical 编码）、`Observe`、`Decide`、`SelectIssued` 与 canonical Plan；compiler 同一生成动作产出 `definitions/workflow.json` 与期望身份常量，禁止人工双写 digest。
- 实现 DecisionAuthority、RunnerKind、合法 reason 与 failure-class 的静态校验；八类非法定义拒绝按 enforcement matrix 分层拦截且结果全保留。
- canonical 制品独立验收：authoring source 重新生成 checked-in 制品字节无 diff（freshness CI，不能用 round-trip 替代）、任意 assembly 顺序同字节、decode→encode 字节不变、跨进程/重复构建同字节、definition/package digest 分离、语义变化必变 definition digest、registry 完备性、constructor 非法状态测试、mutation tests；复杂度止损规则：新增普通业务节点不得要求修改 compiler core，compiler 不得理解具体业务语义。
- `MISSING_ENGINE_ADAPTER` 只能作为 diagnostic-only marker；正常 compile/drive 必须路由为 `BLOCKED_BUG` 并拒绝签发 Ready/HostAction，最终候选必须有 marker 扫描证明不存在该技术债。
- 以 fixtures、golden traces 和 property tests 验证合法边、非法图、稳定排序、完整 frontier、乱序/遗漏/重复拒绝。
- Shadow 只读取 legacy 状态和外部事实，输出预测与差异；不改写 state，不触发副作用，也不参与用户正式决定。
- 从阶段候选的实际 installed binary 启动独立 test project，回归 legacy 正常公开入口，并执行声明的 shadow/diagnostic harness；不得只用 source tree 或纯 Go 单测证明候选可用。

Seal 后状态：legacy 插件所有正常路径保持可用；隔离候选新增可审计的只读计划/诊断能力，尚不存在第二个状态写入权威。

### 阶段 2：持久协议与恢复内核

目标是在隔离 namespace/test harness 中完成 engine 写入与恢复协议，不接管正常公开流程。

范围：

- 实现版本 envelope、原子保存、完整性摘要、文件锁、revision/CAS 和 external fingerprint 重验。
- 实现 expected tasks、Attempt、pending action、typed request/event/action、幂等 `submit` 和 freshness 校验。
- 实现 SpawnReceipt、worker result、Ask/Operator、HostAction receipt、lifecycle event 的统一接纳。
- 实现 intent -> execute -> observe/reconcile -> commit、副作用 UNKNOWN、result-before-receipt、旧 Attempt、重复 submit 和中断恢复。
- 用 fake host、fake worker 和 fake VCS 在每个持久边界做确定性故障注入。
- engine 写入只进入隔离状态目录；正常公开插件仍由 legacy runtime 完整驱动。
- 从阶段候选的实际 installed binary 启动独立 test project，执行 legacy regression 和声明的 protocol/recovery harness；验证候选 host/config/state/resource namespace 不污染固定稳定环境。
- 对缺失版本字段、schema/definition mismatch、旧版本写入和 raw `diagnose` 建立负向测试；`show/status/next` 只从带版本绑定的 terminal summary 回落。

Seal 后状态：持久协议可独立验证和恢复，但不会改变稳定插件的公开 workflow 行为。

### 阶段 3：最小纵向 engine 闭环

目标是用最小、完整、可 Seal 的产品路径证明新公共面和 façade 的整条 run 路由。

范围：

- 实现 `workflow start`、`drive`、`submit`、`show`、`status`、`next`、`diagnose` 的 engine 路径。
- 先以 lightweight 跑通 start -> 需求登记 -> Seal 内部自动 terminal cleanup -> Complete；不通过公开 `workflow cleanup` 推进或删除活动 run。
- 完成 Ask、Ready、HostAction、Wait、Operator、Complete 六类外部边界的最小真实闭环。
- façade 按 run envelope 选择整条 legacy 或 engine runtime；同一 run 不允许跨 runtime。
- 隔离候选运行 engine lightweight；固定稳定插件继续承担当前开发流程与 legacy 正常入口。
- engine loader 在任何写入前严格校验 `workflowDefinitionVersion` 和 `stateSchemaVersion`；缺失或不匹配返回 `UNSUPPORTED_RUN_VERSION`。仅 `diagnose` 可使用最小 raw/envelope parser 只读报告，terminal summary 必须保留 writer、schema、definition 和 package digest。
- 以同一 run 交替尝试 legacy 写入口、engine `submit` 和公开 cleanup，证明旧入口不能绕过唯一 handler，公开 cleanup 不能删除 engine 活动 run。

Seal 后状态：候选已经有第一条真正可用的 engine 端到端路径；regular、split 和多 VCS 尚未宣称由 engine 支持。

### 阶段 4：Git 非分片 regular 全流程

目标是在一个真实宿主上完成 Git no-split regular 的完整 engine 交权。

范围：

- 迁移 intake、产品审、技术审、start-readiness、start-readiness PASS 后的拓扑确认（no-split 需理由留痕）和 full/custom 路线。
- 迁移开发、黑盒/白盒 QA、普通门、完整候选 freeze、validation-view reuse、promotion、repair 和三轮规则。
- 实现任意非终态需求变化、finding/remedy 处置、adopt-external、reset、abort、中断和资源 cleanup；typed contract 至少覆盖 `REQUEST_REQUIREMENT_CHANGE`、`REVIEW_FINDING_FIX`、`VALIDATION_DETAIL_DISPOSITION`、`QA_ARTIFACT_REPAIR`，并固定 `ReviewScopeMode` 的 `FULL`/`AFFECTED` barrier 和“新鲜复审”要求。
- 完成 Git provider 的 status/diff/track/commit/snapshot/squash、whitebox workspace、candidate promotion 和 cleanup。
- 在一个真实宿主上跑 Git 非分片 full canary，覆盖至少一次 FAIL -> repair -> 新候选 -> 重验 -> Seal。
- 对所有仍保留的 legacy 公开写命令执行 route matrix：每个命令只能转发到对应 engine handler 或明确返回 unsupported；不得直接写 engine state、调用公开 cleanup 或绕过 `submit`。同一 run 交替调用旧入口与 engine 入口的 negative tests 必须证明 writer/schema/definition 绑定不可改变。

Seal 后状态：隔离候选可用于 Git 非分片 regular 的完整正式流程；fixed stable driver 和 legacy run 仍不受影响。

### 阶段 5：Git 分片、child 与 merge 闭环

目标是在已经稳定的 Git 非分片能力上增加完整分片生命周期，不同时引入其他 VCS 变量。

范围：

- 实现精确 topology、child 创建、`sliceID -> childRunID` map、继承、逐 child route 和级联控制。
- 实现 `SLICE_READY`、包含最终白盒测试的 child identity、durable receipt、case/cost 汇总。
- 实现 Git child worktree、主线集成、冲突窄 agent 边界、合并 QA、合并门、AffectedChildSet 和主线 repair。
- 实现 child/master 的 reset、abort、需求变化、cleanup 和崩溃恢复。
- 跑 Git 分片 master/child/merge full canary，同时回归 Git 非分片 full。

Seal 后状态：隔离候选在 Git 上同时支持非分片和分片完整流程；SVN、P4 和剩余宿主仍不宣称完成。

### 阶段 6：SVN/P4 与四宿主完成

目标是在 workflow 语义已经稳定后完成 provider 和 host 的机械适配面。

范围：

- 按同一 VCS adapter 契约实现 SVN 的 working copy、revision/property、integrate/commit、candidate identity、receipt 和 cleanup。
- 按同一 VCS adapter 契约实现 P4 的 client/workspace、changelist/filetype/view、integrate/submit、candidate identity、receipt 和 cleanup。
- 完成 Claude Code、Codex、Cursor、DeepSeek Harness 的 provider identity、bridge、dispatch、lifecycle、resume/terminate 和 receipt 对账。
- 每个 host canary 必须使用候选专属 host home/config、hook/managed-rule 路径和 state/resource roots，并记录 canonical-path disjoint proof；不能调用固定全局 binary、全局 hook 或稳定 host home。
- DSH canary 必须使用隔离 `DSH_HOME` 和候选 Cordis bridge，使 binary 解析为 required DeepSeek provider；project-local DSH 的 `ProviderDefault/UNAVAILABLE` 不能作为 lifecycle full canary 证据，provider/bridge mismatch 必须硬拒绝。
- SVN、P4 和不同宿主可在本阶段内部使用独立 worktree/working copy/client 并行开发，但必须在同一候选 join 后统一审查和 Seal。
- 跑 SVN/P4 的非分片与分片 full canary，并完成四宿主各自的 Git 非分片 full canary；每条 full canary 都覆盖产品/技术审、并行开发与黑盒设计、whitebox authoring/review、candidate freeze、view reuse 或受影响重跑、FAIL -> repair -> 新候选、promotion 中断恢复、receipt/result、无残留 cleanup 和 Seal。
- 每条证据绑定 provider/bridge、source identity、installed-target/package digest、snapshot、candidate/promotion/cleanup receipt；不能用 hook-only、lifecycle-only、source tree 或旧全局包冒充候选 full canary。
- 回归 Git 非分片、Git 分片和 fixed stable driver 正常入口。

Seal 后状态：隔离候选已经覆盖最终要求的全部 provider、split 形态和宿主，但 legacy/过渡 façade 尚未删除，全局安装尚未切换。

### 阶段 7：唯一权威切换与最终发布

目标是从功能完整候选收敛到没有第二权威路径的最终产品，并只在全部最终证据成立后切换一次全局安装。

范围：

- 将仍需的旧能力内收为 engine handler 后，删除旧公开推进命令、别名、运行时兼容 decoder/migration、legacy mode、authority handoff、legacy QA、过渡 façade 和公开 cleanup；保留仅供 `diagnose` 使用的最小 raw/envelope parser、terminal summary fallback，以及维护面允许保留的 hook/lifecycle/canary 和 `write_block` 辅助边界。
- 最终清理扫描必须确认 executable definition 中不存在 `MISSING_ENGINE_ADAPTER` 或 `BLOCKED_BUG` marker 伪装成可执行路径；`REQUEST_REQUIREMENT_CHANGE` 的两阶段 barrier、`REVIEW_FINDING_FIX` 的新鲜复审、`VALIDATION_DETAIL_DISPOSITION`、`QA_ARTIFACT_REPAIR` 和 `ReviewScopeMode FULL/AFFECTED` 的 freshness 证据都必须绑定最终 candidate。
- 在最终公共面真正可用后，同步切换 `SKILL.md`、README、references、prompts、catalog 和测试；不得保留指导用户调用已删除入口的文档。
- 根据阶段 0 建立的 requirements-precedence/supersession 清单，更新或标记所有仍把 `prepare-*`、`record-*`、`qa-worktree`、公开 `cleanup` 等旧入口写成当前公共面的旧 OpenSpec/root 文档；历史文档保留可追溯性，但不能继续作为已确认的当前实现契约。
- 从主线集成后的唯一 post-integration canonical revision 构建不可变隔离安装副本，并以 promotion/integration receipt 绑定到清理完成的 sealed candidate；若 digest 不等价，必须先重验并取得新的 Seal。
- 在该 post-integration revision 的包摘要和 installed-target digest 上重新运行 Git/SVN/P4 × 非分片/分片六格和四宿主 Git 非分片 canary；允许按总需求定义复用一个 Git 非分片单元格，最低九条完整 canary。每条都必须覆盖产品/技术审、并行开发+黑盒设计、whitebox authoring/review、candidate freeze、view reuse/受影响重跑、FAIL -> repair -> 新候选、promotion 中断恢复、receipt/result、无残留 cleanup 和 Seal。
- 完成前置产品审、技术审、QA 和选定 gates，但在主线集成前不 Seal；final-release run 保持 ACTIVE，集成后用受支持的 `workflow resume --adopt-external --reason` receipt 绑定 post-integration canonical identity，重建候选，重新执行安装 smoke、cleanup 核对、全部最终 canary 和切换前 QA，并将这些结果绑定同一个 final-release run。无法安全 rebind 时新建 final-release run，前置结果全部降为非权威说明。
- 只有 post-integration candidate 的全部最终证据、全局 registry bootstrap/inventory receipt 和所有 abort/complete/UNKNOWN 处置记录成立后，才由基线 stable driver 执行阶段 7 唯一的 final-release Seal；随后按全局 inventory/fencing 协议冻结新旧 writer，确认不存在会被新版本拒绝的活动旧版本 run，再执行一次覆盖 runtime、hook/config、managed-rule、release/current pointer 的原子全局切换；切换成功且逐 host 的 binary、pointer、hook/config、managed-rule、provider/bridge post-switch smoke 全部通过后才退役固定稳定包。

Seal 后状态：全局插件只保留 engine 唯一权威路径；所有最终证据绑定同一不可变候选；旧稳定包在切换确认后退役。

## 4. 阶段依赖与并行边界

正式阶段的主依赖为：

```text
阶段 0 -> 阶段 1 -> 阶段 2 -> 阶段 3 -> 阶段 4 -> 阶段 5 -> 阶段 6 -> 阶段 7
```

不并行推进会改变 schema、definition 或公共写入口的正式阶段。允许的并行只发生在同一正式阶段内部，并由该阶段统一 join、验证和 Seal，例如：

- 阶段 1 内不同 compiler/property-test work package；
- 阶段 2 内 fake host、fake VCS 和故障注入；
- 阶段 6 内 SVN、P4 和四宿主 adapter；
- 阶段 7 内不同 canary 单元格。

任何内部并行分支都不能单独成为下一阶段基线；只有该正式阶段的共同候选 Seal 后才能推进。

## 5. 每阶段退出条件与最终退出条件

每个阶段都要完整走该阶段自己的 formal-gates 流程并 Seal，但 Seal 的证据只要求本阶段已承诺的能力，不把尚未实现的最终公共面倒灌到早期阶段。以下 1–6、8–10 是所有阶段的共同条件；7、11、12 按“本阶段实际 surface”验收，最终公共面附加条件只在阶段 7 生效：

1. 阶段需求、技术方案、范围和路线已按该阶段 formal-gates run 单独确认。
2. 所有范围内 P0/P1/P2/P3 obligation 已按项目规则闭环，不存在未处置的阻塞结果。
3. `go test ./...`、package validation 和 portable canary 通过。
4. 固定稳定插件的文档化正常入口 smoke 通过，且其安装内容未指向阶段开发 worktree；候选 `Lstat`/realpath、source/installed digest 和 host-path disjoint proof 通过。
5. 本阶段新增能力在不可变候选安装中由实际 installed binary 执行，并在独立测试项目、host config、state/resource namespace 中完成端到端验证。
6. 本阶段承诺支持的既有 engine 路径完成回归；未承诺支持的路径明确拒绝或继续由绑定的 legacy runtime 处理，不出现半迁移。
7. 对本阶段实际交付的 executable/diagnostic surface 绑定 state writer（若有）、schema/definition version、definition digest、候选提交、集成后 canonical identity、包摘要和测试证据；相关的 `UNSUPPORTED_RUN_VERSION`、raw `diagnose` 和 terminal summary fallback 负向测试通过。未实现的最终 surface 必须显式拒绝或保持绑定的 legacy/Shadow 语义，不得用缺省实现冒充完成。阶段 1 的 `MISSING_ENGINE_ADAPTER` 只能留在 diagnostic-only fixture，不能编译或签发 Ready/HostAction；“最终 executable definition 无 marker”、全部 typed request/change/fix 和 `FULL/AFFECTED` freshness 证据属于阶段 7 的附加条件。
8. 通过覆盖本阶段所触及 scope 的 inventory/fencing 和 registry bootstrap receipt 确认没有本阶段候选无法恢复的活动 run、未完成 intent、未对账 UNKNOWN receipt、未登记 target/root 或登记资源残留；阶段 7 再扩展为最终全局 inventory，不能在早期阶段以尚未存在的 engine writer 证明最终切换已完成。
9. 阶段 worktree 的变更已提交，正式审查和 QA 基于该候选完成，阶段 Seal 成功；阶段 7 的前置审查只能是 provisional，最终 Seal 必须在 post-integration final-release run 中完成。
10. 阶段 0–6 的 sealed candidate identity 已通过 integration/promotion receipt 绑定到 post-integration canonical identity；阶段 7 则要求 final-release Seal、全部最终 canary 和 post-integration identity 直接绑定，不能用先 Seal 的旧 run 代替。
11. 当前阶段实际暴露的 workflow/维护入口 route matrix 已逐项绑定到本阶段允许的 legacy、Shadow、隔离 engine 或明确 unsupported surface；直接写 state、绕过 `submit`、绕过 freshness 或公开 cleanup 的 negative tests 通过。本阶段实际 writer 的 epoch/fencing token 或 legacy launcher invocation lease 有 stale-token/lease rejection 和并发窗口测试；阶段 7 才要求当前与最终公共面的完整矩阵及全部 writer 路径。
12. 本阶段涉及的 runtime、hook/config、managed-rule、pointer、provider/bridge 和 recovery journal 故障窗口有可恢复证据；阶段 0 验证安装/registry bootstrap，阶段 1–6 验证候选 namespace，阶段 7 才执行最终原子全局切换和逐 host post-switch smoke。

阶段 7 在上述共同条件之外还必须同时满足：最终 executable definition 不存在 `MISSING_ENGINE_ADAPTER/BLOCKED_BUG` marker；`REQUEST_REQUIREMENT_CHANGE` 两阶段 barrier、`REVIEW_FINDING_FIX` 新鲜复审、`VALIDATION_DETAIL_DISPOSITION`、`QA_ARTIFACT_REPAIR` 和 `ReviewScopeMode FULL/AFFECTED` 的 freshness 证据齐全；当前/最终完整 route matrix 和所有 engine writer/legacy launcher fencing 路径通过；最终九条 canary、registry inventory、原子全局切换、逐 host post-switch smoke 和旧 stable 退役证据全部绑定同一 post-integration candidate。

阶段记录至少保存：

- 阶段编号、run ID、sealed commit 与主线集成 commit；
- 包摘要、installed-target digest、state schema version、workflow definition version、definition digest；
- 固定稳定插件摘要和候选安装摘要；
- 本阶段公开能力矩阵与唯一 writer；
- 正常入口 smoke、新增能力 E2E、QA/gates 与 canary 证据；
- 资源 cleanup receipt；
- 下一阶段 worktree 的精确 post-integration canonical base，以及与 sealed candidate identity 的关联 receipt。

## 6. 失败与回退原则

1. 阶段开发或候选验证失败，不改变固定稳定驱动插件；修复只发生在该阶段 worktree。
2. 阶段未 Seal，不创建下一阶段正式基线。
3. 阶段 Seal 后的后续开发失败，回到最近 sealed commit 和对应不可变候选；不从未提交 worktree 恢复。
4. 最终全局切换前，候选始终在隔离目录验证；不通过覆盖现有安装来“试一下”。
5. 最终原子换位失败时，按 `SWITCHING -> ROLLING_BACK -> OPEN` 的单调 generation 协议恢复旧稳定 runtime、hook/config、managed-rule、release/current pointer；绝不把 registry epoch/generation 写回旧值。只有 rollback smoke 和新的 stable generation 成功后才允许再次 start，且新版本尚未创建 run 前才允许回退安装指针。
6. 新版本一旦创建活动 run，不通过旧版本继续写该 run；需要回退时先按新版本完成或正式终止活动 run。

## 7. 文档关系与旧主需求处理

`openspec/changes/orchestration-pipeline-engine/master-requirements.md` 保留为总需求、非目标、最终公共面和最终验收口径的权威来源，不因分阶段实施而删除。`refactor-plan/final-implementation-draft.md` 保留为目标架构说明。

阶段 0 另建立 `requirements-precedence/supersession` 清单，枚举所有 `openspec/changes/**/master-requirements.md`、根目录需求和旧 plan 文档，逐项标明 `current-authority`、`orthogonal`、`superseded` 或 `historical` 及其替代文档。只有编排流水线重构以本文主需求为 current authority；其他正交需求仍按自身范围生效，冲突的旧入口文档在阶段 7 前标记 superseded，不靠删除文件消除歧义。

主需求和方案稿中的“阶段 0–6”是总体实现工作包编号，不是要求实施时只能有七个 Seal，也不与本文的增量 Seal 编号同义。本文把它们细化为八个可独立 Seal、每个 Seal 后仍可继续开发的切片，映射固定如下：

| 总体方案工作包 | 增量 Seal 阶段 | 说明 |
| --- | --- | --- |
| 0 冻结契约 | 0 分发安全与基线冻结 | 同时冻结 contract fixtures、稳定驱动和 registry bootstrap；不改变公开 workflow 语义 |
| 1 纯决策内核 | 1 纯决策内核与 Shadow | 只读，不产生第二 workflow writer |
| 2 可靠写入与动作协议 | 2 持久协议与恢复内核 | 仅隔离 namespace/test harness 写入 |
| 3 完整流程迁移 | 3 最小纵向闭环 + 4 Git regular | 先证明最小 engine 路由，再扩展到 Git 非分片完整流程 |
| 4 split 与三 VCS | 5 Git split/child/merge + 6 SVN/P4/四宿主 | 先固定 Git split，再接入其他 VCS/host |
| 5 删除旧逻辑 + 6 最终 canary/安装 | 7 唯一权威切换与最终发布 | 这两个总体工作包在最终一个 Seal 内严格按“前置验证（不 Seal）→ 主线集成 → 重建最终候选 → 最终 canary → 基线 driver 执行 final-release Seal → 全局切换”顺序完成；阶段 6 增量 Seal 绝不等同于最终交付 |

任何引用总体方案阶段号的需求、任务或验收记录都必须同时注明本文增量 Seal 阶段号；只有增量阶段 7 Seal 后，才可宣称主需求的总体阶段 5/6 和本次最终交付完成。

每个阶段另建自己的 requirements/design/tasks 文档，只登记该阶段新增或迁移的能力、明确依赖的上一 sealed baseline、阶段内不做的内容和本阶段退出条件。阶段文档不能修改或缩小总需求；若实际实施需要改变已确认的总需求或重大技术选择，必须先走需求变化流程，而不是在子阶段静默改写。
