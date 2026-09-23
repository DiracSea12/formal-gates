# C1 验收记录：结果解码迁移到 Go（G1a 第一项）

## 结论

替换单元 C1 通过：结果框架判定已从 Python 迁入 Go，Python 侧薄化为适配器；逐项合同、拒绝行为对照、累计组合完整流程与累计自动化回归全部通过。

| 检查 | 结果 |
| --- | --- |
| 本块合同（Go 单元测试） | 通过 |
| 固定样本差分对照（参考实现 vs Go） | 通过，71 个样本判定/原因/值一致 |
| 累计组合完整流程（fixture） | 实际达到 `DEMO_COMPLETE` |
| 累计自动化回归（Go 无缓存全量） | 通过：26 包、1911 用例通过事件、0 失败、无停滞 |
| 累计自动化回归（Python 全套） | 通过 |
| SQLite 探针 | 通过，CGO 开/关各 8/8 |
| DSH / ZCode `doctor` | 通过，接入探测、`model_calls=0` |
| 原生定向验收 | **不适用**：本单元是纯计算，不涉及宿主协议、进程、连接、权限或工具继承 |
| 真实原生行为复验 | **NOT_RUN**（授权关口在 G2b 之前） |
| **总体** | **`PASS`** |

上一验收基线：`B0`（候选 `41d3375`，记录提交 `284ceff`，准入已放行）。

## 迁移内容

| 项 | 内容 |
| --- | --- |
| 新归属 | `internal/harness.FrameResult`（`internal/harness/frame.go`） |
| 新桥接操作 | `frame-result`，载荷 `{"text": "..."}` |
| 返回 | `{"accepted": true, "result": {...}}` 或 `{"accepted": false, "refusal": "NATIVE_RESULT: ..."}` |
| Python 侧 | `native_result.py` 保留 `decode_result(text)` 签名，变为输入输出转换 + 调用 + 拒绝翻译 |
| 未改动 | `internal/harness.StrictDecode`（固定类型严格解码）保持独立；桥接失败形状与退出码不变 |

**拒绝为什么走载荷字段而不是桥接失败形状**：失败形状（`{ok:false, code:"INVALID_HARNESS_OPERATION"}`，退出码 2）是既有合同，本单元不改动它。"消息框架不合格"是**预期内的类型化业务结果**，不是传输或操作错误；混进失败形状会让调用方无法区分"Go 拒绝了这条消息"和"桥接本身坏了"。

## 差分对照

按方案 12.2，纯计算可以同时对照两个实现。迁移前的实现逐字保留在**仅测试使用**的 `runtime/langgraph/tests/reference_result.py`（生产代码不导入），固定样本在 `runtime/langgraph/tests/result_corpus.py`。

`runtime/langgraph/tests/test_result_frame_differential.py` 对 71 个样本比较：接受/拒绝判定、拒绝原因、接受值的规范序列化。样本集刻意覆盖 Unicode（非 BMP、组合字符、转义、RTL、Unicode 键）、空集合（`{}`、`[]`、嵌套空）、以及拒绝项（重复字段含转义键、非有限数字、竞争内容、顶层非对象、畸形输入、BOM）。**全部一致。**

对照中我写错三处测试预期（行内花括号不是裸对象候选、尾随逗号归"invalid result object"、散文含方括号属竞争内容），以测量到的参考行为为准修正——不是调整实现去迁就预期。

### 两处有界差异（显式记录，不静默忽略）

| 编号 | 输入 | 参考实现 | Go 实现 | 性质 |
| --- | --- | --- | --- | --- |
| `LONE-SURROGATE-ESCAPE` | 文本含 JSON 转义 `\ud800` | 接受，值为孤立代理项，规范序列化 `{"note":"\ud800"}` | 接受，值为 U+FFFD，规范序列化 `{"note":"\ufffd"}` | Go 的 UTF-8 字符串无法表示孤立代理项，解码器替换为 U+FFFD。**判定一致、值不同** |
| `LONE-SURROGATE-LITERAL` | 文本含字面孤立代理项 | 接受，随后 `atomic()` 写盘抛 `UnicodeEncodeError` | 桥接调用处即抛 `UnicodeEncodeError` | **不是新差异**：两侧都以 `UnicodeEncodeError` 失败且不产出 `answer.json`，只是失败点提前一步。`infra.canonical` 用 `ensure_ascii=False` 本来就无法编码孤立代理项 |

