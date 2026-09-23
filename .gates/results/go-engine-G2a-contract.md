# G2a 替换单元合同：命令作业（Go 监督器的直接 argv 检查路径）

基线为 M3（候选 `d71113e`，记录提交 `603f019`，准入已放行 G2a）。G1b 已收口；G2a 是 G2 阶段第一项。

合同依据：

- §13.1 G2a 行：职责为"先交付 Go 监督器的直接 argv 检查路径，再验证它承载固定的原生传输；**不同时更换宿主协议**"；验收出口为"**已有检查输入和退出码不变；主控退出后同一 job 可接回；取消不影响其他进程**"。
- §8.1："事务登记调用意图与输入绑定 → **独立监督器取得该调用的执行所有权** → 启动或附着原生调用，尽早记录身份 → 持续保存公开进度、交互及输出 → **保存真实结束状态和输入输出证明** → runner 核验并接纳。"以及"**同一个意图不得由两个监督器同时执行**。"
- §9.1："检查使用用户确认的**真实 argv**，在一次性候选克隆中以当前用户权限执行…**保留作业监督、stdout/stderr、真实退出码**、执行后 HEAD 与已跟踪文件核对。"
- §12.1："Go 组件通过已存在的桥接方式或**同一二进制的窄内部命令**接入，不新增 RPC 服务、任意跨语言对象序列化或插件基础设施。"

## 与 C1–C4/M1–M3 的边界原则不同之处（写给评审）

前七个单元的边界是"**Go 持有规则与格式，Python 持有进程与文件系统**"。G2a 换的是**另一种东西**：进程的执行所有权。本单元把"谁拥有这次执行"从 Python 侧的一次性脚本（`formal_gates_harness.job`）换成 Go 二进制的窄内部命令。文件系统写入仍在 Python 侧的同一条路径上（`atomic`/`log` 文件的语义由监督器自己实现，但**工件、事件、SQLite 仍不归 Go**）。

因此本单元**不新增桥接操作**：监督器是一个长驻进程，不是请求/应答。它走 §12.1 的第二种接入方式。

## 职责边界

**做**：把 `job.py::main`（一次性监督器）的执行所有权搬进 Go。

| 归属 | 内容 |
| --- | --- |
| **Go（新）** | `harness supervise <directory>` 窄内部命令：`owner` 独占取得、`process.json` 身份、子进程启动（argv/cwd/stdin/env）、stdout/stderr 落盘与 fsync、SIGTERM 取消语义、`exit.json` 收据（含 `request_digest` 与两个流证明） |
| **Python（不变）** | `Store.job` 的**全部**逻辑：job 目录布局、`request.json` 写入与复用判定（`job id reused with different input`）、`start_new_session=True` 的派生、轮询与 `UNKNOWN_JOB`/`UNKNOWN_START`、收据/输入摘要比对（`job receipt/input mismatch`）、`verify_process` 委派、返回记录的组装；以及 `Store.cancel_jobs` 的进程组判定 |

**唯一改动**：`Store.job` 里派生监督器的那一行 —— `[sys.executable, "-m", "formal_gates_harness.job", dir]` 改为 `[self.bridge.binary, "harness", "supervise", dir]`。其余参数（`stdin=DEVNULL`、`stdout=DEVNULL`、`stderr=wrapper.stderr`、`start_new_session=True`、`close_fds=True`）**逐字不动**。

`self.bridge.binary` 是 `Bridge` 已经解析好的路径，因此不引入第二套二进制查找规则。

**明确不在本单元范围**：

1. **宿主协议不变**。`dsh` / `zcode` 的适配器、`native-request.json` 的形状、交互投递、`verify_source` 全部不动。§13.1 明说"不同时更换宿主协议"。
2. **`Store.job` 的其余逻辑不动**。特别是轮询循环里的两个 `RuntimeBlocked`（`UNKNOWN_JOB` / `UNKNOWN_START`）与"不盲目重放"的语义。
3. **`cancel_jobs` 的进程组判定不动**（`getpgid(pid) == pid` 才发 SIGTERM）。监督器只需继续保持"被派生时是组长"这一事实（由 Python 侧 `start_new_session=True` 保证）。
4. **工件、事件、SQLite、retention 不动**。
5. **不新增桥接操作**。

## 接口

```sh
formal-gates harness supervise <job-directory>
```

- 退出码 **0**：写出了 `exit.json`（无论子进程成败）。
- 退出码 **2**：`owner` 已存在，**未取得执行所有权**，且**不写** `exit.json`、**不写** `process.json`。
- 其他非零：监督器自身失败（`request.json` 缺失/畸形）。此时同样不写收据。

