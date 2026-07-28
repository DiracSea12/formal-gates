---
name: formal-gates
description: 用于每一个创建、编辑、移动或删除项目内容的请求，无论仓库、产品、文件类型或预估规模；也用于用户明确要求的 formal-gates 对齐、审查、发布、Seal、安装或诊断。只读的提问、解释、诊断和 review 不进入自动修改 intake，除非用户要求正式执行。
---

# Formal Gates

运行一条轻量、以 VCS 为依托的流程。CLI 只保存一个临时状态文件；host 启动各个独立
代理；仓库的原生 VCS 拥有所有快照和 diff。

## 范围边界

只保证文档化的正常使用和常见操作失误。除非用户明确要求加固，对抗性输入、恶意编
辑、手工改写状态、权限失败、不可变文件失败、不受支持的平台，以及其他人为构造的
流程违规，都仅作建议，不能阻塞 PASS，也不能引出新机制、兼容路径或测试。

每个阻塞性发现项都必须能从文档化的公开入口出发，用正常用户操作或一个常见失误复
现。确定性规则在拥有它的最低层测试；不要添加主要目的是重测另一个测试或校验器的
测试。

## 通用修改 intake

对每一个创建、编辑、移动或删除项目内容的请求，启用这道 intake。在 formal-gates
源码仓库和其他任何项目中都适用同一套规则。不要求任何修改的只读提问、解释、诊断
和 review 不进入自动 intake，除非用户明确要求正式执行。

在任何文件写入或实现派发之前，主代理必须：

1. 检视工作区，确定它能直接判定的事实。
2. 澄清用户想要的结果，以及每一个会实质改变公开行为、验收或架构的技术选择。用
   日常语言一次只问一个有实质影响的决策，次要实现细节自行决定。
3. 呈现完整整合后的需求与技术方案，然后等待用户明确确认。
4. 评估总规模、耦合、风险和验证复杂度；解释推荐理由；查询
   `formal-gates package route-candidates --root <package>`；并且只呈现一个合并
   后的选择：
   - lightweight 执行，不创建正式 run；
   - `full`，即 QA 加上全部被发现的门；或
   - `custom`，展示 QA 和完整的已发现门列表供选择子集。

当之前不需要任何澄清问题时，完整摘要确认和路线选择可以合并为用户的一次回复。路
线只问一次。后续出现新需求时，暂停相关写入以进行澄清、刷新完整摘要并取得明确确
认，但保留已选路线，除非用户明确要求重新考虑。

lightweight 执行不创建流程状态，也不创建正式任务切片。它可以省略 PRD、OpenSpec、
设计和任务计划产物，但用户可以把其中任何一项作为普通交付物来要求，而不必选择正式
审查门。

选择 `full` 或 `custom` 时，在开发之前把完整的已确认需求与技术方案，持久化到环境
中任何一种稳定的文档格式里。不要求特定格式或文档插件。然后运行下面的正式流程。这
两条路线都始终运行启动就绪度。

当已确认的正式请求整体无法作为一个连贯、有界的单元来实现和验证时，按依赖、职责归
属、风险和验证面拆分它。在任何切片开发之前，用 `workflow start` 传入
`--retained-overall`，在完整请求的原始基线上启动并保留一个总体正式 run，带上完整
需求和已选路线。给每个切片单独的正式 run 和原生 VCS 工作树；容量允许时并发运行相
互独立的切片，并在依赖边上等待。合并已 Seal 的切片分支并解决冲突之后，用
`workflow snapshot` 把合并后的标识记录为保留总体 run 中已完成的开发，然后从原始基
线到合并快照执行它的集成 QA 与各个门。不要创建第二个总体 run、不要替换它的基线、
不要为总体准备或派发开发工作者，也不要在合并之后重复澄清或路线选择。集成阶段的发
现项回到各自所属的切片 run。那些切片修复被 Seal 并合并之后，在保留总体 run 中直接
用 `workflow snapshot` 记录新的合并标识。

## 单一正式流程

