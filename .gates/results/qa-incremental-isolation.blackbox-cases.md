# Blackbox QA cases (run: qa-incremental-isolation)

Derived from the run state at qa-design record time. Review these cases against the current confirmed requirement.

CASE-001
mode: blackbox
description: 黑盒 qa-design 记录时，CLI 从 run-state 派生 blackbox 用例 mirror 写入已登记隔离工作区的 .gates/cases/blackbox.md（内容与 run-state 该 mode 用例一致）；主工作区在 run 期间不出现该文件（隔离）。
procedure: 通用前置 start（<fg> workflow start --root <scratch> --package-root <pkg> --run-id iso-1 --flow formal --requirement <scratch>/REQ.md --vcs git --split no）→ 需求确认（prepare-action requirements-clarification → claim-dispatch → record-action PASS → workflow requirement --source <scratch>/REQ.md --confirmed）→ 产品审 PASS（prepare-action product-review → claim → record-action PASS）→ 建隔离工作区 git -C <scratch> worktree add <iso> <base>（<iso> 置于 <scratch> 外）→ workflow qa-worktree --worktree <iso>（RC=0）→ prepare-action qa-design --mode blackbox → claim → workflow qa-design --case '<desc-1>' --mode blackbox --procedure '<proc-1>' --oracle '<oracle-1>'（RC=0）。记录后 cat <iso>/.gates/cases/blackbox.md 与 ls -la <scratch>/.gates/cases/ 2>&1、cat <scratch>/.gates/tmp/iso-1/state.json 或 workflow show 留证据。
oracle: <iso>/.gates/cases/blackbox.md 存在且内容包含与记录一致的黑盒用例（description/procedure/oracle）；<scratch>/.gates/cases/blackbox.md 不存在（主工作区隔离期间无该文件）；qa-design RC=0、run-state 该 mode 用例已记录。文件缺失/内容与记录不一致/主工作区出现该文件任一即 FAIL。
review status: PASS

CASE-002
mode: blackbox
description: 镜像仅按 blackbox mode 写入：whitebox qa-design 记录不产生 .gates/cases/blackbox.md。
procedure: 同用例 1 前置（start → 需求确认 → 产品审 PASS → 注册隔离工作区 <iso>）→ prepare-action qa-design --mode whitebox → claim → workflow qa-design --case '<desc-w>' --mode whitebox --procedure '<proc-w>' --oracle '<oracle-w>' --test '<file>::<func>'（RC=0）→ ls -la <iso>/.gates/ 2>&1、find <iso>/.gates -name 'blackbox.md' 2>&1 留证据。
oracle: whitebox 记录 RC=0 且 run-state 白盒用例已记录；<iso>/.gates/cases/blackbox.md 不存在。出现该文件即 FAIL（whitebox 写入了 blackbox mirror）。
review status: PASS

CASE-003
mode: blackbox
description: 黑盒 qa-review 派发以隔离工作区为审查工作区并读取其用例文件：规范提示词文件引用隔离工作区路径与其 .gates/cases/blackbox.md 用例文件。
procedure: 同用例 1 前置并记录一个黑盒用例（qa-design 完成）→ prepare-action --action qa-review --mode blackbox（RC=0，记录返回的 dispatch id 与 promptFile 路径）→ cat .gates/tmp/iso-3/prompts/<dispatch-id>.md，grep -nE '<iso>|blackbox.md' 留证据。
oracle: 规范提示词文件（.gates/tmp/<rid>/prompts/<dispatch-id>.md）的 worktree/工作区字段指向隔离工作区 <iso>，并引用隔离工作区 .gates/cases/blackbox.md 用例文件路径。未引用隔离工作区或用例文件即 FAIL。
review status: PASS

