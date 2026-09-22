# P2 修复与注释清理需求

> 本文件登记一次正式流程的已确认需求：修复 P2 待办清单中核查复现的 50 项、新增
> 受理流程规则、清理 Go 源码注释元数据。整理日期：2026-08-10。

---

## 背景

`P2-BACKLOG.md`（gitignored、不跟踪）记录了 formal-gates 项目全量审查发现的 P2 项。
2026-08-09 逐条核查后确认其中 50 项仍存在（另有 4 项已不复现，不在本次范围；P0/P1
已另行走正式流程处理）。本次以正式流程一次性修复全部 50 项，并按用户要求新增一条受
理流程规则、清理 Go 源码注释中的内部元数据。

---

## 需求 1：修复 50 项 P2（按领域拆片）

按领域分为 7 片，每片独立开发、独立验证。各片修复意图如下（每条对应 P2-BACKLOG.md
中的记录项，括号内为该片项数）。

### 片 1：类别三 — CLI 行为（19 项）

1. `workflow show`/`workflow abort` 不接受 `--package-root`，参数面与其余子命令不一致
   （cleanup 接受但静默忽略）。修复：show/abort 补上 `--package-root`；cleanup 不再静默
   忽略。
2. 托管 agent 环境无 hooks 时 `record-action` 被生命周期校验拒绝（`missing matching
   start and stop event`），提示不友好。修复：给出可行动的友好提示（说明 review 记录依
   赖 capture hooks 或非托管环境）。
3. `settle-findings` 允许同一 finding 同时 confirm+dismiss，不查重，产生自相矛盾的已拍
   板记录。修复：重迭时拒绝。
4. `show` 对已 abort 的 run 报裸文件错误（`open .../state.json: no such file`）。修复：
   友好提示该 run 已终止/不存在。
5. `workflow`（无子命令）紧跟 flags 报误导性错误（把 `--root` 当子命令名）。修复：提示
   子命令必填。
6. `workflow --help` 打印顶层 usage 而非 workflow 子命令 usage。修复：粒度对齐。
7. `authorize-repair --qa-scope blackbox=`（空决策）被 flag 解析拒绝；CARRY_FORWARD 自动
   沿用实际只在"只传 `--qa-cases` 不传 `--qa-scope`"时触发，文档未说明。修复：空决策给
   明确错误；文档补 CARRY_FORWARD 触发条件。
8. 缺 run-id 报裸 `open ...state.json.lock: no such file`。修复：友好提示缺 run-id。
9. `qa-execution-scope --mode purple` 报误导消息而非"非法 mode"。修复：非法 mode 明确报错。
10. `gate run <ids...>` 位置参数在 flags 前被 Go flag 当门 id，报误导性错误。修复：提示
    flag 前置。
11. `record-action` status 大小写不敏感（`pass` 被接受），宽容处理仅记录。修复：严格大小
    写校验（或明确记录宽容并文档化）。
12. `hook decide --provider` 不校验：非 `codex` 值静默按默认处理。修复：非法值报错。
13. `canary portable --format` 不校验：任意字符串静默回落为 text。修复：非法值报错。
14. `canary codex-hook-probe` 成功时零输出，空 stdin 写 0 字节载荷文件。修复：成功时输
    出说明（载荷文件路径/字节数）。
15. `install`/`uninstall` 无参时 `--source` 默认 `.`（cwd），报"source 不完整"而非"source
    必填"。修复：source 必填时明确报错。
16. 二层 `--help` 风格不一致：`gate --help`/`hook --help` 只打印顶层清单，`gate run
    --help` 打印 Go flag 默认 usage。修复：统一粒度。
17. `gate run --vcs svn` 在 git 仓库透传 svn 原始 unicode 转义 stderr。修复：错误消息转
    义可读（或明确提示 vcs 与仓库不匹配）。
18. 边界未定义项：`hook decide` 空 stdin 报 `unexpected end of JSON input`；`--version`
    不存在（报 unknown command）。修复：空输入给友好错误；`--version` 给出可用提示或版本
    输出。
19. 不受支持平台备注：`bin/formal-gates.exe` 在 macOS 上 `exec format error`（rc=126）。
    修复：文档注明 .exe 为 Windows 资产，不试图在 macOS 运行。

### 片 2：类别一 — 架构（7 项）

