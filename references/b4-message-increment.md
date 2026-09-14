# B4 message increment and validation

Bound candidate: `cf8764e46ab31a265755a896ec7a9366ffd7810e`.
Requirement revision: `14bccf5f1ac00fd612385207078a03cb7133a9788360303da15cdf03320b59fe`.

This increment removes repeated authoring/review instructions and selects blackbox
and whitebox review/execution sections through existing assembly. Historical runs
retain their captured unsplit instructions. Native structured-output instructions
now have one common owner; supplemental material instructions refer to actual
input paths rather than obsolete field names. Gate-specific copies of shared
reviewer obligations were removed. Contract tests now check the shared owner,
and actual assembly tests require each acceptance-surface obligation exactly once
in both gate inputs. No progress or cost implementation changes remain.

The candidate already had role-selected QA tool descriptions and a whitebox result
schema without `uniqueItems`; these were retained, not newly implemented here.

## Actual assembly measurements

One shared small fixture, production assembly, Unicode character counts. Before
and after use the same fixture and bound candidate. Temporary paths are freshly
allocated; their random suffix lengths can contribute a few characters to the raw counts. These are assembled inputs, not billed tokens or
real-host latency measurements. QA-review measurement uses the production prompt,
schema and tool assembly functions without issuing a second designer action.
Schema and tool columns show before/after separately; unchanged values are intentional.

| Stage | Message before | Message after | Schema before/after | Tool before/after |
|---|---:|---:|---:|---:|
| product-review | 3206 | 3150 | 1476/1476 | 983/983 |
| start-readiness | 4437 | 4330 | 2852/2852 | 983/983 |
| development-worker | 3601 | 3494 | 1536/1536 | 983/983 |
| blackbox-design | 6863 | 6792 | 5548/5548 | 983/983 |
| whitebox-design | 8691 | 7846 | 6375/6375 | 983/983 |
| whitebox-coverage | 1676 | 1676 | 355/355 | 0/0 |
| blackbox-review | 7047 | 6465 | 3281/3281 | 983/983 |
| whitebox-review | 8638 | 7811 | 3281/3281 | 983/983 |
| blackbox-execution | 5993 | 5900 | 2568/2568 | 2554/2554 |
| whitebox-execution | 5996 | 5881 | 2568/2568 | 2554/2554 |
| complexity-gate | 8537 | 8367 | 2568/2568 | 983/983 |
| implementation-quality-gate | 8416 | 8176 | 2568/2568 | 983/983 |
| repair-worker | 3859 | 3836 | 2263/2263 | 2554/2554 |

## Scope and continuation audit

`loadActionInstructions` binds the six action documents used by native workers;
`prepareNativeActionInput` selects review, preparation or verification assembly.
Both gates additionally receive the common reviewer and their own gate rules.
Coverage has its separate prompt/schema and no workflow tools. The matrix covers
all native step kinds dispatched through these assembly paths.

Requirement clarification is an owner/user confirmation operation in this regular
route (`RegularAvailableActions` and confirm-requirement REQUEST/DECIDE), not a
native worker invocation. `requirements-clarification.md`, `carry.md` and
`merge-gate.md` are not loaded by this native action loader. There is no corresponding
native message/schema/tool sample to count; no synthetic zero or model result is
reported for these operations. Existing confirmation behavior is covered by the
installed consumer fixture. These non-worker documents were inspected, not changed. The opt-in
`TestActualStagePromptPathsReadOnly` was skipped because no explicit formal-state
input was supplied; no active formal state was used as a fixture.

Development continuation uses the same developer section and schema, adding only
the captured previousDevelopmentResult. Design revision uses the same mode-specific
section with previousDesign/reviewGaps; review carries retainedReview. The existing
material binding tests cover exact paths, unrelated-body exclusion and retention.
QA continuation uses the same execution section and schema with a newly bound
remaining/checkpoint slice. The checkpoint replay fixture actually received three
turns, read each bound material file, retained one identity and executed C1/C2/C3
without rerunning previously completed cases. These are protocol-fixture results,
not independent semantic QA approvals or billed-token measurements.


## Checks completed

- `go build -o bin/formal-gates ./cmd/formal-gates`: passed with
  `GOCACHE=/tmp/formal-gates-b4-cache`.
