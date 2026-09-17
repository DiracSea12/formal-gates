# G2a 验收记录：命令作业（Go 监督器的直接 argv 路径）

## 结论

替换单元 G2a **未收口**。它有两半：**命令作业半通过**（候选 `63b0650`，本记录的验收尝试 `20260917T100854Z-99375` 对此半全部通过），**承载原生传输的半受阻**——2026-09-17 经用户显式授权费用执行了一次真实原生运行，它在第一个角色处因宿主权限请求而停住（详见下文"原生定向验收（已授权、已执行、受阻）"）。按方案 §13.1（"必要原生验证受权限、服务或费用授权阻塞时，当前单元保持未验收"）与 §14.3（"必需测试未结束、被跳过或原生验证受阻 | 保持未验收；不将缺测视为 PASS 或改标不适用"），**G2a 作为整体保持未验收，G2b 不得开始**。

**命名更正**：本记录此前写作"下一单元为 G2b（原生定向验收关口）"，这是错的。方案 §13.1 的表把 G2b 定义为"**DSH 薄适配**"，而"验证监督器承载固定的原生传输"是 **G2a 自己的后半**（§13.1 表内即写"先交付 Go 监督器的直接 argv 检查路径，**再验证它承载固定的原生传输**"；§13.1 正文写"G2a **先验命令监督，再验其承载原生传输**"）。把原生验收说成 G2b 的关口，是本记录的一处实质错误，已连同索引一并更正。

| 检查 | 结果 |
| --- | --- |
| 本块合同（Go 单元测试 `supervise_test.go`） | 通过：**14 个测试函数、31 个通过事件**（含 17 个子用例），覆盖退出码、流字节、stdin、环境合并、cwd、spawn 错误、PATH 解析与回退、owner 锁、收据形状、取消（进程内与进程组）、`kind` 不参与 |
| 本块合同（差分对照） | 通过：**16 项**，收据逐字段、流字节逐字节与迁移前实现一致 |
| 独立裁判比对（我自己构造，**不依赖**交付的测试与参考模块） | 通过：**三个 oracle 共 33 个场景 + 取消 23 轮，0 分歧**（PATH 搜索 16、执行语义 17、取消稳定性 20 轮 + 作用域 3 轮） |
| **独立复核发现 3 处交付缺陷（D1、D2、D3）** | 全部已修并复验；D3 只在**完整验收的负载下**才复现 |
| **负控制** | 通过：PATH 来源 + defpath 回退的定向变异被检出（17 中 2 处）；另**如实登记一次等价变异**（见下） |
| 累计组合完整流程（fixture） | 实际达到 `DEMO_COMPLETE` |
| 累计自动化回归（Go 无缓存全量） | 通过：26 包、23 包执行、3 包无测试文件、**2065 用例通过事件、0 失败、`stall=None`** |
| 累计自动化回归（Python 全套） | 通过，**190 项** |
| SQLite 探针 | 通过，CGO 开/关各 8/8 |
| DSH / ZCode `doctor` | 通过，接入探测 |
| 原生定向验收 | **`BLOCKED`（不是"不适用"）**：本单元搬的正是**进程所有权**，按方案 §13.1 第 513 行属"改动涉及…进程…时，实测受影响宿主的对应路径"，按同章第 520 行属"涉及宿主或进程行为的单元必须增加对应真实调用"。2026-09-17 经授权执行一次真实 DSH 原生运行，在第一个角色（产品审）处停在 `session/request_permission`，宿主返回 `DSH_INTERACTION_REQUIRED`。**受阻事实保留，不改标不适用**（§13.1 第 541 行、§14.3 第 681 行）。证据：`.harness/validation/g2a-native-20260917T103650Z/` |
| **总体** | **`BLOCKED`**（命令作业半 `PASS`；承载原生传输半未验收） |

上一验收基线：`M3`（候选 `d71113e`，记录提交 `603f019`）。

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

## 原生定向验收（已授权、已执行、受阻）

这一节是 G2a **后半**的验收尝试。它不是 `acceptance.py` 的一次尝试，而是一次真实宿主运行；证据在 `.harness/validation/g2a-native-20260917T103650Z/`。

