# Blackbox QA cases (run: 3-5a-fix-r2)

Derived from the run state at qa-design record time. Review these cases against the current confirmed requirement.

Artifact correction: the original review marked all 40 definitions PASS, but later blackbox execution exposed
case-admission defects. CASE-001, CASE-002, CASE-008, CASE-011, CASE-032 and CASE-036 are corrected below
to FAIL for case quality/executability reasons; these are not additional product defects. CASE-028 remains an
approved case and exposed the real Seal defect. No new execution evidence is asserted by this file.

CASE-001
mode: blackbox
description: [[AC-001, AC-007]] 同一 UTF-8 Markdown 需求案可同时保留普通正文与多个合法 `REQ/AC`，且产品审、技术审和 QA design 消费同一确认 revision 的完整投影。
procedure: 在临时 Git 项目 `P` 中创建含普通背景、方案、`REQ-001/REQ-003` 和 `AC-001/AC-007/AC-009` 的 `requirements.md`。依次执行 `workflow start --split no`、准备并记录 `requirements-clarification` PASS、`workflow requirement --source requirements.md --confirmed --activate-guarantee`。准备 `product-review` 并保存规范提示词，认领后按提示词中全部 pending item 逐项记录 PASS；准备并保存 `start-readiness` 提示词，认领并记录 PASS；执行 `workflow slicing --decision no-split --note "单一交付"`、`workflow route --mode custom --gate blackbox`。从 run 的 base snapshot 创建 Git QA worktree `Q`，把已确认需求文件原样注入 `Q`，执行 `workflow qa-worktree --worktree "$Q"`，再准备 `qa-design --mode blackbox` 并保存提示词。保存确认输出、三份提示词、文件 SHA-256 和 `workflow show`。
oracle: 全部命令退出 0；确认 revision 等于文件摘要；三份提示词引用相同路径与 revision，投影均精确包含两个 REQ、三个 AC 及正确父子关系；普通正文原样可见且不产生结构错误。
review status: FAIL
review correction: oracle incorrectly requires full projection text to be duplicated in every prompt instead of accepting the bound requirement source and revision.

CASE-002
mode: blackbox
description: [[AC-029, AC-043]] 单个需求点、编号不连续以及任意有限数量的合法需求点均可通过，既不强制重编号也不设置数量上限。
procedure: 在三个独立临时项目中分别生成：仅含 `REQ-009/AC-017` 的文件；含 `REQ-001/REQ-900`、`AC-001/AC-777` 的非连续编号文件；由脚本生成 1,001 个唯一 `REQ`、每个一个唯一 `AC` 的文件。逐一通过 `workflow start`、`requirements-clarification` PASS、`workflow requirement --confirmed` 使用候选 CLI 确认，并保存确认前后文件摘要和 `workflow show`。
oracle: 三个 run 均确认成功；输出 ID 与输入逐字一致，没有自动补号或重编号；大文件投影精确包含 1,001 个 REQ 和 1,001 个 AC，不能因数量较多失败。
review status: FAIL
review correction: setup calls `workflow start` without the required split declaration, so the documented procedure is not executable as written.

CASE-003
mode: blackbox
description: [[AC-003, AC-017]] 缺失或重复 `## 需求点`、空章节、非规范直接三级标题、非法或空 `REQ` 标题以及非 UTF-8 输入均被结构预检拒绝。
procedure: 为每个错误建立独立 run：删除 `## 需求点`；复制该章节形成两个；保留空章节；加入 `### 普通标题`；使用 `### REQ-1：标题`、`### REQ-001：`；另以原始字节写入无效 UTF-8。每个 run 完成 clarification PASS 后执行 `workflow requirement --confirmed`，保存 argv、退出码、stdout、stderr 和 `workflow show`。
oracle: 每次确认均非零退出，状态保持未确认；错误明确包含需求文件路径、对应行号和具体结构规则；可识别时包含 REQ ID；不得静默忽略非法标题或替换无效字节。
review status: PASS

CASE-004
mode: blackbox
description: [[AC-004]] 重复 `REQ` ID 和重复 `AC` ID 均报告重复值及全部冲突行。
procedure: 建立一份含两个 `REQ-007` 的文件和另一份在不同 REQ 下重复 `AC-019` 的文件；分别启动 run、记录 clarification PASS，然后执行 `workflow requirement --confirmed`，保留带行号的源文件和完整错误输出。
oracle: 两次确认均被拒绝且保持未确认；第一份输出包含 `REQ-007` 和两个实际行号，第二份包含 `AC-019` 和两个实际行号；不得只报首处、自动去重或覆盖前一条。
review status: PASS

CASE-005
mode: blackbox
description: [[AC-005, AC-028]] 缺失、重复、乱序、空内容或额外直接 `####` 字段以及字段跨越区块边界时均失败并给出可定位诊断。
procedure: 从一个合法需求点复制出独立变体：分别删除“要求”“验收条件”“来源”；交换字段顺序；重复每种字段；清空字段正文；加入 `#### 其他`；在下一个 `### REQ` 或 `##` 后补写上一需求点字段。逐个 run 执行确认入口并保存源文件行号与诊断。
oracle: 所有变体均非零退出且未确认；诊断包含路径、真实错误行、违反的字段规则和可识别的 REQ ID；不得跨区块拼接字段或接受重复/额外字段。
review status: PASS

