# C4 验收记录（重签）：证据校验迁移到 Go（G1a 第四项，也是最后一项）

> **本记录取代同一路径下的上一版 C4 记录。** 上版绑定的候选 `0f1cd2b` 存在一处由独立验收发现的 **P1 数值语义缺陷**（证据大小按两种不一致的整数转换规则比较，既误拒绝又误接受），修复后候选前移到 `9687945`，因此上版结论不适用于新组合。上版内容保留在 git 历史中；其尝试目录未被覆盖。

## 结论

替换单元 C4 通过；**同时修复了 P1 数值比较缺陷**。G1a 四个单元（C1–C4）全部通过并被固定为下一基线。

| 检查 | 结果 |
| --- | --- |
| 本块合同（Go 单元测试 `verify_test.go`） | 通过：8 个测试函数，含数值精确比较与各拒绝家族 |
| 本块合同（差分对照） | 通过：13 项，拒绝的异常类与消息与迁移前实现**逐字一致** |
| P1 回归定向验证 | 通过：修复前四项症状（含正式桥接、`Store.verify_process`、内容寻址 `verify_evidence` 三条路径）全部按预期接受/拒绝 |
| **P1 负控制** | 通过：把 `verify.go` 临时撤回后，新增差分用例与两个 Go 用例**确实失败**，失败信息与评审描述一致 |
| 累计组合完整流程（fixture） | 实际达到 `DEMO_COMPLETE` |
| 累计自动化回归（Go 无缓存全量） | 通过：23 包执行、3 包无测试文件、**1993 用例通过事件、0 失败、`stall=None`** |
| 累计自动化回归（Python 全套） | 通过，**137 项**（连续两遍） |
| SQLite 探针 | 通过，CGO 开/关各 8/8 |
| DSH / ZCode `doctor` | 通过，接入探测 |
| 原生定向验收 | **不适用**：纯计算 |
| **总体** | **`PASS`** |

上一验收基线：`C3`（候选 `7492c01`）。

## P1：同一个数值换一种表示就改变校验结果——已修复

位置：`internal/harness/verify.go` 的 `proofMatches()` 与 `asInt64()`。

契约一直声明"数字按值比较，`1` 与 `1.0` 相等"，但实现把它变成了**两种互不一致的整数转换规则**：

```go
case json.Number:
    parsed, err := typed.Int64()   // "7.0" 解析失败
case float64:
    return int64(typed), true      // 7.75 被直接截断成 7
```

用一个真实的 **7 字节文件**、配上正确 sha256 与完整回执实测：

| 记录的 `size` | 应有 | 修复前 | 修复后 |
| --- | --- | --- | --- |
| 整数 `7` / `int 7` | 接受 | 接受 | 接受 |
| `json.Number("7.0")`（Python `7.0` 经桥接） | 接受 | **误拒绝 `EVIDENCE_CHANGED`** | 接受 |
| `json.Number("7e0")` | 接受 | 误拒绝 | 接受 |
| `float64(7.0)` | 接受 | 接受 | 接受 |
| `float64(7.75)` | 拒绝 | **截断成 7 后误接受** | 拒绝 |
| `json.Number("7.75")` / `float64(7.5)` | 拒绝 | 正确拒绝 | 拒绝 |
| `json.Number("8")` / 字符串 `"7"` | 拒绝 | 正确拒绝 | 拒绝 |

**误拒绝在三条正式路径上均已复现**：正式 `verify-process` 桥接、`Store.verify_process()`、内容寻址的 `Store.verify_evidence()`。文件没有变化却被报告为证据已变化。误接受仅出现在直接 Go 调用路径。

同一条比较链的另一表现：`valuesEqual(int(0), float64(0))` 原为 `false`，而 Python 的 `0 != 0.0` 为 `False`（应判相等）。**四项归并为同一个数值语义根因，未拆成多个问题分别打补丁。**

**修复**：引入唯一原语 `exactRational()`，把本包可能收到的任何数值（`json.Number`／`int`／`int64`／`float64`）精确化为 `*big.Rat`，非数与不有限浮点返回 false。`proofMatches` 经它比较记录大小与实际字节数；`valuesEqual` **先**咨询同一原语，因此 `int(0)` 与 `float64(0)` 现在相等。`asInt64` 与 `numbersEqual` 删除——**一条数值规则取代两条**。

**未采用规避手段**：没有改绑定证明、没有绕过比较、没有把非整数"接受后归一"。

