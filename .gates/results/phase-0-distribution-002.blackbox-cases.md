# Blackbox QA cases (run: phase-0-distribution-002)

Derived from the run state at qa-design record time. Review these cases against the current confirmed requirement.

CASE-001
mode: blackbox
description: 冻结候选包具有可复核的 VCS/package/installed identity，且 stable、candidate、开发 worktree 与 host/state/resource/registry canonical paths 互不重叠；候选运行时不读取安装后继续变化的开发区。
procedure: 在独立测试项目按文档从已提交候选构建并安装；用 package baseline 留存 VCS/路径清单，从实际 installed binary 运行 package validate 与 canary portable；安装后改变开发 worktree，再从 installed path 重跑并保存 install/baseline/canary receipts、digest、Lstat/realpath 和目录证据。
oracle: 命令均成功；receipt 含冻结 VCS、source/package/installed digest、host/config/state/resource/registry canonical paths 与 disjoint proof；目标不是 live symlink，开发区变化不改变 installed digest/行为；回指、重叠、digest 不符或只能从 worktree 运行即 FAIL。
review status: PASS

CASE-002
mode: blackbox
description: 首次 bootstrap 只登记文档化 target/host/root/state/resource/runtime 记录，提交 bootstrap receipt 后 stable launcher 才允许首次 workflow 写入。
procedure: 在全新隔离 host/project/state/resource/registry namespace，用冻结 stable installed binary 的 install --bootstrap 执行 bootstrap；保存 receipt 并检查 bootstrap 前后 state/runtime/registry；随后用同一 stable binary 带 registry/record 执行 workflow start --split no，再 workflow show。
oracle: receipt 含 target/scope/host/project/state/resource/runtime、epoch/generation、lease/token、identity 且原子提交；bootstrap 不创建 .gates/tmp/state；成功 receipt 后首次 start 才在登记 namespace 写 state；失败不得留下半记录。
review status: PASS

CASE-003
mode: blackbox
description: 首次 bootstrap 在全新 namespace 中允许 registry 根不存在并原子创建登记；仅在已有 registry 的 record 缺失、冲突、不可对账或旧 target 无法迁移时返回 disabled/UNREGISTERED_INSTALL，且失败入口在写 state 前拒绝。
procedure: 分别准备两类隔离 fixture：A 为全新 host/project/registry namespace，registry root 不存在且无既有 record；B 为 registry 已存在但 target record 缺失、canonical path 冲突或不可对账，以及旧裸 target。用冻结 stable installed binary 执行文档化 install --bootstrap。A 保存 bootstrap 前后 registry/record/state/resource/launcher 快照，随后用同一 launcher 执行 workflow start --split no 和 show；B 为每个场景保存退出码、receipt、registry/target/host/project/state/resource bytes 与 digest，随后尝试 workflow start，检查 .gates/tmp、state、registry、lease/token 和旧 target 的迁移结果。
oracle: A 必须成功并生成 machine-readable bootstrap receipt，包含 target/scope/host/project/state/resource/runtime、epoch/generation、lease/token、canonical-path 与 identity；bootstrap 本身不得创建 .gates/tmp 或 workflow state，首次 start 只能在 receipt 提交后写 state。B 必须非零或返回文档化 disabled，并生成含 UNREGISTERED_INSTALL、原因、target/scope/path 的 receipt；不得创建 .gates/tmp、state/resource、lease/token、半提交 registry 或回退 generation，既有 authoritative bytes 保持不变。旧 target 只能迁移到 launcher 或保持 disabled；后续 start 必须在写入前硬拒绝。把首次 registry 缺失当作失败、允许已有异常 registry 写入或留下混合/半记录均为 FAIL。
review status: PASS

CASE-004
mode: blackbox
description: 文档化 global/project 与支持 host 的安装入口落到正确 immutable runtime、hook/config、managed-rule、registry，并保留无关宿主内容。
procedure: 为 claude/codex/cursor/dsh 准备隔离 global/project，预置外部 hook、插件行和规则区外文本；按安装文档安装候选，运行 installed package validation、portable/install smoke，读取 hook/config/rule 和 receipt。
oracle: 可执行 host/scope 的目标路径正确且与 stable/worktree disjoint；只改 formal-gates 自有内容并保留外部内容；Cursor global 不生成全局规则、DSH project 不写 home hook；receipt/registry含真实路径/digest；不支持平台仅记录 P3。
review status: PASS

