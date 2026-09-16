# G1a 状态索引（供外部评审）

G1a 是分块替换的第一阶段：**只读计算**。方案要求解码与证据校验**分别验收**。

> **G1a 尚未完成。** 上一版索引记为"已搬完"，该表述已撤回。原因见"范围更正"。

## 基线与验收链

每个替换单元在上一已验收基线上实施，累计组合通过后固定为下一基线。源码提交与验收结论分开提交。

| 单元 | 内容 | 候选提交 | 记录提交 | 结论 |
| --- | --- | --- | --- | --- |
| B0 | 基线冻结与标定（含验收工具、SQLite 锁定） | `41d3375` | `284ceff` | `PASS` |
| C1 | 结果解码 | `20b0422` | `a756cde` | `PASS`（后被独立验收发现回归，见 C2 重签） |
| C2 | 证据内容摘要原语 + C1 回归修复 | `87895e4` | 见 `go-engine-C2.json` | `PASS` |

C2 记录取代了绑定 `2ed75d4` 的上一版 C2 记录：该候选存在一处 C1 回归，修复后候选前移，原结论不再适用。分支 `codex/go-engine-v1`，基线起点 `c6d49323`。

## 独立验收三项，逐项处置结果

| 项 | 结论 | 处置 |
| --- | --- | --- |
| P1 说明中提及 NaN/Infinity 会拒绝合法结果 | **真实回归，已复现** | 非有限数字判定从"整条消息预扫描"改为"在解析器所在位置检查正在解析的片段"；补齐"说明中提及→接受"与"对象内实际包含→拒绝"两个方向共 7 个用例 |
| P2 重复空字符串键未被拒绝 | **真实漏检，已复现** | "是否发现重复"与"重复键的值"分离；同处第二个隐患（非字符串键伪装成空键重复）一并修掉 |
| G1a 范围的理由不足 | **成立** | 撤回原理由，并给出实测依据（见下） |

## 范围更正：原"阻塞"不成立

原索引称"把 JSON 规范化搬到 Go 会改变 Python 侧每一个摘要，必须另作版本化迁移"。**该理由过强。**

真正的问题只是"Go 的默认编码器与 Python 不同"。我据此实现了复现 Python `json.dumps(ensure_ascii=False, sort_keys=True, separators=(",",":"))` 的 Go 编码器并逐字节对照：

| 样本 | 结果 |
| --- | --- |
| 448 个：200 个跨约 40 个数量级的随机浮点、大整数、次正规数 `5e-324`、`1e15/1e16/1e21` 阈值、Unicode 非 BMP／组合字符／RTL、控制字符、`<`/`&`、U+2028/U+2029、空键、150 个随机嵌套结构 | **逐字节不一致 0 个** |

关键难点是浮点格式化（Python `repr(float)` 的定点/科学计数法阈值与指数位数规则）与字符串转义规则，均可复现。

**因此证据校验可以在 G1a 内继续补齐。**

## G1a 剩余工作

| 单元 | 内容 | 前置 |
| --- | --- | --- |
| C3 | `canonical` / `digest` 迁入 Go（用实测过的编码器），附逐字节等价对照 | 无 |
| C4 | `verify_process` / `verify_evidence` 迁入 Go：请求/回执绑定、证明集合检查、结果比较 | 依赖 C3 |

仍是宿主编配器生命周期策略、随 G2b/G2c 迁移的：`verify_source` / `validate_binding`。

## 本阶段已完成的两项搬动

| 单元 | 新归属（Go） | 桥接操作 | Python 侧 |
| --- | --- | --- | --- |
| C1 | `internal/harness.FrameResult` | `frame-result` | `native_result.decode_result` 变为适配器 |
| C2 | `internal/harness.FileProof` | `file-proof` | `infra.file_proof` 变为适配器 |

