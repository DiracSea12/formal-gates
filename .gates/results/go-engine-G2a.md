# G2a 验收记录（2026-09-18 整条重签）：命令作业 + 承载原生传输

## 结论

替换单元 G2a **收口，`PASS`**。两半在同一候选 `2d363757` 上分别取证：

- **命令作业半**（Go 监督器的直接 argv 检查路径）：验收尝试
  `.harness/validation/acceptance/G2a/20260918T060912Z-34198` 九项全过
  （`overall=PASS`、`failed=[]`）。
- **承载原生传输半**：2026-09-18 经用户明确指示执行的一次真实原生运行（provider `zcode`）
  **到达 `HARNESS_COMPLETE`，7/7 角色全部 `PASS`、零中断、470.3 秒**，且全部七个作业在
  **Go 监督器**下执行并产出收据——§13.1 第 513 行要求的"原生请求及**完成关联**"最小证据就此补齐。

**为什么是整条重签**：旧的命令作业半签在候选 `63b0650` 上，该候选早于 M3 的 P1 修复
`f136303`、自带该缺陷，且与 M3 现在的 `PASS` 不共享候选树——按索引第 27 条，旧半不得复用，
重签必须两半都要。本记录即按该处置执行。

**为什么 provider 是 zcode**：合同对原生半的要求是"验证监督器承载**固定的原生传输**；
不同时更换宿主协议"，**未指定宿主**。DSH 路径两次受阻于环境问题（`sandbox-exec` 在继承自
代理会话的上下文里无法 apply，裸探针 exit 71），迁出继承上下文后也曾 7/7 通过（launchd
上下文、逐字节副本）；zcode 路径在**普通代理会话上下文**直接 7/7 通过。两条宿主路径互相
印证"监督器承载原生传输"这一事实本身。

**候选中两处 zcode 适配器变更的披露**（`d7a687c` source 收据落盘、`2d36375` 运营方权限
模式声明）：均为本单元派发之后、按用户明确授权落地的**公共前置修复**，沿
`6757231`/`8a06fc1`/`4e3fa0e`/`d2f4847` 的既有先例；监督器本体、`native-request.json` 形状、
交互投递与 `verify_source` 未动。`2d36375` 的契约：**新建** dev/返修会话在首个 turn 前经
`session/setMode` 一次性声明运营方无人值守上下文的完全自动模式并回读确认（app-server 把
新会话硬默认为 `build` 且无宿主设置可改、无头客户端无法应答审批）；恢复会话保持既有模式；
绝不在权限询问/超时/失败后改模式；只读会话与探针从不设模式。

| 检查 | 结果 |
| --- | --- |
| identity / go-vet / go-test-full（无缓存全量 396.3s） | PASS |
| sqlite-probe（CGO 开/关各 8/8） | PASS |
| python-unittest（全套 228 项，70.0s） | PASS |
| demo-fixture（`DEMO_COMPLETE`） | PASS |
| doctor-dsh / doctor-zcode（0 次模型调用） | PASS |
| tree-clean-after | PASS |
| 原生定向验收（已授权、已执行） | **`PASS`**：`HARNESS_COMPLETE`、7/7 角色、0 中断 |
| **总体** | **`PASS`** |

上一验收基线：`M3`（记录候选 `aa3312e`）。本候选 `2d363757` 含 `f136303`，血缘已按索引第 27 条核对。

## 原生运行证据（zcode，2026-09-18）

- 运行：`.harness/runs/harness-20260918-133821-34281339`（revision `23e15db2…`，
  模型 `account:bigmodel-individual-coding-plan / GLM-5.3`）。
- 探针与报告：`.harness/validation/zcode-native-third-20260918T053817/zcode-flow/report.json`
  （`passed: true`、`status: HARNESS_COMPLETE`、`interrupts: []`）。