- `bin/formal-gates package validate --root .`: passed.
- `bin/formal-gates canary portable --root . --format json`: exited successfully.
- `go vet ./...`: passed with the same cache.
- `go test ./internal/cost -count=1`: passed; existing incremental, owner interval,
  source-presence and unavailable serialization contracts retained.
- Targeted facade tests passed: stage assembly, Markdown selection, both QA design
  contracts, readiness/prestate checks, readonly review output, captured old rules,
  actual bound material paths, Codex coverage-to-authoring protocol, progress,
  wait timing, static checker, candidate source recovery and checkpoint material
  replay. The replay executed C1/C2/C3 in separate turns under one identity and read
  the remaining bound material without repeating prior cases.
- `git diff --check`: passed before this evidence note was added.

## Failure history and final verification

The first targeted run rejected the removal of native structured-output wording;
putting it in the shared section resolved that test. Early validate runs failed
because this checkout lacked `bin`; the documented build resolved that prerequisite.
The next tagged/race runs failed because two historical contract tests required
acceptance-surface clauses in both individual gate files. Tests now verify the
common owner and the actual assembled inputs, without weakening the obligation.
The repaired targeted tests, tagged validate suite and assembly measurement pass.

Early full facade runs reached Go's default ten-minute package timeout while still
progressing through tests. This is retained as a failed test run, not a product
failure. The final full regression uses `go test ./... -timeout 30m`; this affects
only Go's test-run limit and introduces no native model or workflow task budget.
The repaired race suite passed (`internal/validate` 64.271s, `internal/cli` 198.698s).
The first 30-minute-limit full run completed with failures (facade 576.218s):
three whitebox prompt assertions still expected wording removed by deduplication.
The mixed-file test now checks the retained Chinese clause; the short trial
instruction explicitly retains native-shell and not-formal-QA wording. The same
run's lifecycle test inherited this worker's CODEX_HOME and detected Codex despite
setting a ZCode marker. Running that test with CODEX_HOME/CODEX_CLI_PATH unset
passed (0.511s); no provider detection behavior was changed. The repaired whitebox/assembly targeted suite passed (30.654s). The final full run
uses this environment isolation. It passed with exit 0:
`env -u CODEX_HOME -u CODEX_CLI_PATH GOCACHE=/tmp/formal-gates-b4-cache go test ./... -timeout 30m`.
Packages without changes reused valid Go test cache entries; no fresh real-model
run or formal QA approval is implied.

`TestInstalledConsumerReadsBoundMaterialWithoutContext` passed through the normal
installed CLI. Its local Codex protocol endpoint receives actual turn/start input,
reads the referenced material and returns a result accepted by the engine. It
substitutes the model endpoint, so it does not establish a real model's reasoning
quality or a real-host full workflow PASS. No fresh full native workflow was added
solely for character measurement.

The existing public operations/script index remains in `references/engine-regular.md`:
installed package-root lookup, parameters, dependencies, version boundaries and
historical scripts requiring adaptation remain explicit. The static checker and
readiness tests passed; they parse scripts without importing or sourcing them and
exercise an unreachable terminal prestate. No new index or accounting system was
introduced.

Raw local logs are `/tmp/formal-gates-b4-*.log` (transient); character counts,
methods, sample boundaries and failure history are retained above.

## Final result

All required local checks passed: full tests, race tests for validate/CLI, tagged
phase0whitebox validate, vet, build, package validation, portable canary and diff
whitespace checks. The final targeted assembly suite also passed after the last
wording correction. The final canary initially reported a Cursor temporary
bootstrap identity mismatch while verification was in progress; a subsequent
run with files unchanged passed without any installation code change. The first
failure is retained and its root cause is not asserted.

The B4 development checks are complete. Formal review, independent QA, Seal and
release/install adoption remain engine-owned later actions; this document does
not claim they occurred. No unresolved implementation decisions remain.


### B4 全阶段提示词复核：尚未落实的事实

以下是对实际源码和派发输入的复核记录，范围仍为已确认 REQ-005。