监督器**不向自己的 stdout 写任何字节**（迁移前亦然；`Store.job` 把 stdout 接到 `DEVNULL`）。自身诊断信息写 stderr（接到 `wrapper.stderr`，**全仓库无人读取它**，故其文本属有界差异，见下）。

## 迁移前语义（**已实测**，不是推断）

以下全部由 `/tmp/g2a-probe/` 下的探针在迁移前实现上实测得出，逐条要求 Go 复现。

### 子进程退出码

| 情形 | `exit_code` |
| --- | --- |
| 正常退出 N | `N`（实测 7 → 7） |
| 子进程显式 `exit(130)` | `130`（**与取消码同值，不得因此把两者混为一谈**） |
| 被子进程信号终止（SIGKILL） | **`-9`**（CPython 的 `Popen.returncode` 对信号死亡给 `-signum`） |
| 启动失败（见下表） | **`127`**（`code` 初值），同时 `error` 非空 |

Go 的 `ProcessState.ExitCode()` 对信号死亡返回 `-1`，**不能直接使用**；必须取 `syscall.WaitStatus`：`Exited()` → `ExitStatus()`，`Signaled()` → `-int(Signal())`。

### 启动失败的 `error` 文本（实测逐字）

`error` 的形状是 `f"{type(exc).__name__}: {exc}"`。可达与不可达分开列：

| 情形 | 实测 `error` | 检查路径可达性 |
| --- | --- | --- |
| `argv[0]` 不存在 | `FileNotFoundError: [Errno 2] No such file or directory: '<argv0>'` | **可达**（用户配置了未安装的工具） |
| `argv[0]` 存在但不可执行 | `PermissionError: [Errno 13] Permission denied: '<argv0>'` | **可达** |
| `argv[0]` 是目录 | `PermissionError: [Errno 13] Permission denied: '<argv0>'` | 可达（macOS 上 `execve` 对目录返回 EACCES，**不是** `IsADirectoryError`） |
| `argv[0]` 是带执行位的文本文件 | `OSError: [Errno 8] Exec format error: '<argv0>'` | 可达 |
| `cwd` 不存在 | `FileNotFoundError: [Errno 2] No such file or directory: '<cwd>'` | 不可达（`clone` 已建目录），仍须复现 |
| `cwd` 是文件 | `NotADirectoryError: [Errno 20] Not a directory: '<cwd>'` | 不可达，仍须复现 |
| `argv` 为空 | `IndexError: list index out of range` | **不可达**（`ValidateSpec` 要求 argv 非空） |
| `argv` 含 NUL | `ValueError: embedded null byte` | **不可达**（`ValidateSpec` 拒绝 NUL） |
| `environment` 的值非字符串 | `TypeError: expected str, bytes or os.PathLike object, not int` | **不可达**（`graph.py` 构造的 environment 全是字符串值） |
| `environment` 的键含 `=` | `ValueError: illegal environment variable name` | **不可达**（键是固定的四个） |

**strerror 的大小写是陷阱**：Python 的 `[Errno 2] No such file or directory` 首字母大写（来自 `os.strerror`），而 Go 的 `syscall.ENOENT.Error()` 是小写 `no such file or directory`。**必须**用显式映射表复现，不能直接拼接 Go 的 errno 文本。实测对照：errno 2 `No such file or directory`、8 `Exec format error`、13 `Permission denied`、20 `Not a directory`、21 `Is a directory`、26 `Text file busy`。

可达的四条**必须逐字复现**；不可达的五条也按实测文本复现，并在实现处注明不可达论证（它们是下一次"类型边界静默收窄"的候选）。

### `PATH` 在**合并后的环境**里解析（本单元最高风险点）

实测结论：

| 探针 | 结果 |
| --- | --- |
| 裸 `argv[0]`，**只**存在于请求 `environment` 覆盖后的 `PATH` 里 | **成功执行**（退出码 3） |
| 裸 `argv[0]`，请求 `environment` 里**删掉** `PATH` | 仍能执行 `ls`（成功，退出码 0） |

即：Python 的 `execvp` 在**子进程内、设置新环境之后**做 PATH 搜索，因此用的是**请求合并后的 `PATH`**；`PATH` 缺失时回落到 `os.defpath` = **`/bin:/usr/bin`**。

