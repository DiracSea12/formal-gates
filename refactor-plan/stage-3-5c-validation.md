# 阶段 3.5c 开发验证与真实 Codex 对比

本文是开发自测和固定比较记录，不是正式 run 的独立 QA 或门结论。已补齐两版设计、审查、
执行的真实 Codex 用量与原生模型任务计时；所有必需模型指标均与原始记录对账。最终完整
检查的回收情况在文末单独列出；正式验收与封板另见阶段记录。

## 源码、环境与隔离

- QA 基线：`fd9983e06fbcdf042aa3bf6f839e03a8aab58d5e`。实际比较源码
  `3370e035f1d6be30e6f9e6ed761420cca64e81ff` 保留其旧 QA 行为，仅接入 Codex
  计量、宿主 fixture 和固定比较用测试；该 detached commit 的差异可独立检查。
- 实际候选源码：`f3e29d01ad4c2b300713391dfd2b834a0c4f3593`，通过已提交快照的
  `git archive` 导出至 `/tmp/formal-gates-stage35c-codex/metered-source` 并实际构建、安装。
  交接前后续提交只同步本文及相关操作说明，Go 实现、测试与比较脚本相同。
- 两版均使用 `/Applications/ChatGPT.app/Contents/Resources/codex`，版本 **0.153.4**，
  模型 **gpt-6-astra**，reasoning effort **xhigh**；完整参数保留在每次 `invocation.json`。
  本机 macOS 15.7.9 arm64，Go 1.26.4。模型缓存命中来自真实宿主数据，不控制其波动。
- 证据根为 `/tmp/formal-gates-stage35c-codex`（macOS 的 `/private/tmp` 为同处）。
  模型使用已有认证，没有替换全局 Codex 或正式 run 的固定 driver。
- 两个实测安装分别位于 `baseline-installed` 和 `metered-installed`，各自独立的
  `project/.codex/skills/formal-gates`、`home/.local/bin/formal-gates`、
  `home/.formal-gates/registry.json` 和 `project/.gates`。安装及运行输入为真实副本。
  绝对路径、package 和 fixture 候选 identity 见各自 `installation.json`。

## 固定场景与真实执行边界

两条独立 `workflow show` 用例共用准备说明，各自解码实际 stdout，核对 run identity 与
ACTIVE 状态。固定注入错误 SEALED oracle 和“只完成第一条却宣称整组 PASS”的缺陷，
并保留可选 P3、两条开发提交和第一条观察后的中断。两版模型独立完成设计、审查，并用
命令工具实际运行观察脚本；脚本只准备参数、真实调用 CLI、保存输出并断言。

只构造昂贵的受理前提及开发者 fixture 提交；正式 QA 目标结果没有预置。模型输出与真实
命令记录独立保存，安装、设计登记、审查、建议处置、checkpoint、续跑、候选整理和 Seal
都是公开 CLI 实际调用。生命周期绑定采用实际 Codex thread ID 与原始 rollout，不用模拟
usage。开发/owner fixture 未发起对应真实模型，不混入本表的 QA 指标。

基线第一次执行只观察 CASE-001，登记中断后重新派发并重做两条；旧 Seal squash 改变
候选后再次执行两条。候选先只观察 CASE-001，实际写入 checkpoint，再用 `codex exec
resume` 继续同一 thread，仅观察 CASE-002。checkpoint 保持 PENDING、同 dispatch/
TaskKey；最终 cost 在该任务完成后才固定，包含两次 native turn 的全部用量。

最终用例结果均有两条，已知错误仍被发现。基线与候选各自的最终 fixture identity 为：

- baseline: `a2313ec08daad4116a43bc8e5df520247bf20cc9`。
- candidate: `3cb3f1bd343b3620fb2959404d10da8b050b5d9c`。

候选 Seal 前后 identity 相同。逐例真实输出在各自 `model-commands.jsonl`，逐条登记调用
在 `commands.jsonl`，原始既有 cost 投影在
`project/.gates/results/qa-efficiency-installed.json`。没有第二个权威成本账本。

## 模型任务耗时与 token

下表来自既有 cost 投影，并逐条与原始 `token_usage_record` 按 response ID 去重后的 usage
对账；原始 `task_started` 和 `task_complete` 时间也独立核对。

墙钟时间为原生模型任务区间之和，**包含工具执行和等待**，多次调用间以及同 thread 两 turn
之间的空闲不相加。不是 CLI 写入耗时，也不是整个开发过程的经过时间。`invocation.json`
另存进程启动/结束与 process wall；它包括 CLI 启动开销，不替代模型任务时间。

