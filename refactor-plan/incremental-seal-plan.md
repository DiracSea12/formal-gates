# formal-gates 分阶段 Seal 开发计划

> 状态：2026-09-07 用户修订为“固定稳定插件驱动 + 阶段候选隔离验证 + 阶段 4 交付后全局切换 + 阶段 7 升级最终版”。
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
5. 阶段 0–3 不替换全局稳定安装；阶段 4 交付后发布到全局，阶段 5/6 保持该版本，阶段 7 再升级最终完整版本。旧逻辑的源码清理仍在阶段 7；早期 canary 不拼接成最终交付证据。

自狗粮硬规则：阶段 0–3 保留各自历史 driver 绑定。阶段 4 按用户 2026-09-07“用当前版本开发”的决定，使用本阶段需求案登记的精确当前版本作为固定 driver，以新 run ID 绑定；不让当前版续写不兼容的旧状态，也不让阶段 4 候选签发自己的权威 Seal。阶段 4 Seal、主线集成及必要重验完成后，通过既有安装事务把全局入口切换到该发布包；切换与实际全局入口检查成功后，从同一精确包冻结阶段 4 stable driver，驱动阶段 5–7 的正式 run。阶段 5–7 的开发驱动使用 Codex/Git no-split engine 路径，新增 split/VCS/宿主能力仍只由隔离候选作为被测对象执行。每个 run 的 driver 在创建时固定，候选不得驱动自己的正式 run、写稳定 registry 或签发自己的权威 Seal；阶段 7 final-release Seal 由冻结的阶段 4 driver 写出，再把全局入口从阶段 4 升级到最终版。

## 2. 环境模型

### 2.1 三类隔离环境

每个阶段使用三种相互独立的环境：

| 环境 | 用途 | 约束 |
| --- | --- | --- |
| 固定稳定驱动环境 | 驱动本次重构各阶段的 formal-gates 流程 | 阶段 0–3 保留历史绑定；阶段 4 使用该阶段需求案锁定的当前版本；阶段 4 发布后，阶段 5–7 使用同一发布包的冻结阶段 4 安装。每个正式 run 内不换 driver；`prompts/`、`gates/` 和其他输入不得软链接回开发区 |
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

阶段 0–6 固定执行以下源码推进顺序；阶段 4 与阶段 7 的全局发布都以主线集成后的 canonical identity 为输入，阶段 4 增加交付后发布步骤，阶段 7 的最终 Seal 顺序例外见下文：

```text
上一阶段 post-integration canonical identity（首阶段为冻结的初始 VCS identity）
    -> 创建本阶段分支与开发 worktree
    -> 用本阶段固定 stable driver 启动正式 run（阶段 0–3 保留历史绑定，阶段 4 用已锁定的当前版本，阶段 5–7 用阶段 4 包）
    -> 开发、审查、QA、修复
    -> 从已提交候选构建隔离安装
    -> 在隔离测试项目验证新增能力和既有正常路径
    -> 本阶段 Seal
    -> 记录 sealed commit、包摘要和证据
    -> 集成到主线并取得 promotion/integration receipt
    -> 如集成改变树或包摘要，基于 post-integration identity 重建/重验候选
    -> 若为阶段 4：全局切换、实际入口检查与历史功能逐项回归通过，再冻结阶段 4 driver
    -> 以 post-integration canonical identity 创建下一阶段 worktree
```

一个阶段未 Seal 时，不得让下一阶段基于其未完成 worktree 开发。阶段 worktree 在 Seal、主线集成和证据核对完成前保留；完成后按正式流程清理。sealed candidate identity 与 post-integration identity 必须分别记录并以 promotion/integration receipt 关联；若集成产生新的提交、revision 或 changelist，下一阶段绑定后者而不是猜测前者仍等价。

候选隔离测试可以执行候选自己的 `workflow` surface 来验证功能，但这类 run、receipt 和 summary 只能落在候选 namespace，不能被本阶段 stable driver 的正式 run 读取为权威状态。阶段 4 发布前使用本阶段已锁定的当前版本 driver，发布后新建的阶段 5–7 正式 run 使用阶段 4 driver；不能在一个活动 run 中换 writer。旧阶段 0 driver 的阶段 4 run 保留原绑定，当前版新建 run；旧记录的结束/清理使用原版支持的流程，不手改迁移或伪造终态。

阶段 0–6 使用上面的通用顺序；阶段 7 是唯一的最终 Seal 顺序例外：前置开发、审查和候选验证可以先完成，但不得先 Seal；final-release run 在集成期间保持 ACTIVE，先把已审计树集成到主线并取得 post-integration canonical identity，再由阶段 4 stable driver 经 availableActions/REQUEST → adopt-external Ask → typed submit 的授权 receipt 绑定该 identity，重建最终候选、运行全部最终 canary 和切换前 QA，最后才执行阶段 7 唯一的 final-release Seal。前置验证结果只能作为 provisional evidence，不能与 post-integration 结果拼接成最终证据；若 run 无法安全 rebind，必须新建 final-release run，且不得继承前置验证结果。

### 2.3 唯一写入者与版本绑定

过渡期间虽然 legacy runtime 与 engine runtime 同时存在于代码库，但同一个 run 只能有一个权威写入者：

1. façade 只能在 `workflow start` 或最小读取版本 envelope 时选择整条 runtime。
2. 新建的版本化 engine/candidate run 创建后永久绑定 writer、`stateSchemaVersion` 和 `workflowDefinitionVersion`；阶段 0 的 stable driver 与既有 legacy run 继续使用当前 state 格式和写入语义。
3. 禁止同一 run 按子命令在 legacy 与 engine 之间回退、翻译或双写。
4. engine run 不由 legacy 命令续跑；legacy run 不由 engine 改写。
5. 过渡 façade 只是开发期脚手架，最终阶段必须删除。

候选升级和最终安装不等待活动 run 排空。安装不迁移、兼容或续跑旧
generation 的 run；切换后旧 token/lease 由常规 admission 与版本校验拒绝。

每个阶段还必须生成一份命令路由矩阵，逐项覆盖当前和最终公共面的全部 workflow 子命令：`start`、`show`、`status`、`next`、`diagnose`、`guard-preview`、`resume`、`abort`、`reset`、`requirement`、`review-delta-preview`、`requirement-impact-preview`、`authorize-retry`、`route-candidates`、`slicing`、`settle-findings`、`route`、`route-add`、`qa-worktree`、`prepare-gate`、`prepare-action`、`claim-dispatch`、`record-action`、`record-gate`、`qa-design`、`qa-review`、`qa-execution`、`qa-execution-scope`、`qa-thaw`、`qa-suggestion`、`prepare-candidate`、`prepare-review-wave`、`adjudicate-wave`、`resolve-wave-target`、`snapshot`、`cleanup`、`carry`、`authorize-repair`、`repair-preview`、`complete-p2-repair`、`seal` 以及新 `drive/submit`。矩阵逐项明确 runtime、唯一 writer、schema/definition 版本、允许的状态变化、错误码和是否只读。保留的过渡入口只能调用对应 engine handler；不支持的入口必须显式拒绝；最终旧推进命令全部删除，能力去向按矩阵逐项验收；任何直接写 state、绕过 `submit` 或公开删除活动 run 的路径都必须有 negative test。`diagnose` 的 raw read 是唯一例外，不得被当作 workflow writer。

同一矩阵还要覆盖 top-level 维护/transport 面：`hook`、`lifecycle capture/verify`、`canary`、`gate`、`coverage`、`install`、`uninstall` 和 `package`，以及 registry `admission/register/reconcile`、cutover、rollback 的受支持维护动作或内部 owner handler。逐项标明它是只读、只写外部 observation/receipt，还是委托 engine `submit`；生命周期事件可以写 observation buffer，但不得直接改变 workflow state，install/package 不得拥有 workflow writer。每个 registry/cutover 动作都必须记录 owner、generation/token、receipt schema、恢复入口和权限边界。除 coverage 的纯输入计算外，这些入口仍必须经过 registry launcher/lease 或 per-operation token，并有“绕过 submit、绕过 freshness、绕过 scope/host 隔离”的 negative tests。coverage 的 validate、project-whitelist、reconcile-execution 保留为只读 JSON 契约入口，不新增状态或 registry 副作用。

