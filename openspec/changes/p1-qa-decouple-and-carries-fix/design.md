# 设计：P1 QA 彻底解耦与 Carry 继承修复

## 概述

本 change 修复两个 P1 运行缺陷与两个提示词重复维护问题，P2 仅文档化。
核心是让 QA review/design 动作结果按 mode 完全独立，并修正 carry 主代理
继承的取数入口。

## P1-A：QA review/design 按 mode 解耦

### 现状（已逐行确认）

- `RunState.Actions["qa-review"]`、`Actions["qa-design"]` 是单一共享 `ActionResult`。
- `RecordQAReview`（workflow.go:1804）写共享 `Actions["qa-review"]=PASS/FAIL`；
  白盒 review prepare 时 `requireTransition`（:3299）看到共享状态 PASS 即拒绝 → 死锁。
- 隐藏耦合：黑盒 review FAIL 时 `RecordQAReview`（:1806）把共享
  `Actions["qa-design"]=PENDING`，同样卡住白盒。
- `qa-execution` 已按 mode 解耦（`QAExecutionByMode`），可仿照。

### 目标结构

在 `RunState` 新增按 mode 存储的 review/design 结果：

```go
// 按 QA 派发 mode 分开存储 qa-review 权威结果；空 mode 键对应合并/单派发流程。
QAReviewByMode map[string]ActionResult `json:"qaReviewByMode,omitempty"`
// 按 QA 派发 mode 分开存储 qa-design 权威结果；语义同 QAReviewByMode。
QADesignByMode map[string]ActionResult  `json:"qaDesignByMode,omitempty"`
```

辅助方法（仿 `qaExecution`/`setQAExecution`）：

- `state.qaReview(mode) ActionResult` / `state.setQAReview(mode, ActionResult)`
- `state.qaDesign(mode) ActionResult` / `state.setQADesign(mode, ActionResult)`

空 mode（合并/单派发流程）用 `""` 键，是设计内形态；不为旧状态做迁移。

**格式校验（F2 处置 + start-readiness P2 #2 补机制）**：`LoadRunState` 读取
state.json 时校验结构与当前 schema 一致。新 schema 弃用顶层
`Actions["qa-review"]`/`Actions["qa-design"]` 的 QA 结果承载后，旧格式（或任何
schema 不符）的状态文件应清晰报错（指出格式不匹配），不得静默降级。该校验通用化：
任何必需字段缺失/类型不符均报错，不窄化为"旧格式"专属提示。

区分"新 run 尚未做 QA（map 为空，正常）"与"旧格式（无 per-mode map）"的机制：
- 两个新字段**不标 `omitempty`**（`json:"qaReviewByMode"` / `json:"qaDesignByMode"`），
  使字段恒在场（新 run 序列化为 `{}`）；`LoadRunState` 以 strict decode（拒绝未知
  字段）+ 必需字段在场校验实现：文件缺这两个字段（旧格式）→ 报格式不符；文件有
  `{}` → 视为新 run 正常。为避免与既有 `QAExecutionByMode` 的 nil 容忍迁移模式
  冲突，本 change 对这两个**新字段**不做 nil 容忍；既有字段的迁移逻辑保持不动。
- `state.qaReview(mode)` / `state.qaDesign(mode)` 的读取回退语义与
  `qaModeResultKey`/`qaModeCasesWithKey` 一致：per-mode 键非空即用，否则回退合并
  `""` 键；recorder 写回与读取用同一存储键。快照黑盒门在 canary/单派发 `""` 形态
  下读 `qaReview("")`，不读空。

### 读写点改造清单（逐处核对）

