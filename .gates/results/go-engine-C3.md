# C3 验收记录：规范化序列化与内容摘要迁移到 Go（G1a 第三项）

## 结论

替换单元 C3 通过：规范字节与内容摘要的权威移到 Go，Python 侧只做转发。**逐字节等价成立。**

| 检查 | 结果 |
| --- | --- |
| 本块合同（Go 单元测试） | 通过：形状、浮点边界、摘要一致性、确定性、拒绝不支持的类型 |
| 本块合同（逐字节等价对照） | 通过：450 个样本，字节与摘要 0 差异 |
| 传输编码非第二实现 | 通过（信封编码不等于规范字节） |
| 累计组合完整流程（fixture） | 实际达到 `DEMO_COMPLETE` |
| 累计自动化回归（Go 无缓存全量） | 通过：26 包、1974 用例通过事件、0 失败、无停滞 |
| 累计自动化回归（Python 全套） | 通过，122 项 |
| SQLite 探针 | 通过，CGO 开/关各 8/8 |
| DSH / ZCode `doctor` | 通过，接入探测、`model_calls=0` |
| 原生定向验收 | **不适用**：纯计算 |
| **总体** | **`PASS`** |

上一验收基线：`C2`（候选 `87895e4`，记录提交 `8ac722a`）。

## 迁移内容

| 项 | 内容 |
| --- | --- |
| 新归属 | `internal/harness.CanonicalJSON` / `CanonicalDigest`（`internal/harness/canonical.go`） |
| 新桥接操作 | `canonical`（返回规范文本）、`digest`（返回 64 位 hex） |
| Python 侧 | `infra.canonical` / `infra.digest` 保留签名，变为转发 |
| 受影响调用点 | 约 20 处：`atomic`、`Store.put`/`event`、`verify_process`、`job`、`handover`、`workers`、`retention`、`feedback`、`native_zcode`、`graph` |

### 为什么不能用 Go 默认编码器

| 差异 | Go 默认 | Python `json.dumps` |
| --- | --- | --- |
| `<` `>` `&` | 转义为 `\u003c` 等 | 原样输出 |
| 浮点定点/指数阈值 | `strconv` 自己的规则 | 十进制指数落在 `[-4, 16)` 用定点，且至少一位小数 |
| 指数位数 | 不定 | 至少两位：`e+16`、`e-05` |
| 任意精度整数 | 需显式处理 | 原生 |
| U+2028 / U+2029 | 转义 | 原样输出 |

因此编码逐条写出：键按 UTF-8 字节序（等价码位序）、`,":"` 紧凑分隔、短转义表、其余控制字符 `\u00xx` 小写、非 ASCII 原样 UTF-8、整数走 `big.Int` 规范化（`-0` → `0`）、浮点用自定义的 `pythonFloatRepr`。

### 桥接信封的处置（关键边界）

`Bridge.call` 原先用 `canonical` 序列化信封，把 `canonical` 指向桥接会**无限递归**。

信封改用私有传输编码：紧凑、保留非 ASCII，但**不排序**——只要求"是 Go `StrictDecode` 能接受的合法 JSON"。它不是内容寻址格式，也不承载摘要语义，因此 Python 侧**不存在规范化格式的第二实现**。

保留 `ensure_ascii=False` 是有意的：孤立代理项在信封层仍以 `UnicodeEncodeError` 响亮失败，而不是被静默替换成 U+FFFD。这让已登记的 `LONE-SURROGATE-ESCAPE` / `LONE-SURROGATE-LITERAL` 两处差异行为保持不变。

有一条测试专门守这个边界：断言信封编码**不等于**规范字节，一旦相等就说明信封悄悄变成了第二实现。

## 逐字节等价对照

迁移前的实现逐字保留在仅测试使用的 `runtime/langgraph/tests/reference_canonical.py`。

