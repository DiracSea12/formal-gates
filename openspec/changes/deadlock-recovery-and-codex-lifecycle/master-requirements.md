# 死锁恢复与 Codex 生命周期修复

Date: 2026-08-05
Status: confirmed
Route: (formal 流程内确认)

## 背景

用户反馈指出 formal-gates 流程存在两条硬死锁与若干可用性问题。已由主代理独立核实并
实测复现（真实二进制 + /tmp 临时仓库），并派零上下文独立子代理复查确认。本需求文档
登记已确认的修复范围；用户已确认完整需求与方案，并选择正式流程。

## Complete confirmed requirements

1. **pre-dev 采纳死锁（问题 1）。** 首个开发快照之前执行 `resume --adopt-external` 会
   无条件设置 `PreRepairSnapshot` 修复边界，而该边界在无任何可继承结果时无法解除，
   导致后续 `workflow snapshot` 被 `the current repair still requires verification`
   挡住，唯一出口是 `--meaning changed` 全量重来。
   **修复方案：** `AdoptExternalChange` 在**尚无开发快照时**（dev status 为
   `PENDING`/`PREPARED`，谓词不是 `!hasDevelopmentSnapshot`——后者会误伤
   `REPAIR_PREPARED`）不设置 `PreRepairSnapshot`、不重置审查面，只把 `CurrentSnapshot`
   重绑到新原生 HEAD 并记录采纳来源（Carry ADOPT provenance）。已有开发快照时行为不变
   （走既有 carry 继承）。
   **产品审已处置（P2-3）：** 重绑时既有 OPEN/CLAIMED 派发经 `rebindCurrentSnapshot`
   自然标 STALE（源快照失效），需求明确此行为，消除「采纳前已 prepare 的派发是否可
   用」歧义。
   **验收：** pre-dev 采纳后，`prepare development-worker` → commit → `workflow snapshot`
   能正常走通；采纳时 `PreRepairSnapshot` 为空。

2. **开发派发提交后认领（问题 2a）。** 开发/修复派发是 reviewer-required，认领要求
   原生 HEAD == 当前快照；worker 一旦提交 HEAD 即前进，主代理拿到身份时认领必失败，
   使「会提交的开发 worker」没有可行路径。
   **修复方案：** `ClaimDispatch` 对 `development-worker` 派发放宽原生身份检查：当前
   原生 HEAD 是派发源快照的后代（或相等）即允许认领（覆盖 worker 已提交的情形）；其余
   派发（审查、QA 等）检查不变。
   **产品审已处置（P2-2）：** 放宽后的检查不验证 HEAD 是否由 worker 产生——开发期间
   无关外部提交落地会被**静默吸收**进开发快照。用户已接受此行为，文档（formal-flow.md
   开发与快照段）注明。
   **验收：** worker 提交后再认领开发派发成功，随后 `workflow snapshot` 成功。

3. **Codex 生命周期生效（问题 2b）。** `codexAdapter.required = false` 使 Codex 上的
   生命周期验证恒为 Unavailable。已实测（codex-cli 0.145.0）：Codex 官方 hook 事件
   `SubagentStart`/`SubagentStop` 在真实子代理派发时触发，payload 携带一致的
   `agent_id`，与 `codexAdapter` 的身份提取字段匹配。
   **修复方案：** 把 `codexAdapter.required` 从 `false` 改为 `true`，使 Codex 生命周期
   验证生效。实现前先端到端验证：`spawn_agent` 返回的 `agent_id` 主代理能否取得用于
   认领、claim → lifecycle 配对验证是否走通。文档同步注明：Codex 派发独立代理走原生
   `spawn_agent`、用返回的 `agent_id` 认领。
   **产品审已处置（P1-1）：** 修正 `references/install-and-hooks.md:135-136` 原文
   「Claude Code 和 Cursor 要求 start 与 stop 事件配对。Codex 报告 `UNAVAILABLE`，
   因此既有的派发与身份检查仍然是权威依据。」——`required` 改 `true` 后 Codex 不再
   报告 UNAVAILABLE，改为「Claude Code、Cursor 和 Codex 都要求 start 与 stop 事件
   配对。」
   **产品审已处置（P2-1）：** Codex 生命周期事件不配对时采用**硬阻断**（与 Claude
   一致），不定义软回退；按文档化 `spawn_agent` 派发 + `agent_id` 认领可避免误伤。
   **产品审已处置（P2-5）：** 翻转 `required` 以 claim → lifecycle 配对端到端验证通过
   为前提；**若验证失败，保持 `required=false` 并文档化 Codex lifecycle 不支持**，不
   翻转——避免 Codex 从宽松（恒 Unavailable）退化为硬阻断（恒 Rejected）。
   **技术审已处置（P1）：** 翻转 `required` 必须同时处理本地/测试/canary 上下文——未安
   装二进制（非 `~/.claude/skills/...`、`~/.cursor/...`、`~/.codex/skills/...` 安装路径）
   解析为独立的宽松 provider（`required=false`），保持 `go test`、`canary portable` 与
   workflow 测试套件在无生命周期事件时仍走 Unavailable；仅真实安装的 Codex 二进制
   `required=true`。实现时把「默认/未安装 provider」与「已安装 Codex provider」分开，
   并更新 `TestLifecycleCodexIsUnavailable` 与 canary 使之适配。
   **验收：** Codex 上生命周期验证不再恒为 Unavailable；按文档化 `spawn_agent` 派发 +
   `agent_id` 认领时验证通过；未安装/测试/canary 上下文保持宽松（测试套件与
   `canary portable` 不因翻转而失败）。