CASE-014
mode: blackbox
description: seal 时 CLI 把已批准 blackbox 用例从 run-state 物化到主工作区 .gates/results/ 的 <run-name>.blackbox-cases.md（与 seal ledger 同目录）；由 CLI 完成、不经 agent 或 git merge；即使黑盒 review PASS 后隔离工作区已清空，物化仍从 run-state 完成，不依赖工作区残留。
procedure: 通用前置 start（workflow start --root <scratch> --package-root <pkg> --run-id <rid> --flow formal --requirement <scratch>/REQ.md --vcs git --split no）→ 需求确认（prepare-action requirements-clarification → claim-dispatch → record-action PASS → workflow requirement --source <scratch>/REQ.md --confirmed）→ 产品审 PASS（prepare-action product-review → claim → record-action PASS）→ start-readiness PASS（prepare-action start-readiness → claim → record-action PASS）→ 建隔离工作区 git -C <scratch> worktree add <iso> <base> → workflow qa-worktree --worktree <iso>（RC=0）→ 记录一个黑盒用例（prepare-action qa-design --mode blackbox → claim → workflow qa-design --case '<desc>' --mode blackbox --procedure '<proc>' --oracle '<oracle>'，RC=0）→ 拆分决定（workflow slicing --decision no-split --note '<reason>'，RC=0）→ 路线确认（workflow route --mode custom --gate blackbox，RC=0）→ 黑盒 review PASS（prepare-action qa-review --mode blackbox → claim → workflow qa-review --case CASE-001 --outcome PASS；review PASS 后隔离工作区按现有机制清空，host 物理移除旧区 git -C <scratch> worktree remove <iso> --force，mirror 随之消失）→ 开发派发（prepare-action development-worker → claim-dispatch → worker 写代码并提交 → workflow snapshot --dispatch <dev-dispatch-id>，RC=0）→ 黑盒执行 PASS（prepare-action qa-execution --mode blackbox → claim → workflow qa-execution --case-result CASE-001 --outcome PASS --procedure '<actual>' --observation '<observed>' --oracle-result '<comparison>'，RC=0）→ workflow seal（RC=0）。seal 后 ls -la <scratch>/.gates/results/、cat <scratch>/.gates/results/<rid>.blackbox-cases.md，并与 <scratch>/.gates/results/<rid>.json（ledger）对照留证据。
oracle: seal RC=0；主工作区 .gates/results/ 出现 <rid>.blackbox-cases.md（与 ledger 同目录），内容为已批准（PASS）blackbox 用例的完整规格（description/procedure/oracle）；即使 <iso> 已随 review PASS 移除、mirror 已消失，文件仍生成。文件缺失/位置不在 .gates/results/ 与 ledger 同目录/内容不含已批准黑盒用例任一即 FAIL。
review status: PASS

CASE-015
mode: blackbox
description: qa-design 默认增量：在已记录并 review PASS 的用例之外再仅提交新用例时，未提及的既有用例不被清除，其 PASS 状态自动保留；每轮记录重写隔离工作区 mirror，mirror 反映完整合并集（run-state 为单一来源，无“未提及即清除”）。
procedure: 前置同用例 1（start → 需求确认 → 产品审 PASS → 建隔离工作区 → qa-worktree 注册，run-id 记为 <rid>）→ 第一轮记录用例 A（prepare-action qa-design --mode blackbox → claim → workflow qa-design --case '<desc-A>' --mode blackbox --procedure '<proc-A>' --oracle '<oracle-A>'，RC=0）→ start-readiness PASS（prepare-action start-readiness → claim → record-action PASS）→ 拆分决定（workflow slicing --decision no-split --note '<reason>'，RC=0）→ 路线确认（workflow route --mode custom --gate blackbox，RC=0）→ qa-review 用例 A PASS（prepare-action qa-review --mode blackbox → claim → workflow qa-review --case CASE-001 --outcome PASS）→ 第二轮开始前重建并重登记隔离工作区（git -C <scratch> worktree remove <iso> --force → git -C <scratch> worktree add <iso> <base> → 重新注入当前需求文档到 <iso> → workflow qa-worktree --worktree <iso>，RC=0）→ 第二轮仅新增用例 B（prepare-action qa-design --mode blackbox → claim → workflow qa-design --case '<desc-B>' --mode blackbox --procedure '<proc-B>' --oracle '<oracle-B>'，RC=0）→ cat <scratch>/.gates/tmp/<rid>/state.json、cat <iso>/.gates/cases/blackbox.md 留证据。
oracle: 第二轮 qa-design RC=0；run-state 中 blackbox mode 同时含 A 与 B，A 保留且其 reviewStatus 仍为 PASS、B 为 PENDING；隔离工作区 mirror（.gates/cases/blackbox.md）被重写为含 A+B 的完整合并集。A 被清除或 PASS 状态丢失即 FAIL。
review status: PASS