| 位置 | 现状 | 改后 |
|---|---|---|
| workflow.go:1701 `RecordQADesign` 成功 | `Actions["qa-design"]=PASS`、`Actions["qa-review"]=PENDING` | `setQADesign(mode, PASS)`、`setQAReview(mode, PENDING)` |
| workflow.go:1620 `RecordQADesign` RUNTIME_ERROR 路径 | 共享重置 | 按 mode |
| workflow.go:1804 `RecordQAReview` 成功 | `Actions["qa-review"]=status` | `setQAReview(mode, status)` |
| workflow.go:1806 FAIL 路径 | `Actions["qa-design"]=PENDING` | `setQADesign(mode, PENDING)` |
| workflow.go:1735 RUNTIME_ERROR | `Actions["qa-review"]=RUNTIME_ERROR` | `setQAReview(mode, ...)` |
| workflow.go:3296 `requireTransition` qa-review 前置 | `Actions["qa-design"].Status` | `state.qaDesign(target).Status` |
| workflow.go:3299 权威结果判定 | `Actions["qa-review"].Status` | `state.qaReview(target).Status`（target=mode） |
| workflow.go:156/160 `blackboxReviewPassed` | `Actions["qa-review"]` | `state.qaReview("blackbox")`（兼容 legacy "qa" 合并态） |
| workflow.go:1875 `RecordQAExecution` 空集放行 | `Actions["qa-review"].Status` | `state.qaReview(dispatch.Mode).Status` |
| workflow.go:3388 快照门 | `Actions["qa-review"].Status` | 按 blackbox mode |
| workflow.go:970/989 `actionPromptDetail` | 读 review FAIL | 按 mode |
| workflow.go:496/506 `SetRoute` 废弃设计 | 共享重置 | 按 mode |
| workflow.go:1683 `RecordQADesign` FAIL 检查 | `Actions["qa-review"].Status` | 按 mode |
| parallel.go:66 并行提示 | `Actions["qa-review"].Status` | 按 blackbox mode |
| canary.go:173 单跑 | 合并 `""` | 保持 `""`（canary 单跑语义） |
| runstate.go:258 `pendingRequirementActions` | 初始化共享 | 初始化为空 map 或按需初始化 |
| runner.go:158/186 结果契约 | 读 review | 按 mode（如需要） |

### requireTransition 的 target 传递

`PrepareAction` 目前只对 `qa-design` 传 `target=mode`（:785-786），`qa-review`
传 `target=""`。改为 `qa-review` 也传 mode，使 `requireTransition` 能按 mode 判定。

## P1-B：carry --main-agent 继承

### 现状

`eligibleMainCarryResults`（workflow.go:2202）对 QA mode 调 `qaModeResult(state, mode)`，
而 `qaModeResultKey`（:3767）只返回 `Snapshot == CurrentSnapshot` 的结果；修复快照后
CurrentSnapshot=S2，S1 的白盒 PASS 取不到，落到 `""` 回退（空）→ 永不 eligible。

### 修法

```go
result := state.qaExecution(qaDispatchMode(id))  // 直取该 mode，不要求 current snapshot
if result.Status != "PASS" { continue }
eligible := state.PreRepairSnapshot != "" && result.Snapshot == state.PreRepairSnapshot
eligible = eligible || (catalogChanged && result.Snapshot == state.CurrentSnapshot)
```

保留 legacy 合并流程：单派发 mode 为 `""`，`state.qaExecution("")` 即取合并结果，
行为不变。核查 `priorQAExecution` 回退路径以兼容旧状态中"结果被 reset 到 prior"
的情形。

### 影响面核实

- 改动只放宽 carry 的取数（能取到 S1 PASS），不改变重跑判定逻辑；
  因此不会导致原本 PASS 的项被强制重跑。
- 已 PASS 项可被 carry 继承而非重跑，符合文档意图。
- 重跑支持：`qa-execution-scope --decision AFFECTED --cases <ids>` 只重跑受影响
  子集，未受影响保持 PASS 继承（已有机制，黑盒测试 SC11 已验证）。

## P1-C：共享契约注入（限定审查类动作）

- `runner.go:ComposeActionPrompt`（:146）加 `isReviewerAction(actionID)` 判断，
  仅对 product-review、qa-review、start-readiness 注入
  `[Shared reviewer contract]\n` + `catalog.Base`（注入顺序：契约头部在前、
  action 块随后，与 `ComposeGatePrompt` 块序一致）。
- development-worker、carry、qa-execution、requirements-clarification **不注入**，
  避免"你是独立审查者、不要编辑仓库文件"段落污染非审查动作语义。
- **qa-design 也不注入**（产品审 P1 处置）：qa-design 已是设计写者（白盒写测试、
  黑盒写用例文档，RQ-011/RQ-013），收到"不要编辑仓库文件"契约（共享规则优先）
  会与写角色矛盾；`isReviewerAction` 排除 qa-design。
