# G2a 验收记录（2026-09-18 第四次整条重签，三轮评审返修后）：命令作业 + 承载原生传输

> **第四次重签缘起**：第三轮评审（`g2a-raw-evidence-review-20260918T103345Z/`，含真实 Windows 设备
> 执行）确认上一轮两项修复有效，但再驳两项 P1——原始证据核验只走到 worker 回执与终态工件自身，
> **没有核终态依赖的 checks/gates 证据引用**（缺一个必需命令检查的输出流或工件仍放行，而同一缺失
> 交给 `Store.verify_evidence()` 会拒绝）；Windows 测试**能编译但不能执行**（驱动无 `.exe` 后缀、
> 平台中性测试用 Unix 夹具、owner 句柄泄漏）。两项均独立复现后在 `187d1e4` 修复：探针拆为命名脚本
> `scripts/go-engine/native_evidence_probe.py`（评审的 P3 整理意见一并落实），走 `seal()` 同款链——
> worker 回执之外**逐个核终态引用的 check/gate 证据**并与规格必需 id 对齐；九个 POSIX 合同测试全部
> 移入 `supervise_unix_test.go`，三个平台中性测试改用驱动自身 `--version` 作零退出子进程并关闭
> owner 句柄，`supervisorBinary` 按平台命名。负控制：原位移走单个 check 工件，完整探针精确拒绝
> （FileNotFoundError 点名该工件），去掉 check 遍历的降级探针完全漏过；新控制项 `ADM-E17` 原位
> 锚定（relocated 副本会在绝对路径绑定处提前失败、使控制空洞化，故必须原位做）。运行时身份不变
> （`017ea1db`），第五次授权原生运行原样重绑，本轮零模型调用。

> **第三次重签缘起**：第二轮评审（`g2a-repair-review-20260918T091325Z/`）确认上一轮三项修复关闭、
> M3 容量修复保持通过，但驳回两项 P1——原生准入仍未核**原始执行证据**（报告与清单原样、
> 154 份作业原始文件＋16 份工件全删的副本仍被放行）；监督器测试包在 **Windows 目标不编译**
> （`Setpgid/Getpgid/Kill` 无平台边界，CI 含 Windows `go test ./...`；M2 基线仍可编译）。两项
> 均独立复现后在 `fa44747` 修复：准入经候选 venv 走 harness 自有证据链（`open_run` 身份/规格/
> 需求摘要 → 逐 worker 回执 `Store.verify_evidence`（工件完整性、比较输入、作业回执与流证明、
> 原生 source 四件）→ 终态工件回读），控制项 `ADM-E16` 以剥离副本锚定；两个 POSIX 专用取消测试
> 移入 `supervise_unix_test.go`（`//go:build !windows`），其余平台断言原样，windows/linux vet 通过。
> 运行时身份不变（`017ea1db`），第五次授权原生运行仍绑定本候选，本轮未消耗新的模型调用。

## 结论

替换单元 G2a **收口，`PASS`**。本记录是**第三次**整条重签（前两次分别被第一、二轮评审驳回，处置见上引言与下表）；取证：

- **命令作业半**（Go 监督器的直接 argv 检查路径）：验收尝试
  `.harness/validation/acceptance/G2a/20260918T215040Z-78741` 九项全过（`overall=PASS`、`failed=[]`）。
- **承载原生传输半**：经授权的真实原生运行（provider `zcode`）**`HARNESS_COMPLETE`、7/7 角色、
  0 中断、556.1 秒**，七个作业全部在 Go 监督器下执行（每作业 `process.json.pid == native pgid`、
  `exit.json` 全 0），运行清单绑定本候选的运行时身份（`017ea1db…`）——§13.1 第 513 行的
  "完成关联"证据就此落账。

**为什么是整条重签**：旧的命令作业半签在候选 `63b0650` 上，该候选早于 M3 的 P1 修复
`f136303`、自带该缺陷，且与 M3 现在的 `PASS` 不共享候选树——按索引第 27 条，旧半不得复用，
重签必须两半都要。本记录即按该处置执行。

