# Blackbox QA cases (run: stage0-1-delivery-repair)

Derived from the run state at qa-design record time. Review these cases against the current confirmed requirement.

CASE-001
mode: blackbox
description: 矩阵 workflow 面覆盖 §2.3 枚举的全部 31 个子命令名（含 drive/submit），且每行具备 runtime、唯一 writer、schema/definition 版本绑定、允许的状态变化、错误码、是否只读六个必需列与阶段 0/1 绑定标注
procedure: 在 MAIN 下打开 refactor-plan/route-matrix.md；对 31 个名字（start、show、status、next、diagnose、resume、abort、reset、requirement、route-candidates、slicing、settle-findings、route、route-add、qa-worktree、prepare-gate、prepare-action、claim-dispatch、record-action、record-gate、qa-design、qa-review、qa-execution、qa-execution-scope、snapshot、cleanup、carry、authorize-repair、seal、drive、submit）逐个检索是否存在对应 workflow 面行；再抽查 start、abort、seal、record-gate、drive 五行，逐行核对六列取值均非空，且阶段 0/1 绑定使用 legacy、install/bootstrap、Shadow/diagnostic、unsupported 四词表之一
oracle: PASS：31 个名字每个都有同名行；抽查 5 行六列全非空、绑定标签属于四词表。失败：任一名字无行（如缺 status、drive 或 submit），或抽查行必需列为空/缺失，或绑定标签不在四词表
review status: PASS

CASE-002
mode: blackbox
description: 矩阵维护/transport 面覆盖 hook、lifecycle capture/verify、canary、gate、install、uninstall、package 及 registry admission/register/reconcile、cutover、rollback 全部条目，且每行具备分类、owner、generation/token、receipt schema、恢复入口、权限边界必需列
procedure: 在 MAIN 的 route-matrix.md 维护/transport 面逐项确认 13 个条目各有独立行：hook、lifecycle capture、lifecycle verify、canary、gate、install、uninstall、package、registry admission、registry register、registry reconcile、cutover、rollback；抽查 install、registry admission、cutover、rollback 四行，核对分类、owner、generation/token、receipt schema、恢复入口、权限边界六项非空且含阶段 0/1 绑定标注
oracle: PASS：13 条目各有一行；抽查 4 行六项全非空并带四词表绑定。失败：任一维护面条目缺行（如缺 cutover 或 rollback），或抽查行任一必需列为空，或绑定缺词表标注
review status: PASS

CASE-003
mode: blackbox
description: 实际公开 workflow 面 ⊆ 矩阵——构建出的 CLI 二进制自述的全部 workflow 子命令在矩阵中都有对应行
procedure: 在 MAIN 按 README 文档化方式构建 go build -o bin/formal-gates ./cmd/formal-gates；运行 ./bin/formal-gates workflow --help（exit 0），记录 Subcommands: 行的竖线分隔清单（应为 28 项：start、show、diagnose、resume、abort、reset、requirement、route-candidates、route、route-add、slicing、settle-findings、qa-worktree、prepare-gate、prepare-action、claim-dispatch、record-action、record-gate、qa-design、qa-review、qa-execution、qa-execution-scope、snapshot、future、carry、authorize-repair、seal、cleanup）；对清单中每个名字确认矩阵 workflow 面有同名行
oracle: PASS：usage 清单与 28 项完全一致且每个名字有矩阵行。失败：usage 清单出现 28 项之外的子命令而无对应矩阵行（实际入口缺席矩阵即缺陷），或清单与 28 项不符（交付同时改动了公开面）
review status: PASS

CASE-004
mode: blackbox
description: 矩阵中超出 §2.3 枚举的实际公开入口（workflow future、registry show 等）均以计划未枚举标注补行，且绑定仅允许 legacy 或 unsupported
procedure: 在 MAIN 的 route-matrix.md 中找出所有条目名不属于 §2.3 枚举（31 个 workflow 名与 13 个维护名）的行——至少应含 future 与 registry show 两行；逐行核对含字面标注计划未枚举、阶段 0/1 绑定列只写 legacy 或 unsupported；反向核对 §2.3 已枚举的行（如 start、install）不带该标注
oracle: PASS：future、registry show 行存在、含计划未枚举字面标注、绑定为 legacy/unsupported 之一；其余超枚举行同样满足；§2.3 枚举行无该标注。失败：future 或 registry show 缺行/缺标注，或绑定写成 install/bootstrap、Shadow/diagnostic，或枚举行被误标
review status: PASS