- 实现选判断分支（非函数解耦）：`ComposeActionPrompt` 调用点共有 4 处
  （workflow.go:821 prepareBoundPrompt、:1302、:3669、composeDispatchPrompt
  :3675），注入写在 ComposeActionPrompt 内部统一生效，其余调用者用非审查
  actionID 或 dispatch.Target，判断分支改动最小（start-readiness P2 #6 更正）；
  `actionResultContract` 已是按 actionID 分发的 switch，`isReviewerAction` 与
  其模式一致；拆函数会让两个函数各自复制约 20 行公共组装逻辑，反而更重复。
- 从 product-review.md、qa-design.md、qa-review.md、start-readiness.md 删除
  内联「任务完整性检查」块。
- **继承判定纳入 base**（start-readiness P2 #3）：注入 base 后，审查动作的组装
  提示词含 base，但 `requireNoPendingInheritance` 的 `actionPromptChanged`
  （workflow.go:3540）目前只比较 `action.Content` 原始哈希、base 变更不触发动作
  继承判定（gate 路径 `composedGatePromptHash` 含 base、会触发）。需让
  `actionPromptChanged` 对注入动作把 base 纳入比较（与 gate 对称），或显式写明
  base 变更不产生动作继承判定的取舍；同步更新 workflow.go:3481 注释（"base 只注入
  gate prompt"将失效）。
- **requirements-clarification 处置**（start-readiness P2 #4）：RQ-003 只删四个
  审查动作的内联块后，requirements-clarification.md（非审查、主代理交互执行）仍带
  整段「任务完整性检查」/独立审查者文本，留下第 5 份 reviewer-base 副本。处置：
  从该动作删除内联块（其语义由主代理交互执行、不需零上下文审查者契约），并在
  ComposeActionPrompt 中对 requirements-clarification 明确不注入。

## P1-D：formal-flow 去重（复审规则唯一持有在 formal-flow）

- **复审规则唯一持有处在 formal-flow**（deadlock 需求 6）：formal-flow 保留
  复审规则全文；SKILL.md 第 4 步收敛为**可执行摘要 + 指针**，删除重复的机制全文。
- SKILL.md 第 4 步保留的**可执行摘要**（决策级铁律，直接写主文档、非纯指针）：
  ① 复审结果按 P0/P1/P2/P3 分级；② 仅含 P2/P3 可记录 PASS、含 P0/P1 记录 FAIL；
  ③ 用户逐项处置，确认→需重审、驳回→作废；④ 主代理无破例权。细节在 formal-flow，
  SKILL 用一行指针指向它。收敛目标是同一条规则全仓只在一个权威位置写一次。
- formal-flow.md：其余机制重述（快照要求行 117 与 204 合并、squash、QA scope、
  隔离工作区）缩为一句「决策本体见 SKILL 第 N 步」，只留命令形式；清
  「问题 6」「RQ-014」等内部编号残留。
- **唯一持有处覆盖全部副本**（start-readiness P2 #5）：除 SKILL 第 4 步外，检查
  product-review.md、start-readiness.md 动作提示词内是否内联复审规则全文——若是
  审查者任务提示词有意自带副本则显式注明，否则一并收敛为指针。

## P2 文档化 + 调研知识库（RQ-005 / RQ-006）

- **RQ-005**：根目录新建 `P2-BACKLOG.md`：按领域记录全部 P2（54 条，位置+建议）。
  `.gitignore` 增加 `P2-BACKLOG.md`，不跟踪。
- **RQ-006（start-readiness P2 #1 补入设计）**：根目录新建
  `PROMPT-ENGINEERING-KNOWLEDGE.md`：按 2026 年 6-8 月业界最佳实践整理通用提示词
  工程知识（文档组织、指针方向、上下文工程、去重/单一事实源、长度成本），来源标注
  日期与出处类型。`.gitignore` 增加，不跟踪。知识库只承载通用知识，不针对本项目；
  其核心最佳实践（主文档=可执行指令+精确引用、关键规则直接写主文档、每条机制只在
  一个权威位置写一次、引用单向一层深）已吸收进 RQ-003/RQ-004。验收：两个文件存在
  且被 .gitignore 忽略（git check-ignore 通过）。