### 开工前的绑定（先固定身份，再花费用）

| 项 | 值 |
| --- | --- |
| HEAD | `c0f306ba4431160cd0ce61e688e60353828481dd`，工作树干净 |
| 驱动二进制 | `.harness/bin/formal-gates` == `8bca2e2ca56b5e0979dfd01fe9f3828043b5dc7ad97335acc7963e06c129da3a`（与候选 `63b0650` 一致） |
| 运行时身份 | `a50f2289fa9ebb7857dd49ac8295397c9b2b1ac1058fd357f80e06147adac756` |
| 生产路径的监督器 | `Store.job` 只有一行：`owner = subprocess.Popen([self.bridge.binary, "harness", "supervise", str(directory)], …)` |
| **负证明**：迁移前的 Python 监督器已不存在 | `formal_gates_harness.job` 模块缺失 → 直接派生它以 `exit=1`、`No module named formal_gates_harness.job` 退出（`negative-proof-old-supervisor.{exit,stderr,stdout}`） |

负证明的用途：既然旧实现已经不可运行，那么本次作业里出现的每一份收据都**只能**来自 Go 监督器。这替代了本环境中做不到的进程表观察（`/bin/ps` 与 Python 的 `subprocess` 调用 `ps` 在本机都被 OS 层拒绝，`EPERM`）。

### 实际运行

```sh
.harness/venv/bin/python runtime/langgraph/tests/native_multihost.py --provider dsh \
  --out .harness/validation/g2a-native-20260917T103650Z/dsh-flow
```

| 项 | 实测 |
| --- | --- |
| 运行目录 | `.harness/runs/harness-20260917-183818-280a7a16` |
| 模型 | 宿主实际选择的 `deepseek-official / deepseek-v4-flash`，思考配置 `high` |
| 流程到达 | `stage-start: approval` → `user-decision: approve` → `stage-complete: approval` → `stage-start: product` → `worker-start: product-review` → `job-start: kind=native` |
| 终态 | `passed: false`、`status: WAITING_USER`，无任何角色完成（`roles: []`） |
| 中断 | `NATIVE_BLOCKED: RuntimeBlocked: DSH_INTERACTION_REQUIRED: native permission needs a user decision; none was sent` |

**停在哪里**：产品审角色真实建立了 DSH 会话（`sessionId dab165ec-1074-4eb4-8886-27a89f91356c`、`inputId 15e491ca-…`、`turn 1`、真实 transcript 路径），用 `read` 读了 `requirement.md`/`task.json`/`reviewer-base.md`，用 `glob` 与 `bash`（`ls -la; git status; git log`，**未请求提权**）做了只读勘察——这些都正常执行。随后它在第 49 号序列上发出了一次 `bash` 调用，其参数里带：

```json
{ "sandbox_permissions": "danger-full-access",
  "justification": "The read-only sandbox has no usable backend on this host, so I need unconfined execution once to inspect the bound revision and run the two requirement-bound checks for this review." }
```

DSH 据此发出 `{"id": 0, "method": "session/request_permission"}`（`protocol.jsonl` 第 55 号序列），轮次以 `{kind:"aborted", reason:{kind:"user"}}` 结束。本版本**没有**原生答复通路，无人能回答该请求，故宿主按合同报告 `DSH_INTERACTION_REQUIRED`。

### Go 监督器确实承载了这次原生传输（后半中被证明的那一半）