CASE-006
mode: blackbox
description: [[AC-006, AC-017]] 无验收项、普通列表项、短编号 AC 和空 AC 正文分别被结构预检拒绝。
procedure: 在四个独立临时 run 中，从同一合法需求案分别构造：`#### 验收条件` 下没有列表项；仅有 `- 普通文本`；仅有 `- AC-1：文本`；仅有 `- AC-001：`。每个 run 完成 clarification PASS 后执行 `workflow requirement --source requirements.md --confirmed --activate-guarantee`，保存带行号源文件、argv、stdout、stderr、退出码及 `workflow show`。
oracle: 四次确认均非零退出且保持未确认；诊断分别指出至少需要一个合法 AC、直接列表项格式错误、编号必须为至少三位数字、正文不能为空，并包含文件路径、实际错误行和所属 REQ；两个非法分支不得合并成二选一 fixture。
review status: PASS

CASE-007
mode: blackbox
description: [[AC-030]] 区块边界外的 REQ/AC 标记是结构错误，而 CommonMark 围栏代码块中的相同示例完全不参与解析和重复检查。
procedure: 建立四个 fixture：在 `## 需求点` 外放置 `### REQ-900`；在“验收条件”字段外放置 `- AC-900`；让需求点在下一个 `###` 或 `##` 前缺字段；在合法文件的围栏代码块中放置与真实条目重复的 REQ/AC 示例。分别执行确认入口并保存投影。
oracle: 前三个 fixture 被拒绝并定位越界标记或不完整区块；围栏 fixture 确认成功，投影只含围栏外条目，围栏内重复值不触发重复错误也不进入覆盖集合。
review status: PASS

CASE-008
mode: blackbox
description: [[AC-027]] 产品审发现需求点外独立存在必须实现的产品或流程行为时必须 FAIL，迁入对应 REQ/AC 前不能取得完整覆盖保证。
procedure: 创建结构合法需求案，但在“方案”正文加入明确且可独立验收的强制行为，REQ/AC 中不承接它；完成确认后准备 `product-review`，交给全新零上下文审查者完整审查并通过公开记录入口提交；随后把该行为迁入一个新 AC、重新确认 revision，再派发 fresh product review。保存两轮报告、裁决与 `workflow show`。
oracle: 第一轮明确列出正文中的该行为并为 FAIL，不能进入技术审或显示完整覆盖保证；修订后第二轮可 PASS，投影包含新增 AC；不得把正文行为静默排除后仍宣称完整保证。
review status: FAIL
review correction: execution requires a genuinely fresh product reviewer and retained reviewer evidence; the prior run did not establish that capability or perform that review.

CASE-009
mode: blackbox
description: [[AC-042, AC-048]] 产品审逐个 REQ 一次列全未由同 REQ 的 AC 承接的独立义务；补齐、映射、批准并在当前候选执行前，REQ 不显示完成。
procedure: 创建两个 REQ，每个“要求”含两个独立义务、但最初各只有一个 AC。确认 guarantee 后，派发 fresh `product-review`，按实际报告用 `record-action` 记录恰好两个所属明确的 P1 finding，并用 `settle-findings --confirm` 逐项确认。把两个遗漏义务各补为新 AC，执行 `workflow requirement --meaning changed`，重新完成 clarification 和 `--confirmed --activate-guarantee`，再派 fresh 产品审并对提示词中的全部 REQ completeness item 记录 PASS。随后完成 start-readiness、no-split、custom+blackbox、QA worktree；为四个 AC 各用 `qa-design --ac <AC-ID>` 登记一条可执行黑盒用例。`qa-review` 对全部 case 使用 `--case/--outcome PASS`，并逐项提交完整的 `--source-decision`、`--point-decision`、`--case-decision`。准备 development-worker，在临时项目提交候选标记并 snapshot；执行四条批准用例，以 `qa-execution --case-result ...` 全量记录当前候选 PASS。分别保存初审、补齐后确认、QA design 后、review 后、execution 后的 `workflow show`。
oracle: 初审一次列出两个且仅两个遗漏义务并阻断；补齐后投影含四个 AC。仅映射或仅批准时两个 REQ 均未完成；四个 case 全部在当前候选执行 PASS 后，两个 REQ 才逐项显示完成，且每项可追溯到所属 AC、批准 case 和当前执行结果。
review status: PASS

CASE-010
mode: blackbox
description: [[AC-008, AC-009, AC-010, AC-014, AC-046]] 结构预检与用户确认仍位于既有确认链路，未确认 revision 不会生成任何后续消费提示词。
procedure: Run A 使用结构错误文件：完成 clarification PASS 后执行 `workflow requirement --confirmed --activate-guarantee`。Run B 使用合法文件：只完成 start 和 clarification PASS，保存 show，并尝试准备 `product-review`、`start-readiness`、`qa-design --mode blackbox` 和 `development-worker`，记录失败及未生成提示词。随后在 Run B 执行同一 `workflow requirement --confirmed --activate-guarantee`；按顺序准备、认领并记录 product-review PASS，再准备、认领并记录 start-readiness PASS；执行 no-split、`route --mode custom --gate blackbox`，创建并登记基线 QA worktree；此后重新准备 qa-design 和 development-worker。保存所有成功提示词及其 requirement revision。
oracle: Run A 确认失败并保持未确认。Run B 确认前四个消费者均不能取得规范提示词；确认后，只有按产品审→技术审→slicing/route→QA worktree 顺序完成前置，相关准备才成功，且提示词全部绑定刚确认的 revision；没有旁路或第二确认入口。
review status: PASS