CASE-005
mode: blackbox
description: 矩阵绑定结论与产品实际行为一致——unsupported 行被二进制显式拒绝，非 unsupported 的 workflow 行被二进制接受，不得出现与实现相反的行
procedure: 在 MAIN 构建二进制后：①对 workflow 面 unsupported 行逐个执行 ./bin/formal-gates workflow status|next|drive|submit，记录 exit code 与 stderr；②对绑定非 unsupported 的行抽查 start、resume、record-gate、qa-execution-scope、carry、seal，逐个执行 ./bin/formal-gates workflow <名> --help，记录 exit code 与首行；③执行 ./bin/formal-gates cutover、rollback、registry register、registry reconcile，记录 exit code 与 stderr；④对照矩阵相应行绑定列
oracle: PASS：①四命令均 exit 1 且 stderr 恰为 unknown workflow subcommand: <名>；②六命令均 exit 0 且首行为 Usage of workflow <名>:；③四命令均 exit 1 且 stderr 以 unknown command: 或 unknown registry subcommand: 开头；矩阵行绑定与观测一致。失败：任一 unsupported 命令 exit 0（缺省冒充实现），或非 unsupported 行被拒，或绑定与探测相反
review status: PASS

CASE-006
mode: blackbox
description: 矩阵 start 行注明 legacy start 仍带 --split 的当前事实，且产品实际仍要求显式 --split
procedure: 在 MAIN 的 route-matrix.md 找到 start 行，核对其含 --split 字样并注明该约束属 legacy 维持项（含阶段 3 迁移表述）；随后在临时目录 /tmp/fg-split-probe 用交付二进制复现：mkdir -p /tmp/fg-split-probe && ./bin/formal-gates workflow start --root /tmp/fg-split-probe --vcs git --requirement <MAIN>/openspec/changes/stage0-1-delivery-repair/master-requirements.md，记录 exit code 与 stderr；rm -rf /tmp/fg-split-probe
oracle: PASS：start 行含 --split 与 legacy 维持注记；探测 exit 1 且 stderr 含片段 requires an explicit --split yes|no declaration。失败：start 行未提 --split 或宣称 start 已不接受拆分声明，或探测 exit 0（行为已变而矩阵未按事实记载），或错误消息缺该片段
review status: PASS

CASE-007
mode: blackbox
description: 矩阵逐行 negative tests 证据引用要么指向仓库中真实存在的机器证据、要么显式标注证据缺口+原因，不存在静默留空
procedure: 在 MAIN 的 route-matrix.md 逐行检查 negative-test/机器证据单元格：①统计是否存在空单元格；②对引用了路径/文件/符号的单元格（如 UNSUPPORTED_RUN_VERSION、install journal 恢复测试、shadow 只读测试），逐个用 grep -r <引用符号> --include=*.go MAIN 或 test -f <引用路径> 验证至少命中一处；③对含证据缺口的单元格核对其后跟非空原因文本
oracle: PASS：无空单元格；每个非缺口引用都能在仓库中解析到至少一处真实存在；每个证据缺口单元格带非空原因。失败：存在静默空单元格、引用零命中（虚构证据），或证据缺口后无原因
review status: PASS

CASE-008
mode: blackbox
description: stage-records.md 阶段 0 节七项必需内容齐全非空，且第 4 项指向 RQ-1 矩阵并给一句式唯一 writer 结论
procedure: 在 MAIN 打开 refactor-plan/stage-records.md 定位阶段 0 节；逐项确认七项各有内容且非空：①阶段编号/run ID/sealed commit/主线集成 commit；②包摘要/installed-target digest/state schema version/workflow definition version/definition digest；③固定稳定插件摘要与候选安装摘要；④公开能力矩阵与唯一 writer（须含指向 route-matrix.md 的引用及一句话结论）；⑤正常入口 smoke/新增能力 E2E/QA/gates 与 canary 证据指针；⑥资源 cleanup receipt；⑦下一阶段 worktree 的 post-integration canonical base 与关联 receipt
oracle: PASS：七项逐一存在非空；第 4 项含 route-matrix.md 引用及一句式结论。失败：任一项缺失或空占位，或第 4 项未引用 route-matrix.md/无结论
review status: PASS

CASE-009
mode: blackbox
description: stage-records.md 阶段 1 节七项必需内容齐全非空，且第 4 项指向 RQ-1 矩阵并给一句式唯一 writer 结论
procedure: 同阶段 0 用例，但定位阶段 1 节，逐项核对同样的七项内容与第 4 项的矩阵引用及一句式结论
oracle: PASS：阶段 1 节七项逐一存在非空；第 4 项含 route-matrix.md 引用与一句式结论。失败：任一项缺失/为空，或第 4 项缺引用或缺结论
review status: PASS