**超出本单元范围但已核查**：`valuesEqual` 现仅存在于 `verify.go`，是该包内唯一的数值规则；`internal/harness` 中没有第二处把解码数字转整数的写法（其余同形态命中都在迁移范围外的包）。

## 测试本身的一处遗漏——已补，并加了负控制

上一版的 `TestVerifyProcessNumbersCompareByValue` 把回执与过程记录的 `exit_code` **两边都设成 `0.0`**，因此它从未执行名称与断言所声称的"`0` 对 `0.0`"。已改为真正的不同表示。

新增回归覆盖（评审指出的缺口）：

| 用例 | 覆盖 |
| --- | --- |
| `TestVerifyProcessSizeComparesByValue`（Go） | 对真实 7 字节文件：`7`／`7.0`／`7e0`／`float64(7)`／`int(7)` 接受；`7.75`／`float64(7.75)`／`float64(7.5)`／`8` 拒绝（家族与措辞正确） |
| `TestValuesEqualNumbersCompareByValue`（Go） | 跨表示相等（`int(0)` vs `float64(0)`、`json.Number("0")` vs `json.Number("0.0")`、`json.Number("1e0")` vs `int(1)`）；`json.Number("7.75")` vs `int(7)`、`json.Number("7")` vs `"7"` 不等 |
| `test_size_written_as_float_is_compared_by_value`（Python 差分） | proof.size 写作 Python `7.0`（Go 侧落为 `json.Number("7.0")`）——**两侧原先都漏掉的形态** |

**负控制已实际执行**：把 `verify.go` 临时撤回后重建驱动，Python 差分用例失败于 `('ok', None) != ('RuntimeBlocked', 'EVIDENCE_CHANGED: stderr.log')`，两个 Go 用例亦失败。**用例确实能检出该缺陷，不只是伴随缺陷存在。**

## 本单元搬了什么

把 Python `Store.verify_process` 与 `Store.verify_evidence` 里的**证据校验**搬进 Go：回执/请求绑定、字段比较、证明集合检查、逐文件证明校验，经桥接操作 `verify-process`、`verify-files` 暴露。

Python 侧保留两件属于存储层的事：**哪个 job 目录属于本次运行**，以及**读 `exit.json`/`request.json`**。拒绝随带 `kind`（`binding`／`missing`／`changed`），由 `refuse()` 映射回原异常类：`binding` → `HarnessError`，`missing`／`changed` → `RuntimeBlocked`。措辞逐字未变。

## 两处"照搬就会错"的地方，在写测试之前先修掉

| 陷阱 | 后果 | 处置 |
| --- | --- | --- |
| 证明用 map 传递 | 多条证明同时不一致时，**参考实现按文档顺序报第一条**，Go 的 map 无序会让拒绝消息随机化 | 证明改为**有序列表**。回执经规范编码发布（键有序），故两侧都先报 `stderr.log` 再报 `stdout.log` |
| 不可读证据折叠进 `EVIDENCE_CHANGED` | 迁移前的 `file_proof` 对缺文件**抛** `EVIDENCE_MISSING: <path>`（并非返回哨兵值），折叠会改变拒绝家族 | 读失败原样传播，与内容不符的 `EVIDENCE_CHANGED` 分开 |

## 顺带修正：超范围浮点字面量

**超出 float64 范围的浮点字面量**：Python 把 `1e400` 解析为 `+Inf` 并用 `Infinity` 渲染；Go 的 `strconv.ParseFloat` 会**同时返回该值和 `ErrRange`**，而编码器把 `ErrRange` 当成了拒绝。修复：只容忍 `ErrRange`，格式非法仍然拒绝。对照 `1e400`／`-1e400`／`1e309`／`2.5e400`／`1e-400` 均与参考实现一致。

## 已测量的边界（不可达，如实记录而非加机制）

参考实现读输入文件用 `file_proof(Path(name))`，缺失文件的消息里路径是 `Path` **规范化后**的形式；Go 按调用方传入的原样字符串命名。键非规范化**且**文件缺失时两者措辞不同（实测：参考 `…/missing.txt`，Go `…/./missing.txt`）。

**不可达**：键由 `workers.py` 以 `str(Path(comparison['directory']) / name)` 写出（构造 `Path` 时即已规范化）；`verify_evidence` 只接收内容寻址的 `Store.get(ref)` 输出，手工改写键会先触发完整性校验失败。

## 已知差异（非阻塞，P3，仅手工改写可达）