CASE-011
mode: blackbox
description: [[AC-002, AC-012, AC-044, AC-045]] Codex 在一个可复现的新任务中只维护当前 run 绑定的正式需求案，并展示全文与精确 revision 后取得一次用户确认。
procedure: 新建临时 Git 项目并记录初始 `git status --porcelain=v1 --untracked-files=all` 及 `find . -type f -not -path './.git/*' | LC_ALL=C sort`。在 Codex 公共“新建任务”入口绑定该项目，依次发送并保留消息：①“走 formal-gates regular intake：实现 `hello NAME` 输出 `Hello, NAME`，空 NAME 退出 2；只写一个正式需求案，展示全文和 revision，确认前不要审查。”②在首次文件生成后：“把 AC-002 正文临时清空并运行公开确认命令，给出结构错误；随后只修复同一文件，不建草稿。”③“把 AC-002 改为‘空 NAME 时 stderr 含 missing name 且退出 2’，仍只改同一文件，重新展示全文和 revision。”④看到全文后：“我明确确认刚刚展示的这一版。”通过任务的公开分享/复制入口保存完整用户消息、助手回复、任务 ID 和关键截图。最后保存 `git status`、`git diff --name-status`、同一 `find` 清单、正式需求案每版字节与 SHA-256、最终 `workflow show`。
oracle: 消息和文件证据可独立复现完整过程；首次生成、结构修复和用户修改始终操作同一绑定路径，没有 draft、task-list 或 QA-list 需求事实文件；每次确认提示展示全文及当前 revision，最终确认记录与最后展示文件摘要一致；用户只执行最后一次既有确认，没有新增独立确认阶段。
review status: FAIL
review correction: this procedure requires creating and interacting with a real new Codex task; the recorded execution environment did not provide that capability, so it was not executable there.

CASE-012
mode: blackbox
description: [[AC-011]] 结构预检只判断机械结构，不替代用户判断业务语义。
procedure: 创建结构完全合法但业务内容明显荒谬、冲突或优先级不合理的需求案；完成 clarification PASS 前置后执行确认入口，并分别记录用户拒绝与用户明确确认两条路径。
oracle: 预检不产生“业务不合理”等语义结论；用户拒绝时保持未确认，用户明确确认时结构门允许记录确认；程序不得擅自替代用户接受或拒绝业务内容。
review status: PASS

CASE-013
mode: blackbox
description: [[AC-013, AC-022]] 确认后的需求案 revision 漂移会被识别；QA-enabled guarantee run 的全部覆盖绑定、审查、执行及分片责任证据均失效，旧结果不得重绑。
procedure: 建立已确认且完成 QA review/execution 的非分片 guarantee run，保存 show；修改同一文件中一个 AC 并提交，再执行 `workflow resume`/`workflow show` 和 meaning-preserved 重绑尝试。另在已分配责任并完成切片 QA 的 split fixture 重复该操作。
oracle: 两个 run 均报告 revision 漂移；旧 confirmation、coverage binding、review、execution 变为 stale，分片 fixture 还使责任分配、切片及 merge 证据失效；meaning-preserved 不能恢复旧证据，必须重新确认并完整重做闭环。
review status: PASS

CASE-014
mode: blackbox
description: [[AC-015]] custom 无 QA 的正常路线仍通过现有 workflow 生命周期完成，但必须选择并完成至少一个普通门。
procedure: 在临时 Git 项目建立合法需求案，完成 start、clarification、`requirement --confirmed --activate-guarantee`、product-review PASS、start-readiness PASS 和 no-split。运行 `workflow route-candidates` 确认 `implementation-quality-gate` 是普通非 QA 门，再执行 `workflow route --mode custom --gate implementation-quality-gate`。准备并认领 development-worker，提交 `delivery.txt` 后 snapshot；准备并认领该门，以实际 `<base>..<current>` 记录 PASS；执行 `workflow seal`。同时保存 `workflow --help`、route candidates、各阶段 show 和 Seal 结果。
oracle: 全生命周期只使用现有 `workflow` 入口；普通门实际完成后 Seal 成功。show/结果明确为 `not-guaranteed`，没有 QA case/review/execution，也不声称全部 REQ/AC 闭环；缺少 QA 不新增额外阻断。
review status: PASS

CASE-015
mode: blackbox
description: [[AC-018, AC-019]] 自动投影精确保留全部 REQ、AC 及父子关系；QA 公共参数拒绝未知 REQ/AC。
procedure: 创建三个非连续 REQ、每个含不同数量 AC 的合法需求案，完成 guarantee 确认、两段审查、no-split、custom+blackbox 和 QA worktree。运行 `workflow qa-design --help` 与 `workflow qa-review --help`，保存公开的 `--ac`、`--source-decision`、`--point-decision` 参数。逐个用 `qa-design --ac <真实AC>` 登记 case，并把需求文件中的 ID/父子关系与 `workflow show`、QA-design 提示词投影排序比对。另在独立 fresh run 中分别提交 `qa-design --ac AC-999`、完整 review 中的 `--source-decision REQ-999=PASS` 和 `--point-decision AC-999=PASS`；每次保存失败前后的 show。
oracle: 冻结投影与文件的 REQ、AC、父子关系逐项一致，无缺少、额外或重编号。三个未知 ID 操作均在写入前被拒绝并点名未知值，状态 revision、投影、case 集和 review 均不变；帮助中若没有这些公开绑定参数，用例直接失败。
review status: PASS

