# C4 验收记录：证据校验迁移到 Go（G1a 第四项，也是最后一项）

## 结论

替换单元 C4 通过；**G1a 到此结束**。

| 检查 | 结果 |
| --- | --- |
| 本块合同（Go 单元测试 `verify_test.go`） | 通过：数值相等、各拒绝家族、逐文件证明校验 |
| 本块合同（差分对照） | 通过：12 项，拒绝的异常类与消息与迁移前实现**逐字一致** |
| 累计组合完整流程（fixture） | 实际达到 `DEMO_COMPLETE` |
| 累计自动化回归（Go 无缓存全量） | 通过：23 包执行、3 包无测试文件、**1991 用例通过事件、0 失败、`stall=None`** |
| 累计自动化回归（Python 全套） | 通过，**136 项** |
| SQLite 探针 | 通过，CGO 开/关各 8/8 |
| DSH / ZCode `doctor` | 通过，接入探测 |
| 原生定向验收 | **不适用**：纯计算 |
| **总体** | **`PASS`** |

上一验收基线：`C3`（候选 `7492c01`）。

## 本单元搬了什么

把 Python `Store.verify_process` 与 `Store.verify_evidence` 里的**证据校验**搬进 Go：回执/请求绑定、字段比较、证明集合检查、逐文件证明校验，经桥接操作 `verify-process`、`verify-files` 暴露。

Python 侧保留两件属于存储层的事：**哪个 job 目录属于本次运行**，以及**读 `exit.json`/`request.json`**。拒绝不再靠消息文本猜异常类，而是随拒绝带 `kind`（`binding`／`missing`／`changed`），由 `refuse()` 映射回原异常类：`binding` → `HarnessError`，`missing`／`changed` → `RuntimeBlocked`。措辞逐字未变。

## 两处"照搬就会错"的地方，在写测试之前先修掉

| 陷阱 | 后果 | 处置 |
| --- | --- | --- |
| 证明用 map 传递 | 多条证明同时不一致时，**参考实现按文档顺序报第一条**，Go 的 map 无序会让拒绝消息随机化 | 证明改为**有序列表** `[{name, expected}]`。回执经规范编码发布（键有序），故两侧都先报 `stderr.log` 再报 `stdout.log`；差分用例固定了这一条 |
| 不可读证据折叠进 `EVIDENCE_CHANGED` | 迁移前的 `file_proof` 对缺文件**抛** `EVIDENCE_MISSING: <path>`（并非返回哨兵值），折叠会改变拒绝家族 | 读失败原样传播 `EVIDENCE_MISSING: <path>`，与内容不符的 `EVIDENCE_CHANGED` 分开 |

## 顺带修正：超范围浮点字面量

C4 差分语料准备时发现 C3 的另一处缺口（评审未列出）：**超出 float64 范围的浮点字面量**。Python 把 `1e400` 解析为 `+Inf` 并用 `Infinity` 渲染；Go 的 `strconv.ParseFloat` 会**同时返回该值和 `ErrRange`**，而编码器把 `ErrRange` 当成了拒绝。

修复：只容忍 `ErrRange`（值仍被正确取用），**格式非法仍然拒绝**。对照 `1e400`／`-1e400`／`1e309`／`2.5e400`／`1e-400` 均与参考实现一致。

## 已测量的边界（不可达，如实记录而非加机制）

参考实现读输入文件用 `file_proof(Path(name))`，缺失文件的消息里路径是 `Path` **规范化后**的形式；Go 按调用方传入的原样字符串命名。键非规范化（含 `./` 或重复斜杠）**且**文件缺失时两者措辞不同（实测：参考 `…/missing.txt`，Go `…/./missing.txt`）。

**该差异不可达**，因此不为此增加机制：

1. 键由 `workers.py` 以 `str(Path(comparison['directory']) / name)` 写出，构造 `Path` 时即已规范化；
2. `verify_evidence` 只接收 `Store.get(ref)` 的返回值，工件**内容寻址**——手工改写键会先触发完整性校验失败，根本到不了 `verify-files`。

篡改分支（`EVIDENCE_CHANGED`）不涉及路径规范化，已由差分用例覆盖。

## 差分对照