4. **`--confirmed` 信号文档（问题 4）。** 开发开始后做 meaning-preserved 需求重绑，
   CLI 要求用户确认信号（`--confirmed`），但 `references/formal-flow.md` 的
   `--meaning preserved` 命令未说明。
   **修复方案：** formal-flow.md 注明开发开始后 meaning-preserved 重绑必须同时传
   `--confirmed`。
   **验收：** 文档涵盖该确认信号。

5. **重复 prepare 静默 STALE（问题 6）。** 对同一 action/gate 重复 prepare 会生成
   attempt+1 新派发，旧 OPEN/CLAIMED 派发静默标 STALE，无提示。
   **修复方案：** formal-flow.md 注明重复 prepare 同一 action/gate 会生成新派发、旧
   OPEN/CLAIMED 派发标 STALE，以当前返回的派发 ID 为准。
   **验收：** 文档涵盖该行为。

6. **产品审/技术审处置协议说清楚（本次流程暴露）。** 处置/重审机制是编排层（主代理 +
   用户）的事，reviewer 子代理不需要知道。原来 `prompts/actions/product-review.md` 与
   `prompts/actions/start-readiness.md` 提示词、`SKILL.md` 第 4 步与
   `references/formal-flow.md` 的处置协议把「用户接受」与「用户认同问题」含混，且把编排
   逻辑写进了子代理提示词。
   **修复方案：**
   - 处置/重审机制**唯一持有处**在 `references/formal-flow.md`（产品审段与技术审段的执行
     参考，按需加载）：**用户接受**（认为不是问题或不需要修改）→ 该发现项作废、不阻塞、
     不改需求；**用户认同问题**（认为需要修订的真问题）→ 按指示修订需求/方案。是否重新
     审：没审出问题 → 直接通过；用户认同的问题里不存在 P0/P1 → 修订后不再重新审，直接
     进入下一步；用户认同的问题里存在 P0/P1 → 修订后重新审。
   - `SKILL.md` 第 4 步按渐进式披露原则留简短指引（处置与重审机制见 formal-flow.md），
     不重复机制内容。
   - `prompts/actions/product-review.md` 与 `prompts/actions/start-readiness.md`
     **删除**处置/重审逻辑，只留 reviewer 需要的「分级 P0/P1/P2 + 候选输入、不产生终态
     FAIL」；**必填拆分建议要求与已拍板（settled）规则保留在提示词**——拆分建议要求只
     存在于提示词、不由 CLI 注入，删除会破坏正式 run 的必填留痕。
   **验收：** 处置/重审机制只存在于 references/formal-flow.md 一处（覆盖产品审与技术审）；
   SKILL.md 第 4 步为指引不重复；产品审与 start-readiness 提示词均不含处置/重审逻辑。

7. **SKILL 渐进式披露重构（本次流程确立的架构）。** 采纳渐进式披露架构：`SKILL.md`
   只持有流程骨架（九步顺序与目的）+ 跨步骤不变量 + 对参考文档的指引；步骤级执行机制
   统一迁到 `references/formal-flow.md`（执行该步时按需加载）。
   **修复方案：**
   - 把 `SKILL.md` 第 2 步（需求修订/start-readiness FAIL 时黑盒 QA 用例增量修订、
     meaning-preserved 重绑、语义已变回需求澄清）、第 3 步（拆分/路线细节、合并门自动
     附加、custom 不延伸、路线跳过授权、后续新增需明确指示、开发开始后不能再加 QA）、
     第 4 步（双速调度/快速路径细节、不消耗开发后审查轮次；处置机制已在第 6 条迁出）、
     第 7 步（合并 QA/合并门细节）、第 8 步（修复流程 + 便宜健全性检查 + 继承判定规
     则）、第 9 步（Seal 跳过规则、跳过记录、跳过不延续、仅 P2 PASS 可见）的**步骤级
     执行机制**迁到/补到 `references/formal-flow.md` 对应段——凡仅在 SKILL 的机制必须
     写入 formal-flow，不能只删不留。
   - `SKILL.md` 保留：范围边界、开发受理流程、九步骨架（每步留叙述 + 指引）、接手与
     中途修改、独立派发纪律、结果校验与修复上限、门文件、状态与 VCS（全局不变量）。
   - 每步指引明确指向 formal-flow.md 对应段。
   **验收：** SKILL.md 不再含步骤级执行机制（只留骨架 + 不变量 + 指引）；formal-flow.md
   成为步骤执行机制唯一持有处；每个 SKILL 步骤段有指向 formal-flow 的指引；**内容对账**：
   从 SKILL 迁出的每条机制均能在 formal-flow.md 找到对应表述，不静默丢失文档化行为。

