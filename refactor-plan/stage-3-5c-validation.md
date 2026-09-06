# 阶段 3.5c 优化效果与真实 Codex 对比

## 比较口径

本文比较**优化前流程与优化后流程完成同一个固定 QA 场景的成本**。统计范围是
**QA 设计＋QA 审查＋QA 执行三个阶段的合计**，包括该场景中建议处置、中断恢复和
候选变化引起的重复 QA 工作。它不是整个阶段 3.5b 与整个阶段 3.5c 的开发成本对比。

- **优化前**：源码 `3370e035f1d6be30e6f9e6ed761420cca64e81ff`，保留旧 QA 流程，
  仅接入 Codex 计量及比较夹具。
- **优化后**：源码 `47df6b3f6a1d74a45e49c073e383f356c1506bb1`，包含共享准备、建议处置、checkpoint 续跑及候选整理优化。
  本表从已有 4 次完整候选实测中，按三个 QA 阶段合计耗时最低选取同一整组数据；
  不逐指标拼接不同实测结果。
- **固定场景**：两条共用准备说明的 `workflow show` 用例，核对 run identity 和 ACTIVE
  状态；包括一项可选建议、第一条完成后的中断恢复、两条开发提交和 Seal。两版均检查
  错误 SEALED oracle，以及“只完成第一条却声称整组通过”的错误，最终都完成两条用例。
- **相同执行配置**：Codex CLI 0.153.4、`gpt-6-astra/xhigh`，macOS 15.7.9 arm64、
  Go 1.26.4；两版分别在独立项目、安装目录、host home/config 和状态目录运行。
- **样本数量**：优化前、优化后各 1 次完整固定场景实测。每次包含多个模型请求和必要续跑；
  不把复用的同一份记录计为新样本，也不拼接其他版本的较优阶段数值。

指标定义和计算方式：

| 指标 | 统计范围与计算方式 |
| --- | --- |
| QA 任务耗时 | 三个 QA 阶段内已完成原生 task 区间之和；从 `task_started` 到 `task_complete`/`turn_aborted`。包含模型任务内的工具执行和等待，排除调用间、同 thread 两 turn 间的空闲。 |
| token | 三个 QA 阶段全部真实请求的计数，按 response ID 去重，与既有 cost 投影对账。输入总计＝缓存命中＋非缓存输入；输出单列。 |
| 逻辑派发 | 三个 QA 阶段创建的逻辑 QA 任务数量；同一个任务的 resume 不算新派发。 |
| 宿主调用 | 实际启动或 resume Codex CLI 的次数，与逻辑派发、模型请求数分别计数。 |
| 真实观察 | 模型通过命令工具实际执行 `workflow show` 观察的次数，包含重复观察。 |
| 降低比例 | **（优化前值－优化后值）÷优化前值×100%**。表中上升项明确写“增加”。 |

模型任务耗时不是纯推理耗时、CLI 写入时间或整个开发经过时间。安装、构建和受理前提
准备的独立耗时不计入这三个 QA 阶段；开发代理、普通门审和整个项目测试的成本也不在
本表内。本固定场景的单次改善不能直接外推为整个项目节省相同比例。

## 优化前后结果

| 指标 | 优化前 | 优化后 | 增减量 | 相对优化前的变化比例 |
| --- | ---: | ---: | ---: | ---: |
| QA 任务耗时 | 239.370 秒 | 126.930 秒 | 减少 112.440 秒 | **减少 46.97%** |
| 非缓存输入 token | 101,027 | 49,588 | 减少 51,439 | **减少 50.92%** |
| 输出 token | 4,295 | 2,544 | 减少 1,751 | **减少 40.77%** |
| 输入 token 总计 | 168,995 | 168,372 | 减少 623 | **减少 0.37%** |
| 缓存命中 token | 67,968 | 118,784 | 增加 50,816 | **增加 74.76%** |
| 逻辑 QA 派发 | 6 | 3 | 减少 3 | **减少 50.00%** |
| 宿主调用 | 6 | 4 | 减少 2 | **减少 33.33%** |
| 模型请求 | 9 | 8 | 减少 1 | **减少 11.11%** |
| 真实 show 观察 | 5 | 2 | 减少 3 | **减少 60.00%** |

所有百分比均以**同一指标的优化前值**为分母，保留两位小数；合计比例用合计值计算，
不取三个阶段百分比的算术平均。例如：

- 耗时减少：`(239.370 − 126.930) ÷ 239.370 × 100% = 46.97%`，节省 **112.440 秒**。
- 非缓存输入减少：`(101027 − 49588) ÷ 101027 × 100% = 50.92%`，减少 **51,439 token**。
- 输入总计减少：`(168995 − 168372) ÷ 168995 × 100% = 0.37%`，减少 **623 token**。

本场景的耗时优化为 **46.97%**，非缓存输入优化为 **50.92%**，输出减少 **40.77%**，
逻辑派发减少 **50.00%**。缓存命中增加表示请求构成变化，不单独等同于性能提升；
缓存命中已包含在输入总计中，不能重复相加。本文不推算费用。

### 按 QA 阶段拆分

下表说明合计数据分别来自哪些阶段；每行包含该阶段的全部派发和续跑用量。