CASE-005
mode: blackbox
description: --skip-hooks 安装运行时但不改宿主 hook 配置。
procedure: 隔离 fixture 预置含外部内容的 host config，记录 bytes/权限/mtime；执行 install --skip-hooks --force，从 installed path 跑 package validate、portable canary、CLI smoke，保存 receipt。
oracle: 运行时/manifest/digest/smoke 成功；hook/config bytes、权限、mtime不变，receipt 明确 skip；任何意外 hook/rule 变化、worktree目标或不完整安装即 FAIL。
review status: PASS

CASE-006
mode: blackbox
description: uninstall 只移除安装器拥有的 runtime、hook 条目和 marker 规则区块，保留外部内容，重复卸载幂等。
procedure: 先安装并记录 hook/config/rule/runtime，预置外部内容；调用相同 host/scope/project 的 uninstall，保存 uninstall/recovery receipt、快照和 registry，再重复卸载并运行旧 stable smoke。
oracle: 首次只删自有内容并逐字节保留外部内容；重复卸载稳定成功/幂等，无临时/备份/错误 registry；stable smoke仍通过。
review status: PASS

CASE-007
mode: blackbox
description: 安装事务在文档化 FORMAL_GATES_INSTALL_FAULT 故障点 intent|prepared|switched|hook|managed-rule|post-switch-smoke 及真实 malformed hook JSON 失败时，回滚旧 stable runtime、pointer/config、hook、managed-rule 和 registry，并写 recovery receipt。
procedure: 记录旧 stable identity 与 runtime/pointer/config/hook/rule/registry bytes；对每个文档化 fault label 从 installed stable launcher 执行 install --force，另以公开入口提供 malformed hook JSON fixture；保存命令结果、journal、failure/recovery receipt、新旧 digest和临时/备份，随后 reconcile 并跑旧 stable package/canary/CLI smoke。
oracle: 每个 fault 在文档化阶段前非零；旧对象恢复原字节，receipt含operation/phase/observed/reconcile/recovered identity/digest，generation/epoch不回退；无残留或混合安装，旧 stable smoke通过；不得使用未公开的 pointer-config-commit/registry-commit selector。
review status: PASS

CASE-008
mode: blackbox
description: 安装进程在 intent/prepared/switched journal 窗口崩溃后，受支持入口自动对账恢复旧稳定包。
procedure: 用 QA harness 在持久 journal 的各边界终止 installer（不手改 journal）；保存 journal及外部快照；重新执行 install --force 或 uninstall，检查 recovery receipt、stable smoke 和残留。
oracle: 重启先 observe/reconcile，恢复旧 runtime/pointer/config/registry，写含 phase/observed/reconcile/recovered identity 的 recovery receipt；registry epoch/generation单调递进不回写旧值；清理 temp/backup/journal，不把未完成事务标 committed。
review status: PASS

CASE-009
mode: blackbox
description: 未来版本化 engine/candidate surface 的合法 envelope 固定 stateSchemaVersion、workflowDefinitionVersion、source 和 definition digest，并严格执行文档化的 definition/schema bump 规则。
procedure: 在独立 candidate namespace 中，以 installed package 的文档化公开 envelope 生成/查看入口分别生成同一 definition 两次、按文档化规则改变 definition、改变 schema 的 envelope；保存每次命令、退出码、完整 envelope、source 追溯、definition digest、版本字段、前后 bytes/digest/mtime/权限及目录证据。
oracle: 每个合法 envelope 都成功生成并明确报告四个冻结字段；source 可追溯且 definition digest 可独立复算；相同 definition 的两次结果版本与 digest 稳定，definition/schema 变化只按文档化规则提升相应版本、无额外递增或回退；任一字段缺失、digest 不一致、bump 错误或仅能从 worktree 生成均 FAIL。
review status: PASS