| 样本构成 | 数量 |
| --- | --- |
| 固定边界（空对象/空数组/空字符串、键排序含大写与 Unicode 与空键、大整数、`-0`、浮点阈值、次正规数、最大有限值、字符串转义、U+2028/9、非 BMP、组合字符、RTL、随机嵌套） | 50 |
| 随机浮点（固定种子 `20260916`，跨约 40 个数量级） | 300 |
| 随机整数（±10^18） | 50 |
| 随机嵌套结构（含空键、Unicode 键、控制字符、`<>&`） | 100 |
| **合计** | **450，字节与摘要不一致 0** |

**另一层等价证据**：122 项 Python 测试全过。工件引用、证据摘要、作业请求摘要、以及增量检查绑定（`check-retained` 路径依赖摘要一致）都仍然吻合——若字节有变，这些断言会先失败。

Go 侧 `canonical_test.go` 另以 Go 自己的语言固定同一格式：键序、空键、原样 Unicode、不转义 `< > &` 与 U+2028/9、控制字符短转义、任意精度整数、`-0` 规范化、定点与科学计数法的全部边界、摘要等于字节的 sha256、确定性、以及不支持类型的拒绝。

## 已测量的开销

| 场景 | 迁移后（Go） | 迁移前（进程内） |
| --- | --- | --- |
| `canonical` | 6.12 ms/次 | 0.0037 ms/次 |
| `digest` | 6.30 ms/次 | 约 0.004 ms/次 |
| fixture 流程 | **4.8 s** | 约 3.3 s |
| Python 全套 | 70.7 s | 约 50 s |

一次 fixture 运行约 160 次调用，增加约 1.5 s。`canonical`/`digest` 是调用最频繁的原语，这是本单元的主要代价。若将来需要批量，应加**批量操作**而不是改回逐调用机制。

## 候选身份

```text
工作树   /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
分支     codex/go-engine-v1
提交     04f343d4d8fc0e5129efb061221c866b239cdb93
上一基线 87895e411194d2266fb8e1a7293f1febe5babd9d（C2）
运行时身份 b334e4fc96741c13a90f56bbe81fa03bdb8da1581b81cc6d42e858973830006b
驱动二进制 3770b1bf2368b367d6ad01f05e600779dec249d1547eaf3c4abf3dc52774107c
证据清单 64 个文件
首次通过尝试 20260916T085743Z-87974
```

## 累计回归的实际结果

| 项 | 耗时 | 说明 |
| --- | --- | --- |
| `identity` | 1.1s | 解释器、41 项已安装依赖、源码与二进制摘要 |
| `go-vet` | 0.5s | |
| `go-test-full` | 459.1s | 26 包、1974 用例通过事件、0 失败、`stall=None`；`internal/validate` 450.2s |
| `sqlite-probe` | 3.0s | CGO 开/关各 8/8 |
| `python-unittest` | 70.7s | 122 项 |
| `demo-fixture` | 4.8s | 达 `DEMO_COMPLETE` |
| `doctor-dsh` / `doctor-zcode` | 2.5s / 3.5s | 接入探测 |
| 合计 | 545.2s | |

## 验收过程记录

本单元一次运行被自身安全网拦下：补写 Go 测试之前我已生成记录草稿，使工作树出现未跟踪文件；运行器的 `identity` 项要求工作树干净，于是 `overall=FAIL`、**未签发任何记录**。清理未跟踪文件后重跑通过。该次尝试目录保留在 `.harness/validation/acceptance/C3/20260916T085713Z-87794`，未被覆盖。

Go 用例通过事件由 C2 的 1932 增至 1974，增量即本轮新增的规范化 Go 测试。

## 未解决问题

沿用既有项。本单元新增/变更：

| 编号 | 状态 |
| --- | --- |
| `CANONICAL-CALL-CROSSES-PROCESS` | 新增：`canonical`/`digest` 跨进程 6.1–6.3 ms/次，fixture 流程 +1.5 s，Python 全套 +21 s。已测量登记；批量需求出现时加批量操作 |
| `PROOF-CALL-CROSSES-PROCESS` | 保留（C2 的 `file_proof` 6.1 ms/次） |
| C4 待做 | `verify_process` / `verify_evidence` 现可在 Go 内计算 `digest(request)`，前置已解除 |

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