| 证据 | 内容 | 说明 |
| --- | --- | --- |
| `request.json` | `kind=native`，`argv = [<venv>/bin/python, "-m", "formal_gates_harness.native_entry", <job 目录>]` | 是**原生入口** argv，不是检查 argv。§13.1 要验的正是"监督器把这条 argv 当普通子进程执行" |
| `exit.json` | **恰好六个字段**：`exit_code=1`、`error=""`、`finished_at`、`request_digest=c2f2106e…`、`streams`（`stdout.log` 0 字节 `e3b0c442…`；`stderr.log` 97 字节 `a92bb47d…`） | 与合同规定的收据形状**逐字段一致**，流证明齐备 |
| `native-process.json` | `{"pgid":30044,"pid":30048}`，而 `process.json` 的 `pid` 也是 `30044` | 监督器是**进程组组长**（pgid == 自身 pid），原生子进程 30048 是**它的**子进程、在**它的**组里。这是 §8.1"独立监督器取得该调用的执行所有权"与 `Store.cancel_jobs` 的组判定所依赖的性质 |
| `stderr.log` / `native-error.json` | 都是 `RuntimeBlocked: DSH_INTERACTION_REQUIRED: native permission needs a user decision; none was sent` | 子进程的失败报告被**逐字**落盘并转入收据流证明；`Store.job` 的轮询据此得到 `RuntimeBlocked`，再升级为本次 run 的 `NATIVE_BLOCKED` 中断 |
| `wrapper.stderr` | 0 字节 | 与合同"监督器 stdout 静默、自身诊断走 stderr"一致 |

**结论**：监督器的那一半——**执行原生 argv、取得所有权、写出收据、如实转述流与退出码**——是被证明的。**没有**被证明的是**原生完成关联**：方案 §13.1 第 513 行给"原生定向验收"规定的最小证据是"真实宿主/配置、原生请求及**完成关联**、实际文件或服务端证据"，而本次没有任何角色抵达完成的轮次。因此这一半是**受阻**，不是通过、也不是不适用。

### 与先前的通过运行对照：阻塞原因已定位为模型自己的提权请求

`live-20260915-2050`（另一 worktree，`codex/dogfood-dsh`，DSH `0.1.5-rc.1`）曾让**同一宿主**的七个角色全部通过并到达 `HARNESS_COMPLETE`。同一宿主、同一版本为何这次第一个角色就停？逐项对照如下。

| 对照项 | 先前通过的那次 | 本次 |
| --- | --- | --- |
| 宿主与版本 | DSH `0.1.5-rc.1` | **相同** |
| harness 工具策略 | `read_only:true`，`allowed` 含 `bash`/`read`/`subagent`… | **同一集合**（仅 `directory` 指向各自 job 目录，属设计差异） |
| 角色 `bash` 调用数 | **32** | 4 |
| 其中进入 `…/workspaces/…`（越出 job 目录） | **16** | 1（seq 49） |
| 其中运行同类检查命令（`<venv>/bin/python check_add.py`，含 `PYTHONDONTWRITEBYTECODE=1`） | **13** | 1（seq 49） |
| 其中带 `sandbox_permissions` 提权声明 | **0** | **1**（seq 49，`danger-full-access`） |
| 全部作业中的 `session/request_permission` 数 | **0** | 1 |

（先前那次的 `bash` 计数以解析后的工具调用为准；按文本 grep `"name":"bash"` 会得到两倍，因为流式事件里每个调用还会出现一次 `tool-call-chunks`。）

三点因此可以排除：**不是路径范围**（先前那次的 bash 有 16 次同样进入 `workspaces/…` 而未被拦）、**不是配置差异**（工具策略逐项相同）、**不是 Go 监督器**（同一次运行里 seq 24 的 bash 与全部 `read`/`glob` 都正常执行，阻塞发生在模型提交提权请求之后）。

模型给出的理由（"只读沙箱在本机没有可用后端"）**与证据不符**：先前那次在只读沙箱内执行了同一类命令、13 次运行检查脚本而**没有**任何提权；更早的 DSH 专项验收还记录过只读会话真的尝试追加文件并被 `Operation not permitted` 拒绝——即只读沙箱在本机是有后端、且确实生效的。

### 处置：这是既定的无头交互边界，不是回归

