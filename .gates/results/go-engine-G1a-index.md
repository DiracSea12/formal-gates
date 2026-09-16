# G1a 完成索引（供外部评审）

G1a 是分块替换的第一个阶段：**只读计算**。本阶段把可以搬动的无副作用计算从 Python 迁入 Go，并保持对外行为逐字不变。

## 基线与验收链

每个替换单元在上一已验收基线上实施，累计组合通过后固定为下一基线。源码提交与验收结论分开提交。

| 单元 | 内容 | 候选提交 | 记录提交 | 结论 |
| --- | --- | --- | --- | --- |
| B0 | 基线冻结与标定（含验收工具、SQLite 锁定） | `41d3375` | `284ceff` | `PASS` |
| C1 | 结果解码（G1a 第一项） | `20b0422` | `a756cde` | `PASS` |
| C2 | 证据内容摘要原语（G1a 第二项） | `2ed75d4` | 见 `go-engine-C2.json` | `PASS` |

分支 `codex/go-engine-v1`，基线起点 `c6d49323`（父提交）。逐步回退入口见各单元记录。

## 本阶段实际搬动了什么

| 单元 | 新归属（Go） | 桥接操作 | Python 侧 |
| --- | --- | --- | --- |
| C1 | `internal/harness.FrameResult` | `frame-result` | `native_result.decode_result` 变为适配器 |
| C2 | `internal/harness.FileProof` | `file-proof` | `infra.file_proof` 变为适配器 |

两处拒绝都**不走桥接失败形状**，而是走 `value` 里的类型化结果（`{"accepted": false, "refusal": ...}` / `{"ok": false, "refusal": ...}`）。失败形状（`{ok:false, code}` + 退出码 2）保留原义，用于"桥接本身坏了"。

## 两级验证

1. **逐项合同**：Go 单元测试 + **差分对照**。迁移前的实现逐字保留在仅测试使用的位置（`tests/reference_result.py`、`tests/reference_evidence.py`），生产代码不导入；用固定样本比较判定、拒绝原因与接受值。
   - `test_result_frame_differential.py`：71 个输入（Unicode 非 BMP／组合字符／转义／RTL／Unicode 键、空集合、各类拒绝）全部一致。
   - `test_evidence_proof_differential.py`：15 类真实文件（空／二进制／CRLF／大文件／Unicode 文件名／符号链接／断链／目录／不可读／不存在）证明与拒绝全部一致。
2. **累计回归**：每个单元都跑完整入口（9 项必需项），不是只跑本块测试。

## 复核命令

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine

# 准入：读取记录并核验结果、身份与运行环境
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-C2.json --next-unit C3

# 完整验收（新尝试目录，不覆盖历史）
python3 scripts/go-engine/acceptance.py --unit C2

# 两个工具自身的控制项（需要干净工作树）
sh scripts/go-engine/negative-controls.sh "" .gates/results/go-engine-C2.json
```

记录、合同与工具说明：

| 文件 | 内容 |
| --- | --- |
| `.gates/results/go-engine-B0.md` / `.json` | 基线冻结记录 |
| `.gates/results/go-engine-C1-contract.md` / `C2-contract.md` | 实施前固定的单元合同 |
| `.gates/results/go-engine-C1.md` / `C2.md` 与同名 `.json` | 单元验收记录（含各必需项结论、尝试目录、摘要见证） |
| `scripts/go-engine/README.md` | 验收与准入工具的约定、判定条件、控制项清单 |
| `scripts/go-engine/dependency-lock.json` | Go/SQLite 驱动与依赖锁定 |

原始证据在记录 `attempt` 字段指向的目录（`.harness/validation/acceptance/...`），含 `evidence.json`（逐文件摘要）、各必需项 `summary.json`、每个命令的 stdout/stderr/exit/cmd。**未被后续运行覆盖**：每次尝试使用全新目录，已存在即拒绝。

## 明确未完成与未验证的事项

**未搬动（G1a 范围内的技术性缺口）**

- `verify_process` / `verify_evidence`：它们比较 `digest(request)`，即 Python 命名空间的 JSON 规范化摘要。实测确认两侧摘要互不比较、各属独立命名空间（Go 转义 `<`/`&`，Python 不转义），**搬动会改变 Python 侧每一个摘要**，属方案 12.2 的版本化格式迁移，不在语言替换范围内。登记为 `CANONICALISATION-FORMAT-BLOCKED`。
- `verify_source` / `validate_binding`：宿主编配器生命周期策略，随适配器在 G2b/G2c 迁移。
- `read` / `canonical` / `atomic`：通用 JSON 读取与落盘原语，见各单元合同中的理由。

**未验证**

- 真实原生模型流水线：`NATIVE-REVERIFY-AT-BASELINE` 仍未执行，需要显式授权费用。本阶段两项都是纯计算，**不需要**原生定向验收；授权关口在 G2b 之前。此前 `live-20260915-2050` 的原生证据绑定更早的提交与运行时，不可当作当前候选的原生验收。
- 跨平台：只在本地 macOS 实测。

**已知有界差异与风险**

| 编号 | 内容 |
| --- | --- |
| `LONE-SURROGATE-ESCAPE` | 文本含转义 `\ud800` 时两侧都接受，但 Python 保留代理项码位、Go 为 U+FFFD |
| `ENV-THROUGHPUT-UNIDENTIFIED` | 独立环境整体吞吐约为本机 1/4，成因未查明；回归判定已改为不依赖吞吐（60m 后备 + 240s 停滞预算） |
| `BUILD-PATH-IN-RUNTIME-IDENTITY` | `go build` 不加 `-trimpath` 时运行时身份随构建路径变化 |
| `NODE-PATH-AMBIGUOUS` | 机器上两份 node 摘要不同，跨 shell 恢复可能触发 `HOST_CHANGED` |
| `PROOF-CALL-CROSSES-PROCESS` | 细粒度原语跨进程约 6 ms/次，已测量 |
| `ZCODE-HEADLESS-BLOCK` | ZCode 无头在原生 Edit 审批处按合同返回 `ZCODE_INTERACTION_REQUIRED` |

## 评审时值得重点看的点

1. **差分对照是否足够**：样本集是否覆盖了你关心的边界；拒绝原因逐字不变是否是正确的约束。
2. **C2 的范围判定**：`CANONICALISATION-FORMAT-BLOCKED` 是否接受为"G1a 内的合理缺口"，还是要求先做规范化迁移。
3. **跨进程原语的取舍**：`file_proof` 从进程内 0.036 ms 变为 6.1 ms，是否接受这一层次的代价。
4. **拒绝走载荷而非失败形状**：这一设计是否与既有桥接合同兼容。
5. **验收工具本身**：`scripts/go-engine/README.md` 列的判定条件与控制项，尤其是"任何以检测某现象为放行条件的工具，必须有故意制造该现象的控制项"。

## 建议的下一步（未开始）

- G1b：比较材料、反馈包装与工件发布。
- 或按评审意见先处理 `CANONICALISATION-FORMAT-BLOCKED`。

本索引只汇总各单元记录中已观测的结果；不含密钥或认证材料。
