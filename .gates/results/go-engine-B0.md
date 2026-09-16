# B0 验收记录（第五次重签）：基线冻结与标定

## 结论

起始基线在本机可运行：确定性回归、夹具全流程与原生接入探测全部通过。前几轮指出的问题——两项 G0 缺失交付、两处准入误放行、停滞监控未真正生效、重签记录误判——均已修复且各有控制项覆盖。**B0 成立，允许开始 G1a。**

| 检查 | 结果 |
| --- | --- |
| 第一轮两项 G0 缺失交付 | 已补齐 |
| 第二轮两处准入误放行 | 已修复，控制项覆盖 |
| 第三轮停滞监控未生效 | 已修复，控制项证明其会触发 |
| 第三轮重签记录误判 | 已修复，控制项覆盖 ` M ` 与 `??` 两种状态 |
| Go 无缓存全量回归 | **通过**：26 包、1868 用例通过事件、0 失败、无停滞 |
| Python 全套回归 | 通过 |
| fixture 公开流程 | 实际达到 `DEMO_COMPLETE` |
| Go/SQLite 驱动兼容验证与锁定 | 通过，8 项探针在 CGO 开/关下全绿 |
| 运行依赖实际安装核对 | 通过，41 项锁定发行版逐项一致 |
| 公开基线冒烟（准入） | 通过，经公开入口达 `DEMO_COMPLETE` |
| DSH / ZCode `doctor` | 通过，接入探测、`model_calls=0` |
| 真实原生行为复验 | **NOT_RUN**（需显式授权费用） |
| **总体** | **`PASS`** |

## 本轮修复的两项

### P1：停滞监控检测不到静默——已修复

上一版的 `sh_stream` 先做阻塞式 `proc.stdout.readline()`，再检查是否超过停滞预算。子进程不输出、管道又未关闭时，`readline()` 本身就一直在等，**计时检查根本走不到**；等它后来恢复输出，又会先重置进度时钟，把整段静默漏掉。也就是说：我用"240 秒无输出"替换了旧的固定耗时门槛，并把"无停滞"写进通过条件，但**这条判据从未真正执行过**。正常全量通过不能替代验收工具自身的拒绝行为正确。

修复：输出由**读线程**读入队列，主循环用**带截止时间的队列等待**，计时因而不再被阻塞读取挡住。进度以"收到任意字节"为准，而不是"收到完整行"，所以只输出半行再卡住同样能被发现。日志与非零汇总保持不变。

控制项用真实生产代码、秒级预算（`--stall-budget`）驱动，短时即可验证：

| 控制 | 场景 | 断言 |
| --- | --- | --- |
| RS-1 | 完全静默 `sleep 30`，预算 1s | 判停滞、子进程被杀、理由指明静默时长 |
| RS-2 | `printf start` 无换行后静默 | 同样判停滞（半行不算"有进展到结束"） |
| RS-2b | 稳定输出 | 不误判停滞，退出码保留 |
| RS-3 | 非零退出且有输出 | 退出码如实记录，不误判停滞 |
| RS-5 | 真实运行器、`--stall-budget 0.001` 只跑 `go-test-full` | 该项 FAIL、`summary.overall=FAIL`、整体非零、日志含 `STALL` |

`RS-5` 是端到端的一条：它证明停滞不只是被记录，而是真会让验收失败。

### P2：覆盖已跟踪记录时误报工作树不符合要求——已修复

`issue_record` 把 `git status --porcelain` 的输出 `.strip()` 后再比对，删掉了开头有意义的空格：已跟踪文件被修改时 git 报 ` M <path>`，修剪后成为 `M <path>`，不再匹配允许集合，于是**记录已经写出却返回失败**。

修复：按"前 2 字符状态码 + 第 4 字符起的路径"整行比较，不再修剪前导空格，允许 `??`/` M`/`M `/`A `/`AM`。

控制项：`RG-1` 覆盖已跟踪记录（` M `）→ 接受；`RG-2` 新写未跟踪记录（`??`）→ 接受；`RG-3` 存在无关脏文件 → 拒绝。