1. **本版本按设计没有原生答复通路。** 权限投递的既有工作是**未实现**的，且有明确记录：`permission-fix-20260915-2236/final-audit.json` 写 `permission_bridge: "BLOCKED: file_edit add permissions.py rejected by tool safety check; file not created"`、`completion: "PARTIAL: automatic mode regression corrected; live permission forwarding not implemented"`、`model_calls: 0`。
2. **原生交互是方案里的独立单元。** §12.4 要求"原生交互通路与宿主语言迁移**分别验收**"，§13.1 把 `G2d：原生交互`单列为"原生请求持有、展示、投递和原调用继续"，**正是为了让 G2a/G2b 不顺手获得该能力**。在 G2d 之前，遇到宿主权限请求时唯一合规的动作就是如实报告能力不足。
3. **harness 的行为正是方案要求的行为。** §6.2/§13.1："没有原生答复通路的版本明确报告能力不足，**不靠免审批模式让测试通过**"；§14.2："宿主原本关闭某能力 | harness 不开启；**测试不靠临时强制授权凑通过**"；§14.2："原生受限于平台能力而阻塞时**保留该事实，不自动修改宿主配置完成测试**"。
4. **与既有的 ZCode 记录同类。** 先前的验收记录在 ZCode 于真实 Edit 审批处返回 `ZCODE_INTERACTION_REQUIRED` 时写的是"**这符合既定交互边界**，但也意味着目前不能在该配置下无人值守跑完整条 ZCode 开发流水线"。本次 DSH 的情形与之同型，索引中已有一条 `ZCODE-HEADLESS-BLOCK` 与之并列。

**因此 G2a 保持未验收**（方案 §13.1"必要原生验证受权限、服务或费用授权阻塞时，当前单元保持未验收，不擅自消费额外权限或跳过测试继续推进"）。本次**没有**再重跑：重跑须重新取得授权，而且阻塞取决于模型是否选择提权（先前那次七个角色都没有提权），"模型这次没提权所以过了"本身并不构成对监督器的验证。

### 什么能收口 G2a 的后半

任一条即可，但都需要新的授权或评审裁定：

1. 在一次真实运行里，至少一个角色经 Go 监督器**抵达完成的轮次**，并取得完成关联（`sessionId`/`inputId`/`turn`）与实际文件效果；
2. 或者，由评审裁定 §13.1 的 G2a 应作**两个验收单元**（合同"待评审的判断"第 1 条已提出该问），从而"承载原生传输"只由合同第 178 行那两点结构性证据支撑：**逐字的原生入口 argv 形态跑通** + **监督器不读 `request.json` 的 `kind`**。

本记录采取第 1 条所要求的严格读法，因为那与 §13.1 第 513 行给该检查规定的最小证据（"原生请求及**完成关联**"）一致。

## 候选身份

```text
工作树   /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
分支     codex/go-engine-v1
提交     63b0650f4ecaf5a6e709b78b8f16170f6aa21d4e
上一基线 d71113e5ada1f84c9f6f5d94b022e33dfba477e3（M3）
运行时身份 a50f2289fa9ebb7857dd49ac8295397c9b2b1ac1058fd357f80e06147adac756
驱动二进制 8bca2e2ca56b5e0979dfd01fe9f3828043b5dc7ad97335acc7963e06c129da3a
证据清单 64 个文件
通过尝试 20260917T100854Z-99375（7 分 15 秒）
失败尝试 20260917T094334Z-52257（保留，未被覆盖）
```

驱动二进制摘要 `8bca2e2c…` 与我在验收前**从候选源码重建**所得的摘要一致，也与**重建后可复现**的结论一致（`go build -o .harness/bin/formal-gates ./cmd/formal-gates` 连续两次得到同一摘要），故该二进制可确认来自被验收的候选，而不是旧产物。运行时身份较 M3（`e15fde53…`）改变属预期：源码变了。

D3 的修复以 **amend** 并入候选提交（该提交未推送，分支无上游）；候选因此仍是**单一**提交 `63b0650`。

## 累计回归的实际结果

| 项 | 耗时 | 说明 |
| --- | --- | --- |
| `identity` | 0.9s | |
| `go-vet` | 0.5s | |
| `go-test-full` | 367.9s | 26 包、23 包执行、3 包无测试文件、**2065 用例通过事件**、0 失败、`stall=None`；最慢 `internal/validate` 360.8s、`internal/cli` 78.1s、`internal/engine/acceptance` 17.9s |
| `sqlite-probe` | 3.0s | CGO 开/关各 8/8 |
| `python-unittest` | 54.1s | **190 项** |
| `demo-fixture` | 2.8s | 达 `DEMO_COMPLETE` |
| `doctor-dsh` / `doctor-zcode` | 2.0s / 2.7s | 接入探测 |
| `tree-clean-after` | 0.04s | 运行未改动工作树 |
| 合计 | 433.9s | |