`SKILL.md` 是这个顺序的唯一拥有者。intake 已经选定了 `full` 或 `custom`；被省略的
审查阶段不会在 Seal 期间补做：

1. **启动。** 选择一个外部 VCS，冻结它的原生基线标识，并运行
   `formal-gates workflow start`。登记主需求，以及每一份额外的需求或方案文档。不
   支持无 VCS 的正式 run。
2. **需求。** 主代理按 `requirements-clarification` 执行，把已确认的完整结果与方
   案记录为 PASS，然后用 `workflow requirement --confirmed` 绑定确切的持久版本。
   在一次改变含义的修订之后，QA 设计会收到此前的每一个用例及其审查标记，然后返回
   修订后的完整候选用例集。对未受影响需求而言完全保留的通过用例仍然处于已批准状
   态；新增或已改变的用例变为待定。
   对任何已改变修订的分类，同时会绑定包含它的当前不可变实时 VCS 标识。
3. **绑定已选路线。** 读取 `workflow route-candidates` 以核对与该 run 绑定的目录，
   并记录已经选好的 `full` 或 `custom` 路线，不再重复询问。custom 省略的部分获得
   路线跳过授权。后续新增需要用户明确指示；开发开始之后不能再加入 QA。
4. **正式开发之前。** 运行 `start-readiness`。只有在 QA 被选中时，运行盲态
   `qa-design`。这些被选中的动作可以并行运行。只通过 `workflow qa-design` 记录完
   整的候选用例；每个完整用例集都包含 STATIC 直接归属者检查和 LIVE 公开入口执行。
   然后派发独立的 `qa-review`，用新的零上下文审查者身份认领它的派发，并为每个待定
   用例记录一个决策。在设计返工过程中，未改变的通过用例仍然处于已批准状态。PASS
   批准这些用例并解锁开发。FAIL 退回 QA 设计；RUNTIME_ERROR 重试 QA 审查而不重新
   打开设计。这个循环不消耗开发后的审查轮次。
5. **开发。** 准备 `development-worker`；这一准备动作会记录开发已开始，并冻结开发
   前的结果和 QA 的后期路线变更。
   正常中断之后，重新准备同一个处于 PREPARED 状态的开发或修复任务，会重新组装它，
   而不移动已记录的开始边界。
   派发一个与正式审查者相互独立的工作者。不要把 QA 用例发给它。该工作者只实现已确
   认范围，在继续之前把每一条新的或此前未跟踪的交付路径显式添加到指定 VCS，并在返
   回前核对完整的基线到当前原生比较。
6. **固定当前快照。** 用原生 VCS 为已完成的实现创建一个不可变标识，并用
   `workflow snapshot` 记录它。绝不把可变的工作树状态送去审查。
7. **开发后审查。** 派发被选中的 QA 执行与已发现门的每一种组合。被选中的独立动作
   可以在同一个并行轮次中运行。QA 收到已批准的用例。每个被选中的门都收到完整的基
   线到当前 VCS 路线，并独立检视那份 diff。代理从不写入流程状态；编排者校验并记录
   返回的语义结果。已记录的语义 PASS 或 FAIL 对其快照具有权威性；只有 PENDING 和
   RUNTIME_ERROR 可以重试。
8. **修复。** 出现 QA FAIL 或 P0/P1 门发现项的轮次退回修复，并把该轮次的每个 P2
   发现项一并纳入。编辑之前，冻结当前 VCS 标识。对于保留的总体 run，把集成发现项
   退回所属切片 run，合并它们已 Seal 的修复，并直接调用 `workflow snapshot`，不为
   总体准备开发工作者。对于其他每一个 run，准备并派发开发工作者，然后冻结它的新标
   识并调用 `workflow snapshot`。主代理检视修复前紧邻快照到当前的原生比较。只有当
   它能把修复限定到不会影响任何此前通过的被选验证时，才可以使用主代理 Carry，以非
   空理由继承包括 QA 在内的每一个此前 PASS。否则，为每一个此前通过的被选已发现门
   派发独立 Carry，并重跑 QA。共享 API、公开行为、配置、依赖、跨门职责归属，以及
   不确定的因果链，一律使用独立 Carry。只重跑被标记为 `RERUN` 的门，并给每个重跑
   的门完整的基线到当前比较。QA 和每个被选中的门共享三个已完成审查轮次，其中开发
   后的首个完整轮次和每个完整的修复后轮次各计一次。不完整或运行时错误的轮次不计入。
   自动上限用尽之后，在每一次额外修复之前呈现当前阻塞项并征询用户。每一次明确授权
   只增加恰好一个下一轮修复/审查；它不能预先授权后续轮次。如果那一轮仍然阻塞，呈现
   它的问题，并在再次修复之前再次征询。