CASE-016
mode: blackbox
description: [[AC-016, AC-020]] custom 同时选择黑盒和白盒时，AC 覆盖按两种 QA 的并集计算，且 whitelist 只由完整 source/point/case 决定生成。
procedure: 创建一个 REQ、两个 AC 的 Go fixture 项目；完成 guarantee 确认、两段审查、no-split，并执行 `workflow route --mode custom --gate blackbox --gate whitebox`。登记 QA worktree，用 `qa-design --mode blackbox --ac AC-001` 创建黑盒 case，随后以完整 `--source-decision REQ-001=PASS --point-decision AC-001=PASS --case-decision <case>=PASS` 和对应 `--case/--outcome PASS` 记录 review。完成开发 snapshot；由白盒设计者新增并实际运行 `qa/coverage_test.go::TestAC002Projection`，用 `qa-design --mode whitebox --ac AC-002 --test ...` 登记，再提交该模式完整三层 review。用相同公开链路重新创建独立变体，分别省略一个 source、point、case 决定，重复决定，使用未知 ID，或只提交 case-level PASS；所有变体均保存 review 命令和 show。
oracle: 完整 fixture 的两个模式分别覆盖 AC-001、AC-002，并集显示两项均有批准覆盖，不要求任一模式重复另一项；每种不完整、重复、未知或仅 case-level PASS 变体均写前拒绝且不生成 whitelist。帮助或提示词必须公开三层决定所需 ID 与参数。
review status: PASS

CASE-018
mode: blackbox
description: [[AC-023, AC-048]] `workflow show` 的四阶段状态由公开 QA 操作建立，并逐 AC 展示映射、批准和当前候选执行。
procedure: 创建一个 REQ、三个 AC 的 custom 双 QA run。采集阶段一：确认 guarantee 后立即 show。完成审查、no-split、`route --mode custom --gate blackbox --gate whitebox`；用两条黑盒 case 分别 `--ac AC-001/AC-002`，开发 snapshot 后用一条带独立测试引用的白盒 case `--ac AC-003`。采集阶段二：完整批准两条黑盒 case，但尚未 review 白盒 case。采集阶段三：完整批准白盒 case，实际执行并记录黑盒模式两个 case 的当前候选 PASS，白盒尚未执行。采集阶段四：运行白盒测试并记录白盒 case 当前候选 PASS。每阶段保存 `workflow show` JSON、revision/digest 和 case ID。
oracle: 四阶段均显示 requirement revision、solution revision、manifest digest、REQ=1、AC=3 及每项 case/review/execution。阶段一无映射；阶段二 AC-003 未批准；阶段三 AC-003 未执行，REQ 仍未完成；只有阶段四三个 AC 全部批准且当前候选 PASS 后，REQ 和整体显示完成。
review status: PASS

CASE-019
mode: blackbox
description: [[AC-024]] full、custom+QA、custom 无 QA 及含无-QA责任切片的 split master 通过公开路线和责任分配得到不同 guarantee 状态。
procedure: 从同一合法需求案重建四组 run。A 完成前置后 `route --mode full`。B 执行 `route --mode custom --gate blackbox`。C 执行 `route --mode custom --gate implementation-quality-gate`，完成该门并 Seal。D 以 `--split yes --retained-overall` 启动 master，在 `slicing --decision split --count 2 --slice slice-a --slice slice-b` 中用 `--ac-owner` 把 AC 分配到两个 slice；分别启动并继承 master 的 slice。slice-a 选择 custom+blackbox并完成 QA/Seal；slice-b 选择 custom+`implementation-quality-gate`、完成门并 Seal。保存四组 route 命令、slice Seal 证据、master show 和 Seal 尝试。
oracle: A、B 显示 active guarantee 并受逐 AC QA 门控；C 可 Seal 但明确 `not-guaranteed`；D 因一个 AC 主责 slice 无 QA，master 聚合为 `not-guaranteed`。C、D 均不得输出全部 REQ/AC 闭环 PASS。
review status: PASS

CASE-020
mode: blackbox
description: [[AC-025]] lightweight 保持原有记录路线，不要求本需求案结构或 QA 闭环。
procedure: 在临时 Git 项目中创建普通非结构化 `requirements.md`，执行 `workflow start --route lightweight`、requirements-clarification PASS、`workflow requirement --confirmed` 和 `workflow seal`，保存 show 与结果文件。
oracle: lightweight 生命周期成功且不要求 `## 需求点`、REQ/AC 投影、QA design/review/execution；结果明确标识未经验证，但不因本 change 新增 QA 阻断。
review status: PASS