| 版本 / 阶段 | 模型任务秒 | 输入 | 缓存命中 | 非缓存输入 | 输出 | 逻辑派发 | 宿主调用 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 基线 / 设计 | 82.089 | 37,436 | 12,160 | 25,276 | 2,088 | 2 | 2 |
| 基线 / 审查 | 30.364 | 18,599 | 0 | 18,599 | 714 | 1 | 1 |
| 基线 / 执行 | 126.917 | 112,960 | 55,808 | 57,152 | 1,493 | 3 | 3 |
| 基线 / 合计 | 239.370 | 168,995 | 67,968 | 101,027 | 4,295 | 6 | 6 |
| 候选 / 设计 | 58.842 | 37,557 | 18,432 | 19,125 | 1,495 | 1 | 1 |
| 候选 / 审查 | 35.024 | 18,633 | 0 | 18,633 | 883 | 1 | 1 |
| 候选 / 执行 | 103.178 | 78,900 | 57,728 | 21,172 | 1,060 | 1 | 2 |
| 候选 / 合计 | 197.044 | 135,090 | 76,160 | 58,930 | 3,438 | 3 | 4 |

缓存命中为 0 的审查行是原始宿主计数确实为 0；全部必需字段都有来源。候选执行的两次
宿主调用属于同一个 thread/dispatch，不增加逻辑任务。实际模型请求数为基线 9、候选 7，
与派发次数分开记录。单次固定比较受到调度、模型输出和缓存的影响，不承诺固定提速倍数。

每条派发的原生 UTC 起止如下；该 ID 同时对应 cost key 和 `codex/<dispatch>/` 目录。

| 版本 / 阶段 | dispatch | 原生开始 UTC | 原生完成 UTC |
| --- | --- | --- | --- |
| baseline / qa-design | `dispatch-45ec81d3fd56b00d4faeb694` | 2026-09-06T16:55:30.911Z | 2026-09-06T16:56:01.834Z |
| baseline / qa-review | `dispatch-b223c183366ab16d7486305e` | 2026-09-06T16:56:04.511Z | 2026-09-06T16:56:34.875Z |
| baseline / qa-design | `dispatch-edb35d3ae6a9f76868cab7e2` | 2026-09-06T16:56:37.42Z | 2026-09-06T16:57:28.586Z |
| baseline / qa-execution | `dispatch-9967bd3b2fa632e663829d73` | 2026-09-06T16:57:32.421Z | 2026-09-06T16:58:01.572Z |
| baseline / qa-execution | `dispatch-8aa8df1e288406b2057ca988` | 2026-09-06T16:58:07.916Z | 2026-09-06T16:59:18.817Z |
| baseline / qa-execution | `dispatch-deafe535080f3e064eefcd28` | 2026-09-06T16:59:21.902Z | 2026-09-06T16:59:48.767Z |
| candidate / qa-design | `dispatch-22c5538c7bacae3efa53a79e` | 2026-09-06T17:09:02.661Z | 2026-09-06T17:10:01.503Z |
| candidate / qa-review | `dispatch-5afb8a65bd2c0aa516a6ad23` | 2026-09-06T17:10:04.123Z | 2026-09-06T17:10:39.147Z |
| candidate / qa-execution | `dispatch-4e4335dbe2dcda7c4ef71b47` | 2026-09-06T17:10:42.593Z | 2026-09-06T17:12:28.585Z |

各目录保存完整 `prompt.txt`、`stdout.jsonl`、`stderr.log`、`answer.json`、`invocation.json`
及原始 `rollout.jsonl`；候选续跑调用保存在同 dispatch 的 `continuation/` 子目录。原生
rollout 还保留在用户 Codex sessions 中，其原路径在 invocation 记录内。派发 `transcriptPath`
指向该同一真实来源副本。`reconcile.py` 与 `reconciled-comparison.json` 是本次对账记录，
只读取既有 cost 投影和原始记录，不写正式 state 或提供第二 writer。

## 重复操作与准备成本

| 指标 | 基线 | 候选 |
| --- | ---: | ---: |
| 共同准备说明/审查位置 | 2 | 1 |
| 实际设计派发（含可选建议处置） | 2 | 1 |
| 实际审查派发 | 1 | 1 |
| 实际执行派发 | 3 | 1 |
| 真实 `workflow show` 观察 | 5 | 2 |
| 中断后重复观察已完成第一条 | 1 | 0 |
| Seal 改候选后再次观察 | 2 | 0 |
| 两个独立实例的构建次数（含共用源构建） | 3 | 1 |
| 独立安装/准备实例 | 2 | 2 |
| 共用构建秒 | 0.114246 | 0.080296 |
| 两个实例准备秒合计 | 6.436182 | 4.975220 |