9. **Seal。** 在汇总之前和之后，各确认一次实时原生 VCS 标识。被选中项中存在
   PENDING 会阻塞 Seal。RUNTIME_ERROR 需要用户明确跳过；QA FAIL 或 P0/P1 必须先修
   复到共享上限用尽，才可以授权跳过。路线跳过和 Seal 跳过都会保留在摘要中。已命名
   的 Seal 授权在该次尝试仍被另一个结果阻塞时继续有效，只适用于当前快照，并在后续
   修复快照上清除。仅有 P2 的 PASS 建议保持可见，但不阻塞 Seal。

用 `workflow show` 检视一个 run，中断之后用 `workflow resume`。被中断的派发保持
`PENDING`；保留已完成的结果。用 `workflow abort` 保留一份中止摘要，并移除该 run 的
临时目录。

## CLI 命令映射

使用已安装的 `formal-gates` 二进制。下面的示例只省略了重复的发现项或用例分组。

```bash
# 启动。CLI 会把原生的当前标识解析为基线。
formal-gates workflow start --root <repo> --package-root <package> \
  --run-id <id> --flow formal --requirement <requirement-file> \
  [--requirement-artifact <requirement-or-solution-file> ...] \
  --vcs <git|svn|p4> [--base-snapshot <identity-to-verify>] [--retained-overall]

formal-gates workflow show --root <repo> --run-id <id>
formal-gates workflow resume --root <repo> --package-root <package> --run-id <id>
formal-gates workflow abort --root <repo> --run-id <id>

# 准备并记录需求/就绪度动作。
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
formal-gates workflow route-candidates --root <repo> --package-root <package> \
  --run-id <id>
formal-gates workflow route --root <repo> --package-root <package> \
  --run-id <id> --mode <full|custom> [--gate <gate-id> ...]
formal-gates workflow route-add --root <repo> --package-root <package> \
  --run-id <id> --gate <gate-id>
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action start-readiness
# 每准备一个独立派发的动作或门之后，都重复这条认领命令。
formal-gates workflow claim-dispatch --root <repo> --package-root <package> \
  --run-id <id> --dispatch <dispatch-id> --reviewer <host-agent-id>
formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action start-readiness --dispatch <dispatch-id> \
  --status <PASS|FAIL|RUNTIME_ERROR>

# QA 设计、独立 QA 审查，然后是开发工作者。
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action qa-design
formal-gates workflow qa-design --root <repo> --package-root <package> --run-id <id> \
  --dispatch <dispatch-id> --case '<description>' --kind <STATIC|LIVE> \
  --procedure '<public procedure>' --oracle '<expected result>'
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action qa-review
formal-gates workflow claim-dispatch --root <repo> --package-root <package> \
  --run-id <id> --dispatch <dispatch-id> --reviewer <reviewer-or-session-id>
formal-gates workflow qa-review --root <repo> --package-root <package> \
  --run-id <id> --dispatch <dispatch-id> \
  --case CASE-001 --outcome <PASS|FAIL> [--reason '<required for FAIL>'] \
  [--finding '<set-level finding>' --location '<path:line>']
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action development-worker

# 记录开发后或修复后的不可变标识。
formal-gates workflow snapshot --root <repo> --package-root <package> \
  --run-id <id> --dispatch <development-or-repair-dispatch-id>

# 并行准备 QA 执行和每一个门。
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action qa-execution
formal-gates workflow prepare-gate --root <repo> --package-root <package> \
  --run-id <id> --gate <gate-id>

# 为每个已批准的 QA 用例和每个被选中的已发现门各记录一组。
formal-gates workflow qa-execution --root <repo> --package-root <package> \
  --run-id <id> --dispatch <dispatch-id> \
  --case-result CASE-001 --outcome <PASS|FAIL> --procedure '<actual>' \
  --observation '<observed>' --oracle-result '<comparison>'
formal-gates workflow claim-dispatch --root <repo> --package-root <package> \
  --run-id <id> --dispatch <dispatch-id> --reviewer <reviewer-or-session-id>
formal-gates workflow record-gate --root <repo> --package-root <package> \
  --run-id <id> --gate <gate-id> --dispatch <dispatch-id> \
  --status <PASS|FAIL|RUNTIME_ERROR> \
  [--finding '<message>' --severity <P0|P1|P2> --location '<path:line>']

# 对于有界的修复，不派发任何代理即可继承此前每一个被选中的 PASS。
formal-gates workflow carry --root <repo> --package-root <package> \
  --run-id <id> --main-agent --main-reason '<reason>'

# 否则，在存在此前通过的门时，准备并记录独立 Carry。
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action carry
formal-gates workflow carry --root <repo> --package-root <package> \
  --run-id <id> --dispatch <dispatch-id> \
  --gate <gate-id> --decision <INHERIT|RERUN> --reason '<reason>'

formal-gates workflow authorize-repair --root <repo> --package-root <package> \
  --run-id <id> --cycles 1

# 只有当每个被选中的结果都通过、或已获得允许的授权之后，才 Seal。
formal-gates workflow seal --root <repo> --package-root <package> --run-id <id> \
  [--skip <selected-non-passing-gate> ...]
```