| 项目 | 当前证据 | 状态 |
| --- | --- | --- |
| 旧章节兼容 | `preparation.go`、`verification.go`、`runtime_operation_tool.go` 仍根据旧正文是否存在某个标题选择回退路径。 | 新版接入后的清理尚未完成。 |
| 安装后的规则引用 | QA 审查和执行正文仍引用项目相对路径 `prompts/actions/qa-design.md`、`prompts/reviewer-base.md`，实际材料发布入口没有为外部项目提供这些绑定文件。 | 引用尚未解决；正文已含相应摘要的引用也仍保留。 |
| 不适用工具 | 产品审和技术审正文禁止请求辅助模型，装配却仍声明 `model.run` 及其工具说明。 | 规则与工具声明不一致。 |
| 需求澄清等包内指南 | `requirements-clarification.md` 重复全文/revision/确认步骤，并保留 RQ-011 登记集说明；未创建原生 worker 不等于该指南已完成内容优化。 | 全阶段核对尚未体现该指南的处置。 |
| 续做输入 | 同身份 B4 恢复曾再次发送 6,569 字完整角色消息；现有阶段计量主要覆盖首次装配。 | 仍需区分必要的恢复约束、可按需读取的内容及重复材料，不能把首次派发计量当作续做验证。 |

### 当前 B4 增量的接纳前复核

以下为原生开发身份 `01a09c9a-a4c4-7c03-8c4b-edd5aa74deb3` 在途改动的复核事实，尚非其最终交付结论：

- `runtime_input.go` 的续做消息将 `initialMessage` 指向 `prompt.txt`，重新把实际消息记录作为材料披露入口，不符合已确认的取消 prompt 中间层要求。已 claimed 的正常恢复指向当前 job 的同名文件，而 `runtime_bridge.go` 在发送前写入该文件；因此此路径会用续做消息覆盖所引用的原始消息，链接指向自身。历史实际消息记录与对模型提供的正式材料入口应区分，不能以另一份提示词副本替代直接材料来源。
- 同一改动在 `nativePromptMaterial.render` 内硬编码整段固定续做指令；项目规则要求固定内部指令由已有 Markdown 持有，代码只选取并组装动态材料。这一职责约束不能因缩短消息而丢失。
- 用户随后明确工具限制应采用黑名单方式。当前 WorkBuddy 的 `workBuddyRoleTools` 通过 `--tools` 枚举可用工具，ZCode 的 `zcodeRoleTools` 返回工具白名单；在途第二次尝试仍保留两处枚举。它们不是仅禁用写入等不允许操作、保留宿主默认能力的实现。REQ-011 要求保留正常读取、搜索与 Git 差异能力并使用宿主现有写入限制；需核对实际宿主可用接口及正常调用结果，不能将提示词约束或另一平台的只读配置当成所有平台已实现的证明。
  本机只读接口核实：WorkBuddy `codebuddy --help` 明确说明 `--tools default` 保留全部默认工具，`--disallowedTools` 按名称禁止工具；ZCode 已安装脚本的配置 schema 包含 `permission.disallowedTools: string[]`。前者帮助输出保存在 `/tmp/stage4a-workbuddy-role-help.txt`；ZCode 当前 app-server 的创建和恢复 schema 均接受 `toolDenylist`；已安装脚本的 `u8i` 读取该字段并传入运行时 `toolDisallowlist`，而当前适配仍发送 `toolAllowlist`。这些是接口证据，不是模型实跑或全部平台验收 PASS。

复核原始记录：`/private/tmp/stage4a-reference-bind-ss_0ezih/evidence/all-prompts-source-audit.json`。上述判断不改变既有开发、QA 原结果或费用记录。


### B4 本次增量（绑定基线 5ecf7b5）

上表保留为修改前复核记录。本次移除基于旧标题的章节回退；缺失必需章节明确报错，
不混装当前包规则。QA 审查、执行及 carry 的规则已直接表达，不再引用外部项目中不存在的
包内相对文件。需求澄清指南合并重复确认步骤，移除过时 RQ-011 登记集说明，保留正式
全文、revision、预检和用户确认约束。未新增提示词文件。

产品审和技术审不提供 workflow 工具，工具说明为空；原生读取、搜索与只读 shell/diff
能力保留，不能借用白盒前置覆盖的 NoTools 模式限制这些能力。操作桥也拒绝这两种角色的请求。其余角色工具范围保持不变。
同身份恢复及跨 action 续做使用现有 development-worker.md 的“续做约定”，携带当前
绑定、完整当前任务与材料路径，并引用前次实际消息；新身份/替换仍收完整规则。
前次消息只用于规则追溯，当前候选与执行范围以本轮输入为准。没有新增材料索引或账本。

#### 同一小型夹具的实际装配计量

