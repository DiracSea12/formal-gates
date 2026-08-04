# 正式流程 CLI 命令映射

流程顺序只由 `SKILL.md` 拥有。这份参考只提供每一步的确切命令形式。使用已安装的
`formal-gates` 二进制。示例只省略重复的发现项或用例分组。

- [启动与 run 控制](#启动与-run-控制)
- [需求与路线](#需求与路线)
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
  [--note '<reason>']
# 用户已拍板（settled）的发现项清单：注入下一次 product-review / start-readiness
# 派发，审查者不再重提（需求修订改变前提时例外）。
formal-gates workflow settle-findings --root <repo> --package-root <package> \
  --run-id <id> --action <product-review|start-readiness> \
  --finding '<settled-message>' ...

# 路线在拆分决定之后确认：单一 run 确认一次；拆后逐切片各确认一次（主代理给统一
# 默认推荐、用户拍板）。full = 黑盒 QA + 白盒 QA + 全部已发现门；custom 可任意组
# 合黑盒 QA、白盒 QA 与各门，至少选一项。合并门/合并 QA 不进正常选择列表。
formal-gates workflow route-candidates --root <repo> --package-root <package> \
  --run-id <id>
formal-gates workflow route --root <repo> --package-root <package> \
  --run-id <id> --mode <full|custom> [--gate <gate-id> ...]
formal-gates workflow route-add --root <repo> --package-root <package> \
  --run-id <id> --gate <gate-id>
```

## 开发之前

开发前检查分两段：先派发 Part 1 产品审（`product-review`），全部通过后再进入
Part 2。Part 2 双速调度：高置信要拆 → 呈现拆分建议、用户确认后设置分片，然后确认
路线（逐切片）；非高置信要拆（高置信不拆或不确定）→ 快速路径，黑盒 QA 设计可与
`start-readiness` 并行开始，"建议不拆（原因）"必填留痕，按单一 run 进行。路线确认
在拆分决定之后；QA 模式拆为黑盒（LIVE 行为执行）与白盒（STATIC 结构测试），各自可
选、均由独立代理设计并经 review 批准。

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
# 产品审的 FAIL 发现项是候选输入，由用户逐项拍板：接受则重新派发并记录 PASS，
# 未接受则按用户指示修订需求/方案后重审。仅 P2 时修订后不再复审，直接进入下一步；
# 存在 P0/P1 时修订后重新审。FAIL 不构成终态。

# Part 2 技术审：承接技术方案选择与对齐，发现项同样分级 P0/P1/P2。
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action start-readiness
formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action start-readiness --dispatch <dispatch-id> \
  --status <PASS|FAIL|RUNTIME_ERROR> \
  [--finding '<message>' --severity <P0|P1|P2>]

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

## 开发与快照

```bash
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action development-worker

# 记录开发后或修复后的不可变标识。
formal-gates workflow snapshot --root <repo> --package-root <package> \
  --run-id <id> --dispatch <development-or-repair-dispatch-id>
```

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
为零，此时留痕注明"切片基本独立、无跨切片交互用例"。合并门与合并 QA 不进正常路线
选择列表，custom 的省略不延伸到合并验证。

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

按需重复 `--case`、`--case-result`、发现项和继承判定门分组。当某个代理或原生比较无法
运行时，使用命令的 `--runtime-error` 或 `--status RUNTIME_ERROR --message ...` 形
式；不要伪造语义结果。
