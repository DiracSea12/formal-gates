# 正式流程 CLI 命令映射

流程顺序只由 `SKILL.md` 拥有。这份参考只提供每一步的确切命令形式与步骤级执行机制。
使用已安装的 `formal-gates` 二进制。示例只省略重复的发现项或用例分组。

会话级「不走流程」声明见 `SKILL.md` 开发受理流程；已有正式 run 进行中时声明的行为见
`references/example-run.md` §1。

- [启动与 run 控制](#启动与-run-控制)
- [需求、拆分决定与路线](#需求拆分决定与路线)
- [开发之前](#开发之前)
- [开发与快照](#开发与快照)
- [开发后审查](#开发后审查)
- [继承判定、修复授权与 Seal](#继承判定修复授权与-seal)

## 启动与 run 控制

```bash
# 不指定 --base-snapshot 时，CLI 会把原生的当前标识解析为基线。
# 接手中断的 run 时，--base-snapshot 接受当前 HEAD 的任意祖先（或相等），
# 使已提交的在途工作落在"基线到当前"的审查 diff 内。
formal-gates workflow start --root <repo> --package-root <package> \
  --run-id <id> --flow formal --requirement <requirement-file> \
  [--requirement-artifact <requirement-or-solution-file> ...] \
  --vcs <git|svn|p4> [--base-snapshot <ancestor-or-current-identity>] [--retained-overall]

formal-gates workflow show --root <repo> --run-id <id>
formal-gates workflow resume --root <repo> --package-root <package> --run-id <id>
# 原生 HEAD 已漂移（外部改动）时，显式重绑当前快照并记录原因（需用户确认）。
formal-gates workflow resume --root <repo> --package-root <package> --run-id <id> \
  --adopt-external --reason '<reason>'
formal-gates workflow abort --root <repo> --run-id <id>
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
formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action requirements-clarification --dispatch <dispatch-id> \
  --status <PASS|FAIL|RUNTIME_ERROR>
formal-gates workflow requirement --root <repo> --package-root <package> \
  --run-id <id> --source <requirement-file> \
  [--requirement-artifact <requirement-or-solution-file> ... | \
   --clear-requirement-artifacts] --confirmed
# Resume 报告修订已改变之后，对它的语义影响做分类。
formal-gates workflow requirement --root <repo> --package-root <package> \
  --run-id <id> --meaning <preserved|changed>

# 拆分决定在 Part 2（start-readiness）PASS 后记录，是所有正式 run 的必填留痕；
# 记录后即为绑定点、不重切。no-split 必须带原因留痕；split 需要分片数 >= 2。
formal-gates workflow slicing --root <repo> --package-root <package> \
  --run-id <id> --decision <split|no-split> [--count <n>] \
  [--slice '<slice-definition>' ...] [--parallel '<parallel-suggestion>'] \
  [--note '<reason>'] [--master <retained-overall-master-run-id>]
# 拆分建议对所有正式 run 必填呈现并留痕，含：拆分理由、如何拆、哪些子任务可并行、
# 以及改拆后果说明（若改拆，黑盒 QA 设计按新拆分拓扑展开、已覆盖用例复用）；仅高置信
# 要拆时需用户确认拆分方案。分片场景下整体级产品审/技术审足够，切片继承整体审查结果、
# 不单独重跑（切片实例在记录拆分决定时继承整体级 product-review/start-readiness）。
# 用户已拍板（settled）的发现项清单：注入下一次 product-review / start-readiness
# 派发，审查者不再重提（需求修订改变前提时例外）。
formal-gates workflow settle-findings --root <repo> --package-root <package> \
  --run-id <id> --action <product-review|start-readiness> \
  --finding '<settled-message>' ...

# 路线在拆分决定之后确认（时序见 SKILL.md 正式流程开头）；full = 黑盒 QA + 白盒 QA +
# 全部已发现门；custom 可任意组合黑盒 QA、白盒 QA 与各门，至少选一项。合并门/合并 QA
# 不进正常选择列表。
formal-gates workflow route-candidates --root <repo> --package-root <package> \
  --run-id <id>
formal-gates workflow route --root <repo> --package-root <package> \
  --run-id <id> --mode <full|custom> [--gate <gate-id> ...]
formal-gates workflow route-add --root <repo> --package-root <package> \
  --run-id <id> --gate <gate-id>
```

需求修订（SKILL 第 2 步的执行机制；三分支决策本体见 SKILL 第 2 步）：
- 开发开始后做 meaning-preserved 需求重绑时，CLI 要求用户确认信号：`workflow
  requirement` 必须**同时传 `--confirmed`**，否则被拒。

拆分与路线细节（SKILL 第 3 步的执行机制）：
- 路线跳过授权：custom 未选中的部分获得路线跳过授权（`ROUTE` 来源），Seal 期间不会
  补做；custom 的省略不延伸到合并验证。
- 后续新增需要用户明确指示；开发开始之后不能再加入 QA。
- 分片 >= 2 时保留总任务实例自动附加合并门与合并 QA，不涉常规路线选择。

重复 prepare（问题 6）：对同一 action/gate 重复 prepare 会生成 attempt+1 的新派发，
旧的 OPEN/CLAIMED 派发静默标 STALE（源快照失效），以当前返回的派发 ID 为准。

## 开发之前

开发前检查分两段：先派发 Part 1 产品审（`product-review`），全部通过后再进入
Part 2。Part 2 双速调度：高置信要拆 → 呈现拆分建议、用户确认后设置分片，然后确认
路线（逐切片）；非高置信要拆（高置信不拆或不确定）→ 快速路径，黑盒 QA 设计可与
`start-readiness` 并行开始，"建议不拆（原因）"必填留痕，按单一 run 进行。QA 模式拆
为黑盒（LIVE 行为执行）与白盒（STATIC 结构测试），各自可选、均由独立代理设计并经
review 批准。这个循环不消耗开发后的审查轮次。

```bash
# Part 1 产品审：承接需求细节澄清，审已实例化的需求文档，只评产品/策划层面。
# 发现项分级 P0/P1/P2；用户已拍板的发现项不再重提（CLI 注入已拍板清单）。
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action product-review
# 每准备一个独立派发的动作或门之后，都重复这条认领命令。
formal-gates workflow claim-dispatch --root <repo> --package-root <package> \
  --run-id <id> --dispatch <dispatch-id> --reviewer <host-agent-id>
formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action product-review --dispatch <dispatch-id> \
  --status <PASS|FAIL|RUNTIME_ERROR> \
  [--finding '<message>' --severity <P0|P1|P2>]
# 产品审的 FAIL 发现项是候选输入，由用户逐项处置：用户接受（认为不是问题或不需要修改）
# 则该发现项作废、不阻塞、不改需求；用户认同问题（认为需要修订的真问题）则按指示修订
# 需求/方案。是否重新产品审：没审出问题 → 直接通过；用户认同的问题里不存在 P0/P1 →
# 修订后不再重新审，直接进入下一步；用户认同的问题里存在 P0/P1 → 修订后重新审。FAIL
# 不构成终态。处置由主代理执行，reviewer 子代理只报告分级发现项。

# Part 2 技术审：承接技术方案选择与对齐，发现项同样分级 P0/P1/P2。
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action start-readiness
formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action start-readiness --dispatch <dispatch-id> \
  --status <PASS|FAIL|RUNTIME_ERROR> \
  [--finding '<message>' --severity <P0|P1|P2>]
# 技术审的 FAIL 发现项同样是候选输入，由用户逐项处置：用户接受 → 该发现项作废、不
# 阻塞、不改需求/方案；用户认同问题 → 按指示修订需求/方案。是否重新技术审：没审出问题
# → 直接通过；用户认同的问题里不存在 P0/P1 → 修订后不再重新审，直接进入下一步；用户
# 认同的问题里存在 P0/P1 → 修订后重新审。FAIL 不构成终态。处置由主代理执行，reviewer
# 子代理只报告分级发现项。

# start-readiness PASS 后记录拆分决定，再确认路线（见上节）。

formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action qa-design
formal-gates workflow qa-design --root <repo> --package-root <package> --run-id <id> \
  --dispatch <dispatch-id> --case '<description>' --kind <STATIC|LIVE> \
  --procedure '<public procedure>' --oracle '<expected result>'
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action qa-review
formal-gates workflow qa-review --root <repo> --package-root <package> \
  --run-id <id> --dispatch <dispatch-id> \
  --case CASE-001 --outcome <PASS|FAIL> [--reason '<required for FAIL>'] \
  [--finding '<set-level finding>' --location '<path:line>']
```

双速调度细节（SKILL 第 4 步的执行机制）：黑盒 QA 用例（LIVE 行为执行）设计后派发独立
`qa-review` 批准；白盒 QA（STATIC 结构测试）开发后由独立代理读实现设计并执行。

## 开发与快照

```bash
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action development-worker

# 记录开发后或修复后的不可变标识。
formal-gates workflow snapshot --root <repo> --package-root <package> \
  --run-id <id> --dispatch <development-or-repair-dispatch-id>
```

开发派发认领（问题 2a）：开发/修复派发是 reviewer-required，认领要求原生 HEAD 与当前
快照一致；worker 一旦提交 HEAD 即前进，所以 `workflow claim-dispatch` 对
`development-worker` 派发放宽——当前原生 HEAD 是派发源快照的后代（或相等）即允许认领
（覆盖 worker 已提交的情形），随后 `workflow snapshot` 成功。其余派发（审查、QA 等）
检查不变。放宽后的检查不验证 HEAD 是否由 worker 产生：开发期间无关外部提交落地会被
**静默吸收**进开发快照（用户已接受此行为）。

## 开发后审查

```bash
# 并行准备 QA 执行和每一个门。开发后阶段并行：黑盒 QA 执行、白盒 QA 与各门。
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action qa-execution
formal-gates workflow prepare-gate --root <repo> --package-root <package> \
  --run-id <id> --gate <gate-id>

# 为每个已批准的 QA 用例和每个被选中的已发现门各记录一组。
formal-gates workflow qa-execution --root <repo> --package-root <package> \
  --run-id <id> --dispatch <dispatch-id> \
  --case-result CASE-001 --outcome <PASS|FAIL> --procedure '<actual>' \
  --observation '<observed>' --oracle-result '<comparison>'
formal-gates workflow record-gate --root <repo> --package-root <package> \
  --run-id <id> --gate <gate-id> --dispatch <dispatch-id> \
  --status <PASS|FAIL|RUNTIME_ERROR> \
  --compared '<base>..<current>' \
  [--finding '<message>' --severity <P0|P1|P2> --location '<path:line>']
# --compared 是审查者实际比较的快照对；与指定的基线到当前范围不匹配时结果被丢弃。
# RUNTIME_ERROR 不要求 --compared。
```

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

formal-gates workflow authorize-repair --root <repo> --package-root <package> \
  --run-id <id> --cycles 1

# 只有当每个被选中的结果都通过、或已获得允许的授权之后，才 Seal。FAIL 跳过需要共享
# 审查轮次上限耗尽；用户主动要求跳过时加 --user-requested，可提前跳过并记录为
# SEAL-USER 授权。
formal-gates workflow seal --root <repo> --package-root <package> --run-id <id> \
  [--skip <selected-non-passing-gate> ...] [--user-requested]
```

修复流程（SKILL 第 8 步的执行机制）：触发条件（QA FAIL 或 P0/P1 → 整轮退回修复、P2
一并处理）见 SKILL.md 第 8 步；下面只列命令形式与其余机制。
- 开始编辑前先冻结当前 VCS 标识。
- 总任务实例把集成发现项分发给各子任务实例，收到各子任务已 Seal 的修复后直接调用
  `workflow snapshot`；其他子任务实例准备并派发开发工作者，完成后冻结新标识并调用
  `workflow snapshot`。
- 主代理通过修复前紧邻快照到当前的比较检查修复内容。
- 派发独立门审/QA 前，先用目标项目自己的构建/测试做便宜健全性检查，明显失败如编译
  不过直接退回修复、不派发子代理；无校验入口则跳过。

继承判定（SKILL 第 8 步的执行机制）：如果修复范围不影响任何此前已通过的被选验证，主
代理可以直接做继承判定（写明理由，继承所有此前 PASS，包括 QA）；否则，对每个此前通过
的被选门单独派发继承判定并重跑 QA。涉及共享 API、公开行为、配置、依赖、跨门职责或因
果不确定的修复，一律用独立继承判定。只重跑标记为 `RERUN` 的门，给它完整的基线到当前
比较。

Seal 跳过规则（SKILL 第 9 步的执行机制）：
- PENDING 结果直接阻塞 Seal；RUNTIME_ERROR 需要用户手动跳过；QA FAIL 或 P0/P1 必须先
  修复，直到共享审查轮次上限耗尽或用户主动要求才可授权跳过。
- 路线跳过和 Seal 跳过都会记录在摘要里。
- 已授权的 Seal 跳过在当前快照仍被其他结果阻塞时继续有效，但不延续到后续修复快照。
- 只有 P2 的 PASS 建议保持可见，不阻塞 Seal。

按需重复 `--case`、`--case-result`、发现项和继承判定门分组。当某个代理或原生比较无法
运行时，使用命令的 `--runtime-error` 或 `--status RUNTIME_ERROR --message ...` 形
式；不要伪造语义结果。
