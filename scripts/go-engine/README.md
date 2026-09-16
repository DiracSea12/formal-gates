# Go 引擎迁移的验收与准入工具

本目录只服务「独立 Go 引擎」这条开发线的分块替换，不参与产品 run 的业务状态，也不是通用工作流平台。方案第 12.5 节要求：每个替换单元留一份机器可读的验收记录，派发下一单元前读取并校验该记录，记录缺失、结果未完成、原始证据缺失、必需项非通过或候选身份改变时返回非零并拒绝推进。

## 入口

```sh
# 完整验收（唯一可以签发记录的入口）
python3 scripts/go-engine/acceptance.py --unit B0 \
  --record-out .gates/results/go-engine-B0.json

# 准入检查（派发下一单元前必须执行）
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-B0.json --next-unit C1

# 两个工具自身的控制项
sh scripts/go-engine/negative-controls.sh "" .gates/results/go-engine-B0.json
python3 scripts/go-engine/admission-controls.py --record .gates/results/go-engine-B0.json --out DIR
```

`acceptance.py --only <项>` 是诊断模式，只跑指定项，**不允许签发记录**。它用于复现故障分类，不用于放行。

## 运行器的约定

- **每次调用使用全新尝试目录**：`.harness/validation/acceptance/<unit>/<UTC时间戳>-<pid>`。目录已存在时直接拒绝（退出码 2），从不覆盖。
- **逐项独立留存**：每个必需项的命令原文、stdout、stderr、退出码分别写在自己的 `steps/<项>/` 下。
- **逐项断言业务结果**，不以退出码代替结论。例如 fixture 演示必须真正达到 `DEMO_COMPLETE` 且 `evidence_level=fixture`，每个检查 `PASS` 且退出 0，每个门审 `PASS`；停在 `WAITING_USER` 一律判失败。
- **聚合结论**：`summary.json` 的 `overall` 只有在全部必需项 PASS 且项数与预期一致时才为 `PASS`。
- **证据清单**：每次尝试写 `evidence.json`，为 `steps/**`、`summary.json`、`identity.json`、`run.log` 逐文件记录 sha256。
- **不发送模型请求**。宿主部分只做 `doctor` 接入探测，并要求 `model_calls == 0`。

必需项：`identity`、`go-vet`、`go-test-full`、`sqlite-probe`、`python-unittest`、`demo-fixture`、`doctor-dsh`、`doctor-zcode`、`tree-clean-after`。

## 回归按"结果与进度"判定，不按机器速度

固定墙钟超时会把这个门槛变成"机器有多快"的量具，而不是"代码是否正确"的量具。实测依据：

- `internal/validate` 有 **785 个串行顶层用例**，最慢单例 5.76 秒，没有长尾、没有死锁；本机无缓存耗时 378–404 秒。
- 同一候选在较慢的独立环境上，该包耗时 **900.6 秒**并触发超时；同一次运行里每个包都慢 1.6–3.2 倍，说明差异来自环境吞吐而非某个用例。
- 受控负载实验：8/10 核满载时同一组用例 34.66s → 46.45s（+34%）。负载确有影响，但不足以解释 2.4 倍。

因此：

| 参数 | 值 | 含义 |
| --- | --- | --- |
| `go test -timeout` | `60m` | **仅作后备**。Go 自己的包级超时，不是判定依据 |
| 停滞预算 | `240s` | 事件流超过该时长无任何输出即判为停滞（等于最慢单例的 40 倍）。卡死的用例无论机器快慢都不产生事件 |

判定条件：退出码 0、无失败包、无未说明跳过、无停滞。包耗时作为**观测值**记录，用于让环境变慢可见，不作为阈值。

停滞监控由**读线程 + 有截止时间的队列等待**实现。这一点不能省：如果直接在主循环里做阻塞式 `readline()`，子进程不输出且管道未关闭时读取会一直等待，计时检查**根本走不到**；等它恢复输出又会先重置进度时钟，把整段静默漏掉。进度以"收到任意字节"为准而不是"收到完整行"，因此只输出半行再卡住同样能被发现。

`--stall-budget <秒>` 可覆盖生产预算，供控制项用秒级等待验证同一套代码；诊断模式（`--only`）不能签发记录。