CASE-010
mode: blackbox
description: candidate installed binary 只能在 candidate namespace 做验证；其 bootstrap、workflow start、resume、seal 和其他写入 stable 正式 run 的尝试都必须在写前拒绝，stable driver 继续独占 stable run、registry 与权威 Seal。
procedure: 用冻结 stable installed binary 在 stable host/project/registry 完成 bootstrap 并建立活动 stable run。攻击前保存 stable state、host hook/config、pointer/current、runtime、resource、registry authoritative record 与 receipts 的完整 bytes、digest、epoch/generation 快照。再从不可变 candidate installed path 指向 stable namespace 依次尝试 install --bootstrap、workflow start、workflow resume --run-id <stable-run>、documented workflow abort --user-confirm 写入 stable run，以及 documented workflow seal 为该 stable run 签发权威 Seal；保存每次命令、receipt、退出码，并在攻击后重新保存同一整套 stable 快照。随后只在独立 candidate host/project/state/resource/registry namespace 从 candidate path 运行 regression、canary、install smoke；最后用 stable binary 执行 workflow show 和 resume，分别保存两侧 namespace 与 identity 证据。
oracle: candidate 的 bootstrap、start、resume、abort、seal 都必须非零或返回明确的 unauthorized/candidate-writer 拒绝，并在写入前结束；不得出现 candidate 写出的 SEALED summary、seal receipt 或任何权威 Seal 证据。攻击前后 stable state、host/config/hook、pointer/current、runtime、resource、authoritative registry record、epoch/generation 与 stable receipts 必须逐字节/逐 digest 保持不变；不得出现 candidate receipt、lease、state 或 registry 写入。candidate 证据只能出现在 candidate namespace；stable binary 仍可 show/resume 且继续是唯一 stable writer。任何 candidate 修改 stable bytes、取得 stable writer、签发权威 Seal、跨 namespace 写入或以 candidate 驱动正式 run 均为 FAIL。
review status: PASS

CASE-011
mode: blackbox
description: stable 安装包文档与 help 遵守 requirements-precedence/supersession，阶段0不泄漏后续 drive/submit 或 engine/Shadow。
procedure: 从 installed path检查precedence inventory、SKILL、README、stage-0 prompts，并运行CLI/workflow/package/install help与package validate；按当前文档尝试阶段0 public entry，保存命令目录/标签/help。
oracle: package validate通过；inventory含current-authority/reference/orthogonal/superseded/historical；stable docs/help只宣称阶段0/legacy合同，文档/help一致，未来语义泄漏即FAIL。
review status: PASS

CASE-012
mode: blackbox
description: 真实 host payload 下，PreToolUse 依据 development-worker 的开发开始边界和 agent_type/host identity 决定 allow/block；活动 run 的重复 lifecycle 事件幂等，无活动 run 的 capture 独立丢弃。
procedure: 在已安装 stable binary 和可发出 live payload 的支持 host 上执行 workflow start，确认 development-worker 仍为 PENDING；发送不含 agent_type/agent_id 的主线程 PreToolUse 代码写入 payload，保存 host 原始 payload、决策 JSON 与退出码。通过文档化 prepare-action/claim-dispatch 让 development-worker 从 PENDING 进入 PREPARED，保存 dispatch、claim、host provider、agent_id 和 agent_type。边界后分别发送真实 host payload：claimed development-worker 对活动仓库的代码写入、主线程无身份的代码或 run-state 写入、qa-review agent_type 的代码或 run-state 写入；保存每个响应。随后用已 claim 的 host identity 发送成对 lifecycle start/stop 事件，逐字节重放相同事件各一次，运行 lifecycle verify，并分别保存事件文件、计数、verify/claim receipt；另用未 claim identity 发送事件并验证。最后在第二个全新且无活动正式 run 的 project/host fixture 中发送 lifecycle capture start/stop，保存 capture 前后 lifecycle 目录和状态快照。
oracle: development-worker 尚未进入 PREPARED 时主线程写入必须 allow；进入 PREPARED 后，claimed development-worker 必须 allow，主线程和 qa-review 必须 block，并符合所选 host 的真实决策协议（Codex block 为 decision=block 且 hook 退出码为 0）。已 claim identity 的活动 run start/stop 必须得到 lifecycle VERIFIED；相同 host event identity 的重复事件不得新增观测、文件或计数，未 claim identity 不得 VERIFIED。无活动 run 的 capture 必须是文档化 no-op/UNAVAILABLE，且不创建 lifecycle 日志、state 或 run 记录。静态配置、模拟 payload、把活动重复与无活动 capture 混为同一判据均为 FAIL。
review status: PASS