执行前准入矩阵同时列出原生子代理派发/恢复工具、工作流模型 CLI/API 和已声明 QA 操作的
实际调用入口、前置校验点与绑定字段。要求见最终实施方案 §9.1.1、§12.1 第 14 项：执行前
拒绝必须证明底层调用数为零，事后登记拒绝不能替代。本合同在 3.5c Seal 后补充：阶段 4
接入 Controller 并验证首个真实宿主，阶段 6 覆盖五宿主，阶段 7 验证最终安装态；不追溯
宣称 3.5c 或既有只读 guard-preview 已具有该能力。

矩阵的 `start` 行不得引入任何拆分意向声明（无 `--split` 参数）：`start` 不接受也不冻结拆分意向；拆分绑定唯一发生在 start-readiness PASS 后的拓扑确认——split 需精确拓扑、no-split 需理由留痕，确认前用户可改变意向、确认后不得重切（变更走用户需求变化、reset/rebuild 或 abort）。阶段 4/5 的 no-split/split 出口都以该拓扑确认时点为准，不得恢复启动时声明，也不得用宽泛的“拆分决定”替代该唯一绑定点。

### 2.3.1 开发分块计划的引擎接入

开发分块与上面的 Run 级 split 拓扑分开处理。Part 2 `start-readiness` 技术审必须产出结构化
`granularity_review`；它先判断单一 Batch 或多个 Batch，再给出 `Batch → Subtask` 映射、
依赖/接口/状态边界、风险、DoD/测试、回滚/恢复、交接与上下文成本及不拆/改拆理由。用户
在拆分决定与路线确认时确认该计划后，才允许进入开发。

引擎接入固定为：

1. Controller 将已确认的 `granularity_review` 绑定到需求/方案 revision、拆分决定、路线和
   拓扑，并保存计划摘要/digest；校验 ID 唯一、映射无遗漏/重复、TaskKey 归属明确、批间
   依赖无环以及 DoD/验证/恢复/交接字段齐全。
2. 引擎把 Batch 作为现有 TaskKey 的分组与依赖信息承载，不新增 Batch 独立状态机；批次
   完成从成员 task 的终态和已登记的 DoD/验证/交接结果派生。
3. 每次 `development-worker` 派发必须带 `batch_id`，一个 Batch 只能有一次开发派发；其
   `subtask_id` 仅表示该派发内的顺序步骤，不得单独派发、换代理或 reset。
4. 前一 Batch 的成员任务、DoD、验证和交接完成后，下一 Batch 才进入 eligible frontier，
   并允许派发新的零上下文开发代理。同批未完成时 engine/adapter 强制恢复原身份；
   仅实际恢复/查询取得明确不可恢复事实并完成旧执行/副作用对账，才签发同批替换。
   自测失败、RUNTIME_ERROR、retry、重编号或重复计划不能解除绑定；复用既有状态与
   intent/receipt，不另建代理状态机。阶段 4 按 AC-061 实跑合法续用、替换及违规新建零调用。
   Batch 契约、接口、依赖或 DoD 改变时，旧计划失效，
   必须重新做粒度审并取得用户确认。

引擎不负责从自然语言判断最佳拆法，也不按编号、commit 数、文件/行数或阶段名自动切块；
它只机械校验和执行已确认的分块计划。各阶段的具体 Batch/Subtask 清单在该阶段的需求入口
中登记，阶段 3 的清单见 `refactor-plan/stage-3-requirements.md`。

### 2.4 全局安装切换与活动 run 处置

固定稳定插件跨阶段继续驱动 formal-gates，因此最终切换不是单个测试项目的局部操作。安装入口不扫描、等待或阻断任何活动 run；只要安装包、目标路径、registry 与原子事务校验通过，任何时候都可以安装。已存在的 run 不迁移、不转换、不作兼容保证，也不因安装而被改写。

registry 不是某个项目的 `.gates/tmp`，而是由安装清单钉死的、跨 global/project scope 共用的用户级 registry root；候选验证通过显式配置使用完全不同的 registry namespace。每个安装 target、host hook、项目 root、state/resource root 和 runtime identity 都必须有 registry record，record 的 canonical paths 与 scope 是可机械校验的。阶段 0 必须先交付 admission bridge/launcher：所有文档化的 global/project 安装 target 和 host hook 都指向该 launcher，真实 immutable runtime 放在 launcher 管理的 sibling 路径，不能再暴露未登记的绝对旧 binary。首次 bootstrap 的公开入口固定为冻结 stable driver 提供的 `install --bootstrap` 维护动作；它只建立 registry/launcher record 和 bootstrap receipt，不创建 workflow state。

bootstrap/migration 必须覆盖现有 global 安装和受支持的 project roots：冻结 stable driver 先取得 install/uninstall lock，再取得 registry lock，写入 bootstrap intent，逐项登记 root/scope/host/runtime、epoch/generation、lease/token 和 canonical-path receipt；所有 record 提交成功后才允许第一次 `workflow start`。registry 不存在时允许这一次 bootstrap 创建；已有 record 缺失、冲突、registry 不可达或无法对账时，只留下可审计的 disabled/`UNREGISTERED_INSTALL` receipt 并停止，不得先创建 `.gates/tmp`。之后 `workflow start` 在创建 `.gates/tmp` 前再次 admission/校验 root；无法登记或发现 `UNREGISTERED_INSTALL` 时在写入前硬拒绝。已有 project-scope target 必须迁移到 launcher 或留下可审计的 disabled/待处理 receipt；最终切换遇到任何未登记 target、root、旧 launcher lease 或未知 scope 都只能 Operator/Wait。legacy launcher 为固定稳定驱动取得覆盖整个进程调用的 invocation lease，并在入口检查 admission epoch；engine 则在每个 intent/receipt/commit/cleanup 使用 per-operation fencing token。两者都不能通过直达 runtime sibling 绕过 registry。

切换协议是显式的、只前进的 generation 状态机（`authority` 为 stable 或 candidate）：

```text
OPEN(E, stable)
  -> SWITCHING(E+1, candidate)  # 只有 cutover token 可写；不等待活动 run
  -> OPEN(E+2, candidate)
```

候选安装、pointer/hook/config/rule/registry staged record 提交和 post-switch/pre-commit smoke 都在 `SWITCHING` 下由带 cutover token 的同一 owner 执行；普通 stable/candidate `start` 一律拒绝。`post-switch` 特指 release/installed target 已切换而公共 pointer/config/registry 尚未提交，smoke 必须从实际 installed path 启动；只有 smoke 通过后才共同提交 runtime 与 registry。失败回退不能恢复任何旧 generation，而是继续前进：

```text
SWITCHING(E+1, candidate)
  -> ROLLING_BACK(E+2, stable)  # 恢复旧 pointer/config，并用 rollback token 做旧包 smoke
  -> OPEN(E+3, stable)
```

因此已 fence 的 token 永久 stale；回退后重新开放的是新 generation 的 stable lease，不是旧 token。切换前后的活动 run 不阻断安装，它们的旧 token 也不能绕过新 generation 继续写入。

每个状态转换都遵循以下锁与排空规则：

1. writer 先取得 registry admission lock、校验 token/lease，再取得单 run state lock，按相反顺序释放；不能先持有 state lock 再等待 registry lock。
2. cutover 先取得 install/uninstall lock，再在短临界区取得 registry lock 写入状态；不得持有 registry lock 等待外部 smoke。
3. 未完成的安装 intent 或 UNKNOWN transaction receipt 必须先 reconcile，但 stable lease、active run 和 host/lifecycle attempt 不是安装前置条件。推进到 `SWITCHING(E+1)` 后，所有旧 token 返回稳定的 `STALE_FENCING_TOKEN`。
4. 只有 `OPEN(E+2, candidate)` 并核对新 registry identity 后，才允许 candidate 新建 run。必须有并发故障测试覆盖：切换中的新 start、切换后迟到的旧 commit、candidate smoke 失败、进程崩溃恢复以及回退 generation 仍单调递增。