CASE-021
mode: blackbox
description: [[AC-026]] QA-enabled guarantee 仅允许绑定一个正式需求案文件；`split=no`、串行 `split=yes` 和并行 `split=yes` 采用相同标准：单 artifact 可进入 QA design，额外 artifact 必须在生成任何 QA design 结果前被拒绝，且不得形成部分覆盖。
procedure: 建立六个 fresh Git fixture，均使用同一份含两个 AC 的合法正式需求案，并按“单 artifact／额外登记 `--requirement-artifact extra.md`”分别覆盖：① `split=no`；② `split=yes` 串行处理切片；③ `split=yes` 并行处理切片。每个 run 执行 start、`requirements-clarification` PASS，并从 `workflow requirement --source requirements.md --confirmed --activate-guarantee` 开始，依次尝试产品审、技术审及后续公开流程。非分片执行 no-split slicing、custom+blackbox route、登记基线 QA worktree并准备 blackbox qa-design。分片 master 使用 `--split yes --retained-overall`，以 `--ac-owner` 将两个 AC 完整分配给 `slice-a`、`slice-b`；启动并继承两个 slice。串行组先完成 slice-a 的 QA route、QA worktree和 qa-design 准备，再处理 slice-b；并行组先启动两个 slice并分别完成 route和 QA worktree登记，再在任一方产生 case 前分别准备 qa-design。双 artifact 组从 guarantee activation 起逐命令执行，成功才继续，在首个拒绝处停止；该拒绝不得晚于 qa-design 规范提示词生成。每一步保存 argv、退出码、stdout、stderr、`workflow show`、artifact 路径与 SHA-256，并检查 prompts、QA case及覆盖投影在命令前后的变化。
oracle: 三个单 artifact 组的 guarantee activation、相应 slicing/route、QA worktree登记和所有责任范围的 qa-design 准备均成功；串行或并行顺序不改变结果，生成的提示词均绑定同一已确认 requirement revision，且没有额外 artifact。三个双 artifact 组均在进入保证模式至 qa-design 准备之间的首个适用公开边界非零拒绝，诊断明确指出 QA-enabled guarantee 必须恰好绑定一个正式需求案文件；无论拒绝发生在 activation、route或 qa-design 前置校验，均不得生成 qa-design 提示词、case、部分 `REQ/AC` 覆盖投影或保证 PASS，也不得因 `split=no`、串行或并行执行而采用不同 artifact 数量标准。
review status: PASS

CASE-022
mode: blackbox
description: [[AC-057]] guarantee activation、来源、预检结果、冻结投影和 revision/CAS 原子提交；缺失、损坏或部分状态安全拒绝保证 PASS。
procedure: 对合法 full run，在确认命令前后分别保存 state revision、完整字节和 show，验证一次提交同时出现 activation 与冻结投影。复制该一次性 fixture，分别删除 activation、来源、投影、revision/digest，或制造字段不一致，再调用 `workflow show`、QA design、QA completion 和 Seal；不得在原 run 手改状态。
oracle: 正常确认只产生一次原子 revision 变化，所有绑定字段同时出现且一致；每个损坏副本均拒绝准备或产生 guarantee PASS，报告状态损坏/不一致，且不得根据 route、artifact 数量或文件外观补造 activation。
review status: PASS

CASE-023
mode: blackbox
description: [[AC-021, AC-035]] 对 guarantee 缺口的显式 Seal 授权携带逐项理由，只产生 `waived/not-guaranteed`。
procedure: 建立 custom+whitebox guarantee run，需求含 AC-001～003。完成开发 snapshot 后，只为 AC-002、AC-003 编写两条独立白盒测试并用 `qa-design --ac` 登记；review 时令 AC-002 case FAIL、AC-003 case PASS，AC-001 保持无映射，不执行 AC-003。普通 Seal 必须先失败并输出三个稳定缺口标识。运行 `workflow seal --help`，使用公开 guarantee waiver 形式；候选预期形式为 `--skip requirement-guarantee --user-requested --guarantee-waiver-reason "AC-001=无映射由用户接受; <CASE2>=未批准由用户接受; <CASE3>=未执行由用户接受"`，若帮助提供逐项可重复参数则用等价三项参数。保存帮助、失败输出、授权命令、show 和 Seal ledger。
oracle: 普通 Seal 阻断；授权 Seal 成功但永久标记 `waived/not-guaranteed`，逐项列出无映射、未批准、未执行及各自理由和 USER 来源；不得出现 guarantee PASS 或全部 REQ/AC 完成。
review status: PASS

CASE-024
mode: blackbox
description: [[AC-031]] retained master 通过公开 `--ac-owner` 完整冻结每个 AC 的唯一主责任范围，并原子拒绝五类错误映射。
procedure: 创建含 AC-001～003 的 retained master，完成 guarantee 确认和两段审查。运行 `workflow slicing --help` 保存 `--ac-owner` 契约；合法 fixture 执行 `slicing --decision split --count 2 --slice slice-a --slice slice-b --ac-owner AC-001=slice-a --ac-owner AC-002=slice-b --ac-owner AC-003=master-merge`，随后启动两个 slice，确认相同 revision、继承 master slicing，选择 QA route、登记 QA worktree并准备 QA design。另以 fresh master 分别尝试：遗漏 AC-003；重复提交 AC-001 两个 owner；未知 AC-999；不在 `--slice` 列表的 owner；合法 master 后让 slice 修改需求 revision 再执行继承 slicing。保存每次命令前后 show。
oracle: 只有完整合法映射允许 slice QA design；遗漏、重复、未知 AC、未知 slice、revision 不一致分别点名错误并拒绝。失败前后不存在部分 `acResponsibilities` 写入，合法映射每个 AC 恰有一个 owner。
review status: PASS