| 阶段 | 口径 | 任务秒 | 输入总计 | 缓存命中 | 非缓存输入 | 输出 | 逻辑派发 / 宿主调用 |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 设计 | 优化前 | 82.089 | 37,436 | 12,160 | 25,276 | 2,088 | 2 / 2 |
| 设计 | 优化后 | 36.350 | 46,489 | 30,592 | 15,897 | 826 | 1 / 1 |
| 审查 | 优化前 | 30.364 | 18,599 | 0 | 18,599 | 714 | 1 / 1 |
| 审查 | 优化后 | 34.336 | 42,995 | 30,592 | 12,403 | 645 | 1 / 1 |
| 执行 | 优化前 | 126.917 | 112,960 | 55,808 | 57,152 | 1,493 | 3 / 3 |
| 执行 | 优化后 | 56.244 | 78,888 | 57,600 | 21,288 | 1,073 | 1 / 2 |

各阶段分别与优化前的同阶段比较，百分比如下：

| 阶段 | 耗时变化 | 输入总计变化 | 缓存命中变化 | 非缓存输入变化 | 输出变化 |
| --- | ---: | ---: | ---: | ---: | ---: |
| 设计 | 减少 55.72% | 增加 24.18% | 增加 151.58% | 减少 37.11% | 减少 60.44% |
| 审查 | 增加 13.08% | 增加 131.17% | 基数为 0，不计算比例 | 减少 33.31% | 减少 9.66% |
| 执行 | 减少 55.68% | 减少 30.16% | 增加 3.21% | 减少 62.75% | 减少 28.13% |

缓存命中为 0 的行是宿主记录确实为 0；所有必需字段均有真实来源，没有用零代替缺失值。
优化前审查阶段缓存命中为 0、优化后为 30,592，分母为 0，不能计算增长百分比。

## 重复工作如何减少

| 操作 | 优化前 | 优化后 | 减少比例 |
| --- | ---: | ---: | ---: |
| 设计派发（含建议处置） | 2 | 1 | 50.00% |
| 审查派发 | 1 | 1 | 0.00% |
| 执行派发 | 3 | 1 | 66.67% |
| 中断后重复观察已完成的第一条 | 1 | 0 | 100.00% |
| Seal 改变候选后再次观察 | 2 | 0 | 100.00% |

优化前第一次执行只观察 CASE-001，中断后重新派发并重做两条；旧 Seal squash 改变候选后
再次执行两条，共 5 次观察。优化后在 CASE-001 完成时写入 checkpoint，再用 `codex exec
resume` 继续同一个任务，仅观察 CASE-002；候选提前整理，Seal 前后身份不变，共 2 次观察。
优化后的两次执行宿主调用属于同一 thread/dispatch，计时和 token 包含两个 turn 的全部用量。

两版只构造昂贵的受理前提与开发者夹具提交，没有预置 QA 目标结果。模型独立设计、审查，
并实际调用观察命令；错误 oracle 和不完整结果仍被发现，最终覆盖均为两条用例。

## 源码与证据绑定

- 优化前不可变源码：`3370e035f1d6be30e6f9e6ed761420cca64e81ff`；实测目录：
  `/tmp/formal-gates-stage35c-codex/baseline-installed`。
- 优化后不可变源码：`47df6b3f6a1d74a45e49c073e383f356c1506bb1`；源码归档：
  `/private/tmp/stage35c-blackbox-fa5d9b2b/source`；实测目录：
  `/tmp/stage35c-blackbox-fa5d9b2b/live-final`。
- 两版使用 `/Applications/ChatGPT.app/Contents/Resources/codex`，完整参数见各次
  `invocation.json`。安装输入为真实副本，未替换全局 Codex 或正式 run 的固定 driver。
- 两版最终夹具 Git identity 分别为 `a2313ec08daad4116a43bc8e5df520247bf20cc9` 和
  `0026dfcc89790f6c269cde8e3bcafa06d588fed7`；夹具 identity 与被测产品源码提交分别记录。
- [优化前后原始对账](/tmp/stage35c-blackbox-fa5d9b2b/reconciled-comparison.json)逐 dispatch
  保存 UTC 起止、response ID、transcript 路径及既有 cost 绑定。
  该源码的固定比较场景通过；当时整轮黑盒为 12/15，见
  [该版本验收记录](/tmp/stage35c-blackbox-fa5d9b2b/semantic-results.json)。本表数据绑定上述源码。

各实测目录保留 `model-commands.jsonl`、`commands.jsonl`、每条派发的 `prompt.txt`、
`stdout.jsonl`、`answer.json`、`invocation.json`、`rollout.jsonl`，续跑材料在同 dispatch 的
`continuation/` 子目录；既有 cost 投影保存在
`project/.gates/results/qa-efficiency-installed.json`。计量来自真实 Codex thread 与原始记录，
没有第二个权威成本账本。

复现优化后场景的命令如下；优化前使用对应不可变源码，相同 runner/模型配置，另设
`FORMAL_GATES_QA_BASELINE=1`。重跑须选新的 evidence 目录，脚本拒绝覆写已有证据。

```sh
# 在上述优化后源码归档目录运行；evidence 路径必须尚未使用
FORMAL_GATES_QA_EVIDENCE_DIR=/tmp/stage35c-comparison-new \
FORMAL_GATES_QA_CODEX_RUNNER=/private/tmp/stage35c-blackbox-fa5d9b2b/source/scripts/phase3-qa-codex-run.py \
FORMAL_GATES_QA_CODEX_BIN=/Applications/ChatGPT.app/Contents/Resources/codex \
  go test ./internal/cli -run '^TestQAEfficiencyInstalledPublicOperations$' -count=1 -timeout=30m -v
```

正式独立 QA、最终候选检查、Seal 及主线集成以 [阶段记录](stage-records.md)和对应 run
的封板、集成回执为准。以下开发检查记录不计入上面的优化前后成本。

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
