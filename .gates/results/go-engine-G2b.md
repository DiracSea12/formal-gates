# G2b 验收记录（2026-09-19，原生关口已关闭重签）：DSH 薄适配（在已验收监督器下移植 DSH 请求、身份、结果和错误映射）

候选 `9d12645`（分支 `codex/go-engine-v1`）。上一验收基线：`G2a`（候选 `df33e9c`）。九项验收全过
（尝试 `.harness/validation/acceptance/G2b/20260919T075930Z-19871`，`overall=PASS`、`failed=[]`）。
**原生定向验收已执行并 PASS**（机主 2026-09-19 逐字授权「可以执行」；见下节与
`native_verification.authorized`）——G2b 按此收口。

## 原生定向验收（DSH，2026-09-19，授权运行）

- **完整原生小流程**：`native_multihost.py --provider dsh`，运行
  `/tmp/g2b-copy/.harness/runs/harness-20260919-161642-52992e09`，报告
  `/tmp/g2b-copy/.harness/validation/g2b-native-first-20260919T081635/dsh-flow/report.json`
  （`passed:true`、`HARNESS_COMPLETE`、**7/7 角色全 PASS**、`interrupts: []`、134.9 秒、模型
  `deepseek-official / deepseek-v4-flash`＋`high`，选型经宿主 `configOptions` 回读）。
- **完成关联**：七个 native 作业的 argv 全部是 Go 窄命令 `[<driver>,"harness","native",<dir>]`，
  `exit.json` 全 0、owner 锁齐备、每作业 `native-process.json` 的 **pgid ＝监督器 pid**（监督器是
  进程组组长）；证据探针从候选工作树重走 `open_run` 身份绑定＋7 份 worker 回执（含经
  `native-verify-source` 的原生四件证明）＋2 检查＋2 门审＋终态回读，exit 0。
- **只读约束**：只读角色的原生事件只有 `assistant/message`＋`turn/end`；mjs 工具策略拒绝 12 项
  写能力工具（`tool-policy.json read_only:true`）；唯一的 development 作业独立承载 `read_only:false`。
  全程 **0 次权限请求**。
- **续用/身份失效**：`native_missing_session.py`（`test_passed:true`，证据
  `/tmp/g2b-copy/.harness/validation/g2b-native-missing-20260919T082029`）——失效身份被显式拒绝
  （仅 `restart-session`/`abort`）、替换会话拿到**新**原生身份、旧证据经 handover 保留、对抗性反馈
  注入被忽略、替换 worker 以真实用量证据 PASS。
- **运行上下文与重定位披露**：`sandbox-exec` 在代理继承上下文无法 apply（G2a 期已定性的环境事实），
  故按已验证程序在 launchd 拉起的 runner 下、于候选工作树的**逐字节副本** `/tmp/g2b-copy` 执行：
  副本 HEAD `c5164bd`（仅记账领先候选）、驱动二进制 sha256 `c1dda95f…` 与记录**逐字节相等**、
  内容寻址运行时身份 `a1edad6a…` 与记录**相等**（启动前核验），探针事后又从真实工作树重验身份绑定。
- **模型调用消耗**：小流程 7 角色＋missing-session 证明 1 次＝授权范围内的全部支出。

## 本单元搬了什么