CASE-025
mode: blackbox
description: [[AC-032]] 切片 QA manifest 由责任分配和 `--ac/--additional-ac` 公开参数建立；额外覆盖不改变主责。
procedure: 建立 master，执行 `--ac-owner AC-001=slice-a --ac-owner AC-002=slice-b`，启动并继承两个 slice。slice-a 选择 custom+blackbox、登记 QA worktree，执行 `qa-design --mode blackbox --ac AC-001 --additional-ac AC-002`，对 REQ、两个 point 和 case 提交完整 PASS review；完成开发、snapshot、当前候选执行和 slice Seal。保存 slice-a 的 QA design 提示词、Seal 产生的不可变 manifest、slice show、master show；不完成 slice-b。
oracle: slice-a 的 responsibility input 只含 AC-001；manifest 可把 AC-002 作为额外证据，但其 owner 仍为 slice-b。slice show/Seal 仅声明 slice-a 责任范围阶段完成；master 因 slice-b 未完成而不显示整体 PASS。
review status: PASS

CASE-026
mode: blackbox
description: [[AC-033]] slice Seal 通过公开流程留下不可变清单；master 幂等聚合并只接受最终合并候选的 FULL 全量执行。
procedure: 建立两个 AC、两个责任 slice；两边各从本地 `CASE-001` 开始登记并完整批准、执行、Seal。连续运行 master `workflow show`、`resume`、再次 show，使同一 sealed manifest 被重复聚合，并保存 qualified identity。合并两个 slice 的 Git 提交，记录 master snapshot；用公开 master QA 聚合提示词取得最终 qualified case IDs。以 fresh master 流程分别构造：只有 slice 执行；旧 merged candidate FULL PASS 后 repair 并选择 AFFECTED；最终 execution 命令遗漏一个 case；FULL 中一个 FAIL；当前 merged candidate 上全部 qualified case FULL PASS。每个变体尝试正常 Seal。
oracle: 两个同名本地 ID 因 scope 不同均保留；同一 sealed manifest 的重复聚合只计一次且字节绑定不变。前四种整体 QA/Seal 均阻断并指出具体 case/范围；只有最终 merged candidate 的完整 FULL PASS 通过。
review status: PASS

CASE-027
mode: blackbox
description: [[AC-034]] master merge case 通过公开 QA design 登记，但不能替代两个责任 slice case 的最终重跑。
procedure: 创建三个 AC，分别以 `--ac-owner AC-001=slice-a --ac-owner AC-002=slice-b --ac-owner AC-003=master-merge` 分配。两个 slice 各完成一条批准 case 并 Seal；master 在分片开发期间准备 merge qa-design，登记一条通过最终公共入口观察跨 slice 交互的 case，`--ac AC-003`，并完成三层 review。合并提交并 snapshot。用三个 fresh master fixture 的 `qa-execution` 分别提交：仅 merge case；merge case 加 slice-a case；三个 qualified case 全部 PASS。每次保存 argv、原子记录结果、show 和 Seal。
oracle: 前两次因缺少最终候选上的 slice case 被写前拒绝或使 Seal 阻断；第三次全部 PASS 后通过。每个 AC 都能追溯到唯一 owner、批准 case 和最终 merged candidate 结果，merge case 不替代 slice case。
review status: PASS

CASE-028
mode: blackbox
description: [[AC-035]] master 的四类聚合状态均由完整公开生命周期建立，不以手工状态名称模拟。
procedure: 用仅调用 workflow/VCS 公开入口的驱动函数重建以下 master：A 一个责任 slice 选择 custom 无 QA、完成普通门并 Seal；B1 active guarantee 但责任 slice 尚无 case/execution，B2 在旧 requirement revision 留有 QA 证据，B3 最终执行含 FAIL；C 从 B1 的普通 Seal 缺口输出出发，经公开 guarantee waiver 参数逐项授权；D 所有责任 slice 和 merge case 均批准，合并后在当前候选 FULL PASS。各 run 完成 `--ac-owner` 映射、slice 路线、必要 QA/Seal 与 master 聚合，分别运行 master show 和 Seal。
oracle: A 精确为 `not-guaranteed`；B1/B2/B3 均显示未完成及各自 missing/old/FAIL 原因并阻断；C 为 `waived/not-guaranteed`；D 为完整 PASS。前三类逐项显示 owner、case、review、execution，且绝不混成完整 PASS。
review status: PASS

CASE-029
mode: blackbox
description: [[AC-036, AC-040, AC-058]] 新 run 原子初始化 product-review 与 start-readiness 两条独立 series，并公开 completed、默认上限 3、剩余额度和授权来源。
procedure: 创建新 regular run，start 后立即保存 show；完成一次 product-review PASS 后再 show；准备但不完成一次 start-readiness 后第三次 show。
oracle: 首次 show 两条 series 均为 completed=0、limit=3、remaining=3 且有初始化来源；产品审后仅其 completed=1、remaining=2，技术审仍为 0/3；仅准备未完成不计数，字段与 run 创建同一次原子状态提交存在。
review status: PASS

CASE-030
mode: blackbox
description: [[AC-037]] 成功记录的 PASS/FAIL 各恰计一轮；RUNTIME_ERROR、无效、不完整或中断派发不计轮且不消费额外授权，运行故障后可复用同一授权。
procedure: 对两个动作分别记录一个 PASS 和一个 FAIL；另在独立 fixture 制造 RUNTIME_ERROR、无效 binding、未完成和中断。对已到上限动作授权一轮，先令该轮 RUNTIME_ERROR，再重派并成功记录 PASS；每步执行 show。
oracle: 每个有效 PASS/FAIL 仅增加 1；四类无效完成均增加 0；额外授权在 RUNTIME_ERROR 后仍可用，最终成功时只消费一次且 completed 只增加一次。
review status: PASS