修改前在 `git archive 5ecf7b5` 的临时副本运行生产装配，修改后在本工作区运行；
均使用 TestStageInputAssemblyMeasurement。基线只在测试中增加一次 resume renderer 调用，
没有修改基线生产实现。单位为 Unicode 字符，schema/tool 为序列化 JSON；工具为空计 0。
随机临时目录长度会带来少数字符抖动。它不是 tokenizer 计量、真实模型调用或宿主效果证据。

| 阶段 | 消息 前→后 | schema 前→后 | 工具 前→后 |
| --- | ---: | ---: | ---: |
| product-review | 3150 → 3148 | 1476 → 1476 | 983 → 0 |
| start-readiness | 4332 → 4332 | 2852 → 2852 | 983 → 0 |
| development-worker | 3494 → 3494 | 1536 → 1536 | 983 → 983 |
| development-resume | 3535 → 2228 | 1536 → 1536 | 983 → 983 |
| blackbox-design | 6791 → 6792 | 5548 → 5548 | 983 → 983 |
| whitebox-design | 7846 → 7846 | 6375 → 6375 | 983 → 983 |
| whitebox-coverage | 1676 → 1676 | 355 → 355 | 0 → 0 |
| blackbox-review | 6467 → 6423 | 3281 → 3281 | 983 → 983 |
| whitebox-review | 7809 → 7767 | 3281 → 3281 | 983 → 983 |
| blackbox-execution | 5900 → 5829 | 2568 → 2568 | 2554 → 2554 |
| whitebox-execution | 5878 → 5807 | 2568 → 2568 | 2554 → 2554 |
| complexity-gate | 8367 → 8367 | 2568 → 2568 | 983 → 983 |
| implementation-quality-gate | 8176 → 8175 | 2568 → 2568 | 983 → 983 |
| repair-worker | 3836 → 3836 | 2263 → 2263 | 2554 → 2554 |

`requirements-clarification.md` 包内指南正文：1624 → 1246 字符；没有独立原生 worker，故不虚构 schema 或工具计量。

`carry.md` 包内指南正文：753 → 725 字符；没有独立原生 worker，故不虚构 schema 或工具计量。

白盒前置覆盖、两类设计/复审、执行、开发、返修和两类门均经过实际装配；仍检查
schema 无 uniqueItems、无无关角色段落、绑定材料可读。首次装配未受影响的阶段保持原义。
跨 action 的真实本地 transport 夹具验证 CONTINUE 后沿原身份续做，前次 prompt.txt
实际存在且可读，续做消息不重复开发角色段落，结果可接纳；夹具结果不代表独立审查 PASS。

#### 本地验证与边界

本次验证日志位于 `/tmp/b4-*-653.log`，属于临时原始输出。最终检查状态在本节更新。
进度、等待时长、候选变化、静态检查、计量归属和 unavailable 沿既有实现，未增加新机制。
公共操作、脚本参数、安装根目录与版本索引仍位于 references/engine-regular.md。
正式审查、独立 QA、真实宿主完成情况和 Seal 由后续 engine action 认定。

首轮文档契约检查指出精简指南缺少两处规定措辞；已恢复预检后 REQUEST 与确认前禁止
DECIDE 的原措辞。其余首轮或最终检查结果照实记录，不以首次装配测量代替续做验证。

第二轮修正同步更新了旧章节装配夹具，并保留“等待用户明确确认”的规定措辞。
`go test -tags phase0whitebox ./internal/validate -timeout 5m` 通过；
定向 `TestMarkdownRoleAssemblyKeepsInputBoundaries|TestProgress|TestOperationDuration|TestStaticChecker|TestQAReadiness|TestNativeProgress|TestNativePlatformDevelopmentResumeAndWhiteboxPreflight`
通过（35.447s）。`go test -race ./internal/cli` 所在首轮组合检查中的 CLI 包通过（199.042s）；
修正文档后单独 `go test -race ./internal/validate` 通过（38.481s）。
最终构建、`go vet ./...`、package validate、portable canary 均退出 0。
以上 Go 检查使用 `env -u CODEX_HOME -u CODEX_CLI_PATH GOCACHE=/tmp/formal-gates-b4-cache`。

首轮全量 facade 达到五分钟超时，栈停在
`TestExternalShellQAPreparationIgnoresUnrelatedGo/declared-command-fails` 的子进程等待；
该测试会设置无效 GOTOOLCHAIN 并调用 go env。原因尚未证实，不将其报告为产品缺陷。
最终全量另以 `GOPROXY=off GOSUMDB=off` 禁止下载来源核验；没有修改该用例或被测逻辑。