CASE-010
mode: blackbox
description: stage-records 两节记录的指针全部可溯源——git SHA 存在于仓库历史、run ID 与 .gates/results/phase-*.json 的 runId 一致、版本与 sealed commit 处 definitions/workflow.json 字段值一致
procedure: 在 MAIN 的 stage-records.md 两节提取全部 git commit SHA，逐个 git cat-file -e <SHA>^{commit}；提取 run ID 与 .gates/results/ 下 phase-0-distribution-002.json、phase-1-decision-kernel.json 的 runId 比对（两者 status 均为 SEALED）；对记录的 workflow definition version 与 state schema version，用 git show <sealed-commit>:definitions/workflow.json 读取 version 与 stateSchemaVersion 比对（仓库事实：阶段 0 sealed commit 处为 1/1，阶段 1 为 2/1）；记录的文件路径逐个 test -f
oracle: PASS：所有 SHA 解析成功；run ID 与 JSON runId 相等且 SEALED；版本值逐一相等（阶段 0 为 1/1、阶段 1 为 2/1）；路径全部存在。失败：任一 SHA 无法解析、run ID 对不上、版本与 sealed commit 处不符或路径不存在（虚构指针）
review status: PASS

CASE-011
mode: blackbox
description: stage-records 不虚构——两节 14 个项槽位中，凡指针不可解析的内容必须显式标不可考+原因，不存在静默空缺或无标注的悬空指针
procedure: 在 MAIN 的 stage-records.md 对阶段 0/1 两节共 14 个项槽位逐槽检查：内容非空；对每个形似指针执行解析——SHA 用 git cat-file -e <SHA>^{commit}、run ID 与 .gates/results/phase-*.json 的 runId 比对、路径用 test -f、digest 形指针与 .gates/results/ 对应封板 JSON 内字段（packageDigest/currentSnapshot 等）或 receipts 文件中的 digest 值逐一字符串比对；对解析失败的槽位核对该槽位文本含字面不可考且其后跟非空原因
oracle: PASS：14 槽位无一为空；每个解析失败的指针所在槽位均含不可考标记与非空原因。失败：任一槽位为空，或指针解析失败却无不可考标注，或不可考后无原因
review status: PASS (SUGGESTION_APPLIED)

CASE-012
mode: blackbox
description: RQ-3 守卫在交付树上基线 PASS，且人为删除 workflow 面一行（§2.3 枚举、当前未实现的 status 行）后守卫转为 FAIL 并点名缺失项
procedure: cp -R <MAIN> /tmp/qa-guard-12；在副本先跑基线：cd /tmp/qa-guard-12 && go test ./internal/validate/...，记录结果；随后在副本 route-matrix.md 中整行删除 status 行，再次 go test ./internal/validate/...，记录 exit code 与输出中是否点名 status；rm -rf /tmp/qa-guard-12（篡改只发生在临时副本，交付树不动）
oracle: PASS：基线 exit 0 且含 ok formal-gates/internal/validate；删行后 exit 非 0、输出含 FAIL 且失败信息出现 status 字样。失败：基线即 FAIL，或删行后仍 exit 0/无 FAIL（义务漏装不被机器查出）
review status: PASS

CASE-013
mode: blackbox
description: 人为删除维护/transport 面的 rollback 行后 RQ-3 守卫转为 FAIL 并点名缺失条目
procedure: cp -R <MAIN> /tmp/qa-guard-13；在副本 route-matrix.md 中整行删除维护面 rollback 行；cd /tmp/qa-guard-13 && go test ./internal/validate/...，记录 exit code 与输出；rm -rf /tmp/qa-guard-13
oracle: PASS：exit 非 0、输出含 FAIL 且点名 rollback（或明确指维护面枚举缺失）。失败：exit 0 或无 FAIL——维护面条目缺失不被查出
review status: PASS

CASE-014
mode: blackbox
description: 人为移除 future 行的计划未枚举标注后 RQ-3 守卫转为 FAIL（超枚举行必须带标注才允许存在）
procedure: cp -R <MAIN> /tmp/qa-guard-14；在副本 route-matrix.md 的 future 行中仅删除字面文本计划未枚举（行本身与其余列保留）；cd /tmp/qa-guard-14 && go test ./internal/validate/...，记录 exit code 与输出；rm -rf /tmp/qa-guard-14
oracle: PASS：exit 非 0、输出含 FAIL 且失败信息指向缺少计划未枚举标注（含 future 或标注字样）。失败：exit 0 或无 FAIL——无标注超枚举行静默通过
review status: PASS

CASE-015
mode: blackbox
description: 人为清空 workflow 面某行的必需列取值（abort 行的错误码列）后 RQ-3 守卫转为 FAIL
procedure: cp -R <MAIN> /tmp/qa-guard-15；在副本 route-matrix.md 中定位 abort 行，将其错误码列取值替换为空字符串（保留行列结构）；cd /tmp/qa-guard-15 && go test ./internal/validate/...，记录 exit code 与输出；rm -rf /tmp/qa-guard-15
oracle: PASS：exit 非 0、输出含 FAIL 且失败信息指向必需列缺失（含 abort 行或错误码列指认）。失败：exit 0 或无 FAIL——必需列被删空不被查出
review status: PASS