**布尔大小**：`size` 若以布尔值写入，Go 与参考实现的判定可能不同（Python 中 `True == 1`）。该值无法由任何正常写入者产生——`size` 来自 `len(body)`（int），`Store` 仅透传。外部评审明确列为 P3、非阻塞，本单元**不修复、不扩大范围**；`exactRational` 对 `bool` 返回 false，落入与改动前一致的 `a == b` 分支。

## 候选身份

```text
工作树   /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
分支     codex/go-engine-v1
提交     9687945080bbad616fa00766896fe149a4ed6d0e
上一基线 7492c01fa6b9d680ca540f8f04437a0884a0a12c（C3）
运行时身份 e286a5409edb1797057c924de3e58c11ee097705a8150a994940122fce50b381
驱动二进制 b8d174f37488c2ea1dfbec4a42ce1467758526995fb9eb990ec54b8d3877512d
证据清单 64 个文件
通过尝试 20260916T234200Z-85127
```

## 累计回归的实际结果

| 项 | 耗时 | 说明 |
| --- | --- | --- |
| `identity` | 0.9s | 解释器、已安装依赖、源码与二进制摘要 |
| `go-vet` | 0.5s | |
| `go-test-full` | 410.5s | 23 包执行、3 包无测试文件、1993 用例通过事件、0 失败、`stall=None`；最慢 `internal/validate` 402.8s、`internal/cli` 85.7s |
| `sqlite-probe` | 2.8s | CGO 开/关各 8/8 |
| `python-unittest` | 47.7s | 137 项 |
| `demo-fixture` | 2.8s | 达 `DEMO_COMPLETE` |
| `doctor-dsh` / `doctor-zcode` | 2.2s / 3.1s | 接入探测 |
| 合计 | 470.5s | |

C4 至今共跑了**三个独立尝试**，**均 `overall=PASS`、`failed=[]`**：`20260916T215311Z-75843` 与 `20260916T220122Z-99176`（候选 `0f1cd2b`，已被本版取代）、`20260916T234200Z-85127`（候选 `9687945`，本记录绑定）。三个目录均原样保留，未被覆盖。

## 未解决问题

| 编号 | 状态 |
| --- | --- |
| `CANONICAL-CALL-CROSSES-PROCESS` | 保留：`canonical`/`digest` 跨进程 6.1–6.3 ms/次；事件追加仍是每事件一次进程。按评审意见，后续按业务边界组织调用，不把逐原语起进程固化成最终架构 |
| `PROOF-CALL-CROSSES-PROCESS` | 保留（`file_proof` 6.1 ms/次）。C4 未新增高频路径，校验复用既有原语 |
| `PYTHON-SUITE-FLAKY-UNREPRODUCED` | 保留：上一轮的 1 FAIL + 1 ERROR 本机未复现，相关启动链上的 0.53s 导入已移除。C4 至今 6 次全套（含验收内）全过，仍未复现 |
| `BOOL-SIZE-IS-A-NUMBER-IN-PYTHON` | **新增（P3，非阻塞）**：`size` 为布尔值时两侧判定可能不同，仅手工改写可达。按评审定级不修复 |
| `ENV-THROUGHPUT-UNIDENTIFIED`、`BUILD-PATH-IN-RUNTIME-IDENTITY`、`NODE-PATH-AMBIGUOUS`、`ZCODE-HEADLESS-BLOCK`、`LONE-SURROGATE-ESCAPE`、`NON-FINITE-CROSSES-TRANSPORT`、`INPUT-PATH-NOT-NORMALISED` | 不变 |
| G1a | **完成**。按 §13.1，下一单元为 **G1b（材料生成：比较材料、反馈包装、工件发布，三项分别验收）**，前置需经准入核验 |
| 评审提到但未纳入本单元的简化点 | `receipt.streams` 与 `proofs` 列表重复携带证明；当前包装从同一回执直接构造，未发现生产路径不一致。记为 P3 备查，不加校验平台 |

## 回退入口

```sh
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine \
  reset --hard 7492c01fa6b9d680ca540f8f04437a0884a0a12c
```

## 复核入口

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-C4.json --next-unit G1b
go test ./internal/harness -run 'TestVerifyProcessSizeComparesByValue|TestValuesEqualNumbersCompareByValue' -v
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$PWD/runtime/langgraph" \
  .harness/venv/bin/python -m unittest discover -s runtime/langgraph/tests \
  -p 'test_evidence_verification_differential.py' -v
```

单元合同：`.gates/results/go-engine-C4-contract.md`（数值不变量的措辞已按本次修复写准）。
本记录只汇总已观测结果；日志保留原始失败，不以最终通过覆盖中间尝试。