若 registry 自身不可用、安装事务出现竞态或有未对账状态，切换进入 Operator/Wait；无法穷举活动 run 不得单独阻断安装。

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
- 先以 lightweight 跑通“start 前已确认 intake -> `start` -> 首次 `drive` 自动登记 intake receipt/digest -> Seal 内部自动 terminal cleanup -> Complete”；清理中的终结 intent 只投影 `FINALIZING_CLEANUP`/`WAIT`，对账完成后才返回 Complete；terminal summary 标记 `unverified=true`。start 绑定 requirement source/revision 与完整登记工件集，首次 drive 不重复询问或等待确认事件，不通过公开 `workflow cleanup` 推进或删除活动 run。
- 阶段 0 冻结的 stable driver 仍只写既有确认状态；固定 launcher 从已确认 formal run 状态和登记集 requirement/solution 工件派生 `intakeConfirmationReceipt.json`，并按原相对路径注入候选 test project。候选 `start` 唯一从 `--intake-receipt <path>`（或等价 host config 指针）读取 typed 回执，并以当前注入工件重算 binding。回执缺失、旧回执配当前工件或任一 binding 不一致均在首写前返回 `INVALID_INTAKE_CONFIRMATION` 且零写入；候选不生成回执、不读 stable run state、不驱动正式 run，stable driver 不新增 writer/公共入口。
- 本阶段的 `granularity_review` 已登记为单一 Batch `phase-3-engine-vertical-loop`：S1 façade/admission/runtime 选择 → S2 `StartRequest`/intake receipt/canonical digest/envelope → S3 `drive`/`submit`/六类 `NextResult` → S4 terminal cleanup/`Complete`/replay → S5 installed 隔离、legacy 回归与 negative fixtures。五段共享未冻结的 façade、`state.json`、`Store/CAS`、typed protocol、envelope 和 cleanup 接口，不能独立验收或回滚，故不拆 Batch；一个 Batch 只派发一个全新的零上下文开发代理，Subtask 间连续执行、不换代理。验证档位为 full，选定 blackbox、whitebox、complexity-gate、implementation-quality-gate，并在批边界执行项目声明的构建、测试、installed harness、package/canary、stable smoke、digest/namespace、registry/fencing、cleanup 检查。
- 在 engine protocol 的 whitebox/test-only sequence harness 中完成 Ask、Ready、HostAction、Wait、Operator、Complete 六类外部边界的最小真实闭环；阶段 3 installed-candidate 黑盒只覆盖 lightweight 可公开到达的 Complete、façade 读写、版本/身份/旧入口负向和 legacy 回归，其余五类公开路径留到阶段 4 regular。
- façade 按 run envelope 选择整条 legacy 或 engine runtime；同一 run 不允许跨 runtime。candidate 的未迁移旧 workflow 写入口统一显式 unsupported/zero-write，不得误落 stable handler。
- `start` 不新增 runtime、identity 或 digest 参数：候选 installed target 由 admission/launcher registry 的 target identity/package digest 固定选择 engine 并创建 envelope，stable driver 继续选择 legacy；缺失、冲突或未登记的 admission 在写前返回 `UNREGISTERED_INSTALL`。后续所有 façade 调用只信任 envelope 的 owning runtime，不能中途切换。
- 隔离候选运行 engine lightweight；固定稳定插件继续承担当前开发流程与 legacy 正常入口。
- engine loader 在任何写入前严格校验 `workflowDefinitionVersion` 和 `stateSchemaVersion`；缺失或不匹配返回 `UNSUPPORTED_RUN_VERSION`。仅 `diagnose` 可使用最小 raw/envelope parser 只读报告，terminal summary 必须保留 writer、schema、definition 和 package digest。
- 以同一 run 交替尝试 legacy `requirement`/`cleanup` 写入口、engine `submit` 和公开 cleanup：legacy 入口只对 stable legacy run 保持原语义，对 engine run 明确拒绝且零写入；engine 的唯一写入仍是 typed `submit`，公开 cleanup 不能删除 engine 活动 run。

Seal 后状态：候选已经有第一条真正可用的 engine 端到端路径；regular、split 和多 VCS 尚未宣称由 engine 支持。

### 阶段 3.5a：QA 覆盖契约 checkpoint（阶段 4 Batch A，非独立正式阶段）

当前状态：覆盖契约 checkpoint 已 Seal，run/candidate 见 stage-records 的 2026-09-07 状态索引。
本 checkpoint 不计入八个正式阶段，不新增 `RunPhase` 或另一套 QA 状态机；正式受理任务的
Seal 单独留痕。它冻结阶段 4 接入所需的最小 QA 覆盖契约，详见
`refactor-plan/stage-3-5a-qa-coverage-contract.md`：已确认需求/方案中有限的
`requiredSources`、有限 `AcceptanceManifest`、显式 source↔point↔case 映射、逐 source/
逐 point/逐 case review、由 source inventory/manifest/map digest 投影的 approved whitelist，
以及与当前 `ValidationCandidate` 绑定的 expected/actual execution 结果。未知、缺失、重复、
orphan、仅集合级 PASS、`FULL` 对账不全或 `AFFECTED` 的执行/继承/未执行集合不全都必须在
结构层拒绝；`AUTHORIZED_SKIP` 不等同 `EXECUTED_PASS`。

3.5a 已交付 schema、validator、digest/白名单投影、fixtures 和三个只读 coverage CLI；
该 checkpoint 不改 legacy `QACase` 完整结构，不新增状态机或通用框架，也不设置固定用例数、
覆盖率阈值或强制测试工具组合。后续 3.5a-fix 已在 legacy 接入单文件 REQ/AC 保证、
显式 activation、分片责任与完整用例义务；阶段 4/5 迁移这些能力，不重新设计第二套覆盖核心。
覆盖基座与后续消费接入保持先后依赖，不并行实施。

### 阶段 3.5b：成本记录与运行时派发止损 checkpoint（非独立正式阶段）

本 checkpoint 已随审查收敛合并任务 Seal，不计入八个正式阶段，不新增 `RunPhase` 或第二
套账本；guard 本身不新增公开写命令，合并任务的 legacy 治理入口列于 route matrix。
详见 `refactor-plan/stage-3-5b-runtime-guardrails.md`。它冻结两件事的最小合同：
（1）保留现有子代理 dispatch 计量，并用 `workflow start` 已保存的 owner transcript 做
启动/终态快照差值，在可核对时补记主代理用量；（2）规定 engine 派发止损、成本回填和
恢复入口的统一顺序。3.5b 当前代码只交付 legacy owner 计量，以及无 I/O、无状态写入的
engine-side guard allowance/basis 纯计算核心、fixtures 和公开只读
`workflow guard-preview --input <basis.json>`（见 `references/guard-preview.md`）。成功只说明
声明自洽，不认证真实 run/确认/合法任务，不创建 run、不写状态；runtimeIntegration 固定
NOT_INSTALLED。阶段 4 才把它们接入 engine
`State`/envelope、source bridge、签发/恢复入口、typed Ask 和公开展示。`internal/cost`
仍是独立解析/计量包，`report.cost` 仍只做报告；阶段 4 接入前不宣称 engine 已具备运行时
止损能力。

request 额度只由 engine Controller 从 canonical expected external `TaskKey` 集、已确认
route/topology、compiled definition 和有限 retry policy 机械得出；用户、host 和 AI 都不填写
或估算底层数字。TaskKey 只能由 closed-world definition 及已确认、持久化的
Batch/route/topology、批准 QA、已选 gate 或已成立 obligation 派生。当前已确认 workflow
policy 预授权的 obligation/repair slot 由 Controller 机械加入；其他新增槽位若按现有流程
要求用户确认，必须先绑定对应 revision/typed Ask。代理输出/receipt/resume 不得直接扩容。
产品审和技术审各自的前三个 completed semantic review round 也是 workflow policy
预授权上限：只有 finding 处置实际要求 fresh review 或用户明确授权重审时才按 action/revision/round 生成一个
distinct `TaskKey`，并非启动时一次性创建三项。达到三轮后，typed Ask 每次只能为对应
action 增加一个 fresh-review slot；该槽位与 `MaxAttempts` 的瞬态 retry 余量分别计算，不能
互相转换。revision/invalidation 不重置语义轮数，显式 reset 或新 run 才重建；split child
继承 master 的整体产品/技术审，不自行增加 series。
外部 agent/host 派发
没有显式策略时使用 canonical `MaxAttempts=2`（首次 +
一次自动重试）；显式策略必须有限。`MaxAttempts` 是逻辑动作的总执行次数，额外 retry 余量
为 `max(0, MaxAttempts-1)`；绑定/快照/责任变化形成新计划时按新 basis 重算，不沿用旧 retry
池。计算 basis/result 与已用 action/Attempt 账目随同一 guard 投影持久化，签发前重算不一致
即 `BLOCKED_BUG` 且零派发。token cap 只接受项目/安装级维护配置，缺失或 usage 不可靠时标为
unavailable，但 request/attempt 止损仍生效。只重试已分类的 `TRANSIENT_ENGINE_ERROR`，且
所有恢复入口（含 `ResumeAgent`、`RecoveryNewAttempt`、HostAction retry）共用同一 guard；
欠费、配额、权限和参数错误不循环，也不静默换 provider/model。额度或 retry 耗尽必须是
typed Ask：继续只解锁一个按既有 admission capacity 裁剪的 canonical Ready batch，重试只
增加当前逻辑动作的一个 retry slot，二者不接受自由数字且不能预先累积；Wait 只用于容量、
运行中、依赖、可能迟到的 receipt/lifecycle 或 retryable I/O。
3.5b 不做价格库、USD 账单、provider fallback、动态调参或新状态机。