- **完成关联**：七个作业全部经 `store.job → harness supervise` 执行；每个作业 `process.json`
  的 pid **等于**原生子进程的 pgid（监督器是进程组组长，`Store.cancel_jobs` 组取消的前提，
  旧评审问题 3 所要求的显式取证在此落成），每份 `exit.json` 收据 `exit_code=0` 且流证明齐备。
- **只读边界**：只读角色的原生事件中只有 Read/Glob/Grep 调用；Bash（27 次）与 Edit（9 次）
  只出现在 development 作业中。
- 同日两轮先行运行：第一轮暴露 zcode 成功路径不落盘 `source.json`（`d7a687c` 修复，此前任何
  zcode 运行都未完整跑完过角色、故从未踩到完成路径）；第二轮 4 角色 PASS、dev 停在
  `interaction/requestPermission`（设计边界），促成 `2d36375` 的模式声明修订。

## 本单元搬了什么

监督器——**替一个作业目录取得执行所有权**的那段代码——搬进 Go，作为**同一二进制的内部窄命令** `formal-gates harness supervise <dir>` 暴露。合同依据是方案 §12.1（"同一二进制的内部窄命令"路径）与 §13.1 的 G2 行。

| 归属 | 内容 |
| --- | --- |
| **Go（新）** | 取 owner 锁、写 `process.json` 进程身份、按**合并环境**解析并执行 argv、把 stdout/stderr 落盘并 fsync、SIGTERM 取消、写 `exit.json` 收据（含 `request_digest` 与两份流证明） |
| **Python（不变）** | `Store.job` 除**一行 spawn argv** 外全部保留；`Store.cancel_jobs` 未改 |

`Store.job` 的改动只有：

```python
- owner = subprocess.Popen([sys.executable, "-m", "formal_gates_harness.job", str(directory)], ...)
+ owner = subprocess.Popen([self.bridge.binary, "harness", "supervise", str(directory)], ...)
```

即 owner 锁、存活探测、收据轮询、组取消四件事全部保持迁移前的形状。迁移前实现**逐字**移入 `tests/reference_job.py`（仅测试使用，生产永不导入），使差分两侧能作为**各自独立的进程**、在**各自的作业目录**上运行。

### 为什么走"内部窄命令"而不是桥接操作

前七个单元都在同一原则下：**Go 持有规则与格式，Python 持有进程与文件系统**。G2a 搬动的恰恰**就是进程所有权**，与桥接操作的前提冲突：桥接是请求/响应，而监督器要长期存活、要在收到信号时自己改写收据，且子进程必须是**监督器的**子进程而不是 Python 适配器的。因此本单元改用 §12.1 的另一条路径（同一二进制的内部窄命令）。这不是原则的例外，而是原则在此处的必然结论。

### 逐条实测的迁移前语义（全部实测，不靠推断）

| 类别 | 实测结论 |
| --- | --- |
| 退出码 | 子进程正常退出 → `N`；显式 `exit 130` → `130` 且 `error=""`（**不**当作取消）；子进程被自身信号打死 → `-N`（实测 `-1/-2/-9/-15`） |
| `error` 文案 | CPython 的异常类名 + `os.strerror` 的**首字母大写**（`No such file or directory`）。Go 的 `errno.Error()` 是小写，故显式建表 |
| PATH 来源 | 裸 `argv[0]` 按**合并后环境**的 PATH 解析（请求环境优先于监督器自身环境）；无 PATH 时回退 `os.defpath`（`/bin:/usr/bin`） |
| PATH 走查 | 遇到"存在但 exec 不了"的候选（EACCES / 目录 / ENOEXEC）**继续搜索**，记录第一条该类错误；ENOENT 候选不记录；第一个真正执行成功的候选胜出。含分隔符的 `argv[0]` 是**单一候选**，不走 PATH |
| 空 PATH 分量 | `:` 之间的空分量表示**当前目录** |
| owner 锁 | `owner` 已存在 → 退出码 2，**不写** `exit.json`、**不写** `process.json`、**不启动**子进程 |
| 取消 | `Store.cancel_jobs` 向监督器的**进程组**发 SIGTERM，该信号同时到达子进程，故收据必须是**确定性**的 `130` / `owned job was cancelled`，而不是"两者谁先死" |