CASE-013
mode: blackbox
description: package validation 对常见错误输入逐项执行 Lstat、realpath、disjoint 和 digest 校验，并拒绝 symlink、回指开发/stable、路径重叠、非regular entry、unknown target 与 digest mismatch。
procedure: 在隔离包分别准备 symlink、回指、重叠、非regular、manifest/registry未登记 unknown target 和 digest mismatch fixture；只用公开 package validate/install，保存退出码、machine receipt、原因、前后 target/registry/state及残留扫描；清理后换回正常immutable package并跑installed smoke。
oracle: 每个错误包（含unknown target）在候选/state前非零并明确原因；不生成可用target/pointer/config/registry/state或残留；正常immutable package随后安装成功且installed smoke通过。
review status: PASS

CASE-014
mode: blackbox
description: Go installer、install.command、install.ps1（支持的平台）共享同一 native transaction owner、lock/journal/receipt。
procedure: 在同一 fixture 依次用三入口安装/替换并注入fault，再卸载；收集owner、journal/recovery/install receipt、runtime/pointer/config/hook/rule/registry digest、临时/备份和installed smoke；不支持平台记录P3。
oracle: 三入口事务顺序和receipt schema等价；失败恢复旧stable bytes并可reconcile；脚本不先删release/切pointer/写registry/绕journal；支持性限制不冒充PASS。
review status: PASS

CASE-015
mode: blackbox
description: 安装替换的 --force 与非 --force 语义稳定、幂等，不把另一 host 安装当回退。
procedure: 先装stable；同一host/scope/project不带--force重装候选，另用不同host global target尝试；保存结果和digest；再用--force连续安装两次，跑package/canary/smoke并读hook/rule。
oracle: 无--force对已存在目标明确拒绝且旧stable全字节不变；不同host不作为替换回退；两次--force最终仅一个完整candidate、无重复hook/rule、digest稳定、receipt committed、smoke通过。
review status: PASS

CASE-016
mode: blackbox
description: 并发 install/uninstall 在跨进程 lock 下串行化并保持最终状态原子可对账。
procedure: 隔离已安装目标同时启动install --force、uninstall及第二个install，记录进程结果、journal/lock/recovery receipt与registry/config/runtime bytes；结束后重新registry/show、package/canary/smoke并查临时/备份。
oracle: 无死锁/崩溃/半提交；每个操作完整提交或机读busy/failure并可reconcile；最终runtime/pointer/config/hook/rule/registry/receipt属于单一identity，无混合、重复marker、残留或错误state。
review status: PASS

CASE-017
mode: blackbox
description: uninstall/delete 事务在文档化故障控制的 intent、managed-rule、hook 和 post-switch-smoke 删除窗口逐点失败时可恢复旧 stable package，不留下半删除 runtime、hook、managed-rule 或 registry。
procedure: 在隔离 host/scope/project 先用 stable installed binary 安装并 bootstrap，预置外部 hook/规则内容并记录 runtime、pointer/config、hook、managed-rule、registry bytes；分别用 FORMAL_GATES_INSTALL_FAULT=intent|managed-rule|hook|post-switch-smoke 调用公开 uninstall，保存退出码、journal、failure/recovery receipt和bytes；随后通过受支持入口 reconcile，运行旧 stable package/canary/CLI smoke。
oracle: 每个删除 fault 非零并生成 operation=uninstall 的机读 receipt，含 phase/observed/reconcile/recovered identity；旧对象恢复原字节、外部内容逐字节保留、generation/epoch不回退；无 temp/backup/lock/journal/半删除target，旧 stable smoke通过；无 fault 完整/重复卸载仍由 CASE-006 覆盖。
review status: PASS

CASE-018
mode: blackbox
description: 公开 pointer/config commit-failure fixture 触发安装替换事务的原子提交失败时，旧 stable runtime、pointer/config、hook、managed-rule 和 registry authoritative record 完整恢复，并写可对账 recovery receipt。
procedure: 在隔离 host/scope/project 先用 stable installed package 完成 bootstrap 和基线安装，保存旧 runtime、pointer/current、config、hook、managed-rule、registry record、journal/receipt 的 bytes、digest、epoch/generation。只从 installed package 的 install --help 与维护文档取得公开的 pointer/config commit-failure fixture 及 exact invocation；若文档/help 不公开该 fixture，记录命令证据并判定失败。用该公开 fixture 通过受支持 install --force 入口触发 pointer/config 提交失败，保存命令/退出码、fixture invocation、journal phase、failure/recovery receipts、旧新 bytes/digest、临时与备份扫描。随后经受支持入口 reconcile，再运行旧 stable package validate、portable canary、CLI smoke 和 workflow show/resume。
oracle: fixture 必须在 post-switch/pre-commit 的 pointer/config commit 阶段确定性失败并返回非零；旧 runtime、pointer/current、config、hook、managed-rule 与 registry authoritative record 必须恢复为原 bytes/digest，recovery receipt 至少含 operation、phase、observed fact、reconcile action、recovered identity/digest、generation/token，generation/epoch 只能单调前进。不得留下混合安装、错误 current、未清理 temp/backup/journal 或 candidate writer；旧 stable smoke 与 workflow 继续通过。fixture 不可由公开文档/help 发现、使用未公开 selector、未命中 pointer/config 阶段或未恢复旧对象均为 FAIL。
review status: PASS