#### 本次交回：FAIL，Batch 尚未完成

两轮配套自测修正后，全量检查仍未全部通过，本轮停止继续修复并交回 engine：

- 当前源码重现 `TestNativeMandatoryInputsBeforeClaim/tools-only` 失败：夹具以 product-review
  删除工具说明后仍期待 preflight 报错；产品审已不声明该工具，因此返回 nil。
  尚须将必需工具说明检查放到实际需要该工具的角色，并核验产品/技术审没有工具说明也可派发。
- 较早十分钟全量检查的 facade 超时发生在
  `TestRepairTaskOperationUsesWorkingEditsAndRecovery`（当时该测试运行 13s），不是持续停在
  五分钟那次的同一个用例；不足以断定下载导致超时。
- 该次 validate 的 `TestResolveEngineAdmissionChoosesProjectBinding` 报
  `UNREGISTERED_INSTALL: registered target digest is stale`；尚须在稳定交付文件和有效安装目标
  绑定下完成全量验证，不能用单项 canary 成功抵消该失败。
- 本轮已启动的最终源码全量输出保留在 `/tmp/b4-all-stable-653.log`（20m 上限）与
  `/tmp/b4-offline-final-653.log`（10m，上述离线环境）。交回时尚未收齐，不能认定 PASS。

上述未完成项属于当前 B4；无新增范围、公开 API 或架构决定。后续继续沿 engine 派发的
原开发身份处理，不能把本文早先历史轮次的“开发检查完成”当成本次增量完成。


### B4 原身份续跑（attempt-2）

上轮两项全量输出已收齐：20m 上限检查的 facade 实际运行 685.711s，仅
`TestNativeMandatoryInputsBeforeClaim/tools-only` 失败，其余包（含 validate）均通过。
10m 离线检查发生总时限超时，另有 `TestContinuousUserAskInterruptsTransientWait`
临时目录清理的 directory not empty；这不证明离线与下载是前次超时的原因。

本轮把缺失工具说明的 preflight 夹具改为实际需要该工具的 development-worker；
产品审、技术审无工具说明可装配的检查纳入共用角色测试。同时纠正上轮 NoTools 使用：
只取消 workflow 辅助工具，保留各宿主现有只读读取、搜索和 Git diff 能力。
共用 nativeTurn 判定承载工具许可；Codex、WorkBuddy、ZCode、DSH 只在各自原生声明
位置消费该判定/角色政策，不增加新能力或放开写入。对应配套测试分别检查读取/shell
仍存在、辅助工具不声明以及前置覆盖仍不提供 workflow 工具。

剩余验证使用稳定源码、单次全量及既有安装检查，不并发启动多个全量副本，也不在
检查过程中修改包内交付文件。最终观察通过本 action 的结构化 validation 交回；
不为写验证结论而在包摘要检查期间再次改动文件。


本轮第一组稳定源码全量已返回：facade 运行 523.543s，仅
`TestZCodeBoundToolsConfigurationAndUsage` 的 product-review/start-readiness 初始与续用四个
子项因仍要求已取消的辅助工具而失败；其余包与测试通过，安装 digest 错误未再出现。
本轮第二次配套修正将该断言改为保留 Read/Glob/Grep/Bash，明确禁止两个 workflow 工具名称。
生产实现未因此改变；该测试修正及最终验证结果由当前 action 的 validation 记录。
本轮第一次 portable canary 的 Codex bootstrap identity 失败也保留在
`/tmp/b4-canary-657.json`；固定文件并重建后的 `/tmp/b4-canary-final-657.json` 退出 0，
未将失败原因推定为已证实的产品问题。


### B4 当前增量（绑定基线 c3526fa，attempt 683）

移除续做输入的 `previousMessage`/`prompt.txt` 材料间接引用。原身份续做依赖其原生会话
已收到的规则，继续携带当前 binding、任务和直接材料路径；新建或替换身份仍收到完整规则。
固定续做文字仍由已有 development-worker.md 的“续做约定”持有。实际 prompt.txt
继续作为用户可见的发送记录保留，不作为模型的材料入口。本节取代上文对该引用的保留说明。