两处都由 `test_documented_divergences_from_the_reference` 断言，任何一个方向变化都会被测试发现。两处都不影响两处调用点的对外行为（它们在解码后立即写盘）。

## 候选身份

```text
工作树   /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
分支     codex/go-engine-v1
提交     20b0422e1292c0a2812f03410e3390c4a129f58d
上一基线 41d3375cf16f1bd411f102db2d9edfaeb28c6a29（B0）
运行时身份 4e1d7588802863d7d6d51f6406fc91d6c9e2d8df290eb7f07917fb7387dfb77d
驱动二进制 5223291a0ce8f052b08c7ed735195221a8e011c127ba958db90c80ce1cdd5dfb
证据清单 64 个文件
```

运行时身份与驱动二进制摘要相对 B0 均已变化：本单元改了 harness 源码、`internal/harness`、Python 适配器并重建了驱动二进制，这正是方案 12.1 要求"候选运行时在独立位置构建"的情形。

## 累计回归的实际结果

| 项 | 耗时 | 说明 |
| --- | --- | --- |
| `identity` | 1.1s | 解释器、41 项已安装依赖、源码与二进制摘要 |
| `go-vet` | 0.4s | |
| `go-test-full` | 429.4s | 26 包、1911 用例通过事件、0 失败、`stall=None`；`internal/validate` 424.5s |
| `sqlite-probe` | 2.8s | CGO 开/关各 8/8 |
| `python-unittest` | 38.1s | 含新增差分对照 |
| `demo-fixture` | 2.6s | 达 `DEMO_COMPLETE` |
| `doctor-dsh` / `doctor-zcode` | 2.4s / 3.4s | 接入探测 |

Go 用例通过事件由 B0 的 1868 增至 1911，增量即本单元新增的 `internal/harness` 框架测试。

## 未解决问题

除 B0 记录中的既有项（`NATIVE-REVERIFY-AT-BASELINE`、`ENV-THROUGHPUT-UNIDENTIFIED`、`BUILD-PATH-IN-RUNTIME-IDENTITY`、`NODE-PATH-AMBIGUOUS`、`ZCODE-HEADLESS-BLOCK`、`DRIVER-VERSION-DEFERRED`、`REGRESSION-COST`、`CONTROL-SCRIPT-NOT-IN-RUNNER`）外，本单元新增：

| 编号 | 影响 | 处置 |
| --- | --- | --- |
| `LONE-SURROGATE-ESCAPE` | 病态输入下接受值不同 | 已显式记录并断言；不影响两处调用点行为。若将来需要逐字保留代理项，作为单独的版本化迁移处理 |
| `BRIDGE-CANNOT-CARRY-SURROGATES` | 字面孤立代理项无法过桥 | 迁移前同样失败（写盘时），非新增缺陷；已在记录中界定 |

## 回退入口

```sh
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine \
  reset --hard 41d3375cf16f1bd411f102db2d9edfaeb28c6a29
```

## 复核入口

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-C1.json --next-unit C2
go test ./internal/harness -run TestFrameResult -v
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$PWD/runtime/langgraph" \
  .harness/venv/bin/python -m unittest discover -s runtime/langgraph/tests -p 'test_result_frame_differential.py' -v
```

机器可读记录：`.gates/results/go-engine-C1.json`。
单元合同（实施前固定）：`.gates/results/go-engine-C1-contract.md`。
原始证据：记录 `attempt` 指向的目录。

本记录只汇总已观测结果；日志保留原始失败，不以最终通过覆盖中间尝试。