Go 的 `exec.Command("bare")` 会用 `LookPath`，而 `LookPath` 读的是**父进程**的 `os.Getenv("PATH")`。**这是真实分歧**：`graph.py` 给检查作业传的 `environment` 不含 `PATH`，但 `test_comparison_inputs.py:94` 会 `patch.dict(os.environ, {'PATH': ...})` 改父进程环境；一旦将来有人让请求环境覆盖 `PATH`，两侧就会不一致。

**要求**：Go 监督器必须对不含 `/` 的 `argv[0]` 在**合并后的环境**的 `PATH` 上搜索，`PATH` 缺失时用 `/bin:/usr/bin`；找到但要执行失败时，错误按上表归类。**必须**有专门用例钉住（"只在请求 PATH 里找得到的裸 argv0"），否则这个分歧不会有人发现。

### `owner` 与身份

| 规则 | 内容 |
| --- | --- |
| `owner` | `O_CREAT\|O_EXCL\|O_WRONLY`，模式 `0o600`。已存在 → 直接**退出 2**，不写任何收据 |
| `owner` 生命周期 | 永不删除。一个 job 目录**只能被监督一次** |
| `process.json` | 原子写入 `{"pid": <监督器自身 pid>, "started_at": <float 秒>}`，**在子进程启动之前** |
| 为什么要提前写 | `Store.job` 的存活探测依赖它在启动后 10 秒内出现（否则 `UNKNOWN_START`） |

### 流与收据

| 规则 | 内容 |
| --- | --- |
| 流文件 | `stdout.log` / `stderr.log`，以 **`wb`（截断）** 打开，直接承接子进程输出 |
| 同步 | 子进程结束后 `flush` + `fsync` 两个流文件 |
| 证明时机 | 证明在流文件**关闭之后**计算（读到的是最终字节）；形状为 `{"sha256": "<64 hex>", "size": <int>}`，复用 C2 的 `internal/harness.FileProof` |
| `exit.json` | 原子写入，字段**恰好**六个：`exit_code`、`error`、`started_at`、`finished_at`、`request_digest`、`streams` |
| `streams` | 恰好两个键 `stdout.log`、`stderr.log` |
| `request_digest` | 请求的规范摘要，复用 C3 的 `CanonicalDigest`（Python 侧 `digest(request)` 的结果） |
| `started_at` | **在 `process.json` 写入之后**取值（故 `exit.json.started_at >= process.json.started_at`） |
| `error` 初值 | 空串；启动失败时保持 `127` 并填入错误文本；正常结束时为空串 |

### 取消（SIGTERM）

| 规则 | 内容 |
| --- | --- |
| 触发 | 收到 `SIGTERM`（由 `Store.cancel_jobs` 对**进程组**发出，监督器因 `start_new_session` 是组长） |
| 处理 | 向子进程发 `terminate`（SIGTERM）；**只**发给直接子进程，不向自己所在的组广播 |
| `exit.json` | `exit_code = 130`、`error = "owned job was cancelled"`（**逐字**），随后仍然写出完整收据 |
| 结算 | 最多等子进程 5 秒；超时则 kill 并 wait |
| 退出码 | 监督器自身仍返回 **0** |
| 与子进程退出码的关系 | 无论子进程是被信号杀死还是自行退出，取消路径**一律**写 `130`。因此"子进程显式 `exit(130)`"与"被取消"在收据上同值而**成因不同**——不得据此合并两条路径 |

## 不变量

1. **检查输入不变**：授权 argv、`cwd`、`stdin`、`environment` 逐字传给子进程；`test_comparison_inputs.py:94` 的 `job.call_args.args[1]['argv'] == check['argv']` 继续成立。
2. **退出码不变**：上表全部成立，含信号死亡的 `-N`、启动失败的 `127`、取消的 `130`。
3. **收据形状不变**：六个字段、两个流、两个证明键，一个不多一个不少（`verify-process` 的字段比对继续通过）。
4. **开工前先留身份**：`process.json` 在子进程启动前写入，`Store.job` 的 `UNKNOWN_START` 与存活探测语义不变。
5. **一个意图只有一个监督器**：`owner` 的 `O_EXCL` 语义不变；并发第二次调用退出 2 且不产生收据。
6. **主控退出后同一 job 可接回**：监督器由 Python 侧 `start_new_session=True` 派生，不随控制器死亡；`RecoveryTests.test_controller_exit_does_not_repeat_completed_side_effect` 必须继续通过（副作用恰好一次）。
7. **取消不影响其他进程**：只动自己那个子进程；`cancel_jobs` 的 `getpgid(pid) == pid` 筛选不变，非组长进程不被发信号。
8. **监督器 stdout 静默**：不向自己的 stdout 写字节。
9. **不新增桥接操作**、不改宿主协议、不改 `Store.job` 的其余逻辑。
10. **`job.py` 从生产路径退休**：其内容逐字移入仅测试使用的 `tests/reference_job.py`，生产代码不得导入它（沿用 C3/M3 的 `reference_*` 约定）。
11. **往返次数不变**：本单元不新增跨进程桥接调用；监督器是独立进程而非桥接调用。