**为什么 provider 是 zcode**：合同对原生半的要求是"验证监督器承载**固定的原生传输**；
不同时更换宿主协议"，**未指定宿主**。DSH 路径两次受阻于环境问题（`sandbox-exec` 在继承自
代理会话的上下文里无法 apply，裸探针 exit 71），迁出继承上下文后也曾 7/7 通过（launchd
上下文、逐字节副本）；zcode 路径在**普通代理会话上下文**直接 7/7 通过。两条宿主路径互相
印证"监督器承载原生传输"这一事实本身。

**候选中 zcode 适配器变更的披露**（`d7a687c` source 收据落盘、`2d36375` 运营方权限模式声明、
`91255c6` 评审返修：模式证据事实化、有界请求截止、干净树测试构建、准入原生关口）：均为本单元
派发之后、按用户明确授权落地的**公共前置修复**，沿 `6757231`/`8a06fc1`/`4e3fa0e`/`d2f4847` 的
既有先例；监督器本体、`native-request.json` 形状、交互投递与 `verify_source` 未动。权限契约
（授权依据与用户 2026-09-18 的逐字确认——"无头的这种没有办法让用户选择的可yolo，其余的先不吧"——
均见 `native_verification.authorized`）：**新建** dev/返修会话（无头、无用户应答通路）在首个 turn 前经
`session/setMode` 一次性声明运营方无人值守上下文的完全自动模式并回读确认（app-server 把新会话
硬默认为 `build` 且无宿主设置可改、无头客户端无法应答审批）；恢复会话保持既有模式并**如实记录
宿主报告的模式**（绝不为对齐常量而切换）；绝不在权限询问/超时/失败后改模式；只读会话与探针
从不设模式。

| 检查 | 结果 |
| --- | --- |
| identity / go-vet / go-test-full（无缓存全量） | PASS |
| sqlite-probe（CGO 开/关各 8/8） | PASS |
| python-unittest（全套 229 项） | PASS |
| demo-fixture（`DEMO_COMPLETE`） | PASS |
| doctor-dsh / doctor-zcode（0 次模型调用） | PASS |
| tree-clean-after | PASS |
| 原生定向验收（已授权、已执行） | **`PASS`**：`HARNESS_COMPLETE`、7/7 角色、0 中断 |
| **总体** | **`PASS`** |

上一验收基线：`M3`（记录候选 `aa3312e`）。本候选 `91255c6` 含 `f136303`，血缘已按索引第 27 条核对。

## 独立评审轮（2026-09-18）与处置

评审证据：`.harness/validation/m3-g2a-independent-20260918T064153Z/`（`review.json`、
`code-review.json`、`findings.json`）。四项发现的独立复现与处置（每项的回归/控制项在
"撤掉修复即失败"意义上验证过）：

| 发现 | 级别 | 独立复现 | 修复与防线 |
| --- | --- | --- | --- |
| 准入不检查记录声明为必需的原生验收：篡改成绩为 FAIL 或证据路径缺失仍 `ADMISSION GRANTED`（exit 0） | P1 | 两种篡改副本实测均放行 | `admission.py` 新增原生校验：重读 `native_verification.report` 的业务结果（passed/provider/roles）、要求 run 清单绑定候选运行时身份；新控制项 `ADM-E12..E15`（缺失/FAIL/证据丢失/报告与记录矛盾） |
| 干净源码上监督器测试被构建目录阻断：`supervise_test.go` 的临时构建未设工作目录，`.harness/bin` 缺失（CI 环境）时找 `internal/harness/cmd/...` 必败 | P1 | 干净 worktree + CI 式二进制准备实测退出 1 | 构建命令设 `Dir = root`；CI 式干净树复检通过 |
| 恢复会话未切模式但证据写 `yolo`：`tool-policy.json`/`native-source.json` 记录常量而非实际模式 | P2 | 代码路径确认（resume 不调 setMode 而落盘常量） | 落盘"实际生效模式"：新会话记宿主 `settings` 回读、恢复会话记快照报告值；继承测试内嵌"落盘模式＝宿主回读"断言（负控制：撤修复 8 项失败） |
| 共享传输抽取后有界请求不再因总预算耗尽而终止：通知持续到达时晚到响应仍被接受 | P2 | 受控投递复现（0.05s 预算下旧实现拒绝、新实现处理 30 条通知后接受迟到响应） | `request()` 循环顶部显式截止检查；洪水服务器回归用例（负控制：撤修复该用例失败、跑满 1s）；DSH 的 `UNBOUNDED`/逐消息语义不变 |