1. 结果契约 .md 与 Go 双源维护（`internal/validate/runner.go` 硬编码结果契约文本，同时
   `prompts/actions/*.md` 描述同一契约）。修复：契约语义收敛到单一事实源。
2. `workflow.go` 单文件 4464 行 / 147 函数职责过载。修复：按领域拆文件（QA、Carry/Seal、
   transition、prompt 组装）。
3. 8 个 `Record*` 函数共享校验 prologue 未提取（`requireCurrentDefinitions` 调 18 次、
   `requireTransition` 15 次、`requireNoPendingInheritance` 13 次、`completeDispatch` 13
   次、`backfillDispatchCost` 8 次）。修复：抽统一包装。
4. 6 个零引用死函数：`validate.go` 的 `resolvePath`/`relativePath`/`firstNonEmpty`、
   `workflow.go` 的 `sortedGateIDs`、`canary.go` 的 `PortableCanaryJSON`、
   `codex_hook_canary.go` 的 `CodexHookCanaryJSON`。修复：删除。
5. 跨包小工具重复：`cleanRoot`（validate.go 与 lifecycle/journal.go 各一份）、
   `scalarString`（validate.go 与 lifecycle/provider.go 各一份）、`resolvePath` 与
   `resolveFromRoot` 同包逐字重复。修复：去重为单一实现。
6. 长函数：`runWorkflow`（cli.go，304 行）、`requireTransition`（workflow.go:3435，215
   行）。修复：按子命令拆 runner + 表驱动。
7. flag 类型族重复：11 个 start+field 类型（`caseStart`/`caseField`、
   `qaReviewStart`/`qaReviewField` 等）同一模板，`assignedGroup` 出现 15 次。修复：收敛
   模板。

### 片 3：类别二 — 提示词（5 项）

1. 6 个 action 缺项目边界重申：carry、qa-execution、development-worker、
   requirements-clarification 不含「对抗性→P2、只审正常使用」边界（product-review /
   start-readiness 已经共享契约注入）。修复：4 个非审查动作补边界重申。
2. `CLAUDE.md` 与 `AGENTS.md` 双份漂移：边界规则两份已漂移。修复：两份各指
   `prompts/reviewer-base.md` 为唯一本体，去除内联副本。
3. `example-run.md` 是第三个维护面：§10-§14 仍内联重述 SKILL/formal-flow 规则。修复：
   walkthrough 只写"做什么/读什么"，规则用引用。
4. `SKILL.md` 第 4/6/7/9 步内嵌命令语法，与「步骤级执行机制统一持有在 formal-flow」自声
   明冲突。修复：缩为「执行本步前必读 formal-flow「XX」」。
5. cost-metering 无文档化入口：`internal/cost/` 与 `RunState.Cost` 已实现、seal 摘要携带
   token 投影，但 SKILL/formal-flow/README 均未提及。修复：补文档化入口。

### 片 4：类别四 — 测试工程（5 项）

1. 全局可变状态换桩 24 处（`workflowLifecycle` 24 处、`parallelCooldown` 8 处、
   `lifecycle.executablePath` 3 处）。修复：收敛为 `stubLifecycle(t,…)` helper 并钉死禁
   止并行。
2. 文档耦合测试脆弱：`workflow_test.go` `TestWordingCovers…`、`standalone_gate_test.go`
   `TestContaminationCheck…` 读 `../../` 断言中文文案，任何文档措辞改动即红。修复：移出
   单元套件。
3. 单跑 canary 集成测试在单元套件：`canary_test.go` `TestPortableCanaryPassesAgainstRepoRoot`
   直接对真实仓库根跑完整 canary。修复：与单元套件隔离。
4. 弱断言：`codex_hook_canary_test.go` 丢弃两个 error 只断 ID 唯一；
   `lifecycle_test.go` 断言静态 map 返回自身字面量，近乎恒真。修复：加固断言。
5. 小覆盖缺口：`workflow.RouteCandidates` 及 CLI `route-candidates` 无直接测试（仅有
   `PackageRouteCandidates` 覆盖）。修复：补 run-state 与 CLI 层直接测试。

### 片 5：类别五 — openspec / 文档档案（5 项）

1. `lifecycle-hooks/spec.md` 的 RQ-005「Codex SHALL be treated as UNAVAILABLE」已被 deadlock
   变更推翻（`codexAdapter.required` 翻转为 true）未标注。修复：加 superseded 注记。
