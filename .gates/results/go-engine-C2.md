# C2 验收记录：证据内容摘要原语迁移到 Go（G1a 第二项）

## 结论

替换单元 C2 通过：证据文件的内容证明（读取 + sha256 + size）已从 Python 迁入 Go，`file_proof` 薄化为适配器，7 个模块约 15 个调用点的对外行为不变。

| 检查 | 结果 |
| --- | --- |
| 本块合同（Go 单元测试） | 通过 |
| 文件样本差分对照（参考实现 vs Go） | 通过，15 类样本证明与拒绝原因一致 |
| 累计组合完整流程（fixture） | 实际达到 `DEMO_COMPLETE` |
| 累计自动化回归（Go 无缓存全量） | 通过：26 包、1922 用例通过事件、0 失败、无停滞 |
| 累计自动化回归（Python 全套） | 通过，117 项 |
| SQLite 探针 | 通过，CGO 开/关各 8/8 |
| DSH / ZCode `doctor` | 通过，接入探测、`model_calls=0` |
| 原生定向验收 | **不适用**：纯只读计算，不涉及宿主协议、进程、权限或工具继承 |
| 真实原生行为复验 | **NOT_RUN**（授权关口在 G2b 之前） |
| **总体** | **`PASS`** |

上一验收基线：`C1`（候选 `20b0422`，记录提交 `a756cde`）。

## 迁移内容

| 项 | 内容 |
| --- | --- |
| 新归属 | `internal/harness.FileProof`（`internal/harness/evidence.go`） |
| 新桥接操作 | `file-proof`，载荷 `{"path": "..."}` |
| 返回 | `{"ok": true, "proof": {"sha256": ..., "size": ...}}` 或 `{"ok": false, "refusal": "EVIDENCE_MISSING: ..."}` |
| Python 侧 | `infra.file_proof(path)` 保留签名，变为路径格式化 + 调用 + 拒绝翻译 |
| 路径格式化 | 留在调用方：拒绝消息按调用方构造的路径原样呈现，不引入第二套规范化 |
| 顺带收敛 | 驱动桥接解析统一为 `infra.driver_bridge()`；C1 引入的 `native_result` 私有解析已删除，不存在两套查找机制 |

## 范围判定（为什么只搬这一层）

这不是取舍口径，而是实测与代码事实的结论。

**`verify_process` / `verify_evidence` 搬不动，除非先做格式迁移。** 它们比较 `digest(request)`，即 Python 命名空间下的 JSON 规范化摘要。实测确认两侧摘要互不比较、各属独立命名空间：

```text
值 {"a":"x<y & z"}
Python canonical 字节   b'{"a":"x<y & z"}'
Python digest           cfe273e8e26377ee952f273b7ce8e8b0520fa24221f325355288d556618b144b
Go 编码（bridge 响应）  "x\u003cy \u0026 z"    ← Go 转义 < 与 &
```

Go 的 `harness.Digest` 仅用于 `revision` 与 `binding`（Go 内部自比）；Python 的 `digest` 仅用于操作 ID、反馈与交接键。**把规范化搬到 Go 会改变 Python 侧每一个摘要**，属方案 12.2 所说的格式变更，需另作版本化迁移。在此决定之前，这两个校验函数无法整体搬动。

**`verify_source` / `validate_binding` 不搬**：它们是宿主编配器的生命周期策略（`HOST_CHANGED` / `EVIDENCE_CHANGED` 的措辞与适用对象），随宿主适配器在 G2b/G2c 迁移。

**`read` / `canonical` / `atomic` 不搬**：`read` 是通用 JSON 读取，异常面含 `UnicodeDecodeError`/`JSONDecodeError`/`FileNotFoundError`，是基础设施而非证据完整性业务；另两者见上。

**结论：G1a 范围内所有能在不做格式迁移的前提下搬动的只读计算，在 C2 之后已搬完。**

## 差分对照

迁移前的实现逐字保留在仅测试使用的 `runtime/langgraph/tests/reference_evidence.py`。