评审轮的附带收获：严格回读在真实宿主上暴露 `session/setMode` 应答中 `projection` 块可以是
**陈旧快照**（实测 `mode: "build"`、`sessionId: "unknown"`），权威确认在 `settings`——第一次返修
候选的原生运行因此如实停在 `ZCODE_CONFIG: permission mode was not applied`，修正回读字段后
7/7 通过（运行史见 `native_verification.history`）。假服务器夹具同步改为镜像真实形状。

## 原生运行证据（zcode，2026-09-18，本记录绑定的运行）

- 运行：`.harness/runs/harness-20260918-161308-6e657186`，其清单绑定本候选运行时身份 `017ea1db…`，
  模型 `account:bigmodel-individual-coding-plan / GLM-5.3`，556.1 秒、0 中断。
- 探针与报告：`.harness/validation/zcode-native-fifth-20260918T081304/zcode-flow/report.json`
  （`passed: true`、`status: HARNESS_COMPLETE`、7/7 角色 `PASS`）——准入的 `ADM-E12..E15` 之后的
  正式关口会**重读这份报告并核对清单身份**，手填字段不再自证。
- **完成关联**：七个作业全部经 `store.job → harness supervise` 执行；每个作业 `process.json`
  的 pid **等于**原生子进程的 pgid（监督器是进程组组长，`Store.cancel_jobs` 组取消的前提，
  旧评审问题 3 所要求的显式取证在此落成），每份 `exit.json` 收据 `exit_code=0` 且流证明齐备。
- **只读边界**：只读角色的原生事件中只有 Read/Glob/Grep 调用；带副作用的工具只出现在
  development 作业中，其 `tool-policy.json` 记录的 `permission_mode: yolo` 来自宿主 `settings`
  回读而非常量。
- 同日先行运行史（完整列表见 `native_verification.history`）：第一轮暴露 source 收据不落盘
  （`d7a687c`）；第二轮停在 Edit 审批边界，促成模式声明修订（`2d36375`）；第三轮 7/7 但记录被
  评审驳回（缺陷在原生运行之外）；第四轮如实停在严格回读（发现 `projection` 陈旧快照问题）；
  第五轮即本记录绑定 的运行。

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

## 候选身份（2026-09-18 第二次重签）

```text
工作树   /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
分支     codex/go-engine-v1
提交     187d1e41c0（三轮评审返修候选；91255c6/2d363757 的记录已随评审轮作废）
上一基线 M3（记录候选 aa3312e）
运行时身份 017ea1db64a3ab0b（见 identity.json 与原生 run 清单一致）
证据清单 61 个文件
通过尝试 20260918T215040Z-78741
```

驱动二进制由验收器自行从候选源码构建并计入 identity 项。运行时身份较第一次重签（`8b204721…`）改变属预期：评审返修改动了传输与适配器文件。

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

# 准入：**预期放行**——本记录 overall=PASS 且两半证据齐备；
# 原生关口会重读 zcode-flow/report.json 并核对 run 清单绑定的运行时身份
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-G2a.json --next-unit G2b

# 新增的原生准入控制项（ADM-E12..E15）
python3 scripts/go-engine/admission-controls.py --record .gates/results/go-engine-G2a.json --out .harness/tmp/adm-controls-g2a

# 原生半证据（只读）
cat .harness/validation/zcode-native-fifth-20260918T081304/zcode-flow/report.json | python3 -m json.tool | head -30
R=.harness/runs/harness-20260918-161308-6e657186
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