相对 M3：Go 用例通过事件 `2034 → 2065`（**+31**）、Python 用例 `174 → 190`（**+16**）。

两处增量都与新增用例数**精确对应**，既有用例未被改写：

- +31 = `supervise_test.go` 的 14 个顶层用例 + 17 个子用例（该文件 `-run Supervise` 单独跑出的通过事件数同为 31）；
- +16 = `test_job_supervision_differential.py` 中新增的 16 项差分用例（该文件单独跑为 `Ran 16 tests`）。

唯一的 skip 是 `internal/validate::TestNormalizeHostPath`（Windows 专用，darwin 上带明确理由跳过），与既往一致。

G2a 的**通过**尝试只有一个（`20260917T100854Z-99375`），`overall=PASS`、`failed=[]`；同单元的失败尝试 `20260917T094334Z-52257` 作为证据保留。

## 未解决问题

| 编号 | 状态 |
| --- | --- |
| `CANCEL-WINDOW-REFERENCE-INHERITED` | **新增（有意保留的继承行为）**：`process.json` 写在 SIGTERM handler 装载之前，落在该窗口的组信号会让监督器无收据地死掉。**两侧一致**，故不关闭；`Store.job` 已把它映射为 `RuntimeBlocked(UNKNOWN_JOB)`。实测 6 轮中两侧均未落进窗口（0/6），但窗口客观存在。D3 即由此而来 |
| `CANCEL-POSTCHILD-WINDOW` | **新增（已知有界差异，未构造用例）**：参考实现的 handler 以 `raise KeyboardInterrupt` 实现，会在**任意**执行点抛出；若信号落在外层 `try` 结束之后、写 `exit.json` 之前，参考实现会带 traceback 退出且**不写收据**。Go 在 `waitChild` 返回后不再理会已缓冲的信号，会**正常写收据**。窗口极窄（子进程收割与收据落盘之间），Go 侧更稳定；按"实测差异 + 不可达论证"记录，未加机制 |
| `SUPERVISE-NATIVE-FORM-DIGEST` | **新增（测试约定，非差异）**：原生入口形态下 `argv` 里含**各自**的作业目录，故 `request_digest` 两侧**合法地不同**，差分用例只断言**形状**相同（退出 1、`error=""`、写出 `native-error.json`、两份流证明齐备）。若评审认为应连 `request_digest` 一并相等，需要先给出两侧共享目录的办法（与 M3 的 D1 同一类约束） |
| `EMPTY-PATH-COMPONENT-ORACLE-BLIND` | **新增（覆盖边界）**：`PATH` 空分量（当前目录）在 macOS 上**不被**该 oracle 区分（见负控制一节的等价变异分析），只由 Go 单元测试覆盖 |
| `NATIVE-GATE-BLOCKED-BY-MODEL-ESCALATION` | **新增（本单元真实阻塞项）**：G2a 后半的真实原生运行停在宿主权限请求上。触发者是模型自己在 `bash` 参数里写 `sandbox_permissions: "danger-full-access"`，而非配置、路径范围或 Go 监督器——同一宿主、同一版本、同一工具策略下，先前那次的 32 次 `bash`（其中 16 次进入 `workspaces/…`、13 次运行同类检查脚本）**0 次**提权、**0 次**权限请求。本版本无原生答复通路（见 `NATIVE-ANSWER-PATH-ABSENT`），故唯一合规动作是报告能力不足。**保持未验收** |
| `NATIVE-ANSWER-PATH-ABSENT` | **新增（既定的能力缺口，非本单元回归）**：本版本**不能**回答宿主的原生权限请求。既有工作的记录是 `permission_bridge: BLOCKED: file_edit add permissions.py rejected by tool safety check`、`completion: PARTIAL: … live permission forwarding not implemented`、`model_calls: 0`（`permission-fix-20260915-2236/final-audit.json`）。方案 §12.4/§13.1 把该项单列为 **G2d：原生交互**。与索引中既有的 `ZCODE-HEADLESS-BLOCK` 同型；先前验收记录对 ZCode 的同类阻塞写的是"这符合既定交互边界" |
| `MODEL-ESCALATION-JUSTIFICATION-UNSUPPORTED` | **新增（本次实测发现，写给宿主评审）**：模型为提权给出的理由——"只读沙箱在本机没有可用后端"——**与证据不符**。先前那次在同一只读沙箱里 13 次运行检查脚本均成功且未提权，更早的 DSH 专项验收还记录过只读会话写被 `Operation not permitted` 拒绝（即沙箱确有生效的后端）。这不改变处置（harness 仍不得自行授予提权），但它说明"模型自述的阻塞原因"也不可直接当作环境事实 |
| `NATIVE-TRANSPORT-CARRIED-BUT-NO-COMPLETION` | **新增（本单元后半的精确状态）**：Go 监督器**确实**承载了原生传输——原生入口 argv 被当普通子进程执行、`exit.json` 六字段齐备且流证明正确、`native-process.json` 显示监督器是进程组组长（pgid == 自身 pid）而原生子进程是它的子进程——但**没有完成关联**（无角色抵达完成的轮次）。方案 §13.1 第 513 行要求的最小证据含"完成关联"，故该半为**受阻**而非通过 |
| `HOST-SHIM-BREAKS-PATH-OPS`、`TMPDIR-UNRESOLVED-SYMLINK`、`COPY-STATUS-UNREACHABLE`、`MALFORMED-RECORD-ERROR-SHAPE`、`BOOL-SIZE-IS-A-NUMBER-IN-PYTHON`、`NON-FINITE-CROSSES-TRANSPORT`、`INPUT-PATH-NOT-NORMALISED`、`ARTIFACT-READ-COSTS-TWO-CALLS`、`ARTIFACT-LOCATION-STAYS-IN-PYTHON`、`INDEX-MISSING-M2-ROW`、`GOFMT-WINDOWS-FILE-PREEXISTING`、`WORKTREE-LOCATION-AMBIGUOUS`、`ZCODE-HEADLESS-BLOCK` 等 | 保留，不变 |
| G2a | **未收口**：命令作业半 `PASS`（候选 `63b0650`），承载原生传输半 `BLOCKED`。**G2b 不得开始**（方案 §13.1"必要原生验证受权限、服务或费用授权阻塞时，当前单元保持未验收"、§14.3"必需测试未结束、被跳过或原生验证受阻 | 保持未验收"）。G2b 是 **DSH 薄适配**，不是"原生定向验收关口" |

