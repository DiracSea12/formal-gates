# 需求：P1 QA 彻底解耦与 Carry 继承修复

## 背景

对项目功能与全流程暴力黑盒测试 + 全量文件穷尽审查后，确认两个 P1 真实运行缺陷
（正常操作可达），以及两个提示词重复维护问题。本 change 修复全部 P1，P2 仅文档化。

## 需求项

### RQ-001：QA review/design 动作彻底解耦（P1-A）

黑盒与白盒两个 QA mode 的 review 与 design 权威结果必须**完完全全解耦**，
不再共享单一动作状态。具体：

- `qa-review` 的权威结果（PASS/FAIL/RUNTIME_ERROR）按 mode（blackbox/whitebox）
  独立存储、独立判定；一个 mode 记录 review 结果 SHALL NOT 使另一 mode 的
  review 判定受影响。
- `qa-design` 的权威结果按 mode 独立存储；一个 mode 的 review FAIL 重置设计
  SHALL NOT 把另一 mode 的设计重置为 PENDING。
- 任一 mode 的 `prepare-action qa-review` 只受本 mode 的 review 状态约束；
  两个 mode 都完成设计后，可分别独立记录各自的 review，顺序不限。
- 快照黑盒门、`blackboxReviewPassed`、并行提示、record 校验等所有读
  `Actions["qa-review"]` / `Actions["qa-design"]` 的路径，全部改为按 mode 取。
- **不兼容旧 run**：不添加旧状态文件迁移、不保留仅为旧状态服务的合并 `""` 回退
  兼容路径。旧 run 状态文件不再适配。**格式不符即报错**：读取任何 run 状态文件
  时，若其结构与当前 CLI 期望的 schema 不符（包括但不限于旧格式的
  `Actions["qa-review"]`/`Actions["qa-design"]` 字段缺失、未知的必需字段缺失或
  字段类型不匹配），CLI SHALL 返回清晰错误（指出格式不匹配），不得静默降级为
  空/默认状态继续执行。该校验不窄化为"旧格式"专属：任何 schema 不符均明确报错。

### RQ-002：carry --main-agent 继承修复前 PASS 的 QA mode（P1-B）

`carry --main-agent` 必须能继承修复快照（pre-repair）之前已 PASS 的 QA mode，
与文档化行为一致（formal-flow 第 272-274 行、example-run 第 206-208 行）：

- 修复快照推进后，`eligibleMainCarryResults` 对 QA mode 直接取该 mode 的执行结果
  （`state.qaExecution(mode)`），不再经过只返回 current-snapshot 结果的
  `qaModeResult`/`qaModeResultKey`。
- 判定仍按 `PreRepairSnapshot` 匹配或 catalogChanged 分支；legacy 合并流程行为不变。
- 修复后，凡可能受本次改动影响的既有用例，由主代理判定受影响子集，
  用 `qa-execution-scope --decision AFFECTED --cases ...` 只重跑受影响用例，
  未受影响且已 PASS 的用例保持 PASS 继承。

### RQ-003：「任务完整性检查」块去重（P1-C）

- `ComposeActionPrompt` 注入 `[Shared reviewer contract]` + reviewer-base 契约，
  **仅限审查类动作**：product-review、qa-design、qa-review、start-readiness 四个
  零上下文审查者动作注入；非审查动作（development-worker、carry、qa-execution、
  requirements-clarification）**不注入**，避免"你是独立审查者、不要编辑仓库文件"
  的契约段落出现在开发工/执行动作中造成语义矛盾。注入顺序：`[Shared reviewer
  contract]` 头部在前、action 块随后，与 `ComposeGatePrompt` 块序一致。
- 从上述四个审查动作提示词删除内联的「任务完整性检查」重复块，reviewer-base 为
  唯一本体。
- `requirements-clarification` 由主代理交互执行、不是独立审查者，不注入、单独斟酌。

### RQ-004：formal-flow 与 SKILL 机制重述去重（P1-D）

按调研知识库落地要点（主文档=可执行指令+精确引用、关键规则必须直接写主文档、
每条机制只在一个权威位置写一次），收敛本项目文档结构：