## 独立复核发现的三处缺陷

| 编号 | 缺陷 | 症状 | 处置 |
| --- | --- | --- | --- |
| D1 | PATH 走查**在第一个"存在"的候选处停住**（用 `os.Stat` 预筛） | 与 `execvp` 语义相悖：`PATH=<不可执行目录>:<可执行目录>` 时应当跑第二个候选，实际报 127。独立 oracle **16 个场景中 5 个分歧**（A/B/V/W/X） | 改为返回**有序候选表**，逐个 `Start()`：ENOENT 继续且不记录，EACCES/ENOEXEC 记录首条后继续，第一个执行成功者胜出。复验 **0/16** |
| D2 | 组信号取消时收据**不确定** | `cancelled` 标志在 goroutine 里置位、在 `cmd.Wait()` 之后读取；当组信号先打死子进程而 goroutine 尚未被调度时，收据会记成子进程自己的 `-15`/`''` 而非 `130`/`owned job was cancelled`。实测：参考实现 **20/20 恒为 130**，Go 侧两种时序下均**不稳定**（`-15` 与 `130` 交替），生产路径 `cancel_jobs` 3 轮中失败 1 轮 | `cmd.Wait()` 移入 goroutine；主 goroutine 用 `select` 在信号通道（优先）与收割通道间择一；子进程被信号打死时给信号通道 **250 ms 有界宽限**以确认组信号是否同样到达监督器。复验 **PASS**，连续 8 次一致 |
| D3 | 四个取消用例**抢跑启动窗口** | 见下（单独成节） | 四个用例全部改为等待"子进程确已运行"的正向证据；实现侧**不动** |

D1、D2 是我在验收前复核交付物时用**自建 oracle** 查出的，不是外部评审发现的；交付的差分测试与子代理的自述当时都是全绿。**这再次印证：子代理的自评不能替代独立复现。** D1、D2 的修复由第二次派发完成，我逐条复验。

### D3：只完整验收的负载下才复现的竞态

**第一次完整验收尝试**（`20260917T094334Z-52257`，保存在 `.harness/validation/` 下，从不被后续运行覆盖）**失败**：

```text
  go-test-full go-test: exit=1 (368.0s)
  -> go-test-full: FAIL (2 problem(s))
       ! packages failed: ['formal-gates/internal/harness']
```

失败用例是本次新增的 `TestSuperviseCancelGroup`：

```text
    supervise_test.go:544: round 1: supervisor exit error: signal: terminated
--- FAIL: TestSuperviseCancelGroup (0.18s)
```

它**单独运行时通过**，只在 `go test ./...` 的负载下失败 20 轮中的 1 轮。

根因：四个取消用例都等到 `process.json` 出现就发信号，并假定此时 SIGTERM handler 已装好。**并非如此**——`process.json` 是在 handler 装好**之前**写的，两侧都一样：

| 实现 | 写 `process.json` | 装 SIGTERM handler |
| --- | --- | --- |
| Go `supervise.go` | `Supervise` 前段 `writeAtomicJSON(process.json)` | 之后进入等待循环时 `signal.Notify` |
| 迁移前 `reference_job.py` | 第 31 行 `atomic(directory / "process.json", ...)` | 第 38 行 `signal.signal(signal.SIGTERM, terminate)` |

落在这个窗口里的信号会把监督器**直接打死**：没有 `exit.json`、没有收据，进程以 `signal: terminated` 结束。`TestSuperviseCancelGroup` 中那行注释（"Past the startup window and after the SIGTERM handler is installed"）与代码实际等待的东西**不符**，是这次缺陷的直白证据。