8. **会话级「不走流程」轻量化声明。** 正式流程首次被触发时（本会话第一个内容修改请
   求），主代理以书面化提示显式提醒用户可作会话级「不走流程」声明——受理澄清与开发流
   程一并跳过（纯 vibe coding，不建 run、不问询、不审）。用户声明后本会话所有内容修改
   不再走流程；可随时提出或撤销。默认仍走流程；不影响用户明确要求正式执行的请求。
   **已有正式 run 进行中时用户声明不走流程：不预设固定行为、不为该情形增加任何提示词
   或开发，由主代理按当时情境自行决定。**
   **书面提醒原文**（写入 `SKILL.md` 受理流程开头，主代理照读）：
   > **【流程提示】** 本会话已触发formal-gates正式开发流程。您可声明本会话范围内不适用
   > 本流程——受理澄清与开发流程一并跳过，后续内容修改按常规方式直接处理。未声明前，
   > 默认按正式流程执行。该声明可随时提出或撤销。
   **语言适配：** 用户以其他语言沟通时，提醒以该语言表达相同意思（用该语言的对等书面
   表述，不逐字直译中文）。实现时**先测试是否无需任何语言提示即可自然适配用户语言**；
   若不能，加一条简短语言提示词（如「以用户当前使用的语言呈现本提醒」）。
   **文档落点：** `SKILL.md` 受理流程开头 + `references/formal-flow.md` 相应指引。
   **验收：** 会话首个内容修改请求时主代理照读上述书面提醒（含【流程提示】前缀、按用
   户语言适配）；用户声明后本会话内容修改跳过受理与开发流程；可随时提出/撤销。

## Scope boundaries

注：本节「问题 N」指**原始用户反馈的问题清单**（问题 1-7），与条目编号（1/2a/2b/4/5/6/7/8）不一致，勿混淆。

- 问题 3（需求中途修订后 QA 用例无法更新）：现代码已支持增量修订，经核实不复现，不在
  本次范围。
- 问题 5（主代理直行动作需伪造 lifecycle 事件）：文档化的主代理直行动作
  （requirements-clarification、carry --main-agent）本就豁免生命周期验证，经核实不复现，
  不在本次范围。
- 主代理冒充 worker 等需要违反文档化流程才能成立的场景：项目边界明确排除，不阻塞、不
  触发实现工作。

## 产品审处置记录（2026-08-05）

独立产品审 reviewer（dispatch-cc9a7c13edf1d8036be55608）返回 FAIL 含 4 项发现项，
主代理逐项核查后转达用户，用户逐项处置——按处置协议（见需求 6 的分类），4 项均被
**用户认同为需要修订的真问题**（非「接受作废」）；其中 P1-1 为 P1，故触发修订后
**重新产品审**（dispatch-97bea15d33c9aa808f10c72e）。

| 项 | 严重度 | 用户处置 |
|---|---|---|
| 修复 2b 需同步修正 install-and-hooks.md 的 Codex UNAVAILABLE 陈述 | P1 | 认同，纳入 fix 2b 文档范围 |
| 修复 2b 事件不配对的失败路径未定义 | P2 | 认同，硬阻断（与 Claude 一致） |
| 修复 2a 放宽认领会静默吸收开发期间无关外部提交 | P2 | 认同，静默吸收（文档注明） |
| 修复 1 未说明既有 OPEN/CLAIMED 派发命运 | P2 | 认同，接受澄清（重绑自然标 STALE） |

已处置结论分别并入上述第 1、2、3 条需求的「产品审已处置」注记，并经
`settle-findings` 注入后续派发不再重提。

### 重新审 2（dispatch-eaa323720ce96d4836cf6bfa）

最终重审 reviewer 对修订后需求（revision 3b810fc）返回 3 条 P2 发现项，主代理逐项
核查属实后转达用户，用户全部认同：