- **复审规则（复审由 CLI 强制、P0/P1/P2 分级、仅 P2 可 PASS、确认→需重审、
  驳回→作废、主代理无破例权）的唯一持有处在 `references/formal-flow.md`**
  （与已拍板 deadlock 需求 6 一致）；SKILL.md 第 4 步收敛为**可执行摘要 + 指针**，
  不再重复机制全文。
- SKILL.md 第 4 步必须保留的**可执行摘要**（决策级铁律，直接写主文档、不是纯
  指针，避免 agent 不读参考文档就丢失这些判定）：① 复审结果按 P0/P1/P2 分级；
  ② 仅含 P2 可记录 PASS、含 P0/P1 记录 FAIL；③ 用户逐项处置，确认→需重审、
  驳回→作废；④ 主代理无破例权。细节（每条的具体判定、复审流程）在 formal-flow，
  SKILL 用一行指针指向它。
- formal-flow.md 删除其余对 SKILL 规则的机制重述（快照要求、squash、QA scope、
  隔离工作区），只保留命令形式与一句引用；清内部编号残留（「问题 6」/RQ-014 等）。
- 收敛目标：SKILL.md 与 formal-flow.md 的机制重复消除，同一条规则（尤其复审规则）
  在全仓只在一个权威位置写一次；SKILL 保持为可执行骨架 + 单向指针（引用单向、
  一层深，不做双向互指）。

### RQ-005：P2 文档化（不修）

- 根目录新建 `P2-BACKLOG.md`，记录全部 P2 发现项（位置+建议），加入 `.gitignore`
  不跟踪。本次不实现任何 P2。

### RQ-006：调研知识落盘为知识库（新增，用户 2026-08-08 追加）

- 按当前（2026 年 6-8 月）业界提示词工程最佳实践调研结论，把**通用**提示词
  工程知识落盘为知识库：根目录新建 `PROMPT-ENGINEERING-KNOWLEDGE.md`，
  加入 `.gitignore` 不跟踪。
- 知识库内容为通用最佳实践（文档组织、指针方向、上下文工程、去重/单一事实源、
  长度与成本），**不针对本项目具体需求**；来源标注日期与出处类型，区分官方
  权威与社区观点。
- 本项只落盘通用知识，不因此修改任何项目文档；据此知识库对 RQ-004 等项目的
  落点优化单独在开发阶段体现。

### RQ-008：需求失效路径重置 per-mode review/design（P1 修复）

- `requirement --meaning changed`（语义变更作废全部结果）时，除重置 `Actions`
  外，**必须同时作废 per-mode review/design 权威结果与各 mode 用例的
  reviewStatus（置回 PENDING）**，使快照黑盒门读到旧 PASS 不放行，须对新需求
  重新设计/重审后才能推进快照。
- 现状缺陷：开发把 qa-review/qa-design 移出 Actions 后，失效路径只重置 Actions
  （已不含这两动作），per-mode 旧 PASS 残留，快照黑盒门读到旧 PASS 放行。

### RQ-009：sha256 状态完整性硬阻止（新增，用户明确要求加固）

- **用户明确要求**：run 状态只能由 CLI 写入，任何人（含 host/主代理）不得手工
  改写；做成 CLI 硬阻止。
- `RunState` 加 `StateIntegrity` 字段，`SaveRunState` 写盘前计算 sha256（排除
  自身），`LoadRunState` 校验，不匹配即硬拒绝 "state was modified outside the
  CLI"、非零退出。旧 state（无字段）跳过校验。随 Seal 保留。

### RQ-010：start 支持显式指定当前快照（新增，用户 2026-08-08 拍板）

- `workflow start` 增加 `--current-snapshot <identity>`，显式指定当前快照停在某
  祖先（默认不传时仍取 HEAD）。传入值必须是原生 HEAD 的祖先或相等。用于接手
  "开发已提交"的 run 时让 current 停在开发前、已有开发提交作为待登记快照。

### RQ-011：主代理与审查类代理写阻断（PreToolUse hook，新增，用户明确要求）

- **用户明确要求**：正式流程进入开发阶段后，主代理（host）与全部审查类代理不得
  写代码或直接改 run 状态；run 状态只能经 CLI 写入，代码与执行文件只能经
  development-worker 派发写入。
- **阻断**：主代理（主线程）与审查类代理（product-review、start-readiness、
  qa-review、qa-execution、carry 继承判定、各门审查）对代码与 run 状态的直接
  写入（Edit/Write/MultiEdit、git commit、写文件 Bash）。