构建与准备来自 `final-batch/commands.json` 的六条真实命令及 `sh -x` 输出。两版先构建
一次；基线 prepare 又构建两次，候选两次 prepare 均传 `--reuse-build`。安装属于本轮验收
目标，所以两版都真实安装两次。独立副本不相互污染及已存在输出路径的错误检查见
`copy-check.json`；该 fixture 脚本自最初实测以来未变，可按同版本复用。

## 可复现操作与正确性检查

候选实际操作命令如下；baseline 使用上述 detached 比较源码，相同 runner/模型配置，
另传 `FORMAL_GATES_QA_BASELINE=1`，将 evidence 改为 `baseline-installed`：

```sh
# 在 metered-source 的不可变源码导出目录中运行
FORMAL_GATES_QA_EVIDENCE_DIR=/tmp/formal-gates-stage35c-codex/metered-installed \
FORMAL_GATES_QA_CODEX_RUNNER=/tmp/formal-gates-stage35c-codex/metered-source/scripts/phase3-qa-codex-run.py \
FORMAL_GATES_QA_CODEX_BIN=/Applications/ChatGPT.app/Contents/Resources/codex \
  go test ./internal/cli -run '^TestQAEfficiencyInstalledPublicOperations$' -count=1 -timeout=30m -v
```

重跑须选择新的 evidence 目录；脚本拒绝覆写已有任务证据。普通 `go test` 不设置 runner
时不调用模型。两版 live fixture 均通过；原始日志为 `baseline.log`、`metered.log`。
基线构造命令保存在 `generate-baseline.py`，其生成的 harness 已纳入 detached 比较提交。

新增回归覆盖当前请求/镜像去重、旧来源回退、原生 resume 累计范围差异、原生计时含工具
且排除 turn 间空闲、未完成计时缺失、来源绑定及 Codex checkpoint 不提前固定 cost。
live fixture 还直接把每条 cost 与真实原始 request usage 对账，任一差异都会失败。

相同已知错误检查在两版均通过（即都拒绝了预期的错误输入）：
`TestQAExecutionEvidenceRejectsOnlyExactNormalizedProtocolSentinels`、
`TestQAExecutionRecordsRealEvidenceContainingSentinelWords`、
`TestQA35BAffectedScopeRejectsBadSubsets`、`TestQA35BQAReviewFailureDoesNotFreeze`。
输出为 `baseline-known-bad.log` 和 `candidate-known-bad.log`。既有 3.5c 回归继续验证错误
oracle 不被共同审批掩盖、未齐不能 PASS、已完成 FAIL 不丢失及候选/用例/工具/环境绑定。

历史 checkpoint 测试已改为同时从其声明的 base/delivered 读取路径和文档，聚焦回归通过。
没有修改 `stage-records.md`、旧提交或旧 Seal。此前 CLI 0.145.0 拒绝当前模型，以及两次
对账定位修复的试跑保留在同证据根目录，均不混入本次最终比较数字。

## 开发交接检查回收

聚焦回归、真实隔离 fixture 和上面的来源对账已通过。以下检查对应开发实现/测试 `f3e29d0`，
交接时尚在执行的三组检查后来均已完成并由编排层核对。它们是该开发快照的历史自测，
不替代白盒补测、返修及候选整理后的正式验收。

| 检查 | 当前结果 / 原始日志 |
| --- | --- |
| Codex / cost / 历史聚焦回归 | PASS；`/tmp/stage35c-codex-focused.log`、`/tmp/stage35c-codex-resume-focused.log` |
| checkpoint 与普通安装 fixture | PASS；`/tmp/stage35c-codex-checkpoint.log`、`/tmp/stage35c-codex-native-harness.log` |
| 真实 Codex 两版对比 | PASS；`baseline.log`、`metered.log`、`reconciled-comparison.json` |
| `go test -timeout=30m ./...` | PASS，exit 0；`/tmp/stage35c-verified-full.log` |
| `go test -race -timeout=30m ./internal/validate ./internal/cli` | PASS，exit 0；`/tmp/stage35c-verified-race.log` |
| `go test -tags=phase0whitebox -timeout=30m ./internal/validate` | PASS，exit 0；`/tmp/stage35c-verified-whitebox.log` |
| `go vet ./...` | PASS，exit 0；`/tmp/stage35c-verified-vet.log` |
| `go build -o bin/formal-gates ./cmd/formal-gates` | PASS，exit 0 |
| package validation / portable canary | PASS，exit 0，canary 44/44；`/tmp/stage35c-verified-package.log`、`/tmp/stage35c-verified-canary.json` |
| `git diff --check` / cached / baseline-to-current | PASS，exit 0 |

正式独立 QA、最终候选检查、Seal 及主线集成以 `stage-records.md` 和对应 run 的封板、
集成回执为准。本阶段安装验证使用隔离 namespace，按冻结需求不切换全局安装。