2. `runtime-review-guards/specs/runtime-review-guards/spec.md` 的 STATIC/LIVE 机制已被
   blackbox/whitebox 模式推翻未标注。修复：加注记保留未过时部分。
3. `tasks.md` 勾选状态与交付脱节（cost-metering、readme-optimization 等已交付但未勾选）。
   修复：同步勾选。
4. phase4/phase5 的 `specs/` 子目录形态误导：不是活的 spec 集合，只是历史记录。修复：
   加形态说明。
5. `qa-execution-rerun-scope.json` ledger 未发布：该 change 已交付但正式 run ledger 未随
   惯例提交。修复：发布正式 run ledger。

### 片 6：类别六 — 安装脚本 / CI / 散文件（6 项）

1. `formal-gates.manifest.json` 声明 `examples/skill-behavior-prompts.json`，文件已删。
   修复：清 stale。
2. `references/local-validation.md` 要求执行已删的 `behavior evaluate` 并引用两个已删的
   `examples/*.json`。修复：清 stale。
3. CI 缺 vet/race：`portable-validation.yml` 仅 `go test ./...`。修复：补 vet/race。
4. Windows 符号链接前提未文档化：`install.ps1` 的 SymbolicLink 需管理员/开发者模式。修
   复：补文档。
5. `install.command` 装饰输出指向被删除的临时路径（EXIT trap 已删）。修复：删/改输出。
6. CHANGELOG Unreleased 用 RQ-012/013/014 内部编号。修复：去编号只留规则描述。

### 片 7：类别七 — run ledger 数据（3 项）

1. `phase4-seal-20260723-2208.json` 两条 FAIL finding 缺 `severity`（run 本身 ABORTED，
   无矛盾）。修复：补 severity。
2. cost schema 两代并存：旧代 `totalTokens` vs 新代 `totalInputTokens`。修复：加注记说明
   两代并存、各 run 自洽。
3. 两个 canary 文件混在 `.gates/results/`（schema 与 run ledger 完全不同）。修复：移出
   results 目录。

---

## 需求 2：新增受理流程规则

**规则内容**：主代理高置信度判断当前请求应走轻量、或根本不涉及项目内容修改时，跳过整个
受理流程（含需求澄清、确认与路线提问），按常规方式直接处理；拿不准、或用户明确要求走正
式流程时，仍走完整受理流程。新规则**本次优先**。

**落点**：
1. 仓库 `SKILL.md`「开发受理流程」段；
2. 同步更新全局已安装副本 `~/.claude/skills/formal-gates/SKILL.md`；
3. **协调全局 `~/.claude/CLAUDE.md` 的受理强制条款**——其「当用户请求创建/编辑/移动/删
   除任何项目内容时，必须先执行 formal-gates 受理流程…确认前不得选择路线、写入文件或
   派发代理」条款须与本规则一致，高置信轻量/无关时不强制先走受理流程（产品审 P2 确认项，
   本次优先）。

---

## 需求 3：清理 Go 源码注释元数据

**范围**：仅 `.go` 源码注释（`internal/`、`cmd/` 等），不含 `.md` 文档。

**方式**：去掉注释中所有内部元数据前缀——包括但不限于 RQ-0xx 这类需求编号引用（如
「RQ-011：」），以及其他内部编号/交叉引用标记——**保留解释文字本身**。注释的语义内容不
得丢失；`gofmt` 不改变。

「其他内部编号/交叉引用标记」的具体形态（产品审 P3 确认项，代理自行判断采纳）：
- 区间编号引用，如「RQ-012/013/014」「RQ-001~003」；
- 指向内部修复/审查记录的引用，如「R 修复清单 item 10」「item N」；
- 出现在注释里的提交 SHA 简写、run id、dispatch id 等内部标识符。

以上形态去掉编号/标识符，保留其后的解释文字；若某编号本身就是句子的必要语义（非元数据
标签），保留。

---

## 需求 4：`workflow start` 强制预判并行/拆分（用户追加，2026-08-10）

**问题**：拆分/并行意图须在 `workflow start` 时用 `--retained-overall` 声明，但 CLI 不在
启动时强制，主代理可在事后"忘记"声明，到拆分决定时才被拒绝（如本次 run 启动时漏带
`--retained-overall`），需重启整个 run 重过整体审查。