生产装配使用相同 TestStageInputAssemblyMeasurement 小型夹具；修改前来自绑定 HEAD 的
只读 git archive 临时副本，修改后来自本工作区。下表为 Unicode 字符数，不是实际 token、
费用或真实宿主延迟；随机临时目录导致少量字符浮动。没有为计量启动真实模型任务。

| 阶段 | 消息 前→后 | schema 前→后 | 工具 前→后 |
| --- | ---: | ---: | ---: |
| product-review | 3150 → 3150 | 1476 → 1476 | 0 → 0 |
| start-readiness | 4330 → 4330 | 2852 → 2852 | 0 → 0 |
| development-worker | 3494 → 3494 | 1536 → 1536 | 983 → 983 |
| development-resume | 2228 → 2046 | 1536 → 1536 | 983 → 983 |
| blackbox-design | 6791 → 6791 | 5548 → 5548 | 983 → 983 |
| whitebox-design | 7846 → 7846 | 6375 → 6375 | 983 → 983 |
| whitebox-coverage | 1676 → 1676 | 355 → 355 | 0 → 0 |
| blackbox-review | 6423 → 6423 | 3281 → 3281 | 983 → 983 |
| whitebox-review | 7767 → 7767 | 3281 → 3281 | 983 → 983 |
| blackbox-execution | 5826 → 5829 | 2568 → 2568 | 2554 → 2554 |
| whitebox-execution | 5810 → 5804 | 2568 → 2568 | 2554 → 2554 |
| complexity-gate | 8367 → 8367 | 2568 → 2568 | 983 → 983 |
| implementation-quality-gate | 8176 → 8176 | 2568 → 2568 | 983 → 983 |
| repair-worker | 3836 → 3836 | 2263 → 2263 | 2554 → 2554 |

定向装配、缺失材料 preflight、同身份 transport 续做和替换身份规则选择检查已通过
（31.804s）。新增回归覆盖 resume 后 render replace/spawn，以及缺失原生身份时保留完整规则；
transport 夹具验证前次实际消息仍存在，但本次输入不将其作为材料入口。

B4-S2/S3 保留既有进度与 internal/cost 实现；此次未改调度、计量或第二套记录。
B4-S4 的公共操作索引仍由 references/engine-regular.md 持有，已有可达性审查和安全静态
检查入口保持不变。全量、race、tagged、vet、构建、package validate、portable canary 与
安装后无上下文材料检查的最终结果由本 action 的结构化 validation 如实交回。
临时输出位于 /tmp/b4-*-683.log；这不是正式 QA、真实宿主验收或 Seal 证据。

#### attempt 683 自测交回：FAIL

稳定源码的全量 `go test ./... -timeout 30m` 退出 1，facade 用时 618.919s，唯一失败为
`TestContinuousUserAskInterruptsTransientWait` 的 TempDir RemoveAll 清理错误：
`directory not empty`。其他包全部通过，包括 CLI 的安装后无上下文材料消费夹具。
随后单独 `go test ./internal/engine/facade -run '^TestContinuousUserAskInterruptsTransientWait$'
-count=5 -v` 五次中四次通过，一次重现同一清理错误，整体退出 1（3.345s）。
该测试涉及 Ask 立即交回及后台执行；本次没有改其测试或执行生命周期，也没有认定根因。
尚须解决这一全量验证阻塞并取得完整通过结果，不能以定向通过或重试中的四次通过抵消。

实际通过的检查：构建、package validate、portable canary（52 项全部 PASS）、go vet、
phase0whitebox validate（84.985s）、race validate（42.374s）和 CLI（183.582s）。
进度、等待、可达性、静态检查、材料及计量定向检查通过（facade 24.315s，cost 0.669s）。
Go 检查统一使用 `env -u CODEX_HOME -u CODEX_CLI_PATH GOCACHE=/tmp/formal-gates-b4-cache`。
日志分别为 `/tmp/b4-all-683.log`、`/tmp/b4-wait-recheck-683.log` 及上述同前缀日志；
临时日志不保证跨工作区保留，因此本节保存错误、命令、次数和最终状态。
本 Batch 尚未完成；进度和计量实现保留，不因该清理错误扩展本次产品修改。

#### attempt 687：清理夹具的必要配套修正