`test_evidence_proof_differential.py` 用真实文件比较两个实现：空文件、单字节、ASCII、CRLF、UTF-8 非 BMP、含 NUL 与高位字节的二进制、Unicode 文件名、嵌套路径、≥1 MiB 大文件、无尾换行、符号链接（证明等于目标）、断链、目录、不存在、不存在（嵌套）、不可读。**证明与拒绝原因全部一致。**

Go 侧 `evidence_test.go` 另覆盖：内容直接哈希对照、大文件、符号链接跟随、Unicode 文件名、目录/断链/不存在拒绝、证明形状、确定性。

## 已测量的开销

| 场景 | 迁移后（Go） | 迁移前（进程内） |
| --- | --- | --- |
| 小文件 | 6.1 ms/次 | 0.036 ms/次 |
| 4 MB 文件 | 10.7 ms/次 | 3.1 ms/次 |

约 6 ms/次是跨进程固定成本。现有调用点都是"每次角色完成数个文件"的量级，可接受。若将来出现批量证明的调用方，应加**批量操作**而不是改回逐调用机制。此项按方案第 15 节记录为实测性能数据。

## 候选身份

```text
工作树   /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
分支     codex/go-engine-v1
提交     2ed75d4eeefdfcd97b4f3a5d71e6a0f96219ef77
上一基线 20b0422e1292c0a2812f03410e3390c4a129f58d（C1）
运行时身份 0bc5cf97f683f981fd5c708e68c830dae10854bc23e4c8d4c9770dc6ab97a687
驱动二进制 2cd551891539aee072e2db5643ba26aa26f2d3ae6f086ee4c397c461bf5d8505
证据清单 64 个文件
```

## 累计回归的实际结果

| 项 | 耗时 | 说明 |
| --- | --- | --- |
| `identity` | 1.1s | 解释器、41 项已安装依赖、源码与二进制摘要 |
| `go-vet` | 0.4s | |
| `go-test-full` | 446.1s | 26 包、1922 用例通过事件、0 失败、`stall=None`；`internal/validate` 438.8s |
| `sqlite-probe` | 2.8s | CGO 开/关各 8/8 |
| `python-unittest` | 51.3s | 117 项，含 C1/C2 两个差分对照 |
| `demo-fixture` | 3.7s | 达 `DEMO_COMPLETE` |
| `doctor-dsh` / `doctor-zcode` | 2.4s / 3.4s | 接入探测 |

Go 用例通过事件由 C1 的 1911 增至 1922；Python 用例由 B0 的 108 增至 117。

## 未解决问题

沿用 B0/C1 记录中的既有项（`NATIVE-REVERIFY-AT-BASELINE`、`ENV-THROUGHPUT-UNIDENTIFIED`、`BUILD-PATH-IN-RUNTIME-IDENTITY`、`NODE-PATH-AMBIGUOUS`、`ZCODE-HEADLESS-BLOCK`、`DRIVER-VERSION-DEFERRED`、`REGRESSION-COST`、`CONTROL-SCRIPT-NOT-IN-RUNNER`、`LONE-SURROGATE-ESCAPE`）。本单元新增：

| 编号 | 影响 | 处置 |
| --- | --- | --- |
| `CANONICALISATION-FORMAT-BLOCKED` | `verify_process`/`verify_evidence` 无法在 G1a 内搬动 | 已用实测界定：搬动会改变 Python 侧全部摘要，属版本化格式迁移。**这是本单元唯一因技术原因留下的 G1a 范围缺口，已显式登记待评审决定** |
| `PROOF-CALL-CROSSES-PROCESS` | 细粒度原语跨进程约 6 ms/次 | 已测量并记录；现调用点量级可接受，批量需求出现时加批量操作 |

## 回退入口

```sh
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine \
  reset --hard 20b0422e1292c0a2812f03410e3390c4a129f58d
```

## 复核入口

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-C2.json --next-unit C3
go test ./internal/harness -run 'TestFileProof' -v
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$PWD/runtime/langgraph" \
  .harness/venv/bin/python -m unittest discover -s runtime/langgraph/tests -p 'test_evidence_proof_differential.py' -v
```

机器可读记录：`.gates/results/go-engine-C2.json`。单元合同：`.gates/results/go-engine-C2-contract.md`。

本记录只汇总已观测结果；日志保留原始失败，不以最终通过覆盖中间尝试。