- **放行**：`formal-gates` CLI 命令（run 状态唯一合法写入者）；只读命令；
  development-worker；qa-design（白盒设计者写测试代码、黑盒设计者写用例文档）；
  主代理对**已登记需求/设计文档**的编辑（需求更改流程的一部分）。
- **登记规则（写入需求澄清提示词）**：登记集 = 该 change 承载需求与方案的文档。
  主代理按"改该文件是否改变该 change 的需求/方案"逐文件判定提出清单，用户确认
  后生效；任务/进度/执行/跟踪类文档不登记。格式无关，不按文件名/后缀/位置识别。
- **冻结**：审查通过、进入开发后，已登记文档被修改即走需求更改流程（CLI 硬阻断、
  回需求澄清重新登记）。
- **生效范围**：存在活动正式 run 时生效；无活动 run 放行，不干扰普通项目。

### RQ-012：产品审/技术审支持只审增量（新增，用户明确要求，2026-08-08）

- **用户明确要求**：产品审（product-review）与技术审（start-readiness）要像
  QA 设计（qa-design）一样支持**只审增量**——需求为 meaning-preserved 修订、
  新增或修订了需求项时，已审过的需求部分保留既有结论，只对新增/受影响部分
  重新审查，而不是对整份需求重新全量审查。
- **格式无关**：增量审查机制**不限定需求文档格式**——无论需求以 openspec、
  PRD 还是其它形式承载，增量判定都应适用；增量范围按"需求修订中新增/变更的
  需求项或验收点"识别，不依赖特定文档结构的解析。
- **机制**：product-review / start-readiness 的审查范围可限定为需求修订的增量
  （新增/变更的需求项或验收点）；已审查通过的存量部分由 CLI 保留、不重审。
  增量判定沿 meaning-preserved 修订的记录：仅当修订改变了某需求项的前提时才将
  其重新纳入审查范围，与 QA 增量的"未受影响保持 PASS"语义一致。
- 覆盖范围：需求澄清登记后的产品审（Part 1）、start-readiness（Part 2）都适用；
  与 QA 增量的既有行为对齐（只增删改确实受影响的项，未受影响保持既有结论）。

### RQ-013：白盒 QA 机制重新定义（新增，用户 2026-08-09 明确）

**背景**：当前白盒 QA 流程断裂——qa-design 产出四字段用例、qa-review 审用例集、
qa-execution 跑 development-worker 交付的已有测试，但 caseId 与测试函数之间没有
机制化绑定（执行"跑哪个测试"由执行代理主观决定，CLI 只做文本非空校验，可能"测 A
的测试给 B 用例标 PASS"且无法发现）。白盒 QA 设计的"独立设计用例"环节在当前做法
下是空转的（设计出的用例没被执行，执行的是另一套已有测试）。

**用户明确的新机制**：

- **白盒设计阶段独立设计并写测试代码**：白盒设计者从需求+实现独立设计用例，并
  **直接编写结构测试代码**（区别于/不依赖 development-worker 交付的已有测试）。
  用例文档（四字段：mode/description/procedure/oracle）仍要有，用来**解释用例并
  作为标 PASS 的依据**——即 caseId 与设计者写的测试之间建立真实对应，执行按用例
  对应的测试跑。
- **白盒 review 审用例本身的问题 → 给设计代理返工**：review 审查设计出的用例与
  测试是否充分、是否覆盖需求；审出用例本身的问题（覆盖缺失/测试不足/描述不清），
  **返工给白盒设计代理**修订用例/测试，不是直接判死。
- **白盒执行运行这些测试 → 有问题给开发代理返修**：qa-execution 运行白盒设计者
  写的结构测试；测试暴露的问题（实现缺陷）→ 返工给 **development-worker** 修复
  实现；用例/测试本身的问题 → 返工给白盒设计代理。

**核心变化**：白盒 QA 的测试代码由**白盒设计者**编写（而非 development-worker
交付、白盒只跑）；caseId 与测试建立真实绑定；review 返工给设计、执行返工按问题
归属（实现→dev、用例/测试→设计）。

## 非目标

- 不修复任何 P2 项（仅文档化）。
- 不做旧 run 状态兼容迁移。
- 不引入新的 QA mode、门或机制。
