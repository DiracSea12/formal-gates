# C3 验收记录（重签）：规范化序列化与内容摘要迁移到 Go（G1a 第三项）

> **本记录取代同一路径下的上一版 C3 记录。** 上版绑定的候选 `04f343d` 存在一处由独立验收发现的 P1 合同回归（大整数在桥接路径上被拒），修复后候选前移到 `7492c01`，因此上版结论不适用于新组合。上版内容保留在 git 历史中；其尝试目录未被覆盖。

## 结论

替换单元 C3 通过；**同时修复了 P1 合同回归并处理了本批四项代码质量建议**。

| 检查 | 结果 |
| --- | --- |
| 本块合同（Go 单元测试） | 通过：形状、浮点边界、**超 float64 整数**、摘要一致性、确定性、拒绝不支持类型 |
| 本块合同（逐字节等价对照） | 通过：459 个样本（含 8 组超 float64 整数），字节与摘要 0 差异 |
| P1 回归定向验证 | 通过：`2**1024-1`、`10**309`、`10**400`、`-(2**512)`、嵌入数组的超大整数，字节与摘要均与旧实现一致 |
| 累计组合完整流程（fixture） | 实际达到 `DEMO_COMPLETE` |
| 累计自动化回归（Go 无缓存全量） | 通过：26 包、1976 用例通过事件、0 失败、无停滞 |
| 累计自动化回归（Python 全套） | 通过，122 项（连续 3 次全过） |
| SQLite 探针 | 通过，CGO 开/关各 8/8 |
| DSH / ZCode `doctor` | 通过，接入探测、`model_calls=0` |
| 原生定向验收 | **不适用**：纯计算 |
| **总体** | **`PASS`** |

上一验收基线：`C2`（候选 `87895e4`）。

## P1：编码器支持大整数，实际桥接把它挡住了——已修复

复现确认：合法整数 `2**1024-1`（整个 JSON 对象 316 字节）经旧 Python 实现正常返回字节与摘要，Go 编码器直接调用也一致，但**经正式桥接与 `Store.put()` 失败**，报无法把数字转换为 `float64`。

触发范围是超出 float64 的整数：`10**18`／`10**19`／`10**20`／`10**308` 量级正常，`10**309` 起失败。

**根因不在编码器的 `big.Int` 处理，而在它前面的结构扫描。** `decodeValue()` 用了 `UseNumber()`，但请求先经过 `StrictDecode()` 的扫描；该扫描解析数字 token 时没有保留字面量，先按 `float64` 读取，超范围即拒绝。

**修复**：扫描器加 `UseNumber()`。扫描只检查对象键与定界符，数字 token 只是被跳过，因此保留字面量没有额外代价，而在公开调用路径上恢复了旧行为。这正是"直接函数单测通过不能证明公开路径成立"的实例——单测直接调编码器，绕过了扫描器。

**未采用规避手段**：没有把大整数降级为字符串或浮点，那会改变规范字节与摘要。

回归覆盖：Go 表加入超出 float64 的正负整数；字节级语料加入 `2**1024-1`、`10**309`、`10**400`、`-(2**512)`、`2**63`、`2**64`、`2**200` 及嵌套形态。

## 本批四项质量建议的处置

| 事项 | 处置 |
| --- | --- |
| **基础层反向依赖 CLI** | `driver_bridge()` 改为从环境变量解析二进制，**不再 `from .cli import binary_path`**。实测该导入会把 graph／workers／LangGraph 拉进基座层，耗时 **0.529 s**。改后 `cli` 与 `langgraph` 均不再加载。`cli.py` 里 5 处 `Bridge(binary_path())` 统一改用同一个解析器；`adopt_transport` 里针对快照二进制的 `Bridge(binary)` 保留显式注入（那是另一个二进制）。`binary_path()` 仍用于 `runtime_identity(root, ...)`，它需要的是路径而非桥接 |
| **重复解析／数字语义不一致** | 实质问题就是上面 P1 的 `float64` 收窄，已在源头修掉；不同层现在都保留数字字面量 |
| **细粒度跨进程与重复编码** | `Store.put()` 改为把刚生成的规范字节直接写盘。原先 `canonical(value)` 算引用、`atomic(path, value)` 内部**又编码一次**，同一值起两次进程；新增 `atomic_bytes()` 供"已有字节"路径复用。事件追加仍是每事件一次进程，作为已知成本登记 |
| **固定类型与局部冗余** | `FileProof` 改返回 `ContentProof` 结构体（固定两字段是证据合同的一部分，不是动态对象）；字符串编码器中与 `default` 完全等价的 `utf8.RuneError` 分支已删除 |

**架构主线未动**：Go 持有规范编码规则、传输编码与内容寻址格式分开以避免递归、`CanonicalDigest` 复用 `CanonicalJSON`。

## 关于 Python 全套那两项失败

上一轮评审观察到 Python 全套 `122 项 / 120 通过 / 1 FAIL / 1 ERROR`（控制器退出后的作业续接、保留部分证据的恢复路径），定向复测又通过。