CASE-006
mode: blackbox
description: --remove-case <id> 显式删除指定用例（该 mode 其余用例保留、mirror 同步）；删除不存在的 id 时 CLI 校验报错拒绝、run-state 不变。
procedure: 前置同用例 5（start → 需求确认 → 产品审 PASS → 建隔离工作区 → qa-worktree 注册）→ 记录用例 A、B（两轮 prepare→claim→qa-design，各自 RC=0）→ prepare-action qa-design --mode blackbox → claim → workflow qa-design --remove-case CASE-001（RC=0）→ cat <scratch>/.gates/tmp/<rid>/state.json、cat <iso>/.gates/cases/blackbox.md 留证据 → 再次 prepare-action qa-design --mode blackbox → claim → workflow qa-design --remove-case CASE-999（期望 RC!=0）→ 再次 cat run-state 留证据。
oracle: 删除存在的 id（CASE-001）RC=0，run-state blackbox mode 只剩 B，mirror 同步只剩 B；删除不存在的 id（CASE-999）RC!=0 且报 id 不存在，run-state 不变。删除不存在 id 未报错、或删除成功却把该 mode 清空即 FAIL。
review status: PASS

CASE-007
mode: blackbox
description: --replace-all 显式整体替换该 mode 用例集：携带新用例规格时整套替换；不带规格（空集）时清空该 mode；仅显式 --replace-all 空集才清空，默认增量不会因“未提及”而清空。
procedure: 前置同用例 5（start → 需求确认 → 产品审 PASS → 建隔离工作区 → qa-worktree 注册）→ 记录用例 A、B → prepare-action qa-design --mode blackbox → claim → workflow qa-design --replace-all --case '<desc-C>' --mode blackbox --procedure '<proc-C>' --oracle '<oracle-C>'（RC=0）→ cat run-state、cat <iso>/.gates/cases/blackbox.md 留证据 → prepare-action qa-design --mode blackbox → claim → workflow qa-design --replace-all --mode blackbox（不带 --case，RC=0）→ 再 cat run-state 与 mirror 留证据。
oracle: 带规格的 --replace-all RC=0，run-state blackbox mode 只剩 C（A、B 清除），mirror 同步只有 C；空集 --replace-all RC=0，该 mode 清空（run-state 该 mode 无用例、mirror 清空或无 blackbox.md）。未整体替换、或空集未清空即 FAIL。
review status: PASS

CASE-008
mode: blackbox
description: 修改既有用例时引用不存在的 id（--case-id <非存在 id>）→ CLI 拒绝并报错，不静默当作新增用例造成重复、不分配新 id。
procedure: 前置同用例 5（start → 需求确认 → 产品审 PASS → 建隔离工作区 → qa-worktree 注册）→ 记录用例 A → prepare-action qa-design --mode blackbox → claim → workflow qa-design --case-id CASE-999 --case '<desc-X>' --mode blackbox --procedure '<proc-X>' --oracle '<oracle-X>'（期望 RC!=0）→ cat <scratch>/.gates/tmp/<rid>/state.json 留证据。
oracle: RC!=0 且报 id 不存在/拒绝；run-state 不新增任何用例（blackbox mode 仍只有 A）；不得静默分配新 id。静默新增即 FAIL。
review status: PASS

CASE-009
mode: blackbox
description: 无 id 提交与既有用例语义重复（description+procedure+oracle 一致）的规格 → CLI 报错拒绝并提示改用修改语义（--case-id），不分配新 id。
procedure: 前置同用例 5（start → 需求确认 → 产品审 PASS → 建隔离工作区 → qa-worktree 注册）→ 记录用例 A → prepare-action qa-design --mode blackbox → claim → workflow qa-design --case '<desc-A>' --mode blackbox --procedure '<proc-A>' --oracle '<oracle-A>'（与 A 完全一致、无 --case-id，期望 RC!=0）→ cat <scratch>/.gates/tmp/<rid>/state.json 留证据。
oracle: RC!=0，报错提示语义重复/建议改用修改（--case-id）；run-state 不新增用例、仍只有 A（无新 id 分配）。静默新增重复用例即 FAIL。
review status: PASS

CASE-016
mode: blackbox
description: rework 约束保留：无实质变更的 qa-design 记录被拒（必须新增/修订用例，或 --remove-case/--replace-all）——review FAIL 后仅重交相同规格不构成实质变更，CLI 拒绝。
procedure: 前置同用例 5（start → 需求确认 → 产品审 PASS → 建隔离工作区 → qa-worktree 注册，run-id 记为 <rid>）→ 记录用例 A（prepare-action qa-design --mode blackbox → claim → workflow qa-design --case '<desc-A>' --mode blackbox --procedure '<proc-A>' --oracle '<oracle-A>'，RC=0）→ start-readiness PASS（prepare-action start-readiness → claim → record-action PASS）→ 拆分决定（workflow slicing --decision no-split --note '<reason>'，RC=0）→ 路线确认（workflow route --mode custom --gate blackbox，RC=0）→ qa-review 用例 A FAIL（prepare-action qa-review --mode blackbox → claim → workflow qa-review --case CASE-001 --outcome FAIL --reason '<reason>'，RC=0）→ prepare-action qa-design --mode blackbox → claim → workflow qa-design --case '<desc-A>' --case-id CASE-001 --mode blackbox --procedure '<proc-A>' --oracle '<oracle-A>'（与已 FAIL 规格相同，期望 RC!=0）→ cat <scratch>/.gates/tmp/<rid>/state.json 留证据。
oracle: RC!=0 且报无实质变更/rework 拒绝；run-state 该用例规格与状态未变（仍为 FAIL、内容不变）。未拒绝或状态被改动即 FAIL。
review status: PASS