**要求**：`workflow start` 必须强制主代理在启动时预判并显式声明本次 run 是否并行/拆分，
声明后才可继续。具体形态已定：强制 `--split yes|no`，`yes` 时必须为保留总任务实例或切
片实例、`no` 时禁止后续记录 split：

- 强制显式声明拆分/并行意向，未声明拒绝启动并给出明确提示；
- 声明与后续 `workflow slicing` 的拆分决定互相校验，杜绝"启动时说 no、拆分时说 split"或
  反向的不一致被静默放过。

**落点**：`internal/cli/cli.go` 与 `internal/validate/workflow.go` 的 Start 路径（属片 1
CLI 行为领域）。

**开发指引（start-readiness P3 确认项）**：
1. `--split yes` 启动时须能区分「保留总任务实例」（`--retained-overall`）与「切片实例」
   （将用 `--master` 引用主实例），把该映射钉死在启动声明中，使「忘带 retained-overall」
   在启动时就暴露而非等到拆分决定；
2. 为本功能上线前启动的 run 定义「缺失启动声明」的行为，并为需求 4 定义明确验收标准，
   避免在途 run 行为歧义。

**缺失启动声明行为**（start-readiness P2 确认项）：本功能上线前启动的 run（无
`SplitDeclaration` 字段）按旧语义处理——不强制声明，后续 `workflow slicing` 按既有规则
执行（保留总任务实例可记 split；非保留实例记 split 需 `--master` 引用主实例）。

**需求 4 验收标准**（start-readiness P2 确认项）：
- `workflow start` 不带 `--split` 时拒绝启动，报错要求显式 `--split yes|no`；
- `--split yes` 不带 `--retained-overall` 也不带 `--master` 时拒绝启动；
- `--split no` 启动的 run 后续 `slicing --decision split` 被拒；`--split yes --master`
  启动的 run 后续 `slicing --decision no-split` 被拒；
- 上线前 run（无声明）按上述「缺失启动声明行为」处理。

---

## 需求 5：CLI 兜底重置机制（用户权限，2026-08-10 追加）

**问题**：正式 run 编排中，主代理（或任何人）可能把 run 流程状态弄坏（如快照无法推进、
整体审丢失、派发卡 OPEN），而现有 CLI 无「不动已开发内容、只重置流程状态」的出口——
只能手改状态或整个重跑，重跑还要重做整体审。

**要求**：新增一个**超级管理员指令**——**用户权限门控**的流程重置命令（`workflow reset`），
**优先级最高**（可覆盖正常流程顺序约束），满足：
1. **只重置流程状态**（run 的 `.gates` 流程数据），**绝不触碰已开发内容**（工作树、已提交
   代码、需求/方案文档）；
2. **必须用户显式授权**才能执行（如额外 `--user-approve` 交互确认或用户显式传入的授权
   令牌），主代理无权单独触发；
3. **超级管理员优先级（重置后整体审可重做）**：重置后 run 回到可重新登记的干净状态（需
   求可重登记、整体审可重做、开发快照保留），且整体审重做不受正常流程顺序约束——即使
   开发已完成（dev 状态与开发快照保留），允许重做 product-review / start-readiness；正常
   流程的「开发开始后不得重做整体审」顺序守卫须为重置后的恢复重做放行；已开发内容（工作
   树、已提交代码、需求/方案文档）原样保留——「只重置流程状态、不碰已开发内容」是唯一
   终态，两者同时成立；
4. 重置命令本身不删不改已开发内容，输出说明保留了什么、重置了什么。

**需求 5 验收标准**：
- `workflow reset` 必须经用户显式授权（如额外 `--user-approve` 确认或显式授权令牌）才
  执行，未授权拒绝执行、不落任何状态；
- 重置只影响 `.gates` 流程数据，工作树/已提交代码/需求与方案文档在重置前后无任何变化；
- 重置后 run 回到可重新登记的干净状态：需求可重新登记、整体审可重做（**即使开发已完成——
  正常流程的「开发开始后不得重做整体审」顺序守卫不得阻止重置后的恢复重做**）、已记录的
  开发快照保留；`workflow show` 可见该干净状态；
- 重置命令输出说明保留了什么、重置了什么。

## 需求 6：CLI 硬阻断可机械判定的误操作（2026-08-10 追加）