## 完整 Go 回归：量具与独立环境实测

### 独立环境的数据

本轮复验方给出了一次完整跑通的实测：`internal/validate` **1637.65 秒后完整通过**，最长相邻事件间隔约 **55.07 秒**——即持续推进后完成，不是卡死。结合本机数据：

| 观测 | 数据 |
| --- | --- |
| `internal/validate` 的构成 | **785 个串行顶层用例**，最慢单例 **5.76 秒**，无长尾、无死锁 |
| 本机无缓存耗时（7 次） | 373.8 / 392.3 / 395.6 / 396.2 / 397.1 / 398.6 / 417.9 秒，全部通过 |
| 独立环境实测 | 1637.65 秒通过，最长事件间隔 55.07 秒 |
| 受控负载实验（8/10 核满载） | 同一组 8 个用例 34.66s → 46.45s，**+34%** |

### 定位结论

该包是纯串行 CPU 工作量的累加，**不存在某个慢用例或卡死点**：最慢单例只有 5.76 秒。独立环境的耗时约为本机的 4 倍，且同一次运行里每个包都慢，属环境整体吞吐差异。受控负载实验只带来 +34%，**因此我此前"同跑其他任务导致争用"的解释不成立为根因，本记录明确撤回**。该环境的具体成因在本机无法复现，**本记录不声称已查明**。

### 量具

固定墙钟超时把"机器有多快"编码进了放行条件——上一轮那种持续 1637 秒但一直在推进的运行，会被任何固定门槛判失败。现改为：

| 参数 | 值 | 含义 |
| --- | --- | --- |
| `go test -timeout` | `60m` | **仅后备**，不作为判定依据 |
| 停滞预算 | `240s` | 无任何输出超过该时长即判停滞（等于最慢单例的 40 倍） |

判定条件：退出码 0、无失败包、无未说明跳过、无停滞。包耗时改为**观测值**。按此量具，独立环境那次运行（最长间隔 55.07 秒）**不判停滞、在 60 分钟后备内通过**——这正是改造的目的。

安全性有实测：`go test -json` 在管道下确实逐行输出（272 行分布在 4.2 秒，最大行间隔 0.38 秒），停滞检测不会因缓冲误报；卡死的用例无论机器快慢都不产生事件。本轮生产预算实测 `stall=None`、退出 0、399.5 秒。

## 候选身份

```text
工作树   /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
分支     codex/go-engine-v1
提交     41d3375cf16f1bd411f102db2d9edfaeb28c6a29
父提交   c6d49323a54df5b050b902817eee34ffd923f6a1
运行时身份 7226de377d2c330e275df60bd5d48c48d6edb9a6d14961daa026995fcd7c4193
驱动二进制 bb304f0225990093bb9052e67a1410567228cbca000bd604c11b2f57578caf72
解释器   Python 3.13.15，`.harness/venv/bin/python`
已安装依赖 41 项锁定发行版逐项一致，指纹 fdb7f79bed1857d4
证据清单 64 个文件
工具链   go1.26.4 darwin/arm64｜node v22.22.2｜DSH 0.1.5-rc.1
```

冻结工作树 `formal-gates-dogfood-dsh` 保持在 `c6d4932`，本单元未修改其任何受版本管理文件。

**驱动二进制摘要与冻结版不同（`bb304f02…` vs `74786ed4…`）**：两处源码相同，未加 `-trimpath` 时 `go build` 会嵌入源码绝对路径。实测两处都用 `go build -trimpath` 构建后产物逐字节相同（`f1aa7a25c67ab3977cde3a9932b7363b344d40640657906da291ccec6e66bbcd`），差异来自构建路径而非代码。

## 验收过程记录

本单元验收期间被自身准入检查器拦下，并多次自查出缺陷，均保留真实记录：

