# 黑盒 QA 并行 worktree 与 Seal 提交压缩与 QA 模式重构

Date: 2026-08-05
Status: confirmed
Route: (formal 流程内确认)

## 背景

用户初提三项 formal-gates 流程优化（对应需求 1-4，其中黑盒并行含其时序边界需求 2）：

1. **黑盒 QA 与开发并行**——黑盒 QA 设计/review/返修本来就不依赖具体开发内容、只看需求，
   应与开发并行，用独立 worktree 隔离。
2. **Seal 压缩提交**——seal 时把本次工作流的所有提交合并为一条。
3. **去掉 static/live 概念**——过时了，QA 门彻底去掉 static/live 概念（黑盒/白盒 QA 门
   保留）。

初提需求已由主代理逐维拷问澄清、用户逐项确认完整需求与技术方案，并选择正式流程。评审
过程中追加确认需求 5（复审规则 CLI 强制——防止主代理误用复审规则，双向、只有用户可破
例）。本需求文档登记已确认的需求与技术方案，作为本 run 的验收输入与唯一事实来源。

## 术语

- **黑盒 QA（blackbox）**：真实的 QA。不关心代码是什么；只从「产品是什么、怎么用、需
  求是什么」出发，通过**实际使用产品**（文档化的正常使用方式操作产品、覆盖常见操作失
  误）验证需求是否完成并通过。不读实现、不看实现 diff、不看既有测试、不看开发者自测。
- **白盒 QA（whitebox）**：开发后由独立代理读实现设计面向实现结构的测试（单元、系统、
  集成等职责式定义），结构测试充分性依赖实现。

## Complete confirmed requirements