沿同一开发身份续做。上轮失败测试在第三次 capture 中提交 Ask；消费器仍可发起下一次
重试，而 Ask 会立即交回。原夹具释放 action lock 并清理 TempDir 时，这个重试可能仍在
读取状态或尝试锁路径。此次只修改两个测试文件：已有 beforeCapture 钩子允许返回错误；
第四次 capture 到达后，夹具发出信号并返回本地错误，从该位置起不再触及文件系统。
测试在清理前等待该信号，保留首次真实锁拒绝、等待后的 Ask、零原生 turn 和未擅自回答
用户决定的断言。没有修改生产消费器或改变其及时交回语义。该配套用于解除 B4 必需全量
验证的可重复清理阻塞，不增加产品功能。

修正后该测试 `-count=20` 通过（13.107s）。新增检查只针对这项并发夹具修正；上轮
B4-S1/S2/S3、CLI 安装后交接、成本、tagged、race validate/CLI、构建、vet 和 portable
canary 的实际结果保留。全量复验及该测试的 race 复验结果由当前 action 结构化 validation
交回。临时日志为 `/tmp/b4-wait-687.log`、`/tmp/b4-wait-race-687.log` 和
`/tmp/b4-all-687.log`。上轮失败记录保留，不把重试前的全量结果改写为通过。

本次最终结果：`go test ./... -timeout 30m` 退出 0，facade 513.280s、CLI 84.823s；
未变更包复用有效 Go 测试缓存。失败用例的 race `-count=5` 通过（14.473s），构建、
`go vet ./...`、package validate 及差异检查通过。结合上轮仍有效的其余必要检查，
B4 本地开发验证完成；本节不声明独立 QA、Seal 或安装接纳完成。

### B5 工具策略的跨平台接纳前核实

在 B5 当前增量运行期间核实，以下两项尚不能认定满足保留正常角色能力、按禁止操作限制的既有要求：

- `runtime_adapter_dsh.go` 的 `dshRoleTools` 仍枚举可用工具，`runtime_config_dsh.go` 与
  `runtime_tools_dsh.mjs` 继续传递和消费 `allowedTools`。DSH 原生只读 sandbox 已配置，
  但它不能证明额外的工具白名单已移除；仅修改 WorkBuddy/ZCode 不覆盖这一实现。
- Codex 的白盒覆盖判断使用只读 sandbox 并保留 shell；WorkBuddy/ZCode 的当前增量仍在
  coverage 分支禁止整个 Bash，DSH 的 coverage 分支也仅保留枚举读取工具。只读覆盖判断
  不等于冻结用例执行，不能将后者禁止自由执行测试目标的限制直接扩大到前者。

这是一份主代理源码核实记录，不是独立门审 PASS 或新增产品能力。后续处理仍引用原 B5
及 REQ-011，保留已有效完成的开发与验证，平台困难实跑按本 run 既有处置延期。

### 增量派发遗漏的已核实修复范围

`DevelopmentIncrementReceipt` 已保存确认事件、需求版本、原因和新 TaskKey 映射；
`preparationInput` 却只展开原 Batch，没有传递当前增量依据。B5 实际首次输入已证明
这一遗漏，不能靠后续中断结果临时传话作为完成修复。

复用已有确认记录，按当前 TaskKey 精确选择当前增量，传递其已确认依据及材料引用；
新增身份和同身份续做都经过公共装配入口。缺失对应记录的增量任务启动前拒收，普通
首次批次不要求增量记录。验证多次增量、历史任务、首次与续做不会漏传或串用。
不恢复调用者填写 Batch 任务正文，不新增提示词文件或另一套任务系统。

本 run 的困难平台接入实测已获用户延期，不限于模型调用；原生入口探测被环境权限
阻断也不能变成追加验收条件。本地验证成功与宿主实测未验证分别如实报告。

开发结果的最终接纳与后续调度仍由引擎决定。当前 schema 已允许 CONTINUE，但子代理
曾将运行中检查报告为 FAIL。宿主自由命令的后台句柄在原生会话恢复后不可续接，也曾
造成原测试仍运行而补跑相同包。修复须依据真实可获得的状态，不通过匹配说明文字改判，
也不将本轮环境失败改写为产品失败或正式 PASS。

### 剩余工作的执行清单（2026-09-14，revision 741 核实）

本节是已提出问题的核实与执行清单，不是替代开发提示词，也不是新的产品需求。
开发正文仍由引擎从已确认 Batch 装配；新增增量必须取得真实 TaskKey 后才能标记已签发。
用户已要求全部安排并继续处理，不因常规实现选择再次请求许可。

