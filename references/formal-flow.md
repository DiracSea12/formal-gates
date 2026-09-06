# 正式流程 CLI 命令映射

流程顺序只由 `SKILL.md` 拥有。这份参考只提供每一步的确切命令形式与步骤级执行机制。
使用已安装的 `formal-gates` 二进制。示例只省略重复的发现项或用例分组。

- [启动与 run 控制](#启动与-run-控制)
- [需求、拆分决定与路线](#需求拆分决定与路线)
- [开发之前](#开发之前)
- [开发与快照](#开发与快照)
- [开发后审查](#开发后审查)
- [继承判定、修复授权与 Seal](#继承判定修复授权与-seal)
- [成本计量](#成本计量)

## 启动与 run 控制

```bash
# 不指定 --base-snapshot 时，CLI 会把原生的当前标识解析为基线。
# 接手中断的 run 时，--base-snapshot 接受当前 HEAD 的任意祖先（或相等），
# 使已提交的在途工作落在"基线到当前"的审查 diff 内。
formal-gates workflow start --root <repo> --package-root <package> \
  --run-id <id> --flow formal --requirement <requirement-file> \
  [--requirement-artifact <requirement-or-solution-file> ...] \
  --vcs <git|svn|p4> [--base-snapshot <ancestor-or-current-identity>] \
  --split <yes|no> [--retained-overall] [--master <master-run-id>] \
  [--route lightweight]

# --split 是强制声明（需求 4）：启动时必须显式预判本次 run 是否拆分/并行。
#   --split yes：本 run 必须是保留总任务实例（--retained-overall）或切片实例
#     （--master <保留总任务 run id>）；启动即钉死归属，忘带 retained-overall 在
#     启动时就报错，而不是拖到拆分决定。
#   --split no：本 run 默认不拆分；后续 slicing 记录 split 需经用户确认修订声明
#     （--user-confirm + 修订理由，见下方 slicing），预测错了无需重启。
# 未声明 --split 拒绝启动。

# --route lightweight：轻量路线（非受理阶段选项）。创建正式 run 但跳过全部验证、只留
# 记录：免拆分声明与拆分决定、免路线确认、免开发快照，start → 需求登记 → Seal 三步直达，
# 跳过 start-readiness、黑盒 QA、白盒 QA、全部门；封板摘要/记录显式标注「本 run 未经任何
# 验证」。轻量 start 不接受 --split yes / --retained-overall / --master。

formal-gates workflow show --root <repo> --run-id <id>
formal-gates workflow resume --root <repo> --package-root <package> --run-id <id>
# 原生 HEAD 已漂移（外部改动）时，显式重绑当前快照并记录原因（需用户确认）。
formal-gates workflow resume --root <repo> --package-root <package> --run-id <id> \
  --adopt-external --reason '<reason>'
# abort 未经用户级确认（--user-confirm）不执行、不落任何状态；主代理无权单独中止 run
# （需求 6 误操作硬阻断）。
formal-gates workflow abort --root <repo> --run-id <id> --user-confirm

# 流程重置（需求 5）：只重置 run 的 .gates 流程状态、绝不触碰已开发内容（工作树/已提交
# 代码/需求与方案文档）。必须用户显式授权（--user-approve）才执行；重置后 run 回到可重新
# 登记的干净状态（需求可重新登记、整体审可重做、开发快照保留）。输出说明保留了什么、重置
# 了什么。
formal-gates workflow reset --root <repo> --package-root <package> --run-id <id> --user-approve
```

Resume 默认把逐门 catalog delta 报告为 `catalogDelta`；目录变化与需求修订一样是可恢复
分类，不是新 run 的硬要求。采纳外部改动后用 `workflow carry --main-agent --main-reason
'<reason>'` 继承不受影响的 PASS，或按需重新派发门。

采纳（`--adopt-external`）的行为分两种：
- **尚无开发快照**（开发状态 `PENDING`/`PREPARED`，不含 `REPAIR_PREPARED`）：只把
  `CurrentSnapshot` 重绑到新原生 HEAD 并记录采纳来源（Carry ADOPT provenance），不设
  置 `PreRepairSnapshot`、不重置审查面，既有 OPEN/CLAIMED 派发因源快照失效标 STALE。
  之后直接 `prepare development-worker` → commit → `workflow snapshot` 即可走通。
- **已有开发快照**：原快照成为 pre-repair 修复边界，重置审查面，不受影响的 PASS 走
  Carry 继承判定。

## 需求、拆分决定与路线

```bash
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action requirements-clarification
# 主代理把完整需求与方案直接写入本 run 绑定的唯一正式需求文件后，先用不带
# --confirmed 的登记调用执行结构预检并绑定待确认 revision；随后向用户展示文件全文与
# 该精确 revision。用户要求修订时更新同一文件，并按现有修订规则加
# --meaning changed 后重复此步。
formal-gates workflow requirement --root <repo> --package-root <package> \
  --run-id <id> --source <requirement-file>
# 仅在用户明确确认刚才展示的同一 revision 后，记录动作 PASS 并确认该 revision。
formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action requirements-clarification --dispatch <dispatch-id> \
  --status <PASS|FAIL|RUNTIME_ERROR>
formal-gates workflow requirement --root <repo> --package-root <package> \
  --run-id <id> --source <requirement-file> \
  [--requirement-artifact <requirement-or-solution-file> ... | \
   --clear-requirement-artifacts] --confirmed [--activate-guarantee]
# 单一结构化正式需求在首次确认时原子冻结显式 guarantee envelope；
# --activate-guarantee 保留为兼容写法。lightweight 不进入保证链路。
# Resume 报告修订已改变之后，对它的语义影响做分类。
formal-gates workflow requirement --root <repo> --package-root <package> \
  --run-id <id> --meaning <preserved|changed>

# 拆分决定在 Part 2（start-readiness）PASS 后记录，是所有正式 run 的必填留痕；
# 记录后即为绑定点、不重切。no-split 必须带原因留痕；split 需要分片数 >= 2。
# 声明修订（用户确认）：拆分决定与启动声明冲突时，--user-confirm + 必填修订理由
# （--note）即用户确认的声明修订——--split no 的 run 修订为 split 时自升保留总任务
# 实例（不重启、不重过整体审）；保留总任务实例改记 no-split 时降级解除保留身份
# （解开"记 no-split 即死端"）；切片实例（--master）不可经修订脱钩。修订留痕于
# run 状态（splitAmendment），绑定点仍是本次记录。
formal-gates workflow slicing --root <repo> --package-root <package> \
  --run-id <id> --decision <split|no-split> [--count <n>] \
  [--slice '<slice-definition>' ...] [--parallel '<parallel-suggestion>'] \
  [--note '<reason>'] [--master <retained-overall-master-run-id>] \
  [--ac-owner <AC-ID>=<slice-run-id|master-merge> ...] [--user-confirm]
# 拆分建议对所有正式 run 必填呈现并留痕，含：拆分理由、如何拆、哪些子任务可并行、
# 以及改拆后果说明（若改拆，黑盒 QA 设计按新拆分拓扑展开、已覆盖用例复用）。粒度判断
# 由 Part 2 `start-readiness` 技术审一并完成，不另派独立的粒度审查代理；技术审输出
# `granularity_review`，必须说明单一 Batch 或多个 Batch、`Batch → Subtask` 映射、依赖/接口/
# 状态边界、风险、DoD/测试、回滚/恢复、交接与上下文成本，以及不拆或改拆的理由。仅高置信
# 要拆时需用户确认拆分方案。分片场景下整体级产品审/技术审足够，切片继承整体审查结果、
# 不单独重跑（切片实例在记录拆分决定时继承整体级 product-review/start-readiness）。
# 用户已拍板（settled）的发现项清单：注入下一次 product-review / start-readiness
# 派发，审查者不再重提（需求修订改变前提时例外）。对每个发现项，用户逐项处置为确认
# 问题（confirm，认为是真问题、需修订）或驳回问题（dismiss，认为不是问题、作废）。调用
# settle-findings 前，主代理必须在同一条决策消息中逐项展示 finding ID、严重等级、逐条定级
# 理由，并用人话说明问题是什么、正常路径会发生什么错误、实际后果和建议怎么处理。只报
# ID/等级、只说“存在 P0/P1”或直接询问是否确认，均不得登记处置；缺少任一部分时，先前
# 用户回复不能作为授权，必须完整重述问题后重新取得决定。同等级发现可以分组展示，但每条
# finding 与其说明、理由和处置选项的映射必须无歧义。
formal-gates workflow settle-findings --root <repo> --package-root <package> \
  --run-id <id> --action <product-review|start-readiness> \
  [--confirm '<settled-message>' ...] [--dismiss '<settled-message>' ...]

# 路线在拆分决定之后确认；full = 黑盒 QA + 白盒 QA +
# 全部已发现门；custom 可任意组合黑盒 QA、白盒 QA 与各门，至少选一项；lightweight 选中
# 零门（不选 QA、不选门，只留记录，通常由 start --route lightweight 声明）。合并门/合并
# QA 不进正常选择列表。
formal-gates workflow route-candidates --root <repo> --package-root <package> \
  --run-id <id>
formal-gates workflow route --root <repo> --package-root <package> \
  --run-id <id> --mode <lightweight|full|custom> [--gate <gate-id> ...]
formal-gates workflow route-add --root <repo> --package-root <package> \
  --run-id <id> --gate <gate-id>
```

需求修订（SKILL 第 2 步的执行机制）——三分支决策：meaning-preserved → 保留既有 PASS、
快照重绑新修订（开发开始后需同时传 `--confirmed`）；meaning changed → 作废全部结果、回需
求澄清重新登记；需求修订或 start-readiness FAIL 时，黑盒 QA 用例增量修订——只增删改确实受
影响的用例（未受影响保持 PASS）：
- 修订按实际影响分类，不按修改的章节或文件名分类。只修改技术方案、且不改变用户可见
  行为、需求范围和验收标准时，使用 `meaning-preserved`，保留产品审 PASS，不重新派发
  产品审；受影响、原本未通过或尚未完成的 start-readiness 仍须重做。改变上述产品含义、
  或者无法确认是否改变时，才使用 `meaning changed` 并重新进入产品审。
- 开发开始后做 meaning-preserved 需求重绑时，CLI 要求用户确认信号：`workflow
  requirement` 必须**同时传 `--confirmed`**，否则被拒。
- 需求文档被改动导致 revision 漂移后，第一个依赖需求修订的流程命令即被拒，提示先
  `workflow requirement --meaning preserved|changed` 更新修订（仅 `--confirmed` 无法
  更新已变更的 revision）。**记录在途派发结果**（该派发按旧 revision prepare、结果属
  旧 revision）豁免该漂移硬阻断：先记录在途审查/门派发结果、再重绑，恢复路径可执行
  （避免漂移硬阻断与在途派发重绑守卫互锁成死锁）。
- 已确认 product-review/start-readiness finding 推动修订时，主代理必须以编辑前完整 base
  文本生成穷尽的 review-delta JSON（base/candidate revision、finding 来源、每个 hunk 的
  影响与核验、受影响 action），先运行只读 `workflow review-delta-preview --input <json>`，
  再把预览返回的 digest 和用户对完整 delta 的确认写回 JSON，以 `workflow requirement
  --review-delta <json>` 应用。CLI 只校验结构、revision/finding/hunk/确认绑定并保存摘要
  回执，不保存 base 文本或 diff，也不自行判断语义是否变化。
- `meaning preserved` 保留 product-review PASS，只把 delta 明示受影响的、尚未通过的
  start-readiness 投影置 PENDING。`meaning changed` 必须先用只读 `workflow
  requirement-impact-preview` 展示 base/candidate revision、四类含义影响、将失效的 PASS、
  回滚步骤和剩余产品审容量；应用时同时提交 preview digest、其 state revision 和用户确认。
  一次确认可在 review-delta 的 `REVIEW_DELTA_AND_REQUIREMENT_IMPACT` scope 下同时覆盖两份
  预览。digest/revision/确认任一缺失或过期均零写入；全部匹配时需求 revision、delta/impact
  回执、产品审回退及依赖结果失效在一次原子状态转换中提交。

### review-delta JSON 与两份预览的顺序

`review-delta-preview --input` 与 `requirement --review-delta` 使用同一种 JSON。命令读取
JSON 文件，不接受 Markdown 任务书；未知字段由生产解析器拒绝。初始输入只写下表的语义
字段，`baseRevision`、`candidateRevision`、`digest` 留空；预览从当前 run 和完整候选计算
这三项。主代理仍须在编辑前保留并核对 base 全文，逐 hunk 核实穷尽性。

| 字段 | 类型、必填条件与取值 |
| --- | --- |
| `sources` | 非空数组；每个实际来源 finding 一项，不重复 |
| `sources[].action` | `product-review` 或 `start-readiness` |
| `sources[].findingId` | 当前已确认 finding 的稳定 ID，从 `workflow show` 的 `preDevelopmentReviewSeries[action]` 当前候选及处置记录取得；不能用开发后 `postReviewFindings` 的 ID |
| `sources[].decision` | `CONFIRM`；该 finding 必须已由用户确认并通过 `settle-findings`/正式纠错登记 |
| `hunks` | 非空数组，穷尽完整 base 到 candidate 的所有变更 |
| `hunks[].id` | 主代理在本包分配的非空唯一标签，如 `HUNK-001`，不是 CLI 生成的运行 ID |
| `hunks[].affected` | 非空字符串数组；列出受影响 REQ/AC 或非需求章节 |
| `hunks[].sourceFindingIds` | 非空数组，只引用 `sources` 内的 finding；每个 source 至少被一个 hunk 使用 |
| `hunks[].disposition` | `CONFIRM` |
| `hunks[].impact` | 非空的具体变更影响 |
| `hunks[].verification` | 非空的主代理逐 hunk 核实记录，包括如何对比完整 base/candidate、排除无关变更 |
| `affectedActions` | 非空、不重复数组；只接受 `product-review`、`start-readiness`。`meaning preserved` 不能包含 `product-review` |
| `confirmationScope` | `REVIEW_DELTA` 或 `REVIEW_DELTA_AND_REQUIREMENT_IMPACT` |
| `userConfirmation` | 预览输入为空；应用时为实际用户确认内容及可追溯来源。预览输出仍为空，不能视为已确认 |
| `baseRevision`、`candidateRevision`、`digest` | 预览计算并返回；应用原样使用，不能自行填旧 revision 或重算 digest 掩盖变化 |
| `impactPreviewDigest` | 联合确认时填第二份 `requirement-impact-preview` 的 `digest`；仅确认文字变更时不能据此授权回退 |

下面是“一个已确认技术审 finding、一个方案 hunk”的最小完整模板，保存为
`review-delta.input.json`。`${finding_id}` 是待绑定标记；其余文案须替换为本次真实核实内容，
多个 hunk/source 按实际范围展开。模板不包含用户确认。

<!-- review-delta-input-example -->
```json
{
  "baseRevision": "",
  "candidateRevision": "",
  "digest": "",
  "sources": [{"action": "start-readiness", "findingId": "${finding_id}", "decision": "CONFIRM"}],
  "hunks": [{
    "id": "HUNK-001",
    "affected": ["REQ-001", "方案与技术约束"],
    "sourceFindingIds": ["${finding_id}"],
    "disposition": "CONFIRM",
    "impact": "修正已确认技术审指出的职责分配，产品行为和验收条件保持原意。",
    "verification": "逐段对比编辑前保留且 digest 匹配的完整 base 与完整候选；本 hunk 覆盖全部改动，并核对其来源为此 finding。"
  }],
  "affectedActions": ["start-readiness"],
  "confirmationScope": "REVIEW_DELTA",
  "userConfirmation": ""
}
```

运行前设置 `repo`、`package`、`run_id` 为实际项目、已验证包及 run；工作文件放在独立临时
目录中。以下 `read` 要求操作者从展示过的真实记录选择 ID 或填入已经取得的确认，不能由
脚本生成确认。`workflow show` 的原始输出是字段来源，不读取私有 `state.json`。

```bash
formal-gates workflow show --root "$repo" --run-id "$run_id" > show.json
jq '.preDevelopmentReviewSeries["start-readiness"]' show.json
# 核对当前候选的 resultDigest、finding ID 及有效 CONFIRM 后填写。
read -r finding_id
jq --arg fid "$finding_id" \
  '.sources[0].findingId=$fid | .hunks[0].sourceFindingIds=[$fid]' \
  review-delta.input.json > review-delta.bound.json
formal-gates workflow review-delta-preview --root "$repo" --package-root "$package" \
  --run-id "$run_id" --input review-delta.bound.json > review-delta.preview.json
# 向用户展示完整 delta；用户确认后，填写原回复及其来源，再生成应用输入。
read -r delta_confirmation
test -n "$delta_confirmation"
jq --arg confirmation "$delta_confirmation" '.userConfirmation=$confirmation' \
  review-delta.preview.json > review-delta.confirmed.json
formal-gates workflow requirement --root "$repo" --package-root "$package" \
  --run-id "$run_id" --meaning preserved --confirmed --review-delta review-delta.confirmed.json
```

若建议的是 `meaning changed`，在**第一次预览前**把模板的 scope 改为
`REVIEW_DELTA_AND_REQUIREMENT_IMPACT`，并按实际影响填写 `affectedActions` 和各 hunk。
先生成 `review-delta.preview.json`，然后生成影响预览；以下命令接在 delta 预览之后、用户确认
之前，替代上面 preserved 的确认与应用步骤。四个 `*-impact` 字段至少一个非空，其他适用字段
可增加；预览与应用必须使用完全相同的影响内容。

```bash
# scope_impact 是主代理核实后的具体产品范围变化及回退说明。
read -r scope_impact
formal-gates workflow requirement-impact-preview --root "$repo" --package-root "$package" \
  --run-id "$run_id" --review-delta-digest "$(jq -r .digest review-delta.preview.json)" \
  --scope-impact "$scope_impact" > requirement-impact.preview.json
# 同时展示完整 delta 和影响预览：失效 PASS、回退步骤、已有/剩余产品审额度。
# 只有用户的一次回复明确覆盖两者，才在这里填写这次联合确认。
read -r joint_confirmation
test -n "$joint_confirmation"
jq --arg confirmation "$joint_confirmation" \
  --slurpfile impact requirement-impact.preview.json \
  '.userConfirmation=$confirmation | .impactPreviewDigest=$impact[0].digest' \
  review-delta.preview.json > review-delta.confirmed.json
formal-gates workflow requirement --root "$repo" --package-root "$package" \
  --run-id "$run_id" --meaning changed --confirmed --review-delta review-delta.confirmed.json \
  --impact-preview-digest "$(jq -r .digest requirement-impact.preview.json)" \
  --impact-state-revision "$(jq -r .stateRevision requirement-impact.preview.json)" \
  --scope-impact "$scope_impact"
```

这里从联合 scope 的 `userConfirmation` 读取回退授权，无需再问一次。若确认分开取得，应用时
还可通过 `--impact-user-confirmation` 传入单独的回退确认。普通来源（无产品/技术审 finding）
的 `meaning changed` 同样必须做 impact 预览并提供这三个授权参数及相同影响字段；这种路径
不传 `--review-delta`，先登记变更（不加 `--confirmed`），再回需求澄清与确认。所有应用前均须
先收回在途结果。候选文件或来源处置变化后重新生成 delta；state revision 变化后重新生成
impact。单纯阅读期间时间流逝不代表状态变化。

拆分与路线细节（SKILL 第 3 步的执行机制）：
- 路线跳过授权：custom 未选中的部分获得路线跳过授权（`ROUTE` 来源），Seal 期间不会
  补做；custom 的省略不延伸到合并验证。
- 后续新增需要用户明确指示；开发开始之后不能再加入 QA。
- 分片 >= 2 时保留总任务实例自动附加合并门与合并 QA，不涉常规路线选择。

重复 prepare：完整 signature 绑定类型、target/mode、base/current、需求 revision、QA
用例集、catalog/prompt revision 与 action identity。相同 OPEN/PENDING/CLAIMED 返回既有
规范提示词和派发 ID，不增加 attempt；已有 PASS 时拒绝，已有 FAIL 时指向该类型的既有处置路径。
只有 RUNTIME_ERROR/REJECTED_INVALID_RESULT 可在同一 TaskKey 自动重试，默认共两次
attempt；耗尽后 `workflow authorize-retry --task-key ... --state-revision ... --reason ...`
只允许用户追加一次最终 attempt。

**派发提示词以 CLI 写出的规范文件为唯一权威源**（需求 6 硬阻断）：`prepare-action` /
`prepare-gate` 生成提示词时，把完整提示词内容（含 dispatch id、需求 revision、
base/current 快照等关键字段）写入本 run 规范提示词文件
`.gates/tmp/<run-id>/prompts/<dispatch-id>.md`，并把内容 hash 与文件路径作为派发状态
的一部分记录。写入时（第一时间）立即校验内容 hash == 记录值；派发（`claim-dispatch`）
时兜底再次校验文件内容/关键字段与 prepare 记录一致，不一致即硬阻断、该派发判定为
不可用。派发只消费该规范文件——主代理只发指向该文件的薄启动消息、**不得手写/凭记忆
拼写提示词内容**，子代理读文件执行。

## 开发之前

开发前检查分两段：先派发 Part 1 产品审（`product-review`），全部通过后再进入
Part 2。Part 2 双速调度：技术审先完成 `granularity_review`；高置信要拆 → 呈现拆分建议、
用户确认后设置分片，然后确认
路线（逐切片）；非高置信要拆（高置信不拆或不确定）→ 快速路径，黑盒 QA 设计可与
`start-readiness` 并行开始，"建议不拆（原因）"必填留痕，按单一 run 进行。QA 模式拆
为黑盒（真实 QA）与白盒（结构测试），各自可选、均由独立代理设计并经 review 批准。
这个循环不消耗开发后的审查轮次。

黑盒 QA 与开发**并行**：路线确认后开发立即开始、不等待黑盒 QA review；黑盒
qa-design/qa-review/返修在独立的 **QA 隔离工作区**（从基线快照创建、恒等于基线、始终
不含本次开发代码）中推进，快照要求**开发完成 且 黑盒 qa-review PASS 两边都完成**。
QA 隔离工作区的创建/登记/校验/清空/重建机制见下方 `qa-worktree` 命令注释。

```bash
# Part 1 产品审：承接需求细节澄清，审已实例化的需求文档，只评产品/策划层面。
# 发现项分级 P0/P1/P2/P3；用户已拍板的发现项不再重提（CLI 注入已拍板清单）。
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action product-review
# 每准备一个独立派发的动作或门之后，都重复这条认领命令。
formal-gates workflow claim-dispatch --root <repo> --package-root <package> \
  --run-id <id> --dispatch <dispatch-id> --reviewer <host-agent-id>
formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action product-review --dispatch <dispatch-id> \
  --status <PASS|FAIL|RUNTIME_ERROR> \
  [--finding '<message>' --severity <P0|P1|P2|P3> \
   --severity-rationale '<why this exact severity>' --category <category> \
   --requirement <REQ-ID> --normal-path '<documented normal path>' \
   --deferral-impact '<impact if deferred>' --location '<path>:<line>' \
   --verify-scope '<scope check>' --verify-severity '<severity check>' \
   --verify-causality '<causality check>' --verify-reproduction '<reproduction check>'] \
  --operator-check requirement-match --operator-check normal-entry \
  --operator-check evidence --operator-check locations --operator-check scope \
  --operator-check severity --operator-check binding --operator-check completeness \
  --operator-evidence '<Operator 对本候选逐项核验的简明证据>'
# 上述 operator 参数用于 PASS/FAIL 语义候选；RUNTIME_ERROR 不产生候选，不带这些参数。
# Operator 必须在记录前实际核验：结果符合已确认需求、正常入口可复现、证据与位置充分、
# 范围和严重度正确、dispatch/revision 绑定正确、结果逐项完整。CLI 拒绝缺项、重复项、未知
# 项或空证据；固定套话不能替代本次核验事实。
# start-readiness 每条 finding 还必须带 --category 与 --verify-classification；只有
# REQUIREMENT_CONFLICT、RESPONSIBILITY_OR_DEPENDENCY、MISSING_CORE_ACCEPTANCE_ENTRY 可定
# P0/P1，IMPLEMENTATION_DETAIL/OTHER 最高 P2。门审 P0/P1 另需 --actual-error。
# P1 定级理由必须说明对应需求、正常路径的实际后果及为何不能只定 P2；P0 还须说明需求
# 为何无法成立、核心验收无任何可用路径或交付被阻塞。缺少逐 finding 定级理由、正常路径、
# 当前错误、证据位置、延期影响或逐项核验时，CLI 非零返回但只原子追加
# REJECTED_INVALID_RESULT 回执；正式结果投影不变。obligation item FAIL 不自动决定严重度。
# 产品审的发现项是候选输入，复审规则由 CLI 强制（仅适用于 product-review 与
# start-readiness，只有用户可破例）：仅含 P2/P3 → 该轮即记录 PASS 且 P2/P3 建议随 PASS
# 可见、不阻塞、无需重审，但 P2/P3 建议仍须由用户逐项处置（确认→并入需求/开发范围；驳回→作废）、
# 经 settle-findings 登记后才进入下一步；含 P0/P1 → 记录 FAIL，用户对每个 P0/P1 逐项
# 处置为确认（confirm）或驳回（dismiss）。确认的 P0/P1 → CLI 置位"需重审"，修订需求后
# 必须派发新审查轮返回 PASS，重审前 record-action PASS 被拒；驳回的 P0/P1 → 作废、不
# 阻塞。主代理无破例权；任何破例（确认的 P0/P1 未重审前直接 PASS、或要求对已 PASS 轮重
# 审）都须用户显式授权（record-action/prepare-action --user-requested）并由 CLI 记录来源。
# 自动化控制器接纳 `PASS + P2/P3` 时先把 PASS 记为权威结果，再把 P2/P3 规范化为
# Advisory obligation；不得自动改判 FAIL、自动修复或静默丢弃。product-review /
# start-readiness 的 Advisory 合并成一次类型化 Ask，逐项只允许 confirm / dismiss；全部
# 处置并由 settle-findings 记录后才推进。确认 P2/P3 本身不置 `NeedsReReview`；若确认后修改
# 需求并造成语义变化，再由需求修订规则决定重审。开发后 QA Review 使用独立规则：逐用例
# FAIL/集合级 P1 阻塞并要求修改后重审；集合级 P2/P3 不阻塞 PASS，但必须按稳定 suggestion
# ID 全部吸收后 case-set 才可 FROZEN，吸收不复审。开发后 gate 的 P2 必须进入统一返修，
# 修复后用 completion/carry 凭据继承原 PASS、不复审也不增加轮次；gate P3 不返修，只在
# Seal 前集中展示。两类 P2/P3 不得混用处置规则。

# Part 2 技术审：承接技术方案选择与对齐，并一并完成 `granularity_review`。技术审必须
# 以用户确认后的技术档案为审查对象；代码只用于核实既有能力、依赖和边界事实，不把当前
# 候选代码本身当成提前代码审查的对象。技术审只评估宏观技术方案，以及会影响实际开发的
# 逻辑、架构、依赖或责任边界错误；不得因函数/结构体/字段/CLI 参数命名、文件组织、代码
# 写法、具体算法、测试代码质量或等价实现偏好阻塞。只有具体细节已经导致公开行为冲突、
# 接口无法对接、架构无法落地或核心验收无法完成时，才按实际影响报告技术阻塞项。
# 依据独立交付物/DoD、稳定接口/责任/状态边界、依赖与副作用/恢复边界、可管理的上下文与
# 交接成本判断大块（Batch）或小块（Subtask）；不能仅按编号、commit、文件/行数、阶段、
# 层、角色或时间段切分。大块是唯一开发代理换人边界；同一大块内的小块共享开发上下文，
# 不单独派发。输出还必须包含不拆或改拆的理由，并在同一 `granularity_review` 中留痕最小
# 范围边界：允许修改的入口或路径、本批次交付物、明确非目标和验收条件。该边界只能从
# 已确认需求与方案提取，不新增需求；不单独派发审查或新增状态。开发快照完成后按该边界
# 对照实际 diff，越界先按需求、方案或批次契约变化处置，不得静默吸收。发现项同样分级
# P0/P1/P2/P3，复审规则与产品审相同。
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action start-readiness
formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action start-readiness --dispatch <dispatch-id> \
  --status <PASS|FAIL|RUNTIME_ERROR> \
  [--finding '<message>' --severity <P0|P1|P2|P3>] \
  --operator-check requirement-match --operator-check normal-entry \
  --operator-check evidence --operator-check locations --operator-check scope \
  --operator-check severity --operator-check binding --operator-check completeness \
  --operator-evidence '<Operator 对本候选逐项核验的简明证据>'
# PASS/FAIL 必须带完整 Operator 核验；RUNTIME_ERROR 不带，规则同上。
# 技术审的 FAIL 发现项同样是候选输入，由用户逐项处置：确认问题 → 修订需求/方案后强制
# 重审；驳回问题 → 作废、不阻塞、不改需求/方案。复审规则与产品审相同。

# start-readiness PASS 后记录拆分决定，再确认路线（见上节）。

# 黑盒设计轮开始前（无论重建或复用）先登记 QA 隔离工作区：host 按各 VCS 原生机制从
# 基线创建（Git linked worktree / SVN 基线版本工作副本 / P4 基线 changelist 客户端），
# 并把当前已确认需求文档/验收产物注入其中。登记时 CLI 校验其原生标识 == 基线、且注入
# 的文档 revision 与 run 登记一致（防 host 遗忘刷新）。黑盒 review PASS 后自动清空、
# 下一设计轮重建。
formal-gates workflow qa-worktree --root <repo> --package-root <package> \
  --run-id <id> --worktree <path>

# qa-design/qa-review 派发按 mode 限定（blackbox|whitebox）：黑盒派发对 QA 隔离工作区
# 解析原生标识与派发源绑定（绑基线）、提示词的工作区字段指向隔离工作区；白盒派发对主
# 工作区（绑当前快照）。黑盒设计以注入的当前需求文档为准（不以基线产品文档为准，后者
# 仍含旧时序与旧表述）；"实际使用产品"是执行阶段描述。
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action qa-design --mode <blackbox|whitebox>
formal-gates workflow qa-design --root <repo> --package-root <package> --run-id <id> \
  --dispatch <dispatch-id> --case '<description>' --mode <blackbox|whitebox> \
  --procedure '<public procedure>' --oracle '<expected result>' \
  --ac <AC-ID> [--ac <AC-ID> ...] \
  [--test '<file>::<function>']   # 白盒用例必填：文件定位的测试引用，唯一、不被两用例共用
# 增量语义：只返回本轮变更——新增用例不带 id（CLI 分配）；修改既有用例须带既有 id：
formal-gates workflow qa-design --root <repo> --package-root <package> --run-id <id> \
  --dispatch <dispatch-id> --case '<新描述>' --case-id <CASE-id> --mode <blackbox|whitebox> \
  --procedure '<procedure>' --oracle '<expected result>' --ac <AC-ID> [--ac <AC-ID> ...] \
  [--test '<file>::<function>']
formal-gates workflow qa-design --root <repo> --package-root <package> --run-id <id> \
  --dispatch <dispatch-id> --remove-case <CASE-id>   # 删除既有用例
formal-gates workflow qa-design --root <repo> --package-root <package> --run-id <id> \
  --dispatch <dispatch-id> --replace-all [--case ...]
#  --case-id/--remove-case 引用的 id 必须存在于同 mode，否则报错；--replace-all 替换整个用
#  例集（空集即清空该 mode）；三者不能同轮混用。未提及的既有用例及其 review PASS 状态自动
#  保留、遗漏绝不清除。语义重复（同 mode 同描述/过程/预期）的无 id 提交报错并提示改用修改。
# 按该 mode 最近一次 qa-review 记录在案的 P2/P3 集合级建议逐条吸收：每项必须引用稳定
# suggestion ID；本轮新增、修改或删除用例直接置为已批准并留 SUGGESTION_APPLIED 溯源，
# 不派新 qa-review。要求绑定 qa-review 为 PASS；遗漏、重复、无关引用以及使验收点失去批准
# 覆盖的修改均被拒绝。全部建议吸收并重算覆盖完整后，case-set 才进入 FROZEN。
formal-gates workflow qa-design --root <repo> --package-root <package> --run-id <id> \
  --dispatch <dispatch-id> [--case ... | --case-id <id> ... | --remove-case <id>] \
  --per-suggestion --suggestion-id <suggestion-id> [--suggestion-id <suggestion-id> ...]
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action qa-review --mode <blackbox|whitebox>
formal-gates workflow qa-review --root <repo> --package-root <package> \
  --run-id <id> --dispatch <dispatch-id> \
  --case CASE-001 --outcome <PASS|FAIL> [--reason '<required for FAIL>'] \
  --source-decision <REQ-ID>=<PASS|FAIL|PENDING> \
  --point-decision <AC-ID>=<PASS|FAIL|PENDING> \
  --case-decision <CASE-ID>=<PASS|FAIL|PENDING> \
  [--finding '<set-level finding>' --severity <P1|P2|P3> --location '<path:line>']
# 集合层面发现项按严重度分类：覆盖遗漏（用例集未覆盖需求验收点/被选中模式，含被选中
# 模式零用例）判 P1、阻塞、必须补用例；P2/P3 是带稳定 ID 的非阻塞建议，PASS 后必须经
# qa-design --per-suggestion 逐条吸收才可冻结，但吸收不触发重审。不设机械化质量
# 下限，用例集充分性由 qa-review 的 set-level 覆盖判定承担。
# qa-review 审查完整合并集（未提及的既有用例自动保留在集中）；提示词注入本轮变更清单
# （Added/Modified/Removed）。黑盒 qa-review 的提示词工作区指向隔离工作区，用例文件在
# 隔离期间只存在于 `.gates/cases/blackbox.md`（run-state 派生 mirror，主工作区不可见）。

# FROZEN 用例集只有以下四种稳定条件变化可以解冻；普通实现返修、代码快照变化或一次性
# 环境不可用都不是合法类别。THAW 使受影响用例回到待设计/待审并使相关执行/carry 失效。
formal-gates workflow qa-thaw --root <repo> --package-root <package> --run-id <id> \
  --mode <blackbox|whitebox> \
  --category <REQUIREMENT_MEANING|EXTERNAL_BEHAVIOR|SUCCESS_FAILURE_CRITERIA|CASE_EXECUTABILITY> \
  --reason '<reason>' --source-revision <revision> --case-id <affected-id> [--case-id <id> ...]
```

双速调度细节（SKILL 第 4 步的执行机制）：黑盒 QA 用例在 QA 隔离工作区按当前需求设计
（路线未确认的快速路径沿用、与 `start-readiness` 并行），设计后派发独立 `qa-review`
批准；白盒 QA（结构测试）开发后由独立代理读实现设计并执行。黑盒与白盒的用例按 mode
分开存储，`qa-design` 记录轮只新增/更新/删除本派发 mode 的用例，不触碰另一 mode 的
既有用例（含其 review PASS 状态与已记录执行结果）。黑盒用例每次 `qa-design` 记录时镜像
到隔离工作区的 `.gates/cases/blackbox.md`（派生视图，git/svn/p4 统一）；seal 时把已批准
黑盒用例落盘合并回主干 `.gates/results/<run-id>.blackbox-cases.md` 作为交付物（只主干
seal，分片实例不落盘，成本并入主干封板）。黑盒 review 连续 3 次 FAIL
（出现 PASS 即清零；RUNTIME_ERROR 不计入也不打断）视为"长期不过"，主代理向用户展示
当前阻塞项并由用户决策处置；选项包括 abort 重建、走需求修订，或经 `workflow snapshot
--user-requested --reason '<reason>'` 显式授权手动放行（非自动）。放行不限于 3-FAIL
时点——黑盒 review 长时间未返回或反复 RUNTIME_ERROR 致快照门被挡时也可显式放行。放行
后 qa-execution 仍只覆盖已批准用例；未获批准的黑盒用例不写入 execution results，更不
得补造成 `skipped/authorized PASS`。授权只改变是否允许继续固定快照，不等同于该用例
获得审查或执行 PASS。

## 开发与快照

```bash
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action development-worker

# 开发或返修运行失败：结束本 attempt，保留原逻辑义务；随后 prepare 复用同一 TaskKey
# 并消耗有限重试额度。成功仍由下方 snapshot 登记，不能用 record-action 写 PASS/FAIL。
formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action development-worker --dispatch <development-dispatch-id> \
  --status RUNTIME_ERROR --message '<worker runtime failure>'

# 开发分块只使用技术审已记录的 granularity_review：一个 batch_id 对应一次
# development-worker 派发；Batch 内的 subtask_id 只是顺序步骤，不得各自派发或换代理。
# 前一 Batch 完成 DoD、验证和交接后，才准备下一 Batch 的新开发代理；契约、接口、依赖
# 或 DoD 改变时暂停并重新做粒度审查。

# 记录开发后或修复后的不可变标识。快照门：开发完成 且 黑盒 qa-review PASS 两边都
# 完成才允许固定快照，任一边未完成快照被挡；黑盒 review 未 PASS 时只有用户显式授权可手动
# 放行（--user-requested + --reason，记录授权来源，非自动）。
formal-gates workflow snapshot --root <repo> --package-root <package> \
  --run-id <id> --dispatch <development-or-repair-dispatch-id> \
  [--user-requested --reason '<manual blackbox-gate release reason>']
```

开发派发认领：开发/修复派发是 reviewer-required，认领要求原生 HEAD 与当前
快照一致；worker 一旦提交 HEAD 即前进，所以 `workflow claim-dispatch` 对
`development-worker` 派发放宽——当前原生 HEAD 是派发源快照的后代（或相等）即允许认领
（覆盖 worker 已提交的情形），随后 `workflow snapshot` 成功。其余派发（审查、QA 等）
检查不变。放宽后的检查不验证 HEAD 是否由 worker 产生：开发期间无关外部提交落地会被
**静默吸收**进开发快照（用户已接受此行为）。

开发分批的代理纪律（ADR-002 决策 8，对外项目同等生效）：每个批次由**全新的零上下
文开发代理**执行——薄启动（指向规范文件 + 批次任务书）一个新代理实例，同一开发代
理会话不跨批复用，reset 粒度为批次边界（单批一个内聚关注点、批内连续）。批次任务书
只写不可从仓库恢复的信息（决策理由、已知问题/债务、下一任务）；可从 VCS/代码/测试推
导的内容一律不写，防止交接文档自身膨胀成第二份腐化上下文。

## 开发后审查

**先形成 review-ready 快照。** 无白盒 QA 时，开发快照加全部已选 mode 的 `FROZEN`
case-set 即可进入 wave。存在白盒 QA 时，开发快照 S1 只作为白盒设计起点；设计者写入结构
测试后，host 先提交并推进到 S2，再在 S2 做白盒 QA Review。Review FAIL 后的用例/测试修改，
或 PASS 后 suggestion absorption 造成的测试变化，均继续提交并推进快照。只有最终快照包含
白盒测试、全部已选 case-set 均 `FROZEN` 且 version 绑定该快照时，才初始化 review-wave。

**一次准备完整 wave。** `prepare-review-wave` 冻结该快照的 QA execution 与全部已选门为
expected-set/digest，并一次返回所有 eligible 派发，host 并行启动，不得逐目标串行准备。
同一 wave 不得增删改目标。门 prompt 只包含已确认需求、基线到最终快照的完整 diff 与门规则，
不得注入 QA 用例正文、逐用例证据或测试实现细节。

```bash
# 首次执行默认为 FULL。普通中间返修固定为 AFFECTED：子集必须包含上一轮 FAIL 和 host
# 根据修复路径判定的相关既往 PASS，其余合法 PASS 继承；不再询问用户二选一。guarantee
# 路线最终候选 Seal 前还必须完成 FULL；非保证路线只按自身现行规则决定是否最终 FULL。
formal-gates workflow qa-execution-scope --root <repo> --package-root <package> \
  --run-id <id> --mode <blackbox|whitebox|""> --decision <FULL|AFFECTED> \
  [--cases <id,...>] [--reason '<reason>']

formal-gates workflow prepare-review-wave --root <repo> --package-root <package> \
  --run-id <id>

# 为每个已批准的 QA 用例和每个被选中的已发现门各记录一组。
formal-gates workflow qa-execution --root <repo> --package-root <package> \
  --run-id <id> --dispatch <dispatch-id> \
  --case-result CASE-001 --outcome <PASS|FAIL> --procedure '<actual>' \
  --observation '<observed>' --oracle-result '<comparison>'
formal-gates workflow record-gate --root <repo> --package-root <package> \
  --run-id <id> --gate <gate-id> --dispatch <dispatch-id> \
  --status <PASS|FAIL|RUNTIME_ERROR> \
  --compared '<base>..<current>' \
  [--finding '<message>' --severity <P0|P1|P2|P3> --location '<path:line>']
# --compared 是审查者实际比较的快照对；与指定的基线到当前范围不匹配时结果被丢弃。
# RUNTIME_ERROR 不要求 --compared。
# qa-review 对每个尚未批准 case 逐条核对公开路径、setup/前置、当前环境可执行性、证据
# 留存和 oracle 与已确认需求的一致性；要求真实新 Codex task/fresh reviewer 而当前环境
# 不具备该能力的 case 不得批准。qa-execution 的机械校验只按归一化后的完整字段精确拒绝
# 协议哨兵（如 skipped、authorized PASS 和约定的简短占位值），不使用子串、句式或自然语言
# 语义判断证据真假；真实性由执行代理提示词与主代理核验。未执行或环境不可用整轮记录
# RUNTIME_ERROR，不伪装 PASS。对抗性虚假 Operator 声明服从项目边界。

# RUNTIME_ERROR/REJECTED_INVALID_RESULT 在有限 attempt 内复用原 TaskKey 重派。attempt 耗尽后
# 只允许一次由用户绑定 state revision 的追加 attempt。wave 目标仍无法完成时，用户可明确
# 结束该目标的收集，但不能把它记为 PASS；PENDING/在途目标不可处置。晚到结果仅留审计。
formal-gates workflow resolve-wave-target --root <repo> --package-root <package> \
  --run-id <id> --target <target-key> --state-revision <revision> --reason '<reason>'

# expected-set 全部终结后，主代理必须对每个 finding 的需求、正常失败场景、受影响模块、
# 根因和实际后果五维完成核实，以 NEW/SAME_AS/DISMISSED 逐条裁决并归并 canonical problem。
# 输入 JSON 见下面的字段表；缺项、未收齐目标或未处置错误均拒绝进入返修。
formal-gates workflow adjudicate-wave --root <repo> --package-root <package> \
  --run-id <id> --input <complete-adjudication.json>
```

### adjudicate-wave JSON

| 字段 | 类型、必填条件与取值 |
| --- | --- |
| `waveId` | `workflow show` 的 `activeReviewWaveId`，或 `prepare-review-wave` 返回的 `id`；裁决只接受已收齐、待裁决/待返修/已完成的 wave |
| `stateRevision` | 提交前最近一次 `workflow show` 返回的 `stateIntegrity`；任何状态写入后需重新获取 |
| `findings` | 本次要登记/纠正的数组；每项只属于此 wave，同一次提交不能重复 ID |
| `findings[].findingId` | `workflow show.postReviewFindings` 中该 wave 的稳定 `id`，不是原 gate/action 报告的 raw ID |
| `findings[].decision` | `NEW`、`SAME_AS` 或 `DISMISSED` |
| `findings[].problemId` | `NEW`/`DISMISSED` 不填；`SAME_AS` 必填已存在的 `workflow show.problems` ID；需要归并本轮新建问题时，先提交 NEW，再从公开输出取回 ID |
| `findings[].dimensions` | 对所有三种决策均必填对象，以下五个非空字符串缺一不可：`requirement`、`normalFailurePath`、`affectedModule`、`rootCause`、`actualConsequence` |
| `findings[].verification` | 对所有决策必填；非空 `scope`、`severity`、`causality`、`reproduction`，分别写主代理的范围、等级、因果和实际复现核实；可选 `classification` 不替代前四项 |
| `findings[].references` | 非空字符串数组，写可定位的本次核实证据引用；主代理核实引用真实性 |
| `findings[].reason` | 非空裁决理由，说明五维核实为何支持这次 NEW/归并/驳回 |

`NEW` 创建稳定 problem；`SAME_AS` 保留已有 problem 的 OPEN/RESOLVED 状态，已解决问题不
再次要求返修；`DISMISSED` 只保留 finding 审计。裁决纠错仍用同一入口和最新 state revision，
旧裁决保留；尚未派发的返修计划随纠错更新，计划 ID 不变。

下面是“一条已核实的文案重复问题”对应 NEW 的完整模板，保存为 `adjudication.input.json`。
`${wave_id}`、`${state_revision}`、`${finding_id}` 均由后面的命令绑定；其余证据内容必须按
真实核实结果填写。不能自动把一组 finding 全标 NEW，也不能仅靠示例文字声称完成核实。

<!-- adjudicate-wave-input-example -->
```json
{
  "waveId": "${wave_id}",
  "stateRevision": "${state_revision}",
  "findings": [{
    "findingId": "${finding_id}",
    "decision": "NEW",
    "dimensions": {
      "requirement": "REQ-001",
      "normalFailurePath": "执行文档规定的公开命令。",
      "affectedModule": "公开命令输出",
      "rootCause": "同一结果被重复输出。",
      "actualConsequence": "正常成功路径产生重复文案。"
    },
    "verification": {
      "scope": "已对照 REQ-001 的交付范围核实此输出属于任务。",
      "severity": "核实影响仅限重复文案，保留报告的 P2。",
      "causality": "追踪两个输出分支，确认它们在同一次调用均执行。",
      "reproduction": "在绑定快照执行公开命令并观察到重复输出。"
    },
    "references": ["requirements.md:1", "command-observation.txt:1"],
    "reason": "五个维度指向一个尚未解决的问题，账本中没有可归并的既有 problem。"
  }]
}
```

```bash
formal-gates workflow show --root "$repo" --run-id "$run_id" > show.json
jq '. as $s | .postReviewFindings[] | select(.waveId==$s.activeReviewWaveId)' show.json
# 从上面公开输出选择本次已核实 finding 的完整 ID。
read -r finding_id
jq --arg fid "$finding_id" --slurpfile shown show.json \
  '.waveId=$shown[0].activeReviewWaveId | .stateRevision=$shown[0].stateIntegrity |
   .findings[0].findingId=$fid' adjudication.input.json > adjudication.bound.json
formal-gates workflow adjudicate-wave --root "$repo" --package-root "$package" \
  --run-id "$run_id" --input adjudication.bound.json > adjudication.result.json
jq '{reviewWaves, problems}' adjudication.result.json
```

提交部分 findings 只登记这些条目，不代表整轮裁决完成。继续提交前重新 `show` 获取 revision；
全部 findings 均有有效裁决，且存在 OPEN 的必修问题时，wave 才到 `READY_FOR_REPAIR`，然后用
既有 `prepare-action --action development-worker` 准备唯一统一返修。尚未裁决的门审 P2 或
尚未完成修复/carry 的 P2 不能仅凭 gate PASS 进入 Seal。

### 原 gate P2 补证据改为 P1（AC-022）

沿用 `workflow adjudicate-wave --input <json>`，在要纠正的 finding 项增加可选
`gateP1Evidence` 对象；它只表示 **gate P2→P1**，不接受任意目标等级，也不适用于 QA finding
或 gate P3。省略此对象时，NEW/SAME_AS/DISMISSED 的原有规则不变。不得重新派门审来代替纠正。

前置：当前 active wave 已收齐，绑定当前需求 revision 和当前原生快照；工作树干净，引用证据
已包含在该快照中。返修尚未派发（没有 repairPlan，或其状态为 PLANNED）。原 P2 未裁决时
使用 NEW；已有 OPEN problem 时必须使用 SAME_AS 并保留原 problemId。已驳回、已解决、
旧快照或已派发返修的条目不能从此入口重写；先按既有裁决/下一快照流程处置，而不是换 ID。
纠正只补全当前问题的阻塞证据，不改变已派发任务、重置用例或新增额度。

| 字段 | 必填内容与绑定 |
| --- | --- |
| `findings[].gateP1Evidence.severityRationale` | 独立且非占位的 P1 理由，说明正常使用后果以及为何 P2 不足 |
| `.requirement` | 已确认 REQ ID，与本项 `dimensions.requirement` 相同 |
| `.normalPath` | 正常使用/验收路径，与 `dimensions.normalFailurePath` 相同 |
| `.actualError` | 当前快照中的实际错误，不是预计可能出错 |
| `.deferralImpact` | 延后处理为何会导致开发方向错误或交付失败 |
| `.locations` | 至少一条仓库相对的代码/文档/测试定位，如 `delivery.txt:1`；文件及行号须可解析，不把冻结验收产物作为修复目标 |
| 本项原有 `verification` | `scope`、`severity`、`causality`、`reproduction` 四项均须是主代理对本次 P1 的独立核实；不能拿原 P2 报告代填 |

五维、references 和 reason 仍按上节必填。原消息和 finding ID 从已登记条目继承，不需要也不能
在证据对象中重填。下例为尚未裁决的一条 gate P2；保存为 `gate-p1.input.json`，所有事实文字
必须替换为本次真实核实结果。运行相关 ID/revision 由公开输出取得。

<!-- gate-p1-evidence-input-example -->
```json
{
  "waveId": "${wave_id}",
  "stateRevision": "${state_revision}",
  "findings": [{
    "findingId": "${finding_id}",
    "decision": "NEW",
    "dimensions": {
      "requirement": "REQ-001",
      "normalFailurePath": "执行文档规定的首次公开操作。",
      "affectedModule": "公开命令输出",
      "rootCause": "错误分支返回了不可用的结果。",
      "actualConsequence": "核心验收要求的结果无法取得。"
    },
    "verification": {
      "scope": "主代理对照已确认 REQ-001 核实该操作属于交付范围。",
      "severity": "主代理核实结果不可用且无文档化规避，不只是措辞或有限影响。",
      "causality": "主代理追踪该快照的返回分支，确认其导致结果错误。",
      "reproduction": "主代理在绑定快照独立执行首次公开操作，观察到错误结果并核对预期。"
    },
    "references": ["delivery.txt:1"],
    "reason": "独立复现表明原 P2 的影响被低估，按完整阻塞证据纠正为 P1。",
    "gateP1Evidence": {
      "severityRationale": "正常首次操作返回不可用结果，核心验收被实质违反且无可用规避，因此 P2 不足。",
      "requirement": "REQ-001",
      "normalPath": "执行文档规定的首次公开操作。",
      "actualError": "当前快照返回错误结果，无法得到需求规定的输出。",
      "deferralImpact": "延后处理将交付无法满足核心验收的命令。",
      "locations": ["delivery.txt:1"]
    }
  }]
}
```

```bash
# 工作文件放在仓库外；先完成全部结果收集和本次独立复现。
formal-gates workflow show --root "$repo" --run-id "$run_id" > show.json
jq '. as $s | .postReviewFindings[] |
    select(.waveId==$s.activeReviewWaveId and .sourceKind=="gate" and .severity=="P2")' show.json
read -r finding_id
# 最新裁决已有 problem 时保留它；没有裁决时保持模板的 NEW。
jq --arg fid "$finding_id" --slurpfile shown show.json '
  ($shown[0].postReviewFindings[$fid].adjudications[-1].problemId // "") as $pid |
  .waveId=$shown[0].activeReviewWaveId | .stateRevision=$shown[0].stateIntegrity |
  .findings[0].findingId=$fid |
  if $pid!="" then .findings[0].decision="SAME_AS" | .findings[0].problemId=$pid else . end
  ' gate-p1.input.json > gate-p1.bound.json
formal-gates workflow adjudicate-wave --root "$repo" --package-root "$package" \
  --run-id "$run_id" --input gate-p1.bound.json > gate-p1.result.json
jq --arg fid "$finding_id" '{finding: .postReviewFindings[$fid], gates, reviewWaves}' gate-p1.result.json
```

缺字段、无效引用、需求/正常路径不匹配或过期 revision 返回非零，状态不变；不会消耗 reviewer
retry、创建 `REJECTED_INVALID_RESULT` 派发回执或改变额度。成功返回零：同一 postReviewFinding
变为 P1，原门的有效结果与 wave target 变为 FAIL；新裁决保存 `originalFinding`（完整原 P2）
和 `gateP1Evidence`（完整 P1 与核实记录），同时保留原始 dispatch 结果、finding/problem ID
和已有裁决。未裁决的其他 finding 仍须继续处理；收齐裁决后进入同一统一返修，PLANNED 计划
ID 不变。该 P1 在 `repair-preview.blockingFindings` 中可见、阻塞 Seal，修复后不能使用
`complete-p2-repair` 免门审继承，须按 P1 的正常规则验证修复快照。纠正本身不增加 review wave、
dispatch 或任何额度；未纠正的 P2/P3 仍各按原规则处理。

分片 >= 2 时，保留总任务实例在合并后自动附加合并门与合并 QA 作为其合并后验证，只
跑这两者，不重跑常规门于 base→merged。合并 QA 的跨切片交互用例在各分片开发期间并
行设计/审（`qa-design`/`qa-review`），合并后用 `workflow snapshot` 记录合并标识，
再执行（`qa-execution`）并派发合并门（`prepare-gate --gate merge-gate`）。用例集可
为零，此时留痕注明"切片基本独立、无跨切片交互用例"。合并门审查者有权查看各分片
worktree 变更与主线总变更。合并门与合并 QA 不进正常路线选择列表，custom 的省略不
延伸到合并验证。

## 继承判定、修复授权与 Seal

```bash
# 对于有界的修复，不派发任何代理即可继承此前每一个被选中的 PASS。
formal-gates workflow carry --root <repo> --package-root <package> \
  --run-id <id> --main-agent --main-reason '<reason>'

# 否则，在存在此前通过的门时，准备并记录独立继承判定。
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action carry
formal-gates workflow carry --root <repo> --package-root <package> \
  --run-id <id> --dispatch <dispatch-id> \
  --gate <gate-id> --decision <INHERIT|RERUN> --reason '<reason>'

# 轮次上限用尽后的额外返修先生成完全只读摘要。摘要固定包含当前/剩余轮次、派发次数、
# 运行时长、本轮 QA FAIL、有效 P0/P1、门审与 QA Review P2/P3 的数量/状态，以及预计重跑
# 的构建、项目测试和 QA mode/case；阶段 4 未接入时 engine allowance 明示 UNAVAILABLE。
formal-gates workflow repair-preview --root <repo> --package-root <package> \
  --run-id <id> [--build-target <target> ...] [--test-target <target> ...] \
  [--qa-case <mode>=<id,...> ...]

formal-gates workflow authorize-repair --root <repo> --package-root <package> \
  --run-id <id> --cycles 1 --preview-digest <digest> --state-revision <revision> \
  [--build-target <target> ...] [--test-target <target> ...] \
  [--qa-case <mode>=<id,...> ...]

# preview 后任何状态变化都会使旧 digest/revision 失效，须重新预览。授权不改变普通返修固定
# AFFECTED 的规则，也不能绕过 review-wave 收集、裁决、attempt 或唯一返修约束。

# 纯门审 P2 修复完成最高强度自测后，为提出 P2 的门生成完成/carry 凭据；对应门不复审，
# review-wave 与审查轮次计数不增加。其他门仍按自身影响判定，不能仅因同轮含 P2 而重审。
formal-gates workflow complete-p2-repair --root <repo> --package-root <package> \
  --run-id <id> --wave <wave-id> \
  --build-evidence '<evidence>' --test-evidence '<evidence>' \
  [--affected-case <case-id> ...]

# 只有当每个被选中的结果都通过、或已获得允许的授权之后，才 Seal。FAIL 跳过需要共享
# 审查轮次上限耗尽；用户主动要求跳过时加 --user-requested，可提前跳过并记录为
# SEAL-USER 授权。
# Git run 在基线→当前含 >1 条提交时，seal 自动把该范围压缩为单条提交（git reset
# --soft 基线 + 重新提交，保留最终树），作为 seal 的最后一步 VCS 操作；压缩前要求工作
# 树干净。压缩提交是新的最终 candidate：CLI 不改绑旧 QA/门结果，持久化新 candidate 后
# 要求全部选中 QA mode 与门在其上重验，再次 Seal 才完成封板。单条提交或空范围不操作；
# SVN/P4 不压缩。压缩消息由主代理经 --squash-message 传入。Seal 同时把已批准黑盒用例从隔离工作区落盘合并回主干
# `.gates/results/<run-id>.blackbox-cases.md`（git/svn/p4 统一写主干），作为本 run 的
# 黑盒用例交付物；分片实例不落盘。
formal-gates workflow seal --root <repo> --package-root <package> --run-id <id> \
  [--skip <selected-non-passing-gate> ...] [--user-requested] \
  [--squash-message '<combined commit message>']

# 轻量路线（--route lightweight 启动）的封板：跳过全部验证门（无开发快照、无
# product-review / start-readiness PASS 要求、无路线确认、无拆分决定），封板摘要/记录
# 显式标注「本 run 未经任何验证」（summary.unverified 字段），与完整验证封板区分。
```

修复流程（SKILL 第 8 步的执行机制）：同一 review-wave 收齐并裁决后，全部 QA FAIL、有效
P0/P1 与门审 P2 只生成一份统一返修；同一快照已创建逻辑返修后不得再建第二份。门审 P3
不进入返修，只在 Seal 前集中展示。下面只列其余机制。
- 开始编辑前先冻结当前 VCS 标识。
- 总任务实例把集成发现项分发给各子任务实例，收到各子任务已 Seal 的修复后直接调用
  `workflow snapshot`；其他子任务实例准备并派发开发工作者，完成后冻结新标识并调用
  `workflow snapshot`。
- 主代理通过修复前紧邻快照到当前的比较检查修复内容。
- 派发独立门审/QA 前，先用目标项目自己的构建/测试做便宜健全性检查，明显失败如编译
  不过直接退回修复、不派发子代理；无校验入口则跳过。
- 纯门审 P2 修复派发必须写明"本修复不伴随门复审、要求最高强度自测（完整构建+全量项目
  测试+受影响用例逐项自跑）"，完成后记录 P2 completion/carry 凭据。

继承判定（SKILL 第 8 步的执行机制）：如果修复范围不影响任何此前已通过的被选验证，主
代理可以直接做继承判定（写明理由，继承所有此前 PASS，包括 QA）；否则，对每个此前通过
的被选门单独派发继承判定并重跑 QA。涉及共享 API、公开行为、配置、依赖、跨门职责或因
果不确定的修复，一律用独立继承判定。只重跑标记为 `RERUN` 的门，给它完整的基线到当前
比较。

QA 重跑 scope（host 行为）：普通中间返修固定使用 AFFECTED，不再询问 FULL/AFFECTED。
host 依据修复 diff 定死子集，必须包含上一轮失败用例以及与修复路径相关的既往通过用例；
执行者不得在执行中补跑、改判或改选名单外用例。requirement guarantee 的最终候选在 Seal
前另做 FULL；非保证路线只按自身项目或 route 规则决定是否最终 FULL。

Seal 跳过规则（SKILL 第 9 步的执行机制）：
- PENDING 结果直接阻塞 Seal；RUNTIME_ERROR 需要用户手动跳过；QA FAIL 或 P0/P1 必须先
  修复，直到共享审查轮次上限耗尽或用户主动要求才可授权跳过。
- 路线跳过和 Seal 跳过都会记录在摘要里。
- 已授权的 Seal 跳过在当前快照仍被其他结果阻塞时继续有效，但不延续到后续修复快照。
- 门审 P2 必须已有绑定 problem、修复快照和 completion/carry 凭据；门审 P3 不修复、不阻塞，
  Seal 前一次性展示且不逐条询问。QA Review P2/P3 必须已吸收，不能以 advisory 留到 Seal。
- Seal 的黑盒用例等物化全部成功后持久化 `SEALED` 终态，再写封板摘要并清理临时状态；
  物化失败时持久 state 保持 `ACTIVE` 且不写摘要，摘要或清理中断时保留 `SEALED` 并可由
  重复 Seal 继续。retained master 已显式 waiver/not-guaranteed 时允许 slice guarantee
  sidecar 缺失，但已经存在的有效 sidecar 仍读取并保留其逐项 owner/case/review/execution
  证据及已批准分片用例；正常 active guarantee 仍要求完整 sidecar 集合。

## 成本计量

run 状态的单一 `cost` 投影（legacy 为 `RunState.Cost`，engine 为同一投影在其权威
`state.json`/envelope 中的承载，实现见 `internal/cost/`）在结果记录时为每条独立派发（门或
动作）按其 host 转写文件精确计量 token 用量：run 合计 + 每条派发一项。阶段 3.5b 另在
能够核对 `workflow start` 保存的 owner transcript 时，记录启动基线到 Seal/Abort 终态快照的
主代理用量；它是产生该记录的 run 的独立 owner 条目，不冒充派发，也不参与实时派发止损。
数字只来自 host 转写解析（claude/codex 适配器），无转写、provider 未绑定、区间不可靠或
解析失败时记为 unavailable、不捏造数值；分类为输入缓存命中/未命中与输出，
`totalInputTokens` 只统计输入侧、输出单独记录不计入合计。阶段 4 的 engine source bridge
按 `actionID/correlation` 解析 lifecycle sidecar：source 尚未就绪时返回 `COST_PENDING`，
允许引擎进入现有 Wait；source 明确不可用时记 `UNAVAILABLE`，仍由 request/attempt guard
止损。只有完成 backfill/usage 后才执行 guard，再决定 refill；`show/status/next` 读取同一
投影，不另建账本。

request/attempt 额度只由 engine Controller 从 canonical expected external `TaskKey`、已确认
route/topology、compiled definition 和有限 retry policy 机械计算；外部 agent/host 派发没有
显式策略时使用 canonical `MaxAttempts=2`（首次 + 一次自动重试）。计算 basis digest、结果
和已用 action/Attempt 账目随同一 guard 投影持久化并在签发前重算；不一致时
`BLOCKED_BUG` 且零派发。主代理、host、用户和 AI 都不能提交底层额度数字。额度耗尽后的
Ask 中，“继续”只解锁一个按既有 admission capacity 裁剪的 canonical Ready batch，
“重试”只增加当前逻辑动作的一个 retry slot，不能输入自由数字或预先累积。
expected `TaskKey` 只能来自 closed-world definition 及已确认、持久化的
Batch/route/topology、批准 QA、已选 gate 或已成立 obligation。当前已确认 workflow policy
预授权的 obligation/repair slot 由 Controller 机械加入；其他新增槽位若按现有流程要求用户
确认，先绑定对应 revision/typed Ask。代理输出、receipt 或 resume 不能直接扩大额度。

分片场景下，切片实例的封板不产出独立封板文件，成本投影写入主干 temp run 下的 sidecar
（`.gates/tmp/<主干run-id>/slice-costs/`，gitignored、不入交付 diff），主干（保留总任务
实例）最终封板时把各切片的 dispatch 成本并入主干封板文件（派发条目以
`<切片run-id>/<派发id>` 命名空间并入）后消费清除。child owner 明细仍归属于各自 run；
阶段 5 的 master projection 以 `childRunID` 一次性幂等汇总 child owner，master 自身
`Owner` 不被覆盖。非分片 run 的封板行为不变。

按需重复 `--case`、`--case-result`、发现项和继承判定门分组。当某个代理或原生比较无法
运行时，使用命令的 `--runtime-error` 或 `--status RUNTIME_ERROR --message ...` 形
式；不要伪造语义结果。