3.5a 与 3.5b 完成后，先落实下述 3.5c 的 QA 提效，再由阶段 4 接入 regular QA/candidate
E2E；这些 checkpoint 不增加正式阶段，也不因编号为 3.5a 与 3.5b 强加实现耦合。

### 阶段 3.5c：QA 提效（黑盒优先，兼顾白盒）

3.5c 已于 2026-09-07 Seal；正式需求见 [阶段 3.5c 正式需求](stage-3-5c-requirement.md)，
原计划见 [QA 提效](stage-3-5c-qa-efficiency.md)，交付与集成身份见 stage-records。
本 checkpoint 接在 3.5a、3.5b 之后、阶段 4 之前，不新增 `RunPhase` 或另一套 QA 状态机。

核心改动为：黑盒共同准备和操作说明只审一次，每条用例仍分别判断；重复步骤交给现有
脚本和测试工具批量实跑，各用例保持隔离；保存本次执行已完整完成的条目，绑定未变时
补跑剩余部分；白盒先独立判断覆盖，再复用有效测试、补齐缺口。高成本前置状态可手动
构造，但待测操作、属于验收目标的安装和真实宿主交互仍须实跑。

QA Review 的可选建议允许采纳、延期或不采纳并记录理由，不再强制改用例才能冻结；
真实验收缺口继续阻塞。最终提交整理在最后一轮必需验证之前完成，Seal 只固化最终
候选的结果。3.5a 的完整覆盖、逐条结论和集合对账，3.5b 的成本与有限重试，以及现有
`AFFECTED`、首次必要 `FULL` 均保留。阶段 4 接入时按此更新相应 QA 语义；当前已安装
规则与历史 Seal 不因计划落档被改写，固定稳定 driver 的冻结和切换仍按本总计划执行。

### 阶段 4：Git 非分片 regular 全流程

目标是在一个真实宿主上完成 Git no-split regular 的完整 engine 交权。

本阶段必须继承合并需求 REQ-015（engine 主需求 §7.2.1、§8.3–8.4 和最终方案 §7）：
Operator semantic adjudication 持有 raw finding、逐条 severity rationale、范围/严重度/因果/
复现证据、finding/problem ID 与 NEW/SAME_AS/DISMISSED 历史；Controller 只校验结构、
引用/版本绑定与机械路由，不自动判真假、严重度、根因或重复。仅核实后处置仍不唯一才
Ask，产品/技术审 confirm/dismiss 保留。白盒按 3.5c 复用或补测；实际产生的测试和用例相关
代码变化全部提交、case-set FROZEN 且绑定最终 review-ready 快照后，一次冻结 expected-set/
digest 并并行准备所有 QA execution/
门；收齐 wave 和核实后，每快照唯一返修只接 QA FAIL、有效 P0/P1 与门审 P2。
门审 P2 carry 不复审，P3 Seal advisory；QA Review 可选 P2/P3 以 suggestion ID 按 3.5c
记录采纳、延期或不采纳及理由后冻结；采纳导致用例含义或共同路径变化时重查受影响部分。
普通返修保持 FROZEN、固定 AFFECTED 执行，本轮必需 QA 与门全部有效 PASS（含合法继承）即可 Seal，不追加保证模式最终 FULL；最终提交整理须先于
最后一轮必需验证，Seal 不再改变该候选。本次执行中断可保留完整条目并按 3.5c 受控续跑。
仅需求含义/对外行为/判据/稳定可执行性变化可 THAW，临时环境失败用 RUNTIME_ERROR。

非 wave 与 wave 的无效结果均终结原 attempt 并保留拒绝字段/原因，默认同 TaskKey 最多
两次，耗尽 RETRY_EXHAUSTED；一次性用户追加 attempt 绑定 state revision 与非空理由，
只改有限 retry policy/digest，不增基础槽位。追加再失败不扩张。相同 OPEN signature 续用、
相同有效 PASS 不自动重派；用户明确重审按下述理由、授权和额度合同处理。finding 派生 guard source 只接 adjudication 完成且 wave 收齐后的
有效 obligation/唯一 repair TaskKey；基础 source 保持 REQ-013。raw finding/result/receipt/
resume 均不扩容，需确认的扩张先绑定确认 revision，预授权 repair 机械加入。

额外返修 Ask 展示完整 findings/problems、逐条理由/归并、历史修复/验证、QA scope、
已用/剩余额度、dispatch signature/TaskKey 与 cost/guard 可用性；授权绑定完整摘要 digest
和 state revision。阶段 4 把 guard-preview.runtimeIntegration 从 NOT_INSTALLED 切换为
权威 engine 集成状态，把修复摘要 engine allowance 从 UNAVAILABLE 切换为权威 projection；
owner cost 与 token cap 的 UNAVAILABLE 各按自身来源/配置独立判断。

requirement impact 迁移到 engine 唯一 writer：meaning-preserved 不发回退 Ask、保留产品审
PASS；meaning-changed 在接纳新 revision 前 typed Ask，绑定影响预览 digest、state revision、
base/candidate revision，展示回退后果/建议失效 PASS/额度，并允许用户指定任意已有 PASS
继续有效。仅用户确认事件可原子登记新 revision、用户保留清单及其余结果的回退，不重置
额度或自动授权新产品审；旧 legacy 入口不得绕过。review-delta 内容确认、影响授权与保留
决定可共用一次覆盖三者的明确用户决定。具体语义以最终方案 §5.2 为准。

范围：