1. **黑盒 QA 设计/review/返修与开发并行（QA 隔离工作区）。** 路线确认后开发立即开始、
   不再等待黑盒 QA review；黑盒 QA 设计/review/返修在独立的 **QA 隔离工作区**中与开发
   并发推进。隔离工作区基于基线快照（Git 为从基线分支的 linked worktree；SVN 为签出到
   基线版本的工作副本；P4 为同步到基线 changelist 的客户端工作区），本次开发的变更不
   在其中，黑盒 QA 代理在正常导航下读不到本次开发代码——分支级隔离（隔离工作区中本次
   开发提交非祖先、正常导航不可达；非对象级硬隔离），正对应"只看需求"。白盒 QA 无此限
   制（开发后读实现设计/review，在主工作区）。
   **机制：**
   - `RunState` 新增 `qaWorktree` 字段；新增命令 `workflow qa-worktree --root <main>
     --run-id <id> --worktree <path>` 登记隔离工作区路径（登记时校验其原生标识 **== 基
     线**，与"恒基于基线"一致）。不要写死具体 VCS 写法——隔离映射为对应 VCS 的工作副
     本/客户端工作区（与切片隔离一致），由 host 按各 VCS 的原生机制创建。黑盒 qa-review
     记录 PASS 时自动清空 `qaWorktree`；需求作废重置（meaning changed /
     invalidateRequirementResults）时一并清空。
   - 黑盒 qa-design/qa-review 的 prepare/claim/record 中「原生标识 == 当前快照」校验与
     派发源绑定改对 **QA 隔离工作区**解析，且**恒等于基线**（不随当前快照推进变化）；
     提示词的工作区字段指向 QA 隔离工作区。隔离工作区是从基线快照创建的不变基座，可在
     run 内任意时点（含修复轮黑盒返修、快照后需求修订的增量重设计）重建并复用，始终不
     含本次开发代码。快照汇合点（需求 2）由状态守卫（黑盒 qa-review PASS）强制，不依
     赖隔离工作区的身份校验。
   - **守卫放宽：** `development-worker` 准备不再要求 qa-review PASS（黑盒）；黑盒
     qa-design/qa-review 不再被"开发已开始"阻止；`snapshot` 要求开发完成 **且** 黑盒
     qa-review PASS（两边都完成）。
   - **host 生命周期：** 每次黑盒设计轮开始时若无活动隔离工作区则**从基线**重建（含首
     次、需求修订后的增量重设计、修复轮中的黑盒返修），黑盒 review PASS 后移除（清空）
     并待下一设计轮重建。快速路径（路线未确认时的预演设计）沿用；最终路线不含黑盒时设
     计废弃 + 隔离工作区移除。
   - **前提：** 每个黑盒设计轮开始（无论重建或复用）都确保**当前**已确认需求文档/验收
     产物已注入/刷新到隔离工作区（revision 与 run 登记一致），使黑盒代理在隔离环境中
     读取当前版本需求；黑盒 qa-design/qa-review 读取工作区中当前版本需求文档。注入/刷
     新为**工作树状态**、不构成漂移——隔离工作区的原生标识校验对各 VCS 用基线/不可变
     级解析（Git=提交、P4=changelist、SVN=工作副本的 **BASE 版本级**，忽略因注入产生的
     工作树修改，svnversion 的 M 后缀不影响身份校验）。`workflow qa-worktree` 登记或黑
     盒 prepare 时校验隔离工作区内需求文档哈希与 run 登记 revision 一致（防 host 遗忘
     刷新注入）。
   - **设计/执行责任切分：** 黑盒设计阶段（隔离工作区）无已构建产品可"实际使用产品"—
     —用例以注入的**当前需求文档**为准（不得以基线产品文档为准，后者仍含旧时序与
     STATIC/LIVE 表述）；"实际使用产品"是**执行阶段**（快照后对主工作区已构建产品）
     的描述。
   - **时序分离：** 黑盒与白盒设计/review 时序分离——黑盒快照前在隔离工作区审完、白盒
     快照后在主工作区读实现设计/审，各 qa-design/qa-review 派发为单 mode、不混合。
   - **派发按 mode 限定：** qa-design/qa-review 派发按待定用例的 mode 限定——黑盒派发
     （含黑盒待定用例）对隔离工作区解析原生标识与派发源绑定（绑基线）；白盒派发（含白
     盒待定用例）对主工作区解析（绑当前快照）；派发源绑定的陈旧校验（SourceSnapshot）
     按 mode 分叉。CLI 经 `--mode` 入口（blackbox|whitebox）确定该次派发的 mode。
   - **需求问题走增量修订：** 并行 QA 设计暴露的需求问题走既有需求修订流程（meaning
     preserved/changed 增量修订），不新增机制；这是并行化"速度 vs 早期发现需求完备性问
     题"的刻意取舍（用户已确认）。
   - **失败路径：** run 中断后 `workflow resume` 重校验隔离工作区原生标识 **== 基线**
     （工作区应停在基线，未漂移即正常；仅真实漂移才需用户确认/重建）；`workflow abort`
     后由 host 移除隔离工作区（文档注明）；隔离工作区创建失败记 RUNTIME_ERROR。
   - **切片组合：** 切片实例各自持有自己的隔离工作区（仅该切片路线选了黑盒 QA 时）；
     保留总任务实例的合并验证不适用此机制（合并 QA 维持现状）。
   **验收：** 路线确认后可直接 prepare 开发；黑盒 qa-design/qa-review 在隔离工作区中与
   开发并发完成；快照前黑盒 review 未 PASS 则快照被挡；黑盒 review PASS 后隔离工作区
   自动清空、可正常快照；隔离工作区中正常导航看不到开发提交（各 VCS 原生机制保证）。