CASE-019
mode: blackbox
description: 公开 registry commit-failure fixture 触发安装事务 registry record 原子提交失败时，旧 stable runtime、pointer/config 与 registry authoritative bytes 完整恢复，并写可对账 recovery receipt。
procedure: 在独立 host/scope/project 用 stable installed binary 完成 bootstrap 和基线安装，保存旧 runtime、pointer/current、config、hook、managed-rule、registry record、journal/receipt 的 bytes、digest、epoch/generation。通过 installed package 的 install --help 与维护文档发现并记录公开的 registry commit-failure fixture 及 exact invocation；缺少公开入口即记录证据并判定失败。用该 fixture 通过受支持 install --force 入口触发 registry record 提交失败，保存命令/退出码、journal phase、failure/recovery receipts、旧新 runtime/pointer/config/registry bytes 与 digest、临时/备份扫描。随后经受支持入口 reconcile，并运行旧 stable package validate、portable canary、CLI smoke、workflow show/resume。
oracle: fixture 必须在 registry commit 阶段确定性失败并返回非零；旧 runtime、pointer/current、config、hook、managed-rule 与 registry authoritative record 必须恢复为原 bytes/digest，recovery receipt 含 operation、phase、observed fact、reconcile action、recovered identity/digest、generation/token，generation/epoch 不得回退。不得留下 candidate registry record、混合 pointer/config、半提交 runtime 或 temp/backup/journal 残留；旧 stable smoke 与 workflow 继续通过。fixture 不可由公开文档/help 发现、使用未公开 selector、未命中 registry commit 阶段或未恢复旧 registry/runtime 均为 FAIL。
review status: PASS

CASE-020
mode: blackbox
description: 冻结 stable installed driver 的 legacy workflow 正常生命周期产生独立可复核的 characterization 基线。
procedure: 在全新隔离 host/project/state/resource/registry namespace，从冻结 stable installed binary 按当前阶段文档完成 bootstrap；仅执行文档化 legacy 正常生命周期（workflow start --split no、show、resume），逐步保存命令、退出码、响应、state/resource/registry bytes/digest、receipt、terminal summary 及 stable/package/installed identity；通过文档化 characterization/baseline 入口生成并保存基线结果。
oracle: bootstrap 后 legacy start/show/resume 全部成功，stable driver 是唯一 writer；characterization 结果逐步记录当前文档化行为、状态/资源/registry/receipt 与 terminal summary，并绑定同一冻结 VCS/package/installed identity；state/resource/registry 仍为当前 legacy 格式，未混入 future engine/Shadow 语义。缺少可复核步骤结果、identity 不一致、写入未登记 namespace、只能从 worktree 运行或 legacy 行为改变均 FAIL。
review status: PASS

CASE-021
mode: blackbox
description: 版本兼容且 envelope 完整的 surface 通过文档化写入口时被正向接受，并提交其预期内容/state 变化。
procedure: 在独立 candidate namespace 生成文档化支持版本的完整合法 envelope 和 payload，记录写入目标、既有 state/resource/registry/receipt 的 bytes、digest、mtime、权限；通过 installed package 的文档化公开写入口提交一次，保存命令、退出码、响应/receipt、提交后目标及相关 state/resource/registry 快照，并按文档执行一次同内容重放（若定义了幂等语义）。
oracle: 写入口必须返回文档化成功/committed 结果，不得返回 UNSUPPORTED_RUN_VERSION；文档化目标必须按 create/update 语义实际出现或更新，并包含所提交的四字段 envelope、payload 与匹配 digest/version；前后差异仅限文档化写入对象及其 receipt/索引，不能只调用成功而没有可观察提交；重放遵守文档化幂等/更新语义且不产生重复或不一致内容。部分写入、字段被改写、digest/version 不匹配或合法输入被一律拒绝均 FAIL。
review status: PASS