1. 把工具**源码**文件误并进记账提交 → 准入报 `candidate moved after the record was written` 拒绝。
2. 改控制脚本后未提交 → 脏工作树导致控制项因错误原因"通过"。
3. 编写 E6–E8 时发现检查器"记录位置自我放行"与"记录与尝试从不交叉核对"两个漏洞。
4. `reset --soft` 目标不可达导致 `--amend` 改错提交，已修正为并回候选。
5. 编写 E8/E9/E10 时控制脚本自身三处缺陷（改 `identity.json` 未重建清单、丢 `steps/` 时连带重建清单、取到没有命令的 `identity` 项）。
6. 本轮编写 RS/RG 控制时又发现夹具两处缺陷（未忽略 `.harness/`、假 `summary` 缺 `items`、跨用例未复位），已修正。

每次处置都把变更并回候选并重跑完整验收，未沿用旧结论。更早的尝试目录、记录与拒绝输出均保留在 `.harness/validation/` 下，未被覆盖。

## 未解决问题

| 编号 | 影响 | 处置 |
| --- | --- | --- |
| `NATIVE-REVERIFY-AT-BASELINE` | 阻塞原生能力声明 | 真实模型复验仍未执行，需显式授权费用。授权关口设在 G2b 之前；不阻塞 G1a。上一轮 `live-20260915-2050` 的原生证据绑定更早的提交与运行时，不能当作本候选的原生验收 |
| `ENV-THROUGHPUT-UNIDENTIFIED` | 无法复现独立环境的吞吐差异 | 已量化（约 4 倍），成因未查明；量具已改为不依赖吞吐，不再阻塞放行。若后续出现真实停滞，停滞检测会指名位置 |
| `BUILD-PATH-IN-RUNTIME-IDENTITY` | 运行时身份不是源码内容的纯函数 | 未加 `-trimpath` 时同一份源码在不同目录构建得到不同二进制摘要与 `runtime_identity`；移动或重命名工作树即失效。本单元登记，不改构建方式 |
| `NODE-PATH-AMBIGUOUS` | 跨 shell 恢复可能触发 `HOST_CHANGED` | 机器上两份 node 摘要不同（托管 `5c899797…` / hermes `a9bd0630…`），适配器用 `which('node')` 并写入绑定。做跨 shell 恢复验收前先固定解析来源 |
| `ZCODE-HEADLESS-BLOCK` | 无法无人值守跑完整 ZCode 开发流水线 | 原生 Edit 审批处按合同返回 `ZCODE_INTERACTION_REQUIRED`；官方 MCP 未验证。属既定边界，不得改写成通过 |
| `DRIVER-VERSION-DEFERRED` | 驱动落后于最新版 | v1.59.0 需要 `go 1.25.0`。若确需升级，作为单独的版本化迁移重走完整验收 |
| `REGRESSION-COST` | 累计回归成本 | 单个单元无缓存全量回归本机 462–492 秒（`internal/validate` 392–418 秒）；慢环境下可能远超此数，量具已按此改造 |
| `CONTROL-SCRIPT-NOT-IN-RUNNER` | 控制项仍靠人工在干净工作树运行 | 已断言理由并前置检查工作树；`--only` 诊断模式不能签发记录，故不影响放行。留待 G1a 决定是否纳入运行器 |

## 回退入口

```sh
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine \
  reset --hard c6d49323a54df5b050b902817eee34ffd923f6a1
```

整体撤销隔离面：

```sh
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates \
  worktree remove /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates branch -D codex/go-engine-v1
```

## 复核入口

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-B0.json --next-unit G1a
python3 scripts/go-engine/acceptance.py --unit B0                              # 新尝试目录，不覆盖本次
sh scripts/go-engine/negative-controls.sh "" .gates/results/go-engine-B0.json  # 需要干净工作树
```

机器可读记录：`.gates/results/go-engine-B0.json`。
原始证据：记录 `attempt` 指向的目录，含 `evidence.json`（64 个文件的逐文件摘要）、各必需项的 `summary.json`、每个命令的 stdout/stderr/exit/cmd。
依赖锁定：`scripts/go-engine/dependency-lock.json`。

本记录只汇总已观测结果；不含密钥、认证头或完整原生请求日志。日志保留原始失败，不以最终通过覆盖中间尝试。