按需重复 `--case`、`--case-result`、发现项和 Carry 门分组。当某个代理或原生比较无法
运行时，使用命令的 `--runtime-error` 或 `--status RUNTIME_ERROR --message ...` 形
式；不要伪造语义结果。

## 独立派发

主代理自己执行已准备好的需求澄清任务，并直接记录有理由的主代理 Carry，无需准备
Carry 提示词。对于独立派发的动作或门，调用 `workflow prepare-action` 或
`workflow prepare-gate`，然后把 stdout 作为完整任务，经 host 原生的独立代理通道发
出。开发任务只发给那个独立的工作者。不要附加聊天历史、发现项、修复说明、其他审查
者的结果、预期判定或关注点指令。

host 启动之后，用 `workflow claim-dispatch` 把每一个独立派发的动作和门绑定到 host
身份。对应的结果命令，或开发与修复所用的 `workflow snapshot --dispatch`，会在改变
流程状态之前检查生命周期模块。保留总体 run 的集成快照是主代理操作，省略
`--dispatch`。

对 QA 审查和每一个已发现门，创建一个新的零上下文审查者，并在记录时原样传回已准备
的派发 ID。绝不在同一个 run 的任何其他位置复用某个 host 代理身份，包括重试和修复轮
次。被中断的已认领身份保持占用。

QA 审查只收到已确认需求和由 CLI 组装的完整候选用例集。它不得检视生产实现、实现
diff、测试、开发者解释、后续结果或另一位审查者的结论。

每个门任务都在内存中组装，其构成恰好是：一份共享的 `prompts/reviewer-base.md`、恰
好一个被选中的 `gates/<gate-id>.md`、当前需求路线、原生 VCS 路线，以及结果契约。不
要绕过 CLI 组装器，也不要直接调用某个门提示词。提示词文件缺失或无效属于运行时错误，
并中止派发。

每一份返回的独立结果都只是候选输入，直到主代理对照完整的已确认需求、文档化的正常使
用边界和结果契约完成校验。在把那次校验显式化之前，主代理不记录也不呈现任何语义结果。
结果契约是以下三者之一：

- `PASS`，没有发现项，或只有 P2 发现项；
- `FAIL`，至少一个 P0/P1 发现项，可附带 P2 发现项；或
- `RUNTIME_ERROR`，当派发、上下文、VCS 比较或结果解析失败时。