CASE-031
mode: blackbox
description: [[AC-038, AC-040]] 某动作完成三轮后第 4 轮被硬拒绝；带非空用户理由只授权该动作一轮，不能预授权多轮或影响另一动作。
procedure: 将 product-review 连续记录三次完整结果；不带授权准备第 4 轮，再分别尝试空理由、多轮数量、授权 start-readiness 但准备 product-review；最后执行 `prepare-action --action product-review --user-requested --reason "用户同意额外一轮"` 并完成该轮，然后准备第 5 轮。
oracle: 无授权、空理由、批量授权和错动作授权均拒绝；合法授权仅放行第 4 轮并留痕，完成后第 5 轮再次要求用户决定；start-readiness 的计数和额度始终不变。
review status: PASS

CASE-032
mode: blackbox
description: [[AC-039]] revision 变化、meaning-preserved/changed 和普通结果失效保留审查轮数及授权记录；显式 reset、新 run 或终态生命周期建立新额度。
procedure: 让两个动作各完成两轮并给 product-review 留一轮额外授权；依次修改需求 revision、记录 meaning-preserved、meaning-changed、使结果 stale，每步 show；再对副本执行显式 workflow reset，并创建新 run 比对。
oracle: 普通变化中 completed 与授权记录逐项不变；reset 和新 run 按各自生命周期重新初始化为 0/3，不能通过改 revision 静默清零旧 run。
review status: FAIL
review correction: setup attempts extra review authorization before the documented three-round automatic limit is exhausted, contradicting the requirement and making the oracle invalid.

CASE-033
mode: blackbox
description: [[AC-041]] 产品审三轮上限、开发后 review wave 和同一 agent task 的首次尝试加两次瞬时 retry 保持独立。
procedure: 在 regular legacy run 中，通过 `prepare-action product-review` 及需要时的 `--user-requested --user-reason` 完成三轮有效产品审；完成一轮 start-readiness、no-split、custom 普通门、development snapshot，并准备/记录一次开发后 gate PASS，使一轮 post-development wave 完成。各阶段保存 `workflow show`。另按 `refactor-plan/route-matrix.md` 记录的独立 engine fixture CLI，执行 `go build -o "$D/engine-fixture" ./internal/engine/testkit/cmd/harness`，再运行 `"$D/engine-fixture" --scenario failure-routing --failure-class TRANSIENT_ENGINE_ERROR --project-root "$D/engine"`；保存其 JSON 中同一 TaskKey 的 attempt sequence。最后再完成 start-readiness 的下一轮并复采两组状态。
oracle: product-review 为 completed=3，仅限制其下一轮；start-readiness、CompletedReviewWaves/ExtraReviewWaves 均保持自己的数值。engine fixture 显示同一任务 attempts `[1,2,3]`，即首次尝试加两次 retry；这些 retry 不增加任何语义审查轮或开发后 wave，后续审查也不改变 retry 记录。
review status: PASS

CASE-034
mode: blackbox
description: [[AC-058]] 缺失、损坏、部分写入或来源不一致的前置审查 series 必须拒绝准备或记录，不能从 attempt、单值结果或 transcript 猜测。
procedure: 从新 run 的一次性副本分别删除一条 series、删除 activation source、令 completed 与记录数不一致、留下半条记录；另把 dispatch attempt 和单值 ActionResult 伪装成历史轮次。对每个副本调用 prepare-action、record-action 和 show。
oracle: 所有副本均报告持久状态损坏或不一致并拒绝写入；不得从 attempt、ActionResult 或 transcript 重建 completed，也不得静默初始化后继续。
review status: PASS

CASE-035
mode: blackbox
description: [[AC-049]] legacy `workflow show` 同时区分不可变 raw candidate、稳定 finding/digest、被 supersede 的裁决、correction 及当前 EffectiveReviewStatus。
procedure: 建立 ACTIVE legacy run，准备并记录一轮含 P1、P3 的 product-review candidate。每次运行 `workflow show` 保存 raw result bytes/digest、finding IDs 和 effective status；通过 `settle-findings` 完成逐项用户裁决。再从 `workflow --help` 定位 CASE-037 所验证的正式 review-correction 子命令，按其帮助用 run、action、result digest、revision、finding/adjudication、原因、用户来源和 freshness 执行 superseding correction；再次 show。不得对 legacy run 调用 engine-only `workflow status/next`。
oracle: 每次 show 都同时保留原 candidate、旧裁决、correction receipt、当前有效结论及来源；raw candidate 字节和 digest 始终不变，旧裁决标记 superseded 而非被覆盖。若 show 不能单独提供这些投影则失败。
review status: PASS

CASE-036
mode: blackbox
description: [[AC-050]] review candidate 必须先走公开 Operator 核实与用户语义裁决路径，错误绑定、证据或范围不能直接成为权威状态。
procedure: 保存 `record-action --help`、`settle-findings --help` 及 workflow help 中正式 correction/Operator 核实入口；若帮助未公开所需操作则失败。用 fresh run 分别构造：旧 revision dispatch；错误 result/dispatch digest；缺少 finding 证据或完整字段；与确认需求无关的范围外 finding；无法通过文档化正常入口复现的 finding；证据只支持 P2 却声明 P1。对前两类直接提交并保存机械拒绝；对后四类先保存 raw candidate，再由 Operator 通过公开核实入口明确提交 requirement-match、正常入口复现、证据、范围、严重度、binding、完整性判定，确认它们不得成为有效 finding。最后提交一个机械完整但改变语义判断的 correction，先不带用户授权，再带明确 USER 来源重试。
oracle: 六类均不能写成有效 finding 或权威 PASS/FAIL，并报告对应 binding、完整性、正常入口、范围、证据或严重度问题；raw 证据可留档但 effective status 不受其冒充。语义 correction 无用户确认时 pending/拒绝，明确授权后才改变有效状态。
review status: FAIL
review correction: deliberately false natural-language Operator evidence is adversarial input outside the documented normal-use boundary; the case cannot require a semantic lie detector.