- 实现最终方案 §5.2 的用户验证保留决定：复用现有 result/receipt，绑定原 PASS、旧/新需求和候选、state revision 与明确保留清单；支持全选和逐项选择，affected/unknown 判断不否决用户选择。同步接入推进、覆盖、QA 和 Seal，已保留结果继续满足对应验证条件，并显示为用户授权继承；不伪造新版本执行结果，不将 FAIL/未执行项转为 PASS。未选项沿用影响规则，新增且无既有 PASS 的义务正常验证。
- regular 受理迁移为唯一正式文件 → start 绑定受理 run → 预检并展示全文/revision → typed submit 用户确认；模式未明确时受理 run 保持未绑定，确认后再初次绑定 lightweight/regular，lightweight 沿用同一已确认文件。确认前不选路线、不派后续代理，已绑定 regular 后不能转 lightweight。阶段 3 固定 driver 的 receipt 桥接保持为既有隔离验证合同，不作为最终 regular 的确认顺序。
- 按主需求 §8.4.1 接管显式 guarantee activation、来源、投影冻结与原子写入；full/custom+QA 单文件保证、多个 QA 类型覆盖并集、额外 artifact 拒绝、custom 无 QA/lightweight/waiver 非保证状态和逐 REQ/AC 展示均须正反向验收，不从 route 或 artifact 外观猜测激活。
- 迁移 intake、产品审、技术审、start-readiness、start-readiness PASS 后的拓扑确认（no-split 需理由留痕）和 full/custom 路线；在 engine `State`/Controller 中接管按 master run + action kind 持久化的 `PreDevelopmentReviewSeries`，两个动作各自前三轮自动、超限 typed Ask 逐轮授权，legacy `internal/validate` 过渡字段不得继续作为第二权威。
- 接管审查收敛需求 REQ-002 的技术审分类合同：P0–P3 每项必填分类，仅三类实质问题可形成 P0/P1，实现细节最高 P2；Controller 校验分类/严重度矩阵与核实记录，Operator 核实语义。验收缺分类、非法 P0/P1 仅形成 rejected receipt 并有限重试，以及三类合法阻塞路径。
- 接管 REQ-004 的归并与纠正：RESOLVED/DISMISSED 不重复触发返修，错误归并以绑定 revision 的新裁决保留历史并纠正；派发前更新同一计划，派发后新增 OPEN 义务进入返修所得新快照的下一 wave。分别验收这些路径及同快照不能创建第二份返修。
- 接管 REQ-010 的明确重审例外：相同 PASS 只阻止自动重复派发；用户重审通过 typed Ask/submit 绑定当前输入、非空理由与 override 授权，有自动额度时正常消耗、耗尽时先获适用额外授权，额外返修仍须绑定最新摘要。验收原结果保留、缺理由/授权/额度拒绝，以及不能重置或绕过语义轮次、repair wave 与 retry 额度。
- 迁移开发、黑盒/白盒 QA、普通门、最终 FROZEN case-set/review-ready 屏障、完整候选 freeze、promotion、唯一 repair 和开发后三轮规则；产品/技术审语义轮次、三类 pre-wave review series、开发后 repair wave 与 TaskKey `MaxAttempts` 四类计数必须保持独立。
- QA 接入必须消费阶段 3.5a 的覆盖契约：同一次需求/方案确认中登记
  `requiredSources`，标准需求案每个 REQ/AC 都进入 QA 映射，不允许非 QA 处置绕过；只有其他
  合法 source 可使用自身协议的显式非 QA 验证处置，每个 point
  都有经批准的 case 映射，并形成逐 source/point/case review 与绑定当前
  `ValidationCandidate` 的执行证据；不能用集合级 PASS 或旧候选结果补齐缺项。engine
  `State`/envelope、QA 派发和结果校验共同持久化并核对 source inventory/manifest/map/
  whitelist digest，不另建覆盖状态机。case review 逐条核验公开路径、setup、环境可执行性
  和 oracle；执行结果只接受真实逐用例 procedure/observation/comparison，拒绝 skipped、
  凭空授权 PASS 及模板/占位证据，环境不可用时记录 `RUNTIME_ERROR`。§5.2 的已有真实 PASS
  授权继承通过独立的继承凭据满足对应义务，不伪装为当前候选执行证据。不为对抗性虚假
  Operator 声明引入自然语言语义证明器。
- 同一 State 接管 shared-step/逐共享审批/case 引用与 suggestion 理由处置，验证共享修改使全部引用者失效、单条修改仅影响相关项；白盒有效测试独立运行复用，无改动不推进快照。批量执行沿用同一个 QA TaskKey，干净副本、有序依赖、真实逐条输出和前置构造边界按主需求 §8.4.2 验收，不用准备复用替代本轮作为验收目标的安装或宿主调用。
- checkpoint 通过现有 task progress/result 事件进入 submit，保存完整 PASS/FAIL、未完成项、中断原因和复用/失效理由，绑定候选/需求/case/shared-step/tool/environment。验证不完整不 PASS、续跑保留 FAIL、变化精确失效、缺失/重复/未知条目拒绝；checkpoint 不增 attempt，运行错误重派沿原 TaskKey 有限 retry，不另建 QA 状态机。
- QA/开发/门的外部派发统一经过 3.5b engine guard。阶段 4 的 engine `submit` 必须在同一
  写事务内完成“接纳结果/回执 → source resolve/backfill → usage 更新 → guard 判断 → refill”；
  lifecycle stop 尚未到达时保持 `COST_PENDING` 并返回 Wait，source 明确不可用时记
  `UNAVAILABLE` 后继续使用 request/attempt guard。达到额度时只返回 typed Ask，不由主代理
  自行决定是否继续。owner source bridge、engine `State`/envelope 的 cost/guard 投影和
  `show/status/next` 展示也在阶段 4 一并接入，不从 legacy `validate` 旁路补账；owner
  终态差值只作报告，不参与 in-flight guard。额度投影同时绑定 basis digest；相同输入重算
  不一致、账目无法对齐或将要超发时以 `BLOCKED_BUG` 零派发。
- source bridge 保留已交付 Codex transcriptPath、原生 response ID 去重、task timing（含工具等待、不含 turn 间空闲）；checkpoint 不提前固定 dispatch cost，续跑终结后从完整来源结算。以现有 cost/source bridge 回归验收，缺来源仍 unavailable，不扩展其他 provider，不要求重新执行 3.5c 的前后模型 benchmark。
- 本阶段接入项目/安装级 token cap 配置、生效值/来源持久化与运行时止损；次数仍有余额但
  已结算派发用量到顶时也停止自动签发/refill/恢复重试。重启保留用量/限制，配置错误不能
  静默禁用 cap。全部用户可配置项（含既有设置）均方便用户手改和 AI 修改，配置位置、字段、
  默认值、覆盖关系、生效时点及有效来源明确，使用同一校验，符合主需求 §6.5。
- 从公开路径验证跨产品/技术复审的顺序与新零上下文代理，以及一个 QA/门 FAIL 或
  RUNTIME_ERROR 后独立兄弟任务继续、容量和 source/guard 允许时自动补位；不能用单一 QA
  任务内的用例隔离代替工作流任务间隔离。
- 实现任意非终态需求变化、finding/remedy 处置、adopt-external、reset、abort、中断和资源 cleanup；typed contract 至少覆盖 `REQUEST_REQUIREMENT_CHANGE`、`REVIEW_FINDING_FIX`、`VALIDATION_DETAIL_DISPOSITION`、`QA_ARTIFACT_REPAIR`，并固定 `ReviewScopeMode` 的 `FULL`/`AFFECTED` barrier 和“新鲜复审”要求。
- Controller 唯一计算 QA 重跑与 FULL/AFFECTED：消费项目/路线规则、修复
  类型和候选身份；guarantee ACTIVE 本身不产生额外 FULL。纯门审 P2 无其他 FULL 义务时只签发 AFFECTED/合法继承。主代理理由、
  final 标记、host override 和普通“继续”不产生 FULL、取消或 Seal 跳过授权。实际用户例外
  经 typed Ask 绑定当前 run/revision/candidate/mode/scope，一次有效，不改默认规则。
- 使用现有 pendingActions、CAS/intent 和 guard，为首个真实宿主接入执行前认领与校验，
  绑定 actionID/TaskKey/Attempt、provider、payload、scope 和精确 case set；原生派发、模型
  命令和 QA 操作的越界/无签发/重放/过期请求均须零调用。合法任务内请求沿用任务绑定，
  不新增逐请求 TaskKey、第二账本或 daemon；未接入宿主不得宣称已具备该保证。
- 验证 RUNTIME_ERROR 在合法有限重试/用户追加 Attempt 后能完整登记结果，原错误可追溯、
  权威 PASS/FAIL 不被覆盖；非法执行尝试不污染已有通过结果、不增加审查轮次、不要求
  Seal 跳过。该恢复路径与准入正反例一并纳入首个宿主 canary。
- 首宿主还须验收单个操作内部固定步骤的程序化顺序：Git 范围校验 → track → commit →
  snapshot 与已声明 QA 的准备 → 操作 → 采集。主代理/子代理的跳步、倒序、重复或前置失败
  后继续请求在底层执行前拒绝，调用数为零；中断只恢复未完成步骤。复用原子 handler 或
  StepSpec/intent，不以提示词、阶段顺序测试、事后拒绝或新增逐语句 TaskKey 代替。
- 验证需求变更时全部/部分保留、无显式保留、含义改变且 affected/unknown 的 PASS 仍可保留、在途任务不阻塞需求接纳、迟到结果不覆盖决定及连续修订；验证产品/技术审、QA 审查/执行、普通门的授权继承能推进至 Seal，原证据与授权可追溯。本阶段覆盖 Git 非分片，阶段 5 扩展 child/merge。
- 完成 Git provider 的 status/diff/track/commit/snapshot/squash、whitebox workspace、candidate promotion 和 cleanup。
- 在一个真实宿主上跑 Git 非分片 full canary，覆盖至少一次 FAIL -> repair -> 新候选 -> 重验 -> Seal。
- 按用户 2026-09-08 决定，在阶段 4 删除旧 workflow 公开命令、别名、兼容转发、legacy writer/QA/轮次/授权、旧运行态 decoder/migration、legacy mode、authority handoff 和旧流程 cleanup/门禁后门。仍承诺业务语义内收 engine handler；共享维护与纯解析能力保留。检查源码、注册表与最终安装包无第二路径，旧命令统一按未知命令拒绝；历史 run 只作最小原始只读诊断，不迁移或续写。