运行时错误不是审查发现项。它们保持可见，并需要重试或用户明确的跳过授权。审查者必须
完成每一项安全的范围内检查。发现缺陷之后，它在整个当前改动中扫描同一缺陷模式，并沿
着同一条因果、行为、数据、职责归属或依赖链推进，使一份结果报告出完整的相关集合，而
不是一次只报一个症状。建议性意见不阻塞 PASS。

绝不催促审查者。状态询问只能询问进度，并且必须说明要继续，直到所有分配的检查全部完
成。

## 结果校验与修复上限

在记录或呈现任何语义 PASS 或 FAIL 之前，主代理把它的完整需求校验、正常使用边界校验
和结果契约校验显式化。对于 FAIL 或阻塞项，它还要检查保留的流程状态、范围、严重度、
因果主张和所引证据，并且必须从文档化的公开入口出发，用正常用户操作或一个常见失误端
到端复现该失败。如果任何前提、复现或证据检查不成立，丢弃该发现项：不记录它、不把它
作为阻塞项呈现，也不因它改变需求或实现。这是编排层面的校验，不是第二个判定、第二道
门、第二个代理或第二个证据库。

校验之后，编排者把同一根因只归为一组，并在一个阻塞轮次中一并修复所有 P0、P1 和 P2
发现项。QA 和所有被选中的门，在每次交付尝试中最多共享三个已完成的自动审查轮次。开
发后的首个轮次和每个修复后轮次，在所有必需验证完成之后各计一次，与语义结果无关。每
个快照最多计一次。派发失败、中断、缺少验证、PENDING 和 RUNTIME_ERROR 都不计入。用尽
之后，在每一次额外修复之前，呈现当前阻塞项并询问用户是否继续。一次明确授权只允许恰
好一个下一轮修复/审查，不能复用或为后续轮次累积。如果那一轮仍然阻塞，重复解释与授权
这一步。用户也可以改为授权已命名的 Seal 跳过，或授权一次需求变更。

## 门文件

独立的开发后审查门，就是已安装包 `gates/` 目录下有效的直接 Markdown 子文件。文件名
主干即门 ID；文件按字典序排序。`qa` 保留给内置 QA 流程，因此 `gates/qa.md` 无效。添
加任何其他有效文件并重新安装即新增一道门。删除它并重新安装即移除该门。不要添加门注
册表、manifest、front matter、权重、依赖图或项目级覆盖层。

QA 设计、QA 审查、QA 执行、需求澄清、启动就绪度、Carry、开发和 Seal 都是流程动作，
不是门文件。

## 状态与 VCS

唯一的临时流程状态是 `.gates/tmp/<run-id>/state.json`。不要手工编辑它。CLI 拥有原子
更新、提示词/目录修订、动作与门状态、已批准用例，以及当前快照绑定。它不保存 diff 字
节、项目文件内容、证据图、分层 run 产物，也不保存第二套版本控制模型。

CLI 通过 run 启动时选定的 Git、SVN 或 P4 原生命令解析并验证标识。host 和各代理直接
使用同一个 VCS 做比较。基线到当前的比较就是完整交付 diff。修复前紧邻快照到当前的比
较是修复 diff，只由 Carry 使用。如果选定的 VCS 无法复现一个不可变比较，记录
`RUNTIME_ERROR`；不要猜测、不要静默切换 VCS，也不要实现回退 diff 引擎。参见
`references/vcs-snapshots.md`。

## 安装与维护

只在处理安装、host hook、canary、打包或发布检查时，才阅读
`references/install-and-hooks.md`。只在维护本仓库时，才阅读
`references/local-validation.md`。hook 配置不构成 host 已执行它的证明；只有在同一
host 上完成实机 canary 之后，才可以声明 hook 阻塞生效。

绝不根据聊天内容、开发者自测、主代理自审、hook 配置或部分结果来声明正式 PASS。如果
独立代理不可用，把那项审查报告为受阻，而不是自己给出 PASS。