## 评审问题

1. **G2a 的严格读法是否成立**：本记录按方案 §13.1"G2a 先验命令监督，**再验其承载原生传输**"与 §13.1 第 513 行的最小证据（含"完成关联"）判定后半受阻、单元未收口。合同"待评审的判断"第 1 条已预告该分歧：另一种读法是"§13.1 把 G2a 列为**一行**、给一组出口条件（§13.1 表内出口只有命令作业的三条属性），故承载原生传输只由合同第 178 行的结构性证据支撑（原生入口 argv 逐字跑通 + 监督器不读 `kind`）即可"。**若采后一读法，请明确裁定 G2a 收口，并请一并裁定本记录与索引的更正措辞。**
2. **`sandbox_permissions` 提权请求是否应被 harness 预判并阻断为"能力不足"**：当前实现是把它作为一次普通的原生请求送达上层，再由上层报 `DSH_INTERACTION_REQUIRED`。另一案是在适配层识别提权字段并**直接**报能力不足（不建立会话、不消耗模型调用）。两者可观察行为不同：后者会让本次运行在更早的位置、以更少的费用停下。是否需要这样一条路径，还是保持"如实转述宿主请求"？
3. **`native-process.json` 的组长关系是否应升级为显式断言**：本次它以 `{"pgid":30044,"pid":30048}` 与 `process.json` 的 `pid: 30044` 交叉印证了"监督器是组长"。该性质是 `Store.cancel_jobs` 组取消的前提，目前只由"Python 侧 `start_new_session=True`"与差分用例间接覆盖。是否应加一条直接断言，并把它纳入真实原生运行的取证清单？
4. **一次被宿主角落阻塞的真实原生运行，其费用是否算"已消费的授权"**：本次运行消耗了真实模型调用却没有产出完成关联。方案只规定"不擅自消费**额外**权限"与"受阻则保持未验收"，未规定"一次受阻的运行"如何记账。是否需要一条规则（例如：受阻运行须在记录中显式标注已消费的调用范围与理由），以免"受阻"成为反复试跑的通道？

