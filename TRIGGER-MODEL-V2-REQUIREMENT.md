# TRIGGER-MODEL-V2 需求：默认提醒一次 + 大需求额外强调 + 轻量路线回归

本文档登记 trigger-model-v2 正式 run 的需求与方案。该需求变更上一轮封板（4f655120）
的触发模型：普通请求从「静默直接处理、仅大需求提醒一次」改为「所有请求默认提醒一次、
大需求额外强调一次」，并让轻量路线回归（新语义：创建正式 run 但跳过全部验证、只留记
录）。本修订纳入产品审发现项的处置结论：宿主全局指令纳入改动范围、轻量路线明确不做拆
分决定与路线选择、两处带旧「轻量」字样的表述改名对齐；另补充「大/复杂需求」判定标准
（沿用上一轮封板定义）与两个改名目标文件在落地位置清单中的补列。二次修订纳入技术审
发现项处置：轻量路线步骤收敛为三步完事、不快照（start → 需求登记 → Seal），CLI 对轻
量迁移门整体豁免。三次修订纳入两轮审查 8×P3 处置（全部纳入）：背景节标注被取代、效果
澄清 run 溯源、双提及边界写明、验收措辞改为路线模式集合、start 声明机制钉死、封板标注
未验证、SetRoute 文案更新、package_test 断言同步。

## 需求背景

上一轮 trigger-model-removal 把触发模型定为「默认直接处理、不自动触发、仅当需求过大过
复杂时提醒一次（不要求回应）」，并整体移除了轻量路线（旧轻量 = 不创建正式 run 的 vibe
coding）。本轮用户重新定义触发模型与轻量路线语义：

- 用户原话：改为默认提醒一次，大需求额外强调一次；不要求回应是真的，但不用显式写出来。
- 默认文案：普通请求提醒「若需走 formal-gates 流程，可直接提出」（条件式）。
- 大需求文案：检测到复杂需求，建议走 formal-gates 流程（建议式）。
- 轻量机制可以加上：走流程但不走任何 QA 和门，只留记录；最简步骤跳过 start-readiness，
  也不做拆分决定、不选路线。（此处用户原话中的「仍快照 + Seal 封板」已被后续修订取代，
  最终语义见「轻量路线」节：三步完事、不快照。）
- 旧轻量就是现在的用户不主动提及的默认流程（被当前「用户未明确提及即直接处理」取代），
  不再存在「不创建正式 run 的轻量路线」这一独立选项。

## 新触发模型

1. **所有内容修改请求**（含改错别字等小改动）主代理默认提醒一次，文案条件式：
   「若需走 formal-gates 流程，可直接提出」。提醒不要求用户回应，但文案不显式写「不要求
   回应」（行为成立即可，不写出）。
2. **大/复杂需求**在默认提醒之外**额外强调一次**（共两次提及），文案建议式：
   「检测到复杂需求，建议走 formal-gates 流程」。「大/复杂需求」判定沿用上一轮封板
   定义：规模、耦合、风险或验证复杂度超过普通直接处理的能力（与普通请求的分界由主代
   理按此标准判定，不作硬性量化）。两次结构化提及是设计上限。
3. 用户明确要求走正式流程（或明确要求触发 formal-gates）时，才进入完整受理流程：澄清
   → 完整方案 → 单独确认 → 评估 → 路线。受理阶段只决定是否进入正式流程（是/否），不
   决定路线；进入后 `full`/`custom` 正式路线在拆分决定之后确认；`lightweight` 不经拆分
   决定与路线选择（轻量即不验证、只留记录，路线无需选择）。
4. 主代理不得自己触发正式流程，也不得反复提及。「反复提及」指在两次结构化提及（默认
   一次 + 大需求额外一次）之外反复催促或追问，不覆盖第 1、2 条的设计提及。
5. 非修改性的提问、解释、诊断和 review 不触发。
6. 会话级「不走流程」声明、【流程提示】、高置信跳过受理判定：保持移除。

## 轻量路线（回归，新语义）

- 定义：走正式流程但**不做任何验证，只留记录**。
- 步骤：`workflow start` → 需求登记 → **Seal 封板**，三步完事。**不做拆分决定、不选
  QA/门路线、不快照**；跳过 start-readiness、黑盒 QA、白盒 QA、全部门。快照在轻量
  中无意义（start 时的 base/current 已固定，轻量无开发成果可冻结，故省去）。
- 效果：产生正式 run 记录 + 封板摘要；可溯源的是该 run 对 start 基线标识的引用与封板记
  录本身（run 溯源/留痕），不是改动内容钉扎——轻量无开发、无验证，无改动内容可溯源。