CASE-017
mode: blackbox
description: 增量契约下黑盒 qa-review 提示词注入本轮新增/修改/删除的用例 id 列表 + 完整合并集，且仍引用隔离工作区用例文件路径，保持审查全上下文。
procedure: 前置同用例 5（start → 需求确认 → 产品审 PASS → 建隔离工作区 → qa-worktree 注册，run-id 记为 <rid>）→ 记录用例 A（prepare-action qa-design --mode blackbox → claim → workflow qa-design --case '<desc-A>' --mode blackbox --procedure '<proc-A>' --oracle '<oracle-A>'，RC=0）→ start-readiness PASS（prepare-action start-readiness → claim → record-action PASS）→ 拆分决定（workflow slicing --decision no-split --note '<reason>'，RC=0）→ 路线确认（workflow route --mode custom --gate blackbox，RC=0）→ qa-review 用例 A PASS（prepare-action qa-review --mode blackbox → claim → workflow qa-review --case CASE-001 --outcome PASS）→ 第二轮前重建并重登记隔离工作区（git -C <scratch> worktree remove <iso> --force → git -C <scratch> worktree add <iso> <base> → 重新注入当前需求文档到 <iso> → workflow qa-worktree --worktree <iso>，RC=0）→ 第二轮新增记录 B（prepare-action qa-design --mode blackbox → claim → workflow qa-design --case '<desc-B>' --mode blackbox --procedure '<proc-B>' --oracle '<oracle-B>'，RC=0）→ prepare-action qa-review --mode blackbox（RC=0）→ 记录返回的 dispatch id 与 promptFile 路径 → cat <scratch>/.gates/tmp/<rid>/prompts/<dispatch-id>.md，grep 变更 id（新增 B 的 id）、A 的 id、<iso>/.gates/cases/blackbox.md 留证据。
oracle: 提示词包含本轮变更列表（新增用例 id=B/CASE-002）与完整合并集（A 与 B 的 id 及规格均出现），并引用隔离工作区 .gates/cases/blackbox.md 路径。缺变更列表、缺完整合并集、或未引用隔离工作区用例文件任一即 FAIL。
review status: PASS

CASE-012
mode: blackbox
description: 活动 run 下主线程只读命令即使命令文本提到 .gates 也放行：grep/rg/ls/cat/find/python3 读、只读 git 查询（log/status/show/diff）不再因命令文本含 .gates 子串被误拦。
procedure: 通用前置 start（<fg> workflow start --root <scratch> --package-root <pkg> --run-id <rid> --flow formal --requirement <scratch>/REQ.md --vcs git --split no，形成活动 run）→ 对下列每条命令以 PreToolUse 载荷喂文档化 hook 入口 <fg> hook decide，记录 decision 输出与退出码：printf '%s' '{"cwd":"<scratch>","tool_name":"Bash","tool_input":{"command":"grep -rn .gates <scratch>/REQ.md"}}' | <fg> hook decide；同法测 ls <scratch>/.gates、cat <scratch>/.gates/tmp/<rid>/state.json、python3 -c 'print(open("<scratch>/REQ.md").read())'、git -C <scratch> log --oneline、git -C <scratch> status --short。
oracle: 每条只读命令 hook 决策为 allow（决策 JSON 为允许、退出码 0），即使命令文本含 .gates 也不被拦。任一条被拦即 FAIL。
review status: PASS