阶段 4 发布顺序：隔离安装中的全部阶段 QA/canary PASS → 本阶段固定 driver Seal → 主线集成及必要重建/重验 → 既有安装事务全局切换 → 实际全局入口 smoke/准入核验及历史功能逐项回归 → 发布 receipt 与阶段 4 driver 冻结。安装使用已验证的精确 post-integration 包，runtime、hook/config、managed-rule、pointer 与 registry 一起切换；失败按 §6 回退，不新增安装机制。Seal 前通过隔离目标验证发布合同，实际切换后的结果作为交付后发布核验记录，不倒写 Seal 前 QA。

历史功能回归按阶段 4 REQ-022/AC-079–081 执行：从阶段 0、1、2、3、3.5a/fix/b/c 原需求和交付结果建立逐项清单，记录当前入口、用例、结果和证据。Seal 前在隔离候选验证，全局切换后再通过真实全局入口复验；内部合同与破坏性安装测试使用绑定同一发布包的对应隔离环境并注明。已迁移功能验证 engine 的替代路径，明确替代/后续待交付项注明来源，不能把历史 Seal、授权跳过或本次未测写成新版 PASS。仍承诺提供的能力有回归阻塞或缺必需证据时不完成发布、不启用后续 driver，按既有安装失败合同回退并修复。

发布后状态：全局插件为阶段 4 版本，Codex/Git no-split regular 经 engine 运行；阶段 5–7 的正式 run 使用冻结的该版本。本阶段开发驱动退出日常驱动，原稳定安装仅按安装失败回退合同保留；旧 run 不迁移、不交给新 writer 续写。同步 SKILL/README/入口说明与阶段 4 实际能力，未支持的 engine 路径明确拒绝；不提前宣称 split、SVN/P4 或其余宿主完成。

### 阶段 5：Git 分片、child 与 merge 闭环

目标是在已经稳定的 Git 非分片能力上增加完整分片生命周期，不同时引入其他 VCS 变量。

范围：

- 实现精确 topology、child 创建、`sliceID -> childRunID` map、继承、逐 child route 和级联控制。
- master 冻结完整 REQ/AC 投影与 AC → 唯一主责任范围（child/master merge），在任何 child QA 设计前拒绝遗漏/重复/未知责任及 revision 不一致；child 只报告自身范围。receipt 按 scope + QA 类型 + case ID 保存批准用例及 AC 绑定，master 汇总去重；任一责任 child 无 QA 或 waived 时整体 not-guaranteed。保证启用时，合并候选须具备全部批准用例的有效结果；首次必要 FULL 与普通返修 AFFECTED 保留，本轮必需验证全部有效 PASS 后不追加最终 FULL，明确用户保留按主需求 §7.2.2。
- 将阶段 4 的用户验证保留决定扩展至 master/child/merge；由 master 统一接收并级联，验证任意已通过 child/merge 验证可被明确保留并满足对应 join/Seal 条件，未完成的实际集成仍须完成。
- 实现 `SLICE_READY`、包含最终白盒测试的 child identity、durable receipt、case/cost 汇总；
  dispatch 条目沿用 `<childRunID>/<dispatchID>` namespace 去重，child owner 以 `childRunID`
  一次性幂等汇总到 master cost projection，不重新估算 child 限额。主干 owner 与 child
  owner 仍分别保留在各自 run 的 receipt/sidecar；master 自身 `Owner` 不被 child 覆盖，且
  不引入通用 billing schema。
- 实现 Git child worktree、主线集成、冲突窄 agent 边界、合并 QA、合并门、AffectedChildSet 和主线 repair。
- 实现 child/master 的 reset、abort、需求变化、cleanup 和崩溃恢复。
- retained master 的 Seal 只在 active guarantee 下读取并物化 slice guarantee sidecar；显式
  waiver/not-guaranteed 不再要求该 sidecar。物化、摘要和清理失败时持久状态保持非
  `SEALED`，修正故障后可重试。
- 跑 Git 分片 master/child/merge full canary，同时回归 Git 非分片 full。

Seal 后状态：隔离候选在 Git 上同时支持非分片和分片完整流程；SVN、P4 和剩余宿主仍不宣称完成。

### 阶段 6：SVN/P4 与五宿主完成

目标是在 workflow 语义已经稳定后完成 provider 和 host 的机械适配面。

范围：

- 按同一 VCS adapter 契约实现 SVN 的 working copy、revision/property、integrate/commit、candidate identity、receipt 和 cleanup。
- 按同一 VCS adapter 契约实现 P4 的 client/workspace、changelist/filetype/view、integrate/submit、candidate identity、receipt 和 cleanup。
- 完成 Claude Code、Codex、Cursor、DeepSeek Harness、ZCode 的 provider identity、bridge、dispatch、lifecycle、resume/terminate 和 receipt 对账。
- 对五宿主逐项完成执行前准入矩阵，使用实际工具前 hook/执行器覆盖原生派发/恢复、工作流
  模型 CLI/API 和已声明 QA 操作，不能保留同类裸入口绕过。逐宿主运行最终方案 §12.1
  第 14 项的零调用反例和合法 AFFECTED、首次必要 FULL、一次用户例外正例；记录实际
  provider/入口/绑定/拒绝位置与底层调用数。入口能力不足必须明确 unsupported 并拒绝进入
  该执行模式，不能用仅登记时拒绝、提示词自律或 hook 已配置替代实机证据。
- 各 host 明确 transcript/usage 能力；无法核对的 owner 或 dispatch 记 `UNAVAILABLE`，仍受
  request/attempt guard，不为 host 单独引入预算算法。
- 每个 host canary 必须使用候选专属 host home/config、hook/managed-rule 路径和 state/resource roots，并记录 canonical-path disjoint proof；不能调用固定全局 binary、全局 hook 或稳定 host home。
- DSH canary 必须使用隔离 `DSH_HOME` 和候选 Cordis bridge，使 binary 解析为 required DeepSeek provider；project-local DSH 的 `ProviderDefault/UNAVAILABLE` 不能作为 lifecycle full canary 证据，provider/bridge mismatch 必须硬拒绝。
- SVN、P4 和不同宿主可在本阶段内部使用独立 worktree/working copy/client 并行开发，但必须在同一候选 join 后统一审查和 Seal。
- 跑 SVN/P4 的非分片与分片 full canary，并完成五宿主各自的 Git 非分片 full canary；每条 full canary 都覆盖产品/技术审、并行开发与黑盒设计、whitebox authoring/review、candidate freeze、view reuse 或受影响重跑、FAIL -> repair -> 新候选、promotion 中断恢复、receipt/result、无残留 cleanup 和 Seal。
- 每条证据绑定 provider/bridge、source identity、installed-target/package digest、snapshot、candidate/promotion/cleanup receipt；不能用 hook-only、lifecycle-only、source tree 或旧全局包冒充候选 full canary。
- 回归 Git 非分片、Git 分片和 fixed stable driver 正常入口。

Seal 后状态：隔离候选已经覆盖最终要求的全部 provider、split 形态和宿主，且阶段 4 已删除的 legacy/过渡 façade 不得重新引入；全局与正式开发 driver 继续使用已发布的阶段 4 包，不自动切到阶段 6 候选。

### 阶段 7：唯一权威切换与最终发布

目标是从功能完整候选收敛到没有第二权威路径的最终产品，在全部最终证据成立后把全局阶段 4 版本升级为最终完整版本。

范围：

- 复核阶段 4 已完成的旧逻辑删除没有回流：旧公开推进命令、别名、运行时兼容 decoder/migration、legacy mode、authority handoff、legacy QA、过渡 façade 和公开 cleanup 均不存在。保留当前维护能力与仅供 `diagnose` 使用的最小 raw/envelope parser、terminal summary fallback。
- 最终清理扫描必须确认 executable definition 中不存在 `MISSING_ENGINE_ADAPTER` 或 `BLOCKED_BUG` marker 伪装成可执行路径；`REQUEST_REQUIREMENT_CHANGE` 的两阶段 barrier、`REVIEW_FINDING_FIX` 的新鲜复审、`VALIDATION_DETAIL_DISPOSITION`、`QA_ARTIFACT_REPAIR` 和 `ReviewScopeMode FULL/AFFECTED` 的 freshness 证据都必须绑定最终 candidate。
- 最终安装态逐项核对执行前准入矩阵，全部受支持入口的零调用反例和合法正例绑定同一
  post-integration candidate/installed target；原生派发和命令入口无工作流旁路。复用既有
  canary 单元格承载验证，不因该要求新增审查角色、轮次或可自由扩大范围的 QA 入口。