| 归属 | 内容 |
| --- | --- |
| **Go（新，全部在 `internal/harness` 单包）** | `harness native <dir>` 窄内部命令（`native_dsh.go::RunNative`）：读 `native-request.json`→provider 分派→ACP 时序（initialize/session new|resume/set_config_option/prompt/close）→证据落盘→stdout 结果→`native-error.json`；共享传输机械（`native_transport.go`：进程派生、逐行读循环、无界队列、逐消息/UNBOUNDED 等待、trace、交互回执、关闭语义、Python 真值/相等语义）；`pydump.go::PyDumpsRaw`（保序再编码器，**唯一新导出**）；四个桥接操作 `native-capture`／`native-verify-binding`／`native-verify-source`／`native-reconcile`；`command.go` 的 native 分支 |
| **Python（改）** | `native_hosts.py`：`GoAdapter`（dsh；`entry_argv`→`[binary,'harness','native',dir]`、探针路径写同一 `native-request.json` 合同后派生同一 Go 命令、四个生命周期方法走桥接、`transport_files()==('native_dsh.mjs',)`）；`workers.py` 仅 argv 一行 |
| **Python（退役）** | `native_dsh.py` 删除，逐字移入 `tests/reference_native_dsh.py`（git 识别为 rename，相似度 79%＝docstring＋相对导入改绝对导入两处，其余逐字节） |
| **不动** | `native_zcode.py`／`native_transport.py`／`native_entry.py`／`zcode_credential.mjs`／`native_dsh.mjs`（对 HEAD diff 为空）；`supervise.go`、`cmd/formal-gates`、`go.mod` 零改动 |

## 形态与反过度设计（合同"反过度设计约束"节的执行结果）

- 原生回合是长驻进程＋UNBOUNDED 等待＋宿主反向调用，与桥接操作的请求/应答前提冲突——沿 G2a 先例走
  §12.1 内部窄命令。监督器零改动（`kind` 仍不被读取），"在已验收监督器下"由 argv 一行成立。
- 派发前自查砍掉两处过度设计：`internal/native` 新包＋单实现 `NativeOperations` 接口＋装配注入
  （违反方案 §15"至少两个实际调用点/不为假设调用方预建体系"）；`PyDumps`/`OrdObj` 值编码器
  （为不可观察的出站行空格保序）。最终 Go 侧新增 3 个产品文件＋6 个测试文件，触点 7 处；
  G2c 的 `zcode.go` 将落同包，维持"每平台一个薄文件"。
- 字节保真只保留**可观察面**：证据文件（canonical）、错误文本（含内嵌 JSON 的文档键序＋Python 数字
  语义，经 `PyDumpsRaw`）、prompt 文本（发模型）。出站协议行、`protocol.jsonl`、stdout 改 canonical
  紧凑形（JSON 等价，消费者按值解析），登记为有界差异，差分按值比对。

## 实施与独立复核（子代理实施，主代理独立复核；自述一律当未验证）

- **派发 A（Go）**：合同收紧后按单包规格实施。主代理独立重验：`go build`、`go vet`（darwin＋
  GOOS=windows/linux）、`gofmt -l internal/harness cmd` 空、`go test ./internal/harness -count=1` 全过
  （42.6s）。其一处越界修复（`internal/engine/definition/replace_file_windows.go` 的一行注释空白，
  索引 `GOFMT-WINDOWS-FILE-PREEXISTING` 登记的独立修复项）**已回退**，不混入本单元。
- **派发 B（Python）**：按收紧合同收拾首个被取消代理留下的旧规格草稿（差分从逐字节改为按合同分面）。
  主代理独立重验：全套 **261 项 OK**（90.3s）、`demo --approve-sample`→`DEMO_COMPLETE`、
  `doctor --provider zcode`→`ok:true, model_calls:0`。
- **主代理独立 oracle**（`/tmp/g2b-oracle/`，不复用交付夹具）：自建假 ACP 宿主（镜像 dsh＋mjs 可观察
  行为）驱动两侧——Go 窄命令子进程 vs `reference_native_dsh.execute`。两模式（成功回合／权限交互）
  **全 PASS**：七类证据文件归一后逐字节相同、profile 除按设计不同的 mjs 定位外逐字节相同（Go 侧
  断言指向生产 `native_dsh.mjs`）、**host-prompt 逐字节**（含 Unicode 与 schema 的浮点/嵌套/排序键）、
  帧值等价、stdout 等于 reference 返回、permission 错误文本逐字节、反向请求送达且零答复。
- **负控制（"撤掉修复必须真失败"）**：主代理两处源码定向变异——prompt 后缀（→`host-prompt.txt`
  bytes differ 真失败）、`DSH_INTERACTION_REQUIRED` 文本（→native-error 文本精确差异真失败）——每处
  变异后即恢复并重建验证；交付差分自带另两处（prompt 尾注、`DSH_TRANSPORT` 前缀）。