CASE-022
mode: blackbox
description: 缺失字段或版本不兼容的 future surface 在写入前被拒绝，不改变既有内容。
procedure: 在同一独立 candidate namespace 准备分别缺失四字段之一、stateSchemaVersion 不匹配、workflowDefinitionVersion 不匹配及其他文档化不兼容 envelope；逐项通过 installed package 的公开写入口调用，保存命令/退出码、错误/receipt、目标及 state/resource/registry 的前后 bytes、digest、mtime、权限，并扫描事务临时目录。
oracle: 每项必须在新增或修改 bytes 前返回非零或文档化 UNSUPPORTED_RUN_VERSION；既有目标、envelope、state/resource/registry、generation/epoch、receipt 与权限/mtime 保持原样，不生成可用 candidate record、lease/token、半提交 pointer 或残留 temp/backup；任何先写后报错、静默降级接受、回写迁移或混合状态均 FAIL。
review status: PASS

CASE-023
mode: blackbox
description: diagnose 对 raw/envelope 与可读 terminal summary 只读解析，在 malformed 输入上给出文档化诊断并保留 terminal summary fallback。
procedure: 在独立 namespace 准备可读 terminal summary、合法 raw/envelope、缺字段/版本不匹配和 malformed raw fixtures；逐项通过 installed package 文档化 diagnose --path 公开入口执行，保存报告/退出码、stdout/stderr、fixture 与所有目标/目录的 bytes、digest、mtime、权限前后快照，并检查 terminal summary 文件。
oracle: 合法输入报告四字段及可复算 digest/来源；可读 terminal summary 在报告中被保留并可回退展示；malformed/不兼容输入返回文档化诊断而非静默成功；所有 diagnose 调用均不迁移、修复、清理、重写或改变任何 fixture、state、registry、receipt、mtime、权限或目录。任何只读检查产生写入、丢失 terminal fallback 或把错误输入当合法均 FAIL。
review status: PASS

CASE-024
mode: blackbox
description: stable installed driver 对既有 legacy run 继续使用当前 state 格式和写入语义，不因 future surface 诊断或写入而迁移、回写或升级 legacy 状态。
procedure: 在独立 legacy project/host/state/resource/registry namespace 预置当前格式的 legacy state 与 terminal summary；仅从冻结 stable installed binary 执行文档化 workflow start --split no、show、resume 和 diagnose，并分别保存命令/退出码、state/resource/registry/receipt/terminal-summary bytes、digest、版本字段和目录快照；同时留存 future surface 操作前后的 legacy 快照。
oracle: legacy 正常入口均成功且 stable driver 是唯一 writer；状态继续保持当前 legacy 格式和既有写入语义，terminal summary 可读；future surface 的 diagnose/写入不得添加 envelope、改变版本字段、迁移或回写既有 state，不得改变未涉及 bytes。任何自动迁移、版本升级、旧格式被拒绝或 stable 入口行为改变均 FAIL。
review status: PASS

CASE-025
mode: blackbox
description: 阶段 0 characterization 的候选、测试项目、state、resource、registry 与 evidence 目录具有独立公开的层级、canonical-path 和 identity 证据。
procedure: 在全新隔离 namespace 通过文档化 baseline/characterization evidence 入口完成一次 legacy 基线；分别对 candidate、test-project、state、resource、registry、evidence 目录及 host/config、managed-rule、runtime 目标执行 Lstat/realpath/digest 采集，保存目录树、命名/层级、VCS/package/installed identity 和 receipts。
oracle: 每个要求目录都存在于文档化层级并有 machine-readable canonical path、Lstat/realpath 与 digest；evidence 与 candidate/test-project/state/resource/registry 绑定同一 VCS/package/installed identity，stable、candidate、开发 worktree 及各 namespace 互不重叠且无回指；证据不落到未登记路径、不缺目录/identity、不混用不同安装包。任何越界、回指、identity 漂移、目录层级/命名不符或缺少可复核证据均 FAIL。
review status: PASS