| 项 | 严重度 | 用户处置 |
|---|---|---|
| 需求 6 范围未覆盖 start-readiness.md 的同类处置协议 | P2 | 认同，一并迁出（start-readiness.md 处置文本迁至 formal-flow.md） |
| 需求 7 迁移需补内容对账标准 | P2 | 认同，补对账标准 |
| 拆分建议漏第 5、7 项 | P2 | 认同，补全拆分建议 |

全部处置分别并入第 6、7 条需求与拆分建议；均为 P2，按处置协议不触发重新审，但用户
要求再来一轮，故进入下一轮产品审。

### 重新审 3（dispatch-e7fca423ccd66ccced124d9c）

第 3 轮产品审 reviewer 对修订后需求（revision 60ba665）返回 2 条 P2 发现项，主代理
逐项核查属实后转达用户，用户全部认同：

| 项 | 严重度 | 用户处置 |
|---|---|---|
| 需求 6「只留」会误删提示词里的必填拆分建议与已拍板规则 | P2 | 认同，补保留说明 |
| 需求 3 未定义端到端验证失败回退 | P2 | 认同，补失败回退（验证失败则保持 required=false 不翻转） |

全部处置并入第 6、3 条需求；均为 P2，按处置协议不触发重新审。产品审已记录 PASS
（dispatch-e7fca423ccd66ccced124d9c）。

### 技术审（start-readiness，dispatch-f37879c4342b212eac20b02a）

Part 2 技术审 reviewer 返回 FAIL 含 3 条发现项（1 条 P1 + 2 条 P2），主代理逐项核查
属实后转达用户，用户全部认同：

| 项 | 严重度 | 用户处置 |
|---|---|---|
| Codex required 翻转会打挂本地/测试/canary 验证表面 | P1 | 认同，补本地宽松机制（未安装二进制走独立宽松 provider） |
| 修复 1 谓词 !hasDevelopmentSnapshot 误伤 REPAIR_PREPARED | P2 | 认同，改谓词为「尚无开发快照（PENDING/PREPARED）」 |
| 需求 7 迁移枚举漏 SKILL 第 2 步 | P2 | 认同，补第 2 步入迁移清单 |

全部处置并入第 1、3、7 条需求；存在 P1，故修订后重新技术审。

### 需求变更重走（2026-08-05）

第 8 条（会话级「不走流程」轻量化声明）为用户中途新增需求，属 **meaning-changing**
范围扩张；按项目规则以 `--meaning changed` **作废全部结果并回需求澄清**，全量重走产品
审与技术审。第 8 条内容已经用户澄清与确认（含【流程提示】前缀与语言适配）。

### 全量重审产品审（dispatch-d61caa4d85a90627c0188f2d）

meaning-changed 后全量重走产品审，reviewer 对当前完整需求（8 条）返回 2 条 P2 发现项，
主代理逐项核查属实后转达用户，用户处置：

| 项 | 严重度 | 用户处置 |
|---|---|---|
| Scope boundaries「问题 N」编号与条目编号歧义 | P2 | 认同，加注编号（问题 N 指原始问题清单） |
| 第 8 条未定义已有 run 进行中声明不走流程的行为 | P2 | 不预设固定行为，由主代理自行决定 |

两处处置并入 Scope boundaries 与第 8 条；均为 P2，处置后不重新审。产品审已记录 PASS
（dispatch-d61caa4d85a90627c0188f2d）。

## 拆分建议（必填留痕）

- **拆分理由 / 建议：不拆。** 八条共享「让正式流程真实场景可用」主题：{1, 2a} 为两处
  工作流守卫改动，{2b} 为一处布尔翻转，{4, 5} 为 formal-flow 文档注记，{6, 7, 8} 为处
  置协议、渐进式披露与会话级轻量化的文档重构（低风险、无行为面）。按测试原则只新增两
  条最低层 workflow 测试，风险低、无跨切片交互耦合。拆分会引入多 run 实例、合并门/合
  并 QA 与逐切片路线的流程开销，对本单元规模不成比例。
- **如何拆（若改拆）：** {1, 2a} 工作流死锁 | {2b} Codex 生命周期 | {4, 5} formal-flow
  文档注记 | {6, 7, 8} 处置协议 + 渐进式披露 + 会话级轻量化。各组相互独立、可并行。
- **改拆后果说明：** 若改拆，黑盒 QA 设计沿新拆分拓扑展开、已覆盖用例复用；整体级产
  品审结果被各切片继承、不单独重跑。

## 测试原则

确定性规则在拥有它的最低层测试：问题 1、问题 2a 各补一条最低层 workflow 测试。不添加
主要目的是重测其他测试或校验器的测试。