CASE-013
mode: blackbox
description: 活动 run 下主线程真实写代码或 .gates 的命令/编辑仍被拦：git commit/push/merge/rebase/reset --hard/checkout --/clean/add；输出重定向到 .gates 或代码（> .gates/...、>> main.go）；文件变更工具（tee/rm/mv/cp/touch/mkdir/sed -i/install）指向 .gates 或代码；以及 Edit/Write 工具写代码或 .gates（isCodeOrRunStatePath 判定不变）。
procedure: 前置同用例 12（start 形成活动 run）→ 对下列每条以 PreToolUse 载荷喂 <fg> hook decide，记录 decision 与退出码：Bash 载荷 {"cwd":"<scratch>","tool_name":"Bash","tool_input":{"command":"git commit -m x"}}、git add .gates/results/x、echo x > .gates/tmp/x、echo x >> <scratch>/main.go、tee .gates/cases/blackbox.md < /dev/null、rm <scratch>/main.go、sed -i s/a/b/ <scratch>/main.go；Write 载荷 {"cwd":"<scratch>","tool_name":"Write","tool_input":{"file_path":"<scratch>/internal/code.go"}} 与 {"cwd":"<scratch>","tool_name":"Write","tool_input":{"file_path":"<scratch>/.gates/x"}}。
oracle: 每条真实写载荷 hook 决策为 deny/block（阻断决策、拒绝退出码），写 .gates 与写代码均被拦，且真实写未被实际执行。任一条被放行即 FAIL。
review status: PASS

CASE-018
mode: blackbox
description: 修改既有用例成功路径：--case-id 引用存在的 id + 变更规格 → RC=0，该用例 id 不变（仍为原 CASE-001）、description/procedure/oracle 更新为变更后规格、其 reviewStatus 置回 PENDING（修改后须重新审查）；隔离工作区 mirror 同步重写为更新后的规格。
procedure: 前置同用例 5（start → 需求确认 → 产品审 PASS → 建隔离工作区 → qa-worktree 注册，run-id 记为 <rid>）→ 记录用例 A（prepare-action qa-design --mode blackbox → claim → workflow qa-design --case '<desc-A>' --mode blackbox --procedure '<proc-A>' --oracle '<oracle-A>'，RC=0）→ start-readiness PASS（prepare-action start-readiness → claim → record-action PASS）→ 拆分决定（workflow slicing --decision no-split --note '<reason>'，RC=0）→ 路线确认（workflow route --mode custom --gate blackbox，RC=0）→ qa-review 用例 A PASS（prepare-action qa-review --mode blackbox → claim → workflow qa-review --case CASE-001 --outcome PASS）→ 重建并重登记隔离工作区（git -C <scratch> worktree remove <iso> --force → git -C <scratch> worktree add <iso> <base> → 重新注入当前需求文档到 <iso> → workflow qa-worktree --worktree <iso>，RC=0）→ prepare-action qa-design --mode blackbox → claim → workflow qa-design --case '<desc-A2>' --case-id CASE-001 --mode blackbox --procedure '<proc-A2>' --oracle '<oracle-A2>'（变更规格，期望 RC=0）→ cat <scratch>/.gates/tmp/<rid>/state.json、cat <iso>/.gates/cases/blackbox.md 留证据。
oracle: 修改记录 RC=0；run-state 中 CASE-001 的 description/procedure/oracle 更新为变更后规格（<desc-A2>/<proc-A2>/<oracle-A2>），id 仍为 CASE-001、其 reviewStatus 置回 PENDING（非 FAIL、非 PASS）；隔离工作区 mirror 中该用例内容同步为更新后规格。RC!=0、id 改变、规格未更新、或 reviewStatus 未置回 PENDING 任一即 FAIL。
review status: PASS

CASE-019
mode: blackbox
description: 隔离工作区未登记时黑盒 qa-design 不写 mirror：未执行 qa-worktree 注册时，黑盒 qa-design 派发被拒（报隔离工作区未登记），不产生任何 .gates/cases/blackbox.md mirror 文件，run-state 黑盒用例不变——mirror 写入以登记隔离工作区为前提。
procedure: 通用前置 start（workflow start --root <scratch> --package-root <pkg> --run-id <rid> --flow formal --requirement <scratch>/REQ.md --vcs git --split no）→ 需求确认（prepare-action requirements-clarification → claim-dispatch → record-action PASS → workflow requirement --source <scratch>/REQ.md --confirmed）→ 产品审 PASS（prepare-action product-review → claim → record-action PASS）→ 不建隔离工作区、不执行 qa-worktree → prepare-action qa-design --mode blackbox（期望 RC!=0）→ find <scratch> -name 'blackbox.md' 2>&1、ls -la <scratch>/.gates/cases/ 2>&1、cat <scratch>/.gates/tmp/<rid>/state.json 留证据。
oracle: prepare-action qa-design --mode blackbox 在无登记隔离工作区时 RC!=0 且报“隔离工作区未登记 / QA isolation worktree is not registered”；find 无 blackbox.md（主工作区任何位置均无 mirror）、run-state 黑盒 mode 无用例。未报错、或出现任何 blackbox.md mirror、或 run-state 出现用例任一即 FAIL。
review status: PASS