## 必需用例

差分对照须以 `tests/reference_job.py`（迁移前 `main` 逐字）为参照，两侧各用**独立 job 目录**，比对前把各自根替换为 `<root>`；`pid`、`started_at`、`finished_at` 为**非确定字段**，只断言存在且类型正确（浮点/整数），不比较值。

| 用例 | 判据 |
| --- | --- |
| 退出 0 | 两侧 `exit_code=0`、`error=""`、流证明一致 |
| 退出 7 | 两侧 `exit_code=7` |
| 显式 `exit(130)` | 两侧 `exit_code=130`，`error=""`（**不被当成取消**） |
| 子进程 SIGKILL | 两侧 `exit_code=-9` |
| stdout/stderr 字节 | 两侧 `stdout.log`/`stderr.log` 逐字节相同（含多字节 UTF-8、无尾换行、>64KiB 大输出） |
| stdin 传递 | 子进程收到的字节与请求 `stdin` 逐字相同（含多字节；`stdin` 缺省为 `""`） |
| `environment` 合并 | 覆盖已有变量、新增变量、`environment` 缺省为 `{}` 三种情形，子进程读到的值与迁移前一致 |
| `cwd` 生效 | 子进程打印自身 `cwd`，与请求一致 |
| `argv[0]` 不存在 | 两侧 `exit_code=127`、`error` 逐字相同（`FileNotFoundError: [Errno 2] …`） |
| `argv[0]` 不可执行 | 两侧 `exit_code=127`、`PermissionError: [Errno 13] …` 逐字相同 |
| `argv[0]` 是目录 | 两侧 `PermissionError: [Errno 13] …` 逐字相同（**不得**变成 `IsADirectoryError`） |
| `argv[0]` 是带执行位的文本 | 两侧 `OSError: [Errno 8] Exec format error: …` 逐字相同 |
| `cwd` 不存在 | 两侧 `FileNotFoundError: [Errno 2] …` 逐字相同 |
| `cwd` 是文件 | 两侧 `NotADirectoryError: [Errno 20] …` 逐字相同 |
| **裸 `argv0` 只在请求 PATH 里** | 两侧都**执行成功**（钉住 PATH 解析侧） |
| 裸 `argv0` 且请求环境无 `PATH` | 两侧都成功（钉住 `/bin:/usr/bin` 回落） |
| `owner` 已存在 | 两侧监督器退出 **2**、**无** `exit.json`、**无** `process.json`，且不执行子进程 |
| `exit.json` 字段集合 | 恰好六个；`streams` 恰好两个键 |
| `request_digest` | 两侧等于 `digest(request)` |
| 取消（SIGTERM 到监督器组） | 两侧 `exit_code=130`、`error="owned job was cancelled"` 逐字相同；子进程已停止 |
| 取消后收据仍完整 | 取消路径同样写出六字段收据与两个流证明 |
| **原生传输 argv 形态** | 以**逐字**的原生入口 argv（`[sys.executable, "-m", "formal_gates_harness.native_entry", <dir>]`）作请求，在 Go 监督器下产出与参考实现同形的收据（该子进程按自身逻辑失败退出 1、`native-error.json` 落盘、`error` 为空串） |

**结构性证据（原生传输那半）**：监督器**不读** `request.json` 的 `kind` 字段，因此原理上无法区分检查与原生。须以一条 Go 用例断言"同一 request 只改 `kind` 不改变行为"，并在记录中写明：本次**未**做真实原生定向验收（需显式授权费用，关口在 G2b 之前），故"承载原生传输"的结论只由"argv 形态逐字跑通 + 监督器不读 kind"两点支撑。

## 受影响调用方