CASE-026
mode: blackbox
description: frozen stable installed package 的 package validation 形成独立 baseline 结果，覆盖每个安装输入的 Lstat、realpath disjoint 和 digest 校验。
procedure: 从冻结 stable installed path 在隔离 baseline namespace 执行文档化 package validation 公开入口；保存命令/退出码、machine-readable validation receipt、每个 runtime/prompts/gates/hooks/rules 输入的 Lstat/realpath/manifest/digest、VCS/package/installed identity 与前后目录快照。
oracle: validation 成功并逐项报告全部输入的 regular/Lstat、realpath disjoint proof、manifest/digest 及 identity；稳定目标不是 live symlink，canonical paths 与 stable/candidate/worktree/host/state/resource/registry 不重叠；validation 本身不写 state/registry 或修改输入。缺项、digest 不符、回指/重叠、只能从 worktree 验证或无独立 machine receipt 均 FAIL。
review status: PASS

CASE-027
mode: blackbox
description: frozen stable installed path 的 portable canary 在隔离测试项目中独立成功，证明候选运行不依赖可变开发区。
procedure: 在独立 test-project、host/config、state/resource/registry namespace，从冻结 stable installed path 执行文档化 portable canary 入口；保存 canary receipt、VCS/package/installed identity、canonical paths、目录证据和退出码；安装后对开发 worktree 做文档化允许的变化，再从同一 installed path 重跑 canary。
oracle: 两次 portable canary 均成功并生成绑定同一冻结 identity 的 machine-readable receipt；运行只读取 installed immutable runtime/prompts/gates/hooks/rules，不回指或读取安装后变化的 worktree，candidate/test-project 与 stable namespace disjoint；可变开发区导致 installed digest/行为变化、只能从 source tree 运行或缺少 receipt 均 FAIL。
review status: PASS

CASE-028
mode: blackbox
description: clean-host 的 installed-binary install smoke 作为独立阶段 0 基线结果成功并绑定安装 identity。
procedure: 在全新隔离 host/project/registry/state/resource namespace，从冻结候选 artifact 按文档执行公开 install smoke 入口（不把 package validation/canary 的结果代替本步骤）；保存 smoke 命令/退出码、installed binary 实际路径、runtime/pointer/config/hook/managed-rule/registry canonical paths、receipt、digest 与残留扫描。
oracle: install smoke 必须从实际 installed path 成功启动并完成文档化最小安装/运行流程，receipt/registry 绑定同一 VCS/package/installed identity；runtime、pointer/config、hook/rule、registry 为完整单一 identity，目标与 worktree/stable disjoint，无 temp/backup/journal 残留或未登记写入。仅 source-tree 运行、借用其他 smoke/validation 结果、路径回指、混合 identity 或残留均 FAIL。
review status: PASS

CASE-029
mode: blackbox
description: 安装事务进入真实 copy 阶段后，runtime、prompts、gates、hooks、rules 任一文档化中途 copy 失败都能恢复旧 stable。
procedure: 在隔离 host/scope/project 先用 stable installed binary 完成 bootstrap 和基线安装，保存旧 runtime、pointer/config、host hook、managed-rule、registry authoritative bytes/digests、generation/epoch 与 identity。只从 installed package 的 install help/维护文档取得公开 copy-failure fault fixture；对 runtime、prompts、gates、hooks、rules 各自逐项触发事务已开始且先前/部分 copy 已发生后失败的文档化故障，保存命令/退出码、journal observed phase/component、failure/recovery receipt、临时 sibling/manifest 快照和新旧全量 bytes/digests；随后经受支持入口 reconcile 并运行旧 stable validation/canary/CLI/workflow smoke。
oracle: 每项均在 copy/component 中途确定性非零失败，journal/receipt 证明已越过 prepared 并实际进入对应 copy 阶段，而非仅输入预校验拒绝；未提交 candidate current/pointer/config/registry，旧 stable runtime、pointer/config、hooks、rules、registry authoritative record 恢复原 bytes/digest，generation/epoch 只单调前进；reconcile 后无混合 runtime、半写 marker、candidate writer、temp/backup/lock/journal 残留，旧 stable smoke 与 workflow 继续成功。缺少公开 fixture、未命中真实 copy 阶段、只测 prepared 边界或恢复不完整均 FAIL。
review status: PASS