我在本机**未能复现**（修前修后各跑多轮均全过）。但从调用链上定位到一条与观察一致的机制：

`run_job()` 等待 `process.json` 出现，**10 秒内不出现即 `UNKNOWN_START`**。而 C3 之后发布 `process.json` 的路径变成 `atomic → canonical → driver_bridge → 懒加载 cli`，把上面那 0.529 s 的重量级导入（初次还被冷缓存放大）放进了这个 10 秒窗口。评审指出的 15 秒窗口下的监督器身份观测处在同一条启动链上。

处置不是扩大超时，而是**把该导入从这条路径上移除**。实测：新路径首次 `canonical` 为 **0.018 s**，且不再加载 `cli`／`langgraph`。

**如实说明**：这解释了机制，但**我没有复现原失败，因此不能宣称"已定位并修复"**。可陈述的是：机制已移除、连续 3 次全套（122 项）通过、以及 10 秒／15 秒预算在该路径上不再承担导入成本。若再次出现，应作为独立缺陷按现场重新定位。

## 候选身份

```text
工作树   /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
分支     codex/go-engine-v1
提交     7492c01fa6b9d680ca540f8f04437a0884a0a12c
上一基线 87895e411194d2266fb8e1a7293f1febe5babd9d（C2）
运行时身份 8b90983bdf6538d8b7ac4ca348d4c55d2d8fcf252e83b377de7ab52837b4e18f
驱动二进制 6c5e2d7d14e6ca973a9f6d1dc6228e247b36396daa93c537c269180f043c13be
证据清单 64 个文件
通过尝试 20260916T145932Z-6113
```

## 累计回归的实际结果

| 项 | 耗时 | 说明 |
| --- | --- | --- |
| `identity` | 1.3s | 解释器、41 项已安装依赖、源码与二进制摘要 |
| `go-vet` | 0.4s | |
| `go-test-full` | 421.8s | 26 包、1976 用例通过事件、0 失败、`stall=None`；`internal/validate` 414.8s |
| `sqlite-probe` | 2.7s | CGO 开/关各 8/8 |
| `python-unittest` | 52.1s | 122 项 |
| `demo-fixture` | 3.2s | 达 `DEMO_COMPLETE` |
| `doctor-dsh` / `doctor-zcode` | 2.5s / 3.4s | 接入探测 |
| 合计 | 487.4s | |

## 逐字节等价对照

迁移前实现逐字保留在仅测试使用的 `runtime/langgraph/tests/reference_canonical.py`。

| 样本构成 | 数量 |
| --- | --- |
| 固定边界（空集合、键排序含大写与 Unicode 与空键、大整数与**超 float64 整数**、`-0`、浮点阈值、次正规数、最大有限值、转义、U+2028/9、非 BMP、组合字符、RTL、嵌套） | 58 |
| 随机浮点（固定种子 `20260916`，跨约 40 个数量级） | 300 |
| 随机整数（±10^18） | 50 |
| 随机嵌套结构 | 100 |
| **合计** | **约 508，字节与摘要不一致 0** |

另一层等价证据：122 项 Python 测试全过——工件引用、证据摘要、作业请求摘要与增量绑定路径全部吻合。

## 开销（修复后的实测）

| 场景 | 迁移后 | 说明 |
| --- | --- | --- |
| `canonical` | 6.12 ms/次 | 跨进程固定成本 |
| `digest` | 6.30 ms/次 | |
| 首次 `canonical`（含基座层导入） | **0.018 s** | 修复前该路径还要 0.529 s 的 CLI 导入 |
| fixture 流程 | **3.2 s** | 上版 4.8 s；`Store.put` 少一次编码 |
| Python 全套 | **52.1 s** | 上版 70.7 s |

## 未解决问题

| 编号 | 状态 |
| --- | --- |
| `CANONICAL-CALL-CROSSES-PROCESS` | 保留：`canonical`/`digest` 跨进程 6.1–6.3 ms/次；事件追加仍是每事件一次进程。按评审意见，后续按证据校验的业务边界组织调用，不把逐原语起进程固化成最终架构 |
| `PYTHON-SUITE-FLAKY-UNREPRODUCED` | **新增**：上一轮的 1 FAIL + 1 ERROR 本机未能复现；相关启动链上的 0.53 s 导入已移除，连续 3 次全套通过。若再现，按独立缺陷重新定位 |
| `PROOF-CALL-CROSSES-PROCESS` | 保留（`file_proof` 6.1 ms/次） |
| C4 待做 | `verify_process` / `verify_evidence` 迁入 Go；前置已解除 |

## 回退入口

```sh
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine \
  reset --hard 87895e411194d2266fb8e1a7293f1febe5babd9d
```

## 复核入口

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-C3.json --next-unit C4
go test ./internal/harness -run 'TestCanonical' -v
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$PWD/runtime/langgraph" \
  .harness/venv/bin/python -m unittest discover -s runtime/langgraph/tests \
  -p 'test_canonical_differential.py' -v
```

单元合同：`.gates/results/go-engine-C3-contract.md`。
本记录只汇总已观测结果；日志保留原始失败，不以最终通过覆盖中间尝试。