## 验证策略

- 单元/白盒：新增双 mode 独立 review/design 顺序用例、carry 继承用例。
- 黑盒：端到端双 mode 流程（先设计两 mode → 分别 review → 均 PASS → snapshot →
  exec → seal）；修复轮 carry 继承；AFFECTED 子集重跑。
- 修复后由主代理判定受影响既有用例，AFFECTED 子集重跑，未受影响保持 PASS。

## RQ-008~012 设计（增量补入，2026-08-09）

### RQ-008：invalidateRequirementResults 重置 per-mode

`invalidateRequirementResults`（workflow.go:3114）除重置 Actions 外，必须：
- 清空 `QAReviewByMode`/`QADesignByMode`（per-mode 权威结果作废）
- **各 mode 用例 `ReviewStatus` 置回 PENDING**（关键：`blackboxReviewPassed` 在有
  用例时要求用例全 PASS 且 review 非 FAIL，若仅清 review/design 权威结果而用例仍
  PASS，快照黑盒门仍放行——CASE-036 缺陷）
- QAExecutionByMode 以空 map 替换重置（start-readiness P2 更正：当前代码无
  "先清空再遍历空集"的死循环，重置即空 map 替换）

### RQ-009：sha256 状态完整性硬阻止

`RunState` 加 `StateIntegrity`（json `stateIntegrity,omitempty`）。`SaveRunState`
marshal 前置空自身、`json.MarshalIndent` 规范化、sha256 回填后写盘；`LoadRunState`
非空时置空重算比对，不匹配返回 "run state integrity check failed: state was
modified outside the CLI"、硬拒绝。旧 state（无字段）跳过校验。测试覆盖 round-trip
/手改拒/legacy 跳过/<5ms 性能。随 Seal 保留。

### RQ-010：start --current-snapshot

`StartOptions` 加 `CurrentSnapshot`；`Start` 校验其为 HEAD 祖先/相等后采用为
currentSnapshot，默认 HEAD。`cli.go` start 加 `--current-snapshot` flag 接线。

### RQ-011：主代理与审查类代理写阻断（PreToolUse hook）

**用户拍板：主阻断即可，不加第二层状态硬门、不加复杂加固。**

**核心语义（用户 2026-08-09 明确）**：**正式流程进入开发阶段后，主代理与全部
审查类代理不得写代码或直接改 run 状态；其余代理不阻断。** 判定按调用者身份，
不按文件路径（无静态文件白名单，千项目通用）。

**登记文档豁免的识别（start-readiness P2 澄清）**：主代理对"已登记需求/设计文档"
的编辑豁免，按 run 状态 `RequirementArtifacts` **动态识别**（每 run 的登记集，
`workflow show` 可查）——不是静态白名单，与"无文件白名单"不矛盾：豁免范围是流程
确认的动态登记集，hook 读该集比对被编辑路径。

**阻断（deny）**：
- 主代理（主线程，payload 无 `agent_id`/`agent_type`）与审查类代理——
  product-review、start-readiness、qa-review、qa-execution、carry、各门审查——
  对**代码与 run 状态的直接写入**：Edit/Write/MultiEdit、`git commit`、写文件 Bash。

**放行（allow）**：
- `formal-gates` CLI 命令（run 状态唯一合法写入者）与只读命令；
- development-worker（写代码）；
- qa-design（白盒设计者写测试代码、黑盒设计者写用例文档）；
- 主代理对**已登记需求/设计文档**的编辑（需求更改流程的一部分）。

**登记规则（需求澄清提示词内，主代理提出、用户确认）**：
登记集 = 该 change 承载需求与方案的文档。逐文件按"改该文件是否改变该 change 的
需求/方案"判定：是 → 登记；否（任务/进度/执行/跟踪类）→ 不登记。格式无关，不按
文件名/后缀/位置识别。登记集 = RQ-011 主代理豁免集 = 需求修订作用域，三者同一。

**冻结**：审查通过、进入开发后，已登记文档被修改即触发 CLI 硬阻断
（`requireCurrentDefinitions`：frozen requirement artifact changed），须走需求
更改流程（`requirement --meaning`）回需求澄清重新登记。