CASE-030
mode: blackbox
description: 安装事务完成 copy 后在 manifest/realpath/digest verify 阶段真实失败时，旧 stable runtime、pointer/config、hooks、rules 与 registry 完整恢复。
procedure: 在隔离 host/scope/project 完成 stable 基线安装并留存旧全量 bytes/digests；只从 installed package 的 install help/维护文档取得公开 verify-failure fixture，分别对 manifest、realpath/disjoint、digest 按文档化 fixture 触发 copy 已完成、切换/公共提交前失败的 install --force，保存命令/退出码、journal observed phase、failure/recovery receipt、临时 sibling manifest/verify 证据及新旧 runtime/pointer/config/hook/rule/registry bytes/digests；随后经受支持入口 reconcile，运行旧 stable package validate、portable canary、CLI/workflow smoke。
oracle: 每项必须在 copy 完成后的 verify 阶段确定性非零失败，journal/receipt 明确 phase 与失败事实，不能降级为 prepared/input-validation；current/pointer/config、registry authoritative record、hook/rule 不能提交 candidate，旧 stable 所有对象恢复原 bytes/digest，generation/epoch 不回退；无混合安装、半提交 registry、candidate lease/record、temp/backup/lock/journal 残留，reconcile 后旧 stable smoke 继续通过。fixture 不可由公开 help/docs 发现、未命中 verify 阶段或任一恢复/证据缺口均 FAIL。
review status: PASS

CASE-059
mode: blackbox
description: 跳过 bootstrap 的常见操作失误下，未登记安装的首次 workflow 写入在创建 .gates/tmp 前被硬拒绝并留下 machine-readable disabled/UNREGISTERED_INSTALL 拒绝证据；补做文档化 install --bootstrap 后，同一入口的首次 workflow start 才成功写入登记 namespace。
procedure: 在全新隔离 host/project/state/resource/registry namespace（registry root 不存在且无任何既有 record）按文档安装候选包，但跳过 install --bootstrap；保存 .gates/tmp、workflow state、resource、registry 目录与 host hook/config 的前置 bytes/digest 快照。从实际 installed path 按文档执行 workflow start --split no，保存命令、退出码、stdout/stderr 与 machine-readable 拒绝 receipt/JSON，随后扫描 .gates/tmp、state、resource、lease/token 与 registry 是否出现任何新 bytes 或新创建的 registry root。最后在同一 namespace 从冻结 installed artifact 按文档执行 install --bootstrap，保存 bootstrap receipt，并重跑同一 workflow start --split no 与 workflow show，保存成功结果与 state/receipt 快照。
oracle: 未 bootstrap 的 start 必须非零或返回文档化 disabled，并在任何写入前结束；拒绝证据 machine-readable，含 UNREGISTERED_INSTALL、原因与 target/scope/path；前置快照逐字节不变，不得创建 .gates/tmp、workflow state、resource、lease/token、registry root 或任何 record（registry 的一次性创建只属于 bootstrap）。补做 bootstrap 成功提交 receipt 后，同一 start 必须在登记 namespace 成功写 state 且 show 可读。把未 bootstrap 的 start 当成功、写成才报错、拒绝缺少 machine-readable 证据、registry 被拒绝路径抢先创建，或 bootstrap 成功后仍拒绝首次写入，均 FAIL。
review status: PASS

CASE-060
mode: blackbox
description: 候选 installed binary 在独立候选 namespace 按文档执行 legacy 正常生命周期回归（workflow start --split no、show、resume）全部成功，可观察语义与 stable characterization 基线一致；其 run state、receipt 与 summary 只落在候选 namespace，不写 stable registry 或 stable 侧任何 bytes。
procedure: 在独立候选 host/config、test-project、state/resource/registry namespace（含文档化候选验证要求的显式 registry 配置与该 namespace 需要的登记步骤）从不可变 candidate installed path 依次执行文档化 legacy 正常入口 workflow start --split no、workflow show、workflow resume；逐步保存命令、退出码、响应、候选 state/resource/registry bytes/digest、receipt、terminal summary 与候选 VCS/package/installed identity，并与文档化 legacy characterization 基线结果逐步比对；执行前后保存 stable registry record、epoch/generation 与 stable namespace 的完整 bytes/digest 快照。
oracle: 候选 binary 的 start/show/resume 全部成功，逐步可观察行为与 stable legacy characterization 基线的文档化正常语义一致，全部证据绑定同一候选 identity；run state、receipt 与 summary 只出现在候选 namespace，stable registry record、epoch/generation 与 stable namespace bytes 逐字节不变，候选 registry/test-project 与 stable/开发 worktree 的 canonical paths 不重叠。任何入口失败、行为偏离基线、候选 run/receipt 写入 stable namespace 或被 stable 正式 run 当作权威状态读取、跨 namespace 写入或 identity 漂移均 FAIL。
review status: PASS