2. **黑盒 QA 审查截止点：等两边都完成再进下一阶段。** 开发开始不等黑盒 QA review；开发
   完成（快照）也不提前进入下一阶段——快照要求**开发完成 且 黑盒 QA review PASS 都满
   足**。这是需求 1 的时序边界，独立成条以明确验收。
   黑盒 QA 设计/review/返修循环**不计入共享审查轮次上限**（与现行开发前 QA 设计循环一
   致）；开发进程不被该循环阻塞；未满 3 次 FAIL 时主代理展示当前阻塞项仅为信息呈现、
   开发进程继续、不构成额外闸门。
   **恢复路径：** 黑盒 review **连续 3 次 FAIL**（中途出现 qa-review PASS 即清零重算；
   RUNTIME_ERROR 不计入、不打断连续）视为"长期不过"，主代理向用户展示当前阻塞项并由
   用户**决策**处置；选项包括：abort 重建、走需求修订（语义变更按 meaning changed）、
   或**显式授权手动放行**（类比 Seal --user-requested，记录授权来源）在快照黑盒 gate
   未通过时带风险继续——这是"等两边都完成"的显式手动覆盖出口，**非自动**放行。
   连续 FAIL 计数存于 RunState；手动放行经 `workflow snapshot` 授权入口（存于新字段、
   记录授权来源）。**放行入口不限于 3-FAIL 时点**——黑盒 review 长时间未返回或反复
   RUNTIME_ERROR 致快照门被挡时，用户也可经显式授权放行。**放行后下游语义：** 未获批准
   的黑盒用例经用户授权跳过、其验证状态**视为 PASS**（记录授权来源）；qa-execution 只
   覆盖已批准用例、未批准用例不计入需执行集；Seal 接受该 PASS 语义。放行走完（含手动
   放行）后，隔离工作区由 host 清理。
   **验收：** 任一未完成时 `workflow snapshot` 被挡；两者都完成后快照成功。

3. **Seal 压缩提交（仅 Git、自动）。** seal 时若 VCS 为 git 且 基线→当前 有 >1 条提交，
   自动压缩为一条（`git reset --soft <base>` + 重新提交，保留最终树）；压缩作为 seal 的
   **最后一步 VCS 操作**执行（校验通过后、落 summary 前）；summary 当前快照记录压缩后的
   提交、基线不变，门审查 compared 记录保持历史；压缩前确认工作树干净；单条提交或空范围
   不操作。SVN/P4 保持原样。
   压缩后的单条提交消息由主代理根据实际修改内容编写（host 决定），经 seal 的 CLI 入口
   传入（`--squash-message`）。
   被压缩的中间提交成为 dangling——**接受此审计性影响**：compared SHA 对**仅本地可追溯**
   （新鲜 clone / 对象 GC 后不可解析）；durable 审计证据为最终树与压缩前一致 +
   summary 记录，不额外打 tag。
   **边界：** 范围覆盖基线→当前内全部提交（含期间采纳的外部提交，一并压缩）；分支已推
   送时压缩后需 force-push（用户责任）。
   **验收：** git run seal 后基线→当前为单条提交、最终树不变；单条/空范围不重写；
   SVN/P4 不压缩；工作树脏时报错。

4. **去掉 static/live（去 kind、保留黑白盒）。** 用例不再带 `kind`(STATIC/LIVE)，改按
   `mode`(blackbox|whitebox) 区分执行方式（黑盒=通过实际使用产品验证需求；白盒=结构测
   试）；去掉逐模式质量下限，**不设机械化质量下限**——用例集充分性由 qa-review 的
   set-level 覆盖判定承担；被选中模式零用例恒为覆盖缺失→FAIL（该保证从 CLI 强制转为
   纯 review 判定是刻意接受）；**合并 QA 可为零用例的既有例外保留**；全部 STATIC/LIVE
   术语从 CLI/提示词/文档移除。黑盒描述按"真实 QA"表述（见术语）。
   **机制：** `QACase.Kind`/`QAResultRecord.Kind` → `Mode`；`--kind` → `--mode`；
   qa-design/qa-review/qa-execution 提示词改写（黑盒=零上下文、实际使用产品、在 QA
   隔离工作区设计——**隔离仅覆盖设计/review，黑盒执行在快照后对主工作区已构建产品进
   行**；白盒=开发后读实现的结构测试）；runner.go 结果契约文本同步。实现时放宽
   `RecordQADesign` 的空用例集拒绝与 qa-review 的 cases-missing 守卫，使被选中模式零用
   例可流到 qa-review 的覆盖判定。`kind`→`mode` 改键**不提供旧 state.json 兼容**（该删
   就删）；在途 run 状态格式变更后需重建。
   **严重度分类（写入 qa-design/qa-review 提示词）：** P2 仅为建议、不阻塞、不需处置；
   **覆盖遗漏**（用例集未覆盖需求验收点/被选中模式）判 **P1**、阻塞、必须补用例。
   **验收：** 活跃代码与活跃文档（CLI/提示词/文档/测试）无 STATIC/LIVE 残留（历史
   CHANGELOG 条目、VCS 历史、以及冻结需求/验收产物路径——与 rejectFrozenArtifactFindings
   一致——除外）；qa-design 用 `--mode` 记录 blackbox/whitebox；执行按 mode 分流；无质
   量下限、用例集充分性由 qa-review 的 set-level 覆盖判定承担（合并 QA 零用例既有例外
   保留）。