迁移前实现逐字保留在仅测试使用的 `runtime/langgraph/tests/reference_evidence.py`（含 C1 基线的 `file_proof`），生产代码不引用它。12 项对照覆盖：完整回执通过、数值相等（`0` 与 `0.0`）、请求摘要不符、四个字段各自不符、进程记录缺 `request_digest`、job 不在本 run、证明集合缺一条、流内容被篡改、流被删除、多条不一致的取名顺序、输入文件的通过/篡改/缺失、拒绝家族到异常类的映射。

另一层等价证据是 **136 项 Python 全套**——工件引用、证据摘要、作业请求摘要、增量绑定与作业续接路径全部吻合；字节或措辞有变会先失败。

## 候选身份

```text
工作树   /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
分支     codex/go-engine-v1
提交     0f1cd2b156e47b3aa7fbe8048ef02d45f6993e68
上一基线 7492c01fa6b9d680ca540f8f04437a0884a0a12c（C3）
运行时身份 30a9b56fc50a1c79559f07900dc57d9afcf0db0ac685926ccef0aeabc928d0b2
驱动二进制 f7b62aa094f5a991fa304ba8581fdb9984d7ae60d5bd8d082e80954e8698727d
证据清单 64 个文件
通过尝试 20260916T220122Z-99176
```

## 累计回归的实际结果

| 项 | 耗时 | 说明 |
| --- | --- | --- |
| `identity` | 1.0s | 解释器、已安装依赖、源码与二进制摘要 |
| `go-vet` | 0.4s | |
| `go-test-full` | 418.8s | 23 包执行、3 包无测试文件、1991 用例通过事件、0 失败、`stall=None`；最慢 `internal/validate` 411.7s、`internal/cli` 87.9s |
| `sqlite-probe` | 2.7s | CGO 开/关各 8/8 |
| `python-unittest` | 50.3s | 136 项 |
| `demo-fixture` | 3.0s | 达 `DEMO_COMPLETE` |
| `doctor-dsh` / `doctor-zcode` | 2.2s / 3.1s | 接入探测 |
| 合计 | 481.7s | |

本次验收跑了**两个独立尝试**（`20260916T215311Z-75843` 与 `20260916T220122Z-99176`），**均 `overall=PASS`、`failed=[]`**；本记录绑定后者，前者原样保留、未被覆盖。

## 未解决问题

| 编号 | 状态 |
| --- | --- |
| `CANONICAL-CALL-CROSSES-PROCESS` | 保留：`canonical`/`digest` 跨进程 6.1–6.3 ms/次；事件追加仍是每事件一次进程。按评审意见，后续按业务边界组织调用，不把逐原语起进程固化成最终架构 |
| `PROOF-CALL-CROSSES-PROCESS` | 保留（`file_proof` 6.1 ms/次）。C4 未新增高频路径，校验复用既有原语 |
| `PYTHON-SUITE-FLAKY-UNREPRODUCED` | 保留：上一轮的 1 FAIL + 1 ERROR 本机未复现，相关启动链上的 0.53s 导入已移除。本单元两轮全套（首轮两次 + 验收内两次）共 4 次 136 项全过，仍未复现 |
| `ENV-THROUGHPUT-UNIDENTIFIED`、`BUILD-PATH-IN-RUNTIME-IDENTITY`、`NODE-PATH-AMBIGUOUS`、`ZCODE-HEADLESS-BLOCK`、`LONE-SURROGATE-ESCAPE` | 不变 |
| G1a | **完成**。按 §13.1，下一单元为 **G1b（材料生成：比较材料、反馈包装、工件发布）**，前置需经准入核验 |

## 回退入口

```sh
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine \
  reset --hard 7492c01fa6b9d680ca540f8f04437a0884a0a12c
```

## 复核入口

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-C4.json --next-unit G1b
go test ./internal/harness -run 'TestVerify' -v
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$PWD/runtime/langgraph" \
  .harness/venv/bin/python -m unittest discover -s runtime/langgraph/tests \
  -p 'test_evidence_verification_differential.py' -v
```

单元合同：`.gates/results/go-engine-C4-contract.md`。
本记录只汇总已观测结果；日志保留原始失败，不以最终通过覆盖中间尝试。