| 项目 | 问题和完成条件 | 现有执行入口及依赖 | 当前状态 |
| --- | --- | --- | --- |
| R1：恢复实际执行 | B5 attempt-5 的原生恢复返回 active writer；先核实原生写入者与恢复结果，依现有恢复协议续用或在证明确实不可恢复时替换。不把重复启动失败算成开发已执行，不以假结果绕过。 | 当前 B5 的恢复与 preparation-retry；是 R2 实际执行的前置。 | 旧 attempt-5 启动被拒的证据保留；用户确认换人后，新原生身份接手，attempt-8 已完成并接纳。 |
| R2：完成工具策略 | 删除 DSH 开发/审查角色的工具集合白名单残留，按角色禁止写入；恢复覆盖判断正常只读命令能力。保留 WorkBuddy 原生预批准与工具集合的区别。只补受影响验证，保留有效检查。 | 已有 B5 增量任务继续；不另起原始 B5 全量开发。 | B5 attempt-8 已接纳 PASS，提交 5acd34442acbc35f8e54e6202a75ed00d91fa47e；DSH 与 coverage 修正及受影响验证已交付。 |
| R3：当前任务材料不漏传 | 公共装配按 TaskKey 选当前已确认增量记录，传递依据与材料路径；首次、续做、多次历史增量均不漏传、不串用。增量缺记录时启动前报错，普通批次不受此约束。本次已授权平台实测延期也须由现有决定记录传到相关动作，不能只靠临时聊天。 | R2 接纳并完成 Git/source 结算后，development-increment optimize B4。批次职责已有 B4-S1/S2。 | 待签发；主代理记录与临时反馈不算任务已派发。 |
| R4：未完成验证的连续执行 | 明确开发交回、检查仍运行、检查失败和任务最终接纳的区别；引擎按真实已有执行状态续做。恢复后复用仍有效的检查及已完成结果，避免原检查仍活着又重复启动。先核实可获得的进程/操作证据，不增加文本关键词判定器，不假定引擎能观察任意自由 shell。覆盖未完成返回、恢复及重复执行的实际路径。 | development-increment optimize B2；R3 的增量材料修复安装并生效后签发，避免修复任务自身再次丢依据。 | 待签发；不是“已有 CONTINUE 字段所以完成”。 |
| R5：核查和删除无用增量 | 核查本轮实际新增 diff 是否重复实现原始 B5、扩大工具限制或造成不必要公共/平台耦合；仅删除有证据的重复、废弃和无益代码，保留合法平台参数映射。按正常安装后外部项目可用、不过度设计/过拟合/窄逻辑两点复核，不预设 B6 全部失败。 | R2–R4 开发接纳后，development-increment optimize B6；独立正式门继续审实际完整候选。 | 待签发；此前九文件 diff 不是“大量垃圾代码”的证据。 |
| R6：正式验证与收尾 | 保留可继承的黑盒设计，针对本轮变化增量补齐并执行；完成白盒及必要独立门、问题裁决和修复，最终集成、安装、删除已替代兼容/临时代码并取得 Seal。平台延期只记本 run 的未实测事实，不写成产品能力或虚构 PASS。 | R2–R5 接纳后继续当前引擎 QA/verification/promotion/Seal 图；独立就绪动作按现有容量并行。 | 黑盒 38 个用例设计已冻结，尚无实际执行结果；未 Seal。 |

上述待签发增量复用公开 REQUEST 入口，分别选择 B4、B2、B6：
`action=development-increment, target=<Batch>, meaning=preserved, increment={kind:optimize,batchId:<Batch>}`。
reason 引用对应已核实问题；workspaceView 由 facade 取得。随后使用真实 Ask 完成已有授权
范围内的影响处置与 DECIDE，再由 drive 启动，记录真实新 TaskKey、原生身份和实际消息。
没有签发回执时不得将清单编号冒充引擎任务编号。

后续执行更新：B5 已提交并完成来源结算，R1 阻塞解除；B4 公共材料修复已通过
 development-increment 确认。后续继续 R4，开发全部完成后才开展 R5 独立调查。
本轮最新用户决定是：Codex 可以实测，其他平台禁止实机测试及原生入口探测。
这仅是本次执行约束，不修改产品能力或正式需求；此前“实测延期”表述以此决定为准。