- CLI：`routeModes` 从 `{"full","custom"}` 恢复为 `{"lightweight","full","custom"}`；
  SetRoute 的错误文案与 full/custom 分支逻辑随 lightweight 一并更新（轻量选中零门）；
  轻量路线对迁移门整体豁免——不要求记录拆分决定（route 前拆分门、routeModes 校验、
  拆分前的 product-review/start-readiness PASS 门）、不要求路线确认（快照前路线确认门
  routeRequiresConfirmation 不适用）、不要求开发快照（seal 的 hasDevelopmentSnapshot
  与 product-review/start-readiness PASS 门对轻量放行）。
- start 声明：`workflow start` 增加显式声明轻量路线的 CLI 表面（如 `--route lightweight`），
  使轻量 run 从 start 即知豁免；全流程仅 start → 需求登记 → Seal 可直达。
- 封板标注：轻量 run 的封板摘要/记录显式标注「本 run 未经任何验证」，与完整验证封板区分。

## 落地位置

1. 工作区根：SKILL.md（frontmatter description + 受理正文 + 开发受理流程节）、
   README.md、README_EN.md、formal-gates.manifest.json、agents/openai.yaml、
   references/formal-flow.md、references/example-run.md、references/managed-rules.json。
2. `~/.claude/skills/formal-gates/`：安装副本（同上各文件）。
3. `~/.codex/skills/formal-gates/`：安装副本。
4. `~/.cursor/formal-gates/`：安装副本。
5. 宿主全局指令：`~/.claude/CLAUDE.md`、`~/.codex/AGENTS.md`（及适用的 Cursor 宿主全局）
   同步新触发模型文案，并去掉旧文案中显式写出的「（不要求回应）」。
6. CLI 源码：`internal/validate/workflow.go` 的 `routeModes` 加回 lightweight，并实现
   轻量路线的迁移门豁免（见「轻量路线」节）：轻量 run 免拆分决定、免路线确认、免开发
   快照即可直达 Seal；`internal/validate/workflow_transition.go` 相关迁移门对轻量放行。
7. 白盒测试断言文案更新（`internal/validate/trigger_model_whitebox_test.go` 等，依据新
   文案；含 canary 改名断言、`package_test.go` 的 `TestInstallableMetadataNoAutoIntake`
   「不含 lightweight」断言按新模型同步更新）。
8. 改名目标文件（与「改名对齐」节对应）：`internal/validate/canary.go`（
   `lightweight-workflow` → `quick-e2e-workflow`）、`prompts/actions/
   requirements-clarification.md`（「轻量澄清兜底」→「快速澄清兜底」）。

## 明确不动（边界）与改名对齐

- canary.go `lightweight-workflow` 自测：**改名为 `quick-e2e-workflow`**（保留快速端到端
  canary 功能，改名避免与新版轻量路线撞名）。
- prompts/actions/requirements-clarification.md「轻量澄清兜底」：**改称「快速澄清兜底」**
  （保留受理期对明显缺口简单提问的功能，改名避免与新轻量路线语义撞名）。
- 发布目录 `~/.formal-gates/releases/`：不动。
- hook 配置、canary 注册（除上述改名外）、打包发布逻辑：不动。
- 实现中发现其他与新版轻量语义撞名或冲突的旧表述，以最新需求与语义为准，由主代理单独
  提出确认后处理。

## 验收

- 工作区 + 三份安装副本 + 宿主全局指令（`~/.claude/CLAUDE.md`、`~/.codex/AGENTS.md`）
  的触发模型表述与本文档一致：所有请求默认提醒一次（条件式文案）、大需求额外强调一次
  （建议式文案）、不显式写「不要求回应」、用户明确要求才完整受理。
- CLI 路线模式集合（routeModes）恢复支持 `lightweight`/`full`/`custom`；轻量 run 可
  start → 需求登记 → Seal 三步直达，且跳过 QA 与门、不快照、不做拆分决定、不选路线。
- 用 10 个以上假任务假对话实测：普通请求提醒一次（条件式）、大需求共两次提及（含建议
  式）、用户明确要求时进入完整受理流程、轻量选项在受理阶段不出现（轻量是正式流程内的
  路线，非受理阶段选项）。
- 旧轻量表述（不创建正式 run 的 vibe coding 路线）不再作为独立路线出现；`lightweight-
  workflow` canary 名与「轻量澄清兜底」表述不再出现（已分别改名 `quick-e2e-workflow` 与
  「快速澄清兜底」）。