5. **复审规则 CLI 强制、只有用户可破例——仅适用于 product-review 与 start-readiness。**
   其他审查（qa-review、已选门等）不适用本强制，遵循各自既有规则（qa-review 的 3-FAIL
   计数与返修循环不受影响）。用户对发现项的处置表述为：**确认问题**（认为是真问题、需
   修订）或**驳回问题**（认为不是问题、作废）。CLI 强制下列规则：
   - **仅 P2 → 即 PASS，但须处置：** 审查只含 P2 发现项时，该轮即记录 **PASS**——P2 建
     议随 PASS 可见、不阻塞、不产生 FAIL；CLI 允许 PASS 携带仅 P2 的发现项；无需重审。
     但 P2 建议仍须由用户**逐项处置**（确认问题→并入需求/开发范围；驳回问题→作废）、
     经已拍板清单登记后，才进入下一步骤。
   - **P0/P1 → FAIL，用户逐项处置：** 含 P0/P1 时记录 FAIL；用户对每个 P0/P1 处置为
     **确认**或**驳回**。**确认的 P0/P1 → 强制重审**（修订需求后须派发新审查轮返回
     PASS，CLI 在重审前拒绝记录 PASS）；**驳回的 P0/P1 → 作废不阻塞**。
   - **只有用户可破例：** 任一侧的破例（确认的 P0/P1 未重审前直接 PASS、或要求对已 PASS
     轮重审）都须用户显式授权（类比 --user-requested），由 CLI 记录授权来源；主代理无
     破例权。
   **CLI 落地：** RunState 记录"需重审"标记（确认的 P0/P1 未重审时置位）；`record-action`
   PASS 在该标记置位且未重审时被拒、在仅 P2 时允许携带 P2 发现项记录 PASS；用户破例信
   号经 CLI 入口传入并记录来源；处置的确认/驳回经已拍板清单登记。标记随需求语义变更全
   盘重置（invalidateRequirementResults）一并清除。
   **验收：** 仅 P2 的审查记录为 PASS 且 P2 建议可见；确认的 P0/P1 修订后未重审直接
   `record-action PASS` 被拒；驳回的 P0/P1 不阻塞；用户显式破例可越过任一侧并记录来
   源。

## 非目标

- SVN/P4 不做提交压缩（仅 Git）。
- 合并 QA 维持现状，不适用隔离工作区机制。
- 隔离是分支级（非对象级硬隔离），不为隔离工作区加固对抗性访问。
- 不新增自动放行/自动闸门；手动放行需用户显式授权（见需求 2 恢复路径）。
- 不引入需求修订之外的新机制处理并行 QA 暴露的需求问题。
- 不设任何机械化质量下限（含集合非空）；覆盖充分性由 qa-review 的 set-level 覆盖判定承担。
- 历史 CHANGELOG 条目与 VCS 历史中的 STATIC/LIVE 不在清理范围。
- 复审规则 CLI 强制仅适用于 product-review 与 start-readiness，不扩展到 qa-review/门。

## 涉及文件（技术方案落地范围）

- 文档：SKILL.md、references/formal-flow.md、references/vcs-snapshots.md、
  references/sliced-runs.md、references/example-run.md、prompts/actions/qa-design.md +
  qa-review.md + qa-execution.md、CHANGELOG.md、README.md、README_EN.md
- 代码：internal/validate/workflow.go、internal/validate/runstate.go、
  internal/validate/runner.go、internal/validate/catalog.go、internal/validate/vcs.go、
  internal/cli/cli.go
- 测试：internal/validate/workflow_test.go、internal/cli/workflow_test.go、
  internal/validate/catalog_test.go 等所有涉及 STATIC/LIVE 与守卫时序的测试
- 安装：变更后重新安装已安装的 skill（~/.claude/skills/formal-gates）