- 在最终公共面真正可用后，同步切换 `SKILL.md`、README、references、prompts、catalog 和测试；不得保留指导用户调用已删除入口的文档。
- 同步清理成本口径：保留“精确或 unavailable、无估算、无第二账本”，并更新 owner 报告、
  engine source bridge/guard、额度 Ask/Wait、retry 算术和分片 owner 汇总的说明。
- 根据阶段 0 建立的 requirements-precedence/supersession 清单，更新或标记所有仍把 `prepare-*`、`record-*`、`qa-worktree`、公开 `cleanup` 等旧入口写成当前公共面的旧 OpenSpec/root 文档；历史文档保留可追溯性，但不能继续作为已确认的当前实现契约。
- 先将清理完成、已做前置验证但尚未 Seal 的候选集成到主线，以 promotion/integration receipt 关联集成前候选与 post-integration canonical identity；完成最终候选整理（含必要 squash）后，从最终 identity 构建不可变隔离安装副本。无论 digest 是否等价，均按下项完成全部最终验证；验证中候选变化时重新整理、绑定并重验，全部最终证据就绪前保持未 Seal，不产生中间 Seal。
- 在该 post-integration revision 的包摘要和 installed-target digest 上重新运行 Git/SVN/P4 × 非分片/分片六格和五宿主 Git 非分片 canary；允许按总需求定义复用一个 Git 非分片单元格，最低十条完整 canary。每条都必须覆盖产品/技术审、并行开发+黑盒设计、whitebox authoring/review、candidate freeze、view reuse/受影响重跑、FAIL -> repair -> 新候选、promotion 中断恢复、receipt/result、无残留 cleanup 和 Seal。
- 完成前置产品审、技术审、QA 和选定 gates，但在主线集成前不 Seal；final-release run 保持 ACTIVE，集成后由阶段 4 driver 经 availableActions/REQUEST → adopt-external Ask → typed submit receipt 绑定 post-integration canonical identity，重建候选，重新执行安装 smoke、cleanup 核对、全部最终 canary 和切换前 QA，并将这些结果绑定同一个 final-release run。无法安全 rebind 时新建 final-release run，前置结果全部降为非权威说明。
- 只有 post-integration candidate 的全部最终证据、全局 registry bootstrap/transaction receipt 和所有 UNKNOWN 事务处置记录成立后，才由冻结的阶段 4 stable driver 执行阶段 7 唯一的 final-release Seal；随后把全局阶段 4 包原子升级为最终版，覆盖 runtime、hook/config、managed-rule、release/current pointer，不等待活动 run 终结。切换成功且逐 host 的 binary、pointer、hook/config、managed-rule、provider/bridge post-switch smoke 全部通过后才退役阶段 4 固定驱动包。

Seal 后状态：全局插件只保留 engine 唯一权威路径；所有最终证据绑定同一不可变候选；旧稳定包在切换确认后退役。

阶段 5–7 继承阶段 4 的 finding/problem ID、归并、唯一返修、门审 P3 advisory、QA 冻结/THAW
和 requirement impact 用户授权合同；adapter 不重解释语义、不增加第二 writer。

## 4. 阶段依赖与并行边界

正式阶段的主依赖为：

```text
阶段 0 -> 阶段 1 -> 阶段 2 -> 阶段 3 -> 阶段 4 -> 阶段 5 -> 阶段 6 -> 阶段 7
```

阶段 3 完成后，须完成 3.5a、3.5b，再完成 3.5c QA 提效，才能开始阶段 4；它们不改变
上述八个正式阶段的编号。checkpoint 名称本身不新增 formal run 或 Seal；若合并受理为
正式任务，按该任务的流程留痕。3.5a、3.5b 是否并行由各自拆分决定；3.5c 则在两者的
覆盖、冻结、计量与重试合同之上优化 QA，阶段 4 继承其交付语义。

不并行推进会改变 schema、definition 或公共写入口的正式阶段。允许的并行只发生在同一正式阶段内部，并由该阶段统一 join、验证和 Seal，例如：

- 阶段 1 内不同 compiler/property-test work package；
- 阶段 2 内 fake host、fake VCS 和故障注入；
- 阶段 6 内 SVN、P4 和五宿主 adapter；
- 阶段 7 内不同 canary 单元格。

任何内部并行分支都不能单独成为下一阶段基线；只有该正式阶段的共同候选 Seal 后才能推进。

### 阶段内分批推进

各阶段开发按总需求 §8.5“开发分批与开发后统一门审”执行，沿用 ADR-002 的批次判据：批次计划随拆分决定/路线确认一并产生并留痕（受理不冻结）；批内执行项目声明的轻量检查、批边界执行声明的回归命令。按 2026-09-07 本次修订，插件固定流程直接实现，宿主接入随对应功能实现和验证；不自动安排独立首批探路。只有列明具体未决接口风险、受影响工作、最小实验及通过判据后，才评估是否独立 spike；实验先于依赖其结论的工作，不阻止无依赖工作，结论/证据留痕、实验代码不进交付。历史已完成的 compiler 等 spike 记录保持原状。阶段 4 此前“两批：探路→实现”的建议须重新评估，不自动沿用，也不因驱动缺少批次入口就改为单批或削减产品多批次能力。

2026-09-07 用户取消可选组间增量门审：自阶段 4 起代码/质量门统一在本 run 全部开发完成且最终候选 review-ready 后运行，批间不派门、不记录正式门结果或 repair wave；开发前审查与并行黑盒用例审查保持原职责。

引擎承载义务按本文增量 Seal 阶段分配：批次计划登记（作为 TaskKey 的分组与依赖信息，完成状态从成员派生，无新生命周期）进阶段 1 任务模型；阶段 1 的组间 DevelopmentSnapshot 只读投影与阶段 3 最小纵向闭环保留历史交付事实。阶段 4 不再承接原计划的组间增量门审，按最新决定实现开发全部完成后的统一门审；审查时点、范围和结果接纳保持独立代码边界，便于局部迭代，不预建另一套流程或改造底层协议。Git split 的 master/child 生命周期归阶段 5，SVN/P4 与其分片适配归阶段 6；具体拆分机制见主需求 §8.1。既有 legacy/历史 run 不因本条追溯变化。

本次重构的补充约束：

1. 各阶段收口段固定且独立，含隔离安装构建、installed binary 回归与 namespace disjoint proof（三环境模型要求），不得并入开发批次。
2. 预期生产逻辑多模块扩散或独立验收单元 ≥ 2 的阶段，在拆分决定/路线确认时仍须评估开发分批与 split 并留痕；不得据此恢复组间门审。该规则替代 ADR-002 中评估组间增量审查档的要求，不改变本阶段 Git 非分片的产品范围。
3. 阶段候选的测试与实现同批推进，不留到收口段补写。

## 5. 每阶段退出条件与最终退出条件

每个阶段都要完整走该阶段自己的 formal-gates 流程并 Seal，但 Seal 的证据只要求本阶段已承诺的能力，不把尚未实现的最终公共面倒灌到早期阶段。以下 1–6、8–10 是所有阶段的共同条件；7、11、12 按“本阶段实际 surface”验收，最终公共面附加条件只在阶段 7 生效：