两处拒绝**不走桥接失败形状**，而走 `value` 里的类型化结果（`{"accepted": false, "refusal": ...}` / `{"ok": false, "refusal": ...}`）。失败形状（`{ok:false, code}` + 退出码 2）保留原义，用于"桥接本身坏了"。

## 复核命令

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine

# 准入：读取记录并核验结果、身份、已安装依赖与公开基线冒烟
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-C2.json --next-unit C3

# 完整验收（新尝试目录，不覆盖历史）
python3 scripts/go-engine/acceptance.py --unit C2

# 两个工具自身的控制项（需要干净工作树）
sh scripts/go-engine/negative-controls.sh "" .gates/results/go-engine-C2.json
```

| 文件 | 内容 |
| --- | --- |
| `.gates/results/go-engine-B0.md` / `.json` | 基线冻结记录 |
| `.gates/results/go-engine-C1-contract.md`、`C2-contract.md` | 实施前固定的单元合同 |
| `.gates/results/go-engine-C1.md`、`C2.md` 与同名 `.json` | 单元验收记录 |
| `scripts/go-engine/README.md` | 验收与准入工具的约定、判定条件、控制项清单 |
| `scripts/go-engine/dependency-lock.json` | Go/SQLite 驱动与依赖锁定 |

原始证据在记录的 `attempt` 字段所指目录，含 `evidence.json`（逐文件摘要）、各必需项 `summary.json`、每个命令的 stdout/stderr/exit/cmd。每次尝试使用全新目录，已存在即拒绝，**不被后续运行覆盖**。

## 明确未验证的事项

- **真实原生模型流水线**：`NATIVE-REVERIFY-AT-BASELINE` 未执行，需显式授权费用。G1a 两项都是纯计算，**不需要**原生定向验收；授权关口在 G2b 之前。`live-20260915-2050` 的原生证据绑定更早的提交与运行时，不可当作当前候选的原生验收。
- **跨平台**：只在本地 macOS 实测。

## 已知有界差异与风险

| 编号 | 内容 |
| --- | --- |
| `LONE-SURROGATE-ESCAPE` | 文本含转义 `\ud800` 时两侧都接受，但 Python 保留代理项码位、Go 为 U+FFFD |
| `ENV-THROUGHPUT-UNIDENTIFIED` | 独立环境整体吞吐约为本机 1/4，成因未查明；回归判定已改为不依赖吞吐 |
| `BUILD-PATH-IN-RUNTIME-IDENTITY` | `go build` 不加 `-trimpath` 时运行时身份随构建路径变化 |
| `NODE-PATH-AMBIGUOUS` | 两份 node 摘要不同，跨 shell 恢复可能触发 `HOST_CHANGED` |
| `PROOF-CALL-CROSSES-PROCESS` | 细粒度原语跨进程约 6 ms/次，已测量 |
| `ZCODE-HEADLESS-BLOCK` | ZCode 无头在原生 Edit 审批处按合同返回 `ZCODE_INTERACTION_REQUIRED` |

## 评审时值得重点看的点

1. **P1/P2 的修复是否到位**：非有限数字的判定范围、两个方向的对照用例是否覆盖了你关心的形态；空键之外是否还有同类"哨兵值被当作合法值"的写法。
2. **C3 的编码器等价性**：448 样本是否够，是否需要补充你关心的浮点/Unicode 形态；等价证明应作为 C3 的合同条款。
3. **C4 的边界**：`verify_process` 里除 `digest(request)` 外还有路径绑定与字段比较，哪些属证据校验、哪些属存储层。
4. **跨进程原语的取舍**：`file_proof` 从 0.036 ms 变为 6.1 ms。
5. **回归为何没被本阶段自己的测试发现**：本轮新增的 7 个用例都来自评审反例，说明我的语料偏向"结构变体"而非"语义相邻文本"。这是我需要在后续单元改进的地方。

本索引只汇总各单元记录中已观测的结果；不含密钥或认证材料。
