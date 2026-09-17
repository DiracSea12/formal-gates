# G2a 验收记录：命令作业（Go 监督器的直接 argv 路径）

## 结论

替换单元 G2a 通过，被固定为下一基线。**G2 的第一项完成**；下一单元为 G2b（原生定向验收关口），需显式授权费用。

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
| 原生定向验收 | **不适用**：本单元只搬进程所有权与 argv 路径，不承载真实原生模型流水线；授权关口仍在 G2b 之前 |
| **总体** | **`PASS`** |

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
| `HOST-SHIM-BREAKS-PATH-OPS`、`TMPDIR-UNRESOLVED-SYMLINK`、`COPY-STATUS-UNREACHABLE`、`MALFORMED-RECORD-ERROR-SHAPE`、`BOOL-SIZE-IS-A-NUMBER-IN-PYTHON`、`NON-FINITE-CROSSES-TRANSPORT`、`INPUT-PATH-NOT-NORMALISED`、`ARTIFACT-READ-COSTS-TWO-CALLS`、`ARTIFACT-LOCATION-STAYS-IN-PYTHON`、`INDEX-MISSING-M2-ROW`、`GOFMT-WINDOWS-FILE-PREEXISTING`、`WORKTREE-LOCATION-AMBIGUOUS` 等 | 保留，不变 |
| G2a | **已完成**：本记录。下一单元 G2b（原生定向验收关口），需显式授权费用 |

## 回退入口

```sh
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine \
  reset --hard d71113e5ada1f84c9f6f5d94b022e33dfba477e3
```

与 M1、M2、M3、C4 记录同一约定：指向上一基线的**候选**提交。

## 复核入口

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine

# 准入：核验本记录并放行 G2b
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-G2a.json --next-unit G2b

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