1. 阶段需求、技术方案、范围和路线已按该阶段 formal-gates run 单独确认。
2. QA FAIL、有效 P0/P1 与门审 P2 obligation 按规则闭环；门审 P3 只 Seal advisory。3.5c 实施后的 QA Review 可选 P2/P3 在冻结前按该计划登记处置，不强制改用例；此前记录保留原规则。不存在未处置的阻塞结果。
3. `go test ./...`、package validation 和 portable canary 通过。
4. 固定稳定插件的文档化正常入口 smoke 通过，且其安装内容未指向阶段开发 worktree；候选 `Lstat`/realpath、source/installed digest 和 host-path disjoint proof 通过。
5. 本阶段新增能力在不可变候选安装中由实际 installed binary 执行，并在独立测试项目、host config、state/resource namespace 中完成端到端验证。
6. 本阶段承诺支持的既有 engine 路径完成回归；未承诺支持的路径明确拒绝或继续由绑定的 legacy runtime 处理，不出现半迁移。
7. 对本阶段实际交付的 executable/diagnostic surface 绑定 state writer（若有）、schema/definition version、definition digest、候选提交、集成后 canonical identity、包摘要和测试证据；相关的 `UNSUPPORTED_RUN_VERSION`、raw `diagnose` 和 terminal summary fallback 负向测试通过。未实现的最终 surface 必须显式拒绝或保持绑定的 legacy/Shadow 语义，不得用缺省实现冒充完成。阶段 1 的 `MISSING_ENGINE_ADAPTER` 只能留在 diagnostic-only fixture，不能编译或签发 Ready/HostAction；“最终 executable definition 无 marker”、全部 typed request/change/fix 和 `FULL/AFFECTED` freshness 证据属于阶段 7 的附加条件。
8. 通过覆盖本阶段所触及 scope 的 transaction/fencing 和 registry bootstrap receipt 确认没有未完成 intent、未对账 UNKNOWN receipt、未登记 target/root 或登记资源残留。活动 run 不参与安装准入；阶段 4 交付后完成自身全局发布的 registry/transaction 对账，阶段 7 再完成最终版本对账，不能把阶段 4 发布当作最终全部能力交付。
9. 阶段 worktree 的变更已提交，正式审查和 QA 基于该候选完成，阶段 Seal 成功；阶段 7 的前置审查只能是 provisional，最终 Seal 必须在 post-integration final-release run 中完成。
10. 阶段 0–6 的 sealed candidate identity 已通过 integration/promotion receipt 绑定到 post-integration canonical identity；阶段 7 则要求 final-release Seal、全部最终 canary 和 post-integration identity 直接绑定，不能用先 Seal 的旧 run 代替。
11. 当前阶段实际暴露的 workflow/维护入口 route matrix 已逐项绑定到本阶段允许的 legacy、Shadow、隔离 engine 或明确 unsupported surface；直接写 state、绕过 `submit`、绕过 freshness 或公开 cleanup 的 negative tests 通过。本阶段实际 writer 的 epoch/fencing token 或 legacy launcher invocation lease 有 stale-token/lease rejection 和并发窗口测试；阶段 7 才要求当前与最终公共面的完整矩阵及全部 writer 路径。
12. 本阶段涉及的 runtime、hook/config、managed-rule、pointer、provider/bridge 和 recovery journal 故障窗口有可恢复证据；阶段 0 验证安装/registry bootstrap，阶段 1–6 验证候选 namespace；阶段 4 另在交付后执行全局切换、该版本实际入口检查与阶段 0–3.5 历史功能逐项回归，阶段 7 执行最终版本升级和五宿主 post-switch smoke。实际全局检查记入各次发布 receipt，不要求在 Seal 前覆盖真实全局安装。

阶段 7 在上述共同条件之外还必须同时满足：最终 executable definition 不存在 `MISSING_ENGINE_ADAPTER/BLOCKED_BUG` marker；`REQUEST_REQUIREMENT_CHANGE` 两阶段 barrier、`REVIEW_FINDING_FIX` 新鲜复审、`VALIDATION_DETAIL_DISPOSITION`、`QA_ARTIFACT_REPAIR` 和 `ReviewScopeMode FULL/AFFECTED` 的 freshness 证据齐全；当前/最终完整 route matrix 和所有 engine writer/legacy launcher fencing 路径通过；最终十条 canary、registry inventory、原子全局切换、逐 host post-switch smoke 和旧 stable 退役证据全部绑定同一 post-integration candidate。

执行前准入属于阶段 4 首宿主（含发布后的实际全局入口）、阶段 6 五宿主和阶段 7 最终安装态的退出条件：已编码流程
规则必须在实际执行前强制，越界调用为零，合法操作无需逐步确认；未覆盖入口不得标记
支持。该结论不等于保证模型判断或实现语义永远正确，仍保留既有 Operator 核实职责。

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
4. 每次全局发布前，候选先在隔离目录验证；阶段 4 与阶段 7 分别在各自 Seal/集成/必需验证完成后安装，不通过覆盖现有安装来“试一下”。
5. 阶段 4 或阶段 7 原子换位失败时，按 `SWITCHING -> ROLLING_BACK -> OPEN` 的单调 generation 协议恢复该次发布前的稳定 runtime、hook/config、managed-rule、release/current pointer；绝不把 registry epoch/generation 写回旧值。只有 rollback smoke 和新的 stable generation 成功后才允许再次 start。
6. 安装或回退均不等待活动 run；旧 generation 的 run 不由新或回退后的 runtime 迁移、兼容或继续写入。

## 7. 文档关系与旧主需求处理

3.5 系列及 Seal 后补充的来源、生效范围和替代关系见 `references/requirements-precedence.md`
2026-09-07 条目。阶段当前状态以 stage-records 的同日索引和精确结果文件为准；checkpoint
Seal 不意味着 regular engine、运行时 guard、执行前准入或全局安装已经完成。

`openspec/changes/orchestration-pipeline-engine/master-requirements.md` 保留为总需求、非目标、最终公共面和最终验收口径的权威来源，不因分阶段实施而删除。`refactor-plan/final-implementation-draft.md` 保留为目标架构说明。

阶段 0 另建立 `requirements-precedence/supersession` 清单，枚举所有 `openspec/changes/**/master-requirements.md`、根目录需求和旧 plan 文档，逐项标明 `current-authority`、`orthogonal`、`superseded` 或 `historical` 及其替代文档。只有编排流水线重构以本文主需求为 current authority；其他正交需求仍按自身范围生效，冲突的旧入口文档在阶段 4 发布前标记 superseded，不靠删除文件消除歧义。

主需求和方案稿中的“阶段 0–6”是总体实现工作包编号，不是要求实施时只能有七个 Seal，也不与本文的增量 Seal 编号同义。本文把它们细化为八个可独立 Seal、每个 Seal 后仍可继续开发的切片，映射固定如下：

| 总体方案工作包 | 增量 Seal 阶段 | 说明 |
| --- | --- | --- |
| 0 冻结契约 | 0 分发安全与基线冻结 | 同时冻结 contract fixtures、稳定驱动和 registry bootstrap；不改变公开 workflow 语义 |
| 1 纯决策内核 | 1 纯决策内核与 Shadow | 只读，不产生第二 workflow writer |
| 2 可靠写入与动作协议 | 2 持久协议与恢复内核 | 仅隔离 namespace/test harness 写入 |
| 3 完整流程迁移 | 3 最小纵向闭环 + 4 Git regular | 先证明最小 engine 路由，再扩展到 Git 非分片完整流程；阶段 4 交付后发布全局并成为后续 driver |
| 4 split 与三 VCS | 5 Git split/child/merge + 6 SVN/P4/五宿主 | 先固定 Git split，再接入其他 VCS/host |
| 5 删除旧逻辑 + 6 最终 canary/安装 | 4 完成旧逻辑删除；7 核对无回流并最终发布 | 按“前置验证（不 Seal）→ 主线集成 → 重建最终候选 → 最终 canary → 阶段 4 driver 执行 final-release Seal → 全局从阶段 4 升级最终版”完成；阶段 6 增量 Seal 不等同于最终交付 |

任何引用总体方案阶段号的需求、任务或验收记录都必须同时注明本文增量 Seal 阶段号；只有增量阶段 7 Seal 后，才可宣称主需求的总体阶段 5/6 和本次最终交付完成。

每个阶段另建自己的 requirements/design/tasks 文档，只登记该阶段新增或迁移的能力、明确依赖的上一 sealed baseline、阶段内不做的内容和本阶段退出条件。阶段文档不能修改或缩小总需求；若实际实施需要改变已确认的总需求或重大技术选择，必须先走需求变化流程，而不是在子阶段静默改写。

3.5c 候选的公开路径为 `qa-design/qa-review` 的 shared-step 引用、`qa-suggestion` 的理由
处置、`qa-execution --checkpoint` 的绑定续跑，以及最后必需验证前的 `prepare-candidate`。
操作示例见 `references/formal-flow.md`；验证记录见 `stage-3-5c-validation.md`。
上述 3.5c 候选能力不修改其固定 driver、正式 run、既有 Seal 或全局安装；阶段 4 后续发布按本文更新后的时点执行。