`-count=1` 强制无缓存。部分通过数量、缓存成绩或旧记录都不算完整回归通过。

`TestNormalizeHostPath` 是 Windows 专用，在 darwin 上带明确理由跳过；其它任何跳过都判失败。

## 控制项

| 控制 | 覆盖 |
| --- | --- |
| NC-A | 必需项真实失败 → 非零 |
| NC-B | 夹具流程停在 `WAITING_USER` → 非零 |
| NC-C | 两次尝试互不覆盖 |
| PC-D | 正常 `DEMO_COMPLETE` → 零 |
| RS-1 | 完全静默超过预算 → 判停滞并杀掉子进程 |
| RS-2 | 输出半行（无换行）后静默 → 同样判停滞 |
| RS-2b | 稳定输出 → 不误判停滞 |
| RS-3 | 非零退出码被如实记录 |
| RS-5 | 真实运行器中一次停滞必须让该项 FAIL、整体非零 |
| RG-1 | 覆盖**已跟踪**的记录（` M <path>`）→ 接受 |
| RG-2 | 新写未跟踪的记录（`?? <path>`）→ 接受 |
| RG-3 | 存在无关脏文件 → 拒绝签发 |
| ADM-PC / E1–E11 | 准入放行与各类拒绝理由（见 `admission-controls.py`） |
| ADM-PC-smoke | 冒烟终态判定 |

Git 状态行按"状态码 + 路径"整行比较，**不去掉前导空格**：已跟踪文件被修改时报 ` M <path>`，修剪前导空格会把它误判成"候选已移动"。

## 记录的签发

记录只在运行 PASS 时签发，且要求签发后工作树增量恰好是记录本身。记录包含候选提交、分支、运行时身份、驱动二进制摘要、各必需项结论、解释器与已安装依赖指纹，以及尝试目录 `summary.json`/`identity.json`/`evidence.json` 的摘要见证。

记录是「验收结论」，与被验收的候选分开提交：准入检查器容忍 `HEAD` 比记录中的候选多出**恰好记账清单里那些文件**的差异（记录只能隐式声明规范位置 `.gates/results/` 下的自身），任何其它差异都判候选已移动并拒绝。

## 准入检查器核验的内容

1. 记录存在、JSON 可解析、结论为 `PASS`，且 `summary.json` 摘要与记录一致。
2. 尝试跑满了全部预期必需项，逐项为 `PASS`。
3. **原始证据完整**：按 `evidence.json` 逐文件重算摘要，拒绝缺失、被改写或多余的文件；并沿 `items[].commands` 核对引用的 stdout/stderr/exit 文件存在，退出码证据与摘要一致。
4. 记录与其尝试在 `commit`/`branch`/`runtime_identity`/`driver_binary_sha256` 上一致。
5. 候选工作树干净、`HEAD` 继承记录的候选提交、增量仅为已声明记账文件、分支一致。
6. 源码、依赖锁、审查政策、驱动二进制摘要与记录一致。
7. **运行依赖真的装好了**：解释器身份未变，`requirements.lock` 中 41 项发行版的**已安装版本**逐项一致（锁文件摘要不能证明这一点）。
8. **基线能通过公开入口启动**：`scripts/harness demo --approve-sample` 必须真正到达 `DEMO_COMPLETE`，检查与门审全 `PASS`。`--help`、导入包或 `doctor` 都不能替代。

## 目录内容

| 路径 | 用途 |
| --- | --- |
| `acceptance.py` | 验收运行器 |
| `admission.py` | 准入检查器 |
| `runtime_check.py` | 已安装依赖与解释器核对（两个工具共用） |
| `negative-controls.sh` | 运行器控制项 |
| `admission-controls.py` | 准入控制项（含未填充运行时的克隆环境） |
| `dependency-lock.json` | G0 的 Go/SQLite 驱动与依赖锁定记录 |
| `sqlprobe/` | SQLite 驱动兼容探针（Go 包，可 `go test`，可独立运行输出 JSON） |

## 本地证据

原始日志与尝试目录位于 `.harness/validation/` 下，不进入版本管理。受版本管理的只有记录本身（`.gates/results/`）、工具与锁定记录。