**该窗口是参考实现固有的，因此有意不关闭**：若在窗口里写出 130 收据，Go 就会在参考实现**根本没有收据**的地方给出收据——`Store.job` 对缺失收据的处置是 `RuntimeBlocked(UNKNOWN_JOB)`，两者可观察地不同。所以正确处置是**修用例、不动实现**。

修法：四个用例改为等待"**子进程确已运行**"的正向证据——子进程先 `touch` 一个 marker 再 `exec` 长睡眠，而 handler 的装载严格早于第一次 spawn，故"有子进程在跑"足以证明监督器已进入等待循环。两侧各用**独立** marker：共享一个 marker 只能证明其中一侧已就绪，另一侧仍可能在启动窗口里被信号打死（修复过程中先撞上过这个次生问题，实测表现为 `-15`）。

`TestSuperviseCancelGroup` 的 20 轮与全部断言（退出码 130、错误文案、收据键集、哨兵存活、组长判定）逐条保留，未加任何重试、未放宽任何断言。

## 负控制

| 变异 | 结果 |
| --- | --- |
| **PATH 来源改为监督器自身环境、且去掉 defpath 回退**（`searchCandidates`） | **被检出**：执行语义 oracle **17 中 2 处分歧**（"请求 PATH 优先"与"无 PATH 时 defpath 找到 `ls`"），退出码 1 |

变异后源码与二进制**逐字节还原**（`shasum`：`supervise.go` `efebc030636fee29…`；驱动二进制 `8bca2e2ca56b5e09…`，与还原前一致）。

**如实登记一次失败尝试**：第一次负控制试图拿掉"空 PATH 分量表示当前目录"这一分支（把 `if dir == "" { cand = "./" + name }` 简化为 `filepath.Join(dir, name)`），oracle **没有**报分歧——它是一次**等价变异**：`filepath.Join("", "tool")` 得到 `"tool"`，而 `execve` 对**不含分隔符的相对路径**按 cwd 解析（PATH 搜索是 `execvp` 的行为、不是 `execve` 的），故与 `"./tool"` 实际等效。记录它的原因是它同时说明：**该分支在 macOS 上不被该 oracle 区分**，只由 Go 单元测试覆盖；"变异没被检出"与"变异是等价的"必须分开判断，不能一律当作 oracle 失效。

D1、D2 的 oracle 在修复前**确实失败过**（5/16 分歧、取消不稳定），这本身即是这两个 oracle 有效力的原始证据。


## 受阻史（保留，均已处置）

- **2026-09-17，dsh，两次同配置受阻于第一角色**：只读沙箱在外层上下文无法 apply
  （`SANDBOX_UNAVAILABLE` / 裸 `sandbox-exec` 探针 exit 71）。定性为环境问题、非回归。
  证据目录随 2026-09-18 证据清理被回收（索引第 26 条）。
- **2026-09-18，dsh，launchd 上下文一次通过**：7/7、89 秒，但在 `/tmp` 逐字节副本上（TCC
  限制），未作 record 级运行。证据目录同样被回收。
- **2026-09-18，zcode，record 级运行（本记录绑定）**：普通会话上下文、真实工作树，无需任何
  上下文迁移手段。

## 评审问题的处置（承接旧记录）

1. **G2a 是否应拆成两个验收单元**：本重签按"一个单元、两半都有证据"处置——同一记录同时
   绑定九项验收（命令作业半）与授权原生运行（传输半）。此前按严格读法保持未验收的处置维持
   不变；本次是证据补齐，不是改判。
2. **`sandbox_permissions` 提权请求的预判阻断**（旧问题 2）：维持"如实转述宿主请求"；zcode
   路径以运营方模式声明从源头消解了该场景（dev 会话不再触发审批请求）。
3. **组长关系的显式断言**（旧问题 3）：本次七个作业的 `process.json.pid == native pgid` 已作为
   完成关联证据记录在案。
4. **受阻运行的费用记账**（旧问题 4）：本次三轮运行的授权与用途已在 `native_verification`
   的 `history` 中逐条登记；`usage_evidence` 落盘仍缺（见下）。