CASE-016
mode: blackbox
description: 人为删除 stage-records.md 阶段 1 节第 6 项（资源 cleanup receipt）后 RQ-3 守卫转为 FAIL 并点名 stage-records 缺项
procedure: cp -R <MAIN> /tmp/qa-guard-16；在副本 stage-records.md 阶段 1 节删除第 6 项（资源 cleanup receipt）整条内容；cd /tmp/qa-guard-16 && go test ./internal/validate/...，记录 exit code 与输出；rm -rf /tmp/qa-guard-16
oracle: PASS：exit 非 0、输出含 FAIL 且失败信息指认 stage-records（阶段 1 节或七项清单）缺项。失败：exit 0 或无 FAIL——阶段记录缺必需项不被查出
review status: PASS

CASE-017
mode: blackbox
description: 在临时副本的 incremental-seal-plan.md §2.3 命令枚举中追加计划外名字后 RQ-3 守卫转为 FAIL（§2.3 枚举与测试内固定清单双向绑定）
procedure: cp -R <MAIN> /tmp/qa-guard-17；在副本 refactor-plan/incremental-seal-plan.md 的 §2.3 枚举句中在 `seal` 后插入 、`zz-fake-cmd`（sed/perl 均可，只改枚举句）；cd /tmp/qa-guard-17 && go test ./internal/validate/...，记录 exit code 与输出；rm -rf /tmp/qa-guard-17
oracle: PASS：exit 非 0、输出含 FAIL 且失败信息指认 §2.3 枚举与固定清单不一致（含 zz-fake-cmd 或枚举比对字样）。失败：exit 0 或无 FAIL——计划枚举漂移不被双向机制查出
review status: PASS

CASE-018
mode: blackbox
description: 在临时副本的 internal/cli/cli.go workflow 子命令注册表中注册新入口 zz-probe 而不给矩阵补行后，RQ-3 守卫转为 FAIL 并点名该入口（实际公开面 ⊆ 矩阵由源码注册表机械落实）
procedure: cp -R <MAIN> /tmp/qa-guard-18；在副本 internal/cli/cli.go 的 workflowSubcommands 映射中追加一行注册 "zz-probe": runWorkflowShow（不修改 route-matrix.md）；cd /tmp/qa-guard-18 && go test ./internal/validate/...，记录 exit code 与输出；rm -rf /tmp/qa-guard-18
oracle: PASS：exit 非 0、输出含 FAIL 且失败信息点名 zz-probe 缺矩阵行（或指注册表/实际面与矩阵不一致）。失败：exit 0 或无 FAIL——新增子命令不进矩阵不被查出，先覆盖后实现防线失效
review status: PASS

CASE-019
mode: blackbox
description: 交付不修改三份受保护文档原文与引擎代码——相对本 run base snapshot be6a787，四个受保护路径的 diff 为空
procedure: 在 MAIN 执行 git diff --stat be6a787..HEAD -- refactor-plan/incremental-seal-plan.md refactor-plan/final-implementation-draft.md openspec/changes/orchestration-pipeline-engine/master-requirements.md internal/engine；另用 git log --oneline be6a787..HEAD -- 同四路径确认零提交触及
oracle: PASS：--stat 输出为空且 log 为空（三份文档与 internal/engine 字节未动）。失败：任一路径出现 diff 行或触及提交——受保护原文/引擎代码被改动，违反验收标准 4/5
review status: PASS

CASE-020
mode: blackbox
description: 交付不破坏文档化正常入口——构建后 --help、--version、workflow --help 正常返回，package validate 以 PASS 收尾并输出含 digest 的校验 receipt
procedure: 在 MAIN 执行 go build -o bin/formal-gates ./cmd/formal-gates；依次运行 ./bin/formal-gates --help、--version、workflow --help，各记录 exit code 与首行；再运行 ./bin/formal-gates package validate --root <MAIN>，记录 exit code、首行与 JSON receipt 中的 digest 字段
oracle: PASS：--help exit 0 且首行为 Usage: formal-gates <command>、清单含 workflow/gate/hook/lifecycle/canary；--version exit 0；workflow --help exit 0 且含 Subcommands: 行；package validate exit 0、首行为 PASS formal-gates package validation、receipt 含非空 digest。失败：任一命令 exit 非 0、usage 缺命令行、package validate 输出 FAIL 或 receipt 无 digest
review status: PASS