- **真实宿主零模型调用探针**：`doctor --provider dsh` 经 Go 路径 `ok:true`（真 dsh 二进制完成
  initialize/session/close 往返）。
- **过程披露**：首个派发被机主中断，其未提交草稿（含超范围的 Python 改动）由派发 B 按现行合同逐块
  审查收拾；两代理与主代理的分工与该过程在合同"反过度设计约束"节与本记录如实登记。

## 验收结果（九项公共门槛）

| 检查 | 结果 |
| --- | --- |
| identity / go-vet / go-test-full（无缓存全量） | PASS（go-test 391s） |
| sqlite-probe（CGO 开/关） | PASS |
| python-unittest（全套 261 项） | PASS（86.1s） |
| demo-fixture（`DEMO_COMPLETE`） | PASS |
| doctor-dsh / doctor-zcode（0 次模型调用） | PASS |
| tree-clean-after | PASS |
| **原生定向验收（已授权、已执行）** | **`PASS`**：`HARNESS_COMPLETE`、7/7 角色、0 中断、134.9s；续用/身份失效证明 `test_passed:true` |

## 准入控制项的本次形态

`admission.py` 将 G2b 入列 `UNITS_REQUIRING_NATIVE_VERIFICATION`（一行）。控制项在候选提交后对
**G2a 旧记录**跑过两轮：核心拒绝项（E1–E10 等）全过；原生簇（ADM-PC、E11–E18）呈**标准
"candidate moved" 滞后形态**（G2a 记录对已前移的 HEAD 深查被拒，逐例输出均为该拒绝理由）——与
M2 等更早单元在链头前进后的形态一致，非 checker 回归。**定性绿跑**待 G2b 原生段补齐后对本记录执行。

## 原生关口（已关闭）

授权（机主 2026-09-19 逐字「可以执行」，对应点名 DSH 完整小流程＋非继承上下文程序的请求）已执行：
完整小流程 7/7 `HARNESS_COMPLETE`＋续用/身份失效证明通过，记录原生段为 `PASS`。DSH 在本机的
非继承上下文程序（launchd 拉起＋逐字节副本）已由本次运行再次验证可用。

## 已知有界差异与遗留

| 项 | 内容 |
| --- | --- |
| 出站行/trace/stdout 序列化 | canonical 紧凑形（Python 侧为默认分隔符＋插入序）；JSON 等价、消费者按值解析；差分按值比对 |
| mjs 定位 | reference 侧按自身位置推导（`tests/`），Go 侧按根发现规则指向生产 `native_dsh.mjs`；差分归一化该字段 |
| Windows 出站行尾 | Go 统一 `\n`（Python 文本模式在 Windows 发 `\r\n`）；仅影响值比对面 |
| 不可达路径宽恕 | 宿主应答 null `agentCapabilities`/非对象 `reason`/`message` 等，Python 抛 `AttributeError`、Go 按缺省处理（错误文本可观察面仍逐字）；pgid ESRCH 补杀子进程 |
| 差分超时用例 | Go 命令无短超时入口，Go 侧超时文本由 `TestTransportTimeoutText` 钉住，差分只跑 reference 侧 |
| GOFMT-WINDOWS-FILE-PREEXISTING | 保持独立修复项未动（派发 A 的一行修复已回退） |

## 复核入口

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
go build -o .harness/bin/formal-gates ./cmd/formal-gates
go test ./internal/harness -count=1
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$PWD/runtime/langgraph" TMPDIR=/private/tmp \
  .harness/venv/bin/python -m unittest discover -s runtime/langgraph/tests
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$PWD/runtime/langgraph" TMPDIR=/private/tmp \
  .harness/venv/bin/python -m unittest runtime.langgraph.tests.test_native_dsh_differential -v
./scripts/harness doctor --provider dsh   # 0 模型调用
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-G2b.json --next-unit G2c
# 原生段已补齐为 PASS，预期 ADMISSION GRANTED for G2c
```