## 候选身份（2026-09-18 重签）

```text
工作树   /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
分支     codex/go-engine-v1
提交     2d363757a575850f0c602c2cf25ae59b2018d77b
上一基线 M3（记录候选 aa3312e）
运行时身份 8b2047217e97cf6eaa2fb0824749b830b0ebc2925af18f83305c09651e6e3bbe
驱动二进制 5bd9e2438443a0bc5e4a63c9dd9e7c10923ecfe5421b3249240131ffc11bdf51
证据清单 61 个文件
通过尝试 20260918T060912Z-34198
```

驱动二进制由验收器自行从候选源码构建并计入 identity 项。运行时身份较旧记录
（`a50f2289…`）改变属预期：源码变了（M3 修复与两处 zcode 前置修复）。

## 明确未验证 / 已知边界

- 跨平台：本机 macOS 实测。
- `usage_evidence` 未落进 source 收据（各角色为 None），用量记账不完整（独立小缺口，不阻塞本单元）。
- 旧 DSH 尝试的原始证据目录已随 2026-09-18 清理回收（索引第 26 条）；其结论以索引与 git 历史为准。
- capability_validation 各项（subagents/mcp/network/read_only_tools 深度验证）仍为 `not_verified`；
  本运行证明流程完成与完成关联，不主张更深的宿主能力验证（`doctor` 语义不变）。

## 回退入口

```sh
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine \
  reset --hard aa3312e
```

与既有记录同一约定：指向上一基线（M3）的**候选**提交。

## 复核入口

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine

# 准入：**预期放行**——本记录 overall=PASS 且两半证据齐备
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-G2a.json --next-unit G2b

# 原生半证据（只读）
cat .harness/validation/zcode-native-third-20260918T053817/zcode-flow/report.json | python3 -m json.tool | head -30
R=.harness/runs/harness-20260918-133821-34281339
for j in "$R"/jobs/*/; do
  python3 -c "import json;p=json.load(open('$j/process.json'));n=json.load(open('$j/native-process.json'));e=json.load(open('$j/exit.json'));print(p['pid'],n['pgid'],e['exit_code'])"
done  # 期望：七行，每行 supervisor_pid == native_pgid，exit 0

# 本块合同
go test ./internal/harness -run 'Supervise' -v

# 差分对照（PYTHONPATH 覆盖掉宿主的 sitecustomize 垫片，见索引）
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$PWD/runtime/langgraph" TMPDIR=/private/tmp \
  .harness/venv/bin/python -m unittest discover -s runtime/langgraph/tests \
  -p 'test_job_supervision_differential.py' -v

# 三个独立裁判（不依赖交付的测试）
export PYTHONPATH="$PWD/runtime/langgraph" TMPDIR=/private/tmp
.harness/venv/bin/python .gates/results/g2a-probes/reference_semantics.py
.harness/venv/bin/python .gates/results/g2a-probes/path_search_oracle.py    # D1：期望 0/16 分歧
.harness/venv/bin/python .gates/results/g2a-probes/exec_semantics_oracle.py # 期望 0/17
.harness/venv/bin/python .gates/results/g2a-probes/cancel_oracle.py         # D2：期望 PASS

# 直接以内部窄命令驱动一次作业
job=$(mktemp -d)
printf '%s' '{"kind":"check","argv":["/bin/sh","-c","printf HI; exit 7"],"cwd":"'"$job"'","stdin":"","environment":{}}' > "$job/request.json"
.harness/bin/formal-gates harness supervise "$job"; echo "supervisor rc=$?"
cat "$job/exit.json"; echo; cat "$job/stdout.log"; echo
```

单元合同：`.gates/results/go-engine-G2a-contract.md`（派发前固定的历史件，其中"授权关口在
G2b 之前"的命名错误与"宿主协议不变"约束按本记录与索引的更正读法理解）。
本记录只汇总已观测结果；日志保留原始失败，不以最终通过覆盖中间尝试。