CASE-037
mode: blackbox
description: [[AC-051]] ACTIVE 非终态 run 的正式 correction 入口精确绑定 run、action、result digest、revision 和目标记录，要求原因与授权来源，并用 CAS/freshness 拒绝过期或冲突。
procedure: 从 `workflow --help` 定位候选快照公开提供的 review-correction 子命令；对一条已裁决 finding 执行合法 correction，随后分别改错 run、action、digest、revision、finding ID、目标裁决、清空原因/来源、复用旧 freshness token 和重复提交同一 correction。保存 receipts 与 show。
oracle: 合法 correction 成功并追加含旧值、新值、原因、来源及 superseded 目标的 receipt，raw result 保留；所有错误、过期和冲突变体均写前拒绝，状态 revision 不变。
review status: PASS

CASE-038
mode: blackbox
description: [[AC-052]] EffectiveReviewStatus 按 finding 状态确定性重算：未处置或确认 P0/P1 阻塞，仅 P2/P3 为 PASS+advisory，全部 P0/P1 驳回或失效后直接 PASS。
procedure: 从同一完整 candidate 创建四个独立裁决 fixture：未处置 P1；确认 P1；仅确认 P2/P3；全部 P0/P1 驳回或纠正无效。每次只通过公开裁决/correction 入口修改并运行 show/next，不派新 reviewer。
oracle: 四个状态依次为 blocked、blocked、PASS with advisory、PASS；重算不需要新审查输出，P2/P3 不阻塞，已驳回或无效 finding 不再计入阻塞。
review status: PASS

CASE-039
mode: blackbox
description: [[AC-053]] 一轮完整审查的唯一或全部 P0/P1 被用户驳回后立即开放下一阶段，不额外派 fresh review 或消耗授权。
procedure: 记录一轮仅含一个 P1 的完整 product-review FAIL candidate，用户通过正式裁决入口驳回该 finding；立即执行 show/next，并尝试 prepare start-readiness 与再次 prepare product-review。对含三个 P1 的 fixture 在全部驳回后重复。
oracle: 两个 fixture 均在最后一个 P1 驳回后直接有效 PASS，start-readiness 可准备；completed 仍只计原审查一轮，额外授权不变；不得要求或允许一轮只用于把 FAIL 改写为 PASS 的空 product-review。
review status: PASS

CASE-040
mode: blackbox
description: [[AC-054]] finding 裁决与整轮有效性纠正只机械重算 completed semantic rounds，不改变其他 action series 或 agent retry attempts。
procedure: 建立 product-review 完整 candidate，记录一个 P1 后依次通过公开 settle/correction 入口执行确认、驳回、P1→P2、P2→P1，每步 show；同时让 start-readiness 已完成一轮。再用四个 fresh run，经 CASE-037 的正式 correction 入口把目标整轮分别纠正为 invalid、incomplete、stale、wrong-binding，保存 correction receipts 和 show，禁止手改 state。另用 CASE-033 的公开 engine fixture 建立同一 TaskKey 的 `[1,2,3]` attempt 记录，保存其状态字节与摘要，在全部 legacy correction 后复采。
oracle: finding 级变化期间 product-review completed 恒为 1；四种整轮无效状态各自把该轮从权威 completed 投影移除，但保留 raw candidate 和 correction 历史。任何 correction 均不重复计轮、不重置 start-readiness series，engine fixture 的 TaskKey attempts、字节和摘要不变。
review status: PASS

CASE-041
mode: blackbox
description: [[AC-055]] legacy correction 由 PASS 变阻塞时使依赖证据 stale/blocked；解除错误阻塞后通过公开可执行性检查开放合法下一步，不误用 engine-only status/next。
procedure: 建立 product-review 含一个 P1 的完整 candidate，用户先 `settle-findings --dismiss`，使 effective status 为 PASS；随后完成 start-readiness、slicing、custom+blackbox+普通门、QA design/review、development snapshot、QA execution 和 gate PASS，保持 run ACTIVE。用正式 correction 入口、精确 digest/revision/finding/adjudication 和 USER 来源，把原 dismissal supersede 为确认 P1；运行 `workflow show` 并尝试 Seal。再用同一入口把该 finding 纠正为 invalid/dismissed，重新 show，并实际尝试准备当前合法下一步 `start-readiness`。记录 correction 前后 dispatch 总数、raw evidence、依赖及无关审计记录；全程不对 legacy run 调用 `workflow status/next`。
oracle: PASS→blocked 后，show 逐项标出技术审、slicing、route、development、QA、门和 Seal 依赖为 stale/blocked，Seal 拒绝；blocked→PASS 后 effective product review 直接 PASS，合法的 start-readiness prepare 成功。raw candidate、correction 历史和无关审计记录未删除，且没有自动新增 product-review dispatch。
review status: PASS