**问题**：本次编排暴露的几类误操作，能在 CLI 层机械判定、应硬阻断，不让代理/用户把
流程带进坏状态。

**原则**：每个可机械判定的误操作，都在**第一时间（最早可检测点）**硬阻断——不是拖到
后续阶段（如记录结果、快照、门审）才拦。能机械判定的才阻断，不引入猜测。

**要求**（逐条）：
1. **需求 revision 漂移第一时间阻断**：任何依赖需求修订的流程命令（prepare-action、
   prepare-gate、record-action、record-gate、snapshot、seal 等）在**其最早执行点**即校验
   需求文档当前内容 hash == run 注册的 `requirementRevision`；一旦需求文件被改动导致 hash
   漂移，**从改动后的第一个流程命令起就拒绝**，提示「需求文档已改动，先
   `workflow requirement --confirmed` 更新修订」。不等到 prepare 之后、记录时才发现。
2. **abort 误触第一时间阻断**：`workflow abort` 在命令调用即需额外用户级确认（如
   `--user-confirm` 或交互确认），防主代理「保险性 abort」误中止 run；确认前不执行、不
   落任何状态。
3. **start 参数第一时间校验**：`--base-snapshot`/`--current-snapshot` 在 start 调用即校验
   必须是仓库中存在的合法对象，且 base 是 current 的祖先或相等、current 是原生 HEAD 的
   祖先或相等（start 时 current 可取 HEAD，快照阶段 current 须严格落后于 HEAD）；非法
   参数拒绝启动，不把坏快照写进 run。
4. **派发提示词以 CLI 写出的规范文件为唯一权威源**：`prepare-action`/`prepare-gate` 生成
   提示词时，把完整提示词内容（含 dispatch id、需求 revision、base/current 快照等关键
   字段）写入本 run 规范提示词文件（如 `.gates/tmp/<run-id>/prompts/<dispatch-id>.md`），
   并把内容 hash 与文件路径作为派发状态的一部分记录。校验分两个时点：**写入时（第一时间）**
   立即校验内容 hash == 记录值（或对该文件写入钩子）；**派发时（兜底）**再次校验文件内容/
   关键字段与 prepare 记录一致，不一致即硬阻断、判定为不可用。派发只消费该规范文件——
   主代理只发指向该文件的薄启动消息、**不得手写/凭记忆拼写提示词内容**，子代理读文件执行。
5. **需求修订与结果记录的顺序由 CLI 强制（不靠主代理自觉）**：`workflow requirement`
   （`--confirmed`/`--meaning` 重绑）执行前，CLI 校验本 run 是否存在**未记录结果的在途审查
   派发**（已准备或已认领、尚未 `record-action`/`record-gate`；以既有派发状态判定，不依赖
   新增 returned 状态）；存在则拒绝重绑、提示先记录该结果，杜绝「先改需求导致在途审查结果
   无法记录、被迫重跑」的误操作路径。

**需求 6 验收标准**：
- 需求文档被改动后，第一个依赖需求修订的流程命令即被拒，提示先 `workflow requirement
  --confirmed` 更新修订；
- `workflow abort` 未经用户级确认（如 `--user-confirm`）不执行、不落任何状态；
- `workflow start` 传非法或关系不合法的 `--base-snapshot`/`--current-snapshot` 拒绝启动，
  不把坏快照写进 run；
- `prepare-action`/`prepare-gate` 写出的规范提示词文件写入即校验，内容/关键字段与 prepare
  记录不一致时派发被硬阻断；
- `workflow requirement` 重绑前，存在未记录结果的在途审查派发时被拒，提示先记录。

---

## 非目标

- 4 项核查为「未证明」的 P2 项不在本次范围（formal-flow 内部编号已清、测试样板已抽
  helper、CI runner 标签实际有效、qa-execution-rerun-scope 旧 ledger 文件已不在）。
- 已有 P0/P1 项已另行处理，不在本次范围。
- `.gates/results/` 下的历史 run 结论不改写，只补 schema/加注记/挪文件。

---

## 验收方式

- 每片开发后跑既有测试（`go test ./...`、`go vet ./...`、`go build ./...`），通过门审
  （复杂度/实现质量/合并）与黑盒 QA 验证。
- 正式 run 走 full 路线，三门上齐。