- `infra.py::Store.job`：仅派生监督器的那一行
- `infra.py::Store.cancel_jobs`：**不改**（只作回归）
- `graph.py::one_check`（`graph.py:180`）、`workers.py:120`：`Store.job` 的调用方，行为须不变
- `handover.py`、`retention.py`、`cli.py:137`：读 `exit.json`/`process.json`，须不变
- `tests/test_driver.py::RecoveryTests`：G2a 出口条件的直接证据，须原样通过
- `tests/test_driver.py::DriverTests::test_unknown_job_is_not_relaunched`、`tests/test_boundary_recovery.py`、`tests/test_nonhappy_regressions.py`、`tests/test_comparison_inputs.py`：须原样通过

## 交付物

- `internal/harness/supervise.go`：`Supervise(directory string) int`（窄内部命令的**执行**入口）
- `internal/harness/supervise_test.go`：含上表全部 Go 侧用例与"不读 kind"的结构性用例
- `internal/harness/command.go`：新增 `supervise` 分支（在 `bridge` 分支旁，**不**转发给 Python）
- `runtime/langgraph/formal_gates_harness/infra.py`：`Store.job` 换掉派生 argv
- `runtime/langgraph/formal_gates_harness/job.py`：**删除**（内容移入 `tests/reference_job.py` 逐字保留）
- `runtime/langgraph/tests/reference_job.py`、`runtime/langgraph/tests/test_job_supervision_differential.py`
- 同步 `runtime/langgraph/formal_gates_harness/__init__.py`（若有 `job` 的导出）

**参考实现的调用约定**：`tests/reference_job.py` 保存迁移前 `job.py::main` 的**逐字**实现，并保留 `if __name__ == "__main__": raise SystemExit(main(Path(sys.argv[1])))` 形状，使差分测试能把两侧都当作**独立进程**派生。两侧都必须是独立进程（监督器的语义就是进程所有权，同进程内调用无法证明它）。参考实现依赖 `from formal_gates_harness.infra import atomic, digest, read, file_proof`，因此派生时须保证包可导入（沿用 `PYTHONPATH=<repo>/runtime/langgraph`）。`tests/` 不是包（无 `__init__.py`），测试之间以裸模块名互相导入，沿用即可。

**提交顺序的既有限制（过程，写给评审）**：`admission.py` 用 `git diff --name-only <记录候选>..HEAD` 判定"候选是否被移动"，因此记录候选之后的**任何**未申报文件都会使该记录的准入核验失败。这意味着合同文件**不能**作为独立提交先于候补提交落地，只能与该单元的候选提交同批。前七个单元（C1–C4、M1–M3）的合同均如此。**后果是"合同先于实施固定"这一点无法由提交顺序证明**，只能由合同内容与派发记录佐证。本次派发前已记录合同摘要，见 G2a 验收记录。

手工跑 Python 套件须同时设 `PYTHONPATH="$PWD/runtime/langgraph"`（覆盖宿主 `sitecustomize` 垫片，见 `HOST-SHIM-BREAKS-PATH-OPS`）与 `TMPDIR=/private/tmp`（见 `TMPDIR-UNRESOLVED-SYMLINK`）。

## 待评审的判断（不是遗漏）

1. **G2a 是否应拆成两个验收单元。** §13.1 把 G2a 列为**一行**、给一组出口条件；§13.1 的正文却说"G2a 先验命令监督，再验其承载原生传输"。本单元按**一个**验收记录处理，但要求两半都有证据。若评审读作"两次分别验收"，应在 G2a 记录之后补一次仅针对原生传输的验收。
2. **不可达的五条错误文本是否值得逐字复现。** 它们（空 argv、NUL、非字符串环境值、含 `=` 的键、以及 `cwd` 两条）在当前配置下不可达。选择复现而非省略，理由是它们是"类型边界静默收窄"的高发区；但复现本身也带来"为不可达情形写代码"的成本。若评审认为应改为结构性拒绝，需指认由哪一侧拒绝。
3. **`job.py` 删除还是保留。** 删除使 `runtime_identity` 变化（预期内），并保证不存在第二实现；保留则多一份可被误用的生产代码。本单元选择删除。
4. **`error` 文本的长期归属。** 它现在由 Go 复现 CPython 的异常文本（`FileNotFoundError: [Errno 2] …`）。这是"复现参考实现的观测行为"的又一例，但其来源是**语言运行时**而非本项目的合同。若评审认为不该长期由 Go 复现，应改为结构性拒绝或在迁移后重新定义该字段（那会改变对外观测，需要单独授权）。
5. **监督器自身的 stderr 文本未纳入比对**（`wrapper.stderr` 全仓库无人读取）。若评审认为它也该逐字一致，需要先说明谁在消费它。