## 回退入口

```sh
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine \
  reset --hard d71113e5ada1f84c9f6f5d94b022e33dfba477e3
```

与 M1、M2、M3、C4 记录同一约定：指向上一基线的**候选**提交。

## 复核入口

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine

# 准入：**预期被拒绝**——本记录的总体结论是 BLOCKED，故不得放行 G2b
# （方案 §13.1"当前单元保持未验收"、§14.3"拒绝派发/实施下一替换单元"。
#   admission.py 先看 overall，因此会以 `recorded overall conclusion is 'BLOCKED'` 拒绝。）
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-G2a.json --next-unit G2b

# 走一遍仍未收口的那一半的证据（不需要新的模型调用，只读）
ls .harness/validation/g2a-native-20260917T103650Z/
cat .harness/validation/g2a-native-20260917T103650Z/dsh-flow/report.json
# 监督器承载原生传输的收据与身份（六字段、两份流证明、组长关系）
J=.harness/runs/harness-20260917-183818-280a7a16/jobs/7780d44222abf647d6c1e13010ffd562dd9349374fcaf8240367403fc8d6447c
cat "$J/request.json"; echo; cat "$J/exit.json"; echo; cat "$J/process.json"; echo; cat "$J/native-process.json"; echo
# 停在权限请求处的原始协议记录
grep -n request_permission "$J/protocol.jsonl"
# 迁移前的实现确实已不可运行（负证明）
cat .harness/validation/g2a-native-20260917T103650Z/negative-proof-old-supervisor.exit \
    .harness/validation/g2a-native-20260917T103650Z/negative-proof-old-supervisor.stderr

# 本块合同
go test ./internal/harness -run 'Supervise' -v

# 差分对照（PYTHONPATH 覆盖掉宿主的 sitecustomize 垫片，见索引）
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$PWD/runtime/langgraph" TMPDIR=/private/tmp \
  .harness/venv/bin/python -m unittest discover -s runtime/langgraph/tests \
  -p 'test_job_supervision_differential.py' -v

# 三个独立裁判（我自己构造，不依赖交付的测试）
export PYTHONPATH="$PWD/runtime/langgraph" TMPDIR=/private/tmp
.harness/venv/bin/python .gates/results/g2a-probes/reference_semantics.py   # 迁移前语义的重新实测
.harness/venv/bin/python .gates/results/g2a-probes/path_search_oracle.py    # D1：期望 0/16 分歧
.harness/venv/bin/python .gates/results/g2a-probes/exec_semantics_oracle.py # -N 退出码、空 PATH 分量：期望 0/17
.harness/venv/bin/python .gates/results/g2a-probes/cancel_oracle.py         # D2：期望 PASS

# 直接以内部窄命令驱动一次作业（最小心智负担的复现路径）
job=$(mktemp -d)
printf '%s' '{"kind":"check","argv":["/bin/sh","-c","printf HI; exit 7"],"cwd":"'"$job"'","stdin":"","environment":{}}' > "$job/request.json"
.harness/bin/formal-gates harness supervise "$job"; echo "supervisor rc=$?"
cat "$job/exit.json"; echo; cat "$job/stdout.log"; echo
```

单元合同：`.gates/results/go-engine-G2a-contract.md`。
本记录只汇总已观测结果；日志保留原始失败，不以最终通过覆盖中间尝试。

本次授权原生运行的完整证据（含原始 `report.json`、调用输出与负证明）在
`.harness/validation/g2a-native-20260917T103650Z/`；它所绑定的 run 目录
`.harness/runs/harness-20260917-183818-280a7a16/` 一并保留，未被改写。