**生效范围**：存在活动正式 run 时生效；无活动 run 放行。要求 host ≥2.1.143
（历史 headless/bypass 洞已修复）；实机 canary 验证主线程阻断、development-worker
放行、审查类代理阻断、登记文档豁免、无 run 放行。

### RQ-012：产品审/技术审增量审查（参考 QA）

参考 QA 的逐项 `ReviewStatus` + 语义键保留机制：
- **逐项存储**：`RunState` 加 `ReviewItemsByAction map[string]map[string]ReviewItem`
  （action → 需求项键 → {Status PASS|FAIL|PENDING}）；`Actions[actionID]` 保留为
  聚合结果（下游判断不变）。`ReviewItem` 含 Status/DispatchID/Message。
- **增量来源（格式无关）**：主代理在 `prepare-action` 显式传 `--scope <item>...`
  （可重复）声明本次审查范围，不解析文档结构（openspec/PRD 统一）。
- **让 AI 不犯错（照搬 QA）**：
  - `--scope` 声明的项置 PENDING 待判；未声明的已 PASS 项保持 PASS、任何轮不可改
    （除非主代理下次显式声明变更）
  - 审查者提示词：PENDING 项"必须判定"、PASS 项"accepted context 不得重判"
  - `record-action --item <key> --item-status <PASS|FAIL>`：所有 PENDING 必须全判、
    对 PASS 项下发判定被拒、FAIL 项必须带 finding
  - meaning-preserved 重绑不清表；meaning-changed 清空（全量重审）

### RQ-013：白盒 QA 机制重新定义（2026-08-09 用户明确）

**现状缺陷**：白盒 QA 三步断裂——qa-design 产出四字段用例（不进 ledger）、qa-review
审用例集、qa-execution 跑 dev 交付的已有测试。caseId 与测试函数无机制绑定（QACase
无测试字段；执行时跑哪个测试由执行代理主观决定；CLI 只校验 caseId 覆盖+文本非空）。
设计出的用例没被执行（执行的是另一套测试），白盒设计空转。

**新机制**：
1. **白盒设计者独立设计并写测试代码**：设计者从需求+实现独立设计用例，并直接编写
   结构测试代码（区别于 dev 交付的已有测试）。用例四字段文档仍要有，用于解释用例
   并作标 PASS 依据。caseId 与设计者写的测试建立真实对应。
2. **caseId↔测试绑定（可验证，产品审 P2 + start-readiness P1 处置）**：每条用例
   文档在对应用例下**写明实现该用例的测试引用** = `<文件路径>::<函数名>`（文件路径
   定位到交付测试代码所在文件、函数名定位到该文件里的测试，两个不透明字符串、不解析
   代码内容），使用例文档自包含——读文档即知该测试在哪个文件、叫什么，使"测 A 的
   测试给 B 用例标 PASS"可被发现。**实现**：
   - `QACaseInput`/`QACase` 增加 `Test` 字段（测试引用 = `<文件>::<函数>`）；
   - `qa-design` CLI 增加 `--test <file>::<function>` flag 记录该用例的测试引用；
   - 校验算法：白盒记录用例时，CLI 只校验 `Test` 非空、且同一引用不被两条白盒用例
     共用（一个测试实现一个用例）；不满足即拒绝记录。测试的存在性与对应性由
     qa-review（读代码核对）与 qa-execution（实际运行）验证，CLI 不做解析/编译检查。
   - 验证策略补一条：白盒绑定用例（Test 字段非空、引用 1:1）。
3. **白盒 review 审用例本身 → 返工给设计**：review 审查用例/测试充分性与覆盖，审出
   用例本身问题（覆盖缺失/测试不足/描述不清）→ 返工给白盒设计代理修订。
4. **白盒执行运行这些测试 → 按归属返修**：qa-execution 运行白盒设计者写的测试；
   实现缺陷 → 返工给 development-worker；用例/测试问题 → 返工给白盒设计。

**对 RQ-011 阻断清单的影响**：白盒设计者（qa-design whitebox）现在**写测试代码**，
因此在 RQ-011 的放行清单中——qa-design 全部模式放行（白盒写测试、黑盒写用例文档）。
qa-execution 只跑测试（Bash）不写文件，按"审查类代理不写"置于阻断清单；qa-review
仍不写代码。具体清单在开发时按此对齐。
